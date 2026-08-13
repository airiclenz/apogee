package main

import (
	"errors"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/tui"
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

// The rows `/model` browses are the launcher's own list, projected: its display order (favourites
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

// The predicate both folds ask their question through: a bind address and a dial address may be one
// server, and everything else is two. The machine's own addresses are stated rather than looked up,
// so the LAN cases mean the same thing on a host that happens to hold 192.168.1.50 as on one that
// does not.
func TestSameServerMatchesOneServerSpelledTwice(t *testing.T) {
	t.Parallel()

	machine := func() []net.IP {
		return []net.IP{net.ParseIP("192.168.1.7"), net.ParseIP("fe80::abcd")}
	}
	tests := []struct {
		name     string
		launcher string
		endpoint string
		want     bool
	}{
		{"one spelling twice", "127.0.0.1:1111", "127.0.0.1:1111", true},
		{"a wildcard bind, a loopback dial", "0.0.0.0:1111", "127.0.0.1:1111", true},
		{"a v6 wildcard bind, a v6 loopback dial", "[::]:1111", "[::1]:1111", true},
		{"a v6 wildcard bind, a v4 loopback dial", "[::]:1111", "127.0.0.1:1111", true},
		{"a bare-empty bind host", ":1111", "127.0.0.1:1111", true},
		{"loopback by its one name", "0.0.0.0:1111", "localhost:1111", true},
		{"this machine's own LAN address", "0.0.0.0:1111", "192.168.1.7:1111", true},
		// The case that must not regress: the one mistake available here is stopping somebody
		// else's server, and a wildcard bind is not a claim on the LAN.
		{"a LAN PEER", "0.0.0.0:1111", "192.168.1.50:1111", false},
		{"a name this side cannot vouch for", "0.0.0.0:1111", "remote.invalid:1111", false},
		{"a remote endpoint entirely", "0.0.0.0:1111", "remote.invalid:9999", false},
		{"two ports on one host", "127.0.0.1:1111", "127.0.0.1:2222", false},
		{"two ports, one of them wildcard-bound", "0.0.0.0:1111", "127.0.0.1:2222", false},
		{"a bind host the launcher actually stated", "192.168.1.7:1111", "127.0.0.1:1111", false},
		{"nothing that parses as an address", "not-an-address", "127.0.0.1:1111", false},
		{"nothing on the endpoint side", "0.0.0.0:1111", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sameServerOn(tt.launcher, tt.endpoint, machine); got != tt.want {
				t.Errorf("sameServer(%q, %q) = %v; want %v", tt.launcher, tt.endpoint, got, tt.want)
			}
		})
	}

	// The wired-up form asks the machine itself, on the two answers no host can disagree about.
	if !sameServer("0.0.0.0:1111", "127.0.0.1:1111") {
		t.Errorf("sameServer over the real interfaces refused the wildcard/loopback pair")
	}
	if sameServer("0.0.0.0:1111", "remote.invalid:1111") {
		t.Errorf("sameServer over the real interfaces claimed a server it cannot name")
	}
}

// A profile whose resolved host is the WILDCARD a server binds to reaches the row as the loopback
// address a client on this machine dials it at. The projection belongs here, at the root: the picker
// decides the already-loaded profile's exclusion and the elsewhere-port stamp by comparing this
// string against the session's endpoint, and internal/tui knows nothing of bind addresses (ADR 0029
// D1). An address the launcher actually stated is left exactly as it stands.
func TestLaunchProfilesProjectsTheDialSpelling(t *testing.T) {
	t.Parallel()

	cfg := launcherFixture(t, []string{"wild.gguf", "six.gguf", "named.gguf"}, `
servers:
  llamacpp: true
defaults:
  server: llamacpp
  host: 0.0.0.0
  port: 1111
profiles:
  wild:
    model: wild.gguf
  six:
    model: six.gguf
    host: "::"
    port: 2222
  named:
    model: named.gguf
    host: 192.168.1.50
    port: 3333
`)
	rows, warnings, err := launchProfiles(&fakeLauncher{cfg: cfg}, "config.yaml")
	if err != nil {
		t.Fatalf("launchProfiles: unexpected error %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v; want none — every profile resolves", warnings)
	}

	want := map[string]string{
		"wild":  "127.0.0.1:1111",
		"six":   "[::1]:2222",
		"named": "192.168.1.50:3333",
	}
	got := make(map[string]string, len(rows))
	for _, row := range rows {
		got[row.Name] = row.Addr
	}
	if !maps.Equal(got, want) {
		t.Errorf("row addresses = %v; want %v — a wildcard bind reaches the picker as the spelling "+
			"the session dials, so the two agree by construction", got, want)
	}
}

// The per-entry ladder, which differs from the retired top-level key's in exactly two places: the
// off state is the key being absent (nothing to spell), and `auto` is taken VERBATIM — a machine
// with no launcher config still reports the integration on, so the first verb can name the file it
// wanted.
func TestEntryLauncherPathLadder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	auto := llamalauncher.DefaultConfigPath()
	explicit := filepath.Join(home, "elsewhere", "launcher.yaml")
	tests := []struct {
		name        string
		value       string
		wantPath    string
		wantEnabled bool
	}{
		{name: "absent", value: "", wantPath: "", wantEnabled: false},
		{name: "whitespace only reads as absent", value: "   ", wantPath: "", wantEnabled: false},
		{name: "auto", value: "auto", wantPath: auto, wantEnabled: true},
		{name: "AUTO", value: "AUTO", wantPath: auto, wantEnabled: true},
		{name: "auto with surrounding space", value: " auto ", wantPath: auto, wantEnabled: true},
		{name: "a ~ path expands", value: "~/x.yaml", wantPath: filepath.Join(home, "x.yaml"), wantEnabled: true},
		{name: "an absolute path is taken as written", value: explicit, wantPath: explicit, wantEnabled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, enabled := entryLauncherPath(tt.value)
			if path != tt.wantPath || enabled != tt.wantEnabled {
				t.Errorf("entryLauncherPath(%q) = (%q, %v); want (%q, %v)",
					tt.value, path, enabled, tt.wantPath, tt.wantEnabled)
			}
		})
	}

	// `auto` names the launcher's default config whether or not that file is there: the old key
	// stat-gated because it lit up silently, and an explicit opt-in has nothing to be silent about.
	if _, err := os.Stat(auto); err == nil {
		t.Fatalf("the temp home unexpectedly has a launcher config at %q — the auto cases above "+
			"would then prove nothing about the missing-file behaviour", auto)
	}

	// No home to expand a `~` against: the value survives as written rather than disappearing, so
	// the first verb fails naming the path the user typed.
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	if path, enabled := entryLauncherPath("~/x.yaml"); !enabled || path != "~/x.yaml" {
		t.Errorf("unexpandable ~ path ⇒ (%q, %v); want (\"~/x.yaml\", true) — a configured "+
			"integration is not hidden by a home lookup failure", path, enabled)
	}
}

// The path the verbs read MOVES with the key (ADR 0037): a config named in the `/settings` pane is
// what the next verb reads, and clearing the key switches the integration off from the next verb —
// both without a relaunch, because nothing about this bridge is connected to anything.
func TestLauncherVerbsFollowThePathSwap(t *testing.T) {
	t.Parallel()

	cfg := launcherFixture(t, []string{"alpha.gguf"}, `
servers:
  llamacpp: true
defaults:
  server: llamacpp
  host: 127.0.0.1
profiles:
  alpha:
    model: alpha.gguf
    port: 8080
`)
	ops := &fakeLauncher{cfg: cfg}
	path := newLauncherPath("/etc/llama-launcher/first.yaml", "first")
	wiring := launcherWiring{ops: ops, path: path}

	if _, err := wiring.profiles(); err != nil {
		t.Fatalf("profiles on the startup path: %v", err)
	}
	path.set("/etc/llama-launcher/second.yaml", "second")
	if _, err := wiring.profiles(); err != nil {
		t.Fatalf("profiles after the swap: %v", err)
	}
	want := []string{"/etc/llama-launcher/first.yaml", "/etc/llama-launcher/second.yaml"}
	if !slices.Equal(ops.configPaths, want) {
		t.Errorf("config reads = %v; want %v — the verb reads the file the key names NOW", ops.configPaths, want)
	}

	// Cleared: every verb reports the integration off, in the renderer's own sentence, and none of
	// them touches the launcher at all.
	path.set("", "")
	reads := len(ops.configPaths)
	verbs := map[string]error{}
	_, verbs["profiles"] = wiring.profiles()
	_, verbs["load"] = wiring.load("alpha", nil)
	_, verbs["unload"] = wiring.unload("http://127.0.0.1:8080")
	_, verbs["stop"] = wiring.stop("http://127.0.0.1:8080")
	for verb, err := range verbs {
		if !errors.Is(err, tui.ErrNoLauncher) {
			t.Errorf("%s with the key cleared = %v; want tui.ErrNoLauncher", verb, err)
		}
	}
	if len(ops.configPaths) != reads {
		t.Errorf("a disabled verb still read a config: %v", ops.configPaths[reads:])
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
	// same address — the property a profile load's same-server comparison rests on.
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

// ----------------------------------------------------------------------------
// The start-up restore — remember-model's boot half
// ----------------------------------------------------------------------------

// restoreFixture is the launcher config the boot-restore tests decide against: two profiles on two
// ports, so "the recorded one" and "some other one" are both real answers discovery can give.
func restoreFixture(t *testing.T) *llamalauncher.Config {
	t.Helper()
	return launcherFixture(t, []string{"alpha.gguf", "beta.gguf"}, `
servers:
  llamacpp: true
defaults:
  server: llamacpp
  host: 127.0.0.1
  context_size: 4096
profiles:
  alpha:
    model: alpha.gguf
    port: 8080
  beta:
    model: beta.gguf
    port: 9090
`)
}

// restoreWiring builds the bridge as a session that started on entry would hold it: the entry is the
// whole `servers:` list and the one the launcher path was followed from, so what the restore reads is
// exactly what the file says about the server this session is on.
func restoreWiring(ops launcherOps, entry config.ServerEntry, remember bool, path string) launcherWiring {
	return launcherWiring{
		sessionMover: sessionMover{
			live: newLiveSettings(config.Options{
				Servers: []config.ServerEntry{entry}, HostAlias: entry.Name}, nil),
		},
		ops:      ops,
		path:     newLauncherPath(path, entry.Name),
		remember: func() bool { return remember },
	}
}

// launcherEntry is a launcher-fronted `servers:` entry carrying the recorded pointer — the only shape
// this key is ever valid on (ValidateServers refuses one without `llama-launcher:`).
func launcherEntry(profile string) config.ServerEntry {
	return config.ServerEntry{
		Name: "rig", Endpoint: "http://127.0.0.1:9090",
		LlamaLauncher: "/etc/llama-launcher/config.yaml", LaunchProfile: profile,
	}
}

// Nothing is serving under the launcher, so the recorded profile is what this session opens on: the
// answer names it, and nothing else is decided here — the renderer actuates it through the latch.
func TestRestoreLoadsTheRecordedProfileWhenNothingRuns(t *testing.T) {
	t.Parallel()

	ops := &fakeLauncher{cfg: restoreFixture(t)}
	got, err := restoreWiring(ops, launcherEntry("beta"), true, "/etc/llama-launcher/config.yaml").restore()

	if err != nil {
		t.Fatalf("restore: unexpected error %v", err)
	}
	if want := (tui.ProfileRestore{Load: "beta"}); got != want {
		t.Errorf("restore = %+v; want %+v", got, want)
	}
	if len(ops.configPaths) != 1 || ops.configPaths[0] != "/etc/llama-launcher/config.yaml" {
		t.Errorf("config reads = %v; want one FRESH read of the entry's path (ADR 0029 D4)", ops.configPaths)
	}
	if ops.discoverCalls != 1 {
		t.Errorf("discover calls = %d; want exactly one sweep", ops.discoverCalls)
	}
}

// Design call 9: ANY instance under this launcher yields, whatever profile it serves and whatever port
// it is on — a second model stacked onto the GPU, or a server somebody started by hand and displaced,
// are the two mistakes this rule exists to make impossible. What runs is named when the launcher
// attributed it, and left unnamed when it could not.
func TestRestoreYieldsToARunningInstance(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		instance *llamalauncher.RunningInstance
		want     string
	}{
		{
			name:     "another profile",
			instance: &llamalauncher.RunningInstance{Backend: "llamacpp", Host: "127.0.0.1", Port: 8080, ActiveProfile: "alpha"},
			want:     "the launcher is already serving alpha — beta not restored",
		},
		{
			name:     "an instance the launcher could not attribute",
			instance: &llamalauncher.RunningInstance{Backend: "llamacpp", Host: "127.0.0.1", Port: 8080},
			want:     "a server is already running under the launcher — beta not restored",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ops := &fakeLauncher{cfg: restoreFixture(t), instances: []*llamalauncher.RunningInstance{tc.instance}}
			got, err := restoreWiring(ops, launcherEntry("beta"), true, "/etc/llama-launcher/config.yaml").restore()

			if err != nil {
				t.Fatalf("restore: unexpected error %v", err)
			}
			if want := (tui.ProfileRestore{Note: tc.want}); got != want {
				t.Errorf("restore = %+v; want %+v", got, want)
			}
		})
	}
}

// The recorded profile is ALREADY what the launcher serves, so the session's ordinary start-up bind is
// the restore: nothing is loaded and nothing is said, because announcing a thing that has already
// happened is noise on the first screen of a session.
func TestRestoreIsSilentWhenTheRecordedProfileIsServing(t *testing.T) {
	t.Parallel()

	ops := &fakeLauncher{
		cfg: restoreFixture(t),
		instances: []*llamalauncher.RunningInstance{
			{Backend: "llamacpp", Host: "127.0.0.1", Port: 9090, ActiveProfile: "beta"},
		},
	}
	got, err := restoreWiring(ops, launcherEntry("beta"), true, "/etc/llama-launcher/config.yaml").restore()

	if err != nil {
		t.Fatalf("restore: unexpected error %v", err)
	}
	if got != (tui.ProfileRestore{}) {
		t.Errorf("restore = %+v; want the zero answer — the start-up bind already IS the restore", got)
	}
}

// A pointer the launcher's config no longer defines is a note and nothing else. The pointer stays in
// apogee's file — it is the human's line to repoint or delete — and no discovery is done for a profile
// that could not be loaded anyway.
func TestRestoreNotesAProfileTheLauncherNoLongerDefines(t *testing.T) {
	t.Parallel()

	ops := &fakeLauncher{cfg: restoreFixture(t)}
	got, err := restoreWiring(ops, launcherEntry("gamma"), true, "/etc/llama-launcher/config.yaml").restore()

	if err != nil {
		t.Fatalf("restore: unexpected error %v", err)
	}
	want := tui.ProfileRestore{Note: "launch-profile: gamma is not in the launcher's config — nothing restored"}
	if got != want {
		t.Errorf("restore = %+v; want %+v", got, want)
	}
	if ops.discoverCalls != 0 {
		t.Errorf("discover calls = %d; want none — there is nothing to yield to a running server about", ops.discoverCalls)
	}
}

// A launcher config that cannot be read at all is the one failure the check reports. The renderer
// states it as a note; nothing about the session changes.
func TestRestoreReportsAnUnreadableLauncherConfig(t *testing.T) {
	t.Parallel()

	ops := &fakeLauncher{cfgErr: errors.New("open config.yaml: no such file or directory")}
	got, err := restoreWiring(ops, launcherEntry("beta"), true, "/etc/llama-launcher/config.yaml").restore()

	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("restore error = %v; want the launcher's own read failure", err)
	}
	if got != (tui.ProfileRestore{}) {
		t.Errorf("restore = %+v; want nothing decided from a config nobody could read", got)
	}
}

// The three states that do no launcher I/O AT ALL. Each is the ordinary configuration of a session
// this feature has nothing to say to, and the assertion is about the SILENCE as much as the answer: a
// start-up that reads a launcher config and probes for servers is a start-up the user did not ask for.
func TestRestoreDoesNoLauncherWorkWhenThereIsNothingToRestore(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		entry    config.ServerEntry
		remember bool
		path     string
	}{
		{
			name:  "the remember-model toggle is off",
			entry: launcherEntry("beta"),
			path:  "/etc/llama-launcher/config.yaml",
		},
		{
			name:     "the entry records no launch-profile",
			entry:    config.ServerEntry{Name: "rig", LlamaLauncher: "/etc/llama-launcher/config.yaml"},
			remember: true,
			path:     "/etc/llama-launcher/config.yaml",
		},
		{
			name:     "no launcher fronts this session's server",
			entry:    config.ServerEntry{Name: "rig", LaunchProfile: "beta"},
			remember: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ops := &fakeLauncher{cfg: restoreFixture(t)}
			got, err := restoreWiring(ops, tc.entry, tc.remember, tc.path).restore()

			if err != nil {
				t.Fatalf("restore: unexpected error %v", err)
			}
			if got != (tui.ProfileRestore{}) {
				t.Errorf("restore = %+v; want the zero answer", got)
			}
			if len(ops.configPaths) != 0 || ops.discoverCalls != 0 {
				t.Errorf("launcher work = %d config reads, %d sweeps; want none at all",
					len(ops.configPaths), ops.discoverCalls)
			}
		})
	}
}

// A wiring that composed no toggle answers nothing rather than dereferencing one — the nil-degrade
// every seam on this bridge takes, and the posture of a Driver that has no `remember-model:` at all.
func TestRestoreWithoutAToggleRestoresNothing(t *testing.T) {
	t.Parallel()

	ops := &fakeLauncher{cfg: restoreFixture(t)}
	wiring := restoreWiring(ops, launcherEntry("beta"), true, "/etc/llama-launcher/config.yaml")
	wiring.remember = nil

	got, err := wiring.restore()
	if err != nil {
		t.Fatalf("restore: unexpected error %v", err)
	}
	if got != (tui.ProfileRestore{}) || len(ops.configPaths) != 0 {
		t.Errorf("restore = %+v after %d config reads; want the zero answer and no launcher work",
			got, len(ops.configPaths))
	}
}
