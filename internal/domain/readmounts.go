package domain

import "io/fs"

// ReadMounts are the read-only trees a read tool resolves a path over BESIDE its own workspace
// root. Roots names extra DISK roots (host paths the operator opened up — a skills library),
// Scratch names the session's own scratch dir (the one dir the host announces WRITABLE, which must
// therefore be readable too), and Virtual names trees that have no host path at all, keyed by the
// prefix their addresses are spelled under (`shipped:` — internal/tools/path_virtual.go). It is
// one struct rather than three parameters because the three answer ONE question — what else may a
// read tool reach — and a tool that grows a fourth kind of mount should not grow a fifth
// constructor argument. internal/tools spells it tools.ReadMounts, an alias of this type.
//
// The contract every mount shares, in four clauses:
//
//   - READ-ONLY: the read tools take it, and copy_file for its SOURCE alone — a copy's source is a
//     read (2026-08-12). Every WRITE stays workspace-fenced, copy_file's destination included, as
//     does every execution tool (the workspaceScopedWriter discipline, ADR 0012 D1) — mounting a
//     tree here never makes it writable.
//   - ABSOLUTE paths only for the disk mounts: a relative argument keeps resolving against the
//     workspace root alone, so no one name can mean two files. A virtual mount is reached by its
//     prefix, a spelling no host path can take.
//   - LIVE: each func is evaluated once per tool call, so a mid-session change on the host's side
//     (a rotated session's scratch dir, a changed skill setting) is honoured by the next read with
//     no re-wiring. That is why the three are funcs, not values.
//   - The ZERO value is workspace-only, byte-identical to the fence before any mount seam existed,
//     which is why it is the value every test and every tool-less host passes.
//
// It is a generic seam: a skills library is the first thing mounted through it, but nothing in the
// read tools knows that (ADR 0031 — engine seams stay driver-agnostic). Each disk root keeps its
// own os.Root fence, so a symlink inside one that escapes it is still refused, and a root that does
// not exist yet is skipped rather than failing the call.
type ReadMounts struct {
	// Roots reports extra read-only DISK roots, each the host's symlink-RESOLVED real path (a root
	// that is not its own real path is skipped by the read scope). It is also what the host
	// announces as its LIBRARY roots, on the orientation's `Read-only library roots:` line. nil ⇒
	// the workspace root alone.
	Roots func() []string
	// Scratch reports the session's live scratch dir in the spelling the host ANNOUNCES it under
	// (the orientation's `Scratch dir:` bullet) — the one dir the read tools must never refuse: a
	// model that writes a probe where it was told to and cannot read it back was misled by its own
	// host. The read scope resolves it to its real path itself, on the way in, because an announced
	// spelling may run through a symlinked home and the disk-root contract is real paths only. It
	// rides beside Roots rather than inside it so the host's own view of Roots (its announced
	// library roots) stays what it is, and the scratch dir keeps the bullet of its own. nil, or a
	// func answering "" ⇒ no scratch root.
	Scratch func() string
	// Virtual reports the host's virtual mounts by prefix, colon included — read-only trees with no
	// host path (apogee's shipped skills are compiled into the binary, ADR 0065 §3), so no root
	// string could have named them. Consulted BEFORE the disk roots, which costs nothing: a mount
	// reference is a spelling no host path can take. A write NEVER reaches it — the spelling itself
	// is refused on the write side. nil ⇒ none.
	Virtual func() map[string]fs.FS
}
