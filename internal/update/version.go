package update

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// releaseTagPattern is the one release-tag shape apogee publishes: `vMAJOR.MINOR.PATCH`, decimal
// numbers without leading zeros. Pre-release suffixes are not part of it — /releases/latest never
// names a pre-release, and a tag that does not match is treated as malformed, never as newer.
var releaseTagPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// buildMetadataSeparator starts semver build metadata (`v0.24.11+436.g28b6f838e6e1.dirty`), which
// carries no precedence: Newer drops it, so the full Version() string orders like BaseVersion().
const buildMetadataSeparator = "+"

// retractedVersions is the exact list of burned v1.x versions, ascending: the numbers
// proxy.golang.org or sum.golang.org already hold for apogee, which no release may reuse and go.mod
// retracts by name. The highest is the self-retracting placeholder tag that carries the retract
// block above every other burned version; recovering from a bad placeholder appends the next v1.8.x
// here. Every other v1.x number, v1.0.0 included, stays releasable. Those tags outrank every 0.x
// release numerically, so a lookup that ever surfaced one must not announce it as an upgrade.
var retractedVersions = []string{"v1.1.0", "v1.7.0", "v1.8.0", "v1.8.1"}

// version is a parsed `vMAJOR.MINOR.PATCH` release tag.
type version struct {
	major int
	minor int
	patch int
}

// Newer reports whether latest is a strictly higher release than current, comparing the numeric
// major, minor and patch fields in that order (so v0.10.0 outranks v0.9.0). Build metadata after a
// `+` on either side is ignored. It answers false — never newer — when either value is malformed
// (`dev`, an empty string, a pre-release tag) or when latest is one of the burned versions
// [RetractedVersions] lists.
func Newer(current, latest string) bool {
	currentVersion, isCurrentValid := parseVersion(current)
	latestVersion, isLatestValid := parseVersion(latest)
	if !isCurrentValid || !isLatestValid {
		return false
	}
	if IsRetracted(latest) {
		return false
	}
	return latestVersion.compare(currentVersion) > 0
}

// RetractedVersions returns the burned v1.x versions go.mod retracts, ascending. The slice is a
// copy, so a caller may change it without touching the list.
func RetractedVersions() []string {
	return slices.Clone(retractedVersions)
}

// IsRetracted reports whether tag is exactly one of the burned versions [RetractedVersions]
// lists. Build metadata after a `+` is dropped first; a malformed tag is never retracted.
func IsRetracted(tag string) bool {
	parsed, isValid := parseVersion(tag)
	if !isValid {
		return false
	}
	return slices.Contains(retractedVersions, parsed.String())
}

// parseVersion parses a release tag, dropping any build metadata first. The bool is false when
// the remainder is not exactly `vMAJOR.MINOR.PATCH`.
func parseVersion(text string) (version, bool) {
	tag, _, _ := strings.Cut(text, buildMetadataSeparator)
	fields := releaseTagPattern.FindStringSubmatch(tag)
	if fields == nil {
		return version{}, false
	}

	numbers := make([]int, 0, len(fields)-1)
	for _, field := range fields[1:] {
		number, err := strconv.Atoi(field)
		if err != nil {
			// Only an overflowing field reaches here; the pattern admits digits alone.
			return version{}, false
		}
		numbers = append(numbers, number)
	}
	return version{major: numbers[0], minor: numbers[1], patch: numbers[2]}, true
}

// compare returns a negative number when v orders before other, zero when they are equal and a
// positive number when v orders after it.
func (v version) compare(other version) int {
	if v.major != other.major {
		return v.major - other.major
	}
	if v.minor != other.minor {
		return v.minor - other.minor
	}
	return v.patch - other.patch
}

// String renders v as its canonical `vMAJOR.MINOR.PATCH` tag.
func (v version) String() string {
	return "v" + strconv.Itoa(v.major) + "." + strconv.Itoa(v.minor) + "." + strconv.Itoa(v.patch)
}
