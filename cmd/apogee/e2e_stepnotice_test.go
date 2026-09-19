package main

// The step-budget notice end to end (ADR 0077 addendum): a real `apogee headless` run over a stub
// whose Exchange delegates once to a child capped at four Turns, asserted on what the STUB
// received — the child's own tool messages, which are the model's view — because a notice that
// was booked but never rendered onto the wire would look identical from inside the Agent. The
// unit tests in internal/agent pin the threshold to the Turn; what these prove is the announced
// surface: the exact fence and line a child on a real run is handed, once, on the result closing
// its third Turn, and nothing at all when the key is absent.

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/stubllm"
)

const (
	// stepNoticeName is the reaction id the notice's fence, its firing line and its `/settings`
	// row are keyed by.
	stepNoticeName = "step-budget-notice"

	// stepNoticePrompt is the prompt the script's delegating turn answers (testdata/stubllm/step-notice.yaml).
	stepNoticePrompt = "Delegate the survey to a sub-agent."

	// stepNoticeTask is the child's task, and how a child's request is told from the parent's:
	// every message of the child's conversation carries it and none of the parent's does.
	stepNoticeTask = "Survey the workspace files one at a time"

	// stepNoticeCap is the `delegate-max-steps:` the journeys pin; the notice fires on the Turn
	// reaching ceil(0.75 × 4) = 3. stepNoticeTurns is how many of the child's tool results the
	// stub sees as a request's LAST message short of the wrap-up — the cap's fourth closes the Turn
	// that trips the cap, and the wrap-up request that follows ends on it with the directive fenced
	// on as an engine note, which childToolMessages leaves out.
	stepNoticeCap   = 4
	stepNoticeTurns = 3

	// stepNoticeLine is the exact line the notice writes at that cap
	// (internal/agent/prompts/step-budget-notice.txt).
	stepNoticeLine = "steps: 3 of 4 used — 1 left before the wrap-up Turn; write your output now"

	// stepNoticeConfig is the `extraConfig` a notice journey runs with: the pinned cap and the key
	// switched on; stepNoticeOffConfig is the same home with the key ABSENT — the shipped default.
	stepNoticeConfig    = "delegate-max-steps: 4\n" + stepNoticeName + ": true\n"
	stepNoticeOffConfig = "delegate-max-steps: 4\n"
)

// stepNoticeFence is the substring every fence of the notice's own opens with.
var stepNoticeFence = domain.AdviceFencePrefix + stepNoticeName

// TestE2EStepNoticeReachesTheChild is the announced-surface journey: with the key on, the third
// tool message the child read ends with the exact shipped trailer — the fence domain.RenderAdvice
// composes for the notice, engine origin, at post-tool-result, on the child's Turn 2 (its third)
// — around the rendered line, and no other child tool message carries the notice's fence at all.
func TestE2EStepNoticeReachesTheChild(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "step-notice"))
	headlessStepNotice(t, stub, stepNoticeConfig)
	stub.AssertConsumed(t)

	msgs := childToolMessages(t, stub)
	if len(msgs) != stepNoticeTurns {
		t.Fatalf("the child read %d tool messages, want %d — one per Turn short of the cap", len(msgs), stepNoticeTurns)
	}
	for i, got := range msgs {
		if i == 2 {
			continue
		}
		if strings.Contains(got, stepNoticeFence) {
			t.Errorf("the child's tool message %d carries %q; only the third may:\n%s", i+1, stepNoticeFence, got)
		}
	}

	got := msgs[2]
	want := domain.RenderAdvice(domain.AdviceSpan{
		Reaction: stepNoticeName,
		Origin:   domain.OriginEngine,
		Moment:   domain.MomentPostToolResult,
		Turn:     2,
	}, stepNoticeLine)
	if !strings.HasSuffix(got, want) {
		t.Errorf("the child's third tool message reached the model as\n%q\nwant it to end with the trailer\n%q", got, want)
	}
	if n := strings.Count(got, adviceFenceMark); n != 1 {
		t.Errorf("the child's third tool message carries %d advice fences, want exactly the notice's one:\n%s", n, got)
	}
}

// TestE2EStepNoticeBooksOneFiring is the same run read off the Event stream: `--format json`
// carries exactly one `reaction_fired` line for the notice, at the child's depth, booked under the
// `notice` action with the step and the cap as its detail.
func TestE2EStepNoticeBooksOneFiring(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "step-notice"))
	stdout := headlessStepNotice(t, stub, stepNoticeConfig, "--format", formatJSON)

	var fired []int
	lines := jsonEventLines(t, stdout)
	for i, line := range lines {
		if line["event"] != "reaction_fired" || stringMember(t, i, line, "reaction") != stepNoticeName {
			continue
		}
		fired = append(fired, i)
	}
	if len(fired) != 1 {
		t.Fatalf("the stream booked %d firings for %s; want one (lines %v)", len(fired), stepNoticeName, fired)
	}
	i := fired[0]
	if action, detail := stringMember(t, i, lines[i], "action"), stringMember(t, i, lines[i], "detail"); action != "notice" || detail != "step 3 of 4" {
		t.Errorf("the firing books action %q detail %q; want notice / \"step 3 of 4\"", action, detail)
	}
	if depth := envelopeNumber(t, i, lines[i], "depth"); depth != 1 {
		t.Errorf("the firing carries depth %v; want the child's 1", depth)
	}
}

// TestE2EStepNoticeIsOffByDefault is the shipped default: the same capped delegation in a home
// that never names the key hands the child every result whole and fenceless. The three child tool
// messages are the positive control — the child reached its third Turn, so a silent run is the
// switch and not a short delegation.
func TestE2EStepNoticeIsOffByDefault(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "step-notice"))
	headlessStepNotice(t, stub, stepNoticeOffConfig)

	msgs := childToolMessages(t, stub)
	if len(msgs) != stepNoticeTurns {
		t.Fatalf("the child read %d tool messages, want %d, so a silent run proves nothing", len(msgs), stepNoticeTurns)
	}
	for n, req := range stub.Requests() {
		for _, msg := range req.Messages {
			if strings.Contains(msg.Content, stepNoticeFence) {
				t.Errorf("request %d carries a %q message with the notice's fence; the key is absent, "+
					"so the notice is off:\n%s", n+1, msg.Role, msg.Content)
			}
		}
	}
}

// childToolMessages is the tool message each CHILD request ended on, in request order and each
// once — the child's own view of the call that request followed — read off the requests whose
// conversation carries the child's task and none of the parent's; a request ending on anything
// but a tool result contributes nothing, and neither does the wrap-up request, whose tail is the
// capping Turn's tool result with the wrap-up directive fenced on as an engine note — the
// closing report's request, not a working Turn's.
func childToolMessages(t *testing.T, stub *stubllm.Server) []string {
	t.Helper()

	var out []string
	for _, req := range stub.Requests() {
		isChild := false
		for _, msg := range req.Messages {
			if strings.Contains(msg.Content, stepNoticeTask) {
				isChild = true
				break
			}
		}
		if !isChild {
			continue
		}
		last := req.Messages[len(req.Messages)-1]
		if last.ToolCallID == "" || strings.Contains(last.Content, domain.EngineNoteFencePrefix) {
			continue
		}
		out = append(out, last.Content)
	}
	return out
}

// headlessStepNotice runs one real `apogee headless` for the notice journeys and hands back what
// the answer stream carried — headlessFillNotice's shape (e2e_fillnotice_test.go) without the
// fixture seeding: the workspace the kit makes is all a four-Turn child needs.
func headlessStepNotice(t *testing.T, stub *stubllm.Server, extraConfig string, extra ...string) (stdout string) {
	t.Helper()

	prev := runOnce
	runOnce = run.Once
	t.Cleanup(func() { runOnce = prev })

	// The environment must not move the home or the mode out from under the run.
	assertNoAmbientApogeeConfig(t)
	t.Setenv(config.EnvMode, "")

	home := t.TempDir()
	writeConfigHome(t, home, extraConfig+
		"servers:\n"+
		"  - name: stub\n"+
		"    endpoint: "+stub.URL+"\n"+
		"    model: "+stub.Model+"\n"+
		"server: stub\n")

	cmd := newHeadlessCommand()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetIn(strings.NewReader(""))
	args := []string{"--config", home, "--workspace", e2eWorkspace(t)}
	args = append(args, extra...)
	cmd.SetArgs(append(args, stepNoticePrompt))
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("headless: %v\n%s", err, errBuf.String())
	}
	return outBuf.String()
}
