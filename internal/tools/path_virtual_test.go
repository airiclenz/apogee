package tools

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/airiclenz/apogee/internal/domain"
)

// demoMount is a stand-in for any embedded tree a host mounts (the shipped skills are the first,
// but nothing in this package knows what a skill is). Its contents are deliberately boring: what
// is under test is the ADDRESSING, not the bytes.
func demoMount() ReadMounts {
	tree := fstest.MapFS{
		"notes/SKILL.md":     &fstest.MapFile{Data: []byte("# notes\nfindable marker\n")},
		"notes/checklist.md": &fstest.MapFile{Data: []byte("one\ntwo\nthree\n")},
	}
	return ReadMounts{Virtual: func() map[string]fs.FS { return map[string]fs.FS{"demo:": tree} }}
}

// runTool executes tool with args and returns its text plus whether the tool refused, which is the
// only distinction every case below turns on.
func runTool(t *testing.T, tool domain.Tool, args map[string]any) (text string, refused bool) {
	t.Helper()
	res, err := tool.Execute(context.Background(), callWith(t, "c1", args))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	return res.Content, res.IsError
}

// All four read tools serve a mounted address, because the `files:` line a skill block announces
// names all four of them by name: an address one of them refuses is an announced path apogee's own
// harness made unreadable.
func TestVirtualMountServesEveryReadTool(t *testing.T) {
	root, mounts := tempRoot(t), demoMount()

	cases := []struct {
		name string
		tool domain.Tool
		args map[string]any
		want string
	}{
		{"read_file", NewReadFile(root, mounts), map[string]any{"path": "demo:notes/checklist.md"}, "two"},
		{"list_dir", NewListDir(root, mounts), map[string]any{"path": "demo:notes"}, "checklist.md"},
		{"grep", NewGrep(root, mounts), map[string]any{"pattern": "findable", "path": "demo:notes"}, "SKILL.md"},
		{"find_files", NewFindFiles(root, mounts), map[string]any{"pattern": "*.md", "path": "demo:notes"}, "demo:notes/checklist.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, refused := runTool(t, tc.tool, tc.args)
			if refused {
				t.Fatalf("%s refused the mounted address: %s", tc.name, text)
			}
			if !strings.Contains(text, tc.want) {
				t.Errorf("%s = %q, want it to mention %q", tc.name, text, tc.want)
			}
		})
	}
}

// find_files reports what it found under the MOUNT'S OWN address, not under a bare walk name: the
// paths it lists are addresses the model hands straight back to read_file, so a name missing its
// mount prefix would be a listing of files nothing can then open.
func TestVirtualFindFilesReportsMountAddresses(t *testing.T) {
	text, refused := runTool(t, NewFindFiles(tempRoot(t), demoMount()),
		map[string]any{"pattern": "*.md", "path": "demo:notes"})
	if refused {
		t.Fatalf("find_files refused the mounted address: %s", text)
	}
	for _, want := range []string{"demo:notes/SKILL.md", "demo:notes/checklist.md"} {
		if !strings.Contains(text, want) {
			t.Errorf("find_files = %q, want it to list %q", text, want)
		}
	}
}

// A copy's SOURCE is a read, which is the one sanctioned crossing (path_read.go), so a mounted file
// materializes into the workspace byte for byte — the step a skill's instructions call for when a
// bundled template is meant to become a project file.
func TestCopyFileMaterializesFromAVirtualMount(t *testing.T) {
	root := tempRoot(t)
	tool := NewCopyFile(root, demoMount())

	text, refused := runTool(t, tool, map[string]any{"source": "demo:notes/checklist.md", "destination": "list.md"})
	if refused {
		t.Fatalf("copy_file refused a mounted source: %s", text)
	}
	got, err := os.ReadFile(filepath.Join(root, "list.md"))
	if err != nil {
		t.Fatalf("the copy did not land: %v", err)
	}
	if string(got) != "one\ntwo\nthree\n" {
		t.Errorf("copied bytes = %q, want the mounted file verbatim", got)
	}

	// The destination rules are the disk copy's, unchanged: a second copy without overwrite is
	// refused rather than silently clobbering.
	if _, refused := runTool(t, tool, map[string]any{"source": "demo:notes/checklist.md", "destination": "list.md"}); !refused {
		t.Error("a second copy onto an existing destination was allowed without overwrite")
	}
}

// Nothing WRITES into a mount: there is no filesystem behind it, so every write verb refuses the
// address outright rather than resolving it to a colon-named file inside the workspace and
// reporting a landed write the model believed went to the mount.
func TestVirtualMountRefusesEveryWrite(t *testing.T) {
	root, mounts := tempRoot(t), demoMount()
	if err := os.WriteFile(filepath.Join(root, "here.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	cases := []struct {
		name string
		tool domain.Tool
		args map[string]any
	}{
		{"write_file", NewWriteFile(root), map[string]any{"path": "demo:notes/new.md", "content": "x"}},
		{"copy_file destination", NewCopyFile(root, mounts), map[string]any{"source": "here.md", "destination": "demo:notes/new.md"}},
		{"move_file destination", NewMoveFile(root), map[string]any{"source": "here.md", "destination": "demo:notes/new.md"}},
		{"delete_file", NewDeleteFile(root), map[string]any{"path": "demo:notes/SKILL.md"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, refused := runTool(t, tc.tool, tc.args)
			if !refused {
				t.Fatalf("%s wrote into a virtual mount: %s", tc.name, text)
			}
			if !strings.Contains(text, "outside the workspace root") {
				t.Errorf("%s refusal = %q, want the uniform out-of-root wording", tc.name, text)
			}
			if _, err := os.Stat(filepath.Join(root, "demo:notes")); err == nil {
				t.Errorf("%s created a colon-named path inside the workspace", tc.name)
			}
		})
	}
}

// The mount is the whole fence, so a reference that climbs out of it is refused with the same
// uniform escape message a disk root gives — never with absence, which reads to the model as a
// mis-spelling worth retrying.
func TestVirtualMountRefusesAClimbOut(t *testing.T) {
	text, refused := runTool(t, NewReadFile(tempRoot(t), demoMount()), map[string]any{"path": "demo:../secret"})
	if !refused {
		t.Fatalf("a climb out of the mount was served: %s", text)
	}
	if !strings.Contains(text, "outside the workspace root") {
		t.Errorf("refusal = %q, want the uniform out-of-root wording", text)
	}
}

// With no mounts registered the whole seam is inert: an address-shaped path is an ordinary
// relative name again, resolved against the workspace exactly as it was before any mount existed.
// That is the byte-identical guarantee the zero ReadMounts carries.
func TestUnmountedAddressResolvesOnDisk(t *testing.T) {
	root := tempRoot(t)
	if err := os.WriteFile(filepath.Join(root, "demo:file.md"), []byte("on disk"), 0o644); err != nil {
		t.Skipf("this filesystem rejects a colon in a file name: %v", err)
	}

	text, refused := runTool(t, NewReadFile(root, ReadMounts{}), map[string]any{"path": "demo:file.md"})
	if refused {
		t.Fatalf("an unmounted address stopped resolving on disk: %s", text)
	}
	if !strings.Contains(text, "on disk") {
		t.Errorf("read = %q, want the workspace file's bytes", text)
	}
}

// The mount grammar decides which spellings the writers refuse, so it is pinned directly: a
// Windows drive letter and an ordinary path must never read as a mount reference.
func TestVirtualMountRefGrammar(t *testing.T) {
	cases := []struct {
		in    string
		mount string
		rel   string
		ok    bool
	}{
		{in: "shipped:debugging", mount: "shipped:", rel: "debugging", ok: true},
		{in: "shipped:debugging/refs/x.md", mount: "shipped:", rel: "debugging/refs/x.md", ok: true},
		{in: "shipped:", mount: "shipped:", rel: ".", ok: true},
		{in: "shipped:../escape", mount: "shipped:", rel: "../escape", ok: true},
		{in: `C:\Users\me\notes.md`},
		{in: "D:/tmp/x"},
		{in: "docs/notes.md"},
		{in: "/abs/path"},
		{in: "dir/has:colon.md"},
		{in: "Shipped:debugging"},
	}
	for _, tc := range cases {
		mount, rel, ok := virtualMountRef(tc.in)
		if ok != tc.ok {
			t.Errorf("virtualMountRef(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if ok && (mount != tc.mount || rel != tc.rel) {
			t.Errorf("virtualMountRef(%q) = (%q, %q), want (%q, %q)", tc.in, mount, rel, tc.mount, tc.rel)
		}
	}
}
