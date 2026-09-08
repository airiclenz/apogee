package main

// Hooks end to end (ADR 0073): runs whose `hooks:` block actually runs a command and actually POSTs
// a webhook, asserted from OUTSIDE apogee — a file on disk the command appended to, and an httptest
// server the webhook reached. Everything below the composition root has unit tests in
// internal/reactions; what these prove is the wiring the unit tests cannot see, and the promise that
// costs the most to break: nothing a Hook does reaches the screen or the Session record.
//
// Three roots compose that wiring and each gets its own case: the driven TUI session, an
// unattended `apogee headless` run, and one daemon Firing. The last two are where the payload's
// `schedule` field earns its keep — a Firing names the Schedule it ran for, a plain headless run
// names none — and where a Hook's trouble has no transcript to land in and goes to stderr and to
// the daemon log instead.

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
	"strings"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/reactions"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The environment a fired Hook is expected to inherit. A Hook's command is run with apogee's own
// environment plus the payload's APOGEE_REACTION_* facts, which is what lets a one-line `sh -c` script
// know where to write without the test rewriting the script for every temp directory.
const (
	hookSinkEnv     = "APOGEE_TEST_SINK"
	hookEnvSinkEnv  = "APOGEE_TEST_ENV_SINK"
	hookTokenEnv    = "APOGEE_TEST_HOOK_TOKEN"
	hookTokenValue  = "hook-token-not-in-the-config-file"
	hookTokenHeader = "X-Apogee-Hook-Token"
)

// hookMarkers are the spellings a Hook would leave behind that no run's own vocabulary can
// produce: the environment a fired command inherits, and the event names its payload carries. None
// of them may appear in a frame — or on the streams an unattended root writes — of a run whose
// Hooks all succeeded (ADR 0073 §1).
//
// The reports a Hook can put on screen are NOT in this list. They are caught by shape instead — see
// [hookReportPattern] — because a whitelist of prose stops biting the moment a report is reworded.
var hookMarkers = []string{
	"APOGEE_REACTION",
	string(reactions.FileChanged), string(reactions.ExchangeFinished), string(reactions.ApprovalRequested),
}

// hookReportPattern is the shape EVERY report the hooks path can put in front of a human takes: the
// word `reaction`, the entry's configured name, and then a separator — a colon before a Runner message
// (internal/reactions/runner.go:240, :397) or a space before the parenthesised event of a failure
// (:363). Those reports reach a frame through Report → Bridge.NotifyHook → an ephemeral note,
// and an unattended root's stderr the same way.
//
// It is built from the names a case CONFIGURES rather than from the prose those sites happen to use
// today, which is what keeps an absence check armed: renaming a Hook in the config re-aims the
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
// replaced never had. That whitelist spelled the reports out — "reaction sink", "reaction bell" — so it
// went blind on the one config change it exists to survive: a Hook renamed in the very block the
// absence check is watching. The derived pattern follows the rename instead, and covers every
// report shape the Runner emits rather than the two a whitelist happened to list.
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
			t.Errorf("the pattern %v misses %q, a report a renamed Hook would put on screen",
				pattern, report)
		}
	}

	// And it stays silent about what the runs themselves say, which is what makes a match a
	// finding rather than noise — the separator included: a Hook named `sink` does not make the
	// word `sinks` a hook report.
	configured := hookReportPattern(hooksSinkName, hooksBellName, hooksFailingName)
	for _, innocent := range []string{
		hooksAnswer,
		smokeWriteReply,
		"the hook sinks are files the test reads",
	} {
		if report := configured.FindString(innocent); report != "" {
			t.Errorf("the pattern reads %q in %q, which no Hook wrote", report, innocent)
		}
	}
}

// smokeWriteReply is what the smoke script answers the write prompt with
// (testdata/stubllm/smoke.yaml), and so the one thing the TUI half's final frame is certain to
// carry. The absence check uses it as its positive control: a frame that has lost it is not the
// frame the journey produced, and its silence about Hooks would prove nothing.
const smokeWriteReply = "Appended the smoke test line"

// TestE2EHooksFireFromTheTUI drives the smoke journey with two Hooks configured — a command on
// `file-changed` and `exchange-finished`, a webhook on `approval-requested` — and asserts what each
// of them received, from the outside: the file the command appended to, and the requests the
// httptest server recorded.
//
// The webhook claim is made BEFORE the approval is answered, which is the whole point of the event:
// a Hook that only learned about a waiting approval after the human dealt with it could not ring a
// bell for the prompt nobody was watching.
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
			"Hook fired while the human was still being waited on is untestable")
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
		t.Errorf("the approval-requested payload names the hook %q; want bell", waiting.payload.Reaction)
	}
	if waiting.token != hookTokenValue {
		t.Errorf("the webhook's %s header = %q; want the value headers-env named in the "+
			"environment", hookTokenHeader, waiting.token)
	}

	// The approval is answered, the write lands, and the command Hook receives the two events the
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
	}, tuitest.Awaiting("the file-changed and exchange-finished payloads in the hook sink"))

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
		t.Fatalf("read the hook environment sink: %v", err)
	}
	if !strings.Contains(string(envLines), wantLine) {
		t.Errorf("the fired command's environment sink holds\n%s\nwant a line %q — the executor "+
			"sets APOGEE_REACTION_EVENT and APOGEE_REACTION_PATH under exactly those names",
			envLines, wantLine)
	}

	// Nothing a Hook did reached the screen. The positive control comes first: an empty or
	// scrolled-away frame would satisfy every absence check below without proving anything, so the
	// frame must first be shown to carry the reply the successful run put there.
	drv.WaitQuiet(settled)
	final := drv.Frame().String()
	if !strings.Contains(final, smokeWriteReply) {
		t.Fatalf("the final frame does not carry the run's own reply %q, so its silence about "+
			"Hooks proves nothing:\n%s", smokeWriteReply, final)
	}
	if report := hookReportPattern(hooksSinkName, hooksBellName).FindString(final); report != "" {
		t.Errorf("the final frame carries the hook report %q:\n%s", report, final)
	}
	for _, marker := range hookMarkers {
		if strings.Contains(final, marker) {
			t.Errorf("the final frame carries the hook marker %q:\n%s", marker, final)
		}
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EHooksReportAFailureAsAnEphemeralNote is the other half of ADR 0073 §8: a Hook that refuses
// to run is the human's business and nobody else's. The run's config is rewritten on disk to a Hook
// that exits 1, the watcher applies it, and the next exchange fires it — the failure lands as ONE
// transcript note and is absent from the record the session saved.
func TestE2EHooksReportAFailureAsAnEphemeralNote(t *testing.T) {
	script, err := stubllm.Load("testdata/stubllm/smoke.yaml")
	if err != nil {
		t.Fatalf("load the smoke script: %v", err)
	}
	stub := stubllm.New(t, script)
	drv := tuitest.NewDriver(t, e2eSize)
	// The run starts with a Hook that never fires, so the rewrite below is a CHANGE to the `hooks:`
	// key rather than its first appearance — which is what the watcher's applied-keys note names.
	sess := launchTUIConfigured(t, drv, stub, hookBlockOf(
		"  - name: quiet\n    events: [error]\n    command: [sh, -c, 'exit 0']\n"))

	rewriteHomeHooks(t, sess.Home(),
		"  - name: failing\n    events: [exchange-finished]\n    command: [sh, -c, 'exit 1']\n")
	drv.WaitText(appliedNote)
	drv.WaitQuiet(settled)
	if note := rowContaining(t, drv.Frame(), appliedNote); !strings.Contains(note, "hooks") {
		t.Fatalf("the applied-keys note %q does not name the hooks key", note)
	}

	submit(drv, "What files are in this workspace?")
	drv.WaitText("The workspace holds one file")

	const failureLine = "reaction failing"
	drv.WaitText(failureLine)
	drv.WaitQuiet(settled)
	notice := drv.Frame()
	if n := rowsContaining(notice, failureLine); n != 1 {
		t.Errorf("a failing hook left %d notice lines; want exactly one:\n%s", n, notice)
	}
	if row := rowContaining(t, notice, failureLine); !strings.Contains(row, "exit 1") {
		t.Errorf("the hook notice %q does not say the command exited 1", row)
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
			t.Errorf("the session record %s kept a hook notice; nothing a Hook does may reach it",
				saved.Name())
		}
	}
}

// The unattended halves' conversation, restated from testdata/stubllm/hooks.yaml so an assertion
// reads as the claim it makes. The answer shares no word with a Hook's own vocabulary — neither a
// [hookMarkers] spelling nor a [hookReportPattern] shape — so finding it on stdout is finding the
// model's reply and nothing else, which is what lets it serve as the absence check's positive
// control.
const (
	hooksPrompt = "Say what both roots do."
	hooksAnswer = "Both roots fired their hooks."

	// The schedule the daemon half puts on the clock, and the Hooks the cases subscribe: one that
	// records every payload it is handed, one that refuses to run at all, and the webhook the
	// driven half rings. Every absence check derives its [hookReportPattern] from these same
	// names, so renaming a Hook here re-aims the assertion rather than un-arming it.
	hooksScheduleName = "hook-probe"
	hooksSinkName     = "sink"
	hooksFailingName  = "noisy"
	hooksBellName     = "bell"
)

// TestE2EHooksFireFromAHeadlessRun is the unattended half: no screen, no human, one prompt, and the
// `exchange-finished` Hook still fires — with a payload that names the workspace the run was rooted
// in and NO schedule, because a plain headless run belongs to none.
//
// The stdout claim rides along because this is the only Driver that has one, and because it is the
// contract a script piping apogee depends on (TestHeadlessAnswerLandsOnTheProcessStdout): a Hook
// writes to its own sink and to nowhere else, so the answer stream is the answer alone even while a
// Hook is firing off it.
func TestE2EHooksFireFromAHeadlessRun(t *testing.T) {
	sink := filepath.Join(t.TempDir(), "fired.jsonl")
	t.Setenv(hookSinkEnv, sink)

	stub := stubllm.New(t, loadScript(t, "hooks"))
	stdout, stderr, workspace := headlessHooksAgainst(t, stub, hooksPrompt, hookBlockOf(
		"  - name: "+hooksSinkName+"\n"+
			"    events: [exchange-finished]\n"+
			"    command: [sh, -c, 'cat >> \"$APOGEE_TEST_SINK\"']\n"))

	if got := strings.TrimSpace(stdout); got != hooksAnswer {
		t.Errorf("stdout = %q; want the answer alone (%q)", stdout, hooksAnswer)
	}

	// The run is over, and a headless run drains its Hooks before it returns, so the sink is final:
	// what is in it now is everything that ever fired.
	fired := readHookPayloads(t, sink)
	if len(fired) != 1 {
		t.Fatalf("the hook sink holds %d payloads, want exactly one for the run's one exchange:\n%+v",
			len(fired), fired)
	}
	payload := fired[0]
	if payload.Event != reactions.ExchangeFinished {
		t.Errorf("the payload carries the %q event; want %q", payload.Event, reactions.ExchangeFinished)
	}
	if payload.Reaction != hooksSinkName {
		t.Errorf("the payload names the hook %q; want %q", payload.Reaction, hooksSinkName)
	}
	if payload.Workspace != workspace {
		t.Errorf("the payload's workspace = %q; want the run's own %q", payload.Workspace, workspace)
	}
	if payload.Schedule != nil {
		t.Errorf("the payload carries the schedule %+v; a plain headless run belongs to none",
			payload.Schedule)
	}

	// Nothing a Hook did reached either stream. Hooks that succeed are silent, and the one that
	// fired here did. The positive control comes first, for the reason the driven half has one: a
	// run that never produced any output at all would pass every absence check below.
	if !strings.Contains(stdout, hooksAnswer) {
		t.Fatalf("stdout does not carry the run's own answer %q, so its silence about Hooks "+
			"proves nothing:\n%s", hooksAnswer, stdout)
	}
	reports := hookReportPattern(hooksSinkName)
	if report := reports.FindString(stdout); report != "" {
		t.Errorf("stdout carries the hook report %q:\n%s", report, stdout)
	}
	if report := reports.FindString(stderr); report != "" {
		t.Errorf("stderr carries the hook report %q:\n%s", report, stderr)
	}
	for _, marker := range hookMarkers {
		if strings.Contains(stdout, marker) {
			t.Errorf("stdout carries the hook marker %q:\n%s", marker, stdout)
		}
		if strings.Contains(stderr, marker) {
			t.Errorf("stderr carries the hook marker %q:\n%s", marker, stderr)
		}
	}
}

// TestDaemonFiringFiresHooks is the third root: one `apogee daemon` Firing, whose `turn-finished`
// payload names the Schedule it ran for — the fact no other Driver's payload carries, and the whole
// reason the Runner is built per Firing rather than per daemon (ADR 0073 §9).
//
// The second Hook refuses to run, because a daemon has no transcript to note a failure in and its
// log IS its user interface (ADR 0034 decision 10). One line, once: a Hook that fails every Firing
// for a week must not fill a journal.
//
// It runs the REAL runner rather than the harness's stub, for the reason TestDaemonFaultedVerbColumn
// runs it: the events a Hook fires off are the ENGINE's, and a stubbed runner emits none of them.
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
		"  - name: "+hooksSinkName+"\n"+
			"    events: [turn-finished]\n"+
			"    command: [sh, -c, 'cat >> \"$APOGEE_TEST_SINK\"']\n"+
			"  - name: "+hooksFailingName+"\n"+
			"    events: [turn-finished]\n"+
			"    command: [sh, -c, 'exit 1']\n")+
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

	// The failing Hook's line, in the shape the Runner reports and the log's own sanitiser passed
	// through: the Hook's name, the event it was fired by, and why it failed.
	h.awaitLog(t, "reaction "+hooksFailingName+" (turn-finished): exit 1")
	// And the Firing landing, which is what makes the sink below final: a daemon drains a Firing's
	// Hooks before the Outcome that prints this line is returned.
	h.awaitLog(t, "completed "+hooksScheduleName)

	h.stop()
	if err := wait(); err != nil {
		t.Fatalf("the daemon returned %v; a fired Hook is not a daemon failure\n%s", err, h.errOut.String())
	}

	fired := readHookPayloads(t, sink)
	payload, ok := hookPayloadFor(fired, reactions.TurnFinished)
	if !ok {
		t.Fatalf("the hook sink holds no turn-finished payload; it holds:\n%+v", fired)
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

	// Once, and only once: the de-dup is what keeps a permanently broken Hook from filling a
	// journal a supervisor reads for a week.
	if n := strings.Count(h.out.String(), "reaction "+hooksFailingName+" ("); n != 1 {
		t.Errorf("the failing hook left %d lines in the daemon log, want exactly one:\n%s",
			n, h.out.String())
	}
}

// headlessHooksAgainst runs one real `apogee headless` against stub in a home of its own and hands
// back the three things this file has to read: what the answer stream carried, what the run
// narrated, and the workspace it was rooted in — the path a payload's `workspace` is asserted
// against. extraConfig is written above the `servers:` block, which is how a case reaches a
// file-only key such as `hooks:`.
//
// It is [headlessAgainst]'s twin rather than a call to it: that one returns stderr ALONE, pins the
// naming journey's own prompt, and wraps the run's Event sink to drive a gate
// (e2e_naming_test.go) — three things this file needs otherwise. Nothing else differs, the runner
// least of all: it BINDS `runOnce` to the production [run.Once] and restores it after, because what
// a Hook observes is exactly the engine's own event stream and a stubbed runner produces none of it
// — a claim that has to rest on this helper's own code rather than on whatever ran before it.
func headlessHooksAgainst(t *testing.T, stub *stubllm.Server, prompt, extraConfig string) (stdout, stderr, workspace string) {
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
	cmd.SetArgs([]string{"--config", home, "--workspace", workspace, prompt})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("headless: %v\n%s", err, errBuf.String())
	}
	return outBuf.String(), errBuf.String(), workspace
}

// hookBlock is the `hooks:` block the first test runs with: the command Hook that appends every
// payload it receives to $APOGEE_TEST_SINK and every firing's two headline environment facts to
// $APOGEE_TEST_ENV_SINK, and the webhook Hook whose only header is read from the environment rather
// than written in the file.
//
// The script names APOGEE_REACTION_EVENT and APOGEE_REACTION_PATH rather than reading the JSON on
// its stdin, because those are the EXACT variable names the executor sets: a rename of either would
// leave the line it writes blank, which is what the assertion in TestE2EHooksFireFromTheTUI reads.
func hookBlock(webhook string) string {
	return hookBlockOf(
		"  - name: " + hooksSinkName + "\n" +
			"    events: [file-changed, exchange-finished]\n" +
			"    command: [sh, -c, 'cat >> \"$APOGEE_TEST_SINK\"; " +
			"echo \"$APOGEE_REACTION_EVENT $APOGEE_REACTION_PATH\" >> \"$APOGEE_TEST_ENV_SINK\"']\n" +
			"  - name: " + hooksBellName + "\n" +
			"    events: [approval-requested]\n" +
			"    webhook: " + webhook + "\n" +
			"    headers-env:\n" +
			"      " + hookTokenHeader + ": " + hookTokenEnv + "\n")
}

// hookBlockOf wraps one or more already-indented entries in the `hooks:` key.
func hookBlockOf(entries string) string { return "hooks:\n" + entries }

// rewriteHomeHooks replaces a home's whole `hooks:` block with entries, keeping everything the
// config said before it. [appendHomeConfig] cannot do this — a second `hooks:` key is a duplicate
// rather than a change of mind — and the block is the tail of the file by construction, so cutting
// at it leaves the `servers:` the run is talking through untouched.
func rewriteHomeHooks(t *testing.T, home, entries string) {
	t.Helper()

	path := filepath.Join(home, "config.yaml")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the run's config: %v", err)
	}
	head, _, found := strings.Cut(string(body), "hooks:\n")
	if !found {
		t.Fatalf("the run's config carries no hooks: block to rewrite:\n%s", body)
	}
	if err := os.WriteFile(path, []byte(head+hookBlockOf(entries)), 0o600); err != nil {
		t.Fatalf("write the run's config: %v", err)
	}
}

// readHookPayloads reads back what a command Hook appended to its sink. The payloads are
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
		t.Fatalf("read the hook sink: %v", err)
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

// hookRequest is one POST a webhook Hook made: the payload it carried and the header whose value
// came from the environment rather than the config file.
type hookRequest struct {
	payload reactions.Payload
	token   string
}

// hookWebhook records what a webhook Hook POSTed. It is read from the test's goroutine while the
// server writes from its own, so every field is behind the mutex.
type hookWebhook struct {
	mu       sync.Mutex
	requests []hookRequest
}

// newHookWebhook starts a server that accepts a Hook's POST and remembers it. The caller closes the
// server.
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
