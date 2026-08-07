package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/scheme"
)

// ----------------------------------------------------------------------------
// /color-scheme — routing the palette verb (command.go owns its grammar)
// ----------------------------------------------------------------------------
//
// The command half of ADR 0039's colour schemes: the settings pane offers the same switch through a
// picker, and this verb is the one that can be typed, scripted into a habit, and — with `export` —
// the only way to GET a scheme file to edit, since built-ins are embedded in the binary and never
// written to disk.
//
// Every form is synchronous and idle-safe like /confine: no upstream call, no worker, no engine
// seam. The switch writes one config key and rebuilds the theme; the export writes one file. The
// report builders below are pure, so the wording stays table-testable without a Model (the
// confine.go posture).

// colorSchemeSource labels the error entries this verb writes, the way an ErrorEvent's Source labels
// a faulted tool: an export that was refused is a failed ACT and reads as one, where a switch that
// merely loaded with complaints is a note plus its warnings.
const colorSchemeSource = "color-scheme"

// runColorScheme routes a parsed /color-scheme line from the idle state: list what can be switched
// to, switch to one, or export an editable copy of a built-in. Only the switch returns a Cmd (the
// repaint its new palette needs); the other two always return nil.
func (m Model) runColorScheme(args colorSchemeArgs) (tea.Model, tea.Cmd) {
	switch args.action {
	case colorSchemeSwitch:
		return m.switchColorScheme(args.name)
	case colorSchemeExport:
		return m.exportColorScheme(args.name)
	}
	m.transcript.addNote(colorSchemeListNote(m.availableSchemes(), m.currentSchemeName()))
	m.layout()
	return m, nil
}

// switchColorScheme persists the choice and puts it into effect, in that order — ADR 0037's
// validate → persist → apply, which is the same order and the same two seams the settings pane's
// picker uses ([Options.WriteSetting] then [Model.applyColorScheme]). Going through the pane's own
// path is the point: a scheme switched by command and one switched by picker must leave the file and
// the screen saying the same thing, and there is exactly one way to do each half.
//
// A refused write changes nothing at all — the screen keeps the palette the file still names — and
// says why. An apply that fails after the write LANDED does not unwind it (ADR 0037 decision 1): the
// file expresses the choice, the next start is drawn in it, and the note says so.
func (m Model) switchColorScheme(name string) (tea.Model, tea.Cmd) {
	if m.opts.WriteSetting == nil {
		m.transcript.addError(colorSchemeSource, noSettingsWriterNote, 0)
		m.layout()
		return m, nil
	}
	if err := m.opts.WriteSetting(settingKeyColorScheme, name); err != nil {
		m.transcript.addError(colorSchemeSource, err.Error(), 0)
		m.layout()
		return m, nil
	}
	// The same journal entry the pane records, so a key changed from the transcript wears the pane's
	// "changed this session" marker when the human next opens it (recordSettingEdit).
	m = m.recordSettingEdit(settingEdit{path: settingKeyColorScheme, value: name})
	warnings, cmd, err := m.applyColorScheme(name)
	if err != nil {
		m.transcript.addNote(settingsApplyFailedNote + err.Error())
		m.layout()
		return m, cmd
	}
	m.transcript.addNote(colorSchemeSwitchedNote(name, warnings))
	m.layout()
	return m, cmd
}

// exportColorScheme writes an editable copy of a built-in into the human's schemes folder through
// [Options.ExportScheme]. Success names the path written and the one-liner that loads it back;
// refusal — an unknown name, a file already there — is an error entry, because nothing happened and
// a note would read like something had.
func (m Model) exportColorScheme(name string) (tea.Model, tea.Cmd) {
	if m.opts.ExportScheme == nil {
		m.transcript.addError(colorSchemeSource, noSchemeExporterNote, 0)
		m.layout()
		return m, nil
	}
	path, err := m.opts.ExportScheme(name)
	if err != nil {
		m.transcript.addError(colorSchemeSource, err.Error(), 0)
		m.layout()
		return m, nil
	}
	m.transcript.addNote(colorSchemeExportedNote(name, path))
	m.layout()
	return m, nil
}

// noSchemeExporterNote is the nil-seam degrade of the one form that writes a FILE rather than a
// config key: without the seam there is nowhere to write to, because the folder is the binary's to
// resolve, and saying so beats a listing that quietly omits the copy the human asked for.
const noSchemeExporterNote = "this build cannot write scheme files"

// availableSchemes is what the session can switch to right now — the built-ins plus every file in
// the schemes folder, re-read on every ask ([Options.ListSchemes]). Nil seam ⇒ nothing is offered,
// which the listing reports rather than papering over with the built-in names: a build with no
// discovery wired cannot switch to them either.
func (m Model) availableSchemes() []string {
	if m.opts.ListSchemes == nil {
		return nil
	}
	return m.opts.ListSchemes()
}

// currentSchemeName is the scheme this session is drawn in, as it is spelled in the config. Empty ⇒
// unwired, and the answer is the built-in default — which is what an unset key resolves to anyway.
func (m Model) currentSchemeName() string {
	if m.opts.ColorSchemeName == "" {
		return scheme.DefaultName
	}
	return m.opts.ColorSchemeName
}

// ----------------------------------------------------------------------------
// The rendered notes (pure)
// ----------------------------------------------------------------------------

// colorSchemeListNote renders the bare verb: one row per scheme that can be loaded, the one in force
// marked, and the two lines that say what to type next. names arrives sorted and de-duplicated
// (scheme.Discover), a user file shadowing a built-in of the same name having collapsed into a
// single row there — which is why nothing here says which of the two a row came from: the human
// cannot pick between them, so naming the distinction would only offer a choice that does not exist.
//
// A configured name that is not among them is reported on its own line rather than silently
// unmarked: the screen is then drawn in the default while the file names something else, and that
// gap is exactly what the human opened the listing to understand.
func colorSchemeListNote(names []string, current string) string {
	if len(names) == 0 {
		return "/color-scheme — no schemes are on offer (this build wired no scheme discovery)"
	}
	lines := []string{"/color-scheme — the schemes this session can switch to"}
	for _, name := range names {
		row := "  " + name
		if name == current {
			row += "  (current)"
		}
		lines = append(lines, row)
	}
	if !slices.Contains(names, current) {
		lines = append(lines, fmt.Sprintf("  the config names %q, which is not among them — the default is in force", current))
	}
	return strings.Join(lines, "\n") + "\n" +
		"  /color-scheme <name> switches; /color-scheme export <name> writes an editable copy"
}

// colorSchemeSwitchedNote confirms a switch that landed, and says whether the palette it loaded
// arrived clean. warnings is [colorSchemeWarningNote]'s sentence ("" for the ordinary switch); the
// warnings THEMSELVES are already in the transcript immediately above, one ephemeral note each
// (applyColorScheme), so this only points at them rather than repeating them.
func colorSchemeSwitchedNote(name, warnings string) string {
	line := fmt.Sprintf("color-scheme %q — saved and in force", name)
	if warnings != "" {
		line += "\n  " + warnings + ", listed above"
	}
	return line
}

// colorSchemeExportedNote confirms an export by naming the file it wrote and the command that loads
// it, because the copy is inert until one of the two happens: the file is edited, or it is switched
// to. Switching to it is spelled with the SAME name that was exported — a user file shadows the
// built-in it was copied from (ADR 0039 design call 6), so the copy is what that name means from
// here on.
func colorSchemeExportedNote(name, path string) string {
	return fmt.Sprintf("color-scheme %q written to %s", name, path) + "\n" +
		fmt.Sprintf("  edit it, then /color-scheme %s to load your copy — it shadows the built-in", name)
}
