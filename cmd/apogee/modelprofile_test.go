package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/profiles"
)

// The two tiers and the silence rule in one table (ADR 0044): a user entry outranks the shipped
// table and applies without a word, a shipped entry applies and says so, and a model neither tier
// knows runs the zero profile — the pass-through an unprofiled model has always had.
func TestResolveModelProfileMatchesAndNarrates(t *testing.T) {
	t.Parallel()

	userMinimax := []profiles.Entry{{
		Pattern: "minimax",
		Profile: apogee.ModelProfile{Thinking: apogee.ThinkingProfile{
			Style: domain.ThinkingDelimited, Start: "<user>", End: "</user>",
		}},
	}}

	tests := []struct {
		name       string
		model      string
		user       []profiles.Entry
		wantStart  string
		wantStyle  domain.ThinkingStyle
		wantNotice string
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
			// The escape hatch for a wrong built-in match: an entry that spells the zero profile.
			name:  "a user entry can turn a shipped match back off",
			model: "minimax-m3", user: []profiles.Entry{{Pattern: "minimax-m3"}},
			wantStyle: "",
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
