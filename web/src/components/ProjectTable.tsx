import { Fragment, useEffect, useRef } from "react";
import type { Row } from "../model";
import type { FleetEvent, SessionState } from "../types";
import {
  STATE_CLASS,
  STATE_ICON,
  STATE_LABEL,
  age,
  clockTime,
  fmtTokens,
} from "../util";

interface Props {
  rows: Row[];
  tails: Map<string, FleetEvent[]>;
  usage: Map<string, { input: number; output: number }>;
  selectedId: string | null;
  now: number;
  filter: string;
  onFilterChange: (v: string) => void;
  filterRef: React.RefObject<HTMLInputElement>;
  onSelect: (id: string) => void;
  onActivate: (id: string) => void;
}

// Live activity tail: recent events shown dim, indented under an active row.
function Tail({ events }: { events: FleetEvent[] }) {
  return (
    <>
      {events.map((e) => (
        <div className="tail-line" key={e.id}>
          <span className="tail-ts">{clockTime(e.created_at)}</span>
          {e.tool && <span className="tail-tool">{e.tool}</span>}
          {e.tool && e.summary ? ": " : ""}
          <span className="tail-sum">{e.summary}</span>
        </div>
      ))}
    </>
  );
}

function Detail({ tool, summary }: { tool?: string; summary?: string }) {
  if (!tool && !summary) return <span>—</span>;
  return (
    <>
      {tool && <span className="tool">{tool}</span>}
      {tool && summary ? ": " : ""}
      {summary}
    </>
  );
}

export function ProjectTable({
  rows,
  tails,
  usage,
  selectedId,
  now,
  filter,
  onFilterChange,
  filterRef,
  onSelect,
  onActivate,
}: Props) {
  const bodyRef = useRef<HTMLDivElement>(null);

  // Keep the selected row scrolled into view during j/k navigation.
  useEffect(() => {
    if (!selectedId || !bodyRef.current) return;
    const el = bodyRef.current.querySelector<HTMLElement>(
      `[data-rowid="${CSS.escape(selectedId)}"]`,
    );
    el?.scrollIntoView({ block: "nearest" });
  }, [selectedId]);

  return (
    <div className="panel col-left-panel" style={{ flex: 1, minHeight: 0 }}>
      <div className="panel-head">
        <span className="title">┌─ FLEET</span>
        <span style={{ color: "var(--dimmer)", fontSize: 11 }}>
          j/k move · enter open · / filter
        </span>
      </div>
      <div className="filter-row">
        <span className="prompt">/</span>
        <input
          ref={filterRef}
          value={filter}
          placeholder="filter projects…"
          spellCheck={false}
          onChange={(e) => onFilterChange(e.target.value)}
        />
        {filter && <span className="hint">esc to clear</span>}
      </div>
      <div className="panel-body" ref={bodyRef}>
        <div className="rows">
          {rows.length === 0 && <div className="empty">no matches</div>}
          {rows.map((row) => {
            if (row.kind === "divider") {
              return (
                <div className="divider" key={row.id}>
                  {row.label}
                </div>
              );
            }
            if (row.kind === "project") {
              const st = row.state;
              const icon = st === "none" ? "○" : STATE_ICON[st];
              const iconCls =
                st === "none" ? "st-none" : STATE_CLASS[st as SessionState];
              const label =
                st === "none" ? "NO SESSION" : STATE_LABEL[st as SessionState];
              const isSel = selectedId === row.id;
              const multi = row.sessions.length > 1;
              const active = st === "working" || st === "needs_input";
              const tail = active ? tails.get(row.name) : undefined;
              const u = active ? usage.get(row.name) : undefined;
              const hasUsage = u && (u.input > 0 || u.output > 0);
              return (
                <Fragment key={row.id}>
                  <div
                    data-rowid={row.id}
                    className={
                      "row" +
                      (isSel ? " sel" : "") +
                      (st === "needs_input" ? " needs" : "")
                    }
                    onClick={() => onSelect(row.id)}
                    onDoubleClick={() => onActivate(row.id)}
                  >
                    <span className={"icon " + iconCls}>{icon}</span>
                    <span className="name">
                      {row.name}
                      {multi ? ` (${row.sessions.length})` : ""}
                    </span>
                    <span className={"state " + iconCls}>{label}</span>
                    <span className="detail">
                      <Detail
                        tool={row.lead?.tool}
                        summary={row.lead?.summary}
                      />
                    </span>
                    <span className="age">
                      {row.lead ? age(row.lead.updated_at, now) : ""}
                    </span>
                  </div>
                  {hasUsage && u && (
                    <div className="tail-line usage-tail">
                      <span className="tail-tree">└</span>
                      <span className="tail-sum">
                        {fmtTokens(u.input)} in / {fmtTokens(u.output)} out today
                      </span>
                    </div>
                  )}
                  {tail && tail.length > 0 && <Tail events={tail} />}
                </Fragment>
              );
            }
            // session row
            const s = row.session;
            const isSel = selectedId === row.id;
            return (
              <div
                key={row.id}
                data-rowid={row.id}
                className={
                  "row session" +
                  (isSel ? " sel" : "") +
                  (s.state === "needs_input" ? " needs" : "")
                }
                onClick={() => onSelect(row.id)}
                onDoubleClick={() => onActivate(row.id)}
              >
                <span className="tree">└</span>
                <span className={"icon " + STATE_CLASS[s.state]}>
                  {STATE_ICON[s.state]}
                </span>
                <span className="name">{s.session_id.slice(0, 8)}</span>
                <span className={"state " + STATE_CLASS[s.state]}>
                  {STATE_LABEL[s.state]}
                </span>
                <span className="detail">
                  <Detail tool={s.tool} summary={s.summary} />
                </span>
                <span className="age">{age(s.updated_at, now)}</span>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
