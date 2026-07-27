package agent

// White-box test for the domain→provider wire projection. It lives in package agent so it
// can call the unexported toProviderRequest directly with a hand-built domain.Request —
// the projection is a pure mapping, so no Step, no fake responder, and no Upstream is needed.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// TestProviderRequestOmitsInterjected pins the marker as process-local: an interjected user
// message projects onto the wire as an ordinary user message. The projection maps fields
// explicitly (wire.go), so the guarantee holds by construction — this test is what makes a
// future "just copy the struct" refactor fail loudly instead of leaking an Apogee-owned flag
// into a provider request.
func TestProviderRequestOmitsInterjected(t *testing.T) {
	t.Parallel()

	a := &Agent{cfg: domain.Config{Model: "test-model"}}
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "the ask"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file"}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: "contents"},
		{Role: domain.RoleUser, Content: "also check the tests", Interjected: true},
	}
	req := domain.NewRequest("test-model", msgs, nil, domain.Budget{}, 0, nil)

	got := a.toProviderRequest(req)

	want := []provider.Message{
		{Role: "user", Content: "the ask"},
		{Role: "assistant", ToolCalls: []provider.ToolCall{{
			ID:       "c1",
			Type:     "function",
			Function: provider.FunctionCall{Name: "read_file"},
		}}},
		{Role: "tool", ToolCallID: "c1", Content: "contents"},
		// The interjection: same shape as any other user message, no marker.
		{Role: "user", Content: "also check the tests"},
	}
	if !reflect.DeepEqual(got.Messages, want) {
		t.Errorf("projected messages = %+v, want %+v", got.Messages, want)
	}

	// The wire type has no carrier for the marker at all.
	for i := 0; i < reflect.TypeOf(provider.Message{}).NumField(); i++ {
		if name := reflect.TypeOf(provider.Message{}).Field(i).Name; strings.Contains(strings.ToLower(name), "interject") {
			t.Errorf("provider.Message grew field %q — the marker must never reach the wire", name)
		}
	}

	// Belt and braces: no trace of the marker anywhere in the serialized request.
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal provider request: %v", err)
	}
	if strings.Contains(strings.ToLower(string(encoded)), "interject") {
		t.Errorf("serialized provider request mentions the marker: %s", encoded)
	}
}
