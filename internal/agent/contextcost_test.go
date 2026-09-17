package agent

import (
	"strings"
	"testing"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// contextCostConfig is menuConfig (one read-only tool, so the tool surface is non-empty) with a
// system prompt and one workspace context file — a seeded Agent whose every standing row that a
// top-level session can carry renders something.
func contextCostConfig(t *testing.T, sink domain.EventSink) domain.Config {
	t.Helper()
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "AGENTS.md", "# Guide\n\nworkspace conventions.\n")
	cfg := menuConfig(t, sink)
	cfg.WorkspaceDir = dir
	cfg.ContextFiles = []string{"AGENTS.md"}
	cfg.SystemPrompt = "You are a test agent."
	return cfg
}

// rowNames is the report's row names in order — what the wire-order assertions compare.
func rowNames(report domain.ContextCost) []string {
	names := make([]string, 0, len(report.Rows))
	for _, row := range report.Rows {
		names = append(names, row.Name)
	}
	return names
}

// rowBytes returns the named row's bytes and whether the row is present.
func rowBytes(report domain.ContextCost, name string) (int, bool) {
	for _, row := range report.Rows {
		if row.Name == name {
			return row.Bytes, true
		}
	}
	return 0, false
}

// TestContextCostRowsInWireOrder pins the shape of a seeded top-level session's report: the
// standing rows in standingBlocks order (a top-level session carries no delegate report and an
// empty task list, so neither has a row), then the native tool menu; every row's tokens are the
// Budget's own estimate over its bytes, the total is the sum of the rows and Calibrated is false
// before any request has been sent.
func TestContextCostRowsInWireOrder(t *testing.T) {
	t.Parallel()

	a := newProfileAgent(t, contextCostConfig(t, &recordingSink{}), echoResponder(t, "ok"))

	report := a.ContextCost()

	want := []string{"prompt", "orientation", "context files", contextCostToolMenu}
	if got := rowNames(report); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %v, want %v (standing blocks in wire order, then the tool menu)", got, want)
	}
	budget := a.budget()
	sum := 0
	for _, row := range report.Rows {
		if row.Bytes == 0 {
			t.Errorf("row %q has 0 bytes; an empty piece has no row", row.Name)
		}
		if row.Tokens != budget.EstimateTokens(row.Bytes) {
			t.Errorf("row %q tokens = %d, want the Budget's estimate %d", row.Name, row.Tokens, budget.EstimateTokens(row.Bytes))
		}
		sum += row.Bytes
	}
	if report.Bytes != sum {
		t.Errorf("Bytes = %d, want the sum of the rows %d", report.Bytes, sum)
	}
	if report.Tokens != budget.EstimateTokens(sum) {
		t.Errorf("Tokens = %d, want the estimate over the total %d", report.Tokens, budget.EstimateTokens(sum))
	}
	if report.Calibrated {
		t.Errorf("Calibrated = true before any request was sent")
	}
}

// TestContextCostStandingRowsMatchStandingSystem pins that the report is the seed's own
// arithmetic: the standing rows' bytes plus the blank-line joiners between them equal the length
// of the very string buildRequest seeds, and the row names are exactly the table's — a row added
// to standingBlocks would be counted here without a change.
func TestContextCostStandingRowsMatchStandingSystem(t *testing.T) {
	t.Parallel()

	a := newProfileAgent(t, contextCostConfig(t, &recordingSink{}), echoResponder(t, "ok"))

	report := a.ContextCost()

	standing := 0
	for _, row := range report.Rows {
		if row.Name == contextCostToolMenu || row.Name == contextCostToolInstructions {
			continue
		}
		if standing > 0 {
			standing += len("\n\n")
		}
		standing += row.Bytes
	}
	if seed := len(a.standingSystem()); standing != seed {
		t.Errorf("standing rows + joiners = %d bytes, want len(standingSystem()) = %d", standing, seed)
	}
}

// TestContextCostSeedsNothingWithoutConfiguredSource pins the ride-along rule on the report
// side: a workspace-only session with no prompt template and no context file seeds no system
// message at all, so the report carries no standing row — not even the orientation block, which
// would render on its own — while the tool surface stays.
func TestContextCostSeedsNothingWithoutConfiguredSource(t *testing.T) {
	t.Parallel()

	cfg := menuConfig(t, &recordingSink{})
	cfg.WorkspaceDir = t.TempDir()
	a := newProfileAgent(t, cfg, echoResponder(t, "ok"))
	if a.orientationBlock() == "" {
		t.Fatal("precondition: the orientation block renders for a workspace-only session")
	}

	report := a.ContextCost()

	if got := rowNames(report); strings.Join(got, ",") != contextCostToolMenu {
		t.Errorf("rows = %v, want the tool menu alone — no standing row rides along an unseeded message", got)
	}
}

// TestContextCostToolMenuMeasuresTheMenu pins the tool-menu row's measure: PromptChars over the
// mode-filtered menu the request carries — every ToolDef's name, description and schema.
func TestContextCostToolMenuMeasuresTheMenu(t *testing.T) {
	t.Parallel()

	a := newProfileAgent(t, contextCostConfig(t, &recordingSink{}), echoResponder(t, "ok"))

	report := a.ContextCost()

	menu := a.toolMenu()
	if len(menu) == 0 {
		t.Fatal("precondition: the menu is non-empty")
	}
	got, ok := rowBytes(report, contextCostToolMenu)
	if !ok {
		t.Fatalf("rows = %v, want a %q row", rowNames(report), contextCostToolMenu)
	}
	if want := domain.PromptChars(nil, menu); got != want {
		t.Errorf("tool menu bytes = %d, want PromptChars over the menu %d", got, want)
	}
}

// TestContextCostToolSurfaceIsOneRow pins the profile split the wire seam makes: a native profile
// sends the tools array, so the report carries `tool menu`; a non-native profile folds the
// rendered instruction block into the system channel and suppresses the array, so `tool
// instructions` replaces it — never both, and the instruction row measures the rendered block.
func TestContextCostToolSurfaceIsOneRow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		title   string
		profile domain.ModelProfile
		want    string
		absent  string
	}{
		{title: "native profile carries the tool menu", want: contextCostToolMenu, absent: contextCostToolInstructions},
		{
			title:   "non-native profile carries the tool instructions instead",
			profile: domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced},
			want:    contextCostToolInstructions,
			absent:  contextCostToolMenu,
		},
	}
	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			t.Parallel()

			cfg := contextCostConfig(t, &recordingSink{})
			cfg.Profile = tt.profile
			a := newProfileAgent(t, cfg, echoResponder(t, "ok"))

			report := a.ContextCost()

			if _, ok := rowBytes(report, tt.absent); ok {
				t.Errorf("rows = %v, carry %q; the tool surface is one row", rowNames(report), tt.absent)
			}
			got, ok := rowBytes(report, tt.want)
			if !ok {
				t.Fatalf("rows = %v, want a %q row", rowNames(report), tt.want)
			}
			if tt.want == contextCostToolInstructions {
				if want := len(a.toolInstructions(a.toolMenu())); got != want {
					t.Errorf("tool instructions bytes = %d, want the rendered block's %d", got, want)
				}
			}
		})
	}
}

// TestContextCostCalibratedAfterUsage pins Calibrated: false on a fresh Agent, true once one
// Turn's stream has carried the server's usage through the estimator's Calibrate.
func TestContextCostCalibratedAfterUsage(t *testing.T) {
	t.Parallel()

	cfg := contextCostConfig(t, &recordingSink{})
	a := newProfileAgent(t, cfg, scriptedResponder(t, usageScript("hello", stubllm.Usage{Prompt: 40, Completion: 7})))
	if a.ContextCost().Calibrated {
		t.Fatal("Calibrated = true before any request was sent")
	}

	stepOnce(t, a, "hi")

	report := a.ContextCost()
	if !report.Calibrated {
		t.Errorf("Calibrated = false after a Turn whose stream carried usage")
	}
	if ratio := a.budget().CharsPerToken; ratio == apogeectx.DefaultCharsPerToken {
		t.Errorf("CharsPerToken still the default %v; the report's estimate did not follow the calibration", ratio)
	}
}
