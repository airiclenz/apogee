package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/library"
	"github.com/airiclenz/apogee/internal/probe"
)

// modelUpstream is a fake OpenAI-compatible server that passes the whole capability battery. It
// branches on request shape exactly as a real server would — tool count, message count, whether
// logprobs were asked for — so the command under test drives the real provider client.
func modelUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"battery-model","context_length":4096}]}`))
			return
		}
		if r.URL.Path == "/props" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var body struct {
			Messages []json.RawMessage `json:"messages"`
			Tools    []json.RawMessage `json:"tools"`
			LogProbs *bool             `json:"logprobs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		switch {
		case body.LogProbs != nil && *body.LogProbs:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":" Paris"},"logprobs":{"content":` +
				`[{"token":" Paris","top_logprobs":[{"token":" Paris"},{"token":" the"}]}]},"finish_reason":"length"}]}`))
		case len(body.Tools) == 1:
			_, _ = w.Write([]byte(toolCallReply("call-1", "probe_echo", `{\"text\":\"apogee\"}`)))
		case len(body.Tools) == 2 && len(body.Messages) <= 2:
			_, _ = w.Write([]byte(toolCallReply("call-2", "probe_lookup", `{\"key\":\"alpha\"}`)))
		case len(body.Tools) == 2:
			_, _ = w.Write([]byte(toolCallReply("call-3", "probe_report", `{\"value\":\"omega-7\"}`)))
		default:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true,\"name\":\"apogee\"}"},"finish_reason":"stop"}]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func toolCallReply(id, name, args string) string {
	return `{"choices":[{"message":{"content":null,"tool_calls":[{"id":"` + id + `","type":"function",` +
		`"function":{"name":"` + name + `","arguments":"` + args + `"}}]},"finish_reason":"tool_calls"}]}`
}

// runProbeModel executes `apogee probe model` against a hermetic apogee home and returns
// everything it printed on both streams.
func runProbeModel(t *testing.T, configHome string, args ...string) string {
	t.Helper()
	cmd := newProbeCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"model", "--config", configHome, "--workspace", t.TempDir()}, args...))

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("probe model: %v\n%s", err, out.String())
	}
	return out.String()
}

// The battery runs, the report states what it observed, and the behavioral fingerprint is
// RECORDED — the write is the point of the command (ADR 0021 §3), and the record's path is
// printed so deleting it is a supported undo.
func TestProbeModelRunsTheBatteryAndRecords(t *testing.T) {
	t.Parallel()
	srv := modelUpstream(t)
	configHome := t.TempDir()

	report := runProbeModel(t, configHome, "--endpoint", srv.URL)

	for _, want := range []string{
		"apogee probe model: calling the model live",
		"apogee probe — model battery",
		"native-tool-call",
		"battery-model — unchanged; the probe raises its confidence, it does not rename it",
		"probe:1:tools+json+chain:lp-",
		"medium — a dated behavioral claim",
		"suggested model profile",
		"    tool-call-format: native",
		"yes — delete the file above to undo",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report does not state %q:\n%s", want, report)
		}
	}

	rec, warning, ok := library.LoadProbeRecord(library.ProbeDir(configHome), srv.URL, "battery-model")
	if !ok {
		t.Fatalf("no probe record was written (warning=%q)", warning)
	}
	if !strings.HasPrefix(rec.Behavior, "probe:1:tools+json+chain") {
		t.Errorf("recorded behavior = %q; want the observed signature", rec.Behavior)
	}
	if rec.ModelLabel != "battery-model" {
		t.Errorf("recorded label = %q; want the advertised label the identity is keyed on", rec.ModelLabel)
	}
	if rec.CapabilityTier != string(probe.TierFull) {
		t.Errorf("recorded tier = %q; want %q", rec.CapabilityTier, probe.TierFull)
	}
}

// Once the record exists, the identity ladder resolves the same model at MEDIUM confidence
// offline — which is the whole reason the probe persists. Print-only would leave
// ConfidenceMedium a tier nothing can produce.
func TestProbeModelRecordReachesTheResolver(t *testing.T) {
	t.Parallel()
	srv := modelUpstream(t)
	configHome := t.TempDir()

	_ = runProbeModel(t, configHome, "--endpoint", srv.URL)

	roots, err := resolveRoots(configHome, t.TempDir())
	if err != nil {
		t.Fatalf("resolve roots: %v", err)
	}
	fp := library.ResolveFingerprintFrom(library.Sources{
		ModelID:  "battery-model",
		Endpoint: srv.URL,
		ProbeDir: roots.probe,
	})
	if fp.Confidence.String() != "medium" {
		t.Fatalf("confidence = %s; want medium after the probe recorded a fingerprint", fp.Confidence)
	}
	if fp.Label != "battery-model" {
		t.Errorf("label = %q; want the advertised label unchanged — probing promotes the tier, "+
			"it must not re-key the model", fp.Label)
	}
}

// --no-save is a genuine off-switch, not a rollback: the full battery runs, the full report
// prints, and the apogee home is left exactly as it was found (ADR 0021 §4).
func TestProbeModelNoSaveWritesNothing(t *testing.T) {
	t.Parallel()
	srv := modelUpstream(t)
	configHome := t.TempDir()

	report := runProbeModel(t, configHome, "--endpoint", srv.URL, "--no-save")

	if !strings.Contains(report, "NO — --no-save was given") {
		t.Errorf("the report must say the record was not written:\n%s", report)
	}
	if !strings.Contains(report, "probe:1:tools+json+chain") {
		t.Errorf("--no-save still runs the full battery and prints the identity:\n%s", report)
	}
	entries, err := os.ReadDir(configHome)
	if err != nil || len(entries) != 0 {
		t.Errorf("--no-save wrote into the apogee home (entries=%v, err=%v)", entries, err)
	}
}

// A second probe that derives a DIFFERENT fingerprint under the same endpoint + advertised
// label says so, naming the earlier claim's date: a model swapped behind an unchanged label is
// detectable rather than silent (ADR 0021 §3).
func TestProbeModelReportsAChangedModelBehindTheLabel(t *testing.T) {
	t.Parallel()
	srv := modelUpstream(t)
	configHome := t.TempDir()
	dir := library.ProbeDir(configHome)

	if _, err := library.SaveProbeRecord(dir, library.ProbeRecord{
		Endpoint:   srv.URL,
		ModelLabel: "battery-model",
		ProbedAt:   mustTime(t, "2026-01-02T03:04:05Z"),
		Behavior:   "probe:1:tools",
	}); err != nil {
		t.Fatalf("seed previous record: %v", err)
	}

	report := runProbeModel(t, configHome, "--endpoint", srv.URL)
	if !strings.Contains(report, "the model behind this label changed since 2026-01-02T03:04:05Z") {
		t.Errorf("report does not flag the changed model:\n%s", report)
	}
}

// THE PROMOTION, end to end, in the direction ADR 0021 §4 promises: a model whose Validated set
// is merely OFFERED before the probe has that same set AUTO-APPLIED after it. This is the
// regression guard for the defect a behavioral RE-LABELLING would introduce — a probe that
// re-keys the model silently DEMOTES it instead, because the offered entry, the user's alias and
// the Library's observations are all filed under the label the probe just walked away from.
func TestProbeModelPromotesAnOfferedValidatedSet(t *testing.T) {
	t.Parallel()
	srv := modelUpstream(t)
	configHome := t.TempDir()
	roots, err := resolveRoots(configHome, t.TempDir())
	if err != nil {
		t.Fatalf("resolve roots: %v", err)
	}
	opts := baseOpts(gemmaKey)
	opts.endpoint = srv.URL

	before, offerNotices, err := resolveValidatedSet(opts, roots.validated, roots.probe)
	if err != nil {
		t.Fatalf("resolveValidatedSet before the probe: %v", err)
	}
	if before != nil {
		t.Fatalf("before the probe the set is offered, not applied; got %v", before)
	}
	if !noticeContains(offerNotices, "a Validated set exists for "+strconv.Quote(gemmaKey)) {
		t.Fatalf("before the probe the surface must OFFER the set; notices=%v", offerNotices)
	}

	report := runProbeModel(t, configHome, "--endpoint", srv.URL, "--model", gemmaKey)
	if !strings.Contains(report, "Validated set "+gemmaKey+" now AUTO-APPLIES") {
		t.Errorf("the report must name the promotion it just performed:\n%s", report)
	}

	after, applyNotices, err := resolveValidatedSet(opts, roots.validated, roots.probe)
	if err != nil {
		t.Fatalf("resolveValidatedSet after the probe: %v", err)
	}
	if len(after) == 0 {
		t.Fatalf("probing DEMOTED the model: the offered set no longer matches (notices=%v)", applyNotices)
	}
	if !noticeContains(applyNotices, "Validated set for "+gemmaKey+" applied") {
		t.Errorf("the applying notice must name the entry; notices=%v", applyNotices)
	}
}

// A user who already pasted the ADR 0016 §3 identity alias must not LOSE their applying set by
// running the probe: the alias keys on the same label the probe promotes, so the set keeps
// applying — and the report says the record promoted nothing rather than claiming a promotion
// that did not happen.
func TestProbeModelKeepsAnAliasedSetApplying(t *testing.T) {
	t.Parallel()
	srv := modelUpstream(t)
	configHome := t.TempDir()
	writeProbeConfig(t, configHome, "validated-sets:\n  alias:\n    "+gemmaKey+": "+gemmaKey+"\n")

	roots, err := resolveRoots(configHome, t.TempDir())
	if err != nil {
		t.Fatalf("resolve roots: %v", err)
	}
	opts := baseOpts(gemmaKey)
	opts.endpoint = srv.URL
	opts.validatedSetsAlias = map[string]string{gemmaKey: gemmaKey}

	before, _, err := resolveValidatedSet(opts, roots.validated, roots.probe)
	if err != nil || len(before) == 0 {
		t.Fatalf("the alias must already apply the set: set=%v err=%v", before, err)
	}

	report := runProbeModel(t, configHome, "--endpoint", srv.URL, "--model", gemmaKey)
	if !strings.Contains(report, "was already applying through your validated-sets alias") {
		t.Errorf("the report claimed a promotion that did not happen:\n%s", report)
	}

	after, _, err := resolveValidatedSet(opts, roots.validated, roots.probe)
	if err != nil {
		t.Fatalf("resolveValidatedSet after the probe: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("probing changed the aliased set from %d mechanisms to %d; it must change nothing",
			len(before), len(after))
	}
}

// An entry the live catalogue cannot assemble does not auto-apply at startup — resolveValidatedSet
// skips it whole — so `probe model` must not claim it will. The report names the skip instead,
// which is the only way this command's promise and the next session's behaviour can be the same
// answer about the same entry.
func TestProbeModelDoesNotClaimAnEntryStartupWillSkip(t *testing.T) {
	t.Parallel()
	srv := modelUpstream(t)
	configHome := t.TempDir()
	const key = "ghost-set-model"

	roots, err := resolveRoots(configHome, t.TempDir())
	if err != nil {
		t.Fatalf("resolve roots: %v", err)
	}
	writeUserValidatedEntry(t, roots.validated, key,
		`{"version":1,"key":"`+key+`","set":["ghost_mechanism"],"evidence":{"campaign":"c"}}`)

	report := runProbeModel(t, configHome, "--endpoint", srv.URL, "--model", key)

	if strings.Contains(report, "AUTO-APPLIES") {
		t.Errorf("the report claims an auto-apply startup will refuse:\n%s", report)
	}
	for _, want := range []string{
		"skips validated-set entry " + strconv.Quote(key),
		"ghost_mechanism",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the report does not name the invalid entry (%q missing):\n%s", want, report)
		}
	}

	// The claim itself, at the seam that makes it: nothing applied, nothing promoted, the skip named.
	opts := baseOpts(key)
	opts.endpoint = srv.URL
	keys, promoted, suppressed := autoApplyKeys(probe.Model{
		Endpoint:    srv.URL,
		Model:       key,
		Fingerprint: domain.ModelFingerprint{Label: key, Confidence: domain.ConfidenceMedium},
	}, opts, roots.validated)
	if len(keys) != 0 || promoted {
		t.Errorf("autoApplyKeys claimed keys=%v promoted=%v for an entry startup skips", keys, promoted)
	}
	if !strings.Contains(suppressed, "ghost_mechanism") {
		t.Errorf("suppressed = %q; want the catalogue defect named", suppressed)
	}
}

// The startup half of the same promise: with a probe record stored for this endpoint + label, the
// identity resolves at medium confidence and the shipped set that is merely OFFERED without one
// APPLIES — the rung `probe model` sells, proven at the startup path that has to deliver it.
func TestResolveValidatedSetAppliesOnAStoredProbeRecord(t *testing.T) {
	t.Parallel()
	const endpoint = "http://127.0.0.1:65535"
	configHome := t.TempDir()
	roots, err := resolveRoots(configHome, t.TempDir())
	if err != nil {
		t.Fatalf("resolve roots: %v", err)
	}
	opts := baseOpts(gemmaKey)
	opts.endpoint = endpoint

	if _, err := library.SaveProbeRecord(roots.probe, library.ProbeRecord{
		Endpoint:   endpoint,
		ModelLabel: gemmaKey,
		ProbedAt:   mustTime(t, "2026-07-22T10:00:00Z"),
		Behavior:   "probe:1:tools+json+chain",
	}); err != nil {
		t.Fatalf("save probe record: %v", err)
	}

	set, notices, err := resolveValidatedSet(opts, roots.validated, roots.probe)
	if err != nil {
		t.Fatalf("resolveValidatedSet: %v", err)
	}
	if len(set) == 0 {
		t.Fatalf("the stored record must promote the offered set to applied; notices=%v", notices)
	}
	if !noticeContains(notices, "Validated set for "+gemmaKey+" applied") {
		t.Errorf("want the applying notice, got %v", notices)
	}
	if noticeContains(notices, "To apply it") {
		t.Errorf("the offer notice must be gone once a record exists: %v", notices)
	}
}

// writeUserValidatedEntry seeds one user-local Validated-set entry in a hermetic apogee home.
func writeUserValidatedEntry(t *testing.T, validatedDir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(validatedDir, 0o700); err != nil {
		t.Fatalf("mkdir validated dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(validatedDir, name+".json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write validated entry: %v", err)
	}
}

// writeProbeConfig seeds a config.yaml in a hermetic apogee home so the command under test
// resolves the same options a real session would.
func writeProbeConfig(t *testing.T, configHome, body string) {
	t.Helper()
	if err := os.MkdirAll(configHome, 0o700); err != nil {
		t.Fatalf("mkdir config home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configHome, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
}

// noticeContains reports whether any per-session notice carries want.
func noticeContains(notices []string, want string) bool {
	for _, n := range notices {
		if strings.Contains(n, want) {
			return true
		}
	}
	return false
}

// The model half never runs off an absent endpoint: with nothing to call there is no battery,
// and the refusal points at the free host report that needs none.
func TestProbeModelRefusesWithoutAnEndpoint(t *testing.T) {
	t.Parallel()
	cmd := newProbeCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"model", "--config", t.TempDir(), "--workspace", t.TempDir()})

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("probe model with no endpoint should fail:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "no endpoint configured") {
		t.Errorf("error = %v; want the endpoint refusal", err)
	}
}

// The battery is never entered against a server that cannot name the model to probe. Both
// pre-spend gates refuse BEFORE the first /chat/completions call, for the same reason ADR 0021 §4
// states the costs up front: a probe that spends tokens and then reports that it could not tell
// what it probed has already charged for the answer it failed to give. Nothing is recorded
// either — an identity may not be minted from a discovery that did not happen.
func TestProbeModelRefusesBeforeSpendingTokens(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{
			// An empty model list is a discovery FAILURE by the provider's own contract
			// (Discover rejects a server that advertises nothing), so the refusal that fires
			// is discovery's — one rung above errProbeModelNeedsLabel. Pinning the wording is
			// what makes this row catch a mutation of the `derr != nil` branch: drop that
			// branch and the run falls through to the label gate's different sentence.
			name:    "the server advertises no model",
			status:  http.StatusOK,
			body:    `{"data":[]}`,
			wantErr: "server returned no models",
		},
		{
			name:    "discovery itself fails",
			status:  http.StatusInternalServerError,
			body:    `{"error":"boom"}`,
			wantErr: "upstream HTTP 500",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var chatCalls int
			srv := discoveryUpstream(t, tc.status, tc.body, &chatCalls)
			configHome := t.TempDir()

			err := probeModelRefusal(t, configHome, "--endpoint", srv.URL)

			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v; want the refusal to name %q", err, tc.wantErr)
			}
			if chatCalls != 0 {
				t.Errorf("the refusal spent %d battery call(s); the gate must land before the first one", chatCalls)
			}
			if entries, readErr := os.ReadDir(configHome); readErr != nil || len(entries) != 0 {
				t.Errorf("a refused probe wrote into the apogee home (entries=%v, err=%v)", entries, readErr)
			}
		})
	}
}

// discoveryUpstream is a fake Upstream whose /v1/models answers with status and body, and which
// counts every battery call it is asked for — that counter is what turns "the command failed"
// into "the command failed before spending anything".
func discoveryUpstream(t *testing.T, status int, body string, chatCalls *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		case "/v1/chat/completions":
			*chatCalls++
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// probeModelRefusal runs `apogee probe model` against a hermetic apogee home EXPECTING a refusal
// and returns it. A nil error is the failure: it means the run continued past the gate under test.
func probeModelRefusal(t *testing.T, configHome string, args ...string) error {
	t.Helper()
	cmd := newProbeCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"model", "--config", configHome, "--workspace", t.TempDir()}, args...))

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("probe model succeeded; the refusal gate did not fire:\n%s", out.String())
	}
	return err
}

// `apogee probe` (the free half) never runs the battery, even with a perfectly reachable
// endpoint sitting in the config: the model half is an explicit act, never a side effect of a
// port answering (ADR 0021 §1).
func TestBareProbeNeverRunsTheBattery(t *testing.T) {
	t.Parallel()
	var chatCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"battery-model"}]}`))
		case "/v1/chat/completions":
			chatCalls++
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	configHome := t.TempDir()
	_ = runProbe(t, newProbeCommand(), configHome, t.TempDir(), "--endpoint", srv.URL)

	if chatCalls != 0 {
		t.Errorf("bare `apogee probe` made %d chat call(s); the host half must call no model", chatCalls)
	}
	if entries, err := os.ReadDir(configHome); err != nil || len(entries) != 0 {
		t.Errorf("bare `apogee probe` wrote into the apogee home (entries=%v, err=%v)", entries, err)
	}
}

// The record lands in its own subdirectory of the apogee home, beside library/ and sessions/,
// as one deletable file per probed model.
func TestProbeRecordLivesUnderTheApogeeHome(t *testing.T) {
	t.Parallel()
	roots, err := resolveRoots(filepath.Join(t.TempDir(), "home"), t.TempDir())
	if err != nil {
		t.Fatalf("resolve roots: %v", err)
	}
	if got, want := filepath.Base(roots.probe), "probe"; got != want {
		t.Errorf("probe root = %q; want the %q subdirectory of the apogee home", roots.probe, want)
	}
	if filepath.Dir(roots.probe) != roots.config {
		t.Errorf("probe root %q is not under the apogee home %q", roots.probe, roots.config)
	}
}

// The live smoke: the whole battery against a REAL model, which is the only thing that can tell
// us the probes actually elicit what they claim to — a scripted server proves the plumbing, not
// the prompts. It is opt-in on APOGEE_LIVE_ENDPOINT exactly like internal/tui's live tests, so
// the default suite stays offline and deterministic:
//
//	APOGEE_LIVE_ENDPOINT=http://127.0.0.1:1111 go test -count=1 -run TestProbeModelLiveSmoke -v ./cmd/apogee/
//
// APOGEE_LIVE_MODEL pins the model; left empty, the battery probes whatever the server
// advertises as active. It runs with --no-save, so an exploratory live run never silently
// switches Validated-set automatism on for the owner's own machine (ADR 0021 §4).
func TestProbeModelLiveSmoke(t *testing.T) {
	endpoint := os.Getenv("APOGEE_LIVE_ENDPOINT")
	if endpoint == "" {
		t.Skip("set APOGEE_LIVE_ENDPOINT (and optionally APOGEE_LIVE_MODEL) to run the live model battery")
	}

	args := []string{"--endpoint", endpoint, "--no-save"}
	if model := os.Getenv("APOGEE_LIVE_MODEL"); model != "" {
		args = append(args, "--model", model)
	}

	report := runProbeModel(t, t.TempDir(), args...)
	t.Log("\n" + report)
	if !strings.Contains(report, "apogee probe — model battery") {
		t.Fatalf("live probe produced no report:\n%s", report)
	}
	if strings.Contains(report, "the battery did not complete") {
		t.Errorf("the live battery did not complete against %s:\n%s", endpoint, report)
	}
}

// mustTime parses an RFC3339 timestamp for a seeded record.
func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return parsed
}
