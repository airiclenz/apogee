package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ErrChecksumMismatch reports a downloaded archive whose SHA-256 differs from the one the
// release's SHA256SUMS lists for it: the bytes are not the published ones, so nothing is staged.
var ErrChecksumMismatch = errors.New("update: archive does not match its SHA256SUMS entry")

// downloadTimeout bounds one download end to end. A release archive is tens of megabytes, so this
// is far looser than the lookup's, yet it still ends a stalled transfer.
const downloadTimeout = 2 * time.Minute

// releaseDownloadDirectory is the path a release's assets live under:
// `<repo>/releases/download/<tag>/<asset>`.
const releaseDownloadDirectory = "/releases/download/"

// checksumsAssetName is the asset `make dist` writes beside the archives: one `<sha256>  <archive>`
// line per archive.
const checksumsAssetName = "SHA256SUMS"

// binaryName is the executable inside every release archive; Windows archives add windowsSuffix.
const binaryName = "apogee"

// windowsOS is the GOOS whose archives are zips holding an `.exe`; every other target ships a
// tar.gz.
const windowsOS = "windows"

// windowsSuffix is the executable extension a Windows binary carries.
const windowsSuffix = ".exe"

// Archive extensions, matching the `make dist` recipe.
const (
	zipExtension   = ".zip"
	tarGzExtension = ".tar.gz"
)

// stagingPattern names the temporary file the binary is written to before it is renamed into
// place, so a failed extraction never leaves a file under the final name.
const stagingPattern = ".apogee-staging-*"

// stagedFileMode is the permission the staged binary gets: executable by everyone, like the
// binary in the archive.
const stagedFileMode os.FileMode = 0o755

// Download size ceilings. They are generous multiples of the real sizes and exist only so a
// hostile or broken server cannot exhaust memory or disk.
const (
	maxChecksumsBytes = 64 << 10
	maxArchiveBytes   = 256 << 20
	maxBinaryBytes    = 512 << 20
)

// targetPattern is the shape of a GOOS or GOARCH value. It keeps the asset name — and so the URL
// path — free of separators and dots.
var targetPattern = regexp.MustCompile(`^[a-z0-9]+$`)

// Stage downloads the release archive for goos/goarch from release tag (`vX.Y.Z`), verifies it
// against the release's SHA256SUMS and extracts its one binary into dir, returning the binary's
// path: `<dir>/apogee`, or `<dir>/apogee.exe` for windows. dir must already exist; a file of that
// name in it is replaced.
//
// The archive is `apogee_<bare>_<goos>_<goarch>.tar.gz` (`.zip` for windows, <bare> being tag
// without its `v`) and the binary is its `apogee_<bare>_<goos>_<goarch>/apogee[.exe]` entry —
// the `make dist` layout. Both it and SHA256SUMS are fetched from `<base>/releases/download/<tag>/`
// over a client that follows redirects (assets are served from a CDN), honours
// HTTP_PROXY/HTTPS_PROXY/NO_PROXY and gives up after two minutes per download.
//
// Only that exact entry is read, and its name never reaches the file system, so an entry such as
// `../apogee` cannot write outside dir. Stage fails, leaving no file behind in dir, on: a malformed
// tag or target, a client not built with [NewClient], a failed or non-200 download, an archive
// SHA256SUMS does not list, a checksum mismatch ([ErrChecksumMismatch]), an archive without the
// expected entry as a regular file, an oversized download or binary, or a write error. Cancelling
// ctx aborts the downloads.
func Stage(ctx context.Context, client Client, tag, goos, goarch, dir string) (string, error) {
	if client.httpClient == nil {
		return "", errors.New("update: client not built with NewClient")
	}
	if !releaseTagPattern.MatchString(tag) {
		return "", fmt.Errorf("update: stage: %q is not a vX.Y.Z release tag", tag)
	}
	if !targetPattern.MatchString(goos) || !targetPattern.MatchString(goarch) {
		return "", fmt.Errorf("update: stage: %q/%q is not a release target", goos, goarch)
	}

	asset := newReleaseAsset(tag, goos, goarch)
	downloader := newDownloadClient()
	assetDirectoryURL := client.baseURL + releaseDownloadDirectory + tag + "/"

	checksums, err := download(ctx, downloader, assetDirectoryURL+checksumsAssetName, maxChecksumsBytes)
	if err != nil {
		return "", err
	}
	wantSum, err := checksumFor(checksums, asset.archiveName)
	if err != nil {
		return "", err
	}
	archive, err := download(ctx, downloader, assetDirectoryURL+asset.archiveName, maxArchiveBytes)
	if err != nil {
		return "", err
	}
	if gotSum := sha256.Sum256(archive); !bytes.Equal(gotSum[:], wantSum) {
		return "", fmt.Errorf("%w: %s", ErrChecksumMismatch, asset.archiveName)
	}

	return writeBinary(dir, asset, archive)
}

// releaseAsset names one target's archive and the binary entry inside it.
type releaseAsset struct {
	archiveName string
	entryName   string
	binaryFile  string
	isZip       bool
}

// newReleaseAsset derives the `make dist` names for tag on goos/goarch.
func newReleaseAsset(tag, goos, goarch string) releaseAsset {
	stem := binaryName + "_" + strings.TrimPrefix(tag, "v") + "_" + goos + "_" + goarch
	asset := releaseAsset{archiveName: stem + tarGzExtension, binaryFile: binaryName}
	if goos == windowsOS {
		asset = releaseAsset{archiveName: stem + zipExtension, binaryFile: binaryName + windowsSuffix, isZip: true}
	}
	asset.entryName = stem + "/" + asset.binaryFile
	return asset
}

// newDownloadClient returns the asset client: unlike the lookup's, it follows redirects, because
// GitHub answers an asset URL with a redirect to its CDN.
func newDownloadClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyFromEnvironment},
		Timeout:   downloadTimeout,
	}
}

// download GETs url and returns its body, failing on a non-200 answer or a body over limit bytes.
func download(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("update: build download request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("update: download %s: %w", url, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update: download %s: %s", url, response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("update: download %s: %w", url, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("update: download %s: larger than %d bytes", url, limit)
	}
	return body, nil
}

// checksumFor returns the SHA-256 SHA256SUMS lists for archiveName. Lines are
// `<hex>  <name>` (text mode) or `<hex> *<name>` (binary mode), as sha256sum and shasum write them.
func checksumFor(checksums []byte, archiveName string) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(checksums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != archiveName {
			continue
		}
		sum, err := hex.DecodeString(fields[0])
		if err != nil || len(sum) != sha256.Size {
			return nil, fmt.Errorf("update: SHA256SUMS entry for %s is not a SHA-256", archiveName)
		}
		return sum, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("update: read SHA256SUMS: %w", err)
	}
	return nil, fmt.Errorf("update: release asset %s is not listed in SHA256SUMS", archiveName)
}

// writeBinary extracts asset's binary entry from archive into dir. It writes a temporary file
// first and renames it into place only once the whole binary is written, made executable and
// fsynced, so every failure leaves dir as it found it.
func writeBinary(dir string, asset releaseAsset, archive []byte) (string, error) {
	staging, err := os.CreateTemp(dir, stagingPattern)
	if err != nil {
		return "", fmt.Errorf("update: stage binary: %w", err)
	}
	stagingPath := staging.Name()
	isStaged := false
	defer func() {
		if !isStaged {
			_ = staging.Close()
			_ = os.Remove(stagingPath)
		}
	}()

	if err := extractEntry(staging, asset, archive); err != nil {
		return "", err
	}
	if err := staging.Chmod(stagedFileMode); err != nil {
		return "", fmt.Errorf("update: stage binary: %w", err)
	}
	if err := staging.Sync(); err != nil {
		return "", fmt.Errorf("update: stage binary: %w", err)
	}
	if err := staging.Close(); err != nil {
		return "", fmt.Errorf("update: stage binary: %w", err)
	}

	binaryPath := filepath.Join(dir, asset.binaryFile)
	if err := os.Rename(stagingPath, binaryPath); err != nil {
		return "", fmt.Errorf("update: stage binary: %w", err)
	}
	isStaged = true
	return binaryPath, nil
}

// extractEntry copies asset's binary entry from archive to destination.
func extractEntry(destination io.Writer, asset releaseAsset, archive []byte) error {
	if asset.isZip {
		return extractZipEntry(destination, asset, archive)
	}
	return extractTarGzEntry(destination, asset, archive)
}

// extractZipEntry copies the zip archive's binary entry to destination.
func extractZipEntry(destination io.Writer, asset releaseAsset, archive []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return fmt.Errorf("update: open %s: %w", asset.archiveName, err)
	}
	for _, file := range reader.File {
		if file.Name != asset.entryName || !file.Mode().IsRegular() {
			continue
		}
		entry, err := file.Open()
		if err != nil {
			return fmt.Errorf("update: open %s in %s: %w", asset.entryName, asset.archiveName, err)
		}
		defer func() { _ = entry.Close() }()
		return copyBinary(destination, entry, asset)
	}
	return missingEntryError(asset)
}

// extractTarGzEntry copies the tar.gz archive's binary entry to destination.
func extractTarGzEntry(destination io.Writer, asset releaseAsset, archive []byte) error {
	decompressed, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("update: open %s: %w", asset.archiveName, err)
	}
	defer func() { _ = decompressed.Close() }()

	reader := tar.NewReader(decompressed)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return missingEntryError(asset)
		}
		if err != nil {
			return fmt.Errorf("update: read %s: %w", asset.archiveName, err)
		}
		if header.Name == asset.entryName && header.Typeflag == tar.TypeReg {
			return copyBinary(destination, reader, asset)
		}
	}
}

// copyBinary copies the binary entry from source, refusing one over maxBinaryBytes.
func copyBinary(destination io.Writer, source io.Reader, asset releaseAsset) error {
	written, err := io.Copy(destination, io.LimitReader(source, maxBinaryBytes+1))
	if err != nil {
		return fmt.Errorf("update: extract %s from %s: %w", asset.entryName, asset.archiveName, err)
	}
	if written > maxBinaryBytes {
		return fmt.Errorf("update: %s in %s is larger than %d bytes", asset.entryName, asset.archiveName, int64(maxBinaryBytes))
	}
	return nil
}

// missingEntryError reports an archive that holds no regular-file entry under the expected name.
func missingEntryError(asset releaseAsset) error {
	return fmt.Errorf("update: %s holds no file %s", asset.archiveName, asset.entryName)
}
