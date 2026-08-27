package tui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// The tests below pin the [Build] seam — the split that lets a Driver (ADR 0031, ADR 0062) enter
// through the SAME construction the binary uses. Two things need pinning and they pull in opposite
// directions: the production path must be byte-for-byte what it was before the split, and the
// driver path must genuinely put the caller's writer where the renderer paints.

// syncBuffer is the smallest stand-in for a terminal a running program can paint into: a buffer
// the renderer goroutine writes and the test goroutine reads, behind one mutex so -race is happy.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p) //nolint:wrapcheck
}

func (b *syncBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

// runUntil starts program, waits for ready to hold, then cancels the program's context and waits
// for the loop to return. It fails rather than hangs: every wait is bounded.
func runUntil(t *testing.T, program *tea.Program, cancel context.CancelFunc, ready func() bool, what string) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = program.Run()
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			program.Kill()
			<-done
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		program.Kill()
		<-done
		t.Fatal("the program did not return when its context was cancelled")
	}
}

// TestBuildNilOutputMatchesRun is the regression guard on the split itself: with no driver output
// the options are [programOptions]' options, unchanged, in every shape the flag combination can
// take. If this drifts, the binary is painting through something the pre-split binary did not.
func TestBuildNilOutputMatchesRun(t *testing.T) {
	t.Parallel()

	trace := filepath.Join(t.TempDir(), "trace.txt")
	cases := []struct {
		name    string
		opts    Options
		environ []string
		decline bool
	}{
		{name: "nothing switched on"},
		{name: "an environment", environ: []string{"TERM=xterm-256color"}},
		{name: "a trace path", opts: Options{TracePath: trace}},
		{name: "the sync-query stripper", decline: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			wantOpts, wantTraced, wantErr := programOptions(ctx, tc.opts, tc.environ, tc.decline)
			if wantTraced != nil {
				defer func() { _ = wantTraced.Close() }()
			}
			gotOpts, gotTraced, gotErr := buildProgramOptions(ctx, tc.opts, tc.environ, tc.decline, nil)
			if gotTraced != nil {
				defer func() { _ = gotTraced.Close() }()
			}
			if !errors.Is(gotErr, wantErr) {
				t.Fatalf("error = %v, want %v", gotErr, wantErr)
			}
			if len(gotOpts) != len(wantOpts) {
				t.Errorf("got %d program options, want %d", len(gotOpts), len(wantOpts))
			}
			if (gotTraced == nil) != (wantTraced == nil) {
				t.Errorf("traced output = %v, want the same as programOptions (%v)", gotTraced, wantTraced)
			}
		})
	}
}

// TestBuildDriverOutputSkipsTrace: a driver's writer replaces the whole output half. No traced
// wrapper, no sync-query stripper — both of those wrap os.Stdout, which is not where this program
// paints — and the renderer really does write into the writer that was handed in.
func TestBuildDriverOutputSkipsTrace(t *testing.T) {
	ctx := context.Background()
	out := &syncBuffer{}
	// The stripper is asked for and must still be refused: it is an os.Stdout wrapper.
	opts, traced, err := buildProgramOptions(ctx, Options{}, nil, true, out)
	if err != nil {
		t.Fatalf("buildProgramOptions: %v", err)
	}
	if traced != nil {
		t.Errorf("a traced output was installed on a driver output")
	}
	if len(opts) != 2 { // tea.WithContext plus tea.WithOutput
		t.Errorf("got %d program options, want 2 (context plus the driver's output)", len(opts))
	}

	// And the option is not merely present: it is the writer the frames land in.
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	program, cleanup, err := Build(runCtx, &fakeEngine{}, NewBridge(), testOpts, out,
		tea.WithInput(nil), tea.WithWindowSize(80, 24))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer cleanup()
	runUntil(t, program, cancel, func() bool { return out.Len() > 0 }, "the driver's output to be painted into")
}

// TestBuildRefusesTraceWithOutput: --tui-trace wraps the real terminal, so a driver output makes it
// a lie. It is refused at the seam rather than honoured into a file that stays empty — an empty
// trace looks exactly like a run that painted nothing, which is the bug it exists to diagnose.
func TestBuildRefusesTraceWithOutput(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "trace.txt")
	_, cleanup, err := Build(context.Background(), &fakeEngine{}, NewBridge(),
		Options{TracePath: path}, io.Discard)
	if !errors.Is(err, errTraceWithDriverOutput) {
		if cleanup != nil {
			cleanup()
		}
		t.Fatalf("Build with a trace path and a driver output: err = %v, want errTraceWithDriverOutput", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Errorf("the refused trace file %s was created anyway", path)
	}
}

// TestBuildAppendsCallerOptionsLast: a Driver's options are appended last precisely so they WIN.
// The proof is the strongest one available — two writers, and the frames land in the caller's.
func TestBuildAppendsCallerOptionsLast(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	built, caller := &syncBuffer{}, &syncBuffer{}
	program, cleanup, err := Build(ctx, &fakeEngine{}, NewBridge(), testOpts, built,
		tea.WithInput(nil), tea.WithWindowSize(80, 24), tea.WithOutput(caller))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer cleanup()
	runUntil(t, program, cancel, func() bool { return caller.Len() > 0 }, "the caller's output to be painted into")
	if n := built.Len(); n != 0 {
		t.Errorf("Build's own tea.WithOutput received %d bytes; the caller's option must win", n)
	}
}
