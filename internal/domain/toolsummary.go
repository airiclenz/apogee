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
// NO WIRE FORM: a summary describes what a tool did during one call, and the summary VALUE
// itself never reaches disk. domain.Message carries only Content, and the TUI's session codec
// stores the RENDERED tool view rather than the ToolResult, so no ToolSummary is ever decoded
// back (fromWireToolView re-runs no presenter) and a session written before a variant existed
// reopens unchanged. What a HOST's codec may do is mirror the FACTS a variant carries onto a
// wire type of its own, where a replayed view cannot be re-composed without them — the TUI does
// exactly that for the Edit regions below (wireEditRegion, ADR 0052 §5). That mirror is the
// codec's own additive contract about its rendering; it is not a wire form for this type.

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

// EditRegion is one changed region of an applied edit: the lines the edit removed and the
// lines it inserted, with up to three unchanged lines of context each side. The tool counts
// all of it from the operations it applied, at the moment it holds both sides of the change
// in hand — a view that recovered the same facts later would have to re-read the file and
// would race the next edit (ADR 0052).
//
// Neighbouring changes stay SEPARATE regions whose context TILES the lines between them:
// the earlier region takes up to three of them as its Trailing, the later takes whatever is
// left as its Leading. Leading and Trailing therefore never overlap between regions — no
// line is context for two regions at once — and a gap of at most six lines comes out
// adjacent in line numbering, which a consumer paints end to end without an elision between
// them and without de-duplicating lines.
type EditRegion struct {
	// BeforeStart is the 1-based line the region starts on in the BEFORE file, leading
	// context included; AfterStart is the same line in the AFTER file. With no leading
	// context — a region at the head of the file — that is the first changed line.
	BeforeStart int
	AfterStart  int

	// Leading and Trailing are the unchanged lines bracketing the change, at most three
	// each and fewer at the head or tail of a file, where there are no more to give.
	Leading []string
	// Removed and Inserted are the region's changed lines, in file order. A pure insertion
	// leaves Removed empty; a pure deletion leaves Inserted empty.
	Removed  []string
	Inserted []string
	Trailing []string
}

// EditRegions is the three edit tools' outcome — edit_existing_file, single_find_and_replace
// and multi_find_and_replace: every changed region of the edit just applied, in file order.
// It rides the Tool summary contract unchanged, which is to say it is DISPLAY DATA — never sent
// to the model, and the summary VALUE has no wire form — and it is what the Split diff and its
// Stacked reading are painted from (ADR 0052). The TUI's session codec does mirror the region
// FACTS onto a wire type of its own (wireEditRegion), because the Split reading is composed at
// paint time against the live width and has nothing to compose from once the regions are gone;
// that persistence is the codec's rendering contract (ADR 0052 §5), not this type's. A result
// carrying no regions, because the pair was over budget to diff or because an embedder's own
// tool attached nothing, renders the argument-derived list exactly as before.
type EditRegions struct{ Regions []EditRegion }

func (EditRegions) isToolSummary() {}

// Stat returns these regions' diffstat: Added and Removed summed over every region's
// inserted and removed lines. It is the ONE derivation of that pair — the card's +A −R slot
// and the tests read it here instead of each recounting the regions — so what counts as a
// changed line moves in a single place. The zero EditRegions yields the zero DiffStat.
func (e EditRegions) Stat() DiffStat {
	var stat DiffStat
	for _, region := range e.Regions {
		stat.Added += len(region.Inserted)
		stat.Removed += len(region.Removed)
	}
	return stat
}

// SearchHits is web_search's outcome: how many structured results the search returned.
type SearchHits struct{ Count int }

func (SearchHits) isToolSummary() {}
