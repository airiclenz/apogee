// Package profiles resolves WHICH Model profile a given model runs under (CONTEXT: Model
// profile, ADR 0044): the user's `model-profiles:` pattern map ▸ apogee's shipped shape
// table ▸ the zero profile. Resolution is AXIS-WISE across those layers (ADR 0057 decision
// 5): each of the three axes — tool-call format, thinking channel, tool roster — takes the
// nearest layer whose matching entry SPELLS it, so an axis an entry leaves out defers
// downward instead of being turned off. The 'inherit' spelling ADR 0044 deferred is
// therefore obsolete: absence IS inherit, and the OFF switch is the spelled zero
// (`tool-call-format: native`, `thinking: {style: none}`, an empty `tools:`), which
// overrides like any other value. Whole-entry replacement became a trap the moment the
// roster joined the map — the likely user entry is a tools-only line for a model whose wire
// shape the shipped table carries, and replacement would have wiped it silently. The thinking
// axis itself resolves as two sub-axes (ADR 0058) — the channel style, carrying its Start/End
// tokens, and the effort dial — because the same trap reappeared inside it: an `effort:`-only
// entry took the whole axis and dropped the shipped style. Both halves are self-describing
// from the domain value (`style: none` is a spelled zero, effort's "" is the absence of the
// dial), so each defers downward on its own and needs no presence flag of its own.
//
// The match rule is a case-insensitive SUBSTRING of the entry's pattern in the advertised
// or resolved model name: the name is what every Upstream reports, and quants, providers
// and `:tag` suffixes all keep some spelling of it (`minimax/minimax-m3:exacto`,
// `minimax-m3-Q4_K_M`). Within one layer the longest pattern wins, equal lengths break
// lexicographically (the smaller pattern wins) so resolution is stable and testable, and
// ANY user match beats ANY shipped match — even a shorter user pattern against a longer
// shipped one, which is what makes an ordinary user entry (`thinking: {style: none}`) the
// escape hatch for a false-positive shipped match. An empty pattern is a substring of
// every name and would silently profile every model, so it never matches.
//
// The confidence gate of the sibling internal/validated surface is deliberately NOT reused
// here: confidence is a fingerprint property that needs the explicit model probe battery
// (ADR 0021), which never runs against a remote server — gating the shipped table on it
// would leave the table dark in precisely the out-of-box case that motivates it. That is
// safe because a tag shape is a chat-template/family fact that survives quants and
// provider spellings, and because a wrong profile is visible and reversible (garbled
// stripping, one config line to override) with no bench-honesty claim riding on it — the
// opposite of a Validated set, whose whole value is the measured claim.
//
// Like internal/validated the package imports internal/domain only and decides nothing:
// it maps a model name to a Decision, and the composition root (cmd/apogee) applies it
// and emits the shipped-match notice, so the engine and the bench never see this package.
//
// Files: entry.go — the pattern-keyed Entry; match.go — Source, Decision and Resolve;
// shipped.go — the built-in shape table.
package profiles
