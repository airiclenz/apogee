package main

// T-03 and T-04 of the v0.17.1 release checklist — "a running delegation reaches the session
// record" and "the delegate-max-steps cap and honest partial results" — as tests.
//
// Both were manual for the same reason: a delegation only misbehaves against a live model that
// keeps asking for tools, and nobody could kill a TUI mid-delegation and reopen it. The scripted
// upstream supplies both halves. testdata/stubllm/delegate-hang.yaml gives a child that does one
// real piece of work and then never answers again, which is what makes "still running" a fact a
// test can hold still; testdata/stubllm/delegate-cap.yaml gives a child that would keep calling
// tools for four turns, so that a cap of three is a cap the run really meets.
//
// Everything about a delegation lives inside its block, and a delegation block is COLLAPSED by
// default (layout.md): the child's cards, its error line and the result handed back to the parent
// are all elided until the block is opened. That is why these tests open it — the keyboard route to
// the click the checklist's own steps describe.

import (
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/judge"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The prompts these tests send, and the wordings they assert on. The wordings are internal
// constants (toolview.go's interruptedSummary, sessions.go's progressSavedNote, agent.go's
// stepCapErrFormat, subagent.go's stepCapResultFormat) restated here because cmd/apogee cannot
// import them — which is the point: they are what the release checklist promises a human will read,
// so a rename over there has to fail here.
const (
	delegatePrompt  = "Delegate a survey of this workspace to a sub-agent."
	plainPrompt     = "Just answer this yourself: what is in the workspace?"
	capPrompt       = "Delegate the survey to a sub-agent."
	raisedCapPrompt = "Delegate it again with a raised cap in the call."
	// childTask is the delegate's own instruction, and it is how a child's requests are told from
	// the parent's: every message of the child's conversation carries it and none of the parent's
	// does.
	childTask       = "Read the workspace files one at a time"
	interruptedCall = "interrupted — the run did not finish"
	progressSaved   = "saved while a delegation was still running"
	resumedNote     = "resumed: "
	stepCapErrLead  = "delegate stopped at its step cap (3 steps)"
	stepCapErrTail  = "raise delegate-max-steps"
	childFinalWords = "The workspace holds a.txt and it says hello."
)

// ----------------------------------------------------------------------------
// T-03 — a delegation that was still running when the program died
// ----------------------------------------------------------------------------

// TestE2EDelegationRecordSurvivesAKill is T-03 steps 2–10 in process: a delegation is opened, a
// record reaches disk while its child is still working, the program is killed outright, and the
// reopened session says what actually happened to the work that was in flight.
//
// The kill is the program's context cancelled — the in-process half of ratified call 14. The other
// half, a real SIGKILL to a real pid, is TestE2EDelegationRecordSurvivesSIGKILL below.
func TestE2EDelegationRecordSurvivesAKill(t *testing.T) {
	t.Run("a delegation still running reopens closed and noted", func(t *testing.T) {
		stub := stubllm.New(t, loadScript(t, "delegate-hang"))
		drv := tuitest.NewDriver(t, e2eSize)
		sess := launchTUI(t, drv, stub)

		// Step 2 — the delegation opens.
		submit(drv, delegatePrompt)
		drv.WaitText("Sub-Agent")

		// Step 3 — a record is on disk WHILE the child is still working. Waiting for the hanging
		// request is what makes "still working" true rather than likely: the fixture's fourth
		// request is the one that is never answered, so from here until the kill the delegation
		// cannot possibly have finished.
		drv.WaitFor(func() bool { return len(stub.Requests()) >= 4 },
			tuitest.Awaiting("the child's second request, which the script never answers"))
		drv.WaitFor(func() bool { return len(sess.sessionRecords()) > 0 },
			tuitest.Awaiting("the progress save to reach the session store"))

		// Step 4 — the program dies where it stands.
		drv.Kill()

		// Steps 5 to 7 — reopen on the same home and restore the killed session from /sessions.
		next := restoreNewestSession(t, sess)

		// Step 8 — every call that was in flight is closed, and closed as what it was.
		flat := flatten(next.Frame().String())
		if !strings.Contains(flat, interruptedCall) {
			t.Errorf("no call in the reopened transcript reads %q:\n%s", interruptedCall, next.Frame())
		}
		// And nothing paints as running. The status line is the one surface that says a tool is in
		// flight — it carries the tool's own present participle (toolActivityVerb) — so a reopened
		// session that still claims to be delegating would say so there.
		for _, verb := range []string{"delegating", "reading"} {
			if strings.Contains(flat, verb) {
				t.Errorf("the reopened session still paints as %q:\n%s", verb, next.Frame())
			}
		}

		// Step 9 — the notes say which session came back and what became of the unfinished work.
		for _, want := range []string{resumedNote, progressSaved} {
			if !strings.Contains(flat, flatten(want)) {
				t.Errorf("the reopened transcript carries no %q note:\n%s", want, next.Frame())
			}
		}

		if err := sess.Quit(); err != nil {
			t.Fatalf("the reopened run returned %v; want a clean quit", err)
		}
	})

	// Step 10's edge: the same shape with nothing delegated. The note that names an unfinished
	// delegation must not appear on a session that never had one — a note that fires on every
	// resume says nothing.
	t.Run("a session with no delegation reopens with the resumed note alone", func(t *testing.T) {
		stub := stubllm.New(t, loadScript(t, "delegate-hang"))
		drv := tuitest.NewDriver(t, e2eSize)
		sess := launchTUI(t, drv, stub)

		submit(drv, plainPrompt)
		drv.WaitText("There is one file in the workspace")
		drv.WaitFor(func() bool { return len(sess.sessionRecords()) > 0 },
			tuitest.Awaiting("the answered exchange to reach the session store"))
		drv.Kill()

		next := restoreNewestSession(t, sess)

		flat := flatten(next.Frame().String())
		if !strings.Contains(flat, resumedNote) {
			t.Errorf("the reopened transcript carries no %q note:\n%s", resumedNote, next.Frame())
		}
		for _, unwanted := range []string{progressSaved, interruptedCall} {
			if strings.Contains(flat, flatten(unwanted)) {
				t.Errorf("a session that never delegated came back saying %q:\n%s", unwanted, next.Frame())
			}
		}

		if err := sess.Quit(); err != nil {
			t.Fatalf("the reopened run returned %v; want a clean quit", err)
		}
	})
}

// TestE2EDelegationRecordSurvivesSIGKILL is T-03 again through the SHIPPED BINARY, killed the way
// step 4 kills it: SIGKILL to a real pid, no teardown, no chance to save anything on the way out.
// That is the claim no in-process driver can make — a cancelled context still unwinds — and it is
// what proves the record on disk was written by the progress save rather than by a tidy exit.
func TestE2EDelegationRecordSurvivesSIGKILL(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "delegate-hang"))
	sess := launchPTY(t, stub)
	drv := sess.drv

	submit(drv, delegatePrompt)
	drv.WaitText("Sub-Agent")
	drv.WaitFor(func() bool { return len(stub.Requests()) >= 4 },
		tuitest.Awaiting("the child's second request, which the script never answers"))
	drv.WaitFor(func() bool { return len(sessionRecordsIn(t, sess.Home())) > 0 },
		tuitest.Awaiting("the progress save to reach the session store"))

	drv.Kill()

	next := sess.Relaunch()
	next.WaitText("Send a message")
	submit(next, "/sessions")
	next.WaitText("⏎ resume")
	next.WaitQuiet(settled)
	next.Press(tuitest.Enter)
	next.WaitText(interruptedCall)
	next.WaitQuiet(settled)

	flat := flatten(next.Frame().String())
	for _, want := range []string{interruptedCall, resumedNote, progressSaved} {
		if !strings.Contains(flat, flatten(want)) {
			t.Errorf("the reopened transcript carries no %q:\n%s", want, next.Frame())
		}
	}
	if code := next.Quit(); code != 0 {
		t.Errorf("the reopened binary exited %d; want 0", code)
	}
}

// ----------------------------------------------------------------------------
// T-04 — the delegate step cap
// ----------------------------------------------------------------------------

// TestE2EDelegationStepCap is T-04 steps 3–10: a child that would keep calling tools is stopped at
// the configured bound, the human is told once and told what raises it, and the parent is handed a
// clearly-labelled partial result rather than a failure.
func TestE2EDelegationStepCap(t *testing.T) {
	t.Run("a capped child stops, says so, and hands back a labelled partial", func(t *testing.T) {
		stub := stubllm.New(t, loadScript(t, "delegate-cap"))
		drv := tuitest.NewDriver(t, e2eSize)
		sess := launchTUIConfigured(t, drv, stub, "delegate-max-steps: 3\n")

		// Steps 3 and 4 — the delegation opens and ends by itself, short of the four turns its
		// script would have run.
		submit(drv, capPrompt)
		drv.WaitText("The delegate handed back what it had.")
		drv.WaitQuiet(settled)

		// The child asked exactly three times. Its requests are the ones carrying its task; the
		// parent's and the title call's do not.
		if got := childRequests(stub, childTask); got != 3 {
			t.Errorf("the child made %d requests; a cap of 3 turns allows 3", got)
		}
		// And it never reached the turn where it would have spoken.
		if strings.Contains(drv.Frame().String(), childFinalWords) {
			t.Errorf("the child ran past its cap and answered:\n%s", drv.Frame())
		}

		// Step 7 — the delegation is NOT painted as a failure. A step cap is a stop, not an error.
		// The row asked about is the head's own, found by the OUTCOME SLOT it ends with: the prompt
		// two rows above names the delegation too, and a search for its name would settle the
		// question against a line nobody was asking about. It is asked of the COLLAPSED row, which is
		// the shape a delegation wears in the conversation now that expanding one opens its run view
		// instead (ADR 0063).
		collapsed := drv.Frame()
		assertNoErrorTone(t, collapsed, "tool calls · done")

		// That row, byte for byte: the one surface of T-04 that is a RENDERING claim rather than a
		// wording one — what the head's slot says, and that the run behind it is elided to the single
		// line the conversation carries. Refresh it with
		// `go test ./cmd/apogee -run TestE2EDelegationStepCap -update`.
		tuitest.Golden(t, "t04-step-cap-block", collapsed, goldenRedactions(sess)...)

		openLastRun(drv)

		// Step 6 — the run view opens on the child's own conversation: the task it was handed, and
		// the work it got through before the cap stopped it.
		opened := drv.Frame()
		if !strings.Contains(flatten(opened.String()), flatten(childTask)) {
			t.Errorf("the run view does not open on the task the child was handed:\n%s", opened)
		}

		// Step 5 — one error line, naming the cap and the key that raises it. It stands at the END
		// of the child's run, past the bottom of a terminal the run overflows, so it is read by
		// scrolling rather than off one frame.
		flat := flatten(scrollTranscript(drv))
		for _, want := range []string{stepCapErrLead + " — returning what it has", stepCapErrTail} {
			if !strings.Contains(flat, flatten(want)) {
				t.Errorf("the transcript does not say %q:\n%s", want, flat)
			}
		}

		// Step 9's edge — a call may lower the bound and never raise it: max_steps: 50 still stops
		// at three, and the marker still names three.
		before := childRequests(stub, childTask)
		submit(drv, raisedCapPrompt)
		drv.WaitFor(func() bool {
			return childRequests(stub, childTask) >= before+3
		}, tuitest.Awaiting("the second delegation's three child turns"))
		drv.WaitQuiet(settled)
		if got := childRequests(stub, childTask); got != before+3 {
			t.Errorf("the second delegation made %d child requests; max_steps: 50 must not raise a "+
				"cap of 3", got-before)
		}

		if err := sess.Quit(); err != nil {
			t.Fatalf("the run returned %v; want a clean quit", err)
		}
	})

	// Step 10's edge — `delegate-max-steps: 0` is the documented spelling of "unbounded": the same
	// child now runs its whole script and ends on its own, with no cap marker anywhere.
	t.Run("an unbounded cap lets the child finish", func(t *testing.T) {
		stub := stubllm.New(t, loadScript(t, "delegate-cap"))
		drv := tuitest.NewDriver(t, e2eSize)
		sess := launchTUIConfigured(t, drv, stub, "delegate-max-steps: 0\n")

		submit(drv, capPrompt)
		drv.WaitText("The delegate handed back what it had.")
		drv.WaitQuiet(settled)

		if got := childRequests(stub, childTask); got != 4 {
			t.Errorf("the child made %d requests; unbounded, its script runs four turns", got)
		}
		openLastRun(drv)
		flat := flatten(scrollTranscript(drv))
		if !strings.Contains(flat, flatten(childFinalWords)) {
			t.Errorf("the uncapped child never got to answer:\n%s", flat)
		}
		for _, unwanted := range []string{"step cap", "delegate-max-steps"} {
			if strings.Contains(flat, unwanted) {
				t.Errorf("an unbounded delegation still mentions %q:\n%s", unwanted, flat)
			}
		}

		if err := sess.Quit(); err != nil {
			t.Fatalf("the run returned %v; want a clean quit", err)
		}
	})
}

// TestJudgeDelegationStepCap puts T-04's own oracles to the judge over the block a human would have
// read. It is the judgement half the checklist called manual — "whether the partial marker reads
// sensibly to a human, which no assertion makes" — and it is binding wherever the gate is set.
func TestJudgeDelegationStepCap(t *testing.T) {
	if !judge.Enabled() {
		judge.Skip(t)
		return
	}

	stub := stubllm.New(t, loadScript(t, "delegate-cap"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIConfigured(t, drv, stub, "delegate-max-steps: 3\n")

	submit(drv, capPrompt)
	drv.WaitText("The delegate handed back what it had.")
	drv.WaitQuiet(settled)
	openLastRun(drv)

	tones := schemeTones()
	judge.Require(t, t.Context(), judge.Rubric{
		Item: "T-04",
		Claim: "the expanded delegation block tells a human the delegate was stopped at its step " +
			"cap and that what came back is partial, without reading as a failure",
		PassWhen: "the capped delegation ends at the configured number of steps, the human sees one " +
			"error line naming the cap and the key that raises it, and the parent receives a " +
			"clearly-labelled partial result rather than a failure or a silent truncation.",
		FailsIf: "the child runs past the cap; the parent's result is marked an error / painted " +
			"red; the marker text is missing so the partial text poses as the answer; " +
			"max_steps: 50 raises the cap above 3; delegate-max-steps: 0 still caps; or the MAIN " +
			"conversation (not a delegate) ever hits a cap.",
		Extra: []string{
			"The configured cap is 3 steps.",
			"Colour is tagged: ⟨error⟩…⟨/error⟩ is the scheme's failure tone. Rule on whether the " +
				"delegation reads as a failure from those tags, not from the wording alone.",
			"Rule ONLY on what this frame shows. The max_steps and unbounded edges are settled by " +
				"other tests and no frame here is evidence about them.",
		},
	}, judge.FrameArtifact("the expanded delegation block", drv.Frame(), true, tones...))

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// ----------------------------------------------------------------------------
// The helpers
// ----------------------------------------------------------------------------

// restoreNewestSession reopens apogee on the same home and restores the top row of /sessions — the
// killed run, since the browser lists newest first and the fresh session has said nothing yet. It
// returns the driver the restored session is being typed into.
func restoreNewestSession(t *testing.T, sess *e2eSession) *tuitest.Driver {
	t.Helper()

	next := sess.Relaunch()
	next.WaitText("Send a message")
	submit(next, "/sessions")
	next.WaitText("⏎ resume")
	next.WaitQuiet(settled)
	next.Press(tuitest.Enter)
	next.WaitText(resumedNote)
	next.WaitQuiet(settled)
	return next
}

// sessionRecordsIn lists the records a home's session store holds. It takes the HOME rather than a
// session so both drivers can ask it: [e2eSession.sessionRecords] is the in-process form and a
// ptySession has no twin. A store that does not exist yet is no records, not an error — a run that
// has saved nothing has an empty store by definition.
func sessionRecordsIn(t *testing.T, home string) []os.DirEntry {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(home, "sessions"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read the session store: %v", err)
	}
	return entries
}

// goldenRedactions are [e2eSession.Redactions] with the footer's workdir cell — and everything the
// width leaves after it — replaced first.
//
// The session's own redactions replace the workspace path where it appears WHOLE, and the footer
// does not show it whole: it truncates the workdir from the left to the room it has, and drops the
// mode marker beside it when there is none. How long a temp path is, and therefore how much of that
// row survives, is a fact about the machine and not about the block this golden is of — so the whole
// tail of the row goes, and a golden recorded on one box reads on another.
//
// It goes FIRST because the session's redactions would otherwise rewrite the very path this one
// anchors on.
func goldenRedactions(sess *e2eSession) []tuitest.Redaction {
	cell := "✦ " + sess.stub.Model + " ✦ "
	return append([]tuitest.Redaction{
		tuitest.Redact(regexp.QuoteMeta(cell)+".*", cell+"<workdir>"),
	}, sess.Redactions()...)
}

// childRequests counts the requests the stub answered that carry task in one of their messages —
// the delegate's own conversation, told apart from the parent's by the task it was given.
func childRequests(stub *stubllm.Server, task string) int {
	n := 0
	for _, req := range stub.Requests() {
		for _, msg := range req.Messages {
			if strings.Contains(msg.Content, task) {
				n++
				break
			}
		}
	}
	return n
}

// assertNoErrorTone fails when the row carrying marker holds any cell painted in the colour
// scheme's error role. It is the automated form of T-04 step 7's "look at how the block is
// painted": design call 11 of the tool-layout work makes that red the ONLY failure marking — no
// glyph, no coloured header — so a row without it is a row that does not read as a failure.
//
// marker names the row, and it is worth choosing one that only the row in question can hold.
func assertNoErrorTone(t *testing.T, f tuitest.Frame, marker string) {
	t.Helper()

	_, y, ok := f.Find(marker)
	if !ok {
		t.Fatalf("no row of the frame holds %q:\n%s", marker, f)
	}
	want := mustColor(t, scheme.Default().Error)
	for _, run := range f.StyleRuns(y) {
		if tuitest.SameColor(run.Style.FG, want) {
			t.Errorf("the delegation's row is painted in the scheme's error tone at column %d (%q):\n%s",
				run.X, run.Text, f)
		}
	}
}

// schemeTones names the colours a judged frame is tagged with, so a rubric can say "painted red"
// and mean the scheme's own failure role rather than any red.
func schemeTones() []judge.Tone {
	s := scheme.Default()
	out := make([]judge.Tone, 0, 3)
	for _, tone := range []struct {
		name string
		hex  string
	}{{"error", s.Error}, {"success", s.Success}, {"warning", s.Warning}} {
		if c, ok := parseHexColor(tone.hex); ok {
			out = append(out, judge.Tone{Name: tone.name, Color: c})
		}
	}
	return out
}

// mustColor parses a scheme's "#rrggbb" value, failing the test when it cannot.
func mustColor(t *testing.T, hex string) color.Color {
	t.Helper()

	c, ok := parseHexColor(hex)
	if !ok {
		t.Fatalf("the colour scheme's value %q is not #rrggbb", hex)
	}
	return c
}

// parseHexColor turns a scheme's "#rrggbb" into a colour. The driver runs the program at a
// true-colour profile, so a role's hex is exactly the RGB the emulator ends up holding and a
// comparison against it is a comparison against the scheme rather than against a rendering.
func parseHexColor(hex string) (color.Color, bool) {
	if len(hex) != 7 || hex[0] != '#' {
		return nil, false
	}
	var rgb [3]uint8
	for i := range rgb {
		v, err := strconv.ParseUint(hex[1+i*2:3+i*2], 16, 8)
		if err != nil {
			return nil, false
		}
		rgb[i] = uint8(v)
	}
	return color.RGBA{R: rgb[0], G: rgb[1], B: rgb[2], A: 0xff}, true
}
