package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

// errProbeContextLiveNeedsModel is the refusal when --live has no model to send as: the startup
// entry names none, --model was not passed, and the server's own listing names no active model —
// the same absence probe model refuses on (errProbeModelNeedsLabel), for the same reason: the
// request has nothing to carry as its model id.
var errProbeContextLiveNeedsModel = errors.New(
	"apogee probe context --live: no model to send as — the server names no active model; pass " +
		"--model or set model: on the startup entry")

// The fixed Turn-1 request `--live` sends: one short instruction, and a reply ceiling that keeps
// the answer to a handful of tokens. The prompt is constant so two live probes measure the same
// request and differ only in what apogee put in front of it; the cap bounds what the probe can
// spend on a model that ignores the instruction.
const (
	probeContextLivePrompt   = "Reply with the single word OK."
	probeContextLiveReplyCap = 8
)

// probeContextCommand builds `apogee probe context` — the Turn-1 context cost (ADR 0079): what
// apogee itself puts in front of the model before the user's first message, piece by piece, as an
// offline estimate. It sits on the FREE side of the probe split (ADR 0021 §1): an Agent is
// constructed exactly as a headless run in the same mode would construct one, its ContextCost is
// read while it is idle, and it is closed — no request is sent, no record is written, no scratch
// dir is created. `--live` is the PAID side (ADR 0021 §4): the same Agent Submits one fixed
// one-word request and Steps until the server's Turn-1 usage arrives, so the report can carry the
// server's own prompt_tokens beside the estimate — see measureContextCost.
//
// The composition is the unattended composer's own server-bound half (bindFiringConfig,
// wire_firing.go) rather than a third assembly of the Config: the number this prints is only worth
// charting if it is the number a `headless --mode <mode>` run reports on `run_finished.context_cost`,
// and one composition is how the two stay one number. What firingConfig adds on top — the beat, the
// scratch mkdir, the routing — is exactly what an offline estimate must not do, which is where the
// split between the two functions falls.
func probeContextCommand() *cobra.Command {
	var opts config.Options
	var live bool

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
			"The same number a headless run in the same mode reports on run_finished.context_cost.\n\n" +
			"--live sends one fixed one-word request (\"Reply with the single word OK.\", reply\n" +
			"capped at 8 tokens) and adds a measured column: the server's own prompt_tokens for\n" +
			"that Turn 1, with the estimate rows re-rendered through the ratio it calibrated. When\n" +
			"advise or shape Reactions are armed it sends twice — as configured, then under Bypass —\n" +
			"and prints both columns with the tokens they add. It spends tokens; it still writes\n" +
			"nothing.",
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

			cfg, err := probeContextConfig(opts, roots, mode, probeContextOverlay{})
			if err != nil {
				return err
			}

			var report probe.ContextCost
			if live {
				// Said BEFORE the first call, per ADR 0021 §4: a command that spends tokens
				// announces it in advance, not in its epilogue.
				cmd.PrintErrln(probeContextLivePreamble)
				report, err = measureContextCost(cmd.Context(), cfg, opts, roots, mode)
			} else {
				report, err = estimateContextCost(cfg, opts, mode)
			}
			if err != nil {
				return err
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
	flags.StringVar(&opts.Endpoint, "endpoint", "",
		"OpenAI-compatible LLM server URL to compose against (nothing is sent without --live)")
	flags.StringVar(&opts.Model, "model", "",
		"model to compose for (default: the startup server's model:)")
	flags.StringVar(&opts.Mode, "mode", string(domain.ModeAskBefore),
		"mode to compose under: plan | ask-before | allow-edits | auto (default: the startup mode)")
	flags.StringVar(&opts.Workspace, "workspace", "",
		"workspace root to compose for (default: current directory)")
	flags.StringVar(&opts.ConfigDir, "config", "",
		"apogee home directory for config/library/sessions (default: ~/.apogee)")
	flags.BoolVar(&live, "live", false,
		"send one fixed one-word request and add the server's measured prompt tokens (spends tokens)")

	return cmd
}

// probeContextLivePreamble is what --live says on stderr before its first call: that tokens are
// about to be spent, and on what.
const probeContextLivePreamble = "apogee probe context --live: sending one fixed one-word request to " +
	"measure Turn 1 (this spends tokens; nothing is written)"

// estimateContextCost is the offline half: an idle Agent's report through the default ratio, with
// the armed count for the trailing line.
func estimateContextCost(cfg apogee.Config, opts config.Options, mode domain.Mode) (probe.ContextCost, error) {
	cost, err := readContextCost(cfg)
	if err != nil {
		return probe.ContextCost{}, err
	}
	return probe.ContextCost{
		Estimate: cost,
		Mode:     string(mode),
		// The ratio an idle Agent estimates through: it has sent nothing, so nothing has
		// calibrated it away from the default (internal/context).
		CharsPerToken: apogeectx.DefaultCharsPerToken,
		Armed:         armedDirectiveReactions(opts.Reactions),
	}, nil
}

// measureContextCost is the live half: it sends the fixed Turn-1 request and reads what the server
// counted. When the configuration arms advise/shape Reactions it sends twice — once with the sync
// lane armed exactly as run.Once arms it, once on a fresh Agent under Bypass with the lane left
// unarmed — and the report carries both readings; otherwise once, as one `measured` column. The
// estimate rows are the first reading's Agent's, re-rendered through the ratio that reading
// calibrated.
//
// A bound entry that names no model is resolved the way probe model resolves its label: the
// server's own listing names the active model, and the Config is recomposed on it so the prompt and
// the profile key on the model a session would bind (ADR 0023, ADR 0044). The recompose reuses the
// key the first composition resolved, so an `api-key-cmd:` runs once.
func measureContextCost(ctx context.Context, cfg apogee.Config, opts config.Options, roots stateRoots, mode domain.Mode) (probe.ContextCost, error) {
	if cfg.Model == "" {
		info, err := provider.NewClient(cfg.Endpoint, "",
			provider.WithAPIKey(cfg.APIKey), provider.WithWire(provider.WireFor(cfg.Wire))).Discover(ctx)
		if err != nil {
			return probe.ContextCost{}, err
		}
		if info.ActiveModel == "" {
			return probe.ContextCost{}, errProbeContextLiveNeedsModel
		}
		cfg, err = probeContextConfig(opts, roots, mode, probeContextOverlay{model: info.ActiveModel, apiKey: cfg.APIKey})
		if err != nil {
			return probe.ContextCost{}, err
		}
	}

	_, sync := domain.SplitLanes(opts.Reactions)
	armed := armedDirectiveReactions(opts.Reactions)
	first, err := measureTurn1(ctx, cfg, sync, false)
	if err != nil {
		return probe.ContextCost{}, err
	}
	report := probe.ContextCost{
		Estimate: first.cost,
		Mode:     string(mode),
		// The calibrated ratio, read back off the report the calibrated Agent produced: the
		// estimator's own value is not on the public surface, and the total's bytes over its
		// tokens is that ratio to the rounding the ceiling introduces.
		CharsPerToken: calibratedRatio(first.cost),
		Armed:         armed,
		Measured:      []probe.ContextCostMeasured{first.column(probe.ContextCostColumnMeasured)},
	}
	if armed == 0 {
		return report, nil
	}
	second, err := measureTurn1(ctx, cfg, nil, true)
	if err != nil {
		return probe.ContextCost{}, err
	}
	report.Measured = []probe.ContextCostMeasured{
		first.column(probe.ContextCostColumnAsConfigured),
		second.column(probe.ContextCostColumnBypass),
	}
	return report, nil
}

// calibratedRatio derives the chars→token ratio a calibrated report went through from its total:
// one estimate over the sum, so bytes over tokens is the ratio to within the ceiling's rounding.
// A report with no tokens (nothing standing, no menu) reports the default.
func calibratedRatio(cost domain.ContextCost) float64 {
	if cost.Tokens <= 0 {
		return apogeectx.DefaultCharsPerToken
	}
	return float64(cost.Bytes) / float64(cost.Tokens)
}

// turn1Reading is one live measurement: the server's per-call counts for the Turn-1 request and
// the Agent's report re-read after the reply calibrated its estimator.
type turn1Reading struct {
	cost   domain.ContextCost
	prompt int
	cached int
}

// column is the reading as the report's measured column under label.
func (r turn1Reading) column(label string) probe.ContextCostMeasured {
	return probe.ContextCostMeasured{Label: label, PromptTokens: r.prompt, CachedPromptTokens: r.cached}
}

// measureTurn1 constructs a fresh Agent on cfg, arms it, Submits the fixed prompt and Steps ONCE:
// the Turn ends on the server's Turn-1 usage. What keeps a tool the model might call from running
// is the ctx cancel, not the mode: turn1Sink cancels the ctx synchronously on the first Depth-0
// UsageEvent, which streamResponse emits right after Calibrate and BEFORE respondAndReview's
// ctx.Err() check that precedes any dispatch (internal/agent/loop.go), so the Turn is cancelled
// before a call is executed. Plan would not do it — Plan runs read-only tools without the Approver
// and permits scratch writes once ScratchDir is set — and the deny-all Approver the Config carries
// is belt-and-braces only.
//
// The sync lane is armed as run.Once arms it (internal/run/run.go): Generation read, Sync set,
// handed back through SetReactions, so the Floor enable set and Bypass agent.New seeded stay as
// seeded; an empty lane makes no swap. Under bypass the lane stays unarmed and Bypass is switched
// on through the same read-edit-hand-back — liveSettings.setBypass's shape — so the second reading
// is the first with the model-shaping classes off (ADR 0076 D9) and nothing else moved.
//
// The reply ceiling is pinned on the Config: newProjection stamps the loop's own cap on every
// request, and Context.MaxOutputTokens is the operator's pin for it.
func measureTurn1(ctx context.Context, cfg apogee.Config, sync []domain.Reaction, bypass bool) (turn1Reading, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sink := &turn1Sink{cancel: cancel}
	cfg.Events = sink
	cfg.Context.MaxOutputTokens = probeContextLiveReplyCap

	a, err := apogee.New(cfg)
	if err != nil {
		return turn1Reading{}, fmt.Errorf("apogee probe context --live: construct the agent: %w", err)
	}
	defer func() { _ = a.Close() }()

	if bypass || len(sync) > 0 {
		gen := a.Generation()
		if bypass {
			gen.Bypass = true
		} else {
			gen.Sync = sync
		}
		if err := a.SetReactions(gen); err != nil {
			return turn1Reading{}, fmt.Errorf("apogee probe context --live: arm the sync lane: %w", err)
		}
	}

	if err := a.Submit(apogee.UserInput{Text: probeContextLivePrompt}); err != nil {
		return turn1Reading{}, fmt.Errorf("apogee probe context --live: %w", err)
	}
	if _, err := a.Step(ctx); err != nil {
		return turn1Reading{}, fmt.Errorf("apogee probe context --live: %w", err)
	}
	usage, ok := sink.reading()
	if !ok {
		msg := "apogee probe context --live: the server reported no usage on the Turn-1 reply, so there is nothing to measure"
		if fault := a.LastFault(); fault != "" {
			msg += ": " + fault
		}
		return turn1Reading{}, errors.New(msg)
	}
	return turn1Reading{cost: a.ContextCost(), prompt: usage.PromptTokens, cached: usage.CachedPromptTokens}, nil
}

// turn1Sink is the EventSink a live measurement runs under: it keeps the first Depth-0,
// non-maintenance UsageEvent — the Turn-1 reading — and cancels the Step's ctx the moment it
// arrives, synchronously inside the emit, so the Turn ends before the loop can dispatch anything
// the reply asked for. Every other event is dropped.
type turn1Sink struct {
	cancel context.CancelFunc

	mu    sync.Mutex
	usage domain.UsageEvent
	seen  bool
}

// Emit latches the Turn-1 usage and cancels the run on it.
func (s *turn1Sink) Emit(ev domain.Event) {
	u, ok := ev.(domain.UsageEvent)
	if !ok || u.Depth != 0 || u.Maintenance {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen {
		return
	}
	s.usage, s.seen = u, true
	s.cancel()
}

// reading returns the latched Turn-1 usage, and whether one arrived.
func (s *turn1Sink) reading() (domain.UsageEvent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usage, s.seen
}

// probeContextOverlay is what the live path hands the composer on a recompose: the model the
// server's listing named, and the key the first composition already resolved.
type probeContextOverlay struct {
	model  string
	apiKey string
}

// probeContextConfig composes the Config the estimate is read off: the unattended composer's
// server-bound half over the startup entry, under mode — the overlay's model and key when the live
// path recomposes on a discovered model, the entry's own otherwise — with the two live delegates an idle Agent
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
func probeContextConfig(opts config.Options, roots stateRoots, mode domain.Mode, overlay probeContextOverlay) (apogee.Config, error) {
	bound, err := bindFiringConfig(firingInputs{
		opts:     opts,
		entry:    opts.StartupEntry,
		roots:    roots,
		confiner: platform.NewDenyConfiner(),
		mode:     mode,
		model:    overlay.model,
		apiKey:   overlay.apiKey,
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

// denyApprover refuses every Approval. The offline probe never Steps, so no gate is ever reached,
// and the live probe's Turn is cancelled before any dispatch (measureTurn1); it is pinned so that
// an Agent composed here can never acquire a human, whatever a later change to the Config's
// defaults might hand it.
type denyApprover struct{}

// Approve refuses the call.
func (denyApprover) Approve(context.Context, domain.ApprovalRequest) (domain.ApprovalDecision, error) {
	return domain.ApprovalDeny, nil
}

// discardEvents is the EventSink construction requires (agent.New refuses a nil one), for the
// offline Agent, which emits nothing because it never runs; the live one runs under turn1Sink.
type discardEvents struct{}

// Emit drops the event.
func (discardEvents) Emit(domain.Event) {}
