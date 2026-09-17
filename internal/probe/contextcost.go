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
}

// Report renders the table: a one-line header naming the mode and the ratio, one row per piece
// with its bytes and `~` token estimate, the total, and — when Armed is above 0 — the line naming
// what the estimate does not see. Plain text, one column layout, no trailing newline.
func (c ContextCost) Report() string {
	lines := []string{fmt.Sprintf(
		"Context cost — what apogee puts in front of the model at Turn 1 (mode %s; estimate, ~%.1f chars/token)",
		orUnknown(c.Mode), c.CharsPerToken)}
	for _, row := range c.Estimate.Rows {
		lines = append(lines, contextCostRow(row.Name, row.Bytes, row.Tokens))
	}
	lines = append(lines, contextCostRow(contextCostTotalRow, c.Estimate.Bytes, c.Estimate.Tokens))
	if c.Armed > 0 {
		lines = append(lines, fmt.Sprintf(
			"%d advise/shape %s armed — their directives are measured with --live",
			c.Armed, plural(c.Armed, "Reaction", "Reactions")))
	}
	return strings.Join(lines, "\n")
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
