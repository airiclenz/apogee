---
title: TUI glyphs missing from common monospace fonts drift rows on DOM-rendered terminals
status: open
type: bug
priority: 3
plan: 2026-10-13 - 00 - open-bugs-sweep-plan
aliases: [apogee-glyphs-missing-from-fonts]
created: 2026-10-10T08:30:46Z
---

The TUI draws glyphs that the common Windows monospace fonts lack. In a terminal that renders text as flowing DOM text instead of on a strict cell grid, the fallback glyph (Segoe UI Symbol, proportional) is wider than a cell and every later cell on the row shifts right. Long runs of the tool-row leader push the outcome slot ("pending", "running", "exit 0") past the right edge.

Observed 2026-10-10: VS Code integrated terminal (cmd and PowerShell, via ConPTY) with `terminal.integrated.gpuAcceleration: "off"` and font 'Source Code Pro', Consolas. Fine in a plain console window (strict grid). Setting `gpuAcceleration` to "auto" (WebGL renderer, cell grid) fixed it for the reporter, so this is a robustness and compatibility issue, not a blocker. apogee's width measure is correct: the glyphs are single-cell in both WcWidth and GraphemeWidth; only the renderer drifts.

Coverage checked against SourceCodePro-Regular, consola.ttf and CascadiaMono.ttf (GlyphTypeface.CharacterToGlyphMap):

Theme constants (internal/tui/theme.go:41-98):
- ⋯ U+22EF leader: missing in all three (the main offender, because it repeats)
- ✦ U+2726 assistant/tool star and skill: missing in all three
- ❯ U+276F user prompt / menu selection: missing in SCP and Consolas
- ⧖ U+29D6 interjection: missing in all three
- ▤ U+25A4 presented doc: missing in SCP and Consolas
- ✓ U+2713 done: missing in Consolas
- ▶ U+25B6 collapsed: missing in Consolas
- ⊞ ◐ ✔ ⏵⏵ mode glyphs: missing in SCP and Consolas (⊞ ✔ ⏵ in all three)

Other UI literals:
- spinner braille ⣾⣽⣻⢿⡿⣟⣯⣷ (spinner.go:60): missing in SCP and Consolas, shown on every in-flight row
- key hints ⇥ ⇧ ⌃ ⌥ ⌫ ⏎ ␣ (help.go, ask.go:354/362, prompteditor.go:136, settings.go:241, cmd/apogee/settingsrows.go:51)
- ⟳ ⑂ (sessions.go:89,96); ✗ (settings.go:1485); ✔ (tasklist.go:41, ask.go:238)
- block eighths ▁ ▉▊▋▍▎▏ ▔ (model.go:3641,4447; sessionrule.go:51; sessions.go:76; settings.go:269): missing in Consolas only

Out of scope: probe/terminal.go (deliberately exercises width edge cases), sanitize, judge, floor/salvage, htmltext, agent/loop, run_tests and config strings (text processing or text sent to the model, not drawn).

Candidate replacements present in SCP, Consolas and Cascadia: · (leader), ♦ (star), › (prompt), » (interject), ≡ (presented), √ (done and allow-edits), ► (collapsed), □ ○ ►► (plan, ask-before, auto). Options considered: (1) swap the theme constants only; (2) also swap the spinner and key hints (text names like Tab, Shift, Ctrl, Enter); (3) keep the current glyphs and add a font-safe glyph-set setting. Not yet decided. Tests and layout.md pin the current glyphs.
