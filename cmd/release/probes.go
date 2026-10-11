package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// The repositories and paths the publish steps write to, and so the ones their probes read.
const (
	apogeeRepo        = "airiclenz/apogee"
	tapRepo           = "airiclenz/homebrew-tap"
	formulaPath       = "Formula/apogee.rb"
	bucketRepo        = "airiclenz/scoop-bucket"
	bucketPath        = "bucket/apogee.json"
	wingetRepo        = "microsoft/winget-pkgs"
	wingetPackageID   = "AiricLenz.Apogee"
	wingetManifestDir = "manifests/a/AiricLenz/Apogee"
)

// The publish steps, by the name status and publish report them under.
const (
	stepTag      = "tag"
	stepRelease  = "release"
	stepHomebrew = "homebrew"
	stepScoop    = "scoop"
	stepWinget   = "winget"
	stepSmoke    = "smoke"
)

// stepState is where a publish step stands for one version.
type stepState string

// The states a probe reports. Only stateDone lets publish skip a step; stateUnknown means the
// probe could not tell, and is never read as done.
const (
	// stateDone: the remote already holds the step's result for the version.
	stateDone stepState = "done"
	// statePending: publish has the step to do.
	statePending stepState = "pending"
	// stateSetupPending: the channel's one-time setup has not landed; publish skips the step.
	stateSetupPending stepState = "setup-pending"
	// stateUnknown: the probe could not run, or found the remote in a state publish cannot
	// converge from; the detail names which.
	stateUnknown stepState = "unknown"
)

// stepReport is one publish step's probed state. Its JSON keys are a contract the release skills
// read, like the rest of status's report.
type stepReport struct {
	Name   string    `json:"name"`
	State  stepState `json:"state"`
	Detail string    `json:"detail"`
}

// stepProbe asks the remote, read-only, where one publish step stands for version. dir is the
// checkout whose origin is apogee's repository.
type stepProbe func(ctx context.Context, commands runner, dir string, version releaseVersion) stepReport

// publishStep is one step publish runs, in order, with the probe that tells whether it is done.
type publishStep struct {
	name  string
	probe stepProbe
}

// publishSteps are the steps publish runs, in the order it runs them. status and publish probe
// through this one list, so what status reports done is exactly what publish skips.
var publishSteps = []publishStep{
	{name: stepTag, probe: probeTag},
	{name: stepRelease, probe: probeRelease},
	{name: stepHomebrew, probe: probeHomebrew},
	{name: stepScoop, probe: probeScoop},
	{name: stepWinget, probe: probeWinget},
	{name: stepSmoke, probe: probeSmoke},
}

// formulaVersionPattern finds the version a Homebrew formula pins: its `version "X.Y.Z"` line.
var formulaVersionPattern = regexp.MustCompile(`(?m)^\s*version\s+"([^"]*)"`)

// probeSteps probes every publish step for version, in publish order.
func probeSteps(ctx context.Context, commands runner, dir string, version releaseVersion) []stepReport {
	steps := make([]stepReport, 0, len(publishSteps))
	for _, step := range publishSteps {
		steps = append(steps, step.probe(ctx, commands, dir, version))
	}
	return steps
}

// probeTag reports the tag done only when origin holds an annotated tag for the version: a
// lightweight tag carries no message and is not what a release is cut from, so it is unknown,
// never done.
func probeTag(ctx context.Context, commands runner, dir string, version releaseVersion) stepReport {
	tag := version.String()
	listed, err := commands.run(ctx, dir, nil, "git", "ls-remote", "--tags", remoteName, "refs/tags/"+tag)
	if err != nil {
		return unknownStep(stepTag, "could not list %s's tags: %v", remoteName, err)
	}
	if strings.TrimSpace(listed) == "" {
		return stepReport{Name: stepTag, State: statePending, Detail: fmt.Sprintf("%s has no tag %s", remoteName, tag)}
	}

	kind, err := commands.run(ctx, dir, nil, "gh", "api", "repos/"+apogeeRepo+"/git/ref/tags/"+tag, "--jq", ".object.type")
	if err != nil {
		return unknownStep(stepTag, "could not read the type of tag %s: %v", tag, err)
	}
	switch strings.TrimSpace(kind) {
	case "tag":
		return stepReport{Name: stepTag, State: stateDone, Detail: fmt.Sprintf("annotated tag %s on %s", tag, remoteName)}
	case "commit":
		return unknownStep(stepTag, "%s on %s is a lightweight tag; a release tag must be annotated", tag, remoteName)
	default:
		return unknownStep(stepTag, "tag %s points at a %q object, not an annotated tag", tag, strings.TrimSpace(kind))
	}
}

// probeRelease reports the GitHub release done only when it carries every asset a release ships:
// the six archives and SHA256SUMS, each fully uploaded.
func probeRelease(ctx context.Context, commands runner, dir string, version releaseVersion) stepReport {
	tag := version.String()
	uploaded, isFound, err := readReleaseAssets(ctx, commands, dir, version)
	if err != nil {
		return unknownStep(stepRelease, "could not read GitHub release %s: %v", tag, err)
	}
	if !isFound {
		return stepReport{Name: stepRelease, State: statePending, Detail: fmt.Sprintf("no GitHub release %s", tag)}
	}

	want := releaseAssetNames(version)
	missing := missingAssets(want, uploaded)
	if len(missing) > 0 {
		return stepReport{Name: stepRelease, State: statePending, Detail: fmt.Sprintf(
			"release %s lacks %d of its %d assets: %s", tag, len(missing), len(want), strings.Join(missing, ", "))}
	}
	return stepReport{Name: stepRelease, State: stateDone, Detail: fmt.Sprintf("release %s with all %d assets", tag, len(want))}
}

// readReleaseAssets reads the GitHub release of version: whether it exists and, when it does, the
// names of its fully uploaded assets — an asset in any other state is a failed or unfinished
// upload, not one the release carries.
func readReleaseAssets(ctx context.Context, commands runner, dir string, version releaseVersion) ([]string, bool, error) {
	output, err := commands.run(ctx, dir, nil, "gh", "release", "view", version.String(), "--json", "assets")
	if err != nil {
		if strings.Contains(err.Error(), "release not found") {
			return nil, false, nil
		}
		return nil, false, err
	}
	var release struct {
		Assets []struct {
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"assets"`
	}
	if err := json.Unmarshal([]byte(output), &release); err != nil {
		return nil, false, fmt.Errorf("parse gh release view output: %w", err)
	}

	uploaded := make([]string, 0, len(release.Assets))
	for _, asset := range release.Assets {
		if asset.State == "uploaded" {
			uploaded = append(uploaded, asset.Name)
		}
	}
	return uploaded, true, nil
}

// missingAssets returns the names in want that uploaded lacks, in want's order.
func missingAssets(want, uploaded []string) []string {
	var missing []string
	for _, name := range want {
		if !slices.Contains(uploaded, name) {
			missing = append(missing, name)
		}
	}
	return missing
}

// releaseAssetNames are the files a release of version carries: one archive per release
// platform, then SHA256SUMS.
func releaseAssetNames(version releaseVersion) []string {
	bare := bareRelease(version)
	names := make([]string, 0, len(distTargets)+1)
	for _, target := range distTargets {
		names = append(names, target.archiveName(bare))
	}
	return append(names, checksumsFile)
}

// probeHomebrew reports the tap done when its formula pins the version.
func probeHomebrew(ctx context.Context, commands runner, dir string, version releaseVersion) stepReport {
	formula, err := readRepoFile(ctx, commands, dir, tapRepo, formulaPath)
	if err != nil {
		if isNotFound(err) {
			return stepReport{Name: stepHomebrew, State: statePending, Detail: fmt.Sprintf("%s has no %s", tapRepo, formulaPath)}
		}
		return unknownStep(stepHomebrew, "could not read %s from %s: %v", formulaPath, tapRepo, err)
	}

	fields := formulaVersionPattern.FindStringSubmatch(formula)
	if fields == nil {
		return stepReport{Name: stepHomebrew, State: statePending, Detail: fmt.Sprintf("%s pins no version", formulaPath)}
	}
	return pinnedVersionStep(stepHomebrew, "formula", fields[1], version)
}

// probeScoop reports the bucket setup-pending while its repository does not exist, and done when
// its manifest describes the version. An empty bucket repository is pending, not setup-pending:
// the repository is the one-time setup, the manifest is what publish writes.
func probeScoop(ctx context.Context, commands runner, dir string, version releaseVersion) stepReport {
	if _, err := commands.run(ctx, dir, nil, "gh", "api", "repos/"+bucketRepo, "--jq", ".full_name"); err != nil {
		if isNotFound(err) {
			return stepReport{Name: stepScoop, State: stateSetupPending, Detail: fmt.Sprintf("no %s repository yet", bucketRepo)}
		}
		return unknownStep(stepScoop, "could not read the %s repository: %v", bucketRepo, err)
	}

	raw, err := readRepoFile(ctx, commands, dir, bucketRepo, bucketPath)
	if err != nil {
		if isNotFound(err) {
			return stepReport{Name: stepScoop, State: statePending, Detail: fmt.Sprintf("%s has no %s", bucketRepo, bucketPath)}
		}
		return unknownStep(stepScoop, "could not read %s from %s: %v", bucketPath, bucketRepo, err)
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return stepReport{Name: stepScoop, State: statePending, Detail: fmt.Sprintf("%s is not a readable manifest: %v", bucketPath, err)}
	}
	return pinnedVersionStep(stepScoop, "manifest", manifest.Version, version)
}

// probeWinget reports winget setup-pending until the package exists in winget-pkgs (the first
// `komac new` pull request has merged), and done once the version's manifest is published there
// or an open pull request submits it.
func probeWinget(ctx context.Context, commands runner, dir string, version releaseVersion) stepReport {
	bare := bareRelease(version)
	listing, err := commands.run(ctx, dir, nil, "gh", "api", "repos/"+wingetRepo+"/contents/"+wingetManifestDir)
	if err != nil {
		if isNotFound(err) {
			return stepReport{Name: stepWinget, State: stateSetupPending, Detail: fmt.Sprintf(
				"%s is not in %s yet: the first `komac new` pull request has not merged", wingetPackageID, wingetRepo)}
		}
		return unknownStep(stepWinget, "could not read %s from %s: %v", wingetManifestDir, wingetRepo, err)
	}
	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(listing), &entries); err != nil {
		return unknownStep(stepWinget, "could not parse the %s listing: %v", wingetManifestDir, err)
	}
	for _, entry := range entries {
		if entry.Type == "dir" && entry.Name == bare {
			return stepReport{Name: stepWinget, State: stateDone, Detail: fmt.Sprintf(
				"published: %s/%s in %s", wingetManifestDir, bare, wingetRepo)}
		}
	}

	output, err := commands.run(ctx, dir, nil, "gh", "pr", "list", "--repo", wingetRepo, "--state", "open",
		"--search", wingetPackageID+" "+bare+" in:title", "--json", "number,title,url")
	if err != nil {
		return unknownStep(stepWinget, "could not list the open %s pull requests: %v", wingetRepo, err)
	}
	var pulls []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal([]byte(output), &pulls); err != nil {
		return unknownStep(stepWinget, "could not parse gh pr list output: %v", err)
	}
	for _, pull := range pulls {
		if titleNamesRelease(pull.Title, bare) {
			return stepReport{Name: stepWinget, State: stateDone, Detail: fmt.Sprintf(
				"submitted: pull request #%d %s", pull.Number, pull.URL)}
		}
	}
	return stepReport{Name: stepWinget, State: statePending, Detail: fmt.Sprintf(
		"no %s %s manifest or open pull request in %s", wingetPackageID, bare, wingetRepo)}
}

// probeSmoke reports the smoke test pending always: it is an action publish takes last, not a
// state any remote holds.
func probeSmoke(context.Context, runner, string, releaseVersion) stepReport {
	return stepReport{Name: stepSmoke, State: statePending, Detail: "an action, not a remote state: publish runs it last"}
}

// titleNamesRelease reports whether a winget pull request's title names the package and exactly
// the version — a search for 0.24.1 also finds 0.24.11, which must not count.
func titleNamesRelease(title, bare string) bool {
	hasPackage, hasVersion := false, false
	for _, word := range strings.Fields(title) {
		word = strings.Trim(word, ".,:;()[]")
		hasPackage = hasPackage || word == wingetPackageID
		hasVersion = hasVersion || word == bare
	}
	return hasPackage && hasVersion
}

// pinnedVersionStep reports a channel file done when the version it pins is the release's bare
// version, pending otherwise.
func pinnedVersionStep(name, file, pinned string, version releaseVersion) stepReport {
	bare := bareRelease(version)
	if pinned == bare {
		return stepReport{Name: name, State: stateDone, Detail: fmt.Sprintf("%s pins %s", file, bare)}
	}
	return stepReport{Name: name, State: statePending, Detail: fmt.Sprintf("%s pins %q, not %s", file, pinned, bare)}
}

// readRepoFile reads one file of a GitHub repository through the contents API.
func readRepoFile(ctx context.Context, commands runner, dir, repo, path string) (string, error) {
	encoded, err := commands.run(ctx, dir, nil, "gh", "api", "repos/"+repo+"/contents/"+path, "--jq", ".content")
	if err != nil {
		return "", err
	}
	// The contents API wraps its base64 at 60 columns.
	decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(encoded), ""))
	if err != nil {
		return "", fmt.Errorf("decode %s: %w", path, err)
	}
	return string(decoded), nil
}

// isNotFound reports whether a gh api failure is GitHub answering 404 — the path or repository
// does not exist, or the repository is empty — as opposed to a call that could not be made.
func isNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "(HTTP 404)")
}

// unknownStep is a step whose probe could not tell where it stands.
func unknownStep(name, format string, args ...any) stepReport {
	return stepReport{Name: name, State: stateUnknown, Detail: fmt.Sprintf(format, args...)}
}

// bareRelease is the version without its `v`, as archive names and the channel files carry it.
func bareRelease(version releaseVersion) string {
	return strings.TrimPrefix(version.String(), "v")
}
