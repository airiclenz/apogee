// Package processing turns an Upstream response into the loop's domain values: it parses
// tool calls into domain.ToolCall and strips inline thinking / harmony channels from the
// assistant's visible content. apogee-code's TypeScript is the behavioural oracle and its
// ported test vectors are the parity gate (TDD §8 #6).
//
// Phase 1 (P1.3) ported one tool-call format end-to-end — the native/JSON shape the
// provider already extracts structurally (FunctionCall.Arguments kept verbatim) and that
// the bench relies on — plus single-pair thinking-channel stripping (gemma `<think>`,
// gpt-oss harmony `<|channel|>analysis<|message|>…<|end|>`).
//
// Phase 3 (P3.5) completes the parity port. The two text tool-call formats are added behind
// the ToolCallParser interface: MarkdownFencedParser (a ```tool fenced block with named
// argument markers, plus a marker-based fallback) and CustomRegexParser (a user-supplied
// named-group regex). NewToolCallParser is the processor-factory that selects native /
// markdown-fenced / custom-regex per model config; native is a text no-op because the
// structured path (ParseNativeToolCalls) owns native calls. StripHarmony adds the full
// gpt-oss harmony channel set (analysis / commentary / final) over the single analysis-pair
// StripThinking handles, routing each channel and honouring the <|end|> / <|call|> /
// <|return|> terminators. Every format is gated by ported apogee-code TS test vectors (the
// riskiest-port discipline — the TS is the oracle); a malformed payload degrades to the
// no-call path, never a panic and never a Turn failure (the P1.3 contract).
//
// The package is wired into the loop through ParserFor: it translates the declarative
// domain.ModelProfile on Config into the two parse-seam collaborators — the text-format
// ToolCallParser (native / markdown-fenced / custom-regex) and a unified ContentStripper over the
// thinking styles (none / delimited / harmony) — by mapping the profile onto the frozen
// ToolCallingConfig / ThinkingConfig and calling NewToolCallParser. internal/agent selects both
// once at construction and calls them at the seam, so the format→parser knowledge stays here and
// the oracle config types never surface in the loop. A zero profile yields the native no-op
// parser and no-op stripper, so a native model's content path is byte-identical.
//
// InstructionsFor is the emit-side mirror of ParserFor at the request seam: for a non-native
// profile it renders the text tool menu plus the format-specific tool-call instructions the model
// needs to LEARN its tools and the exact markup to emit (ported from the apogee-code context
// builder — the same profile knobs and withDefaults() the parser reads, so what we tell the model
// and what we parse cannot drift). internal/agent injects the block as a wire-only system message
// and suppresses the native tools array for a non-native format; a native/zero profile or an empty
// menu renders "", so the wire request stays byte-identical. Emission-side format knowledge lives
// here beside the parsers, never in the loop.
//
// The package depends only on internal/domain (+ stdlib): the loop adapts provider wire
// tool calls into NativeToolCall at the seam, so wire types stay provider-local (ADR 0010).
package processing
