package tui

import (
	"slices"
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
			{id: footerSegHost, text: footerTestHost},
			{id: footerSegModel, text: footerTestModel},
			{id: footerSegEffort, text: footerTestEffort},
			{id: footerSegWorkdir, text: footerTestWorkdir},
			gitFooterSegment(gitFooterSpans(footerGitTestSnapshot, defaultGlyphs)),
			{id: footerSegSpend, text: footerTestSpend},
		},
		offline: offlineLabel,
		mode:    footerTestMode,
		margin:  measure.Width(bodyIndent),
		measure: measure,
		glyphs:  defaultGlyphs,
	}
}

// withSegment is in with the named segment replaced by plain text — on a copy of the segment list,
// so the input it was taken from keeps its own.
func withSegment(in footerInput, id footerSegmentID, text string) footerInput {
	segments := make([]footerSegment, len(in.segments))
	copy(segments, in.segments)
	for i := range segments {
		if segments[i].id == id {
			segments[i] = footerSegment{id: id, text: text}
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

// footerDeepWorkdir and footerLongBranch are the two long, free-form facts the row shortens before
// it drops anything, deep and long enough that every step of each elision shows.
const (
	footerDeepWorkdir = "~/repos/apogee/internal/tui"
	footerLongBranch  = "feature/long-branch-name"
)

// footerLongCase is footerFitCase with a deep workdir and a long branch.
func footerLongCase(t *testing.T, branch string) footerInput {
	t.Helper()

	in := withSegment(footerFitCase(), footerSegWorkdir, footerDeepWorkdir)
	snap := footerGitTestSnapshot
	snap.Branch = branch
	for i := range in.segments {
		if in.segments[i].id == footerSegGit {
			in.segments[i] = gitFooterSegment(gitFooterSpans(snap, defaultGlyphs))
		}
	}
	return in
}

// footerFitRuns is the distinct left runs the fit composes as the window narrows a column at a time
// from width, in the order they appear, up to and including the first that reads until.
func footerFitRuns(t *testing.T, in footerInput, width int, until string) []string {
	t.Helper()

	var runs []string
	for w := width; w > 0; w-- {
		at := in
		at.width = w
		info := footerFit(at).info
		if len(runs) == 0 || info != runs[len(runs)-1] {
			runs = append(runs, info)
		}
		if info == until {
			return runs
		}
	}
	t.Fatalf("the fit never narrowed to %q: %q", until, runs)
	return nil
}

// TestElidePathStepsDownToItsFloor pins the workdir's middle-elision on its own: the head and the
// tail kept k components deep, k shrinking, and last the floor `…/<last>` — each form strictly
// narrower than the one before, so a path too shallow to save a cell has no forms at all.
func TestElidePathStepsDownToItsFloor(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		path string
		want []string
	}{
		{"a deep home path", footerDeepWorkdir, []string{"~/repos/…/internal/tui", "~/…/tui", "…/tui"}},
		{"a rooted path keeps its root", "/home/user/src/project/internal",
			[]string{"/home/user/…/project/internal", "/home/…/internal", "…/internal"}},
		{"a shallow rooted path has only its floor", "/ws/proj", []string{"…/proj"}},
		{"a relative path", "src/project/internal", []string{"src/…/internal", "…/internal"}},
		{"wide glyphs", "~/項目/内部/ツール", []string{"~/…/ツール", "…/ツール"}},
		{"a path no form shortens", "~/apogee", nil},
		{"the home itself", "~", nil},
		{"nothing at all", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := elidePath(tc.path, newWidthAuthority()); !slices.Equal(got, tc.want) {
				t.Errorf("elidePath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestElideMiddleKeepsBothEnds pins the branch's middle-elision on its own: the head keeps half of
// the cells left beside the ellipsis, the extra one to it, the tail the rest — and a wide glyph
// either cut would split goes whole, so the result never spends a cell past the width it was given.
func TestElideMiddleKeepsBothEnds(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		text  string
		width int
		want  string
	}{
		{"a branch that fits is untouched", footerLongBranch, 24, footerLongBranch},
		{"one cell short", footerLongBranch, 23, "feature/lon…branch-name"},
		{"at the floor", footerLongBranch, 9, "feat…name"},
		{"an even split", footerLongBranch, 10, "featu…name"},
		{"wide glyphs on a clean cut", "機能/長い名前", 9, "機能…名前"},
		{"a wide glyph the tail's cut lands in goes whole", "機能/長い名前", 8, "機能…前"},
		{"a wide glyph the head's cut lands in goes whole", "機能/長い名前", 7, "機…前"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			measure := newWidthAuthority()
			got := elideMiddle(tc.text, tc.width, measure)
			if got != tc.want {
				t.Errorf("elideMiddle(%q, %d) = %q, want %q", tc.text, tc.width, got, tc.want)
			}
			if w := measure.Width(got); w > tc.width {
				t.Errorf("elideMiddle(%q, %d) spends %d cells, want at most %d", tc.text, tc.width, w, tc.width)
			}
		})
	}
}

// TestFooterFitMiddleElidesBeforeItDrops is the shortening ahead of the ladder, read off the fit
// column by column: a row that fits is untouched; then the workdir steps down its middle-elided
// forms to its floor; then the branch gives up a cell a column, its counts whole beside it, down to
// its own floor; and only then does a segment drop — composed with the full workdir and branch
// again, since the drop is what bought the room — and the ladder carries on in its own order.
func TestFooterFitMiddleElidesBeforeItDrops(t *testing.T) {
	t.Parallel()

	in := footerLongCase(t, footerLongBranch)
	const (
		upstream = "apollo ✦ gpt-oss-20b ✦ high ✦ "
		counts   = " 5Δ 0↓ 4↑ ✦ ≥ 0.42 EUR"
	)
	full := upstream + footerDeepWorkdir + " ✦ " + footerLongBranch + counts
	runs := footerFitRuns(t, in, footerSeatWidth(t, in, full, footerOfflineJoined), footerRunModelOnly)

	want := []string{
		full,
		upstream + "~/repos/…/internal/tui ✦ " + footerLongBranch + counts,
		upstream + "~/…/tui ✦ " + footerLongBranch + counts,
		upstream + "…/tui ✦ " + footerLongBranch + counts,
	}
	if len(runs) < len(want) || !slices.Equal(runs[:len(want)], want) {
		t.Fatalf("the fit's runs = %q, want them to open %q", runs, want)
	}

	rest := runs[len(want):]
	branches := 0
	for ; branches < len(rest); branches++ {
		branch, ok := strings.CutPrefix(rest[branches], upstream+"…/tui ✦ ")
		if !ok {
			break
		}
		branch, ok = strings.CutSuffix(branch, counts)
		if !ok || !strings.Contains(branch, "…") {
			t.Fatalf("the fit's run %q, want the branch middle-elided beside its whole counts", rest[branches])
		}
		if wantCells := len([]rune(footerLongBranch)) - 1 - branches; in.measure.Width(branch) != wantCells {
			t.Errorf("the fit's branch %q spends %d cells, want %d — one cell a column", branch, in.measure.Width(branch), wantCells)
		}
	}
	if branches == 0 || !strings.Contains(rest[branches-1], " feat…name ") {
		t.Fatalf("the fit's runs = %q, want the branch elided down to its floor %q", runs, "feat…name")
	}

	// Past both floors the next run is a drop, the full workdir back: the shortened row saved more
	// than the spend alone would, so the spend AND the git segment have gone, and the ladder then
	// goes on in its own order.
	drops := []string{
		"apollo ✦ gpt-oss-20b ✦ high ✦ " + footerDeepWorkdir,
		"apollo ✦ gpt-oss-20b ✦ " + footerDeepWorkdir,
		footerRunNoWorkdir,
		footerRunModelOnly,
	}
	if got := rest[branches:]; !slices.Equal(got, drops) {
		t.Errorf("the fit's runs past both floors = %q, want %q", got, drops)
	}
}

// TestFooterFitPaintsTheElidedBranchFromItsPieces pins the span table's carrier for the git
// segment: once the branch is elided, the git span's pieces are the ELIDED words, re-joining to
// exactly the text the fit measured, and the counts keep their tone — the painter tones these
// pieces rather than re-wording the snapshot, whose full branch would no longer re-join.
func TestFooterFitPaintsTheElidedBranchFromItsPieces(t *testing.T) {
	t.Parallel()

	in := footerLongCase(t, footerLongBranch)
	in.width = footerSeatWidth(t, in,
		"apollo ✦ gpt-oss-20b ✦ high ✦ …/tui ✦ feat…name 5Δ 0↓ 4↑ ✦ ≥ 0.42 EUR", footerOfflineJoined)
	got := footerFit(in)

	git, ok := got.span(footerSegGit)
	if !ok || git.text != "feat…name 5Δ 0↓ 4↑" {
		t.Fatalf("footerFit(width %d) git span = %q (kept %t), want the branch at its floor", in.width, git.text, ok)
	}
	if footerSpansText(git.pieces) != git.text {
		t.Errorf("git span pieces re-join to %q, want %q", footerSpansText(git.pieces), git.text)
	}
	want := []footerSpan{{text: "feat…name", isBranch: true}, {text: "5Δ", bright: true}, {text: "0↓"}, {text: "4↑", bright: true}}
	if !slices.Equal(git.pieces, want) {
		t.Errorf("git span pieces = %+v, want %+v", git.pieces, want)
	}
}

// TestFooterFitNeverOverrunsWithWideGlyphs sweeps a row whose workdir and branch are wide glyphs:
// at every width that seats the marker, the shortened run still leaves the blank column before it,
// since an elision that split a wide glyph would hand back a cell more than the fit counted.
func TestFooterFitNeverOverrunsWithWideGlyphs(t *testing.T) {
	t.Parallel()

	in := withSegment(footerLongCase(t, "機能/とても長い枝の名前"), footerSegWorkdir, "~/項目/内部/ツール/部品")
	sawElided := false
	for w := 160; w >= 0; w-- {
		at := in
		at.width = w
		got := footerFit(at)
		if !got.hasMode || got.info == "" {
			continue
		}
		if left := in.margin + in.measure.Width(got.info) + in.measure.Width(got.offline); left >= got.col {
			t.Errorf("footerFit(width %d) = %q runs to %d, marker at %d — no blank column", w, got.info, left, got.col)
		}
		sawElided = sawElided || strings.Contains(got.info, "…/部品 ✦ 機能")
	}
	if !sawElided {
		t.Error("the sweep never elided the wide workdir and branch together")
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
		layout := m.footerRow(0, w)
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
