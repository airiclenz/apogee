package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/update"
)

// TestParseReleaseVersionAcceptsTheTagShapeOnly pins what counts as a release version: vX.Y.Z,
// or a bare X.Y.Z read as vX.Y.Z, and nothing looser.
func TestParseReleaseVersionAcceptsTheTagShapeOnly(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		text    string
		want    string
		isValid bool
	}{
		"tagged":              {text: "v0.24.11", want: "v0.24.11", isValid: true},
		"bare":                {text: "0.24.11", want: "v0.24.11", isValid: true},
		"zeroes":              {text: "v0.0.0", want: "v0.0.0", isValid: true},
		"leading zero":        {text: "v0.024.11"},
		"pre-release":         {text: "v0.25.0-rc.1"},
		"build metadata":      {text: "v0.24.11+436.g28b6f838e6e1"},
		"two fields":          {text: "v0.24"},
		"surrounding space":   {text: " v0.24.11"},
		"capital v":           {text: "V0.24.11"},
		"empty":               {text: ""},
		"overflowing a field": {text: "v0.99999999999999999999.0"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			version, err := parseReleaseVersion(tc.text)

			if !tc.isValid {
				if err == nil {
					t.Fatalf("parseReleaseVersion(%q) = %s, want an error", tc.text, version)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseReleaseVersion(%q): %v", tc.text, err)
			}
			if got := version.String(); got != tc.want {
				t.Errorf("parseReleaseVersion(%q) = %s, want %s", tc.text, got, tc.want)
			}
		})
	}
}

// TestHighestReleaseTagOrdersNumericallyAndSetsAsideTheBurnedVersions pins the highest-tag
// lookup: fields compare as numbers (v0.10.0 outranks v0.9.0), the burned v1.x tags never win
// however high they read, an unburned v1.x tag does, and a tag that is not exactly vX.Y.Z is
// ignored.
func TestHighestReleaseTagOrdersNumericallyAndSetsAsideTheBurnedVersions(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		tags string
		want string
	}{
		"numeric order":    {tags: "v0.9.0\nv0.10.0\nv0.2.1\n", want: "v0.10.0"},
		"patch decides":    {tags: "v0.24.10\nv0.24.11\nv0.24.9\n", want: "v0.24.11"},
		"burned v1.8.0":    {tags: "v0.24.11\nv1.8.0\n", want: "v0.24.11"},
		"every burned tag": {tags: "v1.1.0\nv0.3.0\nv1.7.0\nv1.8.1\nv1.8.0\n", want: "v0.3.0"},
		"v1.0.0 wins":      {tags: "v0.24.11\nv1.0.0\nv1.8.0\nv1.8.1\n", want: "v1.0.0"},
		"an unburned v1.x": {tags: "v1.0.0\nv1.2.0\nv1.7.0\n", want: "v1.2.0"},
		"malformed tags":   {tags: "v0.25.0-rc.1\nvnext\nv0.24.11\n0.30.0\n", want: "v0.24.11"},
		"no release tag":   {tags: "v1.8.0\nvnext\n", want: ""},
		"no tags at all":   {tags: "", want: ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := highestReleaseTag(tc.tags); got != tc.want {
				t.Errorf("highestReleaseTag(%q) = %q, want %q", tc.tags, got, tc.want)
			}
		})
	}
}

// TestCheckNotRetractedRefusesTheBurnedVersionsOnly pins the refusal to the exact burned list:
// v1.1.0, v1.7.0, v1.8.0 and the v1.8.1 placeholder are refused, and every other number — v1.0.0
// and the unburned v1.x minors and patches between them included — is not.
func TestCheckNotRetractedRefusesTheBurnedVersionsOnly(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		version   string
		isRefused bool
	}{
		"the current 0.x line":       {version: "v0.32.0"},
		"a high 0.x":                 {version: "v0.99.99"},
		"the first stable release":   {version: "v1.0.0"},
		"an unburned minor":          {version: "v1.2.0"},
		"an unburned patch":          {version: "v1.7.1"},
		"just above the placeholder": {version: "v1.8.2"},
		"a later minor":              {version: "v1.9.0"},
		"a later major":              {version: "v2.0.0"},
		"burned v1.1.0":              {version: "v1.1.0", isRefused: true},
		"burned v1.7.0":              {version: "v1.7.0", isRefused: true},
		"burned v1.8.0":              {version: "v1.8.0", isRefused: true},
		"the v1.8.1 placeholder":     {version: "v1.8.1", isRefused: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			version, err := parseReleaseVersion(tc.version)
			if err != nil {
				t.Fatal(err)
			}

			err = checkNotRetracted(version)

			if isRefused := err != nil; isRefused != tc.isRefused {
				t.Errorf("checkNotRetracted(%s) = %v, want refused %t", tc.version, err, tc.isRefused)
			}
		})
	}
}

// TestCheckRetractKeptAcceptsTheBurnedListOnly pins which go.mod retractions prepare accepts: the
// burned list exactly, as a block, as one-line directives or a mix, in any order and with
// comments; a range, a missing or extra version, or a commented-out directive is refused.
func TestCheckRetractKeptAcceptsTheBurnedListOnly(t *testing.T) {
	t.Parallel()

	const header = "module example.com/apogee\n\ngo 1.26\n\n"
	cases := map[string]struct {
		goMod      string
		isRefused  bool
		wantInText string
	}{
		"the block": {goMod: header + "retract (\n\tv1.1.0\n\tv1.7.0\n\tv1.8.0\n\tv1.8.1\n)\n"},
		"one-line directives": {
			goMod: header + "retract v1.1.0\nretract v1.7.0 // rationale\nretract v1.8.0\nretract v1.8.1\n",
		},
		"a block and a line, out of order": {
			goMod: header + "retract v1.8.1\n\n// Why.\nretract (\n\tv1.8.0 // placeholder\n\tv1.1.0\n\n\tv1.7.0\n)\n",
		},
		"the old range": {
			goMod: header + "retract [v1.0.0, v1.8.0]\n", isRefused: true, wantInText: "[v1.0.0, v1.8.0]",
		},
		"a range inside the block": {
			goMod:     header + "retract (\n\t[v1.1.0, v1.1.0]\n\tv1.7.0\n\tv1.8.0\n\tv1.8.1\n)\n",
			isRefused: true, wantInText: "[v1.1.0, v1.1.0]",
		},
		"the placeholder missing": {
			goMod:     header + "retract (\n\tv1.1.0\n\tv1.7.0\n\tv1.8.0\n)\n",
			isRefused: true, wantInText: "retracts v1.1.0, v1.7.0, v1.8.0,",
		},
		"an extra version": {
			goMod:     header + "retract (\n\tv1.0.0\n\tv1.1.0\n\tv1.7.0\n\tv1.8.0\n\tv1.8.1\n)\n",
			isRefused: true, wantInText: "v1.0.0",
		},
		"commented out": {
			goMod:     header + "// retract (\n//\tv1.1.0\n//\tv1.7.0\n//\tv1.8.0\n//\tv1.8.1\n// )\n",
			isRefused: true, wantInText: "retracts nothing",
		},
		"no directive": {goMod: header, isRefused: true, wantInText: "retracts nothing"},
		"a word that only starts with retract": {
			goMod: header + "retracted v1.1.0\n", isRefused: true, wantInText: "retracts nothing",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := checkRetractKept([]byte(tc.goMod))

			if isRefused := err != nil; isRefused != tc.isRefused {
				t.Fatalf("checkRetractKept = %v, want refused %t", err, tc.isRefused)
			}
			if err != nil && (!strings.Contains(err.Error(), tc.wantInText) ||
				!strings.Contains(err.Error(), retractBlock())) {
				t.Errorf("checkRetractKept = %v, want it naming %q and the expected block", err, tc.wantInText)
			}
		})
	}
}

// TestRepoKeepsTheRetractLineAndAReleasableVersion is the drift guard on the live checkout: the
// repository's own go.mod carries one retract block listing exactly the burned versions
// internal/update owns, in its order, and VERSION does not read a burned number. CI runs it on
// every push, so a commit that changes the block or bumps onto a burned number turns the build red
// before any release is cut.
func TestRepoKeepsTheRetractLineAndAReleasableVersion(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")

	goMod, err := os.ReadFile(filepath.Join(root, goModFile))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := goModRetractions(goMod), update.RetractedVersions(); !slices.Equal(got, want) {
		t.Errorf("%s retracts %q, want exactly %q", goModFile, got, want)
	}
	if !strings.Contains(string(goMod), retractBlock()+"\n") {
		t.Errorf("%s does not carry the burned list as one block:\n%s", goModFile, retractBlock())
	}

	raw, err := os.ReadFile(filepath.Join(root, versionFile))
	if err != nil {
		t.Fatal(err)
	}
	version, err := parseReleaseVersion(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("%s: %v", versionFile, err)
	}
	if err := checkNotRetracted(version); err != nil {
		t.Errorf("%s: %v", versionFile, err)
	}
}

// TestRepoVersionBumpScriptBurnsTheSameList is the drift guard on CI's tagger: the `burned=` line
// of .github/scripts/version-bump.sh names exactly the burned versions internal/update owns, in its
// order. The script cannot import the list, and a refusal there is only an annotation, so without
// this a list that drifted would leave a releasable bump untagged, or tag a burned one, with CI green.
func TestRepoVersionBumpScriptBurnsTheSameList(t *testing.T) {
	t.Parallel()
	const scriptPath = "../../.github/scripts/version-bump.sh"

	script, err := os.ReadFile(filepath.FromSlash(scriptPath))
	if err != nil {
		t.Fatal(err)
	}
	burnedLine := regexp.MustCompile(`(?m)^burned='([^']*)'$`)
	matches := burnedLine.FindAllSubmatch(script, -1)
	if len(matches) != 1 {
		t.Fatalf("%s has %d `burned='…'` lines, want exactly one", scriptPath, len(matches))
	}

	if got, want := strings.Fields(string(matches[0][1])), update.RetractedVersions(); !slices.Equal(got, want) {
		t.Errorf("%s burns %q, want exactly %q", scriptPath, got, want)
	}
}
