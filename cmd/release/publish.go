package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// How long publish waits for the CI-made tag after pushing, and how often it looks. The
// tag-on-version-bump workflow tags a pushed VERSION bump within a minute or two; ten minutes
// leaves room for a queued runner.
const (
	defaultTagTimeout = 10 * time.Minute
	tagPollInterval   = 10 * time.Second
)

// gitObjectTag is what `git cat-file -t` prints for an annotated tag object; a lightweight tag
// resolves straight to a commit.
const gitObjectTag = "tag"

// nonInteractiveEnv keeps git and gh from prompting on the terminal: publish never prompts, so a
// missing credential fails the command instead of waiting on a human.
var nonInteractiveEnv = []string{"GIT_TERMINAL_PROMPT=0", "GH_PROMPT_DISABLED=1"}

// publishOptions is what the publish verb was asked to do.
type publishOptions struct {
	// dir is the checkout publish pushes from and reads origin through.
	dir string
	// version is the release to publish, as typed: publish parses it itself, so a malformed one
	// is a refused run (exit 1) rather than a usage error.
	version string
	// notesFile is the release notes the GitHub release is created with.
	notesFile string
	isDryRun  bool
	// tagTimeout bounds the wait for the CI-made tag; pollInterval is the pause between looks,
	// taken through sleep.
	tagTimeout   time.Duration
	pollInterval time.Duration
	sleep        func(ctx context.Context, duration time.Duration) error
}

// publishRun is one publish run: what every step reads.
type publishRun struct {
	commands runner
	output   io.Writer
	options  publishOptions
	version  releaseVersion
	// notesPath is the notes file as an absolute path, so it reads the same from any directory.
	notesPath string
}

// publishAction is one step publish converges: what a dry run says it would do, and the work
// itself, run only while the step's probe reports it pending.
type publishAction struct {
	name     string
	describe func(run *publishRun) string
	converge func(ctx context.Context, run *publishRun) error
}

// publishActions are the steps publish converges, in the order it runs them; each one's probe
// is the publishSteps entry of the same name, so what status reports done is what publish skips.
var publishActions = []publishAction{
	{name: stepTag, describe: describeTag, converge: convergeTag},
	{name: stepRelease, describe: describeRelease, converge: convergeRelease},
}

// newPublishCommand builds the publish verb.
func newPublishCommand(commands runner) *cobra.Command {
	options := publishOptions{
		dir:          ".",
		tagTimeout:   defaultTagTimeout,
		pollInterval: tagPollInterval,
		sleep:        sleepContext,
	}

	cmd := &cobra.Command{
		Use:   "publish vX.Y.Z --notes-file F",
		Short: "Publish a prepared release: push, wait for the tag, create the GitHub release",
		Long: "publish converges each publish step, skipping every step its probe reports done, so\n" +
			"a half-finished release is resumed by running it again. While the tag is not on origin\n" +
			"it refuses unless the release gate clears and VERSION at HEAD reads the version; it\n" +
			"then pushes HEAD to main and waits (--tag-timeout) for the CI-made annotated tag — it\n" +
			"never creates one. The release step builds the archives in a fresh clone at the tag\n" +
			"and creates the GitHub release, or uploads only the assets it lacks after checking the\n" +
			"rebuilt sums against its SHA256SUMS. It never prompts or reads stdin, and exits 1\n" +
			"after printing the step table when anything fails. --dry-run runs the gate and the\n" +
			"probes and prints what each pending step would do, changing nothing.",
		Args: cobra.ExactArgs(1),
		RunE: runE(func(cmd *cobra.Command, args []string) error {
			options.version = args[0]
			return runPublish(cmd.Context(), commands, cmd.OutOrStdout(), options)
		}),
	}
	cmd.Flags().StringVar(&options.notesFile, "notes-file", "", "the release notes file (required)")
	cmd.Flags().BoolVar(&options.isDryRun, "dry-run", false, "print what each pending step would do, changing nothing")
	cmd.Flags().DurationVar(&options.tagTimeout, "tag-timeout", defaultTagTimeout, "how long to wait for the CI-made tag after pushing")
	if err := cmd.MarkFlagRequired("notes-file"); err != nil {
		panic(err) // the flag is declared just above; failing here is a defect of this program
	}
	return cmd
}

// runPublish probes every step, refuses before changing anything when a precondition fails, and
// then converges each pending step in order. The step table is printed whatever happens.
func runPublish(ctx context.Context, commands runner, output io.Writer, options publishOptions) error {
	version, err := parseReleaseVersion(options.version)
	if err != nil {
		return err
	}
	run := &publishRun{commands: commands, output: output, options: options, version: version}

	var refusals []error
	run.notesPath, err = checkNotesFile(options.notesFile)
	if err != nil {
		refusals = append(refusals, err)
	}

	steps := run.probe(ctx)
	// publishActions runs the tag step first, so steps[0] is its probe.
	gate := fmt.Sprintf("not checked: %s is on %s, so the release converges from remote state", version, remoteName)
	if steps[0].State != stateDone {
		gate, refusals = run.checkPushable(ctx, refusals)
	}
	for _, step := range steps {
		if step.State == stateUnknown {
			refusals = append(refusals, fmt.Errorf("could not tell where the %s step stands: %s", step.Name, step.Detail))
		}
	}

	if options.isDryRun {
		if err := printPublish(output, run, gate, steps, true); err != nil {
			return err
		}
		return errors.Join(refusals...)
	}
	if len(refusals) > 0 {
		if err := printPublish(output, run, gate, steps, false); err != nil {
			return err
		}
		return fmt.Errorf("refusing to publish %s, nothing was changed:\n%w", version, errors.Join(refusals...))
	}
	return run.converge(ctx, gate, steps)
}

// converge runs every pending step in order; a done or setup-pending step is skipped. A failed
// step stops the run, which prints the steps as the remote now holds them.
func (r *publishRun) converge(ctx context.Context, gate string, steps []stepReport) error {
	for index, action := range publishActions {
		if steps[index].State != statePending {
			continue
		}
		_, _ = fmt.Fprintf(r.output, "%s: %s\n", action.name, action.describe(r))
		if err := action.converge(ctx, r); err != nil {
			if printErr := printPublish(r.output, r, gate, r.probe(ctx), false); printErr != nil {
				return errors.Join(err, printErr)
			}
			return fmt.Errorf("the %s step failed: %w", action.name, err)
		}
		steps[index] = stepReport{Name: action.name, State: stateDone, Detail: "done by this run"}
	}
	return printPublish(r.output, r, gate, steps, false)
}

// probe asks the remote where each of publish's steps stands, in publish order.
func (r *publishRun) probe(ctx context.Context) []stepReport {
	steps := make([]stepReport, 0, len(publishActions))
	for _, action := range publishActions {
		index := slices.IndexFunc(publishSteps, func(step publishStep) bool { return step.name == action.name })
		steps = append(steps, publishSteps[index].probe(ctx, r.commands, r.options.dir, r.version))
	}
	return steps
}

// checkPushable judges what pushing HEAD needs: the release gate, and VERSION at HEAD reading the
// version — the bump the CI workflow tags. It returns the gate's line for the table and refusals
// with every blocking condition added.
func (r *publishRun) checkPushable(ctx context.Context, refusals []error) (string, []error) {
	report := checkGate(ctx, r.commands, r.options.dir)
	gate := "cleared"
	if !report.IsOK {
		gate = "BLOCKED"
	}
	if report.Commit != "" {
		gate += " at " + short(report.Commit)
	}
	for _, failure := range report.Failures {
		refusals = append(refusals, fmt.Errorf("gate: %s", failure))
	}

	raw, err := r.commands.run(ctx, r.options.dir, nil, "git", "show", "HEAD:"+versionFile)
	if err != nil {
		return gate, append(refusals, fmt.Errorf("read %s at HEAD: %w", versionFile, err))
	}
	if current := strings.TrimSpace(raw); current != r.version.String() {
		return gate, append(refusals, fmt.Errorf("%s at HEAD reads %q, not %s: run prepare first", versionFile, current, r.version))
	}
	return gate, refusals
}

// checkNotesFile requires the notes file to be readable and to hold some text, and returns its
// absolute path.
func checkNotesFile(notesFile string) (string, error) {
	notesPath, err := filepath.Abs(notesFile)
	if err != nil {
		return "", fmt.Errorf("resolve --notes-file: %w", err)
	}
	notes, err := os.ReadFile(notesPath)
	if err != nil {
		return "", fmt.Errorf("read --notes-file: %w", err)
	}
	if strings.TrimSpace(string(notes)) == "" {
		return "", fmt.Errorf("--notes-file %s is empty", notesFile)
	}
	return notesPath, nil
}

// printPublish writes the step table: the version, the gate, and each step's state. A dry run
// adds what each step would do.
func printPublish(output io.Writer, run *publishRun, gate string, steps []stepReport, isDryRun bool) error {
	table := tabwriter.NewWriter(output, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(table, "version\t%s\n", run.version)
	_, _ = fmt.Fprintf(table, "gate\t%s\n", gate)
	_, _ = fmt.Fprintln(table, "steps")
	for _, step := range steps {
		_, _ = fmt.Fprintf(table, "  %s\t%s\t%s\n", step.Name, step.State, step.Detail)
	}
	if isDryRun {
		_, _ = fmt.Fprintln(table, "plan (dry run, nothing was changed)")
		for index, action := range publishActions {
			plan := "skip: " + string(steps[index].State)
			if steps[index].State == statePending {
				plan = action.describe(run)
			}
			_, _ = fmt.Fprintf(table, "  %s\t%s\n", action.name, plan)
		}
	}
	if err := table.Flush(); err != nil {
		return fmt.Errorf("write the step table: %w", err)
	}
	return nil
}

// describeTag says what the tag step does.
func describeTag(run *publishRun) string {
	return fmt.Sprintf("push HEAD to %s when it is behind, then wait up to %s for the CI-made annotated tag %s at the %s bump",
		remoteMain, run.options.tagTimeout, run.version, versionFile)
}

// convergeTag pushes HEAD to main when origin/main is behind it, waits for the tag-on-version-bump
// workflow to tag the bump, and then requires that tag annotated and on the commit that set
// VERSION. It never creates a tag: the workflow is the one tagger.
func convergeTag(ctx context.Context, run *publishRun) error {
	dir := run.options.dir
	isPushed, err := isAncestor(ctx, run.commands, dir, "HEAD", remoteMain)
	if err != nil {
		return fmt.Errorf("check whether %s holds HEAD: %w", remoteMain, err)
	}
	if !isPushed {
		if _, err := run.commands.run(ctx, dir, nonInteractiveEnv, "git", "push", remoteName, "HEAD:"+mainBranch); err != nil {
			return fmt.Errorf("push HEAD to %s: %w", remoteMain, err)
		}
	}
	if err := waitForTag(ctx, run); err != nil {
		return err
	}
	return verifyTag(ctx, run)
}

// waitForTag polls origin for the version's tag until it appears or the timeout passes.
func waitForTag(ctx context.Context, run *publishRun) error {
	ref := "refs/tags/" + run.version.String()
	for waited := time.Duration(0); ; waited += run.options.pollInterval {
		listed, err := run.commands.run(ctx, run.options.dir, nonInteractiveEnv, "git", "ls-remote", "--tags", remoteName, ref)
		if err != nil {
			return fmt.Errorf("list %s's tags: %w", remoteName, err)
		}
		if strings.TrimSpace(listed) != "" {
			return nil
		}
		if waited >= run.options.tagTimeout {
			return fmt.Errorf("%s had no tag %s after %s: check the tag-on-version-bump workflow's run for the push, then run publish again",
				remoteName, run.version, run.options.tagTimeout)
		}
		if err := run.options.sleep(ctx, run.options.pollInterval); err != nil {
			return fmt.Errorf("wait for tag %s: %w", run.version, err)
		}
	}
}

// verifyTag fetches the tag — on a first run the workflow made it after the gate's fetch, so it
// is not local yet — and requires it annotated and on the last commit that changed VERSION.
func verifyTag(ctx context.Context, run *publishRun) error {
	dir := run.options.dir
	tag := run.version.String()
	ref := "refs/tags/" + tag
	if _, err := run.commands.run(ctx, dir, nonInteractiveEnv, "git", "fetch", "--quiet", remoteName, ref+":"+ref); err != nil {
		return fmt.Errorf("fetch tag %s: %w", tag, err)
	}

	kind, err := run.commands.run(ctx, dir, nil, "git", "cat-file", "-t", ref)
	if err != nil {
		return fmt.Errorf("read the type of tag %s: %w", tag, err)
	}
	if strings.TrimSpace(kind) != gitObjectTag {
		return fmt.Errorf("%s on %s is a lightweight tag; a release tag must be annotated", tag, remoteName)
	}

	tagged, err := revParse(ctx, run.commands, dir, tag)
	if err != nil {
		return fmt.Errorf("resolve tag %s: %w", tag, err)
	}
	bump, err := run.commands.run(ctx, dir, nil, "git", "log", "-1", "--format=%H", "HEAD", "--", versionFile)
	if err != nil {
		return fmt.Errorf("find the %s bump commit: %w", versionFile, err)
	}
	if bump = strings.TrimSpace(bump); tagged != bump {
		return fmt.Errorf("tag %s is on %s, not on the %s bump %s", tag, short(tagged), versionFile, short(bump))
	}
	return nil
}

// describeRelease says what the release step does.
func describeRelease(run *publishRun) string {
	return fmt.Sprintf("build the %d assets in a fresh clone at %s, then create GitHub release %s or upload the assets it lacks",
		len(releaseAssetNames(run.version)), run.version, run.version)
}

// convergeRelease builds the release's assets from the tag and creates the GitHub release, or,
// when the release exists, uploads only the assets it lacks — after checking the rebuilt sums
// against the SHA256SUMS it already carries, so a rebuild never mixes with assets it does not
// match. A release without SHA256SUMS has nothing to check against; the rebuild is
// byte-identical to any earlier one from the same tag, so its own sums are uploaded.
func convergeRelease(ctx context.Context, run *publishRun) error {
	dir := run.options.dir
	tag := run.version.String()
	uploaded, isFound, err := readReleaseAssets(ctx, run.commands, dir, run.version)
	if err != nil {
		return fmt.Errorf("read GitHub release %s: %w", tag, err)
	}

	workDir, err := os.MkdirTemp("", "apogee-publish-")
	if err != nil {
		return fmt.Errorf("create the build directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()
	outDir, err := buildReleaseAssets(ctx, run, workDir)
	if err != nil {
		return err
	}

	if !isFound {
		args := []string{"release", "create", tag, "--verify-tag", "--title", tag, "--notes-file", run.notesPath, "--latest"}
		args = append(args, assetPaths(outDir, releaseAssetNames(run.version))...)
		if _, err := run.commands.run(ctx, dir, nonInteractiveEnv, "gh", args...); err != nil {
			return fmt.Errorf("create GitHub release %s: %w", tag, err)
		}
		return nil
	}

	if slices.Contains(uploaded, checksumsFile) {
		if err := checkPublishedSums(ctx, run, outDir, uploaded); err != nil {
			return err
		}
	}
	missing := missingAssets(releaseAssetNames(run.version), uploaded)
	// --clobber replaces only an asset left in a failed upload state: an uploaded one is never
	// in missing.
	args := append([]string{"release", "upload", tag, "--clobber"}, assetPaths(outDir, missing)...)
	if _, err := run.commands.run(ctx, dir, nonInteractiveEnv, "gh", args...); err != nil {
		return fmt.Errorf("upload %s to GitHub release %s: %w", strings.Join(missing, ", "), tag, err)
	}
	return nil
}

// buildReleaseAssets fetches the tags, clones the checkout fresh into workDir, checks the tag out
// there and runs dist over it — never over the current checkout, whatever it holds. It returns
// the directory holding the assets.
func buildReleaseAssets(ctx context.Context, run *publishRun, workDir string) (string, error) {
	dir := run.options.dir
	tag := run.version.String()
	if _, err := run.commands.run(ctx, dir, nonInteractiveEnv, "git", "fetch", "--quiet", "--tags", remoteName); err != nil {
		return "", fmt.Errorf("fetch %s's tags: %w", remoteName, err)
	}
	toplevel, err := run.commands.run(ctx, dir, nil, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("find the checkout's top level: %w", err)
	}

	sourceDir := filepath.Join(workDir, "src")
	if _, err := run.commands.run(ctx, workDir, nil, "git", "clone", "--quiet", strings.TrimSpace(toplevel), sourceDir); err != nil {
		return "", fmt.Errorf("clone the checkout to build %s: %w", tag, err)
	}
	if _, err := run.commands.run(ctx, sourceDir, nil, "git", "checkout", "--quiet", tag); err != nil {
		return "", fmt.Errorf("check out %s in the clone: %w", tag, err)
	}
	bare, err := readBareVersion(sourceDir)
	if err != nil {
		return "", err
	}
	if bare != bareRelease(run.version) {
		return "", fmt.Errorf("%s at tag %s reads %s, not %s", versionFile, tag, bare, bareRelease(run.version))
	}

	outDir := filepath.Join(workDir, defaultOutDir)
	if err := runDist(ctx, run.commands, run.output, distOptions{sourceDir: sourceDir, outDir: outDir}); err != nil {
		return "", fmt.Errorf("build the %s assets: %w", tag, err)
	}
	return outDir, nil
}

// checkPublishedSums downloads the release's SHA256SUMS and requires every uploaded archive's
// published hash to equal the rebuilt one, naming the way out when one does not.
func checkPublishedSums(ctx context.Context, run *publishRun, outDir string, uploaded []string) error {
	tag := run.version.String()
	published, err := run.commands.run(ctx, run.options.dir, nonInteractiveEnv,
		"gh", "release", "download", tag, "--pattern", checksumsFile, "--output", "-")
	if err != nil {
		return fmt.Errorf("download %s from GitHub release %s: %w", checksumsFile, tag, err)
	}
	rebuilt, err := os.ReadFile(filepath.Join(outDir, checksumsFile))
	if err != nil {
		return fmt.Errorf("read the rebuilt %s: %w", checksumsFile, err)
	}

	var mismatched []string
	for _, name := range releaseAssetNames(run.version) {
		if name == checksumsFile || !slices.Contains(uploaded, name) {
			continue
		}
		want, err := checksumFor(string(rebuilt), name)
		if err != nil {
			return fmt.Errorf("read the rebuilt %s: %w", checksumsFile, err)
		}
		if got, err := checksumFor(published, name); err != nil || got != want {
			mismatched = append(mismatched, name)
		}
	}
	if len(mismatched) > 0 {
		return fmt.Errorf("the rebuilt %s does not match release %s's %s; remove the stale assets with "+
			"`gh release delete-asset %s <name>` (SHA256SUMS and %s), then run publish again",
			strings.Join(mismatched, ", "), tag, checksumsFile, tag, strings.Join(mismatched, ", "))
	}
	return nil
}

// assetPaths joins each asset name onto outDir.
func assetPaths(outDir string, names []string) []string {
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(outDir, name))
	}
	return paths
}

// sleepContext waits for duration or until ctx is done, whichever is first.
func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
