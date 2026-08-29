// Package library is Apogee's cross-session, per-model learning substrate (CONTEXT
// "Library"). It holds two things: a confidence-tagged ModelFingerprint resolver — the
// identity the store keys observations on — and a file-backed Store of those observations
// with Bayesian confidence counts.
//
// This package is the substrate only. The loop-facing halves — an observer that records
// completed-Turn outcomes and a pre-request Mechanism that injects qualifying observations —
// are catalogued Mechanisms built on top of the Store (phase-4 item 14); this package never
// imports internal/agent or internal/mechanisms.
//
// The Store is rooted at an injected directory (Config.LibraryDir) and NEVER reaches for an
// ambient ~/.apogee itself (ADR 0001): the composition root supplies the production default,
// and the bench points it at an ephemeral dir so a sim run never touches the production
// Library (decision 11). The Store is process-local: it guards its in-memory map with a
// mutex for intra-process safety but makes no cross-process locking claims in v1 — two
// apogee processes sharing one LibraryDir may last-writer-win. WITHIN one process there is
// exactly one Store per directory, because every whole-file snapshot is written from one
// memory: Open hands each builder that names a directory the same instance, and NewStore is
// the door to a private store that shares nothing (a test fixture, a bench Driver's seed).
//
// The write model is asynchronous: recording an observation only marks the store dirty, and a
// single writer goroutine debounces those marks into one whole-file snapshot, so no caller — and
// under ADR 0039 fan-out, no sub-agent's post-response hook — ever waits on the filesystem. An
// observation therefore reaches disk within the debounce window or at Close; a process that exits
// within 200ms of its last observation without closing the store loses that observation, which is
// the accepted cost of a best-effort learning substrate.
package library
