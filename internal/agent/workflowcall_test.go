package agent

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/workflow"
)

// The blocking fan_out call (ADR 0087 D1, ADR 0088 D3): each test drives a parent Agent whose reply
// calls fan_out, over workflowResponder, which answers the parent and every item child from a
// script of its own — keyed by a word its last user message contains — so the scripts hold
// however the children interleave.

// fanOutTask is the brief every test's fan_out runs: {item} makes each child's task name its item,
// which is the key its script is routed by.
const fanOutTask = "check {item} carefully and report"

// workflowResponder answers each request from the next script of the first route whose key the
// request's last user message contains. A request that offers no tools is a summary call (a fold):
// it is counted and blocks until its context ends. menus keeps the tool names each route's first
// request offered.
type workflowResponder struct {
	mu     sync.Mutex
	routes []workflowRoute
	folds  int
	asked  []string
	menus  map[string][]string
}

// workflowRoute is one key's queue of scripted replies.
type workflowRoute struct {
	key     string
	scripts []turnScript
}

// route appends one scripted reply for the agent whose last user message contains key. Routes are
// matched in the order they were first registered, so a key that contains another goes first.
func (r *workflowResponder) route(key string, gate func(context.Context), deltas []provider.Delta) *workflowResponder {
	for i := range r.routes {
		if r.routes[i].key == key {
			r.routes[i].scripts = append(r.routes[i].scripts, turnScript{gate: gate, deltas: deltas})
			return r
		}
	}
	r.routes = append(r.routes, workflowRoute{key: key, scripts: []turnScript{{gate: gate, deltas: deltas}}})
	return r
}

func (r *workflowResponder) Stream(ctx context.Context, req provider.Request) iter.Seq[provider.Delta] {
	if len(req.Tools) == 0 {
		r.mu.Lock()
		r.folds++
		r.mu.Unlock()
		return func(yield func(provider.Delta) bool) {
			<-ctx.Done()
			yield(provider.Delta{Kind: provider.DeltaError, Err: ctx.Err().Error()})
		}
	}
	asker := lastUserText(req)
	r.mu.Lock()
	script := turnScript{deltas: []provider.Delta{{Kind: provider.DeltaError, Err: "workflowResponder: no script for " + asker}}}
	for i := range r.routes {
		route := &r.routes[i]
		if !strings.Contains(asker, route.key) || len(route.scripts) == 0 {
			continue
		}
		script, route.scripts = route.scripts[0], route.scripts[1:]
		r.asked = append(r.asked, route.key)
		if _, seen := r.menus[route.key]; !seen {
			if r.menus == nil {
				r.menus = map[string][]string{}
			}
			for _, spec := range req.Tools {
				r.menus[route.key] = append(r.menus[route.key], spec.Name)
			}
		}
		break
	}
	r.mu.Unlock()

	return func(yield func(provider.Delta) bool) {
		if script.gate != nil {
			script.gate(ctx)
		}
		for _, d := range script.deltas {
			if !yield(d) {
				return
			}
		}
	}
}

// askedCount is how many requests were routed to key.
func (r *workflowResponder) askedCount(key string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, asked := range r.asked {
		if asked == key {
			count++
		}
	}
	return count
}

// foldCount is how many summary calls reached the upstream.
func (r *workflowResponder) foldCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.folds
}

// workflowConfig is a parent Config offering fan_out and one read-only tool (plus extra), with a
// scratch dir and a workspace of its own.
func workflowConfig(t *testing.T, sink domain.EventSink, extra ...domain.Tool) domain.Config {
	t.Helper()
	cfg := baseConfig(sink)
	cfg.Mode = domain.ModeAskBefore
	reg := domain.NewToolRegistry()
	_ = reg.Register(tools.NewFanOut())
	_ = reg.Register(fakeTool{name: "read_thing", readOnly: true, result: "package main"})
	for _, tool := range extra {
		_ = reg.Register(tool)
	}
	cfg.Tools = reg
	cfg.ScratchDir = t.TempDir()
	cfg.WorkspaceDir = t.TempDir()
	return cfg
}

// fanOutArgsJSON is a fan_out call's arguments running fanOutTask over items, each child returning
// an int count.
func fanOutArgsJSON(items ...string) string {
	b, _ := json.Marshal(map[string]any{"task": fanOutTask, "over": items, "returns": map[string]string{"count": "int"}})
	return string(b)
}

// finishScript is a child reply calling finish ok with summary and a count of 1.
func finishScript(id, summary string) []provider.Delta {
	return toolCallScript(id, tools.FinishToolName, `{"status":"ok","summary":"`+summary+`","count":1}`)
}

// cancelledScript is a reply that ends on the stream error a cancelled request surfaces.
func cancelledScript() []provider.Delta {
	return []provider.Delta{{Kind: provider.DeltaError, Err: context.Canceled.Error()}}
}

// cancelWith is a gate that cancels the run with cause and waits for the cancel to land.
func cancelWith(cancel context.CancelCauseFunc, cause error) func(context.Context) {
	return func(ctx context.Context) {
		cancel(cause)
		<-ctx.Done()
	}
}

// runWorkflowParent builds a parent on cfg over up, submits text and runs it under ctx.
func runWorkflowParent(t *testing.T, ctx context.Context, cfg domain.Config, up provider.Responder, text string) (*Agent, domain.StepResult) {
	t.Helper()
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	return a, runSubmitted(t, ctx, a, text)
}

// runSubmitted submits text to a and runs it to its boundary under ctx.
func runSubmitted(t *testing.T, ctx context.Context, a *Agent, text string) domain.StepResult {
	t.Helper()
	if err := a.Submit(domain.UserInput{Text: text}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

// callResult is the depth-0 tool result that answered callID.
func callResult(t *testing.T, events []domain.Event, callID string) domain.ToolResult {
	t.Helper()
	for _, e := range events {
		if re, ok := e.(domain.ToolResultEvent); ok && re.Depth == 0 && re.Result.CallID == callID {
			return re.Result
		}
	}
	t.Fatalf("no result answered %s", callID)
	return domain.ToolResult{}
}

// workflowFolders lists the workflow folders under the scratch dir.
func workflowFolders(t *testing.T, scratch string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(scratch, "workflows"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read workflows dir: %v", err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestWorkflowCall_ThreeItemsAnswerThreeLines(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	cfg := workflowConfig(t, sink)
	up := (&workflowResponder{}).
		route("please fan out", nil, toolCallScript("fo1", tools.FanOutToolName, fanOutArgsJSON("alpha", "beta", "gamma"))).
		route("please fan out", nil, contentScript("all done")).
		route("check alpha", nil, finishScript("f1", "alpha is fine")).
		route("check beta", nil, finishScript("f2", "beta is fine")).
		route("check gamma", nil, finishScript("f3", "gamma is fine"))

	_, res := runWorkflowParent(t, context.Background(), cfg, up, "please fan out")

	if res.Status == domain.StatusCancelled {
		t.Fatalf("parent result = %+v, want a completed Exchange", res)
	}
	got := callResult(t, sink.events, "fo1")
	if got.IsError {
		t.Fatalf("fan_out result is an error: %q", got.Content)
	}
	for _, want := range []string{
		"#1 alpha — ok — alpha is fine count=1",
		"#2 beta — ok — beta is fine count=1",
		"#3 gamma — ok — gamma is fine count=1",
		"items 3 · ok 3 · partial 0 · blocked 0",
	} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("fan_out result lacks %q:\n%s", want, got.Content)
		}
	}
	if folders := workflowFolders(t, cfg.ScratchDir); len(folders) != 1 {
		t.Errorf("workflow folders = %v, want one under <scratch>/workflows", folders)
	}
}

func TestWorkflowCall_AChildsMenuDoesNotOfferFanOut(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	cfg := workflowConfig(t, sink)
	up := (&workflowResponder{}).
		route("please fan out", nil, toolCallScript("fo1", tools.FanOutToolName, fanOutArgsJSON("alpha"))).
		route("please fan out", nil, contentScript("all done")).
		route("check alpha", nil, finishScript("f1", "alpha is fine"))

	runWorkflowParent(t, context.Background(), cfg, up, "please fan out")

	menu := up.menus["check alpha"]
	if slices.Contains(menu, tools.FanOutToolName) || !slices.Contains(menu, tools.FinishToolName) {
		t.Errorf("item child's menu = %v, want finish and no fan_out", menu)
	}
}

func TestWorkflowCall_AnInvalidPlanIsAFixableError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args string
		want string
	}{
		{"no items", `{"task":"check {item} carefully and report"}`, "- over: a fanout stage needs items"},
		{"verify on an undeclared field", `{"task":"check {item} carefully and report","over":["a"],"verify":{"when":"findings > 0"}}`, "- verify.when: "},
		{"over of the wrong shape", `{"task":"check {item} carefully and report","over":[1,2]}`, "over must be an array of strings"},
		{"a misspelled source", `{"task":"check {item} carefully and report","over":{"glob":"*.go"}}`, `unknown field "glob"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sink := &recordingSink{}
			cfg := workflowConfig(t, sink)
			up := (&workflowResponder{}).
				route("please fan out", nil, toolCallScript("fo1", tools.FanOutToolName, tc.args)).
				route("please fan out", nil, contentScript("I will fix it"))

			runWorkflowParent(t, context.Background(), cfg, up, "please fan out")

			got := callResult(t, sink.events, "fo1")
			if !got.IsError || !strings.Contains(got.Content, tc.want) {
				t.Errorf("fan_out result = %+v, want an error containing %q", got, tc.want)
			}
			if folders := workflowFolders(t, cfg.ScratchDir); len(folders) != 0 {
				t.Errorf("workflow folders = %v, want none for a plan that did not run", folders)
			}
		})
	}
}

func TestWorkflowCall_AnEmptyScratchDirRefusesTheCall(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	cfg := workflowConfig(t, sink)
	cfg.ScratchDir = ""
	up := (&workflowResponder{}).
		route("please fan out", nil, toolCallScript("fo1", tools.FanOutToolName, fanOutArgsJSON("alpha"))).
		route("please fan out", nil, contentScript("no luck")).
		route("check alpha", nil, finishScript("f1", "alpha is fine"))

	runWorkflowParent(t, context.Background(), cfg, up, "please fan out")

	if got := callResult(t, sink.events, "fo1"); !got.IsError || got.Content != fanOutNoScratch {
		t.Errorf("fan_out result = %+v, want the error %q", got, fanOutNoScratch)
	}
	if n := up.askedCount("check alpha"); n != 0 {
		t.Errorf("item child asked %d times, want no child for a refused call", n)
	}
}

func TestWorkflowCall_ThreeItemsOnACapThreeServerRunAtOnce(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	cfg := workflowConfig(t, sink)
	cfg.ParallelAgents = 3
	probe := newConcurrencyProbe(3, 3*time.Second)
	up := (&workflowResponder{}).
		route("please fan out", nil, toolCallScript("fo1", tools.FanOutToolName, fanOutArgsJSON("alpha", "beta", "gamma"))).
		route("please fan out", nil, contentScript("all done")).
		route("check alpha", probe.enter, finishScript("f1", "alpha is fine")).
		route("check beta", probe.enter, finishScript("f2", "beta is fine")).
		route("check gamma", probe.enter, finishScript("f3", "gamma is fine"))

	runWorkflowParent(t, context.Background(), cfg, up, "please fan out")

	if peak := probe.peakInFlight(); peak != 3 {
		t.Errorf("peak children in flight = %d, want 3 on a cap-3 server", peak)
	}
}

// TestWorkflowCall_ACancelKeepsFinishedItemsAndAnswersTheCall pins ADR 0088 D3 at the dispatch:
// the cancel lands while the second item runs, the first item's receipt stays, and the call is
// answered with the stopped count and the listing so far — not the leaf's while-it-ran text — while
// the call after it in the same reply is answered not-run.
func TestWorkflowCall_ACancelKeepsFinishedItemsAndAnswersTheCall(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	cfg := workflowConfig(t, sink)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	reply := append(
		toolCallScript("fo1", tools.FanOutToolName, fanOutArgsJSON("alpha", "beta", "gamma"))[:1],
		toolCallScript("r1", "read_thing", `{}`)...,
	)
	up := (&workflowResponder{}).
		route("please fan out", nil, reply).
		route("check alpha", nil, finishScript("f1", "alpha is fine")).
		route("check beta", cancelWith(cancel, nil), cancelledScript())

	a, res := runWorkflowParent(t, ctx, cfg, up, "please fan out")

	if res.Status != domain.StatusCancelled {
		t.Fatalf("parent result = %+v, want a cancel", res)
	}
	got := callResult(t, sink.events, "fo1")
	if got.IsError || !strings.HasPrefix(got.Content, "stopped by the user: 1 of 3 done") ||
		!strings.Contains(got.Content, "#1 alpha — ok — alpha is fine") || strings.Contains(got.Content, cancelledWhileRunningContent) {
		t.Errorf("fan_out result = %+v, want the non-error stopped answer keeping alpha", got)
	}
	listing := got.Content[strings.LastIndex(got.Content, "items: ")+len("items: "):]
	if _, err := os.Stat(listing); err != nil {
		t.Errorf("the listing so far %q: %v", listing, err)
	}
	if next := callResult(t, sink.events, "r1"); next.Content != notRunCancelledContent {
		t.Errorf("the next call's result = %+v, want %q", next, notRunCancelledContent)
	}
	kept := slices.ContainsFunc(a.conv.Messages(), func(m domain.Message) bool {
		return m.ToolCallID == "fo1" && m.Content == got.Content
	})
	if !kept {
		t.Error("the history does not keep the stopped answer for fo1")
	}
}

// TestWorkflowCall_TheSameCallAgainResumesTheStoredWorkflow pins ADR 0087 D4 through the call: a
// cancelled fan_out issued again with the same arguments runs only the item that did not finish.
func TestWorkflowCall_TheSameCallAgainResumesTheStoredWorkflow(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	cfg := workflowConfig(t, sink)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	args := fanOutArgsJSON("alpha", "beta")
	up := (&workflowResponder{}).
		route("please fan out", nil, toolCallScript("fo1", tools.FanOutToolName, args)).
		route("resume the work", nil, toolCallScript("fo2", tools.FanOutToolName, args)).
		route("resume the work", nil, contentScript("all done")).
		route("check alpha", nil, finishScript("f1", "alpha is fine")).
		route("check beta", cancelWith(cancel, nil), cancelledScript()).
		route("check beta", nil, finishScript("f2", "beta is fine"))
	a, _ := runWorkflowParent(t, ctx, cfg, up, "please fan out")
	a.SettleExchange()

	runSubmitted(t, context.Background(), a, "resume the work")

	got := callResult(t, sink.events, "fo2")
	if got.IsError || !strings.Contains(got.Content, "#2 beta — ok — beta is fine") ||
		!strings.Contains(got.Content, "resumed 1") {
		t.Errorf("resumed fan_out result = %+v, want beta run and alpha resumed", got)
	}
	if n := up.askedCount("check alpha"); n != 1 {
		t.Errorf("alpha's child asked %d times, want 1 — the resume skips a finished item", n)
	}
	if folders := workflowFolders(t, cfg.ScratchDir); len(folders) != 1 {
		t.Errorf("workflow folders = %v, want the one folder resumed", folders)
	}
}

// deepWorkflowCancel runs a fan_out at delegate-max-depth 2 whose one item child delegates to a
// grandchild; the grandchild's second request cancels the run with cause. It returns the fan_out's
// result, the responder, and how long the run took.
func deepWorkflowCancel(t *testing.T, cause error, foldBound time.Duration) (domain.ToolResult, *workflowResponder, time.Duration) {
	t.Helper()
	sink := &recordingSink{}
	cfg := workflowConfig(t, sink, tools.NewSubAgent())
	cfg.Delegation.MaxDepth = 2
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	delegate, _ := json.Marshal(tools.SubAgentArgs{Task: "dig into alpha", Name: "Digger"})
	up := (&workflowResponder{}).
		route("dig into alpha", nil, toolCallScript("g1", "read_thing", `{}`)).
		route("dig into alpha", cancelWith(cancel, cause), cancelledScript()).
		route("please fan out", nil, toolCallScript("fo1", tools.FanOutToolName, fanOutArgsJSON("alpha"))).
		route("check alpha", nil, toolCallScript("s1", tools.SubAgentToolName, string(delegate)))
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.cancelFoldBound = foldBound
	started := time.Now()

	res := runSubmitted(t, ctx, a, "please fan out")

	if res.Status != domain.StatusCancelled {
		t.Fatalf("parent result = %+v, want a cancel", res)
	}
	return callResult(t, sink.events, "fo1"), up, time.Since(started)
}

func TestWorkflowCall_AQuitCauseCancelSkipsEveryFold(t *testing.T) {
	t.Parallel()

	got, up, _ := deepWorkflowCancel(t, domain.ErrShuttingDown, 0)

	if n := up.foldCount(); n != 0 {
		t.Errorf("the upstream saw %d fold requests, want none on a shutdown", n)
	}
	if !strings.HasPrefix(got.Content, "stopped by the user: 0 of 1 done") {
		t.Errorf("fan_out result = %+v, want the stopped answer", got)
	}
}

func TestWorkflowCall_AChildsEscFoldStopsAtItsBound(t *testing.T) {
	t.Parallel()

	got, up, elapsed := deepWorkflowCancel(t, nil, 20*time.Millisecond)

	if n := up.foldCount(); n != 1 {
		t.Errorf("the upstream saw %d fold requests, want the grandchild's one", n)
	}
	if elapsed > 10*time.Second {
		t.Errorf("the run took %v, want the fold cut at its bound", elapsed)
	}
	if !strings.HasPrefix(got.Content, "stopped by the user: 0 of 1 done") {
		t.Errorf("fan_out result = %+v, want the stopped answer", got)
	}
}

func TestWorkflowCall_FanOutIsWithheldAndRefusedAtTheDepthBound(t *testing.T) {
	t.Parallel()

	cfg := workflowConfig(t, &recordingSink{})
	a, err := newAgent(cfg, scriptedResponder(t))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	call := domain.ToolCall{ID: "fo1", Tool: tools.FanOutToolName}

	if _, offered := a.defaultSubAgentTools().Lookup(tools.FanOutToolName); offered {
		t.Error("a child at the depth bound is offered fan_out")
	}
	if got := resolve(resolutionInput{call: call, atDepthBound: true, maxDepth: 1}); got.kind != resolveRefuse ||
		got.reason != depthLimitReason(1) {
		t.Errorf("resolve at the bound = %+v, want the depth refusal", got)
	}
	if got := resolve(resolutionInput{call: call, maxDepth: 1}); got.kind != resolveWorkflow {
		t.Errorf("resolve below the bound = %v, want workflow", got.kind)
	}
}

func TestWorkflowCall_AChildBelowTheBoundGetsFanOutWithoutRunOn(t *testing.T) {
	t.Parallel()

	cfg := workflowConfig(t, &recordingSink{})
	reg := domain.NewToolRegistry()
	_ = reg.Register(tools.NewFanOutWith(tools.FanOutOptions{SeatChoice: true, Background: true}))
	cfg.Tools = reg
	cfg.Delegation.MaxDepth = 2
	a, err := newAgent(cfg, scriptedResponder(t))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	tool, ok := a.defaultSubAgentTools().Lookup(tools.FanOutToolName)

	fanOut, isFanOut := tool.(*tools.FanOut)
	if !ok || !isFanOut || fanOut.OffersSeatChoice() || !fanOut.OffersBackground() {
		t.Errorf("child's fan_out = %#v, want the variant without run_on that keeps background", tool)
	}
}

func TestWorkflowContextLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		working int
		target  *DelegationTarget
		want    int
	}{
		{"an unrouted session uses its own window", 0, nil, 32000},
		{"an unrouted session uses its own working bound", 20000, nil, 20000},
		{"a routed one uses the target's window", 0, gruntTarget("http://grunt", 2), 32768},
		{"a target naming no window keeps the session's", 0, &DelegationTarget{Endpoint: "http://grunt", Model: "m"}, 32000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := workflowConfig(t, &recordingSink{})
			cfg.Context.MaxContextTokens = 32000
			cfg.Context.WorkingWindow = tc.working
			a, err := newAgent(cfg, scriptedResponder(t))
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			a.SetDelegationTarget(tc.target)

			if got := a.workflowContextLimit(); got != tc.want {
				t.Errorf("workflowContextLimit() = %d, want %d", got, tc.want)
			}
		})
	}
}

// workflowPhaseEvents returns the WorkflowPhaseEvents among events, in emission order.
func workflowPhaseEvents(events []domain.Event) []domain.WorkflowPhaseEvent {
	var phases []domain.WorkflowPhaseEvent
	for _, event := range events {
		if phase, ok := event.(domain.WorkflowPhaseEvent); ok {
			phases = append(phases, phase)
		}
	}
	return phases
}

// phaseNames is the phase of each event, for a failure message.
func phaseNames(events []domain.WorkflowPhaseEvent) []domain.WorkflowPhase {
	names := make([]domain.WorkflowPhase, len(events))
	for index, event := range events {
		names[index] = event.Phase
	}
	return names
}

// TestWorkflowCall_EmitsItsPhases pins the events a fan_out's Workflow reports: started, the item
// stage starting, one item finished per item with its receipt, then finished — every one naming
// the one Workflow and stamped with the parent's own identity.
func TestWorkflowCall_EmitsItsPhases(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	cfg := workflowConfig(t, sink)
	up := (&workflowResponder{}).
		route("please fan out", nil, toolCallScript("fo1", tools.FanOutToolName, fanOutArgsJSON("alpha", "beta"))).
		route("please fan out", nil, contentScript("all done")).
		route("check alpha", nil, finishScript("f1", "alpha is fine")).
		route("check beta", nil, finishScript("f2", "beta is fine"))

	runWorkflowParent(t, context.Background(), cfg, up, "please fan out")

	phases := workflowPhaseEvents(sink.events)
	want := []domain.WorkflowPhase{
		domain.WorkflowStarted, domain.WorkflowStageStarted,
		domain.WorkflowItemFinished, domain.WorkflowItemFinished, domain.WorkflowFinished,
	}
	if !slices.Equal(phaseNames(phases), want) {
		t.Fatalf("workflow phases = %v, want %v", phaseNames(phases), want)
	}
	id := phases[0].Workflow
	if id == "" || !slices.Contains(workflowFolders(t, cfg.ScratchDir), id) {
		t.Errorf("started names workflow %q, want the id of its folder %v", id, workflowFolders(t, cfg.ScratchDir))
	}
	summaries := map[string]string{}
	for _, phase := range phases {
		if phase.Workflow != id || phase.Name != "check {item} carefully and report" || phase.Depth != 0 || phase.RunID != "" {
			t.Errorf("phase %s = %+v, want workflow %q, the fan_out's name and the parent's identity", phase.Phase, phase, id)
		}
		if phase.Phase == domain.WorkflowItemFinished {
			if phase.Stage != fanOutStageName || phase.Resumed || phase.Receipt.Status != "ok" || phase.Receipt.Fields["count"] != "1" {
				t.Errorf("item_finished = %+v, want an ok items receipt with count=1, not resumed", phase)
			}
			summaries[phase.Item] = phase.Receipt.Summary
		}
	}
	if summaries["alpha"] != "alpha is fine" || summaries["beta"] != "beta is fine" {
		t.Errorf("item summaries = %v, want each item's own", summaries)
	}
}

// TestWorkflowCall_ACancelEndsItsPhasesStopped pins a cancelled fan_out's events: the item that
// finished is reported, and the Workflow ends stopped rather than finished.
func TestWorkflowCall_ACancelEndsItsPhasesStopped(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	cfg := workflowConfig(t, sink)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	up := (&workflowResponder{}).
		route("please fan out", nil, toolCallScript("fo1", tools.FanOutToolName, fanOutArgsJSON("alpha", "beta"))).
		route("check alpha", nil, finishScript("f1", "alpha is fine")).
		route("check beta", cancelWith(cancel, nil), cancelledScript())

	runWorkflowParent(t, ctx, cfg, up, "please fan out")

	phases := workflowPhaseEvents(sink.events)
	want := []domain.WorkflowPhase{
		domain.WorkflowStarted, domain.WorkflowStageStarted, domain.WorkflowItemFinished, domain.WorkflowStopped,
	}
	if !slices.Equal(phaseNames(phases), want) {
		t.Fatalf("workflow phases = %v, want %v", phaseNames(phases), want)
	}
	if phases[2].Item != "alpha" {
		t.Errorf("item_finished = %+v, want alpha's", phases[2])
	}
}

// fixedAsker answers every question with answer.
type fixedAsker struct{ answer string }

func (f fixedAsker) Ask(context.Context, workflow.Question) (string, error) { return f.answer, nil }

// TestWorkflowObserver_ReportsAWaitingQuestionAndAFailure pins the two phases a fan_out cannot
// reach yet: a question put through the Runner's Asker is reported as waiting before it is asked,
// and a run that fails after its Workflow started ends failed with the cause — while a run that
// fails before it has an id reports nothing at all.
func TestWorkflowObserver_ReportsAWaitingQuestionAndAFailure(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	a, err := newAgent(workflowConfig(t, sink), &workflowResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runner := &workflow.Runner{Asker: fixedAsker{answer: "yes"}}
	observer := a.observeWorkflow(runner, 3, "audit")

	answer, err := runner.Asker.Ask(context.Background(), workflow.Question{Workflow: "wf-1", Stage: "confirm", Text: "Go on?"})
	observer.end(workflow.Result{ID: "wf-1"}, errors.New("disk full"))

	if answer != "yes" || err != nil {
		t.Errorf("Ask = %q, %v; want the wrapped Asker's answer", answer, err)
	}
	phases := workflowPhaseEvents(sink.events)
	want := []domain.WorkflowPhase{domain.WorkflowStarted, domain.WorkflowWaiting, domain.WorkflowFailed}
	if !slices.Equal(phaseNames(phases), want) {
		t.Fatalf("workflow phases = %v, want %v", phaseNames(phases), want)
	}
	if phases[1].Stage != "confirm" || phases[1].Detail != "Go on?" || phases[2].Detail != "disk full" {
		t.Errorf("waiting = %+v, failed = %+v; want the question and the cause", phases[1], phases[2])
	}
	for _, phase := range phases {
		if phase.Workflow != "wf-1" || phase.Name != "audit" || phase.Turn != 3 {
			t.Errorf("phase %s = %+v, want workflow wf-1, name audit, turn 3", phase.Phase, phase)
		}
	}

	quiet := &recordingSink{}
	b, err := newAgent(workflowConfig(t, quiet), &workflowResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	b.observeWorkflow(&workflow.Runner{}, 1, "audit").end(workflow.Result{}, errors.New("invalid plan"))
	if got := workflowPhaseEvents(quiet.events); len(got) != 0 {
		t.Errorf("a run that failed before it had an id emitted %v, want nothing", phaseNames(got))
	}
}
