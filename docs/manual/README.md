# The apogee manual

The reference documentation for [apogee](../../README.md), the terminal coding
agent for local LLMs. The README is the front door — what apogee is, how to
install it, what it can do. These pages are the full detail:

| Page | Covers |
|---|---|
| [Commands](commands.md) | Every in-chat command, skills, `@file` references, the keys, `/undo` and `/redo`, and the `/settings` screen |
| [Sessions](sessions.md) | How conversations are saved, resumed, forked (`/fork`), browsed, renamed |
| [Configuration](configuration.md) | `~/.apogee/config.yaml` end to end: servers and their wire (OpenAI-compatible or `wire: anthropic`), API keys, model profiles, tools, the Floor guards, the system prompt, llama-launcher, document presentation, Auto mode's confinement, url-safety, web search, and project skills |
| [Reactions](reactions.md) | Commands and webhooks apogee runs on what a session did (`run:`), commands that advise the model on a tool result (`advise:`) and commands that gate a tool call (`gate:`): the sixteen Moments, the payloads, the exec posture, the webhook contract, migrating from `hooks:` |
| [Diagnosing a host — `apogee probe`](probe.md) | What this machine can enforce, what the model can do, what the terminal really does, what the config file says, what apogee puts in front of the model at Turn 1 (`probe context`) |
| [Running one prompt — `apogee headless`](headless.md) | Single unattended runs for scripts and pipelines, the `--format json` Event lines, and `apogee undo` to put one back |
| [Standing schedules — `apogee daemon`](daemon.md) | Prompts on a clock that outlive the session |
| [Building from source](building.md) | Prerequisites, `Makefile` targets, cross-compilation |

Working on the codebase itself? [`AGENTS.md`](../../AGENTS.md) is the map of
where knowledge lives; `CONTEXT.md` defines the domain language; `docs/adr/`
holds the settled decisions.
