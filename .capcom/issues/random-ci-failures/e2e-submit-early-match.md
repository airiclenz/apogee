---
title: e2e submit() matches the typed prompt against the whole frame — the command palette row satisfies it early (/usage flake)
status: done
type: bug
priority: 2
plan: 2026-09-17 - 02 - ci-flake-causes-plan
aliases: [apogee-htf]
created: 2026-09-17T10:46:21Z
closed: 2026-09-19T16:00:09Z
reason: "Implemented by docs/plans/archived/2026-09-17 - 02 - ci-flake-causes-plan.md (items ✅ DONE 2026-09-17); CHANGELOG [Unreleased] carries the entries. Closed late: bd had no database on this host at closeout."
---

TestE2EUsageReportsCachedTokensAndDelegateSpend and TestE2EUsageHidesTheCachedColumnWithoutABreakdown fail on CI with 'no /usage row named main' and the header row being the palette title. Cause: submit() (cmd/apogee/e2e_smoke_test.go ~L463) waits for promptTail(text) anywhere on screen; when the human types '/usage' the command palette paints '❯ /usage  session token usage — main agent and every sub-agent' while the box still holds '/us', the wait is satisfied, Enter ACCEPTS the suggestion instead of sending, and openUsage's usagePaneMarker ('session token usage', e2e_usage_test.go:306) is satisfied by the same palette row. Passes on a fast box only because typing outruns the palette paint. Fix: have submit() look for the tail inside the prompt box rows only (the box is the bordered block above the footer; tuitest.Frame can expose a 'prompt box' accessor), and make openUsage wait for the pane title / a pane-only marker rather than a string the palette also carries. Audit other markers that a suggestion band could echo.

## Acceptance

submit('/usage') sends the command (pane opens) even when the palette paints first; both usage e2e tests pass 20x under -race with taskset -c 0.
