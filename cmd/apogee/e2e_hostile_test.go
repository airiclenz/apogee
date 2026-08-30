package main

// T-12 of the v0.17.1 release checklist — "untrusted text cannot forge rows in any apogee surface" —
// as a test.
//
// It was manual because the fixtures have to be hostile file and skill names on a REAL filesystem
// and the question is what a REAL terminal does with the bytes apogee then paints. Both halves are
// now answerable: the fixture is built on disk exactly as the checklist's own paste block builds it,
// and the terminal is the emulator, which is where a forged row, a leaked colour or a jumped cursor
// would actually show up. A claim about "does this still read as one row" is a claim about cells,
// and cells are what a Frame is.
//
// The one thing no assertion below makes is the escaping of the tool RESULTS as they reach the
// screen: the transcript never paints them. list_dir, find_files and grep declare no `body` in the
// tool registry (internal/tui/toolregistry.go), so what the reader sees of a listing is its target
// and its outcome slot — "6 entries" — and the rows the checklist asks a human to count are the rows
// the MODEL is handed. Those are asserted, out of the stub's request log, in
// TestE2EHostileToolResultsKeepOneRowPerEntry.

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/judge"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The model label the TUI is launched with in the wide test: the checklist's own
// `--model "$(printf 'gpt\033[31m-oss-20b')"`, and what the footer must make of it — the ESC
// dropped, the rest of the sequence left standing as ordinary text on the footer's own colours.
const (
	hostileModelFlag  = "gpt\x1b[31m-oss-20b"
	hostileModelShown = "gpt[31m-oss-20b"
)

// narrowHostileSize is the terminal T-12's steps 6 and 10 are read at. Sixty columns is where the
// approval pane's argument has to wrap and where the settings sub-list runs out of room, which is
// the whole point of both steps.
var narrowHostileSize = tuitest.Size{W: 60, H: 24}

// TestE2EHostileProbeKeepsItsOwnRows is T-12 step 1: the off-session report, on a workspace whose
// ROOT NAME carries an escape sequence. The report is text a terminal will print verbatim, so the
// claim is made on its bytes — the root on one line, no ESC anywhere, and the same line count a
// benign workspace produces, because a report that grew a line grew it from the fixture.
func TestE2EHostileProbeKeepsItsOwnRows(t *testing.T) {
	ws := hostileWorkspace(t)
	stub := stubllm.New(t, loadScript(t, "hostile"))
	home := upstreamHome(t, stub.URL, stub.Model)

	report := runProbe(t, newProbeCommand(), home, ws)

	if n := strings.Count(report, "\x1b"); n != 0 {
		t.Errorf("the probe report carries %d raw ESC bytes:\n%q", n, report)
	}
	line := reportLine(t, report, "workspace:")
	if !strings.Contains(line, "wsRED") && !strings.Contains(line, "ws[31mRED") {
		t.Errorf("the workspace line does not name the hostile root: %q", line)
	}
	// One line for the root: the name holds no newline, so nothing under it may have moved down.
	benign := runProbe(t, newProbeCommand(), upstreamHome(t, stub.URL, stub.Model), t.TempDir())
	if got, want := countLines(report), countLines(benign); got != want {
		t.Errorf("the hostile report is %d lines and a benign one is %d; the fixture authored rows",
			got, want)
	}
}

// TestE2EHostileSurfacesKeepTheirOwnRows walks T-12 steps 2–5 at a hundred columns: the footer drawn
// from a hostile MODEL label, and the /skills note drawn from a hostile skill DIRECTORY name — the
// two surfaces the item's own commits changed.
func TestE2EHostileSurfacesKeepTheirOwnRows(t *testing.T) {
	ws := hostileWorkspace(t)
	stub := stubllm.New(t, loadScript(t, "hostile"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIIn(t, drv, stub, ws, "", "--model", hostileModelFlag)
	red := ansiRed(t)

	// Step 2 — the footer shows the label as ordinary text: the ESC dropped, the rest inert, and
	// every cell of the row painted in the footer's own colours rather than in the red the sequence
	// asked for.
	drv.WaitText("Send a message")
	drv.WaitQuiet(settled)
	first := drv.Frame()
	footer := footerRow(t, first)
	if !strings.Contains(footer, hostileModelShown) {
		t.Errorf("the footer does not show the model label inert: %q", footer)
	}
	assertModelSegmentIsNotRed(t, first, red)
	// The rest of the row is intact: the server it is on, the separators, and the mode marker the
	// footer ends with — a sequence that had eaten the row would have taken one of them.
	for _, want := range []string{"probe-target", "✦", "◐ ask before"} {
		if !strings.Contains(footer, want) {
			t.Errorf("the footer lost %q: %q", want, footer)
		}
	}
	// Step 3 — the WORKDIR cell, reported rather than ruled on, exactly as the checklist asks: only
	// host, model and effort were changed by the commits this item is about.
	reportWorkdirCell(t, first, red)
	assertNoLeakedColour(t, first, red)

	// Steps 4 and 5 — the /skills note. One skill loaded, one that could not load, one shadowed, and
	// each of the last two on exactly the two rows its section authored: the newline inside the skill
	// directory's name opened no further row.
	submit(drv, "/skills")
	drv.WaitText("shadowed by another of the same id")
	drv.WaitQuiet(settled)
	skills := drv.Frame()
	assertSkillSectionRows(t, skills, "1 skill found but not loaded:", 2)
	assertSkillSectionRows(t, skills, "1 skill shadowed by another of the same id:", 2)
	// The hostile directory name is on ONE row, its newline flattened to a space, and no row in the
	// block reads like a loaded skill's entry — which is the impersonation the note exists to disclose.
	if _, _, ok := skills.Find("ev[31mil row —"); !ok {
		t.Errorf("the failed skill's name is not on one row of the note:\n%s", skills)
	}
	if got := strings.Count(skills.String(), " · workspace "); got != 1 {
		t.Errorf("the note draws %d loaded-skill rows; only /dupe is loaded:\n%s", got, skills)
	}
	assertNoLeakedColour(t, skills, red)
	tuitest.Golden(t, "t12-skills", skills, hostileRedactions(sess, ws, hostileModelShown)...)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EHostileToolResultsKeepOneRowPerEntry is T-12 steps 8 and 9. The rows it counts are the ones
// the MODEL is handed, because those are the only rows a listing has: the transcript paints a tool
// call's target and its outcome slot and never its result body (see the file header). A name carrying
// a newline or a carriage return arrives spelled `\n` / `\r` — escapeRowBreaks, internal/tools —
// so one entry is one row and no row can be overwritten by the one after it.
func TestE2EHostileToolResultsKeepOneRowPerEntry(t *testing.T) {
	ws := hostileWorkspace(t)
	stub := stubllm.New(t, loadScript(t, "hostile"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIIn(t, drv, stub, ws, "")
	red := ansiRed(t)

	drv.WaitText("Send a message")
	for _, step := range []struct {
		prompt string
		slot   string
		lines  int
	}{
		{"List the workspace root", "6 entries", 7},    // the header line plus six entries
		{"Find the text files", "4 files", 5},          // the header line plus four hits
		{"Search the workspace for hello", "1 hit", 2}, // the header line plus one match
	} {
		submit(drv, step.prompt)
		drv.WaitText(step.slot)
		drv.WaitQuiet(settled)

		result := lastToolResult(t, stub)
		if got := len(strings.Split(result, "\n")); got != step.lines {
			t.Errorf("%q returned %d rows; want %d:\n%q", step.prompt, got, step.lines, result)
		}
		for _, forbidden := range []string{"\r", "\x1b\n"} {
			if strings.Contains(result, forbidden) {
				t.Errorf("%q returned a raw %q, which is a row break the reader never authored:\n%q",
					step.prompt, forbidden, result)
			}
		}
		assertNoLeakedColour(t, drv.Frame(), red)
	}
	// The two hostile names, spelled the only way a single row can carry them.
	result := allToolResults(stub)
	for _, want := range []string{`two\nrows.txt`, `carriage\rreturn.txt`} {
		if !strings.Contains(result, want) {
			t.Errorf("no listing spelled the row break literally as %q:\n%s", want, result)
		}
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EHostileWrapsUnderItsOwnIndent is T-12 steps 6 and 10, at the sixty columns both of them are
// about: the settings sub-list that has to say what `auto` costs in a column too narrow for the
// sentence, and the approval pane that has to wrap a three-hundred-character argument without letting
// a continuation row fall back to the pane's left edge.
func TestE2EHostileWrapsUnderItsOwnIndent(t *testing.T) {
	ws := hostileWorkspace(t)
	stub := stubllm.New(t, loadScript(t, "hostile"))
	drv := tuitest.NewDriver(t, narrowHostileSize)
	sess := launchTUIIn(t, drv, stub, ws, "")
	red := ansiRed(t)

	// Step 6 — the `mode` row's value sub-list. The sentence beside `auto` does NOT wrap: this pane
	// truncates its right-hand column with an ellipsis (renderSettingsSubList sets menuRows without
	// wrapRows), so the claim it CAN answer is the one a forged row would break — the sentence stays
	// on the row it was given, ends inside the pane's own border, and the value cells stand in one
	// column. See the dated note under this item in the plan.
	submit(drv, "/settings")
	drv.WaitText("Upstream")
	settingsGoTo(t, drv, settingKeyMode)
	drv.Press(tuitest.Enter)
	drv.WaitText("(current)")
	drv.WaitQuiet(settled)
	sub := drv.Frame()
	auto := rowIndexContaining(t, sub, "auto runs")
	sentence, _, ok := sub.Find("auto runs")
	marker, _, markerOK := sub.Find("(current)")
	if !ok || !markerOK {
		t.Fatalf("the mode sub-list is missing the auto sentence or the current marker:\n%s", sub)
	}
	if sentence != marker {
		t.Errorf("the auto sentence starts in column %d and the (current) cell in column %d; the "+
			"sub-list's right-hand cells stand in one column", sentence, marker)
	}
	if !strings.HasSuffix(strings.TrimRight(strings.Trim(sub.Row(auto), " │"), " "), "…") {
		t.Errorf("the auto sentence is not clipped inside the pane: %q", sub.Row(auto))
	}
	if next := strings.Trim(sub.Row(auto+1), " │"); next != "" && !strings.HasPrefix(next, "↑/↓") {
		t.Errorf("the row under `auto` is %q; the clipped sentence authored a second row", next)
	}
	assertNoLeakedColour(t, sub, red)
	drv.Press(tuitest.Esc)
	drv.WaitGone("(current)")
	closePane(drv, settingsHint)

	// Step 10 — the approval pane over a three-hundred-character argument. `command:` on its own row,
	// the value two columns in beneath it, and every wrapped row of the value under that same indent
	// — including the LAST one, which is what a body that fell back to the border would give away.
	submit(drv, "Echo the long string please")
	pane := awaitApprovalPane(drv)
	assertArgumentHangsUnderItsIndent(t, pane)
	assertNoLeakedColour(t, pane, red)
	tuitest.Golden(t, "t12-pane-60", pane, goldenRedactions(sess)...)

	drv.Press(tuitest.Esc)
	drv.Press(tuitest.Esc) // esc×2: the first press arms the stop, the second confirms it
	drv.WaitGone(approvalMarker)
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestJudgeHostileRowsReadAsOneRow is T-12's own judgment half — "whether an escaped row still READS
// as one row, and where a wrapped popup body puts its continuation lines", which is the sentence the
// checklist gives as the reason the item was manual. Everything a cell can settle is settled above;
// what is left is whether a reader looking at these two frames would take the hostile text for
// apogee's own.
func TestJudgeHostileRowsReadAsOneRow(t *testing.T) {
	if !judge.Enabled() {
		judge.Skip(t)
		return
	}

	ws := hostileWorkspace(t)
	stub := stubllm.New(t, loadScript(t, "hostile"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIIn(t, drv, stub, ws, "", "--model", hostileModelFlag)

	submit(drv, "/skills")
	drv.WaitText("shadowed by another of the same id")
	drv.WaitQuiet(settled)
	skills := drv.Frame()

	pane := wrappedPaneFrame(t, ws)

	judge.Require(t, t.Context(), judge.Rubric{
		Item: "T-12",
		Claim: "hostile file and skill names render as inert text on the number of rows apogee " +
			"authored, and a wrapped popup body keeps its continuation lines under its own indent",
		PassWhen: "every surface renders the hostile bytes as inert escaped text on the number of " +
			"rows it authored, the terminal's colours and cursor are unaffected throughout, and " +
			"wrapped popup bodies keep their continuation lines under their own indent.",
		FailsIf: "a row count exceeds the heading's count; a filename appears to add a header, a " +
			"match or a note of its own; text after a hostile name reads reversed; or a wrapped " +
			"line returns to column 0 instead of hanging under its own indent.",
		Extra: []string{
			"The first frame is the /skills note over a workspace holding one loadable skill, one " +
				"SKILL.md whose directory name contains an ESC and a newline, and one shadowed " +
				"duplicate. The second is an approval pane at sixty columns over a " +
				"three-hundred-character argument.",
			"Rule ONLY on how the frames READ. Row counts, the column each continuation row " +
				"starts in, and the absence of any leaked colour are asserted cell by cell in this " +
				"same test file and are not yours to rule on.",
			"The question is impersonation: could a reader take a row that came from a FILE NAME " +
				"for a row apogee wrote about itself?",
		},
	},
		judge.FrameArtifact("the /skills note over hostile skill names", skills, false),
		judge.FrameArtifact("an approval pane wrapping a long argument at 60 columns", pane, false))

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// wrappedPaneFrame is the second artifact the judge is given: an approval pane at sixty columns,
// wrapping the fixture's three-hundred-character argument. It takes a run of its own because the
// two frames are read at two different widths, and a driver's terminal size is fixed when it is
// made — the width IS the case each frame is evidence about.
func wrappedPaneFrame(t *testing.T, ws string) tuitest.Frame {
	t.Helper()

	stub := stubllm.New(t, loadScript(t, "hostile"))
	drv := tuitest.NewDriver(t, narrowHostileSize)
	sess := launchTUIIn(t, drv, stub, ws, "")

	submit(drv, "Echo the long string please")
	pane := awaitApprovalPane(drv)
	drv.Press(tuitest.Esc)
	drv.Press(tuitest.Esc) // esc×2: the first press arms the stop, the second confirms it
	drv.WaitGone(approvalMarker)
	if err := sess.Quit(); err != nil {
		t.Fatalf("the narrow run returned %v; want a clean quit", err)
	}
	return pane
}

// ----------------------------------------------------------------------------
// The fixture
// ----------------------------------------------------------------------------

// hostileWorkspace builds the checklist's own fixture tree and returns its root: a directory whose
// NAME carries an escape sequence, four files that between them hold an escape, a newline, a
// carriage return and a bidi override, a skill directory named with an ESC and a newline holding an
// empty SKILL.md (empty because a file that fails validation is what produces a "found but not
// loaded" row), and a `dupe` skill in both workspace skill roots so one shadows the other.
//
// The root is the checklist's own `/tmp/apogee-hostile-…` rather than t.TempDir, and that is the
// golden's doing: /skills prints the skill PATHS, and a row of the note has to fit at a hundred
// columns or the frame wraps somewhere that depends on the machine. A t.TempDir path carries the
// test's name and a counter, and on macOS TMPDIR alone is fifty columns. It is still a throwaway
// directory the test creates and removes, which is the whole of what the "no test touches the real
// home" rule asks of it.
func hostileWorkspace(t *testing.T) string {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the fixture's names hold an ESC, a newline and a carriage return; Windows has no such files")
	}
	root, err := os.MkdirTemp("/tmp", "apogee-hostile-")
	if err != nil {
		t.Fatalf("make the hostile fixture root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	ws := filepath.Join(root, "ws\x1b[31mRED")
	mkdir := func(dir string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("build the hostile fixture: %v", err)
		}
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("build the hostile fixture: %v", err)
		}
	}

	mkdir(ws)
	write(filepath.Join(ws, "plain.txt"), "hello\n")
	write(filepath.Join(ws, "evil\x1b[31mRED\x1b[0m.txt"), "")
	write(filepath.Join(ws, "two\nrows.txt"), "")
	write(filepath.Join(ws, "carriage\rreturn.txt"), "")
	write(filepath.Join(ws, "bidi\u202etxt.exe"), "")

	failing := filepath.Join(ws, ".apogee", "skills", "ev\x1b[31mil\nrow")
	mkdir(failing)
	write(filepath.Join(failing, "SKILL.md"), "")

	const dupe = "---\nname: dupe\nsummary: a duplicate id\n---\nbody\n"
	for _, dir := range []string{
		filepath.Join(ws, "skills", "dupe"),
		filepath.Join(ws, ".apogee", "skills", "dupe"),
	} {
		mkdir(dir)
		write(filepath.Join(dir, "SKILL.md"), dupe)
	}
	return ws
}

// hostileRedactions are the substitutions the hundred-column golden needs on top of the session's
// own. What the golden KEEPS is the hostile name itself — `wsRED`, `ev[31mil row`, the escaped
// reason — which is the whole reason to record this frame; what goes is everything that is a fact
// about the machine.
//
// Three of them, in this order because each would otherwise rewrite the next one's match:
//
//   - the footer's whole tail past the model cell, whose padding is a fact about how long the temp
//     path came out;
//   - the fixture root, by name and by its resolved form (on macOS /tmp is a symlink, and the path
//     apogee prints is the resolved one);
//   - the padding between a redacted PATH and the scroll rail at the end of its row. The rail's
//     column is downstream of the path length the redaction above just changed, so leaving it would
//     put the temp path's length back into the golden under another name. Only the rows that carry a
//     path are touched; every other row keeps the alignment it was painted with.
func hostileRedactions(sess *e2eSession, ws, model string) []tuitest.Redaction {
	cell := "✦ " + model + " ✦ "
	redactions := []tuitest.Redaction{
		{Pattern: regexp.MustCompile(regexp.QuoteMeta(cell) + ".*"), With: cell + "<workdir>"},
	}
	for _, root := range fixtureRoots(filepath.Dir(ws)) {
		redactions = append(redactions,
			tuitest.Redaction{Pattern: regexp.MustCompile(regexp.QuoteMeta(root)), With: "<root>"})
	}
	redactions = append(redactions, tuitest.Redact(`(?m)^(.*<root>.*?) +([│┃])$`, "$1 $2"))
	return append(redactions, sess.Redactions()...)
}

// fixtureRoots is the fixture root under every name the screen might print it by: as created, and as
// the filesystem resolves it. They are the same path on Linux and two paths on macOS, where /tmp is a
// symlink into /private — and the longest goes first so it cannot be half-rewritten by the shorter.
func fixtureRoots(root string) []string {
	roots := []string{root}
	if resolved, err := filepath.EvalSymlinks(root); err == nil && resolved != root {
		roots = append([]string{resolved}, roots...)
	}
	return roots
}

// ----------------------------------------------------------------------------
// Reading a hostile frame
// ----------------------------------------------------------------------------

// ansiRed is the colour `ESC [ 31 m` sets, measured by the SAME emulator the driven frames are read
// through. Comparing against a literal would be comparing against a guess; this is the exact value a
// leaked sequence would have left in a cell, which is what makes "no cell is red" a claim about the
// escape rather than about a colour that merely looks like it.
func ansiRed(t *testing.T) tuitest.Style {
	t.Helper()

	probe := tuitest.NewScreen(4, 1)
	defer probe.Close()
	if _, err := probe.Write([]byte("\x1b[31mX")); err != nil {
		t.Fatalf("paint the reference red: %v", err)
	}
	return probe.Snapshot().Cell(0, 0).Style
}

// assertNoLeakedColour is the claim the checklist makes about the whole terminal rather than about
// one row: after every step, no cell carries the colour a hostile sequence asked for. A sequence that
// survived sanitisation would have painted at least one.
//
// The FOOTER row is exempt, and reportWorkdirCell says why: its workdir segment is the one place
// apogee hands a directory name to the terminal unstripped, and T-12 step 3 asks for that to be
// reported rather than failed. Every other row of every frame is held to the rule.
func assertNoLeakedColour(t *testing.T, f tuitest.Frame, red tuitest.Style) {
	t.Helper()

	footer := footerRowIndex(f)
	for y := range f.Height() {
		if y == footer {
			continue
		}
		if _, ok := redRun(f, y, red); ok {
			t.Errorf("row %d is painted in the escape's own red, which nothing in the theme is:\n%s", y, f)
			return
		}
	}
}

// assertModelSegmentIsNotRed is T-12 step 2's own claim: the footer shows the hostile model label on
// the FOOTER's colours. It rules on the runs up to the end of that label rather than on the whole
// row, because the workdir segment beyond it is step 3's and is reported, not ruled on.
func assertModelSegmentIsNotRed(t *testing.T, f tuitest.Frame, red tuitest.Style) {
	t.Helper()

	y := footerRowIndex(f)
	// The runs are walked from the left and the walk STOPS at the one that completes the label,
	// rather than from a column found by [tuitest.Frame.Find]: the header box carries the same label
	// and Find answers about the first row holding it, which is not this one.
	seen := ""
	for _, run := range f.StyleRuns(y) {
		if tuitest.SameColor(run.FG, red.FG) {
			t.Errorf("the footer's %q run is painted in the label's own red: %q", run.Text, f.Row(y))
		}
		seen += run.Text
		if strings.Contains(seen, hostileModelShown) {
			return
		}
	}
	t.Errorf("the footer row does not carry the model label: %q", f.Row(y))
}

// reportWorkdirCell performs T-12 step 3 — "report what the workdir does — do not fail T-12 on it
// either way". The workdir is the one footer segment [Model.footerContent] does not put through
// stripEscapes (internal/tui/model.go: the host, the model id and the effort default are stripped as
// server- and config-authored text; the workdir is the operator's own path), so a workspace root
// whose NAME carries a colour sequence paints the terminal with it. That is what this logs.
func reportWorkdirCell(t *testing.T, f tuitest.Frame, red tuitest.Style) {
	t.Helper()

	y := footerRowIndex(f)
	if run, ok := redRun(f, y, red); ok {
		t.Logf("T-12 step 3: the footer's workdir segment painted %q in the directory name's own "+
			"red — the workdir is not escape-stripped; reported, not failed: %q", run.Text, f.Row(y))
		return
	}
	t.Logf("T-12 step 3: the footer's workdir segment reads %q with no colour of its own", f.Row(y))
}

// redRun is the first run of row y painted in red's foreground, if any.
func redRun(f tuitest.Frame, y int, red tuitest.Style) (tuitest.Run, bool) {
	for _, run := range f.StyleRuns(y) {
		if tuitest.SameColor(run.FG, red.FG) {
			return run, true
		}
	}
	return tuitest.Run{}, false
}

// footerRowIndex is the row the status line is painted on: the last row carrying the footer's own
// separator. [footerRow] answers the same question with the row's TEXT; a colour claim needs its
// POSITION, which is the whole of the difference.
func footerRowIndex(f tuitest.Frame) int {
	for y := f.Height() - 1; y >= 0; y-- {
		if strings.Contains(f.Row(y), " "+glyphFooterSeparator+" ") {
			return y
		}
	}
	return -1
}

// glyphFooterSeparator is the ✦ the footer sets its segments apart with (internal/tui/theme.go's
// glyphAssistant, which the status line reuses).
const glyphFooterSeparator = "✦"

// assertSkillSectionRows counts the rows one /skills section AUTHORED, and compares them with the
// count its own heading states.
//
// Authored rows are told from wrapped ones by their indent, which is the note's own layout and not a
// heuristic: failedSkillLines and shadowedSkillLines indent an entry's two lines by two and four
// (internal/tui/skills.go) on top of the note's body column, while a line the frame had to WRAP
// resumes at that body column. So a row indented further than the heading is a row the section wrote,
// and a row at the heading's own indent is the tail of the row above it.
func assertSkillSectionRows(t *testing.T, f tuitest.Frame, heading string, want int) {
	t.Helper()

	start := rowIndexContaining(t, f, heading)
	indent := indentOf(noteRow(f, start))
	authored := 0
	for y := start + 1; y < f.Height(); y++ {
		row := noteRow(f, y)
		if row == "" {
			break // the blank row that closes the section
		}
		if indentOf(row) > indent {
			authored++
		}
	}
	if authored != want {
		t.Errorf("%q is followed by %d authored rows; the heading counts %d:\n%s",
			heading, authored, want, f)
	}
}

// assertArgumentHangsUnderItsIndent is T-12 step 10's geometry: `command:` alone on its row, its
// value indented two columns beneath it, and every continuation row of that value — the last one
// included — starting at the value's own column rather than back at the pane's border.
//
// The pane elides a body too tall for it with a "… (+N more lines)" marker at the pane's own text
// column; that row is the pane speaking, not the argument, so it is skipped rather than counted as a
// continuation that fell flush left.
func assertArgumentHangsUnderItsIndent(t *testing.T, f tuitest.Frame) {
	t.Helper()

	label := rowIndexContaining(t, f, "command:")
	if got := strings.Trim(f.Row(label), " │"); got != "command:" {
		t.Errorf("the command: label shares its row with %q", got)
	}
	want := textColumn(f.Row(label)) + 2
	rows := 0
	for y := label + 1; y < f.Height(); y++ {
		text := strings.Trim(f.Row(y), " │")
		if text == "" || strings.HasPrefix(text, "❯ ") || strings.HasPrefix(text, "· ") {
			break // the blank row that closes the body, or the first decision row
		}
		if strings.HasPrefix(text, "… (+") {
			continue // the pane's own elision marker, not a row of the argument
		}
		if got := textColumn(f.Row(y)); got != want {
			t.Errorf("row %d of the argument starts at column %d; the value's own column is %d:\n%s",
				y, got, want, f)
		}
		rows++
	}
	if rows < 2 {
		t.Fatalf("the argument did not wrap at %d columns, so there is no continuation row to check:\n%s",
			f.Width(), f)
	}
}

// settingsGoTo walks the settings key list to the row that names key. It walks UP because the list
// wraps and the keys this test wants sit past the middle of the registry, so the shorter way round is
// backwards — a fact about the list's length, not about the pane.
func settingsGoTo(t *testing.T, drv *tuitest.Driver, key string) {
	t.Helper()

	// A ceiling on the walk rather than an expectation: the registry decides how many rows there are.
	const maxRows = 100

	for range maxRows {
		if strings.HasPrefix(settingsCursor(drv), "❯ "+key+" ") {
			return
		}
		if !stepSettings(drv, tuitest.Up) {
			break
		}
	}
	t.Fatalf("the settings list never highlighted %q (it stopped on %q)", key, settingsCursor(drv))
}

// noteRow is one row of a transcript note with the scroll rail the frame paints down its right-hand
// edge taken off, so a row the note left blank reads as blank.
func noteRow(f tuitest.Frame, y int) string {
	return strings.TrimRight(f.Row(y), " ┃│")
}

// indentOf is how many columns a row is indented by. Every row it is asked about is indented with
// ASCII spaces, so the byte count is the column count.
func indentOf(row string) int { return len(row) - len(strings.TrimLeft(row, " ")) }

// reportLine is the probe report's one line beginning with label.
func reportLine(t *testing.T, report, label string) string {
	t.Helper()

	for _, line := range strings.Split(report, "\n") {
		if strings.Contains(line, label) {
			return line
		}
	}
	t.Fatalf("the probe report has no %q line:\n%s", label, report)
	return ""
}

// countLines is how many lines a report is.
func countLines(report string) int { return len(strings.Split(strings.TrimRight(report, "\n"), "\n")) }

// lastToolResult is the most recent tool result the stub was handed — the listing as the MODEL
// received it, which is the only place a listing's rows exist (see the file header).
func lastToolResult(t *testing.T, stub *stubllm.Server) string {
	t.Helper()

	for i := len(stub.Requests()) - 1; i >= 0; i-- {
		messages := stub.Requests()[i].Messages
		for j := len(messages) - 1; j >= 0; j-- {
			if messages[j].Role == "tool" {
				return messages[j].Content
			}
		}
	}
	t.Fatal("the stub was never handed a tool result")
	return ""
}

// allToolResults is every tool result the stub saw, joined — what a claim about a name that appears
// in more than one listing is made against.
func allToolResults(stub *stubllm.Server) string {
	var b strings.Builder
	for _, req := range stub.Requests() {
		for _, msg := range req.Messages {
			if msg.Role == "tool" {
				b.WriteString(msg.Content)
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}
