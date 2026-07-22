package probe

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/provider"
)

// gatherModel runs the model half against a scripted Upstream at a fixed clock, so the report's
// dated claim is deterministic.
func gatherModel(t *testing.T, s script, label string) Model {
	t.Helper()
	srv := batteryServer(t, s)
	client := provider.NewClient(srv.URL, label)
	return GatherModel(context.Background(), ModelInputs{
		Endpoint: srv.URL,
		Model:    label,
		Chat: func(ctx context.Context, req provider.Request) (provider.RawResponse, error) {
			return client.Respond(ctx, req)
		},
		Now: func() time.Time { return time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC) },
	})
}

// The report states the evidence, the tier, the identity and the paste-ready profile — and it
// says out loud that the tier drives nothing, because a reported-signal-only field that reads
// like a setting is how a signal quietly becomes a behaviour.
func TestModelReportStatesTheFindings(t *testing.T) {
	t.Parallel()
	m := gatherModel(t, script{nativeTools: true, structured: true, chain: true, logprobs: true}, "fake-model")

	for _, want := range []string{
		"apogee probe — model battery",
		"capability battery v1",
		"native-tool-call",
		"structured-json",
		"multi-step-chain",
		"exposed — 3 candidate tokens",
		"full — a reported signal only; nothing in apogee adapts to it",
		"fake-model — unchanged; the probe raises its confidence, it does not rename it",
		"probe:1:tools+json+chain:lp-",
		"medium — a dated behavioral claim",
		"suggested model profile",
		"  model-profile:",
		"    tool-call-format: native",
		"2026-07-22T09:00:00Z",
	} {
		if !strings.Contains(m.Report(), want) {
			t.Errorf("report does not state %q:\n%s", want, m.Report())
		}
	}
}

// The record section is the consequence ADR 0021 §4 makes binding: what was written, where,
// what it now enables, and how to undo it. Each save outcome gets its own sentence.
func TestModelReportRecordSection(t *testing.T) {
	t.Parallel()
	base := gatherModel(t, script{nativeTools: true, structured: true, chain: true}, "fake-model")

	cases := []struct {
		name string
		save SaveOutcome
		want []string
	}{
		{
			name: "written with a matching validated set",
			save: SaveOutcome{Requested: true, Written: true, Path: "/home/.apogee/probe/abc.json", AutoApply: []string{"gemma-3n"}, Promoted: true},
			want: []string{
				"/home/.apogee/probe/abc.json",
				"yes — delete the file above to undo",
				"Validated set gemma-3n now AUTO-APPLIES",
			},
		},
		{
			name: "written with nothing matching",
			save: SaveOutcome{Requested: true, Written: true, Path: "/p.json"},
			want: []string{
				"now resolves at medium confidence",
				"a Validated set keyed fake-model would AUTO-APPLY",
				"No entry carries that key today.",
			},
		},
		{
			// The set was already applying through the user's own alias, so the record
			// promoted nothing — claiming it "was previously only offered" would be a
			// false statement about the reader's machine.
			name: "written where an alias already applied the set",
			save: SaveOutcome{Requested: true, Written: true, Path: "/p.json", AutoApply: []string{"gemma-3n"}},
			want: []string{"was already applying through your validated-sets alias"},
		},
		{
			// A session-level off-switch holds whatever the record says: the report names
			// it rather than announcing an effect the next startup will decline.
			name: "written while the surface is off",
			save: SaveOutcome{Requested: true, Written: true, Path: "/p.json", Suppressed: "Bypass suppresses the Validated-set surface entirely"},
			want: []string{"but Bypass suppresses the Validated-set surface entirely."},
		},
		{
			name: "--no-save",
			save: SaveOutcome{Requested: false, Path: "/p.json"},
			want: []string{"NO — --no-save was given", "none — with no record stored"},
		},
		{
			name: "write failed",
			save: SaveOutcome{Requested: true, Path: "/p.json", Failure: "permission denied"},
			want: []string{"the write failed: permission denied"},
		},
		{
			name: "the model behind the label changed",
			save: SaveOutcome{Requested: true, Written: true, Path: "/p.json", Changed: "2026-01-02T03:04:05Z"},
			want: []string{"the model behind this label changed since 2026-01-02T03:04:05Z"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := base
			m.Save = tc.save
			for _, want := range tc.want {
				if !strings.Contains(m.Report(), want) {
					t.Errorf("report does not state %q:\n%s", want, m.Report())
				}
			}
		})
	}
}

// An incomplete battery reports no identity and no record, and says why: the report must not
// let a reader mistake "we could not ask" for "the model cannot".
func TestModelReportIncompleteBattery(t *testing.T) {
	t.Parallel()
	m := gatherModel(t, script{fail: true}, "fake-model")

	for _, want := range []string{
		"??  — probe failed:",
		"none — the battery did not complete",
		"not signed — an incomplete run is not an observation",
		"no — an incomplete battery derives no identity to record",
	} {
		if !strings.Contains(m.Report(), want) {
			t.Errorf("report does not state %q:\n%s", want, m.Report())
		}
	}
}

// The preamble states both costs BEFORE the first call, and --no-save changes what it promises.
func TestModelPreambleStatesTheCost(t *testing.T) {
	t.Parallel()
	if got := ModelPreamble(false); !strings.Contains(got, "recording its behavioral fingerprint") || !strings.Contains(got, "--no-save") {
		t.Errorf("preamble must name the write and its off-switch: %q", got)
	}
	if got := ModelPreamble(true); !strings.Contains(got, "nothing will be written") {
		t.Errorf("the --no-save preamble must promise no write: %q", got)
	}
}
