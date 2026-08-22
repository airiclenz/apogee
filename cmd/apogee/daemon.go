package main

// `apogee daemon` — the durable half of the scheduler (ADR 0034): a foreground process that reads
// `~/.apogee/daemon/schedules.yaml`, puts every entry on the clock, and runs each one's prompt
// through the same shared runner a session's Firing and `apogee headless` run through (ADR 0033).
//
// It is the third Driver over the embeddable engine (ADR 0031), and it is deliberately the THINNEST
// of the three. Everything that decides anything lives somewhere else: the file's schema and its
// validation in internal/daemon, the reload diff there too, the clock in internal/schedule, the
// composition of one Firing in daemonfire.go, the single-instance lock in internal/platform. What is
// left here is the lifecycle — seed, lock, load, adopt, reload, shut down — and the log lines that
// say what happened, because a process nobody watches has nothing else to say it with.
//
// Foreground only: it never forks, never writes a pid file it consults, and never detaches. A host
// supervisor — systemd, launchd, the Task Scheduler — owns restarts and log retention, and
// `apogee daemon install` (daemoninstall.go) generates the unit that does it.

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/daemon"
	"github.com/airiclenz/apogee/internal/filewatch"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/schedule"
)

const (
	// daemonDirName is the daemon's own subdirectory of the apogee home. Everything the daemon owns
	// on disk lives in it — the schedules file, the lock, and the Task Scheduler XML `install`
	// writes on Windows — so a human looking for "what is the daemon doing" has one place to look.
	daemonDirName = "daemon"
	// schedulesFileName is the ONLY contract between a human and the daemon (ADR 0034 decision 2):
	// there is no `apogee daemon add`, so this file is what says what runs.
	schedulesFileName = "schedules.yaml"
	// daemonLockFileName is the file the single-instance advisory lock is taken on. It sits beside
	// the schedules file because what it guards is that file: two daemons over one schedules file
	// would double-fire every entry in it.
	daemonLockFileName = "schedules.lock"
	// daemonTimeFormat stamps every log line. RFC3339 rather than a friendlier layout because these
	// lines are journalled by a supervisor and read back weeks later, where an unambiguous, sortable
	// instant with its offset is worth more than a compact one.
	daemonTimeFormat = time.RFC3339
)

// defaultSchedulesYAML is the starter schedules file compiled into the binary from
// defaults/schedules.yaml, on the `config.yaml` seeding precedent (internal/config's
// defaultConfigYAML): //go:embed re-reads it on every build, so the template a first run drops can
// never drift from the binary that dropped it.
//
// Every line of it is commented out, which parses as a valid EMPTY set (internal/daemon's Load).
// Seeding it therefore schedules nothing at all — the daemon starts, says it has 0 schedules, and
// watches — and what the file actually carries is the documentation for the keys, which is exactly
// what a hand-edited contract needs beside it.
//
//go:embed defaults/schedules.yaml
var defaultSchedulesYAML []byte

// acquireDaemonLock is the seam onto the single-instance lock (ADR 0034 decision 7). It exists for
// the reason runOnce does: a test must be able to drive the refusal — and, more often, to keep a
// test daemon off whatever lock the machine's real daemon may be holding — without a second
// process. Production never reassigns it.
var acquireDaemonLock = platform.AcquireLock

// daemonClock is the seam onto the Scheduler's sense of time. nil — its production value — is the
// wall clock and real tickers (schedule.Config.Clock), which is what a daemon whose shortest legal
// cycle is thirty seconds must run on. A test replaces it to make a due tick happen now rather than
// in half a minute. Production never reassigns it.
var daemonClock schedule.Clock

// watchSchedules is the seam onto the live-reload watch: it starts a stat-poll watcher over the
// schedules file (ADR 0041 decision 3, internal/filewatch) and hands back the channel that reports
// a settled save together with the stop that ends the watch and closes it.
//
// It is a seam rather than a direct call for the same reason daemonClock is one. What item 7 owns is
// what the daemon DOES when a save lands — re-read, refuse or swap — and a test of that should not
// have to out-wait a poll cadence to reach it; the poll, the settle window and the two witnesses are
// internal/filewatch's own tested behaviour. A test replaces this with a channel it sends on itself.
// Production never reassigns it.
var watchSchedules = func(path string) (<-chan struct{}, func()) {
	watcher := filewatch.New(path)
	watcher.Start()
	return watcher.Changes(), watcher.Stop
}

// newDaemonCommand builds `apogee daemon` — the standing process behind every schedule on this
// host. It carries exactly one flag: which apogee home to read. Everything else a run needs is a
// fact about the config file or the schedules file, and a daemon that took its bindings from an
// invocation would be a daemon whose behaviour a supervisor unit could silently contradict.
func newDaemonCommand() *cobra.Command {
	var opts config.Options

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run the scheduled prompts in ~/.apogee/daemon/schedules.yaml",
		Long: "apogee daemon runs the schedules declared in ~/.apogee/daemon/schedules.yaml. It\n" +
			"stays in the foreground and never detaches: a host supervisor owns restarts and log\n" +
			"retention, and `apogee daemon install` writes the unit for this OS.\n\n" +
			"The file is the whole contract — there is no command that edits it. On first run a\n" +
			"commented template is created; an existing file is never overwritten. Saved edits are\n" +
			"picked up while the daemon runs, and every schedule you did not touch keeps its place\n" +
			"in its own cycle. An edit that does not validate is refused whole, with every defect\n" +
			"logged and the previous schedules left running.\n\n" +
			"A firing is an unattended run, so it never asks: every gated action is refused rather\n" +
			"than parked, ask_user and present_document are not registered, and no MCP server is\n" +
			"contacted. Only plan and auto make sense there and only those two are accepted. Every\n" +
			"firing is saved to ~/.apogee/sessions and is browsable in /sessions — which for a plan\n" +
			"schedule IS the deliverable, while an auto schedule leaves its work in the workspace.\n\n" +
			"A schedule binds to a server by name out of the servers: list in config.yaml, so no\n" +
			"secret is ever written into the schedules file. config.yaml is read once, at startup:\n" +
			"changing your servers means restarting the daemon. The daemon never loads a model — a\n" +
			"firing against a server with nothing serving fails visibly in its own record.\n\n" +
			"Only one daemon may run per apogee home; a second one refuses to start. The first\n" +
			"SIGTERM or Ctrl-C stops the clock and gives a firing already in flight up to\n" +
			"shutdown-grace (10m by default) to finish; a second one cancels it immediately.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Two signals, so the buffer holds the second one even if it lands while the first is
			// still being acted on: the whole point of the second Ctrl-C is that an operator who
			// has stopped waiting is not made to wait for the delivery too.
			signals := make(chan os.Signal, 2)
			signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
			defer signal.Stop(signals)

			return runDaemon(cmd.Context(), &opts, cmd.Flags().Changed,
				cmd.OutOrStdout(), cmd.ErrOrStderr(), signals)
		},
	}

	cmd.Flags().StringVar(&opts.ConfigDir, "config", "",
		"apogee home directory for config/library/sessions (default: ~/.apogee)")

	cmd.AddCommand(newDaemonInstallCommand())

	return cmd
}

// runDaemon is the command's whole body, taking its signals and its streams as arguments so the
// lifecycle is drivable without a process to send a real signal to. It returns nil for a clean
// stop and an error for every startup refusal — a held lock, a config that will not resolve, a
// schedules file that will not validate — which main turns into exit 1.
//
// The order below is the order the refusals are worth making in. Configuration first, because a
// daemon that cannot say which server it talks to has nothing to schedule; the lock before the
// confinement backend, so a second daemon refuses without building anything; the file after the
// lock, because validating it is only useful for the daemon that is going to run it.
func runDaemon(ctx context.Context, opts *config.Options, changed func(string) bool,
	out, errOut io.Writer, signals <-chan os.Signal) error {
	// The same resolution a session performs (flag > env > file > default), so a Firing runs against
	// the server, and with the Mechanisms, a session on this host would (ADR 0031). Notices go to
	// stderr; the daemon's own narration goes to stdout, which is what a supervisor journals.
	if err := config.ApplyConfig(opts, changed, os.Getenv, os.ReadFile, func(msg string) {
		fmt.Fprintln(errOut, msg)
	}); err != nil {
		return err
	}

	home, err := config.ApogeeHome(opts.ConfigDir)
	if err != nil {
		return err
	}
	dir := filepath.Join(home, daemonDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("apogee daemon: create %s: %w", dir, err)
	}

	log := &daemonLog{out: out, now: time.Now}
	schedulesPath := filepath.Join(dir, schedulesFileName)
	created, err := seedSchedulesFile(schedulesPath)
	if err != nil {
		return err
	}
	if created {
		log.line("created a starter schedules file at %s", schedulesPath)
	}

	// The single-instance refusal, before anything else is built. Two daemons over one schedules
	// file would double-fire every entry in it, and the lock is what makes that impossible rather
	// than unlikely: the kernel drops it with the process, so there is no stale state to reason
	// about and no liveness probe to get wrong (internal/platform's lock.go).
	release, err := acquireDaemonLock(filepath.Join(dir, daemonLockFileName))
	if err != nil {
		var held *platform.LockHeldError
		if errors.As(err, &held) {
			return fmt.Errorf("apogee daemon: %s", alreadyRunning(*held))
		}
		return fmt.Errorf("apogee daemon: %w", err)
	}
	defer release()

	// The host half of every Firing — the confinement backend, the key resolver, the sessions store,
	// the validated `mechanisms:` list — resolved once, because config.yaml is read once (ADR 0055).
	wiring, err := newDaemonWiring(*opts)
	if err != nil {
		return err
	}
	// The teardown the Windows token backend needs to put the disk back (ADR 0020 §2) — the same
	// optional-interface assertion runRoot and runHeadless make, for the same reason. It is deferred
	// rather than run at the end of the shutdown sequence so that every refusal below leaves through
	// it too.
	defer func() {
		if notice := wiring.closeConfiner(); notice != "" {
			fmt.Fprintln(errOut, notice)
		}
	}()

	// The facts internal/daemon's validation cannot learn from the file, resolved once: config.yaml
	// is startup-only (ADR 0055), so the same host answers every reload as answered the first load.
	host := daemonHost(*opts, home, wiring)
	file, err := loadSchedules(schedulesPath, host, log)
	if err != nil {
		return err
	}

	// Gate: nil is the daemon's whole quiescence policy (schedule.Config.Gate says so in as many
	// words). The TUI defers a Firing until the human's session is idle because the two share one
	// single-slot server; a daemon has no session in the way, so a due Firing is simply due.
	scheduler, err := schedule.New(schedule.Config{
		Fire:   wiring.fire,
		Notify: log.notify,
		Clock:  daemonClock,
	})
	if err != nil {
		return fmt.Errorf("apogee daemon: build the scheduler: %w", err)
	}

	// The startup adoption is a reload against an EMPTY running set — the same call an edit makes,
	// so the two paths cannot drift apart (ADR 0034's all-or-nothing swap).
	ids := make(map[string]string, len(file.Schedules))
	if _, err := adoptSchedules(scheduler, wiring, ids, nil, file.Schedules); err != nil {
		scheduler.Close()
		return fmt.Errorf("apogee daemon: %w", err)
	}

	// The live-reload watch. `schedules.yaml` is the ONLY file the daemon re-reads while it runs, and
	// the watch starts AFTER the first adoption so that a save landing during startup is measured
	// against the file the daemon actually put on the clock.
	changes, stopWatching := watchSchedules(schedulesPath)
	defer stopWatching()
	log.line("watching %s — %s on the clock", schedulesPath, countedSchedules(len(file.Schedules)))

	// One wait, two things worth waking for. Folding the watch into the stop request's own select is
	// what keeps a reload from racing a shutdown: the loop is single-threaded, so a save being acted
	// on finishes before the stop is read, and a stop already asked for is never overtaken by a save.
	stopRequested := daemonStopRequest(ctx, signals, log)
	for {
		select {
		case <-stopRequested:
			daemonShutdown(scheduler, ids, file, log, signals, ctx.Done())
			return nil
		case _, watching := <-changes:
			if !watching {
				// The watch ended without the daemon being asked to stop — nothing further will ever
				// arrive on it, so it leaves the select and the daemon runs on with the set it has.
				changes = nil
				log.line("the schedules watch ended — no further edits will be picked up until the daemon restarts")
				continue
			}
			file = reloadSchedules(scheduler, wiring, ids, schedulesPath, host, file, log)
		}
	}
}

// adoptSchedules is the ONE way onto the clock: startup calls it with an empty running set, every
// accepted edit with the set the daemon last adopted. Sharing it is what keeps the two from
// drifting — an entry adopted at boot and the same entry adopted by a save reach the scheduler and
// the Firing composition through exactly the same two calls, in the same order.
//
// The wiring adopts the desired set whatever [daemon.Apply] reported, because the id map — not the
// returned Reload — is what says who is on the clock. An entry whose Add failed is absent from that
// map and can never fire, so carrying its `run:` half costs nothing, while withholding the set on a
// partial failure would leave the entries that DID land with no workspace to run in.
func adoptSchedules(scheduler *schedule.Scheduler, wiring *daemonWiring, ids map[string]string,
	running, desired []daemon.Entry) (daemon.Reload, error) {
	reload, err := daemon.Apply(scheduler, ids, running, desired)
	wiring.adopt(desired)
	return reload, err
}

// reloadSchedules acts on one settled save and returns the file the daemon runs from here on: the
// new one when it validated, the one already running when it did not. `shutdown-grace` therefore
// takes effect on the next shutdown rather than the current one, which is the only moment it means
// anything.
//
// The swap is all-or-nothing (ADR 0034 decision 3). ONE defect anywhere refuses the WHOLE file and
// changes nothing, because a partial swap would leave the file a human just saved and the set the
// daemon is running disagreeing, with nothing on screen to say which entries took — and the daemon
// has no screen. The refusal names every defect and then says what is still running, so a journal
// read the next morning shows both halves of the answer.
func reloadSchedules(scheduler *schedule.Scheduler, wiring *daemonWiring, ids map[string]string,
	path string, host daemon.Host, running daemon.File, log *daemonLog) daemon.File {
	file, defects, err := readSchedules(path, host, log)
	if err != nil {
		if defects == 0 {
			// A read that failed outright — the file was deleted, or its permissions changed. There
			// are no defect lines in the log for it, so the reason is stated here.
			log.line("%s could not be read: %v", path, err)
		}
		log.line("the edit was refused whole — keeping the previous %s on the clock", countedSchedules(len(ids)))
		return running
	}

	// The running set is what the id map actually holds, not what the previous file listed: an entry
	// whose Add failed is not on the clock, and diffing as though it were would ask the scheduler to
	// stop something it never started.
	reload, err := adoptSchedules(scheduler, wiring, ids, adoptedEntries(running.Schedules, ids), file.Schedules)
	log.line("%s", reloadSummary(reload))
	if err != nil {
		log.line("some of the edit did not take: %v", err)
	}
	return file
}

// reloadSummary is the one line an accepted save logs: what it added, replaced and removed, with the
// untouched entries counted rather than named. Naming them is what the line must not do — a file of
// twenty schedules where one changed would otherwise bury the one name that matters under nineteen
// that did not, on every save.
//
// A cosmetic edit — a comment, a reordering, a whitespace change — says so plainly, because a human
// who saves a file and sees nothing in the log cannot tell a reload that decided nothing from a
// watch that never fired.
func reloadSummary(reload daemon.Reload) string {
	if !reload.Changed() {
		return "reloaded " + schedulesFileName + " — no change; every schedule keeps its place in its cycle"
	}
	parts := make([]string, 0, 4)
	for _, part := range []struct {
		verb  string
		names []string
	}{
		{"added", reload.Added},
		{"replaced", reload.Replaced},
		{"removed", reload.Removed},
	} {
		if len(part.names) > 0 {
			parts = append(parts, part.verb+" "+strings.Join(part.names, ", "))
		}
	}
	if kept := len(reload.Kept); kept > 0 {
		parts = append(parts, fmt.Sprintf("%s untouched", countedSchedules(kept)))
	}
	return "reloaded " + schedulesFileName + " — " + strings.Join(parts, "; ")
}

// daemonStopRequest returns the channel that closes when the daemon is asked to stop: the first
// SIGTERM or Ctrl-C, or the command context going away. It logs the reason, so the log says which
// of the two ended the process rather than simply stopping mid-sentence.
func daemonStopRequest(ctx context.Context, signals <-chan os.Signal, log *daemonLog) <-chan struct{} {
	asked := make(chan struct{})
	go func() {
		defer close(asked)
		select {
		case sig := <-signals:
			log.line("%v — stopping the clock", sig)
		case <-ctx.Done():
			log.line("the daemon's context ended — stopping the clock")
		}
	}()
	return asked
}

// daemonShutdown is design call 8's sequence: take every schedule off the clock so nothing new
// starts, give a Firing already in flight up to the file's `shutdown-grace` to finish, then cancel
// whatever is left. A second signal skips the wait — an operator who signals twice has stopped
// waiting, and making them wait anyway is the one thing a shutdown must not do.
//
// Cancelling is Scheduler.Close, which cancels the context every Firing runs under and joins the
// goroutines. What a cancelled Firing had already completed is still saved: internal/run persists
// what it reached, which is why cancelling is a defensible end to the grace rather than a loss.
func daemonShutdown(scheduler *schedule.Scheduler, ids map[string]string, file daemon.File,
	log *daemonLog, signals <-chan os.Signal, ended <-chan struct{}) {
	// Stopping is the same diff every reload runs, against an empty desired set. A Stop ends the
	// CYCLE and never the run in flight (schedule.Scheduler.Stop), which is exactly the split the
	// grace below is about.
	if _, err := daemon.Apply(scheduler, ids, adoptedEntries(file.Schedules, ids), nil); err != nil {
		log.line("stopping the schedules: %v", err)
	}

	drained := log.firings.drain()
	select {
	case <-drained:
	default:
		log.line("a firing is still running — up to %s for it to finish, or signal again to cancel now",
			file.ShutdownGrace)
		grace := time.NewTimer(file.ShutdownGrace)
		defer grace.Stop()
		select {
		case <-drained:
		case <-grace.C:
			log.line("the %s grace expired — cancelling the firing in flight", file.ShutdownGrace)
		case sig := <-signals:
			log.line("%v again — cancelling the firing in flight", sig)
		case <-ended:
			log.line("the daemon's context ended — cancelling the firing in flight")
		}
	}

	scheduler.Close()
	log.line("stopped")
}

// adoptedEntries is the running set a shutdown diffs from: the entries that are actually on the
// clock, which is what the id map names. An entry whose Add failed is not among them, so the
// shutdown never asks the scheduler to stop something it never started.
func adoptedEntries(entries []daemon.Entry, ids map[string]string) []daemon.Entry {
	running := make([]daemon.Entry, 0, len(ids))
	for _, entry := range entries {
		if _, onTheClock := ids[entry.Name]; onTheClock {
			running = append(running, entry)
		}
	}
	return running
}

// alreadyRunning is the second daemon's refusal, in the words a human needs: which process has it,
// when the lock file carried a readable pid, and which file to look at when it did not. The pid is
// diagnostics only — the LOCK is what says the holder is alive (internal/platform's lock.go) — so
// the sentence never suggests acting on the number beyond finding the process.
func alreadyRunning(held platform.LockHeldError) string {
	if held.PID > 0 {
		return fmt.Sprintf("apogee daemon is already running (pid %d) — only one daemon may run per "+
			"apogee home, because two over one schedules file would fire every schedule twice", held.PID)
	}
	return fmt.Sprintf("apogee daemon is already running — only one daemon may run per apogee home, "+
		"because two over one schedules file would fire every schedule twice (the lock is %s)", held.Path)
}

// seedSchedulesFile drops the embedded template at path when nothing is there yet, and reports
// whether it created it. An existing file is never touched: it is the user's contract, and the
// template's only job is to make the file discoverable on the first run (internal/config's
// seedConfig, whose shape this follows).
func seedSchedulesFile(path string) (bool, error) {
	switch _, err := os.Stat(path); {
	case err == nil:
		return false, nil
	case !errors.Is(err, os.ErrNotExist):
		return false, fmt.Errorf("apogee daemon: stat %s: %w", path, err)
	}
	if err := os.WriteFile(path, defaultSchedulesYAML, 0o600); err != nil {
		return false, fmt.Errorf("apogee daemon: write the starter schedules file %s: %w", path, err)
	}
	return true, nil
}

// readSchedules reads and validates the schedules file, logging EVERY defect on its own line, and
// reports how many there were. It is the half startup and reload share; what they do NOT share is
// what a refusal falls back to, so the sentence that says so belongs to each caller and not here.
//
// A read failure — an absent file, a permission change — reports zero defects, which is how a caller
// tells "the file says something wrong" from "there was no file to ask".
func readSchedules(path string, host daemon.Host, log *daemonLog) (daemon.File, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return daemon.File{}, 0, fmt.Errorf("read %s: %w", path, err)
	}
	file, err := daemon.Load(data, host)
	if err == nil {
		return file, 0, nil
	}
	defects := daemonDefects(err)
	for _, defect := range defects {
		log.line("%s", defect)
	}
	return daemon.File{}, len(defects), fmt.Errorf("%s has %s", path, countedDefects(len(defects)))
}

// loadSchedules is the STARTUP read. At startup there is no previous set to fall back on — that is
// the reload path's answer (ADR 0034) — so a defective file stops the daemon rather than starting it
// with nothing on the clock, which would look like a daemon that is working.
//
// The returned error is a summary rather than the joined defects: the defects have already been
// logged where a supervisor journals them, and main would otherwise print all of them a second time
// on the way out.
func loadSchedules(path string, host daemon.Host, log *daemonLog) (daemon.File, error) {
	file, defects, err := readSchedules(path, host, log)
	switch {
	case err == nil:
		return file, nil
	case defects == 0:
		return daemon.File{}, fmt.Errorf("apogee daemon: %w", err)
	}
	return daemon.File{}, fmt.Errorf("apogee daemon: %w — nothing was scheduled; fix the file and start again", err)
}

// daemonDefects splits a validation failure into the lines it is made of. internal/daemon joins its
// defects with errors.Join, whose message is one per line, and a log line per defect is what makes
// a supervisor's journal say which entries are wrong rather than showing one wrapped paragraph.
func daemonDefects(err error) []string {
	lines := strings.Split(strings.TrimSpace(err.Error()), "\n")
	defects := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			defects = append(defects, trimmed)
		}
	}
	return defects
}

// daemonHost is the facts internal/daemon's validation cannot learn from the file itself: where a
// `~` expands to, what a `server:` name resolves to, and whether this host may run an unattended
// Auto at all.
//
// The server lookup answers for the EMPTY name as well as for a written one, because that is what
// an entry with no `server:` asks with — the host's startup default. Skipping it would leave every
// default-bound entry outside ADR 0055's rule that a `model:` on a launcher-fronted server is a
// request to actuate a load, which is precisely the entry most people will write first.
//
// Auto eligibility is the SAME verdict `apogee headless` and the `/schedule` picker reach, through
// the same function, because a user who meets this refusal at one surface must not meet a weaker
// story at another (ADR 0033 decision 3).
func daemonHost(opts config.Options, home string, wiring *daemonWiring) daemon.Host {
	return daemon.Host{
		Home: home,
		LookupServer: func(name string) (daemon.ServerFacts, bool) {
			if strings.TrimSpace(name) == "" {
				// The startup entry's own `llama-launcher:` value, flattened onto options by
				// resolution (config.Options.StartupLauncher). An ephemeral --endpoint run carries
				// none, which is the honest answer for an endpoint no entry names.
				return daemon.ServerFacts{IsLauncherFronted: opts.StartupLauncher != ""}, true
			}
			for _, server := range opts.Servers {
				if server.Name == name {
					return daemon.ServerFacts{IsLauncherFronted: server.LlamaLauncher != ""}, true
				}
			}
			return daemon.ServerFacts{}, false
		},
		AutoEligible: autoUnattendedBlocked("a firing", probe.BackendName(wiring.confiner),
			wiring.confiner.Capabilities(), opts.ConfineToWorkspace) == "",
	}
}

// countedSchedules and countedDefects are the two counted nouns the log lines above use. They exist
// so a log never says "1 schedules", which is the kind of seam a reader notices instead of reading.
func countedSchedules(n int) string { return counted(n, "schedule", "schedules") }

func countedDefects(n int) string { return counted(n, "defect", "defects") }

func counted(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// ----------------------------------------------------------------------------
// The log
// ----------------------------------------------------------------------------

// daemonLog is the daemon's whole user interface: plain timestamped lines on stdout, which the host
// supervisor captures and rotates (ADR 0034 decision 10 — apogee owns no log files and no retention
// policy of its own).
//
// It is written from several goroutines at once — the Scheduler emits every Event from a goroutine
// it owns, one per Schedule (schedule.Config.Notify) — so the mutex is what keeps two Firings that
// land together from interleaving halfway through a line.
type daemonLog struct {
	mu  sync.Mutex
	out io.Writer
	now func() time.Time
	// firings counts what is in flight, from the same Events the lines are rendered from. The
	// shutdown grace is a wait on that count reaching zero, and the count has no other source: the
	// Scheduler's own List stops reporting a Schedule the moment it is stopped, which is exactly
	// when a shutdown starts asking.
	firings firingTracker
}

// line writes one timestamped log line.
func (l *daemonLog) line(format string, args ...any) {
	stamp := l.now().Format(daemonTimeFormat)
	message := fmt.Sprintf(format, args...)

	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.out, "%s %s\n", stamp, message)
}

// notify renders one scheduler Event. It is [schedule.Config.Notify] for the daemon's Scheduler,
// and it is also where the in-flight count is kept — a Firing is in flight from its fired event to
// whichever of completed or failed ends it, which is the same pair of brackets the lines report.
//
// The Event kinds are spelled out rather than defaulted so that a kind added to the library later
// fails this switch's own test rather than vanishing from the log.
func (l *daemonLog) notify(ev schedule.Event) {
	switch ev.Kind {
	case schedule.EventCreated:
		l.line("created   %s — on the clock", ev.ScheduleName)
	case schedule.EventFired:
		l.firings.started()
		l.line("fired     %s — %s", ev.ScheduleName, oneLine(ev.Prompt))
	case schedule.EventCompleted:
		l.firings.landed()
		l.line("completed %s in %s — %s", ev.ScheduleName, daemonElapsed(ev.Elapsed), daemonOutcome(ev.Outcome))
	case schedule.EventFailed:
		l.firings.landed()
		l.line("failed    %s after %s — %v", ev.ScheduleName, daemonElapsed(ev.Elapsed), ev.Err)
	case schedule.EventSkipped:
		l.line("skipped   %s — the previous firing is still running", ev.ScheduleName)
	case schedule.EventStopped:
		l.line("stopped   %s — off the clock", ev.ScheduleName)
	}
}

// daemonOutcome is what a completed Firing left behind, in one clause: the work it did and the
// record it can be read in. A run that saved nothing says so rather than printing an empty id —
// that is the shape of a --no-save run and of a store that refused, and both are worth noticing.
func daemonOutcome(out schedule.Outcome) string {
	work := fmt.Sprintf("%s, %d denied", counted(out.Turns, "turn", "turns"), out.Denied)
	if out.RecordID == "" {
		return work + ", not saved"
	}
	return fmt.Sprintf("%s, saved as %s", work, out.RecordID)
}

// daemonElapsed renders a Firing's duration at the resolution a schedule cares about. A firing is
// measured in minutes and the shortest legal cycle is thirty seconds, so sub-second precision is
// noise in a line a human scans a week's worth of.
func daemonElapsed(d time.Duration) string { return d.Round(time.Second).String() }

// oneLine flattens a prompt onto the single line a log entry is. A prompt is whatever a human would
// type into apogee, newlines included, and a multi-line one would otherwise turn one event into
// several lines with no timestamps on them.
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
}

// ----------------------------------------------------------------------------
// The in-flight count
// ----------------------------------------------------------------------------

// firingTracker counts the Firings between their fired and completed/failed events, and hands the
// shutdown a channel that closes when nothing is left in flight.
//
// The count is kept HERE rather than read off the Scheduler because Stop takes a Schedule out of
// List immediately while leaving its in-flight Firing to finish — so at the exact moment a shutdown
// needs the answer, the library's own surface has stopped giving it.
//
// It only ever signals while draining. Before that, an idle moment between two cycles is not the
// end of anything, and closing the channel then would let a shutdown that started a microsecond
// later skip a grace it should have waited out.
type firingTracker struct {
	mu       sync.Mutex
	inFlight int
	draining bool
	idle     chan struct{}
}

// started records that a Firing has begun.
func (t *firingTracker) started() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.inFlight++
}

// landed records that a Firing has ended, however it ended.
func (t *firingTracker) landed() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inFlight > 0 {
		t.inFlight--
	}
	if t.draining && t.inFlight == 0 {
		t.closeIdle()
	}
}

// drain starts the wait and returns the channel that closes once nothing is in flight — already
// closed when nothing was in flight to begin with, which is what lets a shutdown with no Firing
// running skip the grace entirely and exit at once.
func (t *firingTracker) drain() <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.draining = true
	if t.inFlight == 0 {
		t.closeIdle()
	}
	return t.idleChan()
}

// closeIdle closes the idle channel once. The caller holds the mutex.
func (t *firingTracker) closeIdle() {
	idle := t.idleChan()
	select {
	case <-idle:
	default:
		close(idle)
	}
}

// idleChan is the lazily-built idle channel, so the zero firingTracker is usable. The caller holds
// the mutex.
func (t *firingTracker) idleChan() chan struct{} {
	if t.idle == nil {
		t.idle = make(chan struct{})
	}
	return t.idle
}
