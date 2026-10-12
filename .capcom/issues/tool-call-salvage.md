---
title: "Tool-call salvage floor guard: run a JSON tool call a native-profile model left in its text"
status: done
type: feature
priority: 2
plan: 2026-09-06 - 02 - undo-snapshots-and-tool-call-salvage-plan
aliases: [apogee-e61]
created: 2026-09-06T16:12:32Z
closed: 2026-09-07T10:38:59Z
reason: "Delivered by plan 'docs/plans/archived/2026-09-06 - 02 - undo-snapshots-and-tool-call-salvage-plan.md' items 2-5 (DONE 2026-09-07): internal/floor/salvage.go (SalvageToolCall), loop wiring as the seventh Floor guard behind 'tool-call-salvage' (internal/agent/floorguards.go), ADR 0071 amendment + CONTEXT + manual, probe finding (internal/probe/battery.go), e2e in cmd/apogee/e2e_salvage_test.go. Shipped in v0.20.8 (commits 84a2bef6..a2b52ea3)."
---

Gap 2 of docs/handoffs/2026-09-06 - 02 - contender-gap-assessment.md. A response with no wire tool_calls whose content carries a fenced/<tool_call>/whole-content JSON object naming a registered tool is read today as exchange-complete (loop.go:221); this killed the qwen25-coder-14b campaign (64/70 responses, mechanism-catalogue.md:408-415). Fit: Floor guard under ADR 0071 (a)(b)(c) — fires only on the failure shape, native profile only (markdown-fenced/custom-regex parse content themselves), config key tool-call-salvage, debug-view notice only.
