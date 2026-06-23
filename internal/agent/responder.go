package agent

import "context"

// Message is one role-tagged message in a provider request — the wire-shaped view
// of conversation state. It is deliberately decoupled from the public apogee.Message
// so this package carries no dependency on the root apogee package: the root facade
// imports internal/agent for the provider seam, and keeping the seam root-type-free
// is what keeps that a one-way import (no facade↔loop cycle). Phase 1 widens this to
// carry tool calls and preserved wire fields.
type Message struct {
	Role    string
	Content string
}

// Request is the minimal Upstream request the loop hands a Responder. Phase 0 is
// non-streaming and tool-free, so it is just the model id and the conversation so
// far; Phase 1 adds the tool menu, sampling params, and preserved extras.
type Request struct {
	Model    string
	Messages []Message
}

// RawResponse is the minimal Upstream reply: a single assistant message. Phase 1
// widens it to carry parsed tool calls, a finish reason, a thinking channel, and
// token usage.
type RawResponse struct {
	Content string
}

// Responder is the provider seam (Phase-0 detail plan, Decision C): the loop depends
// on this, not on net/http. P0.6 supplies a deterministic fake in test code; the
// real OpenAI-compatible HTTP provider implements the same interface in Phase 1, so
// the loop never changes when the wire client lands. It is exported (not the lower-
// case sketch name) only because the caller is the root apogee package rather than
// this package itself; living under internal/ it carries no public-API promise.
type Responder interface {
	Respond(ctx context.Context, req Request) (RawResponse, error)
}
