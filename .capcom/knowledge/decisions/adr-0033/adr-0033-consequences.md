---
title: ADR 0033 consequences
---

- **A three-layer split.** `internal/run` performs one Firing; `internal/schedule` owns every
  when-and-how decision (cycles, the skip policy, the quiescence gate, lifecycle) behind injected
  seams and a clock; the TUI owns nothing but input and display — commands, pickers, status rows
  and notices (ADR 0027 for the command
  namespace, ADR 0011 for the
  seam shape).
- **`apogee headless` shrinks to a thin CLI over `internal/run`.** When it ships, its plan is
  argument parsing and exit codes, not a second runner.
- **A Firing's record resumes without scrollback.** v1's runner records no transcript blob, so
  resuming a Firing from `/sessions` takes ADR 0022's documented degrade path ("resumed, no
  scrollback recorded") — an engine-only resume, correct and already handled, not a defect.
  *(Amended 2026-08-31: no longer true. Now that the transcript codec is Driver-neutral
  (`internal/session/transcript.go`), the runner folds its own scrollback from the Event stream
  and fills `Record.Transcript` — `internal/run/transcript.go` — so a Firing's record REPLAYS.
  The fold is narrower than the TUI's by design: stream facts only, no presenter verdicts. The
  degrade path stands for records written before this and for a blob that could not be encoded.)*
- **A Firing runs without MCP tools.** MCP connections are live host state re-established per
  session (ADR 0022, ADR 0008 —
  external effects are non-forkable); handing the TUI's live connections to a concurrent second
  agent is exactly the fork those records forbid.
- **The gate releases on engine idle, not on a Turn boundary.** An Exchange spans Turns
  (ADR 0025), so releasing a
  deferred Firing at `StatusTurnComplete` can land it mid-Exchange, contending with the very task
  the deferral exists to protect. Policy lives in the scheduler; the busy/idle state stays the
  TUI's to publish.
- **The fresh-per-firing answer feeds back into ADR 0031**, whose open-questions list now points
  here for the scheduled-trigger half.
- **Schedules die with the TUI.** Nothing persists to config, and that is the honest v1 promise —
  "while apogee is open, re-run this every N minutes". Durable schedules that survive quit are
  the future daemon's value-add over this same library, so building it here closes no door.
- **`CONTEXT.md` gains `Schedule` and `Firing`** as canonical terms, cross-linked to the
  **Driver** and **Embeddable agent** entries.
