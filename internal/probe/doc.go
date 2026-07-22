// Package probe answers "what is this machine, and what will apogee be able to do on it?"
// without running an agent — the host half of `apogee probe` (ADR 0021).
//
// It is deliberately the SHARED home of the confinement wording rather than a second copy of
// it: the backend label, the capability matrix line and the Auto-degradation notice are
// rendered here and used by both the CLI report and the TUI's /confine status, so the two
// renderings of one verdict cannot drift apart.
//
// Everything in the package is either pure (the report and its wording — table-testable on any
// host) or a read-only observation of facts the machine already has: the Confiner's capability
// matrix, and the Upstream's GET /v1/models + llama.cpp GET /props discovery outcome. Nothing
// here writes, executes, or calls a model. The capability battery — which spends tokens AND
// writes a fingerprint record — is `apogee probe model`'s business (ADR 0021 §3), an explicit
// act that does not hide behind these functions.
package probe
