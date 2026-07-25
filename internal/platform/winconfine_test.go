package platform

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform/winlabel"
)

// The Windows token backend's DECISIONS are pure functions over the Windows rule table, so
// they are exercised here on every OS — the injected-seam pattern the whole package follows
// (host.go). The native Windows run (confiner_windows_test.go) proves the OS half; this file
// proves the policy half, including the cases a real machine will not hand you on demand: a
// box root that is C:\, one that swallows %ProgramFiles%, one spelled as an unresolvable 8.3
// short name.

// winTestRules is the Windows rule table with a deterministic long-path resolver, so an 8.3
// short name resolves exactly when the test says it does: a hit in long is an authoritative
// answer, a miss is a resolver that could not answer (the real seam's ok=false).
func winTestRules(long map[string]string) hostRules {
	rules := windowsRules()
	rules.longPath = func(p string) (string, bool) {
		expanded, ok := long[p]
		return expanded, ok
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
			// Win32 canonicalization strips a trailing dot, so this spelling IS the system
			// root and the guardrail must see it as such (the fold in sameComponent).
			name:      "trailing_dot_spelling_of_a_protected_root_refused",
			box:       domain.ConfinementBox{WorkspaceRoot: `C:\Windows.`},
			wantRefus: "protected location",
		},
		{
			name:      "trailing_space_spelling_of_a_protected_root_refused",
			box:       domain.ConfinementBox{WorkspaceRoot: `C:\Windows `},
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

	// A root GENUINELY named like a short name comes back from the resolver unchanged —
	// demo~1 IS its own long name. That is an authoritative answer, not a failure, and the
	// box is accepted rather than forced into Gate mode (ADR 0020 §6 rejects only what
	// genuinely cannot be normalised).
	genuine := winTestRules(map[string]string{`C:\work\demo~1`: `C:\work\demo~1`})
	if _, err := windowsBoxRoots(genuine, domain.ConfinementBox{WorkspaceRoot: `C:\work\demo~1`}, []string{`C:\Program Files`}); err != nil {
		t.Errorf("a genuinely tilde-named root the resolver verified unchanged was refused: %v", err)
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
			path := winlabel.JournalPath(home, 1234)
			journal := winlabel.Record{PID: 1234, Entries: []winlabel.Entry{{Path: `C:\work`, Root: true}}}
			if err := winlabel.WriteJournal(path, journal); err != nil {
				t.Fatalf("seed journal: %v", err)
			}

			var seen winlabel.Record
			_, err := retireLabelJournal(path, journal, func(j winlabel.Record) ([]winlabel.Entry, error) {
				seen = j
				return nil, tt.revertErr
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
	if _, err := retireLabelJournal("", winlabel.Record{}, func(winlabel.Record) ([]winlabel.Entry, error) { return nil, nil }); err != nil {
		t.Errorf("retireLabelJournal(\"\") = %v, want nil", err)
	}
	sentinel := errors.New("revert failed")
	if _, err := retireLabelJournal("", winlabel.Record{}, func(winlabel.Record) ([]winlabel.Entry, error) { return nil, sentinel }); !errors.Is(err, sentinel) {
		t.Errorf("retireLabelJournal(\"\") = %v, want the revert error", err)
	}

	// An already-absent journal file is not a failure: recovery may run twice over the same
	// home, and the second pass must not invent an error out of work already done.
	gone := winlabel.JournalPath(t.TempDir(), 7)
	if _, err := retireLabelJournal(gone, winlabel.Record{}, func(winlabel.Record) ([]winlabel.Entry, error) { return nil, nil }); err != nil {
		t.Errorf("retireLabelJournal on a missing file = %v, want nil", err)
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
	path := winlabel.JournalPath(home, 4321)
	journal := winlabel.Record{PID: 4321, Entries: []winlabel.Entry{
		{Path: `C:\work`, Root: true, PriorSDDL: "S:AI(ML;OICI;NW;;;ME)"},
		{Path: `C:\scratch`, Root: true},
	}}
	if err := winlabel.WriteJournal(path, journal); err != nil {
		t.Fatalf("seed journal: %v", err)
	}

	handoff := []winlabel.Entry{{Path: `C:\work`, Root: true, PriorSDDL: "S:AI(ML;OICI;NW;;;ME)"}}
	remaining, err := retireLabelJournal(path, journal, func(winlabel.Record) ([]winlabel.Entry, error) {
		return handoff, nil
	})
	if err != nil {
		t.Fatalf("retireLabelJournal = %v, want nil — a handoff is not a failed revert", err)
	}
	if len(remaining) != 1 || remaining[0] != handoff[0] {
		t.Fatalf("remaining = %+v, want the handed-off entry back verbatim", remaining)
	}

	kept, err := winlabel.ReadJournal(path)
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
	if remaining, err := retireLabelJournal(path, kept, func(winlabel.Record) ([]winlabel.Entry, error) {
		return nil, nil
	}); err != nil || len(remaining) != 0 {
		t.Fatalf("retireLabelJournal (final) = %+v, %v; want a full retirement", remaining, err)
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
	journal := winlabel.Record{PID: 100, Entries: []winlabel.Entry{
		{Path: `C:\work`, Root: true, PriorSDDL: foreignMedium},
		{Path: `C:\work\vendor\lib.dll`, PriorSDDL: foreignHigh},
		{Path: `C:\scratch`, Root: true},
	}}

	tests := []struct {
		name        string
		siblings    []winlabel.Record
		wantRestore map[string]string
		wantHandoff []winlabel.Entry
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
			siblings: []winlabel.Record{
				{PID: 200, Entries: []winlabel.Entry{{Path: `C:\work`, Root: true}}},
			},
			wantRestore: map[string]string{},
			wantHandoff: []winlabel.Entry{
				{Path: `C:\work`, Root: true, PriorSDDL: foreignMedium},
				{Path: `C:\work\vendor\lib.dll`, PriorSDDL: foreignHigh},
			},
		},
		{
			name: "case_folded_claim_names_the_same_tree",
			siblings: []winlabel.Record{
				{PID: 200, Entries: []winlabel.Entry{{Path: `c:\WORK`, Root: true}}},
			},
			wantRestore: map[string]string{},
			wantHandoff: []winlabel.Entry{
				{Path: `C:\work`, Root: true, PriorSDDL: foreignMedium},
				{Path: `C:\work\vendor\lib.dll`, PriorSDDL: foreignHigh},
			},
		},
		{
			name: "claim_on_an_unrelated_root_hands_off_nothing",
			siblings: []winlabel.Record{
				{PID: 200, Entries: []winlabel.Entry{{Path: `C:\scratch`, Root: true}}},
			},
			wantRestore: map[string]string{
				`C:\work`:                foreignMedium,
				`C:\work\vendor\lib.dll`: foreignHigh,
			},
		},
		{
			name: "a_sibling_prefix_root_is_not_a_containing_root",
			siblings: []winlabel.Record{
				{PID: 200, Entries: []winlabel.Entry{{Path: `C:\wo`, Root: true}}},
			},
			wantRestore: map[string]string{
				`C:\work`:                foreignMedium,
				`C:\work\vendor\lib.dll`: foreignHigh,
			},
		},
		{
			name: "a_siblings_prior_only_entry_claims_no_tree",
			siblings: []winlabel.Record{
				{PID: 200, Entries: []winlabel.Entry{{Path: `C:\work`, PriorSDDL: foreignMedium}}},
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

	// The below-root accounting clearLabelTree hands to retireLabelJournal's decision. A nil
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
	journal := winlabel.Record{PID: 100, Entries: []winlabel.Entry{
		{Path: `C:\work`, Root: true},
		{Path: `C:\scratch`, Root: true},
	}}

	tests := []struct {
		name     string
		siblings []winlabel.Record
		live     map[int]bool
		want     []string
	}{
		{
			name: "no_siblings_keeps_every_root",
			want: []string{`C:\work`, `C:\scratch`},
		},
		{
			name: "live_sibling_spares_the_shared_root_only",
			siblings: []winlabel.Record{
				{PID: 200, Entries: []winlabel.Entry{{Path: `C:\work`, Root: true}}},
			},
			live: map[int]bool{200: true},
			want: []string{`C:\scratch`},
		},
		{
			name: "dead_sibling_spares_nothing",
			siblings: []winlabel.Record{
				{PID: 200, Entries: []winlabel.Entry{{Path: `C:\work`, Root: true}}},
			},
			want: []string{`C:\work`, `C:\scratch`},
		},
		{
			name: "case_folded_spelling_names_the_same_root",
			siblings: []winlabel.Record{
				{PID: 200, Entries: []winlabel.Entry{{Path: `c:\WORK`, Root: true}}},
			},
			live: map[int]bool{200: true},
			want: []string{`C:\scratch`},
		},
		{
			name: "a_live_siblings_prior_only_entry_claims_no_root",
			siblings: []winlabel.Record{
				{PID: 200, Entries: []winlabel.Entry{{Path: `C:\work`, PriorSDDL: "S:(ML;;NW;;;ME)"}}},
			},
			live: map[int]bool{200: true},
			want: []string{`C:\work`, `C:\scratch`},
		},
		{
			name: "both_roots_claimed_by_live_siblings_spares_everything",
			siblings: []winlabel.Record{
				{PID: 200, Entries: []winlabel.Entry{{Path: `C:\work`, Root: true}}},
				{PID: 300, Entries: []winlabel.Entry{{Path: `C:\scratch`, Root: true}}},
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

func TestConfinementTeardownNoticeDelegatesToWinlabel(t *testing.T) {
	t.Parallel()

	// The wording moved to winlabel and this export is a one-line delegation (D8). A
	// delegation that silently stopped delegating is the one failure mode the move
	// introduced, and it is invisible to every test that moved with the wording.
	err := errors.New(`the journal "C:\Users\dev\.apogee\confinement\labels-9.json" is kept`)
	if got, want := ConfinementTeardownNotice(err), winlabel.TeardownNotice(err); got != want {
		t.Errorf("ConfinementTeardownNotice = %q; want winlabel.TeardownNotice's %q", got, want)
	}
	if got := ConfinementTeardownNotice(nil); got != "" {
		t.Errorf("ConfinementTeardownNotice(nil) = %q, want \"\" so the caller can state it unconditionally", got)
	}
}

func TestWindowsLabelProgressNoticeDelegatesToWinlabel(t *testing.T) {
	t.Parallel()

	const root = `C:\work\proj`
	if got, want := WindowsLabelProgressNotice(root), winlabel.ProgressNotice(root); got != want {
		t.Errorf("WindowsLabelProgressNotice = %q; want winlabel.ProgressNotice's %q", got, want)
	}
}

func TestBelowWindowsFloor(t *testing.T) {
	t.Parallel()

	// The deny-vs-token selection (ADR 0020 §5): below the floor a Windows host gets
	// denyConfiner and today's degradation notice, at or above it the token backend. The
	// below-floor branch is the one no development machine can be on, which is why the
	// decision is a pure predicate rather than an inline read of the host's own build.
	tests := []struct {
		name  string
		build uint32
		want  bool
	}{
		{name: "one_below_the_floor", build: 17762, want: true},
		{name: "the_floor_itself", build: 17763},
		{name: "a_current_windows_11_build", build: 26200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := belowWindowsFloor(tt.build); got != tt.want {
				t.Errorf("belowWindowsFloor(%d) = %v, want %v", tt.build, got, tt.want)
			}
		})
	}
}
