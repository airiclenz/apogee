package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/webhook"
)

// The SYNC lane's out-of-process executor: the one door a user's `advise:` or `gate:` command is
// spawned through, and its webhook POSTed through, while the loop waits on it (ADR 0076 D2, D7,
// D8; docs/design/confinement-execution-contract.md §10.4).
//
// It is deliberately ONE deep module. Everything a sync reaction's command needs settling —
// which permit it spawns under, whether a confinement box has to be built first, what deadline
// it dies at, how its document reaches it, and what a failure is reported as — is settled here,
// and the seams that call it (the advise slot at post-tool-result, the gate stage at
// pre-tool-exec) see nothing but `(stdout, err)`. Neither seam should have to know that a permit
// exists, and neither should be able to spawn a command any other way. The webhook door beside it
// (runSyncWebhook) sends the SAME document as the POST body and hands the seam the reply body on
// the same terms; it takes no permit, because nothing is spawned — the request leaves the process
// over the network, as the observe lane's webhooks already do.

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

	return tools.RunHookSubprocess(
		ctx,
		handler.Argv,
		"", // apogee's own working directory: the payload names the workspace by path
		a.cfg.WorkspaceDir,
		a.cfg.SecretEnvVars,
		doc.Env(),
		syncTimeout(r),
		string(document),
	)
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
// the deadline is the class's (syncTimeout) as it is for a command. Every failure — an unset
// `headers-env:` variable, a transport error, the deadline, a non-2xx status — arrives as one error
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

	response, err := webhook.Post(ctx, handler, syncTimeout(r), document)
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

// syncDocument stamps the IDENTITY half of one sync-lane document — which reaction fired, when, in
// which workspace, and at what depth, turn and call id — over the MOMENT's half the seam filled,
// and renders the JSON both doors send. Those are facts of the firing agent rather than of the
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
