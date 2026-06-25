package agent

import (
	"path/filepath"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// The per-call blast-radius disposition (D5; confinement-execution-contract §4)
// ----------------------------------------------------------------------------
//
// disposition is the single place the autonomy ladder and the blast-radius invariant
// become code: keyed on (mode, tool-class, confine-to-workspace, backend-caps), it
// decides whether a tool call runs directly, runs OS-confined, gates through Approval,
// or is refused. The dangerous-action guard runs FIRST (P3.6, tighten-only) and is not
// part of this table; disposition is what runs after it clears.

// disposition is the outcome of the blast-radius decision for one tool call.
type disposition int

const (
	// dispoRun executes the call directly — no Approval, no Confine.
	dispoRun disposition = iota
	// dispoConfine executes the call's subprocess inside Confiner.Confine — no Approval.
	dispoConfine
	// dispoGate routes the call through Approval (allow-for-session caches apply).
	dispoGate
	// dispoRefuse refuses the call outright (Plan-mode write refusal).
	dispoRefuse
)

// classify returns the tool-class the disposition keys on (confinement-execution-contract
// §4). The order is significant: read-only wins (a read never gates), then the unfakeable
// workspace-scoped-writer marker, then the external-effect kinds, then the subprocess
// marker, and finally a write-capable tool Apogee cannot vouch for (3p-write).
type toolClass int

const (
	classReadOnly        toolClass = iota // IsReadOnly
	classWorkspaceWrite                   // workspaceScopedWriter marker (Apogee's own write)
	classNetwork                          // ExternalEffectTool, kind network
	classMCP                              // ExternalEffectTool, kind mcp
	classSubprocess                       // SubprocessTool (shell/exec; OS-confinable)
	classThirdPartyWrite                  // write-capable, none of the above (can't vouch for scoping)
)

// classifyTool maps a tool onto its blast-radius class. The classes are checked in a
// fixed priority so a tool implementing several markers resolves deterministically:
// read-only first (harmless), then Apogee's own writer, then the external kinds, then the
// confinable subprocess surface, else a third-party in-process writer.
func classifyTool(tool domain.Tool) toolClass {
	if domain.IsReadOnly(tool) {
		return classReadOnly
	}
	if tools.IsWorkspaceScopedWriter(tool) {
		return classWorkspaceWrite
	}
	if ext, ok := tool.(domain.ExternalEffectTool); ok {
		if ext.ExternalEffect() == domain.EffectNetwork {
			return classNetwork
		}
		return classMCP
	}
	if domain.IsSubprocessTool(tool) {
		return classSubprocess
	}
	return classThirdPartyWrite
}

// dispose computes the disposition for one tool call under the Agent's mode, the
// confine-to-workspace flag, and the Confiner's capabilities (confinement-execution-
// contract §4). It is pure given those inputs and the call, so the ladder table is a
// table test (dispatch_test.go).
func (a *Agent) dispose(tool domain.Tool, call domain.ToolCall) disposition {
	class := classifyTool(tool)

	switch a.Mode() {
	case domain.ModePlan:
		// Plan filters the menu to read-only tools; anything else is refused defensively.
		if class == classReadOnly {
			return dispoRun
		}
		return dispoRefuse

	case domain.ModeAllowEdits:
		// Apogee's own workspace-scoped writes auto-approve when the target is in-workspace;
		// everything unbounded (and any out-of-workspace write) gates. NO Confine is ever
		// invoked here — path-safety is the bound, identical on every OS (ADR 0012 D5).
		if class == classReadOnly {
			return dispoRun
		}
		if class == classWorkspaceWrite && a.writeTargetInWorkspace(tool, call) {
			return dispoRun
		}
		return dispoGate

	case domain.ModeAuto:
		return a.disposeAuto(class, tool, call)

	default:
		// An empty / unknown mode is treated as Ask-Before — the safe default that gates
		// every write/exec/external and runs only harmless reads.
		if class == classReadOnly {
			return dispoRun
		}
		return dispoGate
	}
}

// disposeAuto computes the Auto-mode disposition, tuned by confine-to-workspace
// (confinement-execution-contract §4, the load-bearing column).
func (a *Agent) disposeAuto(class toolClass, tool domain.Tool, call domain.ToolCall) disposition {
	if !a.cfg.ConfineToWorkspace {
		// "I am the sandbox" (VM-only): everything auto-runs unfenced. The dangerous-action
		// floor already ran (and may have forced approval / refused) before this point.
		return dispoRun
	}

	// confine-to-workspace = true (the default).
	switch class {
	case classReadOnly:
		return dispoRun
	case classWorkspaceWrite:
		// An in-workspace Apogee write runs path-safety-bounded (no Confine, no Approval);
		// an out-of-workspace one raises Approval (Apogee can inspect the path, so it asks).
		if a.writeTargetInWorkspace(tool, call) {
			return dispoRun
		}
		return dispoGate
	case classNetwork:
		// Native network tools auto-run url-filtered — the network is open (ADR 0012).
		return dispoRun
	case classMCP:
		// MCP executes in a server Apogee cannot fence: gate (server-grain allow-for-session).
		return dispoGate
	case classSubprocess:
		// Subprocess surface: confine if the backend can, else gate ("confine if you can,
		// gate if you can't"). The caps check is what the contract requires before Confine.
		if a.fsConfinementAvailable() {
			return dispoConfine
		}
		return dispoGate
	default: // classThirdPartyWrite
		// A write-capable tool Apogee cannot vouch for: gate.
		return dispoGate
	}
}

// writeTargetInWorkspace reports whether a workspace-scoped writer's call targets a path
// inside the workspace root. A call with no inspectable target (ok==false) is treated as
// in-bounds (the disposition then runs it, path-safety bounding it at Execute). A tool
// that is not a workspace-scoped writer is never in-workspace by this seam.
func (a *Agent) writeTargetInWorkspace(tool domain.Tool, call domain.ToolCall) bool {
	abs, ok := tools.WorkspaceWriteTarget(tool, call)
	if !ok {
		return true // nothing inspectable to classify ⇒ treat as in-bounds (Execute path-bounds it)
	}
	return pathWithin(abs, a.cfg.WorkspaceDir)
}

// fsConfinementAvailable reports whether the injected Confiner can enforce filesystem
// confinement on this host — the caps gate the disposition checks before choosing to
// confine a subprocess tool (confinement-execution-contract §4/§5).
func (a *Agent) fsConfinementAvailable() bool {
	return a.cfg.Confiner != nil && a.cfg.Confiner.Capabilities().FSWrite
}

// pathWithin reports whether abs (an already-resolved real path) is the workspace root or
// lives beneath it, resolving the root through symlinks the same way the write tool's
// target resolver does so the two agree (e.g. macOS /tmp). An empty root cannot contain
// anything, so a write is treated as out-of-workspace — the safe default that gates.
func pathWithin(abs, root string) bool {
	if root == "" {
		return false
	}
	realRoot := security.EvalRealPath(filepath.Clean(root))
	if abs == realRoot {
		return true
	}
	return strings.HasPrefix(abs, realRoot+string(filepath.Separator))
}
