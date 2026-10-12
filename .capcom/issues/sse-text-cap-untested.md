---
title: "Anthropic SSE parser: no fixture test for the 8 MiB text cap"
status: done
type: task
priority: 3
labels: [test]
aliases: [apogee-2kj]
created: 2026-09-16T20:24:30Z
closed: 2026-09-19T16:00:12Z
reason: "Implemented by docs/plans/archived/2026-09-19 - 00 - deferred-follow-ons-plan.md (items ✅ DONE 2026-09-19; commits 9f1b6a7f 0a01b4ce 104d0c93 4e2d1413 and the if9 item); CHANGELOG [Unreleased] carries the entries. Closed late: bd had no database on this host at closeout."
---

The anthropic stream parser's text-cap arm (internal/provider/wire_anthropic_stream.go:190, anthropicStream.text — the DeltaError 'exceeded the N MiB text limit' emitted past maxReplyTextBytes, plan 2026-09-16 - 03 item 10) has no fixture test of its own; the cap is pinned only on the openai wire (internal/provider/stream_test.go:921). Deferred out of the 2026-09-16 - 03 run: add a wire_anthropic_stream_test.go case that streams past maxReplyTextBytes on the Messages wire and asserts the terminal DeltaError plus that delivered text stays at or under the cap.
