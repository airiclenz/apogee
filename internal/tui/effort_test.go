package tui

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// ----------------------------------------------------------------------------
// The footer's effort word (pure)
// ----------------------------------------------------------------------------

// The footer states the level the NEXT request will actually carry, and it resolves it in the one
// order ADR 0060 fixed: session override ▸ profile ▸ the server's own reported default ▸ "auto".
// The bottom rung is not a level at all — a /props sighting proves the dial exists and names
// neither a vocabulary nor a default, so the honest word is that the model decides.
//
// Unsupported is the one case that answers with no word at all: the segment must be present exactly
// when /effort is, so the footer and the command menu can never disagree about whether this model
// has a dial.
func TestFooterEffortLabelResolvesTheLayers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		override        domain.ThinkingEffort
		profile         domain.ThinkingEffort
		reportedDefault string
		supported       bool
		want            string
		wantShow        bool
	}{
		{
			name:            "the session override outranks both layers under it",
			override:        domain.EffortHigh,
			profile:         domain.EffortLow,
			reportedDefault: "medium",
			supported:       true,
			want:            "high",
			wantShow:        true,
		},
		{
			name:            "the profile's own setting stands when no override does",
			profile:         domain.EffortLow,
			reportedDefault: "medium",
			supported:       true,
			want:            "low",
			wantShow:        true,
		},
		{
			name:            "the server's reported default stands under both session layers",
			reportedDefault: "medium",
			supported:       true,
			want:            "medium",
			wantShow:        true,
		},
		{
			name:      "a supported dial nothing has named a level for reads auto",
			supported: true,
			want:      effortAutoLabel,
			wantShow:  true,
		},
		{
			name:            "an unsupported dial has no word, whatever the layers say",
			override:        domain.EffortHigh,
			profile:         domain.EffortLow,
			reportedDefault: "medium",
			want:            "",
			wantShow:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, show := footerEffortLabel(tt.override, tt.profile, tt.reportedDefault, tt.supported)

			if got != tt.want || show != tt.wantShow {
				t.Errorf("footerEffortLabel(%q, %q, %q, %t) = (%q, %t), want (%q, %t)",
					tt.override, tt.profile, tt.reportedDefault, tt.supported, got, show, tt.want, tt.wantShow)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// The footer segment (composed)
// ----------------------------------------------------------------------------

// The word lands in the run BETWEEN the model and the workdir — the line reads outward-in, and how
// hard the model is asked to think belongs on the upstream side of it. A model whose server
// advertises no dial contributes no word: the segment leaves with its separator, and the rest of
// the run closes over the gap exactly as it does for any segment nothing has named.
func TestFooterShowsTheEffortSegmentOnlyWhenDialled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		effort  provider.EffortSupport
		wantRun string
	}{
		{
			name: "a dialled model states its resolved effort between model and workdir",
			effort: provider.EffortSupport{
				Supported: true,
				Dialect:   provider.EffortDialectReasoning,
				Efforts:   []string{"low", "medium", "high"},
				Default:   "medium",
			},
			wantRun: "test-host ✦ test-model ✦ high ✦ /ws/proj",
		},
		{
			name:    "a model with no dial keeps the run it always had",
			wantRun: "test-host ✦ test-model ✦ /ws/proj",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := testOpts
			opts.Workspace = "/ws/proj"
			serverSeams(&opts).beat = (&fakeHeartbeat{}).beat
			m := newTestModelEng(t, &fakeEngine{effortProfile: domain.EffortHigh}, opts)
			beat := upBeat("test-model", 32768)
			beat.EffortSupport = tt.effort

			m = foldBeatMsg(t, m, beat)

			footer := ansiPattern.ReplaceAllString(m.footerContent(120), "")
			if !strings.Contains(footer, tt.wantRun) {
				t.Errorf("footer = %q, want the run %q", footer, tt.wantRun)
			}
		})
	}
}
