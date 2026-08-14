package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// The write-escape permit's minting seam (ADR 0049)
// ----------------------------------------------------------------------------
//
// A write that lands outside the workspace is decided by the ladder and executed by the shared
// write funnel, and until ADR 0049 the two disagreed: the Gate said yes and the funnel — pinned
// to the workspace root and nothing else — refused the very write the human had just read and
// approved. The permit is what carries the ladder's answer to the funnel, and dispatch is the
// only thing that mints it.
//
// These tests pin BOTH halves of that: the verdict resolve() computes (which cells authorise an
// escape at all) and the context the executor hands the tool (that the authorisation actually
// arrives, naming the disclosed target and nothing else). The second half needs a tool that both
// classifies as one of Apogee's own workspace-scoped writers — a marker no test can fake, by
// design — and reports what it was handed, so escapeProbe embeds the real write_file and
// overrides only its Execute.

// escapeProbe is write_file with an instrumented Execute: it classifies exactly as write_file
// does (the unexported workspaceScopedWriter marker is promoted through the embedded value) and
// records the write-escape permit its execution context carried, without writing anything. Item
// 4 of the plan is what makes the tools THEMSELVES honour the permit; this seam only has to hand
// it over.
type escapeProbe struct {
	*tools.WriteFile
	ran     bool
	granted bool
	target  string
}

func (p *escapeProbe) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	permit, granted := domain.WriteEscapePermitFrom(ctx)
	p.ran, p.granted, p.target = true, granted, permit.Real
	return domain.ToolResult{CallID: call.ID, Content: "probed"}, nil
}

// TestWriteEscapeVerdicts pins WHICH verdicts authorise a write outside the workspace root, from
// the pure resolver: the two ADR 0049 minters (an approved Gate, a Run the ladder already
// answered for) and nothing else. writeTargetInWorkspace is the fence-at-classification bool
// dispatch precomputes (workspace root ∪ the box's writable paths) and writeEscapeTarget the
// resolved path that fence would not otherwise let land, so the union row is the pair (true,
// out-of-workspace path) — in-fence for the ladder, still an escape at Execute.
func TestWriteEscapeVerdicts(t *testing.T) {
	t.Parallel()

	ws := t.TempDir()
	outside := filepath.Join(t.TempDir(), "notes.md")
	wsw := tools.NewWriteFile(ws)

	cases := []struct {
		name     string
		mode     domain.Mode
		guard    security.PreCheck
		confine  bool
		inFence  bool
		escape   string
		wantKind resolutionKind
		wantMint string
	}{
		{
			name: "ask-before gates an out-of-workspace write and its allow carries the target",
			mode: domain.ModeAskBefore, guard: proceed, confine: true,
			inFence: false, escape: outside,
			wantKind: resolveGate, wantMint: outside,
		},
		{
			name: "auto/confine=true gates it the same way",
			mode: domain.ModeAuto, guard: proceed, confine: true,
			inFence: false, escape: outside,
			wantKind: resolveGate, wantMint: outside,
		},
		{
			name: "auto/confine=false runs it and mints from dispatch's own classification",
			mode: domain.ModeAuto, guard: proceed, confine: false,
			inFence: false, escape: outside,
			wantKind: resolveRun, wantMint: outside,
		},
		{
			name: "a declared writable path is in-fence in auto and still runs with the permit",
			mode: domain.ModeAuto, guard: proceed, confine: true,
			inFence: true, escape: outside,
			wantKind: resolveRun, wantMint: outside,
		},
		{
			name: "a declared writable path is in-fence in allow-edits too",
			mode: domain.ModeAllowEdits, guard: proceed, confine: true,
			inFence: true, escape: outside,
			wantKind: resolveRun, wantMint: outside,
		},
		{
			// Approval is final (ADR 0049 Q4): the dangerous-action floor turns the call into a
			// forced LOOK, and the human's yes to the path the pane disclosed still runs.
			name: "a Tier-2 forced gate on an out-of-workspace write keeps its target",
			mode: domain.ModeAuto,
			guard: security.PreCheck{
				Outcome: security.GuardForceApproval, Reason: "curl | bash",
				Audit: security.AuditDangerousForceApproval,
			},
			confine: true, inFence: false, escape: outside,
			wantKind: resolveGate, wantMint: outside,
		},
		{
			name: "an in-workspace write authorises no escape",
			mode: domain.ModeAuto, guard: proceed, confine: true,
			inFence: true, escape: "",
			wantKind: resolveRun, wantMint: "",
		},
		{
			// Nothing refused reaches a permit — Plan's write refusal is the whole-ladder proof.
			name: "plan refuses the write and authorises nothing",
			mode: domain.ModePlan, guard: proceed, confine: true,
			inFence: false, escape: outside,
			wantKind: resolveRefuse, wantMint: "",
		},
		{
			// A guard hard-refuse pre-empts the ladder entirely.
			name: "a guard refusal authorises nothing",
			mode: domain.ModeAuto,
			guard: security.PreCheck{
				Outcome: security.GuardRefuse, Reason: "rm -rf /", Audit: security.AuditDangerousRefused,
			},
			confine: true, inFence: false, escape: outside,
			wantKind: resolveRefuse, wantMint: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolve(resolutionInput{
				mode:                   tc.mode,
				call:                   domain.ToolCall{ID: "c1", Tool: "write_file", Arguments: json.RawMessage(`{"path":"x"}`)},
				tool:                   wsw,
				guard:                  tc.guard,
				confineToWorkspace:     tc.confine,
				fsConfineAvailable:     true,
				writeTargetInWorkspace: tc.inFence,
				writeEscapeTarget:      tc.escape,
				approverPresent:        true,
				box:                    domain.ConfinementBox{WorkspaceRoot: ws},
			})
			if got.kind != tc.wantKind {
				t.Fatalf("kind = %s, want %s", got.kind, tc.wantKind)
			}
			if got.writeEscapeTarget != tc.wantMint {
				t.Errorf("writeEscapeTarget = %q, want %q", got.writeEscapeTarget, tc.wantMint)
			}
		})
	}
}

// TestWriteEscapeCtxInstallsOnlyWhatTheVerdictCarries pins the executor's half in isolation: a
// verdict with no escape leaves the context untouched (today's fence alone governs, byte for
// byte), and one with an escape installs a permit naming exactly that path.
func TestWriteEscapeCtxInstallsOnlyWhatTheVerdictCarries(t *testing.T) {
	t.Parallel()

	if _, ok := domain.WriteEscapePermitFrom(writeEscapeCtx(context.Background(), resolution{})); ok {
		t.Error("a verdict carrying no escape target installed a permit; the workspace fence must govern alone")
	}

	ctx := writeEscapeCtx(context.Background(), resolution{writeEscapeTarget: "/out/side/notes.md"})
	permit, ok := domain.WriteEscapePermitFrom(ctx)
	if !ok {
		t.Fatal("a verdict carrying an escape target installed no permit")
	}
	if permit.Real != "/out/side/notes.md" {
		t.Errorf("permit.Real = %q, want the verdict's target %q", permit.Real, "/out/side/notes.md")
	}
}

// TestDispatchMintsTheWriteEscapePermit is the seam end to end: a full Turn drives one write_file
// call through the real dispatch — hooks, guardrails, Resolution, Approval — and the tool reports
// what its execution context carried. Every case pins both facts that matter, whether the tool
// ran at all and what it was handed, because "no permit" is only half of a denial's promise.
func TestDispatchMintsTheWriteEscapePermit(t *testing.T) {
	t.Parallel()

	t.Run("an approved out-of-workspace gate hands over the disclosed target", func(t *testing.T) {
		t.Parallel()
		ws, outside := t.TempDir(), t.TempDir()
		want := filepath.Join(realPath(t, outside), "notes.md")

		probe, sink := &escapeProbe{WriteFile: tools.NewWriteFile(ws)}, &recordingSink{}
		cfg := autoConfigWS(sink, &fakeConfiner{caps: capsBoth()}, true, ws, probe)
		cfg.Approver = &fakeApprover{decision: domain.ApprovalAllow}

		driveToolCall(t, cfg, sink, "c1", "write_file", writeCall(want))

		if !hasEvent[domain.ApprovalEvent](sink.events) {
			t.Fatal("an out-of-workspace write did not gate")
		}
		probe.assert(t, true, want)
	})

	t.Run("a remembered allow-for-session mints the same target with no second prompt", func(t *testing.T) {
		t.Parallel()
		ws, outside := t.TempDir(), t.TempDir()
		want := filepath.Join(realPath(t, outside), "notes.md")

		probe, sink := &escapeProbe{WriteFile: tools.NewWriteFile(ws)}, &recordingSink{}
		approver := &fakeApprover{decision: domain.ApprovalAllowForSession}
		cfg := autoConfigWS(sink, &fakeConfiner{caps: capsBoth()}, true, ws, probe)
		cfg.Approver = approver

		// The same call twice: the second is answered from the session memory, not the human.
		driveTwoToolCalls(t, cfg, sink,
			toolReq{id: "c1", tool: "write_file", args: writeCall(want)},
			toolReq{id: "c2", tool: "write_file", args: writeCall(want)})

		if approver.calls != 1 {
			t.Errorf("Approver consulted %d times, want 1 (the second call is the remembered allow)", approver.calls)
		}
		// The probe holds the LAST execution — the one nobody was asked about.
		probe.assert(t, true, want)
	})

	t.Run("a denied gate hands over nothing and never runs the tool", func(t *testing.T) {
		t.Parallel()
		ws, outside := t.TempDir(), t.TempDir()
		target := filepath.Join(realPath(t, outside), "notes.md")

		probe, sink := &escapeProbe{WriteFile: tools.NewWriteFile(ws)}, &recordingSink{}
		cfg := configWithTools(sink, probe)
		cfg.Mode = domain.ModeAskBefore
		cfg.WorkspaceDir = ws
		cfg.Approver = &fakeApprover{decision: domain.ApprovalDeny}

		driveToolCall(t, cfg, sink, "c1", "write_file", writeCall(target))

		if probe.ran {
			t.Error("a denied out-of-workspace write executed the tool")
		}
		if res, ok := lastToolResult(sink.events); !ok || !res.IsError {
			t.Errorf("denied call result = %+v (ok=%v), want an error result", res, ok)
		}
	})

	t.Run("a Firing's fail-safe denier refuses the escape unattended", func(t *testing.T) {
		t.Parallel()
		// A scheduled Firing runs in Auto with an Approver that refuses every gate (internal/run).
		// The permit changes nothing about that: an unattended out-of-workspace write still gates,
		// and a gate nobody allows never reaches an execution, let alone a permit.
		ws, outside := t.TempDir(), t.TempDir()
		target := filepath.Join(realPath(t, outside), "notes.md")

		probe, sink := &escapeProbe{WriteFile: tools.NewWriteFile(ws)}, &recordingSink{}
		cfg := autoConfigWS(sink, &fakeConfiner{caps: capsBoth()}, true, ws, probe)
		cfg.Approver = &fakeApprover{decision: domain.ApprovalDeny}

		driveToolCall(t, cfg, sink, "c1", "write_file", writeCall(target))

		if probe.ran {
			t.Error("the fail-safe denier's refusal still executed the write")
		}
	})

	t.Run("auto with confine-to-workspace off mints without gating", func(t *testing.T) {
		t.Parallel()
		ws, outside := t.TempDir(), t.TempDir()
		want := filepath.Join(realPath(t, outside), "notes.md")

		probe, sink := &escapeProbe{WriteFile: tools.NewWriteFile(ws)}, &recordingSink{}
		cfg := autoConfigWS(sink, &fakeConfiner{caps: capsBoth()}, false, ws, probe)
		// A denying Approver proves the run never asked: if this cell gated, nothing would run.
		cfg.Approver = &fakeApprover{decision: domain.ApprovalDeny}

		driveToolCall(t, cfg, sink, "c1", "write_file", writeCall(want))

		if hasEvent[domain.ApprovalEvent](sink.events) {
			t.Error("the confine=false cell gated a write it is supposed to run unfenced")
		}
		probe.assert(t, true, want)
	})

	t.Run("a declared writable path classifies in-fence and mints", func(t *testing.T) {
		t.Parallel()
		ws, writable := t.TempDir(), t.TempDir()
		want := filepath.Join(realPath(t, writable), "notes.md")

		probe, sink := &escapeProbe{WriteFile: tools.NewWriteFile(ws)}, &recordingSink{}
		cfg := autoConfigWS(sink, &fakeConfiner{caps: capsBoth()}, true, ws, probe)
		cfg.ConfineWritablePaths = []string{writable}
		// Denying again: the union member must be in-fence at classification, so nothing gates.
		cfg.Approver = &fakeApprover{decision: domain.ApprovalDeny}

		driveToolCall(t, cfg, sink, "c1", "write_file", writeCall(want))

		if hasEvent[domain.ApprovalEvent](sink.events) {
			t.Error("a target inside the box's declared writable paths gated; the union is in-fence")
		}
		probe.assert(t, true, want)
	})

	t.Run("an in-workspace write carries no permit", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()

		probe, sink := &escapeProbe{WriteFile: tools.NewWriteFile(ws)}, &recordingSink{}
		cfg := autoConfigWS(sink, &fakeConfiner{caps: capsBoth()}, true, ws, probe)
		cfg.Approver = &fakeApprover{decision: domain.ApprovalAllow}

		driveToolCall(t, cfg, sink, "c1", "write_file", `{"path":"notes.md","content":"hi"}`)

		probe.assert(t, false, "")
	})

	t.Run("a read-only call carries no permit", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()

		sink := &recordingSink{}
		reader := &readPermitProbe{fakeTool: fakeTool{name: "read_file", readOnly: true}}
		cfg := autoConfigWS(sink, &fakeConfiner{caps: capsBoth()}, true, ws, reader)
		cfg.Approver = &fakeApprover{decision: domain.ApprovalAllow}

		driveToolCall(t, cfg, sink, "c1", "read_file", `{"path":"notes.md"}`)

		if !reader.ran {
			t.Fatal("the read-only call did not run")
		}
		if reader.granted {
			t.Error("a read-only call was handed a write-escape permit; the permit widens no read")
		}
	})
}

// writeCall spells one write_file call's arguments for an absolute target.
func writeCall(target string) string {
	return `{"path":"` + target + `","content":"hi"}`
}

// assert pins the probe's two facts together: a tool that never ran cannot have been handed
// anything, and one that did must have been handed exactly the target the fence classified.
func (p *escapeProbe) assert(t *testing.T, wantPermit bool, wantTarget string) {
	t.Helper()
	if !p.ran {
		t.Fatal("write_file never executed")
	}
	if p.granted != wantPermit {
		t.Fatalf("permit present = %v, want %v (target seen: %q)", p.granted, wantPermit, p.target)
	}
	if p.target != wantTarget {
		t.Errorf("permit target = %q, want %q", p.target, wantTarget)
	}
}

// readPermitProbe is a read-only tool that records whether it was handed a write-escape permit —
// the negative control for "a permit never widens a read".
type readPermitProbe struct {
	fakeTool
	ran     bool
	granted bool
}

func (p *readPermitProbe) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	_, granted := domain.WriteEscapePermitFrom(ctx)
	p.ran, p.granted = true, granted
	return domain.ToolResult{CallID: call.ID, Content: "read"}, nil
}
