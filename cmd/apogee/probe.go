package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/probe"
)

// newProbeCommand builds `apogee probe` — the diagnosis command, whose FREE half is the bare
// noun (ADR 0021 §1). The parent's own RunE prints the host report, because "what is this box
// doing to Auto?" is the overwhelmingly common question and the answer costs nothing: no agent
// runs, no model is called, nothing is written. `apogee probe host` is the same report under a
// named child, so a script never has to rely on the bare parent's semantics staying put.
//
// The model half — `apogee probe model`, which spends live tokens AND writes a fingerprint
// record — is a separate, explicit child: the asymmetry in cost is the whole reason the command
// has halves at all, and nothing about a reachable endpoint may cause the battery to run.
//
// `apogee probe terminal` joins them as a third subject. It sits on the FREE side of that split —
// no agent, no model, no write — but it is a named child rather than part of the bare parent's
// report because it is the one probe that needs the screen: it measures the terminal by painting
// on the alternate screen and reading the cursor back, so it must be asked for.
func newProbeCommand() *cobra.Command {
	cmd := probeHostCommand("probe",
		"Report this host: confinement, roots, and endpoint reachability",
		"apogee probe reports what this machine can do, without running an agent:\n"+
			"the OS, the Confiner backend and what it can actually enforce, whether auto\n"+
			"mode can fence terminal commands here, the workspace and config roots, and\n"+
			"whether the configured endpoint answers.\n\n"+
			"It reads config.yaml and the APOGEE_* environment exactly as a session would,\n"+
			"so the settings it reports are the ones a session on this host would run with.\n"+
			"It never writes: no starter config is seeded and no state is recorded.")

	cmd.AddCommand(probeHostCommand("host",
		"Report this host (the same report as bare `apogee probe`)",
		"apogee probe host is the named child form of the host report bare `apogee probe`\n"+
			"prints — identical output, spelled out for scripts."))
	cmd.AddCommand(probeModelCommand())
	cmd.AddCommand(probeTerminalCommand())

	return cmd
}

// probeHostCommand builds one command that prints the host report. Both the `probe` parent and
// its `host` child are built from it so the two cannot drift into different reports; each gets
// its OWN options value and flag set, because a cobra flag variable is bound per command.
//
// The flags are the subset of the root's that CHANGES what is reported — the endpoint to probe
// and the two roots — declared here rather than inherited: the root's flags are its own (a
// subcommand declares what it needs), and --mode/--bypass/--resume would be noise on a command
// that starts no session.
func probeHostCommand(use, short, long string) *cobra.Command {
	var opts options

	cmd := &cobra.Command{
		Use:           use,
		Short:         short,
		Long:          long,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The same resolution a session performs (flag > env > file > default), including
			// the live host identity that selects a Host acknowledgement — so the effective
			// confine-to-workspace reported below is the one Auto would actually run with here
			// (ADR 0012's 2026-07-21 amendment), not what the file literally says. It reads
			// only; unlike the root's RunE, no starter config is seeded.
			if err := applyConfig(&opts, cmd.Flags().Changed, os.Getenv, os.ReadFile, func(msg string) { cmd.PrintErrln(msg) }); err != nil {
				return err
			}
			roots, err := resolveRoots(opts.configDir, opts.workspace)
			if err != nil {
				return err
			}

			// Mandatory labels an interrupted Windows run left on the disk — "" on every
			// other OS and on the normal Windows path. It is read HERE, before the backend
			// below is constructed, and it resolves the journal's own home rather than
			// roots.config: the backend writes there under any --config, so reading the
			// resolved root instead would let a non-default --config report a clean disk that
			// is not. Reading the journal directory is a directory listing, so the host half
			// stays read-only (ADR 0020 §2 / 0021 §1).
			residue := platform.ConfinementResidue()

			host := probe.GatherHost(cmd.Context(), probe.Inputs{
				GOOS:   runtime.GOOS,
				GOARCH: runtime.GOARCH,
				// The host's real backend for this OS, selected exactly as runRoot selects the
				// one it wires into the Agent, so the report cannot describe a backend the
				// session would not use — but built through the REPORT constructor, which
				// performs no crash recovery. A session's constructor finishes an interrupted
				// run's restore; doing that here would both break ADR 0021 §1's read-only
				// pledge and revert-and-delete the journal the residue line above exists to
				// report (ADR 0020 §2).
				Confiner:   platform.NewReportConfiner(),
				HostID:     platform.HostID(),
				Workspace:  roots.workspace,
				ConfigHome: roots.config,
				Endpoint:   opts.endpoint,
				// The resolved bearer token (the startup `servers:` entry's own `api-key`,
				// which APOGEE_API_KEY overlays; no flag): the
				// probe must authenticate exactly as a session would, or a keyed server
				// would be reported unreachable here and perfectly fine in a session. The
				// report states its PRESENCE only; the value never reaches Host.
				APIKey:             opts.apiKey,
				ConfineToWorkspace: opts.confineToWorkspace,
				Residue:            residue,
			})
			// The report is this command's PRODUCT, so it goes to real stdout: Cobra's whole
			// Print/Printf/Println family resolves to OutOrStderr, which would put the entire
			// report on STDERR in every real invocation and leave `apogee probe >host.txt`
			// empty (the fallback differs only when a caller has wired an out writer, which is
			// tests and nothing else). Notices and warnings above keep travelling by PrintErrln,
			// which does target the err stream — the same split runHeadless makes.
			fmt.Fprintln(cmd.OutOrStdout(), host.Report())
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.endpoint, "endpoint", "", "OpenAI-compatible LLM server URL to probe")
	flags.StringVar(&opts.workspace, "workspace", "",
		"workspace root to report (default: current directory)")
	flags.StringVar(&opts.configDir, "config", "",
		"apogee home directory for config/library/sessions (default: ~/.apogee)")

	return cmd
}
