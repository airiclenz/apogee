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
	srv, _ := batteryServer(t, s)
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
		"  model-profiles:",
		"    \"fake-model\":",
		"      tool-call-format: native",
		// The dated claim, in the spelling the reader gets — see
		// TestModelReportSpellsTheProbedAtTimeLocally for the zone itself.
		m.ProbedAt.Local().Format(time.RFC3339),
	} {
		if !strings.Contains(m.Report(), want) {
			t.Errorf("report does not state %q:\n%s", want, m.Report())
		}
	}
}

// The `probed at` line is the one instant this report prints, and it is spelled in the machine's
// OWN zone. The value behind it is stored UTC by construction (GatherModel stamps it, the record
// on disk keeps it), so leaving the display to inherit that zone would hand every reader outside
// UTC a clock that is not theirs — the same defect the scheduler's titles carried.
//
// The fixture is built from LOCAL's own offset — one instant, expressed in a zone 90 minutes off
// wherever the test runs — so it asserts the same thing on any machine's TZ, and the foreign
// spelling is rejected explicitly: dropping the conversion is a failure, not a coin flip.
func TestModelReportSpellsTheProbedAtTimeLocally(t *testing.T) {
	t.Parallel()
	m := gatherModel(t, script{nativeTools: true}, "fake-model")

	local := time.Date(2026, 7, 22, 23, 0, 0, 0, time.Local)
	_, offset := local.Zone()
	m.ProbedAt = local.In(time.FixedZone("away", offset+90*60))
	if m.ProbedAt.Format(time.RFC3339) == local.Format(time.RFC3339) {
		t.Fatalf("the fixture no longer distinguishes the zones: away %s, local %s", m.ProbedAt, local)
	}

	report := m.Report()
	if want := field("probed at", local.Format(time.RFC3339)); !strings.Contains(report, want) {
		t.Errorf("report does not state %q:\n%s", want, report)
	}
	if foreign := m.ProbedAt.Format(time.RFC3339); strings.Contains(report, foreign) {
		t.Errorf("report carries the foreign spelling %q:\n%s", foreign, report)
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
			name: "written",
			save: SaveOutcome{Requested: true, Written: true, Path: "/home/.apogee/probe/abc.json"},
			want: []string{
				"/home/.apogee/probe/abc.json",
				"yes — delete the file above to undo",
				"this record is the stored signature the next probe of this model compares against",
			},
		},
		{
			name: "--no-save",
			save: SaveOutcome{Requested: false, Path: "/p.json"},
			want: []string{"NO — --no-save was given", "none — with no record stored"},
		},
		{
			// --no-save while an earlier saved record survives on disk: claiming "no record
			// stored" would be false in exactly the drift-check scenario --no-save serves — the
			// surviving record is still what the next probe compares against.
			name: "--no-save with a surviving record",
			save: SaveOutcome{Requested: false, Path: "/p.json", Previous: "2026-01-02T03:04:05Z"},
			want: []string{
				"NO — --no-save was given",
				"none new — the record from 2026-01-02T03:04:05Z continues to apply; this run recorded nothing",
			},
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
		// The native probe never completed, so no trial was asked and no format is claimed.
		field("tool-call format", "native — the native probe never completed, so no tool-call format was tested"),
	} {
		if !strings.Contains(m.Report(), want) {
			t.Errorf("report does not state %q:\n%s", want, m.Report())
		}
	}
}

// The tool-call format line names the evidence behind the suggested format, and quotes the
// model's written call wherever no listed format was confirmed — that shape is the user's to
// write a custom-regex profile from. A native call needs no explaining and gets no line.
func TestModelReportToolCallFormat(t *testing.T) {
	t.Parallel()

	const noFit = "native — no listed format parses this model's call; the model wrote: " +
		`probe_echo(text="apogee")` +
		" — a custom-regex profile (tool-call-pattern:, tool-call-example:) written from that shape is yours to add"

	for _, tc := range []struct {
		name       string
		script     script
		wantLine   string // "" asserts that no tool-call format line is rendered
		wantFormat string
	}{
		{
			name:       "native call",
			script:     script{nativeTools: true, structured: true, chain: true},
			wantFormat: "native",
		},
		{
			name:       "salvageable JSON call",
			script:     script{salvageableTools: true, structured: true},
			wantLine:   "native — the model wrote its call as JSON in the reply, and the salvage guard runs that under native",
			wantFormat: "native",
		},
		{
			name:       "fenced trial parsed",
			script:     script{writtenCall: pythonicCall, structured: true, fencedTrial: fencedCall},
			wantLine:   "markdown-fenced — taught that format, the model wrote a probe_echo call it parses",
			wantFormat: "markdown-fenced",
		},
		{
			name:       "no listed format fits",
			script:     script{writtenCall: pythonicCall, structured: true, fencedTrial: pythonicCall},
			wantLine:   noFit,
			wantFormat: "native",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			report := gatherModel(t, tc.script, "fake-model").Report()

			if tc.wantLine == "" {
				if strings.Contains(report, "tool-call format:") {
					t.Errorf("a native call needs no tool-call format line:\n%s", report)
				}
			} else if want := field("tool-call format", tc.wantLine); !strings.Contains(report, want) {
				t.Errorf("report does not state %q:\n%s", want, report)
			}
			if want := "      tool-call-format: " + tc.wantFormat; !strings.Contains(report, want) {
				t.Errorf("report does not suggest %q:\n%s", want, report)
			}
		})
	}
}

// A trial request that never completed names its failure, confirms no format, and still quotes
// the call the model wrote in the native probe.
func TestModelReportToolCallFormatTrialFailed(t *testing.T) {
	t.Parallel()
	m := gatherModel(t, script{writtenCall: pythonicCall, structured: true, fencedTrialFail: true}, "fake-model")
	report := m.Report()

	for _, want := range []string{
		"  tool-call format: native — the markdown-fenced trial never completed (" + m.Battery.FencedTrial.Failure + ")",
		`, so no text format was confirmed; the model wrote: probe_echo(text="apogee")`,
		"      tool-call-format: native",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report does not state %q:\n%s", want, report)
		}
	}
}

// The quote is one line, cut on a rune boundary, and says so when the reply was empty.
func TestModelReportQuotesTheReply(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("é", quoteReplyLimit+5)
	for _, tc := range []struct {
		name, reply, want string
	}{
		{name: "empty", reply: " \n\t", want: "nothing (the reply was empty)"},
		{name: "multi-line", reply: "probe_echo(\n  text=\"apogee\"\n)", want: `probe_echo( text="apogee" )`},
		{name: "long", reply: long, want: strings.Repeat("é", quoteReplyLimit) + "…"},
	} {
		if got := quoteReply(tc.reply); got != tc.want {
			t.Errorf("%s: quoteReply = %q; want %q", tc.name, got, tc.want)
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
