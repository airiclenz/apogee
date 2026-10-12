---
title: wrapUpCalls keeps every write_file call in the wrap-up reply, so two writes to the output path both run
status: done
type: bug
priority: 3
aliases: [apogee-mfh]
created: 2026-09-15T14:54:18Z
closed: 2026-09-16T17:43:55Z
reason: "fix(agent): the wrap-up keeps one write_file to the output path, not every one"
---

Plan 2026-09-14 - 03 item 8 residual. wrapUpCalls (internal/agent/subagent.go:357) keeps EVERY write_file call in the wrap-up reply, so two writes to the output path both run although the wrap-up directive says the path may be written once. Cosmetic — the second write overwrites the first; no test drives it.
