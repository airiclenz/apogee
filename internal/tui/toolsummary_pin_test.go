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
// the model (toolregistry.go). That removes the silent-degradation failure mode the regexes
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
// "func main" on the third, so every expected number below is readable from here. The three
// edit cases each get their OWN copy of it, because they write: one case's apply must not move
// the numbers another case reads.
const pinFileBody = "package main\n\nfunc main() {}"

// TestToolSummariesRenderThroughThePresenter executes for real each summary-bearing tool that
// needs nothing but a temp workspace — nine of the ten — and pins the card line the presenter
// renders from its outcome. Both halves are asserted: that the tool attached a summary at all,
// and that the view words it the way it always has (the D4 oracle — this card reshaped a seam,
// it did not change the UI).
//
// The tenth is git_status, whose outcome needs a real repository and so a git binary this
// package's tests otherwise never reach for; its domain.ChangedFiles is pinned against a live
// repo where the tool lives (internal/tools/git_test.go) and its rendering here by
// TestChangedFilesStatReadsTheTypedCountsNotTheProse and
// TestGitStatusReportSurvivesItsTypedSummary (toolpresent_test.go).
func TestToolSummariesRenderThroughThePresenter(t *testing.T) {
	root := t.TempDir()
	writePinFile(t, filepath.Join(root, "main.go"), pinFileBody)
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	writePinFile(t, filepath.Join(root, "sub", "a.txt"), "a")
	writePinFile(t, filepath.Join(root, "sub", "b.txt"), "b")
	writePinFile(t, filepath.Join(root, "single.go"), pinFileBody)
	writePinFile(t, filepath.Join(root, "multi.go"), pinFileBody)
	writePinFile(t, filepath.Join(root, "patch.go"), pinFileBody)

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
		{
			// The three edit tools word their slot from the regions the APPLY recorded
			// (editRegionsStat), not from the arguments — so these three rows are also the
			// pin that the tools keep attaching domain.EditRegions at all.
			name: "single_find_and_replace",
			tool: tools.NewSingleFindReplace(root),
			args: `{"path":"single.go","oldText":"func main","newText":"func other"}`,
			want: "+1 −1",
		},
		{
			name: "multi_find_and_replace",
			tool: tools.NewMultiFindReplace(root),
			args: `{"path":"multi.go","replacements":[{"oldText":"package main","newText":"package other"},{"oldText":"func main","newText":"func other"}]}`,
			want: "+2 −2", // the regions, not the argument-derived "2 changes"
		},
		{
			name: "edit_existing_file",
			tool: tools.NewEditExistingFile(root),
			args: `{"path":"patch.go","content":"package main\n\nfunc other() {}"}`,
			want: "+1 −1", // a full replacement reports only the line that differs
		},
	}

	if len(cases) != 9 {
		t.Fatalf("the pin covers %d tools, want the 9 summary-bearing ones that need no git binary", len(cases))
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
// registry key, so a summary can never render under the wrong label. git_status rides this list
// too — the one summary-bearing tool the pin above cannot execute still has to be spelled the way
// the registry keys it.
func TestToolSummaryPinUsesRegisteredToolNames(t *testing.T) {
	for _, name := range []string{
		"read_file", "write_file", "list_dir", "grep", "view_diff", "web_search",
		"single_find_and_replace", "multi_find_and_replace", "edit_existing_file",
		"git_status",
	} {
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
