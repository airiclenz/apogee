package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/gitexec"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/subprocess"
	"github.com/airiclenz/apogee/internal/tools"
)

// The tracked-file mutation floor: git-status snapshots taken around every subprocess
// tool call so the tool result can NAME the workspace files the command changed. It is
// a structural floor in the ADR 0006 class — always on, in every mode including Bypass,
// never a Reaction, no gating and no config key — because a subprocess that silently
// clobbers a tracked file is exactly the failure the 2026-08-22 incident showed the
// model cannot be trusted to notice on its own. The floor observes and reports; it
// never blocks (that is confinement's job, ADR 0012).
//
// A snapshot is a per-path content identity, not the porcelain text: every dirty or untracked
// path `git status --porcelain -uall -z` names, with its status code and an identity read off
// the file itself. Diffing whole porcelain lines missed a file that was already modified and got
// rewritten (its " M" line is the same before and after) and a new file inside an untracked
// directory (plain --porcelain collapses the directory to one unchanged "?? dir/" line).
//
// Robustness contract (binding): each git run carries a 2-second timeout and executes
// in the workspace root; on ANY git error or timeout the check is skipped silently for
// that call. The floor must never break or slow a tool call's success path beyond the
// two snapshots, each bounded by treeSnapshotTimeout: a status listing past
// subprocess.MaxSubprocessOutputBytes skips the check, and the content hashing holds a per-file
// cap and a whole-snapshot byte budget inside that same timeout, falling back past them to the
// file's Lstat identity (type, size, modification time) rather than reading on. Only regular
// files are read, through security.SafeOpen (non-blocking open, no escape from the repository);
// a symlink's identity is its target string, so a link to /dev/zero is never opened. Since
// 2026-08-26 the floor's git goes through the hardened git funnel (gitexec.Host, the runner
// tools.RunGitQuery wraps) rather than a bare exec, so its own git is fenced, hardened,
// environment-scrubbed and torn down exactly as a git TOOL's git is.

// treeSnapshotTimeout bounds each git invocation the floor makes, and each whole snapshot —
// its status run plus its content hashing — so a wedged or enormous repository can never stall
// a tool call behind its own bookkeeping.
const treeSnapshotTimeout = 2 * time.Second

// treeMutationWarningCap is the most paths one warning line names; the remainder is
// folded into an "… and N more" tail so a mass-write cannot balloon the result.
const treeMutationWarningCap = 10

// treeHashFileCap is the largest file the floor content-hashes; a bigger one is identified by
// its Lstat identity alone, so one huge dirty artefact cannot eat the snapshot's budget.
const treeHashFileCap = 2 << 20

// treeHashBudgetBytes is the most file content one snapshot hashes in total; the paths past it
// fall back to their Lstat identity, so a tree with many dirty files stays a bounded read.
const treeHashBudgetBytes = 32 << 20

// treeIdentityAbsent is the Lstat identity of a path git names that is not on disk (a deletion).
const treeIdentityAbsent = "absent"

// treeIdentityDirectory is the content identity of a directory git names whole (a nested
// repository); its entries are that repository's business, so only a type change registers.
const treeIdentityDirectory = "dir"

// errTreeStatusTooLarge is the capped status sink's refusal past its limit: the listing is
// incomplete, so the snapshot cannot be trusted and the check is skipped.
var errTreeStatusTooLarge = errors.New("apogee: tree snapshot: status listing exceeds the output cap")

// treeSnapshotter owns the floor's state for one Agent: the workspace root and the
// once-probed answer to "is this root a git work tree?". The probe runs at most once
// per Agent (sync.Once), on the first subprocess call rather than in the constructor,
// so construction — including every sub-agent spawn — never pays a git invocation.
type treeSnapshotter struct {
	root      string       // the workspace root snapshots run in; "" disables the floor
	host      gitexec.Host // the git runner every snapshot goes through; a test passes a fake
	probeOnce sync.Once    // guards the one rev-parse probe per Agent
	isRepo    bool         // the cached probe answer; false until proven true
	toplevel  string       // the repository's top directory, which porcelain paths are relative to
}

// treeSnapshot is one pre- or post-call picture of the work tree: every path the status listing
// names, keyed by its repository-relative path. A path absent from the map was clean.
type treeSnapshot map[string]pathIdentity

// pathIdentity is what one listed path looked like when the snapshot was taken.
type pathIdentity struct {
	status string // the porcelain XY status code
	stat   string // the Lstat identity: file type, size and modification time, or treeIdentityAbsent
	digest string // the content identity; "" when the file was not hashed (cap, budget, not regular)
}

// sameAs reports whether two identities of one path describe unchanged content: the same status,
// and the same digest when both sides hashed it, else the same Lstat identity. Falling back to
// the Lstat identity whenever either side lacks a digest keeps a path that crossed the budget
// line between the two snapshots from reading as changed.
func (p pathIdentity) sameAs(other pathIdentity) bool {
	if p.status != other.status {
		return false
	}
	if p.digest != "" && other.digest != "" {
		return p.digest == other.digest
	}
	return p.stat == other.stat
}

// newTreeSnapshotter builds the floor for one workspace root, running its git through host
// (gitexec.OS() in production). An empty root yields a permanently inactive snapshotter — an
// Agent with no workspace has no tree to watch.
func newTreeSnapshotter(workspaceRoot string, host gitexec.Host) *treeSnapshotter {
	return &treeSnapshotter{root: workspaceRoot, host: host}
}

// active reports whether the floor applies at all: a workspace root is set and it is a
// git work tree. The git probe runs once per Agent and its answer is cached — a repo
// created or deleted mid-session is picked up at the next Agent, not the next call. The same
// probe learns the repository's top directory, since status paths are relative to it rather than
// to a workspace root that sits below it.
// Nil-safe: a nil snapshotter is an inactive floor, never an error.
func (t *treeSnapshotter) active(ctx context.Context) bool {
	if t == nil || t.root == "" {
		return false
	}
	t.probeOnce.Do(func() {
		out, err := t.git(ctx, "rev-parse", "--is-inside-work-tree", "--show-toplevel")
		if err != nil {
			return
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) != 2 || strings.TrimSpace(lines[0]) != "true" {
			return
		}
		toplevel := strings.TrimSuffix(lines[1], "\r")
		if toplevel == "" {
			return
		}
		t.toplevel = filepath.FromSlash(toplevel)
		t.isRepo = true
	})
	return t.isRepo
}

// beforeCall takes the pre-call snapshot. ok=false means the floor is off for this call —
// not a repo, or git failed, timed out or overran the output cap — and the caller must skip
// the post-call half too: without a trustworthy "before" there is nothing to diff.
func (t *treeSnapshotter) beforeCall(ctx context.Context) (snapshot treeSnapshot, ok bool) {
	if !t.active(ctx) {
		return nil, false
	}
	return t.snapshot(ctx)
}

// mutationWarning takes the post-call snapshot and renders the warning line, or ""
// when no listed path changed — or when the post-call snapshot failed, the silent-skip
// half of the robustness contract.
func (t *treeSnapshotter) mutationWarning(ctx context.Context, before treeSnapshot) string {
	after, ok := t.snapshot(ctx)
	if !ok {
		return ""
	}
	paths := treeSnapshotDiffPaths(before, after)
	if len(paths) == 0 {
		return ""
	}
	return renderMutationWarning(paths)
}

// snapshot lists the dirty and untracked paths and identifies each, the whole of it inside one
// treeSnapshotTimeout. ok=false is any git failure or an overrun listing.
func (t *treeSnapshotter) snapshot(ctx context.Context) (treeSnapshot, bool) {
	deadline := time.Now().Add(treeSnapshotTimeout)
	listing, err := t.statusListing(ctx)
	if err != nil {
		return nil, false
	}
	budget := &treeHashBudget{ctx: ctx, deadline: deadline, remaining: treeHashBudgetBytes}
	snap := make(treeSnapshot)
	for _, entry := range parsePorcelainZ(listing) {
		stat, digest := t.identifyPath(entry.path, budget)
		snap[entry.path] = pathIdentity{status: entry.status, stat: stat, digest: digest}
	}
	return snap, true
}

// statusListing runs the NUL-separated status listing with every untracked file named
// individually (-uall), streamed into a sink that refuses output past
// subprocess.MaxSubprocessOutputBytes: a truncated listing would silently drop paths, so an
// overrun is one more failure that skips the check.
func (t *treeSnapshotter) statusListing(ctx context.Context) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, treeSnapshotTimeout)
	defer cancel()
	sink := &cappedSink{limit: subprocess.MaxSubprocessOutputBytes}
	err := t.host.RunTo(runCtx, t.root, nil, treeSnapshotTimeout, sink, "status", "--porcelain", "-uall", "-z")
	if sink.overran {
		return "", errTreeStatusTooLarge
	}
	if err != nil {
		return "", err
	}
	return sink.buf.String(), nil
}

// git runs one git command in the workspace root under the floor's timeout, returning
// stdout. Every error — git absent, a fenced or refused git, not a repo, timeout — is the
// caller's signal to skip, never to fail the tool call.
//
// It runs through the tools package's git funnel (tools.RunGitQuery), not a bare exec: the
// floor fires around EVERY subprocess tool call in EVERY mode, so its own git is the most
// frequently spawned program apogee runs and must carry the same hardening as a git tool's —
// the exec fence on the resolved binary, core.hooksPath=, GIT_CONFIG_NOSYSTEM, the allowlisted
// workspace-scoped environment (apogee's API key never reaches it), the repo-local
// command-config refusal (every repo-local key whose value is a program git executes) and the
// §2.4 process-tree teardown. The status listing takes the same Host's streaming RunTo
// (statusListing), whose guarantees are Run's.
//
// ctx is the call's, so a cancelled Turn skips the check as the contract allows; the per-run
// timeout stays the outer bound as well as the funnel's, since a ctx that is never cancelled
// must still not let one wedged git hold a tool result.
func (t *treeSnapshotter) git(ctx context.Context, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, treeSnapshotTimeout)
	defer cancel()
	return tools.RunGitQuery(runCtx, t.host, t.root, treeSnapshotTimeout, args...)
}

// identifyPath reads one listed path's Lstat identity and, where it can within budget, its
// content identity: a regular file's SHA-256, a symlink's target string, a directory's type.
// A path that is gone is treeIdentityAbsent with no digest; any other kind (a FIFO, a device)
// keeps its Lstat identity alone and is never opened.
func (t *treeSnapshotter) identifyPath(path string, budget *treeHashBudget) (stat, digest string) {
	full := filepath.Join(t.toplevel, filepath.FromSlash(path))
	info, err := os.Lstat(full)
	if err != nil {
		return treeIdentityAbsent, ""
	}
	stat = fmt.Sprintf("%v|%d|%d", info.Mode().Type(), info.Size(), info.ModTime().UnixNano())
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(full)
		if err != nil {
			return stat, ""
		}
		return stat, "link:" + target
	case info.IsDir():
		return stat, treeIdentityDirectory
	case info.Mode().IsRegular():
		return stat, t.hashRegularFile(full, info.Size(), budget)
	}
	return stat, ""
}

// hashRegularFile returns the SHA-256 of one regular file, or "" when it is over
// treeHashFileCap, the snapshot's budget or deadline is spent, or it cannot be read. It opens
// through security.SafeOpen — non-blocking, so a file swapped for a FIFO since the Lstat cannot
// wedge the call, and fenced to the repository — and reads at most the cap either way.
func (t *treeSnapshotter) hashRegularFile(full string, size int64, budget *treeHashBudget) string {
	if size > treeHashFileCap || !budget.take(size) {
		return ""
	}
	f, err := security.SafeOpen(t.toplevel, full)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	hash := sha256.New()
	read, err := io.Copy(hash, io.LimitReader(f, treeHashFileCap+1))
	if err != nil || read > treeHashFileCap {
		return ""
	}
	return string(hash.Sum(nil))
}

// treeHashBudget is one snapshot's allowance for content hashing: a byte budget and the
// snapshot's deadline. Both only ever shrink, so a spent budget stays spent for the snapshot.
type treeHashBudget struct {
	ctx       context.Context // the call's ctx; a cancelled Turn stops the hashing too
	deadline  time.Time       // the snapshot's treeSnapshotTimeout bound
	remaining int64           // file bytes still allowed to be hashed
}

// take reserves size bytes of the budget, or reports false — leaving the budget untouched —
// when the bytes, the deadline or the ctx has run out.
func (b *treeHashBudget) take(size int64) bool {
	if b.ctx.Err() != nil || !time.Now().Before(b.deadline) || size > b.remaining {
		return false
	}
	b.remaining -= size
	return true
}

// cappedSink collects a child's standard output up to limit bytes and refuses the write that
// would cross it, which stops the copy and fails the run; overran records that it happened, so
// the caller skips whatever the run's own error says.
type cappedSink struct {
	buf     bytes.Buffer
	limit   int
	overran bool
}

// Write appends p, or refuses it whole with errTreeStatusTooLarge when it would cross the limit.
func (c *cappedSink) Write(p []byte) (int, error) {
	if c.buf.Len()+len(p) > c.limit {
		c.overran = true
		return 0, errTreeStatusTooLarge
	}
	return c.buf.Write(p)
}

// porcelainEntry is one record of a `git status --porcelain -z` listing.
type porcelainEntry struct {
	status string // the XY status code
	path   string // the repository-relative path; a rename's or copy's destination
}

// parsePorcelainZ splits a NUL-separated porcelain v1 listing into its entries. A record is
// "XY <path>"; a rename or copy (R or C in either column) is followed by one more field, its
// source path, which is consumed and dropped — the destination is the path that names the
// change. A malformed record is dropped.
func parsePorcelainZ(listing string) []porcelainEntry {
	fields := strings.Split(listing, "\x00")
	var entries []porcelainEntry
	for i := 0; i < len(fields); i++ {
		record := fields[i]
		if len(record) < 4 || record[2] != ' ' {
			continue
		}
		status := record[:2]
		if strings.ContainsAny(status, "RC") {
			i++
		}
		entries = append(entries, porcelainEntry{status: status, path: record[3:]})
	}
	return entries
}

// treeSnapshotDiffPaths names every path whose identity differs between two snapshots: a path
// listed in only one of them (newly dirty, newly untracked, cleaned or removed) and a path listed
// in both whose status or content changed. The floor reports "this call touched these", not a
// semantic diff. Sorted so the warning is deterministic.
func treeSnapshotDiffPaths(before, after treeSnapshot) []string {
	var paths []string
	for path, now := range after {
		if was, listed := before[path]; !listed || !was.sameAs(now) {
			paths = append(paths, path)
		}
	}
	for path := range before {
		if _, listed := after[path]; !listed {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
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
