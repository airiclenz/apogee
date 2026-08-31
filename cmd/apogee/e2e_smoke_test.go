package main

// T-25 of the v0.17.1 release checklist — "the one pass a human makes over the most-used path end
// to end" — as a test. It was manual because the TUI had no driver and needed a live model; it now
// has both, in the form of internal/tuitest and internal/stubllm, and what the human was asked to
// look at is what this asserts on: the frame a terminal would have shown.
//
// The black-box half — the terminal left clean on exit, a real SIGWINCH, a real pid — is
// TestE2ESmokePTY's, below. What is here is everything a driver inside the process can see.

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// settled is how long the screen must go without a byte before a frame is read as final. It is the
// same rule both drivers use, and it is short enough that thirteen steps of it stay inside the
// package's wall-clock budget.
const settled = 150 * time.Millisecond

// TestE2ESmokeInProcess walks checklist T-25 steps 1–13 through the real composition: the root
// command, the config resolution, the Agent, the tool layer, the approval gate, the session store
// and the renderer — everything but the terminal, which is the emulator.
func TestE2ESmokeInProcess(t *testing.T) {
	script, err := stubllm.Load("testdata/stubllm/smoke.yaml")
	if err != nil {
		t.Fatalf("load the smoke script: %v", err)
	}
	stub := stubllm.New(t, script)
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)

	// Step 1 — the frame draws: a header, a transcript, a prompt box and a footer, and the footer
	// ends with the mode marker.
	drv.WaitText("Send a message")
	drv.WaitQuiet(settled)
	first := drv.Frame()
	if !strings.Contains(first.Row(0), "╭") {
		t.Errorf("no header box on the first frame; row 0 = %q", first.Row(0))
	}
	footer := footerRow(t, first)
	for _, want := range []string{stub.Model, "probe-target", sess.Workspace()} {
		if !strings.Contains(footer, want) {
			t.Errorf("the footer does not name %q: %q", want, footer)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(footer), "◐ ask before") {
		t.Errorf("the footer does not end with the mode marker: %q", footer)
	}

	// Step 2 — a prompt, a streamed reply, and the tool block the model's call produced.
	submit(drv, "What files are in this workspace?")
	// The block is asserted the way the transcript paints it — a rendered label and an outcome slot
	// carrying the tool's own data — not the wire's tool name, which the transcript never shows.
	drv.WaitText("List")
	drv.WaitText("entries")
	drv.WaitText("The workspace holds one file")

	// Step 3 — a write asks first. The approval pane offers all four decisions.
	submit(drv, `Append a line saying "smoke test" to a.txt.`)
	drv.WaitText("Always allow this session")
	drv.WaitQuiet(settled)
	approval := drv.Frame()
	for _, row := range []string{"Allow", "Always allow this session", "Deny", "Cancel"} {
		if _, _, ok := approval.Find(row); !ok {
			t.Errorf("the approval pane offers no %q row", row)
		}
	}

	// Step 4 — "a" allows it, the write runs, and the file on disk carries the line.
	drv.Type("a")
	drv.WaitText("Appended the smoke test line")
	if got := sess.readWorkspaceFile("a.txt"); !strings.Contains(got, "smoke test") {
		t.Errorf("a.txt = %q after the approved write; want the appended line", got)
	}

	// Step 5 — /settings opens, every section is there, and walking past the last row is safe. The
	// pane is taller than the terminal, so the sections are collected on the way DOWN — which is
	// also step 5's "walk down past the last row and back up" performed rather than described.
	submit(drv, "/settings")
	drv.WaitText("Upstream")
	seen := walkSettings(t, drv)
	for _, section := range []string{
		"Upstream", "Autonomy", "System prompt", "Confinement", "Tools & skills",
		"Session", "Presentation", "Interface", "Mechanisms", "Model profiles",
	} {
		if !seen[section] {
			t.Errorf("the settings pane has no %q section header", section)
		}
	}
	closePane(drv, settingsHint)

	// Step 6 — /usage reports what the run actually spent. The stub's usage numbers are real
	// numbers, so "non-zero" is a claim with something behind it.
	submit(drv, "/usage")
	drv.WaitText("session token usage")
	drv.WaitQuiet(settled)
	usage := drv.Frame()
	if !hasNonZeroNumber(usage) {
		t.Errorf("the /usage view shows no non-zero number:\n%s", usage)
	}
	closePane(drv, "session token usage")

	// Step 7 — /skills reports the catalog a fresh install has, which since `use-shipped-skills:`
	// (default true) is apogee's own shipped set rather than an empty library. The crash markers are
	// spelled as a runtime would spell them: the report now paints skill prose, and a skill whose
	// job is chasing bugs names "panic" and "error" in its own summary and triggers.
	submit(drv, "/skills")
	drv.WaitQuiet(settled)
	skills := drv.Frame().String()
	if !strings.Contains(skills, "/debugging") {
		t.Errorf("/skills did not list the shipped skills a fresh install carries:\n%s", skills)
	}
	if strings.Contains(skills, "panic:") || strings.Contains(skills, "goroutine ") {
		t.Errorf("/skills crashed:\n%s", skills)
	}
	drv.Press(tuitest.Esc)
	drv.WaitQuiet(settled)

	// Step 8 — /version prints the build version, from the same source --version reads.
	submit(drv, "/version")
	drv.WaitText(apogee.Version())

	// Step 9 — narrower and wider: everything reflows, and the footer gives way from the LEFT with
	// an ellipsis rather than clipping the mode marker it ends with.
	drv.Resize(60, 20)
	drv.WaitQuiet(settled)
	narrow := drv.Frame()
	if narrow.Width() != 60 {
		t.Fatalf("the frame is %d columns after a resize to 60", narrow.Width())
	}
	narrowFooter := footerRow(t, narrow)
	if !strings.Contains(narrowFooter, "…") {
		t.Errorf("the narrow footer does not truncate with an ellipsis: %q", narrowFooter)
	}
	// The marker is dropped WHOLE or kept whole; what must never happen is half of it on screen.
	if marker := "◐ ask before"; strings.Contains(narrowFooter, "◐") &&
		!strings.Contains(narrowFooter, marker) {
		t.Errorf("the narrow footer clipped the mode marker mid-word: %q", narrowFooter)
	}
	if !strings.Contains(narrowFooter, "probe-target") {
		t.Errorf("the narrow footer dropped the server it is about: %q", narrowFooter)
	}
	drv.Resize(120, 40)
	drv.WaitQuiet(settled)
	wide := drv.Frame()
	if wide.Width() != 120 {
		t.Fatalf("the frame is %d columns after a resize to 120", wide.Width())
	}
	if got := footerRow(t, wide); strings.Contains(got, "…") {
		t.Errorf("the wide footer is still truncated: %q", got)
	}

	// Step 10 — Ctrl+C twice ends it, cleanly. (The terminal-left-clean half is the PTY test's.)
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
	records := sess.sessionRecords()
	if len(records) == 0 {
		t.Fatal("the run saved no session record")
	}

	// Step 11 — reopen on the same home: /sessions lists what was just run, with its hint line.
	next := sess.Relaunch()
	next.WaitText("Send a message")
	submit(next, "/sessions")
	next.WaitText("type to filter · ↑/↓ select · ⏎ resume")
	next.WaitQuiet(settled)
	next.Press(tuitest.Enter)

	// Step 12 — the restored transcript is the one that was saved: both prompts, both replies, and
	// the tool blocks with their outcomes.
	// A taller viewport before the first read. The saved transcript now also carries step 7's
	// /skills report, which a fresh install fills with the shipped set, and 30 rows show only its
	// tail — the earliest prompt scrolls off. The claim here is about what was RESTORED, not about
	// what happens to fit on one screen.
	next.Resize(e2eSize.W, 40)
	next.WaitQuiet(settled)
	next.WaitText("What files are in this workspace?")
	restored := next.Frame().String()
	for _, want := range []string{
		"What files are in this workspace?",
		"Append a line saying",
		"entries",
		"Write",
	} {
		if !strings.Contains(restored, want) {
			t.Errorf("the restored transcript is missing %q:\n%s", want, restored)
		}
	}

	// Step 13 — one more prompt in the restored session: it is answered, and the record grows.
	before := recordSize(t, sess)
	submit(next, "Is there anything else worth knowing?")
	next.WaitText("Nothing else")
	next.WaitFor(func() bool { return recordSize(t, sess) > before },
		tuitest.Awaiting("the session record on disk to grow"))

	if err := sess.Quit(); err != nil {
		t.Fatalf("the restored run returned %v; want a clean quit", err)
	}
	stub.AssertConsumed(t)
}

// TestE2ESmokePTY walks T-25 again — the SHIPPED BINARY this time, in a real pseudo-terminal —
// and adds the claims no driver inside the process can make: a resize that is a genuine SIGWINCH,
// an exit status from a real pid, and the state the terminal is LEFT in once apogee is gone. Step
// 10's "the shell prompt is intact and `stty sane` should not be necessary" is the whole reason
// this test exists: it is a property of the terminal, not of any frame.
//
// It is deliberately shorter than the in-process walk. Every pane, every verb and the whole
// save/restore path are asserted there, against the same composition this binary is built from;
// repeating them here would buy a second copy of the same evidence at the price of the package's
// wall-clock budget.
func TestE2ESmokePTY(t *testing.T) {
	script, err := stubllm.Load("testdata/stubllm/smoke.yaml")
	if err != nil {
		t.Fatalf("load the smoke script: %v", err)
	}
	stub := stubllm.New(t, script)
	sess := launchPTY(t, stub)
	drv := sess.drv

	// Step 1 — the binary draws its frame in a terminal it negotiated for itself.
	drv.WaitText("Send a message")
	drv.WaitQuiet(settled)
	footer := footerRow(t, drv.Frame())
	for _, want := range []string{stub.Model, "probe-target"} {
		if !strings.Contains(footer, want) {
			t.Errorf("the footer does not name %q: %q", want, footer)
		}
	}

	// Step 2 — a prompt, the tool call it produces, and the streamed reply after it.
	submit(drv, "What files are in this workspace?")
	drv.WaitText("entries")
	drv.WaitText("The workspace holds one file")

	// Steps 3 and 4 — the write asks first, "a" allows it, and the file on disk carries the line.
	submit(drv, `Append a line saying "smoke test" to a.txt.`)
	drv.WaitText("Always allow this session")
	// The decision keys are dead for the first 100 ms the pane is on screen (approvalArmDelay), so
	// that a keystroke already in flight cannot answer a question the human has not read yet. A
	// driver types faster than a human and has to wait for the arm the same way.
	drv.WaitQuiet(settled)
	drv.Type("a")
	drv.WaitText("Appended the smoke test line")
	if got := sess.readWorkspaceFile("a.txt"); !strings.Contains(got, "smoke test") {
		t.Errorf("a.txt = %q after the approved write; want the appended line", got)
	}

	// Step 8 — /version, from the binary's own embedded VERSION. The base version rather than the
	// full one: build provenance depends on how the binary was built, and this one was built by
	// TestMain rather than by make.
	submit(drv, "/version")
	drv.WaitText(apogee.BaseVersion())

	// Step 9 — a real resize: TIOCSWINSZ on the pty and the SIGWINCH that comes with it. The frame
	// reflows and the footer gives way from the left with an ellipsis.
	drv.Resize(60, 20)
	drv.WaitQuiet(settled)
	narrow := drv.Frame()
	if narrow.Width() != 60 {
		t.Fatalf("the frame is %d columns after a SIGWINCH to 60", narrow.Width())
	}
	if got := footerRow(t, narrow); !strings.Contains(got, "…") {
		t.Errorf("the narrow footer does not truncate with an ellipsis: %q", got)
	}

	// Step 10 — Ctrl+C twice, and then the terminal itself is the assertion.
	if code := drv.Quit(); code != 0 {
		t.Errorf("apogee exited %d; want 0", code)
	}
	raw := string(drv.Bytes())
	if got := lastOf(raw, "\x1b[?1049h", "\x1b[?1049l"); got != "\x1b[?1049l" {
		t.Errorf("the last alternate-screen sequence was %q; apogee did not hand the primary "+
			"screen back", got)
	}
	if got := lastOf(raw, "\x1b[?25l", "\x1b[?25h"); got != "\x1b[?25h" {
		t.Errorf("the last cursor sequence was %q; the terminal is left with an invisible cursor",
			got)
	}
	if got := lastSGR(raw); got != "\x1b[0m" && got != "\x1b[m" {
		t.Errorf("the last SGR sequence was %q; the terminal is left with stuck colours", got)
	}
	// And the line discipline the shell would come back to: echo on, canonical input on. This is
	// "no `stty sane` needed", read off the pty rather than judged by eye.
	if echo, canonical := drv.TTYState(); !echo || !canonical {
		t.Errorf("the terminal was left with echo=%v canonical=%v; want both restored",
			echo, canonical)
	}

	// The --tui-trace seam, which only this driver can exercise: the file holds what was painted,
	// and replaying it gives back the same two counters the in-process driver reads off its screen.
	if got := sess.TraceBytes(); got == 0 {
		t.Error("the --tui-trace file recorded no bytes; the trace seam painted nothing")
	}
	if got := sess.TraceFullRepaints(); got == 0 {
		t.Error("the --tui-trace file recorded no full repaint; not even the first frame")
	}
}

// lastOf returns whichever of a and b appears later in raw, or "" when neither does. It is how a
// paired terminal sequence — enter/leave, hide/show — is asserted on honestly: what matters is not
// that the release was sent but that nothing re-took the screen after it.
func lastOf(raw, a, b string) string {
	ia, ib := strings.LastIndex(raw, a), strings.LastIndex(raw, b)
	switch {
	case ia < 0 && ib < 0:
		return ""
	case ib > ia:
		return b
	default:
		return a
	}
}

// sgrPattern matches one SGR (colour and attribute) sequence.
var sgrPattern = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

// lastSGR is the final colour instruction the terminal was given — the one still in force once the
// program is gone. Anything but a reset is a terminal left painted.
func lastSGR(raw string) string {
	all := sgrPattern.FindAllString(raw, -1)
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1]
}

// settingsHint is the settings pane's own footer line — what a test waits to LEAVE the screen to
// know the pane is really closed, since the section headers scroll and cannot say.
const settingsHint = "↑/↓ select · ⏎ edit"

// walkSettings walks the settings pane from its first row to past its last and back, collecting
// every row it saw on the way. A pane taller than the terminal can only be asserted on this way,
// and walking off both ends is step 5's own claim: the pane must survive it.
//
// Each press waits for the SELECTION to move rather than for a fixed interval, so the walk runs at
// the speed the pane answers and ends when the pane stops moving — which is what "past the last
// row" means from outside.
func walkSettings(t *testing.T, drv *tuitest.Driver) map[string]bool {
	t.Helper()

	seen := map[string]bool{}
	collect := func() {
		for _, row := range drv.Frame().Rows() {
			seen[strings.TrimSpace(strings.Trim(row, "│┃ "))] = true
		}
	}
	// A ceiling on the walk, not an expectation: the registry decides how many rows there are.
	const maxRows = 200

	collect()
	start := settingsCursor(drv)
	presses := 0
	for range maxRows {
		if !stepSettings(drv, tuitest.Down) {
			break // the pane clamps at its last row
		}
		presses++
		collect()
		if settingsCursor(drv) == start {
			break // the pane wraps, and the walk is one whole lap
		}
	}
	if presses == 0 {
		t.Fatal("the settings selection never moved on ↓")
	}
	if presses == maxRows {
		t.Fatalf("the settings selection never came back to %q after %d ↓ presses", start, maxRows)
	}
	// Off the end and back: the pane is still up, still painting, still on a real row.
	if _, _, ok := drv.Frame().Find(settingsHint); !ok {
		t.Errorf("the settings pane is gone after walking past its last row:\n%s", drv.Frame())
	}
	// And back up. A few rows is the whole of what ↑ has to prove once ↓ has walked a lap: the
	// selection moves the other way and the rows behind it are the rows that were there. Walking
	// the whole list back costs another second of the package's budget and proves nothing more.
	back := min(presses, 5)
	for range back {
		if !stepSettings(drv, tuitest.Up) {
			t.Error("the settings selection stopped moving on ↑")
			break
		}
		collect()
	}
	return seen
}

// stepSettings presses key and reports whether the pane's selection moved. A press that does not
// move it is the end of the list, not a failure.
//
// It waits on the screen's byte counter before it reads a frame. Reading one means rebuilding every
// cell of the terminal, and a poll loop that does that every few milliseconds costs more than the
// pane it is measuring — the counter says "something was painted" for the price of a mutex.
func stepSettings(drv *tuitest.Driver, key tuitest.Key) bool {
	before := settingsCursor(drv)
	painted := drv.Screen().BytesWritten()
	drv.Press(key)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if now := drv.Screen().BytesWritten(); now > painted {
			painted = now
			if settingsCursor(drv) != before {
				return true
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}

// settingsCursor is the selected row of whatever list is on screen — the "❯" the pane marks it
// with, and the text beside it.
func settingsCursor(drv *tuitest.Driver) string {
	for _, row := range drv.Frame().Rows() {
		if i := strings.Index(row, "❯ "); i >= 0 {
			return strings.TrimSpace(strings.Trim(row[i:], "│┃ "))
		}
	}
	return ""
}

// closePane presses Esc and waits for the pane to actually be gone. Pressing Esc and carrying on is
// the classic way a driver test types its next command into a pane that never closed.
func closePane(drv *tuitest.Driver, marker string) {
	drv.Press(tuitest.Esc)
	drv.WaitGone(marker)
	drv.WaitQuiet(settled)
}

// submit types a line into the prompt box and sends it. It takes the driver interface rather than
// one driver, so the same step means the same thing in process and through the pty.
func submit(drv driven, text string) {
	drv.Type(text)
	drv.WaitFor(func() bool { _, _, ok := drv.Frame().Find(promptTail(text)); return ok },
		tuitest.Awaiting("the typed prompt to appear in the prompt box"))
	drv.Press(tuitest.Enter)
}

// promptTail is the part of a typed line a wrapped prompt box is guaranteed to show — its tail,
// since that is where the cursor is. A short line is its own tail.
func promptTail(text string) string {
	const tail = 24
	if len(text) <= tail {
		return text
	}
	return text[len(text)-tail:]
}

// footerRow is the last non-empty row of a frame: the status bar apogee paints at the bottom.
func footerRow(t *testing.T, f tuitest.Frame) string {
	t.Helper()

	for y := f.Height() - 1; y >= 0; y-- {
		row := strings.TrimRight(f.Row(y), " ")
		// The very last row is the scroll rail; the footer is the last row with words in it.
		if strings.ContainsAny(row, "abcdefghijklmnopqrstuvwxyz") {
			return row
		}
	}
	t.Fatalf("no footer row on the frame:\n%s", f)
	return ""
}

// hasNonZeroNumber reports whether the frame shows a number other than zero — the automatable half
// of "numbers present and non-zero".
func hasNonZeroNumber(f tuitest.Frame) bool {
	for _, row := range f.Rows() {
		for i := 0; i < len(row); i++ {
			if row[i] >= '1' && row[i] <= '9' {
				return true
			}
		}
	}
	return false
}

// recordSize is the total size of everything in the session store — the "the session keeps saving"
// half of step 13, measured rather than believed.
func recordSize(t *testing.T, sess *e2eSession) int64 {
	t.Helper()

	var total int64
	for _, entry := range sess.sessionRecords() {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total
}
