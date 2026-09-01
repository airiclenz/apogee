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
			got := revertibleRoots(journal, tt.siblings, alive, readsApogeesOwnLabel)
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

	tests := []struct {
		name        string
		current     string
		readErr     error
		wantRestore bool
		wantDrop    bool
	}{
		{name: "apogees_own_low_label_is_restorable", current: ownLabel, wantRestore: true},
		{name: "the_canonical_low_sid_is_the_same_mark", current: canonicalLow, wantRestore: true},
		{name: "a_foreign_medium_label_drops_the_instruction", current: foreignMedium, wantDrop: true},
		{name: "a_foreign_high_label_drops_the_instruction", current: foreignHigh, wantDrop: true},
		{name: "an_unlabelled_path_drops_the_instruction", current: "", wantDrop: true},
		{name: "a_vanished_path_drops_the_instruction", readErr: os.ErrNotExist, wantDrop: true},
		{
			name:     "a_vanished_path_reported_as_a_path_error_drops_too",
			readErr:  &fs.PathError{Op: "read", Path: `C:\work\gone.txt`, Err: fs.ErrNotExist},
			wantDrop: true,
		},
		{name: "an_unreadable_path_is_neither_restored_nor_dropped", readErr: denied},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			restore, drop := priorRestorable(tt.current, tt.readErr)

			if restore != tt.wantRestore || drop != tt.wantDrop {
				t.Errorf("priorRestorable(%q, %v) = restore %v, drop %v; want restore %v, drop %v",
					tt.current, tt.readErr, restore, drop, tt.wantRestore, tt.wantDrop)
			}
		})
	}
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

			got := revertibleRoots(tt.journal, tt.siblings, alwaysAlive, tt.read)
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
