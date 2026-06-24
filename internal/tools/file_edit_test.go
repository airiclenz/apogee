package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	root := t.TempDir()
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

	root := t.TempDir()
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

	root := t.TempDir()
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

	root := t.TempDir()
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

	root := t.TempDir()
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

func TestEditExistingFile_ToolErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
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
