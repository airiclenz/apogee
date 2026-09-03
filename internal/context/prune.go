package context

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Stale-tool-result pruning — the pure policy (context/doc.go)
// ----------------------------------------------------------------------------
//
// Pruning is the cheap, NON-generative half of the reducer pair: where Compaction spends an
// upstream call to summarize the whole history, a prune rewrites individual stale tool results
// into a one-line stub in place, costing nothing and losing nothing the model cannot re-fetch
// with the call the stub names. The two are complementary, and the caller runs this one first
// (internal/agent): a conversation drowning in old file dumps is relieved without a summary,
// and Compaction stays for the case where the CONVERSATION, not its tool output, is the bulk.
//
// Everything here is pure: Prune reads the Budget for its trigger and rewrites the
// Conversation it is handed, and does no I/O, no events and no config reading. The driving
// decisions — when a Turn boundary is quiescent enough to prune, and what the user is told —
// belong to the engine.

const (
	// pruneHighFraction is the fill at which pruning starts and pruneLowFraction the fill it
	// stops at, both as a fraction of the Budget's History allocation. The band is wide on
	// purpose: a prune rewrites committed history, which invalidates the upstream server's
	// prefix cache (ADR 0023 §6), so the policy trades a rare, larger reclaim for the frequent
	// small ones a single threshold would produce.
	pruneHighFraction = 0.6
	pruneLowFraction  = 0.4

	// PruneKeepTurns is how many of the most recent tool-calling Turns are never pruned. The
	// freshest results are what the model is actively reasoning over; stubbing those would
	// break the Exchange in progress rather than relieve it.
	PruneKeepTurns = 4

	// pruneArgMaxChars bounds the argument echoed in a stub, so one pathological call (a long
	// pattern, a here-doc command) cannot spend more context than the result it replaced.
	pruneArgMaxChars = 80
)

const (
	// pruneStubPrefix opens every stub and is also how an already-pruned result is recognised:
	// a second pass must never re-prune (and re-count) its own work.
	pruneStubPrefix = "[pruned:"

	// pruneStubFormat is the ONE spelling of the stub wording — the size lost, where it came
	// from, and the recovery the model is expected to make. The origin verb phrase is
	// pre-rendered by pruneStub, which drops it (and its leading space) when the owning call
	// cannot be resolved.
	pruneStubFormat = pruneStubPrefix + " %d lines%s — re-run the call if you need it]"
)

// PruneResult reports what a Prune pass did: how many tool results were stubbed and how many
// characters that reclaimed. The zero value is "nothing was pruned" — the caller emits its
// user-facing notice only on a non-zero Pruned, and converts Chars to tokens through the same
// Budget the trigger used.
type PruneResult struct {
	Pruned int
	Chars  int
}

// Prune rewrites stale tool results in conv into one-line stubs until its history is back under
// pruneLowFraction of the Budget's History allocation, and reports what it reclaimed.
//
// It is a no-op — the zero PruneResult, conv untouched — when the context window is unknown
// (a non-positive History: there is no basis to bound anything), when the history is still
// under pruneHighFraction, when fewer than keepTurns tool-calling Turns have happened, or when
// every eligible result is already a stub.
//
// What it never touches: the protected prefix (conv.PrefixEnd — the system messages and the
// opening user message), any message that is not a tool result, and every tool result belonging
// to the most recent keepTurns tool-calling Turns. Within what remains, the oldest Turn goes
// first and the largest result within a Turn goes first, so the fewest possible rewrites buy
// the most room and the loss lands furthest from what the model is working on. Message
// positions never move: the rewrite is in place, so the assistant/tool adjacency strict chat
// templates require survives, and a caller mid-Exchange keeps every index it holds.
func Prune(conv *domain.Conversation, b domain.Budget, keepTurns int) PruneResult {
	if conv == nil || b.History <= 0 {
		return PruneResult{}
	}
	if !b.HistoryExceedsFraction(conv.Messages(), pruneHighFraction) {
		return PruneResult{}
	}
	protected, ok := pruneProtectedIndex(conv, keepTurns)
	if !ok {
		return PruneResult{}
	}

	var result PruneResult
	for _, c := range pruneCandidates(conv, protected) {
		before := len(conv.At(c.index).Content)
		conv.SetMessageContent(c.index, c.stub)
		result.Pruned++
		result.Chars += before - len(c.stub)
		if !b.HistoryExceedsFraction(conv.Messages(), pruneLowFraction) {
			break
		}
	}
	return result
}

// pruneCandidate is one prunable tool result: where it sits, which Turn owns it, and the stub
// it will be replaced with. The stub is rendered up front because resolving the owning call is
// the same backward scan that assigns the Turn.
type pruneCandidate struct {
	index int
	turn  int
	chars int
	stub  string
}

// pruneProtectedIndex is the index of the keepTurns-th most recent assistant message carrying
// tool calls — the start of the protected recent window, clamped up to conv.PrefixEnd so the
// protected prefix is never a candidate. It generalises internal/floor's
// mostRecentToolCallTurn (keepTurns == 1) with one deliberate difference: where that helper
// protects NOTHING when the conversation holds too few tool-calling Turns, this one reports
// ok == false, because "the most recent keepTurns Turns are never touched" cannot be honoured
// by pruning the only Turns there are.
func pruneProtectedIndex(conv *domain.Conversation, keepTurns int) (int, bool) {
	if keepTurns <= 0 {
		return 0, false
	}
	seen := 0
	for i := conv.Len() - 1; i >= 0; i-- {
		m := conv.At(i)
		if m.Role != domain.RoleAssistant || len(m.ToolCalls) == 0 {
			continue
		}
		seen++
		if seen == keepTurns {
			if prefix := conv.PrefixEnd(); i < prefix {
				return prefix, true
			}
			return i, true
		}
	}
	return 0, false
}

// pruneCandidates collects every prunable tool result before protected, in the order they are
// to be rewritten: oldest Turn first, and largest result first within a Turn (ties broken by
// position, so the order is total and the pass is deterministic). A result already carrying a
// stub is not a candidate.
func pruneCandidates(conv *domain.Conversation, protected int) []pruneCandidate {
	var candidates []pruneCandidate
	for i := conv.PrefixEnd(); i < protected; i++ {
		m := conv.At(i)
		if m.Role != domain.RoleTool || strings.HasPrefix(m.Content, pruneStubPrefix) {
			continue
		}
		turn, call, found := pruneOwningCall(conv, i, m.ToolCallID)
		tool, arg := "", ""
		if found {
			tool, arg = call.Tool, pruneArgument(call.Arguments)
		}
		candidates = append(candidates, pruneCandidate{
			index: i,
			turn:  turn,
			chars: len(m.Content),
			stub:  pruneStub(pruneLineCount(m.Content), tool, arg),
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].turn != candidates[j].turn {
			return candidates[i].turn < candidates[j].turn
		}
		if candidates[i].chars != candidates[j].chars {
			return candidates[i].chars > candidates[j].chars
		}
		return candidates[i].index < candidates[j].index
	})
	return candidates
}

// pruneOwningCall resolves the tool result at resultIndex back to the ToolCall that produced
// it — the name and arguments live only on the call, never on the result. The scan walks back
// to the assistant message whose ToolCalls hold callID; failing that (a result with no id, or
// one whose call was folded away by an earlier reducer) it reports the nearest preceding
// tool-calling assistant message as the Turn and found == false, and the stub then names no
// tool rather than guessing one.
func pruneOwningCall(conv *domain.Conversation, resultIndex int, callID string) (int, domain.ToolCall, bool) {
	turn := 0
	haveTurn := false
	for i := resultIndex - 1; i >= 0; i-- {
		m := conv.At(i)
		if m.Role != domain.RoleAssistant || len(m.ToolCalls) == 0 {
			continue
		}
		if !haveTurn {
			turn, haveTurn = i, true
		}
		if callID == "" {
			continue
		}
		for _, call := range m.ToolCalls {
			if call.ID == callID {
				return i, call, true
			}
		}
	}
	return turn, domain.ToolCall{}, false
}

// pruneArgument is the one argument echoed in a stub: the first present of the call's path,
// pattern, query or command, trimmed to pruneArgMaxChars. Those four are what identifies a
// re-runnable call to a reader — a path to re-read, a pattern or query to re-search, a command
// to re-run — and any other argument would cost context without telling the model where the
// result came from. Unparseable arguments yield no argument rather than an error: the stub is
// a hint, not a contract.
func pruneArgument(arguments json.RawMessage) string {
	var args struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
		Query   string `json:"query"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(arguments, &args); err != nil {
		return ""
	}
	for _, value := range []string{args.Path, args.Pattern, args.Query, args.Command} {
		if value == "" {
			continue
		}
		if runes := []rune(value); len(runes) > pruneArgMaxChars {
			return string(runes[:pruneArgMaxChars])
		}
		return value
	}
	return ""
}

// pruneStub renders the replacement for one tool result. It is the only place the stub wording
// is assembled: the origin phrase is dropped whole — with its leading space — when the owning
// call is unknown, and the argument alone is dropped when the call carried none.
func pruneStub(lines int, tool, arg string) string {
	origin := ""
	switch {
	case tool != "" && arg != "":
		origin = " from " + tool + " " + arg
	case tool != "":
		origin = " from " + tool
	}
	return fmt.Sprintf(pruneStubFormat, lines, origin)
}

// pruneLineCount is how many lines a result held, as the stub reports it — the size the model
// is told it lost. Empty content is 0 lines, not the 1 a naive split would report.
func pruneLineCount(content string) int {
	if content == "" {
		return 0
	}
	return strings.Count(content, "\n") + 1
}
