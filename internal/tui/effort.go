package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// ----------------------------------------------------------------------------
// /effort — the thinking-effort dial's popup (picker.go paints it)
// ----------------------------------------------------------------------------
//
// The command is the session half of the Thinking-effort dial (ADR 0050, amended by ADR 0060): the
// profile says how hard a MODEL is asked to think, this says how hard THIS session is, and the
// override wins for as long as the session lasts. Nothing here is persisted — the config key is the
// durable door and /settings is not offering this one — so the note every accept ends on states the
// resolution rather than promising it will still hold tomorrow.
//
// The verb reads no arguments. It opens a fixed-choice popup over the levels the MODEL itself
// reports — the pickerCycle/pickerScheduleMode shape, a question that moves session state at its
// accept rather than re-pointing the session at anything (ADR 0060 D7) — and the level-word grammar
// it used to carry is deleted, parser and usage line with it: a half-removed parser is worse than
// either end state.
//
// Routing is synchronous and safe mid-Exchange (commandrun.go says why), and both the offering and
// the report builder are pure, so the rows and the wording are table-testable without a Model.

// noEffortDialNote is the whole answer a hand-typed /effort earns on a model detection saw no dial
// on. The dropdown withholds the row and the footer its segment (ADR 0060 D5), so nobody is INVITED
// to type it — but the registry and the parser stay complete underneath, and a verb that resolved
// fine and then did nothing at all would read as a broken command rather than as an absent dial.
const noEffortDialNote = "this model reports no thinking-effort dial"

// runEffortCommand drives the /effort verb: the popup when the bound model has a dial, one note when
// it has none. The gate is the same fact the menu and the footer read (Model.effortSupport), so the
// three can never disagree about whether this model has a dial — and it is asked HERE rather than at
// the accept, because a popup opened over a dial that does not exist would offer levels no request
// could carry.
func (m Model) runEffortCommand() (tea.Model, tea.Cmd) {
	if !m.effortSupport().Supported {
		return m.pickerNote(noEffortDialNote)
	}
	m.picker = picker{open: true, kind: pickerEffort}
	m.layout()
	return m, nil
}

// ----------------------------------------------------------------------------
// The offering
// ----------------------------------------------------------------------------

// canonicalEfforts is what the picker lists when the model's dial was seen but its vocabulary was
// not: a llama.cpp `/props` chat template proves the dial exists and states no level set at all, so
// the four levels ADR 0050 fixed are the honest offering — the ones every template that reads the
// kwarg has understood.
var canonicalEfforts = []domain.ThinkingEffort{
	domain.EffortOff, domain.EffortLow, domain.EffortMedium, domain.EffortHigh,
}

// openAIEfforts is that same fallback for the one dialect reached by configuration rather than by
// detection (`effort-dialect: openai`): OpenAI's own reasoning endpoints and Groq have no "off" rung
// — the model reasons either way — and offer a "minimal" below "low" instead, so offering "off"
// there would be offering a level the endpoint refuses.
var openAIEfforts = []domain.ThinkingEffort{
	domain.EffortMinimal, domain.EffortLow, domain.EffortMedium, domain.EffortHigh,
}

// effortLevels is the level vocabulary the picker offers for what detection saw: the model's OWN
// reported set when the server named one, and otherwise the canonical fallback its dialect implies
// (ADR 0060 D4). The reported set is taken as it stands — order included, unfiltered by this
// build's spelling union — because it is the model's own answer about itself, and a level apogee
// has no constant for is still the level that server asked to be called by; a level the template
// then rejects fails the turn with the enriched error naming `thinking.effort`, which was always
// the backstop.
//
// The "auto" row is NOT here: it is not a level at all but the absence of one (effortRows appends
// it), and everything above resolves the same way for the rows, the accept and any test that asks.
func effortLevels(support provider.EffortSupport) []domain.ThinkingEffort {
	if len(support.Efforts) == 0 {
		if support.Dialect == provider.EffortDialectOpenAI {
			return openAIEfforts
		}
		return canonicalEfforts
	}
	levels := make([]domain.ThinkingEffort, 0, len(support.Efforts))
	for _, reported := range support.Efforts {
		levels = append(levels, domain.ThinkingEffort(reported))
	}
	return levels
}

// effortRows is one row per level (effortLevels), with the "auto" row LAST — the row that clears the
// session override rather than setting one, and the only row of the pane that needs saying out loud,
// since a level names itself and "auto" could be read as a level the model offers. The levels are
// escape-stripped like every other popup cell that came off the wire (pickerOfferingRows' contract):
// a reported vocabulary is the SERVER's text.
func effortRows(support provider.EffortSupport) []popupRow {
	levels := effortLevels(support)
	rows := make([]popupRow, 0, len(levels)+1)
	for _, level := range levels {
		rows = append(rows, popupRow{stripEscapes(string(level))})
	}
	return append(rows, popupRow{effortAutoLabel, "— drop the override; the profile's setting stands"})
}

// ----------------------------------------------------------------------------
// The accept
// ----------------------------------------------------------------------------

// acceptEffort takes the effort picker's highlighted row, named by its index into the OFFERING
// (acceptPicker resolves the filter first), and turns it into the level the session asked for: a
// level row is itself, and the "auto" row appended past the last level is the zero value, which is
// what SetEffortOverride reads as "no override". The offering is re-derived rather than trusted
// from the frame that drew it, the acceptScheduleStop posture: a beat landing under the open pane
// can re-report this model's levels, and an index resolved against the older list would set a level
// the human never saw.
func (m Model) acceptEffort(offered int) (tea.Model, tea.Cmd) {
	levels := effortLevels(m.effortSupport())
	if offered < 0 || offered > len(levels) {
		m.picker = picker{}
		return m, nil
	}
	var level domain.ThinkingEffort // the auto row: absence, not a level
	if offered < len(levels) {
		level = levels[offered]
	}
	return m.runEffort(level)
}

// runEffort states this session's Thinking effort and says what that resolved to. A level layers a
// session override above the bound model profile's own `thinking.effort:`; the zero value clears
// that override, so the profile's setting stands again. Either way it closes on one note — "what is
// it now" is the whole of what the human asked by opening the pane — and it never launches a worker,
// so it always returns a nil Cmd.
//
// The write is unconditional, even where it re-asserts the live value: the human picked a state, not
// a transition, and the door is idempotent. The layers are then re-READ from the engine rather than
// assumed from the argument (/confine's `was` posture): the override is engine state, and a note
// that reported the request instead of the result would be a guess dressed as a fact.
func (m Model) runEffort(level domain.ThinkingEffort) (tea.Model, tea.Cmd) {
	m.picker = picker{}
	m.eng.SetEffortOverride(level)
	override, profile := m.eng.ThinkingEffort()
	m.transcript.addNote(effortResolutionNote(override, profile))
	m.layout()
	return m, nil
}

// ----------------------------------------------------------------------------
// The rendered note (pure)
// ----------------------------------------------------------------------------

// effortResolutionNote renders the line every /effort form ends on: the effort the next request will
// actually carry, then the two layers it was resolved from. Both layers are shown even when only one
// says anything, because the same level means something different depending on WHERE it sits — an
// override survives a model switch, a profile setting is replaced by one — and a single number could
// not tell the human which of those they are looking at.
func effortResolutionNote(override, profile domain.ThinkingEffort) string {
	return fmt.Sprintf("thinking effort: %s (session override: %s; profile: %s)",
		effortEffectiveLabel(override, profile), effortLayerLabel(override), effortLayerLabel(profile))
}

// effortEffectiveLabel names the effort the next request carries: the override when there is one,
// the profile's setting otherwise, and — when neither layer says anything — the plain fact that
// nothing is put on the wire at all and the model's own template decides (ADR 0050's wire anchor).
func effortEffectiveLabel(override, profile domain.ThinkingEffort) string {
	switch {
	case override != "":
		return string(override)
	case profile != "":
		return string(profile)
	default:
		return "the model's own default"
	}
}

// effortLayerLabel names one layer's setting, or the em dash standing for "this layer says nothing"
// — never an empty string, which would read as a missing value rather than an absent one.
func effortLayerLabel(e domain.ThinkingEffort) string {
	if e == "" {
		return "—"
	}
	return string(e)
}

// ----------------------------------------------------------------------------
// The footer segment (pure)
// ----------------------------------------------------------------------------

// effortAutoLabel is the footer's word for a dial that is supported but that nothing has named a
// level for. It is the honest reading of a /props sighting: the chat template proves the dial
// exists and states neither a vocabulary nor a default, so the level the next request carries is
// whatever the model itself decides — a word, not a level apogee ever asked for.
const effortAutoLabel = "auto"

// footerEffortLabel names the effort word the footer shows between the model and the workdir, and
// reports whether there is a segment to show at all. It is the same resolution the /effort note
// states (effortEffectiveLabel), one layer deeper: the SERVER's reported default stands under the
// two session layers, because a server that names its own default has told us the level the next
// request will actually carry even though apogee puts nothing on the wire for it (ADR 0031's wire
// anchor — the footer reads the outcome, it does not create one).
//
// The order is override ▸ profile ▸ reported default ▸ "auto" (ADR 0060). Unsupported yields the
// empty word and false together: the segment is present exactly when /effort is, so the footer and
// the command menu can never disagree about whether this model has a dial.
func footerEffortLabel(override, profile domain.ThinkingEffort, reportedDefault string, supported bool) (string, bool) {
	switch {
	case !supported:
		return "", false
	case override != "":
		return string(override), true
	case profile != "":
		return string(profile), true
	case reportedDefault != "":
		return reportedDefault, true
	default:
		return effortAutoLabel, true
	}
}
