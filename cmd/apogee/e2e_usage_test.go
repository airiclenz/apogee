package main

// T-06 of the v0.17.1 release checklist — cached-token accounting, delegate spend, and the totals a
// resumed session carries — as a test.
//
// It was manual for one reason: "the `cached` column only appears when a real server reports
// `prompt_tokens_details.cached_tokens`, which no unit test can conjure honestly". A SCRIPTED server
// reports exactly what its fixture says it reports (testdata/stubllm/cached-usage.yaml), which is
// the same honesty a live caching server offers and none of its variance — and the thing actually
// being judged, whether the pane and the browser row read plausibly side by side, is then a claim
// about the frames both surfaces painted.

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/format"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The four prompts the cached-usage fixture answers, named once so the test reads as the
// conversation it is.
const (
	coldPrompt      = "Summarise the workspace."
	warmPrompt      = "And what is in it?"
	delegatePrompt6 = "Delegate a one-line description of every file."
	resumePrompt    = "One more question about it?"
	uncachedPrompt  = "Answer with no cache breakdown, please."
)

// The counts testdata/stubllm/cached-usage.yaml reports, named here so every expectation below is
// arithmetic on what the SERVER said rather than on what the pane happened to draw. A pane asserted
// against its own cells would agree with itself.
const (
	coldPrompt6, coldCompletion6                 = 900, 40
	warmPrompt6, warmCached6, warmCompletion6    = 950, 880, 30
	wrapPrompt6, wrapCached6, wrapCompletion6    = 1100, 900, 25
	childPrompt6, childCached6, childCompletion6 = 640, 320, 12
	resumePrompt6, resumeCompletion6             = 1200, 15
)

// usageHeaderOrder is the ratified column order, with `cached` DIRECTLY after `prompt` because it is
// a share of that very count rather than a spend beside it. The gutter between popup columns is
// spaces, so the order is asserted over whitespace rather than over the "·" the design text uses to
// write it down.
var usageHeaderOrder = regexp.MustCompile(`agent\s+calls\s+prompt\s+cached\s+completion\s+total\s+ctx`)

// TestE2EUsageReportsCachedTokensAndDelegateSpend walks T-06 steps 2–8: a cold call, a call the
// server answers mostly from its cache, the /usage pane, a delegation, the /sessions row, and the
// totals a --continue resume carries forward.
func TestE2EUsageReportsCachedTokensAndDelegateSpend(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "cached-usage"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)

	// Steps 2–3 — two messages in one session. The second re-sends the first as its prefix, which
	// is what the server answers out of its cache.
	submit(drv, coldPrompt)
	drv.WaitText("The workspace holds one file")
	submit(drv, warmPrompt)
	drv.WaitText("One line, and it says hello.")

	// Steps 4–5 — the pane's header carries the cached column in its ratified place, and the main
	// row's cached figure sits INSIDE the prompt count it qualifies.
	usage := openUsage(t, drv)
	header := rowContaining(t, usage, "prompt")
	if !usageHeaderOrder.MatchString(header) {
		t.Errorf("the /usage header is not %q: %q", "agent calls prompt cached completion total ctx", header)
	}
	main := usageCells(t, usage, "main")
	wantMain := wantUsageRow("main", 2,
		coldPrompt6+warmPrompt6, warmCached6, coldCompletion6+warmCompletion6)
	if !slices.Equal(main, wantMain) {
		t.Errorf("the main row is %q; the server reported %q", main, wantMain)
	}
	if cached, prompt := tokensValue(t, main[3]), tokensValue(t, main[2]); cached > prompt {
		t.Errorf("the main row's cached %s exceeds its prompt %s", main[3], main[2])
	}
	closePane(drv, usagePaneMarker)

	// Step 6 — a delegation spends a window of its own, and the pane grows a row for it plus the
	// session total those rows sum to.
	submit(drv, delegatePrompt6)
	drv.WaitText("The delegate reported back.")

	usage = openUsage(t, drv)
	// One row per delegate, named by the name the delegation carried, with the child's OWN counts
	// on it — the spend the main row must not have absorbed.
	delegate := usageCells(t, usage, "survey")
	wantDelegate := wantUsageRow("survey", 1, childPrompt6, childCached6, childCompletion6)
	if !slices.Equal(delegate, wantDelegate) {
		t.Errorf("the delegate row is %q; the child's server reported %q", delegate, wantDelegate)
	}
	// And the session row is main + delegates, asserted against the arithmetic on the FIXTURE's
	// numbers rather than on the coarse spellings the pane rounds them to.
	mainPrompt := coldPrompt6 + warmPrompt6 + wrapPrompt6
	mainCached := warmCached6 + wrapCached6
	mainCompletion := coldCompletion6 + warmCompletion6 + wrapCompletion6
	main = usageCells(t, usage, "main")
	if want := wantUsageRow("main", 3, mainPrompt, mainCached, mainCompletion); !slices.Equal(main, want) {
		t.Errorf("the main row is %q; the server reported %q", main, want)
	}
	session := usageCells(t, usage, "session")
	wantSession := wantUsageRow("session", 4,
		mainPrompt+childPrompt6, mainCached+childCached6, mainCompletion+childCompletion6)
	if !slices.Equal(session, wantSession) {
		t.Errorf("the session row is %q; main + delegates is %q", session, wantSession)
	}
	sessionTotal := session[5]
	closePane(drv, usagePaneMarker)

	// Step 7 — the browser row ends with the SAME sum, not the main agent's share alone.
	submit(drv, "/sessions")
	drv.WaitText(sessionsPaneMarker)
	drv.WaitQuiet(settled)
	row := rowContaining(t, drv.Frame(), "msg")
	cells := splitCells(row)
	if last := cells[len(cells)-1]; last != "· "+sessionTotal {
		t.Errorf("the /sessions row ends with %q; the session spent %q", last, sessionTotal)
	}
	closePane(drv, sessionsPaneMarker)

	// Step 8 — a resumed session's totals CONTINUE from the record: they are larger than what the
	// one new message cost, and they never restart from zero.
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
	next := sess.RelaunchWith("--continue")
	next.WaitText("Send a message")
	submit(next, resumePrompt)
	next.WaitText("Nothing has changed since.")

	usage = openUsage(t, next)
	resumed := usageCells(t, usage, "main")
	if len(resumed) != 6 {
		t.Fatalf("the resumed main row has %d cells, want 6: %q", len(resumed), resumed)
	}
	if got, alone := tokensValue(t, resumed[5]), resumePrompt6+resumeCompletion6; got <= alone {
		t.Errorf("the resumed total is %s (%d): it restarted from the new message's own %d rather "+
			"than continuing the record", resumed[5], got, alone)
	}
}

// TestE2EUsageHidesTheCachedColumnWithoutABreakdown is T-06 step 10, the negative half: against a
// server that reports counts and says nothing about caching, the pane must draw NO cached column.
// A column of zeros would read as a cache miss, which is a different fact and not one the server
// stated.
func TestE2EUsageHidesTheCachedColumnWithoutABreakdown(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "cached-usage"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)

	submit(drv, uncachedPrompt)
	drv.WaitText("Nothing here was answered from a cache.")

	usage := openUsage(t, drv)
	header := rowContaining(t, usage, "prompt")
	if strings.Contains(header, "cached") {
		t.Errorf("the /usage header carries a cached column against a server that reported none: %q", header)
	}
	if cells := usageCells(t, usage, "main"); len(cells) != 5 {
		t.Errorf("the main row has %d cells, want 5 without the cached column: %q", len(cells), cells)
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EUsageHeadlessCachedCellIsSelfHiding is T-06 step 9 through the headless surface, where the
// same rule has to hold on one line of stderr: the cached counter is appended only when the run
// actually hit a cache, because "· cached 0" reads as a measured miss.
//
// It drives the CLI over the canned runner (headlessRun) rather than a driven TUI: what is under
// test is the line the command composes out of a Result, and a Result carrying a cache share is
// exactly what a fixture is for.
func TestE2EUsageHeadlessCachedCellIsSelfHiding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		usage  run.Usage
		wantIn string
		wantNo string
	}{
		{
			name: "a cached call names its share",
			usage: run.Usage{
				Calls: 2, PromptTokens: 1850, CompletionTokens: 70, TotalTokens: 1920,
				CachedPromptTokens: 880,
			},
			wantIn: "· cached 880",
		},
		{
			name: "an uncached call draws no cached cell",
			usage: run.Usage{
				Calls: 1, PromptTokens: 900, CompletionTokens: 40, TotalTokens: 940,
			},
			wantIn: "usage: calls 1 · prompt 900 · completion 40 · total 940",
			wantNo: "cached",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubRunner{res: run.Result{FinalText: "the answer", Turns: 1, Usage: tc.usage}}

			_, errOut, err := headlessRun(t, stub, "say hi in five words")

			if err != nil {
				t.Fatalf("headless returned %v; want a clean run", err)
			}
			if !strings.Contains(errOut, tc.wantIn) {
				t.Errorf("stderr carries no %q:\n%s", tc.wantIn, errOut)
			}
			if tc.wantNo != "" && strings.Contains(errOut, tc.wantNo) {
				t.Errorf("stderr names %q against a server that reported none:\n%s", tc.wantNo, errOut)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Reading the two panes
// ----------------------------------------------------------------------------

// usagePaneMarker and sessionsPaneMarker are each pane's own furniture — the line a test waits to
// see and then waits to LEAVE, rather than any content line, which a differently-scripted run might
// not have.
const (
	usagePaneMarker    = "session token usage"
	sessionsPaneMarker = "type to filter · ↑/↓ select · ⏎ resume"
)

// openUsage opens the /usage pane and returns the settled frame it painted.
func openUsage(t *testing.T, drv *tuitest.Driver) tuitest.Frame {
	t.Helper()

	submit(drv, "/usage")
	drv.WaitText(usagePaneMarker)
	drv.WaitQuiet(settled)
	return drv.Frame()
}

// usageCells is the /usage row whose first cell is name, split back into its columns. The pane lays
// its columns out with a two-space gutter and pads every cell to its column's width, so splitting on
// runs of two or more spaces recovers exactly the cells that were composed — and an EMPTY cell
// collapses into the gutter beside it, which is why the caller checks the count it got.
func usageCells(t *testing.T, f tuitest.Frame, name string) []string {
	t.Helper()

	for _, row := range f.Rows() {
		fields := splitCells(row)
		if len(fields) > 0 && fields[0] == name {
			return fields
		}
	}
	t.Fatalf("no /usage row named %q:\n%s", name, f)
	return nil
}

// splitCells breaks a laid-out popup row into its cells on the two-space gutter, dropping the pane's
// own border and padding at either end.
func splitCells(row string) []string {
	inner := strings.Trim(row, " │╭╮╰╯─")
	fields := regexp.MustCompile(` {2,}`).Split(strings.TrimSpace(inner), -1)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// tokensCell matches the coarse token spelling every count on these surfaces is written in
// (internal/format.Tokens): a bare integer under a thousand, then k / M / G with at most one decimal.
var tokensCell = regexp.MustCompile(`^(\d+)(?:\.(\d))?([kMG])?$`)

// tokensValue reads one of those spellings back as a number, failing the test on anything that is
// not spelled in that language — which is itself the "same units as everything else" half of step 5.
func tokensValue(t *testing.T, cell string) int {
	t.Helper()

	m := tokensCell.FindStringSubmatch(cell)
	if m == nil {
		t.Fatalf("%q is not a token count in the coarse form (18k, 1.2M)", cell)
	}
	whole, _ := strconv.Atoi(m[1])
	value := float64(whole)
	if m[2] != "" {
		tenth, _ := strconv.Atoi(m[2])
		value += float64(tenth) / 10
	}
	switch m[3] {
	case "k":
		value *= 1024
	case "M":
		value *= 1024 * 1024
	case "G":
		value *= 1024 * 1024 * 1024
	}
	return int(value)
}

// wantUsageRow is the row the pane must draw for an agent that made calls calls and was charged
// these counts: the name, the call count, and the four token cells in their ratified order, each
// spelled by the one formatter every token reading on screen goes through. The ctx cell is absent
// because the stub advertises no context window, and an empty cell collapses into the gutter beside
// it (splitCells).
func wantUsageRow(name string, calls, prompt, cached, completion int) []string {
	return []string{
		name,
		strconv.Itoa(calls),
		format.Tokens(prompt),
		format.Tokens(cached),
		format.Tokens(completion),
		format.Tokens(prompt + completion),
	}
}
