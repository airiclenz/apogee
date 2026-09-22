package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/gitexec"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/subprocess"
)

// ----------------------------------------------------------------------------
// The git tools (P3.9) — branch / commit / diff-range / status / log / show over the system git
// ----------------------------------------------------------------------------
//
// Six one-shot tools shell out to the system `git` (§3a — a convenience dep, detected on
// PATH and degrading gracefully when absent, never a hard dependency). All six are
// SubprocessTools (domain.SubprocessTool): the dispatch disposition runs the write pair —
// git_branch and git_commit — under Confiner.Confine in Auto and gates them when
// fs-confinement is unavailable ("confine if you can, gate if you can't").
//
// git_diff_range, git_status, git_log and (since 2026-09-15) git_show also declare ReadOnly(),
// and since 2026-09-06 they carry the unexported readOnlySubprocess marker
// (readonly_subprocess.go) — that marker is what classifies them: RO-subproc, a class the
// ladder gives the READ-ONLY row in EVERY mode (confinement-execution-contract §4, amended
// 2026-09-06). So Plan offers and runs the read set, and Auto runs it unconfined like
// read_file. Their Subprocess() declaration is unchanged and still drives the execution
// mechanics — the scoped environment, the argv fence, the §2.4 process-group teardown.
//
// All six are stateless across Turns (ADR 0008 — a fresh git process per call), path-scope
// their inputs to the workspace root, and run with a scrubbed, allowlisted environment so a
// stray inherited variable cannot change git's behaviour.
//
// The environment is only half of that: git also runs programs the REPOSITORY names — hooks,
// filesystem monitors, filter, diff, merge and credential drivers, editors, pagers — which on an
// attacker-authored checkout are attacker-authored scripts. Neither half is implemented here any
// more: internal/gitexec is the hardened runner every one of these tools spawns through, and it
// owns the allowlisted PATH-scoped environment, the per-invocation switches that neutralise what
// they can, and the outright refusal of a repository whose own config names a program git would
// execute. What sits between the tools and that package are the thin wrappers below.

// gitTimeout bounds a single git invocation. git operations are local (no network
// op is exposed by these tools), so a short ceiling is ample and a hung git never
// wedges a Turn (the §2.4 teardown reaps the process group when it fires).
const gitTimeout = 15 * time.Second

// gitDiffTimeout bounds a diff-range, which can be larger; it matches the oracle's
// separate diff ceiling.
const gitDiffTimeout = 10 * time.Second

// runGit runs git with gitArgs in root under the per-call timeout and the hardened, scrubbed
// environment, honouring the confinement handle the disposition installed (if any), and returns
// the captured outcome in the core's shape. Every git TOOL invocation goes through here; the
// hardening, the repo-local command-config refusal and its memoised probe are gitexec.Capture's.
// A missing git is signalled by the caller's gitexec.Program, not here. The Go error is non-nil only
// for ctx cancellation or a confinement-unavailable demotion (the runSubprocess contract).
func runGit(ctx context.Context, gitPath, root string, timeout time.Duration, gitArgs ...string) (subprocess.SubprocessResult, error) {
	return gitexec.Capture(ctx, gitPath, root, timeout, gitArgs...)
}

// runGitUnchecked is runGit without the command-config probe — the shape a test asserting what
// the probe itself would see needs. Nothing a MODEL causes may use it: every tool goes through
// runGit.
func runGitUnchecked(ctx context.Context, gitPath, root string, timeout time.Duration, gitArgs ...string) (subprocess.SubprocessResult, error) {
	return gitexec.CaptureUnchecked(ctx, gitPath, root, nil, timeout, gitArgs...)
}

// RunGitQuery runs one read-side git command in root for the ENGINE itself and returns its
// standard output alone. It is the funnel entry for apogee's own bookkeeping git — today the
// tracked-file mutation floor (internal/agent/treesnapshot.go), which snapshots the tree around
// every subprocess tool call — so that git gets everything a git TOOL's git gets: the exec fence
// on the resolved binary, the hardening options, GIT_CONFIG_NOSYSTEM, the allowlisted
// workspace-scoped environment (no APOGEE_API_KEY, no inherited config redirection), the
// repo-local command-config refusal, and the §2.4 process-tree teardown.
//
// It is gitexec.Run under this package's name: the engine's git resolves through
// gitexec.LookPath, while the git TOOLS resolve through the execHost they were built with — a
// test that plants a git for the tools hands it to their host; one that plants it for the
// engine swaps gitexec.LookPath. The empty-args check stays here so the sentence is this
// funnel's own.
//
// Every failure is ONE error and they are deliberately not distinguished: git absent, a fenced
// binary, a refused repository, a non-zero exit, a timeout, a wedged drain and a cancelled ctx
// all mean "no trustworthy answer", which is precisely what the caller acts on — the floor's
// contract is that any failure skips the check silently for that call (ADR 0056 decision 4).
//
// It is NOT for tool results. A tool shows the model what git printed, exit code and stderr
// included, so the git tools keep runGit's captured outcome; this returns stdout as DATA, with
// the diagnostics left out of the payload.
func RunGitQuery(ctx context.Context, root string, timeout time.Duration, args ...string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("apogee: RunGitQuery: no git subcommand")
	}
	return gitexec.Run(ctx, root, nil, timeout, args...)
}

// gitResultText renders a captured git outcome as text the model reads: the
// combined output trimmed, or the fallback when git printed nothing on success.
func gitResultText(res subprocess.SubprocessResult, successFallback string) string {
	out := strings.TrimSpace(res.CombinedOutput)
	if res.ExitCode == 0 && out == "" {
		return successFallback
	}
	return out
}

// ----------------------------------------------------------------------------
// gitRead — the one read call under git_status, git_log, git_diff_range and git_show
// ----------------------------------------------------------------------------

// gitRef is a revision the ref guard admitted: validRef's conservative character class AND
// looksLikeOption's refusal of a leading "-" (SEC-06 — "-" is itself a legal ref character, so
// a ref passing the class alone could still be read by git as an option flag). guardRef is its
// only mint, which is what lets gitRead take revisions by this type and lets the RO-subproc
// conditions (readonly_subprocess.go) read "a gitRef" rather than "a ref each tool remembered
// to check". git_branch keeps its own name guard: a branch name there is CREATED as well as
// named, and validRef would tighten what may be created.
type gitRef string

// guardRef mints a gitRef from a model-supplied revision, or reports ok=false when the ref
// fails either half of the guard. The caller renders the refusal, since the wording names the
// field the ref came from ("invalid base ref: …", "invalid ref: …").
func guardRef(ref string) (gitRef, bool) {
	if !validRef.MatchString(ref) || looksLikeOption(ref) {
		return "", false
	}
	return gitRef(ref), true
}

// gitReadCall is one read-side invocation for gitRead: the verb and whether it produces a diff,
// its own options, the revisions and pathspecs the model chose, and the two fallback wordings
// gitResultText renders when git printed nothing. Its argv method is the one spelling of the
// command line the four read tools share.
type gitReadCall struct {
	// verb is the git subcommand; it also keys the timeout (gitReadTimeout).
	verb string
	// diffProducing puts gitexec.DiffHardeningArgs right after the verb — required on every
	// invocation that could run a textconv or ext-diff driver (log, diff, and a blob read by
	// path). `git status` takes none and would reject them.
	diffProducing bool
	// flags are the verb's own options, after the hardening.
	flags []string
	// refs are bare revisions, positional after the flags and ALWAYS followed by "--": a bare
	// name is where git's ref-vs-pathspec ambiguity lives — `git log <name>` where <name> is
	// not a ref but IS a tracked path is a pathspec log that silently answers a different
	// question with exit 0 — and the terminator turns that into a loud "bad revision".
	refs []gitRef
	// object is a composed positional argument that is not a bare ref — git_diff_range's
	// `base...head` range, git_show's `<ref>:./<rel>` blob — spelled by the caller from gitRef
	// values. It takes "--" only when pathspecs follow.
	object string
	// pathspecs narrow the call; each is workspace-relative (workspacePathspec) because the
	// process runs in the root and carries the :(literal) magic (literalPathspec) so a glob
	// metacharacter in a model-supplied name is not interpreted, and they come last, after the
	// "--" that is the one place git reads them as pathspecs and nothing else.
	pathspecs []string
	// failWording is what gitResultText shows for a non-zero exit that printed nothing.
	failWording string
	// fallback is what gitResultText shows for a success that printed nothing.
	fallback string
}

// argv spells the command line, without the program: verb, hardening, flags, refs, object,
// the "--" terminator whenever a bare ref or a pathspec is present, then the pathspecs.
func (c gitReadCall) argv() []string {
	args := []string{c.verb}
	if c.diffProducing {
		args = append(args, gitexec.DiffHardeningArgs...)
	}
	args = append(args, c.flags...)
	for _, ref := range c.refs {
		args = append(args, string(ref))
	}
	if c.object != "" {
		args = append(args, c.object)
	}
	if len(c.refs) > 0 || len(c.pathspecs) > 0 {
		args = append(args, "--")
	}
	return append(args, c.pathspecs...)
}

// gitReadTimeout keys the per-call ceiling on the verb: a diff and a blob read can be larger
// and keep gitDiffTimeout (the oracle's separate diff ceiling); status and log keep gitTimeout.
func gitReadTimeout(verb string) time.Duration {
	switch verb {
	case "diff", "show":
		return gitDiffTimeout
	default:
		return gitTimeout
	}
}

// gitRead is the one read call the four RO-subproc git tools spawn through: it resolves git for
// root (gitexec.Program, with look — the calling tool's execHost look), runs the call's argv
// through runGit under the verb's timeout, and renders the outcome with gitResultText. It
// returns the raw capture alongside the rendering because two callers read the bytes rather than
// the text — git_show hands res.CombinedOutput to renderFile untrimmed, git_status parses its
// porcelain — and text is the model-facing sentence for the rest: on ok=false the failure (git
// absent or fenced, a refused repository, or a non-zero exit rendered with failWording), on
// ok=true the success rendered with fallback. A git that could not be resolved is returned in the
// same shape
// gitexec.Capture gives a refused repository — a failed outcome carrying the sentence — so a
// caller has one failure branch. The Go error is non-nil only for ctx cancellation or a
// confinement-unavailable demotion (the runSubprocess contract).
func gitRead(ctx context.Context, root string, look gitexec.LookFunc, c gitReadCall) (res subprocess.SubprocessResult, text string, ok bool, err error) {
	gitPath, refusal, ok := gitexec.Program(ctx, root, look)
	if !ok {
		return subprocess.SubprocessResult{CombinedOutput: refusal, ExitCode: 1}, refusal, false, nil
	}
	res, err = runGit(ctx, gitPath, root, gitReadTimeout(c.verb), c.argv()...)
	if err != nil {
		return subprocess.SubprocessResult{}, "", false, err
	}
	if res.ExitCode != 0 {
		return res, gitResultText(res, c.failWording), false, nil
	}
	return res, gitResultText(res, c.fallback), true, nil
}

// ----------------------------------------------------------------------------
// gitWrite — the one write call under git_branch, git_commit and the staging helper
// ----------------------------------------------------------------------------

// gitWrite is the one call every MUTATING git invocation spawns through — git_branch's four
// actions, git_commit's `add` and `commit`, and the staging helper's trackedness probe and
// `add -A` (internal/tools/git_stage.go). It resolves git for root (gitexec.Program, with look —
// the calling tool's execHost look), runs `verb args...` through runGit under gitTimeout, and
// returns the raw capture beside its rendering the way gitRead does: on ok=false text is the
// failure (git absent or fenced, a refused repository, or a non-zero exit rendered with
// failWording); on ok=true it is git's trimmed output, which may be empty — the success wording
// is the caller's, since it names the action ("Created and switched to branch …", "commit
// created") and git_branch's list re-renders the raw output first. A git that could not be
// resolved is returned in the shape
// gitexec.Capture gives a refused repository — a failed outcome carrying the sentence — so a
// caller has one failure branch. The Go error is non-nil only for ctx cancellation or a
// confinement-unavailable demotion (the runSubprocess contract).
//
// The write verbs carry no diff hardening (none of them renders a diff) and take their argv
// as the caller spelled it: git_branch's argv is buildBranchArgs' validated output, and
// git_commit terminates its pathspecs with "--" under the workspace-relative rule
// (workspacePathspec) with the :(literal) magic (literalPathspec) on each, exactly as the
// staging helper's pathspecs carry it. The read-side pre-check and summary git_commit makes
// around its commit are reads and go through gitRead.
func gitWrite(ctx context.Context, root string, look gitexec.LookFunc, verb string, args []string, failWording string) (res subprocess.SubprocessResult, text string, ok bool, err error) {
	gitPath, refusal, ok := gitexec.Program(ctx, root, look)
	if !ok {
		return subprocess.SubprocessResult{CombinedOutput: refusal, ExitCode: 1}, refusal, false, nil
	}
	res, err = runGit(ctx, gitPath, root, gitTimeout, append([]string{verb}, args...)...)
	if err != nil {
		return subprocess.SubprocessResult{}, "", false, err
	}
	if res.ExitCode != 0 {
		return res, gitResultText(res, failWording), false, nil
	}
	return res, gitResultText(res, ""), true, nil
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
	host execHost
}

// NewGitBranch returns a git-branch tool operating in root that resolves git on the real operating
// system (defaultExecHost); builtinTools builds the git family on one host through newGitBranch.
func NewGitBranch(root string) *GitBranch { return newGitBranch(root, defaultExecHost()) }

// newGitBranch is NewGitBranch with the host whose look resolves git supplied — one execHost shared by
// the execution tools in production, a host carrying a fake look in a test.
func newGitBranch(root string, host execHost) *GitBranch {
	return &GitBranch{toolSpec: gitBranchSpec, root: root, host: host}
}

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

	res, text, ok, err := gitWrite(ctx, t.root, t.host.look, gitArgs[0], gitArgs[1:], "git branch failed")
	if err != nil {
		return domain.ToolResult{}, err
	}
	if !ok {
		return errorResult(call.ID, text), nil
	}
	if args.Action == "list" {
		res.CombinedOutput = renderBranchList(res.CombinedOutput)
	}
	return okResult(call.ID, gitResultText(res, branchSuccessMessage(args))), nil
}

// branchListFormat is the --format `list` asks git for: the FULL refname rather than the short
// one, so the one branch process says which entries are remote-tracking refs (refs/remotes/…)
// and renderBranchList can mark them — `%(refname:short)` had already folded that away, and a
// second `branch -r` process would break the "one list, one process" pin. `%(HEAD)` is the
// current-branch "*" as before.
const branchListFormat = "%(refname) %(HEAD)"

// Where a full refname says a branch lives: under refs/heads/ it is local, under refs/remotes/
// it is a remote-tracking ref.
const (
	localRefPrefix  = "refs/heads/"
	remoteRefPrefix = "refs/remotes/"
)

// renderBranchList turns the branchListFormat lines back into the short names the model read
// before — `main *`, `feature  ` — with every remote-tracking ref suffixed ` (remote)` after its
// name (`origin/main (remote)  `), so a model choosing a branch to switch to or delete can tell
// `origin/main` from `main` without a second call. A line git synthesises without a refname
// (the detached-HEAD entry) passes through untouched.
func renderBranchList(out string) string {
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		name, rest, _ := strings.Cut(line, " ")
		switch {
		case strings.HasPrefix(name, localRefPrefix):
			name = strings.TrimPrefix(name, localRefPrefix)
		case strings.HasPrefix(name, remoteRefPrefix):
			name = strings.TrimPrefix(name, remoteRefPrefix) + " (remote)"
		default:
			continue
		}
		lines[i] = name + " " + rest
	}
	return strings.Join(lines, "\n")
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
		return []string{"branch", "-a", "--format=" + branchListFormat}, ""
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

// GitCommitToolName is the registry name of the commit tool — the one call whose staged
// diff the engine scans for secret material before it resolves (internal/agent/secretsguard.go).
const GitCommitToolName = "git_commit"

var gitCommitSpec = toolSpec{
	name:        GitCommitToolName,
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
	host execHost
}

// NewGitCommit returns a git-commit tool operating in root that resolves git on the real operating
// system (defaultExecHost); builtinTools builds the git family on one host through newGitCommit.
func NewGitCommit(root string) *GitCommit { return newGitCommit(root, defaultExecHost()) }

// newGitCommit is NewGitCommit with the host whose look resolves git supplied — one execHost shared by
// the execution tools in production, a host carrying a fake look in a test.
func newGitCommit(root string, host execHost) *GitCommit {
	return &GitCommit{toolSpec: gitCommitSpec, root: root, host: host}
}

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

	// Amend is refused on a commit some remote already holds, so the tool never rewrites
	// history a remote has seen. The question is put to git rather than inferred from the
	// tip's decoration: `branch -r --contains HEAD` answers it for a remote under any name
	// and for a local branch that has fallen BEHIND its remote, where the decoration names
	// only the refs pointing AT the commit. A non-zero exit — no remotes configured, or a
	// state git cannot answer — reads as unpublished, the guard's existing degrade: it lets
	// the amend through rather than blocking work on an answer it does not have. It is a
	// read and goes through gitRead; HEAD is --contains' value, not a positional revision, so
	// it rides the flags slot. A git that cannot be resolved at all is the same ok=false and
	// falls through to the commit itself, which reports the refusal.
	if args.Amend {
		remotes, _, ok, err := gitRead(ctx, t.root, t.host.look, gitReadCall{
			verb:  "branch",
			flags: []string{"-r", "--contains", "HEAD"},
		})
		if err != nil {
			return domain.ToolResult{}, err
		}
		if ok && remoteBranchesListed(remotes.CombinedOutput) {
			return errorResult(call.ID, "cannot amend a commit that has been pushed to a remote; create a new commit instead"), nil
		}
	}

	// Stage the named files first (path-safe), so a commit only ever touches paths
	// inside the workspace: each is fenced, spelled workspace-relative (workspacePathspec) and
	// carries the :(literal) magic (literalPathspec) — a name holding *, ? or [ stages the file
	// it names and never a glob's other matches — after the "--" that makes git read it as a
	// pathspec and nothing else. An entry git cannot match is git's own stderr, which quotes
	// the :(literal) spelling.
	if len(args.Files) > 0 {
		pathspecs, err := CommitPathspecs(args.Files, t.root)
		if err != nil {
			return errorResult(call.ID, err.Error()), nil
		}
		_, text, ok, err := gitWrite(ctx, t.root, t.host.look, "add", append([]string{"--"}, pathspecs...), "git add failed")
		if err != nil {
			return domain.ToolResult{}, err
		}
		if !ok {
			return errorResult(call.ID, text), nil
		}
	}

	// --no-verify refuses the pre-commit and commit-msg hooks explicitly, on top of the
	// emptied core.hooksPath every invocation already carries (gitexec's hardening options). The
	// belt-and-braces is deliberate: this is the one path where a hook both runs an
	// attacker-authored script AND can rewrite or veto the message the operator approved.
	//
	// --no-gpg-sign is the same treatment for the other program a commit can launch: a repo-local
	// commit.gpgsign=true plus a gpg.program pointing at an attacker-authored script would run it
	// on every commit. gpg.program is refused outright (gitexec.CommandConfigName), so this covers the
	// residual — the operator's OWN global gpg.program, which the refusal deliberately leaves
	// alone — by not asking for a signature at all. apogee's commits are unsigned by design; a
	// commit the operator wants signed is one they make themselves.
	commitArgs := []string{"--no-verify", "--no-gpg-sign", "-m", message}
	if args.Amend {
		commitArgs = append(commitArgs, "--amend")
	}
	if args.AllowEmpty {
		commitArgs = append(commitArgs, "--allow-empty")
	}
	res, text, ok, err := gitWrite(ctx, t.root, t.host.look, "commit", commitArgs, "git commit failed")
	if err != nil {
		return domain.ToolResult{}, err
	}
	if !ok {
		return errorResult(call.ID, text), nil
	}

	// Report the new commit's one-line summary (best-effort; the commit already
	// succeeded, so a failed summary is not surfaced as the call's error). A read, so it
	// goes through gitRead and carries the diff hardening every log does.
	_, summary, ok, err := gitRead(ctx, t.root, t.host.look, gitReadCall{
		verb:          "log",
		diffProducing: true,
		flags:         []string{"-1", "--oneline"},
	})
	if err != nil {
		return domain.ToolResult{}, err
	}
	if ok && summary != "" {
		return okResult(call.ID, summary), nil
	}
	return okResult(call.ID, gitResultText(res, "commit created")), nil
}

// remoteBranchesListed reports whether `git branch -r --contains <commit>` listed at least
// one remote-tracking branch, i.e. some remote holds the commit. git lists one branch per
// line and prints nothing at all when none contains it, so any non-blank line is a hit —
// the remote's name is irrelevant, only that a remote has seen the commit.
func remoteBranchesListed(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) != "" {
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
	host execHost
}

// NewGitDiffRange returns a git-diff-range tool operating in root that resolves git on the real operating
// system (defaultExecHost); builtinTools builds the git family on one host through newGitDiffRange.
func NewGitDiffRange(root string) *GitDiffRange { return newGitDiffRange(root, defaultExecHost()) }

// newGitDiffRange is NewGitDiffRange with the host whose look resolves git supplied — one execHost shared by
// the execution tools in production, a host carrying a fake look in a test.
func newGitDiffRange(root string, host execHost) *GitDiffRange {
	return &GitDiffRange{toolSpec: gitDiffRangeSpec, root: root, host: host}
}

// ReadOnly reports that git_diff_range performs no writes (a diff is harmless
// inspection) — an honest statement about the tool, read by self-regulation's
// read/write tally. On its own it does not classify the call; what does is the
// readOnlySubprocess marker below, which the ladder reads as the read-only row in
// every mode (confinement-execution-contract §4, amended 2026-09-06).
func (t *GitDiffRange) ReadOnly() bool { return true }

// Subprocess reports that git_diff_range launches an OS subprocess (the system
// git). The marker still drives the execution MECHANICS — the scoped environment,
// the argv fence, the §2.4 process-group teardown — but no longer the class: the
// readOnlySubprocess marker below classifies the call RO-subproc.
func (t *GitDiffRange) Subprocess() bool { return true }

// readOnlySubprocess mints the RO-subproc marker for git_diff_range: its one invocation goes
// through gitRead as a diff-producing call (gitexec.DiffHardeningArgs), names both refs as
// gitRef values guardRef minted, and writes nothing (readonly_subprocess.go).
func (t *GitDiffRange) readOnlySubprocess() {}

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
	// The two-part ref guard (guardRef): the conservative character class, plus an explicit
	// leading-"-" rejection, because "-" is a legal ref character and a ref beginning with it
	// would be read as an option even after the "..." join (SEC-06).
	base, ok := guardRef(args.Base)
	if !ok {
		return errorResult(call.ID, "invalid base ref: "+args.Base), nil
	}
	head, ok := guardRef(args.Head)
	if !ok {
		return errorResult(call.ID, "invalid head ref: "+args.Head), nil
	}

	var flags []string
	if args.Stat {
		flags = append(flags, "--stat")
	}
	if args.NameOnly {
		flags = append(flags, "--name-only")
	}
	// Path-scope each restriction to the workspace, so the diff cannot be pointed outside the
	// root; the pathspec git gets is the workspace-relative spelling (workspacePathspec) under
	// the :(literal) magic (literalPathspec), so a name holding *, ? or [ narrows to that file
	// and never to a glob's other matches — the rule git_commit and git_log follow too; git_show
	// alone stays bare, its argument being an object name rather than a pathspec.
	pathspecs := make([]string, 0, len(args.Paths))
	for _, p := range args.Paths {
		pathspec, err := workspacePathspec(p, t.root)
		if err != nil {
			return errorResult(call.ID, err.Error()), nil
		}
		pathspecs = append(pathspecs, literalPathspec(pathspec))
	}

	_, text, ok, err := gitRead(ctx, t.root, t.host.look, gitReadCall{
		verb:          "diff",
		diffProducing: true,
		flags:         flags,
		object:        string(base) + "..." + string(head),
		pathspecs:     pathspecs,
		failWording:   "git diff failed",
		fallback:      "No differences found",
	})
	if err != nil {
		return domain.ToolResult{}, err
	}
	if !ok {
		return errorResult(call.ID, text), nil
	}
	return okResult(call.ID, text), nil
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
	host execHost
}

// NewGitStatus returns a git-status tool operating in root that resolves git on the real operating
// system (defaultExecHost); builtinTools builds the git family on one host through newGitStatus.
func NewGitStatus(root string) *GitStatus { return newGitStatus(root, defaultExecHost()) }

// newGitStatus is NewGitStatus with the host whose look resolves git supplied — one execHost shared by
// the execution tools in production, a host carrying a fake look in a test.
func newGitStatus(root string, host execHost) *GitStatus {
	return &GitStatus{toolSpec: gitStatusSpec, root: root, host: host}
}

// ReadOnly reports that git_status performs no writes (reading the index and working tree
// changes nothing) — an honest statement about the tool, read by self-regulation's read/write
// tally. As with git_diff_range it does not classify the call on its own: the
// readOnlySubprocess marker below does, and the ladder reads that as the read-only row in
// every mode (confinement-execution-contract §4, amended 2026-09-06).
func (t *GitStatus) ReadOnly() bool { return true }

// Subprocess reports that git_status launches an OS subprocess (the system git). The marker
// still drives the execution mechanics — the scoped environment, the argv fence, the §2.4
// teardown — while the readOnlySubprocess marker below classifies the call RO-subproc.
func (t *GitStatus) Subprocess() bool { return true }

// readOnlySubprocess mints the RO-subproc marker for git_status: its one invocation goes
// through gitRead, takes no ref from the model, and writes nothing (readonly_subprocess.go).
func (t *GitStatus) readOnlySubprocess() {}

// Execute runs `git status` in porcelain v2 form and renders it for the model. A missing git
// or a git failure (not a repository, most often) is surfaced as a result; only ctx
// cancellation or a confinement-unavailable demotion is a Go error.
func (t *GitStatus) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	// Porcelain v2 is the stable machine format (it carries the branch and ahead/behind
	// headers v1 lacks), and -z makes every record NUL-terminated so a path containing a
	// space, a quote, or a newline arrives verbatim instead of C-quoted.
	//
	// --ignore-submodules=dirty is what keeps this a single process: with git's default of
	// none, git status runs `git status --porcelain=2` INSIDE every submodule, and that child
	// reads the submodule's own config (.git/modules/<name>/config), which
	// repoLocalCommandConfig never scans — so the program-key refusal that guards this
	// repository does not guard the child. dirty drops the child spawn while still reporting a
	// submodule whose recorded commit moved; only work-tree dirt inside a submodule goes
	// unreported.
	//
	// It is the one read that produces no diff, so it carries no gitexec.DiffHardeningArgs —
	// `git status` would reject them.
	res, text, ok, err := gitRead(ctx, t.root, t.host.look, gitReadCall{
		verb:        "status",
		flags:       []string{"--porcelain=v2", "--branch", "--ignore-submodules=dirty", "-z"},
		failWording: "git status failed",
	})
	if err != nil {
		return domain.ToolResult{}, err
	}
	if !ok {
		return errorResult(call.ID, text), nil
	}
	rep := parseGitStatus(res.CombinedOutput)
	return okSummary(call.ID, renderGitStatus(rep), rep.changedFiles()), nil
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

// changedFiles reports the report's three section counts as git_status' structured outcome. Each
// is the FULL length of its list — the same number writeStatusSection prints in its header — so a
// section the render capped is still counted whole, and a clean tree reports three zeros rather
// than no summary at all. It is the ONE derivation of that triple: Execute attaches what this
// returns, so what the host reads and what the prose states cannot drift apart.
func (r gitStatusReport) changedFiles() domain.ChangedFiles {
	return domain.ChangedFiles{
		Staged:    len(r.staged),
		Unstaged:  len(r.unstaged),
		Untracked: len(r.untracked),
	}
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
	description: "Show the recent commit history of a git ref (branch, tag, or commit) as one line per commit: short hash, ISO date, and subject, optionally narrowed to the commits that touched one path. Read-only — it changes nothing. Defaults to HEAD and to the 20 most recent commits.",
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "ref": {"type": "string", "description": "Ref to log (branch name, tag, or commit SHA). Defaults to HEAD."},
    "max_count": {"type": "integer", "description": "How many commits to show, most recent first (default 20, clamped to 100)"},
    "path": {"type": "string", "description": "Optional file or directory, relative to the workspace root: only the commits that touched it are shown"}
  }
}`),
}

type gitLogArgs struct {
	Ref      string `json:"ref"`
	MaxCount int    `json:"max_count"`
	Path     string `json:"path"`
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
	host execHost
}

// NewGitLog returns a git-log tool operating in root that resolves git on the real operating
// system (defaultExecHost); builtinTools builds the git family on one host through newGitLog.
func NewGitLog(root string) *GitLog { return newGitLog(root, defaultExecHost()) }

// newGitLog is NewGitLog with the host whose look resolves git supplied — one execHost shared by
// the execution tools in production, a host carrying a fake look in a test.
func newGitLog(root string, host execHost) *GitLog {
	return &GitLog{toolSpec: gitLogSpec, root: root, host: host}
}

// ReadOnly reports that git_log performs no writes (reading history changes nothing) — an
// honest statement about the tool, read by self-regulation's read/write tally. As with
// git_diff_range and git_status it does not classify the call on its own: the
// readOnlySubprocess marker below does, and the ladder reads that as the read-only row in
// every mode (confinement-execution-contract §4, amended 2026-09-06).
func (t *GitLog) ReadOnly() bool { return true }

// Subprocess reports that git_log launches an OS subprocess (the system git). The marker still
// drives the execution mechanics — the scoped environment, the argv fence, the §2.4 teardown —
// while the readOnlySubprocess marker below classifies the call RO-subproc.
func (t *GitLog) Subprocess() bool { return true }

// readOnlySubprocess mints the RO-subproc marker for git_log: its one invocation goes through
// gitRead as a diff-producing call (gitexec.DiffHardeningArgs), names its ref as the gitRef
// guardRef minted, and writes nothing (readonly_subprocess.go).
func (t *GitLog) readOnlySubprocess() {}

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
	// Same two-part ref guard as git_diff_range (guardRef): the conservative character class,
	// plus an explicit leading-"-" rejection because "-" is itself a legal ref character and
	// git would otherwise read such a ref as an option flag (SEC-06).
	guarded, ok := guardRef(ref)
	if !ok {
		return errorResult(call.ID, "invalid ref: "+ref), nil
	}

	// The ref rides gitReadCall's refs slot, which gitRead ALWAYS follows with "--": that
	// terminator closes the same ref-vs-pathspec ambiguity buildBranchArgs closes — `git log
	// <name>` where <name> is not a ref but IS a tracked path is a PATHSPEC log, silently
	// answering "which commits touched this file" with exit 0, so a model's typo'd branch name
	// would return a plausible, wrong history reported as success; with "--" the same call
	// fails loudly ("fatal: bad revision"). A path the call DID ask for goes after that "--" —
	// the one place git reads it as a pathspec and nothing else — workspace-relative, since
	// the process runs in the root, and under the :(literal) magic (literalPathspec), so a
	// name holding *, ? or [ is the file it names and not a glob — the same rule git_commit
	// and git_diff_range follow.
	var pathspecs []string
	if strings.TrimSpace(args.Path) != "" {
		pathspec, err := workspacePathspec(args.Path, t.root)
		if err != nil {
			return errorResult(call.ID, err.Error()), nil
		}
		pathspecs = []string{literalPathspec(pathspec)}
	}
	_, text, ok, err := gitRead(ctx, t.root, t.host.look, gitReadCall{
		verb:          "log",
		diffProducing: true,
		flags: []string{
			fmt.Sprintf("--max-count=%d", clampGitLogCount(args.MaxCount)),
			"--date=" + gitLogDateFormat,
			"--format=%h %ad %s",
		},
		refs:        []gitRef{guarded},
		pathspecs:   pathspecs,
		failWording: "git log failed",
		fallback:    "No commits found",
	})
	if err != nil {
		return domain.ToolResult{}, err
	}
	if !ok {
		return errorResult(call.ID, text), nil
	}
	return okResult(call.ID, text), nil
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

// CommitPathspecs is the pathspec builder git_commit stages its `files` through: each entry
// resolved through the workspace fence, spelled workspace-relative (workspacePathspec) and
// carrying the :(literal) magic (literalPathspec), in the order the model wrote them, without
// the terminating "--" the caller places ahead of them. The first path that escapes the root —
// a symlink out of it or a ".." climb — is the error, so nothing is staged from a list that
// names anything outside the workspace. It is exported because the engine's secrets pre-check
// (internal/agent/secretsguard.go) stages the same list into a shadow index before the tool
// runs, and the two sites must agree on every spelling: a path the tool would refuse is a path
// the pre-check must not judge.
func CommitPathspecs(files []string, root string) ([]string, error) {
	pathspecs := make([]string, 0, len(files))
	for _, f := range files {
		pathspec, err := workspacePathspec(f, root)
		if err != nil {
			return nil, err
		}
		pathspecs = append(pathspecs, literalPathspec(pathspec))
	}
	return pathspecs, nil
}

// workspacePathspec resolves a model-supplied path through the workspace fence (security.ResolveInRoot,
// so a symlink out of the root or a ".." climb is ErrPathEscape) and hands back the
// WORKSPACE-RELATIVE spelling git reads as a pathspec from a process running in the root —
// `internal/cli`, never the absolute real path, which would name the wrong tree on a box whose
// root is reached through a symlink. A trailing separator on the argument is kept, because
// `internal/` and `internal` are different pathspecs to git (the first matches a directory
// only) and the model wrote the one it meant. The spelling comes back BARE: git_commit,
// git_diff_range and git_log wrap it in literalPathspec before git sees it, and git_show alone
// uses it as-is, since its `<ref>:./<rel>` is an object name rather than a pathspec.
func workspacePathspec(input, root string) (string, error) {
	abs, err := security.ResolveInRoot(input, root)
	if err != nil {
		return "", err
	}
	rel := filepath.ToSlash(security.WorkspaceRelative(abs, root))
	if rel != "." && strings.HasSuffix(input, "/") {
		rel += "/"
	}
	return rel, nil
}

// literalPathspec prefixes a path with git's :(literal) pathspec magic, which turns off both
// glob interpretation and any other magic the string might otherwise be read as.
func literalPathspec(path string) string { return ":(literal)" + path }

// ----------------------------------------------------------------------------
// git_show — a file's content at a revision
// ----------------------------------------------------------------------------

var gitShowSpec = toolSpec{
	name:        "git_show",
	description: "Read a file as it was at a git revision (a commit, branch, or tag) without touching the working tree — the committed version, an older one, or a file that has since been deleted. Read-only — it changes nothing. Takes the same range arguments as read_file: without a range the first 400 lines (or 40 KiB) come back and the tail says how to get the rest.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["ref", "path"],
  "properties": {
    "ref": {"type": "string", "description": "Revision to read the file at (commit SHA, branch name, tag, or a form like HEAD~1)"},
    "path": {"type": "string", "description": "File path, relative to the workspace root"},
    "start_line": {"type": "integer", "description": "Optional 1-based start line"},
    "end_line": {"type": "integer", "description": "Optional 1-based end line (inclusive)"},
    "max_lines": {"type": "integer", "description": "Maximum number of lines to return"},
    "locate": {"type": "string", "description": "Optional substring to locate; the result reports the absolute 1-based line numbers where it occurs, and without a range the content is a window of 10 lines around each hit."}
  }
}`),
}

// gitShowArgs is read_file's argument set plus the revision: the embedded readFileArgs carries
// path and the range arguments under their read_file names, so the one decode serves both the
// fence and renderFile, and a model that knows read_file knows git_show.
type gitShowArgs struct {
	Ref string `json:"ref"`
	readFileArgs
}

// GitShow reads one file as it was at a revision — `git show <ref>:./<path>` — over the system
// git, scoped to a workspace root. It is the read-at-a-revision the working tree cannot answer:
// what a file looked like before the last commit, or a file a later commit deleted. Like
// git_log it is read-only by construction and carries the readOnlySubprocess marker; what it
// renders is read_file's own shape (renderFile), header, range arguments, locate and the
// open-ended cap included.
type GitShow struct {
	toolSpec
	root string
	host execHost
}

// NewGitShow returns a git-show tool operating in root that resolves git on the real operating
// system (defaultExecHost); builtinTools builds the git family on one host through newGitShow.
func NewGitShow(root string) *GitShow { return newGitShow(root, defaultExecHost()) }

// newGitShow is NewGitShow with the host whose look resolves git supplied — one execHost shared by
// the execution tools in production, a host carrying a fake look in a test.
func newGitShow(root string, host execHost) *GitShow {
	return &GitShow{toolSpec: gitShowSpec, root: root, host: host}
}

// ReadOnly reports that git_show performs no writes (reading an object changes nothing) — an
// honest statement about the tool, read by self-regulation's read/write tally. As with the other
// git reads it does not classify the call on its own: the readOnlySubprocess marker below does,
// and the ladder reads that as the read-only row in every mode (confinement-execution-contract
// §4, amended 2026-09-06).
func (t *GitShow) ReadOnly() bool { return true }

// Subprocess reports that git_show launches an OS subprocess (the system git). The marker still
// drives the execution mechanics — the scoped environment, the argv fence, the §2.4 teardown —
// while the readOnlySubprocess marker below classifies the call RO-subproc.
func (t *GitShow) Subprocess() bool { return true }

// readOnlySubprocess mints the RO-subproc marker for git_show: its one invocation goes through
// gitRead as a diff-producing call (gitexec.DiffHardeningArgs — --no-textconv matters here, since
// `git show <ref>:<path>` would otherwise run the repository's textconv driver on the blob),
// names its ref as the gitRef guardRef minted, fences its path with workspacePathspec, and
// writes nothing (readonly_subprocess.go).
func (t *GitShow) readOnlySubprocess() {}

// Execute reads the file at the revision and renders it exactly as read_file would render the
// same bytes: the `[File: <path> @ <ref>, N lines total, showing lines a-b]` header, the range
// arguments honoured as written, an open-ended read capped at defaultReadLines /
// defaultReadBytes with the tail that says how to get the rest, and a locate report when a
// term was asked for. The path is fenced to the workspace (security.ResolveInRoot — an escape is refused
// with the uniform ErrPathEscape message) and handed to git in its `./`-prefixed
// workspace-relative form, which git resolves against the process's cwd rather than the
// repository root, so a workspace that is a subdirectory of its repository reads the right
// file. A missing git, an invalid ref, a path escape, a binary blob, or a git failure — a path
// that does not exist at that ref, an unknown ref — is surfaced as a result naming the path and
// the ref; only ctx cancellation or a confinement-unavailable demotion is a Go error.
//
// The blob comes back through the subprocess's capped output (subprocess.MaxSubprocessOutputBytes),
// so a file larger than that arrives with the capped buffer's own truncation marker at its end
// rather than silently short.
func (t *GitShow) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[gitShowArgs](call)
	if !ok {
		return fail, nil
	}
	ref := strings.TrimSpace(args.Ref)
	if ref == "" {
		return errorResult(call.ID, "ref is required"), nil
	}
	// The same two-part ref guard as git_log (guardRef): the conservative character class, plus
	// an explicit leading-"-" rejection (SEC-06). The class has no ":" in it, so the ref can
	// never carry a second path into the <ref>:<path> join below.
	guarded, ok := guardRef(ref)
	if !ok {
		return errorResult(call.ID, "invalid ref: "+ref), nil
	}
	if strings.TrimSpace(args.Path) == "" {
		return errorResult(call.ID, "path is required"), nil
	}
	rel, err := workspacePathspec(args.Path, t.root)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}
	if rel == "." {
		return errorResult(call.ID, "git_show: path must name a file inside the workspace, not the workspace root"), nil
	}

	// The object is the composed `<ref>:./<rel>` argument, not a bare ref or a pathspec: the
	// `./` prefix makes git resolve the path against the process's cwd (the workspace root)
	// rather than the repository root. --no-textconv matters here — `git show <ref>:<path>`
	// would otherwise run the repository's textconv driver on the blob.
	res, text, ok, err := gitRead(ctx, t.root, t.host.look, gitReadCall{
		verb:          "show",
		diffProducing: true,
		object:        string(guarded) + ":./" + rel,
		failWording:   "git show failed",
	})
	if err != nil {
		return domain.ToolResult{}, err
	}
	if !ok {
		return errorResult(call.ID, fmt.Sprintf("git_show: cannot read %s at %s: %s", rel, ref, text)), nil
	}
	if looksBinary([]byte(res.CombinedOutput)) {
		return errorResult(call.ID, fmt.Sprintf("git_show: %s at %s is a binary file (%d bytes)",
			rel, ref, len(res.CombinedOutput))), nil
	}

	text, span, rangeFailure := renderFile(rel+" @ "+ref, res.CombinedOutput, args.readFileArgs)
	if rangeFailure != "" {
		return errorResult(call.ID, rangeFailure), nil
	}
	return okSummary(call.ID, text, span), nil
}

var (
	_ domain.Tool           = (*GitBranch)(nil)
	_ domain.SubprocessTool = (*GitBranch)(nil)
	_ domain.Tool           = (*GitCommit)(nil)
	_ domain.SubprocessTool = (*GitCommit)(nil)
	_ domain.Tool           = (*GitDiffRange)(nil)
	_ domain.ReadOnlyTool   = (*GitDiffRange)(nil)
	_ domain.SubprocessTool = (*GitDiffRange)(nil)
	_ readOnlySubprocess    = (*GitDiffRange)(nil)
	_ domain.Tool           = (*GitStatus)(nil)
	_ domain.ReadOnlyTool   = (*GitStatus)(nil)
	_ domain.SubprocessTool = (*GitStatus)(nil)
	_ readOnlySubprocess    = (*GitStatus)(nil)
	_ domain.Tool           = (*GitLog)(nil)
	_ domain.ReadOnlyTool   = (*GitLog)(nil)
	_ domain.SubprocessTool = (*GitLog)(nil)
	_ readOnlySubprocess    = (*GitLog)(nil)
	_ domain.Tool           = (*GitShow)(nil)
	_ domain.ReadOnlyTool   = (*GitShow)(nil)
	_ domain.SubprocessTool = (*GitShow)(nil)
	_ readOnlySubprocess    = (*GitShow)(nil)
)
