package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/tools"
)

// The shakeout's two bounds and the ceiling it holds the parent to. They are deliberately
// far tighter than the shipped defaults (delegate-max-steps 80, working-window 0): the point
// is to make both bounds bite within a few Turns of a real model rather than to reproduce a
// production posture.
const (
	// liveDelegateStepCap is the delegate-max-steps this shakeout runs the child under.
	liveDelegateStepCap = 5
	// liveDelegateWorkingWindow is the working-window the child's Budget must be bounded to,
	// whatever window the server advertises.
	liveDelegateWorkingWindow = 32768
	// liveParentGrowthCeiling is the most, in tokens, a capped delegation may cost the parent's
	// context. A child that ran to its cap may have read hundreds of thousands of tokens; what
	// reaches the parent is one marker line plus the child's last visible text, and this is the
	// bound that says so.
	liveParentGrowthCeiling = 4096
)

// liveDelegateTask is the delegated task: a real one that invites an unbounded number of
// single-file reads, so a model that simply does what it is told keeps asking for tools until
// the engine stops it. Nothing here is a trick — it is the shape of the /code-audit lens
// delegation that burned 633 steps on 2026-08-25.
const liveDelegateTask = "List every Go source file under the internal/ directory of this workspace, " +
	"then read them ONE AT A TIME with read_file — a single read per turn, never several — and " +
	"write a one-sentence summary of each file as you go. Work through the whole list in order; " +
	"do not stop early, and do not summarise the directory instead of the files."

// liveEventLog is an EventSink that records every Event a live run emits. Unlike
// recordingSink it locks: the live path runs with -race under `make live-eval`, and a
// delegation is free to emit from a goroutine of its own.
type liveEventLog struct {
	mu     sync.Mutex
	events []domain.Event
}

func (l *liveEventLog) Emit(e domain.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

// snapshot returns a copy of everything emitted so far, safe to walk without the lock.
func (l *liveEventLog) snapshot() []domain.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]domain.Event(nil), l.events...)
}

// budgetProbe is an experimental pre-request hook that records the Budget every request was
// built under, keyed by the Depth of the agent that built it. It is the only seam a test has
// onto a CHILD's Budget: the child Agent is constructed inside runSubAgent and closed there,
// so nothing outside the delegation ever holds it, while its requests all pass through here.
//
// It carries no live state to isolate between a parent and its children, so it deliberately
// does NOT implement domain.SubAgentScoped — the child inherits this very instance
// (MechanismRegistry.ForSubAgent), which is what lets one probe see both depths.
type budgetProbe struct {
	mu   sync.Mutex
	seen map[int][]domain.Budget
}

func (p *budgetProbe) PreRequest(_ context.Context, req *domain.Request) error {
	view := req.View()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.seen == nil {
		p.seen = make(map[int][]domain.Budget)
	}
	p.seen[view.Depth()] = append(p.seen[view.Depth()], view.Budget())
	return nil
}

// at returns the Budgets recorded for agents at the given nesting depth, in request order.
func (p *budgetProbe) at(depth int) []domain.Budget {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]domain.Budget(nil), p.seen[depth]...)
}

// childTurns counts the Turns a delegation actually took, from the events it emitted: the
// DISTINCT Turn indices of its non-maintenance UsageEvents. One model call per Turn is what
// the loop does, but neither an in-place retry (same Turn, a second call) nor a mid-run
// compaction fold (a Maintenance-flagged call) is a Turn, and both are live possibilities —
// counting indices rather than calls is immune to each.
func childTurns(events []domain.Event, depth int) int {
	turns := make(map[int]struct{})
	for _, e := range events {
		if ev, ok := e.(domain.UsageEvent); ok && ev.Depth == depth && !ev.Maintenance {
			turns[ev.Turn] = struct{}{}
		}
	}
	return len(turns)
}

// childUsage returns the non-maintenance UsageEvents a delegation at the given depth emitted.
func childUsage(events []domain.Event, depth int) []domain.UsageEvent {
	var out []domain.UsageEvent
	for _, e := range events {
		if ev, ok := e.(domain.UsageEvent); ok && ev.Depth == depth && !ev.Maintenance {
			out = append(out, ev)
		}
	}
	return out
}

// TestLiveDelegateCapAndWorkingWindow is the live-path shakeout for the delegate bounds this
// plan added: the step cap that ends a runaway child's Exchange (Agent.Run / endAtStepCap),
// the working window that bounds its Budget below the advertised one (loop.budget), and the
// partial-result marker that is all a capped delegation costs the parent's context
// (runSubAgent). Every unit test around them scripts the Upstream; this is the only proof that
// the three hold against a real model reading real files, which is the run that produced them
// (a 633-step delegate, 910K of context, 346M tokens for one delegation).
//
// It is opt-in, gated on APOGEE_LIVE_ENDPOINT exactly like the other live tests in this repo,
// so `make check` never depends on a running model:
//
//	APOGEE_LIVE_ENDPOINT=http://127.0.0.1:1111 go test -race -count=1 \
//	    -run TestLiveDelegateCapAndWorkingWindow -v ./internal/agent/
//
// -count=1 is load-bearing: the live server's loaded model is not a Go-visible input, so
// caching would replay a stale PASS across a model swap. APOGEE_LIVE_MODEL pins the model
// (else it is discovered) and APOGEE_API_KEY carries the bearer token for a keyed server.
//
// The delegation is driven through runSubAgent with a synthesised sub_agent call rather than
// by prompting the parent model into emitting one. Everything under test is downstream of that
// call — the child's construction, its cap, its Budget, and the result the parent is handed —
// so making the PARENT's tool choice live would add the one failure mode this shakeout is not
// about (a model that declines to delegate) and prove nothing extra.
func TestLiveDelegateCapAndWorkingWindow(t *testing.T) {
	endpoint := os.Getenv("APOGEE_LIVE_ENDPOINT")
	if endpoint == "" {
		t.Skip("set APOGEE_LIVE_ENDPOINT (and optionally APOGEE_LIVE_MODEL) to run the live delegate shakeout")
	}
	apiKey := os.Getenv("APOGEE_API_KEY")

	discoverCtx, cancelDiscover := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelDiscover()
	info, err := provider.NewClient(endpoint, os.Getenv("APOGEE_LIVE_MODEL"),
		provider.WithAPIKey(apiKey)).Discover(discoverCtx)
	if err != nil {
		t.Fatalf("discover the model at %s: %v", endpoint, err)
	}
	// The whole point of the working window is that it sits INSIDE the advertised one, so a
	// server whose window is already at or below the bound cannot show the split at all.
	if info.ContextWindow <= liveDelegateWorkingWindow {
		t.Fatalf("the server at %s advertises a %d-token window; this shakeout needs a large-window model "+
			"(more than the %d-token working window it bounds the child to)",
			endpoint, info.ContextWindow, liveDelegateWorkingWindow)
	}
	t.Logf("live delegate shakeout: endpoint=%s model=%s advertised-window=%d",
		endpoint, info.ActiveModel, info.ContextWindow)

	// The workspace is this repository: the delegated task reads its own internal/ tree, which is
	// the closest a test gets to the real thing without inventing a corpus. `go test` runs in the
	// package's directory, so the root is two levels up.
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve the workspace root: %v", err)
	}

	log := &liveEventLog{}
	probe := &budgetProbe{}
	mechanisms := domain.NewMechanismRegistry()
	if err := mechanisms.AddExperimental(domain.HookPreRequest, probe); err != nil {
		t.Fatalf("register the budget probe: %v", err)
	}

	// A read-only tool set in Plan mode: the child can discover and read, and can do nothing that
	// would gate on an Approver this test does not supply.
	registry := domain.NewToolRegistry()
	for _, tool := range []domain.Tool{
		tools.NewSubAgent(),
		tools.NewFindFiles(root, nil),
		tools.NewListDir(root, nil),
		tools.NewReadFile(root, nil),
	} {
		if err := registry.Register(tool); err != nil {
			t.Fatalf("register %s: %v", tool.Name(), err)
		}
	}

	cfg := domain.Config{
		Endpoint:     endpoint,
		APIKey:       apiKey,
		Model:        info.ActiveModel,
		Mode:         domain.ModePlan,
		WorkspaceDir: root,
		Events:       log,
		Tools:        registry,
		Mechanisms:   mechanisms,
		Delegation:   domain.DelegationConfig{MaxSteps: liveDelegateStepCap},
		Context: domain.ContextConfig{
			MaxContextTokens: info.ContextWindow,
			WorkingWindow:    liveDelegateWorkingWindow,
			// The shipped default, and the posture the child's mid-Exchange fold needs to exist
			// at all: a delegation that folds under pressure is half of what bounds it.
			CompactionEnabled: true,
		},
	}

	parent, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = parent.Close() }()

	args, err := json.Marshal(tools.SubAgentArgs{Task: liveDelegateTask, Name: "live cap shakeout"})
	if err != nil {
		t.Fatalf("marshal the sub_agent arguments: %v", err)
	}

	// Wide enough that a slow single-slot server is not what fails the test, and short enough to
	// land inside `go test`'s own default 10-minute ceiling with a diagnosis rather than a panic.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	result, outcome := parent.runSubAgent(ctx, domain.ToolCall{
		ID:        "live-delegate-cap",
		Tool:      tools.SubAgentToolName,
		Arguments: args,
	})
	if outcome != dispatchDone {
		t.Fatalf("delegation outcome = %v, want dispatchDone — the run was cancelled or rolled back", outcome)
	}

	events := log.snapshot()

	// 1. The run ended AT the cap, and said so: a non-error partial result opening with the
	//    marker, and exactly one ErrorEvent on the child's own stream naming the bound.
	if result.IsError {
		t.Fatalf("delegation reported IsError; a capped child is not a failure: %q", result.Content)
	}
	marker := fmt.Sprintf(stepCapResultFormat, liveDelegateStepCap)
	if !strings.HasPrefix(result.Content, marker+"\n") {
		t.Fatalf("delegation result = %q, want it to open with the step-cap marker %q — the child "+
			"finished on its own inside %d Turns, so the cap never bit",
			result.Content, marker, liveDelegateStepCap)
	}
	if got := countCapErrors(events, 1); got != 1 {
		t.Errorf("step-cap ErrorEvents at Depth 1 = %d, want exactly 1", got)
	}
	if turns := childTurns(events, 1); turns < 1 || turns > liveDelegateStepCap {
		t.Errorf("child Turns = %d, want between 1 and the cap of %d", turns, liveDelegateStepCap)
	}

	// 2. The child worked in the WINDOW the server advertises and the ROOM the key allows: its
	//    readings are stamped with the advertised window, its Budget is bounded to the working one.
	usage := childUsage(events, 1)
	if len(usage) == 0 {
		t.Fatal("the child emitted no UsageEvent; the server reported no usage, so the window stamp cannot be checked")
	}
	for _, u := range usage {
		if u.ContextWindow != info.ContextWindow {
			t.Errorf("child UsageEvent (Turn %d) ContextWindow = %d, want the advertised %d",
				u.Turn, u.ContextWindow, info.ContextWindow)
		}
	}
	budgets := probe.at(1)
	if len(budgets) == 0 {
		t.Fatal("the budget probe saw no request at Depth 1; the child never built one")
	}
	for i, b := range budgets {
		if b.Window != info.ContextWindow {
			t.Errorf("child Budget[%d].Window = %d, want the advertised %d", i, b.Window, info.ContextWindow)
		}
		if b.ContextLimit != liveDelegateWorkingWindow {
			t.Errorf("child Budget[%d].ContextLimit = %d, want the working window %d",
				i, b.ContextLimit, liveDelegateWorkingWindow)
		}
	}

	// 3. What all of that cost the PARENT's context: the delegation appends one tool result to it,
	//    so the result's own token size IS the growth. It is measured through the chars→token ratio
	//    the child's last Budget carries, which this very model calibrated against its own reported
	//    usage — an honest count rather than the uncalibrated default.
	last := budgets[len(budgets)-1]
	grew := domain.Budget{CharsPerToken: last.CharsPerToken}.EstimateTokens(len(result.Content))
	if grew >= liveParentGrowthCeiling {
		t.Errorf("the delegation grew the parent's context by ~%d tokens (%d chars at %.2f chars/token), "+
			"want under %d — the whole point of the marker is that a capped child reports, not replays",
			grew, len(result.Content), last.CharsPerToken, liveParentGrowthCeiling)
	}
	t.Logf("capped after %d Turns; parent context grew by ~%d tokens", childTurns(events, 1), grew)
}
