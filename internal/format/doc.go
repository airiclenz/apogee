// Package format spells the numbers Apogee shows a user, so one figure is not spelled two ways
// by two Drivers over one engine. Today it owns exactly one family — token and context-window
// sizes — and it is the single global formatter for them: the TUI's context gauge, the startup
// box, the pickers, the headless CLI's sub-agent lines.
//
// The rule (ratified 2026-08-07): no spelling ever shows a value of 1000 or more in its unit.
// The ladder divides by 1024 per rung — bare, then k, M, G — because real context windows are
// powers of two (32768, 131072) and models are named that way ("a 128k model"), so 131072 has
// to read as "128k" rather than "131k". The accepted consequence of the binary step is that a
// decimal round number reads slightly small: 128000 spells "125k" and 1000000 spells "977k".
// A unit steps up before its value can reach a thousand, which is the whole point: 1048576 is
// "1M", never "1024k" and never the "1048k" that started this.
//
// Two forms, differing only in precision:
//
//   - Tokens is the coarse form, for the sizes that are round numbers to begin with — a window
//     size, a budget, a limit. Its k rung is always whole ("32k", "977k"); M and G carry one
//     decimal only while it says something ("1.1M", against "1M" and "15M").
//   - TokensFine keeps one decimal in every unit ("4.1k", "32.0k", "1.2M"). Reach for it where
//     two counts are COMPARED — a measured usage against a budget — because the coarse form
//     rounds both sides of a narrow overrun to the same spelling and reads like a bug.
//
// Both return "" for a non-positive count, and that empty string is load-bearing: it is the
// "unknown" sentinel callers test to drop a row, a cell or half a line rather than print a zero
// they do not actually know.
//
// The package is stdlib-only and depends on nothing else in Apogee, which is what lets both
// halves of the binary — the TUI and the CLI — reach one helper instead of keeping twins in
// step by hand.
package format
