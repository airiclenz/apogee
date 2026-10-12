---
title: terminal output from Windows console programs decoded as UTF-8 (OEM codepage mojibake)
status: done
type: bug
priority: 2
labels: [windows]
plan: 2026-10-11 - 01 - p1-p2-bugs-plan
aliases: [apogee-terminal-oem-mojibake]
created: 2026-10-11T18:16:26Z
closed: 2026-10-11T21:53:34Z
reason: "Resolved by plan '2026-10-11 - 01 - p1-p2-bugs-plan' (items 9-10: terminal decodes non-UTF-8 output from the console codepage), archived at 3fb76cff"
---

`terminal` tool output from Windows console programs is decoded as UTF-8, so OEM-codepage bytes reach the model as U+FFFD.

**Seen:** session `20261011T131113Z-1ae692f2` (2026-10-11): `cmd /c "dir ..."` returned `83�293�634�560 bytes free` — cmd's OEM thousands separator decoded as invalid UTF-8.

**Cause:** child output is captured raw (`internal/subprocess/subprocess.go`, `cmd.Stdout = &out` ~:274) and stringified with `out.String()` (`subprocess.go:368`); `subprocessToolResult` (`internal/tools/terminal.go:390`) passes it on unchanged. No codepage handling exists anywhere (no `chcp`, `65001`, `GetConsoleOutputCP`, `GetOEMCP`). Any non-ASCII output from cmd and other OEM-codepage tools (localised messages, paths with umlauts) is corrupted the same way.

**Acceptance:** on Windows, output that is not valid UTF-8 is decoded from the console output codepage (falling back to `GetOEMCP`) before it reaches the model; valid UTF-8 passes untouched. Test feeds a CP850/CP437 byte string and checks the decoded text.
