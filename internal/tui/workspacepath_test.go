package tui

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestWorkspaceRootShorten pins the four cases the rule is made of (layout.md, "The rules behind
// the tool-call sketch"): a path inside the workspace prints relative to it with no leading `./`
// and no leading separator, the root itself prints `.`, a path OUTSIDE the workspace keeps its
// absolute form, and a path that arrived relative is passed through untouched.
//
// The rows past those four are the boundary rules, and they are the ones that would break
// silently: a sibling directory whose name merely opens with the root's spelling must survive
// whole, a longer path that merely ENDS in the root's spelling is a different directory, and
// ordinary prose carrying a slash is not a path at all.
func TestWorkspaceRootShorten(t *testing.T) {
	t.Parallel()

	ws := newWorkspaceRoot("/home/me/proj")
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"a path inside the workspace loses the root", "/home/me/proj/docs/plan.md", "docs/plan.md"},
		{"a file at the top of the workspace keeps no separator", "/home/me/proj/main.go", "main.go"},
		{"the workspace root itself reads as here", "/home/me/proj", "."},
		{"a trailing separator is still the root", "/home/me/proj/", "."},
		{"a repeated separator is one separator", "/home/me/proj//docs/plan.md", "docs/plan.md"},
		{"a path outside the workspace stays absolute", "/etc/passwd", "/etc/passwd"},
		{"a path in another project stays absolute", "/home/me/other/main.go", "/home/me/other/main.go"},
		{"an already-relative path is untouched", "docs/plan.md", "docs/plan.md"},
		{"a bare filename is untouched", "main.go", "main.go"},
		{"a sibling opening with the root's spelling stays whole", "/home/me/proj-old/main.go", "/home/me/proj-old/main.go"},
		{"a longer path ending in the root's spelling stays whole", "/mnt/backup/home/me/proj/main.go", "/mnt/backup/home/me/proj/main.go"},
		{"the root mid-sentence is shortened in place", "replaced text in /home/me/proj/main.go", "replaced text in main.go"},
		{"a quoted argument is shortened inside its quotes", `"path": "/home/me/proj/docs/x.md"`, `"path": "docs/x.md"`},
		{"a command shortens each of its paths", "cp /home/me/proj/a.go /home/me/proj/b.go", "cp a.go b.go"},
		{"a command shortens only what is inside", "cp /home/me/proj/a.go /tmp/b.go", "cp a.go /tmp/b.go"},
		{"prose with a slash is not a path", "3/4 of the way through, see also http://example.com/x", "3/4 of the way through, see also http://example.com/x"},
		{"an empty line stays empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ws.shorten(tc.in); got != tc.want {
				t.Errorf("shorten(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A root that cannot be shortened against leaves every line exactly as it came. The zero value is
// the one every hand-built test view carries, so this is also what pins the rest of the package's
// expectations to the unshortened paint; a filesystem root is refused because it would make every
// absolute path on the machine "inside the workspace".
func TestWorkspaceRootWithoutARootShortensNothing(t *testing.T) {
	t.Parallel()

	const line = "/home/me/proj/docs/plan.md"
	for _, tc := range []struct {
		name string
		ws   workspaceRoot
	}{
		{"the zero value", workspaceRoot{}},
		{"an unwired workspace", newWorkspaceRoot("")},
		{"a relative workspace", newWorkspaceRoot("proj")},
		{"the filesystem root", newWorkspaceRoot("/")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.ws.shorten(line); got != line {
				t.Errorf("shorten(%q) = %q, want it untouched", line, got)
			}
		})
	}
}

// A workspace path is cleaned once, at construction, so a trailing separator or a `..` segment in
// the wiring shortens exactly as the canonical spelling does.
func TestWorkspaceRootIsCleanedOnce(t *testing.T) {
	t.Parallel()

	for _, spelling := range []string{"/home/me/proj", "/home/me/proj/", "/home/me/./proj", "/home/me/other/../proj"} {
		if got := newWorkspaceRoot(spelling).shorten("/home/me/proj/main.go"); got != "main.go" {
			t.Errorf("workspace %q shortened to %q, want %q", spelling, got, "main.go")
		}
	}
}

// The shortening reaches the two halves of a tool card that NAME a path: the target that leads the
// branch line, and the one-line summary of the outcome beside it. The call's own arguments are the
// oracle — what the model asked for is untouched, only its spelling on screen changes.
func TestToolCardPaintsWorkspaceRelativePaths(t *testing.T) {
	t.Parallel()

	ws := newWorkspaceRoot("/home/me/proj")
	call := domain.ToolCall{ID: "1", Tool: "terminal", Arguments: []byte(`{"command":"ls /home/me/proj/docs"}`)}
	tv := presentToolCall(call, ws)
	if tv.Target != "ls docs" {
		t.Errorf("target = %q, want %q", tv.Target, "ls docs")
	}
	if !strings.Contains(string(call.Arguments), "/home/me/proj/docs") {
		t.Errorf("the call's own arguments were rewritten: %s", call.Arguments)
	}

	summarised := presentToolCall(domain.ToolCall{ID: "2", Tool: "single_find_and_replace",
		Arguments: []byte(`{"path":"/home/me/proj/main.go"}`)}, ws)
	summarised.enrichWithResult(domain.ToolResult{CallID: "2", Content: "replaced text in /home/me/proj/main.go"}, ws)
	if summarised.Target != "main.go" {
		t.Errorf("target = %q, want %q", summarised.Target, "main.go")
	}
	if summarised.Summary.Text != "replaced text in main.go" {
		t.Errorf("summary = %q, want %q", summarised.Summary.Text, "replaced text in main.go")
	}
}

// A block's BODY is text it QUOTES, not a path it names, so the shortening stops at it. The case
// that makes this a rule rather than a preference is the write a human is approving: a diff hunk
// and an edit's replacement string are verbatim file content, and an in-workspace absolute path
// inside that content is content too — displayed shortened, the block would show a spelling the
// file will not actually contain. The same block's target still shortens, because that IS a path
// the block names.
func TestToolCardBodyKeepsTheSpellingOfWhatItQuotes(t *testing.T) {
	t.Parallel()

	ws := newWorkspaceRoot("/home/me/proj")
	cases := []struct {
		name string
		call domain.ToolCall
		res  domain.ToolResult
		want []string // the body, line for line, exactly as the tool wrote it
	}{
		{
			name: "a diff hunk quoting a workspace path",
			call: domain.ToolCall{ID: "1", Tool: "view_diff", Arguments: []byte(`{"path":"/home/me/proj/main.go"}`)},
			res: domain.ToolResult{CallID: "1", Content: strings.Join([]string{
				`  cfg, err := load(`,
				`- 	"/home/me/proj/config/dev.yaml")`,
				`+ 	"/home/me/proj/config/prod.yaml")`,
			}, "\n"), Summary: domain.DiffStat{Added: 1, Removed: 1}},
			want: []string{
				`  cfg, err := load(`,
				`- 	"/home/me/proj/config/dev.yaml")`,
				`+ 	"/home/me/proj/config/prod.yaml")`,
			},
		},
		{
			name: "a command's output, which may itself be file content",
			call: domain.ToolCall{ID: "2", Tool: "terminal", Arguments: []byte(`{"command":"cat /home/me/proj/paths.txt"}`)},
			res: domain.ToolResult{CallID: "2", Content: strings.Join([]string{
				"/home/me/proj/docs/plan.md",
				"/home/me/proj/docs/notes.md",
				"/etc/hosts",
			}, "\n")},
			want: []string{"/home/me/proj/docs/plan.md", "/home/me/proj/docs/notes.md", "/etc/hosts"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tv := presentToolCall(tc.call, ws)
			tv.enrichWithResult(tc.res, ws)

			body := make([]string, 0, tv.Details.len())
			for _, d := range tv.Details.all() {
				body = append(body, d.Text)
			}
			if strings.Join(body, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("body = %q, want it verbatim: %q", body, tc.want)
			}
		})
	}

	// The target of the diff above is the one path that block names, and it shortens.
	diff := presentToolCall(cases[0].call, ws)
	if diff.Target != "main.go" {
		t.Errorf("target = %q, want %q — a named path still shortens beside a verbatim body", diff.Target, "main.go")
	}
}

// An unregistered tool's arguments are the model's request quoted verbatim — the approval surface's
// whole point — so they are shown exactly as they were sent, workspace paths included. What is
// equally untouched is the tool's own id: it is a name, not a path.
func TestUnregisteredToolCardQuotesItsArgumentsVerbatim(t *testing.T) {
	t.Parallel()

	ws := newWorkspaceRoot("/home/me/proj")
	tv := presentToolCall(domain.ToolCall{ID: "1", Tool: "mcp_thing",
		Arguments: []byte(`{"path":"/home/me/proj/docs/plan.md","other":"/etc/hosts"}`)}, ws)
	if tv.Label != "mcp_thing" || tv.Verb != "running mcp_thing" {
		t.Errorf("view = %+v; want the raw-name fallback", tv)
	}
	got := detailsText(tv)
	if !strings.Contains(got, `"/home/me/proj/docs/plan.md"`) {
		t.Errorf("arguments body = %q; want the argument quoted exactly as it was sent", got)
	}
	if !strings.Contains(got, `"/etc/hosts"`) {
		t.Errorf("arguments body = %q; want the outside path left absolute", got)
	}
}

// The status line words a call from the same view the block does (toolActivityLabel →
// presentToolCall), so the phrase beside the spinner and the branch line beneath it can never
// spell one path two ways.
func TestActivityLabelSharesTheWorkspaceRelativePath(t *testing.T) {
	t.Parallel()

	ws := newWorkspaceRoot("/home/me/proj")
	got := toolActivityLabel(domain.ToolCall{Tool: "read_file",
		Arguments: []byte(`{"path":"/home/me/proj/docs/plan.md"}`)}, ws)
	if want := "reading · docs/plan.md"; got != want {
		t.Errorf("activity label = %q, want %q", got, want)
	}
}

// The transcript resolves the root once and keeps it across a session reset: /clear opens a new
// conversation, not a new workspace.
func TestTranscriptShortensPathsAndKeepsItsRootAcrossReset(t *testing.T) {
	t.Parallel()

	var tr transcript
	tr.ws = newWorkspaceRoot("/home/me/proj")
	tr.addToolCall(domain.ToolCall{ID: "1", Tool: "read_file",
		Arguments: []byte(`{"path":"/home/me/proj/main.go"}`)}, 0)
	if got := tr.entries[0].tool.Target; got != "main.go" {
		t.Fatalf("target = %q, want %q", got, "main.go")
	}

	tr.reset()
	tr.addToolCall(domain.ToolCall{ID: "2", Tool: "read_file",
		Arguments: []byte(`{"path":"/home/me/proj/docs/plan.md"}`)}, 0)
	if got := tr.entries[0].tool.Target; got != "docs/plan.md" {
		t.Errorf("after reset the target = %q, want %q — the root did not survive /clear", got, "docs/plan.md")
	}
}
