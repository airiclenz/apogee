package tools

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestToolSpecEmbedding pins the spec-embedding contract: a tool built from a toolSpec
// reports exactly the spec's name, description, and schema bytes through the three
// promoted metadata methods — first on a minimal probe embedding a fresh spec, then on
// a real built-in, proving the built-ins route their metadata through their spec value.
func TestToolSpecEmbedding(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","required":["x"],"properties":{"x":{"type":"string"}}}`)
	spec := toolSpec{name: "probe_tool", description: "A probe built from a spec.", schema: schema}

	probe := struct{ toolSpec }{toolSpec: spec}
	if got := probe.Name(); got != "probe_tool" {
		t.Errorf("Name() = %q, want %q", got, "probe_tool")
	}
	if got := probe.Description(); got != "A probe built from a spec." {
		t.Errorf("Description() = %q, want %q", got, "A probe built from a spec.")
	}
	if !bytes.Equal(probe.Schema(), schema) {
		t.Errorf("Schema() = %s, want %s", probe.Schema(), schema)
	}

	rf := NewReadFile(t.TempDir())
	if got := rf.Name(); got != readFileSpec.name {
		t.Errorf("ReadFile.Name() = %q, want the spec's %q", got, readFileSpec.name)
	}
	if got := rf.Description(); got != readFileSpec.description {
		t.Errorf("ReadFile.Description() = %q, want the spec's %q", got, readFileSpec.description)
	}
	if !bytes.Equal(rf.Schema(), readFileSpec.schema) {
		t.Errorf("ReadFile.Schema() = %s, want the spec's %s", rf.Schema(), readFileSpec.schema)
	}
}
