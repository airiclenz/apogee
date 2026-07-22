package probe

import (
	"context"

	"github.com/airiclenz/apogee/internal/provider"
)

// Discovery is the outcome of the two read-only probes the host report makes against the
// configured Upstream: GET /v1/models (the authoritative model list) and llama.cpp's GET
// /props (the runtime context window). No model is called and nothing is generated — this is
// the same discovery the binary already performs at startup, reported instead of consumed.
//
// The zero value is "no endpoint was configured, so nothing was asked" (Attempted false), which
// is a legitimate report on an offline machine rather than a failure.
type Discovery struct {
	// Endpoint is the resolved Upstream URL the probes were sent to ("" when none is set).
	Endpoint string
	// Attempted reports whether an endpoint was configured at all, distinguishing "nothing to
	// ask" from "asked and got nothing".
	Attempted bool
	// Reached reports whether GET /v1/models completed and yielded a usable model list. It is
	// false both for a server that could not be dialled and for one that answered with an
	// error status or an empty list — Failure carries which, because the distinction matters
	// to the user and not to the report's structure.
	Reached bool
	// Failure is the discovery error message when Reached is false, and "" otherwise.
	Failure string
	// Models are the advertised model ids, in the order the server listed them.
	Models []string
	// ActiveModel is the model the Upstream resolves to with no model pinned — the first
	// advertised one (provider.Discover's rule).
	ActiveModel string
	// ContextWindow is the active model's window in tokens, 0 when unknown.
	ContextWindow int
	// RuntimeContextWindow is the window llama.cpp's GET /props reported, 0 when that probe
	// found none. Non-zero is the llama.cpp-shaped server; zero with Reached true is the
	// bare-OpenAI-shaped one.
	RuntimeContextWindow int
}

// Discover runs the host report's Upstream probes against endpoint and reports what answered.
// An empty endpoint returns the zero Discovery (nothing to ask). It never returns an error: a
// server that is down, wrong, or not OpenAI-shaped is a FINDING of the report, not a failure of
// the command — `apogee probe` exists precisely to be run on a machine where something is
// wrong. The probes go through the same provider client the session uses (bounded by that
// package's discovery timeout), so the report cannot describe a discovery the binary would not
// actually perform.
func Discover(ctx context.Context, endpoint string) Discovery {
	if endpoint == "" {
		return Discovery{}
	}
	d := Discovery{Endpoint: endpoint, Attempted: true}

	info, err := provider.NewClient(endpoint, "").Discover(ctx)
	if err != nil {
		d.Failure = err.Error()
		return d
	}

	d.Reached = true
	d.ActiveModel = info.ActiveModel
	d.ContextWindow = info.ContextWindow
	d.RuntimeContextWindow = info.RuntimeContextWindow
	d.Models = make([]string, 0, len(info.AvailableModels))
	for _, m := range info.AvailableModels {
		d.Models = append(d.Models, m.ID)
	}
	return d
}
