// Package snapshot is the session-owned object store behind persistent undo: a bare git
// object database, living outside the workspace, that holds a whole-tree image of the
// workspace per capture (ADR 0074).
//
// Why a git object database. Undo needs two things a hand-rolled store makes expensive:
// content-addressed dedupe across dozens of near-identical whole-tree images, and a diff
// between two of them. git's plumbing has both, and apogee already runs a hardened git for
// its tools. The price — git is a convenience dependency (ADR 0042 decision 2) — is paid by
// the caller, not here: where git is missing or unusable, [Open] fails and the engine keeps
// ADR 0051's in-memory funnel journal, which is a supported configuration rather than a
// degraded one.
//
// What it never touches. The store is a bare GIT_DIR the session owns, its index file lives
// inside that directory, and the workspace is handed in as GIT_WORK_TREE for the duration of
// a call. Nothing is ever written under the workspace — no repository of ours, no index, no
// ignore file, no lock, no stray object — and the user's own repository, index, stash,
// branches and reflog are neither read nor written. [Open] refuses a store directory that
// would sit inside the workspace, so that invariant cannot be lost by a bad caller.
//
// What a capture covers, and what it does not. [Store.Capture] stages the whole work-tree and
// returns the tree id, so it covers every write however it arrived — subprocess, MCP,
// embedder-registered tool — which is exactly the coverage the funnel journal cannot have.
// Its boundary is git's own add pipeline (ADR 0074 decision 12): a path the workspace's own
// .gitignore excludes, a nested repository git will not descend into, a path a filter driver
// rewrites. That residue is a rule rather than a list, and the funnel's pre-image stays
// authoritative for every path in it. The operator's global core.excludesFile is deliberately
// disabled during a capture (decision 11) so what undo covers is a property of the workspace
// the human can see, not of a dotfile in their home directory.
//
// Reads are uncapped. [Store.Content], [Store.Diff] and [Store.ListBlobs] stream through
// gitexec.RunTo rather than the 256 KiB-capped capture path: a blob read back for a restore,
// or the path list of a wide Exchange, must never be silently truncated into a corrupt undo.
//
// Restoration is NOT here. This package reads and writes objects; putting bytes back on disk
// — the conflict check, the skip-and-report, the fenced writes — is internal/undo's, which
// reaches this package through an interface of its own rather than importing it. The package
// is a leaf over internal/gitexec and imports nothing else of apogee's.
//
// Files:
//   - doc.go — this map and the package's rationale.
//   - store.go — the Tree id type, Open/Remove/Available, and the capture, diff, listing and
//     blob-read calls that make up the store's surface.
package snapshot
