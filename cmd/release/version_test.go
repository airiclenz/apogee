package main

import "testing"

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

// TestHighestReleaseTagOrdersNumericallyAndSetsAsideTheRetractedSeries pins the highest-tag
// lookup: fields compare as numbers (v0.10.0 outranks v0.9.0), the retracted v1.0.0–v1.8.0 tags
// never win however high they read, and a tag that is not exactly vX.Y.Z is ignored.
func TestHighestReleaseTagOrdersNumericallyAndSetsAsideTheRetractedSeries(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		tags string
		want string
	}{
		"numeric order":         {tags: "v0.9.0\nv0.10.0\nv0.2.1\n", want: "v0.10.0"},
		"patch decides":         {tags: "v0.24.10\nv0.24.11\nv0.24.9\n", want: "v0.24.11"},
		"retracted v1.8.0":      {tags: "v0.24.11\nv1.8.0\n", want: "v0.24.11"},
		"whole retracted range": {tags: "v1.0.0\nv0.3.0\nv1.4.2\nv1.8.0\n", want: "v0.3.0"},
		"malformed tags":        {tags: "v0.25.0-rc.1\nvnext\nv0.24.11\n0.30.0\n", want: "v0.24.11"},
		"no release tag":        {tags: "v1.8.0\nvnext\n", want: ""},
		"no tags at all":        {tags: "", want: ""},
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
