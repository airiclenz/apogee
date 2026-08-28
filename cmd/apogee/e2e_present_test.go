package main

// T-19 of the v0.17.1 release checklist — "present_document: no capability token in results;
// ODF/.epub refused" — as tests, for the half a human had to sit at a desktop for.
//
// It was manual because rung 1 hands a file to whatever application the desktop associates with it,
// and no test can assert that LibreOffice did NOT open. The ratified proxy (design call 9) replaces
// the desktop with a program of our own: openerLookPath resolves rung 1's `xdg-open`/`open` to a
// script that appends its argv to a log and exits. What the checklist reads off a screen — which
// documents opened — this reads off that log, and the claim is the stronger one, because a log line
// names the exact path apogee handed over.
//
// The second test is the served rung, where the checklist's own steps are already a READING rather
// than a desktop act: the transcript carries a capability URL and the tool result must not. A
// REMOTE session (SSH_CONNECTION) is what puts the ladder on rung 2 — climb's two branches are
// exclusive (internal/tui/presenter.go), so a local session never reaches the doc server however its
// opener is resolved.

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// presentInert are the workspace documents rung 1's allow-list keeps: a browser or an OS handler
// shows them and nothing in them runs (ADR 0019 §4, fourth amendment).
var presentInert = []string{"report.md", "chart.png", "notes.pdf", "sheet.csv"}

// presentRefused are the ones it will not hand over: the ODF family and .epub, which open an
// application with a macro engine behind it, and .html, whose active content belongs to the served
// rung's Content-Security-Policy rather than to a file:// launch.
var presentRefused = []string{"text.odt", "deck.odp", "book.epub", "page.html"}

// TestE2EPresentOpensOnlyTheAllowedFormats is T-19 steps 2–7: eight documents presented in one
// session, and exactly four of them reach an opener.
//
// The negative half is the half that was never automatable before. "Nothing opened for text.odt" is
// a claim about a program that was not started, and the only place it can be made is the log the
// opener would have written — so the refused four are asserted by the log's ABSENCE of them, after
// the same session proved the log records what it is given.
func TestE2EPresentOpensOnlyTheAllowedFormats(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rung 1 on Windows is `cmd /c start \"\" <path>`, which a POSIX log script cannot stand in for")
	}

	opener := fakeOpener(t)
	presentDesktop(t)
	stub := stubllm.New(t, loadScript(t, "present"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIIn(t, drv, stub, presentWorkspace(t), "")

	// Steps 2, 3 and 7 — the four inert formats. The last one's tool result is what says the whole
	// reply has been dispatched; the opener log is read only after it.
	submit(drv, "Present the finished deliverables.")
	drv.WaitText("The deliverables are presented.")
	drv.WaitQuiet(settled)

	opened := opener.argvs()
	if len(opened) != len(presentInert) {
		t.Fatalf("the opener ran %d time(s): %q; want one launch per inert document %q",
			len(opened), opened, presentInert)
	}
	for i, name := range presentInert {
		want := filepath.Join(sess.Workspace(), name)
		if opened[i] != want {
			t.Errorf("launch %d handed the opener %q; want %q", i+1, opened[i], want)
		}
	}

	// Steps 4, 5 and 6 — the ODF family, the e-book and the page. Each is presented, each says so
	// in the baseline wording, and none of them starts a program.
	submit(drv, "Present the office documents.")
	drv.WaitText("The office documents are presented.")
	drv.WaitQuiet(settled)

	if after := opener.argvs(); len(after) != len(presentInert) {
		t.Errorf("the opener ran %d time(s) in total: %q; want the refused four to have launched nothing",
			len(after), after)
	}

	// The wording is the model-facing half of the same claim, and it is a refusal reported as an
	// OUTCOME rather than as a tool error: the checklist fails the item either way round.
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
	record := sessionRecordText(t, sess.Home())
	for _, name := range presentInert {
		if !strings.Contains(record, "Presented "+name+": opened on the user's machine.") {
			t.Errorf("the session record does not say %s opened on the user's machine", name)
		}
	}
	for _, name := range presentRefused {
		if !strings.Contains(record, "Presented "+name+": the path is shown in the transcript for the user to open.") {
			t.Errorf("the session record does not carry the baseline wording for %s", name)
		}
	}
	if strings.Contains(record, "present_document failed") || strings.Contains(record, "could not open") {
		t.Error("a refused extension was reported as a tool error; the baseline wording is the outcome")
	}
}

// TestE2EPresentServesWithoutLeakingTheToken is T-19 steps 8 and 9: on a REMOTE session the document
// climbs to the doc server, the transcript carries the capability URL, and the tool result — the
// text the model reads, POSTs upstream and leaves in the session record — carries neither the URL
// nor the token in it.
//
// Step 9's reopen is the same claim one layer down: the record replays the presentation without a
// live link, because the codec drops the served URL on encode (ADR 0019 §3).
func TestE2EPresentServesWithoutLeakingTheToken(t *testing.T) {
	presentRemote(t)
	stub := stubllm.New(t, loadScript(t, "present"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIIn(t, drv, stub, presentWorkspace(t), "")

	submit(drv, "Present the report for review.")
	drv.WaitText("The report is presented.")
	drv.WaitQuiet(settled)

	// The URL is the transcript's alone — rung 0 prints it raw so the terminal can make it
	// clickable — and the token is the part of it that is a capability.
	served := servedURL(t, drv.Frame())
	token := servedToken(t, served)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
	record := sessionRecordText(t, sess.Home())
	if strings.Contains(record, token) {
		t.Errorf("the saved session carries the doc server's capability token %q", token)
	}
	if strings.Contains(record, served) {
		t.Errorf("the saved session carries the served URL %q", served)
	}
	if !strings.Contains(record, "Presented notes.pdf: shown in the transcript with a link.") {
		t.Error("the tool result the model was given is not in the record; the wording claim is untested")
	}

	// Step 9 — reopened from /sessions, the presentation is there and the link is not: the URL died
	// with the listener that issued it, and a replay that showed one would be offering a capability
	// nothing can honour.
	next := sess.Relaunch()
	next.WaitText("Send a message")
	submit(next, "/sessions")
	next.WaitText("⏎ resume")
	next.WaitQuiet(settled)
	next.Press(tuitest.Enter)
	next.WaitText("notes.pdf")
	next.WaitQuiet(settled)

	replay := next.Frame().String()
	if strings.Contains(replay, token) {
		t.Errorf("the reopened transcript replays the capability token %q:\n%s", token, next.Frame())
	}
}

// ----------------------------------------------------------------------------
// The fixtures a presentation needs
// ----------------------------------------------------------------------------

// presentWorkspace is T-19's workspace: the eight documents of the checklist's precondition, each a
// real file of its type as far as the ladder is concerned — the ladder judges the EXTENSION, so the
// bytes behind it are deliberately unimportant and the fixtures cost nothing to make.
func presentWorkspace(t *testing.T) string {
	t.Helper()

	ws := t.TempDir()
	for _, name := range append(append([]string{}, presentInert...), presentRefused...) {
		if err := os.WriteFile(filepath.Join(ws, name), []byte("fixture: "+name+"\n"), 0o600); err != nil {
			t.Fatalf("seed the presentation workspace: %v", err)
		}
	}
	return ws
}

// presentDesktop puts this test process on a LOCAL machine with a desktop, whatever the developer's
// own shell says: DISPLAY is what makes rung 1 eligible on Linux (present.HasDesktop), and an SSH
// variable left over from a real session would make the ladder REMOTE and skip rung 1 entirely.
func presentDesktop(t *testing.T) {
	t.Helper()

	t.Setenv("DISPLAY", ":0")
	for _, name := range []string{"SSH_CONNECTION", "SSH_TTY", "SSH_CLIENT"} {
		t.Setenv(name, "")
	}
}

// presentRemote is presentDesktop's opposite: a session reached over SSH, which is the only thing
// that puts the ladder on rung 2 (present.Locality). The third field of SSH_CONNECTION is the
// address this box was reached ON, and it is what the doc server advertises — loopback here, so the
// URL the transcript prints is one this machine could really answer.
func presentRemote(t *testing.T) {
	t.Helper()

	t.Setenv("SSH_CONNECTION", "192.0.2.10 54321 127.0.0.1 22")
	t.Setenv("DISPLAY", "")
}

// openerLog is the fake desktop: a program that records the argv it was launched with, standing in
// for the OS handler a real desktop would start.
type openerLog struct {
	t    *testing.T
	mu   sync.Mutex
	path string
}

// fakeOpener installs a resolver for rung 1's program name that answers with a script of our own,
// and returns the log that script writes. The script lives OUTSIDE the workspace on purpose: rung 1
// refuses a program that resolves inside the root the model may write (Opener.resolveProgram), so a
// fake opener placed there would be refused rather than launched and the test would pass for the
// wrong reason.
func fakeOpener(t *testing.T) *openerLog {
	t.Helper()

	dir := t.TempDir()
	log := &openerLog{t: t, path: filepath.Join(dir, "opened.log")}
	script := filepath.Join(dir, "fake-opener")
	body := "#!/bin/sh\nprintf '%s\\n' \"$1\" >> " + strconv.Quote(log.path) + "\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatalf("write the fake opener: %v", err)
	}

	was := openerLookPath
	openerLookPath = func(string) (string, error) { return script, nil }
	t.Cleanup(func() { openerLookPath = was })
	return log
}

// argvs are the document paths the opener has been launched with so far, in order. A log that does
// not exist yet is no launches — which is the answer the refused-extension half is asking for.
func (l *openerLog) argvs() []string {
	l.t.Helper()

	l.mu.Lock()
	defer l.mu.Unlock()
	data, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		l.t.Fatalf("read the opener log: %v", err)
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// servedURLPattern is the capability link rung 2 prints into the transcript. The frame is where it
// is read from, because the transcript is the ONLY place it is allowed to be.
var servedURLPattern = regexp.MustCompile(`http://[^\s]+`)

// servedURL is the doc server's URL as the transcript printed it.
func servedURL(t *testing.T, f tuitest.Frame) string {
	t.Helper()

	for _, row := range f.Rows() {
		if url := servedURLPattern.FindString(row); url != "" {
			return url
		}
	}
	t.Fatalf("no served URL in the transcript; rung 2 printed no link:\n%s", f)
	return ""
}

// servedToken is the capability path segment of a served URL — the part that grants access, and the
// part a tool result may never carry.
func servedToken(t *testing.T, url string) string {
	t.Helper()

	parts := strings.Split(strings.TrimPrefix(url, "http://"), "/")
	for _, part := range parts {
		if len(part) >= 16 {
			return part
		}
	}
	t.Fatalf("no capability segment in the served URL %q", url)
	return ""
}

// sessionRecordText is everything this home's session store holds, concatenated: the tool results
// the model was given, the transcript entries, and whatever else was persisted. A claim that some
// text is NOT in the record is only as good as the amount of record it read, so it reads all of it.
func sessionRecordText(t *testing.T, home string) string {
	t.Helper()

	dir := filepath.Join(home, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the session store: %v", err)
	}
	var b strings.Builder
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read the session record %s: %v", entry.Name(), err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	if b.Len() == 0 {
		t.Fatal("the session store is empty; there is no record to read")
	}
	return b.String()
}
