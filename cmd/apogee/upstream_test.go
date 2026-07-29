package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/heartbeat"
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

// The holder is what makes "the same two seams" true across a server switch: Options.Heartbeat is
// wired to its Beat once and for the life of the session, so replacing the Monitor behind it must
// be all it takes to observe another server. The two fake servers advertise different models, so
// only the swap can explain a changed ActiveModel.
func TestUpstreamHolderBeatFollowsTheSwap(t *testing.T) {
	t.Parallel()

	first := upstreamServer(t, "model-a", 4096)
	second := upstreamServer(t, "model-b", 8192)

	holder := newUpstreamHolder(first.URL, heartbeat.NewMonitor(first.URL, "", ""))

	if beat := holder.Beat(context.Background()); !beat.Reachable || beat.ActiveModel != "model-a" {
		t.Fatalf("first beat = %+v; want a reachable model-a from the seeded Monitor", beat)
	}
	if got := holder.Endpoint(); got != first.URL {
		t.Errorf("Endpoint before the swap = %q; want the seeded %q", got, first.URL)
	}

	holder.Swap(second.URL, heartbeat.NewMonitor(second.URL, "", ""))

	if beat := holder.Beat(context.Background()); !beat.Reachable || beat.ActiveModel != "model-b" {
		t.Errorf("beat after Swap = %+v; want a reachable model-b — the holder still observes the old server", beat)
	}
	// The endpoint moves with the Monitor, because "which server is this session on" is the question
	// `/load` asks to decide whether it has to follow the profile it just activated.
	if got := holder.Endpoint(); got != second.URL {
		t.Errorf("Endpoint after the swap = %q; want %q", got, second.URL)
	}
	// The hint moves through the holder too (the rebind closure's line), and lands on the CURRENT
	// Monitor: a hint for a model the swapped-in server does not serve simply falls back, which is
	// the pre-existing "the pin is a hint, not a claim" posture rather than a failure.
	holder.SetModel("model-b")
	if beat := holder.Beat(context.Background()); beat.ActiveModel != "model-b" {
		t.Errorf("beat after SetModel = %+v; want model-b", beat)
	}
}

// Choice assembly is the picker's data: the configured entries in file order, plus a synthesized
// row for the endpoint the session STARTED on — but only when no entry already names it, or the
// list would offer the same server twice under two labels.
func TestUpstreamChoicesAssembly(t *testing.T) {
	t.Parallel()

	startup := options{
		endpoint:  "http://local:1111",
		model:     "local-model",
		apiKey:    "local-key",
		hostAlias: "workstation",
	}
	remote := serverEntry{Name: "remote", Endpoint: "http://remote:8080", APIKey: "remote-key", Model: "remote-model"}
	spare := serverEntry{Name: "spare", Endpoint: "http://spare:8080"}
	// A configured entry pointing at the startup endpoint under its own name.
	named := serverEntry{Name: "home", Endpoint: "http://local:1111", APIKey: "other-key", Model: "other-model"}

	tests := []struct {
		name    string
		servers []serverEntry
		want    []serverEntry
	}{
		{
			name: "no servers block ⇒ the startup endpoint alone",
			want: []serverEntry{{
				Name: "workstation", Endpoint: "http://local:1111", APIKey: "local-key", Model: "local-model",
			}},
		},
		{
			name:    "unlisted startup endpoint is prepended, entries keep file order",
			servers: []serverEntry{remote, spare},
			want: []serverEntry{
				{Name: "workstation", Endpoint: "http://local:1111", APIKey: "local-key", Model: "local-model"},
				remote,
				spare,
			},
		},
		{
			name:    "a configured entry already names the startup endpoint ⇒ nothing synthesized",
			servers: []serverEntry{remote, named},
			want:    []serverEntry{remote, named},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := startup
			opts.servers = tt.servers

			got := upstreamChoices(opts)

			if len(got) != len(tt.want) {
				t.Fatalf("assembled %d choices (%+v); want %d", len(got), got, len(tt.want))
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("choice %d = %+v; want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// The renderer is handed display and identity only: the per-server key and discovery hint are what
// the SWITCH needs, so they stop at the composition root.
func TestServerChoicesCarryNoSecrets(t *testing.T) {
	t.Parallel()

	entries := []serverEntry{
		{Name: "workstation", Endpoint: "http://local:1111", APIKey: "local-key", Model: "local-model"},
		{Name: "remote", Endpoint: "http://remote:8080", APIKey: "remote-key", Model: "remote-model"},
	}

	got := serverChoices(entries)

	want := []tui.ServerChoice{
		{Name: "workstation", Endpoint: "http://local:1111"},
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

	entries := []serverEntry{{Name: "workstation", Endpoint: "http://local:1111"}, {Name: "remote", Endpoint: "http://remote:8080"}}

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
	opts := options{
		endpoint:      first.URL,
		model:         "model-a",
		mode:          "ask-before",
		hostAlias:     "workstation",
		workspace:     t.TempDir(),
		configDir:     configHome,
		contextWindow: 16384, // the global pin, which a switch must not drop
		autoCompact:   true,
		servers:       []serverEntry{{Name: "second", Endpoint: second.URL, Model: "model-b", APIKey: "second-key"}},
	}

	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}

	// The picker's rows: the synthesized startup row first, then the configured entry.
	wantChoices := []tui.ServerChoice{
		{Name: "workstation", Endpoint: first.URL},
		{Name: "second", Endpoint: second.URL},
	}
	if len(rec.opts.Servers) != len(wantChoices) {
		t.Fatalf("tui.Options.Servers = %+v; want %+v", rec.opts.Servers, wantChoices)
	}
	for i := range wantChoices {
		if rec.opts.Servers[i] != wantChoices[i] {
			t.Errorf("Servers[%d] = %+v; want %+v", i, rec.opts.Servers[i], wantChoices[i])
		}
	}
	if rec.opts.Heartbeat == nil || rec.opts.SwitchServer == nil {
		t.Fatal("the composition root left an upstream seam unwired")
	}
	if beat := rec.opts.Heartbeat(context.Background()); beat.ActiveModel != "model-a" {
		t.Fatalf("beat before the switch = %+v; want model-a from the startup server", beat)
	}

	result, err := rec.opts.SwitchServer("second")
	if err != nil {
		t.Fatalf("SwitchServer: %v", err)
	}
	if result.Endpoint != second.URL || result.HostAlias != "second" {
		t.Errorf("result = %+v; want the second server's endpoint and its name as the alias", result)
	}
	if result.ContextWindow != 16384 {
		t.Errorf("result.ContextWindow = %d; want the global 16384 pin, which survives a switch", result.ContextWindow)
	}
	// The seam the renderer keeps calling now observes the other server: the Monitor was swapped
	// behind it, key and hint and all.
	if beat := rec.opts.Heartbeat(context.Background()); beat.ActiveModel != "model-b" {
		t.Errorf("beat after the switch = %+v; want model-b — the holder did not follow the switch", beat)
	}

	// The session metadata no longer claims the old server's model: the switch unbound it.
	if rec.opts.Sessions == nil {
		t.Fatal("tui.Options.Sessions is nil; the session host was not wired")
	}
	if err := rec.opts.Sessions.Save(apogee.Session{}, nil, "switched", 1, 0); err != nil {
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

// A name that resolves to nothing is refused before the engine is touched: the error names the
// candidates and the session keeps observing — and talking to — the server it was on.
func TestRunRootSwitchServerUnknownNameTouchesNothing(t *testing.T) {
	t.Parallel()

	first := upstreamServer(t, "model-a", 4096)
	second := upstreamServer(t, "model-b", 8192)

	rec := &recordingLauncher{}
	opts := options{
		endpoint:    first.URL,
		model:       "model-a",
		mode:        "ask-before",
		hostAlias:   "workstation",
		workspace:   t.TempDir(),
		configDir:   t.TempDir(),
		autoCompact: true,
		servers:     []serverEntry{{Name: "second", Endpoint: second.URL}},
	}

	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}

	result, err := rec.opts.SwitchServer("typo")
	if err == nil {
		t.Fatal("SwitchServer with an unknown name returned no error")
	}
	if result != (tui.ServerSwitchResult{}) {
		t.Errorf("a refused switch still returned %+v; want the zero result", result)
	}
	for _, want := range []string{`"typo"`, "workstation", "second"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if beat := rec.opts.Heartbeat(context.Background()); beat.ActiveModel != "model-a" {
		t.Errorf("beat after the refused switch = %+v; want the unchanged model-a", beat)
	}
}
