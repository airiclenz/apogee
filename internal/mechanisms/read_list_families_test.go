package mechanisms

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/library"
)

// The four tests below pin the F8 gap fixes: each list set was hand-maintained short of the complete
// five-spelling list family, so it composes from listSpellings and now carries the previously-missing
// apogee spelling. Every test exercises the newly-covered spelling through the mechanism's observable
// behaviour and FAILS if that set drops the spelling (verified by reverting each while writing). The
// spellings the sets already carried have their own coverage elsewhere (the camelCase listFiles /
// readFile tests), so each test uses only the spelling its gap fix adds.

// list_directory keeps cot's read-only streak alive: a run of turns calling only list_directory is
// still exploration, so the stall nudge fires at the threshold. Pins the gap fix adding list_directory
// to cotReadOnlyTools via the list family — the other four list spellings were already counted, so
// only list_directory discriminates this addition.
func TestCotReadOnlyStreakCountsListDirectory(t *testing.T) {
	t.Parallel()
	msgs := append([]domain.Message{
		{Role: domain.RoleSystem, Content: "SYS"},
	}, readOnlyTurns(cotStallThreshold, "list_directory")...)
	msgs = append(msgs, domain.Message{Role: domain.RoleUser, Content: "now fix the failing test"})

	req := shaperRequest(msgs, cotMenu)
	if err := (stallNudgeMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if !hasSystemMarker(req, cotStallMarker) {
		t.Fatal("a list_directory-only read-only streak did not advance the stall window; cotReadOnlyTools must carry list_directory")
	}
}

// The library shallow-exploration observation fires on apogee's camelCase list spellings (listFiles /
// listDir): a listing with no read on an analysis request records the behavioural pattern. Pins the gap
// fix adding listFiles / listDir to libraryListTools via the list family (list_files / list_directory /
// list_dir were already covered).
func TestLibraryObserveShallowExplorationOnCamelCaseList(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"listFiles", "listDir"} {
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			st := library.NewStore(t.TempDir())
			m := newLibraryMech(st, libFP("sha256:m", domain.ConfidenceHigh))

			tools := []domain.ToolDef{{Name: tool}}
			history := []domain.Message{{Role: domain.RoleUser, Content: "summarize the code in this package"}}
			resp := observeResponse(history, tools, domain.ToolCall{ID: "c1", Tool: tool, Arguments: json.RawMessage(`{}`)})
			if _, err := m.PostResponse(context.Background(), resp); err != nil {
				t.Fatalf("PostResponse: %v", err)
			}

			all := st.All()
			if len(all) != 1 || !all[0].HasTag("shallow_exploration") {
				t.Errorf("a %s listing without a read should record shallow_exploration; libraryListTools must carry %s. got %+v", tool, tool, all)
			}
		})
	}
}

// A listDir listing opens a filehint opportunity just like list_dir does, so a mixed MCP menu still
// triggers. Pins the gap fix adding listDir to fileHintListTools via the list family (list_dir /
// list_files / list_directory were already covered; the camelCase listFiles has its own test).
func TestFileHintInjectsForListDir(t *testing.T) {
	t.Parallel()
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "fix the config in config.go"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "listDir"}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: "main.go\nconfig.go\nserver.go"},
	}
	req := shaperRequest(msgs, nil)
	before := req.Revision()

	if err := (fileHintMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() == before || !hasMarker(req) {
		t.Fatal("a listDir listing should open a hint opportunity; fileHintListTools must carry listDir")
	}
}

// An analysis-focused request keeps apogee's camelCase list spellings (listFiles / listDir) whole even
// at a zero keyword score, so a mixed MCP menu still exposes them. Pins the gap fix adding listFiles /
// listDir to toolFilterAnalysisKeep via the list family (list_dir / list_files / list_directory were
// already covered; readFile has its own test). Without analysis-keep the zero-scored tool, appended
// last to a 30-tool menu, falls outside the top-scored keep window and is trimmed.
func TestToolFilterKeepsCamelCaseListOnAnalysisIntent(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"listFiles", "listDir"} {
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			// "analyze" trips hasAnalysisIntent; none of analyze/project/architecture matches the tool's
			// name parts or description, so only the analysis-keep set can save it from the trim.
			tools := append(genericTools(30), domain.ToolDef{Name: tool, Description: "a generic tool"})
			msgs := []domain.Message{{Role: domain.RoleUser, Content: "analyze the project architecture"}}
			req := shaperRequest(msgs, tools)
			if err := (toolFilterMechanism{}).PreRequest(context.Background(), req); err != nil {
				t.Fatalf("PreRequest: %v", err)
			}
			if !nameSet(req.State().Tools)[tool] {
				t.Errorf("analysis-intent narrowing dropped %s; toolFilterAnalysisKeep must carry it", tool)
			}
		})
	}
}
