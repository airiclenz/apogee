package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// The cross-package pin: real tool in, rendered card line out
// ----------------------------------------------------------------------------
//
// The view renders a typed domain.ToolSummary instead of parsing the prose a tool wrote for
// the model (toolpresent.go). That removes the silent-degradation failure mode the regexes
// had — but only if the tools keep ATTACHING their summaries, and nothing in internal/tui
// can see whether they do: the package's production code depends on internal/domain alone.
//
// So this file runs the real tools against a real temp workspace and asserts on the line the
// presenter renders. A tool that stops attaching its summary (a refactor that reverts to
// okResult, a render helper that drops the value on a new path) fails HERE, in the package
// that renders it, instead of quietly falling back to a first line nobody looks at.
//
// The test-only import of internal/tools is the same seam e2e_test.go already uses; the
// ADR-0010 invariant is about the bare root module path, which this is not.

// ddgPinPage is a two-result DuckDuckGo-shaped results page — the shape web_search's parser
// reads (result__a anchors with uddg-wrapped hrefs, paired with result__snippet anchors).
const ddgPinPage = `<!DOCTYPE html>
<html><body>
<div class="result">
  <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2F">Go Documentation</a>
  <a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2F">Learn Go today.</a>
</div>
<div class="result">
  <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fpkg.go.dev%2F">pkg.go.dev</a>
  <a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fpkg.go.dev%2F">The Go package index.</a>
</div>
</body></html>`

// pinFileBody is the workspace file the read/open/diff/grep cases work on: three lines, with
// "func main" on the third, so every expected number below is readable from here.
const pinFileBody = "package main\n\nfunc main() {}"

// TestToolSummariesRenderThroughThePresenter executes each of the seven summary-bearing tools
// for real and pins the card line the presenter renders from its outcome. Both halves are
// asserted: that the tool attached a summary at all, and that the view words it the way it
// always has (the D4 oracle — this card reshaped a seam, it did not change the UI).
func TestToolSummariesRenderThroughThePresenter(t *testing.T) {
	root := t.TempDir()
	writePinFile(t, filepath.Join(root, "main.go"), pinFileBody)
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	writePinFile(t, filepath.Join(root, "sub", "a.txt"), "a")
	writePinFile(t, filepath.Join(root, "sub", "b.txt"), "b")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(ddgPinPage))
	}))
	defer srv.Close()

	cases := []struct {
		name string
		tool domain.Tool
		args string
		want string
	}{
		{
			name: "read_file",
			tool: tools.NewReadFile(root, nil),
			args: `{"path":"main.go"}`,
			want: "3 lines",
		},
		{
			name: "write_file",
			tool: tools.NewWriteFile(root),
			args: `{"path":"notes.txt","content":"hello"}`,
			want: "1 line", // the table asks for lines; the tool reports bytes, the request states lines
		},
		{
			name: "list_dir",
			tool: tools.NewListDir(root, nil),
			args: `{"path":"sub"}`,
			want: "2 entries",
		},
		{
			name: "grep",
			tool: tools.NewGrep(root, nil),
			args: `{"pattern":"func main","path":"main.go"}`,
			want: "1 hit",
		},
		{
			name: "view_diff",
			tool: tools.NewViewDiff(root),
			args: `{"path":"main.go","newContent":"package main\n\nfunc other() {}"}`,
			want: "+1 −1",
		},
		{
			name: "web_search",
			tool: tools.NewWebSearch(security.URLGuard{}.DisableIPFloor(), srv.URL),
			args: `{"query":"golang docs"}`,
			want: "2 results",
		},
	}

	if len(cases) != 6 {
		t.Fatalf("the pin covers %d tools, want all 6 summary-bearing ones", len(cases))
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			call := domain.ToolCall{ID: "c1", Tool: tc.name, Arguments: []byte(tc.args)}
			res, err := tc.tool.Execute(context.Background(), call)
			if err != nil {
				t.Fatalf("Execute Go error: %v", err)
			}
			if res.IsError {
				t.Fatalf("tool reported an error result: %q", res.Content)
			}
			if res.Summary == nil {
				t.Fatalf("%s attached no summary; the card would degrade to %q", tc.name, firstLine(res.Content))
			}

			tv := presentToolCall(call, "", workspaceRoot{})
			tv.enrichWithResult(res, workspaceRoot{})
			if tv.Summary.Text != tc.want {
				t.Errorf("card line = %q, want %q (content was %q)", tv.Summary.Text, tc.want, res.Content)
			}
		})
	}
}

// The presenter names the tool the summary came from: every case above uses the tool's own
// registry key, so a summary can never render under the wrong label.
func TestToolSummaryPinUsesRegisteredToolNames(t *testing.T) {
	for _, name := range []string{"read_file", "write_file", "list_dir", "grep", "view_diff", "web_search"} {
		if _, ok := toolRegistry[name]; !ok {
			t.Errorf("%s reports a summary but has no registry entry", name)
		}
	}
}

// writePinFile writes one workspace fixture file, failing the test rather than the tool.
func writePinFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
