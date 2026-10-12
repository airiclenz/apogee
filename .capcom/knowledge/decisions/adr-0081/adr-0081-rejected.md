---
title: ADR 0081 considered options
---

- **Rebuilding the kernel with landlock on the boot line.** Host-only: it fixes one machine, it is a
  boot-configuration edit the user must make and keep, and on Raspberry Pi OS it is lost on the next
  `apt upgrade` of the kernel package. It also does nothing for the container case. Rejected as the
  answer, though it remains the user's best option where they control the boot line — the
  `Unavailable` reason names landlock first so they can see it.
- **A native `CLONE_NEWUSER` re-exec, mirroring the landlock helper.** The cleanest long-term shape:
  no external binary, the same `__confined-exec` sentinel. Rejected **for now** because a CGO-free Go
  process cannot `unshare(CLONE_NEWUSER)` — the kernel refuses it to a multithreaded caller and the
  runtime is multithreaded before user code runs. The workable variants (`SysProcAttr.Cloneflags`
  with `UidMappings`/`GidMappings` on the child, then a helper that sets up the mounts in the new
  namespaces before `exec`) are a real design of their own; it is left as a follow-up, and nothing in
  this ADR forecloses it — the selector would gain a rung, the box shape would not change.
- **`--tmpfs /dev` with `/dev/null` bound in, to keep the exemption at exactly `/dev/null`.** It would
  have preserved contract §2.3 property 2 verbatim. Rejected: it breaks `/dev/tty` (any program that
  talks to its terminal), `/dev/urandom` (every TLS handshake, every `mktemp`) and `/dev/stdout`
  (the `>/dev/stdout` idiom), so ordinary tool calls fail inside the box for reasons that have nothing
  to do with confinement. bwrap's minimal set is the smallest one that leaves a shell usable, and each
  node in it is side-effect-free or the child's own terminal; the contract is amended rather than the
  box crippled.
- **A config key to force a backend.** Rejected: the selector's order is a decision, not a preference,
  and a key that picks the weaker fence on a landlock host is a footgun with no use case. "Which
  backend do I have and why" is answered by `probe host`, not by a setting.
- **Reporting `NetworkEgress: false` to match landlock below ABI 4.** Rejected: capabilities describe
  what the backend **can enforce** (contract §5), and `--unshare-net` enforces deny-all. Claiming
  false would gate every network-deny box on a host that could have confined it.
