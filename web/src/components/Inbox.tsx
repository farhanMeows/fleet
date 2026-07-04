import type { InboxItem } from "../types";
import { age } from "../util";

interface Props {
  items: InboxItem[];
  now: number;
  onPick: (item: InboxItem) => void;
}

export function Inbox({ items, now, onPick }: Props) {
  // Oldest-waiting first (smallest `since` timestamp on top).
  const sorted = [...items].sort((a, b) => a.since - b.since);
  return (
    <div className="panel" style={{ flex: "0 0 auto", maxHeight: "38%" }}>
      <div className="panel-head">
        <span className="title">
          ┌─ NEEDS YOU <span className="count">({items.length})</span>
        </span>
      </div>
      <div className="panel-body">
        {sorted.length === 0 && (
          <div className="empty">nothing waiting — all clear</div>
        )}
        {sorted.map((it, i) => {
          const label =
            it.kind === "permission"
              ? it.summary
              : `finished, review${it.summary ? " · " + it.summary : ""}`;
          return (
            <div
              key={it.session_id + i}
              className={"inbox-item " + it.kind}
              onClick={() => onPick(it)}
              title={it.summary}
            >
              <span className="ic">{it.kind === "permission" ? "⚠" : "✓"}</span>
              <span className="who">{it.project}</span>
              <span className="what">— {label}</span>
              <span className="since">({age(it.since, now)})</span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
