package agent

import (
	"context"
	"errors"

	"github.com/airiclenz/apogee/internal/domain"
)

// errEmptyInterjection refuses an interjection that would commit nothing — no text, no
// @file references, no attached skills. It is unexported because the condition is a host
// bug rather than a state the caller negotiates: an empty remark would land as a blank
// user message wedged after the tool results, which is noise on the wire and invisible in
// the transcript. The TUI never produces one (a blank input stages no row); an embedder
// gets a loud error instead of a silent no-op.
var errEmptyInterjection = errors.New("apogee: interjection is empty")

// Interject commits a user message into the OPEN Exchange at the between-Steps boundary —
// the human's remark reaching the model mid-task rather than waiting for the Exchange to
// end. It is the engine half of the TUI's interjection queue: the host stages what the
// human types while the model works and delivers it here, between Steps of the Exchange it
// is driving.
//
// Contract: call it ONLY from the goroutine driving Step, between Steps — the same class
// the worker's Snapshot call already occupies (ADR 0025). It is deliberately NOT an
// anytime-goroutine-safe mutator (not the SetMode / SetConfineToWorkspace class, which
// move live state behind the mutex that guards it — the Agent type's doc lists them): it appends to the conversation, which the driving
// goroutine owns outright, so the boundary IS the synchronization. Calling it while a Step
// is in flight races the loop's own history writes.
//
// The message lands marked domain.Message.Interjected, so the derived Exchange opening —
// and every Reaction reading it — does not move: the remark joins the running Exchange's
// body instead of starting a new one (domain.CurrentExchange). It reaches the model on the
// next Turn's request, after the tool results already in the tail; that user-after-tool
// shape is legal OpenAI chat but breaks strict Gemma-class templates, an accepted posture
// recorded in ADR 0025.
//
// Attached skills and @file references resolve HERE, at delivery — so a file the model just
// rewrote is read as it stands now, not as it stood when the human typed the remark. An
// unresolvable reference is reported as an ErrorEvent and skipped, exactly as it is for a
// Submitted message (resolveFileRefs / resolveSkillRefs). ctx bounds ONLY that resolution work
// — the caller passes the context of the Step it is about to make, so a cancelled Step stops a
// document extraction mid-walk (resolveFileRefs) instead of finishing one nobody will read. It
// changes nothing about the return: a reference the cancel cut short is skipped with the same
// refIgnored ErrorEvent a Submitted message gets, and the remark still commits — a cancelled ctx
// is not a third refusal, so a Driver's drain (deliverInterjections, drainMailbox) never stops on
// one.
//
// It refuses with domain.ErrNoOpenExchange when no Exchange is in flight (the caller wants
// Submit), with an empty-interjection error when in carries no text, references, or
// skills, and with errRecipeInterjection when in would launch a recipe (recipe.go). On any
// refusal the conversation is untouched.
//
// Fates: an interjection is committed history, so it survives a cancelled Turn (the
// rollback boundary is armed after this window — turn.go) and rides snapshot/resume with
// its marker intact. AbortExchange discards it along with the rest of the scrapped
// Exchange, which is the point: the human threw the whole Exchange away — except a workflow
// finish note TakeWorkflowNotes handed over, which the abort holds again for the next Exchange
// (Agent.exchangeAborted): the human threw away the Exchange, not a note the model never answered.
//
// What a STAGED message does before it reaches here is the host's to say through
// Config.InterjectionPending: while it answers true, the delegations of the running group
// that have not started are skipped (dispatch.go, skipDelegation) so the boundary this
// call needs arrives when the children already running finish, instead of after every
// queued one. That seam is a predicate, not a drain — the message itself still commits
// only here, at the boundary, on the driving goroutine.
func (a *Agent) Interject(ctx context.Context, in domain.UserInput) error {
	if !a.turns.inExchange {
		return domain.ErrNoOpenExchange
	}
	if in.Text == "" && len(in.FileRefs) == 0 && len(in.SkillIDs) == 0 {
		return errEmptyInterjection
	}
	// A recipe launch opens an Exchange of its own around the Workflow it runs (recipe.go), so it
	// cannot join one already running; the human sends it once the agent is idle.
	if _, launches := a.recipeLaunch(in); launches {
		return errRecipeInterjection
	}

	// The same composition step()'s pending-input consumption uses (composeUserMessage, loop.go)
	// minus openExchange — the Exchange is already open and its cached boundary must not move —
	// so an interjection reads identically to an opening message, splits one structural bound
	// across its whole reference set the same way, and is bounded by the same ctx: the caller's,
	// the Step's, so the one cancel the host holds reaches both paths. A cancelled ctx here only
	// skips the document (refIgnored); the commit is unconditional either way.
	a.conv.Append(a.composeUserMessage(ctx, a.turns.index, in, true))
	return nil
}

// TakeWorkflowNotes hands the Driver the finish notes of the background workflows that ended while
// the OPEN Exchange runs (ADR 0089 D3), as one interjection for it to commit with Interject at the
// same between-Steps boundary — the drain its human remarks go through (ADR 0025), so the engine
// consumes no slot of its own for them. The notes are taken: a second call returns nothing until
// another workflow ends. It reports false, and takes nothing, when no Exchange is open (the idle
// agent is woken on them instead — Wake — or the next opening message carries them) or no note is
// held. The notes are recorded as delivered into the open Exchange, so an abort that scraps it holds
// them again (Agent.exchangeAborted). Call it where Interject is called: on the goroutine driving
// Step, between Steps.
func (a *Agent) TakeWorkflowNotes() (domain.UserInput, bool) {
	if !a.turns.inExchange {
		return domain.UserInput{}, false
	}
	notes := a.background.takeIntoExchange()
	if len(notes) == 0 {
		return domain.UserInput{}, false
	}
	return domain.UserInput{Text: renderWorkflowNotes(notes)}, true
}
