package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// The names the gate reads remote state through.
const (
	remoteName   = "origin"
	mainBranch   = "main"
	remoteMain   = remoteName + "/" + mainBranch
	ciWorkflow   = "ci.yml"
	shortSHASize = 12
)

// peelablePaths are the only files a commit may touch for the gate to look past it: the two a
// cut's own commits write (the changelog rollup and the VERSION bump), and the two every
// per-feature micro-bump writes.
var peelablePaths = []string{"CHANGELOG.md", versionFile}

// gateReport is the CI gate's verdict: whether a cut may proceed, the commit it was judged on,
// and every condition that blocks it, each named.
type gateReport struct {
	IsOK     bool     `json:"ok"`
	Commit   string   `json:"commit"`
	Failures []string `json:"failures"`
}

// checkGate judges the release gate over the checkout in dir. The gate commit G is HEAD with its
// trailing first-parent commits that touch only CHANGELOG.md and/or VERSION peeled off. The gate
// clears when the tree is clean, G is on origin/main, origin/main is contained in HEAD, and
// ci.yml has a successful run for G or for a peeled commit origin/main contains — a push runs CI
// on its head alone, so a feature pushed with its micro-bump leaves the green run on the bump.
// A check that cannot run is a failure too, named with its cause; the gate has no bypass.
func checkGate(ctx context.Context, commands runner, dir string) gateReport {
	report := gateReport{Failures: []string{}}
	fail := func(format string, args ...any) {
		report.Failures = append(report.Failures, fmt.Sprintf(format, args...))
	}

	if _, err := commands.run(ctx, dir, nil, "git", "fetch", "--quiet", remoteName, mainBranch, "--tags"); err != nil {
		fail("could not fetch %s: %v", remoteName, err)
	}

	status, err := commands.run(ctx, dir, nil, "git", "status", "--porcelain")
	switch {
	case err != nil:
		fail("could not read the working tree's status: %v", err)
	case strings.TrimSpace(status) != "":
		fail("the working tree is not clean")
	}

	head, err := revParse(ctx, commands, dir, "HEAD")
	if err != nil {
		fail("could not resolve HEAD: %v", err)
		return report
	}
	gate, peeled, err := peelBumps(ctx, commands, dir, head)
	if err != nil {
		fail("could not find the gate commit: %v", err)
		return report
	}
	report.Commit = gate

	isPushed, err := isAncestor(ctx, commands, dir, gate, remoteMain)
	switch {
	case err != nil:
		fail("could not check whether %s is on %s: %v", short(gate), remoteMain, err)
	case !isPushed:
		fail("gate commit %s is not on %s: push it first", short(gate), remoteMain)
	}

	isCurrent, err := isAncestor(ctx, commands, dir, remoteMain, "HEAD")
	switch {
	case err != nil:
		fail("could not check whether HEAD contains %s: %v", remoteMain, err)
	case !isCurrent:
		fail("%s has commits HEAD does not: pull first", remoteMain)
	}

	if err := checkGreenRun(ctx, commands, dir, gate, peeled); err != nil {
		fail("%v", err)
	}

	report.IsOK = len(report.Failures) == 0
	return report
}

// peelBumps walks HEAD's first-parent line back past every commit whose own changes touch only
// peelablePaths, and returns the first commit it stops at (the gate commit) and the commits it
// walked past, newest first. It stops at any commit that is not an ordinary one-parent commit — a
// merge is never peeled — and at one that touches nothing at all.
func peelBumps(ctx context.Context, commands runner, dir, head string) (string, []string, error) {
	var peeled []string
	commit := head
	for {
		parents, err := commands.run(ctx, dir, nil, "git", "rev-list", "--parents", "-n1", commit)
		if err != nil {
			return "", nil, fmt.Errorf("read the parents of %s: %w", short(commit), err)
		}
		// rev-list --parents prints the commit followed by each of its parents.
		fields := strings.Fields(parents)
		if len(fields) != 2 {
			return commit, peeled, nil
		}
		parent := fields[1]

		// The two-tree form lists the paths alone; the one-commit form would print the commit id
		// first, and nothing at all for a merge.
		changed, err := commands.run(ctx, dir, nil, "git", "diff-tree", "-r", "--name-only", parent, commit)
		if err != nil {
			return "", nil, fmt.Errorf("list the paths %s changes: %w", short(commit), err)
		}
		if !touchesOnlyPeelablePaths(changed) {
			return commit, peeled, nil
		}
		peeled = append(peeled, commit)
		commit = parent
	}
}

// touchesOnlyPeelablePaths reports whether the diff-tree path list names at least one path and
// nothing outside peelablePaths.
func touchesOnlyPeelablePaths(changed string) bool {
	paths := strings.Fields(changed)
	if len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		if !slices.Contains(peelablePaths, path) {
			return false
		}
	}
	return true
}

// checkGreenRun returns nil when ci.yml has a successful run for the gate commit, or for one of
// the peeled commits that origin/main contains — the only ones a push could have run CI on in
// its place. Otherwise it returns the failure that names what was looked for.
func checkGreenRun(ctx context.Context, commands runner, dir, gate string, peeled []string) error {
	candidates := []string{gate}
	for _, commit := range peeled {
		isPushed, err := isAncestor(ctx, commands, dir, commit, remoteMain)
		if err != nil {
			return fmt.Errorf("could not check whether %s is on %s: %w", short(commit), remoteMain, err)
		}
		if isPushed {
			candidates = append(candidates, commit)
		}
	}

	for _, commit := range candidates {
		isGreen, err := hasGreenRun(ctx, commands, dir, commit)
		if err != nil {
			return fmt.Errorf("could not read the %s runs for %s: %w", ciWorkflow, short(commit), err)
		}
		if isGreen {
			return nil
		}
	}

	shortCandidates := make([]string, 0, len(candidates))
	for _, commit := range candidates {
		shortCandidates = append(shortCandidates, short(commit))
	}
	return fmt.Errorf("%s has no successful run for %s", ciWorkflow, strings.Join(shortCandidates, " or "))
}

// hasGreenRun reports whether ci.yml has at least one successful run for commit.
func hasGreenRun(ctx context.Context, commands runner, dir, commit string) (bool, error) {
	output, err := commands.run(ctx, dir, nil, "gh", "run", "list",
		"--workflow", ciWorkflow, "--commit", commit, "--status", "success", "--json", "databaseId")
	if err != nil {
		return false, err
	}
	var runs []struct {
		DatabaseID int64 `json:"databaseId"`
	}
	if err := json.Unmarshal([]byte(output), &runs); err != nil {
		return false, fmt.Errorf("parse gh run list output: %w", err)
	}
	return len(runs) > 0, nil
}

// revParse resolves a revision to its full commit id.
func revParse(ctx context.Context, commands runner, dir, revision string) (string, error) {
	output, err := commands.run(ctx, dir, nil, "git", "rev-parse", "--verify", revision+"^{commit}")
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(output)
	if commit == "" {
		return "", fmt.Errorf("git rev-parse printed no commit for %s", revision)
	}
	return commit, nil
}

// exitCoder is what a failed command's error carries when the command ran and exited non-zero
// (exec.ExitError has it), so a caller can tell a "no" answer from a command that broke.
type exitCoder interface {
	ExitCode() int
}

// isAncestor reports whether ancestor is descendant or one of its ancestors. `git merge-base
// --is-ancestor` answers with its exit status: 0 yes, 1 no, anything else a failure.
func isAncestor(ctx context.Context, commands runner, dir, ancestor, descendant string) (bool, error) {
	_, err := commands.run(ctx, dir, nil, "git", "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	var exit exitCoder
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

// short abbreviates a commit id for a message; the full id stays in the report's commit field.
func short(commit string) string {
	if len(commit) <= shortSHASize {
		return commit
	}
	return commit[:shortSHASize]
}
