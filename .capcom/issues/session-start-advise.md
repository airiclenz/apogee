---
title: "advise: at session start cannot reach the model"
status: open
type: feature
priority: 2
deps: [session-started-notice]
plan: 2026-10-21 - 00 - user-session-advice-plan
created: 2026-10-19T16:54:07Z
---

A Claude Code `SessionStart` hook's stdout reaches the model's context (e.g. `capcom ready` prints the ready issues and the model starts the session knowing them). apogee has no equivalent: `advise:` reacts only at `post-tool-result` / `file-changed`, landing as a fenced trailer on a tool result, and nothing a `run:` entry prints reaches the model. `system-prompt-layers:` is static text, not command output.

Wanted: an `advise:` entry (command or webhook) on `session-started` whose output the model reads as fenced advice from the user, via ADR 0076 D6's second slot (`non-tool-advise-slot`), never the system message. It must not make the model perform worse than the bare loop: the slot is admitted by bench evidence, and Bypass behaviour follows ADR 0076.

## Acceptance
- `on: [session-started]` is accepted under `advise:`; its output reaches the model once per counted session start inside apogee's fence, in the D6 slot, on both wires.
- A failing, timing-out or empty entry adds nothing to the request and is reported the way other advise: failures are.
- Adopted repo reactions (ADR 0104) may use it; unadopted ones never fire.
- `docs/manual/reactions.md` documents it, including where the text lands and its cost.

## Log

- 2026-10-19 Airic Lenz: Plan 2026-10-19 - 00 - session-started-reactions-plan builds the slot and lane for engine-origin Go handlers only. Left to this issue's enable step: the user key's D8 workspace exec fence + redaction on argv/webhook handlers, session-start spans shown in the /advice pane (internal/tui advicepane foldAdvice), and an adopted repo advise entry starting at once when adopted before the first prompt.
- 2026-10-21 Airic Lenz: User/adopted advise: on session-started shipped by plan 2026-10-21 - 00 - user-session-advice-plan (archived at 51b3a10d), items 3-7, 13-14; left open: deferred item-6 gap (SetReactions after Close starts a run nothing ends) tracked as advise-run-after-close.
