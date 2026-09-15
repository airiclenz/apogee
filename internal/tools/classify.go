package tools

import "github.com/airiclenz/apogee/internal/domain"

// ----------------------------------------------------------------------------
// Classify — the blast-radius class the autonomy ladder keys on
// (confinement-execution-contract §4; ADR 0012)
// ----------------------------------------------------------------------------
//
// Classify sits in THIS package, beside the unfakeable markers it reads, rather than in
// internal/agent beside the ladder that consumes it (moved 2026-09-15; it was classifyTool in
// internal/agent/resolution.go). The classes are the vocabulary a tool's blast radius is spoken
// in, the markers are minted here, and the ladder is the ONE consumer — so the class travels
// out of this package as a value and the markers themselves stay unexported (contract §3.5):
// nothing outside internal/tools can spell workspaceScopedWriter, urlFilteredNetworker or
// readOnlySubprocess, which is what makes the class trustworthy.
//
// The load-bearing part is the CHECK ORDER, the table below. Every marker that is unfakeable by
// construction is consulted before the bare self-declarations, and ClassReadOnly is the TERMINAL
// FLOOR, reached only by a tool no marker claimed. A tool that carries neither a marker nor a
// read-only declaration is a third-party in-process writer.
//
//	order | predicate                                            | class
//	------+------------------------------------------------------+------------------------
//	  1   | IsWorkspaceScopedWriter (unexported marker)          | ClassWorkspaceWrite
//	  2   | ExternalEffectTool of kind network, url-filtered     | ClassNetwork
//	      | (unexported marker, IsURLFilteredNetworker)          |
//	  3   | ExternalEffectTool of kind network, no url-filter    | ClassThirdPartyNetwork
//	  4   | ExternalEffectTool of any other kind (mcp)           | ClassMCP
//	  5   | IsReadOnlySubprocess (unexported marker)             | ClassReadOnlySubprocess
//	  6   | domain.IsSubprocessTool (self-declaration)           | ClassSubprocess
//	  7   | domain.IsReadOnly (self-declaration)                 | ClassReadOnly
//	  8   | none of the above                                    | ClassThirdPartyWrite
//
// Why the order is the invariant. ReadOnly() is a bare SELF-DECLARATION, so it can never outrank
// a structural fact about what the tool does (ADR 0012 Amendment 2026-07-25(a): classification
// keys on the marker). A tool declaring itself read-only that also launches an OS subprocess
// Apogee cannot vouch for (diagnostics) is classified by the subprocess it launches and is
// confined/gated accordingly; one declaring itself read-only that also reaches the network takes
// a network class and is url-filtered or gated. Otherwise a call could be both unsupervised and
// unbounded — the one thing ADR 0012's core invariant forbids. The declaration keeps its own job:
// it decides the floor for the tools no marker claims (read_file, grep, view_diff, list_dir,
// ask_user). It is no longer what Plan mode's menu filter reads (2026-08-02): the menu keys on
// this class through the ladder's planAdmits, because a filter on the bare declaration offered
// diagnostics in Plan and the ladder then refused it.
//
// ClassReadOnlySubprocess is read-only BY CONSTRUCTION and a subprocess only by MECHANISM: the
// hardened git read set (git_status, git_log, git_diff_range, git_show) builds every argv itself,
// runs with hooks/fsmonitor off, refuses repo-local program keys and passes no user string to a
// shell, so the "a subprocess is unbounded" premise that puts ClassSubprocess behind a
// confinement box does not hold for it (contract §4 amendment 2026-09-06). It is row 5 — AFTER
// the workspace-write and external-effect markers and BEFORE the bare subprocess declaration —
// so a tool that also writes the workspace or reaches the network still takes the outranking
// class: the marker narrows a subprocess call, it never widens one.
//
// The network kind splits on the url-filter marker (rows 2 and 3): an EffectNetwork tool that
// routes through this package's network funnel is ClassNetwork (Apogee vouches that every
// outbound URL passed the host's URLGuard, so it auto-runs in Auto); one WITHOUT the marker is
// ClassThirdPartyNetwork — its URLs are unfiltered, so it gates instead of reaching the network
// unattended (ADR 0012 Amendment 2026-07-25, the network analogue of ClassThirdPartyWrite).

// ToolClass is the blast-radius class the autonomy ladder keys on
// (confinement-execution-contract §4). Classify decides which one a tool takes; the classes
// themselves are just the list, and the load-bearing part is Classify's check order.
type ToolClass int

const (
	ClassReadOnly           ToolClass = iota // IsReadOnly and NO other marker (the terminal floor)
	ClassReadOnlySubprocess                  // readOnlySubprocess marker (Apogee's own hardened git reads)
	ClassWorkspaceWrite                      // workspaceScopedWriter marker (Apogee's own write)
	ClassNetwork                             // network + urlFilteredNetworker marker (Apogee's own)
	ClassThirdPartyNetwork                   // network, no url-filter marker (unfiltered URLs — gates)
	ClassMCP                                 // ExternalEffectTool, kind mcp
	ClassSubprocess                          // SubprocessTool (shell/exec; OS-confinable)
	ClassThirdPartyWrite                     // write-capable, none of the above (can't vouch for scoping)
)

// toolClassNames spells each class for String — the contract's row names (§4), so a test
// failure and a table read the same way the ladder is documented.
var toolClassNames = [...]string{
	ClassReadOnly:           "RO",
	ClassReadOnlySubprocess: "RO-subproc",
	ClassWorkspaceWrite:     "WS-write",
	ClassNetwork:            "net",
	ClassThirdPartyNetwork:  "3p-net",
	ClassMCP:                "mcp",
	ClassSubprocess:         "subproc",
	ClassThirdPartyWrite:    "3p-write",
}

// String spells the class the way the contract's §4 ladder table names its rows.
func (c ToolClass) String() string {
	if c < 0 || int(c) >= len(toolClassNames) {
		return "ToolClass(?)"
	}
	return toolClassNames[c]
}

// Classify maps a tool onto its blast-radius class by the table in this file's header: the
// unfakeable markers first, the self-declarations after, ClassReadOnly as the terminal floor.
func Classify(tool domain.Tool) ToolClass {
	if IsWorkspaceScopedWriter(tool) {
		return ClassWorkspaceWrite
	}
	if ext, ok := tool.(domain.ExternalEffectTool); ok {
		if ext.ExternalEffect() == domain.EffectNetwork {
			if IsURLFilteredNetworker(tool) {
				return ClassNetwork
			}
			return ClassThirdPartyNetwork
		}
		return ClassMCP
	}
	if IsReadOnlySubprocess(tool) {
		return ClassReadOnlySubprocess
	}
	if domain.IsSubprocessTool(tool) {
		return ClassSubprocess
	}
	if domain.IsReadOnly(tool) {
		return ClassReadOnly
	}
	return ClassThirdPartyWrite
}
