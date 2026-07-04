import { forwardRef } from "react";

export interface CmdMessage {
  text: string;
  kind: "ok" | "err";
  fading: boolean;
}

interface Props {
  value: string;
  onChange: (v: string) => void;
  onSubmit: () => void;
  onEscape: () => void;
  message: CmdMessage | null;
  focused: boolean;
}

export const CommandBar = forwardRef<HTMLInputElement, Props>(
  function CommandBar(
    { value, onChange, onSubmit, onEscape, message, focused },
    ref,
  ) {
    return (
      <div className="cmd-bar">
        {message && (
          <div
            className={
              "cmd-msg " + message.kind + (message.fading ? " fade" : "")
            }
          >
            {message.text}
          </div>
        )}
        <div className="cmd-input-row">
          <span className="sigil">:</span>
          <div className="cmd-input-wrap">
            <input
              ref={ref}
              value={value}
              spellCheck={false}
              autoComplete="off"
              placeholder="dispatch <project>: <prompt>   ·   help"
              onChange={(e) => onChange(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  onSubmit();
                } else if (e.key === "Escape") {
                  e.preventDefault();
                  onEscape();
                }
              }}
            />
            {focused && <span className="cursor">▊</span>}
          </div>
        </div>
      </div>
    );
  },
);
