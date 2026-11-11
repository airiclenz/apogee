---
title: ADR 0112 context
---

The 2026-11-10 code audit (`docs/reviews/code-audit-2026-11-10.md`, Medium, raised to High under
SECURITY.md's posture; issue `fence-escape-local-spawner`) found Auto's write fence escapable without
breaking it. A confined command may still `connect(2)` to a pathname AF_UNIX socket it can reach
through the read-only view of the host: landlock has no right for a pathname connect below ABI 9,
and bwrap's `--ro-bind / /` (ADR 0081) carries the sockets into the box, since a read-only bind
does not stop a connect. On macOS the seatbelt profile is `(allow default)` minus `file-write*`, so
`mach-lookup` reaches launchd and every Mach service.

A same-user service that spawns processes on request then writes for the command, outside the box:

- `systemd-run --user sh -c '… >> ~/.bashrc'` over the user bus (`/run/user/<uid>/bus`), on a stock
  systemd host;
- `docker run -v /:/host …` for a user in the docker group (`/var/run/docker.sock`, which resolves
  to `/run/docker.sock`), and the same through a rootless docker or podman socket;
- `launchctl submit` or `osascript` on macOS.

A prompt-injected model in Auto reaches each with one shell command. SECURITY.md promises that an
unsupervised run cannot write outside the workspace and scratch directory, and ranks a fence escape
as the highest-severity report.

What apogee disclosed did not match. ADR 0081's 2026-09-22 note named the reachability as one
network-egress residual, `connect(2) AF_UNIX`, and classed it "not write-class", so
`probe.ResidualNotice` filtered it out and no host was told at Auto start. Landlock disclosed it only
at ABI ≥ 4 (wherever `NetworkEgress` is true), seatbelt not at all. An environment scrub
(`DBUS_SESSION_BUS_ADDRESS`, `XDG_RUNTIME_DIR`, `DOCKER_HOST`) would hide the hint, not the socket: a
child names the path itself.

The kernel facts the decision rests on (https://docs.kernel.org/userspace-api/landlock.html):
`LANDLOCK_ACCESS_FS_RESOLVE_UNIX` is landlock ABI 9 — a ruleset that handles it refuses a pathname
AF_UNIX connect beneath any path it is not granted on; UDP network rights are ABI 10;
`LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET` (ABI 6) scopes abstract sockets to the sandbox's domain.
