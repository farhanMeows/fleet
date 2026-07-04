import type { SessionState } from "./types";

// Compact "age" formatting from a unix-seconds timestamp to now.
export function age(sinceSec: number, nowMs: number): string {
  const s = Math.max(0, Math.floor(nowMs / 1000 - sinceSec));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h`;
  const d = Math.floor(h / 24);
  return `${d}d`;
}

export function clockTime(sec: number): string {
  const d = new Date(sec * 1000);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

// Terminal-style compact token count: >=1M → 1.2M, >=1k → 45.2k (no decimal
// once it reads in the hundreds, e.g. 718k), else the raw number.
export function fmtTokens(n: number): string {
  const trim = (v: number, d: number) =>
    parseFloat(v.toFixed(d)).toString();
  if (n >= 1e6) {
    const v = n / 1e6;
    return trim(v, v >= 100 ? 0 : 1) + "M";
  }
  if (n >= 1e3) {
    const v = n / 1e3;
    return trim(v, v >= 100 ? 0 : 1) + "k";
  }
  return String(n);
}

export const STATE_ICON: Record<SessionState, string> = {
  working: "●", // ●
  needs_input: "⚠", // ⚠
  idle: "✓", // ✓
  ended: "○", // ○
};

export const STATE_CLASS: Record<SessionState, string> = {
  working: "st-working",
  needs_input: "st-needs",
  idle: "st-idle",
  ended: "st-ended",
};

export const STATE_LABEL: Record<SessionState, string> = {
  working: "WORKING",
  needs_input: "NEEDS INPUT",
  idle: "IDLE",
  ended: "ENDED",
};

// Rank used to pick the "worst" (most attention-demanding) state in a project.
const RANK: Record<SessionState, number> = {
  needs_input: 3,
  working: 2,
  idle: 1,
  ended: 0,
};

export function worstState(states: SessionState[]): SessionState {
  let best: SessionState = "ended";
  for (const s of states) {
    if (RANK[s] > RANK[best]) best = s;
  }
  return best;
}
