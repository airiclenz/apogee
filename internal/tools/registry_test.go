package tools

import (
	"context"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

func TestNewDefaultRegistry_HoldsTheBuiltInTools(t *testing.T) {
	t.Parallel()

	registry := NewDefaultRegistry(t.TempDir())

	for _, name := range []string{
		"read_file", "write_file", "list_dir", "grep",
		"single_find_and_replace", "multi_find_and_replace", "edit_existing_file",
		"view_diff", "open_file",
		"terminal", "python_exec",
		"git_branch", "git_commit", "git_diff_range",
		"diagnostics",
		"web_fetch", "http_request", "web_search",
	} {
		if _, ok := registry.Lookup(name); !ok {
			t.Errorf("default registry is missing %q", name)
		}
	}

	// ask_user is omitted when no Asker is configured (NewDefaultRegistry uses a zero
	// HostTools), so the default set is 18 (15 base/exec/git/diag + 3 network).
	if got := len(registry.All()); got != 18 {
		t.Errorf("default registry holds %d tools, want 18", got)
	}
	if _, ok := registry.Lookup("ask_user"); ok {
		t.Error("ask_user must NOT be registered without an Asker")
	}
}

func TestNewDefaultRegistry_MenuOrderIsDeterministic(t *testing.T) {
	t.Parallel()

	registry := NewDefaultRegistry(t.TempDir())

	want := []string{
		"read_file", "write_file", "list_dir", "grep",
		"single_find_and_replace", "multi_find_and_replace", "edit_existing_file",
		"view_diff", "open_file",
		"terminal", "python_exec",
		"git_branch", "git_commit", "git_diff_range",
		"diagnostics",
		"web_fetch", "http_request", "web_search",
	}
	for i, tool := range registry.All() {
		if tool.Name() != want[i] {
			t.Errorf("tool %d = %q, want %q", i, tool.Name(), want[i])
		}
	}
}

func TestNewDefaultRegistryWithHost_RegistersAskUserOnlyWithAsker(t *testing.T) {
	t.Parallel()

	// No Asker ⇒ ask_user absent.
	if _, ok := NewDefaultRegistryWithHost(t.TempDir(), HostTools{}).Lookup("ask_user"); ok {
		t.Error("ask_user must be absent without an Asker")
	}

	// An Asker ⇒ ask_user present, appended after the network tools (last in the menu).
	reg := NewDefaultRegistryWithHost(t.TempDir(), HostTools{Asker: stubAsker{}})
	if _, ok := reg.Lookup("ask_user"); !ok {
		t.Fatal("ask_user must be present with an Asker")
	}
	all := reg.All()
	if got := all[len(all)-1].Name(); got != "ask_user" {
		t.Errorf("ask_user should be last in the menu, got last = %q", got)
	}
	if got := len(all); got != 19 {
		t.Errorf("registry with Asker holds %d tools, want 19", got)
	}
}

func TestDefaultTools_DeclareReadOnlyNature(t *testing.T) {
	t.Parallel()

	want := map[string]bool{
		"read_file":  true,
		"list_dir":   true,
		"grep":       true,
		"write_file": false, // the lone write tool: must gate through Approval (P1.2)
		// File-editing family (P3.7): writers gate, diff/open-file read.
		"single_find_and_replace": false,
		"multi_find_and_replace":  false,
		"edit_existing_file":      false,
		"view_diff":               true,
		"open_file":               true,
		// Execution tools (P3.8): write-capable subprocess tools — the loop confines/gates
		// them, so they must not declare read-only.
		"terminal":    false,
		"python_exec": false,
		// Git tools (P3.9): branch/commit mutate the repo (write-capable); diff-range is a
		// harmless read (read-only, runs in Plan).
		"git_branch":     false,
		"git_commit":     false,
		"git_diff_range": true,
		// Diagnostics (P3.10): inspects only — read-only, runs in Plan.
		"diagnostics": true,
		// Network tools (P3.11): external-effect, write-capable (the loop gates/auto-runs them
		// by effect kind, not read-only) — they must NOT declare read-only.
		"web_fetch":    false,
		"http_request": false,
		"web_search":   false,
		// ask_user (P3.11): asking a question writes nothing — read-only, runs in Plan.
		"ask_user": true,
	}

	for _, tool := range DefaultToolsWithHost(t.TempDir(), HostTools{Asker: stubAsker{}}) {
		if got := domain.IsReadOnly(tool); got != want[tool.Name()] {
			t.Errorf("IsReadOnly(%q) = %v, want %v", tool.Name(), got, want[tool.Name()])
		}
	}
}

// stubAsker is a no-op Asker for the registry tests (it is never called — the tests only
// check registration/ordering). ask_user's round-trip behaviour is covered in ask_user_test.go.
type stubAsker struct{}

func (stubAsker) Ask(context.Context, domain.AskRequest) (domain.AskAnswer, error) {
	return domain.AskAnswer{}, nil
}
