package tui

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/gitstatus"
)

// The footer's seven facts and its marker, as one narrow window after another takes them away.
// The literals are deliberately spelled out rather than composed from the code under test: the
// drop ORDER is the contract, and a test that built its own expectation with the same joiner
// would agree with a wrong order as happily as with the right one.
const (
	footerTestHost    = "apollo"
	footerTestModel   = "gpt-oss-20b"
	footerTestEffort  = "high"
	footerTestWorkdir = "~/apogee"
	footerTestGit     = "main 5Δ 0↓ 4↑"
	footerTestSpend   = "≥ 0.42 EUR"
	footerTestMode    = "⏵⏵ auto · confined"

	footerRunFull       = "apollo ✦ gpt-oss-20b ✦ high ✦ ~/apogee ✦ main 5Δ 0↓ 4↑ ✦ ≥ 0.42 EUR"
	footerRunNoSpend    = "apollo ✦ gpt-oss-20b ✦ high ✦ ~/apogee ✦ main 5Δ 0↓ 4↑"
	footerRunNoGit      = "apollo ✦ gpt-oss-20b ✦ high ✦ ~/apogee"
	footerRunNoEffort   = "apollo ✦ gpt-oss-20b ✦ ~/apogee"
	footerRunNoWorkdir  = "apollo ✦ gpt-oss-20b"
	footerRunModelOnly  = "gpt-oss-20b"
	footerOfflineJoined = " ✦ offline"
)

// footerFitCase is the full seven-segment footer every case below narrows, offline included so the
// ladder is exercised with the one segment it may never drop before the model.
func footerFitCase() footerInput {
	measure := newWidthAuthority()
	return footerInput{
		segments: []footerSegment{
			{footerSegHost, footerTestHost},
			{footerSegModel, footerTestModel},
			{footerSegEffort, footerTestEffort},
			{footerSegWorkdir, footerTestWorkdir},
			{footerSegGit, footerTestGit},
			{footerSegSpend, footerTestSpend},
		},
		offline: offlineLabel,
		mode:    footerTestMode,
		margin:  measure.Width(bodyIndent),
		measure: measure,
		glyphs:  defaultGlyphs,
	}
}

// withSegment is in with the named segment's text replaced — on a copy of the segment list, so the
// input it was taken from keeps its own.
func withSegment(in footerInput, id footerSegmentID, text string) footerInput {
	segments := make([]footerSegment, len(in.segments))
	copy(segments, in.segments)
	for i := range segments {
		if segments[i].id == id {
			segments[i].text = text
		}
	}
	in.segments = segments
	return in
}

// footerSeatWidth is the narrowest window a given left run seats the marker in, stated from the
// fit's own rule rather than from its code: both margins, the left run, the marker, and the one
// blank column that keeps the two ends apart.
func footerSeatWidth(t *testing.T, in footerInput, info, offline string) int {
	t.Helper()

	return in.margin + in.measure.Width(info) + in.measure.Width(offline) +
		1 + in.measure.Width(in.mode) + in.margin
}

// TestFooterFitDropsSegmentsInPriorityOrder is the ladder itself: at the narrowest window each
// rung still seats in, the row says exactly that rung — and one column below it, a further segment
// has gone. The spend goes first, then the git segment, and then the order is the order the row is
// read for, outward-in: the effort word, then the workdir, then the host. While the git segment is
// on the row the layout names where it sits, so the painter tones that span and nothing else.
func TestFooterFitDropsSegmentsInPriorityOrder(t *testing.T) {
	t.Parallel()

	in := footerFitCase()
	rungs := []struct {
		name     string
		info     string
		keepsGit bool
	}{
		{"everything the session knows", footerRunFull, true},
		{"the spend goes first", footerRunNoSpend, true},
		{"then the git segment", footerRunNoGit, false},
		{"then the effort word", footerRunNoEffort, false},
		{"then the workdir", footerRunNoWorkdir, false},
		{"then the host", footerRunModelOnly, false},
	}

	for _, rung := range rungs {
		t.Run(rung.name, func(t *testing.T) {
			t.Parallel()

			w := footerSeatWidth(t, in, rung.info, footerOfflineJoined)
			at := in
			at.width = w
			got := footerFit(at)

			if got.info != rung.info {
				t.Errorf("footerFit(width %d).info = %q, want %q", w, got.info, rung.info)
			}
			if got.offline != footerOfflineJoined {
				t.Errorf("footerFit(width %d).offline = %q, want %q", w, got.offline, footerOfflineJoined)
			}
			git, hasGit := got.span(footerSegGit)
			if rung.keepsGit {
				if !hasGit || git.text != footerTestGit || got.info[git.at:git.at+len(git.text)] != footerTestGit {
					t.Errorf("footerFit(width %d) = (git %q at %d in %q), want %q named where it sits",
						w, git.text, git.at, got.info, footerTestGit)
				}
			} else if hasGit {
				t.Errorf("footerFit(width %d).git = %q, want none once the segment has gone", w, git.text)
			}
			if !got.hasMode || got.mode != footerTestMode {
				t.Errorf("footerFit(width %d) = (mode %q, hasMode %t), want the marker whole", w, got.mode, got.hasMode)
			}
			if col := w - in.measure.Width(in.mode) - in.margin; got.col != col {
				t.Errorf("footerFit(width %d).col = %d, want %d — the marker is right-anchored", w, got.col, col)
			}

			narrow := in
			narrow.width = w - 1
			if dropped := footerFit(narrow); dropped.info == rung.info {
				t.Errorf("footerFit(width %d).info = %q, want a further segment dropped", w-1, dropped.info)
			}
		})
	}
}

// TestFooterFitSpendsThePriorityZeroFloorInHalves proves what happens once the ladder has nothing
// left to drop: the model gives way in halves — an ellipsis first, then the segment whole — and
// only then does offline go, leaving the marker alone on the row. Nothing here ever drops the
// marker: that is the defect this fit exists to close.
func TestFooterFitSpendsThePriorityZeroFloorInHalves(t *testing.T) {
	t.Parallel()

	in := footerFitCase()

	t.Run("the model truncates before it goes", func(t *testing.T) {
		t.Parallel()

		at := in
		at.width = footerSeatWidth(t, in, footerRunModelOnly, footerOfflineJoined) - 1
		got := footerFit(at)

		if !strings.HasSuffix(got.info, "…") || !strings.HasPrefix(footerTestModel, strings.TrimSuffix(got.info, "…")) {
			t.Errorf("footerFit(width %d).info = %q, want the model truncated with an ellipsis", at.width, got.info)
		}
		if got.offline != footerOfflineJoined || !got.hasMode {
			t.Errorf("footerFit(width %d) = (offline %q, hasMode %t), want offline and the marker kept",
				at.width, got.offline, got.hasMode)
		}
	})

	t.Run("then the model goes whole", func(t *testing.T) {
		t.Parallel()

		at := in
		at.width = footerSeatWidth(t, in, "", offlineLabel)
		got := footerFit(at)

		if got.info != "" {
			t.Errorf("footerFit(width %d).info = %q, want the model gone", at.width, got.info)
		}
		// Alone on the row, offline sheds the separator that would otherwise dangle at its start.
		if got.offline != offlineLabel || !got.hasMode {
			t.Errorf("footerFit(width %d) = (offline %q, hasMode %t), want %q and the marker kept",
				at.width, got.offline, got.hasMode, offlineLabel)
		}
	})

	t.Run("then offline goes and the marker stands alone", func(t *testing.T) {
		t.Parallel()

		at := in
		at.width = footerSeatWidth(t, in, "", offlineLabel) - 1
		got := footerFit(at)

		if got.info != "" || got.offline != "" {
			t.Errorf("footerFit(width %d) = (info %q, offline %q), want the left run empty",
				at.width, got.info, got.offline)
		}
		if !got.hasMode || got.mode != footerTestMode {
			t.Errorf("footerFit(width %d) = (mode %q, hasMode %t), want the marker whole", at.width, got.mode, got.hasMode)
		}
	})
}

// TestFooterFitDropsTheMarkerOnlyBelowItsOwnFloor pins the one window the marker does not survive
// — it cannot seat WHOLE between the two margins, and a clipped mode word would name a blast
// radius the session is not in — and the shape the row falls back to there: the full left run,
// truncated to the window, with nothing to click.
func TestFooterFitDropsTheMarkerOnlyBelowItsOwnFloor(t *testing.T) {
	t.Parallel()

	in := footerFitCase()
	floor := in.margin + in.measure.Width(footerTestMode) + in.margin

	at := in
	at.width = floor - 1
	got := footerFit(at)

	if got.hasMode || got.mode != "" || got.col != 0 {
		t.Fatalf("footerFit(width %d) = (mode %q, col %d, hasMode %t), want the marker dropped whole",
			at.width, got.mode, got.col, got.hasMode)
	}
	if !strings.HasPrefix(footerRunFull, strings.TrimSuffix(got.info, "…")) {
		t.Errorf("footerFit(width %d).info = %q, want the full left run truncated", at.width, got.info)
	}
	if len(got.spans) != 0 {
		t.Errorf("footerFit(width %d).spans = %+v, want the truncated floor run painted plain", at.width, got.spans)
	}
	if spent := in.measure.Width(got.info) + in.measure.Width(got.offline); spent > at.width-in.margin {
		t.Errorf("footerFit(width %d) spends %d columns beside its margin, want at most %d",
			at.width, spent, at.width-in.margin)
	}

	seated := in
	seated.width = floor
	if got := footerFit(seated); !got.hasMode {
		t.Errorf("footerFit(width %d).hasMode = false, want the marker seated at its own floor", floor)
	}
}

// TestFooterFitSeatsTheMarkerAtEveryWidthThatHoldsIt is the invariant behind the whole file, swept
// rather than sampled: the marker is present at exactly the widths it fits between the two margins,
// it is always right-anchored, and it never overlaps the left run.
func TestFooterFitSeatsTheMarkerAtEveryWidthThatHoldsIt(t *testing.T) {
	t.Parallel()

	in := footerFitCase()
	markerFloor := in.margin + in.measure.Width(footerTestMode) + in.margin

	for w := -1; w <= footerSeatWidth(t, in, footerRunFull, footerOfflineJoined)+10; w++ {
		at := in
		at.width = w
		got := footerFit(at)

		if want := w >= markerFloor; got.hasMode != want {
			t.Fatalf("footerFit(width %d).hasMode = %t, want %t", w, got.hasMode, want)
		}
		if !got.hasMode {
			continue
		}
		if got.mode == "" {
			t.Errorf("footerFit(width %d).mode is empty while hasMode is set", w)
		}
		if got.col+in.measure.Width(got.mode)+in.margin != w {
			t.Errorf("footerFit(width %d).col = %d, want the marker to end %d short of the edge",
				w, got.col, in.margin)
		}
		// The blank column is asked for only where there IS a left run: on the last rung the marker
		// stands alone between the two margins, with nothing for a gap to separate it from.
		left := in.margin + in.measure.Width(got.info) + in.measure.Width(got.offline)
		if got.info+got.offline != "" && left >= got.col {
			t.Errorf("footerFit(width %d) leaves no blank column: left run ends at %d, marker starts at %d",
				w, left, got.col)
		}
	}
}

// TestFooterFitSkipsASegmentNothingNamed proves the ladder is stated over priorities rather than
// over positions: a session with no priced call, outside any git work tree, on a server that
// reports no effort dial has nothing to drop at priorities 5, 4 and 3, so the first segment the
// narrowing row gives up is the workdir.
func TestFooterFitSkipsASegmentNothingNamed(t *testing.T) {
	t.Parallel()

	in := footerFitCase()
	in = withSegment(in, footerSegSpend, "")
	in = withSegment(in, footerSegGit, "")
	in = withSegment(in, footerSegEffort, "")

	const runNoEffort = "apollo ✦ gpt-oss-20b ✦ ~/apogee"
	at := in
	at.width = footerSeatWidth(t, in, runNoEffort, footerOfflineJoined)
	if got := footerFit(at); got.info != runNoEffort {
		t.Errorf("footerFit(width %d).info = %q, want %q", at.width, got.info, runNoEffort)
	}

	at.width--
	if got := footerFit(at); got.info != footerRunNoWorkdir {
		t.Errorf("footerFit(width %d).info = %q, want the workdir to be the first drop", at.width, got.info)
	}
}

// TestFooterFitKeepsOfflineThroughEveryLadderDrop pins offline's priority: the state a send is
// refused in outranks every fact about where the session points, so it is still on the row when
// the spend, the host, the workdir and the effort word have all gone.
func TestFooterFitKeepsOfflineThroughEveryLadderDrop(t *testing.T) {
	t.Parallel()

	in := footerFitCase()
	for _, info := range []string{footerRunFull, footerRunNoSpend, footerRunNoGit, footerRunNoEffort, footerRunNoWorkdir, footerRunModelOnly} {
		at := in
		at.width = footerSeatWidth(t, in, info, footerOfflineJoined)
		if got := footerFit(at); got.offline != footerOfflineJoined {
			t.Errorf("footerFit(width %d).offline = %q, want %q while %q still shows",
				at.width, got.offline, footerOfflineJoined, info)
		}
	}
}

// TestFooterFitLeavesNoDanglingSeparator sweeps every width for the one shape a hand-joined row
// gets wrong: a segment that left without the ✦ that introduced it — with the git segment on the
// row, and with it absent, as it is outside a work tree.
func TestFooterFitLeavesNoDanglingSeparator(t *testing.T) {
	t.Parallel()

	withGit := footerFitCase()
	noGit := withSegment(footerFitCase(), footerSegGit, "")
	for _, in := range []footerInput{withGit, noGit} {
		for w := 0; w <= footerSeatWidth(t, in, footerRunFull, footerOfflineJoined)+10; w++ {
			at := in
			at.width = w
			got := footerFit(at)

			row := got.info + got.offline
			if strings.HasPrefix(row, " ") || strings.HasSuffix(row, " ") ||
				strings.HasPrefix(row, defaultGlyphs.assistant) || strings.HasSuffix(row, defaultGlyphs.assistant) {
				t.Errorf("footerFit(width %d, git %q) left run = %q, want no dangling separator", w, in.segment(footerSegGit), row)
			}
		}
	}
}

// TestGitFooterSpansWordTheWorkingTree pins the git segment's words: the branch, the changed-path
// count, and — only where git compared the branch with an upstream — how far it is behind and
// ahead. Each count is bright exactly when it is not zero; the branch is never bright.
func TestGitFooterSpansWordTheWorkingTree(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		snap   gitstatus.Snapshot
		want   string
		bright []string
	}{
		{
			name:   "a branch with an upstream",
			snap:   gitstatus.Snapshot{Branch: "main", Upstream: "origin/main", HasAheadBehind: true, Dirty: 5, Ahead: 4},
			want:   "main 5Δ 0↓ 4↑",
			bright: []string{"5Δ", "4↑"},
		},
		{
			name:   "no upstream drops both upstream counts",
			snap:   gitstatus.Snapshot{Branch: "main", Dirty: 5},
			want:   "main 5Δ",
			bright: []string{"5Δ"},
		},
		{
			name: "a detached HEAD drops the branch",
			snap: gitstatus.Snapshot{Detached: true, Dirty: 5},
			want: "5Δ", bright: []string{"5Δ"},
		},
		{
			name: "a clean level tree is all muted",
			snap: gitstatus.Snapshot{Branch: "main", Upstream: "origin/main", HasAheadBehind: true},
			want: "main 0Δ 0↓ 0↑",
		},
		{
			name: "an escape-laden branch is stripped",
			snap: gitstatus.Snapshot{Branch: "ma\x1b]8;;http://evil\x07in\x1b[31m", Dirty: 1},
			want: "main 1Δ", bright: []string{"1Δ"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			spans := gitFooterSpans(tc.snap, defaultGlyphs)
			if got := footerSpansText(spans); got != tc.want {
				t.Errorf("gitFooterSpans(%+v) = %q, want %q", tc.snap, got, tc.want)
			}
			var bright []string
			for _, span := range spans {
				if span.bright {
					bright = append(bright, span.text)
				}
			}
			if strings.Join(bright, ",") != strings.Join(tc.bright, ",") {
				t.Errorf("gitFooterSpans(%+v) bright spans = %q, want %q", tc.snap, bright, tc.bright)
			}
		})
	}
}

// TestGitFooterSpansKeepTheirMarksUnderSafeGlyphs: `ui.glyphs: safe` keeps `Δ ↓ ↑` — the marks a
// count reads by are the same under either set.
func TestGitFooterSpansKeepTheirMarksUnderSafeGlyphs(t *testing.T) {
	t.Parallel()

	snap := gitstatus.Snapshot{Branch: "main", Upstream: "origin/main", HasAheadBehind: true, Dirty: 5, Ahead: 4}
	if got := footerSpansText(gitFooterSpans(snap, safeGlyphs)); got != footerTestGit {
		t.Errorf("gitFooterSpans under safe glyphs = %q, want %q", got, footerTestGit)
	}
}

// TestFooterSpanTableAgreesWithThePaintedRow sweeps a painted footer through every rung — the git
// segment kept and dropped, each priority drop, the model cut and gone, the floor — and reads each
// entry of the layout's span table off the row the painter drew: the cells the entry names hold
// exactly its text, and its byte offset names the same text in the layout's info run. The painter
// and the span table are one layout, so a segment's cells can be addressed without searching the
// row for it.
func TestFooterSpanTableAgreesWithThePaintedRow(t *testing.T) {
	t.Parallel()

	m := withGitSnapshot(footerFactsModel(t), footerGitTestSnapshot)
	m.hb.offline = true

	sawGit, sawCut := false, false
	for w := 160; w >= 0; w-- {
		layout := m.footerRow(w)
		flat := ansiPattern.ReplaceAllString(m.footerContent(w), "")
		for _, span := range layout.spans {
			if got := m.th.measure.Cut(flat, span.col, span.col+span.cells); got != span.text {
				t.Errorf("width %d: %s span cells [%d, %d) of %q = %q, want %q",
					w, span.id, span.col, span.col+span.cells, flat, got, span.text)
			}
			if !strings.HasPrefix(layout.info[span.at:], span.text) {
				t.Errorf("width %d: %s span at byte %d of info %q, want it to begin %q",
					w, span.id, span.at, layout.info, span.text)
			}
			sawGit = sawGit || span.id == footerSegGit
			sawCut = sawCut || strings.HasSuffix(span.text, "…")
		}
	}
	if !sawGit || !sawCut {
		t.Errorf("the sweep named the git span %t and a cut model %t, want both rungs crossed", sawGit, sawCut)
	}
}
