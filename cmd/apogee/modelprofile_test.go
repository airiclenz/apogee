package main

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/profiles"
	"github.com/airiclenz/apogee/internal/tools"
)

// The two tiers and the silence rule in one table (ADR 0044): a user entry outranks the shipped
// table and applies without a word, a shipped entry applies and says so, and a model neither tier
// knows runs the zero profile — the pass-through an unprofiled model has always had. Since ADR 0057
// the tiers meet AXIS BY AXIS, so the table also fixes who narrates when both had a word: the
// built-in speaks whenever it still supplied an axis the user's entry left unspelled.
func TestResolveModelProfileMatchesAndNarrates(t *testing.T) {
	t.Parallel()

	userMinimax := []profiles.Entry{{
		Pattern: "minimax",
		Profile: apogee.ModelProfile{Thinking: apogee.ThinkingProfile{
			Style: domain.ThinkingDelimited, Start: "<user>", End: "</user>",
		}},
	}}

	tests := []struct {
		name        string
		model       string
		user        []profiles.Entry
		wantStart   string
		wantStyle   domain.ThinkingStyle
		wantEnabled string
		wantNotice  string
	}{
		{
			// The live spelling that started this: the shipped table matches the family inside a
			// provider-prefixed, tag-suffixed id.
			name:  "a shipped entry matches the live minimax spelling and announces itself",
			model: "minimax/minimax-m3:exacto", wantStyle: domain.ThinkingDelimited,
			wantStart:  "<mm:think>",
			wantNotice: "model profile: minimax-m3 (built-in) — thinking: delimited",
		},
		{
			name:  "a user entry beats the shipped table and applies silently",
			model: "minimax/minimax-m3:exacto", user: userMinimax,
			wantStyle: domain.ThinkingDelimited, wantStart: "<user>",
		},
		{
			// The escape hatch for a wrong built-in match: an entry that SPELLS the off value. Under
			// axis-wise resolution the spelling is what does it — an entry that says nothing at all
			// says nothing about this axis either, and inherits the built-in (the case below).
			name:  "a user entry can turn a shipped match back off by spelling style: none",
			model: "minimax-m3", user: []profiles.Entry{{
				Pattern: "minimax-m3",
				Profile: apogee.ModelProfile{Thinking: apogee.ThinkingProfile{Style: domain.ThinkingNone}},
			}},
			wantStyle: domain.ThinkingNone,
		},
		{
			// The ratifying case for axis-wise resolution (ADR 0057 decision 5): a tools-only entry
			// for a model the table shapes keeps that shape instead of wiping it — and the built-in
			// still announces itself, because it still supplied the axis being announced.
			name:  "a tools-only user entry keeps the shipped shape, and the built-in still speaks",
			model: "gpt-oss-20b", user: []profiles.Entry{{
				Pattern:     "gpt-oss",
				Profile:     apogee.ModelProfile{Tools: domain.ToolRosterDelta{Enabled: []string{"web_search"}}},
				SpellsTools: true,
			}},
			wantStyle:   domain.ThinkingHarmony,
			wantEnabled: "web_search",
			wantNotice:  "model profile: gpt-oss (built-in) — thinking: harmony",
		},
		{
			name:  "a model neither tier knows runs the zero profile, silently",
			model: "some-unknown-14b", wantStyle: "",
		},
		{
			// The cold start: nothing is bound yet, so nothing can match and nothing is said. The
			// first beat's rebind re-runs the resolution against the model the server reports.
			name: "no model at all resolves to nothing and says nothing", model: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			profile, notice := resolveModelProfile(tt.model, tt.user)
			if profile.Thinking.Style != tt.wantStyle {
				t.Errorf("thinking style = %q, want %q", profile.Thinking.Style, tt.wantStyle)
			}
			if tt.wantStart != "" && profile.Thinking.Start != tt.wantStart {
				t.Errorf("thinking start = %q, want %q", profile.Thinking.Start, tt.wantStart)
			}
			if got := strings.Join(profile.Tools.Enabled, ","); got != tt.wantEnabled {
				t.Errorf("roster enabled = %q, want %q", got, tt.wantEnabled)
			}
			if notice != tt.wantNotice {
				t.Errorf("notice = %q, want %q", notice, tt.wantNotice)
			}
		})
	}
}

// The rebind seam is where a model SWITCH picks the shape up (ADR 0044 ratified call 6): the spec
// carries the profile, so Agent.Rebind installs it atomically with the prompt and the Mechanisms
// rather than leaving the session parsing the departed model's dialect. The notice rides the same
// per-session channel the validated-set lines do.
func TestRebindSpecForCarriesThePerModelProfile(t *testing.T) {
	t.Parallel()

	const builtIn = "model profile: minimax-m3 (built-in) — thinking: delimited"
	user := []profiles.Entry{{
		Pattern: "minimax-m3",
		Profile: apogee.ModelProfile{Thinking: apogee.ThinkingProfile{
			Style: domain.ThinkingDelimited, Start: "<user>", End: "</user>",
		}},
	}}

	tests := []struct {
		name       string
		model      string
		user       []profiles.Entry
		wantStart  string
		wantNotice string
	}{
		{
			name:  "the shipped table fills the spec and narrates the match",
			model: "minimax/minimax-m3:exacto", wantStart: "<mm:think>", wantNotice: builtIn,
		},
		{
			name:  "the user map fills the spec and narrates nothing",
			model: "minimax-m3-Q4_K_M", user: user, wantStart: "<user>",
		},
		{
			name: "an unmatched model leaves the spec's profile zero", model: "some-unknown-14b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			roots := stateRoots{config: t.TempDir(), validated: t.TempDir(), probe: t.TempDir()}
			opts := config.Options{ModelProfiles: tt.user}

			spec, notices, err := rebindSpecFor(opts, roots, nil, tt.model, 8192, 0, 0)
			if err != nil {
				t.Fatalf("rebindSpecFor: %v", err)
			}
			if spec.Profile.Thinking.Start != tt.wantStart {
				t.Errorf("RebindSpec.Profile start = %q, want %q", spec.Profile.Thinking.Start, tt.wantStart)
			}
			got := ""
			for _, n := range notices {
				if strings.HasPrefix(n, "model profile:") {
					got = n
				}
			}
			if got != tt.wantNotice {
				t.Errorf("profile notice = %q, want %q", got, tt.wantNotice)
			}
		})
	}
}

// rosterEditTools is a live tool set for the config-edit door's tests: built from the global
// `tools.disabled:` list it is handed and re-composed, on every rebuild, under the profile roster the
// spec carries — the two rungs the door moves between.
func rosterEditTools(workspace string, disabled []string) *liveTools {
	build := func(spec toolSetSpec) *apogee.ToolRegistry {
		return tools.NewDefaultRegistryWithHost(workspace,
			tools.HostTools{Disabled: spec.disabled, ProfileRoster: spec.roster})
	}
	spec := toolSetSpec{disabled: disabled}
	return newLiveTools(build(spec), spec, build)
}

// The config-EDIT door, the other half of ratified call 6: the model has not changed, so the map is
// re-read, resolved for the model the session is bound to right now, and pushed at SetProfile —
// never at a whole rebind, which an open Exchange would refuse.
func TestApplySettingModelProfilesResolvesForTheBoundModel(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	spy := &applySettingSpy{}
	live := newLiveSettings(config.Options{}, nil)
	apply := applySettingFor(settingsApplier{
		engine:     spy,
		live:       live,
		binding:    func() upstreamBinding { return upstreamBinding{Model: "minimax-m3"} },
		configPath: path,
		tools:      rosterEditTools(t.TempDir(), nil),
		// No rebind closure at all: this key must not need one.
	})

	writeSettingsFixture(t, path, "model-profiles:\n"+
		"  minimax:\n    thinking:\n      style: delimited\n      start: \"<edited>\"\n      end: \"</edited>\"\n")
	if _, err := apply("model-profiles", "1 model profile"); err != nil {
		t.Fatalf("apply model-profiles: %v", err)
	}
	if len(spy.profiles) != 1 || spy.profiles[0].Thinking.Start != "<edited>" {
		t.Fatalf("SetProfile = %+v, want one call carrying the re-read user entry", spy.profiles)
	}

	// And the map lands in the holder, so the NEXT model the session switches to is resolved against
	// the file as edited rather than as launched.
	base, _, _, _ := live.rebindInputs(config.Options{}, upstreamBinding{})
	if len(base.ModelProfiles) != 1 || base.ModelProfiles[0].Pattern != "minimax" {
		t.Errorf("rebindInputs carries %+v, want the re-read map", base.ModelProfiles)
	}

	// A map the human emptied hands the model back to the shipped table — apogee's own answer, not
	// whatever this process launched with.
	writeSettingsFixture(t, path, "mode: ask-before\n")
	if _, err := apply("model-profiles", "0 model profiles"); err != nil {
		t.Fatalf("apply the emptied map: %v", err)
	}
	if len(spy.profiles) != 2 || spy.profiles[1].Thinking.Start != "<mm:think>" {
		t.Errorf("SetProfile = %+v, want the shipped minimax shape back", spy.profiles)
	}
}

// The config-edit door carries the profile's THIRD axis too: an entry for the bound model that spells
// a roster re-composes the tool set through the set's own swap door (ADR 0057's Bounds — the host
// that built the set folds its deltas in), and only AFTER the dialect committed, so a profile the
// engine refuses moves the set no more than it moves the parser.
func TestApplySettingModelProfilesRecomposesTheToolSet(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	spy := &applySettingSpy{}
	live := rosterEditTools(t.TempDir(), []string{"view_diff"})
	apply := applySettingFor(settingsApplier{
		engine:     spy,
		live:       newLiveSettings(config.Options{}, nil),
		binding:    func() upstreamBinding { return upstreamBinding{Model: "minimax-m3"} },
		configPath: path,
		tools:      live,
	})

	writeSettingsFixture(t, path, "model-profiles:\n  minimax:\n    tools:\n      enabled: [view_diff]\n")
	if _, err := apply("model-profiles", "1 model profile"); err != nil {
		t.Fatalf("apply model-profiles: %v", err)
	}
	if len(spy.profiles) != 1 {
		t.Fatalf("SetProfile calls = %d, want 1", len(spy.profiles))
	}
	if len(spy.swaps) != 1 {
		t.Fatalf("SwapTools calls = %d, want 1: which tools exist is a set-level change", len(spy.swaps))
	}
	if _, ok := spy.swaps[0].Lookup("view_diff"); !ok {
		t.Error("the swapped-in set lacks the tool the bound model's entry lifts over the global list")
	}
	if want := []string{"view_diff"}; !slices.Equal(live.built().roster.Enabled, want) {
		t.Errorf("the holder's roster = %+v, want Enabled %q carried for the next rebuild", live.built().roster, want)
	}

	// Validate-then-commit at the parser AND at the set: a profile the engine refuses is reported
	// on the row and the tool set stays exactly where it was.
	spy.profileErr = errors.New("a dialect this build cannot parse")
	writeSettingsFixture(t, path, "model-profiles:\n  minimax:\n    tools:\n      enabled: [view_diff, python_exec]\n")
	if _, err := apply("model-profiles", "1 model profile"); err == nil {
		t.Fatal("a refused SetProfile must fail the apply")
	}
	if len(spy.swaps) != 1 {
		t.Errorf("SwapTools calls = %d, want still 1: a refused profile must not move the set", len(spy.swaps))
	}
	if want := []string{"view_diff"}; !slices.Equal(live.built().roster.Enabled, want) {
		t.Errorf("the holder's roster = %+v, want the committed %q untouched by the refused edit", live.built().roster, want)
	}
}

// With nothing bound — a cold start before the first beat, or the gap a `/server` switch opens —
// there is no model to resolve the map against, so the holder carries the edit alone and the engine
// hears nothing. rideTheRebind's own posture, for the same reason.
func TestApplySettingModelProfilesWithNothingBoundHoldsOnly(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	spy := &applySettingSpy{}
	live := newLiveSettings(config.Options{}, nil)
	apply := applySettingFor(settingsApplier{
		engine:     spy,
		live:       live,
		binding:    func() upstreamBinding { return upstreamBinding{} },
		configPath: path,
	})

	writeSettingsFixture(t, path, "model-profiles:\n  gemma:\n    thinking:\n      style: harmony\n")
	if _, err := apply("model-profiles", "1 model profile"); err != nil {
		t.Fatalf("apply model-profiles: %v", err)
	}
	if spy.drove() != 0 {
		t.Errorf("an unbound session drove the engine: %+v", spy.profiles)
	}
	base, _, _, _ := live.rebindInputs(config.Options{}, upstreamBinding{})
	if len(base.ModelProfiles) != 1 {
		t.Errorf("rebindInputs carries %+v, want the edit held for the first bind", base.ModelProfiles)
	}
}

// The switch line itself (ADR 0057 decision 8): one line for a non-empty roster axis, sorted with
// additions before removals so the same entry renders the same line on every run, and NOTHING at all
// for an entry that spells no tools axis or spells one that resolves empty. The golden strings are
// the point — this line is the only trail a vanished tool leaves.
func TestRosterDeltaNoticeAnnouncesOnlyRealDeltas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		roster domain.ToolRosterDelta
		want   string
	}{
		{
			name: "mixed deltas render additions first, each half sorted",
			roster: domain.ToolRosterDelta{
				Enabled:  []string{"web_search", "apply_patch"},
				Disabled: []string{"single_find_and_replace", "run_terminal_command"},
			},
			want: "tools: +apply_patch +web_search −run_terminal_command −single_find_and_replace (profile)",
		},
		{
			name:   "an additions-only axis says only what it added",
			roster: domain.ToolRosterDelta{Enabled: []string{"web_search"}},
			want:   "tools: +web_search (profile)",
		},
		{
			name:   "a removals-only axis says only what it took away",
			roster: domain.ToolRosterDelta{Disabled: []string{"web_search"}},
			want:   "tools: −web_search (profile)",
		},
		{
			// The zero axis — every profile that predates the roster, and every entry that spells
			// none. Silence is the whole contract: nothing about the menu moved.
			name: "an entry with no tools axis says nothing", roster: domain.ToolRosterDelta{},
		},
		{
			// An axis WRITTEN but empty — `tools: {disabled: [], enabled: []}`, or a sequence of
			// blanks a YAML editor left behind. It moves nothing either, so it says nothing.
			name: "an axis that resolves empty says nothing",
			roster: domain.ToolRosterDelta{
				Enabled: []string{}, Disabled: []string{"", "   "},
			},
		},
		{
			// Disabled wins a same-scope clash (tools.EffectiveRoster), so the line describes the
			// roster the session actually gets: one removal, never an addition contradicting it.
			// The clash itself was reported at load time, against the config key that has to change.
			name: "a name written in both directions renders once, as a removal",
			roster: domain.ToolRosterDelta{
				Enabled: []string{"web_search"}, Disabled: []string{"web_search"},
			},
			want: "tools: −web_search (profile)",
		},
		{
			// Names reach here from YAML sequences a human wrote: the ladder trims before it
			// compares, so a stray space is the same tool on the menu and in the line.
			name: "names are trimmed and folded exactly as the ladder compares them",
			roster: domain.ToolRosterDelta{
				Enabled: []string{" web_search ", "web_search"},
			},
			want: "tools: +web_search (profile)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := rosterDeltaNotice(tt.roster); got != tt.want {
				t.Errorf("roster notice = %q, want %q", got, tt.want)
			}
		})
	}
}

// And the seam that says it: a switch resolves the roster with the rest of the per-model binding, so
// the line rides the same notices slice the shape line does — one switch, one place to read what
// apogee decided. A model whose entry has no tools axis adds no line at all.
func TestRebindSpecForAnnouncesRosterDeltas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		model      string
		user       []profiles.Entry
		wantNotice string
	}{
		{
			name:  "a switch to a model with a tools axis announces the deltas",
			model: "big-model-70b",
			user: []profiles.Entry{{
				Pattern: "big-model",
				Profile: apogee.ModelProfile{Tools: domain.ToolRosterDelta{
					Enabled: []string{"web_search"}, Disabled: []string{"single_find_and_replace"},
				}},
				SpellsTools: true,
			}},
			wantNotice: "tools: +web_search −single_find_and_replace (profile)",
		},
		{
			name:  "a switch to a model whose entry spells no tools axis stays silent",
			model: "minimax-m3",
			user: []profiles.Entry{{
				Pattern: "minimax-m3",
				Profile: apogee.ModelProfile{Thinking: apogee.ThinkingProfile{
					Style: domain.ThinkingDelimited, Start: "<user>", End: "</user>",
				}},
			}},
		},
		{
			// The shipped roster, end to end (ADR 0059 §3): no user entry at all, and the switch
			// still announces the Console family, because the built-in table is what equipped this
			// model. The names are sorted, not written in table order — one entry, one line, every
			// run.
			name:       "a switch to a qwen3.8 model announces the shipped Console roster",
			model:      "Qwen3.8-27B-Instruct",
			wantNotice: "tools: +console_close +console_open +console_read +console_send (profile)",
		},
		{
			name: "a switch to a model no tier knows stays silent", model: "some-unknown-14b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			roots := stateRoots{config: t.TempDir(), validated: t.TempDir(), probe: t.TempDir()}
			opts := config.Options{ModelProfiles: tt.user}

			spec, notices, err := rebindSpecFor(opts, roots, nil, tt.model, 8192, 0, 0)
			if err != nil {
				t.Fatalf("rebindSpecFor: %v", err)
			}
			got := ""
			for _, n := range notices {
				if strings.HasPrefix(n, "tools:") {
					got = n
				}
			}
			if got != tt.wantNotice {
				t.Errorf("roster notice = %q, want %q", got, tt.wantNotice)
			}
			// The line and the binding are one fact: what the notice announces is what the spec
			// carries to Agent.Rebind, never a second, separately computed answer.
			if (len(spec.Profile.Tools.Enabled) > 0 || len(spec.Profile.Tools.Disabled) > 0) != (tt.wantNotice != "") {
				t.Errorf("spec roster = %+v but notice = %q — the line and the binding disagree",
					spec.Profile.Tools, got)
			}
		})
	}
}
