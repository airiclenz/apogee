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
	releaseScoopScript  = "scripts/release-scoop.sh"
	releaseWingetScript = "scripts/release-winget.sh"

	// executableIndexMode is the git index mode of a committed executable file.
	executableIndexMode = "100755"
)

// releaseScriptEnvKeys are the variables the release scripts read; the tests clear every one
// the developer's shell might carry, so only what a test sets explicitly reaches the script.
var releaseScriptEnvKeys = []string{
	"REPO", "VERSION", "BUCKET_REPO", "BUCKET_URL", "SUMS_FILE", "DRY_RUN", "GITHUB_TOKEN",
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

	stdout, _ := runReleaseScript(t, releaseScoopScript, "VERSION=v"+bare, "DRY_RUN=1", "SUMS_FILE="+sums)

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

// TestReleaseWingetDryRun runs scripts/release-winget.sh with DRY_RUN, a sentinel GITHUB_TOKEN and
// no komac on PATH, and checks the printed komac command: the package id, the bare version, both
// Windows archives' release URLs and --submit — and that the token's value is printed nowhere.
func TestReleaseWingetDryRun(t *testing.T) {
	t.Parallel()
	requireBash(t)

	const (
		bare        = "9.8.7"
		token       = "sentinel-token-must-not-print"
		releaseBase = "https://github.com/airiclenz/apogee/releases/download/v" + bare + "/"
	)
	stdout, stderr := runReleaseScript(t, releaseWingetScript,
		"VERSION=v"+bare, "DRY_RUN=1", "GITHUB_TOKEN="+token, pathWithoutKomac(t))

	command := strings.TrimSpace(string(stdout))
	want := "GITHUB_TOKEN=<redacted> komac update AiricLenz.Apogee --version " + bare +
		" --urls " + releaseBase + "apogee_" + bare + "_windows_amd64.zip " +
		releaseBase + "apogee_" + bare + "_windows_arm64.zip --submit"
	if command != want {
		t.Errorf("dry-run command:\n got: %s\nwant: %s", command, want)
	}
	if strings.Contains(string(stdout), token) || strings.Contains(stderr, token) {
		t.Errorf("the dry run printed the GITHUB_TOKEN value\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

// TestReleaseWingetRequiresKomac runs scripts/release-winget.sh for real with no komac on PATH: it
// must exit non-zero before any token lookup and name how to install komac.
func TestReleaseWingetRequiresKomac(t *testing.T) {
	t.Parallel()
	requireBash(t)

	cmd := releaseScriptCommand(releaseWingetScript, "VERSION=v9.8.7", pathWithoutKomac(t))
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("bash %s without komac: err = %v; want a non-zero exit\n%s", releaseWingetScript, err, out)
	}
	if !strings.Contains(string(out), "komac is not installed") || !strings.Contains(string(out), "install komac") {
		t.Errorf("the missing-komac failure gives no install hint:\n%s", out)
	}
}

// TestReleaseWingetScriptIsExecutable pins scripts/release-winget.sh's committed mode: `make
// release-winget` execs it directly, so a checkout without the executable bit fails there.
func TestReleaseWingetScriptIsExecutable(t *testing.T) {
	t.Parallel()

	requireExecutableInIndex(t, releaseWingetScript)
}

// requireBash skips the test where no bash is on PATH: the release scripts are bash.
func requireBash(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed; the release scripts cannot run here")
	}
}

// releaseScriptCommand builds the command that runs a release script under bash with the
// developer's release variables cleared and env added.
func releaseScriptCommand(script string, env ...string) *exec.Cmd {
	cmd := exec.Command("bash", script)
	cmd.Env = append(environWithout(releaseScriptEnvKeys), env...)
	return cmd
}

// runReleaseScript runs a release script as releaseScriptCommand builds it, failing the test on a
// non-zero exit. It returns the script's stdout and stderr.
func runReleaseScript(t *testing.T, script string, env ...string) (stdout []byte, stderr string) {
	t.Helper()

	cmd := releaseScriptCommand(script, env...)
	var errOut strings.Builder
	cmd.Stderr = &errOut
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("bash %s: %v\nstderr:\n%s", script, err, errOut.String())
	}
	return stdout, errOut.String()
}

// pathWithoutKomac returns a PATH entry holding only the external commands the release scripts
// need before they reach komac, so komac is absent from it whatever the developer has
// installed; a later PATH entry in an exec.Cmd's Env wins over the inherited one. It skips where
// dirname cannot be found or linked.
func pathWithoutKomac(t *testing.T) string {
	t.Helper()

	dirname, err := exec.LookPath("dirname")
	if err != nil {
		t.Skip("dirname is not installed; the release scripts cannot run here")
	}
	dir := t.TempDir()
	if err := os.Symlink(dirname, filepath.Join(dir, "dirname")); err != nil {
		t.Skipf("cannot link dirname into a komac-free PATH: %v", err)
	}
	return "PATH=" + dir
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
