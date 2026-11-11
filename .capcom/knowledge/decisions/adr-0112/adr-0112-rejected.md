---
title: ADR 0112 rejected
---

- **Keep the token network-class.** It is what ADR 0081's 2026-09-22 note did, and it left SECURITY.md's
  promise silently false on every host: a spawning service turns a reachable socket into a write.
- **Scope abstract sockets** (`LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET`, or a network namespace on the
  landlock rung). It closes the abstract half but breaks X11's abstract socket, and with it `xclip`,
  `xdg-open` and headed browser tests — common workflows an agent runs in Auto.
- **Deny every pathname AF_UNIX connect.** It would close the whole class, but tmux, postgres, the
  ssh and gpg agents and many dev tools talk over pathname sockets; Auto would stop being usable for
  ordinary work.
- **Mask the runtime dir without an allowlist.** It breaks `git push` over ssh and commit signing,
  which an agent does constantly; the ssh-agent and gpg-agent sockets do not spawn arbitrary
  processes for a caller.
- **Move landlock hosts below ABI 9 to the bwrap rung**, or refuse Auto there, to gain the closed set.
  It changes ADR 0081's rung order for one residual and gates Auto on most shipping kernels; the
  disclosure covers that gap honestly.
- **Change the seatbelt profile** to deny `mach-lookup` of launchd job submission and
  AppleScript/LaunchServices. Which Mach services a denial may cut without breaking ordinary tools
  is unmapped, and a profile change is out of this record's scope; disclosure is the honest floor.
- **Scrub the confined environment.** Cosmetic: a child names the socket path itself.
- **Close localhost TCP.** ADR 0012 keeps the network open in Auto by design; reversing that is a
  separate decision, and the residual is disclosed instead.
