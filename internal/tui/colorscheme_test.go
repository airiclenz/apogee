package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/scheme"
)

// ----------------------------------------------------------------------------
// /color-scheme — listing, switching, exporting
// ----------------------------------------------------------------------------

// colorSchemeOpts is the wiring every form of the verb reads: what can be switched to, what a name
// resolves to, what the switch persists through, and where an export writes. Each test blanks the
// seams it wants unwired, which is how the nil-seam degrades below are provoked.
func colorSchemeOpts(log *settingsWriteLog, list []string,
	resolve func(string) (scheme.Scheme, []string), export func(string) (string, error)) Options {
	opts := testOpts
	opts.ColorSchemeName = "dark"
	opts.ListSchemes = func() []string { return list }
	opts.ResolveScheme = resolve
	opts.ExportScheme = export
	if log != nil {
		opts.WriteSetting = log.write
	}
	return opts
}

// runColorSchemeLine types one /color-scheme line and submits it, asserting the verb stayed local:
// no worker, and the input emptied like every other whole-line command. The Cmd is handed back
// because the switch form has one (its repaint) and the other two do not.
func runColorSchemeLine(t *testing.T, m Model, line string) (Model, tea.Cmd) {
	t.Helper()
	m.input.SetValue(line)
	m, cmd := stepCmd(t, m, keyEnter())
	if m.state != stateIdle {
		t.Errorf("state = %v after %q, want idle — the verb opens no worker", m.state, line)
	}
	if v := m.input.Value(); v != "" {
		t.Errorf("input not cleared after %q: %q", line, v)
	}
	return m, cmd
}

// colorSchemeNote is the text of the note the verb just wrote, failing loudly when what it wrote was
// something else — an error entry, in the cases where the act was refused.
func colorSchemeNote(t *testing.T, m Model) string {
	t.Helper()
	last := lastEntry(t, m)
	if last.kind != entryNote {
		t.Fatalf("the verb wrote a %v entry, want a note: %q", last.kind, last.text)
	}
	return last.text
}

// The bare verb answers "what can I pick?": one row per scheme the session discovered, the one in
// force marked, and the grammar for the two things to do next. Built-ins and user files are one
// list, because the human picks a NAME and shadowing has already decided which file that name means.
func TestColorSchemeCommandListsWhatCanBeSwitchedTo(t *testing.T) {
	opts := colorSchemeOpts(nil, []string{"dark", "light", "mine"}, nil, nil)
	opts.ColorSchemeName = "light"
	m, _ := runColorSchemeLine(t, newTestModelEng(t, &fakeEngine{}, opts), "/color-scheme")

	note := colorSchemeNote(t, m)
	for _, want := range []string{"dark", "light  (current)", "mine", "/color-scheme export <name>"} {
		if !strings.Contains(note, want) {
			t.Errorf("the listing is missing %q:\n%s", want, note)
		}
	}
	if strings.Count(note, "(current)") != 1 {
		t.Errorf("the listing marks more than one scheme as current:\n%s", note)
	}
}

// A configured name nothing on disk or in the binary answers to is reported rather than left as an
// unmarked list: the screen is drawn in the default while the file names something else, and that
// gap is exactly what the listing is opened to explain (the forgiving load, ADR 0039 design call 8).
func TestColorSchemeCommandNamesAConfiguredSchemeThatIsNotOnOffer(t *testing.T) {
	opts := colorSchemeOpts(nil, []string{"dark", "light"}, nil, nil)
	opts.ColorSchemeName = "solarized"
	m, _ := runColorSchemeLine(t, newTestModelEng(t, &fakeEngine{}, opts), "/color-scheme")

	if note := colorSchemeNote(t, m); !strings.Contains(note, `the config names "solarized"`) {
		t.Errorf("the listing hides the unresolvable configured name:\n%s", note)
	}
}

// Without a discovery seam there is nothing to offer AND nothing to switch to, so the listing says
// so instead of naming the built-ins it could not load either — the nil-seam degrade every provider
// in [Options] takes.
func TestColorSchemeCommandWithNoDiscoverySeam(t *testing.T) {
	opts := testOpts // no ListSchemes, no ResolveScheme, no ExportScheme
	m, _ := runColorSchemeLine(t, newTestModelEng(t, &fakeEngine{}, opts), "/color-scheme")

	if note := colorSchemeNote(t, m); !strings.Contains(note, "no schemes are on offer") {
		t.Errorf("the empty listing does not say why it is empty:\n%s", note)
	}
}

// The switch is the settings picker's own validate → persist → apply, reached by typing instead of
// by picking (ADR 0037): the key is written through the same seam, the name is RESOLVED again from
// disk, every style is rebuilt from what came back, and the terminal is asked to clear — so the file
// and the screen say the same thing whichever way the human got there.
func TestColorSchemeCommandSwitchesAndPersists(t *testing.T) {
	log := &settingsWriteLog{}
	var asked []string
	opts := colorSchemeOpts(log, []string{"dark", "light"}, func(name string) (scheme.Scheme, []string) {
		asked = append(asked, name)
		return stubScheme("#123456"), nil
	}, nil)
	m, cmd := runColorSchemeLine(t, newTestModelEng(t, &fakeEngine{}, opts), "/color-scheme light")

	if want := []settingEdit{{path: "ui.color-scheme", value: "light"}}; !reflect.DeepEqual(log.writes, want) {
		t.Fatalf("writes = %+v, want %+v — the command persists through the pane's own seam", log.writes, want)
	}
	if !reflect.DeepEqual(asked, []string{"light"}) {
		t.Fatalf("the resolver was asked for %v, want exactly the named scheme", asked)
	}
	if got := hexOf(m.th.errorFg); got != "#123456" {
		t.Errorf("the model's error tone = %s, want the switched scheme's #123456 — the theme did not move", got)
	}
	if m.opts.ColorSchemeName != "light" {
		t.Errorf("ColorSchemeName = %q, want %q", m.opts.ColorSchemeName, "light")
	}
	if got, want := cmdMsg(cmd), tea.ClearScreen(); !reflect.DeepEqual(got, want) {
		t.Errorf("the switch produced %#v, want tea.ClearScreen's Msg %#v", got, want)
	}
	if note := colorSchemeNote(t, m); !strings.Contains(note, `color-scheme "light" — saved and in force`) {
		t.Errorf("the switch note = %q, want it to confirm both halves", note)
	}
	// The journal the pane reads for its "changed this session" marker agrees with the file.
	if want := []settingEdit{{path: "ui.color-scheme", value: "light"}}; !reflect.DeepEqual(m.settingEdits, want) {
		t.Errorf("settingEdits = %+v, want %+v", m.settingEdits, want)
	}
}

// A switch that resolved with complaints still lands — the forgiving load always yields a usable
// palette — and each complaint reaches the human as its own ephemeral note, with the confirmation
// pointing at them rather than repeating them.
func TestColorSchemeCommandSurfacesResolveWarnings(t *testing.T) {
	const warning = `color-scheme "mine.yaml": key "error": bad hex "#zz0000" — using default`
	opts := colorSchemeOpts(&settingsWriteLog{}, []string{"dark", "mine"},
		func(string) (scheme.Scheme, []string) { return stubScheme("#654321"), []string{warning} }, nil)
	m, _ := runColorSchemeLine(t, newTestModelEng(t, &fakeEngine{}, opts), "/color-scheme mine")

	if !hasEntry(m, entryNote, warning) {
		t.Errorf("no note carries %q; entries = %+v", warning, m.transcript.entries)
	}
	if note := colorSchemeNote(t, m); !strings.Contains(note, "applied with 1 warning") {
		t.Errorf("the switch note = %q, want it to point at the warning above it", note)
	}
	if got := hexOf(m.th.errorFg); got != "#654321" {
		t.Errorf("the model's error tone = %s, want #654321 — a warning is not a refusal", got)
	}
}

// A refused write changes NOTHING: the file still names the old scheme, so the screen must go on
// showing it, and the refusal is an error entry because the act did not happen.
func TestColorSchemeCommandReportsARefusedWrite(t *testing.T) {
	log := &settingsWriteLog{err: errors.New("config home is read-only")}
	var resolved bool
	opts := colorSchemeOpts(log, []string{"dark", "light"}, func(string) (scheme.Scheme, []string) {
		resolved = true
		return stubScheme("#123456"), nil
	}, nil)
	before := newTestModelEng(t, &fakeEngine{}, opts)
	m, cmd := runColorSchemeLine(t, before, "/color-scheme light")

	if resolved {
		t.Error("the scheme was applied after the write was refused; the file and the screen would disagree")
	}
	if cmd != nil {
		t.Error("a refused switch asked for a repaint; nothing was repainted")
	}
	if got := hexOf(m.th.errorFg); got != hexOf(before.th.errorFg) {
		t.Errorf("the theme moved to %s on a refused write, want the previous %s", got, hexOf(before.th.errorFg))
	}
	if last := lastEntry(t, m); last.kind != entryError || !strings.Contains(last.text, "config home is read-only") {
		t.Errorf("the refusal was recorded as %v %q, want an error entry naming the reason", last.kind, last.text)
	}
}

// The write landed and the apply could not happen: ADR 0037 decision 1 says the write STANDS, so the
// note is the same "saved — live apply failed" sentence the settings row gives, not a refusal.
func TestColorSchemeCommandKeepsTheWriteWhenTheApplyCannotHappen(t *testing.T) {
	log := &settingsWriteLog{}
	opts := colorSchemeOpts(log, []string{"dark", "light"}, nil, nil) // no ResolveScheme
	m, _ := runColorSchemeLine(t, newTestModelEng(t, &fakeEngine{}, opts), "/color-scheme light")

	if len(log.writes) != 1 {
		t.Fatalf("writes = %+v, want the key persisted even though the apply could not run", log.writes)
	}
	if note := colorSchemeNote(t, m); !strings.HasPrefix(note, settingsApplyFailedNote) {
		t.Errorf("note = %q, want it to open with %q", note, settingsApplyFailedNote)
	}
}

// The export is the only way a scheme file comes into existence, so the note names the file it
// wrote and the line that loads it back — under the SAME name, because a user file shadows the
// built-in it was copied from (design call 6).
func TestColorSchemeCommandExportsAnEditableCopy(t *testing.T) {
	var asked []string
	opts := colorSchemeOpts(nil, []string{"dark", "light"}, nil, func(name string) (string, error) {
		asked = append(asked, name)
		return "/home/u/.apogee/schemes/" + name + ".yaml", nil
	})
	m, cmd := runColorSchemeLine(t, newTestModelEng(t, &fakeEngine{}, opts), "/color-scheme export dark")

	if !reflect.DeepEqual(asked, []string{"dark"}) {
		t.Fatalf("the exporter was asked for %v, want exactly the named scheme", asked)
	}
	if cmd != nil {
		t.Error("the export asked for a repaint; it changes no palette")
	}
	note := colorSchemeNote(t, m)
	for _, want := range []string{"/home/u/.apogee/schemes/dark.yaml", "/color-scheme dark"} {
		if !strings.Contains(note, want) {
			t.Errorf("the export note is missing %q:\n%s", want, note)
		}
	}
}

// An export that would overwrite work in progress is refused by the seam, and the refusal reaches
// the transcript as an ERROR — the file the human asked for is not there, and a note would read like
// it was.
func TestColorSchemeCommandSurfacesARefusedExport(t *testing.T) {
	const refusal = `apogee: color-scheme "/home/u/.apogee/schemes/dark.yaml" already exists — edit it or delete it first`
	opts := colorSchemeOpts(nil, []string{"dark"}, nil, func(string) (string, error) {
		return "", errors.New(refusal)
	})
	m, _ := runColorSchemeLine(t, newTestModelEng(t, &fakeEngine{}, opts), "/color-scheme export dark")

	last := lastEntry(t, m)
	if last.kind != entryError {
		t.Fatalf("the refused export wrote a %v entry, want an error: %q", last.kind, last.text)
	}
	if !strings.Contains(last.text, "already exists") || !strings.Contains(last.text, colorSchemeSource) {
		t.Errorf("the error entry = %q, want it to name the verb and the reason", last.text)
	}
}

// Without an export seam there is nowhere to write to, and saying so beats a silence that looks
// exactly like a successful export.
func TestColorSchemeCommandWithNoExportSeam(t *testing.T) {
	opts := colorSchemeOpts(nil, []string{"dark"}, nil, nil)
	m, _ := runColorSchemeLine(t, newTestModelEng(t, &fakeEngine{}, opts), "/color-scheme export dark")

	last := lastEntry(t, m)
	if last.kind != entryError || !strings.Contains(last.text, noSchemeExporterNote) {
		t.Errorf("the unwired export wrote %v %q, want an error carrying %q", last.kind, last.text, noSchemeExporterNote)
	}
}

// A line the parser could not read reports the usage and drives nothing — the /confine posture, and
// the reason it matters here: a guessed switch repaints the screen and writes the config.
func TestColorSchemeCommandReportsItsUsageOnABadLine(t *testing.T) {
	log := &settingsWriteLog{}
	opts := colorSchemeOpts(log, []string{"dark", "light"},
		func(string) (scheme.Scheme, []string) { return stubScheme("#123456"), nil }, nil)
	m, cmd := runColorSchemeLine(t, newTestModelEng(t, &fakeEngine{}, opts), "/color-scheme light please")

	if len(log.writes) != 0 {
		t.Errorf("writes = %+v, want none — a line that did not parse changes nothing", log.writes)
	}
	if cmd != nil {
		t.Error("a line that did not parse asked for a repaint")
	}
	if note := colorSchemeNote(t, m); !strings.Contains(note, colorSchemeUsage) {
		t.Errorf("note = %q, want it to carry %q", note, colorSchemeUsage)
	}
}
