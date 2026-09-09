package main

// Reactions end to end (ADR 0073): runs whose `reactions:` block actually runs a command and
// actually POSTs a webhook, asserted from OUTSIDE apogee — a file on disk the command appended to,
// and an httptest server the webhook reached. Everything below the composition root has unit tests
// in internal/reactions; what these prove is the wiring the unit tests cannot see, and the promise
// that costs the most to break: nothing a Reaction does reaches the screen or the Session record.
//
// Three roots compose that wiring and each gets its own case: the driven TUI session, an unattended
// `apogee headless` run, and one daemon Firing. The last two are where the payload's `schedule`
// field earns its keep — a Firing names the Schedule it ran for, a plain headless run names none —
// and where a Reaction's trouble has no transcript to land in and goes to stderr and to the daemon
// log instead.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/reactions"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The environment a fired Reaction is expected to inherit. A Reaction's command is run with
// apogee's own environment plus the payload's APOGEE_REACTION_* facts, which is what lets a
// one-line `sh -c` script know where to write without the test rewriting the script for every temp
// directory.
const (
	hookSinkEnv     = "APOGEE_TEST_SINK"
	hookEnvSinkEnv  = "APOGEE_TEST_ENV_SINK"
	hookTokenEnv    = "APOGEE_TEST_HOOK_TOKEN"
	hookTokenValue  = "hook-token-not-in-the-config-file"
	hookTokenHeader = "X-Apogee-Hook-Token"
)

// hookMarkers are the spellings a Reaction would leave behind that no run's own vocabulary can
// produce: the environment a fired command inherits, and the event names its payload carries. None
// of them may appear in a frame — or on the streams an unattended root writes — of a run whose
// Reactions all succeeded (ADR 0073 §1).
//
// The reports a Reaction can put on screen are NOT in this list. They are caught by shape instead —
// see [hookReportPattern] — because a whitelist of prose stops biting the moment a report is
// reworded.
var hookMarkers = []string{
	"APOGEE_REACTION",
	string(reactions.FileChanged), string(reactions.ExchangeFinished), string(reactions.ApprovalRequested),
}

// hookReportPattern is the shape EVERY report the reactions path can put in front of a human takes:
// the word `reaction`, the entry's configured name, and then a separator — a colon before a Runner
// message (internal/reactions/runner.go:240, :397) or a space before the parenthesised event of a
// failure (:363). Those reports reach a frame through Report → Bridge.NotifyHook → an ephemeral
// note, and an unattended root's stderr the same way.
//
// It is built from the names a case CONFIGURES rather than from the prose those sites happen to use
// today, which is what keeps an absence check armed: renaming a Reaction in the config re-aims the
// pattern instead of silently un-arming it, and a report worded in some way nobody whitelisted —
// a queue-drop line, say — still matches.
func hookReportPattern(names ...string) *regexp.Regexp {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, regexp.QuoteMeta(name))
	}
	return regexp.MustCompile(`reaction (` + strings.Join(quoted, "|") + `)[ :(]`)
}

// TestE2EHooksAbsenceCheckMatchesARenamedHooksReport is the bite the whitelist [hookReportPattern]
// replaced never had. That whitelist spelled the reports out — "reaction sink", "reaction bell" —
// so it went blind on the one config change it exists to survive: a Reaction renamed in the very
// block the absence check is watching. The derived pattern follows the rename instead, and covers
// every report shape the Runner emits rather than the two a whitelist happened to list.
func TestE2EHooksAbsenceCheckMatchesARenamedHooksReport(t *testing.T) {
	t.Parallel()

	const renamed = "watcher"
	pattern := hookReportPattern(renamed)

	for _, report := range []string{
		"reaction " + renamed + ": dropped 3 events",
		"reaction " + renamed + ": dropped 1 event (queue full)",
		"reaction " + renamed + " (turn-finished): exit 1",
	} {
		if strings.Contains(report, "reaction "+hooksSinkName) {
			t.Fatalf("the report %q still carries the whitelisted spelling, so it cannot prove "+
				"that a rename is what the pattern buys", report)
		}
		if !pattern.MatchString(report) {
			t.Errorf("the pattern %v misses %q, a report a renamed Reaction would put on screen",
				pattern, report)
		}
	}

	// And it stays silent about what the runs themselves say, which is what makes a match a
	// finding rather than noise — the separator included: a Reaction named `sink` does not make the
	// word `sinks` a reaction report.
	configured := hookReportPattern(hooksSinkName, hooksBellName, hooksFailingName)
	for _, innocent := range []string{
		hooksAnswer,
		smokeWriteReply,
		"the reaction sinks are files the test reads",
	} {
		if report := configured.FindString(innocent); report != "" {
			t.Errorf("the pattern reads %q in %q, which no Reaction wrote", report, innocent)
		}
	}
}

// smokeWriteReply is what the smoke script answers the write prompt with
// (testdata/stubllm/smoke.yaml), and so the one thing the TUI half's final frame is certain to
// carry. The absence check uses it as its positive control: a frame that has lost it is not the
// frame the journey produced, and its silence about Reactions would prove nothing.
const smokeWriteReply = "Appended the smoke test line"

// TestE2EHooksFireFromTheTUI drives the smoke journey with two Reactions configured — a command on
// `file-changed` and `exchange-finished`, a webhook on `approval-requested` — and asserts what each
// of them received, from the outside: the file the command appended to, and the requests the
// httptest server recorded.
//
// The webhook claim is made BEFORE the approval is answered, which is the whole point of the event:
// a Reaction that only learned about a waiting approval after the human dealt with it could not
// ring a bell for the prompt nobody was watching.
func TestE2EHooksFireFromTheTUI(t *testing.T) {
	bell, server := newHookWebhook(t)
	// Closed by defer rather than t.Cleanup so it is torn down BEFORE the leak check and the
	// driver's own cleanups, whose ordering it must not depend on.
	defer server.Close()

	sink := filepath.Join(t.TempDir(), "fired.jsonl")
	envSink := filepath.Join(t.TempDir(), "fired.env")
	t.Setenv(hookSinkEnv, sink)
	t.Setenv(hookEnvSinkEnv, envSink)
	t.Setenv(hookTokenEnv, hookTokenValue)

	script, err := stubllm.Load("testdata/stubllm/smoke.yaml")
	if err != nil {
		t.Fatalf("load the smoke script: %v", err)
	}
	stub := stubllm.New(t, script)
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIConfigured(t, drv, stub, hookBlock(server.URL))

	// One exchange that changes nothing, so `exchange-finished` has fired before any write has.
	submit(drv, "What files are in this workspace?")
	drv.WaitText("The workspace holds one file")

	// The write asks first, and the webhook hears about it while the pane is still up.
	submit(drv, `Append a line saying "smoke test" to a.txt.`)
	drv.WaitText("Always allow this session")
	drv.WaitFor(func() bool { return bell.count() > 0 },
		tuitest.Awaiting("the approval-requested webhook to arrive"))

	if _, _, ok := drv.Frame().Find("Always allow this session"); !ok {
		t.Fatal("the approval pane was gone by the time the webhook arrived; the claim that the " +
			"Reaction fired while the human was still being waited on is untestable")
	}
	waiting := bell.first()
	if waiting.payload.Event != reactions.ApprovalRequested {
		t.Errorf("the webhook received the %q event; want %q",
			waiting.payload.Event, reactions.ApprovalRequested)
	}
	if waiting.payload.Tool != "write_file" {
		t.Errorf("the approval-requested payload names the tool %q; want write_file",
			waiting.payload.Tool)
	}
	if waiting.payload.Reaction != "bell" {
		t.Errorf("the approval-requested payload names the reaction %q; want bell",
			waiting.payload.Reaction)
	}
	if waiting.token != hookTokenValue {
		t.Errorf("the webhook's %s header = %q; want the value headers-env named in the "+
			"environment", hookTokenHeader, waiting.token)
	}

	// The approval is answered, the write lands, and the command Reaction receives the two events the
	// journey produced.
	drv.WaitQuiet(settled)
	drv.Type("a")
	drv.WaitText(smokeWriteReply)

	wantPath := filepath.Join(sess.Workspace(), "a.txt")
	drv.WaitFor(func() bool {
		fired := readHookPayloads(t, sink)
		changed, ok := hookPayloadFor(fired, reactions.FileChanged)
		if !ok || changed.Path != wantPath {
			return false
		}
		_, ok = hookPayloadFor(fired, reactions.ExchangeFinished)
		return ok
	}, tuitest.Awaiting("the file-changed and exchange-finished payloads in the reaction sink"))

	fired := readHookPayloads(t, sink)
	changed, _ := hookPayloadFor(fired, reactions.FileChanged)
	if changed.Tool != "write_file" {
		t.Errorf("the file-changed payload names the tool %q; want write_file", changed.Tool)
	}
	if changed.Workspace != sess.Workspace() {
		t.Errorf("the file-changed payload's workspace = %q; want the run's own %q",
			changed.Workspace, sess.Workspace())
	}
	if changed.Schedule != nil {
		t.Errorf("a session's payload carries the schedule %+v; a TUI session belongs to none",
			changed.Schedule)
	}

	// And the environment the command actually ran with, read by the exact names the executor sets:
	// the script echoed $APOGEE_REACTION_EVENT and $APOGEE_REACTION_PATH, so the line it wrote is
	// what those two variables held when the file-changed firing ran.
	wantLine := string(reactions.FileChanged) + " " + wantPath
	envLines, err := os.ReadFile(envSink)
	if err != nil {
		t.Fatalf("read the reaction environment sink: %v", err)
	}
	if !strings.Contains(string(envLines), wantLine) {
		t.Errorf("the fired command's environment sink holds\n%s\nwant a line %q — the executor "+
			"sets APOGEE_REACTION_EVENT and APOGEE_REACTION_PATH under exactly those names",
			envLines, wantLine)
	}

	// Nothing a Reaction did reached the screen. The positive control comes first: an empty or
	// scrolled-away frame would satisfy every absence check below without proving anything, so the
	// frame must first be shown to carry the reply the successful run put there.
	drv.WaitQuiet(settled)
	final := drv.Frame().String()
	if !strings.Contains(final, smokeWriteReply) {
		t.Fatalf("the final frame does not carry the run's own reply %q, so its silence about "+
			"Reactions proves nothing:\n%s", smokeWriteReply, final)
	}
	if report := hookReportPattern(hooksSinkName, hooksBellName).FindString(final); report != "" {
		t.Errorf("the final frame carries the reaction report %q:\n%s", report, final)
	}
	for _, marker := range hookMarkers {
		if strings.Contains(final, marker) {
			t.Errorf("the final frame carries the reaction marker %q:\n%s", marker, final)
		}
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EHooksReportAFailureAsAnEphemeralNote is the other half of ADR 0073 §8: a Reaction that
// refuses to run is the human's business and nobody else's. The run's config is rewritten on disk
// to a Reaction that exits 1, the watcher applies it, and the next exchange fires it — the failure
// lands as ONE transcript note and is absent from the record the session saved.
func TestE2EHooksReportAFailureAsAnEphemeralNote(t *testing.T) {
	script, err := stubllm.Load("testdata/stubllm/smoke.yaml")
	if err != nil {
		t.Fatalf("load the smoke script: %v", err)
	}
	stub := stubllm.New(t, script)
	drv := tuitest.NewDriver(t, e2eSize)
	// The run starts with a Reaction that never fires, so the rewrite below is a CHANGE to the
	// `reactions:` key rather than its first appearance — which is what the watcher's applied-keys
	// note names.
	sess := launchTUIConfigured(t, drv, stub, hookBlockOf(
		"  - id: quiet\n    on: [error]\n    run: [sh, -c, 'exit 0']\n"))

	rewriteHomeHooks(t, sess.Home(),
		"  - id: failing\n    on: [exchange-finished]\n    run: [sh, -c, 'exit 1']\n")
	drv.WaitText(appliedNote)
	drv.WaitQuiet(settled)
	if note := rowContaining(t, drv.Frame(), appliedNote); !strings.Contains(note, "reactions") {
		t.Fatalf("the applied-keys note %q does not name the reactions key", note)
	}

	submit(drv, "What files are in this workspace?")
	drv.WaitText("The workspace holds one file")

	const failureLine = "reaction failing"
	drv.WaitText(failureLine)
	drv.WaitQuiet(settled)
	notice := drv.Frame()
	if n := rowsContaining(notice, failureLine); n != 1 {
		t.Errorf("a failing reaction left %d notice lines; want exactly one:\n%s", n, notice)
	}
	if row := rowContaining(t, notice, failureLine); !strings.Contains(row, "exit 1") {
		t.Errorf("the reaction notice %q does not say the command exited 1", row)
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
	for _, saved := range sess.sessionRecords() {
		body, err := os.ReadFile(filepath.Join(sess.Home(), "sessions", saved.Name()))
		if err != nil {
			t.Fatalf("read the session record %s: %v", saved.Name(), err)
		}
		if strings.Contains(string(body), failureLine) {
			t.Errorf("the session record %s kept a reaction notice; nothing a Reaction does may reach it",
				saved.Name())
		}
	}
}

// The unattended halves' conversation, restated from testdata/stubllm/hooks.yaml so an assertion
// reads as the claim it makes. The answer shares no word with a Reaction's own vocabulary — neither
// a [hookMarkers] spelling nor a [hookReportPattern] shape — so finding it on stdout is finding the
// model's reply and nothing else, which is what lets it serve as the absence check's positive
// control.
const (
	hooksPrompt = "Say what both roots do."
	hooksAnswer = "Both roots fired their hooks."

	// The schedule the daemon half puts on the clock, and the Reactions the cases subscribe: one that
	// records every payload it is handed, one that refuses to run at all, and the webhook the
	// driven half rings. Every absence check derives its [hookReportPattern] from these same
	// names, so renaming a Reaction here re-aims the assertion rather than un-arming it.
	hooksScheduleName = "hook-probe"
	hooksSinkName     = "sink"
	hooksFailingName  = "noisy"
	hooksBellName     = "bell"
)

// TestE2EHooksFireFromAHeadlessRun is the unattended half: no screen, no human, one prompt, and the
// `exchange-finished` Reaction still fires — with a payload that names the workspace the run was
// rooted in and NO schedule, because a plain headless run belongs to none.
//
// The stdout claim rides along because this is the only Driver that has one, and because it is the
// contract a script piping apogee depends on (TestHeadlessAnswerLandsOnTheProcessStdout): a
// Reaction writes to its own sink and to nowhere else, so the answer stream is the answer alone
// even while a Reaction is firing off it.
func TestE2EHooksFireFromAHeadlessRun(t *testing.T) {
	sink := filepath.Join(t.TempDir(), "fired.jsonl")
	t.Setenv(hookSinkEnv, sink)

	stub := stubllm.New(t, loadScript(t, "hooks"))
	stdout, stderr, workspace := headlessHooksAgainst(t, stub, hooksPrompt, hookBlockOf(
		"  - id: "+hooksSinkName+"\n"+
			"    on: [exchange-finished]\n"+
			"    run: [sh, -c, 'cat >> \"$APOGEE_TEST_SINK\"']\n"))

	if got := strings.TrimSpace(stdout); got != hooksAnswer {
		t.Errorf("stdout = %q; want the answer alone (%q)", stdout, hooksAnswer)
	}

	// The run is over, and a headless run drains its Reactions before it returns, so the sink is
	// final: what is in it now is everything that ever fired.
	fired := readHookPayloads(t, sink)
	if len(fired) != 1 {
		t.Fatalf("the reaction sink holds %d payloads, want exactly one for the run's one exchange:\n%+v",
			len(fired), fired)
	}
	payload := fired[0]
	if payload.Event != reactions.ExchangeFinished {
		t.Errorf("the payload carries the %q event; want %q", payload.Event, reactions.ExchangeFinished)
	}
	if payload.Reaction != hooksSinkName {
		t.Errorf("the payload names the reaction %q; want %q", payload.Reaction, hooksSinkName)
	}
	if payload.Workspace != workspace {
		t.Errorf("the payload's workspace = %q; want the run's own %q", payload.Workspace, workspace)
	}
	if payload.Schedule != nil {
		t.Errorf("the payload carries the schedule %+v; a plain headless run belongs to none",
			payload.Schedule)
	}

	// Nothing a Reaction did reached either stream. Reactions that succeed are silent, and the one
	// that fired here did. The positive control comes first, for the reason the driven half has one: a
	// run that never produced any output at all would pass every absence check below.
	if !strings.Contains(stdout, hooksAnswer) {
		t.Fatalf("stdout does not carry the run's own answer %q, so its silence about Reactions "+
			"proves nothing:\n%s", hooksAnswer, stdout)
	}
	reports := hookReportPattern(hooksSinkName)
	if report := reports.FindString(stdout); report != "" {
		t.Errorf("stdout carries the reaction report %q:\n%s", report, stdout)
	}
	if report := reports.FindString(stderr); report != "" {
		t.Errorf("stderr carries the reaction report %q:\n%s", report, stderr)
	}
	for _, marker := range hookMarkers {
		if strings.Contains(stdout, marker) {
			t.Errorf("stdout carries the reaction marker %q:\n%s", marker, stdout)
		}
		if strings.Contains(stderr, marker) {
			t.Errorf("stderr carries the reaction marker %q:\n%s", marker, stderr)
		}
	}
}

// TestDaemonFiringFiresHooks is the third root: one `apogee daemon` Firing, whose `turn-finished`
// payload names the Schedule it ran for — the fact no other Driver's payload carries, and the whole
// reason the Runner is built per Firing rather than per daemon (ADR 0073 §9).
//
// The second Reaction refuses to run, because a daemon has no transcript to note a failure in and
// its log IS its user interface (ADR 0034 decision 10). One line, once: a Reaction that fails every
// Firing for a week must not fill a journal.
//
// It runs the REAL runner rather than the harness's stub, for the reason
// TestDaemonFaultedVerbColumn runs it: the events a Reaction fires off are the ENGINE's, and a
// stubbed runner emits none of them.
func TestDaemonFiringFiresHooks(t *testing.T) {
	sink := filepath.Join(t.TempDir(), "fired.jsonl")
	t.Setenv(hookSinkEnv, sink)

	stub := stubllm.New(t, loadScript(t, "hooks"))
	h := newDaemonHarness(t)
	// The harness installs a stub runner; this test wants the composition — and restores what it
	// found, so the swap cannot outlive the case under `go test -shuffle`.
	prev := runOnce
	runOnce = run.Once
	t.Cleanup(func() { runOnce = prev })
	writeConfigHome(t, h.home, hookBlockOf(
		"  - id: "+hooksSinkName+"\n"+
			"    on: [turn-finished]\n"+
			"    run: [sh, -c, 'cat >> \"$APOGEE_TEST_SINK\"']\n"+
			"  - id: "+hooksFailingName+"\n"+
			"    on: [turn-finished]\n"+
			"    run: [sh, -c, 'exit 1']\n")+
		"servers:\n"+
		"  - name: stub\n    endpoint: "+stub.URL+"\n    model: "+stub.Model+"\nserver: stub\n")
	ws := t.TempDir()
	h.writeSchedules(t, "schedules:\n"+
		"  - name: "+hooksScheduleName+"\n"+
		"    on:\n      cycle: 30s\n"+
		"    run:\n      prompt: "+hooksPrompt+"\n      workspace: "+ws+"\n      mode: plan\n")

	wait := h.run(t)
	h.awaitLog(t, "1 schedule on the clock")
	h.clock.tick()

	// The failing Reaction's line, in the shape the Runner reports and the log's own sanitiser passed
	// through: the Reaction's name, the event it was fired by, and why it failed.
	h.awaitLog(t, "reaction "+hooksFailingName+" (turn-finished): exit 1")
	// And the Firing landing, which is what makes the sink below final: a daemon drains a Firing's
	// Reactions before the Outcome that prints this line is returned.
	h.awaitLog(t, "completed "+hooksScheduleName)

	h.stop()
	if err := wait(); err != nil {
		t.Fatalf("the daemon returned %v; a fired Reaction is not a daemon failure\n%s",
			err, h.errOut.String())
	}

	fired := readHookPayloads(t, sink)
	payload, ok := hookPayloadFor(fired, reactions.TurnFinished)
	if !ok {
		t.Fatalf("the reaction sink holds no turn-finished payload; it holds:\n%+v", fired)
	}
	if payload.Schedule == nil {
		t.Fatalf("a Firing's payload carries no schedule; it must name the one it ran for:\n%+v", payload)
	}
	if payload.Schedule.Name != hooksScheduleName {
		t.Errorf("the payload names the schedule %q; want %q", payload.Schedule.Name, hooksScheduleName)
	}
	// The id is minted by the Scheduler (schedule.mintID) and so is not knowable from the file the
	// test wrote — what is knowable is that the payload carries the real one rather than an empty
	// field or the name a second time.
	if !strings.HasPrefix(payload.Schedule.ID, "sch-") || len(payload.Schedule.ID) <= len("sch-1-") {
		t.Errorf("the payload's schedule id = %q; want the Scheduler's own sch-N-<hex>",
			payload.Schedule.ID)
	}

	// Once, and only once: the de-dup is what keeps a permanently broken Reaction from filling a
	// journal a supervisor reads for a week.
	if n := strings.Count(h.out.String(), "reaction "+hooksFailingName+" ("); n != 1 {
		t.Errorf("the failing reaction left %d lines in the daemon log, want exactly one:\n%s",
			n, h.out.String())
	}
}

// ----------------------------------------------------------------------------
// The stage-2 journeys (ADR 0076)
// ----------------------------------------------------------------------------

// TestE2EReactionsMigrateAHooksFileAtStartup is the migration journey: a home whose config.yaml is
// still written in the retired schema — a `reactions:` block naming the retired approval event, the
// `mechanisms:` key both top-level and on the server entry, and `validated-sets:` — booted at the
// TUI root, which is where the fold runs and the only place it ever does.
//
// It is the claim the unit tests in internal/config cannot make: that a user who upgrades and
// starts apogee gets a file that has already been rewritten, a copy of what they had, a line saying
// so, and — the part a fold that merely parsed would miss — an entry that still FIRES, off the
// block apogee wrote for them rather than the one they did.
func TestE2EReactionsMigrateAHooksFileAtStartup(t *testing.T) {
	sink := filepath.Join(t.TempDir(), "fired.jsonl")
	t.Setenv(hookSinkEnv, sink)

	script, err := stubllm.Load("testdata/stubllm/smoke.yaml")
	if err != nil {
		t.Fatalf("load the smoke script: %v", err)
	}
	stub := stubllm.New(t, script)
	drv := tuitest.NewDriver(t, e2eSize)
	home := legacyReactionsHome(t, stub)
	sess := launchTUIOn(t, drv, stub, home, "")

	// The folded entry fires, which is what makes the rewritten block a working one.
	submit(drv, "What files are in this workspace?")
	drv.WaitText("The workspace holds one file")
	drv.WaitFor(func() bool {
		_, ok := hookPayloadFor(readHookPayloads(t, sink), reactions.ExchangeFinished)
		return ok
	}, tuitest.Awaiting("the migrated reaction to fire on exchange-finished"))
	if payload, _ := hookPayloadFor(readHookPayloads(t, sink), reactions.ExchangeFinished); payload.Reaction != hooksSinkName {
		t.Errorf("the exchange-finished payload names the reaction %q; want the folded entry %q",
			payload.Reaction, hooksSinkName)
	}

	// Quit first: the startup notice is written to the command tree's own error stream from the
	// run's goroutine, so it is only safely readable once that goroutine has returned.
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}

	path := filepath.Join(home, "config.yaml")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the migrated config: %v", err)
	}
	if !strings.Contains(string(body), migratedReactionsBlock) {
		t.Errorf("the migrated config does not carry the folded block\n%s\nwant it to contain\n%s",
			body, migratedReactionsBlock)
	}
	for _, retired := range []string{"hooks:", "mechanisms:", "validated-sets:", retiredApprovalSpelling} {
		if strings.Contains(string(body), retired) {
			t.Errorf("the migrated config still carries %q:\n%s", retired, body)
		}
	}

	backup := soleConfigBackup(t, home)
	kept, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("read the backup %s: %v", backup, err)
	}
	if !strings.Contains(string(kept), "hooks:") {
		t.Errorf("the backup %s does not hold the file as it was:\n%s", backup, kept)
	}

	// The note, whole: the user's only warning that a file they own was rewritten, and the only
	// place the two facts a script author needs — the environment prefix and the payload's key —
	// are said out loud.
	wantNote := "apogee: rewrote " + path + " — " +
		"hooks: became reactions: (2 entries); " +
		retiredApprovalSpelling + " is now approval-requested; " +
		"the retired mechanisms: key was dropped (cached_content_intercept → read-cache:, " +
		"tool_use_enforcer → tool-use-enforcer:); " +
		"the inert validated-sets: key was dropped; " +
		"comments inside the old block did not survive; backup at " + backup + "." +
		` Scripts must read APOGEE_REACTION_* (was APOGEE_HOOK_*) and the payload's ` +
		`"reaction" field (was "hook").`
	if !strings.Contains(sess.Output(), wantNote) {
		t.Errorf("the run's notices do not carry the migration note\n%s\nwant\n%s",
			sess.Output(), wantNote)
	}
}

// TestE2EReactionsApprovalDecidedCarriesTheVerdict is the second half of the approval pair (ADR
// 0076 A6): `approval-requested` says a human is being waited on, `approval-decided` says what they
// answered. Only the driven root can make the claim — a decision needs somebody to make it — and
// the payload's `decision` is the fact no other notice carries.
func TestE2EReactionsApprovalDecidedCarriesTheVerdict(t *testing.T) {
	sink := filepath.Join(t.TempDir(), "fired.jsonl")
	t.Setenv(hookSinkEnv, sink)

	script, err := stubllm.Load("testdata/stubllm/smoke.yaml")
	if err != nil {
		t.Fatalf("load the smoke script: %v", err)
	}
	stub := stubllm.New(t, script)
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIConfigured(t, drv, stub, hookBlockOf(
		"  - id: "+hooksSinkName+"\n"+
			"    on: [approval-decided]\n"+
			"    run: [sh, -c, 'cat >> \"$APOGEE_TEST_SINK\"']\n"))

	submit(drv, `Append a line saying "smoke test" to a.txt.`)
	drv.WaitText("Always allow this session")
	drv.WaitQuiet(settled)
	drv.Type("a")
	drv.WaitText(smokeWriteReply)

	drv.WaitFor(func() bool {
		_, ok := hookPayloadFor(readHookPayloads(t, sink), reactions.ApprovalDecided)
		return ok
	}, tuitest.Awaiting("the approval-decided payload in the reaction sink"))

	decided, _ := hookPayloadFor(readHookPayloads(t, sink), reactions.ApprovalDecided)
	if decided.Decision != string(domain.ApprovalAllow) {
		t.Errorf("the approval-decided payload's decision = %q; want %q — the human pressed Allow",
			decided.Decision, domain.ApprovalAllow)
	}
	if decided.Tool != "write_file" {
		t.Errorf("the approval-decided payload names the tool %q; want write_file", decided.Tool)
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EReactionsPostToolResultFinishedCarriesTheResult is a seam-closing notice at the headless
// root: `post-tool-result-finished` reports that the post-tool-result seam finished passing, and
// its payload carries the working value that came out of it — the call and the text the model is
// about to read.
//
// The claim is made on the script's STDIN rather than on a field of the run: what a command entry
// receives is the payload as JSON on its standard input, so a projection that never reached the
// wire would look identical from inside the Runner and be worth nothing here.
func TestE2EReactionsPostToolResultFinishedCarriesTheResult(t *testing.T) {
	sink := filepath.Join(t.TempDir(), "fired.jsonl")
	t.Setenv(hookSinkEnv, sink)

	stub := stubllm.New(t, loadScript(t, "reactions"))
	headlessHooksAgainst(t, stub, "What files are in this workspace?", hookBlockOf(
		"  - id: "+hooksSinkName+"\n"+
			"    on: ["+string(domain.MomentPostToolResultFinished)+"]\n"+
			"    run: [sh, -c, 'cat >> \"$APOGEE_TEST_SINK\"']\n"))

	fired := readHookPayloads(t, sink)
	closed, ok := hookPayloadFor(fired, domain.MomentPostToolResultFinished)
	if !ok {
		t.Fatalf("the reaction sink holds no post-tool-result-finished payload; it holds:\n%+v", fired)
	}
	if closed.Seam != domain.MomentPostToolResult {
		t.Errorf("the payload names the seam %q; want %q", closed.Seam, domain.MomentPostToolResult)
	}

	value := seamValue(t, closed)
	call, _ := value["call"].(map[string]any)
	if name, _ := call["name"].(string); name != "list_dir" {
		t.Errorf("the value's call names the tool %q; want list_dir — the one the script called", name)
	}
	content, _ := value["content"].(string)
	if !strings.Contains(content, "a.txt") {
		t.Errorf("the value's content = %q; want the tool result the seeded workspace produces, "+
			"which names a.txt", content)
	}
}

// TestE2EReactionsPreRequestFinishedCarriesThePrompt is the seam at the other end of the loop:
// `pre-request-finished` closes over the request as the pre-request cascade left it, so its value
// is what apogee is about to SEND — the user's own words among it.
//
// One prompt, one Turn, no tool call: the run makes exactly one request, so the single payload in
// the sink is that request and there is nothing to disambiguate.
func TestE2EReactionsPreRequestFinishedCarriesThePrompt(t *testing.T) {
	sink := filepath.Join(t.TempDir(), "fired.jsonl")
	t.Setenv(hookSinkEnv, sink)

	stub := stubllm.New(t, loadScript(t, "hooks"))
	headlessHooksAgainst(t, stub, hooksPrompt, hookBlockOf(
		"  - id: "+hooksSinkName+"\n"+
			"    on: ["+string(domain.MomentPreRequestFinished)+"]\n"+
			"    run: [sh, -c, 'cat >> \"$APOGEE_TEST_SINK\"']\n"))

	fired := readHookPayloads(t, sink)
	closed, ok := hookPayloadFor(fired, domain.MomentPreRequestFinished)
	if !ok {
		t.Fatalf("the reaction sink holds no pre-request-finished payload; it holds:\n%+v", fired)
	}
	if closed.Seam != domain.MomentPreRequest {
		t.Errorf("the payload names the seam %q; want %q", closed.Seam, domain.MomentPreRequest)
	}

	messages, _ := seamValue(t, closed)["messages"].([]any)
	if !messagesCarry(messages, hooksPrompt) {
		t.Errorf("the value's messages do not carry the prompt %q:\n%+v", hooksPrompt, messages)
	}
}

// TestE2EReactionsReloadSwapsTheArmedList is the live-reload journey: the `reactions:` key is
// rewritten under a running session, the watcher applies it, and the list that fires from then on
// is the NEW one — one generation, swapped whole, rather than a list the old entries linger in.
//
// The claim is made on what fires rather than on how many times the engine was told, because the
// swap itself is invisible from out here: the count belongs to the unit spy over SetReactions. What
// a user can see is that their edit took, and that the entry they deleted stopped firing.
func TestE2EReactionsReloadSwapsTheArmedList(t *testing.T) {
	sink := filepath.Join(t.TempDir(), "fired.jsonl")
	t.Setenv(hookSinkEnv, sink)

	const (
		before = "before"
		after  = "after"
	)
	script, err := stubllm.Load("testdata/stubllm/smoke.yaml")
	if err != nil {
		t.Fatalf("load the smoke script: %v", err)
	}
	stub := stubllm.New(t, script)
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIConfigured(t, drv, stub, hookBlockOf(sinkEntry(before)))

	submit(drv, "What files are in this workspace?")
	drv.WaitText("The workspace holds one file")
	drv.WaitFor(func() bool { return len(readHookPayloads(t, sink)) == 1 },
		tuitest.Awaiting("the first generation's entry to fire"))

	rewriteHomeHooks(t, sess.Home(), sinkEntry(after))
	drv.WaitText(appliedNote)
	drv.WaitQuiet(settled)

	submit(drv, "Is there anything else worth knowing?")
	drv.WaitText("Nothing else")
	drv.WaitFor(func() bool { return len(readHookPayloads(t, sink)) == 2 },
		tuitest.Awaiting("the second generation's entry to fire"))
	drv.WaitQuiet(settled)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}

	// Two exchanges, two firings, one from each generation and in that order: a third payload
	// would be the deleted entry still armed, and a second `before` would be it firing again.
	fired := readHookPayloads(t, sink)
	names := make([]string, 0, len(fired))
	for _, payload := range fired {
		names = append(names, payload.Reaction)
	}
	if want := []string{before, after}; !slices.Equal(names, want) {
		t.Errorf("the sink holds the firings %v; want %v — the reload swaps the armed list rather "+
			"than adding to it", names, want)
	}
}

// The gate journeys' conversation and vocabulary, restated from testdata/stubllm/reactions.yaml so
// an assertion reads as the claim it makes: one prompt that makes the model call a tool, and the
// reply it gives once that call has been answered — refused or not, because the script matches on
// the tool's NAME and a refusal is still that tool's result.
//
// The reaction is named once here and read everywhere, engine-authored sentences included: a gate's
// model-facing refusal names the reaction and nothing else (internal/agent/gate.go:38), and its
// human-facing question names the reaction and then whatever the script said (:310) — so renaming
// the entry re-aims every assertion below rather than un-arming one.
const (
	gatePrompt = "What files are in this workspace?"
	gateAnswer = "Both roots fired their reactions."

	gateWardenName = "warden"
	gateAskReason  = "looks risky"

	// What the model reads when the gate says no, and what the HUMAN reads when it asks. The two
	// are deliberately different sentences: a gate's reason is written for the person being asked
	// and never reaches the conversation, which is the whole of why a gate cannot smuggle
	// instructions into the model through a refusal it manufactured.
	gateDenial   = "tool call denied by reaction " + gateWardenName
	gateQuestion = "reaction " + gateWardenName + " asks: " + gateAskReason

	// The unattended denier's own refusal (internal/run/run.go:291). An `ask` that reached a
	// headless run ends here rather than at finishGate's no-Approver sentence: an unattended root
	// installs a denying Approver, so the Approver is consulted and says no.
	gateApproverDenial = "tool call denied by approver"
)

// TestE2EGateDeniesAHeadlessToolCall is the gate cell's first journey: a `gate:` entry in the
// user's own config.yaml refuses a call the mode ladder had already allowed, and the model reads
// the engine's refusal where the tool's output would have been.
//
// The claim is made on what the STUB received rather than on anything apogee printed. A refusal
// that never reached the conversation would look identical from inside the Agent, and the tool
// message on the next request is the only place the model's own view of the call is visible from
// out here.
func TestE2EGateDeniesAHeadlessToolCall(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "reactions"))
	stdout, _, _ := headlessHooksArgs(t, stub, gatePrompt, gateConfig("echo deny"),
		"--format", formatJSON)

	if got := gateToolResult(t, stub); got != gateDenial {
		t.Errorf("the model was handed the tool result %q; want the gate's refusal %q",
			got, gateDenial)
	}

	lines := jsonEventLines(t, stdout)
	fired := gateFirings(t, lines)
	if want := []string{"deny"}; !slices.Equal(fired, want) {
		t.Errorf("the stream booked the gate firings %v; want %v — one firing, naming what the "+
			"gate decided", fired, want)
	}
}

// TestE2EGateAsksInAHeadlessRun is the same entry saying `ask` where nobody is watching. An `ask`
// is not a verdict of its own: it hands the call to the human whatever the ladder said, and an
// unattended run's human is the denier that refuses every gate without parking. So the firing books
// `ask`, the approval that follows books `deny`, and the run goes on to its answer rather than
// dying — a Plan run that reached for something and was told no did its job and says so in the
// summary.
func TestE2EGateAsksInAHeadlessRun(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "reactions"))
	stdout, stderr, _ := headlessHooksArgs(t, stub, gatePrompt, gateConfig("echo ask"),
		"--format", formatJSON)

	if got := gateToolResult(t, stub); got != gateApproverDenial {
		t.Errorf("the model was handed the tool result %q; want the unattended denier's refusal %q "+
			"— an ask is answered by the Approver, not by the gate", got, gateApproverDenial)
	}

	// The order is the claim: the gate asked, and only then was the human's stand-in consulted.
	// A `deny` decision that preceded the firing would be some other gate closing the call.
	lines := jsonEventLines(t, stdout)
	asked := gateFirstIndex(t, lines, "reaction_fired", "action", "ask")
	decided := gateFirstIndex(t, lines, "approval", "decision", string(domain.ApprovalDeny))
	if asked < 0 {
		t.Fatalf("the stream carries no reaction_fired line booking an ask:\n%s", stdout)
	}
	if decided < 0 {
		t.Fatalf("the stream carries no approval line deciding deny:\n%s", stdout)
	}
	if decided < asked {
		t.Errorf("the deny approval is line %d and the gate's ask is line %d; the approval must "+
			"follow the ask that forced it", decided+1, asked+1)
	}

	// The run reached its own end: the answer the script gives once the call has been answered,
	// the closing frame's success, and the one line a script greps counting the refusal.
	_, data := finishedFrame(t, lines)
	wantExitCode(t, data, 0)
	if got, _ := data["final_text"].(string); !strings.Contains(got, gateAnswer) {
		t.Errorf("the closing frame's final_text = %q; want the reply %q the run continued to",
			got, gateAnswer)
	}
	if !strings.Contains(stderr, "denied: 1") {
		t.Errorf("the run's summary does not count the refusal:\n%s", stderr)
	}
}

// TestE2EGateAsksInTheTUI is the third root, and the only one with a person in it: the same `ask`
// in front of a human puts the gate's own words on the approval pane. The second and later lines of
// a gate's stdout are a reason for the human and for nobody else, so this frame is the one place in
// the whole surface where they are visible — which is why the journey is driven rather than
// asserted on an ApprovalRequest inside the Agent.
//
// The mode is named rather than left to the default: `list_dir` is a read, and a read is allowed
// without asking in every mode on the ladder, so the pane in front of the human here exists because
// a gate asked for it and for no other reason.
func TestE2EGateAsksInTheTUI(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "reactions"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIConfigured(t, drv, stub, gateConfig(`printf 'ask\nlooks risky\n'`),
		"--mode", string(domain.ModeAskBefore))

	submit(drv, gatePrompt)
	drv.WaitText(gateQuestion)

	if _, _, ok := drv.Frame().Find("Always allow this session"); !ok {
		t.Fatalf("the gate's words are on screen but the approval menu is not, so the frame "+
			"carrying them is not the approval pane:\n%s", drv.Frame().String())
	}

	// Answered, so the session ends the way every other driven case does rather than being quit
	// out from under a pending question.
	drv.WaitQuiet(settled)
	drv.Type("a")
	drv.WaitText(gateAnswer)
	drv.WaitQuiet(settled)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// gateConfig is the config a gate journey runs with: the `warden` entry armed at `pre-tool-exec`
// with script as its gate command, above a `confine-to-workspace: false`.
//
// The confinement key is not incidental. A user-origin sync reaction spawns under a
// SubprocessPermit whose box is the workspace when the fence is ON, and a host whose kernel offers
// no confinement capabilities then refuses the permit outright — the gate would fail and every
// journey below would read an `ask` it never scripted. Off, the permit carries no box and the
// handler spawns on any host; the fenced case is internal/agent's own unit test.
//
// script is written into a YAML flow scalar, so its single quotes are doubled here rather than in
// every caller — a gate's protocol puts its reason on the second line, and `printf` is how a one
// line `sh -c` says that.
func gateConfig(script string) string {
	return "confine-to-workspace: false\n" +
		hookBlockOf("  - id: "+gateWardenName+"\n"+
			"    on: ["+string(domain.MomentPreToolExec)+"]\n"+
			"    gate: [sh, -c, '"+strings.ReplaceAll(script, "'", "''")+"']\n")
}

// gateToolResult is the tool message the stub was handed on the request that FOLLOWED the gated
// call: the model's own view of what happened to it. The script's second Turn matches on the tool's
// NAME, so a refused call still reaches it — which is what makes the run continue far enough for
// this message to be sent at all.
func gateToolResult(t *testing.T, stub *stubllm.Server) string {
	t.Helper()

	reqs := stub.Requests()
	if len(reqs) < 2 {
		t.Fatalf("the stub answered %d requests; want the tool Turn and the one carrying its "+
			"result", len(reqs))
	}
	messages := reqs[1].Messages
	last := messages[len(messages)-1]
	if last.ToolCallID == "" {
		t.Fatalf("the second request's last message is a %q message rather than a tool result: %+v",
			last.Role, last)
	}
	return last.Content
}

// gateFirings is the action every reaction_fired line booked, in stream order. The whole line is
// reduced to its action because that is the gate's answer: which verdict the fold read off the
// script's stdout.
func gateFirings(t *testing.T, lines []map[string]any) []string {
	t.Helper()

	var actions []string
	for i, line := range lines {
		if line["event"] == "reaction_fired" {
			if id := stringMember(t, i, line, "reaction"); id != gateWardenName {
				t.Fatalf("line %d books a firing for %q; the run armed only %q",
					i+1, id, gateWardenName)
			}
			actions = append(actions, stringMember(t, i, line, "action"))
		}
	}
	return actions
}

// gateFirstIndex is the position of the first line of kind whose data member holds want, or -1.
// The POSITION rather than the line, because what the asking journey claims is an order.
func gateFirstIndex(t *testing.T, lines []map[string]any, kind, member, want string) int {
	t.Helper()

	for i, line := range lines {
		if line["event"] == kind && stringMember(t, i, line, member) == want {
			return i
		}
	}
	return -1
}

// sinkEntry is one `reactions:` entry, named, that appends every payload it is handed to the shared
// sink. The NAME is what the reload journey reads back: two entries writing to one file are told
// apart by the payload's own `reaction` field, which is the field a firing is attributed by.
func sinkEntry(id string) string {
	return "  - id: " + id + "\n" +
		"    on: [exchange-finished]\n" +
		"    run: [sh, -c, 'cat >> \"$APOGEE_TEST_SINK\"']\n"
}

// retiredApprovalSpelling is the event name the retired `hooks:` schema used for the approval
// notice, which the fold rewrites. It is assembled rather than written whole so the acceptance
// sweep that forbids the literal in Go source stays armed.
const retiredApprovalSpelling = "approval-" + "waiting"

// migratedReactionsBlock is the block the fold writes over the `reactions:` one in
// [legacyReactionsHome] — byte for byte, because "what did apogee put in my file" is a question a
// user asks of the file and not of a parser. Both entry shapes are here: the argv list on one line,
// and the webhook as the mapping the schema documents.
const migratedReactionsBlock = "reactions:\n" +
	"  - id: " + hooksSinkName + "\n" +
	"    on: [exchange-finished]\n" +
	"    run: [sh, -c, cat >> \"$APOGEE_TEST_SINK\"]\n" +
	"  - id: " + hooksBellName + "\n" +
	"    on: [approval-requested]\n" +
	"    run:\n" +
	"      url: https://example.invalid/apogee\n"

// legacyReactionsHome writes an apogee home in the RETIRED schema: a `reactions:` block carrying
// both entry shapes and the retired approval spelling, the `mechanisms:` key top-level and on the
// server entry, and `validated-sets:`. It is written by hand rather than through [writeConfigHome]
// because a per-server key sits inside a list item, which no helper can reach after the fact.
func legacyReactionsHome(t *testing.T, stub *stubllm.Server) string {
	t.Helper()

	home := t.TempDir()
	body := "servers:\n" +
		"  - name: probe-target\n" +
		"    endpoint: " + stub.URL + "\n" +
		"    model: " + stub.Model + "\n" +
		"    mechanisms:\n" +
		"      tool_use_enforcer: false\n" +
		"server: probe-target\n" +
		"mechanisms:\n" +
		"  cached_content_intercept: false\n" +
		"validated-sets:\n" +
		"  " + stub.Model + ": [list_dir]\n" +
		"hooks:\n" +
		"  - name: " + hooksSinkName + "\n" +
		"    events: [exchange-finished]\n" +
		"    command: [sh, -c, 'cat >> \"$APOGEE_TEST_SINK\"']\n" +
		"  - name: " + hooksBellName + "\n" +
		"    events: [" + retiredApprovalSpelling + "]\n" +
		"    webhook: https://example.invalid/apogee\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write the legacy home's config: %v", err)
	}
	return home
}

// soleConfigBackup is the one `.bak-` sibling the migration left beside the home's config, and
// fails when there is any other number of them: the note names exactly one path, and a second
// backup would mean the fold ran twice.
func soleConfigBackup(t *testing.T, home string) string {
	t.Helper()

	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("read the migrated home: %v", err)
	}
	var found []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "config.yaml.bak-") {
			found = append(found, filepath.Join(home, entry.Name()))
		}
	}
	if len(found) != 1 {
		t.Fatalf("the home holds %d config backups (%v); the migration writes exactly one",
			len(found), found)
	}
	return found[0]
}

// seamValue is a seam-closing payload's `value` as it comes back off the wire: the projection is
// built as `any` and read here through a JSON decode, so it arrives as the object a script's own
// `jq` would see rather than as the Go type that produced it.
func seamValue(t *testing.T, payload reactions.Payload) map[string]any {
	t.Helper()

	value, ok := payload.Value.(map[string]any)
	if !ok {
		t.Fatalf("the %s payload carries no value object: %#v", payload.Event, payload.Value)
	}
	return value
}

// messagesCarry reports whether any projected message's content holds want.
func messagesCarry(messages []any, want string) bool {
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if content, _ := message["content"].(string); strings.Contains(content, want) {
			return true
		}
	}
	return false
}

// headlessHooksAgainst runs one real `apogee headless` against stub in a home of its own and hands
// back the three things this file has to read: what the answer stream carried, what the run
// narrated, and the workspace it was rooted in — the path a payload's `workspace` is asserted
// against. extraConfig is written above the `servers:` block, which is how a case reaches a
// file-only key such as `reactions:`.
//
// It is [headlessAgainst]'s twin rather than a call to it: that one returns stderr ALONE, pins the
// naming journey's own prompt, and wraps the run's Event sink to drive a gate (e2e_naming_test.go)
// — three things this file needs otherwise. Nothing else differs, the runner least of all: it BINDS
// `runOnce` to the production [run.Once] and restores it after, because what a Reaction observes is
// exactly the engine's own event stream and a stubbed runner produces none of it — a claim that has
// to rest on this helper's own code rather than on whatever ran before it.
func headlessHooksAgainst(t *testing.T, stub *stubllm.Server, prompt, extraConfig string) (stdout, stderr, workspace string) {
	t.Helper()
	return headlessHooksArgs(t, stub, prompt, extraConfig)
}

// headlessHooksArgs is [headlessHooksAgainst] with extra command-line arguments, which is the whole
// of what the gate journeys need beyond it: a `reaction_fired` line exists only under
// `--format json` (cmd/apogee/headless.go:390 — text mode prints nothing at all for a
// ReactionFiredEvent, :178), so a case reading the stream has to ask for it, and `--config`,
// `--workspace` and the prompt are still this helper's own.
func headlessHooksArgs(t *testing.T, stub *stubllm.Server, prompt, extraConfig string,
	extra ...string) (stdout, stderr, workspace string) {
	t.Helper()

	prev := runOnce
	runOnce = run.Once
	t.Cleanup(func() { runOnce = prev })

	// The environment must not move the home or the mode out from under the run.
	assertNoAmbientApogeeConfig(t)
	t.Setenv(config.EnvMode, "")

	home := t.TempDir()
	writeConfigHome(t, home, extraConfig+
		"servers:\n"+
		"  - name: stub\n"+
		"    endpoint: "+stub.URL+"\n"+
		"    model: "+stub.Model+"\n"+
		"server: stub\n")

	workspace = e2eWorkspace(t)
	cmd := newHeadlessCommand()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetIn(strings.NewReader(""))
	args := []string{"--config", home, "--workspace", workspace}
	args = append(args, extra...)
	cmd.SetArgs(append(args, prompt))
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("headless: %v\n%s", err, errBuf.String())
	}
	return outBuf.String(), errBuf.String(), workspace
}

// hookBlock is the `reactions:` block the first test runs with: the command Reaction that appends
// every payload it receives to $APOGEE_TEST_SINK and every firing's two headline environment facts
// to $APOGEE_TEST_ENV_SINK, and the webhook Reaction whose only header is read from the environment
// rather than written in the file.
//
// The script names APOGEE_REACTION_EVENT and APOGEE_REACTION_PATH rather than reading the JSON on
// its stdin, because those are the EXACT variable names the executor sets: a rename of either would
// leave the line it writes blank, which is what the assertion in TestE2EHooksFireFromTheTUI reads.
func hookBlock(webhook string) string {
	return hookBlockOf(
		"  - id: " + hooksSinkName + "\n" +
			"    on: [file-changed, exchange-finished]\n" +
			"    run: [sh, -c, 'cat >> \"$APOGEE_TEST_SINK\"; " +
			"echo \"$APOGEE_REACTION_EVENT $APOGEE_REACTION_PATH\" >> \"$APOGEE_TEST_ENV_SINK\"']\n" +
			"  - id: " + hooksBellName + "\n" +
			"    on: [approval-requested]\n" +
			"    run:\n" +
			"      url: " + webhook + "\n" +
			"      headers-env:\n" +
			"        " + hookTokenHeader + ": " + hookTokenEnv + "\n")
}

// hookBlockOf wraps one or more already-indented entries in the `reactions:` key — the one spelling
// the schema has since the migration folded `reactions:` away (ADR 0076 A6).
func hookBlockOf(entries string) string { return "reactions:\n" + entries }

// rewriteHomeHooks replaces a home's whole `reactions:` block with entries, keeping everything the
// config said before it. [appendHomeConfig] cannot do this — a second `reactions:` key is a
// duplicate rather than a change of mind — and the block is the tail of the file by construction, so
// cutting at it leaves the `servers:` the run is talking through untouched.
func rewriteHomeHooks(t *testing.T, home, entries string) {
	t.Helper()

	path := filepath.Join(home, "config.yaml")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the run's config: %v", err)
	}
	head, _, found := strings.Cut(string(body), "reactions:\n")
	if !found {
		t.Fatalf("the run's config carries no reactions: block to rewrite:\n%s", body)
	}
	if err := os.WriteFile(path, []byte(head+hookBlockOf(entries)), 0o600); err != nil {
		t.Fatalf("write the run's config: %v", err)
	}
}

// readHookPayloads reads back what a command Reaction appended to its sink. The payloads are
// concatenated JSON documents with no separator, so they are streamed rather than split; a trailing
// document that is still being written stops the read, which is the ordinary state of a file a
// worker may be appending to at this very moment.
func readHookPayloads(t *testing.T, path string) []reactions.Payload {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read the reaction sink: %v", err)
	}
	var fired []reactions.Payload
	decoder := json.NewDecoder(bytes.NewReader(body))
	for {
		var payload reactions.Payload
		if err := decoder.Decode(&payload); err != nil {
			return fired
		}
		fired = append(fired, payload)
	}
}

// hookPayloadFor answers the first payload carrying event, and whether there was one.
func hookPayloadFor(fired []reactions.Payload, event reactions.Event) (reactions.Payload, bool) {
	for _, payload := range fired {
		if payload.Event == event {
			return payload, true
		}
	}
	return reactions.Payload{}, false
}

// hookRequest is one POST a webhook Reaction made: the payload it carried and the header whose
// value came from the environment rather than the config file.
type hookRequest struct {
	payload reactions.Payload
	token   string
}

// hookWebhook records what a webhook Reaction POSTed. It is read from the test's goroutine while
// the server writes from its own, so every field is behind the mutex.
type hookWebhook struct {
	mu       sync.Mutex
	requests []hookRequest
}

// newHookWebhook starts a server that accepts a Reaction's POST and remembers it. The caller closes
// the server.
func newHookWebhook(t *testing.T) (*hookWebhook, *httptest.Server) {
	t.Helper()

	recorder := &hookWebhook{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var payload reactions.Payload
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		recorder.add(hookRequest{payload: payload, token: r.Header.Get(hookTokenHeader)})
		w.WriteHeader(http.StatusNoContent)
	}))
	return recorder, server
}

// add records one received POST.
func (h *hookWebhook) add(req hookRequest) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.requests = append(h.requests, req)
}

// count is how many POSTs have arrived so far.
func (h *hookWebhook) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.requests)
}

// first is the earliest POST recorded; it is only ever asked after count reported one.
func (h *hookWebhook) first() hookRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.requests[0]
}
