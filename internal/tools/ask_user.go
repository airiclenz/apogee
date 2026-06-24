package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

var askUserSchema = json.RawMessage(`{
  "type": "object",
  "required": ["question"],
  "properties": {
    "question": {"type": "string", "description": "The question to ask the human. Use this when you need a clarification or a decision only the user can make."}
  }
}`)

type askUserArgs struct {
	Question string `json:"question"`
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
// Turns (ADR 0008): a fresh question per call, no held state.
type AskUser struct{ asker domain.Asker }

// NewAskUser returns an ask_user tool routing to asker. A nil asker yields a tool whose
// Execute reports the delegate is unavailable (the registry omits it in practice).
func NewAskUser(asker domain.Asker) *AskUser { return &AskUser{asker: asker} }

// Name returns the stable identifier the model calls.
func (t *AskUser) Name() string { return "ask_user" }

// Description returns the model-facing summary of the tool.
func (t *AskUser) Description() string {
	return "Ask the human a free-text question and get their answer. Use this for a clarification or a decision only the user can make. It is not a tool-approval prompt; it is a direct question to the person."
}

// Schema returns the JSON schema of the tool's arguments.
func (t *AskUser) Schema() json.RawMessage { return askUserSchema }

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

	var args askUserArgs
	if err := decodeArgs(call.Arguments, &args); err != nil {
		return errorResult(call.ID, "invalid arguments: "+err.Error()), nil
	}
	if strings.TrimSpace(args.Question) == "" {
		return errorResult(call.ID, "question is required"), nil
	}
	if t.asker == nil {
		return errorResult(call.ID, "ask_user is unavailable: no Asker delegate is configured"), nil
	}

	answer, err := t.asker.Ask(ctx, domain.AskRequest{Question: args.Question})
	if err != nil {
		if ctx.Err() != nil {
			return domain.ToolResult{}, ctx.Err()
		}
		return errorResult(call.ID, "could not ask the user: "+err.Error()), nil
	}
	return okResult(call.ID, answer.Text), nil
}

var (
	_ domain.Tool         = (*AskUser)(nil)
	_ domain.ReadOnlyTool = (*AskUser)(nil)
)
