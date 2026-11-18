package tui

import (
	"path/filepath"
	"strconv"
	"strings"
)

// groupPhrase is the verb phrase a tool's calls are counted under in an umbrella's Group summary:
// "read 3 files", "ran 2 commands", "checked git". It is presenter knowledge (toolPresenter.group),
// so a tool says how its work reads in a sentence in the same registry row that says how its block
// reads; the summary merges calls by this VALUE, never by label, which is what lets grep and
// find_files read as one "searched N patterns" and the four writing tools as one "edited N files".
//
// past is the verb a settled phrase reads with and live the present participle it takes while any
// of its calls is still in flight. nounForOne and nounForMany are the two spellings of what it
// counts. distinct counts the distinct path arguments of its calls rather than the calls themselves
// (three reads of one file read 1 file); uncounted says the phrase names its work without a number
// at all ("checked git"), and then nounForOne is the whole object of the verb.
type groupPhrase struct {
	past        string
	live        string
	nounForOne  string
	nounForMany string
	distinct    bool
	uncounted   bool
}

// The phrases the registry rows point at. Each is one value shared by every tool that does that
// kind of work, because equality of the value is what merges their calls into one phrase. There is
// no "created": a write and an edit both read as "edited" (owner, 2026-11-18).
var (
	readFilesPhrase        = groupPhrase{past: "read", live: "reading", nounForOne: "file", nounForMany: "files", distinct: true}
	searchedPatternsPhrase = groupPhrase{past: "searched", live: "searching", nounForOne: "pattern", nounForMany: "patterns"}
	ranCommandsPhrase      = groupPhrase{past: "ran", live: "running", nounForOne: "command", nounForMany: "commands"}
	editedFilesPhrase      = groupPhrase{past: "edited", live: "editing", nounForOne: "file", nounForMany: "files", distinct: true}
	fetchedPagesPhrase     = groupPhrase{past: "fetched", live: "fetching", nounForOne: "page", nounForMany: "pages"}
	listedDirsPhrase       = groupPhrase{past: "listed", live: "listing", nounForOne: "dir", nounForMany: "dirs", distinct: true}
	copiedFilesPhrase      = groupPhrase{past: "copied", live: "copying", nounForOne: "file", nounForMany: "files"}
	movedFilesPhrase       = groupPhrase{past: "moved", live: "moving", nounForOne: "file", nounForMany: "files"}
	deletedFilesPhrase     = groupPhrase{past: "deleted", live: "deleting", nounForOne: "file", nounForMany: "files"}
	checkedGitPhrase       = groupPhrase{past: "checked", live: "checking", nounForOne: "git", uncounted: true}
	madeCommitsPhrase      = groupPhrase{past: "made", live: "making", nounForOne: "commit", nounForMany: "commits"}
)

// The Group summary's own words. groupPathArg is the argument a distinct phrase counts by — every
// distinct-flagged tool names its file or directory under it. groupLabelCountMark is what a tool
// with no phrase is counted with (`Fetch ×2`, `github · search ×1`); groupMoreLead and groupMoreTail
// close a list cut down to its first groupKeptPhrases phrases ("… and 2 more"); the two outcome nouns
// count the calls that failed and the calls the approver or a reaction refused.
const (
	groupPathArg        = "path"
	groupLabelCountMark = " ×"
	groupKeptPhrases    = 2
	groupMoreLead       = " and "
	groupMoreTail       = " more"
	groupFailedNoun     = "failed"
	groupDeniedNoun     = "denied"
)

// groupCall is one member of an umbrella as the Group summary reads it: the call's view and
// whether its result has landed (entry.done). The done state is what picks a phrase's live verb,
// and it rides beside the view because the view itself carries no such fact.
type groupCall struct {
	view toolView
	done bool
}

// groupTone is what a segment of the Group summary is painted as. The summary states it and paints
// nothing, so the painter maps each tone onto the scheme's styles: the phrases in the header's own
// text tone, the count fallback as the `Tools (N calls)` header it has always been, the diffstat's
// two halves in the diff colours, and the failed/denied counts in the failure red.
type groupTone int

const (
	groupTonePhrase groupTone = iota
	groupToneCalls
	groupToneAdded
	groupToneRemoved
	groupToneOutcome
)

// groupSegment is one run of the Group summary's text in one tone. A segment after the first
// carries its own leading separator, so the segments concatenate into the summary's text as they
// stand.
type groupSegment struct {
	text string
	tone groupTone
}

// groupSummary is an umbrella header's text as styled segments, in reading order: the head (verb
// phrases, or the count fallback), then the diffstat's two halves, then the failed and denied
// counts. Any but the head may be absent.
type groupSummary []groupSegment

// text is the summary as it is measured and laid out: its segments joined as they stand.
func (s groupSummary) text() string {
	var b strings.Builder
	for _, seg := range s {
		b.WriteString(seg.text)
	}
	return b.String()
}

// buildGroupSummary words an umbrella's calls as its Group summary at a width budget (cells):
// "Read 3 files, ran 2 commands, edited 2 files +18 −5 · 1 failed". The phrases follow the order
// each one first ran, the first word capitalised; a phrase with a call in flight takes its live
// verb ("Reading 2 files"). The diffstat totals the edit phrase's diffed members alone, and the
// failed and denied counts stand apart, a refused call counting as denied and never as failed.
//
// Where the whole sentence does not fit, the fallback spends detail in a fixed order: the phrases
// after the first two give way to "and K more", then the diffstat goes, then the head becomes
// today's `Tools (N calls)`, and last that head is clipped. The outcome counts survive every step
// but the clip, and the clip spends the head before them. Nil for no calls or no room.
func buildGroupSummary(th theme, calls []groupCall, budget int) groupSummary {
	if len(calls) == 0 || budget < 1 {
		return nil
	}
	outcome := groupOutcome(calls)
	for _, candidate := range groupCandidates(tallyPhrases(calls), groupDiffstat(calls), len(calls)) {
		summary := append(candidate, outcome...)
		if th.measure.Width(summary.text()) <= budget {
			return summary
		}
	}
	return clipGroupSummary(th, groupCountHead(len(calls)), outcome, budget)
}

// groupCandidates is the fallback ladder above the clip, widest first: every phrase with the
// diffstat, the first two phrases and "and K more" with it, that shorter head without it, and the
// count fallback. A rung that would read exactly as the one above it is left out.
func groupCandidates(tallies []*phraseTally, diff groupSummary, calls int) []groupSummary {
	full := groupPhraseHead(tallies)
	short := full
	if len(tallies) > groupKeptPhrases {
		short = groupPhraseHead(tallies[:groupKeptPhrases])
		short.text += groupMoreLead + strconv.Itoa(len(tallies)-groupKeptPhrases) + groupMoreTail
	}
	candidates := []groupSummary{append(groupSummary{full}, diff...)}
	if short != full {
		candidates = append(candidates, append(groupSummary{short}, diff...))
	}
	if len(diff) > 0 {
		candidates = append(candidates, groupSummary{short})
	}
	return append(candidates, groupSummary{groupCountHead(calls)})
}

// groupPhraseHead joins the phrases into the head segment, capitalising the first word when it is a
// verb. A tool label leading the list keeps its own spelling: it is a name, not a word of the
// sentence.
func groupPhraseHead(tallies []*phraseTally) groupSegment {
	words := make([]string, len(tallies))
	for i, t := range tallies {
		words[i] = t.spell()
	}
	if len(tallies) > 0 && tallies[0].phrase != (groupPhrase{}) {
		words[0] = strings.ToUpper(words[0][:1]) + words[0][1:]
	}
	return groupSegment{text: strings.Join(words, superKindSeparator), tone: groupTonePhrase}
}

// groupCountHead is the last-resort head, today's umbrella header text: "Tools (7 calls)".
func groupCountHead(calls int) groupSegment {
	return groupSegment{text: superGroupLabel + " (" + plural(calls, superCallNoun) + ")", tone: groupToneCalls}
}

// clipGroupSummary is the fallback's last step: the head clipped into whatever the outcome counts
// leave of the budget, so a failure is the last thing to go. Where the counts leave no room for a
// clipped head at all, they are dropped and the head alone is clipped to the budget.
func clipGroupSummary(th theme, head groupSegment, outcome groupSummary, budget int) groupSummary {
	tail := th.measure.Width(clipTail)
	if room := budget - th.measure.Width(outcome.text()); len(outcome) > 0 && room > tail {
		clipped, _ := clipCells(th, head.text, room)
		return append(groupSummary{{text: clipped, tone: head.tone}}, outcome...)
	}
	if budget <= tail {
		return groupSummary{{text: th.measure.Truncate(head.text, budget, ""), tone: head.tone}}
	}
	clipped, _ := clipCells(th, head.text, budget)
	return groupSummary{{text: clipped, tone: head.tone}}
}

// groupDiffstat is the summed `+A −R` of the edit phrase's members whose slot carries a diffstat —
// the two halves as their own segments, in the house spelling (diffCounts). Every other member is
// skipped rather than summed: a write's line count, a multi-replace's "N changes" and a failed
// edit's verdict carry no diffstat, and sumStats gives up on the first value that does not add.
func groupDiffstat(calls []groupCall) groupSummary {
	var diffed []toolView
	for _, call := range calls {
		if call.view.Summary.stat.kind == statDiffed && groupKeyOf(call.view).phrase == editedFilesPhrase {
			diffed = append(diffed, call.view)
		}
	}
	total, ok := sumStats(diffed)
	if !ok {
		return nil
	}
	added, removed, _ := strings.Cut(total.spell(), " ")
	return groupSummary{
		{text: " " + added, tone: groupToneAdded},
		{text: " " + removed, tone: groupToneRemoved},
	}
}

// groupOutcome is the summary's failure half: `· N failed` and `· N denied`, each present only
// when it counts something. A call the approver or a reaction refused carries the failure verdict
// on its slot as any failure does, and is counted as denied instead (toolView.denied).
func groupOutcome(calls []groupCall) groupSummary {
	failed, denied := 0, 0
	for _, call := range calls {
		switch {
		case call.view.denied || call.view.Summary.Text == deniedSummary:
			denied++
		case call.view.Summary.failed:
			failed++
		}
	}
	var out groupSummary
	if failed > 0 {
		out = append(out, groupSegment{text: slotSeparator + strconv.Itoa(failed) + " " + groupFailedNoun, tone: groupToneOutcome})
	}
	if denied > 0 {
		out = append(out, groupSegment{text: slotSeparator + strconv.Itoa(denied) + " " + groupDeniedNoun, tone: groupToneOutcome})
	}
	return out
}

// groupKey is what merges calls into one phrase of the summary: the presenter's phrase where the
// tool has one, and the call's label where it has none — an MCP tool, or a built-in no phrase
// covers.
type groupKey struct {
	phrase groupPhrase
	label  string
}

// groupKeyOf is the key a call is counted under.
func groupKeyOf(tv toolView) groupKey {
	if p, known := tv.presenter(); known && p.group != (groupPhrase{}) {
		return groupKey{phrase: p.group}
	}
	return groupKey{label: tv.Label}
}

// phraseTally is one phrase of the summary as its calls are counted into it: how many calls, the
// distinct paths a distinct phrase has seen, the calls that named none (each its own item, never
// zero), and whether any of its calls is still in flight.
type phraseTally struct {
	groupKey
	calls    int
	paths    map[string]bool
	pathless int
	live     bool
}

// tallyPhrases counts the calls into their phrases, in the order each phrase first ran.
func tallyPhrases(calls []groupCall) []*phraseTally {
	var order []*phraseTally
	byKey := make(map[groupKey]*phraseTally, len(calls))
	for _, call := range calls {
		key := groupKeyOf(call.view)
		t, seen := byKey[key]
		if !seen {
			t = &phraseTally{groupKey: key, paths: make(map[string]bool)}
			byKey[key] = t
			order = append(order, t)
		}
		t.add(call)
	}
	return order
}

// add counts one call into the tally.
func (t *phraseTally) add(call groupCall) {
	t.calls++
	if !call.done {
		t.live = true
	}
	if !t.phrase.distinct {
		return
	}
	if path, ok := callPath(call.view); ok {
		t.paths[path] = true
		return
	}
	t.pathless++
}

// items is the number the phrase spells: its distinct paths (and path-less calls) for a distinct
// phrase, its calls for every other.
func (t *phraseTally) items() int {
	if t.phrase.distinct {
		return len(t.paths) + t.pathless
	}
	return t.calls
}

// spell writes the tally as its phrase: "read 3 files", "running 1 command", "checked git", or a
// phraseless tool's `<Label> ×N`.
func (t *phraseTally) spell() string {
	if t.phrase == (groupPhrase{}) {
		return t.label + groupLabelCountMark + strconv.Itoa(t.calls)
	}
	verb := t.phrase.past
	if t.live {
		verb = t.phrase.live
	}
	if t.phrase.uncounted {
		return verb + " " + t.phrase.nounForOne
	}
	n := t.items()
	noun := t.phrase.nounForMany
	if n == 1 {
		noun = t.phrase.nounForOne
	}
	return verb + " " + strconv.Itoa(n) + " " + noun
}

// callPath is the path argument a call named, read off the record's own copy of its arguments
// (toolView.argsWire) — the presenter's parsed map is dropped for most tools — and never parsed back
// out of the rendered Target. A call whose arguments name none, or were elided for size, has none.
func callPath(tv toolView) (string, bool) {
	path, _ := decodeArgsPreservingNumbers(tv.argsWire)[groupPathArg].(string)
	if strings.TrimSpace(path) == "" {
		return "", false
	}
	return filepath.Clean(path), true
}
