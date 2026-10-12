---
title: ADR 0081 decision
---

**1. The Linux selector has two rungs, tried in order: landlock, then namespace.** `NewConfiner()`
constructs the landlock backend first; if it can fence writes (ABI ≥ 1) it is returned and the
namespace backend is never built. Otherwise the namespace backend is constructed — its probe forks
`bwrap` once for real, so the rung is only climbed when landlock has already said no — and returned
if it can fence. **Landlock wins whenever both would work**: it is kernel-enforced on the child's own
process, needs no external binary, and fences network egress per-host from ABI 4, where the namespace
backend's network tightening is all-or-nothing. When **neither** fences, the namespace backend is
returned anyway, carrying **both reasons** in `Capabilities().Unavailable` (`landlock unavailable
(landlock_create_ruleset: operation not supported); bwrap not found in /usr/bin, /bin, /usr/local/bin or
/run/current-system/sw/bin`), so every wording surface
names what would have to change on this host. There is **no config key**: the backend is
auto-selected only; "configurable" means the selector has two Linux rungs, nothing more. The backend
type is `namespaceConfiner`, its label is `namespace` (`probe.BackendName`), and `NewReportConfiner()`
stays `NewConfiner()` verbatim on Linux — the probe launches a no-op shell and touches no disk.

**2. The launcher is bubblewrap (`bwrap`), an optional external enhancement in the ADR 0042 pattern.**
`bwrap` is resolved on `PATH` once at construction and never re-queried; absent, the backend fences
nothing and says so. This is the **second bounded exception** to
ADR 0042's "external
programs are never prerequisites": macOS had the only one (`/usr/bin/sandbox-exec`), and Linux now has
one too — bounded the same way. It buys a **mode, not the agent**: a host without `bwrap` still runs
Plan, Ask-Before and Allow-Edits under the confine-if-you-can/gate-if-you-can't net, so the missing
program costs autonomy, not function. And it is reached only on the second rung: a landlock-capable
kernel never looks for it. `bwrap` is chosen because it is small, single-threaded, setuid-free, present
on every mainstream distribution (it is Flatpak's own sandbox), and built to do exactly this — a
native re-exec is a later, separate decision (see *Considered options*). No new module dependency is
taken; the backend is `os/exec` and argv.

> **Amended 2026-10-02 — bwrap is taken from fixed system directories, never from `PATH`.** "`bwrap`
> is resolved on `PATH` once at construction" above is superseded. Construction takes the first
> executable among `/usr/bin/bwrap`, `/bin/bwrap`, `/usr/local/bin/bwrap` and
> `/run/current-system/sw/bin/bwrap`, still once and never re-queried; `PATH` never chooses it,
> because a `PATH` entry a project or the user writes (a direnv-managed `bin/`, a checked-in shim)
> would otherwise pick the program that builds the fence itself (bead `apogee-bwrap-direnv-path`).
> None present ⇒ unavailable with `bwrap not found in /usr/bin, /bin, /usr/local/bin or
> /run/current-system/sw/bin`; a bwrap found only on `PATH` ⇒ unavailable with `bwrap at <path> is
> outside the trusted system directories`, and that bwrap is never executed. Both are
> `CauseBackendAbsent`, and unavailable behaves exactly as an absent bwrap always has — Auto gates to
> approval, unattended runs block, sync reactions are refused; there is no unconfined fallback. The
> neither-host example in decision 1 and decision 4's "not at lookup time" are reworded to match.

**3. The box shape is fixed.** `Confine` rewrites the command to run under

```
bwrap --ro-bind / / --dev /dev --proc /proc --die-with-parent [--unshare-net]
      --bind <root> <root> …  --  <original cmd.Path> <original args…>
```

- `--ro-bind / /` — the whole filesystem, read-only: deny-default for writes; reads and exec stay
  open, because the box bounds where a confined child may **write**, like every other backend.
- `--dev /dev` — bwrap's minimal device set, defined by bwrap and named by reference. A live run
  shows the nodes `null`, `zero`, `full`, `random`, `urandom`, `tty`; the symlinks `ptmx`, `fd`,
  `core` and `stdin`/`stdout`/`stderr` into the child's own `/proc` view; a fresh `devpts`; a
  private `tmpfs` at `/dev/shm`; and, only when bwrap holds a controlling terminal, that terminal at
  `/dev/console`. This is the backend's **write-exempt set** — wider than landlock's and seatbelt's
  exact `/dev/null`, and the contract's §2.3 property 2 is amended to say so. Each entry is either
  side-effect-free (a sink, a source, a private scratch mount), a `/proc` symlink, or the terminal
  the child already owns, so the exemption widens nothing the box protects.
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

> **Amended 2026-10-05 — a confined one-shot run starts in a new session.** "It would detach the
> child from the terminal the tool drives" above held only for the Console's pty run: a one-shot
> run kept apogee's own controlling terminal, so a confined command could open `/dev/tty` and
> `TIOCSTI` keystrokes into the operator's terminal (2026-10-05 code audit). The subprocess funnel
> now starts every confined POSIX run — this backend, landlock and seatbelt — with `Setsid` in place
> of `Setpgid` (contract §2.4), so the detach comes from the fork, `/dev/tty` fails with `ENXIO`, and
> bwrap holds no terminal to bind at `/dev/console`. The argv still never carries `--new-session`
> (the Console keeps its pty as the controlling terminal), and the teardown is unchanged: a session
> leader's PGID is its PID, the group the negative-PID kill aims at.

**4. The construction probe launches bwrap for real.** "bwrap is installed" is not "bwrap can fence
here": kernels and profiles that refuse `CLONE_NEWUSER` to an unprivileged process
(`kernel.apparmor_restrict_unprivileged_userns`, a seccomp filter, `user.max_user_namespaces=0`)
refuse it at run time, not at lookup time. So construction runs the platform shell's no-op
under `bwrap` with the exact flag line `Confine` would generate for a box rooted at the temp dir,
bounded by a timeout. Exit 0 ⇒ `{FSWrite:true, NetworkEgress:true}` — one launch fences both the
filesystem and, when the box asks, the network. Any failure ⇒ `{false, false}` with `Unavailable`
set to `bwrap refused: <bwrap's last stderr line>` (e.g. `bwrap: setting up uid map: Permission
denied`) or `bwrap timed out`. The probe has no disk side effect.

> **Amended 2026-09-22 — the fenceable answer carries one residual.** This section originally read
> `{FSWrite:true, NetworkEgress:true, Residuals:nil}`, "and nothing is residual". That overstated the
> network half. `--unshare-net` gives the box an empty network namespace, which does cut UDP and
> abstract AF_UNIX sockets along with TCP — but a **pathname** AF_UNIX socket is a filesystem object,
> not a network-namespace one, so the `--ro-bind / /` root of decision 3 carries `/var/run/docker.sock`
> and `/run/user/<uid>/bus` into the box and a confined command can still `connect(2)` to them. A
> network-deny box therefore is not a total egress fence, and capability honesty
> (confinement-execution-contract §5) makes the gap disclosed rather than hidden: exit 0 now yields
> `Residuals: ["connect(2) AF_UNIX"]` (`domain.ResidualUnixEgress`, the token landlock discloses for
> its own TCP-only rights), wherever `NetworkEgress` is true. A host without bwrap is unchanged — it
> fences nothing, so it admits no gap either and stays wholly `Unavailable`. The token is not
> write-class, so `probe.CapabilityLine` words it and `probe.ResidualNotice` filters it out: no host
> gains a startup banner for it.

**5. Any run-time failure is `ErrConfinementUnavailable`, never an unfenced run.** `Confine` on a
backend whose probe failed returns `ErrConfinementUnavailable` carrying the reason, so the contract
§4 fallback forces the gate; the empty-argv guard runs first so that refusal reads the same on every
host. The backend never lets a command run outside the box because the box could not be built.

**6. The kill-on-denial signature learns EROFS** (amends
ADR 0056 D2, dated note in place). Under a
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
