package tools

import (
	"testing"

	"github.com/airiclenz/apogee/internal/platform"
)

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
