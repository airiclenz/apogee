package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/gitexec"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tools"
)

// The commit-secrets pre-check: before a git_commit resolves, dispatch stages what the call
// would stage into a SHADOW index, diffs it, and hands the pure scanner (security.SecretFindings)
// the staged diff and the staged paths. A finding tightens the guardrail verdict to a Tier-2
// forced approval in every mode (ADR 0012's tighten-only rule; ADR 0080) — the human sees the
// finding on the prompt's Fix row, a denied look answers the model with the same words, and
// approval is the only way through. It is dispatch-side I/O precompute in the resolution.go D6
// sense: resolve() stays pure and reads the result as one more guard fact.
//
// Robustness contract (binding, the treesnapshot.go shape): each git run carries
// commitSecretsTimeout and executes in the workspace root; on ANY git failure — git absent, a
// fenced or refused git, not a repository, a timeout, a path the tool itself would refuse — the
// check is skipped silently for that call (ADR 0056 decision 4) and the text guard's verdict
// stands. The pre-check must never fail a commit the tool would have made.
//
// The shadow is what keeps the pre-check side-effect free on the real repository: the staged
// state is a COPY of the index (GIT_INDEX_FILE), and the blobs `git add` writes for the call's
// files land in a private object directory (GIT_OBJECT_DIRECTORY) that reads the real one
// through GIT_ALTERNATE_OBJECT_DIRECTORIES — so a denied commit leaves no secret blob in the
// real object store, and the real index is never touched before the tool's own `git add`.

// commitSecretsTimeout bounds each git invocation the pre-check makes, so a wedged git can
// never hold a commit's resolution; on expiry the check is skipped for that call.
const commitSecretsTimeout = 2 * time.Second

// shadowDirPattern names the per-call temp dir the shadow index and object directory live in.
const shadowDirPattern = "apogee-commit-secrets-*"

// shadowGitQuery is the funnel every shadow run goes through — gitexec.Query, so the runs carry
// the git tools' hardening, environment allowlist and command-config refusal. It is a package
// var so a test can count the git children a mode spawns (Plan mode must spawn none).
var shadowGitQuery = gitexec.Query

// gitCommitFiles is the one git_commit argument the pre-check reads: the files the tool would
// stage before committing. Everything else the tool validates itself.
type gitCommitFiles struct {
	Files []string `json:"files"`
}

// tightenForStagedSecrets returns the guard verdict prepareCall hands resolve() for call: the
// text guard's own PreCheck, or — for a git_commit it let proceed outside Plan mode — the
// commit-secrets pre-check's forced approval when the staged diff carries secret material. The
// guard is tighten-only: a stricter text verdict (a Tier-1 refuse or a Tier-2 force from the
// call's own arguments) stands unexamined, and Plan mode — where resolve() refuses every write
// leaf and applyOverlays ignores the overlay — spawns no git child at all.
func (a *Agent) tightenForStagedSecrets(ctx context.Context, call domain.ToolCall, guard security.PreCheck) security.PreCheck {
	if call.Tool != tools.GitCommitToolName || guard.Outcome != security.GuardProceed {
		return guard
	}
	if a.effectiveMode() == domain.ModePlan {
		return guard
	}
	if secrets, found := a.commitSecretsCheck(ctx, call); found {
		return secrets
	}
	return guard
}

// commitSecretsCheck runs the shadow staging for one git_commit call and reports the forced
// approval its findings warrant: ok is true only when the staged diff or the staged paths carry
// secret material, and the PreCheck then names the rule (security.SecretsRuleID) with the
// classes found as its Reason, the scanner's Hint as its Hint, and the Tier-2 audit decision.
// A clean diff, a non-repository root, a git failure of any kind, or a `files` entry the tool
// itself would refuse all return (PreCheck{}, false): nothing to tighten with.
func (a *Agent) commitSecretsCheck(ctx context.Context, call domain.ToolCall) (security.PreCheck, bool) {
	root := a.cfg.WorkspaceDir
	if root == "" {
		return security.PreCheck{}, false
	}
	var args gitCommitFiles
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return security.PreCheck{}, false
		}
	}
	pathspecs, err := tools.CommitPathspecs(args.Files, root)
	if err != nil {
		return security.PreCheck{}, false
	}

	gitPath, err := gitexec.Resolve(ctx, root, nil)
	if err != nil {
		return security.PreCheck{}, false
	}
	shadow, err := newShadowIndex(ctx, gitPath, root)
	if err != nil {
		return security.PreCheck{}, false
	}
	defer shadow.remove()

	if len(pathspecs) > 0 {
		if _, err := shadow.git(ctx, append([]string{"add", "--"}, pathspecs...)...); err != nil {
			return security.PreCheck{}, false
		}
	}
	diff, err := shadow.git(ctx, "diff", "--cached", "--diff-filter=ACMR", "--no-textconv", "--no-ext-diff", "--no-color")
	if err != nil {
		return security.PreCheck{}, false
	}
	names, err := shadow.git(ctx, "diff", "--cached", "--diff-filter=ACMR", "--name-only")
	if err != nil {
		return security.PreCheck{}, false
	}

	findings := security.SecretFindings(diff, nonEmptyLines(names))
	if len(findings) == 0 {
		return security.PreCheck{}, false
	}
	return security.PreCheck{
		Outcome: security.GuardForceApproval,
		Reason:  security.SecretsRuleID + ": " + strings.Join(findingClasses(findings), ", "),
		Hint:    security.SecretsHint(findings),
		Audit:   security.AuditDangerousForceApproval,
	}, true
}

// shadowIndex is one call's private staging area: a temp dir holding a copy of the repository's
// index (or no index at all, for a repository that has never staged anything) and an object
// directory of its own, plus the environment that points git at both.
type shadowIndex struct {
	gitPath string
	root    string
	dir     string
	env     []string
}

// newShadowIndex prepares the shadow for root: it asks git where the real index and object
// directory live (`rev-parse --git-path`, which resolves a linked worktree's `.git` FILE to
// its own index), copies the index into the temp dir when one exists, and builds the env.
// A missing index is left MISSING — GIT_INDEX_FILE names a path that does not exist yet, which
// git treats as an empty index; an empty FILE would make every run fail with "index file
// smaller than expected". The temp object directory must exist before git will accept it as a
// repository, so it is created here; the alternate is the real object store, read-only.
func newShadowIndex(ctx context.Context, gitPath, root string) (*shadowIndex, error) {
	paths, err := runShadowGit(ctx, gitPath, root, nil,
		"rev-parse", "--git-path", "index", "--git-path", "objects")
	if err != nil {
		return nil, err
	}
	lines := nonEmptyLines(paths)
	if len(lines) != 2 {
		return nil, errGitPathShape
	}
	realIndex, realObjects := absoluteIn(root, lines[0]), absoluteIn(root, lines[1])

	dir, err := os.MkdirTemp("", shadowDirPattern)
	if err != nil {
		return nil, err
	}
	s := &shadowIndex{gitPath: gitPath, root: root, dir: dir}
	index := filepath.Join(dir, "index")
	objects := filepath.Join(dir, "objects")
	if err := os.Mkdir(objects, 0o700); err != nil {
		s.remove()
		return nil, err
	}
	if err := copyFileIfExists(realIndex, index); err != nil {
		s.remove()
		return nil, err
	}
	s.env = []string{
		"GIT_INDEX_FILE=" + index,
		"GIT_OBJECT_DIRECTORY=" + objects,
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=" + realObjects,
	}
	return s, nil
}

// errGitPathShape is the failure when `rev-parse --git-path` answers with anything but the two
// paths asked for — treated like any other git failure, a silent skip.
var errGitPathShape = errors.New("apogee: commit-secrets: unexpected rev-parse --git-path output")

// git runs one git command in the workspace root against the shadow, returning stdout. Every
// error is the caller's signal to skip.
func (s *shadowIndex) git(ctx context.Context, args ...string) (string, error) {
	return runShadowGit(ctx, s.gitPath, s.root, s.env, args...)
}

// runShadowGit runs one git command in root under the pre-check's timeout, through the shadow
// funnel. ctx is the call's, so a cancelled Turn skips the check; the per-run timeout stays the
// outer bound as well as the funnel's, since a ctx that is never cancelled must still not let
// one wedged git hold a commit's resolution.
func runShadowGit(ctx context.Context, gitPath, root string, env []string, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, commitSecretsTimeout)
	defer cancel()
	return shadowGitQuery(runCtx, gitPath, root, env, commitSecretsTimeout, args...)
}

// remove deletes the shadow's temp dir — index, object directory and all — on every path.
func (s *shadowIndex) remove() {
	_ = os.RemoveAll(s.dir)
}

// absoluteIn makes a path git printed relative to the repository root absolute; a path git
// already printed absolute (a linked worktree's index) is returned as is.
func absoluteIn(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, p)
}

// copyFileIfExists copies src to dst when src exists and does nothing when it does not; any
// other read or write failure is returned.
func copyFileIfExists(src, dst string) error {
	data, err := os.ReadFile(src)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}

// nonEmptyLines splits git's newline-terminated output into its non-empty lines.
func nonEmptyLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// findingClasses lists the distinct classes among findings in first-seen order — the noun
// list the guard's Reason names, so the audit trail reads `commit-secrets: private key,
// secret-bearing file name` rather than one row per path.
func findingClasses(findings []security.SecretFinding) []string {
	var (
		classes []string
		seen    = map[string]bool{}
	)
	for _, f := range findings {
		if seen[f.Class] {
			continue
		}
		seen[f.Class] = true
		classes = append(classes, f.Class)
	}
	return classes
}
