package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// /effort — routing the thinking-effort verb (command.go owns its grammar)
// ----------------------------------------------------------------------------
//
// The command is the session half of the Thinking-effort dial (ADR 0050): the profile says how hard
// a MODEL is asked to think, this says how hard THIS session is, and the override wins for as long
// as the session lasts. Nothing here is persisted — the config key is the durable door and /settings
// is not offering this one — so every note below states the resolution rather than promising it will
// still hold tomorrow.
//
// Routing is synchronous and safe mid-Exchange (commandrun.go says why), and the report builder is
// pure so the wording is table-testable without a Model.

// runEffort routes a parsed /effort line. A level layers a session override above the bound model
// profile's own `thinking.effort:`; "auto" clears that override with the zero value, so the
// profile's setting stands again; a bare line moves nothing. Every form ends the same way — one note
// stating the resolution — because "what is it now" is the whole of what the report form asks and
// the only confirmation worth having in the other two. It never launches a worker, so it always
// returns a nil Cmd.
//
// The layers are re-READ from the engine after the write rather than assumed from the argument
// (/confine's `was` posture): the override is engine state, and a note that reported the request
// instead of the result would be a guess dressed as a fact.
func (m Model) runEffort(args effortArgs) (tea.Model, tea.Cmd) {
	if args.action != effortReport {
		// Asserted unconditionally, even when it is already the live value: the human asked for a
		// state, not a transition, and the door is idempotent.
		m.eng.SetEffortOverride(args.level)
	}
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
