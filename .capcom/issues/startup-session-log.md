---
title: Collapsed session-log block for startup notices (side panel when open)
status: done
type: feature
priority: 3
plan: 2026-11-12 - 00 - go127-gate-and-session-log-plan.md
created: 2026-11-12T08:19:33Z
closed: 2026-11-12T16:45:38Z
reason: "Resolved by plan '2026-11-12 - 00 - go127-gate-and-session-log-plan' (archived a1d86ac0), items 4-6: routine start-up and first-beat notes gather into a collapsed '✦ Session log (N notes)' block until the session's first Exchange launch; notices that need the human keep their own line; layout spec plus driven TUI and e2e tests cover collapsed and expanded states. The side-panel placement is follow-up session-log-side-panel."
---

Gather the start-up notices (context files, confinement posture and residuals, model facts such as mandatory reasoning, rebind notes of the first beat) into one **session log** block in the transcript, collapsed by default like tool blocks and sub-agent blocks. Its collapsed header is one line (e.g. `session log · 4 notes`); expanding shows each note. When the side panel is available, the log may live there instead of in the transcript.

Anomalies stay loud: a notice that needs the human (Auto unconfined, the degraded-Auto notice, an unreadable context file, a refused rebind) still prints as its own line, or opens the block expanded.

Open design questions: does the block stay in the transcript once the panel shows it; does it collect later session facts (model/window changes) or only start-up ones; how does the stderr/transcript verbatim rule (`announce`, `cmd/apogee/wire_boot.go`) carry over; layout spec in `layout.md` / `docs/layout/tool-layout.md`.

## Why

The start-up notes crowd the first screen with facts that are rarely news (owner request, 2026-11-12). Shorter wording (startup-notice-wording) helps; a collapsed block removes them from view until asked for.

## Acceptance

- A clean start shows the box and one collapsed session-log line; expanding it shows every start-up note.
- An anomaly notice is still visible without expanding.
- Layout spec and driven TUI test cover collapsed and expanded states.
