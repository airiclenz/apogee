// Package tui is the Bubble Tea terminal UI: a thin renderer over the agent's
// typed Events that supplies the Approval delegate. It holds no agent logic — it
// only renders Events and sends user input (broad plan §4; phase-2 detail plan §1).
//
// It depends on the engine only through the narrow [Engine] interface and on the
// public types through internal/domain; it never imports the root module path, so the
// ADR-0010 invariant "internal/* never imports root" holds (phase-2 detail plan §3 C5).
//
// Phase 2 build order: P2.0 landed the seam boundary (the [Engine] interface, [Options],
// and the [Run] entry point). P2.1 landed the concurrency seam — the worker-goroutine
// engine driver ([startExchange]/[driveExchange]), the Event→Msg bridge ([teaSink]), and
// the approval rendezvous ([uiApprover]), all late-bound to the running program through
// the [Bridge] (phase-2 detail plan §3 C1–C5; ADR 0011) and proven under -race against a
// stub program. P2.2 lands the Bubble Tea skeleton that drives them: the [Model] with its
// four-state machine, the input box, the transcript viewport, and the status line, with
// [Run] now building the [tea.Program] and binding the [Bridge] to it. The Charm v2 stack
// (bubbletea/bubbles/lipgloss, all on the charm.land path) is taken over the v1 fallback.
// The rich event fold (P2.3) and the Approval UI keys (P2.4) build on this skeleton.
//
// P2.7 (the pre-Phase-3 TUI presentation pass) reshapes the look to layout.md and splits the
// rendering into reusable seams the Phase-3 tool fan-out and sub-agent work extend rather than
// rework: [theme] holds the palette, glyphs, and styles; toolview.go turns a tool call+result
// into a compact [toolView], keyed by an OPEN, name-keyed registry of presentation vocabulary
// (each later tool adds one entry; where a card's facts come from is the tool-summary paragraph
// below); render.go is the line-oriented renderer (the full-width user block, ✦ assistant/tool
// headers, ┝/┕ tool-detail branches, depth indenting, and the [userBlock] ranges the sticky
// header rides on so the owning prompt sticks to the top while a reply streams). The
// transcript now groups a tool call with its result by ToolCall ID, the input box is a rounded,
// auto-growing black field, and the chrome is a braille status line plus a footer bar (host alias
// ✦ model ✦ workdir, then the mode behind its own symbol ("◐ ask before", [modeMarker]) — the
// workspace written with the home directory as `~`,
// [workdirDisplay], resolved once at construction). The live token gauge (reserved at P2.7) is now
// wired: the post-v1 track folds each top-level UsageEvent's total into the status-line
// context-fill gauge, measured against the discovered context window ([Model.contextGauge] /
// [Model.statusRight]) — which is why the window is stated THERE and no longer in the footer, since
// the gauge is the one place the number moves. The red/green diff detail reserved there has its
// producer: the Phase-3 view_diff tool (diffBody).
//
// P3.14 turns the Depth-tolerating renderer into a Depth-rendering one: a sub-agent (Depth > 0)
// run is framed as a vertical-ruled sub-section — each line carries one "│ " rail gutter per
// nesting level ([railLines]). The framing lives entirely in render.go (the value-copied Model holds
// no new state — the run boundary is derived from each entry's depth inside
// [transcript.renderView]), so the flat Depth==0 transcript renders byte-for-byte as before
// (ADR 0011 still holds — render only).
// The frame is CONTINUOUS across the one blank row layout.md puts between blocks: that separator is
// itself railed, at the JOIN — the min — of the depths of the two blocks it sits between
// ([railJoin], fed by a per-appended-block depth rather than the loop's per-entry one, since a block
// ending on an OPEN delegation reports the depth of the span that follows it). The min rule is the
// whole run-boundary logic: inside a run every join is ≥ 1 so the rail never breaks, a climb-out
// keeps only the rails both sides reach, and two consecutive sub_agent calls are never joined,
// because the second call's own tool-call block sits at the PARENT's depth — the join dips there and
// the first run's frame ends before the second opens one. The one separator that is not a spacer is
// the join climbing out of an expanded group member's span INTO the next row of that member's list:
// there it is the single ┊ closing the span. Every other climb-out keeps its spacer, because the
// spec draws the closer only where another grouped sub-agent follows the expanded one — a group's
// last member and a lone delegation alike close against nothing and so show none
// (docs/layout/tool-layout.md, "Grouped Sub-agents"). Nothing announces a DESCENT any more — the
// delegation's own ┌─┶ header row opens the frame and the rail runs down from it, on the live
// preview's path as much as on the committed one. What opens it is the delegation being OPEN and not
// its having produced anything yet ([subAgentFramed]): a delegate's first words are in the streaming
// buffer and in no entry at all, so a frame waiting for the span to exist would snap open under the
// reader the moment those words settled. The whole frame —
// rail, corner and closer alike — is one style role (theme's subRail) in the tool-header gold (the
// scheme's `tool-header` role), coherent with the gold ✦ tool markers; the arm and tee reaching
// across to the delegation's branch stay in that row's own detail tone ([paintRowMarker]). A run's stretch stays CONTIGUOUS however its events
// arrive: a concurrent fan-out interleaves N children's events at one depth, and each entry is
// placed at the end of the run its spawning call id names ([transcript.place], ADR 0039) instead of
// at the end of the list — so every rule here reads adjacency off the entries exactly as it did
// while delegation was serial. A HOST note — a `· cancelled` line, a Firing block, anything the
// program says to the human rather than the conversation ([isHostNote]) — is worded at whatever
// moment it happens, which is routinely the middle of somebody's run, so it lands at the tail and
// the run's remaining entries slide in FRONT of it ([transcript.tailBeforeHostNotes]): the note
// keeps its depth-0 unrailed block and stands after the stretch it interrupted, and neither a run's
// span nor a fan-out group's adjacency is ever cut by one. That whole frame is what a COLLAPSED run elides: by
// default a sub_agent call block and the span of deeper entries behind it are ONE block ([subAgentSpan],
// [renderSubAgentRun]), its summary slot carrying the run's transitive tool-call count, the
// delegate's own context fill where it has reported one ([subAgentFill] — not transitive, since each
// agent fills a window of its own), and its gist ([subAgentGist]). While the run WORKS that cell is
// empty — count and fill are the whole line, and both hold still between one landing and the next —
// save for the single word `delegating`, which stands only while the span's MOST RECENT open call
// is itself a sub_agent: the one live fact the run's own blocks cannot stand in for, since opening
// that row shows a nested run that is itself collapsed. Once the report lands the gist
// becomes that report's own first line, the one durable thing it had to say. The head's own
// report body is elided with that frame, so a collapsed run reads as ONE summarised line and never
// repeats in a body row what the summary slot just said; the framing and the full report are what
// expanding it reveals, each inner block in its own state (layout.md, "Collapsed and expanded
// blocks"). That summary slot is the one thing the fold does NOT take: an open head wears the very
// line its shut row wore ([expandedSubAgentView]) and adds the report under it, since a row that
// dropped the count and the fill on the way open would say LESS about the delegation than leaving
// it shut. An OPEN run's railed span opens on the prompt the delegate was handed — the retained
// task text rendered as markdown behind the rail ([subAgentPromptRows]) — so the frame reads as
// what was ASKED, then the work, then what came back; the header row above it says what the
// delegation IS and how the run went, and the prompt is the half only the span has room for. While anything behind a block's header is still waiting for a result, that
// header's ✦ blinks against a bare cell ([blockState.star]) on the STATUS SPINNER's half-second
// phase ([spinnerAnim.blink]) — the transcript keeps no clock of its own, and the spinner tick
// repaints the viewport only on the tick that flips that phase and only while
// [transcript.hasOpenToolCall] holds, so an idle chain costs no repaints.
//
// The chat mini-language (post-v1 apogee-code feature-parity) adds a thin parse/route layer
// between the input box and the engine without thickening the renderer (ADR 0011 still holds):
// command.go is a pure [parseInput] that classifies a line as a local /command or an agent
// message and extracts @file references; autocomplete.go is the suggestion overlay (ONE merged menu
// of commands and skills on a "/" token, a bounded os.Root workspace-file listing on "@") rendered
// above the input like the approval-prompt slot. Every region is scoped to the TOKEN AT THE CARET
// (caretToken — the word it stands in or just after, found by scanning to the whitespace on either
// side), so neither a draft already in the box nor a caret sent back into the middle of it shuts the
// menu out, accept splices over that token's own range rather than to the end of the buffer (the
// caret re-seated after it through offsetToLineCol), and accepting a command row RUNS the command
// — [Model.acceptAutocomplete] cuts the verb out and leaves the rest of the draft standing, which
// is why [Model.runCommand] never touches the editor and its callers prepare it instead. A row
// that takes arguments is the carve-out: it COMPLETES to "/verb " and waits for them rather than
// firing unfinished, unless it also carries commandSpec.runsBareAtAccept — /model and /server,
// whose bare form only opens a picker — which puts it back among the rows that run. The
// whole-input form keeps ownership of arguments ("/confine off --save"), so ⏎ on a finished token
// falls through to submit exactly there and executes at accept everywhere else. All three regions
// stay open while the model WORKS — the namespace is most wanted exactly where it used to vanish,
// on the message being composed for next — under the per-command policy the table carries
// (commandSpec.whileRunning, read off the parsed line by [parsedInput.safeWhileRunning] so
// "/confine" and "/confine off" can differ): the reporting verbs run mid-run, every other row is
// TAGGED "— idle only" in the menu and earns commandsAtIdleNote instead of running, and skill and
// file tokens are simply message content that rides the interjection. /clear (aliased by /new) and
// /compact drive the engine's context seams ([Engine.ClearContext]/[Engine.Compact]); /version and
// /skills are pure reports written straight into the scrollback; /schedule and /schedule-stop drive
// the scheduler library through the [Scheduler] seam and are the first argument-taking verbs that
// are ALSO live mid-run, because a Schedule fires as a separate headless run and touches this
// session's engine at no point (schedule.go, ADR 0033); /confine reports and toggles
// Auto's blast radius through [Engine.SetConfineToWorkspace], one of the two argument-taking verbs
// with a grammar of its own (/color-scheme is the other): [parseConfine] reads "status | off
// [--save] | on" where the remaining rows are handed a plain token list (/schedule apart: its
// prompt form reads the line's raw tail, [parsedInput.rest], because a prompt is text the human
// wrote and not a token list to be re-spaced), and an argument it does not understand is a parse
// error carrying the usage line — never a silent no-op on the command that widens what Auto may
// touch; @file *resolution* stays in the agent loop (reusing the workspace
// fence), so the TUI only parses references — it never reads files itself. The settled questions
// behind all of it — one namespace, tokens not chips, accept-executes, caret-aware regions, the
// while-running policy, resolve-gated accents, the sole-token guard — are recorded in ADR 0027.
//
// confine.go is the routing half of that verb (ADR 0012, amendment 2026-07-21): [Model.runConfine]
// asserts the requested blast radius on the [Engine] and records a transcript note whose pure
// builders state the radius in plain words — never as repairing a malfunction, because a host that
// cannot fence is the ladder working as specified, not a fault. The diagnostic facts the status
// report names (which backend answered, what it can enforce, the host id an acknowledgement is
// recorded against) arrive as [Options.Confinement] from the composition root — the renderer never
// imports internal/platform — while the *effective* setting is read live off
// [Engine.ConfineToWorkspace], since the user can change it mid-session. `--save` delegates the
// config write to [Options.SaveHostAcknowledgement] for the same reason the session saver is a
// seam: file paths and formats are the binary's business, and a save that fails or is unwired
// never invalidates the session toggle that already happened.
//
// effort.go is the routing half of `/effort`, the session end of the Thinking-effort dial (ADR
// 0050): [Model.runEffort] layers a level above the bound model profile's own `thinking.effort:`
// through [Engine.SetEffortOverride], clears it again on `auto` with the zero value, and — in every
// form, the bare report included — closes with one note built by the pure [effortResolutionNote]
// stating the effective effort AND both layers behind it, because the same word means one thing as
// an override (it survives a model switch) and another as a profile setting (a switch replaces it).
// The layers are re-read off [Engine.ThinkingEffort] AFTER the write rather than taken from the
// argument, the /confine posture. Nothing here persists: the override is session intent, the config
// key is the durable door, and the verb is safe mid-Exchange because the value is read when the next
// request is built.
//
// undo.go is the routing half of `/undo`, the human end of the engine's per-exchange pre-image
// journal (ADR 0051): [Model.runUndo] reads the top un-undone group off [Engine.UndoPreview] and
// records the note that DISCLOSES it — every recorded path at its resolved spelling, classified
// restore / delete / skip-with-reason — then `/undo confirm` hands [Engine.UndoRevert] the
// generation that preview carried, so a journal which moved in between refuses and earns a fresh
// preview instead of reverting a step nobody read ([Model.undoGeneration] is where that stamp
// waits). It is the one command verb that writes to the human's files, which is what makes it
// idle-only where /confine's report is not: the group it would revert is the one a running Step is
// still filling. The notes are built by the pure [undoPreviewNote], [undoReportNote] and
// [undoNothingNote] — the last of which states the journal's LIFETIME as well as its emptiness,
// because it is memory and not storage, so a resumed session can never reach an earlier process's
// writes.
//
// The skill flow (post-v1 apogee-code feature-parity) is the mini-language's second half, and it
// is TEXT rather than state beside it: a skill is invoked by naming its id as a "/token" at a word
// boundary in the message — "/code-audit please check the parser" — exactly as an @path names a
// file. extractSkillRefs collects the tokens the [SkillCatalog] confirms (any other /word inside a
// message is prose, so a path or a typo travels untouched), the token STAYS in the text the model
// reads, and the ids ride out as [domain.UserInput.SkillIDs] on a submitted message and on a staged
// interjection alike. Like @file, *resolution* (turning an ID into the prepended skill body) stays
// in the agent loop, through Config.Skills. The merged "/" menu offers the catalog beside the
// commands (marked with glyphSkill, and accepting one writes its token), with commands SHADOWING a
// skill of the same id — the collision is settled menu-side, so the parse layer never has to know
// skills exist. A shadowed skill stays invocable by typing its "/id" token anywhere but at the head
// of the line, which is the only position the whole-input command rule claims.
// The one input that is neither command nor message is the SOLE-TOKEN guard (kindUnknownSlash): an
// input that is nothing but one /word naming no verb and no skill is refused with a note and left
// in the box ([Model.refuseUnknownSlash], at idle and mid-run alike), because a mistyped invocation
// silently sent to the model is the confusion the mini-language exists to remove.
//
// inputaccent.go is what makes all of that VISIBLE while it is typed: a post-render pass over the
// prompt block that accents a token exactly when it RESOLVES — a "/id" the catalog confirms, an
// "@path" the workspace listing holds — and leaves everything else plain, so a typo or a
// non-existent file fails to light up instead of failing at submit. Both grammars are located by
// the one scanner the extractors read (refSpan, command.go), so what lights up is by construction
// what would be acted on; the byte ranges become visual cells through a mirror of the widget's own
// soft-wrap (wrapRowStarts, pinned against a real textarea), and [Model.inputView] composes the
// pass BEFORE the drag-selection so a selection wins over any accent it covers. The render path
// never walks the disk for it (fileCache.holds answers from the listing it already has).
//
// presenter.go supplies the last host delegate, and it is the one that decides rather than asks:
// [uiPresenter] is the Presenter present_document routes a finished deliverable to, and it walks
// the presentation ladder itself (ADR 0019). It shares [uiAsker]'s seam — called inside a Step, on
// the worker goroutine, through the same late-bound programRef — minus the rendezvous: it picks a
// rung from the [Presentation] the composition root installed ([Bridge.SetPresentation]), attempts
// it, sends a [presentedMsg] and returns, so a presentation can never park a Turn on the UI. The
// ladder gates the opener on LOCALITY only and lets internal/present answer whether this machine
// has anything to open into, so the desktop test lives in exactly one place and a configured
// present.command — which deliberately stands in for that test — is not second-guessed here. The
// Update loop folds the message into a transcript entry of its own (entryPresented,
// renderPresentedBlock): the ▤ block that is deliberately not shaped like a tool card, whose path
// and URL are emitted as raw plain text because terminal linkification is the whole mechanism.
//
// Three files round out the renderer without touching the state machine. markdown.go turns the
// common markdown subset in assistant text (**bold**, <u>underline</u> — the one HTML pair, since
// markdown spells no underline of its own — # headings, `inline`/fenced code, bullet/
// numbered lists, GFM pipe tables) into styled physical lines — a spare, pure, lipgloss-only
// renderer matching toolregistry.go's posture, with render.go still owning the marker and depth
// framing. Tables are the one construct with a file of their own, mdtable.go: it parses the block
// and lays it out as aligned columns ruled by a faint │ and, horizontally, by one ─ under the header
// and another between each pair of adjacent body rows — every rule crossing every divider at a ┼,
// and no outer frame — measuring the RENDERED cell so markup and colour escapes never widen a column,
// WRAPPING a cell too wide for its column inside that column rather than cutting it — so a row is
// as many lines as its tallest cell and nothing is ever dropped — and falling back to plain
// paragraphs where the width cannot give every column its four-cell readable floor (layout.md is
// the visual contract). filecache.go
// backs the "@" overlay with a short-TTL, single-walk workspace listing filtered in memory, so a
// typing burst reuses one os.Root walk instead of re-scanning the disk per keystroke. mouse.go
// implements click-to-position caret and drag-to-select (with OSC52 copy) in TWO rectangles —
// apogee captures the mouse for transcript scrolling, which turns off the terminal's own click-drag
// selection, so both are re-implemented here, the prompt's in rune offsets into the textarea Value
// and the transcript's in content coordinates over the cached rendered lines ("copy what you see").
// A THIRD rectangle joins them while /settings is open: the pane's row list, where a click selects a
// key, the wheel walks the list and the row being typed into takes a caret seat and a drag of its own
// ([Model.settingsPaint], whose geometry is the painter's own — renderPopupPlaced reports where the
// rows landed rather than the mouse re-deriving it) — and, where the multi-line prompt field has
// replaced that list, the field itself, over all of its rows ([Model.settingsTextPaint], which reads
// the same placement plus the wrap the painter chose). A FOURTH and a FIFTH join them while /usage and
// /inspect are open, and they are ONE rectangle written once (reportpane.go), not two: a report has
// nothing to select, so a click inside it is only swallowed, a click outside dismisses it and then
// goes on to whatever it named, and the wheel scrolls its rows ([Model.reportWindow], reading the
// same painter's placement). They are also the two panes that can be up TOGETHER, so the report is
// asked first and the raw-protocol pane right after it, in the order the slot draws them. The
// handlers arbitrate by region, so no two of them coexist. Scope is the owner's rule, not an
// accident of routing: the TRANSCRIPT selects in every state, while the PROMPT follows
// [Model.inputEditable] — idle, ask, running — and stays inert at approval/errored, where a/d/s and
// Enter-dismiss own the keyboard and the transcript covers copying. A transcript selection survives
// a repaint by the KEEP-IF-UNCHANGED rule ([transcriptSel.spanUnchanged], applied in
// [Model.refreshViewport] against the outgoing lines): it lives on exactly while every line it spans
// is identical before and after, so a drag over settled text keeps extending while the model streams
// beneath it, and it drops the moment the text under it moves (the streaming tail, a rewrap, a tool
// call joining its group). Freezing repaints under a held button was rejected — the stream must not
// visibly stall — and the release slices the very lines the rule protected, which is what makes copy
// equal sight by construction rather than by care. clipboard.go holds the second half of the copy
// itself: OSC52 stays the primary and SSH-safe channel, and beside it a best-effort write to the
// host's own clipboard program covers the terminals that ignore the escape, behind one injectable
// package-level seam so a test can watch what a copy actually hands over.
//
// blockcursor.go is that same reach from the KEYBOARD: a modal block cursor over the transcript
// (docs/layout/tool-layout.md, design call 7). ⌥↑/⌥↓ enter the walk and move it, plain ↑/↓ move
// inside it, ⏎ opens or closes what the highlight stands on, and esc — or typing, which ends the
// mode and still lands in the prompt — hands the keys back. It walks the PAINT'S OWN click map
// ([lineTarget], render.go), collapsed to one stop per surface, so the levels a pointer can open are
// exactly the stops a cursor has and "deepest visible" needs no second rule; ⏎ goes through the
// mouse's own [Model.toggleBlockAt], so both ways in flip and anchor identically. The state is two
// plain fields on the value-copied Model — on/off and the CONTENT line under the highlight — re-
// seated against every fresh paint in [Model.refreshViewport] ([blockCursor.clamp]), suspended
// (keys and highlight together) while an approval or ask prompt owns ↑/↓/⏎, and painted as the
// theme's one selection field, which the drag-selection it replaces on entry can therefore never be
// confused with.
//
// Module map — the input cluster has its own home (review candidate #3). prompteditor.go lifts the
// loose input-side concerns the architecture review called one coherent concept — the textarea, the
// autocomplete overlay (+ its skillRegion edge-trigger), the workspace file cache, and the prompt
// drag-selection — into a [promptEditor] type the [Model] embeds anonymously. Field and
// self-contained-method promotion keeps the value-copied Model idiom and every call site unchanged
// (m.input, m.autocomplete, m.caretTo(...) resolve through it). The lift is deliberately partial:
// only methods touching nothing but the editor's own fields move there (newPromptEditor,
// submitParse, reset, rows, and the caret re-seat family caretTo/reseatCaret for a VISUAL row and
// seatCaret — which caretToOffset and reseatInput both express — for a LOGICAL one, plus the offset
// pair caretByteOffset/caretToOffset a mid-draft completion splices by); methods that also read
// Model-owned state — theme, width/height, opts, lifecycle — stay on the Model rather than
// duplicate that state (computeAutocomplete, acceptAutocomplete, insertSkillToken, highlightInput,
// accentTokens, inputContentRect, the region-arbitrating mouse handlers). One level below it,
// lineeditor.go holds what a text FIELD is — the textarea and that whole caret family — as
// [lineEditor]: promptEditor embeds it, and every popup-painted surface that is typed into builds a
// single-line one of its own ([newPopupField] — the /settings value row, the picker's filter and the
// /sessions browser's, the /sessions rename row), so a caret that moves correctly is written once,
// "what does backspace do" is answered once, and a config value never inherits the chat box's
// vocabulary (recall, submit, the "/" and "@" overlays). A field painted
// inside the popup module cannot use the widget's own View or the real terminal cursor — the module
// styles rows whole and takes plain cells — so it renders through textWithCaret, a caret glyph AT
// the offset, and the glyph is the FIELD's own ([lineEditor.caret]) because the surfaces disagree on
// it (the filter line's ▌, the rename row's and the value row's ▏). The Model stays the
// coordinator that owns the lifecycle state machine, the transcript + render cache, the
// stats/gauge, the theme, and the layout; the editor never touches the engine. The empty box's
// invitation is state the Model SETS, not a render-time choice: setPlaceholder swaps the idle legend
// ("⏎ send") for runningPlaceholder ("⏎ queue · esc stop") on the lifecycle transitions that open and
// close an Exchange, so the chrome names what ⏎ will actually do — which is also why the ask
// rendezvous swaps BACK to the idle legend while it borrows the box for an answer. The idle side of
// that swap is two constants rather than one, because a key it names is not on every terminal: ⇧⏎
// reaches the program only where the enhanced keyboard protocol's key disambiguation was negotiated,
// and everywhere else the terminal folds the chord into a plain ⏎ — which is a SEND, so advertising
// it unconditionally promises a newline and delivers a sent message. idlePlaceholder therefore names
// ⌥⏎ alone and idleShiftPlaceholder names ⇧⏎/⌥⏎, idleLegend() picks between them off the editor's own
// keyDisambiguation flag, and the tea.KeyboardEnhancementsMsg fold in prompteditor.go sets that flag when the
// terminal answers bubbletea's query — repainting an already-drawn idle legend in place, since capable
// terminals answer a few frames after the first one is on screen. The startup default is the
// pessimistic form: a terminal that never answers keeps the ⌥⏎-only legend, which is the honest
// reading rather than a guess.
//
// Typing while the model works is that swap's substance, and it is a three-party split ADR 0025
// records: this package STAGES, the worker DELIVERS, the engine COMMITS. interject.go holds the
// TUI's two thirds — [interjectBox], the per-Exchange mutex-guarded FIFO the Update goroutine pushes
// into and the worker drains between Steps (held by POINTER, per the no-copy invariant below: a
// mutex copied by value would give each Model copy its own lock and unsynchronise the two goroutines
// silently rather than fail loudly), and the staging half around it — ⏎ turns the editor into a
// queued row, Backspace on an empty box pops the newest back into it (withdrawing it from the
// mailbox first, and giving up if the worker already took it, so a message cannot be sent twice),
// the delivery report moves exactly the rows that LANDED into the transcript as ⧖ blocks, and a
// terminal boundary rules on whatever is left: flushed as ONE joined message on a natural
// completion, HELD under a note after Esc or a fault, because Esc means stop everything. A stop
// holds what was DELIVERED too — the worker skips a drain whose ctx is already cancelled, and the
// cancel fold re-stages what it did deliver, since AbortExchange takes those rows out of the
// conversation and nothing else would put them back (ADR 0025 decision 7, as amended). Keys reach
// the box wherever [Model.inputEditable] says so — the same predicate the mouse arbitrates by, which
// is what keeps the keyboard and the mouse from disagreeing about which states are live — and the
// cost is the deliberate one the plan named: single-key transcript scrolling while running, with
// PgUp/PgDn and the wheel keeping it in every state.
//
// The caret under all that is the REAL terminal cursor. The bubbles textarea's virtual one is
// retired (SetVirtualCursor(false)) and View attaches textarea.Cursor(), translated by
// inputContentRect's content origin, onto tea.View.Cursor — but only where inputEditable holds, so
// at approval and errored the cursor is simply absent rather than blurred out of a still-focused
// widget. It never blinks, and its shape is the `cursor-shape` config key (block | underline | bar,
// parsed once here through [ParseCursorShape], the ParseSpinnerStyle posture): Bubble Tea's renderer
// must name a shape on every frame, so "inherit the terminal's own configured shape" is not
// expressible while the app runs and the key is the honest substitute. cursor_test.go pins the
// translation, the hidden states, and the shape.
//
// activity.go replaces the status line's turn index — which answered nothing the human was
// asking — with a live activity phrase and an elapsed clock ("thinking · 12s", "reading · 3s",
// "sub-agent · searching · 6s"). A tool's phrase is the presented VERB and nothing else: the target
// it used to carry restated the tool-call block one row beneath it and routinely pushed the context
// gauge off the row, so it stays with the block. No surface words verb and target together any
// more: a collapsed sub-agent run's gist was the last one that did, and it now says nothing while
// the child works (see the transcript section above). Dropping the target makes the elapsed clock's
// key a real question: back-to-back reads
// both word themselves "reading", so a clock restarted on a change of TEXT would count the first
// file's call straight through the second. A tool activity therefore carries the id of the call it
// describes (domain.ToolCall.ID) and restarts when that changes; every other kind keeps the
// phrase-change rule, since for those the text is what actually changed.
// [Model.foldActivity] derives all of it from the same
// Event stream the transcript folds (including [domain.ReasoningEvent], the observability seam
// that makes "thinking" a fact rather than a guess), and the transitions no Event announces —
// submit, /compact, the stop key, the worker's terminal Msg — set it directly. It adds no
// lifecycle state: compacting and stopping are activities, not uiStates, so the ADR 0011 state
// machine is untouched and only statusLine's running branch consults it. The per-tool verb it
// renders comes from the same open registry toolregistry.go already keys by tool name.
//
// The same file carries the STALL GUARD, which answers the half of that question a phrase alone
// cannot: whether anything is still coming. [Model.lastEvent] is when the engine was last heard
// from — every Event stamps it, at any depth and of any variant, and so does an activity move to a
// kind the guard WATCHES, which is either an Event moving it or a worker just launched
// (moveActivity, gated by activityKind.isQuietWatched so the restamp can never outgrow what quiet
// reports), the seam that keeps a fresh Exchange from inheriting the silence of the one before it. Nothing on a timer touches it:
// a heartbeat or a spinner frame proves the TUI is alive, which was never the question. Once the
// gap to now passes the `ui.stall-after` threshold, statusLine qualifies the phrase with a bare
// amber " · quiet" in front of the activity's own clock — "thinking · quiet · 21m 03s"
// ([Model.runningPhrase], activity.quiet holds the rule). One clock, not two: the silence and the
// activity are the same span in the case the guard was built for, so the qualifier says only THAT
// nothing is arriving. It is a FACT and never a verdict — a large prompt is legitimately silent
// for a minute or two, and the incident that motivated it (2026-08-14: a bare "thinking" for
// twenty minutes) ended in a normal completion — so the row stops at the fact, and drops the
// qualifier the moment an Event lands. It speaks only for thinking and responding: a silent tool call is the
// tool taking its time, a stopping worker already answers for itself, and the states waiting on the
// HUMAN never show it, because the silence there is the human's own. It is also the first thing the
// left slot gives up on a narrow row, dropped whole rather than truncated (layout.md).
//
// reasoning.go retains what that phrase is ABOUT, and renders none of it. NOTHING IN THE VIEW
// READS [Model.reasoning] — no tail row, no transcript entry, no config key — and the "thinking"
// above still comes from the ARRIVAL of a [domain.ReasoningEvent] rather than from these bytes. It
// is the retention seam a future reasoning display will be built on, landed ahead of that display
// on purpose: the three rules such a display would live or die by settle here, where they are
// testable, instead of inside a renderer already painting. The chunk is escape-stripped at THIS
// seam (a ReasoningEvent's Text is raw model output that may carry ESC bytes — the invariant
// below, extended to reasoning's one entrance); the buffer is BOUNDED to the last
// [reasoningTailCap] bytes, dropped from the front on a rune boundary, because the Model is copied
// on every Update (ADR 0011) and a Turn may reason for an hour; and it holds ONE agent's reasoning
// at a time, keyed on the same depth-and-spawn identity the activity line uses, since a fan-out's
// delegates interleave their chunks in one stream and a concatenation of them is a sentence nobody
// wrote. [Model.foldReasoning] is the fourth fold foldEvent runs and the only writer, with a
// StreamResetEvent (the Turn is superseded) and a MessageEvent (the Turn is committed, and its
// reasoning_content is the canonical copy) each ending a tail, beside the worker boundaries
// launchExchange and finishWorker.
//
// spinner.go paints the glyph that phrase runs beside, and the animation is this package's own
// rather than a charm.land/bubbles/v2/spinner widget: the widget renders frames[i] through one
// fixed style, which leaves no room for a glyph CHOSEN per frame or a colour COMPUTED per frame,
// and the styles need both. Three animations live in one registry keyed by [SpinnerStyle] — snake
// (six dots walking the outer ring of the 4×4 dot grid two braille cells form side by side, a lap
// a second), glitter (the braille block sorted by density, re-rolled at 20 fps under a six-second
// breath), and classic, the eight-cell rotation apogee shipped before, which stays a first-class
// choice rather than a deprecated fallback — while the ten-second Oklch colour loop is a FLAG
// beside the style, never a property of one, so all six style × colour combinations render and
// [spinnerAnim.view] is the single place the two compose. cmd/apogee's file-only `ui:` block
// selects both ([Options.Spinner], [Options.SpinnerColor]); theme.go keeps the field the glyph is
// painted on (spinnerBase) and the loop's colour stops ([theme.spinnerStops], so a scheme switch
// moves the loop with it), never the frames.
//
// Every frame is a pure function of a frame counter, and that is ADR 0011 rather than taste: the
// Model is copied on every Update, so the animation state may hold no RNG handle — a *rand.Rand
// would be shared across the copies and advance from the ones Update discards, so the same frame
// would paint differently depending on how often View ran — which is why glitter HASHES
// (frame, cell) into its density bucket instead of drawing from a generator, and why the state is
// four plain ints. What the widget did give for free is chain safety: its TickMsg carried a tag,
// so re-arming while a tick was still in flight could not leave two chains running. [spinnerAnim]
// reproduces that as a generation counter — arm opens a new generation and the Update loop drops a
// tick carrying an older one — which is what keeps the frame rate from doubling after an approval
// prompt or an ask_user question re-arms the chain.
//
// The upstream heartbeat is the package's SECOND tick chain, and it deliberately reuses the
// first one's shape. [ServerHost.Beat] is a narrow seam the binary backs with an
// internal/heartbeat monitor — this package imports that one for the Beat value and the Interval
// cadence, never internal/provider (ADR 0010) — and the Model owns only WHEN: [Model.Init] fires
// the first beat immediately, because startup discovery IS that beat now (the binary paints before
// the server has answered), and the beatMsg fold re-arms from the LANDED beat rather than off a
// fixed clock, so an observation and its wait are strictly sequential and two beats can never
// overlap. The spinner's generation counter guards it identically: a Msg from a retired chain is
// inert — and it is [Model.armBeat], not [Model.beatCmd], that every out-of-rhythm beat goes out
// through (a committed `/server` switch, a completed profile load), because opening a generation is
// what RETIRES the chain already running. A beat merely issued on the current generation would leave
// two live chains polling one server, which is the frame-rate doubling spinnerAnim.arm exists to
// prevent, one seam across. What a beat MEANS is [Model.foldBeat] — a failure while an Exchange is in flight is
// IGNORED (a streaming reply is stronger evidence that the server is there than a timed-out probe
// on a saturated one), a failure before any beat has ever landed is believed at once (a cold start
// against a stopped server should say so), and otherwise the offline crossing waits for
// offlineFailureThreshold consecutive idle failures, each crossing noted exactly once (the
// saveFailing fail-once posture). The consequence is [Model.blockedUpstream]: the three paths a
// HUMAN opens an Exchange with — a message, /continue, /compact — are refused with a note while
// there is nothing to send to, and the typed message STAYS IN THE BOX (a held interjection queue
// stays held for the same reason), while scrollback, /clear, /sessions, /version, /confine and
// Shift+Tab all stay live. The interjection auto-flush (ADR 0025) is a FOURTH opener and is
// deliberately ungated: it runs inside a natural completion's own fold, where foldBeat's own rule —
// a failed beat during an in-flight Exchange is ignored — means the offline state cannot have moved
// since the Exchange that just completed was allowed to start. An unwired seam arms no chain, folds
// nothing and blocks nothing, which is exactly the pre-heartbeat renderer.
//
// What a beat DOES about a changed upstream is the second act, [ServerHost.Rebind], and the split
// between the two is the design: the heartbeat seam observes, the rebind seam applies, and the
// binary owns everything in between — re-resolving the per-model system prompt (ADR 0023), the
// validated set (ADR 0016), the mechanisms registry and the compaction budget, then driving
// Agent.Rebind. This package decides only WHEN. [Model.observeBinding] measures each landed beat
// against the last OBSERVATION rather than against the binding — which is what lets a
// `context-window:` pin outrank the server's window forever without the renderer knowing a pin
// exists — and a change is applied at once when the engine is idle, or stashed as a latest-wins
// pendingRebind and applied at the next quiescent boundary when something else owns the engine:
// finishWorker when a worker does (the boundary AbortExchange and the idle save use), and the
// actuation completion fold while a launcher verb owns the server the session talks to, since that
// completion may re-point the session itself. [Model.applyRebind] then adopts what was actually BOUND
// (never merely what was observed), restates the start-up box in place (transcript.refreshStartup —
// its facts were frozen when it was seeded, and a late-bound session would otherwise keep a
// "connecting" box at the top of its scrollback), and words the change once: connected / model
// changed / context window changed, a refusal noted once per distinct target, and nothing at all
// when a pin means nothing visible moved or when the session's FIRST beat lands clean (the restated
// box is already saying it). A host that cannot rebind is a display-frozen heartbeat — the offline state and the
// model list still live, no binding ever does.
//
// A SERVER switch (`/server`) is the same machinery one level up, and ADR 0024's "cold start, late
// seed and mid-session switch are ONE code path" is what makes it small. [ServerHost.Switch] is
// the binary's half — it re-points the provider client, swaps in a Monitor for the new server, and
// returns the display facts — while [Model.foldServerSwitch] is this package's: the Options adopt
// the endpoint, the alias and the surviving window pin, the model is UNBOUND (the footer says
// "connecting…" and blockedUpstream refuses a send, exactly as at a cold start), and the whole
// heartbeatState is replaced rather than patched. That replacement is the design: the fresh
// generation retires the old chain so every beat and tick still in flight lands inert, the offering
// empties with the server that advertised it, and the offline debounce returns to its cold-start
// posture, so a dead new server is believed on its first failed beat instead of being debounced
// against evidence about a different machine. The first beat of the new chain fires at once rather
// than one Interval later, and the ordinary rebind path binds whatever it reports. The one fact
// carried across the reset is heartbeatState.switched, which defeats the quiet first-contact seed:
// a launch stays silent because the start-up box a few rows above says it all, while a session that
// has moved says "connected: <model>" — the box is far up the scrollback by then, and the human
// asked.
//
// The /model picker (picker.go) is UI over that machinery and deliberately NOT a third way to bind.
// The offering every beat carries is already held in heartbeatState.models, so the overlay derives
// its rows from it at render time — a beat landing under an open picker refreshes the list in place,
// with the selection clamped — and an accepted row becomes a [rebindIntent] fed to the very same
// [Model.applyRebind]: one seam call, one set of words, one fail-once posture, whether the switch was
// observed or asked for. The one thing the accept adds is that it records the pick as the last
// OBSERVATION before calling the seam, exactly as [Model.observeBinding] records one, so the next
// beat on a multi-model server measures the picked model as "nothing new" instead of binding the
// session straight back off the config'd discovery hint. Everything the picker cannot do it SAYS: no
// monitor, an offline server, a nil rebind seam, an empty offering — each is one honest note and no
// overlay, and picking the row the session is already on is answered too (rebindNote's "" contract is
// about the observations nobody asked for, not about an explicit act). An explicit pick is also a
// CHOICE, the way a committed switch is: the id that BOUND goes to [Options.RecordModelChoice], which
// writes it as this server's `model:` key while `remember-model:` is on and skips it silently
// otherwise. It hangs off the accept path ([Model.bindPickedModel]) rather than off applyRebind,
// because that orchestration is shared with the heartbeat and a rebind the beat merely OBSERVED is
// news about the server rather than a choice — recording there would write config nobody asked for.
// /server is the SAME overlay
// over [ServerHost.List] (one pickerKind, no callback field on the value-copied state), with the
// current row marked by entry NAME rather than by id (the name is the entry's identity, ADR 0036
// decision 1, so a sibling entry sharing an endpoint is a switchable row of its own) and the accept
// calling Switch instead of
// applyRebind; both verbs also take their choice as an argument ("/model <id>", "/server <name>"),
// both are idle-only by the commandSpecs table, and /server's whole degrade ladder is one line —
// an unwired seam and an empty list are the same situation for the human. A committed switch is also
// a CHOICE (ADR 0036): the name goes to [ServerHost.RecordChoice], which writes it as the entry
// the next session starts on when the binary recognises it as a configured one and skips it silently
// otherwise — the renderer offers every name and believes the answer, stating the recording at the
// end of the move's own note and a failed write as a footnote under it (prebound.go).
//
// A session may also start with NO server, and that is the one state in which the renderer opens a
// pane nobody asked for (prebound.go, ADR 0036). The binary could not resolve which `servers:` entry
// this run starts on — none recorded, the recorded one gone, or nothing configured — and the TUI is
// the one Driver that can ASK rather than refuse, so [Options.Prebound] carries the reason and the
// same /server picker comes up under a notice saying why. Its accept goes one level lower than a
// switch's: [ServerHost.Bind] CONSTRUCTS the engine that does not exist yet
// ([Model.bindToServer]), the display adopts the result exactly as it adopts a move, and the name is
// handed to [ServerHost.RecordChoice] as the one the next session starts on. Nothing else about
// the state is a new surface: no beat goes out while there is nothing to observe, a typed message
// re-opens the ask instead of reaching the absent engine, the reason with nothing to pick opens
// /settings instead, and the status line carries the standing fact that outlives the esc closing
// either pane. One predicate ([Model.prebound]) is the whole of the state, and the single write that
// clears it — a committed bind — ends it everywhere at once.
//
// The second pane nobody asked for is the start-up key-migration offer (keymigration.go, ADR 0047),
// and it is the same overlay again: a `servers:` entry whose API key is a literal `api-key:` line in
// the config file, on a machine with a secret store apogee can both write to and read back from,
// earns one three-row question — move it, not now, never for this entry. [Options.KeyMigration]
// carries the entry NAMES and the store's human name and never a key; each answer is one call to
// [Options.MigrateKey] or [Options.KeepPlaintextKey], the SaveHostAcknowledgement contract, so the
// store, the read-back verification and the file format all stay the binary's business. A round of
// several entries rides the overlay's own queue — one pane each, the next opening where the last
// closed, esc ending the round with nothing persisted — and the whole offer gives way to the
// pre-bound ask above, because a session with no server has the more urgent question and "not now"
// is what this one already means.
//
// That fold has ONE owner (post-v0.8 architecture deepening, review candidate 06). fold.go's
// [Model.foldEvent] is the single door every engine Event enters the view through: the Update
// loop's eventMsg case hands it over and does nothing else with it, and foldEvent runs the four
// folds a view update is made of — [Model.foldStats] (which moved there out of model.go, a file it
// belonged to only because [Model] does), [Model.foldReasoning], transcript.apply, then
// [Model.foldActivity] — in the one order that works. That order used to be enforced by three comments in three files; it is now a
// data dependency, because foldEvent reads the open-tool-call fact transcript.apply establishes and
// PASSES it to foldActivity, which can no longer ask for it early. TestFoldEventCoversEveryEventVariant
// parses internal/domain/events.go and fails on a variant with no row in the fold table, so a new
// Event variant has to be answered for — including with "deliberately nothing".
//
// The tool-call layout pass (post-v1.5.0) tightens how a session reads without touching what the
// model sees: committed assistant text is trimmed of its leading and trailing blank lines
// (trimBlankLines) and interior blank runs collapse outside fenced code, so layout.md's "exactly
// one empty line between blocks" holds; a tool header drops its square brackets for a bold-gold
// label (the [theme] toolLabel role, styled before the wrap — the markdown.go posture); and
// consecutive same-label calls at the same depth fold into one block (toolCallRun /
// groupable) headed by "✦ Label (N)". Grouping is render-time only — the append-only entry list, the
// call/result pairing, and transcript.hasOpenToolCall are untouched, so a call arriving mid-stream
// joins its group on the next repaint. What a call CARRIES has stopped mattering to it: a Terminal
// call and its output group like a batch of reads, each member held to one row with its body behind an
// indicator of its own (renderGroupMember), and a presenter that needs its block left alone says so
// outright (toolView.solo — the answered ask_user record, and the sub_agent call whose block heads
// a whole run even when the run came to nothing).
//
// The shape a tool call takes is uniform, and one renderer draws it: [renderToolBlock] takes a
// slice of [toolView] — a lone call is a slice of one — and emits a ✦ header carrying the **label
// alone, never a target**, then one ┝/┕ branch per call led by that call's target
// ([renderToolBranch]). A call's outcome is split in two, and that split — not any line count —
// is the grammar: the one-line [toolView] Summary fills the branch row's right-aligned outcome
// slot, a dotted ⋯ leader flexing between it and the target ("┕ main.go ⋯⋯⋯ 154 lines",
// "┕ main.go ⋯⋯⋯ +2 −2"; an in-flight call has none yet and lets the dots run to the row's edge),
// while the Details body lays out beneath at the branch marker's width
// ([renderSubDetails]) rather than sprouting branches of its own — a Terminal call's output, a
// diff's coloured lines under their diffstat. A call with no target at all is the one shape with no
// target line: its body, closed by its summary, is rendered as the branches themselves
// ([renderDetails] — the stray-result and unregistered-tool fallbacks). One grammar covers both
// counts — the reason the standalone and grouped paths were converged rather than kept in sync —
// and a body of one line lays out exactly like a body of ten. Where they part is the ROW BUDGET a
// group imposes and the STATE it hands out: a collapsed member is one line wearing its own ▶ at the
// block's right edge, and a member opens ALONE — the group header toggles nothing, each member is
// painted by its own entry's expanded flag and every row it paints is marked back to that entry
// ([renderToolGroup], [renderExpandedMember], [blockPaint.addFor]) — where a block of one spends up
// to three rows collapsed and toggles from ANY row it paints, header, leader row and body alike, the
// whole-surface rule the prompt block already followed. The `+N more lines` marker is the one row
// that does not: it belongs to the collapsed paint, so a click there only ever opens. Whichever
// shape it is, opening also lifts the block's TEXT a step out of the collapsed dim
// ([detailTone], the scheme's `muted-bright` role) while the chrome around it — indicator, marker,
// gutter — and the BAND a diff line sits on stay where they were, so what brightens is exactly what
// the reader opened the block to read.
// A scheduled Firing borrows that whole shape
// through the same painter under a leading glyph of its own — ⟳ rather than the star
// ([blockState]'s glyph override, whose zero value is the star; schedule.go, layout.md's "The firing
// block") — so the surface is one grammar while the MEANING stays a tool call's alone: the entry
// kind is entrySchedule, and grouping, the sub-agent span and the live status line go on keying on
// entryToolCall (ADR 0033). TestTranscriptLayoutGolden pins the whole rendered scrollback.
//
// A card's FACTS arrive as data; only its WORDING is this package's (post-v0.8 architecture
// deepening, review candidate 03). The tool presenter used to reconstruct what a tool had done by
// pattern-matching the free-text result the tool wrote for the MODEL — five regexes and the five
// extractors around them, over seven of the registry's 21 entries. That was a cross-package
// contract with no type (this package does not import internal/tools), so a wording change over
// there silently degraded a card here, with no compiler nudge and no failing test in the package
// that changed. A tool now attaches a sealed [domain.ToolSummary] beside its prose Content and
// the registry's per-tool stat hook words it, with [viewDiffRegions] cutting view_diff's printed
// diff into the numbered regions its coloured body paints beneath that stat ([diffBody] staying
// the plain floor for output that carries no diff tags to walk) and [gitDiffRangeRegions] doing the
// same for the diff git prints, one section per file it spans, over the plain output rendering that
// stays that block's own floor. What the registry keeps is presentation vocabulary — label,
// verb, target, stat — plus the detail extractor that stays the FLOOR for a result carrying no summary:
// a third-party tool, or any built-in that attaches none, still renders its first line exactly as
// before. What did NOT fully go is the READING: six stat hooks — testVerdictStat, foundFilesStat,
// changedFilesStat, commitCountStat, commitHashStat, diffLinesStat — still word their slot off a
// fixed header the tool writes into its own output, because design call 14 rules out growing the
// engine for presentation. That residue is a documented trade rather than an oversight, and
// toolregistry.go states it beside the hooks: each reading is anchored on a token the tool formats
// deliberately and each is TOTAL, so a shape it does not recognise returns false and leaves that
// tool's prose floor in the slot rather than a wrong number — a wording change over there degrades
// such a card to what it showed before this existed instead of lying in it.
// The wording stays the view's own; that several lines read like the tool's own header is
// what made "the rendered output does not change, byte for byte" a checkable oracle for the
// change, not a contract, and this package may reword without touching a tool.
// toolsummary_pin_test.go executes all nine summary-bearing tools for real and asserts the
// rendered line — the cross-package pin the old regexes never had.
//
// The rest of the package, one line each, so this narration names every file in it: tui.go is the
// seam boundary the binary sees ([Run], [Options], and the [Engine], [SkillCatalog] and
// [SessionHost] interfaces); bridge.go the late-bound programRef every seam sends the running
// program through — including [Bridge.NotifySchedule], the composition root's own way in, which is
// how a Firing narrates from the scheduler's goroutine without the renderer knowing one exists, and
// [Bridge.NotifyRouting] beside it, the same way in for the Sub-agent server's routing state (ADR
// 0045); sink.go the Event→Msg [teaSink]; messages.go the plain values those sends carry
// into Update; approver.go and asker.go the two cross-goroutine rendezvous that park a Step on a
// human ([uiApprover] on an approval decision, [uiAsker] on a typed answer); worker.go the
// cancellable engine driver; model.go the [Model] itself — the lifecycle state machine, the
// layout, the frame's one stacking order and the block spans View publishes while walking it
// ([frameSpans], read by every pane rectangle), the status line and the footer; sessionsave.go the record-write cluster lifted out of
// model.go (ADR 0043) — the assembled [savePayload], the per-Turn and idle saves, and the
// single-flight queue that orders every Save, Rename, Delete, Rotate and Activate against one
// another (the Model still owns the three fields it latches on); approval.go the approval-decision
// concern lifted out of model.go beside it (ADR 0043), both halves in one file — the [approvalMenu]
// the keys and the pane both read, the keypress half over it ([Model.handleApprovalKey],
// [Model.resolveApproval], [Model.sendApproval]) and the pane that paints it
// ([Model.approvalPrompt] with its Sub-agent identity line and its argument block), so a row can
// never be paintable and unreachable (the Model still owns the pending request and the menu
// selection); ask.go the ask_user pane lifted out of model.go beside approval.go (ADR 0043) and
// shaped like it, both halves in one file — the fold that borrows the input box for a question
// ([Model.foldAskRequest]), the keys the offering claims while that box is still empty
// ([Model.askChoiceKey]), the reply that sends the answer back and gives the box up
// ([Model.submitAnswer], [Model.checkedLabels], [Model.restoreAskDraft]) and the pane that paints
// the question with its choices ([Model.askPrompt], [askChoiceRows] and the row/line budgeting
// around them), so a choice can never be paintable and unreachable (the Model still owns the
// pending question, the highlight and the ticked set); commandrun.go the third cluster lifted out
// of model.go (ADR 0043) — what a
// recognised /command DOES ([Model.runCommand]'s switchboard, [Model.startNewSession]'s session
// reset, [Model.launchExchange]'s worker start) beside the refusals an unrunnable line meets
// ([Model.refuseUnknownSlash], [Model.refuseIdleOnlyCommand]) and the [Model.commandRunnable] gate
// both invocation routes share, while the parse that classifies the line stays in command.go and
// [Model.submit] stays with the input concern; heartbeat.go the fourth cluster lifted out of
// model.go beside them (ADR 0043) — the upstream heartbeat end to end (ADR 0024): the
// [heartbeatState] the footer and the send gate read, the tick chain that keeps it current
// ([Model.beatCmd], [Model.armBeat], [Model.beatTick]), the folds a beat, a failure or a
// /server switch lands in ([Model.foldBeat], [Model.foldBeatFailure], [Model.foldServerSwitch]),
// the re-binding an advertised model earns at a quiescent boundary ([rebindIntent],
// [Model.observeBinding], [Model.applyRebind], [Model.applyPendingRebind]) and the
// [Model.blockedUpstream] refusal the offline state is spent on (the Model still owns the
// [heartbeatState] field itself); theme.go the palette, the
// marker glyphs, and the
// lipgloss styles, with colorscheme.go the routing half of the `/color-scheme` verb that swaps
// that palette under the running frame (status, pick, and the `export` that is the only way to
// get a built-in scheme onto disk to edit — internal/scheme owns what a scheme IS, ADR 0040);
// width.go the display-width authority the theme carries — one measure for the
// whole TUI, and it is whichever one the painter itself is using; inputaccent.go the
// resolve-gated inline accents the prompt box paints its
// "/id" and @file tokens with; transcript.go the append-only scrollback model, entrykind.go the
// [entryKind] enum beside the behaviour table every kind-keyed rule outside the paint switch reads
// — what a kind is called on the wire, whether it owns a block state, whether it is a host note,
// whether it may be cached, whether its header blinks, whether it heads a prompt — so a new kind is
// a const row and its table row, then a case in [renderEntryLines]; and transcriptcodec.go the
// versioned wire form of that scrollback inside a saved session record; sessions.go the /sessions history browser;
// schedule.go the /schedule surface — the status note, the cycle/mode/stop pickers and the notices
// the scheduler's own Events become, with every when-and-how decision left to internal/schedule,
// plus the one thing this package publishes rather than renders: [Options.ReportActivity], the
// busy/idle fact the binary's Gate holds a due Firing on, computed by [Model.quiescent] and sent
// from a defer in [Model.Update] so no fold can forget a transition (ADR 0033);
// autotitle.go the state machine behind a session's NAME — one cosmetic out-of-band completion
// ([Options.GenerateTitle]) fired at the first prompt's submit, in parallel with the Exchange it
// starts, which never reaches the Engine (ADR 0011), is not a Turn, enters no transcript, applies
// through Rename (the only writer of a stored title) or waits for the id that first Save mints, and
// is dropped without a word whenever it fails or a human has named the session first — plus /rename
// ([Model.runRename]), the human's half of that same machinery, which takes the name it is given or
// asks the model for one on demand and, being asked for rather than automatic, reports every
// outcome and outranks a title the human set a moment ago; sessionrule.go the session name the
// frame wears on its own top rule — the ▔ hairline above the status line, which carries the name
// centered in place of the rule runes it would otherwise draw there, and which is where
// [Model.topRule] gets both the name (the landed one, else the first prompt's heuristic, else none)
// and the cell arithmetic that centers and clips it;
// recall.go prompt recall — the per-workspace list of sent inputs the box walks with ↑/↓, where
// this package owns only WHEN (one load at start-up, one fire-and-forget append per send) while
// internal/recall owns the file and cmd/apogee owns which directory and which workspace
// ([Options.Recall]); picker.go the modal single-select overlay behind /model and /server;
// listsurface.go the surface that overlay, the /sessions browser, the /settings key list with its
// two sub-lists and the "/" | "@" dropdown all ARE underneath their own wording — the [listCursor]
// value ({selected}) every one of them embeds and the key contract it carries (arrows by a per-pane
// wrap rule, esc, the ⏎ that lands on a row), which the two soft-modal DECISION panes (approval.go,
// ask.go) borrow the walk and the clamp of without the contract, the [listSurface] the two that
// FILTER embed instead
// (that cursor plus the field typed into it, and the keys that type), the filter that prunes the
// rows and maps a highlight back to what it names, and the one budget→render call behind every list
// pane — a pane's own body block, or the filter line and its two blanks (ADR 0053);
// settings.go the /settings pane — the frame's one FULL-HEIGHT pane, listing every config key the
// binary resolved this run over one display seam ([SettingsHost.Rows]), claiming the whole
// transcript budget while it is open, and persisting ONE key per deliberate edit through
// [SettingsHost.Write] / [SettingsHost.Reset] — the renderer owning the idiom (⏎ toggles a bool,
// opens an enum's value sub-list or a string's caret buffer; backspace arms a reset a ⏎ confirms)
// while the binary owns the file AND what a key may hold: a value it refuses comes back as an error
// the row carries, with the buffer still open to correct. `mode` is the one edit that also applies
// live, through the Engine.SetMode seam Shift+Tab drives (ADR 0035);
// settingswatcher.go the two ways that same file changes from OUTSIDE the pane — a row's ⏎ opening
// the human's own editor on the key's line (foreground, which suspends the program; detached, which
// does not) and the binary's config watcher reporting a save made anywhere else — one round trip with
// two triggers, both re-reading through [Options.ReloadConfig] and both landing through the one apply
// loop, so a key edited in a terminal editor cannot take a different path from the same key edited in
// a GUI one (ADR 0041); settingsapply.go what a committed key then DOES — the armed reset (the one
// commit whose value is "remove the line"), the write, the live-apply router that turns a persisted
// value into an effect (the renderer-owned keys applied in-package because there is no engine on the
// other side of them, every other key out through the binary's dispatcher, which owns the schema —
// ADR 0037 decision 2), and the session's edit journal the row's edit marker and the value an edit
// starts from are both read off; and
// usage.go the /usage report — one row per agent of what this session has SPENT, read off the
// per-agent totals the folds already keep (the Model's for the main agent, each run head's for a
// delegate) rather than summed here, and the lightest pane in the frame: no filter, no selection,
// esc its only key; inspector.go the /inspect raw-protocol pane and the bounded ring behind it —
// the request bodies and response payloads the engine reports as domain.WireEvents while
// `ui.inspector` arms the capture, folded beside the transcript rather than into it (a wire record
// is not a conversation entry), shown in the /usage report's shape and paired request-to-reply
// within one (depth, callID) wire stream, since the one ring interleaves every run's traffic;
// reportpane.go the pane both of those two ARE — the reportPane value ({open, top}), the key
// contract, the dismiss, the budget→render path and the whole mouse family (rect, window, click,
// wheel), written once and named twice, with every rectangle in the transcript-side slot a lookup
// into the geometry View publishes while it stacks that slot (model.go) rather than a prefix sum of
// its own; popup.go the one bordered pane every overlay — those three, the autocomplete
// dropdown, the ask and approval prompts — is painted through; logo.go the embedded start-up wordmark;
// actuation.go the launcher-verb latch and the folds that close one out (ADR 0029) — at most one
// world-changing call in flight per address, narrated while it blocks, with the next Beat rather
// than the call's own return deciding what the world became, plus the start-up restore that enters
// that same latch with whatever Launch profile the binary says this server was left on
// ([Options.RestoreProfile], `remember-model:`); skills.go the browsing half of the
// skill flow — the /skills report, and [Model.knownSkillID], the single predicate the parser, the
// inline accents and the merged "/" menu all resolve a token through, so the three can never
// disagree about what a skill is; workspacepath.go the presentation-only shortening of the
// workspace root out of the paths a tool block NAMES (its target, and its own one-line summary)
// and out of nothing it QUOTES; paintcache.go the per-block paint memo that makes a streaming
// repaint cost the live tail instead of the whole scrollback — a VALIDATION cache nothing
// invalidates, safe exactly as long as [paintKey] keeps naming every input;
// diagnostics.go the two hidden rendering-diagnostic seams (`--tui-trace`, which tees the
// renderer's exact bytes to a file as quoted strings a virtual terminal can replay, and
// `--tui-diag`, the log of what the terminal told the program about itself) — portable rather than
// Windows-only, off unless a path is named, and in the repo because the alternative was measuring a
// build against a patched bubbletea (ADR 0038);
// and the three Windows-only rules about the ground the painter stands on, each a `_windows.go`
// carrying the reasoning beside an `_other.go` no-op twin the six-target cross-build pins:
// environ_windows.go names the terminal (TERM=xterm-256color, COLORTERM=truecolor, into
// bubbletea's own environment slice and never the process's) because a Windows shell leaves TERM
// empty and an empty TERM hands ultraviolet noCaps, environ_other.go being the nil the rule
// collapses to wherever the shell names the terminal itself; syncoutput.go filters
// bubbletea's mode-2026 question out of the stream and carries the measurement behind declining
// it — ConPTY forwards the synchronized-output window EMPTY and re-serializes the frame outside
// it while the ask costs the cursor-hide flicker mitigation — with syncoutput_windows.go the
// real-terminal predicate that asks for the filter and syncoutput_other.go the false that leaves
// every other host on the mode it honours; and altscreen_windows.go sets
// DISABLE_NEWLINE_AUTO_RETURN on the
// primary buffer before [Run] claims the alternate one — the ghosting fix, since the console mode
// word is per screen buffer and a buffer without that flag rewrites the bare LF ultraviolet means
// "next row, same column" by into CR LF — returning the restore closure that gives the shell back
// the console mode it lent (ADR 0038), altscreen_other.go being the never-nil no-op restore that
// stands in where there is no console mode word to lend;
// toolview.go the tool CARD itself and the lifecycle that fills it (ADR 0043), beside
// toolregistry.go, which holds the presentation vocabulary the lifecycle reads — the [toolView] a
// call becomes, with the [detailLine]s a [detailKind] colours and the [toolBody] carrying them, built by [presentToolCall] the moment the call is seen (its body
// included, for the tools whose ARGUMENTS already say what the call will change) and completed by
// [toolView.enrichWithResult] when the result lands, both leaving through
// [toolView.finishDisplay] so the escape-strip ([toolView.sanitize]) and the workspace-relative
// spelling of the paths a card NAMES ([toolView.shortenPaths]) hold for every card rather than per
// producer, plus the run aggregation over them ([runAggregate]) that words what a whole GROUP of
// calls did by ADDING the typed stat values its members carry ([statValue]) rather than reading
// their wording back out of the slots they show;
// toolregistry.go that vocabulary itself, split off beside it (ADR 0043) — the OPEN, name-keyed
// [toolRegistry] whose one entry per tool carries its label, its active verb, the extractor that reads the target off the call's arguments, the prose detail
// extractor that stays the floor for a result carrying no typed summary, and the body renderers
// ([toolPresenter]), so a new tool is one entry rather than a control-flow statement to grow; and
// the per-tool hooks those entries point at — the stat that words the right-hand outcome slot, off
// the domain.ToolSummary the tool already reports where there is one and otherwise off the call's
// own arguments or off a fixed header in the tool's own output (the residue design call 14 leaves,
// stated beside the hooks), the target extractors that read the call's arguments, and the detail
// extractors that quote a summary-less result's first line;
// splitdiff.go the SPLIT reading of a diff body — the Edit regions a tool recorded
// (domain.EditRegion) arranged as two panes, each numbering its own file, with the width rule
// that says whether that reading fits at all ([splitDiffFits], splitPaneMinCols) and the gutter,
// wrap and padding that keep the two panes level (ADR 0052, docs/layout/split-diff-layout.md);
// it composes rows and paints no block, so WHICH reading a body gets — these panes or the
// stacked rows diffbody.go builds from the same regions — stays the painter's choice, made
// per paint against the width it holds;
// diffbody.go those bodies themselves — every row a change-shaped call renders (ADR 0043), beside
// that composer: the three edit tools' bodies derived from the call's own ARGUMENTS ([changedLines] over [editPair]s), with no file read and nothing guessed,
// and the two diff tools' regions walked back out of the output they print instead
// ([viewDiffRegions] through [diffRegionCutter]; [gitDiffRangeRegions] through [gitDiffWalk],
// which reads its numbers off the hunk headers git ELIDES the gaps between); plus
// [stackedDiffLines], the one builder of the narrow reading, so that reading cannot come to
// differ per tool, and [viewDiffBody], the coloured prose that is view_diff's floor once its
// output carries no tags at all;
// toolargs.go the ONE rendering of a call's own ARGUMENTS, which both surfaces that show them
// read rather than formatting the bytes themselves (ADR 0043) — [argumentDetails]' labelled
// lines, one `name:` per argument with the value's real lines hanging beneath it, showing a
// repeated key as the last-wins reading the executor will actually run ([orderedArgs],
// [lastWins]) instead of in wire order, capping one value so a long `content` cannot evict the
// `path:` beside it and keeping that value's LAST line as well as its head ([argumentValueLines]),
// and hanging every argument-derived line at [argumentValueIndent] so nothing a model wrote can
// paint where a label of the surface's own lives; plus [resolvedPathNote], the one wording every
// decision surface discloses a redirected path with, and [parseArgs], the same bytes as the map
// the registry's target extractors read a field out of;
// textutil.go the generic text helpers none of that display owns alone — [clipDetail] and
// [clipRunes], the flood bound they spend in runes rather than in the cells the screen bills
// ([detailClipRunes], which states why and names the probe that measured it), [plural]'s naive
// count and [firstLine] / [splitLines];
// sanitize.go the escape-stripping security seam every one of those surfaces passes untrusted text
// through before a cell of it is painted (ADR 0043) — [stripEscapes], the batch form
// [stripEscapesAll] a request's choices are sanitized in one call by, and [bidiControl], the
// reordering characters that go with the control ones; it is the invariant stated at the end of
// this file, given the file of its own that a seam referenced from two dozen call sites earns;
// the renderer itself is nine files rather than one, split along the seams the painters already
// had once the tool-display overhaul grew render.go past the house ~400-line guideline — a pure
// file move, nothing renamed and nothing reworded: render.go keeps the transcript walk
// ([transcript.renderView], [renderEntryLines]) and the [blockPaint] click-mark primitive under
// it; subagentblock.go the run span, its railed frame and the collapsed sub-agent umbrella;
// userblock.go the full-width prompt block and its skill-span accents; startupbox.go the startup
// banner beside the presented-block painter; toolblock.go the tool block, group and super-group
// walk with the member rows they paint; toolleader.go the leader row, the dotted leader and the
// promote-guard that flexes it; blockstate.go the [blockState] a painter is told and the
// predicates that decide what a collapsed block hides; toolbranch.go the ┝/┕ branch rows and the
// detail lines beneath them; toolbody.go the ONE painter those bodies are all drawn by — the
// [bodyFrame] value each of the five framing paths states its own frame in (what leads a detail
// line, what continues it, which tone it takes, how many rows one line may spend) and
// [bodyFrame.paint], which spends the wrap primitives per line in that shape; plus [paintToolBody],
// which puts ADR 0052's reading rule in front of that painter so the split-vs-stacked choice
// ([splitBody]) is made in ONE place rather than at each path that can reach it;
// wrap.go the hanging-wrap, clip and depth-rail primitives every
// painter above shares; boxdraw.go the box and join primitives beside it, lifted out of model.go
// (ADR 0043) — the width-authority squaring a painted row is finished with ([squareLine],
// [squareOnField]), the four-sided border the two boxed surfaces are drawn in ([drawBox],
// [drawTitledBox]) and the two joins that stack the frame and hang the transcript's scrollbar in
// the painter's own measure rather than lipgloss's ([Model.joinFrame], [Model.joinScrollbar]);
// and chromelayout.go the two frame-arithmetic helpers
// ([inputContentRows], [clampInt]) the input box and the overlays size themselves with;
// and doc.go this narration.
//
// Invariant — the value-copied Model holds no self-referential no-copy type by value.
// [Model] is a value type with value-receiver Bubble Tea methods (ADR 0011), so the whole
// Model — every field it holds, recursively — is copied on every Update. A type that records
// a pointer to itself and checks it on use (strings.Builder, sync.Mutex/Once, bytes.Buffer's
// copyCheck-free but lock-like cousins) breaks under that copy: a strings.Builder held by
// value panics "illegal use of non-zero Builder copied by value" on the first write after a
// copy. Hold such a type by pointer, or use a plain value (the in-progress assistant buffer
// is a string, not a Builder, for exactly this reason). TestModelNoBuilderByValue guards the
// strings.Builder case structurally — the behaviour is address-dependent and a behavioural
// test cannot reliably reproduce the panic.
//
// Invariant — untrusted text is escape-stripped at the SEAM it enters the view through, never
// at each producer. The frame is painted through ultraviolet's cell buffer, which drops most
// zero-width sequences but deliberately HONOURS OSC 8 hyperlinks and never resets the link state
// across cells or newlines — so one unterminated OSC 8 opener turns every remaining cell of the
// frame (the transcript below it, the input box, the footer) into a clickable link to an
// attacker's URL, which is aimed straight at ADR 0019's rung 0 ("cmd+click the path we print").
// The producers reaching those seams are the least trustworthy strings in the program: a hostile
// model owns every tool-call argument and message, a malicious repo owns file first lines, command
// output, filenames and SKILL.md front matter, and a session record on disk is sanitized by no
// codec on its Meta. Enumerating them per call site is exactly what failed before — several were
// missed — so the seams strip on every producer's behalf: [transcript.addNote],
// [transcript.addEphemeralNote], [transcript.addError], [transcript.addApproval] and
// [transcript.addToolResult]'s orphan branch for the scrollback, [toolView.sanitize] (run by
// [toolView.finishDisplay], which presentToolCall and enrichWithResult both leave through) for the
// tool card and everything derived from it (toolActivityVerb, the gist), and each popupRow
// builder for the overlays, since the popup module strips
// nothing and truncates ANSI-preservingly. The strip itself lives in sanitize.go — [stripEscapes],
// its batch form [stripEscapesAll] and the [bidiControl] set they drop beside the control
// characters — which is where a seam that needs one goes to read the rule. stripEscapes is
// idempotent and allocation-free on text
// with nothing to rewrite — no control character, no DEL, no bidi formatting character, no invalid
// UTF-8 byte — so a producer that also strips costs nothing. TestTranscriptStripsTerminalEscapes and its siblings pin every
// one of those paths.
//
// The COMPOSER is that invariant's second door, and the "@" dropdown is what walks through it: an
// autocomplete row is not only shown, it is SPLICED, so an acItem carrying a stripped cell beside a
// raw value hands the escape to the input box on accept. What stops it there today is the bubbles
// textarea's own internal rune sanitizer rather than anything this package does — an undocumented
// third-party internal standing in for a seam — so fileSuggestions strips the workspace path itself,
// once, before the row's value and cell are both derived from it. Nothing is lost to a second
// channel by that: an @ref resolves from the "@token" read back out of the composed text
// (extractFileRefs → the loop's resolveFileRefs), never from the acItem, so display and resolution
// are one string and are sanitized once — which also keeps fileRefToken's promise that a row shows
// exactly what accepting it inserts, the property autocompleteExactMatch's ⏎-submits rule needs.
package tui
