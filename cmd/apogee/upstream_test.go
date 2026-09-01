package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/tui"
)

// upstreamServer serves the one path a beat reads — GET /v1/models — advertising a single model id
// and 404ing everything else (llama.cpp's /props included, so nothing overrides the advertised
// window). Two of them are how a swapped Monitor is told apart at the seam: the beat's ActiveModel
// names which server answered.
func upstreamServer(t *testing.T, modelID string, window int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"`+modelID+`","context_length":`+strconv.Itoa(window)+`}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// serverActsOf is what the projected [tui.ServerHost] says it performs, with an unwired seam
// answering the zero value — the shape every "is it wired" assertion in this package reads now that
// the six Upstream funcs are one interface (ADR 0054).
func serverActsOf(opts tui.Options) tui.ServerActs {
	if opts.Server == nil {
		return tui.ServerActs{}
	}
	return opts.Server.Acts()
}

// The holder is what makes "the same two seams" true across a server switch: [tui.ServerHost.Beat] is
// wired to its Beat once and for the life of the session, so replacing the Monitor behind it must
// be all it takes to observe another server. The two fake servers advertise different models, so
// only the swap can explain a changed ActiveModel.
func TestUpstreamHolderBeatFollowsTheSwap(t *testing.T) {
	t.Parallel()

	first := upstreamServer(t, "model-a", 4096)
	second := upstreamServer(t, "model-b", 8192)

	holder := newUpstreamHolder()
	holder.Bind(first.URL, "key-a", "model-a", heartbeat.NewMonitor(first.URL, "", ""))

	if beat := holder.Beat(context.Background()); !beat.Reachable || beat.ActiveModel != "model-a" {
		t.Fatalf("first beat = %+v; want a reachable model-a from the seeded Monitor", beat)
	}
	if got := holder.Endpoint(); got != first.URL {
		t.Errorf("Endpoint before the swap = %q; want the seeded %q", got, first.URL)
	}
	// The launch-time binding an out-of-band call (the session naming completion) would be built
	// from: the endpoint, the entry's own `model` hint, and the resolved key.
	want := upstreamBinding{Endpoint: first.URL, Model: "model-a", APIKey: "key-a"}
	if got := holder.Binding(); got != want {
		t.Errorf("Binding before the swap = %+v; want the seeded %+v", got, want)
	}

	holder.Swap(second.URL, "key-b", heartbeat.NewMonitor(second.URL, "", ""))

	if beat := holder.Beat(context.Background()); !beat.Reachable || beat.ActiveModel != "model-b" {
		t.Errorf("beat after Swap = %+v; want a reachable model-b — the holder still observes the old server", beat)
	}
	// The endpoint moves with the Monitor, because "which server is this session on" is the question
	// a profile load asks to decide whether it has to follow the profile it just activated.
	if got := holder.Endpoint(); got != second.URL {
		t.Errorf("Endpoint after the swap = %q; want %q", got, second.URL)
	}
	// The key follows the server, and the bound model is CLEARED: a switch unbinds the model, so a
	// call built from this binding names none until the new server's first beat rebinds one — the
	// same clearing the session record's stamped model takes.
	want = upstreamBinding{Endpoint: second.URL, Model: "", APIKey: "key-b"}
	if got := holder.Binding(); got != want {
		t.Errorf("Binding after the swap = %+v; want %+v", got, want)
	}
	// The hint moves through the holder too (the rebind closure's line), and lands on the CURRENT
	// Monitor: a hint for a model the swapped-in server does not serve simply falls back, which is
	// the pre-existing "the pin is a hint, not a claim" posture rather than a failure.
	holder.SetModel("model-b")
	if beat := holder.Beat(context.Background()); beat.ActiveModel != "model-b" {
		t.Errorf("beat after SetModel = %+v; want model-b", beat)
	}
	// A rebind is the moment the hint and the binding become one fact, so the same call records both.
	if got := holder.Binding().Model; got != "model-b" {
		t.Errorf("bound model after SetModel = %q; want model-b — a rebind did not reach the binding", got)
	}
}

// Choice assembly is the picker's data: since ADR 0036 the `servers:` list verbatim, in file order,
// with a synthesized row prepended ONLY for the ephemeral entry a raw endpoint override builds —
// the one server this session can be on that no `servers:` entry could name.
func TestUpstreamChoicesAssembly(t *testing.T) {
	t.Parallel()

	remote := config.ServerEntry{Name: "remote", Endpoint: "http://remote:8080", APIKey: "remote-key", Model: "remote-model"}
	spare := config.ServerEntry{Name: "spare", Endpoint: "http://spare:8080"}
	// The configured entry a startup-by-name lands on: it is already a row, so nothing is added.
	laptop := config.ServerEntry{Name: "laptop", Endpoint: "http://local:1111", APIKey: "local-key", Model: "local-model"}
	// What an override run resolves to: the endpoint's host as the alias, no name in any file.
	ephemeral := config.Options{
		Endpoint:         "http://rented:8080",
		Model:            "rented-model",
		APIKey:           "rented-key",
		HostAlias:        "rented",
		StartupEphemeral: true,
	}
	ephemeralRow := config.ServerEntry{
		Name: "rented", Endpoint: "http://rented:8080", APIKey: "rented-key", Model: "rented-model",
	}

	tests := []struct {
		name    string
		startup config.Options
		servers []config.ServerEntry
		want    []config.ServerEntry
	}{
		{
			name: "a configured startup synthesizes nothing — it is already a row",
			startup: config.Options{
				Endpoint:  laptop.Endpoint,
				Model:     laptop.Model,
				APIKey:    laptop.APIKey,
				HostAlias: laptop.Name,
			},
			servers: []config.ServerEntry{remote, laptop, spare},
			// File order, untouched: the startup entry is NOT hoisted to the front.
			want: []config.ServerEntry{remote, laptop, spare},
		},
		{
			name:    "an ephemeral startup is prepended, entries keep file order",
			startup: ephemeral,
			servers: []config.ServerEntry{remote, spare},
			want:    []config.ServerEntry{ephemeralRow, remote, spare},
		},
		{
			name:    "an ephemeral startup against an empty list ⇒ exactly one row",
			startup: ephemeral,
			want:    []config.ServerEntry{ephemeralRow},
		},
		{
			name: "an ephemeral endpoint a configured entry happens to share is still its own row",
			// Endpoint equality no longer decides anything: the override run is on an unnamed
			// server, and the row that says so is what makes the switch away reversible.
			startup: ephemeral,
			servers: []config.ServerEntry{{Name: "same-box", Endpoint: ephemeral.Endpoint}},
			want:    []config.ServerEntry{ephemeralRow, {Name: "same-box", Endpoint: ephemeral.Endpoint}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := tt.startup
			opts.Servers = tt.servers

			got := upstreamChoices(opts)

			if len(got) != len(tt.want) {
				t.Fatalf("assembled %d choices (%+v); want %d", len(got), got, len(tt.want))
			}
			for i := range tt.want {
				// DeepEqual rather than ==: ServerEntry carries a map since it gained the
				// sub-agent posture keys, so it is no longer a comparable struct.
				if !reflect.DeepEqual(got[i], tt.want[i]) {
					t.Errorf("choice %d = %+v; want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// The renderer is handed display and identity only: the per-server key and discovery hint are what
// the SWITCH needs, so they stop at the composition root. The entry's free-text `description:` DOES
// cross, because it is display and nothing else — the words the `/sub-agents-server` pane offers a
// human choosing between boxes (ADR 0069).
func TestServerChoicesCarryNoSecrets(t *testing.T) {
	t.Parallel()

	entries := []config.ServerEntry{
		{
			Name: "workstation", Endpoint: "http://local:1111", APIKey: "local-key",
			Model: "local-model", Description: "the big box upstairs",
		},
		{Name: "remote", Endpoint: "http://remote:8080", APIKey: "remote-key", Model: "remote-model"},
	}

	got := serverChoices(entries)

	want := []tui.ServerChoice{
		{Name: "workstation", Endpoint: "http://local:1111", Description: "the big box upstairs"},
		{Name: "remote", Endpoint: "http://remote:8080"},
	}
	if len(got) != len(want) {
		t.Fatalf("projected %d choices (%+v); want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("choice %d = %+v; want %+v", i, got[i], want[i])
		}
	}
}

// findServer answers an unknown name with the candidates, because the one surface this error can
// reach is a note the user reads.
func TestFindServerUnknownNameNamesTheCandidates(t *testing.T) {
	t.Parallel()

	entries := []config.ServerEntry{{Name: "workstation", Endpoint: "http://local:1111"}, {Name: "remote", Endpoint: "http://remote:8080"}}

	if got, err := findServer(entries, "remote"); err != nil || got.Endpoint != "http://remote:8080" {
		t.Fatalf("findServer(remote) = %+v, %v; want the remote entry and no error", got, err)
	}

	_, err := findServer(entries, "nope")
	if err == nil {
		t.Fatal("findServer with an unknown name returned no error")
	}
	for _, want := range []string{`"nope"`, "workstation", "remote"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// The whole seam, end to end through runRoot: the assembled choices reach the TUI, the switch
// closure re-points the session at another server, the holder behind the unchanged Heartbeat seam
// follows it, and the session record stops claiming the old server's model — a Save landing in the
// unbound gap before the new server's first beat must not name a model this session left behind.
func TestRunRootSwitchServerRepointsTheSession(t *testing.T) {
	t.Parallel()

	first := upstreamServer(t, "model-a", 4096)
	second := upstreamServer(t, "model-b", 8192)
	configHome := t.TempDir()

	rec := &recordingLauncher{}
	opts := config.Options{
		Endpoint:      first.URL,
		Model:         "model-a",
		Mode:          "ask-before",
		HostAlias:     "workstation",
		Workspace:     t.TempDir(),
		ConfigDir:     configHome,
		ContextWindow: 16384, // the global pin, which a switch must not drop
		AutoCompact:   true,
		Servers:       []config.ServerEntry{{Name: "second", Endpoint: second.URL, Model: "model-b", APIKey: "second-key"}},
		// An override run: the session starts on an endpoint no entry names, which is the one case
		// that still synthesizes a row — and the case that makes switching away reversible.
		StartupEphemeral: true,
	}

	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}

	// The picker's rows: the synthesized startup row first, then the configured entry.
	wantChoices := []tui.ServerChoice{
		{Name: "workstation", Endpoint: first.URL},
		{Name: "second", Endpoint: second.URL},
	}
	choices := rec.opts.Server.List()
	if len(choices) != len(wantChoices) {
		t.Fatalf("tui.ServerHost.List() = %+v; want %+v", choices, wantChoices)
	}
	for i := range wantChoices {
		if choices[i] != wantChoices[i] {
			t.Errorf("Servers()[%d] = %+v; want %+v", i, choices[i], wantChoices[i])
		}
	}
	if acts := serverActsOf(rec.opts); !acts.CanObserve || !acts.CanSwitch {
		t.Fatal("the composition root left an upstream seam unwired")
	}
	if beat := rec.opts.Server.Beat(context.Background()); beat.ActiveModel != "model-a" {
		t.Fatalf("beat before the switch = %+v; want model-a from the startup server", beat)
	}

	result, err := rec.opts.Server.Switch("second")
	if err != nil {
		t.Fatalf("Switch: %v", err)
	}
	if result.Endpoint != second.URL || result.HostAlias != "second" {
		t.Errorf("result = %+v; want the second server's endpoint and its name as the alias", result)
	}
	if result.ContextWindow != 16384 {
		t.Errorf("result.ContextWindow = %d; want the global 16384 pin, which survives a switch", result.ContextWindow)
	}
	// The seam the renderer keeps calling now observes the other server: the Monitor was swapped
	// behind it, key and hint and all.
	if beat := rec.opts.Server.Beat(context.Background()); beat.ActiveModel != "model-b" {
		t.Errorf("beat after the switch = %+v; want model-b — the holder did not follow the switch", beat)
	}

	// The session metadata no longer claims the old server's model: the switch unbound it.
	if rec.opts.Sessions == nil {
		t.Fatal("tui.Options.Sessions is nil; the session host was not wired")
	}
	if err := rec.opts.Sessions.Save(apogee.Session{}, nil, "switched", 1, 0, session.Usage{}, session.Usage{}); err != nil {
		t.Fatalf("Save after the switch: %v", err)
	}
	metas, err := session.NewStore(filepath.Join(configHome, "sessions")).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("one Save produced %d records; want 1", len(metas))
	}
	if metas[0].Model != "" {
		t.Errorf("Meta.Model = %q; want empty — a Save in the unbound gap must not claim the old server's model",
			metas[0].Model)
	}
}

// The recording seam, end to end through runRoot (ADR 0036 decision 2): a name the `servers:` list
// holds is spliced into config.yaml as the entry the NEXT session starts on; a name it does not hold —
// the synthesized ephemeral startup row is the one the picker offers — is skipped silently, without
// so much as creating the file; and a write that cannot land is reported rather than swallowed.
func TestRunRootRecordServerChoiceWritesOnlyConfiguredNames(t *testing.T) {
	t.Parallel()

	first := upstreamServer(t, "model-a", 4096)
	second := upstreamServer(t, "model-b", 8192)
	configHome := t.TempDir()

	rec := &recordingLauncher{}
	opts := config.Options{
		Endpoint:         first.URL,
		Model:            "model-a",
		Mode:             "ask-before",
		HostAlias:        "workstation",
		Workspace:        t.TempDir(),
		ConfigDir:        configHome,
		AutoCompact:      true,
		Servers:          []config.ServerEntry{{Name: "second", Endpoint: second.URL}},
		StartupEphemeral: true, // an override run, so "workstation" is the synthesized row's label
	}

	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if rec.opts.Server == nil {
		t.Fatal("the composition root left the Upstream seam unwired")
	}
	configPath := filepath.Join(configHome, "config.yaml")

	// The ephemeral startup row: switchable, and deliberately not writable-back — it names no entry.
	if saved, err := rec.opts.Server.RecordChoice("workstation"); saved || err != nil {
		t.Errorf("recording the synthesized row = (%v, %v); want (false, nil) — a silent skip", saved, err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Errorf("a skipped recording touched %s (stat err %v); want the file left absent", configPath, err)
	}

	// A configured entry: the choice is written, through the same splice writer every other key uses.
	if saved, err := rec.opts.Server.RecordChoice("second"); !saved || err != nil {
		t.Fatalf("recording a configured entry = (%v, %v); want (true, nil)", saved, err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read the config the recording wrote: %v", err)
	}
	if !strings.Contains(string(data), "server: second") {
		t.Errorf("config.yaml does not carry `server: second`:\n%s", data)
	}

	// A write that cannot land: the seam reports it, so the renderer can warn rather than claim a
	// recording that never happened. A directory where the file belongs is the failure, arranged
	// where no real run could reach it.
	if err := os.Remove(configPath); err != nil {
		t.Fatalf("clear the config: %v", err)
	}
	if err := os.Mkdir(configPath, 0o755); err != nil {
		t.Fatalf("stage the unwritable config: %v", err)
	}
	if saved, err := rec.opts.Server.RecordChoice("second"); saved || err == nil {
		t.Errorf("recording onto an unwritable config = (%v, %v); want (false, an error)", saved, err)
	}
}

// The MODEL recording seam, end to end through runRoot (remember-model): an explicit /model pick is
// spliced into the `model:` key of the `servers:` entry the session is on — and skipped silently,
// without so much as touching the file, on every pick this key cannot honestly carry.
func TestRunRootRecordModelChoiceWritesOnlyWritablePicks(t *testing.T) {
	t.Parallel()

	const picked = "model-b"

	// wire puts a session on entry and hands back the seam the renderer would call plus the file it
	// writes through. The config is staged FIRST because a splice edits the entry the user's own file
	// carries: a config seeded from the embedded template names no server at all.
	wire := func(t *testing.T, remember bool, entry config.ServerEntry, alias string) (func(string) (bool, error), string) {
		t.Helper()
		configHome := t.TempDir()
		configPath := filepath.Join(configHome, "config.yaml")
		staged := "servers:\n  - name: " + entry.Name + "\n    endpoint: " + entry.Endpoint + "\n"
		if entry.LlamaLauncher != "" {
			staged += "    llama-launcher: " + entry.LlamaLauncher + "\n"
		}
		if err := os.WriteFile(configPath, []byte(staged), 0o600); err != nil {
			t.Fatalf("stage the config: %v", err)
		}
		rec := &recordingLauncher{}
		opts := config.Options{
			Endpoint:      entry.Endpoint,
			Model:         "model-a",
			Mode:          "ask-before",
			HostAlias:     alias,
			Workspace:     t.TempDir(),
			ConfigDir:     configHome,
			AutoCompact:   true,
			RememberModel: remember,
			Servers:       []config.ServerEntry{entry},
		}
		if err := runRoot(context.Background(), opts, rec.launch); err != nil {
			t.Fatalf("runRoot: %v", err)
		}
		if rec.opts.RecordModelChoice == nil {
			t.Fatal("the composition root left the model-recording seam unwired")
		}
		return rec.opts.RecordModelChoice, configPath
	}

	// assertUnwritten is what all three skips have in common: the seam said no, and the file the human
	// wrote is exactly as they left it.
	assertUnwritten := func(t *testing.T, record func(string) (bool, error), configPath string) {
		t.Helper()
		before, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read the staged config: %v", err)
		}
		saved, err := record(picked)
		if saved || err != nil {
			t.Errorf("recording = (%v, %v); want (false, nil) — a silent skip", saved, err)
		}
		after, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read the config back: %v", err)
		}
		if string(after) != string(before) {
			t.Errorf("a skipped recording rewrote the config:\n%s\nwant:\n%s", after, before)
		}
	}

	t.Run("a plain entry with the toggle on is written", func(t *testing.T) {
		srv := upstreamServer(t, "model-a", 4096)
		entry := config.ServerEntry{Name: "workbench", Endpoint: srv.URL}
		record, configPath := wire(t, true, entry, entry.Name)

		saved, err := record(picked)
		if !saved || err != nil {
			t.Fatalf("recording a plain entry = (%v, %v); want (true, nil)", saved, err)
		}
		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read the config the recording wrote: %v", err)
		}
		if !strings.Contains(string(data), "model: "+picked) {
			t.Errorf("config.yaml does not carry `model: %s`:\n%s", picked, data)
		}
	})

	t.Run("the toggle off writes nothing at all", func(t *testing.T) {
		srv := upstreamServer(t, "model-a", 4096)
		entry := config.ServerEntry{Name: "workbench", Endpoint: srv.URL}
		record, configPath := wire(t, false, entry, entry.Name)

		assertUnwritten(t, record, configPath)
	})

	// A launcher-fronted entry's `model:` is a deliberately empty discovery hint, and its choice is a
	// Launch profile rather than a wire model id: the pick is bound, and nothing is recorded here.
	t.Run("a launcher-fronted entry is skipped", func(t *testing.T) {
		srv := upstreamServer(t, "model-a", 4096)
		entry := config.ServerEntry{
			Name:          "rig",
			Endpoint:      srv.URL,
			LlamaLauncher: filepath.Join(t.TempDir(), "llama-launcher.yaml"),
		}
		record, configPath := wire(t, true, entry, entry.Name)

		assertUnwritten(t, record, configPath)
	})

	// The synthesized ephemeral startup row names no entry, so there is nothing in the file to splice.
	t.Run("a session on no configured entry is skipped", func(t *testing.T) {
		srv := upstreamServer(t, "model-a", 4096)
		entry := config.ServerEntry{Name: "workbench", Endpoint: srv.URL}
		record, configPath := wire(t, true, entry, "workstation")

		assertUnwritten(t, record, configPath)
	})
}

// The LAUNCH-PROFILE recording seam, end to end through runRoot (remember-model): a committed profile
// load is spliced into the `launch-profile:` key of the entry the session ACTUATES through — the one
// whose `llama-launcher:` key the session's launcher path was resolved from — and skipped silently
// when the toggle is off or no such entry can be named.
func TestRunRootRecordLaunchProfileWritesOntoTheActuatingEntry(t *testing.T) {
	t.Parallel()

	const loaded = "gpt-oss-20b"

	// wire puts a session on entry and hands back the seam the completion fold would call plus the file
	// it writes through. The config is staged first for the model seam's reason: a splice edits the
	// entry the user's own file carries.
	wire := func(t *testing.T, remember bool, entry config.ServerEntry) (func(string) (bool, error), string) {
		t.Helper()
		configHome := t.TempDir()
		configPath := filepath.Join(configHome, "config.yaml")
		staged := "servers:\n  - name: " + entry.Name + "\n    endpoint: " + entry.Endpoint + "\n"
		if entry.LlamaLauncher != "" {
			staged += "    llama-launcher: " + entry.LlamaLauncher + "\n"
		}
		if err := os.WriteFile(configPath, []byte(staged), 0o600); err != nil {
			t.Fatalf("stage the config: %v", err)
		}
		rec := &recordingLauncher{}
		opts := config.Options{
			Endpoint:      entry.Endpoint,
			Model:         "model-a",
			Mode:          "ask-before",
			HostAlias:     entry.Name,
			Workspace:     t.TempDir(),
			ConfigDir:     configHome,
			AutoCompact:   true,
			RememberModel: remember,
			Servers:       []config.ServerEntry{entry},
			// What ApplyConfig flattens off the SELECTED entry: the launcher this session starts
			// actuating through, which is also what names the entry the pointer lands on.
			StartupLauncher: entry.LlamaLauncher,
		}
		if err := runRoot(context.Background(), opts, rec.launch); err != nil {
			t.Fatalf("runRoot: %v", err)
		}
		if rec.opts.Launcher == nil {
			t.Fatal("the composition root left the launcher host — and with it the profile recording — unwired")
		}
		return rec.opts.Launcher.RecordProfile, configPath
	}

	// assertUnwritten is what both skips have in common: the seam said no, and the file the human wrote
	// is exactly as they left it.
	assertUnwritten := func(t *testing.T, record func(string) (bool, error), configPath string) {
		t.Helper()
		before, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read the staged config: %v", err)
		}
		saved, err := record(loaded)
		if saved || err != nil {
			t.Errorf("recording = (%v, %v); want (false, nil) — a silent skip", saved, err)
		}
		after, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read the config back: %v", err)
		}
		if string(after) != string(before) {
			t.Errorf("a skipped recording rewrote the config:\n%s\nwant:\n%s", after, before)
		}
	}

	launcherFronted := func(t *testing.T) config.ServerEntry {
		t.Helper()
		srv := upstreamServer(t, "model-a", 4096)
		return config.ServerEntry{
			Name:          "rig",
			Endpoint:      srv.URL,
			LlamaLauncher: filepath.Join(t.TempDir(), "llama-launcher.yaml"),
		}
	}

	t.Run("the actuating entry carries the pointer", func(t *testing.T) {
		entry := launcherFronted(t)
		record, configPath := wire(t, true, entry)

		saved, err := record(loaded)
		if !saved || err != nil {
			t.Fatalf("recording a committed load = (%v, %v); want (true, nil)", saved, err)
		}
		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read the config the recording wrote: %v", err)
		}
		if !strings.Contains(string(data), "launch-profile: "+loaded) {
			t.Errorf("config.yaml does not carry `launch-profile: %s`:\n%s", loaded, data)
		}
	})

	t.Run("the toggle off writes nothing at all", func(t *testing.T) {
		entry := launcherFronted(t)
		record, configPath := wire(t, false, entry)

		assertUnwritten(t, record, configPath)
	})

	// No launcher key is no actuating entry: nothing fronts this server, so there is no entry a load
	// could be recorded onto — and on this class of entry the model id is what gets remembered instead.
	t.Run("a session with no actuating entry is skipped", func(t *testing.T) {
		srv := upstreamServer(t, "model-a", 4096)
		record, configPath := wire(t, true, config.ServerEntry{Name: "workbench", Endpoint: srv.URL})

		assertUnwritten(t, record, configPath)
	})
}

// The `remember-model:` toggle is LIVE, end to end through runRoot: a session that started with
// remembering off and had it switched on in `/settings` records the very next pick — and one that had
// it switched off records nothing again. Both recording seams are asked, because both read the toggle
// and the whole point of the key is that the two answer the same question.
//
// This is the seam-level half of the pane's live apply (ADR 0037 decision 1): the pane persists the
// key and then applies it, and the apply has to reach the values the composition root's own closures
// read — not a snapshot they captured at launch, which would leave the flip governing nothing until
// the next start.
func TestRunRootRememberModelTogglesLive(t *testing.T) {
	t.Parallel()

	// wire stages a config carrying entry, starts a session on it with remembering OFF, and hands back
	// the renderer's whole seam set plus the file the recordings write through.
	wire := func(t *testing.T, entry config.ServerEntry) (tui.Options, string) {
		t.Helper()
		configHome := t.TempDir()
		configPath := filepath.Join(configHome, "config.yaml")
		staged := "servers:\n  - name: " + entry.Name + "\n    endpoint: " + entry.Endpoint + "\n"
		if entry.LlamaLauncher != "" {
			staged += "    llama-launcher: " + entry.LlamaLauncher + "\n"
		}
		if err := os.WriteFile(configPath, []byte(staged), 0o600); err != nil {
			t.Fatalf("stage the config: %v", err)
		}
		rec := &recordingLauncher{}
		opts := config.Options{
			Endpoint:        entry.Endpoint,
			Model:           "model-a",
			Mode:            "ask-before",
			HostAlias:       entry.Name,
			Workspace:       t.TempDir(),
			ConfigDir:       configHome,
			AutoCompact:     true,
			RememberModel:   false, // the default, and what the flip below has to be able to overrule
			Servers:         []config.ServerEntry{entry},
			StartupLauncher: entry.LlamaLauncher,
		}
		if err := runRoot(context.Background(), opts, rec.launch); err != nil {
			t.Fatalf("runRoot: %v", err)
		}
		if rec.opts.Settings == nil {
			t.Fatal("the composition root left the live-apply dispatcher unwired")
		}
		return rec.opts, configPath
	}

	flip := func(t *testing.T, opts tui.Options, on string) {
		t.Helper()
		if _, err := opts.Settings.Apply("remember-model", on); err != nil {
			t.Fatalf("apply remember-model=%s: %v", on, err)
		}
	}

	t.Run("a plain entry starts and stops recording model picks", func(t *testing.T) {
		srv := upstreamServer(t, "model-a", 4096)
		opts, configPath := wire(t, config.ServerEntry{Name: "workbench", Endpoint: srv.URL})

		if saved, err := opts.RecordModelChoice("model-b"); saved || err != nil {
			t.Fatalf("recording with the toggle off = (%v, %v); want (false, nil)", saved, err)
		}
		flip(t, opts, "true")
		if saved, err := opts.RecordModelChoice("model-b"); !saved || err != nil {
			t.Fatalf("recording after the flip = (%v, %v); want (true, nil) — the flip governs the next pick",
				saved, err)
		}
		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read the config the recording wrote: %v", err)
		}
		if !strings.Contains(string(data), "model: model-b") {
			t.Errorf("config.yaml does not carry `model: model-b`:\n%s", data)
		}

		// And off again: a human who switches remembering off has said to stop writing their picks
		// down, which a session that only ever learned to start would ignore for the rest of its life.
		flip(t, opts, "false")
		before, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read the config back: %v", err)
		}
		if saved, err := opts.RecordModelChoice("model-c"); saved || err != nil {
			t.Errorf("recording after switching off = (%v, %v); want (false, nil)", saved, err)
		}
		after, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read the config back: %v", err)
		}
		if string(after) != string(before) {
			t.Errorf("a skipped recording rewrote the config:\n%s\nwant:\n%s", after, before)
		}
	})

	t.Run("a launcher-fronted entry starts recording profile loads", func(t *testing.T) {
		srv := upstreamServer(t, "model-a", 4096)
		opts, configPath := wire(t, config.ServerEntry{
			Name:          "rig",
			Endpoint:      srv.URL,
			LlamaLauncher: filepath.Join(t.TempDir(), "llama-launcher.yaml"),
		})

		if saved, err := opts.Launcher.RecordProfile("gpt-oss-20b"); saved || err != nil {
			t.Fatalf("recording with the toggle off = (%v, %v); want (false, nil)", saved, err)
		}
		flip(t, opts, "true")
		if saved, err := opts.Launcher.RecordProfile("gpt-oss-20b"); !saved || err != nil {
			t.Fatalf("recording after the flip = (%v, %v); want (true, nil)", saved, err)
		}
		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read the config the recording wrote: %v", err)
		}
		if !strings.Contains(string(data), "launch-profile: gpt-oss-20b") {
			t.Errorf("config.yaml does not carry `launch-profile: gpt-oss-20b`:\n%s", data)
		}
	})
}

// A name that resolves to nothing is refused before the engine is touched: the error names the
// candidates and the session keeps observing — and talking to — the server it was on.
func TestRunRootSwitchServerUnknownNameTouchesNothing(t *testing.T) {
	t.Parallel()

	first := upstreamServer(t, "model-a", 4096)
	second := upstreamServer(t, "model-b", 8192)

	rec := &recordingLauncher{}
	opts := config.Options{
		Endpoint:         first.URL,
		Model:            "model-a",
		Mode:             "ask-before",
		HostAlias:        "workstation",
		Workspace:        t.TempDir(),
		ConfigDir:        t.TempDir(),
		AutoCompact:      true,
		Servers:          []config.ServerEntry{{Name: "second", Endpoint: second.URL}},
		StartupEphemeral: true, // an override run, so the startup row is synthesized and offered
	}

	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}

	result, err := rec.opts.Server.Switch("typo")
	if err == nil {
		t.Fatal("Switch with an unknown name returned no error")
	}
	if result != (tui.ServerSwitchResult{}) {
		t.Errorf("a refused switch still returned %+v; want the zero result", result)
	}
	for _, want := range []string{`"typo"`, "workstation", "second"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if beat := rec.opts.Server.Beat(context.Background()); beat.ActiveModel != "model-a" {
		t.Errorf("beat after the refused switch = %+v; want the unchanged model-a", beat)
	}
}

// parallelAgentsSpy records every width pushed at the engine seam, so a test can say both WHAT the
// cap resolved to and that it was actually installed rather than merely computed.
type parallelAgentsSpy struct{ widths []int }

func (s *parallelAgentsSpy) SetParallelAgents(width int) { s.widths = append(s.widths, width) }

func (s *parallelAgentsSpy) last() int {
	if len(s.widths) == 0 {
		return 0
	}
	return s.widths[len(s.widths)-1]
}

// The cap follows the SERVER (ADR 0039 decision 2), which is the whole reason it is re-stated at
// every arrival: a `/server` switch onto a pinned entry installs that entry's pin, a switch onto an
// unpinned one starts serial, and the slot count the retired server advertised is forgotten rather
// than carried onto a machine that never claimed it.
func TestParallelAgentsCapFollowsTheBoundServer(t *testing.T) {
	t.Parallel()

	spy := &parallelAgentsSpy{}
	caps := newParallelAgentsCap(spy)

	if got := caps.follow(config.ServerEntry{Name: "pinned", ParallelAgents: 4}); got != 4 {
		t.Errorf("follow(pinned 4) = %d, want the pin", got)
	}
	if spy.last() != 4 {
		t.Errorf("installed %v; want the pin pushed at the engine", spy.widths)
	}
	// A pin is never overruled by what the server says about itself.
	if got := caps.observe(2); got != 4 {
		t.Errorf("observe(2) under a pin of 4 = %d, want 4 — discovery never outranks a pin", got)
	}

	// Onto an unpinned server: nothing is claimed, so the floor is serial — and the 2 slots the
	// previous server reported must play no part in it.
	if got := caps.follow(config.ServerEntry{Name: "open"}); got != 1 {
		t.Errorf("follow(unpinned) = %d, want 1 — an unknown server runs one agent at a time", got)
	}
	// …until its own first beat says how wide it is.
	if got := caps.observe(3); got != 3 {
		t.Errorf("observe(3) unpinned = %d, want the discovered 3", got)
	}
	// A beat that could name no width is not evidence the server shrank.
	if got := caps.observe(0); got != 3 {
		t.Errorf("observe(0) = %d, want the last 3 — a silent beat is not an observation", got)
	}
}

// A `servers:` list the human edits mid-session (ADR 0037) moves the cap of the server the session
// is ALREADY on: the entry is matched back by name, the observed slot count is kept — the file
// changed, the server did not — and a list that no longer names this session's server changes
// nothing.
func TestParallelAgentsCapRelistMovesThePinInPlace(t *testing.T) {
	t.Parallel()

	spy := &parallelAgentsSpy{}
	caps := newParallelAgentsCap(spy)
	caps.follow(config.ServerEntry{Name: "here", ParallelAgents: 2})
	caps.observe(6)

	if got := caps.relist([]config.ServerEntry{{Name: "here", ParallelAgents: 5}, {Name: "there", ParallelAgents: 9}}); got != 5 {
		t.Errorf("relist = %d, want the edited pin 5 — and never another entry's 9", got)
	}
	// A cleared pin hands the width back to what the server itself advertised, exactly as clearing
	// `context-window:` hands the window back.
	if got := caps.relist([]config.ServerEntry{{Name: "here"}}); got != 6 {
		t.Errorf("relist with the pin removed = %d, want the observed 6", got)
	}
	if got := caps.relist([]config.ServerEntry{{Name: "elsewhere", ParallelAgents: 8}}); got != 6 {
		t.Errorf("relist without this session's server = %d, want 6 unchanged", got)
	}
	if spy.last() != 6 {
		t.Errorf("installed %v; want every re-resolution pushed at the engine", spy.widths)
	}
}

// current answers the question follow and observe answer, for the caller that composes a Config of
// its own instead of mutating the running engine — a scheduled Firing. What the assertion is really
// about is the ENGINE: a read must install nothing, or a Schedule's cadence would be re-stating the
// running Agent's cap from the scheduler's goroutine for no reason.
func TestParallelAgentsCapCurrentReadsWithoutInstalling(t *testing.T) {
	t.Parallel()

	spy := &parallelAgentsSpy{}
	caps := newParallelAgentsCap(spy)

	// Nothing bound yet, so the honest answer is the serial floor — and it is still only an answer.
	if got := caps.current(); got != 1 {
		t.Errorf("current() before any bind = %d, want the serial floor 1", got)
	}
	if len(spy.widths) != 0 {
		t.Errorf("current() installed %v; a read must push nothing at the engine", spy.widths)
	}

	caps.follow(config.ServerEntry{Name: "open"})
	caps.observe(3)
	installed := len(spy.widths)

	if got := caps.current(); got != 3 {
		t.Errorf("current() = %d, want the discovered 3 the cap already stands at", got)
	}
	// A pin edited into the list mid-session moves what current reports, exactly as it moves what is
	// installed: one resolution, read through two doors.
	caps.relist([]config.ServerEntry{{Name: "open", ParallelAgents: 5}})
	if got := caps.current(); got != 5 {
		t.Errorf("current() after the pin was edited in = %d, want the pin 5", got)
	}
	if len(spy.widths) != installed+1 {
		t.Errorf("the engine saw %v; want only relist's own install — the two current() reads pushed "+
			"nothing", spy.widths[installed:])
	}
}

// A move is an arrival too (ADR 0039), and the shared fold is where every arrival that is not a bind
// makes the cap follow: the entry the session lands on supplies the pin, and that written pin outranks
// whatever the new server's beats go on to observe.
func TestMoveReFollowsTheParallelAgentsCap(t *testing.T) {
	t.Parallel()

	spy := &parallelAgentsSpy{}
	caps := newParallelAgentsCap(spy)
	holder := newUpstreamHolder()
	holder.Bind("http://old.invalid:1111", "old-key", "old-model",
		heartbeat.NewMonitor("http://old.invalid:1111", "old-model", "old-key"))
	mover := sessionMover{
		agent: &fakeSwitcher{}, holder: holder, host: &fakeStamper{},
		live: newLiveSettings(config.Options{}, nil), keys: config.NewKeyResolver(""), caps: caps,
	}

	pinned := config.ServerEntry{
		Name: "workstation", Endpoint: "http://192.168.64.1:1111", ParallelAgents: 2,
	}
	if _, err := mover.move(pinned); err != nil {
		t.Fatalf("move onto the pinned entry: %v", err)
	}

	if got := spy.last(); got != 2 {
		t.Errorf("the width the move pushed = %d, want the arrived-on entry's pin 2", got)
	}
	if got := caps.observe(8); got != 2 {
		t.Errorf("cap after a beat naming 8 slots = %d, want 2 — a written pin outranks observation", got)
	}
}

// The startup half of the same fact: the entry a session was launched on carries its own pin, so a
// session that starts on a pinned server is capped from its first Turn rather than from its first
// beat.
func TestStartupEntryCarriesTheParallelAgentsPin(t *testing.T) {
	t.Parallel()

	entry := startupEntry(config.Options{
		HostAlias:             "here",
		Endpoint:              "http://127.0.0.1:1111",
		StartupParallelAgents: 4,
	})
	if entry.ParallelAgents != 4 {
		t.Errorf("startupEntry().ParallelAgents = %d, want the resolved startup entry's 4", entry.ParallelAgents)
	}
}

// The notice a rebind adds when the model it bound is one the server never advertised. Silence is
// half the contract: an advertised model and a no-hint start are ordinary, and a line printed about
// them would teach the human to ignore the one that matters.
func TestHintNoticeSpeaksOnlyForAnUnadvertisedModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		model        string
		grade        provider.HintResolution
		window       int
		bound        int
		want         string
		wantContains []string
	}{
		{
			name:   "an advertised model is unremarkable",
			model:  "served",
			grade:  provider.HintExact,
			window: 128000,
			bound:  128000,
			want:   "",
		},
		{
			name:   "so is the model a session with no hint fell back to",
			model:  "first",
			grade:  provider.HintFirstAdvertised,
			window: 4096,
			bound:  4096,
			want:   "",
		},
		{
			name:   "no beat has graded this model yet",
			model:  "picked",
			grade:  "",
			window: 4096,
			bound:  4096,
			want:   "",
		},
		{
			// The live OpenRouter case: the variant slug is on the wire, the base entry supplied
			// the window, and the notice says both.
			name:         "a variant slug credits the base entry it inherited its window from",
			model:        "deepseek/deepseek-v4-pro:exacto",
			grade:        provider.HintBaseSlug,
			window:       1048576,
			bound:        1048576,
			wantContains: []string{"deepseek/deepseek-v4-pro:exacto", "not advertised", "base 'deepseek/deepseek-v4-pro'", "1M"},
		},
		{
			// A pin outranks the observation, so the base entry no longer supplied the number in
			// force and must not be credited with it.
			name:         "a pinned window is reported as the window, not as the base's",
			model:        "deepseek/deepseek-v4-pro:exacto",
			grade:        provider.HintBaseSlug,
			window:       1048576,
			bound:        200000,
			wantContains: []string{"not advertised", "context window: 195k"},
		},
		{
			name:         "an unlisted id is used as configured with no window at all",
			model:        "my-alias",
			grade:        provider.HintTrusted,
			window:       0,
			bound:        0,
			wantContains: []string{"my-alias", "not advertised", "unknown", "Budget"},
		},
		{
			name:         "an unlisted id under a pin still has a window to report",
			model:        "my-alias",
			grade:        provider.HintTrusted,
			window:       0,
			bound:        32768,
			wantContains: []string{"my-alias", "not advertised", "context window: 32k"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := hintNotice(tt.model, tt.grade, tt.window, tt.bound)
			if len(tt.wantContains) == 0 {
				if got != tt.want {
					t.Fatalf("hintNotice(%q, %q) = %q, want silence", tt.model, tt.grade, got)
				}
				return
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("hintNotice(%q, %q) = %q, want it to name %q", tt.model, tt.grade, got, want)
				}
			}
		})
	}
}

// The grade is a statement about ONE id against one advertised list, so the observer answers for the
// model it was observed for and for no other: a human picking another model rebinds before any beat
// has graded it, and inheriting the retired id's grade would explain that binding with the wrong
// evidence — the very "not advertised" line an advertised pick must never print.
func TestHintObserverAnswersOnlyForTheModelItObserved(t *testing.T) {
	t.Parallel()

	var hints hintObserver

	if got := hints.gradeFor("anything"); got != "" {
		t.Errorf("gradeFor before any beat = %q, want no grade", got)
	}

	hints.observe("my-alias", provider.HintTrusted)
	if got := hints.gradeFor("my-alias"); got != provider.HintTrusted {
		t.Errorf("gradeFor(observed) = %q, want %q", got, provider.HintTrusted)
	}
	if got := hints.gradeFor("served"); got != "" {
		t.Errorf("gradeFor(another model) = %q, want no grade", got)
	}

	// An unreachable beat names no model; it is not evidence the hint stopped resolving.
	hints.observe("", provider.HintFirstAdvertised)
	if got := hints.gradeFor("my-alias"); got != provider.HintTrusted {
		t.Errorf("gradeFor after a silent beat = %q, want the last real observation %q", got, provider.HintTrusted)
	}
}
