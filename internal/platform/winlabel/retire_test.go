package winlabel

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestRetireLabelJournalKeepsTheFileWhenTheRevertFails(t *testing.T) {
	t.Parallel()

	// The journal is the ONLY record of the labels apogee put on the disk, so the retention
	// rule is a safety property, not bookkeeping: removing it after a failed revert would
	// strand those labels with nothing left to describe them. The revert itself is Windows-only
	// (it calls SetNamedSecurityInfo), so it is injected here and the DECISION is proven on
	// every OS.
	revertFailed := errors.New("clear the mandatory label of \"C:\\\\work\": access is denied")

	tests := []struct {
		name       string
		revertErr  error
		wantKept   bool
		wantErrIs  error
		wantNoFile bool
	}{
		{
			name:       "successful_revert_removes_the_journal",
			wantNoFile: true,
		},
		{
			name:      "failed_revert_keeps_the_journal_for_the_next_run",
			revertErr: revertFailed,
			wantKept:  true,
			wantErrIs: revertFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			path := JournalPath(home, 1234)
			journal := Record{PID: 1234, Entries: []Entry{{Path: `C:\work`, Root: true}}}
			if err := WriteJournal(path, journal); err != nil {
				t.Fatalf("seed journal: %v", err)
			}

			var seen Record
			_, err := retire(path, journal, func(r Record) ([]Entry, error) {
				seen = r
				return nil, tt.revertErr
			})
			if !errors.Is(err, tt.wantErrIs) {
				t.Fatalf("retire err = %v, want %v", err, tt.wantErrIs)
			}
			if len(seen.Entries) != 1 || seen.Entries[0].Path != `C:\work` {
				t.Errorf("the revert was handed %+v, want the journal itself", seen)
			}

			_, statErr := os.Stat(path)
			if tt.wantKept && statErr != nil {
				t.Errorf("the journal was removed after a FAILED revert (%v); the labels it describes would be stranded", statErr)
			}
			if tt.wantNoFile && statErr == nil {
				t.Error("the journal survived a successful revert; a stale journal reports residue that is not there")
			}
		})
	}
}

func TestRetireLabelJournalWithoutAJournalFile(t *testing.T) {
	t.Parallel()

	// A backend with no journal location (no resolvable user profile) has nothing to remove,
	// so the revert outcome passes straight through — in both directions.
	if _, err := retire("", Record{}, func(Record) ([]Entry, error) { return nil, nil }); err != nil {
		t.Errorf("retire(\"\") = %v, want nil", err)
	}
	sentinel := errors.New("revert failed")
	if _, err := retire("", Record{}, func(Record) ([]Entry, error) { return nil, sentinel }); !errors.Is(err, sentinel) {
		t.Errorf("retire(\"\") = %v, want the revert error", err)
	}

	// An already-absent journal file is not a failure: recovery may run twice over the same
	// home, and the second pass must not invent an error out of work already done.
	gone := JournalPath(t.TempDir(), 7)
	if _, err := retire(gone, Record{}, func(Record) ([]Entry, error) { return nil, nil }); err != nil {
		t.Errorf("retire on a missing file = %v, want nil", err)
	}
}

func TestRetireLabelJournalRewritesTheFileToTheHandedOffEntries(t *testing.T) {
	t.Parallel()

	// The third fate a journal can meet, beside "retired" and "kept whole": the revert
	// succeeded but handed entries off — a foreign prior under a root a sibling journal still
	// claims. Those entries are undischarged instructions, so the file must survive REWRITTEN
	// to exactly them under its original owner: removing it would lose the only record of the
	// foreign label (the previously-lost handoff), and keeping it whole would re-clear roots
	// whose obligation already transferred. The remains are also returned, so a session
	// backend keeps its in-memory journal in step and a repeated Close converges instead of
	// deleting the handoff record.
	home := t.TempDir()
	path := JournalPath(home, 4321)
	journal := Record{PID: 4321, Entries: []Entry{
		{Path: `C:\work`, Root: true, PriorSDDL: "S:AI(ML;OICI;NW;;;ME)"},
		{Path: `C:\scratch`, Root: true},
	}}
	if err := WriteJournal(path, journal); err != nil {
		t.Fatalf("seed journal: %v", err)
	}

	handoff := []Entry{{Path: `C:\work`, Root: true, PriorSDDL: "S:AI(ML;OICI;NW;;;ME)"}}
	remaining, err := retire(path, journal, func(Record) ([]Entry, error) {
		return handoff, nil
	})
	if err != nil {
		t.Fatalf("retire = %v, want nil — a handoff is not a failed revert", err)
	}
	if len(remaining) != 1 || remaining[0] != handoff[0] {
		t.Fatalf("remaining = %+v, want the handed-off entry back verbatim", remaining)
	}

	kept, err := ReadJournal(path)
	if err != nil {
		t.Fatalf("the journal did not survive the handoff: %v", err)
	}
	if kept.PID != journal.PID {
		t.Errorf("rewritten journal PID = %d, want the original owner %d", kept.PID, journal.PID)
	}
	if len(kept.Entries) != 1 || kept.Entries[0] != handoff[0] {
		t.Errorf("rewritten journal entries = %+v, want only the handed-off entry — the discharged root must not be re-cleared later", kept.Entries)
	}

	// With nothing handed off the same journal retires fully — the handoff is the ONLY thing
	// that keeps a successfully reverted journal alive.
	if remaining, err := retire(path, kept, func(Record) ([]Entry, error) {
		return nil, nil
	}); err != nil || len(remaining) != 0 {
		t.Fatalf("retire (final) = %+v, %v; want a full retirement", remaining, err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("the journal survived a revert that handed nothing off; a stale journal reports residue that is not there")
	}
}

func TestRestorablePriorsHandsOffSiblingClaimedTrees(t *testing.T) {
	t.Parallel()

	// The split that preserves a foreign prior under sibling concurrency, decided purely so it
	// is provable on every OS. A prior at or under a root ANY sibling journal still names as a
	// Root entry is handed off, not restored: the sibling's pending clear — at its own
	// teardown, or at recovery once it is dead — would wipe a label restored now, and the
	// sibling journalled no prior of its own (it saw only apogee's Low label), so the record
	// would be lost with it. Liveness is deliberately absent from the signature: the sibling
	// FILE is the undischarged claim either way.
	const (
		foreignMedium = "S:AI(ML;OICI;NW;;;ME)"
		foreignHigh   = "S:(ML;;NW;;;HI)"
	)
	journal := Record{PID: 100, Entries: []Entry{
		{Path: `C:\work`, Root: true, PriorSDDL: foreignMedium},
		{Path: `C:\work\vendor\lib.dll`, PriorSDDL: foreignHigh},
		{Path: `C:\scratch`, Root: true},
	}}

	tests := []struct {
		name        string
		siblings    []Record
		wantRestore map[string]string
		wantHandoff []Entry
	}{
		{
			name: "no_siblings_restores_everything",
			wantRestore: map[string]string{
				`C:\work`:                foreignMedium,
				`C:\work\vendor\lib.dll`: foreignHigh,
			},
		},
		{
			name: "sibling_claim_on_the_shared_root_hands_off_the_root_prior_and_its_descendants",
			siblings: []Record{
				{PID: 200, Entries: []Entry{{Path: `C:\work`, Root: true}}},
			},
			wantRestore: map[string]string{},
			wantHandoff: []Entry{
				{Path: `C:\work`, Root: true, PriorSDDL: foreignMedium},
				{Path: `C:\work\vendor\lib.dll`, PriorSDDL: foreignHigh},
			},
		},
		{
			name: "case_folded_claim_names_the_same_tree",
			siblings: []Record{
				{PID: 200, Entries: []Entry{{Path: `c:\WORK`, Root: true}}},
			},
			wantRestore: map[string]string{},
			wantHandoff: []Entry{
				{Path: `C:\work`, Root: true, PriorSDDL: foreignMedium},
				{Path: `C:\work\vendor\lib.dll`, PriorSDDL: foreignHigh},
			},
		},
		{
			name: "claim_on_an_unrelated_root_hands_off_nothing",
			siblings: []Record{
				{PID: 200, Entries: []Entry{{Path: `C:\scratch`, Root: true}}},
			},
			wantRestore: map[string]string{
				`C:\work`:                foreignMedium,
				`C:\work\vendor\lib.dll`: foreignHigh,
			},
		},
		{
			name: "a_sibling_prefix_root_is_not_a_containing_root",
			siblings: []Record{
				{PID: 200, Entries: []Entry{{Path: `C:\wo`, Root: true}}},
			},
			wantRestore: map[string]string{
				`C:\work`:                foreignMedium,
				`C:\work\vendor\lib.dll`: foreignHigh,
			},
		},
		{
			name: "a_siblings_prior_only_entry_claims_no_tree",
			siblings: []Record{
				{PID: 200, Entries: []Entry{{Path: `C:\work`, PriorSDDL: foreignMedium}}},
			},
			wantRestore: map[string]string{
				`C:\work`:                foreignMedium,
				`C:\work\vendor\lib.dll`: foreignHigh,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			restore, handoff := restorablePriors(journal, tt.siblings)
			if len(restore) != len(tt.wantRestore) {
				t.Fatalf("restore = %v, want %v", restore, tt.wantRestore)
			}
			for path, want := range tt.wantRestore {
				if restore[path] != want {
					t.Errorf("restore[%q] = %q, want %q", path, restore[path], want)
				}
			}
			if len(handoff) != len(tt.wantHandoff) {
				t.Fatalf("handoff = %+v, want %+v", handoff, tt.wantHandoff)
			}
			for i, want := range tt.wantHandoff {
				if handoff[i] != want {
					t.Errorf("handoff[%d] = %+v, want %+v — the entry must survive verbatim, Root flag included", i, handoff[i], want)
				}
			}
		})
	}
}

func TestClearTreeOutcome(t *testing.T) {
	t.Parallel()

	// The below-root accounting ClearTree hands to retire's decision. A nil
	// verdict is what retires the journal, so it may only ever mean "every descendant is
	// verifiably cleared or gone"; any remaining failure must surface as an error naming the
	// first one and the count, which keeps the journal for the next run. The walk itself is
	// Windows-only (it clears real labels), so the verdict is proven here on every OS — the
	// same seam TestRetireLabelJournalKeepsTheFileWhenTheRevertFails uses.
	first := errors.New(`"C:\work\stuck.txt": access is denied`)

	if err := clearTreeOutcome(`C:\work`, 0, nil); err != nil {
		t.Errorf("clearTreeOutcome with no failures = %v, want nil so the journal is retired", err)
	}

	err := clearTreeOutcome(`C:\work`, 3, first)
	if err == nil {
		t.Fatal("clearTreeOutcome with 3 failures = nil; the journal would be retired over labels still on the disk")
	}
	if !errors.Is(err, first) {
		t.Errorf("err = %v, want the first failure wrapped so callers can inspect it", err)
	}
	for _, want := range []string{"3 path(s)", `C:\work`, first.Error()} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to contain %q", err, want)
		}
	}
}

func TestRevertibleRootsSparesOnlyALiveSiblingsRoots(t *testing.T) {
	t.Parallel()

	// Two sessions confining one workspace journal the same root, and the first to tear down
	// must not strip the label out from under the survivor — its memoised label pass would
	// never re-label, and every later confined write in that session would be denied. The
	// exclusion is decided here, purely: this journal's roots minus every root a LIVE sibling
	// journal also names. A spared root is not a failed revert — the sibling's own Root entry
	// carries the clear obligation, so this journal may still retire — and a DEAD sibling
	// spares nothing, because its roots are an interrupted run recovery clears anyway.
	journal := Record{PID: 100, Entries: []Entry{
		{Path: `C:\work`, Root: true},
		{Path: `C:\scratch`, Root: true},
	}}

	tests := []struct {
		name     string
		siblings []Record
		live     map[int]bool
		want     []string
	}{
		{
			name: "no_siblings_keeps_every_root",
			want: []string{`C:\work`, `C:\scratch`},
		},
		{
			name: "live_sibling_spares_the_shared_root_only",
			siblings: []Record{
				{PID: 200, Entries: []Entry{{Path: `C:\work`, Root: true}}},
			},
			live: map[int]bool{200: true},
			want: []string{`C:\scratch`},
		},
		{
			name: "dead_sibling_spares_nothing",
			siblings: []Record{
				{PID: 200, Entries: []Entry{{Path: `C:\work`, Root: true}}},
			},
			want: []string{`C:\work`, `C:\scratch`},
		},
		{
			name: "case_folded_spelling_names_the_same_root",
			siblings: []Record{
				{PID: 200, Entries: []Entry{{Path: `c:\WORK`, Root: true}}},
			},
			live: map[int]bool{200: true},
			want: []string{`C:\scratch`},
		},
		{
			name: "a_live_siblings_prior_only_entry_claims_no_root",
			siblings: []Record{
				{PID: 200, Entries: []Entry{{Path: `C:\work`, PriorSDDL: "S:(ML;;NW;;;ME)"}}},
			},
			live: map[int]bool{200: true},
			want: []string{`C:\work`, `C:\scratch`},
		},
		{
			name: "both_roots_claimed_by_live_siblings_spares_everything",
			siblings: []Record{
				{PID: 200, Entries: []Entry{{Path: `C:\work`, Root: true}}},
				{PID: 300, Entries: []Entry{{Path: `C:\scratch`, Root: true}}},
			},
			live: map[int]bool{200: true, 300: true},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			alive := func(pid int) bool { return tt.live[pid] }
			got := revertibleRoots(journal, tt.siblings, alive)
			if len(got) != len(tt.want) {
				t.Fatalf("revertibleRoots = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("revertibleRoots[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
