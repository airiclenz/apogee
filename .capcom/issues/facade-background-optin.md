---
title: Facade has no opt-in for fan_out background or the workflow tool
status: done
type: feature
priority: 2
plan: 2026-09-28 - 00 - enterable-workflow-stages-plan
aliases: [apogee-facade-background-optin]
created: 2026-09-28T11:31:56Z
closed: 2026-09-28T20:49:29Z
reason: "Resolved by plan '2026-09-28 - 00 - enterable-workflow-stages-plan' item 3 (archived at dd782ee9): domain.Config.OffersBackground lets a facade engine opt into fan_out background and the workflow tool."
---

The facade has no opt-in for fan_out's background field or the workflow tool: internal/agent/construct.go:405 defaultRoster passes offersBackground false (tools.HostToolsOf(cfg, false, false)), and an out-of-module embedder cannot import internal/tools. Until one exists the bench cannot run the engine-run workflows plan's item 39 Experiment 3 through the facade, straining ADR 0031's benchable-all-the-way-up invariant.
