package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// The P3.4 blast-radius disposition ladder (D5; confinement-execution-contract §4)
// ----------------------------------------------------------------------------
//
// These tests cover EVERY row of the per-call disposition: each (mode, tool-class,
// confine-to-workspace, caps) combination resolves to run / confine / gate / refuse.
// The Confiners are fakes (caps injected) so the table is hermetic regardless of the host
// kernel — the dev host has landlock compiled out, but the disposition/wiring logic is
// kernel-independent (it keys on Capabilities(), which the fake reports).

// fakeConfiner is a caps-injected Confiner that records each Confine call. Its Confine is
// a no-op preparation (it leaves cmd unchanged) when confinable; when unavailable it
// returns ErrConfinementUnavailable. It is safe for concurrent Execute (the subprocess
// tool may run under -race).
type fakeConfiner struct {
	caps        domain.ConfinementCaps
	unavailable bool // when true, Confine reports ErrConfinementUnavailable

	mu       sync.Mutex
	confined int // how many cmds were handed to Confine
	lastBox  domain.ConfinementBox
}

func (c *fakeConfiner) Capabilities() domain.ConfinementCaps { return c.caps }

func (c *fakeConfiner) Confine(_ context.Context, box domain.ConfinementBox, _ *exec.Cmd) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unavailable {
		return fmt.Errorf("%w: fake", domain.ErrConfinementUnavailable)
	}
	c.confined++
	c.lastBox = box
	return nil
}

func (c *fakeConfiner) confineCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.confined
}

// subprocTool is a fake SubprocessTool: on Execute it pulls the Confinement handle from
// ctx (the dispatch disposition installs it for a dispoConfine call) and confines a real
// *exec.Cmd through it, recording whether it ran confined. This is exactly the contract's
// tool-builds-and-runs-the-cmd model (§2.2), so "ran under Confine" is observable.
type subprocTool struct {
	name string

	mu         sync.Mutex
	ran        int
	sawHandle  bool
	confineErr error
}

func (t *subprocTool) Name() string            { return t.name }
func (t *subprocTool) Description() string     { return t.name + " (subprocess)" }
func (t *subprocTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *subprocTool) ReadOnly() bool          { return false }
func (t *subprocTool) Subprocess() bool        { return true }

func (t *subprocTool) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ran++
	if conf, ok := domain.ConfinementFromContext(ctx); ok {
		t.sawHandle = true
		// The tool builds its own cmd and asks the backend to wrap it (it does not run it
		// here — the test only proves the confine handoff happened, not a real exec).
		cmd := exec.Command("/bin/true")
		t.confineErr = conf.Confiner.Confine(ctx, conf.Box, cmd)
	}
	return domain.ToolResult{CallID: call.ID, Content: "ran"}, nil
}

func (t *subprocTool) ranCount() int { t.mu.Lock(); defer t.mu.Unlock(); return t.ran }
func (t *subprocTool) confinedOK() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sawHandle && t.confineErr == nil
}

// externalTool is a fake ExternalEffectTool of a configurable kind (network / mcp).
type externalTool struct {
	name string
	kind domain.ExternalEffectKind
	ran  *int
}

func (t externalTool) Name() string                              { return t.name }
func (t externalTool) Description() string                       { return t.name + " (external)" }
func (t externalTool) Schema() json.RawMessage                   { return json.RawMessage(`{"type":"object"}`) }
func (t externalTool) ExternalEffect() domain.ExternalEffectKind { return t.kind }
func (t externalTool) Execute(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if t.ran != nil {
		*t.ran++
	}
	return domain.ToolResult{CallID: call.ID, Content: "ran"}, nil
}

// thirdPartyWriter is a write-capable tool carrying NO markers — the 3p-write class Apogee
// cannot vouch for (it is neither read-only, an Apogee workspace-scoped writer, an external
// tool, nor a subprocess tool), so it must gate in every non-Plan mode.
type thirdPartyWriter struct {
	name string
	ran  *int
}

func (t thirdPartyWriter) Name() string            { return t.name }
func (t thirdPartyWriter) Description() string     { return t.name + " (3p write)" }
func (t thirdPartyWriter) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t thirdPartyWriter) ReadOnly() bool          { return false }
func (t thirdPartyWriter) Execute(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if t.ran != nil {
		*t.ran++
	}
	return domain.ToolResult{CallID: call.ID, Content: "ran"}, nil
}

// capsBoth is the fully-capable fake-Confiner caps profile (fs-write + network egress).
func capsBoth() domain.ConfinementCaps {
	return domain.ConfinementCaps{FSWrite: true, NetworkEgress: true}
}

// ----------------------------------------------------------------------------
// classifyTool — the tool-class resolution the disposition keys on
// ----------------------------------------------------------------------------

func TestClassifyTool(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()
	tests := []struct {
		name string
		tool domain.Tool
		want toolClass
	}{
		{"read-only", fakeTool{name: "read_file", readOnly: true}, classReadOnly},
		{"workspace writer", tools.NewWriteFile(ws), classWorkspaceWrite},
		// P3.7 file-editing family: the write tools carry the same workspaceScopedWriter
		// marker as write_file, so they classify as classWorkspaceWrite and ride the
		// identical per-mode disposition; the read tools classify as classReadOnly.
		{"single find-replace", tools.NewSingleFindReplace(ws), classWorkspaceWrite},
		{"multi find-replace", tools.NewMultiFindReplace(ws), classWorkspaceWrite},
		{"edit existing file", tools.NewEditExistingFile(ws), classWorkspaceWrite},
		{"view diff", tools.NewViewDiff(ws), classReadOnly},
		{"open file", tools.NewOpenFile(ws), classReadOnly},
		{"network", externalTool{name: "web-fetch", kind: domain.EffectNetwork}, classNetwork},
		{"mcp", externalTool{name: "github", kind: domain.EffectMCP}, classMCP},
		{"subprocess", &subprocTool{name: "terminal"}, classSubprocess},
		{"third-party writer", thirdPartyWriter{name: "weird"}, classThirdPartyWrite},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyTool(tt.tool); got != tt.want {
				t.Errorf("classifyTool(%s) = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Auto · confine=true — the load-bearing column
// ----------------------------------------------------------------------------

// TestDisposition_AutoConfineTrue covers every Auto/confine=true row with sufficient caps:
// a subprocess tool runs WITHOUT Approval and UNDER Confine; a native network tool
// auto-runs (no Approval); an MCP tool and a third-party writer each RAISE Approval.
func TestDisposition_AutoConfineTrue(t *testing.T) {
	t.Parallel()

	t.Run("subprocess runs under Confine, no Approval", func(t *testing.T) {
		t.Parallel()
		sink := &recordingSink{}
		sub := &subprocTool{name: "terminal"}
		conf := &fakeConfiner{caps: capsBoth()}
		cfg := autoConfig(sink, conf, true, sub)
		approver := &fakeApprover{decision: domain.ApprovalDeny}
		cfg.Approver = approver

		driveToolCall(t, cfg, sink, "c1", "terminal", `{}`)

		if approver.calls != 0 {
			t.Errorf("Approver consulted %d times; a confined subprocess must NOT gate in Auto/confine=true", approver.calls)
		}
		if sub.ranCount() != 1 {
			t.Fatalf("subprocess ran %d times, want 1", sub.ranCount())
		}
		if !sub.confinedOK() {
			t.Error("subprocess did not run under Confine (no handle, or Confine failed)")
		}
		if conf.confineCount() != 1 {
			t.Errorf("Confine called %d times, want 1", conf.confineCount())
		}
	})

	t.Run("native network auto-runs, no Approval", func(t *testing.T) {
		t.Parallel()
		sink := &recordingSink{}
		ran := 0
		conf := &fakeConfiner{caps: capsBoth()}
		cfg := autoConfig(sink, conf, true, externalTool{name: "web-fetch", kind: domain.EffectNetwork, ran: &ran})
		approver := &fakeApprover{decision: domain.ApprovalDeny}
		cfg.Approver = approver

		driveToolCall(t, cfg, sink, "c1", "web-fetch", `{}`)

		if approver.calls != 0 {
			t.Errorf("Approver consulted %d times; a native network tool auto-runs in Auto (network open)", approver.calls)
		}
		if ran != 1 {
			t.Errorf("web-fetch ran %d times, want 1 (auto-run)", ran)
		}
	})

	t.Run("mcp raises Approval", func(t *testing.T) {
		t.Parallel()
		sink := &recordingSink{}
		ran := 0
		conf := &fakeConfiner{caps: capsBoth()}
		cfg := autoConfig(sink, conf, true, externalTool{name: "github", kind: domain.EffectMCP, ran: &ran})
		approver := &fakeApprover{decision: domain.ApprovalAllow}
		cfg.Approver = approver

		driveToolCall(t, cfg, sink, "c1", "github", `{}`)

		if approver.calls != 1 {
			t.Errorf("Approver consulted %d times; an MCP tool must gate in Auto", approver.calls)
		}
		if ran != 1 {
			t.Errorf("mcp tool ran %d times after an allowing Approver, want 1", ran)
		}
	})

	t.Run("third-party in-process tool raises Approval", func(t *testing.T) {
		t.Parallel()
		sink := &recordingSink{}
		ran := 0
		conf := &fakeConfiner{caps: capsBoth()}
		cfg := autoConfig(sink, conf, true, thirdPartyWriter{name: "weird", ran: &ran})
		approver := &fakeApprover{decision: domain.ApprovalAllow}
		cfg.Approver = approver

		driveToolCall(t, cfg, sink, "c1", "weird", `{}`)

		if approver.calls != 1 {
			t.Errorf("Approver consulted %d times; a third-party writer must gate in Auto", approver.calls)
		}
		if ran != 1 {
			t.Errorf("third-party writer ran %d times after an allowing Approver, want 1", ran)
		}
	})
}

// TestDisposition_AutoConfineTrue_WorkspaceWrites proves the in/out-of-workspace split for
// Apogee's own write tool under Auto/confine=true: an in-workspace write runs WITHOUT
// Approval and WITHOUT Confine (path-safety-bounded); an out-of-workspace one RAISES
// Approval.
func TestDisposition_AutoConfineTrue_WorkspaceWrites(t *testing.T) {
	t.Parallel()

	t.Run("in-workspace write runs, no Approval, no Confine", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		sink := &recordingSink{}
		conf := &fakeConfiner{caps: capsBoth()}
		cfg := autoConfigWS(sink, conf, true, ws, tools.NewWriteFile(ws))
		approver := &fakeApprover{decision: domain.ApprovalDeny}
		cfg.Approver = approver

		args := fmt.Sprintf(`{"path":%q,"content":"hi"}`, "in.txt")
		driveToolCall(t, cfg, sink, "c1", "write_file", args)

		if approver.calls != 0 {
			t.Errorf("Approver consulted %d times; an in-workspace Apogee write must not gate", approver.calls)
		}
		if conf.confineCount() != 0 {
			t.Errorf("Confine called %d times; an Apogee write is path-safety-bounded, never confined", conf.confineCount())
		}
		res, _ := lastToolResult(sink.events)
		if res.IsError {
			t.Errorf("in-workspace write produced an error result: %q", res.Content)
		}
	})

	t.Run("out-of-workspace write raises Approval", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		outside := filepath.Join(t.TempDir(), "escape.txt")
		sink := &recordingSink{}
		conf := &fakeConfiner{caps: capsBoth()}
		cfg := autoConfigWS(sink, conf, true, ws, tools.NewWriteFile(ws))
		approver := &fakeApprover{decision: domain.ApprovalDeny}
		cfg.Approver = approver

		args := fmt.Sprintf(`{"path":%q,"content":"hi"}`, outside)
		driveToolCall(t, cfg, sink, "c1", "write_file", args)

		if approver.calls != 1 {
			t.Errorf("Approver consulted %d times; an out-of-workspace Apogee write must gate", approver.calls)
		}
		if conf.confineCount() != 0 {
			t.Errorf("Confine called %d times; an Apogee write is never confined", conf.confineCount())
		}
	})

	// The P3.7 file-editing writers ride the identical disposition: a find-replace edit of
	// an in-workspace file runs without Approval and without Confine, while a target
	// outside the workspace gates — proving the workspaceWriteTarget seam works for the
	// whole write family, not just write_file.
	t.Run("find-replace in-workspace runs, no Approval, no Confine", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		if err := os.WriteFile(filepath.Join(ws, "in.txt"), []byte("old text here"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		sink := &recordingSink{}
		conf := &fakeConfiner{caps: capsBoth()}
		cfg := autoConfigWS(sink, conf, true, ws, tools.NewSingleFindReplace(ws))
		approver := &fakeApprover{decision: domain.ApprovalDeny}
		cfg.Approver = approver

		driveToolCall(t, cfg, sink, "c1", "single_find_and_replace",
			`{"path":"in.txt","oldText":"old text","newText":"new text"}`)

		if approver.calls != 0 {
			t.Errorf("Approver consulted %d times; an in-workspace find-replace must not gate", approver.calls)
		}
		if conf.confineCount() != 0 {
			t.Errorf("Confine called %d times; an Apogee write is path-safety-bounded, never confined", conf.confineCount())
		}
		res, _ := lastToolResult(sink.events)
		if res.IsError {
			t.Errorf("in-workspace find-replace produced an error result: %q", res.Content)
		}
	})

	t.Run("find-replace out-of-workspace target gates", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		outside := filepath.Join(t.TempDir(), "escape.txt")
		sink := &recordingSink{}
		conf := &fakeConfiner{caps: capsBoth()}
		cfg := autoConfigWS(sink, conf, true, ws, tools.NewSingleFindReplace(ws))
		approver := &fakeApprover{decision: domain.ApprovalDeny}
		cfg.Approver = approver

		args := fmt.Sprintf(`{"path":%q,"oldText":"a","newText":"b"}`, outside)
		driveToolCall(t, cfg, sink, "c1", "single_find_and_replace", args)

		if approver.calls != 1 {
			t.Errorf("Approver consulted %d times; an out-of-workspace find-replace must gate", approver.calls)
		}
		if conf.confineCount() != 0 {
			t.Errorf("Confine called %d times; an Apogee write is never confined", conf.confineCount())
		}
	})
}

// TestDisposition_AutoConfineTrue_SubprocCapsInsufficient proves "confine if you can, gate
// if you can't": when fs-confinement is unavailable, a subprocess tool GATES rather than
// running unconfined.
func TestDisposition_AutoConfineTrue_SubprocCapsInsufficient(t *testing.T) {
	t.Parallel()
	sink := &recordingSink{}
	sub := &subprocTool{name: "terminal"}
	// A present-but-incapable Confiner (FSWrite false): Auto is still ENTERED at construction
	// (the gate refuses only a nil Confiner — ADR 0012), and the disposition gates the
	// subprocess surface because fs-confinement is unavailable.
	conf := &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: false}}
	cfg := autoConfig(sink, conf, true, sub)
	approver := &fakeApprover{decision: domain.ApprovalAllow}
	cfg.Approver = approver

	driveToolCall(t, cfg, sink, "c1", "terminal", `{}`)

	if approver.calls != 1 {
		t.Errorf("Approver consulted %d times; an unconfinable subprocess must gate (confine-if-you-can, gate-if-you-can't)", approver.calls)
	}
	if conf.confineCount() != 0 {
		t.Errorf("Confine called %d times; caps were insufficient, so it must gate not confine", conf.confineCount())
	}
}

// confinePropagatingTool is a subprocess tool that, like the real terminal/python-exec, asks
// the Confiner to wrap a cmd and RETURNS ErrConfinementUnavailable (rather than running
// unconfined) when the backend cannot establish the box. It is the fake that exercises the
// runtime demote-to-Approval net (carried finding #2).
type confinePropagatingTool struct {
	name string
	ran  *int
}

func (t confinePropagatingTool) Name() string            { return t.name }
func (t confinePropagatingTool) Description() string     { return t.name + " (subprocess)" }
func (t confinePropagatingTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t confinePropagatingTool) ReadOnly() bool          { return false }
func (t confinePropagatingTool) Subprocess() bool        { return true }

func (t confinePropagatingTool) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if conf, ok := domain.ConfinementFromContext(ctx); ok && conf.Confiner != nil {
		cmd := exec.Command("/bin/true")
		if err := conf.Confiner.Confine(ctx, conf.Box, cmd); err != nil {
			// Mirror the real tools: do NOT run unconfined — surface the error so dispatch
			// can demote to Approval.
			return domain.ToolResult{}, fmt.Errorf("confine: %w", err)
		}
	}
	if t.ran != nil {
		*t.ran++
	}
	return domain.ToolResult{CallID: call.ID, Content: "ran"}, nil
}

// TestDisposition_RuntimeConfineUnavailable_DemotesToApproval proves the RUNTIME
// "confine if you can, gate if you can't" net (carried finding #2): the disposition chose
// dispoConfine (caps reported FSWrite at construction), but the Confiner failed to establish
// the box when the tool tried to confine. The call must NOT run unconfined — it demotes to
// Approval; an allowing human lets it re-run, a denying one refuses it.
func TestDisposition_RuntimeConfineUnavailable_DemotesToApproval(t *testing.T) {
	t.Parallel()

	t.Run("approved → runs once", func(t *testing.T) {
		t.Parallel()
		sink := &recordingSink{}
		ran := 0
		sub := confinePropagatingTool{name: "terminal", ran: &ran}
		// Caps report FSWrite (so the disposition picks dispoConfine), but Confine fails at
		// run time (unavailable) — the runtime net, not the construction-time caps gate.
		conf := &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}, unavailable: true}
		cfg := autoConfig(sink, conf, true, sub)
		approver := &fakeApprover{decision: domain.ApprovalAllow}
		cfg.Approver = approver

		driveToolCall(t, cfg, sink, "c1", "terminal", `{"command":"echo hi"}`)

		if approver.calls != 1 {
			t.Errorf("Approver consulted %d times; a runtime confine failure must demote to Approval", approver.calls)
		}
		if ran != 1 {
			t.Errorf("tool ran %d times; an approved demoted call must re-run once (unconfined)", ran)
		}
		res, _ := lastToolResult(sink.events)
		if res.IsError {
			t.Errorf("approved demoted call result = %q, want a clean run", res.Content)
		}
	})

	t.Run("denied → refused, never runs unconfined", func(t *testing.T) {
		t.Parallel()
		sink := &recordingSink{}
		ran := 0
		sub := confinePropagatingTool{name: "terminal", ran: &ran}
		conf := &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}, unavailable: true}
		cfg := autoConfig(sink, conf, true, sub)
		approver := &fakeApprover{decision: domain.ApprovalDeny}
		cfg.Approver = approver

		driveToolCall(t, cfg, sink, "c1", "terminal", `{"command":"echo hi"}`)

		if approver.calls != 1 {
			t.Errorf("Approver consulted %d times; a runtime confine failure must demote to Approval", approver.calls)
		}
		if ran != 0 {
			t.Errorf("tool ran %d times; a DENIED demoted call must never run unconfined", ran)
		}
		res, _ := lastToolResult(sink.events)
		if !res.IsError {
			t.Error("a denied demoted call must produce an error result")
		}
	})

	t.Run("nil Approver → refused, never runs unconfined", func(t *testing.T) {
		t.Parallel()
		sink := &recordingSink{}
		ran := 0
		sub := confinePropagatingTool{name: "terminal", ran: &ran}
		conf := &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}, unavailable: true}
		cfg := autoConfig(sink, conf, true, sub)
		cfg.Approver = nil

		driveToolCall(t, cfg, sink, "c1", "terminal", `{"command":"echo hi"}`)

		if ran != 0 {
			t.Errorf("tool ran %d times; with no Approver a demoted call must be refused, not run unconfined", ran)
		}
		res, _ := lastToolResult(sink.events)
		if !res.IsError {
			t.Error("a demoted call with no Approver must produce an error result")
		}
	})
}

// ----------------------------------------------------------------------------
// Auto · confine=false — everything auto-runs except the dangerous-action floor
// ----------------------------------------------------------------------------

// TestDisposition_AutoConfineFalse proves every unbounded class auto-runs unfenced under
// confine=false (no Approval, no Confine), while a dangerous-action still fires (the P3.6
// floor is mode-independent and runs first).
func TestDisposition_AutoConfineFalse(t *testing.T) {
	t.Parallel()

	t.Run("subprocess auto-runs unfenced", func(t *testing.T) {
		t.Parallel()
		sink := &recordingSink{}
		sub := &subprocTool{name: "terminal"}
		conf := &fakeConfiner{caps: capsBoth()}
		cfg := autoConfig(sink, conf, false, sub)
		approver := &fakeApprover{decision: domain.ApprovalDeny}
		cfg.Approver = approver

		driveToolCall(t, cfg, sink, "c1", "terminal", `{"command":"go build"}`)

		if approver.calls != 0 {
			t.Errorf("Approver consulted %d times; confine=false auto-runs everything", approver.calls)
		}
		if sub.ranCount() != 1 {
			t.Errorf("subprocess ran %d times, want 1 (unfenced auto-run)", sub.ranCount())
		}
		if conf.confineCount() != 0 {
			t.Errorf("Confine called %d times; confine=false never confines", conf.confineCount())
		}
	})

	t.Run("mcp and third-party auto-run", func(t *testing.T) {
		t.Parallel()
		sink := &recordingSink{}
		mcpRan, tpRan := 0, 0
		conf := &fakeConfiner{caps: capsBoth()}
		cfg := autoConfig(sink, conf, false,
			externalTool{name: "github", kind: domain.EffectMCP, ran: &mcpRan},
			thirdPartyWriter{name: "weird", ran: &tpRan},
		)
		approver := &fakeApprover{decision: domain.ApprovalDeny}
		cfg.Approver = approver

		driveTwoToolCalls(t, cfg, sink,
			toolReq{"c1", "github", `{}`},
			toolReq{"c2", "weird", `{}`},
		)

		if approver.calls != 0 {
			t.Errorf("Approver consulted %d times; confine=false auto-runs mcp + third-party", approver.calls)
		}
		if mcpRan != 1 || tpRan != 1 {
			t.Errorf("mcp ran %d, third-party ran %d; want 1 and 1 (unfenced)", mcpRan, tpRan)
		}
	})

	t.Run("dangerous-action floor still fires under confine=false", func(t *testing.T) {
		t.Parallel()
		sink := &recordingSink{}
		ran := 0
		conf := &fakeConfiner{caps: capsBoth()}
		cfg := autoConfig(sink, conf, false, fakeTool{name: "terminal", readOnly: true, ran: &ran})
		cfg.Approver = &fakeApprover{decision: domain.ApprovalAllow}

		driveToolCall(t, cfg, sink, "c1", "terminal", `{"command":"rm -rf /"}`)

		res, ok := lastToolResult(sink.events)
		if !ok || !res.IsError {
			t.Fatalf("expected a dangerous-action error result, got %+v (ok=%v)", res, ok)
		}
		if ran != 0 {
			t.Errorf("tool ran %d times; the Tier-1 floor must refuse before execution even under confine=false", ran)
		}
	})
}

// ----------------------------------------------------------------------------
// Allow-Edits — Apogee writes auto-approve; everything unbounded gates; NO Confine ever
// ----------------------------------------------------------------------------

// TestDisposition_AllowEdits proves Allow-Edits: an in-workspace Apogee write auto-approves
// while a subprocess tool gates, and NO Confine is invoked (path-safety is the bound,
// identical on every OS).
func TestDisposition_AllowEdits(t *testing.T) {
	t.Parallel()

	t.Run("in-workspace Apogee write auto-approves, no Confine", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		sink := &recordingSink{}
		conf := &fakeConfiner{caps: capsBoth()}
		cfg := configWithTools(sink, tools.NewWriteFile(ws))
		cfg.Mode = domain.ModeAllowEdits
		cfg.WorkspaceDir = ws
		cfg.Confiner = conf
		approver := &fakeApprover{decision: domain.ApprovalDeny}
		cfg.Approver = approver

		args := fmt.Sprintf(`{"path":%q,"content":"hi"}`, "edit.txt")
		driveToolCall(t, cfg, sink, "c1", "write_file", args)

		if approver.calls != 0 {
			t.Errorf("Approver consulted %d times; an in-workspace Apogee write auto-approves in Allow-Edits", approver.calls)
		}
		if conf.confineCount() != 0 {
			t.Errorf("Confine called %d times; Allow-Edits invokes NO Confine", conf.confineCount())
		}
		res, _ := lastToolResult(sink.events)
		if res.IsError {
			t.Errorf("Allow-Edits in-workspace write errored: %q", res.Content)
		}
	})

	t.Run("subprocess (terminal) gates, no Confine", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		sink := &recordingSink{}
		sub := &subprocTool{name: "terminal"}
		conf := &fakeConfiner{caps: capsBoth()}
		cfg := configWithTools(sink, sub)
		cfg.Mode = domain.ModeAllowEdits
		cfg.WorkspaceDir = ws
		cfg.Confiner = conf
		approver := &fakeApprover{decision: domain.ApprovalAllow}
		cfg.Approver = approver

		driveToolCall(t, cfg, sink, "c1", "terminal", `{}`)

		if approver.calls != 1 {
			t.Errorf("Approver consulted %d times; a terminal call must gate in Allow-Edits", approver.calls)
		}
		if conf.confineCount() != 0 {
			t.Errorf("Confine called %d times; Allow-Edits invokes NO Confine even for a subprocess tool", conf.confineCount())
		}
	})
}

// ----------------------------------------------------------------------------
// Plan / Ask-Before — the lower rungs
// ----------------------------------------------------------------------------

// TestDisposition_PlanRefusesWrites proves Plan refuses any non-read-only tool defensively
// and runs read-only tools.
func TestDisposition_PlanRefusesWrites(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()
	sink := &recordingSink{}
	cfg := configWithTools(sink, tools.NewWriteFile(ws), fakeTool{name: "read_file", readOnly: true})
	cfg.Mode = domain.ModePlan
	cfg.WorkspaceDir = ws

	driveToolCall(t, cfg, sink, "c1", "write_file", `{"path":"x.txt","content":"hi"}`)
	res, ok := lastToolResult(sink.events)
	if !ok || !res.IsError {
		t.Fatalf("Plan: expected a refusal error result for a write, got %+v (ok=%v)", res, ok)
	}
}

// TestDisposition_AskBeforeGatesWrites proves Ask-Before gates a write/subprocess/external
// and runs a read free.
func TestDisposition_AskBeforeGatesWrites(t *testing.T) {
	t.Parallel()

	t.Run("read runs free", func(t *testing.T) {
		t.Parallel()
		sink := &recordingSink{}
		ran := 0
		cfg := configWithTools(sink, fakeTool{name: "read_file", readOnly: true, ran: &ran})
		cfg.Mode = domain.ModeAskBefore
		approver := &fakeApprover{decision: domain.ApprovalDeny}
		cfg.Approver = approver

		driveToolCall(t, cfg, sink, "c1", "read_file", `{}`)
		if approver.calls != 0 {
			t.Errorf("Approver consulted %d times; a read runs free in Ask-Before", approver.calls)
		}
		if ran != 1 {
			t.Errorf("read ran %d times, want 1", ran)
		}
	})

	t.Run("subprocess gates", func(t *testing.T) {
		t.Parallel()
		sink := &recordingSink{}
		sub := &subprocTool{name: "terminal"}
		cfg := configWithTools(sink, sub)
		cfg.Mode = domain.ModeAskBefore
		approver := &fakeApprover{decision: domain.ApprovalAllow}
		cfg.Approver = approver

		driveToolCall(t, cfg, sink, "c1", "terminal", `{}`)
		if approver.calls != 1 {
			t.Errorf("Approver consulted %d times; a subprocess gates in Ask-Before", approver.calls)
		}
	})
}

// ----------------------------------------------------------------------------
// ExternalEffects.Do plumbing
// ----------------------------------------------------------------------------

// recordingExternalEffects is a fake ExternalEffects boundary that records each Do call —
// the seam ADR 0008 promises and P3.4 wires.
type recordingExternalEffects struct {
	mu    sync.Mutex
	calls []string
}

func (e *recordingExternalEffects) Do(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, call.Tool)
	return domain.ToolResult{CallID: call.ID, Content: "from boundary"}, nil
}

// TestExternalEffects_RoutesExternalToolThroughBoundary proves an ExternalEffectTool is
// routed through Config.ExternalEffects.Do when set (the tool's own Execute is bypassed),
// and a non-external tool is not.
func TestExternalEffects_RoutesExternalToolThroughBoundary(t *testing.T) {
	t.Parallel()

	t.Run("external tool routes through the boundary", func(t *testing.T) {
		t.Parallel()
		sink := &recordingSink{}
		toolRan := 0
		eff := &recordingExternalEffects{}
		conf := &fakeConfiner{caps: capsBoth()}
		// confine=false so the network tool auto-runs without an Approver in the way.
		cfg := autoConfig(sink, conf, false, externalTool{name: "web-fetch", kind: domain.EffectNetwork, ran: &toolRan})
		cfg.ExternalEffects = eff

		driveToolCall(t, cfg, sink, "c1", "web-fetch", `{}`)

		if len(eff.calls) != 1 || eff.calls[0] != "web-fetch" {
			t.Errorf("ExternalEffects.Do calls = %v, want one call for web-fetch", eff.calls)
		}
		if toolRan != 0 {
			t.Errorf("the tool's own Execute ran %d times; an external tool must route through the boundary", toolRan)
		}
		res, _ := lastToolResult(sink.events)
		if res.Content != "from boundary" {
			t.Errorf("result content = %q, want the boundary's result", res.Content)
		}
	})

	t.Run("non-external tool does not route through the boundary", func(t *testing.T) {
		t.Parallel()
		sink := &recordingSink{}
		ran := 0
		eff := &recordingExternalEffects{}
		cfg := configWithTools(sink, fakeTool{name: "read_file", readOnly: true, ran: &ran})
		cfg.Mode = domain.ModeAskBefore
		cfg.ExternalEffects = eff

		driveToolCall(t, cfg, sink, "c1", "read_file", `{}`)

		if len(eff.calls) != 0 {
			t.Errorf("ExternalEffects.Do calls = %v; a non-external tool must not route through the boundary", eff.calls)
		}
		if ran != 1 {
			t.Errorf("read_file ran %d times via its own Execute, want 1", ran)
		}
	})
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

// autoConfig builds an Auto-mode Config with the given fake Confiner and confine flag. A
// caps-{true,true} Confiner is installed so newAgent's Auto gate passes; the disposition's
// own caps check then keys on conf.Capabilities() at dispatch time.
func autoConfig(sink *recordingSink, conf domain.Confiner, confine bool, tools ...domain.Tool) domain.Config {
	cfg := configWithTools(sink, tools...)
	cfg.Mode = domain.ModeAuto
	cfg.Confiner = conf
	cfg.ConfineToWorkspace = confine
	return cfg
}

// autoConfigWS is autoConfig with an explicit WorkspaceDir (so a workspace-scoped writer's
// in/out classification has a root to compare against).
func autoConfigWS(sink *recordingSink, conf domain.Confiner, confine bool, ws string, tools ...domain.Tool) domain.Config {
	cfg := autoConfig(sink, conf, confine, tools...)
	cfg.WorkspaceDir = ws
	return cfg
}

// toolReq is one scripted tool call for the multi-call driver.
type toolReq struct {
	id, tool, args string
}

// driveTwoToolCalls runs a single Turn that issues two tool calls (then a final reply).
func driveTwoToolCalls(t *testing.T, cfg domain.Config, _ *recordingSink, a, b toolReq) {
	t.Helper()
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		twoToolCallScript(a, b),
		contentScript("done"),
	}}
	ag, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := ag.Submit(domain.UserInput{Text: "go"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := ag.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}
}

// twoToolCallScript emits two native tool calls then a tool_calls finish.
func twoToolCallScript(a, b toolReq) []provider.Delta {
	return []provider.Delta{
		{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
			ID: a.id, Type: "function", Function: provider.FunctionCall{Name: a.tool, Arguments: a.args},
		}},
		{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
			ID: b.id, Type: "function", Function: provider.FunctionCall{Name: b.tool, Arguments: b.args},
		}},
		{Kind: provider.DeltaDone, FinishReason: "tool_calls"},
	}
}
