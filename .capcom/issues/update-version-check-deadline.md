---
title: apogee update fails a slow first exec of the staged binary
status: open
type: bug
priority: 3
labels: [slow-box]
plan: 2026-11-13 - 00 - urgent-defects-and-doc-truth-plan
created: 2026-11-13T09:11:48Z
---

cmd/apogee/update.go stagedVersionTimeout is a fixed 30 s. A cold first exec under load (SD card, zram, Defender scan) fails the update as 'the downloaded binary did not run: signal: killed'. Violates the slow-box rule in AGENTS.md.

## Acceptance

- A 5-minute backstop; a timeout says 'did not answer --version within 5m0s; the installed binary is unchanged'
