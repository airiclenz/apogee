package update

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/testtime"
)

// The rehearsal proves the placeholder retraction scheme with Go's own module resolution: it
// writes a file:// module proxy for a stand-in module whose tags mirror apogee's history, and asks
// `go list -m` what `@latest` resolves to and which versions are retracted. The go command reads
// retractions from the go.mod of the module's highest version, so the scheme holds only if the
// placeholder — the highest burned version, tagged on v1.0.0's commit — carries the live go.mod's
// retract block above v1.8.0's immutable range. Nothing here reaches the network: GOPROXY names
// only the local proxy, the checksum database is off and the toolchain is pinned to the local one.
const (
	// schemeModulePath is the stand-in module the local proxy serves.
	schemeModulePath = "example.com/apogee"
	// bridgeVersion stands in for the bridge release: a v0.x tag whose go.mod already retracts the
	// burned list. Any v0.x number works; it only has to sit below every v1.x tag.
	bridgeVersion = "v0.99.0"
	// firstMajorVersion is the release the scheme makes resolvable.
	firstMajorVersion = "v1.0.0"
	// rangeRetractedVersion is the burned version whose go.mod, immutable on proxy.golang.org,
	// retracts the whole range below.
	rangeRetractedVersion = "v1.8.0"
	// rangeRetractBlock is that go.mod's retract line, as the proxy serves it.
	rangeRetractBlock = "retract [v1.0.0, v1.8.0]\n"
	// fakeGoDirective is the stand-in module's go line: low enough for any local toolchain.
	fakeGoDirective = "go 1.21"
	// fakeReleaseTime is the commit time every .info file reports; resolution ignores it.
	fakeReleaseTime = "2026-11-05T00:00:00Z"
	// fakeMainSource is the stand-in module's cmd/apogee/main.go.
	fakeMainSource = "package main\n\nfunc main() {}\n"
	// goListBudget bounds one `go list -m` run against the local proxy.
	goListBudget = 2 * time.Minute
)

// fakeRelease is one tag the local proxy serves: its version and the go.mod at its commit.
type fakeRelease struct {
	version string
	goMod   string
}

// moduleListing is the part of `go list -m -json` output the rehearsal reads.
type moduleListing struct {
	Version   string
	Retracted []string
}

// schemeReleases holds the tags of the rehearsal: the bridge, the burned history below the
// placeholder, and the two release-day tags — v1.0.0 and the placeholder on v1.0.0's commit.
type schemeReleases struct {
	bridge         fakeRelease
	history        []fakeRelease
	firstMajor     fakeRelease
	placeholder    fakeRelease
	burnedVersions []string
}

func TestRetractionScheme_ProxyStates_LatestResolves(t *testing.T) {
	requireGoToolchain(t)
	t.Parallel()
	releases := newSchemeReleases(t)
	cases := []struct {
		name  string
		extra []fakeRelease
		want  string
	}{
		{name: "bridge only", want: bridgeVersion},
		{name: "v1.0.0 alone", extra: []fakeRelease{releases.firstMajor}, want: bridgeVersion},
		{name: "placeholder alone", extra: []fakeRelease{releases.placeholder}, want: bridgeVersion},
		{
			name:  "v1.0.0 and the placeholder",
			extra: []fakeRelease{releases.firstMajor, releases.placeholder},
			want:  firstMajorVersion,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			proxyURL := writeModuleProxy(t, releases.withBase(testCase.extra...))

			// No -retracted here: with it, @latest may resolve to a retracted version.
			got := goListModule(t, proxyURL, schemeModulePath+"@latest")

			if got.Version != testCase.want {
				t.Errorf("@latest = %s; want %s", got.Version, testCase.want)
			}
		})
	}
}

func TestRetractionScheme_BurnedVersions_CarryRetracted(t *testing.T) {
	requireGoToolchain(t)
	t.Parallel()
	releases := newSchemeReleases(t)
	proxyURL := writeModuleProxy(t, releases.withBase(releases.firstMajor, releases.placeholder))

	for _, burned := range releases.burnedVersions {
		t.Run(burned, func(t *testing.T) {
			t.Parallel()

			got := goListModule(t, proxyURL, "-retracted", schemeModulePath+"@"+burned)

			if len(got.Retracted) == 0 {
				t.Errorf("go list -m -json -retracted %s@%s reports no Retracted field; want it retracted",
					schemeModulePath, burned)
			}
		})
	}
}

// requireGoToolchain skips the test when no go command is on PATH: the rehearsal asks Go's own
// resolver, and a host without one has nothing to rehearse against.
func requireGoToolchain(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no 'go' toolchain on PATH; skipping the offline retraction rehearsal")
	}
}

// newSchemeReleases builds the rehearsal's tags from the burned list and the live go.mod: the
// bridge, v1.0.0 and the placeholder all carry the live retract block, the placeholder at
// v1.0.0's very commit; v1.8.0 carries its immutable range; the rest of the history carries none.
func newSchemeReleases(t *testing.T) schemeReleases {
	t.Helper()
	burned := RetractedVersions()
	placeholderVersion := burned[len(burned)-1]
	releaseCommit := fakeGoMod(readRetractBlock(t))

	history := make([]fakeRelease, 0, len(burned)-1)
	for _, version := range burned[:len(burned)-1] {
		block := ""
		if version == rangeRetractedVersion {
			block = rangeRetractBlock
		}
		history = append(history, fakeRelease{version: version, goMod: fakeGoMod(block)})
	}

	return schemeReleases{
		bridge:         fakeRelease{version: bridgeVersion, goMod: releaseCommit},
		history:        history,
		firstMajor:     fakeRelease{version: firstMajorVersion, goMod: releaseCommit},
		placeholder:    fakeRelease{version: placeholderVersion, goMod: releaseCommit},
		burnedVersions: burned,
	}
}

// withBase returns the bridge and the burned history followed by extra.
func (r schemeReleases) withBase(extra ...fakeRelease) []fakeRelease {
	all := make([]fakeRelease, 0, 1+len(r.history)+len(extra))
	all = append(all, r.bridge)
	all = append(all, r.history...)
	return append(all, extra...)
}

// fakeGoMod returns the stand-in module's go.mod carrying retractBlock (which may be empty).
func fakeGoMod(retractBlock string) string {
	goMod := "module " + schemeModulePath + "\n\n" + fakeGoDirective + "\n"
	if retractBlock == "" {
		return goMod
	}
	return goMod + "\n" + retractBlock
}

// readRetractBlock returns the repository go.mod's retract block verbatim, from its `retract`
// line through the closing parenthesis of a block form.
func readRetractBlock(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("read %s: %v", goModPath, err)
	}

	var block strings.Builder
	isInBlock := false
	for line := range strings.Lines(string(contents)) {
		trimmed := strings.TrimSpace(line)
		if !isInBlock && !strings.HasPrefix(trimmed, "retract") {
			continue
		}
		block.WriteString(line)
		if !isInBlock && !strings.HasSuffix(trimmed, "(") {
			return block.String()
		}
		if isInBlock && trimmed == ")" {
			return block.String()
		}
		isInBlock = true
	}
	t.Fatalf("%s carries no complete retract block", goModPath)
	return ""
}

// writeModuleProxy lays the releases out as a GOPROXY file tree under a temp directory —
// `<module>/@v/{list,vX.info,vX.mod,vX.zip}` — and returns its file:// URL.
func writeModuleProxy(t *testing.T, releases []fakeRelease) string {
	t.Helper()
	root := t.TempDir()
	versionDir := filepath.Join(root, filepath.FromSlash(schemeModulePath), "@v")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("create %s: %v", versionDir, err)
	}

	var list strings.Builder
	for _, release := range releases {
		list.WriteString(release.version + "\n")
		info := `{"Version":"` + release.version + `","Time":"` + fakeReleaseTime + `"}`
		writeProxyFile(t, versionDir, release.version+".info", []byte(info))
		writeProxyFile(t, versionDir, release.version+".mod", []byte(release.goMod))
		writeProxyFile(t, versionDir, release.version+".zip", moduleZip(t, release))
	}
	writeProxyFile(t, versionDir, "list", []byte(list.String()))

	return fileURL(root)
}

// writeProxyFile writes contents to name under dir.
func writeProxyFile(t *testing.T, dir, name string, contents []byte) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// moduleZip returns the release's module zip: its go.mod and cmd/apogee/main.go under the
// `<module>@<version>/` prefix the proxy protocol requires.
func moduleZip(t *testing.T, release fakeRelease) []byte {
	t.Helper()
	prefix := schemeModulePath + "@" + release.version + "/"
	files := []struct{ name, contents string }{
		{name: "go.mod", contents: release.goMod},
		{name: "cmd/apogee/main.go", contents: fakeMainSource},
	}

	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, file := range files {
		entry, err := archive.Create(prefix + file.name)
		if err != nil {
			t.Fatalf("zip %s: %v", file.name, err)
		}
		if _, err := entry.Write([]byte(file.contents)); err != nil {
			t.Fatalf("zip %s: %v", file.name, err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close zip for %s: %v", release.version, err)
	}
	return buffer.Bytes()
}

// fileURL returns the file:// URL of an absolute directory, with the leading slash a Windows
// drive path lacks.
func fileURL(dir string) string {
	path := filepath.ToSlash(dir)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return "file://" + path
}

// goListModule runs `go list -m -json <args>` against the local proxy alone and decodes the
// result. It runs in a temp directory, so apogee's go.mod is not the main module, with every
// setting that could send the query elsewhere pinned: GOENV=off ignores only the go env file, so
// the GOPRIVATE family is pinned empty too — a box with GOPRIVATE=* would otherwise go direct.
func goListModule(t *testing.T, proxyURL string, args ...string) moduleListing {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), testtime.Budget(goListBudget))
	defer cancel()

	gopath := t.TempDir()
	command := exec.CommandContext(ctx, "go", append([]string{"list", "-m", "-json"}, args...)...)
	command.Dir = t.TempDir()
	command.Env = offlineGoEnv(proxyURL, gopath)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go list -m -json %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}

	var listing moduleListing
	if err := json.Unmarshal(output, &listing); err != nil {
		t.Fatalf("decode go list output %q: %v", output, err)
	}
	return listing
}

// offlineGoEnv returns the inherited environment with every module setting pinned so the go
// command resolves against proxyURL alone, caches under gopath and never fetches a toolchain.
func offlineGoEnv(proxyURL, gopath string) []string {
	pins := map[string]string{
		"GOPROXY":     proxyURL,
		"GOSUMDB":     "off",
		"GOFLAGS":     "-modcacherw",
		"GOWORK":      "off",
		"GOTOOLCHAIN": "local",
		"GOENV":       "off",
		"GO111MODULE": "on",
		"GOPRIVATE":   "",
		"GONOPROXY":   "",
		"GONOSUMDB":   "",
		"GOINSECURE":  "",
		"GOPATH":      gopath,
		"GOMODCACHE":  filepath.Join(gopath, "pkg", "mod"),
	}

	env := make([]string, 0, len(os.Environ())+len(pins))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, isPinned := pins[name]; !isPinned {
			env = append(env, entry)
		}
	}
	for name, value := range pins {
		env = append(env, name+"="+value)
	}
	return env
}
