// Package adoption records the user's answers to a Project config's granting entries (ADR 0096 §4,
// the glossary's "Adoption"): which of them are adopted and which rejected, pinned to each entry's exact
// content and to the Project root it sits under.
//
// A Project config may be committed, so anything in it can arrive from a teammate, a pull or a
// write behind apogee's back. An entry that GRANTS something — an Allow rule today — therefore does
// nothing until the user adopts it, and the record of that adoption lives outside the repository,
// where nothing the repository carries can write it. The pin is a fingerprint of the entry's kind
// and text, so an edited entry, even one whose only change is whitespace, no longer matches and is
// proposed again; a rejection is pinned the same way and stays quiet until the entry changes. An
// Allow rule's text is the rule exactly as written; a structured entry's (an MCP server, a
// Reaction — ADR 0104) is a canonical form its caller builds, so the exact-text rule applies to
// that canonical text.
//
// One Project root has one record file, `<dir>/<sha256 of the symlink-resolved root>.yaml` — the
// caller passes dir, which the binary sets to `~/.apogee/workspaces` (ADR 0001: this package never
// reaches for an ambient home). The file names the root it belongs to and maps each fingerprint to
// `adopted` or `rejected`; a fingerprint it does not hold is proposed. For a structured entry —
// one with a handle — the record also keeps the adopted text itself under `pins`, keyed
// `<kind>/<handle>` ([Store.Adopted]), so a re-ask can name what changed; an Allow rule's text is
// never written, only its fingerprint. A record written before pins existed reads unchanged. The
// directory is owner-only, the file is owner-read/write, and every write replaces the file
// atomically under a lock file beside it, so two apogee processes in one repository cannot drop
// each other's answers. The same
// directory holds the Project config writer's lock ([ConfigLockPath]), so a write to the Project
// config leaves nothing in the repository but the config itself.
//
// The package is a leaf above internal/platform (its lock file) and knows no config schema: an
// [Entry] is a kind — the granting key the entry sits under — an optional handle naming a
// structured entry, and the entry's text, and the caller decides which entries grant and builds
// each structured entry's canonical text.
//
// Files:
//
//	store.go  Entry, Fingerprint, the per-root Store (Classify, Adopted, Adopt, Reject, Forget) and ConfigLockPath
package adoption
