package tools

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

func TestNewDefaultRegistry_HoldsTheBuiltInTools(t *testing.T) {
	t.Parallel()

	registry := NewDefaultRegistry(t.TempDir())

	for _, name := range []string{
		"read_file", "write_file", "list_dir", "grep", "find_files",
		"single_find_and_replace", "multi_find_and_replace", "edit_existing_file",
		"view_diff", "open_file", "copy_file", "move_file", "delete_file",
		"terminal", "python_exec",
		"git_branch", "git_commit", "git_diff_range", "git_status", "git_log",
		"diagnostics",
		"web_fetch", "http_request", "web_search",
		"sub_agent",
	} {
		if _, ok := registry.Lookup(name); !ok {
			t.Errorf("default registry is missing %q", name)
		}
	}

	// ask_user (no Asker) and present_document (no Presenter) are omitted when NewDefaultRegistry
	// uses a zero HostTools, so the default set is 25 (21 base/exec/git/diag + 3 network +
	// sub_agent).
	if got := len(registry.All()); got != 25 {
		t.Errorf("default registry holds %d tools, want 25", got)
	}
	if _, ok := registry.Lookup("ask_user"); ok {
		t.Error("ask_user must NOT be registered without an Asker")
	}
	if _, ok := registry.Lookup("present_document"); ok {
		t.Error("present_document must NOT be registered without a Presenter")
	}
}

func TestNewDefaultRegistry_MenuOrderIsDeterministic(t *testing.T) {
	t.Parallel()

	registry := NewDefaultRegistry(t.TempDir())

	want := []string{
		"read_file", "write_file", "list_dir", "grep", "find_files",
		"single_find_and_replace", "multi_find_and_replace", "edit_existing_file",
		"view_diff", "open_file", "copy_file", "move_file", "delete_file",
		"terminal", "python_exec",
		"git_branch", "git_commit", "git_diff_range", "git_status", "git_log",
		"diagnostics",
		"web_fetch", "http_request", "web_search",
		"sub_agent",
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
	if got := len(all); got != 26 {
		t.Errorf("registry with Asker holds %d tools, want 26", got)
	}
}

func TestNewDefaultRegistryWithHost_RegistersPresentDocumentOnlyWithPresenter(t *testing.T) {
	t.Parallel()

	// No Presenter ⇒ present_document absent (a headless host never offers a document-showing
	// affordance nobody can honour — ADR 0019).
	if _, ok := NewDefaultRegistryWithHost(t.TempDir(), HostTools{}).Lookup("present_document"); ok {
		t.Error("present_document must be absent without a Presenter")
	}

	// A Presenter alone ⇒ present_document present; ask_user stays absent (the two delegates are
	// independent).
	reg := NewDefaultRegistryWithHost(t.TempDir(), HostTools{Presenter: stubPresenter{}})
	if _, ok := reg.Lookup("present_document"); !ok {
		t.Fatal("present_document must be present with a Presenter")
	}
	if _, ok := reg.Lookup("ask_user"); ok {
		t.Error("ask_user must stay absent when only a Presenter is configured")
	}
	if got := len(reg.All()); got != 26 {
		t.Errorf("registry with a Presenter holds %d tools, want 26", got)
	}

	// Both delegates ⇒ both tools, present_document last in the menu.
	both := NewDefaultRegistryWithHost(t.TempDir(), HostTools{Asker: stubAsker{}, Presenter: stubPresenter{}}).All()
	if got := len(both); got != 27 {
		t.Errorf("registry with both delegates holds %d tools, want 27", got)
	}
	if got := both[len(both)-1].Name(); got != "present_document" {
		t.Errorf("present_document should be last in the menu, got last = %q", got)
	}
}

// TestNewDefaultRegistryWithHost_ThreadsURLGuardIntoNetworkTools pins the assembly seam the
// url-filter marker's vouching rests on: the marker asserts every outbound URL passed THE
// HOST's URLGuard (ADR 0012 Amendment 2026-07-25(a)), which is false the moment the registry
// hands a network tool a guard that is not the host's. The zero URLGuard still has the SSRF
// floor on — and the composition root currently passes exactly that — so replacing
// host.URLGuard with security.URLGuard{} at one of the three call sites would drop an
// embedder's DenyHosts policy SILENTLY: the tool set still assembles, every other registry
// test still passes, and only the embedder's own allow/deny policy quietly stops applying.
//
// The DENY WORDING is the load-bearing assertion, not merely "an error": a dropped guard
// leaves the zero value, whose floor refuses this host too (it does not resolve), so a test
// that only asked for a url-safety refusal would stay green through exactly the regression it
// exists to catch. Only "host … is denied" proves the HOST's policy is what refused.
func TestNewDefaultRegistryWithHost_ThreadsURLGuardIntoNetworkTools(t *testing.T) {
	t.Parallel()

	const deniedHost = "blocked.example"

	// The host's policy: one denied host. The injected resolver answers a public address for
	// every name, so the test stays hermetic (no DNS) even though the deny check fires first —
	// a future reordering that let the floor run ahead of it cannot turn this into a lookup.
	guard := security.URLGuard{DenyHosts: []string{deniedHost}}.
		WithResolver(func(context.Context, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		})
	registry := NewDefaultRegistryWithHost(t.TempDir(), HostTools{
		URLGuard: guard,
		// web_search never takes a URL from the model: the CONFIGURED endpoint is the only
		// host it ever requests, so that is where its guard threading shows.
		WebSearchEndpoint: "https://" + deniedHost + "/s",
	})

	cases := []struct {
		tool string
		args map[string]any
	}{
		{tool: "web_fetch", args: map[string]any{"url": "https://" + deniedHost + "/"}},
		{tool: "http_request", args: map[string]any{"url": "https://" + deniedHost + "/"}},
		{tool: "web_search", args: map[string]any{"query": "needle"}},
	}

	// A future fourth network tool must be threaded too, so the table is required to cover
	// every EffectNetwork tool in the default set rather than trusting an author to remember.
	covered := make(map[string]bool, len(cases))
	for _, tc := range cases {
		covered[tc.tool] = true
	}
	for _, tool := range DefaultToolsWithHost(t.TempDir(), HostTools{}) {
		ext, ok := tool.(domain.ExternalEffectTool)
		if !ok || ext.ExternalEffect() != domain.EffectNetwork {
			continue
		}
		if !covered[tool.Name()] {
			t.Errorf("%q declares EffectNetwork but no row of this table drives it: add one, so the host's URLGuard is pinned as arriving in that tool too", tool.Name())
		}
	}

	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			t.Parallel()

			tool, ok := registry.Lookup(tc.tool)
			if !ok {
				t.Fatalf("%q is not in the default registry", tc.tool)
			}

			res, err := tool.Execute(context.Background(), domain.ToolCall{
				ID: "c1", Tool: tc.tool, Arguments: jsonArgs(t, tc.args),
			})

			if err != nil {
				t.Fatalf("a blocked URL is not caller cancellation, so it must not be a Go error: %v", err)
			}
			if !res.IsError {
				t.Fatalf("%q did not refuse the host the embedder denied: the host's URLGuard never reached the tool: %q", tc.tool, res.Content)
			}
			if !strings.Contains(res.Content, "url-safety") {
				t.Errorf("%q: the refusal must come from url-safety: %q", tc.tool, res.Content)
			}
			if !strings.Contains(res.Content, "is denied") {
				t.Errorf("%q: the refusal must be the HOST's DenyHosts policy, not the zero guard's floor refusing an unresolvable host: %q", tc.tool, res.Content)
			}
			if !strings.Contains(res.Content, deniedHost) {
				t.Errorf("%q: the refusal should name the denied host for diagnosability: %q", tc.tool, res.Content)
			}
		})
	}
}

func TestDefaultTools_DeclareReadOnlyNature(t *testing.T) {
	t.Parallel()

	want := map[string]bool{
		"read_file":  true,
		"list_dir":   true,
		"grep":       true,
		"find_files": true,  // naming files is a read, like grep's reading of them
		"write_file": false, // the lone write tool: must gate through Approval (P1.2)
		// File-editing family (P3.7): writers gate, diff/open-file read.
		"single_find_and_replace": false,
		"multi_find_and_replace":  false,
		"edit_existing_file":      false,
		"view_diff":               true,
		"open_file":               true,
		// File operations (2026-08-10): moving or removing bytes that already exist is still a
		// write — all three must gate, like every other member of the family.
		"copy_file":   false,
		"move_file":   false,
		"delete_file": false,
		// Execution tools (P3.8): write-capable subprocess tools — the loop confines/gates
		// them, so they must not declare read-only.
		"terminal":    false,
		"python_exec": false,
		// Git tools (P3.9): branch/commit mutate the repo (write-capable); diff-range is a
		// harmless read. Its read-only DECLARATION is an honest statement about the tool, but it
		// decides neither the class nor the Plan menu, because the subprocess marker outranks it
		// (§4 amended 2026-07-26, menu 2026-08-02): the call is confined/gated as a subprocess
		// and Plan does not offer it.
		"git_branch":     false,
		"git_commit":     false,
		"git_diff_range": true,
		// git_status and git_log (2026-08-10) report the working tree and the history and mutate
		// nothing — the same declaration-vs-marker split as git_diff_range.
		"git_status": true,
		"git_log":    true,
		// Diagnostics (P3.10): inspects only — read-only, same declaration-vs-marker split as
		// git_diff_range (the go vet half shells out, so the call takes the subprocess row).
		"diagnostics": true,
		// Network tools (P3.11): external-effect, write-capable (the loop gates/auto-runs them
		// by effect kind, not read-only) — they must NOT declare read-only.
		"web_fetch":    false,
		"http_request": false,
		"web_search":   false,
		// sub_agent (P3.13): the recursion point carries NO disposition marker — it is not
		// read-only (its blast radius is owned per-child-call one level down, ADR 0013).
		"sub_agent": false,
		// ask_user (P3.11): asking a question writes nothing — read-only, runs in Plan.
		"ask_user": true,
		// present_document (ADR 0019): showing a document writes nothing — read-only, runs in
		// Plan, mode-independent through the Presenter.
		"present_document": true,
	}

	for _, tool := range DefaultToolsWithHost(t.TempDir(), HostTools{Asker: stubAsker{}, Presenter: stubPresenter{}}) {
		if got := domain.IsReadOnly(tool); got != want[tool.Name()] {
			t.Errorf("IsReadOnly(%q) = %v, want %v", tool.Name(), got, want[tool.Name()])
		}
	}
}

// TestHostToolsDisabled_DropsExactlyTheListedTools pins the roster switch (`tools.disabled:`): the
// named tools leave the menu, everything else keeps its place and its order, and both halves of
// "disabled" hold at once — the set the model is OFFERED and the set a call can RESOLVE against are
// the same registry, so a tool that is not offered cannot be dispatched either.
func TestHostToolsDisabled_DropsExactlyTheListedTools(t *testing.T) {
	t.Parallel()

	full := NewDefaultRegistry(t.TempDir()).All()
	registry := NewDefaultRegistryWithHost(t.TempDir(), HostTools{Disabled: []string{"view_diff", "python_exec"}})

	for _, name := range []string{"view_diff", "python_exec"} {
		if _, ok := registry.Lookup(name); ok {
			t.Errorf("%q is disabled but a call could still resolve it", name)
		}
	}
	var offered []string
	for _, tool := range registry.All() {
		offered = append(offered, tool.Name())
	}
	if len(offered) != len(full)-2 {
		t.Fatalf("disabling two tools left %d of %d", len(offered), len(full))
	}
	// Everything else survives, in unchanged menu order.
	want := make([]string, 0, len(full))
	for _, tool := range full {
		if name := tool.Name(); name != "view_diff" && name != "python_exec" {
			want = append(want, name)
		}
	}
	for i, name := range offered {
		if name != want[i] {
			t.Errorf("tool %d = %q, want %q — the switch must subtract, never reorder", i, name, want[i])
		}
	}
}

// TestHostToolsDisabled_EmptyAndUnknownChangeNothing pins the two no-ops the key must have: the
// default (nothing listed) is the whole roster, and a name that is no tool disables nothing rather
// than failing the assembly — reporting the typo is the host's job, not this one's.
func TestHostToolsDisabled_EmptyAndUnknownChangeNothing(t *testing.T) {
	t.Parallel()

	full := len(NewDefaultRegistry(t.TempDir()).All())
	for _, tc := range []struct {
		name     string
		disabled []string
	}{
		{name: "nil", disabled: nil},
		{name: "empty", disabled: []string{}},
		{name: "unknown name", disabled: []string{"grepp"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := NewDefaultRegistryWithHost(t.TempDir(), HostTools{Disabled: tc.disabled}).All()
			if len(got) != full {
				t.Errorf("roster holds %d tools, want the full %d", len(got), full)
			}
		})
	}
}

// TestHostToolsDisabled_TrimsTheNamesItIsGiven pins the forgiveness the key needs: the list reaches
// the assembly from a YAML sequence a human wrote, so a stray space around a name is a spelling of
// that name rather than a different tool.
func TestHostToolsDisabled_TrimsTheNamesItIsGiven(t *testing.T) {
	t.Parallel()

	registry := NewDefaultRegistryWithHost(t.TempDir(), HostTools{Disabled: []string{"  grep  "}})
	if _, ok := registry.Lookup("grep"); ok {
		t.Error("a name written with surrounding spaces must still disable its tool")
	}
}

// TestKnownToolNamesCoversTheComposedSet pins the catalogue a host checks a configured name against
// to the assembly itself: every tool a fully-composed registry holds must be a name KnownToolNames
// answers for, or a valid `tools.disabled:` entry would be reported to the user as a typo.
func TestKnownToolNamesCoversTheComposedSet(t *testing.T) {
	t.Parallel()

	known := make(map[string]bool)
	for _, name := range KnownToolNames() {
		known[name] = true
	}
	composed := DefaultToolsWithHost(t.TempDir(), HostTools{Asker: stubAsker{}, Presenter: stubPresenter{}})
	for _, tool := range composed {
		if !known[tool.Name()] {
			t.Errorf("%q is a built-in tool but KnownToolNames does not list it", tool.Name())
		}
	}
	if got := len(KnownToolNames()); got != len(composed) {
		t.Errorf("KnownToolNames lists %d names for %d composed tools", got, len(composed))
	}
}

// stubAsker is a no-op Asker for the registry tests (it is never called — the tests only
// check registration/ordering). ask_user's round-trip behaviour is covered in ask_user_test.go.
type stubAsker struct{}

func (stubAsker) Ask(context.Context, domain.AskRequest) (domain.AskAnswer, error) {
	return domain.AskAnswer{}, nil
}

// stubPresenter is a no-op Presenter for the registry tests (never called — the tests only
// check registration/ordering). present_document's behaviour lives in present_document_test.go.
type stubPresenter struct{}

func (stubPresenter) Present(context.Context, domain.PresentRequest) (domain.PresentOutcome, error) {
	return domain.PresentOutcome{}, nil
}
