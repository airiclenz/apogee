package main

// The Model profile seam of the composition root (ADR 0044).
//
// How apogee equips and speaks to a model — its tool-call format, its inline thinking channel and its
// tool roster — is a PER-MODEL fact resolved out of two tiers: the user's `model-profiles:` pattern
// map and the shipped shape table. Each axis is resolved on its own against those tiers (ADR 0057),
// so a user entry answers the axes it spells and leaves the rest to the table. The engine never reads
// either (ADR 0031: it is handed values, never config), so the resolution happens here, at every
// point the binary learns which model is bound: startup, the rebind an observed model change drives
// (ADR 0024), a scheduled Firing, and a `model-profiles:` edit committed while the session runs
// (ADR 0037).
//
// It lives in a file of its own for what it is: one small resolution with notices of its own,
// reached from several wiring points and testable without a session. The two points that also
// need the model's system-prompt template beside the profile — startup and the rebind — read the
// pair through resolveModelBindings, so the binary spells that resolution exactly once.

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/profiles"
)

// modelBindings is what resolving ONE model name yields in this binary: the system-prompt
// template selected for it (ADR 0023) and the Model profile it speaks the wire in (ADR 0044),
// with the notices that resolution says out loud — the built-in shape match and the roster deltas
// the profile brings (ADR 0057 decision 8) — in the order they are spoken. The two travel together
// because they are the same kind of fact, a per-model binding, and every point the binary learns
// which model it is bound to needs both at once: startup and the rebind an observed model change
// drives read them through resolveModelBindings, so the binary spells the pair exactly once.
type modelBindings struct {
	SystemPrompt string
	Profile      apogee.ModelProfile
	Notices      []string
}

// resolveModelBindings resolves the per-model pair for model out of opts — its `system-prompt*`
// keys against home for the template, its `model-profiles:` map over the shipped shape table for
// the profile. A template that cannot be read, or one carrying an unknown placeholder, is returned
// as the error it is: the prompt is structural configuration, and the two callers fail their step
// on it rather than degrade quietly around it. A cold start names no model at all, which selects
// the global template and the zero profile, silently; the first beat's rebind re-runs exactly this
// call with the model the server reports.
func resolveModelBindings(opts config.Options, home, model string) (modelBindings, error) {
	sysPrompt, err := config.ResolveSystemPrompt(opts.SystemPrompt, model, home, opts.UseDefaultPrompt, os.ReadFile)
	if err != nil {
		return modelBindings{}, err
	}
	bindings := modelBindings{SystemPrompt: sysPrompt}

	// The shape the model speaks the wire in. A built-in match announces itself, because to the
	// human this is one kind of fact: something apogee decided about this model that nobody typed.
	profile, notice := resolveModelProfile(model, opts.ModelProfiles)
	bindings.Profile = profile
	if notice != "" {
		bindings.Notices = append(bindings.Notices, notice)
	}

	// And the profile's THIRD axis, which is the one axis a human can otherwise only infer from a
	// tool that stopped being offered: a model whose roster deltas are non-empty says so in one
	// line, on the channel the shape above already travels. Silent when the matched entry spells no
	// `tools:` axis, which is every profile that predates the axis.
	if notice := rosterDeltaNotice(profile.Tools); notice != "" {
		bindings.Notices = append(bindings.Notices, notice)
	}
	return bindings, nil
}

// resolveModelProfile picks the Model profile for model and returns it with the one-line notice the
// resolution deserves. user is the resolved `model-profiles:` map; the shipped shape table is this
// build's own and needs no caller to supply it.
//
// A model no entry describes on a given axis takes that axis's ZERO — native tool calls, no inline
// thinking, no roster deltas — which is the pass-through every model had before the table existed, so
// a name nothing knows behaves exactly as it did.
func resolveModelProfile(model string, user []profiles.Entry) (apogee.ModelProfile, string) {
	decision := profiles.Resolve(model, user, profiles.Shipped())
	return decision.Profile, modelProfileNotice(decision)
}

// modelProfileNotice is the sentence a resolution says out loud, and the empty string when it says
// nothing. Only the SHIPPED tier speaks (ADR 0044 ratified call 7): apogee applied a shape the human
// never asked for, so a wrong match needs a first debugging clue. That now includes a resolution the
// user's own entry took part in — a tools-only entry over a shipped gpt-oss still gets the table's
// harmony parsing, and the line naming it is the same first clue. The user's tier is silent when it
// answered everything the table had to say, and no match at all is silent because nothing happened.
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

// minusSign is U+2212, the typographic minus a removed tool is announced with below. It is spelled
// as an escape rather than as a literal because it is deliberately NOT the ASCII hyphen sitting
// next to it in the same line, and nothing but the escape makes that visible in the source.
const minusSign = "\u2212"

// rosterDeltaNotice is the one line a model switch says when the model it lands on brings roster
// deltas with it (ADR 0057 decision 8) — `tools: +web_search −single_find_and_replace (profile)` —
// and the empty string when it brings none, which is every entry that spells no `tools:` axis and
// every axis whose lists resolve empty. Its reason is ADR 0044 ratified call 7's, applied to the
// third axis: announce what changes observable behavior, because a tool that silently vanished from
// the menu at a switch is otherwise a mystery with no trail.
//
// It speaks for the PROFILE rung alone, and says so in the trailing `(profile)`: the global
// `tools.disabled:`/`tools.enabled:` lists are a standing fact of the configuration that no switch
// moved, and they already had their say at load time. Additions come before removals and each half
// is sorted, so one entry renders one line on every run and two models' lines compare at a glance.
//
// A name the entry writes in BOTH directions renders once, as a removal — the ladder's own verdict
// that disabled wins a same-scope clash (tools.EffectiveRoster) — because this line describes the
// roster the session actually gets rather than the two lists it was spelled from. The clash itself
// is reported at load time, where the config key that has to be fixed is still in hand.
func rosterDeltaNotice(roster domain.ToolRosterDelta) string {
	off := toolNameSet(roster.Disabled)
	on := toolNameSet(roster.Enabled)
	for name := range off {
		delete(on, name)
	}
	if len(on) == 0 && len(off) == 0 {
		return ""
	}
	parts := make([]string, 0, len(on)+len(off))
	for _, name := range slices.Sorted(maps.Keys(on)) {
		parts = append(parts, "+"+name)
	}
	for _, name := range slices.Sorted(maps.Keys(off)) {
		parts = append(parts, minusSign+name)
	}
	return fmt.Sprintf("tools: %s (profile)", strings.Join(parts, " "))
}

// toolNameSet is the set of tools one roster list actually names: trimmed exactly as the ladder
// trims them (internal/tools) so a stray space around a name in a YAML sequence is the same tool in
// the notice as on the menu, with blank entries dropped and a repeated name folded to one.
func toolNameSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			set[name] = true
		}
	}
	return set
}
