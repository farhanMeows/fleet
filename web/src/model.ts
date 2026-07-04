import type { Project, Session, SessionState } from "./types";

export interface ProjectRow {
  kind: "project";
  id: string; // "proj:<name>"
  name: string;
  registered: boolean;
  state: SessionState | "none";
  sessions: Session[];
  // Representative session for the collapsed row detail (worst-state one).
  lead?: Session;
}

export interface SessionRow {
  kind: "session";
  id: string; // "sess:<session_id>"
  parent: string;
  session: Session;
}

export interface DividerRow {
  kind: "divider";
  id: string;
  label: string;
}

export type Row = ProjectRow | SessionRow | DividerRow;

const RANK: Record<SessionState, number> = {
  needs_input: 3,
  working: 2,
  idle: 1,
  ended: 0,
};

function leadSession(sessions: Session[]): Session | undefined {
  if (sessions.length === 0) return undefined;
  return [...sessions].sort((a, b) => {
    const r = RANK[b.state] - RANK[a.state];
    if (r !== 0) return r;
    return b.updated_at - a.updated_at;
  })[0];
}

export interface BuiltRows {
  rows: Row[];
  // Ordered list of selectable row ids (projects + expanded sessions).
  selectable: string[];
}

export function buildRows(
  projects: Project[],
  sessions: Session[],
  expanded: Set<string>,
  filter: string,
): BuiltRows {
  const f = filter.trim().toLowerCase();
  const match = (name: string) => !f || name.toLowerCase().includes(f);

  const byProject = new Map<string, Session[]>();
  for (const s of sessions) {
    const arr = byProject.get(s.project) ?? [];
    arr.push(s);
    byProject.set(s.project, arr);
  }

  const registeredNames = new Set(projects.map((p) => p.name));
  const rows: Row[] = [];
  const selectable: string[] = [];

  const pushProject = (name: string, registered: boolean) => {
    if (!match(name)) return;
    const sess = byProject.get(name) ?? [];
    const lead = leadSession(sess);
    const state: SessionState | "none" = lead ? lead.state : "none";
    const pid = `proj:${name}`;
    rows.push({
      kind: "project",
      id: pid,
      name,
      registered,
      state,
      sessions: sess,
      lead,
    });
    selectable.push(pid);
    if (expanded.has(pid) && sess.length > 1) {
      const ordered = [...sess].sort(
        (a, b) => RANK[b.state] - RANK[a.state] || b.updated_at - a.updated_at,
      );
      for (const s of ordered) {
        const sid = `sess:${s.session_id}`;
        rows.push({ kind: "session", id: sid, parent: pid, session: s });
        selectable.push(sid);
      }
    }
  };

  // Registered projects first, in alpha order.
  const regSorted = [...projects].sort((a, b) => a.name.localeCompare(b.name));
  for (const p of regSorted) pushProject(p.name, true);

  // Unregistered projects that have sessions.
  const unregistered = [...byProject.keys()]
    .filter((n) => !registeredNames.has(n))
    .sort();
  if (unregistered.length > 0) {
    const anyMatch = unregistered.some(match);
    if (anyMatch) {
      rows.push({ kind: "divider", id: "div:unreg", label: "── UNREGISTERED" });
    }
    for (const n of unregistered) pushProject(n, false);
  }

  return { rows, selectable };
}
