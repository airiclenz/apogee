---
title: "The prune note is worded at three sites instead of on the event (architecture review 2026-10-14 #16)"
status: parked
type: task
priority: 4
labels: [driver-parity]
created: 2026-11-21T12:44:48Z
---

Found by docs/reviews/archived/architecture-review-2026-10-14.html (#16 leftover); re-verified 2026-11-21 at 43ece2f2.

`domain.PruneEvent` (internal/domain/events.go:535) has no `Notice()`, while RefClippedEvent (:563), MalformedChunksEvent (:590), UpstreamSwitchedEvent (:883) and UpstreamLoadingEvent (:914) do. So `pruned %d tool results (~%d tokens)` is written by hand three times: headless narration (cmd/apogee/headless.go:284), the record fold (internal/run/transcript.go:103, whose comment says it is "worded exactly as the TUI words it") and the TUI (internal/tui/transcript.go:3359, `addPrune`). Only a comment keeps them in step. The card's main concern, a forgotten Event variant dropping silently, is already fixed by `TestEncodeMapsEveryEventVariant` (internal/eventjson/encode_test.go:1167, b839a7fb).

Direction: add `PruneEvent.Notice()` and have all three sites print it, as the other notice-bearing events do. No wire form changes; ADR 0018 D6 adds no Event variant.

## Acceptance

- One wording, pinned by a domain test; the three sites call `Notice()`.
