import { Fragment } from "react";

interface Props {
  onClose: () => void;
}

const KEYS: Array<[string, string] | { group: string }> = [
  { group: "NAVIGATION" },
  ["j / ↓", "move selection down"],
  ["k / ↑", "move selection up"],
  ["enter", "expand project / open transcript"],
  ["esc", "close transcript · clear filter · blur"],
  { group: "SEARCH & COMMANDS" },
  ["/", "filter projects"],
  [": or ⌘K", "focus command bar"],
  ["?", "toggle this keymap"],
  { group: "COMMANDS" },
  ["dispatch p: …", "send a prompt to project p"],
  ["d p: …", "shorthand for dispatch"],
  ["force-dispatch p: …", "dispatch even if busy"],
  ["help", "show command help"],
];

export function KeymapOverlay({ onClose }: Props) {
  return (
    <div className="overlay" onClick={onClose}>
      <div className="card" onClick={(e) => e.stopPropagation()}>
        <div className="panel-head">
          <span className="title">┌─ KEYMAP</span>
          <span className="close" onClick={onClose}>
            ✕
          </span>
        </div>
        <div className="km">
          {KEYS.map((k, i) =>
            "group" in k ? (
              <div className="group" key={i}>
                {k.group}
              </div>
            ) : (
              <Fragment key={i}>
                <div className="key">{k[0]}</div>
                <div className="desc">{k[1]}</div>
              </Fragment>
            ),
          )}
        </div>
      </div>
    </div>
  );
}
