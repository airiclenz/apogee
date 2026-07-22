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
// backends (seatbelt / landlock / AppContainer) live in internal/platform.
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
// beyond these two).
type ConfinementCaps struct {
	FSWrite       bool
	NetworkEgress bool
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
