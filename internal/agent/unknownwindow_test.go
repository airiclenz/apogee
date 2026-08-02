package agent

// What the growth bounds do when NO context window is known — neither discovery nor
// `context-window:` reported one, so context.Allocate has nothing to split and the Budget's
// allocation fields are all zero.
//
// Before the 2026-08-01 audit's follow-up B, four bounds read that zero as "no basis, stand down":
// the emergency fold rendered the whole conversation (fixed with its own regression tests in
// emergencyfold_test.go / compact_test.go), and the predictive guard, the boundary compaction
// trigger and the structural tool-result clamp all went inert. These tests pin the shared
// replacement: every one of them measures a TRANSCRIPT quantity — the conversation, or one result
// about to enter it — against compactUnknownWindowTranscriptTokens, the SAME ceiling the fold
// renders against, so nothing that survives a bound can defeat the fold and no fixed cost a fold
// cannot shed can trip one. A known window keeps its window-derived arithmetic untouched.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
)

// unknownWindowCeilingChars is the assumed ceiling in characters: the shared token ceiling through
// the Budget's chars→token ratio, which is the conversion every bound applies.
func unknownWindowCeilingChars(a *Agent) int {
	return int(float64(compactUnknownWindowTranscriptTokens) * a.budget().CharsPerToken)
}

// linesOfExactly builds a payload of exactly n characters across many short lines, so a bound can
// be probed to the character while the clamp's LINE-based head/tail elision can still shrink it.
func linesOfExactly(n int) string {
	const line = "payload line\n"
	return strings.Repeat(line, n/len(line)+1)[:n]
}

// unknownWindowAgent is an Agent whose window is unknown, with its assumed ceiling in characters.
func unknownWindowAgent(t *testing.T, sink domain.EventSink) (*Agent, int) {
	t.Helper()
	a, err := newAgent(baseConfig(sink), echoResponder{reply: "unused"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if b := a.budget(); b.ContextLimit != 0 || b.History != 0 {
		t.Fatalf("budget = %+v with no configured window, want the zero allocation", b)
	}
	return a, unknownWindowCeilingChars(a)
}

// TestUnknownWindowBoundsShareOneCeiling is the property the fix rests on: with no window there is
// nothing to ALLOCATE across a request's parts, so all three bounds collapse onto the single
// conservative ceiling rather than each inventing a default — and each sits exactly on it, one
// character under passing and one character over firing.
func TestUnknownWindowBoundsShareOneCeiling(t *testing.T) {
	a, ceiling := unknownWindowAgent(t, &recordingSink{})
	calibrate(a) // CALIBRATED: the predictive guard's ratio margin is out of the way

	t.Run("the predictive guard", func(t *testing.T) {
		guarded, _ := unknownWindowAgent(t, &recordingSink{})
		calibrate(guarded) // CALIBRATED: the ratio margin is out of the way and the ceiling is the bare one
		exceeds := func() bool {
			req := domain.NewRequest(guarded.cfg.Model, guarded.conv.Messages(), guarded.toolMenu(), guarded.budget(), 0, nil)
			return guarded.requestExceedsWindow(req)
		}
		guarded.conv.Append(domain.Message{Role: domain.RoleUser, Content: strings.Repeat("x", ceiling)})
		if exceeds() {
			t.Errorf("a transcript measuring exactly the %d-char ceiling exceeds it; the bound is not the ceiling", ceiling)
		}
		guarded.conv.Append(domain.Message{Role: domain.RoleUser, Content: "x"})
		if !exceeds() {
			t.Errorf("a transcript one char past the %d-char ceiling does not exceed it", ceiling)
		}
	})

	t.Run("the boundary compaction trigger", func(t *testing.T) {
		over, err := newAgent(baseConfig(&recordingSink{}), echoResponder{reply: "unused"})
		if err != nil {
			t.Fatalf("newAgent: %v", err)
		}
		over.conv.Append(domain.Message{Role: domain.RoleUser, Content: strings.Repeat("x", ceiling)})
		if over.historyExceedsAllocation() {
			t.Errorf("a history of exactly the %d-char ceiling trips the trigger", ceiling)
		}
		over.conv.Append(domain.Message{Role: domain.RoleUser, Content: "x"})
		if !over.historyExceedsAllocation() {
			t.Errorf("a history one char past the %d-char ceiling does not trip the trigger", ceiling)
		}
	})

	t.Run("the structural tool-result clamp", func(t *testing.T) {
		atCeiling := linesOfExactly(ceiling)
		if got := a.clampToolResult(atCeiling); got != atCeiling {
			t.Errorf("a result of exactly the %d-char ceiling was clamped to %d chars", ceiling, len(got))
		}
		over := linesOfExactly(ceiling + 1)
		if got := a.clampToolResult(over); got == over {
			t.Errorf("a result of %d chars past the %d-char ceiling was committed whole", len(over), ceiling)
		}
	})
}

// TestUnknownWindowGuardMeasuresTheTranscriptNotTheWholeRequest is the regression on the trap the
// shared ceiling opens: it is a TRANSCRIPT budget, so a bound that measures a REQUEST against it
// compares the wrong quantity — and the tool menu alone is bigger than the ceiling. Measured that
// way the predictive guard fires on a handful of 30-character messages and folds the conversation
// away at every Exchange, since no fold can shrink a menu. This is the one agent test that
// configures the real default registry, which is why the defect was invisible: every other test
// runs on a menu of nothing.
func TestUnknownWindowGuardMeasuresTheTranscriptNotTheWholeRequest(t *testing.T) {
	const exchanges = 5

	sink := &recordingSink{}
	up := &recoveryResponder{reply: "the reply", summary: "UNREACHED"}
	cfg := baseConfig(sink) // no MaxContextTokens: nothing reported a window
	cfg.Tools = tools.NewDefaultRegistry(t.TempDir())
	cfg.Context.CompactionEnabled = true
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	// A code-heavy 3.0 chars/token, reached exactly through the estimator's half-weight blend from
	// the 4.0 seed. CALIBRATED, so no uncertainty margin softens the compare.
	a.tokens.Calibrate(2000, 1000)

	menuChars := domain.PromptChars(nil, a.toolMenu())
	if ceiling := unknownWindowCeilingChars(a); menuChars <= ceiling {
		t.Fatalf("the default tool menu measures %d chars, not past the %d-char ceiling — this test pins nothing",
			menuChars, ceiling)
	}

	for i := range exchanges {
		if err := a.Submit(domain.UserInput{Text: fmt.Sprintf("question %d", i)}); err != nil {
			t.Fatalf("Submit %d: %v", i, err)
		}
		if _, err := a.Step(context.Background()); err != nil {
			t.Fatalf("Step %d: %v", i, err)
		}
	}

	if up.summaries != 0 {
		t.Errorf("summarizer calls = %d over %d short Exchanges, want 0 — the %d-char tool menu is a fixed cost no fold can shed",
			up.summaries, exchanges, menuChars)
	}
	if a.conv.Len() != 2*exchanges {
		t.Errorf("conv.Len() = %d (roles %s), want %d — a conversation that fits was folded away",
			a.conv.Len(), convRoles(a), 2*exchanges)
	}
	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("%d ErrorEvent(s) %v on a session that never overflowed", len(errs), errs)
	}
}

// TestUnknownWindowClampKeepsTheFoldSurvivable drives the case that made the clamp's inertness
// load-bearing rather than merely conservative: a giant tool result committed under an unknown
// window becomes the most recent message, and the emergency fold's transcript render keeps the most
// recent message UNCONDITIONALLY — so an unclamped one re-wedges the session that bounding the
// fold un-wedged. The clamped result must therefore fit the fold's own budget.
func TestUnknownWindowClampKeepsTheFoldSurvivable(t *testing.T) {
	a, ceiling := unknownWindowAgent(t, &recordingSink{})

	giant := numberedLines(20_000) // ~1.6 MB — a whole-file read, the audit's shape
	committed := a.clampToolResult(giant)

	if len(committed) >= len(giant) {
		t.Fatalf("a %d-char result was committed unclamped", len(giant))
	}
	if !strings.Contains(committed, "omitted") {
		t.Errorf("clamped result missing the elision marker:\n%.200s", committed)
	}
	if len(committed) > a.compactTranscriptChars() {
		t.Errorf("clamped result is %d chars, past the fold's own %d-char transcript budget — the fold cannot shed it",
			len(committed), a.compactTranscriptChars())
	}
	if len(committed) > ceiling {
		t.Errorf("clamped result is %d chars, past the %d-char ceiling", len(committed), ceiling)
	}
}

// TestUnknownWindowFoldsAtTheExchangeBoundary drives the boundary trigger end to end: a history
// past the assumed ceiling folds at the next Exchange opening exactly as a known-window one does,
// so an unbudgeted session sheds history proactively instead of growing until the server rejects it.
func TestUnknownWindowFoldsAtTheExchangeBoundary(t *testing.T) {
	sink := &recordingSink{}
	up := &compactSpyResponder{reply: "FOLDED-SUMMARY"}
	cfg := baseConfig(sink)
	cfg.Context.CompactionEnabled = true // compaction is ON: the WINDOW is what used to make it inert
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedLargeConv(a) // ~42k chars ≈ 10.5k tokens, well past the assumed ceiling
	if !a.historyExceedsAllocation() {
		t.Fatal("setup: the seeded history does not exceed the assumed ceiling; the trigger would be untested")
	}

	if err := a.Submit(domain.UserInput{Text: "the fresh question"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	if up.summaryCalls != 1 {
		t.Fatalf("summarizer calls = %d, want exactly 1 auto-fold", up.summaryCalls)
	}
	if n := len(up.last.Messages); n > 5 {
		t.Errorf("main request carried %d messages; the boundary fold did not shed history", n)
	}
	last := up.last.Messages[len(up.last.Messages)-1]
	if last.Role != string(domain.RoleUser) || !strings.Contains(last.Content, "the fresh question") {
		t.Errorf("fresh user message not preserved as its own turn: %+v", last)
	}
	if hasEvent[domain.ErrorEvent](sink.events) {
		t.Error("a successful auto-fold emitted an ErrorEvent")
	}
}

// TestKnownWindowIgnoresTheUnknownWindowCeiling pins the other half: the assumed ceiling is a
// FALLBACK, never a second threshold a budgeted session also has to clear. A history and a tool
// result sized between the ceiling and the 8k window's larger History allocation leave a
// known-window session completely untouched.
func TestKnownWindowIgnoresTheUnknownWindowCeiling(t *testing.T) {
	sink := &recordingSink{}
	a, floorChars := floorAgent(t, sink)
	ceiling := unknownWindowCeilingChars(a)
	if floorChars <= ceiling {
		t.Fatalf("test setup: the %d-token window allocates %d chars, not more than the %d-char ceiling",
			floorWindow, floorChars, ceiling)
	}

	between := linesOfExactly(ceiling + 1) // one char past the ceiling, well under the allocation
	if got := a.clampToolResult(between); got != between {
		t.Errorf("a result past the assumed ceiling but under the History allocation was clamped (%d chars in, %d out)",
			len(between), len(got))
	}

	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: between})
	if a.historyExceedsAllocation() {
		t.Error("a history past the assumed ceiling but under the History allocation tripped the boundary trigger")
	}
	req := domain.NewRequest(a.cfg.Model, a.conv.Messages(), nil, a.budget(), 0, nil)
	if a.requestExceedsWindow(req) {
		t.Error("a request past the assumed ceiling but inside the working room tripped the predictive guard")
	}
}
