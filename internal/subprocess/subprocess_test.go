package subprocess

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
)

// fakeConfiner is a caps-injected Confiner for the core's own tests. It records each Confine
// call; when unavailable it returns ErrConfinementUnavailable so the demote path is exercisable.
// Its no-op Confine leaves cmd as the real subprocess so a confined run still executes /bin/sh in
// these hermetic tests (the dev host has no landlock, contract §6).
type fakeConfiner struct {
	caps        domain.ConfinementCaps
	unavailable bool

	mu       sync.Mutex
	confined int
}

func (c *fakeConfiner) Capabilities() domain.ConfinementCaps { return c.caps }

func (c *fakeConfiner) Confine(_ context.Context, _ domain.ConfinementBox, _ *exec.Cmd) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unavailable {
		return fmt.Errorf("%w: fake", domain.ErrConfinementUnavailable)
	}
	c.confined++
	return nil
}

// TestRunSubprocessNilConfinerFailsClosed pins the §2.2 posture on the one handle shape the
// confine guard used to wave through: a Confinement installed with no Confiner behind it. That
// is broken wiring, not permission to run free — it must surface as ErrConfinementUnavailable,
// which dispatch turns into the truthful demote to Approval, and the command must never run.
func TestRunSubprocessNilConfinerFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the guard it pins is platform-independent")
	}
	t.Parallel()

	// The canary is a file the command would create: its absence is the proof that nothing
	// ran, which an error alone cannot give.
	canary := filepath.Join(t.TempDir(), "ran")
	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: nil,
		Box:      domain.ConfinementBox{WorkspaceRoot: t.TempDir()},
	})

	_, err := RunSubprocess(ctx, SubprocessSpec{
		Argv: []string{"/bin/sh", "-c", fmt.Sprintf("touch %s", strconv.Quote(canary))},
	})
	if !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Fatalf("RunSubprocess err = %v, want ErrConfinementUnavailable (a handle with no Confiner must fail closed)", err)
	}
	if _, statErr := os.Stat(canary); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("stat %s = %v, want not-exist — the command must not have run unconfined", canary, statErr)
	}
}

// TestConfinementHandoff pins the one handoff rule both spawners read a handle through: no
// handle is an unconfined run with nothing to prepare; a handle with no Confiner is refused
// closed; a live handle yields a hook that confines the cmd and then seeds the scratch env on top
// of the cmd's own environment — the order the console and the funnel both depend on.
func TestConfinementHandoff(t *testing.T) {
	t.Parallel()

	t.Run("absent handle is unconfined", func(t *testing.T) {
		t.Parallel()
		prepare, confined, box, err := ConfinementHandoff(context.Background(), "sh")
		if err != nil || prepare != nil || confined || box != nil {
			t.Fatalf("ConfinementHandoff() = (prepare nil=%v, %v, %v, %v), want (nil, false, nil, nil)", prepare == nil, confined, box, err)
		}
	})

	t.Run("nil Confiner fails closed", func(t *testing.T) {
		t.Parallel()
		ctx := domain.WithConfinement(context.Background(), domain.Confinement{
			Box: domain.ConfinementBox{WorkspaceRoot: t.TempDir()},
		})
		prepare, confined, box, err := ConfinementHandoff(ctx, "sh")
		if !errors.Is(err, domain.ErrConfinementUnavailable) {
			t.Fatalf("err = %v, want ErrConfinementUnavailable", err)
		}
		if want := "confine sh: "; !strings.HasPrefix(err.Error(), want) || !strings.HasSuffix(err.Error(), ": the installed handle carries no Confiner") {
			t.Errorf("err = %q, want the %q … \"carries no Confiner\" sentence", err, want)
		}
		if prepare != nil || confined || box != nil {
			t.Errorf("a refused handoff must yield nothing to prepare (prepare nil=%v, %v, %v)", prepare == nil, confined, box)
		}
	})

	t.Run("live handle confines then seeds", func(t *testing.T) {
		t.Parallel()
		scratch := filepath.Join(t.TempDir(), "scratch")
		confiner := &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}}
		want := domain.ConfinementBox{WorkspaceRoot: t.TempDir(), ScratchDir: scratch}
		ctx := domain.WithConfinement(context.Background(), domain.Confinement{Confiner: confiner, Box: want})

		prepare, confined, box, err := ConfinementHandoff(ctx, "sh")

		if err != nil {
			t.Fatalf("ConfinementHandoff err = %v, want nil", err)
		}
		if !confined || box == nil || !reflect.DeepEqual(*box, want) {
			t.Fatalf("confined=%v box=%v, want true and %v", confined, box, want)
		}
		cmd := exec.Command("sh")
		cmd.Env = []string{"PATH=/usr/bin", "TMPDIR=/host/tmp"}
		if err := prepare(cmd); err != nil {
			t.Fatalf("prepare err = %v, want nil", err)
		}
		if confiner.confined != 1 {
			t.Errorf("Confine called %d times, want 1", confiner.confined)
		}
		seeded := "TMPDIR=" + filepath.Join(scratch, "tmp")
		if len(cmd.Env) < 3 || cmd.Env[0] != "PATH=/usr/bin" || cmd.Env[len(cmd.Env)-len(scratchEnvEntries)] != seeded {
			t.Errorf("cmd.Env = %q, want the caller's entries first and %q leading the seed", cmd.Env, seeded)
		}
		if info, err := os.Stat(filepath.Join(scratch, "tmp")); err != nil || !info.IsDir() {
			t.Errorf("scratch/tmp was not created by the hook: %v", err)
		}
	})

	t.Run("backend refusal propagates through the hook", func(t *testing.T) {
		t.Parallel()
		ctx := domain.WithConfinement(context.Background(), domain.Confinement{
			Confiner: &fakeConfiner{unavailable: true},
			Box:      domain.ConfinementBox{WorkspaceRoot: t.TempDir()},
		})
		prepare, _, _, err := ConfinementHandoff(ctx, "sh")
		if err != nil {
			t.Fatalf("ConfinementHandoff err = %v, want nil — the backend is only asked inside the hook", err)
		}
		if err := prepare(exec.Command("sh")); !errors.Is(err, domain.ErrConfinementUnavailable) {
			t.Errorf("prepare err = %v, want ErrConfinementUnavailable", err)
		}
	})
}

// TestRunSubprocessReportsAWedgedDrain pins the second half of the same finding: when something
// the command left running still holds the output pipe, exec cuts the drain off at
// platform.ProcessWaitDelay and returns exec.ErrWaitDelay — which is not an *exec.ExitError, so
// the exit code falls through to the leader's own status. The leader exited 0, so the call used
// to render as a green tick with a silently truncated tail.
func TestRunSubprocessReportsAWedgedDrain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell; the exit-code mapping it pins is platform-independent")
	}
	// platform.ProcessWaitDelay is a package var, so this test cannot run in parallel;
	// shrinking it is what keeps a five-second drain out of the suite.
	prev := platform.ProcessWaitDelay
	platform.ProcessWaitDelay = 250 * time.Millisecond
	t.Cleanup(func() { platform.ProcessWaitDelay = prev })

	// The sleep INHERITS the captured pipes and outlives the shell, so the output copy cannot
	// finish: Wait blocks until the delay expires. The sleep is short enough that a failed
	// reap cannot leave a process around for long.
	res, err := RunSubprocess(context.Background(), SubprocessSpec{Argv: []string{"/bin/sh", "-c", `sleep 10 &`}})
	if err != nil {
		t.Fatalf("RunSubprocess err = %v, want nil (a wedged drain is a result, not a Go error)", err)
	}
	if !res.DrainWedged {
		t.Fatalf("DrainWedged = false, want true — the pipe was still held when the delay expired (exit code %d)", res.ExitCode)
	}
	if res.ExitCode == 0 {
		t.Errorf("ExitCode = 0 for a run whose descendants held the pipe and were killed; the operator would read that as a clean success")
	}
}

// TestRunSubprocessRecordsConfined pins the confined flag on the result: true exactly when a
// Confinement handle wrapped the run, false on a plain unconfined run — the structural half
// the terminal's denial label keys on, so an unconfined EPERM can never be blamed on the box.
func TestRunSubprocessRecordsConfined(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the flag it pins is platform-independent")
	}
	t.Parallel()

	spec := SubprocessSpec{Argv: []string{"/bin/sh", "-c", "true"}}

	res, err := RunSubprocess(context.Background(), spec)
	if err != nil {
		t.Fatalf("unconfined RunSubprocess err = %v, want nil", err)
	}
	if res.Confined {
		t.Error("unconfined run reported Confined = true")
	}

	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}},
		Box:      domain.ConfinementBox{WorkspaceRoot: t.TempDir()},
	})
	res, err = RunSubprocess(ctx, spec)
	if err != nil {
		t.Fatalf("confined RunSubprocess err = %v, want nil", err)
	}
	if !res.Confined {
		t.Error("confined run reported Confined = false")
	}
}

// TestRunSubprocessDenialWatchKillsConfinedRun proves fix A of the 2026-08-22
// workspace-clobber incident at the funnel: a CONFINED run whose stream carries an
// OS-denial signature is killed by the live watch before its later, unguarded write line
// runs — the job `set -e` cannot do for an AND-OR list, since POSIX exempts every command
// of one but the last. The script mimics the incident: the "denial", intervening work
// (the sleep, which the incident's own commands stood in for — the kill is asynchronous),
// then the destructive write that must never land.
func TestRunSubprocessDenialWatchKillsConfinedRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script; the watch keys on POSIX EPERM spellings only")
	}
	t.Parallel()

	dir := t.TempDir()
	clobber := filepath.Join(dir, "clobber.txt")
	script := `echo "mkdir: cannot create directory: Operation not permitted" >&2` + "\n" +
		"sleep 5\n" +
		"echo clobbered > " + clobber + "\n"
	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}},
		Box:      domain.ConfinementBox{WorkspaceRoot: dir},
	})

	res, err := RunSubprocess(ctx, SubprocessSpec{Argv: []string{"/bin/sh", "-c", script}})

	if err != nil {
		t.Fatalf("RunSubprocess err = %v, want nil (a denial kill is a result, not a Go error)", err)
	}
	if !res.DenialStopped {
		t.Error("DenialStopped = false, want the watch to have matched and killed the run")
	}
	if res.ExitCode == 0 {
		t.Error("ExitCode = 0, want non-zero for the killed run")
	}
	if res.TimedOut {
		t.Error("timedOut = true, want the denial kill reported as a kill, not a timeout")
	}
	if _, statErr := os.Stat(clobber); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("stat %q = %v, want not-exist — the kill must land before the unguarded write", clobber, statErr)
	}
}

// TestRunSubprocessDenialWatchIgnoresStdout pins the stderr-only wiring of the pipe path
// (ADR 0056 D2, amended 2026-09-16): a CONFINED script that prints a real, line-ending Go
// denial on STDOUT — the session-mining fc413fb5 shape, a `cat` of a log whose lines end in
// `open /dev/ptmx: permission denied` — is not killed, its later write lands, and the run is
// not flagged DenialStopped; stdout is the command's data, not its complaint. The denial
// text still reaches CombinedOutput, so the model reads what the command printed.
func TestRunSubprocessDenialWatchIgnoresStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script; the watch keys on POSIX EPERM spellings only")
	}
	t.Parallel()

	dir := t.TempDir()
	written := filepath.Join(dir, "after.txt")
	script := `echo "open /dev/ptmx: permission denied"` + "\n" +
		"echo landed > " + written + "\n"
	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}},
		Box:      domain.ConfinementBox{WorkspaceRoot: dir},
	})

	res, err := RunSubprocess(ctx, SubprocessSpec{Argv: []string{"/bin/sh", "-c", script}})

	if err != nil {
		t.Fatalf("RunSubprocess err = %v, want nil", err)
	}
	if res.DenialStopped {
		t.Error("DenialStopped = true, want the stdout denial text left unwatched")
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 — the run must complete untouched", res.ExitCode)
	}
	if _, statErr := os.Stat(written); statErr != nil {
		t.Errorf("stat %q = %v, want the write after the stdout denial to land", written, statErr)
	}
	if !strings.Contains(res.CombinedOutput, "open /dev/ptmx: permission denied") {
		t.Errorf("CombinedOutput = %q, want the stdout line captured", res.CombinedOutput)
	}
}

// TestCappedBufferConcurrentWrites pins the lock a confined run relies on: two writers — the
// shape of exec's stdout copier and the denial watch's stderr copier feeding one buffer —
// land every chunk whole, the cap holds, and the discard count is exact, under -race.
func TestCappedBufferConcurrentWrites(t *testing.T) {
	t.Parallel()

	const writers, perWriter = 4, 100
	chunk := strings.Repeat("x", 8)
	buf := CappedBuffer{Limit: writers * perWriter * len(chunk) / 2}
	var wg sync.WaitGroup
	for writer := 0; writer < writers; writer++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				if _, err := buf.Write([]byte(chunk)); err != nil {
					t.Errorf("Write: %v", err)
				}
			}
		}()
	}
	wg.Wait()

	got := buf.String()
	wantMarker := fmt.Sprintf("… [output truncated: %d more bytes]", buf.Limit)
	if !strings.HasSuffix(got, wantMarker) {
		t.Errorf("String() = %q, want it to end in %q", got, wantMarker)
	}
	if body := strings.TrimSuffix(got, "\n"+wantMarker); len(body) != buf.Limit || strings.Trim(body, "x") != "" {
		t.Errorf("captured body = %d bytes of %q, want exactly %d x's", len(body), body, buf.Limit)
	}
}

// TestRunSubprocessDenialWatchNeverWatchesUnconfined pins the watch's structural gate: the
// identical denial-shaped output on an UNCONFINED run is not scanned, not killed, and not
// flagged — an unconfined EPERM can never be blamed on the box (the same gate the confined
// flag itself pins above).
func TestRunSubprocessDenialWatchNeverWatchesUnconfined(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script; the gate it pins is platform-independent")
	}
	t.Parallel()

	script := `echo "mkdir: cannot create directory: Operation not permitted" >&2`

	res, err := RunSubprocess(context.Background(), SubprocessSpec{Argv: []string{"/bin/sh", "-c", script}})

	if err != nil {
		t.Fatalf("RunSubprocess err = %v, want nil", err)
	}
	if res.DenialStopped {
		t.Error("DenialStopped = true on an unconfined run")
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 — the run must complete untouched", res.ExitCode)
	}
}

// scratchEnvProbe is a POSIX line printing the three variables the confined seed is measured by:
// one temp spelling, the Go build cache and the XDG cache root, space-separated on one line.
const scratchEnvProbe = `echo "$TMPDIR $GOCACHE $XDG_CACHE_HOME"`

// TestRunSubprocessConfinedRunSeedsTheScratchEnv pins the confined-run seed at the funnel: a run
// under a box naming a ScratchDir sees TMPDIR, GOCACHE and XDG_CACHE_HOME pointing beneath that
// dir — `tmp`, `go-build` and `cache` — and the directories exist by the time the child runs, so
// a toolchain that would otherwise reach for /tmp or ~/.cache (both outside the fence) writes
// where the box allows. The spec hands in an explicit Env carrying the host's own TMPDIR, so the
// test also proves the seed wins the last-wins duplicate resolution rather than merely filling a
// gap.
func TestRunSubprocessConfinedRunSeedsTheScratchEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the seed it pins is platform-independent")
	}
	t.Parallel()

	scratch := filepath.Join(t.TempDir(), "scratch")
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}},
		Box:      domain.ConfinementBox{WorkspaceRoot: t.TempDir(), ScratchDir: scratch},
	})
	spec := SubprocessSpec{
		Argv: []string{"/bin/sh", "-c", scratchEnvProbe},
		Env:  []string{"PATH=" + os.Getenv("PATH"), "TMPDIR=/host/tmp"},
	}

	res, err := RunSubprocess(ctx, spec)

	if err != nil {
		t.Fatalf("RunSubprocess err = %v, want nil", err)
	}
	want := strings.Join([]string{
		filepath.Join(scratch, "tmp"),
		filepath.Join(scratch, "go-build"),
		filepath.Join(scratch, "cache"),
	}, " ")
	if got := strings.TrimSpace(res.CombinedOutput); got != want {
		t.Errorf("confined child printed %q, want the three scratch paths %q", got, want)
	}
	for _, sub := range []string{"tmp", "go-build", "cache"} {
		if info, err := os.Stat(filepath.Join(scratch, sub)); err != nil || !info.IsDir() {
			t.Errorf("scratch/%s was not created before the spawn: %v", sub, err)
		}
	}
}

// TestRunSubprocessUnconfinedRunKeepsTheHostEnv is the other half of the seed's contract: with
// no box on the context the child's environment is exactly what the caller handed in — the host
// values print unchanged and nothing is created under the scratch-shaped temp dir.
func TestRunSubprocessUnconfinedRunKeepsTheHostEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the contract it pins is platform-independent")
	}
	t.Parallel()

	spec := SubprocessSpec{
		Argv: []string{"/bin/sh", "-c", scratchEnvProbe},
		Env: []string{
			"PATH=" + os.Getenv("PATH"),
			"TMPDIR=/host/tmp", "GOCACHE=/host/go-build", "XDG_CACHE_HOME=/host/cache",
		},
	}

	res, err := RunSubprocess(context.Background(), spec)

	if err != nil {
		t.Fatalf("RunSubprocess err = %v, want nil", err)
	}
	if got, want := strings.TrimSpace(res.CombinedOutput), "/host/tmp /host/go-build /host/cache"; got != want {
		t.Errorf("unconfined child printed %q, want the host values %q unchanged", got, want)
	}
}

// TestScratchEnvKeysMatchTheSeed pins the allowlist contract: every key ScratchEnv seeds is
// named by ScratchEnvKeys, in the same order, so an environment allowlist built from the keys can
// never strip a seeded value.
func TestScratchEnvKeysMatchTheSeed(t *testing.T) {
	t.Parallel()

	seed, err := ScratchEnv(domain.ConfinementBox{ScratchDir: t.TempDir()})
	if err != nil {
		t.Fatalf("ScratchEnv err = %v, want nil", err)
	}

	keys := ScratchEnvKeys()
	if len(keys) != len(seed) {
		t.Fatalf("ScratchEnvKeys names %d keys, ScratchEnv seeds %d entries", len(keys), len(seed))
	}
	for i, entry := range seed {
		if !strings.HasPrefix(entry, keys[i]+"=") {
			t.Errorf("seed[%d] = %q, want key %q", i, entry, keys[i])
		}
	}
	if seed, err := ScratchEnv(domain.ConfinementBox{}); err != nil || seed != nil {
		t.Errorf("ScratchEnv on a box with no ScratchDir = %v, %v; want nil, nil", seed, err)
	}
}

// oversizeStdoutScript is a POSIX line printing a deterministic payload past the output cap, so
// the two stdout paths — the capped one and the streaming one — can be measured against the SAME
// bytes. yes/head is used rather than a Go writer because the point is what a real child process
// pushes down a pipe.
const oversizeStdoutScript = `yes 0123456789abcdefghijklmnopqrstuvwxyz | head -c 400000`

// oversizeStdoutBytes is what that script prints: 400000 bytes, comfortably past the cap.
const oversizeStdoutBytes = 400000

// TestRunSubprocessCapsAnOversizeStdout pins the ceiling on the capped path: a child printing far
// more than MaxSubprocessOutputBytes keeps exactly the cap's worth and says how much it dropped,
// so a runaway command can neither exhaust memory nor flood a context window.
func TestRunSubprocessCapsAnOversizeStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script; the cap it pins is platform-independent")
	}
	t.Parallel()

	res, err := RunSubprocess(context.Background(), SubprocessSpec{
		Argv: []string{"/bin/sh", "-c", oversizeStdoutScript},
	})
	if err != nil {
		t.Fatalf("RunSubprocess err = %v, want nil", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", res.ExitCode)
	}
	marker := fmt.Sprintf("… [output truncated: %d more bytes]", oversizeStdoutBytes-MaxSubprocessOutputBytes)
	if !strings.HasSuffix(res.CombinedOutput, marker) {
		t.Errorf("CombinedOutput does not end with %q — the model would not know the tail is missing", marker)
	}
	if kept := len(res.CombinedOutput) - len("\n") - len(marker); kept != MaxSubprocessOutputBytes {
		t.Errorf("kept %d bytes before the marker, want %d (MaxSubprocessOutputBytes)", kept, MaxSubprocessOutputBytes)
	}
}

// TestRunSubprocessToStreamsStdoutUncapped pins the streaming variant against the capped one: the
// SAME oversize payload reaches the caller's writer whole and byte-identical, because a caller
// splicing a child's stdout into a file has no business receiving a truncation marker in the
// middle of it. The diagnostics stay capped and stay out of the payload.
func TestRunSubprocessToStreamsStdoutUncapped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script; the streaming path it pins is platform-independent")
	}
	t.Parallel()

	var got bytes.Buffer
	res, err := RunSubprocessTo(context.Background(), SubprocessSpec{
		Argv: []string{"/bin/sh", "-c", oversizeStdoutScript + " ; echo diagnostic >&2"},
	}, &got)
	if err != nil {
		t.Fatalf("RunSubprocessTo err = %v, want nil", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0 (output %q)", res.ExitCode, res.CombinedOutput)
	}
	if got.Len() != oversizeStdoutBytes {
		t.Errorf("streamed %d bytes, want %d — the payload must not be capped", got.Len(), oversizeStdoutBytes)
	}
	want := strings.Repeat("0123456789abcdefghijklmnopqrstuvwxyz\n", 1+oversizeStdoutBytes/37)[:oversizeStdoutBytes]
	if got.String() != want {
		t.Error("streamed bytes differ from what the child printed")
	}
	if strings.TrimSpace(res.CombinedOutput) != "diagnostic" {
		t.Errorf("CombinedOutput = %q, want the diagnostics alone", res.CombinedOutput)
	}
	if res.Stdout != "" {
		t.Errorf("Stdout = %q, want empty — the payload left through the writer", res.Stdout)
	}
}

// TestRunSubprocessRefusesAnEmptyArgv pins the one argument check the core makes for itself: a
// spec with no program is a caller bug, refused before a process is built rather than handed to
// exec as an empty name.
func TestRunSubprocessRefusesAnEmptyArgv(t *testing.T) {
	t.Parallel()

	if _, err := RunSubprocess(context.Background(), SubprocessSpec{}); err == nil {
		t.Fatal("RunSubprocess err = nil for an empty argv, want a refusal")
	}
}

// TestEffectiveTimeoutAdmitsASlowBoxSubprocessBudget pins the reach of the funnel's ceiling. The budget a
// caller names is what a cold toolchain build on throttled hardware needs, so a request past the
// ten-minute maximum this ceiling used to hold is honoured unclamped; only a request past the
// constant itself is cut back, and to the constant rather than to a literal a future resize would
// leave behind.
func TestEffectiveTimeoutAdmitsASlowBoxSubprocessBudget(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		requested time.Duration
		want      time.Duration
	}{
		{
			name:      "a cold-build budget past the old ten-minute ceiling is honoured",
			requested: 30 * time.Minute,
			want:      30 * time.Minute,
		},
		{
			name:      "a budget past the ceiling is clamped to the ceiling",
			requested: MaxSubprocessTimeout + time.Minute,
			want:      MaxSubprocessTimeout,
		},
		{
			name:      "a caller naming no budget takes the default",
			requested: 0,
			want:      DefaultSubprocessTimeout,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := effectiveTimeout(tc.requested)

			if got != tc.want {
				t.Fatalf("effectiveTimeout(%s) = %s, want %s", tc.requested, got, tc.want)
			}
		})
	}
}
