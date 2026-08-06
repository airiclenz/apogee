package agent

// Swapping the Agent's model profile mid-session (ADR 0037). The Model profile (CONTEXT: Model
// profile) says how the configured model speaks the wire — which tool-call format it emits and
// which inline thinking channel it hides its reasoning in — and it is a GLOBAL setting, not a
// per-model binding: `model-profile` describes the human's chosen dialect, so Rebind deliberately
// leaves it alone when the Upstream's loaded model changes (RebindSpec's own doc). SetProfile is
// the separate, explicit door for changing it, opened by the settings surface's `model-profile`
// edit.
//
// The engine takes the RESOLVED profile value, never a config file: which YAML block a profile
// comes from is a composition-root question (cmd/apogee resolves it at startup and again on a
// settings reload), and the engine stays wire-silent about the host's configuration (ADR 0031) —
// the same posture Rebind takes for the per-model bindings and SwapTools for the tool set.

import (
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/processing"
)

// errSetProfileInExchange refuses a profile swap mid-Exchange. It WRAPS domain.ErrInputPending so
// the idle-only class stays matchable with errors.Is, and adds the reason for SwapTools' reason:
// this refusal is user-facing — the settings surface renders it on the row whose edit could not be
// applied, where the bare "cannot submit input mid-exchange" would name something the user never
// did.
var errSetProfileInExchange = fmt.Errorf("%w: the model profile can only be swapped between runs", domain.ErrInputPending)

// SetProfile replaces the Agent's model profile outright — the parse seam's tool-call parser and
// content stripper, and the profile the emit seam renders its wire-only tool menu from. It is the
// ONE door for a mid-session profile change; Rebind's exclusion of the profile stands (a model
// change is not a dialect change), so the two never fight over the same fields.
//
// Idle-only, like Rebind and SwapTools: it refuses mid-Exchange (errSetProfileInExchange, which
// matches domain.ErrInputPending) and the host either applies the change at the next terminal
// boundary or lets the user re-commit the edit, since the value is already persisted. That
// discipline IS the synchronization: every read of these three fields — the stripper and text
// parser in the response seam, cfg.Profile in toolInstructions — runs inside Step on the host's
// worker goroutine, and the boundary the host crosses to call this establishes happens-before in
// both directions, so the hot path needs no lock (ADR 0011's idle-only engine-call class).
//
// Validate-then-commit: the profile is translated into its two collaborators FIRST
// (processing.ParserFor, exactly the construction path), so a profile naming an unknown tool-call
// format or thinking style leaves the session parsing as it did rather than half-swapped. The
// emit half needs no validation of its own — processing.InstructionsFor refuses precisely the
// formats ParserFor refuses, which is what keeps toolInstructions' error path unreachable at
// runtime.
//
// Sub-agents see the new profile from the next spawn: newChildAgent copies this Agent's LIVE cfg
// and newAgent re-runs ParserFor over it, and a spawn happens mid-Exchange on the worker
// goroutine, where a swap is refused — so the two can never interleave.
//
// What stands: everything else. The conversation and Turn counters, the autonomy mode, session
// approvals, the confinement flag, the resolved tools and the per-model bindings all outlive a
// dialect change. Content ALREADY parsed keeps whatever the old profile made of it: this changes
// how the next response is read, never how the last one was.
func (a *Agent) SetProfile(profile domain.ModelProfile) error {
	if a.turns.inExchange {
		return errSetProfileInExchange
	}
	textParser, stripper, err := processing.ParserFor(profile)
	if err != nil {
		return err
	}

	// Commit — from here on nothing can fail.
	a.textParser = textParser
	a.stripper = stripper
	a.cfg.Profile = profile
	return nil
}
