package main

// `apogee update` (ADR 0097 decision 8): the explicit upgrade of a release-archive install. Every
// other install belongs to its package manager or to the user's toolchain, so the command names
// that channel's own upgrade and refuses; nothing here ever runs a package manager.
//
// On an archive install it downloads the running platform's release archive, verifies it against
// the release's SHA256SUMS (internal/update), stages the binary in a directory beside the
// executable, runs the staged binary's `--version` and only then swaps it into place. The swap is
// per-OS (update_swap_unix.go, update_swap_windows.go); every step before it leaves the installed
// binary untouched.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/update"
)

// stagedVersionTimeout bounds the staged binary's `--version` run, so a binary that hangs fails
// the update instead of holding it forever.
const stagedVersionTimeout = 30 * time.Second

// updateStagingPattern names the directory the new binary is staged in. It sits beside the
// executable so the swap is a rename on one file system, never a copy across two.
const updateStagingPattern = ".apogee-update-*"

// oldBinarySuffix ends the name the Windows swap moves the running executable to: Windows lets a
// running executable be renamed but not deleted, so it stays behind until the next start sweeps it.
const oldBinarySuffix = ".old"

// updateDeps is what `apogee update` takes from its host rather than deciding for itself. A test
// fills it to force an install Method, a release server and a terminal; newUpdateCommand fills the
// production values.
type updateDeps struct {
	// current is the running binary's release version (apogee.BaseVersion).
	current string
	// inputs reads the install-method detection inputs — item 6's installInputs seam in production.
	// Its ExePath, symlinks resolved, is also the file an archive update replaces.
	inputs func() update.Inputs
	// client is the release server the lookup and the download go to.
	client update.Client
	// isTerminal reports whether stdin is a terminal the confirmation may be asked on.
	isTerminal func() bool
	// stdin is where the confirmation answer is read from.
	stdin io.Reader
	// goos and goarch name the release archive downloaded: the running platform's.
	goos   string
	goarch string
}

// updateFlags are the command's two switches.
type updateFlags struct {
	// isConfirmed skips the [y/N] prompt (--yes).
	isConfirmed bool
	// isCheckOnly reports whether a newer release exists and changes nothing (--check).
	isCheckOnly bool
}

// newUpdateCommand builds `apogee update` over this process's own executable, release server and
// terminal.
func newUpdateCommand() *cobra.Command {
	return newUpdateCommandWith(updateDeps{
		current:    apogee.BaseVersion(),
		inputs:     installInputs,
		client:     update.NewClient(updateBaseURL),
		isTerminal: func() bool { return term.IsTerminal(os.Stdin.Fd()) },
		stdin:      os.Stdin,
		goos:       runtime.GOOS,
		goarch:     runtime.GOARCH,
	})
}

// newUpdateCommandWith is newUpdateCommand over the dependencies the caller states.
func newUpdateCommandWith(deps updateDeps) *cobra.Command {
	var flags updateFlags
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Replace a release-archive install with the latest release",
		Long: "apogee update replaces this binary with the latest published release when it was\n" +
			"unpacked by hand from a release archive. It asks before it changes anything, verifies\n" +
			"the download against the release's SHA256SUMS and runs the new binary's --version\n" +
			"before swapping it in; any failure leaves the installed binary as it was.\n\n" +
			"An install made by Homebrew, Scoop, winget, `go install` or a source build is not\n" +
			"apogee's to replace: the command names that channel's own upgrade and exits 1.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpdate(cmd.Context(), deps, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&flags.isConfirmed, "yes", false, "update without asking")
	cmd.Flags().BoolVar(&flags.isCheckOnly, "check", false, "report whether a newer release exists and change nothing")
	return cmd
}

// runUpdate is the whole command: refuse a managed install, look the latest release up, report or
// confirm, then replace the binary. Every refusal and failure is an error, which exits 1.
func runUpdate(ctx context.Context, deps updateDeps, flags updateFlags, out io.Writer) error {
	inputs := deps.inputs()
	if method := update.Detect(inputs); method != update.Archive {
		return managedInstallError(method)
	}

	latest, err := update.Latest(ctx, deps.client)
	if err != nil {
		return fmt.Errorf("apogee update: could not look up the latest release: %w", err)
	}
	if !update.Newer(deps.current, latest) {
		_, _ = fmt.Fprintf(out, "apogee %s is up to date\n", deps.current)
		return nil
	}
	if flags.isCheckOnly {
		_, _ = fmt.Fprintf(out, "apogee %s → %s is available — run: apogee update\n", deps.current, latest)
		return nil
	}

	isConfirmed, err := confirmUpdate(deps, flags, out, latest)
	if err != nil {
		return err
	}
	if !isConfirmed {
		_, _ = fmt.Fprintln(out, "Update cancelled.")
		return nil
	}
	if err := replaceExecutable(ctx, deps, inputs.ExePath, latest); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "Updated apogee %s → %s\n", deps.current, latest)
	return nil
}

// managedInstallError is the refusal for an install apogee does not own: the channel it came from
// and the command that upgrades it.
func managedInstallError(method update.Method) error {
	return fmt.Errorf("apogee was installed via %s — run: %s", channelName(method), method.UpgradeCommand())
}

// channelName is how the refusal names the channel an install came from.
func channelName(method update.Method) string {
	switch method {
	case update.Homebrew:
		return "Homebrew"
	case update.Scoop:
		return "Scoop"
	case update.Winget:
		return "winget"
	case update.GoInstall:
		return "go install"
	case update.Source:
		return "a source build"
	default:
		return "a release archive"
	}
}

// confirmUpdate asks `Update <cur> → <latest>? [y/N]` unless --yes answered it already. Without
// --yes, stdin must be a terminal: a script that wants the update says so with --yes rather than
// by piping an answer in.
func confirmUpdate(deps updateDeps, flags updateFlags, out io.Writer, latest string) (bool, error) {
	if flags.isConfirmed {
		return true, nil
	}
	if !deps.isTerminal() {
		return false, fmt.Errorf("apogee update: stdin is not a terminal — re-run with --yes to update %s → %s",
			deps.current, latest)
	}
	_, _ = fmt.Fprintf(out, "Update %s → %s? [y/N] ", deps.current, latest)
	answer, err := bufio.NewReader(deps.stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("apogee update: read the answer: %w", err)
	}
	return isYes(answer), nil
}

// replaceExecutable stages latest beside exePath, proves the staged binary runs and reports
// latest, and swaps it in. The staging directory is removed on the way out, whatever happened.
func replaceExecutable(ctx context.Context, deps updateDeps, exePath, latest string) error {
	if exePath == "" {
		return errors.New("apogee update: could not locate the running executable")
	}
	stagingDir, err := os.MkdirTemp(filepath.Dir(exePath), updateStagingPattern)
	if err != nil {
		return fmt.Errorf("apogee update: could not stage beside %s: %w", exePath, err)
	}
	// Best-effort: after a successful swap the directory is empty, and a failure has already been
	// reported — a leftover directory must not turn either outcome into a different one.
	defer func() { _ = os.RemoveAll(stagingDir) }()

	stagedPath, err := update.Stage(ctx, deps.client, latest, deps.goos, deps.goarch, stagingDir)
	if err != nil {
		return fmt.Errorf("apogee update: %w", err)
	}
	if err := verifyStagedVersion(ctx, stagedPath, latest); err != nil {
		return err
	}
	return swapExecutable(stagedPath, exePath)
}

// verifyStagedVersion runs `<stagedPath> --version` and requires its output to name latest, so a
// binary that cannot start on this host, or is not the release asked for, is never swapped in.
func verifyStagedVersion(ctx context.Context, stagedPath, latest string) error {
	ctx, cancel := context.WithTimeout(ctx, stagedVersionTimeout)
	defer cancel()

	output, err := exec.CommandContext(ctx, stagedPath, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("apogee update: the downloaded binary did not run: %w", err)
	}
	if !strings.Contains(string(output), latest) {
		return fmt.Errorf("apogee update: the downloaded binary reports %q, not %s",
			strings.TrimSpace(string(output)), latest)
	}
	return nil
}

// sweepLeftoverExecutable removes the `<exe>.old` a previous Windows update left behind. It runs
// at every start and is a no-op where the swap never leaves one (swapLeavesOldExecutable), so it
// never deletes a user's own `apogee.old` on Unix.
func sweepLeftoverExecutable() {
	if !swapLeavesOldExecutable {
		return
	}
	removeLeftoverExecutable(installInputs().ExePath)
}

// removeLeftoverExecutable deletes `<exePath>.old`. It is best-effort: a missing file is the
// ordinary case, and a file still locked is retried at the next start.
func removeLeftoverExecutable(exePath string) {
	if exePath == "" {
		return
	}
	_ = os.Remove(exePath + oldBinarySuffix)
}
