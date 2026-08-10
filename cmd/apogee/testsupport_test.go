package main

// Shared test fixtures for the cmd/apogee suite: the apogee-home builders and assertions that
// more than one test file leans on. They live here rather than in any one subject's test file so
// no file owns another file's scaffolding.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noNotify drops applyConfig's soft startup notices — its callers assert resolved
// values, not the wording (resolveConfineToWorkspace's own table covers the notices).
func noNotify(string) {}

// startupServerYAML is the smallest upstream a config can describe since ADR 0036 retired the
// top-level `endpoint:` key: one `servers:` entry and the `server:` pointer that starts on it.
// Startup refuses a config that names no server at all, so every test whose subject is some OTHER
// key carries this block to get past selection — the way a real config always will.
const startupServerYAML = "servers:\n  - name: testbox\n    endpoint: http://127.0.0.1:1111\nserver: testbox\n"

// The facts startupServerYAML resolves to, for the tests that assert what selection produced.
const (
	testServerName     = "testbox"
	testServerEndpoint = "http://127.0.0.1:1111"
)

// writeConfigHome writes an apogee home's config.yaml: the caller's own keys, followed by a startup
// server when they name none, so a test states only the keys it is about. A caller that DOES write
// a `servers:` block owns the whole upstream half of the file, `server:` included.
func writeConfigHome(t *testing.T, dir, extra string) {
	t.Helper()
	body := extra
	if !namesServersBlock(extra) {
		body += startupServerYAML
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
}

// namesServersBlock reports whether the yaml opens a top-level `servers:` block — the column-1
// check keeps `mcp-servers:` from counting as one.
func namesServersBlock(yamlText string) bool {
	for _, line := range strings.Split(yamlText, "\n") {
		if strings.HasPrefix(line, "servers:") {
			return true
		}
	}
	return false
}

// testConfigHome is writeConfigHome into a fresh temp dir, returning the home.
func testConfigHome(t *testing.T, extra string) string {
	t.Helper()
	dir := t.TempDir()
	writeConfigHome(t, dir, extra)
	return dir
}

// assertHomeHoldsOnlyConfig fails unless the apogee home still holds nothing but the config.yaml
// the test wrote — the read-only pledge, asserted this way now that a home has to carry a config
// at all for a command to have a server to talk to (ADR 0036).
func assertHomeHoldsOnlyConfig(t *testing.T, home, what string) {
	t.Helper()
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("read the apogee home: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "config.yaml" {
			t.Errorf("%s wrote %q into the apogee home; it must write nothing", what, e.Name())
		}
	}
}

// upstreamHome writes an apogee home whose ONE configured server is endpoint — with the first
// modelHint, if any, as that server's discovery hint — and returns the home. Since ADR 0036 the
// `servers:` list is the single definition of what a command can talk to, so this is how a test
// points a command at its own httptest upstream.
func upstreamHome(t *testing.T, endpoint string, modelHint ...string) string {
	t.Helper()
	entry := "servers:\n  - name: probe-target\n    endpoint: " + endpoint + "\n"
	if len(modelHint) > 0 && modelHint[0] != "" {
		entry += "    model: " + modelHint[0] + "\n"
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte(entry+"server: probe-target\n"), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	return dir
}
