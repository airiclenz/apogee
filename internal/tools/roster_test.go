package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestEffectiveRoster_WalksTheLadder is the ladder itself, one row per rung and per crossing:
// profile > global > build default (ADR 0057). The probe set holds a plain tool, a tool that
// declares itself default-off, and a tool that implements the marker returning false — the
// carve-out that must still count as on the menu.
func TestEffectiveRoster_WalksTheLadder(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		deltas RosterDeltas
		want   []string
	}{
		{
			name: "no deltas — the build default alone, so the default-off tool is absent",
			want: []string{"plain_probe", "declared_on_probe"},
		},
		{
			name:   "global enabled lifts the default-off tool",
			deltas: RosterDeltas{Global: domain.ToolRosterDelta{Enabled: []string{"off_probe"}}},
			want:   []string{"plain_probe", "off_probe", "declared_on_probe"},
		},
		{
			name:   "global disabled drops a menu tool",
			deltas: RosterDeltas{Global: domain.ToolRosterDelta{Disabled: []string{"plain_probe"}}},
			want:   []string{"declared_on_probe"},
		},
		{
			name: "profile disabled beats global enabled",
			deltas: RosterDeltas{
				Global:  domain.ToolRosterDelta{Enabled: []string{"off_probe"}},
				Profile: domain.ToolRosterDelta{Disabled: []string{"off_probe"}},
			},
			want: []string{"plain_probe", "declared_on_probe"},
		},
		{
			name: "profile enabled beats global disabled — the ratifying use case",
			deltas: RosterDeltas{
				Global:  domain.ToolRosterDelta{Disabled: []string{"plain_probe", "off_probe"}},
				Profile: domain.ToolRosterDelta{Enabled: []string{"off_probe"}},
			},
			want: []string{"off_probe", "declared_on_probe"},
		},
		{
			name: "profile enabled lifts a default-off tool with no global word at all",
			deltas: RosterDeltas{
				Profile: domain.ToolRosterDelta{Enabled: []string{"off_probe"}},
			},
			want: []string{"plain_probe", "off_probe", "declared_on_probe"},
		},
		{
			name:   "an unknown name in any list names nothing",
			deltas: RosterDeltas{Profile: domain.ToolRosterDelta{Disabled: []string{"plain_probee"}}},
			want:   []string{"plain_probe", "declared_on_probe"},
		},
		{
			name:   "names are trimmed — a stray space is a spelling, not another tool",
			deltas: RosterDeltas{Global: domain.ToolRosterDelta{Disabled: []string{"  plain_probe  "}}},
			want:   []string{"declared_on_probe"},
		},
		{
			name: "an empty YAML item names nothing and is not a conflict",
			deltas: RosterDeltas{
				Global: domain.ToolRosterDelta{Disabled: []string{"  "}, Enabled: []string{""}},
			},
			want: []string{"plain_probe", "declared_on_probe"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			kept, conflicts := EffectiveRoster(rosterProbes(), tc.deltas)
			if got := rosterNamesOf(kept); got != strings.Join(tc.want, ",") {
				t.Errorf("roster = %q, want %q", got, strings.Join(tc.want, ","))
			}
			if len(conflicts) != 0 {
				t.Errorf("conflicts = %v, want none", conflicts)
			}
		})
	}
}

// TestEffectiveRoster_SameScopeConflictDisablesAndReports pins the fail-closed half of decision 4:
// a tool named in BOTH lists of one scope leaves the menu, and the conflict comes back to the
// caller so the host can say so in one line. The roster is still built — a roster the user is
// editing must never be able to stop a session from starting.
func TestEffectiveRoster_SameScopeConflictDisablesAndReports(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		deltas RosterDeltas
		want   []RosterConflict
	}{
		{
			name: "global",
			deltas: RosterDeltas{Global: domain.ToolRosterDelta{
				Disabled: []string{"plain_probe"},
				Enabled:  []string{"plain_probe"},
			}},
			want: []RosterConflict{{Scope: RosterScopeGlobal, Tool: "plain_probe"}},
		},
		{
			name: "profile",
			deltas: RosterDeltas{Profile: domain.ToolRosterDelta{
				Disabled: []string{"plain_probe"},
				Enabled:  []string{"plain_probe"},
			}},
			want: []RosterConflict{{Scope: RosterScopeProfile, Tool: "plain_probe"}},
		},
		{
			name: "both scopes, reported global first and deduplicated",
			deltas: RosterDeltas{
				Global: domain.ToolRosterDelta{
					Disabled: []string{"plain_probe", "plain_probe"},
					Enabled:  []string{"plain_probe"},
				},
				Profile: domain.ToolRosterDelta{
					Disabled: []string{" plain_probe "},
					Enabled:  []string{"plain_probe"},
				},
			},
			want: []RosterConflict{
				{Scope: RosterScopeGlobal, Tool: "plain_probe"},
				{Scope: RosterScopeProfile, Tool: "plain_probe"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			kept, conflicts := EffectiveRoster(rosterProbes(), tc.deltas)
			if got := rosterNamesOf(kept); got != "declared_on_probe" {
				t.Errorf("roster = %q, want %q — disabled must win a same-scope tie", got, "declared_on_probe")
			}
			if len(conflicts) != len(tc.want) {
				t.Fatalf("conflicts = %v, want %v", conflicts, tc.want)
			}
			for i, want := range tc.want {
				if conflicts[i] != want {
					t.Errorf("conflict %d = %v, want %v", i, conflicts[i], want)
				}
			}
			// The same rule answered without a tool in sight — the query the config layer asks at
			// load time must agree with the one the assembly applied.
			if standalone := RosterConflicts(tc.deltas); len(standalone) != len(conflicts) {
				t.Errorf("RosterConflicts alone = %v, want the %d the roster pass reported", standalone, len(conflicts))
			}
		})
	}
}

// TestEffectiveRoster_LeavesTheGivenSetUntouched pins the function's purity, which is what lets an
// injected domain.Config.Tools stay the host's verbatim authority: the ladder only ever answers
// about the set it is HANDED — it never mutates or reorders it — and with nothing to subtract it
// hands the very same slice back, so the default roster costs nothing.
func TestEffectiveRoster_LeavesTheGivenSetUntouched(t *testing.T) {
	t.Parallel()

	given := rosterProbes()
	before := rosterNamesOf(given)

	kept, _ := EffectiveRoster(given, RosterDeltas{
		Global:  domain.ToolRosterDelta{Disabled: []string{"plain_probe"}},
		Profile: domain.ToolRosterDelta{Enabled: []string{"off_probe"}},
	})
	if after := rosterNamesOf(given); after != before {
		t.Errorf("the given set became %q, want the untouched %q", after, before)
	}
	if got := rosterNamesOf(kept); got == before {
		t.Error("the deltas subtracted nothing — the probe set no longer proves purity")
	}

	// A set with nothing to subtract comes back as the identical slice, not a copy.
	onlyMenu := []domain.Tool{newRosterProbe("plain_probe")}
	same, _ := EffectiveRoster(onlyMenu, RosterDeltas{})
	if len(same) != len(onlyMenu) || &same[0] != &onlyMenu[0] {
		t.Error("a roster with no deltas and no default-off tool must return the very slice it was given")
	}
}

// TestDefaultToolsHonourTheRoster pins the assembly's own use of the ladder against the shipped
// menu: with no deltas the default set is the whole build MINUS the tools registered default-off
// (the Console family, ADR 0059 — the build rung's first users), and the global lists still
// subtract exactly what they name.
func TestDefaultToolsHonourTheRoster(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	full := DefaultTools(root)

	build := builtinTools(root, HostTools{})
	onMenu := make([]domain.Tool, 0, len(build))
	var offMenu []string
	for _, tool := range build {
		if domain.IsDefaultOff(tool) {
			offMenu = append(offMenu, tool.Name())
			continue
		}
		onMenu = append(onMenu, tool)
	}
	if got, want := rosterNamesOf(full), rosterNamesOf(onMenu); got != want {
		t.Errorf("default menu = %q, want the build minus the default-off tools %q", got, want)
	}
	if got, want := strings.Join(offMenu, ","), strings.Join(consoleFamilyNames, ","); got != want {
		t.Errorf("default-off built-ins = %q, want the Console family %q", got, want)
	}
	if len(KnownToolNames()) < len(full) {
		t.Error("KnownToolNames must name at least every tool the default menu offers")
	}

	pruned := DefaultToolsWithHost(root, HostTools{
		Disabled:      []string{"view_diff"},
		Enabled:       []string{"grep"},
		ProfileRoster: domain.ToolRosterDelta{Disabled: []string{"python_exec"}},
	})
	names := rosterNamesOf(pruned)
	for _, gone := range []string{"view_diff", "python_exec"} {
		if strings.Contains(names, gone) {
			t.Errorf("%q is disabled but still on the menu", gone)
		}
	}
	if !strings.Contains(names, "grep") {
		t.Error("grep is on the default menu and enabled — it must stay")
	}
	if len(pruned) != len(full)-2 {
		t.Errorf("the roster left %d tools, want %d", len(pruned), len(full)-2)
	}
}

// TestDefaultToolsLiftTheConsoleFamily is the positive half of the ladder the shipped menu could
// not show until a built-in went default-off: a name in `enabled:` ADDS a tool rather than merely
// failing to remove one, and the two configuration rungs settle the same tool in the order ADR
// 0057 spells — profile over global, in BOTH directions.
func TestDefaultToolsLiftTheConsoleFamily(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	base := rosterNamesOf(DefaultTools(root))

	cases := []struct {
		name string
		host HostTools
		want string // the whole menu, in order
	}{
		{
			name: "the global rung lifts all four",
			host: HostTools{Enabled: consoleFamilyNames},
			want: base + ",console_open,console_send,console_read,console_close",
		},
		{
			name: "a profile `enabled:` lifts what the global rung disabled — the profile is the last word",
			host: HostTools{
				Disabled:      consoleFamilyNames,
				ProfileRoster: domain.ToolRosterDelta{Enabled: []string{"console_read"}},
			},
			want: base + ",console_read",
		},
		{
			name: "a profile `disabled:` keeps off what the global rung lifted",
			host: HostTools{
				Enabled:       consoleFamilyNames,
				ProfileRoster: domain.ToolRosterDelta{Disabled: []string{"console_open", "console_send"}},
			},
			want: base + ",console_read,console_close",
		},
		{
			name: "naming nothing leaves the family where the build left it",
			host: HostTools{},
			want: base,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := rosterNamesOf(DefaultToolsWithHost(root, tc.host)); got != tc.want {
				t.Errorf("menu = %q, want %q", got, tc.want)
			}
		})
	}
}

// rosterProbes returns the three-tool probe set the ladder tests walk: a plain tool, one that
// declares itself default-off, and one that implements DefaultOffTool returning false (the
// degraded-build carve-out, which is on the menu like a tool that never implemented it).
func rosterProbes() []domain.Tool {
	return []domain.Tool{
		newRosterProbe("plain_probe"),
		offRosterProbe{rosterProbe: newRosterProbe("off_probe"), off: true},
		offRosterProbe{rosterProbe: newRosterProbe("declared_on_probe")},
	}
}

// rosterNamesOf joins a tool set's names in menu order, so a table row can state the whole expected
// roster — and its ORDER, which the ladder must never change — in one string.
func rosterNamesOf(all []domain.Tool) string {
	names := make([]string, 0, len(all))
	for _, tool := range all {
		names = append(names, tool.Name())
	}
	return strings.Join(names, ",")
}

// rosterProbe is a minimal named tool for the roster tests: the ladder only ever reads Name and the
// default-off marker, so nothing here executes.
type rosterProbe struct {
	toolSpec
}

// Execute satisfies domain.Tool; the roster never runs a tool.
func (rosterProbe) Execute(context.Context, domain.ToolCall) (domain.ToolResult, error) {
	return domain.ToolResult{}, nil
}

// newRosterProbe returns a probe tool named name.
func newRosterProbe(name string) rosterProbe {
	return rosterProbe{toolSpec: toolSpec{name: name, description: "test-only roster probe"}}
}

// offRosterProbe carries the optional DefaultOffTool declaration, in both directions: off=true is a
// tool the build keeps off the menu, off=false the carve-out that implements the marker and stays
// on it.
type offRosterProbe struct {
	rosterProbe
	off bool
}

// DefaultOff reports whether this probe is left out of the default menu.
func (p offRosterProbe) DefaultOff() bool { return p.off }
