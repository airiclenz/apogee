package tools

import (
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// HostTools carries the host-supplied configuration the network and ask-user tools need but
// the workspace-scoped file tools do not: the url-safety guard (with its default-on SSRF
// floor) for the network tools, the configured web-search endpoint (empty ⇒ the built-in
// DuckDuckGo default; "off" disables the tool), and the Asker delegate (nil ⇒ ask_user is NOT
// registered, so the model is never offered a question it cannot have answered). It is the
// seam NewDefaultRegistryWithHost threads from Config so the registry stays the single place
// the default tool set is assembled (P3.11).
type HostTools struct {
	URLGuard          security.URLGuard
	WebSearchEndpoint string
	Asker             domain.Asker
}

// NewDefaultRegistry assembles the built-in tool set — the read/write/list/grep base
// (P1.4), the file-editing family (P3.7), and the execution tools (P3.8) — each scoped to
// root, into a domain.ToolRegistry. It is the seam the engine uses to give an Agent its
// default tools (the loop's dispatch wires it in P1.2); an embedder can equally build a
// registry by hand and Register its own.
//
// It wires the network/host tools (P3.11) with a zero HostTools: the network tools run with
// the default URLGuard (SSRF floor on, no extra allow/deny), web_search uses its built-in
// DuckDuckGo default, and ask_user is omitted (nil Asker). NewDefaultRegistryWithHost is the variant
// the composition root uses to thread the host's url-safety policy, search endpoint, and Asker.
//
// Registration cannot fail here: the names are distinct and non-empty, the only
// conditions Register rejects.
func NewDefaultRegistry(root string) *domain.ToolRegistry {
	return NewDefaultRegistryWithHost(root, HostTools{})
}

// NewDefaultRegistryWithHost assembles the built-in tool set scoped to root, threading the
// host-supplied url-safety guard, web-search endpoint, and Asker into the network and
// ask-user tools (P3.11). ask_user is registered only when host.Asker is non-nil (graceful
// degradation — a host with no question delegate never exposes the tool).
func NewDefaultRegistryWithHost(root string, host HostTools) *domain.ToolRegistry {
	registry := domain.NewToolRegistry()
	for _, tool := range DefaultToolsWithHost(root, host) {
		_ = registry.Register(tool)
	}
	return registry
}

// DefaultTools returns the built-in tools scoped to root, in menu order. It is exposed
// so a caller can register a subset, or add them to a registry that already holds
// host-supplied tools. The file-editing family (P3.7) follows the base set; the write
// tools among them (find-replace, edit_existing_file) carry the workspaceScopedWriter
// marker so the dispatch disposition path-bounds rather than confines them (ADR 0012 D1).
// The execution tools (P3.8 — terminal, python_exec) and the git tools (P3.9 —
// git_branch, git_commit, git_diff_range) follow; they are SubprocessTools the
// disposition confines in Auto (or gates when confinement is unavailable), not
// workspace-scoped writers (git_diff_range is read-only and runs freely). The
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
// through the Asker).
func DefaultToolsWithHost(root string, host HostTools) []domain.Tool {
	all := []domain.Tool{
		NewReadFile(root),
		NewWriteFile(root),
		NewListDir(root),
		NewGrep(root),
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
	return all
}
