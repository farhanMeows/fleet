export type SessionState = "idle" | "working" | "needs_input" | "ended";

export interface Session {
  session_id: string;
  project: string;
  cwd: string;
  transcript_path?: string;
  state: SessionState;
  tool?: string;
  summary?: string;
  started_at: number;
  updated_at: number;
}

export interface Project {
  name: string;
  path: string;
  created_at: number;
}

export interface FleetEvent {
  id: number;
  session_id: string;
  project: string;
  event: string;
  tool?: string;
  summary?: string;
  cwd: string;
  created_at: number;
}

export interface InboxItem {
  kind: "permission" | "review";
  project: string;
  session_id: string;
  summary: string;
  since: number;
}

export type TranscriptRole = "user" | "assistant" | "tool";

export interface TranscriptEntry {
  role: TranscriptRole;
  text: string;
}
