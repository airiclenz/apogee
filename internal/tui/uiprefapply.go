package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// ui.* — applying a UI pref on this screen
// ----------------------------------------------------------------------------
//
// The one place a `ui.*` key goes into effect on the running renderer. By default a key lands
// through [domain.UIPrefs.Set] and nothing more: the frame paints from the Options' block, so the
// value on the block IS the apply. Only the keys whose value a widget holds a copy of, or that ask
// the program to do something next, carry a follow-up row (uiPrefWidgets). The set of keys applied
// here is exported (AppliedSettingKeys), so the binary's own guard reads it rather than keeping a
// mirror of it.
//
// It is also the one door a screen gesture's change to a `ui.*` key goes back to the config file
// through (persistUIPref): the write or the reset, the journal entry a landed one records, and the
// one transcript error a failed one speaks.

// uiPrefWidget is a `ui.*` key's follow-up to the Set: the widget the landed value has to be told
// to, and the note and Cmd that telling produces. tell runs AFTER the Set and reads the landed
// value off m.opts.UI, unless beforeSet says the widget goes first — the colour scheme, whose resolve
// can fail, and a Set ahead of it would leave the block naming a palette the screen is not drawn in.
type uiPrefWidget struct {
	beforeSet bool
	tell      func(m Model, value string) (Model, string, tea.Cmd, error)
}

// uiPrefWidgets are the follow-up rows: the `ui.*` keys whose apply has more to do than the Set.
// Every key this module applies and this table does not name — the scroll bar, the spinner and its
// colour, the stall threshold, the skill-suggestion band, the notify mode, the footer's rows and
// colours, the side panel's mode, width, sections and folds — is read where it is painted or used,
// so the Set is its whole apply, and a key whose value moves frame geometry (the scroll bar's gutter,
// the footer's row count, the side panel) is laid out again by Update's repaint tail from the frame
// key (doc.go, "an arm mutates"), never here.
var uiPrefWidgets = map[string]uiPrefWidget{
	domain.UIKeyGlyphs:            {tell: tellGlyphs},
	domain.UIKeyTaskListOpen:      {tell: tellTaskListOpen},
	domain.UIKeyToolsOpen:         {tell: tellToolsOpen},
	domain.UIKeyToolsFoldOver:     {tell: tellToolsFoldOver},
	domain.UIKeyToolsAutoCollapse: {tell: tellToolsAutoCollapse},
	domain.UIKeyPointerShapes:     {tell: tellPointerShapes},
	domain.UIKeyGitFetch:          {tell: tellGitFetch},
	domain.UIKeyColorScheme:       {beforeSet: true, tell: tellColorScheme},
}

// AppliedSettingKeys returns every settings key the renderer applies to itself and never routes out
// through [SettingsHost.Apply]: the whole `ui:` block, structured keys included, but `ui.inspector`
// — which the binary mirrors onto its own live Options for the next wire observer, the renderer's
// Inspector keeping its start-up state — plus the two renderer-owned keys outside the block,
// `auto-title` and `cursor-shape`. It hands back a fresh slice on every call.
func AppliedSettingKeys() []string {
	keys := []string{settingKeyAutoTitle}
	for _, key := range domain.AllUIKeys() {
		if appliesUIPrefOnScreen(key) {
			keys = append(keys, key)
		}
	}
	return append(keys, settingKeyCursorShape)
}

// appliesUIPrefOnScreen reports whether key is a `ui.*` key this module applies: one of
// [domain.AllUIKeys] other than `ui.inspector`.
func appliesUIPrefOnScreen(key string) bool {
	return key != domain.UIKeyInspector && slices.Contains(domain.AllUIKeys(), key)
}

// applyUIPref applies one `ui.*` key on this screen and reports whether the key was one of this
// module's: every other key — `ui.inspector` among them — comes back handled=false, untouched, for
// the binary's dispatcher. The value lands through [domain.UIPrefs.Set], the one parser the config
// file, the registry rows and this pane share (ADR 0043), so a spelling the file accepts is the
// spelling the pane accepts and a refusal is domain's own sentence. The binary validates before it
// writes, but the pane is not the only thing that puts a value in the file, so a value the block
// cannot read comes back as an apply error rather than being ignored; a refused Set has touched
// nothing.
//
// A key with a follow-up row is then told to its widget (uiPrefWidgets): the note is the row's own
// sentence about the apply (empty for a key that simply took effect), and the Cmd is what the apply
// needs the program to do next.
func (m Model) applyUIPref(key, value string) (Model, string, tea.Cmd, bool, error) {
	if !appliesUIPrefOnScreen(key) {
		return m, "", nil, false, nil
	}
	widget, tells := uiPrefWidgets[key]
	if tells && widget.beforeSet {
		applied, note, cmd, err := widget.tell(m, value)
		if err != nil {
			return applied, note, cmd, true, err
		}
		if err := applied.opts.UI.Set(key, value); err != nil {
			return applied, note, cmd, true, err
		}
		return applied, note, cmd, true, nil
	}
	if err := m.opts.UI.Set(key, value); err != nil {
		return m, "", nil, true, err
	}
	if !tells {
		return m, "", nil, true, nil
	}
	applied, note, cmd, err := widget.tell(m, value)
	return applied, note, cmd, true, err
}

// tellGlyphs swaps the drawn marks. Every memoised block paint holds the outgoing set's marks and its
// key does not name the set (paintcache.go), so the cache is cleared as a scheme switch clears it;
// the set is in the frame key, so the repaint tail lays the transcript out again in the new marks.
func tellGlyphs(m Model, _ string) (Model, string, tea.Cmd, error) {
	m.th.glyphs = glyphsFor(m.opts.UI.Glyphs)
	m.transcript.paints.clear()
	return m, "", nil, nil
}

// tellTaskListOpen lands the value on every task-list card at once: the same sweep a click on a card
// takes (toggleTaskListFold), which is what makes a `/settings` edit and a hand-edited file apply
// live — and a resumed session, whose cards were seeded from the file, paint per the file. The sweep
// bumps the transcript's generation, so the repaint tail lays out again for the cards' moved height.
func tellTaskListOpen(m Model, _ string) (Model, string, tea.Cmd, error) {
	m.transcript.setTaskListOpen(m.opts.UI.TaskListOpen)
	return m, "", nil, nil
}

// tellToolsOpen is the task list's shape of apply for the large Tools umbrellas: the value lands on
// every one at once through the setter a header toggle takes (toggleToolsFold), and the setter
// touches the transcript, so the repaint tail draws each umbrella afresh for its moved height.
func tellToolsOpen(m Model, _ string) (Model, string, tea.Cmd, error) {
	m.transcript.setToolsOpen(m.opts.UI.ToolsOpen)
	return m, "", nil, nil
}

// tellToolsFoldOver moves the line between small and large umbrellas. Which umbrellas are large is a
// paint fact, so the setter touches the transcript: an umbrella the new threshold moves across the
// line gains or loses its glyph on the next frame.
func tellToolsFoldOver(m Model, _ string) (Model, string, tea.Cmd, error) {
	m.transcript.setToolsFoldOver(m.opts.UI.ToolsFoldOver)
	return m, "", nil, nil
}

// tellToolsAutoCollapse moves a paint input (blockFoldBy), so the setter touches the transcript on a
// move: every settled, untouched tool block — umbrella (a group of one included), Sub-agent or Skill
// group — folds, or gets its own fold back, on the next frame.
func tellToolsAutoCollapse(m Model, _ string) (Model, string, tea.Cmd, error) {
	m.transcript.setToolsAutoCollapse(m.opts.UI.ToolsAutoCollapse)
	return m, "", nil, nil
}

// tellPointerShapes acts on half of the pointer-shape gate (pointerprobe.go); the terminal's answer
// is the other half. Switched on in a session that never asked, the query goes out now; one that
// already asked keeps its answer and sends nothing (ADR 0106 §3). Switched off, the shape the hover
// left in force is reset, so the terminal is not left showing a pointer nothing keeps up to date any
// more. Either way View reads the gate on the next frame, so the mouse mode follows the flag.
func tellPointerShapes(m Model, _ string) (Model, string, tea.Cmd, error) {
	if m.opts.UI.PointerShapes {
		asked, query := m.askPointerShapes()
		return asked, "", query, nil
	}
	forgot, written := m.forgetPointerShape()
	if written {
		return forgot, "", tea.Raw(ansi.SetPointerShape("")), nil
	}
	return forgot, "", nil, nil
}

// tellGitFetch acts on the flag the footer's git fetch chain reads (gitstatus.go). Switched on, the
// chain is armed here — its first fetch runs now — when the workspace has an upstream and no chain
// runs; switched off, the running chain retires at its next step, and a fetch already in flight
// finishes. Nothing on the screen moves.
func tellGitFetch(m Model, _ string) (Model, string, tea.Cmd, error) {
	fetch := m.armGitFetch()
	return m, "", fetch, nil
}

// tellColorScheme resolves the named scheme and draws in it (applyColorScheme) — run BEFORE the Set
// (beforeSet), because a resolve that fails, the ADR 0031 Driver degrade with no resolver wired,
// must leave UI.ColorScheme naming the palette the screen is still drawn in, which is what
// currentSchemeName (colorscheme.go) reports as in force.
func tellColorScheme(m Model, value string) (Model, string, tea.Cmd, error) {
	note, cmd, err := m.applyColorScheme(value)
	return m, note, cmd, err
}

// ----------------------------------------------------------------------------
// ui.* — persisting a gesture's change
// ----------------------------------------------------------------------------

// uiBlockPrefix opens every `ui.*` key; a failed write-back's transcript error names the key without
// it (`task-list-open: not saved: …`, `side-panel.width: not saved: …`), the key as it reads inside
// its block.
const uiBlockPrefix = "ui."

// uiPrefWrite is one gesture's write-back of a `ui.*` key: value written to key or, with reset, the
// key's line removed from the file rather than its default written into it (ADR 0035). For a key
// [uiPrefMerges] names, value is the gesture's one-section delta ([sidePanelDelta]), not the key's
// whole text.
type uiPrefWrite struct {
	key   string
	value string
	reset bool
}

// persistUIPref is the one door a screen gesture's change to a `ui.*` key is written back through —
// a fold of the task-list cards or the Tools umbrellas, a pin or fold in the side panel, the
// separator's drag or reset, a `/panel` line. The gesture has already moved the session, so the
// screen shows what the human asked for whatever the file then does.
//
// A landed write is journalled (recordSettingEdit, so the `/settings` row shows the value with its
// ` *` marker) and says nothing more: the moved screen is the whole report, and a `saved` line
// under every fold would be noise the reader learns to skip (ADR 0035 addendum). Only a write that
// FAILED speaks, as one transcript error naming the key (uiBlockPrefix), and the gesture's change
// stands for the session: the file simply did not keep it. A nil seam writes nothing — the Driver
// degrade (ADR 0031): a composition without a settings host still has the gesture, it just has
// nowhere to remember it. The binary's own seam refreshes the config watcher's baseline on every
// landed write (ADR 0041 decision 8), which keeps the write from coming back a second later as
// somebody else's edit.
//
// The write is synchronous, on the keypress — the [SettingsHost.Write] contract — and nothing here
// schedules anything, so the door returns the Model alone for the caller to repaint.
func (m Model) persistUIPref(write uiPrefWrite) Model {
	if m.opts.Settings == nil {
		return m
	}
	edit, err := m.writeUIPref(write)
	if err != nil {
		source := strings.TrimPrefix(write.key, uiBlockPrefix)
		m.transcript.addError(source, taskListNotSavedNote+err.Error(), runRef{})
		return m
	}
	return m.recordSettingEdit(edit)
}

// writeUIPref makes write's call on the settings host and answers the journal entry a landed one
// records: the text written; for a reset, the key's documented default ([domain.UIDefaultText]),
// the text its `/settings` row then reads; and for a merged list ([uiPrefMerges]), the whole list
// the files now resolve to, which is first put in force through the key's own parser
// ([domain.UIPrefs.Set]). A list text that parser refuses is reported like a refused write, and a
// refused Set has touched nothing. It is a pointer method because that Set lands on the Model its
// caller returns.
func (m *Model) writeUIPref(write uiPrefWrite) (settingEdit, error) {
	if write.reset {
		if err := m.opts.Settings.Reset(write.key); err != nil {
			return settingEdit{}, err
		}
		return settingEdit{path: write.key, value: domain.UIDefaultText(write.key), reset: true}, nil
	}
	landed, err := m.opts.Settings.Write(write.key, write.value)
	if err != nil {
		return settingEdit{}, err
	}
	if !uiPrefMerges(write.key) {
		return settingEdit{path: write.key, value: write.value}, nil
	}
	if err := m.opts.UI.Set(write.key, landed); err != nil {
		return settingEdit{}, err
	}
	return settingEdit{path: write.key, value: landed}, nil
}

// uiPrefMerges reports whether a gesture writes key as a one-section delta the binary merges into
// the file's own list under its edit lock ([SettingsHost.Write]) — the side panel's two lists, so a
// hand-kept list keeps what it says of every other section. What lands is then the whole list, not
// the text the gesture handed over.
func uiPrefMerges(key string) bool {
	return key == domain.UIKeySidePanelSections || key == domain.UIKeySidePanelFolded
}
