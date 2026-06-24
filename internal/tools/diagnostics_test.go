package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
// toolchain-absent path is exercisable without depending on the host's PATH.
func withFakeGo(t *testing.T, found bool, path string) {
	t.Helper()
	orig := lookGo
	lookGo = func() (string, bool) { return path, found }
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
	d := NewDiagnostics(t.TempDir())
	if d.Name() != "diagnostics" {
		t.Errorf("Name() = %q, want diagnostics", d.Name())
	}
	if !domain.IsReadOnly(d) {
		t.Error("diagnostics must be read-only (it only inspects)")
	}
	if !domain.IsSubprocessTool(d) {
		t.Error("diagnostics must declare SubprocessTool (the go vet / linter half shells out)")
	}
	if IsWorkspaceScopedWriter(d) {
		t.Error("diagnostics must NOT be a workspace-scoped writer (it never writes)")
	}
}

func TestDiagnostics_PathRequired(t *testing.T) {
	t.Parallel()
	d := NewDiagnostics(t.TempDir())
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
	d := NewDiagnostics(t.TempDir())
	res, err := d.Execute(context.Background(), diagnosticsCall("c1", "../../etc/passwd"))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if !res.IsError {
		t.Errorf("path escape must be an error result, got %q", res.Content)
	}
}

func TestDiagnostics_UnsupportedLanguageDegradesGracefully(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
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
	dir := t.TempDir()
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
	dir := t.TempDir()
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
}

func TestDiagnostics_CleanGoFileNoVetRequested(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
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
	dir := t.TempDir()
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
	dir := t.TempDir()
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

func TestDiagnostics_ReadOnlyDoesNotConfine(t *testing.T) {
	// diagnostics is read-only, so the disposition never installs a confinement handle.
	// Even if one is present, the tool must not require it: a vet finding still reports
	// without the Confiner being load-bearing. Force the toolchain-absent branch so this
	// stays deterministic and toolchain-free; the syntax half proves the read path runs.
	withFakeGo(t, false, "")
	dir := t.TempDir()
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
