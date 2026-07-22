//go:build windows

package platform

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform/confinetest"
)

// The Windows counterpart of landlock_linux_test.go / seatbelt_darwin_test.go. Unlike those
// two it runs NATIVELY in this project's development loop (Phase 5 is executed on a Windows
// machine), so the escape battery here is not a deferred CI promise: a confined child really
// is minted a restricted Low-integrity token, really does write inside the box, and really is
// denied by the kernel's mandatory integrity check outside it.
//
// Rows #9 and #10 (contract §6.2, added by ADR 0020) are Windows-only and live here rather
// than in the shared harness, because they are about what this backend ADDS to the model: a
// disk mutation that must be undone, and a capability it must refuse to fake.

// newProbeConfiner builds a backend whose journal lives in a temp home and whose teardown
// runs at the end of the test, so a failure never leaves labels on the developer's disk.
func newProbeConfiner(t *testing.T) *tokenConfiner {
	t.Helper()
	c := newTokenConfiner(t.TempDir())
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestWindowsTokenProbe(t *testing.T) {
	// Not parallel: the confined children are real subprocesses, and the harness labels the
	// box roots on the real filesystem.
	confinetest.Probe(t, newProbeConfiner(t), Current())
}

func TestWindowsTokenProbeNetwork(t *testing.T) {
	// Skips by construction: the token backend reports NetworkEgress=false (ADR 0020 §4), so
	// rows #7/#8 go unproven on Windows — acceptable, because nothing is being enforced.
	confinetest.ProbeNetwork(t, newProbeConfiner(t), Current())
}

func TestWindowsTokenCapabilitiesHonest(t *testing.T) {
	// The facility probe: on a host at or above the floor with the token minted, fs-write is
	// enforceable and network egress is not — and that combination is Auto-eligible, because
	// AutoEligible() is FSWrite-only (ADR 0012), which is what makes the degradation notice
	// vanish here.
	c := newProbeConfiner(t)
	caps := c.Capabilities()
	if !caps.FSWrite {
		t.Fatalf("FSWrite = false on this host; the restricted token could not be minted (build floor is %d)", windowsFloorBuild)
	}
	if caps.NetworkEgress {
		t.Error("NetworkEgress = true; no token or integrity facility can express per-host egress (ADR 0020 §4)")
	}
	if !caps.AutoEligible() {
		t.Error("AutoEligible() = false; fs-write confinement alone must satisfy the Auto gate (ADR 0012)")
	}

	// The version floor is a fact about this host, asserted so a below-floor machine reports
	// the reason rather than a mysterious failure.
	if _, _, build := windows.RtlGetNtVersionNumbers(); build < windowsFloorBuild {
		t.Errorf("this host is build %d, below the %d floor; NewConfiner must return denyConfiner here", build, windowsFloorBuild)
	}
}

func TestWindowsNewConfinerSelectsTheTokenBackend(t *testing.T) {
	// The wiring assertion behind the acceptance criterion: on a capable Windows host the
	// per-OS selector must hand back the real backend, not denyConfiner — that is precisely
	// what makes probe.DegradedNotice return "" and the startup notice disappear.
	c := NewConfiner()
	if closer, ok := c.(interface{ Close() error }); ok {
		t.Cleanup(func() { _ = closer.Close() })
	}
	if _, ok := c.(*tokenConfiner); !ok {
		t.Fatalf("NewConfiner() = %T, want *tokenConfiner on a host at or above build %d", c, windowsFloorBuild)
	}
	if !c.Capabilities().AutoEligible() {
		t.Error("the backend NewConfiner selected is not Auto-eligible; the degradation notice would still fire")
	}
}

func TestWindowsConfineSetsOnlyTheToken(t *testing.T) {
	// Contract §9.2: Confine sets SysProcAttr.Token and NOTHING else on the cmd. There is no
	// argv sentinel and no re-exec — the Linux 42-liner has no Windows counterpart — so a
	// rewritten cmd.Path or cmd.Args would mean the design had drifted from ADR 0020 §1.
	c := newProbeConfiner(t)
	box := domain.ConfinementBox{WorkspaceRoot: t.TempDir()}
	cmd := exec.Command("cmd", "/c", "echo hi")
	wantPath, wantArgs := cmd.Path, append([]string(nil), cmd.Args...)

	// The teardown wires cmd.Cancel and the raw command line BEFORE Confine; both must
	// survive it, since each side only ever appends to SysProcAttr.
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd /c echo hi"}

	if err := c.Confine(context.Background(), box, cmd); err != nil {
		t.Fatalf("Confine: %v", err)
	}
	if cmd.Path != wantPath {
		t.Errorf("cmd.Path = %q, want it untouched (%q)", cmd.Path, wantPath)
	}
	if strings.Join(cmd.Args, " ") != strings.Join(wantArgs, " ") {
		t.Errorf("cmd.Args = %v, want them untouched (%v)", cmd.Args, wantArgs)
	}
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Token == 0 {
		t.Fatal("Confine did not set SysProcAttr.Token — nothing would be confined")
	}
	if cmd.SysProcAttr.CmdLine != "cmd /c echo hi" {
		t.Errorf("Confine clobbered SysProcAttr.CmdLine = %q", cmd.SysProcAttr.CmdLine)
	}
}

func TestWindowsConfineRefusesNetworkDenyBox(t *testing.T) {
	// Contract §6.2 row #10. A box asking for a network tightening the backend cannot enforce
	// is refused outright; it never runs network-open behind the user's back.
	c := newProbeConfiner(t)
	box := domain.ConfinementBox{
		WorkspaceRoot: t.TempDir(),
		NetworkAllow:  []string{"example.com:443"},
	}
	cmd := exec.Command("cmd", "/c", "echo hi")

	err := c.Confine(context.Background(), box, cmd)
	if err == nil {
		t.Fatal("Confine accepted a network-deny box; want ErrConfinementUnavailable")
	}
	if !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Errorf("err = %v; want ErrConfinementUnavailable so dispatch demotes to a forced Gate", err)
	}
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Token != 0 {
		t.Error("a refused Confine still handed the cmd a token")
	}
}

func TestWindowsConfineRefusesAGuardrailedRoot(t *testing.T) {
	// The guardrails are not merely a pure-function property: they must fire through the real
	// Confine, on this machine's real %SystemRoot%, and leave the disk untouched.
	c := newProbeConfiner(t)
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		t.Skip("%SystemRoot% is unset; cannot exercise the guardrail on this host")
	}
	cmd := exec.Command("cmd", "/c", "echo hi")

	err := c.Confine(context.Background(), domain.ConfinementBox{WorkspaceRoot: systemRoot}, cmd)
	if err == nil || !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Fatalf("Confine(%q) = %v; want an ErrConfinementUnavailable refusal", systemRoot, err)
	}
	if label, readErr := readLabelSDDL(systemRoot); readErr == nil && strings.Contains(label, ";LW)") {
		t.Fatalf("%%SystemRoot%% carries a Low label (%q) — the guardrail did not hold", label)
	}
}

func TestWindowsNoJournalHomeRefusesToLabel(t *testing.T) {
	// ADR 0020 §2's invariant with its last bypass closed: the journal is the ONLY record of
	// the disk mutation this backend makes, so a backend that has nowhere to write one must
	// label nothing at all. The trigger in the wild is os.UserHomeDir failing, which reaches
	// the backend as an empty home.
	c := newTokenConfiner("")
	t.Cleanup(func() { _ = c.Close() })

	// Capabilities answer for the FACILITY, which is present and untouched by the missing
	// profile — the refusal is the routine per-run kind contract §4 demotes to a forced Gate,
	// not an incapable host. (A host that cannot mint the token refuses for the other reason,
	// which would make the assertions below vacuous.)
	caps := c.Capabilities()
	if !caps.FSWrite {
		t.Skip("no restricted token on this host; the refusal under test cannot be told apart from the mint failure")
	}
	if !caps.AutoEligible() {
		t.Error("a journal-less backend reports itself Auto-ineligible; the facility is present and construction must be unaffected")
	}

	ws := t.TempDir()
	cmd := exec.Command("cmd", "/c", "echo hi")

	err := c.Confine(context.Background(), domain.ConfinementBox{WorkspaceRoot: ws}, cmd)
	if err == nil {
		t.Fatal("Confine labelled a box with no journal to undo it; want ErrConfinementUnavailable")
	}
	if !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Errorf("err = %v; want ErrConfinementUnavailable so dispatch demotes to a forced Gate", err)
	}
	label, readErr := readLabelSDDL(ws)
	if readErr != nil {
		t.Fatalf("read label of %q: %v", ws, readErr)
	}
	if label != "" {
		t.Errorf("%q carries the mandatory label %q; the refusal must precede every label read and write", ws, label)
	}
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Token != 0 {
		t.Error("a refused Confine still handed the cmd a token")
	}
}

func TestWindowsUnwritableJournalRefusesToLabel(t *testing.T) {
	// The other half of ADR 0020 §2's invariant, and the one a pure test cannot reach: the
	// journal location EXISTS but cannot be written. It is simulated by occupying the journal
	// directory's path with a file, so MkdirAll — and with it every journal write — fails. What
	// must follow is a refusal, not a labelled box: the flush failure happens between reading
	// the root's prior label and writing the first new one, so this pins the ordering ("journal
	// first, label second") as well as the fail-closed outcome.
	home := t.TempDir()
	if err := os.WriteFile(labelJournalDir(home), []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("occupy the journal directory's path with a file: %v", err)
	}

	c := newTokenConfiner(home)
	t.Cleanup(func() { _ = c.Close() })
	if !c.Capabilities().FSWrite {
		t.Skip("no restricted token on this host; the flush refusal cannot be told apart from the mint failure")
	}

	ws := t.TempDir()
	cmd := exec.Command("cmd", "/c", "echo hi")

	err := c.Confine(context.Background(), domain.ConfinementBox{WorkspaceRoot: ws}, cmd)
	if err == nil {
		t.Fatal("Confine labelled a box whose journal could not be written; want ErrConfinementUnavailable")
	}
	if !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Errorf("err = %v; want ErrConfinementUnavailable so dispatch demotes to a forced Gate", err)
	}
	if !strings.Contains(err.Error(), "label journal") {
		t.Errorf("err = %v; want the refusal to name the journal, so this pins the FLUSH failure and not some other guardrail", err)
	}
	label, readErr := readLabelSDDL(ws)
	if readErr != nil {
		t.Fatalf("read label of %q: %v", ws, readErr)
	}
	if label != "" {
		t.Errorf("%q carries the mandatory label %q; no label may precede the journal that undoes it", ws, label)
	}
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Token != 0 {
		t.Error("a refused Confine still handed the cmd a token")
	}
}

func TestWindowsLabelsAreRevertedOnTeardown(t *testing.T) {
	// Contract §6.2 row #9: the disk mutation is undone. The assertion is behavioural as well
	// as descriptive — after Close a confined child must be denied the very write that
	// succeeded while the box was up, which is what "back to their prior state" MEANS.
	c := newTokenConfiner(t.TempDir())
	if !c.Capabilities().FSWrite {
		t.Skip("no restricted token on this host; nothing to revert")
	}

	ws := t.TempDir()
	existing := filepath.Join(ws, "existing.txt")
	if err := os.WriteFile(existing, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed existing file: %v", err)
	}
	before, err := readLabelSDDL(ws)
	if err != nil {
		t.Fatalf("read label of %q: %v", ws, err)
	}
	if before != "" {
		t.Fatalf("the fresh temp dir already carries a label (%q); the test cannot tell a revert from a no-op", before)
	}

	box := domain.ConfinementBox{WorkspaceRoot: ws}
	if err := runConfinedLine(t, c, box, filepath.Join(ws, "inbox.txt")); err != nil {
		t.Fatalf("an in-box write failed while the box was up: %v", err)
	}

	// While the box is up, the root and its pre-existing contents are labelled Low — the
	// second half is the one that matters, since inheritance covers new objects only.
	if label, _ := readLabelSDDL(ws); !strings.Contains(label, ";LW)") {
		t.Fatalf("box root label = %q, want a Low mandatory label while the box is up", label)
	}
	if label, _ := readLabelSDDL(existing); !strings.Contains(label, ";LW)") {
		t.Fatalf("pre-existing file label = %q; the walk must reach files that predate the box", label)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close (teardown): %v", err)
	}

	for _, path := range []string{ws, existing, filepath.Join(ws, "inbox.txt")} {
		label, err := readLabelSDDL(path)
		if err != nil {
			t.Fatalf("read label of %q after teardown: %v", path, err)
		}
		if label != "" {
			t.Errorf("%q still carries a mandatory label after teardown: %q", path, label)
		}
	}

	// The behavioural half: a fresh backend's confined child, aimed at the now-unlabelled
	// directory, is denied — the box is genuinely gone, not merely described as gone.
	after := newTokenConfiner(t.TempDir())
	t.Cleanup(func() { _ = after.Close() })
	elsewhere := domain.ConfinementBox{WorkspaceRoot: t.TempDir()}
	target := filepath.Join(ws, "after-teardown.txt")
	if err := runConfinedLine(t, after, elsewhere, target); err == nil {
		t.Error("a confined write into the reverted directory succeeded; the label was not really removed")
	}
	if _, err := os.Stat(target); err == nil {
		t.Error("the reverted directory accepted a Low write; teardown left it writable")
	}
}

func TestWindowsForeignPriorLabelIsRestoredOnTeardown(t *testing.T) {
	// The one behaviour the journal machinery exists to deliver, pinned end-to-end: a file
	// that carried a FOREIGN explicit label before the run gets that exact label back after
	// teardown, while the paths apogee labelled itself read back unlabelled. Every piece of
	// this — journalling the prior before the label lands, clearing the trees FIRST and
	// restoring priors SECOND (revertLabelJournal's order comment) — is otherwise invisible
	// to the suite: deleting the prior-restore loop, or swapping the clear/restore order,
	// passes every other test.
	home := t.TempDir()
	ws := t.TempDir()
	child := filepath.Join(ws, "foreign.txt")
	sibling := filepath.Join(ws, "sibling.txt")
	for _, path := range []string{child, sibling} {
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatalf("seed %q: %v", path, err)
		}
	}

	c := newTokenConfiner(home)
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = c.Close()
		}
	})
	if !c.Capabilities().FSWrite {
		t.Skip("no restricted token on this host; nothing to label or restore")
	}

	// Plant the foreign prior with the same helper the backend labels with — an explicit
	// Medium label, i.e. one apogee never writes — then capture the OS's own rendering of
	// it: the journal records readLabelSDDL's output, so THAT string, not the spelling
	// written here, is the verbatim the restore must reproduce.
	if err := setLabelSDDL(child, "S:(ML;;NW;;;ME)"); err != nil {
		t.Fatalf("apply the foreign Medium label to %q: %v", child, err)
	}
	foreignPrior, err := readLabelSDDL(child)
	if err != nil {
		t.Fatalf("read the planted label of %q: %v", child, err)
	}
	if foreignPrior == "" || isLowLabelSDDL(foreignPrior) {
		t.Fatalf("planted label reads back as %q; the test needs a non-empty, non-Low prior", foreignPrior)
	}

	if err := c.labelBox(domain.ConfinementBox{WorkspaceRoot: ws}); err != nil {
		t.Fatalf("labelBox: %v", err)
	}

	// While the box is up, the foreign-prior file is Low like everything else — the
	// restore below is only meaningful because the label pass really overwrote the prior.
	if label, _ := readLabelSDDL(child); !isLowLabelSDDL(label) {
		t.Fatalf("foreign-prior file label = %q while the box is up, want apogee's Low label over the prior", label)
	}

	// The ON-DISK journal — what a crash recovery would replay — carries the prior
	// verbatim for the child, beside the root's own entry.
	journals := listLabelJournals(home)
	if len(journals) != 1 {
		t.Fatalf("journals = %v, want exactly one", journals)
	}
	onDisk, err := readLabelJournal(journals[0])
	if err != nil {
		t.Fatalf("read journal %q: %v", journals[0], err)
	}
	var childEntry *labelJournalEntry
	for i := range onDisk.Entries {
		if strings.EqualFold(onDisk.Entries[i].Path, child) {
			childEntry = &onDisk.Entries[i]
		}
	}
	if childEntry == nil {
		t.Fatalf("journal entries = %+v; no entry journals the foreign prior of %q", onDisk.Entries, child)
	}
	if childEntry.PriorSDDL != foreignPrior {
		t.Errorf("journalled prior = %q, want the planted descriptor %q verbatim", childEntry.PriorSDDL, foreignPrior)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close (teardown): %v", err)
	}
	closed = true

	// The restore: the foreign Medium label is back verbatim — NOT cleared to "" — while
	// the root and the sibling are unlabelled again. Restored, not wiped wholesale.
	restored, err := readLabelSDDL(child)
	if err != nil {
		t.Fatalf("read label of %q after teardown: %v", child, err)
	}
	if restored == "" {
		t.Fatal("the foreign Medium label was cleared, not restored; teardown destroyed a label apogee did not write")
	}
	if restored != foreignPrior {
		t.Errorf("label after teardown = %q, want the prior %q back verbatim", restored, foreignPrior)
	}
	for _, path := range []string{ws, sibling} {
		label, err := readLabelSDDL(path)
		if err != nil {
			t.Fatalf("read label of %q after teardown: %v", path, err)
		}
		if label != "" {
			t.Errorf("%q still carries a mandatory label after teardown: %q", path, label)
		}
	}
	if left := listLabelJournals(home); len(left) != 0 {
		t.Errorf("journals = %v after teardown, want the fully reverted journal retired", left)
	}
}

func TestWindowsUnreadablePriorDescendantIsNotLabelled(t *testing.T) {
	// The unreadable-prior rung of descendantLabelDecision, on a real DACL. The child's DACL
	// withholds READ_CONTROL from the current user (an OWNER_RIGHTS ACE displaces the owner's
	// implicit grant) while keeping WRITE_OWNER, so the walk's prior read is denied by the
	// kernel — the split under which the walk used to fall through to the label write with no
	// journalled prior, a possibly-foreign label destroyed with no record. The path must
	// instead be skipped entirely: no Low label, no journal entry, and the rest of the box
	// labelled as normal. (The skip-versus-attempt distinction itself is pinned by the
	// untagged TestDescendantLabelDecision; this proves the wiring against a real deny.)
	home := t.TempDir()
	ws := t.TempDir()
	child := filepath.Join(ws, "opaque.txt")
	sibling := filepath.Join(ws, "sibling.txt")
	for _, path := range []string{child, sibling} {
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatalf("seed %q: %v", path, err)
		}
	}

	c := newTokenConfiner(home)
	t.Cleanup(func() { _ = c.Close() })
	if !c.Capabilities().FSWrite {
		t.Skip("no restricted token on this host; nothing to label")
	}

	// Replace the child's DACL with a protected one granting the owner
	// WRITE_DAC|WRITE_OWNER|DELETE|SYNCHRONIZE|FILE_READ_ATTRIBUTES (0x1d0080) and nothing
	// else — everything the restore below and the temp-dir cleanup need, minus READ_CONTROL.
	// SYNCHRONIZE and FILE_READ_ATTRIBUTES must be granted because CreateFile implicitly
	// requests them on top of the WRITE_DAC the restore handle asks for.
	setFileDACL(t, child, "D:P(A;;0x1d0080;;;OW)")
	t.Cleanup(func() { restoreFileDACL(t, child, "D:(A;;FA;;;WD)") })
	if _, err := readLabelSDDL(child); err == nil {
		t.Skip("this host can read a label through a deny-READ_CONTROL DACL (elevated?); the rung cannot be exercised")
	}

	if err := c.labelBox(domain.ConfinementBox{WorkspaceRoot: ws}); err != nil {
		t.Fatalf("labelBox: %v", err)
	}

	// The journal — in memory and the on-disk record a crash recovery would replay — carries
	// NO entry for the child: an entry could only describe a prior the read never delivered.
	journals := listLabelJournals(home)
	if len(journals) != 1 {
		t.Fatalf("journals = %v, want exactly one", journals)
	}
	onDisk, err := readLabelJournal(journals[0])
	if err != nil {
		t.Fatalf("read journal %q: %v", journals[0], err)
	}
	for _, j := range []labelJournal{onDisk, c.journal} {
		for _, entry := range j.Entries {
			if strings.EqualFold(entry.Path, child) {
				t.Errorf("journal entry %+v names the unreadable-prior child; nothing may be journalled for a path the walk skipped", entry)
			}
		}
	}

	// Restore the DACL to read the child back: it carries NO Low label — the walk skipped
	// it — while the root and the sibling are labelled as normal, so the one opaque path did
	// not gate the box.
	restoreFileDACL(t, child, "D:(A;;FA;;;WD)")
	label, err := readLabelSDDL(child)
	if err != nil {
		t.Fatalf("read label of %q after restoring its DACL: %v", child, err)
	}
	if label != "" {
		t.Errorf("the unreadable-prior child carries the label %q; a path whose prior could not be read must not be labelled", label)
	}
	for _, path := range []string{ws, sibling} {
		if got, _ := readLabelSDDL(path); !isLowLabelSDDL(got) {
			t.Errorf("label of %q = %q, want apogee's Low label — one opaque descendant must not gate the box", path, got)
		}
	}
}

// setFileDACL replaces path's DACL from sddl via the named-object API, protecting it from
// inherited ACEs so the grants written there are the only ones in force. That API needs
// READ_CONTROL as well as WRITE_DAC (it reads the current descriptor to propagate
// inheritance), so it can RESTRICT a readable file but cannot put back the DACL of one this
// test has made unreadable — restoreFileDACL is the way back.
func setFileDACL(t *testing.T, path, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatalf("parse DACL %q: %v", sddl, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatalf("read DACL from %q: %v", sddl, err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil); err != nil {
		t.Fatalf("set DACL of %q: %v", path, err)
	}
}

// restoreFileDACL replaces path's DACL through a WRITE_DAC handle — the one access the
// restricted DACL still grants. The handle-based kernel API performs no inheritance
// propagation, so unlike SetNamedSecurityInfo it never needs to READ the descriptor it is
// replacing.
func restoreFileDACL(t *testing.T, path, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatalf("parse DACL %q: %v", sddl, err)
	}
	pathW, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("encode %q: %v", path, err)
	}
	handle, err := windows.CreateFile(pathW, windows.WRITE_DAC,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("open %q for WRITE_DAC: %v", path, err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	if err := windows.SetKernelObjectSecurity(handle, windows.DACL_SECURITY_INFORMATION, sd); err != nil {
		t.Fatalf("restore DACL of %q: %v", path, err)
	}
}

func TestWindowsUnclearableDescendantKeepsTheJournal(t *testing.T) {
	// Half (a) of the verified-revert item: teardown may retire the journal only when every
	// label it describes is verifiably gone. A descendant whose clear fails at Close — here a
	// DACL withholding WRITE_OWNER, the access the label write needs, planted after the box
	// was labelled the way a confined child (which owns in-box objects) can — must fail the
	// revert, so the journal survives and the next session's recovery finishes the job once
	// the obstacle is gone. Before this, clearLabelTree swallowed every descendant failure
	// and the Low label was stranded with no record and no residue report.
	home := t.TempDir()
	ws := t.TempDir()
	child := filepath.Join(ws, "stuck.txt")
	if err := os.WriteFile(child, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed %q: %v", child, err)
	}

	c := newTokenConfiner(home)
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = c.Close()
		}
	})
	if !c.Capabilities().FSWrite {
		t.Skip("no restricted token on this host; nothing to label")
	}

	if err := c.labelBox(domain.ConfinementBox{WorkspaceRoot: ws}); err != nil {
		t.Fatalf("labelBox: %v", err)
	}
	if label, _ := readLabelSDDL(child); !isLowLabelSDDL(label) {
		t.Fatalf("child label = %q, want apogee's Low label before the revert is obstructed", label)
	}

	// Withhold WRITE_OWNER while keeping READ_CONTROL and the rights the cleanup below needs
	// (READ_CONTROL|WRITE_DAC|DELETE|SYNCHRONIZE|FILE_READ_ATTRIBUTES = 0x170080), so the
	// clear's label write — and only that write — is denied by the kernel.
	setFileDACL(t, child, "D:P(A;;0x170080;;;OW)")
	t.Cleanup(func() { restoreFileDACL(t, child, "D:(A;;FA;;;WD)") })

	err := c.Close()
	closed = true
	if err == nil {
		t.Fatal("Close reported success while a descendant kept its Low label; the journal would be retired over live residue")
	}
	if journals := listLabelJournals(home); len(journals) != 1 {
		t.Fatalf("journals = %v after the failed revert, want the journal kept so the next run retries", journals)
	}

	// The keep is what makes the retry real: with the DACL healed, the next session's
	// recovery finishes the revert and retires the journal.
	restoreFileDACL(t, child, "D:(A;;FA;;;WD)")
	recovered := newTokenConfiner(home)
	t.Cleanup(func() { _ = recovered.Close() })
	if label, _ := readLabelSDDL(child); label != "" {
		t.Errorf("child still labelled %q after recovery; the kept journal must let the next run finish the revert", label)
	}
	if left := listLabelJournals(home); len(left) != 0 {
		t.Errorf("journals = %v after recovery, want the fully reverted journal retired", left)
	}
}

func TestWindowsDeletedPriorLabelledPathDoesNotWedgeTheRevert(t *testing.T) {
	// Half (b) of the verified-revert item: a prior-labelled file the agent deleted is a
	// COMPLETED revert, not a failure — there is no object left to carry the label. Before
	// this, the prior-restore loop failed on it forever: Close warned every session, recovery
	// retried and failed every startup, and the only remedy was deleting the journal by hand.
	home := t.TempDir()
	ws := t.TempDir()
	child := filepath.Join(ws, "foreign.txt")
	if err := os.WriteFile(child, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed %q: %v", child, err)
	}
	if err := setLabelSDDL(child, "S:(ML;;NW;;;ME)"); err != nil {
		t.Fatalf("apply the foreign Medium label to %q: %v", child, err)
	}

	c := newTokenConfiner(home)
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = c.Close()
		}
	})
	if !c.Capabilities().FSWrite {
		t.Skip("no restricted token on this host; nothing to label")
	}

	if err := c.labelBox(domain.ConfinementBox{WorkspaceRoot: ws}); err != nil {
		t.Fatalf("labelBox: %v", err)
	}
	// The journal really carries a restore obligation for the child — otherwise the delete
	// below would prove nothing.
	prior, found := c.journal.priorLabels()[child]
	if !found || prior == "" {
		t.Fatalf("journal priors = %v; the foreign prior of %q was not journalled", c.journal.priorLabels(), child)
	}

	// Routine workspace activity: the (simulated) agent deletes the prior-labelled file.
	if err := os.Remove(child); err != nil {
		t.Fatalf("delete %q: %v", child, err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close = %v; a vanished prior-labelled path must not fail the revert", err)
	}
	closed = true
	if left := listLabelJournals(home); len(left) != 0 {
		t.Errorf("journals = %v after teardown, want the journal retired — the revert is complete", left)
	}
	if label, err := readLabelSDDL(ws); err != nil || label != "" {
		t.Errorf("root label = %q (err %v) after teardown, want the disk clean", label, err)
	}
	if got := confinementResidue(home); got != "" {
		t.Errorf("confinementResidue = %q after teardown, want nothing outstanding", got)
	}
}

func TestWindowsInterruptedRunIsRecoveredFromTheJournal(t *testing.T) {
	// ADR 0020 §2's interrupted-cleanup remedy. A process killed mid-run leaves labels and a
	// journal; the next NewConfiner finishes the restore. It is simulated by dropping the
	// backend on the floor (no Close) and constructing a second one against the same home.
	home := t.TempDir()
	ws := t.TempDir()

	crashed := newTokenConfiner(home)
	if !crashed.Capabilities().FSWrite {
		t.Skip("no restricted token on this host; nothing to journal")
	}
	if err := crashed.labelBox(domain.ConfinementBox{WorkspaceRoot: ws}); err != nil {
		t.Fatalf("labelBox: %v", err)
	}
	if label, _ := readLabelSDDL(ws); !strings.Contains(label, ";LW)") {
		t.Fatalf("box root label = %q, want Low before the simulated crash", label)
	}
	journals := listLabelJournals(home)
	if len(journals) != 1 {
		t.Fatalf("journals = %v, want exactly one written BEFORE the labels", journals)
	}
	// Hand the journal to a dead PID: recovery must never revert a LIVE apogee's labels, so
	// a journal still owned by this process is deliberately left alone.
	rewriteJournalPID(t, home, journals[0])

	recovered := newTokenConfiner(home)
	t.Cleanup(func() { _ = recovered.Close() })

	if label, _ := readLabelSDDL(ws); label != "" {
		t.Errorf("%q still labelled %q after recovery; the next NewConfiner must finish the restore", ws, label)
	}
	if left := listLabelJournals(home); len(left) != 0 {
		t.Errorf("journals = %v after recovery, want the recovered one removed", left)
	}
	if got := confinementResidue(home); got != "" {
		t.Errorf("confinementResidue = %q after recovery, want nothing outstanding", got)
	}
}

func TestWindowsReportConstructionLeavesAnOutstandingJournalAlone(t *testing.T) {
	// The constructor half of ADR 0021 §1's read-only pledge. `apogee probe host` builds a real
	// backend to describe this machine, and on Windows the SESSION constructor finishes an
	// interrupted run's restore — a revert and a delete. Doing that on the report path would
	// break the pledge and, worse, destroy the state the report exists to state: ADR 0020 §2
	// promises the host report surfaces an outstanding journal, which is unreachable if the
	// backend consumed it first. The report constructor therefore skips recovery, and its
	// session twin still performs it (TestWindowsInterruptedRunIsRecoveredFromTheJournal), which
	// is what makes this a real distinction rather than a no-op.
	home := t.TempDir()
	ws := t.TempDir()

	crashed := newTokenConfiner(home)
	if !crashed.Capabilities().FSWrite {
		t.Skip("no restricted token on this host; nothing to journal")
	}
	if err := crashed.labelBox(domain.ConfinementBox{WorkspaceRoot: ws}); err != nil {
		t.Fatalf("labelBox: %v", err)
	}
	journals := listLabelJournals(home)
	if len(journals) != 1 {
		t.Fatalf("journals = %v, want exactly one written BEFORE the labels", journals)
	}
	// Dropped on the floor (no Close) and re-owned by a dead PID: an interrupted run, which is
	// precisely the state recovery would act on.
	rewriteJournalPID(t, home, journals[0])

	report := newTokenConfinerWithoutRecovery(home)
	t.Cleanup(func() { _ = report.Close() })

	if left := listLabelJournals(home); len(left) != 1 {
		t.Errorf("journals = %v after the report backend was constructed, want the outstanding one untouched", left)
	}
	if label, _ := readLabelSDDL(ws); !strings.Contains(label, ";LW)") {
		t.Errorf("the report constructor reverted %q (label = %q); the host half writes nothing", ws, label)
	}
	if got := confinementResidue(home); !strings.Contains(got, ws) {
		t.Errorf("confinementResidue = %q; want the interrupted run still reportable after the report backend exists", got)
	}

	// And the deferral costs nothing: the next SESSION constructor finishes the restore, which
	// also leaves this test's disk clean.
	session := newTokenConfiner(home)
	t.Cleanup(func() { _ = session.Close() })
	if label, _ := readLabelSDDL(ws); label != "" {
		t.Errorf("%q still labelled %q; a session constructor must finish the restore the report deferred", ws, label)
	}
}

func TestWindowsRecoveryLeavesALiveProcessAlone(t *testing.T) {
	// The other side of recovery: a journal owned by a process that is still running belongs
	// to a concurrently running apogee whose labels are in use. Reverting them would un-fence
	// that session, so recovery must skip it — and the host report must still surface it.
	home := t.TempDir()
	ws := t.TempDir()

	live := newTokenConfiner(home)
	if !live.Capabilities().FSWrite {
		t.Skip("no restricted token on this host; nothing to journal")
	}
	t.Cleanup(func() { _ = live.Close() })
	if err := live.labelBox(domain.ConfinementBox{WorkspaceRoot: ws}); err != nil {
		t.Fatalf("labelBox: %v", err)
	}
	// Re-own the journal by a different but definitely-alive PID (this process's parent
	// stand-in: our own PID is skipped by identity, so use the live-process branch instead).
	rewriteJournalOwner(t, home, listLabelJournals(home)[0], liveForeignPID(t))

	second := newTokenConfiner(home) // a second apogee starting up
	t.Cleanup(func() { _ = second.Close() })
	if label, _ := readLabelSDDL(ws); !strings.Contains(label, ";LW)") {
		t.Errorf("a second apogee reverted a LIVE run's label (%q); its session would be un-fenced", label)
	}
	if got := confinementResidue(home); !strings.Contains(got, ws) {
		t.Errorf("confinementResidue = %q; want the outstanding journal reported off-session", got)
	}
}

func TestWindowsRelabellingNeverJournalsApogeesOwnLabel(t *testing.T) {
	// The self-perpetuating-residue scenario, on a real SACL. Both triggers are exercised: a
	// box labelled a SECOND time by the same backend (a partial pass, or the once-per-box memo
	// reopened), and a second backend reading a box another session has already labelled. Under
	// either, a journal that recorded apogee's own Low label as "prior state" would make
	// teardown RE-APPLY it, and the residue would survive every future cleanup.
	home := t.TempDir()
	ws := t.TempDir()
	existing := filepath.Join(ws, "existing.txt")
	if err := os.WriteFile(existing, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed existing file: %v", err)
	}

	first := newTokenConfiner(home)
	if !first.Capabilities().FSWrite {
		t.Skip("no restricted token on this host; nothing to journal")
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = first.Close()
		}
	})

	box := domain.ConfinementBox{WorkspaceRoot: ws}
	if err := first.labelBox(box); err != nil {
		t.Fatalf("labelBox: %v", err)
	}
	if label, _ := readLabelSDDL(ws); !strings.Contains(label, ";LW)") {
		t.Fatalf("box root label = %q, want Low before the second pass", label)
	}

	// Reopen the once-per-box memo: the label pass now runs over a tree that already carries
	// apogee's own labels, which is exactly what it reads as "prior state".
	first.mu.Lock()
	first.labelled = make(map[string]bool)
	first.mu.Unlock()
	if err := first.labelBox(box); err != nil {
		t.Fatalf("second labelBox over the same box: %v", err)
	}

	// A second backend with its own journal home, over the still-labelled box — the concurrent
	// session's read of a transient Low label. (Its own home keeps construction from recovering
	// the first backend's journal, which shares this process's PID.)
	second := newTokenConfiner(t.TempDir())
	secondClosed := false
	t.Cleanup(func() {
		if !secondClosed {
			_ = second.Close()
		}
	})
	if err := second.labelBox(box); err != nil {
		t.Fatalf("a second backend could not label the already-labelled box: %v", err)
	}

	journals := listLabelJournals(home)
	if len(journals) != 1 {
		t.Fatalf("journals under %q = %v, want exactly one", home, journals)
	}
	onDisk, err := readLabelJournal(journals[0])
	if err != nil {
		t.Fatalf("read journal %q: %v", journals[0], err)
	}
	for _, j := range []labelJournal{onDisk, second.journal} {
		for _, entry := range j.Entries {
			if isLowLabelSDDL(entry.PriorSDDL) {
				t.Errorf("journal entry %+v records apogee's own Low label as prior state; the revert would put it back", entry)
			}
		}
	}
	if len(onDisk.Entries) != 1 || !onDisk.Entries[0].Root {
		t.Errorf("journal entries = %+v, want the single box root recorded once", onDisk.Entries)
	}

	if err := second.Close(); err != nil {
		t.Fatalf("second Close (teardown): %v", err)
	}
	secondClosed = true
	if err := first.Close(); err != nil {
		t.Fatalf("Close (teardown): %v", err)
	}
	closed = true

	for _, path := range []string{ws, existing} {
		label, err := readLabelSDDL(path)
		if err != nil {
			t.Fatalf("read label of %q after teardown: %v", path, err)
		}
		if label != "" {
			t.Errorf("%q still carries a mandatory label after teardown: %q", path, label)
		}
	}
}

// runConfinedLine runs a confined `cmd /c echo x> "<target>"` and returns the run error.
func runConfinedLine(t *testing.T, c domain.Confiner, box domain.ConfinementBox, target string) error {
	t.Helper()
	sh := Current()
	line := "echo x> " + sh.Quote(target)
	argv := sh.Command(line)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: sh.CommandLine(line)}
	if err := c.Confine(context.Background(), box, cmd); err != nil {
		return err
	}
	return cmd.Run()
}

// rewriteJournalPID re-owns a journal by a PID that is certainly gone, so the recovery path
// treats it as an interrupted run.
func rewriteJournalPID(t *testing.T, home, path string) {
	t.Helper()
	rewriteJournalOwner(t, home, path, deadPID(t))
}

// rewriteJournalOwner rewrites a journal under the PID-named file its new owner would use.
func rewriteJournalOwner(t *testing.T, home, path string, pid int) {
	t.Helper()
	journal, err := readLabelJournal(path)
	if err != nil {
		t.Fatalf("read journal %q: %v", path, err)
	}
	journal.PID = pid
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove journal %q: %v", path, err)
	}
	if err := writeLabelJournal(labelJournalPath(home, pid), journal); err != nil {
		t.Fatalf("rewrite journal: %v", err)
	}
}

// deadPID returns a PID that is not running, by starting a process and waiting for it to exit.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("cmd", "/c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Fatalf("start a throwaway process: %v", err)
	}
	pid := cmd.Process.Pid
	if processAlive(pid) {
		t.Skipf("pid %d is still reported alive after exiting; cannot synthesise a dead owner", pid)
	}
	return pid
}

// liveForeignPID returns the PID of a process that is running and is not this one. It must
// outlive the assertion without needing input — a `pause` with no console reads EOF and exits
// immediately, which would make the test assert against a dead PID.
func liveForeignPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("cmd", "/c", "ping -n 60 127.0.0.1 >nul")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start a long-running process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	if !processAlive(cmd.Process.Pid) {
		t.Skipf("pid %d is not reported alive; cannot synthesise a live owner", cmd.Process.Pid)
	}
	return cmd.Process.Pid
}
