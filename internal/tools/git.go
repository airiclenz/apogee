package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The git tools (P3.9) — branch / commit / diff-range / status / log over the system git
// ----------------------------------------------------------------------------
//
// Five one-shot tools shell out to the system `git` (§3a — a convenience dep,
// detected on PATH and degrading gracefully when absent, never a hard
// dependency). They are SubprocessTools (domain.SubprocessTool): the dispatch
// disposition runs all of them under Confiner.Confine in Auto and gates them when
// fs-confinement is unavailable ("confine if you can, gate if you can't").
// git_diff_range, git_status and git_log also declare ReadOnly(), but the unfakeable
// subprocess marker outranks that self-declaration
// (confinement-execution-contract §4, amended 2026-07-26) — and since 2026-08-02
// the Plan tool menu keys on the same class the ladder does, so none of them is offered
// nor run in Plan. All of them are
// stateless across Turns (ADR 0008 — a fresh git
// process per call), path-scope their inputs to the workspace root, and run with
// a scrubbed, allowlisted environment so a stray inherited variable cannot change
// git's behaviour.

// gitTimeout bounds a single git invocation. git operations are local (no network
// op is exposed by these tools), so a short ceiling is ample and a hung git never
// wedges a Turn (the §2.4 teardown reaps the process group when it fires).
const gitTimeout = 15 * time.Second

// gitDiffTimeout bounds a diff-range, which can be larger; it matches the oracle's
// separate diff ceiling.
const gitDiffTimeout = 10 * time.Second

// safeEnvKeys is the allowlist of environment variables a git subprocess inherits
// (ported from the TS oracle's SAFE_ENV_KEYS). Everything else is dropped, so a
// surprising inherited variable cannot redirect git (config, auth, pager) — the
// process sees only the keys a normal git invocation needs.
//
// The list is POSIX-shaped, which is a policy choice, not a portability one: the
// variables a Windows process cannot start without (%SystemRoot%, %ComSpec%,
// %PATHEXT%, the profile paths git reads its user config from) are the platform's
// floor and are appended by platform.Shell.ScopeEnv rather than restated here.
var safeEnvKeys = []string{
	"PATH", "HOME", "USER", "SHELL", "LOGNAME", "HOSTNAME", "PWD",
	"LANG", "LC_ALL", "LC_CTYPE", "LC_MESSAGES", "LC_COLLATE",
	"TERM", "TERM_PROGRAM", "COLORTERM",
	"TMPDIR", "TMP", "TEMP",
	"XDG_RUNTIME_DIR", "XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME",
	"EDITOR", "VISUAL", "PAGER",
	"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL",
	"CI", "GITHUB_ACTIONS", "GITLAB_CI", "JENKINS_URL",
}

// safeGitEnv returns the allowlisted environment for a git subprocess: each
// safeEnvKeys entry that is present in the host environment, in "KEY=value" form,
// followed by the platform's own essentials (none on POSIX — the output there is
// exactly the allowlist, as before). It is a package var so a test can substitute
// the lookup.
var safeGitEnv = func() []string {
	return shellHost.ScopeEnv(safeEnvKeys, os.LookupEnv)
}

// lookGit resolves the system git on PATH (a package var so a test can inject a
// fake resolver). It returns the absolute path and ok=false when git is absent —
// the signal a tool degrades to a graceful "git not available" result (§3a).
var lookGit = func() (string, bool) {
	path, err := exec.LookPath("git")
	return path, err == nil
}

// runGit runs git with gitArgs in root under the per-call timeout and the scrubbed
// environment, honouring the confinement handle the disposition installed (if any).
// It returns the captured outcome; a missing git is signalled by ok=false on the
// caller's lookGit, not here. The Go error is non-nil only for ctx cancellation or
// a confinement-unavailable demotion (the runSubprocess contract).
func runGit(ctx context.Context, gitPath, root string, timeout time.Duration, gitArgs ...string) (subprocessResult, error) {
	spec := subprocessSpec{
		argv:    append([]string{gitPath}, gitArgs...),
		dir:     root,
		timeout: timeout,
		env:     safeGitEnv(),
	}
	return runSubprocess(ctx, spec)
}

// gitResultText renders a captured git outcome as text the model reads: the
// combined output trimmed, or the fallback when git printed nothing on success.
func gitResultText(res subprocessResult, successFallback string) string {
	out := strings.TrimSpace(res.combinedOutput)
	if res.exitCode == 0 && out == "" {
		return successFallback
	}
	return out
}

// ----------------------------------------------------------------------------
// git_branch — create / switch / list / delete
// ----------------------------------------------------------------------------

var gitBranchSpec = toolSpec{
	name:        "git_branch",
	description: "Manage git branches: create, switch, list, or delete. Uses safe delete (-d) which refuses to delete unmerged branches. Deletion of main/master/develop is blocked.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["action"],
  "properties": {
    "action": {"type": "string", "enum": ["create", "switch", "list", "delete"], "description": "The branch operation to perform"},
    "name": {"type": "string", "description": "Branch name (required for create, switch, delete)"},
    "start_point": {"type": "string", "description": "Starting point for create (commit, tag, or branch). Default: HEAD"}
  }
}`),
}

type gitBranchArgs struct {
	Action     string `json:"action"`
	Name       string `json:"name"`
	StartPoint string `json:"start_point"`
}

// protectedBranches are the long-lived branches git_branch refuses to delete — a
// footgun-guard, never a hard wipe of a mainline branch (parity with the oracle).
var protectedBranches = map[string]bool{
	"main": true, "master": true, "develop": true, "development": true,
}

// GitBranch manages git branches (create, switch, list, delete) over the system
// git, scoped to a workspace root. Deletion uses the safe `-d` (which refuses an
// unmerged branch) and is blocked outright for the protected mainline branches. It
// is a SubprocessTool the disposition confines in Auto.
type GitBranch struct {
	toolSpec
	root string
}

// NewGitBranch returns a git-branch tool operating in root.
func NewGitBranch(root string) *GitBranch { return &GitBranch{toolSpec: gitBranchSpec, root: root} }

// ReadOnly reports that git_branch is write-capable (false): create/switch/delete
// mutate the repository.
func (t *GitBranch) ReadOnly() bool { return false }

// Subprocess reports that git_branch launches an OS subprocess (the system git) —
// the marker the disposition confines in Auto (domain.SubprocessTool).
func (t *GitBranch) Subprocess() bool { return true }

// Execute performs the branch operation through the system git. A missing git, an
// invalid action, a protected-branch deletion, or a git failure are surfaced as
// results; only ctx cancellation or a confinement-unavailable demotion is a Go
// error.
func (t *GitBranch) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[gitBranchArgs](call)
	if !ok {
		return fail, nil
	}

	gitArgs, errMsg := buildBranchArgs(args)
	if errMsg != "" {
		return errorResult(call.ID, errMsg), nil
	}

	gitPath, ok := lookGit()
	if !ok {
		return errorResult(call.ID, gitUnavailableMessage), nil
	}

	res, err := runGit(ctx, gitPath, t.root, gitTimeout, gitArgs...)
	if err != nil {
		return domain.ToolResult{}, err
	}
	if res.exitCode != 0 {
		return errorResult(call.ID, gitResultText(res, "git branch failed")), nil
	}
	return okResult(call.ID, gitResultText(res, branchSuccessMessage(args))), nil
}

// looksLikeOption reports whether a model-supplied ref/branch argument begins with "-", which
// git would interpret as an option flag rather than a ref/branch name (e.g. a name "-D" or a
// start-point "--upload-pack=…"). Such arguments are rejected up front: the git tools pass argv
// arrays (no shell), so this is the remaining argument-injection class to close. A legitimate
// ref/branch never starts with "-".
func looksLikeOption(arg string) bool {
	return strings.HasPrefix(strings.TrimSpace(arg), "-")
}

// buildBranchArgs validates the branch arguments and returns the git argv (without
// the program), or a non-empty error message describing why the call is rejected.
func buildBranchArgs(args gitBranchArgs) (gitArgs []string, errMsg string) {
	action := args.Action
	if action != "create" && action != "switch" && action != "list" && action != "delete" {
		return nil, "action must be one of: create, switch, list, delete"
	}
	if action != "list" && strings.TrimSpace(args.Name) == "" {
		return nil, "name is required for create, switch, and delete"
	}
	// Reject a name / start-point that git would read as an option flag (leading "-"), so a
	// model-supplied argument cannot smuggle an option past the subcommand (SEC-06).
	if action != "list" && looksLikeOption(args.Name) {
		return nil, "branch name may not begin with '-'"
	}
	if action == "create" && args.StartPoint != "" && looksLikeOption(args.StartPoint) {
		return nil, "start_point may not begin with '-'"
	}

	// Terminate the ref position with "--" on the two checkout forms. looksLikeOption closes
	// the option-flag class; this closes the ref-vs-pathspec one: `git checkout <name>` with a
	// name that is not a ref but IS a tracked path is a pathspec checkout, which silently
	// restores those files from the index and destroys uncommitted work while reporting
	// success. A model asking to switch to a branch that does not exist but shares a name with
	// a directory ("docs", "tests") is a routine mistake, and the human approving "switch
	// branch" is not approving a working-tree revert. With "--" the same call fails loudly
	// ("fatal: invalid reference: docs") and the edit survives.
	switch action {
	case "create":
		out := []string{"checkout", "-b", args.Name}
		if args.StartPoint != "" {
			out = append(out, args.StartPoint)
		}
		return append(out, "--"), ""
	case "switch":
		return []string{"checkout", args.Name, "--"}, ""
	case "list":
		return []string{"branch", "-a", "--format=%(refname:short) %(HEAD)"}, ""
	case "delete":
		if protectedBranches[strings.ToLower(args.Name)] {
			return nil, "cannot delete protected branch '" + args.Name + "'"
		}
		return []string{"branch", "-d", args.Name}, ""
	default:
		return nil, "action must be one of: create, switch, list, delete"
	}
}

// branchSuccessMessage is the fallback text when git prints nothing on a successful
// branch operation, so the model gets a clear confirmation.
func branchSuccessMessage(args gitBranchArgs) string {
	switch args.Action {
	case "create":
		return "Created and switched to branch '" + args.Name + "'"
	case "switch":
		return "Switched to branch '" + args.Name + "'"
	case "list":
		return "No branches found"
	case "delete":
		return "Deleted branch '" + args.Name + "'"
	default:
		return ""
	}
}

// ----------------------------------------------------------------------------
// git_commit — stage and commit
// ----------------------------------------------------------------------------

var gitCommitSpec = toolSpec{
	name:        "git_commit",
	description: "Stage files and create a git commit. If files are specified they are staged first; otherwise commits whatever is currently staged. Amend is blocked on published commits to prevent divergent history.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["message"],
  "properties": {
    "message": {"type": "string", "description": "Commit message"},
    "files": {"type": "array", "items": {"type": "string"}, "description": "Files to stage before committing. If omitted, commits whatever is currently staged."},
    "amend": {"type": "boolean", "description": "Amend the previous commit (blocked on published commits)"},
    "allow_empty": {"type": "boolean", "description": "Allow creating an empty commit"}
  }
}`),
}

type gitCommitArgs struct {
	Message    string   `json:"message"`
	Files      []string `json:"files"`
	Amend      bool     `json:"amend"`
	AllowEmpty bool     `json:"allow_empty"`
}

// GitCommit stages files (if given) and creates a commit over the system git,
// scoped to a workspace root. Amend is blocked on a commit already pushed to a
// remote, to prevent divergent published history. It is a SubprocessTool the
// disposition confines in Auto.
type GitCommit struct {
	toolSpec
	root string
}

// NewGitCommit returns a git-commit tool operating in root.
func NewGitCommit(root string) *GitCommit { return &GitCommit{toolSpec: gitCommitSpec, root: root} }

// ReadOnly reports that git_commit is write-capable (false): it mutates the
// repository's index and history.
func (t *GitCommit) ReadOnly() bool { return false }

// Subprocess reports that git_commit launches an OS subprocess (the system git) —
// the marker the disposition confines in Auto (domain.SubprocessTool).
func (t *GitCommit) Subprocess() bool { return true }

// Execute stages the named files (path-safe) and commits with the message,
// honouring the confinement handle the disposition installed. A missing git, an
// empty message, an amend of a published commit, a path escape, or a git failure
// are surfaced as results; only ctx cancellation or a confinement-unavailable
// demotion is a Go error.
func (t *GitCommit) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[gitCommitArgs](call)
	if !ok {
		return fail, nil
	}
	message := strings.TrimSpace(args.Message)
	if message == "" {
		return errorResult(call.ID, "message is required and must be a non-empty string"), nil
	}

	gitPath, ok := lookGit()
	if !ok {
		return errorResult(call.ID, gitUnavailableMessage), nil
	}

	// Amend is refused on a commit already published to a remote (origin/…), so the
	// tool never rewrites history a remote has seen.
	if args.Amend {
		refs, err := runGit(ctx, gitPath, t.root, gitTimeout, "log", "-1", "--format=%D", "HEAD")
		if err != nil {
			return domain.ToolResult{}, err
		}
		if refs.exitCode == 0 && commitIsPublished(refs.combinedOutput) {
			return errorResult(call.ID, "cannot amend a commit that has been pushed to a remote; create a new commit instead"), nil
		}
	}

	// Stage the named files first (path-safe), so a commit only ever touches paths
	// inside the workspace.
	if len(args.Files) > 0 {
		resolved := make([]string, 0, len(args.Files))
		for _, f := range args.Files {
			abs, err := resolveInRoot(f, t.root)
			if err != nil {
				return errorResult(call.ID, err.Error()), nil
			}
			resolved = append(resolved, abs)
		}
		addArgs := append([]string{"add", "--"}, resolved...)
		res, err := runGit(ctx, gitPath, t.root, gitTimeout, addArgs...)
		if err != nil {
			return domain.ToolResult{}, err
		}
		if res.exitCode != 0 {
			return errorResult(call.ID, gitResultText(res, "git add failed")), nil
		}
	}

	commitArgs := []string{"commit", "-m", message}
	if args.Amend {
		commitArgs = append(commitArgs, "--amend")
	}
	if args.AllowEmpty {
		commitArgs = append(commitArgs, "--allow-empty")
	}
	res, err := runGit(ctx, gitPath, t.root, gitTimeout, commitArgs...)
	if err != nil {
		return domain.ToolResult{}, err
	}
	if res.exitCode != 0 {
		return errorResult(call.ID, gitResultText(res, "git commit failed")), nil
	}

	// Report the new commit's one-line summary (best-effort; the commit already
	// succeeded, so a failed summary is not surfaced as the call's error).
	summary, err := runGit(ctx, gitPath, t.root, gitTimeout, "log", "-1", "--oneline")
	if err != nil {
		return domain.ToolResult{}, err
	}
	if summary.exitCode == 0 {
		if text := strings.TrimSpace(summary.combinedOutput); text != "" {
			return okResult(call.ID, text), nil
		}
	}
	return okResult(call.ID, gitResultText(res, "commit created")), nil
}

// commitIsPublished reports whether the decoration refs of a commit (the output of
// `git log -1 --format=%D`) include a remote-tracking ref (origin/…), i.e. the
// commit has been pushed.
func commitIsPublished(refDecoration string) bool {
	for _, ref := range strings.Split(refDecoration, ",") {
		if strings.HasPrefix(strings.TrimSpace(ref), "origin/") {
			return true
		}
	}
	return false
}

// ----------------------------------------------------------------------------
// git_diff_range — diff between two refs
// ----------------------------------------------------------------------------

var gitDiffRangeSpec = toolSpec{
	name:        "git_diff_range",
	description: "Show the diff between two git refs (commits, branches, or tags). Uses three-dot diff to show what changed on the head ref since it diverged from the base ref.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["base", "head"],
  "properties": {
    "base": {"type": "string", "description": "Base ref (commit SHA, branch name, or tag)"},
    "head": {"type": "string", "description": "Head ref (commit SHA, branch name, or tag)"},
    "paths": {"type": "array", "items": {"type": "string"}, "description": "Restrict diff to specific file paths"},
    "stat": {"type": "boolean", "description": "Show diffstat summary instead of full diff (default: false)"},
    "name_only": {"type": "boolean", "description": "Show only names of changed files (default: false)"}
  }
}`),
}

type gitDiffRangeArgs struct {
	Base     string   `json:"base"`
	Head     string   `json:"head"`
	Paths    []string `json:"paths"`
	Stat     bool     `json:"stat"`
	NameOnly bool     `json:"name_only"`
}

// validRef is the conservative character class a git ref may use, rejecting an
// argument that could smuggle an option or a shell metacharacter into the diff
// (parity with the oracle's VALID_REF).
var validRef = regexp.MustCompile(`^[a-zA-Z0-9._\-/~^@{}]+$`)

// GitDiffRange shows the three-dot diff between two refs (what changed on head
// since it diverged from base) over the system git, scoped to a workspace root. It
// is read-only — it never mutates the repository.
type GitDiffRange struct {
	toolSpec
	root string
}

// NewGitDiffRange returns a git-diff-range tool operating in root.
func NewGitDiffRange(root string) *GitDiffRange {
	return &GitDiffRange{toolSpec: gitDiffRangeSpec, root: root}
}

// ReadOnly reports that git_diff_range performs no writes (a diff is harmless
// inspection) — an honest statement about the tool, read by self-regulation's
// read/write tally. It is NOT what classifies the call, and (since 2026-08-02) NOT
// what the Plan menu filters on: the Subprocess marker below outranks it in both.
func (t *GitDiffRange) ReadOnly() bool { return true }

// Subprocess reports that git_diff_range launches an OS subprocess (the system
// git). The unfakeable marker OUTRANKS the read-only self-declaration in the
// per-call classification (confinement-execution-contract §4, amended 2026-07-26),
// so the call takes the subprocess row: confined in Auto, gated below it.
func (t *GitDiffRange) Subprocess() bool { return true }

// Execute runs the three-dot diff between the validated refs through the system
// git. A missing git, an invalid/missing ref, a path escape, or a git failure are
// surfaced as results; the Go error is reserved for ctx cancellation and a
// confinement-unavailable demotion (the runSubprocess contract).
func (t *GitDiffRange) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[gitDiffRangeArgs](call)
	if !ok {
		return fail, nil
	}
	if strings.TrimSpace(args.Base) == "" {
		return errorResult(call.ID, "base ref is required"), nil
	}
	if strings.TrimSpace(args.Head) == "" {
		return errorResult(call.ID, "head ref is required"), nil
	}
	// validRef's character class permits "-" (it is a legal git ref char), so a ref could
	// otherwise begin with "-" and be read as an option even after the diff-range "..." join.
	// Reject a leading-"-" ref explicitly (SEC-06) before the class check.
	if !validRef.MatchString(args.Base) || looksLikeOption(args.Base) {
		return errorResult(call.ID, "invalid base ref: "+args.Base), nil
	}
	if !validRef.MatchString(args.Head) || looksLikeOption(args.Head) {
		return errorResult(call.ID, "invalid head ref: "+args.Head), nil
	}

	gitArgs := []string{"diff", args.Base + "..." + args.Head}
	if args.Stat {
		gitArgs = append(gitArgs, "--stat")
	}
	if args.NameOnly {
		gitArgs = append(gitArgs, "--name-only")
	}
	if len(args.Paths) > 0 {
		// Path-scope each restriction to the workspace, so the diff cannot be
		// pointed outside the root.
		paths := make([]string, 0, len(args.Paths))
		for _, p := range args.Paths {
			abs, err := resolveInRoot(p, t.root)
			if err != nil {
				return errorResult(call.ID, err.Error()), nil
			}
			paths = append(paths, abs)
		}
		gitArgs = append(gitArgs, "--")
		gitArgs = append(gitArgs, paths...)
	}

	gitPath, ok := lookGit()
	if !ok {
		return errorResult(call.ID, gitUnavailableMessage), nil
	}

	res, err := runGit(ctx, gitPath, t.root, gitDiffTimeout, gitArgs...)
	if err != nil {
		return domain.ToolResult{}, err
	}
	if res.exitCode != 0 {
		return errorResult(call.ID, gitResultText(res, "git diff failed")), nil
	}
	return okResult(call.ID, gitResultText(res, "No differences found")), nil
}

// ----------------------------------------------------------------------------
// git_status — branch, upstream divergence, and the working-tree lists
// ----------------------------------------------------------------------------

var gitStatusSpec = toolSpec{
	name:        "git_status",
	description: "Show the git working-tree status: the current branch, how far it is ahead of and behind its upstream, and the staged, unstaged, and untracked files. Read-only — it changes nothing. Long file lists are capped and the result says how many entries were left out.",
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {}
}`),
}

// maxGitStatusPaths bounds EACH of the three path lists git_status reports. A repository
// mid-refactor can hold thousands of changed paths, and a status the model cannot read is
// worse than a truncated one — the section header states the real total and the tail states
// how many were withheld, so nothing is silently lost.
const maxGitStatusPaths = 50

// gitStatusReport is the parsed shape of `git status --porcelain=v2 --branch -z`: the branch
// headers plus the three path lists, each entry already rendered as "<code> <path>".
type gitStatusReport struct {
	branch   string // the current branch; empty when the header is absent (e.g. detached)
	detached bool   // HEAD is not on a branch
	upstream string // the upstream ref, empty when the branch tracks nothing
	hasAB    bool   // an ahead/behind header was present (only ever with an upstream)
	ahead    int
	behind   int

	staged    []string
	unstaged  []string
	untracked []string
}

// GitStatus reports the working-tree status of the repository at a workspace root over the
// system git. It is read-only — it inspects and never mutates — but like git_diff_range it is
// a SubprocessTool, and that marker is what classifies the call.
type GitStatus struct {
	toolSpec
	root string
}

// NewGitStatus returns a git-status tool operating in root.
func NewGitStatus(root string) *GitStatus { return &GitStatus{toolSpec: gitStatusSpec, root: root} }

// ReadOnly reports that git_status performs no writes (reading the index and working tree
// changes nothing) — an honest statement about the tool, read by self-regulation's read/write
// tally. As with git_diff_range it is NOT what classifies the call: the Subprocess marker
// below outranks it in both the disposition and the Plan menu.
func (t *GitStatus) ReadOnly() bool { return true }

// Subprocess reports that git_status launches an OS subprocess (the system git) — the
// unfakeable marker that OUTRANKS the read-only declaration above
// (confinement-execution-contract §4, amended 2026-07-26), so the call takes the subprocess
// row: confined in Auto, gated below it.
func (t *GitStatus) Subprocess() bool { return true }

// Execute runs `git status` in porcelain v2 form and renders it for the model. A missing git
// or a git failure (not a repository, most often) is surfaced as a result; only ctx
// cancellation or a confinement-unavailable demotion is a Go error.
func (t *GitStatus) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	gitPath, ok := lookGit()
	if !ok {
		return errorResult(call.ID, gitUnavailableMessage), nil
	}

	// Porcelain v2 is the stable machine format (it carries the branch and ahead/behind
	// headers v1 lacks), and -z makes every record NUL-terminated so a path containing a
	// space, a quote, or a newline arrives verbatim instead of C-quoted.
	res, err := runGit(ctx, gitPath, t.root, gitTimeout, "status", "--porcelain=v2", "--branch", "-z")
	if err != nil {
		return domain.ToolResult{}, err
	}
	if res.exitCode != 0 {
		return errorResult(call.ID, gitResultText(res, "git status failed")), nil
	}
	return okResult(call.ID, renderGitStatus(parseGitStatus(res.combinedOutput))), nil
}

// parseGitStatus reads the NUL-terminated porcelain v2 records into a report. A record it does
// not recognise is skipped rather than failing the call: git may prepend a warning on stderr
// (the capture is combined), and a status the model can mostly read beats an error it cannot.
func parseGitStatus(out string) gitStatusReport {
	var rep gitStatusReport
	records := strings.Split(out, "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		switch {
		case strings.HasPrefix(record, "# branch.head "):
			head := strings.TrimSpace(strings.TrimPrefix(record, "# branch.head "))
			if head == "(detached)" {
				rep.detached = true
			} else {
				rep.branch = head
			}
		case strings.HasPrefix(record, "# branch.upstream "):
			rep.upstream = strings.TrimSpace(strings.TrimPrefix(record, "# branch.upstream "))
		case strings.HasPrefix(record, "# branch.ab "):
			rep.ahead, rep.behind, rep.hasAB = parseAheadBehind(strings.TrimPrefix(record, "# branch.ab "))
		case strings.HasPrefix(record, "1 "):
			// Ordinary change: "1 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <path>".
			if fields, ok := porcelainFields(record, 9); ok {
				rep.addChange(fields[1], fields[8], "")
			}
		case strings.HasPrefix(record, "2 "):
			// Rename/copy: "2 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <Xscore> <path>", with the
			// ORIGINAL path in the next record — the one place a record spans two fields.
			if fields, ok := porcelainFields(record, 10); ok {
				origin := ""
				if i+1 < len(records) {
					origin = records[i+1]
					i++
				}
				rep.addChange(fields[1], fields[9], origin)
			}
		case strings.HasPrefix(record, "u "):
			// Unmerged: "u <XY> <sub> <m1> <m2> <m3> <mW> <h1> <h2> <h3> <path>". A conflicted
			// path is reported once, under unstaged — it is work the tree still owes, not a
			// change already staged.
			if fields, ok := porcelainFields(record, 11); ok {
				rep.unstaged = append(rep.unstaged, "U  "+fields[10]+" (unmerged)")
			}
		case strings.HasPrefix(record, "? "):
			rep.untracked = append(rep.untracked, strings.TrimPrefix(record, "? "))
		}
	}
	return rep
}

// addChange files one ordinary or renamed entry under the lists its XY code selects: X (index
// vs HEAD) puts it in staged, Y (working tree vs index) in unstaged, and a path that is both —
// staged then edited again — is listed in both, which is exactly what git reports.
func (r *gitStatusReport) addChange(xy, path, origin string) {
	if len(xy) != 2 {
		return
	}
	suffix := ""
	if origin != "" {
		suffix = " (from " + origin + ")"
	}
	if xy[0] != '.' {
		r.staged = append(r.staged, string(xy[0])+"  "+path+suffix)
	}
	if xy[1] != '.' {
		r.unstaged = append(r.unstaged, string(xy[1])+"  "+path+suffix)
	}
}

// porcelainFields splits a porcelain v2 record into n space-separated fields, the last of which
// is the trailing path (which may itself contain spaces, so the split is bounded). It reports
// false for a record with too few fields — a truncated or unexpected line the caller skips.
func porcelainFields(record string, n int) ([]string, bool) {
	fields := strings.SplitN(record, " ", n)
	if len(fields) < n {
		return nil, false
	}
	return fields, true
}

// parseAheadBehind reads the "+<ahead> -<behind>" pair of the branch.ab header. It reports
// false when the header is malformed, in which case the divergence is simply not stated.
func parseAheadBehind(header string) (ahead, behind int, ok bool) {
	fields := strings.Fields(header)
	if len(fields) != 2 || !strings.HasPrefix(fields[0], "+") || !strings.HasPrefix(fields[1], "-") {
		return 0, 0, false
	}
	ahead, err := strconv.Atoi(fields[0][1:])
	if err != nil {
		return 0, 0, false
	}
	behind, err = strconv.Atoi(fields[1][1:])
	if err != nil {
		return 0, 0, false
	}
	return ahead, behind, true
}

// renderGitStatus writes the report as the text the model reads: a branch line, an upstream
// line when the branch tracks one, then the three capped sections — or the clean-tree line
// when there is nothing to list.
func renderGitStatus(rep gitStatusReport) string {
	var b strings.Builder
	switch {
	case rep.detached:
		b.WriteString("HEAD detached (not on a branch)")
	case rep.branch != "":
		b.WriteString("On branch " + rep.branch)
	default:
		b.WriteString("On branch (unknown)")
	}
	if rep.upstream != "" {
		b.WriteString("\nUpstream " + rep.upstream)
		if rep.hasAB {
			fmt.Fprintf(&b, ": ahead %d, behind %d", rep.ahead, rep.behind)
		}
	}

	if len(rep.staged) == 0 && len(rep.unstaged) == 0 && len(rep.untracked) == 0 {
		b.WriteString("\n\nWorking tree clean")
		return b.String()
	}
	writeStatusSection(&b, "Staged", rep.staged)
	writeStatusSection(&b, "Unstaged", rep.unstaged)
	writeStatusSection(&b, "Untracked", rep.untracked)
	return b.String()
}

// writeStatusSection writes one titled, capped list. The header carries the FULL count, so a
// truncated section never misreports how much changed.
func writeStatusSection(b *strings.Builder, title string, entries []string) {
	if len(entries) == 0 {
		return
	}
	fmt.Fprintf(b, "\n\n%s (%d):", title, len(entries))
	shown := entries
	if len(shown) > maxGitStatusPaths {
		shown = shown[:maxGitStatusPaths]
	}
	for _, entry := range shown {
		b.WriteString("\n  " + entry)
	}
	if len(entries) > len(shown) {
		fmt.Fprintf(b, "\n  [...%d more]", len(entries)-len(shown))
	}
}

// ----------------------------------------------------------------------------
// git_log — recent commits on a ref
// ----------------------------------------------------------------------------

var gitLogSpec = toolSpec{
	name:        "git_log",
	description: "Show the recent commit history of a git ref (branch, tag, or commit) as one line per commit: short hash, ISO date, and subject. Read-only — it changes nothing. Defaults to HEAD and to the 20 most recent commits.",
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "ref": {"type": "string", "description": "Ref to log (branch name, tag, or commit SHA). Defaults to HEAD."},
    "max_count": {"type": "integer", "description": "How many commits to show, most recent first (default 20, clamped to 100)"}
  }
}`),
}

type gitLogArgs struct {
	Ref      string `json:"ref"`
	MaxCount int    `json:"max_count"`
}

// defaultGitLogCount and maxGitLogCount bound how much history one git_log call returns. A
// repository's full log would flood a small model's context with commits it did not ask for,
// so the default is a screenful and the ceiling is firm: a model wanting more pages by moving
// its ref, which is how git history is read anyway.
const (
	defaultGitLogCount = 20
	maxGitLogCount     = 100
)

// gitLogDateFormat is the --date argument. iso-strict (rather than plain iso) renders the
// timestamp as a single space-free ISO 8601 token, so every log line splits cleanly into its
// three fields — hash, date, subject — for a model reading the output positionally.
const gitLogDateFormat = "iso-strict"

// GitLog reports the recent commits of a ref in the repository at a workspace root over the
// system git. Like git_diff_range and git_status it is read-only — it inspects history and
// never mutates — and like them it is a SubprocessTool, which is the marker that classifies
// the call.
type GitLog struct {
	toolSpec
	root string
}

// NewGitLog returns a git-log tool operating in root.
func NewGitLog(root string) *GitLog { return &GitLog{toolSpec: gitLogSpec, root: root} }

// ReadOnly reports that git_log performs no writes (reading history changes nothing) — an
// honest statement about the tool, read by self-regulation's read/write tally. As with
// git_diff_range and git_status it is NOT what classifies the call: the Subprocess marker
// below outranks it in both the disposition and the Plan menu.
func (t *GitLog) ReadOnly() bool { return true }

// Subprocess reports that git_log launches an OS subprocess (the system git) — the unfakeable
// marker that OUTRANKS the read-only declaration above (confinement-execution-contract §4,
// amended 2026-07-26), so the call takes the subprocess row: confined in Auto, gated below it.
func (t *GitLog) Subprocess() bool { return true }

// Execute runs `git log` over the validated ref and renders one line per commit. A missing
// git, an invalid ref, or a git failure (an unknown ref, a repository with no commits yet, or
// no repository at all) is surfaced as a result carrying git's own message; only ctx
// cancellation or a confinement-unavailable demotion is a Go error.
func (t *GitLog) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[gitLogArgs](call)
	if !ok {
		return fail, nil
	}

	ref := strings.TrimSpace(args.Ref)
	if ref == "" {
		ref = "HEAD"
	}
	// Same two-part ref guard as git_diff_range: the conservative character class, plus an
	// explicit leading-"-" rejection because "-" is itself a legal ref character and git would
	// otherwise read such a ref as an option flag (SEC-06).
	if !validRef.MatchString(ref) || looksLikeOption(ref) {
		return errorResult(call.ID, "invalid ref: "+ref), nil
	}

	gitPath, ok := lookGit()
	if !ok {
		return errorResult(call.ID, gitUnavailableMessage), nil
	}

	// The trailing "--" terminates the ref position, closing the same ref-vs-pathspec
	// ambiguity buildBranchArgs closes: `git log <name>` where <name> is not a ref but IS a
	// tracked path is a PATHSPEC log — it silently answers "which commits touched this file"
	// with exit 0, so a model's typo'd branch name would return a plausible, wrong history
	// reported as success. With "--" the same call fails loudly ("fatal: bad revision").
	gitArgs := []string{
		"log",
		fmt.Sprintf("--max-count=%d", clampGitLogCount(args.MaxCount)),
		"--date=" + gitLogDateFormat,
		"--format=%h %ad %s",
		ref,
		"--",
	}
	res, err := runGit(ctx, gitPath, t.root, gitTimeout, gitArgs...)
	if err != nil {
		return domain.ToolResult{}, err
	}
	if res.exitCode != 0 {
		return errorResult(call.ID, gitResultText(res, "git log failed")), nil
	}
	return okResult(call.ID, gitResultText(res, "No commits found")), nil
}

// clampGitLogCount pins a requested commit count to the 1–maxGitLogCount range. A count of
// zero is JSON-absent (Go's zero value carries no "was it supplied?" bit), and a negative one
// is nonsense, so both take the default rather than the floor — the same reading of a
// non-positive bound that grep's max_results uses.
func clampGitLogCount(n int) int {
	switch {
	case n <= 0:
		return defaultGitLogCount
	case n > maxGitLogCount:
		return maxGitLogCount
	default:
		return n
	}
}

// gitUnavailableMessage is the graceful-degradation result when git is not on PATH
// (§3a — git is a convenience dep, never a hard requirement).
const gitUnavailableMessage = "git not available: no git executable found on PATH"

var (
	_ domain.Tool           = (*GitBranch)(nil)
	_ domain.SubprocessTool = (*GitBranch)(nil)
	_ domain.Tool           = (*GitCommit)(nil)
	_ domain.SubprocessTool = (*GitCommit)(nil)
	_ domain.Tool           = (*GitDiffRange)(nil)
	_ domain.ReadOnlyTool   = (*GitDiffRange)(nil)
	_ domain.SubprocessTool = (*GitDiffRange)(nil)
	_ domain.Tool           = (*GitStatus)(nil)
	_ domain.ReadOnlyTool   = (*GitStatus)(nil)
	_ domain.SubprocessTool = (*GitStatus)(nil)
	_ domain.Tool           = (*GitLog)(nil)
	_ domain.ReadOnlyTool   = (*GitLog)(nil)
	_ domain.SubprocessTool = (*GitLog)(nil)
)
