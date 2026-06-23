package provider

import "context"

// Responder is the provider seam (Phase-0 detail plan, Decision C; ADR 0010 homes it
// beside the HTTP client): the loop depends on this, not on net/http. Tests inject a
// deterministic fake; the real OpenAI-compatible Client (P1.1) implements the same
// interface, so the loop never changes when the wire client lands. Living under
// internal/ it carries no public-API promise.
//
// Respond is the non-streaming primary: it performs one Upstream round-trip and returns
// the assembled reply. Streaming is a separate capability on the concrete Client
// (Stream), wired into the loop when the full Turn/Step state machine lands (P1.2); the
// seam stays minimal so a fake need only answer one method.
type Responder interface {
	Respond(ctx context.Context, req Request) (RawResponse, error)
}
