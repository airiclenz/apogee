---
title: "The theme is a function of its inputs (architecture review 2026-11-10 #10)"
status: parked
type: task
priority: 4
labels: [tui]
created: 2026-11-10T19:36:32Z
---

Found by docs/reviews/architecture-review-2026-11-10.html, verified 2026-11-10 at a5c4f2db. Verified VALID (Speculative): the hand-kept carry list and clear-by-discipline are real; no bug has been seen, and the paint-cache comment records clearing over keying as a deliberate call.

Problem: the theme is the scheme plus three terminal/user facts (measure, padPrompts, glyphs) written at four sites (internal/tui/model.go:768, :1241; settingsapply.go:545; width.go:97). `applyColorScheme` carries the three across a rebuild by hand (settingsapply.go:689-691), so a fourth fact would be silently dropped on a scheme switch. The paint cache key names measure and padPrompts but not scheme or glyphs (paintcache.go:176-186), so the glyph and scheme applies must each remember `m.transcript.paints.clear()`. The scheme is stored three times (`m.th`, `opts.ColorScheme`, `opts.UI.ColorScheme`).

Solution: one rebuild entry, `theme(scheme, glyphs, terminal facts)`, and a theme generation in the paint-cache key so the cache clears itself. One test replaces the per-field carry guards.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
