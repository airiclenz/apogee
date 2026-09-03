package tools

import (
	"io/fs"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// HostTools carries the host-supplied configuration the network and host-delegate tools need
// but the workspace-scoped file tools do not: the url-safety guard (with its default-on SSRF
// floor) for the network tools, the configured web-search endpoint (empty ⇒ the built-in
// DuckDuckGo default; "off" disables the tool), the Asker delegate (nil ⇒ ask_user is NOT
// registered, so the model is never offered a question it cannot have answered), the
// Presenter delegate (nil ⇒ present_document is NOT registered, so a headless host never
// offers the model a document-showing affordance nobody can honour — ADR 0019), and the
// SkillLookup catalog (nil ⇒ load_skill is NOT registered — ADR 0065). It is the
// seam NewDefaultRegistryWithHost threads from Config so the registry stays the single place
// the default tool set is assembled (P3.11).
type HostTools struct {
	URLGuard          security.URLGuard
	WebSearchEndpoint string
	Asker             domain.Asker
	Presenter         domain.Presenter

	// SkillLookup is the catalog the load_skill tool searches on the model's behalf (ADR 0065 §6);
	// nil ⇒ load_skill is NOT registered, so a Driver with no skill catalog never offers the model
	// a door onto nothing — the same graceful degradation a nil Asker gives ask_user.
	//
	// It carries no knowledge of what a skill IS into this package: the seam is a query in and an
	// answer out (domain.SkillLookup), and the host decides what backs it (ADR 0031 — engine seams
	// stay driver-agnostic).
	SkillLookup domain.SkillLookup

	// Disabled names the built-in tools this host must NOT offer — the global `tools.disabled:`
	// key. A named tool is left OUT of the assembled set entirely, which is both halves of the
	// switch in one act: the model is never offered it, and a call naming it resolves against
	// nothing, so the loop refuses it as an unknown tool. Empty/nil ⇒ the whole roster, which is
	// the default and byte-identical to the set built before this field existed.
	//
	// A name matching no built-in tool is simply ignored here. Reporting it is the HOST's job
	// (KnownToolNames is what it checks against): a registry assembly has nowhere to put a
	// warning, and a roster the user is pruning must not be able to stop a session from starting.
	//
	// It is the GLOBAL rung of the roster precedence ladder — profile > global > build default
	// (ADR 0057) — so it is the default word on a tool rather than the last one: a name in
	// ProfileRoster.Enabled puts back what this list drops.
	Disabled []string

	// Enabled names the built-in tools this host must offer even though the BUILD leaves them out
	// of the default menu — the global `tools.enabled:` key, the lift a tool registered default-off
	// (domain.DefaultOffTool) needs. Empty/nil ⇒ nothing is lifted, which leaves the four tools of
	// the Console family — console_open, console_send, console_read, console_close — registered and
	// unoffered: they are the first built-ins to ship default-off (ADR 0059 §3, on ADR 0057's build
	// rung), so naming one here is what this key is for.
	//
	// It is the enable direction of the same GLOBAL rung Disabled spells, so a name in BOTH lists
	// is a conflict DISABLED wins — fail closed. Like an unknown name, that conflict is the HOST's
	// to report (RosterConflicts is the query it asks); the assembly builds a roster either way.
	Enabled []string

	// ProfileRoster is the matched Model profile's `tools:` axis — the delta pair that makes the
	// roster per-model (ADR 0057). It is the MOST SPECIFIC rung of the ladder, so a name here is
	// the last word on that tool in either direction: a profile `enabled:` entry lifts a globally
	// disabled or default-off tool, a profile `disabled:` entry turns off what global allows. A
	// zero delta ⇒ this rung says nothing and the global lists and the build default decide.
	ProfileRoster domain.ToolRosterDelta

	// ExtraReadRoots reports directories the READ-ONLY tools may reach outside the workspace.
	// The contract, in four clauses:
	//
	//   - READ-ONLY: the read tools take it, and copy_file for its SOURCE alone — a copy's source
	//     is a read (2026-08-12). Every WRITE stays workspace-fenced, copy_file's destination
	//     included, as does every execution tool (the workspaceScopedWriter discipline, ADR 0012
	//     D1) — mounting a directory here never makes it writable.
	//   - ABSOLUTE paths only: a relative argument keeps resolving against the workspace root
	//     alone, so no one name can mean two files.
	//   - LIVE: the func is evaluated once per tool call, so a mid-session change on the host's
	//     side is honoured by the next read with no re-wiring.
	//   - nil ⇒ workspace-only, byte-identical to the fence before this field existed.
	//
	// It is a generic seam: a skills library is the first thing mounted through it, but nothing
	// in this package knows that (ADR 0031 — engine seams stay driver-agnostic). Each root keeps
	// its own os.Root fence, so a symlink inside one that escapes it is still refused, and a root
	// that does not exist yet is skipped rather than failing the call.
	ExtraReadRoots func() []string

	// VirtualReadRoots reports read-only trees the host mounts under a NAME rather than under a
	// host path, keyed by the prefix their addresses are spelled with (`shipped:`). It carries
	// ExtraReadRoots' four clauses unchanged — read-only, live per call, nil ⇒ none — and adds
	// the one property that makes it a separate seam: there is no host path at all, so no root
	// string could have named these trees (apogee's shipped skills are compiled into the binary,
	// ADR 0065 §3). Consulted BEFORE the disk roots, which costs nothing: a mount reference is a
	// spelling no host path can take (path_virtual.go).
	//
	// It is generic in the same way ExtraReadRoots is: shipped skills are the first thing mounted
	// through it, but nothing in this package knows what a skill is (ADR 0031). A write NEVER
	// reaches it — the spelling itself is refused on the write side — so mounting a tree here can
	// no more make it writable than mounting a directory there can.
	VirtualReadRoots func() map[string]fs.FS

	// SecretEnvVars names environment variables the EXECUTION tools (terminal, python_exec,
	// run_tests) must drop from the environment they hand a subprocess, on top of apogee's own
	// credential names, which are always dropped. It is the host's list of the variables its
	// configured key sources read (`api-key-env:`, ADR 0047): a key the operator exported into the
	// shell apogee was started from would otherwise be inherited by every child whose contents the
	// MODEL chose. Empty/nil ⇒ the fixed scrub alone, byte-identical to the behaviour before this
	// field existed.
	//
	// The names arrive as plain strings rather than as config types because internal/config imports
	// THIS package, so the dependency cannot point back; they are compared case-insensitively
	// (isSecretEnv), and a name matching nothing in the environment is simply not there to drop.
	SecretEnvVars []string

	// SubAgentSeatChoice offers the model the `run_on` argument on sub_agent — the host's
	// `sub-agents-choice: model` gate (ADR 0069). False is the default and the whole of
	// `sub-agents-choice: fixed`: the plain variant is registered and the schema is byte-identical
	// to the one built before seat choice existed, so a model that cannot pick a seat is never
	// told about one.
	//
	// It shapes the tool this build carries rather than whether it carries it, which is why it is
	// not a roster delta: sub_agent is offered either way, and the two variants differ only in the
	// argument published. Where the choice IS offered, the seat is still resolved by the engine —
	// this flag decides what the model may ASK for, never where the child ends up.
	SubAgentSeatChoice bool
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
// (P3.7) follows the base set, closed since 2026-08-10 by the file-operation trio (copy_file,
// move_file and delete_file — the same family's move- and remove-bytes-that-exist half); the write
// tools among them (find-replace, edit_existing_file, copy_file, move_file, delete_file) carry the
// workspaceScopedWriter marker so the dispatch disposition path-bounds rather than confines
// them (ADR 0012 D1).
// The execution tools (P3.8 — terminal, python_exec) and the git tools (P3.9 —
// git_branch, git_commit, git_diff_range, joined 2026-08-10 by git_status and git_log)
// follow; they are SubprocessTools the
// disposition confines in Auto (or gates when confinement is unavailable), not
// workspace-scoped writers (git_diff_range, git_status and git_log declare ReadOnly(), but the
// subprocess marker outranks the declaration — they too are confined or gated, and since
// 2026-08-02 Plan neither offers nor runs them, because the menu keys on the same
// class the ladder does). The
// diagnostics tool (P3.10) closes the file/exec set: a read-only SubprocessTool that checks
// Go in-process (plus optional go vet) and degrades gracefully for other languages, joined
// 2026-08-10 by run_tests — the same verification question asked of the whole project rather
// than of one file, a write-capable SubprocessTool on terminal's disposition whose result is a
// condensed verdict rather than the runner's log. The
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
// own is not a non-forkable remote effect. load_skill (ADR 0065) joins them on the same terms:
// appended only when host.SkillLookup is set, ReadOnly (fetching prompt text writes nothing) and
// no ExternalEffectTool either — the catalog it searches is in this process.
//
// host.ExtraReadRoots and host.VirtualReadRoots are threaded, as ONE ReadMounts, into the four
// read-only file tools (read_file, list_dir, grep, find_files) and — since 2026-08-12, for its
// SOURCE alone — into copy_file, each of which resolves an ABSOLUTE path over those roots (or a
// mount reference over those mounts) when the workspace refuses it; a zero ReadMounts leaves them
// workspace-only. Nothing else receives it, and no WRITE widens: copy_file's destination, like
// every other write and execution tool, stays workspace-fenced — see the field's contract.
//
// host.SecretEnvVars is threaded into the three EXECUTION tools (terminal, python_exec, run_tests)
// — the only tools that hand a subprocess the operator's inherited environment — where it joins
// apogee's own credential names in the scrub each of them applies; nil leaves that scrub exactly
// as it was. Nothing else receives it: the git and Go-toolchain subprocesses take an allowlist
// instead, which no configured name can widen.
//
// The roster ladder is applied LAST, to the assembled menu (EffectiveRoster): the build's own
// default-off declarations, then host.Disabled / host.Enabled, then host.ProfileRoster subtract
// from and add back to the set this build offers, rather than deciding per tool whether to
// construct it — so a tool's presence stays one line in builtinTools and its availability one list
// in the user's config. A same-scope conflict is dropped here for the same reason an unknown name
// is: an assembly has nowhere to put a warning (RosterConflicts is the host's query).
func DefaultToolsWithHost(root string, host HostTools) []domain.Tool {
	kept, _ := EffectiveRoster(builtinTools(root, host), host.rosterDeltas())
	return kept
}

// builtinTools returns every tool this BUILD carries, scoped to root and configured from host, in
// menu order and before any roster delta — a tool registered default-off included. It is the rung
// the ladder starts from: DefaultToolsWithHost applies the deltas to it, and KnownToolNames reads
// its names off it, so a default-off tool is still a name apogee knows while nothing offers it.
func builtinTools(root string, host HostTools) []domain.Tool {
	mounts := host.readMounts()
	all := []domain.Tool{
		NewReadFile(root, mounts),
		NewWriteFile(root),
		NewListDir(root, mounts),
		NewGrep(root, mounts),
		NewFindFiles(root, mounts),
		NewSingleFindReplace(root),
		NewMultiFindReplace(root),
		NewEditExistingFile(root),
		NewViewDiff(root),
		NewCopyFile(root, mounts),
		NewMoveFile(root),
		NewDeleteFile(root),
		NewTerminal(root, host.SecretEnvVars),
		NewPythonExec(root, host.SecretEnvVars),
		NewGitBranch(root),
		NewGitCommit(root),
		NewGitDiffRange(root),
		NewGitStatus(root),
		NewGitLog(root),
		NewDiagnostics(root),
		NewRunTests(root, host.SecretEnvVars),
		NewWebFetch(host.URLGuard),
		NewHTTPRequest(host.URLGuard),
		NewWebSearch(host.URLGuard, host.WebSearchEndpoint),
		NewSubAgentWith(SubAgentOptions{SeatChoice: host.SubAgentSeatChoice}),
		// task_list (ADR 0072) is the last DEFAULT-ON slot: it holds the model's own checklist as
		// engine state, so it is offered to every model and `tools.disabled:` is what turns it off.
		NewTaskList(),
		// The Console family (ADR 0059) sits last because it is the first family registered
		// DEFAULT-OFF: nothing here reaches a default menu, so its place in build order costs no
		// model a slot, and a roster that lifts it appends it after the tools every model gets.
		NewConsoleOpen(root, host.SecretEnvVars),
		NewConsoleSend(),
		NewConsoleRead(),
		NewConsoleClose(),
	}
	if host.SkillLookup != nil {
		all = append(all, NewLoadSkill(host.SkillLookup))
	}
	if host.Asker != nil {
		all = append(all, NewAskUser(host.Asker))
	}
	if host.Presenter != nil {
		all = append(all, NewPresentDocument(root, host.Presenter))
	}
	return all
}

// readMounts pairs the host's two read-only mount seams into the one value every read tool takes,
// so a tool can never be wired with the disk roots and without the virtual mounts.
func (h HostTools) readMounts() ReadMounts {
	return ReadMounts{Roots: h.ExtraReadRoots, Virtual: h.VirtualReadRoots}
}

// rosterDeltas reads the two CONFIGURATION rungs of the ladder off HostTools: the global lists the
// host folds in from `tools.disabled:`/`tools.enabled:`, and the matched Model profile's axis.
func (h HostTools) rosterDeltas() RosterDeltas {
	return RosterDeltas{
		Global:  domain.ToolRosterDelta{Disabled: h.Disabled, Enabled: h.Enabled},
		Profile: h.ProfileRoster,
	}
}

// RosterDeltas carries the two CONFIGURATION rungs of the roster precedence ladder — the global
// `tools.disabled:`/`tools.enabled:` lists and the matched Model profile's `tools:` axis (ADR
// 0057). The third rung, the build default, travels with the tool itself as its DefaultOffTool
// declaration rather than in this struct. The zero value is no deltas at all: the roster the build
// offers, unchanged.
type RosterDeltas struct {
	// Global is the host-wide rung (domain.Config.DisabledTools / EnabledTools). It overrides a
	// tool's build default and is itself overridden, per tool, by Profile.
	Global domain.ToolRosterDelta

	// Profile is the matched Model profile's roster axis — the most specific rung, so a name here
	// is the last word on that tool whichever direction it points.
	Profile domain.ToolRosterDelta
}

// RosterScope names the rung a conflict was found in, so the host's one-line NOTICE can say which
// list the user has to fix.
type RosterScope string

const (
	// RosterScopeGlobal is the global `tools.disabled:` / `tools.enabled:` pair.
	RosterScopeGlobal RosterScope = "global"
	// RosterScopeProfile is the matched Model profile's `tools:` axis.
	RosterScopeProfile RosterScope = "profile"
)

// RosterConflict reports ONE tool named in BOTH directions of ONE scope — `disabled:` and
// `enabled:` of the same rung, which cannot both be honoured. Disabled wins (fail closed) and the
// conflict is handed back to the caller, because that is the only layer that can tell the user:
// neither a registry assembly nor a rebind may refuse a session over a roster the user is editing
// (ADR 0057).
type RosterConflict struct {
	// Scope is the rung whose two lists disagree.
	Scope RosterScope
	// Tool is the offending name, trimmed as the ladder compares it.
	Tool string
}

// EffectiveRoster answers the whole roster question in one pure pass: which of the tools in all a
// host actually offers, given the ladder profile > global > build default (ADR 0057). It starts
// from the BUILD default — every tool except those registered default-off (domain.IsDefaultOff) —
// then lets the global lists override that per tool, then the profile lists override those, so the
// most specific word about a tool wins in either direction. Within ONE scope a name in both lists
// resolves to DISABLED and is returned as a RosterConflict for the caller to report.
//
// Menu order is preserved and nothing is reordered or constructed: the ladder only subtracts from
// and adds back to the set it is given. When it drops nothing it returns the very slice it was
// given, so the default roster — no deltas, no default-off tool — costs nothing. A name matching
// no tool matches nothing; reporting the typo is the caller's job (KnownToolNames).
//
// It applies to the DEFAULT tool set only. An injected domain.Config.Tools is the host's own
// assembly, taken exactly as given (ADR 0001), and never reaches this function.
//
// Names are trimmed before they are compared, because the lists reach here from YAML sequences a
// human wrote: a stray space around a name is a spelling of that name, not a different tool.
func EffectiveRoster(all []domain.Tool, deltas RosterDeltas) ([]domain.Tool, []RosterConflict) {
	conflicts := RosterConflicts(deltas)

	// One verdict per NAMED tool, written in ladder order: enabled first and disabled second
	// within a scope (so disabled wins the same-scope tie), global before profile (so the profile
	// has the last word). A tool no list names keeps its build default below.
	verdict := make(map[string]bool)
	for _, scope := range []domain.ToolRosterDelta{deltas.Global, deltas.Profile} {
		for _, name := range trimmedNames(scope.Enabled) {
			verdict[name] = true
		}
		for _, name := range trimmedNames(scope.Disabled) {
			verdict[name] = false
		}
	}

	kept := make([]domain.Tool, 0, len(all))
	for _, tool := range all {
		on, named := verdict[tool.Name()]
		if !named {
			on = !domain.IsDefaultOff(tool)
		}
		if on {
			kept = append(kept, tool)
		}
	}
	if len(kept) == len(all) {
		return all, conflicts
	}
	return kept, conflicts
}

// RosterConflicts returns every tool named in both directions of one scope, global rung first and
// within a rung in the order `disabled:` spells them — a deterministic line for the host's NOTICE.
// It is the query the config layer asks at load time, before any tool exists to compose: the rule
// that disabled wins a same-scope tie lives here alone, so the notice and the roster cannot drift.
// No conflicts ⇒ nil.
func RosterConflicts(deltas RosterDeltas) []RosterConflict {
	scopes := []struct {
		scope RosterScope
		delta domain.ToolRosterDelta
	}{
		{RosterScopeGlobal, deltas.Global},
		{RosterScopeProfile, deltas.Profile},
	}
	var conflicts []RosterConflict
	for _, s := range scopes {
		enabled := make(map[string]bool, len(s.delta.Enabled))
		for _, name := range trimmedNames(s.delta.Enabled) {
			enabled[name] = true
		}
		seen := make(map[string]bool, len(enabled))
		for _, name := range trimmedNames(s.delta.Disabled) {
			if enabled[name] && !seen[name] {
				seen[name] = true
				conflicts = append(conflicts, RosterConflict{Scope: s.scope, Tool: name})
			}
		}
	}
	return conflicts
}

// trimmedNames returns the roster list with each name trimmed and the empty ones dropped: an empty
// YAML item names no tool, and letting it through would make two blank lines look like a conflict.
func trimmedNames(names []string) []string {
	trimmed := make([]string, 0, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			trimmed = append(trimmed, name)
		}
	}
	return trimmed
}

// KnownToolNames returns the name of every tool this BUILD carries, in menu order. It is the
// catalogue a host checks a configured roster entry against — `tools.disabled:`, `tools.enabled:`
// and a profile's `tools:` axis alike — so a misspelled name is reported to the user instead of
// silently disabling nothing.
//
// It reads the build rung rather than the composed menu, so a tool registered default-off is still
// a name apogee knows: `tools.enabled:` exists precisely to name one, and must never be told its
// only valid entry is a typo.
//
// The three host-delegate tools are included by CONSTRUCTION rather than by composition: a nil
// Asker, Presenter or SkillLookup leaves them out of a registry (graceful degradation), but their
// names are still names apogee knows — so the answer is a fact about the build, not about one
// Driver's wiring. TestKnownToolNamesCoversTheComposedSet pins it to the assembly above.
func KnownToolNames() []string {
	all := builtinTools("", HostTools{})
	all = append(all, NewLoadSkill(nil), NewAskUser(nil), NewPresentDocument("", nil))
	names := make([]string, 0, len(all))
	for _, tool := range all {
		names = append(names, tool.Name())
	}
	return names
}
