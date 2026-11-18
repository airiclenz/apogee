package tui

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
)

// autoCollapseUmbrella builds a two-call umbrella at the top of a transcript with auto-collapse on
// and the Turn running — the shape every settle below starts from: both calls returned, nothing
// after them yet, so the umbrella is the last entry of a running Turn and stands open.
func autoCollapseUmbrella(t *testing.T) *transcript {
	t.Helper()
	tr := &transcript{}
	tr.setToolsAutoCollapse(true)
	tr.setTurnLive(true)
	readCall(tr, "c1", "a.go", 1, 5, 0)
	runCall(tr, "c2", "go test", "ok", 0)
	return tr
}

// TestAutoCollapseSettlesOnWhatFollows pins the settle half of the rule: a done umbrella folds the
// moment a later entry of its own run follows it — narration, a note, a user message — or, with
// nothing after it, the moment its Turn ends.
func TestAutoCollapseSettlesOnWhatFollows(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		follow func(tr *transcript)
	}{
		{"narration", func(tr *transcript) { tr.apply(domain.MessageEvent{Text: "All green."}) }},
		{"note", func(tr *transcript) { tr.addNote("saved") }},
		{"user message", func(tr *transcript) { tr.addUser("next, please", nil) }},
		{"turn end", func(tr *transcript) { tr.setTurnLive(false) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tr := autoCollapseUmbrella(t)
			if tr.umbrellaFolded(0) {
				t.Fatal("umbrellaFolded(0) = true before anything follows a running Turn's last umbrella; want false")
			}
			tc.follow(tr)
			if !tr.umbrellaSettled(0) {
				t.Error("umbrellaSettled(0) = false; want true")
			}
			if !tr.umbrellaFolded(0) {
				t.Error("umbrellaFolded(0) = false on a settled, untouched umbrella under auto-collapse; want true")
			}
			if got := tr.umbrellaFoldBy(0); got != foldBySettled {
				t.Errorf("umbrellaFoldBy(0) = %d; want foldBySettled", got)
			}
		})
	}
}

// TestAutoCollapseKeepsLiveUmbrellasOpen pins the other half: an umbrella with a call still waiting
// for its result stays open even after something follows it, and so does a done one that is the
// last entry of a running Turn.
func TestAutoCollapseKeepsLiveUmbrellasOpen(t *testing.T) {
	t.Parallel()

	t.Run("open call", func(t *testing.T) {
		t.Parallel()

		tr := &transcript{}
		tr.setToolsAutoCollapse(true)
		tr.setTurnLive(false)
		readCall(tr, "c1", "a.go", 1, 5, 0)
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "terminal", Arguments: []byte(`{"command":"go test"}`)}})
		if tr.umbrellaSettled(0) {
			t.Error("umbrellaSettled(0) = true with a call still open; want false")
		}
		if tr.umbrellaFolded(0) {
			t.Error("umbrellaFolded(0) = true on a live umbrella; want false — it folds by its size as before")
		}
	})

	t.Run("last entry of a running turn", func(t *testing.T) {
		t.Parallel()

		tr := autoCollapseUmbrella(t)
		if tr.umbrellaSettled(0) {
			t.Error("umbrellaSettled(0) = true on the last entry of a running Turn; want false")
		}
		if got := tr.umbrellaFoldBy(0); got != foldByHead {
			t.Errorf("umbrellaFoldBy(0) = %d; want foldByHead — a small unsettled umbrella keeps its head flag", got)
		}
	})
}

// TestAutoCollapseFoldsLargeDespiteToolsOpen: settledness comes before size, so a LARGE umbrella
// under an open shared preference folds once settled — and stands open by that preference while it
// is not.
func TestAutoCollapseFoldsLargeDespiteToolsOpen(t *testing.T) {
	t.Parallel()

	tr := autoCollapseUmbrella(t)
	tr.setToolsFoldOver(1)
	tr.setToolsOpen(true)
	if !tr.umbrellaIsLarge(0) {
		t.Fatal("umbrellaIsLarge(0) = false; want a large umbrella for this test")
	}
	if tr.umbrellaFolded(0) {
		t.Fatal("umbrellaFolded(0) = true on an unsettled large umbrella under toolsOpen; want false")
	}
	tr.setTurnLive(false)
	if !tr.umbrellaFolded(0) {
		t.Error("umbrellaFolded(0) = false on a settled large umbrella under toolsOpen; want true — settled comes before size")
	}
}

// TestAutoCollapseExemptsTouchedUmbrellas: an umbrella the reader acted on is never auto-collapsed;
// it folds by its size, or — given a fold of its own — by its head flag at any size.
func TestAutoCollapseExemptsTouchedUmbrellas(t *testing.T) {
	t.Parallel()

	t.Run("touched folds by size", func(t *testing.T) {
		t.Parallel()

		tr := autoCollapseUmbrella(t)
		tr.setTurnLive(false)
		tr.entries[0].umbrellaTouched = true
		if tr.umbrellaFolded(0) {
			t.Error("umbrellaFolded(0) = true on a touched, settled small umbrella; want false — touched is exempt")
		}
	})

	t.Run("touched with its own fold reads the head flag even when large", func(t *testing.T) {
		t.Parallel()

		tr := autoCollapseUmbrella(t)
		tr.setTurnLive(false)
		tr.setToolsFoldOver(1)
		tr.entries[0].umbrellaTouched = true
		tr.entries[0].umbrellaOwnFold = true
		if got := tr.umbrellaFoldBy(0); got != foldByHead {
			t.Fatalf("umbrellaFoldBy(0) = %d; want foldByHead", got)
		}
		if tr.umbrellaFolded(0) {
			t.Error("umbrellaFolded(0) = true with the head flag open; want false — the shared shut preference is not read")
		}
		tr.setUmbrellaFolded(0, true)
		if !tr.umbrellaFolded(0) {
			t.Error("umbrellaFolded(0) = false with the head flag shut; want true")
		}
	})
}

// TestAutoCollapseOffIsTodaysFold: with the switch off a settled umbrella folds exactly by the
// size rule — the head flag when small, the shared preference when large.
func TestAutoCollapseOffIsTodaysFold(t *testing.T) {
	t.Parallel()

	tr := autoCollapseUmbrella(t)
	tr.setToolsAutoCollapse(false)
	tr.setTurnLive(false)
	if !tr.umbrellaSettled(0) {
		t.Fatal("umbrellaSettled(0) = false; want a settled umbrella for this test")
	}
	if tr.umbrellaFolded(0) {
		t.Error("umbrellaFolded(0) = true on a small umbrella with auto-collapse off; want false")
	}
	tr.setToolsFoldOver(1)
	if !tr.umbrellaFolded(0) {
		t.Error("umbrellaFolded(0) = false on a large umbrella under the shut default; want true")
	}
	tr.setToolsOpen(true)
	if tr.umbrellaFolded(0) {
		t.Error("umbrellaFolded(0) = true on a large umbrella under toolsOpen with auto-collapse off; want false")
	}
}

// TestAutoCollapseFoldsReplayedUmbrellas: a resumed scrollback's umbrellas are settled and
// untouched by construction — no Turn runs in a fresh process — so each folds with nothing extra.
func TestAutoCollapseFoldsReplayedUmbrellas(t *testing.T) {
	t.Parallel()

	src := &transcript{}
	readCall(src, "c1", "a.go", 1, 5, 0)
	runCall(src, "c2", "go test", "ok", 0)

	tr := &transcript{}
	tr.setToolsAutoCollapse(true)
	tr.replay(append([]entry(nil), src.entries...))
	if !tr.umbrellaFolded(0) {
		t.Error("umbrellaFolded(0) = false on a replayed umbrella under auto-collapse; want true")
	}
}

// TestAutoCollapseRunViewFoldsOnChildReport: inside a delegated run the umbrella that ends the
// child's work stays open while the child runs — whatever the parent Turn — and folds once the
// child reports, as its run view paints it.
func TestAutoCollapseRunViewFoldsOnChildReport(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	tr.setToolsAutoCollapse(true)
	tr.setTurnLive(true)
	delegationCall(tr, "", "sa1", "scout", "look around", 0)
	subAgentStarted(tr, "sa1", 1)
	delegatedRead(tr, "sa1", "r1", "a.go", 1)
	delegatedRead(tr, "sa1", "r2", "b.go", 1)
	head := 1
	if len(toolSuperGroup(tr.entries, head)) == 0 {
		t.Fatalf("entries[%d] heads no umbrella; want the child's two reads under one", head)
	}
	if tr.umbrellaFolded(head) {
		t.Fatal("umbrellaFolded = true while the child is still running; want false")
	}
	subAgentReport(tr, "sa1", "done", 0)
	if !tr.umbrellaFolded(head) {
		t.Error("umbrellaFolded = false once the child reported; want true")
	}
	tr.setRoot(tr.entries[0].spawned())
	if got := tr.umbrellaFoldBy(head); got != foldBySettled {
		t.Errorf("umbrellaFoldBy = %d in the run view; want foldBySettled", got)
	}
}

// TestAutoCollapseSettleRepaints: the settle reaches the paint key, so a cached paint of the open
// umbrella is not served once a later entry has settled it — the cached frame matches an uncached
// one, and the umbrella's type rows are gone from it.
func TestAutoCollapseSettleRepaints(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	tr := autoCollapseUmbrella(t)
	tr.paints = newPaintCache()
	open := strings.Join(tr.renderLines(th, 80), "\n")

	tr.addNote("saved")
	cached := strings.Join(tr.renderLines(th, 80), "\n")
	fresh := *tr
	fresh.paints = nil
	uncached := strings.Join(fresh.renderLines(th, 80), "\n")
	if cached != uncached {
		t.Errorf("cached paint after the settle differs from an uncached one:\ncached:\n%s\nuncached:\n%s", cached, uncached)
	}
	for _, row := range []string{"┝ Read", "┕ Terminal"} {
		if !strings.Contains(ansiPattern.ReplaceAllString(open, ""), row) {
			t.Errorf("open paint lacks type row %q:\n%s", row, open)
		}
		if strings.Contains(ansiPattern.ReplaceAllString(cached, ""), row) {
			t.Errorf("settled paint still shows type row %q — the cache served the open paint:\n%s", row, cached)
		}
	}
}

// TestAutoCollapseWatchedTurn: while watching, the watched Turn's last umbrella stays open while
// that Turn runs and folds once it goes idle — watchIdle ends a watched Turn, since it never
// reaches finishWorker; and the watcher's own Turn settles in finishWorker.
func TestAutoCollapseWatchedTurn(t *testing.T) {
	t.Parallel()

	t.Run("watched", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(t)
		m.transcript.setToolsAutoCollapse(true)
		readCall(&m.transcript, "c1", "a.go", 1, 5, 0)
		runCall(&m.transcript, "c2", "go test", "ok", 0)
		head := len(m.transcript.entries) - 2
		m.watchRunning()
		if m.transcript.umbrellaFolded(head) {
			t.Fatal("umbrellaFolded = true while the watched Turn runs; want false")
		}
		m.watchIdle()
		if !m.transcript.umbrellaFolded(head) {
			t.Error("umbrellaFolded = false once the watched Turn went idle; want true")
		}
	})

	t.Run("own", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(t)
		m.transcript.setToolsAutoCollapse(true)
		m.markRunning()
		readCall(&m.transcript, "c1", "a.go", 1, 5, 0)
		runCall(&m.transcript, "c2", "go test", "ok", 0)
		head := len(m.transcript.entries) - 2
		if m.transcript.umbrellaFolded(head) {
			t.Fatal("umbrellaFolded = true while the Turn runs; want false")
		}
		m.finishWorker(stateIdle)
		if !m.transcript.umbrellaFolded(head) {
			t.Error("umbrellaFolded = false once the Turn finished; want true")
		}
	})
}
