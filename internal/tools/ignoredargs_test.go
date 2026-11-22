package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/workflow"
)

// readFileIgnoredLine is the note the issue (undeclared-args-ignored) asks read_file to carry for
// an invented `line` key; the declared list is read_file's live schema, in schema order.
const readFileIgnoredLine = `note: ignored unknown argument "line" — read_file takes path, start_line, end_line, max_lines, locate`

// undeclaredFakeTool is a tool from outside this package's toolSpec family — an embedder's or an
// MCP server's stand-in — that declares one property and ignores everything it is sent.
type undeclaredFakeTool struct{}

func (undeclaredFakeTool) Name() string        { return "fake" }
func (undeclaredFakeTool) Description() string { return "a tool that is not a built-in" }
func (undeclaredFakeTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}}}`)
}
func (undeclaredFakeTool) Execute(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	return domain.OKResult(call.ID, "ran"), nil
}

func TestNoteIgnoredArguments(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	readFile := func(*testing.T) domain.Tool { return NewReadFile(root, ReadMounts{}) }

	cases := []struct {
		name      string
		tool      func(*testing.T) domain.Tool
		arguments string
		// wantNote is the line the result must end with; empty means the result is unchanged.
		wantNote string
		// wantError is the IsError the bare Execute result carries, checked so a row exercises
		// the path it claims to.
		wantError bool
	}{
		{
			name:      "an invented key is named after a read that still ran",
			tool:      readFile,
			arguments: `{"path":"a.txt","line":3}`,
			wantNote:  readFileIgnoredLine,
		},
		{
			name:      "a declared-only call is unchanged",
			tool:      readFile,
			arguments: `{"path":"a.txt","start_line":1,"end_line":2}`,
		},
		{
			name:      "two invented keys are named in wire order",
			tool:      readFile,
			arguments: `{"path":"a.txt","zeta":1,"alpha":2}`,
			wantNote:  `note: ignored unknown arguments "zeta", "alpha" — read_file takes path, start_line, end_line, max_lines, locate`,
		},
		{
			name:      "a key repeated under one spelling is named once",
			tool:      readFile,
			arguments: `{"path":"a.txt","line":3,"line":3}`,
			wantNote:  readFileIgnoredLine,
		},
		{
			name:      "a declared key in another case is not named",
			tool:      readFile,
			arguments: `{"Path":"a.txt"}`,
		},
		{
			name:      "empty arguments are unchanged",
			tool:      readFile,
			arguments: ``,
			wantError: true,
		},
		{
			name:      "arguments that are not an object are unchanged",
			tool:      readFile,
			arguments: `[1]`,
			wantError: true,
		},
		{
			name:      "a decode failure still carries the note",
			tool:      readFile,
			arguments: `{"path":5,"line":3}`,
			wantNote:  readFileIgnoredLine,
			wantError: true,
		},
		{
			name:      "a tool declaring nothing takes no arguments",
			tool:      func(t *testing.T) domain.Tool { return NewGitStatus(gitRepo(t)) },
			arguments: `{"verbose":true}`,
			wantNote:  `note: ignored unknown argument "verbose" — git_status takes no arguments`,
		},
		{
			name: "finish refuses its own undeclared fields and is unchanged",
			tool: func(*testing.T) domain.Tool {
				return NewFinish(workflow.ReceiptSpec{"count": "int"}, nil)
			},
			arguments: `{"status":"partial","summary":"s","count":1,"bogus":1}`,
			wantError: true,
		},
		{
			name:      "a tool outside the built-ins is unchanged",
			tool:      func(*testing.T) domain.Tool { return undeclaredFakeTool{} },
			arguments: `{"b":1}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tool := tc.tool(t)
			call := domain.ToolCall{ID: "c1", Tool: tool.Name(), Arguments: json.RawMessage(tc.arguments)}
			bare, err := tool.Execute(context.Background(), call)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if bare.IsError != tc.wantError {
				t.Fatalf("bare IsError = %v, want %v (content %q)", bare.IsError, tc.wantError, bare.Content)
			}

			got := NoteIgnoredArguments(tool, call, bare)

			assertIgnoredArgumentsResult(t, bare, got, tc.wantNote)
		})
	}
}

// assertIgnoredArgumentsResult checks got against the bare Execute result: equal in every field
// when wantNote is empty, and otherwise the bare content followed by exactly one line break and
// wantNote, every other field untouched.
func assertIgnoredArgumentsResult(t *testing.T, bare, got domain.ToolResult, wantNote string) {
	t.Helper()

	if wantNote == "" {
		if !reflect.DeepEqual(got, bare) {
			t.Fatalf("result changed:\n got %#v\nwant %#v", got, bare)
		}
		return
	}

	separator := "\n"
	if bare.Content == "" || strings.HasSuffix(bare.Content, "\n") {
		separator = ""
	}
	if want := bare.Content + separator + wantNote; got.Content != want {
		t.Fatalf("content:\n got %q\nwant %q", got.Content, want)
	}
	rest := got
	rest.Content = bare.Content
	if !reflect.DeepEqual(rest, bare) {
		t.Fatalf("fields beside Content changed:\n got %#v\nwant %#v", rest, bare)
	}
}
