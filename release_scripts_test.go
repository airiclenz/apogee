package apogee_test

// The release scripts under scripts/ publish a cut release to the package channels. They reach
// the network on purpose and are never run by `make check`; these tests drive only their offline
// halves (DRY_RUN with a fixture SHA256SUMS) and pin how they are committed.

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	releaseScoopScript = "scripts/release-scoop.sh"

	// executableIndexMode is the git index mode of a committed executable file.
	executableIndexMode = "100755"
)

// releaseScriptEnvKeys are the variables the release scripts read; the tests clear every one
// the developer's shell might carry, so only what a test sets explicitly reaches the script.
var releaseScriptEnvKeys = []string{
	"REPO", "VERSION", "BUCKET_REPO", "BUCKET_URL", "SUMS_FILE", "DRY_RUN",
}

// scoopArchitecture is one entry of a Scoop manifest's `architecture` block.
type scoopArchitecture struct {
	URL        string `json:"url"`
	Hash       string `json:"hash"`
	ExtractDir string `json:"extract_dir"`
}

// scoopManifest is the part of bucket/apogee.json the dry-run test asserts.
type scoopManifest struct {
	Version      string                       `json:"version"`
	Description  string                       `json:"description"`
	Homepage     string                       `json:"homepage"`
	License      string                       `json:"license"`
	Architecture map[string]scoopArchitecture `json:"architecture"`
	Bin          string                       `json:"bin"`
	Checkver     string                       `json:"checkver"`
	Autoupdate   struct {
		Architecture map[string]json.RawMessage `json:"architecture"`
	} `json:"autoupdate"`
}

// TestReleaseScoopDryRun runs scripts/release-scoop.sh with DRY_RUN and a fixture SHA256SUMS and
// checks the printed manifest: both Windows archives' release URLs, their hashes taken from the
// fixture, the extract dirs `make dist` packs, and the GitHub homepage Scoop's checkver needs.
func TestReleaseScoopDryRun(t *testing.T) {
	t.Parallel()
	requireBash(t)

	const (
		bare        = "9.8.7"
		amd64Hash   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		arm64Hash   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		linuxHash   = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		releaseBase = "https://github.com/airiclenz/apogee/releases/download/v" + bare + "/"
	)
	sums := filepath.Join(t.TempDir(), "SHA256SUMS")
	fixture := linuxHash + "  apogee_" + bare + "_linux_amd64.tar.gz\n" +
		amd64Hash + "  apogee_" + bare + "_windows_amd64.zip\n" +
		arm64Hash + "  apogee_" + bare + "_windows_arm64.zip\n"
	if err := os.WriteFile(sums, []byte(fixture), 0o600); err != nil {
		t.Fatalf("write the fixture SHA256SUMS: %v", err)
	}

	stdout := runReleaseScript(t, releaseScoopScript, "VERSION=v"+bare, "DRY_RUN=1", "SUMS_FILE="+sums)

	var manifest scoopManifest
	if err := json.Unmarshal(stdout, &manifest); err != nil {
		t.Fatalf("the dry run's stdout is not a JSON manifest: %v\n%s", err, stdout)
	}
	if manifest.Version != bare {
		t.Errorf("version = %q; want %q", manifest.Version, bare)
	}
	if manifest.Homepage != "https://github.com/airiclenz/apogee" {
		t.Errorf("homepage = %q; want the release repository's GitHub page", manifest.Homepage)
	}
	if manifest.Description == "" || manifest.License == "" {
		t.Errorf("description %q / license %q must both be set", manifest.Description, manifest.License)
	}
	if manifest.Bin != "apogee.exe" || manifest.Checkver != "github" {
		t.Errorf("bin = %q, checkver = %q; want apogee.exe and github", manifest.Bin, manifest.Checkver)
	}
	wants := map[string]scoopArchitecture{
		"64bit": {
			URL:        releaseBase + "apogee_" + bare + "_windows_amd64.zip",
			Hash:       amd64Hash,
			ExtractDir: "apogee_" + bare + "_windows_amd64",
		},
		"arm64": {
			URL:        releaseBase + "apogee_" + bare + "_windows_arm64.zip",
			Hash:       arm64Hash,
			ExtractDir: "apogee_" + bare + "_windows_arm64",
		},
	}
	for key, want := range wants {
		if got := manifest.Architecture[key]; got != want {
			t.Errorf("architecture.%s = %+v; want %+v", key, got, want)
		}
		if _, ok := manifest.Autoupdate.Architecture[key]; !ok {
			t.Errorf("autoupdate.architecture has no %s entry", key)
		}
	}
}

// TestReleaseScoopScriptIsExecutable pins scripts/release-scoop.sh's committed mode: `make
// release-scoop` execs it directly, so a checkout without the executable bit fails there.
func TestReleaseScoopScriptIsExecutable(t *testing.T) {
	t.Parallel()

	requireExecutableInIndex(t, releaseScoopScript)
}

// requireBash skips the test where no bash is on PATH: the release scripts are bash.
func requireBash(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed; the release scripts cannot run here")
	}
}

// runReleaseScript runs a release script under bash with the developer's release variables
// cleared and env added, failing the test on a non-zero exit. It returns the script's stdout.
func runReleaseScript(t *testing.T, script string, env ...string) []byte {
	t.Helper()

	cmd := exec.Command("bash", script)
	cmd.Env = append(environWithout(releaseScriptEnvKeys), env...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("bash %s: %v\nstderr:\n%s", script, err, stderr.String())
	}
	return stdout
}

// environWithout returns the process environment minus every variable named in keys.
func environWithout(keys []string) []string {
	environ := os.Environ()
	kept := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if !slices.Contains(keys, name) {
			kept = append(kept, entry)
		}
	}
	return kept
}

// requireExecutableInIndex fails unless git's index records path with mode 100755. It skips
// where git is absent or the tree is not a git checkout (a source archive carries no index).
func requireExecutableInIndex(t *testing.T, path string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; the index mode cannot be read")
	}
	if err := exec.Command("git", "rev-parse", "--git-dir").Run(); err != nil {
		t.Skip("not a git checkout; the index mode cannot be read")
	}
	out, err := exec.Command("git", "ls-files", "-s", "--", path).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("git ls-files -s %s: %v\n%s", path, err, exitErr.Stderr)
		}
		t.Fatalf("git ls-files -s %s: %v", path, err)
	}
	mode, _, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
	if mode != executableIndexMode {
		t.Errorf(
			"git ls-files -s %s reports mode %q; want %s — run git update-index --chmod=+x %s",
			path, mode, executableIndexMode, path,
		)
	}
}
