---
title: "Reactions have no session-start notice for run: entries"
status: open
type: feature
priority: 2
plan: 2026-10-19 - 00 - session-started-reactions-plan.md
created: 2026-10-19T16:54:07Z
---

None of the eleven Reaction notices fires when a session starts, so a `run:` entry cannot do per-session setup or bookkeeping (log the session, sync a register, warm a cache) the way a Claude Code `SessionStart` hook can. The vocabulary ends at turn, exchange, file, approval, error and seam-closing notices.

Wanted: a `session-started` notice that `run:` (command or webhook) can subscribe to, with the usual payload and `APOGEE_REACTION_*` environment, fired once per session start on every Driver that runs reactions. Which starts count (fresh, resume, `/clear`, headless, daemon Firings, sub-agents) is a design call for the plan.

Why: it is the observe-class half of "hooks like capcom's" and the Moment the session-start `advise:` entry (`session-start-advise`) subscribes to.

## Acceptance
- `on: [session-started]` is accepted under `run:` and fires once per counted session start, with a payload and env documented in `docs/manual/reactions.md`.
- The notice count the public docs state follows (drift test green).
- Nothing a `run:` entry prints reaches the model.
