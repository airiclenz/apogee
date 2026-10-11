package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// The errors gh fails with, copied from real runs: a GitHub 404 (a missing path or repository),
// an empty repository's contents, and a release that does not exist.
const (
	ghNotFound        = "exit status 1: gh: Not Found (HTTP 404)"
	ghEmptyRepository = "exit status 1: gh: This repository is empty. (HTTP 404)"
	ghNoRelease       = "exit status 1: release not found"
)

// testFormula is the head of the tap's formula, copied from airiclenz/homebrew-tap at v0.24.11.
const testFormula = `class Apogee < Formula
  desc "Terminal coding agent built for smaller local models, better with bigger ones"
  homepage "https://github.com/airiclenz/apogee"
  version "0.24.11"
  license "MIT"
`

// releaseAssetsV02411 are the assets the published v0.24.11 release carries, copied from
// `gh release view v0.24.11 --json assets`.
var releaseAssetsV02411 = []string{
	"apogee_0.24.11_darwin_amd64.tar.gz",
	"apogee_0.24.11_darwin_arm64.tar.gz",
	"apogee_0.24.11_linux_amd64.tar.gz",
	"apogee_0.24.11_linux_arm64.tar.gz",
	"apogee_0.24.11_windows_amd64.zip",
	"apogee_0.24.11_windows_arm64.zip",
	"SHA256SUMS",
}

// fakeRemote answers the publish-step probes' commands the way git and gh would over remotes
// in a given state. Its zero value is a release nothing has been published for, on channels
// whose one-time setup is still pending.
type fakeRemote struct {
	// tagObject is the type of the version's tag on origin: "" for no tag, "tag" for an
	// annotated one, "commit" for a lightweight one.
	tagObject string
	// release is what `gh release view <tag> --json assets` prints, "" for no release.
	release string
	// formula is the tap's formula, "" for none.
	formula string
	// hasBucket is whether the bucket repository exists; bucketManifest is its manifest, ""
	// for an empty repository.
	hasBucket      bool
	bucketManifest string
	// wingetListing is the package's manifest directory listing in winget-pkgs, "" while the
	// package is not there; wingetPulls is what `gh pr list` prints, "" for no pull request.
	wingetListing string
	wingetPulls   string
	// failing names a command — "gh api repos/airiclenz/homebrew-tap" — that cannot run.
	failing string
}

// respond answers one probe command.
func (r fakeRemote) respond(call recordedCall) (string, error) {
	command := call.name + " " + strings.Join(call.args, " ")
	fail := func(detail string) (string, error) {
		return "", fmt.Errorf("%s: %s", command, detail)
	}
	if r.failing != "" && strings.HasPrefix(command, r.failing) {
		return fail("cannot run")
	}
	args := call.args
	switch {
	case call.name == "git" && len(args) == 4 && args[0] == "ls-remote":
		if r.tagObject == "" {
			return "", nil
		}
		return "81eec1b3ac7d27f7ef38fe2368b59b89cbd8832b\t" + args[3] + "\n", nil
	case call.name == "gh" && len(args) == 4 && args[0] == "api" && strings.HasPrefix(args[1], "repos/"+apogeeRepo+"/git/ref/tags/"):
		if r.tagObject == "" {
			return fail(ghNotFound)
		}
		return r.tagObject + "\n", nil
	case call.name == "gh" && len(args) == 5 && args[0] == "release" && args[1] == "view" && args[4] == "assets":
		if r.release == "" {
			return fail(ghNoRelease)
		}
		return r.release, nil
	case command == "gh api repos/"+tapRepo+"/contents/"+formulaPath+" --jq .content":
		if r.formula == "" {
			return fail(ghNotFound)
		}
		return contentsAPIBase64(r.formula), nil
	case command == "gh api repos/"+bucketRepo+" --jq .full_name":
		if !r.hasBucket {
			return fail(ghNotFound)
		}
		return bucketRepo + "\n", nil
	case command == "gh api repos/"+bucketRepo+"/contents/"+bucketPath+" --jq .content":
		switch {
		case !r.hasBucket:
			return fail(ghNotFound)
		case r.bucketManifest == "":
			return fail(ghEmptyRepository)
		}
		return contentsAPIBase64(r.bucketManifest), nil
	case command == "gh api repos/"+wingetRepo+"/contents/"+wingetManifestDir:
		if r.wingetListing == "" {
			return fail(ghNotFound)
		}
		return r.wingetListing, nil
	case call.name == "gh" && len(args) > 1 && args[0] == "pr" && args[1] == "list":
		if r.wingetPulls == "" {
			return "[]\n", nil
		}
		return r.wingetPulls, nil
	}
	return "", fmt.Errorf("unexpected command %s", command)
}

// runner is a fake runner over the remote alone.
func (r fakeRemote) runner() *fakeRunner {
	return &fakeRunner{respond: r.respond}
}

// contentsAPIBase64 encodes content the way `gh api .../contents/<path> --jq .content` prints it:
// base64 wrapped at 60 columns.
func contentsAPIBase64(content string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	var lines []string
	for len(encoded) > 60 {
		lines = append(lines, encoded[:60])
		encoded = encoded[60:]
	}
	return strings.Join(append(lines, encoded), "\n") + "\n"
}

// releaseJSON is `gh release view --json assets` output for a release carrying names, each in
// state; the real output's other asset fields are left out, as the probe reads none of them.
func releaseJSON(state string, names ...string) string {
	assets := make([]string, 0, len(names))
	for _, name := range names {
		assets = append(assets, fmt.Sprintf(`{"name":%q,"state":%q}`, name, state))
	}
	return `{"assets":[` + strings.Join(assets, ",") + `]}` + "\n"
}

// wingetListing is a winget-pkgs contents listing of the package's manifest directory holding one
// directory per version.
func wingetListing(versions ...string) string {
	entries := make([]string, 0, len(versions))
	for _, version := range versions {
		entries = append(entries, fmt.Sprintf(`{"name":%q,"path":"%s/%s","type":"dir"}`, version, wingetManifestDir, version))
	}
	return "[" + strings.Join(entries, ",") + "]\n"
}

// wingetPull is `gh pr list --json number,title,url` output holding one komac pull request.
func wingetPull(number int, version string) string {
	return fmt.Sprintf(`[{"number":%d,"title":"New version: AiricLenz.Apogee version %s","url":"https://github.com/microsoft/winget-pkgs/pull/%d"}]`+"\n",
		number, version, number)
}

// mustVersion parses a release version or fails the test.
func mustVersion(t *testing.T, text string) releaseVersion {
	t.Helper()
	version, err := parseReleaseVersion(text)
	if err != nil {
		t.Fatal(err)
	}
	return version
}

// TestReleaseAssetNamesMatchAPublishedRelease pins the seven assets the release probe wants
// against the ones a real release carries.
func TestReleaseAssetNamesMatchAPublishedRelease(t *testing.T) {
	t.Parallel()

	got := releaseAssetNames(mustVersion(t, "v0.24.11"))

	if !slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(releaseAssetsV02411))) {
		t.Errorf("release asset names = %q, want %q", got, releaseAssetsV02411)
	}
}

// TestProbesReportEachState pins every probe's done, pending, setup-pending and unknown branches
// over canned remote answers.
func TestProbesReportEachState(t *testing.T) {
	t.Parallel()

	sixAssets := releaseJSON("uploaded", releaseAssetsV02411[:6]...)
	allAssets := releaseJSON("uploaded", releaseAssetsV02411...)
	halfUploaded := strings.Replace(allAssets, `"name":"SHA256SUMS","state":"uploaded"`, `"name":"SHA256SUMS","state":"starter"`, 1)
	olderFormula := strings.Replace(testFormula, `version "0.24.11"`, `version "0.24.10"`, 1)

	cases := map[string]struct {
		probe      stepProbe
		remote     fakeRemote
		version    string
		wantState  stepState
		wantDetail string
	}{
		"tag absent":            {probe: probeTag, wantState: statePending, wantDetail: "origin has no tag v0.24.11"},
		"tag annotated":         {probe: probeTag, remote: fakeRemote{tagObject: "tag"}, wantState: stateDone, wantDetail: "annotated tag v0.24.11"},
		"tag lightweight":       {probe: probeTag, remote: fakeRemote{tagObject: "commit"}, wantState: stateUnknown, wantDetail: "lightweight tag"},
		"tag ls-remote fails":   {probe: probeTag, remote: fakeRemote{tagObject: "tag", failing: "git ls-remote"}, wantState: stateUnknown, wantDetail: "could not list origin's tags"},
		"tag type unreadable":   {probe: probeTag, remote: fakeRemote{tagObject: "tag", failing: "gh api"}, wantState: stateUnknown, wantDetail: "could not read the type of tag v0.24.11"},
		"release absent":        {probe: probeRelease, wantState: statePending, wantDetail: "no GitHub release v0.24.11"},
		"release 6 of 7 assets": {probe: probeRelease, remote: fakeRemote{release: sixAssets}, wantState: statePending, wantDetail: "lacks 1 of its 7 assets: SHA256SUMS"},
		"release half-uploaded": {probe: probeRelease, remote: fakeRemote{release: halfUploaded}, wantState: statePending, wantDetail: "lacks 1 of its 7 assets: SHA256SUMS"},
		"release complete":      {probe: probeRelease, remote: fakeRemote{release: allAssets}, wantState: stateDone, wantDetail: "all 7 assets"},
		"release unreadable":    {probe: probeRelease, remote: fakeRemote{release: allAssets, failing: "gh release"}, wantState: stateUnknown, wantDetail: "could not read GitHub release v0.24.11"},
		"formula absent":        {probe: probeHomebrew, wantState: statePending, wantDetail: "has no Formula/apogee.rb"},
		"formula older":         {probe: probeHomebrew, remote: fakeRemote{formula: olderFormula}, wantState: statePending, wantDetail: `formula pins "0.24.10", not 0.24.11`},
		"formula current":       {probe: probeHomebrew, remote: fakeRemote{formula: testFormula}, wantState: stateDone, wantDetail: "formula pins 0.24.11"},
		"formula unreadable":    {probe: probeHomebrew, remote: fakeRemote{formula: testFormula, failing: "gh api"}, wantState: stateUnknown, wantDetail: "could not read Formula/apogee.rb"},
		"bucket repo absent":    {probe: probeScoop, wantState: stateSetupPending, wantDetail: "no airiclenz/scoop-bucket repository yet"},
		"bucket repo empty":     {probe: probeScoop, remote: fakeRemote{hasBucket: true}, wantState: statePending, wantDetail: "has no bucket/apogee.json"},
		"bucket older":          {probe: probeScoop, remote: fakeRemote{hasBucket: true, bucketManifest: `{"version": "0.24.10"}`}, wantState: statePending, wantDetail: `manifest pins "0.24.10", not 0.24.11`},
		"bucket current":        {probe: probeScoop, remote: fakeRemote{hasBucket: true, bucketManifest: `{"version": "0.24.11"}`}, wantState: stateDone, wantDetail: "manifest pins 0.24.11"},
		"bucket unreadable":     {probe: probeScoop, remote: fakeRemote{hasBucket: true, failing: "gh api"}, wantState: stateUnknown, wantDetail: "could not read the airiclenz/scoop-bucket repository"},
		"winget not set up":     {probe: probeWinget, wantState: stateSetupPending, wantDetail: "first `komac new` pull request has not merged"},
		"winget absent": {probe: probeWinget, remote: fakeRemote{wingetListing: wingetListing("0.24.10")},
			wantState: statePending, wantDetail: "no AiricLenz.Apogee 0.24.11 manifest or open pull request"},
		"winget pull request open": {probe: probeWinget, remote: fakeRemote{wingetListing: wingetListing("0.24.10"), wingetPulls: wingetPull(312345, "0.24.11")},
			wantState: stateDone, wantDetail: "submitted: pull request #312345"},
		"winget merged": {probe: probeWinget, remote: fakeRemote{wingetListing: wingetListing("0.24.10", "0.24.11")},
			wantState: stateDone, wantDetail: "published: manifests/a/AiricLenz/Apogee/0.24.11"},
		"winget pull request for a longer version": {probe: probeWinget, version: "v0.24.1",
			remote:    fakeRemote{wingetListing: wingetListing("0.24.0"), wingetPulls: wingetPull(312345, "0.24.11")},
			wantState: statePending, wantDetail: "no AiricLenz.Apogee 0.24.1 manifest"},
		"winget pull requests unreadable": {probe: probeWinget, remote: fakeRemote{wingetListing: wingetListing("0.24.10"), failing: "gh pr list"},
			wantState: stateUnknown, wantDetail: "could not list the open microsoft/winget-pkgs pull requests"},
		"smoke": {probe: probeSmoke, remote: fakeRemote{tagObject: "tag", release: allAssets}, wantState: statePending, wantDetail: "publish runs it last"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			version := tc.version
			if version == "" {
				version = "v0.24.11"
			}

			step := tc.probe(t.Context(), tc.remote.runner(), ".", mustVersion(t, version))

			if step.State != tc.wantState || !strings.Contains(step.Detail, tc.wantDetail) {
				t.Errorf("step = %s %q, want %s containing %q", step.State, step.Detail, tc.wantState, tc.wantDetail)
			}
		})
	}
}

// TestProbeStepsRunsEveryStepInPublishOrder pins the step list status reports and publish walks.
func TestProbeStepsRunsEveryStepInPublishOrder(t *testing.T) {
	t.Parallel()

	steps := probeSteps(t.Context(), fakeRemote{}.runner(), ".", mustVersion(t, "v0.24.14"))

	var names []string
	for _, step := range steps {
		names = append(names, step.Name)
	}
	want := []string{"tag", "release", "homebrew", "scoop", "winget", "smoke"}
	if !slices.Equal(names, want) {
		t.Errorf("steps = %q, want %q", names, want)
	}
}

// TestProbesNeverReadAFailedCallAsDone pins that a probe whose every command cannot run — gh
// missing, git unauthenticated — reports unknown, never done (smoke, which runs nothing, aside).
func TestProbesNeverReadAFailedCallAsDone(t *testing.T) {
	t.Parallel()
	commands := &fakeRunner{respond: func(recordedCall) (string, error) {
		return "", errors.New(`exec: "gh": executable file not found in $PATH`)
	}}

	for _, step := range probeSteps(t.Context(), commands, ".", mustVersion(t, "v0.24.11")) {
		if step.Name == stepSmoke {
			continue
		}
		if step.State != stateUnknown {
			t.Errorf("step %s = %s %q, want unknown", step.Name, step.State, step.Detail)
		}
	}
}
