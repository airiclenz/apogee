---
title: No test pins the rendered timeout_seconds schema text for terminal and python_exec
status: done
type: task
priority: 3
plan: 2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan
aliases: [apogee-tool-schema-render-untested]
created: 2026-09-23T04:59:01Z
closed: 2026-09-23T06:55:57Z
reason: "Item 3: schema timeout figures pinned by tests; resolved by plan '2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan' (archived at 152dc896)"
---

Item 6 of 'docs/plans/2026-09-22 - 01 - slow-box-deadlines-plan.md' made internal/tools/terminal.go and internal/tools/python_exec.go RENDER their timeout_seconds descriptions from subprocess.DefaultSubprocessTimeout and subprocess.MaxSubprocessTimeout (fmt.Sprintf) instead of restating the numbers by hand, so the model cannot be told a figure the funnel does not apply. No shipped test asserts the rendered schema text, so a hand-written revert to literal numbers would pass silently. Remedy: a test that asserts the rendered description contains the two constants' values.
