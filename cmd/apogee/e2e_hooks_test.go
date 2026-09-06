package main

// Hooks end to end (ADR 0073): a driven TUI run whose `hooks:` block actually runs a command and
// actually POSTs a webhook, asserted from OUTSIDE apogee — a file on disk the command appended to,
// and an httptest server the webhook reached. Everything below the composition root has unit tests
// in internal/hooks; what these prove is the wiring the unit tests cannot see, and the promise that
// costs the most to break: nothing a Hook does reaches the screen or the Session record.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/hooks"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The environment a fired Hook is expected to inherit. A Hook's command is run with apogee's own
// environment plus the payload's APOGEE_HOOK_* facts, which is what lets a one-line `sh -c` script
// know where to write without the test rewriting the script for every temp directory.
const (
	hookSinkEnv     = "APOGEE_HOOK_SINK"
	hookTokenEnv    = "APOGEE_TEST_HOOK_TOKEN"
	hookTokenValue  = "hook-token-not-in-the-config-file"
	hookTokenHeader = "X-Apogee-Hook-Token"
)

// hookMarkers are the spellings a Hook would leave on the screen if any part of it leaked into the
// transcript. None of them may appear in a frame of a run whose Hooks all succeeded (ADR 0073 §1).
var hookMarkers = []string{
	"hook sink", "hook bell", "APOGEE_HOOK",
	string(hooks.FileChanged), string(hooks.ExchangeFinished), string(hooks.ApprovalWaiting),
}

// TestE2EHooksFireFromTheTUI drives the smoke journey with two Hooks configured — a command on
// `file-changed` and `exchange-finished`, a webhook on `approval-waiting` — and asserts what each
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
	t.Setenv(hookSinkEnv, sink)
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
		tuitest.Awaiting("the approval-waiting webhook to arrive"))

	if _, _, ok := drv.Frame().Find("Always allow this session"); !ok {
		t.Fatal("the approval pane was gone by the time the webhook arrived; the claim that the " +
			"Hook fired while the human was still being waited on is untestable")
	}
	waiting := bell.first()
	if waiting.payload.Event != hooks.ApprovalWaiting {
		t.Errorf("the webhook received the %q event; want %q",
			waiting.payload.Event, hooks.ApprovalWaiting)
	}
	if waiting.payload.Tool != "write_file" {
		t.Errorf("the approval-waiting payload names the tool %q; want write_file",
			waiting.payload.Tool)
	}
	if waiting.payload.Hook != "bell" {
		t.Errorf("the approval-waiting payload names the hook %q; want bell", waiting.payload.Hook)
	}
	if waiting.token != hookTokenValue {
		t.Errorf("the webhook's %s header = %q; want the value headers-env named in the "+
			"environment", hookTokenHeader, waiting.token)
	}

	// The approval is answered, the write lands, and the command Hook receives the two events the
	// journey produced.
	drv.WaitQuiet(settled)
	drv.Type("a")
	drv.WaitText("Appended the smoke test line")

	wantPath := filepath.Join(sess.Workspace(), "a.txt")
	drv.WaitFor(func() bool {
		fired := readHookPayloads(t, sink)
		changed, ok := hookPayloadFor(fired, hooks.FileChanged)
		if !ok || changed.Path != wantPath {
			return false
		}
		_, ok = hookPayloadFor(fired, hooks.ExchangeFinished)
		return ok
	}, tuitest.Awaiting("the file-changed and exchange-finished payloads in the hook sink"))

	fired := readHookPayloads(t, sink)
	changed, _ := hookPayloadFor(fired, hooks.FileChanged)
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

	// Nothing a Hook did reached the screen.
	drv.WaitQuiet(settled)
	final := drv.Frame().String()
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

	const failureLine = "hook failing"
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

// hookBlock is the `hooks:` block the first test runs with: the command Hook that appends every
// payload it receives to $APOGEE_HOOK_SINK, and the webhook Hook whose only header is read from the
// environment rather than written in the file.
func hookBlock(webhook string) string {
	return hookBlockOf(
		"  - name: sink\n" +
			"    events: [file-changed, exchange-finished]\n" +
			"    command: [sh, -c, 'cat >> \"$APOGEE_HOOK_SINK\"']\n" +
			"  - name: bell\n" +
			"    events: [approval-waiting]\n" +
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
func readHookPayloads(t *testing.T, path string) []hooks.Payload {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read the hook sink: %v", err)
	}
	var fired []hooks.Payload
	decoder := json.NewDecoder(bytes.NewReader(body))
	for {
		var payload hooks.Payload
		if err := decoder.Decode(&payload); err != nil {
			return fired
		}
		fired = append(fired, payload)
	}
}

// hookPayloadFor answers the first payload carrying event, and whether there was one.
func hookPayloadFor(fired []hooks.Payload, event hooks.Event) (hooks.Payload, bool) {
	for _, payload := range fired {
		if payload.Event == event {
			return payload, true
		}
	}
	return hooks.Payload{}, false
}

// hookRequest is one POST a webhook Hook made: the payload it carried and the header whose value
// came from the environment rather than the config file.
type hookRequest struct {
	payload hooks.Payload
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
		var payload hooks.Payload
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
