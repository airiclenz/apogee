package main

// The Delegation-seat journey end to end (ADR 0069): with `sub-agents-choice: model`, the top-level
// model says where each delegation runs, and apogee puts the child there.
//
// Every seam below this is pinned one layer down — the schema variant in internal/tools, the spawn
// in internal/agent, the orientation line in internal/agent, the gate and the seat facts in
// cmd/apogee. None of them proves the ROPE, and the rope is the whole feature: a gate that never
// reaches the roster publishes no `run_on` at all, a seat pushed at the wrong moment describes the
// box the human left, and a `run_on` honoured against a latch nobody filled sends the child to the
// same server either way. So these runs assert the one thing no unit can — which SERVER answered
// the child — by giving the two scripted upstreams separate logs and asking which of them the
// child's own conversation landed in.
//
// The five cases are the five ways the choice can go: named near, named far, named far with nothing
// there, not offered at all, and offered while the far box goes down underneath it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The two `servers:` entries every run below is written against, and what their `description:` keys
// say. The descriptions are what the Delegations line relays, so they are the fixture's own words
// for what each box is FOR — and neither carries a semicolon, because the line joins its clauses
// with one and the announced-surface capture reads up to it.
const (
	seatSessionServer = "workstation"
	seatTargetServer  = "grunt-box"

	seatSessionDescription = "the session box, for review and design"
	seatTargetDescription  = "the grunt box, for greps and mechanical edits"
)

// The prompts seat-session.yaml answers, and the tasks the delegations they ask for carry. Each
// task names the half of the survey its child was given, which is how a child's conversation is
// told from the parent's — and one child's from the other's — in a server's request log.
const (
	seatSessionPrompt   = "Delegate this to the session seat."
	seatSubAgentPrompt  = "Delegate this to the sub-agents seat."
	seatAnnouncedPrompt = "Delegate one to each seat the orientation names."
	seatPlainPrompt     = "Say what you see."

	seatNearTask = "Survey the near half of the workspace"
	seatFarTask  = "Survey the far half of the workspace"

	seatWrapUp     = "The delegate reported."
	seatPlainReply = "I see the workspace."
)

// seatFallbackNoteText is the line a delegation that asked for the far seat and did not get it
// carries back to the model (ADR 0069 decision 9). It is internal/agent's own constant restated
// here because cmd/apogee cannot import it — which is the point: it is what the feature promises
// the MODEL will read, so a re-wording over there has to fail here.
const seatFallbackNoteText = "note: ran on the session server — the sub-agents server was unavailable"

// seatDelegationsPrefix opens the orientation bullet this whole file is about. A line that stopped
// being rendered, or started being spelled another way, takes every assertion below with it.
const seatDelegationsPrefix = "- Delegations: "

// seatUnreachable is the endpoint a `servers:` entry gets when a case wants a Sub-agent server that
// is configured and not there. Port 1 refuses immediately, which is what makes "the target is down"
// a fact one beat resolves rather than a wait.
const seatUnreachable = "http://127.0.0.1:1"

// TestE2ESeatChoiceSessionAskKeepsTheChildOnTheSessionServer is the near seat: a delegation that
// names `run_on: "session"` runs on the box the conversation is on, while a perfectly usable
// Sub-agent server stands beside it taking nothing.
func TestE2ESeatChoiceSessionAskKeepsTheChildOnTheSessionServer(t *testing.T) {
	run := launchSeatSession(t, seatChoiceModel)

	// Routing is waited for even though the near seat never consults it: the claim is that a
	// delegation stayed here BY CHOICE, and an ask sent before the latch landed would have stayed
	// here anyway. With the far seat in force, the session server answering the child is the
	// `run_on` value and nothing else.
	awaitNotice(t, run.drv, "sub-agents: routing to "+seatTargetServer)
	submit(run.drv, seatSessionPrompt)
	run.drv.WaitText(seatWrapUp)
	run.drv.WaitQuiet(settled)

	if got := childRequests(run.session, seatNearTask); got != 1 {
		t.Errorf("the session server answered %d of the child's requests; want the one the call seated there", got)
	}
	if got := len(run.target.Requests()); got != 0 {
		t.Errorf("the sub-agents server answered %d requests; a session-seated child never reaches it", got)
	}
	run.quit(t)
}

// TestE2ESeatChoiceSubAgentsAskRunsTheChildOnTheTarget is the far seat: the same session, the same
// tool, and a delegation that names `run_on: "sub-agents-server"` has its conversation answered by
// the OTHER server.
func TestE2ESeatChoiceSubAgentsAskRunsTheChildOnTheTarget(t *testing.T) {
	run := launchSeatSession(t, seatChoiceModel)

	// The send waits for routing to be in force. A `run_on: "sub-agents-server"` sent before the
	// first beat lands would find no target and fall back — correctly, and to the wrong assertion.
	awaitNotice(t, run.drv, "sub-agents: routing to "+seatTargetServer)
	submit(run.drv, seatSubAgentPrompt)
	run.drv.WaitText(seatWrapUp)
	run.drv.WaitQuiet(settled)

	if got := childRequests(run.target, seatFarTask); got != 1 {
		t.Errorf("the sub-agents server answered %d of the child's requests; want the one the call sent there", got)
	}
	if got := childRequests(run.session, seatFarTask); got != 0 {
		t.Errorf("the session server answered %d of the child's requests; the call named the other seat", got)
	}
	results := toolResults(run.session)
	if len(results) != 1 || strings.Contains(results[0], seatFallbackNoteText) {
		t.Errorf("a delegation that GOT the seat it asked for came back with the fallback note:\n%s",
			strings.Join(results, "\n---\n"))
	}
	run.quit(t)
}

// TestE2ESeatFallbackNoteRidesTheResultWhenTheTargetIsDown is the overruled ask: the far seat is
// configured and unreachable, so the child runs on the session server instead — and the model that
// made the choice is told, on the result of the call that asked, as its last line.
//
// The note's POSITION is the assertion and not merely its presence: ADR 0063 D3 owns the result's
// final line for a steered child, so a note that had been prefixed — or appended below a trailer —
// would be a decision this feature is not allowed to take. Nothing steers here, so the body is the
// whole content and its last line is where the note belongs.
func TestE2ESeatFallbackNoteRidesTheResultWhenTheTargetIsDown(t *testing.T) {
	run := launchSeatSessionOn(t, seatChoiceModel, seatUnreachable, "down-model")

	awaitNotice(t, run.drv, "sub-agents: "+seatTargetServer+" unavailable")
	submit(run.drv, seatSubAgentPrompt)
	run.drv.WaitText(seatWrapUp)
	run.drv.WaitQuiet(settled)

	if got := childRequests(run.session, seatFarTask); got != 1 {
		t.Errorf("the session server answered %d of the child's requests; an unusable ask still runs, here", got)
	}
	results := toolResults(run.session)
	if len(results) != 1 {
		t.Fatalf("the run produced %d tool results; want the one delegation's:\n%s",
			len(results), strings.Join(results, "\n---\n"))
	}
	if last := seatLastLine(results[0]); last != seatFallbackNoteText {
		t.Errorf("the delegation result's last line is %q; want the routing note %q:\n%s",
			last, seatFallbackNoteText, results[0])
	}
	run.quit(t)
}

// TestE2ESeatChoiceFixedStatesNoDelegationsLine is the gate: under the default `fixed` the model is
// told about no choice at all, because it has none — the same session, the same two described
// servers, and not one word about seats in what it is sent.
//
// It asks about the SYSTEM TEXT and not about the tool menu: a request's tool schemas are not
// observable through the stub's log, so the roster half of the gate is pinned by item 13's unit
// test and this case pins the prompt half.
func TestE2ESeatChoiceFixedStatesNoDelegationsLine(t *testing.T) {
	run := launchSeatSession(t, seatChoiceFixed)

	submit(run.drv, seatPlainPrompt)
	run.drv.WaitText(seatPlainReply)
	run.drv.WaitQuiet(settled)

	for _, req := range run.session.Requests() {
		if line := seatDelegationsLineOf(req); line != "" {
			t.Errorf("request %d states a Delegations line under sub-agents-choice: fixed:\n%s", req.N, line)
		}
	}
	run.quit(t)
}

// TestE2ESeatDelegationsLineSurvivesATargetDownBeat is ADR 0023 §6 held to: the Delegations line
// states per-session constants, so the far server going down underneath the session does not
// rewrite it.
//
// The beat is the point of the run. The routing LATCH follows availability — that is what makes an
// unusable ask fall back — and the temptation is to let the prompt follow it too, which would churn
// the standing system message every ten seconds and cost the prefix cache exactly what ADR 0023 §6
// promises. So this closes the far server for real, waits for apogee to notice, and asks whether
// the line moved.
func TestE2ESeatDelegationsLineSurvivesATargetDownBeat(t *testing.T) {
	run := launchSeatSession(t, seatChoiceModel)

	awaitNotice(t, run.drv, "sub-agents: routing to "+seatTargetServer)
	submit(run.drv, seatPlainPrompt)
	run.drv.WaitText(seatPlainReply)
	run.drv.WaitQuiet(settled)
	before := seatLastDelegationsLine(t, run.session)

	// The far box goes away, and the next beat finds it gone. Beats land on a fixed ten-second
	// cadence (internal/heartbeat.Interval), which is longer than the kit's default wait, so this
	// one is given room for two of them.
	run.target.Close()
	awaitNotice(t, run.drv, "sub-agents: "+seatTargetServer+" unavailable",
		tuitest.Within(2*heartbeat.Interval))

	sent := len(run.session.Requests())
	submit(run.drv, seatPlainPrompt)
	run.drv.WaitFor(func() bool { return len(run.session.Requests()) > sent },
		tuitest.Awaiting("the request that follows the target-down beat"))
	run.drv.WaitQuiet(settled)

	if after := seatLastDelegationsLine(t, run.session); after != before {
		t.Errorf("the Delegations line moved when the target went down:\nbefore: %s\nafter:  %s", before, after)
	}
	run.quit(t)
}

// ----------------------------------------------------------------------------
// The seat-choice fixture
// ----------------------------------------------------------------------------

// The two words `sub-agents-choice:` takes, restated here because cmd/apogee's tests write the home
// as text: a fixture that spelled the gate wrong would silently take the default.
const (
	seatChoiceFixed = "fixed"
	seatChoiceModel = "model"
)

// seatRun is one driven seat-choice session: the two scripted upstreams whose request logs answer
// "which server ran the child", plus the session and driver typing into it.
type seatRun struct {
	session *stubllm.Server
	target  *stubllm.Server // nil for a run whose Sub-agent server is a dead endpoint
	sess    *e2eSession
	drv     *tuitest.Driver
}

// quit ends the run the way a human does and fails when it did not come back cleanly.
func (r seatRun) quit(t *testing.T) {
	t.Helper()

	if err := r.sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// launchSeatSession starts a driven run against a LIVE Sub-agent server. choice is the
// `sub-agents-choice:` the home is written with.
func launchSeatSession(t *testing.T, choice string) seatRun {
	t.Helper()

	target := stubllm.New(t, loadScript(t, "seat-target"))
	run := launchSeatSessionOn(t, choice, target.URL, target.Model)
	run.target = target
	return run
}

// launchSeatSessionOn is [launchSeatSession] against a Sub-agent server the CALLER names, which is
// how a case reaches a target that is configured and not there.
func launchSeatSessionOn(t *testing.T, choice, targetEndpoint, targetModel string) seatRun {
	t.Helper()

	session := stubllm.New(t, loadScript(t, "seat-session"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIOn(t, drv, session, seatHome(t, session, choice, targetEndpoint, targetModel), "")
	waitIdle(drv)
	drv.WaitQuiet(settled)
	return seatRun{session: session, sess: sess, drv: drv}
}

// seatHome writes an apogee home with BOTH servers in its `servers:` list, each carrying the
// `description:` the Delegations line relays, the session bound to the first and the second named
// as the Sub-agent server.
//
// It is spelled out here rather than taken from [e2eHome] for [parallelHome]'s reason: the keys
// under test sit INSIDE `servers:` entries, and no line appended to the file afterwards can reach
// in there.
//
// The standing prompt is stated rather than left to the embedded default for the announced suite's
// reason: the orientation block rides ALONG on a standing system message (ADR 0023 §6 amendment),
// and a fixture leaning on apogee's own default text would be asserting about that text as much as
// about the block.
func seatHome(t *testing.T, session *stubllm.Server, choice, targetEndpoint, targetModel string) string {
	t.Helper()

	body := "system-prompt-text: |\n" +
		"  You are apogee, a terminal coding agent.\n" +
		"sub-agents-choice: " + choice + "\n" +
		"sub-agents-server: " + seatTargetServer + "\n" +
		"servers:\n" +
		"  - name: " + seatSessionServer + "\n" +
		"    endpoint: " + session.URL + "\n" +
		"    model: " + session.Model + "\n" +
		"    description: " + seatSessionDescription + "\n" +
		"  - name: " + seatTargetServer + "\n" +
		"    endpoint: " + targetEndpoint + "\n" +
		"    model: " + targetModel + "\n" +
		"    description: " + seatTargetDescription + "\n" +
		"server: " + seatSessionServer + "\n"
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write the two-server home's config: %v", err)
	}
	return home
}

// awaitNotice waits until the frame carries text, wrapping and all. It flattens both sides for the
// reason [flatten] exists: a routing notice is a whole sentence, and the transcript may break it
// across rows at whatever width the pane has, so a row-wise search would be asking about the layout
// rather than about the notice.
func awaitNotice(t *testing.T, drv *tuitest.Driver, text string, opts ...tuitest.Option) {
	t.Helper()

	want := flatten(text)
	opts = append(opts, tuitest.Awaiting("the notice "+text))
	drv.WaitFor(func() bool { return strings.Contains(flatten(drv.Frame().String()), want) }, opts...)
}

// seatSystemText is a request's system messages joined in wire order — what the orientation block
// arrives inside, and the one place a claim about what apogee ANNOUNCED can be made from.
func seatSystemText(req stubllm.Request) string {
	var parts []string
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			parts = append(parts, msg.Content)
		}
	}
	return strings.Join(parts, "\n")
}

// seatDelegationsLineOf is the request's Delegations bullet, or "" when it states none.
func seatDelegationsLineOf(req stubllm.Request) string {
	for _, line := range strings.Split(seatSystemText(req), "\n") {
		if strings.HasPrefix(line, seatDelegationsPrefix) {
			return line
		}
	}
	return ""
}

// seatLastDelegationsLine is the Delegations bullet off the most recent request that stated one. It
// fails the test when no request did, so a case comparing the line across a beat can never pass by
// comparing two absences.
func seatLastDelegationsLine(t *testing.T, stub *stubllm.Server) string {
	t.Helper()

	requests := stub.Requests()
	for i := len(requests) - 1; i >= 0; i-- {
		if line := seatDelegationsLineOf(requests[i]); line != "" {
			return line
		}
	}
	t.Fatalf("no request the server answered states a Delegations line")
	return ""
}

// seatLastLine is the last line of a tool result — where ADR 0069 decision 9 puts the fallback
// note, directly beneath the work it qualifies.
func seatLastLine(body string) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	return lines[len(lines)-1]
}
