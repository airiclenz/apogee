package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// callWith builds a ToolCall whose Arguments is the JSON encoding of args.
func callWith(t *testing.T, id string, args map[string]any) domain.ToolCall {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return domain.ToolCall{ID: id, Arguments: raw}
}

// outsideMarker is the body of the file OUTSIDE the workspace: a result carrying it escaped.
const outsideMarker = "PRIVATE KEY"

// escapesUnderComponentSwap reads "ssh/id_rsa" through tool (constructed on root) n times
// while a concurrent goroutine flips the "ssh" component between a directory INSIDE the
// workspace and one outside it — the swap a write-capable confined subprocess can perform
// between a check and a use. It returns how many results carried the outside file's
// content; any non-zero count is a fence crossing. Both symlink targets are RELATIVE, so the
// count isolates the swap from the absolute-target narrowing the two
// RefusesAbsoluteInRootSymlink tests pin. The old resolveInRoot → os.Stat → os.ReadFile
// trio leaks here (its check-time verdict is stale by the time it reads); a read pinned to
// an os.Root cannot, so a zero count is deterministic rather than lucky.
func escapesUnderComponentSwap(t *testing.T, tool domain.Tool, root string, n int) int {
	t.Helper()

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "id_rsa"), []byte(outsideMarker), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	inside := filepath.Join(root, "inside")
	if err := os.Mkdir(inside, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inside, "id_rsa"), []byte("SAFE"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	relOutside, err := filepath.Rel(root, outside)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	link := filepath.Join(root, "ssh")
	if err := os.Symlink("inside", link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	stop, swapped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(swapped)
		targets := [2]string{"inside", relOutside}
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = os.Remove(link)
			_ = os.Symlink(targets[i%2], link)
		}
	}()
	defer func() {
		close(stop)
		<-swapped
	}()

	escapes := 0
	for i := 0; i < n; i++ {
		result, err := tool.Execute(context.Background(),
			callWith(t, "c1", map[string]any{"path": "ssh/id_rsa"}))
		if err != nil {
			t.Fatalf("Execute returned a Go error: %v", err)
		}
		if strings.Contains(result.Content, outsideMarker) {
			escapes++
		}
	}
	return escapes
}

func TestReadFile_Execute(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("line1\nline2\nline3"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tool := NewReadFile(root, nil)

	cases := []struct {
		name        string
		args        map[string]any
		wantErr     bool
		wantContain string
	}{
		{
			name:        "reads full file with a header",
			args:        map[string]any{"path": "hello.txt"},
			wantContain: "line1\nline2\nline3",
		},
		{
			name:        "header reports total line count",
			args:        map[string]any{"path": "hello.txt"},
			wantContain: "3 lines total",
		},
		{
			name:        "line range narrows output",
			args:        map[string]any{"path": "hello.txt", "start_line": 2, "end_line": 2},
			wantContain: "line2",
		},
		{
			name:        "missing file is a tool error",
			args:        map[string]any{"path": "absent.txt"},
			wantErr:     true,
			wantContain: "file not found",
		},
		{
			name:        "path escape is a tool error",
			args:        map[string]any{"path": "../escape.txt"},
			wantErr:     true,
			wantContain: "outside the workspace",
		},
		{
			name:        "missing path argument is a tool error",
			args:        map[string]any{},
			wantErr:     true,
			wantContain: "path is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := tool.Execute(context.Background(), callWith(t, "c1", tc.args))

			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if result.IsError != tc.wantErr {
				t.Fatalf("IsError = %v, want %v (content: %q)", result.IsError, tc.wantErr, result.Content)
			}
			if !strings.Contains(result.Content, tc.wantContain) {
				t.Errorf("content %q does not contain %q", result.Content, tc.wantContain)
			}
		})
	}
}

func TestReadFile_Execute_ReportsTheSpanItRendered(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("line1\nline2\nline3"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tool := NewReadFile(root, nil)

	cases := []struct {
		name        string
		args        map[string]any
		wantContent string
		wantSummary domain.ReadSpan
	}{
		{
			name:        "whole file",
			args:        map[string]any{"path": "hello.txt"},
			wantContent: "[File: hello.txt, 3 lines total, showing lines 1-3]\nline1\nline2\nline3",
			wantSummary: domain.ReadSpan{Start: 1, End: 3, Total: 3},
		},
		{
			name:        "narrowed range",
			args:        map[string]any{"path": "hello.txt", "start_line": 2, "end_line": 3},
			wantContent: "[File: hello.txt, 3 lines total, showing lines 2-3]\nline2\nline3",
			wantSummary: domain.ReadSpan{Start: 2, End: 3, Total: 3},
		},
		{
			name:        "max_lines truncation",
			args:        map[string]any{"path": "hello.txt", "max_lines": 2},
			wantContent: "[File: hello.txt, 3 lines total, showing lines 1-2]\nline1\nline2\n[...truncated at 2 lines]",
			wantSummary: domain.ReadSpan{Start: 1, End: 2, Total: 3},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := tool.Execute(context.Background(), callWith(t, "c1", tc.args))

			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if result.Content != tc.wantContent {
				t.Errorf("Content = %q, want %q", result.Content, tc.wantContent)
			}
			span, ok := result.Summary.(domain.ReadSpan)
			if !ok {
				t.Fatalf("Summary = %#v, want a domain.ReadSpan", result.Summary)
			}
			// ReadSpan carries a []int since it gained the locate fields, so it is
			// uncomparable — the whole-struct == this replaces no longer compiles.
			if !reflect.DeepEqual(span, tc.wantSummary) {
				t.Errorf("Summary = %+v, want %+v", span, tc.wantSummary)
			}
		})
	}
}

// TestReadFile_Execute_LocatesASubstring pins the locate parameter read_file absorbed from
// the retired open_file: one "Located …" line between the header and the content, worded
// exactly as open_file worded it, plus the same numbers on the summary. The scan covers the
// WHOLE file even when a range narrows what is returned, so a match the caller cannot see
// in the body is still reported — with its ABSOLUTE line number.
func TestReadFile_Execute_LocatesASubstring(t *testing.T) {
	t.Parallel()

	const body = "alpha\nneedle here\ngamma\ndelta\nneedle again"

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hay.txt"), []byte(body), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tool := NewReadFile(root, nil)

	cases := []struct {
		name        string
		args        map[string]any
		wantContent string
		wantSummary domain.ReadSpan
	}{
		{
			name:        "a hit lists every 1-based line",
			args:        map[string]any{"path": "hay.txt", "locate": "needle"},
			wantContent: "[File: hay.txt, 5 lines total, showing lines 1-5]\nLocated \"needle\" on lines: 2, 5\n" + body,
			wantSummary: domain.ReadSpan{Start: 1, End: 5, Total: 5, Locate: "needle", LocatedOn: []int{2, 5}},
		},
		{
			name:        "a miss says so rather than staying silent",
			args:        map[string]any{"path": "hay.txt", "locate": "absent"},
			wantContent: "[File: hay.txt, 5 lines total, showing lines 1-5]\nLocated \"absent\" on no lines\n" + body,
			// A set Locate with no LocatedOn is what distinguishes "asked, found
			// nothing" from "never asked" — the prose cannot.
			wantSummary: domain.ReadSpan{Start: 1, End: 5, Total: 5, Locate: "absent"},
		},
		{
			name:        "no locate leaves the output byte-identical",
			args:        map[string]any{"path": "hay.txt"},
			wantContent: "[File: hay.txt, 5 lines total, showing lines 1-5]\n" + body,
			wantSummary: domain.ReadSpan{Start: 1, End: 5, Total: 5},
		},
		{
			name:        "a match outside the returned range is still reported",
			args:        map[string]any{"path": "hay.txt", "start_line": 1, "end_line": 2, "locate": "needle again"},
			wantContent: "[File: hay.txt, 5 lines total, showing lines 1-2]\nLocated \"needle again\" on lines: 5\nalpha\nneedle here",
			wantSummary: domain.ReadSpan{Start: 1, End: 2, Total: 5, Locate: "needle again", LocatedOn: []int{5}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := tool.Execute(context.Background(), callWith(t, "c1", tc.args))

			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if result.Content != tc.wantContent {
				t.Errorf("Content = %q, want %q", result.Content, tc.wantContent)
			}
			span, ok := result.Summary.(domain.ReadSpan)
			if !ok {
				t.Fatalf("Summary = %#v, want a domain.ReadSpan", result.Summary)
			}
			if !reflect.DeepEqual(span, tc.wantSummary) {
				t.Errorf("Summary = %+v, want %+v", span, tc.wantSummary)
			}
		})
	}
}

func TestReadFile_Execute_ErrorCarriesNoSummary(t *testing.T) {
	t.Parallel()

	result, err := NewReadFile(t.TempDir(), nil).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "absent.txt"}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("IsError = false, want true (content: %q)", result.Content)
	}
	if result.Summary != nil {
		t.Errorf("Summary = %#v, want nil on a failed call", result.Summary)
	}
}

// TestReadFile_Execute_RefusesEscapingSymlink pins the STATIC half of the fence at the tool
// boundary: a workspace component that is a symlink pointing OUTSIDE the workspace is refused
// with the uniform ErrPathEscape message, and nothing from outside reaches the result.
//
// This is a boundary pin, not new behaviour: the former resolveInRoot → os.Stat →
// os.ReadFile trio refused this case too (resolveInRoot resolves symlinks and rejected the
// escape at check time), so the test passes against the pre-change code. It is kept because
// the check-time pre-pass is gone — the refusal must now come from the os.Root-pinned stat, and
// this pin fails if that stat is ever replaced by an unfenced one. What this change actually
// gained is pinned by TestReadFile_Execute_RefusesComponentSwappedMidRead (the racy half, which
// the old trio followed) and TestReadFile_Execute_RefusesAbsoluteInRootSymlink (the narrowing).
func TestReadFile_Execute_RefusesEscapingSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "id_rsa"), []byte(outsideMarker), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "ssh")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	result, err := NewReadFile(root, nil).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "ssh/id_rsa"}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("IsError = false, want true: the read followed a symlink out of the workspace (content: %q)", result.Content)
	}
	if !strings.Contains(result.Content, ErrPathEscape.Error()) {
		t.Errorf("content %q does not carry the ErrPathEscape message %q", result.Content, ErrPathEscape.Error())
	}
	if strings.Contains(result.Content, outsideMarker) {
		t.Errorf("content leaked the file outside the workspace: %q", result.Content)
	}
}

// TestReadFile_Execute_RefusesComponentSwappedMidRead is the behaviour read_file gained by
// reading through the fence rather than around it: a workspace component swapped to an
// outside-pointing symlink while the call is in flight no longer redirects the read. The old
// trio validated the path and then re-walked it, so a swap landing in that window was followed;
// the pinned root has no such window. This test fails against the pre-change code.
func TestReadFile_Execute_RefusesComponentSwappedMidRead(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	escapes := escapesUnderComponentSwap(t, NewReadFile(root, nil), root, 2000)

	if escapes != 0 {
		t.Errorf("%d of 2000 reads returned the file outside the workspace, want 0", escapes)
	}
}

// TestReadFile_Execute_RefusesAbsoluteInRootSymlink pins the one narrowing this change
// carries: an in-workspace symlink whose target is spelled as an ABSOLUTE path is refused
// even when that target is inside the workspace, because the pinned root resolves relative
// components only. Such a link read fine before; the fence is tighten-only, so the
// narrowing is kept and recorded in the CHANGELOG. Relative in-workspace symlinks still
// read (TestReadFile_Execute_ReadsRelativeInRootSymlink).
func TestReadFile_Execute_RefusesAbsoluteInRootSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "real.txt")
	if err := os.WriteFile(target, []byte("inside the workspace"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	result, err := NewReadFile(root, nil).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "link.txt"}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("IsError = false, want true: the absolute-target symlink narrowing is gone (content: %q)", result.Content)
	}
	if !strings.Contains(result.Content, ErrPathEscape.Error()) {
		t.Errorf("content %q does not carry the ErrPathEscape message %q", result.Content, ErrPathEscape.Error())
	}
}

// TestReadFile_Execute_ReadsRelativeInRootSymlink bounds that narrowing: a RELATIVE
// in-workspace symlink, as the named file and as a directory component, reads as before.
func TestReadFile_Execute_ReadsRelativeInRootSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "real.txt"), []byte("inside the workspace"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(filepath.Join("sub", "real.txt"), filepath.Join(root, "file_link.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := os.Symlink("sub", filepath.Join(root, "dir_link")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	for _, path := range []string{"file_link.txt", "dir_link/real.txt"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			result, err := NewReadFile(root, nil).Execute(context.Background(),
				callWith(t, "c1", map[string]any{"path": path}))

			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if result.IsError {
				t.Fatalf("IsError = true on an in-workspace relative symlink (content: %q)", result.Content)
			}
			if !strings.Contains(result.Content, "inside the workspace") {
				t.Errorf("content %q does not carry the linked file's body", result.Content)
			}
		})
	}
}

func TestReadFile_Execute_RejectsRangeOnLineTwo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("a\nb\nc\nd"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := NewReadFile(root, nil).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "f.txt", "start_line": 2, "end_line": 3}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if strings.Contains(result.Content, "\na\n") || strings.Contains(result.Content, "\nd") {
		t.Errorf("range leaked lines outside 2-3: %q", result.Content)
	}
	if !strings.Contains(result.Content, "b\nc") {
		t.Errorf("range did not include lines 2-3: %q", result.Content)
	}
}

// TestReadFile_Execute_ReadsUnderAnExtraReadRoot pins the mount half of the read-only roots
// seam: an ABSOLUTE path under a configured extra root reads, a workspace-relative path is
// untouched by the mount, and a path under no root at all is still refused with the one
// uniform escape message — the extra roots widen what can be read, never what a refusal says.
func TestReadFile_Execute_ReadsUnderAnExtraReadRoot(t *testing.T) {
	t.Parallel()

	root, extra, outside := t.TempDir(), t.TempDir(), t.TempDir()
	writeFixtureFile(t, filepath.Join(root, "in-workspace.txt"), "workspace bytes")
	writeFixtureFile(t, filepath.Join(extra, "skill", "SKILL.md"), "skill bytes")
	writeFixtureFile(t, filepath.Join(outside, "id_rsa"), outsideMarker)

	tool := NewReadFile(root, func() []string { return []string{extra} })

	cases := []struct {
		name    string
		path    string
		want    string // substring the result content must carry
		wantErr bool
	}{
		{"absolute under the extra root", filepath.Join(extra, "skill", "SKILL.md"), "skill bytes", false},
		{"workspace relative unchanged", "in-workspace.txt", "workspace bytes", false},
		{"under no root", filepath.Join(outside, "id_rsa"), ErrPathEscape.Error(), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := tool.Execute(context.Background(), callWith(t, "c1", map[string]any{"path": tc.path}))

			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if result.IsError != tc.wantErr {
				t.Fatalf("IsError = %v, want %v (content: %q)", result.IsError, tc.wantErr, result.Content)
			}
			if !strings.Contains(result.Content, tc.want) {
				t.Errorf("content %q does not contain %q", result.Content, tc.want)
			}
			if tc.wantErr && strings.Contains(result.Content, outsideMarker) {
				t.Errorf("content leaked the file under no root: %q", result.Content)
			}
		})
	}
}

// TestReadFile_Execute_ExtraRootIsReadableNotWritable pins the read-ONLY half of the seam,
// where it is easiest to get wrong: the very directory read_file now reads from is refused by
// the write tools, which never receive the extra-roots func and stay workspace-fenced through
// the workspaceScopedWriter discipline (ADR 0012 D1). Mounting a directory for reading must
// not make one byte of it writable.
func TestReadFile_Execute_ExtraRootIsReadableNotWritable(t *testing.T) {
	t.Parallel()

	root, extra := t.TempDir(), t.TempDir()
	target := filepath.Join(extra, "SKILL.md")
	writeFixtureFile(t, target, "skill bytes")

	read, err := NewReadFile(root, func() []string { return []string{extra} }).Execute(
		context.Background(), callWith(t, "c1", map[string]any{"path": target}))
	if err != nil {
		t.Fatalf("read of the extra root returned a Go error: %v", err)
	}
	if read.IsError {
		t.Fatalf("read of the extra root failed: %q", read.Content)
	}

	writers := map[string]domain.Tool{
		"write_file":         NewWriteFile(root),
		"edit_existing_file": NewEditExistingFile(root),
	}
	for name, tool := range writers {
		t.Run(name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(),
				callWith(t, "c2", map[string]any{"path": target, "content": "overwritten"}))

			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if !result.IsError {
				t.Fatalf("%s wrote into the read-only root (content: %q)", name, result.Content)
			}
			if !strings.Contains(result.Content, ErrPathEscape.Error()) {
				t.Errorf("content %q does not carry the ErrPathEscape message %q", result.Content, ErrPathEscape.Error())
			}
		})
	}

	data, err := os.ReadFile(target)
	if err != nil || string(data) != "skill bytes" {
		t.Errorf("file under the read-only root = %q (err %v), want it untouched", data, err)
	}
}

func TestReadFile_Execute_HonoursCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewReadFile(t.TempDir(), nil).Execute(ctx, callWith(t, "c1", map[string]any{"path": "x"}))

	if err == nil {
		t.Fatalf("Execute on a cancelled ctx returned nil error, want ctx error")
	}
}
