package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// The paths and headings prepare edits. The rollup heading's shape — `## [X.Y.Z] — YYYY-MM-DD`,
// the version without its `v`, an em dash, the local date — is the one every published release
// in CHANGELOG.md carries (5b3ed35c cut v0.24.11 this way).
const (
	changelogFile      = "CHANGELOG.md"
	unreleasedHeading  = "## [Unreleased]"
	releaseHeadingLead = "## ["
	releaseDateLayout  = "2006-01-02"
)

// prepareOptions is what the prepare verb was asked to do.
type prepareOptions struct {
	// dir is the checkout prepare edits and commits in.
	dir string
	// version is the release to cut, as typed: prepare parses it itself, so a malformed one is
	// a refused run (exit 1) rather than a usage error.
	version string
	// now is the clock the rollup heading's date is read from.
	now func() time.Time
}

// newPrepareCommand builds the prepare verb.
func newPrepareCommand(commands runner) *cobra.Command {
	options := prepareOptions{dir: ".", now: time.Now}

	cmd := &cobra.Command{
		Use:   "prepare vX.Y.Z",
		Short: "Make the CHANGELOG rollup and VERSION bump commits for a release",
		Long: "prepare makes exactly two local commits and pushes nothing: \"chore(release): cut vX.Y.Z\",\n" +
			"which gives every bullet under ## [Unreleased] in CHANGELOG.md its own\n" +
			"## [X.Y.Z] — YYYY-MM-DD heading (today's local date), then\n" +
			"\"chore(version): bump version to vX.Y.Z\", which writes vX.Y.Z to VERSION. It refuses,\n" +
			"writing nothing, when the version is malformed or not above the highest release tag,\n" +
			"VERSION already reads it, the working tree is not clean, ## [Unreleased] is missing or\n" +
			"holds no bullet, or CHANGELOG.md already has a heading for the version. A bare X.Y.Z\n" +
			"reads as vX.Y.Z.",
		Args: cobra.ExactArgs(1),
		RunE: runE(func(cmd *cobra.Command, args []string) error {
			options.version = args[0]
			return runPrepare(cmd.Context(), commands, cmd.OutOrStdout(), options)
		}),
	}
	return cmd
}

// runPrepare checks every precondition before it writes anything, then makes the rollup commit
// and the bump commit, in that order, each staging its one path by name.
func runPrepare(ctx context.Context, commands runner, output io.Writer, options prepareOptions) error {
	version, err := parseReleaseVersion(options.version)
	if err != nil {
		return err
	}

	rollup, err := checkPrepare(ctx, commands, options.dir, version, options.now())
	if err != nil {
		return err
	}

	rollupSubject := "chore(release): cut " + version.String()
	if err := commitFile(ctx, commands, options.dir, changelogFile, rollup, rollupSubject); err != nil {
		return err
	}
	bumpSubject := "chore(version): bump version to " + version.String()
	if err := commitFile(ctx, commands, options.dir, versionFile, []byte(version.String()+"\n"), bumpSubject); err != nil {
		return fmt.Errorf("%w (the rollup commit %q was made)", err, rollupSubject)
	}

	_, _ = fmt.Fprintf(output, "committed %q\ncommitted %q\nnothing was pushed\n", rollupSubject, bumpSubject)
	return nil
}

// checkPrepare runs every refusal check and, when none refuses, returns CHANGELOG.md as the
// rollup commit will write it. Every refusal is reported together, so one run names them all.
func checkPrepare(ctx context.Context, commands runner, dir string, version releaseVersion, today time.Time) ([]byte, error) {
	var refusals []error

	status, err := commands.run(ctx, dir, nil, "git", "status", "--porcelain")
	switch {
	case err != nil:
		refusals = append(refusals, fmt.Errorf("read the working tree's status: %w", err))
	case strings.TrimSpace(status) != "":
		refusals = append(refusals, errors.New("the working tree is not clean"))
	}

	tags, err := commands.run(ctx, dir, nil, "git", "tag", "--list", "v*")
	if err != nil {
		refusals = append(refusals, fmt.Errorf("list the release tags: %w", err))
	} else if highest := highestReleaseTag(tags); highest != "" {
		if refusal := checkAboveTag(version, highest); refusal != nil {
			refusals = append(refusals, refusal)
		}
	}

	if refusal := checkVersionFile(dir, version); refusal != nil {
		refusals = append(refusals, refusal)
	}

	var rollup []byte
	changelog, err := os.ReadFile(filepath.Join(dir, changelogFile))
	if err != nil {
		refusals = append(refusals, fmt.Errorf("read %s: %w", changelogFile, err))
	} else if rollup, err = rollUpChangelog(changelog, version, today); err != nil {
		refusals = append(refusals, err)
	}

	if len(refusals) > 0 {
		return nil, fmt.Errorf("refusing to prepare %s, nothing was written:\n%w", version, errors.Join(refusals...))
	}
	return rollup, nil
}

// checkAboveTag refuses a version that is not strictly above highest, the highest release tag.
func checkAboveTag(version releaseVersion, highest string) error {
	highestVersion, err := parseReleaseVersion(highest)
	if err != nil {
		return fmt.Errorf("read the highest release tag: %w", err)
	}
	if version.compare(highestVersion) <= 0 {
		return fmt.Errorf("%s is not above the highest release tag %s", version, highest)
	}
	return nil
}

// checkVersionFile refuses a version VERSION already reads: an unpushed micro-bump has claimed
// it, and pushing that bump tags it, so the release must be the next one instead.
func checkVersionFile(dir string, version releaseVersion) error {
	raw, err := os.ReadFile(filepath.Join(dir, versionFile))
	if err != nil {
		return fmt.Errorf("read %s: %w", versionFile, err)
	}
	current, err := parseReleaseVersion(strings.TrimSpace(string(raw)))
	if err != nil || current.compare(version) != 0 {
		// A VERSION that does not parse cannot equal the version; the bump commit replaces it.
		return nil
	}
	next := releaseVersion{major: version.major, minor: version.minor, patch: version.patch + 1}
	return fmt.Errorf("%s already reads %s, an unpushed micro-bump: cut the next patch, %s, instead",
		versionFile, version, next)
}

// rollUpChangelog returns changelog with a `## [X.Y.Z] — YYYY-MM-DD` heading inserted directly
// under `## [Unreleased]`, one blank line on each side, so every bullet that sat under
// Unreleased now sits under the release. It refuses a changelog with no Unreleased heading, an
// Unreleased section with no bullet, or a heading for the version already. The file's own line
// ending is kept, so a CRLF checkout gains a CRLF heading.
func rollUpChangelog(changelog []byte, version releaseVersion, today time.Time) ([]byte, error) {
	text := string(changelog)
	newline := "\n"
	if strings.Contains(text, "\r\n") {
		newline = "\r\n"
	}
	lines := strings.SplitAfter(text, "\n")

	bare := strings.TrimPrefix(version.String(), "v")
	versionHeading := releaseHeadingLead + bare + "]"
	unreleased := -1
	for index, line := range lines {
		content := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(content, versionHeading) {
			return nil, fmt.Errorf("%s already has a %s heading", changelogFile, versionHeading)
		}
		if unreleased < 0 && content == unreleasedHeading {
			unreleased = index
		}
	}
	if unreleased < 0 {
		return nil, fmt.Errorf("%s has no %s heading", changelogFile, unreleasedHeading)
	}

	// The Unreleased section runs to the next release heading; its body starts at its first
	// non-blank line.
	body := unreleased + 1
	for body < len(lines) && strings.TrimSpace(lines[body]) == "" {
		body++
	}
	hasBullet := false
	for _, line := range lines[body:] {
		if strings.HasPrefix(line, releaseHeadingLead) {
			break
		}
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			hasBullet = true
			break
		}
	}
	if !hasBullet {
		return nil, fmt.Errorf("%s has no bullet under %s to release", changelogFile, unreleasedHeading)
	}

	heading := fmt.Sprintf("%s%s] — %s", releaseHeadingLead, bare, today.Format(releaseDateLayout))
	var rollup strings.Builder
	for _, line := range lines[:unreleased] {
		rollup.WriteString(line)
	}
	rollup.WriteString(unreleasedHeading + newline + newline + heading + newline + newline)
	for _, line := range lines[body:] {
		rollup.WriteString(line)
	}
	return []byte(rollup.String()), nil
}

// commitFile writes content to the checkout's path, stages that one path by name — never every
// change — and commits it with subject.
func commitFile(ctx context.Context, commands runner, dir, path string, content []byte, subject string) error {
	target := filepath.Join(dir, path)
	mode := os.FileMode(documentMode)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(target, content, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if _, err := commands.run(ctx, dir, nil, "git", "add", path); err != nil {
		return fmt.Errorf("stage %s: %w", path, err)
	}
	if _, err := commands.run(ctx, dir, nil, "git", "commit", "--quiet", "-m", subject); err != nil {
		return fmt.Errorf("commit %q: %w", subject, err)
	}
	return nil
}
