package agent

// Swapping the Agent's model profile mid-session (ADR 0037). The Model profile (CONTEXT: Model
// profile) says how the bound model speaks the wire — which tool-call format it emits and which
// inline thinking channel it hides its reasoning in — and which tools it is offered at all (the
// roster axis, ADR 0057) — and it is a PER-MODEL binding (ADR 0044):
// the tag shape is a fact of the model's chat template, not a dialect the human picked, so an
// observed model change re-resolves the profile and carries it in through Rebind. SetProfile is
// the other door onto the same swap — the one a settings or config edit takes when the profile
// changed under a STABLE model.
//
// The engine takes the RESOLVED profile value, never a config file: which entry of the
// `model-profiles:` map, or of the shipped shape table, a profile comes from is a
// composition-root question (cmd/apogee resolves it per model), and the engine stays wire-silent
// about the host's configuration (ADR 0031) — the same posture Rebind takes for the rest of the
// per-model bindings and SwapTools for the tool set.

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
// content stripper, the profile the emit seam renders its wire-only tool menu from, and the tool
// roster that menu is drawn over (ADR 0057). It is the
// SAME-MODEL door for a profile change: the config's `model-profiles:` map resolved to a different
// profile for the model already bound. A profile that changes BECAUSE the model changed rides
// RebindSpec instead (ADR 0044), and both doors run the same applyProfile, so they cannot disagree
// about what a swap does.
//
// Idle-only, like Rebind and SwapTools: it refuses mid-Exchange (errSetProfileInExchange, which
// matches domain.ErrInputPending) and the host either applies the change at the next terminal
// boundary or lets the user re-commit the edit, since the value is already persisted. That
// discipline IS the synchronization: every read of these three fields — the stripper and text
// parser in the response seam, cfg.Profile in toolInstructions — runs inside Step on the host's
// worker goroutine, and the boundary the host crosses to call this establishes happens-before in
// both directions, so the hot path needs no lock (ADR 0011's idle-only engine-call class).
//
// Validate-then-commit: the profile is translated into its two collaborators FIRST (applyProfile's
// processing.ParserFor, exactly the construction path), so a profile naming an unknown tool-call
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
// approvals, the confinement flag and the per-model bindings all outlive a dialect change. The
// resolved TOOLS no longer do, since ADR 0057 made the roster the profile's third axis: a profile
// with roster deltas re-composes the engine's own default set through applyRoster, exactly as a
// model switch does — a set the host injected or swapped in is still untouched. Content ALREADY
// parsed keeps whatever the old profile made of it: this changes how the next response is read,
// never how the last one was.
func (a *Agent) SetProfile(profile domain.ModelProfile) error {
	if a.turns.inExchange {
		return errSetProfileInExchange
	}
	return a.applyProfile(profile)
}

// applyProfile installs a resolved profile on the seams that read it — the parse seam's tool-call
// parser and content stripper, cfg.Profile, which the emit seam renders its wire-only tool menu
// from, and the resolved tool set, which the profile's roster axis composes (applyRoster, ADR
// 0057). It is the ONE place that replacement exists (ADR 0044 decision 6):
// both doors onto a profile change call it, so a model switch (Rebind) and a config edit
// (SetProfile) can never install a profile two different ways.
//
// Validate-then-commit: the translation runs first, so a profile processing.ParserFor refuses
// leaves every one of those fields, the tool set included, exactly as it was. The idle-only
// refusal belongs to the CALLERS
// rather than here — each names the thing the user actually did (errSetProfileInExchange on an
// edit, ErrInputPending on a rebind) — and every caller is past that gate by the time it arrives.
func (a *Agent) applyProfile(profile domain.ModelProfile) error {
	textParser, stripper, err := processing.ParserFor(profile)
	if err != nil {
		return err
	}

	// Commit — from here on nothing can fail.
	a.textParser = textParser
	a.stripper = stripper
	a.cfg.Profile = profile
	a.applyRoster()
	return nil
}

// applyRoster re-assembles the tool set for the profile now on cfg — the roster axis ADR 0057 adds
// to the Model profile, which makes WHICH TOOLS a session offers a per-model binding rather than a
// launch-time constant. It is applyProfile's third seam and is called from inside it, so the roster
// travels with the other two axes through the one swap both doors run: a model switch (Rebind) and
// a config edit under a stable model (SetProfile) can no more disagree about a roster than they can
// about a stripper.
//
// It cannot fail — assembling the default registry has no error path — so it commits in place
// rather than validating first, and it is called after the parse seam is already committed.
//
// It is a no-op unless the ENGINE composed the set (ownsToolSet): an injected Config.Tools is the
// host's authority verbatim and a SwapTools set is the host's own assembly, so under either the
// roster axis is simply not this layer's to apply — the host folds its deltas in where it builds,
// and the tools of such a session stand across a switch exactly as they always have.
func (a *Agent) applyRoster() {
	if !a.ownsToolSet {
		return
	}
	a.tools = defaultRoster(a.cfg)
}
