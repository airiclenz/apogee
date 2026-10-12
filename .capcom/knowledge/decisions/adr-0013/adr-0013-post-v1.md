---
title: "ADR 0013 post-v1 realisation (apogee-code track): the child sees a LIVE mode tightening, tighten-only"
---

D2/§2 say the child inherits the parent's Mode "verbatim (never loosened)." That was written
before the mode became **live**: the apogee-code track made Shift+Tab cycle the autonomy mode
mid-session (`SetMode`, ADR 0011's
realisation), and a sub-agent runs its whole Exchange — many Turns on a small local model — inside
one parent tool dispatch. Freezing the parent's mode at spawn (`childCfg.Mode = a.Mode()`) is
correct for **loosening** (a parent that later loosens must not loosen a running child) but leaves
a **tighten**-direction hole: a mid-delegation Shift+Tab from Auto down to Plan flips the footer
and the TUI promises it "takes effect on the next tool call," yet the child kept auto-approving
writes on its frozen spawn mode until its Exchange ended — the child running Auto while the parent
is now Plan, the exact tighten-direction failure ADR 0005 forbids.

The realisation refines — it does not change — the Decision: privileges are still **≤ parent**,
and loosening mid-flight is still impossible. It closes the tighten gap by making "≤ parent"
track the parent's *live* mode, not just its spawn-time snapshot:

- **`newChildAgent` injects a tighten-only live view.**
  *(Amended 2026-08-02 — see the Amendment section below: the captured accessor is the parent's
  `effectiveMode`, not its own `Mode`. Capturing `Mode` composed only one level and left a
  depth-2 grandchild on a stale mode.)* The child captures the parent's
  `modeMu`-guarded mode accessor as a closure (`Agent.liveMode`), **not** the shared mode field
  or mutex pointer — the child reads the parent's live mode race-free but has no seam to mutate it
  (the same "read-only view, no re-derivation" shape §3 uses for the shared dangerous floor). A
  top-level agent has a nil view and behaves exactly as before.
- **The disposition takes the tighter of the two modes.** `Agent.effectiveMode` returns
  `TighterMode(parentLive, spawnMode)` — a new ladder-index helper in `internal/domain/config.go`
  where Plan < Ask-Before < Allow-Edits < Auto — so a parent tightening **below** the child's
  spawn mode gates/refuses the child's next call, while a parent loosening **above** it is ignored
  (the child never rises above its spawn privilege). The spawn mode remains the ceiling; the live
  parent mode can only lower the effective floor.

**Acceptance:** a child spawned in Auto refuses its next write once the parent tightens to Plan
mid-run; a child spawned in Plan keeps refusing even after the parent cycles up to Auto (loosening
stays impossible); the parent's `modeMu` covers the child's cross-agent read under `-race`
(`internal/agent/setmode_test.go`, `internal/domain/config_test.go`).
