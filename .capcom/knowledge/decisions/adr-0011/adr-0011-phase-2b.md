---
title: ADR 0011 phase-2 realisation (p2.2 — the skeleton plugs in)
---

- **The model folds exactly the five C1–C4 Msgs.** `internal/tui/model.go` adds the `Model`
  (a value type with value-receiver `Init`/`Update`/`View`), its four-state machine
  `{idle, running, awaitingApproval, errored}`, the Bubbles `textarea` input + `viewport`
  transcript + `spinner`, and the status line. `Update` folds `eventMsg` /
  `approvalReqMsg` / `exchangeDoneMsg` / `cancelledMsg` / `errMsg` plus keypresses, the window
  size, and the spinner tick — nothing else, and no agent logic (C5). A keypress→submit
  launches the worker via `startExchange` and stores its `CancelFunc` (C4); **submit while
  running is a no-op** (the single-worker invariant). `transcript.go` is the C6 entry model
  (typed entries + an in-progress assistant token buffer) with an exhaustive switch over all
  eight Events — P2.2 folds the streaming-text and error paths; the tool/approval/mechanism/
  reset bodies are marked P2.3 stubs on the now-stable structure.

  > **Note (2026-07-21).** "All eight Events" was true at P2.2. The Event set has since grown
  > additively to **eleven** (`UsageEvent`, `AuditEvent`, `ReasoningEvent`), and the
  > transcript switch still folds the same eight — the three additions are not transcript
  > entries (the activity line and the status-line stats read them) and fall to its default
  > case. The realisation above is left as written; only the count is dated.
- **`Run` does the one wiring step P2.1 left.** It builds
  `tea.NewProgram(newModel(ctx, eng, opts), tea.WithContext(ctx))`, then calls
  `br.Bind(program)` **before** `program.Run()` — so the late-bound sink/approver reach the
  live program the instant the first worker emits, and the program context cancels an
  in-flight Exchange on shutdown (C4). `*tea.Program` satisfies the `programSender` seam
  (`Send(tea.Msg)`), exactly as P2.1 verified.
- **Charm v2-vs-v1: kept v2.** textarea/viewport/spinner all exist in `bubbles/v2 v2.1.0`, so
  no widget lagged and the v1 fallback was not triggered. **lipgloss and bubbles also live on
  the `charm.land/…` path** (`charm.land/lipgloss/v2 v2.0.4`, `charm.land/bubbles/v2 v2.1.0`)
  — both moved there at these versions, exactly as Bubble Tea did. The v2 `View()` returns a
  `tea.View` struct (alt-screen is a view field, not a program option) and key presses arrive
  as `tea.KeyPressMsg` — the seam message types stay plain values, unaffected.
