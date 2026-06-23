package tools

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

func TestNewDefaultRegistry_HoldsTheBuiltInTools(t *testing.T) {
	t.Parallel()

	registry := NewDefaultRegistry(t.TempDir())

	for _, name := range []string{"read_file", "write_file", "list_dir", "grep"} {
		if _, ok := registry.Lookup(name); !ok {
			t.Errorf("default registry is missing %q", name)
		}
	}

	if got := len(registry.All()); got != 4 {
		t.Errorf("default registry holds %d tools, want 4", got)
	}
}

func TestNewDefaultRegistry_MenuOrderIsDeterministic(t *testing.T) {
	t.Parallel()

	registry := NewDefaultRegistry(t.TempDir())

	want := []string{"read_file", "write_file", "list_dir", "grep"}
	for i, tool := range registry.All() {
		if tool.Name() != want[i] {
			t.Errorf("tool %d = %q, want %q", i, tool.Name(), want[i])
		}
	}
}

func TestDefaultTools_DeclareReadOnlyNature(t *testing.T) {
	t.Parallel()

	want := map[string]bool{
		"read_file":  true,
		"list_dir":   true,
		"grep":       true,
		"write_file": false, // the lone write tool: must gate through Approval (P1.2)
	}

	for _, tool := range DefaultTools(t.TempDir()) {
		if got := domain.IsReadOnly(tool); got != want[tool.Name()] {
			t.Errorf("IsReadOnly(%q) = %v, want %v", tool.Name(), got, want[tool.Name()])
		}
	}
}
