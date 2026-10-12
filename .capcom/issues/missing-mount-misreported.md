---
title: missing mount root misreported as a workspace-fence refusal
status: open
type: bug
priority: 3
plan: 2026-10-12 - 00 - regression-and-doc-truth-plan
aliases: [apogee-missing-mount-misreported]
created: 2026-10-11T18:16:44Z
---

A path under a mounted read root that does not exist yet is refused as "path resolves outside the workspace root" instead of reported as not found.

**Seen:** session `20261011T131113Z-1ae692f2` (2026-10-11): `list_dir` on `C:\Users\airic\Repos\dotfiles\apogee\skills` (the resolved `~/.apogee/skills` mount) returned `security: path resolves outside the workspace root`, because the folder did not exist at that moment. Minutes later, once it existed, `find_files` and `read_file` on the same path succeeded. The model read the refusal as a fence and went looking elsewhere.

**Cause:** `matchRoot` (`internal/tools/path_read.go:498-513`) skips a mount root that is not usable (`rootUsable`), so `readScope.resolve` falls through and returns the workspace-fence error (`path_read.go:256`). The tools are consistent with each other; the wording is wrong for this case.

**Acceptance:** a path inside a configured mount root (skills dir, session scratch) whose root is missing gets a not-found answer naming the path, not the workspace-fence refusal; paths outside every root keep the fence refusal. Test pins both.
