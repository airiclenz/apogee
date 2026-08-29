//go:build windows

package platform

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform/winlabel"
)

// Windows token Confiner backend (Phase 5 item 8 — ADR 0020;
// confinement-execution-contract §9).
//
// The two shipped backends fence by PATH POLICY: landlock is handed a ruleset of
// path-beneath allow rules, seatbelt a profile with `allow file-write*` under the box's
// roots, and neither touches the user's disk. Windows has no facility of that shape.
// Mandatory integrity control fences by IDENTITY — a token carries an integrity level, an
// object may carry a mandatory label, and the kernel's mandatory check runs BEFORE the
// DACL — and nothing in that model takes "these paths are writable" as an argument. The
// whole design follows from that one asymmetry (ADR 0020):
//
//   - The FENCE is a restricted, Low-integrity primary token handed to
//     SysProcAttr.Token. The child runs at Low; every object carrying no explicit label is
//     implicitly Medium with NO_WRITE_UP, so every write outside the box is denied by the
//     kernel. A process the child creates inherits the token, so the denial covers the whole
//     descendant tree — the Windows equivalent of "the domain survives execve".
//   - The BOX is a label on the DISK. Because the token cannot carry path policy, the
//     writable half of a ConfinementBox can only be expressed on the objects themselves:
//     WorkspaceRoot ∪ WritablePaths are labelled Low for the run and REVERTED on teardown.
//     This is a side effect on the user's disk that landlock and seatbelt do not have; it is
//     journalled against a crash and it is the headline consequence of this backend.
//   - There is NO helper process, NO argv sentinel and NO argv rewrite. Linux needs its
//     42-line helper because the only CGO-free way to run code between fork and execve is to
//     BE a separate process that restricts itself; Windows has no "restrict myself" API to
//     mirror — the restriction is a token handed to the process-creation call, which is
//     exactly what SysProcAttr exposes. cmd/apogee's maybeDispatchConfinedExec gains no
//     Windows arm and confined_exec_windows.go is not written (ADR 0020 §1).
//
// This file is the COMPOSER, and holds neither of the two mechanisms above. The fence is
// minted in wintoken_windows.go. The box — the journal, the write-before-label invariant,
// the label walk, the revert, the crash recovery and the wording that reports all of it —
// is internal/platform/winlabel, a leaf package that knows nothing about apogee; read it
// there rather than here, and note that its failures arrive PLAIN, so labelBox is the one
// place the ErrConfinementUnavailable sentinel is wrapped back on. What is left in this
// file is what only a Confiner backend can do: pick a backend for the host, answer for the
// facility once, refuse the boxes this OS cannot express, resolve the roots the OS will
// actually mutate, and hand the token to the cmd. The version floor and the labelling
// guardrails those refusals consult are in winguard.go.

// tokenConfiner is the Windows Confiner backend. Its capabilities are probed ONCE at
// construction (contract §5): the host is at or above the version floor and the restricted
// Low token minted. Capability honesty splits in two here, the one structural difference
// from Linux and macOS — Capabilities answers for the FACILITY, while a per-run failure to
// label a box's roots is a Confine-time ErrConfinementUnavailable that contract §4 demotes
// to a forced Gate (ADR 0020 §3).
//
// The token is minted once and reused for every confined command: it carries no path policy,
// so it is box-independent, which also settles who owns the handle given that Confine
// returns before Start. The label pass IS per box and is memoised, so the first confined
// command of a session pays it and the rest are free.
//
// The backend keeps ONE lock and it guards the LIFECYCLE — the token handle, the capabilities
// derived from it, and the closed flag — because Close and Confine run on different goroutines
// at shutdown: bubbletea's abnormal exit (SIGINT, a closed console) runs the composition root's
// deferred Close while a tool goroutine is still inside Confine, and an unsynchronised backend
// would let that Confine read a token of 0 AFTER its guard passed and hand CreateProcess an
// UNCONFINED child while the caller recorded it as confined (audit C-20). Confine holds the
// READ lock for its whole body — the label walk included — so the handle can never be closed
// under a CreateProcess that is about to use it; Close holds the WRITE lock, and once it has run
// every later Confine refuses with ErrConfinementUnavailable, which contract §4 demotes to a
// forced Gate: a command can fail to START during shutdown and can never start unconfined
// (ADR 0020's 2026-08-29 amendment).
//
// LOCK ORDER: tokenConfiner.mu OUTSIDE, Journal.mu inside, never the reverse. labelBox and
// resolveBoxRoots run under the read lock and take no lock of their own; the journal takes its
// own beneath them.
type tokenConfiner struct {
	// mu guards the lifecycle fields — caps, token and closed — against a shutdown racing a
	// tool goroutine (see the lock discipline above). It is an RWMutex because Confine,
	// Capabilities and the startup label prewarm are all readers: concurrent confinements stay
	// as parallel as they were, and only Close excludes them.
	mu sync.RWMutex
	// closed latches at Close: the token handle is gone by then, so Confine must REFUSE rather
	// than prepare a cmd with a zero token. Guarded by mu.
	closed bool
	caps   domain.ConfinementCaps
	// token is the restricted Low-integrity primary token, or 0 when minting failed (in
	// which case caps is {false, false} and Confine refuses).
	token windows.Token
	// rules is the Windows path rule set with the OS long-path resolver wired in — the same
	// value Current returns — used to collapse the box's roots and evaluate the guardrails.
	rules hostRules
	// protected are the locations the backend refuses to label (ADR 0020 §2).
	protected []string
	// journal is this session's label journal: the record of what has been labelled, the file
	// it is written to, and the roots already walked. It carries its own lock for the label
	// record, which nests INSIDE the backend's lifecycle lock (see the lock order above) —
	// Confine records through it from whichever goroutine is driving a tool call, and Close
	// retires it at shutdown.
	journal *winlabel.Journal
}

// NewConfiner returns the host's real Confiner backend for this OS
// (confinement-execution-contract §2.6/§9): the Windows restricted-low-integrity-token
// backend, constructed for a SESSION. Below the version floor it returns denyConfiner
// instead, so a below-floor Windows host is exactly today's Windows host — {false, false},
// the subprocess surface gated, and the existing degradation notice firing unchanged, with
// no new wording and no special case (ADR 0020 §5).
//
// Construction performs no disk I/O beyond finishing an interrupted PREVIOUS run's restore:
// labelling belongs to Confine and never to the constructor. The recovery path reads one
// directory that normally does not exist and writes only when a crashed run actually left
// labels behind — the state ADR 0020 §2 requires the next NewConfiner to finish cleaning up.
// A caller that must not write at all asks for NewReportConfiner instead.
func NewConfiner() domain.Confiner { return selectWindowsConfiner(newTokenConfiner) }

// NewReportConfiner returns the backend `apogee probe host` DESCRIBES — the same selection
// NewConfiner makes, minus the crash-recovery pass (ADR 0021 §1).
//
// The host report is pinned free, offline and read-only on three surfaces (ADR 0021 §1, the
// README, the command's own Long text) and that pledge is absolute: no exception is carved
// for Windows. Recovering here would also destroy the very thing the report exists to state
// — ADR 0020 §2 promises the report SURFACES an outstanding journal, and a constructor that
// reverted and deleted it first would make that line unreachable for exactly the interrupted
// run it was written for. Nothing is lost by waiting: the journal survives until a real
// session's constructor finishes the restore.
func NewReportConfiner() domain.Confiner {
	return selectWindowsConfiner(newTokenConfinerWithoutRecovery)
}

// selectWindowsConfiner applies the version floor and hands the surviving hosts to build,
// which is the caller's choice of session (recovering) or report (recovery-free)
// construction. The floor decision lives in ONE place so the two selectors cannot disagree
// about which hosts get the token backend.
func selectWindowsConfiner(build func(home string) *tokenConfiner) domain.Confiner {
	if _, _, buildNumber := windows.RtlGetNtVersionNumbers(); belowWindowsFloor(buildNumber) {
		return NewDenyConfiner()
	}
	return build(winlabel.Home())
}

// newTokenConfiner builds the SESSION backend against a given apogee home (the journal's
// location), mints the token, and finishes any outstanding restore — ADR 0020 §2's
// interrupted-cleanup remedy, which is a write and therefore belongs to a session and not to
// a report (NewReportConfiner).
func newTokenConfiner(home string) *tokenConfiner {
	c := newTokenConfinerWithoutRecovery(home)
	if home != "" {
		winlabel.Recover(home)
	}
	return c
}

// newTokenConfinerWithoutRecovery builds the backend and mints the token, touching the disk
// NOWHERE: it resolves the journal's path without reading, writing or removing anything under
// it. It is what the probe path constructs, so the host report can read the journal directory
// and report what it finds rather than consuming it.
//
// home may be "" — os.UserHomeDir failed, so there is no user profile to write a journal
// under, or a test deliberately withholds one. Construction and Capabilities are unaffected:
// caps still answer for the FACILITY, which is present (the token mints, the kernel enforces),
// and the backend simply keeps no journal and cannot recover one. What it can no longer do is
// LABEL: Confine refuses with ErrConfinementUnavailable, the routine per-run failure kind
// contract §4 demotes to a forced Gate. The invariant is ADR 0020 §2's — the one disk mutation
// apogee performs is only ever made against a record of how to undo it, so no journal means no
// label rather than an unrevertable one.
func newTokenConfinerWithoutRecovery(home string) *tokenConfiner {
	rules := currentRules()
	c := &tokenConfiner{
		rules:     rules,
		protected: windowsProtectedRoots(os.LookupEnv, userProfileRoot()),
		journal:   winlabel.Open(home),
	}

	token, err := mintRestrictedLowToken()
	if err != nil {
		// A mint failure is honest incapacity, not a crash: caps stay {false, false}, the
		// disposition gates the subprocess surface, and the degradation notice explains it.
		return c
	}
	c.token = token
	c.caps = domain.ConfinementCaps{FSWrite: true, NetworkEgress: false}
	return c
}

// Capabilities reports what this backend can enforce on this host, probed once at
// construction (contract §5). FSWrite is true once the restricted Low token is minted at or
// above the floor. NetworkEgress is FALSE always and by construction: ConfinementBox's
// NetworkAllow is a per-host tightening list, no token or integrity facility can express
// per-host egress, and the Windows facilities that can (WFP, firewall rules) are
// machine-scoped and admin-requiring. The backend is Auto-eligible anyway, because
// AutoEligible() is FSWrite-only (ADR 0012) — the same position a 5.13–6.6 Linux kernel
// occupies.
//
// It reads under the lifecycle lock, so it answers the empty {false, false} once Close has run
// rather than a torn value — the same honest incapacity a mint failure reports.
func (c *tokenConfiner) Capabilities() domain.ConfinementCaps {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.caps
}

// Confine prepares cmd to execute confined to box, then returns — it does not run cmd
// (contract §2.2). It sets cmd.SysProcAttr.Token and NOTHING ELSE on the cmd: cmd.Path and
// cmd.Args are untouched, and the §2.4 process-tree teardown's Job Object (which the
// execution tools own, and which is teardown, never a fence) composes with it because each
// side only ever appends to SysProcAttr.
//
// Before that it labels the box's roots, once per box. Contract §2.2's "performs no I/O" is
// amended for this backend (§9): the label pass is bounded, idempotent, once-per-box disk
// I/O — it still never runs the command and never blocks on it. Every way the box cannot be
// expressed on this disk — a network-deny box, a guardrailed root, a read-only root, a
// filesystem with no SACL support (FAT32/exFAT, many network shares) — returns
// ErrConfinementUnavailable, which contract §4's precomputed fallback demotes to a forced
// Gate. On Linux and macOS that path is nearly unreachable; here it is routine.
//
// One failure lands in neither Capabilities nor here, by construction: a CreateProcessAsUser
// refusal happens at cmd.Start(), after Confine has returned, so it surfaces as the tool's
// own run error. The command FAILS; it does not run unconfined.
//
// The whole body runs under the lifecycle read lock and reads the token ONCE, into a local: a
// Close racing this call either waits for it to finish or precedes it entirely, and a Confine
// that starts after one refuses outright. There is no interleaving in which a zero token
// reaches SysProcAttr while this returns nil (audit C-20).
func (c *tokenConfiner) Confine(_ context.Context, box domain.ConfinementBox, cmd *exec.Cmd) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return fmt.Errorf("%w: the Windows token backend has been closed — the session is shutting down", domain.ErrConfinementUnavailable)
	}
	if !c.caps.FSWrite || c.token == 0 {
		return fmt.Errorf("%w: the Windows token backend could not mint a restricted token on this host", domain.ErrConfinementUnavailable)
	}
	token := c.token
	if err := windowsNetworkDenyDecision(box); err != nil {
		return err
	}
	if err := c.labelBox(box); err != nil {
		return err
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Token = syscall.Token(token)
	return nil
}

// Close reverts every label this session applied and releases the token. The backend is an
// io.Closer rather than a Confiner method on purpose: domain.Confiner is a public interface
// (ADR 0010) and must not sprout a lifecycle hook for one OS, so the composition root
// asserts the optional interface and defers it beside its other Close calls (ADR 0020 §2).
//
// It takes the lifecycle WRITE lock, so it cannot land between a Confine's guard and its store,
// and it latches closed: every later Confine refuses with ErrConfinementUnavailable rather than
// preparing a cmd with a token that has been handed back to the kernel.
//
// It is safe to call repeatedly, and journal.Retire() runs on EVERY call — a repeated Retire is
// designed to CONVERGE (winlabel/session.go: a handed-off entry is discharged by a later call),
// so the closed latch must never short-circuit it. Only the token close and the capability
// zeroing latch, because a handle may be closed once.
func (c *tokenConfiner) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
	err := c.journal.Retire()
	if c.token != 0 {
		_ = c.token.Close()
		c.token = 0
	}
	c.caps = domain.ConfinementCaps{}
	return err
}

// labelBox labels the box's roots Low, once per root per session. Everything the label pass
// itself decides — journal before label, the memo, the unwind of a root whose write failed,
// which descendants are tolerated — belongs to winlabel.LabelTree, and this method is what is
// left once it does: the two refusals only the backend can make, root resolution, and the
// loop.
//
// A backend with nowhere to write its journal refuses the box outright, before any label is
// read or written: an unrevertable Low label on the user's disk is a worse outcome than a
// forced Gate, and refusing here is what leaves the "journal first, label second" invariant
// with no bypass at all (ADR 0020 §2).
//
// The roots are first resolved to their final on-disk form — and a reparse-point root
// refused — by resolveBoxRoots, so the guardrails, the journal and the label pass all see
// the location the OS will actually mutate rather than a spelling of it.
//
// winlabel returns its failures PLAIN, so the confinement sentinel is wrapped here, once
// (D4): the rendered message is what it always was, and every caller's errors.Is still holds,
// which is what lets the label mechanism stay a leaf package that knows nothing about apogee.
//
// Both callers (Confine, PrewarmLabelWalk) hold the lifecycle READ lock for the whole call and
// this method takes none of its own; the journal's lock nests inside it (the lock order on the
// type).
func (c *tokenConfiner) labelBox(box domain.ConfinementBox) error {
	if !c.journal.Writable() {
		return fmt.Errorf("%w: no user profile could be resolved, so there is nowhere to write the label journal; refusing to label %q rather than leave a mandatory label with no record of how to undo it",
			domain.ErrConfinementUnavailable, box.WorkspaceRoot)
	}

	box, err := c.resolveBoxRoots(box)
	if err != nil {
		return err
	}
	roots, err := windowsBoxRoots(c.rules, box, c.protected)
	if err != nil {
		return err
	}

	for _, root := range roots {
		if err := winlabel.LabelTree(root, c.journal); err != nil {
			return fmt.Errorf("%w: %v", domain.ErrConfinementUnavailable, err)
		}
	}
	return nil
}

// resolveBoxRoots returns box with each root replaced by its final on-disk form, or a
// refusal wrapping ErrConfinementUnavailable. It runs BEFORE windowsBoxRoots because the
// guardrails there are lexical while SetNamedSecurityInfo is not: the OS strips the
// trailing dots and spaces Win32 canonicalization ignores and follows every reparse point,
// so a guardrail judging the SPELLING would wave through a root whose label write then
// lands on a protected location — `C:\Windows.` is C:\Windows, and a junction is wherever
// it points (ADR 0020 §6).
func (c *tokenConfiner) resolveBoxRoots(box domain.ConfinementBox) (domain.ConfinementBox, error) {
	workspace, err := c.resolveBoxRoot(box.WorkspaceRoot)
	if err != nil {
		return box, err
	}
	box.WorkspaceRoot = workspace
	if len(box.WritablePaths) > 0 {
		resolved := make([]string, len(box.WritablePaths))
		for i, path := range box.WritablePaths {
			if resolved[i], err = c.resolveBoxRoot(path); err != nil {
				return box, err
			}
		}
		box.WritablePaths = resolved
	}
	return box, nil
}

// resolveBoxRoot resolves one box root to its final form. An empty root names nothing —
// windowsBoxRoots drops it — and resolves to itself. Two rules:
//
//   - A root that IS a reparse point (a junction or symlink) is refused outright: labelling
//     it would silently mutate its target, which is why the label walk skips descendant
//     reparse points entirely (winlabel.LabelTree) — the root was the one spelling that
//     escaped that rule, and no resolution makes it honestly labellable.
//   - Every other root must resolve through the finalPath seam (GetFinalPathNameByHandle),
//     so the guardrails judge the answer; a root the resolver cannot answer for is refused,
//     never guessed about — the same posture windowsLabelGuardrail takes with a path split
//     cannot compare.
func (c *tokenConfiner) resolveBoxRoot(root string) (string, error) {
	if root == "" {
		return "", nil
	}
	if info, err := os.Lstat(root); err == nil && info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
		return "", fmt.Errorf("%w: refusing to label %q — a box root that is itself a reparse point (a junction or symlink) would be labelled through to its target",
			domain.ErrConfinementUnavailable, root)
	}
	final, ok := "", false
	if c.rules.finalPath != nil {
		final, ok = c.rules.finalPath(root)
	}
	if !ok {
		return "", fmt.Errorf("%w: refusing to label %q — this host cannot resolve it to its final on-disk form, so the guardrails cannot be evaluated",
			domain.ErrConfinementUnavailable, root)
	}
	return final, nil
}

// The backend must satisfy the Confiner contract, and the optional teardown interface the
// composition root asserts, at compile time.
var (
	_ domain.Confiner            = (*tokenConfiner)(nil)
	_ interface{ Close() error } = (*tokenConfiner)(nil)
)
