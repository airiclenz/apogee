package main

// The dial facts of a server entry, and the two things they build (ADR 0083 §4 as amended
// 2026-09-30).
//
// An entry reaches the wire through two host-built values: the Client a request is sent on and
// the heartbeat Monitor that observes the server. Both are built HERE, from one upstreamBinding,
// so the options each needs are spelled once — a new Client-side entry key is the binding's
// field and a line in this file, not an edit at every site that dials. The engine's own dial
// fields — on the Config it is built from, the UpstreamSpec a move switches it with, the
// DelegationTarget a routed child dials — and the probe's are projected from the same binding
// here too (fillDial, upstreamSpec, delegationTarget, probeDial): each sets dial fields only and
// never the model, which stays the caller's.
//
// The split between the two is the facts' own. The Client carries the key, the wire and the
// `request-extra:` passthrough, because every body it sends must; the forced dialect does not
// ride on it, because the dialect a request is encoded in is ranked by the caller against an
// observation. The Monitor carries the key, the wire and the forced `effort-dialect:`, because
// that verdict overrules what discovery detects; it never carries request-extra, because a
// discovery probe sends no body to merge it into.
//
// One field of the binding is not a dial fact at all: the entry's `price:` (ADR 0093 decision 3).
// Neither the Client nor the Monitor reads it — it never reaches the wire — but it is a fact about
// the server a call goes to, so it rides the binding to every engine projection (fillDial,
// upstreamSpec, delegationTarget) and the bind, a `/server` move and a routed delegation each carry
// the price of the server they dial. The entry's `vision:` opt-in rides beside it for the same
// reason: whether a server accepts images is a fact about the server, never sent on the wire.

import (
	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/probe"
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
		Price:         priceOfEntry(entry.Price),
		Vision:        entry.Vision,
	}
}

// priceOfEntry is an entry's `price:` as the engine prices a call by (ADR 0093 decision 2): the
// three rates per 1M tokens, with `cached-input:` resolved to the input rate when the entry leaves
// it out (config.Price.CachedInputRate), and whether the entry states a price at all. An entry with
// no `price:` is the unpriced zero.
func priceOfEntry(price config.Price) domain.ServerPrice {
	if !price.IsStated() {
		return domain.ServerPrice{}
	}
	return domain.ServerPrice{
		Rate: domain.Price{
			Input:       price.Input,
			Output:      price.Output,
			CachedInput: price.CachedInputRate(),
		},
		IsStated: true,
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
		Price:        target.Price,
		Vision:       target.Vision,
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
		Price:        cfg.Price,
		Vision:       cfg.Vision,
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

// fillDial sets cfg's dial fields from the binding — the endpoint, the key, the wire and the
// request-extra passthrough — and the server's price and vision opt-in beside them, and nothing else. Model stays the caller's (a bind pins the entry's
// own, a Firing the one its spec resolved), and so does EffortDialect: the Config carries the
// RANKED dialect, which the caller resolves against an observation, never the forced spelling.
func (b upstreamBinding) fillDial(cfg *apogee.Config) {
	cfg.Endpoint = b.Endpoint
	cfg.APIKey = b.APIKey
	cfg.Wire = b.Wire
	cfg.RequestExtra = b.RequestExtra
	cfg.Price = b.Price
	cfg.Vision = b.Vision
}

// upstreamSpec is the switch a `/server` move hands the engine, carrying the binding's dial fields
// and the price and vision opt-in only. The caller sets the arrived-at server's name, description and its window, working-window,
// reply-cap and reserve; a move carries no model at all — the first beat on the new server binds
// one.
func (b upstreamBinding) upstreamSpec() apogee.UpstreamSpec {
	return apogee.UpstreamSpec{
		Endpoint:     b.Endpoint,
		APIKey:       b.APIKey,
		Wire:         b.Wire,
		RequestExtra: b.RequestExtra,
		Price:        b.Price,
		Vision:       b.Vision,
	}
}

// delegationTarget is a Delegation target carrying the binding's dial fields, the price and the vision opt-in only — the dial a
// routed child is built on. The caller sets the server's name, the model it resolved, the window
// and the rest; EffortDialect is the beat's ranked one (resolveDelegationTarget), never the forced
// spelling the binding holds for its Monitor.
func (b upstreamBinding) delegationTarget() apogee.DelegationTarget {
	return apogee.DelegationTarget{
		Endpoint:     b.Endpoint,
		APIKey:       b.APIKey,
		Wire:         b.Wire,
		RequestExtra: b.RequestExtra,
		Price:        b.Price,
		Vision:       b.Vision,
	}
}

// probeDial sets the dial fields `apogee probe` discovers with: the endpoint, the key and the wire,
// folded the way the Monitor folds it. A probe sends no body, so request-extra has no field here.
func (b upstreamBinding) probeDial(in *probe.Inputs) {
	in.Endpoint = b.Endpoint
	in.APIKey = b.APIKey
	in.Wire = provider.WireFor(b.Wire)
}
