package main

// The pop-up frame goldens: one recorded frame per boxed overlay, so the layout of every pop-up
// can be read, edited by hand and then held. They are rendering surfaces — the whole point of a
// pop-up is how it looks — which is what a golden is for (ADR 0062, ratified call 13); everything
// the panes DO is asserted elsewhere.
//
// Two runs, one per family. The list pop-ups — the `/` dropdown, the picker and the `/sessions`
// browser — need no upstream turn but the title and the fallback. The decision surfaces — the approval pane and
// the ask pane in its four states — are raised by testdata/stubllm/popups.yaml.
//
// The frames on disk are HELD: they began as hand-edited designs of the target layout, that layout
// has landed, and they are now records like every other golden — re-recorded, when a deliberate
// layout change makes them stale, with `go test ./cmd/apogee -run TestE2EPopupFrames -update`.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The prompts popups.yaml answers.
const (
	popupApprovalPrompt = "Run ls in /tmp and nothing else."
	popupSinglePrompt   = "Ask me how to continue."
	popupMultiPrompt    = "Ask me which findings to fix."
	popupFreePrompt     = "Ask me an open question."

	// The hint each ask state paints, which is what tells the pane's states apart on a frame.
	askChoiceHint = "type for a custom answer"
	askFreeHint   = "type your answer below"
	askReply      = "Noted, thank you."

	// The filled checkbox a ticked multi-select row paints — what the pointer's first click leaves
	// on the frame.
	askTicked = "[✔]"

	// The headings the three list pop-ups paint — the one text on each that does not scroll.
	dropdownTitle = "commands and skills"
	pickerTitle   = "schedule — how often"
	browserHint   = "type to filter · ↑/↓ select · ⏎ resume"
)

// popupRedactions is goldenRedactions plus the one age the default set does not cover: a record
// saved seconds ago reads `just now` rather than `N secs ago`, and a slow relaunch would age it.
// The row is re-padded at its END rather than behind the token (RedactPadded's way), because the
// token sits mid-row with more cells after it: padding behind it would shift those cells, while
// the surface itself pads the row out to the border.
func popupRedactions(sess *e2eSession) []tuitest.Redaction {
	age := tuitest.Redaction{
		Pattern: regexp.MustCompile(`(?m)just now(.*)│$`),
		With:    "<age>${1}   │",
	}
	return append([]tuitest.Redaction{age}, goldenRedactions(sess)...)
}

// TestE2EPopupFramesLists records the three list pop-ups: the `/` dropdown, the picker and the
// `/sessions` browser.
func TestE2EPopupFramesLists(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "popups"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)
	waitIdle(drv)
	drv.WaitQuiet(settled)

	// The `/` dropdown.
	drv.Type("/")
	drv.WaitText(dropdownTitle)
	drv.WaitQuiet(settled)
	tuitest.Golden(t, "popup-dropdown", drv.Frame(), goldenRedactions(sess)...)
	drv.Press(tuitest.Backspace)
	drv.WaitGone(dropdownTitle)

	// The picker, through the one verb that opens it under a one-server, no-dial config: the
	// prompt-only /schedule form, whose first popup asks how often. /model notes that the server
	// serves no other model, /server that there is no other server, and /effort that the stub
	// reports no dial.
	submit(drv, "/schedule tidy the logs")
	drv.WaitText(pickerTitle)
	drv.WaitQuiet(settled)
	tuitest.Golden(t, "popup-picker", drv.Frame(), goldenRedactions(sess)...)
	drv.Press(tuitest.Esc)
	drv.WaitGone(pickerTitle)

	// The `/sessions` browser lists saved records, so one exchange is run and saved first and the
	// browser is opened on a relaunch over the same home — the way a human reaches it.
	submit(drv, "Hello there.")
	drv.WaitText("Nothing else to add.")
	if err := sess.Quit(); err != nil {
		t.Fatalf("the first run returned %v; want a clean quit", err)
	}
	next := sess.Relaunch()
	waitIdle(next)
	submit(next, "/sessions")
	next.WaitText(browserHint)
	next.WaitQuiet(settled)
	tuitest.Golden(t, "popup-sessions", next.Frame(), popupRedactions(sess)...)
	next.Press(tuitest.Esc)
	next.WaitGone(browserHint)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EPopupClickAsk drives the ask pane with the POINTER, through the bytes a terminal in SGR
// mouse mode sends (tuitest.Click / tuitest.Release): a click on an offered answer ticks it, and a
// second click on that same row sends it. It is the one e2e click on this pane family — the
// semantics are asserted at the reducer (internal/tui/mouse_test.go); what only a real program can
// show is the report reaching the model at the cell the frame drew.
//
// The multi-select question is the one driven because its first click leaves a mark on the screen:
// the box on the clicked row fills, so the highlight-then-send rule is readable in the frame rather
// than only in the answer.
func TestE2EPopupClickAsk(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "popups"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)
	waitIdle(drv)

	submit(drv, popupMultiPrompt)
	drv.WaitText(askChoiceHint)
	drv.WaitQuiet(settled)

	const secondFinding = "Add the missing layout() call"
	x, y, ok := drv.Frame().Find(secondFinding)
	if !ok {
		t.Fatalf("the pane does not paint %q:\n%s", secondFinding, drv.Frame())
	}

	// The first click: the row is highlighted and its box ticked, and the question stays up.
	drv.Press(tuitest.Click(x, y))
	drv.Press(tuitest.Release(x, y))
	drv.WaitFor(func() bool { return strings.Contains(drv.Frame().Row(y), askTicked) },
		tuitest.Awaiting("the clicked row's box to fill"))
	drv.WaitQuiet(settled)
	if !strings.Contains(drv.Frame().String(), askChoiceHint) {
		t.Fatalf("the first click answered the question:\n%s", drv.Frame())
	}

	// The second click on the same row is the ⏎: the ticked answer reaches the blocked tool and the
	// model's wrap-up comes back.
	drv.Press(tuitest.Click(x, y))
	drv.Press(tuitest.Release(x, y))
	drv.WaitText(askReply)
	drv.WaitGone(askChoiceHint)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EPopupFramesPrompts records the two decision surfaces: the approval pane, and the ask pane
// single-select, multi-select with one box ticked, single-select with a custom answer typed, and
// free-text.
func TestE2EPopupFramesPrompts(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "popups"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)
	waitIdle(drv)

	// The approval pane.
	submit(drv, popupApprovalPrompt)
	pane := awaitApprovalPane(drv)
	tuitest.Golden(t, "popup-approval", pane, goldenRedactions(sess)...)
	decide(drv, "a")
	drv.WaitText("That is what the command had to say.")

	// Single-select, the first choice highlighted.
	submit(drv, popupSinglePrompt)
	drv.WaitText(askChoiceHint)
	drv.WaitQuiet(settled)
	tuitest.Golden(t, "popup-ask-single", drv.Frame(), goldenRedactions(sess)...)
	drv.Press(tuitest.Enter)
	drv.WaitText(askReply)

	// Multi-select, the first box ticked.
	submit(drv, popupMultiPrompt)
	drv.WaitText(askChoiceHint)
	drv.WaitQuiet(settled)
	drv.Press(tuitest.Space)
	drv.WaitText("[✔]")
	drv.WaitQuiet(settled)
	tuitest.Golden(t, "popup-ask-multi", drv.Frame(), goldenRedactions(sess)...)
	drv.Press(tuitest.Enter)
	drv.WaitGone(askChoiceHint)

	// Single-select with a custom answer typed: the highlight drops and the box shows the draft.
	submit(drv, popupSinglePrompt)
	drv.WaitText(askChoiceHint)
	drv.WaitQuiet(settled)
	drv.Type("Do the config first")
	drv.WaitText("Do the config first")
	drv.WaitQuiet(settled)
	tuitest.Golden(t, "popup-ask-typed", drv.Frame(), goldenRedactions(sess)...)
	drv.Press(tuitest.Enter)
	drv.WaitGone(askChoiceHint)

	// Free-text: no choices at all.
	submit(drv, popupFreePrompt)
	drv.WaitText(askFreeHint)
	drv.WaitQuiet(settled)
	tuitest.Golden(t, "popup-ask-free", drv.Frame(), goldenRedactions(sess)...)
	drv.Type("popups")
	drv.WaitText("popups")
	drv.Press(tuitest.Enter)
	drv.WaitGone(askFreeHint)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}
