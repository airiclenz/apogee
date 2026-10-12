---
title: Anthropic replay merges a reply's text blocks, editing the prefix signed thinking was produced on
status: done
type: bug
priority: 2
aliases: [apogee-anthropic-merged-text-blocks]
created: 2026-10-02T16:24:21Z
closed: 2026-10-02T22:20:33Z
reason: "Resolved by plan 2026-10-02 - 02 - refocus-findings-plan, archived at 4fa71d90 (item 2: reply text blocks replay in original places)"
---

The anthropic encoder folds every text block of an assistant reply into one leading text block. A reply shaped [text A, thinking, text B] is sent back as [text "AB", thinking]. That edits the turn: model-migration 'Breaking change 3' (preserved thinking) says editing or reordering an earlier turn invalidates later thinking blocks, and accounts created on or after 2026-08-31 get a 400 for it. 324cd2fd placed thinking blocks back by tool-call/text position but deliberately left text merging as it was, so thinking can still land in the wrong place relative to text. Predates the 2026-10-02 run.

## Acceptance

An assistant reply round-trips through decode → persist → encode with every block (text, thinking, redacted_thinking, tool_use) in its original order and content; test covers interleaved text/thinking.
