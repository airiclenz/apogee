# Changelog

All notable changes to Apogee are recorded here. The public Go API follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html) from `v1.0.0`
onward (ADR 0001 §consequences, as amended at the Phase-3 cut): Events and
hook points stay **additively extensible**, so a new Event variant or hook
point is a **minor** bump, not a breaking change.

## [Unreleased]

Post-`v1.1.0`, **additive** (minor) — Phase 4 merges the apogee-sim Mechanisms into the
loop (`docs/plans/phase-4-detail-plan.md`; ratified catalogue at
`docs/design/mechanism-catalogue.md`).

### Catalogued Mechanisms now dispatch in a deterministic order behind the Bypass gate

- **Registered Mechanisms finally run.** A Mechanism added to the `MechanismRegistry` via
  `Add` used to be validated but never dispatched — only the bench's experimental hooks
  fired. Now, at each of the five hook points, the loop dispatches the catalogued
  Mechanisms **first**, in a deterministic total order (`MechanismRegistry.Ordered` — a
  topological sort of each Mechanism's `Before`/`After` `OrderingConstraints` with a stable
  tiebreak by canonical `MechanismID`, so a shuffled registration order yields identical
  output, ADR 0003), then the experimental hooks in registration order (unchanged). Each
  fires under the same recover boundary and emits a `MechanismFiredEvent` under its **real**
  `MechanismID` (experimental hooks keep the synthetic `experimental` attribution).
- **`Config.Bypass` now gates dispatch (ADR 0006).** Under Bypass, every catalogued
  non-`off-ramp` Mechanism is skipped — proactive-nudge and response-repair go silent —
  while `off-ramp` recovery guarantees still run; experimental hooks are never Bypass-gated
  (they are the bench's own instruments), and the structural context machinery (Budget,
  Compaction) is unaffected.
- **Incompatible Mechanisms fail loudly at construction.** `New` now also runs
  `MechanismRegistry.ValidateIncompatibilities`, returning the new
  `ErrIncompatibleMechanisms` sentinel when two registered Mechanisms declare each other via
  `MechanismDescriptor.IncompatibleWith` — the same startup-gate posture as
  `ErrOrderingCycle`, so a config that enables two mutually-exclusive Mechanisms is refused
  rather than silently running both. (`internal/domain`, `internal/agent`, root re-exports.)

### Mechanisms now self-regulate: effectiveness tracking, Adaptive Suppression, the Turn Budget

- **A catalogued Mechanism that is not helping is now withdrawn for the rest of the
  Session.** A per-Session tracker judges each Turn on proxy signals — a Turn is
  **productive** when it reads a new file or writes one (a tool error or an empty/no-op
  response is not). **Adaptive Suppression** (per Mechanism): a Mechanism that fires through
  three consecutive non-productive Turns is skipped at dispatch for the rest of the Session,
  with a clear-path that re-opens every Mechanism on the next productive Turn. **The Turn
  Budget** (global): after eight consecutive non-productive Turns every non-exempt Mechanism
  is withdrawn until productive activity resumes. A `SuppressionPolicy: exempt` off-ramp
  bypasses both — suppressing it would leave a failed Turn with no way out (ADR 0006).
- **`LoopView.Fired` finally answers.** The declared-but-inert per-Session fire counter now
  reports real fires, read live within a hook pass (a Mechanism sees a peer's fire from
  earlier in the same pass — the cross-Mechanism coupling seam). No new public surface: the
  tracker is internal to `internal/agent`; `domain.NewRequest` gains a `fired` ledger
  argument on the engine seam only.
- **Reset on Resume.** The tracker is per-Session and not serialized: a resumed Agent starts
  with clean suppression state (the accepted v1 posture — fresh state can only cause a
  withdrawn Mechanism to be re-tried, never wrongly withheld). (`internal/agent`,
  `internal/domain`.)

### A file-only `mechanisms:` config block wires the catalogue into the loop

- **Catalogued Mechanisms are now opt-in from `config.yaml`.** A new file-only `mechanisms:`
  block (no flag/env, like `mcp-servers` / `model-profile`) maps a canonical mechanism ID to
  `enabled: true|false`. Every Mechanism defaults **off** (D1 — default-off until bench-proven);
  a `true` entry turns one on. An **unknown ID is a loud startup error** listing the catalogue
  this build knows, so a typo'd key never silently disables a Mechanism. `--bypass` still wins:
  an enabled non-off-ramp Mechanism is not dispatched under bypass (ADR 0006 / the item-2 gate).
- **The catalogue constructor seam.** `internal/mechanisms` gains `Build(id, deps)` over a
  constructor table (`Deps` carries the construction-injected collaborators — D3; the Library
  store is nil until it lands). The composition root (`cmd/apogee`) drives the table for each
  enabled ID and folds the built Mechanisms into `Config.Mechanisms` before construction. The
  table ships **empty** — the port waves fill one row per Mechanism — so a config with no
  `mechanisms:` block behaves exactly as before. (`cmd/apogee`, `internal/mechanisms`, README +
  starter `config.yaml`.)

### Wave 1: the `validate` / `syntax` / `autofix` response-robustness Mechanisms

- **The measured-win response cascade is ported.** Three post-response Mechanisms — dispatched in
  the deterministic order `validate` → `autofix` → `syntax` (catalogue Table A as amended by the
  reorder entry below; originally shipped `validate` → `syntax` → `autofix`) — now ship in the
  `internal/mechanisms` catalogue (default **off**, D1). `validate` checks each requested tool call
  against the tool menu the model was shown and its own arguments (unknown tool name, empty/malformed
  JSON, missing required parameter); `syntax` checks a file-writing call's content (Go through the
  real parser, other languages through a bracket/string/truncation heuristic); `autofix` repairs
  syntax-broken write content and writes the improved payload back to the call the loop will dispatch.
- **Corrections retry in place (amended C5 — R1; superseding this entry's original ActionDefer
  delivery).** `validate`/`syntax` return `ActionRetry` with the sim's correction message — the
  loop re-streams the corrected request in the same Turn (see the delivery-switch entry below).
  `autofix` intercepts in place via `Response.SetToolCallArguments`, which is effective because a
  Response's tool calls are dispatched only after post-response review.
- **`gofmt` is always in-process; other formatters are construction-probed and gracefully absent
  (superseding this entry's original fire-time PATH-gating — see the autofix entry below).** Go is
  formatted with the standard library's `go/format` — no external dependency — with `goimports`
  preferred when found; `black` / `prettier` / `rustfmt` repair only when their executable was
  resolved at construction, and a formatter's absence, failure, or timeout leaves the payload
  untouched (standing requirement #2). What no formatter can improve is left for `syntax` to
  correct. (`internal/mechanisms`.)

### Wave 1: the `empty_response_recovery` / `tool_use_enforcer` off-ramps

- **The two recovery guarantees are ported (catalogue Table A).** Both are post-response Mechanisms
  with Capability **off-ramp** and SuppressionPolicy **exempt**, so they run even under Bypass (D5)
  and are never withdrawn by Adaptive Suppression or the Turn Budget — without them a failed Turn has
  no way out (CONTEXT "Off-ramp"). They ship in the `internal/mechanisms` catalogue, default **off**
  (D1). `empty_response_recovery` fires when the model returns nothing — no text and no tool call —
  mid-task with tools available and recent progress; `tool_use_enforcer` fires when the user asked for
  an action but the model answered with prose twice running, having never used a tool (the sim's
  intent classifier, folded in inline per catalogue C6).
- **Empty replies and narration both retry in place (amended C5 — R1; superseding this entry's
  original retry/defer split).** `empty_response_recovery` returns `ActionRetry` carrying the sim's
  first-attempt completion-check nudge verbatim; `tool_use_enforcer` returns `ActionRetry` with the
  sim's "use a tool" correction, the retried request carrying the superseded narration (the sim's
  `retryForToolUse` exchange). Both stay bounded by the loop's existing `maxPostResponseRetries`
  cap so an always-empty model still terminates. (`internal/mechanisms`.)

### ActionRetry now carries the corrective exchange onto the retried request

- **A post-response retry delivers its correction in the same Turn (R1, amending catalogue
  C5).** `PostResponseDecision.Inject` now rides `ActionRetry` too: when a post-response
  Mechanism retries with a correction, the loop appends the superseded assistant message
  (text + tool calls, when non-empty) and then the correction as a role-safe user message
  to the in-flight request before re-streaming — request-scoped, never committed to history
  — mirroring apogee-sim's own retry builders. Corrections accumulate across attempts (the
  sim's escalating re-asks), bounded by the existing `maxPostResponseRetries` cap; at the
  cap the last response passes through. An `Inject`-less retry stays a bare re-stream, and
  `ActionDefer` keeps its next-request semantics unchanged. (`internal/domain`,
  `internal/agent`.)

### Wave 1 rides the retry seam: corrections deliver in the same Turn

- **The four shipped Mechanisms switch `ActionDefer` → `ActionRetry` (amended C5, R1).**
  `validate` and `syntax` now short-circuit the response-repair cascade on a failing call —
  the correction re-streams the corrected request in the same Turn instead of waiting for the
  next request — so the catalogue's "short-circuits cascade on fail" holds for real.
  `tool_use_enforcer` re-calls in-cycle exactly like the sim's `retryForToolUse`: the retried
  request carries the superseded narration plus the "use a tool" correction, fixing the review
  finding that the correction sat until the next user Submit. `empty_response_recovery`
  upgrades its bare re-stream to carry the sim's first-attempt completion-check nudge verbatim
  (`empty_recovery.go` @pin); the attempt-2 nudge ladder, system directive, and temperature
  escalation stay recorded bench-pending divergences (R2). Everything remains bounded by
  `maxPostResponseRetries` — an always-empty model terminates, its final reply passing through.
  Proven loop-level through the scripted-responder harness, including both off-ramps firing at
  dispatch (registry-built) under Bypass and through a tripped Turn Budget.
  (`internal/mechanisms`; tests in `internal/agent`.)

### autofix repairs like the sim: construction-probed formatters, issue-count gating, repair-before-correct

- **The formatter table is resolved once at construction (D3).** `mechanisms.Deps` gains
  `LookPath` (nil ⇒ `exec.LookPath`); `newAutofix` probes goimports/black/prettier/rustfmt
  through it exactly once and caches the resolved paths — the sim's LookPath-cached formatter
  table — so a fire never touches PATH. The package-var-at-fire-time probe is deleted, and
  `cmd/apogee` wires the production `exec.LookPath`.
- **Repair only, gated on improvement.** autofix now acts only on syntax-broken write content
  and keeps a formatter's output only when it *reduces* the `checkSyntax` issue count (the
  sim's `AttemptFix` gate) — clean content is never beautified, and a "fix" that fixes nothing
  is discarded. The sim's `sanitizePath` NUL/CR/LF guard is restored alongside the kept `-`
  prefix hardening on formatter argv.
- **Cascade reorder: `validate` → `autofix` → `syntax`.** The sim runs detect → `tryAutoFix` →
  correct-the-remainder (`response_analysis.go:72-88` @pin), so repair now precedes the
  correction stage — `syntax`'s retry covers only what a formatter could not fix, ending the
  review's double-correction finding. Catalogue Table A and the post-response cascade section
  record the amendment. (`internal/mechanisms`, `cmd/apogee`.)

## [1.1.0] — 2026-07-03

Post-`v1.0.0`, **additive** (minor) — the start of the apogee-code TUI
feature-parity track. See
`docs/handoffs/2026-06-26 - 00 - chat-mini-language-core.md` and
`docs/handoffs/2026-06-26 - 01 - skills-system.md`.

### Drag-select-to-copy in the transcript (screen-space)

- **You can now drag-select text in the chat transcript and copy it to the clipboard**, the same
  gesture the prompt box already supported. A left-click-drag inside the transcript viewport
  highlights the span and, on release, copies the rendered text over OSC52 (`tea.SetClipboard` —
  cross-terminal and SSH-safe) with the usual "copied N chars" confirmation. The selection is
  **screen-space** ("copy what you see"): it anchors in content coordinates (rendered-line index +
  display cell) into the cached `m.lines`, so it survives a mid-drag wheel-scroll; on release it
  slices each spanned line with `ansi.Cut`, strips the styling, and trims the block's trailing pad.
  Markers, rail gutters, and soft-wrap breaks are copied verbatim (the accepted terminal-native
  semantics — the one-way render pipeline stays one-way, no line→entry reverse index). The mouse
  handlers arbitrate by region — a point in the input rectangle drives the prompt editor, a point
  in the viewport drives the transcript — so the two selections never coexist. The selection clears
  on any transcript change (a streamed token, a submit) and on resize; a bare click copies nothing.
  Drag auto-scroll at the viewport edge is deferred. (`internal/tui/mouse.go`, `model.go`.) Closes
  the "cannot select text in the transcript" ISSUES entry.

### Chat input lifted into a `promptEditor` module (internal refactor)

- **The chat input cluster now lives in its own type**, `promptEditor` (`internal/tui/prompteditor.
  go`), instead of scattered across the god-Model. It gathers the five loose input-side concerns the
  architecture review (candidate #3) called one coherent concept — the textarea, the autocomplete
  overlay (+ its `skillRegion` edge-trigger), the staged-skill chips, the workspace file cache, and
  the prompt drag-selection. The `Model` embeds it **anonymously**, so the fields and the
  self-contained methods promote onto the Model (`m.input`, `m.pendingSkills`, `m.caretTo(...)` all
  resolve through it) and every existing call site — and all package tests — stay unchanged. Model
  top-level field count drops **32 → 27**; the six input-cluster fields now have a single home.
- **Purely structural — no behaviour changes.** Only methods that touch nothing but the editor's own
  fields moved to it (`newPromptEditor`, `submitParse`, `reset`, `rows`, and the caret re-seat trio
  `caretTo`/`reseatCaret`/`reseatInput`); methods that also read Model-owned state (theme, window
  size, `Options`, lifecycle) deliberately stay on the Model rather than duplicate that state. The
  Model stays the coordinator (lifecycle state machine, transcript + render cache, stats/gauge,
  theme, layout); the editor never touches the engine — it only turns typed input into
  send-ingredients the Model routes. New editor-direct unit tests exercise the lifted logic without
  a Model or a fake engine (`internal/tui/prompteditor_test.go`).

### Model profile config surface (tool-call format + thinking channels)

- **`Config` gains a `Profile ModelProfile` seam** describing how the configured model speaks the
  wire (CONTEXT: Model profile) — its tool-call format (native / markdown-fenced / custom-regex)
  and its inline thinking-channel style (none / delimited `<think>…</think>` / gpt-oss harmony).
  The new public domain types are re-exported from the root facade (`apogee.ModelProfile`,
  `ToolCallFormat`, `ThinkingProfile`, `ThinkingStyle` and their consts) — an **additive minor**
  (decision #18). A **zero profile is native tool calls with no inline thinking**, so every
  shipped model behaves exactly as before (the byte-identical anchor).
- **Plumbed from `config.yaml`** as a file-only `model-profile:` block (a per-model concern, like
  `mcp-servers` — no flag/env), mapped to the domain type at the host boundary. **No loop consumer
  yet**: the loop's parse seam is crossed in a following change, so this is a pure, provably
  behaviour-neutral config-surface addition.

### Model profile wired into the loop (fenced/regex tool calls + thinking/harmony stripping)

- **The loop now consumes `Config.Profile` at the parse seam.** A new `processing.ParserFor(domain.
  ModelProfile)` translates the declarative profile onto `internal/processing`'s existing, frozen
  `ToolCallingConfig`/`ThinkingConfig` and returns the text-format `ToolCallParser` plus a unified
  `ContentStripper` (the `none`/`delimited`/`harmony` thinking styles behind one `Strip` +
  `IsMidChannel` interface). `internal/agent` selects both once in `newAgent`, so the oracle config
  types never surface in the loop and a bad profile (unknown format / thinking style) fails
  construction loudly rather than falling back to native.
- **At the seam:** the reply's inline thinking/harmony channel is stripped out of the visible
  content and preserved as `reasoning_content` in history (the harmony `commentary` channel folds
  into reasoning); when the structured **native** path produced no calls, a markdown-fenced or
  custom-regex tool call is recovered from the *stripped* visible content, its markup removed from
  the committed assistant text, and it is assigned a deterministic `text_call_<turn>` ID (not the
  oracle's wall-clock ID, so snapshot/resume and tests stay stable). Native calls always win when
  present.
- **A recorded, deliberate divergence from the apogee-code oracle:** a text-parsed call is stored
  **structurally** on the assistant message (`ToolCalls`), so dispatch, events, and snapshot/resume
  keep **one** path for every format; the oracle instead commits stripped text with only a
  tool-role result. Chat templates tolerate native-shaped history better than the loop tolerates two
  history shapes.
- **A zero profile is byte-identical** to the pre-change loop: the no-op stripper and no-op parser
  leave `reply.content` and the native calls untouched, so every shipped (native) model behaves
  exactly as before. The frozen `internal/processing` oracle types, parsers, and parity tests are
  unchanged — only the new `ParserFor`/`ContentStripper` and the loop caller were added. **Live
  in-flight token suppression while streaming is a following change; this fixes committed history
  and the final message.**

### In-flight thinking/harmony tokens held off the live stream (native unchanged)

- **`streamResponse` now emits a `TokenEvent` for the newly-revealed *visible* content**, not the
  raw content delta, using the same `ContentStripper`. While the accumulated content ends inside an
  unclosed inline reasoning span (`IsMidChannel`), token emission is held, so a model that inlines
  `<think>…</think>` or gpt-oss harmony channels no longer flashes that markup (or its reasoning)
  onto a live UI before the post-stream strip; the visible text is revealed once the span closes.
- **A native / no-inline-thinking profile is byte-identical, event-for-event:** the no-op stripper
  is never mid-channel and returns the content untouched, so every content delta emits verbatim and
  unbuffered exactly as before. A channel start token split across two deltas briefly reveals its
  partial prefix live (matching the oracle's `isThinking`); this recorded edge is accepted — the
  post-stream strip still removes it from the committed message and final `MessageEvent`.

### Fenced/regex models now receive a text tool menu + emission instructions (native unchanged)

- **A new `processing.InstructionsFor(domain.ModelProfile, []domain.ToolDef)` renders the emit
  side of a non-native profile:** the text tool menu (name, description, JSON-schema parameters)
  plus the format-specific tool-call instructions and a live example — ported from the apogee-code
  context builder, driven by the *same* profile knobs and defaults the parser reads, so what the
  model is told and what the loop parses cannot drift. It is the request-seam mirror of `ParserFor`.
- **`toProviderRequest` now injects the block and suppresses the native `tools` array for a
  non-native tool-call format.** The rendered menu + instructions are folded into the wire request's
  system channel (appended to a hook-seeded system message, else a sole system message at position
  0) and the native `tools` array is dropped — sending both would double-tell the model in two
  formats, and a chat template without tool support can error on the array. For a non-native profile
  the text menu is the **only** channel the model learns its tools from; before this change a
  fenced/regex model received a native array its template may not render and no instructions.
- **Wire-only, tracked per request:** the block never enters domain history, the snapshot, or any
  event — exactly like the native `tools` array, which is also rebuilt per request and never
  persisted. It is re-rendered over each request's **mode-filtered** menu, so a Plan-mode switch (or
  any menu change) is reflected on the next Turn with no history rewrite.
- **A native/zero profile is byte-identical:** `InstructionsFor` returns `""`, so there is no
  injection and no suppression — the native `tools` array and the message list are exactly today's.

### Dispatch decision collapsed into one Resolution verdict (internal refactor)

- **The per-call dispatch decision is now one `Resolution`**, computed by a single pure resolver
  (`internal/agent/resolution.go`): the tighten-only guard floor, the autonomy-ladder × blast-radius
  table, the confinement-capability check, and the precomputed runtime-demote contingency are all
  decided in full before anything executes. `internal/agent/dispatch.go` is now a thin executor that
  gathers facts, calls the resolver once, and carries the verdict out — it holds no ladder,
  guard-tier, or demote decision of its own. The old `disposition.go` decision path is retired.
  **No behavior change**: unexported and internal-only (no public API / semver impact). The term
  "disposition" is retired from code, surviving in prose only as the historical name of the
  post-guard ladder stage. `docs/design/confinement-execution-contract.md` §4 amended in place.

### MCP "allow for this session" now caches at server grain (ADR 0012 conformance)

- **Approving one of an MCP server's tools "for this session" now clears the whole server**, not
  just that one qualified tool: approving `github__search` pre-clears `github__create_issue` and
  every other `github__*` tool for the Session, honouring ADR 0012's server-grain promise (the
  cache had always keyed on the qualified tool name, so each `github__*` tool re-prompted). The
  allow-for-session cache key for an `mcp` gate is now `mcp-server:<alias>`; the `mcp-server:`
  prefix keeps that grain collision-proof against ordinary tool names, and a **different** server
  (`jira__*`) is never pre-cleared by another's approval. A **forced** gate (a Tier-2
  dangerous-action speed-bump) still skips the cache and re-prompts, unchanged. Every non-MCP
  class keeps the tighter tool-name grain, so nothing else loosens.

### Compact tool print-outs in the chat (full built-in coverage)

- **The TUI's tool-presentation registry now covers every built-in tool**, not just the
  Phase-2 four: the edit family, `view_diff`, `open_file`, `terminal`, `python_exec`, the
  git family, `diagnostics`, `web_fetch`, `http_request`, `web_search`, `sub_agent`, and
  `ask_user` each render as `✦ [Label] target` — no more raw tool names with JSON argument
  braces in the transcript. Only a dynamic (MCP) tool keeps the raw-name + JSON fallback.
- **Results no longer dump raw into the chat**: `web_search` shows "N results", the fetch/
  request tools their `HTTP 200 OK` status line, free-form output (a command run, a
  diagnostics or sub-agent report) its first line plus a "+N more lines" count, `open_file`
  its Located line or a line count. `view_diff` renders red/green diff lines (the reserved
  diff detail kinds get their first producer), capped at 20 lines.
- Detail and target lines are clipped at 160 runes so a minified blob cannot flood a row.
  The approval dialog still shows the full pretty-printed arguments — the security surface
  (the model's request is never hidden) is unchanged.

### Web search works out of the box (DuckDuckGo default)

- **`web_search` is now default-ON**: with no `web-search-endpoint` configured it uses a
  built-in DuckDuckGo HTML provider — no config, no API key (reverses the P3.11 default-off
  decision; the predecessor apogee-code shipped the same built-in). Set
  `web-search-endpoint: off` (or `none`/`disabled`) to disable the tool — a graceful
  "web search is disabled" result, no request made.
- **The DuckDuckGo provider POSTs the query** as a form field, the way DDG's own search
  form submits: the HTML front-end answers a plain GET with its bot-challenge ("anomaly")
  page — zero result anchors, so every search rendered "No results found". A custom
  endpoint keeps the `q` GET-parameter contract unchanged.
- **An explicitly configured DuckDuckGo endpoint selects the built-in provider**: an
  endpoint whose host is `html.duckduckgo.com` (with or without scheme) now gets the same
  POST + browser-header treatment as the default, instead of degrading to the
  custom-endpoint GET that DDG answers with the challenge page.
- **Results are auto-cleaned**: the DuckDuckGo page (and any custom endpoint's HTML
  response, by Content-Type or body sniff) is parsed into numbered `title / url / snippet`
  results; a custom endpoint's JSON/text response still passes through verbatim. A
  rate-limit/consent page degrades to "No results found", never a crash.
- **Non-2xx responses are now tool errors** naming only the status and endpoint host
  (previously the status + raw body passed through as a normal result). The M2 key
  redaction (`endpointHost`/`scrubURLError`) and the always-on SSRF floor are unchanged.
- **Scheme-less custom endpoints self-heal**: an endpoint like `search.example.com/s`
  (no `https://`) used to parse with an empty host and every request was rejected by
  url-safety; it now self-heals to `https://`. This repairs hand-edited configs — the
  shipped config template never carried a broken value (its endpoint line was always
  commented out), and first-run seeding never overwrites an existing config.

### Context compaction (`/compact`)

- **`/compact` now performs real generative compaction** (replaces the
  `ErrCompactionNotImplemented` stub). The new `internal/context.Compact` reducer
  summarizes the conversation through a single upstream call and replaces the folded
  history with one assistant summary message, keeping the protected prefix (leading
  system messages + the first user message, `Conversation.PrefixEnd`) verbatim so the
  original task framing survives. A conversation with too little past the prefix is
  skipped; a summary-call failure or cancellation leaves the history untouched.
- **Wired through `Agent.Compact`** (guarded to a quiescent boundary like `ClearContext`,
  returning `ErrInputPending` mid-Exchange). The summary call is *silent* — it reuses the
  loop's request projection but emits no `TokenEvent`/`UsageEvent`, so it neither streams
  into the transcript nor moves the live gauge; it runs at low temperature.
- **TUI** drives `/compact` on a worker goroutine (it is a real upstream call and must not
  block the `Update` loop — ADR 0011): the spinner runs, `Esc` cancels, and on success a
  "context compacted" note lands while the context-fill gauge resets so the next Turn
  re-measures the smaller fill.
- **Removed** the now-unused `ErrCompactionNotImplemented` sentinel (it was never in a
  released version).

### Fixes

- **Prompt box no longer scrolls the first line out of view as it auto-grows.** Typing past the
  wrap width grew the input box, but bubbles' `textarea.SetHeight` only repositions its internal
  view when the caret falls *outside* it — never when the box grows — so a stale downward scroll
  offset survived: the first line was hidden above and a phantom blank row showed below, with the
  caret pinned to the top visual row. `layout` (`internal/tui/model.go`) now re-seats the caret
  after a height change through the shared `reseatCaret` idiom (`MoveToBegin` "unscrolls" to the
  top, then the widget's own `CursorDown` walks back to the caret's real row, re-clamping the
  offset with none of the textarea's wrap re-derived); it runs only on an actual height change, so
  vertical caret navigation keeps the widget's sticky goal column. A companion fix corrects
  `inputContentRows` (`internal/tui/render.go`) to count the trailing row the textarea reserves for
  a logical line that exactly fills the width, so the box no longer sizes one row short at a
  wrap-fill boundary (which had stranded the same offset the re-seat could not then reach). At the
  `maxInputRows` cap the textarea's legitimate internal scrolling is preserved (offset =
  contentRows − height). Closes the prompt-scroll and auto-sizing ISSUES entries.

- **Auto mode now works on macOS — seatbelt fences the workspace correctly.** The
  `sandbox-exec` profile embedded the box's writable roots verbatim, but seatbelt
  matches a write against its *kernel-canonical* path; on macOS `/tmp` and `/var`
  are symlinks into `/private`, so a box rooted at `/var/folders/...` never matched
  the resolved `/private/var/folders/...` and seatbelt denied **every** in-workspace
  write — Auto mode could not write at all. `seatbeltProfile`
  (`internal/platform/seatbelt.go`) now resolves each writable root through symlinks
  (`filepath.EvalSymlinks`, falling back to the cleaned path for a not-yet-created
  root) before emitting the `(subpath ...)`, so the profile matches the kernel's view
  and agrees with path-safety (which already resolves the same way). Landlock is
  unaffected — it is fd-based (`unix.Open(root, O_PATH)`), so the kernel resolves
  symlinks to the inode the rule keys on. Closes the `v1.0.0` "Box-root
  canonicalization" post-release residual; verified on real macOS hardware
  (`TestSeatbeltProbe` in-box write rows now pass under live `sandbox-exec`).

- **Context window now reads the runtime size from llama.cpp `/props`.** Discovery
  (`internal/provider.Discover`) probes `GET /props` after `/v1/models` and prefers
  its `default_generation_settings.n_ctx` — the `-c`/`--ctx-size` the server was
  actually launched with — over the model's advertised *training* window
  (`context_length`, else `meta.n_ctx_train`), which is often far larger than the
  loaded window. This fixes the live context-fill gauge measuring usage against the
  wrong denominator (it barely moved on a server loaded well under its training
  context). Best-effort: a non-llama.cpp server (no `/props`) keeps the `/v1/models`
  value, and a probe failure never fails discovery. Ports the oracle's previously
  deferred `llamacpp-props` strategy; the `ollama-show`/`ollama-tags` strategies
  remain unported (additive, not needed yet).

- **`/compact` and the context gauge now tell the truth.** Four fixes to the
  compaction/gauge seam that had it reporting outcomes it did not produce:
  (a) an Esc landing *after* a compaction committed reported "cancelled" while the
  history had already folded — `startCompact` (`internal/tui/worker.go`) now
  classifies the outcome from `Compact`'s returned error (`context.Canceled`), not a
  post-hoc `ctx.Err()` read, so a committed fold reports as compacted;
  (b) a no-op compaction (conversation too small to fold — the reducer's
  `Result.Skipped`) printed "context compacted" and hid the gauge — `Agent.Compact`
  now returns the skip signal through the `Engine` seam and the TUI says "nothing to
  compact" and leaves the gauge untouched;
  (c) `/clear` left the gauge and tok/s readout lit from the discarded session —
  `ClearContext` now zeroes `ctxUsed`/`tokPerSec` like a fold does;
  (d) a cancelled or faulted stream emits no terminal `UsageEvent`, so the
  generation clock survived into the next turn and mistimed its tok/s — `finishWorker`
  now clears `genStart` on every terminal message.

- **A loop fault no longer risks re-wedging the engine.** The `errMsg` handler
  (`internal/tui/model.go`) now calls `AbortExchange` before returning to the errored
  state, mirroring the `cancelledMsg` recovery: if a `Step` ever faults mid-Exchange
  the interrupted Exchange is discarded so the next `/clear` or message is accepted
  rather than refused with `ErrInputPending`. A latent fix — `Step` surfaces faults as
  an `ErrorEvent` at a boundary today — but it closes the error flavour of the post-Esc
  un-wedge. The `/compact` failure/cancel spine (both `startCompact` outcomes and the
  reducer's overflow/cancel/silence faults) is now covered by tests.

- **`/compact` now survives high context fill.** The reducer sent the *entire* rendered
  transcript as one summary request, so near `n_ctx − compactMaxTokens` the summary call
  itself overflowed (`DeltaContextOverflow`) — compaction deterministically failed at exactly
  the fill it exists to relieve, leaving `/clear` as the only recovery. `internal/context.Compact`
  now bounds the rendered transcript to a character budget derived from the discovered context
  window: it keeps the protected prefix and a budgeted tail of the most recent messages (the
  latest is always kept) and elides the middle with a `[... N earlier message(s) omitted ...]`
  marker, so the summary call stays within the window. The budget is computed in
  `Agent.compactTranscriptChars` from `Context.MaxContextTokens` (now threaded from upstream
  discovery in `cmd/apogee/wire.go`) minus the response reserve and prompt overhead; it is 0
  (render everything, as before) when the window is unknown, since there is no safe basis to
  bound. The overflow test flips from "errors cleanly" to "succeeds via the budget"; the
  unbudgetable case (no discovered window, or a server that rejects even a minimal prompt) still
  surfaces the fault cleanly with the conversation untouched. This makes on-demand `/compact`
  robust; the automatic compaction trigger (which fires *at* high fill by definition) is still
  parked in `TODO.md`.

- **Mouse selection and bracketed paste now handle the prompt correctly.** Two input
  fixes on shipped TUI behaviour:
  (a) a click or drag on a prompt row with wide glyphs (CJK, emoji) landed the caret on
  the wrong rune — `caretTo` (`internal/tui/mouse.go`) fed a display-**cell** column into
  the textarea's rune-indexed `SetCursorColumn` (clamped by cell width, not rune count),
  so a drag-copy could put **different text on the clipboard than was highlighted**. It
  now converts the cell column to a rune offset by walking the visual sub-line's runes and
  accumulating `runewidth` (the same width the widget's own cursor math uses), clamped by
  rune count;
  (b) bracketed paste (default-on in bubbletea v2) fell into `Update`'s `default:` case,
  so the textarea inserted the text but skipped the post-edit refresh — a multi-line paste
  rendered unwrapped until the next keypress, the autocomplete overlay went stale, and a
  live drag-selection's cached offsets no longer matched the value (a later copy took the
  wrong runes). A new `tea.PasteMsg` case (`internal/tui/model.go`) mirrors the keypress
  edit path: it clears the selection, inserts, recomputes autocomplete, and re-lays out;
  a paste while a worker runs is dropped, as keystrokes are.

- **A sub-agent now sees a mid-delegation mode tightening (ADR 0013).** `newChildAgent`
  froze the parent's mode at spawn, so a Shift+Tab from Auto down to Plan while a sub-agent
  ran (many Turns on a small model) flipped the footer but left the child auto-approving
  writes until its Exchange ended — a tighten-direction ADR-0005 violation. The orchestrator
  now injects a tighten-only view of the parent's live mode into the child (`Agent.liveMode`,
  the parent's `modeMu`-guarded `Mode` accessor captured as a closure — never the shared field
  or mutex). The child's disposition (`effectiveMode`) takes `TighterMode(parentLive,
  spawnMode)` — a new ladder-index helper in `internal/domain/config.go` where Plan <
  Ask-Before < Allow-Edits < Auto — so a parent tightening below the child's spawn mode
  gates/refuses the child's next call, while a parent loosening can never loosen a child
  spawned tighter (loosening mid-flight stays impossible). A top-level agent (nil view)
  behaves exactly as before.

- **Cleanup batch — leaked cancels, bounded untrusted reads, escape hardening, quit race,
  dead code.** A sweep of small hardening fixes on shipped behaviour:
  - *Leaked cancels.* `finishWorker` (`internal/tui/model.go`) nil'd the worker's `CancelFunc`
    without calling it, leaking one cancellable child context (and its timer resources) per
    completed exchange for the session. It now cancels before clearing.
  - *Bounded reads of untrusted files.* Skills discovery read `SKILL.md` unbounded at startup
    (`.apogee/skills` is always scanned — a hostile-repo OOM), and the `@file` 10 MB cap was
    checked only *after* `SafeReadFile` had already slurped the whole file. Both now bound
    before materializing — skills via an `io.LimitReader` (1 MiB/file) plus a global skill-count
    cap, `@file` via a new `security.SafeStat` fenced size check — mirroring the read_file tool.
  - *Terminal-escape hardening.* Untrusted model text and skill display names are now
    escape-stripped at the transcript boundary (`internal/tui/transcript.go`), so a
    model- or `SKILL.md`-supplied `\x1b]52;…` (OSC 52 clipboard write) or CSI payload can never
    reach the terminal. Not exploitable in the current layout (verified empirically at review),
    but this closes it at the source rather than relying on the cellbuf and footer ordering.
  - *Quit-while-busy teardown race.* `quit()` returned `tea.Quit` without joining the in-flight
    worker, so `runRoot`'s deferred `Close()` teardown could race a worker still inside `Step`
    (benign while `Close` is a no-op, a use-after-close the moment it gains real teardown). The
    exit is now deferred until the worker's single terminal Msg arrives.
  - *Dead code.* Removed the zero-caller `Engine.Mode()` seam method, the unused `fitLeftRight`
    footer helper, and the standalone `workspaceFiles` walk plus its unreachable `m.files == nil`
    autocomplete fallback (`newModel` always installs the cache). The three skill-chip
    render/ID-resolution copies were merged onto one `renderSkillChip` renderer and the shared
    `skillDisplayNames` resolver.
  - *Test gaps.* Added coverage for the loop's `UsageEvent` emission hop (Delta.Usage → event
    fields/Depth, and no event when Usage is nil), the combined skills→files→text injection
    order in one Submit, the `@file` oversize refusal, the escape-strip boundary, and the
    bounded skill-file read.

### TUI

- **Context-fill gauge restyled** to match `llama-launcher`: a solid two-tone strip —
  full blocks for the filled cells, an eighth-block partial cell (`▏▎▍▌▋▊▉`) for
  sub-cell granularity, and a solid dark-gray track behind the remainder — replacing
  the old `█░` dotted bar. Periwinkle fill, a min-sliver floor so any nonzero usage
  shows at least `▏`, and a clamp at the window limit. Bar width is now 10 cells (was
  6). The status line composes the gauge raw rather than re-wrapping it in a
  background style, so the bar keeps its own per-cell backgrounds.

### Skills system + `/skill` (apogee-code feature-parity)

- **`internal/skills` package** discovers user-authored skills — a folder
  containing a `SKILL.md` (YAML frontmatter `id`|`name`, `displayName`,
  `summary`|`description`, plus a Markdown body; a no-frontmatter fallback sniffs
  the first lines) — from three layered dirs: `~/.apogee/skills`, the workspace's
  `.apogee/skills`, and (when `use-project-skills` is on) the workspace's bare
  `skills/`. Later source wins on an ID collision. Each dir is walked through
  `os.OpenRoot` so a symlink can't escape it; a missing dir is skipped and a
  malformed skill is skipped with a soft error (one bad file never blanks the
  catalog). No builtin/embedded skills and no auto-created `~/.apogee/skills` ship
  in v1 (additive future hooks).
- **`/skill` in the TUI** — the `/` menu offers `/skill`, which chains into a skill
  picker; a pick pops a chip above the input, and submit attaches the chosen IDs.
  An empty message with skills attached is a valid send. `/skill` is deliberately
  **not** a parser command (attachment is the only way it acts), so an unknown
  `/skill foo` is still sent as an ordinary message. `/clear` and `/compact` drop
  staged chips; `/continue` carries them.
- **Attached skills now resolve** (replaces the `SkillIDs` "reserved/ignored"
  stub): the loop maps each `UserInput.SkillIDs` entry through `Config.Skills` and
  prepends its body to the user message for that one Turn (order: skills → `@file`
  blocks → user text). An unknown ID, or any ID with no resolver wired, is reported
  via an `ErrorEvent` and dropped — never silently ignored.

### Configuration

- **`use-project-skills`** (config-file only, default **true**) gates discovery of
  the workspace's bare `skills/` folder (the global library and the project's
  `.apogee/skills` are always loaded). Documented in the seeded `config.yaml`.

### Chat input mini-language (core)

- **Parse/route layer** between the TUI input box and the agent: `/`-prefixed
  lines route to local command handlers, `@file` tokens are extracted as
  references, and an autocomplete overlay (commands + workspace files, the latter
  via a bounded `os.Root` walk) mirrors the approval-prompt overlay.
- **Commands**: `/clear` (drop the model's context, keep the visible transcript),
  `/continue` ("Please continue"), and `/compact` (generative compaction — the command
  surface and the `Agent.Compact` seam landed here; the reducer that folds the history
  through them shipped in the same track, see the "Context compaction (`/compact`)"
  section above).
- **`@file` references now resolve** (behaviour change): the loop reads each
  `UserInput.FileRefs` entry within the workspace fence (`security.SafeReadFile`,
  `os.Root`-pinned) and injects its content into the user message — replacing the
  prior "refs ignored" `ErrorEvent`. A missing, oversized, or escaping ref is
  reported and skipped; the Turn still proceeds.

### Public API (additive — minor)

- `Agent.ClearContext() error` — drop the conversation history at a quiescent
  boundary (the host's transcript is unaffected); refused mid-Exchange.
- `Agent.Compact(context.Context) (skipped bool, err error)` — on-demand generative
  Compaction: summarizes the conversation and folds the history at a quiescent boundary
  (refused mid-Exchange with `ErrInputPending`, like `ClearContext`). `skipped` is true
  when the conversation was too small past the protected prefix to fold — no upstream
  call, history untouched; always false on error.
- `UserInput.SkillIDs []string` — the skills attached in chat; the loop resolves
  each through `Config.Skills` and prepends its body to the Turn (was reserved).
- `Config.Skills SkillResolver` — host-supplied resolver for attached skill IDs
  (nil ⇒ attached IDs are reported and dropped). `SkillResolver` and its return
  type `ResolvedSkill` are re-exported on the root facade; the disk-backed catalog
  stays internal (`internal/skills`).

## [1.0.0] — 2026-06-25

The first stable release. `v1.0.0` cuts the public Go API after Phase 3 brought
the agent to feature-parity with apogee-code's non-UI behaviour, with **Auto
mode confined** on Linux (landlock) and macOS (seatbelt). Every consumer — the
TUI, the bench, and the embeddable library surface — has exercised the API, so
semver now begins (ADR 0001 §18, amended).

The public surface is the root `apogee` package: `Agent` (`New`/`Resume`),
`Config` and its host delegates (`EventSink`, `Approver`, `Asker`,
`ExternalEffects`), the four-rung `Mode` ladder, the `Tool`/`ToolRegistry`
extension point with the `ReadOnlyTool`/`ExternalEffectTool` markers, the
`Event` variants, and the hook points. Tools live behind the registry (an open
extension point, ADR 0002), not as root types.

### Confinement (Auto mode is real)

- **Blast-radius confinement model** (ADR 0012, supersedes ADR 0004): a tool
  call runs without a human gate only if its blast radius is bounded — by **OS
  confinement** for the unbounded subprocess/network surface, or by Apogee's own
  **path-safety-to-workspace** for its own in-process writes. Confinement
  attaches to blast radius, at a single **subprocess granularity** on every OS
  (no in-process per-thread landlock, no thread-discard).
- **Four-rung autonomy ladder**: Plan → Ask-Before → **Allow-Edits** → Auto.
  The new `ModeAllowEdits` rung auto-approves Apogee's own workspace-scoped
  writes (no confinement needed; identical on every OS) and gates everything
  else.
- **Linux landlock backend** (`//go:build linux`): ABI probed at startup; an
  honest capability matrix (`FSWrite` at ABI ≥1 / kernel ≥5.13, `NetworkEgress`
  at ABI ≥4 / kernel ≥6.7); a confined subprocess applies the landlock domain
  after fork, before `execve`, so the child is fenced and the parent stays
  unrestricted. Raw `golang.org/x/sys/unix` syscalls (now a direct dependency).
- **macOS seatbelt backend** (`//go:build darwin`): a `sandbox-exec` profile
  generated from the `ConfinementBox` (workspace-write-only + network-open by
  default), presence-probed, no new Go dependency.
- **`Confine(ctx, box, *exec.Cmd)`** prepare-in-place contract: the tool builds
  an idiomatic `*exec.Cmd`; the backend rewrites it to launch confined. The
  `confine-to-workspace` global-config key (default `true`) tunes Auto's blast
  radius; `confine-to-workspace=false` is the explicit "I am the sandbox"
  (VM-only) opt-out. `AutoEligible()` requires filesystem confinement only;
  where confinement is unavailable, subprocess tools gate through Approval
  ("confine if you can, gate if you can't") rather than refusing Auto.

### Tools (feature-parity with apogee-code's non-UI surface)

- **File-editing family**: find-replace (single + multi), `edit`/apply-edit,
  `diff`, `open-file` — pure-Go, stateless, carrying the unexported
  `workspaceScopedWriter` marker so Allow-Edits/Auto bound them by path-safety.
- **Execution tools**: `terminal` and `python-exec` — one-shot, stateless, the
  first `Confiner` consumers; process-group teardown on cancel
  (`Setpgid` + `cmd.Cancel` + `WaitDelay`).
- **`git` tool**: branch / commit / diff-range over the system `git`, detected
  and graceful-degrading when absent.
- **`diagnostics` tool**: in-process `go/parser` + optional `go vet`,
  read-only, graceful when the toolchain is absent.
- **Network + host tools**: `web_fetch`, `http_request`, `web_search`
  (external-effect, Approval-gated as MCP-kind / auto-run url-filtered as
  network-kind per the disposition table) and `ask_user` (the new `Asker` host
  delegate). These are routed through the `ExternalEffects.Do` boundary
  (ADR 0008) so the bench can stub them.
- The existing `read_file` / `write_file` / `list_dir` / `grep` built-ins carry
  forward; `write_file` carries the workspace-scoped-writer marker.

### Processing (parity-complete port)

- **All apogee-code tool-call formats parse**: native/JSON `tool_calls`,
  markdown-fenced, and custom-regex, each gated by **ported TS test vectors**.
- **Full harmony / thinking-channel set** handled, with a `processor-factory`
  that selects the format per model/response. The package stays `domain`-only.

### Security guardrails (the human-in-the-loop layer)

- **`internal/security`** consolidates the Phase-1 per-tool path-safety into one
  reusable guard and adds **url-safety**, an **arg-guard**, a **circuit-breaker**
  (halts a runaway tool-loop), and an **audit record** (bounded ring buffer with
  a dropped-count). These run in all modes and a sub-agent inherits them.
- **Two-tier dangerous-action guard** (a footgun-guard, NOT a security
  boundary): a hard-refuse tier (`rm -rf` of root/home/system, fork bombs,
  `~/.ssh`/credential/persistence writes) and a force-approval tier
  (`curl | bash`-class). It runs first and is **tighten-only**; project config
  may only add rules, never dissolve a floor rule by ID.
- **Default-on SSRF floor** for the network tools: loopback / private ranges /
  IMDS `169.254.169.254` / link-local / CGNAT / `0.0.0.0` / NAT64 denied by
  **resolved IP** (pre-flight and at dial time, closing DNS-rebinding),
  tighten-only.

### Sub-agents

- **Sub-agent orchestrator** (ADR 0013): a sub-agent is the embeddable `Agent`,
  constructed through an internal orchestrator that threads the parent's `Mode`,
  `Approver`, `Confiner`, and guardrails verbatim (or stricter) with a tool
  **`Subset` ≤ the parent's** (ADR 0005). It is exposed as a
  dispatch-transparent **`sub_agent`** recursion point — never confined or gated
  as a unit; each child tool call gets the full per-call disposition one level
  down.
- **Isolated live guard state** (`Guards.ForSubAgent`): a sub-agent gets a fresh
  circuit-breaker and audit log over a shared read-only dangerous ruleset.
- Nested events re-emit into the parent stream at **`Depth = parent.Depth + 1`**.
- Stepping is **top-level-only for v1** behind a swappable driver; a sub-agent
  runs atomically within the parent Turn (no mid-sub-agent snapshot; cancel
  rolls back to the parent's pre-`sub_agent` boundary).

### MCP

- **MCP client** on the official Go SDK (`modelcontextprotocol/go-sdk` v1.6.1):
  stdio / SSE / streamable-http transports. Server tools surface into the
  registry as `ExternalEffectTool` of kind `mcp`, so they **Approval-gate in
  Auto** under `confine-to-workspace=true` (an external server Apogee cannot
  fence). **Resume reconnects fresh** — no server-side-state promise (ADR 0008).

### TUI

- **Nested-event rendering**: `Depth > 0` sub-agent events render as a framed,
  labelled block (Phase-2's "tolerate" → "render").

### Notes

- Cross-build stays green on all 6 targets (linux/darwin/windows ×
  amd64/arm64, `CGO_ENABLED=0`); OS-specific confinement is build-tagged behind
  the `denyConfiner` (Windows/other) fallback. **Windows confinement is Phase 5**
  — Auto is simply unavailable on Windows until then.
- The `internal/` packages never import the root module path (ADR 0010).
- Direct dependency additions this release: `golang.org/x/sys` (landlock),
  `github.com/google/shlex` (terminal command splitting),
  `github.com/modelcontextprotocol/go-sdk` (MCP client).

### Known post-release verification (owner-run / CI)

These confinement **enforcement** proofs cannot run in the development
environment and are deferred to an owner-run / CI verification after the tag.
They are not acceptance failures — the hermetic disposition/logic tests (caps
honesty, generated profile strings, command rewriting, fail-closed paths) run
on every host and pass, and the live escape-probe batteries **self-skip loudly**
where the OS cannot enforce:

- **Linux landlock live enforcement** — the dev-host kernel has
  `CONFIG_SECURITY_LANDLOCK` **off**, so `confinetest.Probe` self-skips here.
  Confirm on a landlock-enabled kernel (≥5.13 fs, ≥6.7 net) that a confined
  subprocess's out-of-workspace write and non-allowlisted connect are OS-denied
  while the parent stays unrestricted.
- **macOS seatbelt live enforcement** — ✅ **confirmed on macOS hardware
  (2026-07-02).** `confinetest.Probe` now runs under live `sandbox-exec` on a real
  Mac: a confined subprocess is fenced to the workspace, out-of-box and `~/.ssh`
  writes are OS-denied, the parent stays unrestricted, and network-deny tightens
  while network-open connects. (This surfaced and fixed the box-root canonicalization
  bug below.) The Linux landlock arm above is still open.
- **Live Auto-confined deliverable run** — the opt-in `APOGEE_LIVE_ENDPOINT`
  end-to-end run (a real coding conversation in Auto, a shell write outside the
  workspace OS-denied, an MCP tool still raising Approval, a sub-agent delegated
  and its nested work rendered) is owner-run on Linux (landlock) and macOS
  (seatbelt). *(Still open.)*
- **Box-root canonicalization** — ✅ **resolved (2026-07-02).** Was a real bug, not
  just a verification gap: seatbelt embedded box roots verbatim and denied every
  in-workspace write when the root passed through a symlink (macOS `/var`, `/tmp`).
  Fixed by resolving each writable root through symlinks in `seatbeltProfile`; see
  the `[1.1.0]` Fixes entry.

[1.1.0]: https://github.com/airiclenz/apogee/releases/tag/v1.1.0
[1.0.0]: https://github.com/airiclenz/apogee/releases/tag/v1.0.0
