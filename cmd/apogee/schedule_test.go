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
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/schedule"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/tui"
)

// gateSettleWindow is how long a test waits before concluding that a blocked Gate really is
// blocked. Nothing under test should ever consume it on the release path — those assertions wait on
// a channel — so it buys only the negative claim, and buys it cheaply.
const gateSettleWindow = 50 * time.Millisecond

// firingUpstream starts a server that answers every request with one final, no-tool reply and
// records the tool menu each request offered — which is how the "a Firing carries the library's own
// registry, never the session's" prescription is asserted from the wire rather than from the Config.
func firingUpstream(t *testing.T, reply string) (url string, menus func() [][]string) {
	t.Helper()
	var seen [][]string
	done := make(chan struct{}, 32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded struct {
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
		w.Header().Set("Content-Type", "text/event-stream")
		e2eWriteFinal(w, reply)
		done <- struct{}{}
	}))
	t.Cleanup(srv.Close)
	// The handler appends without a lock, so the reader waits for the request it asked for rather
	// than racing the server goroutine. A Firing makes exactly one call in these tests.
	return srv.URL, func() [][]string {
		<-done
		return seen
	}
}

// sessionOnlyTool stands for anything the interactive session's registry carries that a Firing must
// not inherit — an MCP server's tool, in production (ADR 0008: external effects are re-established
// per session, never shared into a second concurrent Agent). It declares itself read-only so Plan
// mode would genuinely offer it, which is what makes its ABSENCE from the wire menu evidence.
type sessionOnlyTool struct{}

func (sessionOnlyTool) Name() string            { return "session_only_tool" }
func (sessionOnlyTool) Description() string     { return "a tool only the session's registry has" }
func (sessionOnlyTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (sessionOnlyTool) ReadOnly() bool          { return true }
func (sessionOnlyTool) Execute(context.Context, apogee.ToolCall) (apogee.ToolResult, error) {
	return apogee.ToolResult{}, nil
}

// TestScheduleFiringRunsAgainstTheCurrentBinding is the item's headline: the Fire seam composes one
// headless run from the binding the session holds AT FIRE TIME, in the Schedule's own mode, and the
// record it leaves behind is an ordinary session record marked with the Schedule's identity.
func TestScheduleFiringRunsAgainstTheCurrentBinding(t *testing.T) {
	t.Parallel()

	url, menus := firingUpstream(t, "the build is green")
	roots, err := resolveRoots(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	store := session.NewStore(roots.sessions)

	// The session's own surface: ask-before (a mode a Firing may NOT run in) and a registry carrying
	// a tool of its own. If fire() inherited either, this run would fail rather than pass — the mode
	// is refused by run.Once, and the tool would show up on the menu below.
	base := apogee.Config{
		Mode:         modeAskBefore,
		WorkspaceDir: roots.workspace,
		ConfigDir:    roots.config,
		LibraryDir:   roots.library,
		SessionsDir:  roots.sessions,
	}
	base.Tools = registryWithMCP(roots.workspace, base, []apogee.Tool{sessionOnlyTool{}})

	w := scheduleWiring{
		base:  base,
		opts:  options{endpoint: "http://launch.invalid", model: "launch-model"},
		roots: roots,
		// The binding the session has MOVED to since launch (a /server switch, a rebind). The Firing
		// must follow it rather than the launch values in opts above.
		binding: func() upstreamBinding { return upstreamBinding{Endpoint: url, Model: "bound-model"} },
		store:   store,
	}

	out, err := w.fire(context.Background(), schedule.Firing{
		ScheduleID:   "sch-1-abcd",
		ScheduleName: "Nightly build",
		Prompt:       "check the build",
		Mode:         modePlan,
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
	w := scheduleWiring{
		base: apogee.Config{WorkspaceDir: roots.workspace},
		opts: options{systemPrompt: systemPromptSettings{
			models: map[string]promptSource{"bound-model": {file: "no-such-prompt.md"}},
		}},
		roots: roots,
		binding: func() upstreamBinding {
			return upstreamBinding{Endpoint: "http://unused.invalid", Model: "bound-model"}
		},
	}

	if _, err := w.fire(context.Background(), schedule.Firing{Prompt: "check", Mode: modePlan}); err == nil {
		t.Fatal("fire returned nil for a model whose system prompt cannot be read; want the failure")
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
			Mode:   modePlan,
		})
		live = len(o.Schedules.List())
		return nil
	}

	err := runRoot(context.Background(), options{
		endpoint:           "http://127.0.0.1:1111",
		model:              "fake",
		mode:               "ask-before",
		workspace:          t.TempDir(),
		configDir:          t.TempDir(),
		confineToWorkspace: false, // the one ladder cell that is the same on every host
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
		Cycle: time.Hour, Prompt: "after the tui", Mode: modePlan,
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
			Cycle: 10 * time.Minute, Prompt: "What time is it?", Mode: modePlan,
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
