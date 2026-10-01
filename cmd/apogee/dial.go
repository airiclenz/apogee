package main

// The dial facts of a server entry, and the two things they build (ADR 0083 §4 as amended
// 2026-09-30).
//
// An entry reaches the wire through two host-built values: the Client a request is sent on and
// the heartbeat Monitor that observes the server. Both are built HERE, from one upstreamBinding,
// so the options each needs are spelled once — a new Client-side entry key is the binding's
// field and a line in this file, not an edit at every site that dials.
//
// The split between the two is the facts' own. The Client carries the key, the wire and the
// `request-extra:` passthrough, because every body it sends must; the forced dialect does not
// ride on it, because the dialect a request is encoded in is ranked by the caller against an
// observation. The Monitor carries the key, the wire and the forced `effort-dialect:`, because
// that verdict overrules what discovery detects; it never carries request-extra, because a
// discovery probe sends no body to merge it into.

import (
	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/provider"
)

// bindingOfEntry is entry's dial facts, dialled with apiKey — the key resolved for it, which the
// entry itself never holds in the clear. Model is the entry's own `model:` pin, "" when it pins
// none; a caller that has already resolved a model sets Model on the result.
func bindingOfEntry(entry config.ServerEntry, apiKey string) upstreamBinding {
	return upstreamBinding{
		Endpoint:      entry.Endpoint,
		Model:         entry.Model,
		APIKey:        apiKey,
		Wire:          entry.Wire,
		RequestExtra:  string(entry.RequestExtra),
		EffortDialect: entry.EffortDialect,
	}
}

// bindingOfTarget is the dial facts a Delegation target was resolved with. EffortDialect stays
// "": a target carries no forced spelling, only the dialect its landing beat ranked
// (DelegationTarget.EffortDialect), which its caller keeps beside the binding.
func bindingOfTarget(target *apogee.DelegationTarget) upstreamBinding {
	return upstreamBinding{
		Endpoint:     target.Endpoint,
		Model:        target.Model,
		APIKey:       target.APIKey,
		Wire:         target.Wire,
		RequestExtra: target.RequestExtra,
	}
}

// bindingOfConfig is the dial facts an assembled engine Config carries. EffortDialect stays "" for
// the reason bindingOfTarget's does: Config.EffortDialect is the ranked dialect, not the entry's
// forced spelling.
func bindingOfConfig(cfg apogee.Config) upstreamBinding {
	return upstreamBinding{
		Endpoint:     cfg.Endpoint,
		Model:        cfg.Model,
		APIKey:       cfg.APIKey,
		Wire:         cfg.Wire,
		RequestExtra: cfg.RequestExtra,
	}
}

// Client builds a Client dialled from the binding: its endpoint and model, with the key, the wire
// and the request-extra passthrough. opts are the caller's own — a timeout, a retry budget — and
// are applied after the dial facts.
func (b upstreamBinding) Client(opts ...provider.Option) *provider.Client {
	dial := []provider.Option{
		provider.WithAPIKey(b.APIKey),
		provider.WithWire(provider.WireFor(b.Wire)),
		provider.WithRequestExtra(b.RequestExtra),
	}
	return provider.NewClient(b.Endpoint, b.Model, append(dial, opts...)...)
}

// Monitor builds the heartbeat Monitor that observes the binding's server, with Model as its
// discovery hint: the key, the wire and the forced effort dialect. It never carries request-extra.
func (b upstreamBinding) Monitor() *heartbeat.Monitor {
	return heartbeat.NewMonitor(b.Endpoint, b.Model, b.APIKey,
		provider.WithEffortDialect(provider.EffortDialectFor(b.EffortDialect)),
		provider.WithWire(provider.WireFor(b.Wire)))
}
