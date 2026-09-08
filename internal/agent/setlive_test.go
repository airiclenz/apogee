package agent

// The anytime-safe setters the settings surface drives mid-session — SetReactions (and the
// SetBypass wrapper over it),
// SetCompactionEnabled and SetContextFiles (the SetMode/SetConfineToWorkspace class). What each
// test pins is the CONSUMPTION BOUNDARY: Bypass lands at the next reaction evaluation, the
// auto-Compaction gate at the next fold decision, and the context-file names only at the next
// session boundary — deliberately NOT at once, so a session keeps seeding byte-identical content.

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// gateRow is a bare armed Reaction: the Bypass gate reads nothing but the Class, so no handler
// implementation is needed to probe it.
func gateRow(id string, class domain.Class) domain.Reaction {
	return domain.Reaction{ID: id, Origin: domain.OriginUser, Class: class}
}

// TestAgentSetBypassFlipsTheGateBetweenEvaluations proves a runtime SetBypass changes the skip
// decision on the SAME Agent with no rebuild, and that the class exemption (ADR 0076 D9) holds on
// both sides of the flip: switching Bypass on never withdraws an observe or gate Reaction.
func TestAgentSetBypassFlipsTheGateBetweenEvaluations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		title        string
		class        domain.Class
		wantUnderOff bool // skipped with Bypass off
		wantUnderOn  bool // skipped with Bypass on
	}{
		{title: "advise", class: domain.ClassAdvise, wantUnderOff: false, wantUnderOn: true},
		{title: "shape (view)", class: domain.ClassShapeView, wantUnderOff: false, wantUnderOn: true},
		{title: "observe survives", class: domain.ClassObserve, wantUnderOff: false, wantUnderOn: false},
	}
	for _, tc := range tests {
		t.Run(tc.title, func(t *testing.T) {
			t.Parallel()
			a, err := newAgent(baseConfig(&recordingSink{}), echoResponder{reply: "ok"})
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			row := gateRow("probe", tc.class)

			if got := a.bypassSkips(row); got != tc.wantUnderOff {
				t.Fatalf("bypassSkips at construction = %t, want %t", got, tc.wantUnderOff)
			}

			a.SetBypass(true)

			if got := a.bypassSkips(row); got != tc.wantUnderOn {
				t.Fatalf("bypassSkips after SetBypass(true) = %t, want %t", got, tc.wantUnderOn)
			}

			a.SetBypass(false)

			if got := a.bypassSkips(row); got != tc.wantUnderOff {
				t.Fatalf("bypassSkips after SetBypass(false) = %t, want %t again", got, tc.wantUnderOff)
			}
		})
	}
}

// TestAgentSetBypassObservedByTheNextHookFire drives the switch through real dispatch: an armed
// shape-view Reaction fires in the first Exchange and is gone from the next one, while the observe
// Reaction beside it keeps firing. It pins the wiring end to end — dispatch reads the LIVE flag,
// not cfg's construction seed.
func TestAgentSetBypassObservedByTheNextHookFire(t *testing.T) {
	nudged, offRamped := 0, 0
	cfg := baseConfig(&recordingSink{})
	cfg.Reactions = []domain.Reaction{
		recordingReaction("nudge", domain.ClassShapeView, &nudged),
		countingReaction("offramp", domain.ClassObserve, &offRamped),
	}
	a, err := newAgent(cfg, echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	_ = runExchange(t, a, "first")
	if nudged != 1 || offRamped != 1 {
		t.Fatalf("before the switch: the shape-view Reaction fired %d times, the observe one %d; want 1 and 1", nudged, offRamped)
	}

	a.SetBypass(true)

	_ = runExchange(t, a, "second")
	if nudged != 1 {
		t.Errorf("the shape-view Reaction fired %d times in total; want 1 — Bypass must drop it from the next Moment", nudged)
	}
	if offRamped != 2 {
		t.Errorf("the observe Reaction fired %d times in total; want 2 — Bypass never switches observe off", offRamped)
	}
}

// TestSetReactionsSwapsFloorAndBypassAtomically drives the ONE swap seam against a concurrent
// cascade under the race detector. The generation is a compound value now — Floor and Bypass
// under one lock, with the builtin ladder rebuilt from the Floor (ADR 0076 A8) — so the setter
// races both the per-Moment Bypass read and the ladder read fire takes, which is what the old
// per-field locks could not cover. It asserts nothing beyond "no data race and both halves
// land": that is the whole point of a swap that must never be observed half applied.
func TestSetReactionsSwapsFloorAndBypassAtomically(t *testing.T) {
	a, err := newAgent(baseConfig(&recordingSink{}), echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	const iters = 500
	generations := []domain.Generation{
		{},
		{Bypass: true, Floor: domain.FloorConfig{DisableToolLoopBreaker: true}},
		{Floor: domain.FloorConfig{DisableReadCache: true, DisableToolResultCap: true}},
		{Bypass: true},
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			a.SetReactions(generations[i%len(generations)])
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			if _, err := a.fire(context.Background(), domain.MomentPostResponse, postResponse(true)); err != nil {
				t.Errorf("fire: %v", err)
				return
			}
			_ = a.Generation()
		}
	}()
	wg.Wait()

	// The last generation installed is what the Agent runs, both halves of it.
	want := domain.Generation{Bypass: true, Floor: domain.FloorConfig{DisableToolCallSalvage: true}}
	a.SetReactions(want)
	got := a.Generation()
	if got.Floor != want.Floor || got.Bypass != want.Bypass {
		t.Errorf("Generation() = %+v, want %+v", got, want)
	}
	if ids := builtinIDs(a); slices.Contains(ids, guardToolCallSalvage) {
		t.Errorf("ladder = %v, want the salvage guard switched out of it", ids)
	}
}

// TestAgentSetCompactionEnabledMovesTheAutoFoldGate proves the switch arms and disarms the
// automatic, budget-driven fold for the NEXT boundary: an over-budget history folds once the gate
// is switched on mid-session, and is sent whole once it is switched off — neither needs a rebuild.
func TestAgentSetCompactionEnabledMovesTheAutoFoldGate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		title            string
		seed             bool // cfg.Context.CompactionEnabled at construction
		set              bool // the value SetCompactionEnabled installs
		wantSummaryCalls int
	}{
		{title: "switched on arms the next fold", seed: false, set: true, wantSummaryCalls: 1},
		{title: "switched off disarms it", seed: true, set: false, wantSummaryCalls: 0},
	}
	for _, tc := range tests {
		t.Run(tc.title, func(t *testing.T) {
			t.Parallel()
			up := &compactSpyResponder{reply: "reply"}
			cfg := autoCompactConfig(&recordingSink{})
			cfg.Context.CompactionEnabled = tc.seed
			a, err := newAgent(cfg, up)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			seedLargeConv(a) // far past the History allocation for an 8k window

			a.SetCompactionEnabled(tc.set)

			if err := a.Submit(domain.UserInput{Text: "next"}); err != nil {
				t.Fatalf("Submit: %v", err)
			}
			if _, err := a.Step(context.Background()); err != nil {
				t.Fatalf("Step: %v", err)
			}

			if up.summaryCalls != tc.wantSummaryCalls {
				t.Fatalf("summarizer calls = %d, want %d", up.summaryCalls, tc.wantSummaryCalls)
			}
		})
	}
}

// TestAgentSetContextFilesLandsAtTheNextSessionBoundary pins the deliberate freeze: new names
// change nothing for the session in flight — the standing content stays byte-identical, so the
// server's prefix KV cache survives — and are picked up by the next boundary (/clear). It also
// pins the defensive copy: a caller mutating its own slice afterwards cannot reach the engine.
func TestAgentSetContextFilesLandsAtTheNextSessionBoundary(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeContextFile(t, dir, "A.md", "alpha conventions")
	writeContextFile(t, dir, "B.md", "beta conventions")

	a, err := newAgent(contextConfig(&recordingSink{}, dir, "A.md"), echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if got := cachedContent(a.contextFiles, "A.md"); got != "alpha conventions" {
		t.Fatalf("cached A.md at construction = %q, want the seeded content", got)
	}

	names := []string{"B.md"}
	a.SetContextFiles(true, names)
	names[0] = "A.md" // the engine copied the list; a later caller mutation must not reach it

	if got := cachedContent(a.contextFiles, "A.md"); got != "alpha conventions" {
		t.Errorf("cached A.md after SetContextFiles = %q; the session's content must not move mid-session", got)
	}
	if got := cachedContent(a.contextFiles, "B.md"); got != "" {
		t.Errorf("B.md was folded in before the next boundary: %q", got)
	}

	if err := a.ClearContext(); err != nil {
		t.Fatalf("ClearContext: %v", err)
	}

	if got := cachedContent(a.contextFiles, "B.md"); got != "beta conventions" {
		t.Errorf("cached B.md after /clear = %q, want the new name's content", got)
	}
	if got := cachedContent(a.contextFiles, "A.md"); got != "" {
		t.Errorf("the replaced name survived the boundary: A.md = %q", got)
	}

	a.SetContextFiles(false, []string{"B.md"}) // enable false is the other spelling of "no names"
	if err := a.ClearContext(); err != nil {
		t.Fatalf("ClearContext after disabling: %v", err)
	}
	if len(a.contextFiles) != 0 {
		t.Errorf("context files still cached after SetContextFiles(false, …): %+v", a.contextFiles)
	}
}

// writeContextFile writes one workspace context file, failing the test if it cannot.
func writeContextFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// TestAgentAnytimeSettersConcurrent runs the three setters (the settings-surface side) against the
// worker-side live reads under the race detector, proving each lock covers its field. It asserts
// nothing beyond "no data race" — that is the whole point of a setter that lands mid-Step.
func TestAgentAnytimeSettersConcurrent(t *testing.T) {
	a, err := newAgent(contextConfig(&recordingSink{}, t.TempDir(), "A.md"), echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	row := gateRow("probe", domain.ClassAdvise)

	const iters = 1000
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			a.SetBypass(i%2 == 0)
			a.SetCompactionEnabled(i%2 == 0)
			a.SetContextFiles(i%2 == 0, []string{"A.md"})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_ = a.bypassSkips(row)
			_ = a.shouldAutoCompact()
			_ = a.contextFileList()
			_, _ = a.newChildAgent("call_sub", "the delegated task", "") // the spawn seam reads the live Bypass and Compaction gates too
		}
	}()
	wg.Wait()
}

// TestAgentSetParallelAgentsMovesTheFanOutWidth proves the fifth anytime-safe setter: the Parallel
// agents cap (ADR 0039) is seeded from the construction Config and moved by SetParallelAgents with
// no rebuild — which is what a `/server` switch onto a differently-sized server, and a beat that
// observes one, both need. cfg.ParallelAgents stays the seed and is never read again.
func TestAgentSetParallelAgentsMovesTheFanOutWidth(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(&recordingSink{})
	cfg.ParallelAgents = 3
	a, err := newAgent(cfg, &compactSpyResponder{reply: "reply"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if got := a.parallelAgentsCap(); got != 3 {
		t.Errorf("seeded cap = %d, want the Config's 3", got)
	}

	// A move onto a server with no width to offer: serial, and stated as such rather than left at
	// the previous server's number.
	a.SetParallelAgents(1)
	if got := a.parallelAgentsCap(); got != 1 {
		t.Errorf("cap after SetParallelAgents(1) = %d, want 1", got)
	}

	a.SetParallelAgents(8)
	if got := a.parallelAgentsCap(); got != 8 {
		t.Errorf("cap after SetParallelAgents(8) = %d, want 8", got)
	}
}

// The setter is in the anytime-safe class, so it must be race-free against a concurrent read from
// the goroutine driving the loop — the property `-race` proves and a comment cannot.
func TestAgentSetParallelAgentsIsRaceFree(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(&recordingSink{})
	a, err := newAgent(cfg, &compactSpyResponder{reply: "reply"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 1; i <= 200; i++ {
			a.SetParallelAgents(i)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = a.parallelAgentsCap()
		}
	}()
	wg.Wait()
}
