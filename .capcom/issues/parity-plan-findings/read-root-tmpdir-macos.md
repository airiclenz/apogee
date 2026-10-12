---
title: Extra-read-root tests hand the mount an unresolved TMPDIR, so 12 tests fail on macOS
status: done
type: bug
priority: 2
aliases: [apogee-v00.1]
created: 2026-09-04T11:14:52Z
closed: 2026-09-04T11:32:18Z
reason: "Fixed: the affected tests now resolve their temp roots through the package's own helper (tempRoot / readFenceRealDir / a new realTempDir in internal/agent). ./internal/tools, ./internal/agent and ./cmd/apogee green except TestE2ESmokeInProcess, which is apogee-v00.2."
---

domain.Config.ExtraReadRoots' contract is that every mounted root is already the host's symlink-RESOLVED real path — internal/tools matchRoot skips any root that is not its own real path, and internal/skills readRoots resolves before mounting. Several tests hand the seam a raw t.TempDir(), which on macOS sits under /var, a symlink to /private/var. The mount then serves nothing and the test measures the wrong refusal. On Linux the two spellings coincide, which is why CI never saw it.

PARTLY FIXED in an uncommitted working tree (2026-09-04): internal/tools grep/find_files/list_dir now use the package's existing tempRoot(t); internal/agent mountFixture resolves through a new realTempDir helper; cmd/apogee firingRoots, TestRegistryWithMCPThreadsExtraReadRoots and presentWorkspace use the package's existing readFenceRealDir(t). internal/tools, internal/agent and the three named cmd/apogee tests pass after those edits.

REMAINDER: TestE2ESmokeInProcess still fails, and NOT for the symlink reason — see the sibling bead.

## Acceptance

go test ./internal/tools/ ./internal/agent/ ./cmd/apogee/ passes on macOS with no /var-vs-/private/var failures.
