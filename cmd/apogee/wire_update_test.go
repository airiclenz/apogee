package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/update"
)

// homebrewExePath is an executable path inside a Homebrew Cellar — what the detection seam hands
// back when a test wants the Homebrew upgrade command named.
const homebrewExePath = "/opt/homebrew/Cellar/apogee/0.1.0/bin/apogee"

// newCountingReleaseServer starts a stand-in for the GitHub repository whose /releases/latest
// redirects to the tag latest, and counts every request it receives.
func newCountingReleaseServer(t *testing.T, latest string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writer.Header().Set("Location", "/releases/tag/"+latest)
		writer.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

// pointUpdateSeams aims the package-level update seams at baseURL and a detection that reports
// inputs, restoring both when the test ends. A test calling it must not be parallel: the seams are
// package vars every root launch reads.
func pointUpdateSeams(t *testing.T, baseURL string, inputs update.Inputs) {
	t.Helper()
	previousURL, previousInputs := updateBaseURL, installInputs
	updateBaseURL = baseURL
	installInputs = func() update.Inputs { return inputs }
	t.Cleanup(func() {
		updateBaseURL, installInputs = previousURL, previousInputs
	})
}

// TestUpdateHostNamesTheNewerReleaseAndTheDetectedCommand drives the enabled path end to end: the
// gate admits a release build, the host asks the stand-in server and names the upgrade command the
// detection seam's Homebrew path selects.
func TestUpdateHostNamesTheNewerReleaseAndTheDetectedCommand(t *testing.T) {
	server, requests := newCountingReleaseServer(t, "v0.25.0")
	pointUpdateSeams(t, server.URL, update.Inputs{ExePath: homebrewExePath, HasVCS: true, DistBuild: true})

	host := updateHostFor(true, "v0.24.11", "v0.24.11")
	if host == nil {
		t.Fatal("updateHostFor(true, release) = nil; want a host for a clean release build with the check on")
	}
	latest, command, ok := host.CheckForUpdate(context.Background())

	if !ok || latest != "v0.25.0" || command != "brew upgrade apogee" {
		t.Errorf("CheckForUpdate = (%q, %q, %t); want (%q, %q, true)", latest, command, ok, "v0.25.0", "brew upgrade apogee")
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("the stand-in release server saw %d requests; want 1", got)
	}
}

// TestUpdateHostSaysNothingWithoutANewerRelease covers the ok=false answers: a published release
// that is not newer, and a server that does not answer with a release redirect.
func TestUpdateHostSaysNothingWithoutANewerRelease(t *testing.T) {
	t.Parallel()
	inputs := func() update.Inputs { return update.Inputs{ExePath: homebrewExePath, HasVCS: true} }
	failing := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failing.Close)
	same, _ := newCountingReleaseServer(t, "v0.24.11")
	cases := []struct {
		name    string
		baseURL string
	}{
		{name: "latest is the running release", baseURL: same.URL},
		{name: "server error", baseURL: failing.URL},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			host := updateHost{current: "v0.24.11", baseURL: testCase.baseURL, inputs: inputs}

			latest, command, ok := host.CheckForUpdate(context.Background())

			if ok || latest != "" || command != "" {
				t.Errorf("CheckForUpdate = (%q, %q, %t); want (\"\", \"\", false)", latest, command, ok)
			}
		})
	}
}

// TestUpdateCheckEnabledAdmitsOnlyACleanReleaseWithTheKeyOn table-tests the enable gate on the
// version strings apogee.BaseVersion and apogee.Version would report.
func TestUpdateCheckEnabledAdmitsOnlyACleanReleaseWithTheKeyOn(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		updateCheck bool
		baseVersion string
		fullVersion string
		want        bool
	}{
		{name: "release archive", updateCheck: true, baseVersion: "v0.24.11", fullVersion: "v0.24.11", want: true},
		{name: "clean source build", updateCheck: true, baseVersion: "v0.24.11", fullVersion: "v0.24.11+436.g28b6f838e6e1", want: true},
		{name: "key off", updateCheck: false, baseVersion: "v0.24.11", fullVersion: "v0.24.11", want: false},
		{name: "dev build", updateCheck: true, baseVersion: "dev", fullVersion: "dev+g28b6f838e6e1", want: false},
		{name: "dirty tree", updateCheck: true, baseVersion: "v0.24.11", fullVersion: "v0.24.11+436.g28b6f838e6e1.dirty", want: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := updateCheckEnabled(testCase.updateCheck, testCase.baseVersion, testCase.fullVersion); got != testCase.want {
				t.Errorf("updateCheckEnabled(%t, %q, %q) = %t; want %t",
					testCase.updateCheck, testCase.baseVersion, testCase.fullVersion, got, testCase.want)
			}
			if host := updateHostFor(testCase.updateCheck, testCase.baseVersion, testCase.fullVersion); (host != nil) != testCase.want {
				t.Errorf("updateHostFor(%t, %q, %q) = %v; want a host exactly when the gate admits",
					testCase.updateCheck, testCase.baseVersion, testCase.fullVersion, host)
			}
		})
	}
}

// TestRootOptionsWireTheUpdateHostOnlyWhenTheCheckIsOn drives the root command to the launcher and
// reads tui.Options.Update: nil under `update-check: false` or APOGEE_NO_UPDATE_CHECK, and with
// neither, wired exactly as the gate decides for this binary's own version. The recording launcher
// never runs the program, so no case sends a request.
func TestRootOptionsWireTheUpdateHostOnlyWhenTheCheckIsOn(t *testing.T) {
	cases := []struct {
		name     string
		extra    string
		env      string
		wantHost bool
	}{
		{name: "update-check false", extra: "update-check: false\n", env: "", wantHost: false},
		{name: "APOGEE_NO_UPDATE_CHECK", extra: "", env: "1", wantHost: false},
		{name: "neither", extra: "", env: "",
			wantHost: updateCheckEnabled(true, apogee.BaseVersion(), apogee.Version())},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv(config.EnvNoUpdateCheck, testCase.env)
			rec := &recordingLauncher{}
			cmd := newRootCommand(rec.launch)
			var out strings.Builder
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{"--workspace", t.TempDir(), "--config", testConfigHome(t, testCase.extra)})

			if err := cmd.ExecuteContext(context.Background()); err != nil {
				t.Fatalf("Execute: %v\n%s", err, out.String())
			}
			if !rec.called {
				t.Fatal("the launcher was not invoked")
			}
			if got := rec.opts.Update != nil; got != testCase.wantHost {
				t.Errorf("tui.Options.Update wired = %t; want %t", got, testCase.wantHost)
			}
		})
	}
}

// TestSuiteEnvironmentTurnsTheUpdateCheckOff pins the regression guard: TestMain sets
// APOGEE_NO_UPDATE_CHECK for every in-process launch and ptyEnv hands it to every PTY launch, so
// no driven run asks the real release server for a newer version.
func TestSuiteEnvironmentTurnsTheUpdateCheckOff(t *testing.T) {
	t.Parallel()
	want := config.EnvNoUpdateCheck + "=" + suiteNoUpdateCheck
	if got := os.Getenv(config.EnvNoUpdateCheck); got != suiteNoUpdateCheck {
		t.Errorf("%s=%q in the suite's environment; TestMain sets it to %q", config.EnvNoUpdateCheck, got, suiteNoUpdateCheck)
	}
	if env := ptyEnv(); !slices.Contains(env, want) {
		t.Errorf("ptyEnv() = %q; want it to carry %s", env, want)
	}
}

// TestHeadlessRunSendsNoUpdateRequest runs one headless invocation with the check enabled in every
// way the TUI's would be — the variable cleared, the key at its default, the seams aimed at a
// stand-in release server — and asserts the server never hears from it: only the interactive TUI
// asks.
func TestHeadlessRunSendsNoUpdateRequest(t *testing.T) {
	server, requests := newCountingReleaseServer(t, "v99.0.0")
	pointUpdateSeams(t, server.URL, update.Inputs{ExePath: homebrewExePath, HasVCS: true})
	t.Setenv(config.EnvNoUpdateCheck, "")
	stub := &stubRunner{}

	out, errOut, err := headlessRunOn(t, stub, nil, fenceableHost, "", "hello")

	if err != nil {
		t.Fatalf("headless run: %v\n%s%s", err, out, errOut)
	}
	if !stub.called {
		t.Fatal("the headless run never reached its runner")
	}
	if got := requests.Load(); got != 0 {
		t.Errorf("a headless run sent %d requests to the release server; want 0", got)
	}
}
