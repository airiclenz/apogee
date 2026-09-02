package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/agent"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/stubllm"
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
	if res.SessionID == callerRecordID {
		t.Errorf("Result.SessionID = %q, the id a caller names elsewhere; an empty Spec.RecordID mints its own",
			res.SessionID)
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

	// The record is engine-resumable AND carries its own scrollback: the runner folds the run's
	// entries onto Record.Transcript, so a resume repaints what the Firing did instead of taking
	// ADR 0022's no-scrollback degrade path.
	rec, err := store.Load(res.SessionID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entries, err := session.DecodeTranscript(rec.Transcript)
	if err != nil {
		t.Fatalf("DecodeTranscript: %v", err)
	}
	wantEntries := []session.Entry{
		{Kind: session.EntryKindUser, Text: "check the build"},
		{Kind: session.EntryKindAssistant, Text: "the build is green"},
	}
	if !reflect.DeepEqual(entries, wantEntries) {
		t.Errorf("the record's blob decodes to %+v, want %+v", entries, wantEntries)
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

// callerRecordID is the id a caller hands Once through Spec.RecordID below. It is shared with
// the minting test above, whose point is that an empty RecordID never produces this id.
const callerRecordID = "2026-08-24-090000-firing"

// TestOnceFilesTheRecordUnderTheCallersRecordID pins Spec.RecordID: a caller that already keyed
// something on the id before the run started — a Firing's scratch dir is created under it — gets
// the record filed under exactly that id, and the Result reports the id actually used.
func TestOnceFilesTheRecordUnderTheCallersRecordID(t *testing.T) {
	t.Parallel()

	up := newUpstream(t, alwaysFinal("filed where I asked"))
	store := session.NewStore(t.TempDir())

	spec := planSpec(up.url, "run under a name I chose")
	spec.Store = store
	spec.RecordID = callerRecordID
	spec.Now = at(time.Date(2026, 8, 24, 9, 0, 0, 0, time.Local))

	res, err := Once(context.Background(), spec)

	if err != nil {
		t.Fatalf("Once: %v", err)
	}
	if res.SessionID != callerRecordID {
		t.Errorf("Result.SessionID = %q, want the caller's %q", res.SessionID, callerRecordID)
	}
	rec, err := store.Load(callerRecordID)
	if err != nil {
		t.Fatalf("Load(%q): %v", callerRecordID, err)
	}
	if rec.Meta.ID != callerRecordID {
		t.Errorf("the loaded record's Meta.ID = %q, want %q", rec.Meta.ID, callerRecordID)
	}
}

// TestOnceReportsARecordIDThatCannotNameAFile: Once validates nothing about a caller's id, so
// one that cannot address a file inside the store is refused by session.Store and surfaces as
// the ordinary "save the firing's record" error — with the run's own Result still returned,
// because the Firing did reach its answer.
func TestOnceReportsARecordIDThatCannotNameAFile(t *testing.T) {
	t.Parallel()

	up := newUpstream(t, alwaysFinal("the run itself was fine"))
	store := session.NewStore(t.TempDir())

	spec := planSpec(up.url, "file me outside the store")
	spec.Store = store
	spec.RecordID = "../escape"

	res, err := Once(context.Background(), spec)

	if err == nil {
		t.Fatal("Once returned no error; an id that is not a safe filename component must be refused")
	}
	if !errors.Is(err, session.ErrInvalidID) {
		t.Errorf("Once error = %v, want one wrapping session.ErrInvalidID", err)
	}
	if !strings.Contains(err.Error(), "save the firing's record") {
		t.Errorf("Once error = %v, want the save-the-record error", err)
	}
	if res.FinalText != "the run itself was fine" {
		t.Errorf("Result.FinalText = %q, want the answer the firing did reach", res.FinalText)
	}
	if res.Err != nil {
		t.Errorf("Result.Err = %v, want nil: the run succeeded, only the save failed", res.Err)
	}
	if res.SessionID != "" {
		t.Errorf("Result.SessionID = %q, want empty: nothing was saved", res.SessionID)
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
			// DeepEqual rather than ==: Result carries a slice since it gained SubAgents.
			if !reflect.DeepEqual(res, Result{}) {
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

// TestOnceReportsAnAbandonedFinalTurn pins the fault half of the contract: a Firing whose
// final Turn the loop ABANDONED reports it as data on the Result, so an unattended caller
// with no event sink still learns its text is not an answer. The Exchange reached its
// boundary, so Err stays nil and the record still saves; only Faulted says otherwise.
//
// The empty reply is the fault the engine has always raised for this (the guard
// internal/agent/emptyreply_test.go pins): a stop-finished reply with no visible text and no
// tool call, which no Mechanism is registered here to recover.
func TestOnceReportsAnAbandonedFinalTurn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		reply       func(http.ResponseWriter, request)
		wantFaulted bool
	}{
		{"an abandoned turn", func(w http.ResponseWriter, _ request) { writeFinal(w, "") }, true},
		{"a clean run", alwaysFinal("the build is green"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			up := newUpstream(t, tt.reply)
			spec := planSpec(up.url, "check the build")
			spec.Store = session.NewStore(t.TempDir())

			res, err := Once(context.Background(), spec)

			if err != nil {
				t.Fatalf("Once: %v", err)
			}
			if res.Faulted != tt.wantFaulted {
				t.Fatalf("Result.Faulted = %v, want %v", res.Faulted, tt.wantFaulted)
			}
			if res.Err != nil {
				t.Errorf("Result.Err = %v, want nil — a fault is not a loop error", res.Err)
			}
			if tt.wantFaulted && res.Fault == "" {
				t.Error("Result.Fault is empty; an abandoned turn must name why")
			}
			if !tt.wantFaulted && res.Fault != "" {
				t.Errorf("Result.Fault = %q, want empty on a run that faulted nothing", res.Fault)
			}
			if res.SessionID == "" {
				t.Error("Result.SessionID is empty; a firing that reached its boundary saves its record")
			}
		})
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

// The routing fixture the three Spec-seam tests below share. The delegated task's own words are
// what tell the two conversations apart on the wire: the parent's requests never carry them —
// its first is the user's prompt and its second the delegation's result — so "which server
// answered the CHILD" is readable straight off a request log.
const (
	routedTask      = "list every open issue on the tracker"
	routedSubAnswer = "the sub-agent found four open issues"
	routedAnswer    = "the day is summarised"
)

// delegatingSession scripts the SESSION server for a Firing that delegates once: the sub_agent
// call, then the answer once the result is back. Its first turn is a TRAP — it answers the
// child's fresh conversation, which reaches this server only when nothing routed the child away
// — so a test that expects routing fails on the request log rather than on an unmatched 500,
// which would say the script was wrong instead of that the routing was.
func delegatingSession() stubllm.Script {
	return stubllm.Script{Model: "session-model", Turns: []stubllm.Turn{
		{When: &stubllm.Match{LastMessage: regexp.QuoteMeta(routedTask)}, Text: routedSubAnswer},
		{ToolCalls: []stubllm.ToolCall{{
			ID: "call_1", Name: tools.SubAgentToolName, Arguments: `{"task":"` + routedTask + `"}`,
		}}},
		{Text: routedAnswer},
	}}
}

// delegatingSpec is the Firing those tests fire: Plan mode against up, with sub_agent registered
// so the scripted call has something to dispatch.
func delegatingSpec(t *testing.T, up *stubllm.Server) Spec {
	t.Helper()

	registry := domain.NewToolRegistry()
	if err := registry.Register(tools.NewSubAgent()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	spec := planSpec(up.URL, "summarise the day")
	spec.Config.Model = up.Model
	spec.Config.Tools = registry
	return spec
}

// childRequests counts the requests up answered whose last message carries the delegated task —
// the child's own fresh conversation, and nothing the parent ever sends.
func childRequests(up *stubllm.Server) int {
	n := 0
	for _, r := range up.Requests() {
		if len(r.Messages) > 0 && strings.Contains(r.Messages[len(r.Messages)-1].Content, routedTask) {
			n++
		}
	}
	return n
}

// TestOnceRoutesADelegationToTheSpecsTarget is the item's headline: a Driver that resolved a
// Sub-agent server hands it to the Firing on the Spec, and the delegated child is built against
// THAT server while the parent's own conversation stays on the session one. It is the seam the
// TUI reached through Agent.SetDelegationTarget and no other Driver could.
func TestOnceRoutesADelegationToTheSpecsTarget(t *testing.T) {
	t.Parallel()

	sessionUp := stubllm.New(t, delegatingSession())
	targetUp := stubllm.New(t, stubllm.Script{Model: "grunt-model", Turns: []stubllm.Turn{
		{Text: routedSubAnswer},
	}})

	spec := delegatingSpec(t, sessionUp)
	spec.DelegationTarget = &agent.DelegationTarget{
		Endpoint: targetUp.URL,
		Model:    targetUp.Model,
	}

	res, err := Once(context.Background(), spec)
	if err != nil {
		t.Fatalf("Once: %v", err)
	}
	if res.FinalText != routedAnswer {
		t.Fatalf("Result.FinalText = %q, want %q — the parent never finished", res.FinalText, routedAnswer)
	}
	if got := childRequests(targetUp); got != 1 {
		t.Errorf("the target server answered %d of the child's requests, want 1 — the Spec's "+
			"DelegationTarget did not reach the Agent (target log %v)", got, targetUp.Requests())
	}
	if got := childRequests(sessionUp); got != 0 {
		t.Errorf("the session server answered %d of the child's requests, want 0 — the child was "+
			"built on the session Upstream despite a latched target", got)
	}
	if got := len(sessionUp.Requests()); got != 2 {
		t.Errorf("the session server answered %d requests, want 2 (the parent's two Turns)", got)
	}
}

// TestOnceWithoutARoutingSpecRunsEveryDelegationOnTheSessionServer is the other half of the
// seam: both fields nil is what every Firing did before it existed, and a second server standing
// ready proves the nil is a NO-OP rather than a default that routes somewhere.
func TestOnceWithoutARoutingSpecRunsEveryDelegationOnTheSessionServer(t *testing.T) {
	t.Parallel()

	sessionUp := stubllm.New(t, delegatingSession())
	idleUp := stubllm.New(t, stubllm.Script{Model: "grunt-model", Turns: []stubllm.Turn{
		{Text: routedSubAnswer},
	}})

	spec := delegatingSpec(t, sessionUp)

	res, err := Once(context.Background(), spec)
	if err != nil {
		t.Fatalf("Once: %v", err)
	}
	if res.FinalText != routedAnswer {
		t.Fatalf("Result.FinalText = %q, want %q — the parent never finished", res.FinalText, routedAnswer)
	}
	if got := len(idleUp.Requests()); got != 0 {
		t.Errorf("the second server answered %d requests, want 0 — an unset DelegationTarget "+
			"routed something (log %v)", got, idleUp.Requests())
	}
	if got := childRequests(sessionUp); got != 1 {
		t.Errorf("the session server answered %d of the child's requests, want 1 — the child did "+
			"not run on the session Upstream", got)
	}
	if got := len(sessionUp.Requests()); got != 3 {
		t.Errorf("the session server answered %d requests, want 3 (the parent's two Turns and the "+
			"child's one)", got)
	}
}

// TestOnceCarriesASeatWithoutRoutingAnything pins the two fields as INDEPENDENT. A seat is
// display text — what the orientation block tells the model about the far server (ADR 0069) —
// and carrying one moves no delegation: without a target beside it every child still runs on the
// session Upstream. What the engine RENDERED from the seat is not observable from here (the
// Delegations line needs a registry that publishes run_on, which a Firing's own roster does not
// build), so this asserts the routing half alone; the rendered seat is the seat-choice Driver's
// test, not this seam's.
func TestOnceCarriesASeatWithoutRoutingAnything(t *testing.T) {
	t.Parallel()

	sessionUp := stubllm.New(t, delegatingSession())
	idleUp := stubllm.New(t, stubllm.Script{Model: "grunt-model", Turns: []stubllm.Turn{
		{Text: routedSubAnswer},
	}})

	spec := delegatingSpec(t, sessionUp)
	spec.DelegationSeat = &agent.DelegationSeat{
		Name:        "grunt-box",
		Description: "fast local 27B — search and edits",
		Model:       "grunt-model",
	}

	res, err := Once(context.Background(), spec)
	if err != nil {
		t.Fatalf("Once: %v", err)
	}
	if res.FinalText != routedAnswer {
		t.Fatalf("Result.FinalText = %q, want %q — the parent never finished", res.FinalText, routedAnswer)
	}
	if got := len(idleUp.Requests()); got != 0 {
		t.Errorf("the second server answered %d requests, want 0 — a seat without a target routed "+
			"a delegation (log %v)", got, idleUp.Requests())
	}
	if got := childRequests(sessionUp); got != 1 {
		t.Errorf("the session server answered %d of the child's requests, want 1 — the child did "+
			"not run on the session Upstream", got)
	}
}

// TestOnceReportsEachSubAgentsContextFill is the headline of the per-run readout: a delegated
// run fills a window of its OWN, and that fill reaches the Firing's caller on
// Result.SubAgents — labelled by the first line of the task it was given, and without
// disturbing the Firing's own reading, which stays the top-level one the record's gauge
// relights from. The script gives the parent and the child deliberately different totals, so
// a tap that confused the two would be caught by either assertion.
func TestOnceReportsEachSubAgentsContextFill(t *testing.T) {
	t.Parallel()

	const (
		taskArgs = `{"task":"audit every open issue\nthen summarise what you found"}`
		taskLine = "audit every open issue"
		window   = 32000
	)

	registry := domain.NewToolRegistry()
	if err := registry.Register(tools.NewSubAgent()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	up := newUpstream(t, func(w http.ResponseWriter, req request) {
		switch {
		case req.lastRoleIs(domain.RoleTool):
			// The parent is back with the delegated result and answers for itself.
			writeUsage(w, 800, 100, 900)
			writeFinal(w, "four issues are open")
		case req.lastTextHas(taskLine):
			// The sub-agent's own fresh conversation — and its own, far fuller window.
			writeUsage(w, 11800, 200, 12000)
			writeFinal(w, "the sub-agent found four open issues")
		default:
			writeUsage(w, 600, 100, 700)
			writeToolCall(w, "call_1", tools.SubAgentToolName, taskArgs)
		}
	})

	store := session.NewStore(t.TempDir())
	spec := planSpec(up.url, "summarise the day")
	spec.Config.Tools = registry
	spec.Config.Context.MaxContextTokens = window
	spec.Store = store

	res, err := Once(context.Background(), spec)
	if err != nil {
		t.Fatalf("Once: %v", err)
	}

	if len(res.SubAgents) != 1 {
		t.Fatalf("Result.SubAgents = %+v, want exactly one finished run", res.SubAgents)
	}
	// The entry carries the child's cumulative spend beside its fill: one call, the counts this
	// script gave it. TestOnceReportsWhatTheFiringSpent is where that half is the point; here it
	// travels along because the assertion is on the WHOLE entry.
	want := SubAgentUsage{
		Used: 12000, Limit: window, Task: taskLine,
		Calls: 1, PromptTokens: 11800, CompletionTokens: 200, TotalTokens: 12000,
	}
	if res.SubAgents[0] != want {
		t.Errorf("Result.SubAgents[0] = %+v, want %+v", res.SubAgents[0], want)
	}

	metas, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("store holds %d records, want 1", len(metas))
	}
	if metas[0].CtxUsed != 900 {
		t.Errorf("Meta.CtxUsed = %d, want 900 — the sub-agent's 12000 fills its own window, "+
			"never the firing's", metas[0].CtxUsed)
	}
}

// subAgentCall is the delegating tool-call event a parent at depth emits, opening the bracket
// for its child.
func subAgentCall(depth int, id, task string) domain.ToolCallEvent {
	return domain.ToolCallEvent{
		EventBase: domain.EventBase{Depth: depth},
		Call: domain.ToolCall{
			ID:        id,
			Tool:      tools.SubAgentToolName,
			Arguments: json.RawMessage(`{"task":"` + task + `"}`),
		},
	}
}

// toolResult is the result event that closes call id, emitted at the CALLER's depth.
func toolResult(depth int, id string) domain.ToolResultEvent {
	return domain.ToolResultEvent{
		EventBase: domain.EventBase{Depth: depth},
		Result:    domain.ToolResult{CallID: id, Content: "done"},
	}
}

// usageAt is one Turn's token accounting at depth, reported by the agent spawnCallID
// delegated to ("" for the Firing's own top-level agent — the run identity every event now
// carries, domain.EventBase.CallID).
func usageAt(depth int, spawnCallID string, total int) domain.UsageEvent {
	return domain.UsageEvent{
		EventBase:   domain.EventBase{Depth: depth, CallID: spawnCallID},
		TotalTokens: total,
	}
}

// TestEventTapKeepsTheFiringsFillFreeOfADelegatedRun pins the top-level filter the record's
// CtxUsed depends on — a depth-1 usage event must never move the Firing's own reading — and,
// on the same stream, the latest-wins semantics of the run's reading: a Turn restates the
// whole fill, so the run reports what it ENDED at, not the sum of what it passed through.
func TestEventTapKeepsTheFiringsFillFreeOfADelegatedRun(t *testing.T) {
	t.Parallel()

	const window = 32000
	tap := &eventTap{window: window}

	tap.Emit(usageAt(0, "", 900))
	tap.Emit(subAgentCall(0, "call_1", "audit the issues"))
	tap.Emit(usageAt(1, "call_1", 5000))
	tap.Emit(usageAt(1, "call_1", 12000))

	if got := tap.fill(); got != 900 {
		t.Errorf("fill() = %d, want 900 — a delegated run's usage is not the firing's", got)
	}
	if runs := tap.subAgentRuns(); len(runs) != 0 {
		t.Errorf("subAgentRuns() = %+v while the run is still open, want none", runs)
	}

	tap.Emit(toolResult(0, "call_1"))

	runs := tap.subAgentRuns()
	if len(runs) != 1 {
		t.Fatalf("subAgentRuns() = %+v, want exactly one finished run", runs)
	}
	want := SubAgentUsage{Used: 12000, Limit: window, Task: "audit the issues"}
	if runs[0] != want {
		t.Errorf("subAgentRuns()[0] = %+v, want %+v", runs[0], want)
	}
	if got := tap.fill(); got != 900 {
		t.Errorf("fill() = %d after the run closed, want 900", got)
	}
}

// TestEventTapReportsAModelOnlyWhenItDiffers pins what SubAgentUsage.Model means: the model a run
// went to when that is NOT the Firing's own — a delegation routed to the Sub-agent server (ADR
// 0045) — and nothing when the two match, so a surface prints the cell without holding the session's
// model to compare against. A stream that names no model at all is every stream a build without
// routing produced, and it reports none.
func TestEventTapReportsAModelOnlyWhenItDiffers(t *testing.T) {
	t.Parallel()

	const window = 32000

	usageOn := func(callID, model string, total int) domain.UsageEvent {
		ev := usageAt(1, callID, total)
		ev.Model = model
		return ev
	}

	tap := &eventTap{window: window, model: "gpt-oss-20b"}
	tap.Emit(subAgentCall(0, "call_1", "audit the issues"))
	tap.Emit(usageOn("call_1", "qwen3-4b", 12000)) // routed: another server, another model
	tap.Emit(toolResult(0, "call_1"))
	tap.Emit(subAgentCall(0, "call_2", "summarise the findings"))
	tap.Emit(usageOn("call_2", "gpt-oss-20b", 4000)) // fell back to the session's own upstream
	tap.Emit(toolResult(0, "call_2"))
	tap.Emit(subAgentCall(0, "call_3", "check the docs"))
	tap.Emit(usageAt(1, "call_3", 2000)) // a stream from before the model was stamped at all
	tap.Emit(toolResult(0, "call_3"))

	want := []SubAgentUsage{
		{Used: 12000, Limit: window, Task: "audit the issues", Model: "qwen3-4b"},
		{Used: 4000, Limit: window, Task: "summarise the findings"},
		{Used: 2000, Limit: window, Task: "check the docs"},
	}
	if runs := tap.subAgentRuns(); !slices.Equal(runs, want) {
		t.Errorf("subAgentRuns() = %+v, want %+v", runs, want)
	}
}

// TestEventTapReportsTheChildsOwnWindow pins SubAgentUsage.Limit's other half, and it inverts the
// model rule above on purpose: a routed child works against the Delegation target's window (ADR
// 0045), so its fill must be reported against THAT number — a 7k fill on an 8k grunt server is
// `7k/8k`, not `7k/128k` against the Firing's window — while a reading that names no window falls
// back to the Firing's, which is exactly the window an unrouted child inherited.
func TestEventTapReportsTheChildsOwnWindow(t *testing.T) {
	t.Parallel()

	const firingWindow = 131072

	usageIn := func(callID string, total, window int) domain.UsageEvent {
		ev := usageAt(1, callID, total)
		ev.ContextWindow = window
		return ev
	}

	tap := &eventTap{window: firingWindow}
	tap.Emit(subAgentCall(0, "call_1", "audit the issues"))
	tap.Emit(usageIn("call_1", 7000, 8192)) // routed: a small window on the grunt server
	tap.Emit(toolResult(0, "call_1"))
	tap.Emit(subAgentCall(0, "call_2", "summarise the findings"))
	tap.Emit(usageAt(1, "call_2", 4000)) // unrouted, or a stream from before the stamp existed
	tap.Emit(toolResult(0, "call_2"))
	tap.Emit(subAgentCall(0, "call_3", "check the docs"))
	tap.Emit(usageIn("call_3", 2000, 8192))
	tap.Emit(usageAt(1, "call_3", 3000)) // names none: the established window stands, not the Firing's
	tap.Emit(toolResult(0, "call_3"))

	want := []SubAgentUsage{
		{Used: 7000, Limit: 8192, Task: "audit the issues"},
		{Used: 4000, Limit: firingWindow, Task: "summarise the findings"},
		{Used: 3000, Limit: 8192, Task: "check the docs"},
	}
	if runs := tap.subAgentRuns(); !slices.Equal(runs, want) {
		t.Errorf("subAgentRuns() = %+v, want %+v", runs, want)
	}
}

// TestEventTapAttributesANestedRunToItsOwnDepth pins the non-transitivity: each agent fills
// its own window, so a grandchild's reading belongs to the grandchild's entry alone and
// nothing rolls up into the run that spawned it. The nested run also finishes FIRST, which is
// the order Result.SubAgents reports.
func TestEventTapAttributesANestedRunToItsOwnDepth(t *testing.T) {
	t.Parallel()

	const window = 32000
	tap := &eventTap{window: window}

	tap.Emit(subAgentCall(0, "call_1", "outer task"))
	tap.Emit(usageAt(1, "call_1", 4000))
	tap.Emit(subAgentCall(1, "call_2", "nested task"))
	tap.Emit(usageAt(2, "call_2", 9000))
	tap.Emit(toolResult(1, "call_2"))    // the nested run closes first
	tap.Emit(usageAt(1, "call_1", 5000)) // the outer run keeps going, on its own window
	tap.Emit(toolResult(0, "call_1"))

	want := []SubAgentUsage{
		{Used: 9000, Limit: window, Task: "nested task"},
		{Used: 5000, Limit: window, Task: "outer task"},
	}
	runs := tap.subAgentRuns()
	if len(runs) != len(want) {
		t.Fatalf("subAgentRuns() = %+v, want %+v", runs, want)
	}
	for i := range want {
		if runs[i] != want[i] {
			t.Errorf("subAgentRuns()[%d] = %+v, want %+v", i, runs[i], want[i])
		}
	}
}

// TestEventTapAttributesTwoRunsFromOneTurnByCallID pins the re-keying (ADR 0039): two
// delegations dispatched in ONE Turn share a depth, so nothing but each reading's run identity
// can say whose fill it is. The readings interleave and the runs close out of dispatch order,
// which is exactly the shape a concurrent fan-out produces — a depth-keyed bracket would report
// whichever landed last as both runs' fill, and lose one of them entirely.
func TestEventTapAttributesTwoRunsFromOneTurnByCallID(t *testing.T) {
	t.Parallel()

	const window = 32000
	tap := &eventTap{window: window}

	tap.Emit(subAgentCall(0, "call_a", "audit the issues"))
	tap.Emit(subAgentCall(0, "call_b", "write the docs"))
	tap.Emit(usageAt(1, "call_a", 4000))
	tap.Emit(usageAt(1, "call_b", 9000))
	tap.Emit(usageAt(1, "call_a", 7000)) // a later Turn of the FIRST child restates its whole fill
	tap.Emit(toolResult(0, "call_b"))    // the second child finishes first
	tap.Emit(toolResult(0, "call_a"))

	want := []SubAgentUsage{
		{Used: 9000, Limit: window, Task: "write the docs"},
		{Used: 7000, Limit: window, Task: "audit the issues"},
	}
	runs := tap.subAgentRuns()
	if len(runs) != len(want) {
		t.Fatalf("subAgentRuns() = %+v, want %+v", runs, want)
	}
	for i := range want {
		if runs[i] != want[i] {
			t.Errorf("subAgentRuns()[%d] = %+v, want %+v", i, runs[i], want[i])
		}
	}
}

// namedSubAgentCall is the delegating tool-call event for a delegation that carries the OPTIONAL
// short name beside its task.
func namedSubAgentCall(depth int, id, task, name string) domain.ToolCallEvent {
	args, err := json.Marshal(tools.SubAgentArgs{Task: task, Name: name})
	if err != nil {
		panic("marshal sub_agent args: " + err.Error())
	}
	return domain.ToolCallEvent{
		EventBase: domain.EventBase{Depth: depth},
		Call:      domain.ToolCall{ID: id, Tool: tools.SubAgentToolName, Arguments: args},
	}
}

// subAgentNamed is the out-of-band naming event for the run call id delegated (ADR 0068), emitted
// at the CHILD's depth with the spawning call as its run identity — the stamp every event that
// child emits carries.
func subAgentNamed(depth int, id, name string) domain.SubAgentNamedEvent {
	return domain.SubAgentNamedEvent{
		EventBase: domain.EventBase{Depth: depth, CallID: id},
		Name:      name,
	}
}

// TestEventTapFoldsTheGeneratedDelegationName pins the second event that feeds SubAgentUsage.Name
// (ADR 0068): a delegation the model left unnamed is GIVEN a name while it runs, and the reading
// filed when it closes carries that name instead of nothing — which is what makes a headless run
// report a generated name on its sub-agent line. A delegation whose own call named it is never
// renamed, because no event is emitted for one: the name the model gave wins by never being
// contested, and this pins that it still stands beside a sibling that was renamed.
func TestEventTapFoldsTheGeneratedDelegationName(t *testing.T) {
	t.Parallel()

	const window = 32000
	tap := &eventTap{window: window}

	tap.Emit(subAgentCall(0, "call_a", "audit the config loader"))
	tap.Emit(namedSubAgentCall(0, "call_b", "summarise the findings", "scribe"))
	tap.Emit(usageAt(1, "call_a", 4000))
	tap.Emit(usageAt(1, "call_b", 9000))
	tap.Emit(subAgentNamed(1, "call_a", "audit config keys"))
	tap.Emit(toolResult(0, "call_a"))
	tap.Emit(toolResult(0, "call_b"))

	want := []SubAgentUsage{
		{Used: 4000, Limit: window, Task: "audit the config loader", Name: "audit config keys"},
		{Used: 9000, Limit: window, Task: "summarise the findings", Name: "scribe"},
	}
	runs := tap.subAgentRuns()
	if len(runs) != len(want) {
		t.Fatalf("subAgentRuns() = %+v, want %+v", runs, want)
	}
	for i := range want {
		if runs[i] != want[i] {
			t.Errorf("subAgentRuns()[%d] = %+v, want %+v", i, runs[i], want[i])
		}
	}
}

// TestEventTapDropsANameWithNothingToName pins the two names that change no reading: one for a call
// this tap opened no bracket for — a run it never saw, or one that has already closed, the same
// drop every unbracketed reading takes — and an empty one, which would replace a usable label with
// nothing.
func TestEventTapDropsANameWithNothingToName(t *testing.T) {
	t.Parallel()

	const window = 32000
	tap := &eventTap{window: window}

	tap.Emit(subAgentCall(0, "call_a", "audit the config loader"))
	tap.Emit(usageAt(1, "call_a", 4000))
	tap.Emit(subAgentNamed(1, "call_gone", "names nobody here"))
	tap.Emit(subAgentNamed(1, "call_a", ""))
	tap.Emit(toolResult(0, "call_a"))

	want := SubAgentUsage{Used: 4000, Limit: window, Task: "audit the config loader"}
	runs := tap.subAgentRuns()
	if len(runs) != 1 {
		t.Fatalf("subAgentRuns() = %+v, want exactly one finished run", runs)
	}
	if runs[0] != want {
		t.Errorf("subAgentRuns()[0] = %+v, want %+v", runs[0], want)
	}
}

// TestEventTapRecordsTheDelegationName pins what a headless caller can say about a run beyond its
// fill: the short name the delegation was given, normalised on the way in the way the recursion
// point normalises it (first line, trimmed), so a record carries a label a line can hold. A
// delegation that named nothing records nothing — the emptiness that means "fall back to the task".
func TestEventTapRecordsTheDelegationName(t *testing.T) {
	t.Parallel()

	const window = 32000
	tap := &eventTap{window: window}

	tap.Emit(namedSubAgentCall(0, "call_a", "audit the issues", "  repo-scout \n and some prose"))
	tap.Emit(subAgentCall(0, "call_b", "write the docs"))
	tap.Emit(usageAt(1, "call_a", 4000))
	tap.Emit(usageAt(1, "call_b", 9000))
	tap.Emit(toolResult(0, "call_a"))
	tap.Emit(toolResult(0, "call_b"))

	want := []SubAgentUsage{
		{Used: 4000, Limit: window, Task: "audit the issues", Name: "repo-scout"},
		{Used: 9000, Limit: window, Task: "write the docs"},
	}
	runs := tap.subAgentRuns()
	if len(runs) != len(want) {
		t.Fatalf("subAgentRuns() = %+v, want %+v", runs, want)
	}
	for i := range want {
		if runs[i] != want[i] {
			t.Errorf("subAgentRuns()[%d] = %+v, want %+v", i, runs[i], want[i])
		}
	}
}

// TestEventTapDropsWhatItCannotAttribute covers the defensive edges: usage with no run to
// belong to, a result that closes some OTHER call, a plain tool that is not a delegation at
// all, and a run whose Upstream never reported usage — the last one is skipped rather than
// reported as a fill of zero, which would read as an empty window.
func TestEventTapDropsWhatItCannotAttribute(t *testing.T) {
	t.Parallel()

	tap := &eventTap{window: 32000}

	// Nothing is open: a deep usage event has no owner and is dropped.
	tap.Emit(usageAt(1, "call_9", 7000))
	tap.Emit(toolResult(0, "call_0"))
	// A plain tool call opens no bracket, so the usage that follows it stays unattributed.
	tap.Emit(domain.ToolCallEvent{
		EventBase: domain.EventBase{},
		Call:      domain.ToolCall{ID: "call_1", Tool: "read_file"},
	})
	tap.Emit(usageAt(1, "call_1", 7000))
	if runs := tap.subAgentRuns(); len(runs) != 0 {
		t.Fatalf("subAgentRuns() = %+v, want none — nothing was ever delegated", runs)
	}

	// A real delegation, but the same Turn's OTHER tool result must not close it.
	tap.Emit(subAgentCall(0, "call_2", "audit the issues"))
	tap.Emit(usageAt(1, "call_2", 6000))
	tap.Emit(toolResult(0, "call_1"))
	if runs := tap.subAgentRuns(); len(runs) != 0 {
		t.Fatalf("subAgentRuns() = %+v, want none — call_1 is not the delegating call", runs)
	}
	tap.Emit(toolResult(0, "call_2"))
	if runs := tap.subAgentRuns(); len(runs) != 1 || runs[0].Used != 6000 {
		t.Fatalf("subAgentRuns() = %+v, want the one run at 6000", runs)
	}

	// A run whose server reported no usage at all reports nothing.
	tap.Emit(subAgentCall(0, "call_3", "silent task"))
	tap.Emit(toolResult(0, "call_3"))
	if runs := tap.subAgentRuns(); len(runs) != 1 {
		t.Errorf("subAgentRuns() = %+v, want the silent run absent", runs)
	}
}

// usageWithTotals is one accounting event in the shape internal/agent stamps: the call's OWN
// counts in the fill fields, the emitting agent's running counters in the cumulative ones.
// maintenance marks the Compaction fold — an event whose tokens are real spend but whose fill
// describes the summarizer's request rather than the conversation.
func usageWithTotals(depth int, spawnCallID string, fill int, cumulative Usage, maintenance bool) domain.UsageEvent {
	return domain.UsageEvent{
		EventBase:                  domain.EventBase{Depth: depth, CallID: spawnCallID},
		TotalTokens:                fill,
		CumulativePromptTokens:     cumulative.PromptTokens,
		CumulativeCompletionTokens: cumulative.CompletionTokens,
		CumulativeTotalTokens:      cumulative.TotalTokens,
		CumulativeCalls:            cumulative.Calls,
		Maintenance:                maintenance,
	}
}

// TestEventTapKeepsCumulativeTotalsPerAgent pins the second grain of the readout: an agent's
// running totals ride its own events, so the Firing's land on Result.Usage and a delegated run's
// on its own entry — never mixed, never summed by the tap. The script gives the child far bigger
// totals than the parent and lets the parent report AGAIN after the child closed, so a tap that
// folded the two together would be caught by either assertion.
func TestEventTapKeepsCumulativeTotalsPerAgent(t *testing.T) {
	t.Parallel()

	const window = 32000
	tap := &eventTap{window: window}

	tap.Emit(usageWithTotals(0, "", 900, Usage{Calls: 1, PromptTokens: 800, CompletionTokens: 100, TotalTokens: 900}, false))
	tap.Emit(subAgentCall(0, "call_1", "audit the issues"))
	tap.Emit(usageWithTotals(1, "call_1", 5000, Usage{Calls: 1, PromptTokens: 4800, CompletionTokens: 200, TotalTokens: 5000}, false))
	tap.Emit(usageWithTotals(1, "call_1", 12000, Usage{Calls: 2, PromptTokens: 16600, CompletionTokens: 400, TotalTokens: 17000}, false))
	tap.Emit(toolResult(0, "call_1"))
	tap.Emit(usageWithTotals(0, "", 1800, Usage{Calls: 2, PromptTokens: 2500, CompletionTokens: 200, TotalTokens: 2700}, false))

	wantTotals := Usage{Calls: 2, PromptTokens: 2500, CompletionTokens: 200, TotalTokens: 2700}
	if got := tap.totals(); got != wantTotals {
		t.Errorf("totals() = %+v, want %+v — the firing counts its own calls only", got, wantTotals)
	}

	runs := tap.subAgentRuns()
	if len(runs) != 1 {
		t.Fatalf("subAgentRuns() = %+v, want exactly one finished run", runs)
	}
	want := SubAgentUsage{
		Used: 12000, Limit: window, Task: "audit the issues",
		Calls: 2, PromptTokens: 16600, CompletionTokens: 400, TotalTokens: 17000,
	}
	if runs[0] != want {
		t.Errorf("subAgentRuns()[0] = %+v, want %+v", runs[0], want)
	}
}

// TestEventTapCountsMaintenanceInTheTotalsOnly pins the flagged Compaction event's split
// treatment at BOTH grains: its tokens are real spend and advance the totals, while the fill it
// carries describes the summarizer's own request and must leave the context reading where the
// last Turn put it. Getting this wrong the other way would report a fold as the run's fill —
// which is what the engine's old silence about compaction avoided by losing the tokens entirely.
func TestEventTapCountsMaintenanceInTheTotalsOnly(t *testing.T) {
	t.Parallel()

	const window = 32000
	tap := &eventTap{window: window}

	tap.Emit(usageWithTotals(0, "", 12000, Usage{Calls: 1, PromptTokens: 11800, CompletionTokens: 200, TotalTokens: 12000}, false))
	tap.Emit(usageWithTotals(0, "", 14000, Usage{Calls: 2, PromptTokens: 25600, CompletionTokens: 600, TotalTokens: 26000}, true))

	if got := tap.fill(); got != 12000 {
		t.Errorf("fill() = %d after a maintenance event, want 12000 — a fold does not restate the window", got)
	}
	wantFolded := Usage{Calls: 2, PromptTokens: 25600, CompletionTokens: 600, TotalTokens: 26000}
	if got := tap.totals(); got != wantFolded {
		t.Errorf("totals() = %+v, want %+v — a fold's tokens are spent tokens", got, wantFolded)
	}

	// The Turn after the fold restates the (now much smaller) window and keeps counting from the
	// totals the fold left behind.
	tap.Emit(usageWithTotals(0, "", 3000, Usage{Calls: 3, PromptTokens: 28400, CompletionTokens: 800, TotalTokens: 29000}, false))
	if got := tap.fill(); got != 3000 {
		t.Errorf("fill() = %d after the post-fold turn, want 3000", got)
	}
	wantAfter := Usage{Calls: 3, PromptTokens: 28400, CompletionTokens: 800, TotalTokens: 29000}
	if got := tap.totals(); got != wantAfter {
		t.Errorf("totals() = %+v, want %+v", got, wantAfter)
	}

	// A delegated run folds too, and the same split holds one level down: its entry reports the
	// fill of its last TURN and the totals of everything it spent, the fold included.
	tap.Emit(subAgentCall(0, "call_1", "audit the issues"))
	tap.Emit(usageWithTotals(1, "call_1", 9000, Usage{Calls: 1, PromptTokens: 8800, CompletionTokens: 200, TotalTokens: 9000}, false))
	tap.Emit(usageWithTotals(1, "call_1", 11000, Usage{Calls: 2, PromptTokens: 19600, CompletionTokens: 400, TotalTokens: 20000}, true))
	tap.Emit(toolResult(0, "call_1"))

	runs := tap.subAgentRuns()
	if len(runs) != 1 {
		t.Fatalf("subAgentRuns() = %+v, want exactly one finished run", runs)
	}
	wantRun := SubAgentUsage{
		Used: 9000, Limit: window, Task: "audit the issues",
		Calls: 2, PromptTokens: 19600, CompletionTokens: 400, TotalTokens: 20000,
	}
	if runs[0] != wantRun {
		t.Errorf("subAgentRuns()[0] = %+v, want %+v", runs[0], wantRun)
	}
}

// TestOnceReportsWhatTheFiringSpent is the end-to-end half of the totals: the engine stamps the
// cumulative fields, the tap keeps the latest per agent, and Result.Usage carries the Firing's own
// spend to a caller that never saw the events. The script's two parent calls and one child call
// have deliberately different counts, so the assertion fails on a tap that kept a single call's
// numbers, summed the stream, or let the child's totals reach the parent's.
func TestOnceReportsWhatTheFiringSpent(t *testing.T) {
	t.Parallel()

	const (
		taskArgs = `{"task":"audit every open issue"}`
		taskLine = "audit every open issue"
	)

	registry := domain.NewToolRegistry()
	if err := registry.Register(tools.NewSubAgent()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	up := newUpstream(t, func(w http.ResponseWriter, req request) {
		switch {
		case req.lastRoleIs(domain.RoleTool):
			writeUsage(w, 800, 100, 900)
			writeFinal(w, "four issues are open")
		case req.lastTextHas(taskLine):
			writeUsage(w, 11800, 200, 12000)
			writeFinal(w, "the sub-agent found four open issues")
		default:
			writeUsage(w, 600, 100, 700)
			writeToolCall(w, "call_1", tools.SubAgentToolName, taskArgs)
		}
	})

	spec := planSpec(up.url, "summarise the day")
	spec.Config.Tools = registry
	spec.Config.Context.MaxContextTokens = 32000

	res, err := Once(context.Background(), spec)
	if err != nil {
		t.Fatalf("Once: %v", err)
	}

	wantUsage := Usage{Calls: 2, PromptTokens: 1400, CompletionTokens: 200, TotalTokens: 1600}
	if res.Usage != wantUsage {
		t.Errorf("Result.Usage = %+v, want %+v — the firing's two calls, and only those", res.Usage, wantUsage)
	}
	if len(res.SubAgents) != 1 {
		t.Fatalf("Result.SubAgents = %+v, want exactly one finished run", res.SubAgents)
	}
	got := res.SubAgents[0]
	if got.Calls != 1 || got.PromptTokens != 11800 || got.CompletionTokens != 200 || got.TotalTokens != 12000 {
		t.Errorf("Result.SubAgents[0] = %+v, want the child's own single call (11800/200/12000)", got)
	}
}

// TestOnceRecordsWhatTheFiringAndItsDelegatesSpent is the producer half of the /sessions spend
// cell: an unattended record carries BOTH grains, because nothing else will fill them — a
// Schedule fires and walks away, so there is no Driver holding run heads at Save. Meta.Usage
// takes the Firing's own totals and Meta.DelegateUsage the SUM over its delegated runs. The
// script delegates TWICE with deliberately different counts, so a save that kept one run's
// numbers instead of summing them fails, as does one that let a child's spend reach Meta.Usage.
func TestOnceRecordsWhatTheFiringAndItsDelegatesSpent(t *testing.T) {
	t.Parallel()

	const (
		firstTask  = "audit every open issue"
		secondTask = "summarise the release notes"
		firstArgs  = `{"task":"audit every open issue"}`
		secondArgs = `{"task":"summarise the release notes"}`
	)

	registry := domain.NewToolRegistry()
	if err := registry.Register(tools.NewSubAgent()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	up := newUpstream(t, func(w http.ResponseWriter, req request) {
		switch {
		case req.lastRoleIs(domain.RoleTool) && req.toolMsgs() == 1:
			// The first delegation is back; the parent spends again and delegates once more.
			writeUsage(w, 800, 100, 900)
			writeToolCall(w, "call_2", tools.SubAgentToolName, secondArgs)
		case req.lastRoleIs(domain.RoleTool):
			// The second is back too; the parent answers for itself.
			writeUsage(w, 900, 100, 1000)
			writeFinal(w, "both audits are done")
		case req.lastTextHas(firstTask):
			writeUsage(w, 5000, 50, 5050)
			writeFinal(w, "four issues are open")
		case req.lastTextHas(secondTask):
			writeUsage(w, 3000, 30, 3030)
			writeFinal(w, "the notes are ready")
		default:
			writeUsage(w, 600, 100, 700)
			writeToolCall(w, "call_1", tools.SubAgentToolName, firstArgs)
		}
	})

	store := session.NewStore(t.TempDir())
	spec := planSpec(up.url, "close out the day")
	spec.Config.Tools = registry
	spec.Config.Context.MaxContextTokens = 32000
	spec.Store = store

	res, err := Once(context.Background(), spec)
	if err != nil {
		t.Fatalf("Once: %v", err)
	}
	if len(res.SubAgents) != 2 {
		t.Fatalf("Result.SubAgents = %+v, want two finished runs; the script proves nothing", res.SubAgents)
	}

	rec, err := store.Load(res.SessionID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	wantOwn := session.Usage{Calls: 3, PromptTokens: 2300, CompletionTokens: 300, TotalTokens: 2600}
	if rec.Meta.Usage != wantOwn {
		t.Errorf("Meta.Usage = %+v, want %+v — the firing's own three calls, and only those",
			rec.Meta.Usage, wantOwn)
	}
	wantDelegated := session.Usage{Calls: 2, PromptTokens: 8000, CompletionTokens: 80, TotalTokens: 8080}
	if rec.Meta.DelegateUsage != wantDelegated {
		t.Errorf("Meta.DelegateUsage = %+v, want %+v — the SUM over both delegated runs",
			rec.Meta.DelegateUsage, wantDelegated)
	}
}

// TestOnceRecordsTheRunsScrollback is item 4's headline: the saved record carries a transcript
// blob folded from the run's own Event stream, so an unattended Firing REPLAYS in /sessions
// instead of taking ADR 0022's no-scrollback degrade path. The script exercises every kind the
// fold writes — the submitted prompt, a plain tool call and its result, a delegation and the
// text its child committed at depth 1, and the parent's final answer.
func TestOnceRecordsTheRunsScrollback(t *testing.T) {
	t.Parallel()

	const delegated = "summarise the notes"

	registry := domain.NewToolRegistry()
	if err := registry.Register(notingTool{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := registry.Register(tools.NewSubAgent()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	up := newUpstream(t, func(w http.ResponseWriter, req request) {
		switch {
		case req.lastTextHas(delegated):
			writeFinal(w, "the notes are short")
		case req.lastRoleIs(domain.RoleTool) && req.toolMsgs() == 1:
			writeToolCall(w, "call_2", tools.SubAgentToolName, `{"task":"`+delegated+`"}`)
		case req.lastRoleIs(domain.RoleTool):
			writeFinal(w, "all done")
		default:
			writeToolCall(w, "call_1", "note_something", `{"note":"hello"}`)
		}
	})

	store := session.NewStore(t.TempDir())
	spec := planSpec(up.url, "close out the day")
	spec.Config.Tools = registry
	spec.Store = store

	res, err := Once(context.Background(), spec)
	if err != nil {
		t.Fatalf("Once: %v", err)
	}

	rec, err := store.Load(res.SessionID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entries, err := session.DecodeTranscript(rec.Transcript)
	if err != nil {
		t.Fatalf("DecodeTranscript: %v", err)
	}

	want := []foldedEntry{
		{kind: session.EntryKindUser, text: "close out the day"},
		{kind: session.EntryKindToolCall, name: "note_something", args: `{"note":"hello"}`, done: true},
		{kind: session.EntryKindToolResult, text: "noted: hello"},
		{kind: session.EntryKindToolCall, name: tools.SubAgentToolName, args: `{"task":"` + delegated + `"}`, done: true},
		{kind: session.EntryKindAssistant, text: "the notes are short", depth: 1, spawn: "call_2"},
		{kind: session.EntryKindToolResult, text: "the notes are short"},
		{kind: session.EntryKindAssistant, text: "all done"},
	}
	if got := foldedEntries(entries); !reflect.DeepEqual(got, want) {
		t.Errorf("the record's blob decodes to\n%+v\nwant\n%+v", got, want)
	}
}

// TestOnceBoundsTheStoredToolArguments pins the cap the fold applies to a call's stored
// arguments. internal/run may not import internal/tui, so the bound is a MIRROR of the
// 1024-byte per-field cap internal/tui/wireargs.go applies (wireArgsFieldCap) rather than a
// shared helper — an unbounded ToolView.Args in a runner-written blob is exactly the regression
// this constant prevents, and the two spellings must not drift.
func TestOnceBoundsTheStoredToolArguments(t *testing.T) {
	t.Parallel()

	oversized := strings.Repeat("x", boundArgsFieldCap+1)

	registry := domain.NewToolRegistry()
	if err := registry.Register(notingTool{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	up := newUpstream(t, func(w http.ResponseWriter, req request) {
		if req.lastRoleIs(domain.RoleTool) {
			writeFinal(w, "noted")
			return
		}
		args, err := json.Marshal(map[string]string{"note": oversized})
		if err != nil {
			return // the handler runs on the server goroutine; a fixed literal never fails
		}
		writeToolCall(w, "call_1", "note_something", string(args))
	})

	store := session.NewStore(t.TempDir())
	spec := planSpec(up.url, "write a long note")
	spec.Config.Tools = registry
	spec.Store = store

	res, err := Once(context.Background(), spec)
	if err != nil {
		t.Fatalf("Once: %v", err)
	}

	rec, err := store.Load(res.SessionID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entries, err := session.DecodeTranscript(rec.Transcript)
	if err != nil {
		t.Fatalf("DecodeTranscript: %v", err)
	}

	var stored string
	for _, e := range entries {
		if e.Kind == session.EntryKindToolCall && e.Tool != nil {
			stored = string(e.Tool.Args)
		}
	}
	want := fmt.Sprintf(`{"note":"…[%d bytes]"}`, len(oversized))
	if stored != want {
		t.Errorf("stored arguments = %s, want %s — the over-long value is replaced by its own size",
			stored, want)
	}
}

// foldedEntry is the projection the scrollback assertions compare on: the facts the fold is
// responsible for, without the wire members it never writes. Comparing whole session.Entry values
// would pin members this item does not own and break on the next additive one.
type foldedEntry struct {
	kind  string
	text  string
	depth int
	spawn string
	name  string
	args  string
	done  bool
}

// foldedEntries projects a decoded blob onto that shape.
func foldedEntries(entries []session.Entry) []foldedEntry {
	out := make([]foldedEntry, 0, len(entries))
	for _, e := range entries {
		f := foldedEntry{kind: e.Kind, text: e.Text, depth: e.Depth, spawn: e.SpawnCallID, done: e.Done}
		if e.Tool != nil {
			f.name, f.args = e.Tool.Name, string(e.Tool.Args)
		}
		out = append(out, f)
	}
	return out
}

// TestOnceRecordsNoDelegateSpendWhenNothingWasDelegated is the zero-value guard: a Firing that
// delegated nothing leaves Meta.DelegateUsage at the zero Usage, which omitzero keeps out of the
// JSON entirely — so a record written by this build has exactly the shape one written before the
// fill did, and an older build reading it finds no key it cannot place.
func TestOnceRecordsNoDelegateSpendWhenNothingWasDelegated(t *testing.T) {
	t.Parallel()

	up := newUpstream(t, func(w http.ResponseWriter, _ request) {
		writeUsage(w, 600, 100, 700)
		writeFinal(w, "the build is green")
	})

	dir := t.TempDir()
	store := session.NewStore(dir)
	spec := planSpec(up.url, "check the build")
	spec.Store = store

	res, err := Once(context.Background(), spec)
	if err != nil {
		t.Fatalf("Once: %v", err)
	}

	rec, err := store.Load(res.SessionID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantOwn := session.Usage{Calls: 1, PromptTokens: 600, CompletionTokens: 100, TotalTokens: 700}
	if rec.Meta.Usage != wantOwn {
		t.Errorf("Meta.Usage = %+v, want %+v", rec.Meta.Usage, wantOwn)
	}
	if (rec.Meta.DelegateUsage != session.Usage{}) {
		t.Errorf("Meta.DelegateUsage = %+v, want the zero Usage — nothing was delegated",
			rec.Meta.DelegateUsage)
	}

	raw, err := os.ReadFile(filepath.Join(dir, res.SessionID+".json"))
	if err != nil {
		t.Fatalf("read the record: %v", err)
	}
	if strings.Contains(string(raw), "delegateUsage") {
		t.Errorf("the record carries a delegateUsage key at zero; omitzero must keep the "+
			"old JSON shape:\n%s", raw)
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

// The two derived title forms get one test EACH, deliberately, and never a shared table. They are
// different paths with different callers — a Schedule's Firing, and any other run.Once — and each
// states its own time zone at its own site in Spec.title. One test per site is what proves that:
// change the zone of one site and exactly one of these two goes red. A single case covering both
// would let one path's spelling ride along on the other's, which is the coupling they replaced.

// TestSpecTitleScheduleFormSpellsTheLocalWallClock pins the SCHEDULE path's zone: "<name> — HH:MM"
// is what a human reads a scheduled run by, in its firing block and in /sessions, so it is spelled
// against the wall clock they set the schedule by whatever zone the instant handed in carries. Now
// is an injectable seam and a Driver's clock may well be UTC-located, which is exactly this case.
// The record's own stamps are unaffected and stay UTC.
func TestSpecTitleScheduleFormSpellsTheLocalWallClock(t *testing.T) {
	t.Parallel()

	local, away := titleZoneFixture(t)
	spec := Spec{ScheduleName: "Nightly", Prompt: "ignored"}
	want := "Nightly — " + local.Format("15:04")
	if got := spec.title(away); got != want {
		t.Errorf("title = %q, want the local spelling %q", got, want)
	}
}

// TestSpecTitleDatedFallbackSpellsTheLocalWallClock pins the GENERIC Once path's zone, which is a
// separate choice from the schedule form's above and reached by every caller whose prompt yields no
// title: the "Session <date>" label answers "which day did I run this?" for whoever browses their
// own sessions, so it is their day, not the day the caller's clock happened to be located in.
func TestSpecTitleDatedFallbackSpellsTheLocalWallClock(t *testing.T) {
	t.Parallel()

	local, away := titleZoneFixture(t)
	spec := Spec{Prompt: "```go\nfunc main() {}"} // a code fence has no useful title
	want := "Session " + local.Format("2006-01-02")
	if got := spec.title(away); got != want {
		t.Errorf("title = %q, want the local spelling %q", got, want)
	}
}

// titleZoneFixture returns one instant twice: as local's own wall clock, and expressed in a zone 90
// minutes ahead of wherever the test runs. Building the away zone from LOCAL's own offset is what
// makes both title tests assert the same thing on any machine's TZ, and 23:00 local puts the two
// spellings on different dates as well as different minutes, so the date form is pinned as hard as
// the clock form.
func titleZoneFixture(t *testing.T) (local, away time.Time) {
	t.Helper()

	local = time.Date(2026, 8, 4, 23, 0, 0, 0, time.Local)
	_, offset := local.Zone()
	away = local.In(time.FixedZone("away", offset+90*60))
	if away.Format("2006-01-02 15:04") == local.Format("2006-01-02 15:04") {
		t.Fatalf("the fixture no longer distinguishes the zones: away %s, local %s", away, local)
	}
	return local, away
}

// ---------------------------------------------------------------------------

// TestOnceResolvesTheFiringsFileRefs is the Driver-parity claim of ADR 0031: the @file grammar
// a session's message carries is read from a Firing's prompt too (internal/refs), so an
// unattended run reaches the very same file context the loop builds for a chat message.
func TestOnceResolvesTheFiringsFileRefs(t *testing.T) {
	t.Parallel()

	const firstLine = "module github.com/airiclenz/apogee"
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "go.mod"), []byte(firstLine+"\n\ngo 1.25\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	up := newUpstream(t, alwaysFinal("it declares the module"))
	spec := planSpec(up.url, "summarise @go.mod")
	spec.Config.WorkspaceDir = ws

	if _, err := Once(context.Background(), spec); err != nil {
		t.Fatalf("Once: %v", err)
	}

	reqs := up.requests()
	if len(reqs) != 1 {
		t.Fatalf("the Upstream saw %d requests, want 1", len(reqs))
	}
	got := reqs[0].Texts[len(reqs[0].Texts)-1]
	if !strings.Contains(got, "Referenced file `go.mod`:\n") {
		t.Errorf("the firing's user message carries no file-context block for @go.mod:\n%s", got)
	}
	if !strings.Contains(got, firstLine) {
		t.Errorf("the firing's user message does not carry go.mod's first line %q:\n%s", firstLine, got)
	}
	if !strings.HasSuffix(got, "summarise @go.mod") {
		t.Errorf("the prompt itself did not survive the prepended file context:\n%s", got)
	}
}

// TestOnceSkipsAMissingFileRefWithoutNotice pins what a Firing CANNOT do: the loop reports an
// unresolvable @ref as an ErrorEvent, and a Firing has no event sink to carry one (headless and
// the daemon both leave Config.Events nil). So the run simply completes, nothing is injected,
// and the prompt travels verbatim — the skip leaves no notice behind.
func TestOnceSkipsAMissingFileRefWithoutNotice(t *testing.T) {
	t.Parallel()

	const prompt = `summarise @"no such.md"`

	up := newUpstream(t, alwaysFinal("there was nothing to read"))
	spec := planSpec(up.url, prompt)
	spec.Config.WorkspaceDir = t.TempDir()

	res, err := Once(context.Background(), spec)
	if err != nil {
		t.Fatalf("Once: %v", err)
	}
	if res.Err != nil {
		t.Errorf("Result.Err = %v, want nil; a missing ref is skipped, never fatal", res.Err)
	}

	reqs := up.requests()
	if len(reqs) != 1 {
		t.Fatalf("the Upstream saw %d requests, want 1", len(reqs))
	}
	if got := reqs[0].Texts[len(reqs[0].Texts)-1]; got != prompt {
		t.Errorf("the firing's user message = %q, want the prompt verbatim %q", got, prompt)
	}
}

// ---------------------------------------------------------------------------

// stubSkills is a domain.SkillResolver over a literal catalog — the run-layer stand-in for the
// internal/skills provider a host injects. It answers ResolveSkills the way the real catalog
// does: the known IDs in the requested order, unknown ones skipped.
type stubSkills struct {
	byID map[string]domain.ResolvedSkill
}

// newStubSkills builds a resolver holding one skill per given entry.
func newStubSkills(skills ...domain.ResolvedSkill) *stubSkills {
	byID := make(map[string]domain.ResolvedSkill, len(skills))
	for _, s := range skills {
		byID[s.ID] = s
	}
	return &stubSkills{byID: byID}
}

// ResolveSkills returns the known skills among ids, in the order asked.
func (s *stubSkills) ResolveSkills(ids []string) []domain.ResolvedSkill {
	var out []domain.ResolvedSkill
	for _, id := range ids {
		if sk, ok := s.byID[id]; ok {
			out = append(out, sk)
		}
	}
	return out
}

// TestOnceResolvesTheFiringsSkillRefs is the Driver-parity claim of ADR 0031 for the OTHER half
// of the prompt mini-language: the /skill grammar a session's message carries is read from a
// Firing's prompt too, so a headless or scheduled run reaches the same injected skill body a
// chat message does. It asserts the ANNOUNCED surface — the exact block spelling the loop emits
// — because that block is what the model reads.
func TestOnceResolvesTheFiringsSkillRefs(t *testing.T) {
	t.Parallel()

	const prompt = "/code-audit internal/tui"
	skill := domain.ResolvedSkill{
		ID:          "code-audit",
		DisplayName: "Code Audit",
		Body:        "Review correctness first, then report.",
	}

	up := newUpstream(t, alwaysFinal("audited"))
	spec := planSpec(up.url, prompt)
	spec.Config.WorkspaceDir = t.TempDir()
	spec.Config.Skills = newStubSkills(skill)

	if _, err := Once(context.Background(), spec); err != nil {
		t.Fatalf("Once: %v", err)
	}

	reqs := up.requests()
	if len(reqs) != 1 {
		t.Fatalf("the Upstream saw %d requests, want 1", len(reqs))
	}
	got := reqs[0].Texts[len(reqs[0].Texts)-1]
	want := "<skill: " + skill.DisplayName + ">\n" + skill.Body + "\n</skill>\n\n" + prompt
	if got != want {
		t.Errorf("the firing's user message =\n%q\nwant\n%q", got, want)
	}
}

// TestOnceLeavesAnUnknownSkillTokenAsProse pins the grammar's own gate: only a token the wired
// catalog confirms is a reference, so a slash word that names no skill — a path, a typo — travels
// to the model untouched and attaches nothing.
func TestOnceLeavesAnUnknownSkillTokenAsProse(t *testing.T) {
	t.Parallel()

	const prompt = "/code-adit internal/tui"

	up := newUpstream(t, alwaysFinal("nothing to audit"))
	spec := planSpec(up.url, prompt)
	spec.Config.WorkspaceDir = t.TempDir()
	spec.Config.Skills = newStubSkills(domain.ResolvedSkill{
		ID:          "code-audit",
		DisplayName: "Code Audit",
		Body:        "Review correctness first, then report.",
	})

	if _, err := Once(context.Background(), spec); err != nil {
		t.Fatalf("Once: %v", err)
	}

	reqs := up.requests()
	if len(reqs) != 1 {
		t.Fatalf("the Upstream saw %d requests, want 1", len(reqs))
	}
	if got := reqs[0].Texts[len(reqs[0].Texts)-1]; got != prompt {
		t.Errorf("the firing's user message = %q, want the prompt verbatim %q", got, prompt)
	}
}

// TestOnceWithoutASkillCatalogSendsTheTokenVerbatim pins the nil-resolver case: with no catalog
// wired there is nothing to test a token against, so no "/" word is a reference and the prompt
// is byte-identical to what it was before this seam existed.
func TestOnceWithoutASkillCatalogSendsTheTokenVerbatim(t *testing.T) {
	t.Parallel()

	const prompt = "/code-audit internal/tui"

	up := newUpstream(t, alwaysFinal("audited"))
	spec := planSpec(up.url, prompt)
	spec.Config.WorkspaceDir = t.TempDir()

	if _, err := Once(context.Background(), spec); err != nil {
		t.Fatalf("Once: %v", err)
	}

	reqs := up.requests()
	if len(reqs) != 1 {
		t.Fatalf("the Upstream saw %d requests, want 1", len(reqs))
	}
	if got := reqs[0].Texts[len(reqs[0].Texts)-1]; got != prompt {
		t.Errorf("the firing's user message = %q, want the prompt verbatim %q", got, prompt)
	}
}
