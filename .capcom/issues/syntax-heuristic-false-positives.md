---
title: Write-tool syntax trailer flags valid JSX, PHP, C# and Rust as broken, and Bypass cannot turn it off
status: done
type: bug
priority: 2
created: 2026-11-10T19:36:31Z
closed: 2026-11-11T06:36:34Z
reason: resolved by audit-defects plan (2026-11-10 - 02 - audit-defects-plan), archived at e5e5bb54
---

Source: docs/reviews/code-audit-2026-11-10.md ("The syntax check false-positives on valid JSX, PHP and C#"). Verified PARTLY (consumer corrected, one more shape found) 2026-11-10 at a5c4f2db.

**Where:** `internal/syntaxcheck/syntaxcheck.go` `checkBrackets` (~:185), consumed by `internal/tools/syntaxtrailer.go` `syntaxTrailer`, which `write_file`, `file_edit` and `find_replace` append to their success results.

**What:** the heuristic opens a string on every `'`, `"` and backtick and keeps string/escape state across line ends for every language. Valid code it misreads:
- JSX/TSX text with an apostrophe: `<p>Don't do this</p>` → "unclosed string literal" plus unclosed `(`/`{`.
- PHP `# don't` comments (`#` deliberately not a PHP comment because of `#[Attr]`).
- C# verbatim strings ending in a backslash: `@"C:\temp\"` (the `\` escapes the closing quote).
- Rust char literals of a bracket: `'('`, `'{'` (`'` is skipped for Rust, so the bracket is pushed) — not in the report.

**Why it matters:** the report says this triggers a correction Turn; it does not — that Mechanism was retired (ADR 0071). The real consumer is the always-on syntax trailer, which is neither a Floor guard nor a Reaction (docs/manual/configuration.md), so `--bypass` cannot switch it off. A valid write comes back with "syntax check (heuristic): N problem(s)" and a small model may churn or "fix" correct code — model-facing output that can make a model do worse than the bare loop, in every mode.

**Suggested fix:** stay silent on these shapes rather than guess: no `'` string in JSX text (between `>` and `<`); reset `'`/`"` state at end of line where such strings cannot span lines (keep multi-line only for JS/TS backticks, Python triple quotes, C# raw/verbatim strings); PHP `#` is a comment unless followed by `[`; `\` does not escape the closing quote of C# `@"…"`; Rust `'x'` char literals are inert. Correct the package comment that still speaks of "deferring a needless correction". Add regression vectors for each shape.

## Acceptance

- syntaxcheck.Check reports valid for a .jsx/.tsx file with an apostrophe in JSX text, a PHP file with a # don't comment, a C# @"C:\temp\" literal and a Rust '(' char literal.
- Writing each of those files with write_file returns no syntax trailer.
- Existing true-positive vectors (unclosed bracket, truncation) still fire.
