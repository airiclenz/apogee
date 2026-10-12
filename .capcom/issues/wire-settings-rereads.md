---
title: Consolidate wire_settings.go's five independent LoadFileConfig re-reads
status: done
type: task
priority: 3
aliases: [apogee-o60]
created: 2026-09-08T06:10:22Z
closed: 2026-09-11T10:28:50Z
reason: delivered by docs/plans/archived/2026-09-09 - 00 - deferred-findings-plan.md (17c105c1 090b0f63 480095e0 4cb5922e f3f9b321 94978659 5fe4fcc4 300336ff edb2bbe4 9ba9619e)
---

cmd/apogee/wire_settings.go re-reads config.LoadFileConfig(a.configPath, ...) independently at five sites (reloadMechanisms, reloadValidatedSets, reconnectMCP, reloadHooks, reloadModelProfiles). The Reaction core's stage 2 collapses the reactions half into one SetReactions generation swap (ADR 0076 amendment A8) and stage 2 deletes validated-sets; the remaining MCP and model-profile re-reads are unrelated to reactions and were left out of scope deliberately. One shared re-read per reload would be the tidy end state.
