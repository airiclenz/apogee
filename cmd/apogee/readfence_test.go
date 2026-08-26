package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
)

// The read fence, driven end to end through the composition root (audit 2026-08-25 F-13).
//
// The exploit these tests replicate needs nothing but a `git clone`: a repo that ships
// `.apogee/skills` as a symlink to /home, /etc or any other tree. Skill DISCOVERY has always
// refused to walk such an anchor, but the MOUNT the same sources fed the model's read tools was
// the unresolved path — so grep, read_file, list_dir and find_files read the tree discovery
// would not. Both halves of the fix are wired here and nowhere else (the provider vouches for
// the roots, the root mounts what it vouched for), so the proof belongs at this seam rather
// than at either half's own package.

// readFenceRealDir returns a fresh temp directory by its own real path. Both the workspace root
// and the tree behind the symlink have to be real paths for this to test what it claims: the
// mount side skips any root that is not its own real path, so a workspace living under a
// symlinked TMPDIR would make even the honest case go unmounted for the wrong reason.
func readFenceRealDir(t *testing.T) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	return resolved
}

// readFenceWiring assembles a live session over workspace exactly as runRoot does — the boot
// phase and the live phase, short of the launch — and hands back the wiring so the TOOLS the
// model would be offered can be driven directly. It is rosterSwitchWiring's shape with the
// workspace as the variable, because what a repo-shipped symlink does to the fence is only
// observable on the registry the composition root actually built.
func readFenceWiring(t *testing.T, workspace, configDir string) *rootWiring {
	t.Helper()
	opts := config.Options{
		Endpoint:    "http://127.0.0.1:1111",
		Model:       "fake",
		Mode:        "ask-before",
		Workspace:   workspace,
		ConfigDir:   configDir,
		AutoCompact: true,
	}
	roots, err := resolveRoots(opts.ConfigDir, opts.Workspace)
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	w := newRootWiring(opts, apogee.ModeAskBefore, roots)
	t.Cleanup(w.close)
	if err := w.resolveConfig(); err != nil {
		t.Fatalf("resolveConfig: %v", err)
	}
	if err := w.wireSession(context.Background()); err != nil {
		t.Fatalf("wireSession: %v", err)
	}
	return w
}

// readFenceExecute looks the named tool up on the registry the composition root built and runs
// one call against it, failing the test when the tool is missing or answers with a Go error (a
// refusal is a RESULT, never an error — only caller cancellation propagates).
func readFenceExecute(t *testing.T, w *rootWiring, tool string, arguments string) apogee.ToolResult {
	t.Helper()
	found, ok := w.cfg.Tools.Lookup(tool)
	if !ok {
		t.Fatalf("%s is missing from the session's tool registry", tool)
	}
	result, err := found.Execute(context.Background(), apogee.ToolCall{
		ID:        "c1",
		Tool:      tool,
		Arguments: []byte(arguments),
	})
	if err != nil {
		t.Fatalf("%s returned a Go error: %v", tool, err)
	}
	return result
}

// A repo-shipped symlink at `.apogee/skills` must not relocate the read fence: the provider
// drops an anchor that resolves outside the workspace, so the composition root never mounts it
// and all four read tools answer the ordinary "outside the workspace" refusal for a file that
// used to be readable through it. The secret's own bytes are asserted absent from every result
// as well, because a refusal that quoted the file would leak it just the same.
func TestRepoSymlinkedSkillsDirCannotRelocateTheReadFence(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the exploit is a POSIX symlink; the mount rule is asserted on the packages' own tests there")
	}

	workspace, outside := readFenceRealDir(t), readFenceRealDir(t)
	const secretBytes = "sk-live-not-yours"
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte(secretBytes), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, ".apogee"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, ".apogee", "skills")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	w := readFenceWiring(t, workspace, readFenceRealDir(t))

	quotedSecret, quotedOutside := strconv.Quote(secret), strconv.Quote(outside)
	calls := []struct {
		tool      string
		arguments string
		// namesTheFile marks a call whose own argument spells secret.txt, so the refusal
		// echoing that name back proves nothing either way; only the calls that named the
		// DIRECTORY can be asked whether the tree behind the symlink was enumerated.
		namesTheFile bool
	}{
		{tool: "read_file", arguments: `{"path":` + quotedSecret + `}`, namesTheFile: true},
		{tool: "list_dir", arguments: `{"path":` + quotedOutside + `}`},
		{tool: "grep", arguments: `{"pattern":"sk-live","path":` + quotedSecret + `}`, namesTheFile: true},
		{tool: "grep", arguments: `{"pattern":"sk-live","path":` + quotedOutside + `}`},
		{tool: "find_files", arguments: `{"pattern":"*.txt","path":` + quotedOutside + `}`},
	}
	for _, call := range calls {
		t.Run(call.tool+" "+call.arguments, func(t *testing.T) {
			t.Parallel()

			result := readFenceExecute(t, w, call.tool, call.arguments)

			if !result.IsError || !strings.Contains(result.Content, "outside the workspace") {
				t.Errorf("%s(%s) = %q (IsError %v); want the outside-the-workspace refusal — the "+
					"symlinked .apogee/skills relocated the read fence", call.tool, call.arguments,
					result.Content, result.IsError)
			}
			if strings.Contains(result.Content, secretBytes) {
				t.Errorf("%s(%s) leaked the file behind the symlink: %q", call.tool, call.arguments, result.Content)
			}
			if !call.namesTheFile && strings.Contains(result.Content, "secret.txt") {
				t.Errorf("%s(%s) listed the tree behind the symlink: %q", call.tool, call.arguments, result.Content)
			}
		})
	}
}

// The rule is "resolved", not "no mounts": an honest `.apogee/skills` — a real directory in the
// repo — is still mounted, so the model can read the bundled files that live beside a skill's
// SKILL.md exactly as it could before the fix.
func TestRealSkillsDirStillMountsItsBundledFiles(t *testing.T) {
	t.Parallel()

	workspace := readFenceRealDir(t)
	const bundledBytes = "the bundled reference the skill points at"
	bundle := filepath.Join(workspace, ".apogee", "skills", "demo")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "SKILL.md"), []byte("---\nname: demo\ndescription: demo\n---\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	notes := filepath.Join(bundle, "notes.txt")
	if err := os.WriteFile(notes, []byte(bundledBytes), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	w := readFenceWiring(t, workspace, readFenceRealDir(t))

	result := readFenceExecute(t, w, "read_file", `{"path":`+strconv.Quote(notes)+`}`)

	if result.IsError || !strings.Contains(result.Content, bundledBytes) {
		t.Errorf("read_file(%q) = %q (IsError %v); want the bundled bytes — mounting by resolved "+
			"path took an honest skills dir away", notes, result.Content, result.IsError)
	}
}

// The Driver half's own promise, and the one behaviour the tools-side rule cannot deliver on its
// own: the apogee home is the operator's control plane, so a `~/.apogee/skills` that is a symlink
// into a dotfiles repo is a supported way to name the global library — discovery follows it
// (openAnchor) and the mount has to be pinned at what it RESOLVES to. Mounting the configured
// path instead would leave the read tools refusing every file in the operator's own library,
// because a root that is not its own real path is never matched (path_read.go's matchRoot).
func TestSymlinkedHomeLibraryStaysReadableThroughTheMount(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the operator's dotfiles library is named by a POSIX symlink")
	}

	workspace, configDir, dotfiles := readFenceRealDir(t), readFenceRealDir(t), readFenceRealDir(t)
	const bundledBytes = "the reference the operator's library skill points at"
	bundle := filepath.Join(dotfiles, "demo")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "SKILL.md"), []byte("---\nname: demo\ndescription: demo\n---\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	notes := filepath.Join(bundle, "notes.txt")
	if err := os.WriteFile(notes, []byte(bundledBytes), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(dotfiles, filepath.Join(configDir, "skills")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	w := readFenceWiring(t, workspace, configDir)

	result := readFenceExecute(t, w, "read_file", `{"path":`+strconv.Quote(notes)+`}`)

	if result.IsError || !strings.Contains(result.Content, bundledBytes) {
		t.Errorf("read_file(%q) = %q (IsError %v); want the bundled bytes — the mount was not pinned "+
			"at the path the operator's library symlink resolves to", notes, result.Content, result.IsError)
	}
}
