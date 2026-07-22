package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// The Windows token backend's DECISIONS are pure functions over the Windows rule table, so
// they are exercised here on every OS — the injected-seam pattern the whole package follows
// (host.go). The native Windows run (confiner_windows_test.go) proves the OS half; this file
// proves the policy half, including the cases a real machine will not hand you on demand: a
// box root that is C:\, one that swallows %ProgramFiles%, one spelled as an unresolvable 8.3
// short name.

// winTestRules is the Windows rule table with a deterministic long-path resolver, so an 8.3
// short name resolves exactly when the test says it does.
func winTestRules(long map[string]string) hostRules {
	rules := windowsRules()
	rules.longPath = func(p string) string {
		if expanded, ok := long[p]; ok {
			return expanded
		}
		return p
	}
	return rules
}

func TestWindowsBoxRootsCollapsesAndGuards(t *testing.T) {
	t.Parallel()

	protected := []string{`C:\Windows`, `C:\Program Files`, `C:\Users\dev`}

	tests := []struct {
		name      string
		box       domain.ConfinementBox
		want      []string
		wantRefus string // substring of the refusal; empty means the box must be accepted
	}{
		{
			name: "single_workspace_root",
			box:  domain.ConfinementBox{WorkspaceRoot: `C:\Users\dev\proj`},
			want: []string{`C:\Users\dev\proj`},
		},
		{
			name: "nested_writable_path_collapses_into_workspace",
			box: domain.ConfinementBox{
				WorkspaceRoot: `C:\Users\dev\proj`,
				WritablePaths: []string{`C:\Users\dev\proj\build`, `D:\cache`},
			},
			want: []string{`C:\Users\dev\proj`, `D:\cache`},
		},
		{
			name: "case_and_separator_variants_are_one_root",
			box: domain.ConfinementBox{
				WorkspaceRoot: `C:\Users\dev\proj`,
				WritablePaths: []string{`c:/users/DEV/proj`},
			},
			want: []string{`C:\Users\dev\proj`},
		},
		{
			name: "sibling_prefix_is_not_nested",
			box: domain.ConfinementBox{
				WorkspaceRoot: `C:\Users\dev\proj`,
				WritablePaths: []string{`C:\Users\dev\proj2`},
			},
			want: []string{`C:\Users\dev\proj`, `C:\Users\dev\proj2`},
		},
		{
			name: "outer_root_absorbs_the_inner_one_regardless_of_order",
			box: domain.ConfinementBox{
				WorkspaceRoot: `C:\work\proj\sub`,
				WritablePaths: []string{`C:\work\proj`},
			},
			want: []string{`C:\work\proj`},
		},
		{
			name:      "volume_root_refused",
			box:       domain.ConfinementBox{WorkspaceRoot: `C:\`},
			wantRefus: "volume root",
		},
		{
			name:      "unc_share_root_refused",
			box:       domain.ConfinementBox{WorkspaceRoot: `\\server\share`},
			wantRefus: "volume root",
		},
		{
			name:      "system_root_refused",
			box:       domain.ConfinementBox{WorkspaceRoot: `C:\Windows`},
			wantRefus: "protected location",
		},
		{
			name:      "ancestor_of_a_protected_location_refused",
			box:       domain.ConfinementBox{WorkspaceRoot: `C:\Users`},
			wantRefus: "protected location",
		},
		{
			name:      "user_profile_root_refused",
			box:       domain.ConfinementBox{WorkspaceRoot: `C:\Users\dev`},
			wantRefus: "protected location",
		},
		{
			name: "a_guardrailed_WRITABLE_PATH_refuses_the_whole_box",
			box: domain.ConfinementBox{
				WorkspaceRoot: `C:\Users\dev\proj`,
				WritablePaths: []string{`C:\Program Files`},
			},
			wantRefus: "protected location",
		},
		{
			name:      "drive_relative_root_refused_as_unresolvable",
			box:       domain.ConfinementBox{WorkspaceRoot: `C:work`},
			wantRefus: "cannot resolve",
		},
		{
			name:      "device_path_refused_as_unresolvable",
			box:       domain.ConfinementBox{WorkspaceRoot: `\\.\PhysicalDrive0`},
			wantRefus: "cannot resolve",
		},
		{
			name:      "empty_box_names_nothing_to_label",
			box:       domain.ConfinementBox{},
			wantRefus: "no writable root",
		},
	}

	rules := winTestRules(nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := windowsBoxRoots(rules, tt.box, protected)
			if tt.wantRefus != "" {
				if err == nil {
					t.Fatalf("windowsBoxRoots(%+v) = %v, nil; want a refusal mentioning %q", tt.box, got, tt.wantRefus)
				}
				if !errors.Is(err, domain.ErrConfinementUnavailable) {
					t.Errorf("err = %v; want ErrConfinementUnavailable so dispatch demotes to a forced Gate", err)
				}
				if !strings.Contains(err.Error(), tt.wantRefus) {
					t.Errorf("err = %q; want it to mention %q", err, tt.wantRefus)
				}
				return
			}
			if err != nil {
				t.Fatalf("windowsBoxRoots(%+v): unexpected refusal: %v", tt.box, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("roots = %v, want %v", got, tt.want)
			}
			for i, want := range tt.want {
				if got[i] != want {
					t.Errorf("roots[%d] = %q, want %q", i, got[i], want)
				}
			}
		})
	}
}

func TestWindowsBoxRootsRefusesUnresolvableShortName(t *testing.T) {
	t.Parallel()

	// The fence's sharpest edge (ADR 0020 §6). Contains reports "not contained" for a path it
	// cannot resolve, which is the SAFE answer when collapsing roots — but a guardrail caller
	// reading that as "outside the guardrail" would wave the path straight through. An 8.3
	// short name nothing can expand must be REFUSED, not labelled.
	rules := winTestRules(nil) // resolves nothing: every ~1 name stays short
	_, err := windowsBoxRoots(rules, domain.ConfinementBox{WorkspaceRoot: `C:\PROGRA~1\app`}, []string{`C:\Program Files`})
	if err == nil {
		t.Fatal("an unresolvable 8.3 root was accepted; it must be refused, never labelled")
	}
	if !errors.Is(err, domain.ErrConfinementUnavailable) || !strings.Contains(err.Error(), "cannot resolve") {
		t.Errorf("err = %v; want an ErrConfinementUnavailable refusal naming the unresolvable path", err)
	}

	// Resolved, the same root expands INTO a protected location and is refused for that
	// reason instead — never silently accepted.
	resolving := winTestRules(map[string]string{`C:\PROGRA~1\app`: `C:\Program Files\app`})
	if _, err := windowsBoxRoots(resolving, domain.ConfinementBox{WorkspaceRoot: `C:\PROGRA~1\app`}, []string{`C:\Program Files`}); err != nil {
		t.Fatalf("a resolvable short name below a protected root must be labellable: %v", err)
	}
	if _, err := windowsBoxRoots(resolving, domain.ConfinementBox{WorkspaceRoot: `C:\PROGRA~1`}, []string{`C:\Program Files`}); err == nil {
		t.Error("a short name resolving TO the protected location itself was accepted; want a refusal")
	}
}

func TestWindowsBoxRootsRefusesUnresolvableProtectedLocation(t *testing.T) {
	t.Parallel()

	// If a guardrail location itself cannot be compared, no root can be checked against it,
	// so the honest answer is to label nothing — the same refuse-to-label posture as an
	// unresolvable root.
	rules := winTestRules(nil)
	_, err := windowsBoxRoots(rules, domain.ConfinementBox{WorkspaceRoot: `C:\work`}, []string{`C:\PROGRA~1`})
	if err == nil || !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Fatalf("err = %v; want a refusal when a protected location cannot be resolved", err)
	}
}

func TestWindowsProtectedRootsFromEnvironment(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"SystemRoot":        `C:\Windows`,
		"windir":            `C:\Windows`, // the same location twice: emitted once
		"ProgramFiles":      `C:\Program Files`,
		"ProgramFiles(x86)": `C:\Program Files (x86)`,
		"ProgramData":       "", // unset: names no location, must not veto a box
	}
	lookup := func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	}

	got := windowsProtectedRoots(lookup, `C:\Users\dev`)
	want := []string{`C:\Windows`, `C:\Program Files`, `C:\Program Files (x86)`, `C:\Users\dev`}
	if len(got) != len(want) {
		t.Fatalf("protected = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("protected[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestWindowsNetworkDenyDecisionFailsClosed(t *testing.T) {
	t.Parallel()

	// Network OPEN (empty NetworkAllow) is the ADR 0012 default: nothing to enforce, so the
	// box is accepted and the token backend simply reports NetworkEgress=false.
	if err := windowsNetworkDenyDecision(domain.ConfinementBox{WorkspaceRoot: `C:\work`}); err != nil {
		t.Errorf("a network-open box must be accepted: %v", err)
	}

	// A non-empty NetworkAllow is a TIGHTENING the token backend cannot enforce. Running
	// network-open silently would leave a fence the user believes is in place as a no-op, so
	// it fails closed — the same position landlock takes below ABI 4.
	err := windowsNetworkDenyDecision(domain.ConfinementBox{
		WorkspaceRoot: `C:\work`,
		NetworkAllow:  []string{"example.com:443"},
	})
	if err == nil {
		t.Fatal("a network-deny box was accepted; want ErrConfinementUnavailable (never a silent no-op)")
	}
	if !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Errorf("err = %v; want ErrConfinementUnavailable so dispatch gates the call", err)
	}
}

func TestLabelJournalRoundTripAndAccessors(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := labelJournalPath(home, 4242)
	journal := labelJournal{
		PID: 4242,
		Entries: []labelJournalEntry{
			{Path: `C:\work`, Root: true},
			{Path: `D:\cache`, Root: true, PriorSDDL: "S:AI(ML;OICI;NW;;;ME)"},
			{Path: `C:\work\downloaded.txt`, PriorSDDL: "S:AI(ML;;NW;;;LW)"},
		},
	}
	if err := writeLabelJournal(path, journal); err != nil {
		t.Fatalf("writeLabelJournal: %v", err)
	}

	got, err := readLabelJournal(path)
	if err != nil {
		t.Fatalf("readLabelJournal: %v", err)
	}
	if got.PID != journal.PID || len(got.Entries) != len(journal.Entries) {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, journal)
	}

	// roots() drives the teardown walk; priorLabels() drives what is put back afterwards.
	roots := got.roots()
	if len(roots) != 2 || roots[0] != `C:\work` || roots[1] != `D:\cache` {
		t.Errorf("roots() = %v, want the two journalled box roots", roots)
	}
	priors := got.priorLabels()
	if len(priors) != 2 || priors[`C:\work\downloaded.txt`] != "S:AI(ML;;NW;;;LW)" {
		t.Errorf("priorLabels() = %v, want the two pre-existing descriptors", priors)
	}

	// listLabelJournals finds it by name, and ignores anything else in the directory.
	if err := os.WriteFile(filepath.Join(labelJournalDir(home), "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("seed stray file: %v", err)
	}
	found := listLabelJournals(home)
	if len(found) != 1 || found[0] != path {
		t.Errorf("listLabelJournals = %v, want just %q", found, path)
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
	path := labelJournalPath(home, 77)

	first := labelJournal{PID: 77, Entries: []labelJournalEntry{{Path: `C:\work`, Root: true}}}
	if err := writeLabelJournal(path, first); err != nil {
		t.Fatalf("writeLabelJournal (create): %v", err)
	}
	second := labelJournal{PID: 77, Entries: []labelJournalEntry{
		{Path: `C:\work`, Root: true},
		{Path: `C:\work\vendor`, PriorSDDL: "S:AI(ML;;NW;;;ME)"},
	}}
	if err := writeLabelJournal(path, second); err != nil {
		t.Fatalf("writeLabelJournal (replace): %v", err)
	}

	got, err := readLabelJournal(path)
	if err != nil {
		t.Fatalf("readLabelJournal after the replacing write: %v", err)
	}
	if len(got.Entries) != 2 || got.Entries[1].Path != `C:\work\vendor` {
		t.Errorf("journal = %+v, want the second write's entries whole", got)
	}

	entries, err := os.ReadDir(labelJournalDir(home))
	if err != nil {
		t.Fatalf("read the journal dir: %v", err)
	}
	if len(entries) != 1 || filepath.Join(labelJournalDir(home), entries[0].Name()) != path {
		t.Errorf("journal dir holds %v, want only %q — a temp file left behind is debris", entries, path)
	}

	// Even if debris DID survive a crash, it is not a journal: the name matches neither half
	// of the journal naming rule, so nothing lists, reads or reports it.
	debris := filepath.Join(labelJournalDir(home), labelJournalTempPrefix+"1234"+labelJournalTempSuffix)
	if err := os.WriteFile(debris, []byte("half a journal"), 0o600); err != nil {
		t.Fatalf("seed temp debris: %v", err)
	}
	if found := listLabelJournals(home); len(found) != 1 || found[0] != path {
		t.Errorf("listLabelJournals = %v, want just %q", found, path)
	}
	if got := confinementResidue(home); strings.Contains(got, "unreadable") || strings.Contains(got, debris) {
		t.Errorf("confinementResidue = %q; temp debris must not be reported as an unreadable journal", got)
	}
}

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
			path := labelJournalPath(home, 1234)
			journal := labelJournal{PID: 1234, Entries: []labelJournalEntry{{Path: `C:\work`, Root: true}}}
			if err := writeLabelJournal(path, journal); err != nil {
				t.Fatalf("seed journal: %v", err)
			}

			var seen labelJournal
			err := retireLabelJournal(path, journal, func(j labelJournal) error {
				seen = j
				return tt.revertErr
			})
			if !errors.Is(err, tt.wantErrIs) {
				t.Fatalf("retireLabelJournal err = %v, want %v", err, tt.wantErrIs)
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
	if err := retireLabelJournal("", labelJournal{}, func(labelJournal) error { return nil }); err != nil {
		t.Errorf("retireLabelJournal(\"\") = %v, want nil", err)
	}
	sentinel := errors.New("revert failed")
	if err := retireLabelJournal("", labelJournal{}, func(labelJournal) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Errorf("retireLabelJournal(\"\") = %v, want the revert error", err)
	}

	// An already-absent journal file is not a failure: recovery may run twice over the same
	// home, and the second pass must not invent an error out of work already done.
	gone := labelJournalPath(t.TempDir(), 7)
	if err := retireLabelJournal(gone, labelJournal{}, func(labelJournal) error { return nil }); err != nil {
		t.Errorf("retireLabelJournal on a missing file = %v, want nil", err)
	}
}

func TestConfinementTeardownNoticeWordsTheFailure(t *testing.T) {
	t.Parallel()

	if got := ConfinementTeardownNotice(nil); got != "" {
		t.Errorf("ConfinementTeardownNotice(nil) = %q, want \"\" so the caller can state it unconditionally", got)
	}

	got := ConfinementTeardownNotice(errors.New(`the journal "C:\Users\dev\.apogee\confinement\labels-9.json" is kept`))
	if strings.Contains(got, "\n") {
		t.Errorf("notice = %q; want a single stderr line", got)
	}
	if !strings.Contains(got, `labels-9.json`) {
		t.Errorf("notice = %q; want it to name the journal that survived the failure", got)
	}
	if !strings.Contains(got, windowsLabelRemedy) {
		t.Errorf("notice = %q; want the same manual remedy the host report names", got)
	}
	if !strings.Contains(windowsResidueNotice([]string{`C:\work`}, nil), windowsLabelRemedy) {
		t.Error("the host report no longer quotes the shared remedy; the two surfaces have drifted")
	}
}

func TestConfinementResidueReportsOnlyForeignJournals(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if got := confinementResidue(home); got != "" {
		t.Errorf("confinementResidue on a clean home = %q, want \"\" (there is nothing to report)", got)
	}

	// This process's own journal is the live session's fence, not residue: reporting it would
	// tell a user their own running session had left labels behind.
	if err := writeLabelJournal(labelJournalPath(home, os.Getpid()), labelJournal{
		PID:     os.Getpid(),
		Entries: []labelJournalEntry{{Path: `C:\mine`, Root: true}},
	}); err != nil {
		t.Fatalf("write own journal: %v", err)
	}
	if got := confinementResidue(home); got != "" {
		t.Errorf("confinementResidue reported this process's own journal: %q", got)
	}

	// A journal from another process is the finding the host report exists to surface, and it
	// must name both the affected path and the manual remedy.
	if err := writeLabelJournal(labelJournalPath(home, os.Getpid()+1), labelJournal{
		PID:     os.Getpid() + 1,
		Entries: []labelJournalEntry{{Path: `C:\work\proj`, Root: true}},
	}); err != nil {
		t.Fatalf("write foreign journal: %v", err)
	}
	got := confinementResidue(home)
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
	garbage := labelJournalPath(home, 909)
	if err := os.MkdirAll(labelJournalDir(home), 0o700); err != nil {
		t.Fatalf("create journal dir: %v", err)
	}
	if err := os.WriteFile(garbage, []byte(`{"pid":909,"entries":[{"path":"C:\\wo`), 0o600); err != nil {
		t.Fatalf("seed a half-written journal: %v", err)
	}

	got := confinementResidue(home)
	if !strings.Contains(got, "unreadable") || !strings.Contains(got, garbage) {
		t.Fatalf("residue = %q; want it to name the unreadable journal %q", got, garbage)
	}
	if !strings.Contains(got, windowsLabelRemedy) {
		t.Errorf("residue = %q; want the manual remedy, which is the ONLY one for a journal no run can decode", got)
	}

	// A readable journal alongside it is still reported on its own terms: one finding must not
	// swallow the other.
	if err := writeLabelJournal(labelJournalPath(home, os.Getpid()+1), labelJournal{
		PID:     os.Getpid() + 1,
		Entries: []labelJournalEntry{{Path: `C:\work\proj`, Root: true}},
	}); err != nil {
		t.Fatalf("write foreign journal: %v", err)
	}
	got = confinementResidue(home)
	if !strings.Contains(got, garbage) || !strings.Contains(got, `C:\work\proj`) {
		t.Errorf("residue = %q; want both the unreadable journal and the still-labelled path", got)
	}
}

func TestWindowsResidueNoticeWordsBothFindings(t *testing.T) {
	t.Parallel()

	const journal = `C:\Users\dev\.apogee\confinement\labels-9.json`

	tests := []struct {
		name       string
		roots      []string
		unreadable []string
		want       []string // substrings the notice must carry
		wantEmpty  bool
	}{
		{
			name:      "nothing_outstanding",
			wantEmpty: true,
		},
		{
			name:  "outstanding_labels",
			roots: []string{`C:\work`, `D:\cache`},
			want:  []string{"2 path(s)", `C:\work, D:\cache`, "reverts them automatically", windowsLabelRemedy},
		},
		{
			name:       "unreadable_journal",
			unreadable: []string{journal},
			want:       []string{"journal present but unreadable: " + journal, "undecodable", windowsLabelRemedy},
		},
		{
			name:       "both_findings_are_stated",
			roots:      []string{`C:\work`},
			unreadable: []string{journal},
			want:       []string{`C:\work`, "journal present but unreadable: " + journal},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := windowsResidueNotice(tt.roots, tt.unreadable)
			if tt.wantEmpty {
				if got != "" {
					t.Fatalf("notice = %q, want \"\" so the caller can state it unconditionally", got)
				}
				return
			}
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("notice = %q; want it to carry %q", got, want)
				}
			}
			// Every continuation line stays aligned under the host report's "labels:" field.
			for _, line := range strings.Split(got, "\n")[1:] {
				if !strings.HasPrefix(line, windowsResidueIndent) {
					t.Errorf("continuation line %q is not indented under the labels field", line)
				}
			}
		})
	}

	// The labels half is worded exactly as it was before the unreadable finding joined it: the
	// host report renders this verbatim and its wording is pinned by internal/probe's tests.
	want := "1 path(s) may still carry apogee's Low integrity label: C:\\work\n" +
		windowsResidueIndent + "(a run was interrupted, or another apogee holds them now; a new session\n" +
		windowsResidueIndent + "reverts them automatically, or: " + windowsLabelRemedy + ")"
	if got := windowsResidueNotice([]string{`C:\work`}, nil); got != want {
		t.Errorf("the outstanding-labels wording drifted:\n got %q\nwant %q", got, want)
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
		entries     []labelJournalEntry
		entry       labelJournalEntry
		wantChanged bool
		wantEntries []labelJournalEntry
	}{
		{
			name:        "first_root_is_recorded",
			entry:       labelJournalEntry{Path: `C:\work`, Root: true},
			wantChanged: true,
			wantEntries: []labelJournalEntry{{Path: `C:\work`, Root: true}},
		},
		{
			name:        "foreign_prior_is_kept_verbatim",
			entry:       labelJournalEntry{Path: `C:\work\vendor`, PriorSDDL: foreignMedium},
			wantChanged: true,
			wantEntries: []labelJournalEntry{{Path: `C:\work\vendor`, PriorSDDL: foreignMedium}},
		},
		{
			name:        "foreign_root_prior_is_kept_verbatim",
			entry:       labelJournalEntry{Path: `C:\work`, Root: true, PriorSDDL: foreignHigh},
			wantChanged: true,
			wantEntries: []labelJournalEntry{{Path: `C:\work`, Root: true, PriorSDDL: foreignHigh}},
		},
		{
			name:        "own_dir_label_as_root_prior_is_recorded_as_no_prior",
			entry:       labelJournalEntry{Path: `C:\work`, Root: true, PriorSDDL: windowsDirLabelSDDL},
			wantChanged: true,
			wantEntries: []labelJournalEntry{{Path: `C:\work`, Root: true}},
		},
		{
			name:  "own_file_label_on_a_descendant_is_not_recorded_at_all",
			entry: labelJournalEntry{Path: `C:\work\main.go`, PriorSDDL: windowsFileLabelSDDL},
		},
		{
			name:  "inherited_own_label_on_a_descendant_is_not_recorded_at_all",
			entry: labelJournalEntry{Path: `C:\work\main.go`, PriorSDDL: ownInherited},
		},
		{
			name:  "canonical_low_sid_is_recognised_as_our_own",
			entry: labelJournalEntry{Path: `C:\work\main.go`, PriorSDDL: ownCanonical},
		},
		{
			name:        "duplicate_path_keeps_the_first_prior",
			entries:     []labelJournalEntry{{Path: `C:\work`, Root: true, PriorSDDL: foreignMedium}},
			entry:       labelJournalEntry{Path: `C:\work`, Root: true, PriorSDDL: windowsDirLabelSDDL},
			wantEntries: []labelJournalEntry{{Path: `C:\work`, Root: true, PriorSDDL: foreignMedium}},
		},
		{
			name:        "case_varied_duplicate_path_is_the_same_path",
			entries:     []labelJournalEntry{{Path: `C:\Work`, Root: true}},
			entry:       labelJournalEntry{Path: `c:\work`, Root: true, PriorSDDL: windowsDirLabelSDDL},
			wantEntries: []labelJournalEntry{{Path: `C:\Work`, Root: true}},
		},
		{
			name:        "a_journalled_descendant_can_still_become_a_root",
			entries:     []labelJournalEntry{{Path: `C:\work\vendor`, PriorSDDL: foreignMedium}},
			entry:       labelJournalEntry{Path: `C:\WORK\VENDOR`, Root: true, PriorSDDL: windowsDirLabelSDDL},
			wantChanged: true,
			wantEntries: []labelJournalEntry{{Path: `C:\work\vendor`, Root: true, PriorSDDL: foreignMedium}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, changed := journalLabelEntry(tt.entries, tt.entry, foldLabelPath)
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
				if isLowLabelSDDL(entry.PriorSDDL) {
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
	entries := []labelJournalEntry{{Path: `C:\Work`, Root: true}}
	if _, changed := journalLabelEntry(entries, labelJournalEntry{Path: `c:\WORK`, Root: true}, nil); changed {
		t.Error("the default fold treated two case spellings of one path as two paths")
	}
	identity := func(p string) string { return p }
	if _, changed := journalLabelEntry(entries, labelJournalEntry{Path: `c:\WORK`, Root: true}, identity); !changed {
		t.Error("the injected fold was ignored; the helper is not honouring its seam")
	}
}

func TestIsLowLabelSDDL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sddl string
		want bool
	}{
		{name: "empty_descriptor", sddl: ""},
		{name: "no_label_ace", sddl: "S:"},
		{name: "own_dir_label", sddl: windowsDirLabelSDDL, want: true},
		{name: "own_file_label", sddl: windowsFileLabelSDDL, want: true},
		{name: "inherited_own_label", sddl: "S:AI(ML;OICIID;NW;;;LW)", want: true},
		{name: "canonical_low_sid", sddl: "S:AI(ML;;NW;;;s-1-16-4096)", want: true},
		{name: "medium_label", sddl: "S:AI(ML;;NW;;;ME)"},
		{name: "high_label", sddl: "S:(ML;OICI;NW;;;HI)"},
		{name: "truncated_ace", sddl: "S:(ML;OICI;NW;;;LW"},
		{name: "audit_ace_then_low_label", sddl: "S:(AU;SA;WD;;;WD)(ML;;NW;;;LW)", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := isLowLabelSDDL(tt.sddl); got != tt.want {
				t.Errorf("isLowLabelSDDL(%q) = %v, want %v", tt.sddl, got, tt.want)
			}
		})
	}
}
