package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/library"
	"github.com/airiclenz/apogee/internal/provider"
)

// BatteryVersion is the version of the capability suite below. It stamps every recorded
// fingerprint, because a label derived by one battery is not comparable to a label derived by
// another — change a prompt or a pass rule and every stored claim must be re-earned. The
// constant itself is homed in internal/library (see library.ProbeBatteryVersion): the resolver
// is what has to decide comparability, and it cannot import this package.
const BatteryVersion = library.ProbeBatteryVersion

// Capability names one thing the battery asks the model to do. The values are stable slugs:
// they are reported to the user AND folded into the behavioral fingerprint, so renaming one
// is a battery-version bump, not a cosmetic edit.
type Capability string

const (
	// CapNativeToolCall — the model emits a structured tool_calls entry when offered a tool
	// (mission.md item 3's first probe; the axis model-profile's tool-call-format selects).
	CapNativeToolCall Capability = "native-tool-call"
	// CapStructuredJSON — the model returns a parseable JSON object when asked for one.
	CapStructuredJSON Capability = "structured-json"
	// CapMultiStepChain — the model carries a tool RESULT into a second tool call, which is
	// the thing an agent loop actually needs and the thing small models most often cannot do.
	CapMultiStepChain Capability = "multi-step-chain"
)

// batteryCapabilities is the suite in report order — the order the probes run and the order
// their findings are stated, so the report reads as the session it performed.
var batteryCapabilities = []Capability{CapNativeToolCall, CapStructuredJSON, CapMultiStepChain}

// featureCode is the short token each observed capability contributes to the behavioral
// fingerprint. Short because the label is a thing users paste into a validated-sets alias;
// stable because it IS the identity (see BatteryVersion).
var featureCode = map[Capability]string{
	CapNativeToolCall: "tools",
	CapStructuredJSON: "json",
	CapMultiStepChain: "chain",
}

// Chat is the narrow seam the battery calls the Upstream through: one non-streaming
// round-trip. It is a func rather than an interface because there is exactly one method and
// two implementations — provider.Client.Respond in production, a scripted stub in tests — and
// because the battery must never be handed the streaming seam the loop uses: a capability
// probe wants the whole reply, including the tool calls and the candidate distribution.
type Chat func(ctx context.Context, req provider.Request) (provider.RawResponse, error)

// Finding is one probe's outcome. Observed answers the capability question; Detail says what
// was actually seen (in both directions — a pass that names the tool called is what makes the
// report evidence rather than a verdict); Failure is set only when the probe never completed
// (transport fault, HTTP error), which is distinct from a completed probe that observed
// nothing and is the difference between "this model cannot" and "we could not ask".
type Finding struct {
	Capability Capability
	Observed   bool
	Detail     string
	Failure    string
}

// Battery is the finished capability run: one Finding per capability in suite order, the
// candidate-token distribution when the server exposed one, and what the replies revealed
// about the model's thinking channel. It is a value, so the report, the tier, the suggested
// profile and the fingerprint are all pure functions OF it — which is what makes every one of
// them table-testable against a scripted server.
type Battery struct {
	Version    int
	Findings   []Finding
	Candidates []string
	Thinking   ThinkingObservation
}

// ThinkingObservation records how the model surfaced private reasoning, if at all: through the
// Upstream's own reasoning_content split (Channel), or inline in the visible content under a
// recognisable style. It feeds the suggested model-profile's thinking block and nothing else.
type ThinkingObservation struct {
	Channel bool
	Style   string // "" | "delimited" | "harmony"
	Start   string
	End     string
}

// Observed reports whether the battery saw the capability. An incomplete probe is not an
// observation.
func (b Battery) Observed(c Capability) bool {
	for _, f := range b.Findings {
		if f.Capability == c {
			return f.Observed && f.Failure == ""
		}
	}
	return false
}

// Complete reports whether every capability probe finished. It gates the fingerprint: a run
// where the server dropped one call observed an INCOMPLETE feature set, and recording that as
// an identity would tell every later session the model cannot do something it was never asked.
// The candidate-distribution probe is deliberately not counted — its absence is a legitimate
// finding about the server, not a hole in the evidence.
func (b Battery) Complete() bool {
	if len(b.Findings) != len(batteryCapabilities) {
		return false
	}
	for _, f := range b.Findings {
		if f.Failure != "" {
			return false
		}
	}
	return true
}

// Features returns the observed capabilities' short codes in suite order — the fuzzy feature
// vector the behavioral fingerprint is built from (ADR 0021 §6).
func (b Battery) Features() []string {
	out := make([]string, 0, len(batteryCapabilities))
	for _, c := range batteryCapabilities {
		if b.Observed(c) {
			out = append(out, featureCode[c])
		}
	}
	return out
}

// RunBattery runs the capability suite against chat: the three capability probes in suite
// order, then the candidate-distribution probe. It spends real tokens on a live Upstream and
// is therefore never called on a path the user did not explicitly ask for (ADR 0021 §1).
//
// It returns a Battery rather than an error because a failed probe is a REPORTED finding: a
// server that drops the multi-step call has told us something, and the command's job is to say
// so. Only the caller decides what an incomplete run means (GatherModel refuses to fingerprint
// one).
func RunBattery(ctx context.Context, chat Chat) Battery {
	b := Battery{Version: BatteryVersion}

	toolFinding, toolResp := probeNativeToolCall(ctx, chat)
	jsonFinding, jsonResp := probeStructuredJSON(ctx, chat)
	chainFinding := probeMultiStepChain(ctx, chat)
	b.Findings = []Finding{toolFinding, jsonFinding, chainFinding}

	b.Thinking = observeThinking(toolResp, jsonResp)
	b.Candidates = probeCandidates(ctx, chat)
	return b
}

// probeNativeToolCall offers exactly one tool and asks for it by name. A model that answers in
// prose ("I would call probe_echo...") fails the probe, which is the point: the question is
// whether the STRUCTURED channel carries the call, because that is what the loop reads.
func probeNativeToolCall(ctx context.Context, chat Chat) (Finding, provider.RawResponse) {
	f := Finding{Capability: CapNativeToolCall}
	resp, err := chat(ctx, provider.Request{
		Messages: []provider.Message{
			{Role: "system", Content: batterySystemPrompt},
			{Role: "user", Content: `Call the tool "probe_echo" with the text "apogee".`},
		},
		Tools: []provider.ToolSpec{echoTool},
	})
	if err != nil {
		f.Failure = err.Error()
		f.Detail = "the probe never completed, so this capability is unknown"
		return f, resp
	}
	if len(resp.ToolCalls) == 0 {
		f.Detail = "the reply carried no tool_calls entry — " + firstWords(resp.Content)
		return f, resp
	}
	f.Observed = true
	f.Detail = fmt.Sprintf("the reply carried a native tool_calls entry for %q", resp.ToolCalls[0].Function.Name)
	return f, resp
}

// probeStructuredJSON asks for a bare JSON object with no tools offered. Servers and models
// commonly wrap JSON in a markdown fence even when told not to, so the fence is stripped
// before parsing: fencing is a formatting habit, not an inability to produce structure, and
// conflating the two would mis-identify half the small models this project exists for.
func probeStructuredJSON(ctx context.Context, chat Chat) (Finding, provider.RawResponse) {
	f := Finding{Capability: CapStructuredJSON}
	resp, err := chat(ctx, provider.Request{
		Messages: []provider.Message{
			{Role: "system", Content: batterySystemPrompt},
			{Role: "user", Content: `Reply with only a JSON object with the keys "ok" (boolean true) ` +
				`and "name" (the string "apogee"). No prose.`},
		},
	})
	if err != nil {
		f.Failure = err.Error()
		f.Detail = "the probe never completed, so this capability is unknown"
		return f, resp
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(stripFence(resp.Content)), &decoded); err != nil {
		f.Detail = "the reply did not parse as a JSON object — " + firstWords(resp.Content)
		return f, resp
	}
	f.Observed = true
	if decoded["ok"] == true && decoded["name"] == "apogee" {
		f.Detail = "the reply parsed as a JSON object with both requested keys"
	} else {
		f.Detail = "the reply parsed as a JSON object, though not with the requested keys"
	}
	return f, resp
}

// probeMultiStepChain is the two-round-trip probe: offer a lookup and a report tool, answer the
// model's lookup call with a value it could not have guessed, and see whether that value comes
// back in a SECOND tool call. Carrying a tool result forward is the whole of agentic behaviour,
// and a model that emits one tool call but cannot chain will stall every loop it is put in.
func probeMultiStepChain(ctx context.Context, chat Chat) Finding {
	f := Finding{Capability: CapMultiStepChain}
	tools := []provider.ToolSpec{lookupTool, reportTool}
	messages := []provider.Message{
		{Role: "system", Content: batterySystemPrompt},
		{Role: "user", Content: `Use probe_lookup to look up the key "alpha", then pass the ` +
			`value it returns to probe_report. Use the tools, one step at a time.`},
	}

	first, err := chat(ctx, provider.Request{Messages: messages, Tools: tools})
	if err != nil {
		f.Failure = err.Error()
		f.Detail = "the probe never completed, so this capability is unknown"
		return f
	}
	if len(first.ToolCalls) == 0 {
		f.Detail = "the first step produced no tool call, so there was nothing to chain from"
		return f
	}

	call := first.ToolCalls[0]
	messages = append(messages,
		provider.Message{Role: "assistant", ToolCalls: first.ToolCalls},
		provider.Message{Role: "tool", ToolCallID: call.ID, Content: `{"value":"` + chainSecret + `"}`},
	)

	second, err := chat(ctx, provider.Request{Messages: messages, Tools: tools})
	if err != nil {
		f.Failure = err.Error()
		f.Detail = "the second step never completed, so this capability is unknown"
		return f
	}
	if len(second.ToolCalls) == 0 {
		f.Detail = "the tool result did not produce a second tool call — " + firstWords(second.Content)
		return f
	}

	f.Observed = true
	next := second.ToolCalls[0]
	if strings.Contains(next.Function.Arguments, chainSecret) {
		f.Detail = fmt.Sprintf("the tool result was carried into a second call to %q", next.Function.Name)
	} else {
		f.Detail = fmt.Sprintf("a second call to %q followed, though it did not carry the tool result forward", next.Function.Name)
	}
	return f
}

// probeCandidates asks for a single token with the candidate distribution attached — the
// cheapest call the battery makes and the most identifying one, because two models with the
// same capability set still disagree about what could come next (ADR 0021 §6, "logprobs
// preferred where the server exposes them"). A server that ignores the fields returns nothing
// here and the fingerprint falls back to the feature match alone; a failure is likewise silent,
// since this probe answers an optional question.
func probeCandidates(ctx context.Context, chat Chat) []string {
	maxTokens, temperature := 1, 0.0
	resp, err := chat(ctx, provider.Request{
		Messages: []provider.Message{{Role: "user", Content: candidatePrompt}},
		Sampling: provider.Sampling{MaxTokens: &maxTokens, Temperature: &temperature},
		LogProbs: true,
	})
	if err != nil {
		return nil
	}
	return resp.TopCandidates
}

// observeThinking reads the battery's replies for how the model surfaced reasoning: the
// Upstream's own reasoning_content split, or an inline channel in the visible content. It only
// ever REPORTS — the suggested model-profile is printed for the user to paste, never applied
// (ADR 0021 §5).
func observeThinking(responses ...provider.RawResponse) ThinkingObservation {
	var obs ThinkingObservation
	for _, r := range responses {
		if r.Thinking != "" {
			obs.Channel = true
		}
		if obs.Style != "" {
			continue
		}
		switch {
		case strings.Contains(r.Content, harmonyMarker):
			obs.Style = "harmony"
		case strings.Contains(r.Content, thinkOpen) && strings.Contains(r.Content, thinkClose):
			obs.Style, obs.Start, obs.End = "delimited", thinkOpen, thinkClose
		}
	}
	return obs
}

// The battery's fixed prompt material. Every string here is part of the fingerprint's
// derivation by construction — re-wording one changes what models are observed to do, which is
// why doing so requires a BatteryVersion bump.
const (
	batterySystemPrompt = "You are a capability probe. Follow the instruction exactly and add no extra prose."
	candidatePrompt     = "Complete with a single word: the capital of France is"
	chainSecret         = "omega-7"
	harmonyMarker       = "<|channel|>"
	thinkOpen           = "<think>"
	thinkClose          = "</think>"
)

// The tools the battery offers. Their schemas are deliberately tiny: the probe asks whether the
// model can emit a call at all, not whether it can satisfy an elaborate schema.
var (
	echoTool = provider.ToolSpec{
		Name:        "probe_echo",
		Description: "Echo the given text back.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
	}
	lookupTool = provider.ToolSpec{
		Name:        "probe_lookup",
		Description: "Look up the value stored under a key.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"}},"required":["key"]}`),
	}
	reportTool = provider.ToolSpec{
		Name:        "probe_report",
		Description: "Report a value that was looked up.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`),
	}
)

// stripFence removes a surrounding markdown code fence (```json … ```) so a fenced JSON object
// still parses. Anything that is not a whole fenced block is returned untouched.
func stripFence(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return t
	}
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[i+1:]
	} else {
		return t
	}
	if i := strings.LastIndex(t, "```"); i >= 0 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}

// firstWords renders a snippet of a model reply for a finding's Detail: single-line and short,
// so a chatty model's paragraph cannot take over the report. An empty reply says so, because
// "the model said nothing" is itself the diagnosis.
func firstWords(s string) string {
	t := strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	if t == "" {
		return "the reply was empty"
	}
	const limit = 60
	if len(t) > limit {
		t = t[:limit] + "…"
	}
	return "the reply began: " + t
}
