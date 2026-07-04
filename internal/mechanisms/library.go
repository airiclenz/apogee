package mechanisms

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/library"
)

// errLibraryStoreRequired is the construction error newLibrary returns when the Library store was
// not injected (Deps.Library nil). A library Mechanism with no store cannot observe or inject, so it
// fails loudly at build rather than registering a silently-inert Mechanism.
var errLibraryStoreRequired = errors.New("apogee: library mechanism requires a Library store (Deps.Library)")

// library registers the cross-session learning Mechanism in the catalogue constructor table
// (Phase-4 item 14, the Library's two loop-facing halves). Default-off (D1) — the config surface
// builds it only when the `mechanisms:` block enables it, and it needs the Library store +
// resolved fingerprint injected at construction (D3, cmd/apogee/wire.go). It is ported from
// apogee-sim internal/library/{observer,transform}.go @pin.
//
// The catalogue lists a SINGLE `library` row (Table A) whose hook point is "pre-request (inject);
// observer half's hook point decided in item 14". That one row is realized here as ONE Mechanism
// implementing BOTH hooks — an inject side (PreRequest, AppendToSystem with a marker) and an
// observe side (PostResponse). Splitting it into two Mechanisms would need a second catalogue ID
// the ratified map does not list (D7 forbids inventing one), so the single `library` Mechanism
// carries both halves. Both are proactive-nudge (not off-ramp), so item 2's dispatch gate skips
// BOTH under Bypass — the Library is fully inert (no inject AND no observe/write, decision 13).
func init() { catalogue[libraryID] = newLibrary }

const libraryID domain.MechanismID = "library"

// libraryMinInjectConfidence is the confidence gate the item mandates ("prefer not to inject under
// uncertainty": low-confidence fingerprints don't inject). ConfidenceLow is a bare metadata label —
// two distinct builds can advertise the same label, so an observation keyed there is weak evidence.
// Requiring at least ConfidenceMedium means only a weights-hash (high) — or a Phase-5 behavioral
// probe (medium, D8) once it exists — ever injects; a metadata-label-only setup observes but does
// not inject. Observe is NOT confidence-gated (the item gates injection only): the store still learns
// on any identified model.
const libraryMinInjectConfidence = domain.ConfidenceMedium

// libraryInjectionMarker is the idempotency marker the injected system-prompt block leads with
// (apogee-sim transform.go InjectionMarker @pin). It is embedded in the block, so AppendToSystem's
// marker check makes a second inject on the same request a no-op.
const libraryInjectionMarker = "[Apogee context notes"

// libraryInjectionBudgetTokens caps the injected notes (apogee-sim store defaultInjectionBudgetTokens
// @pin). Item 13 deferred the injection-budget token cap to this Mechanism (its Query gates only on
// Bayesian score + observation count), so the cap lives here.
const libraryInjectionBudgetTokens = 200

// libraryDefaultCharsPerToken is the fallback chars→token ratio when the Budget has not yet
// calibrated one (apogee-sim queryOptions default @pin), used only for the injection-budget estimate.
const libraryDefaultCharsPerToken = 3.0

// libraryContextFullFraction is the window-fill above which injection backs off (apogee-sim
// transform.go @pin: skip when usage > 0.85) — a nearly-full window has no room for notes.
const libraryContextFullFraction = 0.85

// libraryAnalysisOnlyTags marks entries applicable only to analysis-intent requests (apogee-sim
// store analysisOnlyTags @pin). Item 13 deferred this intent filter to the inject Mechanism.
var libraryAnalysisOnlyTags = map[string]bool{"shallow_exploration": true}

// libraryListTools / libraryReadTools are the list- and read-tool name sets the shallow-exploration
// observation keys on (apogee-sim observer.go @pin, extended with apogee's own spellings — list_dir /
// open_file — the wave-10/12 precedent, so the observation fires on apogee's real menu, not just the
// sim's).
var libraryListTools = map[string]bool{
	"list_files": true, "list_directory": true, "list_dir": true,
}

var libraryReadTools = map[string]bool{
	"read_file": true, "readFile": true, "open_file": true,
}

// libraryToolUseContent / libraryShallowContent are the behavioural notes the observer records,
// ported verbatim from apogee-sim observer.go @pin so the wording the sim's A/B measured is what a
// later inject shows the model.
const (
	libraryToolUseContent = "This model tends to respond with text instead of tool calls when " +
		"tools are available. Always prefer tool calls over text responses when tools can " +
		"accomplish the task."

	libraryShallowContent = "This model tends to summarize code from filenames alone without " +
		"reading file contents. When asked to review or analyze code, always read files with " +
		"read_file before drawing conclusions."
)

// libraryMechanism is the cross-session learning Mechanism: it injects qualifying observations into
// the system prompt (PreRequest) and records completed-Turn outcomes into the store (PostResponse).
// The store and the resolved fingerprint are construction-injected (D3) — the fingerprint is
// resolved once at wire time from the configured model id, so both halves key on the same identity.
type libraryMechanism struct {
	store       *library.Store
	fingerprint domain.ModelFingerprint
}

// newLibrary builds the library Mechanism from the injected Deps (D3). The store is required — a
// library Mechanism with no store cannot observe or inject, so a nil store is a loud construction
// error rather than a silently-inert Mechanism. The fingerprint may be zero (an unidentified model):
// that is not an error, it just leaves the Library inert (nothing to key on).
func newLibrary(deps Deps) (domain.Mechanism, error) {
	if deps.Library == nil {
		return nil, errLibraryStoreRequired
	}
	return &libraryMechanism{store: deps.Library, fingerprint: deps.Fingerprint}, nil
}

// Descriptor identifies library as a strikes-3 proactive-nudge Mechanism (catalogue Table A footnote
// 4): disabled under Bypass (D5), with strikes-3 as the uniform self-regulation backstop over its
// confidence-driven injection gate.
func (*libraryMechanism) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{
		ID:          libraryID,
		Capability:  domain.CapProactiveNudge,
		Suppression: domain.SuppressStrikesThree,
	}
}

// Ordering declares no constraints (catalogue Table A `library`: "none"): the inject side shapes the
// request independently of the other pre-request shapers, and the observe side is a pure reader.
func (*libraryMechanism) Ordering() domain.OrderingConstraints { return domain.OrderingConstraints{} }

// PreRequest injects qualifying observations into the system prompt when the fingerprint clears the
// confidence gate (apogee-sim Injector.Transform @pin). It books a fire only when it actually injects
// (AppendToSystem bumps Revision), so a gated-off or empty-query pass is not a fire (R4).
func (m *libraryMechanism) PreRequest(_ context.Context, req *domain.Request) error {
	// Confidence gate (item 14): prefer not to inject under uncertainty. A zero (unidentified) or
	// low-confidence fingerprint does not inject.
	if m.fingerprint.IsZero() || m.fingerprint.Confidence < libraryMinInjectConfidence {
		return nil
	}
	// Window-fill backoff (apogee-sim transform.go @pin): a nearly-full window has no room for notes.
	if libraryContextTooFull(req) {
		return nil
	}

	entries := m.store.Query(m.fingerprint)
	if len(entries) == 0 {
		return nil
	}
	// Intent filter + injection-budget cap (item 13 deferred both to the inject Mechanism).
	lastUser, _, _ := req.View().Conversation().LastUser()
	entries = libraryFilterByIntent(entries, lastUser.Content)
	entries = libraryCapToBudget(entries, req.View().Budget().CharsPerToken)
	if len(entries) == 0 {
		return nil
	}

	// AppendToSystem's marker check makes a repeat a no-op — the sim's "notes already present" skip.
	req.AppendToSystem(libraryInjectionMarker, libraryBuildInjectionBlock(entries))
	return nil
}

// PostResponse records completed-Turn outcomes into the store (the observe half). It is a pure
// observer: it mutates the store as a side effect but never the *Response and always returns the zero
// decision, so it never short-circuits the cascade and books no fire (R4 — an inspect-only invocation
// is not a fire, which is exactly right for an observer that must not skew self-regulation).
func (m *libraryMechanism) PostResponse(_ context.Context, resp *domain.Response) (domain.PostResponseDecision, error) {
	m.observe(resp)
	return domain.PostResponseDecision{}, nil
}

// observe extracts library-worthy observations from a completed request-response cycle (apogee-sim
// Observer.Observe @pin). A zero fingerprint is inert (store.Record would also refuse it). Failures
// (corrections/behavioural) and the eventual success signal are mutually exclusive within one call:
// recordSuccesses returns early when this response carried validation issues, so a correction is never
// both recorded and success-bumped in the same observe.
func (m *libraryMechanism) observe(resp *domain.Response) {
	if m.fingerprint.IsZero() {
		return
	}
	calls := resp.ToolCalls()
	tools := resp.View().Tools()
	issues := validateToolCalls(calls, tools)

	m.observeValidationFailures(issues)
	m.observeToolNameHallucinations(issues, tools)
	m.observeToolUseEnforcement(resp)
	m.observeShallowExploration(resp, calls)
	m.observeSuccessfulComplexToolCalls(calls, tools, issues)
	m.recordSuccesses(resp, calls, issues)
}

// observeValidationFailures records a correction entry for each validation problem in the response
// (apogee-sim observeValidationFailures @pin). apogee's validate issues carry no Field/Severity, so
// the tag and the supporting-list append are derived from the message text and context keys.
func (m *libraryMechanism) observeValidationFailures(issues []robustnessIssue) {
	for _, issue := range issues {
		tags := []string{"correction"}
		content := issue.message
		if name := libraryToolNameFromIssue(issue.message); name != "" {
			tags = append(tags, name)
		}
		switch {
		case strings.Contains(issue.message, "parameter"):
			tags = append(tags, "missing_param")
			if params, ok := issue.context["required_params"]; ok {
				content += " Required parameters: " + params
			}
		case strings.Contains(issue.message, "not in the tool set"), strings.Contains(issue.message, "missing function name"):
			tags = append(tags, "wrong_tool_name")
			if tools, ok := issue.context["available_tools"]; ok {
				content += " Available tools: " + tools
			}
		case strings.Contains(issue.message, "JSON"), strings.Contains(issue.message, "json"):
			tags = append(tags, "invalid_json")
		}
		m.store.Record(m.fingerprint, library.CategoryCorrection, tags, content)
	}
}

// observeToolNameHallucinations records a targeted "use X instead" correction when the model called a
// tool close to a real one (apogee-sim observeToolNameHallucinations @pin).
func (m *libraryMechanism) observeToolNameHallucinations(issues []robustnessIssue, tools []domain.ToolDef) {
	if len(issues) == 0 || len(tools) == 0 {
		return
	}
	known := make(map[string]bool, len(tools))
	for _, t := range tools {
		known[t.Name] = true
	}
	for _, issue := range issues {
		if !strings.Contains(issue.message, "not in the tool set") {
			continue
		}
		wrong := libraryToolNameFromIssue(issue.message)
		if wrong == "" {
			continue
		}
		closest := libraryFindClosest(wrong, known)
		if closest == "" {
			continue
		}
		content := "The tool \"" + wrong + "\" does not exist. Use \"" + closest + "\" instead."
		m.store.Record(m.fingerprint, library.CategoryCorrection, []string{"correction", "wrong_tool_name", wrong}, content)
	}
}

// observeToolUseEnforcement records the "narrates instead of acting" behavioural pattern when the
// response meets the tool-use enforcer's trigger (apogee-sim observeToolUseEnforcement @pin). apogee
// detects the condition directly via shouldEnforceToolUse (the enforcer's own shape check) rather than
// reading a set flag, so the observation is self-contained — it does not depend on the enforcer
// Mechanism being enabled, and it runs before the enforcer in the post-response cascade regardless.
func (m *libraryMechanism) observeToolUseEnforcement(resp *domain.Response) {
	if !shouldEnforceToolUse(resp) {
		return
	}
	m.store.Record(m.fingerprint, library.CategoryBehavioral, []string{"behavioral", "text_instead_of_tool"}, libraryToolUseContent)
}

// observeShallowExploration records the "summarizes from filenames without reading" behavioural
// pattern: the model listed files but read none, on an analysis-intent request (apogee-sim
// observeShallowExploration @pin).
func (m *libraryMechanism) observeShallowExploration(resp *domain.Response, calls []domain.ToolCall) {
	if len(calls) == 0 {
		return
	}
	hasList, hasRead := false, false
	for _, tc := range calls {
		if libraryListTools[tc.Tool] {
			hasList = true
		}
		if libraryReadTools[tc.Tool] {
			hasRead = true
		}
	}
	if !hasList || hasRead {
		return
	}
	last, _, ok := resp.View().Conversation().LastUser()
	if !ok || !hasAnalysisIntent(last.Content) {
		return
	}
	m.store.Record(m.fingerprint, library.CategoryBehavioral, []string{"behavioral", "shallow_exploration"}, libraryShallowContent)
}

// observeSuccessfulComplexToolCalls records an example of a valid, complex tool call worth showing the
// model again — a clean response (no issues) that used a tool with 5+ parameters (apogee-sim
// observeSuccessfulComplexToolCalls @pin).
func (m *libraryMechanism) observeSuccessfulComplexToolCalls(calls []domain.ToolCall, tools []domain.ToolDef, issues []robustnessIssue) {
	if hasIssues(issues) || len(calls) == 0 {
		return
	}
	complexTools := make(map[string]bool)
	for _, t := range tools {
		if libraryCountSchemaParams(t.Schema) >= 5 {
			complexTools[t.Name] = true
		}
	}
	for _, tc := range calls {
		if !complexTools[tc.Tool] {
			continue
		}
		args := string(tc.Arguments)
		if len(args) > 200 {
			args = args[:200]
		}
		m.store.Record(m.fingerprint, library.CategoryExample, []string{"example", tc.Tool}, "Example valid call for "+tc.Tool+": "+args)
	}
}

// recordSuccesses bumps the success count on this fingerprint's entries when the model did the
// opposite of the recorded failure (apogee-sim recordSuccesses @pin). A clean response (no issues,
// at least one tool call) is the positive signal; matching is by exact fingerprint label (apogee
// keys entries on the full fingerprint, not the sim's substring model-name pattern).
func (m *libraryMechanism) recordSuccesses(resp *domain.Response, calls []domain.ToolCall, issues []robustnessIssue) {
	if hasIssues(issues) || len(calls) == 0 {
		return
	}
	for _, e := range m.store.All() {
		if e.ModelLabel != m.fingerprint.Label {
			continue
		}
		switch e.Category {
		case library.CategoryCorrection:
			for _, tc := range calls {
				if e.HasTag(tc.Tool) && e.HasTag("missing_param") {
					m.store.RecordSuccess(e.ID)
				}
			}
		case library.CategoryBehavioral:
			if libraryBehavioralSuccess(e, resp, calls) {
				m.store.RecordSuccess(e.ID)
			}
		}
	}
}

// libraryBehavioralSuccess reports whether the current turn is a positive signal for a behavioural
// entry — the model just did the opposite of the failure mode that created it (apogee-sim
// behavioralSuccess @pin). The outer recordSuccesses guard already requires no issues and 1+ tool call.
func libraryBehavioralSuccess(e library.Entry, resp *domain.Response, calls []domain.ToolCall) bool {
	switch {
	case e.HasTag("shallow_exploration"):
		last, _, ok := resp.View().Conversation().LastUser()
		if !ok || !hasAnalysisIntent(last.Content) {
			return false
		}
		for _, tc := range calls {
			if libraryReadTools[tc.Tool] {
				return true
			}
		}
		return false
	case e.HasTag("text_instead_of_tool"):
		// A tool call is only evidence of preferring tools over text if the request offered tools.
		return len(resp.View().Tools()) > 0
	}
	return false
}

// libraryContextTooFull reports whether the request's window is too full to inject (apogee-sim
// transform.go usage>0.85 @pin). It estimates the current request's token fill from its message
// content and the calibrated chars→token ratio; an unknown window or ratio (0) disables the backoff,
// matching the sim's `ContextBudget > 0 && CharsPerToken > 0` guard.
func libraryContextTooFull(req *domain.Request) bool {
	budget := req.View().Budget()
	if budget.ContextLimit <= 0 || budget.CharsPerToken <= 0 {
		return false
	}
	totalChars := 0
	req.View().Conversation().Range(func(_ int, msg domain.Message) bool {
		totalChars += len(msg.Content)
		return true
	})
	usage := float64(totalChars) / budget.CharsPerToken / float64(budget.ContextLimit)
	return usage > libraryContextFullFraction
}

// libraryFilterByIntent drops analysis-only entries when the request lacks analysis intent
// (apogee-sim Query WithRequestIntent + entryRequiresAnalysis @pin). The injector always declares the
// request intent, so the filter always applies.
func libraryFilterByIntent(entries []library.Entry, lastUserMessage string) []library.Entry {
	if hasAnalysisIntent(lastUserMessage) {
		return entries
	}
	kept := make([]library.Entry, 0, len(entries))
	for _, e := range entries {
		if libraryEntryRequiresAnalysis(e) {
			continue
		}
		kept = append(kept, e)
	}
	return kept
}

// libraryEntryRequiresAnalysis reports whether an entry is bound to analysis-intent requests only.
func libraryEntryRequiresAnalysis(e library.Entry) bool {
	for tag := range libraryAnalysisOnlyTags {
		if e.HasTag(tag) {
			return true
		}
	}
	return false
}

// libraryCapToBudget keeps the highest-scoring entries (the store already sorts by score desc) whose
// estimated tokens fit the injection budget, de-duplicated by content (apogee-sim Query budget cap
// @pin).
func libraryCapToBudget(entries []library.Entry, charsPerToken float64) []library.Entry {
	if charsPerToken <= 0 {
		charsPerToken = libraryDefaultCharsPerToken
	}
	kept := make([]library.Entry, 0, len(entries))
	used := 0
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		if seen[e.Content] {
			continue
		}
		est := int(float64(len(e.Content)) / charsPerToken)
		if used+est > libraryInjectionBudgetTokens {
			continue
		}
		seen[e.Content] = true
		used += est
		kept = append(kept, e)
	}
	return kept
}

// libraryBuildInjectionBlock renders the entries as a bulleted system-prompt block led by the
// idempotency marker (apogee-sim buildInjectionBlock @pin).
func libraryBuildInjectionBlock(entries []library.Entry) string {
	var b strings.Builder
	b.WriteString(libraryInjectionMarker + " for this model:]\n")
	for _, e := range entries {
		b.WriteString("- ")
		b.WriteString(e.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// libraryToolNameFromIssue extracts the function name a validation message names, matching the
// `function "NAME"` shape apogee's validate messages carry ("... not in the tool set", "missing
// required parameter ... for function ..."). It is the tag that lets recordSuccesses match a
// correction back to a later valid call of the same tool. "" when the message names no function.
func libraryToolNameFromIssue(msg string) string {
	const key = `function "`
	i := strings.Index(msg, key)
	if i < 0 {
		return ""
	}
	rest := msg[i+len(key):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// libraryFindClosest returns the known tool sharing the longest common prefix with wrong, or "" when
// the best match is too weak (apogee-sim findClosest @pin: a shared prefix of at least 3).
func libraryFindClosest(wrong string, known map[string]bool) string {
	wrong = strings.ToLower(wrong)
	best := ""
	bestScore := 0
	for name := range known {
		if score := libraryCommonPrefixLen(wrong, strings.ToLower(name)); score > bestScore {
			bestScore = score
			best = name
		}
	}
	if bestScore < 3 {
		return ""
	}
	return best
}

// libraryCommonPrefixLen returns the length of the shared prefix of a and b (apogee-sim
// commonPrefixLen @pin).
func libraryCommonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// libraryCountSchemaParams counts the properties a tool's argument schema declares (apogee-sim
// countParams @pin). A tool with no schema, or a non-object schema, has zero.
func libraryCountSchemaParams(schema json.RawMessage) int {
	if len(schema) == 0 {
		return 0
	}
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(schema, &s) != nil {
		return 0
	}
	return len(s.Properties)
}
