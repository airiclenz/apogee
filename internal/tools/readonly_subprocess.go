package tools

import "github.com/airiclenz/apogee/internal/domain"

// ----------------------------------------------------------------------------
// The readOnlySubprocess marker (confinement-execution-contract §4)
// ----------------------------------------------------------------------------
//
// readOnlySubprocess is the unexported marker carried ONLY by Apogee's own hardened
// read-side git tools. Like workspaceScopedWriter (workspace_scoped.go) its unexported
// method means no type outside this package — and no third-party tool in another module
// (Go's internal/ rule) — can satisfy it, so the dispatch disposition may trust it as
// "a subprocess whose every reachable invocation is one of Apogee's own hardened reads".
//
// It exists because §4's 2026-07-26 amendment — the unfakeable subprocess marker outranks a
// tool's own ReadOnly() declaration — was written when "a subprocess is unbounded" was true
// of these tools. It no longer is. Every hardening that closed that gap landed afterwards:
// the allowlisted PATH-scoped child environment and the per-invocation switches that
// neutralise the programs a repository names (internal/gitexec, reached through runGit), the
// outright refusal of a repository whose own config names a program git would execute
// (gitexec's repo-local command-config probe), the argv[0] fence, --no-textconv and
// --no-ext-diff on every diff-producing path (gitDiffHardeningArgs), and the two-part ref
// guard (validRef plus looksLikeOption). The engine already runs that same hardened
// read-side git unattended in every mode for its tree-snapshot floor (tools.RunGitQuery,
// internal/agent/treesnapshot.go), so refusing the model the identical read in Plan was a
// rule outliving its reason.
//
// The marker may therefore be minted ONLY for a tool that, on EVERY reachable path:
//
//   - spawns git through runGit alone, so internal/gitexec's hardening options,
//     GIT_CONFIG_NOSYSTEM, the repo-local command-config refusal and the argv[0] fence all
//     apply;
//   - passes gitDiffHardeningArgs on every diff-producing invocation;
//   - validates each ref it accepts with validRef AND looksLikeOption;
//   - writes nothing to the tree, the index or the repository.
//
// A tool that grows a path missing any of those must lose the marker in the same change.
// The marker rides the tool VALUE (a method set), so it survives registry.Subset for free —
// a sub-agent one level down inherits it with no threading (contract §3.4).
//
// It changes classification only: a tool carrying it KEEPS its Subprocess() declaration,
// which still drives the execution mechanics (the scoped environment, the process-group
// teardown, the argv fence). What the marker says is that the ladder should read the call as
// the READ it is — the RO row in every mode, Auto included — rather than as the unbounded
// subprocess row.
type readOnlySubprocess interface {
	domain.Tool

	// readOnlySubprocess is the unexported mint: only a type declared in this package can
	// spell it, which is what makes the marker unfakeable in the way §4 requires of anything
	// the disposition trusts.
	readOnlySubprocess()
}

// IsReadOnlySubprocess reports whether t is one of Apogee's own hardened read-side git tools —
// the signal the ladder keys on to give a subprocess call the read-only row in every mode
// (confinement-execution-contract §4). It is false for every other tool, including a
// write-capable git tool and a read-only subprocess tool that is not one of Apogee's own.
func IsReadOnlySubprocess(t domain.Tool) bool {
	_, ok := t.(readOnlySubprocess)
	return ok
}
