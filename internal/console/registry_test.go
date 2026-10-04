package console

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
)

// registryTestTimeout bounds every wait in this file: long enough that a loaded CI host does not
// flake, short enough that a genuinely wedged Console fails the test instead of the suite.
const registryTestTimeout = 10 * time.Second

// TestRegistryAssignsMonotonicIDsNeverReusingAClosedOne pins the id contract the model depends on:
// a stale id in its context can never come back pointing at somebody else's process.
func TestRegistryAssignsMonotonicIDsNeverReusingAClosedOne(t *testing.T) {
	t.Parallel()
	requireConsoleBackend(t)

	registry := newTestRegistry(t)
	first := openTestShell(t, registry, "")
	second := openTestShell(t, registry, "")
	if first.ID != 1 || second.ID != 2 {
		t.Fatalf("ids = %d, %d; want 1, 2", first.ID, second.ID)
	}

	if err := registry.Close(first.ID); err != nil {
		t.Fatalf("Close(%d): %v", first.ID, err)
	}
	third := openTestShell(t, registry, "")

	if third.ID != 3 {
		t.Fatalf("id after closing 1 = %d, want 3", third.ID)
	}
	if ids := registry.OpenIDs(); len(ids) != 2 || ids[0] != 2 || ids[1] != 3 {
		t.Fatalf("OpenIDs() = %v, want [2 3]", ids)
	}
}

// TestRegistryRefusesToOpenPastTheCap covers the fixed cap (ADR 0059 §6) and, as much as the cap
// itself, the refusal's text: the model is told which Consoles it already has so it can close one.
func TestRegistryRefusesToOpenPastTheCap(t *testing.T) {
	t.Parallel()
	requireConsoleBackend(t)

	registry := newTestRegistry(t)
	for opened := 0; opened < MaxOpen; opened++ {
		openTestShell(t, registry, "")
	}

	_, err := registry.Open(OpenSpec{Command: "sh", Argv: []string{"sh"}})

	if !errors.Is(err, ErrTooMany) {
		t.Fatalf("open past the cap = %v, want ErrTooMany", err)
	}
	if !strings.Contains(err.Error(), "1, 2, 3, 4") {
		t.Errorf("refusal %q does not name the open ids", err)
	}
}

// TestRegistryKeepsAnExitedConsoleUntilItIsClosed pins that exiting is not closing: the process is
// gone but its id, its last words and its exit code are still there to be read, and it still
// counts against the cap until someone closes it.
func TestRegistryKeepsAnExitedConsoleUntilItIsClosed(t *testing.T) {
	t.Parallel()
	requireConsoleBackend(t)

	registry := newTestRegistry(t)
	exiting, err := registry.Open(OpenSpec{
		Command: "echo bye; exit 7",
		Argv:    []string{"sh", "-c", "echo bye; exit 7"},
	})
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}

	awaitRegistry(t, func() bool { return !exiting.Alive() }, "the command to exit")

	found, ok := registry.Get(exiting.ID)
	if !ok || found != exiting {
		t.Fatalf("Get(%d) = %v, %t; want the exited Console", exiting.ID, found, ok)
	}
	if code := found.ExitCode(); code != 7 {
		t.Errorf("ExitCode() = %d, want 7", code)
	}
	if tail := readRegistryUntil(t, found, "bye"); !strings.Contains(tail, "bye") {
		t.Errorf("tail %q lost the output the command printed before exiting", tail)
	}
	for opened := 1; opened < MaxOpen; opened++ {
		openTestShell(t, registry, "")
	}
	if _, err := registry.Open(OpenSpec{Command: "sh", Argv: []string{"sh"}}); !errors.Is(err, ErrTooMany) {
		t.Errorf("open with an exited Console still registered = %v, want ErrTooMany", err)
	}
}

// TestRegistryCloseOwnedByClosesOnlyThatDelegationsConsoles is the delegation-end sweep: a
// sub-agent's Consoles die with it, and the ones the top-level agent opened — owner "" — do not.
func TestRegistryCloseOwnedByClosesOnlyThatDelegationsConsoles(t *testing.T) {
	t.Parallel()
	requireConsoleBackend(t)

	registry := newTestRegistry(t)
	delegated := openTestShell(t, registry, "call-7")
	sibling := openTestShell(t, registry, "call-9")
	topLevel := openTestShell(t, registry, "")

	registry.CloseOwnedBy("call-7")

	if ids := registry.OpenIDs(); len(ids) != 2 || ids[0] != sibling.ID || ids[1] != topLevel.ID {
		t.Fatalf("OpenIDs() = %v, want the sibling's and the top-level's", ids)
	}
	if delegated.Alive() {
		t.Error("the delegation's Console is still running after its owner ended")
	}
	if !sibling.Alive() || !topLevel.Alive() {
		t.Error("CloseOwnedBy killed a Console another owner holds")
	}
}

// TestRegistryCloseIsFinalForThatID pins that a close both stops the process and forgets the id,
// so a second close is an error the caller can report rather than a silent no-op.
func TestRegistryCloseIsFinalForThatID(t *testing.T) {
	t.Parallel()
	requireConsoleBackend(t)

	registry := newTestRegistry(t)
	console := openTestShell(t, registry, "")

	if err := registry.Close(console.ID); err != nil {
		t.Fatalf("Close(%d): %v", console.ID, err)
	}

	if console.Alive() {
		t.Error("the Console is still running after Close returned")
	}
	if _, ok := registry.Get(console.ID); ok {
		t.Errorf("Get(%d) still finds a closed Console", console.ID)
	}
	if err := registry.Close(console.ID); !errors.Is(err, ErrUnknown) {
		t.Errorf("second Close(%d) = %v, want ErrUnknown", console.ID, err)
	}
}

// TestRegistryCloseReportsAnIDItNeverIssued covers the other unknown-id case: an id no Console
// ever had, which is also what a model's id from before a restart looks like from here.
func TestRegistryCloseReportsAnIDItNeverIssued(t *testing.T) {
	t.Parallel()

	err := New().Close(9)

	if !errors.Is(err, ErrUnknown) {
		t.Fatalf("Close(9) on an empty registry = %v, want ErrUnknown", err)
	}
	if !strings.Contains(err.Error(), "9") {
		t.Errorf("error %q does not name the id", err)
	}
}

// TestRegistryClosingToleratesANilOrEmptyRegistry pins the nil contract the engine's shutdown path
// relies on: an engine that never opened a Console — or never built a registry at all — still runs
// the same sweeps at the end of a delegation and at exit.
func TestRegistryClosingToleratesANilOrEmptyRegistry(t *testing.T) {
	t.Parallel()

	var absent *Registry
	absent.CloseOwnedBy("call-7")
	absent.CloseAll()

	empty := New()
	empty.CloseOwnedBy("call-7")
	empty.CloseAll()

	if ids := empty.OpenIDs(); len(ids) != 0 {
		t.Fatalf("OpenIDs() = %v on an empty registry, want none", ids)
	}
}

// TestRegistryMintOwnerIssuesDistinctNonEmptyKeys pins the privilege namespace the ownership sweep
// matches on. The registry that COMPARES the key mints it, so no two delegations of one engine can
// share one however the model numbers its tool calls, and no minted key can collide with the ""
// that means "the top level owns this".
func TestRegistryMintOwnerIssuesDistinctNonEmptyKeys(t *testing.T) {
	t.Parallel()

	registry := New()
	seen := make(map[string]bool, 100)
	for range 100 {
		key := registry.MintOwner()
		if key == "" {
			t.Fatal(`MintOwner() = "", want a key no top-level Console can be mistaken for`)
		}
		if seen[key] {
			t.Fatalf("MintOwner() reissued %q", key)
		}
		seen[key] = true
	}
}

// TestRegistryMintOwnerOnANilRegistryIsTheTopLevelKey pins the other half of the nil contract: an
// engine with no registry holds no Console for a key to reach, so minting on one is the honest ""
// rather than a panic the caller has to guard against.
func TestRegistryMintOwnerOnANilRegistryIsTheTopLevelKey(t *testing.T) {
	t.Parallel()

	var absent *Registry

	if key := absent.MintOwner(); key != "" {
		t.Fatalf("MintOwner() on a nil registry = %q, want the top-level key", key)
	}
}

// TestRegistryOpenIDsOwnedByListsOnlyThatOwnersConsoles pins the query a refusal is worded from:
// a run that names an id it may not address is told what IT holds open, so the message can never
// tell it that another delegation's Console exists (ADR 0059 §6).
func TestRegistryOpenIDsOwnedByListsOnlyThatOwnersConsoles(t *testing.T) {
	t.Parallel()
	requireConsoleBackend(t)

	registry := newTestRegistry(t)
	first := openTestShell(t, registry, "run-1")
	topLevel := openTestShell(t, registry, "")
	second := openTestShell(t, registry, "run-1")

	if ids := registry.OpenIDsOwnedBy("run-1"); !slices.Equal(ids, []int{first.ID, second.ID}) {
		t.Errorf("OpenIDsOwnedBy(run-1) = %v, want %v ascending", ids, []int{first.ID, second.ID})
	}
	if ids := registry.OpenIDsOwnedBy(""); !slices.Equal(ids, []int{topLevel.ID}) {
		t.Errorf("OpenIDsOwnedBy(top level) = %v, want [%d]", ids, topLevel.ID)
	}
	if ids := registry.OpenIDsOwnedBy("run-2"); len(ids) != 0 {
		t.Errorf("OpenIDsOwnedBy(run-2) = %v, want none for an owner holding nothing", ids)
	}
}

// TestRegistryOpenRunsAConfinedConsoleOnWindows pins what a Windows host gets for a confined open:
// the caller's Prepare hook puts the real Confiner's restricted token on the command, the Console
// opens under it with an id and records that it was confined, and the command runs.
func TestRegistryOpenRunsAConfinedConsoleOnWindows(t *testing.T) {
	// Not parallel: the real confiner journals its labels under the real apogee home.
	if runtime.GOOS != "windows" {
		t.Skip("the POSIX confined open is covered by TestRegistryOpenRecordsWhetherTheConsoleWasConfined")
	}
	confiner := newRealTestConfiner(t)
	box := domain.ConfinementBox{WorkspaceRoot: t.TempDir()}

	registry := newTestRegistry(t)
	const marker = "apogee-confined-registry-marker"
	console, err := registry.Open(OpenSpec{
		Command:     "cmd /q",
		Argv:        []string{"cmd", "/q"},
		CommandLine: "cmd /q",
		Dir:         box.WorkspaceRoot,
		Confined:    true,
		Prepare: func(cmd *exec.Cmd) error {
			return confiner.Confine(context.Background(), box, cmd)
		},
	})
	if err != nil {
		t.Fatalf("Open(confined): %v", err)
	}
	if _, err := console.Write([]byte("echo " + marker + "\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	readRegistryUntil(t, console, marker)
	if !console.Confined {
		t.Error("Console.Confined = false for a confined open")
	}
	if ids := registry.OpenIDs(); !slices.Equal(ids, []int{console.ID}) {
		t.Errorf("OpenIDs() = %v after a confined open, want [%d]", ids, console.ID)
	}
}

// TestRegistryOpenRecordsWhetherTheConsoleWasConfined pins Console.Confined as a copy of
// OpenSpec.Confined: a later send reads it to tell a fenced Console from an unfenced one, so the
// registry must carry the open's answer rather than guess it.
func TestRegistryOpenRecordsWhetherTheConsoleWasConfined(t *testing.T) {
	t.Parallel()
	requireConsoleBackend(t)

	for _, confined := range []bool{false, true} {
		registry := newTestRegistry(t)

		console, err := registry.Open(OpenSpec{Command: "sh", Argv: []string{"sh"}, Confined: confined})

		if err != nil {
			t.Fatalf("Open(Confined: %v): %v", confined, err)
		}
		if console.Confined != confined {
			t.Errorf("Console.Confined = %v, want %v (OpenSpec.Confined)", console.Confined, confined)
		}
	}
}

// newRealTestConfiner returns this host's real Confiner, closed when the test ends — on Windows it
// journals its labels under the real apogee home, and a journal left behind is replayed by the
// next session. It skips on a host whose Confiner cannot fence writes (Windows below its floor).
func newRealTestConfiner(t *testing.T) domain.Confiner {
	t.Helper()
	confiner := platform.NewConfiner()
	if closer, ok := confiner.(interface{ Close() error }); ok {
		t.Cleanup(func() { _ = closer.Close() })
	}
	if !confiner.Capabilities().FSWrite {
		t.Skipf("this host's Confiner (%T) cannot fence writes", confiner)
	}
	return confiner
}

// requireConsoleBackend skips a test that drives a POSIX `sh` Console. Windows has a backend
// (ConPTY) but no `sh`; process_windows_test.go covers it with cmd.exe instead.
func requireConsoleBackend(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("these tests drive a POSIX sh; the Windows backend is covered by process_windows_test.go")
	}
}

// newTestRegistry returns an empty registry whose Consoles are all closed when the test ends.
func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	registry := New()
	t.Cleanup(registry.CloseAll)
	return registry
}

// openTestShell opens a shell Console owned by owner, failing the test if it cannot start.
func openTestShell(t *testing.T, registry *Registry, owner string) *Console {
	t.Helper()
	console, err := registry.Open(OpenSpec{Owner: owner, Command: "sh", Argv: []string{"sh"}})
	if err != nil {
		t.Fatalf("Open() for owner %q: %v", owner, err)
	}
	return console
}

// readRegistryUntil accumulates a Console's output until it carries want, and returns everything
// read. It fails the test rather than returning short, so a caller can assert on the text it got.
func readRegistryUntil(t *testing.T, console *Console, want string) string {
	t.Helper()
	var seen strings.Builder
	deadline := time.Now().Add(registryTestTimeout)
	for time.Now().Before(deadline) {
		output, dropped := console.Read(200 * time.Millisecond)
		if dropped != 0 {
			t.Fatalf("the ring dropped %d bytes of a short test's output", dropped)
		}
		seen.WriteString(output)
		if strings.Contains(seen.String(), want) {
			return seen.String()
		}
	}
	t.Fatalf("waited %v for %q; read %q", registryTestTimeout, want, seen.String())
	return ""
}

// awaitRegistry polls condition until it holds, failing the test with what it was waiting for.
func awaitRegistry(t *testing.T, condition func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(registryTestTimeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waited %v for %s", registryTestTimeout, what)
}
