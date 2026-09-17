package probe

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// contextCostTotalRow names the closing row of the table — the sum the per-piece rows add up to.
const contextCostTotalRow = "total"

// ContextCost is `apogee probe context` — what apogee itself puts in front of the model at Turn 1
// before the user's first message, piece by piece, as the engine's own report states it (ADR
// 0079). It is the FREE side of the probe split (ADR 0021): the composition root constructs an
// idle Agent under the mode it names, reads Agent.ContextCost, and closes it — nothing is sent,
// nothing is written, and the tokens column is an estimate through the uncalibrated chars→token
// ratio, which is why every token figure is spelled with a `~`.
type ContextCost struct {
	// Estimate is the engine's report — the standing rows in wire order, the tool surface, the
	// total — taken idle, so its ratio is the default and Calibrated is false.
	Estimate domain.ContextCost
	// Mode is the mode the Agent was composed under, printed in the header because the menu is
	// mode-filtered and the orientation names the mode: a Plan table and an Auto table differ.
	Mode string
	// CharsPerToken is the ratio the estimate went through, printed so the reader can judge the
	// tokens column for what it is.
	CharsPerToken float64
	// Armed counts the advise and shape Reactions the configuration arms. Their directives reach
	// the model only once a request is in flight, so the idle estimate cannot see them; a count
	// above 0 adds the trailing line that says where they are measured.
	Armed int
	// Measured holds the `--live` readings, in the order they were taken: empty for the offline
	// estimate, one reading for a plain live probe, two — as configured, then Bypass — when the
	// configuration arms advise/shape Reactions. Each reading is one `measured` column on the
	// total row; two readings also produce the delta line.
	Measured []ContextCostMeasured
}

// ContextCostMeasured is one live Turn-1 reading — what the server itself counted for the fixed
// one-word request the probe sent — labelled by the column it prints under.
type ContextCostMeasured struct {
	// Label names the column: `measured` when it is the only reading, `as configured` and
	// `bypass` when a Reactions delta was taken.
	Label string
	// PromptTokens is the server's own prompt_tokens for the Turn-1 request.
	PromptTokens int
	// CachedPromptTokens is the share of PromptTokens the server answered from its prefix cache,
	// printed beside the count only when it is above zero.
	CachedPromptTokens int
}

// The two column labels a Reactions delta prints under, and the one a plain live probe prints
// under.
const (
	ContextCostColumnMeasured     = "measured"
	ContextCostColumnAsConfigured = "as configured"
	ContextCostColumnBypass       = "bypass"
)

// Report renders the table: a one-line header naming the mode and the ratio, one row per piece
// with its bytes and `~` token estimate, the total, and — when Armed is above 0 — the line naming
// what the estimate does not see. Plain text, one column layout, no trailing newline.
//
// A live report (Measured non-empty) keeps the same rows — re-rendered by the producer through the
// calibrated ratio, which the header then labels `calibrated` — and adds a column line naming the
// measured column(s), the measured count(s) on the total row, and, with two readings, the line
// stating what the armed Reactions added. The armed line is the estimate's and is not printed on
// a live report: the delta line is the measurement it pointed at.
func (c ContextCost) Report() string {
	lines := []string{c.header()}
	if len(c.Measured) > 0 {
		lines = append(lines, c.columnLine())
	}
	for _, row := range c.Estimate.Rows {
		lines = append(lines, contextCostRow(row.Name, row.Bytes, row.Tokens))
	}
	total := contextCostRow(contextCostTotalRow, c.Estimate.Bytes, c.Estimate.Tokens)
	for _, m := range c.Measured {
		total += m.cell(m.String())
	}
	lines = append(lines, total)
	switch {
	case len(c.Measured) >= 2:
		lines = append(lines, fmt.Sprintf("Reactions add %d tokens at Turn 1",
			c.Measured[0].PromptTokens-c.Measured[1].PromptTokens))
	case len(c.Measured) == 0 && c.Armed > 0:
		lines = append(lines, fmt.Sprintf(
			"%d advise/shape %s armed — their directives are measured with --live",
			c.Armed, plural(c.Armed, "Reaction", "Reactions")))
	}
	return strings.Join(lines, "\n")
}

// header spells the first line: the mode, then either `estimate` with the default ratio or the
// calibrated ratio marked as such.
func (c ContextCost) header() string {
	ratio := fmt.Sprintf("estimate, ~%.1f chars/token", c.CharsPerToken)
	if c.Estimate.Calibrated {
		ratio = fmt.Sprintf("~%.1f chars/token, calibrated", c.CharsPerToken)
	}
	return fmt.Sprintf("Context cost — what apogee puts in front of the model at Turn 1 (mode %s; %s)",
		orUnknown(c.Mode), ratio)
}

// columnLine names the columns of a live table — the two estimate columns over their figures and
// one cell per measured reading — so the counts on the total row read as what they are.
func (c ContextCost) columnLine() string {
	line := fmt.Sprintf("  %-18s%9s%7s", "", "bytes", "tokens")
	for _, m := range c.Measured {
		line += m.cell(m.Label)
	}
	return line
}

// String spells the reading as its cell: the prompt tokens, with the cached share in brackets
// when the server reported one.
func (m ContextCostMeasured) String() string {
	if m.CachedPromptTokens > 0 {
		return fmt.Sprintf("%d (%d cached)", m.PromptTokens, m.CachedPromptTokens)
	}
	return strconv.Itoa(m.PromptTokens)
}

// cell right-aligns text in the reading's column: the column is as wide as the wider of the
// label and the count plus a three-space gutter, so the column line and the total row line up
// whatever the count's magnitude.
func (m ContextCostMeasured) cell(text string) string {
	width := max(len([]rune(m.Label)), len([]rune(m.String()))) + 3
	return fmt.Sprintf("%*s", width, text)
}

// contextCostRow spells one row: the name padded to the widest name the engine emits (`tool
// instructions`), the bytes right-aligned with thousands separators, and the token estimate under
// its `~`. The widths are fixed so the columns line up across every row and every mode.
func contextCostRow(name string, bytes, tokens int) string {
	return fmt.Sprintf("  %-18s%7s B%7s", name, groupThousands(bytes), "~"+strconv.Itoa(tokens))
}

// groupThousands spells n with a comma every three digits (1,043 / 17,368), the spelling the
// bytes column takes so a five-figure menu reads at a glance.
func groupThousands(n int) string {
	digits := strconv.Itoa(n)
	if n < 0 {
		return "-" + groupThousands(-n)
	}
	var out strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(d)
	}
	return out.String()
}

// plural picks the singular or plural spelling for n.
func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return singular
	}
	return pluralForm
}
