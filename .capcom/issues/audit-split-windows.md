---
title: "Shipped /audit fails on Windows: split stage needs sh"
status: done
type: bug
priority: 2
plan: 2026-10-10 - 01 - audit-split-windows-plan
aliases: [apogee-audit-split-windows]
created: 2026-10-10T17:35:36Z
closed: 2026-10-11T09:18:07Z
reason: "Resolved by plan 2026-10-10 - 01 - audit-split-windows-plan (archived at 6d0e947d): split ported to Go as hidden apogee audit-split, {apogee} placeholder, platform quoting for cmd, LF pin + Windows CI"
---

On Windows a recipe script stage runs through cmd /c. With Git for Windows' default install, sh is not on PATH, so the audit split stage exits 9009 (split.status=blocked) and every later stage is skipped. Behind that failure: shellQuote sends POSIX single quotes to cmd, split.sh writes MSYS /c/... paths that Go reads as relative, and internal/skills/shipped/** has no LF pin. Fix plan: docs/plans/2026-10-10 - 01 - audit-split-windows-plan.md
