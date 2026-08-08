package agent

// The three anytime-safe setters the settings surface drives mid-session — SetBypass,
// SetCompactionEnabled and SetContextFiles (the SetMode/SetConfineToWorkspace class). What each
// test pins is the CONSUMPTION BOUNDARY: Bypass lands at the next hook evaluation, the
// auto-Compaction gate at the next fold decision, and the context-file names only at the next
// session boundary — deliberately NOT at once, so a session keeps seeding byte-identical content.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// gateRow is a bare catalogued row: the Bypass gate reads nothing but the Capability, so no hook
// implementation is needed to probe it.
func gateRow(id domain.MechanismID, capability domain.Capability) domain.RegisteredMechanism {
	return domain.RegisteredMechanism{Descriptor: domain.MechanismDescriptor{ID: id, Capability: capability}}
}

// TestAgentSetBypassFlipsTheGateBetweenEvaluations proves a runtime SetBypass changes the skip
// decision on the SAME Agent with no rebuild, and that the off-ramp exemption (D5/ADR 0006) holds
// on both sides of the flip: switching Bypass on never withdraws a recovery guarantee.
func TestAgentSetBypassFlipsTheGateBetweenEvaluations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		title        string
		capability   domain.Capability
		wantUnderOff bool // skipped with Bypass off
		wantUnderOn  bool // skipped with Bypass on
	}{
		{title: "proactive nudge", capability: domain.CapProactiveNudge, wantUnderOff: false, wantUnderOn: true},
		{title: "response repair", capability: domain.CapResponseRepair, wantUnderOff: false, wantUnderOn: true},
		{title: "off-ramp survives", capability: domain.CapOffRamp, wantUnderOff: false, wantUnderOn: false},
	}
	for _, tc := range tests {
		t.Run(tc.title, func(t *testing.T) {
			t.Parallel()
			a, err := newAgent(baseConfig(&recordingSink{}), echoResponder{reply: "ok"})
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			row := gateRow("probe", tc.capability)

			if got := a.skipUnderBypass(row); got != tc.wantUnderOff {
				t.Fatalf("skipUnderBypass at construction = %t, want %t", got, tc.wantUnderOff)
			}

			a.SetBypass(true)

			if got := a.skipUnderBypass(row); got != tc.wantUnderOn {
				t.Fatalf("skipUnderBypass after SetBypass(true) = %t, want %t", got, tc.wantUnderOn)
			}

			a.SetBypass(false)

			if got := a.skipUnderBypass(row); got != tc.wantUnderOff {
				t.Fatalf("skipUnderBypass after SetBypass(false) = %t, want %t again", got, tc.wantUnderOff)
			}
		})
	}
}

// TestAgentSetBypassObservedByTheNextHookFire drives the switch through real dispatch: a catalogued
// proactive-nudge Mechanism fires in the first Exchange and is gone from the next one, while the
// off-ramp beside it keeps firing. It pins the wiring end to end — dispatch reads the LIVE flag,
// not cfg's construction seed.
func TestAgentSetBypassObservedByTheNextHookFire(t *testing.T) {
	nudged, offRamped := 0, 0
	cfg := baseConfig(&recordingSink{})
	cfg.Mechanisms = domain.NewMechanismRegistry()
	mustAddMech(t, cfg.Mechanisms, recordingMech{id: "nudge", cap: domain.CapProactiveNudge, fired: &nudged}.row())
	mustAddMech(t, cfg.Mechanisms, recordingMech{id: "offramp", cap: domain.CapOffRamp, fired: &offRamped}.row())
	a, err := newAgent(cfg, echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	_ = runExchange(t, a, "first")
	if nudged != 1 || offRamped != 1 {
		t.Fatalf("before the switch: nudge fired %d times, off-ramp %d; want 1 and 1", nudged, offRamped)
	}

	a.SetBypass(true)

	_ = runExchange(t, a, "second")
	if nudged != 1 {
		t.Errorf("nudge fired %d times in total; want 1 — Bypass must drop it from the next hook fire", nudged)
	}
	if offRamped != 2 {
		t.Errorf("off-ramp fired %d times in total; want 2 — Bypass never withdraws a recovery guarantee", offRamped)
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
	row := gateRow("probe", domain.CapProactiveNudge)

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
			_ = a.skipUnderBypass(row)
			_ = a.shouldAutoCompact()
			_ = a.contextFileList()
			_, _ = a.newChildAgent() // the spawn seam reads the live Bypass and Compaction gates too
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
