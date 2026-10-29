package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/webhook"
)

// The SYNC lane's out-of-process executor: the one door a user's `advise:` or `gate:` command is
// spawned through, its webhook POSTed through and its MCP tool called through, while the loop waits
// on it (ADR 0076 D2, D7, D8, adr-0076-amend-1029; docs/design/confinement-execution-contract.md
// §10.4).
//
// It is deliberately ONE deep module. Everything a sync reaction's command needs settling —
// which permit it spawns under, whether a confinement box has to be built first, what deadline
// it dies at, how its document reaches it, and what a failure is reported as — is settled here,
// and the seams that call it (the advise slot at post-tool-result, the gate stage at
// pre-tool-exec) see nothing but `(stdout, err)`. Neither seam should have to know that a permit
// exists, and neither should be able to spawn a command any other way. The webhook door beside it
// (runSyncWebhook) sends the SAME document as the POST body and hands the seam the reply body on
// the same terms; it takes no permit, because nothing is spawned — the request leaves the process
// over the network, as the observe lane's webhooks already do. The MCP door (runSyncMCP) sends the
// same document under the entry's payload argument and hands the seam the tool's text answer, also
// without a permit: the call travels over the Driver's live server session, and nothing is spawned.
//
// An adopted repo reaction's observe-lane `run:` is the one observe command spawned here too
// (RunProjectReaction, ADR 0104 §8): it runs under the same permit row through the same door, so
// a repo command is fenced the same way whichever lane fires it.

// runSyncArgv runs one sync-lane reaction's command and returns what it wrote to standard output.
//
// The command is spawned through tools.RunHookSubprocess — the same funnel every execution tool
// goes through — so it gets the credential scrub, the process-tree teardown, the output cap, the
// timeout clamp and the argv[0] fence for free rather than through a hand-rolled exec.Command
// that has none of them. What this function adds on top is the three facts the funnel cannot
// know: the permit (syncPermitCtx), the class default deadline, and the payload document.
//
// The caller fills the MOMENT's half of doc — the event, the tool, the path, the arguments, the
// result — and this function stamps the IDENTITY half over it (syncDocument): which reaction
// fired, when, in which workspace, and at what depth, turn and call id.
//
// The error is the caller's whole account of a failure: a refused permit, a timeout, a non-zero
// exit, a cancelled context and a wedged output pipe all arrive as one, and the funnel's message
// already quotes the command's own diagnostics. It passes through VERBATIM — the classes read it
// differently (a gate escalates to ask, an advise reaction contributes nothing and the Turn goes
// on) and neither reading belongs to the executor.
func (a *Agent) runSyncArgv(
	ctx context.Context,
	turn int,
	r domain.Reaction,
	doc domain.SeamPayload,
) (string, error) {
	handler, ok := r.Handler.(domain.ArgvHandler)
	if !ok {
		return "", fmt.Errorf("apogee: reaction %q: the sync lane runs a command, not a %T", r.ID, r.Handler)
	}
	// Unreachable from config: domain.Reaction.Validate refuses an empty or blank-first-element
	// argv at load. The branch stays for a Reaction built in code that skipped Validate.
	if len(handler.Argv) == 0 {
		return "", fmt.Errorf("apogee: reaction %q: run: is empty", r.ID)
	}

	ctx, err := a.syncPermitCtx(ctx)
	if err != nil {
		return "", err
	}

	doc, document, err := a.syncDocument(turn, r, doc)
	if err != nil {
		return "", err
	}

	return a.spawnReactionArgv(ctx, r, handler.Argv, doc.Env(), syncTimeout(r), string(document))
}

// RunProjectReaction is the fenced runner an adopted repo reaction's observe-lane `run:` spawns
// through (domain.ProjectRun, ADR 0104 §8): the same permit row as a sync reaction's
// (syncPermitCtx — inside the workspace exec fence when `confine-to-workspace` is on, refused on a
// host that cannot confine, unfenced when it is off) and the same door (spawnReactionArgv), so the
// observe lane mints no permit of its own. The Driver's Runner reaches it through
// Config.BindProjectRun. stdin is the payload document the Runner encoded, env its headline facts,
// and the deadline the entry's own `timeout:`, which the context the Runner hands in already
// carries. What the command printed is dropped: nothing an observe reaction prints reaches anyone.
func (a *Agent) RunProjectReaction(ctx context.Context, r domain.Reaction, stdin string, env []string) error {
	handler, ok := r.Handler.(domain.ArgvHandler)
	if !ok || len(handler.Argv) == 0 {
		return fmt.Errorf("apogee: reaction %q: no command to run", r.ID)
	}
	ctx, err := a.syncPermitCtx(ctx)
	if err != nil {
		return err
	}
	_, err = a.spawnReactionArgv(ctx, r, handler.Argv, env, r.Timeout, stdin)
	return err
}

// bindProjectRun hands this Agent's fenced runner to the Driver that asked for it
// (Config.BindProjectRun). New and Resume call it once the Agent is built; a sub-agent never does,
// so the runner a Driver's Runner holds is always its top-level Agent's.
func (a *Agent) bindProjectRun() {
	if bind := a.cfg.BindProjectRun; bind != nil {
		bind(a.RunProjectReaction)
	}
}

// spawnReactionArgv is the one call a user reaction's command reaches tools.RunHookSubprocess
// through, on a ctx that already carries its permit. A global reaction runs in apogee's own working
// directory — its payload names the workspace by path — under the workspace program fence. An
// adopted repo reaction (domain.Reaction.Project) is admitted through its pin first
// (checkProjectPin, which cues the Driver on a refusal), then runs in the admitted launch's
// directory — the Project root, also the base of a relative argv[0] — and may run a program inside
// the fence only when it is one of the programs the launch admits (ADR 0104 §4).
func (a *Agent) spawnReactionArgv(
	ctx context.Context,
	r domain.Reaction,
	argv []string,
	env []string,
	timeout time.Duration,
	stdin string,
) (string, error) {
	dir := ""
	var opts []tools.HookOption
	if r.Project {
		launch, err := a.checkProjectPin(r)
		if err != nil {
			return "", err
		}
		dir = launch.Dir
		opts = append(opts, tools.AdmitProjectPrograms(dir, launch.Programs))
	}
	return tools.RunHookSubprocess(ctx, argv, dir, a.cfg.WorkspaceDir, a.cfg.SecretEnvNames(), env, timeout, stdin, opts...)
}

// checkProjectPin is the spawn-time check of an adopted repo reaction (ADR 0104 §4): the launch its
// pin admits when every project file the pin holds is still what the user adopted. A Project
// reaction with no pin, and any refusal the pin's Admit returns, refuse the spawn — none of them is
// a command the user said yes to. A *domain.PinMovedError (a moved file, or files that could not be
// checked) also tells the Driver (Config.PinMoved): the entry's files are no longer what the user
// adopted, so the config layer now proposes it again and a pane can ask. A missing pin is no moved
// file — a re-read would find nothing new to ask about.
func (a *Agent) checkProjectPin(r domain.Reaction) (domain.PinLaunch, error) {
	if r.Pin == nil {
		return domain.PinLaunch{}, fmt.Errorf(
			"apogee: reaction %q: refusing to run — a project reaction carries no adoption pin",
			r.ID,
		)
	}
	launch, err := r.Pin.Admit()
	if err != nil {
		var moved *domain.PinMovedError
		if errors.As(err, &moved) {
			a.notePinMoved()
		}
		return domain.PinLaunch{}, fmt.Errorf("apogee: reaction %q: refusing to run — %w", r.ID, err)
	}
	return launch, nil
}

// notePinMoved hands the Driver its re-ask cue (Config.PinMoved), when it asked for one.
func (a *Agent) notePinMoved() {
	if moved := a.cfg.PinMoved; moved != nil {
		moved()
	}
}

// runSyncWebhook POSTs one sync-lane reaction's document to its webhook and returns the reply body,
// read up to replyCap bytes — the caller's bound, because what a reply is worth is the class's
// business: an advise reply is a trailer the downstream cap truncates, a gate reply is one verdict
// line. The document is the one runSyncArgv writes to a command's stdin, identity half stamped
// here too, so a script that learned to read it from a command reads the identical JSON off the
// request body.
//
// No permit is taken: nothing is spawned, so the confinement contract's spawn posture
// (docs/design/confinement-execution-contract.md §10.4) has nothing to say about the request, and
// the deadline is the class's (syncTimeout) as it is for a command. The post goes through the
// url-safety guard built from the run's own allow/deny host lists, as the observe lane's does. Every
// failure — an unset `headers-env:` variable, a url-safety refusal, a transport error, the deadline,
// a non-2xx status (a redirect among them: it is never followed) — arrives as one error
// worded by internal/webhook (never the URL, which may carry a token), for the classes to read as
// they read a command's: an advise reaction contributes nothing, a gate escalates to ask.
func (a *Agent) runSyncWebhook(
	ctx context.Context,
	turn int,
	r domain.Reaction,
	doc domain.SeamPayload,
	replyCap int64,
) (string, error) {
	handler, ok := r.Handler.(domain.WebhookHandler)
	if !ok {
		return "", fmt.Errorf("apogee: reaction %q: the sync lane POSTs to a webhook, not a %T", r.ID, r.Handler)
	}

	_, document, err := a.syncDocument(turn, r, doc)
	if err != nil {
		return "", err
	}

	response, err := webhook.Post(ctx, security.NewURLGuard(a.cfg.URLAllowHosts, a.cfg.URLDenyHosts), handler,
		syncTimeout(r), document)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()

	reply, err := io.ReadAll(io.LimitReader(response.Body, replyCap))
	if err != nil {
		return "", webhook.PostFailure(syncTimeout(r), err)
	}
	return string(reply), nil
}

// runSyncMCP calls one sync-lane reaction's MCP tool and returns its text answer, which the seam
// reads exactly as it reads a command's stdout (adr-0076-amend-1029). The arguments are the entry's
// static Args plus, under its PayloadArg, the document runSyncArgv writes to a command's stdin —
// identity half stamped here too — as a JSON object, so a tool reads the same fields a script does.
//
// The user authored the call, so it never reaches the Approver; nothing is spawned, so no permit is
// taken and `confine-to-workspace` has nothing to fence; the deadline is the class's (syncTimeout),
// as it is for a command or a webhook. An adopted repo entry passes its pin first (checkProjectPin),
// exactly as its command would, and is resolved as a project entry, so the resolver refuses a server
// its own Project config does not declare.
//
// Every failure is one error for the classes to read as they read a command's: an advise reaction
// contributes nothing, a gate escalates to ask. A failure to RESOLVE the entry — no resolver
// injected, the server not connected, the tool missing, the wrong layer — comes back as an
// *mcpUnresolvedError, which reportSyncFailure reports once per entry rather than on every firing:
// a server that is down stays down for a while, and the call failures that follow a live
// resolution — the tool's isError, a timeout, a transport failure — are reported every time.
func (a *Agent) runSyncMCP(
	ctx context.Context,
	turn int,
	r domain.Reaction,
	doc domain.SeamPayload,
) (string, error) {
	handler, ok := r.Handler.(domain.MCPHandler)
	if !ok {
		return "", fmt.Errorf("apogee: reaction %q: the sync lane calls an MCP tool, not a %T", r.ID, r.Handler)
	}
	resolve := a.cfg.MCP
	if resolve == nil {
		return "", &mcpUnresolvedError{}
	}
	if r.Project {
		if _, err := a.checkProjectPin(r); err != nil {
			return "", err
		}
	}
	call, err := resolve(handler.Server, handler.Tool, r.Project)
	if err == nil && call == nil {
		err = errors.New("mcp: the resolver returned no call")
	}
	if err != nil {
		return "", &mcpUnresolvedError{err: err}
	}

	_, document, err := a.syncDocument(turn, r, doc)
	if err != nil {
		return "", err
	}
	args, err := mcpArguments(r.ID, handler, document)
	if err != nil {
		return "", err
	}

	timeout := syncTimeout(r)
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	answer, err := call(callCtx, args)
	if err != nil {
		if ctx.Err() == nil && errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("timed out after %s", timeout)
		}
		return "", err
	}
	return answer, nil
}

// mcpArguments is the JSON object an MCP reaction's tool is called with: the handler's static Args,
// and — when it names a PayloadArg — the stamped seam document under that name. Validate refuses a
// PayloadArg that Args already holds, so at load the payload never overwrites a static argument. An
// entry with neither sends the empty object.
func mcpArguments(id string, handler domain.MCPHandler, document []byte) (json.RawMessage, error) {
	args := make(map[string]any, len(handler.Args)+1)
	maps.Copy(args, handler.Args)
	if handler.PayloadArg != "" {
		args[handler.PayloadArg] = json.RawMessage(document)
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("apogee: reaction %q: mcp args: %w", id, err)
	}
	return encoded, nil
}

// mcpUnresolvedError is runSyncMCP's failure to turn an MCP entry into a call. err is the
// resolver's refusal; nil means no resolver is injected at all — a Driver that dials no MCP server,
// whose one notice per entry is the Driver's to give (adr-0076-amend-1029), so reportSyncFailure
// reports nothing for it.
type mcpUnresolvedError struct {
	err error
}

func (e *mcpUnresolvedError) Error() string {
	if e.err == nil {
		return mcpNotConnected
	}
	return e.err.Error()
}

func (e *mcpUnresolvedError) Unwrap() error { return e.err }

// mcpNotConnected is the failure an MCP entry reads in a Driver that dials no MCP server — the
// reason an unanswered gate's Approval prompt names.
const mcpNotConnected = "MCP servers are not connected in this run"

// reportSyncFailure is how a sync-lane seam reports a handler failure: reportReaction, behind the
// MCP resolution rule. A failure from a Driver with no resolver is not reported here at all — the
// Driver's own notice owns it — and a resolver's refusal is reported the FIRST time an entry meets
// it on this Agent (mcpUnresolved), since it would otherwise repeat on every firing until the server
// returns. Every other failure is reported every time.
func (a *Agent) reportSyncFailure(turn int, r domain.Reaction, m domain.Moment, err error) {
	var unresolved *mcpUnresolvedError
	if errors.As(err, &unresolved) && (unresolved.err == nil || !a.mcpUnresolved.first(r.ID)) {
		return
	}
	a.reportReaction(turn, r.ID, m, err)
}

// reportedOnce is a set of reaction ids, each admitted once. It is safe for concurrent use — a
// pooled dispatch gates several calls at once — and its zero value is ready, allocated on first use
// under its own mutex.
type reportedOnce struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

// first reports whether id is new to the set, adding it. A nil set admits every id.
func (s *reportedOnce) first(id string) bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[id]; ok {
		return false
	}
	if s.seen == nil {
		s.seen = make(map[string]struct{})
	}
	s.seen[id] = struct{}{}
	return true
}

// syncDocument stamps the IDENTITY half of one sync-lane document — which reaction fired, when, in
// which workspace, and at what depth, turn and call id — over the MOMENT's half the seam filled,
// and renders the JSON every door sends. Those are facts of the firing agent rather than of the
// Moment, so a seam that forgets one cannot ship a document that misreports the run. The stamped
// document is returned beside its encoding because a command's environment (SeamPayload.Env) is
// derived from the same fields.
func (a *Agent) syncDocument(
	turn int,
	r domain.Reaction,
	doc domain.SeamPayload,
) (domain.SeamPayload, []byte, error) {
	doc.Reaction = r.ID
	doc.Time = a.now().Format(time.RFC3339)
	doc.Workspace = a.cfg.WorkspaceDir
	doc.Depth = a.depth
	doc.Turn = turn
	doc.CallID = a.callID

	document, err := json.Marshal(doc)
	if err != nil {
		return doc, nil, fmt.Errorf("apogee: reaction %q: %w", r.ID, err)
	}
	return doc, document, nil
}

// syncTimeout is the deadline one sync-lane reaction's command or request runs under: its entry's own
// `timeout:` when it set one, else the class default (domain.DefaultAdviseTimeout /
// domain.DefaultGateTimeout). A class outside the sync lane cannot reach here — Generation.Validate
// refuses it — so the advise default is the honest fallback for anything that is not a gate.
func syncTimeout(r domain.Reaction) time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	if r.Class == domain.ClassGate {
		return domain.DefaultGateTimeout
	}
	return domain.DefaultAdviseTimeout
}

// reportReaction is what the sync lane does with a failure: one line to the user through the
// Driver's report seam, and one booked firing for anything watching the Event stream.
//
// The two halves are deliberately different audiences. The LINE is the operator's — it is the
// observe lane's own sentence, `reaction <id> (<moment>): <error>` (internal/reactions' runner.go),
// so a user whose command is broken reads the same report whichever lane it fired on — and a nil
// Config.Report drops it. The FIRING is the Event stream's, booked with Action "failed" and the
// error as its Detail; it is never an ErrorEvent, because a user's command failing is that
// reaction doing nothing rather than a fault of the engine's, and the Turn carries on regardless.
func (a *Agent) reportReaction(turn int, id string, m domain.Moment, err error) {
	if err == nil {
		return
	}
	if a.cfg.Report != nil {
		a.cfg.Report(fmt.Sprintf("reaction %s (%s): %v", id, m, err))
	}
	a.cfg.Events.Emit(domain.ReactionFiredEvent{
		EventBase: a.base(turn),
		Reaction:  id,
		Origin:    domain.OriginUser,
		Moment:    m,
		Action:    actionFailed,
		Detail:    err.Error(),
	})
}

// actionFailed is the Action a firing is booked under when a sync-lane reaction's command could
// not produce an answer at all. It sits beside the cascade's own action vocabulary
// (reactions.go's firedAction / actionDefer) for the same reason those share one spelling: a
// reader of a ReactionFiredEvent should not have to know which lane the reaction came from to
// read what happened to it.
const actionFailed = "failed"
