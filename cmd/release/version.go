package main

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/update"
)

// versionPattern is the one release-version shape apogee tags: `vMAJOR.MINOR.PATCH`, decimal
// numbers without leading zeros, the `v` optional on input. It matches internal/update's
// releaseTagPattern, the shape the update check accepts, so this program never names a release
// the shipped binary would read as malformed.
var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// retractKeyword opens a go.mod retract directive, either one line (`retract v1.1.0`) or a
// parenthesised block with one entry per line.
const retractKeyword = "retract"

// goModComment starts a go.mod comment, which runs to the end of its line.
const goModComment = "//"

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

// checkNotRetracted refuses a burned version, one of the exact list internal/update owns
// ([update.RetractedVersions]). Such a release ships already retracted: `go install …@latest` and
// the update check both pass over it, so nobody is offered it, and the number cannot be taken back.
// Every other v1.x number, v1.0.0 included, is releasable.
func checkNotRetracted(version releaseVersion) error {
	if !update.IsRetracted(version.String()) {
		return nil
	}
	return fmt.Errorf("%s is a burned version (%s), which no release may reuse",
		version, strings.Join(update.RetractedVersions(), ", "))
}

// checkRetractKept refuses a go.mod whose retractions are not exactly the burned versions. Go
// reads a retraction from the go.mod of the highest version, so every release carries the whole
// list: a release that lost an entry would bring that burned version back, and one that kept the
// old `[v1.0.0, v1.8.0]` range would retract every unburned v1.x release with it. Both forms Go
// accepts are read — one-line `retract vX` directives and `retract ( … )` blocks — with comments
// stripped and order and repeats ignored; any range, or any other set of versions, is refused,
// naming the block go.mod must carry.
func checkRetractKept(goMod []byte) error {
	retracted := goModRetractions(goMod)
	found := slices.Clone(retracted)
	slices.Sort(found)
	found = slices.Compact(found)
	burned := update.RetractedVersions()
	slices.Sort(burned)
	if slices.Equal(found, burned) {
		return nil
	}

	description := "nothing"
	if len(retracted) > 0 {
		description = strings.Join(retracted, ", ")
	}
	return fmt.Errorf("%s retracts %s, not exactly the burned versions; it must carry:\n%s",
		goModFile, description, retractBlock())
}

// goModRetractions returns every entry go.mod's retract directives name, in file order: a
// version as written (`v1.1.0`), a range as written (`[v1.0.0, v1.8.0]`). Comments are stripped
// first, so a commented-out directive names nothing. It reads both one-line directives and
// parenthesised blocks; any other line is skipped.
func goModRetractions(goMod []byte) []string {
	var entries []string
	isInBlock := false
	for line := range strings.Lines(string(goMod)) {
		text, _, _ := strings.Cut(line, goModComment)
		text = strings.TrimSpace(text)
		if isInBlock {
			if text == ")" {
				isInBlock = false
			} else if text != "" {
				entries = append(entries, text)
			}
			continue
		}

		argument, isDirective := retractArgument(text)
		if !isDirective || argument == "" {
			continue
		}
		if argument == "(" {
			isInBlock = true
			continue
		}
		entries = append(entries, argument)
	}
	return entries
}

// retractArgument returns what follows the retract keyword on a comment-stripped, trimmed go.mod
// line — `(` for a block opener, otherwise the version or range — and false when the line is not a
// retract directive.
func retractArgument(text string) (string, bool) {
	rest, isFound := strings.CutPrefix(text, retractKeyword)
	if !isFound {
		return "", false
	}
	if rest != "" && !strings.ContainsAny(rest[:1], " \t([") {
		// A longer word that merely starts with "retract" is not the directive.
		return "", false
	}
	return strings.TrimSpace(rest), true
}

// retractBlock is the go.mod block that retracts exactly the burned versions, one per line in
// ascending order, as the repository's own go.mod carries it.
func retractBlock() string {
	var block strings.Builder
	block.WriteString(retractKeyword + " (\n")
	for _, version := range update.RetractedVersions() {
		block.WriteString("\t" + version + "\n")
	}
	block.WriteString(")")
	return block.String()
}

// highestReleaseTag returns the highest release tag in tags — one tag per line, as
// `git tag --list` prints them — setting aside the burned v1.x versions, which outrank every 0.x
// release numerically, and every tag that is not exactly `vX.Y.Z`. It returns "" when no tag qualifies.
func highestReleaseTag(tags string) string {
	var highest releaseVersion
	isFound := false
	for _, tag := range strings.Fields(tags) {
		if !strings.HasPrefix(tag, "v") {
			continue
		}
		candidate, err := parseReleaseVersion(tag)
		if err != nil || update.IsRetracted(candidate.String()) {
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
