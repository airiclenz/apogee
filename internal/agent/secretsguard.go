package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
// Robustness contract (binding): the whole check runs in the workspace root under ONE
// commitSecretsTimeout budget, and it has three outcomes rather than two. A root that resolves
// no repository to scan — git absent, fenced or refused, not a repository, a path the tool
// itself would refuse — is still the silent skip: there is nothing to look at, and the text
// guard's verdict stands. But a scan that RESOLVED a repository and then could not finish — the
// budget expired, or a git run failed mid-scan — no longer poses as a clean one: it forces the
// approval look with incompleteScanHint, so a wedged or throttled git degrades the commit to a
// human decision instead of waving the staged bytes through unexamined. That supersedes ADR 0080
// decision 6, whose silent skip on ANY git failure made a security control that did not run
// indistinguishable from one that ran clean, and narrows ADR 0056 decision 4, whose silent skip
// still governs the tree-mutation snapshot (treeSnapshotTimeout) but no longer this pre-check
// (both ADRs carry the 2026-09-23 amendment). The pre-check still never
// FAILS a commit the tool would have made — the worst it does is ask.
//
// The shadow is what keeps the pre-check side-effect free on the real repository: the staged
// state is a COPY of the index (GIT_INDEX_FILE), and the blobs `git add` writes for the call's
// files land in a private object directory (GIT_OBJECT_DIRECTORY) that reads the real one
// through GIT_ALTERNATE_OBJECT_DIRECTORIES — so a denied commit leaves no secret blob in the
// real object store, and the real index is never touched before the tool's own `git add`.

// commitSecretsTimeout bounds the WHOLE pre-check — one context covers all four shadow runs —
// so a wedged git can never hold a commit's resolution beyond it. It is 30 s because the box
// apogee is built for is a loaded local machine where a cold git over a large index is slow
// rather than broken, and a healthy repository answers in milliseconds and spends none of it. On
// expiry the scan is incomplete, not clean, and the call is forced to the approval look (see the
// robustness contract above). It is a package var so a test can lower it.
var commitSecretsTimeout = 30 * time.Second

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

// incompleteScanReason is the guard Reason an incomplete scan records in the audit trail: the
// rule fired on the scan's own failure rather than on a finding, so the trail says which of the
// two happened.
const incompleteScanReason = security.SecretsRuleID + ": the staged-secret scan did not finish"

// incompleteScanHint is the Fix row the human reads on the prompt and the words a denied call
// answers the model with — the exact string the Approver receives. It names what was NOT checked
// rather than claiming a finding, because an incomplete scan knows nothing about the staged
// bytes either way.
const incompleteScanHint = "the staged-secret scan could not finish, so the staged content is unchecked — retry the commit, or approve to commit without the scan"

// errNothingToScan marks the failures that still skip the pre-check silently: no git to run, or
// a workspace root that is no repository, so the scan never had anything to look at.
var errNothingToScan = errors.New("apogee: commit-secrets: no repository to scan")

// errScanTimedOut marks a shadow run that the check's own budget cut short — an incomplete scan,
// not a clean one.
var errScanTimedOut = errors.New("apogee: commit-secrets: the staged-secret scan ran out of time")

// commitSecretsCheck runs the shadow staging for one git_commit call and reports the forced
// approval it warrants. ok is true in two cases: the staged diff or the staged paths carry
// secret material — the PreCheck then names the rule (security.SecretsRuleID) with the classes
// found as its Reason and the scanner's Hint as its Hint — or the scan could not finish, where
// the PreCheck carries incompleteScanReason and incompleteScanHint instead. Both take the Tier-2
// audit decision. A clean scan, a root with no repository to scan, and a `files` entry the tool
// itself would refuse all return (security.PreCheck{}, false): nothing to tighten with.
//
// The one context taken here is what makes commitSecretsTimeout the whole check's ceiling rather
// than each run's, and this is the single place the three outcomes are decided — callers of
// tightenForStagedSecrets read the verdict, never the classification.
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

	scanCtx, cancel := context.WithTimeout(ctx, commitSecretsTimeout)
	defer cancel()

	findings, err := scanStagedSecrets(scanCtx, root, pathspecs)
	switch {
	case errors.Is(err, errNothingToScan):
		return security.PreCheck{}, false
	case err != nil:
		return security.PreCheck{
			Outcome: security.GuardForceApproval,
			Reason:  incompleteScanReason,
			Hint:    incompleteScanHint,
			Audit:   security.AuditDangerousForceApproval,
		}, true
	case len(findings) == 0:
		return security.PreCheck{}, false
	}
	return security.PreCheck{
		Outcome: security.GuardForceApproval,
		Reason:  security.SecretsRuleID + ": " + strings.Join(findingClasses(findings), ", "),
		Hint:    security.SecretsHint(findings),
		Audit:   security.AuditDangerousForceApproval,
	}, true
}

// scanStagedSecrets stages pathspecs into a shadow index for root and scans what the commit
// would carry. The three outcomes are spelled in the return: a nil error carries the findings of
// a scan that RAN (an empty slice is a clean tree); errNothingToScan says there was nothing to
// scan; any other error says the scan started against a resolved repository and could not
// finish, which the caller turns into the forced look.
func scanStagedSecrets(ctx context.Context, root string, pathspecs []string) ([]security.SecretFinding, error) {
	gitPath, err := gitexec.Resolve(ctx, root, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errNothingToScan, err)
	}
	shadow, err := newShadowIndex(ctx, gitPath, root)
	if err != nil {
		return nil, err
	}
	defer shadow.remove()

	if len(pathspecs) > 0 {
		if _, err := shadow.git(ctx, append([]string{"add", "--"}, pathspecs...)...); err != nil {
			return nil, err
		}
	}
	diff, err := shadow.git(ctx, "diff", "--cached", "--diff-filter=ACMR", "--no-textconv", "--no-ext-diff", "--no-color")
	if err != nil {
		return nil, err
	}
	names, err := shadow.git(ctx, "diff", "--cached", "--diff-filter=ACMR", "--name-only")
	if err != nil {
		return nil, err
	}
	return security.SecretFindings(diff, nonEmptyLines(names)), nil
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
		// This run IS the repository resolution, so git refusing it means there was nothing
		// to scan — except when the budget cut it short, which is an incomplete scan of a
		// repository that may well exist.
		if errors.Is(err, errScanTimedOut) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %v", errNothingToScan, err)
	}
	lines := nonEmptyLines(paths)
	if len(lines) != 2 {
		return nil, fmt.Errorf("%w: %v", errNothingToScan, errGitPathShape)
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
// paths asked for — a git that cannot say where its index is leaves nothing to scan, so it keeps
// the silent skip.
var errGitPathShape = errors.New("apogee: commit-secrets: unexpected rev-parse --git-path output")

// git runs one git command in the workspace root against the shadow, returning stdout. Every
// error stops the scan: the repository resolved, so scanStagedSecrets hands the failure on as an
// incomplete scan rather than as a clean one.
func (s *shadowIndex) git(ctx context.Context, args ...string) (string, error) {
	return runShadowGit(ctx, s.gitPath, s.root, s.env, args...)
}

// runShadowGit runs one git command in root through the shadow funnel. ctx carries the check's
// single budget (commitSecretsTimeout, taken once in commitSecretsCheck), so the four runs share
// one ceiling and a cancelled Turn stops the check; the same duration goes to the funnel as the
// per-run timeout, which is what bounds a run whose ctx outlives it.
//
// The classification lives here: gitexec renders a run its own timeout killed as a plain
// `git …: timed out after …` error and never wraps context.DeadlineExceeded, so the budget's
// expiry is read from ctx AFTER the call and marked with errScanTimedOut.
func runShadowGit(ctx context.Context, gitPath, root string, env []string, args ...string) (string, error) {
	out, err := shadowGitQuery(ctx, gitPath, root, env, commitSecretsTimeout, args...)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("%w: %v", errScanTimedOut, err)
	}
	return out, err
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
