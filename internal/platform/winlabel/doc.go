// Package winlabel is the Windows mandatory-label mechanism: the SDDL vocabulary the
// backend writes, the journal that records every label before it is applied, the walk
// that applies and clears them, and the wording every surface that reports outstanding
// labels quotes (ADR 0020; docs/design/confinement-execution-contract.md §9).
//
// A mandatory label is an ACE in an object's SACL naming an integrity level, and the
// kernel's mandatory check runs BEFORE the DACL. The Windows backend therefore fences by
// IDENTITY — the confined child runs under a restricted Low-integrity primary token — and
// nothing in that model takes "these paths are writable" as an argument. The only way to
// express a box's writable half is to label its roots Low ON THE DISK for the duration of
// the run and revert them on teardown. That is a mutation of the user's machine, which
// neither landlock nor seatbelt performs, and it is why ADR 0020 §2's one rule exists: the
// journal is written BEFORE any label lands, so a process killed mid-pass still leaves
// enough behind to undo what it did. Journal first, label second, always — this package
// exists so that ordering is a property of the module rather than a convention three call
// sites in package platform remember.
//
// The stateful half is Journal (session.go): one session's record, the file it lives in and
// the roots it has already walked, behind its own lock. The backend COMPOSES one rather than
// carrying that state as five fields and a mutex of its own, which is what lets its label pass
// be a bare loop. Journal's doc comment carries the lock discipline every method here obeys.
//
// Everything outside walk_windows.go is a pure function or plain JSON file I/O, so the
// mechanism's DECISIONS — what a journal entry may say, which descendants are labelled,
// which journal files may be deleted, how residue is worded — are table-testable on Linux
// and macOS exactly as the seatbelt profile generator is (platform/seatbelt.go). Only the
// walk itself needs a real SACL, and it is the one Windows-tagged file here.
//
// The package is a LEAF: the standard library plus golang.org/x/sys/windows, and nothing
// from apogee — not internal/domain, not internal/platform. Errors come back plain and
// package platform wraps domain.ErrConfinementUnavailable once at the call site, which
// keeps the boundary one-way instead of letting it rot back into a two-way seam.
// TestPackageImportsNothingFromApogee holds the rule for the build-tagged files too.
//
// Some exports here exist for package platform's Windows-tagged tests rather than for
// production callers: confiner_windows_test.go asserts against real SACLs and real
// journal files, and it stays with the backend it tests. They are IsLowLabel, the
// journal-file API (JournalDir, JournalPath, ListJournals, ReadJournal, WriteJournal),
// ResidueIn, the home-taking form of Residue, and the Journal's snapshot readers Entries and
// PriorLabels plus ForgetLabelled. They are the module's honest file/OS surface
// — production code in this package uses the same verbs — but they are named in the doc so
// a later audit does not re-litigate why the surface is as wide as it is.
package winlabel
