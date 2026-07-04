import type { FleetEvent } from "../types";
import { clockTime } from "../util";

interface Props {
  events: FleetEvent[];
}

function eventClass(name: string): string {
  if (name.startsWith("PreTool")) return "e-pre";
  if (name.startsWith("PostTool")) return "e-post";
  if (name === "Notification") return "e-notify";
  if (name === "Stop" || name === "SubagentStop") return "e-stop";
  if (name === "SessionStart" || name === "UserPromptSubmit") return "e-start";
  return "e-other";
}

export function Events({ events }: Props) {
  return (
    <div className="panel" style={{ flex: "1 1 auto", minHeight: 0 }}>
      <div className="panel-head">
        <span className="title">├─ EVENTS</span>
      </div>
      <div className="panel-body">
        {events.length === 0 && <div className="empty">no events yet</div>}
        {events.map((e) => (
          <div className="evt" key={e.id}>
            <span className="ts">{clockTime(e.created_at)}</span>
            <span className="proj">{e.project}</span>
            <span className={"name " + eventClass(e.event)}>{e.event}</span>
            <span className="sum">
              {e.tool && <span className="tool">{e.tool}</span>}
              {e.tool && e.summary ? ": " : ""}
              {e.summary}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}
