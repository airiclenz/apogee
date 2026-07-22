//go:build windows

package platform

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/airiclenz/apogee/internal/domain"
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

// createRestrictedToken is CreateRestrictedToken, which golang.org/x/sys/windows v0.45.0
// does not bind — one advapi32 LazyProc, the same shape landlock takes with raw
// unix.Syscall for the landlock_* numbers x/sys has no wrapper for.
var createRestrictedToken = windows.NewLazySystemDLL("advapi32.dll").NewProc("CreateRestrictedToken")

// disableMaxPrivilege is CreateRestrictedToken's DISABLE_MAX_PRIVILEGE flag: the new token
// drops every privilege but SeChangeNotifyPrivilege. It is DEFENCE IN DEPTH, not the fence
// — it strips SeBackup/SeRestore/SeTakeOwnership/SeDebug, with which a child could otherwise
// walk around a mandatory label. No restricting SIDs and no deny-only SIDs are used: they
// force a double access check that breaks ordinary programs (which must still read system
// DLLs) and buy nothing the integrity level does not already give under ADR 0012's threat
// model.
const disableMaxPrivilege = 0x1

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
type tokenConfiner struct {
	caps domain.ConfinementCaps
	// token is the restricted Low-integrity primary token, or 0 when minting failed (in
	// which case caps is {false, false} and Confine refuses).
	token windows.Token
	// rules is the Windows path rule set with the OS long-path resolver wired in — the same
	// value Current returns — used to collapse the box's roots and evaluate the guardrails.
	rules hostRules
	// protected are the locations the backend refuses to label (ADR 0020 §2).
	protected []string
	// journalPath is this process's label journal under the apogee home.
	journalPath string

	// mu guards the memoised label state and the journal, which Confine mutates from
	// whichever goroutine is driving a tool call and Close reads at shutdown.
	mu sync.Mutex
	// labelled records the box roots whose trees have already been labelled this session,
	// so the once-per-box cost is not paid once per command.
	labelled map[string]bool
	// journal is the in-memory twin of journalPath.
	journal labelJournal
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
	return build(confinementJournalHome())
}

// newTokenConfiner builds the SESSION backend against a given apogee home (the journal's
// location), mints the token, and finishes any outstanding restore — ADR 0020 §2's
// interrupted-cleanup remedy, which is a write and therefore belongs to a session and not to
// a report (NewReportConfiner).
func newTokenConfiner(home string) *tokenConfiner {
	c := newTokenConfinerWithoutRecovery(home)
	if home != "" {
		recoverLabelJournals(home)
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
		labelled:  make(map[string]bool),
	}
	if home != "" {
		c.journalPath = labelJournalPath(home, os.Getpid())
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
func (c *tokenConfiner) Capabilities() domain.ConfinementCaps { return c.caps }

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
func (c *tokenConfiner) Confine(_ context.Context, box domain.ConfinementBox, cmd *exec.Cmd) error {
	if !c.caps.FSWrite || c.token == 0 {
		return fmt.Errorf("%w: the Windows token backend could not mint a restricted token on this host", domain.ErrConfinementUnavailable)
	}
	if err := windowsNetworkDenyDecision(box); err != nil {
		return err
	}
	if err := c.labelBox(box); err != nil {
		return err
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Token = syscall.Token(c.token)
	return nil
}

// Close reverts every label this session applied and releases the token. The backend is an
// io.Closer rather than a Confiner method on purpose: domain.Confiner is a public interface
// (ADR 0010) and must not sprout a lifecycle hook for one OS, so the composition root
// asserts the optional interface and defers it beside its other Close calls (ADR 0020 §2).
func (c *tokenConfiner) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	err := c.restoreLabels()
	if c.token != 0 {
		_ = c.token.Close()
		c.token = 0
	}
	c.caps = domain.ConfinementCaps{}
	return err
}

// labelBox labels the box's roots Low, once per root per session. The journal is written
// BEFORE the first label so a process killed mid-pass still leaves a complete-enough record
// to undo the mutation, and what it may say about a root is journalLabelEntry's decision —
// never apogee's own label as the state to restore.
//
// A backend with nowhere to write its journal refuses the box outright, before any label is
// read or written: an unrevertable Low label on the user's disk is a worse outcome than a
// forced Gate, and refusing here is what leaves the "journal first, label second" invariant
// with no bypass at all (ADR 0020 §2).
//
// The memo is keyed by the folded path for the same reason the journal is: C:\Work and
// c:\work are one root, and labelling it twice would journal it twice.
func (c *tokenConfiner) labelBox(box domain.ConfinementBox) error {
	if c.journalPath == "" {
		return fmt.Errorf("%w: no user profile could be resolved, so there is nowhere to write the label journal; refusing to label %q rather than leave a mandatory label with no record of how to undo it",
			domain.ErrConfinementUnavailable, box.WorkspaceRoot)
	}

	roots, err := windowsBoxRoots(c.rules, box, c.protected)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, root := range roots {
		key := foldLabelPath(root)
		if c.labelled[key] {
			continue
		}
		prior, err := readLabelSDDL(root)
		if err != nil {
			return fmt.Errorf("%w: cannot read the mandatory label of %q: %v", domain.ErrConfinementUnavailable, root, err)
		}
		if err := c.journalLabel(labelJournalEntry{Path: root, Root: true, PriorSDDL: prior}); err != nil {
			return err
		}
		if err := c.labelTree(root); err != nil {
			return err
		}
		c.labelled[key] = true
	}
	return nil
}

// journalLabel records one about-to-be-labelled path and persists the journal when the record
// changed, wrapping a flush failure as ErrConfinementUnavailable — nothing is ever labelled
// without a journal on disk describing how to undo it. Callers hold c.mu.
func (c *tokenConfiner) journalLabel(entry labelJournalEntry) error {
	entries, changed := journalLabelEntry(c.journal.Entries, entry, foldLabelPath)
	c.journal.Entries = entries
	if !changed {
		return nil
	}
	if err := c.flushJournal(); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrConfinementUnavailable, err)
	}
	return nil
}

// labelTree walks root and labels every directory and regular file Low. It must recurse:
// inheritance applies to NEWLY CREATED objects only, so a file that predates the labelling
// is implicitly Medium and a Low child editing an existing source file would be denied.
//
// Symlinks and other reparse points are skipped entirely — SetNamedSecurityInfo follows the
// link, so labelling one would silently mutate a target outside the box. A failure on the
// ROOT fails the box (the fence would be a box the agent cannot write to at all); a failure
// on an individual descendant is tolerated, because a single locked or foreign-owned file
// makes that ONE path read-only to the confined child, exactly as if it were read-only on
// disk, and must not gate a whole session. A descendant whose PRIOR label cannot be read
// takes the same tolerated rung, but before anything is written: no journal entry can
// describe what the read did not deliver, so the path is left exactly as it is
// (descendantLabelDecision) rather than labelled with no record of how to undo it.
func (c *tokenConfiner) labelTree(root string) error {
	if err := setLabelSDDL(root, windowsDirLabelSDDL); err != nil {
		return fmt.Errorf("%w: cannot label %q Low: %v", domain.ErrConfinementUnavailable, root, err)
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == root {
				return fmt.Errorf("%w: cannot walk %q: %v", domain.ErrConfinementUnavailable, root, walkErr)
			}
			return nil // an unreadable sub-tree stays unlabelled rather than failing the box
		}
		if path == root {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		if info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		prior, priorErr := readLabelSDDL(path)
		shouldJournal, shouldLabel := descendantLabelDecision(prior, priorErr)
		if !shouldLabel {
			// The prior could not be read, so labelling would destroy a possibly-foreign
			// label with no journalled record to restore it from. The path takes the
			// tolerated-descendant rung instead (descendantLabelDecision): it stays
			// unlabelled, and only that one path is opaque to the confined child.
			return nil
		}
		if shouldJournal {
			// A Low prior here is apogee's own label — a tree being re-walked, or one a
			// concurrent session labelled — and journalLabel drops it rather than recording
			// an instruction to put it back.
			if err := c.journalLabel(labelJournalEntry{Path: path, PriorSDDL: prior}); err != nil {
				return err
			}
		}
		sddl := windowsFileLabelSDDL
		if entry.IsDir() {
			sddl = windowsDirLabelSDDL
		}
		_ = setLabelSDDL(path, sddl)
		return nil
	})
}

// restoreLabels puts the disk back: every journalled root's tree is cleared of the mandatory
// label, then the paths that carried an explicit label before the run get theirs back
// verbatim, and the journal file is removed — but ONLY if all of that succeeded
// (retireLabelJournal). A failed revert keeps both the file and the in-memory record, so the
// labels it describes are still recoverable: the next NewConfiner retries them and
// ConfinementResidue reports them meanwhile. Callers hold c.mu.
func (c *tokenConfiner) restoreLabels() error {
	if err := retireLabelJournal(c.journalPath, c.journal, revertLabelJournal); err != nil {
		return fmt.Errorf("apogee: confine: could not revert every mandatory label; the journal %q is kept so the next run retries: %w",
			c.journalPath, err)
	}
	c.journal = labelJournal{}
	c.labelled = make(map[string]bool)
	return nil
}

// flushJournal persists the in-memory journal. Callers hold c.mu. The journal-less case is a
// belt on labelBox's braces — it refuses a box before anything is journalled or labelled, so a
// flush with no path can no longer accompany a disk mutation.
func (c *tokenConfiner) flushJournal() error {
	if c.journalPath == "" {
		return nil
	}
	c.journal.PID = os.Getpid()
	return writeLabelJournal(c.journalPath, c.journal)
}

// revertLabelJournal undoes one journal's disk mutation: clear the label from every object
// under each journalled root, then restore the prior descriptors. Clearing first and
// restoring second is the order that matters — a prior label inside a root would otherwise
// be wiped by the walk that follows it.
func revertLabelJournal(j labelJournal) error {
	var firstErr error
	for _, root := range j.roots() {
		if err := clearLabelTree(root); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for path, sddl := range j.priorLabels() {
		if err := setLabelSDDL(path, sddl); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// clearLabelTree removes the mandatory label from root and everything beneath it, returning
// it to the unlabelled (implicitly Medium) state it was in before the run. A path that has
// since been deleted is not an error — the tree is being restored, not reconstructed.
func clearLabelTree(root string) error {
	if err := setLabelSDDL(root, windowsClearLabelSDDL); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("apogee: confine: clear the mandatory label of %q: %w", root, err)
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || path == root {
			return nil
		}
		if info, err := entry.Info(); err == nil && info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		_ = setLabelSDDL(path, windowsClearLabelSDDL)
		return nil
	})
}

// recoverLabelJournals finishes the restore for every journal under home whose owning
// process is gone — ADR 0020 §2's interrupted-cleanup remedy. A journal belonging to a LIVE
// process is left strictly alone: it belongs to a concurrently running apogee whose labels
// are still in use, and reverting them would un-fence its session.
//
// A journal whose revert fails survives this pass (retireLabelJournal): recovery is
// best-effort — there is no user to tell at construction time — but it must never destroy the
// record of labels it did not manage to remove, so a later run gets another attempt.
//
// A journal that cannot be DECODED is likewise left where it is: it names no roots to revert
// and no owner to check, so acting on it is impossible and deleting it would throw away the
// only trace of whatever it described. It is not silent, though — ConfinementResidue reports
// it, which is the only way that state ever reaches a human.
func recoverLabelJournals(home string) {
	self := os.Getpid()
	for _, path := range listLabelJournals(home) {
		j, err := readLabelJournal(path)
		if err != nil {
			continue
		}
		if j.PID != self && processAlive(j.PID) {
			continue
		}
		_ = retireLabelJournal(path, j, revertLabelJournal)
	}
}

// processAlive reports whether pid names a running process, so recovery never reverts the
// labels of a live apogee. A PID that cannot be opened is treated as gone.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	const stillActive = 259 // STILL_ACTIVE
	return code == stillActive
}

// mintRestrictedLowToken mints the fence: a copy of this process's token with every
// privilege stripped (CreateRestrictedToken + DISABLE_MAX_PRIVILEGE) and its integrity level
// set to Low. CreateProcessAsUser accepts it without SeAssignPrimaryToken because it is a
// restricted version of the caller's own token — which is what makes the whole design
// reachable from an ordinary user account.
func mintRestrictedLowToken() (windows.Token, error) {
	var self windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_DUPLICATE|windows.TOKEN_QUERY|windows.TOKEN_ASSIGN_PRIMARY|windows.TOKEN_ADJUST_DEFAULT,
		&self); err != nil {
		return 0, fmt.Errorf("apogee: confine: OpenProcessToken: %w", err)
	}
	defer func() { _ = self.Close() }()

	var restricted windows.Token
	if ret, _, err := createRestrictedToken.Call(
		uintptr(self), disableMaxPrivilege,
		0, 0, // no deny-only SIDs
		0, 0, // no privileges to delete beyond DISABLE_MAX_PRIVILEGE
		0, 0, // no restricting SIDs (ADR 0020 §1)
		uintptr(unsafe.Pointer(&restricted)),
	); ret == 0 {
		return 0, fmt.Errorf("apogee: confine: CreateRestrictedToken: %w", err)
	}
	defer func() { _ = restricted.Close() }()

	// CreateRestrictedToken's result is usable as-is only with the access rights it was
	// granted; duplicating it produces the primary token with the access CreateProcessAsUser
	// needs, and leaves the intermediate handle to be closed here.
	var primary windows.Token
	if err := windows.DuplicateTokenEx(restricted, windows.TOKEN_ALL_ACCESS, nil,
		windows.SecurityImpersonation, windows.TokenPrimary, &primary); err != nil {
		return 0, fmt.Errorf("apogee: confine: DuplicateTokenEx: %w", err)
	}

	sid, err := windows.CreateWellKnownSid(windows.WinLowLabelSid)
	if err != nil {
		_ = primary.Close()
		return 0, fmt.Errorf("apogee: confine: CreateWellKnownSid(Low): %w", err)
	}
	label := windows.Tokenmandatorylabel{
		Label: windows.SIDAndAttributes{Sid: sid, Attributes: windows.SE_GROUP_INTEGRITY},
	}
	if err := windows.SetTokenInformation(primary, windows.TokenIntegrityLevel,
		(*byte)(unsafe.Pointer(&label)), label.Size()); err != nil {
		_ = primary.Close()
		return 0, fmt.Errorf("apogee: confine: SetTokenInformation(TokenIntegrityLevel=Low): %w", err)
	}
	return primary, nil
}

// readLabelSDDL returns the object's mandatory-label descriptor in SDDL form, or "" when it
// carries no explicit label (the state teardown restores by clearing). Only a descriptor
// actually containing a label ACE is reported, so the journal never records "S:" noise as
// something to put back.
func readLabelSDDL(path string) (string, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.LABEL_SECURITY_INFORMATION)
	if err != nil {
		return "", err
	}
	text := sd.String()
	if !strings.Contains(text, windowsLabelACEPrefix) {
		return "", nil
	}
	return text, nil
}

// setLabelSDDL writes sddl's SACL as the object's mandatory label. Only
// LABEL_SECURITY_INFORMATION is requested, which needs WRITE_OWNER on the object and no
// privilege — the caller owns its workspace, and asking for SACL_SECURITY_INFORMATION
// instead would demand SeSecurityPrivilege and fail for an ordinary user.
func setLabelSDDL(path, sddl string) error {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("parse label %q: %w", sddl, err)
	}
	sacl, _, err := sd.SACL()
	if err != nil {
		return fmt.Errorf("read label SACL from %q: %w", sddl, err)
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.LABEL_SECURITY_INFORMATION, nil, nil, nil, sacl)
}

// userProfileRoot returns the user-profile directory the labelling guardrails protect.
func userProfileRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// The backend must satisfy the Confiner contract, and the optional teardown interface the
// composition root asserts, at compile time.
var (
	_ domain.Confiner            = (*tokenConfiner)(nil)
	_ interface{ Close() error } = (*tokenConfiner)(nil)
)
