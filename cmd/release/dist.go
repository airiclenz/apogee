package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// The fixed names a release archive is built from. They mirror the Makefile's BINARY, PKG and
// MODULE, which `make dist` used before this verb replaced it; an archive name, a binary entry or
// an -X path that drifted from them would break `apogee update` and the package channels.
const (
	binaryName    = "apogee"
	mainPackage   = "./cmd/apogee"
	modulePath    = "github.com/airiclenz/apogee"
	versionFile   = "VERSION"
	checksumsFile = "SHA256SUMS"
	defaultOutDir = "dist"
	// distStamp is the value of apogee.distBuild in every archive's binary: the release marker
	// the update check reads, and the string a release check greps the binary for.
	distStamp = "release-archive"
)

// The archive entry modes: the binary is executable, everything else is plain read-only data.
const (
	binaryMode    = 0o755
	documentMode  = 0o644
	directoryMode = 0o755
)

// documentFiles are copied from the source tree into every archive beside the binary.
var documentFiles = []string{"LICENSE", "README.md"}

// distTarget is one platform a release ships a binary for.
type distTarget struct {
	goos   string
	goarch string
}

// distTargets are the six release platforms, in the Makefile's CROSS_TARGETS order.
var distTargets = []distTarget{
	{goos: "linux", goarch: "amd64"},
	{goos: "linux", goarch: "arm64"},
	{goos: "darwin", goarch: "amd64"},
	{goos: "darwin", goarch: "arm64"},
	{goos: "windows", goarch: "amd64"},
	{goos: "windows", goarch: "arm64"},
}

// isWindows reports whether the target ships a .exe in a .zip rather than a bare binary in a
// .tar.gz.
func (t distTarget) isWindows() bool {
	return t.goos == "windows"
}

// stem is the archive's name without its extension, and the one top-level directory inside it:
// apogee_<bare version>_<os>_<arch>. `apogee update` and the package channels look for exactly
// <stem>/<binary> inside the archive.
func (t distTarget) stem(bareVersion string) string {
	return fmt.Sprintf("%s_%s_%s_%s", binaryName, bareVersion, t.goos, t.goarch)
}

// binaryFile is the binary's file name for the target.
func (t distTarget) binaryFile() string {
	if t.isWindows() {
		return binaryName + ".exe"
	}
	return binaryName
}

// archiveName is the archive's file name for the target.
func (t distTarget) archiveName(bareVersion string) string {
	if t.isWindows() {
		return t.stem(bareVersion) + ".zip"
	}
	return t.stem(bareVersion) + ".tar.gz"
}

// distOptions is what the dist verb was asked to do.
type distOptions struct {
	sourceDir string
	outDir    string
}

// archiveEntry is one file packed into an archive: its slash-separated name inside the archive,
// the file it is read from, and the mode it carries.
type archiveEntry struct {
	name   string
	source string
	mode   fs.FileMode
}

// newDistCommand builds the dist verb.
func newDistCommand(commands runner) *cobra.Command {
	var options distOptions

	cmd := &cobra.Command{
		Use:   "dist",
		Short: "Build the six release archives and SHA256SUMS",
		Long: "dist cross-builds apogee for linux, darwin and windows on amd64 and arm64, packs\n" +
			"each binary with LICENSE and README.md into apogee_<version>_<os>_<arch>.tar.gz\n" +
			"(.zip on windows), and writes SHA256SUMS beside them. The archives are byte-identical\n" +
			"across runs over the same commit. --out is emptied first.",
		Args: cobra.NoArgs,
		RunE: runE(func(cmd *cobra.Command, _ []string) error {
			return runDist(cmd.Context(), commands, cmd.OutOrStdout(), options)
		}),
	}
	cmd.Flags().StringVar(&options.sourceDir, "src", ".", "the apogee source tree to build")
	cmd.Flags().StringVar(&options.outDir, "out", "", "where to write the archives (default <src>/dist; emptied first)")
	return cmd
}

// runDist builds every target, packs its archive into the output directory and writes the
// checksums over them.
func runDist(ctx context.Context, commands runner, progress io.Writer, options distOptions) error {
	sourceDir, err := filepath.Abs(options.sourceDir)
	if err != nil {
		return fmt.Errorf("resolve --src: %w", err)
	}
	outDir := options.outDir
	if outDir == "" {
		outDir = filepath.Join(sourceDir, defaultOutDir)
	}
	outDir, err = filepath.Abs(outDir)
	if err != nil {
		return fmt.Errorf("resolve --out: %w", err)
	}
	if isWithin(sourceDir, outDir) {
		return fmt.Errorf("--out %s holds the source tree %s and would be emptied", outDir, sourceDir)
	}

	bareVersion, err := readBareVersion(sourceDir)
	if err != nil {
		return err
	}
	ldflags, err := releaseLdflags(ctx, commands, sourceDir)
	if err != nil {
		return err
	}
	commitTime, err := headCommitTime(ctx, commands, sourceDir)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(outDir); err != nil {
		return fmt.Errorf("empty %s: %w", outDir, err)
	}
	if err := os.MkdirAll(outDir, directoryMode); err != nil {
		return fmt.Errorf("create %s: %w", outDir, err)
	}
	stageDir, err := os.MkdirTemp("", "apogee-dist-")
	if err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stageDir) }()

	archiveNames := make([]string, 0, len(distTargets))
	for _, target := range distTargets {
		_, _ = fmt.Fprintf(progress, "  -> %s\n", target.stem(bareVersion))
		if err := buildArchive(ctx, commands, target, bareVersion, ldflags, commitTime, sourceDir, stageDir, outDir); err != nil {
			return err
		}
		archiveNames = append(archiveNames, target.archiveName(bareVersion))
	}
	if err := writeChecksums(outDir, archiveNames); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(progress, "dist OK -> %s (%d archives + %s)\n", outDir, len(archiveNames), checksumsFile)
	return nil
}

// isWithin reports whether path is directory or lies inside it.
func isWithin(path, directory string) bool {
	relative, err := filepath.Rel(directory, path)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

// readBareVersion reads the source tree's VERSION and returns it without its leading "v": an
// archive name reads apogee_0.11.0_darwin_arm64, never apogee_v0.11.0_darwin_arm64.
func readBareVersion(sourceDir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(sourceDir, versionFile))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", versionFile, err)
	}
	bareVersion := strings.TrimPrefix(strings.TrimSpace(string(raw)), "v")
	if bareVersion == "" {
		return "", fmt.Errorf("could not read a version from %s", versionFile)
	}
	return bareVersion, nil
}

// releaseLdflags is the -ldflags value every release binary is built with: the build number (the
// commit count, omitted when git reports none) and the release stamp.
func releaseLdflags(ctx context.Context, commands runner, sourceDir string) (string, error) {
	output, err := commands.run(ctx, sourceDir, nil, "git", "rev-list", "--count", "HEAD")
	if err != nil {
		return "", fmt.Errorf("count commits: %w", err)
	}
	stamp := "-X " + modulePath + ".distBuild=" + distStamp
	buildCount := strings.TrimSpace(output)
	if buildCount == "" {
		return stamp, nil
	}
	return "-X " + modulePath + ".buildCount=" + buildCount + " " + stamp, nil
}

// headCommitTime is the time every archive entry carries: the HEAD commit's, so the same commit
// always packs to the same bytes.
func headCommitTime(ctx context.Context, commands runner, sourceDir string) (time.Time, error) {
	output, err := commands.run(ctx, sourceDir, nil, "git", "log", "-1", "--format=%ct", "HEAD")
	if err != nil {
		return time.Time{}, fmt.Errorf("read the HEAD commit time: %w", err)
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(output), 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("read the HEAD commit time: %q is not a Unix time", strings.TrimSpace(output))
	}
	return time.Unix(seconds, 0).UTC(), nil
}

// buildArchive cross-builds target's binary into the staging directory and packs it, with the
// documents, into its archive in outDir.
func buildArchive(ctx context.Context, commands runner, target distTarget, bareVersion, ldflags string, commitTime time.Time, sourceDir, stageDir, outDir string) error {
	stem := target.stem(bareVersion)
	binaryDir := filepath.Join(stageDir, stem)
	if err := os.MkdirAll(binaryDir, directoryMode); err != nil {
		return fmt.Errorf("stage %s: %w", stem, err)
	}
	binaryPath := filepath.Join(binaryDir, target.binaryFile())
	env := []string{"CGO_ENABLED=0", "GOOS=" + target.goos, "GOARCH=" + target.goarch}
	if _, err := commands.run(ctx, sourceDir, env, "go", "build", "-trimpath", "-ldflags", ldflags, "-o", binaryPath, mainPackage); err != nil {
		return fmt.Errorf("build %s: %w", stem, err)
	}

	entries := make([]archiveEntry, 0, len(documentFiles)+1)
	entries = append(entries, archiveEntry{name: path.Join(stem, target.binaryFile()), source: binaryPath, mode: binaryMode})
	for _, document := range documentFiles {
		entries = append(entries, archiveEntry{name: path.Join(stem, document), source: filepath.Join(sourceDir, document), mode: documentMode})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })

	archivePath := filepath.Join(outDir, target.archiveName(bareVersion))
	if target.isWindows() {
		return writeArchive(archivePath, func(file io.Writer) error { return packZip(file, stem, entries, commitTime) })
	}
	return writeArchive(archivePath, func(file io.Writer) error { return packTarGz(file, stem, entries, commitTime) })
}

// writeArchive creates archivePath and fills it with pack, removing it again if anything fails.
func writeArchive(archivePath string, pack func(io.Writer) error) (err error) {
	file, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("create %s: %w", archivePath, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("write %s: %w", archivePath, closeErr)
		}
		if err != nil {
			_ = os.Remove(archivePath)
		}
	}()
	if err := pack(file); err != nil {
		return fmt.Errorf("pack %s: %w", filepath.Base(archivePath), err)
	}
	return nil
}

// packTarGz writes a gzip-compressed tar of the stem directory and entries. Owner, group and
// times are fixed and the gzip header carries no name or time, so the bytes depend only on the
// entries' contents.
func packTarGz(destination io.Writer, stem string, entries []archiveEntry, commitTime time.Time) error {
	compressed, err := gzip.NewWriterLevel(destination, gzip.BestCompression)
	if err != nil {
		return err
	}
	archive := tar.NewWriter(compressed)

	directory := &tar.Header{Name: stem + "/", Typeflag: tar.TypeDir, Mode: directoryMode, ModTime: commitTime}
	if err := archive.WriteHeader(directory); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := addTarEntry(archive, entry, commitTime); err != nil {
			return err
		}
	}
	if err := archive.Close(); err != nil {
		return err
	}
	return compressed.Close()
}

// addTarEntry writes one regular-file entry.
func addTarEntry(archive *tar.Writer, entry archiveEntry, commitTime time.Time) error {
	source, err := os.Open(entry.source)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	header := &tar.Header{
		Name:     entry.name,
		Typeflag: tar.TypeReg,
		Mode:     int64(entry.mode),
		Size:     info.Size(),
		ModTime:  commitTime,
	}
	if err := archive.WriteHeader(header); err != nil {
		return err
	}
	if _, err := io.Copy(archive, source); err != nil {
		return fmt.Errorf("add %s: %w", entry.name, err)
	}
	return nil
}

// packZip writes a deflated zip of the stem directory and entries, every entry carrying the same
// fixed time and a Unix mode.
func packZip(destination io.Writer, stem string, entries []archiveEntry, commitTime time.Time) error {
	archive := zip.NewWriter(destination)

	directory := &zip.FileHeader{Name: stem + "/", Method: zip.Store, Modified: commitTime}
	directory.SetMode(fs.ModeDir | directoryMode)
	if _, err := archive.CreateHeader(directory); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := addZipEntry(archive, entry, commitTime); err != nil {
			return err
		}
	}
	return archive.Close()
}

// addZipEntry writes one regular-file entry.
func addZipEntry(archive *zip.Writer, entry archiveEntry, commitTime time.Time) error {
	source, err := os.Open(entry.source)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()

	header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate, Modified: commitTime}
	header.SetMode(entry.mode)
	destination, err := archive.CreateHeader(header)
	if err != nil {
		return err
	}
	if _, err := io.Copy(destination, source); err != nil {
		return fmt.Errorf("add %s: %w", entry.name, err)
	}
	return nil
}

// writeChecksums writes SHA256SUMS over the archives in outDir: one `<hex>  <name>` line per
// archive, sorted by name — the text-mode format sha256sum writes and `apogee update` reads.
func writeChecksums(outDir string, archiveNames []string) error {
	sorted := append([]string(nil), archiveNames...)
	sort.Strings(sorted)

	var lines strings.Builder
	for _, name := range sorted {
		sum, err := fileSHA256(filepath.Join(outDir, name))
		if err != nil {
			return err
		}
		fmt.Fprintf(&lines, "%s  %s\n", sum, name)
	}
	if err := os.WriteFile(filepath.Join(outDir, checksumsFile), []byte(lines.String()), documentMode); err != nil {
		return fmt.Errorf("write %s: %w", checksumsFile, err)
	}
	return nil
}

// fileSHA256 returns the hex SHA-256 of the file at filePath.
func fileSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("hash %s: %w", filePath, err)
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", filePath, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
