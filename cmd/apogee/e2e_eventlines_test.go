package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// pipeGate is the label the closed-pipe run's only scripted reply waits on, so the test can close
// the reader while the run is still going and be certain the lines that meet the broken pipe are
// written AFTER the close rather than racing it into the pipe's buffer.
const pipeGate = "answer"

// TestE2EEventLinesSurviveAClosedPipe is the SIGPIPE claim end to end, in the only place it can be
// made: a real process with a real pipe on a real file descriptor 1. In the test binary the signal
// is already handled, so nothing in-process can prove the disarm — the Go runtime re-raises SIGPIPE
// with the default disposition only for writes to fd 1 and 2 of a program that has not asked
// otherwise, which is precisely what `apogee headless --format json` asks for (sigpipe_unix.go).
//
// What the run must do when its consumer walks away: keep going, keep its own exit code, and say
// once on stderr that the stream stopped (ADR 0075 decision 9). What it must NOT do is die of a
// signal with its record unsaved because something downstream stopped reading.
func TestE2EEventLinesSurviveAClosedPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGPIPE is a POSIX signal; on Windows a broken pipe is already an ordinary write error")
	}
	if e2eBinary == "" {
		t.Skipf("the binary under test was not built: %v", e2eBuildErr)
	}
	assertNoAmbientApogeeConfig(t)

	// One held reply: the run opens its stream, then waits, so the close below lands mid-run.
	stub := stubllm.New(t, stubllm.Script{
		Model: "stub-model",
		Turns: []stubllm.Turn{{Text: "the answer", Await: pipeGate}},
	})
	home := e2eHome(t, stub)
	// Naming off: a second request would be a second thing to script, and this case is about the
	// stream's writes, not about how many the run makes.
	appendHomeConfig(t, home, "auto-title: false\n")

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("make the stdout pipe: %v", err)
	}
	defer reader.Close()

	cmd := exec.Command(e2eBinary,
		"--config", home, "--workspace", e2eWorkspace(t),
		"headless", "--format", "json", "list the files")
	cmd.Env = ptyEnv()
	cmd.Stdout = writer
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the headless run: %v", err)
	}
	// The child holds the only writer from here, so the read side sees EOF if it ever exits.
	writer.Close()

	// The opening frame is written before the run makes its first request, so it is on the pipe
	// while the reply is still held.
	line, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil {
		t.Fatalf("read the stream's first line: %v\nstderr:\n%s", err, errBuf.String())
	}
	if !strings.Contains(line, `"run_started"`) {
		t.Fatalf("the stream's first line is not the opening frame: %s", line)
	}

	// The consumer walks away. Every line the run writes from now on meets a broken pipe.
	if err := reader.Close(); err != nil {
		t.Fatalf("close the read end: %v", err)
	}
	stub.Release(pipeGate)

	err = cmd.Wait()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			// -1 is what a process killed by a signal reports, which is exactly the death this
			// item exists to prevent.
			t.Fatalf("the run exited with code %d (-1 means a signal killed it: %v); a closed "+
				"reader must break the stream, not the run\nstderr:\n%s",
				exit.ExitCode(), exit.ProcessState, errBuf.String())
		}
		t.Fatalf("wait for the headless run: %v\nstderr:\n%s", err, errBuf.String())
	}

	const stopped = "event lines stopped"
	if n := strings.Count(errBuf.String(), stopped); n != 1 {
		t.Errorf("the stream reported its own failure %d times, want exactly once:\n%s",
			n, errBuf.String())
	}
}

// eventLinesPrompt is what the scripted upstream matches its first turn on (eventlines.yaml).
const eventLinesPrompt = "Read the workspace file and say what is in it"

// eventLinesGolden names one of this contract's goldens. They are `.jsonl` files in a directory of
// their own rather than frames under testdata/frames/, which is what [tuitest.GoldenText] exists
// for: the bytes being pinned are a machine-readable protocol, not a rendered pane.
func eventLinesGolden(name string) string {
	return filepath.Join("testdata", "eventlines", name+".jsonl")
}

// eventLinesRedactions is what has to come out of the stream before it can be compared to a file on
// disk: the clock, the run's own id, the build string and the temporary workspace. Everything else
// a line carries is either scripted (the model, the token chunks, the two usage blocks) or derived
// from the prompt, and is therefore the same on every host — which is the point of comparing at
// all. Nothing here redacts a NAME or a KEY: a member that silently disappeared would still be a
// diff, and that is exactly the failure a golden is here to catch.
func eventLinesRedactions(workspace string) []tuitest.Redaction {
	return []tuitest.Redaction{
		tuitest.Redact(`"time":"[^"]*"`, `"time":"<time>"`),
		tuitest.Redact(`"session":"[^"]*"`, `"session":"<session>"`),
		tuitest.Redact(`"version":"[^"]*"`, `"version":"<version>"`),
		tuitest.Redact(regexp.QuoteMeta(workspace), "<workspace>"),
	}
}

// headlessEventLines runs one `apogee headless --format json` in-process — the shape
// headlessAgainst uses (e2e_naming_test.go), with stdout captured instead of stderr, because
// stdout is where the whole contract lives.
//
// The runner is explicit and always restored. Two of the cases below pass [run.Once], because a
// golden taken against a stubbed runner would pin nothing about the engine; the third passes a
// canned one on purpose, and stating the seam at every call site is what keeps the difference
// visible rather than ambient — the neighbouring tests in package main swap it too.
func headlessEventLines(
	t *testing.T,
	runner func(context.Context, run.Spec) (run.Result, error),
	home, workspace, prompt string,
) (out, errOut string, err error) {
	t.Helper()

	prev := runOnce
	runOnce = runner
	t.Cleanup(func() { runOnce = prev })
	// The environment must not move the home, the mode or the server out from under the run.
	assertNoAmbientApogeeConfig(t)

	cmd := newHeadlessCommand()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{
		"--config", home, "--workspace", workspace, "--format", "json", prompt,
	})
	err = cmd.ExecuteContext(context.Background())
	return outBuf.String(), errBuf.String(), err
}

// eventLinesHome writes the apogee home a golden run reads: one server, and a pinned context
// window. The window is pinned rather than discovered because the `usage` lines carry it and a
// stub advertises none — an unpinned run would record a zero here and a real number the day the
// stub grows a `/v1/models` field, which is a golden that churns for no reason.
func eventLinesHome(t *testing.T, endpoint, model string) string {
	t.Helper()

	home := t.TempDir()
	writeConfigHome(t, home,
		"context-window: 32768\n"+
			"servers:\n"+
			"  - name: stub\n"+
			"    endpoint: "+endpoint+"\n"+
			"    model: "+model+"\n"+
			"server: stub\n")
	return home
}

// assertEventLineEnvelopes is the semantic half of every golden below, and the half that survives a
// re-record: whatever the lines say, the envelope's own promises hold — `v` is 1 on every line,
// `seq` starts at 1 and skips nothing, and the last line is the closing frame (ADR 0075
// decisions 3 and 5). A golden alone would pass a stream that renumbered itself.
func assertEventLineEnvelopes(t *testing.T, lines []map[string]any) {
	t.Helper()

	if len(lines) == 0 {
		t.Fatal("the stream carried no lines at all")
	}
	for i, line := range lines {
		if line["v"] != float64(1) {
			t.Errorf("line %d: v = %v; every line of this contract is version 1", i+1, line["v"])
		}
		if want := float64(i + 1); line["seq"] != want {
			t.Errorf("line %d: seq = %v; want %v — the sequence starts at 1 and skips nothing",
				i+1, line["seq"], want)
		}
	}
	if last := lines[len(lines)-1]; last["event"] != "run_finished" {
		t.Errorf("the stream's last line is %v; every exit path closes with run_finished", last["event"])
	}
}

// TestE2EEventLinesGolden pins the Event lines byte for byte, which ADR 0075 §14 asks for and the
// rendering-surfaces-only rule at the top of internal/tuitest/golden.go is superseded for: this is
// a documented stdout protocol, and a consumer parses the bytes rather than the meaning. Key order,
// the `null` a member takes where a line has no value, and the exact set of members a variant
// carries are all things a semantic assertion would let drift and a script would break on.
//
// Three streams, because the contract's hard promise is about EXIT PATHS rather than about the
// happy one: a run that completed, a run refused before it had a sink to write to, and a run
// refused after it had one. Each is compared to its own golden and then asserted semantically, so a
// re-recorded golden still has to satisfy the envelope's invariants.
func TestE2EEventLinesGolden(t *testing.T) {
	t.Run("a completed run", func(t *testing.T) {
		stub := stubllm.New(t, loadScript(t, "eventlines"))
		workspace := e2eWorkspace(t)
		home := eventLinesHome(t, stub.URL, stub.Model)

		out, errOut, err := headlessEventLines(t, run.Once, home, workspace, eventLinesPrompt)
		if err != nil {
			t.Fatalf("the scripted run returned %v\nstderr:\n%s", err, errOut)
		}
		stub.AssertConsumed(t)

		tuitest.GoldenText(t, eventLinesGolden("run"), out, eventLinesRedactions(workspace)...)

		lines := jsonEventLines(t, out)
		assertEventLineEnvelopes(t, lines)
		if lines[0]["event"] != "run_started" {
			t.Errorf("the stream opens with %v; a run that started announces itself first",
				lines[0]["event"])
		}
		_, data := finishedFrame(t, lines)
		wantExitCode(t, data, 0)
	})

	t.Run("a run whose server answered nothing", func(t *testing.T) {
		// The dial itself is refused: port 1 is reserved and nothing listens on it, so the beat the
		// composer takes comes back unanswered and the offline gate refuses the run before any sink
		// exists (headless.go). The stream is therefore the closing frame ALONE — no run_started,
		// because nothing started.
		workspace := e2eWorkspace(t)
		home := eventLinesHome(t, "http://127.0.0.1:1", "eventlines-model")

		out, _, err := headlessEventLines(t, run.Once, home, workspace, eventLinesPrompt)
		if err == nil {
			t.Fatal("a run whose server answered nothing was allowed to start")
		}

		// The error text carries the dial's own words, which name the OS's error string and vary
		// between hosts; the golden holds the token and the assertion below holds the sentence.
		redactions := append(eventLinesRedactions(workspace),
			// The pattern walks escaped characters rather than stopping at the first `"`: the text
			// quotes the URL it dialled, so a naive `[^"]*` would redact half the sentence and
			// leave the OS-specific half in the golden.
			tuitest.Redact(`"error":"(?:[^"\\]|\\.)*"`, `"error":"<error>"`))
		tuitest.GoldenText(t, eventLinesGolden("not-started"), out, redactions...)

		lines := jsonEventLines(t, out)
		assertEventLineEnvelopes(t, lines)
		if len(lines) != 1 {
			t.Fatalf("the stream carried %d lines; a refusal ahead of the opening frame is the "+
				"closing frame alone:\n%s", len(lines), out)
		}
		envelope, data := finishedFrame(t, lines)
		wantExitCode(t, data, exitNotStarted)
		if envelope["session"] == nil {
			t.Error("session is null; the id was minted before the offline gate refused")
		}
		if data["saved"] != false {
			t.Errorf("saved = %v; a refused run wrote no record", data["saved"])
		}
		text, _ := data["error"].(string)
		if !strings.Contains(text, "cannot send — server offline") {
			t.Errorf("the closing frame's error is %q; want the offline sentence every Driver "+
				"shares (internal/notice)", text)
		}
	})

	t.Run("a run refused after the sink exists", func(t *testing.T) {
		// run.Once's own never-started shape — an error with ZERO turns behind it — reached through
		// a canned runner, because every real way to provoke it is a host fact a golden must not
		// depend on (a kernel that cannot fence, an endpoint no config set, a model that will not
		// bind). It lands AFTER the opening frame has been written, which is the whole difference
		// from the case above: the stream is the two frames and nothing between them.
		workspace := e2eWorkspace(t)
		home := eventLinesHome(t, "http://127.0.0.1:1", "eventlines-model")
		// The offline gate stands between this run and its runner, and its verdict is a real dial.
		// An answering beat is what lets the refusal below be the one this case is about.
		swapAnsweringBeat(t)

		canned := &stubRunner{err: errors.New(
			"apogee: construct the firing's agent: apogee: Config.Endpoint is required")}
		out, _, err := headlessEventLines(t, canned.once, home, workspace, eventLinesPrompt)
		if err == nil {
			t.Fatal("a construction refusal was reported as a success")
		}
		if !canned.called {
			t.Fatal("the run never reached the runner; this case is about the exit that follows it")
		}

		tuitest.GoldenText(t, eventLinesGolden("not-started-after-sink"), out,
			eventLinesRedactions(workspace)...)

		lines := jsonEventLines(t, out)
		assertEventLineEnvelopes(t, lines)
		if len(lines) != 2 || lines[0]["event"] != "run_started" {
			t.Fatalf("the stream carried %d lines; want the opening frame then the closing one:\n%s",
				len(lines), out)
		}
		_, data := finishedFrame(t, lines)
		wantExitCode(t, data, exitNotStarted)
	})
}
