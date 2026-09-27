package workflow

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// namePattern is the spelling of a stage name and of a declared receipt field: lower-case, led by
// a letter, so it reads the same in a condition, a folder name and a result line.
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// stageField ties one Stage key to the kinds that read it, so a key set on any other kind is a
// problem instead of a silent no-op.
type stageField struct {
	key   string
	isSet func(Stage) bool
	kinds []StageKind
}

// stageFields is every kind-specific Stage key. `name`, `kind` and `when` belong to every kind and
// are not listed.
var stageFields = []stageField{
	{"task", func(s Stage) bool { return s.Task != "" }, []StageKind{StageFanout, StageVerify, StageMerge}},
	{"prompt", func(s Stage) bool { return s.Prompt != "" }, []StageKind{StageFanout, StageVerify, StageMerge}},
	{"over", func(s Stage) bool { return s.Over != nil }, []StageKind{StageFanout}},
	{"returns", func(s Stage) bool { return len(s.Returns) > 0 }, []StageKind{StageFanout, StageMerge, StageScript}},
	{"out", func(s Stage) bool { return s.Out != "" }, []StageKind{StageFanout}},
	{"context", func(s Stage) bool { return len(s.Context) > 0 }, []StageKind{StageFanout, StageVerify, StageMerge}},
	{"tools", func(s Stage) bool { return len(s.Tools) > 0 }, []StageKind{StageFanout, StageVerify, StageMerge}},
	{"from", func(s Stage) bool { return s.From != "" }, []StageKind{StageVerify, StageMerge, StagePick}},
	{"field", func(s Stage) bool { return s.Field != "" }, []StageKind{StagePick}},
	{"file", func(s Stage) bool { return s.File != "" }, []StageKind{StagePick}},
	{"cap", func(s Stage) bool { return s.Cap != 0 }, []StageKind{StagePick}},
	{"batch", func(s Stage) bool { return s.Batch != 0 }, []StageKind{StagePick}},
	{"run", func(s Stage) bool { return s.Run != "" }, []StageKind{StageScript}},
	{"question", func(s Stage) bool { return s.Question != "" }, []StageKind{StageAsk}},
	{"options", func(s Stage) bool { return len(s.Options) > 0 }, []StageKind{StageAsk}},
	{"default", func(s Stage) bool { return s.Default != "" }, []StageKind{StageAsk}},
	{"repeat", func(s Stage) bool { return s.Repeat != "" }, []StageKind{StageRepeat}},
	{"max", func(s Stage) bool { return s.Max != 0 }, []StageKind{StageRepeat}},
}

// Validate reports every problem with p, each naming the stage and field it sits in and saying
// how to fix it. It checks shape only — kinds, required and misplaced keys, bounds, references to
// earlier stages, receipt type spellings, `when:` conditions — and reads no file. An empty result
// means p can run.
func Validate(p Plan) []Problem {
	if len(p.Stages) == 0 {
		return []Problem{{Field: "stages", Message: "the workflow has no stages; add at least one fanout stage"}}
	}

	var problems []Problem
	for index := range p.Stages {
		problems = append(problems, validateStage(p.Stages, index)...)
	}
	return problems
}

// ValidateModelPlan is Validate plus the limits on a plan the model writes through `fan_out`
// (ADR 0087 D1): exactly one fanout, then at most one verify, then at most one merge.
// Anything longer exists only in a Recipe.
func ValidateModelPlan(p Plan) []Problem {
	problems := Validate(p)
	if len(p.Stages) == 0 {
		return problems
	}

	counts := map[StageKind]int{}
	for _, stage := range p.Stages {
		counts[stage.Kind]++
		if message := modelShapeMessage(stage, counts); message != "" {
			problems = append(problems, Problem{Stage: stage.Name, Field: "kind", Message: message})
		}
	}
	if counts[StageFanout] == 0 {
		problems = append(problems, Problem{Field: "stages", Message: "a fan_out needs one fanout stage; add it first"})
	}
	return problems
}

// modelShapeMessage returns why stage breaks the fan_out shape, given the kinds counted so far
// (this stage included), or "" when it fits. The fanout comes first without a rule of its own: a
// verify or merge before it already fails Validate, and every other kind is refused here.
func modelShapeMessage(stage Stage, counts map[StageKind]int) string {
	switch stage.Kind {
	case StageFanout:
		if counts[StageFanout] > 1 {
			return "a fan_out runs one fanout stage; a second one needs a recipe"
		}
	case StageVerify:
		if counts[StageVerify] > 1 {
			return "a fan_out runs at most one verify stage"
		}
		if counts[StageMerge] > 0 {
			return "verify comes before merge in a fan_out"
		}
	case StageMerge:
		if counts[StageMerge] > 1 {
			return "a fan_out runs at most one merge stage"
		}
	default:
		return fmt.Sprintf(
			"a fan_out runs only fanout, verify and merge; a %s stage exists only in a recipe", stage.Kind,
		)
	}
	return ""
}

// validateStage reports the problems of stages[index], reading the stages before it for the names
// it may refer to.
func validateStage(stages []Stage, index int) []Problem {
	stage := stages[index]
	problems := nameProblems(stages, index)
	if !slices.Contains(stageKinds, stage.Kind) {
		return append(problems, Problem{Stage: stage.Name, Field: "kind", Message: fmt.Sprintf(
			"kind %q is not a stage kind; use one of %s", stage.Kind, kindList(),
		)})
	}

	problems = append(problems, misplacedFieldProblems(stage)...)
	problems = append(problems, conditionProblems(stages, index)...)
	switch stage.Kind {
	case StageFanout:
		problems = append(problems, childStageProblems(stage, true)...)
		problems = append(problems, itemSourceProblems(stages, index)...)
		problems = append(problems, receiptSpecProblems(stage)...)
	case StageVerify:
		problems = append(problems, childStageProblems(stage, false)...)
		problems = append(problems, fanoutSourceProblems(stages, index)...)
	case StageMerge:
		problems = append(problems, childStageProblems(stage, true)...)
		problems = append(problems, fanoutSourceProblems(stages, index)...)
		problems = append(problems, receiptSpecProblems(stage)...)
	case StagePick:
		problems = append(problems, pickProblems(stages, index)...)
	case StageScript:
		problems = append(problems, scriptProblems(stage)...)
	case StageAsk:
		problems = append(problems, askProblems(stage)...)
	case StageRepeat:
		problems = append(problems, repeatProblems(stages, index)...)
	}
	return problems
}

// kindList names every stage kind, for a fix message.
func kindList() string {
	names := make([]string, len(stageKinds))
	for i, kind := range stageKinds {
		names[i] = string(kind)
	}
	return strings.Join(names, ", ")
}

// nameProblems reports a missing, badly spelled or repeated stage name.
func nameProblems(stages []Stage, index int) []Problem {
	name := stages[index].Name
	if name == "" {
		return []Problem{{Field: "name", Message: fmt.Sprintf(
			"stage %d has no name; give it one so later stages and the report can refer to it", index+1,
		)}}
	}
	if !namePattern.MatchString(name) {
		return []Problem{{Stage: name, Field: "name", Message: "a stage name is lower-case letters, digits, - and _, led by a letter"}}
	}
	for _, earlier := range stages[:index] {
		if earlier.Name == name {
			return []Problem{{Stage: name, Field: "name", Message: "another stage already has this name; give each stage its own"}}
		}
	}
	return nil
}

// misplacedFieldProblems reports every kind-specific key set on a kind that does not read it.
func misplacedFieldProblems(stage Stage) []Problem {
	var problems []Problem
	for _, field := range stageFields {
		if !field.isSet(stage) || slices.Contains(field.kinds, stage.Kind) {
			continue
		}
		readers := make([]string, len(field.kinds))
		for i, kind := range field.kinds {
			readers[i] = string(kind)
		}
		problems = append(problems, Problem{Stage: stage.Name, Field: field.key, Message: fmt.Sprintf(
			"a %s stage does not read %s; remove it (only %s stages do)",
			stage.Kind, field.key, strings.Join(readers, ", "),
		)})
	}
	return problems
}

// childStageProblems reports the problems every child-running stage (fanout, verify, merge)
// shares: its brief given twice, or missing where isBriefRequired, and a blank context or tools
// entry.
func childStageProblems(stage Stage, isBriefRequired bool) []Problem {
	var problems []Problem
	if stage.Task != "" && stage.Prompt != "" {
		problems = append(problems, Problem{Stage: stage.Name, Field: "task", Message: "the brief is given twice; keep either task (inline) or prompt (a file path)"})
	} else if isBriefRequired && stage.Task == "" && stage.Prompt == "" {
		problems = append(problems, Problem{Stage: stage.Name, Field: "task", Message: fmt.Sprintf(
			"a %s stage needs a brief; set task (inline) or prompt (a file path)", stage.Kind,
		)})
	}
	problems = append(problems, blankEntryProblems(stage.Name, "context", stage.Context)...)
	return append(problems, blankEntryProblems(stage.Name, "tools", stage.Tools)...)
}

// itemSourceProblems reports a fanout's `over` problems: no source or not exactly one, a blank literal
// item, a negative batch, or a stage source that is not an earlier pick.
func itemSourceProblems(stages []Stage, index int) []Problem {
	stage := stages[index]
	source := stage.Over
	if source == nil {
		return []Problem{{Stage: stage.Name, Field: "over", Message: "a fanout stage needs items; set over to a list, files, lines, split or stage"}}
	}
	set := 0
	for _, isSet := range []bool{len(source.List) > 0, source.Files != "", source.Lines != "", source.Split != "", source.Stage != ""} {
		if isSet {
			set++
		}
	}
	problem := func(message string) []Problem {
		return []Problem{{Stage: stage.Name, Field: "over", Message: message}}
	}
	if set != 1 {
		return problem("set exactly one item source: list, files, lines, split or stage")
	}
	if source.Batch < 0 {
		return problem("batch is negative; set it to how many items one child gets")
	}
	if slices.ContainsFunc(source.List, func(item string) bool { return strings.TrimSpace(item) == "" }) {
		return problem("the list has a blank item; remove it")
	}
	if source.Stage == "" {
		return nil
	}
	if source.Batch != 0 {
		return problem("batch the items on the pick stage, not here")
	}
	picked, message := earlierStage(stages, index, source.Stage)
	if message != "" {
		return problem(message)
	}
	if picked.Kind != StagePick {
		return problem(fmt.Sprintf("stage %q is a %s stage; over.stage names a pick stage", source.Stage, picked.Kind))
	}
	return nil
}

// fanoutSourceProblems reports a verify or merge stage whose `from` does not name an earlier
// fanout, or which has no earlier fanout to default to.
func fanoutSourceProblems(stages []Stage, index int) []Problem {
	stage := stages[index]
	problem := func(message string) []Problem {
		return []Problem{{Stage: stage.Name, Field: "from", Message: message}}
	}
	if stage.From == "" {
		for _, earlier := range stages[:index] {
			if earlier.Kind == StageFanout {
				return nil
			}
		}
		return problem(fmt.Sprintf("a %s stage works over a fanout, and none comes before it; add one first", stage.Kind))
	}
	source, message := earlierStage(stages, index, stage.From)
	if message != "" {
		return problem(message)
	}
	if source.Kind != StageFanout {
		return problem(fmt.Sprintf("stage %q is a %s stage; a %s stage works over a fanout", stage.From, source.Kind, stage.Kind))
	}
	return nil
}

// pickProblems reports a pick stage with not exactly one of field and file, a field read from a
// stage that does not declare it as a list, or a negative cap or batch.
func pickProblems(stages []Stage, index int) []Problem {
	stage := stages[index]
	problem := func(field, message string) []Problem {
		return []Problem{{Stage: stage.Name, Field: field, Message: message}}
	}
	if stage.Cap < 0 {
		return problem("cap", "cap is negative; set it to the most items to keep, or leave it out for all")
	}
	if stage.Batch < 0 {
		return problem("batch", "batch is negative; set it to how many items one child gets")
	}
	if (stage.Field == "") == (stage.File == "") {
		return problem("field", "set exactly one of field (a list receipt field) or file (an output file's lines)")
	}
	if stage.File != "" {
		if stage.From != "" {
			return problem("from", "from is read only with field; remove it, the file names itself")
		}
		return nil
	}
	if stage.From == "" {
		return problem("from", "a pick by field needs from: the earlier stage whose receipt carries it")
	}
	source, message := earlierStage(stages, index, stage.From)
	if message != "" {
		return problem("from", message)
	}
	fieldType, declared := source.Returns.Field(stage.Field)
	if !declared || fieldType.Kind != FieldList {
		return problem("field", fmt.Sprintf(
			"stage %q returns no list field %q; declare it there as %s: list", stage.From, stage.Field, stage.Field,
		))
	}
	return nil
}

// scriptProblems reports a script stage with no command, or a bad receipt declaration.
func scriptProblems(stage Stage) []Problem {
	var problems []Problem
	if strings.TrimSpace(stage.Run) == "" {
		problems = append(problems, Problem{Stage: stage.Name, Field: "run", Message: "a script stage needs run: the command to execute"})
	}
	return append(problems, receiptSpecProblems(stage)...)
}

// askProblems reports an ask stage with no question or default, or options the default is not
// among.
func askProblems(stage Stage) []Problem {
	problem := func(field, message string) []Problem {
		return []Problem{{Stage: stage.Name, Field: field, Message: message}}
	}
	if strings.TrimSpace(stage.Question) == "" {
		return problem("question", "an ask stage needs question: what to ask the user")
	}
	if stage.Default == "" {
		return problem("default", "an ask stage needs default: the answer taken when no one is there to ask")
	}
	if len(stage.Options) == 0 {
		return nil
	}
	if len(stage.Options) < 2 {
		return problem("options", "offer at least two options, or leave options out for a free answer")
	}
	if !slices.Contains(stage.Options, stage.Default) {
		return problem("default", fmt.Sprintf("default %q is not among the options (%s)", stage.Default, strings.Join(stage.Options, ", ")))
	}
	return nil
}

// repeatProblems reports a repeat stage with no earlier target, no condition, or a max outside
// 1..MaxRepeatRounds.
func repeatProblems(stages []Stage, index int) []Problem {
	stage := stages[index]
	var problems []Problem
	problem := func(field, message string) {
		problems = append(problems, Problem{Stage: stage.Name, Field: field, Message: message})
	}
	if stage.Repeat == "" {
		problem("repeat", "a repeat stage needs repeat: the earlier stage to run again")
	} else if target, message := earlierStage(stages, index, stage.Repeat); message != "" {
		problem("repeat", message)
	} else if target.Kind == StageRepeat {
		problem("repeat", fmt.Sprintf("stage %q is itself a repeat; repeat the stage it repeats", stage.Repeat))
	}
	if strings.TrimSpace(stage.When) == "" {
		problem("when", "a repeat stage needs when: the condition to run again on")
	}
	if stage.Max < 1 || stage.Max > MaxRepeatRounds {
		problem("max", fmt.Sprintf("max is %d; set it between 1 and %d rounds", stage.Max, MaxRepeatRounds))
	}
	return problems
}

// receiptSpecProblems reports every declared receipt field with a bad name, a reserved name or an
// unreadable type spelling.
func receiptSpecProblems(stage Stage) []Problem {
	var problems []Problem
	for _, name := range sortedKeys(stage.Returns) {
		field := "returns." + name
		switch {
		case name == FieldStatus || name == FieldSummary:
			problems = append(problems, Problem{Stage: stage.Name, Field: field, Message: fmt.Sprintf(
				"%s is part of every receipt already; remove it from returns", name,
			)})
		case !namePattern.MatchString(name):
			problems = append(problems, Problem{Stage: stage.Name, Field: field, Message: "a field name is lower-case letters, digits, - and _, led by a letter"})
		default:
			if _, err := ParseFieldType(stage.Returns[name]); err != nil {
				problems = append(problems, Problem{Stage: stage.Name, Field: field, Message: err.Error()})
			}
		}
	}
	return problems
}

// conditionProblems reports a `when:` that does not parse, and a verify stage's `when:` that does
// not type-check against the ReceiptSpec of the fanout it works over — the per-item receipts its
// condition selects from. Every other kind's condition reads an earlier stage's receipt rather than
// its own, so it is checked for syntax here. A blank `when:` is no condition (a repeat's missing one
// is repeatProblems').
func conditionProblems(stages []Stage, index int) []Problem {
	stage := stages[index]
	if strings.TrimSpace(stage.When) == "" {
		return nil
	}
	problem := func(err error) []Problem {
		return []Problem{{Stage: stage.Name, Field: "when", Message: err.Error()}}
	}

	cond, err := ParseCond(stage.When)
	if err != nil {
		return problem(err)
	}
	if stage.Kind != StageVerify {
		return nil
	}
	source, found := sourceFanout(stages, index)
	if !found {
		return nil
	}
	if err := cond.Check(source.Returns); err != nil {
		return problem(err)
	}
	return nil
}

// sourceFanout returns the fanout a verify or merge stage at index works over: the one its `from`
// names, or the nearest earlier fanout. found is false when there is none, which
// fanoutSourceProblems reports.
func sourceFanout(stages []Stage, index int) (Stage, bool) {
	stage := stages[index]
	if stage.From != "" {
		source, message := earlierStage(stages, index, stage.From)
		return source, message == "" && source.Kind == StageFanout
	}
	for earlier := index - 1; earlier >= 0; earlier-- {
		if stages[earlier].Kind == StageFanout {
			return stages[earlier], true
		}
	}
	return Stage{}, false
}

// blankEntryProblems reports a list key holding a blank entry.
func blankEntryProblems(stageName, field string, entries []string) []Problem {
	if slices.ContainsFunc(entries, func(entry string) bool { return strings.TrimSpace(entry) == "" }) {
		return []Problem{{Stage: stageName, Field: field, Message: fmt.Sprintf("%s has a blank entry; remove it", field)}}
	}
	return nil
}

// earlierStage finds the stage called name before stages[index]. When there is none it returns a
// fix message saying whether the name comes later (a forward reference) or names no stage at all.
func earlierStage(stages []Stage, index int, name string) (Stage, string) {
	for _, earlier := range stages[:index] {
		if earlier.Name == name {
			return earlier, ""
		}
	}
	for _, later := range stages[index:] {
		if later.Name == name {
			return Stage{}, fmt.Sprintf("stage %q does not come before this one; a stage may refer only to earlier stages", name)
		}
	}
	earlierNames := make([]string, 0, index)
	for _, earlier := range stages[:index] {
		earlierNames = append(earlierNames, earlier.Name)
	}
	if len(earlierNames) == 0 {
		return Stage{}, fmt.Sprintf("no stage is named %q, and no stage comes before this one", name)
	}
	return Stage{}, fmt.Sprintf("no stage is named %q; the earlier stages are %s", name, strings.Join(earlierNames, ", "))
}
