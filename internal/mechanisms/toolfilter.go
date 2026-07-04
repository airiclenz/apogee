package mechanisms

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"github.com/airiclenz/apogee/internal/domain"
)

// toolfilter registers the tool-menu narrowing pre-request Mechanism in the catalogue constructor
// table (Phase-4 item 10, Wave 3 request shapers). Default-off (D1) — the config surface builds it
// only when the `mechanisms:` block enables it. It is ported from apogee-sim
// internal/toolfilter/toolfilter.go @pin: a relevance-scored reduction of the tool menu for small
// models, activating only when the menu is large (30+ tools) or the model has hallucinated a tool.
func init() { catalogue[toolFilterID] = newToolFilter }

const toolFilterID domain.MechanismID = "toolfilter"

// toolFilter thresholds mirror apogee-sim ToolFilter.Transform (`toolfilter.go:41-49` @pin):
// filtering activates only at toolFilterActivateThreshold tools (or on a hallucination) and never
// runs when the menu is already within toolFilterKeepLimit; a filtered menu keeps at most
// toolFilterKeepLimit tools.
const (
	toolFilterKeepLimit         = 10
	toolFilterActivateThreshold = 30
	toolFilterRecentTurns       = 3
)

// toolFilterStopWords are the low-signal tokens dropped from the user message before scoring
// (apogee-sim toolfilter stopWords @pin) — namespaced because internal/mechanisms is one package
// and filehint carries its own, larger stop-word set.
var toolFilterStopWords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "in": true,
	"to": true, "for": true, "of": true, "and": true, "or": true,
	"it": true, "this": true, "that": true, "my": true, "me": true,
	"i": true, "be": true, "do": true, "can": true, "you": true,
	"with": true, "on": true, "at": true, "from": true, "by": true,
	"please": true, "should": true, "would": true, "could": true,
}

// toolFilterAnalysisKeep are the read-only exploration tools kept regardless of keyword score when
// the user's request is analysis-focused (apogee-sim toolfilter.go:57-62 @pin, mapped to apogee's
// own tool names: list_dir/read_file/grep/open_file rather than the sim's list_files/readFile).
var toolFilterAnalysisKeep = map[string]bool{
	"list_dir": true, "list_files": true, "list_directory": true,
	"read_file": true, "open_file": true, "grep": true, "search_files": true,
}

// toolFilterMechanism is the pre-request Mechanism that narrows the tool menu (catalogue Table A
// `toolfilter`; ported from apogee-sim ToolFilter.Transform @pin). It scores the request's tools
// against the last user message's keywords, keeps the recently-used tools whole, and re-sets the
// menu to the top-scored subset via Request.SetTools — a request-scoped replacement (the loop
// rebuilds the full menu next Turn, so the narrowing never mutates the menu globally). It carries
// no per-Mechanism state: the descriptor's strikes-3 policy routes self-regulation through the
// loop's per-Session tracker (item 3).
type toolFilterMechanism struct{}

// newToolFilter builds the toolfilter Mechanism. It needs no injected Deps (D3): filtering reads
// the tool menu and conversation off the Request it is handed.
func newToolFilter(Deps) (domain.Mechanism, error) { return toolFilterMechanism{}, nil }

// Descriptor identifies toolfilter as a strikes-3 proactive-nudge Mechanism (catalogue Table A):
// disabled under Bypass (D5), withdrawn by self-regulation after repeated non-help.
func (toolFilterMechanism) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{
		ID:          toolFilterID,
		Capability:  domain.CapProactiveNudge,
		Suppression: domain.SuppressStrikesThree,
	}
}

// Ordering declares toolfilter Before decompose (catalogue Table A: "trim the menu before the
// user-message rewrite"). decompose lands in item 12; until then the edge names an absent
// Mechanism and MechanismRegistry.Ordered ignores it, so declaring it now is forward-compatible.
func (toolFilterMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{Before: []domain.MechanismID{"decompose"}}
}

// PreRequest narrows the tool menu when it is large or the model has hallucinated a tool, keeping
// the recently-used tools whole and the top-scored remainder up to the keep limit. It is a no-op —
// booking no fire (the loop keys acted fires on Request.Revision, R4) — when filtering is not
// warranted (menu below the threshold with no hallucination, or already within the limit), matching
// apogee-sim ToolFilter.Transform's Skip paths.
func (toolFilterMechanism) PreRequest(_ context.Context, req *domain.Request) error {
	tools := req.View().Tools()
	conv := req.View().Conversation()

	if len(tools) < toolFilterActivateThreshold && !toolFilterHasHallucinations(tools, conv) {
		return nil
	}
	if len(tools) <= toolFilterKeepLimit {
		return nil
	}

	lastUser := ""
	if msg, _, ok := conv.LastUser(); ok {
		lastUser = msg.Content
	}
	keywords := toolFilterExtractKeywords(lastUser)
	recent := toolFilterRecentlyUsed(conv)
	if hasAnalysisIntent(lastUser) {
		for _, t := range tools {
			if toolFilterAnalysisKeep[t.Name] {
				recent[t.Name] = true
			}
		}
	}

	kept := toolFilterSelect(toolFilterScore(tools, keywords), recent, toolFilterKeepLimit)
	req.SetTools(kept)
	return nil
}

// toolFilterHasHallucinations reports whether any assistant message in the conversation issued a
// tool call for a name absent from the current menu — a signal the model is inventing tools, which
// activates filtering even below the size threshold (apogee-sim hasToolHallucinations @pin).
func toolFilterHasHallucinations(tools []domain.ToolDef, conv domain.ConversationView) bool {
	available := make(map[string]bool, len(tools))
	for _, t := range tools {
		available[t.Name] = true
	}
	found := false
	conv.Range(func(_ int, m domain.Message) bool {
		if m.Role != domain.RoleAssistant {
			return true
		}
		for _, tc := range m.ToolCalls {
			if !available[tc.Tool] {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// toolFilterRecentlyUsed collects the tool names called in the last toolFilterRecentTurns
// assistant tool-call turns — the tools kept whole regardless of keyword score (apogee-sim
// recentlyUsedTools @pin), so the model keeps access to what it was just using.
func toolFilterRecentlyUsed(conv domain.ConversationView) map[string]bool {
	used := make(map[string]bool)
	turnsSeen := 0
	for i := conv.Len() - 1; i >= 0; i-- {
		m := conv.At(i)
		if m.Role == domain.RoleAssistant && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				used[tc.Tool] = true
			}
			turnsSeen++
			if turnsSeen >= toolFilterRecentTurns {
				break
			}
		}
	}
	return used
}

// toolFilterExtractKeywords lower-cases the user message and returns the significant tokens
// (length > 1, not a stop word) scoring keys (apogee-sim extractKeywords @pin).
func toolFilterExtractKeywords(text string) []string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	keywords := make([]string, 0, len(words))
	for _, w := range words {
		if len(w) > 1 && !toolFilterStopWords[w] {
			keywords = append(keywords, w)
		}
	}
	return keywords
}

// toolFilterScoredTool pairs a tool with its keyword-relevance score.
type toolFilterScoredTool struct {
	tool  domain.ToolDef
	score int
}

// toolFilterScore scores each tool by keyword relevance: an exact name match weighs most, a
// name-part match next, a description substring least (apogee-sim scoreTools @pin).
func toolFilterScore(tools []domain.ToolDef, keywords []string) []toolFilterScoredTool {
	scored := make([]toolFilterScoredTool, len(tools))
	for i, t := range tools {
		score := 0
		nameParts := toolFilterSplitName(t.Name)
		descLower := strings.ToLower(t.Description)
		for _, kw := range keywords {
			if strings.EqualFold(t.Name, kw) {
				score += 10
				continue
			}
			for _, part := range nameParts {
				if strings.EqualFold(part, kw) {
					score += 5
					break
				}
			}
			if strings.Contains(descLower, kw) {
				score += 2
			}
		}
		scored[i] = toolFilterScoredTool{tool: t, score: score}
	}
	return scored
}

// toolFilterSelect keeps every recently-used tool whole, then fills the remaining budget with the
// highest-scored candidates in a stable order (apogee-sim selectTools @pin). It preserves the input
// order of the must-keep tools and breaks score ties by original order (SortStableFunc), so the
// output is deterministic for a given menu.
func toolFilterSelect(scored []toolFilterScoredTool, recent map[string]bool, limit int) []domain.ToolDef {
	mustKeep := make([]domain.ToolDef, 0, len(scored))
	candidates := make([]toolFilterScoredTool, 0, len(scored))
	for _, st := range scored {
		if recent[st.tool.Name] {
			mustKeep = append(mustKeep, st.tool)
		} else {
			candidates = append(candidates, st)
		}
	}

	remaining := limit - len(mustKeep)
	if remaining <= 0 {
		return mustKeep
	}

	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	for i := 0; i < remaining && i < len(candidates); i++ {
		mustKeep = append(mustKeep, candidates[i].tool)
	}
	return mustKeep
}

// toolFilterSplitName splits a tool name on underscores and camelCase boundaries into lower-cased
// parts, so a keyword can match a component of a compound name (apogee-sim splitToolName @pin).
func toolFilterSplitName(name string) []string {
	var result []string
	for _, part := range strings.Split(name, "_") {
		result = append(result, toolFilterSplitCamel(part)...)
	}
	return result
}

// toolFilterSplitCamel splits a camelCase token into its lower-cased words (apogee-sim
// splitCamelCase @pin).
func toolFilterSplitCamel(s string) []string {
	var parts []string
	var current strings.Builder
	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) && current.Len() > 0 {
			parts = append(parts, strings.ToLower(current.String()))
			current.Reset()
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		parts = append(parts, strings.ToLower(current.String()))
	}
	return parts
}
