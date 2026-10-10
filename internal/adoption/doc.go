// Package adoption records the user's answers to a Project config's granting entries (ADR 0096 §4,
// CONTEXT.md "Adoption"): which of them are adopted and which rejected, pinned to each entry's exact
// content and to the Project root it sits under.
//
// A Project config may be committed, so anything in it can arrive from a teammate, a pull or a
// write behind apogee's back. An entry that GRANTS something — an Allow rule today — therefore does
// nothing until the user adopts it, and the record of that adoption lives outside the repository,
// where nothing the repository carries can write it. The pin is a fingerprint of the entry's exact
// text, so an edited entry, even one whose only change is whitespace, no longer matches and is
// proposed again; a rejection is pinned the same way and stays quiet until the entry changes.
//
// One Project root has one record file, `<dir>/<sha256 of the symlink-resolved root>.yaml` — the
// caller passes dir, which the binary sets to `~/.apogee/workspaces` (ADR 0001: this package never
// reaches for an ambient home). The file names the root it belongs to and maps each fingerprint to
// `adopted` or `rejected`; a fingerprint it does not hold is proposed. The directory is owner-only,
// the file is owner-read/write, and every write replaces the file atomically under a lock file
// beside it, so two apogee processes in one repository cannot drop each other's answers. The same
// directory holds the Project config writer's lock ([ConfigLockPath]), so a write to the Project
// config leaves nothing in the repository but the config itself.
//
// The package is a leaf above internal/platform (its lock file) and knows no config schema: an
// [Entry] is a kind — the granting key the entry sits under — and the entry's text, and the caller
// decides which entries grant.
//
// Files:
//
//	store.go  Entry, Fingerprint, the per-root Store (Classify, Adopt, Reject, Forget) and ConfigLockPath
package adoption
