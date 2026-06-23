package platform

import (
	"context"

	"github.com/airiclenz/apogee/internal/domain"
)

// Shell abstracts how a command line is handed to the operating system's shell —
// POSIX wraps it in `sh -c`, Windows in `cmd /c`. The terminal tool (Phase 3) is
// the first real caller; the Phase-0 capstone harness (P0.6) needs none of these
// methods, so the surface is deliberately minimal.
//
// TODO(phase-3): widen to the real surface — environment-scoped execution,
// executable lookup (exec.LookPath semantics differ per OS) and argument
// quoting. Kept to one method here so the seam exists without pre-designing the
// full abstraction (plan §P0.5: "do not design the full shell abstraction").
type Shell interface {
	// Command returns the argv that runs line through the platform shell, e.g.
	// {"sh", "-c", line} on POSIX. The caller wires the result into os/exec.
	Command(line string) []string
}

// Path abstracts the path semantics that the standard library's path/filepath
// does not settle on its own (chiefly the executable suffix). Phase 0 needs none
// of these methods; Phase 3 (tool sandboxing) is the first real caller.
//
// TODO(phase-3): widen to the real surface — case-folded containment for
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

// denyConfiner is the Phase-0 stub Confiner backend (plan §P0.5). It enforces
// nothing: Capabilities reports neither fs-write nor network-egress
// confinement, so AutoEligible is false and New rejects Auto mode against
// it (ADR 0004). Confine runs fn unchanged. The real backends (seatbelt /
// landlock / AppContainer) land in Phase 3; this stub exists so New's Auto gate
// can be exercised by the P0.6 harness before any of them do.
type denyConfiner struct{}

// Capabilities reports a backend that can enforce nothing — both fs-write and
// network-egress are false, so this backend never satisfies the Auto gate.
func (denyConfiner) Capabilities() domain.ConfinementCaps {
	return domain.ConfinementCaps{FSWrite: false, NetworkEgress: false}
}

// Confine runs fn unchanged: the stub applies no confinement box (Phase 0).
func (denyConfiner) Confine(ctx context.Context, _ domain.ConfinementBox, fn func(context.Context) error) error {
	return fn(ctx)
}

// NewDenyConfiner returns the Phase-0 stub Confiner backend (plan §P0.5). It
// enforces nothing and never satisfies the Auto gate, so it lets New's Auto-mode
// rejection (ADR 0004) be tested before the real backends land. It returns
// domain.Confiner — the same type the root re-exports as apogee.Confiner (ADR 0010),
// so callers in either package assign it interchangeably.
func NewDenyConfiner() domain.Confiner { return denyConfiner{} }

// The stub must satisfy the Confiner contract at compile time.
var _ domain.Confiner = (*denyConfiner)(nil)
