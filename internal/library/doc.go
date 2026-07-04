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
// apogee processes sharing one LibraryDir may last-writer-win.
package library
