package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"text/template"
)

// The package-channel files publish writes, rendered whole from the templates below at each
// release. The templates are the source of truth: the tap's formula and the bucket's manifest
// say they are generated here, so a fix to either lands in this repository, never in theirs.
const (
	formulaTemplate = "apogee.rb.tmpl"
	scoopTemplate   = "scoop.json.tmpl"
)

// releaseDownloadBase is the URL every release asset is downloaded from, before `<tag>/<asset>`.
const releaseDownloadBase = "https://github.com/" + apogeeRepo + "/releases/download/"

//go:embed templates/*.tmpl
var templateFiles embed.FS

// packageTemplates holds both package-channel templates, parsed once. A template that fails to
// parse is a defect of this program, caught by every render test, so it panics at start-up.
var packageTemplates = template.Must(template.ParseFS(templateFiles, "templates/*.tmpl"))

// checksumPattern is the one hash shape SHA256SUMS may list for an archive: 64 lower-case hex
// digits, after lower-casing.
var checksumPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// The two ways a SHA256SUMS body can fail to name an archive's hash.
var (
	errNoChecksum        = errors.New("SHA256SUMS lists no hash")
	errMalformedChecksum = errors.New("SHA256SUMS lists a malformed hash")
)

// packageArchive is one release archive as a package-channel file names it.
type packageArchive struct {
	// URL is the archive's download URL on the release.
	URL string
	// SHA256 is the archive's hash, as SHA256SUMS lists it.
	SHA256 string
	// Stem is the archive's name without its extension, and its one top-level directory.
	Stem string
}

// packageData is what a package-channel template renders from.
type packageData struct {
	// Version is the bare release version, X.Y.Z.
	Version string
	tag     string
	sums    string
}

// Archive resolves the archive a template names by its target. Templates call it for exactly the
// archives they install, so a SHA256SUMS that omits an archive the template does not use is no
// error.
func (d packageData) Archive(goos, goarch string) (packageArchive, error) {
	target := distTarget{goos: goos, goarch: goarch}
	if !isDistTarget(target) {
		return packageArchive{}, fmt.Errorf("%s/%s is not a release target", goos, goarch)
	}

	name := target.archiveName(d.Version)
	hash, err := checksumFor(d.sums, name)
	if err != nil {
		return packageArchive{}, err
	}
	return packageArchive{
		URL:    releaseDownloadBase + d.tag + "/" + name,
		SHA256: hash,
		Stem:   target.stem(d.Version),
	}, nil
}

// isDistTarget reports whether target is one of the platforms a release ships.
func isDistTarget(target distTarget) bool {
	for _, known := range distTargets {
		if known == target {
			return true
		}
	}
	return false
}

// checksumFor returns the hash the SHA256SUMS body sums lists for the archive name, lower-cased.
// Both the text (`hash  name`) and binary (`hash *name`) line forms are read; the first line
// naming the archive wins, as `sha256sum --check` would read it.
func checksumFor(sums, name string) (string, error) {
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || (fields[1] != name && fields[1] != "*"+name) {
			continue
		}
		hash := strings.ToLower(fields[0])
		if !checksumPattern.MatchString(hash) {
			return "", fmt.Errorf("%w for %s: %q", errMalformedChecksum, name, fields[0])
		}
		return hash, nil
	}
	return "", fmt.Errorf("%w for %s", errNoChecksum, name)
}

// renderFormula renders the whole Homebrew formula for version (vX.Y.Z or a bare X.Y.Z), reading
// the four macOS and Linux archive hashes from the release's SHA256SUMS body.
func renderFormula(version, sums string) (string, error) {
	return renderPackageFile(formulaTemplate, version, sums)
}

// renderScoopManifest renders the whole Scoop manifest for version (vX.Y.Z or a bare X.Y.Z),
// reading the two Windows archive hashes from the release's SHA256SUMS body. The result is
// checked to parse as JSON, so a template edit that breaks it fails here rather than in the
// bucket.
func renderScoopManifest(version, sums string) (string, error) {
	manifest, err := renderPackageFile(scoopTemplate, version, sums)
	if err != nil {
		return "", err
	}
	if !json.Valid([]byte(manifest)) {
		return "", fmt.Errorf("render %s: the manifest is not valid JSON", scoopTemplate)
	}
	return manifest, nil
}

// renderPackageFile executes the named template for version against the SHA256SUMS body sums.
func renderPackageFile(name, version, sums string) (string, error) {
	parsed, err := parseReleaseVersion(version)
	if err != nil {
		return "", err
	}

	tag := parsed.String()
	data := packageData{Version: strings.TrimPrefix(tag, "v"), tag: tag, sums: sums}
	var rendered bytes.Buffer
	if err := packageTemplates.ExecuteTemplate(&rendered, name, data); err != nil {
		return "", fmt.Errorf("render %s for %s: %w", name, tag, err)
	}
	return rendered.String(), nil
}
