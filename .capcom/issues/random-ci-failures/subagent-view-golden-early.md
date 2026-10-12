---
title: TestE2ESubAgentView t17-run-view golden captured one tick early (wait lacks the read receipt)
status: done
type: bug
priority: 2
plan: 2026-09-17 - 02 - ci-flake-causes-plan
aliases: [apogee-ig7]
created: 2026-09-17T10:46:22Z
closed: 2026-09-19T16:00:10Z
reason: "Implemented by docs/plans/archived/2026-09-17 - 02 - ci-flake-causes-plan.md (items ✅ DONE 2026-09-17); CHANGELOG [Unreleased] carries the entries. Closed late: bd had no database on this host at closeout."
---

3 CI failures: golden t17-run-view diff where the a.txt read row lacks its '2 lines' receipt. The frameWhen condition (cmd/apogee/e2e_subagent_view_test.go ~L111) requires childParkedLine='a.txt', which paints when the read CALL row appears, before its result lands; the golden was recorded after the receipt. On a slow runner the condition is met one frame early. Fix: require the receipt ('2 lines' — restate as a named constant) in the condition alongside !holds(secondRead).

## Acceptance

Test passes 20x under -race pinned to one core.
