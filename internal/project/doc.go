// Package project owns Adoption as a Driver meets it (ADR 0096 §4, ADR 0104): one [Snapshot] of
// the layered config per event — the global config with the Project config layered over it, sorted
// by the Project root's adoption record — and the [Diff] between two of them, so a Driver applies
// only the kinds an answer or an edit moved.
//
// A Snapshot is read once ([Load]) and never changes: its options, the Project config's offer and
// the entries that offer asks the user about all come from that one resolution, and every accessor
// hands out a copy. The record itself stays in internal/adoption; this package reads it through the
// layered loader (config.LoadLayeredOffer) and pins answers with adoption.Fingerprint.
//
// Files:
//   - doc.go: this map.
//   - snapshot.go: Snapshot, Load, Diff, and the entries an answer records (ProposalEntry,
//     RuleEntry) in the spellings the engine and the `allow:` key use (EngineAllowRules, AllowKind).
package project
