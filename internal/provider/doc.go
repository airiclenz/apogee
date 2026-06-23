// Package provider talks to the Upstream: it owns the Responder seam (the interface
// the engine calls instead of net/http), the provider-local wire types, and the
// OpenAI-compatible Client that implements the seam — non-streaming Respond plus a
// streaming Stream, with bounded retries and timeouts, /v1/models discovery, and a
// local server-process manager. It is the seam behind which streaming, retries, and
// timeouts live (P1.1).
//
// ADR 0010 homes the Responder seam here (moved out of internal/agent) beside the
// HTTP client that implements it; the wire types (Request / RawResponse / Message)
// stay provider-local and domain-free, and the loop translates domain conversation
// state ↔ wire shape at the boundary. Tests inject their own fakes through the
// engine's unexported seam; the wire path itself is exercised hermetically against an
// httptest.Server.
package provider
