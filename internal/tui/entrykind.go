package tui

// entryKind tags a transcript entry so the renderer can prefix and style it. The set
// mirrors the C6 entry kinds (user / assistant / tool call+result / error / note).
//
// A kind is not only a tag: every kind-keyed RULE outside the paint switch — what a kind is
// called on the wire, whether it owns a collapsed/expanded block, whether it is a host note,
// whether its paint may be memoised, whether its header can blink, whether it heads a prompt —
// used to be a predicate of its own in another file, six of them across four files. They are one
// table here ([entryKindRules]), and the kind answers each question for itself.
//
// So a new kind is two edits: a row here — the const and its table entry, side by side — and a
// case in [renderEntryLines], which stays a switch because a painter is code and not a fact.
// [TestEntryKindRulesAnswerForEveryKind] fails the moment the second half of that row is
// forgotten, the way [TestFoldEventCoversEveryEventVariant] does for an Event variant.
type entryKind int

const (
	entryUser entryKind = iota
	entryAssistant
	entryToolCall
	entryToolResult
	entryError
	entryNote
	entryPresented
	entryStartup
	entryInterjected
	entrySchedule
)

// entryKindRule is one kind's answer to every question the view asks ABOUT A KIND. Each field is
// a fact about the kind alone: nothing here may depend on an entry's size, depth, text or run,
// because the answers are read on the repaint path and any answer measured at paint time would be
// stale by the next resize.
type entryKindRule struct {
	// persistedName is the kind's stable wire string in a saved session record, or "" for a kind
	// that is never serialized. It is a STRING rather than the [entryKind] iota so a future
	// reordering of the const block above cannot silently re-tag every stored entry
	// (transcriptcodec.go). A new name is ADDITIVE within transcriptVersion and needs no bump: an
	// older build simply does not find the string and skips that entry, exactly as it skips a
	// "future-variant" today.
	persistedName string

	// carriesBlockState reports whether the kind owns a collapsed/expanded block state — the gate
	// [transcript.setExpanded] and [transcript.toggleExpanded] both answer through. Five kinds do:
	// a tool call, a stray result (entryToolResult) and a scheduled Firing (entrySchedule), whose
	// retained bodies are capped when the block is collapsed — the stray result and the Firing are
	// painted by the same block painter, so they collapse and expand by the tool block's rule — and
	// the human's own two voices, the prompt (entryUser) and the interjection (entryInterjected),
	// which read as one block and collapse by one rule when they run long (layout.md, "Collapsed
	// and expanded blocks"). Every other kind — an assistant answer, a note, the start-up box —
	// paints one way whatever is asked of it, so a click on it keeps its selection meaning.
	carriesBlockState bool

	// isHostNote reports whether the kind is one the PROGRAM speaks in rather than the
	// conversation. It is the kind-level half of [isHostNote], which adds the two facts about the
	// entry a table cannot hold: standing at depth 0, and belonging to no run.
	isHostNote bool

	// cacheable reports whether a block headed by this kind may be stored in the paint cache at
	// all ([paintKey], paintcache.go). Exactly one kind may not: [transcript.refreshStartup]
	// rewrites the start-up box's facts in place without touching a single field the key reads, so
	// a cached box would keep saying "connecting" after the model bound late. The box is one small
	// block at the very top of the scrollback and is not what the cache is for.
	cacheable bool

	// hasLiveStar reports whether the kind's header can still be WAITING for something, and so is
	// the one that blinks while it waits (layout.md, "The live star"). It is the kind's half of the
	// question only: renderView asks it together with the entry's own `!done`, because a closed
	// call waits for nothing. A Firing (entrySchedule) says no BY CONSTRUCTION rather than by
	// accident — the spinner belongs to the worker driving this session's Exchange, and the session
	// is idle while a Firing runs (renderEntryLines).
	hasLiveStar bool

	// isUserPrompt reports whether the kind heads a [userBlock] — the stops ctrl+↑/↓ walk the
	// scrollback by. Only the human's SUBMITTED prompt does: an interjection is the same voice and
	// paints the same block, but it is a remark inside an Exchange rather than the top of one, so
	// jumping to it would offer a stop the reader never started a turn at (render.go).
	isUserPrompt bool
}

// entryKindRules is the behaviour table: one row per [entryKind], read by every kind-keyed rule in
// the package outside [renderEntryLines]'s paint switch.
//
// A kind with no row here answers the zero value to everything, and every zero is the SAFE answer:
// never persisted, no block state, not a host note, never cached, no star, no prompt stop. That is
// a degrade rather than a licence — [TestEntryKindRulesAnswerForEveryKind] fails on the missing
// row — and it is why the accessors below can read the map straight rather than guarding each read.
var entryKindRules = map[entryKind]entryKindRule{
	entryUser:       {persistedName: "user", carriesBlockState: true, cacheable: true, isUserPrompt: true},
	entryAssistant:  {persistedName: "assistant", cacheable: true},
	entryToolCall:   {persistedName: "toolCall", carriesBlockState: true, cacheable: true, hasLiveStar: true},
	entryToolResult: {persistedName: "toolResult", carriesBlockState: true, cacheable: true},
	entryError:      {persistedName: "error", cacheable: true},
	entryNote:       {persistedName: "note", cacheable: true, isHostNote: true},
	entryPresented:  {persistedName: "presented", cacheable: true},
	// The start-up box is opening chrome re-seeded fresh on every launch: never serialized (an
	// empty persistedName is what encodeTranscript skips on, and a "startup" string on decode is
	// unknown and skipped — symmetric), and never cached for refreshStartup's sake.
	entryStartup:     {},
	entryInterjected: {persistedName: "interjected", carriesBlockState: true, cacheable: true},
	// "schedule" — the firing block — joined transcriptVersion 1 on the additive terms above. It is
	// a host note because a Firing is the program's own headless run announcing itself in the
	// scrollback while the conversation is elsewhere (ADR 0033).
	entrySchedule: {persistedName: "schedule", carriesBlockState: true, cacheable: true, isHostNote: true},
}

// entryKindByName is the decode-side inverse of the table's persistedName column, built once at
// init so decode is a map lookup. Kinds with no wire name are left out, so an unrecognised kind
// string — a future variant, or the excluded "startup" — is not found and its entry is skipped
// rather than failing the whole replay (fromWireEntry).
var entryKindByName = func() map[string]entryKind {
	byName := make(map[string]entryKind, len(entryKindRules))
	for kind, rule := range entryKindRules {
		if rule.persistedName == "" {
			continue
		}
		byName[rule.persistedName] = kind
	}
	return byName
}()

// persistedName returns the kind's stable wire string, or "" when the kind is never serialized.
func (k entryKind) persistedName() string { return entryKindRules[k].persistedName }

// carriesBlockState reports whether entries of this kind own a collapsed/expanded block state.
func (k entryKind) carriesBlockState() bool { return entryKindRules[k].carriesBlockState }

// isHostNote reports whether this kind is one the program speaks in rather than the conversation.
// The whole question about an ENTRY is [isHostNote], which adds its depth and its run.
func (k entryKind) isHostNote() bool { return entryKindRules[k].isHostNote }

// cacheable reports whether a block headed by this kind may be memoised by the paint cache.
func (k entryKind) cacheable() bool { return entryKindRules[k].cacheable }

// hasLiveStar reports whether this kind's header is one that can blink while it waits. The caller
// pairs it with the entry's own doneness — a closed call waits for nothing.
func (k entryKind) hasLiveStar() bool { return entryKindRules[k].hasLiveStar }

// isUserPrompt reports whether this kind heads a [userBlock] — a stop in the prompt walk.
func (k entryKind) isUserPrompt() bool { return entryKindRules[k].isUserPrompt }
