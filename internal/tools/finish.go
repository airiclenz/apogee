package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/workflow"
)

// FinishToolName is the name a workflow item's child hands its receipt back under (ADR 0087).
// The tool is never in the default registry and never in KnownToolNames: the engine builds one
// per workflow child, its schema drawn from that child's stage (NewFinish), so no roster, profile
// or `tools:` key can name it and no top-level Agent ever holds it.
const FinishToolName = "finish"

// finishDescription is what the model reads about the tool. It states the three things a child
// must know to use it: that the call ends its run, that the receipt is short and fixed-shape
// (the detail belongs in the output file), and that a refused receipt is fixed and sent again.
const finishDescription = "Hand back your receipt for this item and end your run. Call it once," +
	" when the work is done or cannot go further: status ok when the item is done, partial when" +
	" you got part of the way, blocked when you could not proceed; a one-line summary; and the" +
	" other fields in its parameters. The detail belongs in your output file, not here. If the call is" +
	" refused, fix what the refusal names and call finish again."

// The descriptions of the two core receipt properties, in the schema every finish carries.
const (
	finishStatusDescription  = "ok when the item is done, partial when it is part done, blocked when you could not proceed"
	finishSummaryDescription = "One line saying what the item came to (at most %d words)"
)

// finishRefusalHead leads the error result a malformed receipt is refused with; each problem
// follows on its own line, naming the field and the fix (workflow.ReceiptSpec.Check).
const finishRefusalHead = "finish refused — fix these and call finish again:"

// finishAcceptedFormat is the result a well-formed receipt gets; %s is its status.
const finishAcceptedFormat = "receipt accepted (%s) — your run ends here"

// finishDuplicateResult is the result of a well-formed receipt sent after one was already
// accepted in the same run: the first receipt stands.
const finishDuplicateResult = "a receipt was already accepted for this run; this one is ignored"

// Finish is the tool a workflow item's child reports through (ADR 0087): a checked receipt of a
// status, a one-line summary and the typed fields its stage declared. A receipt that does not
// fit the stage's ReceiptSpec is refused with an error result naming every problem, and the
// child keeps running; a receipt that fits is handed to accept and the run ends there (the
// agent ends the child's Exchange once the call has been dispatched).
//
// It takes the READ-ONLY floor (domain.ReadOnlyTool), task_list's precedent: handing back a
// receipt touches no file, starts no process and reaches no network, so Classify resolves it to
// ClassReadOnly and it runs ungated in every mode, Plan and ask-before included.
type Finish struct {
	toolSpec
	spec   workflow.ReceiptSpec
	accept func(workflow.Receipt) bool
}

// NewFinish returns a finish tool whose schema and check are spec's. accept receives each
// well-formed receipt and reports whether it was taken — false when the run already holds one,
// which the model is told.
func NewFinish(spec workflow.ReceiptSpec, accept func(workflow.Receipt) bool) *Finish {
	return &Finish{
		toolSpec: toolSpec{
			name:        FinishToolName,
			description: finishDescription,
			schema:      FinishSchema(spec),
		},
		spec:   spec,
		accept: accept,
	}
}

// ReadOnly reports that finish only reads (true) as far as blast radius goes: a receipt is
// engine state, nothing the host owns.
func (f *Finish) ReadOnly() bool { return true }

// Execute checks the call's receipt against the stage's spec. A malformed one is an error result
// listing each problem and its fix; a well-formed one is handed to accept. Only ctx cancellation
// is a Go error.
func (f *Finish) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[map[string]any](call)
	if !ok {
		return fail, nil
	}
	receipt := receiptFromArgs(args)
	if problems := f.spec.Check(receipt); len(problems) > 0 {
		lines := make([]string, 0, len(problems)+1)
		lines = append(lines, finishRefusalHead)
		for _, problem := range problems {
			lines = append(lines, "- "+problem.String())
		}
		return errorResult(call.ID, strings.Join(lines, "\n")), nil
	}
	if f.accept != nil && !f.accept(receipt) {
		return okResult(call.ID, finishDuplicateResult), nil
	}
	return okResult(call.ID, fmt.Sprintf(finishAcceptedFormat, receipt.Status)), nil
}

// receiptFromArgs splits a decoded finish call into a Receipt: status and summary are the core
// fields, every other key a typed field for the spec to judge. A core value that is not a string
// is rendered as one, so the check's message quotes what the model actually sent.
func receiptFromArgs(args map[string]any) workflow.Receipt {
	receipt := workflow.Receipt{
		Status:  workflow.Status(argText(args[workflow.FieldStatus])),
		Summary: argText(args[workflow.FieldSummary]),
	}
	for name, value := range args {
		if name == workflow.FieldStatus || name == workflow.FieldSummary {
			continue
		}
		if receipt.Fields == nil {
			receipt.Fields = make(map[string]any, len(args))
		}
		receipt.Fields[name] = value
	}
	return receipt
}

// argText is value as text: a string verbatim, nothing as "", anything else in its %v spelling.
func argText(value any) string {
	switch text := value.(type) {
	case nil:
		return ""
	case string:
		return text
	default:
		return fmt.Sprint(text)
	}
}

// FinishSchema renders the JSON schema of a finish call for spec: the required status (an enum)
// and summary, then each declared field typed as its spelling says — an integer, a string capped
// at workflow.TextMaxRunes, an array of strings, or a string enum. A field whose spelling does not
// parse is left out of the schema; the check still refuses it by name.
func FinishSchema(spec workflow.ReceiptSpec) json.RawMessage {
	statusType, _ := spec.Field(workflow.FieldStatus)
	properties := map[string]any{
		workflow.FieldStatus: map[string]any{
			"type":        "string",
			"enum":        statusType.Values,
			"description": finishStatusDescription,
		},
		workflow.FieldSummary: map[string]any{
			"type":        "string",
			"description": fmt.Sprintf(finishSummaryDescription, workflow.SummaryMaxWords),
		},
	}
	names := make([]string, 0, len(spec))
	for name := range spec {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		fieldType, ok := spec.Field(name)
		if !ok {
			continue
		}
		properties[name] = fieldSchema(fieldType)
	}
	schema, err := json.Marshal(map[string]any{
		"type":       "object",
		"required":   []string{workflow.FieldStatus, workflow.FieldSummary},
		"properties": properties,
	})
	if err != nil {
		// Unreachable: every value above is a string, a string slice or a map of them.
		panic("apogee: finish schema does not marshal: " + err.Error())
	}
	return schema
}

// fieldSchema is one declared receipt field's JSON schema.
func fieldSchema(fieldType workflow.FieldType) map[string]any {
	switch fieldType.Kind {
	case workflow.FieldInt:
		return map[string]any{"type": "integer"}
	case workflow.FieldList:
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	case workflow.FieldEnum:
		return map[string]any{"type": "string", "enum": fieldType.Values}
	default:
		return map[string]any{
			"type":        "string",
			"description": fmt.Sprintf("at most %d characters", workflow.TextMaxRunes),
		}
	}
}

var (
	_ domain.Tool         = (*Finish)(nil)
	_ domain.ReadOnlyTool = (*Finish)(nil)
)
