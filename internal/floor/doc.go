// Package floor is the Floor guards: the engine behaviour that changes only what the model sees
// after its own failure, or shapes the request without steering it. A guard needs no per-model
// proof and cannot regress Bypass, so — unlike a catalogued Mechanism — it is not a nudge the bench
// switches on for a small model but plain behaviour every model runs with (ADR 0071).
//
// The package is PURE POLICY. Each guard is a function over the values the engine already holds — a
// *domain.Response, a *domain.Request, a domain.LoopView and the tool call about to run — returning
// what should change and whether it should. It fires no hook, holds no state between calls, reads
// no clock and touches no filesystem; the seams that call it, the live on/off gate and the events a
// firing emits all live in internal/agent (floorguards.go). That split is internal/agent/prune.go
// over internal/context's: the wiring is the engine's, the decision is the package's.
//
// One deep module, one direction: internal/floor imports internal/domain for the values it reads
// and internal/context for the shared context arithmetic, and nothing else in the tree — never
// internal/agent, never internal/tools, never the root module path (ADR 0010).
//
// Every guard is opted out of individually through domain.FloorConfig, whose ZERO VALUE keeps all
// six ON: the fields are Disable… bools, so an embedder that constructs a bare Config still gets
// the floor, and only a deliberate `<key>: false` in config.yaml takes one away.
//
// # The files, one line each
//
// The substrate. toolnames.go is the tool-name spelling families — the read family and the
// apogee-complete file-mutation superset — and the two predicates composed from them.
// conversation.go is the read-only history scans a guard asks its question with: the path a call
// targets, how many assistant messages there are, whether the model wrote recently, narrated last,
// ever used a tool at all, or is making progress. readerror.go answers whether a committed result
// was a FAILED read (the tool's own marker first, an anchored first-line sniff only for a record
// older than that marker) and canonicalises a path for cross-Turn comparison. intent.go is the
// lexical action-vs-analysis classifier the tool-use enforcer gates on. correction.go renders the
// model-facing correction a repair guard hands back with its retry. prompts.go embeds the guards'
// fixed prose from prompts/*.txt and loads one asset by name.
//
// And doc.go this map.
package floor
