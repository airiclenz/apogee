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
	"time"

	"github.com/airiclenz/apogee/internal/provider"
)

// Interval is the monitor's cadence. It is a named constant, not a config key — the owner
// fixed ten seconds. provider.Discover's own five-second timeout keeps a beat strictly
// shorter than the interval, and the caller re-arms only from a landed beat, so beats can
// never overlap.
const Interval = 10 * time.Second

// ModelSummary is one model the Upstream advertises through GET /v1/models. The list travels
// on every Beat so a caller can hold the current offering in state — the data layer a future
// model picker reads; nothing renders it today.
type ModelSummary struct {
	// ID is the model id to send on the wire.
	ID string
	// DisplayName is the server's human label, "" when it reports none.
	DisplayName string
	// ContextWindow is the model's window in tokens, 0 when the server does not report it.
	ContextWindow int
}

// Beat is one observation of the Upstream. It is never accompanied by an error: an
// unreachable or wrong-shaped server sets Reachable false and explains itself in Failure,
// because that is a finding about the server rather than a fault of the observation.
type Beat struct {
	// Reachable reports whether GET /v1/models answered with a usable model list.
	Reachable bool
	// Failure says why the server could not be read, and is "" when Reachable.
	Failure string
	// ActiveModel is the model the Upstream resolves to — the monitor's hint while the server
	// still serves it, otherwise the first model advertised.
	ActiveModel string
	// ContextWindow is the active model's window in tokens, 0 when unknown. It is llama.cpp's
	// runtime window from GET /props when that probe answered, which overrides the advertised
	// one (provider.Discover's rule: /v1/models reports the model's TRAINING context).
	ContextWindow int
	// AvailableModels is every advertised model, in the order the server listed them.
	AvailableModels []ModelSummary
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
// Upstream permanently unreachable while the session talks to it perfectly well. The endpoint
// never changes mid-session (ADR 0024), so this one key holds for the Monitor's whole life —
// a model rebind reuses the same client through SetModel and therefore the same key.
func NewMonitor(endpoint, modelHint, apiKey string) *Monitor {
	return &Monitor{client: provider.NewClient(endpoint, modelHint, provider.WithAPIKey(apiKey))}
}

// Beat performs one observation and reports what answered. It never returns an error (see
// Beat) and is bounded by the provider package's discovery timeout as well as by ctx, so a
// hung server cannot outlive the interval.
func (m *Monitor) Beat(ctx context.Context) Beat {
	info, err := m.client.Discover(ctx)
	if err != nil {
		return Beat{Failure: err.Error()}
	}

	beat := Beat{
		Reachable:       true,
		ActiveModel:     info.ActiveModel,
		ContextWindow:   info.ContextWindow,
		AvailableModels: make([]ModelSummary, 0, len(info.AvailableModels)),
	}
	for _, model := range info.AvailableModels {
		beat.AvailableModels = append(beat.AvailableModels, ModelSummary{
			ID:            model.ID,
			DisplayName:   model.DisplayName,
			ContextWindow: model.ContextWindow,
		})
	}
	return beat
}
