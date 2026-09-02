package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// writeTempFile creates name under root with the given content and returns its path.
func writeTempFile(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("setup: write %s: %v", name, err)
	}
	return path
}

func TestCountOccurrences(t *testing.T) {
	t.Parallel()

	cases := []struct {
		haystack, needle string
		want             int
	}{
		{"aaa bbb aaa", "aaa", 2},
		{"line one\nline two\nline three\n", "line two", 1},
		{"abcabc", "abc", 2},
		{"aaaa", "aa", 2}, // non-overlapping
		{"nothing here", "xyz", 0},
		{"anything", "", 0}, // empty needle never matches
	}
	for _, tc := range cases {
		if got := countOccurrences(tc.haystack, tc.needle); got != tc.want {
			t.Errorf("countOccurrences(%q, %q) = %d, want %d", tc.haystack, tc.needle, got, tc.want)
		}
	}
}

func TestSingleFindReplace_Execute(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	path := writeTempFile(t, root, "test.txt", "line one\nline two\nline three\n")

	result, err := NewSingleFindReplace(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "test.txt", "oldText": "line two", "newText": "line TWO"}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if !strings.Contains(result.Content, "replaced text") {
		t.Errorf("output %q does not confirm the replacement", result.Content)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "line one\nline TWO\nline three\n" {
		t.Errorf("file content = %q, want the single span replaced", string(got))
	}
}

// Both find-and-replace tools read before they write, and the read follows an in-root symlink, so
// a patched `docs/notes.md` can be .git/config read out and overwritten under a name that says
// otherwise. Each tool's success sentence must name the file the bytes came from, and both must
// carry it — a disclosure only the single form made would be dodged by sending the same edit as a
// one-element array.
func TestFindReplace_NameTheFileTheyRead(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	config := symlinkedReadFixture(t, root, "docs", "notes.md")
	realConfig := realPath(t, config)

	single, err := NewSingleFindReplace(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "docs/notes.md", "oldText": "[core]", "newText": "[clean]"}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if single.IsError {
		t.Fatalf("unexpected tool error: %q", single.Content)
	}
	if want := "replaced text in docs/notes.md → resolves to " + realConfig; single.Content != want {
		t.Errorf("Content = %q, want %q", single.Content, want)
	}

	// The write replaced the NAME rather than going through the link, so the redirect target is
	// still the file it was.
	if got := string(mustRead(t, config)); got != gitConfigFixture {
		t.Errorf("redirect target content = %q, want it untouched", got)
	}

	multiTarget := symlinkedReadFixture(t, root, "notes", "entry.md")
	multi, err := NewMultiFindReplace(root).Execute(context.Background(),
		callWith(t, "c2", map[string]any{
			"path":         "notes/entry.md",
			"replacements": []map[string]any{{"oldText": "[core]", "newText": "[clean]"}},
		}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if multi.IsError {
		t.Fatalf("unexpected tool error: %q", multi.Content)
	}
	if want := "applied 1 replacement to notes/entry.md → resolves to " + realPath(t, multiTarget); multi.Content != want {
		t.Errorf("Content = %q, want %q", multi.Content, want)
	}

	// An ordinary path reports the sentence it always did.
	writeTempFile(t, root, "plain.md", "[core]\n")
	ordinary, err := NewSingleFindReplace(root).Execute(context.Background(),
		callWith(t, "c3", map[string]any{"path": "plain.md", "oldText": "[core]", "newText": "[clean]"}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if ordinary.Content != "replaced text in plain.md" {
		t.Errorf("Content = %q, want the bare sentence for a path that resolves to itself", ordinary.Content)
	}
}

func TestSingleFindReplace_ToolErrors(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	writeTempFile(t, root, "dup.txt", "aaa bbb aaa")
	writeTempFile(t, root, "ok.txt", "hello world")
	tool := NewSingleFindReplace(root)

	cases := []struct {
		name        string
		args        map[string]any
		wantContain string
		// unchanged names the file whose content must be byte-identical after the call.
		unchanged string
		want      string
	}{
		{"old text not found", map[string]any{"path": "ok.txt", "oldText": "absent", "newText": "x"}, "not found", "ok.txt", "hello world"},
		{"old text found twice", map[string]any{"path": "dup.txt", "oldText": "aaa", "newText": "ccc"}, "found 2 times", "dup.txt", "aaa bbb aaa"},
		{"missing path", map[string]any{"oldText": "a", "newText": "b"}, "path is required", "", ""},
		{"missing oldText", map[string]any{"path": "ok.txt", "newText": "b"}, "oldText is required", "ok.txt", "hello world"},
		{"file not found", map[string]any{"path": "nope.txt", "oldText": "a", "newText": "b"}, "file not found", "", ""},
		{"path traversal", map[string]any{"path": "../../../etc/passwd", "oldText": "root", "newText": "x"}, "outside the workspace", "", ""},
		{"oversized newText", map[string]any{"path": "ok.txt", "oldText": "hello", "newText": strings.Repeat("x", maxFileContentBytes+1)}, "exceeds maximum size", "ok.txt", "hello world"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := tool.Execute(context.Background(), callWith(t, "c1", tc.args))
			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if !result.IsError {
				t.Fatalf("IsError = false, want true (content: %q)", result.Content)
			}
			if !strings.Contains(result.Content, tc.wantContain) {
				t.Errorf("content %q does not contain %q", result.Content, tc.wantContain)
			}
			if tc.unchanged != "" {
				got, _ := os.ReadFile(filepath.Join(root, tc.unchanged))
				if string(got) != tc.want {
					t.Errorf("file %s changed on a failing call: %q", tc.unchanged, string(got))
				}
			}
		})
	}
}

func TestMultiFindReplace_AppliesSequentially(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	path := writeTempFile(t, root, "test.txt", "line one\nline two\nline three\n")

	result, err := NewMultiFindReplace(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{
			"path": "test.txt",
			"replacements": []map[string]any{
				{"oldText": "line one", "newText": "first line"},
				{"oldText": "line two", "newText": "second line"},
				{"oldText": "line three", "newText": "third line"},
			},
		}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if !strings.Contains(result.Content, "3 replacements") {
		t.Errorf("output %q does not report 3 replacements", result.Content)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "first line\nsecond line\nthird line\n" {
		t.Errorf("file content = %q, want all three replaced", string(got))
	}
}

func TestMultiFindReplace_SequentialDependentEdit(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	path := writeTempFile(t, root, "test.txt", "hello world")

	// Edit #1 introduces "MARKER world" that edit #2 then targets (oracle vector).
	_, err := NewMultiFindReplace(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{
			"path": "test.txt",
			"replacements": []map[string]any{
				{"oldText": "hello", "newText": "MARKER"},
				{"oldText": "MARKER world", "newText": "goodbye universe"},
			},
		}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "goodbye universe" {
		t.Errorf("file content = %q, want %q", string(got), "goodbye universe")
	}
}

func TestMultiFindReplace_FailsAtomically(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	original := "line one\nline two\nline three\n"
	path := writeTempFile(t, root, "test.txt", original)

	// Replacement #2 cannot be found; the whole call must fail and leave the file intact.
	result, err := NewMultiFindReplace(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{
			"path": "test.txt",
			"replacements": []map[string]any{
				{"oldText": "line one", "newText": "FIRST"},
				{"oldText": "DOES NOT EXIST", "newText": "x"},
			},
		}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("IsError = false, want true")
	}
	if !strings.Contains(result.Content, "replacement #2") || !strings.Contains(result.Content, "not found") {
		t.Errorf("error %q does not name replacement #2 as not found", result.Content)
	}

	got, _ := os.ReadFile(path)
	if string(got) != original {
		t.Errorf("file changed on an atomic failure: %q", string(got))
	}
}

func TestMultiFindReplace_DuplicateCreatedBySequentialEdit(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	original := "alpha beta"
	path := writeTempFile(t, root, "test.txt", original)

	// Edit #1 turns "alpha" into "beta", so "beta" then appears twice for edit #2 (oracle vector).
	result, err := NewMultiFindReplace(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{
			"path": "test.txt",
			"replacements": []map[string]any{
				{"oldText": "alpha", "newText": "beta"},
				{"oldText": "beta", "newText": "gamma"},
			},
		}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("IsError = false, want true")
	}
	if !strings.Contains(result.Content, "replacement #2") || !strings.Contains(result.Content, "found 2 times") {
		t.Errorf("error %q does not report replacement #2 found twice", result.Content)
	}

	got, _ := os.ReadFile(path)
	if string(got) != original {
		t.Errorf("file changed despite the failure: %q", string(got))
	}
}

func TestMultiFindReplace_DeletionWithEmptyNewText(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	path := writeTempFile(t, root, "test.txt", "line one\nline two\nline three\n")

	result, err := NewMultiFindReplace(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{
			"path":         "test.txt",
			"replacements": []map[string]any{{"oldText": "line two\n", "newText": ""}},
		}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "line one\nline three\n" {
		t.Errorf("file content = %q, want the deleted line gone", string(got))
	}
}

func TestMultiFindReplace_ValidationErrors(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	writeTempFile(t, root, "f.txt", "x")
	tool := NewMultiFindReplace(root)

	cases := []struct {
		name        string
		args        map[string]any
		wantContain string
	}{
		{"missing path", map[string]any{"replacements": []map[string]any{{"oldText": "a", "newText": "b"}}}, "path is required"},
		{"empty replacements", map[string]any{"path": "f.txt", "replacements": []map[string]any{}}, "non-empty array"},
		{"empty oldText", map[string]any{"path": "f.txt", "replacements": []map[string]any{{"oldText": "", "newText": "b"}}}, "replacements[0].oldText"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := tool.Execute(context.Background(), callWith(t, "c1", tc.args))
			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if !result.IsError {
				t.Fatalf("IsError = false, want true (content: %q)", result.Content)
			}
			if !strings.Contains(result.Content, tc.wantContain) {
				t.Errorf("content %q does not contain %q", result.Content, tc.wantContain)
			}
		})
	}
}

// TestFindReplace_CarryTheMarker proves both find-replace writers carry the
// workspaceScopedWriter marker and that view_diff does not.
func TestFindReplace_CarryTheMarker(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	writers := []domain.Tool{NewSingleFindReplace(root), NewMultiFindReplace(root)}
	for _, w := range writers {
		if !IsWorkspaceScopedWriter(w) {
			t.Errorf("%s does not carry the workspaceScopedWriter marker", w.Name())
		}
	}
	readers := []domain.Tool{NewViewDiff(root)}
	for _, r := range readers {
		if IsWorkspaceScopedWriter(r) {
			t.Errorf("%s wrongly carries the writer marker (it is read-only)", r.Name())
		}
	}
}

// editRegionsOf returns the domain.EditRegions a successful result attached, failing the test
// when the result carries no summary or carries some other variant.
func editRegionsOf(t *testing.T, result domain.ToolResult) domain.EditRegions {
	t.Helper()
	if result.Summary == nil {
		t.Fatalf("result carries no ToolSummary (content: %q)", result.Content)
	}
	regions, ok := result.Summary.(domain.EditRegions)
	if !ok {
		t.Fatalf("summary is %T, want domain.EditRegions", result.Summary)
	}
	return regions
}

// A successful single_find_and_replace attaches the Edit regions of what it actually wrote, with
// the line numbers and context a Split diff is painted from — and leaves its prose sentence, the
// half the MODEL reads, byte-for-byte as it was before regions existed.
func TestSingleFindReplace_RecordsEditRegions(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	writeTempFile(t, root, "test.txt", "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\n")

	result, err := NewSingleFindReplace(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "test.txt", "oldText": "l5", "newText": "L5"}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.Content != "replaced text in test.txt" {
		t.Errorf("content = %q, want the unchanged prose sentence", result.Content)
	}

	want := []domain.EditRegion{{
		BeforeStart: 2,
		AfterStart:  2,
		Leading:     []string{"l2", "l3", "l4"},
		Removed:     []string{"l5"},
		Inserted:    []string{"L5"},
		Trailing:    []string{"l6", "l7", "l8"},
	}}
	if got := editRegionsOf(t, result).Regions; !reflect.DeepEqual(got, want) {
		t.Errorf("regions = %+v, want %+v", got, want)
	}
}

// multi_find_and_replace summarises the file's whole change, not one summary per replacement: two
// replacements far enough apart to stay separate come back as two regions, each numbered where it
// landed, and their stat is the edit's own added/removed pair.
func TestMultiFindReplace_RecordsEditRegions(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, fmt.Sprintf("l%02d", i))
	}
	writeTempFile(t, root, "test.txt", strings.Join(lines, "\n")+"\n")

	result, err := NewMultiFindReplace(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{
			"path": "test.txt",
			"replacements": []map[string]any{
				{"oldText": "l03", "newText": "L03"},
				{"oldText": "l15", "newText": "L15"},
			},
		}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.Content != "applied 2 replacements to test.txt" {
		t.Errorf("content = %q, want the unchanged prose sentence", result.Content)
	}

	want := []domain.EditRegion{
		{
			BeforeStart: 1,
			AfterStart:  1,
			Leading:     []string{"l01", "l02"},
			Removed:     []string{"l03"},
			Inserted:    []string{"L03"},
			Trailing:    []string{"l04", "l05", "l06"},
		},
		{
			BeforeStart: 12,
			AfterStart:  12,
			Leading:     []string{"l12", "l13", "l14"},
			Removed:     []string{"l15"},
			Inserted:    []string{"L15"},
			Trailing:    []string{"l16", "l17", "l18"},
		},
	}
	regions := editRegionsOf(t, result)
	if !reflect.DeepEqual(regions.Regions, want) {
		t.Errorf("regions = %+v, want %+v", regions.Regions, want)
	}
	if stat := regions.Stat(); stat.Added != 2 || stat.Removed != 2 {
		t.Errorf("Stat() = %+v, want 2 added and 2 removed", stat)
	}
}

// No summary rides along unless regions were actually cut. A refused replacement never wrote
// anything, so there is nothing to paint; a replacement whose new text equals its old one wrote
// the same bytes back, and an empty EditRegions would claim the edit changed nothing rather than
// letting the card fall back to its argument-derived list.
func TestFindReplace_NoRegionsNoSummary(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	writeTempFile(t, root, "test.txt", "l1\nl2\nl3\n")

	cases := []struct {
		name    string
		tool    domain.Tool
		args    map[string]any
		wantErr bool
	}{
		{
			name:    "single: old text not found",
			tool:    NewSingleFindReplace(root),
			args:    map[string]any{"path": "test.txt", "oldText": "absent", "newText": "x"},
			wantErr: true,
		},
		{
			name: "multi: a later replacement fails and nothing is written",
			tool: NewMultiFindReplace(root),
			args: map[string]any{"path": "test.txt", "replacements": []map[string]any{
				{"oldText": "l2", "newText": "L2"},
				{"oldText": "absent", "newText": "x"},
			}},
			wantErr: true,
		},
		{
			name: "single: replacement text equal to what it replaces",
			tool: NewSingleFindReplace(root),
			args: map[string]any{"path": "test.txt", "oldText": "l2", "newText": "l2"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tc.tool.Execute(context.Background(), callWith(t, "c1", tc.args))
			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if result.IsError != tc.wantErr {
				t.Fatalf("IsError = %v, want %v (content: %q)", result.IsError, tc.wantErr, result.Content)
			}
			if result.Summary != nil {
				t.Errorf("result carries a %T summary, want none", result.Summary)
			}
		})
	}
}

// Both find-replace tools report the syntax verdict on the file they leave behind. The single
// tool's fixture is Go — the real parser, so the header states its verdict plainly.
func TestSingleFindReplace_Execute_AppendsTheSyntaxTrailer(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	path := writeTempFile(t, root, "main.go", "package main\n\nfunc main() {}\n// end\n")

	result, err := NewSingleFindReplace(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "main.go", "oldText": "// end", "newText": "}"}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("result is an error: %q — a breaking replacement still lands", result.Content)
	}
	if _, ok := result.Summary.(domain.EditRegions); !ok {
		t.Errorf("Summary = %#v, want the Edit regions a successful replacement always carries", result.Summary)
	}
	if !strings.HasPrefix(result.Content, "replaced text in main.go\n") {
		t.Errorf("Content = %q, want the trailer behind the tool's own sentence", result.Content)
	}
	if !strings.Contains(result.Content, "syntax check: 1 problem") || !strings.Contains(result.Content, "line 4:") {
		t.Errorf("Content = %q, want the located syntax problem", result.Content)
	}
	if want := "package main\n\nfunc main() {}\n}\n"; string(mustRead(t, path)) != want {
		t.Errorf("file = %q, want exactly the bytes the replacement produced", mustRead(t, path))
	}
}

// The multi tool's fixture is JavaScript, whose verdict comes from the bracket heuristic — known to
// false-positive on a regex literal — so its header must name itself a heuristic rather than let a
// model read a guess as a finding.
func TestMultiFindReplace_Execute_NamesTheHeuristicInItsTrailer(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	writeTempFile(t, root, "app.js", "function f() {\n  return 1;\n}\n")

	result, err := NewMultiFindReplace(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "app.js", "replacements": []map[string]any{
			{"oldText": "return 1;", "newText": "return 2;"},
			{"oldText": "}\n", "newText": ""},
		}}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("result is an error: %q — a breaking replacement still lands", result.Content)
	}
	if !strings.HasPrefix(result.Content, "applied 2 replacements to app.js\n") {
		t.Errorf("Content = %q, want the trailer behind the tool's own sentence", result.Content)
	}
	if !strings.Contains(result.Content, "syntax check (heuristic): ") {
		t.Errorf("Content = %q, want the heuristic header for javascript", result.Content)
	}
}

// A file whose language the checker does not know reads exactly as it always did.
func TestMultiFindReplace_Execute_NoTrailerForAnUnknownLanguage(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	writeTempFile(t, root, "notes.txt", "alpha\n")

	result, err := NewMultiFindReplace(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "notes.txt", "replacements": []map[string]any{
			{"oldText": "alpha", "newText": "func main( {"},
		}}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.Content != "applied 1 replacement to notes.txt" {
		t.Errorf("Content = %q, want the bare sentence for a path with no known language", result.Content)
	}
}
