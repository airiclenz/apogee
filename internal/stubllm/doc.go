// Package stubllm is the one scripted upstream apogee's tests talk to (ADR 0062), on either
// of the wires apogee speaks: OpenAI chat-completions and Anthropic Messages.
//
// A test names the replies it wants as a [Script] — an ordered list of [Turn]s — and gets an
// HTTP server ([New], [Serve]) or an in-process handler ([InProcess], reached through
// [Server.Transport]) that plays them back through the wire shapes a real llama.cpp or OpenRouter
// endpoint uses: SSE content deltas, a reasoning channel, streamed tool-call fragments, a
// terminal usage object with the cached-prompt breakdown, plain HTTP failures, a stall, a
// mid-stream connection loss and an in-band upstream error. The same Script answers on
// POST /v1/messages in the Messages API's shapes — event-typed SSE with content blocks, or a
// whole message — so a fixture is written once whichever wire the code under test dials. A
// Script's [Discovery] block is what the server advertises to the two probes apogee makes
// before its first completion — the model list on GET /v1/models, rendered in the list shape of
// the wire that asked, and llama.cpp's launch facts on GET /props — or a refused or held probe.
// Nothing about apogee is imported here — the stub is a server, and the code under test
// reaches it through internal/provider exactly as it reaches a real one.
//
// # Why a script rather than a handler
//
// Before this package every test that needed an upstream wrote its own httptest closure, so
// the SSE framing, the tool-call fragment split and the usage shape were re-invented per test
// and drifted from what servers actually send. One scripted server makes the wire shape a
// single reviewed thing, and makes a fixture RECORDABLE from a real server rather than
// hand-written.
//
// # Strictness
//
// The stub is deliberately strict: a request no Turn answers is an HTTP 500 and a logged
// [Request] with Unmatched set, never a plausible improvised reply. A silent fallback would
// turn "the agent asked something the test did not anticipate" — the most interesting failure
// a driver test can surface — into a green run.
//
// # Files
//
//   - script.go — the Script/Turn/Match/Usage/HTTPReply/Cut/InBandError types, the Discovery
//     block a Script advertises, their YAML form, and validation.
//   - match.go — which Turn answers which request: ordered by default, a `when:` turn first;
//     and what that Turn's captures lift out of the request before it is played.
//   - server.go — the HTTP surface: the /v1/models and /props probes, /v1/chat/completions and
//     /v1/messages, SSE and whole replies on both wires, and the `await:` gate a test opens to
//     order one turn's reply behind something apogee did.
//   - transport.go — the in-process transport: the same Handler served over a pipe instead of
//     a socket, for an engine test that plays a Script without listening.
//   - wire.go — the literal OpenAI request/reply JSON the server reads and writes, the
//     /v1/models list and /props payloads included.
//   - wire_anthropic.go — the literal Anthropic Messages request/reply JSON, its /v1/models
//     list shape, and the reduction of a Messages request to the same neutral log entry the
//     chat route records.
//   - log.go — the request log every served request lands in, the probe log the discovery
//     GETs land in, and the assertions over them.
//   - record.go — the recording proxy that turns a real server's traffic — its discovery
//     answers included — into a Script.
//   - cassette.go — the Cassette a live capture is stored as: raw reply chunks with their
//     arrival offsets, verbatim probe answers, and the conversation key an exchange is filed under.
//   - capture.go — the CassetteRecorder: the recording proxy that captures a live upstream into a
//     Cassette, carrying the upstream's key so the client's config stays keyless.
package stubllm
