package keystore

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/shlex"
)

// TestStoreRoundTripLive drives the REAL store on this machine: probe it, write a throwaway secret
// into it, and read that secret back through the exact `api-key-cmd:` line migration would persist.
// Every other test in this package talks to a fake tool, so this is the only proof that apogee's
// assumptions about `security -i` and `secret-tool store` — the quoting, the stdin contract, the
// attribute spellings — hold against the programs the operating system actually ships.
//
// It is opt-in, gated on APOGEE_LIVE_KEYSTORE exactly like the other live tests in this repo, so
// `make check` never touches a developer's keychain:
//
//	APOGEE_LIVE_KEYSTORE=1 go test -count=1 -run TestStoreRoundTripLive -v ./internal/keystore/
//
// It writes under the entry name below and deletes the item afterwards. A failed run may leave that
// one item behind — apogee never deletes store items on its own (the store is the user's), so the
// cleanup here is the test's own courtesy, not a package feature.
func TestStoreRoundTripLive(t *testing.T) {
	if os.Getenv("APOGEE_LIVE_KEYSTORE") == "" {
		t.Skip("set APOGEE_LIVE_KEYSTORE=1 to exercise this machine's real secret store")
	}

	store, found := Probe()
	if !found {
		t.Fatalf("Probe() found no store on %s — the gate says this machine has one", runtime.GOOS)
	}

	const entry = "apogee-live-test"
	secret := fmt.Sprintf("sk-live-%d", time.Now().UnixNano())
	t.Cleanup(func() { deleteLiveItem(t, entry) })

	if err := store.Write(entry, secret); err != nil {
		t.Fatalf("Write() into %s = %v", store.Name(), err)
	}

	argv, err := shlex.Split(store.ReadCmd(entry))
	if err != nil {
		t.Fatalf("ReadCmd() produced %q, which does not split: %v", store.ReadCmd(entry), err)
	}
	output, err := exec.Command(argv[0], argv[1:]...).Output()
	if err != nil {
		t.Fatalf("running %q: %v", store.ReadCmd(entry), err)
	}

	if got := strings.TrimRight(string(output), " \t\r\n"); got != secret {
		t.Errorf("%q printed %q, want the stored key", store.ReadCmd(entry), got)
	}
}

// deleteLiveItem removes the item the live round trip wrote. Its failure is reported and not fatal:
// the test's subject is the write and the read, and an item that outlives them is a mess to clean up
// rather than a defect to fail on.
func deleteLiveItem(t *testing.T, entry string) {
	t.Helper()

	var argv []string
	switch runtime.GOOS {
	case "darwin":
		argv = []string{keychainProgram, "delete-generic-password", "-s", service, "-a", entry}
	case "linux":
		argv = []string{secretServiceProgram, "clear", "service", service, "entry", entry}
	default:
		return
	}

	if output, err := exec.Command(argv[0], argv[1:]...).CombinedOutput(); err != nil {
		t.Logf("could not delete the test item (%s %s): %v — %s", service, entry, err, strings.TrimSpace(string(output)))
	}
}
