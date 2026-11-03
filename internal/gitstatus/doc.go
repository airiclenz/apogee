// Package gitstatus reads the output of `git status --porcelain=v2 --branch -z` into a
// Snapshot: the branch headers (branch, detached HEAD, upstream, ahead/behind) and the change
// counts (Dirty, one per changed path, and the staged / unstaged / untracked tallies).
//
// It is pure: it parses bytes and never runs git, so it imports no exec package
// (internal/gitexec, internal/subprocess). Whoever runs git — the git_status tool through
// internal/tools' hardened gitRead, or any other reader through internal/gitexec — hands it
// the output, and each caller keeps its own spawn posture.
//
// It has two forms over one record fold, so their counts cannot drift apart:
//
//   - Parse reads a whole capture and also keeps the entry lists ("<code> <path>") the
//     git_status tool renders for the model.
//   - Counter is an io.Writer that folds NUL-terminated records as they stream in, split
//     anywhere across writes, and keeps no paths: it holds one partial record at most, so its
//     memory does not grow with the size of the tree and its counts are exact at any size.
//
// A record the fold does not recognise is skipped rather than failing the read — git may
// interleave a warning when stderr is captured alongside — so a status that is mostly
// readable beats none.
//
// # The files, one line each
//
// gitstatus.go is the whole package — Snapshot, the record fold, Parse and Counter.
package gitstatus
