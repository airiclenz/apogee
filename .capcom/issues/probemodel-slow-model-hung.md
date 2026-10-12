---
title: probemodel's 60s per battery call calls a slow local model hung
status: done
type: bug
priority: 2
labels: [slow-box]
aliases: [apogee-5y94]
created: 2026-09-22T16:03:46Z
closed: 2026-09-23T06:02:11Z
reason: Closed
---

cmd/apogee/probemodel.go:22 batteryRequestTimeout = 60 * time.Second per battery call. The comment's premise is the bug: "a call still running after a minute is a hung server rather than a slow model". On a 30B-class model quantised onto CPU — the hardware this product is built for — one short exchange of prefill plus generation past sixty seconds is ordinary.

Found by the slow-box deadline sweep, 2026-09-22.

## Acceptance

A slow local model completes the battery; the budget is sized for CPU inference or made configurable.

## Log

- 2026-09-23 Airic Lenz: Verified fixed in the tree at 533ce9af (slow-box-deadlines plan, item 7, commit 297535bb).

  - cmd/apogee/probemodel.go: batteryRequestTimeout 60s -> 300s, and it is now the DEFAULT rather than the bound — 'apogee probe model --timeout' overrides it (flags.DurationVar at the command's flag set).
  - The comment whose premise was the bug is rewritten. It no longer claims 'a call still running after a minute is a hung server rather than a slow model'; it now says the servers this probe exists for are the slow ones, that a 30B-class model quantised onto CPU ordinarily spends minutes on a single short exchange, and that cutting that off would report a healthy server as broken. It also states what the budget is NOT: not a bound on the command and not a bound on one battery call, since a timed-out attempt is retryable and the client's retries multiply it by the attempt budget.
  - Covered by cmd/apogee/probemodel_test.go; docs/manual/probe.md documents the flag.

  Acceptance met: sized for CPU inference AND made configurable.
