# Settings surface: live apply and full editability — implementation plan

- **Goal:** make the `/settings` pane the place where every configuration key can be inspected AND changed, with every edit taking effect in the running session — kill every "(next launch)" marker and every "edit in config.yaml" dead-end, add the layout polish (description header, white sections, spacers, highlighted edit row), real cursor+mouse text editing, a selection popup for `server`, and an `$EDITOR` jump with reload-and-apply for the nested structures. Includes live MCP reconnect (owner chose to build it now, not defer).
- **Date:** 2026-08-06
- **Status:** not started
- **Authoritative sources:**
  - `docs/design/settings-screen-layout.md` — the owner's layout mockup + 7 requirements. When an item's text and this spec disagree, the spec wins.
  - The **Ratified design calls** list below — resolved with the owner via AskUserQuestion in the 2026-08-06 planning session. It is the ground truth for every behavioral choice; item 1's ADR 0037 *documents* these calls, it does not originate them.
  - ADR 0035 — the persist-one-key-per-edit contract. Its persistence model (validate → single-key comment-preserving splice → atomic write) is UNCHANGED and binding. Its decision 9 (mode-only live apply; "(next launch)" markers are correct; pane never rebinds) is superseded by this plan (item 1 records that formally).
  - ADR 0031 — door-keeping: every new engine mutator must be a driver-agnostic seam (wire-silent engine, benchable-all-the-way-up). No TUI-only backdoors into the engine.
  - ADR 0012 — confinement stays single-homed in `/confine`; this plan never makes those keys pane-editable.
  - ADR 0036 — the `servers:` list is the single upstream definition; the recorded `server:` choice is the startup binding.
  - `layout.md` §settings pane (L215–247, L1134–1148) — the pane grammar; item 18 amends it to match.
- **Ratified design calls** (owner, 2026-08-06, via AskUserQuestion unless noted):
  1. **Edits apply on ⏎ commit.** Each committed edit persists AND applies at once (the `mode` idiom generalized). Closing the pane is dismissal only; nothing is batched.
  2. **Prompt-prefix keys apply at the next natural boundary.** `system-prompt-*` lands at the next engine-idle rebind (effectively the next turn); `context-files.*` at the next `/clear` or session start (KV-prefix stability is deliberate and stays). A short row note is allowed ("· applies at next clear"). NO key ever says "(next launch)".
  3. **A pane edit outranks an env/flag override for the running session.** The edit live-applies and persists; the row notes that the override wins again at next launch. Startup precedence (flag > env > file > default) is unchanged.
  4. **The `server` row performs the full live switch** — identical to `/server` (mover + heartbeat rebind + recorded choice), driven from a selection popup. Never a free-text buffer.
  5. **Structured editing is hybrid.** Promote to in-pane editing: `system-prompt-file` (string row), `context-files.names` (list row), `system-prompt-text` (multiline editor). The six nested structures (`servers`, `system-prompt-models`, `mcp-servers`, `mechanisms`, `validated-sets`, `model-profile`) get ⏎ → suspend to `$EDITOR` at the key's line → reload + validate + live-apply on return. The label "edit in config.yaml" disappears everywhere.
  6. **Live MCP reconnect is IN this plan** (owner explicitly chose "build reconnect now" over deferral). Validate-then-commit: connect the new set first, swap on success, keep the old connections on failure with the error on the row. Startup connect stays fatal.
  7. **Bools keep the instant ⏎ toggle.** Popups are for 3+ options.
  8. **The ` *` marker = "changed through the settings surface this session"** — in-pane edit, `$EDITOR` round-trip, or reset (a reset row shows `default *`). Cleared only by relaunch. It replaces every "(next launch)" note.
  9. **The description header wraps to at most 2 lines** with `…` truncation, and stays a fixed-height region so the list never jumps.
  10. **The multiline editor commits with ctrl+s, discards with esc**; ⏎ inserts a newline. Hint line says so.
  - **Author-derived bindings** (plan author, 2026-08-06 — derived from the calls above and existing contracts, recorded here so their authority survives the session):
    - A. Per-edit ordering is **validate → persist → apply**. An apply failure after a successful persist keeps the persisted value and shows a row failure note `saved — live apply failed: <error>`.
    - B. External editor resolution: `$VISUAL`, then `$EDITOR`, then `vi` (`notepad` on Windows). A `+<line>` argument is passed only when the editor basename is one of `vi, vim, nvim, nano, pico, emacs, micro, hx, kak`; otherwise the file opens without a line jump.
    - C. The `$EDITOR` jump is offered only while no run is in flight; during a run, ⏎ on a nested-structure row shows a note asking to wait. (Sidesteps apply-queueing and suspending the TUI mid-stream. In-pane edits stay allowed mid-run — anytime-safe keys apply at once, rebind-riding keys defer via the existing pending-rebind machinery.)
    - D. Changing `present.port`/`present.host` after the docs listener is bound closes the listener; it rebinds lazily at the next presentation. Previously issued URLs die — inherent to a port change.
    - E. The hint line keeps `⌫ reset` alongside the mockup's `↑/↓ select · ⏎ edit · esc close` (the mockup hint is a minimum, not an exhaustion).
    - F. New engine mutators land in the two existing classes only — anytime-safe (the `SetMode` shape, `agent.go:34`) or idle-only validate-then-commit (the `Rebind` shape, `agent.go:29`). Tool-registry changes ride ONE generic idle-only `SwapTools` mutator.
    - G. GlobalOnly confinement keys are never live-applied or edited by the pane, even when an `$EDITOR` round-trip changed them in the file (ADR 0012); the reload skips them silently.
- **Standing requirements:**
  - skills: coding-standards
  - Any authorized deviation from item text lands as a dated NOTES line under the item.
  - `make check` before every commit.
  - The Bubble Tea `Model` is value-copied on every `Update` (ADR 0011, `internal/tui/doc.go`): no `strings.Builder` or other no-copy type held by value anywhere it reaches. Binds items 10, 11, 14 especially (`TestModelNoBuilderByValue` is the guard).
  - Tests never hit real networks or the user's filesystem; live-LLM tests stay gated behind `APOGEE_LIVE_ENDPOINT`.
  - No version identifier changes (VERSION, CHANGELOG release heading, tags) — see the closing note.
- **Out of scope:**
  - Making `confine-to-workspace` / `unconfined-hosts` pane-editable (ADR 0012; they keep the `use /confine` pointer).
  - Batch-apply-on-close (foreclosed by ratified call 1).
  - In-pane form/sub-screen editors for the six nested structures (foreclosed by ratified call 5).
  - Changing startup precedence, the config home, the seeded template's content, or the splice writer's atomic-write contract.
  - New configuration keys.

## 1. ADR 0037 — every settings edit applies to the running session — ✅ DONE (2026-08-06)

**What:** Write `docs/adr/0037-every-settings-edit-applies-to-the-running-session.md` recording the ratified design calls above as the settled policy: apply-on-⏎-commit; boundary semantics for prompt-prefix keys; pane-edit-outranks-override for the session; the `server` row as a live switch; hybrid structured editing with the `$EDITOR` jump; live MCP reconnect (validate-then-commit, keep-old-on-failure, startup stays fatal); the ` *` session-edit marker replacing "(next launch)"; the two engine-mutator classes as the only homes for new seams (ADR 0031 door-keeping restated). State explicitly: **supersedes ADR 0035 decision 9** (and widens decision 3's v1 simple-keys scope); ADR 0035's persistence contract (one key per deliberate edit, comment-preserving splice, atomic write) remains in force. Follow the house ADR format of `docs/adr/0035-*.md`/`0036-*.md`. Add the "superseded in part by 0037" cross-reference line to ADR 0035's header.

**Tests:** none (docs only).

**Acceptance:**
- `ls docs/adr/ | grep 0037` finds the file.
- `grep -l "0037" docs/adr/0035-*.md` shows the back-reference.
- `make check` passes.

**Commit:** `docs(adr): 0037 — every settings edit applies to the running session`

## 2. Anytime-safe engine setters: bypass, compaction, context files — ✅ DONE (2026-08-06)

NOTES (2026-08-06): the three setters swap their OWN live field behind their own mutex (the literal `SetMode` shape ADR 0037 L70 names — one mutex, one field) instead of writing `a.cfg.*` in place; `cfg.Bypass` / `cfg.Context.CompactionEnabled` / `cfg.ContextFiles` stay the construction seeds, and the consumption sites read the new `bypassEnabled()` / `compactionEnabled()` / `contextFileList()` accessors. Writing cfg in place would have raced the unlocked whole-struct copy `newChildAgent` takes (`subagent.go:129`). Consequence, also new: `newChildAgent` now inherits the parent's LIVE Bypass and auto-Compaction gates at spawn, exactly as it already did for mode and confinement (the context-file NAMES are deliberately not propagated — the child copies the parent's cached content verbatim, since a sub-agent is not a session boundary).

**What:** Add three mutators to `internal/agent` in the exact shape of `SetMode` (`agent.go:250`; anytime-safe class listed at `agent.go:34`), mutex-guarded like it:

- `SetBypass(enabled bool)` — flips `a.cfg.Bypass`. Consumption is already per hook fire (`skipUnderBypass`, `hookrun.go:44`, via `selfreg.go:290`), so the flip takes effect from the next hook evaluation.
- `SetCompactionEnabled(enabled bool)` — flips `a.cfg.Context.CompactionEnabled`. Consumption already per boundary (`compact.go:158`, `compact.go:255`).
- `SetContextFiles(enable bool, names []string)` — replaces `a.cfg.ContextFiles`. It does NOT trigger a reload: `reloadContextFiles` (`contextfiles.go:135`) keeps firing only at its existing boundaries (`construct.go:105`, ClearContext `agent.go:324`, RestoreSession `agent.go:355`), which now read the updated config. This is ratified call 2 — the per-session freeze is deliberate.

Godoc comments on each (start with the symbol name); update the mutator-class listing in the package doc. Doc comments state the consumption boundary ("takes effect at the next hook fire" / "at the next context clear or session start").

**Tests** (in `internal/agent`, table-driven where several cases share a shape, `t.Parallel()` where no shared state): SetBypass flips the hook-skip decision between two hook evaluations; SetCompactionEnabled flips the auto-compact gate; SetContextFiles' new names are picked up by the next ClearContext-driven reload and NOT before it.

**Acceptance:**
- `go test ./internal/agent/` passes.
- `make check` passes.

**Commit:** `feat(agent): anytime-safe setters for bypass, compaction and context files`

## 3. The ApplySetting seam and the live-apply dispatcher — ✅ DONE (2026-08-06)

Depends on item 2.

NOTES (2026-08-06): three deviations from the item's literal text, each to avoid leaving something half-wired. (a) The seam's `note` return is stored on `settingEdit` AND rendered NOW — `settingsNote` gains a `· <note>` branch ahead of the applied-live one — rather than stored for item 8: a value the pane took and never showed would be dead until then. The wording, the ` *` marker and the death of the pending-arrow branches stay item 8's. (b) `*lateEngine` gained `SetBypass` / `SetCompactionEnabled` / `SetContextFiles` (the new `settingsEngine` interface the dispatcher takes, so it is testable against a spy). They pass through to the Agent and are REMEMBERED while unbound — the `SetMode`/`SetConfineToWorkspace` posture, nil pointer = never moved — because `/settings` is open before a server is bound (ADR 0036 decision 3) and an edit that persisted must not be the only half that happened. (c) The `settingsEnumRow` test fixture's vocabulary became the REAL spinner styles (`snake, glitter, classic`): `ui.spinner` now applies in the renderer, which refuses a value `ParseSpinnerStyle` does not know. Also rewritten alongside `model.go`'s stale "process-constant" comment: the same claim in `Options.HideScrollbar`'s doc (`tui.go`), which the live apply falsifies. Known gap, item 13's to close: a `context-files:` block that started OFF resolves to no names, so switching it on live installs none until the names themselves are editable.

**What:** Generalize the pane's live apply from the hard-coded `mode` case to every editable key, per ratified call 1 and binding A.

- `internal/tui/tui.go`: `Options` gains `ApplySetting func(key, value string) (note string, err error)` beside `WriteSetting` (L283–318). `note` is "" for a fully-live apply or a short boundary note (e.g. `applies at next clear`) the pane renders per item 8. Nil seam ⇒ no live apply (bench/headless drivers stay valid — ADR 0031).
- `internal/tui/settings.go`: after a successful persist, `settingsApplied` (L549) routes by key class instead of the `settingsModeKey` special case (L564, which dissolves):
  - TUI-local keys apply in place: `auto-title` → `m.opts.AutoTitle` (read at `autotitle.go:96`); `ui.show-scrollbar` → `m.opts.HideScrollbar` + `m.layout()` (read at `model.go:2753`, `model.go:3060`; rewrite the stale "process-constant" comment at `model.go:2751`); `ui.spinner`/`ui.spinner-color` → the spinner state built at `model.go:280` (style read per frame, `spinner.go:327-339`); `cursor-shape` → re-run `steadyCursor` on the input (`prompteditor.go:142`, idempotent).
  - Every other key → `m.opts.ApplySetting(key, value)`. `mode` additionally keeps its TUI mirror `m.opts.Mode` in step (Shift+Tab parity, `model.go:945-953`).
  - Apply ordering per binding A; an apply error becomes the row failure note `saved — live apply failed: <error>` via the existing `settingFailure` machinery. The reset path (`settingsReset`, L490) applies the default value the same way after `ResetSetting` succeeds.
  - Overridden keys (Source = env/flag) are NOT skipped — ratified call 3; the note wording is item 8's.
- `cmd/apogee/wire.go`: build the dispatcher closure and wire it at the seam block (`wire.go:598-618`). This item implements the entries: `mode` → `eng.SetMode`; `bypass` → `eng.SetBypass`; `auto-compact` → `eng.SetCompactionEnabled`; `context-files.enable` → `eng.SetContextFiles(newEnable, currentNames)` returning note `applies at next clear`. Unknown keys return a descriptive error (later items extend the dispatcher; the error text names the key).

**Tests:** renegotiate `TestSettingsPaneModeEditAppliesLiveAndMarksNothing` (`settings_test.go:527`) for the dispatcher route. New pane tests with a spy `ApplySetting`: a bypass toggle persists then applies; an apply error shows the `saved — live apply failed` note and keeps the persisted value in the row; reset applies the default; `auto-title` and `ui.show-scrollbar` flips take effect without touching the seam. `cmd/apogee` test: the dispatcher maps the four keys to the right engine calls (spy engine seam or the harness idiom already used around `wire.go`).

**Acceptance:**
- `go test ./internal/tui/ ./cmd/apogee/` passes.
- `grep -n "settingsModeKey" internal/tui/settings.go` finds nothing.
- `make check` passes.

**Commit:** `feat(settings): live-apply dispatcher — every editable key applies on commit`

## 4. Mutable settings holder and the rebind-riding applies — ✅ DONE (2026-08-06)

Depends on item 3.

NOTES (2026-08-06): four deviations from the item's literal text. (a) `liveSettings` owns two values beyond the four enumerated: the `system-prompt-*` block and the LAST OBSERVED context window. The prompt block is forced by the item's own second bullet — `rebindSpecFor` re-resolves the prompt per rebind out of the options snapshot (`wire.go:791`), so a re-read block reaches the engine only if the holder owns it; the observed window is what a pin CLEARED to `0` must bind, or "discover-live" would *unbind* the window (the next beat reports no change and never re-drives). The overlay lives in one method, `rebindInputs`, which every re-resolution now opens with. (b) The rebind re-drive is SYNCHRONOUS and root-side: the dispatcher calls the same closure `Options.Rebind` is wired to, on the Update goroutine the pane's keypress arrives on. The TUI's `pendingRebind` deferral the item names is reachable only from a heartbeat observation, and routing a settings edit into it needs a renderer seam this item's file list does not open — so mid-run the engine's own idle-only refusal surfaces as the row's `saved — live apply failed` note (binding A) and a re-committed edit retries. Consequence, worth knowing: the TUI's `m.opts.ContextWindow` mirror (the gauge's denominator, `model.go:2459`) refreshes at the next TUI-driven rebind rather than on the keypress — the ENGINE is on the new pin immediately. (c) `recordServerChoice` was pointed at the holder's server list beside the two switch closures the item names: three closures resolving names against two lists is how they drift, and today the value is identical. (d) `applySettingFor` now takes one `settingsApplier` struct instead of growing a parameter per key class; the function name and the `ApplySetting` seam signature are unchanged. `sessionMover` and `scheduleWiring` hold the holder in place of their captured `pinnedWindow`/`manualIDs`.

**What:** Un-freeze the composition root's startup snapshot so rebind-derived values can change mid-session.

- `cmd/apogee/wire.go`: introduce one mutex-guarded holder (suggested `liveSettings`) owning the values today captured by value in closures: the pinned context window (`pinnedWindow`, captured at `wire.go:355`, `:393`, `:451`), the upstream choices (`choices`/`upstreamChoices`, `wire.go:350`, projected at `wire.go:546`), the manual mechanism ids (`manualIDs`, `wire.go:269`) and the validated-set resolution (`wire.go:282`, `:707`). Point those closures at the holder. No behavior change yet for mechanisms/servers — this item pointerizes; items 12 and 16 consume.
- Dispatcher entries (extending item 3's closure):
  - `context-window` → set the holder's pin, then drive the existing rebind path (`rebindSpecFor` reads the holder → `Options.Rebind` → the TUI's `applyRebind`/`pendingRebind` deferral, `model.go:2237-2243`, `:2481`; `applyRebind` already refreshes `m.opts.ContextWindow` at `model.go:2459`). `0` keeps meaning discover-live (ADR 0024). No note — it lands at the next idle moment.
  - `system-prompt-text`, `system-prompt-file`, `system-prompt-models` → re-read the config file, re-run the startup `resolveSystemPrompt` path (template re-resolution already happens per rebind at `wire.go:702`; the prompt is rendered per request at `loop.go:594`/`:636`), then drive the same rebind. No note. These keys get their edit UIs in items 13, 14, 16 — the dispatcher entries land now, keyed by path, so those items add only UI.

**Tests:** `cmd/apogee` harness tests: a `context-window` apply re-drives rebind with the new pin (spy rebind closure); a `system-prompt-text` apply re-resolves from a fixture config in `t.TempDir()` and the resulting rebind spec carries the new prompt. No real network.

**Acceptance:**
- `go test ./cmd/apogee/` passes.
- `go test ./internal/tui/` passes.
- `make check` passes.

**Commit:** `feat(settings): context-window and system-prompt edits ride the rebind path live`

## 5. Engine SwapTools — the one idle-only tool-registry mutator — ✅ DONE (2026-08-06)

NOTES (2026-08-06): two deviations from the item's literal text. (a) The mid-run refusal WRAPS `domain.ErrInputPending` with its reason (`…: the tool set can only be swapped between runs`) instead of returning it bare as `Rebind` does — `errors.Is` still matches the idle-only class, but this refusal is the only one in that class that is user-facing (items 6/17 render it inside the row's `saved — live apply failed: <error>` note, where the bare "cannot submit input mid-exchange" would name something the user never did). (b) The well-formedness half of the validation (nil entry, empty name, duplicate name) is DEFENCE ONLY and stays untested: `ToolRegistry`'s fields are unexported and its own `Register`/`Subset` already refuse all three, so no caller outside `internal/domain` can build a registry that trips it — the reachable validation is the nil-registry refusal, which is tested. Also worth knowing: `cfg.Tools` stays the immutable construction seed (only `resolveTools` reads it) — the swap replaces `a.tools` alone, and `newChildAgent` overrides `childCfg.Tools` from the live registry, so nothing ever reads the stale seed.

**What:** `internal/agent`: add `SwapTools` in the idle-only validate-then-commit class (the `Rebind` shape, `agent.go:29`): it takes a fully built tool registry, validates (non-nil, well-formed; refuses while a run is in flight, same refusal idiom as `Rebind`), and atomically replaces `a.tools`. Sub-agents already build from the parent's live registry at spawn (the `subagent.go:132` inheritance idiom) — verify and state in the doc comment that a spawned-after-swap sub-agent sees the new registry. This is the single door for every tool-set change: web-search enable/disable (item 6), MCP reconnect (items 15/17). Binding F: no second registry-mutation path may appear later.

**Tests:** swap while idle succeeds and the next dispatch resolves tools from the new registry; swap during an in-flight run is refused with a descriptive error; a sub-agent spawned after the swap inherits the new registry.

**Acceptance:**
- `go test ./internal/agent/` passes.
- `make check` passes.

**Commit:** `feat(agent): SwapTools — idle-only validate-then-commit tool registry swap`

## 6. Live applies: use-project-skills and web-search-endpoint

Depends on items 3 and 5.

**What:**

- `internal/skills`: `Provider` gains `SetSources(Sources)` (mutex-guarded; `provider.go:35` holds `src`, `Reload` at `provider.go:45` re-scans the captured value today). Dispatcher entry `use-project-skills`: rebuild the `Sources` literal exactly as `wire.go:100-104` does with the new flag, `SetSources`, then `Reload`. The shared `*Provider` propagates to the loop and the `/` menu for free.
- `internal/tools`: `WebSearch` gains a mutex-guarded `SetEndpoint(string)` (endpoint frozen at construction today, `web_search.go:64`, read per `Execute` at `web_search.go:130`; the registry holds the pointer, `registry.go:106`). Dispatcher entry `web-search-endpoint`: when the tool is already registered, `SetEndpoint`; when it was absent at startup (empty endpoint) and the new value is non-empty — or present and the new value empty — rebuild the host tool set (the `hostTools` path, `construct.go:218` / `wire.go:774`, plus the already-connected MCP tools folded per `wire.go:250-252`) and `eng.SwapTools`. If the engine is busy the SwapTools refusal surfaces as the row's apply error (binding A) — the persisted value stands and a retry ⏎ applies it.

**Tests:** skills — SetSources with the flag off drops project skills from the next Reload's set (in-memory dirs via `t.TempDir()`); tools — SetEndpoint changes the URL the next Execute hits (`httptest.NewServer`); cmd — the dispatcher chooses SetEndpoint vs SwapTools per presence/absence (spy engine).

**Acceptance:**
- `go test ./internal/skills/ ./internal/tools/ ./cmd/apogee/` passes.
- `make check` passes.

**Commit:** `feat(settings): skills flag and web-search endpoint apply live`

## 7. Live applies: llama-launcher and the presentation ladder

Depends on item 3.

**What:**

- llama-launcher: hold the launcher config path in an atomically swappable holder instead of the startup capture (`launcherConfigPath`, `launcher.go:117`); wire all four launcher seams unconditionally (`wire.go:463-469` nil-or-wires today) with the enabled/empty check moved inside the facade (`realLauncher`, `launcher.go:82-99` — per-verb config re-read already at `launcher.go:453`, `:487`, so there is no connection to rebuild). Dispatcher entry `llama-launcher`: validate (existing registry validator), swap the path. Setting it from empty enables the verbs live; clearing disables them.
- Presentation: the engine captured the `*uiPresenter` at `wire.go:170`, so replacing the presenter is invisible — instead give the live presenter a mutex-guarded rungs setter (rungs installed at `bridge.go:74`, read at presentation time `presenter.go:111-115`). Dispatcher entries `present.auto-open`, `present.command`, `present.port`, `present.host`: rebuild the ladder as `wire.go:117`/`wire.go:750` does with the new value and set it. Port/host per binding D: if the docs listener is already bound (`present/server.go:186` binds lazily on first Serve), close it so the next presentation rebinds on the new address; note on the row: none (the next present just works).

**Tests:** launcher — a verb invoked after the path swap reads the new file; clearing the path makes verbs report disabled (fixture files, no live server). Presenter — rungs swap changes what the next present call uses; a bound listener is closed and rebinds on the new port (`httptest`-style local listener on port 0).

**Acceptance:**
- `go test ./cmd/apogee/ ./internal/tui/ ./internal/present/` passes.
- `make check` passes.

**Commit:** `feat(settings): llama-launcher path and presentation ladder apply live`

## 8. The ` *` marker; death of "(next launch)"

Depends on items 3, 4, 6, 7.

**What:** Rework the row annotation layer to the ratified semantics (calls 3, 8; binding A):

- `internal/tui/settings.go`: the value cell of any key in the session's edit journal (`settingsPane.edits` — in-pane edits, resets, and later the `$EDITOR` round-trip entries of item 16) renders `<value> *`; a reset row renders `<default> *`. Delete the pending-arrow machinery: `settingsPendingLabel`, `settingsSavedNote`, the `→ … (next launch)` and `saved (next launch)` branches of `settingsNote` (L875-900), and the don't-show-restart-edits rule in `settingsValueCell` (L852). Remaining notes: the boundary note from the apply seam (`· applies at next clear`), the override note for env/flag-sourced rows that were edited — `· <ENV VAR or flag> outranks at next launch` (the edit DID apply live; the note is about the next start), and the failure note from binding A.
- `cmd/apogee/registry.go` + `internal/tui/tui.go` + `cmd/apogee/settingsrows.go`: delete `RestartRequired` / `SettingRow.Restart` and their projections — after this plan no key is restart-gated. Update the registry bijection/projection tests.
- Masked-key note handling (`settings.go:875` masked branch) follows the same ` *` rule; the masked VALUE stays masked.

**Tests:** renegotiate `TestSettingsPaneOverriddenRowSaysTheOverrideStillOutranksIt` (`settings_test.go:560`) to the new note text + live apply; new cases: edited bool shows `false *`; reset shows `default *`; `context-files.enable` edit shows the boundary note; no rendering path emits "(next launch)". Update `TestSettingsRowsProjectRegistryMetadata` (`settingsrows_test.go:299`).

**Acceptance:**
- `go test ./internal/tui/ ./cmd/apogee/` passes.
- `grep -rn "next launch" internal/tui/*.go` shows only the override-note string; `grep -n "settingsPendingLabel\|settingsSavedNote\|RestartRequired" internal/tui/ cmd/apogee/ -r` finds nothing.
- `make check` passes.

**Commit:** `feat(settings): session-edit * marker replaces every (next launch) note`

## 9. Pane chrome: description header, sections, highlight

**What:** The layout spec's visual requirements (`docs/design/settings-screen-layout.md` lines 1–10 and the mockup), in `internal/tui/settings.go` (+ `popup.go` only if the pane title needs it):

- A **Description:** header at the top of the pane: bold label, the selected row's `Desc`, wrapped to at most 2 lines with `…` truncation, fixed-height so the list never jumps (ratified call 9), followed by one blank line. It sits outside the scroll window (`popupRowWindow`, `popup.go:485`, `:959-988`) — adjust the budget call at `settings.go:967` so header + hint are always seated and only the list scrolls.
- The pane title renders bold (the mockup's `**SETTINGS**`).
- Section headers render white/bright, one blank spacer line before each (no double blank after the description's own blank — the first section gets no extra spacer). Section rows come from `settingsDisplayRows` (`settings.go:775-788`).
- The row currently in edit mode (buffer or enum sub-list parent) renders in the highlighted style; theme-sourced, both color schemes.
- Hint line per binding E: `↑/↓ select · ⏎ edit · ⌫ reset · esc close` (edit-state hints unchanged here; item 14 adds the multiline hints).

**Tests:** extend `TestSettingsDisplayRowsInterleaveSectionHeaders` (`settings_test.go:237`) for spacer rows; new paint-level cases: description header shows the selected row's text, truncates at 2 lines, stays fixed-height across selections; edit-mode row carries the highlight style; headers carry the white style; the scroll window never hides the description or hint (short-frame case — extend `TestSettingsPaneClaimsTheWholeTranscriptBudget`, `settings_test.go:289`).

**Acceptance:**
- `go test ./internal/tui/` passes.
- `make check` passes.

**Commit:** `feat(tui): settings pane chrome — description header, white sections, spacers, edit highlight`

## 10. Single-line value editing with real cursor movement

**What:** Replace the append/pop-only edit buffer (`settingsBufferKey`, `settings.go:398-420`; `buf` + `settingsCaret`) with a textarea-backed single-line editor, per spec requirement 7 (keyboard half; mouse is item 11):

- Extract a reusable editor from the prompt machinery: a non-embedded instance of the `promptEditor` wrapping (`prompteditor.go:33`, built by `newPromptEditor`, L131) configured single-line — held BY VALUE on `settingsPane` (the textarea is copy-safe by the same argument as the embedded prompt instance; `TestModelNoBuilderByValue` guards). If reuse forces an extraction, a small `internal/tui/lineeditor.go` wrapping `textarea.Model` is acceptable — implementer's choice, NOTES line either way.
- Full cursor support in the value: ←/→, home/end, word jumps, insertion/deletion mid-string — whatever the prompt editor already gives. ⏎ commits (single-line: enter never inserts a newline), esc cancels, exactly as today's buffer state machine (`settingsValueBuffer` kind) — only the editing inside the state improves.
- `settingBufferCells` (`settings.go:799`) renders the editor's view (real caret, highlighted row style from item 9) instead of `buf + "▏"`. Seed with the persisted value, caret at end.
- The refused-value-kept-for-correction behavior stays (`TestSettingsPaneBufferKeepsARefusedValueForCorrection`, `settings_test.go:810`).

**Tests:** renegotiate `TestSettingsPaneBufferEditsAStringAndPersistsIt` (`settings_test.go:723`) and the refused-value test; new cases: insert mid-string via cursor keys, home/end, delete-back mid-string, commit and cancel round-trips.

**Acceptance:**
- `go test ./internal/tui/ -run 'TestSettings'` passes; `go test ./internal/tui/` passes (Builder guard included).
- `make check` passes.

**Commit:** `feat(tui): settings value editing gets full cursor movement`

## 11. Mouse support in the settings pane

Depends on item 10.

**What:** Extend the mouse layer (`internal/tui/mouse.go` — today input+transcript only) to the settings pane, per spec requirement 7 ("incl. usage of mouse"):

- A settings-pane content rect (the `inputContentRect` idiom, `mouse.go:160`): click on a list row selects it; click on the row in edit mode seats the caret at the clicked cell (reuse `cellToRuneOffset`/`caretOffset`, `mouse.go:262`, `:275`); drag inside the edit row selects text (the `promptSel` machinery, `mouse.go:69`, applied to the item-10 editor); wheel moves the list selection (the scroll window follows, as it does for keys).
- Selection rendering in the edit row via the `highlightInput` idiom (`mouse.go:527`) against the item-10 editor.

**Tests:** mirror the existing input-mouse tests: click selects the row under the pointer; click in the edit buffer seats the caret at the right rune (including a double-width-rune case); drag produces the selection; wheel moves the selection.

**Acceptance:**
- `go test ./internal/tui/` passes.
- `make check` passes.

**Commit:** `feat(tui): mouse selection, caret seating and wheel in the settings pane`

## 12. The server row: selection popup driving the live switch

Depends on items 3 and 4.

**What:** Ratified call 4.

- `internal/tui/tui.go`: `Options.Servers` becomes (or is joined by) a provider that reflects the CURRENT choices — sourced from item 4's holder (`wire.go:546` projects a startup snapshot today), so an `$EDITOR` edit of `servers:` (item 16) refreshes the popup.
- `internal/tui/settings.go`: ⏎ on `server` opens the existing enum sub-list (`settingsEnumList` / `renderSettingsEnum`, `settings.go:997`) fed with the server names + `(current)` marker — never the text buffer. Selecting calls `m.opts.SwitchServer(name)` (the `/server` seam, `wire.go:405-432`: mover + heartbeat rebind + `RecordServerChoice`) instead of `WriteSetting` — the recorded choice IS the persist, per ADR 0036. On acceptance, journal the edit (`server` → name) so the row shows `<name> *`; a synchronous refusal from the seam becomes the row failure note. Async switch progress surfaces exactly as `/server` does (the `foldServerSwitch` transcript fold) — the pane adds nothing.
- `cmd/apogee/registry.go` / `settingsrows.go`: the `server` row's projection declares the popup affordance (enum-like with dynamic values); the registry bijection test keeps passing.

**Tests:** the popup lists the configured servers with `(current)`; selecting drives a spy `SwitchServer` and journals the `*`; a seam refusal shows the failure note and journals nothing; the popup reflects a changed provider list on reopen.

**Acceptance:**
- `go test ./internal/tui/ ./cmd/apogee/` passes.
- `make check` passes.

**Commit:** `feat(settings): server row opens a picker and performs the live switch`

## 13. Promote system-prompt-file and context-files.names to in-pane rows

Depends on items 2, 3, 4 and 10.

**What:** The two simple shapes of ratified call 5.

- `cmd/apogee/registry.go`: `system-prompt-file` becomes `Editable: true`, kind string, validator: empty is invalid for a SET (clearing goes through reset/⌫, which splice-deletes the line); a non-empty value must name an existing readable file (pure validator over the expanded path). Apply rides item 4's dispatcher entry.
- `context-files.names` becomes editable with a new registry kind `kindStringList`: the pane edits it as a comma-separated single-line buffer (item 10's editor); parse = split on commas, trim, drop empties. Apply → `eng.SetContextFiles(currentEnable, newNames)` with the `applies at next clear` note.
- `cmd/apogee/configwrite.go`: extend the splice writer to render a string list as a single-line flow sequence (`context-files:\n  names: [AGENTS.md, CLAUDE.md]` — still within `scalarPathDepth = 2`): `renderSettingValue`/`spliceScalarSet`/`spliceScalarDelete` learn the list kind; `verifiedSplice`'s whole-file-compare guard covers it unchanged.
- `cmd/apogee/settingsrows.go`: value formatters — the list renders as today's `[AGENTS.md]` display; `system-prompt-file` shows the path or `none`.

**Tests:** splice round-trip for the list against the seeded template (comments preserved, `verifiedSplice` green) — set, re-set, delete; registry bijection + parse-site tests updated for the new kind; pane e2e: edit names → persists flow list, seam called with parsed names, boundary note shown; `system-prompt-file` validator rejects a missing file and the row keeps the refused value for correction.

**Acceptance:**
- `go test ./cmd/apogee/ ./internal/tui/` passes.
- `make check` passes.

**Commit:** `feat(settings): system-prompt-file and context-files.names edit in-pane`

## 14. The in-pane multiline editor for system-prompt-text

Depends on items 4, 9 and 10.

**What:** Ratified calls 5 and 10.

- `internal/tui/settings.go`: a new pane state (suggested `settingsTextEditor`) entered by ⏎ on `system-prompt-text`: the list body is replaced (within the same pane frame, description header retained) by item 10's editor in multiline mode. ⏎ inserts a newline; **ctrl+s saves, esc discards**; hint line: `ctrl+s save · esc discard`. Seed with the current persisted text; the value cell keeps its `N lines` summary (+ ` *` after an edit).
- `cmd/apogee/configwrite.go`: block-scalar splice — extend the writer so a multiline value renders as `system-prompt-text: |` + indented lines, replacing the existing block (including the template's active default) using the parsed node positions; delete restores absence. `verifiedSplice` covers the round-trip. This is the writer's hardest case — the seeded template's `system-prompt-text` is its one active key; test against it verbatim.
- `cmd/apogee/registry.go`: `system-prompt-text` becomes editable with a new kind (suggested `kindText`); save → `WriteSetting` → item 4's dispatcher (rebind ride, no note).

**Tests:** writer — multiline set over the template's active block preserves every surrounding comment; set → read-back equality; delete; a value containing blank lines and trailing spaces survives. Pane — enter/edit/ctrl+s persists and applies (spy seams); esc discards; the Builder guard still passes.

**Acceptance:**
- `go test ./cmd/apogee/ ./internal/tui/` passes.
- `make check` passes.

**Commit:** `feat(settings): system-prompt-text edits in-pane in a multiline editor`

## 15. Model-profile live apply: SetProfile

Depends on item 5 (class precedent only — no code dependency).

**What:** `internal/agent`: `SetProfile(profile ProfileConfig) error` in the idle-only validate-then-commit class: validate by running `processing.ParserFor` for the new profile (the `construct.go:69` path); on success swap `a.textParser`, `a.stripper` and `a.cfg.Profile` atomically at idle; refuse mid-run like `Rebind`. The emit half needs nothing — `InstructionsFor(a.cfg.Profile, menu)` is read per request (`agent/wire.go:55`). `Rebind`'s deliberate profile exclusion (`rebind.go:52`, `wire.go:685`) STAYS — `SetProfile` is the separate, explicit door; state that in both doc comments. The signature takes the resolved profile value (the root resolves from file, as at startup) — the engine never reads config files (ADR 0031 wire-silence).

**Tests:** swap at idle changes the parser the next response runs through (fixture profiles); refused mid-run; an invalid profile leaves the old parser in place (validate-then-commit).

**Acceptance:**
- `go test ./internal/agent/` passes.
- `make check` passes.

**Commit:** `feat(agent): SetProfile — idle-only live swap of the model profile`

## 16. The $EDITOR jump: suspend, reload, diff, apply

Depends on items 4, 8 and 12.

**What:** Ratified call 5's second half, bindings B, C, G. The six nested-structure rows (`servers`, `system-prompt-models`, `mcp-servers`, `mechanisms`, `validated-sets`, `model-profile`) get ⏎ → external edit. This item builds the whole mechanism and wires the keys with already-existing seams; item 17 wires the last two.

- Seams (`internal/tui/tui.go`): `Options.ExternalEditSpec func(key string) (argv []string, err error)` — root-side resolves the config path (`configFilePath`, `config.go:1846`), the key's line via the writer's node-position parse, and the editor per binding B. And `Options.ReloadConfig func() ([]AppliedSetting, error)` — root-side re-reads (`loadFileConfig`, `config.go:1275`), on parse/validation failure returns the error WITHOUT applying (the user's file is never rewritten), otherwise diffs old vs new `fileConfig` per registry key (string projection, the `settingsrows` formatters), applies each changed editable-or-structured key through the dispatcher, skips GlobalOnly keys silently (binding G), updates item 4's holder (servers list, mechanisms/validated-set inputs — mechanisms/validated-sets re-derive `manualIDs` + the validated-set resolution and ride the existing rebind, whose registry rebuild + three gates at `rebind.go:111-127` is the catalogued path), and returns per-key results (key, new display value, note, error).
- `internal/tui/settings.go`: ⏎ on a nested row while no run is in flight (binding C — otherwise a `wait for the current run to finish` note) → `tea.ExecProcess` with the argv; on the exec-finished message call `ReloadConfig`; journal every returned key (` *`), render per-key notes/errors on their rows; a reload error lands as the failure note on the row that launched the edit. Rows' displayed values refresh from the reload results (the pane still never re-reads the file itself).
- `cmd/apogee/settingsrows.go`: the pointer cell for these rows becomes `⏎ opens $EDITOR` (constant replacing `pointerConfigFile`, which dies — `editPointer`, `settingsrows.go:211`); update `TestSettingsRowsPointReadOnlyKeysAtTheirEditor`.

**Tests:** root — editor resolution chain and the `+line` allowlist (env via the existing getenv-injection idiom); ReloadConfig applies a changed `servers` list to the holder (popup provider sees it), a changed `system-prompt-models`/`mechanisms`/`validated-sets` drives the rebind (spy), a parse error applies nothing and reports, GlobalOnly changes are skipped. TUI — ⏎ mid-run shows the wait note; the post-exec flow journals `*` and shows per-key notes (fake exec via injected cmd runner or message-level test, no real editor).

**Acceptance:**
- `go test ./cmd/apogee/ ./internal/tui/` passes.
- `grep -rn "edit in config.yaml" internal/ cmd/` finds nothing.
- `make check` passes.

**Commit:** `feat(settings): nested structures edit via $EDITOR with reload-and-apply`

## 17. MCP reconnect and model-profile through the reload path

Depends on items 5, 15 and 16.

**What:** The last two dispatcher entries, per ratified call 6 and binding F.

- `internal/mcp`: session teardown — a `Close` on the client/session if absent (`client.go:82` opens; nothing closes today).
- `cmd/apogee`: the reload path's `mcp-servers` entry: connect the NEW server set first (`mcp.Connect`, the `wire.go:245` path, all-or-nothing as at startup but returning the error instead of dying); on success rebuild the full tool registry (host tools + new folded MCP tools, mirroring `wire.go:250-252`/`construct.go:218`) → `eng.SwapTools` → close the old sessions; on any failure keep the old sessions and old registry and return the error as the row note (`reconnect failed: <error> — previous connections kept`). Startup connect failure stays fatal (unchanged). A busy-engine SwapTools refusal is a failure of the same shape (retryable by re-entering the editor or re-committing).
- `model-profile` entry: resolve the new profile from the re-read config (the startup resolution path) → `eng.SetProfile`; failure → row note, old profile keeps running.

**Tests:** with an in-process fake MCP server (the transport `internal/mcp`'s own tests use): a changed server set swaps the registry and the old session is closed; a failing new set keeps the old sessions serving and reports; a busy engine reports and keeps state. Model-profile: a valid change swaps (spy/fixture), an invalid one reports and keeps the old parser.

**Acceptance:**
- `go test ./internal/mcp/ ./cmd/apogee/ ./internal/agent/` passes.
- `make check` passes.

**Commit:** `feat(settings): live MCP reconnect and model-profile apply on config reload`

## 18. Docs sweep: README, layout.md, CONTEXT.md

Depends on items 1–17.

**What:** Make the prose match the shipped behavior — each doc has exactly this one owning item:

- `README.md` L263–300 (settings section): the live-apply story, the ` *` marker, the popup rows, the multiline editor, the `$EDITOR` jump, MCP reconnect; delete the "(next launch)" promise.
- `layout.md` L215–247 and L1134–1148: the pane grammar — description header (2-line region), spacer+white section headers, highlighted edit row, the editor states (single-line, multiline, enum popup), the `⏎ opens $EDITOR` affordance, hint lines.
- `CONTEXT.md` "Settings surface" (L406–430): update the term's definition — live apply on commit, the session edit journal (` *`), the `$EDITOR` round-trip; keep the avoid-saying list coherent.
- Check `ISSUES.md`/`TODO.md` for entries this plan resolves (e.g. any settings-editing deferrals) and remove exactly those.

**Tests:** none (docs only) — but re-run the full suite as the sweep's sanity gate.

**Acceptance:**
- `grep -n "next launch" README.md` finds nothing.
- `go test ./...` passes; `make check` passes.

**Commit:** `docs: settings surface docs match live-apply behavior`

---

**Suggested version bump:** this plan is a user-visible feature wave (live apply everywhere, new editors, MCP reconnect) — a MINOR bump to **v0.12.0** with a CHANGELOG entry would be warranted once it lands. No item performs the bump; whether and when is the owner's call.
