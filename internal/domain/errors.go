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

	// ErrOrderingCycle is returned by New / registry Add when Mechanism ordering
	// constraints form a cycle — it must fail loudly at startup (ADR 0003).
	ErrOrderingCycle = errors.New("apogee: mechanism ordering constraints contain a cycle")

	// ErrSessionVersion is returned by Resume / DecodeSession for a snapshot whose
	// schema version this build does not understand.
	ErrSessionVersion = errors.New("apogee: unsupported session schema version")

	// ErrInputPending is returned by Submit when an Exchange is already in progress.
	ErrInputPending = errors.New("apogee: cannot submit input mid-exchange")
)
