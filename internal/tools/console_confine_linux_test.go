//go:build linux

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/console"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
)

// TestMain intercepts the __confined-exec sentinel so this test binary can play the in-child half
// of the landlock re-exec wrapper — the same dispatch cmd/apogee's main performs for the product
// binary (confinement-execution-contract §2.3/§6.1, and the idiom internal/platform's own landlock
// test already uses). Without it a LIVE confinement proof in this package would re-exec the test
// binary as an ordinary test run instead of confining anything, because the landlock backend
// re-execs os.Executable().
func TestMain(m *testing.M) {
	if len(os.Args) >= 2 && os.Args[1] == platform.ConfinedExecSentinel() {
		os.Exit(runConfinedExecChild(os.Args[2:]))
	}
	os.Exit(m.Run())
}

// runConfinedExecChild mirrors cmd/apogee's sentinel dispatcher: argv is [<encoded-box>, "--",
// <real argv...>]. On success ApplyLandlockAndExec replaces this process image and never returns.
func runConfinedExecChild(args []string) int {
	if len(args) < 2 || args[1] != "--" {
		fmt.Fprintln(os.Stderr, "confined-exec: malformed argv")
		return 2
	}
	box, err := platform.DecodeConfinedBox(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := platform.ApplyLandlockAndExec(box, args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0 // unreachable on success
}

// TestConsoleOpen_LiveConfinementDenialStopsTheConsole is the family's enforcement proof, run
// against the host's real Confiner rather than a fake: a Console opened confined and then told to
// write outside its box is STOPPED where it was denied, the model is told so in the fence's own
// words, and the file it aimed at never appears.
//
// It is the one assertion a fake Confiner cannot make. The kill-on-denial watch and the fence are
// two independent mechanisms, and only a real box proves they compose through a pseudo-terminal:
// the wrapped child is re-exec'd, becomes a session leader, and has to stay inside the ruleset
// across both. It skips where the kernel cannot enforce a box (the capability-gated idiom
// internal/platform/confinetest uses).
func TestConsoleOpen_LiveConfinementDenialStopsTheConsole(t *testing.T) {
	confiner := platform.NewConfiner()
	if !confiner.Capabilities().FSWrite {
		t.Skip("this host reports no enforceable filesystem confinement; skipping the live proof")
	}
	t.Parallel()

	root := t.TempDir()
	escape := filepath.Join(t.TempDir(), "apogee-console-escape")
	registry := console.New()
	t.Cleanup(registry.CloseAll)
	box := domain.ConfinementBox{WorkspaceRoot: root}
	ctx := domain.WithConfinement(
		console.WithRegistry(context.Background(), registry),
		domain.Confinement{Confiner: confiner, Box: box},
	)

	opened, err := NewConsoleOpen(root, nil).Execute(ctx, consoleOpenCall("c1", "sh", 500))
	if err != nil {
		t.Fatalf("console_open err = %v, want nil", err)
	}
	if opened.IsError || strings.Contains(opened.Content, "exited with code") {
		t.Fatalf("confined console did not come up: %q", opened.Content)
	}
	id := registry.OpenIDs()[0]
	fenced, ok := registry.Get(id)
	if !ok {
		t.Fatalf("console %d is not in the registry", id)
	}

	sent, err := NewConsoleSend().Execute(ctx, consoleSendCall("c2", id, "echo escaped > "+escape, false, 5000))

	if err != nil {
		t.Fatalf("console_send err = %v, want nil", err)
	}
	if !strings.Contains(sent.Content, confinementDenialStopLabel(box)) {
		t.Errorf("send result = %q, want the kill-on-denial label", sent.Content)
	}
	if !waitFor(2*time.Second, func() bool { return !fenced.Alive() }) {
		t.Error("the denied console is still alive; the watch did not stop it")
	}
	if _, err := os.Stat(escape); !os.IsNotExist(err) {
		t.Errorf("os.Stat(%s) = %v, want the write outside the box to have been denied", escape, err)
	}
}

// waitFor polls done until it reports true or the budget runs out, so a test can assert on state
// a background goroutine records (a reaped exit) without sleeping for the worst case.
func waitFor(budget time.Duration, done func() bool) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if done() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return done()
}
