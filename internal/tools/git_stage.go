package tools

import (
	"context"
	"strings"
)

// Git-aware file operations (2026-08-22) — the shared best-effort index update
//
// move_file and delete_file reproduce the index half of `git mv` / `git rm` automatically: when
// the workspace is a git worktree and the file the call names is TRACKED, the tool stages the
// rename (or the deletion) after the filesystem operation has already succeeded. A model —
// especially a small one — gets rename tracking for free, with no prompting and no second tool
// call; an untracked file or a workspace that is no repository at all leaves the tool's behaviour
// byte-identical to what it was before.
//
// Two rules make that safe to do unconditionally. Staging is BEST-EFFORT: the file operation has
// already happened by the time this runs, so nothing here may turn a completed operation into a
// failed call — every refusal, every absent git, every non-zero git exit returns a note (or
// nothing) rather than an error. And staging is DISCLOSED: a successful stage appends the
// caller's own wording to the tool result, so the model reads that the index changed rather than
// discovering it later through git_status.
//
// Undo interplay (ADR 0051, ratified 2026-08-22 as accept-and-document). /undo restores WORKTREE
// bytes from the per-exchange pre-image journal and deliberately does not touch the index. After
// undoing a staged rename or deletion the staged entry therefore remains — visible in
// git_status, harmless, and resolved by a later `git add -A` or `git restore --staged <path>`.
// Extending the journal to revert index operations is deliberately out of scope: the journal's
// contract is bytes, and an undo that silently rewrote the index would be the surprising half.

// stageGitPaths stages paths in the workspace's git index after a file operation has already
// succeeded, and returns the note the caller should append to its result text: successNote when
// the stage happened, " (git staging skipped: <reason>)" when git was reachable but the staging
// itself failed, and "" when there was nothing to stage.
//
// paths[0] is by contract the PRE-operation source path — the path whose trackedness decides
// whether anything is staged at all. The probe reads the INDEX (`git ls-files --error-unmatch`),
// so running it after the rename or the unlink still sees that old path; one probe therefore
// covers both "this is not a repository" and "the source was never tracked", which are the two
// cases that must leave the index alone. The remaining paths are the operation's other side (a
// rename's destination, an overwritten destination's replacement) and are staged with the same
// pathspec pair.
//
// Every pathspec carries the :(literal) magic, so a file whose name contains *, ? or [ is staged
// as the file it is rather than glob-interpreted. Paths are passed exactly as the tool received
// them: runGit runs with the workspace root as cwd and git resolves relative and
// absolute-inside-repo pathspecs from there, so a workspace that is a SUBDIRECTORY of the
// repository needs no special handling.
func stageGitPaths(ctx context.Context, root, successNote string, paths ...string) string {
	if len(paths) == 0 {
		return ""
	}

	// A missing git and the exec fence's refusal are both silent skips: staging is a courtesy on
	// top of an operation that already stands, and neither is something the model can act on.
	gitPath, _, ok := gitProgram(ctx, root)
	if !ok {
		return ""
	}

	probe, err := runGit(ctx, gitPath, root, gitTimeout, "ls-files", "--error-unmatch", "--", literalPathspec(paths[0]))
	if err != nil || probe.exitCode != 0 {
		return ""
	}

	args := make([]string, 0, 3+len(paths))
	args = append(args, "add", "-A", "--")
	for _, path := range paths {
		args = append(args, literalPathspec(path))
	}
	// A Go error here is a cancelled context or a confinement-unavailable demotion (the runGit
	// contract) rather than git's own verdict; it is still a stage that did not happen, and the
	// model is told the same way.
	add, err := runGit(ctx, gitPath, root, gitTimeout, args...)
	if err != nil {
		return stagingSkipped(err.Error())
	}
	if add.exitCode != 0 {
		return stagingSkipped(add.combinedOutput)
	}
	return successNote
}

// literalPathspec prefixes a path with git's :(literal) pathspec magic, which turns off both
// glob interpretation and any other magic the string might otherwise be read as.
func literalPathspec(path string) string { return ":(literal)" + path }

// stagingSkipped renders a failed stage as the note appended to an otherwise successful result.
// It keeps the FIRST line of what git said: git's own first line names the problem, and the tail
// is advice aimed at a human at a terminal.
func stagingSkipped(reason string) string {
	first := strings.TrimSpace(reason)
	if cut := strings.IndexAny(first, "\r\n"); cut >= 0 {
		first = strings.TrimSpace(first[:cut])
	}
	if first == "" {
		first = "git add failed"
	}
	return " (git staging skipped: " + first + ")"
}
