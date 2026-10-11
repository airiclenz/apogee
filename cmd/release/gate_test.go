package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// The commits of the test history, oldest first. Each id is longer than a short id, so a test
// can tell the full id in the report from the short one in a message.
const (
	rootCommit    = "1000000000000000000000000000000000000000"
	featureCommit = "2000000000000000000000000000000000000000"
	bumpCommit    = "3000000000000000000000000000000000000000"
	rollupCommit  = "4000000000000000000000000000000000000000"
	sideCommit    = "5000000000000000000000000000000000000000"
	mergeCommit   = "6000000000000000000000000000000000000000"
)

// The `git diff-tree -r --name-only <parent> <commit>` outputs the test commits answer with.
// bumpPaths is copied from real output: `git diff-tree -r --name-only 5b3ed35c 58e85387` over
// this repository's micro-bump to v0.24.11 prints exactly this.
const (
	bumpPaths    = "VERSION\n"
	rollupPaths  = "CHANGELOG.md\nVERSION\n"
	featurePaths = "cmd/release/dist.go\ncmd/release/dist_test.go\n"
	mixedPaths   = "CHANGELOG.md\nREADME.md\n"
)

// fakeCommit is one commit of a fakeRepo: its parents, and the paths it changes against its
// first parent.
type fakeCommit struct {
	parents []string
	changed string
}

// fakeRepo answers the gate's and the baseline's commands the way git and gh would over a
// checkout whose history is commits, and hands the publish-step probes' commands to remote.
type fakeRepo struct {
	commits    map[string]fakeCommit
	head       string
	originMain string
	isDirty    bool
	// greenRuns are the commits ci.yml has a successful run for.
	greenRuns []string
	// failing names a command — "git rev-list" — that fails outright, as one that cannot run.
	failing string
	// release is what `gh release view` prints; tags is what `git tag --list` prints.
	release string
	tags    string
	// remote is where the publish channels stand; its zero value has published nothing.
	remote fakeRemote
}

// linearHistory is root ← feature ← bump ← rollup, each commit with its own path set.
func linearHistory() map[string]fakeCommit {
	return map[string]fakeCommit{
		rootCommit:    {},
		featureCommit: {parents: []string{rootCommit}, changed: featurePaths},
		bumpCommit:    {parents: []string{featureCommit}, changed: bumpPaths},
		rollupCommit:  {parents: []string{bumpCommit}, changed: rollupPaths},
	}
}

// exitStatus is a failed command that ran and exited with this status, as exec.ExitError
// reports one.
type exitStatus int

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
func (e exitStatus) ExitCode() int { return int(e) }

// runner returns a recording runner over the repo.
func (r fakeRepo) runner() *fakeRunner {
	return &fakeRunner{respond: r.respond}
}

// respond answers one command.
func (r fakeRepo) respond(call recordedCall) (string, error) {
	command := call.name + " " + strings.Join(call.args, " ")
	if r.failing != "" && strings.HasPrefix(command, r.failing) {
		return "", fmt.Errorf("%s: cannot run", r.failing)
	}
	args := call.args
	switch {
	case command == "git fetch --quiet origin main --tags":
		return "", nil
	case command == "git status --porcelain":
		if r.isDirty {
			return " M internal/tools/registry.go\n", nil
		}
		return "", nil
	case command == "git rev-parse --verify HEAD^{commit}":
		return r.head + "\n", nil
	case call.name == "git" && len(args) == 4 && args[0] == "rev-list" && args[1] == "--parents":
		commit := args[3]
		return strings.Join(append([]string{commit}, r.commits[commit].parents...), " ") + "\n", nil
	case call.name == "git" && len(args) == 5 && args[0] == "diff-tree":
		commit := args[4]
		if parents := r.commits[commit].parents; len(parents) == 0 || parents[0] != args[3] {
			return "", fmt.Errorf("diff-tree asked for %s against %s, not its first parent", commit, args[3])
		}
		return r.commits[commit].changed, nil
	case call.name == "git" && len(args) == 4 && args[0] == "merge-base" && args[1] == "--is-ancestor":
		if r.reaches(r.resolve(args[3]), r.resolve(args[2])) {
			return "", nil
		}
		return "", exitStatus(1)
	case call.name == "gh" && len(args) > 1 && args[0] == "run" && args[1] == "list":
		if slices.Contains(r.greenRuns, args[slices.Index(args, "--commit")+1]) {
			return `[{"databaseId":38131086119}]` + "\n", nil
		}
		return "[]\n", nil
	case command == "gh release view --json tagName,publishedAt":
		return r.release, nil
	case call.name == "git" && len(args) == 3 && args[0] == "rev-list" && args[1] == "--count":
		return "7\n", nil
	case command == "git tag --list v*":
		return r.tags, nil
	}
	return r.remote.respond(call)
}

// resolve turns the gate's symbolic names into commits.
func (r fakeRepo) resolve(revision string) string {
	switch revision {
	case "HEAD":
		return r.head
	case "origin/main":
		return r.originMain
	}
	return revision
}

// reaches reports whether target is from or one of its ancestors.
func (r fakeRepo) reaches(from, target string) bool {
	if from == target {
		return true
	}
	return slices.ContainsFunc(r.commits[from].parents, func(parent string) bool {
		return r.reaches(parent, target)
	})
}

// TestGatePeelsOnlyTrailingChangelogAndVersionCommits pins how the gate commit is found: HEAD's
// trailing first-parent commits that touch only CHANGELOG.md and/or VERSION are walked past, the
// walk stops at the first commit touching anything else, and a merge is never walked past.
func TestGatePeelsOnlyTrailingChangelogAndVersionCommits(t *testing.T) {
	t.Parallel()

	mixedHistory := linearHistory()
	mixedHistory[sideCommit] = fakeCommit{parents: []string{bumpCommit}, changed: mixedPaths}
	mixedHistory[rollupCommit] = fakeCommit{parents: []string{sideCommit}, changed: rollupPaths}

	mergeHistory := linearHistory()
	mergeHistory[sideCommit] = fakeCommit{parents: []string{featureCommit}, changed: featurePaths}
	mergeHistory[mergeCommit] = fakeCommit{parents: []string{bumpCommit, sideCommit}, changed: bumpPaths}

	cases := map[string]struct {
		commits map[string]fakeCommit
		head    string
		want    string
	}{
		"a feature commit at HEAD is the gate":   {commits: linearHistory(), head: featureCommit, want: featureCommit},
		"a VERSION-only bump is peeled":          {commits: linearHistory(), head: bumpCommit, want: featureCommit},
		"a rollup and a bump are both peeled":    {commits: linearHistory(), head: rollupCommit, want: featureCommit},
		"the peel stops at another path":         {commits: mixedHistory, head: rollupCommit, want: sideCommit},
		"a merge at HEAD is never peeled":        {commits: mergeHistory, head: mergeCommit, want: mergeCommit},
		"a root commit at HEAD is the gate":      {commits: linearHistory(), head: rootCommit, want: rootCommit},
		"a bump onto the root stops at the root": {commits: map[string]fakeCommit{rootCommit: {}, bumpCommit: {parents: []string{rootCommit}, changed: bumpPaths}}, head: bumpCommit, want: rootCommit},
		"an empty commit is not peeled":          {commits: map[string]fakeCommit{rootCommit: {}, bumpCommit: {parents: []string{rootCommit}}}, head: bumpCommit, want: bumpCommit},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := fakeRepo{commits: tc.commits, head: tc.head, originMain: tc.head, greenRuns: []string{tc.want}}
			commands := repo.runner()

			report := checkGate(t.Context(), commands, ".")

			if report.Commit != tc.want {
				t.Errorf("gate commit = %s, want %s", report.Commit, tc.want)
			}
			if !report.IsOK {
				t.Errorf("gate blocked: %v", report.Failures)
			}
			for _, call := range commands.recorded("git") {
				if call.args[0] != "diff-tree" {
					continue
				}
				if call.args[1] != "-r" || call.args[2] != "--name-only" {
					t.Errorf("diff-tree called as %v, want the two-tree -r --name-only form", call.args)
				}
				if call.args[4] == mergeCommit {
					t.Errorf("diff-tree was asked about the merge commit: %v", call.args)
				}
			}
		})
	}
}

// TestGateNamesEachFailingCondition pins every way the gate blocks — each named in its own
// failure — and the CI condition's widening: a green run on a peeled commit origin/main contains
// clears it in place of one on the gate commit, since a push runs CI on its head alone.
func TestGateNamesEachFailingCondition(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		repo         fakeRepo
		wantCommit   string
		wantFailures []string
	}{
		"green on the gate commit clears": {
			repo:       fakeRepo{head: bumpCommit, originMain: bumpCommit, greenRuns: []string{featureCommit}},
			wantCommit: featureCommit,
		},
		"green run on the peeled bump, none on G clears": {
			repo:       fakeRepo{head: bumpCommit, originMain: bumpCommit, greenRuns: []string{bumpCommit}},
			wantCommit: featureCommit,
		},
		"green run on a peeled commit origin/main lacks does not clear": {
			repo:         fakeRepo{head: rollupCommit, originMain: bumpCommit, greenRuns: []string{rollupCommit}},
			wantCommit:   featureCommit,
			wantFailures: []string{"ci.yml has no successful run for 200000000000 or 300000000000"},
		},
		"dirty tree": {
			repo:         fakeRepo{head: featureCommit, originMain: featureCommit, isDirty: true, greenRuns: []string{featureCommit}},
			wantCommit:   featureCommit,
			wantFailures: []string{"the working tree is not clean"},
		},
		"gate commit not pushed": {
			repo:       fakeRepo{head: bumpCommit, originMain: rootCommit, greenRuns: []string{featureCommit}},
			wantCommit: featureCommit,
			wantFailures: []string{
				"gate commit 200000000000 is not on origin/main: push it first",
			},
		},
		"origin/main ahead of HEAD": {
			repo:         fakeRepo{head: featureCommit, originMain: bumpCommit, greenRuns: []string{featureCommit}},
			wantCommit:   featureCommit,
			wantFailures: []string{"origin/main has commits HEAD does not: pull first"},
		},
		"no green run": {
			repo:         fakeRepo{head: featureCommit, originMain: featureCommit},
			wantCommit:   featureCommit,
			wantFailures: []string{"ci.yml has no successful run for 200000000000"},
		},
		"every condition at once": {
			repo:       fakeRepo{head: featureCommit, originMain: rootCommit, isDirty: true},
			wantCommit: featureCommit,
			wantFailures: []string{
				"the working tree is not clean",
				"gate commit 200000000000 is not on origin/main: push it first",
				"ci.yml has no successful run for 200000000000",
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := tc.repo
			repo.commits = linearHistory()

			report := checkGate(t.Context(), repo.runner(), ".")

			if report.Commit != tc.wantCommit {
				t.Errorf("gate commit = %s, want %s", report.Commit, tc.wantCommit)
			}
			wantFailures := tc.wantFailures
			if wantFailures == nil {
				wantFailures = []string{}
			}
			if !slices.Equal(report.Failures, wantFailures) {
				t.Errorf("failures = %q, want %q", report.Failures, wantFailures)
			}
			if report.IsOK != (len(wantFailures) == 0) {
				t.Errorf("ok = %t with failures %q", report.IsOK, report.Failures)
			}
		})
	}
}

// TestGateNamesACheckThatCannotRun pins that a check the gate cannot run blocks it with the
// cause named, never passes it by default.
func TestGateNamesACheckThatCannotRun(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		failing string
		want    string
	}{
		"fetch":        {failing: "git fetch", want: "could not fetch origin"},
		"status":       {failing: "git status", want: "could not read the working tree's status"},
		"head":         {failing: "git rev-parse", want: "could not resolve HEAD"},
		"peel":         {failing: "git rev-list --parents", want: "could not find the gate commit"},
		"ancestry":     {failing: "git merge-base", want: "could not check whether 200000000000 is on origin/main"},
		"ci runs (gh)": {failing: "gh run list", want: "could not read the ci.yml runs for 200000000000"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := fakeRepo{commits: linearHistory(), head: bumpCommit, originMain: bumpCommit,
				greenRuns: []string{featureCommit}, failing: tc.failing}

			report := checkGate(t.Context(), repo.runner(), ".")

			if report.IsOK {
				t.Fatal("gate cleared although a check could not run")
			}
			if !slices.ContainsFunc(report.Failures, func(failure string) bool {
				return strings.HasPrefix(failure, tc.want)
			}) {
				t.Errorf("failures = %q, want one starting %q", report.Failures, tc.want)
			}
		})
	}
}
