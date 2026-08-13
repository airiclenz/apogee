// Package mechanisms is the curated Mechanism catalogue: a constraint-declared
// registry that the loop resolves into a deterministic total order (topo-sort
// with a stable canonical-ID tiebreak — ADR 0003). Each Mechanism declares its
// hook point, descriptor, and ordering constraints; the hook point is data, not
// package structure (the package-per-hook layout remains provisional — TDD §6.4).
//
// The catalogue was ported from apogee-sim and A/B-validated, one Mechanism at a
// time, in Phase 4 (completed 2026-07-04); each Mechanism file registers one
// catalogue row — descriptor, ordering constraints, constructor, and the Deps the
// Mechanism needs the engine to derive (row.needs, zero for every row but library)
// — in its own init(). The row is the single source of a Mechanism's metadata:
// Build joins it to the constructed hook once, so a Mechanism and the row
// describing it cannot disagree (ADR 0003 as amended 2026-07-25).
//
// File naming here is compact, no underscores, following the package's own majority and
// internal/tui (ADR 0043); an underscore now means only a _test.go twin.
//
// # The Mechanism files, one line each
//
// Nineteen files carrying twenty-one catalogue rows — one row per file, except cot.go, which
// carries three. Each holds its Mechanism's init() registration, descriptor, constructor, hook
// implementation, and the logic only that Mechanism uses.
//
// autofix.go is formatter repair: a written file is handed to the language's real formatter in a
// gated sub-process and the repaired content is spliced back into the call. cachedcontent.go is
// cached_content_intercept, the pre-tool-exec interceptor that answers a redundant successful
// re-read from the conversation instead of the filesystem. cot.go is the Wave-4 completion-nudge
// trio — tool_use_directive, stall_nudge, list_nudge — apogee-sim's `cot` Transform split into
// three independently firing pre-request nudges (catalogue C4/Table C), of which at most one of
// stall_nudge / list_nudge may be armed. decompose.go is the task-decomposition steer, and the home of
// the F8 spelling families the history family composes with (readSpellings, listSpellings,
// wave4WriteTools). emptyresponse.go is empty_response_recovery, the off-ramp that turns an empty
// reply into a continue-the-task nudge (Bypass-exempt: without it a failed Turn has no exit).
// errorenrich.go is error_enrichment, which classifies a tool error and appends category-shaped
// suggestions when the same category repeats. filehint.go scores a freshly listed directory
// against the user's prompt and hints at the files worth reading. grammar.go derives a json_schema
// from the tool menu into the request's response_format, constraining a model that cannot emit
// native tool calls into a valid tool-call shape. guideddecomposition.go is apogee's own
// guided_decomposition (ADR 0014): the enumeration steer plus the batched sub-agent fan-out
// follow-through, Depth-0 and once-per-Exchange gated. library.go is the Library Mechanism — the
// only row that declares a needs (the Library store the engine derives). readloop.go detects a
// path read over and over without progress and hints at it; readrepeat.go catches the redundant
// re-read in the response about to be sent. syntax.go is the write-content syntax-check Mechanism
// (the checker itself is syntaxengine.go, below). toolfilter.go narrows the tool menu before the
// request goes out. toolloop.go is tool_loop_interceptor, the identical-repeat-turn detector, and
// the file that declares this package's prompt-asset embed and its mustPrompt loader (the prompts/
// directory, below) — cot.go, decompose.go, emptyresponse.go and library.go load their own assets
// through it. toolresultcap.go caps oversized tool results mid-Exchange — the one reducer that shapes a
// request rather than the history. tooluseenforcer.go is the narration off-ramp: an action request
// answered in prose is corrected into a tool call. truncatehistory.go is the drop-the-middle
// history rewrite. validate.go checks a response's tool calls against the menu and the schema.
//
// # The shared plumbing, one line each
//
// Seven files that register nothing. They hold the registry itself and the helpers more than one
// Mechanism needs, so a shape lives once instead of once per Mechanism (D5).
//
// catalogue.go is the registry: the row, the package-private register() every Mechanism file
// calls from init(), the injected Deps and the DepNeeds derivation, and the Build / KnownIDs /
// Descriptors surface the engine resolves a stack through. historyhints.go carries what the Wave-3
// history-aware family shares — the read/write tool-name sets, the path and error-content
// sniffers, and the list-tool set greenfield detection inspects (note the two write semantics:
// isFileMutatingTool for "did this mutate a file", the narrower isWriteTool for content repair).
// historyscan.go owns one copy of each shared conversation-walk shape (read-attempt counting,
// recent successful reads, write-path collection); membership and thresholds stay at the call
// site. intent.go is the lexical action/analysis/question classifier — a shared helper with no
// catalogue row of its own (catalogue C6). offramps.go carries the Wave-1 off-ramp helpers: the
// read-tool set, tool-call path extraction, and the conversation predicates the off-ramps gate on.
// robustness.go carries the Wave-1 robustness helpers: the robustnessIssue type, the correction
// message built from a set of issues, the write-tool sets, and the write payload accessors that
// read and rewrite a call's path/content. syntaxengine.go is the pure syntax checker — the Go
// parser path and the bracket/string/truncation heuristic for everything else — called by
// syntax.go and registering nothing itself.
//
// # The prompt assets
//
// prompts/ is not Go: it holds this package's prompt text as plain .txt files — the wording as
// editable prose rather than string literals buried in code (ISSUES.md: hard-coded prompt
// literals) — which toolloop.go compiles into the binary with go:embed, so nothing is read from
// disk at runtime and nothing is user-overridable. It carries the fixed sentence fragments of
// tool_loop_interceptor's loop-breaking directive, the cot and decompose directives, the two
// library behavioural notes and that Mechanism's injection-block header, and the
// empty_response_recovery nudge. Only the fixed text lives there: the branching, the %s
// substitution, the sentence-joining spaces and the trailing newlines stay in Go, as do the
// @pin/rationale comments (an asset carries no comments — a comment line in a .txt would be sent
// to the model) and the idempotency markers. That last split is the one cross-file invariant:
// AppendToSystem suppresses a repeat inject by finding a directive's marker in the system prompt,
// so every marker const must remain a substring of the asset it belongs to — re-word an asset and
// keep its marker verbatim. TestPromptAssetsKeepTheirMarkers (prompts_test.go) is the gate.
//
// And doc.go this map.
package mechanisms
