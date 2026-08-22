package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/daemon"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/schedule"
)

// ----------------------------------------------------------------------------
// The harness
// ----------------------------------------------------------------------------

// daemonHarness is one `apogee daemon` invocation with every seam onto the host replaced: the
// runner, so nothing is sent; the confinement backend, so the Auto verdict is the test's rather
// than the kernel's; the slot probe, so no Firing dials a server; and the lock, so a test daemon
// never contends with whatever the machine's real one holds. Tests here replace package-level vars
// and so never call t.Parallel, exactly as the headless and daemon-firing tests do.
type daemonHarness struct {
	home      string
	dir       string
	schedules string
	runner    *stubRunner
	clock     *fakeDaemonClock
	// signals is what a test signals the daemon through; it is buffered so a test can queue the
	// stop before the daemon reaches its wait.
	signals chan os.Signal
	// out collects the daemon's own stdout narration, guarded because the daemon writes it from
	// the Scheduler's goroutines while the test reads it.
	out *syncBuffer
	// errOut collects stderr — resolution notices and the confinement teardown notice.
	errOut *syncBuffer
	// locked records the path the lock seam was asked for.
	locked string
	// released reports whether the lock was given back.
	released bool
}

// newDaemonHarness prepares an apogee home with a startup server and installs every seam.
func newDaemonHarness(t *testing.T) *daemonHarness {
	t.Helper()

	home := testConfigHome(t, "")
	h := &daemonHarness{
		home:      home,
		dir:       filepath.Join(home, daemonDirName),
		schedules: filepath.Join(home, daemonDirName, schedulesFileName),
		runner:    &stubRunner{},
		clock:     newFakeDaemonClock(),
		signals:   make(chan os.Signal, 2),
		out:       &syncBuffer{},
		errOut:    &syncBuffer{},
	}

	prevRunner, prevSlots, prevConfiner := runOnce, discoverSlots, newConfiner
	prevLock, prevClock := acquireDaemonLock, daemonClock
	runOnce = h.runner.once
	discoverSlots = func(context.Context, string, string, string) int { return 0 }
	newConfiner = func() apogee.Confiner { return fenceableHost }
	acquireDaemonLock = func(path string) (func(), error) {
		h.locked = path
		return func() { h.released = true }, nil
	}
	daemonClock = h.clock
	t.Cleanup(func() {
		runOnce, discoverSlots, newConfiner = prevRunner, prevSlots, prevConfiner
		acquireDaemonLock, daemonClock = prevLock, prevClock
	})
	return h
}

// writeSchedules puts a schedules file in place before the daemon starts.
func (h *daemonHarness) writeSchedules(t *testing.T, body string) {
	t.Helper()
	if err := os.MkdirAll(h.dir, 0o700); err != nil {
		t.Fatalf("create the daemon directory: %v", err)
	}
	if err := os.WriteFile(h.schedules, []byte(body), 0o600); err != nil {
		t.Fatalf("write schedules.yaml: %v", err)
	}
}

// run drives one daemon to completion on its own goroutine and returns a func that waits for it and
// yields the error it ended with. Nothing is signalled here — the caller decides when to stop.
func (h *daemonHarness) run(t *testing.T) func() error {
	t.Helper()

	opts := config.Options{ConfigDir: h.home}
	done := make(chan error, 1)
	go func() {
		done <- runDaemon(context.Background(), &opts, func(string) bool { return false },
			h.out, h.errOut, h.signals)
	}()
	return func() error {
		select {
		case err := <-done:
			return err
		case <-time.After(10 * time.Second):
			t.Fatalf("the daemon never returned; log so far:\n%s", h.out.String())
			return nil
		}
	}
}

// stop queues one SIGTERM.
func (h *daemonHarness) stop() { h.signals <- syscall.SIGTERM }

// awaitLog blocks until the daemon's stdout holds want, and fails when it never does.
func (h *daemonHarness) awaitLog(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(h.out.String(), want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the log never held %q; it held:\n%s", want, h.out.String())
}

// syncBuffer is a strings.Builder behind a mutex — the daemon narrates from several goroutines at
// once, and a test that reads while it writes must not race.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// ----------------------------------------------------------------------------
// The fake clock
// ----------------------------------------------------------------------------

// fakeDaemonClock is the Scheduler's sense of time, driven by the test: tick() makes every live
// Schedule due at once, which is how a 24h cycle fires inside a unit test.
type fakeDaemonClock struct {
	mu      sync.Mutex
	now     time.Time
	tickers []*fakeDaemonTicker
}

func newFakeDaemonClock() *fakeDaemonClock {
	return &fakeDaemonClock{now: time.Date(2026, 8, 22, 3, 0, 0, 0, time.UTC)}
}

func (c *fakeDaemonClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeDaemonClock) NewTicker(time.Duration) schedule.Ticker {
	c.mu.Lock()
	defer c.mu.Unlock()
	ticker := &fakeDaemonTicker{c: make(chan time.Time, 1)}
	c.tickers = append(c.tickers, ticker)
	return ticker
}

// tick delivers one tick to every ticker this clock has handed out.
func (c *fakeDaemonClock) tick() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(time.Minute)
	for _, ticker := range c.tickers {
		select {
		case ticker.c <- c.now:
		default:
		}
	}
}

type fakeDaemonTicker struct{ c chan time.Time }

func (t *fakeDaemonTicker) C() <-chan time.Time { return t.c }

func (t *fakeDaemonTicker) Stop() {}

// ----------------------------------------------------------------------------
// Seeding
// ----------------------------------------------------------------------------

// A first run drops the documented template, and what it drops is a VALID empty set: the daemon
// that seeded it goes straight on to watch it (ADR 0034 decision 10).
func TestDaemonSeedsTheTemplateOnFirstRun(t *testing.T) {
	h := newDaemonHarness(t)

	h.stop()
	wait := h.run(t)
	if err := wait(); err != nil {
		t.Fatalf("daemon: %v\n%s", err, h.errOut.String())
	}

	seeded, err := os.ReadFile(h.schedules)
	if err != nil {
		t.Fatalf("read the seeded schedules file: %v", err)
	}
	if string(seeded) != string(defaultSchedulesYAML) {
		t.Error("the seeded file is not the embedded template")
	}
	for _, want := range []string{
		"created a starter schedules file at " + h.schedules,
		"0 schedules on the clock",
	} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("the log never said %q; it said:\n%s", want, h.out.String())
		}
	}
}

// The user's own file is their contract: a second run must never write over it.
func TestDaemonNeverOverwritesAnExistingSchedulesFile(t *testing.T) {
	h := newDaemonHarness(t)
	const mine = "# mine, edited by hand\nschedules: []\n"
	h.writeSchedules(t, mine)

	h.stop()
	wait := h.run(t)
	if err := wait(); err != nil {
		t.Fatalf("daemon: %v\n%s", err, h.errOut.String())
	}

	kept, err := os.ReadFile(h.schedules)
	if err != nil {
		t.Fatalf("read schedules.yaml: %v", err)
	}
	if string(kept) != mine {
		t.Errorf("the daemon rewrote the file:\n%s", kept)
	}
	if strings.Contains(h.out.String(), "created a starter") {
		t.Error("the daemon claimed to have seeded a file that already existed")
	}
}

// ----------------------------------------------------------------------------
// Startup refusals
// ----------------------------------------------------------------------------

// Every defect is logged — not the first — and the daemon refuses to start: at startup there is no
// previous set to keep running, so a daemon with nothing on the clock would look like one that works.
func TestDaemonRefusesAnInvalidFileAndLogsEveryDefect(t *testing.T) {
	h := newDaemonHarness(t)
	h.writeSchedules(t, "schedules:\n"+
		"  - name: \"\"\n"+
		"    on:\n      cycle: 1s\n"+
		"    run:\n      prompt: \"\"\n      workspace: /nowhere-at-all\n")

	wait := h.run(t)
	err := wait()
	if err == nil {
		t.Fatal("the daemon started on a file that does not validate")
	}
	if !strings.Contains(err.Error(), "nothing was scheduled") {
		t.Errorf("the refusal reads %q; it should say nothing was scheduled", err)
	}
	logged := h.out.String()
	for _, want := range []string{"name:", "cycle:", "prompt:", "workspace:"} {
		if !strings.Contains(logged, want) {
			t.Errorf("no logged defect names %q; the log holds:\n%s", want, logged)
		}
	}
	if h.locked == "" {
		t.Error("the file was validated before the single-instance lock was taken")
	}
}

// A held lock is a refusal a human can act on: which process has it, in the daemon's own words.
func TestDaemonRefusesWhenAnotherDaemonHoldsTheLock(t *testing.T) {
	h := newDaemonHarness(t)
	acquireDaemonLock = func(path string) (func(), error) {
		return nil, &platform.LockHeldError{Path: path, PID: 4242}
	}

	wait := h.run(t)
	err := wait()
	if err == nil {
		t.Fatal("a second daemon started while the lock was held")
	}
	if !strings.Contains(err.Error(), "already running (pid 4242)") {
		t.Errorf("the refusal reads %q; it should name the holder's pid", err)
	}
}

// A daemon whose config cannot say which server it talks to has nothing to schedule, so it stops
// where `apogee headless` and `apogee probe` do rather than starting a clock over no upstream.
func TestDaemonRefusesWhenNoStartupServerIsResolved(t *testing.T) {
	h := newDaemonHarness(t)
	if err := os.WriteFile(filepath.Join(h.home, "config.yaml"), []byte("servers: []\n"), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	wait := h.run(t)
	err := wait()
	var undetermined *config.StartupUndetermined
	if !errors.As(err, &undetermined) {
		t.Fatalf("daemon error = %v; want the startup-undetermined refusal", err)
	}
	if h.locked != "" {
		t.Error("the lock was taken before the configuration was known to resolve")
	}
}

// ----------------------------------------------------------------------------
// Adoption and the run
// ----------------------------------------------------------------------------

// twoSchedulesYAML is a valid file with two entries, one per mode the schema accepts.
func twoSchedulesYAML(workspace string) string {
	return fmt.Sprintf("shutdown-grace: 1s\nschedules:\n"+
		"  - name: nightly-audit\n    on:\n      cycle: 24h\n"+
		"    run:\n      prompt: \"/code-audit internal/tui\"\n      workspace: %s\n"+
		"  - name: hourly-sweep\n    on:\n      cycle: 1h\n"+
		"    run:\n      prompt: sweep the tree\n      workspace: %s\n      mode: auto\n"+
		"      server: %s\n", workspace, workspace, testServerName)
}

// Every entry in a valid file goes on the clock, and each one says so in the log.
func TestDaemonAdoptsEveryEntry(t *testing.T) {
	h := newDaemonHarness(t)
	h.writeSchedules(t, twoSchedulesYAML(t.TempDir()))

	h.stop()
	wait := h.run(t)
	if err := wait(); err != nil {
		t.Fatalf("daemon: %v\n%s", err, h.errOut.String())
	}

	logged := h.out.String()
	for _, want := range []string{
		"created   nightly-audit",
		"created   hourly-sweep",
		"2 schedules on the clock",
		"stopped   nightly-audit",
		"stopped   hourly-sweep",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("the log never said %q; it said:\n%s", want, logged)
		}
	}
	if !h.released {
		t.Error("the daemon exited without releasing the single-instance lock")
	}
}

// The whole path end to end: an adopted entry, a due tick, a Firing composed from the entry, and
// the two log lines that bracket it. The runner is stubbed, so nothing is sent.
func TestDaemonFiresAnAdoptedScheduleOnItsTick(t *testing.T) {
	h := newDaemonHarness(t)
	workspace := t.TempDir()
	h.writeSchedules(t, fmt.Sprintf("schedules:\n"+
		"  - name: nightly-audit\n    on:\n      cycle: 24h\n"+
		"    run:\n      prompt: \"/code-audit internal/tui\"\n      workspace: %s\n", workspace))
	h.runner.res = run.Result{SessionID: "ses_abc", Turns: 3, Denied: 1}

	wait := h.run(t)
	h.awaitLog(t, "1 schedule on the clock")
	h.clock.tick()
	h.awaitLog(t, "completed nightly-audit")

	h.stop()
	if err := wait(); err != nil {
		t.Fatalf("daemon: %v\n%s", err, h.errOut.String())
	}

	logged := h.out.String()
	if !strings.Contains(logged, "fired     nightly-audit — /code-audit internal/tui") {
		t.Errorf("the fired line is missing or reworded; the log holds:\n%s", logged)
	}
	if !strings.Contains(logged, "3 turns, 1 denied, saved as ses_abc") {
		t.Errorf("the completed line does not report the run; the log holds:\n%s", logged)
	}
	if h.runner.spec.Config.WorkspaceDir != workspace {
		t.Errorf("Config.WorkspaceDir = %q; want the entry's workspace %q", h.runner.spec.Config.WorkspaceDir, workspace)
	}
	if h.runner.spec.Config.Mode != domain.ModePlan {
		t.Errorf("Config.Mode = %q; want the entry's plan mode", h.runner.spec.Config.Mode)
	}
}

// A stop with nothing in flight does not sit out the grace: the daemon exits at once.
func TestDaemonStopsPromptlyWithNoFiringInFlight(t *testing.T) {
	h := newDaemonHarness(t)
	h.writeSchedules(t, "shutdown-grace: 1h\nschedules:\n"+
		"  - name: nightly-audit\n    on:\n      cycle: 24h\n"+
		"    run:\n      prompt: audit\n      workspace: "+t.TempDir()+"\n")

	wait := h.run(t)
	h.awaitLog(t, "1 schedule on the clock")

	started := time.Now()
	h.stop()
	if err := wait(); err != nil {
		t.Fatalf("daemon: %v\n%s", err, h.errOut.String())
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("the shutdown took %s with nothing in flight; it waited out a grace it should have skipped", elapsed)
	}
	logged := h.out.String()
	if strings.Contains(logged, "a firing is still running") {
		t.Errorf("the daemon waited on a firing that was not there:\n%s", logged)
	}
	if !strings.HasSuffix(strings.TrimSpace(logged), "stopped") {
		t.Errorf("the log does not end on the stopped line:\n%s", logged)
	}
}

// A firing still running when the stop arrives gets its grace, and a second signal ends the wait
// rather than making the operator sit through it.
func TestDaemonSecondSignalCancelsTheFiringInFlight(t *testing.T) {
	h := newDaemonHarness(t)
	h.writeSchedules(t, "shutdown-grace: 1h\nschedules:\n"+
		"  - name: nightly-audit\n    on:\n      cycle: 24h\n"+
		"    run:\n      prompt: audit\n      workspace: "+t.TempDir()+"\n")

	// A runner that blocks until its context is cancelled: the Firing is in flight for exactly as
	// long as the shutdown lets it be.
	firing := make(chan struct{})
	runOnce = func(ctx context.Context, _ run.Spec) (run.Result, error) {
		close(firing)
		<-ctx.Done()
		return run.Result{}, ctx.Err()
	}

	wait := h.run(t)
	h.awaitLog(t, "1 schedule on the clock")
	h.clock.tick()
	<-firing

	h.stop()
	h.awaitLog(t, "a firing is still running")
	h.stop()
	if err := wait(); err != nil {
		t.Fatalf("daemon: %v\n%s", err, h.errOut.String())
	}
	if !strings.Contains(h.out.String(), "again — cancelling the firing in flight") {
		t.Errorf("the second signal was not acted on; the log holds:\n%s", h.out.String())
	}
}

// ----------------------------------------------------------------------------
// The host facts validation is given
// ----------------------------------------------------------------------------

// The empty server name is the startup default, and the lookup must answer for it — otherwise ADR
// 0055's `model:`-on-a-launcher rule silently skips every entry that names no server, which is the
// entry most people write first.
func TestDaemonHostLooksUpTheStartupDefault(t *testing.T) {
	h := newDaemonHarness(t)
	opts := config.Options{
		ConfigDir:       h.home,
		StartupLauncher: "auto",
		Servers: []config.ServerEntry{
			{Name: "plain", Endpoint: testServerEndpoint},
			{Name: "fronted", Endpoint: testServerEndpoint, LlamaLauncher: "auto"},
		},
	}
	wiring, err := newDaemonWiring(opts)
	if err != nil {
		t.Fatalf("newDaemonWiring: %v", err)
	}
	host := daemonHost(opts, h.home, wiring)

	for _, tc := range []struct {
		name     string
		known    bool
		launcher bool
	}{
		{name: "", known: true, launcher: true},
		{name: "plain", known: true, launcher: false},
		{name: "fronted", known: true, launcher: true},
		{name: "nobody", known: false, launcher: false},
	} {
		facts, known := host.LookupServer(tc.name)
		if known != tc.known {
			t.Errorf("LookupServer(%q) known = %v; want %v", tc.name, known, tc.known)
		}
		if facts.IsLauncherFronted != tc.launcher {
			t.Errorf("LookupServer(%q) launcher-fronted = %v; want %v", tc.name, facts.IsLauncherFronted, tc.launcher)
		}
	}
	if !host.AutoEligible {
		t.Error("a host whose backend can fence is not Auto-eligible")
	}
	if host.Home != h.home {
		t.Errorf("Host.Home = %q; want the apogee home %q", host.Home, h.home)
	}
}

// A default-bound entry with a `model:` is refused when the startup server is launcher-fronted —
// the rule the empty-name lookup above exists to reach, asserted through Load itself.
func TestDaemonHostRefusesAModelOnTheLauncherFrontedDefault(t *testing.T) {
	h := newDaemonHarness(t)
	opts := config.Options{ConfigDir: h.home, StartupLauncher: "auto"}
	wiring, err := newDaemonWiring(opts)
	if err != nil {
		t.Fatalf("newDaemonWiring: %v", err)
	}
	file := "schedules:\n  - name: nightly\n    on:\n      cycle: 24h\n" +
		"    run:\n      prompt: audit\n      workspace: " + t.TempDir() + "\n      model: qwen/qwen3-72b\n"
	if _, err := daemon.Load([]byte(file), daemonHost(opts, h.home, wiring)); err == nil {
		t.Fatal("a model: on the launcher-fronted startup default was accepted")
	} else if !strings.Contains(err.Error(), "llama-launcher fronts") {
		t.Errorf("the defect reads %q; it should be the ADR 0055 launcher rule", err)
	}
}

// A host that cannot fence refuses `mode: auto` at load, where the refusal names the entry — the
// same verdict `apogee headless` reaches, through the same function.
func TestDaemonHostRefusesAutoOnAHostThatCannotFence(t *testing.T) {
	h := newDaemonHarness(t)
	newConfiner = func() apogee.Confiner { return fakeConfiner{} }
	opts := config.Options{ConfigDir: h.home, ConfineToWorkspace: true}
	wiring, err := newDaemonWiring(opts)
	if err != nil {
		t.Fatalf("newDaemonWiring: %v", err)
	}
	host := daemonHost(opts, h.home, wiring)
	if host.AutoEligible {
		t.Fatal("a host with no filesystem confinement was reported Auto-eligible")
	}
}

// ----------------------------------------------------------------------------
// The log's own shape
// ----------------------------------------------------------------------------

// The notify line for every Event kind the library emits, pinned: these lines are the daemon's
// entire user interface, and a supervisor's journal is where they are read back.
func TestDaemonNotifyLinesArePinned(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 8, 22, 3, 4, 5, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		event schedule.Event
		want  string
	}{
		{
			name:  "created",
			event: schedule.Event{Kind: schedule.EventCreated, ScheduleName: "nightly-audit"},
			want:  "2026-08-22T03:04:05Z created   nightly-audit — on the clock",
		},
		{
			name:  "fired",
			event: schedule.Event{Kind: schedule.EventFired, ScheduleName: "nightly-audit", Prompt: "audit\nthe tree"},
			want:  "2026-08-22T03:04:05Z fired     nightly-audit — audit the tree",
		},
		{
			name: "completed",
			event: schedule.Event{
				Kind: schedule.EventCompleted, ScheduleName: "nightly-audit", Elapsed: 4*time.Minute + 1200*time.Millisecond,
				Outcome: schedule.Outcome{RecordID: "ses_abc", Turns: 12, Denied: 0},
			},
			want: "2026-08-22T03:04:05Z completed nightly-audit in 4m1s — 12 turns, 0 denied, saved as ses_abc",
		},
		{
			name: "completed with nothing saved",
			event: schedule.Event{
				Kind: schedule.EventCompleted, ScheduleName: "nightly-audit", Elapsed: time.Second,
				Outcome: schedule.Outcome{Turns: 1},
			},
			want: "2026-08-22T03:04:05Z completed nightly-audit in 1s — 1 turn, 0 denied, not saved",
		},
		{
			name: "failed",
			event: schedule.Event{
				Kind: schedule.EventFailed, ScheduleName: "nightly-audit", Elapsed: 2 * time.Second,
				Err: errors.New("the server refused the connection"),
			},
			want: "2026-08-22T03:04:05Z failed    nightly-audit after 2s — the server refused the connection",
		},
		{
			name:  "skipped",
			event: schedule.Event{Kind: schedule.EventSkipped, ScheduleName: "nightly-audit"},
			want:  "2026-08-22T03:04:05Z skipped   nightly-audit — the previous firing is still running",
		},
		{
			name:  "stopped",
			event: schedule.Event{Kind: schedule.EventStopped, ScheduleName: "nightly-audit"},
			want:  "2026-08-22T03:04:05Z stopped   nightly-audit — off the clock",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := &syncBuffer{}
			log := &daemonLog{out: out, now: func() time.Time { return at }}
			log.notify(tc.event)
			if got := strings.TrimRight(out.String(), "\n"); got != tc.want {
				t.Errorf("line =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Registration
// ----------------------------------------------------------------------------

// The command is reachable by name off the root, and registering it leaves the bare invocation
// alone — the seam's standing guarantee (root_test.go).
func TestDaemonIsRegisteredOnTheRoot(t *testing.T) {
	var found *cobra.Command
	for _, sub := range subcommands() {
		if sub.Name() == "daemon" {
			found = sub
		}
	}
	if found == nil {
		t.Fatal("`daemon` is not registered in subcommands()")
	}
	if !found.SilenceUsage || !found.SilenceErrors {
		t.Error("the command dumps usage or prints its own error; main owns both")
	}

	root := newRootCommand((&recordingLauncher{}).launch, subcommands()...)
	help := &syncBuffer{}
	root.SetOut(help)
	root.SetErr(help)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("apogee --help: %v", err)
	}
	if !strings.Contains(help.String(), "daemon") {
		t.Errorf("`apogee --help` does not list daemon:\n%s", help.String())
	}
}
