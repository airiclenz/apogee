---
title: "gitexec: a root that is no repository memoises its command-config answer on no prints, so a repo git init-ed under it mid-session is served the pre-init answer until restart"
status: done
type: bug
priority: 3
plan: 2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan
aliases: [apogee-se0]
created: 2026-09-22T07:48:02Z
closed: 2026-09-23T06:56:02Z
reason: "Item 9: unreached-repo command-config probe never memoised; resolved by plan '2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan' (archived at 152dc896)"
---

Deferred out of plan 2026-09-20 - 02 - fence-sanitize-and-defects-plan, item 7b (the command-config memo keyed on the config's identity).

A root that is no repository yields no file from `git rev-parse --git-path`, so its memoised answer carries no fileprints and `commandConfigProbe.holds()` holds trivially for the process lifetime (internal/gitexec/gitexec.go:402-411, the 'No prints at all … holds trivially' rule; the cache hit at internal/gitexec/gitexec.go:463). A repository `git init`-ed under such a root mid-session and then given a command-valued key (`filter.<x>.clean`, `diff.<x>.textconv`, …) is therefore still served the pre-init clean answer until apogee restarts, and the next git tool runs it.

This is the plan's own rule — no `.git/` path is assumed, a `GIT_DIR`-redirected run is judged by its own store — and the fake-git memo test in internal/gitexec/gitexec_test.go depends on the no-prints answer memoising. Closing it needs an assumed `<root>/.git` print (or a print of the root directory itself) that the rule forbids; the design call is the owner's.

Behaviour is unchanged from before the run for this case (the memo was once-per-process for every root); the repository case is closed by 4e394d43.
