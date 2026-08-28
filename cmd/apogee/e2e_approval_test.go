package main

// T-10 and T-13 of the v0.17.1 release checklist — the forced look at apogee's own control plane,
// and the latch that keeps a decision key from answering a pane the human has not seen yet — as
// tests.
//
// Both were manual for the same reason in two different shapes. T-10: "the wording and placement of
// the sanctioned-route hint on a live approval pane is human judgment". T-13: "a 100 ms arming latch
// is a timing behaviour ... only a human at a real keyboard can feel that". What a driver can settle
// is everything up to the judgment — that the pane is raised at all, that the `Fix:` line stands on
// its own row under the `Reason:` it answers, that its continuation rows hang under its own indent
// rather than falling flush left, that an early keystroke is swallowed and a deliberate one is not.
// The judgment half that is left is one rubric, at the bottom of this file.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/judge"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The three prompts testdata/stubllm/guard.yaml answers, and the pane text they are asked about.
const (
	controlPrompt    = "Run ls in /tmp and nothing else."
	guardReadPrompt  = "List the apogee home with the terminal tool."
	guardWritePrompt = "Touch a probe file under the apogee home."

	controlReason = "Reason: subprocess execution"
	forcedReason  = "Reason: dangerous-action guard forced approval"
	// forcedFix is the sanctioned-route hint, verbatim from internal/security's rule set. A test
	// that paraphrased it would pass over the day somebody rewrote half of it.
	forcedFix = "Fix: a terminal command naming ~/.apogee needs approval, even for a read; " +
		"list, read or copy from there with the dedicated tools instead (list_dir, " +
		"read_file, grep, find_files, or copy_file's source argument)"
	approvalMarker = "Always allow this session"
)

// narrowSize is the terminal T-10 is read at: 60 columns is where the `Fix:` line has to wrap, and
// its wrapping is half of what the checklist asks a human to look at.
var narrowSize = tuitest.Size{W: 60, H: 30}

// TestE2EApprovalForcesALookAtTheControlPlane walks T-10 steps 2–9: the ordinary subprocess gate as
// a control, the forced look a `~/.apogee` command raises, the hint the model gets back on a deny,
// and the write that RUNS once a human says yes — which is the whole behaviour change from the hard
// refusal this rule used to be.
//
// It runs with --config pointing at a temp home, as every driven run must, and that is not a
// weakening: the rule matches the literal TEXT `~/.apogee` in the command line rather than any path
// the run resolved, so a relocated home changes nothing about what it fires on.
func TestE2EApprovalForcesALookAtTheControlPlane(t *testing.T) {
	home := guardHome(t)
	stub := stubllm.New(t, loadScript(t, "guard"))
	drv := tuitest.NewDriver(t, narrowSize)
	sess := launchTUI(t, drv, stub)

	// Step 2 — the control. An ordinary gate names the mode's own cause and offers no way out of
	// it, because there is nothing to fix: the human chose this rung.
	submit(drv, controlPrompt)
	control := awaitApprovalPane(drv)
	if flat := flatten(control.String()); !strings.Contains(flat, controlReason) {
		t.Errorf("the control pane does not read %q:\n%s", controlReason, control)
	}
	if _, _, ok := control.Find("Fix:"); ok {
		t.Errorf("the control pane carries a Fix: line it has no rule for:\n%s", control)
	}

	// Step 3 — allow it, and let the reply finish.
	decide(drv, "a")
	drv.WaitText("That is what the command had to say.")

	// Step 4 — a READ under the control plane raises the forced pane, with the sanctioned route on
	// the line under the reason it answers.
	submit(drv, guardReadPrompt)
	pane := awaitApprovalPane(drv)
	assertFixFollowsReason(t, pane)
	if !strings.Contains(paneText(pane), flatten(forcedFix)) {
		t.Errorf("the forced pane does not carry the sanctioned-route hint:\n%s", pane)
	}
	assertFixWrapsAsOneBlock(t, pane)
	tuitest.Golden(t, "t10-forced-pane", pane, goldenRedactions(sess)...)

	// Step 7 — deny it, and the same hint reaches the MODEL, appended to the denial. The claim is
	// about what the model was TOLD, so it is made against the request the stub received rather than
	// against the transcript, where the same sentence is clipped inside a collapsed block.
	decide(drv, "d")
	want := "tool call denied by approver — " + strings.TrimPrefix(forcedFix, "Fix: ")
	drv.WaitFor(func() bool { return stubSawMessage(stub, want) },
		tuitest.Awaiting("the denial, with its hint, to reach the model"))

	// Steps 8–9 — a WRITE under the control plane is a look, not a refusal: the same pane, and an
	// informed yes RUNS the command.
	submit(drv, guardWritePrompt)
	write := awaitApprovalPane(drv)
	if flat := flatten(write.String()); !strings.Contains(flat, forcedReason) {
		t.Errorf("the write pane does not read %q:\n%s", forcedReason, write)
	}
	decide(drv, "a")
	probe := filepath.Join(home, ".apogee", "guard-probe.txt")
	drv.WaitFor(func() bool { _, err := os.Stat(probe); return err == nil },
		tuitest.Awaiting("the approved write to run"))

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EApprovalForcedLookSurvivesAutoMode is T-10 step 10: under --mode auto, where an ordinary
// terminal call runs with nobody asked, the guard still raises its pane. A Tier-2 rule is a
// per-call speed bump rather than a property of the rung.
func TestE2EApprovalForcedLookSurvivesAutoMode(t *testing.T) {
	guardHome(t)
	stub := stubllm.New(t, loadScript(t, "guard"))
	// A wide terminal rather than T-10's narrow one: what is under test here is the RUNG, and the
	// footer truncates the workdir cell and drops the mode marker beside it when the row runs out of
	// room — which a temp workspace path under a long test name manages at a hundred columns.
	drv := tuitest.NewDriver(t, tuitest.Size{W: 140, H: 30})
	sess := launchTUI(t, drv, stub, "--mode", "auto")

	// The rung is asserted before the claim about it, so a run that quietly failed to reach auto
	// cannot pass this test by paning for the ordinary reason instead.
	drv.WaitText("Send a message")
	drv.WaitQuiet(settled)
	if footer := footerRow(t, drv.Frame()); !strings.Contains(footer, "⏵⏵ auto") {
		t.Fatalf("the run is not in auto: %q", footer)
	}

	submit(drv, guardReadPrompt)
	pane := awaitApprovalPane(drv)
	if flat := paneText(pane); !strings.Contains(flat, forcedReason) {
		t.Errorf("auto did not raise the forced pane:\n%s", pane)
	}
	// Cancel rather than allow: under auto the allow executes inside the workspace fence, so
	// whether the command then succeeds is confinement's business and not this item's.
	drv.Press(tuitest.Esc)
	drv.WaitGone(approvalMarker)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EApprovalKeysAreArmedAfterPaint is T-13, through the SHIPPED BINARY under a real pty —
// the only driver that can answer step 9's question, because the evidence the checklist asks for is
// the `--tui-trace` file, and the trace wraps the process's own stdout (item 4).
//
// The claim is one sentence in two halves: a decision key that arrives BEFORE the pane could be
// read never answers it, and a deliberate one costs nothing. It is made over four panes raised by
// the SAME prompt, because "the pane was visible" is exactly the thing one lucky run cannot
// establish — and one exchange is told from the next by waiting for the prompt box to go idle,
// never by waiting for reply text the exchange before it already painted.
func TestE2EApprovalKeysAreArmedAfterPaint(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "guard"))
	sess := launchPTY(t, stub)
	drv := sess.drv

	// Steps 2–3 — the key held down through the moment the pane appears. Every byte is written
	// before the pane could have armed, so a pane that is gone afterwards was answered by a
	// keystroke the human never saw a frame for, which is the whole failure this item exists to
	// catch.
	submit(drv, controlPrompt)
	release := holdKey(drv, "a", approvalMarker)
	drv.WaitText(approvalMarker)
	release()
	drv.WaitQuiet(settled)
	if _, _, ok := drv.Frame().Find(approvalMarker); !ok {
		t.Fatalf("the pane was answered by keys that arrived before it painted:\n%s", drv.Frame())
	}

	// Step 4 — and the deliberate press costs nothing. The frame has been settled for longer than
	// the latch, so the very next `a` rules.
	pressAndSettle(t, drv, "a")
	clearPrompt(drv)
	waitIdle(drv)

	// Step 5 — `d` denies, just as promptly.
	raisePane(drv)
	pressAndSettle(t, drv, "d")
	waitIdle(drv)

	// Step 6 — Esc is deliberately OUTSIDE the latch: it is the stop path, and the stop path is
	// never the one made harder to reach. It is pressed the instant the pane's own text appears,
	// which is inside the window every decision letter is still dead in.
	submit(drv, controlPrompt)
	drv.WaitText(approvalMarker)
	drv.Press(tuitest.Esc)
	drv.WaitGone(approvalMarker)
	waitIdle(drv)

	// Step 7 — unclaimed keys go to the transcript underneath and leave the pane standing. What is
	// asserted is that they did not RULE, which is the item's own failure mode; whether the
	// viewport actually moved is not, because this conversation is short enough to fit in it and a
	// scroll that had nowhere to go would fail an assertion about the wrong thing.
	raisePane(drv)
	for _, key := range []tuitest.Key{"j", "k", tuitest.PgUp} {
		drv.Press(key)
	}
	drv.WaitQuiet(settled)
	if _, _, ok := drv.Frame().Find(approvalMarker); !ok {
		t.Errorf("an unclaimed key ruled on the pane:\n%s", drv.Frame())
	}

	// Step 8 — `s` takes the session, and the NEXT identical call runs with no pane at all. The
	// idle wait is half the claim: a pane that appeared would still be waiting to be answered.
	pressAndSettle(t, drv, "s")
	waitIdle(drv)
	submit(drv, controlPrompt)
	waitIdle(drv)
	if _, _, ok := drv.Frame().Find(approvalMarker); ok {
		t.Errorf("the call after an always-allow raised a pane anyway:\n%s", drv.Frame())
	}

	// Step 9 — the evidence. Each of the four panes above was WRITTEN to the terminal before
	// anything removed it, which is what the trace file records and what no frame taken afterwards
	// can say.
	const panes = 4
	if got := strings.Count(paneTrace(t, sess), approvalMarker); got < panes {
		t.Errorf("the trace carries the pane %d times; %d panes were raised", got, panes)
	}
}

// TestJudgeForcedApprovalPaneReadsAsHelp is T-10's steps 5 and 6 — the half the checklist wrote the
// item for. Everything about the pane a cell can settle is settled above; what is left is whether
// the hint reads as help beside a question that is still open, given that it says a command "is
// refused" while the pane is asking whether to run it.
func TestJudgeForcedApprovalPaneReadsAsHelp(t *testing.T) {
	if !judge.Enabled() {
		judge.Skip(t)
		return
	}

	guardHome(t)
	stub := stubllm.New(t, loadScript(t, "guard"))
	drv := tuitest.NewDriver(t, narrowSize)
	sess := launchTUI(t, drv, stub)

	submit(drv, guardReadPrompt)
	pane := awaitApprovalPane(drv)

	judge.Require(t, t.Context(), judge.Rubric{
		Item: "T-10",
		Claim: "the forced-approval pane's Fix: line reads as help to a person deciding, rather " +
			"than as a contradiction of the question the pane is asking",
		PassWhen: "a `~/.apogee` terminal call raises an approval pane in every rung including " +
			"auto — never a bare refusal — the pane carries `Reason: dangerous-action guard " +
			"forced approval` on one line and the sanctioned-route `Fix:` line on the next, " +
			"allowing it runs the command, and denying it returns the same hint to the model.",
		FailsIf: "the call is refused outright with no pane (old behaviour); the pane appears with " +
			"no `Fix:` line; the hint is glued onto the end of the `Reason:` line or buried under " +
			"the arguments block; the hint is clipped or wrapped flush-left; auto runs the call " +
			"without a pane; or allowing it still refuses the write.",
		Extra: []string{
			"This frame is 60 columns wide, which is the narrow case the pane has to survive.",
			"Rule ONLY on the WORDING. The pane's geometry — that the Fix: line is its own row " +
				"directly under the Reason: line, where its wrapped rows begin, and that nothing " +
				"is clipped — is asserted cell by cell in this same test file, and so are the " +
				"auto, allow and deny halves of the oracles above. None of them is yours to rule " +
				"on and this frame is not evidence about them.",
			"The pane is ASKING. Judge whether a hint worded as \"is refused\" reads as help or as " +
				"a contradiction beside an open question — that specific tension is what this item exists for.",
		},
	}, judge.FrameArtifact("the forced-approval pane", pane, false))

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// ----------------------------------------------------------------------------
// Reading an approval pane
// ----------------------------------------------------------------------------

// guardHome points HOME at a throwaway directory that HAS an `.apogee` in it, and returns it. The
// guard reads the command's TEXT, so it fires either way; the home matters only to the one step
// that lets the command RUN, which resolves `~` through the environment the tool layer spawns with.
func guardHome(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".apogee"), 0o700); err != nil {
		t.Fatalf("seed the guard's home: %v", err)
	}
	t.Setenv("HOME", home)
	return home
}

// awaitApprovalPane waits for the approval pane and returns the settled frame it painted. The
// settle is not tidiness here: the decision keys arm one latch after the pane opens, so a frame read
// before the screen went quiet is a frame a caller could not yet answer.
func awaitApprovalPane(drv *tuitest.Driver) tuitest.Frame {
	drv.WaitText(approvalMarker)
	drv.WaitQuiet(settled)
	return drv.Frame()
}

// decide answers an armed approval pane and waits for it to go. The caller must have settled the
// frame first (awaitApprovalPane) — an unarmed letter is SWALLOWED, not queued, so a test that
// typed one too early would wait out its whole timeout on a pane nobody answered.
func decide(drv *tuitest.Driver, key string) {
	drv.Type(key)
	drv.WaitGone(approvalMarker)
}

// promptPlaceholder is what the input box shows in exactly one state: empty, with nothing in
// flight. While a pane is up or a tool is running the same box offers to QUEUE instead, and a box
// with anything typed in it shows what was typed.
const promptPlaceholder = "Send a message…"

// waitIdle blocks until the run is back at that state. It is how one exchange is told from the
// next: every exchange in T-13 sends the SAME line and gets the same reply, so a wait on reply TEXT
// would be satisfied by the exchange before it and race the one under test. It also asserts, in
// passing, that no pane is standing — a pane that appeared would still be waiting to be answered.
func waitIdle(drv driven) { drv.WaitText(promptPlaceholder) }

// raisePane sends the conversation's one prompt and waits for its approval pane to settle. The
// settle is not tidiness: the decision keys arm one latch after the pane opens, so a caller that
// typed one before the screen went quiet would have it swallowed.
func raisePane(drv driven) {
	submit(drv, controlPrompt)
	drv.WaitText(approvalMarker)
	drv.WaitQuiet(settled)
}

// holdKey writes key into the terminal every few milliseconds until marker is on screen, and the
// returned function stops it. It is the driver's reading of the checklist's "hold the `a` key down
// through the moment the approval pane appears": every byte it sends is delivered before the pane
// could have armed, so the pane must survive all of them.
//
// It is bounded twice over — by the marker and by a key count — so a pane that never appears ends
// the pump rather than filling the prompt box for as long as the test runs.
func holdKey(drv driven, key, marker string) func() {
	const gap = 5 * time.Millisecond

	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for range holdKeyMax {
			select {
			case <-done:
				return
			default:
			}
			if _, _, ok := drv.Frame().Find(marker); ok {
				return
			}
			drv.Type(key)
			time.Sleep(gap)
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

// holdKeyMax bounds how many keys [holdKey] sends, and therefore how many backspaces empty the box
// afterwards. One number rather than two: they are the same number.
const holdKeyMax = 12

// clearPrompt empties the input box. A decision key that arrives before its pane exists is typed at
// the PROMPT — which is the correct place for a keystroke nothing else has claimed — and the next
// line submitted would otherwise carry it as a prefix and match no scripted turn. It presses one
// backspace per key the hold could have left behind; the extra ones land on an empty box and do
// nothing, which is cheaper than reading the box back between each.
func clearPrompt(drv driven) {
	for range holdKeyMax + 1 {
		drv.Press(tuitest.Backspace)
	}
}

// pressAndSettle rules on a pane the way a deliberate operator does and asserts what T-13 step 4
// asks: no perceptible wait. The latch is 100 ms and the frame has already been quiet for longer
// than that, so the pane must be gone well inside the ceiling below.
func pressAndSettle(t *testing.T, drv driven, key string) {
	t.Helper()

	// What "no perceptible wait" is worth as a number. Generous against a loaded CI box and still
	// an order below the wait a re-armed latch would cost.
	const promptly = 2 * time.Second

	start := time.Now()
	drv.Type(key)
	drv.WaitGone(approvalMarker)
	if took := time.Since(start); took > promptly {
		t.Errorf("a deliberate %q took %v to answer the pane; want under %v", key, took, promptly)
	}
}

// assertFixFollowsReason pins the geometry the checklist's step 5 asks a human to check: the hint is
// a line of its OWN, directly under the reason it answers, rather than a tail glued onto that
// reason or a note below the arguments block.
func assertFixFollowsReason(t *testing.T, f tuitest.Frame) {
	t.Helper()

	reason := rowIndexContaining(t, f, forcedReason)
	fix := rowIndexContaining(t, f, "Fix: ")
	if fix != reason+1 {
		t.Errorf("the Fix: line is on row %d and the Reason: line on row %d; want the very next row:\n%s",
			fix, reason, f)
	}
	if strings.Contains(f.Row(reason), "Fix:") {
		t.Errorf("the hint is glued onto the Reason: line: %q", f.Row(reason))
	}
}

// assertFixWrapsAsOneBlock is the wrapping half of step 5: at a width the sentence cannot fit, it
// wraps as prose and every continuation row starts at the SAME column the hint itself does — the
// pane's body column — so the hint reads as one block rather than as a paragraph that steps about.
// Nothing is clipped: the last row is the sentence's own last word.
//
// The column it checks against is the pane's body column rather than a hang under "Fix: ", because
// that is what this pane does: the approval prompt's body is prose lines the popup wraps to the body
// width, with no per-field hanging indent anywhere in it (approvalPrompt, renderPopup). See the
// dated note under this item in the plan.
func assertFixWrapsAsOneBlock(t *testing.T, f tuitest.Frame) {
	t.Helper()

	fix := rowIndexContaining(t, f, "Fix: ")
	body := textColumn(f.Row(fix))
	rows, last := 0, ""
	for y := fix + 1; y < f.Height(); y++ {
		row := f.Row(y)
		text := strings.TrimSpace(strings.Trim(row, " │"))
		if text == "" || strings.HasSuffix(text, ":") {
			break // a blank row or the next field label: the hint has ended
		}
		if got := textColumn(row); got != body {
			t.Errorf("continuation row %d starts at column %d; the hint's own rows start at %d:\n%s",
				y, got, body, f)
		}
		rows, last = rows+1, text
	}
	if rows == 0 {
		t.Fatalf("the Fix: line did not wrap at %d columns, so there are no continuation rows to check:\n%s",
			f.Width(), f)
	}
	// Not clipped: the sentence ends where the rule's own text ends, ellipsis-free. The final word
	// is the claim — whether it sits alone on the last row is the wrap's business, not the test's.
	if want := forcedFix[strings.LastIndex(forcedFix, " ")+1:]; !strings.HasSuffix(last, want) {
		t.Errorf("the hint's last row is %q; the rule's sentence ends with %q", last, want)
	}
}

// textColumn is the column a pane row's own text starts at, past the border and its padding.
func textColumn(row string) int {
	trimmed := strings.TrimLeft(row, " │")
	return len(row) - len(trimmed)
}

// rowIndexContaining is [rowContaining] when the caller needs the row's POSITION — which is what a
// claim about one line standing under another is made of.
func rowIndexContaining(t *testing.T, f tuitest.Frame, want string) int {
	t.Helper()

	for y, row := range f.Rows() {
		if strings.Contains(row, want) {
			return y
		}
	}
	t.Fatalf("no row of the frame holds %q:\n%s", want, f)
	return -1
}

// paneText is a frame's text with the popup's own borders taken out and every run of whitespace
// collapsed — what a claim about a SENTENCE the pane had to wrap is made against. flatten alone
// would leave the "│" that ends each row inside the sentence, so a hint spanning four rows
// would never match itself.
func paneText(f tuitest.Frame) string {
	rows := f.Rows()
	stripped := make([]string, len(rows))
	for i, row := range rows {
		stripped[i] = strings.Trim(row, " │")
	}
	return flatten(strings.Join(stripped, " "))
}

// stubSawMessage reports whether any request the stub answered carried want in one of its messages —
// how a claim about what the MODEL was told is made, since the model is the stub.
func stubSawMessage(stub *stubllm.Server, want string) bool {
	for _, req := range stub.Requests() {
		for _, msg := range req.Messages {
			if strings.Contains(msg.Content, want) {
				return true
			}
		}
	}
	return false
}

// paneTrace is the pty run's --tui-trace file with its escape sequences taken out, so a count of
// the pane's own label counts panes rather than the styling around them.
func paneTrace(t *testing.T, sess *ptySession) string {
	t.Helper()

	data, err := os.ReadFile(sess.trace)
	if err != nil {
		t.Fatalf("read the trace file: %v", err)
	}
	return ansi.Strip(string(data))
}
