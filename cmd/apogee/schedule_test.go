package main

// The composition root's half of the scheduler (ADR 0033): what a Firing is composed FROM, when it
// is allowed to start, and that the whole thing dies with the TUI. The library's own policy —
// cycles, the overlap skip, the lifetime — is proven on a fake clock in internal/schedule, and one
// Firing's engine behaviour in internal/run; nothing here re-proves either.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/library"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/schedule"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/skills"
	"github.com/airiclenz/apogee/internal/tui"
)

// gateSettleWindow is how long a test waits before concluding that a blocked Gate really is
// blocked. Nothing under test should ever consume it on the release path — those assertions wait on
// a channel — so it buys only the negative claim, and buys it cheaply.
const gateSettleWindow = 50 * time.Millisecond

// firingUpstream starts a server that answers every request with one final, no-tool reply and
// records what each request carried: the tool menu it offered — which is how the "a Firing carries
// the library's own registry, never the session's" prescription is asserted from the wire rather
// than from the Config — and its system prompt, which is where an enabled request-shaping Mechanism
// leaves its mark, and so where the wire says which enable set the Firing resolved.
func firingUpstream(t *testing.T, reply string) (url string, menus func() [][]string, systems func() []string) {
	t.Helper()
	var seen [][]string
	var prompts []string
	done := make(chan struct{}, 32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		_ = json.Unmarshal(body, &decoded)
		menu := make([]string, 0, len(decoded.Tools))
		for _, tl := range decoded.Tools {
			menu = append(menu, tl.Function.Name)
		}
		seen = append(seen, menu)
		system := ""
		for _, m := range decoded.Messages {
			if m.Role == "system" {
				system = m.Content
				break
			}
		}
		prompts = append(prompts, system)
		w.Header().Set("Content-Type", "text/event-stream")
		e2eWriteFinal(w, reply)
		done <- struct{}{}
	}))
	t.Cleanup(srv.Close)
	// The handler appends without a lock, so the reader waits for the request it asked for rather
	// than racing the server goroutine. A Firing makes exactly one call in these tests. systems does
	// NOT wait — it reads what the requests released by menus carried, so it is called after it.
	return srv.URL, func() [][]string {
			<-done
			return seen
		}, func() []string {
			return prompts
		}
}

// sessionOnlyTool names anything the interactive session's registry carries that a Firing must not
// inherit — an MCP server's tool, in production (ADR 0008: external effects are re-established per
// session, never shared into a second concurrent Agent). Nothing hands it to a Firing any more:
// scheduleWiring has no Tools seam at all since the Config is composed by firingConfig rather than
// copied off the session's, which is the stronger, structural form of the same guarantee. It is
// declared read-only so Plan mode would genuinely offer it, and its ABSENCE from the wire menu below
// is what says the composed run reached the library's own registry and nothing else.
type sessionOnlyTool struct{}

func (sessionOnlyTool) Name() string            { return "session_only_tool" }
func (sessionOnlyTool) Description() string     { return "a tool only the session's registry has" }
func (sessionOnlyTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (sessionOnlyTool) ReadOnly() bool          { return true }
func (sessionOnlyTool) Execute(context.Context, apogee.ToolCall) (apogee.ToolResult, error) {
	return apogee.ToolResult{}, nil
}

// firingStepPrompt is a complex, action-intent multi-step ask — the shape internal/mechanisms'
// own decompose tests use — so that an ENABLED decompose hints a step into the system prompt of the
// firing's very first request. That injection is the wire's evidence below; the reply is stubbed, so
// nothing here depends on the prompt being answerable.
const firingStepPrompt = "Build a full parser pipeline.\n" +
	"1. First, read the grammar spec.\n" +
	"2. Then create the tokenizer in `lexer.go`.\n" +
	"3. Finally, delegate to a sub-agent to write the tests."

// decomposeStepHintLead is decompose's own step-hint marker (internal/mechanisms, unexported), the
// literal the Mechanism prepends to the hint it appends to the system prompt.
const decomposeStepHintLead = "Apogee step hint:"

// TestScheduleFiringRunsAgainstTheCurrentBinding is the item's headline: the Fire seam composes one
// headless run from the binding the session holds AT FIRE TIME, in the Schedule's own mode, and the
// record it leaves behind is an ordinary session record marked with the Schedule's identity. Both
// halves of that binding are the holder's: the wire the Firing dials AND the endpoint its spec
// resolution keys on.
func TestScheduleFiringRunsAgainstTheCurrentBinding(t *testing.T) {
	t.Parallel()

	url, menus, systems := firingUpstream(t, "the build is green")
	roots, err := resolveRoots(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	store := session.NewStore(roots.sessions)

	// The endpoint-keyed half of the resolution, fixtured so the wire can report which endpoint it
	// was keyed on: the identity ladder's behavioral rung is (probe dir, endpoint, model label), so
	// the record `apogee probe model` left for the server this session MOVED to is found only when
	// the bound endpoint reaches the resolver. Found, the identity is medium-confidence and the
	// matching Validated set APPLIES; missed, the bare label resolves at low confidence and the set
	// is merely offered (validatedsets.go). The set holds decompose alone, so exactly one Mechanism's
	// mark can appear on the wire.
	if _, err := library.SaveProbeRecord(roots.probe, library.ProbeRecord{
		Endpoint:   url,
		ModelLabel: "bound-model",
		ProbedAt:   mustTime(t, "2026-07-22T10:00:00Z"),
		Behavior:   "probe:1:tools+json+chain",
	}); err != nil {
		t.Fatalf("save probe record: %v", err)
	}
	writeUserValidatedEntry(t, roots.validated, "bound-model", `{
		"version": 1,
		"key": "bound-model",
		"set": ["decompose"],
		"evidence": {"campaign": "schedule-test"}
	}`)

	launchOpts := config.Options{
		Endpoint: "http://launch.invalid", Model: "launch-model", ValidatedSetsEnable: true,
	}
	w := scheduleWiring{
		roots: roots,
		live:  newLiveSettings(launchOpts, nil),
		// The binding the session has MOVED to since launch (a /server switch, a rebind). The Firing
		// must follow it rather than the launch values the holder was seeded with.
		binding: func() upstreamBinding { return upstreamBinding{Endpoint: url, Model: "bound-model"} },
		width:   func() int { return 1 },
		store:   store,
	}

	out, err := w.fire(context.Background(), schedule.Firing{
		ScheduleID:   "sch-1-abcd",
		ScheduleName: "Nightly build",
		Prompt:       firingStepPrompt,
		Mode:         domain.ModePlan,
	})
	if err != nil {
		t.Fatalf("fire: %v", err)
	}
	if out.RecordID == "" {
		t.Error("Outcome.RecordID is empty; the firing's record was not persisted into the session store")
	}
	if out.Title == "" {
		t.Error("Outcome.Title is empty; the completed notice would have nothing to name")
	}

	metas, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("the store holds %d records, want the one the firing saved", len(metas))
	}
	meta := metas[0]
	if meta.ScheduleID != "sch-1-abcd" || meta.ScheduleName != "Nightly build" {
		t.Errorf("record identity = (%q, %q), want the firing's (%q, %q)",
			meta.ScheduleID, meta.ScheduleName, "sch-1-abcd", "Nightly build")
	}
	if meta.Model != "bound-model" {
		t.Errorf("record model = %q, want the CURRENT binding's %q — the firing followed the launch "+
			"values instead of the holder", meta.Model, "bound-model")
	}
	if meta.Title != out.Title {
		t.Errorf("record title = %q, want the reported %q", meta.Title, out.Title)
	}

	menu := menus()
	if len(menu) != 1 {
		t.Fatalf("the upstream answered %d requests, want 1", len(menu))
	}
	if len(menu[0]) == 0 {
		t.Error("the firing offered no tools at all; it should carry the library's own registry")
	}
	for _, name := range menu[0] {
		if name == (sessionOnlyTool{}).Name() {
			t.Errorf("the firing offered %q — it inherited the session's registry, MCP tools and all",
				name)
		}
	}

	// The spec resolution's own endpoint, read off the wire: decompose is default-off and reached
	// this run only through the Validated set the probe record promoted, so its step hint standing in
	// the system prompt is the proof that the resolver was handed the BOUND endpoint. Keyed on the
	// launch snapshot's `http://launch.invalid` the record is missed, the set is offered rather than
	// applied, and this system prompt carries no such mark.
	sys := systems()
	if len(sys) != 1 {
		t.Fatalf("recorded %d system prompts, want the one request's", len(sys))
	}
	if !strings.Contains(sys[0], decomposeStepHintLead) {
		t.Errorf("the firing's system prompt carries no %q: the validated set was not applied, so the "+
			"spec resolution keyed on the LAUNCH endpoint rather than the bound one.\nsystem prompt: %q",
			decomposeStepHintLead, sys[0])
	}
}

// A per-model resolution the current binding cannot satisfy — an unreadable system-prompt file for
// the model the session has moved to — fails the Firing rather than running it against the wrong
// prompt. The scheduler words that as its failed Event; nothing is saved.
func TestScheduleFiringReportsAPerModelResolutionFailure(t *testing.T) {
	t.Parallel()

	roots, err := resolveRoots(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	launchOpts := config.Options{SystemPrompt: config.SystemPromptSettings{
		Models: map[string]config.PromptSource{"bound-model": {File: "no-such-prompt.md"}},
	}}
	w := scheduleWiring{
		roots: roots,
		live:  newLiveSettings(launchOpts, nil),
		binding: func() upstreamBinding {
			return upstreamBinding{Endpoint: "http://unused.invalid", Model: "bound-model"}
		},
		width: func() int { return 1 },
	}

	if _, err := w.fire(context.Background(), schedule.Firing{Prompt: "check", Mode: domain.ModePlan}); err == nil {
		t.Fatal("fire returned nil for a model whose system prompt cannot be read; want the failure")
	}
}

// A Firing fans out at the width the session's bound server resolves to (ADR 0039; ADR 0031 — every
// Driver reaches the same engine behaviour). The number can only come from the wired width seam: the
// entry firingSources builds pins no `parallel-agents:` of its own, so a Firing that reached past the
// seam would take the composer's own one-shot probe — a round trip, on the Scheduler's goroutine, for
// a number this session is already holding.
//
// Composed against the package's runner seam rather than a live model, which is why this test does
// not call t.Parallel: it replaces a package-level var, exactly as every headless test that reads a
// composed Spec does.
func TestScheduleFiringCarriesTheParallelAgentsWidth(t *testing.T) {
	roots, err := resolveRoots(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	stub := &stubRunner{}
	prevRunner := runOnce
	runOnce = stub.once
	t.Cleanup(func() { runOnce = prevRunner })

	w := scheduleWiring{
		roots:   roots,
		live:    newLiveSettings(config.Options{}, nil),
		binding: func() upstreamBinding { return upstreamBinding{Endpoint: "http://bound.invalid", Model: "bound-model"} },
		width:   func() int { return 6 },
	}

	if _, err := w.fire(context.Background(), schedule.Firing{Prompt: "check the build", Mode: domain.ModePlan}); err != nil {
		t.Fatalf("fire: %v", err)
	}
	if !stub.called {
		t.Fatal("the firing composed no run at all")
	}
	if got := stub.spec.Config.ParallelAgents; got != 6 {
		t.Errorf("the firing runs at ParallelAgents = %d, want the wired width 6 — it inherited the "+
			"pre-bind zero instead of the cap the session's server resolves to", got)
	}
}

// A Firing writes into a scratch dir of its OWN, named after the record it will be saved under
// (residuals sweep item 6, 2026-08-24). The seed below is the dir minted when this SESSION booted
// (wire_live.go), and a Firing that took it would put its working files in a dir a /clear or a
// /sessions resume has since moved the session off — or, once the 14-day sweep has been past it, in
// one that no longer exists at all.
//
// Composed against the package's runner seam rather than a live model, which is why this test does
// not call t.Parallel: it replaces a package-level var, exactly as the width test above does.
func TestScheduleFiringGetsItsOwnScratchDir(t *testing.T) {
	roots, err := resolveRoots(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	stub := &stubRunner{}
	prevRunner := runOnce
	runOnce = stub.once
	t.Cleanup(func() { runOnce = prevRunner })

	// The session's own boot-time dir, exactly as the composition root seeds it onto the Config a
	// Firing copies — the value this Firing must NOT run in.
	seed := ensureScratchDir(roots.scratch, "2026-08-24T09-00-00-session")
	if seed == "" {
		t.Fatal("could not create the session's seed scratch dir")
	}

	w := scheduleWiring{
		roots:   roots,
		live:    newLiveSettings(config.Options{}, nil),
		binding: func() upstreamBinding { return upstreamBinding{Endpoint: "http://bound.invalid", Model: "bound-model"} },
		width:   func() int { return 1 },
	}

	if _, err := w.fire(context.Background(), schedule.Firing{Prompt: "check the build", Mode: domain.ModePlan}); err != nil {
		t.Fatalf("fire: %v", err)
	}
	if !stub.called {
		t.Fatal("the firing composed no run at all")
	}
	assertFiringScratchDir(t, stub.spec.RecordID, stub.spec.Config.ScratchDir, roots.scratch)
	if stub.spec.Config.ScratchDir == seed {
		t.Error("the firing ran in the session's boot-time scratch dir; want one of its own, named after its record")
	}
}

// A Firing's reply is bounded by the entry the session is ON, never by the one it launched against
// (ADR 0046). The ceiling is a property of the SLOT, like the width above it, and the launch
// snapshot the settings holder is seeded from carries the LAUNCH entry's `max-output-tokens:`
// (wire_boot.go) — so a Firing raised after a `/server` move would be bounded by a server this
// session has left unless the entry firingSources builds restates the number, which is exactly the
// case a runaway reply must not happen in.
//
// The unpinned case is the same invariant read from the other end: an entry that pins nothing
// resolves to the STATED zero, which ADR 0046 decision 3 defines as "derive the cap from the reply
// budget again, clamped" — never "no cap" — and which is the very number an interactive session that
// moved onto that entry is handed (sessionMover.move passes the entry's field as written). The third
// state, a spec SILENT about the ceiling, leaves whatever is bound standing; the composition root's
// resolver always has something to say, so that branch is the engine's own nil contract and is pinned
// where it lives (internal/agent/rebind_test.go).
//
// Composed against the package's runner seam rather than a live model, which is why this test does
// not call t.Parallel: it replaces a package-level var, exactly as the width test above does.
func TestScheduleFiringIsBoundedByTheEntryTheSessionMovedOnto(t *testing.T) {
	// The launch entry's own ceiling — the number seeded onto the Config a Firing copies, and the one
	// that must not survive the move below.
	const launchCap = 2048

	for _, tt := range []struct {
		name    string
		moved   config.ServerEntry
		wantCap int
	}{
		{
			name:    "the moved-onto entry pins its own ceiling",
			moved:   config.ServerEntry{Name: "roomy", MaxOutputTokens: 8192},
			wantCap: 8192,
		},
		{
			name:    "the moved-onto entry pins nothing",
			moved:   config.ServerEntry{Name: "unpinned"},
			wantCap: 0,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			roots, err := resolveRoots(t.TempDir(), t.TempDir())
			if err != nil {
				t.Fatalf("resolveRoots: %v", err)
			}
			stub := &stubRunner{}
			prevRunner := runOnce
			runOnce = stub.once
			t.Cleanup(func() { runOnce = prevRunner })

			// The session as it LAUNCHED: bound to an entry pinning launchCap, which is what the
			// settings holder's own latch was seeded with.
			launchOpts := config.Options{HostAlias: "launch", StartupMaxOutputTokens: launchCap}
			live := newLiveSettings(launchOpts, nil)
			// ...and the move: the one call `/server` makes once the engine's own switch committed
			// (sessionMover.move), which is what makes the moved-onto entry's pins this session's.
			live.followEntry(tt.moved)

			w := scheduleWiring{
				roots: roots,
				live:  live,
				binding: func() upstreamBinding {
					return upstreamBinding{Endpoint: "http://moved.invalid", Model: "bound-model"}
				},
				width: func() int { return 1 },
			}

			firing := schedule.Firing{Prompt: "check the build", Mode: domain.ModePlan}
			if _, err := w.fire(context.Background(), firing); err != nil {
				t.Fatalf("fire: %v", err)
			}
			if !stub.called {
				t.Fatal("the firing composed no run at all")
			}
			if got := stub.spec.Config.Context.MaxOutputTokens; got != tt.wantCap {
				t.Errorf("the firing runs at Context.MaxOutputTokens = %d, want the moved-onto entry's "+
					"%d — it carried the LAUNCH server's ceiling %d instead of the ceiling of the "+
					"server this session is on", got, tt.wantCap, launchCap)
			}
		})
	}
}

// How that window is SPLIT follows the ceiling's rule one key further in: the share is the bound
// entry's `response-reserve:` resolved over the top-level key, read for the server the session is ON.
// The launch snapshot the settings holder is seeded from carries the LAUNCH entry's share
// (wire_boot.go), so a Firing raised after a `/server` move would otherwise hold back a slice of THIS
// server's window on the say-so of a server this session has left — and a share that is too generous
// silently shrinks the room an unattended answer has to land in, exactly where nobody is watching it
// happen.
//
// The unstated row is the same invariant read from the other end: an entry overriding nothing, under
// a config whose top-level key states nothing either, resolves to the honest 0 — "nobody stated a
// share", which hands the split back to the engine's own built-in fifth rather than to the number the
// launch entry happened to state.
//
// Composed against the package's runner seam rather than a live model, which is why this test does
// not call t.Parallel: it replaces a package-level var, exactly as the tests above it do.
func TestScheduleFiringSplitsTheWindowTheEntryTheSessionMovedOntoStates(t *testing.T) {
	// The launch entry's own share — the number seeded onto the Config a Firing copies, and the one
	// that must not survive the move below.
	const launchShare = 0.5

	for _, tt := range []struct {
		name      string
		moved     config.ServerEntry
		wantShare float64
	}{
		{
			name:      "the moved-onto entry states its own share",
			moved:     config.ServerEntry{Name: "roomy", ResponseReserve: 0.35},
			wantShare: 0.35,
		},
		{
			name:      "the moved-onto entry states none",
			moved:     config.ServerEntry{Name: "unstated"},
			wantShare: 0,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			roots, err := resolveRoots(t.TempDir(), t.TempDir())
			if err != nil {
				t.Fatalf("resolveRoots: %v", err)
			}
			stub := &stubRunner{}
			prevRunner := runOnce
			runOnce = stub.once
			t.Cleanup(func() { runOnce = prevRunner })

			// The session as it LAUNCHED: bound to an entry stating launchShare, which is what the
			// settings holder's own latch was seeded with. The top-level key states nothing, so the
			// entry the session moves onto is the only thing that can answer.
			launchOpts := config.Options{HostAlias: "launch", StartupResponseReserve: launchShare}
			live := newLiveSettings(launchOpts, nil)
			// ...and the move `/server` makes once the engine's own switch committed (sessionMover.move).
			live.followEntry(tt.moved)

			w := scheduleWiring{
				roots: roots,
				live:  live,
				binding: func() upstreamBinding {
					return upstreamBinding{Endpoint: "http://moved.invalid", Model: "bound-model"}
				},
				width: func() int { return 1 },
			}

			firing := schedule.Firing{Prompt: "check the build", Mode: domain.ModePlan}
			if _, err := w.fire(context.Background(), firing); err != nil {
				t.Fatalf("fire: %v", err)
			}
			if !stub.called {
				t.Fatal("the firing composed no run at all")
			}
			if got := stub.spec.Config.Context.ResponseReserveFraction; got != tt.wantShare {
				t.Errorf("the firing runs at Context.ResponseReserveFraction = %v, want the moved-onto "+
					"entry's %v — it divided this server's window the way the LAUNCH server's %v says",
					got, tt.wantShare, launchShare)
			}
		})
	}
}

// A Firing composes from the settings the session is running NOW, not from the ones it launched
// with (ADR 0037, amended 2026-08-24). This is the drift the boot-Config inheritance had and the
// whole reason the composition moved onto firingConfig: the pane's commit reaches the running
// session's own seams the moment it lands, but the Config a Firing was built from was copied at
// boot, so an unattended run raised an hour later offered the roster the human had disabled, dialled
// hosts they had denied, and scrubbed the variables of a `servers:` list they had since edited.
//
// The three fields asserted are the ones the fact-check found stale, and each has teeth of its own:
// a tool taken off the roster must not come back for the run nobody is watching, a host put on the
// deny list must stay unreachable there, and a key variable named in the file must be scrubbed out
// of every subprocess the Firing's model chooses the contents of.
//
// Driven through the real settings dispatcher rather than by writing the holder's fields, because
// the claim is about the APPLY landing on the projection — a test that set the fields directly would
// pass over a key whose apply forgot to record itself. Composed against the package's runner seam,
// which is why it does not call t.Parallel.
func TestScheduleFiringFollowsLiveSettingsEdits(t *testing.T) {
	roots, err := resolveRoots(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	stub := &stubRunner{}
	prevRunner := runOnce
	runOnce = stub.once
	t.Cleanup(func() { runOnce = prevRunner })

	// The session as it LAUNCHED, with each of the three keys set to something its edit moves OFF: a
	// value that came back unchanged would be the boot snapshot showing through rather than the edit
	// following the Firing.
	launchOpts := config.Options{
		ToolsDisabled: []string{"python_exec"},
		URLDenyHosts:  []string{"metadata.internal"},
		Servers:       []config.ServerEntry{{Name: "here", Endpoint: "http://bound.invalid"}},
	}
	// The list the `servers:` apply re-reads. Its second entry names a key SOURCE rather than a key,
	// which is the fact a run composed from these Options has to scrub for (SecretEnvVars).
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	const serversFile = "servers:\n" +
		"  - name: here\n    endpoint: http://bound.invalid\n" +
		"  - name: elsewhere\n    endpoint: http://other.invalid\n    api-key-env: ELSEWHERE_KEY\n"
	if err := os.WriteFile(configPath, []byte(serversFile), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	live := newLiveSettings(launchOpts, nil)
	set := newLiveTools(apogee.NewToolRegistry(), toolSetSpec{
		disabled: launchOpts.ToolsDisabled, denyHosts: launchOpts.URLDenyHosts,
	}, func(toolSetSpec) *apogee.ToolRegistry { return apogee.NewToolRegistry() })
	apply := applySettingFor(settingsApplier{
		engine: &applySettingSpy{}, live: live, tools: set, configPath: configPath,
	})
	// The human's session, three commits in. The `servers:` value is unread — that key re-reads the
	// file the pane just persisted — which is why the file above is what carries the new entry.
	for _, edit := range []struct{ key, value string }{
		{"tools.disabled", "[grep, view_diff]"},
		{"url-safety.deny-hosts", "[evil.example.com]"},
		{"servers", ""},
	} {
		if _, err := apply(edit.key, edit.value); err != nil {
			t.Fatalf("apply %s=%s: %v", edit.key, edit.value, err)
		}
	}

	w := scheduleWiring{
		roots:   roots,
		live:    live,
		binding: func() upstreamBinding { return upstreamBinding{Endpoint: "http://bound.invalid", Model: "bound-model"} },
		width:   func() int { return 1 },
	}

	if _, err := w.fire(context.Background(), schedule.Firing{Prompt: "check the build", Mode: domain.ModePlan}); err != nil {
		t.Fatalf("fire: %v", err)
	}
	if !stub.called {
		t.Fatal("the firing composed no run at all")
	}

	cfg := stub.spec.Config
	if want := []string{"grep", "view_diff"}; !slices.Equal(cfg.DisabledTools, want) {
		t.Errorf("the firing runs with DisabledTools = %v, want the edited %v — it offered its model "+
			"the roster the session LAUNCHED with", cfg.DisabledTools, want)
	}
	if want := []string{"evil.example.com"}; !slices.Equal(cfg.URLDenyHosts, want) {
		t.Errorf("the firing runs with URLDenyHosts = %v, want the edited %v — it would reach a host "+
			"the session has since denied", cfg.URLDenyHosts, want)
	}
	if !slices.Contains(cfg.SecretEnvVars, "ELSEWHERE_KEY") {
		t.Errorf("the firing runs with SecretEnvVars = %v, want the re-read list's ELSEWHERE_KEY — a "+
			"key variable named after launch would reach the environment of every command it runs",
			cfg.SecretEnvVars)
	}
}

// The one editable key an in-session Firing deliberately does NOT follow the session on: Auto's
// blast radius. `/confine off|on` moves the fence on the live engine and nothing mirrors it onto
// liveSettings, so `options()` projects the boot value and the Firing is fenced by the fence the
// session was CONFIGURED with. Ratified 2026-08-25 as the second deliberate exception to "a Firing
// sees exactly what the session sees" (ADR 0037's note of that date, beside the mode of ADR 0033
// decision 3): a `/confine off` is a per-session act a human takes while watching their own turn,
// and the unattended run raised beside it keeps the configured fence — `/confine off --save`, which
// writes the host acknowledgement, is the route that loosens a LATER session's Firings. A failure
// here is therefore not a composer bug to re-file; it means the exception stopped holding.
//
// The flip is driven through the engine seam `/confine off` itself calls
// (tui.Engine.SetConfineToWorkspace — see confinement_e2e_test.go) rather than left out: a test that
// toggled nothing would pass just as well against a composer that DID mirror the key.
//
// Composed against the package's runner seam, which is why this test does not call t.Parallel.
func TestScheduleFiringKeepsTheBootFenceAfterConfineOff(t *testing.T) {
	roots, err := resolveRoots(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	stub := &stubRunner{}
	prevRunner := runOnce
	runOnce = stub.once
	t.Cleanup(func() { runOnce = prevRunner })

	// The session as it LAUNCHED: fenced, which is the value the assertion below wants back.
	live := newLiveSettings(config.Options{ConfineToWorkspace: true}, nil)

	// The human's `/confine off`, on the live engine, exactly as the command drives it.
	engine := newLateEngine(domain.ModeAskBefore, true)
	t.Cleanup(func() { _ = engine.Close() })
	engine.SetConfineToWorkspace(false)
	if engine.ConfineToWorkspace() {
		t.Fatal("the engine still reports confined after SetConfineToWorkspace(false) — the session-" +
			"side flip this test is about never landed")
	}

	w := scheduleWiring{
		roots:   roots,
		live:    live,
		binding: func() upstreamBinding { return upstreamBinding{Endpoint: "http://bound.invalid", Model: "bound-model"} },
		width:   func() int { return 1 },
	}

	if _, err := w.fire(context.Background(), schedule.Firing{Prompt: "check the build", Mode: domain.ModePlan}); err != nil {
		t.Fatalf("fire: %v", err)
	}
	if !stub.called {
		t.Fatal("the firing composed no run at all")
	}

	if !stub.spec.Config.ConfineToWorkspace {
		t.Error("the firing runs with ConfineToWorkspace = false, want the boot value true — a blast " +
			"radius a human loosened for their own watched turn reached the run nobody is watching")
	}
}

// A Firing resolves its attached skills through the session's OWN catalogue, and mounts that
// catalogue's dirs as its read roots (design call 5). Shared rather than rebuilt for the reason
// every other live seam here is shared: `use-project-skills` is editable in the `/settings` pane and
// a `/` menu open reloads the catalogue from disk, so a Firing holding a provider of its own would
// resolve an attached id against a source layering the session has moved off — and mount the read
// roots of one.
//
// The provider is built over dirs of its own rather than over the run's roots, which is what makes
// the assertion evidence: a composer that fell back to its own nil default would build one from
// roots.config and answer different dirs.
//
// Composed against the package's runner seam, which is why this test does not call t.Parallel.
func TestScheduleFiringSharesTheSessionsSkillsProvider(t *testing.T) {
	roots, err := resolveRoots(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	stub := &stubRunner{}
	prevRunner := runOnce
	runOnce = stub.once
	t.Cleanup(func() { runOnce = prevRunner })

	provider := skills.NewProvider(skills.Sources{Home: t.TempDir(), Workspace: t.TempDir()})
	w := scheduleWiring{
		roots:   roots,
		live:    newLiveSettings(config.Options{}, nil),
		binding: func() upstreamBinding { return upstreamBinding{Endpoint: "http://bound.invalid", Model: "bound-model"} },
		width:   func() int { return 1 },
		skills:  provider,
	}

	if _, err := w.fire(context.Background(), schedule.Firing{Prompt: "check the build", Mode: domain.ModePlan}); err != nil {
		t.Fatalf("fire: %v", err)
	}
	if !stub.called {
		t.Fatal("the firing composed no run at all")
	}
	if stub.spec.Config.Skills != provider {
		t.Error("the firing resolves attached skills through a catalogue of its own; want the session's " +
			"instance, so a use-project-skills flip keeps following the runs it raises")
	}
	if stub.spec.Config.ExtraReadRoots == nil {
		t.Fatal("the firing mounts no extra read roots at all; want the session catalogue's own dirs")
	}
	if got, want := stub.spec.Config.ExtraReadRoots(), provider.SourceDirs(); !slices.Equal(got, want) {
		t.Errorf("the firing mounts read roots %v, want the session provider's %v — the model could "+
			"not read the bundled files of a skill it was given", got, want)
	}
}

// ----------------------------------------------------------------------------
// What the Fire seam reports back
// ----------------------------------------------------------------------------

// handClock is the Scheduler's sense of time with the cadence under the test's hand: NewTicker
// yields a ticker the test fires itself, so a Firing starts the moment it is asked for rather than
// after MinCycle of real waiting. Now stays the wall clock — nothing here asserts a next-fire time,
// and the elapsed measurement is pinned on the fake clock in internal/schedule.
type handClock struct {
	mu      sync.Mutex
	tickers []chan time.Time
}

// Now reports the wall-clock time.
func (c *handClock) Now() time.Time { return time.Now() }

// NewTicker registers a ticker this clock's tick() delivers to.
func (c *handClock) NewTicker(time.Duration) schedule.Ticker {
	ch := make(chan time.Time, 1)
	c.mu.Lock()
	c.tickers = append(c.tickers, ch)
	c.mu.Unlock()
	return handTicker{ch: ch}
}

// tick delivers one tick to every ticker handed out so far, dropping it where one is already
// pending — exactly what a real time.Ticker does to a Schedule that has not taken its last tick.
func (c *handClock) tick() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range c.tickers {
		select {
		case ch <- time.Now():
		default:
		}
	}
}

// handTicker is one Schedule's cadence, delivered by the test rather than by time.
type handTicker struct{ ch chan time.Time }

// C is the channel the test's ticks arrive on.
func (t handTicker) C() <-chan time.Time { return t.ch }

// Stop is a no-op: a hand-driven ticker stops when the test stops ticking it.
func (t handTicker) Stop() {}

// scheduleHarness is one composition test's whole world: the wiring a Firing is composed from, a
// real Scheduler driving the real fire seam, a clock the test ticks, and the Events that came back.
// It exists because the claim below is about the JOIN — internal/run's tests stop at Result and
// internal/schedule's fire a stub runner, so only a Scheduler over a genuine fire() shows what a
// surface actually receives.
type scheduleHarness struct {
	scheduler *schedule.Scheduler
	clock     *handClock
	events    chan schedule.Event
	store     *session.Store
}

// newScheduleHarness composes a Firing against endpoint and puts a Scheduler in front of it.
func newScheduleHarness(t *testing.T, endpoint string) *scheduleHarness {
	t.Helper()

	roots, err := resolveRoots(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	store := session.NewStore(roots.sessions)
	w := scheduleWiring{
		roots:   roots,
		live:    newLiveSettings(config.Options{}, nil),
		binding: func() upstreamBinding { return upstreamBinding{Endpoint: endpoint, Model: "bound-model"} },
		width:   func() int { return 1 },
		store:   store,
	}

	clock := &handClock{}
	// Buffered past the events one Firing emits: Notify runs on goroutines the Scheduler owns, and
	// a full channel would stall the run rather than the test.
	events := make(chan schedule.Event, 16)
	scheduler, err := schedule.New(schedule.Config{
		Fire:   w.fire,
		Notify: func(ev schedule.Event) { events <- ev },
		Clock:  clock,
	})
	if err != nil {
		t.Fatalf("schedule.New: %v", err)
	}
	t.Cleanup(scheduler.Close)
	return &scheduleHarness{scheduler: scheduler, clock: clock, events: events, store: store}
}

// fire creates one Schedule and delivers its tick. The cycle is nominal — the hand clock decides
// when it fires, so nothing here waits out MinCycle.
func (h *scheduleHarness) fire(t *testing.T) {
	t.Helper()

	if _, err := h.scheduler.Add(schedule.Spec{
		Name:   "Nightly build",
		Cycle:  time.Hour,
		Prompt: "check the build",
		Mode:   domain.ModePlan,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h.clock.tick()
}

// await returns the Event of the wanted kind, bounded so a seam that stops reporting fails the test
// rather than hanging it.
func (h *scheduleHarness) await(t *testing.T, want schedule.EventKind) schedule.Event {
	t.Helper()

	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev := <-h.events:
			if ev.Kind == want {
				return ev
			}
			if ev.Kind == schedule.EventFailed {
				t.Fatalf("the firing failed instead of reaching %q: %v", want, ev.Err)
			}
		case <-deadline:
			t.Fatalf("no %q event ever arrived from the fire seam", want)
		}
	}
}

// The seam's headline: what the run learned about itself reaches the surface as data on the Event —
// the answer, the turn count, the record it left — so a Driver can show the Firing's result without
// decoding a saved record, and without a seam of its own onto the runner.
func TestAFiringsAnswerAndStatsCrossTheFireSeam(t *testing.T) {
	t.Parallel()

	url, _, _ := firingUpstream(t, "the build is green")
	h := newScheduleHarness(t, url)

	h.fire(t)
	ev := h.await(t, schedule.EventCompleted)
	if ev.Outcome.FinalText != "the build is green" {
		t.Errorf("Outcome.FinalText = %q, want the firing's answer %q — the answer never crossed the "+
			"seam, so the chat has nothing to show", ev.Outcome.FinalText, "the build is green")
	}
	if ev.Outcome.Turns != 1 {
		t.Errorf("Outcome.Turns = %d, want 1 for a single-reply firing", ev.Outcome.Turns)
	}
	if ev.Outcome.Denied != 0 {
		t.Errorf("Outcome.Denied = %d, want 0; this firing asked for nothing gated", ev.Outcome.Denied)
	}
	if ev.Outcome.RecordID == "" || ev.Outcome.Title == "" {
		t.Errorf("Outcome record pointer = (%q, %q), want the saved record's id and title",
			ev.Outcome.RecordID, ev.Outcome.Title)
	}
}

// A Firing that dies mid-run still reports what it salvaged. The partial record used to be
// reachable only by parsing the error text; it now rides the Outcome BESIDE the error, which is
// what lets a surface point a human at the half-run it already announced.
func TestAFailedFiringStillCarriesWhatItSalvaged(t *testing.T) {
	t.Parallel()

	// The failure a Firing actually dies of: its Driver going away mid-run (ADR 0033 — a Schedule
	// dies with the TUI). An upstream that never answers holds the run open until Close cancels it,
	// and run.Once words that cancellation as the error while still saving what it had.
	requested := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case requested <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done(): // the firing's context died; the request dies with it
		case <-release: // and the handler never outlives the test, whatever the server noticed
		}
	}))
	t.Cleanup(srv.Close)
	defer close(release)

	h := newScheduleHarness(t, srv.URL)
	h.fire(t)
	select {
	case <-requested:
	case <-time.After(10 * time.Second):
		t.Fatal("the firing never reached the upstream")
	}
	h.scheduler.Close() // the Driver going away, which cancels the run in flight

	ev := h.await(t, schedule.EventFailed)
	if ev.Err == nil {
		t.Fatal("the failed event carries no error")
	}
	if ev.Outcome.RecordID == "" {
		t.Fatal("Outcome.RecordID is empty on a failed firing that saved a partial record; the " +
			"surface can only reach it by parsing the error text")
	}

	metas, err := h.store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 || metas[0].ID != ev.Outcome.RecordID {
		t.Fatalf("the store holds %d records; the reported id %q must name the partial one",
			len(metas), ev.Outcome.RecordID)
	}
	if ev.Outcome.Title != metas[0].Title {
		t.Errorf("Outcome.Title = %q, want the partial record's %q", ev.Outcome.Title, metas[0].Title)
	}
	if !strings.Contains(ev.Err.Error(), ev.Outcome.RecordID) {
		t.Errorf("the error %q no longer names the partial record; a Driver that reads only the "+
			"wording lost it", ev.Err)
	}
}

// The Gate is the whole of decision 7: a due Firing waits for the human's session to be quiescent,
// and lets go the moment it is. The three cells are the ones that matter — the open start, the held
// wait and its release, and the cancellation that must not leave a waiter behind for Close to join.
func TestIdleGateHoldsAFiringUntilTheSessionIsQuiescent(t *testing.T) {
	t.Parallel()

	t.Run("a session that has said nothing is already quiescent", func(t *testing.T) {
		t.Parallel()
		if err := newIdleGate().wait(context.Background()); err != nil {
			t.Fatalf("wait on a fresh gate: %v; want an immediate release", err)
		}
	})

	t.Run("a busy session holds the firing until it goes idle", func(t *testing.T) {
		t.Parallel()
		gate := newIdleGate()
		gate.report(true)

		released := make(chan error, 1)
		go func() { released <- gate.wait(context.Background()) }()

		select {
		case err := <-released:
			t.Fatalf("wait returned %v while the session was busy; want it held", err)
		case <-time.After(gateSettleWindow):
		}

		gate.report(false)
		select {
		case err := <-released:
			if err != nil {
				t.Fatalf("wait after the idle report: %v; want a release", err)
			}
		case <-time.After(time.Second):
			t.Fatal("wait did not return after the session reported idle")
		}
	})

	t.Run("a stopped schedule takes its waiter with it", func(t *testing.T) {
		t.Parallel()
		gate := newIdleGate()
		gate.report(true)

		ctx, cancel := context.WithCancel(context.Background())
		released := make(chan error, 1)
		go func() { released <- gate.wait(ctx) }()
		cancel()

		select {
		case err := <-released:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("wait returned %v; want the context's own error, which is what lets Close join", err)
			}
		case <-time.After(time.Second):
			t.Fatal("wait ignored its cancelled context; Close would hang on this waiter")
		}
	})
}

// The auto ladder a Schedule is held to is the one a LAUNCH is held to (decision 3) — never
// stricter, and never silently escalating.
func TestScheduleAutoBlockedMirrorsTheAutoLadder(t *testing.T) {
	t.Parallel()

	fencing := apogee.ConfinementCaps{FSWrite: true}
	var none apogee.ConfinementCaps

	tests := []struct {
		name               string
		caps               apogee.ConfinementCaps
		confineToWorkspace bool
		wantBlocked        bool
	}{
		{name: "a host that can fence offers auto", caps: fencing, confineToWorkspace: true},
		{
			name:               "a host that cannot fence blocks auto — a firing has no approval rung",
			caps:               none,
			confineToWorkspace: true,
			wantBlocked:        true,
		},
		{name: "the user's own unconfined opt-in offers auto anyway", caps: none},
		{name: "unconfined on a fencing host offers auto", caps: fencing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := scheduleAutoBlocked("deny", tt.caps, tt.confineToWorkspace)
			if blocked := got != ""; blocked != tt.wantBlocked {
				t.Fatalf("scheduleAutoBlocked = %q (blocked=%v), want blocked=%v", got, blocked, tt.wantBlocked)
			}
			if tt.wantBlocked && !strings.Contains(got, "deny") {
				t.Errorf("the refusal %q does not name the backend that caused it", got)
			}
		})
	}
}

// The wiring-level claim: runRoot hands the renderer a live scheduler plus the two values that
// answer it, and the whole thing is closed when the TUI returns — a TUI-hosted Schedule dies with
// the TUI, which is exactly what makes v1 honest about persistence.
func TestRunRootWiresTheSchedulerAndClosesItWithTheTUI(t *testing.T) {
	t.Parallel()

	var opts tui.Options
	var addErr error
	var id string
	var live int
	launch := func(_ context.Context, _ tui.Engine, _ *tui.Bridge, o tui.Options) error {
		opts = o
		if o.Schedules == nil {
			return nil
		}
		id, addErr = o.Schedules.Add(schedule.Spec{
			Name:   "Nightly build",
			Cycle:  time.Hour,
			Prompt: "check the build",
			Mode:   domain.ModePlan,
		})
		live = len(o.Schedules.List())
		return nil
	}

	err := runRoot(context.Background(), config.Options{
		Endpoint:           "http://127.0.0.1:1111",
		Model:              "fake",
		Mode:               "ask-before",
		Workspace:          t.TempDir(),
		ConfigDir:          t.TempDir(),
		ConfineToWorkspace: false, // the one ladder cell that is the same on every host
	}, launch)
	if err != nil {
		t.Fatalf("runRoot: %v", err)
	}

	if opts.Schedules == nil {
		t.Fatal("tui.Options.Schedules is nil; /schedule would report scheduling unavailable")
	}
	if opts.ReportActivity == nil {
		t.Fatal("tui.Options.ReportActivity is nil; the Gate would never learn the session went idle")
	}
	if opts.ScheduleAutoBlocked != "" {
		t.Errorf("ScheduleAutoBlocked = %q on an unconfined host; auto is the user's own opt-in there",
			opts.ScheduleAutoBlocked)
	}
	if addErr != nil || id == "" {
		t.Fatalf("Add through the wired seam = (%q, %v); want a live schedule", id, addErr)
	}
	if live != 1 {
		t.Errorf("List() reported %d live schedules inside the TUI, want 1", live)
	}

	// runRoot has returned, so its deferred Close has run: the scheduler accepts nothing further and
	// holds nothing. Close also joins every goroutine it started, which is what keeps this test
	// meaningful under -race.
	if _, err := opts.Schedules.Add(schedule.Spec{
		Cycle: time.Hour, Prompt: "after the tui", Mode: domain.ModePlan,
	}); !errors.Is(err, schedule.ErrClosed) {
		t.Errorf("Add after the TUI returned = %v, want ErrClosed — the schedules outlived their driver", err)
	}
	if got := opts.Schedules.List(); len(got) != 0 {
		t.Errorf("List() after the TUI returned holds %d schedules, want none", len(got))
	}
}

// The host predicate the wiring reads is the platform backend's own, so the value the renderer gets
// is about THIS machine rather than a guess. It is asserted as an identity rather than a verdict:
// the verdict differs between a fencing host and a container, and both are correct.
func TestScheduleAutoBlockedFollowsTheHostBackend(t *testing.T) {
	t.Parallel()
	caps := platform.NewConfiner().Capabilities()
	if blocked := scheduleAutoBlocked("landlock", caps, true) != ""; blocked == caps.AutoEligible() {
		t.Errorf("auto blocked=%v on a host reporting AutoEligible=%v; the two must be opposites",
			blocked, caps.AutoEligible())
	}
}

// ----------------------------------------------------------------------------
// The composition the two halves' unit tests cannot see
// ----------------------------------------------------------------------------

// loopSender is tea.Program.Send's blocking discipline without a terminal: an unbuffered channel
// that only the program's Update loop drains. The real Send is exactly this (charm.land/bubbletea
// tea.go: `case p.msgs <- msg`), and it is the reason a Notify called on the Update goroutine
// hangs the whole program rather than merely arriving late.
type loopSender struct{ msgs chan tea.Msg }

func (s loopSender) Send(msg tea.Msg) { s.msgs <- msg }

// A Schedule created from the Update loop must not hang the program.
//
// This is the composition neither half's own tests can reach: internal/tui drives a fake Scheduler
// that never emits, and internal/schedule drives a buffered recorder that never blocks, so both
// suites pass while the two together deadlock. `/schedule 10m ...` was that deadlock — Add emitted
// EventCreated on its caller's goroutine, the caller was the Update loop, and Bridge.NotifySchedule
// waited for the loop to take a message it could only take after Add returned.
//
// The Update loop is simulated rather than run because the claim is about ONE goroutine: whatever
// creates a Schedule must be free to go on draining messages afterwards.
func TestCreatingAScheduleFromTheUpdateLoopDoesNotHangTheProgram(t *testing.T) {
	bridge := tui.NewBridge()
	sender := loopSender{msgs: make(chan tea.Msg)}
	bridge.Bind(sender)

	schedules, err := schedule.New(schedule.Config{
		Fire:   func(context.Context, schedule.Firing) (schedule.Outcome, error) { return schedule.Outcome{}, nil },
		Notify: bridge.NotifySchedule,
	})
	if err != nil {
		t.Fatalf("schedule.New: %v", err)
	}
	t.Cleanup(schedules.Close)

	// The Update loop: it is INSIDE this call, so it is not draining sender.msgs meanwhile —
	// precisely the state the TUI is in while folding a submitted /schedule line.
	type result struct {
		id  string
		err error
	}
	done := make(chan result, 1)
	go func() {
		id, addErr := schedules.Add(schedule.Spec{
			Cycle: 10 * time.Minute, Prompt: "What time is it?", Mode: domain.ModePlan,
		})
		done <- result{id, addErr}
	}()

	var got result
	select {
	case got = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Add never returned from the Update loop: the program is deadlocked, " +
			"which is what /schedule 10m did to the TUI")
	}
	if got.err != nil {
		t.Fatalf("Add: %v", got.err)
	}

	// The loop resumes draining, and the notice it was never able to take now arrives.
	select {
	case msg := <-sender.msgs:
		if _, ok := msg.(tea.Msg); !ok || msg == nil {
			t.Fatalf("scheduler notice = %#v, want a message the Update loop can fold", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no scheduler notice ever reached the program")
	}

	// Stop is the same hazard on the same goroutine, and must clear it the same way.
	stopped := make(chan error, 1)
	go func() { stopped <- schedules.Stop(got.id) }()
	select {
	case stopErr := <-stopped:
		if stopErr != nil {
			t.Fatalf("Stop: %v", stopErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stop never returned from the Update loop: the program is deadlocked")
	}
	go func() {
		for range sender.msgs {
		}
	}()
}
