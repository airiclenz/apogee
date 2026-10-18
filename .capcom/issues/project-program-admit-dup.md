---
title: Pinned project-program admission is duplicated in mcp and tools
status: open
type: task
priority: 4
created: 2026-10-18T19:13:08Z
---

The check that admits a pinned in-root project program past the exec fence exists twice, line for line:
`isPinnedProgram` (`internal/mcp/transport.go:411`) and `isAdmittedProgram` (`internal/tools/exec_common.go:393`).
The resolve-then-admit wrappers around them repeat the same shape too: the repo-server resolve in
`internal/mcp/transport.go` and `resolveHookProgram` (`internal/tools/exec_common.go:378`) both call
`security.ResolveProgram` and admit on `security.ErrExecFromWritablePath`. That is about 15 lines. `mcp` cannot import
`tools`, so the copy was made rather than shared. Both copies guard the same security fence
(ADR 0104: only the exact pinned file may run), so a fix to one, such as a symlink or relative-path edge,
can silently miss the other.

Deferred out of plan `2026-10-18 - 00 - project-config-exec-keys-plan` (item 7's verifier, coding-standards dedupe rule).

## Acceptance

- One shared helper in `internal/security` (e.g. `AdmitPinnedProgram(program string, pinned []string) bool`, or a
  resolve-and-admit wrapper) serves both the MCP stdio launch and the reaction `run:` spawn.
- `isPinnedProgram` and `isAdmittedProgram` are gone, and both call sites use the shared helper.
- The existing admission tests in `internal/mcp` and `internal/tools` pass unchanged, and the shared helper has its own
  table test (absolute vs relative, symlinked root, not-pinned).
