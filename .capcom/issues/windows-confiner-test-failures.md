---
title: Two Windows confiner tests fail on the windows/arm64 dev host, already at base ea51204c
status: done
type: bug
priority: 3
aliases: [apogee-windows-confiner-test-failures]
created: 2026-09-23T12:25:11Z
closed: 2026-10-02T09:51:49Z
reason: "Resolved by plan '2026-10-02 - 00 - refocus-followups-plan' item 1 (archived at 4c5d92aa): fixed by 8c76961a; owner saw both tests pass on windows/arm64."
---

Deferred out of the implement-plan run of 'docs/plans/archived/2026-09-23 - 01 - winlabel-journal-trust-and-landlock-udp-proof-plan.md' (2026-09-23).

On the owner's Windows 11 arm64 dev box, go test ./internal/platform/ fails two tests:
- TestWindowsUnclearableDescendantKeepsTheJournal (internal/platform/confiner_windows_test.go:908): "Close reported success while a descendant kept its Low label..."
- TestWindowsFailedRootLabelWriteUnwindsItsJournalEntry (internal/platform/confiner_windows_test.go:1015): "Confine labelled a box whose root label write must be denied..."

Both fail with the same messages in a worktree at ea51204c, the commit before the run, so no commit in the run caused them. Items 1-7 left the failure messages unchanged. Both tests depend on a label write (or a descendant's label clear) being denied, so this host's policy probably grants what the tests expect to be refused. Either the tests need a precondition probe that skips when the host cannot produce the denial, or they expose a real fence gap on this host. Decide which before changing the tests.

Context: the closeout's unraced make test on the same host also saw ~123 other Windows failures, all present at ea51204c as well, and TestE2EDefaultWireStaysChatCompletions is flaky there (about 1 in 3 at base). CI is not affected.

## Log

- 2026-09-30 Airic Lenz: Probe landed; on the windows/arm64 box run: go test -count=1 -run 'TestWindowsUnclearableDescendantKeepsTheJournal|TestWindowsFailedRootLabelWriteUnwindsItsJournalEntry' -v ./internal/platform/ — a SKIP names the host fact; a FAIL is a real fence bug.
