import type {
  FleetEvent,
  InboxItem,
  Project,
  Session,
  TranscriptEntry,
  UsageRow,
} from "./types";

async function getJSON<T>(url: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(url, { signal });
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  return (await res.json()) as T;
}

export async function fetchSessions(signal?: AbortSignal): Promise<Session[]> {
  const d = await getJSON<{ sessions: Session[] }>("/api/sessions", signal);
  return d.sessions ?? [];
}

export async function fetchProjects(signal?: AbortSignal): Promise<Project[]> {
  const d = await getJSON<{ projects: Project[] }>("/api/projects", signal);
  return d.projects ?? [];
}

export async function fetchEvents(
  limit = 50,
  signal?: AbortSignal,
): Promise<FleetEvent[]> {
  const d = await getJSON<{ events: FleetEvent[] }>(
    `/api/events?limit=${limit}`,
    signal,
  );
  return d.events ?? [];
}

export async function fetchInbox(signal?: AbortSignal): Promise<InboxItem[]> {
  // The inbox endpoint may 404 until the backend lands; treat that as empty.
  try {
    const d = await getJSON<{ items: InboxItem[] }>("/api/inbox", signal);
    return d.items ?? [];
  } catch (e) {
    if (e instanceof Error && e.message.startsWith("404")) return [];
    throw e;
  }
}

export async function fetchCosts(
  days = 1,
  signal?: AbortSignal,
): Promise<UsageRow[]> {
  try {
    const d = await getJSON<{ usage: UsageRow[] }>(
      `/api/costs?days=${days}`,
      signal,
    );
    return d.usage ?? [];
  } catch (e) {
    if (e instanceof Error && e.message.startsWith("404")) return [];
    throw e;
  }
}

export interface TranscriptPage {
  entries: TranscriptEntry[];
  offset: number;
}

export async function fetchTranscript(
  sessionId: string,
  after: number,
  signal?: AbortSignal,
): Promise<TranscriptPage> {
  const d = await getJSON<Partial<TranscriptPage>>(
    `/api/transcript/${encodeURIComponent(sessionId)}?after=${after}`,
    signal,
  );
  return { entries: d.entries ?? [], offset: d.offset ?? after };
}

export interface DispatchResult {
  ok: boolean;
  status: number;
  message: string;
}

export async function dispatch(
  project: string,
  prompt: string,
  force = false,
): Promise<DispatchResult> {
  const res = await fetch("/api/dispatch", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ project, prompt, force }),
  });
  const text = (await res.text()).trim();
  return { ok: res.ok, status: res.status, message: text };
}
