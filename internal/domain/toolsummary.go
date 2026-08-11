package domain

// ----------------------------------------------------------------------------
// Tool summaries — the structured half of a tool's outcome (beside the prose half)
// ----------------------------------------------------------------------------

// A tool's outcome has TWO halves. ToolResult.Content is the prose half: it is written
// for the MODEL, and its wording is free to change. A ToolSummary is the structured
// half: it is written for a HOST — the TUI's tool card today, a headless or bench
// renderer later — and it carries the facts the tool already computed for its own
// header (the read span, the byte count, the diffstat) as data rather than as a
// sentence a reader has to parse back out.
//
// It exists because a host that wants those facts otherwise has to re-derive them from
// the prose, which is a cross-package contract with no type: a wording change in a tool
// silently degrades the card, with no compiler nudge and no failing test in the package
// that changed. Text written for a model is not an interface.
//
// SEALED, exactly like Event (events.go): ToolSummary's marker method is unexported, so
// the variant set stays owned by Apogee and is versioned additively. External code
// switches on the concrete variants — the root facade re-exports every one — but cannot
// add a variant. Unlike Event there is deliberately NO exported base struct to embed:
// each variant carries its own one-line marker method, because an embeddable base would
// re-open the sum.
//
// OPTIONAL BY CONSTRUCTION, so ADR 0002's open tool extension point is untouched: a
// summary is an extra a built-in tool may attach, never something a Tool must produce.
// An embedder's tool emits none, ToolResult.Summary stays nil, and the host renders the
// prose exactly as it does today.
//
// NEVER PERSISTED: a summary describes what a tool did during one call and has no wire
// form. domain.Message carries only Content, and the TUI's session codec stores the
// RENDERED tool view rather than the ToolResult, so no summary reaches disk and a
// session written before a variant existed reopens unchanged.

// ToolSummary is the sealed sum type of the structured outcomes a tool may report
// alongside its prose Content. The variants are the seven below; the marker method is
// unexported, so no package outside internal/* can add one.
type ToolSummary interface {
	isToolSummary() // sealing marker; carries no data
}

// ReadSpan is read_file's outcome: which lines of the file were returned, how many lines
// the file has in total, and where an optional locate term was found in it. Start and End
// are 1-based and inclusive.
//
// The locate fields make ReadSpan UNCOMPARABLE (LocatedOn is a slice), so a host that
// wants to know whether two results differ must compare field by field rather than with
// == (see internal/agent/hookrun.go's toolResultChanged).
type ReadSpan struct {
	Start, End, Total int

	// Locate is the requested locate term; "" when none was requested.
	Locate string
	// LocatedOn holds the ABSOLUTE 1-based line numbers the term was found on. The whole
	// file is scanned, so a match outside the returned Start–End span is still listed. A
	// set Locate with an empty LocatedOn means a term was requested and matched nothing,
	// which the prose cannot distinguish without a prefix test.
	LocatedOn []int
}

func (ReadSpan) isToolSummary() {}

// WroteBytes is write_file's outcome: how many bytes were written to the target.
type WroteBytes struct{ Bytes int }

func (WroteBytes) isToolSummary() {}

// ListedEntries is list_dir's outcome: how many entries the directory holds in total, and
// how many of them the listing skipped before the first one shown — the pagination offset
// the tool's header states, clamped to Total. Skipped is NOT the tool's truncation cap on a
// large directory; that is a separate mechanism, and no variant reports it.
type ListedEntries struct{ Total, Skipped int }

func (ListedEntries) isToolSummary() {}

// MatchedLines is grep's outcome: how many lines matched in total, zero included — a
// search that found nothing reports Total 0 rather than no summary at all.
type MatchedLines struct{ Total int }

func (MatchedLines) isToolSummary() {}

// DiffStat is view_diff's outcome: how many lines the diff adds and removes, counted
// from the diff operations the tool built rather than from the text it rendered.
type DiffStat struct{ Added, Removed int }

func (DiffStat) isToolSummary() {}

// SearchHits is web_search's outcome: how many structured results the search returned.
type SearchHits struct{ Count int }

func (SearchHits) isToolSummary() {}

// OpenedFile is open_file's outcome: the size of the file body and where an optional
// locate term was found in it.
type OpenedFile struct {
	Lines     int    // lines in the file body
	Locate    string // the requested locate term; "" when none was
	LocatedOn []int  // 1-based line numbers it was found on
}

func (OpenedFile) isToolSummary() {}
