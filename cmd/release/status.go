package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// errGateBlocked is the run failure status reports after printing a blocked gate, so the exit
// status alone tells a driving script the cut may not proceed.
var errGateBlocked = errors.New("the release gate is blocked")

// statusOptions is what the status verb was asked to do.
type statusOptions struct {
	// dir is the checkout status reads: its VERSION file and its git state.
	dir string
	// version is the release asked about, "" for the checkout's VERSION.
	version string
	isJSON  bool
}

// statusReport is everything status prints. Its JSON keys are a contract: the release skills
// read them, so a rename here breaks those skills.
type statusReport struct {
	Version    string         `json:"version"`
	HighestTag string         `json:"highestTag"`
	Gate       gateReport     `json:"gate"`
	Baseline   baselineReport `json:"baseline"`
	// Steps lists each publish step's state. No step is probed yet, so it is always empty.
	Steps []any `json:"steps"`
}

// baselineReport is the release the next one is measured from: the latest *published* GitHub
// release — never merely the newest tag, since a micro-bump tags every shipped feature — its
// publish date, and the commits made since it.
type baselineReport struct {
	Tag       string `json:"tag"`
	Published string `json:"published"`
	Range     string `json:"range"`
	Commits   int    `json:"commits"`
}

// newStatusCommand builds the status verb.
func newStatusCommand(commands runner) *cobra.Command {
	options := statusOptions{dir: "."}

	cmd := &cobra.Command{
		Use:   "status [vX.Y.Z]",
		Short: "Report the release gate and the release baseline",
		Long: "status is read-only apart from fetching origin. It reports the release gate — a clean\n" +
			"tree, the gate commit (HEAD past its CHANGELOG.md/VERSION-only commits) on origin/main,\n" +
			"origin/main contained in HEAD, and a successful ci.yml run — naming each condition that\n" +
			"blocks it; the baseline (the latest published release and the commits since); and the\n" +
			"highest release tag. The version defaults to VERSION; a bare X.Y.Z reads as vX.Y.Z.\n" +
			"It exits 0 when the gate clears and 1 when it is blocked or a check could not run.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return fmt.Errorf("status takes at most one version, got %d arguments", len(args))
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
			return runStatus(cmd.Context(), commands, cmd.OutOrStdout(), options)
		}),
	}
	cmd.Flags().BoolVar(&options.isJSON, "json", false, "print the report as JSON")
	return cmd
}

// runStatus gathers the report, prints it, and then fails when the gate is blocked or a check
// could not run. The report is printed either way, so a blocked or broken run still shows what
// it found.
func runStatus(ctx context.Context, commands runner, output io.Writer, options statusOptions) error {
	var problems []error
	report := statusReport{Version: options.version, Steps: []any{}}

	if report.Version == "" {
		version, err := readCheckoutVersion(options.dir)
		if err != nil {
			problems = append(problems, err)
		} else {
			report.Version = version
		}
	}

	report.Gate = checkGate(ctx, commands, options.dir)

	tags, err := commands.run(ctx, options.dir, nil, "git", "tag", "--list", "v*")
	if err != nil {
		problems = append(problems, fmt.Errorf("list the release tags: %w", err))
	} else {
		report.HighestTag = highestReleaseTag(tags)
	}

	baseline, err := readBaseline(ctx, commands, options.dir)
	if err != nil {
		problems = append(problems, err)
	}
	report.Baseline = baseline

	if err := printStatus(output, report, options.isJSON); err != nil {
		return err
	}
	if len(problems) > 0 {
		return errors.Join(problems...)
	}
	if !report.Gate.IsOK {
		return errGateBlocked
	}
	return nil
}

// readCheckoutVersion reads the checkout's VERSION file as a release version.
func readCheckoutVersion(dir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, versionFile))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", versionFile, err)
	}
	version, err := parseReleaseVersion(strings.TrimSpace(string(raw)))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", versionFile, err)
	}
	return version.String(), nil
}

// readBaseline finds the latest published release and counts the commits made since it. What
// it found before a failure is returned with the error, so the report still shows it.
func readBaseline(ctx context.Context, commands runner, dir string) (baselineReport, error) {
	output, err := commands.run(ctx, dir, nil, "gh", "release", "view", "--json", "tagName,publishedAt")
	if err != nil {
		return baselineReport{}, fmt.Errorf("find the latest published release: %w", err)
	}
	var release struct {
		TagName     string `json:"tagName"`
		PublishedAt string `json:"publishedAt"`
	}
	if err := json.Unmarshal([]byte(output), &release); err != nil {
		return baselineReport{}, fmt.Errorf("parse gh release view output: %w", err)
	}
	if release.TagName == "" {
		return baselineReport{}, errors.New("gh release view named no latest release")
	}

	baseline := baselineReport{
		Tag:       release.TagName,
		Published: release.PublishedAt,
		Range:     release.TagName + "..HEAD",
	}
	count, err := commands.run(ctx, dir, nil, "git", "rev-list", "--count", baseline.Range)
	if err != nil {
		return baseline, fmt.Errorf("count the commits in %s: %w", baseline.Range, err)
	}
	baseline.Commits, err = strconv.Atoi(strings.TrimSpace(count))
	if err != nil {
		return baseline, fmt.Errorf("count the commits in %s: %w", baseline.Range, err)
	}
	return baseline, nil
}

// printStatus writes the report as indented JSON, or as a short table.
func printStatus(output io.Writer, report statusReport, isJSON bool) error {
	if isJSON {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return fmt.Errorf("write the report: %w", err)
		}
		return nil
	}

	gate := "cleared"
	if !report.Gate.IsOK {
		gate = "BLOCKED"
	}
	if report.Gate.Commit != "" {
		gate += " at " + short(report.Gate.Commit)
	}
	baseline := "unknown"
	if report.Baseline.Tag != "" {
		baseline = fmt.Sprintf("%s (published %s), %s: %d commits",
			report.Baseline.Tag, report.Baseline.Published, report.Baseline.Range, report.Baseline.Commits)
	}

	table := tabwriter.NewWriter(output, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(table, "version\t%s\n", orUnknown(report.Version))
	_, _ = fmt.Fprintf(table, "highest tag\t%s\n", orUnknown(report.HighestTag))
	_, _ = fmt.Fprintf(table, "baseline\t%s\n", baseline)
	_, _ = fmt.Fprintf(table, "gate\t%s\n", gate)
	for _, failure := range report.Gate.Failures {
		_, _ = fmt.Fprintf(table, "\t- %s\n", failure)
	}
	if err := table.Flush(); err != nil {
		return fmt.Errorf("write the report: %w", err)
	}
	return nil
}

// orUnknown stands in for a value status could not read.
func orUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
