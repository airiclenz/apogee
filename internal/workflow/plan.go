package workflow

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"unicode/utf8"
)

// SummaryMaxWords is the longest a Receipt's summary may run. The summary is the one prose piece
// of a receipt and it lands on the parent's result line, so it is held to a line; the detail
// belongs in the item's output file.
const SummaryMaxWords = 20

// TextMaxRunes is the longest a `text` receipt field may run, in runes.
const TextMaxRunes = 200

// MaxRepeatRounds is the largest `max:` a repeat stage may declare. A recipe's repeat is BOUNDED
// (ADR 0087 D6), and this is the bound's own ceiling, so a typo cannot ask for a thousand rounds.
const MaxRepeatRounds = 10

// Plan is one Workflow definition, whichever source wrote it: the stages a `fan_out` call asks
// for or the ones a Recipe declares. It is the in-memory type only — the CONTEXT.md word for the
// human-written definition is Recipe, and "plan" otherwise means a docs/plans/ document. Stages
// run in the order listed; a stage may refer only to stages before it.
type Plan struct {
	// Name labels the workflow (the recipe's name, or a slug of the fan-out's brief); it names the
	// workflow folder and is never validated.
	Name   string  `yaml:"name,omitempty" json:"name,omitempty"`
	Stages []Stage `yaml:"stages" json:"stages"`
}

// StageKind names what a stage does. The seven kinds are ADR 0087 D6's; a `fan_out` plan uses
// only fanout, verify and merge (ValidateModelPlan).
type StageKind string

// The seven stage kinds.
const (
	// StageFanout runs one fresh child per item (or per batch of items), each handing back a Receipt.
	StageFanout StageKind = "fanout"
	// StageVerify runs one adversarial child per item of an earlier fanout that its `when:` selects.
	StageVerify StageKind = "verify"
	// StageMerge runs one child over the outputs of an earlier fanout and writes the report.
	StageMerge StageKind = "merge"
	// StagePick turns a receipt `list` field or the lines of an output file into items.
	StagePick StageKind = "pick"
	// StageScript runs a command and reads its KEY=value stdout lines as receipt fields.
	StageScript StageKind = "script"
	// StageAsk puts a question to the user and stores the answer in the `answer` field.
	StageAsk StageKind = "ask"
	// StageRepeat re-runs an earlier stage while its condition holds, at most `max:` times.
	StageRepeat StageKind = "repeat"
)

// stageKinds lists every kind in ADR 0087 D6's order, for membership checks and fix messages.
var stageKinds = []StageKind{
	StageFanout, StageMerge, StagePick, StageVerify, StageScript, StageAsk, StageRepeat,
}

// AskAnswerField is the receipt field an ask stage stores the user's answer in, so a later
// condition reads it as `answer`.
const AskAnswerField = "answer"

// Stage is one step of a Plan. The struct is flat: every kind reads its own subset of the fields,
// and Validate refuses a field set on a kind that does not read it rather than ignoring it, so an
// author's misplaced key is a fixable problem instead of a silent no-op. The yaml and json keys are
// the recipe's own spelling.
type Stage struct {
	// Name identifies the stage; later stages refer to it by this name. Required, unique.
	Name string    `yaml:"name" json:"name"`
	Kind StageKind `yaml:"kind" json:"kind"`
	// When is a condition on receipt fields. On a verify stage it selects the items to check, reading
	// each item's receipt; on every other kind it skips the stage when false; on a repeat it is the
	// loop condition. Outside a verify each field names the earlier stage it reads — `split.parts`,
	// `find.blocked` — a script, ask or merge stage's receipt or a fanout's tally; a repeat may leave
	// the stage off a field of the stage it repeats.
	When string `yaml:"when,omitempty" json:"when,omitempty"`

	// Task is the brief written inline, with {item} and {out} placeholders; Prompt is the brief
	// as a file path instead. A fanout or merge takes exactly one; a verify at most one (the
	// engine's own adversarial brief always leads).
	Task   string `yaml:"task,omitempty" json:"task,omitempty"`
	Prompt string `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	// Over is where a fanout's items come from.
	Over *ItemSource `yaml:"over,omitempty" json:"over,omitempty"`
	// Returns declares the typed receipt fields beyond the fixed status and summary.
	Returns ReceiptSpec `yaml:"returns,omitempty" json:"returns,omitempty"`
	// Out is a fanout's per-item output path template; empty lets the engine choose one in the
	// item's folder.
	Out string `yaml:"out,omitempty" json:"out,omitempty"`
	// Context lists files every child of the stage reads first.
	Context []string `yaml:"context,omitempty" json:"context,omitempty"`
	// Tools narrows the children's tools to these names.
	Tools []string `yaml:"tools,omitempty" json:"tools,omitempty"`

	// From names the earlier stage this one reads: the fanout a verify or merge works over
	// (default: the nearest earlier fanout), or the stage whose receipt `list` field a pick takes.
	From string `yaml:"from,omitempty" json:"from,omitempty"`
	// Field is the `list` receipt field a pick takes its items from; File is an output file in the
	// workflow folder (a path local to it) whose non-blank lines a pick takes instead, each line
	// once at its first occurrence. Exactly one.
	Field string `yaml:"field,omitempty" json:"field,omitempty"`
	File  string `yaml:"file,omitempty" json:"file,omitempty"`
	// Cap keeps at most this many picked items (0: all); Batch groups them this many per child.
	Cap   int `yaml:"cap,omitempty" json:"cap,omitempty"`
	Batch int `yaml:"batch,omitempty" json:"batch,omitempty"`

	// Run is a script stage's command, run under the Mode and approval rules of the shell tool.
	Run string `yaml:"run,omitempty" json:"run,omitempty"`

	// Question, Options and Default are an ask stage's. Default is required: it is the answer a
	// Driver with no human takes (ADR 0087 D10).
	Question string   `yaml:"question,omitempty" json:"question,omitempty"`
	Options  []string `yaml:"options,omitempty" json:"options,omitempty"`
	Default  string   `yaml:"default,omitempty" json:"default,omitempty"`

	// Repeat names the earlier stage a repeat stage re-runs; Max bounds the rounds.
	Repeat string `yaml:"repeat,omitempty" json:"repeat,omitempty"`
	Max    int    `yaml:"max,omitempty" json:"max,omitempty"`

	// SubAgent, when set on a fanout stage, makes its item run the blocking sub_agent path (ADR
	// 0094) instead of a receipt-reporting item child: it holds that sub_agent call's arguments
	// verbatim, for the Spawner to decode exactly as a sub_agent call's are. Only the engine sets it
	// (a background sub_agent's one-item plan), so it has no yaml spelling a recipe could write, and
	// it is left out of the JSON when unset, which keeps the PlanHash of every plan without it as it
	// was. Such an item calls no finish: the Runner synthesizes its receipt from how the child ended
	// (subAgentReceipt), writes the child's report to the item's output file, and never retries or
	// continues it (runItem).
	SubAgent json.RawMessage `yaml:"-" json:"sub_agent,omitempty"`
}

// RunsSubAgent reports whether the stage's items run the blocking sub_agent path (Stage.SubAgent).
func (s Stage) RunsSubAgent() bool { return len(s.SubAgent) > 0 }

// ItemSource is where a fanout stage's items come from. Exactly one of List, Files, Lines, Split
// and Stage is set; Batch groups the resulting items that many per child.
type ItemSource struct {
	// List is the items written out literally; an entry written twice is one item.
	List []string `yaml:"list,omitempty" json:"list,omitempty"`
	// Files is a workspace-relative glob (`**` allowed); every match is an item.
	Files string `yaml:"files,omitempty" json:"files,omitempty"`
	// Lines is a file whose non-blank lines are the items; a repeated line is one item.
	Lines string `yaml:"lines,omitempty" json:"lines,omitempty"`
	// Split is a directory cut into contiguous parts sized to fit a child's context window.
	Split string `yaml:"split,omitempty" json:"split,omitempty"`
	// Stage names an earlier pick stage whose items this fanout takes.
	Stage string `yaml:"stage,omitempty" json:"stage,omitempty"`
	// Batch is how many items one child gets (0 or 1: one each).
	Batch int `yaml:"batch,omitempty" json:"batch,omitempty"`
}

// Status is a Receipt's fixed verdict on the item.
type Status string

// The three receipt statuses.
const (
	StatusOK      Status = "ok"
	StatusPartial Status = "partial"
	StatusBlocked Status = "blocked"
)

// statuses lists the receipt statuses in the order a fix message names them.
var statuses = []string{string(StatusOK), string(StatusPartial), string(StatusBlocked)}

// The core receipt fields every receipt carries whatever its ReceiptSpec declares.
const (
	FieldStatus  = "status"
	FieldSummary = "summary"
)

// Receipt is the short, fixed-shape note a workflow item's child hands back through `finish`: a
// status, a one-line summary, and the typed fields the workflow asked for. Fields holds values as
// they arrive off a decoded tool call — a number, a string, a list of strings — keyed by the
// field name; ReceiptSpec.Check says whether they are well formed.
type Receipt struct {
	Status  Status         `yaml:"status" json:"status"`
	Summary string         `yaml:"summary" json:"summary"`
	Fields  map[string]any `yaml:"fields,omitempty" json:"fields,omitempty"`
}

// ReceiptSpec declares the typed receipt fields a stage wants beyond status and summary, as a map
// from field name to type spelling: `int`, `text` (at most TextMaxRunes runes), `list` (of
// strings), or an enum written `a|b|c` (optionally prefixed `enum `). A spec is data exactly as
// the author wrote it; Validate reports an unreadable spelling and Field parses one.
type ReceiptSpec map[string]string

// FieldKind is the type family of a receipt field.
type FieldKind string

// The four field kinds.
const (
	FieldInt  FieldKind = "int"
	FieldEnum FieldKind = "enum"
	FieldText FieldKind = "text"
	FieldList FieldKind = "list"
)

// FieldType is a parsed receipt field type; Values holds an enum's allowed values in the order
// written and is empty for every other kind.
type FieldType struct {
	Kind   FieldKind
	Values []string
}

// enumPrefix is the optional lead word of an enum spelling (`enum a|b`).
const enumPrefix = "enum "

// ParseFieldType reads one type spelling. An enum needs at least two distinct, non-empty values.
func ParseFieldType(spelling string) (FieldType, error) {
	trimmed := strings.TrimSpace(spelling)
	switch FieldKind(trimmed) {
	case FieldInt, FieldText, FieldList:
		return FieldType{Kind: FieldKind(trimmed)}, nil
	}

	body := strings.TrimSpace(strings.TrimPrefix(trimmed, enumPrefix))
	if !strings.Contains(body, "|") {
		return FieldType{}, fmt.Errorf(
			"type %q is not one of int, text, list or an enum written a|b|c", spelling,
		)
	}
	values := strings.Split(body, "|")
	seen := make(map[string]bool, len(values))
	for i, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return FieldType{}, fmt.Errorf("enum %q has an empty value; write each value between the bars", spelling)
		}
		if seen[value] {
			return FieldType{}, fmt.Errorf("enum %q lists %q twice; list each value once", spelling, value)
		}
		seen[value] = true
		values[i] = value
	}
	return FieldType{Kind: FieldEnum, Values: values}, nil
}

// Field returns the type of the named receipt field, the two core fields included: status is the
// enum ok|partial|blocked and summary is text. ok is false for a name the spec does not declare or
// whose spelling does not parse.
func (s ReceiptSpec) Field(name string) (FieldType, bool) {
	switch name {
	case FieldStatus:
		return FieldType{Kind: FieldEnum, Values: append([]string(nil), statuses...)}, true
	case FieldSummary:
		return FieldType{Kind: FieldText}, true
	}
	spelling, declared := s[name]
	if !declared {
		return FieldType{}, false
	}
	parsed, err := ParseFieldType(spelling)
	if err != nil {
		return FieldType{}, false
	}
	return parsed, true
}

// Check reports every way r falls short of s, each Problem naming the receipt field and saying
// how to fix it — the error a child sees when the engine bounces its `finish` call. An ok receipt
// carries every declared field; a partial or blocked one may leave any of them out. A field the
// spec does not declare is refused. Problems come back in a stable order: status, summary, then
// the fields by name. An empty result means r is well formed.
func (s ReceiptSpec) Check(r Receipt) []Problem {
	var problems []Problem
	if !isStatus(r.Status) {
		problems = append(problems, Problem{Field: FieldStatus, Message: fmt.Sprintf(
			"status is %q; set it to one of %s", r.Status, strings.Join(statuses, ", "),
		)})
	}
	problems = append(problems, checkSummary(r.Summary)...)

	for _, name := range sortedKeys(r.Fields) {
		spelling, declared := s[name]
		if !declared {
			problems = append(problems, Problem{Field: name, Message: undeclaredFieldMessage(name, s)})
			continue
		}
		fieldType, err := ParseFieldType(spelling)
		if err != nil {
			problems = append(problems, Problem{Field: name, Message: err.Error()})
			continue
		}
		if message := checkValue(fieldType, r.Fields[name]); message != "" {
			problems = append(problems, Problem{Field: name, Message: message})
		}
	}

	if r.Status == StatusOK {
		for _, name := range sortedKeys(s) {
			if _, present := r.Fields[name]; !present {
				problems = append(problems, Problem{Field: name, Message: fmt.Sprintf(
					"field %q is missing; an ok receipt carries every declared field (%s)",
					name, strings.Join(sortedKeys(s), ", "),
				)})
			}
		}
	}
	return problems
}

// isStatus reports whether status is one of the three receipt statuses.
func isStatus(status Status) bool {
	return status == StatusOK || status == StatusPartial || status == StatusBlocked
}

// checkSummary reports a missing or over-long summary.
func checkSummary(summary string) []Problem {
	words := len(strings.Fields(summary))
	if words == 0 {
		return []Problem{{Field: FieldSummary, Message: "summary is empty; say in one line what the item came to"}}
	}
	if words > SummaryMaxWords {
		return []Problem{{Field: FieldSummary, Message: fmt.Sprintf(
			"summary is %d words; keep it to %d or fewer — the detail belongs in the output file",
			words, SummaryMaxWords,
		)}}
	}
	return nil
}

// undeclaredFieldMessage is the fix message for a receipt field the spec does not declare.
func undeclaredFieldMessage(name string, s ReceiptSpec) string {
	if len(s) == 0 {
		return fmt.Sprintf("field %q is not part of this receipt; send only status and summary", name)
	}
	return fmt.Sprintf(
		"field %q is not part of this receipt; the fields beyond status and summary are: %s",
		name, strings.Join(sortedKeys(s), ", "),
	)
}

// checkValue returns why value does not fit fieldType, or "" when it does.
func checkValue(fieldType FieldType, value any) string {
	switch fieldType.Kind {
	case FieldInt:
		if !isInteger(value) {
			return fmt.Sprintf("value %v is not a whole number; send an integer", value)
		}
	case FieldEnum:
		text, isString := value.(string)
		if !isString || !slices.Contains(fieldType.Values, text) {
			return fmt.Sprintf("value %v is not allowed; send one of %s", value, strings.Join(fieldType.Values, ", "))
		}
	case FieldText:
		text, isString := value.(string)
		if !isString {
			return fmt.Sprintf("value %v is not text; send a string", value)
		}
		if runes := utf8.RuneCountInString(text); runes > TextMaxRunes {
			return fmt.Sprintf("text is %d characters; keep it to %d or fewer", runes, TextMaxRunes)
		}
	case FieldList:
		if !isStringList(value) {
			return fmt.Sprintf("value %v is not a list of strings; send an array of strings", value)
		}
	}
	return ""
}

// isInteger reports whether value is a whole number in any of the shapes a decoded tool call or
// a Go caller hands over.
func isInteger(value any) bool {
	switch number := value.(type) {
	case int, int32, int64:
		return true
	case float64:
		return number == math.Trunc(number) && !math.IsInf(number, 0)
	case json.Number:
		_, err := number.Int64()
		return err == nil
	}
	return false
}

// isStringList reports whether value is a list whose every element is a string.
func isStringList(value any) bool {
	switch list := value.(type) {
	case []string:
		return true
	case []any:
		for _, element := range list {
			if _, isString := element.(string); !isString {
				return false
			}
		}
		return true
	}
	return false
}

// sortedKeys returns m's keys in sorted order, so problems and fix messages are deterministic.
func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

// Problem is one thing wrong with a Plan or a Receipt: the stage it sits in (empty for a receipt
// or for the plan as a whole), the field it concerns, and a message that says how to fix it.
type Problem struct {
	Stage   string `yaml:"stage,omitempty" json:"stage,omitempty"`
	Field   string `yaml:"field,omitempty" json:"field,omitempty"`
	Message string `yaml:"message" json:"message"`
}

// String renders the problem as one line: `stage "x", field "y": message`.
func (p Problem) String() string {
	var where []string
	if p.Stage != "" {
		where = append(where, fmt.Sprintf("stage %q", p.Stage))
	}
	if p.Field != "" {
		where = append(where, fmt.Sprintf("field %q", p.Field))
	}
	if len(where) == 0 {
		return p.Message
	}
	return strings.Join(where, ", ") + ": " + p.Message
}
