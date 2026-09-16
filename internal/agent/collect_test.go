package agent

import (
	"context"
	"iter"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// scriptedDeltas is the collector's fake: it plays one fixed Delta script for every request.
type scriptedDeltas []provider.Delta

func (s scriptedDeltas) Stream(context.Context, provider.Request) iter.Seq[provider.Delta] {
	return func(yield func(provider.Delta) bool) {
		for _, d := range s {
			if !yield(d) {
				return
			}
		}
	}
}

// TestCollectCompletionFoldsEveryDeltaKind pins what the one Delta collector returns for each
// shape a stream can take — content, the two reasoning channels, tool calls, the terminal Done
// with its accounting and served model, and the three fault classes — and that every Delta reached
// the observer in wire order before the fold. The delimited-profile row is the strip: the inline
// span leaves the content and joins the Upstream-split channel behind it (joinThinking), so no
// caller strips again.
func TestCollectCompletionFoldsEveryDeltaKind(t *testing.T) {
	t.Parallel()

	usage := provider.Usage{PromptTokens: 12, CompletionTokens: 7, TotalTokens: 19}
	call := provider.ToolCall{ID: "c1", Type: "function"}
	call.Function.Name = "read_file"
	call.Function.Arguments = `{"path":"a.go"}`

	cases := []struct {
		name      string
		delimited bool
		deltas    scriptedDeltas
		want      completion
	}{
		{
			name: "content and native reasoning end on Done with accounting",
			deltas: scriptedDeltas{
				{Kind: provider.DeltaThinking, Thinking: "plan"},
				{Kind: provider.DeltaContent, Content: "hel"},
				{Kind: provider.DeltaContent, Content: "lo"},
				{Kind: provider.DeltaDone, FinishReason: "stop", Usage: &usage, Model: "served-model"},
			},
			want: completion{content: "hello", thinking: "plan", finish: domain.FinishStop, usage: &usage, served: "served-model"},
		},
		{
			name: "tool calls are kept in order and a nil call is skipped",
			deltas: scriptedDeltas{
				{Kind: provider.DeltaToolCall, ToolCall: &call},
				{Kind: provider.DeltaToolCall},
				{Kind: provider.DeltaDone, FinishReason: "tool_calls"},
			},
			want: completion{toolCalls: []provider.ToolCall{call}, finish: domain.FinishReason("tool_calls")},
		},
		{
			name:      "an inline reasoning span is stripped and joined behind the split channel",
			delimited: true,
			deltas: scriptedDeltas{
				{Kind: provider.DeltaThinking, Thinking: "upstream"},
				{Kind: provider.DeltaContent, Content: "<think>inline</think>visible"},
				{Kind: provider.DeltaDone, FinishReason: "length"},
			},
			want: completion{content: "visible", thinking: "upstream\n\ninline", finish: domain.FinishLength},
		},
		{
			name: "a transient fault is failed and retryable, never overflow",
			deltas: scriptedDeltas{
				{Kind: provider.DeltaContent, Content: "partial"},
				{Kind: provider.DeltaError, Err: "502 bad gateway", Retryable: true},
			},
			want: completion{content: "partial", failed: true, retryable: true, errMsg: "502 bad gateway"},
		},
		{
			name:   "a plain fault is failed and neither retryable nor overflow",
			deltas: scriptedDeltas{{Kind: provider.DeltaError, Err: "boom"}},
			want:   completion{failed: true, errMsg: "boom"},
		},
		{
			name:   "a context overflow is failed and overflow",
			deltas: scriptedDeltas{{Kind: provider.DeltaContextOverflow, Err: "prompt too long"}},
			want:   completion{failed: true, overflow: true, errMsg: "prompt too long"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := baseConfig(&recordingSink{})
			if tc.delimited {
				cfg.Profile = domain.ModelProfile{
					Thinking: domain.ThinkingProfile{Style: domain.ThinkingDelimited, Start: "<think>", End: "</think>"},
				}
			}
			a, err := newAgent(cfg, tc.deltas)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			var seen []provider.DeltaKind

			got := a.collectCompletion(context.Background(), provider.Request{}, func(d provider.Delta) {
				seen = append(seen, d.Kind)
			})

			if got.content != tc.want.content || got.thinking != tc.want.thinking || got.finish != tc.want.finish ||
				got.served != tc.want.served || got.failed != tc.want.failed || got.overflow != tc.want.overflow ||
				got.retryable != tc.want.retryable || got.errMsg != tc.want.errMsg {
				t.Errorf("completion = %+v, want %+v", got, tc.want)
			}
			if (got.usage == nil) != (tc.want.usage == nil) || (got.usage != nil && *got.usage != *tc.want.usage) {
				t.Errorf("usage = %v, want %v", got.usage, tc.want.usage)
			}
			if len(got.toolCalls) != len(tc.want.toolCalls) {
				t.Fatalf("tool calls = %d, want %d", len(got.toolCalls), len(tc.want.toolCalls))
			}
			for i := range got.toolCalls {
				if got.toolCalls[i].ID != tc.want.toolCalls[i].ID || got.toolCalls[i].Function.Name != tc.want.toolCalls[i].Function.Name {
					t.Errorf("tool call %d = %+v, want %+v", i, got.toolCalls[i], tc.want.toolCalls[i])
				}
			}
			if len(seen) != len(tc.deltas) {
				t.Fatalf("observer saw %d deltas, want every one of %d", len(seen), len(tc.deltas))
			}
			for i, kind := range seen {
				if kind != tc.deltas[i].Kind {
					t.Errorf("observer delta %d = %q, want %q (wire order)", i, kind, tc.deltas[i].Kind)
				}
			}
		})
	}
}

// TestCollectCompletionWithoutObserverIsSilent pins the summarizer's use: a nil observer is
// accepted and the collector itself emits nothing — accounting and transcript events are the
// caller's, never recorded inside the fold.
func TestCollectCompletionWithoutObserverIsSilent(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	usage := provider.Usage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5}
	a, err := newAgent(baseConfig(sink), scriptedDeltas{
		{Kind: provider.DeltaContent, Content: "quiet"},
		{Kind: provider.DeltaDone, FinishReason: "stop", Usage: &usage},
	})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	got := a.collectCompletion(context.Background(), provider.Request{}, nil)

	if got.content != "quiet" || got.usage == nil || *got.usage != usage {
		t.Errorf("completion = %+v, want the content and the returned usage", got)
	}
	if len(sink.events) != 0 {
		t.Errorf("events = %d, want none: the collector records nothing", len(sink.events))
	}
}
