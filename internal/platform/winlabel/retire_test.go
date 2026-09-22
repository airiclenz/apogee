package winlabel

import (
	"errors"
	"io/fs"
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
	// Judged is set on both priors because the pre-clear pass (judgeEntries) always runs
	// first in production and an UNJUDGED prior is handed off rather than restored whatever
	// the siblings say — the restore prong of F-08, tabled separately below.
	journal := Record{PID: 100, Entries: []Entry{
		{Path: `C:\work`, Root: true, PriorSDDL: foreignMedium, Judged: true},
		{Path: `C:\work\vendor\lib.dll`, PriorSDDL: foreignHigh, Judged: true},
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
				{Path: `C:\work`, Root: true, PriorSDDL: foreignMedium, Judged: true},
				{Path: `C:\work\vendor\lib.dll`, PriorSDDL: foreignHigh, Judged: true},
			},
		},
		{
			name: "case_folded_claim_names_the_same_tree",
			siblings: []Record{
				{PID: 200, Entries: []Entry{{Path: `c:\WORK`, Root: true}}},
			},
			wantRestore: map[string]string{},
			wantHandoff: []Entry{
				{Path: `C:\work`, Root: true, PriorSDDL: foreignMedium, Judged: true},
				{Path: `C:\work\vendor\lib.dll`, PriorSDDL: foreignHigh, Judged: true},
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

func TestRestorablePriorsNeverRestoresAnUnjudgedPrior(t *testing.T) {
	t.Parallel()

	// F-08's restore prong held shut at the last seam. What this function puts in the restore
	// map is handed to revertJournal and written to the disk with SetSDDL, so the ONLY thing
	// between a journal a stranger planted under the apogee home and that write is the
	// pre-clear verdict (judgeEntries), which sets Judged exactly when apogee's own Low label
	// was found on the path. An unjudged prior — a planted one, or one carried because the
	// path wore someone else's label — goes to the hand-off, which writes nothing and merely
	// keeps the record alive for a run that reads the path again.
	const planted = "S:(ML;OICI;NW;;;HI)"
	journal := Record{PID: 100, Entries: []Entry{
		{Path: `C:\Windows\System32\drivers\etc\hosts`, PriorSDDL: planted},
		{Path: `C:\work\carried.dll`, PriorSDDL: planted, Carried: 2},
		{Path: `C:\work\vouched.dll`, PriorSDDL: planted, Judged: true},
	}}

	restore, handoff := restorablePriors(journal, nil)

	if len(restore) != 1 || restore[`C:\work\vouched.dll`] != planted {
		t.Fatalf("restore = %v, want only the judged prior — an unjudged entry must never reach SetSDDL", restore)
	}
	want := []Entry{journal.Entries[0], journal.Entries[1]}
	if len(handoff) != len(want) {
		t.Fatalf("handoff = %+v, want %+v", handoff, want)
	}
	for i := range want {
		if handoff[i] != want[i] {
			t.Errorf("handoff[%d] = %+v, want %+v verbatim — the carry count travels with the entry", i, handoff[i], want[i])
		}
	}
}

func TestRetireCarriesAForeignPriorUntilThePathIsApogeesAgain(t *testing.T) {
	t.Parallel()

	// The carry end to end, over a real journal file: run one finds someone else's label on
	// the path, so the prior is neither written back nor thrown away and the journal survives
	// rewritten to carry it; run two finds apogee's own mark back on the path, restores the
	// prior it would previously have destroyed, and retires the journal. Before the carry
	// existed the first run dropped the prior for good and the foreign label was unrecoverable.
	const (
		foreignPrior = "S:AI(ML;OICI;NW;;;ME)"
		someoneElses = "S:(ML;OICI;NW;;;HI)"
	)
	const labelled = `C:\work\vendor.dll`
	path := JournalPath(t.TempDir(), 4242)
	journal := Record{PID: 4242, Entries: []Entry{{Path: labelled, PriorSDDL: foreignPrior}}}
	if err := WriteJournal(path, journal); err != nil {
		t.Fatalf("seed journal: %v", err)
	}

	var restored map[string]string
	carried, err := retire(path, journal, judgingRevert(staticLabel(someoneElses), &restored))
	if err != nil {
		t.Fatalf("retire (first run) = %v, want nil — a carry is not a failed revert", err)
	}
	if len(restored) != 0 {
		t.Errorf("the first run restored %v; nothing of apogee's was on the path", restored)
	}
	if len(carried) != 1 || carried[0].PriorSDDL != foreignPrior || carried[0].Carried != 1 {
		t.Fatalf("remaining = %+v, want the prior carried forward once", carried)
	}
	kept, err := ReadJournal(path)
	if err != nil {
		t.Fatalf("the journal did not survive the carry: %v", err)
	}
	if len(kept.Entries) != 1 || kept.Entries[0] != carried[0] {
		t.Fatalf("rewritten journal = %+v, want the carried entry — the record of the foreign label must survive the run", kept.Entries)
	}

	second, err := retire(path, kept, judgingRevert(readsApogeesOwnLabel, &restored))
	if err != nil {
		t.Fatalf("retire (second run) = %v, want nil", err)
	}
	if restored[labelled] != foreignPrior {
		t.Errorf("restored = %v, want the carried prior put back once apogee's own mark was on the path again", restored)
	}
	if len(second) != 0 {
		t.Errorf("remaining = %+v, want nothing left to carry", second)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the journal survived a revert that discharged everything; a stale journal reports residue that is not there")
	}
}

// judgingRevert mirrors revertSparingLiveSiblings' pre-clear pass and restore split
// (walk_windows.go) over the pure deciders it composes, so a prior's whole life — judged
// before the clear, carried while the path is not apogee's, restored once it is again — is
// drivable on any OS. The label read is scripted rather than a real SACL and the restore is
// recorded rather than written: what these cases assert is which priors reach SetSDDL and what
// the journal keeps, not the write itself.
func judgingRevert(read func(string) (string, error), restored *map[string]string) func(Record) ([]Entry, error) {
	return func(r Record) ([]Entry, error) {
		if _, _, err := judgeEntries(r.Entries, read); err != nil {
			return nil, err
		}
		restore, handoff := restorablePriors(r, nil)
		*restored = restore
		return handoff, nil
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
	// journal also names. A spared root is not a failed revert, but it is handed BACK to this
	// journal (handoffSparedRoots) rather than dropped, so the file survives carrying it and
	// whichever of the two sessions closes last is the one that clears the tree — and a DEAD
	// sibling spares nothing, because its roots are an interrupted run recovery clears anyway.
	//
	// The label read is stubbed to apogee's own mark throughout, so this table sees the sibling
	// rule alone; the clearability rule it composes with has its own table below.
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
			got, _ := revertibleRoots(journal, tt.siblings, alive, readsApogeesOwnLabel)
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

func TestPriorRestorableTable(t *testing.T) {
	t.Parallel()

	// The read-side check that closes F-08: a journal is an instruction to WRITE mandatory
	// labels, and until this decision existed the revert obeyed it unconditionally — so a
	// journal planted under the apogee home relabelled whatever paths it named. Apogee's own
	// Low label still sitting on the path is the whole warrant, and it is read BEFORE the
	// clear that would remove it (judgePriors). The read itself is Windows-only (ReadSDDL
	// needs a real SACL), so the verdict is proven here on every OS — the retire seam pattern.
	const (
		ownLabel      = "S:AI(ML;OICIID;NW;;;LW)" // the inherited spelling a labelled root propagates
		canonicalLow  = "S:AI(ML;;NW;;;S-1-16-4096)"
		foreignMedium = "S:AI(ML;;NW;;;ME)"
		foreignHigh   = "S:(ML;OICI;NW;;;HI)"
	)
	denied := errors.New("access is denied")

	// A label that is not apogee's CARRIES rather than drops: the prior is not apogee's to
	// write back on this run, but the journalled descriptor is the only record of what the
	// path wore before the run, and dropping it destroys that record for good over a state —
	// a policy refresh, another tool's label — that a later revert may well find gone.
	tests := []struct {
		name    string
		current string
		readErr error
		want    priorVerdict
	}{
		{name: "apogees_own_low_label_is_restorable", current: ownLabel, want: priorRestore},
		{name: "the_canonical_low_sid_is_the_same_mark", current: canonicalLow, want: priorRestore},
		{name: "a_foreign_medium_label_carries_the_instruction", current: foreignMedium, want: priorCarry},
		{name: "a_foreign_high_label_carries_the_instruction", current: foreignHigh, want: priorCarry},
		{name: "an_unlabelled_path_carries_the_instruction", current: "", want: priorCarry},
		{name: "a_vanished_path_drops_the_instruction", readErr: os.ErrNotExist, want: priorDrop},
		{
			name:    "a_vanished_path_reported_as_a_path_error_drops_too",
			readErr: &fs.PathError{Op: "read", Path: `C:\work\gone.txt`, Err: fs.ErrNotExist},
			want:    priorDrop,
		},
		{name: "an_unreadable_path_is_neither_restored_carried_nor_dropped", readErr: denied, want: priorUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := priorRestorable(tt.current, tt.readErr); got != tt.want {
				t.Errorf("priorRestorable(%q, %v) = %v; want %v", tt.current, tt.readErr, got, tt.want)
			}
		})
	}
}

func TestCarryPriorBoundsTheCarrysLife(t *testing.T) {
	t.Parallel()

	// A carry keeps its journal on the disk — retire rewrites the file for whatever remains —
	// so an unbounded one would leave a journal, and the residue notice over it, on the
	// machine forever. The count is what makes the journal always retire in the end.
	for carried := 0; carried < maxPriorCarries-1; carried++ {
		next, exhausted := carryPrior(carried)
		if next != carried+1 || exhausted {
			t.Errorf("carryPrior(%d) = %d, %v; want %d, false — the carry still has runs left",
				carried, next, exhausted, carried+1)
		}
	}
	if next, exhausted := carryPrior(maxPriorCarries - 1); next != maxPriorCarries || !exhausted {
		t.Errorf("carryPrior(%d) = %d, %v; want %d, true — the bound must end the carry",
			maxPriorCarries-1, next, exhausted, maxPriorCarries)
	}
}

func TestJudgeEntriesCarriesAForeignPriorAndDropsItAtTheBound(t *testing.T) {
	t.Parallel()

	// The pre-clear pass's own handling of the carry, tabled off the injected label read so it
	// runs on every OS. A carried entry keeps its descriptor and stays UNJUDGED — which is
	// what keeps restorablePriors from ever writing it back — and settles nothing, so a
	// journal rewrite that fails costs only the count. The revert that exhausts the bound
	// discards the descriptor instead, and that IS a settled verdict: it destroys a record.
	const (
		foreignPrior  = "S:AI(ML;OICI;NW;;;ME)"
		someoneElses  = "S:(ML;OICI;NW;;;HI)"
		labelledByUs  = apogeesOwnLowLabel
		carriedToLast = maxPriorCarries - 1
	)

	tests := []struct {
		name        string
		entry       Entry
		current     string
		wantPrior   string
		wantCarried int
		wantJudged  bool
		wantChanged bool
		wantSettled bool
	}{
		{
			name:        "a_foreign_label_carries_the_prior_forward_unjudged",
			entry:       Entry{Path: `C:\work\vendor.dll`, PriorSDDL: foreignPrior},
			current:     someoneElses,
			wantPrior:   foreignPrior,
			wantCarried: 1,
			wantChanged: true,
		},
		{
			name:        "an_unlabelled_path_carries_too_rather_than_losing_the_record",
			entry:       Entry{Path: `C:\work\vendor.dll`, PriorSDDL: foreignPrior},
			current:     "",
			wantPrior:   foreignPrior,
			wantCarried: 1,
			wantChanged: true,
		},
		{
			name:        "the_carry_past_its_bound_drops_the_prior_and_settles_it",
			entry:       Entry{Path: `C:\work\vendor.dll`, PriorSDDL: foreignPrior, Carried: carriedToLast},
			current:     someoneElses,
			wantPrior:   "",
			wantCarried: maxPriorCarries,
			wantChanged: true,
			wantSettled: true,
		},
		{
			name:        "apogees_own_label_judges_the_carried_prior_restorable",
			entry:       Entry{Path: `C:\work\vendor.dll`, PriorSDDL: foreignPrior, Carried: 3},
			current:     labelledByUs,
			wantPrior:   foreignPrior,
			wantCarried: 3,
			wantJudged:  true,
			wantChanged: true,
			wantSettled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			entries := []Entry{tt.entry}
			changed, settled, err := judgeEntries(entries, staticLabel(tt.current))
			if err != nil {
				t.Fatalf("judgeEntries = %v, want nil", err)
			}
			got := entries[0]
			if got.PriorSDDL != tt.wantPrior || got.Carried != tt.wantCarried || got.Judged != tt.wantJudged {
				t.Errorf("entry = %+v; want prior %q, carried %d, judged %v",
					got, tt.wantPrior, tt.wantCarried, tt.wantJudged)
			}
			if changed != tt.wantChanged || settled != tt.wantSettled {
				t.Errorf("judgeEntries = changed %v, settled %v; want changed %v, settled %v",
					changed, settled, tt.wantChanged, tt.wantSettled)
			}
		})
	}
}

// staticLabel is the label-read seam answering one descriptor for every path — the disk state
// a whole table row is written against.
func staticLabel(current string) func(string) (string, error) {
	return func(string) (string, error) { return current, nil }
}

// apogeesOwnLowLabel is the inherited spelling a labelled root propagates — the mark
// rootClearable takes as apogee's warrant to clear a tree — and readsApogeesOwnLabel is the
// label-read seam that answers it for every path, the state the disk is really in when a
// revert runs over its own journal.
const apogeesOwnLowLabel = "S:AI(ML;OICIID;NW;;;LW)"

func readsApogeesOwnLabel(string) (string, error) { return apogeesOwnLowLabel, nil }

func TestRootClearableTable(t *testing.T) {
	t.Parallel()

	// The CLEAR-side check that closes F-08's second prong. The restore side was vouched for
	// first, while the revert still handed every Root a journal named to ClearTree — a NULL
	// SACL over the whole tree — so a journal planted under the apogee home stripped the
	// mandatory label off whatever it pointed at. Apogee's own Low label still on the root is
	// the warrant, a volume root is refused whatever it carries, and the read itself is
	// Windows-only (ReadSDDL needs a real SACL), so the verdict is proven here on every OS —
	// the retire seam pattern.
	const (
		canonicalLow  = "S:AI(ML;;NW;;;S-1-16-4096)"
		foreignMedium = "S:AI(ML;;NW;;;ME)"
		foreignHigh   = "S:(ML;OICI;NW;;;HI)"
	)
	denied := errors.New("access is denied")

	tests := []struct {
		name    string
		root    string
		current string
		readErr error
		want    bool
	}{
		{name: "apogees_own_low_label_clears_the_tree", root: `C:\work`, current: apogeesOwnLowLabel, want: true},
		{name: "the_canonical_low_sid_is_the_same_mark", root: `C:\work`, current: canonicalLow, want: true},
		{name: "a_forward_slash_spelling_is_the_same_tree", root: "C:/work/box", current: canonicalLow, want: true},
		{name: "a_share_subdirectory_clears", root: `\\server\share\box`, current: canonicalLow, want: true},
		{name: "a_foreign_medium_label_refuses", root: `C:\work`, current: foreignMedium},
		{name: "a_foreign_high_label_refuses", root: `C:\work`, current: foreignHigh},
		{name: "an_unlabelled_root_refuses", root: `C:\work`},
		{name: "an_unreadable_root_refuses", root: `C:\work`, current: canonicalLow, readErr: denied},
		{name: "a_vanished_root_refuses", root: `C:\work`, readErr: os.ErrNotExist},
		{
			name:    "a_vanished_root_reported_as_a_path_error_refuses",
			root:    `C:\work`,
			readErr: &fs.PathError{Op: "read", Path: `C:\work`, Err: fs.ErrNotExist},
		},
		// The volume-root refusal is the guardrail windowsLabelGuardrail makes on the way IN,
		// spelled here over both separators: nothing above a box may be labelled, so nothing
		// above a box may be cleared, whatever label the journal found there.
		{name: "a_drive_root_refuses_despite_apogees_label", root: `C:\`, current: apogeesOwnLowLabel},
		{name: "a_forward_slash_drive_root_refuses", root: "C:/", current: apogeesOwnLowLabel},
		{name: "a_bare_drive_refuses", root: "C:", current: apogeesOwnLowLabel},
		{name: "a_unc_share_root_refuses", root: `\\server\share`, current: apogeesOwnLowLabel},
		{name: "a_trailing_separator_does_not_make_a_share_root_a_tree", root: `\\server\share\`, current: apogeesOwnLowLabel},
		{name: "a_forward_slash_unc_share_root_refuses", root: "//server/share", current: apogeesOwnLowLabel},
		{name: "a_bare_separator_refuses", root: `\`, current: apogeesOwnLowLabel},
		{name: "an_empty_root_refuses", current: apogeesOwnLowLabel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := rootClearable(tt.root, tt.current, tt.readErr); got != tt.want {
				t.Errorf("rootClearable(%q, %q, %v) = %v, want %v", tt.root, tt.current, tt.readErr, got, tt.want)
			}
		})
	}
}

func TestRevertibleRootsClearsOnlyRootsApogeesOwnLabelVouchesFor(t *testing.T) {
	t.Parallel()

	// F-08's second prong composed with the sibling rule: a journal is an instruction to STRIP
	// mandatory labels off whole trees, so a root a planted or corrupted journal names is
	// cleared only where apogee's own Low label still stands, and a root a live sibling still
	// claims is spared as before. Both filters run here, purely, because the label read is
	// Windows-only.
	const foreignMedium = "S:AI(ML;;NW;;;ME)"
	alwaysAlive := func(int) bool { return true }

	tests := []struct {
		name     string
		journal  Record
		siblings []Record
		read     func(string) (string, error)
		want     []string
	}{
		{
			name:    "a_planted_root_is_dropped_while_the_labelled_one_survives",
			journal: Record{PID: 100, Entries: []Entry{{Path: `C:\work`, Root: true}, {Path: `C:\Windows`, Root: true}}},
			read: func(path string) (string, error) {
				if path == `C:\work` {
					return apogeesOwnLowLabel, nil
				}
				return foreignMedium, nil
			},
			want: []string{`C:\work`},
		},
		{
			name:    "an_unreadable_root_is_skipped_rather_than_aborting_the_rest",
			journal: Record{PID: 100, Entries: []Entry{{Path: `C:\locked`, Root: true}, {Path: `C:\work`, Root: true}}},
			read: func(path string) (string, error) {
				if path == `C:\locked` {
					return "", errors.New("access is denied")
				}
				return apogeesOwnLowLabel, nil
			},
			want: []string{`C:\work`},
		},
		{
			name:    "a_volume_root_is_dropped_even_carrying_apogees_label",
			journal: Record{PID: 100, Entries: []Entry{{Path: `C:\`, Root: true}, {Path: `C:\work`, Root: true}}},
			read:    readsApogeesOwnLabel,
			want:    []string{`C:\work`},
		},
		{
			// The persisted verdict, and the reason it exists: a revert that cleared the root
			// but failed a descendant KEEPS the journal (clearTreeOutcome), and the retry reads
			// the NULL SACL ClearTree itself wrote. Re-judging there would refuse the root and
			// let the journal retire over descendants still labelled Low, so the verdict taken
			// before the first clear wins over the read.
			name:    "a_persisted_verdict_beats_the_null_sacl_the_clear_wrote",
			journal: Record{PID: 100, Entries: []Entry{{Path: `C:\work`, Root: true, RootJudged: true}}},
			read:    func(string) (string, error) { return clearSDDL, nil },
			want:    []string{`C:\work`},
		},
		{
			// The verdict skips the label READ, never the GUARDRAIL. A planted entry can set
			// "root_judged": true as easily as it can name a path, so a flag that switched the
			// volume refusal off would hand a stranger the NULL-SACLing of the whole of C:\ —
			// F-08's clear prong reopened through the very field that closed it. The volume
			// refusal is a statement about the SHAPE of the path, which no read ever decided.
			name: "a_persisted_verdict_does_not_buy_a_planted_volume_root",
			journal: Record{PID: 100, Entries: []Entry{
				{Path: `C:\`, Root: true, RootJudged: true},
				{Path: `\\server\share`, Root: true, RootJudged: true},
				{Path: `C:\work`, Root: true, RootJudged: true},
			}},
			read: func(string) (string, error) { return clearSDDL, nil },
			want: []string{`C:\work`},
		},
		{
			name:    "a_live_siblings_claim_still_spares_a_vouched_root",
			journal: Record{PID: 100, Entries: []Entry{{Path: `C:\work`, Root: true}, {Path: `C:\scratch`, Root: true}}},
			siblings: []Record{
				{PID: 200, Entries: []Entry{{Path: `c:\WORK`, Root: true}}},
			},
			read: readsApogeesOwnLabel,
			want: []string{`C:\scratch`},
		},
		{
			name:    "a_prior_only_entry_names_no_root_to_clear",
			journal: Record{PID: 100, Entries: []Entry{{Path: `C:\work\vendor.dll`, PriorSDDL: foreignMedium}}},
			read:    readsApogeesOwnLabel,
			want:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, _ := revertibleRoots(tt.journal, tt.siblings, alwaysAlive, tt.read)
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

// sparingRevert mirrors revertSparingLiveSiblings' join (walk_windows.go) over the pure
// deciders it composes, so the sparing case is drivable on any OS: the production closure is
// //go:build windows because the clear and the restore it drives ARE the label APIs, and the
// pre-clear judgement it opens with (judgePriors) reads one. The clear is recorded rather than
// performed — what these cases assert is which roots reach it and what the revert hands back,
// not the walk itself. Siblings are read off the disk exactly as production reads them
// (siblingJournals), because the interleave under test is two journal FILES seeing each other.
func sparingRevert(home, own string, live map[int]bool, cleared *[]string) func(Record) ([]Entry, error) {
	return func(r Record) ([]Entry, error) {
		siblings := siblingJournals(home, own)
		// The restore half is revertJournal's other argument and has its own table
		// (TestRestorablePriorsHandsOffSiblingClaimedTrees); only the clear side decides what
		// these cases assert.
		_, handoff := restorablePriors(r, siblings)
		clear, spared := revertibleRoots(r, siblings, func(pid int) bool { return live[pid] }, readsApogeesOwnLabel)
		*cleared = append(*cleared, clear...)
		return handoffSparedRoots(handoff, spared), nil
	}
}

func TestRevertibleRootsHandsBackTheRootsALiveSiblingSpared(t *testing.T) {
	t.Parallel()

	// The spared roots are RETURNED, not merely skipped: sparing one is not discharging it,
	// and the caller needs it back to keep this journal alive over it (handoffSparedRoots).
	// A root the clearability rule refuses is NOT handed back — nothing of apogee's is on it,
	// so there is no obligation to carry — which is why the two exclusions are told apart here
	// rather than counted together.
	const foreignMedium = "S:AI(ML;;NW;;;ME)"

	tests := []struct {
		name       string
		journal    Record
		siblings   []Record
		live       map[int]bool
		read       func(string) (string, error)
		wantClear  []string
		wantSpared []string
	}{
		{
			name:       "a_live_siblings_claim_hands_the_root_back",
			journal:    Record{PID: 100, Entries: []Entry{{Path: `C:\work`, Root: true}, {Path: `C:\scratch`, Root: true}}},
			siblings:   []Record{{PID: 200, Entries: []Entry{{Path: `c:\WORK`, Root: true}}}},
			live:       map[int]bool{200: true},
			read:       readsApogeesOwnLabel,
			wantClear:  []string{`C:\scratch`},
			wantSpared: []string{`C:\work`},
		},
		{
			name:      "a_dead_siblings_claim_hands_nothing_back",
			journal:   Record{PID: 100, Entries: []Entry{{Path: `C:\work`, Root: true}}},
			siblings:  []Record{{PID: 200, Entries: []Entry{{Path: `C:\work`, Root: true}}}},
			read:      readsApogeesOwnLabel,
			wantClear: []string{`C:\work`},
		},
		{
			name:      "a_root_apogees_label_no_longer_vouches_for_is_dropped_not_handed_back",
			journal:   Record{PID: 100, Entries: []Entry{{Path: `C:\work`, Root: true}}},
			read:      func(string) (string, error) { return foreignMedium, nil },
			wantClear: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			alive := func(pid int) bool { return tt.live[pid] }
			clear, spared := revertibleRoots(tt.journal, tt.siblings, alive, tt.read)
			if !sameRoots(clear, tt.wantClear) {
				t.Errorf("clear = %v, want %v", clear, tt.wantClear)
			}
			if !sameRoots(spared, tt.wantSpared) {
				t.Errorf("spared = %v, want %v", spared, tt.wantSpared)
			}
		})
	}
}

// sameRoots compares two root lists element for element, treating nil and empty as one answer.
func sameRoots(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestHandoffSparedRootsFoldsTheSparedRootsIntoThePriorHandoff(t *testing.T) {
	t.Parallel()

	// The fold retire's remains are built from. A spared root joins the priors already handed
	// off, and it joins them UNJUDGED: nothing was cleared on it, so no NULL SACL of this
	// revert's own making is in the way, and a verdict carried forward would let a later run
	// strip the tree without reading it (F-08's clear prong). A root that already appears —
	// one entry wearing both instructions — keeps its single entry, because a journal is one
	// entry per path and two would walk the tree twice.
	const foreignMedium = "S:AI(ML;;NW;;;ME)"

	tests := []struct {
		name    string
		handoff []Entry
		spared  []string
		want    []Entry
	}{
		{
			name:    "no_spared_root_leaves_the_handoff_untouched",
			handoff: []Entry{{Path: `C:\work\vendor.dll`, PriorSDDL: foreignMedium, Judged: true}},
			want:    []Entry{{Path: `C:\work\vendor.dll`, PriorSDDL: foreignMedium, Judged: true}},
		},
		{
			name:   "a_spared_root_alone_becomes_the_whole_handoff",
			spared: []string{`C:\work`},
			want:   []Entry{{Path: `C:\work`, Root: true}},
		},
		{
			name:    "a_spared_root_joins_the_priors_already_handed_off",
			handoff: []Entry{{Path: `C:\work\vendor.dll`, PriorSDDL: foreignMedium, Judged: true}},
			spared:  []string{`C:\work`},
			want: []Entry{
				{Path: `C:\work\vendor.dll`, PriorSDDL: foreignMedium, Judged: true},
				{Path: `C:\work`, Root: true},
			},
		},
		{
			name:    "a_spared_root_already_handed_off_for_its_prior_keeps_one_entry_and_loses_the_verdict",
			handoff: []Entry{{Path: `c:\WORK`, Root: true, RootJudged: true, PriorSDDL: foreignMedium, Judged: true}},
			spared:  []string{`C:\work`},
			want:    []Entry{{Path: `c:\WORK`, Root: true, PriorSDDL: foreignMedium, Judged: true}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := handoffSparedRoots(tt.handoff, tt.spared)
			if len(got) != len(tt.want) {
				t.Fatalf("handoffSparedRoots = %+v, want %+v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("handoffSparedRoots[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRetireKeepsTheJournalOfARootSparedForALiveSibling(t *testing.T) {
	t.Parallel()

	// The fourth fate, and the one the audit's overlapping-close finding turns on: the revert
	// succeeded but spared the shared root to a LIVE sibling, so the Low label is still on that
	// tree and this journal is the only record of it that this process owns. It must survive
	// rewritten to the root, exactly as a handed-off prior keeps it. A DEAD sibling spares
	// nothing, and the journal retires as it always has.
	tests := []struct {
		name      string
		live      map[int]bool
		wantClear []string
		wantKept  bool
	}{
		{
			name:     "a_live_sibling_spares_the_root_and_the_journal_survives_carrying_it",
			live:     map[int]bool{200: true},
			wantKept: true,
		},
		{
			name:      "a_dead_sibling_spares_nothing_and_the_journal_retires",
			wantClear: []string{`C:\work`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			own := JournalPath(home, 100)
			mine := Record{PID: 100, Entries: []Entry{{Path: `C:\work`, Root: true, RootJudged: true}}}
			if err := WriteJournal(own, mine); err != nil {
				t.Fatalf("seed this session's journal: %v", err)
			}
			sibling := Record{PID: 200, Entries: []Entry{{Path: `C:\work`, Root: true}}}
			if err := WriteJournal(JournalPath(home, 200), sibling); err != nil {
				t.Fatalf("seed the sibling journal: %v", err)
			}

			var cleared []string
			remaining, err := retire(own, mine, sparingRevert(home, own, tt.live, &cleared))
			if err != nil {
				t.Fatalf("retire = %v, want nil — a spared root is not a failed revert", err)
			}
			if !sameRoots(cleared, tt.wantClear) {
				t.Errorf("cleared roots = %v, want %v", cleared, tt.wantClear)
			}

			kept, statErr := ReadJournal(own)
			if !tt.wantKept {
				if statErr == nil {
					t.Fatalf("the journal survived a revert that discharged everything: %+v", kept)
				}
				if len(remaining) != 0 {
					t.Errorf("remaining = %+v, want nothing handed back", remaining)
				}
				return
			}
			if statErr != nil {
				t.Fatalf("the journal did not survive the spared root: %v — the Low label on that tree is now unrecoverable", statErr)
			}
			want := Entry{Path: `C:\work`, Root: true}
			if len(kept.Entries) != 1 || kept.Entries[0] != want {
				t.Errorf("rewritten journal entries = %+v, want exactly the spared root, unjudged", kept.Entries)
			}
			if kept.PID != mine.PID {
				t.Errorf("rewritten journal PID = %d, want the original owner %d", kept.PID, mine.PID)
			}
			if len(remaining) != 1 || remaining[0] != want {
				t.Errorf("remaining = %+v, want the spared root back, so a repeated Close converges", remaining)
			}
		})
	}
}

func TestRetireOverASparedRootLeavesAJournalResidueReports(t *testing.T) {
	t.Parallel()

	// End to end over the surface a human actually sees: after the sparing retire, the journal
	// is still on the disk AND Residue names the root it carries. Before this rule the file was
	// removed, so the stranded Low label was not merely unrecoverable — no report could even
	// mention it.
	home := t.TempDir()
	own := JournalPath(home, 4242)
	mine := Record{PID: 4242, Entries: []Entry{{Path: `C:\work`, Root: true}}}
	if err := WriteJournal(own, mine); err != nil {
		t.Fatalf("seed this session's journal: %v", err)
	}
	if err := WriteJournal(JournalPath(home, 4343), Record{PID: 4343, Entries: []Entry{{Path: `C:\work`, Root: true}}}); err != nil {
		t.Fatalf("seed the sibling journal: %v", err)
	}

	var cleared []string
	if _, err := retire(own, mine, sparingRevert(home, own, map[int]bool{4343: true}, &cleared)); err != nil {
		t.Fatalf("retire = %v, want nil", err)
	}
	if len(cleared) != 0 {
		t.Fatalf("cleared = %v, want nothing — the live sibling is still fenced by that label", cleared)
	}
	if _, err := os.Stat(own); err != nil {
		t.Fatalf("the journal file did not survive: %v", err)
	}
	if notice := ResidueIn(home); !strings.Contains(notice, `C:\work`) {
		t.Errorf("ResidueIn = %q, want the spared root named — a label nothing reports is a label nothing clears", notice)
	}
}

func TestOverlappingClosesNeverStrandTheSharedRootsLabel(t *testing.T) {
	t.Parallel()

	// The audit's High finding, driven in both orders: two sessions confine one workspace and
	// their teardowns overlap, so each sees the other alive and spares the shared root. The
	// invariant is that the pair can never end with the label on the disk and no journal naming
	// it — a journal survives until a revert actually clears the tree, and only then is the
	// last file removed.
	orders := []struct {
		name  string
		first int
		last  int
	}{
		{name: "the_first_session_closes_first", first: 100, last: 200},
		{name: "the_second_session_closes_first", first: 200, last: 100},
	}

	for _, order := range orders {
		t.Run(order.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			for _, pid := range []int{100, 200} {
				r := Record{PID: pid, Entries: []Entry{{Path: `C:\work`, Root: true, RootJudged: true}}}
				if err := WriteJournal(JournalPath(home, pid), r); err != nil {
					t.Fatalf("seed the journal of %d: %v", pid, err)
				}
			}

			// Both closes run while the other process is still alive — the interleave that
			// used to delete both files over a label neither had cleared.
			var cleared []string
			for _, closing := range []struct{ own, other int }{{order.first, order.last}, {order.last, order.first}} {
				path := JournalPath(home, closing.own)
				r, err := ReadJournal(path)
				if err != nil {
					t.Fatalf("read the journal of %d: %v", closing.own, err)
				}
				live := map[int]bool{closing.other: true}
				if _, err := retire(path, r, sparingRevert(home, path, live, &cleared)); err != nil {
					t.Fatalf("retire the journal of %d: %v", closing.own, err)
				}
			}
			if len(cleared) != 0 {
				t.Fatalf("cleared = %v, want nothing while a sibling is still alive", cleared)
			}
			if survivors := ListJournals(home); len(survivors) == 0 {
				t.Fatal("both journals were removed over a label neither close cleared; the Low label on C:\\work is now unrecoverable and unreportable")
			}

			// Both processes are gone. The next run — a session's constructor or recovery —
			// finds no live claim, clears the tree and only then removes the file.
			for _, path := range ListJournals(home) {
				r, err := ReadJournal(path)
				if err != nil {
					t.Fatalf("read the surviving journal %q: %v", path, err)
				}
				if _, err := retire(path, r, sparingRevert(home, path, nil, &cleared)); err != nil {
					t.Fatalf("retire the surviving journal %q: %v", path, err)
				}
			}
			if len(cleared) == 0 {
				t.Error("no run ever cleared the shared root once both owners were gone")
			}
			if survivors := ListJournals(home); len(survivors) != 0 {
				t.Errorf("journals survived a revert that cleared everything: %v — a stale journal reports residue that is not there", survivors)
			}
		})
	}
}
