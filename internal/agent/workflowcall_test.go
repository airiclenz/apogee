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

// TestWorkflowCall_PlanItemChildrenInheritPlan pins that a Plan parent's fan_out (offered with its
// scratch dir set, planOffersDelegation) spawns item children that run in Plan too, as a Plan
// sub_agent's do (ADR 0013): a child's write_file aimed into the workspace is refused and writes
// nothing, while the item still finishes on its receipt.
func TestWorkflowCall_PlanItemChildrenInheritPlan(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	cfg := workflowConfig(t, sink)
	cfg.Mode = domain.ModePlan
	_ = cfg.Tools.Register(tools.NewWriteFile(cfg.WorkspaceDir))
	target := filepath.Join(cfg.WorkspaceDir, "out.txt")
	writeArgs, _ := json.Marshal(map[string]string{"path": target, "content": "written"})
	up := (&workflowResponder{}).
		route("please fan out", nil, toolCallScript("fo1", tools.FanOutToolName, fanOutArgsJSON("alpha"))).
		route("please fan out", nil, contentScript("all done")).
		route("check alpha", nil, toolCallScript("w1", "write_file", string(writeArgs))).
		route("check alpha", nil, finishScript("f1", "alpha is fine"))

	runWorkflowParent(t, context.Background(), cfg, up, "please fan out")

	if got := callResult(t, sink.events, "fo1"); got.IsError || !strings.Contains(got.Content, "#1 alpha — ok — alpha is fine") {
		t.Fatalf("fan_out result = %+v, want the item's ok line (Plan runs the workflow with a scratch dir)", got)
	}
	var write *domain.ToolResult
	for _, e := range sink.events {
		if re, ok := e.(domain.ToolResultEvent); ok && re.Depth == 1 && re.Result.CallID == "w1" {
			write = &re.Result
		}
	}
	if write == nil {
		t.Fatal("the item child's write_file call was never answered")
	}
	if !write.IsError || !strings.HasPrefix(write.Content, "plan mode:") {
		t.Errorf("the item child's write_file result = %+v, want a Plan refusal", *write)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stat %s: %v; want no file — a Plan item child writes nothing outside the scratch dir", target, err)
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

// TestWorkflowCall_AChildBelowTheBoundGetsFanOutWithoutRunOn pins the child's roster under a parent
// whose fan_out publishes `background` (and, in the second case, `run_on` too) beside the workflow
// tool: below the depth bound the child keeps fan_out, but the plain variant — neither argument in
// its schema, since a delegate's fan_out never runs in the background nor picks a seat — and never
// the workflow tool, which only the top-level Agent runs (ADR 0089 D1/D4). The parent's own menu is
// untouched.
func TestWorkflowCall_AChildBelowTheBoundGetsFanOutWithoutRunOn(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		opts tools.FanOutOptions
	}{
		{"a background fan_out", tools.FanOutOptions{Background: true}},
		{"a background fan_out with seat choice", tools.FanOutOptions{SeatChoice: true, Background: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := workflowConfig(t, &recordingSink{})
			reg := domain.NewToolRegistry()
			_ = reg.Register(tools.NewFanOutWith(tc.opts))
			_ = reg.Register(tools.NewWorkflow())
			cfg.Tools = reg
			cfg.Delegation.MaxDepth = 2
			a, err := newAgent(cfg, scriptedResponder(t))
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}

			roster := a.defaultSubAgentTools()

			tool, ok := roster.Lookup(tools.FanOutToolName)
			if !ok {
				t.Fatal("the child's roster has no fan_out below the bound; the case would be vacuous")
			}
			if _, offered := roster.Lookup(tools.WorkflowToolName); offered {
				t.Error("the child's roster holds the workflow tool; only the top-level Agent runs it")
			}
			fanOut, isFanOut := tool.(*tools.FanOut)
			if !isFanOut || fanOut.OffersSeatChoice() || fanOut.OffersBackground() {
				t.Errorf("child's fan_out = %#v, want the plain variant", tool)
			}
			for _, arg := range []string{"background", "run_on"} {
				if schemaHasProperty(t, tool.Schema(), arg) {
					t.Errorf("child's fan_out schema publishes %q", arg)
				}
			}
			parentTool, _ := a.lookupTool(tools.FanOutToolName)
			if !schemaHasProperty(t, parentTool.Schema(), "background") {
				t.Error("the parent's own fan_out lost its background argument")
			}
		})
	}
}

// TestWorkflowCall_AnItemChildIsNotOfferedTheWorkflowTool pins the same withholding on a workflow's
// item child: its menu keeps the parent's leaf tool and finish, never the workflow tool.
func TestWorkflowCall_AnItemChildIsNotOfferedTheWorkflowTool(t *testing.T) {
	t.Parallel()

	a, upstream := newWorkflowParent(t, &recordingSink{}, domain.ModeAskBefore,
		func(cfg *domain.Config) { _ = cfg.Tools.Register(tools.NewWorkflow()) },
		finishTurn("f1", `{"status":"ok","summary":"done"}`),
	)

	spawnItem(t, a, itemSpec("a.go", nil))

	menu := upstream.requests()[0].Tools
	if slices.Contains(menu, tools.WorkflowToolName) {
		t.Errorf("the item child's menu = %v, want no workflow tool", menu)
	}
	if !slices.Contains(menu, "read_thing") || !slices.Contains(menu, tools.FinishToolName) {
		t.Errorf("the item child's menu = %v, want read_thing and finish kept", menu)
	}
}

// schemaHasProperty reports whether a JSON object schema declares name among its properties.
func schemaHasProperty(t *testing.T, schema json.RawMessage, name string) bool {
	t.Helper()
	var parsed struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("unparseable schema %s: %v", schema, err)
	}
	_, ok := parsed.Properties[name]
	return ok
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

// withSeatChoiceFanOut replaces cfg's tools with a fan_out publishing `run_on` — what the host
// builds under `sub-agents-choice: model` — plus the background switch and the workflow control
// tool when background is set, and the one read-only tool.
func withSeatChoiceFanOut(cfg domain.Config, background bool) domain.Config {
	reg := domain.NewToolRegistry()
	_ = reg.Register(tools.NewFanOutWith(tools.FanOutOptions{SeatChoice: true, Background: background}))
	if background {
		_ = reg.Register(tools.NewWorkflow())
	}
	_ = reg.Register(fakeTool{name: "read_thing", readOnly: true, result: "package main"})
	cfg.Tools = reg
	return cfg
}

// seatArgsJSON is fanOutArgsJSON over items with `run_on` set to runOn ("" leaves it out) and
// `background` to background.
func seatArgsJSON(runOn string, background bool, items ...string) string {
	args := map[string]any{"task": fanOutTask, "over": items, "returns": map[string]string{"count": "int"}}
	if runOn != "" {
		args["run_on"] = runOn
	}
	if background {
		args["background"] = true
	}
	b, _ := json.Marshal(args)
	return string(b)
}

// TestWorkflowCall_RunOnIsTheSeatOfEveryItemChild drives a fan_out whose `run_on` is published,
// with a Sub-agent server latched: "session" builds every item child on the parent's own server at
// the session cap, "sub-agents-server" and an unnamed seat route them to the latched target at its
// cap. The two servers' caps differ, so the peak in flight says whose cap sized the workflow.
func TestWorkflowCall_RunOnIsTheSeatOfEveryItemChild(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		runOn      string
		sessionCap int
		gruntCap   int
		wantGrunt  bool
	}{
		{"session runs on the parent's server at its cap", tools.RunOnSession, 3, 1, false},
		{"sub-agents-server routes at the target's cap", tools.RunOnSubAgentsServer, 1, 3, true},
		{"an unnamed seat routes as configured", "", 1, 3, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sink := &recordingSink{}
			cfg := withSeatChoiceFanOut(workflowConfig(t, sink), false)
			cfg.ParallelAgents = tc.sessionCap
			probe := newConcurrencyProbe(3, 3*time.Second)
			items := []string{"alpha", "beta", "gamma"}
			up := (&workflowResponder{}).
				route("please fan out", nil, toolCallScript("fo1", tools.FanOutToolName, seatArgsJSON(tc.runOn, false, items...))).
				route("please fan out", nil, contentScript("all done"))
			grunt := &workflowResponder{}
			for _, item := range items {
				up.route("check "+item, probe.enter, finishScript("s-"+item, item+" is fine"))
				grunt.route("check "+item, probe.enter, finishScript("g-"+item, item+" is fine"))
			}
			a, err := newAgent(cfg, up)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			a.dial = dialerTo(grunt).dial
			a.SetDelegationTarget(gruntTarget(gruntEndpoint, tc.gruntCap))

			runSubmitted(t, context.Background(), a, "please fan out")

			if got := callResult(t, sink.events, "fo1"); got.IsError {
				t.Fatalf("fan_out result is an error: %q", got.Content)
			}
			for _, item := range items {
				onGrunt, onSession := grunt.askedCount("check "+item), up.askedCount("check "+item)
				if tc.wantGrunt && (onGrunt != 1 || onSession != 0) || !tc.wantGrunt && (onGrunt != 0 || onSession != 1) {
					t.Errorf("%s ran %d times on the target and %d on the session server, want it on the target: %v",
						item, onGrunt, onSession, tc.wantGrunt)
				}
			}
			if peak := probe.peakInFlight(); peak != 3 {
				t.Errorf("peak children in flight = %d, want 3 at the cap of the server they ran on", peak)
			}
		})
	}
}

// TestWorkflowCall_TheRunnerIsSizedForItsSeat pins the width and the split budget a fan_out's
// Runner takes for each seat: a session-seated workflow takes the session's cap and window whatever
// is latched, every other seat the latched target's — and with nothing latched, the session's.
func TestWorkflowCall_TheRunnerIsSizedForItsSeat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		target    *DelegationTarget
		seat      delegationSeat
		wantWidth int
		wantLimit int
	}{
		{"configured with a target", gruntTarget(gruntEndpoint, 2), seatConfigured, 2, 32768},
		{"session with a target", gruntTarget(gruntEndpoint, 2), seatSession, 3, 32000},
		{"sub-agents-server with a target", gruntTarget(gruntEndpoint, 2), seatSubAgentsServer, 2, 32768},
		{"sub-agents-server with none latched", nil, seatSubAgentsServer, 3, 32000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := workflowConfig(t, &recordingSink{})
			cfg.Context.MaxContextTokens = 32000
			cfg.ParallelAgents = 3
			a, err := newAgent(cfg, scriptedResponder(t))
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			a.SetDelegationTarget(tc.target)

			runner, refusal := a.newWorkflowRunner(0, domain.ToolCall{}, tc.seat)

			if refusal != "" {
				t.Fatalf("newWorkflowRunner refused: %s", refusal)
			}
			if runner.Width != tc.wantWidth || runner.Split != workflow.NewSplitBudget(tc.wantLimit) {
				t.Errorf("runner width %d, split %d; want %d and the split of a %d window",
					runner.Width, runner.Split, tc.wantWidth, tc.wantLimit)
			}
		})
	}
}

// TestWorkflowCall_AnInvalidRunOnIsRefusedWithSubAgentsText refuses a `run_on` outside the two
// published spellings — on a plain fan-out and on the recipe form — with sub_agent's own text, and
// runs nothing.
func TestWorkflowCall_AnInvalidRunOnIsRefusedWithSubAgentsText(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args string
		want string
	}{
		{"a fan-out on elsewhere", seatArgsJSON("elsewhere", false, "alpha in src"),
			`invalid run_on "elsewhere": want "session" or "sub-agents-server"`},
		{"a recipe on a number", `{"recipe":"review","inputs":{"scope":"src"},"run_on":5}`,
			`invalid run_on "5": want "session" or "sub-agents-server"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sink := &recordingSink{}
			cfg := withSeatChoiceFanOut(recipeConfig(t, sink, reviewRecipe()), false)
			up := recipeFanOutUpstream(tc.args)

			runWorkflowParent(t, context.Background(), cfg, up, "please run it")

			if got := callResult(t, sink.events, "fo1"); !got.IsError || got.Content != tc.want {
				t.Errorf("fan_out result = %+v, want the error %q", got, tc.want)
			}
			if n := up.askedCount("check alpha in src"); n != 0 {
				t.Errorf("item child asked %d times, want no child for a refused call", n)
			}
			if folders := workflowFolders(t, cfg.ScratchDir); len(folders) != 0 {
				t.Errorf("workflow folders = %v, want none for a refused call", folders)
			}
		})
	}
}

// TestWorkflowCall_RunOnIsIgnoredWhereItWasNeverPublished runs a fan_out whose tool published no
// `run_on`: a seat the call names anyway — even one outside the enum — is ignored, not refused.
func TestWorkflowCall_RunOnIsIgnoredWhereItWasNeverPublished(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	cfg := workflowConfig(t, sink)
	up := (&workflowResponder{}).
		route("please fan out", nil, toolCallScript("fo1", tools.FanOutToolName, seatArgsJSON("elsewhere", false, "alpha"))).
		route("please fan out", nil, contentScript("all done")).
		route("check alpha", nil, finishScript("f1", "alpha is fine"))

	runWorkflowParent(t, context.Background(), cfg, up, "please fan out")

	if got := callResult(t, sink.events, "fo1"); got.IsError || !strings.Contains(got.Content, "#1 alpha — ok — alpha is fine") {
		t.Errorf("fan_out result = %q (error %v), want the item's result line", got.Content, got.IsError)
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
		if phase.Call != "fo1" {
			t.Errorf("phase %s names call %q, want the fan_out call fo1 its item children run under", phase.Phase, phase.Call)
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
	children := 0
	for _, e := range sink.events {
		if base := e.Identity(); base.Depth == 1 {
			children++
			if base.CallID != phases[0].Call {
				t.Errorf("item child's %T carries call %q, want the phases' call %q", e, base.CallID, phases[0].Call)
			}
		}
	}
	if children == 0 {
		t.Error("no item child event was emitted, want each child's events under the fan_out call")
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

// recipeFanOutUpstream answers a parent whose first reply calls fan_out with args and whose second
// closes the Exchange, and the review recipe's two children bound to scope src.
func recipeFanOutUpstream(args string) *workflowResponder {
	return (&workflowResponder{}).
		route("please run it", nil, toolCallScript("fo1", tools.FanOutToolName, args)).
		route("please run it", nil, contentScript("all done")).
		route("check alpha in src", nil, finishScript("f1", "alpha is fine")).
		route("check beta in src", nil, finishScript("f2", "beta is fine"))
}

func TestWorkflowCall_ARecipeRunsAndAnswersItsResultLines(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	cfg := recipeConfig(t, sink, reviewRecipe())
	// Empty fan-out fields beside recipe ask for nothing, so they do not refuse the call.
	up := recipeFanOutUpstream(`{"recipe":"review","inputs":{"scope":"src"},"task":"","over":[]}`)

	runWorkflowParent(t, context.Background(), cfg, up, "please run it")

	got := callResult(t, sink.events, "fo1")
	if got.IsError {
		t.Fatalf("fan_out result is an error: %q", got.Content)
	}
	for _, want := range []string{"#1 alpha — ok — alpha is fine count=1", "#2 beta — ok — beta is fine count=1"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("fan_out result lacks %q:\n%s", want, got.Content)
		}
	}
	if folders := workflowFolders(t, cfg.ScratchDir); len(folders) != 1 {
		t.Errorf("workflow folders = %v, want the recipe's one", folders)
	}
	for _, phase := range workflowPhaseEvents(sink.events) {
		if phase.Call != "fo1" {
			t.Errorf("recipe phase %s names call %q, want the fan_out call fo1", phase.Phase, phase.Call)
		}
	}
}

func TestWorkflowCall_ARecipeCallThatCannotRunIsAFixableError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args string
		want string
	}{
		{"an unknown recipe", `{"recipe":"nope"}`, `"nope" is not a recipe; the recipes are: review`},
		{"fan-out fields beside it", `{"recipe":"review","task":"check {item} carefully and report","over":["a"],"verify":{}}`,
			"cannot be combined with task, over; call it again with only recipe and inputs"},
		{"inputs of the wrong shape", `{"recipe":"review","inputs":{"scope":3}}`, "inputs must be an object of name: text pairs"},
		{"a missing input", `{"recipe":"review"}`, "fan_out could not run recipe review: missing input: scope"},
		{"an unknown input", `{"recipe":"review","inputs":{"scope":"src","depth":"2"}}`, `unknown input "depth"`},
		{"a recipe that is not a string", `{"recipe":5}`,
			"fan_out was not run: recipe must be the name of a recipe (a string), got 5"},
		{"a boolean recipe beside a task", `{"recipe":true,"task":"check {item} carefully and report","over":["alpha in src"]}`,
			"fan_out was not run: recipe must be the name of a recipe (a string), got true"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sink := &recordingSink{}
			cfg := recipeConfig(t, sink, reviewRecipe())

			up := recipeFanOutUpstream(tc.args)
			runWorkflowParent(t, context.Background(), cfg, up, "please run it")

			got := callResult(t, sink.events, "fo1")
			if !got.IsError || !strings.Contains(got.Content, tc.want) {
				t.Errorf("fan_out result = %+v, want an error containing %q", got, tc.want)
			}
			if folders := workflowFolders(t, cfg.ScratchDir); len(folders) != 0 {
				t.Errorf("workflow folders = %v, want none for a recipe that did not run", folders)
			}
			if n := up.askedCount("check alpha in src"); n != 0 {
				t.Errorf("item child asked %d times, want no child for a refused call", n)
			}
		})
	}
}

// recipeLookup finds the review skill, which carries a recipe, for every query.
type recipeLookup struct{}

func (recipeLookup) LookupSkill(string) domain.SkillLookupResult {
	return domain.SkillLookupResult{Found: true, Skill: domain.ResolvedSkill{
		ID: "review", DisplayName: "Review", Body: reviewBody, Recipe: true,
	}}
}

// TestWorkflowCall_LoadSkillNamesHowARecipeStartsFromTheCallersMenu drives ONE load_skill instance
// from a parent that offers fan_out and from its delegate, which at the depth bound does not: the
// recipe line is decided per call, so the parent is told to start the recipe with fan_out and the
// child that the user starts it. A Plan parent is told fan_out exactly when its scratch dir is set.
func TestWorkflowCall_LoadSkillNamesHowARecipeStartsFromTheCallersMenu(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	cfg := workflowConfig(t, sink, tools.NewSubAgent(), tools.NewLoadSkill(recipeLookup{}))
	up := (&workflowResponder{}).
		route("please load it", nil, toolCallScript("p1", "load_skill", `{"query":"review"}`)).
		route("please load it", nil, toolCallScript("p2", tools.SubAgentToolName, `{"task":"child loads the review skill"}`)).
		route("please load it", nil, contentScript("all done")).
		route("child loads the review skill", nil, toolCallScript("c1", "load_skill", `{"query":"review"}`)).
		route("child loads the review skill", nil, contentScript("loaded"))

	runWorkflowParent(t, context.Background(), cfg, up, "please load it")

	if menu := up.menus["child loads the review skill"]; slices.Contains(menu, tools.FanOutToolName) {
		t.Fatalf("the child's menu %v offers fan_out; the test needs one that does not", menu)
	}
	parent := callResult(t, sink.events, "p1")
	if want := `this is a recipe: start it with fan_out{recipe: "review"}`; !strings.Contains(parent.Content, want) {
		t.Errorf("the parent's load_skill result lacks %q:\n%s", want, parent.Content)
	}
	var child *domain.ToolResult
	for _, e := range sink.events {
		if re, ok := e.(domain.ToolResultEvent); ok && re.Depth == 1 && re.Result.CallID == "c1" {
			child = &re.Result
		}
	}
	if child == nil {
		t.Fatal("the child's load_skill call was never answered")
	}
	if want := "this is a recipe: the user starts it with /review"; !strings.Contains(child.Content, want) {
		t.Errorf("the child's load_skill result lacks %q:\n%s", want, child.Content)
	}

	// The Plan case: a Plan parent's menu offers fan_out exactly when a scratch dir is set
	// (planOffersDelegation), so its recipe line names fan_out with one and the user without.
	for _, plan := range []struct {
		scratch bool
		want    string
	}{
		{true, `this is a recipe: start it with fan_out{recipe: "review"}`},
		{false, "this is a recipe: the user starts it with /review"},
	} {
		sink := &recordingSink{}
		cfg := workflowConfig(t, sink, tools.NewLoadSkill(recipeLookup{}))
		cfg.Mode = domain.ModePlan
		if !plan.scratch {
			cfg.ScratchDir = ""
		}
		up := (&workflowResponder{}).
			route("please load it", nil, toolCallScript("p1", "load_skill", `{"query":"review"}`)).
			route("please load it", nil, contentScript("all done"))

		runWorkflowParent(t, context.Background(), cfg, up, "please load it")

		if got := callResult(t, sink.events, "p1"); !strings.Contains(got.Content, plan.want) {
			t.Errorf("Plan (scratch dir %t) load_skill result lacks %q:\n%s", plan.scratch, plan.want, got.Content)
		}
	}
}

// The background fan_out and the workflow control call (ADR 0089 D1, D4): a parent whose fan_out
// publishes `background` and whose menu carries the workflow tool — the set the TUI builds — starts
// a workflow beside its conversation and steers it.

// backgroundWorkflowConfig is workflowConfig with the background pair on the menu: fan_out
// publishing `background`, and the workflow control tool.
func backgroundWorkflowConfig(t *testing.T, sink domain.EventSink) domain.Config {
	t.Helper()
	cfg := baseConfig(sink)
	cfg.Mode = domain.ModeAskBefore
	reg := domain.NewToolRegistry()
	_ = reg.Register(tools.NewFanOutWith(tools.FanOutOptions{Background: true}))
	_ = reg.Register(tools.NewWorkflow())
	_ = reg.Register(fakeTool{name: "read_thing", readOnly: true, result: "package main"})
	cfg.Tools = reg
	cfg.ScratchDir = t.TempDir()
	cfg.WorkspaceDir = t.TempDir()
	return cfg
}

// backgroundArgsJSON is fanOutArgsJSON over items with `background` set.
func backgroundArgsJSON(items ...string) string {
	b, _ := json.Marshal(map[string]any{
		"task": fanOutTask, "over": items, "returns": map[string]string{"count": "int"}, "background": true,
	})
	return string(b)
}

// lockedEvents is a copy of every event sink has seen.
func lockedEvents(sink *lockedSink) []domain.Event {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return slices.Clone(sink.events)
}

// gateOn returns a gate that waits for ch to close or the request's context to end.
func gateOn(ch <-chan struct{}) func(context.Context) {
	return func(ctx context.Context) {
		select {
		case <-ch:
		case <-ctx.Done():
		}
	}
}

// controlCall runs one workflow control call on a with args and returns its answer.
func controlCall(a *Agent, args string) domain.ToolResult {
	return a.workflowControlResult(domain.ToolCall{ID: "wf1", Tool: tools.WorkflowToolName, Arguments: json.RawMessage(args)})
}

// soleWorkflowID is the id of the one workflow a's session holds.
func soleWorkflowID(t *testing.T, a *Agent) string {
	t.Helper()
	infos, err := a.Workflows()
	if err != nil || len(infos) != 1 {
		t.Fatalf("Workflows = %+v, %v; want exactly one", infos, err)
	}
	return infos[0].Status.ID
}

func TestWorkflowCall_BackgroundAnswersAtOnceAndRunsBesideTheConversation(t *testing.T) {
	t.Parallel()

	sink := newLockedSink()
	cfg := backgroundWorkflowConfig(t, sink)
	started, release := make(chan struct{}), make(chan struct{})
	up := (&workflowResponder{}).
		route("please fan out", nil, toolCallScript("fo1", tools.FanOutToolName, backgroundArgsJSON("alpha"))).
		route("please fan out", nil, contentScript("started it")).
		route("check alpha", signalThenWait(started, release), finishScript("f1", "alpha is fine"))
	a := newBackgroundParent(t, cfg, up)

	res := runSubmitted(t, context.Background(), a, "please fan out")
	awaitClosed(t, started, "alpha's child")

	if res.Status == domain.StatusCancelled {
		t.Fatalf("parent result = %+v, want a completed Exchange", res)
	}
	id := soleWorkflowID(t, a)
	got := callResult(t, lockedEvents(sink), "fo1")
	for _, want := range []string{
		"workflow " + id + " started in the background",
		"status: " + filepath.Join(cfg.ScratchDir, "workflows", id, "status.json"),
		`workflow{action: "status", id: "` + id + `"}`,
	} {
		if got.IsError || !strings.Contains(got.Content, want) {
			t.Errorf("fan_out answer lacks %q (error %v):\n%s", want, got.IsError, got.Content)
		}
	}
	if info := workflowInfo(t, a, id); !info.Background {
		t.Errorf("after the Exchange ended the workflow is %+v, want it still running in the background", info)
	}

	close(release)
	a.background.waitAll()

	if ends := workflowEnds(sink, id); !slices.Equal(ends, []domain.WorkflowPhase{domain.WorkflowFinished}) {
		t.Errorf("workflow ends = %v, want one finished", ends)
	}
	for _, phase := range workflowPhaseEvents(lockedEvents(sink)) {
		if phase.Workflow == id && phase.Call != domain.BackgroundWorkflowCallPrefix+id {
			t.Errorf("background phase %s names call %q, want %q", phase.Phase, phase.Call, domain.BackgroundWorkflowCallPrefix+id)
		}
	}
}

// TestWorkflowCall_BackgroundRunsBlockingWhereTheSwitchIsNotOffered pins ADR 0089 D1's other half:
// an engine whose fan_out did not publish `background` — a headless run's, a daemon firing's, the
// facade's own roster — runs a `background: true` that reaches dispatch blocking, to its end.
func TestWorkflowCall_BackgroundRunsBlockingWhereTheSwitchIsNotOffered(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	cfg := workflowConfig(t, sink)
	up := (&workflowResponder{}).
		route("please fan out", nil, toolCallScript("fo1", tools.FanOutToolName, backgroundArgsJSON("alpha"))).
		route("please fan out", nil, contentScript("all done")).
		route("check alpha", nil, finishScript("f1", "alpha is fine"))

	a, _ := runWorkflowParent(t, context.Background(), cfg, up, "please fan out")

	got := callResult(t, sink.events, "fo1")
	if got.IsError || !strings.Contains(got.Content, "#1 alpha — ok — alpha is fine count=1") {
		t.Errorf("fan_out answer = %q (error %v), want the blocking run's result lines", got.Content, got.IsError)
	}
	if info := workflowInfo(t, a, soleWorkflowID(t, a)); info.Background || info.Status.Phase != workflow.PhaseDone {
		t.Errorf("workflow = %+v, want a finished workflow that never ran in the background", info)
	}
}

// TestWorkflowCall_TheFacadeRosterOffersNoBackground pins the engine's own roster (defaultRoster,
// the one a headless run or a daemon firing gets): with fan_out and workflow lifted, fan_out still
// publishes no `background` and no workflow tool is offered.
func TestWorkflowCall_TheFacadeRosterOffersNoBackground(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(&recordingSink{})
	cfg.WorkspaceDir = t.TempDir()
	cfg.EnabledTools = []string{tools.FanOutToolName, tools.WorkflowToolName}

	roster := defaultRoster(cfg)

	if _, ok := roster.Lookup(tools.WorkflowToolName); ok {
		t.Error("the facade roster offers the workflow tool; only a Driver that offers background workflows may")
	}
	tool, ok := roster.Lookup(tools.FanOutToolName)
	if !ok {
		t.Fatal("the facade roster dropped the lifted fan_out")
	}
	if fanOut, _ := tool.(*tools.FanOut); fanOut == nil || fanOut.OffersBackground() {
		t.Error("the facade roster's fan_out publishes background")
	}
}

func TestWorkflowControl_MessageReachesTheRunningItem(t *testing.T) {
	t.Parallel()

	sink := newLockedSink()
	cfg := backgroundWorkflowConfig(t, sink)
	started, release := make(chan struct{}), make(chan struct{})
	const note = "check alpha: read the tests too"
	message := `{"action":"message","item":"alpha","text":"` + note + `"}`
	up := (&workflowResponder{}).
		route("please fan out", nil, toolCallScript("fo1", tools.FanOutToolName, backgroundArgsJSON("alpha"))).
		route("please fan out", gateOn(started), toolCallScript("wf1", tools.WorkflowToolName, message)).
		route("please fan out", nil, contentScript("told it")).
		route("check alpha", signalThenWait(started, release), toolCallScript("r1", "read_thing", `{}`)).
		route("check alpha", nil, finishScript("f1", "alpha is fine"))
	a := newBackgroundParent(t, cfg, up)

	runSubmitted(t, context.Background(), a, "please fan out")
	close(release)
	a.background.waitAll()

	events := lockedEvents(sink)
	if got := callResult(t, events, "wf1"); got.IsError || !strings.Contains(got.Content, "message queued for alpha") {
		t.Errorf("workflow message answer = %q (error %v), want it queued for alpha", got.Content, got.IsError)
	}
	landed := false
	for _, e := range events {
		if event, ok := e.(domain.ChildInterjectionEvent); ok && event.Landed && event.Input.Text == note {
			landed = true
		}
	}
	if !landed {
		t.Error("no ChildInterjectionEvent reports the message landing in alpha's child")
	}
}

func TestWorkflowControl_StatusStopAndRefusals(t *testing.T) {
	t.Parallel()

	sink := newLockedSink()
	cfg := backgroundWorkflowConfig(t, sink)
	started := make(chan struct{})
	up := (&workflowResponder{}).
		route("please fan out", nil, toolCallScript("fo1", tools.FanOutToolName, backgroundArgsJSON("alpha", "beta"))).
		route("please fan out", nil, contentScript("started it")).
		route("check alpha", nil, finishScript("f1", "alpha is fine")).
		route("check beta", signalThenWait(started, nil), cancelledScript())
	a := newBackgroundParent(t, cfg, up)
	runSubmitted(t, context.Background(), a, "please fan out")
	awaitClosed(t, started, "beta's child")
	id := soleWorkflowID(t, a)

	t.Run("status lists every workflow", func(t *testing.T) {
		got := controlCall(a, `{"action":"status"}`)
		if got.IsError || !strings.Contains(got.Content, id+" — ") || !strings.Contains(got.Content, "running in the background") {
			t.Errorf("status = %q (error %v), want %s listed as running in the background", got.Content, got.IsError, id)
		}
	})
	t.Run("status of one shows its items and the running ones", func(t *testing.T) {
		got := controlCall(a, `{"action":"status","id":"`+id+`"}`)
		for _, want := range []string{"#1 alpha — ok — alpha is fine count=1", "running items", " beta"} {
			if got.IsError || !strings.Contains(got.Content, want) {
				t.Errorf("status %s lacks %q (error %v):\n%s", id, want, got.IsError, got.Content)
			}
		}
	})
	for _, tc := range []struct {
		name, args, want string
	}{
		{"an unknown workflow", `{"action":"status","id":"nope"}`, `no workflow "nope"`},
		{"an unknown action", `{"action":"pause"}`, `action "pause"`},
		{"stop without an id", `{"action":"stop"}`, "stop needs the id"},
		{"message without text", `{"action":"message","item":"beta"}`, "message needs item"},
		{"message to no running item", `{"action":"message","item":"gamma","text":"hi"}`, `no running item "gamma"`},
		{"arguments that are not JSON", `{`, "not valid JSON"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := controlCall(a, tc.args); !got.IsError || !strings.Contains(got.Content, tc.want) {
				t.Errorf("answer = %q (error %v), want an error naming %q", got.Content, got.IsError, tc.want)
			}
		})
	}

	got := controlCall(a, `{"action":"stop","id":"`+id+`"}`)
	a.background.waitAll()

	if got.IsError || !strings.Contains(got.Content, "finished items are kept") {
		t.Errorf("stop = %q (error %v), want the stopping answer", got.Content, got.IsError)
	}
	if info := workflowInfo(t, a, id); info.Background || info.Status.Phase != workflow.PhaseStopped {
		t.Errorf("after the stop = %+v, want a stopped workflow no longer live", info)
	}
	if phases := itemPhases(workflowInfo(t, a, id)); phases["alpha"] != workflow.PhaseDone {
		t.Errorf("item phases = %v, want alpha kept done", phases)
	}
}

func TestWorkflowControl_ADelegateIsRefused(t *testing.T) {
	t.Parallel()

	a, err := newAgent(backgroundWorkflowConfig(t, &recordingSink{}), &workflowResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.depth = 1

	if got := controlCall(a, `{"action":"status"}`); !got.IsError || !strings.Contains(got.Content, "only the main agent") {
		t.Errorf("a delegate's status = %q (error %v), want the main-agent refusal", got.Content, got.IsError)
	}
	if a.offersBackground() {
		t.Error("a delegate offers background; a background workflow belongs to the top-level Agent")
	}
}
