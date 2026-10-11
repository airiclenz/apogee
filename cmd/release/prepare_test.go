package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// preparedOn is the local date the tests cut their release on.
var preparedOn = time.Date(2026, time.October, 11, 23, 30, 0, 0, time.Local)

// The fixture changelog: an intro, two Unreleased bullets and the release before them, in the
// layout CHANGELOG.md has had since 5b3ed35c cut v0.24.11.
const (
	unreleasedChangelog = "# Changelog\n" +
		"\n" +
		"All notable changes.\n" +
		"\n" +
		"## [Unreleased]\n" +
		"\n" +
		"- **First.** One.\n" +
		"\n" +
		"- **Second.** Two.\n" +
		"\n" +
		"## [0.24.11] — 2026-10-08\n" +
		"\n" +
		"- **Old.** Three.\n"
	rolledUpChangelog = "# Changelog\n" +
		"\n" +
		"All notable changes.\n" +
		"\n" +
		"## [Unreleased]\n" +
		"\n" +
		"## [0.25.0] — 2026-10-11\n" +
		"\n" +
		"- **First.** One.\n" +
		"\n" +
		"- **Second.** Two.\n" +
		"\n" +
		"## [0.24.11] — 2026-10-08\n" +
		"\n" +
		"- **Old.** Three.\n"
)

// The tags and VERSION of the fixture checkout: v0.24.11 is the highest release tag, and the
// retracted v1.x series outranks it numerically but must be set aside.
const (
	preparedTags    = "v0.24.10\nv0.24.11\nv1.0.0\nv1.8.0\n"
	preparedVersion = "v0.24.11\n"
)

// prepareRepo answers prepare's git commands over a checkout in dir and records, at each
// commit, what CHANGELOG.md and VERSION held — what that commit would have recorded.
type prepareRepo struct {
	dir     string
	isDirty bool
	tags    string
	// failing names a command — "git commit" — that fails outright.
	failing string

	commits []committedFiles
}

// committedFiles is the subject of one commit and the two files as they stood when it was made.
type committedFiles struct {
	subject   string
	changelog string
	version   string
}

// newPrepareRepo writes a checkout with the given CHANGELOG.md and VERSION and answers over it.
func newPrepareRepo(t *testing.T, changelog, version string) *prepareRepo {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, changelogFile), changelog)
	writeTestFile(t, filepath.Join(dir, versionFile), version)
	return &prepareRepo{dir: dir, tags: preparedTags}
}

// writeTestFile writes content to path, failing the test when it cannot.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readTestFile returns path's content, failing the test when it cannot.
func readTestFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// runner returns a recording runner over the repo.
func (r *prepareRepo) runner() *fakeRunner {
	return &fakeRunner{respond: r.respond}
}

// respond answers one command.
func (r *prepareRepo) respond(call recordedCall) (string, error) {
	command := call.name + " " + strings.Join(call.args, " ")
	if r.failing != "" && strings.HasPrefix(command, r.failing) {
		return "", errors.New(r.failing + ": cannot run")
	}
	switch {
	case command == "git status --porcelain":
		if r.isDirty {
			return "?? notes.txt\n", nil
		}
		return "", nil
	case command == "git tag --list v*":
		return r.tags, nil
	case call.name == "git" && len(call.args) == 2 && call.args[0] == "add":
		return "", nil
	case call.name == "git" && len(call.args) == 4 && call.args[0] == "commit":
		changelog, changelogErr := os.ReadFile(filepath.Join(r.dir, changelogFile))
		version, versionErr := os.ReadFile(filepath.Join(r.dir, versionFile))
		if err := errors.Join(changelogErr, versionErr); err != nil {
			return "", err
		}
		r.commits = append(r.commits, committedFiles{
			subject: call.args[3], changelog: string(changelog), version: string(version),
		})
		return "", nil
	}
	return "", errors.New("unexpected command: " + command)
}

// prepare runs the prepare verb's body over the repo, as `release prepare <version>` would.
func (r *prepareRepo) prepare(t *testing.T, commands runner, version string) (string, error) {
	t.Helper()
	var output bytes.Buffer
	options := prepareOptions{dir: r.dir, version: version, now: func() time.Time { return preparedOn }}
	err := runPrepare(t.Context(), commands, &output, options)
	return output.String(), err
}

// gitCommands returns every git command the runner was asked for, in order, as one line each.
func gitCommands(commands *fakeRunner) []string {
	calls := commands.recorded("git")
	lines := make([]string, 0, len(calls))
	for _, call := range calls {
		lines = append(lines, strings.Join(call.args, " "))
	}
	return lines
}

// TestPrepareMakesTheRollupThenTheBumpCommit pins the verb's whole effect: two commits with the
// exact subjects, each staging its one path by name, the rollup recording the new heading while
// VERSION still holds the old version, and the bump writing a fresh `vX.Y.Z\n` — with nothing
// pushed. A bare version reads as its `v` form.
func TestPrepareMakesTheRollupThenTheBumpCommit(t *testing.T) {
	t.Parallel()

	for _, typed := range []string{"v0.25.0", "0.25.0"} {
		t.Run(typed, func(t *testing.T) {
			t.Parallel()
			repo := newPrepareRepo(t, unreleasedChangelog, preparedVersion)
			commands := repo.runner()

			output, err := repo.prepare(t, commands, typed)

			if err != nil {
				t.Fatalf("prepare %s: %v", typed, err)
			}
			wantCommands := []string{
				"status --porcelain",
				"tag --list v*",
				"add CHANGELOG.md",
				"commit --quiet -m chore(release): cut v0.25.0",
				"add VERSION",
				"commit --quiet -m chore(version): bump version to v0.25.0",
			}
			if got := gitCommands(commands); !slices.Equal(got, wantCommands) {
				t.Errorf("git commands =\n%q\nwant\n%q", got, wantCommands)
			}
			wantCommits := []committedFiles{
				{subject: "chore(release): cut v0.25.0", changelog: rolledUpChangelog, version: preparedVersion},
				{subject: "chore(version): bump version to v0.25.0", changelog: rolledUpChangelog, version: "v0.25.0\n"},
			}
			if !slices.Equal(repo.commits, wantCommits) {
				t.Errorf("commits =\n%+v\nwant\n%+v", repo.commits, wantCommits)
			}
			if !strings.Contains(output, "nothing was pushed") {
				t.Errorf("output does not say nothing was pushed:\n%s", output)
			}
		})
	}
}

// TestRollUpChangelog pins the rollup's bytes: the heading goes directly under Unreleased with
// one blank line on each side however many blank lines stood there, and a CRLF changelog keeps
// its line ending.
func TestRollUpChangelog(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		changelog string
		want      string
	}{
		"the house layout": {changelog: unreleasedChangelog, want: rolledUpChangelog},
		"no blank line under Unreleased": {
			changelog: "## [Unreleased]\n- **First.** One.\n",
			want:      "## [Unreleased]\n\n## [0.25.0] — 2026-10-11\n\n- **First.** One.\n",
		},
		"several blank lines under Unreleased": {
			changelog: "## [Unreleased]\n\n\n\n- **First.** One.\n",
			want:      "## [Unreleased]\n\n## [0.25.0] — 2026-10-11\n\n- **First.** One.\n",
		},
		"a CRLF changelog": {
			changelog: "# Changelog\r\n\r\n## [Unreleased]\r\n\r\n- **First.** One.\r\n",
			want:      "# Changelog\r\n\r\n## [Unreleased]\r\n\r\n## [0.25.0] — 2026-10-11\r\n\r\n- **First.** One.\r\n",
		},
		"a newer patch beside an older one": {
			changelog: "## [Unreleased]\n\n- **First.** One.\n\n## [0.25.1] — 2026-10-09\n",
			want:      "## [Unreleased]\n\n## [0.25.0] — 2026-10-11\n\n- **First.** One.\n\n## [0.25.1] — 2026-10-09\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := rollUpChangelog([]byte(tc.changelog), releaseVersion{minor: 25}, preparedOn)

			if err != nil {
				t.Fatalf("rollUpChangelog: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("rollup =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

// TestPrepareRefuses pins every refusal: the run fails, the error names the reason, and nothing
// is written, staged or committed.
func TestPrepareRefuses(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		version   string
		changelog string
		versionIn string
		isDirty   bool
		failing   string
		wantError string
	}{
		"a malformed version": {version: "v0.25", wantError: "is not a release version"},
		"a pre-release version": {
			version: "v0.25.0-rc.1", wantError: "is not a release version",
		},
		"the highest tag itself": {
			version: "v0.24.11", wantError: "v0.24.11 is not above the highest release tag v0.24.11",
		},
		"below the highest tag": {
			version: "v0.24.9", wantError: "v0.24.9 is not above the highest release tag v0.24.11",
		},
		"a dirty working tree": {isDirty: true, wantError: "the working tree is not clean"},
		"the tags cannot be listed": {
			failing: "git tag", wantError: "list the release tags",
		},
		"VERSION already reads the version": {
			versionIn: "v0.25.0\n",
			wantError: "VERSION already reads v0.25.0, an unpushed micro-bump: cut the next patch, v0.25.1, instead",
		},
		"no Unreleased heading": {
			changelog: "# Changelog\n\n## [0.24.11] — 2026-10-08\n\n- **Old.** Three.\n",
			wantError: "CHANGELOG.md has no ## [Unreleased] heading",
		},
		"no bullet under Unreleased": {
			changelog: "## [Unreleased]\n\nProse, not a bullet.\n\n## [0.24.11] — 2026-10-08\n\n- **Old.** Three.\n",
			wantError: "CHANGELOG.md has no bullet under ## [Unreleased] to release",
		},
		"an empty Unreleased at the end of the file": {
			changelog: "## [0.24.11] — 2026-10-08\n\n- **Old.** Three.\n\n## [Unreleased]\n\n",
			wantError: "CHANGELOG.md has no bullet under ## [Unreleased] to release",
		},
		"a heading for the version already": {
			changelog: "## [Unreleased]\n\n- **First.** One.\n\n## [0.25.0] — 2026-10-01\n\n- **Old.** Three.\n",
			wantError: "CHANGELOG.md already has a ## [0.25.0] heading",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			changelog := tc.changelog
			if changelog == "" {
				changelog = unreleasedChangelog
			}
			versionIn := tc.versionIn
			if versionIn == "" {
				versionIn = preparedVersion
			}
			version := tc.version
			if version == "" {
				version = "v0.25.0"
			}
			repo := newPrepareRepo(t, changelog, versionIn)
			repo.isDirty = tc.isDirty
			repo.failing = tc.failing
			commands := repo.runner()

			_, err := repo.prepare(t, commands, version)

			if err == nil {
				t.Fatalf("prepare %s succeeded", version)
			}
			if !strings.Contains(err.Error(), tc.wantError) {
				t.Errorf("error = %v, want it to contain %q", err, tc.wantError)
			}
			for _, command := range gitCommands(commands) {
				if strings.HasPrefix(command, "add") || strings.HasPrefix(command, "commit") {
					t.Errorf("prepare ran git %s, want nothing staged or committed", command)
				}
			}
			if got := readTestFile(t, filepath.Join(repo.dir, changelogFile)); got != changelog {
				t.Errorf("CHANGELOG.md was rewritten:\n%s", got)
			}
			if got := readTestFile(t, filepath.Join(repo.dir, versionFile)); got != versionIn {
				t.Errorf("VERSION was rewritten: %q", got)
			}
		})
	}
}

// TestPrepareNamesEveryRefusal pins that one run reports every refusal it finds, not only the
// first, so the owner fixes them in one pass.
func TestPrepareNamesEveryRefusal(t *testing.T) {
	t.Parallel()
	repo := newPrepareRepo(t, "# Changelog\n", "v0.24.9\n")
	repo.isDirty = true

	_, err := repo.prepare(t, repo.runner(), "v0.24.9")

	for _, want := range []string{
		"the working tree is not clean",
		"not above the highest release tag",
		"VERSION already reads v0.24.9",
		"has no ## [Unreleased] heading",
	} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to contain %q", err, want)
		}
	}
}

// TestPrepareReportsAFailedBumpCommit pins that a bump commit that fails is a run failure naming
// the rollup commit already made, so the owner knows the checkout holds one of the two.
func TestPrepareReportsAFailedBumpCommit(t *testing.T) {
	t.Parallel()
	repo := newPrepareRepo(t, unreleasedChangelog, preparedVersion)
	repo.failing = "git commit --quiet -m chore(version)"

	_, err := repo.prepare(t, repo.runner(), "v0.25.0")

	if err == nil || !strings.Contains(err.Error(), `the rollup commit "chore(release): cut v0.25.0" was made`) {
		t.Errorf("error = %v, want it to name the rollup commit already made", err)
	}
}

// TestPrepareRefusalIsARunFailure pins the exit status through the command tree: a refused
// version — malformed included — exits 1, while a missing version is a usage error (exit 2).
// It changes the working directory, so it cannot run in parallel.
func TestPrepareRefusalIsARunFailure(t *testing.T) {
	repo := newPrepareRepo(t, unreleasedChangelog, preparedVersion)
	t.Chdir(repo.dir)

	for _, version := range []string{"v0.25", "v0.24.11"} {
		_, err := execute(t, repo.runner(), "prepare", version)
		if code := exitCodeFor(err); err == nil || code != exitRunFailed {
			t.Errorf("prepare %s = %v (exit %d), want exit %d", version, err, code, exitRunFailed)
		}
	}

	_, err := execute(t, repo.runner(), "prepare")
	if code := exitCodeFor(err); err == nil || code != exitBadUsage {
		t.Errorf("prepare with no version = %v (exit %d), want exit %d", err, code, exitBadUsage)
	}
}
