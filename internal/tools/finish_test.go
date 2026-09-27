package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/workflow"
)

// finishCall builds a finish call with the given JSON arguments.
func finishCall(args string) domain.ToolCall {
	return domain.ToolCall{ID: "f1", Tool: FinishToolName, Arguments: json.RawMessage(args)}
}

// newRecordingFinish returns a finish tool over spec and the receipts it accepted.
func newRecordingFinish(spec workflow.ReceiptSpec) (*Finish, *[]workflow.Receipt) {
	var accepted []workflow.Receipt
	finish := NewFinish(spec, func(receipt workflow.Receipt) bool {
		if len(accepted) > 0 {
			return false
		}
		accepted = append(accepted, receipt)
		return true
	})
	return finish, &accepted
}

func TestFinish_SchemaIsBuiltFromTheReceiptSpec(t *testing.T) {
	t.Parallel()

	spec := workflow.ReceiptSpec{"count": "int", "note": "text", "paths": "list", "verdict": "confirmed|refuted"}
	var schema struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type  string   `json:"type"`
			Enum  []string `json:"enum"`
			Items *struct {
				Type string `json:"type"`
			} `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(FinishSchema(spec), &schema); err != nil {
		t.Fatalf("schema does not parse: %v", err)
	}

	if strings.Join(schema.Required, ",") != "status,summary" {
		t.Errorf("required = %v, want status and summary", schema.Required)
	}
	if got := strings.Join(schema.Properties["status"].Enum, "|"); got != "ok|partial|blocked" {
		t.Errorf("status enum = %q, want ok|partial|blocked", got)
	}
	cases := map[string]string{"summary": "string", "count": "integer", "note": "string", "paths": "array", "verdict": "string"}
	for name, want := range cases {
		if got := schema.Properties[name].Type; got != want {
			t.Errorf("property %q type = %q, want %q", name, got, want)
		}
	}
	if items := schema.Properties["paths"].Items; items == nil || items.Type != "string" {
		t.Errorf("paths items = %+v, want strings", items)
	}
	if got := strings.Join(schema.Properties["verdict"].Enum, "|"); got != "confirmed|refuted" {
		t.Errorf("verdict enum = %q, want confirmed|refuted", got)
	}
}

func TestFinish_AcceptsAWellFormedReceipt(t *testing.T) {
	t.Parallel()

	finish, accepted := newRecordingFinish(workflow.ReceiptSpec{"count": "int"})
	result, err := finish.Execute(context.Background(), finishCall(`{"status":"ok","summary":"three findings","count":3}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.IsError {
		t.Fatalf("a well-formed receipt was refused: %q", result.Content)
	}
	if len(*accepted) != 1 {
		t.Fatalf("accepted %d receipts, want 1", len(*accepted))
	}
	got := (*accepted)[0]
	if got.Status != workflow.StatusOK || got.Summary != "three findings" || got.Fields["count"] != float64(3) {
		t.Errorf("receipt = %+v, want ok / three findings / count 3", got)
	}
}

func TestFinish_RefusesAnOutOfEnumValueNamingTheFix(t *testing.T) {
	t.Parallel()

	finish, accepted := newRecordingFinish(workflow.ReceiptSpec{"verdict": "confirmed|refuted|unclear"})
	result, err := finish.Execute(context.Background(), finishCall(`{"status":"ok","summary":"checked","verdict":"maybe"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !result.IsError {
		t.Fatalf("an out-of-enum verdict was accepted: %q", result.Content)
	}
	for _, want := range []string{finishRefusalHead, `field "verdict"`, "confirmed, refuted, unclear"} {
		if !strings.Contains(result.Content, want) {
			t.Errorf("refusal = %q, want it to contain %q", result.Content, want)
		}
	}
	if len(*accepted) != 0 {
		t.Errorf("a refused receipt reached accept: %+v", *accepted)
	}
}

func TestFinish_RefusesABadStatusAndAnUndeclaredField(t *testing.T) {
	t.Parallel()

	finish, _ := newRecordingFinish(nil)
	result, err := finish.Execute(context.Background(), finishCall(`{"status":"done","summary":"x","extra":1}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !result.IsError {
		t.Fatalf("a malformed receipt was accepted: %q", result.Content)
	}
	for _, want := range []string{`field "status"`, `field "extra"`} {
		if !strings.Contains(result.Content, want) {
			t.Errorf("refusal = %q, want it to name %s", result.Content, want)
		}
	}
}

func TestFinish_ASecondReceiptIsIgnored(t *testing.T) {
	t.Parallel()

	finish, accepted := newRecordingFinish(nil)
	_, _ = finish.Execute(context.Background(), finishCall(`{"status":"ok","summary":"first"}`))
	result, err := finish.Execute(context.Background(), finishCall(`{"status":"partial","summary":"second"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.Content != finishDuplicateResult {
		t.Errorf("second receipt result = %q, want %q", result.Content, finishDuplicateResult)
	}
	if len(*accepted) != 1 || (*accepted)[0].Summary != "first" {
		t.Errorf("accepted = %+v, want the first receipt alone", *accepted)
	}
}

func TestFinish_ClassifiesAsReadOnly(t *testing.T) {
	t.Parallel()

	finish, _ := newRecordingFinish(nil)

	if got := Classify(finish); got != ClassReadOnly {
		t.Errorf("Classify(finish) = %v, want ClassReadOnly — it must run ungated in Plan and ask-before", got)
	}
}

func TestFinish_IsNotABuildTool(t *testing.T) {
	t.Parallel()

	for _, name := range KnownToolNames() {
		if name == FinishToolName {
			t.Fatalf("KnownToolNames lists %q; finish is built per workflow child, never by a roster", name)
		}
	}
}
