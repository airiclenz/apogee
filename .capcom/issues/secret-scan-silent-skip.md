---
title: The pre-commit secret scan silently skips itself after 2s
status: done
type: bug
priority: 1
labels: [slow-box]
aliases: [apogee-c8l]
created: 2026-09-22T16:03:19Z
closed: 2026-09-23T06:01:43Z
reason: Closed
---

internal/agent/secretsguard.go:40 commitSecretsTimeout = 2 * time.Second per git invocation, and on expiry the check is SKIPPED for that call. That is a security control that degrades off without saying so: two seconds covers a shadow-index `git add` over the call's files, but a large file set on a slow disk or a cold page cache exceeds it and the commit proceeds unscanned.

Contrast internal/agent/treesnapshot.go:32, the same 2s value, where the skip is deliberate and degrades a WARNING rather than a control.

Found by the slow-box deadline sweep, 2026-09-22.

## Acceptance

A scan that runs out of time is reported, not silently skipped; the budget is sized for a large repo on slow storage.

## Log

- 2026-09-23 Airic Lenz: Verified fixed in the tree at 533ce9af (slow-box-deadlines plan, item 2, commit f090b515).

  - internal/agent/secretsguard.go: commitSecretsTimeout 2s -> 30s, and it now bounds the WHOLE pre-check (one context covers all four shadow runs) rather than each git invocation.
  - The silent skip on expiry is gone. The check has three outcomes instead of two, stated as a binding robustness contract in the package doc: (a) no repository to scan — still a silent skip, nothing to look at; (b) clean; (c) RESOLVED a repository and could not finish — errScanTimedOut / a mid-scan git failure now forces the Tier-2 approval look with incompleteScanReason in the audit trail and incompleteScanHint on the prompt's Fix row ('the staged-secret scan could not finish, so the staged content is unchecked'). The pre-check still never FAILS a commit the tool would have made; the worst it does is ask.
  - That explicitly supersedes ADR 0080 decision 6 and ADR 0056 decision 4, whose blanket silent skip made a control that did not run indistinguishable from one that ran clean.
  - Covered by new cases in internal/agent/secretsguard_test.go; the configuration manual gained the note.

  Acceptance met on both halves: the incomplete scan is reported, and the budget fits a large index on slow storage.
