# Plan: ISSUES.md backlog batch — the open defect, both bounded TUI fixes, the url-safety config key, the Inspector, and delegate-request depth

- **Goal:** close six ISSUES.md items that need no further grill, bench, or hardware: the
  `internal/security/doc.go` file-map defect, the phantom-wrapped-line click imprecision, the
  `hangingPrefixes` narrow-width overrun, the dedicated url-safety config key, the Inspector /
  raw-protocol view, and sub-agent depth on `PresentRequest`/`AskRequest`.
- **Date:** 2026-08-15
- **Status:** ready to execute
- **Sized for:** ~200k-context host
- **Authoritative sources:** `ISSUES.md` (the six entries this plan closes — each closing item
  removes its entry); ADR 0030 (width authority — the widget, never `width.md`, is the wrap
  oracle), ADR 0027 (caret family), ADR 0031 (engine invariants: wire-silent = no network-facing
  control surface, and every new surface must stay benchable in-process), ADR 0039 (sub-agent
  identity on requests), ADR 0010 (public surface = root aliases; additive fields only),
  `layout.md` (pane give-way order, column contract); `internal/security/ssrf.go:14-56` and
  `urlsafety.go` doc comments (the tighten-only SSRF-floor contract).
- **Ratified design calls** (owner via AskUserQuestion, 2026-08-15):
  1. **Inspector:** new editable bool key `ui.inspector` (default false) arms wire capture; a
     provider-client observer feeds a new `domain.WireEvent` through the sink (API credentials
     never in the payload); `/inspect` opens a non-modal `/usage`-shaped pane over a bounded ring
     of recent request/response JSON. Key off → zero capture cost; `/inspect` then explains how
     to enable.
  2. **url-safety key:** hosts only, network tools only. Top-level `url-safety:` block with
     `allow-hosts` / `deny-hosts`, file-only (the `tools.disabled` convention), entries
     normalized at load. Scheme allow-set stays code-level; MCP endpoints stay unguarded
     (operator-configured, different trust class).
  3. **Narrow hanging wrap:** when the block cannot hold marker + one text column, the hang
     collapses to zero — marker and indent dropped, text wraps flat at full block width. Same
     rule for the same-shape siblings. `layout.md` gains the rule.
  4. **Depth seam:** both tool-built requests get it — `PresentRequest` gains `Depth` +
     `SpawnCallID` (rail fix and run-ordered insertion), `AskRequest` gains `Depth` — via one ctx
     carrier set installed at the dispatch seam.
  - Scope selection itself (which ISSUES items are in/out) was owner-ratified the same session.
  - Sub-decisions inside those calls (record-per-call response capture, startup-only arming,
    ring size) were resolved by the plan author at write time and appear as binding text in the
    items below.
- **Standing requirements:** skills: coding-standards. Any authorized deviation from item text
  lands as a dated NOTES line under the item. Per the repo conventions, an item that closes an
  ISSUES.md entry removes that entry in the same commit; the CHANGELOG entry travels via the
  run's sidecar as usual. No version identifier changes (see the closing note).
- **Out of scope:** tool×mode matrix, `InjectContext` placement, mid-Exchange compaction,
  adaptive prompt complexity (each needs its own grill/bench per its ISSUES entry); Phase-5
  owner-run passes, Windows `%TEMP%`/job-object (hardware / own design session); model-facing
  `schedule` tool (daemon-era); code signing; tool-surface bench arms; sampling params on the
  Model profile (demand-driven, owner call 2026-08-14); the never-run delegation prompt (owner
  deferred 2026-08-11); "Undo all agent changes" (owner deferred this session); session
  retention/pruning and the cross-instance lock (owner left parked this session); the
  in-transcript startup banner (owner left parked this session); guarding MCP endpoints with the
  url-safety key; surfacing the scheme allow-set; live re-arming of Inspector capture
  mid-session; `URLGuard.DisableIPFloor` as a config key (forbidden — code-level only).

## 1. Name `ErrRootInaccessible` in the security package file map — ✅ DONE (2026-08-15)

NOTES (2026-08-15): the removed ISSUES.md bullet was the only one under the `### Run residuals —
open (2026-08-15, audit-2026-08-11 confirmed findings)` subsection, so that subsection heading and
its one-line preamble went with it — an empty subsection whose preamble narrates a closed run is
exactly what the file's conventions forbid. The `## Open defects` section heading stays, now empty,
per the item's instruction.

**What:** `internal/security/doc.go`'s file map names a sentinel per file — `ErrPathEscape` on
the `pathsafety.go` line (`doc.go:86`), `ErrSymlinkedParent` on the `safeio.go` line (`:92`) —
but `pathsafety.go` now also defines `ErrRootInaccessible`
(`internal/security/pathsafety.go:26`), which the map omits. Add it to the `pathsafety.go`
line so the map names every sentinel the file defines. Remove the corresponding open-defect
entry from `ISSUES.md` (the "Run residuals — open (2026-08-15 …)" bullet; if that leaves the
"Open defects" section empty, leave the section heading in place per the file's conventions).

**Files:** `internal/security/doc.go`, `ISSUES.md`

**Tests:** none new — `TestDocMapNamesEveryFile` (`internal/security/docmap_test.go:13`) pins
that `doc.go` names every file; the sentinel completeness is prose. Confirm the existing test
still passes.

**Acceptance:** `go build ./... && go test ./internal/security/`

**Commit:** `docs(security): file map names ErrRootInaccessible beside pathsafety.go`

## 2. A hang the block cannot hold collapses to zero — ✅ DONE (2026-08-15)

NOTES (2026-08-15): `popupWrappedRowLines` needs no collapse of its own — audited and left as-is per
the item's own alternative. Its single caller `popupRowBlocks` (`popup.go:883`) already guards
`hang == 0 || hang >= budget` and breaks the composed line whole at the full budget there, which IS
this collapse taken one level up, so layout.md:1629's "single-cell row measures a hanging indent of
zero" holds by construction. Only its doc comment gained a sentence naming that invariant.
NOTES (2026-08-15): the item names the third site as `userBlockAccentRows`; no such function exists
— `internal/tui/userblock.go:153` (the cited line) is inside `userBlockCellSpans`, the accent-row
mapping, and that is what took the rule. It must, and not only for the cap: it re-wraps to ask the
same oracle the block's rows came off, so a lead counted there that `hangingPrefixes` did not draw
would shift every accent right by the marker the block had shed.

**What:** `hangingPrefixes` (`internal/tui/wrap.go:31`) wraps at `max(1, width-mw)` and then
prepends the `mw`-column marker, so at block width 1–2 a two-column bullet marker yields
three-cell lines — breaking layout.md's absolute width cap. **Binding rule (ratified call 3):**
when `width < mw+1`, drop the marker AND the continuation indent entirely and wrap the text
flat at the full block width; at `width >= mw+1` behaviour is unchanged. Apply the identical
rule to the same-shape siblings that repeat the floor: `gutteredWrap`
(`internal/tui/toolblock.go:370`) and the `userBlockAccentRows` lead path
(`internal/tui/userblock.go:153`). Audit `popupWrappedRowLines` (`internal/tui/popup.go:956`,
no floor of its own — it relies on `wrapText`'s internal `limit<1 → 1`): if it can compose a
row exceeding its budget the same way, apply the same collapse; if `layout.md:1629`'s
"single-cell row measures a hanging indent of zero" already holds there by construction, leave
it and say so in a NOTES line. Constraints that must survive: `clipWrap`
(`internal/tui/wrap.go:66-80`) keeps returning byte-identical `hangingWrap` lines for fitting
text, and `clipCells`' empty-marker path (`toolleader.go:321`, `mw==0`) stays a no-op.
`wrapText` itself is not changed. Add the rule to `layout.md` beside its narrow-window
doctrine (near `:1612-1629`): a block too narrow to hold a hanging marker plus one text column
collapses the hang to zero — markers shed whole, never squeezed (consistent with the
marker-shedding ladder at `:511-514`). Remove the `hangingPrefixes` entry from `ISSUES.md`
("The TUI width authority — what it did not convert"): delete the width-1–2 paragraph and the
"What is open here…" sentence, but KEEP the "Standing rules the closed work left behind"
paragraph — those rules bind future work; retitle/trim the entry so it reads as standing rules
only.

**Files:** `internal/tui/wrap.go`, `internal/tui/toolblock.go`, `internal/tui/userblock.go`,
`internal/tui/popup.go`, `internal/tui/wrap_test.go`, `layout.md`, `ISSUES.md`

**Tests:** new `wrap_test.go` case sweeping `hangingPrefixes` (via `hangingWrap`) over widths
0…6 with a two-column marker, asserting no returned line measures wider than `max(width, 1)`
in the painter's measure — run under the existing `paintMethods` matrix as
`TestWrapTextHoldsTheWidthCap` (`wrap_test.go:40`) does. Tighten
`TestClipWrapSurvivesNarrowWidths` (`wrap_test.go:309`): its current allowance
`max(width, clipTailWidth)` deliberately permits the marker overrun — keep the clipTail
allowance (that is a different, intended floor) but add an assertion that the marker itself
never overruns. Keep `TestClipWrapLeavesFittingTextAlone` green (parity constraint). Existing
`markdown_test.go` list tests (`TestListHangingIndent:191` etc.) must stay green — they run at
normal widths.

**Acceptance:** `go build ./... && go test ./internal/tui/`

**Commit:** `fix(tui): a hang the block cannot hold collapses to zero instead of overrunning the cap`

## 3. A prompt click below a phantom-wrapped line seats the caret exactly

**What:** the MOUSE path's `reseatCaret` (`internal/tui/lineeditor.go:231`) is a bare
`MoveToBegin` + `visRow × CursorDown` loop, and bubbles' `CursorDown` can never enter the
phantom trailing sub-line `wrap` appends to a width-filling logical line — so a click below
such a line lands a row short. Rewrite `reseatCaret` as the Height-aware walk `seatCaret`
(`lineeditor.go:287`) already expresses for logical targets, aimed at a *visual* target: from
`MoveToBegin`, step whole logical lines (`CursorEnd()` then `CursorDown()`, with `seatCaret`'s
`before := e.input.Line()` no-progress break so termination stays explicit) while accumulating
each logical line's visual row count from the widget's own `LineInfo().Height`, until the
target `visRow` falls inside the current logical line; then seat the residual sub-row.
**Binding:** a click on the phantom trailing sub-row seats the caret at that logical line's
END (the same place `CursorEnd` puts the keyboard caret) — the phantom row is clickable, never
skipped. Finish with the `SetHeight(e.input.Height())` no-op re-clamp `seatCaret` ends with
(today `reseatCaret` lacks it). The widget is the oracle (ADR 0030 §6) — do not derive counts
from `wrapRowStarts` inside the walk; `wrapRowStarts` (`inputaccent.go:221`) may serve tests as
the independent mirror. Downstream callers `caretTo` (`lineeditor.go:242`) and the two mouse
call sites (`mouse.go:402`, `:439`) are unchanged. Remove the ISSUES.md entry (the [P2]
phantom-wrap click bullet under "apogee-code feature parity"); its cited line numbers are
stale, which dies with it.

**Files:** `internal/tui/lineeditor.go`, `internal/tui/mouse_test.go`,
`internal/tui/prompteditor_test.go`, `ISSUES.md`

**Tests:** new mouse-path test beside `TestClickPositionsCaretMultiline` (`mouse_test.go:151`):
a draft whose first logical line exactly fills the inner width (so it carries a phantom row),
click on the row below it — assert the caret seats on the SECOND logical line's first cell, not
a row short; plus a click on the phantom row itself — assert the caret sits at line 1's end.
Model it on `TestPromptEditorCaretToOffsetCrossesWrappedRows` (`prompteditor_test.go:110`),
including its reseat-stability assertion (`reseatInput()` does not move the caret afterwards).
Run a CJK variant (wide runes shift the fill point — `TestClickPositionsCaretCJK:650` is the
pattern).

**Acceptance:** `go build ./... && go test ./internal/tui/`

**Commit:** `fix(tui): a prompt click below a phantom-wrapped line seats the caret exactly`

## 4. url-safety config key — the config pipeline

**What:** add the file-only `url-safety:` block (ratified call 2) through the config package's
six-stop pipeline, mirroring `tools.disabled` at every stop (file-only: bare slices, never
pointers; env/flag never touch it): (1) `fileConfig` (`internal/config/config.go:903`) gains a
`URLSafety *urlSafetyConfig` section struct with `AllowHosts []string \`yaml:"allow-hosts"\`` and
`DenyHosts []string \`yaml:"deny-hosts"\``; (2) `fileConfig.layer()` (`:1838`) copies both into
new `Layer` fields (`:487` — bare slices, per the file-only convention at `:562`); (3)
`ResolveSettings` (`:714`) carries them into `Settings` (`:42`) with the same "file-only;
env/flag never…" comment discipline as `ToolsDisabled` (`:754`); (4) `ApplyConfig` (`:2155`)
copies them onto two new `Options` fields (`internal/config/options.go:17`). Registry
(`internal/config/registry.go`): two rows, `url-safety.allow-hosts` and `url-safety.deny-hosts`,
`KindStringList`, no validator (mirroring `tools.disabled:239` — entries are normalized
permissively at use, item 5), placed per the registry's block-order rule (comment at `:143`) —
the bijection test will hold you to it. Settings pane: give the new section the same presence
`tools.disabled` has (section row + accessors in `cmd/apogee/settingsrows.go`, pattern at
`:84`/`:123`). Embedded template (`internal/config/defaults/config.yaml`): document the block
commented-out beside the `tools:` block (`:377-394` is the pattern);
`TestEmbeddedDefaultConfigSetsOnlyTheSystemPrompt` (`defaults_test.go:63`) enforces
commented-out. This item stops at `Options` — no engine or tools change here (item 5).

**Files:** `internal/config/config.go`, `internal/config/options.go`,
`internal/config/registry.go`, `internal/config/defaults/config.yaml`,
`cmd/apogee/settingsrows.go`, `internal/config/config_test.go`

**Tests:** an end-to-end YAML→Options test modeled on `TestApplyConfigToolsDisabled`
(`config_test.go:1681`) covering both lists; `TestRegistryIsBijectionWithFileConfig`
(`registry_test.go:25`) and `TestRegistryRowInvariants` (`:216`) must pass with the new rows
(they fail the build if a stop is missed — that is the point of mirroring).

**Acceptance:** `go build ./... && go test ./internal/config/ ./cmd/apogee/`

**Commit:** `feat(config): a file-only url-safety block carries allow-hosts and deny-hosts to Options`

## 5. url-safety config key — the lists reach the network tools' guard

**Depends on item 4.**

**What:** thread the two `Options` fields into the running guard. (1) `domain.Config`
(`internal/domain/config.go`) gains `URLAllowHosts`/`URLDenyHosts []string` (additive, D7-safe);
both drivers copy them from `Options` — `cmd/apogee/wire_boot.go:174-179` region and
`cmd/apogee/headless.go:387-391` region. (2) Both `HostTools` composition sites replace their
hardcoded `security.URLGuard{}` with a guard built from the config lists:
`internal/agent/construct.go:404` (update the deferral doc comment at `:398-401` — this IS the
"thin later addition" it promised; keep its warning: never seed from `ConfineNetworkAllow`) and
`cmd/apogee/wire_tools.go:159` (`wire_test.go:873-888` documents the two must mirror). (3)
**Normalization (binding):** config entries must match `NormalizeURL`'s dialled form, so
normalize each list entry at guard construction with a new exported helper in
`internal/security` (e.g. `NormalizeHostPattern`, beside `NormalizeURL` at `urlsafety.go:162`)
applying the same host normal form — trim, IDNA-to-ASCII, lowercase, strip trailing root dots —
so `Example.COM.` in config matches. Put the helper + guard-building in ONE place both
composition sites call (a small exported constructor in `internal/security` or a shared helper
in `internal/tools` — one deep seam, not two copies of the loop). (4) **Tighten-only holds by
construction:** the new fields populate only `AllowHosts`/`DenyHosts`; `disableFloor` stays
unexported and unreachable from config (`urlsafety.go:49`, `:69`) — no code change needed,
`TestURLGuard_FloorIsTightenOnly` (`ssrf_test.go:293`) pins it. MCP wiring
(`cmd/apogee/wire_live.go:40`, `:50`) stays `security.URLGuard{}` — out of scope by ratified
call 2. The key is startup-only: `liveTools`' memoized rebuild set
(`cmd/apogee/wire_tools.go:~40-120`) is NOT extended (file-only keys have no live edit path).
Remove the "Dedicated url-safety config key" entry from `ISSUES.md`; its `HostTools`-duplication
trap note migrates into the doc comment at the shared constructor if not already covered.

**Files:** `internal/domain/config.go`, `cmd/apogee/wire_boot.go`, `cmd/apogee/headless.go`,
`internal/agent/construct.go`, `cmd/apogee/wire_tools.go`, `internal/security/urlsafety.go`,
`internal/security/urlsafety_test.go`, `internal/tools/registry_test.go`, `ISSUES.md`

**Tests:** extend `TestNewDefaultRegistryWithHost_ThreadsURLGuardIntoNetworkTools`
(`internal/tools/registry_test.go:136`) so a config-supplied deny reaches all three network
tools; a `NormalizeHostPattern` table test (case, IDNA, trailing dot); a construct-level test
asserting `domain.Config.URLDenyHosts` lands in the guard on BOTH composition paths (the
`wire_test.go:873` mirror test is the pattern); `TestURLGuard_FloorIsTightenOnly` stays green
untouched.

**Acceptance:** `go build ./... && go test ./internal/security/ ./internal/tools/ ./internal/agent/ ./cmd/apogee/`

**Commit:** `feat(tools): config url-safety hosts reach the network tools' guard, tighten-only`

## 6. Inspector — the provider client can report its wire traffic

**What:** `internal/provider` gains an opt-in wire observer so the bytes it already builds and
parses can be seen without changing any existing behaviour. New exported types in
`internal/provider/wire.go`: `WireRecord{Direction WireDirection; Payload []byte}` with
directions `WireRequest`/`WireResponse`, and a new `Client` option `WithWireObserver(func(WireRecord))`
(`client.go:110-135` holds the option set). **Binding capture shape (one record per direction
per call, actual bytes):** the request record is the marshalled body exactly as posted
(`client.go:190` / `buildBody:398`) — headers are NEVER part of the record, so the API key
(`setAuth:464`) cannot leak by construction; the response record is the concatenation of the
raw SSE `data:` payload lines as received (`parseSSE`, `stream.go:139`), newline-joined,
delivered once at stream end — plus error-path bodies already sanitized (`sanitize:472`,
`statusDelta:93`). A nil observer costs nothing on the hot path (guard every capture with a
nil check; the SSE accumulation must not run at all when the observer is nil). No retention in
the provider — it calls the observer and forgets.

**Files:** `internal/provider/wire.go`, `internal/provider/client.go`,
`internal/provider/stream.go`, `internal/provider/client_test.go`,
`internal/provider/stream_test.go`

**Tests:** with an observer installed against the package's existing fake upstream: exactly one
request record whose payload round-trips as the posted JSON body (no Authorization material
present), exactly one response record containing every SSE data payload in order; with no
observer, no accumulation occurs (behaviour identical — existing suite green).

**Acceptance:** `go build ./... && go test ./internal/provider/`

**Commit:** `feat(provider): an opt-in wire observer reports request and response bytes`

## 7. Inspector — `ui.inspector` arms capture and the engine emits `WireEvent`s

**Depends on item 6.**

**What:** the ratified arming path (call 1). (1) Config: new editable bool key `ui.inspector`
(default false) — `uiConfig` (`internal/config/config.go:1641-1661`), registry row in the `ui.`
block (`registry.go:297-325` region), settings row (`cmd/apogee/settingsrows.go`), commented-out
template doc (`internal/config/defaults/config.yaml`), pointer-typed like other
file/env-overridable scalars, through `layer()`/`ResolveSettings`/`ApplyConfig` to an `Options`
field. (2) `domain.Config` gains `Inspector bool`; both drivers plumb it
(`cmd/apogee/wire_boot.go`, `cmd/apogee/headless.go`). (3) New event
`domain.WireEvent{EventBase; Direction string; Payload string}` in
`internal/domain/events.go` (the vocabulary at `:65-286`; directions `"request"`/`"response"`).
(4) Engine: when `Config.Inspector` is true, `internal/agent`'s construction
(`construct.go:33` region) installs a `provider.WithWireObserver` that emits a
`domain.WireEvent` through the existing `EventSink` with a proper `EventBase` (the loop's
`a.base(turn)` pattern, `loop.go:1138` — depth/callID stamp sub-agent traffic correctly for
free). When false, no observer is installed — zero cost, the ratified off-state. **Binding:**
arming is startup-only in this plan; a mid-session `/settings` edit of `ui.inspector` takes
effect next start (live re-arm is out of scope — state this in the key's template doc line).
This event crosses the engine seam as data, not a control surface — ADR 0031's wire-silence is
about control surfaces, and a `domain.Event` is exactly the benchable-in-process shape
invariant 4 demands; note this in the event's doc comment.

**Files:** `internal/config/config.go`, `internal/config/options.go`,
`internal/config/registry.go`, `internal/config/defaults/config.yaml`,
`cmd/apogee/settingsrows.go`, `internal/domain/config.go`, `internal/domain/events.go`,
`internal/agent/construct.go`, `cmd/apogee/wire_boot.go`, `cmd/apogee/headless.go`,
`internal/config/config_test.go`, `internal/agent/construct_test.go`

**Tests:** YAML→Options test for `ui.inspector` (registry bijection + invariants enforce the
rows); an agent-level test with a fake upstream: `Inspector: true` → the sink sees a
request-direction and a response-direction `WireEvent` per model call with correct
`EventBase`; `Inspector: false` → zero `WireEvent`s.

**Acceptance:** `go build ./... && go test ./internal/config/ ./internal/domain/ ./internal/agent/ ./cmd/apogee/`

**Commit:** `feat(engine): ui.inspector arms wire capture as WireEvents through the sink`

## 8. Inspector — `/inspect` opens the raw-protocol pane

**Depends on item 7.**

**What:** the TUI half. (1) Fold: `WireEvent`s land in a bounded ring on the model — **binding:
keep the most recent 20 records** (a full request body repeats the whole conversation; 20
bounds memory while covering a debugging session's tail), each entry holding direction, turn,
depth, and the payload passed through `stripEscapes` (the transcript's escape-strip precedent)
before it can reach the terminal. The ring lives beside the transcript, not in it — wire
records are not transcript entries and must not disturb entry folding; it survives
`transcript.reset()` the way `debug` does (`transcript.go:566-582`) only if trivially cheap,
otherwise clears with the session (implementer's mechanical call). (2) Command: `commandSpecs`
row `inspect` (`internal/tui/command.go:171`, alphabetical order — the pinning test enforces),
`whileRunning: true`, no args; dispatch case in `commandrun.go:200`'s switch. (3) Pane: a new
`internal/tui/inspector.go` modeled on `/usage` (`internal/tui/usage.go` — state struct `:44`,
open `:96`, key claim `:112` for esc + the four scroll keys, close, render): non-modal,
scrollable, pretty-printing each record's JSON with direction/turn header rows, newest last.
Registered in `frameOverlays()` (`model.go:2452-2467`) and the key-precedence ladder
(`model.go:1077-1130`); **binding:** it slots into layout.md's give-way order beside `/usage`
(`layout.md:145-152`) and follows the shared popup contract (scrollbar per `:541`, bordered-pane
chrome per `:227`). When `ui.inspector` is off (ring empty and config says disarmed),
`/inspect` renders one explanatory row naming the key instead of an empty pane. (4) Docs:
`layout.md` gains the pane in the give-way list; remove the Inspector `[P2]` bullet from
`ISSUES.md`'s apogee-code parity entry.

**Files:** `internal/tui/inspector.go`, `internal/tui/command.go`,
`internal/tui/commandrun.go`, `internal/tui/model.go`, `internal/tui/inspector_test.go`,
`internal/tui/command_test.go`, `layout.md`, `ISSUES.md`

**Tests:** fold test — N>20 `WireEvent`s keep exactly the latest 20 in order; `/inspect` with
records renders direction headers and payload rows (golden or substring, matching the package's
pane-test idiom in `usage.go`'s tests); `/inspect` disarmed renders the explanatory row; esc
closes and returns the rows to the transcript; the alphabetical-specs pin stays green; a
payload containing escape bytes reaches the pane stripped.

**Acceptance:** `go build ./... && go test ./internal/tui/`

**Commit:** `feat(tui): /inspect shows recent wire traffic behind ui.inspector`

## 9. Depth and spawn identity ride the dispatch ctx onto tool-built requests

**What:** ratified call 4, the engine/domain half. (1) New ctx carriers in
`internal/domain/ask.go` beside `WithSubAgentTask`/`WithSubAgentName` (`:97`, `:120`):
`WithSubAgentDepth(ctx, int)` / `SubAgentDepthFromContext` and `WithSpawnCallID(ctx, string)` /
`SpawnCallIDFromContext` (same shape, same file — one carrier set serves both requests). (2)
`internal/agent/dispatch.go` installs them in `executeTool` (`:737-764`) from `a.depth`
(`agent.go:194`) and `a.callID` (`agent.go:195`) — **outside** the `if a.task != ""` gate at
`:751` (depth 0 / empty spawn are honest values for the root agent; the existing gate keeps its
own two installs). (3) `domain.PresentRequest` (`internal/domain/present.go:42-56`) gains
`Depth int` and `SpawnCallID string` (additive — the type's own doc at `:38-41` sanctions it;
the root alias at `apogee.go:242` makes it freeze-safe only as an addition);
`internal/tools/present_document.go:110-114` populates both from the ctx. (4)
`domain.AskRequest` (`internal/domain/ask.go:41-82`) gains `Depth int`;
`internal/tools/ask_user.go:150-163` populates it from the ctx (SpawnCallID is not added to
AskRequest — the ask pane is not a railed transcript entry; depth buys identity only, per the
ratified call). This item does not touch the TUI (item 10).

**Files:** `internal/domain/ask.go`, `internal/domain/present.go`,
`internal/agent/dispatch.go`, `internal/tools/present_document.go`,
`internal/tools/ask_user.go`, `internal/tools/present_document_test.go`,
`internal/agent/delegationname_test.go`

**Tests:** extend `TestPresentDocument_RequestCarriesAbsolutePathDisplayPathAndTitle`
(`present_document_test.go:133`) with ctx-installed depth/spawn asserting both land on the
request; extend the `delegationname_test.go:80-95` fixture (its `asker.seen` recorder) with an
`AskRequest.Depth` assertion; add the missing agent-level presenter coverage — a recorder
delegate asserting a depth-1 sub-agent's `PresentRequest` arrives with `Depth: 1` and its spawn
call ID (the coverage hole the scout named: `PresentRequest` has no `internal/agent` test at
all).

**Acceptance:** `go build ./... && go test ./internal/domain/ ./internal/agent/ ./internal/tools/`

**Commit:** `feat(domain): tool-built requests carry sub-agent depth and spawn identity`

## 10. A presented document renders at its run's depth, inside its run

**Depends on item 9.**

**What:** the TUI half of ratified call 4. (1) `internal/tui/presenter.go:101-124` copies the
request's new `Depth` and `SpawnCallID` onto `presentedMsg`
(`internal/tui/messages.go:73-87`). (2) `transcript.addPresented`
(`internal/tui/transcript.go:519-527`) fixes both defects the scout confirmed: build the entry
with `depth` and `spawnCallID` set, and insert via `t.place(...)` (`:293-303`) instead of raw
`append`, so a sub-agent's presentation lands INSIDE its own run's stretch (under concurrent
fan-out, `spawnCallID` is what picks the right sibling run) — which is exactly what stops
`subAgentSpan` (`subagentblock.go:27-37`) truncating at the depth-0 entry and breaking the
rail. The renderer needs no change: `render.go:530-531` already rails `entryPresented` by
`e.depth`. The codec round-trips `depth` generically (`transcriptcodec.go:302`, `:389`) — no
codec change; confirm the presented payload replays railed. (3) Remove the "A presented
document carries no sub-agent depth" entry from `ISSUES.md`, including its `AskRequest`
parenthetical (item 9 closed that half).

**Files:** `internal/tui/presenter.go`, `internal/tui/messages.go`,
`internal/tui/transcript.go`, `internal/tui/presenter_test.go`,
`internal/tui/transcript_test.go`, `ISSUES.md`

**Tests:** extend `TestPresentedEntryRendering` (`presenter_test.go:476`) with a depth-1 case
asserting the block renders railed (`railLines` output at depth 1); a placement test: a
presentation arriving mid-run with the run's `spawnCallID` inserts inside the run's stretch and
`subAgentSpan` still reaches past it (the rail no longer splits — pattern on
`TestTranscriptDepthRendersFramedBlock`, `transcript_test.go:986`); `TestUpdateFoldsPresentedMsg`
(`presenter_test.go:594`) extended for the two new msg fields; a codec round-trip case
asserting a depth-1 presented entry replays at depth 1.

**Acceptance:** `go build ./... && go test ./internal/tui/`

**Commit:** `fix(tui): a presented document renders at its run's depth, inside its run`

## Suggested version bump

This plan ships two user-facing features (the url-safety config block, the Inspector) plus two
visible TUI fixes — a minor bump (0.15.0) is suggested when the plan closes. No item changes
`VERSION` or `CHANGELOG` release headings; the bump is the owner's call after the run.
