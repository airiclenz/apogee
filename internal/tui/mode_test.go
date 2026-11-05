package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/gitstatus"
	"github.com/airiclenz/apogee/internal/scheme"
)

// keyShiftTab is the autonomy-mode cycle chord; its String() is "shift+tab", which handleKey
// matches (mirroring the textarea's "shift+enter" binding).
func keyShiftTab() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift} }

// newModeModel builds an idle, sized model starting in mode `start`, returning it with the
// fakeEngine so a test can assert SetMode was driven on the engine.
func newModeModel(t *testing.T, start domain.Mode) (Model, *fakeEngine) {
	t.Helper()
	eng := &fakeEngine{}
	opts := testOpts
	opts.Mode = start
	m := step(t, newModel(context.Background(), eng, opts, nil), tea.WindowSizeMsg{Width: 80, Height: 24})
	return m, eng
}

// TestModelShiftTabCyclesMode walks the full ladder (incl. the Auto→Plan wrap): each press
// advances opts.Mode, drives the engine via SetMode, and renders the FRIENDLY footer label
// (spaced, not the hyphenated wire form).
func TestModelShiftTabCyclesMode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		start, want domain.Mode
		label       string
	}{
		{domain.ModePlan, domain.ModeAskBefore, "ask before"},
		{domain.ModeAskBefore, domain.ModeAllowEdits, "allow edits"},
		{domain.ModeAllowEdits, domain.ModeAuto, "auto"},
		{domain.ModeAuto, domain.ModePlan, "plan"}, // wrap-around
	}
	for _, tc := range cases {
		t.Run(string(tc.start), func(t *testing.T) {
			t.Parallel()

			m, eng := newModeModel(t, tc.start)

			m = step(t, m, keyShiftTab())

			if m.opts.Mode != tc.want {
				t.Fatalf("opts.Mode = %q, want %q", m.opts.Mode, tc.want)
			}
			if got := eng.modesSet(); len(got) != 1 || got[0] != tc.want {
				t.Fatalf("engine SetMode = %v, want [%q]", got, tc.want)
			}
			footer := ansiPattern.ReplaceAllString(m.footerContent(80), "")
			if !strings.Contains(footer, tc.label) {
				t.Fatalf("footer = %q, want friendly label %q", footer, tc.label)
			}
			if canon := string(tc.want); strings.Contains(canon, "-") && strings.Contains(footer, canon) {
				t.Fatalf("footer shows hyphenated %q, want friendly label %q", canon, tc.label)
			}
		})
	}
}

// TestFooterDropsSegmentsBeforeTheModeMarker is the fit's pin on the row a human actually reads.
// The footer's one job on a narrow window is to keep saying which blast radius the session runs in,
// and the old shape had it exactly backwards: the marker dropped WHOLE the moment both ends did not
// fit, so the fact that matters most was the first to go. The row now spends its columns in the
// order it is read for — in this session, which priced no call and so shows no spend, the effort
// word first, then the workdir, then the host — and the marker stays whole at every width in this
// sweep, Auto's blast-radius word beside it.
//
// The left run is asserted VERBATIM at each width, not merely searched: a segment that leaves takes
// its separator with it, so no rung of the ladder may open or close on a dangling ✦.
func TestFooterDropsSegmentsBeforeTheModeMarker(t *testing.T) {
	t.Parallel()

	m := footerFactsModel(t)
	marker := footerModeText(modeMarker(newTheme(scheme.Default()), domain.ModeAuto), confinedWord)

	for _, tc := range []struct {
		width int
		want  string
	}{
		{120, "test-host ✦ test-model ✦ high ✦ /ws/proj"}, // everything the session knows
		{80, "test-host ✦ test-model ✦ high ✦ /ws/proj"},
		{60, "test-host ✦ test-model ✦ /ws/proj"}, // priority 3: the effort word
		{40, "test-model"}, // then the workdir, then the host
	} {
		t.Run(fmt.Sprintf("width %d", tc.width), func(t *testing.T) {
			flat := ansiPattern.ReplaceAllString(m.footerContent(tc.width), "")

			if !strings.HasSuffix(flat, marker+bodyIndent) {
				t.Fatalf("footer = %q, want it to end %q — the marker is what the row never gives up",
					flat, marker+bodyIndent)
			}
			left := strings.TrimSpace(strings.TrimSuffix(flat, marker+bodyIndent))
			if left != tc.want {
				t.Errorf("footer's left run = %q, want %q", left, tc.want)
			}
			if strings.HasPrefix(left, defaultGlyphs.assistant) || strings.HasSuffix(left, defaultGlyphs.assistant) {
				t.Errorf("footer's left run = %q, want no dangling %q", left, defaultGlyphs.assistant)
			}
		})
	}
}

// footerLeftRun is the footer's left run as a human reads it at width w: the painted row with its
// styling and the right-anchored Auto marker taken off.
func footerLeftRun(t *testing.T, m Model, w int) string {
	t.Helper()

	marker := footerModeText(modeMarker(newTheme(scheme.Default()), domain.ModeAuto), confinedWord)
	flat := ansiPattern.ReplaceAllString(m.footerContent(w), "")
	if !strings.HasSuffix(flat, marker+bodyIndent) {
		t.Fatalf("footer = %q, want it to end %q", flat, marker+bodyIndent)
	}
	return strings.TrimSpace(strings.TrimSuffix(flat, marker+bodyIndent))
}

// TestFooterStatesTheSessionSpend pins the footer's spend segment (ADR 0093): absent while no call
// was priced, the WHOLE session's amount once one was — the main agent's, a sub-agent's and a
// background workflow's added up, the same sum the /usage session row states — marked partial
// where some of those calls were unpriced, and the first segment a narrowing row gives up, ahead
// of the effort word.
func TestFooterStatesTheSessionSpend(t *testing.T) {
	t.Parallel()

	const (
		facts     = "test-host ✦ test-model ✦ high ✦ /ws/proj"
		withSpend = facts + " ✦ ≥ 0.42 EUR"
	)
	priced := func(t *testing.T) Model {
		t.Helper()

		m := footerFactsModel(t)
		m.opts.Currency = "EUR"
		m.usage = domain.Usage{Calls: 1, PromptTokens: 900, TotalTokens: 900, CostMicros: 100_000, PricedCalls: 1}
		// The sub-agent ran one call on a priced server and one on a server with no `price:`.
		m = delegate(t, m, "s1", "survey the tests", domain.Usage{
			Calls: 2, PromptTokens: 800, TotalTokens: 800, CostMicros: 200_000, PricedCalls: 1, UnpricedCalls: 1,
		}, 0)
		return foldEvents(t, m,
			bgPhase(domain.WorkflowStarted),
			pricedRunUsage(workflowRunUsage(bgChildBase("bg.1"), 1, 1000, 100), 120_000, 1),
		)
	}

	t.Run("no priced call shows no segment", func(t *testing.T) {
		t.Parallel()

		m := footerFactsModel(t)
		m.opts.Currency = "EUR"
		m.usage = domain.Usage{Calls: 3, PromptTokens: 900, TotalTokens: 900, UnpricedCalls: 3}

		if got := footerLeftRun(t, m, 120); got != facts {
			t.Errorf("footer's left run = %q, want %q — an unpriced session names no amount", got, facts)
		}
	})

	t.Run("the whole session's spend, marked partial", func(t *testing.T) {
		t.Parallel()

		if got := footerLeftRun(t, priced(t), 120); got != withSpend {
			t.Errorf("footer's left run = %q, want %q — main, sub-agent and background workflow added up", got, withSpend)
		}
	})

	t.Run("the workdir middle-elides, then the spend goes before the effort word", func(t *testing.T) {
		t.Parallel()

		m := priced(t)
		want := []string{withSpend, "test-host ✦ test-model ✦ high ✦ …/proj ✦ ≥ 0.42 EUR", facts}
		if got := footerNarrowingRuns(t, m, 120, facts); !slices.Equal(got, want) {
			t.Errorf("footer's left runs, narrowing from 120 columns = %q, want %q — the workdir shortens before the spend, the first segment to go", got, want)
		}
	})
}

// withGitSnapshot hands the footer a held git snapshot, as a landed status read leaves it
// (gitstatus.go) — the footer reads the Model's state, not the poll that filled it.
func withGitSnapshot(m Model, snap gitstatus.Snapshot) Model {
	m.git.snap, m.git.hasSnap = snap, true
	return m
}

// footerGitTestSnapshot is the git state the footer tests read: on main, five changed paths, level
// with nothing behind and four commits ahead of its upstream.
var footerGitTestSnapshot = gitstatus.Snapshot{
	Branch: "main", Upstream: "origin/main", HasAheadBehind: true, Dirty: 5, Ahead: 4,
}

// TestFooterStatesTheGitSegment pins the footer's git segment in the painted row: after the workdir
// it describes, `<branch> NΔ N↓ N↑`, its upstream counts absent without an upstream, its branch
// absent on a detached HEAD, and the whole segment absent WITH its separator while no snapshot is
// held. The branch is repository text and leaves its escapes behind on the way in.
func TestFooterStatesTheGitSegment(t *testing.T) {
	t.Parallel()

	const facts = "test-host ✦ test-model ✦ high ✦ /ws/proj"
	for _, tc := range []struct {
		name string
		snap *gitstatus.Snapshot
		want string
	}{
		{"no snapshot shows no segment", nil, facts},
		{"a branch with an upstream", &footerGitTestSnapshot, facts + " ✦ main 5Δ 0↓ 4↑"},
		{"no upstream", &gitstatus.Snapshot{Branch: "main", Dirty: 5}, facts + " ✦ main 5Δ"},
		{"a detached HEAD", &gitstatus.Snapshot{Detached: true, Dirty: 5}, facts + " ✦ 5Δ"},
		{
			"an escape-laden branch",
			&gitstatus.Snapshot{Branch: "ma\x1b]8;;http://evil\x07in\x1b[31m", Dirty: 5},
			facts + " ✦ main 5Δ",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := footerFactsModel(t)
			if tc.snap != nil {
				m = withGitSnapshot(m, *tc.snap)
			}
			if got := footerLeftRun(t, m, 160); got != tc.want {
				t.Errorf("footer's left run = %q, want %q", got, tc.want)
			}
			if raw := m.footerContent(160); strings.Contains(raw, "evil") || strings.Contains(raw, "\x1b]") {
				t.Errorf("footer carries the branch's escape sequence: %q", raw)
			}
		})
	}

	t.Run("the workdir and then the branch middle-elide, then the git segment goes before the effort word", func(t *testing.T) {
		t.Parallel()

		m := withGitSnapshot(footerFactsModel(t), footerLongBranchSnapshot)
		want := []string{
			facts + " ✦ feature/long-branch-name 5Δ 0↓ 4↑",
			"test-host ✦ test-model ✦ high ✦ …/proj ✦ feature/long-branch-name 5Δ 0↓ 4↑",
			"test-host ✦ test-model ✦ high ✦ …/proj ✦ feature/lon…branch-name 5Δ 0↓ 4↑",
		}
		got := footerNarrowingRuns(t, m, 160, facts)
		if len(got) < 4 || !slices.Equal(got[:3], want) {
			t.Fatalf("footer's left runs, narrowing from 160 columns = %q, want them to open %q", got, want)
		}
		// The branch gives up a cell a column until its floor, the counts whole beside it, and only
		// then does the first segment drop — the git segment, with the full workdir back.
		var floor string
		for _, run := range got[2 : len(got)-1] {
			branch, counts, ok := strings.Cut(strings.TrimPrefix(run, "test-host ✦ test-model ✦ high ✦ …/proj ✦ "), " ")
			if !ok || counts != "5Δ 0↓ 4↑" || !strings.Contains(branch, "…") {
				t.Errorf("footer's left run %q, want the branch middle-elided beside its whole counts", run)
			}
			floor = branch
		}
		if floor != "feat…name" {
			t.Errorf("the branch's last elided form = %q, want its floor %q", floor, "feat…name")
		}
		if last := got[len(got)-1]; last != facts {
			t.Errorf("footer's first run past the branch's floor = %q, want %q — the git segment goes before the effort word", last, facts)
		}
	})

	t.Run("an elided branch keeps its counts' tone", func(t *testing.T) {
		t.Parallel()

		m := withGitSnapshot(footerFactsModel(t), footerLongBranchSnapshot)
		bright := footerBrightOpener(t, m)
		elided := 0
		for w := 160; w > 0; w-- {
			run := footerLeftRun(t, m, w)
			if run == facts {
				break
			}
			if !strings.Contains(run, " ✦ feat") || strings.Contains(run, footerLongBranchSnapshot.Branch) {
				continue
			}
			elided++
			footer := m.footerContent(w)
			for _, count := range []string{"5Δ", "4↑"} {
				if !strings.Contains(footer, bright+count) {
					t.Errorf("footer at width %d does not paint %q bright beside the elided branch: %q", w, count, footer)
				}
			}
		}
		if elided == 0 {
			t.Fatal("no width middle-elided the branch")
		}
	})
}

// TestFooterPaintsEachConfiguredRow: each row `ui.footer.rows` names is painted on its own line,
// fitted to the window on its own, with the mode marker — and the offline word just left of it — at
// the right edge of the row naming `mode` and nowhere else.
func TestFooterPaintsEachConfiguredRow(t *testing.T) {
	t.Parallel()

	m := withGitSnapshot(footerFactsModelIn(t, footerTwoRows, "/ws/proj"), footerGitTestSnapshot)
	m.hb.offline = true
	for _, w := range []int{60, 80, 140} {
		lines := strings.Split(m.footerContent(w), "\n")
		if len(lines) != 2 {
			t.Fatalf("width %d: footer is %d rows, want the 2 %s names", w, len(lines), footerTwoRows)
		}
		for i, line := range lines {
			if got := m.th.measure.Width(line); got != w {
				t.Errorf("width %d: footer row %d is %d cells, want the window's %d", w, i, got, w)
			}
		}
		upstream := ansiPattern.ReplaceAllString(lines[0], "")
		workspace := ansiPattern.ReplaceAllString(lines[1], "")

		text, col, ok := m.footerModeSpan(w)
		if !ok {
			t.Fatalf("width %d: the mode marker does not seat; these widths need it drawn", w)
		}
		if !strings.HasSuffix(upstream, text+bodyIndent) || m.th.measure.Cut(upstream, col, col+m.th.measure.Width(text)) != text {
			t.Errorf("width %d: row 0 %q does not end in the marker %q at column %d", w, upstream, text, col)
		}
		if !strings.Contains(upstream, "test-model") || !strings.Contains(upstream, offlineLabel) {
			t.Errorf("width %d: row 0 %q lacks the model or the offline word", w, upstream)
		}
		if !strings.Contains(workspace, "/ws/proj") || !strings.Contains(workspace, "main 5Δ 0↓ 4↑") {
			t.Errorf("width %d: row 1 %q lacks the workdir or the git state", w, workspace)
		}
		for _, other := range []string{text, offlineLabel, "test-model"} {
			if strings.Contains(workspace, other) {
				t.Errorf("width %d: row 1 %q carries %q, which row 0 names", w, workspace, other)
			}
		}
	}
}

// TestFooterUnsetRowsPaintTheDefaultLayout: an unset `ui.footer.rows` paints exactly the layout the
// key's default names — today the one-row shape — at every width.
func TestFooterUnsetRowsPaintTheDefaultLayout(t *testing.T) {
	t.Parallel()

	unset := withGitSnapshot(footerFactsModelIn(t, "", "/ws/proj"), footerGitTestSnapshot)
	stated := withGitSnapshot(footerFactsModelIn(t, domain.DefaultFooterRows, "/ws/proj"), footerGitTestSnapshot)
	want, _ := domain.ParseFooterRows(string(domain.DefaultFooterRows))
	if got := unset.footerRows(); got != len(want) {
		t.Errorf("unset footer rows = %d, want the default's %d", got, len(want))
	}
	for _, w := range []int{20, 60, 80, 140} {
		if got, want := unset.footerContent(w), stated.footerContent(w); got != want {
			t.Errorf("width %d: unset footer\n%q\nwant the default's\n%q", w, got, want)
		}
	}
}

// TestFooterTwoRowsKeepTheGitStateAndTheEffortWordAt80: at 80 columns a long workdir and a long
// branch shorten on their own row — the workdir and then the branch middle-elided — so the git
// counts stay, and the upstream row keeps the effort word; the one-row layout gives the git
// segment up at the same width.
func TestFooterTwoRowsKeepTheGitStateAndTheEffortWordAt80(t *testing.T) {
	t.Parallel()

	const workspace = "/home/someone/repos/a-rather-long-project-name/internal/some/deep/package/directory"
	snap := footerGitTestSnapshot
	snap.Branch = "feature/a-very-long-branch-name-for-the-footer-row-test"
	snap.Upstream = "origin/" + snap.Branch

	m := withGitSnapshot(footerFactsModelIn(t, footerTwoRows, workspace), snap)
	lines := strings.Split(ansiPattern.ReplaceAllString(m.footerContent(80), ""), "\n")
	if len(lines) != 2 {
		t.Fatalf("footer is %d rows, want 2: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "high") {
		t.Errorf("row 0 %q dropped the effort word", lines[0])
	}
	if !strings.Contains(lines[1], "5Δ 0↓ 4↑") {
		t.Errorf("row 1 %q dropped the git counts", lines[1])
	}
	if strings.Contains(lines[1], workspace) || strings.Contains(lines[1], snap.Branch) || !strings.Contains(lines[1], footerEllipsis) {
		t.Errorf("row 1 %q did not middle-elide the long workdir and branch", lines[1])
	}

	one := withGitSnapshot(footerFactsModelIn(t, "", workspace), snap)
	if flat := ansiPattern.ReplaceAllString(one.footerContent(80), ""); strings.Contains(flat, "5Δ") {
		t.Errorf("the one-row footer %q kept the git state at 80 columns; this case needs a state it gives up", flat)
	}
}

// footerLongBranchSnapshot is footerGitTestSnapshot on a branch long enough to middle-elide.
var footerLongBranchSnapshot = gitstatus.Snapshot{
	Branch: "feature/long-branch-name", Upstream: "origin/feature/long-branch-name",
	HasAheadBehind: true, Dirty: 5, Ahead: 4,
}

// footerNarrowingRuns is the distinct left runs the footer states as its window narrows a column at
// a time from width, in the order they appear, up to and including the first that reads until.
func footerNarrowingRuns(t *testing.T, m Model, width int, until string) []string {
	t.Helper()

	var runs []string
	for w := width; w > 0; w-- {
		run := footerLeftRun(t, m, w)
		if len(runs) == 0 || run != runs[len(runs)-1] {
			runs = append(runs, run)
		}
		if run == until {
			return runs
		}
	}
	t.Fatalf("the footer never narrowed to %q: %q", until, runs)
	return nil
}

// footerBrightOpener is the escape sequence the footer opens its bright tone (theme.mutedBright)
// with, refused where it would be indistinguishable from the footer's own tone.
func footerBrightOpener(t *testing.T, m Model) string {
	t.Helper()

	probe := m.th.footerText.Foreground(m.th.mutedBright).Render("X")
	bright := probe[:strings.Index(probe, "X")]
	if bright == "" || strings.HasPrefix(m.th.footerText.Render("X"), bright) {
		t.Fatalf("the bright tone opens with %q, indistinguishable from the footer's own", bright)
	}
	return bright
}

// TestFooterTonesOnlyNonZeroGitCounts reads the styled bytes: a non-zero count opens the brighter
// muted step (theme.mutedBright) and a zero count or the branch never does, so a clean tree stays
// on the footer's own tone and the row draws no new role.
func TestFooterTonesOnlyNonZeroGitCounts(t *testing.T) {
	t.Parallel()

	m := withGitSnapshot(footerFactsModel(t), footerGitTestSnapshot)
	bright := footerBrightOpener(t, m)

	footer := m.footerContent(160)
	if n := strings.Count(footer, bright); n != 2 {
		t.Errorf("footer opens the bright tone %d times, want 2 (5Δ and 4↑): %q", n, footer)
	}
	for _, count := range []string{"5Δ", "4↑"} {
		if !strings.Contains(footer, bright+count) {
			t.Errorf("footer does not paint %q bright: %q", count, footer)
		}
	}
	for _, quiet := range []string{"0↓", "main"} {
		if strings.Contains(footer, bright+quiet) || strings.Contains(footer, bright+" "+quiet) {
			t.Errorf("footer paints %q bright: %q", quiet, footer)
		}
	}

	clean := withGitSnapshot(footerFactsModel(t), gitstatus.Snapshot{Branch: "main", Upstream: "origin/main", HasAheadBehind: true})
	if footer := clean.footerContent(160); strings.Contains(footer, bright) {
		t.Errorf("a clean, level tree opens the bright tone: %q", footer)
	}
}

// TestModeColorDistinct proves each autonomy mode maps to its own footer-marker colour, so the
// four markers are visually distinguishable.
func TestModeColorDistinct(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	modes := []domain.Mode{domain.ModePlan, domain.ModeAskBefore, domain.ModeAllowEdits, domain.ModeAuto}
	seen := map[string]domain.Mode{}
	for _, mode := range modes {
		key := fmt.Sprintf("%v", th.modeColor(mode))
		if prev, dup := seen[key]; dup {
			t.Errorf("modeColor(%q) == modeColor(%q) == %s; want a distinct colour per mode", mode, prev, key)
		}
		seen[key] = mode
	}
}

// TestFooterModeMarkerLeadsWithTheModeSymbol pins the footer's mode marker on every rung of the
// ladder: the word is led by that rung's own glyph — ⊞ plan, ◐ ask before, ✔ allow edits, ⏵⏵ auto
// — and the glyph is part of the SAME styled run as the word rather than a separately coloured
// badge beside it. The styled assertion is the point: a glyph rendered under its own style would
// read identically once the escapes are stripped, and is the exact defect this forbids.
//
// Auto's marker now carries a second word — the blast radius (confinementWord) — so the rung whose
// marker grew states its tail here: a freshly built fake engine confines nothing, so the word is
// "unconfined" and it trails the marker's own styled run in the error tone. The symbol still leads,
// which is what this test is for.
func TestFooterModeMarkerLeadsWithTheModeSymbol(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		mode                domain.Mode
		symbol, label, tail string
	}{
		{domain.ModePlan, "⊞", "plan", ""},
		{domain.ModeAskBefore, "◐", "ask before", ""},
		{domain.ModeAllowEdits, "✔", "allow edits", ""},
		{domain.ModeAuto, "⏵⏵", "auto", " · " + unconfinedWord},
	} {
		t.Run(string(tc.mode), func(t *testing.T) {
			t.Parallel()

			m, _ := newModeModel(t, tc.mode)
			footer := m.footerContent(80)
			want := tc.symbol + " " + tc.label

			if got := modeMarker(newTheme(scheme.Default()), tc.mode); got != want {
				t.Errorf("modeMarker(%q) = %q, want %q", tc.mode, got, want)
			}
			// The marker still ends the line bodyIndent short of the edge — the symbol joined the
			// slot, it did not displace the word from the column the gauge above it ends in.
			end := want + tc.tail + bodyIndent
			if flat := ansiPattern.ReplaceAllString(footer, ""); !strings.HasSuffix(flat, end) {
				t.Errorf("footer = %q, want it to end %q", flat, end)
			}
			if run := m.th.footerText.Foreground(m.th.modeColor(tc.mode)).Render(want); !strings.Contains(footer, run) {
				t.Errorf("footer does not carry %q as ONE styled run in the mode's own colour: %q", want, footer)
			}
		})
	}
}

// TestModeMarkerFallsBackToTheWordAlone proves an off-ladder mode keeps its word and borrows no
// other rung's shape: no glyph, and no orphan leading space where one would have gone.
func TestModeMarkerFallsBackToTheWordAlone(t *testing.T) {
	t.Parallel()

	off := domain.Mode("hands-off")
	if got := modeSymbol(newTheme(scheme.Default()), off); got != "" {
		t.Errorf("modeSymbol(%q) = %q, want no glyph for an off-ladder mode", off, got)
	}
	if got, want := modeMarker(newTheme(scheme.Default()), off), string(off); got != want {
		t.Errorf("modeMarker(%q) = %q, want %q", off, got, want)
	}
}

// TestModelShiftTabCyclesWhileBusy proves mid-turn switching: Shift+Tab cycles the mode and
// drives the engine while running, while awaiting an approval, and while awaiting an answer.
func TestModelShiftTabCyclesWhileBusy(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		state uiState
	}{
		{"running", stateRunning},
		{"awaiting-approval", stateAwaitingApproval},
		{"awaiting-ask", stateAwaitingAsk},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m, eng := newModeModel(t, domain.ModeAskBefore)
			m.state = tc.state

			m = step(t, m, keyShiftTab())

			if m.opts.Mode != domain.ModeAllowEdits {
				t.Fatalf("opts.Mode = %q, want allow-edits (cycle must work mid-turn)", m.opts.Mode)
			}
			if got := eng.modesSet(); len(got) != 1 || got[0] != domain.ModeAllowEdits {
				t.Fatalf("engine SetMode = %v, want [allow-edits]", got)
			}
		})
	}
}

// TestConfinementWordFollowsModeFlagAndBackend pins the footer's confinement word to the three
// cells Auto actually has, and to silence everywhere else. The rung is the gate: confinement
// attaches to Auto alone (ADR 0012), so the lower three say nothing about it however the flag and
// the backend stand — a word there would name a fence nothing reads. Inside Auto the flag outranks
// the backend ("unconfined" is the user's own decision, capable host or not) and the backend
// decides the remaining two: a fence it can keep reads "confined", one it cannot reads "gated" —
// the same word probe.DegradedNotice uses for that host.
func TestConfinementWordFollowsModeFlagAndBackend(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		info    ConfinementInfo
		mode    domain.Mode
		confine bool
		want    string
	}{
		{"auto confined on a fencing backend", capableHost, domain.ModeAuto, true, "confined"},
		{"auto gated where the backend cannot fence", degradedHost, domain.ModeAuto, true, "gated"},
		{"auto unconfined by the user's own decision", capableHost, domain.ModeAuto, false, "unconfined"},
		{"auto unconfined outranks a degraded backend", degradedHost, domain.ModeAuto, false, "unconfined"},
		{"auto with no backend wired reads as gated", ConfinementInfo{}, domain.ModeAuto, true, "gated"},
		{"allow edits says nothing", capableHost, domain.ModeAllowEdits, true, ""},
		{"ask before says nothing", capableHost, domain.ModeAskBefore, false, ""},
		{"plan says nothing", degradedHost, domain.ModePlan, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := confinementWord(tc.info, tc.mode, tc.confine); got != tc.want {
				t.Errorf("confinementWord(%+v, %q, confine=%v) = %q, want %q",
					tc.info, tc.mode, tc.confine, got, tc.want)
			}
		})
	}
}
