// Package stubllm is the one scripted OpenAI-compatible upstream apogee's tests talk to
// (ADR 0062).
//
// A test names the replies it wants as a [Script] — an ordered list of [Turn]s — and gets an
// HTTP server that plays them back through the wire shapes a real llama.cpp or OpenRouter
// endpoint uses: SSE content deltas, a reasoning channel, streamed tool-call fragments, a
// terminal usage object with the cached-prompt breakdown, plain HTTP failures, and a stall.
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
//   - script.go — the Script/Turn/Match/Usage/HTTPReply types, their YAML form, and validation.
//   - match.go — which Turn answers which request: ordered by default, a `when:` turn first;
//     and what that Turn's captures lift out of the request before it is played.
//   - server.go — the HTTP surface: /v1/models, /v1/chat/completions, SSE and whole replies.
//   - wire.go — the literal OpenAI request/reply JSON the server reads and writes.
//   - log.go — the request log every served request lands in, and the assertions over it.
//   - record.go — the recording proxy that turns a real server's traffic into a Script.
package stubllm
