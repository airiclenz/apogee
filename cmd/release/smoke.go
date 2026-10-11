package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

// The build settings `-buildvcs` stamps into every release binary: the commit it was built from
// and whether that tree was dirty. They — not the archive bytes — tie a published asset to the tag.
const (
	vcsRevisionSetting = "vcs.revision"
	vcsModifiedSetting = "vcs.modified"
)

// scoopArchitectures pairs each Scoop manifest architecture key with the Windows archive's
// GOARCH whose hash it carries.
var scoopArchitectures = []struct {
	key    string
	goarch string
}{
	{key: "64bit", goarch: "amd64"},
	{key: "arm64", goarch: "arm64"},
}

// smokeOptions is what the smoke verb was asked to check.
type smokeOptions struct {
	// dir is the checkout whose origin and tags the tag and build-stamp checks read.
	dir string
	// version is the release to check, as vX.Y.Z; empty reads the checkout's VERSION file.
	version string
	// distDir holds a local build of the release whose SHA256SUMS must equal the published one;
	// empty skips that check.
	distDir string
	// goos and goarch pick the host's archive, the one whose binary is run.
	goos   string
	goarch string
	// readBuildInfo reads the build information embedded in the binary at path.
	readBuildInfo func(path string) (*buildinfo.BuildInfo, error)
}

// defaultSmokeOptions checks the checkout at dir from this host, reading build stamps with
// debug/buildinfo — which reads a binary of any platform.
func defaultSmokeOptions(dir string) smokeOptions {
	return smokeOptions{dir: dir, goos: runtime.GOOS, goarch: runtime.GOARCH, readBuildInfo: buildinfo.ReadFile}
}

// smokeRun is one smoke run: what every check reads, and the tally the summary reports.
type smokeRun struct {
	commands runner
	output   io.Writer
	options  smokeOptions
	version  releaseVersion
	// workDir holds the downloads, outside the checkout.
	workDir string
	// sums is the release's SHA256SUMS once read; sumsErr is why it could not be.
	sums       string
	sumsErr    error
	isSumsRead bool
	// isDownloaded is set once all six archives downloaded and matched SHA256SUMS.
	isDownloaded bool

	passed  int
	failed  int
	skipped int
}

// newSmokeCommand builds the smoke verb.
func newSmokeCommand(commands runner) *cobra.Command {
	options := defaultSmokeOptions(".")

	cmd := &cobra.Command{
		Use:   "smoke [vX.Y.Z] [--dist-dir D]",
		Short: "Check a published release from the outside",
		Long: "smoke proves a published release is usable: the tag is on origin and annotated;\n" +
			"with --dist-dir, that build's SHA256SUMS equals the published one; the six archives\n" +
			"download and match SHA256SUMS; every binary's build stamp names the tagged commit; the\n" +
			"host's archive holds a binary reporting the version; where apogee is installed with\n" +
			"Homebrew, `brew upgrade apogee` moves it onto the version; and the Scoop bucket's\n" +
			"manifest names the version and both Windows archives' hashes. A check whose tool or\n" +
			"input is missing prints SKIP and never counts as a pass. The version defaults to\n" +
			"VERSION; a bare X.Y.Z reads as vX.Y.Z. It exits 1 when any check fails. publish runs\n" +
			"it as its last step.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return fmt.Errorf("smoke takes at most one version, got %d arguments", len(args))
			}
			if len(args) == 1 {
				version, err := parseReleaseVersion(args[0])
				if err != nil {
					return err
				}
				options.version = version.String()
			}
			return nil
		},
		RunE: runE(func(cmd *cobra.Command, _ []string) error {
			return runSmoke(cmd.Context(), commands, cmd.OutOrStdout(), options)
		}),
	}
	cmd.Flags().StringVar(&options.distDir, "dist-dir", "", "a local build of the release whose SHA256SUMS must equal the published one")
	return cmd
}

// runSmoke runs every check in order, prints the tally, and fails when any check failed.
func runSmoke(ctx context.Context, commands runner, output io.Writer, options smokeOptions) error {
	version, err := smokeVersion(options)
	if err != nil {
		return err
	}
	workDir, err := os.MkdirTemp("", "apogee-smoke-")
	if err != nil {
		return fmt.Errorf("create the download directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	run := &smokeRun{commands: commands, output: output, options: options, version: version, workDir: workDir}
	_, _ = fmt.Fprintf(output, "smoke: %s %s\n", apogeeRepo, version)
	run.checkTag(ctx)
	run.checkDistSums(ctx)
	run.checkAssets(ctx)
	run.checkBuildStamps(ctx)
	run.checkHostBinary(ctx)
	run.checkHomebrew(ctx)
	run.checkScoop(ctx)

	_, _ = fmt.Fprintf(output, "\nsmoke: %s: %d passed, %d failed, %d skipped\n", version, run.passed, run.failed, run.skipped)
	if run.failed > 0 {
		return fmt.Errorf("%d smoke check(s) failed for %s", run.failed, version)
	}
	return nil
}

// smokeVersion is the version smoke checks: the one asked for, or the checkout's VERSION.
func smokeVersion(options smokeOptions) (releaseVersion, error) {
	text := options.version
	if text == "" {
		version, err := readCheckoutVersion(options.dir)
		if err != nil {
			return releaseVersion{}, err
		}
		text = version
	}
	return parseReleaseVersion(text)
}

// step prints a check's heading.
func (r *smokeRun) step(format string, args ...any) {
	_, _ = fmt.Fprintf(r.output, "\n==> %s\n", fmt.Sprintf(format, args...))
}

// pass, fail and skip print one check result and count it; warn prints a note that counts as
// none of them.
func (r *smokeRun) pass(format string, args ...any) {
	r.passed++
	r.report("PASS", format, args...)
}

func (r *smokeRun) fail(format string, args ...any) {
	r.failed++
	r.report("FAIL", format, args...)
}

func (r *smokeRun) skip(format string, args ...any) {
	r.skipped++
	r.report("SKIP", format, args...)
}

func (r *smokeRun) warn(format string, args ...any) {
	r.report("warning", format, args...)
}

// report prints one indented result line.
func (r *smokeRun) report(label, format string, args ...any) {
	_, _ = fmt.Fprintf(r.output, "    %s: %s\n", label, fmt.Sprintf(format, args...))
}

// checkTag requires the tag on origin and annotated: a lightweight or missing tag fails, a probe
// that cannot run skips.
func (r *smokeRun) checkTag(ctx context.Context) {
	tag := r.version.String()
	r.step("the tag %s is remote and annotated", tag)
	listed, err := r.commands.run(ctx, r.options.dir, nonInteractiveEnv, "git", "ls-remote", "--tags", remoteName, "refs/tags/"+tag)
	if err != nil {
		r.skip("could not list %s's tags: %v", remoteName, err)
		return
	}
	if strings.TrimSpace(listed) == "" {
		r.fail("%s has no tag %s: the release was published without its tag", remoteName, tag)
		return
	}
	kind, err := r.commands.run(ctx, r.options.dir, nonInteractiveEnv, "gh", "api", "repos/"+apogeeRepo+"/git/ref/tags/"+tag, "--jq", ".object.type")
	if err != nil {
		r.skip("tag %s is on %s, but gh could not read its type: %v", tag, remoteName, err)
		return
	}
	switch kind = strings.TrimSpace(kind); kind {
	case gitObjectTag:
		r.pass("annotated tag %s on %s", tag, remoteName)
	case "commit":
		r.fail("%s on %s is a lightweight tag; a release tag must be annotated", tag, remoteName)
	default:
		r.fail("tag %s points at a %q object, not an annotated tag", tag, kind)
	}
}

// checkDistSums requires the local build's SHA256SUMS to list the same hash for every archive as
// the published one.
func (r *smokeRun) checkDistSums(ctx context.Context) {
	r.step("the local build's %s equals the published one", checksumsFile)
	if r.options.distDir == "" {
		r.skip("no --dist-dir: there is no local build to compare")
		return
	}
	published, err := r.publishedSums(ctx)
	if err != nil {
		r.skip("the release's %s did not download: %v", checksumsFile, err)
		return
	}
	local, err := os.ReadFile(filepath.Join(r.options.distDir, checksumsFile))
	if err != nil {
		r.fail("read the local build's %s: %v", checksumsFile, err)
		return
	}

	var differing []string
	for _, name := range r.archiveNames() {
		want, err := checksumFor(published, name)
		got, localErr := checksumFor(string(local), name)
		if err != nil || localErr != nil || got != want {
			differing = append(differing, name)
		}
	}
	if len(differing) > 0 {
		r.fail("%s in %s differs from the published one for %s", checksumsFile, r.options.distDir, strings.Join(differing, ", "))
		return
	}
	r.pass("%s in %s equals the published one", checksumsFile, r.options.distDir)
}

// checkAssets downloads each archive and requires its hash to equal the published SHA256SUMS.
func (r *smokeRun) checkAssets(ctx context.Context) {
	r.step("the published assets download and verify")
	sums, err := r.publishedSums(ctx)
	if err != nil {
		if isMissingTool(err) {
			r.skip("gh is not installed: %v", err)
			return
		}
		r.fail("no %s asset on release %s — is it published? %v", checksumsFile, r.version, err)
		return
	}

	isComplete := true
	for _, name := range r.archiveNames() {
		want, err := checksumFor(sums, name)
		if err != nil {
			r.fail("asset %s is missing from the release's %s: %v", name, checksumsFile, err)
			isComplete = false
			continue
		}
		if _, err := r.commands.run(ctx, r.options.dir, nonInteractiveEnv, "gh", "release", "download", r.version.String(),
			"--repo", apogeeRepo, "--pattern", name, "--dir", r.workDir); err != nil {
			r.fail("asset %s is listed but does not download: %v", name, err)
			isComplete = false
			continue
		}
		got, err := fileSHA256(filepath.Join(r.workDir, name))
		if err != nil {
			r.fail("asset %s: %v", name, err)
			isComplete = false
			continue
		}
		if got != want {
			r.fail("asset %s hashes to %s, not the %s the release's %s lists", name, got, want, checksumsFile)
			isComplete = false
		}
	}
	if isComplete {
		r.isDownloaded = true
		r.pass("%d assets downloaded, all checksums match", len(distTargets))
	}
}

// checkBuildStamps requires every downloaded binary's vcs.revision to be the tagged commit; a
// binary built from a modified tree is only warned about, since an untracked file flips the flag.
func (r *smokeRun) checkBuildStamps(ctx context.Context) {
	r.step("every published binary was built from the tagged commit")
	if !r.isDownloaded {
		r.skip("the assets did not all download and verify")
		return
	}
	want, err := revParse(ctx, r.commands, r.options.dir, r.version.String())
	if err != nil {
		r.skip("tag %s is not in the local clone — git fetch --tags: %v", r.version, err)
		return
	}

	stamped := 0
	for _, target := range distTargets {
		name := target.archiveName(bareRelease(r.version))
		binary, err := r.extractBinary(target, filepath.Join(r.workDir, "stamp", name))
		if err != nil {
			r.fail("asset %s: %v", name, err)
			continue
		}
		info, err := r.options.readBuildInfo(binary)
		if err != nil {
			r.fail("asset %s carries no build information: %v", name, err)
			continue
		}
		revision := buildSetting(info, vcsRevisionSetting)
		if revision == "" {
			r.fail("asset %s carries no %s — was it built with -buildvcs=false?", name, vcsRevisionSetting)
			continue
		}
		if revision != want {
			r.fail("asset %s was built from %s, not the tagged commit %s", name, revision, want)
			continue
		}
		if buildSetting(info, vcsModifiedSetting) == "true" {
			r.warn("asset %s was built from a modified tree", name)
		}
		stamped++
	}
	if stamped == len(distTargets) {
		r.pass("%d assets carry %s %s = the tag", stamped, vcsRevisionSetting, short(want))
	}
}

// checkHostBinary runs the binary from the host's own archive and requires it to report the
// version.
func (r *smokeRun) checkHostBinary(ctx context.Context) {
	r.step("the host's archive holds a binary reporting %s", r.version)
	host := distTarget{goos: r.options.goos, goarch: r.options.goarch}
	if !slices.Contains(distTargets, host) {
		r.skip("no release archive is built for this host (%s/%s)", host.goos, host.goarch)
		return
	}
	name := host.archiveName(bareRelease(r.version))
	if _, err := os.Stat(filepath.Join(r.workDir, name)); err != nil {
		r.skip("the host's archive %s did not download", name)
		return
	}
	binary, err := r.extractBinary(host, filepath.Join(r.workDir, "host"))
	if err != nil {
		r.fail("asset %s: %v", name, err)
		return
	}
	r.checkReportsVersion(ctx, binary, "the released binary")
}

// checkHomebrew upgrades a Homebrew-installed apogee and requires it to report the version; a
// host without Homebrew, or without apogee installed through it, skips.
func (r *smokeRun) checkHomebrew(ctx context.Context) {
	r.step("brew upgrade moves this machine onto %s", r.version)
	if _, err := r.commands.run(ctx, r.options.dir, nil, "brew", "list", "--formula", binaryName); err != nil {
		if isMissingTool(err) {
			r.skip("Homebrew is not installed on this host")
			return
		}
		r.skip("apogee is not installed through Homebrew here")
		return
	}
	for _, args := range [][]string{{"update"}, {"upgrade", binaryName}} {
		if _, err := r.commands.run(ctx, r.options.dir, nil, "brew", args...); err != nil {
			r.fail("brew %s: %v", strings.Join(args, " "), err)
			return
		}
	}
	prefix, err := r.commands.run(ctx, r.options.dir, nil, "brew", "--prefix")
	if err != nil {
		r.fail("brew --prefix: %v", err)
		return
	}
	r.checkReportsVersion(ctx, filepath.Join(strings.TrimSpace(prefix), "bin", binaryName), "brew's apogee")
}

// checkScoop requires the bucket's manifest to name the version and both Windows archives'
// published hashes. An unreadable manifest skips; a manifest at another version fails only when
// the version is the latest release, the only one the bucket can be expected to hold.
func (r *smokeRun) checkScoop(ctx context.Context) {
	bare := bareRelease(r.version)
	r.step("the Scoop bucket's manifest names %s and its %s hashes", r.version, checksumsFile)
	sums, err := r.publishedSums(ctx)
	if err != nil {
		r.skip("the release's %s did not download — nothing to check the bucket's hashes against", checksumsFile)
		return
	}
	raw, err := readRepoFile(ctx, r.commands, r.options.dir, bucketRepo, bucketPath)
	if err != nil {
		r.skip("the bucket's manifest %s in %s is unreadable: %v", bucketPath, bucketRepo, err)
		return
	}
	var manifest struct {
		Version      string `json:"version"`
		Architecture map[string]struct {
			Hash string `json:"hash"`
		} `json:"architecture"`
	}
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		r.fail("the bucket's manifest %s is not valid JSON: %v", bucketPath, err)
		return
	}

	if manifest.Version != bare {
		r.checkStaleBucket(ctx, manifest.Version)
		return
	}
	matched := 0
	for _, architecture := range scoopArchitectures {
		name := distTarget{goos: "windows", goarch: architecture.goarch}.archiveName(bare)
		published, err := checksumFor(sums, name)
		if err != nil {
			r.fail("the release's %s lists no hash for %s: %v", checksumsFile, name, err)
			continue
		}
		listed := strings.TrimPrefix(strings.ToLower(manifest.Architecture[architecture.key].Hash), "sha256:")
		if listed != published {
			r.fail("the bucket's %s hash is %q, not %s's %s", architecture.key, listed, name, published)
			continue
		}
		matched++
	}
	if matched == len(scoopArchitectures) {
		r.pass("bucket at %s, both Windows hashes match %s", bare, checksumsFile)
	}
}

// checkStaleBucket judges a bucket manifest at another version: a failure when the version is
// the latest release, a skip otherwise or when the latest release cannot be read.
func (r *smokeRun) checkStaleBucket(ctx context.Context, bucketVersion string) {
	latest, err := r.commands.run(ctx, r.options.dir, nonInteractiveEnv, "gh", "release", "view", "--repo", apogeeRepo, "--json", "tagName", "--jq", ".tagName")
	latest = strings.TrimSpace(latest)
	switch {
	case err != nil || latest == "":
		r.skip("the bucket is at %q, not %s, and the latest release could not be read: %v", bucketVersion, bareRelease(r.version), err)
	case latest == r.version.String():
		r.fail("the bucket is at %q, not the latest release %s — run publish %s", bucketVersion, bareRelease(r.version), r.version)
	default:
		r.skip("the bucket is at %q; %s is not the latest release (%s)", bucketVersion, r.version, latest)
	}
}

// checkReportsVersion runs binary --version and requires its output to name the version.
func (r *smokeRun) checkReportsVersion(ctx context.Context, binary, label string) {
	reported, err := r.commands.run(ctx, r.options.dir, nil, binary, "--version")
	if err != nil {
		r.fail("%s --version: %v", label, err)
		return
	}
	reported = strings.TrimSpace(reported)
	if !strings.Contains(reported, r.version.String()) {
		r.fail("%s reports %q, not %s", label, reported, r.version)
		return
	}
	r.pass("%s reports %q", label, reported)
}

// publishedSums downloads the release's SHA256SUMS to stdout once, so nothing lands in the
// checkout, and returns it — or why it could not.
func (r *smokeRun) publishedSums(ctx context.Context) (string, error) {
	if !r.isSumsRead {
		r.isSumsRead = true
		r.sums, r.sumsErr = r.commands.run(ctx, r.options.dir, nonInteractiveEnv, "gh", "release", "download", r.version.String(),
			"--repo", apogeeRepo, "--pattern", checksumsFile, "--output", "-")
	}
	return r.sums, r.sumsErr
}

// archiveNames are the six archives a release of the version carries.
func (r *smokeRun) archiveNames() []string {
	names := make([]string, 0, len(distTargets))
	for _, target := range distTargets {
		names = append(names, target.archiveName(bareRelease(r.version)))
	}
	return names
}

// extractBinary copies the target's binary out of its downloaded archive — the one entry
// <stem>/<binary> that `apogee update` and the package channels read — into destDir, executable,
// and returns its path.
func (r *smokeRun) extractBinary(target distTarget, destDir string) (string, error) {
	bare := bareRelease(r.version)
	archivePath := filepath.Join(r.workDir, target.archiveName(bare))
	entry := target.stem(bare) + "/" + target.binaryFile()
	if err := os.MkdirAll(destDir, directoryMode); err != nil {
		return "", fmt.Errorf("create %s: %w", destDir, err)
	}
	binary := filepath.Join(destDir, target.binaryFile())

	var err error
	if target.isWindows() {
		err = extractZipEntry(archivePath, entry, binary)
	} else {
		err = extractTarGzEntry(archivePath, entry, binary)
	}
	if err != nil {
		return "", fmt.Errorf("read %s from the archive: %w", entry, err)
	}
	return binary, nil
}

// errNoEntry is an archive lacking the entry asked for.
var errNoEntry = errors.New("the archive holds no such entry")

// extractZipEntry writes the zip archive's entry to destination.
func extractZipEntry(archivePath, entry, destination string) error {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = archive.Close() }()
	for _, file := range archive.File {
		if file.Name != entry {
			continue
		}
		contents, err := file.Open()
		if err != nil {
			return err
		}
		defer func() { _ = contents.Close() }()
		return writeExecutable(destination, contents)
	}
	return errNoEntry
}

// extractTarGzEntry writes the gzip-compressed tar archive's entry to destination.
func extractTarGzEntry(archivePath, entry, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	archive := tar.NewReader(compressed)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return errNoEntry
		}
		if err != nil {
			return err
		}
		if header.Name == entry && header.Typeflag == tar.TypeReg {
			return writeExecutable(destination, archive)
		}
	}
}

// writeExecutable writes contents to an executable file at destination.
func writeExecutable(destination string, contents io.Reader) (err error) {
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, binaryMode)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}()
	_, err = io.Copy(file, contents)
	return err
}

// buildSetting returns the value of the build setting key, "" when the binary carries none.
func buildSetting(info *buildinfo.BuildInfo, key string) string {
	for _, setting := range info.Settings {
		if setting.Key == key {
			return setting.Value
		}
	}
	return ""
}

// isMissingTool reports whether a command could not run because its program is not installed.
func isMissingTool(err error) bool {
	return errors.Is(err, exec.ErrNotFound)
}
