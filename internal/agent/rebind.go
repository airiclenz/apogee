package agent

// Rebinding the Agent's per-model bindings (ADR 0024). The Upstream's loaded model can change
// under a running session — the host's heartbeat observes it — and every per-model decision the
// engine made at construction (the wire model id, the system-prompt template, the validated
// Mechanism set, the context window) then describes a model that is no longer there. Rebind is
// the one entry point that swaps all of them together, and the deferred-binding relaxation
// (Config.Model may start empty, errNoModelBound guards Submit) is what lets a session start
// before any model is known at all.
//
// SwitchUpstream lives here too: moving the session to another SERVER is the same lifecycle
// concern one level up — it leaves the session unbound and lets the new Upstream's first
// observed model complete the move through Rebind, so there is still exactly one way to bind.

import (
	"errors"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/prompt"
	"github.com/airiclenz/apogee/internal/provider"
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
// It is deliberately NOT the whole Config: mode, approvals, confinement, tools and the
// conversation are session state that a model switch has no business resetting.
//
// Two fields are the stated exceptions to "per-model", MaxOutputTokens (ADR 0046's amendment) and
// ResponseReserveFraction: the reply ceiling and the share of the window that reply is given both
// describe the SERVER, and they ride here because neither the `max-output-tokens:` nor the
// `response-reserve:` pin has an engine setter of its own — the atomic commit a rebind already makes
// is the only door a live edit of either can take. Both are optional precisely so those exceptions
// cost the other callers nothing: a spec that says nothing about a bound leaves it exactly as it was.
//
// The model profile used to be on that list. This doc said "`model-profile` is global, not
// per-model — see Config.Profile; SetProfile is its separate, explicit door", and ADR 0044
// reverses exactly that clause: a tool-call format and a thinking-tag shape are facts OF the model
// (its chat template), not a dialect the human chose, so the profile joins the per-model bindings
// and a switch applies it atomically with them. SetProfile stays as the same-model door a config
// edit takes; both run the one applyProfile.
type RebindSpec struct {
	// Model is the model id to send on the wire. Required — an empty spec is refused.
	Model string
	// SystemPrompt is the re-selected prompt TEMPLATE for the new model; "" ⇒ no system prompt.
	SystemPrompt string
	// MaxContextTokens is the BOUND context window in tokens — the caller has already applied a
	// configured `context-window:` pin over the observed one. 0 ⇒ unknown, which leaves the
	// Budget and automatic Compaction inactive exactly as an undiscovered window does.
	MaxContextTokens int
	// MaxOutputTokens is the ceiling on ONE reply from the server this session is on — the bound
	// `servers:` entry's `max-output-tokens:` pin, carried as written (ADR 0046). It is the one
	// field here that is not a per-MODEL fact: the ceiling describes the SLOT, and it rides this
	// spec because the pin has no engine setter of its own, so the only door a live edit of it can
	// reach the engine through is the re-resolution the caller is already driving.
	//
	// A POINTER, and that is the whole contract: nil ⇒ this spec says NOTHING about the reply
	// ceiling and whatever is in force stands. It is SamplingParams's rule one layer in ("a nil
	// field leaves the loop's value untouched", domain/hooks.go) and it is what keeps a caller that
	// re-resolved only the per-model bindings from silently un-bounding a reply an entry pinned —
	// the failure a plain int would make the DEFAULT, since a spec that simply did not mention the
	// cap would clear it. A non-nil value is applied as written, the ZERO included: 0 is the
	// operator dropping the pin, and the engine derives the cap from the reply room the Budget
	// reserves again (maxOutputTokens, loop.go), never "no cap at all".
	MaxOutputTokens *int
	// ResponseReserveFraction is the share of the bound context window held back for ONE reply on the
	// server this session is on — the bound `servers:` entry's `response-reserve:` resolved over the
	// top-level key by the caller (config.ResolveResponseReserve). Like the ceiling above it is a fact
	// about the SLOT rather than about the model, and it rides this spec for the ceiling's reason: the
	// share has no engine setter of its own, so the re-resolution the caller is already driving is the
	// only door a live edit of it can reach the engine through.
	//
	// A POINTER on the ceiling's contract: nil ⇒ this spec says NOTHING about the split and whatever
	// share is in force stands — which is what keeps a caller that re-resolved only the per-model
	// bindings from silently re-dividing a window an entry pinned. A non-nil value is applied as
	// written, the ZERO included: 0 is "neither scope states a share", and internal/context.Allocate
	// hands the split back to its own built-in default rather than to the departed entry's number.
	ResponseReserveFraction *float64
	// EnableMechanisms is the catalogued Mechanism set re-resolved for the new model. It replaces
	// the current set outright; empty arms nothing (the default-off posture).
	EnableMechanisms []domain.MechanismID
	// Profile is the model profile re-resolved for the new model (ADR 0044): how it speaks the
	// wire, on the two axes of tool-call format and inline thinking channel. It replaces the
	// current profile outright, and the ZERO value is meaningful — native tool calls, no inline
	// thinking — so a model that matches no user entry and no shipped shape parses exactly as an
	// unprofiled session does. A profile the processing seam cannot translate fails the whole
	// spec, leaving every binding standing.
	Profile domain.ModelProfile
}

// Rebind swaps the Agent's per-model bindings at a quiescent boundary — the wire model, the
// system-prompt template, the context window the Budget and Compaction measure against, and the
// catalogued Mechanism set — and rebinds the provider client's wire model with them. It is the
// engine half of the heartbeat's observed model change (ADR 0024). A spec may carry two bounds that
// are NOT per-model, the reply ceiling (ADR 0046) and the share of the window reserved for that
// reply, for the reason their fields state: neither pin has a setter of its own, so a live edit of
// them reaches the engine through this same atomic commit.
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
// confinement flag, and the resolved tools — and the reply ceiling and its reserve share too, unless
// the spec names them: those bounds describe the SERVER, so a spec silent about one (a nil
// MaxOutputTokens or ResponseReserveFraction) leaves it exactly where the bind or the move that set
// it put it.
// What MOVES with the model, since ADR 0044: the profile and its parse-seam collaborators. The
// caller resolves it for the new model and hands it in on the spec; the same unexported
// applyProfile SetProfile uses installs it, so the next response is read in the new model's
// dialect rather than the departed model's.
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
	// The reply ceiling only when the spec speaks about it (see the field's contract): a nil leaves
	// the bound this session already holds exactly where it was, so a rebind driven for a model
	// change — or by any caller that resolves the per-model bindings alone — can never un-bound a
	// reply the bound entry's `max-output-tokens:` pinned. Written onto the copy with the rest, so a
	// spec that fails a gate below moves this no more than it moves the others.
	if spec.MaxOutputTokens != nil {
		next.Context.MaxOutputTokens = *spec.MaxOutputTokens
	}
	// The reserve share on the same contract and for the same reason (see the field): a nil leaves the
	// split this session already holds, so only a caller that actually re-resolved `response-reserve:`
	// moves it, and a stated 0 hands the split back to Allocate's own default. Written onto the copy
	// with the rest, so a spec that fails a gate below moves this no more than it moves the others.
	if spec.ResponseReserveFraction != nil {
		next.Context.ResponseReserveFraction = *spec.ResponseReserveFraction
	}
	next.EnableMechanisms = spec.EnableMechanisms
	// applyProfile below writes the live a.cfg.Profile; carrying the profile on the copy too is
	// what keeps `a.cfg = next` from putting the departed model's profile straight back.
	next.Profile = spec.Profile

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
	// The last step that can fail: translating the new model's profile into its parse-seam
	// collaborators. applyProfile is itself validate-then-commit, so an untranslatable profile
	// leaves the parsers, cfg.Profile and every binding above exactly as they were.
	if err := a.applyProfile(spec.Profile); err != nil {
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

// UpstreamSpec carries the new Upstream target Agent.SwitchUpstream moves the session to: where the
// server lives, how to authenticate to it, and what it BOUNDS a session to in tokens. That last pair
// belongs here for the reason the first two do — a context window, a reply ceiling and the share of
// the window that reply gets are facts about the SLOT, so a session moved to another server measures
// against THAT server's numbers
// rather than against the retired one's (ADR 0045's per-entry `context-window:`, ADR 0046's
// `max-output-tokens:`). Both are computed WHOLE by the caller, the posture RebindSpec takes for the
// per-model bindings: the engine applies what it is handed and reads no config of its own (ADR 0031).
//
// It still names no model — a switch UNBINDS the model rather than guessing what the new server
// serves (ADR 0024's one-code-path rule: the new Upstream's first observed model binds through
// Rebind like every other binding does).
type UpstreamSpec struct {
	// Endpoint is the new Upstream's base URL. Required — errMissingEndpoint stands.
	Endpoint string
	// APIKey is the new server's bearer token; "" sends no auth header. Keys are per-server, so
	// this replaces the old one outright rather than being carried over.
	APIKey string
	// MaxContextTokens is the BOUND context window in tokens on the new server — the caller has
	// already applied the new entry's `context-window:` pin over whatever the session ran on, exactly
	// as RebindSpec.MaxContextTokens carries the resolved window for a model change. 0 ⇒ nobody named
	// one, which leaves the Budget and automatic Compaction inactive until the new server's first
	// observed window binds through Rebind — the state a session before its first beat is already in,
	// and the honest one here, since no request can open while nothing is bound. Keeping the RETIRED
	// server's window instead would budget against a number describing a machine this session no
	// longer talks to.
	MaxContextTokens int
	// MaxOutputTokens is the ceiling on ONE reply from the new server — the new entry's
	// `max-output-tokens:` pin, carried as written (ADR 0046). 0 ⇒ that entry pins no cap, and the
	// engine derives one from the reply room the Budget reserves out of the window above. The zero is
	// applied rather than skipped for the reason the DelegationTarget's is (delegationtarget.go): a
	// cap of nothing is not a broken session, it is a session deriving its own, while keeping the old
	// pin would bound a reply from this server at a number describing another one.
	MaxOutputTokens int
	// ResponseReserveFraction is the share of the window above held back for one reply on the new
	// server — the new entry's `response-reserve:` resolved over the top-level key by the caller
	// (config.ResolveResponseReserve), like the window beside it and for its reason: how a window is
	// split is a statement about the slot the reply has to fit in. 0 ⇒ neither scope states a share,
	// which hands the split back to the engine's own built-in one (internal/context.Allocate) — the
	// zero is applied rather than skipped for the reply cap's reason above, since keeping the RETIRED
	// server's share would divide this server's window by a number describing another one.
	ResponseReserveFraction float64
}

// SwitchUpstream moves the session to another Upstream: it binds a fresh provider client at
// spec.Endpoint carrying spec.APIKey, and leaves the session with NO model bound. It is the
// engine half of the host's `/server` switch (ADR 0024).
//
// A new client rather than a mutated one is the provider's own contract (provider.Client.SetModel
// rebinds the model and deliberately never the endpoint), so the wire target moves atomically
// with the key rather than through two independent mutations.
//
// Unbinding is the honest posture, not a shortcut: the new server's model, context window,
// system-prompt template and Mechanism set are all facts only that server can report, so the
// host's heartbeat discovers them and the ordinary Rebind applies them — one code path with the
// cold start and the late seed. errNoModelBound guards Submit in the gap, exactly as it does
// before a session's first bind.
//
// Idle-only, like Rebind: it refuses mid-Exchange (ErrInputPending) so no request is ever
// re-pointed at a different server underneath itself, and the boundary the host crosses to call
// this IS the synchronization for the loop's un-mutexed cfg reads.
//
// What stands: the conversation and Turn counters, the autonomy mode, session approvals, the
// confinement flag, and the resolved tools — none of them describe a server. The catalogued
// Mechanism registry and the model profile also stand, both still describing the model that just
// went away, until the follow-up Rebind re-resolves them for the new one; they are unreachable
// meanwhile, since no request can open while nothing is bound.
// What MOVES with the server: the two token bounds the new entry pins — the context window the
// Budget and Compaction measure against, and the ceiling the loop states on the wire for one reply —
// and the share of that window the new server holds back for the reply. All three are applied as the
// spec states them, the zeroes included, because an absent pin is a fact about the new server rather
// than a licence to keep the old server's number (see the spec's fields).
// What resets, with Rebind's own rationale: the token estimator (its chars→token calibration
// described a model this session no longer speaks to) and the compaction saturation latch (it was
// judged against a window that is no longer bound).
func (a *Agent) SwitchUpstream(spec UpstreamSpec) error {
	if a.turns.inExchange {
		return domain.ErrInputPending
	}
	if spec.Endpoint == "" {
		return errMissingEndpoint
	}

	// Commit — from here on nothing can fail (provider.NewClient never does; a malformed
	// endpoint surfaces at request time, matching construction).
	a.upstream = provider.NewClient(spec.Endpoint, "", provider.WithAPIKey(spec.APIKey))
	a.cfg.Endpoint = spec.Endpoint
	a.cfg.APIKey = spec.APIKey
	a.cfg.Model = ""
	a.cfg.Context.MaxContextTokens = spec.MaxContextTokens
	a.cfg.Context.MaxOutputTokens = spec.MaxOutputTokens
	a.cfg.Context.ResponseReserveFraction = spec.ResponseReserveFraction
	a.tokens = apogeectx.NewTokenEstimator()
	a.compactSat = false
	return nil
}
