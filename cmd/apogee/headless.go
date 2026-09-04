package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/format"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/notice"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/sanitize"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/title"
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
	// exitRunFaulted is a run that started and reached its boundary, but whose final Turn the
	// engine ABANDONED: whatever is on stdout is the run's last words, not its answer. It is a
	// third thing a script must tell apart — the run did not fail (its record saved, its Turns
	// did their work) and it did not answer either, so neither 0 nor 1 says what happened.
	exitRunFaulted = 3
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

// prewarmLabelWalk is the seam onto the Windows label-walk pre-warm, for the same reason runOnce
// and newConfiner are seams: platform.PrewarmLabelWalk is an empty function off Windows
// (internal/platform/prewarm_other.go), so a test that only asserted "the run made no noise" would
// pass identically against a tree that never calls it at all. Replacing this variable is how the
// suite proves the confined-Auto headless path reaches the pre-warm on the one host where it does
// work. Production never reassigns it.
var prewarmLabelWalk = platform.PrewarmLabelWalk

// pruneNoticeSink is the headless Driver's own EventSink: it prints one stderr line per
// [domain.PruneEvent] and forwards every Event, its own included, to whatever sink it wraps
// (nil ⇒ nothing to forward to). A zero-value inner is the normal case — a bare headless run
// composes no sink of its own — and the wrap still matters, because run.Once installs its tap
// AROUND this one rather than instead of it (run.Spec).
//
// It lives here, in the command, rather than in internal/run's eventTap, because internal/run is
// also the DAEMON's Firing path (daemonfire.go): a print there would put a line on a daemon's
// stderr on every Firing, for a human who is not watching. The record keeps the same fact either
// way — transcriptFold folds a Note entry for it — so this sink is the live view alone.
//
// It prints for a prune and for NOTHING else — a [domain.FloorGuardEvent] included, and
// deliberately: a Floor guard repairing the model's own failure is engine behaviour rather than
// news for the human who is not watching, so it stays in the TUI's hidden debug view and off this
// stderr, exactly as a Mechanism firing already does (ADR 0071). It is still forwarded, like every
// other Event.
//
// Emit is never called concurrently: the engine serializes emission on its side ([domain.EventSink]).
type pruneNoticeSink struct {
	inner domain.EventSink
	out   io.Writer
}

// Emit prints the prune notice, then forwards. The two numbers are rendered verbatim, worded as
// every other Driver words them (internal/tui's transcript.addPrune, internal/run's
// transcriptFold.fold), so one pruning pass reads the same on a terminal, in a session record and
// in a scrollback.
func (s pruneNoticeSink) Emit(e domain.Event) {
	if pe, ok := e.(domain.PruneEvent); ok {
		_, _ = fmt.Fprintf(s.out, "pruned %d tool results (~%d tokens)\n", pe.Results, pe.Tokens)
	}
	if s.inner != nil {
		s.inner.Emit(e)
	}
}

// discoverBeat is the seam onto the ONE observation an unattended run takes of the server it is
// bound to: the whole Beat, because everything the composition needs from discovery comes off it —
// how many generation slots the server reports it was launched with (ADR 0039 decision 2), which
// wire shape it reads a thinking-effort intent in (ADR 0060), and whether it answered at all. Like
// runOnce it exists so the composition is provable without a live server; production never
// reassigns it.
//
// It is ONE beat of the very Monitor the TUI's heartbeat drives, so an unattended run and a session
// read the same numbers out of the same probes rather than growing a second, subtly different
// discovery. One beat and no retry is the whole contract: a headless run composes once and has no
// later beat to widen on, so it asks once and takes what comes.
//
// It never reports an error, and it replaced two probes that each asked the same server the same
// question at the same moment: a server without /props, an unreachable one, a cancelled context all
// answer the zero Beat, whose slot count ResolveParallelAgents turns into the serial floor a run
// with no signal has always had and whose dialect is the historical `chat_template_kwargs` shape
// every unattended run spoke before the seam existed. What the failure MEANS is on the Beat itself
// (Failure, Answered, Throttled) for the Driver that gates on it.
var discoverBeat = func(ctx context.Context, endpoint, model, apiKey string) heartbeat.Beat {
	return heartbeat.NewMonitor(endpoint, model, apiKey).Beat(ctx)
}

// discoverDelegationBeat is the seam onto the ONE observation an unattended run takes of its
// Sub-agent server, kept separate from discoverBeat above because it beats a DIFFERENT box: the
// Sub-agent server's own endpoint, model and key, which is why the primary's beat can never be
// shared with it (resolveDelegationTarget would then resolve a target against the wrong server and
// route delegations to a box nobody observed). Like the beat above it stands in for the heartbeat an
// unattended run has none of, it is one beat with no retry, and it is a variable so the composition
// is provable without a live server; production never reassigns it.
//
// It never reports an error, for discoverBeat's reason: an unreachable server, a cancelled context
// and a server with nothing bound are all "no target", which leaves the run unrouted — the fallback
// every Firing took before this seam existed (ADR 0045 §4's floor).
//
// It fires ONLY when `sub-agents-server:` names an entry. A run that delegates to its own server
// asks nothing here, so the default composition path costs no third round trip.
var discoverDelegationBeat = func(ctx context.Context, endpoint, model, apiKey string) heartbeat.Beat {
	return heartbeat.NewMonitor(endpoint, model, apiKey).Beat(ctx)
}

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
	flags.StringVar(&opts.StartupServer, "server", "",
		"name of the servers: entry to start on (default: the last one /server switched to)")
	// The claim beside the registration above: this command has the flag, so its startup refusal
	// may offer it as the fix. The commands that do not register it say APOGEE_SERVER instead.
	opts.ServerFlagBound = true
	flags.StringVar(&opts.Model, "model", "", "model name to request (default: the configured model)")
	flags.StringVar(&opts.Mode, "mode", string(domain.ModePlan),
		"autonomy mode for the run: plan | auto (an unattended run has nobody to ask)")
	flags.StringVar(&opts.Workspace, "workspace", "",
		"workspace root the file tools are scoped to (default: current directory)")
	flags.StringVar(&opts.ConfigDir, "config", "",
		"apogee home directory for config/library/sessions (default: ~/.apogee)")
	flags.BoolVar(&opts.Bypass, "bypass", false,
		"run with the lab Mechanisms off; Floor guards and structural reducers stay on (ADR 0071)")
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
// The Config itself is not composed here: it comes out of firingConfig (wire_firing.go), the one
// composer every unattended run shares, because a headless run and a Firing are the same thing
// reached by different Drivers (ADR 0031). What stays in this function is only what this Driver
// decides — the prompt, the mode gate, the confinement backend and its eligibility ruling, the
// scratch sweep, the notices this command prints in its own voice, and the store the record lands
// in.
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

	// The scratch sweep, run once here for the reason runRoot runs it at boot (wire.go): this run
	// mints a dir of its own below and a host that is only ever driven headlessly never passes the
	// TUI's boot, so this is the only beat on which the dirs earlier runs left behind are reclaimed.
	// Best-effort and silent, exactly as it is there — GC is never a reason a run fails to start.
	gcScratchDirs(roots.scratch, time.Now())

	// A plaintext `api-key:` in the config file is worth saying out loud here too (ADR 0047), but
	// only saying: the migration OFFER is the TUI's, because moving a key is a consented edit to the
	// human's own config and an unattended run has nobody to consent (the ADR 0036 reasoning that
	// keeps a headless start-up refusing rather than picking). So no store is probed and nothing is
	// written — the notice names the entries and what can be done about them by hand, on stderr,
	// where it cannot contaminate the answer.
	if names := plaintextKeyEntries(opts.Servers); len(names) > 0 {
		cmd.PrintErrln(plaintextKeyNotice(filepath.Join(roots.config, "config.yaml"), reasonHeadless, names))
	}

	// A retired `sub-agents: true` flag is worth the same sentence, and for the same reason: the
	// migration OFFER is the TUI's (prepareSubAgentsMigration, keymigrate.go), which a headless run
	// never reaches because it builds no rootWiring — so the flag would sit there routing nothing,
	// silently, for as long as the config is only ever driven this way. The detection is the same
	// raw-YAML scan the offer uses, because ServerEntry has no field for the retired key; a file the
	// scan stumbles on is silent rather than fatal, exactly as it is there.
	if names, err := config.RetiredSubAgentsEntries(filepath.Join(roots.config, "config.yaml")); err == nil && len(names) > 0 {
		cmd.PrintErrln(subAgentsFlagNotice(filepath.Join(roots.config, "config.yaml"), names))
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
		if blocked := probe.AutoUnattendedBlocked(
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
		//
		// probe.ResidualNotice IS printed, for the opposite reason: its cell is a backend that
		// fences — so the run is never refused — which knowingly leaves a write-class access open
		// (landlock ABI 1–2 and truncate(2)). An unattended run is exactly where that goes
		// unnoticed otherwise, and it names no slash command. It is disclosure on stderr, never a
		// blocker: the answer on stdout is untouched.
		if notice := probe.ResidualNotice(
			probe.BackendName(confiner), confiner.Capabilities(), mode, opts.ConfineToWorkspace); notice != "" {
			cmd.PrintErrln(notice)
		}

		// Eager pre-warm of the confinement label walk, behind the SAME gate and in the same place
		// the launch path uses (announceConfinement, wire_boot.go) — one gate function, never a
		// second copy, because a second copy is how the two boot paths drift apart. On the Windows
		// token backend a confined command labels the workspace tree at ~1 ms/object, and an
		// unattended run is exactly where a first command stalling on a large .git goes unexplained;
		// under Auto+confine a confined command is effectively certain, so the walk is hoisted here.
		// Off Windows PrewarmLabelWalk is an empty no-op (internal/platform/prewarm_other.go), so
		// this changes no byte of this command's output on any other host.
		//
		// The one deliberate difference from the launch path: the progress notice goes to the
		// command's own stderr writer rather than raw os.Stderr, because everything this command
		// narrates travels through the cobra writers.
		if shouldPrewarmLabelWalk(mode, opts.ConfineToWorkspace, confiner.Capabilities().FSWrite) {
			prewarmLabelWalk(confiner, roots.workspace, cmd.ErrOrStderr())
		}
	}

	// Every `mechanisms:` key is validated here — enabled AND disabled — exactly as startup
	// validates them: the engine only ever sees the enabled IDs, so a typo'd disabled key would
	// otherwise never be reported at all (ADR 0015 §1).
	manualIDs, retiredNotices, err := mechanisms.ResolveEnabled(opts.Mechanisms, mechanisms.KnownIDs())
	if err != nil {
		return notStarted(err)
	}
	// A key naming a RETIRED Mechanism is tolerated rather than refused — it was valid at the
	// release before the removal — and this is where this Driver says so. Without the line the run
	// arms nothing and explains nothing: a script whose config still asks for a removed Mechanism
	// would keep paying for runs that quietly differ from the ones it was tuned on. It goes to
	// stderr, beside the plaintext-key and confinement notices above, because stdout is the
	// model's answer and nothing else.
	for _, notice := range retiredNotices {
		cmd.PrintErrln(notice)
	}

	// This run's own record id, minted here because the runner is handed it beside the Config
	// (run.Spec) and the composer creates its scratch dir under that name. A headless run had
	// neither before: nothing on this path mints a session id, so its model was offered no writable
	// scratch inside the box and put its working files wherever else it could reach — the workspace
	// itself, under an Auto fence.
	recordID := session.NewID(time.Now())

	// The construction surface every unattended run shares (wire_firing.go), reached from this
	// Driver's own inputs: the startup selection as the bound entry, this invocation's roots and
	// mode, and no key resolver, skill catalog or width source of its own — a command that runs once
	// has no longer-lived facility to share, so the composer's own defaults are exactly right here.
	//
	// What comes back beside the Config is the per-model rebind's narration: a validated set
	// applying, being offered or being suppressed, and a built-in Model profile announcing itself.
	// It goes to stderr, where it cannot contaminate the answer.
	entry := startupEntry(*opts)
	cfg, routing, notices, err := firingConfig(cmd.Context(), firingInputs{
		opts:      *opts,
		entry:     entry,
		roots:     roots,
		manualIDs: manualIDs,
		confiner:  confiner,
		mode:      mode,
		recordID:  recordID,
	})
	if err != nil {
		return notStarted(err)
	}
	for _, n := range notices {
		cmd.PrintErrln(n)
	}

	// The offline gate: a server that answered NOTHING refuses this run before a prompt is spent on
	// it. The composition above already took the one beat an unattended run gets (firingConfig's
	// unconditional observation, carried out on firingRouting), so the question costs no round trip
	// of its own — it is read off what the composer already learned.
	//
	// The condition is Answered and nothing else: false ONLY for a transport-level failure — a
	// refused dial, a timeout, a DNS or TLS failure, an address that could not be formed. Every
	// server that returned ANY HTTP response — a 401, a 404, a 429, a body that would not decode —
	// keeps today's proceed-and-degrade, because those are answers this Driver cannot judge and a
	// throttled probe is silence rather than a verdict (internal/heartbeat's Beat.Answered). That is
	// deliberately WEAKER than routing.Reachable, which is "handed me a usable model list": a
	// completions-only endpoint that serves no list at all still runs, exactly as it does today.
	//
	// It refuses BEFORE runOnce, which is the whole point: no session record is written, no token is
	// spent, and the exit is the existing never-started code (2), so a script can tell "the model
	// never got the chance" from "the model ran and it went wrong".
	//
	// The wording is the TUI's own, from Model.upstreamBlockNote (internal/tui/heartbeat.go), so a
	// human who has seen a session refuse a send reads the same sentence from an unattended run.
	// The two are composed SEPARATELY — the TUI's is a Model method over its live heartbeat state,
	// and hoisting it is a bigger change than this gate — so each site names the other and the test
	// below pins the exact sentence; an edit to one wording must visit both.
	if !routing.Beat.Answered {
		note := "cannot send — server offline (" + entry.Endpoint + ")"
		if routing.Beat.Failure != "" {
			note += ": " + routing.Beat.Failure
		}
		return notStarted(errors.New(note))
	}

	// The shared sessions store, built whatever --no-save says: the sweep below is about the
	// records ALREADY on disk, not about the one this run may add, so the flag must not switch it
	// off. Building it costs nothing on its own — the store is a directory path and a clock until
	// something reads or writes.
	sessions := session.NewStore(roots.sessions)

	// The store the record lands in: the shared sessions store, so a headless run is browsable in
	// /sessions beside the conversations it ran beside. --no-save leaves it nil, which is
	// internal/run's own "persist nothing" and leaves Result.SessionID empty.
	var store *session.Store
	if !noSave {
		store = sessions
	}

	// The startup session sweep (wire.go), run here for the reason the daemon runs it: a host that
	// only ever runs `apogee headless` never passes the TUI's boot, so this is the only beat on
	// which its retention policy is ever applied. That host is exactly the one most likely to pass
	// --no-save, which is why the sweep is handed the store above rather than the record-writing
	// one: --no-save promises no RECORD of this run, never an unswept store. No id is kept — a
	// headless run resumes nothing, and the record it is about to mint is not in the store yet.
	gcSessions(sessions, opts.Sessions)

	// Ctrl-C and SIGTERM end the run rather than the process: the cancellation flows out of Once
	// as a run failure carrying whatever the run had reached, so an interrupted run still prints
	// its partial answer and still saves its record.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The one Event this Driver renders live. Everything else a headless run reports comes back on
	// Result, but a prune happens MID-run and leaves no trace on the answer, so a human watching an
	// unattended run would otherwise see a window quietly shrink with nothing said. The sink WRAPS
	// whatever the Config already carries (nil here today) rather than replacing it, exactly as
	// run.Once's own tap wraps this one in turn (run.Spec).
	cfg.Events = pruneNoticeSink{inner: cfg.Events, out: cmd.ErrOrStderr()}

	// The routing the composer resolved, latched through run.Spec's own seam (internal/run): a
	// headless run delegates to the `sub-agents-server:` entry exactly as a session does, and both
	// fields are nil when no key named one — the unrouted floor every Firing had before this.
	res, runErr := runOnce(ctx, run.Spec{
		Config:           cfg,
		Prompt:           prompt,
		Store:            store,
		RecordID:         recordID,
		DelegationTarget: routing.target,
		DelegationSeat:   routing.seat,
	})

	// What the workspace context files contributed, one line apiece on stderr: the same three
	// sentences a session shows in its transcript, composed once in internal/notice so the two
	// Drivers narrate one event with one wording. ALL of them print, anomaly or not — an
	// unattended run has no transcript to scroll back through, so the plain record of what loaded
	// is worth as much here as the loud skips are.
	//
	// It sits ABOVE the never-started exit below deliberately: a Firing refused before submit has
	// still LOADED its context files (run.Once populates the report on that exit), and reporting
	// them is the whole point of carrying them out of a run that produced no answer.
	//
	// Escape-stripped exactly as the answer is below: the names trace to config and the errors to
	// the filesystem, and internal/notice composes without sanitising by contract.
	for _, n := range notice.ContextFileNotices(res.ContextFiles) {
		cmd.PrintErrln(sanitize.StripEscapes(n.Text))
	}

	// A refusal that stopped the Firing before it began is exit 2, not exit 1 — nothing was sent
	// and nothing was saved, so what a script must do about it is fix the invocation, not read an
	// outcome. run.Once marks that class structurally rather than by sentinel: it returns ZERO
	// TURNS from each of its three pre-run exits (a mode a Firing may not run, an Agent that would
	// not construct, a prompt it would not accept) and at least one Turn from every run it actually
	// drove. Turns is the field this gate reads, and it is the only one that stays zero: a pre-run
	// exit may still carry a populated Result — the prompt-refusal one reports the context files
	// the session had already loaded. So a non-nil error with no Turn behind it is a run that never
	// started, whatever it travelled out of: Auto on a host with no filesystem confinement, an
	// endpoint no config ever set, a model that would not bind.
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
	// Escape-stripped on the way out (internal/sanitize, which owns the set and the reasons):
	// Result.FinalText is RAW model output by contract (internal/run: the answer crosses as plain
	// data, ADR 0010), and stdout is a terminal often enough that the strip belongs at this render
	// seam, exactly as the TUI strips at its own.
	//
	// Written through OutOrStdout rather than cmd.Println: Cobra's whole Print/Printf/Println
	// family resolves to OutOrStderr, so it would put the answer on STDERR in every real
	// invocation (the fallback only differs when a caller has wired an out writer, which is
	// tests and nothing else). The product goes to real stdout; notices and the summary below
	// travel by PrintErrln, which does target the err stream.
	if text := sanitize.StripEscapes(res.FinalText); text != "" {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), text)
	}
	// What the run CHANGED on disk, ahead of the readings: a header naming the count and one
	// indented path per entry (writtenFilesLines composes; the daemon logs the same block). A run
	// that recorded no write prints nothing at all — silence is the honest report, and a "0 files"
	// header on every read-only run would be noise on the Driver whose whole output is grepped.
	//
	// It sits below the answer and above the fill lines because it is the run's OUTCOME rather than
	// its narration: a human reading an unattended Auto run wants what moved in the workspace
	// before what the window cost. Faulted and failed runs reach here too, and those are exactly
	// the runs whose half-finished writes have to be visible.
	//
	// stderr, like every other narration on this Driver: the stdout contract is the answer alone
	// (TestHeadlessAnswerLandsOnTheProcessStdout), so no part of this block may take OutOrStdout.
	for _, line := range writtenFilesLines(res.Wrote) {
		cmd.PrintErrln(line)
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
	// An abandoned final Turn is exit 3, and it is decided AFTER the failure branch above on
	// purpose: a run that errored is exit 1 whether or not it also faulted, because the error is
	// the more actionable of the two. What reaches here is a run that returned no error and still
	// has no answer — the answer text above is the run's last words, so the error names the fault
	// the engine reported (run.Result.Fault) and, like a failure, the record it can be read in.
	if res.Faulted {
		err := fmt.Errorf("apogee headless: the run's final turn was abandoned — %s", res.Fault)
		if res.SessionID != "" {
			err = fmt.Errorf("%w (partial run saved as %s)", err, res.SessionID)
		}
		return exitError{code: exitRunFaulted, err: err}
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
	if res.Faulted {
		// The one line a script greps has to say the run has no answer, not only the exit code:
		// a pipeline that keeps the summary and drops the status would otherwise read a faulted
		// run as a completed one.
		stats += " · faulted"
	}
	if res.SessionID == "" {
		return stats
	}
	return "session: " + res.SessionID + " · " + stats
}

// writtenFilesLines renders what a Firing CHANGED on disk: a header naming the count, then one
// indented path per entry, in the order the run first wrote each. It is the shared composer behind
// both unattended Drivers — runHeadless prints the block on stderr, daemonWiring.fire logs it — so
// the two narrate one event in one wording, and a run that recorded no write composes nothing at
// all (a nil slice, not an empty header).
//
// The header says "changed", not "wrote", because the list is exactly run.Result.Wrote: the write
// funnel journals a delete_file target and a move_file SOURCE as well as a creation, so paths the
// run REMOVED ride in it and a "wrote —" header over them would be a lie.
//
// Paths only, and no verb column: the TUI's undo lines (internal/tui/undo.go) describe UNDOING a
// change — a created file reads `delete`, a modified one `restore`, and a file touched since reads
// `skip` — which is the opposite account of the one this block gives, and neither Driver here can
// offer a revert anyway: the journal died with the process. These lines say what happened.
//
// Escape-stripped to a single line apiece: a path traces to a model-chosen tool argument, and both
// sinks are one-line-per-entry — the daemon log by contract (daemon.go), a stderr list by shape.
func writtenFilesLines(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	lines := make([]string, 0, 1+len(paths))
	lines = append(lines, "changed — "+strconv.Itoa(len(paths))+" file(s) this run:")
	for _, path := range paths {
		lines = append(lines, "  "+sanitize.StripEscapesToLine(path))
	}
	return lines
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
		if model := clipSubAgentTask(sanitize.StripEscapesToLine(r.Model)); model != "" {
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
			Calls:              r.Calls,
			PromptTokens:       r.PromptTokens,
			CompletionTokens:   r.CompletionTokens,
			TotalTokens:        r.TotalTokens,
			CachedPromptTokens: r.CachedPromptTokens,
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
//
// The cached column is the one counter that hides itself: it is a SUBSET of the prompt count that
// most Upstreams never report, so a zero there means "this server said nothing about caching"
// rather than a spend of zero — the same self-hiding rule the fill lines apply, and the reason it
// is appended after the counters the line always carries rather than wedged between them.
func headlessUsageLine(u run.Usage, who string) string {
	if u.Calls <= 0 {
		return ""
	}
	line := fmt.Sprintf("usage: calls %d · prompt %s · completion %s · total %s",
		u.Calls, headlessTokens(u.PromptTokens), headlessTokens(u.CompletionTokens), headlessTokens(u.TotalTokens))
	if u.CachedPromptTokens > 0 {
		line += " · cached " + headlessTokens(u.CachedPromptTokens)
	}
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
// delegation written before the name argument existed, and every one a Mechanism synthesises. The
// choice itself is title.DelegateLabel, the one rule every Driver's delegation display asks; this
// Driver has no run header to paint, and still names a child exactly as the one that does.
//
// What is this seam's own is the treatment both spellings get before the rule sees them, because
// run.SubAgentUsage hands both over as raw model output on the same terms as the answer: stripped of
// control characters HERE, at this render seam, in the line-safe form — so neither can rewind or
// re-column the reading it sits beside — and clipped afterwards, so a model that "named" a
// delegation with a screenful of instructions cannot take the terminal over with one line. Passing
// the STRIPPED spellings in is what decides the fallback on the rendered form: a name that is
// nothing but control characters leaves the task showing rather than blanking the slot.
func headlessSubAgentTarget(r run.SubAgentUsage) string {
	return clipSubAgentTask(title.DelegateLabel(
		sanitize.StripEscapesToLine(r.Name),
		sanitize.StripEscapesToLine(r.Task),
	))
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
