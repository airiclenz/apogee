---
title: ADR 0081 context
---

Linux confinement has been landlock and nothing else since P3.2: `internal/platform/confiner_linux.go`
returned the landlock backend unconditionally, and a kernel that could not answer
`landlock_create_ruleset` reported `{FSWrite:false, NetworkEgress:false}`. Under
ADR 0012 that is
correct — "confine if you can, gate if you can't" — but it means `--mode auto` on such a host gates
**every** terminal command for approval, and the degradation notice fires on every session. Two host
shapes hit it in practice:

- **A kernel that ships without the landlock LSM on its boot line.** Raspberry Pi OS is the standing
  example: the 6.x kernel has landlock compiled in but `lsm=` omits it, so the probe returns
  `EOPNOTSUPP` ("operation not supported") — and the fix is a boot-line edit to the user's kernel
  configuration, which apogee has no business asking for.
- **A container whose seccomp profile refuses the syscall.** The issue register recorded this shape on
  2026-07-21: `landlock_create_ruleset` returns `ENOSYS` ("function not implemented") under a runtime
  whose default profile predates the syscall, and the process cannot tell that from a kernel without
  the facility.

Both hosts have what landlock's absence does not take away: **unprivileged user namespaces**. A user
namespace plus a mount namespace whose root is a read-only bind of `/`, with the box's writable roots
bound read-write over it, fences writes at subprocess granularity exactly as landlock and seatbelt do
— deny-default for writes, reads and exec untouched, the parent never restricted. What was missing was
a backend that builds that box, an honest answer to *why* a host cannot fence, and a selector that
tries the second mechanism before giving up.

Two constraints shaped how the box is built. Entering a user namespace needs `CLONE_NEWUSER`, which
the kernel refuses to a multithreaded process — and a CGO-free Go program is multithreaded from its
first instruction, with no hook between `fork` and `execve` to unshare in (the constraint that already
made landlock a re-exec helper). And the host that needs this backend most is the one least likely to
have a rebuilt kernel, so the mechanism has to be something the distribution already ships.
