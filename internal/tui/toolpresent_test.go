package tui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// detailsText joins a view's whole outcome — the branch-riding summary, then the body beneath
// it — for substring assertions that do not care which half a line landed in.
func detailsText(tv toolView) string {
	parts := make([]string, 0, len(tv.Details)+1)
	if tv.Summary.Text != "" {
		parts = append(parts, tv.Summary.Text)
	}
	for _, d := range tv.Details {
		parts = append(parts, d.Text)
	}
	return strings.Join(parts, "\n")
}

// TestPresentToolCall proves the open registry: each default tool maps to its friendly label,
// its active status-line verb, and a target pulled from the arguments, its fixed result header
// summarises to one detail line, and an unknown or malformed call falls back to the raw name
// (verb "running <raw name>") with its arguments shown verbatim (the approval surface never
// hides the model's request).
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
			name:       "read_file → Read File + line range",
			call:       domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)},
			result:     domain.ToolResult{CallID: "1", Content: "[File: main.go, 120 lines total, showing lines 1-100]\npackage main"},
			wantLabel:  "Read File",
			wantVerb:   "reading",
			wantTarget: "main.go", wantDetail: "1 - 100",
		},
		{
			name:       "write_file → Write File + byte count",
			call:       domain.ToolCall{ID: "2", Tool: "write_file", Arguments: []byte(`{"path":"notes.txt","content":"hello"}`)},
			result:     domain.ToolResult{CallID: "2", Content: "wrote 5 bytes to notes.txt"},
			wantLabel:  "Write File",
			wantVerb:   "writing",
			wantTarget: "notes.txt", wantDetail: "+5 bytes",
		},
		{
			name:       "list_dir → List Dir + entry count",
			call:       domain.ToolCall{ID: "3", Tool: "list_dir", Arguments: []byte(`{"path":"src"}`)},
			result:     domain.ToolResult{CallID: "3", Content: "[12 entries total]\nfoo\nbar"},
			wantLabel:  "List Dir",
			wantVerb:   "listing",
			wantTarget: "src", wantDetail: "12 entries",
		},
		{
			name:       "grep → Search + match count",
			call:       domain.ToolCall{ID: "4", Tool: "grep", Arguments: []byte(`{"pattern":"TODO"}`)},
			result:     domain.ToolResult{CallID: "4", Content: "[3 total matches, showing 1-3]\na\nb\nc"},
			wantLabel:  "Search",
			wantVerb:   "searching",
			wantTarget: "TODO", wantDetail: "3 matches",
		},
		{
			name:       "grep with no matches → 0 matches",
			call:       domain.ToolCall{ID: "5", Tool: "grep", Arguments: []byte(`{"pattern":"zzz"}`)},
			result:     domain.ToolResult{CallID: "5", Content: "No matches found"},
			wantLabel:  "Search",
			wantVerb:   "searching",
			wantTarget: "zzz",
			wantDetail: "0 matches",
		},
		{
			name:       "web_search → Web Search + result count, never the results",
			call:       domain.ToolCall{ID: "20", Tool: "web_search", Arguments: []byte(`{"query":"golang testing"}`)},
			result:     domain.ToolResult{CallID: "20", Content: "1. Go Testing\n   https://go.dev\n   snippet\n\n2. More\n   https://x.dev"},
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
			name:       "terminal → Run + first output line and remainder count",
			call:       domain.ToolCall{ID: "24", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)},
			result:     domain.ToolResult{CallID: "24", Content: "ok   pkg/a 0.1s\nok   pkg/b 0.2s\nok   pkg/c 0.3s"},
			wantLabel:  "Run",
			wantVerb:   "running",
			wantTarget: "go test ./...", wantDetail: "… +2 more lines",
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
			wantTarget: "main...feature-x", wantDetail: "… +1 more line",
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
			name:       "open_file with locate → the Located line, never the content",
			call:       domain.ToolCall{ID: "31", Tool: "open_file", Arguments: []byte(`{"path":"main.go","locate":"func main"}`)},
			result:     domain.ToolResult{CallID: "31", Content: "File: main.go\nLocated \"func main\" on lines: 5\n\npackage main\n…"},
			wantLabel:  "Open File",
			wantVerb:   "opening",
			wantTarget: "main.go", wantDetail: `Located "func main" on lines: 5`,
		},
		{
			name:       "open_file without locate → line count, never the content",
			call:       domain.ToolCall{ID: "32", Tool: "open_file", Arguments: []byte(`{"path":"main.go"}`)},
			result:     domain.ToolResult{CallID: "32", Content: "File: main.go\n\npackage main\n\nfunc main() {}"},
			wantLabel:  "Open File",
			wantVerb:   "opening",
			wantTarget: "main.go", wantDetail: "3 lines",
		},
		{
			name:       "sub_agent → task first line as target, report gist as detail",
			call:       domain.ToolCall{ID: "33", Tool: "sub_agent", Arguments: []byte(`{"task":"Survey the tests.\nReport gaps."}`)},
			result:     domain.ToolResult{CallID: "33", Content: "The suite covers A and B.\nGap: C is untested."},
			wantLabel:  "Sub-Agent",
			wantVerb:   "delegating",
			wantTarget: "Survey the tests.", wantDetail: "… +1 more line",
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
			tv := presentToolCall(tc.call)
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
				tv.enrichWithResult(tc.result)
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
	tv := presentToolCall(domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"missing"}`)})
	tv.enrichWithResult(domain.ToolResult{CallID: "1", Content: "file not found: missing", IsError: true})
	if got := tv.Summary.Text; got != "error: file not found: missing" {
		t.Errorf("error summary = %q; want the error text", got)
	}
	if len(tv.Details) != 0 {
		t.Errorf("error body = %+v; want nothing beneath the branch", tv.Details)
	}
	if !groupable(tv) {
		t.Error("an errored call must still group with its neighbours")
	}
}

// TestPresentToolCallOutcomeSplit pins which half of the outcome each kind of producer fills —
// the split the block's shape is read off. A fixed result header is summary-only (it rides the
// branch beside the target). Free-form command output fills the half its own size dictates:
// output that compresses to one line (including none at all) rides the branch like any other
// one-line outcome, while output needing the "… +N more lines" remainder is a body beneath the
// command (layout.md's Run sketch). view_diff is the one producer filling both, a diffstat on
// the branch over a coloured body.
func TestPresentToolCallOutcomeSplit(t *testing.T) {
	cases := []struct {
		name        string
		call        domain.ToolCall
		result      domain.ToolResult
		wantSummary string
		wantBody    []string
	}{
		{
			name:        "read_file is summary-only",
			call:        domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)},
			result:      domain.ToolResult{CallID: "1", Content: "[File: main.go, 154 lines total, showing lines 1-154]\npackage main"},
			wantSummary: "1 - 154",
		},
		{
			name:        "multi-line terminal output is body-only",
			call:        domain.ToolCall{ID: "2", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)},
			result:      domain.ToolResult{CallID: "2", Content: "ok   apogee/internal/tui   0.412s\nok   apogee/internal/agent   1.203s\nPASS"},
			wantSummary: "",
			wantBody:    []string{"ok   apogee/internal/tui   0.412s", "… +2 more lines"},
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
			name:        "view_diff is both",
			call:        domain.ToolCall{ID: "5", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)},
			result:      domain.ToolResult{CallID: "5", Content: "  ctx\n- old line\n+ new line"},
			wantSummary: "+1 -1",
			wantBody:    []string{"  ctx", "- old line", "+ new line"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tv := presentToolCall(tc.call)
			tv.enrichWithResult(tc.result)
			if tv.Summary.Text != tc.wantSummary {
				t.Errorf("summary = %q, want %q", tv.Summary.Text, tc.wantSummary)
			}
			body := make([]string, 0, len(tv.Details))
			for _, d := range tv.Details {
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
	tv := presentToolCall(domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)})
	if tv.Summary.Text != "" || len(tv.Details) != 0 {
		t.Errorf("in-flight outcome = %+v / %+v; want both halves empty", tv.Summary, tv.Details)
	}
	if !groupable(tv) {
		t.Error("an in-flight call must group with its neighbours")
	}
}

// TestDiffDetail proves view_diff's extractor is the diff kinds' producer: "+ " lines are
// detailDiffAdded, "- " lines detailDiffRemoved, context plain — and a diff longer than the
// cap is truncated with a remainder count instead of flooding the chat.
func TestDiffDetail(t *testing.T) {
	details := diffDetail("  ctx\n- old line\n+ new line").Details
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
	capped := diffDetail(long).Details
	if len(capped) != diffDetailCap+1 {
		t.Fatalf("capped diff has %d lines, want %d", len(capped), diffDetailCap+1)
	}
	if last := capped[len(capped)-1].Text; !strings.Contains(last, "+5 more lines") {
		t.Errorf("cap line = %q, want the remainder count", last)
	}

	sentinel := diffDetail("No changes detected")
	if sentinel.Summary.Text != "No changes detected" || sentinel.Summary.Kind != detailPlain {
		t.Errorf("the no-changes sentinel must be one plain summary line: %+v", sentinel.Summary)
	}
	if len(sentinel.Details) != 0 {
		t.Errorf("the no-changes sentinel must hang nothing beneath the branch: %+v", sentinel.Details)
	}
}

// TestDiffDetailStat pins the diffstat that rides the branch beside the path: always both
// counts (the layout.md sketch's "+2 -2"), counted over the WHOLE diff rather than the lines
// that survive diffDetailCap — a truncated body cannot tell you how big the change was — and
// only over the diff's own "+"/"- " tags, so a context line is neither.
func TestDiffDetailStat(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "additions and removals",
			content: "  ctx\n- old one\n- old two\n+ new one\n+ new two",
			want:    "+2 -2",
		},
		{
			name:    "additions only still names both counts",
			content: "  ctx\n+ new one\n+ new two",
			want:    "+2 -0",
		},
		{
			name:    "removals only still names both counts",
			content: "  ctx\n- old one\n- old two",
			want:    "+0 -2",
		},
		{
			name:    "a line whose content starts with a plus is tagged, not counted",
			content: "  +++ b/main.go\n+ + a real addition",
			want:    "+1 -0",
		},
		{
			name:    "the count spans the whole diff, not just the uncapped head",
			content: strings.TrimSuffix(strings.Repeat("+ added\n", diffDetailCap+5), "\n"),
			want:    "+" + strconv.Itoa(diffDetailCap+5) + " -0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := diffDetail(tc.content).Summary.Text; got != tc.want {
				t.Errorf("diffstat = %q, want %q", got, tc.want)
			}
		})
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
