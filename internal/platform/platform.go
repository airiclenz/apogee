package platform

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/airiclenz/apogee/internal/domain"
)

// Shell abstracts how a command line is handed to the operating system's shell —
// POSIX wraps it in `sh -c`, Windows in `cmd /c`. The terminal tool (Phase 3) is
// the first real caller; the Phase-0 capstone harness (P0.6) needs none of these
// methods, so the surface is deliberately minimal.
//
// TODO(phase-5): widen to the real surface — environment-scoped execution,
// executable lookup (exec.LookPath semantics differ per OS) and argument
// quoting. Kept to one method here so the seam exists without pre-designing the
// full abstraction (plan §P0.5: "do not design the full shell abstraction").
// Phase 3 shipped the terminal tool without needing the wider surface; it is the
// Windows backend (Phase 5) that forces it.
type Shell interface {
	// Command returns the argv that runs line through the platform shell, e.g.
	// {"sh", "-c", line} on POSIX. The caller wires the result into os/exec.
	Command(line string) []string
}

// Path abstracts the path semantics that the standard library's path/filepath
// does not settle on its own (chiefly the executable suffix). Phase 0 needs none
// of these methods; Phase 3 (tool sandboxing) is the first real caller.
//
// TODO(phase-5): widen to the real surface — case-folded containment for
// ConfinementBox.WritablePaths (case-insensitive on Windows) and PATH lookup.
type Path interface {
	// ExecExt returns the filename extension the platform appends to
	// executables ("" on POSIX, ".exe" on Windows).
	ExecExt() string
}

// Host is the per-OS platform facility: shell invocation plus path semantics.
// Current returns the implementation selected at build time for the target OS
// (POSIX real, Windows stub — plan §P0.5). It is an interface, not a concrete
// type, precisely because the implementation is chosen by build tag.
type Host interface {
	Shell
	Path
}

// denyConfiner is the no-confinement backend. It enforces nothing: Capabilities
// reports neither fs-write nor network-egress confinement, so AutoEligible is false.
// It is the host backend on OSes without a real Confiner (Windows until Phase 5), and
// the seam the P0.6 harness used to exercise New's Auto gate before the real backends
// landed. Because it reports {false, false}, the dispatch disposition gates the
// subprocess surface rather than handing it a cmd to confine; if a cmd is passed
// anyway (a caller that skipped the caps check), Confine honestly reports
// ErrConfinementUnavailable — "confine if you can, gate if you can't" (ADR 0012).
type denyConfiner struct{}

// Capabilities reports a backend that can enforce nothing — both fs-write and
// network-egress are false, so this backend never satisfies the Auto gate.
func (denyConfiner) Capabilities() domain.ConfinementCaps {
	return domain.ConfinementCaps{FSWrite: false, NetworkEgress: false}
}

// Confine cannot prepare a confined command — this backend enforces nothing — so it
// returns ErrConfinementUnavailable rather than running cmd unconfined. The dispatch
// disposition checks Capabilities() first and never reaches here in normal flow
// (confinement-execution-contract §2.2/§2.3).
func (denyConfiner) Confine(_ context.Context, _ domain.ConfinementBox, _ *exec.Cmd) error {
	return fmt.Errorf("%w: no confinement backend on this host", domain.ErrConfinementUnavailable)
}

// NewDenyConfiner returns the no-confinement backend. It enforces nothing and never
// satisfies the Auto gate. It returns domain.Confiner — the same type the root
// re-exports as apogee.Confiner (ADR 0010), so callers in either package assign it
// interchangeably.
func NewDenyConfiner() domain.Confiner { return denyConfiner{} }

// The stub must satisfy the Confiner contract at compile time.
var _ domain.Confiner = (*denyConfiner)(nil)
