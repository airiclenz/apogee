package domain_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// stubTool is a minimal Tool implementation for exercising the registry. It carries
// no behaviour beyond its name — the registry keys only on Name.
type stubTool struct{ name string }

func (s stubTool) Name() string            { return s.name }
func (s stubTool) Description() string     { return "stub" }
func (s stubTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s stubTool) Execute(context.Context, domain.ToolCall) (domain.ToolResult, error) {
	return domain.ToolResult{}, nil
}

func TestToolRegistry_Register_RejectsDuplicateName(t *testing.T) {
	t.Parallel()

	registry := domain.NewToolRegistry()

	if err := registry.Register(stubTool{name: "read_file"}); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}

	err := registry.Register(stubTool{name: "read_file"})

	if !errors.Is(err, domain.ErrDuplicateTool) {
		t.Fatalf("duplicate Register err = %v, want wrapped ErrDuplicateTool", err)
	}
}

func TestToolRegistry_Register_RejectsEmptyName(t *testing.T) {
	t.Parallel()

	registry := domain.NewToolRegistry()

	err := registry.Register(stubTool{name: ""})

	if !errors.Is(err, domain.ErrInvalidTool) {
		t.Fatalf("empty-name Register err = %v, want wrapped ErrInvalidTool", err)
	}
}

func TestToolRegistry_Lookup_FindsRegisteredAndMissesUnknown(t *testing.T) {
	t.Parallel()

	registry := domain.NewToolRegistry()
	want := stubTool{name: "grep"}
	if err := registry.Register(want); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	got, ok := registry.Lookup("grep")
	if !ok {
		t.Fatalf("Lookup(grep) ok = false, want true")
	}
	if got.Name() != "grep" {
		t.Errorf("Lookup(grep).Name() = %q, want %q", got.Name(), "grep")
	}

	if _, ok := registry.Lookup("absent"); ok {
		t.Errorf("Lookup(absent) ok = true, want false")
	}
}

func TestToolRegistry_All_PreservesRegistrationOrder(t *testing.T) {
	t.Parallel()

	registry := domain.NewToolRegistry()
	for _, name := range []string{"read_file", "write_file", "list_dir", "grep"} {
		if err := registry.Register(stubTool{name: name}); err != nil {
			t.Fatalf("Register(%q) returned error: %v", name, err)
		}
	}

	got := toolNames(registry.All())

	want := []string{"read_file", "write_file", "list_dir", "grep"}
	if !equalStrings(got, want) {
		t.Errorf("All() order = %v, want %v", got, want)
	}
}

func TestToolRegistry_Subset_NarrowsToNamedToolsInOrder(t *testing.T) {
	t.Parallel()

	parent := domain.NewToolRegistry()
	for _, name := range []string{"read_file", "write_file", "list_dir", "grep"} {
		if err := parent.Register(stubTool{name: name}); err != nil {
			t.Fatalf("Register(%q) returned error: %v", name, err)
		}
	}

	sub := parent.Subset("grep", "read_file")

	got := toolNames(sub.All())
	want := []string{"grep", "read_file"}
	if !equalStrings(got, want) {
		t.Errorf("Subset names = %v, want %v", got, want)
	}
}

func TestToolRegistry_Subset_NeverASuperset(t *testing.T) {
	t.Parallel()

	parent := domain.NewToolRegistry()
	if err := parent.Register(stubTool{name: "read_file"}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	// Unknown names are skipped and a repeat collapses to one — the result can only
	// ever be a subset of the parent (ADR 0005).
	sub := parent.Subset("read_file", "read_file", "does_not_exist")

	got := toolNames(sub.All())
	want := []string{"read_file"}
	if !equalStrings(got, want) {
		t.Errorf("Subset names = %v, want %v", got, want)
	}
}

func toolNames(tools []domain.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name())
	}
	return names
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
