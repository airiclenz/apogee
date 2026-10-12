---
title: ADR 0038 consequences
---

- **Windows Terminal and VS Code now paint the same frames macOS and Linux always did.** Confirmed
  by the owner on a build of this tree: no corruption in streamed text, no ghost behind the activity
  spinner, scrolling correct.
- **The console-mode restore rides `Run`'s defer path.** A process killed outright (`SIGKILL`, a
  closed window, a crash inside the renderer) leaves the mode as apogee set it, exactly as it leaves
  the alternate screen claimed — inherent to the mechanism, and the same exposure every terminal
  program on the platform has.
- **`apogee probe terminal`'s capability section still compares against `noCaps` on Windows, and
  that is a known limitation of the plan's item-5 acceptance row rather than a defect in the fix.**
  The probe reads the *process* `TERM` (`cmd/apogee/probeterminal.go`), which decision 2
  deliberately never sets, so it reports MISMATCH for capabilities the painter now genuinely has.
  What the injection is actually pinned by is `internal/tui`'s environment-builder tests and
  `TestInjectedEnvironmentResolvesTheSameColorProfile`, both of which have been watched fail. A
  later change could give the probe the same naming rule, or an explicit "assume this `TERM`"
  argument; until then the row is not answerable as written and the plan records it as such.
- **Three Windows-only rules now live as `_windows.go` / `_other.go` twins** — `altscreen_*`,
  `environ_*`, `syncoutput_*` — and the six-target cross-build is the gate that keeps their
  non-Windows halves honest. `internal/tui/doc.go`'s file map names them all.
- **The regression is pinned by a headless pseudoconsole test** that was seen red against a
  pre-fix build and green after, asserting on the rendered buffer rather than the byte stream — a
  byte-stream assertion would re-encode the renderer's own model and pass while the screen is wrong.
  It skips cleanly where a pseudoconsole cannot be created, and off Windows.
- **ADR 0011 and ADR 0030 are untouched.** Nothing here changes the Model, the value-copy invariant,
  or the width authority; the whole record is about the console apogee hands the painter and the
  order in which it does so.
- This is a user-visible fix that arrives beside a new subcommand (`apogee probe terminal`) and a new
  key (`⌃l`), so it **warrants a bump** when the next version is cut. That call is the owner's, and
  no item of the implementing plan touches a version identifier.
