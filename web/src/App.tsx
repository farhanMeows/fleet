import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  dispatch as apiDispatch,
  fetchCosts,
  fetchEvents,
  fetchInbox,
  fetchProjects,
  fetchSessions,
} from "./api";
import { parseCommand } from "./command";
import { CommandBar, type CmdMessage } from "./components/CommandBar";
import { Events } from "./components/Events";
import { Inbox } from "./components/Inbox";
import { KeymapOverlay } from "./components/KeymapOverlay";
import { ProjectTable } from "./components/ProjectTable";
import { Transcript } from "./components/Transcript";
import { buildRows } from "./model";
import type {
  FleetEvent,
  InboxItem,
  Project,
  Session,
  UsageRow,
} from "./types";
import { useStream } from "./useStream";
import { fmtTokens } from "./util";

interface TranscriptTarget {
  sessionId: string;
  project: string;
}

export function App() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [sessions, setSessions] = useState<Record<string, Session>>({});
  const [events, setEvents] = useState<FleetEvent[]>([]);
  const [inbox, setInbox] = useState<InboxItem[]>([]);
  const [usage, setUsage] = useState<UsageRow[]>([]);

  const [filter, setFilter] = useState("");
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [transcript, setTranscript] = useState<TranscriptTarget | null>(null);

  const [cmd, setCmd] = useState("");
  const [cmdMsg, setCmdMsg] = useState<CmdMessage | null>(null);
  const [cmdFocused, setCmdFocused] = useState(false);

  const [showKeymap, setShowKeymap] = useState(false);
  const [scanlines, setScanlines] = useState(true);
  const [now, setNow] = useState(() => Date.now());

  const filterRef = useRef<HTMLInputElement>(null);
  const cmdRef = useRef<HTMLInputElement>(null);
  const fadeTimer = useRef<number>();

  const sessionList = useMemo(() => Object.values(sessions), [sessions]);

  // ---- initial load ----
  const loadInbox = useCallback(() => {
    fetchInbox().then(setInbox).catch(() => {});
  }, []);
  const loadEvents = useCallback(() => {
    fetchEvents(60).then(setEvents).catch(() => {});
  }, []);
  const loadCosts = useCallback(() => {
    fetchCosts(1).then(setUsage).catch(() => {});
  }, []);

  useEffect(() => {
    fetchProjects().then(setProjects).catch(() => {});
    fetchSessions()
      .then((list) => {
        const m: Record<string, Session> = {};
        for (const s of list) m[s.session_id] = s;
        setSessions(m);
      })
      .catch(() => {});
    loadEvents();
    loadInbox();
    loadCosts();
  }, [loadEvents, loadInbox, loadCosts]);

  // ---- token usage poll (fleet-wide + per-project today) ----
  useEffect(() => {
    const id = window.setInterval(loadCosts, 30000);
    return () => window.clearInterval(id);
  }, [loadCosts]);

  // ---- SSE live stream ----
  const mergeSession = useCallback((s: Session) => {
    setSessions((prev) => ({ ...prev, [s.session_id]: s }));
  }, []);
  const connected = useStream(mergeSession, loadInbox);

  // ---- periodic polling (events + inbox as SSE-independent fallback) ----
  useEffect(() => {
    const id = window.setInterval(() => {
      loadEvents();
      loadInbox();
      // refresh registry occasionally so newly-added projects appear
      fetchProjects().then(setProjects).catch(() => {});
    }, 4000);
    return () => window.clearInterval(id);
  }, [loadEvents, loadInbox]);

  // ---- local age clock ----
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, []);

  // ---- derived rows ----
  const { rows, selectable } = useMemo(
    () => buildRows(projects, sessionList, expanded, filter),
    [projects, sessionList, expanded, filter],
  );

  // Per-project "live tail": the 3 most recent tool/activity events, ordered
  // oldest→newest, for rendering under active project rows. `events` arrives
  // newest-first from the API.
  const tailsByProject = useMemo(() => {
    const m = new Map<string, FleetEvent[]>();
    for (const e of events) {
      if (!e.tool && !e.summary) continue; // skip contentless bookkeeping events
      const arr = m.get(e.project);
      if (!arr) m.set(e.project, [e]);
      else if (arr.length < 3) arr.push(e);
    }
    for (const [k, arr] of m) m.set(k, arr.reverse());
    return m;
  }, [events]);

  // Today's token usage: per-project map + fleet-wide totals.
  const { usageByProject, totalIn, totalOut } = useMemo(() => {
    const map = new Map<string, { input: number; output: number }>();
    let ti = 0;
    let to = 0;
    for (const u of usage) {
      map.set(u.project, {
        input: u.input_tokens,
        output: u.output_tokens,
      });
      ti += u.input_tokens;
      to += u.output_tokens;
    }
    return { usageByProject: map, totalIn: ti, totalOut: to };
  }, [usage]);

  // Keep selection valid as rows change.
  useEffect(() => {
    if (selectable.length === 0) {
      if (selectedId !== null) setSelectedId(null);
      return;
    }
    if (!selectedId || !selectable.includes(selectedId)) {
      setSelectedId(selectable[0]);
    }
  }, [selectable, selectedId]);

  // ---- selection movement ----
  const move = useCallback(
    (dir: 1 | -1) => {
      if (selectable.length === 0) return;
      const idx = selectedId ? selectable.indexOf(selectedId) : -1;
      const next = Math.max(
        0,
        Math.min(selectable.length - 1, (idx < 0 ? 0 : idx) + dir),
      );
      setSelectedId(selectable[next]);
    },
    [selectable, selectedId],
  );

  const sessionById = useCallback(
    (id: string) => sessions[id],
    [sessions],
  );

  const openTranscript = useCallback(
    (sessionId: string) => {
      const s = sessionById(sessionId);
      setTranscript({ sessionId, project: s?.project ?? "?" });
    },
    [sessionById],
  );

  // Enter / double-click behavior.
  const activate = useCallback(
    (id: string) => {
      setSelectedId(id);
      if (id.startsWith("sess:")) {
        openTranscript(id.slice(5));
        return;
      }
      // project row
      const name = id.slice(5);
      const projSessions = sessionList.filter((s) => s.project === name);
      if (projSessions.length > 1) {
        setExpanded((prev) => {
          const n = new Set(prev);
          if (n.has(id)) n.delete(id);
          else n.add(id);
          return n;
        });
      } else if (projSessions.length === 1) {
        openTranscript(projSessions[0].session_id);
      }
    },
    [sessionList, openTranscript],
  );

  // ---- command bar ----
  const flashMessage = useCallback(
    (text: string, kind: "ok" | "err", autofade: boolean) => {
      window.clearTimeout(fadeTimer.current);
      setCmdMsg({ text, kind, fading: false });
      if (autofade) {
        fadeTimer.current = window.setTimeout(() => {
          setCmdMsg((m) => (m ? { ...m, fading: true } : m));
          fadeTimer.current = window.setTimeout(
            () => setCmdMsg(null),
            1200,
          );
        }, 2600);
      }
    },
    [],
  );

  const runCommand = useCallback(async () => {
    const parsed = parseCommand(cmd);
    switch (parsed.kind) {
      case "empty":
        return;
      case "help":
        flashMessage(
          "commands: dispatch <project>: <prompt> · d <p>: … · force-dispatch <p>: … · help",
          "ok",
          false,
        );
        setCmd("");
        return;
      case "error":
        flashMessage(parsed.message, "err", false);
        return;
      case "dispatch": {
        flashMessage(`dispatching to ${parsed.project}…`, "ok", false);
        const res = await apiDispatch(
          parsed.project,
          parsed.prompt,
          parsed.force,
        );
        if (res.ok) {
          flashMessage(`→ dispatched to ${parsed.project}`, "ok", true);
          setCmd("");
        } else {
          const msg =
            res.message ||
            (res.status === 409
              ? `${parsed.project} is awaiting a permission decision`
              : `dispatch failed (${res.status})`);
          flashMessage(msg, "err", false);
        }
        return;
      }
    }
  }, [cmd, flashMessage]);

  // ---- focus helpers ----
  const focusFilter = () => {
    setShowKeymap(false);
    setTimeout(() => filterRef.current?.focus(), 0);
  };
  const focusCommand = () => {
    setShowKeymap(false);
    setTimeout(() => cmdRef.current?.focus(), 0);
  };

  // Inbox click → select project + open transcript for that session.
  const pickInbox = useCallback(
    (item: InboxItem) => {
      const projId = `proj:${item.project}`;
      const projSessions = sessionList.filter((s) => s.project === item.project);
      if (projSessions.length > 1) {
        setExpanded((prev) => new Set(prev).add(projId));
        setSelectedId(`sess:${item.session_id}`);
      } else {
        setSelectedId(projId);
      }
      if (item.session_id) {
        setTranscript({ sessionId: item.session_id, project: item.project });
      }
    },
    [sessionList],
  );

  // ---- global keyboard ----
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // Cmd/Ctrl+K focuses the command bar from anywhere.
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        focusCommand();
        return;
      }

      const t = e.target as HTMLElement | null;
      const typing =
        t &&
        (t.tagName === "INPUT" ||
          t.tagName === "TEXTAREA" ||
          t.isContentEditable);

      if (typing) {
        // Filter input: Escape clears + blurs. Command bar handles its own keys.
        if (e.key === "Escape" && t === filterRef.current) {
          e.preventDefault();
          setFilter("");
          filterRef.current?.blur();
        }
        return;
      }

      if (showKeymap && e.key === "Escape") {
        setShowKeymap(false);
        return;
      }

      switch (e.key) {
        case "j":
        case "ArrowDown":
          e.preventDefault();
          move(1);
          break;
        case "k":
        case "ArrowUp":
          e.preventDefault();
          move(-1);
          break;
        case "Enter":
          if (selectedId) {
            e.preventDefault();
            activate(selectedId);
          }
          break;
        case "Escape":
          if (transcript) setTranscript(null);
          else if (filter) setFilter("");
          break;
        case "/":
          e.preventDefault();
          focusFilter();
          break;
        case ":":
          e.preventDefault();
          focusCommand();
          break;
        case "?":
          e.preventDefault();
          setShowKeymap((v) => !v);
          break;
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [move, activate, selectedId, transcript, filter, showKeymap]);

  const activeCount = sessionList.filter((s) => s.state !== "ended").length;

  return (
    <div className="app">
      {scanlines && <div className="scanlines" />}

      <div className="top-bar">
        <span className="brand">
          <span className="glow">▓ FLEET</span>{" "}
          <span style={{ color: "var(--dimmer)", fontWeight: 400 }}>
            mission control
          </span>
        </span>
        <div className="right">
          {(totalIn > 0 || totalOut > 0) && (
            <span className="usage" title="fleet-wide token usage today">
              today {fmtTokens(totalIn)} in / {fmtTokens(totalOut)} out
            </span>
          )}
          <span className={"status " + (connected ? "live" : "offline")}>
            <span className="dot">●</span>
            {connected ? "live" : "offline"}
          </span>
          <button
            className={"icon-btn" + (scanlines ? " active" : "")}
            title="toggle scanlines"
            onClick={() => setScanlines((v) => !v)}
          >
            scan
          </button>
          <button
            className="icon-btn"
            title="keymap (?)"
            onClick={() => setShowKeymap((v) => !v)}
          >
            ?
          </button>
        </div>
      </div>

      <div className="main">
        <div className="col-left">
          <ProjectTable
            rows={rows}
            tails={tailsByProject}
            usage={usageByProject}
            selectedId={selectedId}
            now={now}
            filter={filter}
            onFilterChange={setFilter}
            filterRef={filterRef}
            onSelect={setSelectedId}
            onActivate={activate}
          />
        </div>
        <div className="col-right">
          <Inbox items={inbox} now={now} onPick={pickInbox} />
          <Events events={events} />
        </div>
      </div>

      {transcript && (
        <Transcript
          sessionId={transcript.sessionId}
          project={transcript.project}
          onClose={() => setTranscript(null)}
        />
      )}

      <CommandBar
        ref={cmdRef}
        value={cmd}
        onChange={setCmd}
        onSubmit={runCommand}
        onEscape={() => {
          setCmd("");
          cmdRef.current?.blur();
        }}
        message={cmdMsg}
        focused={cmdFocused}
      />

      <div className="footer">
        <span>fleet v0.1</span>
        <span className="sep">·</span>
        <span>{projects.length} projects</span>
        <span className="sep">·</span>
        <span>{activeCount} active sessions</span>
        <span className="sep">·</span>
        <span>localhost:7433</span>
      </div>

      {showKeymap && <KeymapOverlay onClose={() => setShowKeymap(false)} />}

      {/* focus tracking for the command cursor */}
      <FocusProbe inputRef={cmdRef} onChange={setCmdFocused} />
    </div>
  );
}

// Tracks focus on the command input to drive the blinking cursor.
function FocusProbe({
  inputRef,
  onChange,
}: {
  inputRef: React.RefObject<HTMLInputElement>;
  onChange: (v: boolean) => void;
}) {
  useEffect(() => {
    const el = inputRef.current;
    if (!el) return;
    const on = () => onChange(true);
    const off = () => onChange(false);
    el.addEventListener("focus", on);
    el.addEventListener("blur", off);
    onChange(document.activeElement === el);
    return () => {
      el.removeEventListener("focus", on);
      el.removeEventListener("blur", off);
    };
  }, [inputRef, onChange]);
  return null;
}
