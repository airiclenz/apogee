package tui

import "context"

// GitActionsHost is the seam the footer's git actions run through: the user's own fetch, pull,
// push, sync, branch switch and rebase on the workspace (ADR 0108). Like [GitStatusHost], it keeps HOW git runs — the
// hardened runner, the no-prompt transport, the refused-repository rule — in the binary, which is
// why this package imports neither internal/gitexec nor internal/subprocess. cmd/apogee backs it;
// nil in [Options.GitActions] means no git actions (ADR 0054).
//
// Each call blocks until git lands, bounded only by a generous backstop, so it runs off the
// Update loop on a Cmd goroutine under the program context — Branches included. Each action
// returns a one-line result for the transcript note on success, or an error carrying git's own
// message. A refused repository runs no fetch, merge, push, checkout or rebase and comes back as
// an error; so does a networked call whose
// transport cannot run safely (an ssh command of the operator's own on Windows), which is never
// skipped in silence.
type GitActionsHost interface {
	// Fetch updates root's remote-tracking refs from its default remote.
	Fetch(ctx context.Context, root string) (string, error)
	// Pull fetches, then fast-forwards the branch to its upstream; a diverged branch is an error,
	// never a merge commit.
	Pull(ctx context.Context, root string) (string, error)
	// Push pushes the branch, never forced. A branch with no upstream is pushed to origin and
	// set to track it; without a remote named origin it is an error naming the remotes.
	Push(ctx context.Context, root string) (string, error)
	// Sync runs Pull, then Push, and stops at the first failure.
	Sync(ctx context.Context, root string) (string, error)
	// Branches lists root's local branch names by their bare names, the checked-out branch first
	// and the rest in name order; a detached HEAD puts none first.
	Branches(ctx context.Context, root string) ([]string, error)
	// Switch checks out the local branch name. A dirty tree, a name beginning with "-" and a
	// name with no local branch are each refused with an error, and nothing is checked out.
	Switch(ctx context.Context, root, name string) (string, error)
	// Rebase rebases the branch onto its upstream as last fetched; a dirty tree is refused. A
	// rebase that stops on a conflict is aborted, leaving the branch and tree as they were, and
	// comes back as an error naming the conflicting files.
	Rebase(ctx context.Context, root string) (string, error)
}
