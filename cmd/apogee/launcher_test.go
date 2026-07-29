package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	llamalauncher "github.com/airiclenz/llama-launcher/launcher"
)

// fakeLauncher is the CI-side launcherOps: every facade call is scripted, so the bridge's own
// logic — which rows it assembles, in which order, from which reads — is exercised without a
// launcher config on the user's machine, without a server process, and without a socket. The
// call records are what later items assert their closures reached the right verb with.
//
// Only the CONFIG it hands back is a real parsed one (launcherFixture builds it from a temp
// file through the facade's own parser), because ProfileNames and ResolveProfile are the
// launcher's methods on the launcher's type: faking those would fake the very ordering and
// merging this bridge exists to project.
type fakeLauncher struct {
	cfg       *llamalauncher.Config
	cfgErr    error
	notices   []string
	instances []*llamalauncher.RunningInstance

	loadResult    *llamalauncher.RunningInstance
	loadStarted   bool
	loadErr       error
	loadNotices   []string
	loadProgress  []string
	actuateResult *llamalauncher.StopResult
	actuateErr    error

	configPaths   []string
	discoverCalls int
	loadedNames   []string
	stopped       []string
	unloaded      []string
}

func (f *fakeLauncher) loadConfig(path string, notice func(string)) (*llamalauncher.Config, error) {
	f.configPaths = append(f.configPaths, path)
	for _, n := range f.notices {
		if notice != nil {
			notice(n)
		}
	}
	if f.cfgErr != nil {
		return nil, f.cfgErr
	}
	return f.cfg, nil
}

func (f *fakeLauncher) discover(*llamalauncher.Config) []*llamalauncher.RunningInstance {
	f.discoverCalls++
	return f.instances
}

func (f *fakeLauncher) loadProfile(_ *llamalauncher.Config, p *llamalauncher.ResolvedProfile,
	_ bool, progress func(string), notice func(string)) (*llamalauncher.RunningInstance, bool, error) {
	f.loadedNames = append(f.loadedNames, p.Name)
	for _, s := range f.loadProgress {
		if progress != nil {
			progress(s)
		}
	}
	for _, n := range f.loadNotices {
		if notice != nil {
			notice(n)
		}
	}
	return f.loadResult, f.loadStarted, f.loadErr
}

func (f *fakeLauncher) stop(addr string) (*llamalauncher.StopResult, error) {
	f.stopped = append(f.stopped, addr)
	return f.actuateResult, f.actuateErr
}

func (f *fakeLauncher) unload(backend, addr string) (*llamalauncher.StopResult, error) {
	f.unloaded = append(f.unloaded, backend+" "+addr)
	return f.actuateResult, f.actuateErr
}

// The production adapter must keep satisfying the seam its fake stands in for.
var _ launcherOps = (*fakeLauncher)(nil)

// launcherFixture writes a launcher config under a fresh temp directory — creating each named
// model file inside its models_dir first, since llama.cpp profiles resolve to a path on disk —
// and parses it with the FACADE's own loader, so the Config under test is exactly the shape a
// real one has (profile order, defaults merged, backend fallbacks applied).
func launcherFixture(t *testing.T, models []string, body string) *llamalauncher.Config {
	t.Helper()
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	if err := os.MkdirAll(modelsDir, 0o700); err != nil {
		t.Fatalf("create models dir: %v", err)
	}
	for _, name := range models {
		if err := os.WriteFile(filepath.Join(modelsDir, name), []byte("gguf"), 0o600); err != nil {
			t.Fatalf("write model %q: %v", name, err)
		}
	}
	path := filepath.Join(dir, "config.yaml")
	content := "models_dir: " + strconv.Quote(modelsDir) + "\n" + body
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write launcher config: %v", err)
	}
	cfg, err := llamalauncher.LoadConfig(path, nil)
	if err != nil {
		t.Fatalf("parse launcher config fixture: %v", err)
	}
	return cfg
}

// The rows `/load` browses are the launcher's own list, projected: its display order (favourites
// first, then by server, then by name), the resolved address, and the merged context size —
// inherited from defaults where a profile states none. Running is discovery's attribution, from
// a SINGLE sweep however many rows it marks.
func TestLaunchProfilesAssemblesRows(t *testing.T) {
	t.Parallel()

	cfg := launcherFixture(t, []string{"alpha.gguf", "zeta.gguf"}, `
servers:
  llamacpp: true
  ollama: true
defaults:
  server: llamacpp
  host: 127.0.0.1
  context_size: 4096
profiles:
  zeta:
    model: zeta.gguf
    port: 8080
    context_size: 32768
  alpha:
    model: alpha.gguf
    port: 8081
    is_favourite: true
  chat:
    server: ollama
    model: llama3
    port: 11434
`)
	ops := &fakeLauncher{
		cfg: cfg,
		instances: []*llamalauncher.RunningInstance{
			{Backend: "llamacpp", Host: "127.0.0.1", Port: 8080, ActiveProfile: "zeta"},
			// An instance the launcher could not attribute to ONE profile carries no name, and
			// must therefore mark nothing — the ambiguous case, decided on the launcher's side.
			{Backend: "ollama", Host: "127.0.0.1", Port: 11434, ActiveProfile: ""},
		},
	}

	rows, warnings, err := launchProfiles(ops, "/etc/llama-launcher/config.yaml")
	if err != nil {
		t.Fatalf("launchProfiles: unexpected error %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v; want none — every profile resolves", warnings)
	}
	if got := []string{"/etc/llama-launcher/config.yaml"}; len(ops.configPaths) != 1 || ops.configPaths[0] != got[0] {
		t.Errorf("loadConfig paths = %v; want exactly one read of %v (fresh per open, ADR 0029 D4)", ops.configPaths, got)
	}
	if ops.discoverCalls != 1 {
		t.Errorf("discover calls = %d; want exactly 1 sweep for the whole list", ops.discoverCalls)
	}

	want := []launchProfile{
		{Name: "alpha", Backend: "llamacpp", Addr: "127.0.0.1:8081", ContextWindow: 4096},
		{Name: "zeta", Backend: "llamacpp", Addr: "127.0.0.1:8080", ContextWindow: 32768, Running: true},
		{Name: "chat", Backend: "ollama", Addr: "127.0.0.1:11434", ContextWindow: 4096},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %+v; want %d rows %+v", rows, len(want), want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d = %+v; want %+v", i, rows[i], want[i])
		}
	}
}

// One broken profile is a warning against a skipped row, never the end of the list: a model file
// that moved must not cost the user every other profile. And a profile that pins no context size
// reports 0 — the launcher leaves the server's own default in place, so 0 reads as unknown.
func TestLaunchProfilesSkipsUnresolvableRow(t *testing.T) {
	t.Parallel()

	cfg := launcherFixture(t, []string{"present.gguf"}, `
servers:
  llamacpp: true
defaults:
  server: llamacpp
  host: 127.0.0.1
  port: 8080
profiles:
  good:
    model: present.gguf
  moved:
    model: gone.gguf
`)
	ops := &fakeLauncher{cfg: cfg}

	rows, warnings, err := launchProfiles(ops, "config.yaml")
	if err != nil {
		t.Fatalf("launchProfiles: unexpected error %v — one bad profile must not sink the list", err)
	}
	want := launchProfile{Name: "good", Backend: "llamacpp", Addr: "127.0.0.1:8080"}
	if len(rows) != 1 || rows[0] != want {
		t.Fatalf("rows = %+v; want exactly %+v (unset context_size ⇒ 0, unknown)", rows, want)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "moved") {
		t.Errorf("warnings = %v; want one naming the skipped profile %q", warnings, "moved")
	}
}

// Config warnings are the launcher's own voice and travel with the rows, so the caller can print
// them as notes; a config that cannot be read at all is the one failure that sinks the list.
func TestLaunchProfilesCarriesConfigWarningsAndFailure(t *testing.T) {
	t.Parallel()

	cfg := launcherFixture(t, []string{"m.gguf"}, `
servers:
  llamacpp: true
defaults:
  server: llamacpp
  host: 127.0.0.1
  port: 8080
profiles:
  only:
    model: m.gguf
`)
	ops := &fakeLauncher{cfg: cfg, notices: []string{"api_key for llamacpp has trailing whitespace"}}
	rows, warnings, err := launchProfiles(ops, "config.yaml")
	if err != nil {
		t.Fatalf("launchProfiles: unexpected error %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("rows = %+v; want the one profile — a warning is not a refusal", rows)
	}
	if len(warnings) != 1 || warnings[0] != ops.notices[0] {
		t.Errorf("warnings = %v; want the launcher's notice %q verbatim", warnings, ops.notices[0])
	}

	missing := errors.New("config file not found: /nope/config.yaml")
	broken := &fakeLauncher{cfgErr: missing}
	rows, _, err = launchProfiles(broken, "/nope/config.yaml")
	if !errors.Is(err, missing) {
		t.Errorf("err = %v; want the loader's own error passed through", err)
	}
	if rows != nil {
		t.Errorf("rows = %+v; want none when the config could not be read", rows)
	}
	if broken.discoverCalls != 0 {
		t.Errorf("discover calls = %d; want 0 — nothing to mark, and no reason to probe", broken.discoverCalls)
	}
}

// The `llama-launcher:` ladder, resolved where the launcher is known: off is off, a named path is
// taken as written (and `~` expanded) whether or not it exists, and an absent key auto-detects —
// lighting the verbs up only when the launcher's own config is actually there.
func TestLauncherConfigPathLadder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if path, enabled := launcherConfigPath(options{llamaLauncher: "  OFF "}); enabled || path != "" {
		t.Errorf("off ⇒ (%q, %v); want ('', false) — case and spacing are not a way back on", path, enabled)
	}

	explicit := filepath.Join(home, "elsewhere", "launcher.yaml")
	if path, enabled := launcherConfigPath(options{llamaLauncher: explicit}); !enabled || path != explicit {
		t.Errorf("explicit path ⇒ (%q, %v); want (%q, true) — a NAMED config stays on so the first "+
			"verb can say it is missing", path, enabled, explicit)
	}
	if path, enabled := launcherConfigPath(options{llamaLauncher: "~/launcher.yaml"}); !enabled ||
		path != filepath.Join(home, "launcher.yaml") {
		t.Errorf("~ path ⇒ (%q, %v); want the expanded path under %q", path, enabled, home)
	}

	if path, enabled := launcherConfigPath(options{}); enabled || path != "" {
		t.Errorf("auto-detect with no launcher config ⇒ (%q, %v); want ('', false) — a machine "+
			"without the launcher simply has no local-server verbs", path, enabled)
	}

	auto := llamalauncher.DefaultConfigPath()
	if err := os.MkdirAll(filepath.Dir(auto), 0o700); err != nil {
		t.Fatalf("create launcher config dir: %v", err)
	}
	if err := os.WriteFile(auto, []byte("servers:\n  llamacpp: true\n"), 0o600); err != nil {
		t.Fatalf("write launcher config: %v", err)
	}
	if path, enabled := launcherConfigPath(options{}); !enabled || path != auto {
		t.Errorf("auto-detect with a launcher config present ⇒ (%q, %v); want (%q, true)", path, enabled, auto)
	}
}

// The two directions of the address bookkeeping: an endpoint reduces to the host:port the
// launcher acts on, an address expands back to the endpoint the wire uses, and a value that
// names no host or no port is REFUSED rather than guessed — the one mistake here would stop
// somebody else's server.
func TestEndpointAddrBothDirections(t *testing.T) {
	t.Parallel()

	valid := []struct{ endpoint, addr string }{
		{"http://127.0.0.1:8080", "127.0.0.1:8080"},
		{"  http://192.168.64.1:1111  ", "192.168.64.1:1111"},
		{"http://localhost", "localhost:80"},
		{"https://llm.example.com", "llm.example.com:443"},
		{"http://[::1]:8080", "[::1]:8080"},
	}
	for _, tt := range valid {
		t.Run(tt.endpoint, func(t *testing.T) {
			t.Parallel()
			addr, err := endpointAddr(tt.endpoint)
			if err != nil {
				t.Fatalf("endpointAddr(%q) = error %v; want %q", tt.endpoint, err, tt.addr)
			}
			if addr != tt.addr {
				t.Errorf("endpointAddr(%q) = %q; want %q", tt.endpoint, addr, tt.addr)
			}
		})
	}

	// Round trip: an address the launcher reported becomes an endpoint, and reduces back to the
	// same address — the property `/load`'s same-server comparison rests on.
	for _, addr := range []string{"127.0.0.1:8080", "192.168.64.1:1111", "[::1]:11434"} {
		back, err := endpointAddr(addrEndpoint(addr))
		if err != nil {
			t.Fatalf("endpointAddr(addrEndpoint(%q)) = error %v", addr, err)
		}
		if back != addr {
			t.Errorf("round trip of %q = %q; want the address unchanged", addr, back)
		}
	}
	if got := addrEndpoint("127.0.0.1:8080"); got != "http://127.0.0.1:8080" {
		t.Errorf("addrEndpoint = %q; want the plain http:// form of a local server", got)
	}

	garbage := []string{
		"",
		"not a url",
		"127.0.0.1:1111",   // no scheme: url.Parse reads the host as one
		"http://host:port", // not a number
		"ftp://host",       // no port, and no default this side can honestly assume
	}
	for _, endpoint := range garbage {
		t.Run("refuse "+endpoint, func(t *testing.T) {
			t.Parallel()
			if addr, err := endpointAddr(endpoint); err == nil {
				t.Errorf("endpointAddr(%q) = %q; want an error rather than a guessed address", endpoint, addr)
			}
		})
	}
}
