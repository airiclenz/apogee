package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

var askUserSpec = toolSpec{
	name:        "ask_user",
	description: "Ask the human a free-text question and get their answer. Use this for a clarification or a decision only the user can make. It is not a tool-approval prompt; it is a direct question to the person. Optionally pass `choices` to offer a few answer options the human can pick from; they may still type a custom answer instead. When several choices could apply at once, set multi_select to true so the human can pick more than one. Never repeat the question or the choices in your own message before calling — the tool shows them to the human and the transcript keeps a record after they answer; restating them wastes tokens.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["question"],
  "properties": {
    "question": {"type": "string", "description": "The question to ask the human. Use this when you need a clarification or a decision only the user can make."},
    "choices": {"type": "array", "items": {"type": "string"}, "description": "Optional: 2-5 answer options to offer when the question has a natural closed set. An option may be a full sentence — the prompt wraps it — so say what the option MEANS rather than compressing it to a word. The human can always type a custom free-text answer instead, so choices never gate the reply."},
    "multi_select": {"type": "boolean", "description": "Optional: set true when several of the choices may apply at once. The human can then select any number of them, and the answer returns each chosen option on its own line. Leave it unset for a single-answer question."}
  }
}`),
}

type askUserArgs struct {
	Question    string   `json:"question"`
	Choices     []string `json:"choices"`
	MultiSelect bool     `json:"multi_select"`
}

// AskUser asks the human a free-text question mid-task and returns their typed answer. It
// routes through the host-supplied Asker delegate (the public analogue of Approver, but for
// free-text Q&A — NOT a safety gate), so it is mode-INDEPENDENT: it always goes to the Asker,
// never through the Approval/disposition gate. It is ReadOnly() — asking a question writes
// nothing — so the disposition runs it freely in every mode, INCLUDING Plan. It is NOT an
// ExternalEffectTool (the human is not a non-forkable external service to stub).
//
// A nil Asker means the tool is never registered (NewDefaultRegistryWithHost omits it), so by
// construction Execute always has a non-nil Asker; the defensive nil-check below keeps a
// hand-built registry that registers it with a nil Asker from panicking. Stateless across
// Turns (ADR 0008): a fresh question per call, and nothing about one question survives into the
// next. The one thing it does hold is the queueing seam's slot (queuedAsks), which is not Turn
// state at all — it is the ONE prompt surface, held by whichever concurrent caller is currently in
// front of the human and empty the rest of the time.
type AskUser struct {
	toolSpec
	asker domain.Asker
}

// NewAskUser returns an ask_user tool routing to asker through the queueing seam that keeps the
// host to one question at a time (queuedAsks). A nil asker yields a tool whose Execute reports the
// delegate is unavailable (the registry omits it in practice).
//
// The seam is installed HERE — at the tool — rather than at Agent construction where the Approval
// one is (internal/agent, queuedApprovals), because this is where the Asker enters the engine: the
// loop consults the Approver itself, while it never touches the Asker at all. A host that builds
// its own registry (cmd/apogee does, to register MCP tools on top) hands its raw Asker to this
// constructor and to nothing else, so a seam installed anywhere else would be dead in exactly the
// Driver that needs it. One tool instance is one slot, and a sub-agent runs on a SUBSET of its
// parent's registry (the same *AskUser), so a whole Agent tree queues against that one slot.
func NewAskUser(asker domain.Asker) *AskUser {
	return &AskUser{toolSpec: askUserSpec, asker: queuedAsks(asker)}
}

// queuedAsker queues concurrent Ask calls onto one host Asker: it holds a single slot — "the
// question on the screen" — and admits one caller at a time. It is the engine's half of the Asker
// contract and the exact counterpart of internal/agent's queuedApprover: once a depth-0 fan-out is
// running (ADR 0039), several children can call ask_user at the same instant, and a host that has
// only ever fielded one question at a time must not have to grow a queue of its own. Without it the
// second question simply replaces the first on the Driver's single prompt surface and the first
// child's reply channel is orphaned — that child blocks until the Turn is cancelled.
//
// The wait is a channel rather than a mutex because it has to be ctx-AWARE. A sibling queued behind
// the visible question must be able to give up when the human cancels the Turn; under a mutex it
// could not — it would sit in Lock until the question ahead of it was answered and only THEN hand
// the driver a question belonging to a Turn that has already rolled back. Cancelled-while-queued
// returns exactly what a cancelled visible question returns, (no answer, ctx.Err()), which Execute
// turns into the Go error that rolls the Turn back (ADR 0007).
//
// The slot is held for the whole of the host's Ask — the human's entire deliberation — which is the
// point: the queue behind it is what "one question at a time" means, and the wait-tolerance
// invariant (ADR 0031) is what makes an unbounded hold legitimate. Which queued sibling is admitted
// next is deliberately unspecified; what is guaranteed is that exactly one is inside the host Asker
// at a time and that every other child goes on working while it waits.
type queuedAsker struct {
	slot  chan struct{} // capacity 1: the one in-flight question
	inner domain.Asker
}

func (q *queuedAsker) Ask(ctx context.Context, req domain.AskRequest) (domain.AskAnswer, error) {
	select {
	case q.slot <- struct{}{}:
	case <-ctx.Done():
		return domain.AskAnswer{}, ctx.Err()
	}
	defer func() { <-q.slot }()
	return q.inner.Ask(ctx, req)
}

// queuedAsks wraps ap in the queueing seam, unless it already IS one. The idempotence keeps a
// re-wrapped Asker on the SAME slot rather than stacking a private, uncontended one in front of the
// first — the property internal/agent's queuedApprovals relies on to keep a whole Agent tree in one
// queue, held here for the same reason a registry may be built from an already-wrapped delegate.
//
// A nil Asker stays nil. "No Asker configured" is a FACT the tool reads (Execute answers that
// ask_user is unavailable rather than dereferencing it, and NewDefaultRegistryWithHost does not
// register the tool at all), so wrapping nil into a non-nil forwarder would claim a human is
// reachable where none is.
func queuedAsks(ap domain.Asker) domain.Asker {
	if ap == nil {
		return nil
	}
	if _, already := ap.(*queuedAsker); already {
		return ap
	}
	return &queuedAsker{slot: make(chan struct{}, 1), inner: ap}
}

// ReadOnly reports that ask_user performs no writes (asking a question mutates nothing), so
// the disposition runs it freely in every mode — including Plan.
func (t *AskUser) ReadOnly() bool { return true }

// Execute puts the question to the human via the Asker and returns the typed answer. A
// cancelled ctx (the human abandoned the prompt) is a Go error so the loop rolls the Turn
// back (ADR 0007); any other Asker error is surfaced as a result. An empty question is a
// result-level error.
func (t *AskUser) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[askUserArgs](call)
	if !ok {
		return fail, nil
	}
	if strings.TrimSpace(args.Question) == "" {
		return errorResult(call.ID, "question is required"), nil
	}
	if t.asker == nil {
		return errorResult(call.ID, "ask_user is unavailable: no Asker delegate is configured"), nil
	}

	req := domain.AskRequest{
		Question:    args.Question,
		Choices:     sanitiseChoices(args.Choices),
		MultiSelect: args.MultiSelect,
		// Which agent is asking, read off the ctx the engine installed it on
		// (domain.WithSubAgentTask): empty for the top-level agent, the delegated task for a child.
		// It is stamped here, where the request is BUILT, rather than inside the queueing seam,
		// because it is a fact about the question and not about the queue — the queue only decides
		// when the human sees it.
		SubAgentTask: domain.SubAgentTaskFromContext(ctx),
	}
	answer, err := t.asker.Ask(ctx, req)
	if err != nil {
		if ctx.Err() != nil {
			return domain.ToolResult{}, ctx.Err()
		}
		return errorResult(call.ID, "could not ask the user: "+err.Error()), nil
	}
	return okResult(call.ID, answer.Text), nil
}

// sanitiseChoices trims each choice and drops whitespace-only entries, returning nil when no
// non-blank choice remains. A sloppy or absent array therefore degrades to free-text-only
// rather than raising a result-level error — a malformed choices list never blocks the
// question from reaching the human.
func sanitiseChoices(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	cleaned := make([]string, 0, len(raw))
	for _, choice := range raw {
		if trimmed := strings.TrimSpace(choice); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

var (
	_ domain.Tool         = (*AskUser)(nil)
	_ domain.ReadOnlyTool = (*AskUser)(nil)
)
