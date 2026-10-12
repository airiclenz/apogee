---
title: ADR 0011 phase-2 realisation (p2.1)
---

- **Files.** `internal/tui/messages.go` (the five worker→Update Msgs + the `programSender`
  seam assertion), `bridge.go` (`Bridge` + the atomic late-bound `programRef`), `sink.go`
  (`teaSink`, C2), `approver.go` (`uiApprover`, C3), `worker.go` (`startExchange` /
  `driveExchange`, C1/C4). `cmd/apogee/wire.go` builds the `Bridge` and installs its
  `Sink`/`Approver` (retiring the P2.0 `nopSink` placeholder); the `launcher` seam carries
  the `Bridge` so `Run` can bind it.
- **Dependency / build note.** The plan pinned `github.com/charmbracelet/bubbletea/v2
  v2.0.7`, but Bubble Tea moved its module path to **`charm.land/bubbletea/v2`** exactly at
  v2.0.7 (the `github.com/...` path resolves only through v2.0.6). We took the new canonical
  path `charm.land/bubbletea/v2 v2.0.7` — the best-for-the-future choice (that is where the
  project now publishes). The v1 fallback, if a Bubbles v2 widget lags in P2.2, is
  `github.com/charmbracelet/bubbletea v1.3.10`. The v2-vs-v1 call is still made at the first
  widget need (P2.2), per the plan.
- **What is deliberately not here yet.** `Run` binds nothing and stays a no-op until P2.2
  builds the `Model` and the real `*tea.Program`; the worker is never launched in production
  before then, so the unbound delegates are harmless. Token coalescing (C2) is left as a
  documented hook, not built.
