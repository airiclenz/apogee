---
title: "The config writer reaches any registry path (architecture review 2026-11-10 #2)"
status: parked
type: task
priority: 3
created: 2026-11-10T19:36:32Z
---

Found by docs/reviews/architecture-review-2026-11-10.html, verified 2026-11-10 at a5c4f2db. Verified VALID, with two minor overstatements: SettingsHost has 8 methods, so dropping the two panel-only ones leaves 6 (the write family drops to four); the footer keys being unwritable is by design today (structured, edited in the file).

Problem: `scalarPathDepth = 2` (internal/config/configwrite_scalar.go:41) refuses three-segment paths, and its "every registry row is within it" comment is stale (ui.side-panel.* and ui.footer.* are three deep). So the `ui.side-panel:` block has a second, 454-line splice engine (configwrite_sections.go); `settingsHost.Write`/`Reset` branch on `config.IsSidePanelKey` (cmd/apogee/wire_options.go:588, :633); SettingsHost grew two panel-only methods (internal/tui/tui.go:308, :319); the section-mode and folded-list edits are written twice, in tui (sidepanel.go:1358, :1385) and config (configwrite_sections.go:158, SaveSidePanelFold). SaveTo/ResetIn have no side-panel branch and work only because every ui.* key is global-only — a latent trap.

Solution: generalise the scalar splice to any registry path depth (create absent parents, lists as whole values). Gestures pass the whole next value through Write; the side-panel engine and the two methods go; one writer table covers depth 1-3.

Caveat for design: the sections and fold writes today read-modify-write against the file inside the edit transaction (file entries keep order and modes; an unnamed section brings in the whole resolved list). A whole-value write must prove the session's canonical list always equals that merge, or keep the merge in the writer, else it can clobber a hand edit the watcher has not delivered yet. ADR 0035 D2's splice contract is unchanged.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
