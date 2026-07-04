package mechanisms

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// decompose registers the task-decomposition pre-request Mechanism in the catalogue constructor
// table (Phase-4 item 12, Wave 4 — the last of the request shapers). Default-off (D1) — the config
// surface builds it only when the `mechanisms:` block enables it. It is ported from apogee-sim
// internal/decompose/decompose.go @pin: for a small model that stalls on long multi-step prompts,
// it (1) collapses complex prompts sitting in conversation history so the model cannot re-read a
// full step-by-step plan from an earlier turn, and (2) hints the single next actionable step of the
// current prompt into the system prompt, keeping the full user message intact.
func init() { catalogue[decomposeID] = newDecompose }

const decomposeID domain.MechanismID = "decompose"

// decompose directives + their idempotency markers, ported verbatim from apogee-sim
// internal/decompose/decompose.go @pin (behavior ground-truth, D7 — the wording the sim's A/B
// measured). Each directive embeds its marker so AppendToSystem's marker check makes a second
// inject on the same request a no-op.
const (
	decomposeFocusDirective = "Focus on one action at a time. When given a task, " +
		"perform the first concrete step by calling the appropriate tool. " +
		"Never write code or file contents as text in your response — always use " +
		"the provided tools to create and modify files. " +
		"Do not plan ahead or describe future steps."

	decomposeContinuationDirective = "Your previous explanation or reasoning has been noted. " +
		"That step is complete — now proceed with the next concrete action using tools."

	decomposeFocusMarker        = "Focus on one action"
	decomposeContinuationMarker = "previous explanation"
	decomposeDecomposedMarker   = "Your next step:"
	decomposeStepHintMarker     = "Apogee step hint:"
)

// decomposeThreshold is the sim's fixed complexity setting (decompose.go:112 @pin): only a prompt
// assessed "complex" is decomposed ("medium" — the middle of low/medium/high).
const decomposeThreshold = "medium"

// decomposeStepPattern matches numbered step formats: "1.", "1)", "Step 1 —", "Step 1:", "Step 1 -"
// (apogee-sim stepPattern @pin).
var decomposeStepPattern = regexp.MustCompile(`(?mi)^\s*(?:Step\s+)?\d+\s*[.)\-—:]\s`)

// decomposeStepExtract captures the text after a step marker on the same line (apogee-sim
// stepExtract @pin).
var decomposeStepExtract = regexp.MustCompile(`(?mi)^\s*(?:Step\s+)?\d+\s*[.)\-—:]\s+(.+)`)

// decomposeSubAgentPrefix strips "One sub-agent should", "A second sub-agent should", etc.
// (apogee-sim subAgentPrefix @pin).
var decomposeSubAgentPrefix = regexp.MustCompile(`(?i)^(?:one|a|an|the|first|second|third|another)\s+(?:(?:first|second|third|other|next)\s+)?sub-?agent\s+should\s+`)

var decomposeDelegationPhrases = []string{
	"sub-agent", "subagent", "sub agent", "spawn",
	"delegate", "hand off", "handoff", "orchestrat",
}

var decomposeConditionalPhrases = []string{
	"if ", "otherwise", "depending on", "in case",
	"unless", "fallback", "else ",
}

var decomposeReviewPhrases = []string{
	"review", "validate the", "verify the output",
	"check the result", "double-check", "double check",
	"iterate until",
}

var decomposePhasePhrases = []string{
	"phase 1", "phase 2", "step 1", "step 2",
	"first,", "then,", "finally,", "after that",
	"once done", "once complete", "next,",
}

// decomposeExplanatoryVerbs mark a step that produces text (reasoning/planning/explaining) rather
// than a tool-call action (apogee-sim explanatoryVerbs @pin).
var decomposeExplanatoryVerbs = []string{
	"explain", "describe", "reason", "outline",
	"think about", "think through", "consider",
	"discuss", "summarize", "design", "plan",
}

type decomposeComplexity string

const (
	decomposeSimple   decomposeComplexity = "simple"
	decomposeModerate decomposeComplexity = "moderate"
	decomposeComplex  decomposeComplexity = "complex"
)

// decomposeComplexityResult carries an assessed prompt's level, its numeric score, and the signals
// that contributed (apogee-sim complexityResult @pin).
type decomposeComplexityResult struct {
	level   decomposeComplexity
	score   int
	signals []string
}

// wave4WriteTools is the write-tool set decompose and the cot nudges inspect for "has the model
// written a file yet" — apogee-sim toolsets.WriteTools @pin extended with apogee's own write-tool
// spellings (edit_existing_file / single_&_multi_find_and_replace), so the nudges fire on apogee's
// real menu (the item-10 filehint precedent — apogee's read_file / list_dir / grep already appear
// in the sim's read/list/search sets, so only the write set needs the apogee names added).
var wave4WriteTools = map[string]bool{
	"write_file": true, "writeFile": true, "write_to_file": true, "create_file": true,
	"edit_file": true, "editFile": true, "replace_in_file": true,
	"edit_existing_file": true, "single_find_and_replace": true, "multi_find_and_replace": true,
}

// hasWrittenFiles reports whether any assistant message issued a write-tool call — the "model has
// already started writing" signal decompose and cot both gate on (apogee-sim toolsets.HasWrittenFiles
// @pin, over wave4WriteTools so apogee's own write tools count).
func hasWrittenFiles(conv domain.ConversationView) bool {
	found := false
	conv.Range(func(_ int, m domain.Message) bool {
		if m.Role != domain.RoleAssistant {
			return true
		}
		for _, tc := range m.ToolCalls {
			if wave4WriteTools[tc.Tool] {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// decomposeMechanism is the pre-request Mechanism that collapses complex history and hints the next
// step (catalogue Table A `decompose`). It carries no per-Mechanism state: strikes-3 self-regulation
// routes through the loop's per-Session tracker (item 3), and its read-loop coupling is a live
// LoopView.Fired query (below), not a shared meta map.
type decomposeMechanism struct{}

// newDecompose builds the decompose Mechanism. It needs no injected Deps (D3): decomposition reads
// the conversation and tool menu off the Request it is handed.
func newDecompose(Deps) (domain.Mechanism, error) { return decomposeMechanism{}, nil }

// Descriptor identifies decompose as a strikes-3 proactive-nudge Mechanism (catalogue Table A):
// disabled under Bypass (D5), withdrawn by self-regulation after repeated non-help.
func (decomposeMechanism) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{
		ID:          decomposeID,
		Capability:  domain.CapProactiveNudge,
		Suppression: domain.SuppressStrikesThree,
	}
}

// Ordering declares decompose After toolfilter (catalogue Table A: "trim the menu before the
// user-message rewrite" — toolfilter already declares the mirror Before edge). The read-loop
// coupling is expressed as a runtime LoopView.Fired query in PreRequest (D2), not an ordering edge,
// so it is not encoded here.
func (decomposeMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{After: []domain.MechanismID{toolFilterID}}
}

// PreRequest collapses complex multi-step prompts still sitting in history and hints the single next
// actionable step of the current prompt, faithfully reproducing apogee-sim Decomposer.Transform's
// control flow @pin. All conversation reads happen before any mutation so the decision is taken
// against the request as received; the mutations (SetMessageContent for the history collapse,
// AppendToSystem for the directives) then apply. It books no fire (the loop keys acted fires on
// Request.Revision, R4) when nothing is collapsed and no directive is injected — the sim's Skip paths.
//
// The read-loop coupling (catalogue Table A / C's "muted when read_loop has Fired") is the
// LoopView.Fired query below (D2): when the consolidated read_loop Mechanism has already acted this
// Session, the model is already being told to stop re-reading, so active decomposition — which would
// override the focus to "step 1: …" — is muted (the sim's RequestMeta.FiredCounts peek, which R4
// gives the same acted-fire semantics). History collapse still runs; only the focus/step branch mutes.
func (decomposeMechanism) PreRequest(_ context.Context, req *domain.Request) error {
	if len(req.View().Tools()) == 0 {
		return nil // apogee-sim Skip("skipped — no tools in request")
	}
	conv := req.View().Conversation()

	readLoopFired := req.View().Fired(readLoopID) > 0
	wroteFiles := hasWrittenFiles(conv)

	// Read-only detection, all against the conversation as received (before any mutation).
	collapse := decomposeCollapseTargets(conv, decomposeThreshold)
	priorTextOnly := decomposeHasPriorTextOnlyResponse(conv)

	var stepHint string
	if lastMsg, _, ok := conv.LastUser(); ok && strings.TrimSpace(lastMsg.Content) != "" &&
		hasActionIntent(lastMsg.Content) {
		if result := decomposeAssessComplexity(lastMsg.Content); decomposeExceedsThreshold(result.level, decomposeThreshold) {
			completed := decomposeCountCompletedSteps(conv)
			if simplified, _, _, done := decomposeExtractStep(lastMsg.Content, completed); done {
				stepHint = simplified
			}
		}
	}

	// Mutations, in the sim's order.
	for _, t := range collapse {
		req.SetMessageContent(t.index, t.content)
	}
	if len(collapse) > 0 && !wroteFiles && !readLoopFired {
		decomposeInjectFocusDirective(req)
	}

	if wroteFiles { // sim: model has written files — no continuation, no step hint
		return nil
	}
	if readLoopFired { // S1 mute: active decomposition would contradict the read-loop hint
		return nil
	}

	if priorTextOnly {
		decomposeInjectContinuationDirective(req)
	}
	if stepHint != "" {
		decomposeInjectStepHint(req, stepHint)
	}
	return nil
}

// decomposeCollapseTarget is one older user message the history collapse rewrites, resolved to a
// (index, replacement) pair before any mutation so the collapse is a pure apply step.
type decomposeCollapseTarget struct {
	index   int
	content string
}

// decomposeCollapseTargets finds the complex multi-step user messages in history (every user message
// before the last) and pairs each with its collapsed summary (apogee-sim collapseComplexHistory
// @pin). Returning the plan rather than mutating in place keeps PreRequest's reads strictly before
// its writes.
func decomposeCollapseTargets(conv domain.ConversationView, threshold string) []decomposeCollapseTarget {
	lastUserIdx := -1
	for i := conv.Len() - 1; i >= 0; i-- {
		if conv.At(i).Role == domain.RoleUser {
			lastUserIdx = i
			break
		}
	}
	if lastUserIdx < 0 {
		return nil
	}

	var targets []decomposeCollapseTarget
	for i := 0; i < lastUserIdx; i++ {
		m := conv.At(i)
		if m.Role != domain.RoleUser {
			continue
		}
		if result := decomposeAssessComplexity(m.Content); decomposeExceedsThreshold(result.level, threshold) {
			targets = append(targets, decomposeCollapseTarget{index: i, content: decomposeCollapseMessage(m.Content)})
		}
	}
	return targets
}

// decomposeCollapseMessage replaces a multi-step prompt with a short task summary — the lead-in
// context up to the first numbered step, truncated at a sentence boundary, plus a note that the
// steps were omitted (apogee-sim collapseMessage @pin).
func decomposeCollapseMessage(msg string) string {
	lines := strings.Split(msg, "\n")
	var context []string
	for _, line := range lines {
		if decomposeStepExtract.MatchString(line) {
			break
		}
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			context = append(context, trimmed)
		}
	}
	if len(context) == 0 {
		return "[Multi-step task instructions were provided — focus on the current request.]"
	}
	summary := decomposeTruncateAtSentence(strings.Join(context, " "), 200)
	return summary + " [Detailed steps omitted — focus on the current request.]"
}

// decomposeAssessComplexity scores a prompt's structural complexity from numbered steps, delegation
// / conditional / review / phase language, and length (apogee-sim assessComplexity @pin).
func decomposeAssessComplexity(text string) decomposeComplexityResult {
	score := 0
	var signals []string
	lower := strings.ToLower(text)

	if numberedSteps := len(decomposeStepPattern.FindAllStringIndex(text, -1)); numberedSteps > 1 {
		pts := (numberedSteps - 1) * 2
		score += pts
		signals = append(signals, fmt.Sprintf("%d numbered steps (+%d)", numberedSteps, pts))
	}
	if pts := decomposeCountPhraseMatches(lower, decomposeDelegationPhrases, 4, 8); pts > 0 {
		score += pts
		signals = append(signals, fmt.Sprintf("delegation language (+%d)", pts))
	}
	if pts := decomposeCountPhraseMatches(lower, decomposeConditionalPhrases, 3, 6); pts > 0 {
		score += pts
		signals = append(signals, fmt.Sprintf("conditional logic (+%d)", pts))
	}
	if pts := decomposeCountPhraseMatches(lower, decomposeReviewPhrases, 2, 4); pts > 0 {
		score += pts
		signals = append(signals, fmt.Sprintf("review/validation (+%d)", pts))
	}
	if pts := decomposeCountPhraseMatches(lower, decomposePhasePhrases, 2, 6); pts > 0 {
		score += pts
		signals = append(signals, fmt.Sprintf("phase markers (+%d)", pts))
	}
	if len(text) > 1500 {
		score += 4
		signals = append(signals, "long message (+4)")
	} else if len(text) > 800 {
		score += 2
		signals = append(signals, "long message (+2)")
	}

	level := decomposeSimple
	if score >= 10 {
		level = decomposeComplex
	} else if score >= 4 {
		level = decomposeModerate
	}
	return decomposeComplexityResult{level: level, score: score, signals: signals}
}

// decomposeCountPhraseMatches sums perMatch points for each phrase present in text, capped
// (apogee-sim countPhraseMatches @pin).
func decomposeCountPhraseMatches(text string, phrases []string, perMatch, cap int) int {
	total := 0
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			total += perMatch
			if total >= cap {
				return cap
			}
		}
	}
	return total
}

// decomposeExceedsThreshold reports whether a detected level exceeds the configured setting
// (apogee-sim exceedsThreshold @pin) — at "medium" only a "complex" prompt is decomposed.
func decomposeExceedsThreshold(detected decomposeComplexity, setting string) bool {
	switch setting {
	case "low":
		return detected == decomposeModerate || detected == decomposeComplex
	case "medium":
		return detected == decomposeComplex
	default: // "high" disables decomposition; unknown settings do too
		return false
	}
}

// decomposeCountCompletedSteps counts assistant responses (tool-call AND text-only) as completed
// steps, so a text-only explanation still advances the step counter (apogee-sim countCompletedSteps
// @pin).
func decomposeCountCompletedSteps(conv domain.ConversationView) int {
	completed := 0
	conv.Range(func(_ int, m domain.Message) bool {
		if m.Role == domain.RoleAssistant && (len(m.ToolCalls) > 0 || m.Content != "") {
			completed++
		}
		return true
	})
	return completed
}

// decomposeExtractStep extracts the single step to focus on from a multi-step prompt (apogee-sim
// decompose() @pin). completedSteps selects which step (0 = first). It returns the simplified step
// body (context + "Your next step: …"), the 0-based step index, the total step count, and whether a
// step was extracted.
func decomposeExtractStep(msg string, completedSteps int) (string, int, int, bool) {
	lines := strings.Split(msg, "\n")

	var contextLines []string
	var stepBlocks [][]string
	curStep := -1
	for _, line := range lines {
		if decomposeStepExtract.MatchString(line) {
			m := decomposeStepExtract.FindStringSubmatch(line)
			curStep++
			stepBlocks = append(stepBlocks, []string{strings.TrimSpace(m[1])})
		} else if curStep >= 0 {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				stepBlocks[curStep] = append(stepBlocks[curStep], trimmed)
			}
		} else {
			contextLines = append(contextLines, line)
		}
	}

	if len(stepBlocks) > 0 {
		if completedSteps >= len(stepBlocks) {
			return "", 0, 0, false
		}
		ctx := strings.TrimSpace(strings.Join(contextLines, "\n"))
		if ctx == "" {
			ctx = "Task in progress."
		}
		ctx = decomposeTruncateAtSentence(ctx, 300)

		stepIdx := decomposeEffectiveStepIdx(stepBlocks, completedSteps)

		// An explanatory (non-actionable) step is merged with the next actionable step so the
		// hint never asks for a text-only reply that breaks the coding tool's auto-continuation.
		endIdx := stepIdx
		if decomposeIsExplanatoryStep(stepBlocks[stepIdx]) && stepIdx < len(stepBlocks)-1 {
			for j := stepIdx + 1; j < len(stepBlocks); j++ {
				endIdx = j
				if !decomposeIsExplanatoryStep(stepBlocks[j]) {
					break
				}
			}
		}

		var task string
		if endIdx > stepIdx {
			var explanatoryParts []string
			for i := stepIdx; i < endIdx; i++ {
				explanatoryParts = append(explanatoryParts, decomposeExtractTask(stepBlocks[i]))
			}
			explanatoryCtx := decomposeTruncateAtSentence(strings.Join(explanatoryParts, " "), 250)
			task = explanatoryCtx + "\n\nNow proceed: " + decomposeExtractTask(stepBlocks[endIdx])
		} else {
			task = decomposeExtractTask(stepBlocks[stepIdx])
		}
		return ctx + "\n\n" + decomposeDecomposedMarker + " " + task, stepIdx, len(stepBlocks), true
	}

	// No numbered steps — try the first action sentence.
	first := decomposeExtractFirstActionSentence(msg)
	if first == "" {
		return "", 0, 0, false
	}
	ctx := decomposeFirstSentence(msg)
	if ctx == first {
		return "", 0, 0, false
	}
	ctx = decomposeTruncateAtSentence(ctx, 300)
	return ctx + "\n\n" + decomposeDecomposedMarker + " " + first, 0, 1, true
}

// decomposeExtractTask turns a step block (title + body) into a clean task description, stripping
// delegation framing and sub-agent prefixes (apogee-sim extractTask @pin).
func decomposeExtractTask(block []string) string {
	if len(block) == 0 {
		return ""
	}
	var taskParts []string
	for _, line := range block {
		lower := strings.ToLower(line)
		if decomposeIsDelegationFrame(lower) {
			continue
		}
		cleaned := decomposeSubAgentPrefix.ReplaceAllString(line, "")
		if cleaned != line {
			if len(cleaned) > 0 {
				cleaned = strings.ToUpper(cleaned[:1]) + cleaned[1:]
			}
			taskParts = append(taskParts, cleaned)
			continue
		}
		taskParts = append(taskParts, line)
	}
	if len(taskParts) == 0 {
		return block[0]
	}
	return decomposeTruncateAtSentence(strings.Join(taskParts, " "), 500)
}

// decomposeIsDelegationFrame reports whether a lower-cased line is delegation scaffolding rather than
// an actual work description (apogee-sim isDelegationFrame @pin).
func decomposeIsDelegationFrame(lower string) bool {
	frames := []string{
		"spawn sub-agent", "spawn subagent", "spawn a sub-agent",
		"delegate to", "hand off to", "before creating any files",
		"before writing any", "summarize what the sub-agent",
		"report what each sub-agent", "report what the sub-agent",
	}
	for _, f := range frames {
		if strings.Contains(lower, f) {
			return true
		}
	}
	return false
}

// decomposeExtractFirstActionSentence returns the first sentence expressing an action intent
// (apogee-sim extractFirstActionSentence @pin).
func decomposeExtractFirstActionSentence(text string) string {
	for _, s := range decomposeSplitSentences(text) {
		if s = strings.TrimSpace(s); s != "" && hasActionIntent(s) {
			return s
		}
	}
	return ""
}

// decomposeFirstSentence returns the first sentence of text (apogee-sim firstSentence @pin).
func decomposeFirstSentence(text string) string {
	sentences := decomposeSplitSentences(text)
	if len(sentences) == 0 {
		return text
	}
	return strings.TrimSpace(sentences[0])
}

// decomposeSplitSentences splits text on sentence-terminating punctuation followed by whitespace
// (apogee-sim splitSentences @pin).
func decomposeSplitSentences(text string) []string {
	var sentences []string
	start := 0
	for i, r := range text {
		if r == '.' || r == '!' || r == '?' {
			if i+1 < len(text) && (text[i+1] == ' ' || text[i+1] == '\n') {
				sentences = append(sentences, text[start:i+1])
				start = i + 2
			}
		}
	}
	if start < len(text) {
		sentences = append(sentences, text[start:])
	}
	return sentences
}

// decomposeTruncateAtSentence truncates text to at most maxChars, cutting at the last sentence
// boundary before the limit (apogee-sim truncateAtSentenceBoundary @pin).
func decomposeTruncateAtSentence(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	cut := maxChars
	for i := maxChars - 1; i > 0; i-- {
		if text[i] == '.' || text[i] == '!' || text[i] == '?' {
			cut = i + 1
			break
		}
	}
	return strings.TrimSpace(text[:cut])
}

// decomposeIsExplanatoryStep reports whether a step block's title indicates a text-producing step
// rather than a tool-call action (apogee-sim isExplanatoryStep @pin).
func decomposeIsExplanatoryStep(block []string) bool {
	if len(block) == 0 {
		return false
	}
	lower := strings.ToLower(block[0])
	for _, verb := range decomposeExplanatoryVerbs {
		if strings.Contains(lower, verb) {
			return true
		}
	}
	return false
}

// decomposeEffectiveStepIdx maps a completed-step count to the physical step index, accounting for
// explanatory steps that were merged with their following actionable step in prior turns (apogee-sim
// effectiveStepIdx @pin).
func decomposeEffectiveStepIdx(stepBlocks [][]string, completedSteps int) int {
	i := 0
	for effective := 0; effective < completedSteps && i < len(stepBlocks); effective++ {
		if decomposeIsExplanatoryStep(stepBlocks[i]) && i < len(stepBlocks)-1 {
			for i < len(stepBlocks)-1 && decomposeIsExplanatoryStep(stepBlocks[i]) {
				i++
			}
			i++
		} else {
			i++
		}
	}
	if i >= len(stepBlocks) {
		i = len(stepBlocks) - 1
	}
	return i
}

// decomposeHasPriorTextOnlyResponse reports whether the most recent assistant message was text-only
// (no tool calls) — the signal that the model finished an explanatory step and may need a nudge to
// proceed (apogee-sim hasPriorTextOnlyResponse @pin).
func decomposeHasPriorTextOnlyResponse(conv domain.ConversationView) bool {
	for i := conv.Len() - 1; i >= 0; i-- {
		if m := conv.At(i); m.Role == domain.RoleAssistant {
			return len(m.ToolCalls) == 0 && m.Content != ""
		}
	}
	return false
}

// decomposeInjectFocusDirective appends the focus directive to the system prompt, idempotent on the
// focus marker (apogee-sim injectFocusDirective @pin, via the shared role-safe AppendToSystem).
func decomposeInjectFocusDirective(req *domain.Request) {
	req.AppendToSystem(decomposeFocusMarker, decomposeFocusDirective)
}

// decomposeInjectContinuationDirective appends the continuation directive to the system prompt,
// idempotent on the continuation marker (apogee-sim injectContinuationDirective @pin).
func decomposeInjectContinuationDirective(req *domain.Request) {
	req.AppendToSystem(decomposeContinuationMarker, decomposeContinuationDirective)
}

// decomposeInjectStepHint appends the step-focus addendum to the system prompt (apogee-sim
// injectStepHint @pin). The user message is left intact so the model retains the full task context.
// When no focus directive is present yet, the focus directive is prepended to the addendum (matching
// the sim), so the combined inject carries both markers; AppendToSystem's step-hint marker check
// makes a repeat a no-op.
func decomposeInjectStepHint(req *domain.Request, simplified string) {
	hint := decomposeStepHintMarker + "\n" + simplified
	addition := hint
	if !decomposeSystemHasMarker(req, decomposeFocusMarker) {
		addition = decomposeFocusDirective + "\n\n" + hint
	}
	req.AppendToSystem(decomposeStepHintMarker, addition)
}

// decomposeSystemHasMarker reports whether marker already occurs in a system message of the request
// as it currently stands. It re-reads req.View() so it reflects directives injected earlier in this
// same PreRequest pass (a prior AppendToSystem may have created a new system message).
func decomposeSystemHasMarker(req *domain.Request, marker string) bool {
	found := false
	req.View().Conversation().Range(func(_ int, m domain.Message) bool {
		if m.Role == domain.RoleSystem && strings.Contains(m.Content, marker) {
			found = true
			return false
		}
		return true
	})
	return found
}
