package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// readFileIgnoredNote is the exact line read_file's result gains when called with the undeclared
// key "line": the ignored key, then the declared ones in schema order.
const readFileIgnoredNote = `note: ignored unknown argument "line" — read_file takes path, start_line, end_line, max_lines, locate`

// ignoredArgsFixture is a workspace holding one small file, and the bare-loop configuration
// (Bypass as asked) an Agent reads it under.
func ignoredArgsFixture(t *testing.T, bypass bool) domain.Config {
	t.Helper()

	workspace := realTempDir(t)
	if err := os.WriteFile(filepath.Join(workspace, "notes.txt"), []byte("alpha\nbeta\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	cfg := baseConfig(&recordingSink{})
	cfg.WorkspaceDir = workspace
	cfg.Bypass = bypass
	return cfg
}

// toolMessageSent returns the content of the tool-result message answering callID in the last
// request upstream received — what the model actually read back for that call.
func toolMessageSent(t *testing.T, upstream *scriptedUpstream, callID string) string {
	t.Helper()
	for _, msg := range upstream.last().Messages {
		if msg.Role == "tool" && msg.ToolCallID == callID {
			return msg.Content
		}
	}
	t.Fatalf("the next request carried no tool result for %s", callID)
	return ""
}

// readFileResult is the content read_file returns for args when executed directly, outside the
// loop — the bare result the engine must hand the model unchanged for a declared-only call.
func readFileResult(t *testing.T, a *Agent, args string) string {
	t.Helper()
	tool, ok := a.lookupTool("read_file")
	if !ok {
		t.Fatal("read_file is not on the Agent's roster")
	}
	res, err := tool.Execute(context.Background(), domain.ToolCall{ID: "direct", Tool: "read_file", Arguments: json.RawMessage(args)})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	return res.Content
}

// runReadFileCall drives one Exchange in which the model calls read_file with args and then
// stops, returning the tool message the model received for that call.
func runReadFileCall(t *testing.T, cfg domain.Config, args string) (*Agent, string) {
	t.Helper()
	upstream := scriptedResponder(t, toolCallTurn("c1", "read_file", args), contentTurn("done"))
	a := newProfileAgent(t, cfg, upstream)
	runExchange(t, a, "read notes.txt")
	return a, toolMessageSent(t, upstream, "c1")
}

// assertIgnoredArgumentNote checks the tool message still carries the file's content and ends
// with the one note line naming the ignored key.
func assertIgnoredArgumentNote(t *testing.T, sent string) {
	t.Helper()
	if !strings.Contains(sent, "alpha") || !strings.Contains(sent, "beta") {
		t.Errorf("tool message lost the file content: %q", sent)
	}
	if !strings.HasSuffix(sent, "\n"+readFileIgnoredNote) {
		t.Errorf("tool message = %q, want it to end with the line %q", sent, readFileIgnoredNote)
	}
	if got := strings.Count(sent, "note: ignored"); got != 1 {
		t.Errorf("tool message carries %d ignored-argument notes, want exactly 1: %q", got, sent)
	}
}

// TestIgnoredArgumentNoteReachesModel: a built-in called with a key its schema does not declare
// still runs, and the result the model reads next ends with the line naming that key.
func TestIgnoredArgumentNoteReachesModel(t *testing.T) {
	t.Parallel()

	_, sent := runReadFileCall(t, ignoredArgsFixture(t, false), `{"path":"notes.txt","line":2}`)
	assertIgnoredArgumentNote(t, sent)
}

// TestIgnoredArgumentNoteUnderBypass: the note is a structural tool-result fact, not a
// model-shaping Reaction, so Bypass leaves it on.
func TestIgnoredArgumentNoteUnderBypass(t *testing.T) {
	t.Parallel()

	_, sent := runReadFileCall(t, ignoredArgsFixture(t, true), `{"path":"notes.txt","line":2}`)
	assertIgnoredArgumentNote(t, sent)
}

// TestDeclaredArgumentsResultUnchanged: a call naming only declared keys reaches the model
// byte-identical to the tool's own result — no note line, nothing else added.
func TestDeclaredArgumentsResultUnchanged(t *testing.T) {
	t.Parallel()

	const args = `{"path":"notes.txt","start_line":1}`
	a, sent := runReadFileCall(t, ignoredArgsFixture(t, false), args)
	if want := readFileResult(t, a, args); sent != want {
		t.Errorf("tool message = %q, want read_file's own result %q", sent, want)
	}
	if strings.Contains(sent, "note: ignored") {
		t.Errorf("a declared-only call gained an ignored-argument note: %q", sent)
	}
}
