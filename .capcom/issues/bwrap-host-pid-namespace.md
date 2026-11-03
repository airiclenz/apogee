---
title: bwrap backend shares the host PID namespace, so a confined command can write outside the box and ptrace apogee
status: done
type: bug
priority: 1
labels: [security]
plan: 2026-11-03 - 00 - code-audit-fixes-plan
created: 2026-11-03T14:20:29Z
closed: 2026-11-03T16:07:40Z
reason: Resolved by plan '2026-11-03 - 00 - code-audit-fixes-plan' (archived in c3346711)
---

From docs/reviews/code-audit-2026-11-03.md (High); fixed by item 1 of `docs/plans/2026-11-03 - 00 - code-audit-fixes-plan.md`. internal/platform/namespace_linux.go:348 runs bwrap without `--unshare-pid` while `--proc /proc` shows the host process table, and the child keeps apogee's uid. A confined command can open `/proc/<apogee-pid>/root/<path>` and write outside the box; with `kernel.yama.ptrace_scope=0` (Debian / Raspberry Pi OS default) it can also ptrace or write `/proc/<pid>/mem` of apogee. ADR 0081 and docs/design/confinement-execution-contract.md:232 record the no-`--unshare-pid` trade-off without this consequence.

## Why

A prompt-injected model in Auto mode on the bwrap rung defeats the workspace-write-only fence and can inject code into the unconfined parent.

## Acceptance

- bwrap runs with `--unshare-pid`; a host refusing CLONE_NEWPID falls to the next rung.
- `echo x > /proc/<apogee-pid>/root/tmp/escape-probe` inside a confined run fails.
- ADR 0081 and the confinement contract are amended; no doc or comment still forbids the flag.
