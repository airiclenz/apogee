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
// wire_openai.go is the default and the historical chat-completions behaviour; anthropicCodec
// in wire_anthropic.go speaks the Messages API, ADR 0078), while
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
//
// # Files
//
//   - responder.go — the Responder seam the loop calls instead of net/http.
//   - client.go — the Client: its Options, NewClient, Respond, the send/retry loop with its
//     Retry-After and backoff rules, the upstream StatusError, sanitising, and the wireCodec
//     interface every wire implements.
//   - stream.go — Stream and the Delta it yields: the streaming round-trip, the idle-timeout
//     body that bounds a silent stream, and the SSE scanner both codecs read through.
//   - attempt.go — the Attempt measurement of one HTTP attempt (ADR 0085), the server identity
//     a Client is stamped with, endpoint redaction and the per-call request id.
//   - fault.go — classify: the one place an upstream failure is sorted into its fault class
//     before the blocking and streaming surfaces render it.
//   - discovery.go — Discover: the /v1/models (and Messages-API model list) probe, the
//     ModelInfo and EffortSupport it reports, and the discovery and transport errors.
//   - localdial.go — the shared provider transport whose dialer falls back to mDNS for a
//     `.local` host the system resolver cannot find.
//   - mergepatch.go — the RFC 7396 JSON Merge Patch a server entry's request-extra is decoded
//     into once and applied to every encoded body (ADR 0085).
//   - wire.go — the provider-local seam types: Message, ToolCall, Request, Sampling, Effort,
//     Usage, RawResponse, Wire and the WireRecord a wire observer receives.
//   - wirejson.go — the literal OpenAI chat-completions request/response JSON structs the
//     openai codec maps onto, kept apart from the seam types.
//   - wire_openai.go — openaiCodec, the default wire: chat-completions body building, effort
//     dialects, whole-reply decode and SSE parsing.
//   - wire_anthropic.go — anthropicCodec, the Messages API wire (ADR 0078): headers, body
//     building, message and tool-call translation, and whole-reply decode.
//   - wire_anthropic_stream.go — the streaming half of anthropicCodec: the event-typed SSE
//     parser that folds content blocks into Deltas.
//   - doc.go — this map.
package provider
