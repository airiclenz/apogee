// Package processing turns an Upstream response into the loop's domain values: it parses
// tool calls into domain.ToolCall and strips inline thinking / harmony channels from the
// assistant's visible content. apogee-code's TypeScript is the behavioural oracle and its
// ported test vectors are the parity gate (TDD §8 #6).
//
// Phase 1 (P1.3) ported one tool-call format end-to-end — the native/JSON shape the
// provider already extracts structurally (FunctionCall.Arguments kept verbatim) and that
// the bench relies on — plus single-pair thinking-channel stripping (a delimited
// `<think>`-style pair, gpt-oss harmony `<|channel|>analysis<|message|>…<|end|>`).
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
//
// # The files, one line each
//
// The seams. parser.go declares ToolCallParser — parse a call out of the visible content, strip
// its markup back out — and nothing else, because the interface is what internal/agent is written
// against. parserfor.go is the loop-facing selector: the ContentStripper interface, ParserFor
// turning a declarative domain.ModelProfile into the concrete parser/stripper pair, and the three
// strippers themselves (none, delimited, harmony), each a thin adapter over an engine below.
// factory.go is the parser half of that selection, kept in the oracle's own shape — ToolCallFormat,
// the frozen ToolCallingConfig, NewToolCallParser, and the native text parser that deliberately
// finds nothing, since ParseNativeToolCalls already owns the structured path.
//
// The tool-call formats, one file each. toolcall.go is the native/JSON shape: NativeToolCall as an
// OpenAI-compatible server delivers it, ParseNativeToolCalls, and ErrMalformedToolCall — the
// sentinel a bad call degrades to a tool-error path through instead of failing the Turn.
// markdown_fenced.go is the fenced tool block: its config defaults, the strict fence parse, and the
// marker-based fallback for a model that emitted the markers but forgot the fence. custom_regex.go
// is the user-supplied named-group regex escape hatch, including the JavaScript (?<name>…) → Go
// (?P<name>…) rewrite and the never-match parser an invalid pattern degrades to. args.go is what
// those two share: value coercion (valid JSON kept as a JSON value, anything else a JSON string)
// and sorted-key argument marshalling, so an encoding is deterministic rather than map-ordered.
//
// The thinking channels. thinking.go is the delimited pair — ThinkingConfig, StripThinking, and
// the IsThinking mid-span guard a streaming consumer holds emission on. harmony.go is the gpt-oss
// channel set: the analysis / commentary / final routing, the <|end|> / <|call|> / <|return|>
// terminators, the still-streaming unterminated case, and IsHarmonyThinking.
//
// The emit side. instructions.go renders what we TELL a non-native model — the text tool menu and
// the format-specific markup instructions, with the example call picked out of the menu — from the
// same profile knobs and defaults the parsers read, so the two halves of the contract cannot drift.
//
// And doc.go this map.
package processing
