package probe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// script says how a fake Upstream answers each battery probe. Every field is a capability the
// battery asks about, so a test names the model it is pretending to be rather than a pile of
// canned JSON.
type script struct {
	nativeTools bool
	structured  bool
	chain       bool
	logprobs    bool
	// thinking is prepended to the JSON probe's visible content, so a test can play a model
	// that leaks an inline reasoning channel.
	thinking string
	// fail makes the server answer every chat call with a 500, standing in for an Upstream
	// that is reachable for discovery but cannot complete a generation.
	fail bool
}

// batteryServer starts an httptest Upstream that answers the battery per s. It branches on the
// SHAPE of each request — how many tools were offered, how many messages were sent, whether
// logprobs were asked for — which is exactly how a real server distinguishes them, so the test
// exercises the real provider client and the real wire encoding rather than a stub seam.
func batteryServer(t *testing.T, s script) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"fake-model","context_length":4096}]}`))
			return
		}
		if s.fail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Tools    []json.RawMessage `json:"tools"`
			LogProbs *bool             `json:"logprobs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("upstream received undecodable request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		switch {
		case body.LogProbs != nil && *body.LogProbs:
			_, _ = w.Write([]byte(s.candidateReply()))
		case len(body.Tools) == 1:
			_, _ = w.Write([]byte(s.toolReply()))
		case len(body.Tools) == 2:
			_, _ = w.Write([]byte(s.chainReply(len(body.Messages))))
		default:
			_, _ = w.Write([]byte(s.jsonReply()))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (s script) toolReply() string {
	if !s.nativeTools {
		return chatReply(`"I would call probe_echo with the text apogee."`, "")
	}
	return chatReply("null", `,"tool_calls":[{"id":"call-1","type":"function",`+
		`"function":{"name":"probe_echo","arguments":"{\"text\":\"apogee\"}"}}]`)
}

func (s script) jsonReply() string {
	content := "Sure! Here is what you asked for, in prose."
	if s.structured {
		content = "```json\n{\"ok\":true,\"name\":\"apogee\"}\n```"
	}
	return chatReply(jsonString(s.thinking+content), "")
}

func (s script) chainReply(messages int) string {
	if !s.chain {
		return chatReply(`"I cannot use tools right now."`, "")
	}
	if messages <= 2 {
		return chatReply("null", `,"tool_calls":[{"id":"call-2","type":"function",`+
			`"function":{"name":"probe_lookup","arguments":"{\"key\":\"alpha\"}"}}]`)
	}
	return chatReply("null", `,"tool_calls":[{"id":"call-3","type":"function",`+
		`"function":{"name":"probe_report","arguments":"{\"value\":\"omega-7\"}"}}]`)
}

func (s script) candidateReply() string {
	if !s.logprobs {
		return chatReply(`" Paris"`, "")
	}
	return `{"choices":[{"message":{"content":" Paris"},"logprobs":{"content":[{"token":" Paris",` +
		`"top_logprobs":[{"token":" Paris"},{"token":" the"},{"token":" a"}]}]},"finish_reason":"length"}]}`
}

// chatReply renders one OpenAI chat-completions reply. content is a JSON value (a quoted
// string, or null for a tool-call-only turn) and extra is spliced into the message object.
func chatReply(content, extra string) string {
	return `{"choices":[{"message":{"content":` + content + extra + `},"finish_reason":"stop"}]}`
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

// runBattery drives the real provider client against the fake Upstream, so the battery under
// test speaks the same wire a session does.
func runBattery(t *testing.T, s script) Battery {
	t.Helper()
	srv := batteryServer(t, s)
	client := provider.NewClient(srv.URL, "fake-model")
	return RunBattery(context.Background(), func(ctx context.Context, req provider.Request) (provider.RawResponse, error) {
		return client.Respond(ctx, req)
	})
}

// The all-pass model: every capability observed, the server exposes a candidate distribution,
// and the derived tier, profile and fingerprint all follow from that.
func TestBatteryAllPass(t *testing.T) {
	t.Parallel()
	b := runBattery(t, script{nativeTools: true, structured: true, chain: true, logprobs: true})

	if !b.Complete() {
		t.Fatalf("every probe answered, so the battery should be complete: %+v", b.Findings)
	}
	for _, c := range batteryCapabilities {
		if !b.Observed(c) {
			t.Errorf("capability %s should have been observed: %+v", c, b.Findings)
		}
	}
	if len(b.Candidates) != 3 {
		t.Errorf("candidates = %v; want the 3 tokens the server exposed", b.Candidates)
	}
	if got := Tier(b); got != TierFull {
		t.Errorf("tier = %q; want %q", got, TierFull)
	}

	fp := Fingerprint("fake-model", b)
	if fp.Confidence != domain.ConfidenceMedium {
		t.Errorf("confidence = %v; want medium — the behavioral tier", fp.Confidence)
	}
	if fp.Label != "fake-model" {
		t.Errorf("label = %q; want the advertised label unchanged — the battery raises the tier, "+
			"it does not rename the model (ADR 0021, Amendment 2026-07-22)", fp.Label)
	}
	if sig := BehaviorSignature(b); !strings.HasPrefix(sig, "probe:1:tools+json+chain:lp-") {
		t.Errorf("signature = %q; want the battery/features/logprob-digest form", sig)
	}
}

// The no-native-tools model: it answers in prose when offered a tool, so neither the tool probe
// nor the chain that depends on it passes — and the suggested profile switches the tool-call
// format to the text one, which is the whole point of suggesting a profile at all.
func TestBatteryNoNativeTools(t *testing.T) {
	t.Parallel()
	b := runBattery(t, script{structured: true, logprobs: true})

	if b.Observed(CapNativeToolCall) || b.Observed(CapMultiStepChain) {
		t.Errorf("a model that answers tools in prose observes neither capability: %+v", b.Findings)
	}
	if !b.Observed(CapStructuredJSON) {
		t.Errorf("structured JSON should still have been observed: %+v", b.Findings)
	}
	if got := Tier(b); got != TierBasic {
		t.Errorf("tier = %q; want %q for one observed capability", got, TierBasic)
	}
	if got := SuggestProfile(b).ToolCallFormat; got != domain.FormatMarkdownFenced {
		t.Errorf("suggested tool-call-format = %q; want the text format for a model with no native calls", got)
	}
}

// The JSON-fails model: it calls tools and chains them but answers prose when asked for an
// object. The feature set — and therefore the identity — differs from the all-pass model's,
// which is what makes the fingerprint a feature match rather than a label.
func TestBatteryJSONFails(t *testing.T) {
	t.Parallel()
	b := runBattery(t, script{nativeTools: true, chain: true, logprobs: true})

	if b.Observed(CapStructuredJSON) {
		t.Errorf("prose is not a JSON object: %+v", b.Findings)
	}
	if got := Tier(b); got != TierStructured {
		t.Errorf("tier = %q; want %q for two observed capabilities", got, TierStructured)
	}

	all := runBattery(t, script{nativeTools: true, structured: true, chain: true, logprobs: true})
	if BehaviorSignature(b) == BehaviorSignature(all) {
		t.Error("two models with different capability sets must not share one behavioral signature")
	}
}

// A server that cannot complete a generation leaves every probe INCOMPLETE, and an incomplete
// battery mints no identity: recording one would tell every later session the model cannot do
// things it was never successfully asked.
func TestBatteryIncompleteMintsNoFingerprint(t *testing.T) {
	t.Parallel()
	b := runBattery(t, script{fail: true})

	if b.Complete() {
		t.Fatal("a battery whose probes all failed is not complete")
	}
	for _, f := range b.Findings {
		if f.Failure == "" {
			t.Errorf("probe %s should carry the transport failure: %+v", f.Capability, f)
		}
	}
	if fp := Fingerprint("fake-model", b); !fp.IsZero() {
		t.Errorf("an incomplete battery derived a fingerprint %q; it must derive none", fp.Label)
	}
	if sig := BehaviorSignature(b); sig != "" {
		t.Errorf("an incomplete battery signed %q; a run with a hole in it observes nothing", sig)
	}
}

// A server that ignores the logprobs fields is a finding, not a failure: the battery still
// completes and the fingerprint still resolves — one notch less discriminating, with no
// distribution component in the label.
func TestBatteryWithoutLogprobs(t *testing.T) {
	t.Parallel()
	b := runBattery(t, script{nativeTools: true, structured: true, chain: true})

	if len(b.Candidates) != 0 {
		t.Errorf("candidates = %v; want none from a server that exposes no distribution", b.Candidates)
	}
	if sig := BehaviorSignature(b); sig != "probe:1:tools+json+chain" {
		t.Errorf("signature = %q; want the feature-only form with no logprob digest", sig)
	}
}

// The same model probed twice yields the same signature: it is a feature match over outcomes,
// so it does not move with the response text the way a response hash would.
func TestFingerprintIsStableAcrossRuns(t *testing.T) {
	t.Parallel()
	s := script{nativeTools: true, structured: true, chain: true, logprobs: true}
	first := BehaviorSignature(runBattery(t, s))
	second := BehaviorSignature(runBattery(t, s))

	if first != second {
		t.Errorf("two runs of one model disagreed: %q vs %q", first, second)
	}
}

// An inline thinking channel in the visible content is observed and surfaces as the suggested
// thinking style — the second axis of the model profile the battery exists to fill in.
func TestBatteryObservesInlineThinking(t *testing.T) {
	t.Parallel()
	b := runBattery(t, script{nativeTools: true, structured: true, chain: true, thinking: "<think>hmm</think>"})

	profile := SuggestProfile(b)
	if profile.Thinking.Style != domain.ThinkingDelimited {
		t.Fatalf("thinking style = %q; want delimited for a model emitting <think> tags", profile.Thinking.Style)
	}
	if profile.Thinking.Start != "<think>" || profile.Thinking.End != "</think>" {
		t.Errorf("thinking delimiters = %q/%q; want the tokens actually seen", profile.Thinking.Start, profile.Thinking.End)
	}
	if yaml := ProfileYAML(profile); !strings.Contains(yaml, `start: "<think>"`) {
		t.Errorf("paste-ready YAML must quote the delimiter so YAML cannot re-read it:\n%s", yaml)
	}
}
