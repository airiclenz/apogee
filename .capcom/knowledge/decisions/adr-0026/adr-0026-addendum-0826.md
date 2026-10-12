---
title: "ADR 0026 addendum of 2026-08-26: the blocks are FENCED, and the orientation now precedes them"
---

A block that states only where a repo's text *begins* leaves where it *ends* to be inferred from
the next header — the very inference a repo-controlled file can play with by spelling structure of
its own (security audit F-19). Three changes, all in `contextBlocks`/`standingSystem`:

- **A footer closes every block.** Each file now rides between `## Workspace context: <name>` and
  `## End of workspace context: <name>`, so its extent is stated at both ends by the engine.
- **§3's "verbatim" gains its ONE exception.** A content line that (after `TrimSpace`) begins with
  the header, the footer, or the orientation block's header line is emitted behind the literal
  prefix `[workspace text] `. The line still travels, every byte of it; only its claim to be the
  message's own furniture is removed. Nothing else is rewritten — `{{braces}}` still never meet
  `internal/prompt`, and `TestContextSeam_ContentIsDataNotTemplate` is unchanged.
- **§4's order sentence changes.** The wire order is now **prompt → orientation → context files →
  mechanism directives → tool block** (ADR 0023's
  2026-08-26 addendum): the engine's own facts can no longer be preceded, and therefore framed, by
  a workspace file. §4's independence rule is untouched — either configured source alone still
  seeds the message, and neither still seeds nothing.

The orientation block also gains a line naming the header the blocks ride under, rendered only for
a session that actually holds one, so the fenced content below it is read as project text rather
than as more harness facts. Content is still DATA in the sense §3 means: never a template, never
validated, never able to fail startup.
