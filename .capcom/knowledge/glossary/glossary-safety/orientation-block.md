---
title: "Orientation block: the standing system content stating the host facts a model needs"
---

**Orientation block**:
The engine-composed part of the **standing system content** that states the host facts a model
needs to get oriented — the **workspace** path, its **Scratch dir**, the `/tmp` caveat, the
read-only library roots with the tools that reach them, a **Delegation bounds** line whenever
`sub_agent` is on the roster — `up to W run at once; a reply may fan out at most C — calls past
that are refused and must be delegated again; a reply's whole group returns together; each
delegate is capped at S Turns (a max_steps above that is clamped)`, the width the latched
per-seat number the **Fan-out ceiling** multiplies, the ceiling and cap clauses omitted when
their key is `0` — and — only under `sub-agents-choice:
model` — a **Delegations** line describing both **Delegation seats** symmetrically (each seat's
entry name, its `description:`, its `model:` pin and the bound session model) so the model can
choose a `run_on`. It is **harness text, not persona text**:
the engine writes it, so no edit to the user's `system-prompt-text` — and no install whose config
was seeded before a fact existed — can lose it. It **rides along**: appended only when a standing
system message exists anyway (a rendered template and/or **Context files** blocks), never
on its own, so the no-prompt-**and**-no-context-files posture — since ADR 0064 reached only through
`use-default-prompt: false` or an explicitly empty configured template, no longer by deleting the
key — stays byte-identical on the wire and the **Bypass** floor is untouched. Wire position is
directly after the prompt —
prompt → orientation → delegate block (delegations only) → task list (once the model has written
one) → context files → Reaction directives → tool block — so no workspace text precedes it and a
repo file cannot open with a forged copy the
real one then reads as a correction of; every fact it states moves only on a session-level door, so it is prefix-KV-cache safe between them —
the Delegations line carries no availability state and moves only on the human doors (`/server`,
`/model`, `/sub-agents-server`), the Delegation bounds' width is latched per seat and moves only on
those doors and on a cap's first statement — never on a target-down beat (ADR 0069 decision 6,
superseded in part 2026-09-20) — the way the **Scratch dir** moves at a session boundary (the
**Mode** gated the scratch line for one day, 2026-09-14, until ADR 0012's second loosen made Plan
write there too). The mode is an input again since 2026-09-15, for one bullet only: in **Plan** a
**Mode** line names the families the menu withholds (terminal, run_tests, python_exec, web_fetch,
web_search, http_request, MCP tools) and asks the model to report what it would run — it moves on
the human's Shift+Tab, the same re-encode `{{mode}}` already pays. The block's last bullet always
names the `## Workspace context: <name>` headers that follow as project text. A
fact the session does not have is omitted rather than rendered empty. See
ADR 0023 §6
and ADR 0069.
_Avoid_: "system prompt" (that is the user's configured template; this is the engine's own text),
"preamble" (the terminal tool's fail-fast preamble is a different thing).
