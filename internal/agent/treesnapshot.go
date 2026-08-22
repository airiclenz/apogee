package agent

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// The tracked-file mutation floor: git-status snapshots taken around every subprocess
// tool call so the tool result can NAME the workspace files the command changed. It is
// a structural floor in the ADR 0006 class — always on, in every mode including Bypass,
// never a Mechanism, no gating and no config key — because a subprocess that silently
// clobbers a tracked file is exactly the failure the 2026-08-22 incident showed the
// model cannot be trusted to notice on its own. The floor observes and reports; it
// never blocks (that is confinement's job, ADR 0012).
//
// Robustness contract (binding): each git run carries a 2-second timeout and executes
// in the workspace root; on ANY git error or timeout the check is skipped silently for
// that call. The floor must never break or slow a tool call's success path beyond the
// two porcelain runs.

// treeSnapshotTimeout bounds each git invocation the floor makes, so a wedged or
// enormous repository can never stall a tool call behind its own bookkeeping.
const treeSnapshotTimeout = 2 * time.Second

// treeMutationWarningCap is the most paths one warning line names; the remainder is
// folded into an "… and N more" tail so a mass-write cannot balloon the result.
const treeMutationWarningCap = 10

// treeSnapshotter owns the floor's state for one Agent: the workspace root and the
// once-probed answer to "is this root a git work tree?". The probe runs at most once
// per Agent (sync.Once), on the first subprocess call rather than in the constructor,
// so construction — including every sub-agent spawn — never pays a git invocation.
type treeSnapshotter struct {
	root      string    // the workspace root snapshots run in; "" disables the floor
	probeOnce sync.Once // guards the one rev-parse probe per Agent
	isRepo    bool      // the cached probe answer; false until proven true
}

// newTreeSnapshotter builds the floor for one workspace root. An empty root yields a
// permanently inactive snapshotter — an Agent with no workspace has no tree to watch.
func newTreeSnapshotter(workspaceRoot string) *treeSnapshotter {
	return &treeSnapshotter{root: workspaceRoot}
}

// active reports whether the floor applies at all: a workspace root is set and it is a
// git work tree. The git probe runs once per Agent and its answer is cached — a repo
// created or deleted mid-session is picked up at the next Agent, not the next call.
// Nil-safe: a nil snapshotter is an inactive floor, never an error.
func (t *treeSnapshotter) active() bool {
	if t == nil || t.root == "" {
		return false
	}
	t.probeOnce.Do(func() {
		out, err := t.git("rev-parse", "--is-inside-work-tree")
		t.isRepo = err == nil && strings.TrimSpace(out) == "true"
	})
	return t.isRepo
}

// beforeCall takes the pre-call porcelain snapshot. ok=false means the floor is off
// for this call — not a repo, or git failed or timed out — and the caller must skip
// the post-call half too: without a trustworthy "before" there is nothing to diff.
func (t *treeSnapshotter) beforeCall() (snapshot string, ok bool) {
	if !t.active() {
		return "", false
	}
	out, err := t.git("status", "--porcelain")
	if err != nil {
		return "", false
	}
	return out, true
}

// mutationWarning takes the post-call snapshot and renders the warning line, or ""
// when the tree is unchanged — or when the post-call git run failed, the silent-skip
// half of the robustness contract.
func (t *treeSnapshotter) mutationWarning(before string) string {
	after, err := t.git("status", "--porcelain")
	if err != nil || after == before {
		return ""
	}
	paths := porcelainDiffPaths(before, after)
	if len(paths) == 0 {
		return ""
	}
	return renderMutationWarning(paths)
}

// git runs one git command in the workspace root under the floor's timeout, returning
// stdout. Every error — git absent, not a repo, timeout — is the caller's signal to
// skip, never to fail the tool call.
func (t *treeSnapshotter) git(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), treeSnapshotTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = t.root
	out, err := cmd.Output()
	return string(out), err
}

// porcelainDiffPaths extracts the changed paths from two porcelain snapshots: every
// path whose status line appears in exactly one of them — a tracked file newly
// modified or deleted, an untracked file newly appeared (or removed), a file whose
// status merely deepened. Line-level symmetric difference is deliberately simple: the
// floor reports "this call touched these", not a semantic diff. Sorted and de-duped so
// the warning is deterministic.
func porcelainDiffPaths(before, after string) []string {
	beforeLines := porcelainLineSet(before)
	afterLines := porcelainLineSet(after)

	seen := make(map[string]struct{})
	var paths []string
	add := func(line string) {
		path := porcelainPath(line)
		if path == "" {
			return
		}
		if _, dup := seen[path]; dup {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	for line := range afterLines {
		if _, unchanged := beforeLines[line]; !unchanged {
			add(line)
		}
	}
	for line := range beforeLines {
		if _, unchanged := afterLines[line]; !unchanged {
			add(line)
		}
	}
	sort.Strings(paths)
	return paths
}

// porcelainLineSet splits one porcelain snapshot into its set of status lines.
func porcelainLineSet(snapshot string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, line := range strings.Split(snapshot, "\n") {
		if line != "" {
			set[line] = struct{}{}
		}
	}
	return set
}

// porcelainPath extracts the path from one porcelain v1 status line ("XY <path>",
// or "XY <from> -> <to>" for a rename, where the destination is the path that names
// the change). A malformed line yields "" and is dropped.
func porcelainPath(line string) string {
	if len(line) < 4 {
		return ""
	}
	path := line[3:]
	if i := strings.Index(path, " -> "); i >= 0 {
		path = path[i+len(" -> "):]
	}
	return path
}

// renderMutationWarning renders the one warning line appended to the tool result,
// listing at most treeMutationWarningCap paths with an "… and N more" tail beyond it.
func renderMutationWarning(paths []string) string {
	listed := paths
	var tail string
	if len(paths) > treeMutationWarningCap {
		listed = paths[:treeMutationWarningCap]
		tail = fmt.Sprintf(" … and %d more", len(paths)-treeMutationWarningCap)
	}
	return "[warning: this command changed workspace files: " + strings.Join(listed, ", ") + tail + "]"
}

// appendTreeMutationWarning appends a non-empty warning line to the result's content —
// success and error results alike, because a failed command may still have written
// before it failed (the incident's exact shape). A "" warning appends nothing.
func appendTreeMutationWarning(result *domain.ToolResult, warning string) {
	if warning == "" {
		return
	}
	if result.Content != "" && !strings.HasSuffix(result.Content, "\n") {
		result.Content += "\n"
	}
	result.Content += warning
}
