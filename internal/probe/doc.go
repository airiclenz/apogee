// Package probe answers "what is this machine, and what will apogee be able to do on it?"
// without running an agent — the host half of `apogee probe` (ADR 0021).
//
// It is deliberately the SHARED home of the confinement wording rather than a second copy of
// it: the backend label, the capability matrix line and the Auto-degradation notice are
// rendered here and used by both the CLI report and the TUI's /confine status, so the two
// renderings of one verdict cannot drift apart.
//
// The HOST half is either pure (the report and its wording — table-testable on any host) or a
// read-only observation of facts the machine already has: the Confiner's capability matrix, and
// the Upstream's GET /v1/models + llama.cpp GET /props discovery outcome. Nothing on that path
// writes, executes, or calls a model.
//
// The MODEL half — RunBattery and GatherModel — is the other kind of thing entirely, and the
// package keeps the two textually apart for that reason. It spends real tokens on a live
// Upstream (native tool call, structured JSON, a multi-step tool chain, and a one-token
// candidate-distribution probe) and, when the run completes, it earns the model's advertised
// label a fingerprint at domain.ConfidenceMedium. The battery raises an identity's TIER; it
// never re-spells it (ADR 0021, Amendment 2026-07-22) — the label is the key Validated-set
// entries, aliases and Library observations are filed under. What was observed travels beside
// the identity as the BehaviorSignature: a fuzzy feature match, never a hash of a response,
// which sampling alone would move (ADR 0021 §6). It still writes nothing itself: the
// record is persisted by the composition root through internal/library, so `--no-save` is a
// genuine off-switch rather than a rollback, and so the one act that promotes a model from
// "a Validated set is offered" to "a Validated set is applied" (ADR 0016 §5) stays visible at
// the command that performs it.
//
// The capability tier the model report carries is a REPORTED SIGNAL ONLY — nothing reads it.
// Adaptive prompt complexity, the transform that would, is a follow-on recorded in the issue
// register: a model-facing transform owes the catalogue a bench campaign first (ADR 0009).
//
// # The files, one line each
//
// The host half. host.go is that report: the injected Inputs the composition root has already
// resolved, GatherHost, and the rendering of every line it states — the Auto verdict, the
// confined-roots line, the upstream lines, the API-key line. discovery.go is the one thing this
// half asks the network, and it asks read-only: GET /v1/models plus llama.cpp's GET /props, with a
// zero Discovery meaning "no endpoint was configured", which is a report and not a failure.
// confinement.go is the shared confinement wording — BackendName, CapabilityLine,
// DegradedNotice, ResidualNotice, AutoUnattendedBlocked — rendered once here and quoted by the CLI
// report, the TUI's /confine status and startup, and the two unattended surfaces that refuse Auto
// (a Schedule's Firing and `apogee headless`), so no surface can tell a user its own story about
// one verdict.
//
// The model half, the part that spends tokens. battery.go is the live suite: the Capability slugs
// (stable, because they are folded into the fingerprint), the Chat seam it calls the Upstream
// through, the probes themselves — native tool call, structured JSON, multi-step chain, and the
// one-token candidate distribution — the thinking observation, and BatteryVersion, which stamps
// every record because a label earned under one battery is not comparable to one earned under
// another. The two prompts it sends are not in that file but beside it, as plain embedded assets:
// prompts/system-prompt.txt and prompts/candidate-prompt.txt, with prompts/README.md — the one
// file there that is NOT embedded — stating the rule that governs the directory, that every byte
// under it is folded into the fingerprint and so editing one is a BatteryVersion bump.
// modelfingerprint.go is what a run is turned INTO: the ordinal Tier, the
// domain.ModelFingerprint, the BehaviorSignature (a fuzzy feature match, never a hash of a
// response, which sampling alone would move), and the suggested ModelProfile with its YAML
// rendering. model.go is the model report — ModelInputs, GatherModel, the SaveOutcome the
// composition root fills in after it persists, and the report sections.
//
// The terminal half. terminal.go is `apogee probe terminal` whole: the alternate-screen
// measurement session (DSR-CPR read-back, mode reports, glyph widths, tab stops, the last-column
// wrap, the cursor-addressing capability set) and the table that sets each observation beside what
// apogee's renderer assumes. terminal_windows.go is the one measurement no escape sequence can
// make — reading what is IN a cell, out of the console's own screen buffer — and terminal_other.go
// is the twin that answers "unverified" everywhere else, so all six release targets compile the
// probe rather than guessing.
//
// And doc.go this map.
package probe
