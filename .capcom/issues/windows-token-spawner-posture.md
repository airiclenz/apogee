---
title: Can the Windows token backend's Low-integrity child reach a same-user named pipe or COM server that spawns processes?
status: open
type: task
priority: 2
labels: [security]
created: 2026-11-11T13:13:42Z
---

Source: ADR 0112 decision 8 (`.capcom/knowledge/decisions/adr-0112.md`), filed by item 4 of `docs/plans/2026-11-11 - 01 - fence-escape-and-open-defects-plan.md`; out of that plan's scope.

**What:** on Linux and macOS a confined Auto command escapes the write fence by reaching a same-user service that spawns processes on request (user systemd, dockerd, launchd; issue `fence-escape-local-spawner`). The Windows token backend (`internal/platform/wintoken_windows.go`, `internal/platform/confiner_windows.go`; ADR 0020, confinement-execution-contract §9) runs the command under a restricted, Low-integrity primary token. Whether that token can still open a same-user named pipe or activate a COM server (or reach the Task Scheduler, WMI, or a Docker Desktop named pipe) that runs code at the user's Medium integrity is not established, so ADR 0112 claims nothing for this backend.

**Why it matters:** if such a service is reachable, Auto's fence on Windows is escapable the same way, and SECURITY.md's promise is undisclosed there.

## Acceptance

- A recorded answer (an ADR 0112 amendment) states whether a Low-integrity restricted-token child can reach a same-user named pipe or COM server that runs code at Medium integrity, with the evidence.
- Whatever is reachable is closed or disclosed as a write-class residual on the Windows backend, and the manual and SECURITY.md say so.
