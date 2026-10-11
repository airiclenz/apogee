package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// versionPattern is the one release-version shape apogee tags: `vMAJOR.MINOR.PATCH`, decimal
// numbers without leading zeros, the `v` optional on input. It matches internal/update's
// releaseTagPattern, the shape the update check accepts, so this program never names a release
// the shipped binary would read as malformed.
var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// retractedLow and retractedHigh bound the deleted v1.x series go.mod retracts
// (`retract [v1.0.0, v1.8.0]`). Those tags outrank every 0.x release numerically, so the highest
// tag must be found with them set aside. internal/update keeps the same bounds unexported.
var (
	retractedLow  = releaseVersion{major: 1, minor: 0, patch: 0}
	retractedHigh = releaseVersion{major: 1, minor: 8, patch: 0}
)

// releaseVersion is a parsed release version.
type releaseVersion struct {
	major int
	minor int
	patch int
}

// parseReleaseVersion parses `vX.Y.Z` or a bare `X.Y.Z`, which is read as `vX.Y.Z`. Anything else
// — a pre-release suffix, build metadata, a leading zero — is refused.
func parseReleaseVersion(text string) (releaseVersion, error) {
	fields := versionPattern.FindStringSubmatch(text)
	if fields == nil {
		return releaseVersion{}, fmt.Errorf("%q is not a release version (want vX.Y.Z)", text)
	}

	numbers := make([]int, 0, len(fields)-1)
	for _, field := range fields[1:] {
		number, err := strconv.Atoi(field)
		if err != nil {
			// Only an overflowing field reaches here; the pattern admits digits alone.
			return releaseVersion{}, fmt.Errorf("%q is not a release version: %w", text, err)
		}
		numbers = append(numbers, number)
	}
	return releaseVersion{major: numbers[0], minor: numbers[1], patch: numbers[2]}, nil
}

// String is the version as it is tagged: `vX.Y.Z`.
func (v releaseVersion) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.major, v.minor, v.patch)
}

// compare returns a negative number when v orders before other, zero when they are equal and a
// positive number when v orders after it, comparing major, minor and patch in that order.
func (v releaseVersion) compare(other releaseVersion) int {
	if v.major != other.major {
		return v.major - other.major
	}
	if v.minor != other.minor {
		return v.minor - other.minor
	}
	return v.patch - other.patch
}

// isRetracted reports whether v lies inside the retracted [v1.0.0, v1.8.0] series.
func (v releaseVersion) isRetracted() bool {
	return v.compare(retractedLow) >= 0 && v.compare(retractedHigh) <= 0
}

// highestReleaseTag returns the highest release tag in tags — one tag per line, as
// `git tag --list` prints them — setting aside the retracted v1.x series and every tag that is
// not exactly `vX.Y.Z`. It returns "" when no tag qualifies.
func highestReleaseTag(tags string) string {
	var highest releaseVersion
	isFound := false
	for _, tag := range strings.Fields(tags) {
		if !strings.HasPrefix(tag, "v") {
			continue
		}
		candidate, err := parseReleaseVersion(tag)
		if err != nil || candidate.isRetracted() {
			continue
		}
		if !isFound || candidate.compare(highest) > 0 {
			highest = candidate
			isFound = true
		}
	}
	if !isFound {
		return ""
	}
	return highest.String()
}
