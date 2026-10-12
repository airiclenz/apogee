---
title: ADR 0040 context
---

Apogee's palette was twenty-odd hex literals in package-level vars at the top of
`internal/tui/theme.go` — `colWhite`, `colFaint`, `colDiffAdd`, `colModeAuto`, `colSpinner1..4` —
read directly by `newTheme` and, in a handful of places, by call sites that had no style to reach
for (the prompt textarea's four background slots, the gauge fill's track, the popup's interior, the
mode word's color). Every tone in the product was therefore a compile-time constant, and the whole
palette was tuned for one terminal: a dark one. On a light terminal apogee paints `#8a8a8a` detail
text and `#ffffff` user text onto whatever the terminal's own background is, and both disappear.

The owner's ask was a **user-configurable color scheme**, and a 2026-08-07 grill settled its shape
question by question. This record is that grill's ratification; it is implemented by
`docs/plans/2026-08-07 - 02 - color-schemes-plan.md`.

Three existing decisions bound the design before it started.
ADR 0011 — the renderer is thin, no
YAML enters it, and its Model is copied by value on every `Update`, so whatever a scheme becomes
inside the TUI has to be copy-safe.
ADR 0037 — a settings edit applies to
the *running* session at ⏎, so "restart to see your colors" was never an available answer; and
ADR 0035's persistence contract
(one key per deliberate edit, spliced and verified) is how the choice gets written down.
ADR 0027 — the prompt's `/skill` and `@file`
tokens are told apart **by color**, which makes two specific roles load-bearing rather than
decorative in any scheme that ships.

And one implementation fact bounded it too: `internal/tui/paintcache.go` caches painted blocks and
its own comment named the obligation — the cache has to be cleared when the theme changes — while
noting that no runtime theme change existed. Making one exist is most of the work.
