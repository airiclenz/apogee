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
// It is deliberately NOT the whole Config: mode, approvals, confinement and the conversation are
// session state that a model switch has no business resetting. The tool ROSTER left that list with
// ADR 0057 — which tools a model is offered is a fact about the model (a small model drowns in a
// menu a big one wants), so it is a per-model binding like the rest and rides here, as the third
// axis of Profile below rather than as a field of its own.
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
	// wire, on the axes of tool-call format and inline thinking channel — and, since ADR 0057,
	// WHICH TOOLS it is offered, on the third axis Tools. It replaces the current profile
	// outright, and the ZERO value is meaningful — native tool calls, no inline thinking, no
	// roster deltas — so a model that matches no user entry and no shipped shape parses, and is
	// offered exactly the menu, an unprofiled session gets. A profile the processing seam cannot
	// translate fails the whole spec, leaving every binding standing.
	//
	// The roster axis is the RESOLVED delta pair for this model, computed whole by the caller like
	// everything else here (the axis-wise ladder profile > global > build default lives in the
	// composition root — ADR 0031). The engine applies it to the set it composed ITSELF; a host
	// that injected Config.Tools or took the SwapTools door owns its set and folds its own deltas
	// in where it builds (applyRoster).
	Profile domain.ModelProfile
	// EffortDialect is the wire shape the server this session is on reads a thinking-effort intent
	// in — `chat_template_kwargs`, OpenRouter's `reasoning` object, or OpenAI's top-level
	// `reasoning_effort` (ADR 0060). It is the THIRD field here that is not a per-MODEL fact, and
	// it rides this spec for the two bounds' reason: the dialect is detected per SERVER (or pinned
	// by that entry's `effort-dialect:`), and neither the detection nor the pin has an engine
	// setter of its own, so the atomic commit a rebind already makes is the only door either can
	// reach the engine through.
	//
	// A plain value rather than a pointer, and that is deliberate: the caller resolving a binding
	// always knows what the server said about the dial, so there is nothing to be silent about, and
	// the zero (provider.EffortDialectNone) is itself the meaningful answer — "this server
	// advertises no dial", which keeps the historical `chat_template_kwargs` shape and so
	// reproduces the request bytes that predate the dialect seam (ADR 0031). It is the ONLY effort
	// fact that crosses into the engine: the level vocabulary a server reports, and the level it
	// defaults to, stay host-side.
	EffortDialect provider.EffortDialect
}

// Rebind swaps the Agent's per-model bindings at a quiescent boundary — the wire model, the
// system-prompt template, the context window the Budget and Compaction measure against, the
// catalogued Mechanism set, and the profile with the tool roster its third axis spells (ADR 0057)
// — and rebinds the provider client's wire model with them. It is the
// engine half of the heartbeat's observed model change (ADR 0024). A spec may carry three facts that
// are NOT per-model — the reply ceiling (ADR 0046), the share of the window reserved for that reply,
// and the wire dialect the server reads a thinking-effort intent in (ADR 0060) — for the reason
// their fields state: none of the three has a setter of its own, so a live edit of them reaches the
// engine through this same atomic commit.
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
// What stands: the conversation and Turn counters, the autonomy mode, session approvals and the
// confinement flag — and the reply ceiling and its reserve share too, unless
// the spec names them: those bounds describe the SERVER, so a spec silent about one (a nil
// MaxOutputTokens or ResponseReserveFraction) leaves it exactly where the bind or the move that set
// it put it. A tool set the HOST owns stands as well, injected or swapped in: the engine composes
// no roster under it (applyRoster).
// What MOVES with the model, since ADR 0044: the profile and its parse-seam collaborators — and,
// since ADR 0057, the tool ROSTER the profile's third axis spells. The
// caller resolves it for the new model and hands it in on the spec; the same unexported
// applyProfile SetProfile uses installs it, so the next response is read in the new model's
// dialect rather than the departed model's, and the next request offers the new model's menu
// rather than the departed model's.
// What MOVES with the SERVER, since ADR 0060: the effort wire dialect, applied as the spec states
// it — the zero included, since "this server advertises no dial" is an answer about the server the
// session is on rather than a licence to keep the departed one's shape.
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
	// The newly bound server's effort dialect, mirrored onto the Config the way every binding above
	// is. The live answer is the a.effortDialect field written inside the commit below — the Config
	// only ever SEEDS it (agent.go) — but leaving the seed on the departed server's shape here would
	// leave a stale value behind for any future reader of the Config to pick up.
	next.EffortDialect = toDomainDialect(spec.EffortDialect)

	registry := domain.NewMechanismRegistry()
	deps, err := buildEnabledMechanisms(next, registry)
	if err != nil {
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
	// collaborators — and, once that has committed, composing the new model's tool roster off the
	// same profile's third axis (applyRoster). applyProfile is itself validate-then-commit, so an
	// untranslatable profile leaves the parsers, cfg.Profile, the tool set and every binding above
	// exactly as they were. It runs BEFORE `a.cfg = next` and writes a.cfg.Profile itself, which is
	// what lets the roster it composes read the profile now bound rather than the departed one;
	// next carries the same profile, so the assignment below puts back what is already there.
	if err := a.applyProfile(spec.Profile); err != nil {
		return err
	}

	// Commit — from here on nothing can fail.
	a.cfg = next
	a.registry = registry
	// The rebuilt catalogue's Library store, which library.Open makes the very instance this session
	// already held whenever the LibraryDir is unchanged — so re-holding it costs nothing and a rebind
	// that drops the `library` arm leaves nothing behind for Close to flush.
	a.library = deps.Library
	// The provider client's configured model WINS over the request's (provider.buildBody), so the
	// wire model moves only if the Responder is told. It is an optional interface rather than a
	// widening of the Responder seam: a fake responder in a test simply does not implement it, and
	// a Responder with no notion of a model id has nothing to rebind.
	if binder, ok := a.upstream.(interface{ SetModel(string) }); ok {
		binder.SetModel(spec.Model)
	}
	// The dialect the next request expresses its effort in, taken as the spec states it — the zero
	// included, which is this server advertising no dial and keeps the historical
	// `chat_template_kwargs` shape, reproducing the request bytes that predate the dialect seam.
	// Written here, inside the commit, so a spec that failed a gate above moves it no more than it
	// moves the bindings beside it; it needs no lock for the reason the a.cfg assignment above
	// needs none (see this method's doc).
	a.effortDialect = spec.EffortDialect
	a.tokens = apogeectx.NewTokenEstimator()
	a.compactSat = false
	// The stand-down latch goes with the saturation one: it recorded that an automatic fold FAULTED
	// against the server and model just departed, which says nothing about the pair now bound. The
	// Exchange-scoped clear (turnLifecycle.openExchange) would reach it at the next Exchange anyway
	// — a rebind is a quiescent boundary — so this only keeps the two latches moving together.
	a.compactFailed = false
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
	// WorkingWindow is the room INSIDE that window the session works in on the new server — the new
	// entry's `working-window:` resolved over the top-level key by the caller
	// (config.ResolveWorkingWindow), like the window beside it and for its reason: how much room is
	// affordable is a statement about the slot. 0 ⇒ neither scope bounds anything, which leaves the
	// advertised window as the whole working room. The zero is applied rather than skipped for the
	// reply cap's reason below — keeping the RETIRED server's bound would work in a room describing
	// a machine this session no longer talks to.
	WorkingWindow int
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
// What MOVES with the server: the token bounds the new entry states — the context window the Budget
// and Compaction measure against, the room inside it the session works in, and the ceiling the loop
// states on the wire for one reply — and the share of that window the new server holds back for the
// reply. All four are applied as the spec states them, the zeroes included, because an absent pin is a fact about the new server rather
// than a licence to keep the old server's number (see the spec's fields).
// What resets, with Rebind's own rationale: the token estimator (its chars→token calibration
// described a model this session no longer speaks to) and the compaction saturation latch (it was
// judged against a window that is no longer bound).
// What is TORN DOWN: the client being replaced, when this session owns it (ratified call 9) — the
// switch is the point where it becomes unreachable, and nothing else would ever close it. The new
// client is owned in its turn, so a later switch, or Close, retires it the same way.
func (a *Agent) SwitchUpstream(spec UpstreamSpec) error {
	if a.turns.inExchange {
		return domain.ErrInputPending
	}
	if spec.Endpoint == "" {
		return errMissingEndpoint
	}

	// Commit — from here on nothing can fail (provider.NewClient never does; a malformed
	// endpoint surfaces at request time, matching construction).
	// The Inspector's capture is re-armed onto the new client, since an observer belongs to the
	// client it was built with: without this a `/server` switch would silently disarm a session
	// that started with `ui.inspector` on. The tap binds to THIS Agent, which is the one that will
	// speak over the new connection.
	opts, tap := armWireCapture(a.cfg)
	tap.bind(a)
	// The retired client goes down with the server it dialled: a switch is the one moment where a
	// client this session OWNS stops being reachable, so skipping the teardown would strand its
	// idle sockets to the old server for the rest of the session. A client shared from a parent is
	// left running — it is not this Agent's to close. The error is dropped deliberately rather than
	// swallowed: the switch has already committed above, and provider teardown reports nothing a
	// caller could act on (Client.Close always succeeds), so surfacing it would say the move failed.
	_ = a.closeOwnedUpstream(a.upstream)
	a.upstream = provider.NewClient(spec.Endpoint, "", append(opts, provider.WithAPIKey(spec.APIKey))...)
	a.ownsUpstream = true
	a.cfg.Endpoint = spec.Endpoint
	a.cfg.APIKey = spec.APIKey
	a.cfg.Model = ""
	a.cfg.Context.MaxContextTokens = spec.MaxContextTokens
	a.cfg.Context.WorkingWindow = spec.WorkingWindow
	a.cfg.Context.MaxOutputTokens = spec.MaxOutputTokens
	a.cfg.Context.ResponseReserveFraction = spec.ResponseReserveFraction
	a.tokens = apogeectx.NewTokenEstimator()
	a.compactSat = false
	// Cleared with the saturation latch, for the reason Rebind clears it: a fold that faulted
	// against the retired server judges nothing about the one just dialled.
	a.compactFailed = false
	return nil
}
