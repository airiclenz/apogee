// Package library is Apogee's per-model identity substrate (CONTEXT "Library"). It holds two
// things: a confidence-tagged ModelFingerprint resolver — the identity a Validated set is keyed
// on — and the on-disk behavioral-probe record `apogee probe model` writes and that resolver
// reads back.
//
// The confidence-tagged observation Store this package once carried went with the `library`
// Mechanism it existed for: both retired in v0.20.0 on ADR 0071's ratified verdict, and with them
// Config.LibraryDir and the ~/.apogee/library directory the engine used to inject. A user's
// existing library.json on disk is simply never read again — nothing deletes it.
//
// This package is the substrate only: it never imports internal/agent, internal/mechanisms or
// internal/probe. The last of those is the direction that matters — probe WRITES records, this
// package READS them, so ProbeBatteryVersion is mirrored here rather than imported (see
// proberecord.go).
//
// Probe records are rooted at an injected directory (the apogee home a Driver passes in) and this
// package NEVER reaches for an ambient ~/.apogee itself (ADR 0001): the composition root supplies
// the production default, and a test points it at an ephemeral dir. Each record is its own file,
// so ADR 0021 §4's printed undo — "delete this file" — is a real off-switch.
//
// # The files, one line each
//
// fingerprint.go resolves a configured model id into a domain.ModelFingerprint with the
// confidence its evidence earns — a probe record, the advertised label, or nothing.
// proberecord.go is the record itself: its schema version, the digest that names its file, and
// the Save/Load pair, with the owner-only directory and file permissions the writer uses.
// And doc.go this map.
package library
