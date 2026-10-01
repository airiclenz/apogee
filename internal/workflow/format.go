package workflow

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/airiclenz/apogee/internal/domain"
)

// maxListedItems is how many item lines Format lists in full. Past it only the items that did not
// end ok are listed, and the full listing is items.md (ADR 0087: the parent reads one line per
// item, so a large fan-out must not flood its context).
const maxListedItems = 40

// The words the result lines are built from.
const (
	lineSeparator  = " — "
	totalSeparator = " · "
	noReceiptText  = "no receipt"
	noSummaryText  = "(no summary)"
	verdictKey     = "verdict"
	stoppedFormat  = "stopped by the user: %d of %d done"
	listingTitle   = "# workflow "
	stageHeading   = "## "
	outputIndent   = "   output: "
	reportPrefix   = "report: "
	listingPrefix  = "items: "
)

// Format renders a Result as the text the parent reads back (ADR 0087): a `stopped by the user:
// K of N done` lead when a cancel ended it, then per fan-out stage one line per item —
// `#<n> <item> — <status> — <summary>[ k=v…]`, numbered from 1 within the stage — and a totals
// line `items N · ok A · partial B · blocked C` (unfinished, resumed and the verify verdicts added
// when there are any), then one line for each stage whose outcome is not an item — a failed merge,
// an ask that took its default, a script's receipt, a skip — then `items: <path>` and `report:
// <path>`. Past 40 items only the lines that did not end ok are listed and `items:` points to the
// full listing (Result.Listing); `report:` appears only when a merge wrote report.md. A stage
// header names each fan-out when there are several. Format reads nothing from disk.
func Format(result Result) string {
	return render(result, false)
}

// renderListing renders items.md, the full listing Format points to: every item line of every
// fan-out under its stage's heading with the item's output path, the totals, the stage notes and
// the report path.
func renderListing(result Result) string {
	return render(result, true) + "\n"
}

// render renders result as Format's result lines, or, when isFull, as items.md.
func render(result Result, isFull bool) string {
	var out strings.Builder
	fanouts := listedStages(result)
	total, done := countItems(fanouts)
	isCapped := !isFull && total > maxListedItems

	if isFull {
		out.WriteString(listingTitle + result.ID + "\n")
	}
	if result.Stopped() {
		fmt.Fprintf(&out, stoppedFormat+"\n", done, total)
	}
	for _, stage := range fanouts {
		writeStage(&out, stage, len(fanouts) > 1, isFull, isCapped)
	}
	for _, stage := range result.Stages {
		if line := noteLine(stage, result); line != "" {
			out.WriteString(line + "\n")
		}
	}
	if isCapped && result.Listing != "" {
		out.WriteString(listingPrefix + result.Listing + "\n")
	}
	if result.Report != "" {
		out.WriteString(reportPrefix + result.Report + "\n")
	}
	return strings.TrimSuffix(out.String(), "\n")
}

// writeStage writes one fan-out's header, item lines and totals line. A capped listing skips the
// items that ended ok; the full listing heads every stage and adds each item's output path.
func writeStage(out *strings.Builder, stage StageResult, isHeaded, isFull, isCapped bool) {
	switch {
	case isFull:
		out.WriteString(stageHeading + stage.Name + "\n")
	case isHeaded:
		out.WriteString(stage.Name + ":\n")
	}
	for index, item := range stage.Items {
		if isCapped && isOK(item) {
			continue
		}
		out.WriteString(itemLine(index+1, item) + "\n")
		if isFull && item.Output != "" {
			out.WriteString(outputIndent + item.Output + "\n")
		}
	}
	out.WriteString(totalsLine(stage.Tally) + "\n")
}

// listedStages returns the fan-out stages whose items the result lines list: every fanout that
// ran, in plan order. A skipped fanout has no items; its skip is a note line instead.
func listedStages(result Result) []StageResult {
	var stages []StageResult
	for _, stage := range result.Stages {
		if stage.Kind == StageFanout && stage.Phase != PhaseSkipped {
			stages = append(stages, stage)
		}
	}
	return stages
}

// countItems returns how many items the fan-outs hold and how many of them finished.
func countItems(stages []StageResult) (total, done int) {
	for _, stage := range stages {
		total += len(stage.Items)
		for _, item := range stage.Items {
			if item.Phase == PhaseDone {
				done++
			}
		}
	}
	return total, done
}

// isOK reports whether item finished on an ok receipt.
func isOK(item ItemResult) bool {
	return item.Phase == PhaseDone && item.Receipt != nil && item.Receipt.Status == StatusOK
}

// itemLine renders one item: `#<n> <item> — <status> — <summary>[ k=v…]`.
func itemLine(number int, item ItemResult) string {
	return "#" + strconv.Itoa(number) + " " + item.Label + lineSeparator + receiptText(item)
}

// receiptText renders an item's outcome: `<status> — <summary>[ k=v…]`. The status is the
// receipt's for a finished item and the item's phase (stopped, pending) for an unfinished one; the
// fields follow in key order, then the verify verdict when the item has one.
func receiptText(item ItemResult) string {
	status := ItemStatusWord(item.Phase, item.Receipt)
	summary := noReceiptText
	var pairs []string
	if item.Receipt != nil {
		summary = FirstLine(item.Receipt.Summary)
		if summary == "" {
			summary = noSummaryText
		}
		pairs = fieldPairs(item.Receipt.Fields)
	}
	if item.Verdict != "" {
		pairs = append(pairs, verdictKey+"="+string(item.Verdict))
	}
	text := status + lineSeparator + summary
	if len(pairs) > 0 {
		text += " " + strings.Join(pairs, " ")
	}
	return text
}

// fieldPairs renders a receipt's typed fields as `k=v`, in key order.
func fieldPairs(fields map[string]any) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	pairs := make([]string, len(keys))
	for index, key := range keys {
		pairs[index] = key + "=" + FieldValue(fields[key])
	}
	return pairs
}

// Domain is the receipt in the shape a domain.WorkflowPhaseEvent carries it: its status and
// summary as they are, and each typed field rendered as text — a text field verbatim, any other
// the way the result lines render it (a list joined by commas, a number as written).
func (r Receipt) Domain() domain.WorkflowReceipt {
	fields := make(map[string]string, len(r.Fields))
	for key, value := range r.Fields {
		if text, ok := value.(string); ok {
			fields[key] = text
			continue
		}
		fields[key] = FieldValue(value)
	}
	return domain.WorkflowReceipt{Status: string(r.Status), Summary: r.Summary, Fields: fields}
}

// FieldValue renders one receipt field value on one line, the way every surface shows a `k=v`
// pair: a list joined by commas, a number as written (a receipt read back from receipt.json holds
// its ints as float64), and a text quoted when it is empty or holds an `=` or any whitespace
// (unicode.IsSpace) that would blur the pair or break the line.
func FieldValue(value any) string {
	switch typed := value.(type) {
	case []string:
		return strings.Join(typed, ",")
	case []any:
		parts := make([]string, len(typed))
		for index, part := range typed {
			parts[index] = fmt.Sprint(part)
		}
		return strings.Join(parts, ",")
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case string:
		if typed == "" || strings.ContainsFunc(typed, isBlurringRune) {
			return strconv.Quote(typed)
		}
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

// isBlurringRune reports whether r, inside a text field value, would blur its `k=v` pair: an `=`,
// or any whitespace, so a `\r` or a no-break space never reads as the pair's end.
func isBlurringRune(r rune) bool {
	return r == '=' || unicode.IsSpace(r)
}

// totalsLine renders a fan-out's tally: `items N · ok A · partial B · blocked C`, then the
// unfinished and resumed counts when there are any, then the verdicts when a verify checked any
// item.
func totalsLine(tally Tally) string {
	return tally.render(true)
}

// noteLine renders what a stage came to when its items are not listed, or "" when there is
// nothing to say: `<kind> <stage>: ` then a failed merge's reason, a script's or ask's receipt, and
// the stage's note (a skip, a pick's count, an ask's default taken, a repeat's rounds, the
// finished items a stage redid). The kind
// leads so a stage named `report` or `items` never reads as the `report:` or `items:` line.
func noteLine(stage StageResult, result Result) string {
	var parts []string
	switch {
	case stage.Kind == StageMerge && stage.Phase == PhaseFailed && result.ReportMissing != "":
		parts = append(parts, "no report"+lineSeparator+result.ReportMissing)
	case (stage.Kind == StageScript || stage.Kind == StageAsk) && len(stage.Items) == 1:
		parts = append(parts, receiptText(stage.Items[0]))
	}
	if stage.Note != "" {
		parts = append(parts, stage.Note)
	}
	if len(parts) == 0 {
		return ""
	}
	return string(stage.Kind) + " " + stage.Name + ": " + strings.Join(parts, " ")
}
