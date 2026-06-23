package domain

import "context"

// ----------------------------------------------------------------------------
// Confinement (ADR 0004 — capability matrix; interface is public, backends internal)
// ----------------------------------------------------------------------------
//
// The Confiner interface and its value types live in domain (resolving TDD §6.1):
// the root re-exports them as aliases so the interface stays public (the host injects
// it via Config) while its definition sits where both the loop and the backends
// (internal/platform) see it without importing root (ADR 0010).

// Confiner is the OS-level confinement facility required for Auto mode (ADR 0004).
// The interface is PUBLIC because the host injects it via Config; the backends
// (seatbelt / landlock / AppContainer) live in internal/platform. Capabilities
// reports the matrix the Auto gate reads — Auto requires both fs-write and
// network-egress confinement, evaluated per tool.
type Confiner interface {
	// Capabilities reports what this backend can actually enforce, here and now
	// (e.g. Linux landlock < ABI v4 reports NetworkEgress == false).
	Capabilities() ConfinementCaps
	// Confine runs fn under the confinement box. A tool the backend cannot confine
	// (ExternalEffect / MCP) must not be passed here in Auto — it gates through
	// Approval instead (ADR 0004).
	Confine(ctx context.Context, box ConfinementBox, fn func(context.Context) error) error
}

// ConfinementCaps is the capability matrix a Confiner reports (ADR 0004 — extensible
// beyond these two).
type ConfinementCaps struct {
	FSWrite       bool
	NetworkEgress bool
}

// AutoEligible reports whether these capabilities satisfy the Auto gate — both
// fs-write and network-egress confinement (ADR 0004). There is no escape hatch.
func (c ConfinementCaps) AutoEligible() bool { return c.FSWrite && c.NetworkEgress }

// ConfinementBox is the confinement policy for a run. Default = workspace-write-only
// + network default-deny + per-project allowlist (ADR 0004).
type ConfinementBox struct {
	WorkspaceRoot string
	WritablePaths []string
	NetworkAllow  []string // per-project allowlist; empty = default-deny
}
