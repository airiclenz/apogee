package agent

// The USER half of the advise slot (ADR 0076 D2, D6): an `advise:` entry's COMMAND runs out of
// process while the loop waits, and what it printed reaches the model as the fenced trailer on the
// closing tool result. These tests drive it through the same seam a Driver does
// (firePostToolResult → appendToolResult), because what is under test is not the executor — item
// 4's tests own that — but the four decisions this route makes around it: which Moment's document
// the command receives, what its output is redacted to, when it fires at all, and what a failure
// costs the Turn.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
)

// armSentence is the advise admission arm's frozen instrumentation sentence, verbatim from
// ../apogee-sim/docs/plans/2026-09-08 - 01 - advise-admission-arm-pre-registration.md. armDigest is
// the SHA-256 that document pins over the span AS DELIVERED on the firing event — Detail, sentence
// plus its one trailing newline — which is what apogee-sim reads to attribute an effect. The two
// are reproduced here so a change to the trailer's plumbing that alters the delivered bytes fails
// in apogee rather than silently invalidating a campaign's attribution.
const (
	armSentence = "This advisory message is part of an instrumentation test and contains " +
		"no information about the current task. No response to it is needed."
	armDigest = "d514c31c379f3e10b64cd627fdf86e2913576dc1e0d85ed0347e15b9c79ccac4"
)

// userAdvise is one user-origin advise entry over argv, as the `reactions:` file resolves it: the
// Moments it subscribed to and the command it runs, with the class default deadline.
func userAdvise(id string, on []domain.Moment, argv ...string) domain.Reaction {
	return domain.Reaction{
		ID:      id,
		Origin:  domain.OriginUser,
		Class:   domain.ClassAdvise,
		On:      on,
		Handler: domain.ArgvHandler{Argv: argv},
	}
}

// adviseArgvAgent builds an Agent rooted in ws with the given advise entries armed, a real
// workspace-scoped writer and a read tool to tell a write from anything else, and a Report sink so
// a failure's operator line can be read back. Confinement is left OFF, which is the permit row's
// unfenced arm — the fenced arm needs host capabilities and is item 4's unit test, not a property
// of this route.
func adviseArgvAgent(
	t *testing.T,
	sink *recordingSink,
	ws string,
	bypass bool,
	reported *[]string,
	reactions ...domain.Reaction,
) *Agent {
	t.Helper()

	cfg := configWithTools(sink, tools.NewWriteFile(ws), fakeTool{name: "list_dir", readOnly: true})
	cfg.WorkspaceDir = ws
	cfg.Reactions = reactions
	cfg.Bypass = bypass
	cfg.Report = func(msg string) { *reported = append(*reported, msg) }

	a, err := newAgent(cfg, echoResponder{reply: "unused"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	return a
}

// adviseArgvCall runs one finished tool call through the post-tool-result cascade and commits it,
// exactly as dispatchSerially does, and returns the committed tool message.
func adviseArgvCall(t *testing.T, a *Agent, call domain.ToolCall, result domain.ToolResult) domain.Message {
	t.Helper()

	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{call}})
	advised := a.firePostToolResult(context.Background(), call, &result)
	a.appendToolResult(0, result, advised)
	return a.conv.At(a.conv.Len() - 1)
}

// readCall is a finished read_file-shaped call that changed no file: the ordinary post-tool-result
// case, and the one every entry that did not narrow to file-changed sees.
func readCall() (domain.ToolCall, domain.ToolResult) {
	call := domain.ToolCall{ID: "c1", Tool: "list_dir", Arguments: []byte(`{"path":"."}`)}
	return call, domain.ToolResult{CallID: "c1", Content: "main.go"}
}

// skipWithoutPOSIXShell keeps the canaries off a host with no /bin/sh. The route they pin is
// platform-independent; only the scripts are not.
func skipWithoutPOSIXShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the advise route it pins is platform-independent")
	}
}

// The admission arm's own path, end to end: the entry's command prints the frozen sentence, the
// firing delivers it byte-exact as Detail, and the model reads it inside a fence apogee derived
// from provenance. The digest assertion is what ties this repo to the campaign — apogee-sim
// attributes an effect by matching it.
func TestAdviseArgvDeliversTheArmSentenceVerbatim(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	sink := &recordingSink{}
	var reported []string
	ws := t.TempDir()
	a := adviseArgvAgent(t, sink, ws, false, &reported,
		userAdvise("coach", []domain.Moment{domain.MomentPostToolResult},
			"/bin/sh", "-c", `printf '%s\n' "`+armSentence+`"`))

	call, result := readCall()
	msg := adviseArgvCall(t, a, call, result)

	fired := firedAdvice(sink)
	if len(fired) != 1 {
		t.Fatalf("saw %d firings, want 1: %+v", len(fired), fired)
	}
	if fired[0].Action != "advise" || fired[0].Detail != armSentence+"\n" {
		t.Fatalf("firing = {Action:%q Detail:%q}, want {advise, the frozen sentence and one newline}",
			fired[0].Action, fired[0].Detail)
	}
	digest := sha256.Sum256([]byte(fired[0].Detail))
	if got := hex.EncodeToString(digest[:]); got != armDigest {
		t.Errorf("sha256(Detail) = %s, want the pre-registration's pinned %s", got, armDigest)
	}

	if len(msg.Advice) != 1 {
		t.Fatalf("ledger holds %d spans, want 1: %+v", len(msg.Advice), msg.Advice)
	}
	want := domain.AdviceSpan{
		Reaction: "coach",
		Origin:   domain.OriginUser,
		Moment:   domain.MomentPostToolResult,
		Turn:     0,
		Offset:   len(result.Content),
	}
	if msg.Advice[0] != want {
		t.Errorf("span = %+v, want %+v", msg.Advice[0], want)
	}
	if got := msg.Content; got != result.Content+domain.RenderAdvice(want, armSentence+"\n") {
		t.Errorf("advised content = %q, want the result followed by the rendered fence", got)
	}
	if len(reported) != 0 {
		t.Errorf("reporter said %q, want silence on the success path", reported)
	}
}

// Standard output is the one place a configured secret can walk back INTO apogee: the input-side
// scrub keeps the value out of the child's environment, but the child can print it from anywhere
// else — its own config file, a URL it built, a literal in the operator's script. The redaction is
// what stops the model, the transcript and the firing's Detail from carrying it.
func TestAdviseArgvRedactsAConfiguredSecretFromTheTrailer(t *testing.T) {
	skipWithoutPOSIXShell(t)
	// No t.Parallel: t.Setenv.
	t.Setenv("APOGEE_TEST_ADVISE_SECRET", "hunter2-the-real-one")

	sink := &recordingSink{}
	var reported []string
	ws := t.TempDir()
	// The value is a LITERAL in the script's own text rather than read from the environment,
	// because RunHookSubprocess scrubs every configured name out of the child env — "$NAME" would
	// print an empty line and pin nothing.
	a := adviseArgvAgent(t, sink, ws, false, &reported,
		userAdvise("coach", []domain.Moment{domain.MomentPostToolResult},
			"/bin/sh", "-c", `printf '%s\n' 'token=hunter2-the-real-one'`))
	a.cfg.SecretEnvVars = []string{"APOGEE_TEST_ADVISE_SECRET"}

	call, result := readCall()
	msg := adviseArgvCall(t, a, call, result)

	if strings.Contains(msg.Content, "hunter2-the-real-one") {
		t.Errorf("advised content = %q, want the secret's value replaced", msg.Content)
	}
	if !strings.Contains(msg.Content, "token=[redacted]") {
		t.Errorf("advised content = %q, want it to carry token=[redacted]", msg.Content)
	}
	fired := firedAdvice(sink)
	if len(fired) != 1 || fired[0].Detail != "token=[redacted]\n" {
		t.Errorf("firings = %+v, want one advise firing whose Detail is already redacted", fired)
	}
}

// The file-changed narrowing is the whole reason a user would take it over post-tool-result: the
// entry hears the writes and nothing else, and it is told WHICH file — the same resolved path the
// blast-radius ladder judged the call by and the observe lane's own file-changed firing reports.
func TestAdviseArgvNarrowsToFileChanged(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	entry := func() domain.Reaction {
		return userAdvise("watcher", []domain.Moment{domain.MomentFileChanged},
			"/bin/sh", "-c", `printf '%s\n' "$APOGEE_REACTION_EVENT $APOGEE_REACTION_PATH"`)
	}

	t.Run("a successful workspace write fires it with the resolved path", func(t *testing.T) {
		t.Parallel()

		sink := &recordingSink{}
		var reported []string
		ws := t.TempDir()
		a := adviseArgvAgent(t, sink, ws, false, &reported, entry())

		call := domain.ToolCall{ID: "c1", Tool: "write_file", Arguments: []byte(`{"path":"notes.md","content":"hi"}`)}
		msg := adviseArgvCall(t, a, call, domain.ToolResult{CallID: "c1", Content: "wrote notes.md"})

		want := string(domain.MomentFileChanged) + " " + filepath.Join(realPath(t, ws), "notes.md") + "\n"
		fired := firedAdvice(sink)
		if len(fired) != 1 || fired[0].Detail != want {
			t.Fatalf("firings = %+v, want one advise firing whose Detail is %q", fired, want)
		}
		// The SPAN names the seam the trailer landed on, not the subscription that selected
		// the call: file-changed narrows which calls an entry hears, and there is exactly one
		// place a trailer can go — the closing tool result. The narrowing is said in the
		// DOCUMENT the command read, which is what the Detail above proves it saw.
		if len(msg.Advice) != 1 || msg.Advice[0].Moment != domain.MomentPostToolResult {
			t.Errorf("ledger = %+v, want one span recorded at the seam it landed on", msg.Advice)
		}
	})

	t.Run("a call that changed no file does not fire it", func(t *testing.T) {
		t.Parallel()

		sink := &recordingSink{}
		var reported []string
		a := adviseArgvAgent(t, sink, t.TempDir(), false, &reported, entry())

		call, result := readCall()
		msg := adviseArgvCall(t, a, call, result)

		if len(msg.Advice) != 0 || msg.Content != result.Content {
			t.Errorf("message = %q with %d spans, want the bare result", msg.Content, len(msg.Advice))
		}
		if fired := firedAdvice(sink); len(fired) != 0 {
			t.Errorf("firings = %+v, want none — a file-changed entry hears no read", fired)
		}
	})

	t.Run("a failed write does not fire it", func(t *testing.T) {
		t.Parallel()

		sink := &recordingSink{}
		var reported []string
		ws := t.TempDir()
		a := adviseArgvAgent(t, sink, ws, false, &reported, entry())

		call := domain.ToolCall{ID: "c1", Tool: "write_file", Arguments: []byte(`{"path":"notes.md","content":"hi"}`)}
		result := domain.ToolResult{CallID: "c1", Content: "permission denied", IsError: true}
		msg := adviseArgvCall(t, a, call, result)

		if len(msg.Advice) != 0 {
			t.Errorf("ledger = %+v, want none — a refused write changed no file", msg.Advice)
		}
	})
}

// A broken command is the user's problem, never the Turn's: the tool result stands as the tool
// wrote it, the loop goes on, and the failure is said out loud exactly twice — once to the operator
// through the Driver's report line, once to the Event stream as a "failed" firing.
func TestAdviseArgvFailsOpen(t *testing.T) {
	skipWithoutPOSIXShell(t)

	cases := []struct {
		name    string
		timeout time.Duration
		argv    []string
		detail  string
	}{
		{
			name:   "a non-zero exit",
			argv:   []string{"/bin/sh", "-c", "exit 3"},
			detail: "exited 3",
		},
		{
			name:    "a command that outlives its deadline",
			timeout: 150 * time.Millisecond,
			argv:    []string{"/bin/sh", "-c", "sleep 5"},
			detail:  "timed out",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := &recordingSink{}
			var reported []string
			entry := userAdvise("coach", []domain.Moment{domain.MomentPostToolResult}, tc.argv...)
			entry.Timeout = tc.timeout
			a := adviseArgvAgent(t, sink, t.TempDir(), false, &reported, entry)

			call, result := readCall()
			msg := adviseArgvCall(t, a, call, result)

			if len(msg.Advice) != 0 || msg.Content != result.Content {
				t.Errorf("message = %q with %d spans, want the bare result", msg.Content, len(msg.Advice))
			}
			fired := firedAdvice(sink)
			if len(fired) != 1 || fired[0].Action != "failed" {
				t.Fatalf("firings = %+v, want exactly one booked failed", fired)
			}
			if !strings.Contains(fired[0].Detail, tc.detail) {
				t.Errorf("Detail = %q, want it to name %q", fired[0].Detail, tc.detail)
			}
			if len(reported) != 1 || !strings.HasPrefix(reported[0], "reaction coach (post-tool-result): ") {
				t.Errorf("reporter said %q, want one observe-lane sentence", reported)
			}
		})
	}
}

// The class default is what an entry that set no `timeout:` runs under, and for advise that is ten
// seconds (ADR 0076 D7). It is pinned on the value rather than by a sleeping command: a literal
// ten-second canary would add ten seconds to every suite run to establish a fact the executor
// settles exactly.
func TestAdviseArgvTakesTheAdviseClassDeadline(t *testing.T) {
	t.Parallel()

	entry := userAdvise("coach", []domain.Moment{domain.MomentPostToolResult}, "/bin/sh", "-c", "true")
	if got := syncTimeout(entry); got != domain.DefaultAdviseTimeout {
		t.Errorf("syncTimeout = %s, want the advise class default %s", got, domain.DefaultAdviseTimeout)
	}
}

// Bypass switches the whole model-shaping half of the surface off (D9), and for an out-of-process
// advise entry "off" has to mean the command is never SPAWNED — not that its output is discarded.
// The side-effect file is how a test can tell those two apart at all.
func TestAdviseArgvIsSkippedUnderBypass(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	run := func(t *testing.T, bypass bool) bool {
		t.Helper()

		sink := &recordingSink{}
		var reported []string
		ws := t.TempDir()
		marker := filepath.Join(ws, "the-command-ran")
		a := adviseArgvAgent(t, sink, ws, bypass, &reported,
			userAdvise("coach", []domain.Moment{domain.MomentPostToolResult},
				"/bin/sh", "-c", `: > "`+marker+`"; printf 'advice\n'`))

		call, result := readCall()
		adviseArgvCall(t, a, call, result)

		if _, err := os.Stat(marker); err == nil {
			return true
		}
		return false
	}

	t.Run("Bypass off spawns it", func(t *testing.T) {
		t.Parallel()
		if !run(t, false) {
			t.Error("the command did not run with Bypass off — the canary pins nothing")
		}
	})
	t.Run("Bypass on never spawns it", func(t *testing.T) {
		t.Parallel()
		if run(t, true) {
			t.Error("the command ran under Bypass; advise must be off before a handler is reached")
		}
	})
}
