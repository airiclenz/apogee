package main

// The per-server stats recorder (ADR 0085): the Driver-side subscriber that turns every
// UpstreamAttemptEvent the engine emits into one line of ~/.apogee/server-stats.jsonl. It is a
// Driver sink rather than an engine facility — the engine only reports the measurement (ADR 0031's
// wire-silent engine) — and it wraps Config.Events at the two composition sites that own a sink:
// the TUI session's (wire_boot.go) and every Firing's (wire_firing.go), headless and daemon included.

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/serverstats"
	"github.com/airiclenz/apogee/internal/tui"
)

// serverStatsFile is the stats file's name under the apogee home.
const serverStatsFile = "server-stats.jsonl"

// serverStatsPath is where the stats file of the apogee home configDir lives.
func serverStatsPath(configDir string) string {
	return filepath.Join(configDir, serverStatsFile)
}

// statsRecorder holds the stats store while `server-stats:` is on and nothing while it is off, so
// an off switch neither writes nor reads the file. It is live: set opens or stops it on a
// `/settings` edit, and every sink it wrapped follows at its next Event. A nil recorder is valid
// and records nothing.
type statsRecorder struct {
	path string
	now  func() time.Time

	mu    sync.Mutex
	store *serverstats.Store
}

// newStatsRecorder returns a recorder over path, opened when on is true. Opening trims an
// oversized file (serverstats.Open), which is why an off recorder does not open at all.
func newStatsRecorder(path string, on bool) *statsRecorder {
	r := &statsRecorder{path: path, now: time.Now}
	r.set(on)
	return r
}

// set opens the store (on) or stops it (off). Opening an already-open recorder keeps the store it
// has rather than re-reading the file.
func (r *statsRecorder) set(on bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case !on:
		r.store = nil
	case r.store == nil:
		r.store = serverstats.Open(r.path)
	}
}

// close stops the recorder for good at the end of the run. The store holds no file open between
// appends, so stopping it is the whole teardown.
func (r *statsRecorder) close() { r.set(false) }

// current is the open store, nil while the recorder is off.
func (r *statsRecorder) current() *serverstats.Store {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.store
}

// record appends one attempt's sample, when the recorder is on. A failed append is dropped: stats
// are a convenience, never a reason a Turn fails.
func (r *statsRecorder) record(ev domain.UpstreamAttemptEvent) {
	store := r.current()
	if store == nil {
		return
	}
	_ = store.Append(sampleOf(ev, r.now()))
}

// wrap decorates inner with the recorder: every Event reaches inner first and unchanged, and an
// UpstreamAttemptEvent is then recorded. A nil inner is allowed — a Firing with no Reaction Runner
// has no sink of its own — and forwards nowhere. A nil recorder returns inner untouched.
func (r *statsRecorder) wrap(inner domain.EventSink) domain.EventSink {
	if r == nil {
		return inner
	}
	return statsSink{inner: inner, rec: r}
}

// statsSink is the sink wrap returns.
type statsSink struct {
	inner domain.EventSink
	rec   *statsRecorder
}

// Emit forwards e, then records it when it is an upstream attempt.
func (s statsSink) Emit(e domain.Event) {
	if s.inner != nil {
		s.inner.Emit(e)
	}
	if ev, ok := e.(domain.UpstreamAttemptEvent); ok {
		s.rec.record(ev)
	}
}

// sampleOf is the stored mirror of one attempt event, stamped with the time it was recorded.
func sampleOf(ev domain.UpstreamAttemptEvent, at time.Time) serverstats.Sample {
	return serverstats.Sample{
		Server:       ev.Server,
		Endpoint:     ev.Endpoint,
		Model:        ev.Model,
		At:           at,
		RequestID:    ev.RequestID,
		Index:        ev.Index,
		TTFB:         ev.TTFB,
		TTFT:         ev.TTFT,
		Last:         ev.Last,
		Duration:     ev.Duration,
		OutputTokens: ev.OutputTokens,
		Outcome:      ev.Outcome,
	}
}

// withStats is serverChoices with each entry's measured summary filled in — what both pickers'
// seams (serverHost.List, delegationHost.Targets) hand the renderer. The summary comes from the
// store's in-memory index, so a picker asking on every draw never touches the disk; with the
// recorder off (or unwired) every choice keeps a nil summary and the rows draw no summary at all.
func (w *rootWiring) withStats(entries []config.ServerEntry) []tui.ServerChoice {
	choices := serverChoices(entries)
	store := w.stats.current()
	if store == nil {
		return choices
	}
	var bound upstreamBinding
	if w.holder != nil {
		bound = w.holder.Binding()
	}
	for i, entry := range entries {
		choices[i].Stats = entrySummary(store, entry, bound)
	}
	return choices
}

// entrySummary is one entry's summary for the model bound on it: the session's bound model when
// the session is on that entry's server (the same redacted endpoint — two entries on one URL are
// one server, serving one model), else the entry's own `model:` pin. An entry with neither is
// summarised for the model it last recorded, and the summary says so (LastRecorded).
func entrySummary(store *serverstats.Store, entry config.ServerEntry, bound upstreamBinding) *tui.ServerSummary {
	endpoint := provider.RedactEndpoint(entry.Endpoint)
	model := entry.Model
	if bound.Model != "" && provider.RedactEndpoint(bound.Endpoint) == endpoint {
		model = bound.Model
	}
	sum := store.Summary(entry.Name, endpoint, model)
	return &tui.ServerSummary{
		Model:           sum.Model,
		LastRecorded:    model == "",
		Total:           sum.Total,
		Failed:          sum.Failed,
		NoData:          sum.NoData,
		TTFT:            sum.TTFT,
		HasTTFT:         sum.HasTTFT,
		TokensPerSec:    sum.TokensPerSec,
		HasTokensPerSec: sum.HasTokensPerSec,
	}
}
