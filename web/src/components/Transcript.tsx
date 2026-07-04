import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { fetchTranscript } from "../api";
import type { TranscriptEntry } from "../types";

interface Props {
  sessionId: string;
  project: string;
  onClose: () => void;
}

const PFX: Record<TranscriptEntry["role"], string> = {
  user: "❯ ",
  assistant: "  ",
  tool: "⏺ ",
};
const CLS: Record<TranscriptEntry["role"], string> = {
  user: "tr-user",
  assistant: "tr-assistant",
  tool: "tr-tool",
};

export function Transcript({ sessionId, project, onClose }: Props) {
  const [entries, setEntries] = useState<TranscriptEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [missing, setMissing] = useState(false);
  const [atBottom, setAtBottom] = useState(true);
  const [hasNew, setHasNew] = useState(false);
  const bodyRef = useRef<HTMLDivElement>(null);
  const offsetRef = useRef(0);
  const atBottomRef = useRef(true);

  // Poll the transcript for this session, appending new entries every 2s.
  useEffect(() => {
    let cancelled = false;
    offsetRef.current = 0;
    setEntries([]);
    setLoading(true);
    setMissing(false);
    setHasNew(false);
    atBottomRef.current = true;
    setAtBottom(true);

    const poll = async () => {
      try {
        const page = await fetchTranscript(sessionId, offsetRef.current);
        if (cancelled) return;
        setLoading(false);
        setMissing(false);
        if (page.entries.length > 0) {
          offsetRef.current = page.offset;
          setEntries((prev) => [...prev, ...page.entries]);
          if (!atBottomRef.current) setHasNew(true);
        }
      } catch (e) {
        if (cancelled) return;
        setLoading(false);
        // Endpoint may not be live yet; show a gentle notice, keep polling.
        if (e instanceof Error && e.message.startsWith("404")) setMissing(true);
      }
    };

    poll();
    const id = window.setInterval(poll, 2000);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, [sessionId]);

  // Auto-scroll to bottom on new entries, unless the user scrolled up.
  useLayoutEffect(() => {
    const el = bodyRef.current;
    if (!el) return;
    if (atBottomRef.current) {
      el.scrollTop = el.scrollHeight;
      setHasNew(false);
    }
  }, [entries]);

  const onScroll = () => {
    const el = bodyRef.current;
    if (!el) return;
    const bottom = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
    atBottomRef.current = bottom;
    setAtBottom(bottom);
    if (bottom) setHasNew(false);
  };

  const jumpDown = () => {
    const el = bodyRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
    atBottomRef.current = true;
    setAtBottom(true);
    setHasNew(false);
  };

  return (
    <div className="panel transcript">
      <div className="panel-head">
        <span className="title">
          ┌─ TRANSCRIPT: {project} · {sessionId.slice(0, 8)}
        </span>
        <span className="close" title="close (esc)" onClick={onClose}>
          ✕
        </span>
      </div>
      <div className="panel-body" ref={bodyRef} onScroll={onScroll}>
        {loading && entries.length === 0 && (
          <div className="tr-loading">loading transcript…</div>
        )}
        {!loading && missing && entries.length === 0 && (
          <div className="tr-loading">
            transcript unavailable (endpoint not ready)
          </div>
        )}
        {!loading && !missing && entries.length === 0 && (
          <div className="tr-loading">— empty —</div>
        )}
        {entries.map((e, i) => (
          <div className={"tr-line " + CLS[e.role]} key={i}>
            <span className="pfx">{PFX[e.role]}</span>
            {e.text}
          </div>
        ))}
      </div>
      {!atBottom && hasNew && (
        <button className="new-chip" onClick={jumpDown}>
          ▼ new
        </button>
      )}
    </div>
  );
}
