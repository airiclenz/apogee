package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/format"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/skills"
)

// ----------------------------------------------------------------------------
// Exit codes
// ----------------------------------------------------------------------------

// The process exit codes `apogee headless` distinguishes — apogee's first distinct-exit-code
// convention, introduced here deliberately. A script driving an unattended prompt has to tell
// "the model ran and it went wrong" from "the model never got the chance", and a single non-zero
// code cannot: the first is an outcome to read, the second is an invocation to fix. Everything
// else in the binary still exits 0 or 1, which is exactly what the default below preserves.
const (
	// exitRunFailed is a run that STARTED and did not finish: a model or tool failure, a
	// cancellation, or a record that could not be saved. Whatever the run managed is on stdout.
	exitRunFailed = 1
	// exitNotStarted is a run that never began: a usage mistake, a configuration this host
	// cannot honour, or a mode a headless run may not use. Nothing was sent and nothing saved.
	exitNotStarted = 2
)

// exitError carries the process exit code an error asks the binary to end with. RunE returns it
// like any other error — main's error path reads the code back off it — so a deferred teardown
// (the Confiner's Close, above all) still runs; calling os.Exit inside a command body would skip
// every one of them.
type exitError struct {
	code int
	err  error
}

// Error reports the wrapped error's message: the code is for the process, never for the reader.
func (e exitError) Error() string { return e.err.Error() }

// Unwrap exposes the wrapped error so errors.Is/As see straight through the code.
func (e exitError) Unwrap() error { return e.err }

// notStarted marks err as a refusal that stopped the run before it began (exit 2).
func notStarted(err error) error { return exitError{code: exitNotStarted, err: err} }

// runFailed marks err as the failure of a run that had already started (exit 1).
func runFailed(err error) error { return exitError{code: exitRunFailed, err: err} }

// exitCodeFor reports the exit code an error asks for: the code carried by an exitError anywhere
// in its chain, else 1 — the code every command exited with before headless existed, so nothing
// that does not opt in can have its exit status moved by this mechanism.
func exitCodeFor(err error) int {
	var carrier exitError
	if errors.As(err, &carrier) {
		return carrier.code
	}
	return 1
}

// ----------------------------------------------------------------------------
// The headless command
// ----------------------------------------------------------------------------

// runOnce is the seam onto the shared runner. `apogee headless` is a thin CLI over internal/run —
// argument parsing and exit codes, not a second runner (ADR 0033, decision 6) — and this variable
// is the single point a test replaces, so prompt resolution, composition, output routing and exit
// codes are all provable without a live model. Production never reassigns it.
var runOnce = run.Once

// discoverSlots is the seam onto the discovery half of the Parallel agents cap (ADR 0039 decision
// 2): how many generation slots the bound server reports it was launched with, and 0 when it cannot
// say. Like runOnce it exists so the composition below is provable without a live server; production
// never reassigns it.
//
// It is ONE beat of the very Monitor the TUI's heartbeat drives, so an unattended run and a session
// read the same number out of the same `/props` probe rather than growing a second, subtly different
// discovery. One beat and no retry is the whole contract: a headless run composes once and has no
// later beat to widen on, so it asks once and takes what comes.
//
// It never reports an error. A server without /props, an unreachable one, a cancelled context — all
// of them are "nothing observed", which is 0, which ResolveParallelAgents turns into the serial floor
// a run with no signal has always had. Failing a prompt over a number nobody configured would be a
// worse answer than running it one delegation at a time.
var discoverSlots = func(ctx context.Context, endpoint, model, apiKey string) int {
	return heartbeat.NewMonitor(endpoint, model, apiKey).Beat(ctx).TotalSlots
}

// newConfiner is the seam onto the host's confinement backend, for the same reason runOnce is one:
// what a backend can enforce is a property of the MACHINE the test happens to run on — a kernel
// with landlock or without it — and the Auto gate below is a decision about exactly that. A test
// dictates the capability matrix here and asserts the verdict; production never reassigns it.
var newConfiner = platform.NewConfiner

// errHeadlessNoPrompt is the usage refusal when neither the argument nor stdin carries anything.
// A headless run cannot ask what the user meant, so an empty prompt is refused rather than sent.
var errHeadlessNoPrompt = errors.New(
	"apogee headless: no prompt — pass it as an argument (apogee headless \"...\") or pipe it on stdin")

// newHeadlessCommand builds `apogee headless` — one prompt, one unattended run, printed to
// stdout with a meaningful exit code. It is the second Driver over the embeddable engine (ADR
// 0031) and the tripwire that makes a TUI-welded capability visible: everything it needs comes
// from internal/run, and anything it cannot reach from there is a capability that has grown into
// the UI by mistake.
//
// The Firing posture is not this command's to choose — run.Once imposes it (ADR 0033, decision
// 2): a fail-safe denier in place of an Approver, no ask_user and no present_document, and no
// state carried between runs. What is left for the CLI is exactly what a CLI owns: which prompt,
// which binding, which mode, whether to save, and what the shell learns from the exit status.
func newHeadlessCommand() *cobra.Command {
	var opts config.Options
	var noSave bool

	cmd := &cobra.Command{
		Use:   "headless [prompt]",
		Short: "Run one prompt to completion without a UI and print the answer",
		Long: "apogee headless runs a single prompt to completion with nobody watching and\n" +
			"prints the answer to stdout. Give the prompt as the argument, or pipe it on\n" +
			"stdin; an empty prompt is a usage error.\n\n" +
			"The run is unattended, so it never asks: every gated action is refused rather\n" +
			"than parked (the count is reported), ask_user and present_document are not\n" +
			"registered, and no MCP server is contacted. Only two modes make sense here and\n" +
			"only two are accepted — plan (the default, read-only) and auto (confined and\n" +
			"unattended); ask-before and allow-edits both exist to consult a human. Auto is\n" +
			"refused on a host whose confinement backend cannot fence the filesystem: there\n" +
			"the fallback is approval, and there is nobody here to approve.\n\n" +
			"Settings resolve exactly as a session's do — flag over APOGEE_* environment over\n" +
			"config.yaml — so a headless run has the shape a session on this host would have.\n" +
			"The run is saved to ~/.apogee/sessions like any other session and shows up in\n" +
			"/sessions; pass --no-save to run it and record nothing.\n\n" +
			"The answer goes to stdout and everything else to stderr, so a pipeline reads\n" +
			"only the model's text. A run that delegated states each sub-agent's context\n" +
			"fill on a stderr line of its own, ahead of the closing summary — each child\n" +
			"fills a window the run's own figures say nothing about. Exit codes: 0 the run\n" +
			"completed, 1 the run started and failed (model or tool error, cancellation, a\n" +
			"record that would not save), 2 the run never started (usage, configuration, a\n" +
			"refused mode).",
		Args:          headlessArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHeadless(cmd, args, &opts, noSave)
		},
	}

	// A flag Cobra itself rejects — an unknown name, a value it cannot parse — is a usage mistake,
	// and every usage mistake this command makes is exit 2. Cobra parses flags BEFORE it calls RunE,
	// so the body below never sees that error and cannot mark it: this hook is the only point it
	// passes through. Without it `apogee headless --bogus x` would exit 1 and tell a script the run
	// had started and failed, which is the opposite of what happened.
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return notStarted(fmt.Errorf("apogee headless: %w", err))
	})

	flags := cmd.Flags()
	flags.StringVar(&opts.Endpoint, "endpoint", "", "OpenAI-compatible LLM server URL")
	flags.StringVar(&opts.Model, "model", "", "model name to request (default: the configured model)")
	flags.StringVar(&opts.Mode, "mode", string(domain.ModePlan),
		"autonomy mode for the run: plan | auto (an unattended run has nobody to ask)")
	flags.StringVar(&opts.Workspace, "workspace", "",
		"workspace root the file tools are scoped to (default: current directory)")
	flags.StringVar(&opts.ConfigDir, "config", "",
		"apogee home directory for config/library/sessions (default: ~/.apogee)")
	flags.BoolVar(&noSave, "no-save", false,
		"run the prompt and print the answer, but record no session")

	return cmd
}

// headlessArgs is cobra.MaximumNArgs(1) with this command's exit convention attached. The argument
// count is validated by Cobra before RunE runs, exactly like the flags, so the refusal has to carry
// the code out from here or it would leave as a bare error and exit 1. The hint is worth the extra
// clause: an unquoted multi-word prompt is the way this mistake is actually made.
func headlessArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.MaximumNArgs(1)(cmd, args); err != nil {
		return notStarted(fmt.Errorf(
			"apogee headless: %w — the prompt is a single argument, so quote it", err))
	}
	return nil
}

// runHeadless is the command's body: resolve the prompt and the bindings, compose the Config the
// runner is handed, run it once, and route what came back — the answer to stdout, everything else
// to stderr. It is split out of RunE so the whole path is one testable function.
//
// The composition mirrors scheduleWiring.fire, because a headless run and a Firing are the same
// thing reached by different Drivers: the per-model half is resolved through rebindSpecFor (the
// system prompt keys on the model — ADR 0023 — and so does the validated Mechanism set — ADR
// 0016), the delegates that assume a human are left nil for run.Once to pin, and no MCP tools are
// wired at all.
func runHeadless(cmd *cobra.Command, args []string, opts *config.Options, noSave bool) error {
	// Before anything is resolved or constructed: with no prompt there is no run to configure.
	prompt, err := resolveHeadlessPrompt(args, cmd.InOrStdin())
	if err != nil {
		return notStarted(err)
	}

	// The same resolution a session performs (flag > env > file > default), so a headless run
	// talks to the server, and runs with the Mechanisms, a session on this host would.
	if err := config.ApplyConfig(opts, cmd.Flags().Changed, os.Getenv, os.ReadFile, func(msg string) { cmd.PrintErrln(msg) }); err != nil {
		return notStarted(err)
	}
	// This command's own BOTTOM layer is plan. ApplyConfig's is the interactive ladder's
	// ask-before — a mode that consults a human — so leaving it in place would make the bare
	// `apogee headless "..."` a refusal on any host that has not spelled a mode out. An explicit
	// --mode still wins (it is refused below, loudly, if it names a mode with nobody to ask), and
	// so does an APOGEE_MODE or a `mode:` naming plan or auto.
	if !cmd.Flags().Changed("mode") && opts.Mode == string(domain.ModeAskBefore) {
		opts.Mode = string(domain.ModePlan)
	}
	mode, err := domain.ParseMode(opts.Mode)
	if err != nil {
		return notStarted(err)
	}
	// The refusal happens HERE, before a Confiner is built or a model is bound: run.Once's own
	// ErrMode is the library's backstop, and a CLI that let the composition run first would spend
	// the work only to report a decision it could have made from the flag alone.
	if mode != domain.ModePlan && mode != domain.ModeAuto {
		return notStarted(fmt.Errorf(
			"apogee headless: --mode %s consults a human and an unattended run has none "+
				"(use --mode plan or --mode auto)", mode))
	}

	roots, err := resolveRoots(opts.ConfigDir, opts.Workspace)
	if err != nil {
		return notStarted(err)
	}

	// The host's real Confiner backend for this OS, and the teardown the Windows token backend
	// needs to put the disk back (ADR 0020 §2) — the same optional-interface assertion runRoot
	// makes, for the same reason. It is deferred, which is why every failure below travels as a
	// returned error: an os.Exit in this function would leave the labels on the disk.
	confiner := newConfiner()
	if closer, ok := confiner.(interface{ Close() error }); ok {
		defer func() {
			if notice := platform.ConfinementTeardownNotice(closer.Close()); notice != "" {
				cmd.PrintErrln(notice)
			}
		}()
	}

	// Auto's eligibility is ruled on HERE, by the surface that offered the mode (ADR 0033,
	// decision 3) — the same call the `/schedule` picker makes, through the same sentence, because
	// a Firing and a headless run are the same unattended thing reached by different Drivers.
	//
	// It cannot be left to the engine. agent.New refuses Auto only where it can see no filesystem
	// confinement AT ALL, whereas what breaks an unattended run is subtler: on a host that cannot
	// fence, an interactive Auto keeps working because every terminal command falls back to the
	// Approval path, and that is precisely the rung a headless run does not have. Its Approver
	// denies rather than asks (ADR 0033, decision 2), so auto there is a plan run wearing auto's
	// name that fails loudly at every write — after the model has been paid for the attempt. The
	// refusal happens before the Config is composed for that reason: nothing is sent, and exit 2
	// tells the script this is an invocation to fix rather than an outcome to read.
	if mode == domain.ModeAuto {
		if blocked := autoUnattendedBlocked(
			"a headless run", probe.BackendName(confiner), confiner.Capabilities(), opts.ConfineToWorkspace); blocked != "" {
			return notStarted(fmt.Errorf(
				"apogee headless: --mode auto cannot run on this host — %s (use --mode plan, or "+
					"run unconfined with `confine-to-workspace: false` in ~/.apogee/config.yaml, "+
					"which is safe only on a disposable machine)", blocked))
		}
		// The other cell of the ladder: confinement switched OFF by the user's own explicit
		// acknowledgement. That is not blocked — a headless run is never held to a stricter bar
		// than a launch — but it is the one blanket loosen in the system, so it says so, in the
		// launch's own words and on stderr, where it cannot contaminate the answer.
		if !opts.ConfineToWorkspace {
			cmd.PrintErrln(unconfinedAutoWarning)
		}
		// probe.DegradedNotice is deliberately NOT printed here, though runRoot prints it at this
		// point: its cell — auto, confinement asked for, a backend that cannot fence — is exactly
		// the cell refused two branches above, so the notice could never speak, and its remedies
		// (`/confine off`) are slash commands a headless run has no way to type. What the TUI
		// degrades to, this command refuses; the equivalence is pinned by a test rather than left
		// to the reader (headless_test.go, the degraded-cell test).
	}

	// Every `mechanisms:` key is validated here — enabled AND disabled — exactly as startup
	// validates them: the engine only ever sees the enabled IDs, so a typo'd disabled key would
	// otherwise never be reported at all (ADR 0015 §1).
	manualIDs, err := mechanismIDs(opts.Mechanisms, mechanisms.KnownIDs())
	if err != nil {
		return notStarted(err)
	}
	// The per-model half of the Config: the system prompt selected for THIS model, the validated
	// set matched against its fingerprint, and the enable list with the manual-suppresses-a-set
	// rule applied. The observed window is passed as unknown — nothing beats here to observe one —
	// so a `context-window:` pin binds the Budget and an unpinned run leaves it inactive, which
	// for one bounded prompt is the honest degrade rather than a guess. The pin is the bound
	// entry's own over the top-level key (ResolveContextWindow), honoured here for the reason its
	// reply ceiling below is: one configuration, so a headless run budgets against the same window
	// a session on that entry would.
	spec, notices, err := rebindSpecFor(*opts, roots, manualIDs, opts.Model, 0,
		config.ResolveContextWindow(opts.StartupContextWindow, opts.ContextWindow))
	if err != nil {
		return notStarted(err)
	}
	for _, n := range notices {
		cmd.PrintErrln(n)
	}

	// The skill catalog for this run, held in a variable rather than built inline so the SAME
	// provider serves both halves of the skills contract: it resolves an attached ID into the
	// prompt (Config.Skills) AND names the dirs whose files the model may then read
	// (Config.ExtraReadRoots below).
	skillProvider := skills.NewProvider(skills.Sources{
		Home:             roots.config,
		Workspace:        roots.workspace,
		UseProjectSkills: opts.UseProjectSkills,
	})

	cfg := apogee.Config{
		Endpoint:     opts.Endpoint,
		Model:        spec.Model,
		APIKey:       opts.APIKey,
		Mode:         mode,
		Bypass:       opts.Bypass,
		ConfigDir:    roots.config,
		LibraryDir:   roots.library,
		SessionsDir:  roots.sessions,
		WorkspaceDir: roots.workspace,
		// Confiner and posture as the session's, so an Auto run here is fenced by the same box
		// an Auto session would be. Whether this host may run Auto unattended at all is the
		// eligibility gate, which belongs to the surface that offers the mode.
		Confiner:           confiner,
		ConfineToWorkspace: opts.ConfineToWorkspace,
		WebSearchEndpoint:  opts.WebSearchEndpoint,
		// The `tools.disabled:` roster switch, honoured here for the reason every other file-only
		// key is: it is one configuration, and a headless run of it must offer the model the same
		// tools an interactive session would.
		DisabledTools: opts.ToolsDisabled,
		// The Model profile the resolution above matched for THIS model (ADR 0044) — off the spec
		// rather than off opts, so a headless run reads responses in the same shape a session on the
		// same model would, and a built-in match has already narrated itself through the notices.
		Profile:      spec.Profile,
		SystemPrompt: spec.SystemPrompt,
		ContextFiles: opts.ContextFiles,
		Skills:       skillProvider,
		// The same read-only mounts a session gets, for the same reason every other file-only key
		// is honoured here: one configuration, so a headless run's model can read the bundled
		// files of a skill it was given exactly as an interactive one can. Sub-agents inherit them
		// through the tool instances a Subset carries, so no per-child wiring exists.
		ExtraReadRoots:   skillProvider.SourceDirs,
		EnableMechanisms: spec.EnableMechanisms,
		Context: apogee.ContextConfig{
			MaxContextTokens: spec.MaxContextTokens,
			// The bound entry's `max-output-tokens:` pin, honoured here for the reason every other
			// per-entry fact is: one configuration, so a headless run's reply is bounded exactly as
			// a session's is (ADR 0046). Unpinned it stays 0 and the engine derives the cap — which
			// for an unattended run with no window discovered is the clamp floor, deliberately.
			MaxOutputTokens:   opts.StartupMaxOutputTokens,
			CompactionEnabled: opts.AutoCompact,
		},
		// Tools stays nil: a headless run reaches no external MCP server (the Firing posture,
		// ADR 0034), so the engine builds its own registry. Events, Approver, Asker and Presenter
		// stay nil too — run.Once pins its own, and handing it any of them is how a run acquires
		// a human it does not have.
	}

	// How wide this run may fan its delegations out — the same cap a session resolves, installed here
	// so every Driver reaches the same width (ADR 0031's benchable-all-the-way-up; the resolution
	// itself is ADR 0039 decision 2). The pin is the bound entry's own `parallel-agents:`, read back
	// off the startup entry the way every other per-entry fact is; the discovery half is the one-shot
	// probe above, standing in for the beat an unattended run does not have.
	//
	// A pin skips the probe outright. ResolveParallelAgents never lets discovery overrule a pin, so
	// the round trip could not change the answer — it could only spend an unattended run's latency on
	// a question already settled.
	pin := startupEntry(*opts).ParallelAgents
	slots := 0
	if pin < 1 {
		slots = discoverSlots(cmd.Context(), opts.Endpoint, spec.Model, opts.APIKey)
	}
	cfg.ParallelAgents = config.ResolveParallelAgents(pin, slots)

	// The store the record lands in: the shared sessions store, so a headless run is browsable in
	// /sessions beside the conversations it ran beside. --no-save leaves it nil, which is
	// internal/run's own "persist nothing" and leaves Result.SessionID empty.
	var store *session.Store
	if !noSave {
		store = session.NewStore(roots.sessions)
	}

	// Ctrl-C and SIGTERM end the run rather than the process: the cancellation flows out of Once
	// as a run failure carrying whatever the run had reached, so an interrupted run still prints
	// its partial answer and still saves its record.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	res, runErr := runOnce(ctx, run.Spec{Config: cfg, Prompt: prompt, Store: store})

	// A refusal that stopped the Firing before it began is exit 2, not exit 1 — nothing was sent
	// and nothing was saved, so what a script must do about it is fix the invocation, not read an
	// outcome. run.Once marks that class structurally rather than by sentinel: it returns the ZERO
	// Result from each of its three pre-run exits (a mode a Firing may not run, an Agent that would
	// not construct, a prompt it would not accept) and a populated one from every run it actually
	// drove, which always carries at least one Turn. So a non-nil error with no Turn behind it is a
	// run that never started, whatever it travelled out of: Auto on a host with no filesystem
	// confinement, an endpoint no config ever set, a model that would not bind.
	//
	// It returns here, ahead of the answer and the summary: there is no answer, and a "turns: 0"
	// line would report a run that never happened. friendlyConstructErr names the rungs still open
	// for the Auto case and passes every other refusal through untouched.
	if runErr != nil && res.Turns == 0 {
		return notStarted(friendlyConstructErr(runErr))
	}

	// The product, first and alone on stdout — before the summary, before any error — so a
	// pipeline reads the model's text and nothing else, and so a run whose RECORD failed to save
	// still hands over the answer it produced.
	//
	// Written through OutOrStdout rather than cmd.Println: Cobra's whole Print/Printf/Println
	// family resolves to OutOrStderr, so it would put the answer on STDERR in every real
	// invocation (the fallback only differs when a caller has wired an out writer, which is
	// tests and nothing else). The product goes to real stdout; notices and the summary below
	// travel by PrintErrln, which does target the err stream.
	if text := stripEscapes(res.FinalText); text != "" {
		fmt.Fprintln(cmd.OutOrStdout(), text)
	}
	// Each delegated run's own context fill, one line apiece and ahead of the summary: the summary
	// speaks for the Firing as a whole, and a sub-agent fills a window of its OWN, which no
	// top-level figure stands in for. A run that delegated nothing prints none of these lines.
	for _, line := range headlessSubAgentLines(res.SubAgents) {
		cmd.PrintErrln(line)
	}
	// What the run SPENT, beside what it filled: the fill lines above say how full each window
	// ended, these say how many tokens got it there — the Firing's own totals first, then one
	// line per delegated run that accounted for anything. A run whose Upstream reported no usage
	// prints none of them.
	for _, line := range headlessUsageLines(res) {
		cmd.PrintErrln(line)
	}
	cmd.PrintErrln(headlessSummary(res))

	if runErr != nil {
		// A failed run still reports what it salvaged: run.Once saves whatever completed before
		// it stopped, and naming that record is what lets a human open the interrupted run rather
		// than guess at it (the wording scheduleWiring.fire uses for the same partial).
		if res.SessionID != "" {
			return runFailed(fmt.Errorf("%w (partial run saved as %s)", runErr, res.SessionID))
		}
		return runFailed(runErr)
	}
	return nil
}

// resolveHeadlessPrompt reads the run's single prompt: the positional argument when there is one,
// else all of stdin. The result is trimmed — a heredoc's trailing newline is not part of what the
// user asked — and an empty result is a usage error rather than an empty request to the model.
func resolveHeadlessPrompt(args []string, stdin io.Reader) (string, error) {
	raw := ""
	if len(args) > 0 {
		raw = args[0]
	} else {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("apogee headless: read the prompt from stdin: %w", err)
		}
		raw = string(data)
	}
	prompt := strings.TrimSpace(raw)
	if prompt == "" {
		return "", errHeadlessNoPrompt
	}
	return prompt, nil
}

// headlessSummary is the one line a headless run says about itself, on stderr beside the answer:
// where the record landed, how many Turns it took, and how many gated actions the fail-safe
// denier refused. The denial count is reported rather than promoted to an exit code — a plan run
// that reached for a write did its job and said so — and the record segment is omitted when there
// is no record, which is both --no-save and a save that failed.
func headlessSummary(res run.Result) string {
	stats := fmt.Sprintf("turns: %d · denied: %d", res.Turns, res.Denied)
	if res.SessionID == "" {
		return stats
	}
	return "session: " + res.SessionID + " · " + stats
}

// headlessSubAgentLines renders what each delegated run did to its own context: one line per
// finished sub-agent run, in the order the runs finished, stating how full that run's window got
// and which delegation it was. It is the headless twin of the reading the TUI paints on a collapsed
// sub-agent block — the same fill, from the same events, on the Driver that has no block to paint.
//
// A run whose reading or whose window is zero is omitted rather than spelled against nothing (the
// TUI cell's rule, and the gauge's before it): a fill only means something beside its limit.
//
// A run that went to a DIFFERENT model than the session's — a delegation routed to the Sub-agent
// server (ADR 0045) — closes the line with that model. It is the first thing a reader needs when a
// delegation behaves unlike the session that asked for it, and it is silent otherwise: routing off,
// or a target bound to the same model, prints exactly the line this Driver always printed. The
// field is already the answer to "does it differ" (run.SubAgentUsage.Model), so nothing here holds
// the session's model to compare against; what happens here is the escape strip every wire-sourced
// cell on this line gets, a server-reported id being no more trusted than a model-written name.
func headlessSubAgentLines(runs []run.SubAgentUsage) []string {
	lines := make([]string, 0, len(runs))
	for _, r := range runs {
		if r.Used <= 0 || r.Limit <= 0 {
			continue
		}
		line := "sub-agent: " + format.Tokens(r.Used) + "/" + format.Tokens(r.Limit)
		if who := headlessSubAgentTarget(r); who != "" {
			line += " · " + who
		}
		if model := clipSubAgentTask(stripEscapesToLine(r.Model)); model != "" {
			line += " · " + model
		}
		lines = append(lines, line)
	}
	return lines
}

// headlessUsageLines renders what the run SPENT: the Firing's own cumulative totals on one line,
// then one line per delegated run that accounted for anything, in the order the runs finished. It
// is the headless twin of the /usage popup — the same per-agent totals, from the same events, on
// the Driver that has no popup to open — and it deliberately does NOT print a session total: the
// lines are the addends, and a script that wants the sum can take it without this parser inventing
// a row for it.
//
// A grain that counted no call is omitted rather than printed as four zeros, which is the fill
// lines' self-hiding rule applied to the reading this one carries: an Upstream that reports no
// usage leaves a run with nothing to say, not with a spend of zero. Sub-agent lines are named by
// headlessSubAgentTarget, so a child is named here exactly as it is named a few lines above.
func headlessUsageLines(res run.Result) []string {
	lines := make([]string, 0, 1+len(res.SubAgents))
	if line := headlessUsageLine(res.Usage, ""); line != "" {
		lines = append(lines, line)
	}
	for _, r := range res.SubAgents {
		usage := run.Usage{
			Calls:            r.Calls,
			PromptTokens:     r.PromptTokens,
			CompletionTokens: r.CompletionTokens,
			TotalTokens:      r.TotalTokens,
		}
		if line := headlessUsageLine(usage, headlessSubAgentTarget(r)); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// headlessUsageLine spells one agent's cumulative totals, "" when that agent accounted for no
// call at all. The counts go through format.Tokens like every other reading the binary prints, so
// a spend reads in the same units as the fill beside it; who is the delegation label the line ends
// with, empty for the Firing's own totals, which need no label because they are the run's.
func headlessUsageLine(u run.Usage, who string) string {
	if u.Calls <= 0 {
		return ""
	}
	line := fmt.Sprintf("usage: calls %d · prompt %s · completion %s · total %s",
		u.Calls, headlessTokens(u.PromptTokens), headlessTokens(u.CompletionTokens), headlessTokens(u.TotalTokens))
	if who != "" {
		line += " · " + who
	}
	return line
}

// headlessTokens is format.Tokens with a spelling for zero. The shared formatter renders a
// non-positive count as the EMPTY string, because everywhere else in the binary a zero reading is
// one to hide; on a usage line the counter is a labelled column that the line has already earned by
// counting a call, so an absent number would leave "total ·" hanging rather than say what it means.
// A server that reports the two parts and omits the sum is exactly that case (run.Usage.TotalTokens).
func headlessTokens(n int) string {
	if text := format.Tokens(n); text != "" {
		return text
	}
	return "0"
}

// headlessSubAgentTarget says WHICH delegation a sub-agent line is reporting on: the short name the
// call gave it, falling back to the delegated task's first line when it gave none — which is every
// delegation written before the name argument existed, and every one a Mechanism synthesises. That
// is the collapsed run header's rule (subAgentTarget, internal/tui) kept on the Driver that has no
// header to paint, so the two twins name a child the same way rather than in two dialects.
//
// Both spellings get the same treatment, because run.SubAgentUsage hands both over as raw model
// output on the same terms as the answer: stripped of control characters HERE, at this render seam,
// in the line-safe form — so neither can rewind or re-column the reading it sits beside — and
// clipped, so a model that "named" a delegation with a screenful of instructions cannot take the
// terminal over with one line. The fallback is decided on the RENDERED form, so a name that is
// nothing but control characters leaves the task showing rather than blanking the slot.
func headlessSubAgentTarget(r run.SubAgentUsage) string {
	if name := clipSubAgentTask(stripEscapesToLine(r.Name)); name != "" {
		return name
	}
	return clipSubAgentTask(stripEscapesToLine(r.Task))
}

// headlessTaskMax is how wide a delegated task prints on a sub-agent line, in runes: enough for a
// real instruction to be recognisable, little enough that the reading it follows stays the line's
// point. Runes rather than bytes, so the cap does not vary with the alphabet the task is in.
const headlessTaskMax = 80

// clipSubAgentTask cuts a task label to headlessTaskMax runes, ellipsis included in the cap, so a
// clipped label is never wider than an unclipped one. It never splits a rune: the cut is made on
// the decoded slice, not on the bytes. A delegation's name is spent from the same budget
// (headlessSubAgentTarget): it stands in the same slot, and a name is not licence to be wider.
func clipSubAgentTask(task string) string {
	runes := []rune(task)
	if len(runes) <= headlessTaskMax {
		return task
	}
	return string(runes[:headlessTaskMax-1]) + "…"
}

// stripEscapes drops the C0 control characters from the model's answer before it is printed,
// keeping the two that ordinary prose is written with: the newline and the tab. Result.FinalText is
// RAW model output by contract (internal/run: the answer crosses as plain data, ADR 0010), and a
// control character there is an instruction to the terminal rather than a character in the answer —
// ESC opens an ANSI sequence, BEL rings the bell and closes an OSC 52 clipboard payload, CR rewinds
// the line so what follows overwrites what the reader already saw. Dropping the whole class
// neutralises all three, where dropping ESC alone left the other two to arrive intact. stdout is a
// terminal often enough that the strip belongs at this render seam, exactly as the transcript
// strips at its own.
//
// It began as a copy of internal/tui's identically named helper and widened ahead of it for a
// while; that twin has since caught up, and the two drop the same class and keep the same two
// characters again. The duplication stands for the reason it always did — that one is unexported
// in a package the binary's CLI half must not depend on, and this is a few lines of pure function
// against a one-way dependency.
func stripEscapes(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r // the answer's own line and column structure, which it is printed for
		}
		return dropControl(r)
	}, s)
}

// stripEscapesToLine is stripEscapes for text that must stay on ONE line — a sub-agent's task
// label, printed beside the reading it belongs to — where the two controls the answer keeps would
// forge a second line or a false column. They fold to a space rather than surviving; every other
// control character goes exactly where stripEscapes sends it.
func stripEscapesToLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		return dropControl(r)
	}, s)
}

// dropControl maps a C0 control character — and DEL, its ASCII sibling — to strings.Map's "drop
// this rune", and passes every other rune through untouched. Text carrying none of them maps to
// itself, which strings.Map returns without allocating: the overwhelmingly common case.
func dropControl(r rune) rune {
	if r < 0x20 || r == 0x7f {
		return -1
	}
	return r
}
