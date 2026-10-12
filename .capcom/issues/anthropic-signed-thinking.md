---
title: "Anthropic wire: signed-thinking carrier (replay thinking blocks)"
status: done
type: feature
priority: 3
aliases: [apogee-4kl]
created: 2026-09-16T18:04:11Z
closed: 2026-10-02T09:51:52Z
reason: "Resolved by plan '2026-10-02 - 00 - refocus-followups-plan' items 10-12 (archived at 4c5d92aa): anthropic thinking blocks carried verbatim, persisted and replayed; adaptive thinking requested when an effort resolves."
---

The anthropic codec (internal/provider/wire_anthropic.go, plan 2026-09-16 - 03 item 9) always sends thinking: {"type":"disabled"} — v1 never requests thinking on the Messages wire because the seam (provider.Message / RawResponse) has no carrier for the signed thinking blocks Anthropic requires replayed verbatim on the next request of an assistant turn that thought. Follow-up: add a signed-thinking carrier to the seam (block signature + text, kept opaque), replay it in the encoder, and let the wire request adaptive thinking (thinking: {type: adaptive}) so ThinkingEffort can drive real reasoning; also lifts the Opus-5 constraint that disabled thinking is a 400 at effort xhigh/max. Session-record persistence of the blocks is part of the carrier.

## Log

- 2026-09-16 beads: discovered while working on anthropic-wire-dialect
