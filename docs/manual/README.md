# The apogee manual

The reference documentation for [apogee](../../README.md), the terminal coding
agent for local LLMs. The README is the front door — what apogee is, how to
install it, what it can do. These pages are the full detail:

| Page | Covers |
|---|---|
| [Commands](commands.md) | Every in-chat command, skills, `@file` references, the keys, `/undo`, and the `/settings` screen |
| [Sessions](sessions.md) | How conversations are saved, resumed, browsed, renamed |
| [Configuration](configuration.md) | `~/.apogee/config.yaml` end to end: servers, API keys, model profiles, tools, the Floor guards, the system prompt, llama-launcher, document presentation, Auto mode's confinement, url-safety, web search, and project skills |
| [Diagnosing a host — `apogee probe`](probe.md) | What this machine can enforce, what the model can do, what the terminal really does |
| [Running one prompt — `apogee headless`](headless.md) | Single unattended runs for scripts and pipelines |
| [Standing schedules — `apogee daemon`](daemon.md) | Prompts on a clock that outlive the session |
| [Building from source](building.md) | Prerequisites, `Makefile` targets, cross-compilation |

Working on the codebase itself? [`AGENTS.md`](../../AGENTS.md) is the map of
where knowledge lives; `CONTEXT.md` defines the domain language; `docs/adr/`
holds the settled decisions.
