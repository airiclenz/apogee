package domain

import "errors"

// ----------------------------------------------------------------------------
// Sentinel errors (re-exported as vars by the root facade)
// ----------------------------------------------------------------------------

var (
	// ErrAutoUnavailable is returned by New when Mode==Auto but the Confiner cannot
	// satisfy the Auto gate (missing, or insufficient capabilities — e.g. Linux
	// kernel < 6.7). Auto degrades to Ask-Before; it never runs unconfined (ADR 0004).
	ErrAutoUnavailable = errors.New("apogee: auto mode requires fs-write and network confinement")

	// ErrConfinementUnavailable is the runtime "confine if you can, gate if you can't"
	// safety net (ADR 0012; confinement-execution-contract §2.2): a Confiner backend
	// that finds it cannot establish the requested box for a subprocess returns it, and
	// the dispatch disposition falls back to Approval rather than running the call
	// unconfined. Distinct from ErrAutoUnavailable, which gates Auto at construction.
	ErrConfinementUnavailable = errors.New("apogee: confinement unavailable on this host")

	// ErrOrderingCycle is returned by New / registry Add when Mechanism ordering
	// constraints form a cycle — it must fail loudly at startup (ADR 0003).
	ErrOrderingCycle = errors.New("apogee: mechanism ordering constraints contain a cycle")

	// ErrSessionVersion is returned by Resume / DecodeSession for a snapshot whose
	// schema version this build does not understand.
	ErrSessionVersion = errors.New("apogee: unsupported session schema version")

	// ErrInputPending is returned by Submit when an Exchange is already in progress.
	ErrInputPending = errors.New("apogee: cannot submit input mid-exchange")

	// ErrDuplicateTool is returned by ToolRegistry.Register when a tool with the same
	// Name is already registered — the name is the model's stable handle, so a
	// collision is a configuration error, not a silent overwrite.
	ErrDuplicateTool = errors.New("apogee: a tool with this name is already registered")

	// ErrInvalidTool is returned by ToolRegistry.Register for a tool that cannot be
	// addressed — currently an empty Name.
	ErrInvalidTool = errors.New("apogee: invalid tool")
)
