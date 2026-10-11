package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The source tree every dist test builds from.
const (
	testVersion     = "v1.2.3\n"
	testBareVersion = "1.2.3"
	testLicense     = "the license\n"
	testReadme      = "the readme\n"
	testCommitTime  = "1760000000\n"
	testBuildCount  = "4321\n"
)

// testSourceTree writes a minimal apogee source tree — the three files dist reads itself — and
// returns its path.
func testSourceTree(t *testing.T) string {
	t.Helper()
	sourceDir := t.TempDir()
	files := map[string]string{"VERSION": testVersion, "LICENSE": testLicense, "README.md": testReadme}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(sourceDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return sourceDir
}

// stubBinary is what the fake `go build` writes for a target: distinct per platform, so a test
// can tell which build ended up in which archive.
func stubBinary(env []string) string {
	return fmt.Sprintf("stub binary %v\n", env)
}

// fakeToolchain answers dist's commands the way git and go would over a checkout whose commit
// count is buildCount: the git queries return their values, and `go build` writes a stub binary
// at its -o path.
func fakeToolchain(buildCount string) *fakeRunner {
	return &fakeRunner{respond: func(call recordedCall) (string, error) {
		switch {
		case call.name == "git" && slices.Equal(call.args, []string{"rev-list", "--count", "HEAD"}):
			return buildCount, nil
		case call.name == "git" && slices.Equal(call.args, []string{"log", "-1", "--format=%ct", "HEAD"}):
			return testCommitTime, nil
		case call.name == "go" && len(call.args) > 0 && call.args[0] == "build":
			output := call.args[slices.Index(call.args, "-o")+1]
			return "", os.WriteFile(output, []byte(stubBinary(call.env)), 0o755)
		}
		return "", fmt.Errorf("unexpected command %s %v", call.name, call.args)
	}}
}

// runTestDist runs `release dist` over sourceDir into a fresh output directory and returns it.
func runTestDist(t *testing.T, commands runner, sourceDir string) string {
	t.Helper()
	outDir := filepath.Join(t.TempDir(), "out")
	if output, err := execute(t, commands, "dist", "--src", sourceDir, "--out", outDir); err != nil {
		t.Fatalf("dist failed: %v\n%s", err, output)
	}
	return outDir
}

// TestDistBuildsEveryTargetWithTheReleaseLdflags pins the six cross-builds: the platform
// environment, the -trimpath build of ./cmd/apogee, and the exact -ldflags string — build number
// first, release stamp second, the number dropped when git reports none.
func TestDistBuildsEveryTargetWithTheReleaseLdflags(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		buildCount string
		ldflags    string
	}{
		"with a build count": {
			buildCount: testBuildCount,
			ldflags:    "-X github.com/airiclenz/apogee.buildCount=4321 -X github.com/airiclenz/apogee.distBuild=release-archive",
		},
		"without a build count": {
			buildCount: "",
			ldflags:    "-X github.com/airiclenz/apogee.distBuild=release-archive",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sourceDir := testSourceTree(t)
			commands := fakeToolchain(tc.buildCount)

			runTestDist(t, commands, sourceDir)

			builds := commands.recorded("go")
			if len(builds) != len(distTargets) {
				t.Fatalf("got %d go builds, want %d", len(builds), len(distTargets))
			}
			for i, target := range distTargets {
				build := builds[i]
				wantEnv := []string{"CGO_ENABLED=0", "GOOS=" + target.goos, "GOARCH=" + target.goarch}
				if !slices.Equal(build.env, wantEnv) {
					t.Errorf("build %d env = %v, want %v", i, build.env, wantEnv)
				}
				if build.dir != sourceDir {
					t.Errorf("build %d ran in %s, want the source tree %s", i, build.dir, sourceDir)
				}
				output := build.args[slices.Index(build.args, "-o")+1]
				wantArgs := []string{"build", "-trimpath", "-ldflags", tc.ldflags, "-o", output, "./cmd/apogee"}
				if !slices.Equal(build.args, wantArgs) {
					t.Errorf("build %d args = %q, want %q", i, build.args, wantArgs)
				}
				wantTail := filepath.Join(target.stem(testBareVersion), target.binaryFile())
				if !filepath.IsAbs(output) || !strings.HasSuffix(output, string(filepath.Separator)+wantTail) {
					t.Errorf("build %d writes %s, want an absolute path ending in %s", i, output, wantTail)
				}
			}
		})
	}
}

// archiveFile is one entry read back out of an archive.
type archiveFile struct {
	name    string
	mode    fs.FileMode
	content string
}

// TestDistArchivesHoldTheBinaryAndDocsUnderTheStem reads every archive back the way `apogee
// update` does: one top-level apogee_<version>_<os>_<arch> directory holding the platform's
// binary (executable, a regular file) and the two documents, entries in sorted order.
func TestDistArchivesHoldTheBinaryAndDocsUnderTheStem(t *testing.T) {
	t.Parallel()
	sourceDir := testSourceTree(t)

	outDir := runTestDist(t, fakeToolchain(testBuildCount), sourceDir)

	for _, target := range distTargets {
		stem := target.stem(testBareVersion)
		binary := stubBinary([]string{"CGO_ENABLED=0", "GOOS=" + target.goos, "GOARCH=" + target.goarch})
		want := []archiveFile{
			{name: stem + "/", mode: fs.ModeDir | 0o755},
			{name: stem + "/LICENSE", mode: 0o644, content: testLicense},
			{name: stem + "/README.md", mode: 0o644, content: testReadme},
			{name: stem + "/" + target.binaryFile(), mode: 0o755, content: binary},
		}

		got := readArchive(t, filepath.Join(outDir, target.archiveName(testBareVersion)))

		if !slices.Equal(got, want) {
			t.Errorf("%s entries =\n%v\nwant\n%v", target.archiveName(testBareVersion), got, want)
		}
	}
}

// readArchive returns the entries of the .tar.gz or .zip at archivePath, in archive order.
func readArchive(t *testing.T, archivePath string) []archiveFile {
	t.Helper()
	raw, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(archivePath, ".zip") {
		return readZip(t, raw)
	}
	return readTarGz(t, raw)
}

// readZip returns a zip archive's entries.
func readZip(t *testing.T, raw []byte) []archiveFile {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var files []archiveFile
	for _, file := range reader.File {
		entry, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(entry)
		_ = entry.Close()
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, archiveFile{name: file.Name, mode: file.Mode(), content: string(content)})
	}
	return files
}

// readTarGz returns a tar.gz archive's entries, checking each header's owner and kind on the
// way: a regular file must be TypeReg, the only kind `apogee update` extracts.
func readTarGz(t *testing.T, raw []byte) []archiveFile {
	t.Helper()
	decompressed, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if decompressed.Name != "" || !decompressed.ModTime.IsZero() {
		t.Errorf("gzip header carries name %q and time %v, want neither", decompressed.Name, decompressed.ModTime)
	}
	reader := tar.NewReader(decompressed)
	var files []archiveFile
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return files
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Uid != 0 || header.Gid != 0 {
			t.Errorf("%s owner = %d:%d, want 0:0", header.Name, header.Uid, header.Gid)
		}
		mode := fs.FileMode(header.Mode)
		switch header.Typeflag {
		case tar.TypeDir:
			mode |= fs.ModeDir
		case tar.TypeReg:
		default:
			t.Errorf("%s has type %q, want a directory or a regular file", header.Name, header.Typeflag)
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, archiveFile{name: header.Name, mode: mode, content: string(content)})
	}
}

// TestDistWritesSHA256SUMSSortedByName pins the checksum file `apogee update` and every package
// channel read: one `<hex>  <name>` line per archive, sorted by name, nothing else in the
// directory.
func TestDistWritesSHA256SUMSSortedByName(t *testing.T) {
	t.Parallel()
	sourceDir := testSourceTree(t)

	outDir := runTestDist(t, fakeToolchain(testBuildCount), sourceDir)

	names := []string{
		"apogee_1.2.3_darwin_amd64.tar.gz",
		"apogee_1.2.3_darwin_arm64.tar.gz",
		"apogee_1.2.3_linux_amd64.tar.gz",
		"apogee_1.2.3_linux_arm64.tar.gz",
		"apogee_1.2.3_windows_amd64.zip",
		"apogee_1.2.3_windows_arm64.zip",
	}
	var want strings.Builder
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		fmt.Fprintf(&want, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	got, err := os.ReadFile(filepath.Join(outDir, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want.String() {
		t.Errorf("SHA256SUMS =\n%s\nwant\n%s", got, want.String())
	}
	if listed := listDir(t, outDir); !slices.Equal(listed, append([]string{"SHA256SUMS"}, names...)) {
		t.Errorf("output directory holds %v, want the six archives and SHA256SUMS", listed)
	}
}

// listDir returns the sorted names in dir.
func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	return names
}

// TestDistIsByteIdenticalAcrossRuns pins reproducibility: two runs over the same tree — at
// different times, into different directories — write the same bytes, so a published checksum
// can be re-derived from the tag.
func TestDistIsByteIdenticalAcrossRuns(t *testing.T) {
	t.Parallel()
	sourceDir := testSourceTree(t)

	first := runTestDist(t, fakeToolchain(testBuildCount), sourceDir)
	second := runTestDist(t, fakeToolchain(testBuildCount), sourceDir)

	names := listDir(t, first)
	if !slices.Equal(names, listDir(t, second)) {
		t.Fatalf("the two runs wrote different files: %v and %v", names, listDir(t, second))
	}
	for _, name := range names {
		firstBytes, err := os.ReadFile(filepath.Join(first, name))
		if err != nil {
			t.Fatal(err)
		}
		secondBytes, err := os.ReadFile(filepath.Join(second, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(firstBytes, secondBytes) {
			t.Errorf("%s differs between two runs over the same tree", name)
		}
	}
}

// TestDistDefaultsToAnEmptiedDistDirectory pins the default output: <src>/dist, emptied first so
// a stale archive from an earlier version never rides along into a release.
func TestDistDefaultsToAnEmptiedDistDirectory(t *testing.T) {
	t.Parallel()
	sourceDir := testSourceTree(t)
	stale := filepath.Join(sourceDir, "dist", "apogee_1.2.2_linux_amd64.tar.gz")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	if output, err := execute(t, fakeToolchain(testBuildCount), "dist", "--src", sourceDir); err != nil {
		t.Fatalf("dist failed: %v\n%s", err, output)
	}

	if _, err := os.Stat(stale); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the stale archive survived the run (stat: %v)", err)
	}
	if listed := listDir(t, filepath.Join(sourceDir, "dist")); len(listed) != len(distTargets)+1 {
		t.Errorf("dist/ holds %v, want the six archives and SHA256SUMS", listed)
	}
}

// TestDistRefusesAnOutputThatHoldsTheSource pins the guard on the one destructive step: an --out
// that is the source tree, or contains it, would be emptied with the tree inside it.
func TestDistRefusesAnOutputThatHoldsTheSource(t *testing.T) {
	t.Parallel()
	sourceDir := testSourceTree(t)

	for _, outDir := range []string{sourceDir, filepath.Dir(sourceDir)} {
		_, err := execute(t, fakeToolchain(testBuildCount), "dist", "--src", sourceDir, "--out", outDir)

		if code := exitCodeFor(err); err == nil || code != exitRunFailed {
			t.Errorf("dist --out %s: err = %v (exit %d), want a run failure", outDir, err, code)
		}
		if _, statErr := os.Stat(filepath.Join(sourceDir, "VERSION")); statErr != nil {
			t.Fatalf("the source tree was emptied: %v", statErr)
		}
	}
}

// TestDistBuildFailureIsARunFailure pins a failed cross-build as exit 1, with the failing
// platform named and no archive written for it.
func TestDistBuildFailureIsARunFailure(t *testing.T) {
	t.Parallel()
	sourceDir := testSourceTree(t)
	toolchain := fakeToolchain(testBuildCount)
	commands := &fakeRunner{respond: func(call recordedCall) (string, error) {
		if call.name == "go" && slices.Contains(call.env, "GOOS=darwin") {
			return "", errors.New("compile error")
		}
		return toolchain.respond(call)
	}}
	outDir := filepath.Join(t.TempDir(), "out")

	_, err := execute(t, commands, "dist", "--src", sourceDir, "--out", outDir)

	if err == nil {
		t.Fatal("dist succeeded over a failing build")
	}
	if code := exitCodeFor(err); code != exitRunFailed {
		t.Errorf("exit code = %d, want %d", code, exitRunFailed)
	}
	if !strings.Contains(err.Error(), "apogee_1.2.3_darwin_amd64") {
		t.Errorf("error %q does not name the failing platform", err)
	}
	if listed := listDir(t, outDir); slices.ContainsFunc(listed, func(name string) bool { return strings.Contains(name, "darwin") }) {
		t.Errorf("output holds %v, want no darwin archive", listed)
	}
}
