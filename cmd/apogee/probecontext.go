package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/sanitize"
	"github.com/airiclenz/apogee/internal/session"
)

// errProbeContextNeedsEndpoint is the refusal when resolution left this command with no server to
// bind to. Selection itself refuses first since ADR 0036 — a config that names no startup server
// never gets past ApplyConfig — so this is the belt-and-braces answer, kept because the Agent the
// estimate is read off is constructed against an endpoint (agent.New requires one) even though
// this command never sends to it.
var errProbeContextNeedsEndpoint = errors.New(
	"apogee probe context: no server to compose against — name the servers: entry with server: in " +
		"config.yaml or APOGEE_SERVER, or pass --endpoint (nothing is sent to it)")

// probeContextCommand builds `apogee probe context` — the Turn-1 context cost (ADR 0079): what
// apogee itself puts in front of the model before the user's first message, piece by piece, as an
// offline estimate. It sits on the FREE side of the probe split (ADR 0021 §1): an Agent is
// constructed exactly as a headless run in the same mode would construct one, its ContextCost is
// read while it is idle, and it is closed — no request is sent, no record is written, no scratch
// dir is created.
//
// The composition is the unattended composer's own server-bound half (bindFiringConfig,
// wire_firing.go) rather than a third assembly of the Config: the number this prints is only worth
// charting if it is the number a `headless --mode <mode>` run reports on `run_finished.context_cost`,
// and one composition is how the two stay one number. What firingConfig adds on top — the beat, the
// scratch mkdir, the routing — is exactly what an offline estimate must not do, which is where the
// split between the two functions falls.
func probeContextCommand() *cobra.Command {
	var opts config.Options

	cmd := &cobra.Command{
		Use:   "context",
		Short: "Estimate what apogee puts in front of the model at Turn 1, piece by piece",
		Long: "apogee probe context reports the Turn-1 context cost: the bytes and the estimated\n" +
			"tokens of everything apogee itself sends before your first message — the system\n" +
			"prompt, the orientation block, the workspace context files, the tool menu — one row\n" +
			"per piece and a total, composed under the mode a session would start in.\n\n" +
			"It is an estimate: the token column goes through the default chars-per-token ratio\n" +
			"and is marked with ~. Nothing is sent to the server and nothing is written, so it\n" +
			"costs no tokens. Advise and shape Reactions add their directives only once a request\n" +
			"is in flight, so an armed one is named below the table rather than counted in it.\n\n" +
			"The same number a headless run in the same mode reports on run_finished.context_cost.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The same resolution a session performs (flag > env > file > default), so the
			// estimate is composed over the configuration a session on this host would run with.
			// It reads only; the host half's no-seeding rule holds here too.
			if err := config.ApplyConfig(&opts, cmd.Flags().Changed, os.Getenv, os.ReadFile, func(msg string) { cmd.PrintErrln(msg) }); err != nil {
				return err
			}
			// The mode the table is composed under — the resolved startup mode unless --mode
			// says otherwise. It matters: Plan filters the menu (toolMenu, internal/agent) and
			// adds a bullet to the orientation, so a Plan table and an Auto table differ, and the
			// header names which one this is.
			mode, err := domain.ParseMode(opts.Mode)
			if err != nil {
				return err
			}
			roots, err := resolveRoots(opts.ConfigDir, opts.Workspace)
			if err != nil {
				return err
			}
			if opts.Endpoint == "" {
				return errProbeContextNeedsEndpoint
			}

			cfg, err := probeContextConfig(opts, roots, mode)
			if err != nil {
				return err
			}
			cost, err := readContextCost(cfg)
			if err != nil {
				return err
			}

			report := probe.ContextCost{
				Estimate: cost,
				Mode:     string(mode),
				// The ratio an idle Agent estimates through: it has sent nothing, so nothing
				// has calibrated it away from the default (internal/context).
				CharsPerToken: apogeectx.DefaultCharsPerToken,
				Armed:         armedDirectiveReactions(opts.Reactions),
			}
			// The report is this command's PRODUCT, so it goes to real stdout, exactly as the
			// host half's does (probe.go carries the reasoning), escape-stripped on the way out:
			// the table quotes nothing from a server, but the strip is the render seam's rule
			// for every probe report rather than a per-report judgment.
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), sanitize.StripEscapes(report.Report()))
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Endpoint, "endpoint", "", "OpenAI-compatible LLM server URL to compose against (nothing is sent)")
	flags.StringVar(&opts.Model, "model", "",
		"model to compose for (default: the startup server's model:)")
	flags.StringVar(&opts.Mode, "mode", string(domain.ModeAskBefore),
		"mode to compose under: plan | ask-before | allow-edits | auto (default: the startup mode)")
	flags.StringVar(&opts.Workspace, "workspace", "",
		"workspace root to compose for (default: current directory)")
	flags.StringVar(&opts.ConfigDir, "config", "",
		"apogee home directory for config/library/sessions (default: ~/.apogee)")

	return cmd
}

// probeContextConfig composes the Config the estimate is read off: the unattended composer's
// server-bound half over the startup entry, under mode, with the two live delegates an idle Agent
// still needs pinned to stand-ins that can never act — an Approver that denies and a Confiner that
// enforces nothing — and the keys firingConfig would take from its beat and its scratch mkdir filled
// without either.
//
// The scratch dir is the path a run would be handed, MINTED but never created: the orientation
// block names it, so a Config with none would estimate a shorter orientation than any session
// sends. The fan-out width is the entry's pin or 1, the value a beat that saw one slot would
// resolve; the effort dialect is the entry's forced one or none — neither reaches the Turn-1 bytes,
// and both are set so the Config reads as a Firing's would. The toolchain probe is WAITED on
// (hostToolchain.wait) rather than read live: the orientation lists the toolchain roots once the
// probe has answered, and a table taken a few milliseconds before that would print a number no
// session ever sends.
func probeContextConfig(opts config.Options, roots stateRoots, mode domain.Mode) (apogee.Config, error) {
	bound, err := bindFiringConfig(firingInputs{
		opts:     opts,
		entry:    opts.StartupEntry,
		roots:    roots,
		confiner: platform.NewDenyConfiner(),
		mode:     mode,
	})
	if err != nil {
		return apogee.Config{}, err
	}
	cfg := bound.cfg
	cfg.Approver = denyApprover{}
	cfg.Events = discardEvents{}
	scratchDir := filepath.Join(roots.scratch, session.NewID(time.Now()))
	cfg.ScratchDir = scratchDir
	cfg.ScratchReadRoot = func() string { return scratchDir }
	cfg.ParallelAgents = config.ResolveParallelAgents(opts.StartupEntry.ParallelAgents, 0)
	cfg.EffortDialect = domain.EffortDialect(provider.EffortDialectFor(opts.StartupEntry.EffortDialect))
	hostToolchain.wait()
	return cfg, nil
}

// readContextCost constructs the Agent through the public surface — the ordinary provider client
// bound to the configured endpoint, which does not dial at construction — takes the idle report,
// and closes the Agent. It never Steps, so nothing reaches the wire.
func readContextCost(cfg apogee.Config) (domain.ContextCost, error) {
	a, err := apogee.New(cfg)
	if err != nil {
		return domain.ContextCost{}, fmt.Errorf("apogee probe context: construct the agent: %w", err)
	}
	defer func() { _ = a.Close() }()
	return a.ContextCost(), nil
}

// armedDirectiveReactions counts the configured Reactions whose class puts text in front of the
// model — advise, and the two shape classes — the ones an idle estimate cannot see because their
// directives exist only once a request is in flight. Observe and gate Reactions change nothing the
// model reads and are not counted.
func armedDirectiveReactions(list []domain.Reaction) int {
	n := 0
	for _, r := range list {
		switch r.Class {
		case domain.ClassAdvise, domain.ClassShapeView, domain.ClassShapeWork:
			n++
		}
	}
	return n
}

// denyApprover refuses every Approval. The offline probe never Steps, so no gate is ever reached;
// it is pinned so that an Agent composed here can never acquire a human, whatever a later change
// to the Config's defaults might hand it.
type denyApprover struct{}

// Approve refuses the call.
func (denyApprover) Approve(context.Context, domain.ApprovalRequest) (domain.ApprovalDecision, error) {
	return domain.ApprovalDeny, nil
}

// discardEvents is the EventSink construction requires (agent.New refuses a nil one), for an Agent
// that emits nothing because it never runs.
type discardEvents struct{}

// Emit drops the event.
func (discardEvents) Emit(domain.Event) {}
