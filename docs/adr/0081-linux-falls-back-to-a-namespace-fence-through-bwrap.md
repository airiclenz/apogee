---
Status: accepted
Amends: ADR 0042 §4 (Linux gains the same bounded exception macOS has), ADR 0056 D2 (the kill-on-denial signature learns EROFS)
---

# Linux falls back to a namespace fence through bwrap

## Context

Linux confinement has been landlock and nothing else since P3.2: `internal/platform/confiner_linux.go`
returned the landlock backend unconditionally, and a kernel that could not answer
`landlock_create_ruleset` reported `{FSWrite:false, NetworkEgress:false}`. Under
[ADR 0012](0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md) that is
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

## Decision

**1. The Linux selector has two rungs, tried in order: landlock, then namespace.** `NewConfiner()`
constructs the landlock backend first; if it can fence writes (ABI ≥ 1) it is returned and the
namespace backend is never built. Otherwise the namespace backend is constructed — its probe forks
`bwrap` once for real, so the rung is only climbed when landlock has already said no — and returned
if it can fence. **Landlock wins whenever both would work**: it is kernel-enforced on the child's own
process, needs no external binary, and fences network egress per-host from ABI 4, where the namespace
backend's network tightening is all-or-nothing. When **neither** fences, the namespace backend is
returned anyway, carrying **both reasons** in `Capabilities().Unavailable` (`landlock unavailable
(landlock_create_ruleset: operation not supported); bwrap not on PATH`), so every wording surface
names what would have to change on this host. There is **no config key**: the backend is
auto-selected only; "configurable" means the selector has two Linux rungs, nothing more. The backend
type is `namespaceConfiner`, its label is `namespace` (`probe.BackendName`), and `NewReportConfiner()`
stays `NewConfiner()` verbatim on Linux — the probe launches a no-op shell and touches no disk.

**2. The launcher is bubblewrap (`bwrap`), an optional external enhancement in the ADR 0042 pattern.**
`bwrap` is resolved on `PATH` once at construction and never re-queried; absent, the backend fences
nothing and says so. This is the **second bounded exception** to
[ADR 0042](0042-external-programs-are-optional-enhancements-never-prerequisites.md)'s "external
programs are never prerequisites": macOS had the only one (`/usr/bin/sandbox-exec`), and Linux now has
one too — bounded the same way. It buys a **mode, not the agent**: a host without `bwrap` still runs
Plan, Ask-Before and Allow-Edits under the confine-if-you-can/gate-if-you-can't net, so the missing
program costs autonomy, not function. And it is reached only on the second rung: a landlock-capable
kernel never looks for it. `bwrap` is chosen because it is small, single-threaded, setuid-free, present
on every mainstream distribution (it is Flatpak's own sandbox), and built to do exactly this — a
native re-exec is a later, separate decision (see *Considered options*). No new module dependency is
taken; the backend is `os/exec` and argv.

**3. The box shape is fixed.** `Confine` rewrites the command to run under

```
bwrap --ro-bind / / --dev /dev --proc /proc --die-with-parent [--unshare-net]
      --bind <root> <root> …  --  <original cmd.Path> <original args…>
```

- `--ro-bind / /` — the whole filesystem, read-only: deny-default for writes; reads and exec stay
  open, because the box bounds where a confined child may **write**, like every other backend.
- `--dev /dev` — bwrap's minimal device set (`null`, `zero`, `full`, `random`, `urandom`, `tty`,
  `ptmx`, a fresh `devpts`, a private `tmpfs` at `/dev/shm`, the child's own controlling terminal at
  `/dev/console`). This is the backend's **write-exempt set** — wider than landlock's and seatbelt's
  exact `/dev/null`, and the contract's §2.3 property 2 is amended to say so. Each node is either
  side-effect-free (a sink, a source, a private scratch mount) or the terminal the child already
  owns, so the exemption widens nothing the box protects.
- `--proc /proc` — a fresh procfs, so the child sees its own process tree.
- `--die-with-parent` — the kernel delivers `SIGKILL` to the child the moment `bwrap` dies. Together
  with `Setpgid` (bwrap and its child share the process group the execution tool kills by negative
  PID, contract §2.4) teardown is two-sided: a child that escaped the group cannot outlive its
  launcher.
- `--unshare-net` — **only** when the box opts into network-deny via a non-empty `NetworkAllow`
  (ADR 0012: the network is open by default; deny is a coarse tightening). An empty network
  namespace with only a loopback is the same deny-all landlock ABI 4 enforces; a per-host allow list
  is a later additive change.
- `--bind <root> <root>` for `WorkspaceRoot` and each `WritablePaths` entry, canonicalised and bound
  read-write over the read-only root. A root that does not exist is skipped (bwrap refuses to bind a
  missing source; landlock's `ENOENT` rule) and stays read-only inside the box — fail-closed.

Never `--new-session` (it would detach the child from the terminal the tool drives) and never
`--unshare-pid` (a PID namespace would hide the child's process group from the teardown kill). The
flag line is a **pure function of the box** and is unit-tested as argv with no process, exactly as
seatbelt's profile string is.

**4. The construction probe launches bwrap for real.** "bwrap is installed" is not "bwrap can fence
here": kernels and profiles that refuse `CLONE_NEWUSER` to an unprivileged process
(`kernel.apparmor_restrict_unprivileged_userns`, a seccomp filter, `user.max_user_namespaces=0`)
refuse it at run time, not at `PATH`-lookup time. So construction runs the platform shell's no-op
under `bwrap` with the exact flag line `Confine` would generate for a box rooted at the temp dir,
bounded by a timeout. Exit 0 ⇒ `{FSWrite:true, NetworkEgress:true, Residuals:nil}` — one launch
fences both the filesystem and, when the box asks, the network, and nothing is residual. Any failure
⇒ `{false, false}` with `Unavailable` set to `bwrap refused: <bwrap's last stderr line>` (e.g.
`bwrap: setting up uid map: Permission denied`) or `bwrap timed out`. The probe has no disk side
effect.

**5. Any run-time failure is `ErrConfinementUnavailable`, never an unfenced run.** `Confine` on a
backend whose probe failed returns `ErrConfinementUnavailable` carrying the reason, so the contract
§4 fallback forces the gate; the empty-argv guard runs first so that refusal reads the same on every
host. The backend never lets a command run outside the box because the box could not be built.

**6. The kill-on-denial signature learns EROFS** (amends
[ADR 0056](0056-terminal-fail-fast-and-session-scratch.md) D2, dated note in place). Under a
read-only bind an out-of-box create or truncate fails with `EROFS` — `Read-only file system` (libc)
/ `read-only file system` (Go) — not a permission errno, so without the third spelling the escape
battery's `chained_script_clobber_denied` probe could never stop under this backend. The spelling is
not keyed to a backend: a landlock- or seatbelt-confined run writing to a genuinely read-only mount
is now stopped as a denial too, intended.

**7. Capability honesty gains a *why*.** `domain.ConfinementCaps` carries `Unavailable string`, the
one short sentence naming why `FSWrite` is false on this host; `probe.CapabilityLine` renders it as
` · why: <reason>` while fs-write is unavailable, and the startup notice, `apogee probe host` and
`/confine` all speak it. A backend that cannot fence says what would have to change instead of
reporting a bare `false`.

## Considered options

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

## Consequences

- **Raspberry Pi OS and ENOSYS containers become Auto-eligible** wherever `bwrap` is present and
  unprivileged user namespaces work. The degradation notice vanishes there; where it persists, it now
  says why.
- **Linux carries the same bounded ADR 0042 exception macOS does.** The claim that Linux confinement
  "never reaches outside the binary" is true of the first rung only; ADR 0042 §4 carries a dated note.
- **The `/dev/null` exemption is no longer exactly `/dev/null` on every backend.** Contract §2.3
  property 2 now names the namespace backend's device set as its own write-exempt set. Reads were
  never gated; nothing changes for them.
- **The denial signature is wider.** A write to a genuinely read-only mount inside any confined run is
  now a kill-on-denial match (ADR 0056 D2, amended).
- **`ConfinementCaps` has a fourth field**, `Unavailable`, filled by both Linux backends when they
  cannot fence. The field and its rendering are backend-agnostic — seatbelt and the Windows token
  backend still report a bare `false` and may fill it later without a contract change.
- **The escape battery gains a third Linux driver.** `TestNamespaceProbe` runs rows #1–#6, #11 and #12
  and `TestNamespaceProbeNetwork` rows #7/#8 on a bwrap host; the landlock driver skips there and the
  namespace driver skips where landlock is available, so a single host proves one backend.
- **A native namespace launcher stays open** as a follow-up; `--unshare-pid`, a per-host network
  allow list and a backend-forcing config key are out of scope and stay so.
