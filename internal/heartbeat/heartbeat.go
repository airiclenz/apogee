// Package heartbeat observes the Upstream on a fixed cadence: every Interval it asks the
// server which model it is serving, in which context window, and what else it advertises,
// and reports the answer as a Beat.
//
// It is deliberately NOT a probe. `apogee probe` (ADR 0021) diagnoses ONCE, on demand, and
// prints a report a human reads — CONTEXT.md is explicit that it "diagnoses, it does not
// monitor". The heartbeat is the monitoring half nothing owned before: it runs unasked for
// the life of a session, and its output is consumed (upstream display, offline state, and
// the rebind that follows an observed model change), never printed as a report. Two nouns,
// two jobs — a probe answers "what can this machine and this model do?", a beat answers "is
// the server still there, and is it still serving the model I am bound to?".
//
// A beat is never an error: an unreachable server is a FINDING, not a failure (the same
// posture as probe.Discover), because "the server stopped answering" is exactly the
// observation the caller needs in order to say so.
package heartbeat

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/airiclenz/apogee/internal/provider"
)

// Interval is the monitor's cadence. It is a named constant, not a config key — the owner
// fixed ten seconds. provider.Discover's own five-second timeout keeps a beat strictly
// shorter than the interval, and the caller re-arms only from a landed beat, so beats can
// never overlap.
const Interval = 10 * time.Second

// ModelSummary is one model the Upstream advertises through GET /v1/models. The list travels
// on every Beat so a caller can hold the current offering in state — the data layer the TUI's
// /model picker derives its rows from, so a beat landing under an open picker refreshes them.
type ModelSummary struct {
	// ID is the model id to send on the wire.
	ID string
	// DisplayName is the server's human label, "" when it reports none.
	DisplayName string
	// ContextWindow is the model's window in tokens, 0 when the server does not report it.
	ContextWindow int
	// EffortSupport is what THIS model's advertised entry said about its own thinking-effort dial
	// (provider.DiscoveredModel.EffortSupport), and the zero value when it said nothing. It rides
	// the offering rather than only the beat's active model because a host acting on a `/model`
	// pick decides against the model it is picking INTO — the session effort override a target
	// rules out is cleared at the pick, without waiting a beat to be told (ADR 0060 D8).
	EffortSupport provider.EffortSupport
}

// Beat is one observation of the Upstream. It is never accompanied by an error: an
// unreachable or wrong-shaped server sets Reachable false and explains itself in Failure,
// because that is a finding about the server rather than a fault of the observation.
type Beat struct {
	// Reachable reports whether GET /v1/models answered with a usable model list.
	Reachable bool
	// Failure says why the server could not be read, and is "" when Reachable.
	Failure string
	// Throttled reports that the model list came back HTTP 429: the server ANSWERED, it just would
	// not answer THIS question now. Reachable stays false and Failure keeps the text — a throttled
	// list is no more a usable model list than a timed-out one — but the distinction is there for an
	// observer that treats a rate-limited probe as silence rather than as a verdict about the
	// server (cmd/apogee's Sub-agent routing). Nothing else reads it, and false is the safe default:
	// an observer that ignores the field behaves exactly as it did before the field existed.
	Throttled bool
	// ActiveModel is the model the Upstream resolves to — the monitor's hint whenever one is
	// configured, trusted verbatim even when the server does not advertise it (provider.Discover's
	// rule), and the first model advertised only when no hint is configured.
	ActiveModel string
	// ContextWindow is the active model's window in tokens, 0 when unknown. It is llama.cpp's
	// runtime window from GET /props when that probe answered, which overrides the advertised
	// one (provider.Discover's rule: /v1/models reports the model's TRAINING context).
	ContextWindow int
	// TotalSlots is how many generation slots the same /props probe reported — the `--parallel N`
	// width the server was launched with — and 0 when it reported none. It rides the beat for the
	// window's own reason: the width is a property of the SERVER, it can move under a session when
	// the operator restarts that server, and the caller re-resolves the Parallel agents cap from it
	// at the next boundary (ADR 0039 decision 2, ADR 0024's rebind). One more field on the
	// observation that already lands every Interval, never a probe of its own.
	TotalSlots int
	// EffortSupport is what the same discovery saw about the ACTIVE model's thinking-effort dial
	// (ADR 0060): whether the dial exists, which wire dialect reaches it, and the vocabulary and
	// default the server stated. Like TotalSlots it is a property of the BINDING rather than of the
	// launch — a rebind, or an operator swapping the model under a running server, moves it — so it
	// is re-observed every Interval on the beat that already lands, never probed for on its own.
	// Only the host acts on it (the /effort menu entry, the footer segment and the picker's rows);
	// the zero value is both "no dial" and "no tell to read", and changes no behaviour.
	EffortSupport provider.EffortSupport
	// AvailableModels is every advertised model, in the order the server listed them.
	AvailableModels []ModelSummary
	// Resolution grades HOW discovery reached ActiveModel — advertised verbatim, matched on the
	// base slug of a variant hint, trusted as configured, or the first advertised model when
	// nothing is configured (provider.HintResolution). It rides the beat because only discovery
	// can say it and only the host can act on it: a hint is now trusted rather than substituted,
	// so a non-exact grade is what a host's "not advertised" notice is emitted from, without the
	// host re-deriving the match against AvailableModels.
	Resolution provider.HintResolution
}

// Monitor is the production beat source: one provider client, beaten on demand by whoever
// owns the cadence. It holds no per-beat state, so a beat is safe to run from a goroutine.
type Monitor struct {
	client *provider.Client
}

// NewMonitor builds the Monitor for the Upstream at endpoint. modelHint is the config-pinned
// model id, "" when nothing is pinned: discovery resolves the pin's model AND its window
// while the server still serves that id, and falls back to the server's first advertised
// model once the pin vanishes from /v1/models — the pin is a hint about reality, never a
// claim that overrides it.
//
// apiKey is the upstream bearer token ("" on a keyless local server, which sends no auth
// header at all). It rides every beat, because a keyed server answers /v1/models with 401
// just as readily as it answers a chat call: a monitor without the key would report the
// Upstream permanently unreachable while the session talks to it perfectly well.
//
// A Monitor is per-SERVER: the endpoint and the key it is built with hold for its whole life,
// because moving a session to another server (the host's `/server` switch) swaps the whole
// Monitor rather than mutating one — the composition root holds the current one and replaces
// it, mirroring provider.Client's own "switching servers means a new Client" contract. What
// DOES move within a Monitor's life is only the discovery hint, through SetModel below, which
// is a property of the binding and not of the server.
//
// opts are per-server provider options the caller states about THIS server, applied after the key:
// the composition root passes the entry's forced thinking-effort dialect that way
// (provider.WithEffortDialect), so what the beat reports about the dial is what the file said for a
// provider that advertises nothing (ADR 0060 decision 3). They are options rather than parameters
// because they are all per-server facts of the same class — one variadic tail absorbs the next one
// without moving every caller — and because the Monitor itself has no opinion on any of them: it
// hands them to the one client it owns and reads the beat that comes back.
func NewMonitor(endpoint, modelHint, apiKey string, opts ...provider.Option) *Monitor {
	return &Monitor{client: provider.NewClient(endpoint, modelHint,
		append([]provider.Option{provider.WithAPIKey(apiKey)}, opts...)...)}
}

// SetModel moves the discovery hint to model. The hint is a property of the BINDING, not of
// the launch: the host re-states it whenever a rebind commits — a user-driven pick and a
// heartbeat-observed change alike, the latter a no-op restatement of what the beat just said —
// so discovery keeps resolving the model the session actually runs rather than the one config
// named at launch. Without that, a multi-model server still serving the configured id would
// resolve it on the next beat and the rebind orchestration would dutifully bind back to it,
// undoing the switch seconds after it landed.
//
// It never touches the endpoint, honouring provider.Client's own contract that switching
// servers means a new Client rather than a mutated one. It is safe to call while a beat is in
// flight: the client guards that field for exactly this concurrent use.
func (m *Monitor) SetModel(model string) {
	m.client.SetModel(model)
}

// Beat performs one observation and reports what answered. It never returns an error (see
// Beat) and is bounded by the provider package's discovery timeout as well as by ctx, so a
// hung server cannot outlive the interval.
func (m *Monitor) Beat(ctx context.Context) Beat {
	info, err := m.client.Discover(ctx)
	if err != nil {
		// A 429 is singled out on the observation rather than left for every caller to re-derive by
		// string-matching the failure text: the code is a fact the probe HAS, and reading it back
		// out of a sentence is the sort of thing that breaks the day the sentence is reworded.
		var status *provider.StatusError
		throttled := errors.As(err, &status) && status.Code == http.StatusTooManyRequests
		return Beat{Failure: err.Error(), Throttled: throttled}
	}

	beat := Beat{
		Reachable:       true,
		ActiveModel:     info.ActiveModel,
		ContextWindow:   info.ContextWindow,
		TotalSlots:      info.TotalSlots,
		EffortSupport:   info.EffortSupport,
		Resolution:      info.Resolution,
		AvailableModels: make([]ModelSummary, 0, len(info.AvailableModels)),
	}
	for _, model := range info.AvailableModels {
		beat.AvailableModels = append(beat.AvailableModels, ModelSummary{
			ID:            model.ID,
			DisplayName:   model.DisplayName,
			ContextWindow: model.ContextWindow,
			EffortSupport: model.EffortSupport,
		})
	}
	return beat
}
