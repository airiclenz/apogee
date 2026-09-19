package main

// The step-budget notice end to end: a real `apogee headless` run over a stub whose Exchange
// delegates once to a child capped at four Turns, asserted on what the STUB received — the
// child's own tool messages, which are the model's view — because a note applied in the Agent but
// never rendered onto the wire would look identical from inside it. The unit tests in
// internal/agent pin the threshold to the Turn; what this proves is the announced surface: the
// exact engine fence and line a child on a real run is handed, once, on the result closing its
// third Turn, with no key set — the notice is structural for every delegate.

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
	// stepNoticeTopic is the engine-note topic the notice is fenced under.
	stepNoticeTopic = "step budget"

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
	// (internal/agent/prompts/step-notice.txt).
	stepNoticeLine = "steps: 3 of 4 used — 1 left before the wrap-up Turn; write your output now"

	// stepNoticeConfig is the `extraConfig` the notice journey runs with: the pinned cap and
	// nothing else — no `step-budget-notice` key, because the notice is structural and the
	// key-absent home is the positive journey.
	stepNoticeConfig = "delegate-max-steps: 4\n"
)

// stepNoticeFence is the substring every fence of the notice's own opens with.
var stepNoticeFence = domain.EngineNoteFencePrefix + stepNoticeTopic

// TestE2EStepNoticeReachesTheChild is the announced-surface journey: with no key set, the third
// tool message the child read ends with the exact shipped fence — the engine note
// domain.RenderEngineNote composes on the notice's topic — around the rendered line, carries no
// advice fence at all, and no other child tool message carries the notice's fence.
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
	want := domain.RenderEngineNote(stepNoticeTopic, stepNoticeLine)
	if !strings.HasSuffix(got, want) {
		t.Errorf("the child's third tool message reached the model as\n%q\nwant it to end with the engine note\n%q", got, want)
	}
	if n := strings.Count(got, stepNoticeFence); n != 1 {
		t.Errorf("the child's third tool message carries %d step-budget fences, want exactly one:\n%s", n, got)
	}
	if strings.Contains(got, adviceFenceMark) {
		t.Errorf("the child's third tool message carries an advice fence; the notice is the engine's, not a Reaction's:\n%s", got)
	}
}

// childToolMessages is the tool message each CHILD request ended on, in request order and each
// once — the child's own view of the call that request followed — read off the requests whose
// conversation carries the child's task and none of the parent's; a request ending on anything
// but a tool result contributes nothing, and neither does the wrap-up request, whose tail is the
// capping Turn's tool result with the wrap-up directive fenced on as an engine note — the
// closing report's request, not a working Turn's. Only THAT note is skipped: the step-budget
// notice is an engine note too, and the result it rides is a working Turn's that counts.
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
		if last.ToolCallID == "" || strings.Contains(last.Content, domain.EngineNoteFencePrefix+"wrap-up]") {
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
