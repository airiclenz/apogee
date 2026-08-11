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

// The footer's own respelling of the workspace is the `~` substitution and nothing else, and the
// row that makes it a rule rather than a string trim is the sibling: `/home/me-other/proj` merely
// OPENS with home's spelling, and abbreviating it would name a directory the session is not in.
// The degenerate inputs are pinned beside it because the footer's segment is dropped on "" and the
// home lookup is allowed to fail (newModel passes "" when it does).
func TestWorkdirDisplay(t *testing.T) {
	t.Parallel()

	const home = "/home/me"
	cases := []struct {
		name string
		path string
		home string
		want string
	}{
		{"a path under home is abbreviated", "/home/me/repos/apogee", home, "~/repos/apogee"},
		{"a child of home is abbreviated", "/home/me/proj", home, "~/proj"},
		{"home itself reads as the tilde alone", "/home/me", home, "~"},
		{"a trailing separator is still home", "/home/me/", home, "~"},
		{"a sibling opening with home's spelling stays whole", "/home/me-other/proj", home, "/home/me-other/proj"},
		{"a sibling of home stays whole", "/home/you/proj", home, "/home/you/proj"},
		{"a path outside home stays whole", "/srv/build/apogee", home, "/srv/build/apogee"},
		{"a path is cleaned on the way to the screen", "/home/me/repos/../proj/", home, "~/proj"},
		{"an unknown home leaves the path alone", "/home/me/proj", "", "/home/me/proj"},
		{"no workspace names nothing", "", home, ""},
		{"blank workspace names nothing", "   ", home, ""},
		{"a relative workspace is passed through cleaned", "./proj", home, "proj"},
		{"a root home abbreviates the whole machine", "/proj", "/", "~/proj"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := workdirDisplay(tc.path, tc.home); got != tc.want {
				t.Errorf("workdirDisplay(%q, %q) = %q, want %q", tc.path, tc.home, got, tc.want)
			}
		})
	}
}

// The shortening reaches the two halves of a tool card that NAME a path: the target that leads the
// branch row, and the one-line summary of the outcome standing in that row's outcome slot. The
// call's own arguments are the oracle — what the model asked for is untouched, only its spelling on
// screen changes.
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

// The SUMMARY slot is not always a phrase the block wrote. Output that came to exactly one line is
// PROMOTED into it — the line takes the outcome slot at the branch row's right edge instead of
// hanging beneath it — and promotion moves where the text sits, never whose text it is: `cat` on a
// one-line file puts that file's content in the slot, so an in-workspace absolute path in it must
// reach the screen with the spelling the file holds, exactly as it would one row lower in a body.
//
// The other rows are the contrast the rule needs: a sentence the tool reported about a path it
// acted on, and the typed stat the registry words for a run that printed nothing, are the block's
// words and still shorten. That is why the slot cannot be judged by its text — the mark travels with the line
// (branchSummary), set where the line was put there.
func TestToolCardSummaryQuotesPromotedOutputOnly(t *testing.T) {
	t.Parallel()

	ws := newWorkspaceRoot("/home/me/proj")
	cases := []struct {
		name        string
		call        domain.ToolCall
		res         domain.ToolResult
		wantTarget  string
		wantSummary string
	}{
		{
			name:        "a one-line output is quoted where it lands",
			call:        domain.ToolCall{ID: "1", Tool: "terminal", Arguments: []byte(`{"command":"cat /home/me/proj/paths.txt"}`)},
			res:         domain.ToolResult{CallID: "1", Content: "/home/me/proj/docs/plan.md\n"},
			wantTarget:  "cat paths.txt",
			wantSummary: "/home/me/proj/docs/plan.md",
		},
		{
			name:        "a sentence the tool reported names its path and shortens",
			call:        domain.ToolCall{ID: "2", Tool: "single_find_and_replace", Arguments: []byte(`{"path":"/home/me/proj/main.go"}`)},
			res:         domain.ToolResult{CallID: "2", Content: "replaced text in /home/me/proj/main.go"},
			wantTarget:  "main.go",
			wantSummary: "replaced text in main.go",
		},
		{
			name:        "the typed stat in the slot is the block's own words",
			call:        domain.ToolCall{ID: "3", Tool: "terminal", Arguments: []byte(`{"command":"touch /home/me/proj/new.txt"}`)},
			res:         domain.ToolResult{CallID: "3", Content: "\n"},
			wantTarget:  "touch new.txt",
			wantSummary: "exit 0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tv := presentToolCall(tc.call, ws)
			tv.enrichWithResult(tc.res, ws)

			if tv.Summary.Text != tc.wantSummary {
				t.Errorf("summary = %q, want %q", tv.Summary.Text, tc.wantSummary)
			}
			if tv.Target != tc.wantTarget {
				t.Errorf("target = %q, want %q — the path the block NAMES shortens either way", tv.Target, tc.wantTarget)
			}
			if !strings.Contains(string(tc.call.Arguments), "/home/me/proj") {
				t.Errorf("the call's own arguments were rewritten: %s", tc.call.Arguments)
			}
		})
	}
}

// ask_user shares firstLineDetail's SHAPE — one clipped line on the branch — but not its sense of
// whose words that line is: its result is the answer the human typed, quoted content the block must
// not respell. Someone who answers a question with an absolute path wrote that path, and a
// transcript that shortens it puts words in their mouth.
//
// The second row is the contrast that makes the split a fact about the SLOT rather than the text:
// a tool reporting a sentence that NAMES a path still shortens, through the very same extractor
// shape. Both answers here read exactly like a path, which is why neither can be judged by reading
// it (branchSummary).
func TestAskUserAnswerIsQuotedAndAPathReportStillShortens(t *testing.T) {
	t.Parallel()

	ws := newWorkspaceRoot("/home/me/proj")
	cases := []struct {
		name        string
		call        domain.ToolCall
		res         domain.ToolResult
		wantSummary string
	}{
		{
			name:        "the user's own answer keeps the spelling they typed",
			call:        domain.ToolCall{ID: "1", Tool: "ask_user", Arguments: []byte(`{"question":"Which file?"}`)},
			res:         domain.ToolResult{CallID: "1", Content: "/home/me/proj/docs/plan.md"},
			wantSummary: "/home/me/proj/docs/plan.md",
		},
		{
			name:        "a tool's own sentence about a path still shortens",
			call:        domain.ToolCall{ID: "2", Tool: "single_find_and_replace", Arguments: []byte(`{"path":"/home/me/proj/docs/plan.md"}`)},
			res:         domain.ToolResult{CallID: "2", Content: "replaced text in /home/me/proj/docs/plan.md"},
			wantSummary: "replaced text in docs/plan.md",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tv := presentToolCall(tc.call, ws)
			tv.enrichWithResult(tc.res, ws)

			if tv.Summary.Text != tc.wantSummary {
				t.Errorf("summary = %q, want %q", tv.Summary.Text, tc.wantSummary)
			}
		})
	}
}

// An unregistered tool's arguments are the model's request quoted verbatim — the approval surface's
// whole point — so the VALUES are shown exactly as they were sent, workspace paths included, even
// though the body labels them by name rather than printing the JSON envelope around them. What is
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
	if !strings.Contains(got, "path:\n  /home/me/proj/docs/plan.md") {
		t.Errorf("arguments body = %q; want the argument quoted exactly as it was sent", got)
	}
	if !strings.Contains(got, "other:\n  /etc/hosts") {
		t.Errorf("arguments body = %q; want the outside path left absolute", got)
	}
}

// A collapsed run's live gist words a call from the same view the block does (toolPhrase →
// presentToolCall), so the phrase on the run's summary line and the branch line inside it can never
// spell one path two ways. (The status line spells no path at all — toolActivityVerb.)
func TestToolPhraseSharesTheWorkspaceRelativePath(t *testing.T) {
	t.Parallel()

	ws := newWorkspaceRoot("/home/me/proj")
	got := toolPhrase(newWidthAuthority(), presentToolCall(domain.ToolCall{Tool: "read_file",
		Arguments: []byte(`{"path":"/home/me/proj/docs/plan.md"}`)}, ws))
	if want := "reading · docs/plan.md"; got != want {
		t.Errorf("tool phrase = %q, want %q", got, want)
	}
}

// The transcript resolves the root once and keeps it across a session reset: /clear opens a new
// conversation, not a new workspace.
func TestTranscriptShortensPathsAndKeepsItsRootAcrossReset(t *testing.T) {
	t.Parallel()

	var tr transcript
	tr.ws = newWorkspaceRoot("/home/me/proj")
	tr.addToolCall(domain.ToolCall{ID: "1", Tool: "read_file",
		Arguments: []byte(`{"path":"/home/me/proj/main.go"}`)}, runRef{})
	if got := tr.entries[0].tool.Target; got != "main.go" {
		t.Fatalf("target = %q, want %q", got, "main.go")
	}

	tr.reset()
	tr.addToolCall(domain.ToolCall{ID: "2", Tool: "read_file",
		Arguments: []byte(`{"path":"/home/me/proj/docs/plan.md"}`)}, runRef{})
	if got := tr.entries[0].tool.Target; got != "docs/plan.md" {
		t.Errorf("after reset the target = %q, want %q — the root did not survive /clear", got, "docs/plan.md")
	}
}
