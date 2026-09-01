package main

// The run view end to end (ADR 0063): a human opens a working delegation, reads what it is doing,
// says something to it, walks back up, and the parent is told the task moved under it.
//
// Every earlier test of this feature drives one seam — the mailbox in internal/agent, the fold in
// internal/tui, the paint rooted at a run. This one drives the whole rope at once, because the
// claim is a sequence rather than a state: what is typed into a box that is addressing a child has
// to reach THAT child's conversation, come back as a row inside THAT run, end THAT run's report
// with a notice, and leave the conversation below it exactly where it was. No unit test spans it.
//
// The session fans out two delegates on purpose. One of them is only there to be a sibling: the
// status line's merged phrase is a claim about two runs at once, and the run view's is a claim
// about the row speaking for the ONE the reader is looking at — neither is observable with a single
// child on the board.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The wordings this run asserts on. Every one of them is an internal constant restated here because
// cmd/apogee cannot import it — the breadcrumb and its hint (internal/tui's subagentblock.go), the
// staged row's label and the box's legend (interject.go, prompteditor.go), the merged status phrase
// (activity.go) and the parent notice (internal/agent's subagent.go). That is the point of
// restating them: they are what apogee promises a human and a parent model, so a rename over there
// has to fail here.
const (
	runViewPrompt = "Send two delegates over this workspace."

	// The two delegates' names, as their sub_agent calls carry them, and the tasks that tell their
	// conversations apart from the parent's in the server's log.
	shortDelegate = "tally"
	longDelegate  = "scout"
	shortTask     = "Take the first half of the survey"
	longTask      = "Take the second half of the survey"

	// What the human types into the run view, what the child answers it with, and the one line the
	// sibling nobody spoke to reports.
	childMessage    = "What have you found so far"
	childAnswer     = "So far a.txt, and it says hello."
	childParkedLine = "a.txt"
	siblingAnswer   = "The first half is done."

	// The tail of a collapsed run's row when its report is NOT one line: the count of the work and
	// how the run ended, with no sentence promoted into the slot.
	countedOutcome = "tool calls · done"

	// The run view's own furniture: the header naming the way back, and the key hint both it and
	// the status line's right slot carry while a view is open.
	runViewCrumb = "← main › " + longDelegate
	runViewHint  = "esc back"

	// The staged row's label while a message is waiting for the child, and the legend the box wears
	// once that child's run is over.
	queuedForChild   = "queued for " + longDelegate
	childFinishedBox = longDelegate + " has finished · " + runViewHint

	// The status line with two delegates live, and the word a phrase that counts the board carries
	// and one that speaks for a single run must not.
	mergedPhrase  = "2 sub-agents · working"
	subAgentsWord = "sub-agents"

	// The read group's header once the child's SECOND read has landed — the marker that says the
	// three-second window this run acts inside has already closed.
	secondRead = "Read (2)"

	// The notice a steered child's result ends with, and the wrap-up that echoes it back onto the
	// screen (the fixture captures it out of the tool result it answers).
	parentNotice = "(the user sent 1 message to this sub-agent while it ran)"
	parentEcho   = "Delegates back — " + parentNotice
)

// TestE2ESubAgentView drives the whole run view in one session: it opens, it speaks for the child
// it is showing, it carries a message into that child's conversation, it hands the parent the
// notice, and esc puts the reader back where they were.
func TestE2ESubAgentView(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "run-view"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIOn(t, drv, stub, runViewHome(t, stub), "")
	waitIdle(drv)
	drv.WaitQuiet(settled)

	submit(drv, runViewPrompt)

	// Scene 2, top level — two children are working and the row says so as ONE sentence. It is the
	// merged phrase or nothing: a row that named one of them would flicker between whichever spoke
	// last, which is the defect this phrase exists to close (ADR 0063, consequences).
	drv.WaitText(mergedPhrase)
	if row := statusRow(t, drv.Frame()); !strings.Contains(row, mergedPhrase) {
		t.Errorf("the merged phrase is not on the status line: %q", row)
	}

	// Scene 1 — ⌥↑ ⏎ on the last delegation opens it as a run view, not as a rail in place. The
	// frame is taken from the WAIT rather than after a settle: a working run animates its status
	// line for as long as it works, so a view of one never goes quiet and the only honest moment to
	// read is the one that satisfied the condition.
	openWorkingRun(drv)
	opened := frameWhen(t, drv, "the run view open on the child's first read", func(f tuitest.Frame) bool {
		return holds(f, runViewCrumb) && holds(f, longTask) && holds(f, childParkedLine) &&
			!holds(f, secondRead)
	})

	crumb := rowContaining(t, opened, runViewCrumb)
	if !strings.HasSuffix(strings.TrimRight(crumb, " "), runViewHint) {
		t.Errorf("the breadcrumb does not carry the key that leaves the view: %q", crumb)
	}
	assertFirstBodyRow(t, opened, runViewCrumb, longTask)
	assertLastBodyRow(t, opened, childParkedLine)
	for _, sibling := range []string{shortDelegate, shortTask} {
		if holds(opened, sibling) {
			t.Errorf("the view of one run shows %q, which belongs to the sibling:\n%s", sibling, opened)
		}
	}

	// Scene 2, in the view — the row now speaks for the run on screen and not for the board: the
	// sibling is still working, and the phrase names neither it nor a count.
	viewed := statusRow(t, opened)
	if !strings.Contains(viewed, longDelegate+" · ") {
		t.Errorf("the status line does not speak for the run on screen: %q", viewed)
	}
	if strings.Contains(viewed, subAgentsWord) {
		t.Errorf("the status line still counts the board while a view is open: %q", viewed)
	}

	// The whole view, byte for byte. Refresh it with
	// `go test ./cmd/apogee -run TestE2ESubAgentView -update`.
	tuitest.Golden(t, "t17-run-view", opened, runViewRedactions(sess)...)

	// Scene 3 — the box addresses the child. The message stages under the label that says where it
	// is going, lands as a row INSIDE the run, and reaches the child's own conversation.
	submit(drv, childMessage)
	drv.WaitText(queuedForChild)
	drv.WaitGone(queuedForChild)
	drv.WaitText(childAnswer)

	answered := frameWhen(t, drv, "the child's answer under the message that asked for it",
		func(f tuitest.Frame) bool { return holds(f, childMessage) && holds(f, childAnswer) })
	assertBelow(t, answered, longTask, childMessage)
	assertBelow(t, answered, childMessage, childAnswer)

	if !childCarries(stub, longTask, childMessage) {
		t.Errorf("no request in %s's own conversation carries the message the human typed", longDelegate)
	}
	if elsewhere := carriedOutsideChild(stub, longTask, childMessage); elsewhere > 0 {
		t.Errorf("%d request(s) outside %s's conversation carry the message; the box addressed the "+
			"view, so nobody else may have heard it", elsewhere, longDelegate)
	}
	// The notice is WAITED for rather than asserted: the child's answer is on the screen the moment
	// the child says it, and the result carrying that answer only reaches the wire once the parent
	// has every delegate's report in hand and asks its next question.
	drv.WaitFor(func() bool { return resultEndsWith(stub, parentNotice) },
		tuitest.Awaiting("the delegation result the parent reads to end with "+parentNotice))

	// Scene 5 — the run is over and the box says so instead of inviting a send it would refuse.
	drv.WaitText(childFinishedBox)

	// Scene 4 — esc walks one level up. The conversation is back, with the delegation wearing the
	// one collapsed row it has outside a view, and the parent has read the notice.
	drv.Press(tuitest.Esc)
	drv.WaitGone(runViewCrumb)
	drv.WaitText(parentEcho)
	waitIdle(drv)
	drv.WaitQuiet(settled)

	// Both delegates wear one collapsed row, and the trade item 3 accepted shows in the pair: the
	// steered child's report is no longer one line — the notice is under it — so its row falls back
	// to the count and the outcome, while the sibling nobody spoke to still promotes the sentence it
	// answered with.
	top := drv.Frame()
	if row := rowContaining(t, top, longDelegate); !strings.Contains(row, countedOutcome) {
		t.Errorf("the steered delegation is not back to its collapsed row: %q", row)
	}
	if row := rowContaining(t, top, shortDelegate); !strings.Contains(row, siblingAnswer) {
		t.Errorf("the unsteered delegation's row lost the one-line report it promotes: %q", row)
	}
	if _, _, ok := top.Find(childAnswer); ok {
		t.Errorf("the child's own run is still painted in the conversation:\n%s", top)
	}
	if row := rowContaining(t, top, parentNotice); !strings.Contains(row, parentEcho) {
		t.Errorf("the parent's reply does not echo the notice it was handed: %q", row)
	}

	// Scene 5's golden — the same view reopened over a finished run, at rest, which is the one
	// frame of this feature a human can sit and look at.
	openLastRun(drv)
	drv.WaitText(runViewCrumb)
	drv.WaitQuiet(settled)
	tuitest.Golden(t, "t18-run-view-finished", drv.Frame(), goldenRedactions(sess)...)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// openWorkingRun opens the last delegation in the transcript as its run view while that delegation
// is still WORKING — [openLastRun]'s keystrokes, ⌥↑ then ⏎, waiting on the view's own header
// instead of on a settled screen.
//
// The difference is the whole reason it exists. openLastRun ends on a quiet screen, which is the
// right wait for a run that is over and the one wait a run that is not can never satisfy: a working
// session animates its status line every frame, so a settle over one only ever ends in the wait's
// timeout. The header is the more direct question anyway — the view is open when the breadcrumb
// naming the way back is on the screen.
func openWorkingRun(drv *tuitest.Driver) {
	painted := drv.Screen().BytesWritten()
	drv.Press(tuitest.AltUp)
	drv.WaitFor(func() bool { return drv.Screen().BytesWritten() > painted },
		tuitest.Awaiting("the block cursor to highlight a block"))
	drv.Press(tuitest.Enter)
	drv.WaitText(runViewCrumb)
}

// runViewHome writes an apogee home whose one server is the stub and whose entry pins two parallel
// agents (ADR 0039 decision 2).
//
// The home is spelled out here rather than appended to [e2eHome] for [launchTUIOn]'s own reason:
// `parallel-agents:` sits INSIDE a `servers:` entry, and a line appended to the end of the file
// cannot reach in there — it is not even valid YAML behind a top-level key. The pin rather than a
// discovered `total_slots` is what the width comes from because a stub upstream serves no `/props`
// to discover one from, so an unpinned session would run its two delegates one at a time and the
// merged status phrase would never be true.
func runViewHome(t *testing.T, stub *stubllm.Server) string {
	t.Helper()

	body := "servers:\n" +
		"  - name: probe-target\n" +
		"    endpoint: " + stub.URL + "\n" +
		"    model: " + stub.Model + "\n" +
		parallelPin +
		"server: probe-target\n"
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write the run view home's config: %v", err)
	}
	return home
}

// runViewRedactions are [goldenRedactions] with the status line's elapsed clock swallowed as well.
//
// A run view of a WORKING child is a frame with a second counter on it, and the golden is of a
// moment rather than of a settled screen. The clock redacts PADDED, for the build version's reason
// (frameRedactions): it sits at the left of a row whose right slot is flush to the edge, so a
// two-digit second would eat one of the spaces between them and red the golden on a diff that
// carries nothing.
func runViewRedactions(sess *e2eSession) []tuitest.Redaction {
	return append(goldenRedactions(sess), tuitest.RedactPadded(runningSlot, "<working>"))
}

// runningSlot matches the status line's whole live left slot — the spinner's braille phase, the run
// and phrase it is spinning for, and the seconds it has counted. All three are facts about WHEN the
// frame was taken rather than about the view, and the slot is redacted whole rather than piecemeal
// because only the whole of it has a stable width: the row is squared to the terminal and its right
// slot is flush to the edge, so what the phrase and the clock do not spend is spent on the spaces
// between them. Padding the token back out to the match (tuitest.RedactPadded) therefore lands the
// same column at every clock width. What the row SAYS is pinned by the assertions above, on cells.
const runningSlot = `[\x{2800}-\x{28FF}]{2} ` + longDelegate + ` · [a-z]+ · (?:\d+m )?\d+s`

// frameWhen waits until the frame satisfies want and hands back the very frame that did. It is how
// a claim is made against a run that is still working: such a run animates its status line, so the
// screen never goes quiet and a frame taken after the wait is a different frame from the one the
// condition saw.
func frameWhen(t *testing.T, drv *tuitest.Driver, what string, want func(tuitest.Frame) bool) tuitest.Frame {
	t.Helper()

	var got tuitest.Frame
	drv.WaitFor(func() bool {
		f := drv.Frame()
		if !want(f) {
			return false
		}
		got = f
		return true
	}, tuitest.Awaiting(what))
	return got
}

// holds reports whether any row of the frame carries text.
func holds(f tuitest.Frame, text string) bool {
	_, _, ok := f.Find(text)
	return ok
}

// statusRow is the frame's status line: the row directly above the prompt box's top border. It is
// found by the box rather than counted from the bottom, because what sits BELOW the box — the
// footer's cells, the bottom rule — is the composition's business and not this test's.
func statusRow(t *testing.T, f tuitest.Frame) string {
	t.Helper()

	rows := f.Rows()
	for y := len(rows) - 1; y > 0; y-- {
		if strings.HasPrefix(rows[y], "╭") {
			return rows[y-1]
		}
	}
	t.Fatalf("no prompt box on the frame:\n%s", f)
	return ""
}

// assertFirstBodyRow fails unless the first non-blank row UNDER the row carrying header holds want.
// It is how "the view opens on the child's task" is asked as a claim about a place rather than
// about the screen: the task is the first thing the run has to say, and a row that merely appears
// somewhere would pass with the parent's conversation still painted above it.
func assertFirstBodyRow(t *testing.T, f tuitest.Frame, header, want string) {
	t.Helper()

	_, y, ok := f.Find(header)
	if !ok {
		t.Fatalf("no row of the frame holds %q:\n%s", header, f)
	}
	rows := f.Rows()
	for i := y + 1; i < len(rows); i++ {
		if strings.TrimSpace(rows[i]) == "" {
			continue
		}
		if !strings.Contains(rows[i], want) {
			t.Errorf("the first row under %q is %q; want it to hold %q", header, rows[i], want)
		}
		return
	}
	t.Errorf("nothing is painted under %q:\n%s", header, f)
}

// idleRuleFromBottom is how far above the frame's bottom edge the session-title rule sits, counting
// the rule itself: the rule, the status line, the prompt box's three rows (two borders around one
// content row), the footer's line and the ▁ hairline under it. That stack is the frame's floor
// (layout.md, "The frame's own floor is eight rows": the gap row, the ▔ hairline, the status line,
// the input box, the footer's single line and the ▁ hairline under it — the rule and everything
// below it being seven of those eight), and cmd/apogee/testdata/frames/t17-run-view.txt records it,
// the rule on row 24 of a 30-row frame.
//
// It is the IDLE one-row frame's offset and only that. stackInputSlot (internal/tui/model.go) seats
// the autocomplete dropdown and the staged-interjection strip between the status line and the box,
// and the box itself grows to maxInputRows, so a future call site made with a staged band up or a
// grown draft would find the rule higher — that is the layout doing its job, not a regression, and
// such a call site needs its own offset rather than a change to this one.
const idleRuleFromBottom = 7

// lastRuleRow returns the row index of the LAST row beginning with ▔ — the session-title rule the
// prompt box sits under, and so the transcript's end — in a frame h rows tall. It scans from the
// BOTTOM because a session title, a body row or a wrapped rule of some pane's own may open with the
// same glyph higher up; only the bottom-most one is the chrome. ok is false when the frame carries
// no such row, and when the one it carries is not where an idle frame draws it: either way the
// transcript's end is not located, and a caller that guessed would silently retarget itself at a
// body row.
func lastRuleRow(rows []string, h int) (int, bool) {
	for y := len(rows) - 1; y >= 0; y-- {
		if !strings.HasPrefix(rows[y], "▔") {
			continue
		}
		return y, y == h-idleRuleFromBottom
	}
	return 0, false
}

// assertLastBodyRow fails unless the last non-blank row of the transcript holds want — the other
// half of "the view opened on the run": it opens FOLLOWING the latest line the child has written
// (ADR 0063 D5), not parked at the top of a conversation the reader would have to scroll.
//
// The transcript ends at the session-title rule the prompt box sits under (layout.md), located from
// the bottom and checked against idleRuleFromBottom — so a title or a body row opening with the
// same glyph, or a layout change that moves the chrome, fails this assertion loudly instead of
// quietly retargeting it at another row.
func assertLastBodyRow(t testing.TB, f tuitest.Frame, want string) {
	t.Helper()

	rows := f.Rows()
	end, ok := lastRuleRow(rows, f.Height())
	if !ok {
		t.Fatalf("no session-title rule %d rows above the bottom of this %d-row frame, so the transcript's end cannot be located:\n%s",
			idleRuleFromBottom, f.Height(), f)
		return
	}
	for i := end - 1; i >= 0; i-- {
		if strings.TrimSpace(rows[i]) == "" {
			continue
		}
		if !strings.Contains(rows[i], want) {
			t.Errorf("the transcript's last row is %q; want the run's latest line, holding %q", rows[i], want)
		}
		return
	}
	t.Errorf("the transcript is empty:\n%s", f)
}

// assertBelow fails unless both texts are on the frame with later strictly under earlier. It is the
// ordering half of every claim here that is about a sequence — a message under the task it steers, an
// answer under the message — which a pair of Finds is the only honest way to ask.
func assertBelow(t *testing.T, f tuitest.Frame, earlier, later string) {
	t.Helper()

	_, top, ok := f.Find(earlier)
	if !ok {
		t.Fatalf("no row of the frame holds %q:\n%s", earlier, f)
	}
	_, bottom, ok := f.Find(later)
	if !ok {
		t.Fatalf("no row of the frame holds %q:\n%s", later, f)
	}
	if bottom <= top {
		t.Errorf("%q is on row %d and %q on row %d; want the second under the first:\n%s",
			earlier, top, later, bottom, f)
	}
}

// childCarries reports whether any request in the conversation of the child handed task carries
// text as a user message — the wire half of "the message reached the child".
func childCarries(stub *stubllm.Server, task, text string) bool {
	for _, req := range stub.Requests() {
		if carriesTask(req, task) && hasUserMessage(req, text) {
			return true
		}
	}
	return false
}

// carriedOutsideChild counts the requests that carry text as a user message and are NOT that
// child's — the negative the scene actually rests on: a box addressing a run view must not also
// have said it to the conversation below.
func carriedOutsideChild(stub *stubllm.Server, task, text string) int {
	n := 0
	for _, req := range stub.Requests() {
		if !carriesTask(req, task) && hasUserMessage(req, text) {
			n++
		}
	}
	return n
}

// carriesTask reports whether a request belongs to the conversation of the child handed task. A
// child's every message carries it and no parent message does: a sub_agent call puts the task in
// the call's ARGUMENTS, which are not a message's content.
func carriesTask(req stubllm.Request, task string) bool {
	for _, msg := range req.Messages {
		if strings.Contains(msg.Content, task) {
			return true
		}
	}
	return false
}

// hasUserMessage reports whether the request carries text in a message the human wrote.
func hasUserMessage(req stubllm.Request, text string) bool {
	for _, msg := range req.Messages {
		if msg.Role == "user" && strings.Contains(msg.Content, text) {
			return true
		}
	}
	return false
}

// resultEndsWith reports whether any tool result the run put on the wire ends with suffix. The
// parent notice is asked of the RESULT the parent actually read rather than of the screen, because
// the trailer's whole job is to tell a model something the human already knows.
func resultEndsWith(stub *stubllm.Server, suffix string) bool {
	for _, req := range stub.Requests() {
		for _, msg := range req.Messages {
			if msg.Role == "tool" && strings.HasSuffix(strings.TrimRight(msg.Content, "\n"), suffix) {
				return true
			}
		}
	}
	return false
}

// TestE2ESubAgentViewRuleAnchor is assertLastBodyRow's own guard, asked of the pure scan the
// assertion rests on. It cannot be asked of the assertion itself: testing.TB is sealed by an
// unexported method, so no recording double can stand in for a *testing.T and catch the Fatalf.
//
// The residual this closes was a silent one — the old scan took the FIRST ▔ row on the frame, so a
// session title or a body row opening with that glyph retargeted every assertion at the wrong end
// of the transcript and still passed. Both halves of that are pinned here: the decoy above the rule
// must not win, and a frame with no rule where the chrome puts one must not resolve at all.
func TestE2ESubAgentViewRuleAnchor(t *testing.T) {
	t.Parallel()

	// An idle 10-row frame: three body rows — one of them a decoy opening with the rule's own
	// glyph — then the chrome, whose rule sits idleRuleFromBottom rows above the bottom.
	const h = 10
	framed := func(body ...string) []string {
		rows := append([]string(nil), body...)
		return append(rows,
			"▔▔▔▔▔ a session ▔▔▔▔▔", // the rule: row h-idleRuleFromBottom
			"  <working>", "╭───╮", "│   │", "╰───╯", "  model ✦ dir", "▁▁▁▁▁")
	}

	for _, tc := range []struct {
		name string
		rows []string
		want int
		ok   bool
	}{
		{
			name: "a body row opening with the glyph does not win",
			rows: framed("the child's first line", "▔ a report the child drew itself", "the latest line"),
			want: 3,
			ok:   true,
		},
		{
			name: "a plain body resolves to the rule",
			rows: framed("one", "two", "three"),
			want: 3,
			ok:   true,
		},
		{
			name: "no rule at all does not resolve",
			rows: []string{"one", "two", "three", "  <working>", "╭───╮", "│   │", "╰───╯", "  model ✦ dir", "▁▁▁▁▁", ""},
			ok:   false,
		},
		{
			name: "a rule off the chrome's offset does not resolve",
			rows: []string{"one", "▔▔▔▔▔ a session ▔▔▔▔▔", "three", "four", "five", "six", "seven", "eight", "nine", "ten"},
			want: 1,
			ok:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := lastRuleRow(tc.rows, h)
			if ok != tc.ok {
				t.Fatalf("lastRuleRow reported ok=%v; want %v (row %d)", ok, tc.ok, got)
			}
			if ok && got != tc.want {
				t.Errorf("lastRuleRow found the rule on row %d; want row %d", got, tc.want)
			}
		})
	}
}
