package mechanisms

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/domain/domaintest"
)

// A source file a coding agent reads is full of error-handling strings, and a successful
// read_file's result content IS that file — so the read-error question can only be answered by
// the committed marker, never by the text. This table pins the three-way contract:
// marked-failed → failed, marked-succeeded → succeeded whatever the body says, and unmarked
// (a legacy record) → the anchored first-line sniff.
func TestResultIsReadErrorPrefersTheCommittedMarker(t *testing.T) {
	t.Parallel()

	// The audit's payload: a real Go file whose body carries every read-error signal there is.
	fileBody := strings.Join([]string{
		"[File: store.go, 4 lines total, showing lines 1-4]",
		"// Load reports an error: the record does not exist.",
		"func Load(id string) error {",
		`	return fmt.Errorf("session %q not found: no such file", id)`,
		"}",
	}, "\n")

	cases := []struct {
		name   string
		result domain.Message
		want   bool
	}{
		{
			name:   "marked failed, message says so too",
			result: domaintest.FailedToolResultMessage("c1", "Error: no such file: store.go"),
			want:   true,
		},
		{
			name:   "marked succeeded, body full of error signals",
			result: domaintest.SucceededToolResultMessage("c1", fileBody),
			want:   false,
		},
		{
			name:   "marked failed, message carries no signal at all",
			result: domaintest.FailedToolResultMessage("c1", "gone.go"),
			want:   true,
		},
		{
			name:   "legacy record, failure text on the first line",
			result: domaintest.ToolResultMessage("c1", "Error: no such file: gone.go"),
			want:   true,
		},
		{
			name:   "legacy record, signals only in the body",
			result: domaintest.ToolResultMessage("c1", fileBody),
			want:   false,
		},
		{
			name:   "legacy record, blank lead then the failure text",
			result: domaintest.ToolResultMessage("c1", "\n\nfile not found: gone.go"),
			want:   true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			msgs := []domain.Message{
				domaintest.UserMessage("look at store.go"),
				domaintest.AssistantCallsMessage(domaintest.ReadCall("c1", "store.go")),
				c.result,
			}
			if got := resultIsReadError(scanView(msgs), "c1"); got != c.want {
				t.Errorf("resultIsReadError = %v, want %v", got, c.want)
			}
		})
	}

	// A call with no committed result yet is not a failure — the conservative contract the
	// marker does not change.
	inflight := []domain.Message{
		domaintest.UserMessage("look at store.go"),
		domaintest.AssistantCallsMessage(domaintest.ReadCall("c1", "store.go")),
	}
	if resultIsReadError(scanView(inflight), "c1") {
		t.Error("a call still in flight must not read as a failed read")
	}
}

// The audit's end-to-end repro: the model successfully reads one existing file whose body
// mentions an error, and read_loop used to answer that the workspace was empty and the file
// should be written from scratch — steering the model to overwrite real source with a
// hallucinated reconstruction. With the marker committed, the read is a read.
func TestReadLoopIgnoresErrorTextInASuccessfulReadBody(t *testing.T) {
	t.Parallel()
	body := "[File: store.go, 2 lines total, showing lines 1-2]\n" +
		`return fmt.Errorf("error: %q does not exist", id)`

	msgs := []domain.Message{
		domaintest.UserMessage("fix the loader in `store.go`"),
		domaintest.AssistantCallsMessage(domaintest.ReadCall("r1", "store.go")),
		domaintest.SucceededToolResultMessage("r1", body),
	}
	if isGreenfieldContext(scanView(msgs)) {
		t.Error("a successful read must clear the greenfield signal, whatever the file says")
	}
	if fired, hint := fireReadLoop(t, msgs); fired {
		t.Errorf("read_loop fired on a successful read: %q", hint)
	}

	// Two more reads of the same file (three in all, none acted on) is a genuine loop, so the
	// successful-read branch must still fire — the fix silences the false alarm, not the mechanism.
	msgs = append(msgs,
		domaintest.AssistantCallsMessage(domaintest.ReadCall("r2", "store.go")),
		domaintest.SucceededToolResultMessage("r2", body),
		domaintest.AssistantCallsMessage(domaintest.ReadCall("r3", "store.go")),
		domaintest.SucceededToolResultMessage("r3", body),
	)
	fired, hint := fireReadLoop(t, msgs)
	if !fired {
		t.Fatal("three unacted successful re-reads should still fire the successful-read-loop hint")
	}
	if !strings.Contains(hint, "without making changes") {
		t.Errorf("hint = %q, want the re-read wording", hint)
	}
}

// A genuinely failed read is recognised from the marker alone: the built-in tools happen to
// phrase their failures in words the legacy sniff also catches, so the marker path is only
// provable with a failure message that carries no signal at all.
func TestReadLoopFiresOnAMarkedFailureWithNoErrorWording(t *testing.T) {
	t.Parallel()
	msgs := []domain.Message{
		domaintest.UserMessage("create `app.go`"),
		domaintest.AssistantCallsMessage(domaintest.ReadCall("r1", "app.go")),
		domaintest.FailedToolResultMessage("r1", "app.go"),
	}
	fired, hint := fireReadLoop(t, msgs)
	if !fired {
		t.Fatal("a marked-failed read on an empty workspace should fire the greenfield hint")
	}
	if !strings.Contains(hint, "workspace is empty") || !strings.Contains(hint, "app.go") {
		t.Errorf("greenfield hint = %q, want it to name the empty workspace and app.go", hint)
	}
}

// A session resumed from a snapshot written before the marker existed carries unmarked tool
// results, and must still detect the loop it always did — via the anchored sniff.
func TestReadLoopLegacyRecordFallsBackToTheAnchoredSniff(t *testing.T) {
	t.Parallel()
	msgs := []domain.Message{
		domaintest.UserMessage("create `app.go`"),
		domaintest.AssistantCallsMessage(domaintest.ReadCall("r1", "app.go")),
		domaintest.ToolResultMessage("r1", "Error: no such file: app.go"),
	}
	fired, hint := fireReadLoop(t, msgs)
	if !fired {
		t.Fatal("a legacy unmarked failure should still fire the greenfield hint")
	}
	if !strings.Contains(hint, "app.go") {
		t.Errorf("greenfield hint = %q, want it to name app.go", hint)
	}
}
