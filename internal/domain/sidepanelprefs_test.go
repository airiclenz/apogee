package domain

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestSidePanelShowDefaultsToAutoAndKnowsThreeModes: the shipped default is auto, carried by
// DefaultUIPrefs and read for `""`; the three documented modes parse to themselves; and any other
// name is refused naming the modes this build knows.
func TestSidePanelShowDefaultsToAutoAndKnowsThreeModes(t *testing.T) {
	t.Parallel()

	if got := DefaultUIPrefs().SidePanelShow; got != SidePanelShowAuto {
		t.Errorf("DefaultUIPrefs().SidePanelShow = %q; want auto", got)
	}
	if got, err := ParseSidePanelShow(""); err != nil || got != SidePanelShowAuto {
		t.Errorf(`ParseSidePanelShow("") = %q, %v; want auto`, got, err)
	}
	for _, mode := range SidePanelShowNames() {
		if got, err := ParseSidePanelShow(string(mode)); err != nil || got != mode {
			t.Errorf("ParseSidePanelShow(%q) = %q, %v; want it back", mode, got, err)
		}
	}
	want := []SidePanelShow{SidePanelShowAuto, SidePanelShowOn, SidePanelShowOff}
	if got := SidePanelShowNames(); !slices.Equal(got, want) {
		t.Errorf("SidePanelShowNames() = %v; want %v", got, want)
	}
	_, err := ParseSidePanelShow("sometimes")
	if err == nil || !strings.Contains(err.Error(), `unknown side-panel mode "sometimes" (known modes: auto, on, off)`) {
		t.Errorf("ParseSidePanelShow(sometimes) = %v; want refused naming the known modes", err)
	}
}

// TestSidePanelSectionsDefaultToEverySectionAuto: the default list names every section in auto in
// the documented order — sub-agents, tasks, changed-files, usage, mcp — and `""` reads as it.
func TestSidePanelSectionsDefaultToEverySectionAuto(t *testing.T) {
	t.Parallel()
	want := []SidePanelEntry{
		{SidePanelSubAgents, SidePanelModeAuto}, {SidePanelTasks, SidePanelModeAuto},
		{SidePanelChangedFiles, SidePanelModeAuto}, {SidePanelUsage, SidePanelModeAuto},
		{SidePanelMCP, SidePanelModeAuto},
	}

	for _, text := range []string{string(DefaultUIPrefs().SidePanelSections), "", "  "} {
		got, err := ParseSidePanelSections(text)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("ParseSidePanelSections(%q) = %v, %v; want %v", text, got, err, want)
		}
	}
	if got := SidePanelSectionNames(); !slices.Equal(got,
		[]SidePanelSection{SidePanelSubAgents, SidePanelTasks, SidePanelChangedFiles, SidePanelUsage, SidePanelMCP}) {
		t.Errorf("SidePanelSectionNames() = %v; want the default display order", got)
	}
}

// TestSidePanelSectionsRoundTripThroughTheirCanonicalSpelling: what Parse reads, Spell writes back
// to the same text, and Parse reads that text back to the same entries; a looser spelling of the
// same list lands through Set as the one canonical text, so two spellings never compare unequal.
func TestSidePanelSectionsRoundTripThroughTheirCanonicalSpelling(t *testing.T) {
	t.Parallel()
	const canonical SidePanelSections = "[usage: pinned, tasks: hidden, mcp: pinned, sub-agents: auto, changed-files: pinned]"

	entries, err := ParseSidePanelSections(string(canonical))
	if err != nil {
		t.Fatalf("ParseSidePanelSections(%q): %v", canonical, err)
	}
	if got := SpellSidePanelSections(entries); got != canonical {
		t.Errorf("SpellSidePanelSections(Parse(%q)) = %q; want it back", canonical, got)
	}
	for _, text := range []string{string(canonical), " [usage:pinned ,tasks : hidden,mcp :pinned, sub-agents: auto,changed-files: pinned ] "} {
		u := DefaultUIPrefs()
		if err := u.Set(UIKeySidePanelSections, text); err != nil || u.SidePanelSections != canonical {
			t.Errorf("Set(ui.side-panel.sections, %q) = %v, landing %q; want %q", text, err, u.SidePanelSections, canonical)
		}
	}
	u := UIPrefs{SidePanelSections: canonical}
	if err := u.Set(UIKeySidePanelSections, ""); err != nil || u.SidePanelSections != DefaultSidePanelSections {
		t.Errorf(`Set(ui.side-panel.sections, "") = %v, landing %q; want the default`, err, u.SidePanelSections)
	}
}

// TestSidePanelSectionsAppendTheSectionsAListLeavesOut: a list that names only some sections keeps
// their order and modes first, and every section it leaves out follows in auto, in default order —
// so a list written before a section existed still loads, and an empty list is the default.
func TestSidePanelSectionsAppendTheSectionsAListLeavesOut(t *testing.T) {
	t.Parallel()

	got, err := ParseSidePanelSections("[usage: pinned, tasks: hidden]")

	want := []SidePanelEntry{
		{SidePanelUsage, SidePanelModePinned}, {SidePanelTasks, SidePanelModeHidden},
		{SidePanelSubAgents, SidePanelModeAuto}, {SidePanelChangedFiles, SidePanelModeAuto},
		{SidePanelMCP, SidePanelModeAuto},
	}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("ParseSidePanelSections(partial) = %v, %v; want %v", got, err, want)
	}
	// A list naming every section but mcp — as one written before the section existed does —
	// loads with mcp appended last, in auto.
	const preMCP = "[sub-agents: auto, tasks: pinned, changed-files: auto, usage: hidden]"
	if got, err := ParseSidePanelSections(preMCP); err != nil ||
		SpellSidePanelSections(got) != "[sub-agents: auto, tasks: pinned, changed-files: auto, usage: hidden, mcp: auto]" {
		t.Errorf("ParseSidePanelSections(%q) = %v, %v; want mcp appended in auto", preMCP, got, err)
	}
	if got, err := ParseSidePanelSections("[]"); err != nil || SpellSidePanelSections(got) != DefaultSidePanelSections {
		t.Errorf("ParseSidePanelSections([]) = %v, %v; want the default list", got, err)
	}
}

// TestSidePanelSectionsRefuseEveryBadList: each list the key refuses is refused by Validate in a
// sentence naming the key and quoting the value, and Set refusing it leaves the block as it was.
func TestSidePanelSectionsRefuseEveryBadList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, sections, reason string
	}{
		{"unknown section", "[lsp: auto]", `unknown section "lsp" (known sections: sub-agents, tasks, changed-files, usage, mcp)`},
		{"unknown mode", "[tasks: shown]", `unknown mode "shown" for section "tasks" (known modes: pinned, auto, hidden)`},
		{"no mode", "[tasks: ]", `section "tasks" names no mode`},
		{"named twice", "[tasks: auto, tasks: pinned]", `section "tasks" is named twice`},
		{"no brackets", "tasks: auto", "want a list of section: mode entries"},
		{"no colon", "[tasks]", "want a list of section: mode entries"},
		{"nested", "[[tasks: auto]]", "want a list of section: mode entries"},
		{"a map", "[{tasks: auto}]", "want a list of section: mode entries"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			u := DefaultUIPrefs()
			u.SidePanelSections = SidePanelSections(tt.sections)

			err := u.Validate()

			wantPrefix := "apogee: invalid ui.side-panel.sections " + `"` + tt.sections + `": `
			if err == nil || !strings.HasPrefix(err.Error(), wantPrefix) || !strings.Contains(err.Error(), tt.reason) {
				t.Errorf("Validate() with SidePanelSections %q = %v; want %q naming %q", tt.sections, err, wantPrefix, tt.reason)
			}
			held := DefaultUIPrefs()
			if err := held.Set(UIKeySidePanelSections, tt.sections); err == nil || held != DefaultUIPrefs() {
				t.Errorf("Set(ui.side-panel.sections, %q) = %v, leaving %+v; want refused and untouched", tt.sections, err, held)
			}
		})
	}
}

// TestSidePanelShowZeroValidatesAndBadIsRefusedNamingTheKey: the zero block validates (`""` is
// auto), and a show mode the build does not know is refused by Validate naming the key.
func TestSidePanelShowZeroValidatesAndBadIsRefusedNamingTheKey(t *testing.T) {
	t.Parallel()

	if err := (UIPrefs{}).Validate(); err != nil {
		t.Errorf("the zero block does not validate: %v", err)
	}
	u := DefaultUIPrefs()
	u.SidePanelShow = "wide"
	if err := u.Validate(); err == nil || !strings.HasPrefix(err.Error(), "apogee: invalid ui.side-panel.show: ") {
		t.Errorf("Validate() with SidePanelShow wide = %v; want refused naming ui.side-panel.show", err)
	}
}

// TestSidePanelWidthValidateAndSetJudgeTheStatedWidth: the default is 36 and `""` reads as it; a
// zero field is the unset width and validates, while Set — a stated value — refuses 0 as it refuses
// any width below 24; 24 and up land as written; and the parser refuses text that is no number.
func TestSidePanelWidthValidateAndSetJudgeTheStatedWidth(t *testing.T) {
	t.Parallel()

	if got := DefaultUIPrefs().SidePanelWidth; got != 36 {
		t.Errorf("DefaultUIPrefs().SidePanelWidth = %d; want 36", got)
	}
	if got, err := ParseSidePanelWidth("  "); err != nil || got != DefaultSidePanelWidth {
		t.Errorf(`ParseSidePanelWidth("  ") = %d, %v; want the default`, got, err)
	}
	for _, tt := range []struct {
		width               int
		validates, setLands bool
	}{
		{width: 0, validates: true, setLands: false},
		{width: 23, validates: false, setLands: false},
		{width: 24, validates: true, setLands: true},
		{width: 36, validates: true, setLands: true},
		{width: 200, validates: true, setLands: true},
	} {
		u := DefaultUIPrefs()
		u.SidePanelWidth = tt.width
		if err := u.Validate(); (err == nil) != tt.validates {
			t.Errorf("Validate() with SidePanelWidth %d = %v; want validates %t", tt.width, err, tt.validates)
		}
		set := DefaultUIPrefs()
		err := set.Set(UIKeySidePanelWidth, strconv.Itoa(tt.width))
		if (err == nil) != tt.setLands {
			t.Errorf("Set(%d) = %v; want lands %t", tt.width, err, tt.setLands)
		}
		if tt.setLands && set.SidePanelWidth != tt.width {
			t.Errorf("Set(%d) landed %d", tt.width, set.SidePanelWidth)
		}
		if !tt.setLands && err != nil && !strings.HasPrefix(err.Error(), "apogee: invalid ui.side-panel.width ") {
			t.Errorf("Set(%d) = %q; want refused naming ui.side-panel.width", tt.width, err)
		}
	}
	for _, text := range []string{"wide", "36.5", "-24"} {
		if _, err := ParseSidePanelWidth(text); err == nil {
			t.Errorf("ParseSidePanelWidth(%q) accepted; want refused", text)
		}
	}
}
