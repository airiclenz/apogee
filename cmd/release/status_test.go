package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The latest published release the status tests' gh answers with.
const testLatestRelease = `{"publishedAt":"2026-10-08T19:24:24Z","tagName":"v0.24.11"}` + "\n"

// clearedRepo is a checkout whose gate clears: HEAD is a micro-bump over a feature commit, both
// pushed, with CI green on the bump. Bare tags newer than the latest release exist, as every
// shipped feature's micro-bump tags one.
func clearedRepo() fakeRepo {
	return fakeRepo{
		commits:    linearHistory(),
		head:       bumpCommit,
		originMain: bumpCommit,
		greenRuns:  []string{bumpCommit},
		release:    testLatestRelease,
		tags:       "v0.24.10\nv0.24.11\nv0.24.12\nv0.24.13\nv1.8.0\n",
	}
}

// statusJSON runs status over repo with args and decodes its JSON report, returning the error
// main would turn into an exit code.
func statusJSON(t *testing.T, repo fakeRepo, args ...string) (statusReport, error) {
	t.Helper()
	output, err := execute(t, repo.runner(), append([]string{"status", "--json"}, args...)...)
	var report statusReport
	if decodeErr := json.Unmarshal([]byte(output), &report); decodeErr != nil {
		t.Fatalf("status --json printed no JSON report: %v\n%s", decodeErr, output)
	}
	return report, err
}

// TestStatusReportsTheGateBaselineAndHighestTag pins the cleared report end to end: the
// baseline is the latest *published* release while newer bare tags exist, the highest tag sets
// the retracted v1.8.0 aside, and a cleared gate exits 0.
func TestStatusReportsTheGateBaselineAndHighestTag(t *testing.T) {
	t.Parallel()

	report, err := statusJSON(t, clearedRepo(), "v0.24.14")

	if err != nil {
		t.Fatalf("status failed over a cleared gate: %v", err)
	}
	want := statusReport{
		Version:    "v0.24.14",
		HighestTag: "v0.24.13",
		Gate:       gateReport{IsOK: true, Commit: featureCommit, Failures: []string{}},
		Baseline: baselineReport{
			Tag:       "v0.24.11",
			Published: "2026-10-08T19:24:24Z",
			Range:     "v0.24.11..HEAD",
			Commits:   7,
		},
	}
	steps := report.Steps
	report.Steps = nil
	gotJSON, _ := json.Marshal(report)
	wantJSON, _ := json.Marshal(want)
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Errorf("report = %s\nwant     %s", gotJSON, wantJSON)
	}

	// Nothing is published for v0.24.14 and no channel's one-time setup has landed.
	var states []string
	for _, step := range steps {
		states = append(states, step.Name+" "+string(step.State))
	}
	wantStates := []string{"tag pending", "release pending", "homebrew pending", "scoop setup-pending", "winget setup-pending", "smoke pending"}
	if !slices.Equal(states, wantStates) {
		t.Errorf("steps = %q, want %q", states, wantStates)
	}
}

// TestStatusJSONKeysArePinned pins the JSON report's key set: the release skills read these keys,
// so renaming one must fail here first.
func TestStatusJSONKeysArePinned(t *testing.T) {
	t.Parallel()

	output, err := execute(t, clearedRepo().runner(), "status", "--json", "v0.24.14")
	if err != nil {
		t.Fatalf("status failed: %v\n%s", err, output)
	}
	var report map[string]json.RawMessage
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("status --json printed no JSON object: %v\n%s", err, output)
	}
	var gate, baseline map[string]json.RawMessage
	if err := json.Unmarshal(report["gate"], &gate); err != nil {
		t.Fatalf("gate is not an object: %v", err)
	}
	if err := json.Unmarshal(report["baseline"], &baseline); err != nil {
		t.Fatalf("baseline is not an object: %v", err)
	}

	pinned := map[string]struct {
		object map[string]json.RawMessage
		want   []string
	}{
		"report":   {object: report, want: []string{"baseline", "gate", "highestTag", "steps", "version"}},
		"gate":     {object: gate, want: []string{"commit", "failures", "ok"}},
		"baseline": {object: baseline, want: []string{"commits", "published", "range", "tag"}},
	}
	for name, keys := range pinned {
		if got := slices.Sorted(maps.Keys(keys.object)); !slices.Equal(got, keys.want) {
			t.Errorf("%s keys = %v, want %v", name, got, keys.want)
		}
	}
	var steps []map[string]json.RawMessage
	if err := json.Unmarshal(report["steps"], &steps); err != nil {
		t.Fatalf("steps is not an array of objects: %v", err)
	}
	if len(steps) != len(publishSteps) {
		t.Fatalf("steps has %d entries, want %d", len(steps), len(publishSteps))
	}
	for _, step := range steps {
		if got, want := slices.Sorted(maps.Keys(step)), []string{"detail", "name", "state"}; !slices.Equal(got, want) {
			t.Errorf("step keys = %v, want %v", got, want)
		}
	}
}

// TestStatusProbesTheVersionFileOnlyAboveTheBaseline pins when status probes the publish steps
// for the checkout's VERSION: above the baseline a release is in progress and every step is
// probed; at the baseline none is, and no probe command runs.
func TestStatusProbesTheVersionFileOnlyAboveTheBaseline(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		version   string
		wantSteps int
	}{
		"above the baseline": {version: "v0.24.14", wantSteps: len(publishSteps)},
		"at the baseline":    {version: "v0.24.11", wantSteps: 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, versionFile), []byte(tc.version+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			commands := clearedRepo().runner()
			var output bytes.Buffer

			err := runStatus(t.Context(), commands, &output, statusOptions{dir: dir, isJSON: true})

			if err != nil {
				t.Fatalf("status failed: %v\n%s", err, output.String())
			}
			var report statusReport
			if err := json.Unmarshal(output.Bytes(), &report); err != nil {
				t.Fatalf("status --json printed no JSON report: %v", err)
			}
			if len(report.Steps) != tc.wantSteps {
				t.Errorf("steps = %d, want %d", len(report.Steps), tc.wantSteps)
			}
			if tc.wantSteps == 0 && len(commands.recorded("gh")) != 3 {
				t.Errorf("gh ran %d times, want 3 (the gate's two run lists and the baseline): no probe", len(commands.recorded("gh")))
			}
		})
	}
}

// TestStatusFailsOnAStepItCouldNotProbe pins that an unknown step is a run failure (exit 1) even
// when the gate clears, and that the report naming it is still printed.
func TestStatusFailsOnAStepItCouldNotProbe(t *testing.T) {
	t.Parallel()
	repo := clearedRepo()
	repo.remote.failing = "gh api repos/" + tapRepo

	report, err := statusJSON(t, repo, "v0.24.14")

	if err == nil || exitCodeFor(err) != exitRunFailed {
		t.Fatalf("status = %v, want a run failure", err)
	}
	if !strings.Contains(err.Error(), "could not probe the homebrew step") {
		t.Errorf("error = %v, want it to name the homebrew step", err)
	}
	if !report.Gate.IsOK {
		t.Errorf("gate failures = %q, want a cleared gate", report.Gate.Failures)
	}
	if len(report.Steps) != len(publishSteps) || report.Steps[2].State != stateUnknown {
		t.Errorf("steps = %+v, want homebrew unknown", report.Steps)
	}
}

// TestStatusVersionArgument pins where the reported version comes from: the argument when one is
// given, a bare X.Y.Z read as vX.Y.Z, and a malformed one refused as a usage error before any
// command runs.
func TestStatusVersionArgument(t *testing.T) {
	t.Parallel()

	t.Run("bare X.Y.Z reads as vX.Y.Z", func(t *testing.T) {
		t.Parallel()

		report, err := statusJSON(t, clearedRepo(), "0.24.14")

		if err != nil {
			t.Fatalf("status failed: %v", err)
		}
		if report.Version != "v0.24.14" {
			t.Errorf("version = %q, want v0.24.14", report.Version)
		}
	})

	t.Run("a malformed version is a usage error", func(t *testing.T) {
		t.Parallel()
		commands := &fakeRunner{respond: func(recordedCall) (string, error) {
			return "", errors.New("the runner must not be reached")
		}}

		_, err := execute(t, commands, "status", "v0.25.0-rc.1")

		if err == nil || exitCodeFor(err) != exitBadUsage {
			t.Errorf("status v0.25.0-rc.1 = %v, want a usage error", err)
		}
		if len(commands.calls) != 0 {
			t.Errorf("runner was called %d times, want none", len(commands.calls))
		}
	})

	t.Run("two versions are a usage error", func(t *testing.T) {
		t.Parallel()

		_, err := execute(t, clearedRepo().runner(), "status", "v0.24.14", "v0.24.15")

		if err == nil || exitCodeFor(err) != exitBadUsage {
			t.Errorf("status with two versions = %v, want a usage error", err)
		}
	})
}

// TestStatusDefaultsToTheVersionFile pins that with no argument the checkout's VERSION is the
// version reported.
func TestStatusDefaultsToTheVersionFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, versionFile), []byte("v0.24.14\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer

	err := runStatus(t.Context(), clearedRepo().runner(), &output, statusOptions{dir: dir, isJSON: true})

	if err != nil {
		t.Fatalf("status failed: %v\n%s", err, output.String())
	}
	var report statusReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("status --json printed no JSON report: %v", err)
	}
	if report.Version != "v0.24.14" {
		t.Errorf("version = %q, want the VERSION file's v0.24.14", report.Version)
	}
}

// TestStatusFailsAfterPrintingTheReport pins the exit status: a blocked gate, or a check that
// could not run, is a run failure (exit 1) — and the report is still printed, so whoever reads
// it sees what was found.
func TestStatusFailsAfterPrintingTheReport(t *testing.T) {
	t.Parallel()

	blocked := clearedRepo()
	blocked.isDirty = true
	noGitHub := clearedRepo()
	noGitHub.failing = "gh"
	noCount := clearedRepo()
	noCount.failing = "git rev-list --count"

	cases := map[string]struct {
		repo     fakeRepo
		wantGate bool
		wantTag  string
	}{
		"a blocked gate":            {repo: blocked, wantTag: "v0.24.11"},
		"gh cannot run":             {repo: noGitHub},
		"the baseline cannot count": {repo: noCount, wantGate: true, wantTag: "v0.24.11"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			report, err := statusJSON(t, tc.repo, "v0.24.14")

			if err == nil || exitCodeFor(err) != exitRunFailed {
				t.Fatalf("status = %v, want a run failure", err)
			}
			if report.Gate.IsOK != tc.wantGate {
				t.Errorf("gate ok = %t, want %t (failures %q)", report.Gate.IsOK, tc.wantGate, report.Gate.Failures)
			}
			if report.Baseline.Tag != tc.wantTag {
				t.Errorf("baseline tag = %q, want %q", report.Baseline.Tag, tc.wantTag)
			}
		})
	}
}

// TestStatusTextNamesTheBlockingConditions pins the table form: the gate's verdict, its commit
// and each failure appear in the plain-text report.
func TestStatusTextNamesTheBlockingConditions(t *testing.T) {
	t.Parallel()
	repo := clearedRepo()
	repo.isDirty = true

	output, err := execute(t, repo.runner(), "status", "v0.24.14")

	if !errors.Is(err, errGateBlocked) {
		t.Errorf("status = %v, want %v", err, errGateBlocked)
	}
	for _, want := range []string{
		"version      v0.24.14",
		"baseline     v0.24.11 (published 2026-10-08T19:24:24Z), v0.24.11..HEAD: 7 commits",
		"gate         BLOCKED at 200000000000",
		"- the working tree is not clean",
		"\nsteps\n",
		"  tag       pending        origin has no tag v0.24.14\n",
		"  scoop     setup-pending  no airiclenz/scoop-bucket repository yet\n",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output lacks %q:\n%s", want, output)
		}
	}
}
