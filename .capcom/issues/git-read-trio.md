---
title: git read trio (git_status/git_log/git_diff_range) runs in every agent mode via a read-only-subprocess class
status: done
type: feature
priority: 2
plan: 2026-09-06 - 04 - read-only-subprocess-class-plan
aliases: [apogee-7pj]
created: 2026-09-06T19:39:08Z
closed: 2026-09-07T10:39:00Z
reason: "Delivered by plan 'docs/plans/archived/2026-09-06 - 04 - read-only-subprocess-class-plan.md' (4/4 items DONE 2026-09-07, archived 99ff4b4c): --ignore-submodules=dirty (852cf6e5), internal/tools/readonly_subprocess.go marker + tools.IsReadOnlySubprocess (a5e3a67a), classReadOnlySubprocess taking the RO row in every mode (2c2f6429), contract §4 / ADR 0012 amendment / CONTEXT / manual (b9c0dae7). Shipped in v0.20.9."
---

git_status, git_log and git_diff_range are refused in Plan and gated in Ask-Before/Allow-Edits because the Subprocess marker outranks their ReadOnly declaration (contract §4, 2026-07-26). Every hardening that closed 'a subprocess is unbounded' for these three landed after that rule. Give them an unexported readOnlySubprocess marker and a class that takes the RO row in every mode; close the submodule child-spawn residual with --ignore-submodules=dirty. Designed in docs/plans/2026-09-06 - 04 - read-only-subprocess-class-plan.md (3 owner calls ratified 2026-09-06).
