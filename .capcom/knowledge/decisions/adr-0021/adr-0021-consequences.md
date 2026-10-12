---
title: ADR 0021 consequences
---

- **`ConfidenceMedium` stops being a reserved constant** and becomes a tier the system can
  actually be in. Both consumers gain a middle rung without changing shape: the Library's
  confidence-gated injection and the Validated-set match, keyed identically.
- **`apogee probe model` is a privileged-feeling command in a tool with no other privileged
  commands.** It is the only user-facing act that *enables automatism*. It must therefore read
  like one — explicit output, named record path, `--no-save`, and a printed undo.
- **A new persisted surface joins `~/.apogee/`** (config, library, sessions, validated) — small,
  versioned, owner-private, individually deletable, soft on every defect.
- **`TODO.md:370`'s probe residue closes**, and the confinement-degradation residue that pointed
  at it is updated (the edit itself belongs to the Phase-5 docs roll-up item, not here).
- **`CONTEXT.md` gains three terms** — *Probe*, *capability tier*, *behavioral fingerprint* —
  worded to match this ADR.
- **Adaptive prompt complexity is a named, dated `ISSUES.md` follow-on** carrying the tier's
  intended use, so mission.md item 2 is not lost — merely not assumed.
- **Bench arms are unaffected by construction.** A Medium record is produced only by a human
  running a command; the resolver's ladder is otherwise unchanged, and ADR 0016's
  wire-time-not-engine placement means a Bypass control arm still enables nothing.
- **`probe host` is the first CLI surface for the confinement matrix**, which makes ADR 0012's
  degradation story diagnosable off-session for the first time — including on the Windows hosts
  Phase 5's remaining items are about, where "which backend answered?" is the first question
  anyone will ask.
