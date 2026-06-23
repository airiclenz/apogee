package provider

import (
	"context"
	"iter"
)

// Responder is the provider seam (Phase-0 detail plan, Decision C; ADR 0010 homes it
// beside the HTTP client): the loop depends on this, not on net/http. Tests inject a
// deterministic fake; the real OpenAI-compatible Client (P1.1) implements the same
// interface, so the loop never changes when the wire client lands. Living under
// internal/ it carries no public-API promise.
//
// Stream is the loop's primary: it performs one Upstream round-trip and yields Deltas as
// they arrive (token text, reasoning, accumulated tool calls, a terminal Done/Error), so
// the loop emits TokenEvents live and reaches the §6 #6 streaming-then-Approval boundary.
// Faults surface as a terminal Delta rather than a Go error, so the consumer drives a
// single range loop (matching the TS AsyncIterable). The seam is deliberately streaming-
// only — the loop never needs the whole-response Respond, which stays a concrete Client
// method (model discovery, simple calls) outside the interface so a fake answers one method.
type Responder interface {
	Stream(ctx context.Context, req Request) iter.Seq[Delta]
}
