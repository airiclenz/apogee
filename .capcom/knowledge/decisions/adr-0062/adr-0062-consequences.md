---
title: ADR 0062 consequences
---

- **"Manual" now needs a reason on the record.** `docs/design/test-drivers.md` carries the
  *Which driver observes which claim* table; a test step may be manual only where that table's
  "Not observable by any driver" column covers its claim class. The `test-checklist` skill gates on
  it, so the default for new work flips from "write a numbered human step" to "write a driver test".
- **`teatest`, `vhs` and `tmux` are settled and not to be re-proposed.** `teatest/v2` is a
  pre-release pinned to bubbletea `v2.0.0-rc.1` while this repo is on `v2.0.8` — adopting it means
  downgrading the TUI or maintaining a fork. `vhs` is an external binary that renders a GIF, so it
  is a prerequisite (ADR 0042
  forbids those) and its output is pixels, which no Go test can assert cells against. `tmux` is an
  external prerequisite too, and it inserts a *second* terminal emulator between the program and the
  assertion, so `capture-pane` polling yields strictly less fidelity (styles, cursor, cell width)
  than reading our own emulator.
- **The kit is maintained like production code.** It lives under `internal/` (plus one `cmd/`), it is
  documented in `docs/design/test-drivers.md`, its own behaviour is tested, and it is covered by
  `make check`. A broken driver is a broken build, not a test-only annoyance to be skipped past.
- **The repo gains a second binary that users never install.** `cmd/stubllm` is a developer tool
  built from source; it is not in `make dist`, not in the Homebrew formula, and not in the manual's
  install path.
- **Gating stays boring.** The in-process and PTY e2e tests run in every plain `go test` — no build
  tags, no `-short` — with the whole e2e set budgeted at ~15 s under `-race` at ratification
  (amended 2026-10-29: measured 121.7 s for 36 tests on 2026-08-28, and the set has grown since;
  `docs/design/test-drivers.md` §Gates and budgets holds the current figures); PTY tests skip on
  Windows or where no pty is available; judge tests skip without the gate and join `make live-eval`.
- **Nothing here is a Mechanism.** The kit observes apogee; it never changes what a model sees.
  stubllm replaces the upstream rather than steering it, the drivers add no prompt text, and the
  judge reads artifacts after the fact. There is nothing for Bypass to switch off and nothing for a
  bench arm to measure, so the Bypass floor is untouched.
