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

// patchTestFile is the original content the patch-mode vectors edit (ported from the
// oracle's file-edit-tool.test.ts).
const patchTestFile = `import { foo } from "./foo";
import { bar } from "./bar";

function main() {
  const x = foo();
  const y = bar();
  return x + y;
}
`

func TestEditExistingFile_FullReplacement(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	path := writeTempFile(t, root, "replace.txt", "original content\n")

	result, err := NewEditExistingFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "replace.txt", "content": "new content\n"}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if !strings.Contains(result.Content, "updated") {
		t.Errorf("output %q does not confirm a full update", result.Content)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "new content\n" {
		t.Errorf("file content = %q, want full replacement", string(got))
	}
}

func TestEditExistingFile_SingleHunkPatch(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	path := writeTempFile(t, root, "patch.ts", patchTestFile)

	patch := strings.Join([]string{
		"*** Begin Patch",
		"*** Update File: patch.ts",
		"@@",
		"-  const x = foo();",
		"-  const y = bar();",
		"-  return x + y;",
		"+  const x = foo();",
		"+  const y = bar();",
		"+  const z = baz();",
		"+  return x + y + z;",
		"*** End Patch",
	}, "\n")

	result, err := NewEditExistingFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "patch.ts", "content": patch}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if !strings.Contains(result.Content, "applied patch") || !strings.Contains(result.Content, "1 hunk") {
		t.Errorf("output %q does not report a single applied hunk", result.Content)
	}

	got := string(mustRead(t, path))
	for _, want := range []string{"const z = baz();", "return x + y + z;", `import { foo } from "./foo";`} {
		if !strings.Contains(got, want) {
			t.Errorf("patched file missing %q\n%s", want, got)
		}
	}
}

func TestEditExistingFile_MultiHunkPatch(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	path := writeTempFile(t, root, "patch.ts", patchTestFile)

	patch := strings.Join([]string{
		"*** Begin Patch",
		"*** Update File: patch.ts",
		"@@",
		`-import { bar } from "./bar";`,
		`+import { bar } from "./bar";`,
		`+import { baz } from "./baz";`,
		"@@",
		"-  return x + y;",
		"+  return x + y + baz();",
		"*** End Patch",
	}, "\n")

	result, err := NewEditExistingFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "patch.ts", "content": patch}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if !strings.Contains(result.Content, "2 hunks") {
		t.Errorf("output %q does not report 2 hunks", result.Content)
	}

	got := string(mustRead(t, path))
	for _, want := range []string{`import { baz } from "./baz";`, "return x + y + baz();"} {
		if !strings.Contains(got, want) {
			t.Errorf("patched file missing %q\n%s", want, got)
		}
	}
}

func TestEditExistingFile_PreservesContextLines(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	path := writeTempFile(t, root, "patch.ts", patchTestFile)

	patch := strings.Join([]string{
		"*** Begin Patch",
		"*** Update File: patch.ts",
		"@@",
		" function main() {",
		"-  const x = foo();",
		"+  const x = foo(1);",
		" ",
		"*** End Patch",
	}, "\n")

	result, err := NewEditExistingFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "patch.ts", "content": patch}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}

	got := string(mustRead(t, path))
	for _, want := range []string{"const x = foo(1);", "function main() {"} {
		if !strings.Contains(got, want) {
			t.Errorf("patched file missing %q\n%s", want, got)
		}
	}
}

func TestEditExistingFile_PatchFailuresDoNotCorrupt(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	tool := NewEditExistingFile(root)

	cases := []struct {
		name        string
		patch       []string
		wantContain string
	}{
		{
			"hunk does not match",
			[]string{"*** Begin Patch", "*** Update File: patch.ts", "@@", "-  this line does not exist;", "+  replacement;", "*** End Patch"},
			"did not match",
		},
		{
			"empty patch",
			[]string{"*** Begin Patch", "*** End Patch"},
			"no hunks",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := writeTempFile(t, root, tc.name+".ts", patchTestFile)

			result, err := tool.Execute(context.Background(),
				callWith(t, "c1", map[string]any{"path": tc.name + ".ts", "content": strings.Join(tc.patch, "\n")}))
			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if !result.IsError {
				t.Fatalf("IsError = false, want true (content: %q)", result.Content)
			}
			if !strings.Contains(result.Content, tc.wantContain) {
				t.Errorf("content %q does not contain %q", result.Content, tc.wantContain)
			}

			if got := string(mustRead(t, path)); got != patchTestFile {
				t.Errorf("file corrupted on a failing patch:\n%s", got)
			}
		})
	}
}

// An edit is a READ followed by a write, and the read follows an in-root symlink (the write side
// refuses a symlinked parent, but a symlinked NAME is read through and then replaced). So
// "edit docs/notes.md" can disclose .git/config to the model and destroy the link in the same
// call, reported as an ordinary edit of docs/notes.md. The result sentence must name the file the
// bytes actually came from — resolved before the write, since the write leaves a plain file behind
// whatever the name was.
func TestEditExistingFile_NamesTheFileItRead(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	config := symlinkedReadFixture(t, root, "docs", "notes.md")
	realConfig := realPath(t, config)

	result, err := NewEditExistingFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "docs/notes.md", "content": "clean\n"}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if want := "updated docs/notes.md → resolves to " + realConfig; result.Content != want {
		t.Errorf("Content = %q, want %q", result.Content, want)
	}

	// The write went to the NAME, not through the link: the redirect target keeps its bytes.
	if got := string(mustRead(t, config)); got != gitConfigFixture {
		t.Errorf("redirect target content = %q, want it untouched", got)
	}

	// The patch branch carries the same disclosure.
	patched := symlinkedReadFixture(t, root, "notes", "entry.md")
	patch, err := NewEditExistingFile(root).Execute(context.Background(),
		callWith(t, "c2", map[string]any{
			"path":    "notes/entry.md",
			"content": "*** Begin Patch\n@@\n-[core]\n+[clean]\n*** End Patch\n",
		}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if patch.IsError {
		t.Fatalf("unexpected tool error: %q", patch.Content)
	}
	if want := "applied patch to notes/entry.md (1 hunk) → resolves to " + realPath(t, patched); patch.Content != want {
		t.Errorf("Content = %q, want %q", patch.Content, want)
	}

	// An ordinary edit reports the sentence it always did.
	writeTempFile(t, root, "plain.md", "old\n")
	ordinary, err := NewEditExistingFile(root).Execute(context.Background(),
		callWith(t, "c3", map[string]any{"path": "plain.md", "content": "new\n"}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if ordinary.Content != "updated plain.md" {
		t.Errorf("Content = %q, want the bare sentence for a path that resolves to itself", ordinary.Content)
	}
}

// gitConfigFixture is the content symlinkedReadFixture plants at the redirect target — a stand-in
// for any in-workspace file the operator did not mean to hand over.
const gitConfigFixture = "[core]\n"

// symlinkedReadFixture builds the read-side redirect the disclosure exists for: a real .git/config
// holding gitConfigFixture, a real directory dir, and dir/name as an in-root symlink pointing at
// that config. It returns the redirect target's path. The link is RELATIVE and stays inside the
// root, so the workspace fence follows it — which is exactly why the tool has to say so.
func symlinkedReadFixture(t *testing.T, root, dir, name string) string {
	t.Helper()

	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	config := filepath.Join(gitDir, "config")
	if err := os.WriteFile(config, []byte(gitConfigFixture), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(filepath.Join("..", ".git", "config"), filepath.Join(root, dir, name)); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}
	return config
}

func TestEditExistingFile_ToolErrors(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	tool := NewEditExistingFile(root)

	cases := []struct {
		name        string
		args        map[string]any
		wantContain string
	}{
		{"missing path", map[string]any{"content": "x"}, "path is required"},
		{"file not found", map[string]any{"path": "nope.txt", "content": "x"}, "file not found"},
		{"path escape", map[string]any{"path": "../escape.txt", "content": "x"}, "outside the workspace"},
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

func TestIsPatchContent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want bool
	}{
		{"*** Begin Patch\n@@\n-a\n+b\n*** End Patch", true},
		{"\n\n  *** Begin Patch\n", true}, // leading whitespace tolerated
		{"***BeginPatch", false},          // no space → not a patch marker
		{"just some new content\n", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isPatchContent(tc.in); got != tc.want {
			t.Errorf("isPatchContent(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// mustRead reads path or fails the test.
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Base(path), err)
	}
	return got
}

// A patch of two hunks summarises the file's whole change: one region per hunk, each numbered
// where it landed in the file rather than where the hunk sat in the patch — and the prose
// sentence the MODEL reads stays byte-for-byte what it was before regions existed.
func TestEditExistingFile_PatchRecordsARegionPerHunk(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, fmt.Sprintf("l%02d", i))
	}
	writeTempFile(t, root, "patch.txt", strings.Join(lines, "\n")+"\n")

	patch := strings.Join([]string{
		"*** Begin Patch",
		"*** Update File: patch.txt",
		"@@",
		"-l03",
		"+L03",
		"@@",
		"-l15",
		"+L15",
		"*** End Patch",
	}, "\n")

	result, err := NewEditExistingFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "patch.txt", "content": patch}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.Content != "applied patch to patch.txt (2 hunks)" {
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

// The full-content form summarises the DIFFERENCE between the file it read and the content it
// wrote, not the content itself: a whole-file replacement that touches one line comes back as one
// region, the same regions editRegions cuts from that pair.
func TestEditExistingFile_FullReplacementRecordsEditRegions(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	const before = "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\n"
	const after = "l1\nl2\nl3\nl4\nL5\nl6\nl7\nl8\nl9\n"
	writeTempFile(t, root, "whole.txt", before)

	result, err := NewEditExistingFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "whole.txt", "content": after}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.Content != "updated whole.txt" {
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
	got := editRegionsOf(t, result).Regions
	if !reflect.DeepEqual(got, want) {
		t.Errorf("regions = %+v, want %+v", got, want)
	}
	if regions := editRegions(before, after); !reflect.DeepEqual(got, regions.Regions) {
		t.Errorf("regions = %+v, want editRegions of the two contents %+v", got, regions.Regions)
	}
}

// The applier locates a hunk by its text, so a hunk whose old text repeats lands on the FIRST
// occurrence — not necessarily the one the model pictured. The regions report what actually
// landed, because they are cut from the file as read against the file as written and never from
// the patch's own account of itself.
func TestEditExistingFile_RegionsFollowWhereThePatchLanded(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	writeTempFile(t, root, "repeat.txt", "head\nsame\ntail\nsame\nend\n")

	patch := strings.Join([]string{
		"*** Begin Patch",
		"*** Update File: repeat.txt",
		"@@",
		"-same",
		"+SAME",
		"*** End Patch",
	}, "\n")

	result, err := NewEditExistingFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "repeat.txt", "content": patch}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}

	want := []domain.EditRegion{{
		BeforeStart: 1,
		AfterStart:  1,
		Leading:     []string{"head"},
		Removed:     []string{"same"},
		Inserted:    []string{"SAME"},
		Trailing:    []string{"tail", "same", "end"},
	}}
	if got := editRegionsOf(t, result).Regions; !reflect.DeepEqual(got, want) {
		t.Errorf("regions = %+v, want the change at the first occurrence %+v", got, want)
	}
}

// No summary rides along unless regions were actually cut. A refused patch wrote nothing, so
// there is nothing to paint; content equal to what the file already held wrote the same bytes
// back, and an empty EditRegions would claim the edit changed nothing rather than letting the
// card fall back to its argument-derived list.
func TestEditExistingFile_NoRegionsNoSummary(t *testing.T) {
	t.Parallel()

	const original = "l1\nl2\nl3\n"
	cases := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name:    "a hunk that matches nothing",
			content: "*** Begin Patch\n*** Update File: edit.txt\n@@\n-absent\n+x\n*** End Patch",
			wantErr: true,
		},
		{
			name:    "a patch carrying no hunk at all",
			content: "*** Begin Patch\n*** Update File: edit.txt\n*** End Patch",
			wantErr: true,
		},
		{
			name:    "content equal to what the file already held",
			content: original,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := tempRoot(t)
			writeTempFile(t, root, "edit.txt", original)

			result, err := NewEditExistingFile(root).Execute(context.Background(),
				callWith(t, "c1", map[string]any{"path": "edit.txt", "content": tc.content}))
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

// Both of edit_existing_file's write paths — the whole-content replacement and the patch apply —
// carry the syntax verdict on the bytes they left behind. The result stays a success either way:
// the edit landed, and the trailer is what the model reads to decide whether to fix it.
func TestEditExistingFile_Execute_AppendsTheSyntaxTrailer(t *testing.T) {
	t.Parallel()

	t.Run("whole content", func(t *testing.T) {
		t.Parallel()

		root := tempRoot(t)
		path := writeTempFile(t, root, "main.go", "package main\n\nfunc main() {}\n")
		const broken = "package main\n\nfunc main() {}\n}\n"

		result, err := NewEditExistingFile(root).Execute(context.Background(),
			callWith(t, "c1", map[string]any{"path": "main.go", "content": broken}))
		if err != nil {
			t.Fatalf("Execute returned a Go error: %v", err)
		}
		if result.IsError {
			t.Fatalf("result is an error: %q — a broken edit still lands", result.Content)
		}
		if _, ok := result.Summary.(domain.EditRegions); !ok {
			t.Errorf("Summary = %#v, want the Edit regions a successful edit always carries", result.Summary)
		}
		if !strings.HasPrefix(result.Content, "updated main.go\n") {
			t.Errorf("Content = %q, want the trailer behind the tool's own sentence", result.Content)
		}
		if !strings.Contains(result.Content, "syntax check: 1 problem") || !strings.Contains(result.Content, "line 4:") {
			t.Errorf("Content = %q, want the located syntax problem", result.Content)
		}
		if got := string(mustRead(t, path)); got != broken {
			t.Errorf("file = %q, want exactly the bytes the call asked for", got)
		}
	})

	t.Run("patch", func(t *testing.T) {
		t.Parallel()

		root := tempRoot(t)
		writeTempFile(t, root, "main.go", "package main\n\nfunc main() {\n\tprintln(1)\n}\n")

		patch := strings.Join([]string{
			"*** Begin Patch",
			"*** Update File: main.go",
			"@@",
			"-\tprintln(1)",
			"+\tprintln(1)",
			"+}",
			"*** End Patch",
		}, "\n")

		result, err := NewEditExistingFile(root).Execute(context.Background(),
			callWith(t, "c1", map[string]any{"path": "main.go", "content": patch}))
		if err != nil {
			t.Fatalf("Execute returned a Go error: %v", err)
		}
		if result.IsError {
			t.Fatalf("result is an error: %q — a broken patch apply still lands", result.Content)
		}
		if !strings.HasPrefix(result.Content, "applied patch to main.go (1 hunk)\n") {
			t.Errorf("Content = %q, want the trailer behind the tool's own sentence", result.Content)
		}
		if !strings.Contains(result.Content, "syntax check: 1 problem") {
			t.Errorf("Content = %q, want the located syntax problem", result.Content)
		}
	})
}

// A language the checker does not know reads exactly as it always did: no trailer, no extra line,
// nothing for a model to answer.
func TestEditExistingFile_Execute_NoTrailerForAnUnknownLanguage(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	writeTempFile(t, root, "notes.txt", "old")

	result, err := NewEditExistingFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "notes.txt", "content": "func main( {"}))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.Content != "updated notes.txt" {
		t.Errorf("Content = %q, want the bare sentence for a path with no known language", result.Content)
	}
}
