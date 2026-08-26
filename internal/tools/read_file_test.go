package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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

	outside := tempRoot(t)
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

	root := tempRoot(t)
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

	root := tempRoot(t)
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

	root := tempRoot(t)
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

	result, err := NewReadFile(tempRoot(t), nil).Execute(context.Background(),
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

	root := tempRoot(t)
	outside := tempRoot(t)
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

	root := tempRoot(t)

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

	root := tempRoot(t)
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

	root := tempRoot(t)
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

// TestReadFile_Execute_DisclosesTheResolvedPath is the read side of the writers' disclosure: a
// read FOLLOWS an in-root symlink rather than replacing it, so without a note the header quotes
// the argument's innocuous name while the body carries another file's bytes. The result must end
// with the same ` → resolves to <path>` tail the write tools append — for a symlinked leaf and
// for a symlinked directory component alike — while an ordinary read keeps the bare rendering: a
// note that fired on every read would disclose nothing.
func TestReadFile_Execute_DisclosesTheResolvedPath(t *testing.T) {
	t.Parallel()

	// The root is resolved up front so the negative case below is a fact about the ARGUMENT,
	// not about a box whose temp dir is itself reached through a symlink (macOS /var).
	root := tempRoot(t)
	config := symlinkedReadFixture(t, root, "docs", "notes.md")
	writeFixtureFile(t, filepath.Join(root, "real", "data.txt"), "linked bytes")
	if err := os.Symlink("real", filepath.Join(root, "dir_link")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	writeFixtureFile(t, filepath.Join(root, "plain.txt"), "plain bytes")

	tool := NewReadFile(root, nil)

	cases := []struct {
		name     string
		path     string
		wantReal string
		wantBody string
	}{
		{"symlinked file", "docs/notes.md", config, gitConfigFixture},
		{"symlinked directory component", "dir_link/data.txt", filepath.Join(root, "real", "data.txt"), "linked bytes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := runFileOp(t, tool, map[string]any{"path": tc.path})

			if result.IsError {
				t.Fatalf("IsError = true on an in-workspace relative symlink (content: %q)", result.Content)
			}
			if !strings.Contains(result.Content, tc.wantBody) {
				t.Errorf("content %q does not carry the linked file's body", result.Content)
			}
			if want := " → resolves to " + realPath(t, tc.wantReal); !strings.HasSuffix(result.Content, want) {
				t.Errorf("content %q does not end with the disclosure %q", result.Content, want)
			}
		})
	}

	ordinary := runFileOp(t, tool, map[string]any{"path": "plain.txt"})
	if ordinary.IsError {
		t.Fatalf("unexpected tool error: %q", ordinary.Content)
	}
	if strings.Contains(ordinary.Content, "resolves to") {
		t.Errorf("content %q carries a note for a path that resolves to itself", ordinary.Content)
	}
}

// TestReadFile_Execute_DisclosesTheResolvedPathUnderAnExtraReadRoot is the standing
// skills-access guard on this disclosure: an absolute path under a configured read-only root
// still READS — the note is text appended to a success, never a refusal — and it resolves
// against the root that actually served the read (readScope.readRoot), not the workspace.
func TestReadFile_Execute_DisclosesTheResolvedPathUnderAnExtraReadRoot(t *testing.T) {
	t.Parallel()

	root, extra := tempRoot(t), tempRoot(t)
	target := filepath.Join(extra, "skill", "SKILL.md")
	writeFixtureFile(t, target, "skill bytes")
	if err := os.Symlink(filepath.Join("skill", "SKILL.md"), filepath.Join(extra, "linked.md")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	tool := NewReadFile(root, func() []string { return []string{extra} })

	linked := runFileOp(t, tool, map[string]any{"path": filepath.Join(extra, "linked.md")})
	if linked.IsError {
		t.Fatalf("read under the extra root was refused: %q", linked.Content)
	}
	if !strings.Contains(linked.Content, "skill bytes") {
		t.Errorf("content %q does not carry the linked file's body", linked.Content)
	}
	if want := " → resolves to " + realPath(t, target); !strings.HasSuffix(linked.Content, want) {
		t.Errorf("content %q does not end with the disclosure %q", linked.Content, want)
	}

	direct := runFileOp(t, tool, map[string]any{"path": target})
	if direct.IsError {
		t.Fatalf("read under the extra root was refused: %q", direct.Content)
	}
	if strings.Contains(direct.Content, "resolves to") {
		t.Errorf("content %q carries a note for a path that resolves to itself", direct.Content)
	}
}

func TestReadFile_Execute_RejectsRangeOnLineTwo(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
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

	root, extra, outside := tempRoot(t), tempRoot(t), tempRoot(t)
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

	root, extra := tempRoot(t), tempRoot(t)
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

	_, err := NewReadFile(tempRoot(t), nil).Execute(ctx, callWith(t, "c1", map[string]any{"path": "x"}))

	if err == nil {
		t.Fatalf("Execute on a cancelled ctx returned nil error, want ctx error")
	}
}

// readPDFFixture returns the bytes of a committed PDF under the doctext package's testdata. The
// fixtures are hand-built minimal documents rather than generated at test time, so what the
// parser is fed here is exactly the bytes a reviewer can read in the repository — and they are
// the SAME bytes the extractor's own suite runs on, which is why this reaches across the package
// boundary for them instead of keeping a second copy that could drift.
func readPDFFixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "doctext", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// pdfWorkspace plants the committed PDF fixtures inside a fresh workspace root and returns it.
// The suite below reads them through the tool's real path — off disk, sniffed, extracted — rather
// than calling the extractor directly, which is internal/doctext's own suite's job.
func pdfWorkspace(t *testing.T, names ...string) string {
	t.Helper()

	root := tempRoot(t)
	for _, name := range names {
		writeFixtureFile(t, filepath.Join(root, name), string(readPDFFixture(t, name)))
	}
	return root
}

// TestReadFile_Execute_ReadsAPDFAsExtractedText is the whole point of PDF support: the model is
// handed the document's words, behind a "[Page N]" marker, under a header that says which format
// and how many pages it is reading — and every line-addressed parameter it already knows keeps
// working, because the extracted text flows through the unchanged rendering pipeline. The
// fixture's text is "Hello Apogee" on the third line of the extraction ("[Page 1]", a blank line,
// then the page's own text).
func TestReadFile_Execute_ReadsAPDFAsExtractedText(t *testing.T) {
	t.Parallel()

	const header = "[File: minimal.pdf (PDF, 1 page; extracted text, read-only), 3 lines total, showing lines"

	tool := NewReadFile(pdfWorkspace(t, "minimal.pdf"), nil)

	cases := []struct {
		name        string
		args        map[string]any
		wantContent string
		wantSummary domain.ReadSpan
	}{
		{
			name:        "whole document behind a page marker",
			args:        map[string]any{"path": "minimal.pdf"},
			wantContent: header + " 1-3]\n[Page 1]\n\nHello Apogee",
			wantSummary: domain.ReadSpan{Start: 1, End: 3, Total: 3},
		},
		{
			name:        "start_line addresses the extracted text",
			args:        map[string]any{"path": "minimal.pdf", "start_line": 3},
			wantContent: header + " 3-3]\nHello Apogee",
			wantSummary: domain.ReadSpan{Start: 3, End: 3, Total: 3},
		},
		{
			name:        "locate finds a word inside the document",
			args:        map[string]any{"path": "minimal.pdf", "locate": "Apogee"},
			wantContent: header + " 1-3]\nLocated \"Apogee\" on lines: 3\n[Page 1]\n\nHello Apogee",
			wantSummary: domain.ReadSpan{Start: 1, End: 3, Total: 3, Locate: "Apogee", LocatedOn: []int{3}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := tool.Execute(context.Background(), callWith(t, "c1", tc.args))

			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if result.IsError {
				t.Fatalf("IsError = true on a readable PDF (content: %q)", result.Content)
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

// TestReadFile_Execute_RefusesAPDFWithNoText pins the scanned-document case at the tool boundary:
// an IsError result carrying the extractor's own sentence, and not one byte of the PDF falling
// back into the transcript.
func TestReadFile_Execute_RefusesAPDFWithNoText(t *testing.T) {
	t.Parallel()

	result, err := NewReadFile(pdfWorkspace(t, "notext.pdf"), nil).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "notext.pdf"}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("IsError = false on a PDF with no extractable text (content: %q)", result.Content)
	}
	// The sentence itself belongs to internal/doctext, whose own suite pins it word for word and
	// keeps the constant unexported. What this asserts is the boundary's half of the contract:
	// the tool hands the extractor's sentence on whole instead of wording a failure of its own.
	if !strings.Contains(result.Content, "no extractable text") ||
		!strings.Contains(result.Content, "ask the user for a text version") {
		t.Errorf("Content = %q, want the extractor's no-extractable-text sentence", result.Content)
	}
	if result.Summary != nil {
		t.Errorf("Summary = %#v, want nil on a failed call", result.Summary)
	}
}

// TestReadFile_Execute_BoundsAPhantomPageCount pins the extractor's bounds AT THE TOOL boundary.
// A 240-byte document declaring ten million pages behind an empty page tree cost 51 s and ~142
// GiB of churn before the walk was bounded (audit 2026-08-25, C-07); it now answers promptly with
// the same sentence any document that yields no text gets, because that is what it is.
func TestReadFile_Execute_BoundsAPhantomPageCount(t *testing.T) {
	t.Parallel()

	start := time.Now()
	result, err := NewReadFile(pdfWorkspace(t, "phantompages.pdf"), nil).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "phantompages.pdf"}))
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("IsError = false on a document with no extractable text (content: %q)", result.Content)
	}
	if !strings.Contains(result.Content, "no extractable text") {
		t.Errorf("Content = %q, want the extractor's no-extractable-text sentence", result.Content)
	}
	// Generous on purpose — the assertion is that the read is bounded at all, not how fast this
	// machine is. An unbounded walk of ten million declared pages does not finish in minutes.
	if elapsed > 5*time.Second {
		t.Errorf("reading a 240-byte document took %s; the walk sized itself off its /Count", elapsed)
	}
}

// TestReadFile_Execute_JudgesAPDFByContentNotName bounds the detection: the extension decides
// nothing. A text file someone called notes.pdf reads as the text it is — header unannotated, no
// page marker, no extraction failure — which is also what keeps a mislabelled file readable at all.
func TestReadFile_Execute_JudgesAPDFByContentNotName(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	writeFixtureFile(t, filepath.Join(root, "notes.pdf"), "plain words\nsecond line")

	result, err := NewReadFile(root, nil).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "notes.pdf"}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("IsError = true on a text file named .pdf (content: %q)", result.Content)
	}
	want := "[File: notes.pdf, 2 lines total, showing lines 1-2]\nplain words\nsecond line"
	if result.Content != want {
		t.Errorf("Content = %q, want %q", result.Content, want)
	}
}

// TestPDFDisplayPath pins what the read_file header says about an extracted document: the path
// the argument named, then doctext's annotation in parentheses — the format, the page count, and
// the read-only hint that tells the model these lines are a rendering rather than the file. The
// grammar of the count itself belongs to doctext.PDFAnnotation and is pinned there; what this
// asserts is that the tool's header carries it verbatim and punctuates it the tool's way.
func TestPDFDisplayPath(t *testing.T) {
	t.Parallel()

	const path = "x.pdf"

	cases := []struct {
		name  string
		pages int
		want  string
	}{
		{name: "zero pages reads as plural", pages: 0, want: "x.pdf (PDF, 0 pages; extracted text, read-only)"},
		{name: "one page reads as singular", pages: 1, want: "x.pdf (PDF, 1 page; extracted text, read-only)"},
		{name: "two pages reads as plural", pages: 2, want: "x.pdf (PDF, 2 pages; extracted text, read-only)"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := pdfDisplayPath(path, tc.pages)

			if got != tc.want {
				t.Errorf("pdfDisplayPath(%q, %d) = %q, want %q", path, tc.pages, got, tc.want)
			}
		})
	}
}
