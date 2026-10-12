---
title: ADR 0056 consequences
---

- The 2026-08-22 incident shape is structurally closed and regression-proven: the
  `chained_script_clobber_denied` probe runs in the escape battery under the real backends.
- `docs/design/confinement-execution-contract.md` is amended (2026-08-22): §2.2 states the
  EACCES/EPERM reality and the kill-on-denial watch, §6.2 gains the chained-script row, §7
  records the box's writable set as workspace ∪ per-project `WritablePaths` ∪ the session
  scratch dir (plus the backend-level `/dev/null` exemption). ADR 0012's "EPERM" wording is
  reconciled in place.

  > **Note (2026-10-02, verified against code):** of that writable set, only the scratch dir has a
  > production writer. `Config.ConfinementBox` (`internal/domain/confinement.go`) appends it to
  > `box.WritablePaths`. The per-project half, `Config.ConfineWritablePaths`
  > (`internal/domain/config.go`), is set by no production code, and `internal/config` declares no
  > key for it, so today the box's writable set is the workspace plus the session scratch dir.
- The terminal result surface grows three model-facing signals: the stop label, the
  heuristic denial label, and the mutation warning. All are additive lines on existing
  results; no wire or Driver obligation changes (ADR 0031 holds — the scratch path rides the
  existing Config/Agent seam, and the engine stays wire-silent).
- Windows keeps neither the fail-fast preamble nor the denial watch — its terminal path has
  no `set -e` analogue and its denials print "Access is denied.", deliberately unmatched.
  The asymmetry is recorded here and in the `Terminal` doc comment; closing it is future
  work, not an oversight.
- `~/.apogee` gains a `scratch/` root with a 14-day GC; custom prompts opt into `{{scratch}}`
  like the other placeholders.
