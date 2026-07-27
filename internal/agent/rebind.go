package agent

// Rebinding the Agent's per-model bindings (ADR 0024). The Upstream's loaded model can change
// under a running session — the host's heartbeat observes it — and every per-model decision the
// engine made at construction (the wire model id, the system-prompt template, the validated
// Mechanism set, the context window) then describes a model that is no longer there. Rebind is
// the one entry point that swaps all of them together, and the deferred-binding relaxation
// (Config.Model may start empty, errNoModelBound guards Submit) is what lets a session start
// before any model is known at all.

import (
	"errors"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/prompt"
)

var (
	// errRebindMissingModel refuses a spec that names no model. Rebind BINDS a model; unbinding
	// is not a thing the host asks for (an unreachable server is an observation the host reports,
	// not a reason to tear the engine's bindings down mid-session).
	errRebindMissingModel = errors.New("apogee: Rebind: spec.Model is required")

	// errRebindPrebuiltMechanisms refuses a rebind on an Agent constructed with a host-supplied
	// Config.Mechanisms registry. Rebind rebuilds the catalogued Mechanisms from scratch for the
	// new model, and it cannot rebuild hooks the host handed over pre-built — merging into the old
	// registry would double-register, and dropping it would silently disarm the host's own hooks.
	// An honest refusal instead. The TUI wiring only ever sets Config.EnableMechanisms, so it
	// never meets this; an embedder that pre-builds a registry keeps a fixed model.
	errRebindPrebuiltMechanisms = errors.New("apogee: Rebind: cannot rebuild a host-supplied Config.Mechanisms registry")

	// errNoModelBound refuses a Submit on an Agent that has no model bound yet — the cost of the
	// deferred binding New now allows. It is a request-time gate, not a construction one: a
	// model-less Agent is a legitimate, fully-usable object (the host can clear context, restore a
	// session, switch mode) right up to the point where a request would have to name a model.
	errNoModelBound = errors.New("apogee: no model is bound — Rebind to the model the Upstream serves before submitting")
)

// RebindSpec carries the per-model bindings the composition root re-resolved for an observed
// model change (ADR 0024). It is computed WHOLE by the caller: which system-prompt template a
// model gets (ADR 0023) and which validated Mechanism set matches it (ADR 0016) are config
// questions resolved in cmd/apogee, so the engine neither reads config.yaml nor re-derives an
// identity — it applies what it is handed, atomically.
//
// It is deliberately NOT the whole Config: mode, approvals, confinement, tools, the model
// profile (`model-profile` is global, not per-model — see Config.Profile) and the conversation
// are session state that a model switch has no business resetting.
type RebindSpec struct {
	// Model is the model id to send on the wire. Required — an empty spec is refused.
	Model string
	// SystemPrompt is the re-selected prompt TEMPLATE for the new model; "" ⇒ no system prompt.
	SystemPrompt string
	// MaxContextTokens is the BOUND context window in tokens — the caller has already applied a
	// configured `context-window:` pin over the observed one. 0 ⇒ unknown, which leaves the
	// Budget and automatic Compaction inactive exactly as an undiscovered window does.
	MaxContextTokens int
	// EnableMechanisms is the catalogued Mechanism set re-resolved for the new model. It replaces
	// the current set outright; empty arms nothing (the default-off posture).
	EnableMechanisms []domain.MechanismID
}

// Rebind swaps the Agent's per-model bindings at a quiescent boundary — the wire model, the
// system-prompt template, the context window the Budget and Compaction measure against, and the
// catalogued Mechanism set — and rebinds the provider client's wire model with them. It is the
// engine half of the heartbeat's observed model change (ADR 0024).
//
// Idle-only, like ClearContext and RestoreSession: it refuses mid-Exchange (ErrInputPending), and
// the host applies a change observed mid-Exchange at the terminal boundary instead. That
// discipline IS the synchronization — the loop's cfg reads all run inside Step/Compact on the
// host's worker goroutine, and the boundary the host crosses to call this establishes
// happens-before in both directions — so the hot loop needs no lock (ADR 0011's idle-only
// engine-call class, extended).
//
// Validate-then-commit: the fresh Mechanism registry is built and passed through the ordering,
// incompatibility and requirements gates BEFORE anything is mutated, so a spec the new model
// cannot satisfy leaves every existing binding, and the whole conversation, exactly as it was.
//
// What stands: the conversation and Turn counters, the autonomy mode, session approvals, the
// confinement flag, the resolved tools, and the model profile with its parse-seam collaborators.
// What resets: the token estimator (its chars→token calibration described the OLD model) and the
// compaction saturation latch (it was judged against the old window).
func (a *Agent) Rebind(spec RebindSpec) error {
	if a.turns.inExchange {
		return domain.ErrInputPending
	}
	if spec.Model == "" {
		return errRebindMissingModel
	}
	if a.cfg.Mechanisms != nil {
		return errRebindPrebuiltMechanisms
	}
	if err := prompt.Validate(spec.SystemPrompt); err != nil {
		return err
	}

	// Build against a COPY of the config: nothing below can mutate the live Agent, and
	// buildEnabledMechanisms sees the new model — so deriveDeps re-keys the Library identity
	// fingerprint on it rather than on the model that just went away.
	next := a.cfg
	next.Model = spec.Model
	next.SystemPrompt = spec.SystemPrompt
	next.Context.MaxContextTokens = spec.MaxContextTokens
	next.EnableMechanisms = spec.EnableMechanisms

	registry := domain.NewMechanismRegistry()
	if err := buildEnabledMechanisms(next, registry); err != nil {
		return err
	}
	if err := registry.ValidateOrdering(); err != nil {
		return err
	}
	if err := registry.ValidateIncompatibilities(); err != nil {
		return err
	}
	if err := registry.ValidateRequirements(); err != nil {
		return err
	}

	// Commit — from here on nothing can fail.
	a.cfg = next
	a.registry = registry
	// The provider client's configured model WINS over the request's (provider.buildBody), so the
	// wire model moves only if the Responder is told. It is an optional interface rather than a
	// widening of the Responder seam: a fake responder in a test simply does not implement it, and
	// a Responder with no notion of a model id has nothing to rebind.
	if binder, ok := a.upstream.(interface{ SetModel(string) }); ok {
		binder.SetModel(spec.Model)
	}
	a.tokens = apogeectx.NewTokenEstimator()
	a.compactSat = false
	return nil
}
