// Package provider talks to the Upstream: it owns the Responder seam (the interface
// the engine calls instead of net/http), the provider-local wire types, and — from
// Phase 1 — an OpenAI-compatible client, model discovery, and the local
// server-process manager. It is the seam behind which streaming, retries, and
// timeouts live.
//
// ADR 0010 homes the Responder seam here (moved out of internal/agent) beside the
// HTTP client that will implement it; the wire types (Request / RawResponse /
// Message) stay provider-local and domain-free, and the loop translates domain
// conversation state ↔ wire shape at the boundary. Until the real client lands (P1.1)
// the only implementation is Placeholder, which errors on every call so the
// pre-provider slice stays hermetic; tests inject their own fakes.
package provider
