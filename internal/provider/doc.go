// Package provider talks to the Upstream: it owns the Responder seam (the interface
// the engine calls instead of net/http), the provider-local wire types, and the HTTP
// Client that implements the seam — non-streaming Respond plus a streaming Stream, with
// bounded retries and timeouts and /v1/models discovery. It is the seam behind which
// streaming, retries, and timeouts live (P1.1). Watching the Upstream over time is not
// this package's job: internal/heartbeat observes reachability and the served model on a
// cadence (ADR 0024).
//
// The Client speaks one Wire per server entry, selected by WithWire: the protocol-specific
// half of a round-trip — request path and key header, body encoding, whole-reply decode,
// SSE parsing — is an unexported wireCodec, one deep module per wire (openaiCodec in
// wire_openai.go is the default and the historical chat-completions behaviour), while
// retries, timeouts, redirect refusal, fault classification, sanitising and wire capture
// stay in the Client and are shared by every wire. Nothing outside a codec's file branches
// on its dialect.
//
// ADR 0010 homes the Responder seam here (moved out of internal/agent) beside the
// HTTP client that implements it; the wire types (Request / RawResponse / Message)
// stay provider-local and domain-free, and the loop translates domain conversation
// state ↔ wire shape at the boundary. Tests inject their own fakes through the
// engine's unexported seam; the wire path itself is exercised hermetically against an
// httptest.Server.
package provider
