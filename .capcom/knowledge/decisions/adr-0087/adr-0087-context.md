---
title: ADR 0087 context
---

The handoff `docs/handoffs/2026-09-26 - 00 - engine-run-sub-agent-orchestration-planning.md`
(bead `apogee-engine-run-delegation`) sketched engine-run orchestration, and the owner's grilling
session of 2026-09-27 settled it.

Today a parent delegates with one `sub_agent` call per helper, reads every helper's full report
into its own context, and holds the multi-stage plan itself. The owner's skills (`code-audit`,
`release-notes`, `security-audit`, `implement-plan`) are hand-written orchestrations in exactly that
shape: a context-greedy coordinator dispatches "read this prompt" briefs, routes on six-line
receipts, never opens the output files, retries once, and resumes by skipping phases whose output
exists. Two costs follow. The coordinator's window fills with reports. And a small model cannot hold
the plan: the `-sequential` skill variants exist because a 4B–35B coordinator stalls between phases,
so they drop adversarial verification, rollups and machine checks outright.

The owner's standing direction: sub-agents exist for **context management, not speed**, for local
and frontier models alike, and old decisions are retired when they block the best design.

ADR 0031 invariant 4 already asks for a "thin workflow layer" as an embeddable library the bench can
drive, and ADR 0034 reserved `run: workflow:` for it. No record rejected the idea.
