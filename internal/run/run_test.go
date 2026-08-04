package run

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/tools"
)

// TestOncePersistsAFiringUnderItsScheduleIdentity is the item's headline: one Firing lands
// as an ordinary session record whose browsable Meta carries the Schedule identity and the
// derived "<name> — <HH:MM>" title, so the /sessions browser can label it (ADR 0033).
func TestOncePersistsAFiringUnderItsScheduleIdentity(t *testing.T) {
	t.Parallel()

	up := newUpstream(t, alwaysFinal("the build is green"))
	store := session.NewStore(t.TempDir())
	fired := time.Date(2026, 8, 4, 9, 30, 0, 0, time.Local)

	spec := planSpec(up.url, "check the build")
	spec.Config.WorkspaceDir = t.TempDir()
	spec.ScheduleID = "sch-1"
	spec.ScheduleName = "Nightly build"
	spec.Store = store
	spec.Now = at(fired)

	res, err := Once(context.Background(), spec)
	if err != nil {
		t.Fatalf("Once: %v", err)
	}
	if res.SessionID == "" {
		t.Fatal("Result.SessionID is empty; the firing was not persisted")
	}
	if want := "Nightly build — 09:30"; res.Title != want {
		t.Errorf("Result.Title = %q, want %q", res.Title, want)
	}
	if res.Turns != 1 {
		t.Errorf("Result.Turns = %d, want 1", res.Turns)
	}
	if res.Denied != 0 {
		t.Errorf("Result.Denied = %d, want 0", res.Denied)
	}
	if res.Err != nil {
		t.Errorf("Result.Err = %v, want nil", res.Err)
	}

	metas, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("store holds %d records, want 1", len(metas))
	}
	meta := metas[0]
	if meta.ScheduleID != "sch-1" || meta.ScheduleName != "Nightly build" {
		t.Errorf("Meta schedule identity = (%q, %q), want (%q, %q)",
			meta.ScheduleID, meta.ScheduleName, "sch-1", "Nightly build")
	}
	if meta.Title != res.Title {
		t.Errorf("Meta.Title = %q, want the Result's %q", meta.Title, res.Title)
	}
	if meta.Model != "test-model" || meta.Workspace != spec.Config.WorkspaceDir {
		t.Errorf("Meta model/workspace = (%q, %q), want (%q, %q)",
			meta.Model, meta.Workspace, "test-model", spec.Config.WorkspaceDir)
	}
	if meta.UserMsgs != 1 {
		t.Errorf("Meta.UserMsgs = %d, want 1 (a firing submits one prompt)", meta.UserMsgs)
	}
	if !meta.CreatedAt.Equal(fired.UTC()) {
		t.Errorf("Meta.CreatedAt = %v, want the pinned %v", meta.CreatedAt, fired.UTC())
	}

	// The record is engine-resumable and carries no scrollback: v1 records no transcript
	// blob, so a resume takes ADR 0022's documented degrade path by design.
	rec, err := store.Load(res.SessionID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(rec.Transcript) != 0 {
		t.Errorf("record carries a transcript blob (%d bytes); a firing records none", len(rec.Transcript))
	}
	if rec.Session.Version != domain.SessionVersion {
		t.Errorf("record session version = %d, want %d", rec.Session.Version, domain.SessionVersion)
	}
}

// TestOnceConstructsAFreshAgentPerFiring pins decision 5: each Firing is a new Agent with a
// new conversation, so nothing bleeds from one run into the next. The proof is on the wire —
// the second Firing's request carries exactly one user message, not the first run's history.
func TestOnceConstructsAFreshAgentPerFiring(t *testing.T) {
	t.Parallel()

	up := newUpstream(t, alwaysFinal("done"))
	spec := planSpec(up.url, "summarise the day")

	for i := range 2 {
		if _, err := Once(context.Background(), spec); err != nil {
			t.Fatalf("Once #%d: %v", i+1, err)
		}
	}

	reqs := up.requests()
	if len(reqs) != 2 {
		t.Fatalf("the Upstream saw %d requests, want 2", len(reqs))
	}
	for i, req := range reqs {
		if got := req.userMsgs(); got != 1 {
			t.Errorf("request #%d carried %d user messages, want 1 (state bled between firings)", i+1, got)
		}
	}
}

// TestOnceWithoutAStorePersistsNothing covers the bench case: a nil Store runs the Firing
// and reports its Result, minting no record and no id.
func TestOnceWithoutAStorePersistsNothing(t *testing.T) {
	t.Parallel()

	up := newUpstream(t, alwaysFinal("nothing to save"))
	spec := planSpec(up.url, "just think about it")
	spec.Now = at(time.Date(2026, 8, 4, 11, 0, 0, 0, time.Local))

	res, err := Once(context.Background(), spec)
	if err != nil {
		t.Fatalf("Once: %v", err)
	}
	if res.SessionID != "" {
		t.Errorf("Result.SessionID = %q, want empty for an unpersisted firing", res.SessionID)
	}
	if want := "just think about it"; res.Title != want {
		t.Errorf("Result.Title = %q, want the first-prompt heuristic %q", res.Title, want)
	}
	if up.calls() != 1 {
		t.Errorf("the Upstream saw %d requests, want 1", up.calls())
	}
}

// TestOnceDeniesAGatedActionWithoutParking is the fail-safe denier's proof: a gated tool
// call is refused, the tool never executes, the refusal is counted, and the Exchange still
// reaches its boundary — a Firing fails visibly rather than waiting for a human who is not
// there (decision 2; ADR 0031 invariant 2).
func TestOnceDeniesAGatedActionWithoutParking(t *testing.T) {
	t.Parallel()

	tool := &gatingTool{}
	registry := domain.NewToolRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatalf("Register: %v", err)
	}

	up := newUpstream(t, func(w http.ResponseWriter, req request) {
		if req.lastRoleIs(domain.RoleTool) {
			writeFinal(w, "I could not write the file")
			return
		}
		writeToolCall(w, "call_1", tool.Name(), `{"path":"out.txt"}`)
	})

	spec := planSpec(up.url, "write the report")
	spec.Config.Mode = domain.ModeAuto
	spec.Config.ConfineToWorkspace = true
	spec.Config.Confiner = stubConfiner{}
	spec.Config.Tools = registry

	// A generous deadline rather than a bare Background: if the denier ever parked, this
	// test must fail on the assertion below, not hang the package.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := Once(ctx, spec)
	if err != nil {
		t.Fatalf("Once: %v", err)
	}
	if res.Denied != 1 {
		t.Errorf("Result.Denied = %d, want 1", res.Denied)
	}
	if tool.ran() {
		t.Error("the gated tool executed; the denier must refuse it")
	}
	if res.Turns != 2 {
		t.Errorf("Result.Turns = %d, want 2 (the refused tool Turn, then the final Turn)", res.Turns)
	}
	if res.Err != nil {
		t.Errorf("Result.Err = %v, want nil — a denied action fails the step, not the run", res.Err)
	}
}

// TestOncePinsAskerAndPresenterOff proves the pin is Once's, not the caller's: a Spec whose
// Config supplies both human-facing delegates still runs with ask_user and present_document
// unregistered, so nothing inside a Firing can rendezvous with a human.
func TestOncePinsAskerAndPresenterOff(t *testing.T) {
	t.Parallel()

	up := newUpstream(t, alwaysFinal("no questions asked"))
	spec := planSpec(up.url, "look around")
	spec.Config.Mode = domain.ModeAuto // Auto offers the whole menu; Plan filters it to reads
	spec.Config.Confiner = stubConfiner{}
	spec.Config.WorkspaceDir = t.TempDir() // wires the built-in registry, where the two delegates land
	spec.Config.Asker = stubAsker{}
	spec.Config.Presenter = stubPresenter{}

	if _, err := Once(context.Background(), spec); err != nil {
		t.Fatalf("Once: %v", err)
	}

	reqs := up.requests()
	if len(reqs) != 1 {
		t.Fatalf("the Upstream saw %d requests, want 1", len(reqs))
	}
	menu := reqs[0]
	if len(menu.Tools) == 0 {
		t.Fatal("the request offered no tools at all; the default registry was not wired")
	}
	for _, name := range []string{"ask_user", "present_document"} {
		if menu.offers(name) {
			t.Errorf("the firing offered %q; Once must leave the human-facing delegates unregistered", name)
		}
	}
}

// TestOnceRejectsModesThatNeedAHuman pins decision 2 at the door: only Plan and Auto are
// Firing modes, and a rejected Spec never reaches the Upstream.
func TestOnceRejectsModesThatNeedAHuman(t *testing.T) {
	t.Parallel()

	for _, mode := range []domain.Mode{domain.ModeAskBefore, domain.ModeAllowEdits, domain.Mode("")} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()

			up := newUpstream(t, alwaysFinal("unreached"))
			spec := planSpec(up.url, "do the thing")
			spec.Config.Mode = mode

			res, err := Once(context.Background(), spec)
			if !errors.Is(err, ErrMode) {
				t.Fatalf("Once err = %v, want ErrMode", err)
			}
			if res != (Result{}) {
				t.Errorf("Result = %+v, want the zero Result on a rejected spec", res)
			}
			if up.calls() != 0 {
				t.Errorf("the Upstream saw %d requests; a rejected mode must error before any request", up.calls())
			}
		})
	}
}

// TestOnceReportsTheFinalAnswer is owner decision 1's floor: the Firing's answer comes back
// on the Result, so a Driver can show it without decoding the saved record. It is caught off
// the event stream, not the snapshot, so an UNPERSISTED Firing reports it just the same —
// which is the whole reason the capture does not live on the persisting path.
func TestOnceReportsTheFinalAnswer(t *testing.T) {
	t.Parallel()

	const answer = "the build is green and every test passed"

	tests := []struct {
		name  string
		store bool
	}{
		{"persisted", true},
		{"unpersisted", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			up := newUpstream(t, alwaysFinal(answer))
			spec := planSpec(up.url, "check the build")
			if tt.store {
				spec.Store = session.NewStore(t.TempDir())
			}

			res, err := Once(context.Background(), spec)
			if err != nil {
				t.Fatalf("Once: %v", err)
			}
			if res.FinalText != answer {
				t.Errorf("Result.FinalText = %q, want %q", res.FinalText, answer)
			}
			if tt.store && res.SessionID == "" {
				t.Error("Result.SessionID is empty; the firing was not persisted")
			}
		})
	}
}

// TestOnceReportsTheLastMessageNotTheFirst pins "final" in the contract: a Firing that
// narrates its plan, calls a tool and only then answers reports the ANSWER. The narration
// rides an assistant message that ends no Exchange, so it must never stand in for the
// Firing's result.
func TestOnceReportsTheLastMessageNotTheFirst(t *testing.T) {
	t.Parallel()

	const (
		narration = "I will try to write the report first"
		answer    = "I could not write the file, so here is what I found instead"
	)

	tool := &gatingTool{}
	registry := domain.NewToolRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatalf("Register: %v", err)
	}

	up := newUpstream(t, func(w http.ResponseWriter, req request) {
		if req.lastRoleIs(domain.RoleTool) {
			writeFinal(w, answer)
			return
		}
		writeToolCallWithText(w, narration, "call_1", tool.Name(), `{"path":"out.txt"}`)
	})

	spec := planSpec(up.url, "write the report")
	spec.Config.Mode = domain.ModeAuto
	spec.Config.ConfineToWorkspace = true
	spec.Config.Confiner = stubConfiner{}
	spec.Config.Tools = registry

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := Once(ctx, spec)
	if err != nil {
		t.Fatalf("Once: %v", err)
	}
	if res.Turns != 2 {
		t.Fatalf("Result.Turns = %d, want 2 (the tool Turn, then the answering Turn)", res.Turns)
	}
	if res.FinalText != answer {
		t.Errorf("Result.FinalText = %q, want the last message %q", res.FinalText, answer)
	}
}

// TestOnceReportsNoAnswerWhenCancelled covers the empty half of the contract: a Firing
// stopped before any assistant message has no answer to report, and reports none rather
// than something older.
func TestOnceReportsNoAnswerWhenCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The Firing is cancelled the moment its first request lands, so the Upstream never
	// answers and no assistant message is ever committed.
	up := newUpstream(t, func(http.ResponseWriter, request) { cancel() })

	res, err := Once(ctx, planSpec(up.url, "think about it"))
	if err == nil {
		t.Fatal("Once returned no error; a cancelled firing must report one")
	}
	if res.FinalText != "" {
		t.Errorf("Result.FinalText = %q, want empty — the firing never reached an answer", res.FinalText)
	}
}

// TestOnceIgnoresASubAgentsAnswer pins the top-level half of the contract behaviourally: a
// sub-agent's message reports to its PARENT, never to the Firing's caller. The script
// delegates, lets the sub-agent answer, then cancels the parent before it can answer for
// itself — so the sub-agent's message is the only one the tap ever sees, and a capture that
// did not filter on Depth would hand its words back as the Firing's own.
func TestOnceIgnoresASubAgentsAnswer(t *testing.T) {
	t.Parallel()

	const (
		delegated = "list every open issue"
		subAnswer = "the sub-agent found four open issues"
	)

	registry := domain.NewToolRegistry()
	if err := registry.Register(tools.NewSubAgent()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	up := newUpstream(t, func(w http.ResponseWriter, req request) {
		switch {
		case req.lastRoleIs(domain.RoleTool):
			// The parent is back with the delegated result. Stop it here, before it can
			// answer: the sub-agent's message must be the only one on the stream.
			cancel()
		case req.lastTextHas(delegated):
			writeFinal(w, subAnswer) // the sub-agent's own fresh conversation
		default:
			writeToolCall(w, "call_1", tools.SubAgentToolName, `{"task":"`+delegated+`"}`)
		}
	})

	events := &recordingSink{}
	spec := planSpec(up.url, "summarise the day")
	spec.Config.Tools = registry
	spec.Config.Events = events

	res, _ := Once(ctx, spec)

	if !events.saw(1, subAnswer) {
		t.Fatalf("no depth-1 message reached the sink; the script proves nothing (saw %+v)", events.messages())
	}
	if events.saw(0, subAnswer) {
		t.Fatalf("the sub-agent's answer arrived at depth 0; the script proves nothing (saw %+v)", events.messages())
	}
	if res.FinalText != "" {
		t.Errorf("Result.FinalText = %q, want empty — a sub-agent's answer is its parent's, "+
			"never the Firing's", res.FinalText)
	}
}

// TestSpecTitle covers the three title sources and the truncation the browser's rows rely on.
func TestSpecTitle(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 4, 14, 5, 0, 0, time.Local)
	long := "rewrite the whole confinement contract from scratch and then write the tests too"

	tests := []struct {
		name string
		spec Spec
		want string
	}{
		{"explicit title wins", Spec{Title: "pinned", ScheduleName: "Nightly", Prompt: "ignored"}, "pinned"},
		{"schedule identity derives the clock form", Spec{ScheduleName: "Nightly", Prompt: "ignored"}, "Nightly — 14:05"},
		{"a short prompt is its own title", Spec{Prompt: "check the build"}, "check the build"},
		{"a first line wins over the rest", Spec{Prompt: "check the build\nand the tests"}, "check the build"},
		{"a long prompt truncates on a word boundary", Spec{Prompt: long}, "rewrite the whole confinement contract from…"},
		{"an empty prompt falls back to a dated label", Spec{Prompt: "   "}, "Session 2026-08-04"},
		{"a code fence has no useful title", Spec{Prompt: "```go\nfunc main() {}"}, "Session 2026-08-04"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.spec.title(now); got != tt.want {
				t.Errorf("title = %q, want %q", got, tt.want)
			}
		})
	}
}
