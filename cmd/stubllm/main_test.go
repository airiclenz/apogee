package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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
