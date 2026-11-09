package main

// The request-tail advise slot end to end (ADR 0076 D6): a user `advise:` entry on `pre-request`
// runs before every request the Exchange sends, and what it printed reaches the model fenced on
// that request's tail — a leading fence inside the opening user message, a trailing fence inside a
// tool-result tail — on the request copy alone. The claim is made on what the STUB received, on
// both outbound wires, because the placement is role-safe only if the Messages wire's strict
// user/assistant alternation survives it: the anthropic case reads the raw body's roles, since the
// stub's decoded view turns a tool_result block into a role-tool message and would hide two user
// messages side by side.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// TestE2EPreRequestAdviseReachesBothWires is the pre-request slot's headless journey, once per
// wire: the run's first request ends on the prompt with the Turn-0 fence leading it, the second
// request ends on the tool result with the Turn-1 fence trailing it and carries no Turn-0 fence
// anywhere — the first request's advice was never written into the conversation — and the
// stream books one advise firing per request.
func TestE2EPreRequestAdviseReachesBothWires(t *testing.T) {
	for _, tc := range []struct {
		name, wire, want string
	}{
		{name: "chat completions", wire: "", want: stubllm.WireOpenAI},
		{name: "messages", wire: "anthropic", want: stubllm.WireAnthropic},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := stubllm.New(t, preRequestAdviseScript())
			home := wireHome(t, stub, tc.wire)
			appendConfig(t, home, adviseConfig(domain.MomentPreRequest, "echo "+adviseCoachSays))

			out, errOut, err := headlessEventLines(t, run.Once, home, e2eWorkspace(t), gatePrompt)
			if err != nil {
				t.Fatalf("the headless run returned %v\nstderr:\n%s", err, errOut)
			}
			reqs := stub.Requests()
			assertRequestsOnWire(t, reqs, tc.want)
			if len(reqs) != 2 {
				t.Fatalf("the stub answered %d requests; want 2 — the prompt, then the tool result", len(reqs))
			}

			opening := preRequestFence(0, true)
			if got := openingHandedOn(t, stub, 0); got != opening+gatePrompt {
				t.Errorf("the first request ended on the user message\n%q\nwant the prompt led by "+
					"the pre-request fence\n%q", got, opening+gatePrompt)
			}
			if got := toolResultHandedOn(t, stub, 1); !strings.HasSuffix(got, preRequestFence(1, false)) {
				t.Errorf("the second request ended on the tool result\n%q\nwant it to end with the "+
					"pre-request trailer\n%q", got, preRequestFence(1, false))
			}
			for i, m := range reqs[1].Messages {
				if strings.Contains(m.Content, opening) {
					t.Errorf("message %d (%s) of the second request carries the first request's "+
						"fence; pre-request advice lives on its request copy alone:\n%s", i, m.Role, m.Content)
				}
			}
			for _, r := range reqs {
				assertNoUserAdjacency(t, r)
			}

			fired := adviseFirings(t, jsonEventLines(t, out))
			if want := []string{adviseCoachText, adviseCoachText}; !slices.Equal(fired, want) {
				t.Errorf("the stream booked the advise firings %q; want %q — one per request", fired, want)
			}
		})
	}
}

// preRequestAdviseScript is the upstream the pre-request journey talks to: a list_dir call on the
// prompt, then the answer once its result comes back. The prompt is matched unanchored because the
// opening starts with the pre-request fence, not the prompt (reactions.yaml anchors it).
func preRequestAdviseScript() stubllm.Script {
	return stubllm.Script{
		Model: "pre-request-advise-model",
		Turns: []stubllm.Turn{
			{
				When:      &stubllm.Match{LastMessage: regexp.QuoteMeta(gatePrompt) + "$"},
				ToolCalls: []stubllm.ToolCall{{Name: "list_dir", Arguments: `{"path":"."}`}},
			},
			{When: &stubllm.Match{ToolResult: "list_dir"}, Text: gateAnswer},
			{Repeat: true, Text: "OK."},
		},
	}
}

// preRequestFence is the fence domain.RenderAdvice composes for the coach's pre-request advice at
// turn, leading a user tail or trailing a tool tail.
func preRequestFence(turn int, leading bool) string {
	return domain.RenderAdvice(domain.AdviceSpan{
		Reaction: adviseCoachName,
		Origin:   domain.OriginUser,
		Moment:   domain.MomentPreRequest,
		Turn:     turn,
		Leading:  leading,
	}, adviseCoachText)
}

// appendConfig appends extra to the config.yaml in home — the reactions block a wire home
// (wireHome) does not write, since headlessHooksIn's own home cannot name a wire.
func appendConfig(t *testing.T, home, extra string) {
	t.Helper()

	path := filepath.Join(home, "config.yaml")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open the home's config: %v", err)
	}
	_, err = f.WriteString(extra)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("append to the home's config: %v", err)
	}
}

// assertNoUserAdjacency fails when r's raw body carries two consecutive user messages — the shape
// the Messages wire refuses and a careless advice placement would produce. It reads the body's own
// `messages[].role`, not the stub's decoded view, which reports a tool_result block as role tool.
func assertNoUserAdjacency(t *testing.T, r stubllm.Request) {
	t.Helper()

	var body struct {
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(r.Body, &body); err != nil {
		t.Fatalf("request %d's body is not JSON: %v", r.N, err)
	}
	for i := 1; i < len(body.Messages); i++ {
		if body.Messages[i-1].Role == "user" && body.Messages[i].Role == "user" {
			t.Errorf("request %d carries user messages %d and %d side by side", r.N, i-1, i)
		}
	}
}
