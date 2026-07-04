import { useEffect, useRef, useState } from "react";
import type { Session } from "./types";

interface StreamMessage {
  type: string;
  data: Session;
}

/**
 * Subscribes to the daemon's SSE stream. Each "session" message merges the
 * session object into state via onSession. Reconnects with capped backoff and
 * reports connection status so the header can show live/offline.
 *
 * onTransition fires when a session enters needs_input or idle so the caller
 * can re-fetch the inbox.
 */
export function useStream(
  onSession: (s: Session) => void,
  onTransition: () => void,
): boolean {
  const [connected, setConnected] = useState(false);
  const cbSession = useRef(onSession);
  const cbTransition = useRef(onTransition);
  cbSession.current = onSession;
  cbTransition.current = onTransition;

  useEffect(() => {
    let es: EventSource | null = null;
    let retry = 0;
    let timer: number | undefined;
    let closed = false;
    // Last known state per session, to detect transitions into idle/needs_input.
    const prev = new Map<string, string>();

    const connect = () => {
      if (closed) return;
      es = new EventSource("/api/stream");

      es.onopen = () => {
        retry = 0;
        setConnected(true);
      };

      es.onmessage = (ev) => {
        if (!ev.data) return;
        let msg: StreamMessage;
        try {
          msg = JSON.parse(ev.data);
        } catch {
          return;
        }
        if (msg.type !== "session" || !msg.data) return;
        const s = msg.data;
        cbSession.current(s);
        const before = prev.get(s.session_id);
        if (
          before !== s.state &&
          (s.state === "needs_input" || s.state === "idle")
        ) {
          cbTransition.current();
        }
        prev.set(s.session_id, s.state);
      };

      es.onerror = () => {
        setConnected(false);
        es?.close();
        es = null;
        if (closed) return;
        retry = Math.min(retry + 1, 6);
        const delay = Math.min(1000 * 2 ** (retry - 1), 15000);
        timer = window.setTimeout(connect, delay);
      };
    };

    connect();

    return () => {
      closed = true;
      if (timer) window.clearTimeout(timer);
      es?.close();
    };
  }, []);

  return connected;
}
