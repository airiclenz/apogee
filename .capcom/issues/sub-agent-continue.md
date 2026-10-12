---
title: "sub_agent continue: \"<name>\" restarts a capped delegate from its engine fold (P6)"
status: done
type: feature
priority: 2
plan: 2026-09-18 - 00 - capped-delegate-report-loss-plan
aliases: [apogee-mgp]
created: 2026-09-18T21:03:32Z
closed: 2026-09-19T09:57:15Z
reason: Shipped by plan 2026-09-18 - 00 (commits 3163948f..3e5a017a); CHANGELOG [Unreleased] carries the entries
---

The parent retains {task, name, tools, output_path, fold, closing report} per capped child in memory until its Exchange ends; continue spawns a fresh child from task + fold with a fresh cap (min(max_steps, delegate-max-steps)); by name, latest wins. ADR 0013 §5 / 0022 D8 addenda. Plan items 8–10. Source: docs/handoffs/2026-09-18 - 00 - capped-delegate-report-loss.md F4, P6; overlaps apogee-clb.
