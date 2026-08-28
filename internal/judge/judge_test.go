package judge

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// stubModel is the id the scripted judge advertises; a test that pins a model pins this one.
const stubModel = "judge-stub"

// useStub points the judge at a scripted upstream answering reply to every request, and clears
// every other gate variable so an ambient APOGEE_LIVE_ENDPOINT on the developer's machine cannot
// send these tests to a real model. opts are passed to the stub, for the one test that gates it
// behind an api key; the caller sets APOGEE_API_KEY itself, after this call.
func useStub(t *testing.T, reply string, opts ...stubllm.Option) *stubllm.Server {
	t.Helper()

	server := stubllm.New(t, stubllm.Script{
		Model: stubModel,
		Turns: []stubllm.Turn{{Text: reply, Repeat: true}},
	}, opts...)
	t.Setenv(endpointEnv, server.URL)
	t.Setenv(modelEnv, stubModel)
	t.Setenv(liveEndpointEnv, "")
	t.Setenv(liveModelEnv, "")
	t.Setenv(apiKeyEnv, "")
	return server
}

// testRubric is the rubric the wire assertions look for, with four distinctive fields.
func testRubric() Rubric {
	return Rubric{
		Item:     "T-42",
		Claim:    "the footer names the bound model",
		PassWhen: "the footer row shows the model id",
		FailsIf:  "the footer row is blank or shows a placeholder",
		Extra:    []string{"the run was offline"},
	}
}

func testContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestAskReadsTheVerdict: the reply's JSON object is the verdict, whether it stands alone or is
// buried in the prose a chatty model wraps around it.
func TestAskReadsTheVerdict(t *testing.T) {
	tests := []struct {
		name        string
		reply       string
		wantPass    bool
		wantReasons []string
	}{
		{
			name:        "a bare fail object",
			reply:       `{"verdict":"fail","reasons":["x"]}`,
			wantReasons: []string{"x"},
		},
		{
			name:     "a bare pass object",
			reply:    `{"verdict":"pass","reasons":[]}`,
			wantPass: true,
		},
		{
			name:        "prose around the object",
			reply:       "Here is my assessment.\n\n```json\n{\"verdict\": \"fail\", \"reasons\": [\"the footer is blank\"]}\n```\n\nHope that helps!",
			wantReasons: []string{"the footer is blank"},
		},
		{
			name:     "a capitalised verdict is still the verdict",
			reply:    `{"verdict":"PASS"}`,
			wantPass: true,
		},
		{
			name:        "a stray brace object before the verdict",
			reply:       "I {wrote 3 files} while checking.\n{\"verdict\": \"fail\", \"reasons\": [\"the footer is blank\"]}",
			wantReasons: []string{"the footer is blank"},
		},
		{
			name:     "an unbalanced brace before the verdict",
			reply:    "Note: the frame shows { here.\n{\"verdict\": \"pass\", \"reasons\": []}",
			wantPass: true,
		},
		{
			name:     "a prose quote before the verdict",
			reply:    "A stray { and a lone \" quote in the prose.\n{\"verdict\": \"pass\", \"reasons\": []}",
			wantPass: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useStub(t, tt.reply)

			v, err := Ask(testContext(t), testRubric(), Artifact{Name: "footer", Kind: KindFrame, Text: "apogee · stub-model"})
			if err != nil {
				t.Fatalf("Ask: %v", err)
			}
			if v.Pass != tt.wantPass {
				t.Errorf("Pass = %v, want %v (raw %q)", v.Pass, tt.wantPass, v.Raw)
			}
			if got, want := strings.Join(v.Reasons, "|"), strings.Join(tt.wantReasons, "|"); got != want {
				t.Errorf("Reasons = %v, want %v", v.Reasons, tt.wantReasons)
			}
			if v.Raw == "" {
				t.Error("Raw is empty, want the JSON object the verdict was parsed from")
			}
		})
	}
}

// TestAskRefusesAnUnreadableReply: a reply that is not a verdict is an ERROR, not a fail — a judge
// that malfunctioned has not judged, and reporting it as a failed claim would blame the code.
func TestAskRefusesAnUnreadableReply(t *testing.T) {
	tests := []struct {
		name  string
		reply string
		want  string
	}{
		{name: "a third verdict value", reply: `{"verdict":"maybe"}`, want: `verdict "maybe"`},
		{name: "no object at all", reply: "I think it looks fine.", want: "no JSON object"},
		{name: "reasons of the wrong shape", reply: `{"verdict":"fail","reasons":"nope"}`, want: "decode"},
		{name: "braces but no verdict object", reply: "I {wrote 3 files} and {stopped}.", want: "decode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useStub(t, tt.reply)

			v, err := Ask(testContext(t), testRubric(), Artifact{Name: "footer", Text: "x"})
			if err == nil {
				t.Fatalf("Ask returned verdict %+v, want an error", v)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to mention %q", err, tt.want)
			}
			if v.Pass {
				t.Error("Pass = true on an error, want the zero verdict")
			}
		})
	}
}

// TestAskSendsTheKeyTrimmed: APOGEE_API_KEY is read the way its sibling gate variables are —
// trimmed, because a value pasted from a shell or a secret store carries whitespace an upstream
// rejects, and the rejection looks exactly like a wrong key. Unset stays keyless (ADR 0036): the
// judge sends no Authorization header, and an upstream that wanted one refuses the REQUEST — that
// refusal must surface as an error, never as a verdict about the code under test.
func TestAskSendsTheKeyTrimmed(t *testing.T) {
	const reply = `{"verdict":"pass","reasons":[]}`

	t.Run("a padded key reaches the upstream trimmed", func(t *testing.T) {
		useStub(t, reply, stubllm.WithAPIKey("k"))
		t.Setenv(apiKeyEnv, "  k\t")

		v, err := Ask(testContext(t), testRubric(), Artifact{Name: "footer", Kind: KindFrame, Text: "x"})
		if err != nil {
			t.Fatalf("Ask: %v", err)
		}
		if !v.Pass {
			t.Errorf("Pass = false, want the stub's verdict (raw %q)", v.Raw)
		}
	})

	t.Run("an unset key surfaces the upstream refusal", func(t *testing.T) {
		useStub(t, reply, stubllm.WithAPIKey("k"))

		v, err := Ask(testContext(t), testRubric(), Artifact{Name: "footer", Kind: KindFrame, Text: "x"})
		if err == nil {
			t.Fatalf("Ask returned verdict %+v, want the upstream's refusal", v)
		}
		if !strings.Contains(err.Error(), "401") {
			t.Errorf("error = %v, want it to carry the upstream's 401", err)
		}
		if v.Pass {
			t.Error("Pass = true on an error, want the zero verdict")
		}
	})
}

// TestAskSendsTheRubricAndTheArtifacts: every field of the rubric and every artifact — by name and
// by content — reaches the model, at temperature 0. A rubric field silently dropped from the
// prompt would make the judge answer a question nobody asked.
func TestAskSendsTheRubricAndTheArtifacts(t *testing.T) {
	server := useStub(t, `{"verdict":"pass"}`)

	r := testRubric()
	if _, err := Ask(testContext(t), r,
		Artifact{Name: "the footer", Kind: KindFrame, Text: "apogee · stub-model"},
		Artifact{Name: "the session file", Kind: KindFile, Text: "{\"id\":\"s1\"}"},
	); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	requests := server.Requests()
	if len(requests) != 1 {
		t.Fatalf("the judge made %d requests, want exactly 1 (one vote, one round-trip)", len(requests))
	}
	req := requests[0]
	if req.Model != stubModel {
		t.Errorf("model = %q, want the pinned %q", req.Model, stubModel)
	}
	if req.Stream {
		t.Error("the judge streamed its request, want one non-streaming round-trip")
	}
	if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
		t.Fatalf("messages = %+v, want a system prompt and one user message", req.Messages)
	}
	prompt := req.Messages[1].Content
	for _, want := range []string{r.Item, r.Claim, r.PassWhen, r.FailsIf, r.Extra[0], "the footer", "the session file", "apogee · stub-model", `{"id":"s1"}`} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the user message does not carry %q:\n%s", want, prompt)
		}
	}
	if !strings.Contains(req.Messages[0].Content, "release tester") {
		t.Errorf("system message = %q, want the fixed judging instructions", req.Messages[0].Content)
	}
}

// TestAskResolvesTheModelFromTheEndpoint: with no model pinned, the judge asks the endpoint what
// it serves rather than inventing an id the server would reject.
func TestAskResolvesTheModelFromTheEndpoint(t *testing.T) {
	server := useStub(t, `{"verdict":"pass"}`)
	t.Setenv(modelEnv, "")

	if _, err := Ask(testContext(t), testRubric(), Artifact{Name: "footer", Text: "x"}); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	requests := server.Requests()
	if len(requests) != 1 {
		t.Fatalf("got %d requests, want 1", len(requests))
	}
	if requests[0].Model != stubModel {
		t.Errorf("model = %q, want the advertised %q", requests[0].Model, stubModel)
	}
}

// TestPairwiseLabelsBothArtifacts: a comparison names which artifact is the before and which is
// the after, and says what "no worse" means — the judge cannot infer the direction from two texts.
func TestPairwiseLabelsBothArtifacts(t *testing.T) {
	server := useStub(t, `{"verdict":"pass"}`)

	before := Artifact{Name: "v0.17.1 outcome slot", Kind: KindFrame, Text: "✔ wrote main.go"}
	after := Artifact{Name: "HEAD outcome slot", Kind: KindFrame, Text: "✔ wrote main.go (2 lines)"}
	if _, err := Pairwise(testContext(t), testRubric(), before, after); err != nil {
		t.Fatalf("Pairwise: %v", err)
	}

	prompt := server.Requests()[0].Messages[1].Content
	for _, want := range []string{before.Name, after.Name, before.Text, after.Text, "BEFORE", "AFTER", "no worse"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the pairwise prompt does not carry %q:\n%s", want, prompt)
		}
	}
	if strings.Index(prompt, before.Text) > strings.Index(prompt, after.Text) {
		t.Error("the before artifact is rendered after the after artifact, want them in the stated order")
	}
}

// TestRequireSkipsWithoutTheGate: no endpoint means SKIP, never a pass — a judge test that
// silently succeeds with no judge is worse than no test.
func TestRequireSkipsWithoutTheGate(t *testing.T) {
	t.Setenv(endpointEnv, "")
	t.Setenv(liveEndpointEnv, "")

	if Enabled() {
		t.Fatal("Enabled() = true with both endpoint variables cleared")
	}
	rec := &recordingTB{TB: t}
	Require(rec, testContext(t), testRubric(), Artifact{Name: "footer", Text: "x"})

	if !rec.skipped {
		t.Error("Require did not skip with the gate unset")
	}
	if !strings.Contains(rec.skipMessage, endpointEnv) {
		t.Errorf("skip message = %q, want it to name %s", rec.skipMessage, endpointEnv)
	}
	if len(rec.failures) != 0 {
		t.Errorf("Require reported %v, want a skip and nothing else", rec.failures)
	}
}

// TestRequireFailsOnAFailingVerdict: with the gate set, a fail is binding — the reasons and the
// re-run rule both reach the test's output.
func TestRequireFailsOnAFailingVerdict(t *testing.T) {
	useStub(t, `{"verdict":"fail","reasons":["the footer is blank"]}`)

	rec := &recordingTB{TB: t}
	Require(rec, testContext(t), testRubric(), Artifact{Name: "footer", Text: "   "})

	if rec.skipped {
		t.Fatal("Require skipped with the gate set")
	}
	joined := strings.Join(rec.failures, "\n")
	for _, want := range []string{"T-42", "the footer is blank", "re-run"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the failure does not mention %q:\n%s", want, joined)
		}
	}
}

// TestFrameArtifactNamesTheTones: a styled frame reaches the judge with the caller's tone names
// in it, not with SGR escapes; an unnamed colour stays unnamed.
func TestFrameArtifactNamesTheTones(t *testing.T) {
	screen := tuitest.NewScreen(24, 2)
	t.Cleanup(screen.Close)
	// Row 0: a truecolor-red word, a bold word, and a word in a colour the caller never named.
	if _, err := screen.Write([]byte("\x1b[38;2;255;0;0mfailed\x1b[0m \x1b[1mok\x1b[0m \x1b[38;2;0;255;0mgreen\x1b[0m")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	frame := screen.Snapshot()
	red := Tone{Name: "red", Color: color.RGBA{R: 255, A: 255}}

	plain := FrameArtifact("the pane", frame, false)
	if plain.Kind != KindFrame {
		t.Errorf("Kind = %q, want %q", plain.Kind, KindFrame)
	}
	if strings.Contains(plain.Text, "⟨") {
		t.Errorf("the unstyled artifact carries tags: %q", plain.Text)
	}
	if !strings.Contains(plain.Text, "failed ok green") {
		t.Errorf("the unstyled artifact = %q, want the frame's plain text", plain.Text)
	}

	styled := FrameArtifact("the pane", frame, true, red)
	if !strings.Contains(styled.Text, "⟨red⟩failed⟨/red⟩") {
		t.Errorf("the styled artifact does not name the red run:\n%q", styled.Text)
	}
	if !strings.Contains(styled.Text, "⟨bold⟩ok⟨/bold⟩") {
		t.Errorf("the styled artifact does not name the bold run:\n%q", styled.Text)
	}
	if strings.Contains(styled.Text, "green⟨/") {
		t.Errorf("an unnamed colour was tagged anyway:\n%q", styled.Text)
	}
	if strings.Contains(styled.Text, "\x1b") {
		t.Errorf("the styled artifact still carries escape sequences:\n%q", styled.Text)
	}
}

// recordingTB stands in for a *testing.T so a test can observe what Require did to it: the real
// Skip and Fatalf end the calling goroutine, which is exactly what an assertion about them cannot
// survive. Everything else is the embedded T's.
type recordingTB struct {
	testing.TB
	skipped     bool
	skipMessage string
	failures    []string
}

func (r *recordingTB) Skip(args ...any) {
	r.skipped = true
	r.skipMessage = fmt.Sprint(args...)
}

func (r *recordingTB) SkipNow() { r.skipped = true }

func (r *recordingTB) Errorf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}

func (r *recordingTB) Error(args ...any) { r.failures = append(r.failures, fmt.Sprint(args...)) }

func (r *recordingTB) Fatalf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}

func (r *recordingTB) Fatal(args ...any) { r.failures = append(r.failures, fmt.Sprint(args...)) }

// TestJudgeSelfCheck is the kit's probe of the CONFIGURED judge: a rubric a child could settle,
// put to the real model in both directions. It is gated like every judge test and it runs first
// in `make live-eval`, so a judge that agrees with everything — or disagrees with everything —
// is reported here rather than as twenty rubric failures further down the run.
//
//	APOGEE_JUDGE_ENDPOINT=http://127.0.0.1:1111 go test -count=1 -run TestJudgeSelfCheck -v ./internal/judge/
func TestJudgeSelfCheck(t *testing.T) {
	if !Enabled() {
		Skip(t)
	}
	t.Logf("judge self-check: endpoint=%s model=%s", endpoint(), envOr(modelEnv, envOr(liveModelEnv, "(discovered)")))

	r := Rubric{
		Item:     "self-check",
		Claim:    "the artifact's text is the word hello",
		PassWhen: "the text is exactly the word hello",
		FailsIf:  "the text is any other word",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	agrees, err := Ask(ctx, r, Artifact{Name: "the text", Kind: KindStdout, Text: "hello"})
	if err != nil {
		t.Fatalf("the judge could not answer the positive self-check: %v", err)
	}
	if !agrees.Pass {
		t.Errorf("the judge failed a rubric it must pass (%q against %q): %v\nthe configured judge is not usable; every verdict below it is suspect",
			r.Claim, "hello", agrees.Reasons)
	}

	refuses, err := Ask(ctx, r, Artifact{Name: "the text", Kind: KindStdout, Text: "goodbye"})
	if err != nil {
		t.Fatalf("the judge could not answer the negative self-check: %v", err)
	}
	if refuses.Pass {
		t.Errorf("the judge passed a rubric it must fail (%q against %q): raw %s\nthe configured judge agrees with everything; every pass below it is meaningless",
			r.Claim, "goodbye", refuses.Raw)
	}
}

// envOr is the self-check's log helper: the variable, or a stand-in when it is unset.
func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
