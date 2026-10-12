---
title: TestApplyConfigFoldsTheHooksBlock leaves the fold notice's backup path unasserted
status: done
type: task
priority: 3
aliases: [apogee-330]
created: 2026-09-09T06:59:08Z
closed: 2026-09-11T10:28:51Z
reason: fixed in plan 2026-09-11 - 00 items 2 and 3
---

internal/config/configmigrate_test.go:520 — the ApplyConfig fold test asserts containment of the rewritten file plus a silent second launch rather than re-asserting the byte golden of the rewritten file (that golden lives in TestMigrateLegacyConfigFoldsTheHooksBlock, internal/config/configmigrate_test.go:888), and the backup path named in the one fold notice announced through the startup callback is not asserted at all. Deferred from plan 'docs/plans/2026-09-09 - 00 - deferred-findings-plan.md' item 4.
