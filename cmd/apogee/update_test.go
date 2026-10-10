package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/update"
)

// The release a test install updates from and the one the stand-in server publishes.
const (
	updateTestCurrent = "v0.1.0"
	updateTestLatest  = "v0.2.0"
)

// installedBinary is what the test "exe" holds before an update: anything but the release.
const installedBinary = "installed binary"

// stubReleaseBinary is a release binary that only answers --version, reporting version the way
// cobra's --version does.
func stubReleaseBinary(version string) []byte {
	return []byte("#!/bin/sh\necho \"apogee version " + version + "+1.gabc\"\n")
}

// releaseServer is a stand-in for the GitHub repository: /releases/latest redirects to tag, and
// tag's download directory serves the running platform's archive holding binary plus a SHA256SUMS
// listing it. It records every request path so a test can assert nothing was downloaded.
type releaseServer struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []string
}

// newReleaseServer starts a releaseServer publishing binary as tag.
func newReleaseServer(t *testing.T, tag string, binary []byte) *releaseServer {
	t.Helper()
	stem := "apogee_" + strings.TrimPrefix(tag, "v") + "_" + runtime.GOOS + "_" + runtime.GOARCH
	archive := buildReleaseTarGz(t, stem+"/apogee", binary)
	sum := sha256.Sum256(archive)
	assets := map[string][]byte{
		stem + ".tar.gz": archive,
		"SHA256SUMS":     []byte(hex.EncodeToString(sum[:]) + "  " + stem + ".tar.gz\n"),
	}
	downloadDirectory := "/releases/download/" + tag + "/"

	release := &releaseServer{}
	release.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		release.mu.Lock()
		release.requests = append(release.requests, request.Method+" "+request.URL.Path)
		release.mu.Unlock()
		if request.URL.Path == "/releases/latest" {
			http.Redirect(writer, request, "/releases/tag/"+tag, http.StatusFound)
			return
		}
		content, isServed := assets[strings.TrimPrefix(request.URL.Path, downloadDirectory)]
		if !strings.HasPrefix(request.URL.Path, downloadDirectory) || !isServed {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write(content)
	}))
	t.Cleanup(release.server.Close)
	return release
}

// paths returns the requests the server has answered so far.
func (r *releaseServer) paths() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.requests)
}

// buildReleaseTarGz builds a release tar.gz holding binary as the executable entry name.
func buildReleaseTarGz(t *testing.T, name string, binary []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(compressed)
	header := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}
	if err := writer.WriteHeader(header); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := writer.Write(binary); err != nil {
		t.Fatalf("tar write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buffer.Bytes()
}

// installedExecutable writes the test install's "exe" into a fresh directory and returns its path.
func installedExecutable(t *testing.T) string {
	t.Helper()
	exePath := filepath.Join(t.TempDir(), "apogee")
	if err := os.WriteFile(exePath, []byte(installedBinary), 0o755); err != nil {
		t.Fatalf("write the installed binary: %v", err)
	}
	return exePath
}

// archiveUpdateDeps is a release-archive install of updateTestCurrent at exePath, talking to
// release, whose stdin answers answer and reports isTerminal.
func archiveUpdateDeps(release *releaseServer, exePath, answer string, isTerminal bool) updateDeps {
	return updateDeps{
		current:    updateTestCurrent,
		inputs:     func() update.Inputs { return update.Inputs{ExePath: exePath, DistBuild: true, HasVCS: true} },
		client:     update.NewClient(release.server.URL),
		isTerminal: func() bool { return isTerminal },
		stdin:      strings.NewReader(answer),
		goos:       runtime.GOOS,
		goarch:     runtime.GOARCH,
	}
}

// runUpdateCommand runs `apogee update args...` over deps and returns its stdout and error.
func runUpdateCommand(t *testing.T, deps updateDeps, args ...string) (string, error) {
	t.Helper()
	cmd := newUpdateCommandWith(deps)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := cmd.ExecuteContext(ctx)
	return stdout.String(), err
}

// requireFileContent fails unless path holds want.
func requireFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s holds %q, want %q", path, got, want)
	}
}

// requireOnlyFile fails unless exePath's directory holds exePath and nothing else: no staging
// directory and no `.old` left behind.
func requireOnlyFile(t *testing.T, exePath string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(exePath))
	if err != nil {
		t.Fatalf("read the install directory: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !slices.Equal(names, []string{filepath.Base(exePath)}) {
		t.Fatalf("the install directory holds %v, want only %s", names, filepath.Base(exePath))
	}
}

// skipWithoutShellStub skips a test that executes the shell-script release stub where no
// `/bin/sh` runs it.
func skipWithoutShellStub(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub release binary is a shell script")
	}
}

// Every install apogee does not own is refused with its channel's own upgrade command, exit 1,
// before any request reaches the release server.
func TestUpdate_ManagedInstall_RefusesWithChannelCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		inputs update.Inputs
		want   string
	}{
		{
			name:   "homebrew",
			inputs: update.Inputs{ExePath: "/opt/homebrew/Cellar/apogee/0.1.0/bin/apogee", DistBuild: true, HasVCS: true},
			want:   "apogee was installed via Homebrew — run: brew upgrade apogee",
		},
		{
			name:   "scoop",
			inputs: update.Inputs{ExePath: `C:\Users\dev\scoop\apps\apogee\current\apogee.exe`, DistBuild: true, HasVCS: true},
			want:   "apogee was installed via Scoop — run: scoop update apogee",
		},
		{
			name: "winget",
			inputs: update.Inputs{
				ExePath:   `C:\Users\dev\AppData\Local\Microsoft\WinGet\Packages\AiricLenz.Apogee_Microsoft.Winget.Source_8wekyb3d8bbwe\apogee.exe`,
				DistBuild: true, HasVCS: true,
			},
			want: "apogee was installed via winget — run: winget upgrade AiricLenz.Apogee",
		},
		{
			name:   "go install",
			inputs: update.Inputs{ExePath: "/home/dev/go/bin/apogee"},
			want:   "apogee was installed via go install — run: go install github.com/airiclenz/apogee/cmd/apogee@latest",
		},
		{
			name:   "source",
			inputs: update.Inputs{ExePath: "/home/dev/.local/bin/apogee", HasVCS: true},
			want:   "apogee was installed via a source build — run: git pull && make install",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			release := newReleaseServer(t, updateTestLatest, stubReleaseBinary(updateTestLatest))
			deps := archiveUpdateDeps(release, test.inputs.ExePath, "", true)
			deps.inputs = func() update.Inputs { return test.inputs }

			_, err := runUpdateCommand(t, deps, "--yes")

			if err == nil || err.Error() != test.want {
				t.Fatalf("apogee update error = %v, want %q", err, test.want)
			}
			if code := exitCodeFor(err); code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if requests := release.paths(); len(requests) != 0 {
				t.Errorf("the release server was asked %v; a managed install must send nothing", requests)
			}
		})
	}
}

// An archive install with --yes is replaced by the verified release, leaving nothing beside it.
// Serial: it executes a freshly written file, which a fork from a parallel test could hold open
// for writing (ETXTBSY).
func TestUpdate_ArchiveWithYes_ReplacesExecutable(t *testing.T) {
	skipWithoutShellStub(t)
	release := newReleaseServer(t, updateTestLatest, stubReleaseBinary(updateTestLatest))
	exePath := installedExecutable(t)

	stdout, err := runUpdateCommand(t, archiveUpdateDeps(release, exePath, "", false), "--yes")

	if err != nil {
		t.Fatalf("apogee update --yes: %v", err)
	}
	if want := "Updated apogee v0.1.0 → v0.2.0"; !strings.Contains(stdout, want) {
		t.Errorf("stdout = %q, want it to contain %q", stdout, want)
	}
	requireFileContent(t, exePath, string(stubReleaseBinary(updateTestLatest)))
	requireOnlyFile(t, exePath)
}

// A confirmation answered at the terminal decides: yes replaces the binary, anything else —
// an empty line above all — cancels with the binary untouched. Serial for the same ETXTBSY reason.
func TestUpdate_TerminalAnswer_DecidesTheUpdate(t *testing.T) {
	skipWithoutShellStub(t)
	tests := []struct {
		name       string
		answer     string
		wantOut    string
		wantBinary string
	}{
		{name: "yes", answer: "y\n", wantOut: "Updated apogee v0.1.0 → v0.2.0", wantBinary: string(stubReleaseBinary(updateTestLatest))},
		{name: "default no", answer: "\n", wantOut: "Update cancelled.", wantBinary: installedBinary},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			release := newReleaseServer(t, updateTestLatest, stubReleaseBinary(updateTestLatest))
			exePath := installedExecutable(t)

			stdout, err := runUpdateCommand(t, archiveUpdateDeps(release, exePath, test.answer, true))

			if err != nil {
				t.Fatalf("apogee update: %v", err)
			}
			if !strings.Contains(stdout, "Update v0.1.0 → v0.2.0? [y/N] ") || !strings.Contains(stdout, test.wantOut) {
				t.Errorf("stdout = %q, want the prompt and %q", stdout, test.wantOut)
			}
			requireFileContent(t, exePath, test.wantBinary)
			requireOnlyFile(t, exePath)
		})
	}
}

// A staged binary whose --version does not name the release is never swapped in. Serial for the
// same ETXTBSY reason.
func TestUpdate_StagedVersionMismatch_KeepsInstalledBinary(t *testing.T) {
	skipWithoutShellStub(t)
	release := newReleaseServer(t, updateTestLatest, stubReleaseBinary("v0.1.9"))
	exePath := installedExecutable(t)

	_, err := runUpdateCommand(t, archiveUpdateDeps(release, exePath, "", false), "--yes")

	if err == nil || !strings.Contains(err.Error(), "not v0.2.0") {
		t.Fatalf("apogee update --yes error = %v, want the version mismatch reported", err)
	}
	requireFileContent(t, exePath, installedBinary)
	requireOnlyFile(t, exePath)
}

// --check reports the newer release and downloads and writes nothing.
func TestUpdate_Check_ReportsWithoutWriting(t *testing.T) {
	t.Parallel()
	release := newReleaseServer(t, updateTestLatest, stubReleaseBinary(updateTestLatest))
	exePath := installedExecutable(t)

	stdout, err := runUpdateCommand(t, archiveUpdateDeps(release, exePath, "", false), "--check")

	if err != nil {
		t.Fatalf("apogee update --check: %v", err)
	}
	if want := "apogee v0.1.0 → v0.2.0 is available — run: apogee update"; !strings.Contains(stdout, want) {
		t.Errorf("stdout = %q, want it to contain %q", stdout, want)
	}
	if requests := release.paths(); !slices.Equal(requests, []string{"HEAD /releases/latest"}) {
		t.Errorf("the release server was asked %v, want only the latest-release lookup", requests)
	}
	requireFileContent(t, exePath, installedBinary)
	requireOnlyFile(t, exePath)
}

// Without --yes, a stdin that is not a terminal refuses with exit 1 before any download.
func TestUpdate_NonTerminalWithoutYes_Refuses(t *testing.T) {
	t.Parallel()
	release := newReleaseServer(t, updateTestLatest, stubReleaseBinary(updateTestLatest))
	exePath := installedExecutable(t)

	_, err := runUpdateCommand(t, archiveUpdateDeps(release, exePath, "y\n", false))

	if err == nil || !strings.Contains(err.Error(), "re-run with --yes") {
		t.Fatalf("apogee update error = %v, want the non-terminal refusal", err)
	}
	if code := exitCodeFor(err); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if requests := release.paths(); !slices.Equal(requests, []string{"HEAD /releases/latest"}) {
		t.Errorf("the release server was asked %v, want only the latest-release lookup", requests)
	}
	requireFileContent(t, exePath, installedBinary)
}

// An install already on the latest release says so and exits 0 without a prompt.
func TestUpdate_AlreadyCurrent_ReportsUpToDate(t *testing.T) {
	t.Parallel()
	release := newReleaseServer(t, updateTestCurrent, stubReleaseBinary(updateTestCurrent))
	exePath := installedExecutable(t)

	stdout, err := runUpdateCommand(t, archiveUpdateDeps(release, exePath, "", false))

	if err != nil {
		t.Fatalf("apogee update: %v", err)
	}
	if want := "apogee v0.1.0 is up to date\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	requireFileContent(t, exePath, installedBinary)
}

// The start-up sweep deletes a Windows swap's leftover `<exe>.old`, and a missing one is no error.
func TestUpdate_RemoveLeftoverExecutable_DeletesOldFile(t *testing.T) {
	t.Parallel()
	exePath := installedExecutable(t)
	if err := os.WriteFile(exePath+oldBinarySuffix, []byte(installedBinary), 0o755); err != nil {
		t.Fatalf("write the leftover: %v", err)
	}

	removeLeftoverExecutable(exePath)
	removeLeftoverExecutable(exePath)

	requireOnlyFile(t, exePath)
}

// The registration seam carries `update`.
func TestSubcommandsRegistersUpdate(t *testing.T) {
	t.Parallel()
	root := newRootCommand((&recordingLauncher{}).launch, subcommands()...)

	isRegistered := slices.ContainsFunc(root.Commands(), func(c *cobra.Command) bool { return c.Name() == "update" })

	if !isRegistered {
		t.Fatal("the shipped subcommand set does not register `update`")
	}
}
