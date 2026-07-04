# Fleet × hermes-agent — monitor and drive your agents from your phone

[hermes-agent](https://hermes-agent.nousresearch.com/) is an open-source (MIT) multi-channel AI agent from Nous Research that connects Telegram, WhatsApp, Discord, Signal, and email, and can execute commands on your machine. Fleet doesn't ship any messaging code of its own — hermes is the phone-side operator, driving fleet through its CLI and receiving fleet's alerts through the webhook outbox.

## What you get

- **From any chat app:** "what are my agents doing?" → hermes runs `fleet status` and replies with the table; "queue on dsw: fix the signup bug" → `fleet queue add dsw "fix the signup bug"`; "what happened today?" → `fleet digest`.
- **Pushed to you:** the moment an agent needs a permission approval (or finishes a long task), fleet POSTs an alert to the webhook you configure; hermes forwards it to your chat.
- **Approvals stay at the terminal** (v1 policy): a misclick on a phone must not be able to approve a destructive command. Remote approval may come later behind an explicit config flag.

## 1. Install hermes-agent (on the same Mac that runs fleet)

```sh
curl -fsSL https://hermes-agent.nousresearch.com/install.sh | bash
```

Connect at least one channel (Telegram is the quickest) following its onboarding, and restrict the allowed senders to yourself.

## 2. Teach hermes the fleet CLI

Add this to hermes' system prompt / instructions for your machine:

```
You can monitor and control the user's Claude Code agent fleet with the `fleet` CLI
(/Users/master/Developer/fleet/bin/fleet):

  fleet status                       - table of every project + agent state
  fleet list                         - registered projects
  fleet dispatch <project> "<text>"  - send a prompt to that project's running agent
                                       (refuses if the agent is waiting on a permission)
  fleet queue add <project> "<text>" - queue work; runs when the agent goes idle
  fleet queue list / cancel <id>     - inspect or cancel queued work
  fleet playbook list / run <name> <projects...>
  fleet broadcast "<text>" --projects a,b | --all
  fleet digest [--yesterday]         - per-project daily summary

Rules: never pass --force to dispatch. Never approve permissions on the user's
behalf. When the user asks "status?", run `fleet status` and summarize; include
any ⚠ NEEDS YOU rows first.
```

## 3. Wire fleet's alerts into hermes

Fleet POSTs JSON alerts to every URL in `~/.fleet/webhooks.txt` (one per line):

```json
{"kind": "permission_needed", "project": "job-portal", "tool": "Bash",
 "summary": "pm2 restart ecosystem.config.js", "ts": 1783155488}
```

Point it at whatever inbound webhook your hermes setup (or ntfy.sh, or a
Telegram bot bridge) exposes:

```sh
echo "https://your-hermes-or-ntfy-endpoint/fleet" >> ~/.fleet/webhooks.txt
```

`kind` is `permission_needed` or `turn_done`. Alerts are debounced upstream
(20s per project+kind) so bursts don't spam your chat.

## 4. (Optional) remote dashboard over Tailscale

The daemon binds 127.0.0.1 by default. To view the dashboard from your phone:

```sh
openssl rand -hex 24 > ~/.fleet/token
FLEET_BIND=0.0.0.0 fleet daemon         # or your tailscale IP
```

Non-loopback requests must send `Authorization: Bearer <token>` (or
`?token=`). Without a token file, remote access is refused outright. Only ever
expose the port inside a VPN like Tailscale — never to the public internet.
