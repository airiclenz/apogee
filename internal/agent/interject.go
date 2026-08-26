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
// guard a single scalar behind a mutex): it appends to the conversation, which the driving
// goroutine owns outright, so the boundary IS the synchronization. Calling it while a Step
// is in flight races the loop's own history writes.
//
// The message lands marked domain.Message.Interjected, so the derived Exchange opening —
// and every Mechanism reading it — does not move: the remark joins the running Exchange's
// body instead of starting a new one (domain.CurrentExchange). It reaches the model on the
// next Turn's request, after the tool results already in the tail; that user-after-tool
// shape is legal OpenAI chat but breaks strict Gemma-class templates, an accepted posture
// recorded in ADR 0025.
//
// Attached skills and @file references resolve HERE, at delivery — so a file the model just
// rewrote is read as it stands now, not as it stood when the human typed the remark. An
// unresolvable reference is reported as an ErrorEvent and skipped, exactly as it is for a
// Submitted message (resolveFileRefs / resolveSkillRefs).
//
// It refuses with domain.ErrNoOpenExchange when no Exchange is in flight (the caller wants
// Submit) and with an empty-interjection error when in carries no text, references, or
// skills. On either refusal the conversation is untouched.
//
// Fates: an interjection is committed history, so it survives a cancelled Turn (the
// rollback boundary is armed after this window — turn.go) and rides snapshot/resume with
// its marker intact. AbortExchange discards it along with the rest of the scrapped
// Exchange, which is the point: the human threw the whole Exchange away.
func (a *Agent) Interject(in domain.UserInput) error {
	if !a.turns.inExchange {
		return domain.ErrNoOpenExchange
	}
	if in.Text == "" && len(in.FileRefs) == 0 && len(in.SkillIDs) == 0 {
		return errEmptyInterjection
	}

	// Mirrors step()'s pending-input consumption (loop.go) minus openExchange — the Exchange
	// is already open and its cached boundary must not move. Same block order, so an
	// interjection reads identically to an opening message: skills → @file refs → the text.
	turn := a.turns.index
	skillBlocks := a.resolveSkillRefs(turn, in.SkillIDs)
	// context.Background, not the running Turn's: an interjection is delivered on the HOST's
	// goroutine (a human typed it), it commits before the Turn it steers is cancellable from
	// here, and the Turn's own context is not in scope at this seam. Document extraction is still
	// bounded — the page, read and output bounds are the extractor's own and need no context.
	refs := a.resolveFileRefs(context.Background(), turn, in.FileRefs)
	a.conv.Append(domain.Message{
		Role:        domain.RoleUser,
		Content:     skillBlocks + refs + in.Text,
		Interjected: true,
	})
	return nil
}
