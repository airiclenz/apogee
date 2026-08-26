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
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/library"
	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/sanitize"
)

// modelUpstream is a fake OpenAI-compatible server that passes the whole capability battery. It
// branches on request shape exactly as a real server would — tool count, message count, whether
// logprobs were asked for — so the command under test drives the real provider client.
func modelUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return modelUpstreamAdvertising(t, "battery-model")
}

// modelUpstreamAdvertising is modelUpstream with the advertised active model under the caller's
// control — the fixture for the unpinned-model flow, where the label the probe keys its record
// on is whatever /v1/models names.
func modelUpstreamAdvertising(t *testing.T, active string) *httptest.Server {
	t.Helper()
	return modelUpstreamRecording(t, active, nil)
}

// modelUpstreamRecording is modelUpstreamAdvertising with an Authorization recorder: every
// request's header lands in auth (nil ⇒ record nothing). That is what proves the resolved api
// key reaches BOTH clients this command builds — the label discovery and the battery — since
// `probe model` has no --api-key flag to inspect and the two clients are otherwise invisible
// from outside.
func modelUpstreamRecording(t *testing.T, active string, auth *authLog) *httptest.Server {
	t.Helper()
	// The advertised id is JSON-ENCODED rather than pasted between quotes: the escape-strip
	// guard below advertises an id carrying ESC and BEL, and a literal control character in a
	// JSON string is a syntax error — the client would fail discovery instead of carrying the
	// hostile id through to the report, and the guard would prove nothing.
	activeJSON, err := json.Marshal(active)
	if err != nil {
		t.Fatalf("encode advertised model id %q: %v", active, err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.record(r)
		if r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":` + string(activeJSON) + `,"context_length":4096}]}`))
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

// authLog collects the Authorization header of every request a fake Upstream received, keyed by
// the path that received it. The httptest handler runs on the server's own goroutines, so it is
// guarded; the assertions read it once the command under test has returned. A nil *authLog
// records nothing, which is what every fixture that does not care about auth passes.
type authLog struct {
	mu      sync.Mutex
	entries []authEntry
}

type authEntry struct {
	path   string
	header string
}

func (a *authLog) record(r *http.Request) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, authEntry{path: r.URL.Path, header: r.Header.Get("Authorization")})
}

func (a *authLog) all() []authEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]authEntry(nil), a.entries...)
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
	cmd.SetArgs(append([]string{"model", "--config", configHome}, args...))

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
	configHome := upstreamHome(t, srv.URL)

	report := runProbeModel(t, configHome)

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

// The stream split as a SHELL sees it: `apogee probe model > battery.txt` must leave the report
// in the file and the spend/write preamble in the terminal, where a warning belongs.
//
// The same guard the host half carries, for the same reason: Cobra's cmd.Println resolves to
// OutOrStderr, so printing the product with it puts the whole report on stderr in every real
// invocation and only a test that has called SetOut sees otherwise. No out writer is wired
// here, so this exercises the fallback every real run takes.
func TestProbeModelReportLandsOnTheProcessStdout(t *testing.T) {
	srv := modelUpstream(t)
	configHome := upstreamHome(t, srv.URL)

	var runErr error
	stdout, stderr := captureProcessStreams(t, func() {
		cmd := newProbeCommand()
		// Deliberately no SetOut: the fallback under test is the one every real run takes.
		cmd.SetArgs([]string{"model", "--config", configHome})
		runErr = cmd.ExecuteContext(context.Background())
	})
	if runErr != nil {
		t.Fatalf("probe model: %v (stderr: %q)", runErr, stderr)
	}

	if !strings.Contains(stdout, "apogee probe — model battery") {
		t.Errorf("process stdout = %q; want the battery report", stdout)
	}
	if strings.Contains(stderr, "apogee probe — model battery") {
		t.Errorf("the report reached process stderr; a redirect of stdout would lose it: %q", stderr)
	}
	if !strings.Contains(stderr, "apogee probe model: calling the model live") {
		t.Errorf("the preamble is not on process stderr: %q", stderr)
	}
	if strings.Contains(stdout, "calling the model live") {
		t.Errorf("the preamble reached process stdout, where it would contaminate the report: %q", stdout)
	}
}

// The report is a diagnostic ABOUT a server the operator has reason to distrust, so it may not
// hand that server the terminal: the id it advertises reaches three places in the report (the
// upstream block, the fingerprint label and the suggested profile YAML), and printed raw an OSC 8
// introducer would make the whole diagnosis one forged hyperlink (ADR 0019 rung 0) while a bidi
// override would reorder the very line naming what was probed. The sink strips
// (internal/sanitize); the id's printable text still identifies the model.
func TestProbeModelReportStripsTerminalEscapes(t *testing.T) {
	t.Parallel()
	srv := modelUpstreamAdvertising(t, "\x1b]8;;mailto:evil\aqwen-\u202e3")
	// --no-save keeps the run to the sink under test: nothing is written, so the advertised id
	// never reaches a filename, and the fingerprint label still carries it into the report.
	report := runProbeModel(t, upstreamHome(t, srv.URL), "--no-save")

	if !strings.Contains(report, "qwen-") {
		t.Errorf("the report dropped the advertised id's printable text:\n%q", report)
	}
	assertNoTerminalControls(t, "probe model report", report)
}

// assertNoTerminalControls fails when s carries anything the render seams strip: a C0 control
// other than newline or tab, DEL, or one of the eleven bidi overrides. ESC and BEL are named
// separately because they are the two that carry a payload — an OSC 8 hyperlink needs both.
func assertNoTerminalControls(t *testing.T, what, s string) {
	t.Helper()
	for _, c := range []struct {
		name string
		r    rune
	}{{"ESC", 0x1b}, {"BEL", 0x07}} {
		if strings.ContainsRune(s, c.r) {
			t.Errorf("%s carries %s — an OSC 8 hyperlink needs both", what, c.name)
		}
	}
	// One dump on the first survivor: every subsequent one would print the same report again.
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
		case r < 0x20 || r == 0x7f || sanitize.BidiControl(r):
			t.Fatalf("%s carries the terminal control %U:\n%q", what, r, s)
		}
	}
}

// Once the record exists, the identity ladder resolves the same model at MEDIUM confidence
// offline — which is the whole reason the probe persists. Print-only would leave
// ConfidenceMedium a tier nothing can produce.
func TestProbeModelRecordReachesTheResolver(t *testing.T) {
	t.Parallel()
	srv := modelUpstream(t)
	configHome := upstreamHome(t, srv.URL)

	_ = runProbeModel(t, configHome)

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
	configHome := upstreamHome(t, srv.URL)

	report := runProbeModel(t, configHome, "--no-save")

	if !strings.Contains(report, "NO — --no-save was given") {
		t.Errorf("the report must say the record was not written:\n%s", report)
	}
	if !strings.Contains(report, "probe:1:tools+json+chain") {
		t.Errorf("--no-save still runs the full battery and prints the identity:\n%s", report)
	}
	assertHomeHoldsOnlyConfig(t, configHome, "--no-save")
}

// --no-save with a record an earlier run already stored: the effect line must not deny the
// surviving record — it stays on disk untouched and keeps resolving this model at medium
// confidence, which is exactly the drift-check scenario --no-save serves. "With no record
// stored, identity stays at the label tier" would be a false claim about this machine.
func TestProbeModelNoSaveNamesTheSurvivingRecord(t *testing.T) {
	t.Parallel()
	srv := modelUpstream(t)
	configHome := upstreamHome(t, srv.URL)
	dir := library.ProbeDir(configHome)
	// Seeded in a zone that is NOT local's, so the date the effect line prints proves the display
	// converts to the reader's own clock on any machine's TZ (the stored stamp is unaffected —
	// asserted below by Equal, which compares instants rather than spellings).
	local := time.Date(2026, 1, 2, 23, 0, 0, 0, time.Local)
	probedAt := awayFromLocal(t, local)

	if _, err := library.SaveProbeRecord(dir, library.ProbeRecord{
		Endpoint:   srv.URL,
		ModelLabel: "battery-model",
		ProbedAt:   probedAt,
		Behavior:   "probe:1:tools+json+chain",
	}); err != nil {
		t.Fatalf("seed previous record: %v", err)
	}

	report := runProbeModel(t, configHome, "--no-save")

	if !strings.Contains(report,
		"none new — the record from "+local.Format(time.RFC3339)+" continues to apply; this run recorded nothing") {
		t.Errorf("the effect line must name the surviving record in local time:\n%s", report)
	}
	if foreign := probedAt.Format(time.RFC3339); strings.Contains(report, foreign) {
		t.Errorf("the effect line carries the stored zone's spelling %q:\n%s", foreign, report)
	}
	if strings.Contains(report, "identity stays at the label tier") {
		t.Errorf("the report denies a record that is still on disk:\n%s", report)
	}
	rec, warning, ok := library.LoadProbeRecord(dir, srv.URL, "battery-model")
	if !ok || !rec.ProbedAt.Equal(probedAt) {
		t.Errorf("--no-save must leave the stored record untouched (ok=%v warning=%q rec=%+v)", ok, warning, rec)
	}
}

// The command's Long text promises records from an earlier build "are skipped with a warning" —
// this is that warning, surfaced on stderr (the report's own stream stays clean) while the run
// still renders the full report and records a fresh claim in the current format.
func TestProbeModelWarnsAboutAnOldFormatRecord(t *testing.T) {
	t.Parallel()
	srv := modelUpstream(t)
	configHome := upstreamHome(t, srv.URL)
	dir := library.ProbeDir(configHome)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir probe dir: %v", err)
	}
	v1 := `{"version":1,"battery-version":1,"endpoint":"` + srv.URL +
		`","model-label":"battery-model","probed-at":"2026-01-02T03:04:05Z"}`
	if err := os.WriteFile(library.ProbeRecordPath(dir, srv.URL, "battery-model"), []byte(v1), 0o600); err != nil {
		t.Fatalf("write v1 record: %v", err)
	}

	cmd := newProbeCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"model", "--config", configHome})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("probe model: %v\n%s%s", err, out.String(), errOut.String())
	}

	for _, want := range []string{
		"skipping probe record",
		"schema version 1",
		"re-run `apogee probe model`",
	} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr does not carry the old-format warning (%q missing):\n%s", want, errOut.String())
		}
	}
	if !strings.Contains(out.String(), "apogee probe — model battery") {
		t.Errorf("the report must still render after the warning:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "yes — delete the file above to undo") {
		t.Errorf("the fresh record must still be written:\n%s", out.String())
	}
}

// --workspace is gone from `probe model`: the model path never read roots.workspace (only
// `probe host` reports it), and the probe commands' own flag rule admits only flags that CHANGE
// what is reported — an inert flag trains users to expect an effect it never had.
func TestProbeModelRejectsTheWorkspaceFlag(t *testing.T) {
	t.Parallel()
	cmd := newProbeCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"model", "--config", t.TempDir(), "--workspace", "x"})

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("probe model accepted --workspace:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "unknown flag: --workspace") {
		t.Errorf("error = %v; want the unknown-flag refusal", err)
	}

	help := newProbeCommand()
	var helpOut bytes.Buffer
	help.SetOut(&helpOut)
	help.SetErr(&helpOut)
	help.SetArgs([]string{"model", "--help"})
	if err := help.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("probe model --help: %v", err)
	}
	if strings.Contains(helpOut.String(), "--workspace") {
		t.Errorf("--help still lists --workspace:\n%s", helpOut.String())
	}
}

// A second probe that derives a DIFFERENT fingerprint under the same endpoint + advertised
// label says so, naming the earlier claim's date: a model swapped behind an unchanged label is
// detectable rather than silent (ADR 0021 §3).
func TestProbeModelReportsAChangedModelBehindTheLabel(t *testing.T) {
	t.Parallel()
	srv := modelUpstream(t)
	configHome := upstreamHome(t, srv.URL)
	dir := library.ProbeDir(configHome)
	// As in the surviving-record test: a stored zone that is not local's, so "changed since <date>"
	// is proved to reach the reader in the reader's own clock whatever the machine's TZ.
	local := time.Date(2026, 1, 2, 23, 0, 0, 0, time.Local)
	probedAt := awayFromLocal(t, local)

	if _, err := library.SaveProbeRecord(dir, library.ProbeRecord{
		Endpoint:   srv.URL,
		ModelLabel: "battery-model",
		ProbedAt:   probedAt,
		Behavior:   "probe:1:tools",
	}); err != nil {
		t.Fatalf("seed previous record: %v", err)
	}

	report := runProbeModel(t, configHome)
	if !strings.Contains(report, "the model behind this label changed since "+local.Format(time.RFC3339)) {
		t.Errorf("report does not flag the changed model in local time:\n%s", report)
	}
	if foreign := probedAt.Format(time.RFC3339); strings.Contains(report, foreign) {
		t.Errorf("the changed line carries the stored zone's spelling %q:\n%s", foreign, report)
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
	configHome := upstreamHome(t, srv.URL, gemmaKey)
	roots, err := resolveRoots(configHome, t.TempDir())
	if err != nil {
		t.Fatalf("resolve roots: %v", err)
	}
	opts := baseOpts(gemmaKey)
	opts.Endpoint = srv.URL

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

	report := runProbeModel(t, configHome)
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
	writeProbeConfig(t, configHome, config.ServerEntry{Name: "probe-target", Endpoint: srv.URL, Model: gemmaKey},
		"validated-sets:\n  alias:\n    "+gemmaKey+": "+gemmaKey+"\n")

	roots, err := resolveRoots(configHome, t.TempDir())
	if err != nil {
		t.Fatalf("resolve roots: %v", err)
	}
	opts := baseOpts(gemmaKey)
	opts.Endpoint = srv.URL
	opts.ValidatedSetsAlias = map[string]string{gemmaKey: gemmaKey}

	before, _, err := resolveValidatedSet(opts, roots.validated, roots.probe)
	if err != nil || len(before) == 0 {
		t.Fatalf("the alias must already apply the set: set=%v err=%v", before, err)
	}

	report := runProbeModel(t, configHome)
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
	const key = "ghost-set-model"
	configHome := upstreamHome(t, srv.URL, key)

	roots, err := resolveRoots(configHome, t.TempDir())
	if err != nil {
		t.Fatalf("resolve roots: %v", err)
	}
	writeUserValidatedEntry(t, roots.validated, key,
		`{"version":1,"key":"`+key+`","set":["ghost_mechanism"],"evidence":{"campaign":"c"}}`)

	report := runProbeModel(t, configHome)

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

	// The claim itself, at the seam that makes it: nothing applied, nothing promoted, the skip
	// named. The probe run above left its record in roots.probe, so the seam resolves the same
	// medium-confidence identity the next session start will.
	opts := baseOpts(key)
	opts.Endpoint = srv.URL
	keys, promoted, suppressed := autoApplyKeys(probe.Model{
		Endpoint:    srv.URL,
		Model:       key,
		Fingerprint: domain.ModelFingerprint{Label: key, Confidence: domain.ConfidenceMedium},
	}, opts, roots.validated, roots.probe)
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
	opts.Endpoint = endpoint

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

// With no `model` hint pinned anywhere, the probe discovers the active model and records a
// fingerprint for it — but the next session start resolves identity from the `model` hint on the
// `servers:` entry it starts on, which is empty, so startup's ladder reaches nothing and NO set
// applies. The report must say
// that (and name the pin that would change it) instead of promising an auto-apply the reader's
// machine never delivers (ADR 0021 §4 — the same defect class as the catalogue-skip parity).
func TestProbeModelSuppressesTheClaimWhenNoModelIsPinned(t *testing.T) {
	t.Parallel()
	srv := modelUpstreamAdvertising(t, gemmaKey)
	configHome := upstreamHome(t, srv.URL)

	report := runProbeModel(t, configHome)

	if strings.Contains(report, "AUTO-APPLIES") {
		t.Errorf("the report claims a promotion an unpinned startup cannot resolve:\n%s", report)
	}
	for _, want := range []string{
		"no `model` is pinned on this session's `servers:` entry",
		"pin `model: " + gemmaKey + "`",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the suppressed line must name the missing pin (%q missing):\n%s", want, report)
		}
	}
	// The record itself IS written — the suppression is about startup's identity resolution,
	// not about the save, and adding the entry's `model` hint later makes the record take effect
	// as stored.
	if _, _, ok := library.LoadProbeRecord(library.ProbeDir(configHome), srv.URL, gemmaKey); !ok {
		t.Errorf("the record must still be written; only the effect claim is suppressed")
	}
}

// A --model naming a reachable weight file resolves at the weights tier (high) on this machine:
// startup hashes the file and never reads the behavioral record below that rung, so the report
// must call the record inert here rather than claim a Medium promotion the ladder out-ranks.
func TestProbeModelSuppressesTheClaimForAWeightsFileModel(t *testing.T) {
	t.Parallel()
	srv := modelUpstream(t)
	weights := filepath.Join(t.TempDir(), "local-model.gguf")
	if err := os.WriteFile(weights, []byte("fake weight bytes"), 0o600); err != nil {
		t.Fatalf("write weight file: %v", err)
	}
	configHome := upstreamHome(t, srv.URL, weights)

	report := runProbeModel(t, configHome)

	if strings.Contains(report, "AUTO-APPL") {
		t.Errorf("the report claims an effect the weights tier out-ranks:\n%s", report)
	}
	if !strings.Contains(report, "identity resolves at the weights tier on this machine") {
		t.Errorf("the suppressed line must name the weights tier:\n%s", report)
	}
}

// Every session-level off-switch startup honours must be named by the probe's claim rather than
// silently ignored. Each row seeds a record that would otherwise auto-apply the shipped gemma
// set, so deleting the branch under test makes the row see a claimed apply and fail.
func TestAutoApplyKeysNamesEverySessionOffSwitch(t *testing.T) {
	t.Parallel()
	const endpoint = "http://127.0.0.1:65535"

	cases := []struct {
		name   string
		mutate func(*config.Options)
		want   string
	}{
		{
			name:   "bypass",
			mutate: func(o *config.Options) { o.Bypass = true },
			want:   "Bypass suppresses the Validated-set surface",
		},
		{
			name:   "validated-sets disabled",
			mutate: func(o *config.Options) { o.ValidatedSetsEnable = false },
			want:   "`validated-sets: enable: false` turns the surface off",
		},
		{
			name:   "explicit mechanisms block",
			mutate: func(o *config.Options) { o.Mechanisms = map[string]bool{"validate": true} },
			want:   "explicit mechanisms: config takes precedence",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			probeDir := t.TempDir()
			if _, err := library.SaveProbeRecord(probeDir, library.ProbeRecord{
				Endpoint:   endpoint,
				ModelLabel: gemmaKey,
				ProbedAt:   mustTime(t, "2026-07-22T10:00:00Z"),
				Behavior:   "probe:1:tools+json+chain",
			}); err != nil {
				t.Fatalf("save probe record: %v", err)
			}
			opts := baseOpts(gemmaKey)
			opts.Endpoint = endpoint
			tc.mutate(&opts)

			keys, promoted, suppressed := autoApplyKeys(probe.Model{Endpoint: endpoint, Model: gemmaKey},
				opts, t.TempDir(), probeDir)
			if keys != nil || promoted {
				t.Errorf("keys=%v promoted=%v; an off-switch must claim nothing", keys, promoted)
			}
			if !strings.Contains(suppressed, tc.want) {
				t.Errorf("suppressed = %q; want it to name %q", suppressed, tc.want)
			}
		})
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
// resolves the same options a real session would: the caller's own keys, plus the one configured
// server the probe talks to (ADR 0036 — a config that names no server has nothing to probe).
func writeProbeConfig(t *testing.T, configHome string, upstream config.ServerEntry, body string) {
	t.Helper()
	if err := os.MkdirAll(configHome, 0o700); err != nil {
		t.Fatalf("mkdir config home: %v", err)
	}
	entry := "servers:\n  - name: " + upstream.Name + "\n    endpoint: " + upstream.Endpoint + "\n"
	if upstream.APIKey != "" {
		entry += "    api-key: " + upstream.APIKey + "\n"
	}
	if upstream.Model != "" {
		entry += "    model: " + upstream.Model + "\n"
	}
	entry += "server: " + upstream.Name + "\n"
	if err := os.WriteFile(filepath.Join(configHome, "config.yaml"), []byte(body+entry), 0o600); err != nil {
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

// Both clients `probe model` builds carry the key the config layer resolved: the label
// discovery that names the model, and the battery that spends the tokens. Wiring only one of
// them would leave the command failing against a keyed server for a reason the report cannot
// explain. There is no --api-key flag by design, so the key arrives through config.yaml here —
// the file layer, which is the same path a real user's keyed server takes.
func TestProbeModelSendsTheConfiguredAPIKey(t *testing.T) {
	t.Parallel()
	auth := &authLog{}
	srv := modelUpstreamRecording(t, "battery-model", auth)
	configHome := t.TempDir()
	writeProbeConfig(t, configHome, config.ServerEntry{Name: "probe-target", Endpoint: srv.URL, APIKey: "probe-token"}, "")

	// No --model, so the label discovery client runs too — the request that would 401 first
	// on a keyed server.
	_ = runProbeModel(t, configHome)

	entries := auth.all()
	var sawDiscovery, sawBattery bool
	for _, e := range entries {
		if e.header != "Bearer probe-token" {
			t.Errorf("request to %s carried Authorization %q, want %q", e.path, e.header, "Bearer probe-token")
		}
		switch e.path {
		case "/v1/models":
			sawDiscovery = true
		case "/v1/chat/completions":
			sawBattery = true
		}
	}
	if !sawDiscovery {
		t.Errorf("no discovery request was recorded (entries=%v)", entries)
	}
	if !sawBattery {
		t.Errorf("no battery request was recorded (entries=%v)", entries)
	}
}

// The keyless local server — the default — is unchanged: with no key configured, neither client
// sends an Authorization header at all.
func TestProbeModelWithoutAnAPIKeySendsNoAuthHeader(t *testing.T) {
	t.Parallel()
	auth := &authLog{}
	srv := modelUpstreamRecording(t, "battery-model", auth)

	_ = runProbeModel(t, upstreamHome(t, srv.URL))

	for _, e := range auth.all() {
		if e.header != "" {
			t.Errorf("request to %s carried Authorization %q with no key configured", e.path, e.header)
		}
	}
}

// The model half never runs off a config that names no server: with nothing to call there is no
// battery, and the refusal says what to write (ADR 0036 — a non-interactive driver has nobody to
// ask, so it gets the hard error rather than a picker).
func TestProbeModelRefusesWithoutAServer(t *testing.T) {
	t.Parallel()
	cmd := newProbeCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"model", "--config", t.TempDir()})

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("probe model with no server should fail:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "no servers are configured") {
		t.Errorf("error = %v; want the selection refusal", err)
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
			configHome := upstreamHome(t, srv.URL)

			err := probeModelRefusal(t, configHome)

			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v; want the refusal to name %q", err, tc.wantErr)
			}
			if chatCalls != 0 {
				t.Errorf("the refusal spent %d battery call(s); the gate must land before the first one", chatCalls)
			}
			assertHomeHoldsOnlyConfig(t, configHome, "a refused probe")
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
	cmd.SetArgs(append([]string{"model", "--config", configHome}, args...))

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

	configHome := upstreamHome(t, srv.URL)
	_ = runProbe(t, newProbeCommand(), configHome, t.TempDir())

	if chatCalls != 0 {
		t.Errorf("bare `apogee probe` made %d chat call(s); the host half must call no model", chatCalls)
	}
	assertHomeHoldsOnlyConfig(t, configHome, "bare `apogee probe`")
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

// awayFromLocal expresses a local wall clock in a zone 90 minutes off local's own offset. A record
// seeded with the result round-trips through the store carrying THAT offset, so a test can tell the
// local spelling of the instant from the stored one on any machine's TZ — including a machine whose
// TZ is UTC, where a UTC fixture would make the two indistinguishable and the assertion vacuous.
func awayFromLocal(t *testing.T, local time.Time) time.Time {
	t.Helper()
	_, offset := local.Zone()
	away := local.In(time.FixedZone("away", offset+90*60))
	if away.Format(time.RFC3339) == local.Format(time.RFC3339) {
		t.Fatalf("the fixture no longer distinguishes the zones: away %s, local %s", away, local)
	}
	return away
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
