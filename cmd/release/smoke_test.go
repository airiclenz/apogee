package main

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

// The commit the smoke tests' tag resolves to, and the Homebrew prefix their brew reports.
const (
	smokeTagCommit  = "81eec1b3ac7d27f7ef38fe2368b59b89cbd8832b"
	smokeBrewPrefix = "/opt/homebrew"
)

// The commands smoke runs whose answers the tests vary, keyed as smokeFake.failing reads them.
const (
	smokeListTag     = "git ls-remote --tags origin refs/tags/v0.24.11"
	smokeTagType     = "gh api repos/airiclenz/apogee/git/ref/tags/v0.24.11 --jq .object.type"
	smokeDownloadSum = "gh release download v0.24.11 --repo airiclenz/apogee --pattern SHA256SUMS --output -"
	smokeReadBucket  = "gh api repos/airiclenz/scoop-bucket/contents/bucket/apogee.json --jq .content"
	smokeLatest      = "gh release view --repo airiclenz/apogee --json tagName --jq .tagName"
	smokeRevParse    = "git rev-parse --verify v0.24.11^{commit}"
)

// smokeFake answers smoke's commands the way git, gh, brew and the released binaries would over
// a published publishVersion whose assets sit in releaseDir.
type smokeFake struct {
	releaseDir string
	// tagKind is the remote tag's object type, "" for no tag on origin.
	tagKind string
	// isTagLocal is whether the tag resolves in the local clone.
	isTagLocal bool
	// corrupt names an asset that downloads with the wrong bytes.
	corrupt string
	// revision and modified are the build stamp every binary carries; isUnstamped makes the
	// build information unreadable.
	revision    string
	modified    string
	isUnstamped bool
	// binaryVersion is the version a released binary reports.
	binaryVersion string
	// brew is "missing" (not installed), "absent" (apogee not installed through it) or
	// "installed"; brewReports is what its apogee prints for --version.
	brew        string
	brewReports string
	// manifest is the bucket's manifest, "" for none (GitHub answers 404); latest is the latest
	// release's tag.
	manifest string
	latest   string
	// failing makes a command line fail with the error.
	failing map[string]error
}

// newSmokeFake is a release published in full: the annotated tag is on origin and fetched, the
// six archives and SHA256SUMS are on the release, every binary carries the tag's commit, brew
// is installed and upgrades onto the version, and the bucket's manifest names it.
func newSmokeFake(t *testing.T) *smokeFake {
	t.Helper()
	releaseDir := publishedRelease(t)
	manifest, err := renderScoopManifest(publishVersion, readTestFile(t, filepath.Join(releaseDir, checksumsFile)))
	if err != nil {
		t.Fatal(err)
	}
	return &smokeFake{
		releaseDir:    releaseDir,
		tagKind:       gitObjectTag,
		isTagLocal:    true,
		revision:      smokeTagCommit,
		modified:      "false",
		binaryVersion: publishVersion,
		brew:          "installed",
		brewReports:   "apogee " + publishVersion,
		manifest:      manifest,
		latest:        publishVersion,
		failing:       map[string]error{},
	}
}

// publishedRelease builds publishVersion's six archives and SHA256SUMS with the fake toolchain
// and returns the directory holding them.
func publishedRelease(t *testing.T) string {
	t.Helper()
	sourceDir := t.TempDir()
	if err := writeReleaseTree(sourceDir); err != nil {
		t.Fatal(err)
	}
	return runTestDist(t, fakeToolchain(testBuildCount), sourceDir)
}

// respond answers one command.
func (f *smokeFake) respond(call recordedCall) (string, error) {
	line := call.name + " " + strings.Join(call.args, " ")
	if err, isFailing := f.failing[line]; isFailing {
		return "", err
	}
	args := call.args
	switch {
	case line == smokeListTag:
		if f.tagKind == "" {
			return "", nil
		}
		return smokeTagCommit + "\trefs/tags/" + publishVersion + "\n", nil
	case line == smokeTagType:
		return f.tagKind + "\n", nil
	case line == smokeDownloadSum:
		raw, err := os.ReadFile(filepath.Join(f.releaseDir, checksumsFile))
		return string(raw), err
	case call.name == "gh" && len(args) == 9 && args[0] == "release" && args[1] == "download" && args[7] == "--dir":
		return "", f.download(args[6], args[8])
	case line == smokeRevParse:
		if !f.isTagLocal {
			return "", fmt.Errorf("%s: fatal: needed a single revision", line)
		}
		return smokeTagCommit + "\n", nil
	case line == "brew list --formula apogee":
		switch f.brew {
		case "missing":
			return "", fmt.Errorf("brew list: %w", &exec.Error{Name: "brew", Err: exec.ErrNotFound})
		case "absent":
			return "", fmt.Errorf("brew list: %w", exitStatus(1))
		}
		return "apogee\n", nil
	case line == "brew update", line == "brew upgrade apogee":
		return "", nil
	case line == "brew --prefix":
		return smokeBrewPrefix + "\n", nil
	case line == filepath.Join(smokeBrewPrefix, "bin", "apogee")+" --version":
		return f.brewReports + "\n", nil
	case len(args) == 1 && args[0] == "--version":
		// A binary extracted from an archive: report the version and the stub it holds, so a
		// test can tell which platform's binary ran.
		stub, err := os.ReadFile(call.name)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("apogee %s (%s)\n", f.binaryVersion, strings.TrimSpace(string(stub))), nil
	case line == smokeReadBucket:
		if f.manifest == "" {
			return "", fmt.Errorf("gh: Not Found (HTTP 404)")
		}
		return base64.StdEncoding.EncodeToString([]byte(f.manifest)) + "\n", nil
	case line == smokeLatest:
		return f.latest + "\n", nil
	}
	return "", fmt.Errorf("unexpected command %q", line)
}

// download copies the release's asset name into dir, as `gh release download --dir` would.
func (f *smokeFake) download(name, dir string) error {
	raw, err := os.ReadFile(filepath.Join(f.releaseDir, name))
	if err != nil {
		return fmt.Errorf("no assets match the file pattern: %w", err)
	}
	if name == f.corrupt {
		raw = []byte("corrupted in transit\n")
	}
	return os.WriteFile(filepath.Join(dir, name), raw, 0o644)
}

// readBuildInfo is the fake build-stamp reader: every binary carries the fake's stamp.
func (f *smokeFake) readBuildInfo(string) (*buildinfo.BuildInfo, error) {
	if f.isUnstamped {
		return nil, errors.New("not a Go executable")
	}
	info := &buildinfo.BuildInfo{}
	if f.revision != "" {
		info.Settings = append(info.Settings, debugSetting(vcsRevisionSetting, f.revision))
	}
	info.Settings = append(info.Settings, debugSetting(vcsModifiedSetting, f.modified))
	return info, nil
}

// debugSetting is one build setting as a binary's build information carries it.
func debugSetting(key, value string) debug.BuildSetting {
	return debug.BuildSetting{Key: key, Value: value}
}

// smokeRunResult is what one smoke run did.
type smokeRunResult struct {
	err      error
	output   string
	commands *fakeRunner
}

// runTestSmoke runs smoke of publishVersion over fake from a linux/amd64 host, comparing against
// the published release's own build, after adjust has changed the options.
func runTestSmoke(t *testing.T, fake *smokeFake, adjust func(options *smokeOptions)) smokeRunResult {
	t.Helper()
	options := smokeOptions{
		dir:           t.TempDir(),
		version:       publishVersion,
		distDir:       fake.releaseDir,
		goos:          "linux",
		goarch:        "amd64",
		readBuildInfo: fake.readBuildInfo,
	}
	if adjust != nil {
		adjust(&options)
	}
	commands := &fakeRunner{respond: fake.respond}
	var output bytes.Buffer

	err := runSmoke(t.Context(), commands, &output, options)

	return smokeRunResult{err: err, output: output.String(), commands: commands}
}

// hasResult reports whether output has a result line with label whose text contains fragment.
func hasResult(output, label, fragment string) bool {
	for _, line := range strings.Split(output, "\n") {
		if text, isResult := strings.CutPrefix(line, "    "+label+": "); isResult && strings.Contains(text, fragment) {
			return true
		}
	}
	return false
}

// TestSmokeChecks pins each check's pass, fail and skip, and the tally each run ends on: a skip
// is counted as a skip, never a pass, and only a failure fails the run.
func TestSmokeChecks(t *testing.T) {
	t.Parallel()

	notFound := fmt.Errorf("gh: %w", &exec.Error{Name: "gh", Err: exec.ErrNotFound})
	cases := map[string]struct {
		adjust      func(fake *smokeFake, options *smokeOptions)
		wantResults [][2]string
		wantTally   string
	}{
		"every check passes": {
			wantResults: [][2]string{
				{"PASS", "annotated tag v0.24.11 on origin"},
				{"PASS", "SHA256SUMS in"},
				{"PASS", "6 assets downloaded, all checksums match"},
				{"PASS", "6 assets carry vcs.revision 81eec1b3ac7d = the tag"},
				{"PASS", "the released binary reports \"apogee v0.24.11"},
				{"PASS", "brew's apogee reports \"apogee v0.24.11\""},
				{"PASS", "bucket at 0.24.11, both Windows hashes match SHA256SUMS"},
			},
			wantTally: "7 passed, 0 failed, 0 skipped",
		},
		"no tag on origin": {
			adjust:      func(fake *smokeFake, _ *smokeOptions) { fake.tagKind = "" },
			wantResults: [][2]string{{"FAIL", "origin has no tag v0.24.11"}},
			wantTally:   "6 passed, 1 failed, 0 skipped",
		},
		"lightweight tag": {
			adjust:      func(fake *smokeFake, _ *smokeOptions) { fake.tagKind = "commit" },
			wantResults: [][2]string{{"FAIL", "v0.24.11 on origin is a lightweight tag"}},
			wantTally:   "6 passed, 1 failed, 0 skipped",
		},
		"origin's tags cannot be listed": {
			adjust: func(fake *smokeFake, _ *smokeOptions) {
				fake.failing[smokeListTag] = errors.New("not a git repository")
			},
			wantResults: [][2]string{{"SKIP", "could not list origin's tags"}},
			wantTally:   "6 passed, 0 failed, 1 skipped",
		},
		"tag type unreadable": {
			adjust: func(fake *smokeFake, _ *smokeOptions) {
				fake.failing[smokeTagType] = errors.New("gh: authentication required")
			},
			wantResults: [][2]string{{"SKIP", "gh could not read its type"}},
			wantTally:   "6 passed, 0 failed, 1 skipped",
		},
		"no local build to compare": {
			adjust:      func(_ *smokeFake, options *smokeOptions) { options.distDir = "" },
			wantResults: [][2]string{{"SKIP", "no --dist-dir"}},
			wantTally:   "6 passed, 0 failed, 1 skipped",
		},
		"local build differs from the published sums": {
			adjust: func(fake *smokeFake, options *smokeOptions) {
				options.distDir = otherBuild(t, fake.releaseDir, "apogee_0.24.11_linux_arm64.tar.gz")
			},
			wantResults: [][2]string{{"FAIL", "differs from the published one for apogee_0.24.11_linux_arm64.tar.gz"}},
			wantTally:   "6 passed, 1 failed, 0 skipped",
		},
		"an asset does not match SHA256SUMS": {
			adjust: func(fake *smokeFake, _ *smokeOptions) { fake.corrupt = "apogee_0.24.11_darwin_arm64.tar.gz" },
			wantResults: [][2]string{
				{"FAIL", "asset apogee_0.24.11_darwin_arm64.tar.gz hashes to"},
				{"SKIP", "the assets did not all download and verify"},
				{"PASS", "the released binary reports"},
			},
			wantTally: "5 passed, 1 failed, 1 skipped",
		},
		"the release has no SHA256SUMS": {
			adjust: func(fake *smokeFake, _ *smokeOptions) {
				fake.failing[smokeDownloadSum] = errors.New("no assets match the file pattern")
			},
			wantResults: [][2]string{
				{"SKIP", "the release's SHA256SUMS did not download"},
				{"FAIL", "no SHA256SUMS asset on release v0.24.11"},
				{"SKIP", "the host's archive apogee_0.24.11_linux_amd64.tar.gz did not download"},
			},
			wantTally: "2 passed, 1 failed, 4 skipped",
		},
		"gh is not installed": {
			adjust: func(fake *smokeFake, _ *smokeOptions) {
				fake.failing[smokeTagType] = notFound
				fake.failing[smokeDownloadSum] = notFound
				fake.failing[smokeReadBucket] = notFound
			},
			wantResults: [][2]string{{"SKIP", "gh is not installed"}},
			wantTally:   "1 passed, 0 failed, 6 skipped",
		},
		"tag not fetched into the local clone": {
			adjust:      func(fake *smokeFake, _ *smokeOptions) { fake.isTagLocal = false },
			wantResults: [][2]string{{"SKIP", "tag v0.24.11 is not in the local clone"}},
			wantTally:   "6 passed, 0 failed, 1 skipped",
		},
		"binaries built from another commit": {
			adjust: func(fake *smokeFake, _ *smokeOptions) { fake.revision = "0123456789abcdef0123456789abcdef01234567" },
			wantResults: [][2]string{
				{"FAIL", "asset apogee_0.24.11_linux_amd64.tar.gz was built from 0123456789abcdef"},
				{"FAIL", "asset apogee_0.24.11_windows_arm64.zip was built from 0123456789abcdef"},
			},
			wantTally: "6 passed, 6 failed, 0 skipped",
		},
		"binaries without a revision": {
			adjust:      func(fake *smokeFake, _ *smokeOptions) { fake.revision = "" },
			wantResults: [][2]string{{"FAIL", "carries no vcs.revision"}},
			wantTally:   "6 passed, 6 failed, 0 skipped",
		},
		"binaries without build information": {
			adjust:      func(fake *smokeFake, _ *smokeOptions) { fake.isUnstamped = true },
			wantResults: [][2]string{{"FAIL", "carries no build information"}},
			wantTally:   "6 passed, 6 failed, 0 skipped",
		},
		"binaries built from a modified tree": {
			adjust: func(fake *smokeFake, _ *smokeOptions) { fake.modified = "true" },
			wantResults: [][2]string{
				{"warning", "asset apogee_0.24.11_darwin_amd64.tar.gz was built from a modified tree"},
				{"PASS", "6 assets carry vcs.revision"},
			},
			wantTally: "7 passed, 0 failed, 0 skipped",
		},
		"the host's binary reports another version": {
			adjust:      func(fake *smokeFake, _ *smokeOptions) { fake.binaryVersion = "v0.24.10" },
			wantResults: [][2]string{{"FAIL", "the released binary reports \"apogee v0.24.10"}},
			wantTally:   "6 passed, 1 failed, 0 skipped",
		},
		"no release archive for the host": {
			adjust:      func(_ *smokeFake, options *smokeOptions) { options.goos = "freebsd" },
			wantResults: [][2]string{{"SKIP", "no release archive is built for this host (freebsd/amd64)"}},
			wantTally:   "6 passed, 0 failed, 1 skipped",
		},
		"Homebrew is not installed": {
			adjust:      func(fake *smokeFake, _ *smokeOptions) { fake.brew = "missing" },
			wantResults: [][2]string{{"SKIP", "Homebrew is not installed on this host"}},
			wantTally:   "6 passed, 0 failed, 1 skipped",
		},
		"apogee is not installed through Homebrew": {
			adjust:      func(fake *smokeFake, _ *smokeOptions) { fake.brew = "absent" },
			wantResults: [][2]string{{"SKIP", "apogee is not installed through Homebrew here"}},
			wantTally:   "6 passed, 0 failed, 1 skipped",
		},
		"brew upgrade leaves the old version": {
			adjust:      func(fake *smokeFake, _ *smokeOptions) { fake.brewReports = "apogee v0.24.10" },
			wantResults: [][2]string{{"FAIL", "brew's apogee reports \"apogee v0.24.10\", not v0.24.11"}},
			wantTally:   "6 passed, 1 failed, 0 skipped",
		},
		"the bucket's manifest is unreadable": {
			adjust:      func(fake *smokeFake, _ *smokeOptions) { fake.manifest = "" },
			wantResults: [][2]string{{"SKIP", "the bucket's manifest bucket/apogee.json in airiclenz/scoop-bucket is unreadable"}},
			wantTally:   "6 passed, 0 failed, 1 skipped",
		},
		"the manifest's hashes are prefixed and upper-case": {
			adjust: func(fake *smokeFake, _ *smokeOptions) {
				fake.manifest = prefixHashes(t, fake.manifest, fake.releaseDir)
			},
			wantResults: [][2]string{{"PASS", "both Windows hashes match"}},
			wantTally:   "7 passed, 0 failed, 0 skipped",
		},
		"a manifest hash does not match": {
			adjust: func(fake *smokeFake, _ *smokeOptions) {
				hash, err := checksumFor(readTestFile(t, filepath.Join(fake.releaseDir, checksumsFile)), "apogee_0.24.11_windows_arm64.zip")
				if err != nil {
					t.Fatal(err)
				}
				fake.manifest = strings.Replace(fake.manifest, hash, strings.Repeat("0", 64), 1)
			},
			wantResults: [][2]string{{"FAIL", "the bucket's arm64 hash is"}},
			wantTally:   "6 passed, 1 failed, 0 skipped",
		},
		"the manifest is not JSON": {
			adjust:      func(fake *smokeFake, _ *smokeOptions) { fake.manifest = "{" },
			wantResults: [][2]string{{"FAIL", "is not valid JSON"}},
			wantTally:   "6 passed, 1 failed, 0 skipped",
		},
		"the bucket is behind the latest release": {
			adjust:      func(fake *smokeFake, _ *smokeOptions) { fake.manifest = `{"version": "0.24.10"}` },
			wantResults: [][2]string{{"FAIL", "not the latest release 0.24.11"}},
			wantTally:   "6 passed, 1 failed, 0 skipped",
		},
		"the bucket has moved past an older release": {
			adjust: func(fake *smokeFake, _ *smokeOptions) {
				fake.manifest = `{"version": "0.24.12"}`
				fake.latest = "v0.24.12"
			},
			wantResults: [][2]string{{"SKIP", "v0.24.11 is not the latest release (v0.24.12)"}},
			wantTally:   "6 passed, 0 failed, 1 skipped",
		},
		"the latest release cannot be read": {
			adjust: func(fake *smokeFake, _ *smokeOptions) {
				fake.manifest = `{"version": "0.24.10"}`
				fake.failing[smokeLatest] = errors.New("gh: HTTP 502")
			},
			wantResults: [][2]string{{"SKIP", "the latest release could not be read"}},
			wantTally:   "6 passed, 0 failed, 1 skipped",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := newSmokeFake(t)
			var adjust func(options *smokeOptions)
			if tc.adjust != nil {
				adjust = func(options *smokeOptions) { tc.adjust(fake, options) }
			}

			result := runTestSmoke(t, fake, adjust)

			for _, want := range tc.wantResults {
				if !hasResult(result.output, want[0], want[1]) {
					t.Errorf("output lacks a %s result containing %q:\n%s", want[0], want[1], result.output)
				}
			}
			if want := "smoke: v0.24.11: " + tc.wantTally; !strings.Contains(result.output, want) {
				t.Errorf("output lacks the tally %q:\n%s", want, result.output)
			}
			isFailing := !strings.Contains(tc.wantTally, " 0 failed")
			if (result.err != nil) != isFailing {
				t.Errorf("smoke error = %v, want an error: %t", result.err, isFailing)
			}
		})
	}
}

// otherBuild copies releaseDir's SHA256SUMS into a new directory with name's hash changed, as a
// local build that drifted from the published one would hold it.
func otherBuild(t *testing.T, releaseDir, name string) string {
	t.Helper()
	sums := readTestFile(t, filepath.Join(releaseDir, checksumsFile))
	hash, err := checksumFor(sums, name)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, checksumsFile), strings.Replace(sums, hash, strings.Repeat("f", 64), 1))
	return dir
}

// prefixHashes rewrites the manifest's two Windows hashes as Scoop also accepts them: upper-case,
// behind a `sha256:` prefix.
func prefixHashes(t *testing.T, manifest, releaseDir string) string {
	t.Helper()
	sums := readTestFile(t, filepath.Join(releaseDir, checksumsFile))
	for _, goarch := range []string{"amd64", "arm64"} {
		hash, err := checksumFor(sums, "apogee_0.24.11_windows_"+goarch+".zip")
		if err != nil {
			t.Fatal(err)
		}
		manifest = strings.Replace(manifest, hash, "sha256:"+strings.ToUpper(hash), 1)
	}
	return manifest
}

// TestSmokeRunsTheHostsZipArchive pins the Windows host path: the host's archive is a .zip, and
// the binary run is the apogee.exe inside it — the windows/amd64 build, not another platform's.
func TestSmokeRunsTheHostsZipArchive(t *testing.T) {
	t.Parallel()
	fake := newSmokeFake(t)

	result := runTestSmoke(t, fake, func(options *smokeOptions) {
		options.goos = "windows"
		options.goarch = "amd64"
	})

	if result.err != nil {
		t.Fatalf("smoke failed: %v\n%s", result.err, result.output)
	}
	if !hasResult(result.output, "PASS", "GOOS=windows GOARCH=amd64") {
		t.Errorf("the host check did not run the windows/amd64 binary:\n%s", result.output)
	}
	var ran []string
	for _, call := range result.commands.calls {
		if len(call.args) == 1 && call.args[0] == "--version" && !strings.HasPrefix(call.name, filepath.FromSlash(smokeBrewPrefix)) {
			ran = append(ran, call.name)
		}
	}
	if len(ran) != 1 || filepath.Base(ran[0]) != "apogee.exe" {
		t.Errorf("ran %q --version, want the one apogee.exe from the zip", ran)
	}
}

// TestSmokeVersion pins which version smoke checks: the one asked for, a bare X.Y.Z read as
// vX.Y.Z, and with none, the checkout's VERSION file; a malformed one is a usage error.
func TestSmokeVersion(t *testing.T) {
	t.Parallel()

	t.Run("command line", func(t *testing.T) {
		t.Parallel()
		cases := map[string]struct {
			args      []string
			wantError string
		}{
			"a bare version": {args: []string{"smoke", "0.24.11"}},
			"a tag":          {args: []string{"smoke", "v0.24.11"}},
			"malformed":      {args: []string{"smoke", "v0.24"}, wantError: "is not a release version"},
			"two versions":   {args: []string{"smoke", "v0.24.11", "v0.24.12"}, wantError: "at most one version"},
		}
		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				commands := &fakeRunner{respond: newSmokeFake(t).respond}

				output, err := execute(t, commands, tc.args...)

				if tc.wantError != "" {
					var failed runError
					if err == nil || !strings.Contains(err.Error(), tc.wantError) || errors.As(err, &failed) {
						t.Fatalf("smoke error = %v, want a usage error naming %q", err, tc.wantError)
					}
					return
				}
				if !strings.Contains(output, "smoke: airiclenz/apogee v0.24.11\n") {
					t.Errorf("output does not check v0.24.11:\n%s", output)
				}
				if len(commands.recorded("git")) == 0 || commands.recorded("git")[0].args[3] != "refs/tags/v0.24.11" {
					t.Errorf("the first git command = %v, want the v0.24.11 tag listed", commands.recorded("git"))
				}
			})
		}
	})

	t.Run("the VERSION file", func(t *testing.T) {
		t.Parallel()
		for _, content := range []string{"v0.24.11\n", "0.24.11\n"} {
			fake := newSmokeFake(t)
			dir := t.TempDir()
			writeTestFile(t, filepath.Join(dir, versionFile), content)

			result := runTestSmoke(t, fake, func(options *smokeOptions) {
				options.dir = dir
				options.version = ""
			})

			if result.err != nil || !strings.Contains(result.output, "smoke: airiclenz/apogee v0.24.11\n") {
				t.Errorf("VERSION %q: smoke = %v, want a passing check of v0.24.11:\n%s", content, result.err, result.output)
			}
		}
	})
}

// TestSmokeFailureIsARunFailure pins the exit code: a failed check exits 1, not with usage.
func TestSmokeFailureIsARunFailure(t *testing.T) {
	t.Parallel()
	fake := newSmokeFake(t)
	fake.tagKind = ""

	_, err := execute(t, &fakeRunner{respond: fake.respond}, "smoke", publishVersion)

	if err == nil || exitCodeFor(err) != exitRunFailed {
		t.Errorf("smoke error = %v (exit %d), want a run failure (exit %d)", err, exitCodeFor(err), exitRunFailed)
	}
}

// TestPublishRunsTheSmokeTestLast pins publish's last step: after the release and the channels,
// it smoke-tests the version from the checkout, handing over the directory this run built the
// assets in — still holding them while smoke runs, removed after.
func TestPublishRunsTheSmokeTestLast(t *testing.T) {
	t.Parallel()
	notesPath := filepath.Join(t.TempDir(), "notes.md")
	writeTestFile(t, notesPath, "## What's new\n\n- A release.\n")
	var asked []smokeOptions
	var builtSums string
	smoke := func(_ context.Context, output io.Writer, options smokeOptions) error {
		asked = append(asked, options)
		raw, err := os.ReadFile(filepath.Join(options.distDir, checksumsFile))
		builtSums = string(raw)
		_, _ = fmt.Fprintln(output, "SMOKE RAN")
		return err
	}

	result := runTestPublishWith(t, newPublishFake(), publishOptions{notesFile: notesPath, smoke: smoke})

	if result.err != nil {
		t.Fatalf("publish failed: %v\n%s", result.err, result.output)
	}
	if len(asked) != 1 {
		t.Fatalf("smoke ran %d times, want once", len(asked))
	}
	if asked[0].version != publishVersion || asked[0].distDir == "" || asked[0].readBuildInfo == nil {
		t.Errorf("smoke was asked for %+v, want %s over this run's build", asked[0], publishVersion)
	}
	if builtSums != rebuiltSums(t) {
		t.Errorf("the build directory smoke got held SHA256SUMS %q, want this run's build", builtSums)
	}
	if _, err := os.Stat(asked[0].distDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the build directory %s outlived the run: %v", asked[0].distDir, err)
	}
	lines := strings.Split(result.output, "\n")
	released, ran := lineIndex(lines, "release:"), lineIndex(lines, "SMOKE RAN")
	if released < 0 || ran < released || ran < lineIndex(lines, "smoke: smoke-test v0.24.11 as published") {
		t.Errorf("smoke ran at line %d, after the release at %d? output:\n%s", ran, released, result.output)
	}
	if !hasRow(result.output, "smoke done passed by this run") {
		t.Errorf("the step table does not report smoke done:\n%s", result.output)
	}
}

// TestPublishSmokeStepWithoutABuild pins the smoke step on a run that builds nothing, a dry run
// and a failing smoke test: a finished release is smoke-tested with no build to compare, a dry
// run only says it would smoke-test, and a failed check fails publish.
func TestPublishSmokeStepWithoutABuild(t *testing.T) {
	t.Parallel()

	finished := func() *publishFake {
		fake := newPublishFake()
		fake.repo.remote.tagObject = "tag"
		fake.repo.remote.release = releaseJSON("uploaded", releaseAssetsV02411...)
		return fake
	}

	t.Run("a finished release", func(t *testing.T) {
		t.Parallel()

		result := runTestPublish(t, finished(), false)

		if result.err != nil {
			t.Fatalf("publish failed: %v\n%s", result.err, result.output)
		}
		if len(result.smokes) != 1 || result.smokes[0].distDir != "" || result.smokes[0].version != publishVersion {
			t.Errorf("smoke was asked for %+v, want one run of %s with no build", result.smokes, publishVersion)
		}
	})

	t.Run("a dry run", func(t *testing.T) {
		t.Parallel()

		result := runTestPublish(t, newPublishFake(), true)

		if result.err != nil {
			t.Fatalf("publish failed: %v\n%s", result.err, result.output)
		}
		if len(result.smokes) != 0 {
			t.Errorf("a dry run ran smoke %d times, want none", len(result.smokes))
		}
		if !hasRow(result.output, "  smoke smoke-test v0.24.11 as published") {
			t.Errorf("the plan does not name the smoke step:\n%s", result.output)
		}
	})

	t.Run("a failing smoke test", func(t *testing.T) {
		t.Parallel()
		notesPath := filepath.Join(t.TempDir(), "notes.md")
		writeTestFile(t, notesPath, "notes\n")
		smoke := func(context.Context, io.Writer, smokeOptions) error {
			return errors.New("1 smoke check(s) failed for v0.24.11")
		}

		result := runTestPublishWith(t, finished(), publishOptions{notesFile: notesPath, smoke: smoke})

		if result.err == nil || !strings.Contains(result.err.Error(), "the smoke step failed: 1 smoke check(s) failed") {
			t.Errorf("publish error = %v, want the smoke step's failure", result.err)
		}
		if !hasRow(result.output, "smoke pending") {
			t.Errorf("the step table was not printed after the failure:\n%s", result.output)
		}
	})
}
