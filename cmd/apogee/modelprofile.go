package main

// The Model profile seam of the composition root (ADR 0044).
//
// Which shape a model speaks the wire in — its tool-call format and its inline thinking channel — is
// a PER-MODEL fact resolved out of two tiers: the user's `model-profiles:` pattern map and the
// shipped shape table. The engine never reads either (ADR 0031: it is handed values, never config),
// so the match happens here, at every point the binary learns which model is bound: startup, the
// rebind an observed model change drives (ADR 0024), a scheduled Firing, and a `model-profiles:` edit
// committed while the session runs (ADR 0037).
//
// It sits beside validatedsets.go for the same reason that file exists: one small resolution with a
// notice of its own, reached from several wiring points and testable without a session.

import (
	"fmt"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/profiles"
)

// resolveModelProfile picks the Model profile for model and returns it with the one-line notice the
// match deserves. user is the resolved `model-profiles:` map; the shipped shape table is this
// build's own and needs no caller to supply it.
//
// An unmatched model resolves to the ZERO profile — native tool calls, no inline thinking — which is
// the pass-through every model had before the table existed, so a name nothing knows behaves exactly
// as it did.
func resolveModelProfile(model string, user []profiles.Entry) (apogee.ModelProfile, string) {
	decision := profiles.Resolve(model, user, profiles.Shipped())
	return decision.Profile, modelProfileNotice(decision)
}

// modelProfileNotice is the sentence a resolution says out loud, and the empty string when it says
// nothing. Only a SHIPPED match speaks (ADR 0044 ratified call 7): apogee applied a shape the human
// never asked for, so a wrong match needs a first debugging clue. A user match is silent because the
// user wrote it, and no match at all is silent because nothing happened.
func modelProfileNotice(decision profiles.Decision) string {
	if decision.Source != profiles.SourceShipped {
		return ""
	}
	// A profile whose thinking axis is unset reads as "none" rather than as an empty word: the zero
	// ThinkingStyle IS ThinkingNone, and the notice is the only place a human sees the value.
	style := decision.Profile.Thinking.Style
	if style == "" {
		style = domain.ThinkingNone
	}
	return fmt.Sprintf("model profile: %s (built-in) — thinking: %s", decision.Entry.Pattern, style)
}
