---
title: ADR 0112 decision
---

1. **The fence does not cover same-user spawning services; the posture is close where the backend
   can, disclose everything else as write-class.** A service that runs code for whoever reaches its
   socket is a write outside the box, so reaching it is a write-class residual, not a network one.
   Nothing is refused on account of it: Auto still runs on every backend that fences the
   filesystem, and the operator is told what the fence does not stop.

2. **One closed set, shared by both Linux rungs.** Where a Linux backend closes anything, it closes
   exactly this, and nothing else:
   - the user runtime dir(s) — `/run/user/<uid>` and `$XDG_RUNTIME_DIR` where it names another,
     validated before use — which carry the session bus, the `systemd --user` private socket and
     rootless docker and podman sockets;
   - the system docker and podman sockets (`/run/docker.sock`, `/run/podman/podman.sock`;
     `/var/run` resolves to `/run`, so `/run` is the canonical spelling).

   Inside a closed runtime dir an **agent-socket allowlist** stays reachable: the ssh-agent socket
   (`$SSH_AUTH_SOCK`) and the gpg-agent sockets, so `git push` over ssh and commit signing keep
   working. Nothing else is re-opened.

3. **bwrap** puts `--tmpfs` over the user runtime dir(s), re-binds only the allowlisted agent
   sockets beneath it, and masks the system docker and podman sockets. It still discloses
   `connect(2) AF_UNIX`, because every other pathname socket stays reachable (tmux's under `/tmp`,
   postgres's, the system bus).

4. **landlock, ABI ≥ 9** handles `LANDLOCK_ACCESS_FS_RESOLVE_UNIX` and mirrors bwrap's closed set: it
   grants the right beneath every entry of `/` and of `/run` except the masked paths, recursing down
   a masked path's ancestors to grant their siblings and never granting an ancestor itself, and
   re-grants the agent-socket allowlist beneath a masked dir. One pure rule planner computes the
   grants, from inputs read in the parent and carried in the encoded box, never from the child's
   environment. It still discloses `connect(2) AF_UNIX`, for the same reason as bwrap.

5. **landlock, ABI 1–8** cannot close a pathname AF_UNIX connect. `connect(2) AF_UNIX` is disclosed
   as a write-class residual on every ABI, 1–3 included, not only where `NetworkEgress` is true. The
   rung order — landlock first, bwrap second (ADR 0081) — is unchanged: a host on landlock ABI < 9
   is not moved to bwrap to gain the closed set.

6. **Abstract sockets stay reachable on both Linux rungs.** No `LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET`
   and no network namespace is added for it: scoping breaks X11's abstract socket, which `xclip`,
   `xdg-open` and headed browser tests need. The `connect(2) AF_UNIX` disclosure covers them.

   > **Amended 2026-11-11 (closeout of `docs/plans/archived/2026-11-11 - 01 - fence-escape-and-open-defects-plan.md`).**
   > "Both Linux rungs" holds for Auto's default open network only. A bwrap box that opts into
   > network-deny runs under `--unshare-net`, and that fresh network namespace cuts abstract AF_UNIX
   > sockets along with TCP (ADR 0081, 2026-09-22 note). This record adds no namespace of its own;
   > the open-network box keeps abstract sockets reachable.

7. **Seatbelt discloses only.** Its residuals gain `connect(2) AF_UNIX` and `mach-lookup`, both
   write-class; the profile is unchanged.

8. **Windows token backend: undecided, filed.** Whether its Low-integrity restricted token reaches a
   same-user named pipe or COM server that spawns processes is not established; issue
   `windows-token-spawner-posture` owns the answer. Until it lands this record claims nothing for
   that backend.

9. **Localhost TCP stays open** under ADR 0012's open-network default, so a confined command can
   reach a same-user service listening on loopback. It is disclosed in prose — the manual,
   SECURITY.md and this record — not as a residual token.

10. **No environment scrub.** `DBUS_SESSION_BUS_ADDRESS`, `XDG_RUNTIME_DIR` and `DOCKER_HOST` stay in
    the confined environment: dropping them hides a hint, not a socket.

11. **The write-class class is backend-neutral.** `connect(2) AF_UNIX` and `mach-lookup` join
    `truncate(2)` in `probe.ResidualNotice`, so `apogee probe`'s `unfenced:` field, the Auto startup
    notice, `docs/manual/configuration.md` §Auto mode's blast radius and SECURITY.md all name them
    with the spawn-elsewhere consequence. `connect(2) UDP` stays network-class.
