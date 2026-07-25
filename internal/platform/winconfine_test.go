package platform

import (
	"errors"
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
