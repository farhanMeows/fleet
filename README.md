# fleet

**Mission control for Claude Code agents.** One binary that watches every agent across all your projects, shows a live dashboard, alerts you when an agent needs a permission approval, and lets you dispatch work to any project's agent without switching windows.

```
┌─ FLEET ────────────────────────────────────────────────┐
│  PROJECT         STATE          NOW                    │
│  DSW             ● working      Bash: npm test         │
│  job-portal      ⚠ needs you    Bash: pm2 restart …    │
│  swnms           ✓ done         —                      │
│  khatakhat       ● working      Edit: api/orders.ts    │
│  voter-saas      ○ idle         —                      │
└────────────────────────────────────────────────────────┘
```

## How it works

Claude Code's user-level hooks call `fleet hook <event>` on every session lifecycle event (session start/end, tool use, permission request, turn finished). Fleet normalizes these into a versioned event stream, stores them in SQLite (`~/.fleet/`), and serves a live API + dashboard from a local daemon. A tmux driver gives every project its own agent window with a live state icon.

The hook path is engineered to never affect your agents: it exits 0 in under 100 ms, writes nothing to stdout, and spools events to disk when the daemon isn't running (drained on next start — nothing is lost).

## Quick start

```sh
make build          # builds bin/fleet (Go 1.26+, no CGO)
./bin/fleet install # wires hooks into ~/.claude/settings.json (backs it up first)
./bin/fleet daemon  # start the daemon (API on http://127.0.0.1:7433)
```

Then start any `claude` session and watch it appear:

```sh
curl -s localhost:7433/api/sessions | jq
```

## Commands

| Command | Purpose |
|---|---|
| `fleet install` | Install Claude Code hooks (idempotent, backs up settings) |
| `fleet daemon` | Run the API + dashboard daemon in the foreground |
| `fleet hook <event>` | (internal) hook entrypoint called by Claude Code |

Coming per the [PRD](docs/PRD.md) roadmap: `up`, `add/remove/list`, `status --watch`, `dispatch`, `queue`, `playbook`, `broadcast`, `digest`, `jump`, `guard`.

## API

- `GET /api/sessions` — active sessions (`?all=1` includes ended)
- `GET /api/events?limit=100&project=name` — event history
- `GET /api/projects` — registered projects
- `GET /api/stream` — SSE stream of live session updates
- `POST /api/hook` — event ingestion (used by `fleet hook`)

## Layout

```
cmd/fleet/          CLI entrypoint (cobra)
internal/event/     versioned event schema — the contract everything shares
internal/store/     SQLite persistence (modernc.org/sqlite, pure Go)
internal/hookcmd/   hook ingestion + settings.json installer
internal/server/    daemon: REST + SSE + spool drain
docs/PRD.md         product requirements + roadmap
```

## Configuration

- `FLEET_PORT` — daemon port (default `7433`)
- Data lives in `~/.fleet/` (`fleet.db`, `spool/`, `hook.log`)
