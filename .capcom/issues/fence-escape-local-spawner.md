---
title: Auto's write fence is escapable through a same-user service that spawns processes (user systemd, dockerd, launchd)
status: done
type: bug
priority: 1
labels: [security]
created: 2026-11-10T19:37:31Z
closed: 2026-11-11T14:27:07Z
reason: "Resolved by plan '2026-11-11 - 01 - fence-escape-and-open-defects-plan' items 4-10 (archived at d2bc5fcb): ADR 0112 posture; AF_UNIX write-class at Auto start on every landlock ABI, seatbelt AF_UNIX+mach-lookup disclosed; bwrap and landlock ABI>=9 mask runtime dir and container sockets; confinetest unix-connect row; docs and SECURITY.md disclose. Landlock RESOLVE_UNIX live path is exercised only on an ABI>=9 CI kernel (confinetest row #15)."
---

Source: docs/reviews/code-audit-2026-11-10.md (Medium, "The Auto write fence is escapable through reachable local services"). Verified VALID, severity raised to High under SECURITY.md's posture, 2026-11-10 at a5c4f2db.

Related, closed: `net-deny-egress-gap`, which disclosed the same socket reachability as a network-deny egress gap only. The report's line refs for namespace_linux.go and seatbelt.go were stale; corrected below.

**Where:** `internal/platform/landlock_linux.go:236-250` (`Capabilities`; `connect(2) AF_UNIX` disclosed only at ABI ≥ 4), `internal/platform/namespace_linux.go:273-294` (`--ro-bind / /` carries `/run/user/<uid>/bus` and `/var/run/docker.sock` into the box), `internal/platform/seatbelt.go:157-197` (`(allow default)` minus `file-write*` leaves `mach-lookup`/launchd open), `internal/probe/confinement.go:116-136` (`ResidualNotice` filters AF_UNIX out as "not write-class"), `docs/manual/configuration.md` §Auto mode's blast radius.

**What:** a confined Auto child can still `connect(2)` to path-addressed sockets (landlock does not handle pathname AF_UNIX connect on current ABIs; bwrap's read-only bind does not stop a connect), and on macOS reach launchd and other Mach services. A same-user service that spawns processes on request then runs code outside the fence: `systemd-run --user sh -c '…>> ~/.bashrc'` over the user bus; `docker run -v /:/host …` for a docker-group user; `launchctl submit` / `osascript` on macOS. The residual is disclosed only as a network-deny egress gap, classed "not write-class", not disclosed at all on landlock ABI 1-3 or seatbelt, and never reaches the Auto startup notice. No confined-env scrub drops `DBUS_SESSION_BUS_ADDRESS`, `XDG_RUNTIME_DIR` or `DOCKER_HOST` — but scrubbing alone would be cosmetic, since a child can name the socket path itself. ADR 0012's open-network default also lets a confined child reach localhost TCP services, which should be disclosed alongside.

**Why it matters:** SECURITY.md promises that an unsupervised run cannot write outside the workspace and scratch directory, and ranks a fence escape as the highest-severity report. A prompt-injected model in Auto reaches this with one shell command on a stock systemd host.

**Suggested fix:** decide the posture first (ADR, or amendment to ADR 0012/0081), then:
1. Disclose honestly now: classify `connect(2) AF_UNIX` (and a seatbelt `mach-lookup` token) as write-class so `ResidualNotice` words it at Auto start; disclose it on every landlock ABI; name the spawn-elsewhere consequence and the localhost TCP equivalent in the manual and SECURITY.md.
2. Close what can be closed without breaking common workflows: on bwrap overlay the user bus and docker socket (e.g. `--tmpfs /run/user/<uid>` or targeted binds) while keeping ssh-agent/gpg-agent sockets working for `git push` and signing, or disclose that they break; use landlock's pathname-unix restriction where the kernel offers it (`LANDLOCK_ACCESS_FS_RESOLVE_UNIX` at ABI ≥ 9, verify); on seatbelt deny `mach-lookup` of launchd job submission and AppleScript/LaunchServices.
3. Add an escape-probe row to `confinetest` that tries a session-bus connect from inside the box.

## Acceptance

- A decision record states whether Auto's fence covers same-user spawning services and what each backend does about them.
- Whatever stays open is disclosed as write-class on every backend and ABI where it applies: apogee probe's unfenced: field, the Auto startup notice, docs/manual/configuration.md §Auto mode's blast radius and SECURITY.md.
- Whatever the decision closes is closed, e.g. under bwrap a confined systemd-run --user true (or a connect to /run/user/<uid>/bus) fails, pinned by a confinetest row or a hermetic argv test.
- The drift tests that pin the disclosed residual tokens follow the change.
