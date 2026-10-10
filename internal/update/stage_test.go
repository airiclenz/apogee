package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stageTestTag is the release every Stage case downloads.
const stageTestTag = "v0.25.0"

// cdnDirectory is where the stand-in server really serves assets: every release-download URL
// redirects there, as GitHub's redirect to its CDN does.
const cdnDirectory = "/cdn/"

// binaryContent stands in for the release binary's bytes.
var binaryContent = []byte("#!/bin/sh\necho apogee v0.25.0\n")

// archiveEntry is one file in an archive a test builds.
type archiveEntry struct {
	name      string
	content   []byte
	isSymlink bool
}

// stageTarget is one release target with the archive and entry names `make dist` gives it.
type stageTarget struct {
	goos        string
	goarch      string
	archiveName string
	entryName   string
	binaryFile  string
}

var (
	linuxTarget = stageTarget{
		goos:        "linux",
		goarch:      "arm64",
		archiveName: "apogee_0.25.0_linux_arm64.tar.gz",
		entryName:   "apogee_0.25.0_linux_arm64/apogee",
		binaryFile:  "apogee",
	}
	windowsTarget = stageTarget{
		goos:        "windows",
		goarch:      "amd64",
		archiveName: "apogee_0.25.0_windows_amd64.zip",
		entryName:   "apogee_0.25.0_windows_amd64/apogee.exe",
		binaryFile:  "apogee.exe",
	}
)

// buildArchive builds target's archive format — zip for windows, tar.gz otherwise — holding
// entries, each beside a LICENSE file as in a real release archive.
func buildArchive(t *testing.T, target stageTarget, entries ...archiveEntry) []byte {
	t.Helper()
	entries = append(entries, archiveEntry{name: strings.TrimSuffix(target.entryName, target.binaryFile) + "LICENSE", content: []byte("MIT")})
	if target.goos == windowsOS {
		return buildZip(t, entries)
	}
	return buildTarGz(t, entries)
}

// buildTarGz builds a gzip-compressed tar archive of entries.
func buildTarGz(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(compressed)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: 0o755, Size: int64(len(entry.content)), Typeflag: tar.TypeReg}
		if entry.isSymlink {
			header = &tar.Header{Name: entry.name, Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: "/bin/sh"}
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatalf("tar header %s: %v", entry.name, err)
		}
		if _, err := writer.Write(entry.content); err != nil {
			t.Fatalf("tar write %s: %v", entry.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buffer.Bytes()
}

// buildZip builds a zip archive of entries.
func buildZip(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		file, err := writer.Create(entry.name)
		if err != nil {
			t.Fatalf("zip create %s: %v", entry.name, err)
		}
		if _, err := file.Write(entry.content); err != nil {
			t.Fatalf("zip write %s: %v", entry.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buffer.Bytes()
}

// sumLine is one SHA256SUMS line for content published as name, in sha256sum's text format.
func sumLine(name string, content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]) + "  " + name + "\n"
}

// newAssetServer starts a stand-in for the GitHub repository whose release stageTestTag holds
// assets (name → bytes). A release-download URL redirects to the same name under cdnDirectory,
// which serves it; an asset absent from the map answers 404.
func newAssetServer(t *testing.T, assets map[string][]byte) *httptest.Server {
	t.Helper()
	releaseDirectory := releaseDownloadDirectory + stageTestTag + "/"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if name, isRelease := strings.CutPrefix(request.URL.Path, releaseDirectory); isRelease {
			http.Redirect(writer, request, cdnDirectory+name, http.StatusFound)
			return
		}
		content, isServed := assets[strings.TrimPrefix(request.URL.Path, cdnDirectory)]
		if !strings.HasPrefix(request.URL.Path, cdnDirectory) || !isServed {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write(content)
	}))
	t.Cleanup(server.Close)
	return server
}

// publishedRelease serves archive as target's asset together with a SHA256SUMS listing it.
func publishedRelease(t *testing.T, target stageTarget, archive []byte) *httptest.Server {
	t.Helper()
	return newAssetServer(t, map[string][]byte{
		target.archiveName: archive,
		checksumsAssetName: []byte(sumLine("apogee_0.25.0_darwin_arm64.tar.gz", []byte("other")) + sumLine(target.archiveName, archive)),
	})
}

// assertDirEmpty fails the test when dir holds any file.
func assertDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	if len(entries) != 0 {
		t.Errorf("dir holds %d file(s) after a failed Stage, want none (first: %s)", len(entries), entries[0].Name())
	}
}

func TestStage_PublishedArchive_ReturnsVerifiedBinary(t *testing.T) {
	t.Parallel()
	for _, target := range []stageTarget{linuxTarget, windowsTarget} {
		t.Run(target.goos, func(t *testing.T) {
			t.Parallel()
			archive := buildArchive(t, target, archiveEntry{name: target.entryName, content: binaryContent})
			server := publishedRelease(t, target, archive)
			dir := t.TempDir()

			path, err := Stage(context.Background(), NewClient(server.URL), stageTestTag, target.goos, target.goarch, dir)

			if err != nil {
				t.Fatalf("Stage: unexpected error %v", err)
			}
			if want := filepath.Join(dir, target.binaryFile); path != want {
				t.Errorf("Stage path = %q, want %q", path, want)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read staged binary: %v", err)
			}
			if !bytes.Equal(content, binaryContent) {
				t.Errorf("staged binary = %q, want %q", content, binaryContent)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("read dir: %v", err)
			}
			if len(entries) != 1 {
				t.Errorf("dir holds %d files, want only the staged binary", len(entries))
			}
		})
	}
}

func TestStage_PublishedArchive_StagesExecutableBinary(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == windowsOS {
		t.Skip("Windows has no POSIX execute bit")
	}
	archive := buildArchive(t, linuxTarget, archiveEntry{name: linuxTarget.entryName, content: binaryContent})
	server := publishedRelease(t, linuxTarget, archive)

	path, err := Stage(context.Background(), NewClient(server.URL), stageTestTag, linuxTarget.goos, linuxTarget.goarch, t.TempDir())

	if err != nil {
		t.Fatalf("Stage: unexpected error %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat staged binary: %v", err)
	}
	if mode := info.Mode().Perm(); mode != stagedFileMode {
		t.Errorf("staged binary mode = %v, want %v", mode, stagedFileMode)
	}
}

func TestStage_TamperedArchive_ReturnsChecksumErrorAndStagesNothing(t *testing.T) {
	t.Parallel()
	for _, target := range []stageTarget{linuxTarget, windowsTarget} {
		t.Run(target.goos, func(t *testing.T) {
			t.Parallel()
			published := buildArchive(t, target, archiveEntry{name: target.entryName, content: binaryContent})
			tampered := buildArchive(t, target, archiveEntry{name: target.entryName, content: []byte("malicious")})
			server := newAssetServer(t, map[string][]byte{
				target.archiveName: tampered,
				checksumsAssetName: []byte(sumLine(target.archiveName, published)),
			})
			dir := t.TempDir()

			_, err := Stage(context.Background(), NewClient(server.URL), stageTestTag, target.goos, target.goarch, dir)

			if !errors.Is(err, ErrChecksumMismatch) {
				t.Errorf("Stage error = %v, want ErrChecksumMismatch", err)
			}
			assertDirEmpty(t, dir)
		})
	}
}

func TestStage_MissingAsset_ReturnsErrorAndStagesNothing(t *testing.T) {
	t.Parallel()
	archive := buildArchive(t, linuxTarget, archiveEntry{name: linuxTarget.entryName, content: binaryContent})
	cases := []struct {
		name   string
		assets map[string][]byte
	}{
		{
			name:   "archive listed but not published",
			assets: map[string][]byte{checksumsAssetName: []byte(sumLine(linuxTarget.archiveName, archive))},
		},
		{
			name: "archive published but not listed",
			assets: map[string][]byte{
				linuxTarget.archiveName: archive,
				checksumsAssetName:      []byte(sumLine("apogee_0.25.0_linux_amd64.tar.gz", archive)),
			},
		},
		{
			name:   "no SHA256SUMS",
			assets: map[string][]byte{linuxTarget.archiveName: archive},
		},
		{
			name: "SHA256SUMS entry not a SHA-256",
			assets: map[string][]byte{
				linuxTarget.archiveName: archive,
				checksumsAssetName:      []byte("deadbeef  " + linuxTarget.archiveName + "\n"),
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			server := newAssetServer(t, testCase.assets)
			dir := t.TempDir()

			_, err := Stage(context.Background(), NewClient(server.URL), stageTestTag, linuxTarget.goos, linuxTarget.goarch, dir)

			if err == nil {
				t.Fatal("Stage: want an error, got nil")
			}
			assertDirEmpty(t, dir)
		})
	}
}

func TestStage_ArchiveWithoutExpectedEntry_ReturnsErrorAndStagesNothing(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		target stageTarget
		entry  archiveEntry
	}{
		{name: "tar.gz traversal entry", target: linuxTarget, entry: archiveEntry{name: "../apogee", content: binaryContent}},
		{name: "zip traversal entry", target: windowsTarget, entry: archiveEntry{name: "../apogee.exe", content: binaryContent}},
		{name: "tar.gz traversal inside the release directory", target: linuxTarget, entry: archiveEntry{name: "apogee_0.25.0_linux_arm64/../../apogee", content: binaryContent}},
		{name: "tar.gz binary at the archive root", target: linuxTarget, entry: archiveEntry{name: "apogee", content: binaryContent}},
		{name: "zip binary of another release", target: windowsTarget, entry: archiveEntry{name: "apogee_0.24.0_windows_amd64/apogee.exe", content: binaryContent}},
		{name: "tar.gz expected entry as a symlink", target: linuxTarget, entry: archiveEntry{name: linuxTarget.entryName, isSymlink: true}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			archive := buildArchive(t, testCase.target, testCase.entry)
			server := publishedRelease(t, testCase.target, archive)
			root := t.TempDir()
			dir := filepath.Join(root, "staging")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatalf("mkdir: %v", err)
			}

			_, err := Stage(context.Background(), NewClient(server.URL), stageTestTag, testCase.target.goos, testCase.target.goarch, dir)

			if err == nil {
				t.Fatal("Stage: want an error, got nil")
			}
			assertDirEmpty(t, dir)
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatalf("read %s: %v", root, err)
			}
			if len(entries) != 1 {
				t.Errorf("Stage wrote outside dir: %s holds %d entries, want only staging/", root, len(entries))
			}
		})
	}
}

func TestStage_InvalidArguments_ReturnsErrorWithoutRequest(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		client func(serverURL string) Client
		tag    string
		goos   string
		goarch string
	}{
		{name: "client not built with NewClient", client: func(string) Client { return Client{} }, tag: stageTestTag, goos: "linux", goarch: "arm64"},
		{name: "tag without v", client: NewClient, tag: "0.25.0", goos: "linux", goarch: "arm64"},
		{name: "tag with a path", client: NewClient, tag: "v0.25.0/../x", goos: "linux", goarch: "arm64"},
		{name: "goos with a separator", client: NewClient, tag: stageTestTag, goos: "../linux", goarch: "arm64"},
		{name: "empty goarch", client: NewClient, tag: stageTestTag, goos: "linux", goarch: ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			isRequested := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				select {
				case isRequested <- struct{}{}:
				default:
				}
			}))
			t.Cleanup(server.Close)
			dir := t.TempDir()

			_, err := Stage(context.Background(), testCase.client(server.URL), testCase.tag, testCase.goos, testCase.goarch, dir)

			if err == nil {
				t.Fatal("Stage: want an error, got nil")
			}
			if len(isRequested) != 0 {
				t.Error("Stage sent a request for invalid arguments")
			}
			assertDirEmpty(t, dir)
		})
	}
}
