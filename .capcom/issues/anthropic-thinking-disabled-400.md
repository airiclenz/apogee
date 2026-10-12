---
title: Anthropic wire sends thinking:disabled, which Opus 5.5 / Sonnet 5.5 / Fable 5.x reject with a 400
status: done
type: bug
priority: 1
aliases: [apogee-anthropic-thinking-disabled-400]
created: 2026-10-02T16:24:19Z
closed: 2026-10-02T22:20:31Z
reason: "Resolved by plan 2026-10-02 - 02 - refocus-findings-plan, archived at 4fa71d90 (item 1: per-model no-effort thinking shape)"
---

internal/provider/wire_anthropic.go writes `thinking: {"type":"disabled"}` on every request whose effort is off/none/minimal/unset (anthropicThinkingDisabled), and compaction (internal/agent/compact.go) forces effort off for the summary request. Per the Claude API thinking docs / claude-api skill (thinking table; model-migration 'Breaking change 1'): disabled is a 400 on Opus 5.5 at every effort, on Sonnet 5.5 (use between_tools), and on Fable 5/5.1 (omit the parameter). So a no-effort request — and every compaction summary — to those models fails. Predates the 2026-10-02 refocus-followups run (present at f68c5c56); item 12 (eafbf8bd) kept it as the no-effort branch. Found by the post-run verifier of 324cd2fd.

## Acceptance

No request to a model that rejects thinking:disabled carries it: per-model choice (omit the key / between_tools / disabled) pinned by codec tests in internal/provider; compaction on the anthropic wire still asks for no visible thinking without a 400-prone shape; ADR 0078 amendment and docs/manual/configuration.md updated.
