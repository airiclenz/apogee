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
// Adaptive prompt complexity, the transform that would, is a recorded TODO.md follow-on: a
// model-facing transform owes the catalogue a bench campaign first (ADR 0009).
package probe
