---
title: python_exec picks the WindowsApps Store stub over a real Python
status: done
type: bug
priority: 2
labels: [windows]
plan: 2026-10-11 - 01 - p1-p2-bugs-plan
aliases: [apogee-python-exec-store-stub]
created: 2026-10-11T18:16:15Z
closed: 2026-10-11T21:53:33Z
reason: "Resolved by plan '2026-10-11 - 01 - p1-p2-bugs-plan' (item 8: python_exec skips a candidate whose version probe fails), archived at 3fb76cff"
---

`python_exec` runs the Microsoft Store App Execution Alias stub on Windows instead of a real interpreter.

**Seen:** session `20261011T131113Z-1ae692f2` (2026-10-11, Windows 11): `python_exec` failed with exit 9009, "Python was not found; run without arguments to install from the Microsoft Store…".

**Cause:** `pythonCandidates = {"python3", "python"}` (`internal/tools/python_exec.go:46`); the first name found on PATH wins (`python_exec.go:244-253`). On this box `where python3` returns only `%LOCALAPPDATA%\Microsoft\WindowsApps\python3.exe` (the stub), while `where python` lists a real `Python314-arm64\python.exe` first. Nothing detects the stub (no `WindowsApps` / `IO_REPARSE_TAG_APPEXECLINK` check), and the tool is registered unconditionally (`internal/tools/registry.go:309`), so a box with a working Python still gets a broken `python_exec`.

**Acceptance:** on Windows, a candidate that resolves to a WindowsApps App Execution Alias stub is skipped (or a failed version probe disqualifies that candidate) and the next candidate is tried; with the PATH above, `python_exec` runs the real `python.exe`. Test pins the stub skip.
