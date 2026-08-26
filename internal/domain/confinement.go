package domain

import (
	"context"
	"os/exec"
)

// ----------------------------------------------------------------------------
// Confinement (ADR 0012, supersedes ADR 0004 — confinement attaches to blast radius;
// see docs/design/confinement-execution-contract.md §2 for the execution contract)
// ----------------------------------------------------------------------------
//
// The Confiner interface and its value types live in domain (resolving TDD §6.1):
// the root re-exports them as aliases so the interface stays public (the host injects
// it via Config) while its definition sits where both the loop and the backends
// (internal/platform) see it without importing root (ADR 0010).

// Confiner is the OS-level confinement facility for the unbounded subprocess surface
// (ADR 0012). The interface is PUBLIC because the host injects it via Config; the
// backends (seatbelt / landlock / a restricted low-integrity Windows token) live in
// internal/platform.
//
// Granularity is the single, all-OS subprocess (Linux landlock applied to the child
// after fork, before execve; macOS sandbox-exec wrapping the child). There is no
// in-process per-thread confinement — Apogee's own in-process writes are
// path-safety-bounded instead (ADR 0012's blast-radius split).
type Confiner interface {
	// Capabilities reports what this backend can actually enforce, here and now —
	// probed once at construction, never optimistic (confinement-execution-contract
	// §5). A kernel without landlock, or a macOS without sandbox-exec, reports
	// {false, false}, so the dispatch disposition gates the subprocess surface rather
	// than confining it.
	Capabilities() ConfinementCaps

	// Confine prepares cmd to execute confined to box, then RETURNS — it does not run
	// cmd (confinement-execution-contract §2.2). It rewrites cmd to launch under the
	// host OS confinement facility (macOS: exec under sandbox-exec -p <profile>; Linux:
	// interpose the landlock re-exec wrapper; Windows: hand CreateProcessAsUser a
	// restricted low-integrity token, leaving the argv untouched — ADR 0020) and sets
	// cmd.SysProcAttr so the caller's process-group kill reaches the wrapped child, or,
	// on Windows, so the child starts under that token. The caller has already wired
	// Stdin/Stdout/Stderr/Dir/Env and afterwards invokes cmd.Run()/Output(). The PARENT
	// process is never restricted.
	//
	// Confine is only invoked when Capabilities() reports box is enforceable on this
	// host (the disposition checks caps first, §4). ErrConfinementUnavailable is the
	// runtime safety net: a backend that finds it cannot establish the box returns it,
	// and the caller falls back to Approval ("confine if you can, gate if you can't").
	Confine(ctx context.Context, box ConfinementBox, cmd *exec.Cmd) error
}

// ConfinementCaps is the capability matrix a Confiner reports (ADR 0012 — extensible
// beyond the two enforcement bits).
type ConfinementCaps struct {
	FSWrite       bool
	NetworkEgress bool

	// Residuals names the write-class accesses this backend knowingly cannot fence on this
	// host while FSWrite is true, each named by its syscall; empty when the fence is complete.
	// Capability honesty (confinement-execution-contract §5): a backend that leaves an access
	// open says so rather than reporting a fence it does not have. It is disclosure only —
	// AutoEligible reads FSWrite alone, so a residual never blocks Auto, it only gets said.
	Residuals []string
}

// AutoEligible reports whether these capabilities satisfy the Auto gate. Under ADR 0012
// the network is open by default, so Auto requires filesystem-write confinement ONLY
// (was FSWrite && NetworkEgress under ADR 0004). NetworkEgress is still reported and
// matters only when a user opts back into network-deny via box.NetworkAllow. A host
// without fs-confinement is no longer refused Auto — it gates the subprocess surface
// instead (confinement-execution-contract §5).
func (c ConfinementCaps) AutoEligible() bool { return c.FSWrite }

// ConfinementBox is the confinement policy for a run. Default = workspace-write-only
// + network OPEN + per-project allowlist (ADR 0012). NetworkAllow is a TIGHTENING
// list: empty leaves the network open; non-empty opts the box into network-deny.
type ConfinementBox struct {
	WorkspaceRoot string
	WritablePaths []string
	NetworkAllow  []string // per-project tightening; empty = network open (ADR 0012)
}

// ConfinementBox is the full box this Config declares: the workspace root a subprocess is fenced
// to, the extra writable paths a confined toolchain needs — plus the session's ScratchDir when one
// is set — and the per-project network tightening list (confinement-execution-contract §7). It is
// the single place those Confine* fields (and ScratchDir) are folded into a box, so a new call
// site can no longer open a silent confinement hole by forgetting one of them. The returned slices
// alias Config's — except WritablePaths when a ScratchDir is folded in, which is then a fresh
// slice so the append can never scribble on the host's ConfineWritablePaths backing array; either
// way the box is read-only policy, never mutated by its readers. A caller that wants a
// deliberately NARROWER box calls this and then clears the field it does not want, keeping the
// divergence visible in one line (deriveDeps' exec fence, internal/agent/construct.go, is the only
// such caller today).
func (c Config) ConfinementBox() ConfinementBox {
	writable := c.ConfineWritablePaths
	if c.ScratchDir != "" {
		writable = append(append(make([]string, 0, len(c.ConfineWritablePaths)+1),
			c.ConfineWritablePaths...), c.ScratchDir)
	}
	return ConfinementBox{
		WorkspaceRoot: c.WorkspaceDir,
		WritablePaths: writable,
		NetworkAllow:  c.ConfineNetworkAllow,
	}
}

// Confinement is the handle a subprocess tool uses to confine the *exec.Cmd it builds:
// the Confiner backend plus the box to confine to. The dispatch disposition installs it
// into the Execute context (WithConfinement) for a subprocess tool the Auto disposition
// chose to run confined; the tool retrieves it (ConfinementFromContext) and calls
// Confine on its cmd before running it. Keeping the handle on the context — rather than
// in domain.Tool.Execute's signature — preserves the open Tool extension point (ADR 0002)
// while giving the subprocess tools (P3.8) exactly the contract's tool-builds-and-runs-
// the-cmd model (confinement-execution-contract §2.2).
type Confinement struct {
	Confiner Confiner
	Box      ConfinementBox
}

// confinementCtxKey is the unexported context key under which a Confinement handle rides.
type confinementCtxKey struct{}

// WithConfinement returns a context carrying conf, so a subprocess tool's Execute can
// retrieve the Confiner + box to confine the command it launches. The dispatch
// disposition calls this only when it has decided to run a subprocess tool confined.
func WithConfinement(ctx context.Context, conf Confinement) context.Context {
	return context.WithValue(ctx, confinementCtxKey{}, conf)
}

// ConfinementFromContext returns the Confinement handle installed by WithConfinement and
// whether one is present. ok is false when the call is not running under a confinement
// disposition (every mode other than Auto/confine-on for a subprocess tool), in which
// case the tool runs its command unconfined — the disposition has already ensured that
// path is only reached where unconfined execution is the intended outcome.
func ConfinementFromContext(ctx context.Context) (Confinement, bool) {
	conf, ok := ctx.Value(confinementCtxKey{}).(Confinement)
	return conf, ok
}

// SubprocessPermit is the hook-time counterpart of Confinement: the token a HOOK must find on
// its context before it may spawn a subprocess at all (docs/design/confinement-execution-contract.md
// §10). A hook runs outside the per-call Resolution — no verdict, no Approval, no audit trip — so
// the permit carries the ladder's answer to it instead, in three states:
//
//   - ABSENT (the default, and what a bare context yields): this hook may NOT spawn a subprocess.
//     Plan forbids command execution outright, and Ask-Before / Allow-Edits gate a command behind
//     an Approval that a post-response hook has no way to open, so refusal is the only honest
//     answer in every mode but Auto.
//   - PRESENT with a nil Confinement: run UNFENCED — the Auto + confine-to-workspace:false
//     "I am the sandbox" opt-in, mirroring resolveLadderAuto's unfenced row.
//   - PRESENT carrying a Confinement: confine the cmd to that box first (Confiner.Confine), and
//     skip the spawn entirely if the box cannot be established. Never fall back to unfenced.
//
// Absence means REFUSAL here, while an absent Confinement handle in ConfinementFromContext means
// "run unconfined". The asymmetry is deliberate: a tool reads its handle only after dispatch has
// already resolved a verdict for that call, so the missing handle records a decision that was
// taken; no such resolution runs ahead of a hook, so a missing permit records that nobody ever
// authorised the spawn.
type SubprocessPermit struct {
	// Confinement, when non-nil, is the box the permitted subprocess must be confined to before
	// it runs. Nil means the permit authorises an unfenced spawn.
	Confinement *Confinement
}

// subprocessPermitCtxKey is the unexported context key under which a SubprocessPermit rides.
type subprocessPermitCtxKey struct{}

// WithSubprocessPermit returns a context carrying p, authorising the hooks that run under it to
// spawn a subprocess on the terms p describes. Only the engine installs it, and only for the hook
// points whose ladder row permits execution (today: post-response — hookExecutionCtx,
// internal/agent/dispatch.go).
func WithSubprocessPermit(ctx context.Context, p SubprocessPermit) context.Context {
	return context.WithValue(ctx, subprocessPermitCtxKey{}, p)
}

// SubprocessPermitFromContext returns the SubprocessPermit installed by WithSubprocessPermit and
// whether one is present. ok is false on any context nobody granted a permit on — including a bare
// context.Background() — and a hook that reads false must not spawn a subprocess.
func SubprocessPermitFromContext(ctx context.Context) (SubprocessPermit, bool) {
	p, ok := ctx.Value(subprocessPermitCtxKey{}).(SubprocessPermit)
	return p, ok
}

// WriteEscapePermit is the write-time counterpart of SubprocessPermit (ADR 0049): the token the
// shared write funnel must find on its context before an in-process write may land OUTSIDE the
// workspace fence. It authorises exactly ONE resolved absolute path — the `writeTarget.Real` the
// approval pane disclosed — for the duration of ONE tool execution, and nothing else:
//
//   - ABSENT (the default, and what a bare context yields): the workspace fence ALONE governs the
//     write — today's behaviour, byte-for-byte, for every call nobody granted an escape to.
//   - PRESENT: the write argument re-resolves and may land only on an exact match with Real,
//     written through an os.Root pinned at that target's parent with the final component
//     non-following. Any divergence is an error, never a write.
//
// Only the engine mints it, and only where the ladder has already answered for this call: an
// approved WS-write-out Gate, the Auto + confine-to-workspace:false run verdict, and a target
// inside the box's declared writable paths (docs/design/confinement-execution-contract.md §4). A
// denied or refused call never carries one. The permit is a bound, not a widening: it names a
// single target rather than opening a directory, and it grants no read that the fence refuses.
type WriteEscapePermit struct {
	// Real is the resolved absolute path this permit authorises — the one the human was shown.
	// Empty means the permit authorises nothing: WriteEscapePermitFrom reports it absent.
	Real string
}

// writeEscapePermitCtxKey is the unexported context key under which a WriteEscapePermit rides.
type writeEscapePermitCtxKey struct{}

// WithWriteEscapePermit returns a context carrying p, authorising the tool execution that runs
// under it to write to p.Real outside the workspace fence. Installing a permit whose Real is empty
// REVOKES any permit an outer context granted — the value still rides, so an inner scope can never
// inherit an escape it was not itself granted.
func WithWriteEscapePermit(ctx context.Context, p WriteEscapePermit) context.Context {
	return context.WithValue(ctx, writeEscapePermitCtxKey{}, p)
}

// WriteEscapePermitFrom returns the WriteEscapePermit installed by WithWriteEscapePermit and
// whether one authorises anything. ok is false on any context nobody granted a permit on —
// including a bare context.Background() — and false too for a permit with an empty Real, so an
// unset target can never read as an escape. A caller that reads false applies the workspace fence
// alone.
func WriteEscapePermitFrom(ctx context.Context) (WriteEscapePermit, bool) {
	p, ok := ctx.Value(writeEscapePermitCtxKey{}).(WriteEscapePermit)
	if !ok || p.Real == "" {
		return WriteEscapePermit{}, false
	}
	return p, true
}
