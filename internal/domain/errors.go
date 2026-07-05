package domain

import "errors"

// ----------------------------------------------------------------------------
// Sentinel errors (re-exported as vars by the root facade)
// ----------------------------------------------------------------------------

var (
	// ErrAutoUnavailable is returned by New when Mode==Auto but the Confiner cannot
	// satisfy the Auto gate — missing, or no filesystem-write confinement on this host.
	// Under ADR 0012 the gate is FSWrite-only (the network is open by default), so this
	// is now CONDITIONAL: a host with fs-confinement (Linux kernel ≥5.13) enters Auto;
	// only a host without it is refused. The unfenceable subprocess surface then falls
	// back to Approval rather than refusing Auto outright (confinement-execution-contract §5).
	ErrAutoUnavailable = errors.New("apogee: auto mode requires filesystem-write confinement, unavailable on this host")

	// ErrConfinementUnavailable is the runtime "confine if you can, gate if you can't"
	// safety net (ADR 0012; confinement-execution-contract §2.2): a Confiner backend
	// that finds it cannot establish the requested box for a subprocess returns it, and
	// the dispatch disposition falls back to Approval rather than running the call
	// unconfined. Distinct from ErrAutoUnavailable, which gates Auto at construction.
	ErrConfinementUnavailable = errors.New("apogee: confinement unavailable on this host")

	// ErrOrderingCycle is returned by New / registry Add when Mechanism ordering
	// constraints form a cycle — it must fail loudly at startup (ADR 0003).
	ErrOrderingCycle = errors.New("apogee: mechanism ordering constraints contain a cycle")

	// ErrIncompatibleMechanisms is returned by New when two registered Mechanisms declare
	// each other incompatible (MechanismDescriptor.IncompatibleWith) — they must never
	// co-fire, so registering both is a configuration error that fails loudly at startup
	// (ADR 0003), the same posture as ErrOrderingCycle.
	ErrIncompatibleMechanisms = errors.New("apogee: incompatible mechanisms registered together")

	// ErrMissingRequirement is returned by New when a registered Mechanism declares a required
	// peer (MechanismDescriptor.Requires) that is not itself registered — the two are benched as
	// a stack, so enabling one without the other is a configuration error that fails loudly at
	// startup (ADR 0003 posture, ADR 0014 §4), the dual of ErrIncompatibleMechanisms.
	ErrMissingRequirement = errors.New("apogee: a required mechanism is not registered")

	// ErrUnknownMechanism is wrapped by mechanisms.Build (and, through it, agent construction from
	// Config.EnableMechanisms) when a named Mechanism ID is not in the catalogue — a typo'd or
	// deferred ID fails loudly rather than silently disabling a Mechanism (ADR 0015 §4). The
	// wrapping error still names the known IDs; this sentinel makes the condition matchable with
	// errors.Is (locked decision 5).
	ErrUnknownMechanism = errors.New("apogee: unknown mechanism")

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
