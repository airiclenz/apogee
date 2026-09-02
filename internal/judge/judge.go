package judge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/provider"
)

// The environment the gate reads. The judge pair wins where it is set, so a run can point the
// judge at a bigger model than the one under test; the live pair is the fallback, which makes
// `APOGEE_LIVE_ENDPOINT=… go test` judge with the same server it exercises.
const (
	endpointEnv     = "APOGEE_JUDGE_ENDPOINT"
	modelEnv        = "APOGEE_JUDGE_MODEL"
	liveEndpointEnv = "APOGEE_LIVE_ENDPOINT"
	liveModelEnv    = "APOGEE_LIVE_MODEL"
	// apiKeyEnv is the ADR 0036 startup overlay, not an ADR 0047 per-entry key source: it is one
	// value for whatever endpoint the judge dials, and unset means keyless — the same floor every
	// endpoint apogee dials has, so a local server that wants no key needs nothing set.
	apiKeyEnv = "APOGEE_API_KEY"
)

// skipMessage is the one line a skipped judge test prints. It names the variable to set rather
// than saying "judge disabled", because a skip nobody can act on is a skip nobody notices.
const skipMessage = "set APOGEE_JUDGE_ENDPOINT (and optionally APOGEE_JUDGE_MODEL) to run judge tests"

// requestTimeout bounds ONE attempt at a verdict. It is generous because a local server judging a
// full frame is slower than the same server answering a chat turn, and a judge that times out
// looks exactly like a judge that failed.
const requestTimeout = 2 * time.Minute

// discoveryTimeout bounds the /v1/models lookup that resolves an unpinned model.
const discoveryTimeout = 15 * time.Second

// Rubric is one claim put to the judge, in the shape the release checklist already writes claims
// in: the item it belongs to, what is claimed, and the two oracles. PassWhen and FailsIf are
// copied VERBATIM from the checklist step the test replaces — a rubric paraphrased in the test is
// a rubric that drifts from the thing the release actually promises.
//
// One Rubric is one claim. Two claims in one rubric produce a verdict that cannot say which of
// them failed, and a reason list nobody can act on.
type Rubric struct {
	// Item is the checklist id or short label the claim belongs to, e.g. "T-15".
	Item string
	// Claim is the single thing being judged, in one sentence.
	Claim string
	// PassWhen is the checklist's "Pass when" oracle, verbatim.
	PassWhen string
	// FailsIf is the checklist's "Fails if" oracle, verbatim.
	FailsIf string
	// Extra are additional constraints the judge must honour — context the artifacts do not
	// carry, such as "the run had no network" or "row 0 is the header".
	Extra []string
}

// Verdict is what the judge answered: the binary outcome, the reasons it gave (populated on a
// fail, allowed to be empty on a pass), and the raw JSON object it was parsed from, kept so a
// failure message can show what the model actually said.
type Verdict struct {
	Pass    bool
	Reasons []string
	Raw     string
}

// Enabled reports whether a judge endpoint is configured — the gate every judge test checks
// before it does anything.
func Enabled() bool { return endpoint() != "" }

// Skip skips t with the line that says how to enable the judge.
func Skip(t testing.TB) {
	t.Helper()
	t.Skip(skipMessage)
}

// Client builds a provider client aimed at the configured judge endpoint and returns it beside
// the model it resolved. It is the same client, key and model resolution [Ask] uses, exported for
// the one test that needs the judge model as an AGENT rather than as an assessor: the newcomer
// container (checklist T-23) drives a tool-use loop of its own and then hands the transcript back
// to [Require] for the verdict.
//
// The caller owns the returned client and must Close it. An empty gate is an error, never a nil
// client: a caller that forgot [Enabled] gets told so rather than panicking.
func Client(ctx context.Context) (*provider.Client, string, error) {
	base := endpoint()
	if base == "" {
		return nil, "", fmt.Errorf("no judge endpoint: set %s or %s", endpointEnv, liveEndpointEnv)
	}
	client := provider.NewClient(base, "",
		provider.WithAPIKey(apiKey()),
		provider.WithRequestTimeout(requestTimeout),
	)
	model, err := resolveModel(ctx, client)
	if err != nil {
		_ = client.Close()
		return nil, "", err
	}
	client.SetModel(model)
	return client, model, nil
}

// Ask puts one Rubric and its artifacts to the configured judge and returns the verdict. It is
// one non-streaming round-trip at temperature 0, with one vote: a majority of local votes costs
// N× the time and buys agreement rather than accuracy.
//
// The error return is for the judge failing to answer — no endpoint, an unreachable server, a
// reply that is not the JSON object asked for. A reply that IS a verdict is never an error, even
// when it says fail: those are two different failures and a caller ([Require]) treats them so.
func Ask(ctx context.Context, r Rubric, artifacts ...Artifact) (Verdict, error) {
	return ask(ctx, r, "", artifacts)
}

// Pairwise asks whether after is NO WORSE than before under the rubric — the form a "nothing
// regressed since the last release" claim takes, where no absolute oracle exists but a
// comparison does. The verdict is pass when after holds up, fail when it is worse.
func Pairwise(ctx context.Context, r Rubric, before, after Artifact) (Verdict, error) {
	comparison := fmt.Sprintf(
		"This is a COMPARISON. The artifact named %q is the BEFORE and the artifact named %q is "+
			"the AFTER. Answer \"pass\" when the AFTER is no worse than the BEFORE under this "+
			"rubric, and \"fail\" only when the AFTER is worse; a difference that is not worse is "+
			"a pass, and so is an AFTER that is better.",
		before.Name, after.Name)
	return ask(ctx, r, comparison, []Artifact{before, after})
}

// Require is the binding assertion: it skips t when the gate is unset, fails t outright when the
// judge could not answer, and fails t with the reasons when the verdict is fail.
//
// The re-run rule is printed with the failure rather than left in the design doc, because the
// person reading it is mid-release and needs to know that ONE fail from a local judge is not yet
// a fail (temperature 0 is not bit-reproducible on a batching server).
func Require(t testing.TB, ctx context.Context, r Rubric, artifacts ...Artifact) {
	t.Helper()
	if !Enabled() {
		Skip(t)
		return
	}
	v, err := Ask(ctx, r, artifacts...)
	if err != nil {
		t.Fatalf("judge %s: %v", r.Item, err)
	}
	if v.Pass {
		return
	}
	t.Errorf("judge %s FAIL: %s\n%s", r.Item, r.Claim, failureDetail(v))
}

// failureDetail is the body of a failing verdict's message: the reasons the judge gave, or the
// raw object when it gave none, plus the re-run rule.
func failureDetail(v Verdict) string {
	var b strings.Builder
	if len(v.Reasons) == 0 {
		fmt.Fprintf(&b, "  (the judge gave no reasons) raw: %s\n", oneLine(v.Raw))
	}
	for _, reason := range v.Reasons {
		fmt.Fprintf(&b, "  reason: %s\n", reason)
	}
	b.WriteString("  a local judge is not bit-reproducible at temperature 0: re-run this test ONCE " +
		"(go test -run <name> -count=1) before believing the verdict; two fails in a row are a real fail")
	return b.String()
}

// ask is the one code path behind Ask and Pairwise; comparison is the extra instruction Pairwise
// adds and is empty for a plain verdict.
func ask(ctx context.Context, r Rubric, comparison string, artifacts []Artifact) (Verdict, error) {
	base := endpoint()
	if base == "" {
		return Verdict{}, fmt.Errorf("no judge endpoint: set %s or %s", endpointEnv, liveEndpointEnv)
	}
	if len(artifacts) == 0 {
		return Verdict{}, errors.New("no artifacts to judge: a verdict on nothing is not a verdict")
	}

	client := provider.NewClient(base, "",
		provider.WithAPIKey(apiKey()),
		provider.WithRequestTimeout(requestTimeout),
	)
	defer func() { _ = client.Close() }()

	model, err := resolveModel(ctx, client)
	if err != nil {
		return Verdict{}, err
	}
	client.SetModel(model)

	zero := 0.0
	resp, err := client.Respond(ctx, provider.Request{
		Model: model,
		Messages: []provider.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt(r, comparison, artifacts)},
		},
		Sampling: provider.Sampling{Temperature: &zero},
	})
	if err != nil {
		return Verdict{}, fmt.Errorf("ask %s at %s: %w", model, base, err)
	}
	v, err := parseVerdict(resp.Content)
	if err != nil {
		return v, fmt.Errorf("%w (model %s)", err, model)
	}
	return v, nil
}

// endpoint is the judge server, falling back to the live one so a single variable turns both on.
func endpoint() string {
	if e := strings.TrimSpace(os.Getenv(endpointEnv)); e != "" {
		return e
	}
	return strings.TrimSpace(os.Getenv(liveEndpointEnv))
}

// apiKey is the bearer token the judge sends, trimmed like every other variable the gate reads —
// a value pasted from a shell or a secret store carries whitespace the server would reject.
// Unset stays unset: the client sends no Authorization header at all.
func apiKey() string { return strings.TrimSpace(os.Getenv(apiKeyEnv)) }

// resolveModel picks the judging model: the pinned judge model, else the pinned live model, else
// whatever the endpoint advertises first. Discovery is the fallback rather than the default
// because a server with several models loaded should not have the judge silently follow whichever
// one happens to be listed first.
func resolveModel(ctx context.Context, client *provider.Client) (string, error) {
	if m := strings.TrimSpace(os.Getenv(modelEnv)); m != "" {
		return m, nil
	}
	if m := strings.TrimSpace(os.Getenv(liveModelEnv)); m != "" {
		return m, nil
	}
	discoverCtx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	info, err := client.Discover(discoverCtx)
	if err != nil {
		return "", fmt.Errorf("discover a judge model (pin one with %s): %w", modelEnv, err)
	}
	if info.ActiveModel == "" {
		return "", fmt.Errorf("the judge endpoint advertises no model (pin one with %s)", modelEnv)
	}
	return info.ActiveModel, nil
}

// systemPrompt is fixed: every rubric is judged under the same instructions, so a difference in
// verdicts is a difference in the artifacts and not in how the question was asked.
const systemPrompt = `You are a release tester. You are given one claim about a program's observed output, the two oracles that settle it, and the artifacts that were observed.

Answer ONLY a single JSON object and nothing else — no prose, no markdown fence, no explanation around it:

{"verdict": "pass", "reasons": []}
{"verdict": "fail", "reasons": ["<what is wrong, one per entry>"]}

Rules:
- "verdict" is exactly "pass" or "fail". No other value is allowed.
- Judge ONLY the claim in the rubric, against the artifacts given. Do not invent requirements the rubric does not state, and do not report anything the rubric does not ask about.
- Judge what the artifacts SHOW. If an artifact does not carry the evidence the claim needs, that is a fail, with the missing evidence as the reason.
- Give a reason for every fail. Reasons may be empty on a pass.`

// userPrompt renders the rubric and the artifacts. The artifacts are fenced and labelled by name
// and kind so a reason can point at one of them, and the fence is widened past any backtick run
// inside the text so a frame containing a code block cannot end its own block.
func userPrompt(r Rubric, comparison string, artifacts []Artifact) string {
	var b strings.Builder

	b.WriteString("# Rubric\n\n")
	if r.Item != "" {
		fmt.Fprintf(&b, "Item: %s\n", r.Item)
	}
	fmt.Fprintf(&b, "Claim: %s\n", r.Claim)
	fmt.Fprintf(&b, "Pass when: %s\n", r.PassWhen)
	fmt.Fprintf(&b, "Fails if: %s\n", r.FailsIf)
	for _, extra := range r.Extra {
		fmt.Fprintf(&b, "Also: %s\n", extra)
	}
	if comparison != "" {
		b.WriteString("\n" + comparison + "\n")
	}

	for _, a := range artifacts {
		fmt.Fprintf(&b, "\n# Artifact: %s (%s)\n\n", a.Name, a.kind())
		fence := fenceFor(a.Text)
		b.WriteString(fence + "\n" + a.Text)
		if !strings.HasSuffix(a.Text, "\n") {
			b.WriteByte('\n')
		}
		b.WriteString(fence + "\n")
	}

	b.WriteString("\nAnswer with the JSON object only.")
	return b.String()
}

// fenceFor returns a backtick fence longer than the longest run of backticks in text.
func fenceFor(text string) string {
	longest := 0
	run := 0
	for _, r := range text {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
			continue
		}
		run = 0
	}
	if longest < 3 {
		longest = 3
	}
	return strings.Repeat("`", longest+1)
}

// parseVerdict reads the reply strictly, but not blindly: it walks EVERY balanced {…} span in
// the reply, in order, and takes the first one that decodes to a verdict of exactly pass or fail
// (case and surrounding space are forgiven — a local model answering "Pass" meant the verdict,
// not a third value). A span that does not balance, does not decode, or carries a third verdict
// word moves the anchor to the next `{`, so a stray brace in the prose a chatty model wraps
// around its answer cannot hide the object behind it. Anything else is an error rather than a
// fail: a judge that could not be understood has not judged, and reporting that as a failed claim
// would blame the code for the judge's malfunction.
//
// When no span wins, the reply is reported by what was actually wrong with it: a span that
// decoded but named a third verdict word is the most specific complaint and wins over a span that
// did not decode at all; a reply with no balanced object anywhere has no JSON object in it.
func parseVerdict(reply string) (Verdict, error) {
	var (
		wordErr   error
		wordRaw   string
		decodeErr error
		decodeRaw string
	)
	for anchor := 0; anchor < len(reply); anchor++ {
		next := strings.IndexByte(reply[anchor:], '{')
		if next < 0 {
			break
		}
		anchor += next
		end, ok := balancedObjectAt(reply, anchor)
		if !ok {
			continue
		}
		raw := reply[anchor:end]
		var decoded struct {
			Verdict string   `json:"verdict"`
			Reasons []string `json:"reasons"`
		}
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			decodeErr = fmt.Errorf("decode the judge's verdict %s: %w", oneLine(raw), err)
			decodeRaw = raw
			continue
		}
		switch strings.ToLower(strings.TrimSpace(decoded.Verdict)) {
		case "pass":
			return Verdict{Pass: true, Reasons: decoded.Reasons, Raw: raw}, nil
		case "fail":
			return Verdict{Reasons: decoded.Reasons, Raw: raw}, nil
		default:
			wordErr = fmt.Errorf("the judge answered verdict %q, want \"pass\" or \"fail\"", decoded.Verdict)
			wordRaw = raw
		}
	}
	if wordErr != nil {
		return Verdict{Raw: wordRaw}, wordErr
	}
	if decodeErr != nil {
		return Verdict{Raw: decodeRaw}, decodeErr
	}
	return Verdict{Raw: reply}, fmt.Errorf("no JSON object in the judge's reply: %s", oneLine(reply))
}

// balancedObjectAt returns the exclusive end of the balanced {…} span that opens at start, so
// text[start:end] is the object, skipping braces inside strings so a reason mentioning "{" does
// not end the object early. It reports false when the span never closes. Every bit of scan state
// is local, so each candidate the walk tries starts clean — an unterminated quote in one
// candidate cannot swallow the next.
func balancedObjectAt(text string, start int) (end int, ok bool) {
	if start < 0 || start >= len(text) || text[start] != '{' {
		return 0, false
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(text); i++ {
		switch c := text[i]; {
		case escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}

// oneLine flattens text for a single-line failure message, elided so a whole frame cannot bury
// the line that matters.
func oneLine(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	const limit = 300
	if len(flat) > limit {
		return flat[:limit] + "…"
	}
	if flat == "" {
		return "(empty)"
	}
	return flat
}
