package tools

import (
	"context"
	"os/exec"
	"testing"

	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/subprocess"
)

// capturedRunHost returns the real host with its run replaced by a recorder: the spec a tool
// hands it lands in the returned pointer and nothing is launched, so a test pins the exact argv
// and environment the tool builds on every platform. The host is a value the test owns, so the
// test can run in parallel with every other reader of the real operating system.
func capturedRunHost(t *testing.T) (execHost, *subprocess.SubprocessSpec) {
	t.Helper()
	h := defaultExecHost()
	var captured subprocess.SubprocessSpec
	h.run = func(_ context.Context, spec subprocess.SubprocessSpec) (subprocess.SubprocessResult, error) {
		captured = spec
		return subprocess.SubprocessResult{}, nil
	}
	return h, &captured
}

// fakeLook is a PATH lookup that answers path for EVERY name (found), or exec.LookPath's
// not-found error for every name (!found). It fakes the LOOK alone, never the fence
// security.ResolveProgram applies to what the look answers — a planted path still gets refused.
// A test that needs a per-name answer sets the host's look itself; the field is a func.
func fakeLook(found bool, path string) func(string) (string, error) {
	return func(string) (string, error) {
		if !found {
			return "", exec.ErrNotFound
		}
		return path, nil
	}
}

// fakeLookHost returns the real host with its look replaced by fakeLook(found, path), so a tool
// resolves its program without depending on the test host's PATH.
func fakeLookHost(found bool, path string) execHost {
	h := defaultExecHost()
	h.look = fakeLook(found, path)
	return h
}

// markerHost is a platform.Host a test can recognise by identity: two execHost values cannot be
// compared (func fields, and the real Host's rule table carries a slice), but an interface
// holding a POINTER compares by that pointer, so the tools' shells are compared instead.
type markerHost struct{ platform.Host }

// TestBuiltinToolsShareOneExecHost pins the reason execHost exists: builtinTools builds ONE host
// and every execution tool launches through that same value, so a test — or later a Driver —
// that supplies a host has supplied it to all five, not to whichever tools happened to take it.
func TestBuiltinToolsShareOneExecHost(t *testing.T) {
	t.Parallel()
	marker := &markerHost{Host: platform.Current()}
	h := defaultExecHost()
	h.shell = marker

	shells := map[string]platform.Host{}
	for _, tool := range builtinToolsWith(t.TempDir(), HostTools{}, h) {
		switch typed := tool.(type) {
		case *Terminal:
			shells[tool.Name()] = typed.host.shell
		case *PythonExec:
			shells[tool.Name()] = typed.host.shell
		case *RunTests:
			shells[tool.Name()] = typed.host.shell
		case *Diagnostics:
			shells[tool.Name()] = typed.host.shell
		case *ConsoleOpen:
			shells[tool.Name()] = typed.host.shell
		}
	}

	if len(shells) != 5 {
		t.Fatalf("found %d execution tools in the build, want 5: %v", len(shells), shells)
	}
	for name, shell := range shells {
		if shell != platform.Host(marker) {
			t.Errorf("%s launches through a shell other than the one host builtinTools was handed", name)
		}
	}
}
