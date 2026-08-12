// Package profiles resolves WHICH Model profile a given model runs under (CONTEXT: Model
// profile, ADR 0044): the user's `model-profiles:` pattern map ▸ apogee's shipped shape
// table ▸ the zero profile. The nearest layer with a matching pattern supplies the WHOLE
// profile — both axes, tool-call format and thinking channel — because an absent axis
// already means native/none, so a per-axis merge would need an 'inherit' spelling to say
// "leave it" rather than "turn it off".
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
