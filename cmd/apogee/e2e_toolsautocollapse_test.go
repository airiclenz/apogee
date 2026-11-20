package main

// The settled Tools umbrella's auto-collapse end to end (plan "2026-11-18 - 00", item 9): under the
// DEFAULT config — `ui.tools-auto-collapse: true`, no other `ui:` key — an umbrella whose calls have
// all returned and that a later entry follows folds to its Group summary row whatever its size; one
// the reader has opened something in stays as the reader left it through the next reply; a session
// resumed with `--continue` paints its replayed, settled umbrellas folded; a failed call keeps its
// count on the folded header; and `tools-auto-collapse: false` hands the fold back to the size rule
// and `ui.tools-open`. The collapse reaches past the umbrella too (plan "2026-11-20 - 00", item 6):
// a settled lone call, a settled `✦ Sub-agents (N)` group and a settled `✦ Skill (N)` group each fold
// to one line whose star wears the tools header colour, open on a click, and stay open through the
// next reply. The settledness, the touched exemption and the live apply are pinned in
// internal/tui; only the binary shows that the config default, the composition root's inversion onto
// tui.Options and the transcript's seed hold together.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/testtime"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The prompts toolsautocollapse.yaml answers beyond the survey (whose prompt, wrap-up, header and
// type row are e2e_toolsfold_test.go's, since the survey issues toolsfold.yaml's three calls), and
// the replies the runs are waited on.
const (
	autoCollapseMissingPrompt = "Read the missing file."
	autoCollapseMissingWrapUp = "The file is not there."
	autoCollapseLonePrompt    = "Read the one file."
	autoCollapseLoneWrapUp    = "The one file is read."
	autoCollapseAgentsPrompt  = "Delegate two checks."
	autoCollapseAgentsWrapUp  = "Both checks reported."
	autoCollapseSkillsPrompt  = "Fetch two skills."
	autoCollapseSkillsWrapUp  = "Both skills fetched."
	autoCollapseWingsPrompt   = "Delegate two inspections."
	autoCollapseWingsWrapUp   = "Both wings inspected."
	autoCollapseNextPrompt    = "Anything else?"
	autoCollapseNextReply     = "Nothing more to report."
	// autoCollapseWingsGate is the gate toolsautocollapse.yaml holds the inspections' wrap-up on.
	autoCollapseWingsGate = "wings"
	// autoCollapseFailedHeader is the missing-file umbrella's Group summary: one read that failed
	// and one listing, the failure counted in the summary's outcome half.
	autoCollapseFailedHeader = "✦ Read 1 file, listed 1 dir · 1 failed"
	// autoCollapseOffConfig switches the collapse off at the default threshold, under which the
	// survey's three type rows make a SMALL umbrella.
	autoCollapseOffConfig = "ui:\n  tools-auto-collapse: false\n"
	// autoCollapseOffLargeOpenConfig switches the collapse off with a threshold the survey's three
	// type rows exceed and the shared preference set to open, so the umbrella is LARGE and its fold
	// is `ui.tools-open`'s.
	autoCollapseOffLargeOpenConfig = "ui:\n  tools-fold-over: 2\n  tools-open: true\n  tools-auto-collapse: false\n"
)

// autoCollapseHoldWindow is how long after the held wrap-up request reached the stub the group's
// fold still counts as seen DURING the hold. It is Fixed because what it measures against is fixed:
// the stub refuses a held request after its own unscaled ten seconds (stubllm's awaitLimit), and a
// fold seen after that could be the one the refused request's run end paints rather than the one
// this test is about. It bounds no wait — the wait itself runs on the kit's scaled default — only
// which frames may answer it.
var autoCollapseHoldWindow = testtime.Fixed(9 * time.Second)

// heldWingsRequests is the arrival time of every request the inspections' held wrap-up turn
// answers: a request whose last message is the south wing's delegation result.
func heldWingsRequests(stub *stubllm.Server) []time.Time {
	var held []time.Time
	for _, req := range stub.Requests() {
		if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "tool" &&
			strings.Contains(req.Messages[n-1].Content, "South wing inspected") {
			held = append(held, req.At)
		}
	}
	return held
}

// autoCollapseFailedFolded is the missing-file umbrella folded to its Group summary.
var autoCollapseFailedFolded = umbrellaHeaderMatch{text: autoCollapseFailedHeader, glyph: "▶"}

// autoCollapseTallSize is the window the touched run is driven at: e2eSize's width with ten rows
// more, so the opened umbrella's three type rows, the wrap-up, the second prompt and its reply all
// stand on one screen without scrolling the umbrella's header off the top.
var autoCollapseTallSize = tuitest.Size{W: e2eSize.W, H: e2eSize.H + 10}

// autoCollapseSurvey launches a run of toolsautocollapse.yaml at size under config and drives the
// survey to its wrap-up, returning the session so the caller can go on with it.
func autoCollapseSurvey(t *testing.T, size tuitest.Size, config string) (*tuitest.Driver, *e2eSession) {
	t.Helper()

	stub := stubllm.New(t, loadScript(t, "toolsautocollapse"))
	drv := tuitest.NewDriver(t, size)
	sess := launchTUIConfigured(t, drv, stub, config)

	submit(drv, toolsFoldPrompt)
	drv.WaitText(toolsFoldWrapUp)
	waitIdle(drv)
	return drv, sess
}

// quitCleanly ends the run and fails the test unless it returned cleanly.
func quitCleanly(t *testing.T, sess *e2eSession) {
	t.Helper()

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EToolsAutoCollapseFoldsASettledUmbrella is the default's own claim: the survey's umbrella —
// SMALL at the default threshold, so the size rule alone would stand it open — comes to rest folded
// to `✦ Read 1 file, listed 1 dir, searched 1 pattern ⋯ ▶` once its wrap-up follows it, with no
// type row beneath it.
func TestE2EToolsAutoCollapseFoldsASettledUmbrella(t *testing.T) {
	t.Parallel()

	drv, sess := autoCollapseSurvey(t, e2eSize, "")

	toolsFoldHeaderFolded.await(drv)
	if frame := drv.Frame().String(); strings.Contains(frame, toolsFoldReadRow) {
		t.Errorf("the settled umbrella paints its %q type row:\n%s", toolsFoldReadRow, frame)
	}
	quitCleanly(t, sess)
}

// TestE2EToolsAutoCollapseSparesATouchedUmbrella is the reader's exemption: a click on the settled
// umbrella's folded header opens it onto its rows, and it stays open through the next reply — the
// collapse never folds an umbrella the reader has touched. Opening an auto-collapsed umbrella is
// view state of that umbrella alone, so the home's config.yaml carries no `tools-open` line.
func TestE2EToolsAutoCollapseSparesATouchedUmbrella(t *testing.T) {
	t.Parallel()

	drv, sess := autoCollapseSurvey(t, autoCollapseTallSize, "")
	toolsFoldHeaderFolded.await(drv)
	x, y, ok := toolsFoldHeaderFolded.find(drv.Frame())
	if !ok {
		t.Fatalf("the frame carries no folded %q header to click:\n%s", toolsFoldHeaderFolded, drv.Frame())
	}

	click(drv, x, y)
	toolsFoldHeaderOpen.await(drv)
	drv.WaitText(toolsFoldReadRow)

	submit(drv, autoCollapseNextPrompt)
	drv.WaitText(autoCollapseNextReply)
	waitIdle(drv)

	frame := drv.Frame()
	if !toolsFoldHeaderOpen.in(frame) {
		t.Errorf("the touched umbrella does not stand open as %q after the next reply:\n%s", toolsFoldHeaderOpen, frame)
	}
	if !strings.Contains(frame.String(), toolsFoldReadRow) {
		t.Errorf("the touched umbrella hides its %q type row after the next reply:\n%s", toolsFoldReadRow, frame)
	}
	cfg, err := os.ReadFile(filepath.Join(sess.Home(), "config.yaml"))
	if err != nil {
		t.Fatalf("read the home's config.yaml: %v", err)
	}
	if strings.Contains(string(cfg), "tools-open") {
		t.Errorf("config.yaml carries a `tools-open` line after opening an auto-collapsed umbrella:\n%s", cfg)
	}
	quitCleanly(t, sess)
}

// TestE2EToolsAutoCollapseFoldsAResumedUmbrella is the replay half: the session resumed with
// `--continue` paints its replayed umbrella — every call returned, the wrap-up after it — folded to
// its Group summary, with no type row beneath it.
func TestE2EToolsAutoCollapseFoldsAResumedUmbrella(t *testing.T) {
	t.Parallel()

	drv, sess := autoCollapseSurvey(t, e2eSize, "")
	toolsFoldHeaderFolded.await(drv)
	quitCleanly(t, sess)

	next := sess.RelaunchWith("--continue")
	next.WaitText(toolsFoldWrapUp)
	toolsFoldHeaderFolded.await(next)
	waitIdle(next)

	if frame := next.Frame().String(); strings.Contains(frame, toolsFoldReadRow) {
		t.Errorf("the resumed, settled umbrella paints its %q type row:\n%s", toolsFoldReadRow, frame)
	}
	quitCleanly(t, sess)
}

// TestE2EToolsAutoCollapseCountsAFailure is the folded header's outcome half: an umbrella holding a
// read of a path the workspace lacks folds, once settled, to
// `✦ Read 1 file, listed 1 dir · 1 failed ⋯ ▶` — the collapse hides the failed call's row but never
// its count.
func TestE2EToolsAutoCollapseCountsAFailure(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "toolsautocollapse"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)

	submit(drv, autoCollapseMissingPrompt)
	drv.WaitText(autoCollapseMissingWrapUp)
	autoCollapseFailedFolded.await(drv)
	waitIdle(drv)

	if frame := drv.Frame().String(); strings.Contains(frame, toolsFoldReadRow) {
		t.Errorf("the settled umbrella paints its %q type row:\n%s", toolsFoldReadRow, frame)
	}
	quitCleanly(t, sess)
}

// TestE2EToolsAutoCollapseOffKeepsTheSizeRule is the switch: under `tools-auto-collapse: false` the
// settled umbrella's fold is the size rule's again. At the default threshold the survey's umbrella
// is small and stands open; over `tools-fold-over: 2` it is large and follows `tools-open: true`,
// standing open too — where the default switch would have folded both.
func TestE2EToolsAutoCollapseOffKeepsTheSizeRule(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		config string
	}{
		{name: "a small umbrella stands open", config: autoCollapseOffConfig},
		{name: "a large umbrella follows tools-open", config: autoCollapseOffLargeOpenConfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			drv, sess := autoCollapseSurvey(t, e2eSize, tc.config)

			toolsFoldHeaderOpen.await(drv)
			drv.WaitText(toolsFoldReadRow)
			quitCleanly(t, sess)
		})
	}
}

// settledBlock is one non-umbrella tool block the collapse folds, as a driven run reaches it: the
// prompt toolsautocollapse.yaml answers with the block's calls and the wrap-up that settles it, the
// block's folded line, the header it stands open under once clicked ("" for a lone call, which opens
// onto its own block and wears no header toggle), and a word only its opened rows paint.
type settledBlock struct {
	prompt string
	wrapUp string
	folded umbrellaHeaderMatch
	open   string
	member string
}

// assertFoldsThenStaysOpen drives block's turn under the default config at autoCollapseTallSize, so
// the opened block, the next prompt and its reply all stand on one screen: the settled block comes
// to rest folded to its one line, with none of its rows beneath it and its star in the tools
// header colour; a click on that line opens it onto its rows; and it stays open through the next
// reply, since the reader has touched it.
func assertFoldsThenStaysOpen(t *testing.T, block settledBlock) {
	t.Helper()

	stub := stubllm.New(t, loadScript(t, "toolsautocollapse"))
	drv := tuitest.NewDriver(t, autoCollapseTallSize)
	sess := launchTUI(t, drv, stub)
	submit(drv, block.prompt)
	drv.WaitText(block.wrapUp)
	block.folded.await(drv)
	waitIdle(drv)

	frame := drv.Frame()
	if strings.Contains(frame.String(), block.member) {
		t.Errorf("the settled block paints its %q row beneath %q:\n%s", block.member, block.folded, frame)
	}
	x, y, ok := block.folded.find(frame)
	if !ok {
		t.Fatalf("the frame carries no folded %q line to click:\n%s", block.folded, frame)
	}
	assertToolsStar(t, frame, x, y)

	click(drv, x, y)
	drv.WaitText(block.member)
	submit(drv, autoCollapseNextPrompt)
	drv.WaitText(autoCollapseNextReply)
	waitIdle(drv)

	frame = drv.Frame()
	if block.folded.in(frame) || !strings.Contains(frame.String(), block.member) {
		t.Errorf("the touched block does not stand open on its %q row after the next reply:\n%s", block.member, frame)
	}
	if opened := (umbrellaHeaderMatch{text: block.open, glyph: "▼"}); block.open != "" && !opened.in(frame) {
		t.Errorf("the touched block does not stand open as %q after the next reply:\n%s", opened, frame)
	}
	quitCleanly(t, sess)
}

// assertToolsStar fails the test unless the cell at (x, y) is a tool block's leading ✦ painted in
// the scheme's tools header colour — the tone every tool block's star wears, folded or open.
func assertToolsStar(t *testing.T, f tuitest.Frame, x, y int) {
	t.Helper()

	cell := f.Cell(x, y)
	if cell.Rune != "✦" {
		t.Fatalf("the cell at (%d, %d) is %q; want the block's ✦:\n%s", x, y, cell.Rune, f)
	}
	if want := mustColor(t, scheme.Default().ToolHeader); !tuitest.SameColor(cell.FG, want) {
		t.Errorf("the block's ✦ at (%d, %d) is painted %v; want the tools header colour %v", x, y, cell.FG, want)
	}
}

// TestE2EToolsAutoCollapseFoldsALoneCall is the lone call's fold: a single read, settled by its
// wrap-up, folds to the Group summary an umbrella of one would paint — `✦ Read 1 file ⋯ ▶` — and a
// click opens it onto its own block, which stays open.
func TestE2EToolsAutoCollapseFoldsALoneCall(t *testing.T) {
	t.Parallel()

	assertFoldsThenStaysOpen(t, settledBlock{
		prompt: autoCollapseLonePrompt,
		wrapUp: autoCollapseLoneWrapUp,
		folded: umbrellaHeaderMatch{text: "✦ Read 1 file", glyph: "▶"},
		member: "a.txt",
	})
}

// TestE2EToolsAutoCollapseFoldsASubAgentGroup is the Sub-agents group's fold: two delegations from
// one reply, both reported and the wrap-up after them, fold to `✦ Sub-agents (2) ⋯ ▶`, and a click
// on that header opens it onto its member rows under `▼`, where it stays.
func TestE2EToolsAutoCollapseFoldsASubAgentGroup(t *testing.T) {
	t.Parallel()

	assertFoldsThenStaysOpen(t, settledBlock{
		prompt: autoCollapseAgentsPrompt,
		wrapUp: autoCollapseAgentsWrapUp,
		folded: umbrellaHeaderMatch{text: "✦ Sub-agents (2)", glyph: "▶"},
		open:   "✦ Sub-agents (2)",
		member: "beta",
	})
}

// TestE2EToolsAutoCollapseFoldsASkillGroup is the Skill group's fold: two skill fetches from one
// reply, both answered and the wrap-up after them, fold to `✦ Skill (2) ⋯ ▶`, and a click on that
// header opens it onto its member rows under `▼`, where it stays.
func TestE2EToolsAutoCollapseFoldsASkillGroup(t *testing.T) {
	t.Parallel()

	assertFoldsThenStaysOpen(t, settledBlock{
		prompt: autoCollapseSkillsPrompt,
		wrapUp: autoCollapseSkillsWrapUp,
		folded: umbrellaHeaderMatch{text: "✦ Skill (2)", glyph: "▶"},
		open:   "✦ Skill (2)",
		member: "cut a release",
	})
}

// TestE2EToolsAutoCollapseFoldsAGroupWhenTheNextTurnStarts is the settle's timing: a Sub-agents group
// whose two delegations have reported folds to `✦ Sub-agents (2) ⋯ ▶` as soon as the NEXT Turn
// starts — the Turn that will write the wrap-up — not once that Turn's reply lands. The fixture
// holds the wrap-up request on the `wings` gate, so the folded header is seen while no reply has
// come back, and the group stays folded once the wrap-up is let through.
func TestE2EToolsAutoCollapseFoldsAGroupWhenTheNextTurnStarts(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "toolsautocollapse"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)
	// Registered after the launch so it runs before the session's own teardown: a run that failed
	// with the request still held must not wait out the gate on the way down.
	t.Cleanup(func() { stub.Release(autoCollapseWingsGate) })
	folded := umbrellaHeaderMatch{text: "✦ Sub-agents (2)", glyph: "▶"}

	submit(drv, autoCollapseWingsPrompt)
	// Both children run first, on the kit's scaled default wait: the held request reaching the stub
	// is the moment the next Turn has started.
	drv.WaitFor(func() bool { return len(heldWingsRequests(stub)) > 0 },
		tuitest.Awaiting("the wrap-up request to reach the stub and be held"))
	// The fold must be seen while that one request is still held: no wrap-up on screen, no second
	// attempt at it, and inside the hold window — so the fold a refused request's run end would
	// paint can never answer this wait.
	drv.WaitFor(func() bool {
		held := heldWingsRequests(stub)
		frame := drv.Frame()
		return len(held) == 1 && time.Since(held[0]) < autoCollapseHoldWindow &&
			folded.in(frame) && !strings.Contains(frame.String(), autoCollapseWingsWrapUp)
	}, tuitest.Awaiting(fmt.Sprintf("the %q group header while the wrap-up is held", folded)))

	stub.Release(autoCollapseWingsGate)
	drv.WaitText(autoCollapseWingsWrapUp)
	waitIdle(drv)
	if frame := drv.Frame(); !folded.in(frame) {
		t.Errorf("the group does not stand folded as %q after the wrap-up:\n%s", folded, frame)
	}
	quitCleanly(t, sess)
}
