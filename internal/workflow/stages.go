package workflow

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
)

// The engine's own briefs for the verify and merge stages. Like the Floor guards' text
// (internal/floor/prompts.go) they are asset files rather than Go literals, so the wording reads
// and edits as prose; go:embed compiles them into the binary, and no plan or recipe overrides
// them — an author's brief only follows the engine's.
//
//go:embed briefs/*.txt
var briefFS embed.FS

// The engine briefs, loaded once.
var (
	verifyLead = mustBrief("verify.txt")
	mergeLead  = mustBrief("merge.txt")
)

// The engine-brief placeholders beside {item} and {out}: the claim a verify child is to refute,
// the output path of the item it checks, and the manifest a merge child reads.
const (
	placeholderClaim    = "{claim}"
	placeholderOutput   = "{output}"
	placeholderManifest = "{manifest}"
)

// The names a merge stage writes in the workflow folder: the report at the top, and each merge
// stage's manifest under stages/<stage name>/.
const (
	reportName       = "report.md"
	stagesDirName    = "stages"
	manifestFileName = "manifest.md"
)

// VerdictField is the receipt field a verify child hands its verdict back in.
const VerdictField = "verdict"

// Verdict is a verify child's judgement of the item it checked.
type Verdict string

// The three verdicts. A verify child that ended blocked, or without a readable verdict, counts as
// unclear; an item no verify stage checked has none.
const (
	VerdictConfirmed Verdict = "confirmed"
	VerdictRefuted   Verdict = "refuted"
	VerdictUnclear   Verdict = "unclear"
)

// mustBrief loads one embedded engine brief by file name, its one trailing newline stripped and
// CRLF endings normalised. A missing name fails the build through go:embed first, so it is a
// programming error rather than a runtime condition.
func mustBrief(name string) string {
	data, err := briefFS.ReadFile("briefs/" + name)
	if err != nil {
		panic("apogee: missing embedded workflow brief " + name + ": " + err.Error())
	}
	return strings.TrimSuffix(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
}

// verifyReturns is the receipt a verify child hands back: the core status and summary plus the
// verdict. It is the engine's, never the author's — Validate refuses `returns:` on a verify stage.
func verifyReturns() ReceiptSpec {
	return ReceiptSpec{VerdictField: strings.Join(
		[]string{string(VerdictConfirmed), string(VerdictRefuted), string(VerdictUnclear)}, "|",
	)}
}

// runStage runs plan's stage at index by its kind. result holds the stages already run; a verify
// folds its verdicts into its source fanout's items there, and a merge sets the report fields.
func (s *runState) runStage(ctx context.Context, plan Plan, index int, stageItems map[int][]Item, result *Result) (StageResult, error) {
	stage := plan.Stages[index]
	switch stage.Kind {
	case StageVerify:
		return s.runVerify(ctx, plan, index, stageItems, result)
	case StageMerge:
		return s.runMerge(ctx, plan, index, result)
	default:
		return s.runFanout(ctx, index, stage, stageItems[index])
	}
}

// runVerify runs one adversarial child per item of the source fanout that the stage's `when:`
// selects (every finished item when it has none), through the fanout's wave path. Each child's
// brief leads with the engine's verify brief — refute the item's claim — and its receipt carries
// the verdict, which is folded into the source item's result and both stages' tallies.
func (s *runState) runVerify(ctx context.Context, plan Plan, index int, stageItems map[int][]Item, result *Result) (StageResult, error) {
	stage := plan.Stages[index]
	sourceIndex, source, err := sourceStage(plan, index, result)
	if err != nil {
		return StageResult{}, err
	}
	selected, err := selectForVerify(stage, source.Items)
	if err != nil {
		return StageResult{}, err
	}
	child := stage
	child.Returns = verifyReturns()
	keyBrief, err := stageKeyBrief(child)
	if err != nil {
		return StageResult{}, err
	}

	items := stageItems[sourceIndex]
	drafts := make([]itemDraft, len(selected))
	for position, at := range selected {
		drafts[position] = verifyDraft(keyBrief, items[at], source.Items[at])
	}
	results, err := s.runItems(ctx, index, child, drafts)
	if err != nil {
		return StageResult{}, err
	}

	for position, at := range selected {
		results[position].Verdict = verdictOf(results[position])
		source.Items[at].Verdict = results[position].Verdict
	}
	source.Tally = tallyOf(source.Items)
	return s.endStage(ctx, index, child, results, PhaseDone)
}

// runMerge runs one child over a manifest of every source item — its receipt, verdict and output
// path — written into the workflow folder, and hands it report.md there as its output. A merge
// that leaves no report (its child blocked or stopped, or claimed a report it never wrote) ends
// the stage failed and says why in result.ReportMissing; the items' results are untouched.
func (s *runState) runMerge(ctx context.Context, plan Plan, index int, result *Result) (StageResult, error) {
	stage := plan.Stages[index]
	_, source, err := sourceStage(plan, index, result)
	if err != nil {
		return StageResult{}, err
	}
	store, id := s.runner.Store, s.status.ID
	manifest := renderManifest(source.Items)
	manifestName := stagesDirName + "/" + stage.Name + "/" + manifestFileName
	if err := store.WriteFile(id, manifestName, []byte(manifest)); err != nil {
		return StageResult{}, err
	}
	manifestPath, err := store.Path(id, manifestName)
	if err != nil {
		return StageResult{}, err
	}
	reportPath, err := store.Path(id, reportName)
	if err != nil {
		return StageResult{}, err
	}
	keyBrief, err := stageKeyBrief(stage)
	if err != nil {
		return StageResult{}, err
	}

	draft := itemDraft{
		item:     Item{Label: stage.Name, Units: []string{manifestPath}},
		keyBrief: keyBrief + "\n" + manifest,
		lead: func(output string) string {
			return strings.NewReplacer(placeholderManifest, manifestPath, placeholderOut, output).Replace(mergeLead)
		},
		output: reportPath,
	}
	results, err := s.runItems(ctx, index, stage, []itemDraft{draft})
	if err != nil {
		return StageResult{}, err
	}
	missing, err := reportMissing(results[0], reportPath)
	if err != nil {
		return StageResult{}, err
	}
	finished := PhaseDone
	if missing == "" {
		result.Report = reportPath
	} else {
		result.ReportMissing = missing
		finished = PhaseFailed
	}
	return s.endStage(ctx, index, stage, results, finished)
}

// sourceStage returns the fanout a verify or merge stage at index works over: its index in the
// plan and its result among the stages already run.
func sourceStage(plan Plan, index int, result *Result) (int, *StageResult, error) {
	stage := plan.Stages[index]
	source, found := sourceFanout(plan.Stages, index)
	if !found {
		return 0, nil, fmt.Errorf("workflow: stage %q: no fanout comes before it", stage.Name)
	}
	sourceIndex := slices.IndexFunc(plan.Stages, func(candidate Stage) bool { return candidate.Name == source.Name })
	resultIndex := slices.IndexFunc(result.Stages, func(candidate StageResult) bool { return candidate.Name == source.Name })
	if resultIndex < 0 {
		return 0, nil, fmt.Errorf("workflow: stage %q: its fanout %q has not run", stage.Name, source.Name)
	}
	return sourceIndex, &result.Stages[resultIndex], nil
}

// selectForVerify returns the indexes of the items a verify stage checks: those that finished with
// a receipt and satisfy its `when:`, or every finished one when it has none. An item a cancel left
// unfinished has no claim to check.
func selectForVerify(stage Stage, items []ItemResult) ([]int, error) {
	var cond *Cond
	if strings.TrimSpace(stage.When) != "" {
		parsed, err := ParseCond(stage.When)
		if err != nil {
			return nil, fmt.Errorf("workflow: stage %q, field \"when\": %w", stage.Name, err)
		}
		cond = &parsed
	}
	var selected []int
	for index, item := range items {
		if item.Phase != PhaseDone || item.Receipt == nil {
			continue
		}
		if cond != nil && !cond.Eval(*item.Receipt) {
			continue
		}
		selected = append(selected, index)
	}
	return selected, nil
}

// verifyDraft is the verify child's draft for one source item. Its key covers the source item's
// key and claim as well as the verify stage, so a source item redone with a new receipt is checked
// again; its lead is the engine's verify brief rendered with the item and its claim.
func verifyDraft(keyBrief string, item Item, source ItemResult) itemDraft {
	claim := renderClaim(*source.Receipt)
	return itemDraft{
		item:     item,
		keyBrief: keyBrief + "\n" + source.Key + "\n" + claim,
		lead: func(output string) string {
			return strings.NewReplacer(
				placeholderItem, strings.Join(item.Units, ", "),
				placeholderClaim, claim,
				placeholderOutput, source.Output,
				placeholderOut, output,
			).Replace(verifyLead)
		},
	}
}

// renderClaim writes a receipt out as the claim a verify child reads: status, summary, then the
// typed fields by name, one `name: value` line each.
func renderClaim(receipt Receipt) string {
	lines := []string{
		FieldStatus + ": " + string(receipt.Status),
		FieldSummary + ": " + receipt.Summary,
	}
	for _, name := range sortedKeys(receipt.Fields) {
		lines = append(lines, name+": "+fieldText(receipt.Fields[name]))
	}
	return strings.Join(lines, "\n")
}

// fieldText renders one receipt field value: a string as written, anything else as JSON (a list
// reads ["a","b"]), falling back to Go's formatting for a value JSON cannot encode.
func fieldText(value any) string {
	if text, isString := value.(string); isString {
		return text
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(encoded)
}

// verdictOf reads a verify item's verdict off its receipt: unclear when the child blocked or sent
// no readable verdict, none when a cancel left the item unfinished.
func verdictOf(item ItemResult) Verdict {
	if item.Phase != PhaseDone || item.Receipt == nil {
		return ""
	}
	value, _ := item.Receipt.Fields[VerdictField].(string)
	switch verdict := Verdict(value); verdict {
	case VerdictConfirmed, VerdictRefuted:
		return verdict
	default:
		return VerdictUnclear
	}
}

// renderManifest lists every source item for a merge child, one line each in item order: number,
// label, status and summary (or unfinished), the verdict when a verify gave one, and the output
// path.
func renderManifest(items []ItemResult) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# Manifest — %d items\n\n", len(items))
	for index, item := range items {
		fmt.Fprintf(&builder, "- #%d %s — ", index+1, item.Label)
		if item.Phase == PhaseDone && item.Receipt != nil {
			fmt.Fprintf(&builder, "%s — %s", item.Receipt.Status, item.Receipt.Summary)
		} else {
			builder.WriteString("unfinished")
		}
		if item.Verdict != "" {
			fmt.Fprintf(&builder, " — verdict: %s", item.Verdict)
		}
		fmt.Fprintf(&builder, " — output: %s\n", item.Output)
	}
	return builder.String()
}

// reportMissing says why a merge left no report at path, or "" when it wrote one: its child
// stopped, blocked, or finished without the file existing.
func reportMissing(item ItemResult, path string) (string, error) {
	if item.Phase != PhaseDone || item.Receipt == nil {
		return "the merge stopped before it wrote the report", nil
	}
	if item.Receipt.Status == StatusBlocked {
		return "the merge child blocked: " + item.Receipt.Summary, nil
	}
	_, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Sprintf("the merge child finished %s but wrote no report at %s", item.Receipt.Status, path), nil
	}
	if err != nil {
		return "", fmt.Errorf("workflow: read the merge report %q: %w", path, err)
	}
	return "", nil
}
