package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// The release the publish tests cut, the checkout's top level they clone, and the poll timing
// their tag waits run on.
const (
	publishVersion  = "v0.24.11"
	publishToplevel = "/home/owner/apogee"
	testTagTimeout  = 30 * time.Second
	testPollPause   = 10 * time.Second
)

// mutatingCommands are the commands that change something — origin, a GitHub release, the
// checkout's refs, or the disk a build writes — by their leading words. A run that may change
// nothing issues none of them.
var mutatingCommands = []string{
	"git push",
	"git fetch --quiet origin refs/tags/",
	"git fetch --quiet --tags",
	"git clone",
	"git checkout",
	"go build",
	"gh release create",
	"gh release upload",
	"gh release delete",
}

// publishFake answers publish's commands the way git, go and gh would: the gate's and the
// probes' commands through repo, the rest — the push, the tag the CI workflow makes after it,
// the clone, the build and the release writes — itself.
type publishFake struct {
	repo fakeRepo
	// headVersion is VERSION at HEAD.
	headVersion string
	// tagAppearsAfter is how many polls after the push origin still lacks the tag; -1 for a
	// workflow that never makes it.
	tagAppearsAfter int
	// tagKind is what `git cat-file -t` prints for the fetched tag; tagCommit is the commit it
	// resolves to.
	tagKind   string
	tagCommit string
	// publishedSums is the release's SHA256SUMS, as `gh release download` prints it.
	publishedSums string

	mu             sync.Mutex
	isPushed       bool
	pollsSincePush int
}

// newPublishFake is a first run of publishVersion: HEAD is the VERSION bump, one commit ahead of
// origin/main, whose feature commit CI passed; nothing is tagged or released yet, and the
// workflow tags the bump on the third poll after the push.
func newPublishFake() *publishFake {
	return &publishFake{
		repo: fakeRepo{
			commits:    linearHistory(),
			head:       bumpCommit,
			originMain: featureCommit,
			greenRuns:  []string{featureCommit},
		},
		headVersion:     publishVersion,
		tagAppearsAfter: 2,
		tagKind:         gitObjectTag,
		tagCommit:       bumpCommit,
	}
}

// runner returns a recording runner over the fake.
func (f *publishFake) runner() *fakeRunner {
	return &fakeRunner{respond: f.respond}
}

// respond answers one command.
func (f *publishFake) respond(call recordedCall) (string, error) {
	tagRef := "refs/tags/" + publishVersion
	args := call.args
	switch command := call.name + " " + strings.Join(args, " "); {
	case command == "git show HEAD:VERSION":
		return f.headVersion + "\n", nil
	case command == "git push origin HEAD:main":
		f.mu.Lock()
		defer f.mu.Unlock()
		f.isPushed = true
		return "", nil
	case command == "git ls-remote --tags origin "+tagRef:
		if f.hasRemoteTag() {
			return "81eec1b3ac7d27f7ef38fe2368b59b89cbd8832b\t" + tagRef + "\n", nil
		}
		return "", nil
	case command == "git fetch --quiet origin "+tagRef+":"+tagRef,
		command == "git fetch --quiet --tags origin",
		command == "git checkout --quiet "+publishVersion,
		call.name == "gh" && len(args) > 1 && args[0] == "release" && (args[1] == "create" || args[1] == "upload"):
		return "", requireFiles(args)
	case command == "git cat-file -t "+tagRef:
		return f.tagKind + "\n", nil
	case command == "git rev-parse --verify "+publishVersion+"^{commit}":
		return f.tagCommit + "\n", nil
	case command == "git log -1 --format=%H HEAD -- VERSION":
		return bumpCommit + "\n", nil
	case command == "git rev-parse --show-toplevel":
		return publishToplevel + "\n", nil
	case call.name == "git" && len(args) == 4 && args[0] == "clone" && args[2] == publishToplevel:
		return "", writeReleaseTree(args[3])
	case command == "git log -1 --format=%ct HEAD":
		return testCommitTime, nil
	case call.name == "go" && len(args) > 0 && args[0] == "build":
		output := args[slices.Index(args, "-o")+1]
		return "", os.WriteFile(output, []byte(stubBinary(call.env)), 0o755)
	case command == "gh release download "+publishVersion+" --pattern SHA256SUMS --output -":
		return f.publishedSums, nil
	}
	return f.repo.respond(call)
}

// hasRemoteTag reports whether origin holds the tag: from the start when the remote says so,
// otherwise once the workflow has had tagAppearsAfter polls after the push.
func (f *publishFake) hasRemoteTag() bool {
	if f.repo.remote.tagObject != "" {
		return true
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.isPushed {
		return false
	}
	f.pollsSincePush++
	return f.tagAppearsAfter >= 0 && f.pollsSincePush > f.tagAppearsAfter
}

// requireFiles fails when an absolute path among args names no file: gh would refuse to upload
// an asset that is not there.
func requireFiles(args []string) error {
	for _, arg := range args {
		if !filepath.IsAbs(arg) {
			continue
		}
		if _, err := os.Stat(arg); err != nil {
			return err
		}
	}
	return nil
}

// writeReleaseTree writes the files dist reads from a checkout of publishVersion into dir.
func writeReleaseTree(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	files := map[string]string{"VERSION": publishVersion + "\n", "LICENSE": testLicense, "README.md": testReadme}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// rebuiltSums is the SHA256SUMS a rebuild of publishVersion writes: dist is byte-identical
// across runs over the same tree, so a release published from an earlier run carries exactly it.
func rebuiltSums(t *testing.T) string {
	t.Helper()
	sourceDir := t.TempDir()
	if err := writeReleaseTree(sourceDir); err != nil {
		t.Fatal(err)
	}
	outDir := runTestDist(t, fakeToolchain(testBuildCount), sourceDir)
	return readTestFile(t, filepath.Join(outDir, checksumsFile))
}

// publishRunResult is what one publish run did: the error main would turn into an exit code,
// everything it printed, every command it ran, how many times it paused, and its notes file.
type publishRunResult struct {
	err       error
	output    string
	commands  *fakeRunner
	pauses    int
	notesPath string
}

// runTestPublish runs publish of publishVersion over fake with a written notes file, pausing
// through a counter instead of a clock.
func runTestPublish(t *testing.T, fake *publishFake, isDryRun bool) publishRunResult {
	t.Helper()
	notesPath := filepath.Join(t.TempDir(), "notes.md")
	writeTestFile(t, notesPath, "## What's new\n\n- A release.\n")
	return runTestPublishWith(t, fake, publishOptions{notesFile: notesPath, isDryRun: isDryRun})
}

// runTestPublishWith runs publish of publishVersion over fake with options, filling in the
// version, the test poll timing and a counting sleep.
func runTestPublishWith(t *testing.T, fake *publishFake, options publishOptions) publishRunResult {
	t.Helper()
	result := publishRunResult{commands: fake.runner(), notesPath: options.notesFile}
	options.dir = t.TempDir()
	options.version = publishVersion
	options.tagTimeout = testTagTimeout
	options.pollInterval = testPollPause
	options.sleep = func(context.Context, time.Duration) error {
		result.pauses++
		return nil
	}
	var output bytes.Buffer

	result.err = runPublish(t.Context(), result.commands, &output, options)

	result.output = output.String()
	return result
}

// commandLines returns every command the runner was asked for, one line each, in order.
func commandLines(commands *fakeRunner) []string {
	commands.mu.Lock()
	defer commands.mu.Unlock()
	lines := make([]string, 0, len(commands.calls))
	for _, call := range commands.calls {
		lines = append(lines, call.name+" "+strings.Join(call.args, " "))
	}
	return lines
}

// mutatingLines returns the commands among the runner's calls that change something.
func mutatingLines(commands *fakeRunner) []string {
	var mutating []string
	for _, line := range commandLines(commands) {
		if slices.ContainsFunc(mutatingCommands, func(prefix string) bool { return strings.HasPrefix(line, prefix) }) {
			mutating = append(mutating, line)
		}
	}
	return mutating
}

// lineIndex returns the index of the first line starting with prefix, -1 for none.
func lineIndex(lines []string, prefix string) int {
	return slices.IndexFunc(lines, func(line string) bool { return strings.HasPrefix(line, prefix) })
}

// TestPublishFirstRunConvergesTheTagThenTheRelease pins a first run's order end to end: push
// HEAD, wait for the CI-made tag, fetch it before resolving it, then fetch the tags, clone, check
// the tag out, build every archive and create the release — and print every step done.
func TestPublishFirstRunConvergesTheTagThenTheRelease(t *testing.T) {
	t.Parallel()

	result := runTestPublish(t, newPublishFake(), false)

	if result.err != nil {
		t.Fatalf("publish failed: %v\n%s", result.err, result.output)
	}
	wantOrder := []string{
		"git push origin HEAD:main",
		"git fetch --quiet origin refs/tags/v0.24.11:refs/tags/v0.24.11",
		"git fetch --quiet --tags origin",
		"git clone --quiet " + publishToplevel,
		"git checkout --quiet v0.24.11",
		"go build", "go build", "go build", "go build", "go build", "go build",
		"gh release create v0.24.11",
	}
	mutating := mutatingLines(result.commands)
	if len(mutating) != len(wantOrder) {
		t.Fatalf("mutating commands = %q, want %d of them in the order %q", mutating, len(wantOrder), wantOrder)
	}
	for index, prefix := range wantOrder {
		if !strings.HasPrefix(mutating[index], prefix) {
			t.Errorf("mutating command %d = %q, want one starting %q", index, mutating[index], prefix)
		}
	}
	lines := commandLines(result.commands)
	fetched := lineIndex(lines, "git fetch --quiet origin refs/tags/v0.24.11")
	resolved := lineIndex(lines, "git rev-parse --verify v0.24.11^{commit}")
	if fetched < 0 || resolved < fetched {
		t.Errorf("the tag is fetched at command %d and resolved at %d: a first run must fetch it before resolving it", fetched, resolved)
	}
	if result.pauses != 2 {
		t.Errorf("paused %d times, want 2: the tag appears on the third poll after the push", result.pauses)
	}
	for _, want := range []string{"tag      done  done by this run", "release  done  done by this run"} {
		if !strings.Contains(result.output, want) {
			t.Errorf("output lacks %q:\n%s", want, result.output)
		}
	}
}

// TestPublishCreatesTheReleaseWithTheTagsArgv pins `gh release create`'s argv: the tag, verified
// to exist on the remote, as its title, the notes file, marked latest, then the seven assets in
// release order — each one a built file when gh is asked for it.
func TestPublishCreatesTheReleaseWithTheTagsArgv(t *testing.T) {
	t.Parallel()

	result := runTestPublish(t, newPublishFake(), false)

	if result.err != nil {
		t.Fatalf("publish failed: %v\n%s", result.err, result.output)
	}
	creates := slices.DeleteFunc(result.commands.recorded("gh"), func(call recordedCall) bool {
		return len(call.args) < 2 || call.args[1] != "create"
	})
	if len(creates) != 1 {
		t.Fatalf("got %d gh release create calls, want 1", len(creates))
	}
	args := creates[0].args
	wantHead := []string{"release", "create", publishVersion, "--verify-tag", "--title", publishVersion, "--notes-file", result.notesPath, "--latest"}
	if !slices.Equal(args[:len(wantHead)], wantHead) {
		t.Errorf("gh release create args = %q, want them to start %q", args, wantHead)
	}
	assets := args[len(wantHead):]
	wantNames := releaseAssetNames(mustVersion(t, publishVersion))
	if len(assets) != len(wantNames) {
		t.Fatalf("gh release create uploads %q, want the %d assets %q", assets, len(wantNames), wantNames)
	}
	for index, asset := range assets {
		if !filepath.IsAbs(asset) || filepath.Base(asset) != wantNames[index] {
			t.Errorf("asset %d = %s, want an absolute path to %s", index, asset, wantNames[index])
		}
	}
}

// TestPublishChangesNothingWhenItHasNothingToDo pins the two runs that may change nothing: a
// second run over a finished release, and a dry run over an unstarted one, which still prints
// what each step would do.
func TestPublishChangesNothingWhenItHasNothingToDo(t *testing.T) {
	t.Parallel()

	finished := newPublishFake()
	finished.repo.remote.tagObject = "tag"
	finished.repo.remote.release = releaseJSON("uploaded", releaseAssetsV02411...)
	cases := map[string]struct {
		fake       *publishFake
		isDryRun   bool
		wantOutput []string
	}{
		"second run over a finished release": {
			fake:       finished,
			wantOutput: []string{"tag      done  annotated tag v0.24.11 on origin", "release  done  release v0.24.11 with all 7 assets"},
		},
		"dry run over an unstarted release": {
			fake:       newPublishFake(),
			isDryRun:   true,
			wantOutput: []string{"gate     cleared at", "plan (dry run, nothing was changed)", "  tag      push HEAD to origin/main", "  release  build the 7 assets in a fresh clone at v0.24.11"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result := runTestPublish(t, tc.fake, tc.isDryRun)

			if result.err != nil {
				t.Fatalf("publish failed: %v\n%s", result.err, result.output)
			}
			if mutating := mutatingLines(result.commands); len(mutating) > 0 {
				t.Errorf("issued mutating commands %q, want none", mutating)
			}
			for _, want := range tc.wantOutput {
				if !strings.Contains(result.output, want) {
					t.Errorf("output lacks %q:\n%s", want, result.output)
				}
			}
		})
	}
}

// TestPublishRefusesBeforeChangingAnything pins every refusal a run makes before its first step:
// each is named, nothing is changed, and the step table is still printed.
func TestPublishRefusesBeforeChangingAnything(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		change    func(fake *publishFake)
		notesFile string
		wantError string
	}{
		"blocked gate":   {change: func(fake *publishFake) { fake.repo.isDirty = true }, wantError: "gate: the working tree is not clean"},
		"VERSION behind": {change: func(fake *publishFake) { fake.headVersion = "v0.24.10" }, wantError: `VERSION at HEAD reads "v0.24.10", not v0.24.11: run prepare first`},
		"lightweight tag on origin": {
			change:    func(fake *publishFake) { fake.repo.remote.tagObject = "commit" },
			wantError: "could not tell where the tag step stands: v0.24.11 on origin is a lightweight tag",
		},
		"release unreadable": {
			change:    func(fake *publishFake) { fake.repo.remote.failing = "gh release view" },
			wantError: "could not tell where the release step stands",
		},
		"empty notes file":   {notesFile: "  \n", wantError: "is empty"},
		"missing notes file": {notesFile: "-", wantError: "read --notes-file"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := newPublishFake()
			if tc.change != nil {
				tc.change(fake)
			}
			notesPath := filepath.Join(t.TempDir(), "notes.md")
			switch tc.notesFile {
			case "":
				writeTestFile(t, notesPath, "- A release.\n")
			case "-":
			default:
				writeTestFile(t, notesPath, tc.notesFile)
			}

			result := runTestPublishWith(t, fake, publishOptions{notesFile: notesPath})

			if result.err == nil || !strings.Contains(result.err.Error(), tc.wantError) {
				t.Fatalf("publish error = %v, want one naming %q", result.err, tc.wantError)
			}
			if !strings.Contains(result.err.Error(), "nothing was changed") {
				t.Errorf("publish error = %v, want it to say nothing was changed", result.err)
			}
			if mutating := mutatingLines(result.commands); len(mutating) > 0 {
				t.Errorf("issued mutating commands %q, want none", mutating)
			}
			if !strings.Contains(result.output, "steps") {
				t.Errorf("the step table was not printed:\n%s", result.output)
			}
		})
	}
}

// TestPublishDryRunWithABlockedGateFailsButPrintsThePlan pins a dry run's exit status: a blocked
// gate fails it, yet every probe row and every pending step's plan is still printed.
func TestPublishDryRunWithABlockedGateFailsButPrintsThePlan(t *testing.T) {
	t.Parallel()
	fake := newPublishFake()
	fake.repo.isDirty = true

	result := runTestPublish(t, fake, true)

	if result.err == nil || !strings.Contains(result.err.Error(), "the working tree is not clean") {
		t.Fatalf("publish error = %v, want one naming the dirty tree", result.err)
	}
	for _, want := range []string{
		"gate     BLOCKED at",
		"  tag      pending  origin has no tag v0.24.11",
		"  release  pending  no GitHub release v0.24.11",
		"  tag      push HEAD to origin/main",
		"  release  build the 7 assets",
	} {
		if !strings.Contains(result.output, want) {
			t.Errorf("output lacks %q:\n%s", want, result.output)
		}
	}
	if mutating := mutatingLines(result.commands); len(mutating) > 0 {
		t.Errorf("issued mutating commands %q, want none", mutating)
	}
}

// TestPublishConvergesFromRemoteStateOnceTheTagExists pins the resume: once origin holds the
// annotated tag, neither the gate nor VERSION at HEAD is consulted — a later micro-bump and a
// dirty tree do not stop the release being built from the tag and created.
func TestPublishConvergesFromRemoteStateOnceTheTagExists(t *testing.T) {
	t.Parallel()
	fake := newPublishFake()
	fake.repo.remote.tagObject = "tag"
	fake.repo.isDirty = true
	fake.headVersion = "v0.24.12"

	result := runTestPublish(t, fake, false)

	if result.err != nil {
		t.Fatalf("publish failed: %v\n%s", result.err, result.output)
	}
	lines := commandLines(result.commands)
	for _, unwanted := range []string{"git status", "gh run list", "git show HEAD:VERSION", "git push"} {
		if index := lineIndex(lines, unwanted); index >= 0 {
			t.Errorf("ran %q, want no gate, VERSION check or push once the tag exists", lines[index])
		}
	}
	if lineIndex(lines, "gh release create v0.24.11") < 0 {
		t.Errorf("the release was not created; commands: %q", lines)
	}
}

// TestPublishTagStepFailures pins each way the tag step fails after the push: no tag in time, a
// lightweight tag, a tag on the wrong commit. Each stops the run before the release step, with
// the step table printed.
func TestPublishTagStepFailures(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		change     func(fake *publishFake)
		wantError  string
		wantPauses int
	}{
		"no tag in time": {
			change:     func(fake *publishFake) { fake.tagAppearsAfter = -1 },
			wantError:  "origin had no tag v0.24.11 after 30s",
			wantPauses: int(testTagTimeout / testPollPause),
		},
		"lightweight tag": {
			change:     func(fake *publishFake) { fake.tagKind = "commit" },
			wantError:  "v0.24.11 on origin is a lightweight tag; a release tag must be annotated",
			wantPauses: 2,
		},
		"tag on another commit": {
			change:     func(fake *publishFake) { fake.tagCommit = featureCommit },
			wantError:  "tag v0.24.11 is on 200000000000, not on the VERSION bump 300000000000",
			wantPauses: 2,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := newPublishFake()
			tc.change(fake)

			result := runTestPublish(t, fake, false)

			if result.err == nil || !strings.Contains(result.err.Error(), tc.wantError) {
				t.Fatalf("publish error = %v, want one naming %q", result.err, tc.wantError)
			}
			if result.pauses != tc.wantPauses {
				t.Errorf("paused %d times, want %d", result.pauses, tc.wantPauses)
			}
			if index := lineIndex(commandLines(result.commands), "git clone"); index >= 0 {
				t.Errorf("the release step ran after the tag step failed")
			}
			if !strings.Contains(result.output, "steps") {
				t.Errorf("the step table was not printed:\n%s", result.output)
			}
		})
	}
}

// TestPublishPartialReleaseUploadsOnlyTheMissingAssets pins the resume of a half-uploaded
// release: the rebuilt sums are checked against its SHA256SUMS when it has one, and only the
// assets it lacks are uploaded — never a create, never an asset it already carries.
func TestPublishPartialReleaseUploadsOnlyTheMissingAssets(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		uploaded     []string
		wantUploads  []string
		wantDownload bool
	}{
		"two archives missing": {
			uploaded:     slices.Delete(slices.Clone(releaseAssetsV02411), 4, 6),
			wantUploads:  []string{"apogee_0.24.11_windows_amd64.zip", "apogee_0.24.11_windows_arm64.zip"},
			wantDownload: true,
		},
		"SHA256SUMS missing": {
			uploaded:    releaseAssetsV02411[:6],
			wantUploads: []string{checksumsFile},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := newPublishFake()
			fake.repo.remote.tagObject = "tag"
			fake.repo.remote.release = releaseJSON("uploaded", tc.uploaded...)
			fake.publishedSums = rebuiltSums(t)

			result := runTestPublish(t, fake, false)

			if result.err != nil {
				t.Fatalf("publish failed: %v\n%s", result.err, result.output)
			}
			var uploads []recordedCall
			for _, call := range result.commands.recorded("gh") {
				switch {
				case len(call.args) > 1 && call.args[1] == "create":
					t.Errorf("created the release %q, want only an upload to the existing one", call.args)
				case len(call.args) > 1 && call.args[1] == "upload":
					uploads = append(uploads, call)
				}
			}
			if len(uploads) != 1 {
				t.Fatalf("got %d gh release upload calls, want 1", len(uploads))
			}
			args := uploads[0].args
			if !slices.Equal(args[:4], []string{"release", "upload", publishVersion, "--clobber"}) {
				t.Errorf("gh release upload args = %q, want them to start release upload %s --clobber", args, publishVersion)
			}
			var names []string
			for _, path := range args[4:] {
				names = append(names, filepath.Base(path))
			}
			if !slices.Equal(names, tc.wantUploads) {
				t.Errorf("uploaded %q, want only %q", names, tc.wantUploads)
			}
			isDownloaded := lineIndex(commandLines(result.commands), "gh release download") >= 0
			if isDownloaded != tc.wantDownload {
				t.Errorf("downloaded SHA256SUMS = %t, want %t", isDownloaded, tc.wantDownload)
			}
		})
	}
}

// TestPublishRefusesARebuildThatDoesNotMatchThePublishedSums pins the sums check: an uploaded
// archive whose published hash differs from the rebuilt one stops the run before any upload, and
// the failure names the way out.
func TestPublishRefusesARebuildThatDoesNotMatchThePublishedSums(t *testing.T) {
	t.Parallel()
	fake := newPublishFake()
	fake.repo.remote.tagObject = "tag"
	fake.repo.remote.release = releaseJSON("uploaded", slices.Delete(slices.Clone(releaseAssetsV02411), 4, 6)...)
	sums := rebuiltSums(t)
	linuxHash, err := checksumFor(sums, "apogee_0.24.11_linux_amd64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	fake.publishedSums = strings.Replace(sums, linuxHash, strings.Repeat("0", len(linuxHash)), 1)

	result := runTestPublish(t, fake, false)

	if result.err == nil {
		t.Fatalf("publish succeeded over a mismatched SHA256SUMS:\n%s", result.output)
	}
	for _, want := range []string{"apogee_0.24.11_linux_amd64.tar.gz", "gh release delete-asset v0.24.11"} {
		if !strings.Contains(result.err.Error(), want) {
			t.Errorf("publish error = %v, want it to name %q", result.err, want)
		}
	}
	if index := lineIndex(commandLines(result.commands), "gh release upload"); index >= 0 {
		t.Errorf("uploaded assets over a mismatched SHA256SUMS")
	}
}

// TestPublishCommandLine pins the verb's command-line contract: --notes-file is required (a usage
// error, exit 2), and a malformed version is a refused run (exit 1) that probes nothing.
func TestPublishCommandLine(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		args      []string
		isUsage   bool
		wantError string
	}{
		"no notes file":     {args: []string{"publish", publishVersion}, isUsage: true, wantError: "notes-file"},
		"malformed version": {args: []string{"publish", "v0.24", "--notes-file", "notes.md"}, wantError: "is not a release version"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			commands := newPublishFake().runner()

			_, err := execute(t, commands, tc.args...)

			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("publish error = %v, want one naming %q", err, tc.wantError)
			}
			var failed runError
			if isRunFailure := errors.As(err, &failed); isRunFailure == tc.isUsage {
				t.Errorf("run failure = %t, want %t", isRunFailure, !tc.isUsage)
			}
			if calls := commandLines(commands); len(calls) > 0 {
				t.Errorf("ran %q, want nothing", calls)
			}
		})
	}
}

// TestSleepContextStopsWhenTheContextEnds pins the production pause: a cancelled context ends it
// with the context's error rather than waiting out the duration.
func TestSleepContextStopsWhenTheContextEnds(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := sleepContext(ctx, time.Hour)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("sleepContext = %v, want %v", err, context.Canceled)
	}
}
