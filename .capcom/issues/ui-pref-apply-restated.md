---
title: "Applying a UI pref on screen is one module (architecture review 2026-11-10 #3)"
status: parked
type: task
priority: 3
labels: [tui, test-gap]
created: 2026-11-10T19:36:32Z
---

Found by docs/reviews/architecture-review-2026-11-10.html, verified 2026-11-10 at a5c4f2db. Verified VALID. The latent sweep gap is real but not reachable today (every current ui.* key has an arm).

Problem: applying a ui.* key on screen is restated per key, per gesture and per test. `settingsApplyLocal` (internal/tui/settingsapply.go:475) has 19 arms; 12 keys only call `m.opts.UI.Set` (the 09-30 review's #20 counted 10). The binary's test hand-keeps a 21-key mirror of that switch (cmd/apogee/wire_settings_test.go:964). Seven gesture write-backs repeat apply → persist → "not saved:" → journal (settingsapply.go:784, :822; sidepanel.go:1179, :1199, :1301, :1345, :1597).

Latent gap: `uiKeys` (internal/domain/uiprefs.go:609) leaves out footer rows/colours and side-panel sections/folded, yet `UIPrefs.Set` lands all 20. The tui sweep (settingsapply_test.go:121) walks `UIKeys()`; the cmd sweep skips non-Editable keys; but the reload diff (cmd/apogee/settingsedit.go:267) walks the whole registry. A new structured ui.* key without an arm passes every test and is then refused as `cannotApply` (cmd/apogee/wire_settings.go:1072) on the next file reload. Cheap standalone fix if the refactor is not pursued: sweep every ui.* registry row, not `UIKeys()`, in the tui apply test.

Solution: one TUI module for "UI prefs on this screen": by default a key lands through `UIPrefs.Set`; a small follow-up table covers keys that must tell a widget (glyphs, task-list-open, tools-open, tools-fold-over, pointer-shapes, git-fetch, color-scheme). It exports its key set so the binary's test reads it instead of a copy, and one persist(key) door serves every fold, pin and width gesture. Pairs with #2 (side-panel-writer-split).

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
