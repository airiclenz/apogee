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
