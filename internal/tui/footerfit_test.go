package tui

import (
	"strings"
	"testing"
)

// The footer's five facts and its marker, as one narrow window after another takes them away.
// The literals are deliberately spelled out rather than composed from the code under test: the
// drop ORDER is the contract, and a test that built its own expectation with the same joiner
// would agree with a wrong order as happily as with the right one.
const (
	footerTestHost    = "apollo"
	footerTestModel   = "gpt-oss-20b"
	footerTestEffort  = "high"
	footerTestWorkdir = "~/apogee"
	footerTestMode    = "⏵⏵ auto · confined"

	footerRunFull       = "apollo ✦ gpt-oss-20b ✦ high ✦ ~/apogee"
	footerRunNoEffort   = "apollo ✦ gpt-oss-20b ✦ ~/apogee"
	footerRunNoWorkdir  = "apollo ✦ gpt-oss-20b"
	footerRunModelOnly  = "gpt-oss-20b"
	footerOfflineJoined = " ✦ offline"
)

// footerFitCase is the full five-segment footer every case below narrows, offline included so the
// ladder is exercised with the one segment it may never drop before the model.
func footerFitCase() footerInput {
	measure := newWidthAuthority()
	return footerInput{
		host:    footerTestHost,
		model:   footerTestModel,
		effort:  footerTestEffort,
		workdir: footerTestWorkdir,
		offline: offlineLabel,
		mode:    footerTestMode,
		margin:  measure.Width(bodyIndent),
		measure: measure,
	}
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
// has gone. The order is the order the row is read for, outward-in: the effort word first, then
// the workdir, then the host.
func TestFooterFitDropsSegmentsInPriorityOrder(t *testing.T) {
	t.Parallel()

	in := footerFitCase()
	rungs := []struct {
		name string
		info string
	}{
		{"everything the session knows", footerRunFull},
		{"the effort word goes first", footerRunNoEffort},
		{"then the workdir", footerRunNoWorkdir},
		{"then the host", footerRunModelOnly},
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
// over positions: a session whose server reports no effort dial has nothing to drop at priority 3,
// so the first segment the narrowing row gives up is the workdir.
func TestFooterFitSkipsASegmentNothingNamed(t *testing.T) {
	t.Parallel()

	in := footerFitCase()
	in.effort = ""

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
// the host, the workdir and the effort word have all gone.
func TestFooterFitKeepsOfflineThroughEveryLadderDrop(t *testing.T) {
	t.Parallel()

	in := footerFitCase()
	for _, info := range []string{footerRunFull, footerRunNoEffort, footerRunNoWorkdir, footerRunModelOnly} {
		at := in
		at.width = footerSeatWidth(t, in, info, footerOfflineJoined)
		if got := footerFit(at); got.offline != footerOfflineJoined {
			t.Errorf("footerFit(width %d).offline = %q, want %q while %q still shows",
				at.width, got.offline, footerOfflineJoined, info)
		}
	}
}

// TestFooterFitLeavesNoDanglingSeparator sweeps every width for the one shape a hand-joined row
// gets wrong: a segment that left without the ✦ that introduced it.
func TestFooterFitLeavesNoDanglingSeparator(t *testing.T) {
	t.Parallel()

	in := footerFitCase()
	for w := 0; w <= footerSeatWidth(t, in, footerRunFull, footerOfflineJoined)+10; w++ {
		at := in
		at.width = w
		got := footerFit(at)

		row := got.info + got.offline
		if strings.HasPrefix(row, " ") || strings.HasSuffix(row, " ") ||
			strings.HasPrefix(row, glyphAssistant) || strings.HasSuffix(row, glyphAssistant) {
			t.Errorf("footerFit(width %d) left run = %q, want no dangling separator", w, row)
		}
	}
}
