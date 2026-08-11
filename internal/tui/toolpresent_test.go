package tui

import (
	"encoding/json"
	"reflect"
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
// the "+N more lines" remainder those rows used to assert is the collapsed paint's, and it is
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
			name: "read_file → Read + the line count of the span",
			call: domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)},
			result: domain.ToolResult{CallID: "1", Content: "[File: main.go, 120 lines total, showing lines 1-100]\npackage main",
				Summary: domain.ReadSpan{Start: 1, End: 100, Total: 120}},
			wantLabel:  "Read",
			wantVerb:   "reading",
			wantTarget: "main.go", wantDetail: "100 lines",
		},
		{
			name:       "read_file with no summary → the verbatim first line",
			call:       domain.ToolCall{ID: "1b", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)},
			result:     domain.ToolResult{CallID: "1b", Content: "[File: main.go, 120 lines total, showing lines 1-100]\npackage main"},
			wantLabel:  "Read",
			wantVerb:   "reading",
			wantTarget: "main.go", wantDetail: "[File: main.go, 120 lines total, showing lines 1-100]",
		},
		{
			name: "write_file → Write + the line count of what it writes",
			call: domain.ToolCall{ID: "2", Tool: "write_file", Arguments: []byte(`{"path":"notes.txt","content":"hello"}`)},
			result: domain.ToolResult{CallID: "2", Content: "wrote 5 bytes to notes.txt",
				Summary: domain.WroteBytes{Bytes: 5}},
			wantLabel:  "Write",
			wantVerb:   "writing",
			wantTarget: "notes.txt", wantDetail: "1 line",
		},
		{
			name:       "write_file with no summary → still the line count its own request states",
			call:       domain.ToolCall{ID: "2b", Tool: "write_file", Arguments: []byte(`{"path":"notes.txt","content":"hello"}`)},
			result:     domain.ToolResult{CallID: "2b", Content: "wrote 5 bytes to notes.txt"},
			wantLabel:  "Write",
			wantVerb:   "writing",
			wantTarget: "notes.txt", wantDetail: "1 line",
		},
		{
			name: "list_dir → List + entry count",
			call: domain.ToolCall{ID: "3", Tool: "list_dir", Arguments: []byte(`{"path":"src"}`)},
			result: domain.ToolResult{CallID: "3", Content: "[12 entries total]\nfoo\nbar",
				Summary: domain.ListedEntries{Total: 12}},
			wantLabel:  "List",
			wantVerb:   "listing",
			wantTarget: "src", wantDetail: "12 entries",
		},
		{
			name:       "list_dir with no summary → the verbatim first line",
			call:       domain.ToolCall{ID: "3b", Tool: "list_dir", Arguments: []byte(`{"path":"src"}`)},
			result:     domain.ToolResult{CallID: "3b", Content: "[12 entries total]\nfoo\nbar"},
			wantLabel:  "List",
			wantVerb:   "listing",
			wantTarget: "src", wantDetail: "[12 entries total]",
		},
		{
			name: "grep → Grep + hit count",
			call: domain.ToolCall{ID: "4", Tool: "grep", Arguments: []byte(`{"pattern":"TODO"}`)},
			result: domain.ToolResult{CallID: "4", Content: "[3 total matches, showing 1-3]\na\nb\nc",
				Summary: domain.MatchedLines{Total: 3}},
			wantLabel:  "Grep",
			wantVerb:   "searching",
			wantTarget: "TODO", wantDetail: "3 hits",
		},
		{
			name: "grep with no matches → 0 hits",
			call: domain.ToolCall{ID: "5", Tool: "grep", Arguments: []byte(`{"pattern":"zzz"}`)},
			result: domain.ToolResult{CallID: "5", Content: "No matches found",
				Summary: domain.MatchedLines{Total: 0}},
			wantLabel:  "Grep",
			wantVerb:   "searching",
			wantTarget: "zzz",
			wantDetail: "0 hits",
		},
		{
			name:       "grep with no summary → the verbatim first line",
			call:       domain.ToolCall{ID: "5b", Tool: "grep", Arguments: []byte(`{"pattern":"TODO"}`)},
			result:     domain.ToolResult{CallID: "5b", Content: "[3 total matches, showing 1-3]\na\nb\nc"},
			wantLabel:  "Grep",
			wantVerb:   "searching",
			wantTarget: "TODO", wantDetail: "[3 total matches, showing 1-3]",
		},
		{
			name: "web_search → Search + result count, never the results",
			call: domain.ToolCall{ID: "20", Tool: "web_search", Arguments: []byte(`{"query":"golang testing"}`)},
			result: domain.ToolResult{CallID: "20", Content: "1. Go Testing\n   https://go.dev\n   snippet\n\n2. More\n   https://x.dev",
				Summary: domain.SearchHits{Count: 2}},
			wantLabel:  "Search",
			wantVerb:   "searching the web",
			wantTarget: "golang testing", wantDetail: "2 results",
		},
		{
			name:       "web_search with no results → the sentinel line",
			call:       domain.ToolCall{ID: "21", Tool: "web_search", Arguments: []byte(`{"query":"zzz"}`)},
			result:     domain.ToolResult{CallID: "21", Content: "No results found for: zzz"},
			wantLabel:  "Search",
			wantVerb:   "searching the web",
			wantTarget: "zzz", wantDetail: "No results found for: zzz",
		},
		{
			name:       "web_fetch → Fetch + status line, never the body",
			call:       domain.ToolCall{ID: "22", Tool: "web_fetch", Arguments: []byte(`{"url":"https://go.dev"}`)},
			result:     domain.ToolResult{CallID: "22", Content: "HTTP 200 OK\nContent-Type: text/html\n\n<html>…</html>"},
			wantLabel:  "Fetch",
			wantVerb:   "fetching",
			wantTarget: "https://go.dev", wantDetail: "HTTP 200 OK",
		},
		{
			name:       "http_request → METHOD url target + status line",
			call:       domain.ToolCall{ID: "23", Tool: "http_request", Arguments: []byte(`{"url":"https://api.example.com","method":"post"}`)},
			result:     domain.ToolResult{CallID: "23", Content: "HTTP 201 Created\nLocation: /things/1\n\n{}"},
			wantLabel:  "HTTP",
			wantVerb:   "requesting",
			wantTarget: "POST https://api.example.com", wantDetail: "HTTP 201 Created",
		},
		{
			name:       "terminal → Run + the whole output body (the paint compresses it, not the view)",
			call:       domain.ToolCall{ID: "24", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)},
			result:     domain.ToolResult{CallID: "24", Content: "ok   pkg/a 0.1s\nok   pkg/b 0.2s\nok   pkg/c 0.3s"},
			wantLabel:  "Terminal",
			wantVerb:   "running",
			wantTarget: "go test ./...", wantDetail: "ok   pkg/c 0.3s",
		},
		{
			name:       "terminal with empty output → the bare exit code",
			call:       domain.ToolCall{ID: "25", Tool: "terminal", Arguments: []byte(`{"command":"true"}`)},
			result:     domain.ToolResult{CallID: "25", Content: "\n"},
			wantLabel:  "Terminal",
			wantVerb:   "running",
			wantTarget: "true", wantDetail: "exit 0",
		},
		{
			name:       "python_exec → Python + first code line as target",
			call:       domain.ToolCall{ID: "26", Tool: "python_exec", Arguments: []byte(`{"code":"print('hi')\nprint('there')"}`)},
			result:     domain.ToolResult{CallID: "26", Content: "hi\nthere"},
			wantLabel:  "Python",
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
			name:       "edit_existing_file → Edit + the diffstat of what it sends",
			call:       domain.ToolCall{ID: "30", Tool: "edit_existing_file", Arguments: []byte(`{"path":"main.go","content":"x"}`)},
			result:     domain.ToolResult{CallID: "30", Content: "applied patch to main.go (2 hunks)"},
			wantLabel:  "Edit",
			wantVerb:   "editing",
			wantTarget: "main.go", wantDetail: "+1 −0",
		},
		{
			name: "open_file with locate → the Located line, never the content",
			call: domain.ToolCall{ID: "31", Tool: "open_file", Arguments: []byte(`{"path":"main.go","locate":"func main"}`)},
			result: domain.ToolResult{CallID: "31", Content: "File: main.go\nLocated \"func main\" on lines: 5\n\npackage main\n…",
				Summary: domain.OpenedFile{Lines: 2, Locate: "func main", LocatedOn: []int{5}}},
			wantLabel:  "Open",
			wantVerb:   "opening",
			wantTarget: `main.go · locate "func main"`, wantDetail: `Located "func main" on lines: 5`,
		},
		{
			name: "open_file with a locate that matched nothing → on no lines",
			call: domain.ToolCall{ID: "31b", Tool: "open_file", Arguments: []byte(`{"path":"main.go","locate":"zzz"}`)},
			result: domain.ToolResult{CallID: "31b", Content: "File: main.go\nLocated \"zzz\" on no lines\n\npackage main\n…",
				Summary: domain.OpenedFile{Lines: 2, Locate: "zzz"}},
			wantLabel:  "Open",
			wantVerb:   "opening",
			wantTarget: `main.go · locate "zzz"`, wantDetail: `Located "zzz" on no lines`,
		},
		{
			name: "open_file without locate → line count, never the content",
			call: domain.ToolCall{ID: "32", Tool: "open_file", Arguments: []byte(`{"path":"main.go"}`)},
			result: domain.ToolResult{CallID: "32", Content: "File: main.go\n\npackage main\n\nfunc main() {}",
				Summary: domain.OpenedFile{Lines: 3}},
			wantLabel:  "Open",
			wantVerb:   "opening",
			wantTarget: "main.go", wantDetail: "3 lines",
		},
		{
			name:       "open_file with no summary → the verbatim first line",
			call:       domain.ToolCall{ID: "32b", Tool: "open_file", Arguments: []byte(`{"path":"main.go"}`)},
			result:     domain.ToolResult{CallID: "32b", Content: "File: main.go\n\npackage main\n\nfunc main() {}"},
			wantLabel:  "Open",
			wantVerb:   "opening",
			wantTarget: "main.go", wantDetail: "File: main.go",
		},
		{
			name: "view_diff → Diff Preview + diffstat",
			call: domain.ToolCall{ID: "35", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)},
			result: domain.ToolResult{CallID: "35", Content: "  ctx\n- old line\n+ new line",
				Summary: domain.DiffStat{Added: 1, Removed: 1}},
			wantLabel:  "Diff Preview",
			wantVerb:   "diffing",
			wantTarget: "main.go", wantDetail: "+1 −1",
		},
		{
			name:       "view_diff with no changes carries no summary → the sentinel line",
			call:       domain.ToolCall{ID: "36", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)},
			result:     domain.ToolResult{CallID: "36", Content: "No changes detected"},
			wantLabel:  "Diff Preview",
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
			name:       "unknown tool → raw label, labelled args as detail",
			call:       domain.ToolCall{ID: "6", Tool: "frobnicate", Arguments: []byte(`{"x":1}`)},
			wantLabel:  "frobnicate",
			wantVerb:   "running frobnicate",
			wantTarget: "",
			wantDetail: "x:\n  1",
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

// TestPresentSubAgentNameLeadsTheHeader pins what a delegation's run header says it is. The
// sub_agent call may carry an optional short name, and when it does that name — not the delegated
// task's opening words — is the target the collapsed header leads with, which is what makes a
// fan-out of concurrent children readable as four different jobs. A call that names nothing is
// byte-identical to before: the task's first line, as every delegation written before the argument
// existed and every one a Mechanism synthesises still is.
//
// The presenter also records the name apart from the header text (toolView.agentName), because
// only that says a name was GIVEN — the live status line reads it to word "<name> · reading" —
// and it must be empty on exactly the calls that fall back. The normalisation is the tool's own
// (delegationName, internal/agent): trimmed first line, and a name that empties out is no name.
func TestPresentSubAgentNameLeadsTheHeader(t *testing.T) {
	t.Parallel()
	const task = `Survey the tests.\nReport gaps.`
	cases := []struct {
		name       string
		args       string
		wantTarget string
		wantAgent  string
	}{
		{
			name:       "a named delegation leads with its name",
			args:       `{"name":"test-surveyor","task":"` + task + `"}`,
			wantTarget: "test-surveyor",
			wantAgent:  "test-surveyor",
		},
		{
			name:       "an unnamed delegation leads with the task's first line",
			args:       `{"task":"` + task + `"}`,
			wantTarget: "Survey the tests.",
			wantAgent:  "",
		},
		{
			name:       "a padded multi-line name collapses to its trimmed first line",
			args:       `{"name":"  test-surveyor \nand then some","task":"` + task + `"}`,
			wantTarget: "test-surveyor",
			wantAgent:  "test-surveyor",
		},
		{
			name:       "a name that is only whitespace is no name",
			args:       `{"name":"   ","task":"` + task + `"}`,
			wantTarget: "Survey the tests.",
			wantAgent:  "",
		},
		{
			name:       "a non-string name is no name",
			args:       `{"name":7,"task":"` + task + `"}`,
			wantTarget: "Survey the tests.",
			wantAgent:  "",
		},
		{
			// A name that survives arrival but not the escape strip is no name either: deciding the
			// fallback on the raw string would choose it over the task and then leave the header's
			// slot blank, which is the one thing the slot may never be. The status line reads the
			// same emptiness off agentName and words the run "sub-agent" (transcript.runName).
			name:       "a name of nothing but control characters is no name",
			args:       `{"name":"\u0001\u0002\u007f","task":"` + task + `"}`,
			wantTarget: "Survey the tests.",
			wantAgent:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tv := presentToolCall(domain.ToolCall{ID: "s1", Tool: "sub_agent", Arguments: []byte(tc.args)}, workspaceRoot{})
			if tv.Target != tc.wantTarget {
				t.Errorf("Target = %q, want %q", tv.Target, tc.wantTarget)
			}
			if tv.agentName != tc.wantAgent {
				t.Errorf("agentName = %q, want %q", tv.agentName, tc.wantAgent)
			}
			if tv.Label != "Sub-Agent" || tv.Verb != "delegating" {
				t.Errorf("label/verb = %q/%q; the name changes neither", tv.Label, tv.Verb)
			}
		})
	}

	t.Run("a name is escape-stripped and clipped like any other target", func(t *testing.T) {
		t.Parallel()
		long := strings.Repeat("n", detailClipRunes+20)
		args, err := json.Marshal(map[string]any{"name": "\x1b[31m" + long, "task": "Survey the tests."})
		if err != nil {
			t.Fatalf("marshal args: %v", err)
		}
		tv := presentToolCall(domain.ToolCall{ID: "s2", Tool: "sub_agent", Arguments: args}, workspaceRoot{})
		if strings.ContainsRune(tv.Target, 0x1b) || strings.ContainsRune(tv.agentName, 0x1b) {
			t.Errorf("an ESC byte survived into the header: target=%q name=%q", tv.Target, tv.agentName)
		}
		if n := len([]rune(tv.Target)); n > detailClipRunes+1 {
			t.Errorf("target ran to %d runes; the branch's cap is %d plus the ellipsis", n, detailClipRunes)
		}
		if !strings.HasSuffix(tv.Target, "…") {
			t.Errorf("a clipped target = %q; want it to end in the ellipsis that says it goes on", tv.Target)
		}
	})
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
// the split the block's shape is read off. A fixed result header is summary-only (it fills the
// branch row's outcome slot). Free-form command output fills the half its own size dictates:
// output of one line (including none at all) takes that slot like any other one-line outcome,
// while output with more to say is a body beneath the command (layout.md's Run sketch) — and
// that body now holds every line, since the collapsed shape's remainder is the painter's act.
// view_diff is the one producer filling both, a diffstat in the slot over a coloured body.
func TestPresentToolCallOutcomeSplit(t *testing.T) {
	cases := []struct {
		name        string
		call        domain.ToolCall
		result      domain.ToolResult
		wantSummary string
		wantBody    []string
		// wantStat is the OTHER reading a promoted outcome carries — the typed phrase the
		// promote-guard swaps into the slot on a narrow row (toolView.stat). Every outcome the
		// block worded itself has one reading and so no stat.
		wantStat string
	}{
		{
			name: "read_file is summary-only",
			call: domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)},
			result: domain.ToolResult{CallID: "1", Content: "[File: main.go, 154 lines total, showing lines 1-154]\npackage main",
				Summary: domain.ReadSpan{Start: 1, End: 154, Total: 154}},
			wantSummary: "154 lines",
		},
		{
			name:        "multi-line terminal output is a body under the typed exit code",
			call:        domain.ToolCall{ID: "2", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)},
			result:      domain.ToolResult{CallID: "2", Content: "ok   apogee/internal/tui   0.412s\nok   apogee/internal/agent   1.203s\nPASS"},
			wantSummary: "exit 0",
			wantBody:    []string{"ok   apogee/internal/tui   0.412s", "ok   apogee/internal/agent   1.203s", "PASS"},
		},
		{
			name:        "one-line terminal output is summary-only",
			call:        domain.ToolCall{ID: "3", Tool: "terminal", Arguments: []byte(`{"command":"git rev-parse HEAD"}`)},
			result:      domain.ToolResult{CallID: "3", Content: "abc1234\n"},
			wantSummary: "abc1234",
			wantStat:    "exit 0",
		},
		{
			name:        "empty terminal output is the exit code alone",
			call:        domain.ToolCall{ID: "4", Tool: "terminal", Arguments: []byte(`{"command":"true"}`)},
			result:      domain.ToolResult{CallID: "4", Content: "\n"},
			wantSummary: "exit 0",
		},
		{
			name: "view_diff is both",
			call: domain.ToolCall{ID: "5", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)},
			result: domain.ToolResult{CallID: "5", Content: "  ctx\n- old line\n+ new line",
				Summary: domain.DiffStat{Added: 1, Removed: 1}},
			wantSummary: "+1 −1",
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
			if tv.stat != tc.wantStat {
				t.Errorf("stat = %q, want %q", tv.stat, tc.wantStat)
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

// TestPromotionCarriesBothReadingsOfTheOutcome pins the presenter's half of the promote-guard
// (design call 5, docs/layout/tool-layout.md): a promoted one-line output travels beside the typed
// stat that may stand in for it, and demoting swaps the two — the line to the head of the body, the
// stat into the slot — losing nothing either way. A summary the block WORDED itself has only one
// reading and cannot be demoted at all, and neither can a promotion whose promoter offered no stat:
// ask_user's answer is the row's whole point and its record already holds every line of it.
//
// The measure that decides is the painter's and is asserted there
// (TestPromoteGuardHoldsFifteenCellsOfTarget); what is pinned here is that both readings exist and
// that the swap is exact.
func TestPromotionCarriesBothReadingsOfTheOutcome(t *testing.T) {
	t.Parallel()

	present := func(call domain.ToolCall, content string) toolView {
		t.Helper()
		tv := presentToolCall(call, workspaceRoot{})
		tv.enrichWithResult(domain.ToolResult{CallID: call.ID, Content: content}, workspaceRoot{})
		return tv
	}
	terminal := domain.ToolCall{ID: "1", Tool: "terminal", Arguments: []byte(`{"command":"git rev-parse HEAD"}`)}
	read := domain.ToolCall{ID: "2", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)}
	ask := domain.ToolCall{ID: "3", Tool: "ask_user", Arguments: []byte(`{"question":"Which file?"}`)}

	for _, tc := range []struct {
		name string
		view toolView

		wantPromotable bool
		wantSummary    string   // the slot's text once demoted — unchanged where the view is not promotable
		wantBody       []string // the body once demoted, first line first
	}{{
		name:           "a one-line output demotes into the body",
		view:           present(terminal, "abc1234\n"),
		wantPromotable: true, wantSummary: "exit 0", wantBody: []string{"abc1234"},
	}, {
		// The line lands at the HEAD of whatever body the call already had — where the tool printed
		// it — rather than after it.
		name: "the demoted line leads the body it joins",
		view: func() toolView {
			tv := present(terminal, "abc1234\n")
			tv.Details = tv.Details.with([]detailLine{{Text: "an earlier line"}})
			return tv
		}(),
		wantPromotable: true, wantSummary: "exit 0",
		wantBody: []string{"abc1234", "an earlier line"},
	}, {
		name:        "a summary the block worded is not a promotion",
		view:        present(read, "[File: main.go, 154 lines total, showing lines 1-154]\npackage main"),
		wantSummary: "[File: main.go, 154 lines total, showing lines 1-154]",
	}, {
		name:        "an ask_user answer is promoted and never demoted",
		view:        present(ask, "/tmp/notes.md"),
		wantSummary: "/tmp/notes.md",
		wantBody:    []string{"Which file?"},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.view.promotable(); got != tc.wantPromotable {
				t.Errorf("promotable = %v, want %v (stat %q, quoted %v)",
					got, tc.wantPromotable, tc.view.stat, tc.view.Summary.quoted)
			}

			got := tc.view.demoted()

			if got.Summary.Text != tc.wantSummary {
				t.Errorf("demoted summary = %q, want %q", got.Summary.Text, tc.wantSummary)
			}
			if tc.wantPromotable && got.Summary.quoted {
				t.Error("the typed stat took the slot as quoted text; it is the block's own wording")
			}
			body := make([]string, 0, got.Details.len())
			for _, d := range got.Details.all() {
				body = append(body, d.Text)
			}
			if !slices.Equal(body, tc.wantBody) {
				t.Errorf("demoted body = %q, want %q", body, tc.wantBody)
			}
			// The stat leaves with the promotion, so demoting twice is demoting once — and the
			// entry's own body is untouched by either, the view being a copy the painter holds.
			if got.promotable() {
				t.Error("the demoted view is still promotable; the swap must be a one-way move")
			}
			if tc.wantPromotable && tc.view.Details.len() == got.Details.len() {
				t.Error("demoting wrote through the body the entry shares")
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

// TestAskUserAnswerRecord pins the permanent record an ANSWERED ask_user block keeps of an
// exchange the popup showed and then took away: the question as it was put, every offered choice
// behind "[x]" or "[ ]", and any answer line no choice accounts for.
//
// The branch line is the invariant across every row — the human's own answer, quoted, never
// respelled — because the record is an ADDITION beneath it and not a re-wording of it. The rows
// cover both selection shapes, a typed answer that matched nothing, the multi-line answer whose
// later lines used to reach the screen nowhere at all, a multi-line question, and the free-text
// question that offers no boxes to tick.
func TestAskUserAnswerRecord(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		args        string
		answer      string
		wantSummary string
		wantBody    []string
	}{
		{
			name:        "single-select ticks the one chosen box",
			args:        `{"question":"Which mode?","choices":["Plan","Ask before","Auto"]}`,
			answer:      "Ask before",
			wantSummary: "Ask before",
			wantBody:    []string{"Which mode?", "[ ] Plan", "[x] Ask before", "[ ] Auto"},
		},
		{
			name:        "multi-select ticks every label the answer names",
			args:        `{"question":"Which files?","choices":["main.go","doc.go","render.go"],"multi_select":true}`,
			answer:      "main.go\nrender.go",
			wantSummary: "main.go",
			wantBody:    []string{"Which files?", "[x] main.go", "[ ] doc.go", "[x] render.go"},
		},
		{
			name:        "a typed answer ticks nothing and is recorded after the list",
			args:        `{"question":"Which mode?","choices":["Plan","Auto"]}`,
			answer:      "neither — stay in ask-before",
			wantSummary: "neither — stay in ask-before",
			wantBody:    []string{"Which mode?", "[ ] Plan", "[ ] Auto", "neither — stay in ask-before"},
		},
		{
			name:        "every line of a multi-line answer is kept, not just the branch's",
			args:        `{"question":"How should it behave?","choices":["Fail closed.","Fail open."]}`,
			answer:      "Neither.\n\nRetry twice, then refuse.",
			wantSummary: "Neither.",
			wantBody: []string{"How should it behave?", "[ ] Fail closed.", "[ ] Fail open.",
				"Neither.", "", "Retry twice, then refuse."},
		},
		{
			name:        "a multi-line question is recorded whole",
			args:        `{"question":"Ship it?\nThe migration is irreversible.","choices":["Ship","Hold"]}`,
			answer:      "Hold",
			wantSummary: "Hold",
			wantBody:    []string{"Ship it?", "The migration is irreversible.", "[ ] Ship", "[x] Hold"},
		},
		{
			name:        "a free-text question still gets the record, with no boxes",
			args:        `{"question":"What should the flag be called?"}`,
			answer:      "confine-to-workspace",
			wantSummary: "confine-to-workspace",
			wantBody:    []string{"What should the flag be called?"},
		},
		{
			name:        "with no choices the body starts at the answer's SECOND line",
			args:        `{"question":"What should the flag be called?"}`,
			answer:      "confine-to-workspace\n(keep the old name as an alias)",
			wantSummary: "confine-to-workspace",
			wantBody:    []string{"What should the flag be called?", "(keep the old name as an alias)"},
		},
		{
			name:        "a sloppy choices array degrades to the free-text record",
			args:        `{"question":"Pick one","choices":["  ","Only this one",7]}`,
			answer:      "Only this one",
			wantSummary: "Only this one",
			wantBody:    []string{"Pick one", "[x] Only this one"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			call := domain.ToolCall{ID: "1", Tool: "ask_user", Arguments: []byte(tc.args)}
			tv := presentToolCall(call, workspaceRoot{})
			tv.enrichWithResult(domain.ToolResult{CallID: "1", Content: tc.answer}, workspaceRoot{})

			if tv.Summary.Text != tc.wantSummary {
				t.Errorf("summary = %q, want %q (the answer, quoted)", tv.Summary.Text, tc.wantSummary)
			}
			if !tv.Summary.quoted {
				t.Error("the answer on the branch must stay marked quoted — it is the human's own spelling")
			}
			body := make([]string, 0, tv.Details.len())
			for _, d := range tv.Details.all() {
				if d.Kind != detailPlain {
					t.Errorf("record line %q has kind %v, want detailPlain", d.Text, d.Kind)
				}
				body = append(body, d.Text)
			}
			if !reflect.DeepEqual(body, tc.wantBody) {
				t.Errorf("record body = %q, want %q", body, tc.wantBody)
			}
		})
	}
}

// A question still on the screen keeps the summary-only card it always had: the popup is the live
// view of the offering while the human answers, and the record materialises only with the answer
// (the ratified timing call). This is the row that would fail if the body were built from the
// arguments at presentation time, the way an edit tool's is.
func TestAskUserPendingCallHasNoRecord(t *testing.T) {
	t.Parallel()

	tv := presentToolCall(domain.ToolCall{ID: "1", Tool: "ask_user",
		Arguments: []byte(`{"question":"Which mode?","choices":["Plan","Auto"]}`)}, workspaceRoot{})

	if tv.Summary.Text != "" || tv.Details.len() != 0 {
		t.Errorf("pending question outcome = %+v / %+v; want both halves empty", tv.Summary, tv.Details)
	}
	if tv.Target != "Which mode?" {
		t.Errorf("target = %q, want the question's first line", tv.Target)
	}
	if !groupable(tv) {
		t.Error("a pending question must still group with its neighbours")
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

	const longDiff = 25 // well past the collapsed budget: what is retained answers to no cap at all
	long := strings.TrimSuffix(strings.Repeat("+ added\n", longDiff), "\n")
	whole := diffBody(long)
	if len(whole) != longDiff {
		t.Fatalf("retained diff has %d lines, want every one of the %d", len(whole), longDiff)
	}
	for i, d := range whole {
		if d.Kind != detailDiffAdded {
			t.Errorf("line %d (%q): kind = %v, want %v (no synthesized marker line)", i, d.Text, d.Kind, detailDiffAdded)
		}
	}
}

// changedBody is an edit view's body as plain strings, checked line by line against the one
// pairing the paint depends on: a "- " line must be red and a "+ " line green. Reading the tag off
// the text is exact here because the tag is what the producer put there (changedLines).
func changedBody(t *testing.T, tv toolView) []string {
	t.Helper()
	out := make([]string, 0, tv.Details.len())
	for _, d := range tv.Details.all() {
		want := detailPlain
		switch {
		case strings.HasPrefix(d.Text, "- "):
			want = detailDiffRemoved
		case strings.HasPrefix(d.Text, "+ "):
			want = detailDiffAdded
		}
		if d.Kind != want {
			t.Errorf("line %q: kind = %v, want %v — the tag and the colour must agree", d.Text, d.Kind, want)
		}
		out = append(out, d.Text)
	}
	return out
}

// TestEditCallsCarryTheirChangedLines pins the edit tools' display-only diff body: it is derived
// from the call's OWN ARGUMENTS at presentation time — before any result exists — so the block
// shows what the model asked to change without the tool reporting it and without a byte more
// crossing the wire.
//
// The three tools' arguments say the same thing three ways and the body reads identically: a pair
// per replacement, its removed lines then its inserted lines, pairs in the order the call listed
// them. Multi-line strings stay line per line (a body is lines, not a blob), a trailing newline is
// the last line's terminator rather than a blank line of its own, and a side that changes nothing
// contributes nothing.
//
// The degraded rows carry the weight: arguments that are absent, malformed or of the wrong shape
// yield NO body — the call renders exactly as it did before this existed — because a hostile or
// merely broken model must not be able to turn a card into a panic or into a claim about a change
// nobody asked for.
func TestEditCallsCarryTheirChangedLines(t *testing.T) {
	cases := []struct {
		name string
		call domain.ToolCall
		want []string
	}{
		{
			name: "single: one pair, one line each",
			call: domain.ToolCall{ID: "1", Tool: "single_find_and_replace",
				Arguments: []byte(`{"path":"main.go","oldText":"a := 1","newText":"a := 2"}`)},
			want: []string{"- a := 1", "+ a := 2"},
		},
		{
			name: "single: multi-line strings stay line per line, removed before inserted",
			call: domain.ToolCall{ID: "2", Tool: "single_find_and_replace",
				Arguments: []byte(`{"path":"main.go","oldText":"one\ntwo","newText":"uno\ndos\ntres"}`)},
			want: []string{"- one", "- two", "+ uno", "+ dos", "+ tres"},
		},
		{
			name: "single: a trailing newline terminates the last line, it is not a line",
			call: domain.ToolCall{ID: "3", Tool: "single_find_and_replace",
				Arguments: []byte(`{"path":"main.go","oldText":"one\n","newText":"uno\n"}`)},
			want: []string{"- one", "+ uno"},
		},
		{
			name: "single: a deletion inserts nothing, so it shows nothing green",
			call: domain.ToolCall{ID: "4", Tool: "single_find_and_replace",
				Arguments: []byte(`{"path":"main.go","oldText":"gone","newText":""}`)},
			want: []string{"- gone"},
		},
		{
			name: "single: no replacement arguments at all → no body",
			call: domain.ToolCall{ID: "5", Tool: "single_find_and_replace",
				Arguments: []byte(`{"path":"main.go"}`)},
			want: nil,
		},
		{
			name: "multi: every pair, in argument order",
			call: domain.ToolCall{ID: "6", Tool: "multi_find_and_replace",
				Arguments: []byte(`{"path":"main.go","replacements":[` +
					`{"oldText":"first","newText":"1st"},{"oldText":"second","newText":"2nd"}]}`)},
			want: []string{"- first", "+ 1st", "- second", "+ 2nd"},
		},
		{
			name: "multi: an entry of the wrong shape is skipped, the rest still shows",
			call: domain.ToolCall{ID: "7", Tool: "multi_find_and_replace",
				Arguments: []byte(`{"path":"main.go","replacements":["nonsense",{"oldText":"a","newText":"b"}]}`)},
			want: []string{"- a", "+ b"},
		},
		{
			name: "multi: replacements of the wrong type → no body",
			call: domain.ToolCall{ID: "8", Tool: "multi_find_and_replace",
				Arguments: []byte(`{"path":"main.go","replacements":"all of them"}`)},
			want: nil,
		},
		{
			name: "edit_existing_file: a patch shows its hunks' changed lines, context dropped",
			call: domain.ToolCall{ID: "9", Tool: "edit_existing_file",
				Arguments: []byte(`{"path":"main.go","content":"*** Begin Patch\n*** Update File: main.go\n` +
					`@@\n ctx\n-old one\n+new one\n@@\n-old two\n+new two\n*** End Patch"}`)},
			want: []string{"- old one", "+ new one", "- old two", "+ new two"},
		},
		{
			name: "edit_existing_file: full replacement content removes nothing and inserts the lot",
			call: domain.ToolCall{ID: "10", Tool: "edit_existing_file",
				Arguments: []byte(`{"path":"main.go","content":"package main\n\nfunc main() {}\n"}`)},
			want: []string{"+ package main", "+ ", "+ func main() {}"},
		},
		{
			name: "edit_existing_file: no content argument → no body",
			call: domain.ToolCall{ID: "11", Tool: "edit_existing_file",
				Arguments: []byte(`{"path":"main.go"}`)},
			want: nil,
		},
		{
			name: "malformed arguments degrade to no body rather than to a guess",
			call: domain.ToolCall{ID: "12", Tool: "single_find_and_replace",
				Arguments: []byte("{not json")},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tv := presentToolCall(tc.call, workspaceRoot{})
			if got := changedBody(t, tv); strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("body = %q, want %q", got, tc.want)
			}
		})
	}
}

// An edit's body is retained WHOLE, however big the edit: the four-row shape a reader sees is the
// collapsed paint's cap on these lines (collapsedBodyCap, render.go), never a truncation performed
// here — which is what makes expanding the block able to show the change.
func TestEditBodyRetainsEveryChangedLine(t *testing.T) {
	const lines = 40 // far past the collapsed budget, so a build-time cap could not hide in the noise
	inserted := strings.TrimSuffix(strings.Repeat("added\\n", lines), "\\n")
	tv := presentToolCall(domain.ToolCall{ID: "1", Tool: "single_find_and_replace",
		Arguments: []byte(`{"path":"main.go","oldText":"gone","newText":"` + inserted + `"}`)}, workspaceRoot{})

	if got, want := tv.Details.len(), lines+1; got != want {
		t.Errorf("body has %d lines, want the removed line plus all %d inserted", got, lines)
	}
}

// A long changed line is clipped to the same 160-rune ceiling every other detail line answers to
// — a minified blob pasted into a replacement must not flood a row — and the clip counts RUNES, so
// a multi-byte edit is never cut mid-character.
func TestEditBodyClipsALongChangedLine(t *testing.T) {
	long := strings.Repeat("é", detailClipRunes+50)
	tv := presentToolCall(domain.ToolCall{ID: "1", Tool: "single_find_and_replace",
		Arguments: []byte(`{"path":"main.go","oldText":"x","newText":"` + long + `"}`)}, workspaceRoot{})

	body := tv.Details.all()
	if len(body) != 2 {
		t.Fatalf("body = %+v, want the removed line and the inserted one", body)
	}
	if got := len([]rune(body[1].Text)); got != detailClipRunes+1 { // + the ellipsis clipRunes appends
		t.Errorf("inserted line is %d runes, want it clipped to %d plus the ellipsis", got, detailClipRunes)
	}
}

// TestWriteCallCarriesTheWrittenLines pins write_file's display-only body, the other half of the
// same rule the edit tools follow: what a write puts in a file is stated in its ARGUMENTS, so the
// block hangs those lines beneath its branch from the moment the call is announced — every one of
// them green, because a write inserts the lot and removes nothing.
//
// The degraded rows carry the same weight they do for an edit: content that is absent, empty or of
// the wrong type yields NO body rather than a panic or a claim about a write nobody asked for. An
// empty write is the interesting one — it genuinely writes nothing, so a body of one blank line
// would be a line the call never asked for.
func TestWriteCallCarriesTheWrittenLines(t *testing.T) {
	cases := []struct {
		name string
		args string
		want []string
	}{
		{
			name: "every line of the content, all of it green",
			args: `{"path":"notes.txt","content":"alpha\nbeta\ngamma"}`,
			want: []string{"+ alpha", "+ beta", "+ gamma"},
		},
		{
			name: "a trailing newline terminates the last line, it is not a line",
			args: `{"path":"notes.txt","content":"alpha\nbeta\n"}`,
			want: []string{"+ alpha", "+ beta"},
		},
		{
			name: "single-line content still carries a one-line body",
			args: `{"path":"notes.txt","content":"hello"}`,
			want: []string{"+ hello"},
		},
		{
			name: "empty content writes nothing, so it shows nothing",
			args: `{"path":"notes.txt","content":""}`,
			want: nil,
		},
		{
			name: "no content argument → no body",
			args: `{"path":"notes.txt"}`,
			want: nil,
		},
		{
			name: "content of the wrong type → no body",
			args: `{"path":"notes.txt","content":42}`,
			want: nil,
		},
		{
			name: "malformed arguments degrade to no body rather than to a guess",
			args: "{not json",
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tv := presentToolCall(domain.ToolCall{ID: "1", Tool: "write_file", Arguments: []byte(tc.args)}, workspaceRoot{})
			if got := changedBody(t, tv); strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("body = %q, want %q", got, tc.want)
			}
		})
	}
}

// A write's two halves say different things and neither is derived from the other: the LINE COUNT
// its own request states fills the branch row's outcome slot (the ratified table asks for lines,
// where the tool reports bytes) and the argument-derived lines hang beneath it — including when
// there is only one of them. Nothing is promoted into that slot, because it is already taken.
func TestWriteBodySurvivesItsByteCountSummary(t *testing.T) {
	tv := presentToolCall(domain.ToolCall{ID: "1", Tool: "write_file",
		Arguments: []byte(`{"path":"notes.txt","content":"hello"}`)}, workspaceRoot{})
	tv.enrichWithResult(domain.ToolResult{CallID: "1", Content: "wrote 5 bytes to notes.txt",
		Summary: domain.WroteBytes{Bytes: 5}}, workspaceRoot{})

	if got := tv.Summary.Text; got != "1 line" {
		t.Errorf("summary = %q, want the written line count on the branch", got)
	}
	if got := changedBody(t, tv); len(got) != 1 || got[0] != "+ hello" {
		t.Errorf("body = %q, want the one written line beneath the branch", got)
	}
}

// TestDiffStatSpansTheWholeDiff: the diffstat riding the branch describes the whole diff even
// when the collapsed paint stops at the house budget (collapsedBodyCap) — a truncated paint
// cannot tell you how big the change was, and the stat no longer comes from the body's lines at
// all but from the tool's domain.DiffStat, counted over the diff operations themselves
// (internal/tools). The outcome itself keeps every line, so what the paint hides is only hidden.
func TestDiffStatSpansTheWholeDiff(t *testing.T) {
	const longDiff = 25 // well past the collapsed budget, so the stat and the paint cannot agree by luck
	long := strings.TrimSuffix(strings.Repeat("+ added\n", longDiff), "\n")
	tv := presentToolCall(domain.ToolCall{ID: "1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}, workspaceRoot{})
	tv.enrichWithResult(domain.ToolResult{CallID: "1", Content: long, Summary: domain.DiffStat{Added: longDiff}}, workspaceRoot{})

	if want := "+" + strconv.Itoa(longDiff) + " −0"; tv.Summary.Text != want {
		t.Errorf("diffstat = %q, want %q", tv.Summary.Text, want)
	}
	if tv.Details.len() != longDiff {
		t.Errorf("body has %d lines, want the whole %d", tv.Details.len(), longDiff)
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

// TestToolStat is the ratified table's `<tool-top-level-details>` column in one place: for each
// tool, the arguments and the result its slot is worded from, and the phrase that must come out
// (docs/layout/tool-layout.md). The rows fall in the three kinds the hooks come in — read off a
// typed domain.ToolSummary, read off the call's own arguments, read off a header the tool wrote —
// plus the two answers that are not a phrase at all: the deliberate blank (`—`) and the decline
// that leaves a tool's prose floor in the slot.
func TestToolStat(t *testing.T) {
	cases := []struct {
		name   string
		tool   string
		args   map[string]any
		result domain.ToolResult
		want   string
		wantOK bool
	}{
		// Typed summaries.
		{name: "read words its span as a line count", tool: "read_file", result: domain.ToolResult{Summary: domain.ReadSpan{Start: 1, End: 100, Total: 120}}, want: "100 lines", wantOK: true},
		{name: "a one-line read is singular", tool: "read_file", result: domain.ToolResult{Summary: domain.ReadSpan{Start: 7, End: 7, Total: 9}}, want: "1 line", wantOK: true},
		{name: "an empty file reads zero, never a negative", tool: "read_file", result: domain.ToolResult{Summary: domain.ReadSpan{Start: 1, End: 0}}, want: "0 lines", wantOK: true},
		{name: "open words the body's line count", tool: "open_file", result: domain.ToolResult{Summary: domain.OpenedFile{Lines: 3, Locate: "x", LocatedOn: []int{1}}}, want: "3 lines", wantOK: true},
		{name: "list keeps the fixed 'entries' plural", tool: "list_dir", result: domain.ToolResult{Summary: domain.ListedEntries{Total: 1}}, want: "1 entries", wantOK: true},
		{name: "grep words hits", tool: "grep", result: domain.ToolResult{Summary: domain.MatchedLines{Total: 3}}, want: "3 hits", wantOK: true},
		{name: "grep finding nothing is a number", tool: "grep", result: domain.ToolResult{Summary: domain.MatchedLines{Total: 0}}, want: "0 hits", wantOK: true},
		{name: "search words results", tool: "web_search", result: domain.ToolResult{Summary: domain.SearchHits{Count: 1}}, want: "1 result", wantOK: true},
		{name: "diff preview words the typed diffstat", tool: "view_diff", result: domain.ToolResult{Summary: domain.DiffStat{Added: 8, Removed: 3}}, want: "+8 −3", wantOK: true},
		{name: "a summary-less diff keeps its sentence", tool: "view_diff", result: domain.ToolResult{Content: "No changes detected"}, wantOK: false},

		// Facts about the REQUEST.
		{name: "write counts the lines it writes", tool: "write_file", args: map[string]any{"content": "a\nb\nc\n"}, want: "3 lines", wantOK: true},
		{name: "edit counts the patch it sends", tool: "edit_existing_file", args: map[string]any{"content": "*** Begin Patch\n@@\n-old\n+new\n+extra\n"}, want: "+2 −1", wantOK: true},
		{name: "edit with whole content removes nothing", tool: "edit_existing_file", args: map[string]any{"content": "a\nb\n"}, want: "+2 −0", wantOK: true},
		{name: "replace counts its one pair", tool: "single_find_and_replace", args: map[string]any{"oldText": "a\nb", "newText": "c"}, want: "+1 −2", wantOK: true},
		{name: "an empty replace keeps its prose floor", tool: "single_find_and_replace", args: map[string]any{"path": "a.go"}, wantOK: false},
		{name: "a batch replace counts changes, not lines", tool: "multi_find_and_replace", args: map[string]any{"replacements": []any{map[string]any{"oldText": "a", "newText": "b"}, map[string]any{"oldText": "c", "newText": "d"}}}, want: "2 changes", wantOK: true},

		// Structural facts about the call.
		{name: "a terminal result that is not an error exited zero", tool: "terminal", result: domain.ToolResult{Content: "hi"}, want: "exit 0", wantOK: true},
		{name: "python reads the same way", tool: "python_exec", result: domain.ToolResult{Content: "42"}, want: "exit 0", wantOK: true},
		{name: "diagnostics with no findings is clean", tool: "diagnostics", result: domain.ToolResult{Content: "No diagnostics: a.go looks clean."}, want: "clean", wantOK: true},
		{name: "a delegation that returned is done", tool: "sub_agent", result: domain.ToolResult{Content: "report"}, want: "done", wantOK: true},

		// The table's `—`.
		{name: "copy states a blank slot", tool: "copy_file", result: domain.ToolResult{Content: "copied a.txt to b.txt"}, want: "", wantOK: true},
		{name: "move states a blank slot", tool: "move_file", result: domain.ToolResult{Content: "moved a.txt to b.txt"}, want: "", wantOK: true},
		{name: "delete states a blank slot", tool: "delete_file", result: domain.ToolResult{Content: "deleted a.txt"}, want: "", wantOK: true},
		{name: "git branch states a blank slot", tool: "git_branch", result: domain.ToolResult{Content: "* main"}, want: "", wantOK: true},
		{name: "present states a blank slot", tool: "present_document", result: domain.ToolResult{Content: "Presented a.md"}, want: "", wantOK: true},

		// Headers the tools write themselves.
		{name: "tests word the bare verdict", tool: "run_tests", result: domain.ToolResult{Content: "PASS (go test)\nok  \tpkg\t0.4s"}, want: "PASS", wantOK: true},
		{name: "a failing run words FAIL", tool: "run_tests", result: domain.ToolResult{Content: "FAIL (pytest) — 3 failing tests"}, want: "FAIL", wantOK: true},
		{name: "output in no verdict shape keeps its floor", tool: "run_tests", result: domain.ToolResult{Content: "go: no test files"}, wantOK: false},
		{name: "find files reads its own total", tool: "find_files", result: domain.ToolResult{Content: "[12 files found, showing 1-12]\na.go"}, want: "12 files", wantOK: true},
		{name: "find files states the empty case", tool: "find_files", result: domain.ToolResult{Content: "No files found"}, want: "0 files", wantOK: true},
		{name: "git status sums its section counts", tool: "git_status", result: domain.ToolResult{Content: "On branch main\n\nStaged (2):\n  a.go\n  b.go\n\nUntracked (1):\n  c.go"}, want: "3 changed", wantOK: true},
		{name: "a clean tree changed nothing", tool: "git_status", result: domain.ToolResult{Content: "On branch main\n\nWorking tree clean"}, want: "0 changed", wantOK: true},
		{name: "git log counts its commit lines", tool: "git_log", result: domain.ToolResult{Content: "a1b2c3d 2026-08-10 first\ne4f5a6b 2026-08-09 second"}, want: "2 commits", wantOK: true},
		{name: "git log states the empty case", tool: "git_log", result: domain.ToolResult{Content: "No commits found"}, want: "0 commits", wantOK: true},
		{name: "git commit words the short hash of the oneline it returns", tool: "git_commit", result: domain.ToolResult{Content: "6fd6ff7 add the thing"}, want: "6fd6ff7", wantOK: true},
		{name: "git commit reads the hash of git's own output on the fallback branch", tool: "git_commit", result: domain.ToolResult{Content: "[main a1b2c3d] add the thing\n 2 files changed"}, want: "a1b2c3d", wantOK: true},
		{name: "a commit in another shape keeps its floor", tool: "git_commit", result: domain.ToolResult{Content: "nothing to commit, working tree clean"}, wantOK: false},
		{name: "git diff counts its tagged lines, not its file headers", tool: "git_diff_range", result: domain.ToolResult{Content: "--- a/x.go\n+++ b/x.go\n@@ -1 +1,2 @@\n-old\n+new\n+more"}, want: "+2 −1", wantOK: true},
		{name: "a --stat diff has no tagged lines and keeps its floor", tool: "git_diff_range", result: domain.ToolResult{Content: " x.go | 3 +++\n 1 file changed"}, wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, known := toolRegistry[tc.tool]
			if !known || (p.stat == nil && p.argStat == nil) {
				t.Fatalf("%s has no stat hook — the table gives it a slot", tc.tool)
			}
			got, ok := "", false
			if p.argStat != nil {
				got, ok = p.argStat(tc.args)
			} else {
				got, ok = p.stat(tc.result)
			}
			if ok != tc.wantOK {
				t.Fatalf("%s stat ok = %v, want %v (got %q)", tc.tool, ok, tc.wantOK, got)
			}
			if ok && got != tc.want {
				t.Errorf("%s stat = %q, want %q", tc.tool, got, tc.want)
			}
		})
	}
}

// A tool whose result carries no typed summary declines rather than inventing one, so the prose
// floor stays: a third-party tool (which can never emit a summary — the sum is sealed) renders as
// it always did.
func TestToolStatDeclinesWithoutATypedSummary(t *testing.T) {
	for _, name := range []string{"read_file", "open_file", "list_dir", "grep", "web_search", "view_diff"} {
		if _, ok := toolRegistry[name].stat(domain.ToolResult{Content: "prose"}); ok {
			t.Errorf("%s must decline a summary-less result", name)
		}
	}
}

// A stat that DECLINES leaves the prose floor exactly where it was — the degraded card is the
// tool's own words, never a blank slot or an invented number. git_log run against a ref with
// nothing to show is that floor end to end: its stat recognises no commit lines, so the extractor's
// own "(no output)" phrase keeps the slot.
func TestDecliningStatKeepsTheProseFloor(t *testing.T) {
	tv := presentToolCall(domain.ToolCall{ID: "1", Tool: "git_log", Arguments: []byte(`{"ref":"HEAD"}`)}, workspaceRoot{})
	tv.enrichWithResult(domain.ToolResult{CallID: "1", Content: "\n"}, workspaceRoot{})
	if tv.Summary.Text != "(no output)" {
		t.Errorf("summary = %q, want the extractor's own phrase", tv.Summary.Text)
	}
}

// open_file's locate report moved off the branch and into the body when the table gave the slot to
// the line count: the term is in the target, the lines it was found on are here, and an over-long
// term is clipped like any other detail line so a model asking to locate a minified blob cannot
// flood the row.
func TestOpenFileBodyRecordsTheLocateReport(t *testing.T) {
	matched := openFileBody(domain.ToolResult{Summary: domain.OpenedFile{Lines: 40, Locate: "func main", LocatedOn: []int{5, 9}}})
	if len(matched) != 1 || matched[0].Text != `Located "func main" on lines: 5, 9` {
		t.Errorf("located lines = %+v", matched)
	}
	missed := openFileBody(domain.ToolResult{Summary: domain.OpenedFile{Lines: 40, Locate: "zzz"}})
	if len(missed) != 1 || missed[0].Text != `Located "zzz" on no lines` {
		t.Errorf("a locate that matched nothing = %+v", missed)
	}
	if none := openFileBody(domain.ToolResult{Summary: domain.OpenedFile{Lines: 3}}); len(none) != 0 {
		t.Errorf("a call that asked for no term has no report: %+v", none)
	}
	long := strings.Repeat("x", detailClipRunes+40)
	clipped := openFileBody(domain.ToolResult{Summary: domain.OpenedFile{Lines: 2, Locate: long, LocatedOn: []int{1}}})
	if len(clipped) != 1 || len([]rune(clipped[0].Text)) != detailClipRunes+1 { // +1 for the ellipsis
		t.Errorf("locate line is not clipped to %d runes: %+v", detailClipRunes, clipped)
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

// argumentDetails is the labelled rendering the approval prompt reads a decision off: one `name:`
// line per argument in the order the model wrote them, the value's own real lines indented beneath
// it — a multi-line string becoming the lines it will actually run rather than one escaped blob —
// and no envelope around the set. A value with no flat shape is the one place JSON survives, under
// its own label, because nothing else states its structure without lying about it.
func TestArgumentDetailsLabelsEachArgument(t *testing.T) {
	cases := []struct {
		name string
		args string
		want []string
	}{
		{
			"a single-line value",
			`{"path":"notes.txt"}`,
			[]string{"path:", "  notes.txt"},
		},
		{
			"a multi-line value keeps its own lines",
			`{"command":"cd /ws/a\ngit status\ngit diff"}`,
			[]string{"command:", "  cd /ws/a", "  git status", "  git diff"},
		},
		{
			"several arguments in wire order",
			`{"command":"git status","workdir":"/ws/b","timeout":30}`,
			[]string{"command:", "  git status", "workdir:", "  /ws/b", "timeout:", "  30"},
		},
		{
			"wire order is the model's, not the alphabet's",
			`{"workdir":"/ws/b","command":"git status"}`,
			[]string{"workdir:", "  /ws/b", "command:", "  git status"},
		},
		{
			"a non-string scalar keeps the literal the model sent",
			`{"count":42,"force":true,"note":null}`,
			[]string{"count:", "  42", "force:", "  true", "note:", "  null"},
		},
		{
			"a value with no flat shape is indented JSON under its own label",
			`{"opts":{"deep":1}}`,
			[]string{"opts:", "  {", `    "deep": 1`, "  }"},
		},
		{
			"an empty object has nothing to label",
			`{}`,
			nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := detailLineTexts(argumentDetails(json.RawMessage(tc.args)))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("argumentDetails(%s) =\n%#v\nwant\n%#v", tc.args, got, tc.want)
			}
		})
	}
}

// Arguments with no names to label are shown as they arrived (prettyJSONDetails), because a half
// labelled body would be a claim about the call that the bytes do not support. That holds for a
// blob whose tail is garbage too — a stray `}`/`]` behind the object makes the payload malformed,
// so it falls back rather than being labelled as if the tail were not there. Absent or null
// arguments add no lines at all.
func TestArgumentDetailsFallsBackWhereThereIsNothingToLabel(t *testing.T) {
	cases := []struct {
		name string
		args string
		want []string
	}{
		{"a malformed blob", `{"command":`, []string{`{"command":`}},
		{"not an object", `["a","b"]`, []string{"[", `  "a",`, `  "b"`, "]"}},
		{"a second document behind the first", `{"a":1} {"b":2}`, []string{`{"a":1} {"b":2}`}},
		{"a stray brace behind the object", `{"a":1}}`, []string{`{"a":1}}`}},
		{"a stray bracket behind the object", `{"a":1}]`, []string{`{"a":1}]`}},
		{"loose text behind the object", `{"a":1} trailing`, []string{`{"a":1} trailing`}},
		{"absent arguments", ``, nil},
		{"null arguments", `null`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := detailLineTexts(argumentDetails(json.RawMessage(tc.args)))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("argumentDetails(%s) =\n%#v\nwant\n%#v", tc.args, got, tc.want)
			}
		})
	}
}

// detailTexts reads the plain text off a body's lines, so a rendering is compared as the lines a
// reader sees rather than as the struct carrying them.
func detailLineTexts(lines []detailLine) []string {
	if lines == nil {
		return nil
	}
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = ln.Text
	}
	return out
}
