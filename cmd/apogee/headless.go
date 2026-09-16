package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/eventjson"
	"github.com/airiclenz/apogee/internal/format"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/notice"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/sanitize"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/title"
	"github.com/airiclenz/apogee/internal/tools"
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
//
// The hard SECOND interrupt (hardExit, watchSecondInterrupt) is the one deliberate exception in
// this command, and it is not a hole in the rule: a human pressing Ctrl-C twice is asking for the
// wind-down ITSELF to stop, so that path skips the deferred teardown on purpose — no confinement
// Close, no Reaction drain, no closing frame — and every other exit in this file still travels as
// an error through here.
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

// hardExit is the seam onto os.Exit, and the only place in this binary's headless path that may
// reach it (the exitError rule above says why). It exists so the second-interrupt watch is
// provable: a test that could not replace it would end the test binary instead of asserting on
// the code it asked for. Production never reassigns it.
var hardExit = os.Exit

// interruptSignals is the seam onto the SECOND-interrupt registration: the same two signals
// signal.NotifyContext already watches, delivered a second time to a channel of this command's
// own, because the context can only be cancelled once and the second press has to be visible as
// an event rather than as a state.
//
// It is a variable for the reason runOnce is: a test cannot raise a real SIGTERM at this process
// without ending the suite it runs in, so the whole watch — count to two, print, exit hard —
// would otherwise be unassertable. Replacing it hands the test the very channel the watch reads.
// Production never reassigns it.
var interruptSignals = func(ch chan<- os.Signal) (stop func()) {
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	return func() { signal.Stop(ch) }
}

// secondInterruptNotice is the whole of what a hard exit says on its way out. It goes to stderr,
// never to the stream: the exit writes no `run_finished` — there is no run left to finish — so the
// one honest place to say what happened is the channel the human is watching.
const secondInterruptNotice = "apogee headless: second interrupt — exiting without waiting for the run"

// watchSecondInterrupt ends the PROCESS on a second interrupt, and does nothing at all on the
// first: that one is already the polite stop, cancelling the run's context so the Firing unwinds
// with its record saved, its stderr prose printed and its closing frame written.
//
// The second press is a different request. Under `--format json` the wind-down can take real time
// — a Turn that has to notice the cancellation, a record to write, Reactions draining on their five
// second grace — and a human watching a stream that has stopped moving has no way to tell a slow
// teardown from a wedged one. So the second press is taken literally: one line on stderr and
// [hardExit], skipping every deferred teardown in this command (ADR 0075 decision 9). Nothing is
// half-done that was not already half-done by the first interrupt; what is lost is the tidying.
//
// It ends with the run — done is closed as [runHeadlessBody] returns — so a run that finishes
// normally leaves no goroutine listening for a signal nobody will send. Text mode registers none
// of this: its stdout is prose a reader can simply stop reading, and a second Ctrl-C there has
// always been the terminal's own affair.
func watchSecondInterrupt(sigs <-chan os.Signal, done <-chan struct{}, errOut io.Writer) {
	select {
	case <-sigs:
	case <-done:
		return
	}
	select {
	case <-sigs:
		_, _ = fmt.Fprintln(errOut, secondInterruptNotice)
		hardExit(exitRunFailed)
	case <-done:
	}
}

// prewarmLabelWalk is the seam onto the Windows label-walk pre-warm, for the same reason runOnce
// and newConfiner are seams: platform.PrewarmLabelWalk is an empty function off Windows
// (internal/platform/prewarm_other.go), so a test that only asserted "the run made no noise" would
// pass identically against a tree that never calls it at all. Replacing this variable is how the
// suite proves the confined-Auto headless path reaches the pre-warm on the one host where it does
// work. Production never reassigns it.
var prewarmLabelWalk = platform.PrewarmLabelWalk

// narrationSink is the headless Driver's own EventSink: the live view of an unattended run under
// `--format text`. It prints one stderr line per [domain.PruneEvent], one per tool call and tool
// result at Depth 0, and one per sub-agent lifecycle boundary — and forwards every Event, its own
// included, to whatever sink it wraps (nil ⇒ nothing to forward to). A zero-value inner is the
// normal case — a bare headless run composes no sink of its own — and the wrap still matters,
// because run.Once installs its tap AROUND this one rather than instead of it (run.Spec).
//
// It lives here, in the command, rather than in internal/run's eventTap, because internal/run is
// also the DAEMON's Firing path (daemonfire.go): a print there would put a line on a daemon's
// stderr on every Firing, for a human who is not watching. The record keeps the same facts either
// way — transcriptFold folds them — so this sink is the live view alone. stdout is untouched: the
// answer is still the one thing written there, at the end (TestHeadlessAnswerLandsOnTheProcessStdout).
//
// The lines are narration, not the run's outcome, so they read as arrows rather than as the
// post-hoc block's sentences:
//
//	→ <tool> <summary>           a tool call, the summary being the call's first string argument
//	← <tool> ok                  its result
//	← <tool> error: <first line> its result, when the tool failed
//	sub-agent <name>: started    a delegation's child began running
//	sub-agent <name>: finished   ... reached its boundary (cancelled, when the human cancelled it)
//
// Depth gating is per line family. Tool lines print at Depth 0 only: a child's calls are its own
// business, and the sub-agent lines stand in for them. The sub-agent lines print at Depth 1 —
// the engine stamps a phase and a rename with the CHILD's identity (dispatch.go's
// emitSubAgentPhase), one level below the Depth-0 sub_agent call whose id they carry — so a
// grandchild's phases are as silent as its calls. The prune line has no depth gate, unchanged: a
// child's pruning pass shrinks a window the human never sees otherwise.
//
// It prints for a prune and NOTHING for a [domain.ReactionFiredEvent], deliberately: a Floor guard
// repairing the model's own failure is engine behaviour rather than news for the human who is not
// watching, so it stays in the TUI's hidden debug view and off this stderr (ADR 0071). It is
// still forwarded, like every other Event.
//
// quiet switches the printing half off and leaves the forwarding half exactly as it was. It is what
// `--format json` asks for: there the same Events are already on stdout as their own lines, and
// each stderr sentence would be the one fact told twice in two vocabularies (ADR 0075 decision 6).
// The sink is still WRAPPED under json rather than dropped, because dropping it would drop the
// forward every observer behind it depends on — the Reaction Runner included.
//
// It is installed as a POINTER: the sub-agent lines name a delegation that only the earlier
// sub_agent call Event carried, so the sink remembers each Depth-0 call under its id (calls) and a
// value receiver would forget it on the next Emit. The result line needs no such memory — the
// [domain.ToolResultEvent] names its tool itself. Emit is never called concurrently: the engine
// serializes emission on its side ([domain.EventSink]).
type narrationSink struct {
	inner domain.EventSink
	out   io.Writer
	quiet bool
	// calls remembers every Depth-0 call seen, by id, for the one fact a later Event does not
	// carry: a sub_agent call's delegation display name, which the phase line needs. Nil until the
	// first call, so a zero value is usable.
	calls map[string]narratedCall
}

// narratedCall is what the sink keeps of one Depth-0 tool call once its Event has gone by.
type narratedCall struct {
	// name is the delegation's display name, for a sub_agent call: the `name` argument the model
	// gave, or the one a later [domain.SubAgentNamedEvent] handed it. Empty means neither has
	// landed, and the phase line falls back to the call id.
	name string
}

// Emit narrates the Event — unless quiet, which forwards and says nothing — then forwards to the
// sink it wraps whatever it printed. The prune's two numbers are rendered verbatim, worded as
// every other Driver words them (internal/tui's transcript.addPrune, internal/run's
// transcriptFold.fold), so one pruning pass reads the same on a terminal, in a session record and
// in a scrollback.
func (s *narrationSink) Emit(e domain.Event) {
	if !s.quiet {
		s.narrate(e)
	}
	if s.inner != nil {
		s.inner.Emit(e)
	}
}

// narrate prints the one line an Event earns, or nothing: the depth gates and the line shapes are
// the ones on the type.
func (s *narrationSink) narrate(e domain.Event) {
	switch ev := e.(type) {
	case domain.PruneEvent:
		_, _ = fmt.Fprintf(s.out, "pruned %d tool results (~%d tokens)\n", ev.Results, ev.Tokens)
	case domain.ToolCallEvent:
		if ev.Depth != 0 {
			return
		}
		s.remember(ev.Call)
		line := "→ " + ev.Call.Tool
		if summary := narrationLine(firstStringArgument(ev.Call.Arguments)); summary != "" {
			line += " " + summary
		}
		_, _ = fmt.Fprintln(s.out, line)
	case domain.ToolResultEvent:
		if ev.Depth != 0 {
			return
		}
		_, _ = fmt.Fprintln(s.out, s.resultLine(ev))
	case domain.SubAgentPhaseEvent:
		if ev.Depth != 1 {
			return
		}
		word := string(ev.Phase)
		if ev.Cancelled {
			word = "cancelled"
		}
		_, _ = fmt.Fprintf(s.out, "sub-agent %s: %s\n", s.subAgentName(ev.CallID), word)
	case domain.SubAgentNamedEvent:
		if ev.Depth != 1 {
			return
		}
		if call, ok := s.calls[ev.CallID]; ok {
			call.name = sanitize.StripEscapesToLine(ev.Name)
			s.calls[ev.CallID] = call
		}
	}
}

// remember files a Depth-0 call under its id, reading the delegation name off a sub_agent call's
// arguments while the bytes are at hand. A name that fails to decode is simply absent, and the
// phase line falls back to the id.
func (s *narrationSink) remember(call domain.ToolCall) {
	if s.calls == nil {
		s.calls = make(map[string]narratedCall)
	}
	var remembered narratedCall
	if call.Tool == tools.SubAgentToolName {
		var args tools.SubAgentArgs
		if err := json.Unmarshal(call.Arguments, &args); err == nil {
			remembered.name = sanitize.StripEscapesToLine(args.Name)
		}
	}
	s.calls[call.ID] = remembered
}

// resultLine words one Depth-0 result: `← <tool> ok`, or `← <tool> error: <first line>` for a
// failed call — `← <tool> error` when the failure carried no text at all, rather than a colon with
// nothing after it. The tool is the one the Event names (ToolResultEvent.Tool — the resolved name
// the engine dispatched, stamped at the commit point), so the line needs nothing from the call that
// went by. The first line is the first line the COMMAND wrote: a terminal or python_exec result
// opens with a `cwd:` line the tool writes for the model, and that comes off first
// (tools.StripCwdLine — the one strip the TUI's card shares) so the narration never reads
// `← terminal error: cwd: /ws`. A result stamped with no tool name — a stub driving the sink with a
// bare result, or a slot the engine never resolved — is named by its id alone, so the line still
// says which result it is.
func (s *narrationSink) resultLine(ev domain.ToolResultEvent) string {
	result := ev.Result
	if ev.Tool == "" {
		return "← " + result.CallID
	}
	if !result.IsError {
		return "← " + ev.Tool + " ok"
	}
	first, _, _ := strings.Cut(tools.StripCwdLine(result.Content), "\n")
	if first = narrationLine(first); first == "" {
		return "← " + ev.Tool + " error"
	}
	return "← " + ev.Tool + " error: " + first
}

// subAgentName is what a phase line calls the delegation the sub_agent call callID spawned: the
// name the call gave, else the one the naming call handed it since, else the id itself.
func (s *narrationSink) subAgentName(callID string) string {
	if call, ok := s.calls[callID]; ok && call.name != "" {
		return call.name
	}
	return callID
}

// narrationLine makes an untrusted string safe and short enough to stand beside an arrow: escape
// sequences and line breaks out (sanitize.StripEscapesToLine — a model-supplied argument or a
// tool's error text is exactly the text that must not forge a second line), whitespace trimmed,
// and the rest clipped to the sub-agent line's own rune budget, ellipsis included.
func narrationLine(text string) string {
	return clipSubAgentTask(strings.TrimSpace(sanitize.StripEscapesToLine(text)))
}

// firstStringArgument returns the value of the first string-valued top-level member of a tool
// call's argument object, in the order the object was WRITTEN — which is the order the model
// chose, and for every workspace tool the path or command comes first. The empty string means
// there is none: no object, no members, or none whose value is a string. The tokens are walked
// rather than the object unmarshalled because a map would lose that order, and a nested object or
// array in an earlier member is skipped as a whole value rather than descended into.
func firstStringArgument(arguments json.RawMessage) string {
	dec := json.NewDecoder(bytes.NewReader(arguments))
	if open, err := dec.Token(); err != nil || open != json.Delim('{') {
		return ""
	}
	for dec.More() {
		if _, err := dec.Token(); err != nil {
			return ""
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return ""
		}
		if len(value) == 0 || value[0] != '"' {
			continue
		}
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return ""
		}
		return text
	}
	return ""
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

// The two values `--format` accepts, and the whole of what this command offers a caller who is not
// a human: text is the prose path this command has always had, and json is the versioned JSONL
// Event lines of ADR 0075, which replace stdout entirely rather than decorating it.
const (
	formatText = "text"
	formatJSON = "json"
)

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
	var outputFormat string
	var seams bool

	cmd := &cobra.Command{
		Use:   "headless [prompt]",
		Short: "Run one prompt to completion without a UI and print the answer",
		Long: "apogee headless runs a single prompt to completion with nobody watching and\n" +
			"prints the answer to stdout. Give the prompt as the argument, or pipe it on\n" +
			"stdin; an empty prompt is a usage error.\n\n" +
			"The run is unattended, so it never asks: every gated action is refused rather\n" +
			"than parked (the count is reported), ask_user and present_document are not\n" +
			"registered, and no MCP server is contacted. Only two modes make sense here and\n" +
			"only two are accepted — plan (the default: read-only except its own scratch dir)\n" +
			"and auto (confined and unattended); ask-before and allow-edits both exist to\n" +
			"consult a human. Auto is refused on a host whose confinement backend cannot fence\n" +
			"the filesystem: there the fallback is approval, and there is nobody here to\n" +
			"approve.\n\n" +
			"Settings resolve exactly as a session's do — flag over APOGEE_* environment over\n" +
			"config.yaml — so a headless run has the shape a session on this host would have.\n" +
			"The run is saved to ~/.apogee/sessions like any other session and shows up in\n" +
			"/sessions; pass --no-save to run it and record nothing.\n\n" +
			"The answer goes to stdout and everything else to stderr, so a pipeline reads\n" +
			"only the model's text. A run that delegated states each sub-agent's context\n" +
			"fill on a stderr line of its own, ahead of the closing summary — each child\n" +
			"fills a window the run's own figures say nothing about. Pass --format json to\n" +
			"make stdout the versioned JSONL Event lines instead — one object per engine\n" +
			"event, bracketed by a run_started/run_finished pair. Exit codes: 0 the run\n" +
			"completed, 1 the run started and failed (model or tool error, cancellation, a\n" +
			"record that would not save), 2 the run never started (usage, configuration, a\n" +
			"refused mode).",
		Args:          headlessArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHeadless(cmd, args, &opts, noSave, outputFormat, seams)
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
		"run with advise and shape Reactions of user or bench origin off; "+
			"Floor guards and structural reducers stay on (ADR 0076)")
	flags.BoolVar(&noSave, "no-save", false,
		"run the prompt and print the answer, but record no session")
	flags.StringVar(&outputFormat, "format", formatText,
		"what stdout carries: text (the answer) | json (the JSONL Event lines, ADR 0075)")
	flags.BoolVar(&seams, "seams", false,
		"also emit seam_closed lines (--format json only)")

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

// runHeadless is the command's exit funnel: it reads the requested format, drives the body once,
// and — under json — writes the closing frame on whichever path the body left by.
//
// The funnel exists because ADR 0075 decision 5 promises EXACTLY ONE `run_finished` on every exit
// path, and the body has fifteen of them: eleven `notStarted` refusals, a failed run, an abandoned
// final Turn and a success. Writing the frame at each would be fifteen chances to forget one, and
// a consumer would meet the omission as an empty stdout it has to interpret. Funnelling makes the
// promise structural instead, and leaves the body's own control flow — and therefore the text
// path's every byte — exactly as it was.
//
// An unknown format is refused in TEXT mode: no stream exists yet to carry the refusal, and a
// JSONL stream whose single line said "that is not a format" would be a worse answer than the
// prose every other usage mistake gets. Cobra has already parsed the flag by the time this runs,
// so this is the only point that can judge its VALUE (an unknown flag NAME is the FlagErrorFunc's).
//
// `--seams` is the same class of mistake when it arrives without `--format json`: the flag opts the
// Event lines into the seam_closed kind (eventjson.Options.Seams), and the text path has no Event
// lines for it to reach. It is refused here rather than silently ignored, because a caller who
// asked for seam lines and got prose would read the silence as "the run closed no seams".
func runHeadless(cmd *cobra.Command, args []string, opts *config.Options, noSave bool, outputFormat string, seams bool) error {
	switch outputFormat {
	case formatText:
		if seams {
			return notStarted(errors.New("apogee headless: --seams needs --format json"))
		}
		_, err := runHeadlessBody(cmd, args, opts, noSave, nil)
		return err
	case formatJSON:
		// Disarmed BEFORE the first line is written, and only on this branch: from here on a
		// consumer that walks away breaks the STREAM rather than the process (sigpipe_unix.go).
		// The text path keeps the default disposition it has always had, where dying at a closed
		// pipe is the right and expected behaviour for a program whose stdout is its prose.
		ignoreSIGPIPE()
		// Report is the stream's only word about its own failure, and it is spent on stderr rather
		// than on the stream: whatever broke is the stdout the lines were being written to, so the
		// stream is precisely the channel that cannot carry the news. The Writer calls it once —
		// the FIRST write error and nothing after it (internal/eventjson) — so a consumer that
		// closed the pipe early sees one line, not one per Event the run had left to emit. The run
		// itself continues to its own end and keeps its own exit code: a run that has already
		// edited files is not half-killed because a reader walked away.
		lines := eventjson.New(cmd.OutOrStdout(), eventjson.Options{
			Report: func(err error) {
				cmd.PrintErrln("apogee headless: event lines stopped — " + err.Error())
			},
			Seams: seams,
		})
		res, err := runHeadlessBody(cmd, args, opts, noSave, lines)
		lines.RunFinished(runFinishedFrame(res, err))
		return err
	default:
		return notStarted(fmt.Errorf(
			"apogee headless: --format %s is not an output format (use --format text or --format json)",
			outputFormat))
	}
}

// runFinishedFrame composes the closing frame from what the run reached and the error it left by.
// It is the one place the stream's exit code is decided, and it deliberately does NOT ask
// exitCodeFor on a nil error: that helper answers 1 for "an error carrying no code", which is the
// right default for an error and the exact opposite of the truth for a run that succeeded.
func runFinishedFrame(res run.Result, err error) eventjson.RunFinished {
	frame := eventjson.RunFinished{
		Turns:        res.Turns,
		Denied:       res.Denied,
		Faulted:      res.Faulted,
		Fault:        res.Fault,
		Title:        res.Title,
		FinalText:    res.FinalText,
		Wrote:        res.Wrote,
		ContextFiles: contextFilesFrame(res.ContextFiles),
		UndoNote:     res.UndoNote,
		// The RECORD, not the run: --no-save leaves SessionID empty on a run that carried an id
		// all along, and this is what a consumer reads before it feeds an id to `apogee undo`.
		Saved:     res.SessionID != "",
		Usage:     usageFrame(res.Usage),
		SubAgents: subAgentFrames(res.SubAgents),
	}
	if err != nil {
		frame.ExitCode = exitCodeFor(err)
		text := err.Error()
		frame.Error = &text
	}
	return frame
}

// contextFilesFrame restates run.Result.ContextFiles in the frame's own types. The mapping is
// mechanical and lives here rather than in internal/eventjson because that package renders the
// engine's Events and must not reach into internal/run to compose a frame (ADR 0075: the frames
// are the Driver's to fill).
func contextFilesFrame(report domain.ContextFilesReport) eventjson.ContextFiles {
	var files []eventjson.ContextFileNote
	for _, note := range report.Files {
		files = append(files, eventjson.ContextFileNote{Name: note.Name, Bytes: note.Bytes, Err: note.Err})
	}
	return eventjson.ContextFiles{
		Files:          files,
		StandingTokens: report.StandingTokens,
		SystemShare:    report.SystemShare,
	}
}

// usageFrame restates the Firing's own cumulative token accounting for the frame.
func usageFrame(u run.Usage) eventjson.Usage {
	return eventjson.Usage{
		Calls:              u.Calls,
		PromptTokens:       u.PromptTokens,
		CompletionTokens:   u.CompletionTokens,
		TotalTokens:        u.TotalTokens,
		CachedPromptTokens: u.CachedPromptTokens,
	}
}

// subAgentFrames restates each finished sub-agent run's fill and spend for the frame, in the
// finish order the Result already carries. A Firing that delegated nothing composes nothing, so
// the member is null rather than an empty list.
func subAgentFrames(runs []run.SubAgentUsage) []eventjson.SubAgentUsage {
	var frames []eventjson.SubAgentUsage
	for _, r := range runs {
		frames = append(frames, eventjson.SubAgentUsage{
			Used:               r.Used,
			Limit:              r.Limit,
			Task:               r.Task,
			Name:               r.Name,
			Model:              r.Model,
			Calls:              r.Calls,
			PromptTokens:       r.PromptTokens,
			CompletionTokens:   r.CompletionTokens,
			TotalTokens:        r.TotalTokens,
			CachedPromptTokens: r.CachedPromptTokens,
		})
	}
	return frames
}

// runHeadlessBody is the command's body: resolve the prompt and the bindings, compose the Config the
// runner is handed, run it once, and route what came back — the answer to stdout, everything else
// to stderr. It is split out of RunE so the whole path is one testable function.
//
// The Firing itself is not raised here: it goes through raise (wire_firing.go), the one act every
// unattended run is — the Reaction Runner, the record id, the composition, the offline gate and the
// run — because a headless run and a Firing are the same thing reached by different Drivers (ADR
// 0031). What stays in this function is only what this Driver decides — the prompt, the mode gate,
// the confinement backend and its eligibility ruling, the sweeps, the notices this command prints
// in its own voice, the store the record lands in, and the exit code each outcome maps to.
//
// lines is the Event-line stream when the caller asked for one and nil under `--format text`, which
// is the whole of what this function does differently for the two formats: it stamps the session id
// on the stream once the run has one, writes the opening frame at the moment the run is committed
// to, and withholds the plain-text answer from stdout that the frame will carry. The CLOSING frame
// is never written here — runHeadless above owns it, on every path out of this function.
//
// It returns the Result beside the error because that funnel needs both: a refusal that never
// started a run still carries what the session had already loaded, and the frame reports it.
func runHeadlessBody(cmd *cobra.Command, args []string, opts *config.Options, noSave bool, lines *eventjson.Writer) (run.Result, error) {
	// Every line this command narrates leaves through ONE lock from here on. The Reaction Runner built
	// below reports a Reaction's trouble on a Reaction worker's goroutine (internal/reactions), while
	// this function is still writing its own notices and its closing summary on the goroutine it was
	// called on — and Cobra's Print helpers hand both straight to the same io.Writer: a data race on
	// that writer (`go test -race`), and interleaved bytes on a real terminal. Wrapping the command's
	// error stream serialises the two at the only thing they share, so the reporter stays a plain
	// func(string) that knows nothing about this Driver and every line reads exactly as it did.
	cmd.SetErr(&serialWriter{w: cmd.ErrOrStderr()})

	// Before anything is resolved or constructed: with no prompt there is no run to configure.
	prompt, err := resolveHeadlessPrompt(args, cmd.InOrStdin())
	if err != nil {
		return run.Result{}, notStarted(err)
	}

	// The same resolution a session performs (flag > env > file > default), so a headless run
	// talks to the server, and runs with the Reactions, a session on this host would.
	if err := config.ApplyConfig(opts, cmd.Flags().Changed, os.Getenv, os.ReadFile, func(msg string) { cmd.PrintErrln(msg) }); err != nil {
		return run.Result{}, notStarted(err)
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
		return run.Result{}, notStarted(err)
	}
	// The refusal happens HERE, before a Confiner is built or a model is bound: run.Once's own
	// ErrMode is the library's backstop, and a CLI that let the composition run first would spend
	// the work only to report a decision it could have made from the flag alone.
	if mode != domain.ModePlan && mode != domain.ModeAuto {
		return run.Result{}, notStarted(fmt.Errorf(
			"apogee headless: --mode %s consults a human and an unattended run has none "+
				"(use --mode plan or --mode auto)", mode))
	}

	roots, err := resolveRoots(opts.ConfigDir, opts.Workspace)
	if err != nil {
		return run.Result{}, notStarted(err)
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
	//
	// The hard second interrupt (watchSecondInterrupt) is the one exit that does exactly that, and
	// leaves them there deliberately: the human asked for the process to stop, not for it to tidy
	// up first, and a label walk is exactly the tidying that would hold the shell for seconds. The
	// labels are not lost — a later session reverts them (winlabel.TeardownNotice's own remedy).
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
			return run.Result{}, notStarted(fmt.Errorf(
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

	// The undo stores' own sweep, on the same beat and for the same reason (wire_live.go runs it
	// beside the TUI's session sweep): a headless run opens a snapshot store per record
	// (internal/run) and a host driven only headlessly never passes the TUI's boot, so without this
	// line the stores its earlier runs left behind would accumulate forever. It runs after the
	// session sweep so a record that sweep just pruned is already gone when this one looks for it,
	// and it is handed the always-open store for the reason the session sweep is. No id is kept —
	// this run's own store does not exist yet.
	gcSnapshotDirs(roots.snapshots, sessions, time.Now())

	// Ctrl-C and SIGTERM end the run rather than the process: the cancellation flows out of Once
	// as a run failure carrying whatever the run had reached, so an interrupted run still prints
	// its partial answer and still saves its record.
	//
	// Installed BEFORE raise rather than between its gate and its run, so the composition's one
	// beat of the server is taken under the same ctx the run is: a Ctrl-C during that beat refuses
	// the Firing — the offline gate's own sentence, exit 2, a closing frame — instead of the process
	// dying by signal with nothing said. Deliberately one ctx and not two.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The second press, under `--format json` alone: the escape hatch a stream consumer needs when
	// the polite stop above is taking longer than the human is willing to wait
	// ([watchSecondInterrupt] carries the reasoning). The channel is buffered for the two presses
	// it counts, because signal.Notify drops what a full channel cannot take; the registration and
	// the watch both end with this function, so a run nobody interrupts leaves nothing behind.
	if lines != nil {
		sigs := make(chan os.Signal, 2)
		defer interruptSignals(sigs)()
		done := make(chan struct{})
		defer close(done)
		go watchSecondInterrupt(sigs, done, cmd.ErrOrStderr())
	}

	// The live view of the run, installed by raise on the sink the Config already carries — the
	// Reaction Runner since ADR 0073 — once the Firing is committed to. Everything a headless run
	// REPORTS comes back on Result, but a tool call, a delegation's start and a prune all happen
	// MID-run and leave no trace on the answer, so a human watching an unattended run would otherwise
	// see nothing at all for ten minutes and read it as hung (narrationSink). The sink WRAPS the
	// Runner rather than replacing it, exactly as run.Once's own tap wraps this one in turn
	// (run.Spec). The order that leaves is narration → Reactions → nothing: the renderer sees every
	// Event first, and installing Reactions cannot change what this command prints. It is a pointer
	// because it remembers each sub_agent call for its phase lines.
	//
	// Under `--format json` the encoder goes on TOP of that and never inside it, so the whole chain
	// reads engine → serialEventSink → eventTap → encoder → narration → Reactions. Outermost is the
	// only place it can correctly sit: writing an Event line is lossless and therefore BLOCKING (ADR
	// 0075 decision 9), while a reactions.Runner's Report callback is documented must-not-block
	// (internal/reactions), so an encoder installed inside the Runner would put a blocking stdout
	// write on the one path that promises not to block — and a reader that stopped reading would stall
	// the Reactions. Outermost also makes the stream complete: it sees every Event before any wrapper
	// below it can decide to render, swallow or fail on one.
	//
	// The narration goes quiet in the same breath, for the reason on the type: the prune, the
	// calls and the phases are already their own lines on stdout under json, and each stderr
	// sentence would be the same fact told twice.
	//
	// The opening frame is written HERE and not a line earlier: raise calls this decorator only once
	// its two gates have passed, and a `run_started` ahead of those refusals would announce a run that
	// never happened. What it states is what the run was asked to BE — the bindings and the posture it
	// was composed with — before it has done anything at all. The model is the one the composer
	// actually bound (a per-model rebind may have moved it off the entry's own `model:`), and the
	// version is the full build string `apogee --version` prints, so a consumer can tell which
	// binary produced a stream it is reading back later.
	entry := opts.StartupEntry
	narrate := func(recordID string, cfg apogee.Config, sink domain.EventSink) domain.EventSink {
		var events domain.EventSink = &narrationSink{inner: sink, out: cmd.ErrOrStderr(), quiet: lines != nil}
		if lines == nil {
			return events
		}
		events = lines.Wrap(events)
		lines.RunStarted(eventjson.RunStarted{
			Session:   recordID,
			Workspace: roots.workspace,
			Model:     cfg.Model,
			Server:    entry.Name,
			Mode:      string(mode),
			Bypass:    opts.Bypass,
			Confined:  opts.ConfineToWorkspace,
			Version:   apogee.Version(),
		})
		return events
	}

	// The stream's identity, stamped the moment the run acquires one — raise mints the id and hands it
	// here before it composes anything, so every line written from here on carries it and every line
	// before it carries null: a refusal that happened before the id existed reports honestly that the
	// run had none, rather than being back-dated into one, and a refusal AFTER it (a Config that would
	// not compose, a server that answered nothing) still names the session on its closing frame. The
	// source is raise's own id and never run.Result.SessionID, which --no-save leaves empty on a run
	// that had an id all along (ADR 0075 decision 5).
	onID := func(recordID string) {
		if lines != nil {
			lines.SetSession(recordID)
		}
	}

	// A Reaction's trouble is reported on stderr, beside every other thing this command narrates:
	// stdout is the model's answer and nothing else, and a Reaction that failed is a fact about a
	// script the user configured rather than anything the answer should carry. The same function
	// serves both lanes (firingInputs.report), so one `reactions:` file's trouble reads the same way
	// whichever lane it came from.
	reportReaction := func(line string) { cmd.PrintErrln(line) }

	// The one act every unattended run is (raise, wire_firing.go), reached from this Driver's own
	// inputs: the startup selection as the bound entry, this invocation's roots and mode, and no key
	// resolver, skill catalog or width source of its own — a command that runs once has no
	// longer-lived facility to share, so the composer's own defaults are exactly right here. No
	// Schedule: a headless run belongs to none, so its Reactions stamp none onto their payloads.
	//
	// What comes back beside the Result is the per-model rebind's narration: a built-in Model
	// profile announcing itself, and the roster delta such a profile carries. It goes to stderr,
	// where it cannot contaminate the answer, and it is printed BEFORE the error is read: a refusal
	// still had a composition behind it, and what that composition said stands whether or not the
	// run went on.
	//
	// The interrupt-aware ctx above is the ONE ctx the composition and the run share, which is what
	// makes a Ctrl-C during the composition's beat land as a refusal — the offline gate's own sentence
	// ("… context canceled"), exit 2, the closing frame written — rather than a process dying by
	// signal with nothing said.
	res, notices, runErr := raise(ctx, firingInputs{
		opts:     *opts,
		entry:    entry,
		roots:    roots,
		confiner: confiner,
		mode:     mode,
		report:   reportReaction,
	}, prompt, nil, store, onID, narrate)
	for _, n := range notices {
		cmd.PrintErrln(n)
	}

	// A refusal that stopped the Firing before it was raised is exit 2, not exit 1: the composition
	// would not produce a Config, or the bound server answered NOTHING — Beat.Answered false only for
	// a transport-level failure, never for a 401, a 404 or a 429, which are answers this Driver cannot
	// judge (raise carries the gate's whole reasoning). Nothing was sent, no session record was
	// written and no token was spent, so what a script must do about it is fix the invocation, not
	// read an outcome. The sentence is the composer's own, verbatim (errNotStarted), and for the gate
	// it is notice.ServerOffline — the one composer all three Drivers read, so a human who has seen a
	// session refuse a send reads the same sentence from an unattended run.
	var refused errNotStarted
	if errors.As(runErr, &refused) {
		return run.Result{}, notStarted(runErr)
	}

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
		return res, notStarted(friendlyConstructErr(runErr))
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
	//
	// It is skipped entirely under `--format json`: there stdout IS the Event lines and nothing
	// else (ADR 0075 decision 6), the answer already rides them twice over — every `message` line
	// and the closing frame's `final_text` — and a raw line printed into the middle of a JSONL
	// stream would break every consumer of it.
	if lines == nil {
		if text := sanitize.StripEscapes(res.FinalText); text != "" {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), text)
		}
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
	// And the command that puts those changes back, when this run left a journal that outlives it:
	// the report is only half an account if the human who reads it has no way to act on it.
	if line := undoVerbLine(res); line != "" {
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
		// than guess at it (partialRunSuffix, the wording every Driver's failure carries).
		if res.SessionID != "" {
			return res, runFailed(fmt.Errorf("%w %s", runErr, partialRunSuffix(res.SessionID)))
		}
		return res, runFailed(runErr)
	}
	// An abandoned final Turn is exit 3, and it is decided AFTER the failure branch above on
	// purpose: a run that errored is exit 1 whether or not it also faulted, because the error is
	// the more actionable of the two. What reaches here is a run that returned no error and still
	// has no answer — the answer text above is the run's last words, so the error names the fault
	// the engine reported (run.Result.Fault) and, like a failure, the record it can be read in.
	if res.Faulted {
		err := fmt.Errorf("apogee headless: the run's final turn was abandoned — %s", res.Fault)
		if res.SessionID != "" {
			err = fmt.Errorf("%w %s", err, partialRunSuffix(res.SessionID))
		}
		return res, exitError{code: exitRunFaulted, err: err}
	}
	return res, nil
}

// serialWriter guards one io.Writer with a mutex, so goroutines that narrate at the same time can
// neither race on it nor split each other's lines. One Write is one line here: Cobra's Print
// helpers format the whole line before they write it, so the lock is never taken mid-line.
type serialWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// Write hands p to the wrapped writer with the lock held.
func (s *serialWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
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
// `skip` — which is the opposite account of the one this block gives. These lines say what
// happened; the revert both Drivers can offer is its own line beneath them (undoVerbLine).
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

// undoVerbLine offers the revert this Firing's writes can still have: the exact
// `apogee undo <session-id>` command, indented under the written-files block both unattended
// Drivers print above it. It is the other half of ADR 0074's persistence — the journal now outlives
// the process that wrote it, so a human reading a report after the fact has somewhere to go — and
// it is composed once here for the same reason the block above it is: the daemon's log and the
// headless stderr must name one command, not two spellings of it.
//
// The command it names, and the gate that decides whether there is one, are undoCommand's — this
// line is that command dressed for a report, escape-stripped to one line because the id is the
// record's own and both sinks are one-line-per-entry.
func undoVerbLine(res run.Result) string {
	command := undoCommand(res)
	if command == "" {
		return ""
	}
	return "  undo with: " + sanitize.StripEscapesToLine(command)
}

// undoCommand is the exact `apogee undo <session-id>` that reverts this Firing's writes, or empty
// when there is no revert to offer. It is the ONE gate behind every offer of the verb — the line
// both unattended Drivers print (undoVerbLine) and the Outcome every Firing reports
// (firingOutcome, wire_firing.go) — so a surface never names a command the report would not.
//
// Three conditions, all of them necessary. There is a change to revert (the run wrote something),
// a record to name (an unsaved run's store is nameless and was swept), and run.Result.UndoNote is
// EMPTY — the note is why the journal was the in-memory funnel one, whose records this process
// alone ever held, so offering a verb against it would send a human to a command that answers
// "nothing to undo". Any one of the three missing composes nothing at all.
//
// RAW: the id crosses as the record carries it, and each surface strips at its own render seam.
func undoCommand(res run.Result) string {
	if len(res.Wrote) == 0 || res.SessionID == "" || res.UndoNote != "" {
		return ""
	}
	return "apogee undo " + res.SessionID
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
// delegation written before the name argument existed, and every one a Reaction synthesises. The
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
// (headlessSubAgentTarget): it stands in the same slot, and a name is not licence to be wider. So
// is the live narration's summary and error text (narrationLine): one width for every label this
// Driver prints beside a tool or a delegation.
func clipSubAgentTask(task string) string {
	runes := []rune(task)
	if len(runes) <= headlessTaskMax {
		return task
	}
	return string(runes[:headlessTaskMax-1]) + "…"
}
