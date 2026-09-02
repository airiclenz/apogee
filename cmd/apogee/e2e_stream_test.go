package main

// T-24 of the v0.17.1 release checklist — "a long reply arriving token by token into a real
// terminal" — as tests, plus the T-25 residue that only a streamed reply can show.
//
// The item was manual because the streaming buffer's rewrite (a chunk list, appended in bounded
// copies and joined once at commit) is arithmetic the unit tests already prove, while what nobody
// could see was the picture: text dropped, doubled or reordered on the way to the screen, a resize
// landing mid-stream, a cancel keeping what had arrived, two delegations streaming at once, and
// the flicker of a frame repainted once per token. Each of those is a claim about cells over time,
// which is exactly what internal/tuitest can hold still and internal/stubllm can reproduce.
//
// The scripted upstream does the work a live model used to: testdata/stubllm/stream400.yaml
// streams the checklist's own 400-line numbered list three runes at a time, a millisecond apart,
// so every one of these tests watches the same reply arrive at the same speed.

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/judge"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// streamLines is how many lines the fixture's reply carries — the checklist's own 400.
//
// The reply is described here rather than parsed out of the fixture: every line names its own
// number twice, so a test asserts against strings it built itself, and
// TestE2EStreamFixtureIsTheListItClaims pins that the fixture really streams them. A fixture edited
// out from under these tests then fails there, once, instead of everywhere at once with an
// unreadable diff.
const streamLines = 400

// streamPrompt is the checklist's own step-2 wording, and the text stream400.yaml matches on.
const streamPrompt = "Write a 400-line numbered list, one short sentence per line, " +
	"numbered 1 to 400 with no gaps."

// midStreamWidth is the width the terminal is squeezed to while the reply is still arriving.
//
// Only the width changes, and that is a deliberate limit rather than a shortcut. Reflow is a claim
// about width, and shrinking the HEIGHT under a live stream walks into a fragility of the emulator
// underneath both drivers: the program is still painting a frame sized for the old terminal, so a
// scroll region it sets straight after (DECSTBM, which bubbletea uses for the scrolling area) can
// name a row past the end of the buffer that has already shrunk, and github.com/charmbracelet/x/vt
// indexes it without clamping. T-25 step 9's own 60×20 resize is asserted in
// TestE2ESmokeInProcess, where the session is idle and nothing is in flight.
const midStreamWidth = 60

// streamLine is line n of the fixture's reply.
func streamLine(n int) string { return fmt.Sprintf("%d. Item %d.", n, n) }

// streamText is the whole reply, as the model streams it and as the transcript must commit it.
func streamText() string {
	var b strings.Builder
	for n := 1; n <= streamLines; n++ {
		b.WriteString(streamLine(n))
		b.WriteByte('\n')
	}
	return b.String()
}

// streamRunes is how many runes of reply have been streamed by the time line n is on screen — the
// denominator of the flicker ceiling, which is a cost PER STREAMED RUNE and not per frame.
func streamRunes(n int) int {
	total := 0
	for i := 1; i <= n; i++ {
		total += len([]rune(streamLine(i))) + 1 // + the newline
	}
	return total
}

// TestE2EStreamCommitsCompleteAndInOrder is T-24 steps 2, 5, 6 and 7: a 400-line reply arrives in
// three-rune deltas, the terminal is resized twice while it is still arriving, and what the
// transcript ends up holding is the reply — every line, once, in order, with no join inside a word.
//
// It asserts on both records of the same text, because they fail differently: the session record
// is what the buffer committed, and the frame is what the renderer then painted. A chunk list
// joined wrongly breaks the first; a viewport that reflowed badly breaks only the second.
func TestE2EStreamCommitsCompleteAndInOrder(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "stream400"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)

	submit(drv, streamPrompt)

	// Mid-stream, with lines still arriving: narrower, then back. The checklist's step 5.
	//
	// The WIDTH is what changes, and the height is deliberately left alone — see midStreamWidth.
	drv.WaitText(streamLine(20))
	drv.Resize(midStreamWidth, e2eSize.H)
	narrow := drv.Frame()
	if narrow.Width() != midStreamWidth {
		t.Fatalf("the frame is %d columns after a mid-stream resize to %d",
			narrow.Width(), midStreamWidth)
	}
	// Nothing left torn and nothing left at the old width: the reflowed picture settles into whole
	// lines of the answer, and the answer keeps arriving into it.
	waitWholeStreamRows(t, drv, "the narrow mid-stream frame")
	waitStreamGrows(t, drv)
	drv.Resize(e2eSize.W, e2eSize.H)
	waitWholeStreamRows(t, drv, "the widened mid-stream frame")
	waitStreamGrows(t, drv)

	// Step 6 — the reply commits. The last line on the wire is the first thing to wait for; the
	// record on disk follows once the Exchange settles.
	drv.WaitText(streamLine(streamLines))
	committed := waitForCommittedReply(t, sess, streamLine(1))
	if committed != strings.TrimRight(streamText(), "\n") {
		t.Errorf("the committed reply is not the reply that was streamed:\n%s",
			firstDifferingLine(strings.TrimRight(streamText(), "\n"), committed))
	}

	// And step 6 again, from the other side: scroll the whole answer back and count the lines the
	// terminal actually painted. This is the half a human did by eye.
	seen := scrollbackNumbers(t, drv)
	assertScrollbackIsWhole(t, seen)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EStreamCancelKeepsWhatArrived is T-24 step 9: Esc mid-reply keeps what had arrived rather
// than dropping it or doubling it, and the next prompt opens a NEW entry instead of continuing the
// cancelled one.
func TestE2EStreamCancelKeepsWhatArrived(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "stream400"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)

	submit(drv, streamPrompt)
	drv.WaitText(streamLine(10))
	drv.Press(tuitest.Esc)
	drv.Press(tuitest.Esc) // esc×2: the first press arms the stop, the second confirms it
	// Back at idle is the signal, read off the prompt box's own hint. The "cancelled" note is
	// written by the very fold that returns to idle — the same boundary — so the hint is the
	// steadier thing to wait on, and where the note LANDS is what the assertions below check.
	drv.WaitText("⌃c quit")
	drv.WaitQuiet(settled)

	// What arrived is still there, and it is what arrived: whole lines of the answer, ascending by
	// one, the last of them allowed to be the delta the cancel landed in the middle of.
	cancelled := drv.Frame()
	assertWholeStreamRows(t, "the frame after the cancel", cancelled)
	kept := map[int]bool{}
	collectStreamRows(t, cancelled, kept)
	if len(kept) == 0 {
		t.Fatalf("the cancel emptied the transcript; what had arrived is gone:\n%s", cancelled)
	}
	highest := 0
	for n := range kept {
		highest = max(highest, n)
	}
	if highest >= streamLines {
		t.Errorf("the transcript holds line %d after a cancel; the reply was supposed to be cut short",
			highest)
	}

	// And the next prompt starts a CLEAN entry rather than continuing the cancelled one, while what
	// had arrived is kept as an entry of its OWN: the record holds two answers afterwards, the
	// partial and then the new reply.
	//
	// Model.foldCancelled (internal/tui/model.go) commits the in-flight buffer before writing the
	// "cancelled" note, so the note stands behind the partial and the next prompt behind the note —
	// on screen and, since the idle save follows, in the session record a resume reads back.
	submit(drv, "Anything else?")
	drv.WaitText("Nothing else to add.")
	drv.WaitFor(func() bool { return len(replyEntries(t, sess)) > 1 },
		tuitest.Awaiting("the cancelled partial and the reply after it to reach the session record"))

	replies := replyEntries(t, sess)
	if len(replies) != 2 {
		t.Fatalf("the record holds %d answers after one cancelled reply and one clean one; want 2: %q",
			len(replies), replies)
	}
	if !strings.Contains(replies[0], "Item 1.") {
		t.Errorf("the first answer is %q; want the partial that had arrived before the cancel",
			replies[0])
	}
	for _, line := range strings.Split(replies[0], "\n") {
		m := streamRowStart.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		n := 0
		if _, err := fmt.Sscanf(m[1], "%d", &n); err == nil && n >= streamLines {
			t.Errorf("the committed partial carries line %d; the reply was supposed to be cut short",
				n)
		}
	}
	if got := strings.TrimSpace(replies[1]); got != "Nothing else to add." {
		t.Errorf("the answer after the cancel is %q; want the new reply, standing alone", got)
	}
	if strings.Contains(replies[1], "Item 1.") {
		t.Error("the reply after the cancel continued the cancelled entry rather than starting a new one")
	}

	// The screen reads in that same order: the kept stream rows, the note below the last of them,
	// and the next prompt below the note. This row order is the defect the commit fixes — the
	// prompt used to render ABOVE a partial that belonged to no entry.
	settledFrame := drv.Frame()
	lastStreamRow, noteRow, promptRow := -1, -1, -1
	for y, row := range settledFrame.Rows() {
		text := rowContent(row)
		switch {
		case streamRowPattern.MatchString(text):
			lastStreamRow = y
		case noteRow < 0 && strings.Contains(text, "cancelled"):
			noteRow = y
		case promptRow < 0 && strings.Contains(text, "Anything else?"):
			promptRow = y
		}
	}
	if lastStreamRow < 0 || noteRow < 0 || promptRow < 0 {
		t.Fatalf("the settled frame is missing one of the three rows (stream %d, note %d, prompt %d):\n%s",
			lastStreamRow, noteRow, promptRow, settledFrame)
	}
	if noteRow < lastStreamRow {
		t.Errorf("the cancelled note is on row %d, above the last kept stream row %d:\n%s",
			noteRow, lastStreamRow, settledFrame)
	}
	if promptRow < noteRow {
		t.Errorf("the next prompt is on row %d, above the cancelled note on row %d:\n%s",
			promptRow, noteRow, settledFrame)
	}
	// The cancel itself is on the record, so a reader of the session knows the answer above it was
	// cut short rather than finished.
	if !slices.ContainsFunc(savedTranscript(t, sess), func(e wireTranscriptEntry) bool {
		return e.Kind == "note" && strings.Contains(e.Text, "cancelled")
	}) {
		t.Error("the session record does not say the exchange was cancelled")
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EDelegationsStreamIntoTheirOwnBlocks is T-24 step 8: two sibling delegations stream at the
// same time and their text stays apart. Each child repeats one marker word fifty times and the
// other child's never, so ownership is a claim about which block a marker landed in rather than
// about which order the two ran in.
func TestE2EDelegationsStreamIntoTheirOwnBlocks(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "delegate2"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)

	submit(drv, "Delegate two summaries, one per half of the workspace.")
	drv.WaitText("Both summaries are in.")
	drv.WaitFor(func() bool { return len(replyEntries(t, sess)) > 0 },
		tuitest.Awaiting("the delegating exchange to reach the session record"))

	entries := savedTranscript(t, sess)
	owners := map[string]map[string]bool{"ALPHAMARK": {}, "BETAMARK": {}}
	for _, e := range entries {
		for marker, runs := range owners {
			if !strings.Contains(e.Text, marker) {
				continue
			}
			if e.Depth == 0 {
				t.Errorf("%s reached a depth-0 entry — a child's text leaked into the main reply:\n%s",
					marker, e.Text)
			}
			runs[e.SpawnCallID] = true
		}
	}
	for marker, runs := range owners {
		if len(runs) == 0 {
			t.Fatalf("%s never reached the transcript; the delegation did not stream", marker)
		}
		if len(runs) > 1 {
			t.Errorf("%s is spread over %d delegation runs; each child owns exactly one", marker, len(runs))
		}
	}
	if alpha, beta := only(owners["ALPHAMARK"]), only(owners["BETAMARK"]); alpha == beta {
		t.Errorf("both children's text is filed under run %q; the two blocks are one", alpha)
	}
	// And no entry holds both: a block that carries one child's word and the other's is the leak
	// this step is about, whichever depth it sits at.
	for _, e := range entries {
		if strings.Contains(e.Text, "ALPHAMARK") && strings.Contains(e.Text, "BETAMARK") {
			t.Errorf("one entry at depth %d holds both children's markers:\n%s", e.Depth, e.Text)
		}
	}

	// And the frame shows what the record describes: two delegations, each under its own name.
	final := drv.Frame().String()
	for _, name := range []string{"alpha", "beta"} {
		if !strings.Contains(final, name) {
			t.Errorf("the transcript does not show the %q delegation:\n%s", name, final)
		}
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// repaintCeilingInProcess is the in-process flicker ceiling: bytes the renderer wrote into the
// driver's screen per rune of reply streamed into it. Measured on this test's first green run
// (2026-08-27, go test -race on Linux/arm64): 8.0 bytes per rune early in the stream and 6.9 late.
// The ceiling is three times that — wide enough to absorb a slower box or a wider frame, and
// narrow enough that a full repaint per delta fails it by more than an order of magnitude: a
// 100×30 frame costs a thousand bytes and more for the three runes that arrived.
//
// It is pinned PER DRIVER. The PTY twin's number is measured through --tui-trace on a terminal in
// raw mode, while this one is measured through an output that maps LF to CR LF (the in-process
// driver is the terminal bubbletea believes it is talking to), so the two are not comparable.
const repaintCeilingInProcess = 24.0

// TestE2EStreamRepaintCeiling is T-24 step 3 — "no flicker, no whole-screen redraw per token, no
// visible stutter that worsens as the reply gets longer" — as a measurement.
//
// Flicker has no cell to assert on, so ratified call 9 fixes the proxy: what the renderer WROTE.
// Two windows are measured rather than one, early and late in the same reply, because "worsens as
// it gets longer" is a claim about the second number and not the first, and a full-screen erase
// mid-stream is counted outright.
func TestE2EStreamRepaintCeiling(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "stream400"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)

	submit(drv, streamPrompt)

	// Three marks along the same stream: the ratio is measured across the two gaps between them,
	// so the start-up frame and the prompt's own repaint are outside both windows.
	early := streamMarkAt(t, drv, sess, 40)
	middle := streamMarkAt(t, drv, sess, 120)
	late := streamMarkAt(t, drv, sess, 220)

	earlyRatio := ratio(early, middle)
	lateRatio := ratio(middle, late)
	t.Logf("in-process repaint cost: %.1f bytes/rune early, %.1f late", earlyRatio, lateRatio)
	if earlyRatio > repaintCeilingInProcess {
		t.Errorf("the renderer wrote %.1f bytes per streamed rune early in the reply, ceiling %.1f",
			earlyRatio, repaintCeilingInProcess)
	}
	if lateRatio > repaintCeilingInProcess {
		t.Errorf("the renderer wrote %.1f bytes per streamed rune late in the reply, ceiling %.1f — "+
			"the cost grows with the reply",
			lateRatio, repaintCeilingInProcess)
	}
	// Whole-screen redraws are counted between the marks and not from the launch: the frame is
	// erased once when the answer starts and the input box gives up its rows, which is a layout
	// change rather than flicker. What T-24 forbids is one per token, and that is what the span
	// between two marks in the middle of the same reply measures.
	if got := late.repaints - early.repaints; got != 0 {
		t.Errorf("the renderer repainted the whole screen %d times mid-reply; want none", got)
	}

	// The measurement is taken; the rest of the reply is not needed. Cancel and wait for idle, so
	// the quit below is a quit rather than one deferred behind a running worker.
	drv.Press(tuitest.Esc)
	drv.Press(tuitest.Esc) // esc×2: the first press arms the stop, the second confirms it
	drv.WaitText("⌃c quit")
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// repaintCeilingPTY is the black-box flicker ceiling, read off the --tui-trace file: bytes painted
// into a real pseudo-terminal per rune of reply streamed. Measured on this test's first green run
// (2026-08-27, go test -race on Linux/arm64): 9.1 over the whole run, start-up and both resizes
// included. The ceiling is three times that, as in process. See repaintCeilingInProcess for why
// the two numbers are pinned separately rather than compared.
const repaintCeilingPTY = 27.0

// TestE2EStreamPTY runs the same 400-line reply through the SHIPPED BINARY in a real
// pseudo-terminal, with a real SIGWINCH landing mid-stream — the resize a driver inside the
// process can only simulate — and reads the flicker measure off the --tui-trace seam.
func TestE2EStreamPTY(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "stream400"))
	sess := launchPTY(t, stub)
	drv := sess.drv

	drv.WaitText("Send a message")
	submit(drv, streamPrompt)

	// A genuine TIOCSWINSZ and the signal that comes with it, while the reply is still arriving.
	drv.WaitText(streamLine(20))
	drv.Resize(midStreamWidth, e2eSize.H)
	waitWholeStreamRows(t, drv, "the narrow mid-stream frame")
	waitStreamGrows(t, drv)
	drv.Resize(e2eSize.W, e2eSize.H)

	// The whole reply, through a real terminal, is slower than the default wait allows: 400 lines
	// three runes at a time is some two seconds of scripted delay before the pty and the child
	// process are paid for at all.
	drv.WaitFor(func() bool { _, _, ok := drv.Frame().Find(streamLine(streamLines)); return ok },
		tuitest.Within(15*time.Second), tuitest.Awaiting("the last line of the reply"))
	drv.WaitQuiet(settled)
	assertWholeStreamRows(t, "the committed frame", drv.Frame())

	// The flicker measure only this driver can take: what was painted into the terminal, per rune
	// of reply. The whole run is in the trace, start-up and both resizes included, so the number is
	// an upper bound on the streaming cost rather than the streaming cost alone.
	painted := float64(sess.TraceBytes()) / float64(streamRunes(streamLines))
	t.Logf("pty repaint cost: %.1f bytes/rune over the whole run", painted)
	if painted > repaintCeilingPTY {
		t.Errorf("the binary painted %.1f bytes per streamed rune, ceiling %.1f", painted, repaintCeilingPTY)
	}
	// Full repaints belong to start-up and the two resizes, not to the tokens between them. The
	// bound is generous and still two orders below one erase per delta.
	const maxTraceRepaints = 32
	if got := sess.TraceFullRepaints(); got > maxTraceRepaints {
		t.Errorf("the trace holds %d full repaints for one reply and two resizes, ceiling %d",
			got, maxTraceRepaints)
	}

	if code := drv.Quit(); code != 0 {
		t.Errorf("apogee exited %d; want 0", code)
	}
}

// TestJudgeStreamFrames puts the half of T-24 no assertion settles — whether the streaming reply
// LOOKS right as it arrives — to the configured judge, with the checklist's own two oracles as the
// rubric. It skips without the gate, exactly as the live tests do, and its verdict is binding.
func TestJudgeStreamFrames(t *testing.T) {
	if !judge.Enabled() {
		judge.Skip(t)
		return
	}

	stub := stubllm.New(t, loadScript(t, "stream400"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)

	submit(drv, streamPrompt)
	artifacts := make([]judge.Artifact, 0, 4)
	for _, at := range []int{30, 120, 260} {
		drv.WaitText(streamLine(at))
		artifacts = append(artifacts, judge.FrameArtifact(
			fmt.Sprintf("the frame while line %d was arriving", at), drv.Frame(), false))
	}
	drv.WaitText(streamLine(streamLines))
	drv.WaitQuiet(settled)
	artifacts = append(artifacts, judge.FrameArtifact("the frame once the reply committed",
		drv.Frame(), false))

	judge.Require(t, t.Context(), judge.Rubric{
		Item:  "T-24",
		Claim: "a 400-line reply streams into the transcript and commits as one readable answer",
		PassWhen: "a 400-line streamed reply commits complete, in order, once; a mid-stream resize " +
			"reflows without artefacts; concurrent delegations keep their text apart; and the " +
			"streaming is no slower or flickerier than it felt on v0.17.1.",
		FailsIf: "any line is missing, repeated or out of order in the committed reply; text is " +
			"torn or duplicated after a resize; streamed text from one delegation lands in another " +
			"block; the frame flickers or slows progressively as the reply grows; or the program " +
			"panics.",
		Extra: []string{
			"The frames are snapshots of the same reply arriving, in order: three while it streamed " +
				"and one after it committed.",
			"Only the last 256 raw lines of an in-flight reply are painted while it streams; the " +
				"earlier frames showing a window into the middle of the list is expected, not a defect.",
			"The frame is 100 columns by 30 rows; a header box, the transcript, the prompt box and " +
				"a footer share it.",
			"Rule ONLY on what these frames show — the streamed reply as it arrived and the answer " +
				"once it committed. The oracles' delegation and resize halves are settled by other " +
				"tests and no frame here is evidence about them.",
		},
	}, artifacts...)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EStreamFixtureIsTheListItClaims pins the fixture against the strings the tests above assert
// on. It is the one place the 400 lines are compared to the file, so an edited fixture fails here
// with a readable message instead of failing four driver tests with an unreadable one.
func TestE2EStreamFixtureIsTheListItClaims(t *testing.T) {
	var turn stubllm.Turn
	for _, candidate := range loadScript(t, "stream400").Turns {
		if candidate.When != nil && strings.HasPrefix(candidate.When.LastMessage, "^Write a 400-line") {
			turn = candidate
		}
	}
	if turn.Text == "" {
		t.Fatal("stream400.yaml has no turn matching the 400-line prompt")
	}
	if turn.Text != streamText() {
		t.Errorf("the fixture's reply is not the numbered list the tests assert on:\n%s",
			firstDifferingLine(streamText(), turn.Text))
	}
	if turn.ChunkRunes != 3 || turn.TokenDelay != time.Millisecond {
		t.Errorf("the fixture streams %d runes every %v; T-24 asks for 3 every 1ms",
			turn.ChunkRunes, turn.TokenDelay)
	}
	// And the prompt the tests send is one the fixture's own matcher answers — the other half of
	// the same pin, and the failure a renamed prompt would otherwise produce as a five-second wait.
	matcher, err := regexp.Compile(turn.When.LastMessage)
	if err != nil {
		t.Fatalf("the fixture's matcher %q does not compile: %v", turn.When.LastMessage, err)
	}
	if !matcher.MatchString(streamPrompt) {
		t.Errorf("the prompt %q does not match the turn that answers it (%q)",
			streamPrompt, turn.When.LastMessage)
	}
}

// loadScript loads one of this package's checked-in stubllm fixtures by name.
func loadScript(t *testing.T, name string) stubllm.Script {
	t.Helper()

	script, err := stubllm.Load(filepath.Join("testdata", "stubllm", name+".yaml"))
	if err != nil {
		t.Fatalf("load the %s script: %v", name, err)
	}
	return script
}

// streamMark is one measurement point along a streaming reply: the driver's byte counter read at
// the moment line n was on screen, paired with how many runes of reply that is. The pair is what
// makes a ratio; a byte count on its own says only that the terminal is busy.
type streamMark struct {
	bytes    int64
	runes    int
	repaints int
}

// streamMarkAt waits for line n of the reply and takes the reading.
func streamMarkAt(t *testing.T, drv *tuitest.Driver, sess *e2eSession, n int) streamMark {
	t.Helper()

	drv.WaitText(streamLine(n))
	return streamMark{bytes: sess.BytesWritten(), runes: streamRunes(n), repaints: sess.FullRepaints()}
}

// ratio is the bytes the renderer wrote per rune of reply that arrived between two marks.
func ratio(from, to streamMark) float64 {
	if to.runes <= from.runes {
		return 0
	}
	return float64(to.bytes-from.bytes) / float64(to.runes-from.runes)
}

// only returns the single key of a one-element set, or "" for any other size. It is how a "these
// all belong to one run" assertion names the run it found.
func only(set map[string]bool) string {
	if len(set) != 1 {
		return ""
	}
	for k := range set {
		return k
	}
	return ""
}

// streamRowPattern matches a painted row of the numbered list — the whole line, ignoring whatever
// gutter the transcript puts in front of it.
var streamRowPattern = regexp.MustCompile(`(\d+)\. Item (\d+)\.$`)

// streamRowStart matches the number a painted row opens with, whole line or not.
var streamRowStart = regexp.MustCompile(`^(\d+)\. `)

// rowContent is one painted row with its padding and the transcript's right-hand rail taken off:
// what the row SAYS. The rail is part of the frame rather than of the text, and a `$`-anchored
// assertion that forgets it never matches anything.
func rowContent(row string) string {
	return strings.TrimSpace(strings.TrimRight(row, " │┃"))
}

// firstBrokenStreamRow returns the first row of f that carries part of the numbered list but is
// not a whole line of it, together with its row number — the shape a torn, clipped or
// double-appended line takes on screen.
//
// The LAST list row is allowed to be a partial line and only a partial: mid-stream that row is the
// delta still arriving, and requiring it to be whole would assert that the reply is not streaming.
// It must still be a PREFIX of the line its own number names — a half-painted line is fine, a
// corrupted one is not.
func firstBrokenStreamRow(f tuitest.Frame) (int, string, bool) {
	type painted struct {
		y    int
		text string
	}
	var rows []painted
	for y, row := range f.Rows() {
		if text := rowContent(row); strings.Contains(text, "Item") {
			rows = append(rows, painted{y: y, text: text})
		}
	}
	for i, r := range rows {
		if m := streamRowPattern.FindStringSubmatch(r.text); m != nil && m[1] == m[2] {
			continue
		}
		if i == len(rows)-1 && isStreamLinePrefix(r.text) {
			continue // the delta still arriving
		}
		return r.y, r.text, true
	}
	return 0, "", false
}

// assertWholeStreamRows fails when the frame paints a broken line of the answer. It is for a
// SETTLED frame: on a live one use [waitWholeStreamRows], because a frame caught between two of
// the renderer's incremental writes can hold a row the next write is about to finish.
func assertWholeStreamRows(t *testing.T, what string, f tuitest.Frame) {
	t.Helper()

	if y, text, broken := firstBrokenStreamRow(f); broken {
		t.Errorf("%s, row %d is not a whole line of the list: %q", what, y, text)
	}
}

// waitWholeStreamRows waits for the frame to hold nothing but whole lines of the answer — the
// live form of the same claim, and the honest one while a reply is still arriving.
//
// A repaint is incremental and takes more than one write, so any single snapshot of a streaming
// terminal may catch a row half-written; what a reflow must not do is LEAVE one. So the assertion
// is that the picture converges, bounded, rather than that it was never briefly torn.
func waitWholeStreamRows(t *testing.T, drv driven, what string) {
	t.Helper()

	drv.WaitFor(func() bool { _, _, broken := firstBrokenStreamRow(drv.Frame()); return !broken },
		tuitest.Awaiting(what+" to settle into whole lines of the answer"))
}

// waitStreamGrows waits for the answer to reach further than it had — that it is still streaming.
func waitStreamGrows(t *testing.T, drv driven) {
	t.Helper()

	was := windowHigh(drv.Frame())
	drv.WaitFor(func() bool { return windowHigh(drv.Frame()) > was },
		tuitest.Awaiting("the reply to keep streaming"))
}

// windowHigh is the highest whole list line a frame paints, and 0 when it paints none.
func windowHigh(f tuitest.Frame) int {
	high := 0
	for _, row := range f.Rows() {
		m := streamRowPattern.FindStringSubmatch(rowContent(row))
		if m == nil || m[1] != m[2] {
			continue
		}
		n := 0
		if _, err := fmt.Sscanf(m[1], "%d", &n); err == nil {
			high = max(high, n)
		}
	}
	return high
}

// isStreamLinePrefix reports whether text is the beginning of the list line its own leading number
// names — what a line half-way through arriving looks like.
func isStreamLinePrefix(text string) bool {
	m := streamRowStart.FindStringSubmatch(text)
	if m == nil {
		return false
	}
	n := 0
	if _, err := fmt.Sscanf(m[1], "%d", &n); err != nil || n < 1 || n > streamLines {
		return false
	}
	return strings.HasPrefix(streamLine(n), text)
}

// scrollbackNumbers walks the transcript from where it stands to the top, a window at a time, and
// returns every list line number it painted on the way. It is the frame-side half of "1 to 400
// with no gaps": a human scrolled back and read; this reads every window and collects.
//
// Each ⇞ waits for the window to have MOVED — for the lowest line number on screen to drop — and
// not merely for the frame to differ. A footer that reprints its clock is a difference; collecting
// on it and pressing again skips a whole window, which is one page of missing numbers.
func scrollbackNumbers(t *testing.T, drv *tuitest.Driver) map[int]bool {
	t.Helper()

	// A ceiling on the walk, not an expectation: 400 lines in a 30-row terminal is about 18
	// windows, and the walk stops on its own when the viewport reaches the top.
	const maxWindows = 120

	seen := map[int]bool{}
	for range maxWindows {
		lowest := collectStreamRows(t, drv.Frame(), seen)
		drv.Press(tuitest.PgUp)
		if !waitForScroll(drv, lowest) {
			collectStreamRows(t, drv.Frame(), seen)
			return seen // the viewport is at the top and stopped moving
		}
	}
	t.Fatalf("the scrollback never reached its top in %d windows", maxWindows)
	return seen
}

// collectStreamRows records every whole list line the frame paints and returns the lowest number
// among them — the window's own position in the answer, and math.MaxInt when it shows none.
//
// It also settles the on-screen half of "nothing out of order, nothing repeated": within one
// window the numbers must ascend by exactly one, top to bottom. A repeated or transposed line is
// visible here and nowhere else, because the record holds text and this holds the picture.
func collectStreamRows(t *testing.T, f tuitest.Frame, seen map[int]bool) int {
	t.Helper()

	previous, lowest := 0, math.MaxInt
	for y, row := range f.Rows() {
		m := streamRowPattern.FindStringSubmatch(rowContent(row))
		if m == nil || m[1] != m[2] {
			continue
		}
		n := 0
		if _, err := fmt.Sscanf(m[1], "%d", &n); err != nil {
			continue
		}
		if previous != 0 && n != previous+1 {
			t.Errorf("the window paints line %d directly under line %d (row %d); the answer is "+
				"out of order or a line is repeated on screen", n, previous, y)
		}
		previous = n
		seen[n] = true
		lowest = min(lowest, n)
	}
	return lowest
}

// windowLow is the lowest whole list line a frame paints, without recording anything — the cheap
// read [waitForScroll] polls.
func windowLow(f tuitest.Frame) int {
	lowest := math.MaxInt
	for _, row := range f.Rows() {
		m := streamRowPattern.FindStringSubmatch(rowContent(row))
		if m == nil || m[1] != m[2] {
			continue
		}
		n := 0
		if _, err := fmt.Sscanf(m[1], "%d", &n); err == nil {
			lowest = min(lowest, n)
		}
	}
	return lowest
}

// waitForScroll reports whether the window moved up — whether the lowest line number on screen
// dropped below what it was — within a bounded wait. A window that never moves is the answer, not
// a failure: it is how a viewport says it is at its top.
//
// It waits on the screen's byte counter before it rebuilds a frame, for the reason the settings
// walk does (e2e_smoke_test.go): a poll loop that lays out every cell every few milliseconds costs
// more than the scroll it is measuring.
func waitForScroll(drv *tuitest.Driver, was int) bool {
	painted := drv.Screen().BytesWritten()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if now := drv.Screen().BytesWritten(); now > painted {
			painted = now
			if windowLow(drv.Frame()) < was {
				return true
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}

// assertScrollbackIsWhole settles what the walk of the committed answer proves: the reply reaches
// from its first line to its last, and no run of it is missing.
//
// It stops short of "all 400 numbers were painted", and deliberately. The transcript pins the
// owning prompt as a STICKY HEADER over the viewport's top rows (layout.md; Model.stickyHeaderSpan),
// so a ⇞ that scrolls a whole window leaves the line beneath that header uncovered — exactly one
// per window here, since this prompt is one line. That is the layout's documented overlay and not
// a lost line, and the record on disk is what settles completeness. What a gap of TWO adjacent
// lines would mean is different in kind: the terminal really did not paint that stretch.
func assertScrollbackIsWhole(t *testing.T, seen map[int]bool) {
	t.Helper()

	if !seen[1] {
		t.Error("the scrollback never reached line 1; the committed answer does not scroll back to its start")
	}
	if !seen[streamLines] {
		t.Errorf("the scrollback never showed line %d; the committed answer does not end where it should",
			streamLines)
	}
	for n := 2; n <= streamLines; n++ {
		if !seen[n] && !seen[n-1] {
			t.Fatalf("lines %d and %d were both missing from the scrollback; a stretch of the "+
				"answer was never painted (%d of %d lines seen)", n-1, n, len(seen), streamLines)
		}
	}
}

// wireTranscriptEntry is the part of a saved transcript entry these tests assert on: what was said,
// how deep the agent that said it was, and which delegation run it belongs to. It mirrors
// internal/tui's own wire shape, read here rather than imported because the package that writes it
// keeps it unexported — the record on disk is the contract, and this is a reader of it.
type wireTranscriptEntry struct {
	Kind        string `json:"kind"`
	Text        string `json:"text"`
	Depth       int    `json:"depth"`
	CallID      string `json:"callID"`
	SpawnCallID string `json:"spawnCallID"`
}

// savedTranscript decodes the scrollback the session record holds — what the run committed, as
// opposed to what the terminal painted.
func savedTranscript(t *testing.T, sess *e2eSession) []wireTranscriptEntry {
	t.Helper()

	store := session.NewStore(filepath.Join(sess.Home(), "sessions"))
	metas, err := store.List()
	if err != nil {
		t.Fatalf("list the session store: %v", err)
	}
	if len(metas) == 0 {
		return nil
	}
	newest := metas[0]
	for _, m := range metas[1:] {
		if m.UpdatedAt.After(newest.UpdatedAt) {
			newest = m
		}
	}
	record, err := store.Load(newest.ID)
	if err != nil {
		t.Fatalf("load session %s: %v", newest.ID, err)
	}
	if len(record.Transcript) == 0 {
		return nil
	}
	var envelope struct {
		Entries []wireTranscriptEntry `json:"entries"`
	}
	if err := json.Unmarshal(record.Transcript, &envelope); err != nil {
		t.Fatalf("decode the transcript of session %s: %v", newest.ID, err)
	}
	return envelope.Entries
}

// replyEntries is every main-agent answer the record holds, in order — the entries a claim about
// "what the reply committed to" is about.
func replyEntries(t *testing.T, sess *e2eSession) []string {
	t.Helper()

	var out []string
	for _, e := range savedTranscript(t, sess) {
		if e.Kind == "assistant" && e.Depth == 0 {
			out = append(out, e.Text)
		}
	}
	return out
}

// waitForCommittedReply waits until the record holds a main-agent answer opening with lead, and
// returns it.
func waitForCommittedReply(t *testing.T, sess *e2eSession, lead string) string {
	t.Helper()

	var found string
	sess.drv.WaitFor(func() bool {
		for _, reply := range replyEntries(t, sess) {
			if strings.HasPrefix(reply, lead) {
				found = reply
				return true
			}
		}
		return false
	}, tuitest.Awaiting("the streamed reply to reach the session record"))
	return found
}

// firstDifferingLine reports where two long texts stop agreeing, with the line around it. A 400-line
// diff nobody reads is worse than the one line that actually went wrong.
func firstDifferingLine(want, got string) string {
	wantLines, gotLines := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := range max(len(wantLines), len(gotLines)) {
		w, g := "", ""
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			return fmt.Sprintf("  line %d\n    want %q\n    got  %q\n  (%d lines wanted, %d got)",
				i+1, w, g, len(wantLines), len(gotLines))
		}
	}
	return fmt.Sprintf("  (%d lines, identical)", len(wantLines))
}
