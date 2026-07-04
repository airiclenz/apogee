package mechanisms

import (
	"strings"
	"unicode"
)

// The intent classifier ported inline from apogee-sim internal/intent/intent.go @pin
// (catalogue C6: intent is a shared helper, never its own catalogue row — it fires no hook and
// carries no descriptor). Its first consumer is the tool_use_enforcer off-ramp below; later waves
// (decompose, cot, library) read the same functions. It is a cheap lexical heuristic: does the
// user's last message ask for an ACTION (edit/create/run …) as opposed to ANALYSIS (summarize/
// explain …) or a question. A Mechanism uses it to decide whether narrating instead of acting is a
// failure worth correcting (an analysis request answered in prose is fine; an action request is not).

// actionVerbs are the imperative verbs that mark an action request (apogee-sim actionVerbs @pin).
var actionVerbs = map[string]bool{
	"update": true, "modify": true, "fix": true, "create": true,
	"write": true, "delete": true, "edit": true, "change": true,
	"refactor": true, "add": true, "remove": true, "implement": true,
	"replace": true, "rename": true, "move": true, "read": true,
	"run": true, "execute": true, "install": true, "build": true,
	"insert": true, "append": true, "verify": true, "check": true,
}

// questionWords, as the FIRST token or with a trailing "?", mark a question rather than a command,
// so an action verb inside one does not count as an action request (apogee-sim questionWords @pin).
var questionWords = map[string]bool{
	"what": true, "why": true, "how": true, "when": true,
	"where": true, "who": true, "which": true,
	"explain": true, "describe": true,
}

// analysisVerbs mark an analysis/read-only request the enforcer must NOT push into a tool call
// (apogee-sim analysisVerbs @pin).
var analysisVerbs = map[string]bool{
	"summarize": true, "analyze": true, "audit": true, "assess": true,
	"explore": true, "survey": true, "overview": true, "review": true,
	"examine": true,
}

// analysisPhrases are multi-word analysis cues a single-token scan would miss (apogee-sim
// analysisPhrases @pin).
var analysisPhrases = []string{
	"what does this", "what does each", "walk me through",
	"tell me about", "give me an overview",
}

// hasActionIntent reports whether userMessage is an imperative action request: it contains an
// action verb and is not phrased as a question (apogee-sim intent.HasActionIntent @pin).
func hasActionIntent(userMessage string) bool {
	msg := strings.TrimSpace(userMessage)
	if msg == "" {
		return false
	}
	words := tokenizeWords(msg)
	if len(words) == 0 {
		return false
	}

	hasAction := false
	for _, w := range words {
		if actionVerbs[w] {
			hasAction = true
			break
		}
	}
	if !hasAction {
		return false
	}

	if questionWords[words[0]] || strings.HasSuffix(msg, "?") {
		return false
	}
	return true
}

// hasAnalysisIntent reports whether userMessage asks for analysis/summary rather than a concrete
// change (apogee-sim intent.HasAnalysisIntent @pin) — an analysis phrase or an analysis verb.
func hasAnalysisIntent(userMessage string) bool {
	msg := strings.TrimSpace(userMessage)
	if msg == "" {
		return false
	}

	lower := strings.ToLower(msg)
	for _, phrase := range analysisPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}

	for _, w := range tokenizeWords(msg) {
		if analysisVerbs[w] {
			return true
		}
	}
	return false
}

// tokenizeWords lower-cases text and splits it into alphanumeric words (apogee-sim intent.tokenize
// @pin), the token stream the intent maps are keyed on.
func tokenizeWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
