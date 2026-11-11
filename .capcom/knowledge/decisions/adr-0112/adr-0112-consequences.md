---
title: ADR 0112 consequences
---

- **Every Linux and macOS host gains an Auto startup notice.** `connect(2) AF_UNIX` (and on macOS
  `mach-lookup`) is write-class wherever a filesystem-fencing backend runs, so `probe.ResidualNotice`
  words it at Auto start on landlock at every ABI, on bwrap and on seatbelt. The drift tests that pin
  the disclosed residual tokens and their wording follow the change.
- **Docker and podman stop working inside Auto's box** wherever the backend closes their sockets
  (bwrap, landlock ≥ 9): `docker` and `podman` commands fail there, and the manual says so. Run them
  in another mode, or with `/confine off` on a disposable machine.
- **Masking the runtime dir has collateral beyond docker.** The session bus and the IPC sockets under
  it go too, so `gh` with a keyring-stored token, `git-credential-libsecret` and VS Code's git askpass
  (`$VSCODE_GIT_IPC_HANDLE`) cannot reach their helpers, and `git push` through them and `gh pr` fail
  in Auto's box. The same holds for any other client of a runtime-dir socket, such as a native Wayland
  client. The behaviour stays as ratified; the manual and SECURITY.md name it. `git push` over ssh
  and signing keep working through the agent-socket allowlist.
- **What stays reachable stays disclosed.** On bwrap and landlock ≥ 9: every other pathname socket
  (tmux under `/tmp`, postgres, the system bus) and every abstract socket, X11's included. On
  landlock < 9: every pathname and abstract socket. On seatbelt: every socket and every Mach service.
  A spawning service among them is still an escape; the notice and the docs say so.
- **The implementation is the plan's items 5–10**: the write-class reclass and its wording, the
  per-backend disclosure, a `confinetest` row for a confined connect to a unix socket outside the
  box, the bwrap mask, the landlock `RESOLVE_UNIX` planner, and the manual and SECURITY.md prose.
- **Two questions are filed, not decided:** `landlock-udp-abi10` (landlock ABI 10 handles UDP, so the
  "no kernel closes" claims about `connect(2) UDP` stop being true there) and
  `windows-token-spawner-posture` (decision 8).
