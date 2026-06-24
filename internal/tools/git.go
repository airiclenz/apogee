package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The git tools (P3.9) — branch / commit / diff-range over the system git
// ----------------------------------------------------------------------------
//
// Three one-shot tools shell out to the system `git` (§3a — a convenience dep,
// detected on PATH and degrading gracefully when absent, never a hard
// dependency). They are SubprocessTools (domain.SubprocessTool): the dispatch
// disposition runs the write-capable ones (git_branch, git_commit) under
// Confiner.Confine in Auto and gates them when fs-confinement is unavailable
// ("confine if you can, gate if you can't"); git_diff_range is ReadOnly() so it
// runs freely. All three are stateless across Turns (ADR 0008 — a fresh git
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
// safeEnvKeys entry that is present in the host environment, in "KEY=value" form.
// It is a package var so a test can substitute the lookup.
var safeGitEnv = func() []string {
	env := make([]string, 0, len(safeEnvKeys))
	for _, key := range safeEnvKeys {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
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

var gitBranchSchema = json.RawMessage(`{
  "type": "object",
  "required": ["action"],
  "properties": {
    "action": {"type": "string", "enum": ["create", "switch", "list", "delete"], "description": "The branch operation to perform"},
    "name": {"type": "string", "description": "Branch name (required for create, switch, delete)"},
    "start_point": {"type": "string", "description": "Starting point for create (commit, tag, or branch). Default: HEAD"}
  }
}`)

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
type GitBranch struct{ root string }

// NewGitBranch returns a git-branch tool operating in root.
func NewGitBranch(root string) *GitBranch { return &GitBranch{root: root} }

// Name returns the stable identifier the model calls.
func (t *GitBranch) Name() string { return "git_branch" }

// Description returns the model-facing summary of the tool.
func (t *GitBranch) Description() string {
	return "Manage git branches: create, switch, list, or delete. Uses safe delete (-d) which refuses to delete unmerged branches. Deletion of main/master/develop is blocked."
}

// Schema returns the JSON schema of the tool's arguments.
func (t *GitBranch) Schema() json.RawMessage { return gitBranchSchema }

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

	var args gitBranchArgs
	if err := decodeArgs(call.Arguments, &args); err != nil {
		return errorResult(call.ID, "invalid arguments: "+err.Error()), nil
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

	switch action {
	case "create":
		out := []string{"checkout", "-b", args.Name}
		if args.StartPoint != "" {
			out = append(out, args.StartPoint)
		}
		return out, ""
	case "switch":
		return []string{"checkout", args.Name}, ""
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

var gitCommitSchema = json.RawMessage(`{
  "type": "object",
  "required": ["message"],
  "properties": {
    "message": {"type": "string", "description": "Commit message"},
    "files": {"type": "array", "items": {"type": "string"}, "description": "Files to stage before committing. If omitted, commits whatever is currently staged."},
    "amend": {"type": "boolean", "description": "Amend the previous commit (blocked on published commits)"},
    "allow_empty": {"type": "boolean", "description": "Allow creating an empty commit"}
  }
}`)

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
type GitCommit struct{ root string }

// NewGitCommit returns a git-commit tool operating in root.
func NewGitCommit(root string) *GitCommit { return &GitCommit{root: root} }

// Name returns the stable identifier the model calls.
func (t *GitCommit) Name() string { return "git_commit" }

// Description returns the model-facing summary of the tool.
func (t *GitCommit) Description() string {
	return "Stage files and create a git commit. If files are specified they are staged first; otherwise commits whatever is currently staged. Amend is blocked on published commits to prevent divergent history."
}

// Schema returns the JSON schema of the tool's arguments.
func (t *GitCommit) Schema() json.RawMessage { return gitCommitSchema }

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

	var args gitCommitArgs
	if err := decodeArgs(call.Arguments, &args); err != nil {
		return errorResult(call.ID, "invalid arguments: "+err.Error()), nil
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

var gitDiffRangeSchema = json.RawMessage(`{
  "type": "object",
  "required": ["base", "head"],
  "properties": {
    "base": {"type": "string", "description": "Base ref (commit SHA, branch name, or tag)"},
    "head": {"type": "string", "description": "Head ref (commit SHA, branch name, or tag)"},
    "paths": {"type": "array", "items": {"type": "string"}, "description": "Restrict diff to specific file paths"},
    "stat": {"type": "boolean", "description": "Show diffstat summary instead of full diff (default: false)"},
    "name_only": {"type": "boolean", "description": "Show only names of changed files (default: false)"}
  }
}`)

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
type GitDiffRange struct{ root string }

// NewGitDiffRange returns a git-diff-range tool operating in root.
func NewGitDiffRange(root string) *GitDiffRange { return &GitDiffRange{root: root} }

// Name returns the stable identifier the model calls.
func (t *GitDiffRange) Name() string { return "git_diff_range" }

// Description returns the model-facing summary of the tool.
func (t *GitDiffRange) Description() string {
	return "Show the diff between two git refs (commits, branches, or tags). Uses three-dot diff to show what changed on the head ref since it diverged from the base ref."
}

// Schema returns the JSON schema of the tool's arguments.
func (t *GitDiffRange) Schema() json.RawMessage { return gitDiffRangeSchema }

// ReadOnly reports that git_diff_range performs no writes — it runs in Plan and is
// never gated/confined as a write (a diff is harmless inspection).
func (t *GitDiffRange) ReadOnly() bool { return true }

// Subprocess reports that git_diff_range launches an OS subprocess (the system
// git). It is read-only, so the disposition runs it freely (read-only wins over the
// subprocess class), but the marker keeps the classification honest.
func (t *GitDiffRange) Subprocess() bool { return true }

// Execute runs the three-dot diff between the validated refs through the system
// git. A missing git, an invalid/missing ref, a path escape, or a git failure are
// surfaced as results; only ctx cancellation is a Go error (this tool never
// confines — it is read-only).
func (t *GitDiffRange) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	var args gitDiffRangeArgs
	if err := decodeArgs(call.Arguments, &args); err != nil {
		return errorResult(call.ID, "invalid arguments: "+err.Error()), nil
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
)
