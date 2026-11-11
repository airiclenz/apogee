---
title: A deeply nested PDF overflows the goroutine stack and kills the process
status: done
type: bug
priority: 1
labels: [security]
plan: 2026-11-11 - 00 - pdf-parser-fork-and-defects-plan
created: 2026-11-10T19:37:31Z
closed: 2026-11-11T10:11:34Z
reason: "Resolved: in-tree pdfparse bounds recursion at MaxDepth (item 2) and ends every read loop at end of input (item 3), completing the depth refusal landed earlier (plan '2026-11-11 - 00 - pdf-parser-fork-and-defects-plan', archived at 87846d40)"
---

Source: docs/reviews/code-audit-2026-11-10.md (High, "A hostile PDF can crash the whole process with a stack overflow"). Verified VALID 2026-11-10 at a5c4f2db.

**Where:** `internal/doctext/pdf.go:201-231` (`ExtractPDF`), the preflight `pdfMaxNesting` (`pdf.go:684-687`, enforced at :1009/:1033), and the third-party parser `github.com/ledongthuc/pdf@v0.0.0-20250511090121-5959a4027728` (`lex.go:411-495` `readObject`/`readArray`/`readDict`, reached from `read.go:365` `readXrefTable` on every file and from `ps.go:138` `Interpret` on every page's content stream).

**What:** the parser recurses once per `[` or `<<` with no depth limit. `ExtractPDF`'s `recover` cannot catch Go's fatal stack overflow. `pdfMaxNesting` bounds only the preflight's own lexer, where too much nesting just makes the stream dictionary unreadable — it never refuses the document; nothing else in `refuseAbsurdObjectCount`/`preflightPDF` looks at nesting, and `pdfMaxReads` does not help (10 MiB is ~2,600 reads). At ~200-400 B of stack per level, ~3-5 MB of `[` in a trailer or object (under the 10 MiB read ceiling) exceeds the 1 GB stack; so does a FlateDecode content stream of `[` inflating within the 64 MiB budget (a ~64 KB file).

**Why it matters:** `read_file` on a repo or downloaded PDF (internal/tools/read_file.go:184), an `@file` reference (internal/agent/loop.go:1718) or the path_read route kills the whole process — the TUI (terminal left in raw mode), a headless run, or the daemon with every session in it. Injected text can steer the model into that read. The package comment's "nothing escapes this function" is false for this class. No duplicate: pdf-reference-quadratic-scan and pdf-ceiling-not-discriminating covered other classes.

**Suggested fix:** bound nesting before the parser sees the bytes. In `preflightPDF`, refuse a document whose bytes outside stream bodies nest `[`/`<<` deeper than a small bound (e.g. 64), skipping strings and comments as the lexer does. Apply the same bound to each decoded content stream the walk will interpret (during the preflight's inflation measurement, or before `GetPlainText`). The refusal reads like any other unreadable-PDF failure (`pdfUnreadableFormat`).

## Acceptance

- A PDF whose trailer or an object holds deeply nested arrays (e.g. 5 MB of [) returns a model-facing failure from ExtractPDF, and the process survives.
- A PDF whose FlateDecode page content stream inflates to deeply nested [ (or <<) is refused the same way.
- Tests in internal/doctext cover both shapes and run under the suite's memory limits: the refusal comes before the parser recurses.
- The package comment's 'nothing escapes this function' claim is true again, or worded to what is now guaranteed.

## Log

- 2026-11-11 Airic Lenz: audit-defects plan item 1 (archived at e5e5bb54) landed the depth refusal for nested object bytes, /ObjStm, /CMap, /ToUnicode and page content streams; left open: a forged xref offset into an unscanned binary stream body still overflows the stack, and an unclosed '[' at EOF hangs the parser.
