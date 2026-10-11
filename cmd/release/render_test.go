package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The hashes the published v0.24.11 release lists for its four macOS and Linux archives, which the
// live tap formula pins.
const (
	liveDarwinArm64Hash = "f4a3678e3be3e80e7499f95f28791d1981796e545fa36b66997202800bf7b965"
	liveDarwinAmd64Hash = "9b6b43667a030023354afe4ebb26d3ea1c60603af674fbff21e084a6a738cc82"
	liveLinuxArm64Hash  = "2aceca17269c94686ae6851feb467630f4f5f952d145aaee30e3debd9abfb3d1"
	liveLinuxAmd64Hash  = "832be0f87c23d4f1de97722d822bd424af4f71ddac46df2b03f47e6da0a8d7e3"
)

// The fixture hashes the Scoop tests read, one per Windows archive plus one for an archive the
// manifest does not install.
const (
	scoopAmd64Hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	scoopArm64Hash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	scoopLinuxHash = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

// renderedScoopArchitecture is one entry of a rendered Scoop manifest's `architecture` block.
type renderedScoopArchitecture struct {
	URL        string `json:"url"`
	Hash       string `json:"hash"`
	ExtractDir string `json:"extract_dir"`
}

// renderedScoopManifest is the part of a rendered bucket/apogee.json the tests assert.
type renderedScoopManifest struct {
	GeneratedNote string                               `json:"##"`
	Version       string                               `json:"version"`
	Description   string                               `json:"description"`
	Homepage      string                               `json:"homepage"`
	License       string                               `json:"license"`
	Architecture  map[string]renderedScoopArchitecture `json:"architecture"`
	Bin           string                               `json:"bin"`
	Checkver      string                               `json:"checkver"`
	Autoupdate    struct {
		Architecture map[string]json.RawMessage `json:"architecture"`
	} `json:"autoupdate"`
}

// TestRenderFormulaReproducesTheLiveFormula renders v0.24.11 from its published hashes and expects
// the live tap formula byte for byte, plus the generated note and the reworded `make dist` line.
func TestRenderFormulaReproducesTheLiveFormula(t *testing.T) {
	t.Parallel()

	sums := liveLinuxAmd64Hash + "  apogee_0.24.11_linux_amd64.tar.gz\n" +
		liveLinuxArm64Hash + "  apogee_0.24.11_linux_arm64.tar.gz\n" +
		liveDarwinAmd64Hash + "  apogee_0.24.11_darwin_amd64.tar.gz\n" +
		liveDarwinArm64Hash + "  apogee_0.24.11_darwin_arm64.tar.gz\n"
	want, err := os.ReadFile(filepath.Join("testdata", "apogee-0.24.11.rb"))
	if err != nil {
		t.Fatalf("read the expected formula: %v", err)
	}

	for _, version := range []string{"v0.24.11", "0.24.11"} {
		got, err := renderFormula(version, sums)
		if err != nil {
			t.Fatalf("renderFormula(%q) = %v", version, err)
		}
		if got != string(want) {
			t.Errorf("renderFormula(%q) differs from testdata/apogee-0.24.11.rb:\n%s", version, got)
		}
	}
}

// TestRenderScoopManifest carries every assertion the retired Scoop script's dry-run test made
// to the rendered manifest: version, homepage, description, license, bin, checkver, both Windows
// architectures and their autoupdate entries.
func TestRenderScoopManifest(t *testing.T) {
	t.Parallel()

	const (
		bare        = "9.8.7"
		releaseBase = "https://github.com/airiclenz/apogee/releases/download/v" + bare + "/"
	)
	sums := scoopLinuxHash + "  apogee_" + bare + "_linux_amd64.tar.gz\n" +
		scoopAmd64Hash + "  apogee_" + bare + "_windows_amd64.zip\n" +
		scoopArm64Hash + "  apogee_" + bare + "_windows_arm64.zip\n"

	rendered, err := renderScoopManifest("v"+bare, sums)
	if err != nil {
		t.Fatalf("renderScoopManifest = %v", err)
	}

	var manifest renderedScoopManifest
	if err := json.Unmarshal([]byte(rendered), &manifest); err != nil {
		t.Fatalf("the rendered manifest is not JSON: %v\n%s", err, rendered)
	}
	if !strings.Contains(manifest.GeneratedNote, "cmd/release") || !strings.Contains(manifest.GeneratedNote, "airiclenz/apogee") {
		t.Errorf(`"##" = %q; want the generated-by-cmd/release note`, manifest.GeneratedNote)
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
	wants := map[string]renderedScoopArchitecture{
		"64bit": {
			URL:        releaseBase + "apogee_" + bare + "_windows_amd64.zip",
			Hash:       scoopAmd64Hash,
			ExtractDir: "apogee_" + bare + "_windows_amd64",
		},
		"arm64": {
			URL:        releaseBase + "apogee_" + bare + "_windows_arm64.zip",
			Hash:       scoopArm64Hash,
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
	if !strings.Contains(rendered, `"url": "$baseurl/SHA256SUMS"`) {
		t.Errorf("autoupdate does not hash from $baseurl/SHA256SUMS:\n%s", rendered)
	}
}

// TestChecksumForReadsBothLineForms reads a hash from the text and the binary SHA256SUMS line
// forms, and lower-cases an upper-case one.
func TestChecksumForReadsBothLineForms(t *testing.T) {
	t.Parallel()

	const name = "apogee_1.2.3_windows_amd64.zip"
	tests := map[string]string{
		"text form":          scoopAmd64Hash + "  " + name + "\n",
		"binary form":        scoopAmd64Hash + " *" + name + "\n",
		"upper case":         strings.ToUpper(scoopAmd64Hash) + "  " + name + "\n",
		"crlf line endings":  scoopLinuxHash + "  other.zip\r\n" + scoopAmd64Hash + "  " + name + "\r\n",
		"first match wins":   scoopAmd64Hash + "  " + name + "\n" + scoopArm64Hash + "  " + name + "\n",
		"suffix is no match": scoopArm64Hash + "  x" + name + "\n" + scoopAmd64Hash + "  " + name + "\n",
	}
	for label, sums := range tests {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got, err := checksumFor(sums, name)
			if err != nil {
				t.Fatalf("checksumFor = %v", err)
			}
			if got != scoopAmd64Hash {
				t.Errorf("checksumFor = %q; want %q", got, scoopAmd64Hash)
			}
		})
	}
}

// TestRenderRefusesABadOrMissingHash refuses to render either file when SHA256SUMS omits an
// archive the file installs or lists a hash that is not 64 hex digits.
func TestRenderRefusesABadOrMissingHash(t *testing.T) {
	t.Parallel()

	const (
		bare         = "1.2.3"
		darwinArm64  = "apogee_1.2.3_darwin_arm64.tar.gz"
		windowsAmd64 = "apogee_1.2.3_windows_amd64.zip"
		windowsArm64 = "apogee_1.2.3_windows_arm64.zip"
	)
	// otherFormulaSums lists the three formula archives every formula case leaves intact.
	otherFormulaSums := liveDarwinAmd64Hash + "  apogee_1.2.3_darwin_amd64.tar.gz\n" +
		liveLinuxArm64Hash + "  apogee_1.2.3_linux_arm64.tar.gz\n" +
		liveLinuxAmd64Hash + "  apogee_1.2.3_linux_amd64.tar.gz\n"
	amd64Sums := scoopAmd64Hash + "  " + windowsAmd64 + "\n"
	render := map[string]func(string, string) (string, error){
		"formula": renderFormula,
		"scoop":   renderScoopManifest,
	}

	tests := map[string]struct {
		file    string
		sums    string
		wantErr error
		archive string
	}{
		"formula: archive missing": {file: "formula", sums: otherFormulaSums, wantErr: errNoChecksum, archive: darwinArm64},
		"formula: short hash":      {file: "formula", sums: liveDarwinArm64Hash[1:] + "  " + darwinArm64 + "\n" + otherFormulaSums, wantErr: errMalformedChecksum, archive: darwinArm64},
		"formula: not hex":         {file: "formula", sums: "z" + liveDarwinArm64Hash[1:] + "  " + darwinArm64 + "\n" + otherFormulaSums, wantErr: errMalformedChecksum, archive: darwinArm64},
		"scoop: archive missing":   {file: "scoop", sums: amd64Sums, wantErr: errNoChecksum, archive: windowsArm64},
		"scoop: long hash":         {file: "scoop", sums: amd64Sums + scoopArm64Hash + "b  " + windowsArm64 + "\n", wantErr: errMalformedChecksum, archive: windowsArm64},
		"scoop: empty sums":        {file: "scoop", sums: "", wantErr: errNoChecksum, archive: windowsAmd64},
	}
	for label, tc := range tests {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got, err := render[tc.file]("v"+bare, tc.sums)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("render = %q, %v; want %v", got, err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.archive) {
				t.Errorf("error %q does not name %s", err, tc.archive)
			}
		})
	}
}

// TestRenderRefusesABadVersion refuses a version that is not vX.Y.Z or a bare X.Y.Z before it
// reads a single hash.
func TestRenderRefusesABadVersion(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"", "v1.2", "1.2.3-rc1", "v01.2.3", "latest"} {
		if _, err := renderFormula(version, ""); err == nil || !strings.Contains(err.Error(), "not a release version") {
			t.Errorf("renderFormula(%q) = %v; want a not-a-release-version error", version, err)
		}
		if _, err := renderScoopManifest(version, ""); err == nil || !strings.Contains(err.Error(), "not a release version") {
			t.Errorf("renderScoopManifest(%q) = %v; want a not-a-release-version error", version, err)
		}
	}
}

// TestPackageDataRefusesAnUnknownTarget refuses an archive for a platform no release ships, so a
// template typo fails the render rather than pinning a URL that does not exist.
func TestPackageDataRefusesAnUnknownTarget(t *testing.T) {
	t.Parallel()

	data := packageData{Version: "1.2.3", tag: "v1.2.3", sums: scoopAmd64Hash + "  apogee_1.2.3_freebsd_amd64.tar.gz\n"}
	if _, err := data.Archive("freebsd", "amd64"); err == nil || !strings.Contains(err.Error(), "not a release target") {
		t.Errorf("Archive(freebsd, amd64) = %v; want a not-a-release-target error", err)
	}
}
