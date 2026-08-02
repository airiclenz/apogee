package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// detailsText joins a view's whole outcome — the branch-riding summary, then the body beneath
// it — for substring assertions that do not care which half a line landed in.
func detailsText(tv toolView) string {
	parts := make([]string, 0, tv.Details.len()+1)
	if tv.Summary.Text != "" {
		parts = append(parts, tv.Summary.Text)
	}
	for _, d := range tv.Details.all() {
		parts = append(parts, d.Text)
	}
	return strings.Join(parts, "\n")
}

// TestPresentToolCall proves the open registry: each default tool maps to its friendly label,
// its active status-line verb, and a target pulled from the arguments, its result summarises
// to one detail line, and an unknown or malformed call falls back to the raw name (verb
// "running <raw name>") with its arguments shown verbatim (the approval surface never hides
// the model's request).
//
// The seven summary-bearing tools carry the domain.ToolSummary their tool now attaches, so
// the line comes from the typed outcome rather than from the prose beside it; the "no summary"
// rows pin the D6 floor, where the same result with no summary degrades to its verbatim first
// line instead of to a raw dump. Every wantDetail here is unchanged from when the view parsed
// the prose — that the two agree, character for character, is this card's acceptance oracle —
// except on the three free-form-output rows, whose outcome now RETAINS every line it was given:
// the "… +N more lines" remainder those rows used to assert is the collapsed paint's, and it is
// asserted where it is now composed (TestCollapsedPaintTruncatesRetainedBodies, render_test.go).
func TestPresentToolCall(t *testing.T) {
	tests := []struct {
		name       string
		call       domain.ToolCall
		result     domain.ToolResult
		wantLabel  string
		wantVerb   string
		wantTarget string
		wantDetail string // a substring expected in the view's detail lines
	}{
		{
			name: "read_file → Read File + line range",
			call: domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)},
			result: domain.ToolResult{CallID: "1", Content: "[File: main.go, 120 lines total, showing lines 1-100]\npackage main",
				Summary: domain.ReadSpan{Start: 1, End: 100, Total: 120}},
			wantLabel:  "Read File",
			wantVerb:   "reading",
			wantTarget: "main.go", wantDetail: "1 - 100",
		},
		{
			name:       "read_file with no summary → the verbatim first line",
			call:       domain.ToolCall{ID: "1b", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)},
			result:     domain.ToolResult{CallID: "1b", Content: "[File: main.go, 120 lines total, showing lines 1-100]\npackage main"},
			wantLabel:  "Read File",
			wantVerb:   "reading",
			wantTarget: "main.go", wantDetail: "[File: main.go, 120 lines total, showing lines 1-100]",
		},
		{
			name: "write_file → Write File + byte count",
			call: domain.ToolCall{ID: "2", Tool: "write_file", Arguments: []byte(`{"path":"notes.txt","content":"hello"}`)},
			result: domain.ToolResult{CallID: "2", Content: "wrote 5 bytes to notes.txt",
				Summary: domain.WroteBytes{Bytes: 5}},
			wantLabel:  "Write File",
			wantVerb:   "writing",
			wantTarget: "notes.txt", wantDetail: "+5 bytes",
		},
		{
			name:       "write_file with no summary → the verbatim first line",
			call:       domain.ToolCall{ID: "2b", Tool: "write_file", Arguments: []byte(`{"path":"notes.txt","content":"hello"}`)},
			result:     domain.ToolResult{CallID: "2b", Content: "wrote 5 bytes to notes.txt"},
			wantLabel:  "Write File",
			wantVerb:   "writing",
			wantTarget: "notes.txt", wantDetail: "wrote 5 bytes to notes.txt",
		},
		{
			name: "list_dir → List Dir + entry count",
			call: domain.ToolCall{ID: "3", Tool: "list_dir", Arguments: []byte(`{"path":"src"}`)},
			result: domain.ToolResult{CallID: "3", Content: "[12 entries total]\nfoo\nbar",
				Summary: domain.ListedEntries{Total: 12}},
			wantLabel:  "List Dir",
			wantVerb:   "listing",
			wantTarget: "src", wantDetail: "12 entries",
		},
		{
			name:       "list_dir with no summary → the verbatim first line",
			call:       domain.ToolCall{ID: "3b", Tool: "list_dir", Arguments: []byte(`{"path":"src"}`)},
			result:     domain.ToolResult{CallID: "3b", Content: "[12 entries total]\nfoo\nbar"},
			wantLabel:  "List Dir",
			wantVerb:   "listing",
			wantTarget: "src", wantDetail: "[12 entries total]",
		},
		{
			name: "grep → Search + match count",
			call: domain.ToolCall{ID: "4", Tool: "grep", Arguments: []byte(`{"pattern":"TODO"}`)},
			result: domain.ToolResult{CallID: "4", Content: "[3 total matches, showing 1-3]\na\nb\nc",
				Summary: domain.MatchedLines{Total: 3}},
			wantLabel:  "Search",
			wantVerb:   "searching",
			wantTarget: "TODO", wantDetail: "3 matches",
		},
		{
			name: "grep with no matches → 0 matches",
			call: domain.ToolCall{ID: "5", Tool: "grep", Arguments: []byte(`{"pattern":"zzz"}`)},
			result: domain.ToolResult{CallID: "5", Content: "No matches found",
				Summary: domain.MatchedLines{Total: 0}},
			wantLabel:  "Search",
			wantVerb:   "searching",
			wantTarget: "zzz",
			wantDetail: "0 matches",
		},
		{
			name:       "grep with no summary → the verbatim first line",
			call:       domain.ToolCall{ID: "5b", Tool: "grep", Arguments: []byte(`{"pattern":"TODO"}`)},
			result:     domain.ToolResult{CallID: "5b", Content: "[3 total matches, showing 1-3]\na\nb\nc"},
			wantLabel:  "Search",
			wantVerb:   "searching",
			wantTarget: "TODO", wantDetail: "[3 total matches, showing 1-3]",
		},
		{
			name: "web_search → Web Search + result count, never the results",
			call: domain.ToolCall{ID: "20", Tool: "web_search", Arguments: []byte(`{"query":"golang testing"}`)},
			result: domain.ToolResult{CallID: "20", Content: "1. Go Testing\n   https://go.dev\n   snippet\n\n2. More\n   https://x.dev",
				Summary: domain.SearchHits{Count: 2}},
			wantLabel:  "Web Search",
			wantVerb:   "searching the web",
			wantTarget: "golang testing", wantDetail: "2 results",
		},
		{
			name:       "web_search with no results → the sentinel line",
			call:       domain.ToolCall{ID: "21", Tool: "web_search", Arguments: []byte(`{"query":"zzz"}`)},
			result:     domain.ToolResult{CallID: "21", Content: "No results found for: zzz"},
			wantLabel:  "Web Search",
			wantVerb:   "searching the web",
			wantTarget: "zzz", wantDetail: "No results found for: zzz",
		},
		{
			name:       "web_fetch → Web Fetch + status line, never the body",
			call:       domain.ToolCall{ID: "22", Tool: "web_fetch", Arguments: []byte(`{"url":"https://go.dev"}`)},
			result:     domain.ToolResult{CallID: "22", Content: "HTTP 200 OK\nContent-Type: text/html\n\n<html>…</html>"},
			wantLabel:  "Web Fetch",
			wantVerb:   "fetching",
			wantTarget: "https://go.dev", wantDetail: "HTTP 200 OK",
		},
		{
			name:       "http_request → METHOD url target + status line",
			call:       domain.ToolCall{ID: "23", Tool: "http_request", Arguments: []byte(`{"url":"https://api.example.com","method":"post"}`)},
			result:     domain.ToolResult{CallID: "23", Content: "HTTP 201 Created\nLocation: /things/1\n\n{}"},
			wantLabel:  "HTTP Request",
			wantVerb:   "requesting",
			wantTarget: "POST https://api.example.com", wantDetail: "HTTP 201 Created",
		},
		{
			name:       "terminal → Run + the whole output body (the paint compresses it, not the view)",
			call:       domain.ToolCall{ID: "24", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)},
			result:     domain.ToolResult{CallID: "24", Content: "ok   pkg/a 0.1s\nok   pkg/b 0.2s\nok   pkg/c 0.3s"},
			wantLabel:  "Run",
			wantVerb:   "running",
			wantTarget: "go test ./...", wantDetail: "ok   pkg/c 0.3s",
		},
		{
			name:       "terminal with empty output → (no output)",
			call:       domain.ToolCall{ID: "25", Tool: "terminal", Arguments: []byte(`{"command":"true"}`)},
			result:     domain.ToolResult{CallID: "25", Content: "\n"},
			wantLabel:  "Run",
			wantVerb:   "running",
			wantTarget: "true", wantDetail: "(no output)",
		},
		{
			name:       "python_exec → Run Python + first code line as target",
			call:       domain.ToolCall{ID: "26", Tool: "python_exec", Arguments: []byte(`{"code":"print('hi')\nprint('there')"}`)},
			result:     domain.ToolResult{CallID: "26", Content: "hi\nthere"},
			wantLabel:  "Run Python",
			wantVerb:   "running python",
			wantTarget: "print('hi')", wantDetail: "hi",
		},
		{
			name:       "git_branch → action+name target",
			call:       domain.ToolCall{ID: "27", Tool: "git_branch", Arguments: []byte(`{"action":"create","name":"feature-x"}`)},
			result:     domain.ToolResult{CallID: "27", Content: "created and switched to branch feature-x"},
			wantLabel:  "Git Branch",
			wantVerb:   "branching",
			wantTarget: "create feature-x", wantDetail: "created and switched",
		},
		{
			name:       "git_commit → message first line as target",
			call:       domain.ToolCall{ID: "28", Tool: "git_commit", Arguments: []byte(`{"message":"fix: the thing\n\nlong body"}`)},
			result:     domain.ToolResult{CallID: "28", Content: "[main abc1234] fix: the thing\n 1 file changed"},
			wantLabel:  "Git Commit",
			wantVerb:   "committing",
			wantTarget: "fix: the thing", wantDetail: "[main abc1234] fix: the thing",
		},
		{
			name:       "git_diff_range → base...head target",
			call:       domain.ToolCall{ID: "29", Tool: "git_diff_range", Arguments: []byte(`{"base":"main","head":"feature-x"}`)},
			result:     domain.ToolResult{CallID: "29", Content: "diff --git a/x b/x\n+added"},
			wantLabel:  "Git Diff",
			wantVerb:   "diffing",
			wantTarget: "main...feature-x", wantDetail: "+added",
		},
		{
			name:       "edit_existing_file → Edit File + fixed result line",
			call:       domain.ToolCall{ID: "30", Tool: "edit_existing_file", Arguments: []byte(`{"path":"main.go","content":"x"}`)},
			result:     domain.ToolResult{CallID: "30", Content: "applied patch to main.go (2 hunks)"},
			wantLabel:  "Edit File",
			wantVerb:   "editing",
			wantTarget: "main.go", wantDetail: "applied patch to main.go (2 hunks)",
		},
		{
			name: "open_file with locate → the Located line, never the content",
			call: domain.ToolCall{ID: "31", Tool: "open_file", Arguments: []byte(`{"path":"main.go","locate":"func main"}`)},
			result: domain.ToolResult{CallID: "31", Content: "File: main.go\nLocated \"func main\" on lines: 5\n\npackage main\n…",
				Summary: domain.OpenedFile{Lines: 2, Locate: "func main", LocatedOn: []int{5}}},
			wantLabel:  "Open File",
			wantVerb:   "opening",
			wantTarget: "main.go", wantDetail: `Located "func main" on lines: 5`,
		},
		{
			name: "open_file with a locate that matched nothing → on no lines",
			call: domain.ToolCall{ID: "31b", Tool: "open_file", Arguments: []byte(`{"path":"main.go","locate":"zzz"}`)},
			result: domain.ToolResult{CallID: "31b", Content: "File: main.go\nLocated \"zzz\" on no lines\n\npackage main\n…",
				Summary: domain.OpenedFile{Lines: 2, Locate: "zzz"}},
			wantLabel:  "Open File",
			wantVerb:   "opening",
			wantTarget: "main.go", wantDetail: `Located "zzz" on no lines`,
		},
		{
			name: "open_file without locate → line count, never the content",
			call: domain.ToolCall{ID: "32", Tool: "open_file", Arguments: []byte(`{"path":"main.go"}`)},
			result: domain.ToolResult{CallID: "32", Content: "File: main.go\n\npackage main\n\nfunc main() {}",
				Summary: domain.OpenedFile{Lines: 3}},
			wantLabel:  "Open File",
			wantVerb:   "opening",
			wantTarget: "main.go", wantDetail: "3 lines",
		},
		{
			name:       "open_file with no summary → the verbatim first line",
			call:       domain.ToolCall{ID: "32b", Tool: "open_file", Arguments: []byte(`{"path":"main.go"}`)},
			result:     domain.ToolResult{CallID: "32b", Content: "File: main.go\n\npackage main\n\nfunc main() {}"},
			wantLabel:  "Open File",
			wantVerb:   "opening",
			wantTarget: "main.go", wantDetail: "File: main.go",
		},
		{
			name: "view_diff → View Diff + diffstat",
			call: domain.ToolCall{ID: "35", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)},
			result: domain.ToolResult{CallID: "35", Content: "  ctx\n- old line\n+ new line",
				Summary: domain.DiffStat{Added: 1, Removed: 1}},
			wantLabel:  "View Diff",
			wantVerb:   "diffing",
			wantTarget: "main.go", wantDetail: "+1 -1",
		},
		{
			name:       "view_diff with no changes carries no summary → the sentinel line",
			call:       domain.ToolCall{ID: "36", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)},
			result:     domain.ToolResult{CallID: "36", Content: "No changes detected"},
			wantLabel:  "View Diff",
			wantVerb:   "diffing",
			wantTarget: "main.go", wantDetail: "No changes detected",
		},
		{
			name:       "sub_agent → task first line as target, the whole report as the body",
			call:       domain.ToolCall{ID: "33", Tool: "sub_agent", Arguments: []byte(`{"task":"Survey the tests.\nReport gaps."}`)},
			result:     domain.ToolResult{CallID: "33", Content: "The suite covers A and B.\nGap: C is untested."},
			wantLabel:  "Sub-Agent",
			wantVerb:   "delegating",
			wantTarget: "Survey the tests.", wantDetail: "Gap: C is untested.",
		},
		{
			name:       "ask_user → question as target, answer as detail",
			call:       domain.ToolCall{ID: "34", Tool: "ask_user", Arguments: []byte(`{"question":"Deploy to prod?"}`)},
			result:     domain.ToolResult{CallID: "34", Content: "yes, after the demo"},
			wantLabel:  "Ask User",
			wantVerb:   "asking",
			wantTarget: "Deploy to prod?", wantDetail: "yes, after the demo",
		},
		{
			name:       "unknown tool → raw label, JSON args as detail",
			call:       domain.ToolCall{ID: "6", Tool: "frobnicate", Arguments: []byte(`{"x":1}`)},
			wantLabel:  "frobnicate",
			wantVerb:   "running frobnicate",
			wantTarget: "",
			wantDetail: `"x": 1`,
		},
		{
			name:       "malformed args → shown verbatim, not dropped",
			call:       domain.ToolCall{ID: "7", Tool: "weird", Arguments: []byte("{not json")},
			wantLabel:  "weird",
			wantVerb:   "running weird",
			wantTarget: "",
			wantDetail: "{not json",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tv := presentToolCall(tc.call, workspaceRoot{})
			if tv.Label != tc.wantLabel {
				t.Errorf("Label = %q, want %q", tv.Label, tc.wantLabel)
			}
			if tv.Verb != tc.wantVerb {
				t.Errorf("Verb = %q, want %q", tv.Verb, tc.wantVerb)
			}
			if tv.Target != tc.wantTarget {
				t.Errorf("Target = %q, want %q", tv.Target, tc.wantTarget)
			}
			if tc.result.Content != "" {
				tv.enrichWithResult(tc.result, workspaceRoot{})
			}
			if got := detailsText(tv); !strings.Contains(got, tc.wantDetail) {
				t.Errorf("details = %q; want a line containing %q", got, tc.wantDetail)
			}
		})
	}
}

// An error result is summarised as an "error: …" detail rather than the tool's normal
// summary — a normal in-band outcome the model reacts to. It is the *summary*, not a body
// line, which is what keeps an errored call grouping with its neighbours.
func TestPresentToolCallErrorResult(t *testing.T) {
	tv := presentToolCall(domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"missing"}`)}, workspaceRoot{})
	tv.enrichWithResult(domain.ToolResult{CallID: "1", Content: "file not found: missing", IsError: true}, workspaceRoot{})
	if got := tv.Summary.Text; got != "error: file not found: missing" {
		t.Errorf("error summary = %q; want the error text", got)
	}
	if tv.Details.len() != 0 {
		t.Errorf("error body = %+v; want nothing beneath the branch", tv.Details)
	}
	if !groupable(tv) {
		t.Error("an errored call must still group with its neighbours")
	}
}

// TestPresentToolCallOutcomeSplit pins which half of the outcome each kind of producer fills —
// the split the block's shape is read off. A fixed result header is summary-only (it rides the
// branch beside the target). Free-form command output fills the half its own size dictates:
// output of one line (including none at all) rides the branch like any other one-line outcome,
// while output with more to say is a body beneath the command (layout.md's Run sketch) — and
// that body now holds every line, since the collapsed shape's remainder is the painter's act.
// view_diff is the one producer filling both, a diffstat on the branch over a coloured body.
func TestPresentToolCallOutcomeSplit(t *testing.T) {
	cases := []struct {
		name        string
		call        domain.ToolCall
		result      domain.ToolResult
		wantSummary string
		wantBody    []string
	}{
		{
			name: "read_file is summary-only",
			call: domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)},
			result: domain.ToolResult{CallID: "1", Content: "[File: main.go, 154 lines total, showing lines 1-154]\npackage main",
				Summary: domain.ReadSpan{Start: 1, End: 154, Total: 154}},
			wantSummary: "1 - 154",
		},
		{
			name:        "multi-line terminal output is body-only",
			call:        domain.ToolCall{ID: "2", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)},
			result:      domain.ToolResult{CallID: "2", Content: "ok   apogee/internal/tui   0.412s\nok   apogee/internal/agent   1.203s\nPASS"},
			wantSummary: "",
			wantBody:    []string{"ok   apogee/internal/tui   0.412s", "ok   apogee/internal/agent   1.203s", "PASS"},
		},
		{
			name:        "one-line terminal output is summary-only",
			call:        domain.ToolCall{ID: "3", Tool: "terminal", Arguments: []byte(`{"command":"git rev-parse HEAD"}`)},
			result:      domain.ToolResult{CallID: "3", Content: "abc1234\n"},
			wantSummary: "abc1234",
		},
		{
			name:        "empty terminal output is summary-only",
			call:        domain.ToolCall{ID: "4", Tool: "terminal", Arguments: []byte(`{"command":"true"}`)},
			result:      domain.ToolResult{CallID: "4", Content: "\n"},
			wantSummary: "(no output)",
		},
		{
			name: "view_diff is both",
			call: domain.ToolCall{ID: "5", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)},
			result: domain.ToolResult{CallID: "5", Content: "  ctx\n- old line\n+ new line",
				Summary: domain.DiffStat{Added: 1, Removed: 1}},
			wantSummary: "+1 -1",
			wantBody:    []string{"  ctx", "- old line", "+ new line"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tv := presentToolCall(tc.call, workspaceRoot{})
			tv.enrichWithResult(tc.result, workspaceRoot{})
			if tv.Summary.Text != tc.wantSummary {
				t.Errorf("summary = %q, want %q", tv.Summary.Text, tc.wantSummary)
			}
			body := make([]string, 0, tv.Details.len())
			for _, d := range tv.Details.all() {
				body = append(body, d.Text)
			}
			if strings.Join(body, "\n") != strings.Join(tc.wantBody, "\n") {
				t.Errorf("body = %q, want %q", body, tc.wantBody)
			}
		})
	}
}

// A call still in flight carries neither half of an outcome, and the zero summary is plain, so
// it groups with its finished neighbours rather than breaking their block.
func TestPresentToolCallInFlightHasNoOutcome(t *testing.T) {
	tv := presentToolCall(domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)}, workspaceRoot{})
	if tv.Summary.Text != "" || tv.Details.len() != 0 {
		t.Errorf("in-flight outcome = %+v / %+v; want both halves empty", tv.Summary, tv.Details)
	}
	if !groupable(tv) {
		t.Error("an in-flight call must group with its neighbours")
	}
}

// TestDiffBody proves view_diff's body renderer is the diff kinds' producer: "+ " lines are
// detailDiffAdded, "- " lines detailDiffRemoved, context plain — and that it RETAINS the whole
// diff, however long. The cap that keeps a rewrite from flooding the chat is the collapsed
// paint's (TestCollapsedPaintTruncatesRetainedBodies, render_test.go), so expanding the block
// can show the lines it hides; nothing here counts or truncates.
func TestDiffBody(t *testing.T) {
	details := diffBody("  ctx\n- old line\n+ new line")
	wantKinds := []detailKind{detailPlain, detailDiffRemoved, detailDiffAdded}
	if len(details) != len(wantKinds) {
		t.Fatalf("got %d detail lines, want %d: %+v", len(details), len(wantKinds), details)
	}
	for i, want := range wantKinds {
		if details[i].Kind != want {
			t.Errorf("line %d (%q): kind = %v, want %v", i, details[i].Text, details[i].Kind, want)
		}
	}

	long := strings.TrimSuffix(strings.Repeat("+ added\n", diffDetailCap+5), "\n")
	whole := diffBody(long)
	if len(whole) != diffDetailCap+5 {
		t.Fatalf("retained diff has %d lines, want every one of the %d", len(whole), diffDetailCap+5)
	}
	for i, d := range whole {
		if d.Kind != detailDiffAdded {
			t.Errorf("line %d (%q): kind = %v, want %v (no synthesized marker line)", i, d.Text, d.Kind, detailDiffAdded)
		}
	}
}

// TestBodyKindIsSettledWhereTheLinesAre pins the mechanism the collapsed paint's cap reads: the
// body's kind is decided ONCE, where the lines are put into a body (newToolBody), and carried by
// that body — not re-derived from the lines on every repaint, over a body the entry retains whole.
// Both flavours and both edges are covered: an empty body and a single line never truncate whatever
// their kind, so they are exactly the shapes where a wrong answer would hide until the body grew.
func TestBodyKindIsSettledWhereTheLinesAre(t *testing.T) {
	tests := []struct {
		name    string
		details []detailLine
		want    bool
	}{
		{name: "no body at all", details: nil, want: false},
		{name: "a single plain line", details: []detailLine{{Text: "ok"}}, want: false},
		{
			name:    "a multi-line plain body",
			details: []detailLine{{Text: "ok   a"}, {Text: "ok   b"}, {Text: "PASS"}},
			want:    false,
		},
		{
			name:    "a single diff line",
			details: []detailLine{{Kind: detailDiffAdded, Text: "+ added"}},
			want:    true,
		},
		{
			name: "a diff line behind context — the scan cannot stop at the first line",
			details: []detailLine{
				{Text: "  ctx"}, {Text: "  ctx"}, {Kind: detailDiffRemoved, Text: "- gone"},
			},
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tv := toolView{Label: "Tool", Target: "main.go", Details: newToolBody(tc.details)}
			tv.sanitize()
			if tv.Details.isDiff() != tc.want {
				t.Errorf("body kind = %v, want %v for %+v", tv.Details.isDiff(), tc.want, tc.details)
			}
		})
	}
}

// TestBodyKindFollowsTheProducer proves the seam is reached on the paths that actually make
// bodies: view_diff's tagged body settles as a diff, free-form output settles as plain. Together
// with TestBodyKindIsSettledWhereTheLinesAre this is the whole contract collapsedDetails relies on.
func TestBodyKindFollowsTheProducer(t *testing.T) {
	diff := presentToolCall(domain.ToolCall{ID: "1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}, workspaceRoot{})
	diff.enrichWithResult(domain.ToolResult{
		CallID: "1", Content: "  ctx\n- old line\n+ new line", Summary: domain.DiffStat{Added: 1, Removed: 1},
	}, workspaceRoot{})
	if !diff.Details.isDiff() {
		t.Errorf("a view_diff body must settle as a diff body: %+v", diff.Details)
	}

	run := presentToolCall(domain.ToolCall{ID: "2", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}, workspaceRoot{})
	run.enrichWithResult(domain.ToolResult{CallID: "2", Content: "ok   a\nok   b\nPASS"}, workspaceRoot{})
	if run.Details.isDiff() {
		t.Errorf("free-form output must settle as a plain body: %+v", run.Details)
	}
}

// TestBodyKindMatchesItsLinesEverywhere is the behavioural half of the invariant collapsedDetails
// trusts: whatever a body says about itself must be what a fresh scan of its own lines says, for
// every view the presenter produces and for every view that comes back off the wire. It walks the
// whole registry (plus the unregistered fallback) rather than a chosen few, so a tool added with a
// new body path is covered the day it is registered — and each view is round-tripped through the
// codec, the second producer of views, whose decode has to settle a kind the wire never carried.
//
// The staleness this guards is silent by nature: a diff body that says it is plain paints one line
// and a "… +N more lines" marker where twenty lines of change belong, and nothing else goes wrong.
func TestBodyKindMatchesItsLinesEverywhere(t *testing.T) {
	t.Parallel()

	// One argument blob covering every registered target extractor, so each tool gets a realistic
	// header, and results in both flavours: a tagged diff and free-form prose.
	const args = `{"path":"main.go","paths":["main.go"],"pattern":"TODO","command":"go test ./...",` +
		`"query":"go slices","url":"https://example.test","task":"tidy up","question":"which one?",` +
		`"content":"x","find":"a","replace":"b","edits":[{"find":"a","replace":"b"}]}`
	results := []domain.ToolResult{
		{CallID: "1", Content: "  ctx\n- old line\n+ new line", Summary: domain.DiffStat{Added: 1, Removed: 1}},
		{CallID: "1", Content: "ok   a\nok   b\nPASS"},
		{CallID: "1", Content: "boom", IsError: true},
	}

	names := slices.Sorted(maps.Keys(toolRegistry))
	names = append(names, "not_a_registered_tool")
	for _, name := range names {
		for i, result := range results {
			t.Run(name+"/"+strconv.Itoa(i), func(t *testing.T) {
				tv := presentToolCall(domain.ToolCall{ID: "1", Tool: name, Arguments: []byte(args)}, workspaceRoot{})
				assertBodyKindMatchesLines(t, tv, "before the result")
				tv.enrichWithResult(result, workspaceRoot{})
				assertBodyKindMatchesLines(t, tv, "after the result")
				assertBodyKindMatchesLines(t, fromWireToolView(toWireToolView(tv)), "off the wire")
			})
		}
	}
}

// assertBodyKindMatchesLines fails when a view's body disagrees with a fresh scan of the very lines
// it holds — the stale pairing the body type exists to make unwritable.
func assertBodyKindMatchesLines(t *testing.T, tv toolView, when string) {
	t.Helper()
	if want := bodyIsDiff(tv.Details.all()); tv.Details.isDiff() != want {
		t.Errorf("%s: body says isDiff=%v, its lines say %v: %+v", when, tv.Details.isDiff(), want, tv.Details.all())
	}
}

// toolBodyType and toolBodyConstructor are the names the literal guard watches: the retained body
// and the only function allowed to pair its lines with their kind.
const (
	toolBodyType        = "toolBody"
	toolBodyConstructor = "newToolBody"
)

// TestToolBodyIsBuiltOnlyByItsConstructor closes the last door on a stale body. The type makes the
// natural mistake impossible — a bare []detailLine is not a body, so no caller can hand the painter
// lines with no kind — but a composite literal could still name the fields and pair them wrongly.
// This reads the whole package off disk and fails on any toolBody literal with elements outside
// newToolBody; the zero literal (no lines, no diff) is consistent by construction and stays legal.
//
// It is parsed rather than reflected over because the defect is a line of source that does not exist
// yet: reflection can only see the pairings a run produced, and this one would ship as a paint bug.
func TestToolBodyIsBuiltOnlyByItsConstructor(t *testing.T) {
	t.Parallel()

	dir, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	fset := token.NewFileSet()
	scanned, allowed := 0, 0
	for _, de := range dir {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, de.Name(), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", de.Name(), err)
		}
		scanned++
		ctor := funcDeclNamed(file, toolBodyConstructor)
		// Everything in the file, not only function bodies: a package-level var could pair a
		// body's fields just as wrongly as a statement can.
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || len(lit.Elts) == 0 {
				return true
			}
			if id, ok := lit.Type.(*ast.Ident); !ok || id.Name != toolBodyType {
				return true
			}
			if ctor != nil && lit.Pos() > ctor.Pos() && lit.End() < ctor.End() {
				allowed++
				return true
			}
			t.Errorf("%s: a %s literal with its own fields; build it with %s so its kind is derived from the lines it is paired with",
				fset.Position(lit.Pos()), toolBodyType, toolBodyConstructor)
			return true
		})
	}
	// A guard that parsed nothing, or that no longer finds the one literal it permits, proves
	// nothing: the type or its constructor was renamed and this test stopped watching anything.
	if scanned == 0 {
		t.Fatal("no Go files were parsed; the body-literal guard proved nothing")
	}
	if allowed == 0 {
		t.Fatalf("%s builds no %s literal; the guard is watching for a shape that no longer exists", toolBodyConstructor, toolBodyType)
	}
}

// funcDeclNamed returns the file's declaration of the named function, or nil when it declares none.
func funcDeclNamed(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

// TestDiffStatSpansTheWholeDiff: the diffstat riding the branch describes the whole diff even
// when the collapsed paint stops at diffDetailCap — a truncated paint cannot tell you how big
// the change was, and the stat no longer comes from the body's lines at all but from the tool's
// domain.DiffStat, counted over the diff operations themselves (internal/tools). The outcome
// itself keeps every line, so what the paint hides is only hidden.
func TestDiffStatSpansTheWholeDiff(t *testing.T) {
	long := strings.TrimSuffix(strings.Repeat("+ added\n", diffDetailCap+5), "\n")
	tv := presentToolCall(domain.ToolCall{ID: "1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}, workspaceRoot{})
	tv.enrichWithResult(domain.ToolResult{CallID: "1", Content: long, Summary: domain.DiffStat{Added: diffDetailCap + 5}}, workspaceRoot{})

	if want := "+" + strconv.Itoa(diffDetailCap+5) + " -0"; tv.Summary.Text != want {
		t.Errorf("diffstat = %q, want %q", tv.Summary.Text, want)
	}
	if tv.Details.len() != diffDetailCap+5 {
		t.Errorf("body has %d lines, want the whole %d", tv.Details.len(), diffDetailCap+5)
	}
}

// TestViewDiffNoChangesRendersAsProse: the "No changes detected" result carries NO summary —
// there is no diff to describe — so it falls to the prose floor as one plain summary line
// with nothing beneath the branch, exactly as it rendered before the view read fields.
func TestViewDiffNoChangesRendersAsProse(t *testing.T) {
	tv := presentToolCall(domain.ToolCall{ID: "1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}, workspaceRoot{})
	tv.enrichWithResult(domain.ToolResult{CallID: "1", Content: "No changes detected"}, workspaceRoot{})

	if tv.Summary.Text != "No changes detected" || tv.Summary.Kind != detailPlain {
		t.Errorf("the no-changes sentinel must be one plain summary line: %+v", tv.Summary)
	}
	if tv.Details.len() != 0 {
		t.Errorf("the no-changes sentinel must hang nothing beneath the branch: %+v", tv.Details)
	}
}

// TestSummaryLine is the view's whole vocabulary for a typed outcome, in one table: every
// domain.ToolSummary variant and the line it words. Two rows are traps worth naming — the
// "entries" and "matches" forms are count-INDEPENDENT (they read "1 entries", which is what
// the card has always shown, and plural() would render "matchs" for the singular) — and the
// three OpenedFile rows cover the distinction only the typed summary can make: a locate that
// matched nothing versus no locate requested at all.
func TestSummaryLine(t *testing.T) {
	cases := []struct {
		name    string
		summary domain.ToolSummary
		want    string
	}{
		{name: "read span", summary: domain.ReadSpan{Start: 1, End: 100, Total: 120}, want: "1 - 100"},
		{name: "wrote bytes", summary: domain.WroteBytes{Bytes: 5}, want: "+5 bytes"},
		{name: "listed entries", summary: domain.ListedEntries{Total: 12, Skipped: 4}, want: "12 entries"},
		{name: "one entry is still the plural form", summary: domain.ListedEntries{Total: 1}, want: "1 entries"},
		{name: "matched lines", summary: domain.MatchedLines{Total: 3}, want: "3 matches"},
		{name: "one match is still the plural form", summary: domain.MatchedLines{Total: 1}, want: "1 matches"},
		{name: "no matches is a number, not a prefix test", summary: domain.MatchedLines{Total: 0}, want: "0 matches"},
		{name: "diffstat names both counts", summary: domain.DiffStat{Added: 2, Removed: 0}, want: "+2 -0"},
		{name: "search hits", summary: domain.SearchHits{Count: 2}, want: "2 results"},
		{name: "one search hit is singular", summary: domain.SearchHits{Count: 1}, want: "1 result"},
		{
			name:    "opened file with a locate that matched",
			summary: domain.OpenedFile{Lines: 40, Locate: "func main", LocatedOn: []int{5, 9}},
			want:    `Located "func main" on lines: 5, 9`,
		},
		{
			name:    "opened file with a locate that matched nothing",
			summary: domain.OpenedFile{Lines: 40, Locate: "zzz"},
			want:    `Located "zzz" on no lines`,
		},
		{name: "opened file with no locate", summary: domain.OpenedFile{Lines: 3}, want: "3 lines"},
		{name: "one line is singular", summary: domain.OpenedFile{Lines: 1}, want: "1 line"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line, ok := summaryLine(tc.summary)
			if !ok {
				t.Fatalf("summaryLine(%#v) reported no line", tc.summary)
			}
			if line.Text != tc.want {
				t.Errorf("summaryLine(%#v) = %q, want %q", tc.summary, line.Text, tc.want)
			}
			if line.Kind != detailPlain {
				t.Errorf("summary kind = %v, want detailPlain", line.Kind)
			}
		})
	}
}

// A nil summary is the prose signal: summaryLine declines, and enrichWithResult falls through
// to the registry's extractor. That is the D6 floor, and it is what keeps a third-party tool
// (which can never emit a summary — the sum is sealed) rendering as it always did.
func TestSummaryLineNilFallsToProse(t *testing.T) {
	if line, ok := summaryLine(nil); ok {
		t.Errorf("a nil summary must report no line, got %q", line.Text)
	}
}

// An over-long locate term is clipped like any other detail line, so a model that asks to
// locate a minified blob cannot flood the row.
func TestSummaryLineClipsLongLocate(t *testing.T) {
	long := strings.Repeat("x", detailClipRunes+40)
	line, ok := summaryLine(domain.OpenedFile{Lines: 2, Locate: long, LocatedOn: []int{1}})
	if !ok {
		t.Fatal("an OpenedFile summary must render a line")
	}
	if len([]rune(line.Text)) != detailClipRunes+1 { // +1 for the ellipsis
		t.Errorf("locate line is %d runes, want it clipped to %d", len([]rune(line.Text)), detailClipRunes+1)
	}
}

// TestClipDetail: one over-long line (a minified blob, a wall-of-text report) is truncated
// with an ellipsis rather than soft-wrapping into many rows.
func TestClipDetail(t *testing.T) {
	long := strings.Repeat("x", detailClipRunes+40)
	got := clipDetail(long)
	if want := detailClipRunes + 1; len([]rune(got)) != want { // +1 for the ellipsis
		t.Errorf("clipped length = %d runes, want %d", len([]rune(got)), want)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("clipped line must end in an ellipsis: %q", got[len(got)-8:])
	}
	if short := clipDetail("short"); short != "short" {
		t.Errorf("a short line must pass through unchanged: %q", short)
	}
}
