---
title: "Step-budget notice: a prune that stubs the noted result leaves stepNoticeLive true, so the note is never re-told"
status: done
type: bug
priority: 3
plan: 2026-09-18 - 00 - capped-delegate-report-loss-plan
aliases: [apogee-6ig]
created: 2026-09-19T09:56:31Z
closed: 2026-09-19T16:00:12Z
reason: "Implemented by docs/plans/archived/2026-09-19 - 00 - deferred-follow-ons-plan.md (items ✅ DONE 2026-09-19; commits 9f1b6a7f 0a01b4ce 104d0c93 4e2d1413 and the if9 item); CHANGELOG [Unreleased] carries the entries. Closed late: bd had no database on this host at closeout."
---

A prune (internal/agent/prune.go:42 autoPrune, apogeectx.PruneKeepTurns=4 at internal/context/prune.go:40) that stubs the tool result carrying the step-budget note drops its ledger via dropStaleAdvice (internal/domain/advice.go:173, called from internal/domain/hooks.go:615) but leaves a.stepNoticeLive true (internal/agent/stepnotice.go:49-57), so a long-capped delegate whose note is pruned is not told again — the ratified re-arm (rearmStepNotice, internal/agent/stepnotice.go:64-67) covers folds and cancelled-Turn rollbacks only. Derived from code, not observed failing. Deferred out of plan 2026-09-18 - 00 (item 6).
