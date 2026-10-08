package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/stubllm"
)

// exampleFixture is the checked-in worked example of the script format. Serving it here pins
// two things at once: that the binary plays a fixture off disk, and that the example the design
// doc points a newcomer at is still a script this build can load.
const exampleFixture = "../apogee/testdata/stubllm/example.yaml"

// TestServeCommandPlaysTheCheckedInExample drives `serve` the way a shell script does: start
// it, read the address off stdout, ask it something, interrupt it. The printed line is a
// contract — a caller cannot find an ephemeral port any other way.
func TestServeCommandPlaysTheCheckedInExample(t *testing.T) {
	t.Parallel()

	out := &syncBuffer{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := run(ctx, out, "serve", "--script", exampleFixture, "--listen", "127.0.0.1:0")

	url := waitForAddress(t, out)
	if got := get(t, url+"/v1/models"); !strings.Contains(got, "stub-model") {
		t.Errorf("GET /v1/models = %q, want the model the fixture names", got)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("serve exited with %v, want a clean stop on the interrupt", err)
	}
}

// TestRecordCommandWithoutUpstreamIsAUsageError pins the difference between the two ways this
// binary can fail: a command line that names no server never starts a run, so it exits 2 with
// the usage text that answers it — not 1 with a bare message.
func TestRecordCommandWithoutUpstreamIsAUsageError(t *testing.T) {
	t.Parallel()

	out := &syncBuffer{}
	err := <-run(t.Context(), out, "record", "--out", filepath.Join(t.TempDir(), "fixture.yaml"))

	if err == nil {
		t.Fatal("record without --upstream succeeded")
	}
	if code := exitCodeFor(err); code != exitBadUsage {
		t.Errorf("exit code = %d, want %d", code, exitBadUsage)
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Errorf("output = %q, want the usage text", out.String())
	}
}

// TestServeCommandWithAnUnreadableScriptIsARunFailure pins the other side of that split: the
// command line was fine, so the failure is the run's — exit 1, and no usage dump for a
// misconfiguration the usage text cannot help with.
func TestServeCommandWithAnUnreadableScriptIsARunFailure(t *testing.T) {
	t.Parallel()

	out := &syncBuffer{}
	err := <-run(t.Context(), out, "serve", "--script", filepath.Join(t.TempDir(), "absent.yaml"))

	if err == nil {
		t.Fatal("serve with a missing script succeeded")
	}
	if code := exitCodeFor(err); code != exitRunFailed {
		t.Errorf("exit code = %d, want %d", code, exitRunFailed)
	}
	if strings.Contains(out.String(), "Usage:") {
		t.Errorf("output = %q, want no usage dump for a run failure", out.String())
	}
}

// TestRecordCommandWarnsOnStderrAboutUndecodableEvents pins the wiring of the recorder's warning
// to the command: a recorded stream that held an event the recorder could not decode is reported
// on the command's error writer when the fixture is written, naming the turn it came from.
func TestRecordCommandWarnsOnStderrAboutUndecodableEvents(t *testing.T) {
	t.Parallel()

	origin := malformingUpstream(t, stubllm.Script{
		Model: "rec-model",
		Turns: []stubllm.Turn{{Text: "one two three", ChunkRunes: 4}},
	})
	out := &syncBuffer{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture := filepath.Join(t.TempDir(), "fixture.yaml")
	done := run(ctx, out, "record", "--upstream", origin, "--out", fixture, "--listen", "127.0.0.1:0")

	url := waitForAddress(t, out)
	postStream(t, url)

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("record exited with %v, want the fixture written on the interrupt", err)
	}
	if want := "turn 1: 1 undecodable stream events dropped\n"; !strings.Contains(out.String(), want) {
		t.Errorf("output = %q, want the warning %q", out.String(), want)
	}
}

// malformingUpstream starts a scripted upstream that slips one undecodable data event into a
// streamed reply after its first write, and returns the upstream's URL.
func malformingUpstream(t *testing.T, s stubllm.Script) string {
	t.Helper()

	handler := stubllm.InProcess(t, s).Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		handler.ServeHTTP(&malformingResponse{ResponseWriter: w}, request)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// malformingResponse writes one undecodable SSE data event ahead of a handler's second write.
type malformingResponse struct {
	http.ResponseWriter
	writes int
}

// Write slips the undecodable event in ahead of the second write.
func (m *malformingResponse) Write(p []byte) (int, error) {
	m.writes++
	if m.writes == 2 {
		if _, err := m.ResponseWriter.Write([]byte("data: {\"choices\":[\n\n")); err != nil {
			return 0, err
		}
	}
	return m.ResponseWriter.Write(p)
}

// Flush passes the handler's flush through, so the stream still arrives event by event.
func (m *malformingResponse) Flush() {
	if flusher, ok := m.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// postStream sends one streamed completion request through the recorder and reads the reply to
// its end, so the proxy files the turn.
func postStream(t *testing.T, url string) {
	t.Helper()

	body := `{"model":"rec-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		url+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("read reply: %v", err)
	}
}

// run executes the command tree with args and returns a channel carrying its result, so a
// blocking subcommand can be inspected while it runs and stopped by cancelling ctx.
func run(ctx context.Context, out io.Writer, args ...string) <-chan error {
	cmd := newRootCommand()
	cmd.SetArgs(args)
	cmd.SetOut(out)
	cmd.SetErr(out)

	done := make(chan error, 1)
	go func() { done <- cmd.ExecuteContext(ctx) }()
	return done
}

// waitForAddress returns the URL the command announced, failing t if it never announces one.
func waitForAddress(t *testing.T, out *syncBuffer) string {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, after, found := strings.Cut(out.String(), "listening "); found {
			if url, _, complete := strings.Cut(after, "\n"); complete {
				return url
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no listening line within five seconds; output = %q", out.String())
	return ""
}

// get fetches a URL and returns the body.
func get(t *testing.T, url string) string {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return string(body)
}

// syncBuffer is a command output buffer the test reads while the command is still writing to
// it — the only way to see a blocking subcommand's first line before it exits.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
