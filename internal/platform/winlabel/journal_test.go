package winlabel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLabelJournalRoundTripAndAccessors(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := JournalPath(home, 4242)
	journal := Record{
		PID: 4242,
		Entries: []Entry{
			{Path: `C:\work`, Root: true},
			{Path: `D:\cache`, Root: true, PriorSDDL: "S:AI(ML;OICI;NW;;;ME)"},
			{Path: `C:\work\downloaded.txt`, PriorSDDL: "S:AI(ML;;NW;;;LW)"},
		},
	}
	if err := WriteJournal(path, journal); err != nil {
		t.Fatalf("WriteJournal: %v", err)
	}

	got, err := ReadJournal(path)
	if err != nil {
		t.Fatalf("ReadJournal: %v", err)
	}
	if got.PID != journal.PID || len(got.Entries) != len(journal.Entries) {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, journal)
	}

	// Roots() drives the teardown walk; PriorLabels() drives what is put back afterwards.
	roots := got.Roots()
	if len(roots) != 2 || roots[0] != `C:\work` || roots[1] != `D:\cache` {
		t.Errorf("Roots() = %v, want the two journalled box roots", roots)
	}
	priors := got.PriorLabels()
	if len(priors) != 2 || priors[`C:\work\downloaded.txt`] != "S:AI(ML;;NW;;;LW)" {
		t.Errorf("PriorLabels() = %v, want the two pre-existing descriptors", priors)
	}

	// ListJournals finds it by name, and ignores anything else in the directory.
	if err := os.WriteFile(filepath.Join(JournalDir(home), "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("seed stray file: %v", err)
	}
	found := ListJournals(home)
	if len(found) != 1 || found[0] != path {
		t.Errorf("ListJournals = %v, want just %q", found, path)
	}
}

func TestWriteLabelJournalPublishesAtomically(t *testing.T) {
	t.Parallel()

	// The journal is rewritten every time the label pass discovers something new, so an
	// in-place truncate would leave a window in which the file on disk describes neither the
	// old set of labels nor the new one. The write therefore goes to a temp file and is
	// renamed over the journal: the round trip below reads back the SECOND write whole, and
	// the directory holds exactly one file afterwards — no temp debris a reader could trip on.
	home := t.TempDir()
	path := JournalPath(home, 77)

	first := Record{PID: 77, Entries: []Entry{{Path: `C:\work`, Root: true}}}
	if err := WriteJournal(path, first); err != nil {
		t.Fatalf("WriteJournal (create): %v", err)
	}
	second := Record{PID: 77, Entries: []Entry{
		{Path: `C:\work`, Root: true},
		{Path: `C:\work\vendor`, PriorSDDL: "S:AI(ML;;NW;;;ME)"},
	}}
	if err := WriteJournal(path, second); err != nil {
		t.Fatalf("WriteJournal (replace): %v", err)
	}

	got, err := ReadJournal(path)
	if err != nil {
		t.Fatalf("ReadJournal after the replacing write: %v", err)
	}
	if len(got.Entries) != 2 || got.Entries[1].Path != `C:\work\vendor` {
		t.Errorf("journal = %+v, want the second write's entries whole", got)
	}

	entries, err := os.ReadDir(JournalDir(home))
	if err != nil {
		t.Fatalf("read the journal dir: %v", err)
	}
	if len(entries) != 1 || filepath.Join(JournalDir(home), entries[0].Name()) != path {
		t.Errorf("journal dir holds %v, want only %q — a temp file left behind is debris", entries, path)
	}

	// Even if debris DID survive a crash, it is not a journal: the name matches neither half
	// of the journal naming rule, so nothing lists, reads or reports it.
	debris := filepath.Join(JournalDir(home), journalTempPrefix+"1234"+journalTempSuffix)
	if err := os.WriteFile(debris, []byte("half a journal"), 0o600); err != nil {
		t.Fatalf("seed temp debris: %v", err)
	}
	if found := ListJournals(home); len(found) != 1 || found[0] != path {
		t.Errorf("ListJournals = %v, want just %q", found, path)
	}
	if got := ResidueIn(home); strings.Contains(got, "unreadable") || strings.Contains(got, debris) {
		t.Errorf("ResidueIn = %q; temp debris must not be reported as an unreadable journal", got)
	}
}

func TestSiblingLabelJournalsExcludesOwnAndUndecodable(t *testing.T) {
	t.Parallel()

	// The revert's view of the OTHER sessions in the same home: everything but its own file,
	// with an undecodable journal skipped — it names no owner to check and no roots to spare,
	// and erring toward clearing is the safe direction (less privilege, never more).
	home := t.TempDir()
	own := JournalPath(home, 100)
	if err := WriteJournal(own, Record{PID: 100, Entries: []Entry{{Path: `C:\work`, Root: true}}}); err != nil {
		t.Fatalf("seed own journal: %v", err)
	}
	if err := WriteJournal(JournalPath(home, 200), Record{PID: 200, Entries: []Entry{{Path: `C:\other`, Root: true}}}); err != nil {
		t.Fatalf("seed sibling journal: %v", err)
	}
	if err := os.WriteFile(JournalPath(home, 300), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seed undecodable journal: %v", err)
	}

	got := siblingJournals(home, own)
	if len(got) != 1 || got[0].PID != 200 {
		t.Fatalf("siblingJournals = %+v, want only the decodable sibling (PID 200)", got)
	}

	// Windows paths are case-insensitive, so a differently-cased spelling of own is still own.
	if got := siblingJournals(home, strings.ToUpper(own)); len(got) != 1 || got[0].PID != 200 {
		t.Errorf("siblingJournals with upper-cased own = %+v, want the own journal still excluded", got)
	}

	// No home means no journals — the no-user-profile backend must not read a relative path.
	if got := siblingJournals("", own); got != nil {
		t.Errorf("siblingJournals(\"\") = %+v, want nil", got)
	}
}

func TestConfinementResidueReportsOnlyForeignJournals(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if got := ResidueIn(home); got != "" {
		t.Errorf("ResidueIn on a clean home = %q, want \"\" (there is nothing to report)", got)
	}

	// This process's own journal is the live session's fence, not residue: reporting it would
	// tell a user their own running session had left labels behind.
	if err := WriteJournal(JournalPath(home, os.Getpid()), Record{
		PID:     os.Getpid(),
		Entries: []Entry{{Path: `C:\mine`, Root: true}},
	}); err != nil {
		t.Fatalf("write own journal: %v", err)
	}
	if got := ResidueIn(home); got != "" {
		t.Errorf("ResidueIn reported this process's own journal: %q", got)
	}

	// A journal from another process is the finding the host report exists to surface, and it
	// must name both the affected path and the manual remedy.
	if err := WriteJournal(JournalPath(home, os.Getpid()+1), Record{
		PID:     os.Getpid() + 1,
		Entries: []Entry{{Path: `C:\work\proj`, Root: true}},
	}); err != nil {
		t.Fatalf("write foreign journal: %v", err)
	}
	got := ResidueIn(home)
	if !strings.Contains(got, `C:\work\proj`) {
		t.Errorf("residue = %q; want it to name the still-labelled path", got)
	}
	if !strings.Contains(got, "icacls") {
		t.Errorf("residue = %q; want it to name the manual remedy", got)
	}
}

func TestConfinementResidueReportsAnUnreadableJournal(t *testing.T) {
	t.Parallel()

	// The worst state the journal directory can be in: a file that IS a journal by name but
	// cannot be decoded. Recovery skips it — it has no roots to revert and no PID to check —
	// so it sits on the disk forever, possibly describing labels that are really there. Before
	// this, the residue report skipped it too, which made the one surface that could tell the
	// user silent about precisely the case it exists for.
	home := t.TempDir()
	garbage := JournalPath(home, 909)
	if err := os.MkdirAll(JournalDir(home), 0o700); err != nil {
		t.Fatalf("create journal dir: %v", err)
	}
	if err := os.WriteFile(garbage, []byte(`{"pid":909,"entries":[{"path":"C:\\wo`), 0o600); err != nil {
		t.Fatalf("seed a half-written journal: %v", err)
	}

	got := ResidueIn(home)
	if !strings.Contains(got, "unreadable") || !strings.Contains(got, garbage) {
		t.Fatalf("residue = %q; want it to name the unreadable journal %q", got, garbage)
	}
	if !strings.Contains(got, Remedy) {
		t.Errorf("residue = %q; want the manual remedy, which is the ONLY one for a journal no run can decode", got)
	}

	// A readable journal alongside it is still reported on its own terms: one finding must not
	// swallow the other.
	if err := WriteJournal(JournalPath(home, os.Getpid()+1), Record{
		PID:     os.Getpid() + 1,
		Entries: []Entry{{Path: `C:\work\proj`, Root: true}},
	}); err != nil {
		t.Fatalf("write foreign journal: %v", err)
	}
	got = ResidueIn(home)
	if !strings.Contains(got, garbage) || !strings.Contains(got, `C:\work\proj`) {
		t.Errorf("residue = %q; want both the unreadable journal and the still-labelled path", got)
	}
}

func TestJournalLabelEntryNeverRecordsApogeesOwnLabel(t *testing.T) {
	t.Parallel()

	// A journal entry is an instruction to a future revert, so the one thing it must never say
	// is "this path carried a Low label before the run" — apogee is the only thing that writes
	// Low labels here, and restoring one is residue that puts itself back. The two ways that
	// happens are a path journalled twice (the second read sees apogee's own label) and a prior
	// read off a tree apogee (or a concurrent session) has already labelled, so both are decided
	// here, on every OS, rather than only on a machine that can write a real SACL.
	const (
		foreignMedium = "S:AI(ML;;NW;;;ME)"
		foreignHigh   = "S:(ML;OICI;NW;;;HI)"
		ownInherited  = "S:AI(ML;OICIID;NW;;;LW)"
		ownCanonical  = "S:(ML;;NW;;;S-1-16-4096)"
	)

	tests := []struct {
		name        string
		entries     []Entry
		entry       Entry
		wantChanged bool
		wantEntries []Entry
	}{
		{
			name:        "first_root_is_recorded",
			entry:       Entry{Path: `C:\work`, Root: true},
			wantChanged: true,
			wantEntries: []Entry{{Path: `C:\work`, Root: true}},
		},
		{
			name:        "foreign_prior_is_kept_verbatim",
			entry:       Entry{Path: `C:\work\vendor`, PriorSDDL: foreignMedium},
			wantChanged: true,
			wantEntries: []Entry{{Path: `C:\work\vendor`, PriorSDDL: foreignMedium}},
		},
		{
			name:        "foreign_root_prior_is_kept_verbatim",
			entry:       Entry{Path: `C:\work`, Root: true, PriorSDDL: foreignHigh},
			wantChanged: true,
			wantEntries: []Entry{{Path: `C:\work`, Root: true, PriorSDDL: foreignHigh}},
		},
		{
			name:        "own_dir_label_as_root_prior_is_recorded_as_no_prior",
			entry:       Entry{Path: `C:\work`, Root: true, PriorSDDL: lowSDDL},
			wantChanged: true,
			wantEntries: []Entry{{Path: `C:\work`, Root: true}},
		},
		{
			name:  "own_file_label_on_a_descendant_is_not_recorded_at_all",
			entry: Entry{Path: `C:\work\main.go`, PriorSDDL: lowSDDL},
		},
		{
			name:  "inherited_own_label_on_a_descendant_is_not_recorded_at_all",
			entry: Entry{Path: `C:\work\main.go`, PriorSDDL: ownInherited},
		},
		{
			name:  "canonical_low_sid_is_recognised_as_our_own",
			entry: Entry{Path: `C:\work\main.go`, PriorSDDL: ownCanonical},
		},
		{
			name:        "duplicate_path_keeps_the_first_prior",
			entries:     []Entry{{Path: `C:\work`, Root: true, PriorSDDL: foreignMedium}},
			entry:       Entry{Path: `C:\work`, Root: true, PriorSDDL: lowSDDL},
			wantEntries: []Entry{{Path: `C:\work`, Root: true, PriorSDDL: foreignMedium}},
		},
		{
			name:        "case_varied_duplicate_path_is_the_same_path",
			entries:     []Entry{{Path: `C:\Work`, Root: true}},
			entry:       Entry{Path: `c:\work`, Root: true, PriorSDDL: lowSDDL},
			wantEntries: []Entry{{Path: `C:\Work`, Root: true}},
		},
		{
			name:        "a_journalled_descendant_can_still_become_a_root",
			entries:     []Entry{{Path: `C:\work\vendor`, PriorSDDL: foreignMedium}},
			entry:       Entry{Path: `C:\WORK\VENDOR`, Root: true, PriorSDDL: lowSDDL},
			wantChanged: true,
			wantEntries: []Entry{{Path: `C:\work\vendor`, Root: true, PriorSDDL: foreignMedium}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, changed := recordEntry(tt.entries, tt.entry, foldPath)
			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v (it decides whether the journal is flushed)", changed, tt.wantChanged)
			}
			if len(got) != len(tt.wantEntries) {
				t.Fatalf("entries = %+v, want %+v", got, tt.wantEntries)
			}
			for i, want := range tt.wantEntries {
				if got[i] != want {
					t.Errorf("entries[%d] = %+v, want %+v", i, got[i], want)
				}
			}
			for _, entry := range got {
				if IsLowLabel(entry.PriorSDDL) {
					t.Errorf("entry %+v records a Low prior; the revert would re-apply apogee's own label", entry)
				}
			}
		})
	}
}

func TestJournalLabelEntryUsesTheInjectedFold(t *testing.T) {
	t.Parallel()

	// The fold is a parameter so the rule is provable off Windows: under a fold that treats two
	// spellings as one path, the second spelling adds nothing.
	entries := []Entry{{Path: `C:\Work`, Root: true}}
	if _, changed := recordEntry(entries, Entry{Path: `c:\WORK`, Root: true}, nil); changed {
		t.Error("the default fold treated two case spellings of one path as two paths")
	}
	identity := func(p string) string { return p }
	if _, changed := recordEntry(entries, Entry{Path: `c:\WORK`, Root: true}, identity); !changed {
		t.Error("the injected fold was ignored; the helper is not honouring its seam")
	}
}

func TestUnwindLabelEntry(t *testing.T) {
	t.Parallel()

	// labelBox's phantom-entry undo, proven on every OS: a root entry journalled ahead of a
	// label write that then FAILED describes a mutation that never happened, and it must come
	// out again — left in place, every later Close and recovery fails clearing a label that is
	// not there, the journal is never retired, and Residue alarms forever over a
	// clean disk. The one exception is an entry recording a foreign prior, which is kept:
	// ambiguity resolves toward keeping the record.
	const foreignMedium = "S:AI(ML;;NW;;;ME)"

	tests := []struct {
		name        string
		entries     []Entry
		path        string
		wantRemoved bool
		wantEntries []Entry
	}{
		{
			name:        "no_prior_root_entry_is_removed",
			entries:     []Entry{{Path: `C:\work`, Root: true}},
			path:        `C:\work`,
			wantRemoved: true,
		},
		{
			name:        "foreign_prior_entry_is_kept",
			entries:     []Entry{{Path: `C:\work`, Root: true, PriorSDDL: foreignMedium}},
			path:        `C:\work`,
			wantEntries: []Entry{{Path: `C:\work`, Root: true, PriorSDDL: foreignMedium}},
		},
		{
			name:        "unknown_path_removes_nothing",
			entries:     []Entry{{Path: `C:\other`, Root: true}},
			path:        `C:\work`,
			wantEntries: []Entry{{Path: `C:\other`, Root: true}},
		},
		{
			name:        "case_varied_spelling_is_the_same_path",
			entries:     []Entry{{Path: `C:\Work`, Root: true}},
			path:        `c:\WORK`,
			wantRemoved: true,
		},
		{
			name: "only_the_named_entry_is_removed",
			entries: []Entry{
				{Path: `C:\keep`, Root: true},
				{Path: `C:\work`, Root: true},
				{Path: `C:\keep\vendor`, PriorSDDL: foreignMedium},
			},
			path:        `C:\work`,
			wantRemoved: true,
			wantEntries: []Entry{
				{Path: `C:\keep`, Root: true},
				{Path: `C:\keep\vendor`, PriorSDDL: foreignMedium},
			},
		},
		{
			name: "empty_journal_removes_nothing",
			path: `C:\work`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, removed := unwindEntry(tt.entries, tt.path, foldPath)
			if removed != tt.wantRemoved {
				t.Errorf("removed = %v, want %v (it decides whether the journal is re-flushed)", removed, tt.wantRemoved)
			}
			if len(got) != len(tt.wantEntries) {
				t.Fatalf("entries = %+v, want %+v", got, tt.wantEntries)
			}
			for i, want := range tt.wantEntries {
				if got[i] != want {
					t.Errorf("entries[%d] = %+v, want %+v", i, got[i], want)
				}
			}
		})
	}
}

func TestUnwindLabelEntryUsesTheInjectedFold(t *testing.T) {
	t.Parallel()

	// The fold is a parameter so the rule is provable off Windows: under the default fold two
	// case spellings are one path; under an injected identity fold they are two.
	entries := []Entry{{Path: `C:\Work`, Root: true}}
	if _, removed := unwindEntry(entries, `c:\WORK`, nil); !removed {
		t.Error("the default fold treated two case spellings of one path as two paths")
	}
	identity := func(p string) string { return p }
	if _, removed := unwindEntry(entries, `c:\WORK`, identity); removed {
		t.Error("the injected fold was ignored; the helper is not honouring its seam")
	}
}

func TestJournalRoundTripsJudged(t *testing.T) {
	t.Parallel()

	// Entry.Judged is the verdict the revert takes BEFORE it clears anything — apogee's own
	// Low label was still on the path, so the prior beneath it is apogee's to put back
	// (priorRestorable). It is only worth taking if it SURVIVES to the pass that acts on it: a
	// prior handed off to a later run, or one retried after a failed restore, is read once the
	// clear has unlabelled its path, where a fresh judgement would drop it. So the flag is part
	// of the journal's on-disk compatibility surface, in both directions.
	home := t.TempDir()
	path := JournalPath(home, 7788)
	written := Record{
		PID: 7788,
		Entries: []Entry{
			{Path: `C:\work`, Root: true},
			{Path: `C:\work\vendor\lib.dll`, PriorSDDL: "S:AI(ML;;NW;;;ME)", Judged: true},
			{Path: `C:\work\pending.txt`, PriorSDDL: "S:(ML;;NW;;;HI)"},
		},
	}

	if err := WriteJournal(path, written); err != nil {
		t.Fatalf("write journal: %v", err)
	}
	read, err := ReadJournal(path)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}

	if len(read.Entries) != len(written.Entries) {
		t.Fatalf("entries = %+v, want %+v", read.Entries, written.Entries)
	}
	for i, want := range written.Entries {
		if read.Entries[i] != want {
			t.Errorf("entries[%d] = %+v, want %+v", i, read.Entries[i], want)
		}
	}

	// An unjudged entry carries no `judged` key at all, so a journal this apogee writes stays
	// decodable by the one the user may roll back to — the rule Record's doc states about
	// every tag here.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read journal file: %v", err)
	}
	if got := strings.Count(string(raw), `"judged"`); got != 1 {
		t.Errorf("the journal spells \"judged\" %d time(s) in %s, want exactly the one judged entry", got, raw)
	}
}

func TestJournalWrittenByAnOlderApogeeIsUnjudged(t *testing.T) {
	t.Parallel()

	// The other direction: a journal left by an apogee that predates the judgement — the one a
	// user upgrades over, and the one a crash left behind — has no flag to read, and false is
	// the honest decode. It means "nothing has judged this yet", so the first pass judges it
	// while the path still carries the label that vouches for it, rather than skipping the
	// check over a prior nobody vetted.
	home := t.TempDir()
	path := JournalPath(home, 9911)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create the journal dir: %v", err)
	}
	legacy := `{"pid":9911,"entries":[{"path":"C:\\work","root":true},` +
		`{"path":"C:\\work\\vendor\\lib.dll","prior_sddl":"S:AI(ML;;NW;;;ME)"}]}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("plant the older journal: %v", err)
	}

	read, err := ReadJournal(path)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}

	for _, entry := range read.Entries {
		if entry.Judged {
			t.Errorf("entry %+v decoded as already judged; an older journal has been vetted by nothing", entry)
		}
	}
}
