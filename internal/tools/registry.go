package tools

import (
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// HostTools carries the host-supplied configuration the network and host-delegate tools need
// but the workspace-scoped file tools do not: the url-safety guard (with its default-on SSRF
// floor) for the network tools, the configured web-search endpoint (empty ⇒ the built-in
// DuckDuckGo default; "off" disables the tool), the Asker delegate (nil ⇒ ask_user is NOT
// registered, so the model is never offered a question it cannot have answered), and the
// Presenter delegate (nil ⇒ present_document is NOT registered, so a headless host never
// offers the model a document-showing affordance nobody can honour — ADR 0019). It is the
// seam NewDefaultRegistryWithHost threads from Config so the registry stays the single place
// the default tool set is assembled (P3.11).
type HostTools struct {
	URLGuard          security.URLGuard
	WebSearchEndpoint string
	Asker             domain.Asker
	Presenter         domain.Presenter

	// Disabled names the built-in tools this host must NOT offer — the global `tools.disabled:`
	// key. A named tool is left OUT of the assembled set entirely, which is both halves of the
	// switch in one act: the model is never offered it, and a call naming it resolves against
	// nothing, so the loop refuses it as an unknown tool. Empty/nil ⇒ the whole roster, which is
	// the default and byte-identical to the set built before this field existed.
	//
	// A name matching no built-in tool is simply ignored here. Reporting it is the HOST's job
	// (KnownToolNames is what it checks against): a registry assembly has nowhere to put a
	// warning, and a roster the user is pruning must not be able to stop a session from starting.
	Disabled []string
}

// NewDefaultRegistry assembles the built-in tool set — the read/write/list/grep base
// (P1.4), the file-editing family (P3.7), and the execution tools (P3.8) — each scoped to
// root, into a domain.ToolRegistry. It is the seam the engine uses to give an Agent its
// default tools (the loop's dispatch wires it in P1.2); an embedder can equally build a
// registry by hand and Register its own.
//
// It wires the network/host tools (P3.11) with a zero HostTools: the network tools run with
// the default URLGuard (SSRF floor on, no extra allow/deny), web_search uses its built-in
// DuckDuckGo default, and ask_user (nil Asker) and present_document (nil Presenter) are
// omitted. NewDefaultRegistryWithHost is the variant the composition root uses to thread the
// host's url-safety policy, search endpoint, Asker, and Presenter.
//
// Registration cannot fail here: the names are distinct and non-empty, the only
// conditions Register rejects.
func NewDefaultRegistry(root string) *domain.ToolRegistry {
	return NewDefaultRegistryWithHost(root, HostTools{})
}

// NewDefaultRegistryWithHost assembles the built-in tool set scoped to root, threading the
// host-supplied url-safety guard, web-search endpoint, Asker, and Presenter into the network
// and host-delegate tools (P3.11, ADR 0019). ask_user is registered only when host.Asker is
// non-nil and present_document only when host.Presenter is (graceful degradation — a host
// with no question or presentation delegate never exposes the matching tool).
func NewDefaultRegistryWithHost(root string, host HostTools) *domain.ToolRegistry {
	registry := domain.NewToolRegistry()
	for _, tool := range DefaultToolsWithHost(root, host) {
		_ = registry.Register(tool)
	}
	return registry
}

// DefaultTools returns the built-in tools scoped to root, in menu order. It is exposed
// so a caller can register a subset, or add them to a registry that already holds
// host-supplied tools. find_files sits beside grep in the base set — the two halves of
// discovery, by NAME and by CONTENT — and is read-only like it. The file-editing family
// (P3.7) follows the base set; the write
// tools among them (find-replace, edit_existing_file) carry the workspaceScopedWriter
// marker so the dispatch disposition path-bounds rather than confines them (ADR 0012 D1).
// The execution tools (P3.8 — terminal, python_exec) and the git tools (P3.9 —
// git_branch, git_commit, git_diff_range) follow; they are SubprocessTools the
// disposition confines in Auto (or gates when confinement is unavailable), not
// workspace-scoped writers (git_diff_range declares ReadOnly(), but the subprocess
// marker outranks the declaration — it too is confined or gated, and since
// 2026-08-02 Plan neither offers nor runs it, because the menu keys on the same
// class the ladder does). The
// diagnostics tool (P3.10) closes the file/exec set: a read-only SubprocessTool that checks
// Go in-process (plus optional go vet) and degrades gracefully for other languages. The
// network/host tools (P3.11) and the sub_agent recursion point (P3.13) follow; sub_agent
// carries NO disposition marker — dispatch special-cases it as the recursion point that
// drives a nested Agent, never a leaf tool (ADR 0013).
func DefaultTools(root string) []domain.Tool {
	return DefaultToolsWithHost(root, HostTools{})
}

// DefaultToolsWithHost returns the built-in tools scoped to root, in menu order, with the
// network/host tools (P3.11) configured from host. The network tools (web_fetch,
// http_request, web_search) are ExternalEffectTools of kind network — the disposition
// auto-runs them in Auto (url-filtered) and routes them through ExternalEffects for the
// bench; they carry NO workspaceScopedWriter marker (they are not Apogee's own writes) and
// are NOT SubprocessTools (in-process net/http). ask_user is appended only when host.Asker
// is set (a nil Asker omits it — graceful), and is ReadOnly (it runs in Plan, mode-independent
// through the Asker). present_document (ADR 0019) closes the menu on the same terms: appended
// only when host.Presenter is set, ReadOnly and mode-independent through the Presenter, and
// no more an ExternalEffectTool than ask_user is — showing the user a document they already
// own is not a non-forkable remote effect.
//
// host.Disabled is applied LAST, to the assembled menu: the roster switch subtracts from the set
// this build offers rather than deciding, per tool, whether to construct it — so a tool's presence
// stays one line above and its availability one list in the user's config.
func DefaultToolsWithHost(root string, host HostTools) []domain.Tool {
	all := []domain.Tool{
		NewReadFile(root),
		NewWriteFile(root),
		NewListDir(root),
		NewGrep(root),
		NewFindFiles(root),
		NewSingleFindReplace(root),
		NewMultiFindReplace(root),
		NewEditExistingFile(root),
		NewViewDiff(root),
		NewOpenFile(root),
		NewTerminal(root),
		NewPythonExec(root),
		NewGitBranch(root),
		NewGitCommit(root),
		NewGitDiffRange(root),
		NewDiagnostics(root),
		NewWebFetch(host.URLGuard),
		NewHTTPRequest(host.URLGuard),
		NewWebSearch(host.URLGuard, host.WebSearchEndpoint),
		NewSubAgent(),
	}
	if host.Asker != nil {
		all = append(all, NewAskUser(host.Asker))
	}
	if host.Presenter != nil {
		all = append(all, NewPresentDocument(root, host.Presenter))
	}
	return withoutDisabled(all, host.Disabled)
}

// withoutDisabled returns all minus the tools whose name is listed in disabled, in unchanged menu
// order. An empty list returns the menu untouched — the same slice, so the default roster costs
// nothing — and a listed name that matches no tool simply matches nothing.
//
// Names are trimmed before they are compared, because the list reaches here from a YAML sequence a
// human wrote: a stray space around a name is a spelling of that name, not a different tool.
func withoutDisabled(all []domain.Tool, disabled []string) []domain.Tool {
	if len(disabled) == 0 {
		return all
	}
	off := make(map[string]bool, len(disabled))
	for _, name := range disabled {
		off[strings.TrimSpace(name)] = true
	}
	kept := make([]domain.Tool, 0, len(all))
	for _, tool := range all {
		if !off[tool.Name()] {
			kept = append(kept, tool)
		}
	}
	return kept
}

// KnownToolNames returns the name of every tool this build's default set can offer, in menu order.
// It is the catalogue a host checks a configured `tools.disabled:` entry against, so a misspelled
// name is reported to the user instead of silently disabling nothing.
//
// The two host-delegate tools are included by CONSTRUCTION rather than by composition: a nil Asker
// or Presenter leaves them out of a registry (graceful degradation), but their names are still
// names apogee knows — so the answer is a fact about the build, not about one Driver's wiring.
// TestKnownToolNamesCoversTheComposedSet pins it to the assembly above.
func KnownToolNames() []string {
	all := DefaultToolsWithHost("", HostTools{})
	all = append(all, NewAskUser(nil), NewPresentDocument("", nil))
	names := make([]string, 0, len(all))
	for _, tool := range all {
		names = append(names, tool.Name())
	}
	return names
}
