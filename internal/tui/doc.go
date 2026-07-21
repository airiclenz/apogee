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
// rework: [theme] holds the palette, glyphs, and styles; toolpresent.go turns a tool call+result
// into a compact [toolView] through an OPEN, name-keyed registry (each later tool adds one entry);
// render.go is the line-oriented renderer (the full-width user block, ✦ assistant/tool headers,
// ┝/┕ tool-detail branches, depth indenting, and the [wrappedOffset] that mirrors the viewport's
// soft-wrap so the last user prompt sticks to the top while a reply streams). The transcript now
// groups a tool call with its result by ToolCall ID, the input box is a rounded, auto-growing
// black field, and the chrome is a braille status line plus a footer bar (host-alias ✦ model ✦
// context-window, mode). The live token gauge (reserved at P2.7) is now wired: the post-v1
// track folds each top-level UsageEvent's total into the status-line context-fill gauge,
// measured against the discovered context window ([Model.contextGauge] / [Model.statusRight]). The
// red/green diff detail is still a reserved renderer awaiting its Phase-3 producer.
//
// P3.14 turns the Depth-tolerating renderer into a Depth-rendering one: a sub-agent (Depth > 0)
// run is framed as a vertical-ruled sub-section — each line carries one "│ " rail gutter per
// nesting level ([railLines]), and a [renderSubAgentLabel] ⤷ sub-agent header opens each descent
// to a deeper level. The framing lives entirely in render.go (the value-copied Model holds no new
// state — the run boundary is derived from each entry's depth inside [transcript.renderView]), so
// the flat Depth==0 transcript renders byte-for-byte as before (ADR 0011 still holds — render only).
//
// The chat mini-language (post-v1 apogee-code feature-parity) adds a thin parse/route layer
// between the input box and the engine without thickening the renderer (ADR 0011 still holds):
// command.go is a pure [parseInput] that classifies a line as a local /command or an agent
// message and extracts @file references; autocomplete.go is the suggestion overlay (commands on
// "/", a bounded os.Root workspace-file listing on "@") rendered above the input like the
// approval-prompt slot. /clear (aliased by /new) and /compact drive the engine's context seams
// ([Engine.ClearContext]/[Engine.Compact]); /confine reports and toggles Auto's blast radius
// through [Engine.SetConfineToWorkspace], the one verb that takes arguments ([parseConfine] owns
// its "status | off [--save] | on" grammar, and an argument it does not understand is a parse
// error carrying the usage line — never a silent no-op on the command that widens what Auto may
// touch); @file *resolution* stays in the agent loop (reusing
// the workspace fence), so the TUI only parses references — it never reads files itself.
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
// The /skill flow (post-v1 apogee-code feature-parity) extends the same overlay without
// thickening the renderer: the "/" menu offers /skill, accepting it chains into a skill picker
// ("/skill <id>", an acSkill dropdown over the injected [SkillCatalog]), and a pick pops a chip
// onto Model.pendingSkills — a plain []string rendered as badges above the input. Submit copies
// the chips into [domain.UserInput.SkillIDs]; like @file, *resolution* (turning an ID into the
// prepended skill body) stays in the agent loop, through Config.Skills. /skill is deliberately
// NOT a parser command (it never submits as a message) — attachment is the only way it acts,
// mirroring the oracle's selectSkill, which keeps an unknown "/skill foo" an ordinary message.
//
// Three files round out the renderer without touching the state machine. markdown.go turns the
// common markdown subset in assistant text (**bold**, # headings, `inline`/fenced code, bullet/
// numbered lists) into styled physical lines — a spare, pure, lipgloss-only renderer matching
// toolpresent.go's posture, with render.go still owning the marker and depth framing. filecache.go
// backs the "@" overlay with a short-TTL, single-walk workspace listing filtered in memory, so a
// typing burst reuses one os.Root walk instead of re-scanning the disk per keystroke. mouse.go
// implements the prompt's click-to-position caret and drag-to-select (with OSC52 copy) on top of
// the textarea — apogee captures the mouse for transcript scrolling, which turns off the
// terminal's own click-drag selection, so the prompt re-implements it here.
//
// Module map — the input cluster has its own home (review candidate #3). prompteditor.go lifts the
// five loose input-side concerns the architecture review called one coherent concept — the
// textarea, the autocomplete overlay (+ its skillRegion edge-trigger), the staged-skill chips, the
// workspace file cache, and the prompt drag-selection — into a [promptEditor] type the [Model]
// embeds anonymously. Field and self-contained-method promotion keeps the value-copied Model idiom
// and every call site unchanged (m.input, m.pendingSkills, m.caretTo(...) resolve through it). The
// lift is deliberately partial: only methods touching nothing but the editor's own fields move
// there (newPromptEditor, submitParse, reset, rows, and the caret re-seat trio caretTo/reseatCaret/
// reseatInput); methods that also read Model-owned state — theme, width/height, opts, lifecycle —
// stay on the Model rather than duplicate that state (computeAutocomplete, acceptAutocomplete,
// attachSkill, highlightInput, inputContentRect, the region-arbitrating mouse handlers). The Model
// stays the coordinator that owns the lifecycle state machine, the transcript + render cache, the
// stats/gauge, the theme, and the layout; the editor never touches the engine.
//
// activity.go replaces the status line's turn index — which answered nothing the human was
// asking — with a live activity phrase and an elapsed clock ("thinking · 12s", "reading ·
// main.go · 3s", "sub-agent · searching · 6s"). [Model.foldActivity] derives it from the same
// Event stream the transcript folds (including [domain.ReasoningEvent], the observability seam
// that makes "thinking" a fact rather than a guess), and the transitions no Event announces —
// submit, /compact, the stop key, the worker's terminal Msg — set it directly. It adds no
// lifecycle state: compacting and stopping are activities, not uiStates, so the ADR 0011 state
// machine is untouched and only statusLine's running branch consults it. The per-tool verb it
// renders comes from the same open registry toolpresent.go already keys by tool name.
//
// The tool-call layout pass (post-v1.5.0) tightens how a session reads without touching what the
// model sees: committed assistant text is trimmed of its leading and trailing blank lines
// (trimBlankLines) and interior blank runs collapse outside fenced code, so layout.md's "exactly
// one empty line between blocks" holds; a tool header drops its square brackets for a bold-orange
// label (the [theme] toolLabel role, styled before the wrap — the markdown.go posture); and
// consecutive same-label calls at the same depth fold into one aligned block (toolCallRun /
// groupable / renderToolGroup). Grouping is render-time only — the append-only entry list, the
// call/result pairing, and transcript.hasOpenToolCall are untouched, so a call arriving mid-stream
// joins its group on the next repaint. TestTranscriptLayoutGolden pins the whole rendered
// scrollback.
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
package tui
