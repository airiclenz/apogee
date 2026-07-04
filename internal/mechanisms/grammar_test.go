package mechanisms

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

func grammarTools() []domain.ToolDef {
	return []domain.ToolDef{
		{Name: "read_file", Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)},
		{Name: "write_file", Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}}}`)},
	}
}

func TestGrammarDescriptorAndOrdering(t *testing.T) {
	t.Parallel()
	m, err := newGrammar(Deps{})
	if err != nil {
		t.Fatalf("newGrammar: %v", err)
	}
	d := m.Descriptor()
	if d.ID != grammarID {
		t.Errorf("ID = %q, want %q", d.ID, grammarID)
	}
	if d.Capability != domain.CapProactiveNudge {
		t.Errorf("Capability = %q, want proactive-nudge", d.Capability)
	}
	if d.Suppression != domain.SuppressStrikesThree {
		t.Errorf("Suppression = %q, want strikes-3", d.Suppression)
	}
	if o := m.Ordering(); len(o.Before) != 0 || len(o.After) != 0 {
		t.Errorf("Ordering = %+v, want none (catalogue Table A)", o)
	}
	if _, ok := m.(domain.PreRequestHook); !ok {
		t.Error("grammar does not implement PreRequestHook")
	}
}

// Without the backend capability (the default Deps), grammar is inert even with tools present — the
// catalogue's "may no-op on all current apogee backends" posture (D3-gated off).
func TestGrammarNoOpsWithoutCapability(t *testing.T) {
	t.Parallel()
	m, err := newGrammar(Deps{}) // GrammarConstraint false
	if err != nil {
		t.Fatalf("newGrammar: %v", err)
	}
	req := shaperRequest([]domain.Message{{Role: domain.RoleUser, Content: "go"}}, grammarTools())
	before := req.Revision()
	if err := m.(domain.PreRequestHook).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() != before {
		t.Fatal("grammar fired without the backend capability; it must be inert")
	}
	if _, has := req.State().Extras[grammarResponseFormatKey]; has {
		t.Error("grammar set response_format without the backend capability")
	}
}

// With the injected capability and tools present, grammar sets a json_schema response_format
// enumerating the tool names (apogee-sim SchemaForTools + response_format wrapper).
func TestGrammarSetsResponseFormatWhenSupported(t *testing.T) {
	t.Parallel()
	m, err := newGrammar(Deps{GrammarConstraint: true})
	if err != nil {
		t.Fatalf("newGrammar: %v", err)
	}
	req := shaperRequest([]domain.Message{{Role: domain.RoleUser, Content: "go"}}, grammarTools())
	if err := m.(domain.PreRequestHook).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	raw, has := req.Extra(grammarResponseFormatKey)
	if !has {
		t.Fatal("grammar did not set response_format with the capability on")
	}
	s := string(raw)
	for _, want := range []string{"json_schema", "read_file", "write_file", "enum"} {
		if !strings.Contains(s, want) {
			t.Errorf("response_format missing %q:\n%s", want, s)
		}
	}
	// The wrapper is well-formed JSON.
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Errorf("response_format is not valid JSON: %v", err)
	}
}

// An existing response_format wins: grammar never overwrites one already set (apogee-sim proxy.go:635).
func TestGrammarRespectsExistingResponseFormat(t *testing.T) {
	t.Parallel()
	m, err := newGrammar(Deps{GrammarConstraint: true})
	if err != nil {
		t.Fatalf("newGrammar: %v", err)
	}
	req := shaperRequest([]domain.Message{{Role: domain.RoleUser, Content: "go"}}, grammarTools())
	existing := json.RawMessage(`{"type":"text"}`)
	req.SetExtra(grammarResponseFormatKey, existing)
	after := req.Revision()

	if err := m.(domain.PreRequestHook).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() != after {
		t.Fatal("grammar overwrote an existing response_format")
	}
	if got, _ := req.Extra(grammarResponseFormatKey); string(got) != string(existing) {
		t.Errorf("response_format changed: %s", got)
	}
}

// No tools ⇒ nothing to constrain, even with the capability on.
func TestGrammarNoOpsWithoutTools(t *testing.T) {
	t.Parallel()
	m, err := newGrammar(Deps{GrammarConstraint: true})
	if err != nil {
		t.Fatalf("newGrammar: %v", err)
	}
	req := shaperRequest([]domain.Message{{Role: domain.RoleUser, Content: "go"}}, nil)
	before := req.Revision()
	if err := m.(domain.PreRequestHook).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() != before {
		t.Fatal("grammar fired with no tools to constrain")
	}
}

func TestGrammarBuildsFromCatalogue(t *testing.T) {
	t.Parallel()
	m, err := Build(grammarID, Deps{})
	if err != nil {
		t.Fatalf("Build(%q): %v", grammarID, err)
	}
	if m.Descriptor().ID != grammarID {
		t.Errorf("built ID = %q, want %q", m.Descriptor().ID, grammarID)
	}
}
