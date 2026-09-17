package agent

import (
	"github.com/airiclenz/apogee/internal/domain"
)

// The Context cost report's PRODUCER (ADR 0079). ContextFilesReport (contextfiles.go) measures
// the standing system message as one string — the whole seed, joins included — and that number,
// StandingTokens, stays what it is. ContextCost is the sibling read over the same renders, piece
// by piece: it takes the one walk of the standingBlocks table standingSystem takes
// (standingRenders, standingblocks.go) and adds the tool surface exactly as the wire seam
// (wire.go) sends it. No render path exists here that the request does not take, so a row added
// to the table is counted here without a change.

// contextCostToolMenu and contextCostToolInstructions name the two tool-surface rows: the native
// tools array, or the text block a non-native profile carries in its place.
const (
	contextCostToolMenu         = "tool menu"
	contextCostToolInstructions = "tool instructions"
)

// ContextCost reports what this Agent puts in front of the model at Turn 1 before any user
// message — each standing block that seeds the position-0 system message, in wire order, and the
// tool surface — with the bytes each piece sends and the token estimate those bytes come to
// through the session's chars→token ratio; Calibrated says whether that ratio has been folded
// toward a server's own count yet.
//
// The rows are wire-faithful, which is what makes the report worth charting. The standing rows
// follow standingSystem's ride-along rule to the letter: the engine-owned blocks (orientation,
// delegate report, task list) are rendered — and counted — only when a configured source (the
// prompt template or the workspace context files) seeds the message, so a session that seeds
// nothing reports no standing row at all. The tool surface is one row, never two: on a native
// profile the menu goes as the tools array (`tool menu`, measured PromptChars-style); on a
// non-native profile toProviderRequest folds the rendered instruction block into the system
// channel and suppresses the array, so `tool instructions` REPLACES `tool menu`. A piece that
// renders nothing has no row.
//
// Like ContextFilesReport this is an idle-only read: it renders the prompt (Mode, date, scratch
// dir), composes the mode-filtered menu and reads the token accounting, so a Driver calls it with
// no worker driving the Agent.
func (a *Agent) ContextCost() domain.ContextCost {
	budget := a.budget()
	report := domain.ContextCost{Calibrated: a.tokens.Calibrated()}
	add := func(name string, bytes int) {
		if bytes == 0 {
			return
		}
		report.Rows = append(report.Rows, domain.ContextCostRow{
			Name:   name,
			Bytes:  bytes,
			Tokens: budget.EstimateTokens(bytes),
		})
		report.Bytes += bytes
	}

	for _, row := range a.standingRenders() {
		add(row.name, len(row.rendered))
	}

	menu := a.toolMenu()
	if block := a.toolInstructions(menu); block != "" {
		add(contextCostToolInstructions, len(block))
	} else {
		add(contextCostToolMenu, domain.PromptChars(nil, menu))
	}

	report.Tokens = budget.EstimateTokens(report.Bytes)
	return report
}
