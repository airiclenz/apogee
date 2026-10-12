---
title: read_file locate windows carry no line or byte cap
status: done
type: bug
priority: 3
labels: [plan-2026-09-14-02, run-residual]
aliases: [apogee-8as]
created: 2026-09-15T11:49:40Z
closed: 2026-09-16T17:54:58Z
reason: "fix(tools): read_file caps locate windows and the hit list like a plain read"
---

Deferred from plan 2026-09-14 - 02 item 6. A range-less `locate` with hundreds of hits still renders every merged ±10-line window with no line/byte cap on the windowed body — only the plain (no-locate) read is bounded by the 400-line / 40 KiB default. internal/tools/read_file.go:229 (the locate branch bypasses the default cap), internal/tools/read_file.go:321 (locateWindows renders every merged window).
