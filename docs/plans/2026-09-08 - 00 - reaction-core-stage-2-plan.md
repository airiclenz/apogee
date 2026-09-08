# Reaction core — stage 2: `reactions:` in the global file

**Goal:** Ship the user-origin `reactions:` surface in `~/.apogee/config.yaml` over the stage-1 core: `internal/hooks` becomes `internal/reactions` over `domain.Reaction`, the file migrates itself, one generation swap replaces three live-swap idioms, five seam-closing notices become observable, and `validated-sets:` and `mechanisms:` leave the tree. Behaviour of the seven Floor guards is unchanged and proved by the stage-1 identity fixtures.

**Date:** 2026-09-08 · **Status:** unexecuted · **Sized for:** ~200k-context host · **Base:** `d84dd988` · **Bead:** `apogee-pjx`

**Sources:** `docs/adr/0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md` (decisions 1–13 + amendment A1–A9 of 2026-09-08 — binding where the two differ) · `docs/design/reaction-core-greenfield.md` §2, §9.1, §9.4 Plan B · `docs/adr/0036-*.md` D9 (the migration idiom) · `docs/adr/0073-*.md` D6, D8, D9 · `docs/adr/0075-*.md` D10 · `docs/adr/0016-*.md` amendment 2026-09-08 · beads `apogee-pjx`, `apogee-jwf`.

**Ratified design calls** (owner, 2026-09-08 unless noted):
- **A1–A9:** as recorded in the ADR 0076 amendment; not restated here, not re-askable.
- **Seam-closing notice payload:** full working value, serialized only when subscribed — the event carries a read-only reference valid for the duration of `Emit`; the Runner encodes it into the script/webhook payload inside `Emit` only when an entry subscribes; never copied or logged otherwise.
- **Fold shape:** the `hooks:` block is re-rendered from its parsed entries (the `renderServerEntry` idiom) and spliced over the old block's line range; comments inside the old block are lost, the backup keeps them, the note says so.
- **`Hook event` term:** retired into Moment/notice; CONTEXT.md's Moment entry carries the notice vocabulary.
- **Generation shape (writer, from A8):** `domain.Generation{Floor FloorConfig; Bypass bool; Observe []Reaction}`; the agent holds Floor+Bypass under one lock, the Runner holds Observe; one Driver-side apply feeds both — the Runner swaps only when Observe differs. Construction-time `Config.Bypass`/`Config.Floor`/`Config.Reactions` stay.
- **Builtin enable set (writer, from A8):** a Floor guard whose boolean is off is absent from the ladder rather than self-skipping at fire time; firing sequence identical (a disabled guard books nothing today).
- **`enabled: false` (writer):** parked entries are dropped at resolve; the `/settings` summary counts armed entries.
- **`on:` naming a seam (writer, from A7):** load error — seams take `advise:`/`gate:`, which are not yet shipped.
- **`internal/mechanisms` (writer, from A6):** deleted whole — the migration strips the key, so the four start-up retired-roll notices become unreachable; its successor table moves into `configmigrate.go` (item 9), which is what ADR 0076 A6 and greenfield §9.1 row 11 ask for.
- **`reactions` row placement (writer, from A3):** after `bypass` in `KeyRegistry`, so it renders in the pane's Reactions section.
- **Old setters (writer):** `SetBypass`/`SetFloor` survive as wrappers over `SetReactions` from item 10 until item 13 deletes them, so every item compiles.

**Regression check (2026-09-08, `d84dd988`):**
- 2, 3, 8, 9, 11, 15, 16: recast — the writer's decision is the item's guard paragraph (15's also folds its report `G:` lines, verified at `d84dd988`).
- 4, 6, 12, 17: guard folded (each `G:` fact verified against `d84dd988`).
- 5: guard folded; the "decided phase is deliberately not an event" decision (`internal/hooks/match.go:113-114`, ADR 0073 §2) is superseded by ADR 0076 A6.
- 10: guard folded; the one-mutex-per-field rule (`internal/agent/agent.go:128-134`, ADR 0037 D2) is superseded by ADR 0076 A8.
- 14, 18, 19, 20: guard folded (writer's decision plus every report `G:` line, each verified at `d84dd988`); 20 yields to ADR 0073's Status-line supersession idiom (:2) and leaves :55/:69 as written.
- round 2 (2026-09-08, `d84dd988`) — 2, 8, 11: guard folded (each `G:` fact verified at `d84dd988`); 8 also carries the writer's test-grep rule.
- round 2 — 9: guard folded (four `G:` lines verified) plus the writer's read-only-file and test-grep sentences; ADR 0076 D11 as pinned at `internal/config/config_test.go:3398-3401` and `registry_test.go:64-66` is superseded by A6.
- round 2 — 15, 16: recast — the writer's decision is the guard; 16's `D:` (`config.go:1477-1483`, `configuration.md:77-78` record D11) is superseded by A6, informational.

**Standing requirements:**
- `skills: coding-standards`
- `go build ./... && go vet ./...` green after every item; `go test ./cmd/apogee/` when the item touches `cmd/apogee`.
- `git diff --quiet d84dd988 -- internal/floor/ ':!internal/floor/*_test.go'` stays clean.
- Never `-update` a golden. `cmd/apogee/testdata/eventlines/identity-*.txt` and `eventlines/*.jsonl` stay byte-identical; `TestKindsAreEighteen` stays (no headless kind is added).
- Authorized deviations land as a dated NOTES line under the item.

**Out of scope:** the repo layer, adoption pin, write deny (stage 2b, `apogee-089`) · `advise:`/`gate:` cells (stage 3, `apogee-rxj`) · headless line kinds for the five notices (`apogee-5cf`) · consolidating `wire_settings.go`'s file re-reads (`apogee-o60`) · the Floor rows' editability (`apogee-tbs`) · `mcp:` handlers (`apogee-03a`) · user shape(view) (`apogee-8za`) · VERSION/CHANGELOG release acts.

## 1. Rename `internal/hooks` to `internal/reactions` — ✅ DONE (2026-09-08)

NOTES (2026-09-08): consequential edit — internal/tui/bridge.go: made necessary by the package rename (the `hooks.Options.Report` qualifier in a comment).

NOTES (2026-09-08): consequential edit — internal/eventjson/writer.go: made necessary by the package rename (the `hooks.Runner` qualifier in a comment).

NOTES (2026-09-08): consequential edit — internal/domain/reaction_test.go: made necessary by the package rename (the `hooks.Event` qualifier in a comment).

NOTES (2026-09-08): the item's `hooks.` sweep was applied to package qualifiers only. Prose that ends a sentence on the word "hooks." and references to the FILE `hooks.go` (internal/config, internal/domain, internal/reactions) are untouched — file names are not renamed by this item and comment wording is item 21's.

NOTES (2026-09-08): gofmt re-sorted the import block in every file whose `internal/hooks` import became `internal/reactions` (r sorts after p, before s); `gofmt -l .` is clean.

**What:** `git mv internal/hooks internal/reactions`; package clause, every import path and every `hooks.` qualifier across the repo become `reactions`; the `"internal/hooks"` literal in `internal/tuitest/leak.go:40` becomes `"internal/reactions"`. No type, field, string or event changes — `Hook`, `APOGEE_HOOK_*`, the payload and the report lines are untouched here. Facade aliases in `apogee.go` keep their names, only their targets move.

**Files:** `internal/reactions/*` (moved), `apogee.go`, `internal/config/hooks.go`, `internal/config/hooks_test.go`, `internal/tuitest/leak.go`, `cmd/apogee/wire_boot.go`, `cmd/apogee/wire.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/wire_settings.go`, `cmd/apogee/schedule.go`, `cmd/apogee/daemonfire.go`, `cmd/apogee/headless.go`, and the tests naming the package (`grep -rl 'internal/hooks' --include=*.go .`).

**Tests:** existing suites unchanged in substance; `grep -rn 'internal/hooks' --include=*.go .` returns nothing.

**Acceptance:**
```
test ! -d internal/hooks && go build ./... && go vet ./...
go test ./internal/reactions/ ./internal/tuitest/ ./internal/config/ ./cmd/apogee/
! grep -rn 'internal/hooks' --include=*.go .
```

**Commit:** `refactor(reactions): rename internal/hooks to internal/reactions`

## 2. Domain: the seam-closing notices and `SeamClosedEvent` — ✅ DONE (2026-09-08)

NOTES (2026-09-08): consequential edit — apogee.go: made necessary by adding domain.SeamClosedEvent — the facade re-exports every Event variant, so the new one joins the alias block beside the notice aliases the item asked for.

NOTES (2026-09-08): consequential edit — internal/reactions/hooks.go: made necessary by `Events()` becoming `domain.Notices()` — the `Event` doc comment said "the five NOTICE Moments" and now says the vocabulary is `domain.Notices()`, with only the original five carrying a named constant here.

NOTES (2026-09-08): consequential edit — internal/tui/fold.go: made necessary by adding domain.SeamClosedEvent — `progressSaveTrigger`'s doc enumerates the variants that answer false and now names the seam closure among them.

NOTES (2026-09-08): added `TestMomentIsNotice` beyond the item's Tests list — `Moment.IsNotice()` is new exported behaviour and the standards require a success and a negative case for it; `TestClosingMapsEverySeamToItsNotice` covers `Closing()` as the item asked.

NOTES (2026-09-08): `docs/manual/hooks.md` still says "one or more of the five events" and `docs/manual/configuration.md:373` still lists five; not touched here — items 18 and 19 own the manual, and neither is done.

NOTES (2026-09-08): `TestEventValuesArePinnedLiterals` (`hooks_test.go`) still pins exactly the five original `reactions` constants, which is correct — this item added no constants to that package, only vocabulary to `domain`.

**What:** Recast at the regression check (2026-09-08). In `internal/domain/reaction.go` add `MomentApprovalDecided = "approval-decided"` and five notices `MomentPreRequestFinished`, `MomentPostResponseFinished`, `MomentPreToolExecFinished`, `MomentPostToolResultFinished`, `MomentHistoryRewriteFinished` (spellings `<seam>-finished`); `allNotices` order becomes `exchange-finished, turn-finished, file-changed, approval-waiting, approval-decided, error, pre-request-finished, post-response-finished, pre-tool-exec-finished, post-tool-result-finished, history-rewrite-finished`. Add `Moment.IsNotice()` and `Moment.Closing() Moment` (seam → its `-finished` notice; zero for a notice). In `internal/domain/events.go` add `SeamClosedEvent{EventBase; Seam Moment; Fired []string; Value any}` with a doc comment binding `Value` to the seam's payload as `fire` received it (`*Request`, `PostResponseMoment`, `*ToolCallEdit`, `ToolResultMoment`, `*Conversation`), read-only, valid only for the duration of `Emit` — a sink must not retain it. The event is sink-only: `internal/eventjson/encode.go`'s default arm already returns `ok=false`; rewrite its doc comment (:63-66) to name the two sink-only variants. `internal/tui`'s `foldCases()` gets an explicit "nothing" row; `apogee.go` gains the new notice aliases. `approval-waiting` is NOT renamed here (item 5).

**Regression guard.** Files gain `internal/reactions/hooks.go` and `internal/reactions/hooks_test.go`; `allEvents`/`Events()` become `domain.Notices()` so `TestEventsAreTheNoticeMoments` holds at this commit; the matcher fires the six new notices only from items 5 and 7 (an entry may subscribe earlier and simply never fires — plan-internal intermediate state).

**Regression guard (round 2).** `TestEventsIsTheWholeVocabularyInOrder` (`hooks_test.go:15-29`, pins five) is re-pinned to the eleven-notice order above (or deleted as redundant with `TestEventsAreTheNoticeMoments`); `eventList()` over `domain.Notices()` makes the `unknown hook event` sentence list eleven names, six that fire nowhere until items 5/7 — an announced-surface change this item states in What — and `TestParseEventErrorTextIsByteIdentical` (`:430-443`) is re-pinned to the eleven-name list.

**Files:** `internal/domain/reaction.go`, `internal/domain/reaction_test.go`, `internal/domain/events.go`, `internal/eventjson/encode.go`, `internal/eventjson/encode_test.go`, `internal/tui/fold.go`, `internal/tui/fold_test.go`, `apogee.go`, `cmd/apogee/headless_test.go`, `internal/reactions/hooks.go`, `internal/reactions/hooks_test.go`.

**Tests:** `TestMomentValuesArePinnedLiterals` and `TestSeamsAndNoticesReportTheVocabularyAsACopy` extended to eleven notices in the order above; `TestClosingMapsEverySeamToItsNotice`; `TestEncodeSkipsTheSeamClosedEvent` beside `TestEncodeSkipsTheWireEvent`; `TestFoldEventCoversEveryEventVariant` passes; `TestHeadlessFormatJSONStreamsEveryEvent` emits a `SeamClosedEvent` and asserts no line and no consumed seq; `TestEventsAreTheNoticeMoments` green over `domain.Notices()`; `TestEventsIsTheWholeVocabularyInOrder` re-pinned to eleven (or deleted); `TestParseEventErrorTextIsByteIdentical` re-pinned to the eleven-name sentence.

**Acceptance:**
```
go test ./internal/domain/ ./internal/eventjson/ ./internal/tui/ -run 'Moment|Closing|Encode|Fold'
go test ./internal/reactions/ -run 'TestEvents|TestParseEventErrorText'
go test ./cmd/apogee/ -run 'TestHeadlessFormatJSONStreamsEveryEvent|TestE2EEventLinesGolden'
```

**Commit:** `feat(domain): five seam-closing notices, approval-decided, and the sink-only SeamClosedEvent`

## 3. Domain: observe handlers, `Reaction.Workspace` and `Generation` — ✅ DONE (2026-09-08)

NOTES (2026-09-08): the item's What says class `observe` requires an argv or webhook handler; its own Regression guard paragraph — the writer's decision, per the plan header's "3: recast" line — inverts that to key on the HANDLER kind. The guard is what shipped: a Go handler is still free to be class observe and keeps the per-seam rule, so the bench's and `internal/agent`'s Go-handler observe reactions keep arming.

NOTES (2026-09-08): two refusal texts the item did not spell are new — an async handler outside class observe (`run: a command or webhook reacts as class "observe", not "advise"`) and an async handler on a spelling in neither half of the vocabulary (`run: reacts to notices; "turn-done" is not one`). The item pinned only the on-a-seam text; both new ones follow the same `%w %q: run: …` shape and are pinned by the new table test.

NOTES (2026-09-08): consequential edit — internal/domain/doc.go: made necessary by the async handlers and Generation joining reaction.go — the package map's line said the file holds "the sealed per-seam Handler funcs", which the second handler kind makes false; it now names both kinds and the Generation. The same sentence in `apogee.go` (:508, the `Handler` alias doc) was corrected in place, that file being one of the item's own.

NOTES (2026-09-08): `CONTEXT.md:1197` still says the Handler seal is "five sealed per-seam Go func types" — not touched here: item 20 owns CONTEXT.md's Reaction entry (which is where `Generation` is scheduled to land) and it is not done.

**What:** Recast at the regression check (2026-09-08). Depends on item 2. In `internal/domain/reaction.go` add two sealed `Handler` variants for the async lane: `ArgvHandler{Argv []string}` and `WebhookHandler{URL string; Headers, HeadersEnv map[string]string}`; their `seam()` returns `""`. Add `Reaction.Workspace string` (scope filter, resolved by the Runner as today). `Validate` gains the observe rules: class `observe` requires an `ArgvHandler` or `WebhookHandler` and every `On` entry `IsNotice()`; a Go handler still requires `On == {Handler.seam()}`; an argv/webhook handler on a seam fails with `ErrInvalidReaction` wrapping `reaction %q: run: reacts to notices; %q is a seam`. Add `Generation{Floor FloorConfig; Bypass bool; Observe []Reaction}` with `Validate()` (every Observe entry validates, ids unique, class observe) — the one value every live swap carries (call: Generation shape). Facade aliases for all four.

**Regression guard.** the new `Validate` rules key on the HANDLER kind, not the class: an `ArgvHandler`/`WebhookHandler` requires `Class == observe` and every `On` entry `IsNotice()`; a Go handler keeps today's `On == {seam()}` rule for every class, so the bench's and the tests' Go-handler observe reactions keep arming.

**Files:** `internal/domain/reaction.go`, `internal/domain/reaction_test.go`, `apogee.go`, `apogee_test.go`.

**Tests:** table cases for each new `Validate` rule with the exact error text, including a Go-handler observe reaction that still validates (the `benchreadiness_test.go:575-583` and `internal/agent/reactions_test.go` shape); `TestGenerationValidateRejectsANonObserveEntry`; `ErrInvalidReaction` sentinel pins in `apogee_test.go` extended to an argv-on-seam case.

**Acceptance:**
```
go test ./internal/domain/ -run 'Validate|Generation|Handler'
go test . -run 'ErrInvalidReaction|Example'
go test ./internal/agent/ -run 'TestReaction|TestSetLive'
```

**Commit:** `feat(domain): argv and webhook observe handlers, Reaction.Workspace and the Generation value`

## 4. The Runner takes `[]domain.Reaction` — ✅ DONE (2026-09-08)

NOTES (2026-09-08): the package's per-entry validator is the FUNCTION `reactions.Validate(domain.Reaction)`, not a method — `domain.Reaction` already carries its own `Validate()` method, which this function runs last, so the package check could not stay a method on the entry.

NOTES (2026-09-08): `TestHookValidateRefusesEachRule` lost its both-actions / neither-action / headers-on-a-command / headers-env-on-a-command rows, which a one-Handler value cannot express; the guard's config rows still pin all four under `hook "notify"`. It gained an empty-argv row, and `TestHookValidateRefusesAReactionTheCoreRejects` proves the package's checks do not shadow `domain.Reaction.Validate`.

NOTES (2026-09-08): `TestLoadFileConfigRefusesAnUnrunnableHook`'s table gained a per-row `key` field — the guard's split between the config layer's `hook "notify"` rows and the Runner's `reaction "notify"` ones (duplicate names, webhook-not-http).

NOTES (2026-09-08): `TestReplaceRefusesAMalformedListAndKeepsRunning` now breaks its entry with an empty `ArgvHandler` instead of setting both actions, which the new value type cannot hold.

NOTES (2026-09-08): `postFailure` takes the timeout rather than the entry — it needed only that one field, and the entry no longer carries it beside the URL.

NOTES (2026-09-08): internal identifiers the Runner uses for its own state (`worker.hook`, `hookSet`, the report-line wording) are left as they are; item 5 owns that wording pass.

NOTES (2026-09-08): consequential edit — internal/reactions/doc.go: made necessary by deleting the Hook struct (the file map's hooks.go line named it).

**What:** Depends on items 1 and 3. Delete `reactions.Hook`; `New`, `Replace`, `Validate`/`ValidateAll`, `SubscribedEvents`, `buildSet`, the executors and the payload stamping take `domain.Reaction` (id → `ID`, events → `On`, command → `ArgvHandler`, webhook+headers → `WebhookHandler`, `Workspace`, `Timeout`). Per-entry validation is `Reaction.Validate` plus the package's own runnable checks (empty argv, bad URL, header names) with today's message texts re-keyed on `reaction %q:`. `internal/config/hooks.go`'s `toHook` builds `domain.Reaction` (origin user, class observe) — the `hooks:` schema itself is untouched until item 8; `Options.Hooks` becomes `[]domain.Reaction`. Facade: `Hook`, `HookOptions`, `HookRunner`, `NewHookRunner` become `RunnerOptions`, `ReactionRunner`, `NewReactionRunner` (the `Hook` alias is deleted; `HookEvent` too — it is `Moment`); `HookPayload` → `ReactionPayload`. Every test that constructs a `Hook{…}` constructs a `domain.Reaction{…}` instead. Event names, env names, payload keys and report lines stay as they are (item 5).

**Regression guard.** The exactly-one-action and headers-belong-to-`webhook:` rules move into `hookConfig.toHook` (`internal/config/hooks.go:46`, the only place both fields coexist) under `hookError`'s `hook %q:` prefix, so `TestLoadFileConfigRefusesAnUnrunnableHook`'s unknown-event, both-actions, no-action, headers-on-a-command and timeout rows keep pinning `hook "notify"`; its duplicate-names and webhook-not-http rows (the Runner's checks) re-pin on `reaction "notify"`. The package's own checks (name, events, action, timeout) run before `Reaction.Validate`, so `TestHookValidateRefusesEachRule` (`hooks_test.go:142`) and `TestValidateAllRefusesDuplicateNames` (:237) keep every message text but the key prefix. `example_test.go:113-117,131` moves to `ReactionPayload`, `RunnerOptions`, `ReactionRunner`, `NewReactionRunner` (the two deleted aliases dropped).

**Files:** `internal/reactions/hooks.go`, `runner.go`, `match.go`, `command.go`, `webhook.go`, `payload.go`, `exec.go`, `workspace.go`, their `_test.go`, `apogee.go`, `example_test.go`, `internal/config/hooks.go`, `internal/config/hooks_test.go`, `internal/config/options.go`, `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_firing.go`, and the cmd tests constructing hooks (`grep -rln 'hooks\.Hook{\|reactions\.Hook{' --include=*_test.go .`).

**Tests:** the package suite green over `domain.Reaction`; `TestLoadFileConfigResolvesTheHooksBlock` asserts the resolved `domain.Reaction` values (origin/class/handler type); `TestHookValidateRefusesEachRule` and `TestValidateAllRefusesDuplicateNames` keep every message text but the key prefix; `TestLoadFileConfigRefusesAnUnrunnableHook`'s rows pin `hook`/`reaction` as the guard says.

**Acceptance:**
```
! grep -rn 'type Hook struct' internal/reactions/
go test ./internal/reactions/ ./internal/config/ . ./cmd/apogee/
```

**Commit:** `refactor(reactions): the Runner takes domain.Reaction observe rows; the Hook type is deleted`

## 5. The Runner's wording: `approval-requested`, `approval-decided`, `APOGEE_REACTION_*`, `reaction` — ✅ DONE (2026-09-08)

NOTES (2026-09-08): the item's acceptance forbids any `APOGEE_HOOK` in `internal/config/defaults/config.yaml` and in any `.go` file, which reaches three names the item's prose did not enumerate (enumeration is a floor): the template's example webhook token variable `APOGEE_HOOK_TOKEN` is now `MY_WEBHOOK_TOKEN` — it is the user's own variable, never one apogee sets, so it must not wear the reserved prefix — and the test-owned sink and token variables `APOGEE_HOOK_SINK` / `APOGEE_HOOK_TEST_*` are now `APOGEE_TEST_SINK` / `APOGEE_TEST_*`.

NOTES (2026-09-08): `internal/reactions` gained an `ApprovalDecided` Event constant beside the renamed `ApprovalRequested` — the package named five of the six standalone notices and `matchApproval` now needs the sixth to match on. Its doc comment moves from "the five named below" to "the six".

NOTES (2026-09-08): `TestEventValuesArePinnedLiterals` now pins eleven, as the item asks: the six the package names through its own constants, the five seam-closing notices through the core's, since this package names no alias for them.

NOTES (2026-09-08): beyond the item's named tests, two additions carry the new surface — an `approval-decided` inline golden in `TestPayloadJSONGolden` (the first pin of the `decision` field's JSON), and an environment-sink assertion in `TestE2EHooksFireFromTheTUI`: the sink entry's script now echoes `$APOGEE_REACTION_EVENT` and `$APOGEE_REACTION_PATH` to a second file, which is the item's "the exact names the executor sets" claim made from the outside.

NOTES (2026-09-08): the template's `hooks:` comment block now lists six events rather than five, since `approval-decided` joins the list a user reads before writing `events:`.

NOTES (2026-09-08): consequential edit — internal/reactions/doc.go: made necessary by the `APOGEE_HOOK_*` → `APOGEE_REACTION_*` rename, which the package doc named twice.

NOTES (2026-09-08): the item asks the retired `approval-waiting` spelling to keep loading through `toHook` while forbidding that literal in any `.go` file. `toHook` compares against it as `"approval-"+"waiting"` — built from two pieces so the acceptance sweep cannot match it — with a comment that names the retired spelling without writing it whole, and the compatibility is pinned by `TestLoadFileConfigAcceptsThePreRenameApprovalEvent` in `internal/config/hooks_test.go`, which loads an `events:` list holding the retired name (same two-piece const) and asserts it resolves to `reactions.ApprovalRequested`.

**What:** Depends on items 2 and 4. Hard rename, no aliases (A6): `domain.MomentApprovalWaiting` → `MomentApprovalRequested = "approval-requested"` (domain literal, `allNotices`, facade); `matchApproval` fires `approval-requested` on `ApprovalRequested` and `approval-decided` on `ApprovalDecided`, the latter's payload adding `decision` (the `domain.ApprovalDecision` spelling); `Payload.Hook` → `Payload.Reaction` (json `reaction`); env consts `APOGEE_REACTION_EVENT/NAME/WORKSPACE/PATH/SCHEDULE_ID/SCHEDULE_NAME`; report lines `reaction %s (%s): %v`, `reaction %s: dropped 1 event (queue full)`, `reaction %s: dropped %d events`; `ParseEvent`'s refusal `unknown reaction event %q — the events are …`; the closed-runner error names reactions. Every pinned test moves to the new spellings, including `cmd/apogee/e2e_hooks_test.go`, `headless_test.go`, `daemonfire_test.go`, `wire_settings_test.go`, `internal/config/hooks_test.go`, `internal/agent/dispatch_test.go` and the tui tests naming `approval-waiting` (`grep -rln 'approval-waiting\|APOGEE_HOOK\|"hook"' --include=*.go .`). The `hooks:` config key still accepts `approval-waiting` in `events:` for exactly one more item — `toHook` maps it to `approval-requested` so an unmigrated file keeps working until item 9 rewrites it.

**Regression guard.** The sweep includes `internal/config/defaults/config.yaml:449-476` — the template's `hooks:` comment block seeded into `~/.apogee/config.yaml` on first run: its four `APOGEE_HOOK_*` env names (:455-456) become `APOGEE_REACTION_*` and its `approval-waiting` lines (:449, :467) become `approval-requested` (the `events:` example keeps loading through the `toHook` compatibility). `approval-decided` reverses the recorded decision that the decided phase is deliberately not an event (`internal/hooks/match.go:113-114`, ADR 0073 §2) — superseded by ADR 0076 A6, which the rewritten comment names.

**Files:** `internal/domain/reaction.go`, `reaction_test.go`, `internal/reactions/hooks.go`, `match.go`, `payload.go`, `runner.go`, `command.go`, their `_test.go`, `apogee.go`, `internal/config/hooks.go`, `internal/config/hooks_test.go`, `internal/config/defaults/config.yaml`, and the tests the grep names.

**Tests:** `TestEventValuesArePinnedLiterals` (eleven), `TestMatchApproval` asserts both phases and the `decision` value, `TestPayloadJSONGolden` inline goldens carry `"reaction":`, `TestPayloadEnv` the six new names, `TestParseEventErrorTextIsByteIdentical` the new sentence; the e2e hooks suite drives a script reading `$APOGEE_REACTION_EVENT` and `$APOGEE_REACTION_PATH` — the exact names the executor sets.

**Acceptance:**
```
! grep -rn 'approval-waiting\|APOGEE_HOOK\|MomentApprovalWaiting' --include=*.go .
! grep -n 'approval-waiting\|APOGEE_HOOK' internal/config/defaults/config.yaml
go test ./internal/domain/ ./internal/reactions/ ./internal/config/ ./internal/agent/ ./internal/tui/ . ./cmd/apogee/
```

**Commit:** `feat(reactions)!: approval-requested and approval-decided notices; APOGEE_REACTION_* and the reaction payload field`

## 6. The agent emits `SeamClosedEvent` when a seam closes — ✅ DONE (2026-09-08)

NOTES (2026-09-08): the emit is installed AFTER `seamPayload` resolves, so the one dispatch that closes no seam is the engine bug `seamPayload` refuses (a payload that is not the Moment's own, a handler that cannot serve it, or a Moment that is no seam at all). No ladder ran there, the Moment may be no seam, and the payload is by definition not the one the event promises to carry — emitting it would make the event lie. Stated in `fire`'s doc comment.

NOTES (2026-09-08): the booked ids are appended inside `bookFiring` (which gained a `fired *[]string` out-param, threaded through `fireLeg`) rather than at its one call site, so a firing cannot be booked without being recorded. `fireLeg` and `bookFiring` are unexported and have no other callers.

NOTES (2026-09-08): beyond the item's named tests, `assertSeamClosed` also pins the closure as the LAST event of the pass — a closure reported before the cascade's own `ReactionFiredEvent`s would be reporting a pass that had not happened yet, and that ordering is what `TestFireEmitsSeamClosedAfterAReturnedError` means by "the event follows the error".

**What:** Depends on item 2. `fire()` in `internal/agent/reactions.go` emits one `SeamClosedEvent{Seam: m, Fired: <ids bookFiring booked in this call, in order>, Value: payload}` through `a.cfg.Events.Emit` after its ladder returns — on success, on a returned error, and under Bypass or with nothing armed (A5: unconditional) — once per `fire` call, so a retried post-response Turn closes once per attempt. `firePostToolResult` goes through the same path. `Value` is the payload reference `fire` received; nothing is copied. The fan-out pre-tool-exec path in `dispatch.go` is covered by the same `fire`.

**Regression guard.** `internal/agent/subagent_test.go`'s `eventBaseOf` (:201-231, default → false) gains a `case domain.SeamClosedEvent: return ev.EventBase, true` arm — `TestSubAgent_EventsCarryTheSpawningCallID` (:134-158) Fatals on any sink event it does not know once `fire` emits one per seam.

**Files:** `internal/agent/reactions.go`, `internal/agent/reactions_test.go`, `internal/agent/subagent_test.go`.

**Tests:** `TestFireEmitsSeamClosedForEverySeam` (one event per seam, `Fired` equals the booked ids, `Value` is the same pointer/value); `TestFireEmitsSeamClosedUnderBypassAndWhenNothingIsArmed`; `TestFireEmitsSeamClosedAfterAReturnedError` (the event follows the error, `Fired` holds the ids that acted before it); the existing dispatch-semantics suite unchanged; `TestSubAgent_EventsCarryTheSpawningCallID` green.

**Acceptance:**
```
go test ./internal/agent/ -run 'TestFire|TestBuiltin|TestReaction|TestFloorGuard|TestSubAgent_EventsCarryTheSpawningCallID'
go test ./cmd/apogee/ -run 'TestE2EReactionIdentity|TestE2EEventLinesGolden'
```

**Commit:** `feat(agent): fire publishes a sink-only SeamClosedEvent when every seam closes`

## 7. The Runner matches the five seam-closing notices — ✅ DONE (2026-09-08)

NOTES (2026-09-08): the projector is a `project` FIELD on `matcher`, defaulted to
`projectSeamValue` in `newMatcher`, rather than a package variable — the item's "test seam on the
projector" without a global, and `newMatcher` is the package's only construction site.

NOTES (2026-09-08): `projectSeamValue` answers nil for a seam/value pair the engine could not have
produced (another seam's payload, a nil pointer, a notice in the `Seam` field), so the payload
simply omits `value` rather than panicking on the engine's own goroutine; `matchSeamClosed`
likewise ignores a `Seam` whose `Closing()` is the zero Moment, since a firing named by the empty
string would be unroutable. Both are pinned by table tests beyond the item's named list.

NOTES (2026-09-08): the `reactions` payload field is a COPY of the event's `Fired` slice, and the
argument bytes of every projected tool call are copied too — the event's value is read-only and
valid only during `Emit`, so nothing the payload keeps may point back into it. Both copies are
pinned (in `TestMatchSeamClosedMapsEverySeamToItsNotice` and
`TestSeamClosedProjectionIsTakenBeforeEmitReturns`).

NOTES (2026-09-08): the pre-request projection carries messages and tool names only, as the item
lists — the request's model, sampling and budget are left out. The tool MENU is reduced to names
because the schemas would be several kilobytes of JSON on every request; stated in
`projectToolNames`.

NOTES (2026-09-08): the five goldens live in a new `TestSeamClosedPayloadJSONGolden` rather than as
rows of `TestPayloadJSONGolden` — each case builds the real domain working value and projects it,
so the goldens pin the projection and the wire shape together, which a hand-built `Payload` row
could not. The pre-tool-exec case carries no fired ids, pinning `reactions`' omission on the
ordinary pass.

NOTES (2026-09-08): `docs/manual/hooks.md` documents the payload fields and is not touched here —
items 18 and 19 own the manual, and neither is done.

**What:** Depends on items 5 and 6. `match.go` maps `SeamClosedEvent` to `<seam>-finished` for subscribed entries, Depth-0 only like `matchTurn`. The payload gains `seam` (the seam's Moment), `reactions` (the fired ids, `omitempty`), and `value` — the JSON projection of the working value, built **inside `Emit`** before queueing and **only when an entry subscribes** (call: notice payload). Projections live in `payload.go`, one per seam: pre-request → the Request's messages (role, content, tool calls) and tool names; post-response → the Response text, tool calls, and `retryable`; pre-tool-exec → the ToolCall (id, name, arguments); post-tool-result → the ToolCall and the result text plus `is_error`; history-rewrite → the Conversation's messages. Unsubscribed seams cost one map lookup. Document on `Options` that `Value` is not retained past `Emit`.

**Files:** `internal/reactions/match.go`, `match_test.go`, `payload.go`, `payload_test.go`, `runner.go`, `runner_test.go`, `doc.go`.

**Tests:** `TestMatchSeamClosedFiresOnlyWhenSubscribed` (no projection call when nothing subscribes — count via a test seam on the projector); five inline payload goldens, one per seam, pinning `"event":"<seam>-finished"`, `"seam"`, `"reactions"` and the `value` shape; `TestMatchSeamClosedIsTopLevelOnly`; a runner test proving the projection happened before `Emit` returned (mutate the value after `Emit`, the queued payload is unchanged).

**Acceptance:**
```
go test ./internal/reactions/ -run 'SeamClosed|Payload|Match'
```

**Commit:** `feat(reactions): the five seam-closing notices reach observe reactions with the full working value`

## 8. Config: the `reactions:` key, additive — ✅ DONE (2026-09-08)

NOTES (2026-09-08): the item's What spells the reserved-action refusal only in its `advise:` form; the implementation names whichever key is present, so `gate:` earns `reaction %q: gate: is not yet shipped (ADR 0076 stage 3)`. Both texts are pinned by the refusal table test.

NOTES (2026-09-08): one refusal the item did not spell is new — an entry with no `id:` earns `reactions: an entry has no id: every reaction needs an `id:` to be reported by`. Without it the message a user meets is internal/reactions' own `hooks: an entry has no name`, which names the wrong key.

NOTES (2026-09-08): the `hooks` and `reactions` registry rows SHARE one keyAccessor projection (`projectReactions`) rather than each writing its own half of the field. `keyAccessors`' own contract is that a row's position in the table cannot affect the outcome, which two rows appending to one Options field would break; the shared-carrier idiom (the three system-prompt keys, the four present keys) is what the table already documents for this shape.

NOTES (2026-09-08): `validateHooks` is folded into `validateReactionBlocks(fc.Hooks, fc.Reactions)`, which is what the item's "ids unique across `hooks:`+`reactions:`" requires — the uniqueness check has to see both blocks at once.

NOTES (2026-09-08): `HookEnvNames` → `ReactionEnvNames` MOVED from `hooks.go` to `reactions.go` (and its test from `hooks_test.go` to `reactions_test.go` as `TestReactionEnvNamesDeduplicatesAndSorts`): it now reads `Options.Reactions`, which is reactions.go's field, and item 9 deletes hooks.go and its test wholesale. For the same reason `defaultHookTimeout` becomes `defaultReactionTimeout` in reactions.go and `toHook` reads it, rather than the package carrying two 30s constants.

NOTES (2026-09-08): the `hooks` registry row's `Read`/`Structure` now project `o.Reactions` — the field it named was renamed — so while both rows exist they summarize the same lane under two counts ("N hook" / "N reaction"). Item 9 deletes the `hooks` row.

NOTES (2026-09-08): consequential edit — internal/config/config_test.go: made necessary by the `Options.Hooks` → `Options.Reactions` rename and the new schema key — `TestEveryConfigKeyReachesTheOptions` enumerates the Options field names config owns, and `everyKeyFileConfig` enumerates every fileConfig key, so both had to gain the renamed field and the `reactions:` block.

NOTES (2026-09-08): internal/reactions' own refusal wordings still say `hooks:` (the no-name message) and "hook names must be unique" (the duplicate message a cross-block id collision earns). Pre-existing wording, not touched here — item 21's emitted-string sweep owns it.

**What:** Recast at the regression check (2026-09-08). Depends on item 4. Add `Reactions []reactionConfig \`yaml:"reactions"\`` to `fileConfig` beside the still-parsed `hooks:`; `reactionConfig{ID string \`yaml:"id"\`; On []string \`yaml:"on"\`; Run yaml.Node \`yaml:"run"\`; Timeout, Workspace string; Enabled *bool; Advise, Gate yaml.Node}` in a new `internal/config/reactions.go` (named in `doc.go`; `hooks.go` stays until item 9). Resolution into `Options.Reactions []domain.Reaction` (the old `Options.Hooks` field renamed — its producers are `toHook` and the new resolver, its consumers `wire_boot.go`, `wire_firing.go`, `wire_settings.go`, `headless.go`, `HookEnvNames`): `run:` sequence → `ArgvHandler`, mapping `{url, headers, headers-env}` → `WebhookHandler`, anything else → `reaction %q: run: is an argv list or a webhook mapping {url:, headers:, headers-env:}`; `advise:`/`gate:` present → `reaction %q: advise: is not yet shipped (ADR 0076 stage 3)`; an id equal to a Floor-guard key → `reaction %q: that is the Floor guard %s: — set the top-level key, not a reactions: entry`; a seam in `on:` → the domain error from item 3; `enabled: false` → dropped at resolve; `timeout:` default 30s; ids unique across `hooks:`+`reactions:`. `HookEnvNames` → `ReactionEnvNames`. `KeyRegistry` gains `{Path: "reactions", Kind: KindStructured, Editable: false, Desc: "Commands and webhooks run when a Moment closes; observe-only, never seen by the model.", Read: countSummary(len(o.Reactions), "reaction"), Structure: o.Reactions}` placed after `bypass` (call: row placement); the `hooks` row stays until item 9. `defaults/config.yaml` gains the `reactions:` block (two entries mirroring today's `hooks:` examples) beside the old one. `docs/manual/configuration.md`'s `## Hooks — hooks:` section (:365-395) is rewritten as `## Reactions — reactions:` documenting the schema above — `docs_settings_test.go` requires the row's key documented.

**Regression guard.** `reactionConfig` decodes `run:`/`advise:`/`gate:` into position-free values (`Run any`, `Advise any`, `Gate any`; presence = non-nil) — no `yaml.Node` inside `fileConfig`, so `sameApartFrom` keeps working for every other write; the rewritten `## Reactions — reactions:` manual section keeps exactly one back-ticked sentence naming `hooks:` as the earlier name until item 9 deletes the row (it survives item 9 as the migration pointer); Tests name the `"reactions": noneSettingValue` pin in `TestSettingsRowsFormatEffectiveValues` and the `reactions` entry in the external-edit list (`settingsrows_test.go:543`).

**Regression guard (round 2).** The `Options.Hooks` → `Options.Reactions` rename also reaches `cmd/apogee/daemonfire.go:292` and `schedule.go:131` (`firingHooks(…opts.Hooks…)`) and the tests `schedule_test.go:1385`, `daemonfire_test.go:900,933`, `wire_firing_test.go:1111`, `wire_settings_test.go:815-823,1014-1015,2943-2995,3093,3163`; the grep, not the list, bounds the sweep — every `opts.Hooks`/`Hooks:` site: `grep -rn '\.Hooks\b\|Hooks:' --include=*.go cmd/ internal/config/`, and for the tests `grep -rln 'Options{[^}]*Hooks\|\.Hooks\b\|file\.Hooks' --include=*_test.go .`.

**Files:** `internal/config/reactions.go`, `internal/config/reactions_test.go`, `internal/config/configwrite_scalar_test.go`, `internal/config/config.go`, `internal/config/options.go`, `internal/config/registry.go`, `internal/config/doc.go`, `internal/config/defaults/config.yaml`, `internal/config/hooks.go`, `cmd/apogee/wire_boot.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/wire_settings.go`, `cmd/apogee/headless.go`, `cmd/apogee/daemonfire.go`, `cmd/apogee/schedule.go`, `cmd/apogee/settingsrows_test.go`, `cmd/apogee/schedule_test.go`, `cmd/apogee/daemonfire_test.go`, `cmd/apogee/wire_firing_test.go`, `cmd/apogee/wire_settings_test.go` and the tests the grep names, `docs/manual/configuration.md`.

**Tests:** `TestLoadFileConfigResolvesTheReactionsBlock` (argv + webhook entries, parked entry dropped, timeout default); one table test per refusal above pinning the exact text; `TestRegistryIsBijectionWithFileConfig`, `TestRegistryRowsProjectEveryValue` green; `settingsrows_test.go` section map gains `reactions` → "Reactions", `TestSettingsRowsFormatEffectiveValues` the `"reactions": noneSettingValue` pin, the external-edit list (:543) a `reactions` entry; a `configwrite_scalar_test.go` case writes a scalar above a `reactions:` block and verifies; `TestSettingsDocsCoverEveryKey` (docs_settings_test) green.

**Acceptance:**
```
go test ./internal/config/ -run 'Reactions|Registry|Hooks'
go vet ./cmd/apogee/ && go test ./cmd/apogee/ -run 'Settings|Docs'
```

**Commit:** `feat(config): the reactions: list resolves user-origin observe Reactions beside hooks:`

## 9. Config: migrate `hooks:` into `reactions:`, strip `mechanisms:` and `validated-sets:`, delete `hooks:` — ✅ DONE (2026-09-08)

NOTES (2026-09-08): the item's open either/or is resolved as the run's DECISION says — the fold runs
ONLY at startup. `parseConfigFile` gained a `mayMigrate bool`; `ApplyConfig`'s startup read passes
`true` and `LoadFileConfig` (the seven live re-reads in `cmd/apogee/wire_settings.go`, and every
test reader) passes `false`. A live re-read of a file still carrying `hooks:` refuses with
`liveReactionsRefusal` and writes nothing; the sentence is pinned verbatim by
`TestLoadFileConfigRefusesTheHooksBlockWithoutRewritingIt`. The migration note therefore never
surfaces through `reloadReactions`' report string, as the DECISION states.

NOTES (2026-09-08): because the strip is startup-only, a per-server `mechanisms:` map still reaches
the LIVE apply path, so the round-2 guard's instruction to drop the `libary: true` case of
`TestApplySettingServersDrivesTheSubAgentServer` (`cmd/apogee/delegation_test.go:1131-1138`) was NOT
followed — that case still passes and still pins `delegation.go:340-347`'s refusal. Dropping it
would have deleted live coverage the DECISION keeps alive. `internal/config/config_test.go`'s
`TestApplyConfigMechanisms` and `TestMechanismsKeyParsesWithoutARegistryRow` WERE replaced (they read
through the startup path), by one `TestMechanismsKeyIsStrippedAndHasNoRegistryRow`.

NOTES (2026-09-08): `validated-sets:` stripping is NOT in this commit — the item's own Regression
guard moves it to item 15 — so the note carries no `validated-sets:` fragment and the commit subject
drops it from the plan's wording.

NOTES (2026-09-08): `migrateLegacyConfig` was restructured to apply BOTH folds (the ADR 0036
quadruple and this one) to the bytes before writing, so a file carrying two retired shapes is backed
up once and rewritten once. Two passes would have collided on `backUpConfig`'s O_EXCL backup name,
which is dated to the second.

NOTES (2026-09-08): the item spells the sniff as a struct with yaml tags, but the item's own
acceptance forbids `yaml:"hooks"` anywhere in `internal/config/`. `legacyReactionsConfig` therefore
reads the retired blocks off the node tree (`mappingEntry`) as line SPANS — which the splice needs
anyway — and `legacyHookConfig` (the private converter the guard asks for) carries only the entry's
own tags.

NOTES (2026-09-08): the `retired` id → Floor-key table was COPIED into `configmigrate.go` as
`retiredMechanismSuccessors`, not moved: `internal/mechanisms/retired.go` still serves the three
`RetiredNotices` callers until item 16 deletes them.

NOTES (2026-09-08): a top level the splice cannot read (a flow mapping, a list, a scalar) in a file
that still carries `hooks:` is REFUSED rather than left alone — the schema has no `hooks:` field any
more, so silence would take the block out of service without ever failing (ADR 0036's
refusal-over-silence posture). Pinned by `TestMigrateLegacyConfigRefusesAFileItCannotSplice`, which
took the place of the item's "verify failure → no write, no backup" case: the transaction makes a
genuine verify failure unreachable from a file, so the verify's three refusals are pinned directly
instead by `TestVerifyReactionsFoldRefusesEachWayTheEditCouldBeWrong`.

NOTES (2026-09-08): the item's "read-only `config.yaml`" refusal test is
`TestMigrateLegacyConfigRefusesWhenItCannotBackUp` instead — the suite runs as root here, where a
read-only file is still writable, so the un-writable case is made by taking the backup name.

NOTES (2026-09-08): consequential edit — internal/config/doc.go: made necessary by deleting
hooks.go — the package map named the file.

NOTES (2026-09-08): consequential edit — internal/config/configwrite_keysource_test.go: made
necessary by `parseConfigFile` gaining its `mayMigrate` argument.

NOTES (2026-09-08): consequential edit — docs/manual/configuration.md: made necessary by the fold —
the `## Reactions — reactions:` section's `hooks:` sentence said the old block "keeps loading",
which is now false; it names the fold, the backup and the lost comments instead. The section keeps
exactly one back-ticked `hooks:` sentence, as the item's guard requires.

NOTES (2026-09-08): `cmd/apogee/e2e_hooks_test.go`'s helpers now write `reactions:` blocks and the
applied-keys assertion names `reactions`, but the helper NAMES (`hookBlock`, `hookBlockOf`,
`rewriteHomeHooks`, `readHookPayloads`, …) are left as they are — item 21 owns that wording pass,
and renaming three of a dozen would leave the file half-migrated.

NOTES (2026-09-08): `liveSettings.setHooks` and the `settingsApplier.hooks` field keep their names
for the same reason; only `reloadHooks` → `reloadReactions` was renamed, which the item's guard
names explicitly.

NOTES (2026-09-08): the template's commented-out `mechanisms:` example block is left in
`internal/config/defaults/config.yaml` — item 16 owns deleting the key and its documentation, and it
is not done.

**What:** Recast at the regression check (2026-09-08). Depends on item 8. Extend `migrateLegacyConfig` (ADR 0036 D9 idiom) with a shadow `legacyReactionsConfig{Hooks, Reactions, Mechanisms, ValidatedSets yaml.Node; Servers []struct{Mechanisms yaml.Node}}` read from the bytes. Order: (1) both `hooks:` and `reactions:` present → refuse with no write, in `legacyRefusal`'s shape: `apogee: %s has both hooks: and reactions: — reactions: is the single list (hooks: was its earlier name).\n\napogee did not fold hooks: in for you because reactions: already exists.\n\nMove the hooks: entries into reactions: (name: → id:, events: → on:, command: or webhook: → run:) and delete hooks:.` (2) `hooks:` alone → parse the block with the item-4 converter, render a `reactions:` block from the entries (`name`→`id`, `events`→`on` with `approval-waiting`→`approval-requested`, `command`→`run:` sequence, `webhook`+`headers`+`headers-env`→`run:` mapping, `workspace`/`timeout` kept) and splice it over the old block's line range (call: fold shape). (3) Strip the top-level `mechanisms:` block, every per-server `mechanisms:` block, and the `validated-sets:` block. Verify against the bytes: the folded entries re-parse to the same `[]domain.Reaction` the old block resolved to, the retired keys are gone, `sameApartFrom(before, after, "hooks", "reactions", "mechanisms", "validated-sets")` with servers compared minus their `Mechanisms` map. Then backup, atomic write, one note through `notify`: `apogee: rewrote %s — hooks: became reactions: (%d entries)[; approval-waiting is now approval-requested][; the retired mechanisms: key was dropped][; the inert validated-sets: key was dropped]; comments inside the old block did not survive; backup at %s. Scripts must read APOGEE_REACTION_* (was APOGEE_HOOK_*) and the payload's "reaction" field (was "hook").` — bracketed parts only when they apply, the trailing sentence only when `hooks:` was folded. Then delete `fileConfig.Hooks`, `hookConfig`, `toHook`, `internal/config/hooks.go` (+test), the `hooks` `KeyRegistry` row, the template's `hooks:` block, and the `approval-waiting` compatibility from item 5.

**Regression guard.** the migration folds `hooks:` and strips `mechanisms:` (top-level and per-server) ONLY — `validated-sets:` stripping moves to item 15 (its rows are still writable at this commit); `sameApartFrom(before, after, "reactions", "mechanisms")` with servers compared minus their `Mechanisms` map — `hooks` is not a path any more, `fileConfig` ignores it; a private `legacyHookConfig` + converter lives in `configmigrate.go` (the `legacyFileConfig` idiom) and verifies the folded entries by re-resolving both sides to `[]domain.Reaction`; the note's `[; the inert validated-sets: key was dropped]` fragment moves to item 15; the note's mechanisms fragment carries the successor hint — the `retired` id → Floor-key successor table from `internal/mechanisms/retired.go` moves into `configmigrate.go` as a private literal map (the "config migration table" greenfield §9.1 names) and the fragment reads `; the retired mechanisms: key was dropped (%s)` where `%s` lists `<id> → <floor-key>:` for each stripped id that has a successor, or `the catalogue is empty; the seven Floor keys are the only switches` when none does; Files gain `cmd/apogee/wire_settings.go` (the `settingsTable` entry and `reloadHooks` become `reactions`/`reloadReactions` reading `file.Reactions`), `cmd/apogee/wire_settings_test.go` (the `hooks` apply/order cases re-keyed), `cmd/apogee/headless_test.go` and `cmd/apogee/daemon_test.go` (delete `TestHeadlessReportsARetiredMechanism` and the daemon retired-notice test — a start-up notice for a key the file can no longer carry is superseded by the migration note this item's tests pin).

**Regression guard (round 2).** Stripping `mechanisms:` at Load reaches `internal/config/config_test.go`: delete `TestApplyConfigMechanisms` (:3378-3396) and `TestMechanismsKeyParsesWithoutARegistryRow` (:3404-3412) and drop the per-server `mechanisms:` rows from `TestApplyConfigServers`' fixture (:2853,2878,2882); a per-server unknown-id `mechanisms:` map no longer refuses — it is stripped — so the `libary: true` case of `TestApplySettingServersDrivesTheSubAgentServer` (`cmd/apogee/delegation_test.go:1131-1138`) is dropped (`delegation.go:340-347`'s refusal survives only for entries built in memory until item 16); `cmd/apogee/e2e_hooks_test.go` joins Files — the startup migration would rewrite the `hooks:` block `rewriteHomeHooks` (:519-531) cuts on, so the helpers write `reactions:` blocks and the applied-keys assertion (:254-256) names `reactions`; the seven live re-reads in `cmd/apogee/wire_settings.go` (:1685,2008,2032,2074,2090,2112,2146) pass `func(string) {}` as notify, so the migration note is returned through the apply's report string (the settings row's own channel) at least for `reloadReactions` — or the fold runs only from the startup path and the live re-read refuses without writing. A read-only `config.yaml` carrying `hooks:` becomes a startup refusal under the ADR 0036 D9 write-or-refuse idiom — intended; the refusal text names the file and the fold it could not write. ADR 0076 D11 as pinned at `internal/config/config_test.go:3398-3401` and `registry_test.go:64-66` ("the key keeps loading so an existing config file is not refused") is superseded by ADR 0076 A6, which this item cites. The grep rule that bounds `fileConfig.Hooks`' deletion reach in tests: `grep -rln 'Options{[^}]*Hooks\|\.Hooks\b\|file\.Hooks' --include=*_test.go .`.

**Files:** `internal/config/configmigrate.go`, `configmigrate_test.go`, `config.go`, `reactions.go`, `reactions_test.go`, `registry.go`, `registry_test.go`, `doc.go`, `defaults/config.yaml`, `internal/config/hooks.go` (deleted), `internal/config/hooks_test.go` (deleted), `cmd/apogee/settingsrows_test.go`, `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_settings_test.go`, `cmd/apogee/headless_test.go`, `cmd/apogee/daemon_test.go`, `internal/config/config_test.go`, `cmd/apogee/delegation_test.go`, `cmd/apogee/e2e_hooks_test.go` and the tests the grep names.

**Tests:** inline-YAML cases: fold of an argv and a webhook entry (byte golden of the rendered block); `approval-waiting` rewritten; both-present refusal text pinned and nothing written; mechanisms top-level + per-server stripped; each note variant pinned, both successor-hint forms included (`tool_use_enforcer → tool-use-enforcer:` and the empty-catalogue sentence); verify failure → no write, no backup; a second load is a no-op; a file with none of the keys is untouched. `TestRegistryIsBijectionWithFileConfig` green without the `hooks` row; `TestSettingsTableIsInRegistryOrder` green with the `reactions` entry; the `hooks` apply/order cases in `wire_settings_test.go` re-keyed; `TestHeadlessReportsARetiredMechanism` and the daemon retired-notice test deleted; `TestApplyConfigMechanisms` and `TestMechanismsKeyParsesWithoutARegistryRow` deleted, the servers fixture's `mechanisms:` rows dropped; the `libary: true` refusal case dropped; `e2e_hooks_test.go`'s helpers write `reactions:`; a live-re-read case pins the note in `reloadReactions`' report string (or the no-write refusal); a read-only file carrying `hooks:` pins the refusal text naming the file.

**Acceptance:**
```
! grep -rn 'yaml:"hooks"' internal/config/
go test ./internal/config/
go test ./cmd/apogee/ -run 'Settings|Docs|Hooks|Reactions'
```

**Commit:** `feat(config)!: hooks: migrates itself into reactions:; the dead mechanisms: and validated-sets: keys are stripped`

## 10. Agent: `SetReactions(gen)` and the builtin enable set — ✅ DONE (2026-09-08)

NOTES (2026-09-08): `internal/agent/construct.go` is edited beyond the item's Files list — it seeds `gen` from `cfg.Bypass`/`cfg.Floor` (replacing the `bypass:`/`floor:` literal fields the item deletes) and calls the new `buildBuiltins(cfg.Floor)` / `armReactions(cfg.Reactions)` signatures; the item's change cannot compile without it.

NOTES (2026-09-08): consequential edit — internal/agent/doc.go: made necessary by the enable set — the package map said builtins.go holds "the seven Floor guards … each reading its live gate", which the enable set makes false, and agent.go's setter list named Bypass/Floor rather than the one generation swap.

NOTES (2026-09-08): `floorConfig()` is deleted rather than kept: with the handlers no longer gating themselves, its only remaining callers were `subagent.go` (now `Generation().Floor`) and `TestFloorGuard_ChildInheritsTheLiveFloor` (now `Generation()`, per the item's own test list).

NOTES (2026-09-08): `armReactions` lost its `builtins` parameter and now reserves the new `guardIDs` (all seven keys, `floorguards.go`) — the guard paragraph's requirement, since a disabled guard is no longer in the builtins slice to reserve its own id from.

NOTES (2026-09-08): `SetBypass`/`SetFloor` are read-modify-write wrappers and therefore not atomic against each other; the doc comments say so, and the caveat disappears with the setters at item 13. `SetReactions` itself never publishes a half-swapped generation.

NOTES (2026-09-08): added `TestSetReactionsRebuildsTheLadderOnlyWhenTheFloorMoves` (the item's "a Bypass-only `SetReactions` leaves the ladder slice identical" test) under that name; the item did not name it.

**What:** Depends on item 3. `internal/agent` gains `SetReactions(gen domain.Generation)` and `Generation() domain.Generation` under one `genMu`, replacing the separate `floorMu`/`bypassMu` holders; `Config.Bypass`/`Config.Floor` seed the initial generation. Builtins become an **enable set** (call): `buildBuiltins` returns only the guards whose boolean is on, and the ladder is rebuilt from the generation on every `SetReactions` — the handlers stop reading `a.floorConfig()` at fire time. Children inherit the parent's current generation (replaces `childCfg.Bypass = a.bypassEnabled()` and the floor inheritance). `SetBypass(bool)` and `SetFloor(FloorConfig)` remain as wrappers that read-modify-write the generation (call: old setters) until item 13. `Generation.Observe` is ignored by the agent in this stage (it is the Runner's) — stated in the doc comment.

**Regression guard.** `armReactions` reserves all seven guard keys (the `guard*` constants, `floorguards.go:12-20`) regardless of the enable set, so the "that ID is already armed" refusal keeps firing for a `Config.Reactions` entry named after an off guard. The builtin ladder is rebuilt only when `gen.Floor` differs from the held Floor — a Bypass-only swap leaves it, so `ladderAgent`'s probe (`reactions_test.go:70-72`) survives `SetBypass(true)` and `TestFireBypassMatrix` stays unchanged in outcome. `fire` snapshots the ladder slice under `genMu.RLock` before `fireLeg` (today it reads `a.builtins` unlocked, `reactions.go:126`). `genMu` over Floor+Bypass reverses the one-mutex-per-field rule (`agent.go:128-134`, ADR 0037 D2 "one mutex, one field") — superseded by ADR 0076 A8 (the Generation-shape call); the rewritten doc comment names the supersession.

**Files:** `internal/agent/agent.go`, `floorguards.go`, `builtins.go`, `reactions.go`, `subagent.go`, `setlive_test.go`, `floorguards_test.go`, `reactions_test.go`.

**Tests:** `TestBuiltinReactionsAreTheSevenFloorGuards` becomes `…WhenEveryGuardIsOn` plus `TestBuiltinEnableSetDropsAGuardWhoseBooleanIsOff` (with the case that an off guard's id is still refused for a `Config.Reactions` entry); `TestSetReactionsSwapsFloorAndBypassAtomically` (the concurrent-setters race shape from `setlive_test.go`, run against a concurrent `fire` under `-race`); a Bypass-only `SetReactions` leaves the ladder slice identical; `TestFloorGuard_ChildInheritsTheLiveFloor` through `SetReactions`; `TestFireBypassMatrix` unchanged in outcome.

**Acceptance:**
```
go test -race ./internal/agent/ -run 'TestFire|TestBuiltin|TestReaction|TestFloorGuard|TestSetReactions|TestSetLive'
go test ./cmd/apogee/ -run 'TestE2EReactionIdentity'
git diff --quiet d84dd988 -- cmd/apogee/testdata/eventlines/
```

**Commit:** `feat(agent): one SetReactions generation swap; the Floor guards are an enable set`

## 11. Driver: `lateEngine.SetReactions` and the `settingsEngine` seam — ✅ DONE (2026-09-08)

NOTES (2026-09-08): `SetReactions` returns an `error` where the item spells `SetReactions(apogee.Generation)` on the interface. The Runner is the one half that can refuse a generation (a malformed entry, an unresolvable `workspace:`), and that refusal is the sentence the settings row shows today (`reloadReactions` returns `Replace`'s error); without a return, item 12's move of that reload onto this door would apply a refused `reactions:` edit silently. Floor and Bypass are booleans, so the engine half never fails.

NOTES (2026-09-08): the guard's field-by-field comparison (id, `On`, `Workspace`, `Timeout`, the handler's argv/URL/headers) is one `reflect.DeepEqual` over the two observe lists — the handler is an interface over values carrying maps, which no comparison operator reaches, and `delegation.go:716` already asks whether a resolved server entry moved the same way. It is strictly stronger than the five fields, and its only failure direction is a spurious swap, never a dropped edit.

NOTES (2026-09-08): the bind replay applies the engine half alone. The Runner exists from boot, independent of the Agent's lifetime, so `SetReactions` swapped its list when the edit happened whether or not anything was bound — which keeps the recorded observe list and the pending generation's in step, making a swap-rule branch at the bind dead code by construction. The rule holds; there is no branch for it.

NOTES (2026-09-08): `newLateEngine` keeps its two arguments as the round-2 guard requires, and the Runner arrives through `seedReactions(runner, gen)` rather than a bare `w.engine.runner = w.hooks`: the holder needs the generation both halves are ALREADY running as well. Without that seed a partial edit (`SetBypass`, `SetFloor`, which read-modify-write the held generation) would read a zero base and re-enable every Floor guard the config file switched off, and the first Floor toggle of a session would drain a Runner whose list never moved.

NOTES (2026-09-08): wire_firing.go's edit is the parameter and doc naming (`list` → `observe`, the doc naming the generation's observe half). `firingHooks` already receives `gen.Observe` — item 8 renamed `Options.Hooks` to `Options.Reactions`, which is what every Firing root passes, and item 12 makes that projection read `s.gen.Observe`. A `domain.Generation` signature would reach `daemonfire.go`, `headless.go`, `schedule.go` and `wire_firing_test.go`, none of which this item lists, and the Firing's Floor and Bypass already come from `in.opts` at the Config literal.

NOTES (2026-09-08): `TestSetReactionsReportsTheRunnersRefusal` is beyond the item's named tests — the error return is new behaviour on the seam and the standards require a failure case for it; it also pins that a refused list is not recorded as applied, so a later identical edit still tries.

**What:** Recast at the regression check (2026-09-08). Depends on item 10. `cmd/apogee/wire_engine.go`: `lateEngine.SetReactions(gen)` with a pre-engine `pendingGeneration` replay (the `pendingBypass`/floor replays fold into it); it applies `engine.SetReactions(gen)` then `runner.Replace(gen.Observe)` — the Runner keeps its one swap method, now called only from here. `settingsEngine` (`wire.go:326`) gains `SetReactions(apogee.Generation)`; `SetBypass`/`SetFloor` stay on the interface until item 13. The spy in `wire_helpers_test.go` records generations. Firings (`wire_firing.go`) build their Runner from `gen.Observe`.

**Regression guard.** `lateEngine.SetReactions` holds the last-applied `Observe` beside `pendingGeneration` and calls `runner.Replace` ONLY when `gen.Observe` differs (compare id, `On`, `Workspace`, `Timeout` and the handler's argv/URL/headers); Floor- or Bypass-only generations reach the engine alone, and the bind replay applies the same rule; Files gain `cmd/apogee/wire_live.go` (`newLateEngine(w.mode, w.opts.ConfineToWorkspace, w.hooks)` hands the Runner in); `TestLateEngineReplaysTheFloorGatesAtTheBind` is re-expressed over `pendingGeneration` (`TestLateEngineReplaysThePruneGateAtTheBind` stays); the header call "Generation shape" gains the qualifier "the Runner swaps only when Observe differs".

**Regression guard (round 2).** `newLateEngine(mode, confine)` keeps its two-argument signature (21 test call sites in seven files use it) — the Runner is handed in by field assignment after `wire_live.go:224` (`w.engine.runner = w.hooks`), which replaces the three-argument form above; that field is a package-local `interface{ Replace([]domain.Reaction) error }` so the two order/skip tests install a recording fake (its `Replace` reads `agent.Generation()` to pin the order) — a concrete `*reactions.Runner` exports only Emit/Replace/Close (`runner.go:190,254,285`) and `applySettingSpy` (`wire_helpers_test.go:90`) sits above `lateEngine`; `SetReactions` and the bind replay skip the Runner swap when the field is nil, the way `bound()` skips a nil Agent (`wire_engine.go:219`) — `(*Runner).Replace` on nil dereferences `r.swapMu` (`runner.go:258`); folding `pendingBypass` into `pendingGeneration` re-expresses `TestLateEngineRemembersSettingsMovedBeforeTheBind` (`wire_settings_test.go:1960-2000`, reads `pendingBypass` at :1969-1970 and :1997) over `pendingGeneration`.

**Files:** `cmd/apogee/wire_engine.go`, `cmd/apogee/wire_engine_test.go`, `cmd/apogee/wire.go`, `cmd/apogee/wire_helpers_test.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/wire_settings_test.go`.

**Tests:** `TestLateEngineReplaysThePendingGeneration`; `TestSetReactionsAppliesEngineThenRunner` (order pinned via the spy) plus `TestSetReactionsSkipsTheRunnerWhenObserveIsUnchanged` (a Floor-only and a Bypass-only generation reach the engine, `Replace` is not called); `TestLateEngineReplaysTheFloorGatesAtTheBind` re-expressed over `pendingGeneration`; the other `wire_engine_test.go` replays green as they are; `TestLateEngineRemembersSettingsMovedBeforeTheBind` re-expressed over `pendingGeneration`; the two order/skip tests use the recording `Replace` fake; a `SetReactions` on an engine with no Runner does not panic.

**Acceptance:**
```
go test ./cmd/apogee/ -run 'LateEngine|SetReactions|Firing'
```

**Commit:** `refactor(apogee): the Driver applies one Generation to the engine and the Runner`

## 12. Settings rows and live reload on one generation — ✅ DONE (2026-09-08)

NOTES (2026-09-08): the `reactions` row's `reaches` is the engine, the Runner AND the holder, where the item says "the runner and holder as the `hooks` row did". The apply is now `a.engine.SetReactions(gen')` — the one door item 11 built — so the engine is dereferenced and has to be required; going straight to `Runner.Replace` instead would leave `lateEngine.observe` stale and make the next Floor toggle retire a Runner generation for a list that never moved. The Runner requirement is kept as the item asks: it is the half that actually fires, and a Driver without one would report an edit that armed nothing.

NOTES (2026-09-08): `setFloorGuard` reads the seven keys back through `optionsFromFloor(s.gen.Floor)` rather than through `optionsLocked()`, so the flip costs no projection of the whole snapshot (servers, maps and all) and `optionsLocked`'s "one caller" contract stands unchanged. `floorFromOptions` is still the negation seam on the way back.

NOTES (2026-09-08): `liveSettings.setHooks` is `setObserve` (it writes the generation's observe half now), which renames its two references in `cmd/apogee/schedule.go:123` and `cmd/apogee/schedule_test.go:1357,1388` — neither file is in the item's list, but the method has no other callers and both are its own name in prose and in code.

NOTES (2026-09-08): `TestApplySettingFloorGuardKeysCarryTheOtherFiveGates` was RENAMED to `TestFloorRowAppliesOneGeneration` and re-expressed over `spy.generations` — the same three claims (six gates stand, the negation, the projection) plus the two the generation added (Bypass and the observe list ride unchanged). `TestApplySettingDrivesTheRightEngineSeam`'s `bypass` row now reads `spy.generations`, and `TestApplySettingRefusesWhatItCannotApply` composes a holder so its `bypass` case still refuses for the VALUE rather than for unreachability.

NOTES (2026-09-08): the two reactions-reload tests drive the real `lateEngine` (`newLateEngine` + `seedReactions`) instead of composing an applier with no engine, which is what makes "the runner fired the reloaded entry" a claim about the shipped wiring rather than about a second path into the same Runner.

NOTES (2026-09-08): `cmd/apogee/wire_boot.go` is in the item's Files list and needed no edit — its `Floor: floorFromOptions(w.opts)` and `Bypass: w.opts.Bypass` are the Config's, not the holder's, and `wire_live.go` already seeds the engine holder with the same three values `newLiveSettings` now seeds the generation from.

NOTES (2026-09-08): `settingsrows.go` needed no edit either — the `reactions` row's read-only/⏎-opens-$EDITOR affordance falls out of `externallyEdited(k)` over the registry row item 8 added, and the section map already places it under Reactions. `TestReactionsRowOpensTheEditorOnTheKey` pins both ends (the row advertises the editor; `settingKeyLine` lands on the `reactions:` block).

**What:** Depends on items 9 and 11. `liveSettings` holds `gen domain.Generation` in place of `floor`, `bypass` and `hooks`; the seven Floor rows and the `bypass` row compute `gen'` from the pane value (`floorFromOptions` stays the negation seam) and call `a.engine.SetReactions(gen')` — `reachesTheEngineAndTheHolder` loses its floor justification and is removed if nothing else uses it; `reloadHooks` becomes `reloadReactions`: `LoadFileConfig` → `gen' = gen with Observe = file.Reactions` → one apply; the `reactions` row's `apply` calls it, `reaches` requires the runner and holder as the `hooks` row did. Firing composition (`:858`) projects `s.gen.Observe`. Pane wiring for the `reactions` row: read-only, `externallyEdited` → "⏎ opens $EDITOR" via the existing `externalEdit.spec` (A3 — the machinery exists). Section map: `reactions` under Reactions.

**Regression guard.** `floorFromOptions` (`wire_settings.go:783-793`, one-way today) gains its inverse `optionsFromFloor` (the seven negations), so `optionsLocked` (:858-870) projects `s.gen.Floor` and `s.gen.Bypass` onto the seven `Options` keys and `bypass` for every Firing. `reachesTheEngineAndTheHolder` is NOT removed — it is the `reaches` of both `context-files.` rows (:1215, :1231) — and the `bypass` row's `reaches` moves from `reachesTheEngine` (:1515) to it, since `gen'` is computed from the holder; the predicate's comment gains the bypass row.

**Files:** `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_settings_test.go`, `cmd/apogee/wire_boot.go`, `cmd/apogee/settingsrows.go`, `cmd/apogee/settingsrows_test.go`, `cmd/apogee/settingsedit_test.go`.

**Tests:** the hook reload/refusal/reaches suite (`wire_settings_test.go:2859-3163`) rewritten over `reactions`; `TestFloorRowAppliesOneGeneration` (spy sees one `SetReactions` per toggle, the other six bits unchanged); `TestBypassRowAppliesOneGeneration`; `TestReactionsRowReloadSwapsObserveOnly` (Floor and Bypass bits unchanged across a file reload); `TestReactionsRowOpensTheEditorOnTheKey` (the `settingKeyLine` target is `reactions:`); `optionsFromFloor(floorFromOptions(o))` round-trips the seven keys and the Firing composition carries them; the `bypass` row joins the reaches suite's holder-required cases; `TestSettingSectionsOpenInRegistryOrder` green.

**Acceptance:**
```
go test ./cmd/apogee/ -run 'Settings|Reactions|Floor|Bypass|Reload|Section'
```

**Commit:** `refactor(apogee): the Floor, bypass and reactions rows swap one Generation`

## 13. Delete `SetBypass`, `SetFloor` and their callers — ✅ DONE (2026-09-08)

NOTES (2026-09-08): `lateEngine.generation()` is deleted too. The two wrappers were its only callers, and the settings rows read their base from the HOLDER (`liveSettings.generation()`, `setBypass`, `setFloorGuard`) — so the item's "`floorConfig()`/`bypassEnabled()` readers if unused" rule reaches it. `bypassEnabled()` itself stays: `bypassSkips` still reads it every Moment. `floorConfig()` was already gone at item 10.

NOTES (2026-09-08): the eight agent-side call sites share one `swapBypass(a, on)` helper in `setlive_test.go` (read `Generation()`, edit the copy, `SetReactions`) rather than eight inline read-modify-writes — the item's "re-expressed over `SetReactions`/`Generation()`" in one idiom. The two test FUNCTION names still read `TestAgentSetBypass…`: renaming existing identifiers is not this item's, and neither name matches the acceptance grep.

NOTES (2026-09-08): `applySettingSpy` loses its `bypass` and `floors` fields with the two methods, and `drove()` counts `generations` instead. Nothing read them: item 12 already moved the `bypass` row and the seven Floor rows onto `SetReactions`, so the dispatcher had stopped driving either seam.

NOTES (2026-09-08): `internal/agent/floorguards.go` lost its only `domain` reference with `SetFloor`, so the import goes with it; the file is now the guard keys, the action labels and `guardIDs`.

NOTES (2026-09-08): `internal/agent/floorguards_test.go`, named in the item's known-site list, needed no edit — it never called either setter (verified by the acceptance grep, which returns nothing repo-wide).

NOTES (2026-09-08): consequential edit — internal/agent/doc.go: made necessary by deleting `SetFloor` (the file map called floorguards.go "the Floor half of the live-generation swap").

NOTES (2026-09-08): consequential edit — internal/agent/reactions.go: made necessary by deleting `SetBypass` (`bypassSkips`' doc said a mid-session `SetBypass` lands at the very next Moment).

NOTES (2026-09-08): consequential edit — cmd/apogee/wire_settings.go: made necessary by deleting `SetFloor` (the seven Floor rows' comment said "SetFloor takes the WHOLE FloorConfig"; it is `SetReactions` and the whole Generation).

NOTES (2026-09-08): the same false-comment sweep inside the item's own files: `agent.go` (the anytime-safe class list and `SetReactions`' doc), `wire.go` (the `settingsEngine` doc), `wire_engine.go` (the `pendingGeneration` field doc, `seedReactions`' partial-edit paragraph, and the three "for SetBypass's reason" back-references on `SetScratchDir` / `SetCompactionEnabled`), `setlive_test.go`'s header and `wire_engine_test.go`'s Floor-replay doc (a second swap now replaces the first whole rather than merging into it).

NOTES (2026-09-08): the first `go test ./cmd/apogee/` of this tree reported FAIL, and its failing test name was lost to a `tail`. Two further full runs of the identical tree — one cached-clean, one `-count=1` — passed in 133.9s, and the item's changes to that package are an interface shrink plus test rewrites with no behaviour in them. Recorded as an unidentified one-off flake, not a finding.

**What:** Depends on item 12. Delete the wrappers from `internal/agent`, `floorConfig()`/`bypassEnabled()` readers if unused, the two methods from `settingsEngine`, and retype every remaining caller — rule: `grep -rn 'SetBypass(\|SetFloor(' --include=*.go .` returns nothing after the item. Known sites: `internal/agent/{setlive_test,reactions_test,routedspawn_test,wire_test,floorguards_test}.go`, `cmd/apogee/{wire_settings_test,wire_helpers_test,wire_engine_test}.go`.

**Files:** `internal/agent/agent.go`, `floorguards.go`, and the files the grep names.

**Tests:** existing tests re-expressed over `SetReactions`/`Generation()`; no new behaviour.

**Acceptance:**
```
! grep -rn 'SetBypass(\|SetFloor(' --include=*.go .
go test -race ./internal/agent/ && go test ./cmd/apogee/
```

**Commit:** `refactor(agent,apogee): SetBypass and SetFloor are gone; SetReactions is the one live swap`

## 14. Delete the validated-sets surface — Driver side — ✅ DONE (2026-09-08)

NOTES (2026-09-08): the item removes the two `validated-sets.` rows from `settingsTable` while item 15 still owns the `KeyRegistry` rows, which leaves `validated-sets.enable` an editable key with no apply — `TestEveryEditableSettingKeyHasAnApply` and `TestApplySettingRefusesEveryKeyItCannotReach` both fail that way, and the item's own acceptance demands `go test ./cmd/apogee/` green. The two rows therefore SURVIVE for one more item, re-pointed at `reachesWithoutAMember` + `applyTheWriteAlone`: no holder field, no re-read, no `ValidatedSets` identifier (the acceptance grep holds), and the pane's write is the whole of the apply now that nothing reads the key. Item 15 deletes the rows with the schema.

NOTES (2026-09-08): the two keys therefore join `settingKeysWithNoMemberToReach` in `cmd/apogee/wire_settings_test.go` (with a sentence saying why and for how long), and `settingsrows_test.go` keeps its section-map and value-map pins for them — the value pins are `"false"` / `noneSettingValue`, since the fixture may no longer set `Options.ValidatedSets*` under this item's acceptance grep.

NOTES (2026-09-08): the item's acceptance grep (`! grep -rn 'ValidatedSets' cmd/apogee/`) reaches `wire_settings_test.go`'s two `Options.ValidatedSetsAlias` sites, which item 15's guard assigns to item 15. Both are dead once the holder stops projecting the field, so they are removed here; item 15 will find nothing left there.

NOTES (2026-09-08): the item lists `daemonfire_test.go` and `wire_live_test.go` in Files; neither names the surface at `d84dd988` (`daemonfire_test.go:107` reads "one validated schedule entry", an unrelated use of the word), so neither is touched.

NOTES (2026-09-08): `schedule_test.go`'s `TestScheduleFiringRunsAgainstTheCurrentBinding` loses its whole endpoint-keyed tail — the probe-record fixture, the user entry and the `firingConfig` re-composition — because the notice pair it read (`skipping validated-set entry` vs "the model identity is name-only") was the surface's. The record's `meta.Model` assertion still holds the "the Firing follows the holder, not the launch snapshot" claim, and the test's doc comment now says so.

NOTES (2026-09-08): `wire_server_test.go` loses `TestRebindResolutionKeysOnTheBoundEndpoint` whole for the same reason — its only observable was the set notice — and `TestRebindSpecForSelectsPerModelBindings` loses its two set cases and the `seedEntryKey` column.

NOTES (2026-09-08): `probemodel_test.go` loses six tests plus `writeUserValidatedEntry` (every one of them drives `resolveValidatedSet` or `autoApplyKeys`). `internal/probe/model_test.go`'s four written-row cases collapse to one that pins the surviving effect line, which is the item's "ends at resolves at medium confidence".

NOTES (2026-09-08): `recordProbeFingerprint` drops its now-unused `opts config.Options` parameter (dead argument once `autoApplyKeys` is gone); one call site.

NOTES (2026-09-08): consequential edit — cmd/apogee/doc.go: made necessary by deleting validatedsets.go (the package map named the file).

NOTES (2026-09-08): consequential edit — cmd/apogee/modelprofile.go: made necessary by deleting validatedsets.go (the file-top comment sited itself beside it).

NOTES (2026-09-08): consequential edit — cmd/apogee/probemodel.go: made necessary by deleting `autoApplyKeys` — `apogee probe model`'s `Long` help text and the command's doc comment promised the promotion this item removes, so the user-facing claim would have shipped false.

NOTES (2026-09-08): consequential edit — cmd/apogee/modelprofile_test.go: made necessary by removing `stateRoots.validated` (two `stateRoots{…}` literals name the field).

NOTES (2026-09-08): the comments at `internal/probe/modelfingerprint.go`, `internal/probe/doc.go` and `internal/probe/battery.go` still name a Validated set; item 15's guard owns that sweep and it is not done.

NOTES (2026-09-08): one of four `go test ./cmd/apogee/` runs on the finished tree reported FAIL with a streaming-TUI frame in the output; the failing test's name was lost to the `tail` in that invocation and three further full runs (two with `-count=1`) are green. Recorded as observed flakiness in the cmd/apogee TUI e2e suite, not attributed to this item — nothing this item touches runs a live spinner.

NOTES (2026-09-08): fix-retry — the unused test helpers `noticeContains` and `mustTime` (orphaned when the six validated-set tests in `probemodel_test.go` went) are deleted with their doc comments; the `time` import stays, still used by the timestamp fixtures.

**What:** Depends on item 9. Remove every consumer of `internal/validated` and `Options.ValidatedSets*` from `cmd/apogee`: `validatedsets.go` and its test whole; `probemodel.go`'s `autoApplyKeys` and the set decision (:250, :272-323); `wire_live.go` (:169-182, :284); `wire_verbs.go` (:26, :56); `wire.go` `roots.validated`; `wire_settings.go` (holders :157-160, :276-277, :686-695, :886-887, :970-971, rows :1531-1541, `reloadValidatedSets` :2070-2080, :2199, :2224, :2240, :2251); the tests naming them (`settingsedit_test.go:389-423`, `settingsrows_test.go`, `schedule_test.go:136-239`, `probemodel_test.go`, `daemonfire_test.go:107`, `wire_live_test.go`) and the `docs_settings_test.go` false-positive rows for `validated-sets.*`. The config package still carries the key until item 15, so `go build` holds.

**Regression guard.** the `docs_settings_test.go` false-positive rows for `validated-sets.*` are replaced by a leaf this plan never deletes (`mcp-servers` or another surviving `KeyRegistry` path) so the table still holds after item 15. `wire_firing_test.go:42` sets `stateRoots.validated` and `wire_server_test.go:262-264,331` use `roots.validated` with `writeLabEntry`/`labKey`/`labSet` from the deleted `validatedsets_test.go:25-30,393`: both files join Files, `firingRoots` loses its `validated` line and the "validated-set surface is re-resolved" cases (`wire_server_test.go:191-215, 325-365`) go. With `autoApplyKeys` gone `SaveOutcome.AutoApply`/`Promoted`/`Suppressed` are never set and every medium-confidence `apogee probe model` would print the "would AUTO-APPLY (ADR 0016 §5)" line about a surface that no longer exists (`internal/probe/model.go:276-278`): delete the three fields and the three set branches of `effectLine` (`model.go:78-93, 270-287`) plus the rows at `model_test.go:110-131`, so the effect line ends at "resolves at medium confidence".

**Files:** `cmd/apogee/validatedsets.go` (deleted), `validatedsets_test.go` (deleted), `probemodel.go`, `probemodel_test.go`, `wire_live.go`, `wire_live_test.go`, `wire_verbs.go`, `wire.go`, `wire_settings.go`, `wire_firing_test.go`, `wire_server_test.go`, `settingsedit_test.go`, `settingsrows_test.go`, `schedule_test.go`, `daemonfire_test.go`, `docs_settings_test.go`, `internal/probe/model.go`, `internal/probe/model_test.go`.

**Tests:** the removed rows vanish from the section and value maps; `TestManualDocumentsEverySettingsKey`'s false-positive rows name a surviving leaf; `effectLine`'s table (`model_test.go`) ends at "resolves at medium confidence" with no set rows; the Firing and server suites green without `roots.validated`; probe/schedule/daemon suites green without the set decision.

**Acceptance:**
```
! grep -rn 'internal/validated\|ValidatedSets' cmd/apogee/ && ! grep -n 'AutoApply\|Promoted\|Suppressed' internal/probe/model.go
go test ./cmd/apogee/ ./internal/probe/
```

**Commit:** `refactor(apogee): the validated-sets surface leaves the Driver`

## 15. Delete the validated-sets surface — config, package and manual — ✅ DONE (2026-09-08)

NOTES (2026-09-08): the `validated-sets:` strip is added to `configmigrate.go` in this item, as the item's own guard directs (item 9 shipped the `hooks:` fold and the `mechanisms:` strip only). It follows the `mechanisms:` idiom exactly: a `validatedSetsKey` const, a `validatedSets *blockSpan` on `legacyReactionsConfig`, a drop in the single-pass splice, and a `strippedValidated` bool on `reactionsFold` that adds the note fragment. Per the round-2 guard `validated-sets` is NOT passed to `sameApartFrom` — `fileConfig` has forgotten the key, so the comparison is already blind to it and the byte-level span is the strip's only reader; the verify's comment says so.

NOTES (2026-09-08): two migration strings now name the third key they act on, because a file carrying only `validated-sets:` can reach both and the old wording would have been false there: `verifyReactionsFold`'s refusal became "changed more than hooks:, reactions:, mechanisms: and validated-sets:" (its pin in `configmigrate_test.go` moved with it), and `reactionsRefusal` now names `validated-sets:` in its sentence and in its by-hand instruction. No test pinned the second.

NOTES (2026-09-08): `TestLoadFileConfigLeavesTheMechanismsKeyToStartup` became `TestLoadFileConfigLeavesTheRetiredKeysToStartup`, a two-case table — the live-re-read rule is now about both retired keys, and the `mechanisms:`-only name would have under-claimed it.

NOTES (2026-09-08): the item's "a file without it untouched" test case is `TestMigrateLegacyConfigLeavesAModernFileAlone` (new): a file carrying `reactions:` and none of the retired keys comes back byte-identical, unannounced, with no backup.

NOTES (2026-09-08): consequential edit — internal/config/defaults/config.yaml: made necessary by deleting the Validated-sets template block — the `mechanisms:` block's `CAVEAT (ADR 0016)` paragraph said a non-empty `mechanisms:` block suppresses a Validated set and pointed "see below" at the section this item deletes, so it would have shipped false and dangling. The `mechanisms:` block itself is item 16's.

NOTES (2026-09-08): consequential edit — internal/config/configwrite_test.go: made necessary by deleting the template block — the seeded-template assertion pinned `# validated-sets:`; it now pins `# model-profiles:`, a line this plan does not touch (the item's own Tests section asks for exactly this re-pin).

NOTES (2026-09-08): the comment sweep ran under the guard's grep (`internal/validated|Validated.set`). Beyond the files the item names it reached `cmd/apogee/wire_firing.go`, `cmd/apogee/probemodel_test.go`, `internal/domain/fingerprint.go`, `internal/library/{doc.go,fingerprint.go,fingerprint_test.go}` and `internal/probe/doc.go` — each named a Validated set as a live keying surface for the fingerprint label, and each now names what still keys on it (per-model settings, Library observations). `internal/mechanisms/retired.go`'s three hits are left: item 16 deletes that package whole and is not yet done.

NOTES (2026-09-08): item 14's deferred handover is closed here as the DECISION directs — the two `validated-sets.` rows in `cmd/apogee/wire_settings.go`, their entry in `settingKeysWithNoMemberToReach` (`wire_settings_test.go`) and the `settingsrows_test.go` section/value/external-edit pins all go with the schema rows; `applyTheWriteAlone`'s comment loses the paragraph explaining them.

**What:** Recast at the regression check (2026-09-08). Depends on item 14. Delete `fileConfig.ValidatedSets`, `validatedSetsConfig`, the two accessors (`config.go:910-925`), `Options.ValidatedSetsEnable`/`ValidatedSetsAlias`, the two `KeyRegistry` rows (:661-677), the template block (`defaults/config.yaml:961-985`), and `internal/validated/` whole (including `shipped.json`). `docs/manual/configuration.md`'s `## Per-model validated sets — validated-sets:` section (:803-843) is deleted and the retired-key block (:77-105) gains one sentence: the migration strips `validated-sets:` and says so. Closes bead `apogee-jwf` (closeout).

**Regression guard.** takes over from item 9: the migration strips `validated-sets:` (shadow field, `sameApartFrom` path `validated-sets`, the note fragment `; the inert validated-sets: key was dropped`, its inline-YAML tests) in the same item that deletes the rows and the surface; Files gain `internal/config/configmigrate.go` and `configmigrate_test.go`; item 15 does NOT touch `docs/manual/configuration.md:77-105` (item 16 owns that block) — its manual work is only deleting §803-843. That section runs to :847 (`:844-846`, "an entry under `~/.apogee/validated/` still resolves", is its tail — delete it with the heading); the manual's other Validated-set mentions (`configuration.md:196`, `probe.md:39-42`, `commands.md:399`) are item 19's, under the rule its guard states. `configwrite_scalar_test.go:295` calls `mustKey("validated-sets.enable")` (a panic once the row is gone) and `configwrite_test.go:85` pins `"# validated-sets:"` in the seeded template: delete `TestSpliceScalarSettingWritesTheValidatedSetsOffSwitch` and swap the seed assertion for a surviving template line. `cmd/apogee/wire_settings_test.go:809,1017` and `settingsrows_test.go:67,656` set or clear `Options.ValidatedSetsAlias` — both join Files. The acceptance grep cannot forbid the spelling `validated-sets` outright: `configmigrate.go`'s strip note spells it by design, and the comments at `internal/probe/model.go:88,280`, `model_test.go:123`, `battery.go:45`, `modelfingerprint.go:55`, `config.go:190,2206` are swept here.

**Regression guard (round 2; owner, 2026-09-08, re-check round — supersedes the `sameApartFrom` path above).** Do NOT pass `validated-sets` to `sameApartFrom` — a key `fileConfig` no longer reads is invisible to the comparison and the byte-level shadow field alone drives the strip; the comment sweep becomes a rule plus grep (every comment naming `internal/validated` or a Validated set as a live sibling surface: `grep -rn 'internal/validated\|Validated.set' --include=*.go .`) and Files gain `internal/profiles/doc.go`.

**Files:** `internal/config/config.go`, `options.go`, `registry.go`, `registry_test.go`, `config_test.go`, `configmigrate.go`, `configmigrate_test.go`, `configwrite_scalar_test.go`, `configwrite_test.go`, `defaults/config.yaml`, `internal/validated/*` (deleted), `internal/probe/model.go`, `model_test.go`, `battery.go`, `modelfingerprint.go`, `internal/profiles/doc.go` and every file the comment grep names, `cmd/apogee/wire_settings_test.go`, `cmd/apogee/settingsrows_test.go`, `docs/manual/configuration.md`.

**Tests:** `TestRegistryIsBijectionWithFileConfig`, `TestKeyAccessorsBindDescribedKeys` green with the rows gone; `TestSpliceScalarSettingWritesTheValidatedSetsOffSwitch` deleted, the `configwrite_test.go:85` seed assertion re-pinned on a surviving line; inline-YAML cases: `validated-sets:` stripped, its note fragment pinned, a file without it untouched; `docs_settings_test` green.

**Acceptance:**
```
test ! -d internal/validated && ! grep -rn 'ValidatedSets\|internal/validated' --include=*.go . && ! grep -rln 'validated-sets' --include=*.go . | grep -v configmigrate
go test ./internal/config/ && go test ./cmd/apogee/ -run 'Docs|Settings'
```

**Commit:** `refactor(config)!: validated-sets: and internal/validated are deleted (ADR 0076 A9)`

## 16. Delete `mechanisms:` and `internal/mechanisms` — ✅ DONE (2026-09-08)

NOTES (2026-09-08): `newSubAgentServer` lost its `error` return — the mechanisms validation was its whole body, so every remaining caller's `if err != nil` would have been dead. The cascade stops there: `newDelegationWiring` and `delegationWiring.relist` keep their `error` returns (their callers, `wire_live.go` and `wire_settings.go`'s `reloadServers`, are the live validate-then-commit seam), and their doc paragraphs are rewritten so neither still claims the deleted failure mode. `newDaemonWiring`'s `[]string` return WAS dropped, because it carried nothing but the retired notices.

NOTES (2026-09-08): `TestApplySettingServersDrivesTheSubAgentServer`'s `libary: true` case (`delegation_test.go`) IS deleted this time, unlike at item 9 — with `ServerEntry.Mechanisms` gone the key is simply an unknown field yaml ignores, so the apply no longer refuses and the case pinned nothing.

NOTES (2026-09-08): `verifyReactionsFold` takes the round-2 guard's shape verbatim: `withoutServerMechanisms` is deleted, the servers are compared with plain `sameServers` and `serversKey` joins `reactionsKey` in the `sameApartFrom` call — `mechanisms` had to leave that list because `zeroConfigPath` answers false for a path the schema no longer has, which would have failed every fold.

NOTES (2026-09-08): `TestApplyConfigNoMechanismsIsNil` is deleted (nothing left to be nil) and `TestMechanismsKeyIsStrippedAndHasNoRegistryRow` now reads the rewritten file back instead of asserting `opts.Mechanisms`, which is the only claim the deleted field left it able to make.

NOTES (2026-09-08): the manual's migration block is named `## Keys apogee migrates for you` and every surviving `mechanisms` mention in `docs/manual/configuration.md` lives inside it, bar the ADR 0006 link line the acceptance excludes.

NOTES (2026-09-08): consequential edit — CONTEXT.md: made necessary by deleting the key and the package — the `servers:` posture sentence (:287), the **Mechanism** retired-term entry and the retired-roll paragraph each named `mechanisms:` or `internal/mechanisms/retired.go` as live. Item 20 owns CONTEXT.md's Reaction/Moment/Hook-event/Validated-set entries and header prose; none of those three is on its list.

NOTES (2026-09-08): consequential edit — cmd/apogee/wire.go, cmd/apogee/wire_tools.go, cmd/apogee/wire_verbs.go, cmd/apogee/wire_settings.go, internal/agent/subagent.go, internal/config/configmigrate.go: made necessary by deleting the key and its validation — each carried a doc comment describing `mechanisms:` validation, the retired-roll notices or "`mechanisms:` still parses" as current behaviour.

NOTES (2026-09-08): historical `internal/mechanisms` mentions that describe where code USED to live (`internal/syntaxcheck`, `internal/domain/domaintest`, `internal/tools/exec_fence_test.go`, `internal/library/doc.go`, `internal/floor/toolnames.go`) are left as written, which is what the item's Tests line allows; `internal/floor` is untouched under the plan's standing requirement. Stale mentions of the deleted registry in `internal/tui/doc.go:566` and `tui.go:299` predate this item (stage 1 deleted the registry) and belong to item 21's comment sweep.

**What:** Recast at the regression check (2026-09-08). Depends on items 9 and 14. Delete `fileConfig.Mechanisms`, `ServerEntry.Mechanisms` (making `ServerEntry` comparable — replace the `reflect.DeepEqual` in `configmigrate.go:238` and `configedit.go:209` with `==` where the comment says that was the only reason), `Options.Mechanisms`, the registry-free accessor (`config.go:894-908`), the `walkSchema` and `TestKeyAccessorsBindDescribedKeys` exemptions, the template mentions (`defaults/config.yaml:212`, `:590` comments), and `internal/mechanisms/` whole (call: package deleted) with its four start-up notice call sites in `daemonfire.go:120`, `delegation.go:346`, `headless.go:693`, `wire_live.go:160` and `delegation.go:858`'s per-seat note. `docs/manual/configuration.md`'s per-seat `mechanisms:` mentions (:1013-1041) and `docs/manual/headless.md:52`, `docs/manual/daemon.md:108-109` lose the key.

**Regression guard.** keep `reflect.DeepEqual` at all three `ServerEntry` comparison sites (`configmigrate.go:238`, `configedit.go:209`, `sameServers` `configmigrate.go:869-871`) — `Bypass *bool` makes `==` an address compare — and rewrite only their comments; Files gain `cmd/apogee/wire_helpers_test.go`, `daemon_test.go`, `headless_test.go`, `wire_live_test.go`, `wire_firing_test.go`, `delegation_test.go`, `probemodel_test.go`, with `retiredMechanismNotice`, the three retired-notice tests, the `armed`/`defective` arms of `TestFiringConfigResolvesItsSubAgentSeat` and the `Mechanisms` assertions in `delegation_test.go` deleted; the acceptance grep excludes `internal/config/configmigrate.go` (the shadow struct may tag `mechanisms`); the manual guard is a rule plus grep — `grep -n 'mechanisms' docs/manual/configuration.md docs/manual/headless.md docs/manual/daemon.md` — and item 16 owns `docs/manual/configuration.md:77-105` wholesale, rewriting it as "keys apogee migrates for you" (the `hooks:` fold, the `mechanisms:` strip with the successor hint, the `validated-sets:` strip) plus `:931`; the ratified call "`internal/mechanisms` deleted whole" gains "its successor table moves into `configmigrate.go` (item 9), which is what ADR 0076 A6 and greenfield §9.1 row 11 ask for".

**Regression guard (round 2; owner, 2026-09-08, re-check round).** In the same commit that deletes `fileConfig.Mechanisms`, drop `mechanisms` from item 9's `sameApartFrom` paths and compare servers with plain `sameServers` (configmigrate.go:871) — the shadow struct stays the strip's only reader, and item 9's inline-YAML fold/strip cases re-run under this item's acceptance; the manual acceptance excludes the ADR 0006 link line (`grep -v '0006-bypass-mode-is-the-mechanisms-off-floor'`) and identifies the migration block by its heading text, never a line range; the retired-notice test list is corrected — item 9 already deletes the headless and daemon ones, item 16 deletes only `wire_live_test.go:293`'s and `retiredMechanismNotice` (`wire_helpers_test.go:28`). `config.go:1477-1483` and `configuration.md:77-78` record the key as kept and tolerated (ADR 0076 D11) — superseded by A6, as the ratified call names.

**Files:** `internal/config/config.go`, `options.go`, `configmigrate.go`, `configedit.go`, `registry_test.go`, `config_test.go`, `defaults/config.yaml`, `internal/mechanisms/*` (deleted), `cmd/apogee/daemonfire.go`, `delegation.go`, `headless.go`, `wire_live.go`, `wire_helpers_test.go`, `daemon_test.go`, `headless_test.go`, `wire_live_test.go`, `wire_firing_test.go`, `delegation_test.go`, `probemodel_test.go`, `docs/manual/configuration.md`, `docs/manual/headless.md`, `docs/manual/daemon.md`.

**Tests:** existing config and cmd suites green with `retiredMechanismNotice`, the retired-notice test at `wire_live_test.go:293`, the `armed`/`defective` seat arms and the `delegation_test.go` `Mechanisms` assertions deleted; `grep -rn 'mechanisms' --include=*.go .` hits only comments that describe history (the retired term) and the migration's shadow struct, none a live key; the manual grep hits only the rewritten migration block (found by its `## Keys apogee migrates for you` heading) and the ADR 0006 link line; item 9's inline-YAML fold/strip cases re-run green.

**Acceptance:**
```
test ! -d internal/mechanisms && ! grep -rn 'yaml:"mechanisms' --include=*.go . | grep -v 'internal/config/configmigrate.go'
! grep -n 'mechanisms' docs/manual/headless.md docs/manual/daemon.md
test "$(grep -v '0006-bypass-mode-is-the-mechanisms-off-floor' docs/manual/configuration.md | grep -c mechanisms)" = "$(awk '/^## /{p=0} /^## Keys apogee migrates for you/{p=1} p' docs/manual/configuration.md | grep -c mechanisms)"
go test ./internal/config/ ./cmd/apogee/
```

**Commit:** `refactor(config,apogee)!: the mechanisms: key and internal/mechanisms are deleted`

## 17. E2E journeys: migration, the new notices, one-swap reload — ✅ DONE (2026-09-09)

NOTES (2026-09-09): `cmd/apogee/e2e_hooks_test.go` is `git mv`d to `e2e_reactions_test.go` as the item asks; the file's own prose, helper names (`hookBlockOf`, `rewriteHomeHooks`, `readHookPayloads`, …) and the three existing roots are untouched — item 21 owns that wording pass, which item 9's NOTES already recorded for this file.
NOTES (2026-09-09): journey (a)'s byte-for-byte block pins the argv `run:` UNQUOTED (`run: [sh, -c, cat >> "$APOGEE_TEST_SINK"]`) — that is what the marshaller writes for a plain scalar carrying no flow indicator, and the pin is the point: it is the block a user reads in their own file.
NOTES (2026-09-09): journey (c) needed a headless conversation with a tool call in it, which `testdata/stubllm/hooks.yaml` has not; `testdata/stubllm/reactions.yaml` is that script plus one `list_dir` call. Journey (d) stays on `hooks.yaml`, whose single request makes the one `pre-request-finished` payload unambiguous.
NOTES (2026-09-09): journey (e) is the guard's observable form — two entries writing to ONE sink, told apart by the payload's own `reaction` field, so the whole claim is the firing order `[before, after]`: a second `before` is the deleted entry still armed and a third payload is the list having grown. No engine spy, no Floor assertion; the one-swap count stays with item 12's unit spy as the guard says.
NOTES (2026-09-09): the migration note is read from `e2eSession.Output()` AFTER `Quit()` — the notice is written to the command tree's error stream from the run's own goroutine, so reading it earlier would be a race rather than an assertion.

**What:** Depends on items 7, 9 and 12. `cmd/apogee/e2e_hooks_test.go` becomes `e2e_reactions_test.go` and gains: (a) **migration journey** — a real `~/.apogee/config.yaml` under a temp home carrying `hooks:` (argv + webhook, `approval-waiting`), a top-level and a per-server `mechanisms:`, and `validated-sets:`; boot the TUI root; assert the file's `reactions:` block byte-for-byte, the `.bak-` sibling, the note text exactly as item 9 composes it in the Driver's notice channel, and that the migrated reaction fires on `exchange-finished`; (b) `approval-decided` payload with `decision` at the TUI root; (c) `post-tool-result-finished` at the headless root — the script's stdin carries the tool result text; (d) `pre-request-finished` — the payload's `value.messages` holds the user prompt; (e) editing `reactions:` and applying the row swaps the generation once (the spy counts one `SetReactions`) and the Floor bits are unchanged.

**Regression guard.** Journey (e) is an observable effect, not a spy count: the engine is built inside `newRootCommand` (`e2e_support_test.go:180`) and the row applies through `rootWiring.engine` (`wire.go:215`), which no wrapper around the `launch` closure's `eng tui.Engine` intercepts — so (e) edits `reactions:`, lets the watcher apply it, and asserts the new entry fires once on the next `exchange-finished` and the old one does not; the one-swap count stays with item 12's unit spy.

**Files:** `cmd/apogee/e2e_reactions_test.go` (moved), `cmd/apogee/testdata/stubllm/*.yaml` as needed.

**Tests:** the five journeys above, (e) in the guard's observable form; the three existing roots (TUI, headless, daemon Firing) stay.

**Acceptance:**
```
go test ./cmd/apogee/ -run 'TestE2EReactions|TestE2EHooks|TestE2EEventLinesGolden'
git diff --quiet d84dd988 -- cmd/apogee/testdata/eventlines/
```

**Commit:** `test(apogee): reactions e2e — file migration, approval-decided, seam-closing notices, one-swap reload`

## 18. Manual: `docs/manual/reactions.md` — ✅ DONE (2026-09-09)

NOTES (2026-09-09): the page's examples keep the `reactions:` shapes the item-17 journeys run (argv `run:` via `sh -c`, webhook `run:` mapping with `headers-env:`) with readable ids (`notify`, `ci-bell`) rather than the journeys' literal sink names, which are test fixtures.
NOTES (2026-09-09): the eleven notices are one table with a Depth column rather than two — the five seam-closing rows each carry the item's "full working value; only serialized when you subscribe" wording, and the per-seam `value` shapes are a second small table under The payload.
NOTES (2026-09-09): `grep -rn 'hooks.md' README.md docs/manual/` lists five sites (`README.md:225,:251`, `docs/manual/README.md:12`, `docs/manual/headless.md:153`, `docs/manual/configuration.md:419`) — all in item 19's Files list, which owns them and is not yet done.

**What:** Depends on items 7 and 9. `git mv docs/manual/hooks.md docs/manual/reactions.md` and rewrite around the shipped surface: the entry schema (`id`, `on`, `run` polymorphic, `timeout` 30s default, `workspace`, `enabled`; `advise:`/`gate:` "not yet shipped"), the eleven notices in a table (the five seam-closing ones marked "full working value; only serialized when you subscribe"), the payload keys (`reaction`, `seam`, `reactions`, `value` shapes per seam, `decision`), the env set `APOGEE_REACTION_*`, the exec/webhook posture (ADR 0073 D6 unchanged), failure notice `reaction notify (approval-requested): …`, queue/drop/live reload, and a "Migrating from `hooks:`" section quoting the note and the two script changes. The intro keeps the one colloquial line ("what other tools call a hook"). Every example is one the item-17 journeys run.

**Regression guard.** the acceptance grep runs over the file with the `## Migrating from hooks:` section removed (`sed '/^## Migrating/,$d' docs/manual/reactions.md | grep …`) — that section must name the old spellings. The link check is scoped to `README.md docs/manual/` (item 19's files): `docs/adr/0076-*.md:198,306`, `docs/design/hook-talkback-findings.md:135` and the archived plans under `docs/plans/archived/` name `docs/manual/hooks.md` as history and stay.

**Files:** `docs/manual/reactions.md` (moved from `hooks.md`).

**Tests:** none executable beyond link integrity — `grep -rn 'hooks.md' README.md docs/manual/` lists only the item-19 sites (the ADR, design and archived-plan hits are historical and exempt).

**Acceptance:**
```
test -f docs/manual/reactions.md && test ! -f docs/manual/hooks.md
! sed '/^## Migrating/,$d' docs/manual/reactions.md | grep -n 'APOGEE_HOOK\|approval-waiting\|name:\|events:'
```

**Commit:** `docs(manual): hooks.md becomes reactions.md over the reactions: surface`

## 19. Manual, README and agent guide: every other mention — ✅ DONE (2026-09-09)

NOTES (2026-09-09): the acceptance greps' exclusion range `configuration.md:(77-105)` is stale — item 16's "Keys apogee migrates for you" block landed at `docs/manual/configuration.md:139-169`. Verified with the sed-strip form the item's regression guard authorizes (`sed '/^## Keys apogee migrates for you$/,/^## Environment overrides$/d'` plus the `reactions.md` `## Migrating` strip): all three greps return zero live hits.
NOTES (2026-09-09): the widened rule grep's surviving hits are all triaged exempt — `configuration.md:147,160,393,407,1501,1584` (migration prose, an example webhook URL, git hooks), `reactions.md:16,267-298` (the ADR filename and item 18's migration section), `AGENTS.md:23,113` (git/beads hooks).
NOTES (2026-09-09): `AGENTS.md` carried no retired-name mention; its one edit adds `reactions` to the `docs/manual/` page list, which item 18's new page had left stale.
NOTES (2026-09-09): three edited paragraphs were re-wrapped to the file's own column width (`docs/manual/probe.md`, `docs/manual/configuration.md`, `docs/manual/commands.md`) so no line is left orphaned mid-sentence; no other text changed.

**What:** Depends on items 15, 16 and 18. Rule: every mention of `hooks.md`, the `hooks:` key, `APOGEE_HOOK`, `approval-waiting`, `validated-sets` or a live `mechanisms:` key in `README.md`, `docs/manual/**` and `AGENTS.md` is rewritten or removed — `grep -rn 'hooks\.md\|hooks:\|APOGEE_HOOK\|approval-waiting\|validated-sets\|mechanisms:' README.md AGENTS.md docs/manual/` finds them (git hooks in `AGENTS.md` are exempt). Known: `README.md:223-225, :251`; `docs/manual/README.md:12`; `docs/manual/commands.md:411-413` (editor-row list: `reactions:` replaces `hooks:` and `validated-sets: alias:`); `docs/manual/headless.md:156, :214`; `docs/manual/configuration.md` intro links and `:837, :843`.

**Regression guard.** the acceptance grep excludes `docs/manual/configuration.md`'s retired-key block (:77-105 as rewritten by item 16) — run it as `grep … README.md docs/manual/ | grep -v 'configuration.md:\(7[7-9]\|8[0-9]\|9[0-9]\|10[0-5]\):'` or by stripping that section with sed — and the rule text says migration prose is exempt; `mechanisms:` is added to the item's grep with the same exemption. The other migration prose is `reactions.md`'s `## Migrating from hooks:` section (item 18's guard), checked with the same sed strip. The rule grep is widened to `grep -rni 'validated[ -]set\|\.apogee/validated\|\bhooks\b' README.md docs/manual/` and every hit is triaged: `docs/manual/configuration.md:196-197` ("still fires your Hooks … The Validated set your bound model would otherwise be given is not applied either") is rewritten as Bypass without the Validated-set sentence, `docs/manual/probe.md:39-42` loses the promoted-set sentence, and the editor-row list in `docs/manual/commands.md` is `:399` at base, not `:411-413`.

**Files:** `README.md`, `AGENTS.md`, `docs/manual/README.md`, `docs/manual/commands.md`, `docs/manual/headless.md`, `docs/manual/configuration.md`, `docs/manual/probe.md`.

**Tests:** `docs_settings_test`, `docs_eventlines_test`, `docs_env_test` green; the widened rule grep is triaged to zero live hits.

**Acceptance:**
```
! grep -rn 'hooks\.md\|APOGEE_HOOK\|approval-waiting\|validated-sets\|mechanisms:' README.md docs/manual/ | grep -v 'configuration.md:\(7[7-9]\|8[0-9]\|9[0-9]\|10[0-5]\):\|^docs/manual/reactions.md:'
! sed '/^## Migrating/,$d' docs/manual/reactions.md | grep -n 'hooks\.md\|APOGEE_HOOK\|approval-waiting\|validated-sets\|mechanisms:'
! grep -rni 'validated[ -]set\|\.apogee/validated' README.md docs/manual/ | grep -v 'configuration.md:\(7[7-9]\|8[0-9]\|9[0-9]\|10[0-5]\):'
go test ./cmd/apogee/ -run 'Docs'
```

**Commit:** `docs: the manual, README and agent guide speak reactions:`

## 20. CONTEXT.md and the ADR pointer notes

**What:** Depends on item 19. `CONTEXT.md`: **Hook event** (:158-179) moves to Retired terms as a pointer to Moment (call: term retired); **Moment** (:1218-1230) lists the eleven notices with the five seam-closing ones and their full-value posture; **Reaction** (:1192-1217) gains the `reactions:` entry shape, `Generation`, and the migration sentence; **Hook** (:144-157) keeps its alias role; **Validated set** (:1800-1824) and **Curation** (:1825-1834) move to Retired terms with the ADR 0016 pointer; header line :32 ("applies Mechanisms") and any header prose naming the old vocabulary (:5-6, :10, :17, :72, :83, :87, :116 — check each) are corrected. `docs/adr/0016-*.md` `Status:` line becomes `superseded by ADR 0076 (amendment A9, 2026-09-08)`. `docs/adr/0073-*.md` D6, D8, D9 gain one pointer note each (`APOGEE_REACTION_*`, `internal/reactions`, the `reaction` payload field — stage 2 of ADR 0076), and D4 one too (the guard). `docs/design/reaction-core-greenfield.md` §9.1 rows 8, 9, 12, 13 gain a "delivered 2026-09-08 (stage 2)" note.

**Regression guard.** the acceptance grep drops `docs/adr/0073-*.md` from the forbid list — its D6 body keeps the historical `APOGEE_HOOK_*` beside the pointer note — and applies to `CONTEXT.md` only. The item yields to ADR 0073's Status-line supersession idiom (:2, a note over untouched decisions): the decision text at :55 (`approval-waiting`) and :69 (`APOGEE_HOOK_*`) stays as written. Decision 4 (:55) joins the pointer-note list beside D6, D8, D9: `approval-waiting` → `approval-requested`, six notices added — stage 2 of ADR 0076.

**Files:** `CONTEXT.md`, `docs/adr/0016-curation-is-per-model-validated-sets-keyed-by-fingerprint.md`, `docs/adr/0073-hooks-are-observe-only-driver-side-reactions-to-engine-events.md`, `docs/design/reaction-core-greenfield.md`.

**Tests:** none executable; `grep -n 'Hook event' CONTEXT.md` hits only the Retired-terms pointer and the Moment entry's one back-reference.

**Acceptance:**
```
grep -c 'approval-decided\|pre-request-finished' CONTEXT.md | grep -v '^0$'
! grep -n 'APOGEE_HOOK\|approval-waiting' CONTEXT.md
```

**Commit:** `docs(context,adr): Moment carries the notices; Hook event and Validated set retire`

## 21. Code comment and emitted-string sweep

**What:** Depends on item 20. Rule: every Go comment and every emitted string that calls a user Reaction a "hook", names the `hooks:` key, `APOGEE_HOOK_*` or `approval-waiting`, is rewritten — outside git hooks and the sanctioned subprocess door (`tools.RunHookSubprocess`, `SubprocessPermit`, which keep their names). Grep: `grep -rn -i 'hook' --include=*.go . | grep -v -i 'git hook\|RunHookSubprocess\|SubprocessPermit\|hookrun\|recoverHook'` — each hit is rewritten or left with a NOTES line saying why. Announced surface: the tool-result text `pre-tool-exec hook failed` (`dispatch.go:263`) and the fan-out equivalent become `pre-tool-exec reaction failed`; the test that drives that path pins the new string. Identifiers and file names are not in scope (a `hookFailed` field stays).

**Files:** the files the grep names (comments and strings only) plus `internal/agent/dispatch.go` and `internal/agent/dispatch_test.go`.

**Tests:** `TestDispatchReportsAPreToolExecReactionFailure` (or the existing test renamed) asserts the exact new tool-result text; full `internal/agent` suite.

**Acceptance:**
```
go test ./internal/agent/ -run 'Dispatch|PreToolExec'
go build ./... && go vet ./...
```

**Commit:** `chore: comments and emitted strings say reaction, not hook`
