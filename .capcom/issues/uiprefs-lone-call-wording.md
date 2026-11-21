---
title: UIPrefs.ToolsAutoCollapse comment still names the retired lone call
status: done
type: task
priority: 4
labels: [docs]
created: 2026-11-20T21:50:15Z
closed: 2026-11-21T09:48:45Z
reason: ToolsAutoCollapse comment rewritten in group-of-one vocabulary; no 'lone call' left in internal/domain/uiprefs.go
---

internal/domain/uiprefs.go:93-98 — the `ToolsAutoCollapse` field comment still says "a Tools umbrella or a lone groupable call to its Group summary" and "Off, every umbrella follows the size rule above alone and a lone call stays open". The tool-group-of-one plan (docs/plans/archived/2026-11-20 - 00 - tool-group-of-one-plan.md) made every tool call a group (a single call is a group of one, ADR 0035 amendment adr-0035-amend-1120c) and retired the lone-call fold path; the comment remains true but uses the retired term. Deferred from that run because internal/domain was outside its items' files.

## Why

Code comments speak the shipped vocabulary; the retired "lone call" term misleads a reader into looking for a fold path that no longer exists.

## Acceptance

- internal/domain/uiprefs.go's ToolsAutoCollapse comment describes a single call as a group of one and no longer uses "lone call"
