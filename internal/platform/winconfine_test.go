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

func TestConfinementResidueReportsOnlyForeignJournals(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if got := ConfinementResidue(home); got != "" {
		t.Errorf("ConfinementResidue on a clean home = %q, want \"\" (there is nothing to report)", got)
	}

	// This process's own journal is the live session's fence, not residue: reporting it would
	// tell a user their own running session had left labels behind.
	if err := writeLabelJournal(labelJournalPath(home, os.Getpid()), labelJournal{
		PID:     os.Getpid(),
		Entries: []labelJournalEntry{{Path: `C:\mine`, Root: true}},
	}); err != nil {
		t.Fatalf("write own journal: %v", err)
	}
	if got := ConfinementResidue(home); got != "" {
		t.Errorf("ConfinementResidue reported this process's own journal: %q", got)
	}

	// A journal from another process is the finding the host report exists to surface, and it
	// must name both the affected path and the manual remedy.
	if err := writeLabelJournal(labelJournalPath(home, os.Getpid()+1), labelJournal{
		PID:     os.Getpid() + 1,
		Entries: []labelJournalEntry{{Path: `C:\work\proj`, Root: true}},
	}); err != nil {
		t.Fatalf("write foreign journal: %v", err)
	}
	got := ConfinementResidue(home)
	if !strings.Contains(got, `C:\work\proj`) {
		t.Errorf("residue = %q; want it to name the still-labelled path", got)
	}
	if !strings.Contains(got, "icacls") {
		t.Errorf("residue = %q; want it to name the manual remedy", got)
	}
}
