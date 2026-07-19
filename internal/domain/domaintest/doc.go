// Package domaintest is the hook seam's shared test adapter (the
// internal/platform/confinetest precedent): conversation fixtures and a settable
// LoopView fake, so a Mechanism or engine test builds history and loop state through
// one vocabulary instead of hand-rolled per-file literals.
//
// It is test-support, not production code: it lives in its own package so any
// package's _test.go can import it without a test-only build tag on internal/domain
// itself, and it is internal, so it carries no public-API promise (ADR 0002 —
// Mechanisms are curated; the hook interface, not this adapter, is the surface).
//
// Three pieces:
//
//   - ConversationBuilder — a fluent history builder (User / AssistantText /
//     AssistantCalls / ToolResult / Messages), returning plain []domain.Message.
//   - Canned constructors — the message shapes the builder appends, exposed as
//     package-level functions (UserMessage et al.) so a one-message fixture or a
//     delegating package-local helper stays a one-liner, plus the tool-call builders
//     Call and ReadCall (the read_file shape the read-counting Mechanisms inspect).
//   - FakeLoopView — a settable implementation of domain.LoopView. The LoopView
//     docstring anticipates exactly this fake; an internal implementer of the public
//     interface carries no semver cost, which is why the interface itself gains no
//     test affordances.
package domaintest
