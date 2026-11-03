package agent

import (
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/floor"
)

// testBounds is a delegate's bounds as a test sets or reads them: the step, token and time caps,
// the clock reading the time bound counts from, the bound that ended the Exchange and the
// max_steps a clamped ask requested. withBounds and boundsOf are the only test code that names
// the Agent fields behind it, so a change to where an Agent keeps its bounds changes them alone.
type testBounds struct {
	steps     int
	tokens    int
	time      time.Duration
	start     time.Time
	hit       delegateBound
	requested int
}

// withBounds sets every one of a's delegate bounds to b — a zero field clears that bound — and
// returns a, so a bare test Agent is built and bounded in one expression.
func withBounds(a *Agent, b testBounds) *Agent {
	a.stepCap = b.steps
	a.tokenCap = b.tokens
	a.timeCap = b.time
	a.capStart = b.start
	a.capHit = b.hit
	a.capRequested = b.requested
	return a
}

// boundsOf reads a's delegate bounds.
func boundsOf(a *Agent) testBounds {
	return testBounds{
		steps:     a.stepCap,
		tokens:    a.tokenCap,
		time:      a.timeCap,
		start:     a.capStart,
		hit:       a.capHit,
		requested: a.capRequested,
	}
}

// goldenWrapUpTail is wrapUpDirectiveTail spelled out, so the directive goldens below pin the
// bytes the child reads rather than the constant that writes them.
const goldenWrapUpTail = "no further tool calls are possible: the tools have been withdrawn for this final reply." +
	"\n\nReport back to the agent that delegated this task now: what you found, what you " +
	"concluded, and what remains unfinished. This is your only remaining reply — anything you " +
	"do not write here is lost. Do not continue the task. Do not write what a tool would have " +
	"printed — your reply is read as your report, nothing else."

// goldenFinishTail is the part of wrapUpFinishDirectiveFormat after the bound phrase.
const goldenFinishTail = " for this item: no further tool calls are possible but one call to finish." +
	"\n\nCall finish now with your receipt — status partial when the work is unfinished, blocked" +
	" when you could not proceed — and a summary saying where you stopped. Do not continue the task."

// TestBoundRowsRenderEverySurface pins, per delegate bound, every string its row renders — the
// ErrorEvent, both result heads, the wrap-up directive, the workflow finish directive, the typed
// outcome, the snapshot word and the ¾ notice — byte for byte, off a bare capped Agent (no driven
// child). The token markers quote the raw count while the token notice reads in formatTokens.
func TestBoundRowsRenderEverySurface(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		bound         delegateBound
		errText       string
		reportHead    string
		nonReportHead string
		wrapUp        string
		finish        string
		delegation    domain.DelegationBound
		word          string
		notice        *noticeGolden
	}{
		{
			name:          "steps",
			bound:         boundSteps,
			errText:       "delegate capped at its step cap (3 steps) — asking it to sum up; narrow the task or raise delegate-max-steps",
			reportHead:    "[delegate stopped at its step cap (3 steps); partial result — engine summary and closing report follow]",
			nonReportHead: "[delegate stopped at its step cap (3 steps); no closing report — the delegate's last reply reads as narration of its next step, not a finding; engine summary follows]",
			wrapUp:        "You have reached the step limit for this delegation (3 steps) and " + goldenWrapUpTail,
			finish:        "You have reached the step limit (3 steps)" + goldenFinishTail,
			delegation:    domain.DelegationStepCap,
			word:          "steps",
			notice:        &noticeGolden{spent: 3, limit: 4, want: "steps: 3 of 4 used — 1 left before the wrap-up Turn; write your output now"},
		},
		{
			name:          "tokens",
			bound:         boundTokens,
			errText:       "delegate capped at its token budget (20000000 tokens) — asking it to sum up; narrow the task or raise delegate-max-tokens",
			reportHead:    "[delegate stopped at its token budget (20000000 tokens); partial result — engine summary and closing report follow]",
			nonReportHead: "[delegate stopped at its token budget (20000000 tokens); no closing report — the delegate's last reply reads as narration of its next step, not a finding; engine summary follows]",
			wrapUp:        "You have reached the token budget for this delegation (20000000 tokens) and " + goldenWrapUpTail,
			finish:        "You have reached the token budget (20000000 tokens)" + goldenFinishTail,
			delegation:    domain.DelegationTokenBudget,
			word:          "tokens",
			notice:        &noticeGolden{spent: 26000000, limit: 20000000, want: "tokens: 26.0M of 20.0M spent — 0 left before the wrap-up Turn; write your output now"},
		},
		{
			name:          "time",
			bound:         boundTime,
			errText:       "delegate capped at its time limit (2h0m) — asking it to sum up; narrow the task or raise delegate-timeout",
			reportHead:    "[delegate stopped at its time limit (2h0m); partial result — engine summary and closing report follow]",
			nonReportHead: "[delegate stopped at its time limit (2h0m); no closing report — the delegate's last reply reads as narration of its next step, not a finding; engine summary follows]",
			wrapUp:        "You have reached the time limit for this delegation (2h0m) and " + goldenWrapUpTail,
			finish:        "You have reached the time limit (2h0m)" + goldenFinishTail,
			delegation:    domain.DelegationTimeLimit,
			word:          "time",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			child := withBounds(&Agent{}, testBounds{steps: 3, tokens: 20000000, time: 2 * time.Hour, hit: tc.bound})

			surfaces := []struct{ surface, got, want string }{
				{"error text", child.boundErrText(), tc.errText},
				{"report head", child.capResultHead(floor.ShapeReport), tc.reportHead},
				{"non-report head", child.capResultHead(floor.ShapeNarration), tc.nonReportHead},
				{"wrap-up directive", child.wrapUpDirective(), tc.wrapUp},
				{"finish directive", child.wrapUpFinishDirective(), tc.finish},
				{"snapshot word", tc.bound.word(), tc.word},
			}

			for _, s := range surfaces {
				if s.got != s.want {
					t.Errorf("%s = %q, want %q", s.surface, s.got, s.want)
				}
			}
			if got := tc.bound.delegationBound(); got != tc.delegation {
				t.Errorf("delegation bound = %v, want %v", got, tc.delegation)
			}
			assertBoundNotice(t, tc.bound.row().notice, tc.notice)
		})
	}
}

// noticeGolden is a bound notice's expected render for spent of limit.
type noticeGolden struct {
	spent, limit int
	want         string
}

// assertBoundNotice checks a row's notice against the expected render, or its absence when want
// is nil.
func assertBoundNotice(t *testing.T, got *boundNotice, want *noticeGolden) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Errorf("notice = topic %q, want none for this bound", got.topic)
		}
		return
	}
	if got == nil {
		t.Fatal("notice = none, want one")
	}
	if text := got.render(want.spent, want.limit); text != want.want {
		t.Errorf("notice = %q, want %q", text, want.want)
	}
}

// TestParseDelegateBoundRoundTrips pins that every bound's snapshot word reads back as that bound
// and that a word no build writes is refused.
func TestParseDelegateBoundRoundTrips(t *testing.T) {
	t.Parallel()

	for b := range boundRows {
		bound := delegateBound(b)

		got, ok := parseDelegateBound(bound.word())

		if !ok || got != bound {
			t.Errorf("parseDelegateBound(%q) = %v, %t; want %v, true", bound.word(), got, ok, bound)
		}
	}
	if _, ok := parseDelegateBound("turns"); ok {
		t.Error(`parseDelegateBound("turns") = true; want false for a word this build never writes`)
	}
}
