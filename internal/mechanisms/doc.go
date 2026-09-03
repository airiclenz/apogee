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
// One file carrying the one surviving catalogue row. It holds its Mechanism's init() registration,
// descriptor, constructor, hook implementation, and the logic only that Mechanism uses. prompts.go
// is described with it and registers nothing: it holds the prompt-asset embed library.go loads
// through.
//
// library.go is the Library Mechanism — the only row left, and the only one that ever declared a
// needs (the Library store the engine derives). prompts.go declares this package's prompt-asset
// embed and its mustPrompt loader (the prompts/ directory, below) — library.go loads its own assets
// through it.
//
// # The shared plumbing, one line each
//
// Six files that register nothing. They hold the registry itself, the roll of retired IDs, and the
// helpers more than one Mechanism needs, so a shape lives once instead of once per Mechanism (D5).
//
// catalogue.go is the registry: the row, the package-private register() every Mechanism file
// calls from init(), the injected Deps and the DepNeeds derivation, and the Build / KnownIDs /
// Descriptors surface the engine resolves a stack through. historyhints.go carries what the
// retired Wave-3 history-aware family left behind — the F8 spelling families every set composes
// from (readSpellings, listSpellings, wave4WriteTools) with their toolSet union, and the path and
// error-content sniffers (isFileMutatingTool, "did this call mutate a file", is the one write
// semantic left; the narrower content-repair one went with syntax and autofix).
// intent.go is the lexical action/analysis/question classifier — a shared helper with no
// catalogue row of its own (catalogue C6). retired.go is the roll of IDs this build no longer
// catalogues, so a saved config or Validated set naming one is tolerated rather than refused — and
// the home of ResolveEnabled, the resolver a Driver runs a `mechanisms:` block through: it
// validates every key against the catalogue, drops the retired ones, and hands back the notices
// they earn — one for a removed row the block still turns on, and one for a PROMOTED row the block
// tries to switch off with a key that no longer does that.
// robustness.go carries what the Wave-1 robustness rows left behind: the robustnessIssue type and
// the tool-call validation helpers library observes through.
//
// # The prompt assets
//
// prompts/ is not Go: it holds this package's prompt text as plain .txt files — the wording as
// editable prose rather than string literals buried in code (ISSUES.md: hard-coded prompt
// literals) — which prompts.go compiles into the binary with go:embed, so nothing is read from
// disk at runtime and nothing is user-overridable. It carries the two library behavioural notes and
// that Mechanism's injection-block header. Only the fixed text
// lives there: the branching, the %s
// substitution, the sentence-joining spaces and the trailing newlines stay in Go, as do the
// @pin/rationale comments (an asset carries no comments — a comment line in a .txt would be sent
// to the model) and the idempotency markers. That last split is the one cross-file invariant:
// AppendToSystem suppresses a repeat inject by finding a directive's marker in the system prompt,
// so every marker const must remain a substring of the asset it belongs to — re-word an asset and
// keep its marker verbatim. TestPromptAssetsKeepTheirMarkers (prompts_test.go) is the gate.
//
// And doc.go this map.
package mechanisms
