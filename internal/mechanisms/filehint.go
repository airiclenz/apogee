package mechanisms

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/airiclenz/apogee/internal/domain"
)

// filehint registers the workspace-file-hint pre-request Mechanism in the catalogue constructor
// table (Phase-4 item 10, Wave 3 request shapers). Default-off (D1) — the config surface builds it
// only when the `mechanisms:` block enables it. It is ported from apogee-sim
// internal/filehint/filehint.go + internal/proxy/file_hint_detector.go @pin: after the model lists
// a directory but before it reads anything, it scores the listed files against the user's prompt
// and injects a role-safe hint suggesting the most relevant files to read first.
func init() {
	catalogue[fileHintID] = newFileHint
	descriptors[fileHintID] = fileHintDescriptor
}

const fileHintID domain.MechanismID = "filehint"

// fileHint thresholds mirror apogee-sim file_hint_detector.go:13-17 @pin: a hint needs at least
// fileHintMinFiles listed files, keeps only files scoring at or above fileHintScoreThresh, and
// suggests at most fileHintMaxSuggest of them.
const (
	fileHintMinFiles    = 3
	fileHintScoreThresh = 2.0
	fileHintMaxSuggest  = 5
)

// fileHintMarker is the stable lead of the injected hint. filehint checks the request for it before
// injecting so a second invocation on the same request never double-injects (the item's idempotency
// requirement); Request.InjectContext carries no marker of its own, unlike AppendToSystem.
const fileHintMarker = "Based on the directory listing, these files are likely most relevant"

// fileHintListTools / fileHintReadTools name the directory-listing and file-reading tools whose
// interplay opens and closes a hint opportunity. They compose from the shared spelling families
// (listSpellings / readSpellings, decompose.go) so a mixed MCP menu still triggers: the list set now
// carries the full family — the F8 gap fix adds listDir to the list_dir / list_files / listFiles /
// list_directory it had — and the read set carries apogee's open_file alongside the sim's spellings.
var (
	fileHintListTools = toolSet(listSpellings)
	fileHintReadTools = toolSet(readSpellings)
)

// fileHintWriteTools mark that the model has already shared written files, which suppresses the
// greenfield skip (apogee-sim toolsets.WriteTools @pin, extended with apogee's own write-tool
// names: edit_existing_file / single_find_and_replace / multi_find_and_replace).
var fileHintWriteTools = map[string]bool{
	"write_file": true, "writeFile": true, "write_to_file": true,
	"create_file": true, "edit_file": true, "editFile": true, "replace_in_file": true,
	"edit_existing_file": true, "single_find_and_replace": true, "multi_find_and_replace": true,
}

// fileHintMechanism is the pre-request Mechanism that injects workspace file hints (catalogue
// Table A `filehint`; ported from apogee-sim injectFileHintIfNeeded @pin). It carries no
// per-Mechanism state: the descriptor's strikes-3 policy routes self-regulation through the loop's
// per-Session tracker (item 3), and it is internally greenfield-suppressed (catalogue Table A:
// "none (greenfield-suppressed internally)").
type fileHintMechanism struct{}

// newFileHint builds the filehint Mechanism. It needs no injected Deps (D3): the hint is derived
// entirely from the conversation on the Request it is handed.
func newFileHint(Deps) (domain.Mechanism, error) { return fileHintMechanism{}, nil }

// fileHintDescriptor identifies filehint as a strikes-3 proactive-nudge Mechanism (catalogue
// Table A): disabled under Bypass (D5), withdrawn by self-regulation after repeated non-help.
var fileHintDescriptor = domain.MechanismDescriptor{
	ID:          fileHintID,
	Capability:  domain.CapProactiveNudge,
	Suppression: domain.SuppressStrikesThree,
}

// Descriptor returns filehint's static catalogue descriptor.
func (fileHintMechanism) Descriptor() domain.MechanismDescriptor { return fileHintDescriptor }

// Ordering declares no constraints (catalogue Table A: "none (greenfield-suppressed internally)"):
// the hint injector is a request-prep step with no hard order against the other shapers.
func (fileHintMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{}
}

// PreRequest injects a role-safe file hint when the model has listed a directory it has not yet
// read from and the listed files score against the user prompt. It is a no-op — booking no fire
// (the loop keys acted fires on Request.Revision, R4) — when there is no opportunity, the task is a
// greenfield creation with no files shared yet, no file clears the score threshold, or the hint was
// already injected on this request (fileHintMarker present). Mirrors apogee-sim
// injectFileHintIfNeeded @pin.
func (fileHintMechanism) PreRequest(_ context.Context, req *domain.Request) error {
	conv := req.View().Conversation()

	filenames, userPrompt, ok := fileHintDetectOpportunity(conv)
	if !ok {
		return nil
	}
	if fileHintIsCreationFocused(userPrompt) && !fileHintHasWrittenFiles(conv) {
		return nil
	}

	var relevant []fileHintScoredFile
	for _, sf := range fileHintScoreFiles(filenames, userPrompt) {
		if sf.score >= fileHintScoreThresh {
			relevant = append(relevant, sf)
		}
	}
	if len(relevant) == 0 {
		return nil
	}

	if fileHintAlreadyInjected(conv) {
		return nil
	}
	req.InjectContext(fileHintBuild(relevant, fileHintMaxSuggest))
	return nil
}

// fileHintAlreadyInjected reports whether a hint carrying fileHintMarker is already present in the
// request messages — the idempotency guard against a double-inject.
func fileHintAlreadyInjected(conv domain.ConversationView) bool {
	found := false
	conv.Range(func(_ int, m domain.Message) bool {
		if strings.Contains(m.Content, fileHintMarker) {
			found = true
			return false
		}
		return true
	})
	return found
}

// fileHintDetectOpportunity finds the most recent directory listing the model has not yet read
// from, gathers the listed filenames from the following tool results, and pairs them with the last
// non-empty user prompt (apogee-sim detectFileHintOpportunity @pin). ok is false when the model has
// already read after listing, fewer than fileHintMinFiles files were listed, or no user prompt is
// present.
func fileHintDetectOpportunity(conv domain.ConversationView) (filenames []string, userPrompt string, ok bool) {
	lastListIdx := -1
	hasReadAfterList := false
	for i := 0; i < conv.Len(); i++ {
		m := conv.At(i)
		if m.Role != domain.RoleAssistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			if fileHintListTools[tc.Tool] {
				lastListIdx = i
				hasReadAfterList = false
			}
			if lastListIdx >= 0 && fileHintReadTools[tc.Tool] {
				hasReadAfterList = true
			}
		}
	}
	if lastListIdx < 0 || hasReadAfterList {
		return nil, "", false
	}

	for j := lastListIdx + 1; j < conv.Len(); j++ {
		m := conv.At(j)
		if m.Role == domain.RoleTool {
			filenames = append(filenames, fileHintParseList(m.Content)...)
		}
		if m.Role == domain.RoleAssistant || m.Role == domain.RoleUser {
			break
		}
	}
	if len(filenames) < fileHintMinFiles {
		return nil, "", false
	}

	if msg, _, found := conv.LastUser(); found && strings.TrimSpace(msg.Content) != "" {
		userPrompt = msg.Content
	}
	if userPrompt == "" {
		return nil, "", false
	}
	return filenames, userPrompt, true
}

// fileHintCreationVerbs mark a greenfield creation task (apogee-sim creationVerbs @pin).
var fileHintCreationVerbs = map[string]bool{
	"create": true, "build": true, "implement": true, "make": true,
	"write": true, "develop": true, "scaffold": true,
}

// fileHintBacktickFileRe matches a backticked filename in the prompt (apogee-sim backtickFileRe
// @pin) — two or more of these plus repeated creation verbs mean the task is creating new files,
// not exploring existing ones.
var fileHintBacktickFileRe = regexp.MustCompile("`[a-zA-Z0-9_/.-]+\\.[a-z]{1,4}`")

// fileHintIsCreationFocused reports whether the prompt is primarily about creating new files, in
// which case hinting at existing files to read is unhelpful (apogee-sim isCreationFocused @pin).
func fileHintIsCreationFocused(prompt string) bool {
	lower := strings.ToLower(prompt)
	creationCount := 0
	for _, w := range strings.Fields(lower) {
		if fileHintCreationVerbs[w] {
			creationCount++
		}
	}
	return creationCount >= 2 && len(fileHintBacktickFileRe.FindAllString(prompt, 3)) >= 2
}

// fileHintHasWrittenFiles reports whether any assistant message issued a write tool call — the
// signal that suppresses the greenfield skip (apogee-sim toolsets.HasWrittenFiles @pin).
func fileHintHasWrittenFiles(conv domain.ConversationView) bool {
	found := false
	conv.Range(func(_ int, m domain.Message) bool {
		if m.Role != domain.RoleAssistant {
			return true
		}
		for _, tc := range m.ToolCalls {
			if fileHintWriteTools[tc.Tool] {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// fileHintParseList extracts filenames from a directory-listing tool result: a JSON string array
// when the content parses as one, else one filename per non-empty line with list bullets and the
// `ls`/"Contents of" preambles stripped (apogee-sim parseFileList @pin).
func fileHintParseList(content string) []string {
	var parsed []string
	if json.Unmarshal([]byte(content), &parsed) == nil {
		return parsed
	}
	var files []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		if line != "" && !strings.HasPrefix(line, "total ") && !strings.HasPrefix(line, "Contents of") {
			files = append(files, line)
		}
	}
	return files
}

// fileHintScoredFile is a filename with its relevance score and the tokens that matched.
type fileHintScoredFile struct {
	name    string
	score   float64
	matches []string
}

// fileHintScoreFiles scores each filename against the prompt keywords with a TF-IDF-ish weight —
// a token common across the listing counts for less — plus a small boost when the filename's
// extension matches a language named in the prompt (apogee-sim filehint.ScoreFiles @pin). The
// result is sorted by descending score.
func fileHintScoreFiles(filenames []string, userPrompt string) []fileHintScoredFile {
	keywords := fileHintExtractKeywords(userPrompt)
	if len(keywords) == 0 {
		return nil
	}

	docFreq := make(map[string]int)
	tokenized := make([][]string, len(filenames))
	for i, name := range filenames {
		tokens := fileHintTokenizePath(name)
		tokenized[i] = tokens
		seen := make(map[string]bool)
		for _, t := range tokens {
			if !seen[t] {
				docFreq[t]++
				seen[t] = true
			}
		}
	}

	n := float64(len(filenames))
	var scored []fileHintScoredFile
	for i, name := range filenames {
		var total float64
		var matches []string
		for _, token := range tokenized[i] {
			if weight, ok := keywords[token]; ok {
				idf := math.Log(1 + n/float64(1+docFreq[token]))
				total += float64(weight) * idf
				matches = append(matches, token)
			}
		}
		if ext := fileHintLanguageExt(userPrompt); ext != "" && strings.HasSuffix(strings.ToLower(name), ext) {
			total += 0.5
		}
		if total > 0 {
			scored = append(scored, fileHintScoredFile{name: name, score: total, matches: fileHintDedupe(matches)})
		}
	}

	sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	return scored
}

// fileHintExtractKeywords returns the prompt's significant term frequencies (length > 1, not a stop
// word), the keys ScoreFiles matches path tokens against (apogee-sim filehint.ExtractKeywords @pin).
func fileHintExtractKeywords(text string) map[string]int {
	freq := make(map[string]int)
	for _, w := range fileHintTokenizeText(text) {
		if len(w) > 1 && !fileHintStopWords[w] {
			freq[w]++
		}
	}
	return freq
}

// fileHintBuild formats the compact hint message from the top scored files (apogee-sim
// filehint.BuildHint @pin). The lead is fileHintMarker so the idempotency guard can recognise it.
func fileHintBuild(files []fileHintScoredFile, limit int) string {
	if len(files) > limit {
		files = files[:limit]
	}
	var b strings.Builder
	b.WriteString(fileHintMarker)
	b.WriteString(" to your task:\n")
	for _, f := range files {
		if len(f.matches) > 0 {
			fmt.Fprintf(&b, "- %s (matches: %s)\n", f.name, strings.Join(f.matches, ", "))
		} else {
			fmt.Fprintf(&b, "- %s\n", f.name)
		}
	}
	b.WriteString("Consider reading these files first with read_file.")
	return b.String()
}

// fileHintTokenizeText lower-cases text into alphanumeric tokens, additionally emitting the
// camelCase sub-words of each token (apogee-sim tokenizeText @pin).
func fileHintTokenizeText(text string) []string {
	var tokens []string
	for _, w := range strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		lower := strings.ToLower(w)
		tokens = append(tokens, lower)
		for _, sub := range fileHintSplitCamel(w) {
			if s := strings.ToLower(sub); s != lower {
				tokens = append(tokens, s)
			}
		}
	}
	return tokens
}

// fileHintTokenizePath splits a path on separators and dots into lower-cased tokens, additionally
// emitting each part's camelCase sub-words (apogee-sim tokenizePath @pin).
func fileHintTokenizePath(path string) []string {
	var tokens []string
	for _, part := range strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '.' || r == '_' || r == '-'
	}) {
		lower := strings.ToLower(part)
		tokens = append(tokens, lower)
		for _, sub := range fileHintSplitCamel(part) {
			if s := strings.ToLower(sub); s != lower {
				tokens = append(tokens, s)
			}
		}
	}
	return tokens
}

// fileHintSplitCamel splits a token at lower→upper case boundaries (apogee-sim filehint
// splitCamelCase @pin — note this variant keeps the original case of each sub-word; callers
// lower-case as needed).
func fileHintSplitCamel(s string) []string {
	var parts []string
	start := 0
	runes := []rune(s)
	for i := 1; i < len(runes); i++ {
		if unicode.IsUpper(runes[i]) && unicode.IsLower(runes[i-1]) {
			parts = append(parts, string(runes[start:i]))
			start = i
		}
	}
	return append(parts, string(runes[start:]))
}

// fileHintLangPatterns map a language named in the prompt to its file extension (apogee-sim
// langPatterns @pin) — a filename with that extension gets a small relevance boost.
var fileHintLangPatterns = []struct {
	keyword string
	ext     string
}{
	{"javascript", ".js"}, {"typescript", ".ts"}, {"python", ".py"},
	{"golang", ".go"}, {" go ", ".go"}, {"rust", ".rs"}, {"java", ".java"},
	{"ruby", ".rb"}, {"c++", ".cpp"}, {"csharp", ".cs"}, {"c#", ".cs"},
}

// fileHintLanguageExt returns the file extension of the first language named in text, or "" —
// apogee-sim detectLanguageKeyword @pin (renamed to avoid colliding with the package's existing
// syntax-check detectLanguage).
func fileHintLanguageExt(text string) string {
	lower := strings.ToLower(text)
	for _, lp := range fileHintLangPatterns {
		if strings.Contains(lower, lp.keyword) {
			return lp.ext
		}
	}
	return ""
}

// fileHintDedupe returns ss with duplicates removed, preserving first-seen order (apogee-sim
// dedupe @pin).
func fileHintDedupe(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// fileHintStopWords are the low-signal English tokens dropped before scoring (apogee-sim filehint
// stopWords @pin) — the larger set filehint uses, distinct from toolfilter's.
var fileHintStopWords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
	"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
	"with": true, "by": true, "from": true, "as": true, "is": true, "was": true,
	"are": true, "be": true, "been": true, "being": true, "have": true, "has": true,
	"had": true, "do": true, "does": true, "did": true, "will": true, "would": true,
	"could": true, "should": true, "may": true, "might": true, "shall": true,
	"can": true, "need": true, "must": true, "it": true, "its": true, "this": true,
	"that": true, "these": true, "those": true, "i": true, "me": true, "my": true,
	"we": true, "our": true, "you": true, "your": true, "he": true, "she": true,
	"they": true, "them": true, "their": true, "what": true, "which": true, "who": true,
	"when": true, "where": true, "how": true, "not": true, "no": true, "nor": true,
	"if": true, "then": true, "else": true, "so": true, "up": true, "out": true,
	"just": true, "also": true, "than": true, "too": true, "very": true, "all": true,
	"each": true, "every": true, "both": true, "few": true, "more": true, "most": true,
	"other": true, "some": true, "such": true, "only": true, "own": true, "same": true,
	"about": true, "into": true, "over": true, "after": true, "before": true,
	"between": true, "under": true, "again": true, "there": true, "here": true,
	"please": true, "make": true, "use": true, "file": true, "code": true,
}
