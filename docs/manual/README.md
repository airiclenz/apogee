# The apogee manual — reference for the terminal AI coding agent

The full reference for [apogee](../../README.md), the open-source AI coding agent for local
LLMs that runs in your terminal. The README is the front door: what apogee is, how to
install it, what it can do. These pages hold the detail.

| Page | Covers |
|---|---|
| [Commands and keys](commands.md) | Every in-chat command and key, skills, `@file` references, `/undo` and `/redo`, the `/settings` screen, `/git`, `/mcp`, `/panel` |
| [Sessions](sessions.md) | How conversations are saved, resumed, forked (`/fork`), browsed, renamed, and watched live |
| [Configuration reference](configuration.md) | `~/.apogee/config.yaml` and a project's `.apogee/config.yaml` end to end: servers and their wire (OpenAI-compatible or `wire: anthropic`), fallback servers, API keys, model profiles, tools, the Floor guards, the system prompt, the UI and side panel, llama-launcher, Auto mode's confinement, url-safety, web search, and project skills |
| [Workflows and recipes](workflows.md) | One brief over many items, each done by a fresh helper: the `fan_out` tool, recipes, `/bg` and background workflows, a background `sub_agent`, the `workflow` tool, `/workflows` |
| [Reactions](reactions.md) | Commands, webhooks and MCP tools run on what a session did (`run:`), advice to the model (`advise:`) and gates on tool calls (`gate:`): the nineteen Moments (the five seams and the fourteen notices), the payloads, the webhook contract, migrating from `hooks:` |
| [`apogee probe`](probe.md) | What this machine can enforce, what the model can do, what the terminal really does, what the config resolves to, and what apogee sends the model at Turn 1 |
| [`apogee headless`](headless.md) | One unattended prompt for scripts and CI: exit codes, turn and time budgets, the `--format json` Event lines, and `apogee undo` |
| [`apogee daemon`](daemon.md) | Prompts on a schedule that outlive the session, installed as a system service |
| [Updating apogee](updating.md) | The update notice, the upgrade command for each install channel, and `apogee update` for a release-archive install |
| [Building from source](building.md) | Prerequisites, `Makefile` targets, tests, cross-compilation, and the release runbook |

Working on the codebase itself? [`AGENTS.md`](../../AGENTS.md) is the map of
where knowledge lives; the glossary (`capcom know show glossary`) defines the domain language; the decision
records (`capcom know show decisions`) hold the settled decisions.
