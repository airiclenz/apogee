# Apogee — Confinement Execution Contract (the P3.1 implementation contract)

**Date:** 2026-06-24 · **Status:** ✅ **Accepted** (the P3.1 design deliverable) · **Owner ADR:**
[ADR 0012](../adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md)
(supersedes [ADR 0004](../adr/0004-auto-mode-requires-os-level-confinement.md)) ·
**Realised by:** P3.2 (Linux landlock), P3.3 (macOS seatbelt), P3.4 (mode ladder + dispatch wiring),
P3.7 (write-tool family), P3.8 (execution tools). **No production code lands in P3.1** — this document
*is* P3.1's output: the contract those tasks build to, mechanically.

> **Relationship to ADR 0012.** ADR 0012 settled the *policy* — confinement attaches to blast radius,
> Auto is `confine-to-workspace`-tunable, the network is open by default, the dangerous-action guard is
> the mode-independent floor. Its closing line defers the *implementation contract* to "the P3.1 design
> pass." This document is that contract: the `Confine` signature and semantics (§2), the
> `workspaceScopedWriter` marker (§3), the per-call disposition table dispatch computes (§4), the
> capability-honesty rule (§5), and the shared escape-probe harness P3.2/P3.3 share (§6). Where this
> document and the prose in the phase-3 plan §3 D1/D5 disagree on a mechanism, **this document and ADR
> 0012 win** (the plan predates both on the network/kernel/web-tool/MCP specifics).

---

## 1. What P3.1 must pin, and why now

Three of Phase-3's tasks (P3.2, P3.3, P3.4) are "mechanical" only if four things are unambiguous
*before* they start — the same discipline that settled C1–C8 before any Phase-2 pane was drawn:

1. **The `Confine` contract** — what the backend receives, what it does to it, who runs the subprocess,
   and how the subprocess is torn down. P3.2 (landlock) and P3.3 (seatbelt) implement the *same*
   interface; if its shape is unsettled they diverge.
2. **The `workspaceScopedWriter` marker** — the signal only Apogee's own write tools carry, that lets
   the disposition auto-approve an in-workspace edit without OS confinement. It must be unfakeable by a
   third-party tool, and it must survive `registry.Subset` so a sub-agent inherits it (D2).
3. **The per-call disposition** — the one table, keyed on `(mode, tool-class, confine-to-workspace,
   backend-caps)`, that `needsApproval`'s successor computes. Every later tool task asserts its own row.
4. **The shared escape-probe harness** — the hermetic "try to escape the box" battery both backends'
   acceptance tests call, so "confined" means the same thing on Linux and macOS.

The load-bearing call is §2: ADR 0012 deleted in-process per-thread confinement, which means the
**Phase-0 stub signature `Confine(ctx, box, fn func(ctx) error)` can no longer express the model** —
the backend cannot wrap an opaque closure. §2 settles its replacement. This is a pre-`v1.0.0` change to
a public type (`Confiner` is re-exported at the root, D7), made deliberately here while there is still
no stability promise.

---

## 2. The `Confine` execution contract

### 2.1 Why the closure form is deleted

The Phase-0 stub is:

```go
// internal/domain/confinement.go (today — the stub shape)
Confine(ctx context.Context, box ConfinementBox, fn func(context.Context) error) error
```

ADR 0012 fixes confinement to a **single, all-OS subprocess granularity**: macOS execs the child under
`sandbox-exec -p <profile>`; Linux applies a landlock domain to the child after fork, before `execve`.
For the backend to *own that wrapping*, it must see the command being launched. An opaque
`fn func(ctx) error` hides the `*exec.Cmd` inside the closure, so the backend cannot prepend the
`sandbox-exec` prefix or interpose the landlock re-exec wrapper.

The closure form has exactly one way to "confine" a subprocess spawned inside `fn`: apply the
restriction to the **calling goroutine's OS thread** before invoking `fn`, so the child inherits it.
That is the **per-thread in-process landlock** ADR 0012 explicitly deleted — it is irreversible per
thread on Linux (forcing the thread-discard trick and an unenforceable no-goroutine contract) and has
**no equivalent on macOS at all** (seatbelt confines a subprocess, not a thread). So the closure form
is not merely awkward; it *reintroduces the precise contortion ADR 0012 removed.* It is deleted.

### 2.2 The signature: prepare-in-place over a `*exec.Cmd`

P3.4 changes `Confiner` to:

```go
// internal/domain/confinement.go (P3.4)
import "os/exec"

type Confiner interface {
    // Capabilities reports what this backend can enforce here and now (§5).
    Capabilities() ConfinementCaps

    // Confine prepares cmd to execute confined to box, then RETURNS — it does not run
    // cmd. It rewrites cmd to launch under the host OS confinement facility (macOS:
    // exec under `sandbox-exec -p <profile>`; Linux: interpose the landlock re-exec
    // wrapper, §2.3) and sets cmd.SysProcAttr so the caller's process-group kill reaches
    // the wrapped child (§2.4). The caller has already wired Stdin/Stdout/Stderr/Dir/Env
    // and afterwards invokes cmd.Run()/Output(). The PARENT process is never restricted.
    //
    // Confine is only invoked when Capabilities() reports box is enforceable on this
    // host (the disposition checks caps first, §4). ErrConfinementUnavailable is the
    // runtime safety net: a backend that finds it cannot establish the box returns it,
    // and the caller falls back to Approval ("confine if you can, gate if you can't").
    Confine(ctx context.Context, box ConfinementBox, cmd *exec.Cmd) error
}
```

The semantics flip from **run-fn** to **prepare-cmd**:

- The **tool** builds an idiomatic `*exec.Cmd` (via `exec.CommandContext`), wiring its own
  stdin/stdout/stderr, `Dir` (inside the box), `Env`, and timeout. This keeps all I/O and lifecycle in
  the tool, where it belongs — the backend owns *only* the wrapping.
- `Confine` mutates `cmd` in place: it rewrites `cmd.Path` + `cmd.Args` to launch under the OS facility
  and sets `cmd.SysProcAttr`. It performs no I/O and blocks on nothing.
  > **Amended 2026-07-22 (§9, ADR 0020).** The Windows backend rewrites neither `cmd.Path` nor
  > `cmd.Args` — it sets `cmd.SysProcAttr.Token` and nothing else — and it *does* perform bounded,
  > idempotent, once-per-box filesystem I/O (the mandatory-label pass), because on Windows the box's
  > writable half can only be expressed on the objects themselves. It still never runs the command
  > and never blocks on it. See §9.
- The tool then runs `cmd`. A confined child that writes outside the box gets an OS error (EPERM); the
  command simply fails and the model routes around it — there is no Approval prompt for a *subprocess*
  escape (ADR 0012: the subprocess surface is OS-fenced, not gated).

`domain` gains an `os/exec` import. This is acceptable: confinement *is* about launching OS
subprocesses, so the public interface honestly says so, and `os/exec` is stdlib (ADR 0010's invariant
is about the **root module path**, not stdlib breadth). `ErrConfinementUnavailable` is added to
`internal/domain/errors.go` and re-exported at the root alongside `ErrAutoUnavailable`.

This matches the plan's words "`fn` builds + runs the `*exec.Cmd`; the backend owns the wrapping"
literally, with the ambiguity resolved in the only internally-consistent direction: the **tool** builds
and runs the cmd; the **backend** wraps it.

### 2.3 Backend obligations

Both backends implement the same `Confine`, build-tagged per OS; every other OS keeps `denyConfiner`
(which now reports `AutoEligible()==false` and is never handed a cmd to confine, because the disposition
gates the subprocess surface when caps are insufficient).

> **Amended 2026-07-22 (§9, ADR 0020).** Windows gains a **third** backend — a restricted,
> Low-integrity token handed to `SysProcAttr.Token`. It is neither an argv rewrite nor a re-exec
> wrapper: there is **no Windows helper mode and no `__confined-exec` sentinel arm**, because Windows
> has no "restrict myself, then exec in place" API for a helper to call. Its obligations are in §9.

**macOS (P3.3, `//go:build darwin`).** `Confine` generates a `sandbox-exec` profile string from `box`
(deny default; `allow file-write*` under `WorkspaceRoot` + each `WritablePaths` entry; network
allow-by-default, switching to a deny-list only when `box.NetworkAllow` is non-empty and used as a
*tightening* list), then rewrites:

```
cmd.Path = "/usr/bin/sandbox-exec"
cmd.Args = ["sandbox-exec", "-p", <profile>, <original cmd.Path>, <original cmd.Args[1:]...>]
```

The original `Stdin/Stdout/Stderr/Dir/Env` are inherited by `sandbox-exec`, which execs the real child.
The profile is a **pure function of the box** and is unit-tested as a string with no process (hermetic,
runs in the dev env), per P3.3's acceptance.

**Linux (P3.2, `//go:build linux`).** Go cannot run user code between `fork` and `execve` without CGO,
and the cross-build is `CGO_ENABLED=0`. The portable realisation of "apply landlock to the child before
`execve`" is therefore a **re-exec wrapper**: `Confine` rewrites the command to re-invoke the Apogee
binary itself in a hidden helper mode that, as a *separate process*, applies the landlock domain to
itself and then `syscall.Exec`s the original argv. Landlock domains survive `execve`, so the target runs
confined; the parent (the main Apogee process) never called `RestrictSelf`, so it stays unrestricted.

```
cmd.Path = <self executable, os.Executable()>
cmd.Args = [<self>, "__confined-exec", <box, base64-JSON>, "--", <original cmd.Path>, <original args...>]
```

The `__confined-exec` subcommand lives in `cmd/apogee` (it is a process entry point, not library
logic) and calls a single exported helper in `internal/platform`, e.g.

```go
// internal/platform (linux) — the in-child half of the wrapper
func ApplyLandlockAndExec(box domain.ConfinementBox, argv []string) error
```

which builds the ruleset from `box` (workspace-write + `WritablePaths`; a TCP-connect restriction added
only when the box opts into network-deny), calls `landlock_restrict_self`, then `syscall.Exec(argv[0],
argv, os.Environ())`. Both halves use **raw `golang.org/x/sys/unix` syscalls** (`SYS_LANDLOCK_*` over the
typed `LandlockRulesetAttr`/`LandlockPathBeneathAttr`) — no CGO, consistent with `CGO_ENABLED=0`. P3.2
decides raw-syscall vs the `github.com/landlock-l/go-landlock` helper and records it in the commit; the
*contract* above is the same either way.

> **Why re-exec the Apogee binary rather than ship a separate helper executable.** A single static
> artifact stays a single static artifact (Standing Requirement 2). The helper mode is gated behind an
> argv sentinel (`__confined-exec`) that the normal CLI never surfaces, dispatched in `main` before
> Cobra. The box is passed inline (argv) so the helper needs no shared state with the parent — coherent
> with statelessness (ADR 0008).

### 2.4 Process-tree teardown and cancellation

The wrapping changes the process tree (`sandbox-exec` is the parent of the real child on macOS; the
re-exec helper execs-in-place on Linux, preserving the PID; on Windows there is no wrapper process at
all — §9.2 — but a shell still spawns descendants). A naïve `cmd.Process.Kill()` may leave descendants.
The contract makes teardown OS-agnostic for the tool: **one container holds the whole tree, and cancel
kills the container.** What the container *is* is the only per-OS part.

- **Backend obligation (POSIX):** `Confine` sets `cmd.SysProcAttr.Setpgid = true` so the wrapped child
  and its descendants share a process group. The Windows backend sets **no** `SysProcAttr` field other
  than `Token` (§9.2) — its container is owned entirely by the tool, below.
- **Tool obligation (P3.8) — POSIX:** the execution tools set `cmd.Cancel` to signal the **negative PID**
  (`syscall.Kill(-cmd.Process.Pid, SIGKILL)`) and set a short `cmd.WaitDelay`, so a ctx cancel / timeout
  reaps the whole group — no orphaned `sandbox-exec`, no orphaned child.
  **Amended 2026-08-12 (hostile-bytes hardening, item 18): teardown runs on EVERY exit, not only on
  cancellation.** The same group kill also runs after a normal `Wait` (`processTeardown.reap`,
  `internal/tools/exec_teardown.go`), so a descendant the command backgrounded does not outlive a
  clean exit either — the one-shot contract holds on all three paths, not two. A `cmd.WaitDelay`
  that expires is now reported (`exec.ErrWaitDelay`) instead of falling through to the leader's own
  exit code, which rendered a wedged drain as a success.
- **Tool obligation — Windows (Phase 5):** Windows has no process groups, so the container is a **Job
  Object** created before `Start` with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, which the started process is
  assigned to and which `cmd.Cancel` **terminates** (`TerminateJobObject`) instead of killing the leader;
  `cmd.WaitDelay` is identical. A job that could not be created or assigned degrades to a leader-only
  kill, never to a failed command — teardown is a safety net, not the confinement fence (ADR 0020).
  Two Windows-only properties follow from the facility and are deliberate:
  - **The assignment happens just after `CreateProcess` returns**, because creating a process directly
    into a job needs `PROC_THREAD_ATTRIBUTE_JOB_LIST` on a `STARTUPINFOEX`, which `syscall.SysProcAttr`
    cannot express and a suspended start cannot substitute for (`os/exec` closes the initial thread
    handle). The residual window closes before a shell has parsed its command line.
  - **The `KILL_ON_JOB_CLOSE` limit is cleared before the handle is closed** on a completed run, so
    closing the handle is never itself a teardown. The limit's job is the crash path — apogee dying
    mid-run with nobody left to make the call; every path that started a process terminates the job
    explicitly first, `cmd.Cancel` on the cancelled one and `reap` on the clean one (amended
    2026-08-12, item 18: a process the command deliberately left running does **not** outlive the
    call, on either OS).

The tool never needs to know *how* the command was wrapped, and the two backends' observable behaviour
is the same: the container contract abstracts both. The run is governed by the **cmd's own context**
(built with `exec.CommandContext`); the `ctx` passed to `Confine` covers only the (synchronous,
non-blocking) preparation and is not the run's lifetime.

### 2.5 Rejected alternatives

- **Keep `fn func(ctx) error`.** Rejected — §2.1: it can only confine a subprocess via per-thread
  in-process landlock, the exact contortion ADR 0012 deleted, and it is impossible on macOS.
- **`RunConfined(ctx, box, ConfinedCommand) (ConfinedResult, error)` — backend runs the cmd and returns
  captured output.** Tempting (the backend owns the full lifecycle, including teardown). Rejected for v1:
  it duplicates a slice of `os/exec` into `domain` (`Path/Args/Dir/Env/Stdin` + `Stdout/Stderr/ExitCode`)
  and bakes in capture-only I/O, foreclosing streaming if a later tool wants it. Prepare-in-place keeps
  the tool in control of I/O for one extra obligation (the process-group kill), which the `Setpgid`
  contract makes a two-line idiom. Revisit post-v1 only if a backend appears that cannot express its
  wrapping as argv-rewrite + `SysProcAttr` (none of landlock/seatbelt/AppContainer do).
- **A separate helper binary for the Linux re-exec.** Rejected — §2.3: it breaks the single-artifact
  property for no benefit; the argv-sentinel self-re-exec is standard and self-contained.

### 2.6 Host wiring (P3.4)

`cmd/apogee` stops injecting `denyConfiner` and selects the **real backend for the host OS** behind build
tags — `platform.NewConfiner()` returns the landlock backend on Linux, the seatbelt backend on macOS,
and `denyConfiner` elsewhere. *(Amended 2026-07-22, §9: Windows is no longer "elsewhere" — it selects
the token backend at or above build 17763, and `denyConfiner` only below that floor.)*
The `ConfinementBox` is built from the injected
`WorkspaceDir` plus the per-project `WritablePaths`/`NetworkAllow` from config (see §7 — the box must
include the toolchain's cache/temp dirs or `go test`/`pip` fail under confinement). The `main` entry
point dispatches the `__confined-exec` sentinel before Cobra (§2.3).

---

## 3. The `workspaceScopedWriter` marker

### 3.1 Requirements

ADR 0012 / D1 need a signal that says *"this is one of Apogee's own write tools, path-safety-bounded to
the workspace — code Apogee wrote and tests."* Such a tool needs **no OS confinement**: the same trusted
boundary that auto-approves it in Allow-Edits is what bounds it in Auto. The signal must be:

1. **Detectable** by the dispatch disposition (`internal/agent`).
2. **Carried only by Apogee's own tools** — a third-party tool from outside the module **structurally
   cannot fake it** (D1's explicit words: "cannot fake from outside `internal/`").
3. **Survives `registry.Subset`** so a sub-agent one level down inherits it (D2) — automatic if it is a
   property of the tool *value*, not a side-table.
4. **Able to classify the call's write target** as in- or out-of-workspace *before* `Execute`, because
   P3.4's disposition auto-runs an in-workspace write but gates an out-of-workspace one (§4).

### 3.2 The mechanism

A Go interface is satisfied **structurally**, so an exported marker with only exported methods can be
faked by any third-party type that happens to have those methods. The unfakeable construction is an
**unexported method on an interface defined in the same package as the tools that carry it** — only
types in that package can name the method, and a different *module* cannot even import the package
(Go's `internal/` rule). Both conditions point to one home: `internal/tools`.

```go
// internal/tools/workspace_scoped.go  (P3.7 lands this file)
package tools

// workspaceScopedWriter is the unexported marker carried only by Apogee's own
// write tools. Its unexported method means no type outside this package — and no
// third-party tool in another module — can satisfy it, so the dispatch disposition
// may trust it as "Apogee's own path-safety-bounded write" (ADR 0012 D1).
type workspaceScopedWriter interface {
	domain.Tool

	// workspaceWriteTarget resolves the absolute path this call would write, so
	// dispatch can classify in- vs out-of-workspace before Execute (§4). ok is false
	// when the call writes nothing inspectable (then dispatch treats it as in-bounds).
	// It performs no write — pure path resolution, reusing resolveInRoot's logic
	// without enforcing containment. It yields the path in BOTH spellings: the
	// absolute path the argument NAMES, and that path symlink-resolved. Only the
	// resolver sees the two, and they differ exactly when the argument travels
	// through something that is not the directory it appears to be — the fact the
	// disclosure accessor below hands the surfaces that show the call.
	workspaceWriteTarget(call domain.ToolCall) (target writeTarget, ok bool)
}

// IsWorkspaceScopedWriter reports whether t is one of Apogee's own workspace-scoped
// write tools — the signal dispatch keys on to auto-approve an in-workspace write in
// Allow-Edits/Auto with no OS confinement and no Approval (ADR 0012 D1/D5).
func IsWorkspaceScopedWriter(t domain.Tool) bool {
	_, ok := t.(workspaceScopedWriter)
	return ok
}

// WorkspaceWriteTarget exposes the marker's target-path resolution to dispatch
// without exporting the marker interface itself. Returns ("", false) for any tool
// that is not a workspace-scoped writer.
func WorkspaceWriteTarget(t domain.Tool, call domain.ToolCall) (string, bool) {
	target, ok := writeTargetOf(t, call)
	if !ok {
		return "", false
	}
	return target.Real, true
}

// ResolvedWriteTarget is the DISCLOSURE half of the same seam: the path the write
// really lands on, returned only when it differs from the path the argument names,
// and "" otherwise. Dispatch puts it on the ToolCallEvent and the ApprovalRequest so
// the pane, the tool card and the write's own result can name where a call goes —
// the same resolution the classification above judged it by, so a surface and the
// gate can never be two readings of one call.
func ResolvedWriteTarget(t domain.Tool, call domain.ToolCall) string {
	target, ok := writeTargetOf(t, call)
	if !ok || target.Real == target.Named {
		return ""
	}
	return target.Real
}
```

Each built-in write tool gains the two unexported methods (one-liners delegating to its existing
arg-decode + `resolveInRoot`-style logic). `internal/agent/dispatch.go` calls `tools.IsWorkspaceScopedWriter`
/ `tools.WorkspaceWriteTarget` — a detection-only import, and **`internal/agent` already imports
`internal/tools`** (`loop.go` defaults the registry via `tools.NewDefaultRegistry`), so this adds no new
package edge and no cycle (`tools` imports only `domain`).

### 3.3 Who carries it

- **Today (4 built-ins):** only `write_file` is a writer (`read_file`/`list_dir`/`grep` are
  `ReadOnly()`), so `write_file` is the lone marker-carrier once P3.7 adds the marker.
- **P3.7 adds:** find-replace (single + multi), `patch`/apply-edit — all carry it. `diff` and `open-file`
  are read-only and do **not** (they need no disposition help; Plan already runs them).
- **Never carried by:** `terminal`, `python-exec`, `git` (subprocess surface — OS-confined in Auto, not
  marker-bounded); `web-fetch`/`http-request`/MCP (`ExternalEffectTool`); `sub_agent` (the recursion
  point — D2, carries no disposition marker); any third-party tool (structurally cannot).

### 3.4 Survives `Subset`; sub-agents inherit it

`registry.Subset(names…)` returns the **same tool values** under a new registry (it copies pointers, not
data). The marker is a method set on those values, so a sub-agent constructed with
`Subset("write_file", "grep")` sees `write_file` still carry the marker — the disposition is identical
one level down (D2), for free, with no threading.

### 3.5 Rejected alternatives

- **An exported marker in `domain`** (e.g. `WorkspaceScopedWriter` with an exported `WorkspaceScoped()
  bool`). Rejected — a third-party tool satisfies it structurally; it fails requirement 2.
- **An exported embeddable struct `domain.WorkspaceScopedWriter{}` with an unexported method.** Rejected —
  anyone can embed an *exported* type and inherit its unexported method via promotion, so it is fakeable.
- **A name-set side-table built by `NewDefaultRegistry`** (`cfg.WorkspaceScopedTools map[string]bool`).
  Rejected — weaker (a name, not a structural type), and it must be re-filtered through every `Subset`
  to follow a sub-agent, where the type marker rides the tool value automatically.

---

## 4. The per-call **Resolution** — the one verdict dispatch executes (D5)

> **Amended 2026-07-02 (Resolution refactor, item 2).** §4 previously described only the
> post-guard **ladder stage** (the "per-call disposition"). The **Resolution** subsumes it: the
> single, complete verdict for one call — the guard floor, the ladder table, the confinement
> capability, and the precomputed runtime contingency — computed in full by the pure `resolve()`
> (`internal/agent/resolution.go`) *before* anything executes. Dispatch is a thin executor that
> gathers facts, calls `resolve()` once, and carries out the verdict; it holds no ladder,
> guard-tier, or demote decision of its own. "Disposition" is **retired from code** (it named only
> the ladder stage). See CONTEXT.md → **Resolution** and the 2026-07-02 clarification in ADR 0013.
> The section number is kept because code comments cite "§4".

> **Amended 2026-07-25 (url-safety choke point; ADR 0012 amendment 2026-07-25).** The **net** class
> no longer means "declares `EffectNetwork`" — it means "declares it **and** carries the unexported
> url-filter marker", which is obtainable only by embedding `internal/tools`' network funnel (the
> single path from a tool to the network, which applies the host's `URLGuard`). An `EffectNetwork`
> tool **without** the marker is the new **3p-net** class and **gates** in Auto, with its own gate
> reason. Tighten-only; `confine=false` is unaffected.

> **Amended 2026-07-26 (the RO row is a floor, not a trump card; url-safety live-gap audit H-1).**
> The **RO** row applies **only to a tool carrying no other class marker**. A tool that declares
> itself read-only **and** is also subproc / net / 3p-net / mcp / WS-write takes **that class's
> row**, in every mode — the marker is unfakeable by construction, `ReadOnly()` is a bare
> self-declaration, and the declaration can never outrank a structural fact about what the tool
> does (ADR 0012's core invariant: never both unsupervised *and* unbounded). `classifyTool`
> expresses this through its check ORDER: every marker is consulted first and **RO is the terminal
> floor**, reached only by a tool no marker claimed. The declaration keeps its other jobs — Plan
> mode's menu filter and Ask-Before's harmless-read skip still read it. Two shipped built-ins move
> as a result: `git_diff_range` and `diagnostics` declare read-only **and** launch an OS subprocess
> (the system `git`, the Go toolchain), so they now ride the **subproc** row — `confine` in Auto
> with the caps-insufficient `gate` fallback, `gate` on the middle rungs, `refuse` in Plan (they
> stay in Plan's read-only menu, which is a UX affordance; the class is the boundary). Tighten-only
> in every cell; `confine=false` is unaffected.

> **Amended 2026-08-02 (one classification: the Plan menu keys on the ladder's fact).** The
> amendment above left the Plan **menu filter** reading `ReadOnly()` while the Plan **row** read the
> class, and recorded the gap in footnote ² as deliberate. It was not sustainable: Plan advertised
> `git_diff_range` and `diagnostics` and then refused the call, which reads to a small model as a
> broken tool rather than as a mode boundary. Both now key on ONE predicate — `planAdmits`
> (`internal/agent/resolution.go`), which is `classifyTool(tool) == RO` — so the menu can never
> offer what the ladder refuses. **Plan drops those two tools from the menu**; the `sub_agent`
> recursion point stays (it is `Delegate`d before the ladder, never a leaf). `ReadOnly()` keeps its
> remaining jobs: it is the terminal-floor input to `classifyTool` and what self-regulation's
> read/write tally reads. Menu-only change; no ladder cell moves, and no verdict changes.

> **Amended 2026-08-12 (the writable box fences `argv[0]` too; hostile-bytes hardening, items 2 and
> 5).** `box.WritablePaths` was read only by the OS backends that build the *write* fence, so
> nothing stopped a confined Auto call planting an executable inside its box and a later
> **unconfined** call running it from outside — the box bounded the write and then vouched for the
> bytes. Every site that resolves a program now refuses an `argv[0]` landing inside the workspace
> root or any `box.WritablePaths` entry, before anything starts
> (`security.RefuseExecFromWritablePath`, wrapped package-locally in `internal/tools`,
> `internal/mechanisms` and `internal/present`). It is a **tool-side fence, not a ladder cell**: no
> cell moves and no verdict changes, it applies on the `Run` path exactly as on `Confine` (an
> unconfined site passes a nil box, leaving the workspace root as the whole fence), and it reaches
> the one exec that never gets a Resolution at all — rung 1's OS opener, which `present_document`
> fires read-only in every mode (ADR 0019), and which also stopped resolving its program by bare
> name against apogee's inherited `PATH`. The scoped environments close the same door from the
> child's side: `platform.Host.ScopeEnv` now takes the workspace root and drops workspace-resident
> `PATH` entries, so git and the Go toolchain cannot redo the lookup the fence just refused. The
> ergonomics cost — an activated `<repo>/.venv/bin/python3` or `node_modules/.bin` is refused, with
> no switch — is accepted and recorded in `ISSUES.md` L5.

A Resolution is one of five **kinds** — `Run` · `Confine` · `Gate` · `Refuse` · `Delegate` —
computed in a fixed, load-bearing order:

1. **Guard floor first (tighten-only, ADR 0012 / P3.6).** A Tier-1 dangerous action or a tripped
   circuit-breaker ⇒ **`Refuse`** in every mode. A Tier-2 dangerous action ⇒ **force** the Approver:
   it upgrades any non-`Refuse` *leaf* verdict to a forced `Gate`. Applied to **leaf verdicts only**
   — never to a `Delegate`.
2. **`sub_agent` ⇒ `Delegate`** (ADR 0013): the recursion point drives a nested Agent, not a leaf
   tool. A Tier-2 force is **deliberately not** applied here — nothing executes at delegation, so the
   shared read-only floor re-fires on the child's own dangerous call. At the depth bound the
   delegation is **`Refuse`d** defensively (belt-and-braces with the withheld-tool floor). An
   **unknown tool** (a registry miss — e.g. a tool withheld at the depth bound) is `Refuse`d as a
   dispatch fact, un-audited, before the ladder.
3. **The ladder table (below) yields the leaf verdict**, then the leaf overlays finish it: a `Gate`
   with **no Approver configured ⇒ `Refuse`** ("approval required but no Approver configured") — a
   `Gate` always means the Approver is actually consulted; a `Gate` takes its `Reason` + `CacheKey`;
   every `Confine` takes its box + a precomputed runtime `fallback` (both detailed after the table).

Tool-classes: **RO** = `IsReadOnly` **and no other marker** — the terminal floor (a tool that also
carries a marker below takes that class's row, amendment 2026-07-26); **WS-write** =
`workspaceScopedWriter` (§3); **subproc** =
shell/exec subprocess tool (`terminal`/`python-exec`/`git`); **net** = `ExternalEffectTool` of kind
`network` carrying the `urlFilteredNetworker` marker (Apogee's own — the marker is obtainable only by
embedding `internal/tools`' network funnel, so it cannot exist without the `URLGuard`); **3p-net** =
kind `network` **without** that marker (a third-party network tool whose URLs Apogee cannot vouch
for); **mcp** = `ExternalEffectTool` of kind `mcp`; **3p-write** = a write-capable tool that is
neither RO, WS-write, nor External (a third-party in-process writer Apogee cannot vouch for).

Ladder-leaf outcomes: **run** = execute directly, no gate, no `Confine`; **confine** = execute inside
`Confiner.Confine` (subprocess), no gate; **gate** = route through Approval (allow-for-session caches);
**refuse** = Plan-mode write refusal.

| tool-class | Plan | Ask-Before | Allow-Edits | Auto · `confine=true` | Auto · `confine=false` |
|---|---|---|---|---|---|
| **RO** (and no other marker) | run | run | run | run | run |
| **WS-write**, target **in** workspace | refuse | gate | **run** | **run** (path-safety-bounded) | run |
| **WS-write**, target **out** of workspace | refuse | gate | gate | **gate** | run |
| **subproc** (caps sufficient) | refuse² | gate | gate | **confine** | run |
| **subproc** (caps **insufficient**) | refuse² | gate | gate | **gate** ("confine if you can, gate if you can't") | run |
| **net** (`web-fetch`/`http-request`) | refuse¹ | gate | gate | **run** (url-safety filtered) | run |
| **3p-net** (no url-filter marker) | refuse¹ | gate | gate | **gate** (URLs unfiltered) | run |
| **mcp** | refuse¹ | gate | gate | **gate** (server-grain allow-for-session) | run |
| **3p-write** (can't vouch for scoping) | refuse | gate | gate | **gate** | run |

¹ Plan filters on the **class**, so net / 3p-net / mcp tools are not even offered; a defensive call refuses.
² **Resolved 2026-08-02 (the menu follows the ladder).** A subprocess tool that also *declares*
`ReadOnly()` (`git_diff_range`, `diagnostics`) used to be **offered** in Plan's menu — the filter read the
declaration while the ladder read the class — so Plan advertised two tools it then refused. Both sides now
key on one predicate (`planAdmits`, `internal/agent/resolution.go`), which is the class: Plan **neither
offers nor runs** them. The menu is no longer a UX affordance with its own rule; the class is the boundary
on both sides, and `internal/agent/planmenu_test.go` pins the agreement over the whole registry.
`confine=false` is global-config-only, VM-only, prints a per-session startup warning, and **never**
escapes the dangerous-action floor.

Reading the load-bearing column (**Auto · `confine=true`**, the default): a subprocess escape is
**OS-blocked**; an Apogee in-workspace write is **path-safety-bounded** (no Confine, no prompt); an
out-of-workspace Apogee write **asks** (Apogee can inspect the path, so it can — unlike a subprocess);
`web-fetch` **auto-runs** url-filtered (the network is open; a subprocess could `curl` the same host, and
the native tool is the *safer* path) while a network tool that does **not** route through the funnel
**asks** (its URLs are unfiltered — the network analogue of the 3p-write row); **MCP asks**
(unfenceable server — the per-tool teeth, intact).

**A `Gate` carries `Reason` + `CacheKey`, and on two cells only a `Remedy`.** `Reason` is the human-facing why, mapped from the tool's
class — `network reach` (net), `unfiltered network reach` (3p-net), `unconfinable MCP tool` (mcp),
`subprocess execution` (subproc), `out-of-workspace write` (WS-write), `write` (3p-write); a
Tier-2-forced gate overrides it with `dangerous-action guard forced approval`. Subproc is the one
class with TWO spellings, because it is the only class whose gate has two different causes: a
mode-driven gate — ask-before, allow-edits, unknown-mode — reads the bare `subprocess execution`,
since the rung itself asked for the approval and the host's confinement capability plays no part;
only the **Auto + `confine=true` + caps-insufficient** cell reads
`subprocess execution (confinement unavailable on this host)`, where the backend genuinely could not
give the fence ("gate if you can't") and naming it is what points the user at `/confine` — the same
wording the runtime-demote fallback below carries, deliberately telling that story in the same
words. **`Remedy`** (optional; added 2026-08-12) is the one-line route OUT of the condition the
`Reason` names, carried beside it to the Approval prompt. It is set on exactly the two gates whose
cause the user can lift — the **Auto + `confine=true` + caps-insufficient** cell above, and the
runtime-demote fallback below — and both read
`/confine off runs commands unconfined this session (disposable machines only)` — the escape
`/confine status` already offers, condensed to the one line an Approval prompt has room for.
For that cell the reason and the remedy leave the resolver
TOGETHER, out of ONE cell predicate written once, so a prompt can never name one cause and
prescribe another's fix. Every other gate carries none: a gate the autonomy rung itself asked for
has no condition to lift, only a mode to be in, so most prompts read exactly as they always have —
and the no-Approver `Refuse` carries none either, since its text goes to the MODEL, which has no
terminal to type `/confine` into. Like `Reason` it is a bare sentence: the `Fix: ` label the TUI
paints in front of it is that Driver's presentation, not the engine's (ADR 0031, wire-silent).
`CacheKey` is the
allow-for-session key, and since it decides what ONE "allow for this session" stands for, it is a
blast radius like any other: for every class **except mcp** it is the **tool name plus a digest of
the call's arguments**, so an allow authorises the call the human actually read rather than every
later call of that tool — one allow on `terminal` no longer pre-clears every shell command for the
rest of the Session (and, the memory being the whole agent tree's, for its parent and siblings
too). The arguments are digested in their canonical spelling (`tools.CanonicalArgs`: keys sorted, a
duplicated key collapsed to the **last** — the value stdlib JSON hands the executor and the
approval pane shows), so a reordered or duplicated key cannot mint a second identity for one
executed call; arguments that do not decode yield the **empty** key, which the memory refuses at
both ends, so such a call re-prompts. The deliberate cost is ergonomic: allowing `npm test` does not
clear `npm run build`. **mcp** keeps the **server grain** `mcp-server:<alias>` so approving one of a
server's tools clears its siblings for the Session (ADR 0012's server-grain promise; the
`mcp-server:` prefix keeps the grain collision-proof, and an MCP tool that does not expose its
alias — or the single unnamed server — degrades to the tool-name / `mcp-server:` grain, a
tighten-only fallback). A **forced** gate (Tier-2 or a runtime demote) skips the cache entirely and
is never pre-allowable.

**A `Confine` carries a bounded runtime `fallback` (D4).** The caps check above is a *construction-time*
promise; the box can still fail to establish at run time. So the **subproc, caps sufficient → confine**
cell carries one precomputed contingency: if `Confine` returns `ErrConfinementUnavailable`, the call
demotes to a **forced `Gate`** whose allow-continuation **re-runs the subprocess unconfined** (Approval
is now the bound — the same "gate if you can't" outcome, decided at run time); with **no Approver** the
fallback is a **`Refuse`** ("subprocess could not be confined and approval was not granted"). The
fallback never carries its own fallback — the demote is a single bounded step, and the executor follows
it without re-deciding.

> **Realisation gap — half-landed (updated 2026-08-10; flagged, not silent).** The "WS-write, target out
> of workspace → gate" row needs the write tool to actually *perform* an approved out-of-workspace write.
> The **classification half has landed**: dispatch resolves the target with `resolveTargetUnbounded`
> (`internal/tools/workspace_scoped.go:102`), so an out-of-workspace write now **reaches the Gate**
> instead of being pre-rejected — the row is no longer unreachable, as this paragraph used to say. The
> **`Execute` half is still open**: `internal/tools/write_file.go:82` writes through an `os.Root` fence
> pinned at the workspace root, which refuses the escape whatever the verdict was, so the human approves
> and then gets an error result. Closing it either way is an owner call — land the P3.7 reconciliation
> (the write tool resolves against `WorkspaceRoot ∪ box.WritablePaths` and honours a dispatch-approved
> target) or ratify strict fencing as the permanent answer and amend this row to say the Gate's allow is
> advisory for writes — and it is tracked as the open `ISSUES.md` entry "an *approved* out-of-workspace
> write still errors at `Execute`". The marker's `workspaceWriteTarget` seam (§3.2) is what makes the
> richer behaviour a later additive change, not a rework.

`AutoEligible()` becomes `FSWrite`-only (§5), so `ErrAutoUnavailable` is now **conditional** — a host
with no fs-confinement does not refuse Auto; it lands in the "subproc, caps insufficient → gate" row.

---

## 5. Capability honesty

`Capabilities()` reports what the backend can enforce **on this host, now** — probed once at construction,
never optimistic:

- **Linux:** call `landlock_create_ruleset(NULL, 0, LANDLOCK_CREATE_RULESET_VERSION)` to read the
  supported ABI. ABI ≥ 1 (kernel ≥ 5.13) ⇒ `FSWrite = true`. ABI ≥ 4 (kernel ≥ 6.7) ⇒ `NetworkEgress =
  true`. A kernel without landlock ⇒ `{false, false}`.
- **macOS:** probe for `/usr/bin/sandbox-exec` (present on stock macOS). Present ⇒ `{true, true}` (one
  profile enforces both). Absent ⇒ `{false, false}`.
- **Windows** *(added 2026-07-22, §9 / ADR 0020)*: read the un-shimmed build number
  (`RtlGetNtVersionNumbers`) and mint the restricted Low-integrity token. At/above build **17763**
  with the token minted ⇒ `{FSWrite: true, NetworkEgress: false}` — **Auto-eligible**, since
  `AutoEligible()` is `FSWrite`-only. A mint failure ⇒ `{false, false}`; **below the floor
  `NewConfiner()` returns `denyConfiner`** outright. The probe covers the **facility** only; a
  per-run *path-labelling* failure is a `Confine`-time `ErrConfinementUnavailable` (§9) — the one
  place capability honesty splits in two.
- **Other OSes:** `denyConfiner` ⇒ `{false, false}`.

P3.4 changes `AutoEligible()` from `FSWrite && NetworkEgress` to **`FSWrite` only** (ADR 0012: the
network is open by default, so network-egress confinement is an *optional tightening*, not an Auto
gate). Consequence: Linux Auto needs only kernel ≥ 5.13. A 5.13–6.6 kernel reports
`{FSWrite:true, NetworkEgress:false}` and is **Auto-eligible**; `NetworkEgress` matters only when a user
opts back into network-deny via `box.NetworkAllow`. The `agent.New` Auto gate (`loop.go`) reads
`AutoEligible()` unchanged in shape — only the predicate it calls is loosened.

---

## 6. The shared escape-probe harness (makes P3.2/P3.3 acceptance mechanical)

Both backends prove the same property: **a confined subprocess cannot escape the box, and the parent is
unaffected.** P3.1 pins the harness so the two backend tests differ only in which `Confiner` they pass.

### 6.1 Shape

```go
// internal/platform/confinetest/confinetest.go  (test-support package; P3.2 lands it,
// P3.3 reuses it). It builds confined *exec.Cmd values via the backend under test and
// asserts OS denial — so "confined" means the same thing on landlock and seatbelt.
package confinetest

// Probe drives c through the full escape battery (§6.2) under a box rooted at fresh
// temp dirs the harness itself owns (so cleanup is automatic). The caller (a backend's
// _test.go) passes the OS-specific backend and the platform shell; the battery and its
// assertions are identical across backends.
//
//   t   – the test
//   c   – the backend under test (landlock on Linux, seatbelt on macOS, token on Windows)
//   sh  – the platform shell the probes run through (`platform.Current()`), taken from the
//         caller so the battery runs natively on Windows; redeclared as a local three-method
//         interface rather than imported, because internal/platform's own tests are in
//         package platform and importing it here would be a cycle
func Probe(t *testing.T, c domain.Confiner, sh Shell)

// ProbeNetwork runs the network arm separately (it needs a listener and is skipped
// when the backend reports NetworkEgress=false). Split out so the fs battery runs on
// every Auto-eligible host while the net arm runs only where it is enforceable.
func ProbeNetwork(t *testing.T, c domain.Confiner, sh Shell)
```

The confined child is a real subprocess (confinement wraps subprocesses, §2): the fs battery runs a
one-byte write through the caller's `Shell` — `printf x > <path>` on POSIX, `echo x> "<path>"` under
`cmd.exe` — with those dialect spellings kept per OS in `confinetest/lines_other.go` and
`confinetest/lines_windows.go`; the network battery opens a TCP connection from the shell
(`bash -c 'exec 3<>/dev/tcp/<host>/<port>'`, POSIX-only — it runs only where the backend enforces
egress). Each is built as a normal `*exec.Cmd`, handed to `c.Confine(ctx, box, cmd)`, then run; the
harness asserts on exit status / error.

### 6.2 The battery and assertions

| # | Attempt (as a confined subprocess) | Assertion | Backend |
|---|---|---|---|
| 1 | write `WorkspaceRoot/probe.txt` | **succeeds** (exit 0; file present) — positive control | both |
| 2 | write a `WritablePaths` entry outside the workspace | **succeeds** — the allowlist works | both |
| 3 | write `<sibling-temp>/escape.txt` (outside box) | **denied** — non-zero exit / EPERM; file absent | both |
| 4 | write `$HOME/.ssh/escape` (outside box) | **denied** | both |
| 5 | after #1–#4, the **parent** writes `<sibling-temp>/parent.txt` | **succeeds** — parent unrestricted | both |
| 6 | the confined child `exec`s a second program that writes outside | **denied** — domain inherits across `execve` | Linux, **Windows** |
| 7 | (net) connect a non-allowlisted host, box network-**deny** | **denied** | net-capable |
| 8 | (net) connect a host, box network-**open** (default) | **allowed** — network is open by default | net-capable |
| 9 | after teardown, the box roots' mandatory labels | **back to their prior state** — the disk mutation is reverted | **Windows** |
| 10 | `Confine` a box with a non-empty `NetworkAllow` | **`ErrConfinementUnavailable`** — a requested tightening is never a silent no-op | **Windows** |

#3/#4 are the core "escape is OS-blocked" proof; #5 is the "no per-thread landlock, parent untouched"
proof; #6 is the "after fork, before execve, inherited across exec" proof specific to the re-exec
wrapper; #7/#8 encode ADR 0012's network-open default with deny as a tightening.

> **Amended 2026-07-22 (§9, ADR 0020).** Rows **#9/#10 are Windows-only** and cover what the token
> backend adds to the model: a disk mutation that must be undone, and a capability it must refuse to
> fake. Row **#6 gains a Windows arm** — under a token backend "the restriction is inherited by
> descendants" is exactly as load-bearing as landlock's `execve` claim, and exactly as unproven until
> asserted, so it is **asserted, not assumed**. Rows #7/#8 **skip** on Windows (`ProbeNetwork` guards
> on `NetworkEgress`, which is false there by §9); #5 is free (the restricted token is a copy, so the
> parent's own token is never touched). The harness *was* POSIX-shaped (`sh -c` hard-coded in every
> probe); item 8 widened it — the probes now run through the `Shell` the caller passes
> (`platform.Current()`), and the shell-dialect lines live per OS in `confinetest/lines_other.go` and
> `confinetest/lines_windows.go`; see §9's probe-expectations list.

### 6.3 Per-backend acceptance checklists (now mechanical)

**P3.2 (Linux landlock)** is done when: `Capabilities()` is honest across a ≥6.7 and a 5.13–6.6 kernel
(the latter `NetworkEgress=false` but **`AutoEligible()=true`**); `confinetest.Probe` passes #1–#6;
`confinetest.ProbeNetwork` passes #7 on ≥6.7 (skipped below); the parent stays unrestricted after a
confined child (#5); cross-build green (file `linux`-tagged; other OSes keep `denyConfiner`); x/sys
promoted to a direct dep with `go mod tidy` clean.

*(Added 2026-07-22: the Windows token backend's checklist is **§9.4**, kept there with the rest of its
obligations rather than tacked on here.)*

**P3.3 (macOS seatbelt)** is done when: the generated profile is unit-tested as a pure string from a box
(hermetic, runs in the dev env — no macOS needed for that test); on a macOS runner `confinetest.Probe`
passes #1–#5 and `ProbeNetwork` passes #7/#8; `sandbox-exec`-absent ⇒ `Capabilities()=={false,false}` ⇒
the disposition gates the subprocess surface (Auto not refused, ADR 0012); cross-build green
(`darwin`-tagged).

---

## 7. Box-construction concerns (surfaced for P3.4 / P3.8)

A workspace-only box **breaks real toolchains**, and discovering that during P3.8 would stall it. Pinned
here so the box builder (P3.4) and the execution tools (P3.8) account for it up front:

- **`go build`/`go test`** write to the build cache (`$GOCACHE`, default `~/.cache/go-build`) and
  `$GOTMPDIR`/`$TMPDIR`. These must be in `box.WritablePaths` or every confined Go command fails.
- **`pip`/`npm`/`cargo`** write to their caches (`~/.cache/pip`, `~/.npm`, `~/.cargo`) and to `$TMPDIR`.
  The network being open (ADR 0012) is necessary but not sufficient — the cache dirs must be writable.
- **`git`** writes to `$TMPDIR` and reads global config; a commit writes only inside the repo (in-box).

Recommendation: P3.4 seeds `WritablePaths` with the detected toolchain cache + temp dirs by default
(probed, not hard-coded), and config may extend it per project. This is a box-*construction* concern, not
a `Confiner` concern — the `Confine` contract (§2) is unaffected; it confines to whatever box it is
handed.

---

## 8. Acceptance map — P3.1 done; what each successor implements

| P3.1 acceptance criterion | Where settled |
|---|---|
| ADR 0012 accepted + ADR 0004 amended/cross-referenced | ✅ committed (`54b363c`); this doc adds the implementation-contract pointer |
| `Confine(fn)` signature settled | §2 — prepare-in-place `*exec.Cmd`; closure form deleted with reasoning |
| `workspaceScopedWriter` marker specified | §3 — unexported method in `internal/tools`, `workspaceWriteTarget` seam, survives `Subset` |
| shared confinement-probe contract specified (signatures, escape attempts) | §6 — `confinetest.Probe`/`ProbeNetwork`, the 8-row battery, per-backend checklists |
| per-call disposition pinned | §4 — the full table, dangerous-action guard runs first |
| capability honesty pinned | §5 — startup probe, `AutoEligible()` → `FSWrite`-only |
| **no production code in P3.1** | ✅ — this is a design document only |

**Successor tasks build to this contract mechanically:** P3.2 (§2.3 Linux + §6.3), P3.3 (§2.3 macOS +
§6.3), P3.4 (§2.2 signature change, §4 table, §5 `AutoEligible`, §2.6 wiring, §7 box), P3.7 (§3 marker on
the write family + the §4 out-of-workspace realisation), P3.8 (§2.4 teardown, §7 caches).

---

## 9. Windows — the token backend (amendment, 2026-07-22)

**Owner ADR:** [ADR 0020](../adr/0020-windows-confinement-is-a-low-integrity-token-and-the-box-is-a-disk-label.md).
This section is the Windows half of §2.3's backend obligations, §5's capability table and §6's probe
harness. It is an **amendment**, not a renegotiation: §2.2's prepare-in-place shape, §4's Resolution
and §5's `FSWrite`-only `AutoEligible()` are unchanged. ADR 0020 carries the reasoning and the
rejected alternatives; what follows is only what an implementer must build to.

### 9.1 Disposition row

Windows is not a new column in §4's table — it is a **host that fills in the existing
`subproc (caps sufficient) → confine` cell** for the first time. Before this section a Windows host
always took the `caps insufficient → gate` row; at or above the version floor it now takes the
`confine` row, with §4's precomputed `ErrConfinementUnavailable` fallback carrying the remainder.

| host | `Capabilities()` | Auto · `confine=true`, subproc | `Confine` failure mode |
|---|---|---|---|
| Windows ≥ build 17763, token minted | `{FSWrite: true, NetworkEgress: false}` | **confine** (§4 row 4) | per-run labelling failure ⇒ `ErrConfinementUnavailable` ⇒ forced `Gate` |
| Windows ≥ 17763, token mint failed | `{false, false}` | **gate** (§4 row 5) | never invoked |
| Windows < build 17763 | `denyConfiner` ⇒ `{false, false}` | **gate** (§4 row 5) | never invoked |

The degradation notice (`probe.DegradedNotice`) therefore vanishes on a capable host and persists
verbatim below the floor — no new wording, no new surface.

### 9.2 Backend obligations

- **`Confine` sets `cmd.SysProcAttr.Token` and nothing else on the cmd.** `cmd.Path` and `cmd.Args`
  are untouched; there is no argv sentinel, no helper mode, and no `confined_exec_windows.go`.
  `maybeDispatchConfinedExec` gains no Windows arm.
- **The token** is a `CreateRestrictedToken(…, DISABLE_MAX_PRIVILEGE, …)` copy of the process token,
  relabelled to **Low** integrity (`SetTokenInformation` / `TokenIntegrityLevel` /
  `CreateWellKnownSid(WinLowLabelSid)`). The **fence is the mandatory integrity check**; the
  privilege strip is defence in depth. It is minted **once at construction** and reused — it carries
  no path policy, so it is box-independent, which also settles handle ownership under
  prepare-in-place.
- **The box is a label on the disk.** `WorkspaceRoot ∪ WritablePaths`, collapsed to a minimal set of
  non-overlapping roots, are labelled `S:(ML;OICI;NW;;;LW)` **recursively over existing contents**
  (inheritance covers new objects only). The pass is **memoised per box** — once per session, not
  per command — and is the one piece of I/O §2.2 now permits.
- **Guardrails:** a volume root, `%SystemRoot%`, `%ProgramFiles%`/`%ProgramFiles(x86)%` or the
  user-profile root is **refused** with `ErrConfinementUnavailable`, never labelled.
- **Teardown reverts the labels.** The backend implements `io.Closer`; the composition root defers it
  beside its existing `Close()` calls. **`domain.Confiner` does not change** — the hook is an
  optional-interface assertion. A **journal** written under the apogee home *before* the first label
  makes an interrupted cleanup recoverable by the next `NewConfiner()` and visible to
  `apogee probe host`.
- **Construction performs no disk I/O**, with the single exception of the recovery pass above —
  which is why the selector has two spellings. `NewConfiner()` is the SESSION constructor and
  finishes an outstanding restore; `NewReportConfiner()` is what `cmd/apogee/probe.go` builds, and
  it skips recovery so the host report stays free/offline/read-only as ADR 0021 §1 pins it, and so
  the residue line reports the outstanding journal instead of consuming it (ADR 0020 §2). The
  other three backends define `NewReportConfiner()` as `NewConfiner()` verbatim: nothing about
  constructing them touches the user's disk.
- **`NetworkEgress` is false and a network-deny box fails closed** — a non-empty `NetworkAllow`
  yields `ErrConfinementUnavailable`, mirroring `landlock_linux.go`'s `networkDenyDecision`.
- **Teardown of the process tree is §2.4's Windows half** (the Job Object, owned by the execution
  tools, not by this backend); the backend sets no `SysProcAttr` field other than `Token`.
- **Known gap, fail-closed:** a `CreateProcessAsUser` refusal (`ERROR_PRIVILEGE_NOT_HELD`) happens at
  `cmd.Start()`, after `Confine` has returned, so it surfaces as the tool's run error. The command
  **fails**; it does not run unconfined.

### 9.3 Probe expectations

`internal/platform/confinetest` is POSIX-shaped and must be widened, not assumed:

1. **`sh -c` (`:130`, `:143`, `:160`, `:170`) does not exist on stock Windows** — add a `cmd /c` arm,
   preferably by asking `platform.Current().Command(line)` for the argv rather than hard-coding a
   shell, with a per-OS write line (`printf x > <p>` vs `echo x> <p>`) and platform quoting.
   `assertDenied`'s "non-zero exit **and** no file" holds unchanged: MIC denies the redirect's
   `CreateFile`, `cmd` prints "Access is denied." and exits non-zero.
2. **Row #4's target (`:60`) ports as code but not as intent.** `os.UserHomeDir()` already resolves
   `%USERPROFILE%`; `.ssh` is simply not a meaningful Windows credential path, so the row should be
   worded as what it actually claims — *a path under the user profile, outside the box*.
3. **Row #6 is asserted on Windows**, per §6.2's amendment.
4. **Rows #9/#10** are the Windows-only additions: labels restored after teardown, and a network-deny
   box refused.
5. `t.TempDir()` cleanup survives the labelling — the test process is Medium and writing *down* is
   permitted.

### 9.4 Acceptance checklist (the Windows counterpart of §6.3)

Done when: `Capabilities()` is honest at/above and below the floor (below ⇒ `denyConfiner` and the
unchanged degradation notice); `confinetest.Probe` passes #1–#6 **natively**; #9/#10 pass; a box on a
SACL-less filesystem yields `ErrConfinementUnavailable` and §4's forced `Gate`; `ProbeNetwork` skips;
the labels are provably reverted after teardown and an interrupted run is recoverable from the
journal; `make cross` + `GOOS=windows go vet ./...` green; the other OSes' backends untouched.

## 10. Hook-time subprocess execution (amendment, 2026-08-03)

**Owner finding:** the 2026-08-01 audit — "`autofix` spawns an external formatter outside every
execution guard". This section is the hook-point half of §2's execution contract and §4's
Resolution. It is an **amendment**, not a renegotiation: §2.2's prepare-in-place shape and §4's
verdict table are unchanged; what is new is the seam by which a **hook** — which never reaches §4
at all — learns whether it may spawn a subprocess, and inside which box.

### 10.1 Why a hook needs its own seam

§4 decides one **tool call**: dispatch gathers the facts, `resolve()` returns a verdict, and the
executor carries it out — Approval, Confine, audit trip and all. A Mechanism firing at a hook point
is on none of that path. It runs inside the Turn, between the loop's own steps, with no call to
resolve, no Approver to consult (the Approver is consulted after the stream closes, per call, and a
post-response hook has no way to open a gate of its own) and no verdict to execute. So a hook that
shells out is, by construction, outside every guard §4 provides.

The fix is a **capability token on the context**, granted by the engine, mirroring how §2.2 hands a
subprocess tool its `Confinement` handle:

```go
type SubprocessPermit struct{ Confinement *Confinement }

func WithSubprocessPermit(ctx context.Context, p SubprocessPermit) context.Context
func SubprocessPermitFromContext(ctx context.Context) (SubprocessPermit, bool)
```

Both live in `internal/domain/confinement.go`, beside the `Confinement` helpers, so a Mechanism
(`internal/mechanisms`) reads them without importing the root facade (ADR 0010).

### 10.2 The three states, and why absence means refusal

| permit | meaning for the hook |
|---|---|
| **absent** (`ok == false`) | **may not spawn a subprocess at all** |
| **present**, `Confinement == nil` | spawn **unfenced** — the Auto + `confine-to-workspace: false` "I am the sandbox" opt-in |
| **present**, `Confinement != nil` | `Confiner.Confine(ctx, box, cmd)` first, then run; a `Confine` error — `ErrConfinementUnavailable` included — **skips the spawn**, never falls back to unfenced |

The asymmetry with `ConfinementFromContext` is deliberate and load-bearing. There, an absent handle
means *run unconfined*, because dispatch has already resolved a verdict for that call: the missing
handle records a decision that was **taken** (the Run row). Here, nothing resolves ahead of a hook,
so a missing permit records that **nobody ever authorised** the spawn — and the only safe reading of
an unauthorised subprocess is refusal. A hook point with no permit installed therefore keeps the
default posture (may not spawn) with no further code.

### 10.3 The ladder row the engine installs

`(*Agent).hookExecutionCtx` (`internal/agent/dispatch.go`) is the hook-time analogue of
`resolveLadderAuto`'s `classSubprocess` row:

| effective mode | `confine-to-workspace` | fs confinement caps | installed |
|---|---|---|---|
| not Auto | — | — | **nothing** (Plan forbids command execution; Ask-Before / Allow-Edits gate a command behind an Approval a hook cannot open) |
| Auto | off | — | permit, **nil** `Confinement` |
| Auto | on | available | permit carrying `&Confinement{Confiner, Box}` |
| Auto | on | unavailable | **nothing** — "confine if you can, gate if you can't" has no gate to fall back to here, so the surface closes |

The mode is read through `effectiveMode()`, never `Mode()`, so a sub-agent whose parent tightened
mid-delegation loses the permit exactly as it loses the matching tool verdict (ADR 0013). The box is
built from the same three `Config` fields `resolutionInput` uses — `WorkspaceDir`,
`ConfineWritablePaths`, `ConfineNetworkAllow` — so a hook-spawned process is fenced identically to a
subprocess tool's.

### 10.4 Scope

The permit is installed at **post-response only**, once per cascade, ahead of both the catalogued
and the experimental hooks (`runPostResponseHooks`, `internal/agent/hookrun.go`). Pre-request,
pre-tool-exec and post-tool-result get no permit, which under §10.2 keeps their existing "may not
spawn" posture — the intended one. Widening to another hook point is a deliberate act: install the
permit there and record the row here.
