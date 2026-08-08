package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

	remote := serverEntry{Name: "remote", Endpoint: "http://remote:8080", APIKey: "remote-key", Model: "remote-model"}
	spare := serverEntry{Name: "spare", Endpoint: "http://spare:8080"}
	// The configured entry a startup-by-name lands on: it is already a row, so nothing is added.
	laptop := serverEntry{Name: "laptop", Endpoint: "http://local:1111", APIKey: "local-key", Model: "local-model"}
	// What an override run resolves to: the endpoint's host as the alias, no name in any file.
	ephemeral := options{
		endpoint:         "http://rented:8080",
		model:            "rented-model",
		apiKey:           "rented-key",
		hostAlias:        "rented",
		startupEphemeral: true,
	}
	ephemeralRow := serverEntry{
		Name: "rented", Endpoint: "http://rented:8080", APIKey: "rented-key", Model: "rented-model",
	}

	tests := []struct {
		name    string
		startup options
		servers []serverEntry
		want    []serverEntry
	}{
		{
			name: "a configured startup synthesizes nothing — it is already a row",
			startup: options{
				endpoint:  laptop.Endpoint,
				model:     laptop.Model,
				apiKey:    laptop.APIKey,
				hostAlias: laptop.Name,
			},
			servers: []serverEntry{remote, laptop, spare},
			// File order, untouched: the startup entry is NOT hoisted to the front.
			want: []serverEntry{remote, laptop, spare},
		},
		{
			name:    "an ephemeral startup is prepended, entries keep file order",
			startup: ephemeral,
			servers: []serverEntry{remote, spare},
			want:    []serverEntry{ephemeralRow, remote, spare},
		},
		{
			name:    "an ephemeral startup against an empty list ⇒ exactly one row",
			startup: ephemeral,
			want:    []serverEntry{ephemeralRow},
		},
		{
			name: "an ephemeral endpoint a configured entry happens to share is still its own row",
			// Endpoint equality no longer decides anything: the override run is on an unnamed
			// server, and the row that says so is what makes the switch away reversible.
			startup: ephemeral,
			servers: []serverEntry{{Name: "same-box", Endpoint: ephemeral.endpoint}},
			want:    []serverEntry{ephemeralRow, {Name: "same-box", Endpoint: ephemeral.endpoint}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := tt.startup
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
		// An override run: the session starts on an endpoint no entry names, which is the one case
		// that still synthesizes a row — and the case that makes switching away reversible.
		startupEphemeral: true,
	}

	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}

	// The picker's rows: the synthesized startup row first, then the configured entry.
	wantChoices := []tui.ServerChoice{
		{Name: "workstation", Endpoint: first.URL},
		{Name: "second", Endpoint: second.URL},
	}
	choices := rec.opts.Servers()
	if len(choices) != len(wantChoices) {
		t.Fatalf("tui.Options.Servers() = %+v; want %+v", choices, wantChoices)
	}
	for i := range wantChoices {
		if choices[i] != wantChoices[i] {
			t.Errorf("Servers()[%d] = %+v; want %+v", i, choices[i], wantChoices[i])
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
	opts := options{
		endpoint:         first.URL,
		model:            "model-a",
		mode:             "ask-before",
		hostAlias:        "workstation",
		workspace:        t.TempDir(),
		configDir:        configHome,
		autoCompact:      true,
		servers:          []serverEntry{{Name: "second", Endpoint: second.URL}},
		startupEphemeral: true, // an override run, so "workstation" is the synthesized row's label
	}

	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if rec.opts.RecordServerChoice == nil {
		t.Fatal("the composition root left the recording seam unwired")
	}
	configPath := filepath.Join(configHome, "config.yaml")

	// The ephemeral startup row: switchable, and deliberately not writable-back — it names no entry.
	if saved, err := rec.opts.RecordServerChoice("workstation"); saved || err != nil {
		t.Errorf("recording the synthesized row = (%v, %v); want (false, nil) — a silent skip", saved, err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Errorf("a skipped recording touched %s (stat err %v); want the file left absent", configPath, err)
	}

	// A configured entry: the choice is written, through the same splice writer every other key uses.
	if saved, err := rec.opts.RecordServerChoice("second"); !saved || err != nil {
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
	if saved, err := rec.opts.RecordServerChoice("second"); saved || err == nil {
		t.Errorf("recording onto an unwritable config = (%v, %v); want (false, an error)", saved, err)
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
		endpoint:         first.URL,
		model:            "model-a",
		mode:             "ask-before",
		hostAlias:        "workstation",
		workspace:        t.TempDir(),
		configDir:        t.TempDir(),
		autoCompact:      true,
		servers:          []serverEntry{{Name: "second", Endpoint: second.URL}},
		startupEphemeral: true, // an override run, so the startup row is synthesized and offered
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

	if got := caps.follow(serverEntry{Name: "pinned", ParallelAgents: 4}); got != 4 {
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
	if got := caps.follow(serverEntry{Name: "open"}); got != 1 {
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
	caps.follow(serverEntry{Name: "here", ParallelAgents: 2})
	caps.observe(6)

	if got := caps.relist([]serverEntry{{Name: "here", ParallelAgents: 5}, {Name: "there", ParallelAgents: 9}}); got != 5 {
		t.Errorf("relist = %d, want the edited pin 5 — and never another entry's 9", got)
	}
	// A cleared pin hands the width back to what the server itself advertised, exactly as clearing
	// `context-window:` hands the window back.
	if got := caps.relist([]serverEntry{{Name: "here"}}); got != 6 {
		t.Errorf("relist with the pin removed = %d, want the observed 6", got)
	}
	if got := caps.relist([]serverEntry{{Name: "elsewhere", ParallelAgents: 8}}); got != 6 {
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

	caps.follow(serverEntry{Name: "open"})
	caps.observe(3)
	installed := len(spy.widths)

	if got := caps.current(); got != 3 {
		t.Errorf("current() = %d, want the discovered 3 the cap already stands at", got)
	}
	// A pin edited into the list mid-session moves what current reports, exactly as it moves what is
	// installed: one resolution, read through two doors.
	caps.relist([]serverEntry{{Name: "open", ParallelAgents: 5}})
	if got := caps.current(); got != 5 {
		t.Errorf("current() after the pin was edited in = %d, want the pin 5", got)
	}
	if len(spy.widths) != installed+1 {
		t.Errorf("the engine saw %v; want only relist's own install — the two current() reads pushed "+
			"nothing", spy.widths[installed:])
	}
}

// The startup half of the same fact: the entry a session was launched on carries its own pin, so a
// session that starts on a pinned server is capped from its first Turn rather than from its first
// beat.
func TestStartupEntryCarriesTheParallelAgentsPin(t *testing.T) {
	t.Parallel()

	entry := startupEntry(options{
		hostAlias:             "here",
		endpoint:              "http://127.0.0.1:1111",
		startupParallelAgents: 4,
	})
	if entry.ParallelAgents != 4 {
		t.Errorf("startupEntry().ParallelAgents = %d, want the resolved startup entry's 4", entry.ParallelAgents)
	}
}
