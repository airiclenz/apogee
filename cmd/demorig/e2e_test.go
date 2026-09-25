//go:build !windows

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
)

// demoE2EEnv opts into the end-to-end smoke: it builds apogee, builds a rig with setup.sh (whose
// warm step compiles the stage) and records two takes in a pty, so it is far heavier than the
// package's unit tests and stays out of an ordinary `make test`, as the APOGEE_LIVE_ENDPOINT
// tests do.
const demoE2EEnv = "APOGEE_DEMO_E2E"

// e2eModel is the model id the fixture upstream answers as and the rig's server entry names. The
// cassette key does not carry it, but a footer that shows it is the take the storyboard judges.
const e2eModel = "demo-model"

// TestE2ERecordCheckRender drives the whole pipeline without a network or a key: a cassette is
// captured through `demorig capture` from a scripted upstream, the upstream is closed, `demorig
// record` replays the cassette into a freshly built apogee in a pty, and `check` and
// `render --dry-run` both succeed on the take it wrote.
func TestE2ERecordCheckRender(t *testing.T) {
	if os.Getenv(demoE2EEnv) != "1" {
		t.Skipf("set %s=1 to run the demorig end-to-end smoke", demoE2EEnv)
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve the repo root: %v", err)
	}
	// Every take launches apogee through env.sh, which bakes the directory of the apogee on PATH
	// at setup time; the fresh binary's directory leads PATH so neither a missing nor a stale
	// apogee is the one recorded.
	bin := buildApogee(t, repo)
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, name := range ambientApogeeEnv {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}

	work := filepath.Join(t.TempDir(), "rig")
	setupRig(t, repo, work, freePort(t))
	board := stageStoryboard(t, repo)
	dir := filepath.Dir(board)
	cassette := filepath.Join(dir, "smoke.cassette")
	take := takeFile(work, "smoke")

	script, err := stubllm.Load(filepath.Join("testdata", "e2e", "upstream.yaml"))
	if err != nil {
		t.Fatalf("load the fixture upstream: %v", err)
	}
	upstream := stubllm.New(t, script)
	runDemorig(t, "capture", board, "--upstream", upstream.URL, "--work", work)
	upstream.Close()

	captured, err := stubllm.LoadCassette(cassette)
	if err != nil {
		t.Fatalf("the capture saved no cassette: %v", err)
	}
	if len(captured.Exchanges) < 2 {
		t.Fatalf("cassette holds %d exchanges; want the prompt's tool call and the answer to its result",
			len(captured.Exchanges))
	}

	// The upstream is closed: every reply from here on is the cassette's.
	recorded := runDemorig(t, "record", board, "--work", work)
	assertNoFailRow(t, "record", recorded)
	checked := runDemorig(t, "check", board, take, "--stage", filepath.Join(work, "home", "Repos", "taskman"))
	assertNoFailRow(t, "check", checked)
	t.Logf("check:\n%s", checked)
	if !strings.Contains(checked, "PASS") {
		t.Errorf("check printed no PASS row:\n%s", checked)
	}
	dryRun := runDemorig(t, "render", board, take, "--dry-run", "-o", filepath.Join(dir, "smoke.gif"))
	if !strings.HasPrefix(dryRun, "ffmpeg ") {
		t.Errorf("render --dry-run printed %q; want the ffmpeg command line", dryRun)
	}
}

// buildApogee builds cmd/apogee into a temp dir and returns the binary's path.
func buildApogee(t *testing.T, repo string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "apogee")
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", bin, "./cmd/apogee")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build apogee: %v\n%s", err, out)
	}
	return bin
}

// setupRig builds a throwaway rig in work with the repo's own setup.sh, its server entry on port
// and naming e2eModel.
func setupRig(t *testing.T, repo, work string, port int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), filepath.Join(repo, "graphics", "demo", "setup.sh"))
	cmd.Env = append(apogeeEnv(os.Environ()),
		workDirEnv+"="+work,
		"APOGEE_DEMO_PORT="+strconv.Itoa(port),
		"APOGEE_DEMO_MODEL="+e2eModel,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("setup.sh: %v\n%s", err, out)
	}
}

// stageStoryboard copies the smoke storyboard into a temp dir beside a `fonts` link to the rig's
// committed fonts, so the cassette and the GIF it names land there, and returns its path.
func stageStoryboard(t *testing.T, repo string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "e2e", "smoke.yaml"))
	if err != nil {
		t.Fatalf("read the smoke storyboard: %v", err)
	}
	dir := t.TempDir()
	board := filepath.Join(dir, "smoke.yaml")
	if err := os.WriteFile(board, data, 0o600); err != nil {
		t.Fatalf("stage the smoke storyboard: %v", err)
	}
	if err := os.Symlink(filepath.Join(repo, "graphics", "demo", "fonts"), filepath.Join(dir, "fonts")); err != nil {
		t.Fatalf("link the rig's fonts: %v", err)
	}
	return board
}

// runDemorig runs one demorig command in process and returns what it printed to stdout; any
// failure fails the test with its stderr.
func runDemorig(t *testing.T, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := newRootCommand()
	cmd.SetArgs(args)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("demorig %s: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), err, &stdout, &stderr)
	}
	return stdout.String()
}

// assertNoFailRow fails the test when a check table carries a FAIL row.
func assertNoFailRow(t *testing.T, command, table string) {
	t.Helper()
	if slices.ContainsFunc(strings.Split(table, "\n"), func(line string) bool {
		return slices.Contains(strings.Fields(line), "FAIL")
	}) {
		t.Errorf("%s: the check table has a FAIL row:\n%s", command, table)
	}
}
