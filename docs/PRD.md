# Fleet — Product Requirements Document

**Status:** v1 in development · **Owner:** Farhan Ahmad · **Last updated:** 2026-07-04

## 1. Problem

Developers (starting with the founder) run multiple AI coding agents (Claude Code) across many projects simultaneously. Today that means juggling terminal tabs/windows to:

- see what each agent is currently doing,
- notice when an agent is blocked waiting for a permission approval,
- hand new work to a specific project's agent,
- keep agents from ever touching production data.

There is no single pane of glass. Claude Code's built-in agent view covers only *background* agents, agent teams cannot span repositories, and no IPC exists to message a running interactive session — so an external product layer is needed.

## 2. Product vision

**Fleet is mission control for AI coding agents.** One binary that watches every agent on your machine, shows a live terminal-style dashboard, alerts you the moment an agent needs you, lets you dispatch/queue work to any project without switching windows, and enforces guardrails (like "never touch prod data") uniformly.

Local-first single-user product now → multi-device/team SaaS later (same event schema, additive cloud sync).

## 3. Target users

1. **v1 (now):** solo developer/agency owner running Claude Code agents across many client/product repos (the founder's daily workflow — 7+ projects).
2. **SaaS (later):** small dev teams and agencies running agent fleets, needing shared visibility, remote monitoring, cost reporting, and policy guardrails.

## 4. Core requirements

| # | Requirement | Notes |
|---|---|---|
| R1 | Monitor every Claude Code session on the machine (working / needs permission / idle) | via user-level hooks; interactive AND background sessions |
| R2 | Unlimited dynamic projects | `fleet add <path>`; unregistered sessions still visible + promotable |
| R3 | Switch to any agent's terminal in ≤2 keystrokes | tmux windows + state icons in window names |
| R4 | Dispatch a prompt to a project's *running* agent without switching windows | tmux bracketed paste; refuse when agent awaits a permission decision |
| R5 | macOS notification when an agent needs approval / finishes | osascript; debounced; suppressed when that window is focused |
| R6 | Web dashboard, terminal-themed, keyboard-first | embedded in the binary, localhost:7433 |
| R7 | Live transcript view + "Needs You" attention inbox | transcript_path from hooks |
| R8 | Task queue, playbooks, cross-project broadcast | auto-dispatch next item when agent goes idle |
| R9 | Cost/token tracking + daily digest | parsed from session transcripts |
| R10 | Git + dev-server health per project | branch/dirty; pm2/docker/port probes |
| R11 | Prod-data guardrail | PreToolUse deny on prod-pattern + destructive verb; fail-open |
| R12 | Remote via hermes-agent (Telegram/WhatsApp…) | fleet exposes CLI/API + webhook outbox; hermes is the phone UI |
| R13 | Never slow down or break Claude | hooks exit 0 in <100ms, no stdout, spool when daemon down |

**Non-goals (v1):** remote permission approval (misclick risk — later behind a flag); Linux/Windows; multi-user auth/billing; editing agent conversations.

## 5. Architecture (implemented shape)

Single Go binary (`fleet`), no CGO, SQLite at `~/.fleet/fleet.db`:

- **`fleet hook <event>`** — invoked by Claude Code hooks (SessionStart, PreToolUse, PermissionRequest, Stop, SessionEnd). Normalizes to a versioned event schema, POSTs to the daemon (700ms timeout), spools to `~/.fleet/spool/` if unreachable. Fast local side-effects (notify, tmux icons) happen here so they work even when the daemon is down.
- **`fleet daemon`** — ingestion API, REST (`/api/sessions|events|projects`), SSE stream, spool drain, queue runner, health prober, webhook outbox, embedded web UI.
- **`fleet` CLI** — `install`, `up`, `add/remove/list`, `status`, `dispatch`, `queue`, `playbook`, `broadcast`, `digest`, `jump`, `guard`.
- **tmux driver** — session `fleet`, window 0 dashboard, one window per registered project running `claude`, tagged with `@fleet_project` (icons rewrite window names: ● working, ⚠ needs input, ✓ done, ○ idle).

### Event schema (v1) — the SaaS-critical contract
`{schema_version, event, session_id, cwd, transcript_path?, tool_name?, summary?, permission_mode?, received_at}` → derived session state: `idle | working | needs_input | ended`.

## 6. Success metrics

- v1 (founder): zero missed permission prompts per day; task handoff to any repo in <5s; all 7+ projects visible in one view; zero prod-data incidents.
- Product: time-to-value <10 min from `brew install` to dashboard; daily active dashboard use.

## 7. Rollout phases

1. ✅ Event pipeline (hooks → daemon → SQLite → API) — verified end-to-end 2026-07-04
2. Registry + tmux mission control (up/status/icons/jump)
3. Notifications + dispatch
4. Terminal-themed web dashboard + transcript + attention inbox
5. Queue + playbooks + broadcast
6. Cost tracking + digest + health panel
7. Prod-data guardrail + CLAUDE.md rules
8. Hermes remote (webhook outbox + docs)
9. Productization groundwork (goreleaser, brew tap, cloud-sync design)

## 8. Risks

| Risk | Mitigation |
|---|---|
| Claude Code hook/CLI surface changes between versions | version-gate in `fleet install`; schema versioning; CI against latest CLI |
| send-keys lands input while a permission dialog is open (could select an option) | dispatch refuses when state=needs_input unless `--force` |
| Hook bugs blocking the user's agents | hooks always exit 0, no stdout, 700ms network timeout, fail silent to log |
| Guardrail false positives blocking dev work | require prod-pattern AND destructive verb; fail-open on internal errors |
| SQLite contention | daemon is sole writer, WAL, single connection |
