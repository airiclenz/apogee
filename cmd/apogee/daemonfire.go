package main

// The daemon's Firing composition (ADR 0034, ADR 0055) — the third Driver over the embeddable
// engine (ADR 0031), beside the TUI's scheduleWiring (schedule.go) and runHeadless (headless.go).
//
// A Firing raised here is the same unattended run those two compose — literally the same act,
// raise in wire_firing.go — resolved from a different set of facts: not the session's live
// binding and not one command's flags, but one validated entry of
// `~/.apogee/daemon/schedules.yaml` — which server it names, which model, which workspace, which
// mode — read against a `config.yaml` this process loaded once at startup.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/daemon"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/notice"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/reactions"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/sanitize"
	"github.com/airiclenz/apogee/internal/schedule"
	"github.com/airiclenz/apogee/internal/session"
)

// daemonWiring turns one Firing into one unattended run. It is the value wired into
// [schedule.Config.Fire], and its shape follows the daemon's two reload surfaces rather than the
// TUI's live session:
//
//   - The HOST half — the resolved config, the key sources, the confinement backend, the sessions
//     store — is resolved ONCE, by newDaemonWiring, because
//     `config.yaml` is read once at startup and rebinding servers is a daemon restart (ADR 0055).
//   - The ENTRY half — which server, which model, which workspace, which mode — is resolved per
//     Firing off the adopted set, because `schedules.yaml` IS live-reloaded (ADR 0034) and the
//     entry a name resolves to is whatever the last accepted edit said it was.
//
// That split is why the adopted set lives here rather than beside the daemon's loop: fire runs on
// the Scheduler's own goroutines while a reload swaps the set on the watcher's, so the map and its
// lock belong with the reader that has to survive the swap.
//
// The delegates that assume a human are never composed at all. run.Once pins its own fail-safe
// denier and leaves ask_user and present_document unregistered (ADR 0033, decision 2), and Tools is
// left nil for the engine to build its own registry — except under `sub-agents-choice: model`, the
// one gate that shapes the sub_agent schema rather than a Config field, where firingConfig hands
// over a roster of its own. A Firing reaches no external MCP server either way (ADR 0034).
type daemonWiring struct {
	// opts is the host's resolved configuration: the startup server selection held on it (StartupEntry),
	// the `servers:` list a schedule binds into by name, and every file-only key an unattended run
	// must honour for the one-configuration reason headless honours them (ADR 0031).
	opts config.Options
	// keys resolves an entry's key SOURCE into the token its Firings send. One resolver for the
	// daemon's lifetime, so a `api-key-cmd:` runs once per entry rather than once per Firing — it
	// is goroutine-safe, which is what lets Firings on different Schedules share it.
	keys *config.KeyResolver
	// confiner is this host's confinement backend, built once by newDaemonWiring through the
	// constructor the daemon was handed (daemonDeps). A Firing takes it whatever its mode: an Auto
	// entry is fenced by it, and a Plan entry's terminal commands are confined by the same posture a
	// session's are. Whether this host may run Auto unattended at all was ruled on at VALIDATION
	// (internal/daemon's Host.Confinement), where the refusal could name the entry, so nothing here
	// re-asks. [daemonWiring.closeConfiner] is the other end of it: a backend that has to put the
	// disk back is torn down when the daemon stops.
	confiner apogee.Confiner
	// runner is what every Firing of this daemon is handed to once its gates have passed
	// (firingInputs.runner) — the daemon's half of daemonDeps, carried here because fire runs on the
	// Scheduler's goroutines long after runDaemon's arguments are gone. nil is the production value,
	// resolved by raise when the Firing is raised.
	runner func(context.Context, run.Spec) (run.Result, error)
	// store is the shared sessions store every Firing's record lands in, so a schedule's runs are
	// browsable in /sessions beside the conversations and headless runs on this host (ADR 0034).
	store *session.Store
	// log is the daemon's whole user interface (daemon.go), reached from here because a Firing
	// narrates: what its composition had to say about the binding, and what its run found wrong
	// with the workspace's context files. One log for the daemon's lifetime, handed over by the
	// caller that owns it rather than built a second time here — two writers over one stream would
	// interleave halfway through a line, which is exactly what daemonLog's mutex exists to stop.
	log *daemonLog

	// mu guards adopted, which the daemon's reload replaces wholesale while Firings read it, and
	// the two per-process latches below. Firings on different Schedules run on their own
	// goroutines and overlap, so a check-and-set left outside the lock is how the same warning
	// gets said twice and the same workspace gets walked twice.
	mu sync.RWMutex
	// adopted is the daemon's name→Entry map: the daemon-only half of `run:` — workspace, server,
	// model — which deliberately does not travel through [schedule.Spec] because the scheduler
	// library is runner-agnostic (ADR 0033). A Firing carries the name; this is what turns it back
	// into the instruction the file states.
	adopted map[string]daemon.Entry
	// warnedUnconfined latches the unconfined-Auto warning (see [daemonWiring.latchUnconfinedWarning]).
	warnedUnconfined bool
	// saidWindowUnknown latches the unknown-context-window line (see [daemonWiring.latchWindowUnknown]).
	saidWindowUnknown bool
	// prewarmed is the set of workspace roots whose label walk this daemon has already pre-warmed
	// (see [daemonWiring.latchPrewarm]).
	prewarmed map[string]struct{}
}

// daemonDeps is what `apogee daemon` takes from its host rather than deciding for itself: the
// runner its Firings are handed to, and the constructor of the confinement backend they are fenced
// by — the same two facts, for the same reason, as headlessDeps (headless.go): both are properties
// of the MACHINE and the PROCESS the daemon happens to run in, so a test injects them through
// runDaemonWith or newDaemonWiring rather than swapping a seam under the whole package.
//
// A nil field is the production value, resolved where it is used and never at construction: a nil
// runner leaves daemonWiring.runner nil and raise reads the runOnce var when a Firing is raised; a
// nil confiner reads the newConfiner var when newDaemonWiring builds the backend. The vars, rather
// than run.Once and platform.NewConfiner, for as long as `/schedule` and the e2e stragglers still
// swap them — a daemon test that shares a helper with them must see the same value.
type daemonDeps struct {
	// runner is what a Firing runs through once its gates have passed (firingInputs.runner).
	runner func(context.Context, run.Spec) (run.Result, error)
	// confiner builds this host's confinement backend, once per daemon.
	confiner func() apogee.Confiner
}

// newDaemonWiring resolves everything about the HOST that every Firing of this daemon shares, and
// fails before a clock is started when any of it is wrong: a root it cannot resolve is a defect in
// the config the daemon must report at startup rather than at 3am in a saved record.
//
// The adopted set starts empty. A daemon adopts its first file immediately after this, through the
// same [daemonWiring.adopt] call every later reload makes.
//
// The log is the caller's, not this function's: a Firing narrates through the same stream the
// lifecycle lines land on, and the daemon owns when that stream is opened. So are the deps: the
// runner and the backend constructor are the host's to state (daemonDeps), and zero deps are the
// production values.
func newDaemonWiring(opts config.Options, log *daemonLog, deps daemonDeps) (*daemonWiring, error) {
	roots, err := resolveRoots(opts.ConfigDir, "")
	if err != nil {
		return nil, err
	}

	// The scratch sweep, run once at startup for the reason runRoot runs it (wire.go): a daemon
	// mints a fresh dir per Firing and never passes the TUI's boot, so this is the only beat on
	// which a host that only ever runs daemons reclaims the dirs its Firings left behind.
	// Best-effort and silent, exactly as it is there — GC is never a reason a daemon fails to start.
	gcScratchDirs(roots.scratch, time.Now())

	// The session sweep, on the same beat and for the same reason (wire.go): a host that only ever
	// runs daemons never passes the TUI's boot, so this is where its retention policy is applied.
	// A daemon resumes nothing — every Firing mints its own record — so no id is kept.
	store := session.NewStore(roots.sessions)
	gcSessions(store, opts.Sessions)

	// The undo stores' own sweep, on the same beat and for the same reason (wire_live.go runs it
	// beside the TUI's session sweep): every Firing opens a snapshot store of its own
	// (internal/run) and a host that only ever runs daemons never passes the TUI's boot, so without
	// this line those stores would accumulate forever. Once at startup rather than per Firing — a
	// sweep costs a directory read and a full store listing, work no Firing pays today — and after
	// the session sweep so a record that sweep just pruned is already gone when this one looks for
	// it.
	gcSnapshotDirs(roots.snapshots, store, time.Now())

	// The backend is built through the constructor the host handed this daemon (daemonDeps); a nil
	// one is the production route, read from the newConfiner seam here and not earlier.
	buildConfiner := deps.confiner
	if buildConfiner == nil {
		buildConfiner = newConfiner
	}

	return &daemonWiring{
		opts: opts,
		// The key resolver is built ROOTLESS because the `api-key-cmd:` exec fence is judged per
		// Firing, against the Firing's workspace: the daemon's workspace is the SCHEDULE ENTRY's,
		// minted per Firing, so there is no one root the daemon itself could measure an api-key
		// command against — firingConfig names the Firing's own roots.workspace on every
		// resolution (KeyResolver.ResolveWithin), memoised keys included.
		keys:      config.NewKeyResolver(""),
		confiner:  buildConfiner(),
		runner:    deps.runner,
		store:     store,
		log:       log,
		adopted:   make(map[string]daemon.Entry),
		prewarmed: make(map[string]struct{}),
	}, nil
}

// closeConfiner tears the confinement backend down at the end of the daemon's life and reports the
// one failure that has no other surface. The POSIX backends (landlock, namespace, seatbelt) need nothing; the Windows token
// backend has to put the disk back (ADR 0020 §2), and a teardown that could not is a silently
// mutated disk the user is otherwise never told about — which is why runRoot and runHeadless make
// this same optional-interface assertion, through the same wording (internal/platform).
//
// It returns the notice rather than printing it, because this one is not a Firing's narration —
// it is the daemon's own life ending, said on the stream every other lifecycle refusal is said on
// (stderr), and that stream is daemon.go's to choose. What [daemonWiring.fire] logs is the
// narration of one run, which is why that has a log to write to and this does not.
func (w *daemonWiring) closeConfiner() string {
	closer, ok := w.confiner.(interface{ Close() error })
	if !ok {
		return ""
	}
	return platform.ConfinementTeardownNotice(closer.Close())
}

// adopt replaces the set a Firing resolves its name against. It is called once with the file the
// daemon started on and once per accepted reload, always with the WHOLE validated set: an edit is
// all-or-nothing (ADR 0034), so there is no partial swap to express.
//
// A Firing already in flight is unaffected — it resolved its entry when it started, which is the
// instruction it was raised under.
func (w *daemonWiring) adopt(entries []daemon.Entry) {
	adopted := make(map[string]daemon.Entry, len(entries))
	for _, entry := range entries {
		adopted[entry.Name] = entry
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.adopted = adopted
}

// entryFor answers for one schedule name with the entry the daemon last adopted under it.
func (w *daemonWiring) entryFor(name string) (daemon.Entry, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	entry, adopted := w.adopted[name]
	return entry, adopted
}

// latchUnconfinedWarning reports whether THIS Firing is the one that says the unconfined-Auto
// warning, and marks it said. A launch says that sentence once because a launch happens once; a
// daemon fires forever, so the same latch has to be expressed explicitly or a nightly Auto schedule
// repeats the same paragraph in the supervisor's journal every tick until the disclosure is noise.
//
// It is a field on the wiring rather than a package-level sync.Once because two daemons in one test
// process are two daemons: the second one has its own user, its own log, and owes them the warning
// its own first Auto Firing triggers.
func (w *daemonWiring) latchUnconfinedWarning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.warnedUnconfined {
		return false
	}
	w.warnedUnconfined = true
	return true
}

// latchWindowUnknown reports whether THIS Firing is the one that says the unknown-context-window
// line, and marks it said. Same shape and same reason as the unconfined-Auto warning above: the
// sentence is about the daemon's CONFIGURATION — no `context-window:` is pinned, so the Budget and
// auto-compaction are inactive for every Firing this process will ever raise — and a fact that
// cannot change between ticks said once per tick is a nightly schedule writing the same line into
// the supervisor's journal forever. Every OTHER composition notice keeps logging per Firing,
// because those describe the run that just happened.
//
// It is per WIRING rather than a package-level sync.Once for the same reason: two daemons in one
// test process are two daemons, each owing its own log the disclosure its own first Firing earns.
func (w *daemonWiring) latchWindowUnknown() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.saidWindowUnknown {
		return false
	}
	w.saidWindowUnknown = true
	return true
}

// latchPrewarm reports whether workspace still owes its label walk a pre-warm, and marks it walked.
// Per WORKSPACE rather than per process, because the walk is of one tree: two schedules bound to
// two different workspaces each have a first confined command that would otherwise stall on it,
// while a second Firing of the same tree hits the backend's own once-per-root memo and would pay
// only for a second progress notice nobody needs (ADR 0020 §2).
func (w *daemonWiring) latchPrewarm(workspace string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, walked := w.prewarmed[workspace]; walked {
		return false
	}
	w.prewarmed[workspace] = struct{}{}
	return true
}

// fire performs one Firing and reports the record it left behind. It is [schedule.Config.Fire] for
// the daemon's single Scheduler, and it runs on that Scheduler's goroutine — never the reload's —
// which is why everything it touches is its own copy or explicitly goroutine-safe (the adopted map,
// the key resolver, the store, the skills Provider, the Confiner).
//
// The Config it runs against is composed by [firingConfig] (wire_firing.go), the one composer every
// Driver's unattended run is built by — an unattended run is an unattended run whichever Driver
// raised it (ADR 0031). What this Driver decides is what the ENTRY, rather than a flag or a live
// session, decides: the server it names, the model it overlays, and the workspace it runs in.
func (w *daemonWiring) fire(ctx context.Context, f schedule.Firing) (schedule.Outcome, error) {
	entry, adopted := w.entryFor(f.ScheduleName)
	if !adopted {
		return schedule.Outcome{}, fmt.Errorf("apogee: daemon: the %q schedule fired but no entry of that name is "+
			"adopted — the schedule was taken off the clock while this tick was due", f.ScheduleName)
	}

	// The `servers:` entry this Firing binds to, and the roots it lives in — the entry's own
	// workspace over the daemon's working directory, which is the one root a schedule decides.
	// Everything else those roots name is home-derived and shared by every Firing.
	server, err := w.serverFor(entry)
	if err != nil {
		return schedule.Outcome{}, err
	}
	roots, err := resolveRoots(w.opts.ConfigDir, entry.Run.Workspace)
	if err != nil {
		return schedule.Outcome{}, err
	}

	// The confinement posture THIS Firing runs under, said now that its mode and its workspace are
	// both known — the two facts the launch path has at startup and a daemon does not, because a
	// schedule entry carries its own mode and its own tree (ADR 0034).
	//
	// The unconfined-Auto warning first: `confine-to-workspace: false` is the one blanket loosen in
	// the system (ADR 0012), so it is stated wherever Auto runs under it, in the launch's own words
	// (unconfinedAutoWarning, wire_boot.go) rather than a second spelling a reader would have to
	// reconcile with the one they already met. It goes on the daemon log, which is this Driver's
	// whole user interface (ADR 0034 decision 10). Latched to once per daemon process: the sentence
	// is about the HOST's posture, which no tick changes.
	//
	// The daemon's OTHER confinement sentence — probe.ResidualNotice — is said at boot instead
	// (daemon.go), and stays there: its cell is the backend's own residual write class, a fact of
	// this host that every Firing shares, whereas this one is a fact of this ENTRY.
	if f.Mode == domain.ModeAuto && !w.opts.ConfineToWorkspace && w.latchUnconfinedWarning() {
		w.log.line("%s", unconfinedAutoWarning)
	}

	// Then the eager label walk, behind the SAME gate the launch and headless paths use
	// (shouldPrewarmLabelWalk, wire_boot.go) — one gate function, never a second copy, because a
	// second copy is how the boot paths drift apart. On the Windows token backend a confined
	// command labels the workspace tree at ~1 ms/object, and an unattended run is exactly where
	// that stall goes unexplained; off Windows PrewarmLabelWalk is an empty no-op
	// (internal/platform/prewarm_other.go), so this changes no byte of any other host's log.
	//
	// Latched per workspace path rather than per process, because what is warmed is one tree.
	// The progress notice leaves through the daemon log like every other thing a Firing narrates:
	// one seam, so the journal stays one timestamped line per event.
	if shouldPrewarmLabelWalk(f.Mode, w.opts.ConfineToWorkspace, w.confiner.Capabilities().FSWrite) &&
		w.latchPrewarm(roots.workspace) {
		platform.PrewarmLabelWalk(w.confiner, roots.workspace, daemonLogWriter{log: w.log})
	}

	// A Reaction's trouble goes on the daemon log, which is this Driver's whole user interface (ADR
	// 0034 decision 10), through daemonLogWriter rather than daemonLog.line — the writer is the
	// sanitiser this journal's one-line-per-event shape needs, and it takes the report as DATA, so a
	// `%` in some script's stderr is a percent sign rather than a format verb. The same function
	// serves both lanes of the `reactions:` list (firingInputs.report).
	reportReaction := func(line string) { _, _ = daemonLogWriter{log: w.log}.Write([]byte(line)) }

	// The one act every unattended run is (raise, wire_firing.go), reached from this Driver's own
	// facts: the bound entry, that entry's roots, the daemon's own key resolver — so an
	// `api-key-cmd:` runs once per entry rather than once per Firing — and the mode the Schedule
	// fired, which is the FIRING's and never re-read off the entry (ADR 0033, decision 3). The
	// Schedule this run belongs to travels with it, so this Firing's own Reaction Runner stamps it
	// onto every payload it hands out and a Reaction can tell "the 6am docs sweep finished" from
	// "the nightly audit finished" without a single field of per-event plumbing; a daemon's
	// workspace is the SCHEDULE ENTRY's, so two adopted schedules can be rooted in two different
	// trees and raise compares the `workspace:` filter against the one this tick runs in.
	//
	// The `model:` overlay is handed over as the entry states it. It is legal here because
	// validation already refused it where a model name would be a request to ACTUATE a load rather
	// than a per-request selection (ADR 0055 decision 2); absent, the composer falls back to the
	// bound entry's own `model:` — and on a launcher-fronted server that is whatever is serving,
	// because the daemon never actuates the launcher (decision 3): nothing serving means this Firing
	// fails visibly in its record and the next tick behaves normally.
	//
	// No skills catalog and no width source: a daemon holds no longer-lived provider to share and
	// has no heartbeat to take a slot count off, which is exactly what the composer's nil defaults
	// answer with. No onID and no narrate either: a daemon stamps the id on no stream, and the
	// record raise files under it is the account a supervisor opens.
	res, notices, err := raise(ctx, firingInputs{
		opts:     w.opts,
		entry:    server,
		keys:     w.keys,
		roots:    roots,
		confiner: w.confiner,
		runner:   w.runner,
		model:    entry.Run.Model,
		mode:     f.Mode,
		// The Scheduler's own clock, so the id this Firing is filed under is minted off the same
		// sense of time that made it due; nil in production ⇒ the wall clock (clockNow).
		now:    clockNow(daemonClock),
		report: reportReaction,
	}, f.Prompt, &reactions.ScheduleRef{ID: f.ScheduleID, Name: f.ScheduleName}, w.store, nil, nil)
	// What the composition had to say about this binding — a model the server never advertised, a
	// rebind that had to degrade — reaches the daemon LOG, which is this Driver's whole user
	// interface (ADR 0034 decision 10). The session record still carries the run; what it cannot
	// carry is why the run was composed the way it was, and a Firing that quietly bound something
	// other than what its entry names is exactly the fact a supervisor's journal has to hold.
	// Printed in the composer's own voice, unstripped, as runHeadless prints the same lines
	// (headless.go): these are apogee's sentences about apogee's own configuration. Printed BEFORE
	// the error is read, because raise returns them on every path: a refusal still had a
	// composition behind it, and what that composition said stands whether or not the run went on.
	//
	// The ONE exception to per-Firing narration is the unknown-context-window line, latched to once
	// per process: it reports a standing fact about this daemon's configuration rather than
	// anything this tick did, so a nightly schedule would otherwise repeat it in the journal
	// forever. Recognised by exact-string compare against the exported const, which is the whole
	// point of there being one spelling (internal/notice). Every other notice still logs per
	// Firing, and headless — one run per process — always says it.
	for _, n := range notices {
		if n == notice.WindowUnknown && !w.latchWindowUnknown() {
			continue
		}
		w.log.line("%s", n)
	}

	// A Firing refused before it began — a composition that would not produce a Config, or a bound
	// server that answered NOTHING — is this function's ERROR rather than an Outcome, which is what
	// "a refused Firing is a failed Firing" means concretely: the library lands it as
	// schedule.EventFailed and the daemon log renders it through the existing `failed <name> after
	// <elapsed> — <err>` line (daemon.go). It is never Faulted — internal/schedule reserves that for
	// a run that RETURNED with its Exchange at a boundary, and a run with no Turn at all has none —
	// and it records no Outcome, because nothing was sent and there is nothing to report. The
	// schedule's own retry and next-fire behaviour is untouched.
	//
	// The two stages are told apart by raise's typed refusal (errNotStarted) rather than by the
	// sentence, and mapped by the one helper both Firing-landing Drivers share (firingRefusal,
	// wire_firing.go): a composition refusal is wrapped under this Driver's own schedule-named
	// clause, so a supervisor reading the journal days later sees which entry would not compose,
	// and the gate's refusal passes bare — raise carries the gate's whole reasoning (Beat.Answered
	// false only for a transport-level failure, never for a 401, a 500 or a 429). Each Driver keeps
	// its own delivery — this one's is a failed Firing — and takes only the words.
	var refused errNotStarted
	if errors.As(err, &refused) {
		return schedule.Outcome{}, firingRefusal(
			fmt.Sprintf("apogee: daemon: resolve the %q schedule's", entry.Name), refused)
	}

	// What the run found WRONG with the workspace's context files, and only that: a file present
	// but unreadable, standing content that has outgrown its Budget share. The loaded-files line
	// stays off this log by ratified call — a daemon's journal is read for trouble, and one line
	// per Firing naming every file that loaded as expected is noise a week's worth of ticks
	// multiplies. Reported on an answer and on a failure that still produced a Result, because a
	// Firing that went wrong is the one whose loading is worth suspecting (internal/notice
	// composes; this Driver routes). A failure carrying a ZERO run.Result reports nothing here:
	// its report is empty and the composer yields no notice for it (daemonfire_test.go pins both
	// shapes side by side).
	//
	// Escape-stripped to a single line: the names trace to config and the errors to the
	// filesystem, and this log is one line per event — the same reason a prompt goes through
	// oneLine before it lands here (daemon.go).
	for _, n := range notice.ContextFileNotices(res.ContextFiles) {
		if !n.Anomaly {
			continue
		}
		w.log.line("%s", sanitize.StripEscapesToLine(n.Text))
	}

	// Everything the run learned about itself, mapped onto the scheduler's report by the one
	// mapping every Driver's Firing shares (firingOutcome, wire_firing.go), so both ends of this
	// function tell the daemon's log the same story. The library reads none of it — it is
	// runner-agnostic (ADR 0033) — and the Notify line renders the Firing from these fields alone:
	// the answer without decoding a record, the counts without a second seam onto the run. The
	// anomalies it carries are the sentences logged just above; the daemon log never renders them
	// off the Outcome, and it carries them because the Outcome is one shape for every Driver.
	out := firingOutcome(res)
	// What the Firing CHANGED on disk, after the Outcome is recorded and in the same block the
	// headless Driver prints (writtenFilesLines, headless.go): a header naming the count, then one
	// indented path per entry. This is the account an `auto:` schedule's deliverable IS — the state
	// of the workspace afterwards — and the daemon's log is the only place a supervisor sees it,
	// alongside the command that puts it back (undoVerbLine, logged just under it).
	//
	// Reported on an answer and on a failure that still produced a Result: a Firing that stopped
	// halfway is exactly the one whose partial writes a human has to know about. A `plan:` Firing
	// writes nothing and so logs nothing — the composer returns no lines for an empty list, which
	// is what a failure carrying a ZERO run.Result logs too.
	for _, line := range writtenFilesLines(res.Wrote) {
		w.log.line("%s", line)
	}
	// And the command that puts them back, on the same terms the headless Driver offers it: a
	// supervisor reading this log days later is exactly the reader ADR 0074's persistent journal
	// was for, and the log is the only place the Firing's session id and its writes appear together.
	if line := undoVerbLine(res); line != "" {
		w.log.line("%s", line)
	}

	if err != nil {
		// A failed Firing still reports what it salvaged: run.Once saves whatever completed before
		// it stopped, and naming that record is what lets a human open the interrupted run rather
		// than guess at it (partialRunSuffix, the wording every Driver's failure carries).
		if res.SessionID != "" {
			return out, fmt.Errorf("%w %s", err, partialRunSuffix(res.SessionID))
		}
		return out, err
	}
	return out, nil
}

// serverFor resolves the `servers:` entry one schedule's Firings bind to: the entry it names, or
// the startup default this host would give a fresh session when it names none (ADR 0055 decision
// 1). A name no entry answers to is already refused when the schedules file is validated, so
// reaching that branch means `config.yaml` and the adopted set have drifted apart — which the
// Firing reports rather than silently firing at the wrong server.
func (w *daemonWiring) serverFor(entry daemon.Entry) (config.ServerEntry, error) {
	named := strings.TrimSpace(entry.Run.Server)
	if named == "" {
		return w.opts.StartupEntry, nil
	}
	for _, server := range w.opts.Servers {
		if server.Name == named {
			return server, nil
		}
	}
	return config.ServerEntry{}, fmt.Errorf("apogee: daemon: the %q schedule binds to server %q, which no servers: "+
		"entry in config.yaml answers to — the daemon reads config.yaml once at startup, so restart it after "+
		"editing that list", entry.Name, named)
}

// daemonLogWriter is the daemon log seen as an [io.Writer], for the one caller that narrates
// through a writer rather than through this Driver: [platform.PrewarmLabelWalk] prints its progress
// notice to an io.Writer because the launch path hands it raw stderr. Routing that through the log
// rather than opening a second stream is what keeps the daemon's journal one timestamped line per
// event — two writers over one stream interleave halfway through a line, which is the whole reason
// daemonLog holds a mutex.
//
// Escape-stripped to a single line for the reason a Firing's context-file anomalies are: this
// journal is line-oriented, and a notice that carried a newline would forge a second timestamp-less
// entry.
type daemonLogWriter struct{ log *daemonLog }

var _ io.Writer = daemonLogWriter{}

// Write logs one notice. A blank write is dropped rather than logged as an empty line — an
// Fprintln of nothing is not an event.
func (w daemonLogWriter) Write(p []byte) (int, error) {
	if line := strings.TrimSpace(sanitize.StripEscapesToLine(string(p))); line != "" {
		w.log.line("%s", line)
	}
	return len(p), nil
}
