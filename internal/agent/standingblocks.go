package agent

// The standing system message as ONE TABLE. The message buildRequest seeds at position 0 of every
// request projection (loop.go's standingSystem) is composed from five blocks, and everything
// that is true of the whole — the wire order, which blocks seed a message and which only ride
// along, and which lines a workspace context file may not spell — used to live in four files'
// prose and two hand-written lists. It lives here instead, in standingBlocks: one ordered row per
// block, read by standingSystem for the composition and by forgesStandingStructure (contextfiles.go)
// for the fence, so the order and the fence have exactly one author and a block added to the
// message is a row added here.
//
// Two rules the table encodes rather than each block restating:
//
// RIDE-ALONG (ADR 0023 §6 amendment, 2026-08-25 and its third addendum 2026-09-02). Only the two
// CONFIGURED sources — the rendered prompt template and the workspace context files — seed a
// message; every engine-owned block rides along, composed in only when a configured source already
// put something in the message and never on its own. standingSystem takes the empty check on the
// configured rows BEFORE any ride-along row renders, which is what keeps the documented "delete it
// to send no system prompt" configuration byte-identical on the wire, and with it the Bypass floor.
//
// POSITION (F-19, orientation.go). Every engine-owned block precedes the workspace blocks: those are
// repo-controlled text, so nothing a repo ships can be read as preceding — and thereby overriding —
// the host's own statements. The task list goes LAST of the engine's own for the same reason it
// goes ahead of the workspace's (ADR 0023's 2026-08-26 forgery argument, tasklistblock.go): it is
// model-authored text, so it sits behind every host statement and ahead of every repo one. The
// fence column is the other half of the same guard — the lines a context file would have to spell
// to pass its own prose off as a block, which fenceContent prefixes instead.
//
// The wrap-up directive is NOT a row: it is per-request, stands alone (it creates the system
// message when none exists) and is stamped by buildRequest through req.AppendToSystem.

import (
	"sync"

	"github.com/airiclenz/apogee/internal/domain"
)

// standingBlock is one row of the standing system message: how the block renders for an Agent
// ("" contributes nothing), the line openings a workspace context file may not spell — a block
// with no furniture of its own, the prompt, has none; the context files own their header AND
// their footer — and whether the block rides along (engine-owned) or seeds (configured source).
type standingBlock struct {
	name       string
	render     func(*Agent) string
	fences     []string
	ridesAlong bool
}

// standingContextFilesRow names the table's workspace-context-files row. It is a constant because
// the Budget's measurement splits the table by it (loop.go's standingMeasured: this row is the
// file-context part, every other row together is the system-prompt part), so the name the table
// writes and the name the split reads have one author.
const standingContextFilesRow = "context files"

// standingBlocks returns the table, in ADR 0023 §6 wire order: the user's standing instructions
// first, then the harness's own orientation, then — for a delegation only — what the child's
// final reply is for, then the model's own checklist, then the workspace's own conventions.
// Whatever the mechanism directives and the tool block append comes after all five.
//
// A function rather than a package-level var because the rows name the renders and the renders
// reach the fence: contextBlocks → fenceContent → forgesStandingStructure → standingFences → this
// table, which as a var initializer is an initialization cycle. The five rows are built per call
// and cost nothing worth caching.
func standingBlocks() []standingBlock {
	return []standingBlock{
		{name: "prompt", render: (*Agent).systemPrompt},
		{name: "orientation", render: (*Agent).orientationBlock, fences: []string{orientationHeader()}, ridesAlong: true},
		{name: "delegate report", render: (*Agent).delegateReportBlock, fences: []string{delegateReportFence}, ridesAlong: true},
		{name: "task list", render: (*Agent).taskListBlock, fences: []string{TaskListFence}, ridesAlong: true},
		{name: standingContextFilesRow, render: (*Agent).contextBlocks, fences: []string{contextFileHeader, contextFileFooter}},
	}
}

// standingRender is one standingBlocks row together with what it rendered for this Agent — the
// unit standingSystem joins and ContextCost counts.
type standingRender struct {
	name     string
	rendered string
}

// standingRenders renders the table for this Agent under the RIDE-ALONG rule above — the two
// configured rows first, and only when one of them rendered something the engine-owned rows —
// and returns every row in table order, "" for a row that contributed nothing, or nil when
// nothing seeds. It is the ONE walk of the table: standingSystem (loop.go) joins its non-empty
// renders into the seeded message and ContextCost (contextcost.go) counts them piece by piece,
// so the two cannot disagree on which blocks a request carries.
func (a *Agent) standingRenders() []standingRender {
	rows := standingBlocks()
	rendered := make([]standingRender, len(rows))
	seeded := false
	for i, row := range rows {
		rendered[i].name = row.name
		if row.ridesAlong {
			continue
		}
		rendered[i].rendered = row.render(a)
		seeded = seeded || rendered[i].rendered != ""
	}
	if !seeded {
		return nil
	}
	for i, row := range rows {
		if row.ridesAlong {
			rendered[i].rendered = row.render(a)
		}
	}
	return rendered
}

// standingFences returns the CLOSED list forgesStandingStructure checks a content line against:
// the table's fence column in row order, then the two lines of the advice fence an advise
// Reaction's text is delivered in (domain.RenderAdvice). The advice fence is here for the same
// reason from the other side: its header is derived from provenance so no handler can print one,
// and this is what keeps a repo file from printing one either. Derived from the table, so a row's
// fence cannot be left off the list; derived once, because the fence tests every line of every
// context file against it.
func standingFences() []string {
	standingFencesOnce.Do(func() {
		rows := standingBlocks()
		fences := make([]string, 0, len(rows)+2)
		for _, row := range rows {
			fences = append(fences, row.fences...)
		}
		standingFenceList = append(fences, domain.AdviceFencePrefix, domain.AdviceFenceClosePrefix)
	})
	return standingFenceList
}

// standingFenceList is standingFences' result, built on first use (standingFencesOnce). No
// initializer — that is what keeps it out of the cycle standingBlocks documents.
var (
	standingFencesOnce sync.Once
	standingFenceList  []string
)
