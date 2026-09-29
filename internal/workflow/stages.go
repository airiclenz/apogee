package workflow

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strconv"
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

// The three verdicts. A verify child that did not end ok (blocked or partial), or ended without a
// readable verdict, counts as unclear; an item no verify stage checked has none.
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

// runStage runs plan's stage at index, or skips it: when its `when:` is false, or when the stage
// it works over was skipped. result holds the stages already run, one per earlier stage in plan
// order; a verify folds its verdicts into its source fanout's items there, a merge sets the report
// fields, and a repeat replaces the result of the stage it re-runs.
func (s *runState) runStage(ctx context.Context, plan Plan, index int, stageItems map[int][]Item, result *Result) (StageResult, error) {
	note, err := skipReason(plan, index, result)
	if err != nil {
		return StageResult{}, err
	}
	if note != "" {
		return s.settleStage(index, plan.Stages[index], PhaseSkipped, note, nil)
	}
	return s.runRound(ctx, plan, index, 0, stageItems, result)
}

// runRound runs plan's stage at index by its kind, in the given repeat round (0 for its own run).
func (s *runState) runRound(ctx context.Context, plan Plan, index, round int, stageItems map[int][]Item, result *Result) (StageResult, error) {
	stage := plan.Stages[index]
	s.startRound(index, round)
	var (
		stageResult StageResult
		err         error
	)
	switch stage.Kind {
	case StageVerify:
		stageResult, err = s.runVerify(ctx, plan, index, round, stageItems, result)
	case StageMerge:
		stageResult, err = s.runMerge(ctx, plan, index, round, result)
	case StagePick:
		stageResult, err = s.runPick(plan, index, stageItems, result)
	case StageScript:
		stageResult, err = s.runScript(ctx, index, round, stage)
	case StageAsk:
		stageResult, err = s.runAsk(ctx, index, round, stage)
	case StageRepeat:
		stageResult, err = s.runRepeat(ctx, plan, index, stageItems, result)
	default:
		stageResult, err = s.runFanout(ctx, index, round, stage, fanoutItems(plan, index, stageItems))
	}
	stageResult.Round = round
	return stageResult, err
}

// runVerify runs one adversarial child per item of the source fanout that the stage's `when:`
// selects (every finished item when it has none), through the fanout's wave path. Each child's
// brief leads with the engine's verify brief — refute the item's claim — and its receipt carries
// the verdict, which is folded into the source item's result and both stages' tallies.
func (s *runState) runVerify(ctx context.Context, plan Plan, index, round int, stageItems map[int][]Item, result *Result) (StageResult, error) {
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
	keyBrief, err := stageKeyBrief(child, round, s.runner.promptSource())
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
func (s *runState) runMerge(ctx context.Context, plan Plan, index, round int, result *Result) (StageResult, error) {
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
	keyBrief, err := stageKeyBrief(stage, round, s.runner.promptSource())
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

// verdictOf reads a verify item's verdict off its receipt: unclear when the child did not end ok
// (blocked or partial, whatever verdict it wrote) or sent no readable verdict, none when a cancel
// left the item unfinished.
func verdictOf(item ItemResult) Verdict {
	if item.Phase != PhaseDone || item.Receipt == nil {
		return ""
	}
	if item.Receipt.Status != StatusOK {
		return VerdictUnclear
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

// ScriptRunner runs a script stage's command. The agent implements it under the Mode and approval
// rules of its own shell tool (ADR 0087 D6); tests script a fake. RunScript blocks until the
// command ends and honours ctx. An error is a command that could not run at all — refused, not
// found — which the stage records as a blocked receipt; a command that ran and failed is a
// non-zero ExitCode instead.
type ScriptRunner interface {
	RunScript(ctx context.Context, spec ScriptSpec) (ScriptOutput, error)
}

// ScriptSpec is one script stage's run: which workflow and stage it serves, the command as the
// recipe wrote it, and the workflow folder, where a script writes the files a later pick reads.
type ScriptSpec struct {
	Workflow string
	Stage    string
	Command  string
	Dir      string
}

// ScriptOutput is how a script ended: what it printed on stdout and its exit code.
type ScriptOutput struct {
	Stdout   string
	ExitCode int
}

// Asker puts an ask stage's question to the user and returns the answer. The Driver implements it;
// a Driver with no human passes none, and the stage takes its default (ADR 0087 D10). Ask blocks
// until the user answers and honours ctx.
type Asker interface {
	Ask(ctx context.Context, question Question) (string, error)
}

// Question is one ask stage's question: the stage it comes from, its text, the options to choose
// from (empty for a free answer) and the default an empty answer takes.
type Question struct {
	Workflow string
	Stage    string
	Text     string
	Options  []string
	Default  string
}

// noOneToAskNote is the note an ask stage's result carries when the Runner has no Asker.
const noOneToAskNote = "(default taken: no one to ask)"

// startRound marks the stage at index as running in the given repeat round, its earlier note
// cleared. The stage's next status write persists it.
func (s *runState) startRound(index, round int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	line := &s.status.Stages[index]
	line.Round, line.Note = round, ""
}

// settleStage records a stage that runs no child — skipped, a pick, a script, an ask, a repeat —
// at its final phase with its note, tells the Observer, and returns its result. A non-nil receipt
// is a script or ask stage's: it becomes the stage's one item, in the result and in status.json.
func (s *runState) settleStage(index int, stage Stage, phase Phase, note string, receipt *Receipt) (StageResult, error) {
	stageResult := StageResult{Name: stage.Name, Kind: stage.Kind, Phase: phase, Note: note}
	var lines []ItemStatus
	if receipt != nil {
		stageResult.Items = []ItemResult{{Label: stage.Name, Phase: PhaseDone, Receipt: receipt}}
		stageResult.Tally = tallyOf(stageResult.Items)
		lines = []ItemStatus{{Label: stage.Name, Phase: PhaseDone, Receipt: receipt}}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	line := &s.status.Stages[index]
	line.Phase, line.Note = phase, note
	if lines != nil {
		line.Items = lines
	}
	s.notifyStage(index, stage, phase, 0)
	return stageResult, s.writeStatus()
}

// skipReason returns the note a stage is skipped with, or "" when it runs: the stage it works over
// was skipped, or its `when:` is false against the stages run so far. A verify's `when:` selects
// its items and a repeat's is its loop condition, so neither skips the stage.
func skipReason(plan Plan, index int, result *Result) (string, error) {
	stage := plan.Stages[index]
	if source := sourceName(plan, index); source != "" {
		if sourceResult := stageResultNamed(result, source); sourceResult != nil && sourceResult.Phase == PhaseSkipped {
			return fmt.Sprintf("skipped: stage %s was skipped", source), nil
		}
	}
	if stage.Kind == StageVerify || stage.Kind == StageRepeat || strings.TrimSpace(stage.When) == "" {
		return "", nil
	}
	cond, err := ParseCond(stage.When)
	if err != nil {
		return "", fmt.Errorf("workflow: stage %q, field \"when\": %w", stage.Name, err)
	}
	if cond.Eval(conditionReceipt(plan, index, result)) {
		return "", nil
	}
	return fmt.Sprintf("skipped: %s is false", strings.TrimSpace(stage.When)), nil
}

// sourceName names the earlier stage the stage at index works over, or "" for one that reads none:
// a verify's or merge's fanout, a pick's `from`, a fanout's pick, a repeat's target.
func sourceName(plan Plan, index int) string {
	stage := plan.Stages[index]
	switch stage.Kind {
	case StageVerify, StageMerge:
		source, _ := sourceFanout(plan.Stages, index)
		return source.Name
	case StagePick:
		return stage.From
	case StageFanout:
		return stage.Over.Stage
	case StageRepeat:
		return stage.Repeat
	}
	return ""
}

// stageResultNamed returns the result of the stage called name among those run, or nil.
func stageResultNamed(result *Result, name string) *StageResult {
	at := slices.IndexFunc(result.Stages, func(candidate StageResult) bool { return candidate.Name == name })
	if at < 0 {
		return nil
	}
	return &result.Stages[at]
}

// conditionReceipt gathers what a skip or repeat condition reads into one Receipt for Cond.Eval:
// every stage run so far under `<stage>.<field>` — a script, ask or merge stage's receipt fields,
// a fanout's tally — and, for a repeat, the repeated stage's fields unqualified as well. A skipped
// or unfinished stage contributes nothing, so a term on it is false.
func conditionReceipt(plan Plan, index int, result *Result) Receipt {
	receipt := Receipt{Fields: map[string]any{}}
	for position, stageResult := range result.Stages {
		for name, value := range subjectValues(plan.Stages[position], stageResult) {
			receipt.Fields[plan.Stages[position].Name+subjectSeparator+name] = value
		}
	}
	stage := plan.Stages[index]
	if stage.Kind != StageRepeat {
		return receipt
	}
	target := slices.IndexFunc(plan.Stages, func(candidate Stage) bool { return candidate.Name == stage.Repeat })
	if target < 0 || target >= len(result.Stages) {
		return receipt
	}
	for name, value := range subjectValues(plan.Stages[target], result.Stages[target]) {
		switch name {
		case FieldStatus:
			receipt.Status = Status(fieldText(value))
		case FieldSummary:
			receipt.Summary = fieldText(value)
		default:
			receipt.Fields[name] = value
		}
	}
	return receipt
}

// subjectValues is what a condition may read off one stage's result: a fanout's tally by receipt
// status, or a script, ask or merge stage's receipt — status, summary and its typed fields. A
// skipped stage, an unfinished one and every other kind yield nothing.
func subjectValues(stage Stage, stageResult StageResult) map[string]any {
	if stageResult.Phase == PhaseSkipped {
		return nil
	}
	switch stage.Kind {
	case StageFanout:
		return map[string]any{
			string(StatusOK):      stageResult.Tally.OK,
			string(StatusPartial): stageResult.Tally.Partial,
			string(StatusBlocked): stageResult.Tally.Blocked,
		}
	case StageScript, StageAsk, StageMerge:
		if len(stageResult.Items) != 1 || stageResult.Items[0].Phase != PhaseDone || stageResult.Items[0].Receipt == nil {
			return nil
		}
		receipt := stageResult.Items[0].Receipt
		values := map[string]any{FieldStatus: string(receipt.Status), FieldSummary: receipt.Summary}
		for name, value := range receipt.Fields {
			values[name] = value
		}
		return values
	}
	return nil
}

// fanoutItems returns the items of the fanout at index: its own expanded source, or the items its
// pick stage picked, which it records as its own so a verify over it finds them.
func fanoutItems(plan Plan, index int, stageItems map[int][]Item) []Item {
	stage := plan.Stages[index]
	if stage.Over.Stage == "" {
		return stageItems[index]
	}
	pick := slices.IndexFunc(plan.Stages, func(candidate Stage) bool { return candidate.Name == stage.Over.Stage })
	items := stageItems[pick]
	stageItems[index] = items
	return items
}

// runPick turns an earlier stage's `list` receipt field — unioned across a fanout's items, in item
// order, each entry once — or the non-blank lines of a file in the workflow folder into items for
// the fanout that reads it: the first `cap:` entries (all when 0), `batch:` to a child. A file that
// cannot be read fails the stage with no items; the workflow goes on.
func (s *runState) runPick(plan Plan, index int, stageItems map[int][]Item, result *Result) (StageResult, error) {
	stage := plan.Stages[index]
	if err := s.setStagePhase(index, stage, PhaseRunning, 0); err != nil {
		return StageResult{}, err
	}
	entries, failure := s.pickEntries(stage, result)
	if failure != "" {
		stageItems[index] = nil
		return s.settleStage(index, stage, PhaseFailed, failure, nil)
	}

	picked := entries
	if stage.Cap > 0 && len(picked) > stage.Cap {
		picked = picked[:stage.Cap]
	}
	items := batchItems(singletons(picked), stage.Batch)
	stageItems[index] = items
	note := fmt.Sprintf("picked %d entries into %d items", len(picked), len(items))
	if len(picked) < len(entries) {
		note = fmt.Sprintf("picked %d of %d entries (cap %d) into %d items", len(picked), len(entries), stage.Cap, len(items))
	}
	return s.settleStage(index, stage, PhaseDone, note, nil)
}

// pickEntries reads a pick stage's entries, or says why it could not.
func (s *runState) pickEntries(stage Stage, result *Result) ([]string, string) {
	if stage.File != "" {
		dir, err := s.runner.Store.Dir(s.status.ID)
		if err != nil {
			return nil, err.Error()
		}
		lines, err := nonBlankLines(os.DirFS(dir), path.Clean(stage.File))
		if err != nil {
			return nil, fmt.Sprintf("cannot read %s in the workflow folder: %v", stage.File, err)
		}
		return lines, ""
	}

	source := stageResultNamed(result, stage.From)
	if source == nil {
		return nil, fmt.Sprintf("stage %s has not run", stage.From)
	}
	var entries []string
	seen := map[string]bool{}
	for _, item := range source.Items {
		if item.Phase != PhaseDone || item.Receipt == nil {
			continue
		}
		for _, entry := range stringList(item.Receipt.Fields[stage.Field]) {
			entry = strings.TrimSpace(entry)
			if entry == "" || seen[entry] {
				continue
			}
			seen[entry] = true
			entries = append(entries, entry)
		}
	}
	return entries, ""
}

// stringList reads a list receipt field in either shape it arrives in; anything else is empty.
func stringList(value any) []string {
	switch list := value.(type) {
	case []string:
		return list
	case []any:
		entries := make([]string, 0, len(list))
		for _, element := range list {
			if text, isString := element.(string); isString {
				entries = append(entries, text)
			}
		}
		return entries
	}
	return nil
}

// runScript runs a script stage's command through the ScriptRunner and turns its output into the
// stage's receipt (scriptReceipt). A command that could not run, or whose receipt is blocked,
// fails the stage; the workflow goes on, and a later `when:` may read `<stage>.status`. A command
// that ran is recorded (StageRecord), so a resume replays its result instead of running it again.
func (s *runState) runScript(ctx context.Context, index, round int, stage Stage) (StageResult, error) {
	if replayed, found, err := s.replayStage(index, round, stage); found || err != nil {
		return replayed, err
	}
	if err := s.setStagePhase(index, stage, PhaseRunning, 0); err != nil {
		return StageResult{}, err
	}
	dir, err := s.runner.Store.Dir(s.status.ID)
	if err != nil {
		return StageResult{}, err
	}
	output, runErr := s.runner.Scripts.RunScript(ctx, ScriptSpec{
		Workflow: s.status.ID, Stage: stage.Name, Command: stage.Run, Dir: dir,
	})
	if ctx.Err() != nil {
		return s.settleStage(index, stage, PhaseStopped, "", nil)
	}

	if runErr != nil {
		receipt := Receipt{
			Status:  StatusBlocked,
			Summary: clampWords("the script could not run: "+firstLine(runErr.Error()), SummaryMaxWords),
		}
		return s.settleStage(index, stage, PhaseFailed, "", &receipt)
	}
	receipt := scriptReceipt(output, stage.Returns)
	phase := PhaseDone
	if receipt.Status == StatusBlocked {
		phase = PhaseFailed
	}
	return s.settleRecorded(index, round, stage, StageRecord{Phase: phase, Receipt: receipt})
}

// scriptReceipt reads a script's receipt off its output. Each stdout line `KEY=value` whose key —
// lower-cased — the stage's `returns:` declares becomes that field: an int parsed, a list gaining
// one entry per line, text or an enum as written (the last line wins). A `summary=` line sets the
// summary, which is otherwise the exit code; every other line is ignored. The status is ok on exit
// 0, blocked otherwise — and blocked, saying why, when an ok receipt's fields do not pass the
// declaration.
func scriptReceipt(output ScriptOutput, spec ReceiptSpec) Receipt {
	receipt := Receipt{Status: StatusOK, Summary: fmt.Sprintf("the script exited %d", output.ExitCode)}
	fields := map[string]any{}
	for _, line := range strings.Split(output.Stdout, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found {
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		if key == FieldSummary {
			if value != "" {
				receipt.Summary = clampWords(value, SummaryMaxWords)
			}
			continue
		}
		fieldType, declared := spec.Field(key)
		if !declared || key == FieldStatus {
			continue
		}
		switch fieldType.Kind {
		case FieldInt:
			if number, err := strconv.Atoi(value); err == nil {
				fields[key] = number
			} else {
				fields[key] = value
			}
		case FieldList:
			if value != "" {
				list, _ := fields[key].([]string)
				fields[key] = append(list, value)
			}
		default:
			fields[key] = value
		}
	}
	if len(fields) > 0 {
		receipt.Fields = fields
	}

	if output.ExitCode != 0 {
		receipt.Status = StatusBlocked
		return receipt
	}
	if problems := spec.Check(receipt); len(problems) > 0 {
		receipt.Status = StatusBlocked
		receipt.Summary = clampWords("the script's output: "+problems[0].String(), SummaryMaxWords)
	}
	return receipt
}

// runAsk puts an ask stage's question through the Asker and stores the answer in the `answer`
// field of an ok receipt. The default is taken — and the stage's note says why — when there is no
// Asker, when asking fails, when the answer is empty, or when it is not one of the options. A
// question the Asker answered is recorded (StageRecord), so a resume replays the answer instead of
// asking again; a default taken because no one could be asked is not, so a resume asks.
func (s *runState) runAsk(ctx context.Context, index, round int, stage Stage) (StageResult, error) {
	if replayed, found, err := s.replayStage(index, round, stage); found || err != nil {
		return replayed, err
	}
	if err := s.setStagePhase(index, stage, PhaseRunning, 0); err != nil {
		return StageResult{}, err
	}
	answer, note, answered := stage.Default, noOneToAskNote, false
	if asker := s.runner.Asker; asker != nil {
		given, err := asker.Ask(ctx, Question{
			Workflow: s.status.ID, Stage: stage.Name, Text: stage.Question,
			Options: append([]string(nil), stage.Options...), Default: stage.Default,
		})
		given = strings.TrimSpace(given)
		switch {
		case ctx.Err() != nil:
			return s.settleStage(index, stage, PhaseStopped, "", nil)
		case err != nil:
			note = "(default taken: the question could not be asked: " + firstLine(err.Error()) + ")"
		case given == "":
			note, answered = "(default taken: no answer)", true
		case len(stage.Options) > 0 && !slices.Contains(stage.Options, given):
			note, answered = fmt.Sprintf("(default taken: %q is not one of the options)", given), true
		default:
			answer, note, answered = given, "", true
		}
	}

	summary := "answered " + answer
	if note != "" {
		summary = "took the default " + answer
	}
	receipt := Receipt{
		Status:  StatusOK,
		Summary: clampWords(summary, SummaryMaxWords),
		Fields:  map[string]any{AskAnswerField: answer},
	}
	if !answered {
		return s.settleStage(index, stage, PhaseDone, note, &receipt)
	}
	return s.settleRecorded(index, round, stage, StageRecord{Phase: PhaseDone, Note: note, Receipt: receipt})
}

// settleRecorded records a script or ask stage's outcome in the folder (Store.WriteStageRecord) and
// settles the stage on it. The record is written first, so a status.json that shows the stage
// settled always has its record beside it for a resume to replay.
func (s *runState) settleRecorded(index, round int, stage Stage, record StageRecord) (StageResult, error) {
	if err := s.runner.Store.WriteStageRecord(s.status.ID, stage.Name, round, record); err != nil {
		return StageResult{}, err
	}
	receipt := record.Receipt
	return s.settleStage(index, stage, record.Phase, record.Note, &receipt)
}

// replayStage settles a script or ask stage from the record an earlier run of this workflow left for
// the same round, when the run resumes an unfinished folder (runState.replay): the script is not run
// and the question not asked again. Its one item is marked Resumed, as a skipped fan-out item is.
// found is false when there is nothing to replay and the stage runs.
func (s *runState) replayStage(index, round int, stage Stage) (StageResult, bool, error) {
	if !s.replay {
		return StageResult{}, false, nil
	}
	record, found, err := s.runner.Store.ReadStageRecord(s.status.ID, stage.Name, round)
	if err != nil || !found {
		return StageResult{}, false, err
	}
	receipt := record.Receipt
	stageResult, err := s.settleStage(index, stage, record.Phase, record.Note, &receipt)
	if err != nil {
		return stageResult, true, err
	}
	for i := range stageResult.Items {
		stageResult.Items[i].Resumed = true
	}
	stageResult.Tally = tallyOf(stageResult.Items)
	return stageResult, true, nil
}

// runRepeat re-runs the stage its `repeat:` names while its `when:` holds, at most `max:` times,
// each re-run a round of its own (its items keyed apart, so they are spawned afresh) whose result
// replaces the stage's in result. The condition is read before every round. A cancel stops the
// repeat and the workflow.
func (s *runState) runRepeat(ctx context.Context, plan Plan, index int, stageItems map[int][]Item, result *Result) (StageResult, error) {
	stage := plan.Stages[index]
	cond, err := ParseCond(stage.When)
	if err != nil {
		return StageResult{}, fmt.Errorf("workflow: stage %q, field \"when\": %w", stage.Name, err)
	}
	target := slices.IndexFunc(plan.Stages, func(candidate Stage) bool { return candidate.Name == stage.Repeat })
	if target < 0 || target >= len(result.Stages) {
		return StageResult{}, fmt.Errorf("workflow: stage %q: the stage it repeats, %q, has not run", stage.Name, stage.Repeat)
	}
	if err := s.setStagePhase(index, stage, PhaseRunning, 0); err != nil {
		return StageResult{}, err
	}

	rounds := 0
	for rounds < stage.Max && cond.Eval(conditionReceipt(plan, index, result)) {
		if ctx.Err() != nil {
			return s.settleStage(index, stage, PhaseStopped, repeatNote(stage, rounds, false), nil)
		}
		rounds++
		rerun, err := s.runRound(ctx, plan, target, rounds, stageItems, result)
		if err != nil {
			return StageResult{}, err
		}
		result.Stages[target] = rerun
		if rerun.Phase == PhaseStopped {
			return s.settleStage(index, stage, PhaseStopped, repeatNote(stage, rounds, false), nil)
		}
	}
	isStillHolding := rounds == stage.Max && cond.Eval(conditionReceipt(plan, index, result))
	return s.settleStage(index, stage, PhaseDone, repeatNote(stage, rounds, isStillHolding), nil)
}

// repeatNote says how many rounds a repeat ran, and whether its condition still held at the bound.
func repeatNote(stage Stage, rounds int, isStillHolding bool) string {
	note := fmt.Sprintf("re-ran %s in %d of at most %d rounds", stage.Repeat, rounds, stage.Max)
	if isStillHolding {
		note += "; its condition still held"
	}
	return note
}
