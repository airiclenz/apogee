---
title: TestE2ESmokeInProcess asserts a footer segment that a long TMPDIR path pushes out
status: done
type: bug
priority: 2
aliases: [apogee-v00.2]
created: 2026-09-04T11:15:02Z
closed: 2026-09-04T11:38:42Z
reason: "Fixed: the first frame no longer claims the workspace (100 columns is not a width the fit promises it at), and the wide frame's width is computed from the path's own length via a new footerRoomFor helper. make check is fully green on macOS."
---

e2e_smoke_test.go:50 requires the footer to name the workspace, and :165 requires the 120-column footer to carry more separator glyphs than the narrow one. Both fail on macOS, and resolving the workspace path does not fix them: the resolved spelling (/private/var/folders/<hash>/T/TestE2ESmokeInProcess<n>/002) is about 85 characters, so the footer drops the workspace segment entirely even at 120 columns. On Linux TMPDIR is /tmp/... and it fits.

The test is therefore measuring the length of the host's TMPDIR, not the footer's behaviour. Options: give the e2e workspace a SHORT real path (needs a cross-platform way to get one — hardcoding /tmp breaks Windows), or assert the footer's workspace segment at a width chosen from the path's own length rather than a fixed 120.

Confirmed still failing after the sibling bead's symlink fixes.

## Acceptance

TestE2ESmokeInProcess passes on macOS and on Linux without depending on how long the host's TMPDIR is.
