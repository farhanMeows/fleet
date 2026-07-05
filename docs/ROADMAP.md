# Fleet — productization roadmap (v1 → sellable SaaS)

v1 (everything in [PRD.md](PRD.md) phases 1–8) is a local, single-user product. This document is the path from "works on the founder's Mac" to "people pay for it". Nothing here is built yet.

## 1. Distribution (first — it compounds everything else)

- **goreleaser**: cross-compile darwin/arm64, darwin/amd64, linux/amd64 (pure-Go SQLite already makes this trivial), GitHub Releases with checksums.
- **Homebrew tap**: `brew install <org>/tap/fleet`. Post-install caveat prints `fleet install && fleet add ...`.
- `fleet doctor`: verify claude CLI version, hooks installed, tmux present, daemon healthy — the first-run support tool.
- Versioned event schema is already in place (`schema_version: 1`); bump discipline + migration tests before any breaking change.

## 2. Cross-platform

- Linux: notify via `notify-send`; everything else already portable. Windows: later (tmux driver becomes a "terminal driver" interface; Windows Terminal/wezterm backends).
- Claude Code version matrix in CI: hooks payloads are the coupling surface — test against latest stable weekly.

## 3. Cloud sync (the SaaS seam)

- `fleet login` → device token. Daemon adds a `sync` component that streams the same versioned events to `api.fleet.dev` (websocket or batched POST, offline-tolerant like the local spool).
- Hosted dashboard = the existing web UI reading from the cloud store instead of localhost — the API contract is already identical.
- Multi-machine: one account, N daemons, machine tag on every event.
- Privacy defaults: event metadata only (states, tools, summaries); transcripts stay local unless explicitly enabled per project.

## 4. Teams (the actual product)

- Org accounts; shared fleet view across teammates' machines; who-approved-what audit log.
- Policy sync: guard patterns and playbooks managed centrally, pushed to every member's daemon (guard rules become org policy — this is the enterprise hook).
- Cost reporting per project/team/day — exportable; managers pay for this line item alone.

## 5. Monetization shape

- **Free**: local-only, everything in v1 (the funnel; also the honest open-core boundary).
- **Pro (per seat)**: cloud sync, phone access without self-hosting hermes, digest emails, multi-machine.
- **Team (per seat, higher)**: shared views, policy sync, audit, SSO.
- License keys checked by the daemon for Pro features; core stays usable offline forever.

## 6. Remote approve — ✅ shipped 2026-07-05 (opt-in)

Implemented as designed: config-gated off by default (`~/.fleet/remote-approve` flag file, created only by the human); the daemon tracks each open permission prompt with a tool-input hash and answers only if the record is <30 min old, the session is still `needs_input`, and the tmux dialog is visibly showing a command matching the request. Approvals are single-shot (key `1`, never "don't ask again"); deny sends Escape. Every remote decision is logged (`daemon.log`) and echoed back verbatim. Hermes' instructions restrict it to couriering the user's explicit approve/deny.

Remaining hardening for the SaaS version: signed alerts (HMAC on the webhook payload → approve must return the signature), per-command audit table in SQLite instead of the log file, and rate limiting.

## 7. Known debt / follow-ups

- **Rotate the credentials embedded in project allowlists** (DSW, swnms, job-portal, voter-management-saas, khatakhat `settings.local.json` contain live DB URLs/passwords/API keys). Move to env vars; the allowlists should reference commands, not secrets. (User-action item, flagged during the initial audit.)
- swnms `settings.local.json` has an invalid `"*"` allow rule Claude Code ignores with a startup warning — clean it up.
- Dashboard: surface queue contents + costs charts (backend endpoints exist: `/api/queue`, `/api/costs`, `/api/health`).
- `fleet up` window ordering: windows are created in registry order; deterministic numbering for muscle memory is best-effort after add/remove — consider `fleet up --renumber`.
- Transcript parser and usage parser share JSONL walking — unify if a third consumer appears.
