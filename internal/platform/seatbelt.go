//go:build !windows

package platform

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// macOS seatbelt Confiner backend (P3.3 — ADR 0012, confinement-execution-contract §2.3/§5/§6)
// ----------------------------------------------------------------------------
//
// seatbeltConfiner realises the single, subprocess-granularity confinement model
// (ADR 0012) on macOS: a confined tool call execs under the system sandbox profiler
// `sandbox-exec -p <profile>`, which applies the generated seatbelt profile to the
// real child. This is the SAME single granularity as Linux landlock — there is no
// in-process per-thread confinement and no macOS-gates-every-edit asymmetry. Apogee's
// own in-process writes are path-safety-bounded in every mode (the contract's
// blast-radius split, D1).
//
// The profile is a PURE FUNCTION of the box (seatbeltProfile): deny-default for
// file-write, allow file-write beneath WorkspaceRoot + each WritablePaths entry,
// network OPEN by default and restricted to a deny clause only when the box opts into
// network-deny via a non-empty NetworkAllow (ADR 0012 — network is open by default,
// deny is an optional coarse tightening, mirroring landlock's semantics). Because the
// profile is a string built from the box with no process, it is unit-tested hermetically
// on any non-Windows host (P3.3 acceptance), and the bulk of this backend is
// host-agnostic — only NewSeatbeltConfiner's real presence probe is darwin-tagged
// (seatbelt_darwin.go).
//
// This file is //go:build !windows (not darwin-only) so the hermetic tests run on the
// Linux dev host: SysProcAttr.Setpgid (the process-group teardown contract, §2.4) is
// POSIX-only and does not exist on Windows, where only denyConfiner is selected (Windows
// confinement is Phase 5, plan §6). Only NewSeatbeltConfiner is selected per-OS (P3.4
// picks it on darwin); on Linux this concrete type is compiled but never chosen as the
// backend.
//
// sandbox-exec is deprecated by Apple but remains present and functional on stock macOS;
// it is the same facility apogee-code-class agents and other sandboxing tools rely on.

// sandboxExecPath is the stock-macOS path to the system sandbox profiler. Confine
// rewrites a confined command to launch under it (confinement-execution-contract §2.3).
const sandboxExecPath = "/usr/bin/sandbox-exec"

// seatbeltConfiner is the macOS Confiner backend. Whether sandbox-exec is present is
// probed once at construction (NewSeatbeltConfiner, seatbelt_darwin.go) and never
// re-queried, so Capabilities reports what this host can enforce here and now (the
// contract's capability-honesty rule, §5).
//
// execPath is the sandbox profiler Confine launches; it defaults to sandboxExecPath and
// is overridable only so tests can target a stub binary — production code never sets it.
type seatbeltConfiner struct {
	present  bool   // whether sandbox-exec is available on this host
	execPath string // sandbox profiler to exec ("" => sandboxExecPath)
}

// newSeatbeltConfiner builds the backend from an explicit presence result. It is the
// shared constructor both the darwin presence-probing NewSeatbeltConfiner and the
// hermetic tests use, so the caps/profile/rewrite logic is exercised on any host without
// a real macOS (confinement-execution-contract §5; P3.3 acceptance).
func newSeatbeltConfiner(present bool) *seatbeltConfiner {
	return &seatbeltConfiner{present: present}
}

// Capabilities reports what seatbelt can enforce on this host, probed once at
// construction (confinement-execution-contract §5). A single sandbox-exec profile
// enforces both filesystem-write and network egress, so when sandbox-exec is present
// both caps are true; when it is absent both are false, so the disposition gates the
// subprocess surface rather than confining it ("confine if you can, gate if you can't",
// ADR 0012) and Auto is not refused.
func (c *seatbeltConfiner) Capabilities() domain.ConfinementCaps {
	return domain.ConfinementCaps{
		FSWrite:       c.present,
		NetworkEgress: c.present,
	}
}

// Confine prepares cmd to execute confined to box, then returns — it does not run cmd
// (confinement-execution-contract §2.2). It generates the seatbelt profile from box and
// rewrites cmd to launch under `sandbox-exec -p <profile> <original argv...>`, then sets
// Setpgid so the caller's process-group kill reaches the wrapped child (§2.4). The
// original Stdin/Stdout/Stderr/Dir/Env are inherited by sandbox-exec, which execs the
// real child. The parent process is never restricted.
//
// Confine is only meaningful when Capabilities().FSWrite is true; the dispatch
// disposition checks caps before calling it (§4). If sandbox-exec is not present it
// returns ErrConfinementUnavailable, the contract's "confine if you can, gate if you
// can't" safety net (§2.2). ctx covers only this synchronous preparation; the run's
// lifetime is governed by cmd's own context.
func (c *seatbeltConfiner) Confine(_ context.Context, box domain.ConfinementBox, cmd *exec.Cmd) error {
	if len(cmd.Args) == 0 {
		return fmt.Errorf("apogee: confine: cmd has no argv")
	}
	if !c.present {
		return fmt.Errorf("%w: sandbox-exec not present on this host", domain.ErrConfinementUnavailable)
	}

	profiler := c.execPath
	if profiler == "" {
		profiler = sandboxExecPath
	}

	profile := seatbeltProfile(box)

	// Launch the original command under the sandbox profiler; argv after the profile is
	// the original command, run confined by sandbox-exec.
	orig := cmd.Args
	cmd.Path = profiler
	cmd.Args = append([]string{profiler, "-p", profile}, orig...)

	// Setpgid puts the wrapped child and its descendants in one process group so the
	// tool's negative-PID kill reaps the whole group (contract §2.4); the tool sets
	// cmd.Cancel/WaitDelay (P3.8).
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	return nil
}

// seatbeltProfile builds the sandbox-exec profile string for box (confinement-execution
// -contract §2.3). It is a pure function of the box — no process, no host state — so it
// is unit-tested hermetically on any host (P3.3 acceptance). The profile:
//
//   - allows everything by default, then subtracts file-write (deny-default for writes,
//     read/exec stay open — the box bounds where a child may WRITE, matching
//     ConfinementBox's workspace-write-only semantics, like landlock not handling read);
//   - re-allows file-write* beneath WorkspaceRoot and each WritablePaths entry via
//     (subpath ...) so writes are fenced to the box;
//   - leaves the network OPEN by default (ADR 0012), adding a (deny network*) clause only
//     when the box opts into network-deny via a non-empty NetworkAllow — a coarse
//     tightening matching landlock's deny-all-TCP behaviour (per-host allow is a later
//     additive change, like landlock's deferred per-port rule).
func seatbeltProfile(box domain.ConfinementBox) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(allow default)\n")

	// Deny all file writes, then re-grant beneath the writable roots.
	b.WriteString("(deny file-write*)\n")

	roots := make([]string, 0, 1+len(box.WritablePaths))
	if box.WorkspaceRoot != "" {
		roots = append(roots, box.WorkspaceRoot)
	}
	for _, p := range box.WritablePaths {
		if p != "" {
			roots = append(roots, p)
		}
	}
	if len(roots) > 0 {
		b.WriteString("(allow file-write*\n")
		for _, root := range roots {
			b.WriteString("    (subpath ")
			b.WriteString(seatbeltQuote(root))
			b.WriteString(")\n")
		}
		b.WriteString(")\n")
	}

	// Network is open by default (ADR 0012). A non-empty NetworkAllow opts the box into
	// network-deny: deny all outbound network as a coarse tightening (per-host allow is a
	// later additive change once a finer filter is wired, mirroring landlock's deny-all-TCP).
	if len(box.NetworkAllow) > 0 {
		b.WriteString("(deny network*)\n")
	}

	return b.String()
}

// seatbeltQuote renders s as a TinyScheme string literal for the profile, escaping
// backslashes and double quotes. Profile paths come from the box (WorkspaceDir + config),
// so this is a correctness belt for paths containing quotes/backslashes, not an
// adversarial quoter.
func seatbeltQuote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\', '"':
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	b.WriteByte('"')
	return b.String()
}
