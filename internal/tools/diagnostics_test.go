package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

func diagnosticsCall(id, path string) domain.ToolCall {
	args, _ := json.Marshal(map[string]string{"path": path})
	return domain.ToolCall{ID: id, Tool: "diagnostics", Arguments: args}
}

func diagnosticsCallNoVet(id, path string) domain.ToolCall {
	args, _ := json.Marshal(map[string]any{"path": path, "vet": false})
	return domain.ToolCall{ID: id, Tool: "diagnostics", Arguments: args}
}

// withFakeGo swaps lookGo for the duration of a test (restored on cleanup), so the
// toolchain-absent path is exercisable without depending on the host's PATH. It fakes the LOOK
// alone — the fence security.ResolveProgram applies to what the look answers is the real one,
// which is what makes the planted-toolchain refusal a genuine assertion.
func withFakeGo(t *testing.T, found bool, path string) {
	t.Helper()
	orig := lookGo
	lookGo = func(string) (string, error) {
		if !found {
			return "", exec.ErrNotFound
		}
		return path, nil
	}
	t.Cleanup(func() { lookGo = orig })
}

// realGo skips the test when no Go toolchain is on PATH, so the live go-vet runs
// stay green on a host without `go` (the tool's graceful contract).
func realGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no 'go' toolchain on PATH; skipping the live go-vet run")
	}
}

// writeGoFile writes content to name under dir and returns the absolute path.
func writeGoFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// writeGoModule seeds dir with a minimal go.mod so `go vet` has a real module to
// build and vet (a bare directory makes vet fail with a "go.mod not found" build
// error rather than report findings — a normal workspace is always a module).
func writeGoModule(t *testing.T, dir string) {
	t.Helper()
	writeGoFile(t, dir, "go.mod", "module diagtest\n\ngo 1.26\n")
}

func TestDiagnostics_Markers(t *testing.T) {
	t.Parallel()
	d := NewDiagnostics(tempRoot(t))
	if d.Name() != "diagnostics" {
		t.Errorf("Name() = %q, want diagnostics", d.Name())
	}
	if !domain.IsReadOnly(d) {
		t.Error("diagnostics must be read-only (it only inspects)")
	}
	// As with git_diff_range, the read-only declaration is an honest statement about the tool
	// while the subprocess marker is what classifies the call — and, since 2026-08-02, what the
	// Plan menu filters on (§4 amended 2026-07-26 — the unfakeable marker outranks the
	// self-declaration); TestClassifyTool pins the class.
	if !domain.IsSubprocessTool(d) {
		t.Error("diagnostics must declare SubprocessTool (the go vet / linter half shells out)")
	}
	if IsWorkspaceScopedWriter(d) {
		t.Error("diagnostics must NOT be a workspace-scoped writer (it never writes)")
	}
}

func TestDiagnostics_PathRequired(t *testing.T) {
	t.Parallel()
	d := NewDiagnostics(tempRoot(t))
	res, err := d.Execute(context.Background(), diagnosticsCall("c1", "   "))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "path is required") {
		t.Errorf("result = %q, want a 'path is required' error", res.Content)
	}
}

func TestDiagnostics_PathEscapeRejected(t *testing.T) {
	t.Parallel()
	d := NewDiagnostics(tempRoot(t))
	res, err := d.Execute(context.Background(), diagnosticsCall("c1", "../../etc/passwd"))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if !res.IsError {
		t.Errorf("path escape must be an error result, got %q", res.Content)
	}
}

// TestDiagnostics_RefusesEscapingSymlink pins the workspace fence on the bytes diagnostics
// actually PARSES. A clone can ship `main.go -> ~/.ssh/id_rsa`, and a syntax diagnostic
// quotes the offending source line back to the model, so an unfenced parse is a read of the
// target's content. The refusal must also stay a refusal in the wording — reported as
// "not found" it would read as an absent file and invite a retry.
//
// Like the sibling tools, this is a boundary pin: resolveInRoot already refuses the escape,
// and the fix closes the check-then-use gap behind it by reading the source through the
// pinned root once and handing the BYTES to go/parser instead of the path.
func TestDiagnostics_RefusesEscapingSymlink(t *testing.T) {
	t.Parallel()

	dir := tempRoot(t)
	outside := tempRoot(t)
	// Broken on purpose: a syntax error is what would quote the outside file's own source
	// line back into the result.
	writeGoFile(t, outside, "secret.go", "package secret\n\nfunc Secret() {\n\tapiKey :=\n}\n")
	writeGoFile(t, dir, "clean.go", "package clean\n\nfunc Clean() {}\n")
	if err := os.Symlink(filepath.Join(outside, "secret.go"), filepath.Join(dir, "escape.go")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	// A RELATIVE in-workspace symlink must still be diagnosed: the fence narrows what leaves
	// the workspace, never what stays inside it.
	if err := os.Symlink("clean.go", filepath.Join(dir, "link.go")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	d := NewDiagnostics(dir)

	t.Run("escaping symlink", func(t *testing.T) {
		t.Parallel()

		res, err := d.Execute(context.Background(), diagnosticsCallNoVet("c1", "escape.go"))
		if err != nil {
			t.Fatalf("Execute err = %v, want nil", err)
		}
		if !res.IsError {
			t.Fatalf("IsError = false, want true: a source file outside the workspace was parsed (%q)", res.Content)
		}
		if !strings.Contains(res.Content, "outside the workspace") {
			t.Errorf("content %q does not carry the path-escape message", res.Content)
		}
		if strings.Contains(res.Content, "apiKey") {
			t.Errorf("content quoted the source outside the workspace: %q", res.Content)
		}
	})

	t.Run("in-workspace symlink still diagnosed", func(t *testing.T) {
		t.Parallel()

		res, err := d.Execute(context.Background(), diagnosticsCallNoVet("c2", "link.go"))
		if err != nil {
			t.Fatalf("Execute err = %v, want nil", err)
		}
		if res.IsError {
			t.Fatalf("an in-workspace symlink stopped being diagnosed: %q", res.Content)
		}
		if !strings.Contains(res.Content, "clean.go") {
			t.Errorf("result = %q, want it to name the resolved file", res.Content)
		}
	})
}

func TestDiagnostics_UnsupportedLanguageDegradesGracefully(t *testing.T) {
	t.Parallel()
	dir := tempRoot(t)
	// A .rs file has no built-in provider and no probed linter here.
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte("fn main() {}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	d := NewDiagnostics(dir)
	res, err := d.Execute(context.Background(), diagnosticsCall("c1", "main.rs"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil (graceful degradation)", err)
	}
	if res.IsError {
		t.Errorf("unsupported language must NOT be an error result: %q", res.Content)
	}
	if !strings.Contains(res.Content, "no diagnostics available") {
		t.Errorf("result = %q, want a clear 'no diagnostics available' message", res.Content)
	}
}

func TestDiagnostics_GoSyntaxErrorReportedInProcess(t *testing.T) {
	// Not parallel: withFakeGo swaps the package-level lookGo var. Proves the syntax
	// half needs NO toolchain — a syntax error is reported even with go absent.
	withFakeGo(t, false, "")
	dir := tempRoot(t)
	writeGoFile(t, dir, "broken.go", "package main\n\nfunc main() {\n\tx :=\n}\n")
	d := NewDiagnostics(dir)
	res, err := d.Execute(context.Background(), diagnosticsCall("c1", "broken.go"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if !res.IsError {
		t.Errorf("a Go syntax error must be an error result, got clean: %q", res.Content)
	}
	if !strings.Contains(res.Content, "broken.go") {
		t.Errorf("syntax diagnostic = %q, want it to name the file/location", res.Content)
	}
}

func TestDiagnostics_CleanGoFileWithVetSkipNote(t *testing.T) {
	// Not parallel: withFakeGo swaps lookGo (force the toolchain-absent branch so the
	// result is deterministic regardless of the host).
	withFakeGo(t, false, "")
	dir := tempRoot(t)
	writeGoFile(t, dir, "clean.go", "package main\n\nfunc main() {}\n")
	d := NewDiagnostics(dir)
	res, err := d.Execute(context.Background(), diagnosticsCall("c1", "clean.go"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if res.IsError {
		t.Errorf("a clean Go file must be a success result: %q", res.Content)
	}
	if !strings.Contains(res.Content, "looks clean") {
		t.Errorf("result = %q, want it to confirm the file looks clean", res.Content)
	}
	if !strings.Contains(res.Content, "go vet skipped") {
		t.Errorf("result = %q, want a note that go vet was skipped (toolchain absent)", res.Content)
	}
	// The scope sentence belongs to a vet that RAN: nothing beside the named file was read
	// here, so claiming the package was checked would be the same dishonesty in reverse.
	if strings.Contains(res.Content, "whole package directory") {
		t.Errorf("a skipped vet must not claim it checked the package: %q", res.Content)
	}
}

func TestDiagnostics_CleanGoFileNoVetRequested(t *testing.T) {
	t.Parallel()
	dir := tempRoot(t)
	writeGoFile(t, dir, "clean.go", "package main\n\nfunc main() {}\n")
	d := NewDiagnostics(dir)
	res, err := d.Execute(context.Background(), diagnosticsCallNoVet("c1", "clean.go"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if res.IsError {
		t.Errorf("a clean Go file (vet skipped by request) must be a success result: %q", res.Content)
	}
	if strings.Contains(res.Content, "go vet skipped") {
		t.Errorf("vet:false should NOT print the toolchain-absent note: %q", res.Content)
	}
}

func TestDiagnostics_GoVetFindingReported(t *testing.T) {
	realGo(t)
	t.Parallel()
	dir := tempRoot(t)
	writeGoModule(t, dir)
	// A Printf format-string mismatch is a classic go vet finding (parses fine).
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Printf(\"%d\\n\", \"not a number\")\n}\n"
	writeGoFile(t, dir, "vetme.go", src)
	d := NewDiagnostics(dir)
	res, err := d.Execute(context.Background(), diagnosticsCall("c1", "vetme.go"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if !res.IsError {
		t.Errorf("a go vet finding must be an error result, got clean: %q", res.Content)
	}
	if !strings.Contains(res.Content, "Printf") && !strings.Contains(res.Content, "format") {
		t.Errorf("vet result = %q, want it to mention the Printf/format finding", res.Content)
	}
}

func TestDiagnostics_CleanGoFilePassesVet(t *testing.T) {
	realGo(t)
	t.Parallel()
	dir := tempRoot(t)
	writeGoModule(t, dir)
	writeGoFile(t, dir, "ok.go", "package main\n\nfunc main() {\n\t_ = 1 + 1\n}\n")
	d := NewDiagnostics(dir)
	res, err := d.Execute(context.Background(), diagnosticsCall("c1", "ok.go"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if res.IsError {
		t.Errorf("a clean Go file should pass go vet: %q", res.Content)
	}
	if !strings.Contains(res.Content, "looks clean") {
		t.Errorf("result = %q, want it to confirm the file looks clean", res.Content)
	}
}

// envValues folds an exec environment into effective values the way os/exec does —
// duplicates resolve last-wins — so a test asserts what the process really sees rather
// than what the slice happens to contain twice.
func envValues(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, entry := range env {
		if key, value, ok := strings.Cut(entry, "="); ok {
			out[key] = value
		}
	}
	return out
}

func TestDiagnostics_VetSubprocessEnvironmentIsPinned(t *testing.T) {
	// Not parallel: t.Setenv. The point of the fixture is that every one of these is a
	// setting the HOST already had, spelled the way an operator's `go env -w` or an
	// inherited shell would spell it — the vet subprocess must run on the pins regardless.
	t.Setenv("GOFLAGS", "-toolexec=/tmp/evil -mod=mod")
	t.Setenv("GOWORK", "/tmp/attacker.work")
	t.Setenv("GOTOOLCHAIN", "auto")
	t.Setenv("CGO_ENABLED", "1")
	t.Setenv("GOENV", "/tmp/attacker/env")
	t.Setenv("GOPATH", "/tmp/attacker/gopath")
	t.Setenv("CC", "/tmp/attacker/cc")
	t.Setenv("GIT_AUTHOR_NAME", "somebody")

	root := tempRoot(t)
	abs := filepath.Join(root, "pkg", "file.go")
	spec := goVetSpec("/usr/bin/go", root, abs)

	if got, want := spec.argv, []string{"/usr/bin/go", "vet", filepath.Join(root, "pkg")}; !slices.Equal(got, want) {
		t.Errorf("vet argv = %q, want %q (the PACKAGE directory, not the file)", got, want)
	}
	if spec.dir != root {
		t.Errorf("vet dir = %q, want the workspace root %q", spec.dir, root)
	}
	if spec.timeout != vetTimeout {
		t.Errorf("vet timeout = %v, want %v", spec.timeout, vetTimeout)
	}

	env := envValues(spec.env)
	// The pins: each one is the value the toolchain runs on whatever the host said.
	for key, want := range map[string]string{
		"GOFLAGS":     "-mod=readonly",
		"GOWORK":      "off",
		"GOTOOLCHAIN": "local",
		"CGO_ENABLED": "0",
		"GOENV":       "off",
	} {
		if got, ok := env[key]; !ok || got != want {
			t.Errorf("vet env %s = %q (present=%v), want %q", key, got, ok, want)
		}
	}
	// The allowlist: what a build cache needs, and nothing the host can steer the
	// toolchain with. GIT_AUTHOR_NAME is the tell that this is no longer git's list.
	for _, key := range []string{"GOPATH", "GOMODCACHE", "CC", "GIT_AUTHOR_NAME"} {
		if got, ok := env[key]; ok {
			t.Errorf("vet env inherited %s = %q, want it dropped", key, got)
		}
	}
	if _, ok := env["PATH"]; !ok {
		t.Error("vet env has no PATH; the toolchain resolves its own programs with it")
	}
	if home, set := os.LookupEnv("HOME"); set {
		if got, ok := env["HOME"]; !ok || got != home {
			t.Errorf("vet env HOME = %q (present=%v), want the host's %q (GOCACHE lives under it)", got, ok, home)
		}
	}
}

func TestDiagnostics_VetResultNamesThePackageDirectory(t *testing.T) {
	realGo(t)
	// The call names ONE file; the subprocess reads the whole directory. Both vet
	// outcomes must say which directory that was, or the result describes a narrower
	// action than the one that ran.
	cases := []struct {
		name    string
		src     string
		isError bool
	}{
		{name: "clean", src: "package pkgdir\n\nfunc Add(a, b int) int { return a + b }\n"},
		{
			name:    "findings",
			src:     "package pkgdir\n\nimport \"fmt\"\n\nfunc Bad() { fmt.Printf(\"%d\\n\", \"not a number\") }\n",
			isError: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := tempRoot(t)
			writeGoModule(t, root)
			pkg := filepath.Join(root, "pkgdir")
			if err := os.Mkdir(pkg, 0o700); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			writeGoFile(t, pkg, "ok.go", tc.src)
			// A sibling the call does NOT name — the file the operator would not know was
			// read if the result only echoed the argument.
			writeGoFile(t, pkg, "sibling.go", "package pkgdir\n\nfunc Sub(a, b int) int { return a - b }\n")

			d := NewDiagnostics(root)
			res, err := d.Execute(context.Background(), diagnosticsCall("c1", filepath.Join("pkgdir", "ok.go")))
			if err != nil {
				t.Fatalf("Execute err = %v, want nil", err)
			}
			if res.IsError != tc.isError {
				t.Fatalf("IsError = %v, want %v: %q", res.IsError, tc.isError, res.Content)
			}
			if !strings.Contains(res.Content, "pkgdir") {
				t.Errorf("result = %q, want it to name the vetted package directory", res.Content)
			}
			if !strings.Contains(res.Content, "whole package directory") {
				t.Errorf("result = %q, want it to say the whole package was checked", res.Content)
			}
			if !strings.Contains(res.Content, "ok.go") {
				t.Errorf("result = %q, want the requested file named beside the package", res.Content)
			}
		})
	}
}

func TestDiagnostics_RunsWithoutAConfinementHandle(t *testing.T) {
	// diagnostics takes the SUBPROCESS class (§4 amended 2026-07-26), so Auto installs a
	// confinement handle — but every other rung runs it without one (an approved gate,
	// "I am the sandbox", the library seam). The tool must therefore never REQUIRE the
	// handle: the diagnosis still reports with no Confiner in play. Force the
	// toolchain-absent branch so this stays deterministic and toolchain-free; the syntax
	// half proves the read path runs.
	withFakeGo(t, false, "")
	dir := tempRoot(t)
	writeGoFile(t, dir, "clean.go", "package main\n\nfunc main() {}\n")
	d := NewDiagnostics(dir)
	res, err := d.Execute(context.Background(), diagnosticsCall("c1", "clean.go"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if res.IsError {
		t.Errorf("clean file errored: %q", res.Content)
	}
}

// The vet half reads WIDER than the call that asks for it — one filename in, every .go file in
// that file's directory read — and the tool said so on its description and on both result
// strings while the approval pane, the surface a human actually decides on, said nothing.
// ApprovalScope (domain.ApprovalScoper) is the tool's line for that pane, and the fact worth
// pinning is that it names the SAME package directory the result will name: two surfaces
// describing one call differently is the failure the disclosure exists to prevent.
func TestDiagnostics_ApprovalScopeNamesTheSamePackageAsTheResult(t *testing.T) {
	t.Parallel()
	root := tempRoot(t)
	pkg := filepath.Join(root, "pkgdir")
	if err := os.Mkdir(pkg, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	abs := writeGoFile(t, pkg, "ok.go", "package pkgdir\n\nfunc Add(a, b int) int { return a + b }\n")
	named := filepath.Join("pkgdir", "ok.go")

	d := NewDiagnostics(root)
	scope := d.ApprovalScope(diagnosticsCall("c1", named))

	clause := vettedPackageScope(abs, root)
	if !strings.Contains(scope, clause) {
		t.Errorf("ApprovalScope = %q, want it to carry the vetted-package clause %q", scope, clause)
	}
	if !strings.Contains(vettedPackageLine(abs, root), clause) {
		t.Errorf("the result line stopped deriving the clause the pane shows: %q", vettedPackageLine(abs, root))
	}
	if !strings.Contains(scope, "pkgdir") || !strings.Contains(scope, "ok.go") {
		t.Errorf("ApprovalScope = %q, want the package directory named beside the requested file", scope)
	}
	if strings.Contains(scope, "\n") {
		t.Errorf("ApprovalScope = %q, want ONE line — the pane gives it a single row", scope)
	}
}

// Every call whose arguments already name their own reach declares no scope, so the pane it
// raises is unchanged: an explicit vet:false reads only the named file, a non-Go file has no vet
// half at all, and a path that escapes the workspace is a call Execute refuses outright.
func TestDiagnostics_ApprovalScopeIsEmptyWhenTheCallReadsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	root := tempRoot(t)
	writeGoFile(t, root, "ok.go", "package main\n\nfunc main() {}\n")
	writeGoFile(t, root, "app.ts", "export const x = 1;\n")
	d := NewDiagnostics(root)

	cases := []struct {
		name string
		call domain.ToolCall
	}{
		{name: "vet explicitly off", call: diagnosticsCallNoVet("c1", "ok.go")},
		{name: "not a Go file", call: diagnosticsCall("c2", "app.ts")},
		{name: "path escapes the workspace", call: diagnosticsCall("c3", filepath.Join("..", "elsewhere.go"))},
		{name: "no path at all", call: diagnosticsCall("c4", "")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := d.ApprovalScope(tc.call); got != "" {
				t.Errorf("ApprovalScope = %q, want empty — the call reads only what its arguments name", got)
			}
		})
	}
}
