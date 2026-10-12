---
title: ADR 0027 consequences
---

- **No public Go surface changes.** Everything lives in `internal/tui`;
  `domain.UserInput.SkillIDs`, `Config.Skills` and the agent-side resolution are reused unchanged.
  The `Engine` interface, `apogee.Config` and every exported name are untouched — this is a TUI
  behaviour release, not an API one.
- **A staged interjection now carries its skills.** The silent drop in `stageInterjection` is
  fixed, `joinedInterjections` unions `SkillIDs` first-seen exactly as it unions file refs, and the
  agent already resolves them at delivery (ADR 0025's
  resolve-at-delivery rule). A skill invoked in a remark reaches the model.
- **`runCommand` may be reached while a worker runs**, for reporting verbs only. ADR 0011's
  engine-call taxonomy is unchanged: the three verbs that can arrive mid-run are boundary-FREE by
  inspection (two synchronous notes and one `SetMode`-class read), and everything else is refused
  before it gets there.
- **The value-copied `Model` gains only plain values** — a slice header, ints, bools. ADR 0011's
  no-copy invariant and `TestModelNoBuilderByValue` are untouched.
- **The caret walk had to become correct on wrapped lines.** Splicing mid-draft made the editor's
  logical row/column seat load-bearing, which exposed a pre-existing defect: bubbles' `wrap`
  appends a phantom trailing sub-line that `CursorDown` can never enter, so the old walk could
  stall — and `reseatInput`'s guard-free version could spin. `promptEditor.seatCaret` (a
  `CursorEnd`-then-`CursorDown` step, Height-aware) is now the one walk both express, and a
  deadline-guarded test fails instead of wedging `go test` if it ever spins again. (Since
  2026-09-23 `lineEditor.seatCaret` walks no more: the walk was O(lines²) per seat, so it now
  rebuilds the value around its target, pinned frame for frame against this walk — plan
  `2026-09-23 - 01`, item 2.) The mouse
  path's `reseatCaret` (a click's VISUAL row) is unchanged and still lands imprecisely below a
  phantom-wrapped line — pre-existing, deliberately out of scope, and parked in `ISSUES.md`.
- **CONTEXT.md's *Skill* entry gains the token grammar** and is cross-referenced against *File
  reference* in both directions; the *Interjection* entry records the per-command policy in place
  of the blanket "commands run at idle".
- **`layout.md` gains a prompt-box section** (the merged dropdown, the idle-only tag, the inline
  accents) and the chip strip leaves the spec with the code.
- **Two palette entries are now load-bearing for meaning, not decoration**: `colSkill` (violet,
  inherited from the retired chip) and the new `colFileRef` (blue). A future theme change must keep
  them distinguishable from plain prompt text, or the live validation stops validating.
