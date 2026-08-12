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

	rf := NewReadFile(t.TempDir(), nil)
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

// TestCanonicalArgs pins the canonical spelling a decision made ABOUT a call is keyed on: one
// executed call always canonicalises to one byte sequence (key order, whitespace and a duplicated
// key are not new identities), and two calls the executor would run differently never share one
// (scalars keep their wire bytes, so nothing is rounded or replaced on the way through). Arguments
// the executor would reject are an error rather than a canonical form.
func TestCanonicalArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty arguments are the empty object", "", `{}`},
		{"whitespace-only arguments are the empty object", "  \n\t", `{}`},
		{"keys are sorted and whitespace dropped", `{ "b" : 2 , "a" : 1 }`, `{"a":1,"b":2}`},
		{"the other key order is the same call", `{"a":1,"b":2}`, `{"a":1,"b":2}`},
		{"a duplicated key collapses to the last, as the executor decodes it", `{"a":1,"a":2}`, `{"a":2}`},
		{"nested objects are sorted too", `{"o":{"z":1,"y":{"b":0,"a":0}}}`, `{"o":{"y":{"a":0,"b":0},"z":1}}`},
		{"array order is meaning and is kept", `[ {"b":1,"a":2}, 3 ]`, `[{"a":2,"b":1},3]`},
		{"a large integer keeps its wire digits", `{"n":10000000000000000001}`, `{"n":10000000000000000001}`},
		{"null and booleans survive", `{"a":null,"b":true}`, `{"a":null,"b":true}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := CanonicalArgs(json.RawMessage(tc.raw))
			if err != nil {
				t.Fatalf("CanonicalArgs(%q) returned err %v, want it canonicalised", tc.raw, err)
			}
			if string(got) != tc.want {
				t.Errorf("CanonicalArgs(%q) = %s, want %s", tc.raw, got, tc.want)
			}
		})
	}

	t.Run("arguments the executor would reject are an error", func(t *testing.T) {
		t.Parallel()
		for _, raw := range []string{`{"a":`, `{"a":1}trailing`, `not json`} {
			if got, err := CanonicalArgs(json.RawMessage(raw)); err == nil {
				t.Errorf("CanonicalArgs(%q) = %s with no error, want the decode failure the executor sees", raw, got)
			}
		}
	})
}
