package main

// A `/server` switch keeps the session fanning out — the driven half of two claims that had no test
// at all: that `switchServer` re-follows the Parallel agents cap onto the entry it moves to (ADR
// 0039 decision 2), and that more than one delegation is ever live at once (checklist T-16 step 12,
// which asserted only "the fan-out still happens" because the server it moved to pinned nothing and
// stubllm answers no `/props`, so the cap was 1 whatever the move did).
//
// Both runs are the same session shape and differ in ONE character of config: the entry the session
// switches onto carries `parallel-agents: 2` or it does not. That is deliberate — the pin is the only
// variable, so a difference in what the screen shows is a difference the switch produced. The pin
// rather than a discovered `total_slots` is what the width comes from here because a stub upstream
// serves no `/props` to discover one from, and teaching it one would test the discovery half instead
// of the arrival half these runs are about.
//
// What is asserted is what a human watching would see: the delegation rows in the transcript. Two of
// them standing at once, neither queued, is a fan-out; one of them standing alone for as long as the
// test cares to watch is a session running its delegations one at a time.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

const (
	// The two `servers:` entries every run below is written against: the one the session boots on
	// and leaves, and the one it switches to and fans out from.
	parallelStartServer  = "start"
	parallelFanOutServer = "fanout"

	// parallelPin is the second entry's `parallel-agents:` line — the whole difference between the
	// two runs — and parallelNoPin is the same entry without it.
	parallelPin   = "    parallel-agents: 2\n"
	parallelNoPin = ""

	// fanOutPrompt is the prompt parallel-two-delegations.yaml answers with two `sub_agent` calls,
	// and firstDelegate/secondDelegate are the names those calls carry — the words the delegation
	// rows wear on screen, and the only place either word appears in the fixture.
	fanOutPrompt   = "Fan out two delegations."
	firstDelegate  = "alpha"
	secondDelegate = "beta"

	// childHalfTask is the substring every delegate's own conversation carries and no parent request
	// does, which is how a child's request is told from the parent's in the server's log.
	childHalfTask = "half of the survey"

	// scheduledWord is what a QUEUED delegation's row says — a delegation the model asked for with no
	// child running behind it yet (internal/tui's scheduledSummary, subagentblock.go). It is restated
	// here because cmd/apogee cannot import it, which is the point: it is what the screen promises a
	// human, so a rename over there has to fail here.
	scheduledWord = "scheduled"
)

// parallelSize is the terminal these runs read at. A hundred and forty columns for the same reason
// T-16 takes it: the footer truncates the workdir cell from the left when the row runs out of room,
// and the model name beside it is what says which server the session is on.
var parallelSize = tuitest.Size{W: 140, H: 30}

// serialWatch is how long the control run watches a serial fan-out before it says the second
// delegation is not there. It is a window rather than a settle because a live delegation blinks, so
// the screen never goes quiet while one is running — and it is honest at any length, because the
// child that is running can never finish: a session capped at one has no way to announce the second
// delegate for as long as the first is stalled.
const serialWatch = 500 * time.Millisecond

// TestE2EParallelDelegationsFollowAServerSwitch is the pinned run: the session moves onto an entry
// that pins two, and the reply that follows puts two delegations on screen at the same time.
func TestE2EParallelDelegationsFollowAServerSwitch(t *testing.T) {
	start, fanOut, drv := launchParallelSession(t, parallelPin)

	switchToFanOutServer(t, drv, fanOut)
	submit(drv, fanOutPrompt)

	// Both children are asking the fan-out server before either has answered, which is what "at
	// once" means on the wire: neither request can ever complete, so a second one arriving is a
	// second child genuinely running beside the first.
	drv.WaitFor(func() bool { return childRequests(fanOut, childHalfTask) >= 2 },
		tuitest.Awaiting("both delegates to be asking the server the session moved to"))

	// And what the human sees is the same fact: two delegation rows standing together, neither of
	// them the queued row a fan-out narrower than its group would leave behind.
	awaitPane(t, drv, "two delegation rows on screen at once, neither queued", func(f tuitest.Frame) bool {
		return frameHas(f, firstDelegate) && frameHas(f, secondDelegate) && !frameHas(f, scheduledWord)
	})

	// The whole fan-out ran on the server the session arrived at, not the one it left.
	if got := len(start.Requests()); got != 0 {
		t.Errorf("the server the session left answered %d requests after the switch", got)
	}

	// The children never come back, so the run ends where it stands — the T-03 shape.
	drv.Kill()
}

// TestE2EParallelDelegationsStaySerialWithoutThePin is the control: the same session, the same reply
// and the same two delegations, onto an entry that pins nothing. The cap resolves to the serial
// floor, so the second delegation is not announced at all while the first is still working.
func TestE2EParallelDelegationsStaySerialWithoutThePin(t *testing.T) {
	start, fanOut, drv := launchParallelSession(t, parallelNoPin)

	switchToFanOutServer(t, drv, fanOut)
	submit(drv, fanOutPrompt)

	drv.WaitFor(func() bool { return childRequests(fanOut, childHalfTask) >= 1 },
		tuitest.Awaiting("the first delegate to reach the server the session moved to"))
	awaitPane(t, drv, "the first delegation's row", func(f tuitest.Frame) bool {
		return frameHas(f, firstDelegate)
	})

	paneNeverShows(t, drv, "the second delegation while the first is still working", func(f tuitest.Frame) bool {
		return frameHas(f, secondDelegate)
	})
	if got := childRequests(fanOut, childHalfTask); got != 1 {
		t.Errorf("%d delegates asked the server; a serial fan-out runs one at a time", got)
	}
	if got := len(start.Requests()); got != 0 {
		t.Errorf("the server the session left answered %d requests after the switch", got)
	}

	drv.Kill()
}

// launchParallelSession starts a driven run on the START server, with the fan-out server configured
// beside it and pin appended to its entry. It returns once the session is up and idle.
//
// The start server's script is one turn it is never asked to play: the first prompt is typed AFTER
// the switch, so every request in these runs — the session title, the fan-out reply and both
// children — belongs to the server the session moved to, and both tests assert that the one it left
// answered nothing.
func launchParallelSession(t *testing.T, pin string) (start, fanOut *stubllm.Server, drv *tuitest.Driver) {
	t.Helper()

	start = stubllm.New(t, stubllm.Script{
		Model: "start-model",
		Turns: []stubllm.Turn{{Repeat: true, Text: "The session should not be asking me anything."}},
	})
	fanOut = stubllm.New(t, fanOutScript(t))

	drv = tuitest.NewDriver(t, parallelSize)
	launchTUIOn(t, drv, start, parallelHome(t, start, fanOut, pin), "")
	waitIdle(drv)
	drv.WaitQuiet(settled)
	return start, fanOut, drv
}

// fanOutScript is the script the fan-out server plays: parallel-two-delegations.yaml's turns first,
// parallel-child.yaml's behind them.
//
// Two fixtures rather than one because they are two conversations that happen to share an upstream —
// a parent asking for delegates, and the delegates themselves — and the ORDER is why they are joined
// here rather than by hand: a turn is offered the requests in file order, so the parent's turns have
// to stand in front of the child's catch-all or a parent request would be answered as a child's.
func fanOutScript(t *testing.T) stubllm.Script {
	t.Helper()

	script := loadScript(t, "parallel-two-delegations")
	script.Turns = append(script.Turns, loadScript(t, "parallel-child").Turns...)
	return script
}

// parallelHome writes an apogee home with BOTH servers in its `servers:` list, the session starting
// on the first, and pin appended to the second's entry.
//
// It is spelled out here rather than taken from [e2eHome] for the reason [launcherHome] is: the key
// under test sits INSIDE a `servers:` entry, and no line appended to the file afterwards can reach in
// there. The list is also the reason the run needs a home of its own at all — e2eHome writes exactly
// one server, and a switch needs somewhere to go.
func parallelHome(t *testing.T, start, fanOut *stubllm.Server, pin string) string {
	t.Helper()

	body := "servers:\n" +
		"  - name: " + parallelStartServer + "\n" +
		"    endpoint: " + start.URL + "\n" +
		"    model: " + start.Model + "\n" +
		"  - name: " + parallelFanOutServer + "\n" +
		"    endpoint: " + fanOut.URL + "\n" +
		"    model: " + fanOut.Model + "\n" +
		pin +
		"server: " + parallelStartServer + "\n"
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write the two-server home's config: %v", err)
	}
	return home
}

// switchToFanOutServer runs `/server <name>` and returns once the session is ready to send again.
//
// The wait is not politeness. A switch UNBINDS the model (ADR 0024): the footer says "connecting…"
// and a send is refused until the first beat off the new server binds one, so a prompt typed before
// that would be answered by a note instead of by the fan-out. The bound model is what says the beat
// landed, and the two servers advertise different ones so it can only be the new server's.
func switchToFanOutServer(t *testing.T, drv *tuitest.Driver, fanOut *stubllm.Server) {
	t.Helper()

	submit(drv, "/server "+parallelFanOutServer)
	drv.WaitText(fanOut.Model)
}

// awaitPane waits until the frame satisfies want, and fails with the frame when it never does. It is
// [tuitest.Driver.WaitFor] over a picture rather than over a counter — the claims here are about what
// stands on screen together, which no single wait for a word can say.
func awaitPane(t *testing.T, drv *tuitest.Driver, what string, want func(tuitest.Frame) bool) {
	t.Helper()

	drv.WaitFor(func() bool { return want(drv.Frame()) }, tuitest.Awaiting(what))
}

// paneNeverShows watches the frame for [serialWatch] and fails the moment it satisfies unwanted — the
// negative [awaitPane], for the one claim that is about something NOT arriving.
func paneNeverShows(t *testing.T, drv *tuitest.Driver, what string, unwanted func(tuitest.Frame) bool) {
	t.Helper()

	deadline := time.Now().Add(serialWatch)
	for time.Now().Before(deadline) {
		if f := drv.Frame(); unwanted(f) {
			t.Fatalf("the frame shows %s:\n%s", what, f)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// frameHas reports whether any row of the frame carries text.
func frameHas(f tuitest.Frame, text string) bool {
	_, _, ok := f.Find(text)
	return ok
}
