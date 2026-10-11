// Package auditsplit is the scope split of apogee's shipped `audit` recipe, in Go: it lists the
// workspace files an audit covers and cuts their source files into parts a child can read in one
// window. It replaces the recipe's former `split.sh`, which needed a POSIX `sh` and coreutils a
// stock Windows box does not have; the outputs are the script's, byte for byte, on every host.
//
// # Listing
//
// The scope is the workspace's tracked and untracked-but-not-ignored files when git can list
// them, and every regular file (less `.git` folders) when it cannot — git is a convenience, never
// a prerequisite (ADR 0042 decision 2). git runs through internal/gitexec's hardened runner, the
// one place apogee launches git, and the [gitexec.Host] the split is handed is the listing seam a
// test fills to force either path. Docs, dependencies, build output, fixtures and generated files
// never enter the scope. The scope input names workspace-relative files or folders, whitespace
// separated (`.`, `all` or `everything` for the whole workspace), and is never glob-expanded;
// entries are `/`-separated and sorted in byte order, and a name holding a newline is skipped
// because no one-a-line list can hold it.
//
// # Parts
//
// The scope's files split into source and test files by their names; the source files (or the
// whole scope, when it holds tests only) are cut into parts of at most the part line and file
// bounds. A scope that fits is one part, `all`. One that does not is split per top-level
// directory, an oversized directory re-split one level deeper, a still-oversized flat part
// chunked in its sorted order, and a part under three files merged into a neighbour of its
// directory when the merge stays in bounds. Line counts are newline counts — what `wc -l` gives.
//
// # The files, one line each
//
// plan.go is the split's plan step — the bounds and their environment knobs, the listing, the
// classification and the four split mechanics — computed in memory, with no file written.
package auditsplit
