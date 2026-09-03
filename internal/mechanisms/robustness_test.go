package mechanisms

import (
	"encoding/json"

	"github.com/airiclenz/apogee/internal/domain"
)

// What the Wave-1 robustness tests left behind. Their subjects are gone — the tool-call validator
// became the tool-call-repair Floor guard (its tests moved to internal/floor), and syntax and
// autofix retired outright in v0.20.0 — so the descriptor, cascade-order and write-detection pins
// went with them. The two write-detection semantics they split on are one semantic now
// (isFileMutatingTool), pinned against the registered tool menu by
// TestWave4WriteToolsCoversEveryWorkspaceWritingBuiltin (writedetection_test.go).

// toolMenu is a small tool menu: read_file (no required params) and write_file (path + content
// required) — the surface the tool-call validation helpers read through LoopView.Tools().
func toolMenu() []domain.ToolDef {
	writeSchema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
	return []domain.ToolDef{
		{Name: "read_file"},
		{Name: "write_file", Schema: writeSchema},
	}
}
