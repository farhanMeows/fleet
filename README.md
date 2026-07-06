# fleet

**Mission control for Claude Code agents.** One binary that watches every agent across all your projects, shows a live dashboard, alerts you when an agent needs a permission approval, and lets you dispatch work to any project's agent without switching windows.

```
┌─ FLEET ────────────────────────────────────────────────┐
│  PROJECT         STATE          NOW                    │
│  storefront      ● working      Bash: npm test         │
│  api-server      ⚠ needs you    Bash: pm2 restart …    │
│  data-pipeline   ✓ done         —                      │
│  mobile-app      ● working      Edit: api/orders.ts    │
│  docs-site       ○ idle         —                      │
└────────────────────────────────────────────────────────┘
```

## How it works

Claude Code's user-level hooks call `fleet hook <event>` on every session lifecycle event (session start/end, tool use, permission request, turn finished). Fleet normalizes these into a versioned event stream, stores them in SQLite (`~/.fleet/`), and serves a live API + dashboard from a local daemon. A tmux driver gives every project its own agent window with a live state icon.

The hook path is engineered to never affect your agents: it exits 0 in under 100 ms, writes nothing to stdout, and spools events to disk when the daemon isn't running (drained on next start — nothing is lost).

## Install (macOS)

Apple silicon & Intel — grabs the latest release binary and puts it on your PATH:

```sh
curl -fsSL https://www.fleetdeck.in/install.sh | sh
```

Distribution is self-hosted: `scripts/release.sh vX.Y.Z` builds both darwin binaries and stages them (plus the installer) into `website/`; deploy that directory to your domain and swap `fleetdeck.in` for it (one value in `scripts/install.sh`, plus the URLs shown in `website/index.html` and above).

## Quick start

```sh
make build          # or use the installer above (from source: Go 1.26+, no CGO)
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
| `fleet add [path] [--name x]` | Register a project (any number of them) |
| `fleet remove <name>` / `fleet list` | Manage the project registry |
| `fleet up` | Launch/attach the tmux mission-control session (window 0 = dashboard, one window per project running `claude`, live state icons: ● working ⚠ needs you ✓ idle ○ no session) |
| `fleet status [--watch]` | Status table of every project + session (watch = interactive dashboard: `j/k` move, `Enter` jump to the project's tmux window, `d` dispatch a prompt, `r` refresh, `q` quit) |
| `fleet daemon` | Run the API + dashboard daemon in the foreground (auto-started by other commands) |
| `fleet hook <event>` | (internal) hook entrypoint called by Claude Code |

| `fleet dispatch <project> "<prompt>" [--force]` | Send a prompt to a project's *running* agent (refuses while it awaits a permission decision unless `--force`) |
| `fleet queue add <project> "<prompt>"` | Enqueue work; auto-dispatched when the agent goes idle (strictly one at a time) |
| `fleet queue list [project]` / `fleet queue cancel <id>` | Inspect / cancel queued work |
| `fleet playbook save <name> "<prompt>"` | Save a reusable prompt (`{{project}}` substituted at run time) |
| `fleet playbook run <name> <project>...` | Queue a playbook on one or more projects |
| `fleet broadcast "<prompt>" --projects a,b` / `--all` | Queue one prompt across many projects |

| `fleet reply <project> [-n N]` | Print the agent's most recent answer (read a dispatched task's result) |
| `fleet approve <project> [--deny]` | Answer a pending permission prompt remotely — **opt-in** (`touch ~/.fleet/remote-approve`) and layer-verified: pending record fresh + hash-matched, agent still waiting, dialog visibly matching; always single-shot, deny = Escape |
| `fleet digest [--yesterday]` | Daily standup: sessions/turns/tools/tokens per project |
| `fleet ports <project> 3000,3001` | Dev-server ports to health-check for a project |
| `fleet guard add/list/remove/check` | Prod-data guardrail patterns (blocks destructive commands referencing prod hosts) |

Inside the tmux session: `prefix+<n>` jumps to a project window, `prefix+g` to the dashboard, `prefix+j` opens the window picker.

## Web dashboard

`http://localhost:7433` — terminal-themed, keyboard-first (`j/k` navigate, `Enter` for live transcript, `:` command bar with `dispatch <project>: <prompt>`). Live via SSE; shows the fleet table, a NEEDS YOU inbox, and the event feed. Build with `make web && make build` to refresh the embedded assets.

## Phone access

See [docs/HERMES.md](docs/HERMES.md) — drive fleet from Telegram/WhatsApp via hermes-agent, with permission alerts pushed through the webhook outbox (`~/.fleet/webhooks.txt`; `api.telegram.org` URLs get human-readable messages natively). With remote approve armed, a permission alert can be answered from chat: reply "approve <project>" and hermes runs `fleet approve` — the human decision stays with you; hermes is only the courier. Product direction lives in [docs/ROADMAP.md](docs/ROADMAP.md).

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
