// Package processing turns an Upstream response into the loop's domain values: it parses
// tool calls into domain.ToolCall and strips inline thinking / harmony channels from the
// assistant's visible content. apogee-code's TypeScript is the behavioural oracle and its
// ported test vectors are the parity gate (TDD §8 #6).
//
// Phase 1 (P1.3) ports one tool-call format end-to-end — the native/JSON shape the
// provider already extracts structurally (FunctionCall.Arguments kept verbatim) and that
// the bench relies on — plus thinking-channel stripping (gemma `<think>`, gpt-oss harmony
// `<|channel|>…<|end|>`). The other formats (markdown-fenced, custom-regex) and the full
// harmony channel set are Phase 3.
//
// The package depends only on internal/domain (+ stdlib): the loop adapts provider wire
// tool calls into NativeToolCall at the seam, so wire types stay provider-local (ADR 0010).
package processing
