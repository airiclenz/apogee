---
title: Schedule
---

**Schedule**:
A standing instruction a **Driver** holds for its lifetime — a prompt, a cycle (how often it
re-runs) and an Agent mode — created in the TUI with `/schedule` and ended with
`/schedule-stop`. Everything that decides *when and how* it runs (cycle timing, the
skip-on-overlap policy, the wait for a quiescent host, lifecycle) lives in the scheduler
**library** (`internal/schedule`); the TUI is merely its first Driver surface, owning only
input and display, and `apogee daemon` composes the same library. A TUI-hosted Schedule **dies
with the TUI** — "while apogee is open, re-run this every N minutes" is the whole promise;
nothing persists to config. A Schedule's mode is **Plan or Auto only** and is chosen explicitly
at creation, independent of the host session's mode (Auto still gated by the same eligibility
ladder that gates launching in Auto), so no **Firing** can ever park on an Approval. See
ADR 0033.
_Avoid_: "cron job" / "job" (nothing is queued and nothing survives quit), "recurring task",
"timer" (the timer is one part; the Schedule is the standing instruction).
