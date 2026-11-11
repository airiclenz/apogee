---
title: Can the Windows token backend's Low-integrity child reach a same-user named pipe or COM server that spawns processes?
status: done
type: task
priority: 2
labels: [security]
created: 2026-11-11T13:13:42Z
closed: 2026-11-11T20:04:33Z
reason: Established and disclosed. Probe confirms the Low restricted-token child cannot reach the mainstream same-user spawners (Task Scheduler COM, WMI Win32_Process.Create both refuse it; openable pipes are not spawners); OS default integrity policy holds the fence, token closes nothing itself. Docker Desktop/WSL spawner-pipes disclosed as write-class residual (analogue of Linux docker socket). ADR 0112 decision 8 amended, SECURITY.md + manual + CHANGELOG updated.
---

Source: ADR 0112 decision 8 (`.capcom/knowledge/decisions/adr-0112.md`), filed by item 4 of `docs/plans/2026-11-11 - 01 - fence-escape-and-open-defects-plan.md`; out of that plan's scope.

**What:** on Linux and macOS a confined Auto command escapes the write fence by reaching a same-user service that spawns processes on request (user systemd, dockerd, launchd; issue `fence-escape-local-spawner`). The Windows token backend (`internal/platform/wintoken_windows.go`, `internal/platform/confiner_windows.go`; ADR 0020, confinement-execution-contract §9) runs the command under a restricted, Low-integrity primary token. Whether that token can still open a same-user named pipe or activate a COM server (or reach the Task Scheduler, WMI, or a Docker Desktop named pipe) that runs code at the user's Medium integrity is not established, so ADR 0112 claims nothing for this backend.

**Why it matters:** if such a service is reachable, Auto's fence on Windows is escapable the same way, and SECURITY.md's promise is undisclosed there.

## Acceptance

- A recorded answer (an ADR 0112 amendment) states whether a Low-integrity restricted-token child can reach a same-user named pipe or COM server that runs code at Medium integrity, with the evidence.
- Whatever is reachable is closed or disclosed as a write-class residual on the Windows backend, and the manual and SECURITY.md say so.

## Log

- 2026-11-11 Airic Lenz: Probe (scratchpad, mints the verbatim restricted Low token): at Low integrity the child cannot reach a same-user spawner that runs at Medium. Task Scheduler COM (Schedule.Service.GetFolder) fails 0x80070003; WMI Win32_Process.Create denied 0x80041003; direct profile write denied (fence confirmed). 7/77 named pipes openable RW (lsass/srvsvc/wkssvc/search/crashpad) but none is a process-spawner. Box has no Docker/WSL, so the docker_engine/WSL spawner-pipe vector is untested-because-absent and must be disclosed as write-class, mirroring Linux /run/docker.sock.
