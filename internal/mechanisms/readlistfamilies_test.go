package mechanisms

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/library"
)

// The test below pins the last surviving F8 gap fix: libraryListTools was hand-maintained short of
// the complete five-spelling list family, so it composes from listSpellings and now carries the
// previously-missing apogee spelling. It exercises the newly-covered spelling through the
// mechanism's observable behaviour and FAILS if that set drops it (verified by reverting while
// writing). The spellings the set already carried have their own coverage elsewhere (the camelCase
// listFiles / readFile tests), so the test uses only the spellings its gap fix adds. Its two
// siblings — the filehint and toolfilter list-family pins — retired with their rows in v0.20.0.

// The library shallow-exploration observation fires on apogee's camelCase list spellings (listFiles /
// listDir): a listing with no read on an analysis request records the behavioural pattern. Pins the gap
// fix adding listFiles / listDir to libraryListTools via the list family (list_files / list_directory /
// list_dir were already covered).
func TestLibraryObserveShallowExplorationOnCamelCaseList(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"listFiles", "listDir"} {
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			st := closeOnCleanup(t, library.NewStore(t.TempDir()))
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
