---
title: "No test drives the hooks: fold through ApplyConfig itself"
status: done
type: task
priority: 3
aliases: [apogee-zqs]
created: 2026-09-09T03:23:31Z
closed: 2026-09-11T10:28:50Z
reason: delivered by docs/plans/archived/2026-09-09 - 00 - deferred-findings-plan.md (17c105c1 090b0f63 480095e0 4cb5922e f3f9b321 94978659 5fe4fcc4 300336ff edb2bbe4 9ba9619e)
---

Reaction core stage 2 (plan 2026-09-08 - 00): the startup migration is pinned at `migrateLegacyConfig` and by a LoadFileConfig re-read, but nothing drives the fold through the ApplyConfig path — `internal/config/config.go:2664` and `:2675` (`mayMigrate`). The startup wiring of `mayMigrate=true` is pinned only indirectly, by the `mechanisms:` strip test.
