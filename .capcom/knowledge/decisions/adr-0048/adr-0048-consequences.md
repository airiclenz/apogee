---
title: ADR 0048 consequences
---

- **The config file gains two documented keys**, taught by the seeded template in house voice:
  `remember-model:` beside the other top-level toggles, `launch-profile:` in the per-server key
  documentation next to `llama-launcher:`, stating plainly that plain servers persist into `model:`
  instead and that the launcher's own YAML is never written.
- **Recording is visible.** A written choice states itself on a transcript line of its own — the user
  can see that their file just changed — and a failed write says so where the act happened, without
  unwinding it.
- **Being remembered is opt-in and stays reversible by hand.** With the toggle off nothing is written
  and start-up touches no launcher; with it on, forgetting a recorded choice is deleting a line, since
  the writer has no delete form.
- **The toggle is the one applied setting that pushes at no engine seam and rides no rebind** —
  everything it gates is still in the future — so its live apply is a store into the session's settings
  holder and nothing else. Every reader of it (both record seams and the boot check) reads through that
  holder, because a value captured at launch would have made a `/settings` flip govern nothing until
  the next start.
- **Realisations.** The toggle and the pointer plus their refusals (`internal/config/config.go`,
  `internal/config/options.go`, `internal/config/registry.go`, `internal/config/defaults/config.yaml`);
  the entry-scoped writer (`internal/config/configwrite.go`, one concern per package,
  ADR 0043); the three seams
  `RecordModelChoice`, `LauncherHost.RecordProfile` and `LauncherHost.Restore` with the `ProfileRestore` answer
  (`internal/tui/tui.go`), called at the explicit-pick site (`internal/tui/picker.go`), at the two
  load-commit folds and in the boot fold (`internal/tui/actuation.go`); their implementations and the
  restore's whole decision ladder in the composition root (`cmd/apogee/wire_verbs.go`,
  `cmd/apogee/launcher.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/wire_options.go`), with the live
  toggle held in `cmd/apogee/wire_settings.go`.
