package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// The approved escape, per verb (ADR 0049).
//
// The Gate has always promised that "Allow" on an out-of-workspace write RUNS it. These tests are
// that promise stated once per verb, because the ADR's whole point is uniformity: a family where
// the approval executes for write_file but errors for find_replace would be a gate that is honest
// four times out of seven. Each case is driven three ways from ONE description — with the permit
// dispatch would stamp (it lands), with a permit for a different path (it refuses), and with no
// permit at all (today's fence, byte-for-byte) — so a verb cannot pass the happy path while
// quietly losing the floor.

// escapePermit returns the context dispatch stamps for an approved out-of-workspace target: the
// permit names the RESOLVED path, which is both what the approval pane disclosed and what Execute
// re-resolves the argument against.
func escapePermit(target string) context.Context {
	return domain.WithWriteEscapePermit(context.Background(),
		domain.WriteEscapePermit{Real: security.EvalRealPath(target)})
}

// runWrite executes one write-family call under ctx and fails on a Go error, which these tools
// reserve for cancellation — every expected refusal is an IsError result the model reads.
func runWrite(t *testing.T, ctx context.Context, tool domain.Tool, args map[string]any) domain.ToolResult {
	t.Helper()

	result, err := tool.Execute(ctx, callWith(t, "c1", args))
	if err != nil {
		t.Fatalf("%s returned a Go error: %v", tool.Name(), err)
	}
	return result
}

// escapeCase describes one verb's approved out-of-workspace call: the fixtures it needs, the tool
// that performs it, the arguments naming the outside target, and what the filesystem must look
// like once it has landed.
type escapeCase struct {
	name   string
	setup  func(t *testing.T, root, target string)
	tool   func(root string) domain.Tool
	args   func(target string) map[string]any
	landed func(t *testing.T, root, target string)
}

// escapeCases is the WS-write family, one entry per verb the ADR names. The target always lies in
// a directory the workspace does not contain, which is what makes every one of these calls a
// gated escape rather than an ordinary write.
func escapeCases() []escapeCase {
	return []escapeCase{
		{
			name:  "write_file",
			setup: func(t *testing.T, root, target string) {},
			tool:  func(root string) domain.Tool { return NewWriteFile(root) },
			args: func(target string) map[string]any {
				return map[string]any{"path": target, "content": "escaped\n"}
			},
			landed: func(t *testing.T, root, target string) { mustContain(t, target, "escaped\n") },
		},
		{
			name: "edit_existing_file full replacement",
			setup: func(t *testing.T, root, target string) {
				writeFixture(t, target, "old\n", 0o644)
			},
			tool: func(root string) domain.Tool { return NewEditExistingFile(root) },
			args: func(target string) map[string]any {
				return map[string]any{"path": target, "content": "new\n"}
			},
			landed: func(t *testing.T, root, target string) { mustContain(t, target, "new\n") },
		},
		{
			name: "edit_existing_file patch",
			setup: func(t *testing.T, root, target string) {
				writeFixture(t, target, "alpha\nbeta\n", 0o644)
			},
			tool: func(root string) domain.Tool { return NewEditExistingFile(root) },
			args: func(target string) map[string]any {
				return map[string]any{
					"path":    target,
					"content": "*** Begin Patch\n@@\n-beta\n+gamma\n*** End Patch\n",
				}
			},
			landed: func(t *testing.T, root, target string) { mustContain(t, target, "alpha\ngamma\n") },
		},
		{
			name: "single_find_and_replace",
			setup: func(t *testing.T, root, target string) {
				writeFixture(t, target, "hello world\n", 0o644)
			},
			tool: func(root string) domain.Tool { return NewSingleFindReplace(root) },
			args: func(target string) map[string]any {
				return map[string]any{"path": target, "oldText": "world", "newText": "there"}
			},
			landed: func(t *testing.T, root, target string) { mustContain(t, target, "hello there\n") },
		},
		{
			name: "multi_find_and_replace",
			setup: func(t *testing.T, root, target string) {
				writeFixture(t, target, "a b\n", 0o644)
			},
			tool: func(root string) domain.Tool { return NewMultiFindReplace(root) },
			args: func(target string) map[string]any {
				return map[string]any{"path": target, "replacements": []map[string]any{
					{"oldText": "a", "newText": "x"},
					{"oldText": "b", "newText": "y"},
				}}
			},
			landed: func(t *testing.T, root, target string) { mustContain(t, target, "x y\n") },
		},
		{
			name: "copy_file destination",
			setup: func(t *testing.T, root, target string) {
				writeFixture(t, filepath.Join(root, "src.txt"), "copied\n", 0o644)
			},
			tool: func(root string) domain.Tool { return NewCopyFile(root, nil) },
			args: func(target string) map[string]any {
				return map[string]any{"source": "src.txt", "destination": target}
			},
			landed: func(t *testing.T, root, target string) {
				mustContain(t, target, "copied\n")
				mustContain(t, filepath.Join(root, "src.txt"), "copied\n") // a copy leaves the source
			},
		},
		{
			name: "move_file destination",
			setup: func(t *testing.T, root, target string) {
				writeFixture(t, filepath.Join(root, "mv.txt"), "moved\n", 0o644)
			},
			tool: func(root string) domain.Tool { return NewMoveFile(root) },
			args: func(target string) map[string]any {
				return map[string]any{"source": "mv.txt", "destination": target}
			},
			landed: func(t *testing.T, root, target string) {
				mustContain(t, target, "moved\n")
				mustBeAbsent(t, filepath.Join(root, "mv.txt")) // a move takes the source with it
			},
		},
		{
			name: "delete_file",
			setup: func(t *testing.T, root, target string) {
				writeFixture(t, target, "doomed\n", 0o644)
			},
			tool: func(root string) domain.Tool { return NewDeleteFile(root) },
			args: func(target string) map[string]any {
				return map[string]any{"path": target}
			},
			landed: func(t *testing.T, root, target string) { mustBeAbsent(t, target) },
		},
	}
}

// TestApprovedEscapeLandsOnTheDisclosedTarget is the positive control for the whole family: the
// permit dispatch stamps for an approved gate makes every verb's call land on exactly the path the
// approval disclosed. Before ADR 0049 each of these returned an error AFTER the human said yes.
func TestApprovedEscapeLandsOnTheDisclosedTarget(t *testing.T) {
	t.Parallel()

	for _, tc := range escapeCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root, target := escapeFixtures(t, tc)

			result := runWrite(t, escapePermit(target), tc.tool(root), tc.args(target))
			if result.IsError {
				t.Fatalf("approved escape was refused: %q", result.Content)
			}
			tc.landed(t, root, target)
		})
	}
}

// TestApprovedEscapeThroughAWorkspaceLinkLandsOnTheDisclosedTarget drives the same family through
// the route that used to defeat the approval: an argument spelled INSIDE the workspace that reaches
// the outside target through a symlink. Dispatch resolves it to classify the call, so the pane
// disclosed — and the permit names — the outside path; every verb must act on that path, through
// the permitted root rather than through the link, which survives the call untouched. delete_file is
// the case worth reading twice: it removes the resolved target the operator agreed to destroy and
// leaves the workspace link dangling, rather than quietly unlinking the link and sparing the file.
func TestApprovedEscapeThroughAWorkspaceLinkLandsOnTheDisclosedTarget(t *testing.T) {
	t.Parallel()

	for _, tc := range escapeCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root, target := escapeFixtures(t, tc)
			named, link := linkedPath(t, root, target)

			result := runWrite(t, escapePermit(target), tc.tool(root), tc.args(named))
			if result.IsError {
				t.Fatalf("approved escape through a workspace link was refused: %q", result.Content)
			}
			tc.landed(t, root, target)
			mustBeALink(t, link)
		})
	}
}

// TestEscapeRefusedWhenThePermitNamesAnotherPath pins the bound the permit buys: it authorises ONE
// resolved path, so a call naming a different out-of-workspace target is refused even though a
// permit rides on the context. This is the property that keeps an approval from becoming a
// session-wide licence to write outside the workspace.
func TestEscapeRefusedWhenThePermitNamesAnotherPath(t *testing.T) {
	t.Parallel()

	for _, tc := range escapeCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root, target := escapeFixtures(t, tc)
			decoy := filepath.Join(filepath.Dir(target), "decoy.txt")

			result := runWrite(t, escapePermit(decoy), tc.tool(root), tc.args(target))
			mustRefuseEscape(t, result)
			mustBeAbsent(t, decoy)
		})
	}
}

// TestUnpermittedEscapeStillRefused is the never-worse floor stated per verb: with no permit on the
// context — every call in an ordinary session — an out-of-workspace target is refused exactly as it
// was before ADR 0049, and nothing outside the workspace is touched.
func TestUnpermittedEscapeStillRefused(t *testing.T) {
	t.Parallel()

	for _, tc := range escapeCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root, target := escapeFixtures(t, tc)
			before := snapshot(t, target)

			result := runWrite(t, context.Background(), tc.tool(root), tc.args(target))
			mustRefuseEscape(t, result)
			if after := snapshot(t, target); after != before {
				t.Fatalf("unpermitted call changed the outside target: %q → %q", before, after)
			}
		})
	}
}

// TestEscapeMismatchNamesTheApprovedTarget pins that a mismatched escape is refused BY THE PERMIT
// rather than incidentally by the fence — the permit really is threaded through to the security
// core, and the refusal says which path was expected. An operator who approved a write and got an
// error needs to tell the two truths apart: the argument no longer means what the pane showed, or
// the target itself has changed under them.
func TestEscapeMismatchNamesTheApprovedTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	approved := filepath.Join(outside, "approved.txt")
	other := filepath.Join(outside, "other.txt")

	result := runWrite(t, escapePermit(approved), NewWriteFile(root), map[string]any{
		"path": other, "content": "escaped\n",
	})
	mustRefuseEscape(t, result)
	if !strings.Contains(result.Content, "not the approved target") {
		t.Fatalf("the refusal does not name the approved target: %q", result.Content)
	}
	mustBeAbsent(t, other)
	mustBeAbsent(t, approved)
}

// TestApprovedEscapeCreatesMissingParents covers the pre-flight stat's own edge. An approved
// destination may name directories that do not exist yet, and the stat behind the file-operation
// tools' friendly refusals has to report ordinary absence there — a fence refusal would kill the
// approved copy before the primitive that creates those parents ever ran.
func TestApprovedEscapeCreatesMissingParents(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "nested", "deeper", "landed.txt")
	writeFixture(t, filepath.Join(root, "src.txt"), "copied\n", 0o644)

	result := runWrite(t, escapePermit(target), NewCopyFile(root, nil), map[string]any{
		"source": "src.txt", "destination": target,
	})
	if result.IsError {
		t.Fatalf("approved copy to a missing parent was refused: %q", result.Content)
	}
	mustContain(t, target, "copied\n")
}

// TestPermitLeavesInWorkspaceWritesAlone pins the other half of the floor: a permit is additive.
// A call whose target is inside the workspace behaves identically whether or not one rides along,
// and it certainly does not land on the permitted path instead.
func TestPermitLeavesInWorkspaceWritesAlone(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "approved.txt")

	result := runWrite(t, escapePermit(outside), NewWriteFile(root), map[string]any{
		"path": "notes.md", "content": "inside\n",
	})
	if result.IsError {
		t.Fatalf("in-workspace write was refused: %q", result.Content)
	}
	mustContain(t, filepath.Join(root, "notes.md"), "inside\n")
	mustBeAbsent(t, outside)
}

// TestMoveSourceStaysInWorkspaceUnderAPermit is ADR 0049's one exception, stated as a test. The
// Gate classifies and discloses a move's DESTINATION; its source is never shown to the operator, so
// no approval can authorise taking bytes out of the workspace. The permit therefore reaches the
// destination alone, and an outside source is refused with the permit in hand.
func TestMoveSourceStaysInWorkspaceUnderAPermit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	source := filepath.Join(outside, "secret.txt")
	destination := filepath.Join(outside, "landed.txt")
	writeFixture(t, source, "private\n", 0o644)

	result := runWrite(t, escapePermit(destination), NewMoveFile(root), map[string]any{
		"source": source, "destination": destination,
	})
	mustRefuseEscape(t, result)
	mustContain(t, source, "private\n") // the source is not moved, nor removed
	mustBeAbsent(t, destination)
}

// TestPermitWidensNoRead is the write-side-only rule at the tool surface: a permit authorises the
// write the operator read, never a wider view of the host, so read_file refuses the permitted path
// exactly as it refuses any other path outside the workspace.
func TestPermitWidensNoRead(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "approved.txt")
	writeFixture(t, target, "outside bytes\n", 0o644)

	result := runWrite(t, escapePermit(target), NewReadFile(root, nil), map[string]any{"path": target})
	if !result.IsError {
		t.Fatalf("a permit widened a read: %q", result.Content)
	}
	if strings.Contains(result.Content, "outside bytes") {
		t.Fatalf("the read returned the outside file's bytes: %q", result.Content)
	}
}

// escapeFixtures builds one case's workspace and its out-of-workspace target. The two roots are
// separate temp directories, so the target is outside the workspace by construction rather than by
// a "../" the fence would reject for its own reasons.
func escapeFixtures(t *testing.T, tc escapeCase) (root, target string) {
	t.Helper()

	root = t.TempDir()
	target = filepath.Join(t.TempDir(), "landed.txt")
	tc.setup(t, root, target)
	return root, target
}

// linkedPath renders target as a path INSIDE root that reaches it through a symlink, returning that
// path beside the link itself. The link is a DIRECTORY link on purpose: it resolves whether or not
// the target file exists yet, so one helper serves both the verbs that create their target and the
// verbs that rewrite one — which is exactly the range the permit has to cover.
func linkedPath(t *testing.T, root, target string) (named, link string) {
	t.Helper()

	link = filepath.Join(root, "outside")
	if err := os.Symlink(filepath.Dir(target), link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	return filepath.Join(link, filepath.Base(target)), link
}

// mustBeALink asserts path is still a symlink — the route a call travelled must never become the
// thing it acted on.
func mustBeALink(t *testing.T, path string) {
	t.Helper()

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat %s: %v", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is no longer a symlink (mode %v)", path, info.Mode())
	}
}

// mustRefuseEscape asserts a refusal the model can read, carrying the uniform "outside the
// workspace" sentinel every fence refusal in this family shares — whether it came from the
// pre-flight stat or from the permitted-target mismatch.
func mustRefuseEscape(t *testing.T, result domain.ToolResult) {
	t.Helper()

	if !result.IsError {
		t.Fatalf("expected a refusal, got: %q", result.Content)
	}
	if !strings.Contains(result.Content, "outside the workspace root") {
		t.Fatalf("refusal does not name the fence: %q", result.Content)
	}
}

// mustContain asserts the file at path holds exactly want.
func mustContain(t *testing.T, path, want string) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s holds %q, want %q", path, got, want)
	}
}

// mustBeAbsent asserts nothing exists at path.
func mustBeAbsent(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Lstat(path); err == nil {
		t.Fatalf("%s exists but should not", path)
	}
}

// snapshot renders the state of path as one comparable string — its bytes, or the fact that it is
// absent — so a test can assert a refused call changed nothing without caring which of the two the
// case started from.
func snapshot(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		return "<absent>"
	}
	return string(data)
}
