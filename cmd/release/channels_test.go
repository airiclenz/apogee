package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The token the winget tests hand publish, and the release URLs komac is given for v0.24.11.
const (
	sentinelToken     = "sentinel-token-must-not-print"
	testReleaseBase   = "https://github.com/airiclenz/apogee/releases/download/v0.24.11/"
	testWingetCommand = "GITHUB_TOKEN=<redacted> komac update AiricLenz.Apogee --version 0.24.11 --urls " +
		testReleaseBase + "apogee_0.24.11_windows_amd64.zip " + testReleaseBase + "apogee_0.24.11_windows_arm64.zip --submit"
)

// fakeChannelRepo is a tap or bucket repository as a clone of it holds it.
type fakeChannelRepo struct {
	// file is the generated file the repository holds, "" for none.
	file string
	// isEmpty is a repository with no commits yet.
	isEmpty bool
}

// fakeChannels is what the channel steps meet beyond the probes: the tap and bucket they clone,
// whether komac is installed, and the token gh is signed in with ("" for none).
type fakeChannels struct {
	tap            fakeChannelRepo
	bucket         fakeChannelRepo
	isKomacMissing bool
	ghToken        string
	// clones maps each clone directory to the repository cloned into it.
	clones map[string]string
}

// channelFile is the generated file publish writes in each channel repository.
var channelFile = map[string]string{tapRepo: formulaPath, bucketRepo: bucketPath}

// respondChannel answers the channel steps' commands: the clone of a tap or bucket and the git
// commands run inside it, komac, and `gh auth token`. It reports false for any other command.
func (f *publishFake) respondChannel(call recordedCall) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	args := call.args
	command := call.name + " " + strings.Join(args, " ")
	switch {
	case call.name == "gh" && len(args) == 6 && args[0] == "repo" && args[1] == "clone":
		repo, cloneDir := args[2], args[3]
		if f.channels.clones == nil {
			f.channels.clones = map[string]string{}
		}
		f.channels.clones[cloneDir] = repo
		return "", true, f.writeClone(repo, cloneDir)
	case command == "komac --version":
		if f.channels.isKomacMissing {
			return "", true, fmt.Errorf("komac --version: %w", &exec.Error{Name: "komac", Err: exec.ErrNotFound})
		}
		return "komac 2.11.2\n", true, nil
	case call.name == "komac":
		return "", true, nil
	case command == "gh auth token":
		if f.channels.ghToken == "" {
			return "", true, fmt.Errorf("gh auth token: exit status 1: no oauth token found for github.com")
		}
		return f.channels.ghToken + "\n", true, nil
	}

	repo, isClone := f.channels.clones[call.dir]
	if !isClone || call.name != "git" {
		return "", false, nil
	}
	switch args[0] {
	case "rev-list":
		if f.channelRepo(repo).isEmpty {
			return "", true, nil
		}
		return bumpCommit + "\n", true, nil
	case "diff":
		written, err := os.ReadFile(filepath.Join(call.dir, filepath.FromSlash(channelFile[repo])))
		if err != nil || string(written) == f.channelRepo(repo).file {
			return "", true, err
		}
		return channelFile[repo] + "\n", true, nil
	case "add", "commit", "push":
		return "", true, nil
	}
	return "", false, nil
}

// channelRepo returns the fake state of repo.
func (f *publishFake) channelRepo(repo string) fakeChannelRepo {
	if repo == tapRepo {
		return f.channels.tap
	}
	return f.channels.bucket
}

// writeClone makes the clone directory of repo, holding its generated file when it has one.
func (f *publishFake) writeClone(repo, cloneDir string) error {
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		return err
	}
	content := f.channelRepo(repo).file
	if content == "" {
		return nil
	}
	filePath := filepath.Join(cloneDir, filepath.FromSlash(channelFile[repo]))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filePath, []byte(content), 0o644)
}

// testReleaseSums is a SHA256SUMS body listing every archive of v0.24.11, one fixture hash each.
func testReleaseSums() string {
	var sums strings.Builder
	for index, name := range releaseAssetsV02411[:len(distTargets)] {
		sums.WriteString(strings.Repeat(fmt.Sprintf("%x", index+10), 64) + "  " + name + "\n")
	}
	return sums.String()
}

// releasedFake is a resumed run of publishVersion: the tag and the whole release are on GitHub,
// the tap's formula and the bucket's manifest pin the previous release, and winget-pkgs has the
// package but no manifest or pull request for the version — so every channel step is pending.
func releasedFake() *publishFake {
	fake := newPublishFake()
	fake.repo.remote = fakeRemote{
		tagObject:      "tag",
		release:        releaseJSON("uploaded", releaseAssetsV02411...),
		formula:        strings.Replace(testFormula, "0.24.11", "0.24.10", 1),
		hasBucket:      true,
		bucketManifest: `{"version": "0.24.10"}`,
		wingetListing:  wingetListing("0.24.10"),
	}
	fake.publishedSums = testReleaseSums()
	fake.channels = fakeChannels{
		tap:    fakeChannelRepo{file: "class Apogee < Formula\n  version \"0.24.10\"\nend\n"},
		bucket: fakeChannelRepo{file: "{\"version\": \"0.24.10\"}\n"},
	}
	return fake
}

// runChannelPublish runs publish of publishVersion over fake with a written notes file and an
// environment whose GITHUB_TOKEN is token ("" for unset).
func runChannelPublish(t *testing.T, fake *publishFake, isDryRun bool, token string) publishRunResult {
	t.Helper()
	notesPath := filepath.Join(t.TempDir(), "notes.md")
	writeTestFile(t, notesPath, "## What's new\n\n- A release.\n")
	getenv := func(key string) string {
		if key == githubTokenVariable {
			return token
		}
		return ""
	}
	return runTestPublishWith(t, fake, publishOptions{notesFile: notesPath, isDryRun: isDryRun, getenv: getenv})
}

// callsInClone returns the git commands run inside the clone of repo, one line each.
func callsInClone(fake *publishFake, commands *fakeRunner, repo string) []string {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	commands.mu.Lock()
	defer commands.mu.Unlock()
	var lines []string
	for _, call := range commands.calls {
		if call.name == "git" && fake.channels.clones[call.dir] == repo {
			lines = append(lines, "git "+strings.Join(call.args, " "))
		}
	}
	return lines
}

// TestPublishWingetDryRunPrintsTheKomacCommandWithTheTokenRedacted ports the bash script's dry-run
// test: the plan shows komac's exact command line with the token redacted, the token itself is
// printed nowhere, and nothing — not even komac's version check — is run.
func TestPublishWingetDryRunPrintsTheKomacCommandWithTheTokenRedacted(t *testing.T) {
	t.Parallel()

	result := runChannelPublish(t, releasedFake(), true, sentinelToken)

	if result.err != nil {
		t.Fatalf("publish failed: %v\n%s", result.err, result.output)
	}
	if !strings.Contains(result.output, testWingetCommand) {
		t.Errorf("the dry run's plan lacks the komac command\n%s\nin:\n%s", testWingetCommand, result.output)
	}
	if strings.Contains(result.output, sentinelToken) {
		t.Errorf("the dry run printed the GITHUB_TOKEN value:\n%s", result.output)
	}
	if calls := result.commands.recorded("komac"); len(calls) > 0 {
		t.Errorf("the dry run ran komac %d times, want never", len(calls))
	}
	if mutating := mutatingLines(result.commands); len(mutating) > 0 {
		t.Errorf("issued mutating commands %q, want none", mutating)
	}
}

// TestPublishWingetSubmitsWithTheTokenInKomacsEnvironment pins the submission: komac's argv is the
// dry run's, the token reaches it through GITHUB_TOKEN in its environment — taken from the
// environment, else from `gh auth token` — and never through its argv or the output, and the
// step reports the release submitted, never published.
func TestPublishWingetSubmitsWithTheTokenInKomacsEnvironment(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		envToken string
		ghToken  string
	}{
		"GITHUB_TOKEN set":         {envToken: sentinelToken, ghToken: "gh-token-unused"},
		"token from gh auth token": {ghToken: sentinelToken},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := releasedFake()
			fake.channels.ghToken = tc.ghToken

			result := runChannelPublish(t, fake, false, tc.envToken)

			if result.err != nil {
				t.Fatalf("publish failed: %v\n%s", result.err, result.output)
			}
			updates := slices.DeleteFunc(result.commands.recorded("komac"), func(call recordedCall) bool {
				return len(call.args) == 0 || call.args[0] != "update"
			})
			if len(updates) != 1 {
				t.Fatalf("ran komac update %d times, want once", len(updates))
			}
			argv := "komac " + strings.Join(updates[0].args, " ")
			if want := strings.TrimPrefix(testWingetCommand, "GITHUB_TOKEN=<redacted> "); argv != want {
				t.Errorf("komac argv:\n got: %s\nwant: %s", argv, want)
			}
			if !slices.Contains(updates[0].env, githubTokenVariable+"="+sentinelToken) {
				t.Errorf("komac's environment %q lacks GITHUB_TOKEN=<the token>", updates[0].env)
			}
			if strings.Contains(result.output, sentinelToken) {
				t.Errorf("publish printed the token:\n%s", result.output)
			}
			if !hasRow(result.output, "winget", "done", wingetSubmittedDetail) {
				t.Errorf("the winget row does not report the release submitted:\n%s", result.output)
			}
		})
	}
}

// TestPublishWingetRequiresKomac ports the bash script's missing-komac test: the winget step fails
// with an install hint and submits nothing.
func TestPublishWingetRequiresKomac(t *testing.T) {
	t.Parallel()
	fake := releasedFake()
	fake.channels.isKomacMissing = true

	result := runChannelPublish(t, fake, false, sentinelToken)

	if result.err == nil {
		t.Fatalf("publish succeeded without komac:\n%s", result.output)
	}
	for _, want := range []string{"the winget step failed", "komac is not installed", "install komac"} {
		if !strings.Contains(result.err.Error(), want) {
			t.Errorf("publish error = %v, want it to name %q", result.err, want)
		}
	}
	if index := lineIndex(commandLines(result.commands), "komac update"); index >= 0 {
		t.Errorf("ran komac update without komac installed")
	}
}

// TestPublishCommitsAChannelFileOnlyWhenItChanges pins the tap and the bucket: each takes the
// rendered file, committed as `apogee X.Y.Z` and pushed to its branch, only when that changes the
// file; a clone already holding it commits and pushes nothing.
func TestPublishCommitsAChannelFileOnlyWhenItChanges(t *testing.T) {
	t.Parallel()
	sums := testReleaseSums()
	formula, err := renderFormula(publishVersion, sums)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := renderScoopManifest(publishVersion, sums)
	if err != nil {
		t.Fatal(err)
	}
	pushed := []string{"git commit --quiet -m apogee 0.24.11", "git push --quiet origin HEAD"}

	cases := map[string]struct {
		tap, bucket  string
		wantTapGit   []string
		wantScoopGit []string
	}{
		"both behind": {
			wantTapGit:   pushed,
			wantScoopGit: pushed,
		},
		"tap already current": {
			tap:          formula,
			wantScoopGit: pushed,
		},
		"bucket already current": {
			bucket:     manifest,
			wantTapGit: pushed,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := releasedFake()
			if tc.tap != "" {
				fake.channels.tap.file = tc.tap
			}
			if tc.bucket != "" {
				fake.channels.bucket.file = tc.bucket
			}

			result := runChannelPublish(t, fake, false, sentinelToken)

			if result.err != nil {
				t.Fatalf("publish failed: %v\n%s", result.err, result.output)
			}
			for repo, want := range map[string][]string{tapRepo: tc.wantTapGit, bucketRepo: tc.wantScoopGit} {
				var writes []string
				for _, line := range callsInClone(fake, result.commands, repo) {
					if strings.HasPrefix(line, "git commit") || strings.HasPrefix(line, "git push") {
						writes = append(writes, line)
					}
				}
				if !slices.Equal(writes, want) {
					t.Errorf("%s: committed and pushed %q, want %q", repo, writes, want)
				}
			}
		})
	}
}

// TestPublishWritesTheRenderedFileIntoTheClone pins what the tap and the bucket receive: exactly
// the file rendered from the release's SHA256SUMS, staged at its path, before the commit.
func TestPublishWritesTheRenderedFileIntoTheClone(t *testing.T) {
	t.Parallel()
	fake := releasedFake()
	written := map[string]string{}
	respond := fake.respond
	commands := &fakeRunner{respond: func(call recordedCall) (string, error) {
		if call.name == "git" && len(call.args) > 0 && call.args[0] == "commit" {
			fake.mu.Lock()
			repo := fake.channels.clones[call.dir]
			fake.mu.Unlock()
			content, err := os.ReadFile(filepath.Join(call.dir, filepath.FromSlash(channelFile[repo])))
			if err != nil {
				return "", err
			}
			written[repo] = string(content)
		}
		return respond(call)
	}}
	run := &publishRun{commands: commands, output: &strings.Builder{}, version: mustVersion(t, publishVersion),
		options: publishOptions{dir: t.TempDir()}}

	for _, converge := range []func() error{
		func() error { return convergeHomebrew(t.Context(), run) },
		func() error { return convergeScoop(t.Context(), run) },
	} {
		if err := converge(); err != nil {
			t.Fatal(err)
		}
	}

	formula, err := renderFormula(publishVersion, testReleaseSums())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := renderScoopManifest(publishVersion, testReleaseSums())
	if err != nil {
		t.Fatal(err)
	}
	if written[tapRepo] != formula {
		t.Errorf("the tap's %s is not the rendered formula:\n%s", formulaPath, written[tapRepo])
	}
	if written[bucketRepo] != manifest {
		t.Errorf("the bucket's %s is not the rendered manifest:\n%s", bucketPath, written[bucketRepo])
	}
	for _, repo := range []string{tapRepo, bucketRepo} {
		if index := lineIndex(callsInClone(fake, commands, repo), "git add -- "+channelFile[repo]); index < 0 {
			t.Errorf("%s: %s was not staged", repo, channelFile[repo])
		}
	}
}

// TestPublishPushesAnEmptyBucketToMain pins the first manifest: a bucket repository with no
// commits has no branch for HEAD to push to, so its first commit goes to main.
func TestPublishPushesAnEmptyBucketToMain(t *testing.T) {
	t.Parallel()
	fake := releasedFake()
	fake.repo.remote.bucketManifest = ""
	fake.channels.bucket = fakeChannelRepo{isEmpty: true}

	result := runChannelPublish(t, fake, false, sentinelToken)

	if result.err != nil {
		t.Fatalf("publish failed: %v\n%s", result.err, result.output)
	}
	clone := callsInClone(fake, result.commands, bucketRepo)
	if index := lineIndex(clone, "git push"); index < 0 || clone[index] != "git push --quiet origin HEAD:main" {
		t.Errorf("bucket clone commands = %q, want the push git push --quiet origin HEAD:main", clone)
	}
	tap := callsInClone(fake, result.commands, tapRepo)
	if index := lineIndex(tap, "git push"); index < 0 || tap[index] != "git push --quiet origin HEAD" {
		t.Errorf("tap clone commands = %q, want the push git push --quiet origin HEAD", tap)
	}
}

// TestPublishSkipsChannelsWhoseSetupIsPending pins ADR 0098 §8: with the bucket repository and
// the winget package not there yet, publish names both skips, touches neither, and succeeds.
func TestPublishSkipsChannelsWhoseSetupIsPending(t *testing.T) {
	t.Parallel()
	fake := releasedFake()
	fake.repo.remote.hasBucket = false
	fake.repo.remote.wingetListing = ""

	result := runChannelPublish(t, fake, false, sentinelToken)

	if result.err != nil {
		t.Fatalf("publish failed: %v\n%s", result.err, result.output)
	}
	for _, want := range []string{"scoop: skipped, its one-time setup is pending", "winget: skipped, its one-time setup is pending"} {
		if !strings.Contains(result.output, want) {
			t.Errorf("output lacks %q:\n%s", want, result.output)
		}
	}
	lines := commandLines(result.commands)
	for _, unwanted := range []string{"gh repo clone " + bucketRepo, "komac"} {
		if index := lineIndex(lines, unwanted); index >= 0 {
			t.Errorf("ran %q for a channel whose setup is pending", lines[index])
		}
	}
	if index := lineIndex(lines, "gh repo clone "+tapRepo); index < 0 {
		t.Errorf("the tap was not updated; commands: %q", lines)
	}
}

// TestPublishLeavesASubmittedWingetReleaseAlone pins the winget probe's hold over komac: an open
// pull request for the version is a submission already made, so komac is never run again.
func TestPublishLeavesASubmittedWingetReleaseAlone(t *testing.T) {
	t.Parallel()
	fake := releasedFake()
	fake.repo.remote.wingetPulls = wingetPull(301234, "0.24.11")

	result := runChannelPublish(t, fake, false, sentinelToken)

	if result.err != nil {
		t.Fatalf("publish failed: %v\n%s", result.err, result.output)
	}
	if calls := result.commands.recorded("komac"); len(calls) > 0 {
		t.Errorf("ran komac %d times over a submitted release, want never", len(calls))
	}
	if !hasRow(result.output, "winget", "done", "submitted: pull request #301234") {
		t.Errorf("the winget row does not report the open pull request:\n%s", result.output)
	}
}

// TestPublishDownloadsTheSumsOutsideTheCheckout pins the channel steps' SHA256SUMS download to
// stdout: nothing is written into the checkout, whose clean tree the next gate checks.
func TestPublishDownloadsTheSumsOutsideTheCheckout(t *testing.T) {
	t.Parallel()

	result := runChannelPublish(t, releasedFake(), false, sentinelToken)

	if result.err != nil {
		t.Fatalf("publish failed: %v\n%s", result.err, result.output)
	}
	var downloads []recordedCall
	for _, call := range result.commands.recorded("gh") {
		if len(call.args) > 1 && call.args[0] == "release" && call.args[1] == "download" {
			downloads = append(downloads, call)
		}
	}
	if len(downloads) == 0 {
		t.Fatal("the channel steps never downloaded SHA256SUMS")
	}
	want := []string{"release", "download", publishVersion, "--pattern", checksumsFile, "--output", "-"}
	for _, call := range downloads {
		if !slices.Equal(call.args, want) {
			t.Errorf("gh %q, want gh %q: the sums go to stdout, never a file in the checkout", call.args, want)
		}
	}
}

// TestPublishWingetConvergesFromRemoteStateOnceTheTagExists pins item 6's resume for the channel
// steps: days later, with VERSION moved on and a dirty tree, publish still submits the pending
// winget manifest without consulting the gate or VERSION at HEAD.
func TestPublishWingetConvergesFromRemoteStateOnceTheTagExists(t *testing.T) {
	t.Parallel()
	fake := releasedFake()
	fake.repo.remote.formula = testFormula
	fake.repo.remote.bucketManifest = `{"version": "0.24.11"}`
	fake.repo.isDirty = true
	fake.headVersion = "v0.24.12"

	result := runChannelPublish(t, fake, false, sentinelToken)

	if result.err != nil {
		t.Fatalf("publish failed: %v\n%s", result.err, result.output)
	}
	lines := commandLines(result.commands)
	for _, unwanted := range []string{"git status", "gh run list", "git show HEAD:VERSION", "git push", "gh repo clone"} {
		if index := lineIndex(lines, unwanted); index >= 0 {
			t.Errorf("ran %q, want only the winget submission once the tag exists", lines[index])
		}
	}
	if index := lineIndex(lines, "komac update "+wingetPackageID); index < 0 {
		t.Errorf("winget was not submitted; commands: %q", lines)
	}
}
