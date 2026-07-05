package domain

// White-box tests for the hook working-value bodies (P1.5). They are package-internal
// so they can set the unexported Message.extra field and build the unexported loopView
// the way the engine seam will — exercising the real accessors and mutators the design
// (docs/design/hook-mutation-api.md) requires, not just the exported shape.

import (
	"bytes"
	"encoding/json"
	"testing"
)

func sysUserReq(t *testing.T) *Request {
	t.Helper()
	return NewRequest("m", []Message{
		{Role: RoleSystem, Content: "base"},
		{Role: RoleUser, Content: "do it"},
	}, nil, Budget{}, 0, nil)
}

func roles(msgs []Message) []Role {
	out := make([]Role, len(msgs))
	for i, m := range msgs {
		out[i] = m.Role
	}
	return out
}

func TestRequestAppendToSystem(t *testing.T) {
	t.Run("appends to existing system message", func(t *testing.T) {
		r := sysUserReq(t)
		if injected := r.AppendToSystem("[m1]", "[m1] extra guidance"); !injected {
			t.Fatal("AppendToSystem reported no injection on a fresh marker")
		}
		got := r.State().Messages[0]
		if got.Role != RoleSystem || got.Content != "base\n\n[m1] extra guidance" {
			t.Errorf("system message = %+v, want appended guidance", got)
		}
	})

	t.Run("is idempotent on the marker", func(t *testing.T) {
		r := sysUserReq(t)
		r.AppendToSystem("[m1]", "[m1] once")
		if injected := r.AppendToSystem("[m1]", "[m1] twice"); injected {
			t.Error("AppendToSystem injected a second time despite the marker present")
		}
		if c := r.State().Messages[0].Content; c != "base\n\n[m1] once" {
			t.Errorf("system content mutated on the idempotent call: %q", c)
		}
	})

	t.Run("creates a system message when absent", func(t *testing.T) {
		r := NewRequest("m", []Message{{Role: RoleUser, Content: "hi"}}, nil, Budget{}, 0, nil)
		if injected := r.AppendToSystem("[m1]", "[m1] new"); !injected {
			t.Fatal("AppendToSystem did not inject when no system message existed")
		}
		msgs := r.State().Messages
		if len(msgs) != 2 || msgs[0].Role != RoleSystem || msgs[0].Content != "[m1] new" {
			t.Errorf("prepended system message wrong: roles=%v content[0]=%q", roles(msgs), msgs[0].Content)
		}
	})
}

func TestRequestInjectContext(t *testing.T) {
	t.Run("inserts before the last user message", func(t *testing.T) {
		r := sysUserReq(t)
		r.InjectContext("hint")
		msgs := r.State().Messages
		// [system, user(hint), user(do it)]
		wantRoles := []Role{RoleSystem, RoleUser, RoleUser}
		if got := roles(msgs); !equalRoles(got, wantRoles) {
			t.Fatalf("roles = %v, want %v", got, wantRoles)
		}
		if msgs[1].Content != "hint" || msgs[2].Content != "do it" {
			t.Errorf("hint not placed before the last user message: %+v", msgs)
		}
	})

	t.Run("appends to system when the conversation ends in a tool result", func(t *testing.T) {
		r := NewRequest("m", []Message{
			{Role: RoleSystem, Content: "base"},
			{Role: RoleUser, Content: "go"},
			{Role: RoleAssistant, Content: "", ToolCalls: []ToolCall{{ID: "c1", Tool: "read"}}},
			{Role: RoleTool, Content: "file body", ToolCallID: "c1"},
		}, nil, Budget{}, 0, nil)
		r.InjectContext("note")
		msgs := r.State().Messages
		if len(msgs) != 4 {
			t.Fatalf("a user message was inserted after a tool result (len=%d); want append-to-system", len(msgs))
		}
		if msgs[0].Content != "base\n\nnote" {
			t.Errorf("system message = %q, want the note appended", msgs[0].Content)
		}
	})

	t.Run("appends at the end when there is no user message", func(t *testing.T) {
		r := NewRequest("m", []Message{{Role: RoleSystem, Content: "base"}}, nil, Budget{}, 0, nil)
		r.InjectContext("note")
		msgs := r.State().Messages
		if len(msgs) != 2 || msgs[1].Role != RoleUser || msgs[1].Content != "note" {
			t.Errorf("expected an appended user message, got roles=%v", roles(msgs))
		}
	})
}

func TestRequestSetMessageContent(t *testing.T) {
	r := sysUserReq(t)
	r.SetMessageContent(1, "edited")
	r.SetMessageContent(-1, "ignored")
	r.SetMessageContent(99, "ignored")
	msgs := r.State().Messages
	if msgs[1].Content != "edited" {
		t.Errorf("in-range edit not applied: %q", msgs[1].Content)
	}
	if msgs[0].Content != "base" {
		t.Errorf("out-of-range edit leaked into another message: %q", msgs[0].Content)
	}
}

func TestRequestSetToolsAndExtraAndSampling(t *testing.T) {
	r := sysUserReq(t)

	tools := []ToolDef{{Name: "grep"}, {Name: "read_file"}}
	r.SetTools(tools)
	tools[0].Name = "mutated-after-set" // proves SetTools copied
	if got := r.View().Tools(); len(got) != 2 || got[0].Name != "grep" {
		t.Errorf("SetTools did not isolate the menu from the caller's slice: %+v", got)
	}

	r.SetExtra("response_format", json.RawMessage(`{"type":"json"}`))
	if v, ok := r.Extra("response_format"); !ok || string(v) != `{"type":"json"}` {
		t.Errorf("Extra round-trip failed: %q ok=%v", v, ok)
	}
	if _, ok := r.Extra("absent"); ok {
		t.Error("Extra reported a key that was never set")
	}

	temp := 0.2
	max := 256
	r.SetSampling(SamplingParams{Temperature: &temp, MaxTokens: &max})

	st := r.State()
	if st.Sampling.Temperature == nil || *st.Sampling.Temperature != 0.2 {
		t.Errorf("drained sampling temperature wrong: %+v", st.Sampling)
	}
	if st.Sampling.MaxTokens == nil || *st.Sampling.MaxTokens != 256 {
		t.Errorf("drained sampling max-tokens wrong: %+v", st.Sampling)
	}
	if string(st.Extras["response_format"]) != `{"type":"json"}` {
		t.Errorf("drained extras wrong: %v", st.Extras)
	}
	if len(st.Tools) != 2 {
		t.Errorf("drained tools wrong: %+v", st.Tools)
	}
}

func TestRequestView(t *testing.T) {
	budget := Budget{ContextLimit: 8192, CharsPerToken: 4}
	r := NewRequest("model-x", []Message{
		{Role: RoleSystem, Content: "s"},
		{Role: RoleUser, Content: "u1"},
		{Role: RoleAssistant, Content: "a"},
		{Role: RoleUser, Content: "u2"},
	}, []ToolDef{{Name: "t"}}, budget, 7, nil)
	v := r.View()

	if r.Model() != "model-x" {
		t.Errorf("Model = %q", r.Model())
	}
	if v.Turn() != 7 {
		t.Errorf("Turn = %d, want 7", v.Turn())
	}
	if v.Budget() != budget {
		t.Errorf("Budget = %+v, want %+v", v.Budget(), budget)
	}
	if got := v.Tools(); len(got) != 1 || got[0].Name != "t" {
		t.Errorf("Tools = %+v", got)
	}
	if v.Fired("anything") != 0 {
		t.Errorf("Fired on a Phase-1 view should be 0, got %d", v.Fired("anything"))
	}
	conv := v.Conversation()
	if conv.Len() != 4 {
		t.Errorf("Conversation.Len = %d, want 4", conv.Len())
	}
	if msg, idx, ok := conv.LastUser(); !ok || idx != 3 || msg.Content != "u2" {
		t.Errorf("LastUser = (%+v, %d, %v), want u2 at 3", msg, idx, ok)
	}
	var seen int
	conv.Range(func(i int, m Message) bool { seen++; return i < 1 }) // stop after index 1
	if seen != 2 {
		t.Errorf("Range visited %d messages before stopping, want 2", seen)
	}
	if conv.At(2).Content != "a" {
		t.Errorf("At(2) = %q, want a", conv.At(2).Content)
	}
}

func TestConversationViewPairing(t *testing.T) {
	v := loopView{messages: []Message{
		{Role: RoleUser, Content: "read foo"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call-7", Tool: "read_file", Arguments: json.RawMessage(`{"path":"foo"}`)}}},
		{Role: RoleTool, Content: "foo contents", ToolCallID: "call-7"},
	}}
	conv := v.Conversation()

	call, idx, ok := conv.CallByID("call-7")
	if !ok || idx != 1 || call.Tool != "read_file" {
		t.Errorf("CallByID = (%+v, %d, %v), want read_file at 1", call, idx, ok)
	}
	res, ridx, ok := conv.ResultFor("call-7")
	if !ok || ridx != 2 || res.Content != "foo contents" {
		t.Errorf("ResultFor = (%+v, %d, %v), want the tool result at 2", res, ridx, ok)
	}
	if _, _, ok := conv.CallByID("missing"); ok {
		t.Error("CallByID resolved a nonexistent id")
	}
	if _, _, ok := conv.ResultFor("missing"); ok {
		t.Error("ResultFor resolved a nonexistent id")
	}
}

func TestMessageExtra(t *testing.T) {
	plain := Message{Role: RoleUser, Content: "hi"}
	if _, ok := plain.Extra("reasoning_content"); ok {
		t.Error("a literal Message reported an Extra it never carried")
	}
	withExtra := Message{
		Role:  RoleAssistant,
		extra: map[string]json.RawMessage{"reasoning_content": json.RawMessage(`"thought"`)},
	}
	if v, ok := withExtra.Extra("reasoning_content"); !ok || string(v) != `"thought"` {
		t.Errorf("Extra = %q ok=%v, want the preserved field", v, ok)
	}
}

// TestMessageExtraRoundTrip is the P1.6 schema proof: a Message's preserved wire siblings
// survive a JSON round-trip, are flattened at the top level (the OpenAI chat shape, not
// nested), and the known fields never leak into the Extra set.
func TestMessageExtraRoundTrip(t *testing.T) {
	orig := Message{Role: RoleAssistant, Content: "the answer"}.
		WithExtra("reasoning_content", json.RawMessage(`"chain of thought"`)).
		WithExtra("tool_choice", json.RawMessage(`"auto"`))

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Extras are flattened as top-level siblings of the known fields, not nested.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	if _, nested := raw["extra"]; nested {
		t.Errorf("extras nested under an \"extra\" key, want flattened siblings: %s", data)
	}
	if string(raw["reasoning_content"]) != `"chain of thought"` {
		t.Errorf("reasoning_content not flattened at top level: %s", data)
	}
	if string(raw["content"]) != `"the answer"` {
		t.Errorf("known field content missing or renamed: %s", data)
	}

	var got Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal to Message: %v", err)
	}
	if got.Role != RoleAssistant || got.Content != "the answer" {
		t.Errorf("known fields not restored: %+v", got)
	}
	if v, ok := got.Extra("reasoning_content"); !ok || string(v) != `"chain of thought"` {
		t.Errorf("reasoning_content did not round-trip: %q ok=%v", v, ok)
	}
	if v, ok := got.Extra("tool_choice"); !ok || string(v) != `"auto"` {
		t.Errorf("tool_choice did not round-trip: %q ok=%v", v, ok)
	}
	if _, ok := got.Extra("content"); ok {
		t.Error("a known field leaked into the Extra set")
	}

	t.Run("collects an upstream wire message's unknown siblings into Extra", func(t *testing.T) {
		wire := []byte(`{"role":"assistant","content":"hi","reasoning_content":"because","logprobs":{"x":1}}`)
		var m Message
		if err := json.Unmarshal(wire, &m); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if v, ok := m.Extra("reasoning_content"); !ok || string(v) != `"because"` {
			t.Errorf("reasoning_content not collected: %q ok=%v", v, ok)
		}
		if v, ok := m.Extra("logprobs"); !ok || string(v) != `{"x":1}` {
			t.Errorf("unknown object sibling not collected: %q ok=%v", v, ok)
		}
	})
}

// TestMessageMarshalDeterministic locks in stable, sorted Extra key order on the wire, so a
// snapshot carrying preserved siblings is byte-reproducible — Go's randomized map iteration
// order must not leak into the serialized form.
func TestMessageMarshalDeterministic(t *testing.T) {
	m := Message{Role: RoleAssistant, Content: "x"}.
		WithExtra("zeta", json.RawMessage(`1`)).
		WithExtra("alpha", json.RawMessage(`2`)).
		WithExtra("mu", json.RawMessage(`3`))

	first, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for i := 0; i < 50; i++ {
		again, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("Marshal #%d: %v", i, err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("non-deterministic Message JSON:\n %s\nvs\n %s", first, again)
		}
	}
	// Known fields keep messageJSON order; the extras follow in sorted key order.
	const want = `{"role":"assistant","content":"x","alpha":2,"mu":3,"zeta":1}`
	if string(first) != want {
		t.Errorf("Message JSON = %s, want %s", first, want)
	}
}

func TestResponse(t *testing.T) {
	view := loopView{messages: []Message{{Role: RoleUser, Content: "u"}}}
	calls := []ToolCall{{ID: "c1", Tool: "write_file", Arguments: json.RawMessage(`{"path":"a"}`)}}
	resp := NewResponse("answer", "reasoning", calls, FinishToolCalls, view)

	if resp.Text() != "answer" {
		t.Errorf("Text = %q", resp.Text())
	}
	if think, ok := resp.Thinking(); !ok || think != "reasoning" {
		t.Errorf("Thinking = (%q, %v)", think, ok)
	}
	if resp.FinishReason() != FinishToolCalls {
		t.Errorf("FinishReason = %q", resp.FinishReason())
	}
	if resp.View().Conversation().Len() != 1 {
		t.Errorf("View did not carry the conversation window")
	}

	// Mutate via the intercept surface.
	resp.SetText("intercepted")
	resp.SetToolCallArguments(0, json.RawMessage(`{"path":"b"}`))
	resp.SetToolCallArguments(5, json.RawMessage(`{"ignored":true}`)) // out of range, no-op
	if resp.Text() != "intercepted" {
		t.Errorf("SetText not applied: %q", resp.Text())
	}
	if got := resp.ToolCalls(); len(got) != 1 || string(got[0].Arguments) != `{"path":"b"}` {
		t.Errorf("SetToolCallArguments not applied: %+v", got)
	}

	// A returned ToolCalls copy must not let the caller mutate backing storage.
	got := resp.ToolCalls()
	got[0].Tool = "evil"
	if resp.ToolCalls()[0].Tool != "write_file" {
		t.Error("ToolCalls returned an aliased slice; caller mutated backing storage")
	}

	t.Run("absent thinking reports ok=false", func(t *testing.T) {
		r := NewResponse("x", "", nil, FinishStop, nil) // nil view must not panic
		if _, ok := r.Thinking(); ok {
			t.Error("Thinking ok=true with no thinking channel")
		}
		if r.View() == nil {
			t.Error("View returned nil for a nil-view Response")
		}
	})
}

func TestConversationEditing(t *testing.T) {
	base := func() *Conversation {
		return NewConversation([]Message{
			{Role: RoleSystem, Content: "s0"},
			{Role: RoleSystem, Content: "s1"},
			{Role: RoleUser, Content: "u0"},
			{Role: RoleAssistant, Content: "a0"},
			{Role: RoleTool, Content: "t0", ToolCallID: "x"},
			{Role: RoleUser, Content: "u1"},
		})
	}

	t.Run("PrefixEnd spans leading system + first user", func(t *testing.T) {
		if got := base().PrefixEnd(); got != 3 {
			t.Errorf("PrefixEnd = %d, want 3", got)
		}
	})

	t.Run("AssistantBoundaries lists assistant indices", func(t *testing.T) {
		got := base().AssistantBoundaries()
		if len(got) != 1 || got[0] != 3 {
			t.Errorf("AssistantBoundaries = %v, want [3]", got)
		}
	})

	t.Run("DropRange drops the middle and clamps bounds", func(t *testing.T) {
		c := base()
		c.DropRange(2, 5) // drop u0, a0, t0
		if c.Len() != 3 {
			t.Fatalf("Len after drop = %d, want 3", c.Len())
		}
		if c.At(2).Content != "u1" {
			t.Errorf("tail after drop = %q, want u1", c.At(2).Content)
		}
		c.DropRange(-5, 100) // clamps to whole range
		if c.Len() != 0 {
			t.Errorf("clamped DropRange should empty the conversation, got Len=%d", c.Len())
		}
		c.DropRange(0, 0) // empty range no-op on empty conv
	})

	t.Run("Insert clamps and shifts", func(t *testing.T) {
		c := base()
		c.Insert(0, Message{Role: RoleSystem, Content: "front"})
		c.Insert(999, Message{Role: RoleUser, Content: "back"})
		if c.At(0).Content != "front" {
			t.Errorf("Insert(0) head = %q", c.At(0).Content)
		}
		if c.At(c.Len()-1).Content != "back" {
			t.Errorf("Insert(oob) tail = %q", c.At(c.Len()-1).Content)
		}
	})

	t.Run("Replace swaps the whole list", func(t *testing.T) {
		c := base()
		c.Replace([]Message{{Role: RoleSystem, Content: "only"}})
		if c.Len() != 1 || c.At(0).Content != "only" {
			t.Errorf("Replace failed: Len=%d", c.Len())
		}
	})

	t.Run("Append grows the conversation at the end", func(t *testing.T) {
		c := base()
		c.Append(Message{Role: RoleAssistant, Content: "a1"})
		if c.Len() != 7 || c.At(6).Content != "a1" {
			t.Errorf("Append failed: Len=%d tail=%q", c.Len(), c.At(c.Len()-1).Content)
		}
		empty := NewConversation(nil)
		empty.Append(Message{Role: RoleUser, Content: "first"})
		if empty.Len() != 1 || empty.At(0).Content != "first" {
			t.Errorf("Append onto an empty conversation failed: Len=%d", empty.Len())
		}
	})

	t.Run("SetMessageContent edits in place", func(t *testing.T) {
		c := base()
		c.SetMessageContent(2, "u0-edited")
		c.SetMessageContent(-1, "x")
		if c.At(2).Content != "u0-edited" {
			t.Errorf("SetMessageContent in-range failed: %q", c.At(2).Content)
		}
	})
}

// TestConversationDeferSurvivesRoundTrip is the domain-level proof of the P1.5
// ActionDefer-feed-forward primitive: a deferred correction recorded on the
// Conversation survives a serialize/deserialize boundary (the snapshot/resume path)
// and, drained on the next turn, injects role-safely into the outgoing Request. The
// loop integration that runs post-response hooks and snapshots the unified
// conversation lands in P1.2/P1.6; this proves the primitives those steps compose.
func TestConversationDeferSurvivesRoundTrip(t *testing.T) {
	conv := NewConversation([]Message{{Role: RoleUser, Content: "go"}})
	conv.Defer("apply the correction")
	conv.Defer("and this one")

	data, err := json.Marshal(conv)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var restored Conversation
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	injects, ok := restored.TakeDeferred()
	if !ok || len(injects) != 2 || injects[0] != "apply the correction" || injects[1] != "and this one" {
		t.Fatalf("deferred corrections did not survive the round-trip: %v (ok=%v)", injects, ok)
	}
	if _, ok := restored.TakeDeferred(); ok {
		t.Error("TakeDeferred returned corrections twice; the queue was not drained")
	}

	// Feed-forward: the drained corrections inject into the next request.
	req := NewRequest("m", restored.Messages(), nil, Budget{}, 1, nil)
	for _, in := range injects {
		req.InjectContext(in)
	}
	msgs := req.State().Messages
	if len(msgs) != 3 || msgs[0].Content != "apply the correction" || msgs[1].Content != "and this one" {
		t.Errorf("deferred corrections did not feed-forward into the request: %+v", msgs)
	}
}

// TestResponseAppendToolCall proves the post-response tool-call synthesis seam (ADR 0014
// item 2): a synthesized call appended to a response with none becomes visible through
// ToolCalls() (so the loop dispatches it like a model-emitted call), and the mutation bumps
// the revision so hookrun books it as an acted fire (R4).
func TestResponseAppendToolCall(t *testing.T) {
	resp := NewResponse("here is the plan", "", nil, FinishStop, nil)
	if len(resp.ToolCalls()) != 0 {
		t.Fatalf("precondition: response should start with no tool calls, got %d", len(resp.ToolCalls()))
	}
	before := resp.Revision()

	call := ToolCall{ID: "text_call_0", Tool: "sub_agent", Arguments: json.RawMessage(`{"task":"do the first subtask"}`)}
	resp.AppendToolCall(call)

	if resp.Revision() == before {
		t.Error("AppendToolCall did not bump the revision; the acted-fire probe would miss it")
	}
	got := resp.ToolCalls()
	if len(got) != 1 || got[0].Tool != "sub_agent" || string(got[0].Arguments) != `{"task":"do the first subtask"}` {
		t.Fatalf("appended call not visible via ToolCalls(): %+v", got)
	}

	// A second append accumulates rather than replacing — the loop dispatches each.
	resp.AppendToolCall(ToolCall{ID: "text_call_1", Tool: "sub_agent", Arguments: json.RawMessage(`{"task":"second"}`)})
	if got := resp.ToolCalls(); len(got) != 2 {
		t.Errorf("second AppendToolCall did not accumulate: %+v", got)
	}

	// The returned slice is a copy — a caller mutating it cannot reach the response's storage.
	got = resp.ToolCalls()
	got[0].Tool = "evil"
	if resp.ToolCalls()[0].Tool != "sub_agent" {
		t.Error("AppendToolCall exposed aliased backing storage through ToolCalls()")
	}
}

// TestLoopViewDepth proves the Depth() seam ADR 0014's gate reads: a Request stamped with a
// nesting level surfaces it through View().Depth() (and the Response produced against that
// view), while an unstamped Request and the degraded no-view Response report the top-level 0.
func TestLoopViewDepth(t *testing.T) {
	req := NewRequest("m", []Message{{Role: RoleUser, Content: "hi"}}, nil, Budget{}, 0, nil)
	if got := req.View().Depth(); got != 0 {
		t.Errorf("a Request built without SetDepth reports Depth %d, want 0 (top-level default)", got)
	}

	req.SetDepth(1)
	if got := req.View().Depth(); got != 1 {
		t.Errorf("after SetDepth(1), View().Depth() = %d, want 1", got)
	}
	// SetDepth is loop setup, not a hook mutation — it must not read as an acted fire.
	if req.Revision() != 0 {
		t.Errorf("SetDepth bumped the revision to %d; it must not book an acted fire", req.Revision())
	}

	// A Response produced against the request's view inherits its depth.
	resp := NewResponse("answer", "", nil, FinishStop, req.View())
	if got := resp.View().Depth(); got != 1 {
		t.Errorf("Response.View().Depth() = %d, want 1 (inherited from the request view)", got)
	}
	// The degraded nil-view Response reports the top-level default rather than panicking.
	if got := NewResponse("x", "", nil, FinishStop, nil).View().Depth(); got != 0 {
		t.Errorf("nil-view Response reports Depth %d, want 0", got)
	}
}

func equalRoles(a, b []Role) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
