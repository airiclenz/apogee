# Reaction core — stage 3: the user `advise` and `gate` cells

**Goal:** Ship the two remaining day-one user cells of the Reaction surface matrix over the stage-1 core and the stage-2 `reactions:` file: `gate:` as a stage of the Approver at `pre-tool-exec`, and `advise:` as a fenced, capped, fail-open trailer on the closing tool result with a provenance ledger whose spans never survive a resume. The advise slot is built here and handed unchanged to the context-fill notice (ADR 0077) and to the bench; the advise cell ships only after the admission arm passes.

**Date:** 2026-09-09 · **Status:** unexecuted · **Sized for:** ~200k-context host · **Base:** `7b23f9da` · **Bead:** `apogee-rxj`

**Sources:** `docs/adr/0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md` (D2–D9, A7, A8) · `docs/adr/0077-the-context-fill-notice-is-the-first-engine-advise-reaction.md` D5 (consumer of the slot) · `docs/design/reaction-core-greenfield.md` §2.3, §2.5, §7 step 3 · `docs/design/confinement-execution-contract.md` §10 · `docs/adr/0073-*.md` D6, D8 · `docs/adr/0049-*.md` §4 · `docs/adr/0009-the-ab-decision-rule.md` · `../apogee-sim/docs/plans/2026-09-08 - 01 - advise-admission-arm-pre-registration.md` (P1, P2, the verdict) · bead `apogee-rxj`.

**Ratified design calls** (owner, 2026-09-09 unless noted):
- **Slot owner:** this plan builds the D6 slot, ledger and resume strip; the ADR 0077 plan consumes them.
- **Advise Moments (day one):** `post-tool-result` and `file-changed`, both the trailer slot; `file-changed` = `post-tool-result` narrowed to a successful write-tool call, `path` set. Other Moments refused by sentence; the tail-message slot is bead `apogee-4h5`.
- **Gate protocol:** first stdout line `allow` | `deny` | `ask`; further lines are a reason for the human, never the model; empty, unparseable, non-zero exit, timeout, crash ⇒ `ask`.
- **Redaction:** new `tools.RedactSecrets(text, secretEnv)` replaces every non-empty configured secret value with `[redacted]`; advise stdout passes it before the cap and the fence.
- **Arm evidence:** the gate item passes on a dated `Verdict: pass` status line in the apogee-sim pre-registration doc; otherwise FOLLOW-UP (owner runs the arm on the host).
- **Sync handlers:** argv only for `advise:`/`gate:`; a mapping is refused by sentence; webhooks are bead `apogee-1d8`.
- **Failure:** reporter line plus `ReactionFiredEvent{Action: "failed", Detail: err}`; never an `ErrorEvent`.
- **Permit row (writer, from D8 + contract §10):** a user-origin sync reaction spawns in every mode under a `SubprocessPermit`; `Confinement` = the workspace box when `confine-to-workspace` is on and the Confiner has caps, nil when it is off; on but no caps ⇒ no permit, the handler fails (`workspace confinement is unavailable on this host`) and gate ⇒ ask, advise ⇒ nothing. Recorded as a §10.4 row (item 16).
- **Gate `allow` (writer, from D2 + ADR 0049 §4):** no effect — the mode ladder's own verdict stands; a script can say No, never Yes. Gate runs after `resolve` and before the approval cache, on both `resolve(` sites, in every mode and under Bypass.
- **Sync stdin document (writer):** `domain.SeamPayload`, JSON, same keys as `reactions.Payload` where both carry the field (`event`, `reaction`, `time`, `workspace`, `depth`, `turn`, `call_id`, `tool`, `path`) plus `arguments` (raw JSON) and `result: {content, is_error}`; env `APOGEE_REACTION_EVENT/NAME/WORKSPACE/PATH`. No argv interpolation (ADR 0073 D6).
- **Lanes in one list (writer):** `Options.Reactions` carries every reaction an entry resolves to (one entry ⇒ up to one observe, one advise, one gate, same id); `Generation` gains `Sync []Reaction`; `domain.SplitLanes` divides them; an entry's `on:` list is validated against every key it carries.
- **Defaults (D7):** advise 10s, gate 5s, observe 30s unchanged; `timeout:` applies to every reaction of its entry.
- **Ask on a delegation:** a gate `ask` — including the timeout/crash escalation — on a `resolveDelegate` verdict leaves the verdict untouched: the delegation proceeds and the inherited gate asks on the child's own tool calls, so no action runs without the human question; `deny` still refuses the delegation; the firing books `Action: "ask"` with Detail `deferred to the child's calls`.

**Regression check (2026-09-09, 7b23f9da):** five independent reviewers over items 1–16 at `7b23f9da`; items 12 and 13 SAFE, every other item amended:
- 1: guard folded — the `reaction_test.go:461-471` row flips to the advise sentence, the doc-comment sweep is a grep rule, and the `Outcome` doc comment lists a non-empty `Gate.Verdict` among what counts as acted (dispatcher term is item 7's).
- 2: guard folded — `internal/domain/doc.go` maps `advice.go`; the strip and `SetMessageContent` clamp a span past a pruned message's end.
- 3: guard folded — `internal/tools/doc.go` maps `redact.go`; the test is not parallel (`t.Setenv`).
- 4: guard folded — `exec_common_test.go`'s six call sites; the `confinement.go` Auto-only / post-response-only comments swept; the tag-parity test is `package domain_test`; the contract's §10.3 `:1107` and §10.4 `:1121-1123` posture is superseded by the ratified permit row (item 16 rewrites it).
- 5: guard folded — the three test files the `appendToolResult` signature change reaches join `**Files:**`.
- 6: guard folded — `path` and success come from `tools.WorkspaceWriteTarget` and `!p.Edit.IsError()`; the redaction test embeds the literal value.
- 7: guard folded — an `ask` on a delegation yields to D3/ADR 0013 (`resolution.go:242-245`, `dispatch.go:388-390`) and refuses; the `ask` upgrade mutates the resolved verdict in place; gate reactions fire in `applyGates` alone; the nil-Approver sentence replaced by the unattended-denier outcome.
- 8: recast — the sync gate reactions are iterated by `applyGates` alone, never by `fireLeg`; one route per Driver; the `cmd/apogee` replay tests join the acceptance.
- 9: guard folded — the per-entry mapper is `entryReactions`; the `gate: is spelled` case at `:179` is replaced; an entry with no action key keeps the `run:` sentence.
- 10: recast — `cmd/apogee` never sets `Config.Reactions`: the TUI arms the sync half through the seeded `Generation.Sync`, the one-shot Drivers through one `SetReactions` after construction; `wire_settings.go:904` folds `s.gen.Sync` back; the `reactions` row's `Read` counts distinct ids.
- 11: recast — headless `echo ask` ends as `tool call denied by approver` (the unattended denier, `internal/run/run.go:291`); (a)/(b) run under `--format json`; every journey's temp config sets `confine-to-workspace: false`.
- 14: guard folded — the `file-changed` journey runs `--mode auto` with a `write_file` turn in `reactions.yaml`, under `--format json` and `confine-to-workspace: false`.
- 15: guard folded — the manual's real guards replace the docmap/doctext line; `internal/config/defaults/config.yaml:467-468` and `README.md:224` join the sweep.
- 16: guard folded — the sweep greps narrowed to sentences the tree carries at `7b23f9da`; ADR 0076 A7 `:286-287` stays as the stage-2 record; §10.3's "not Auto ⇒ nothing" row is scoped to the post-response permit so the new §10.4 row does not contradict it; CONTEXT.md locators corrected.
- 7 (round 2): recast — the round-one delegation refusal (`tool call denied by reaction <id> (asked; a delegation is never gated)`) is withdrawn by owner call (*Ask on a delegation*): an `ask` on a `resolveDelegate` verdict leaves it untouched, the delegation proceeds and the inherited gate asks on the child's own calls; the firing books `Action: "ask"`, Detail `deferred to the child's calls`; `deny` still refuses.
- 10 (round 2): guard folded — the one-shot Drivers have no post-construction seam (`run.Once` builds the Agent itself, `internal/run/run.go:296`; `run.Spec` has no hook), so `run.Spec` gains a `Sync` lane that `Once` applies through `SetReactions` and `internal/run` joins the item; `liveSettings.setObserve`, `reloadReactions` and `generationLocked` (`wire_settings.go:703-707`, `:2143-2151`, `:690-694`) carry both lanes; the no-`Config.Reactions` check excludes `_test.go` literals.

**Standing requirements:**
- `skills: coding-standards`
- `go build ./... && go vet ./...` green after every item; `go test ./cmd/apogee/` when the item touches `cmd/apogee`.
- `git diff --quiet 7b23f9da -- internal/floor/ ':!internal/floor/*_test.go'` stays clean.
- Never `-update` a golden; `cmd/apogee/testdata/eventlines/identity-*.txt` and `eventlines/*.jsonl` stay byte-identical; no new headless line kind (the kinds-count test stays).
- Authorized deviations land as a dated NOTES line under the item.

**Out of scope:** the context-fill notice itself (ADR 0077, its own plan) · the tail-message advise slot (`apogee-4h5`) · webhook sync handlers (`apogee-1d8`) · a `/settings` ledger surface (`apogee-d6t`) · `mcp:` handlers (`apogee-03a`) · user shape(view) (`apogee-8za`) · repo layer / adoption pin (`apogee-089`) · apogee-sim's operator surface for `Manifest.AdmissionArm` (apogee-sim work, named in its pre-registration doc) · VERSION/CHANGELOG release acts.

## 1. Domain: the sync classes take an argv handler, a gate decision and a `Generation.Sync` lane — ✅ DONE (2026-09-09)

NOTES (2026-09-09): the Sync duplicate refusal renders as `apogee: invalid reaction: the sync list names "advice" twice` — the item's literal sentence already names the id, so it follows the wrapped sentinel directly instead of taking the `%w %q:` prefix the observe-lane sentences use.
NOTES (2026-09-09): the regression guard's "the test's doc comment at :376-381 is retitled" was read as covering the name too — `TestReactionValidateAppliesTheObserveRulesToAnAsyncHandler` is now `TestReactionValidateAppliesThePerClassRulesToAnAsyncHandler`; it still matches the acceptance's `TestReaction` pattern.
NOTES (2026-09-09): `SplitLanes` drops a reaction whose class is neither observe, advise nor gate — no configured entry resolves to one and `Generation.Validate` would refuse it in either lane; the behaviour is documented on the function and pinned by its test.
NOTES (2026-09-09): the per-class refusal for a handler that serves the class at all keeps the stage-2 sentence (`run: a command or webhook reacts as class "observe", not "gate"`) and so still names observe as the served class — a webhook is the only handler a configuration can push into it, which the code comments.
NOTES (2026-09-09): consequential edit — apogee_test.go: made necessary by the per-class Validate rule; `TestNew_InvalidReaction_MatchableThroughRoot`'s doc comment claimed "the async lane reacts to notices only", now scoped to the observe class.

**What:** In `internal/domain/reaction.go`: (a) `ArgvHandler` is admitted for `ClassAdvise` and `ClassGate` — `Validate` replaces the "async handler ⇒ observe" rule with a per-class rule: observe ⇒ notices only (today's sentences unchanged); advise ⇒ every `On` Moment is `post-tool-result` or `file-changed`, else `%q: advise: reacts at post-tool-result or file-changed; %q is neither`; gate ⇒ `On == [pre-tool-exec]`, else `%q: gate: reacts at pre-tool-exec; %q is not it`; `WebhookHandler` stays observe-only with its existing sentence. (b) `Outcome` gains `Gate GateDecision` where `GateDecision{Verdict GateVerdict; Reason string}` and `GateVerdict` consts `GateAllow="allow"`, `GateDeny="deny"`, `GateAsk="ask"`; a Go `PreToolExecFunc` of class gate may set it. (c) `Generation` gains `Sync []Reaction`; `Generation.Validate` requires every Sync entry to be `OriginUser` with class advise or gate and rejects a duplicate id within Sync (`the sync list names %q twice`), while the same id may appear in both Observe and Sync. (d) `func SplitLanes(list []Reaction) (observe, sync []Reaction)` by class. `apogee.go` re-exports `GateDecision`, `GateVerdict` and the three consts. Update the stale doc comments at `reaction.go:225` ("Stage 3's MCP handler joins here") and `:348-350` (Timeout now binds argv handlers on both lanes).

**Regression guard.** The `reaction_test.go:461-471` row (an argv handler of class advise on `turn-finished`) flips to `advise: reacts at post-tool-result or file-changed; "turn-finished" is neither`; the webhook row at `:472-482` keeps today's sentence; the test's doc comment at `:376-381` ("takes class observe alone") is retitled. Doc-comment sweep rule: every comment saying an argv/async handler serves notices only, takes class observe alone, or that `Generation` carries only the observe list is rewritten — `grep -n 'notices only\|as class observe\|class observe alone\|observe list\|NOTICE Moments\|that class alone' internal/domain/reaction.go apogee.go` finds them (`reaction.go:221-229`, `:258-268`, `:291-293`, `:342-344`, `:436-437`, `:446-447`; `apogee.go:522-523`, `:541-542`). The `Outcome` doc comment at `internal/domain/reaction.go:211-214` lists a non-empty `Gate.Verdict` among what counts as "acted"; the dispatcher-side `acted()` term is item 7's.

**Files:** `internal/domain/reaction.go`, `internal/domain/reaction_test.go`, `apogee.go`.

**Tests:** table test over `Validate` pinning each new sentence and the unchanged observe sentences, the `:461-471` row flipped to the advise sentence and the webhook row unchanged; `Generation.Validate` accepts one id in both lanes and refuses a Sync observe entry and a Sync duplicate; `SplitLanes` keeps order within each lane.

**Acceptance:**
```
go build ./... && go vet ./internal/domain/ ./
go test ./internal/domain/ -run 'TestReaction|TestGeneration|TestSplitLanes'
```

**Commit:** `feat(domain): argv handlers serve the advise and gate classes; Outcome carries a gate decision; Generation gains the Sync lane`

## 2. Domain: the advice span, its fence and the resume strip — ✅ DONE (2026-09-09)

NOTES (2026-09-09): the strip is implemented ONCE, in `Message.MarshalJSON` (via the new `Message.recordContent`), rather than at two independent sites — `Conversation.MarshalJSON` marshals its messages through `Message.MarshalJSON`, so the item's two named sites are one code path; both carry a doc sentence saying so.
NOTES (2026-09-09): `internal/domain/doc.go`'s file-count sentence moves "Twenty-three files" → "Twenty-four files" alongside the new `advice.go` map entry.
NOTES (2026-09-09): `WithAdvice` ignores the caller's `Offset` and stamps it from the message's own length, and copies the ledger slice, matching `WithExtra`'s no-shared-backing contract.

**What:** New `internal/domain/advice.go`: `AdviceSpan{Reaction string; Origin Origin; Moment Moment; Turn int; Offset int}` (Offset = byte index in the message Content where the span's fence begins); `const AdviceCap = 8 << 10`; `func RenderAdvice(span AdviceSpan, text string) string` producing exactly `"\n\n[advice — reaction " + id + " (" + origin + " origin) at " + moment + ", turn " + n + "]\n" + text + "\n[end advice — " + id + "]"`; `func CapAdvice(text string) string` cutting at `AdviceCap` bytes on a rune boundary and appending `"\n[advice truncated at 8 KiB]"` when it cut. `Message` (`internal/domain/hooks.go`) gains `Advice []AdviceSpan` with `json:"-"`; `Message.WithAdvice(span, text)` appends the rendered fence to Content and records the span. The strip: `Conversation.MarshalJSON` (`hooks.go:924`) and the message wire struct at `hooks.go:143-159` write `Content[:spans[0].Offset]` for a message with spans — a session record never carries an advice span, so a resume has nothing to drop. The fence header is derived from the span, never from handler output.

**Regression guard.** `internal/domain/doc.go`'s file map names `advice.go` beside `hooks.go`/`reaction.go` (`doc.go:44-53`), or `docmap_test.go:12` fails the package. The prune (`internal/agent/prune.go:50` → `internal/context/prune.go:98`) rewrites an old tool result's Content to a shorter stub in place through `Conversation.SetMessageContent` (`hooks.go:813`) while its spans keep their Offset, so the strip clamps: `Offset > len(Content)` ⇒ the span is treated as gone and Content is written whole; `SetMessageContent` drops `Advice` when the new content is shorter than the first Offset.

**Files:** `internal/domain/advice.go`, `internal/domain/advice_test.go`, `internal/domain/hooks.go`, `internal/domain/hooks_test.go`, `internal/domain/doc.go`.

**Tests:** `RenderAdvice` byte-exact; `CapAdvice` at 8191/8192/8193 bytes and on a multi-byte boundary; a Conversation with one advised tool message marshals without the span and unmarshals to the bare content; a message without spans marshals byte-identically to today; an advised tool message whose Content is shortened through `SetMessageContent` (the prune's path) marshals without a panic and carries no `Advice`.

**Acceptance:**
```
go build ./... && go vet ./internal/domain/
go test ./internal/domain/ -run 'TestAdvice|TestConversation|TestMessage'
```

**Commit:** `feat(domain): the advice span, its provenance-derived fence, the 8 KiB cap and the session-record strip`

## 3. `tools.RedactSecrets`: an output-side value redactor — ✅ DONE (2026-09-09)

NOTES (2026-09-09): doc.go carries no redaction note at `:252` (the Network paragraph names no redaction), so `redact.go` is named at the end of the "package spine, one line each" list — the map's home for files that register no tool — with an explicit cross-reference to `network.go`'s `redactRequestURL`; `docmap_test.go` passes.
NOTES (2026-09-09): consequential edit — internal/tools/doc.go: "Twelve files register no tool" became "Thirteen", made necessary by adding redact.go to that list.
NOTES (2026-09-09): `go build ./...` over the working tree fails on another item's in-flight work (`internal/domain/reaction.go:434: undefined: servesClass`, items 1/2 running concurrently); left untouched. This item was built, vetted and tested in a throwaway `git archive HEAD` copy carrying only these three files — `go build ./... && go vet ./internal/tools/` green, `go test ./internal/tools/` fully green.

**What:** New `internal/tools/redact.go`: `func RedactSecrets(text string, secretEnv []string) string` replaces every occurrence of the current value of each named env var (via `os.LookupEnv`; empty or unset values skipped; longest value first so a prefix never leaves a tail) with `[redacted]`. No change to the terminal tool or `subprocessEnv`. Pure function, no I/O beyond the env read.

**Regression guard.** `internal/tools/doc.go`'s file map names `redact.go` (beside the `network.go` redaction note at `doc.go:252`), or `docmap_test.go:12` fails the package. The `RedactSecrets` test does not call `t.Parallel` — it uses `t.Setenv`, as `exec_common_test.go:59` does.

**Files:** `internal/tools/redact.go`, `internal/tools/redact_test.go`, `internal/tools/doc.go`.

**Tests:** two secrets where one is a prefix of the other; an unset name; an empty value; text without secrets returned unchanged (same string); not parallel (`t.Setenv`).

**Acceptance:**
```
go build ./... && go vet ./internal/tools/
go test ./internal/tools/ -run TestRedactSecrets
```

**Commit:** `feat(tools): RedactSecrets replaces configured secret values in text`

## 4. Agent: the sync-lane argv executor, its permit row and the Driver reporter — ✅ DONE (2026-09-09)

NOTES (2026-09-09): `runSyncArgv` stamps the payload's IDENTITY block itself — `reaction`, `time`, `workspace`, `depth`, `turn`, `call_id` — over whatever the caller passed, leaving the calling seam only the Moment's half (`event`, `tool`, `path`, `arguments`, `result`). That is what uses the `turn` parameter the item's signature names, and it means a seam that forgets a field cannot ship a document misreporting the run.
NOTES (2026-09-09): `reportReaction` takes `turn` as its first argument — `reportReaction(turn int, id string, m domain.Moment, err error)` — because the `ReactionFiredEvent` it books needs `a.base(turn)` for its `EventBase`, which the three-argument shape the item names cannot build.
NOTES (2026-09-09): the class defaults are pinned by a table over `syncTimeout` (`TestSyncArgvClassDeadlines`) rather than by a sleeping command, and the timeout BEHAVIOUR test sets an explicit 150ms `timeout:` — a literal class-default sleep would add 5–10 seconds to every suite run for a fact the table pins exactly.
NOTES (2026-09-09): `tools.RunHookSubprocess` had no caller-env route, so its signature gained `extraEnv []string` after `secretEnv` (appended after the scrub, so a caller's name wins over an inherited one). All six `exec_common_test.go` call sites pass `nil` and are unchanged in behaviour; a new `TestRunHookSubprocessAppendsTheCallersExtraEnv` pins the route.
NOTES (2026-09-09): `internal/domain/reaction.go` is added to FILES — the item's What places the class default consts there (`DefaultAdviseTimeout`, `DefaultGateTimeout`) but its `**Files:**` list omits the path.
NOTES (2026-09-09): the guard's comment sweep landed on both sites `grep` found — `confinement.go:148` ("refusal is the only honest answer in every mode but Auto") and `:170` ("today: post-response") now state the permit row with its two minting sites; `hookExecutionCtx`'s own doc gains one sentence scoping its table to the post-response row. The contract's `:1121` line is item 16's, as the item says.
NOTES (2026-09-09): consequential edit — internal/agent/doc.go: made necessary by adding syncexec.go (docmap.Check enumerates every file in the package).
NOTES (2026-09-09): consequential edit — internal/domain/doc.go: made necessary by adding seampayload.go (same docmap rule).

**What:** Depends on item 1. New `internal/agent/syncexec.go`: `func (a *Agent) runSyncArgv(ctx, turn int, r domain.Reaction, doc domain.SeamPayload) (stdout string, err error)` — builds the permit per the ratified permit row (new `syncPermitCtx`, beside `hookExecutionCtx` at `dispatch.go:715`, which stays Auto-only for post-response), installs `domain.WithConfinement` when the permit carries a box, applies the class default deadline (`domain.Reaction.Timeout` when set, else 10s advise / 5s gate — consts in `internal/domain/reaction.go`), and calls `tools.RunHookSubprocess(ctx, argv, "", a.cfg.WorkspaceDir, a.cfg.SecretEnvVars, timeout, json(doc))`; errors from the funnel pass through verbatim. `domain.SeamPayload` (new `internal/domain/seampayload.go`) per the ratified stdin document, with `Env() []string` producing `APOGEE_REACTION_EVENT`, `_NAME`, `_WORKSPACE`, `_PATH` — the values are appended to the subprocess env by `RunHookSubprocess`'s caller-env route (extend its signature with `extraEnv []string` if it has none; its three existing callers are in-package). `domain.Config` gains `Report func(msg string)` (nil ⇒ dropped) and `a.reportReaction(id, m, err)` formats `"reaction %s (%s): %v"` — the Runner's sentence — and books `ReactionFiredEvent{Reaction: id, Origin, Moment: m, Action: "failed", Detail: err.Error()}`. Coding standard: one deep module — the executor owns permit, deadline, spawn and reporting; callers (items 6, 7) see only `(stdout, err)`.

**Regression guard.** `**Files:**` gains `internal/tools/exec_common_test.go` (the `RunHookSubprocess` signature change reaches it): `RunHookSubprocess` has no production caller and its six call sites are that file's (`:131`, `:165`, `:191`, `:217`, `:308`, `:344`) — all six are updated. `internal/domain/confinement.go` joins `**Files:**` under the rule "every comment stating the permit is Auto-only or post-response-only is rewritten to the permit row": `grep -n 'every mode but Auto\|today: post-response\|post-response only' internal/domain/confinement.go internal/agent/dispatch.go docs/design/confinement-execution-contract.md` (`confinement.go:145-156`, `:168-170`; the contract line is item 16's). The tag-parity test is written as an external test (`package domain_test` in `seampayload_test.go`) — `internal/reactions` imports `internal/domain` (`command.go:15`), so an in-package import is a cycle. The contract's §10.3 "not Auto ⇒ nothing" row (`:1107`) and §10.4 "post-response only" (`:1121-1123`) record today's posture as intended; they are superseded by the ratified permit row (header design call, from ADR 0076 D8 `:119-122`), which item 16 records in the contract.

**Files:** `internal/agent/syncexec.go`, `internal/agent/syncexec_test.go`, `internal/agent/dispatch.go`, `internal/domain/seampayload.go`, `internal/domain/seampayload_test.go`, `internal/domain/config.go`, `internal/domain/confinement.go`, `internal/tools/exec_common.go`, `internal/tools/exec_common_test.go`, `apogee.go`.

**Tests:** `sh -c 'echo ok'` returns stdout under a nil-Confinement permit; a sleeping script times out at the class default and the failure books a `failed` firing and one reporter line; confine on + no caps ⇒ no spawn, the `workspace confinement is unavailable on this host` error; the `SeamPayload` JSON keys match `reactions.Payload`'s tags for the shared fields (an external `package domain_test` test reads both via reflection); `Env()` order and names; the six `exec_common_test.go` call sites compile and pass unchanged in behaviour.

**Acceptance:**
```
go build ./... && go vet ./internal/agent/ ./internal/domain/ ./internal/tools/
go test ./internal/agent/ -run 'TestSyncArgv|TestReportReaction' && go test ./internal/domain/ -run TestSeamPayload && go test ./internal/tools/ -run 'TestRunHookSubprocess|TestSubprocessEnv'
```

**Commit:** `feat(agent): the sync-lane argv executor runs under a permit and a class deadline and reports through the Driver`

## 5. Agent: the advise slot — spans collected at `post-tool-result`, fenced on the closing tool result — ✅ DONE (2026-09-09)

NOTES (2026-09-09): `fire` keeps its two-value signature — the collecting body is a renamed `fireCascade(ctx, m, payload) (Outcome, []advice, error)` that `fire` and `firePostToolResult` both call. Changing `fire` itself would have churned seven production call sites in `loop.go`/`dispatch.go` and seventeen in four test files the item's `**Files:**` list does not name.
NOTES (2026-09-09): the advise label and the span share ONE predicate, `isAdvice(m, r, out)` (post-tool-result + class advise + non-empty Inject), so `reactionAction` gained the reaction as a parameter — a firing booked `advise` is by construction exactly the one that contributed a span. `acted()` needed no change: its existing `Inject != ""` term already counts an advise injection as a firing without a Retry; only its doc comment now says so.
NOTES (2026-09-09): the capped text is stamped onto `out.Detail` in `fireLeg` where the span is collected, which is what `bookFiring` then emits — the item's "bookFiring sets Detail to the capped text" holds at the event, with the capping done once, beside the span it belongs to.
NOTES (2026-09-09): `internal/agent/subagent_test.go` is dropped from FILES — the regression guard listed it for the `appendToolResult`/`firePostToolResult` signature change, but it calls neither (its only mention is a comment at `:1660`); it compiles and passes untouched.

**What:** Depends on items 1, 2. In `internal/agent/reactions.go`: `fire` at `MomentPostToolResult` collects, in ladder order, one span per reaction of class advise whose `Outcome.Inject` is non-empty (engine Go handlers here; argv handlers in item 6): `type advice struct{ span domain.AdviceSpan; text string }`; `firePostToolResult` (`reactions.go:182`) returns `[]advice`. `acted()` (`:321`) counts a non-empty `Inject` as acted at this seam without Retry; `reactionAction` returns `"advise"` for it, and `bookFiring` sets `Detail` to the capped text (post-cap, pre-fence — apogee-sim hashes it). `appendToolResult` (`dispatch.go:1351`) takes the spans: clamp first, then `Message.WithAdvice` per span with `Offset` measured after the clamp and each prior span, `Turn = a.turns.index`. Both call sites (`dispatch.go:272`, `:526`) pass the spans through. Bypass already skips advise (`bypassSkips`). Advise text never reaches `Request.InjectContext`.

**Regression guard.** `**Files:**` gains `internal/agent/toolresultfloor_test.go`, `internal/agent/toolresultmarker_test.go`, `internal/agent/subagent_test.go` (the `appendToolResult`/`firePostToolResult` signature changes reach them).

**Files:** `internal/agent/reactions.go`, `internal/agent/dispatch.go`, `internal/agent/advise_test.go`, `internal/agent/toolresultfloor_test.go`, `internal/agent/toolresultmarker_test.go`, `internal/agent/subagent_test.go`.

**Tests:** an engine-origin Go advise reaction (harness shape of `hookmutation_test.go:35`) on a stubbed tool call: the tool message Content ends with the exact `RenderAdvice` string, `Advice[0]` carries `{id, engine, post-tool-result, turn, offset}`, the firing event has `Action: "advise"` and `Detail` = the text; two advise reactions ⇒ two spans in ladder order; a 9 KiB text is capped with the marker; under Bypass no span and no firing; the delegation path (`commitDelegation`) attaches the same trailer; `encodeState` → `restoreState` round-trip yields the bare tool message with no `Advice`.

**Acceptance:**
```
go build ./... && go vet ./internal/agent/
go test ./internal/agent/ -run 'TestAdvise|TestReaction|TestDispatch'
```

**Commit:** `feat(agent): advise spans land as a provenance-fenced trailer on the closing tool result and are stripped from the session record`

**Regression guard.** The result-cap Floor guard (`internal/floor/resultcap.go`) still truncates the request projection under budget and may elide a trailer; the conversation and the ledger are untouched, which is the existing marker's contract (`internal/context/toolresult.go:15`).

## 6. Agent: user argv advise through the executor, with the `file-changed` narrowing — ✅ DONE (2026-09-09)

NOTES (2026-09-09): the cap is NOT applied a second time in this route. The item's What says the argv route caps, but item 5 landed `domain.CapAdvice` as a single-point cap in `adviceOf` — documented there as "capped here, once, so the cap cannot be bypassed by a route that renders a span itself" — and `CapAdvice(CapAdvice(x))` re-cuts an already-capped string, splicing the truncation marker. Redaction happens here, before anything else touches the text (the ratified "before the cap and the fence"); the cap is met once downstream, so the trailer, the ledger and the firing's Detail all carry the capped text exactly as the item requires.
NOTES (2026-09-09): `seamPayload.invoke` now takes the whole `domain.Reaction` instead of `domain.Handler` — the argv route needs the reaction's class, id, `on:` list and deadline, none of which a handler carries. All five cases of the invoke table and `fireOne`'s one call site are updated; no behaviour changes for a Go handler.
NOTES (2026-09-09): the advise SPAN records `post-tool-result` even for a `file-changed` entry — file-changed narrows which calls an entry hears, not where a trailer can land, and there is one trailer slot. The narrowing is said in the stdin document the command reads (`event`, `APOGEE_REACTION_EVENT`, `path`), which the file-changed test asserts through Detail. Item 5's `adviceOf` was left untouched.
NOTES (2026-09-09): the 10s advise class default is pinned on `syncTimeout`'s value rather than by a sleeping command — item 4's own rationale — and the timeout BEHAVIOUR (a failed firing, no span, one reporter line) is pinned with an explicit 150ms `timeout:`.

**What:** Depends on items 3, 4, 5. In `internal/agent/reactions.go`'s invoke table (`:400-449`): a reaction with `ArgvHandler` and class advise at `post-tool-result` builds a `domain.SeamPayload` from the call and the `ToolResultEdit` (`event` = the Moment the reaction subscribed: `post-tool-result`, or `file-changed` when it listed that and the call is a successful write per `a.classifyWriteTarget(tool, call)` / `a.resolvedPath(call)` — `path` set; a `file-changed`-only reaction does not fire on other calls), runs `a.runSyncArgv`, passes stdout through `tools.RedactSecrets(out, a.cfg.SecretEnvVars)` then `domain.CapAdvice`, and returns `Outcome{Inject: text}`; empty stdout ⇒ `Outcome{}` (no span, no firing). Any error ⇒ `Outcome{}` after item 4's reporter + `failed` firing (fail-open, D7). `fireLeg`'s "On lacks m" skip (`:203`) learns that `file-changed` matches at `post-tool-result` for class advise. Fixed-sentence path is P1; `Detail` from item 5 is P2 (apogee-sim `analyze.go:438` hashes it).

**Regression guard.** `path` comes from `tools.WorkspaceWriteTarget(tool, call)` (the Runner's own `WriteTarget`, `internal/reactions/match.go:198`) and "successful write" from `!p.Edit.IsError()` (`match.go:217`'s rule) — never from `a.resolvedPath`, which is `""` for an in-workspace write (`workspace_scoped.go:86-92`), nor from `classifyWriteTarget`'s escape target, `""` inside the fence (`dispatch.go:1256-1258`); `resolvedPath` stays the disclosure twin only. The redaction test embeds the secret's literal value in the `sh -c` text (or `printf`s a value the test set in the parent process): `RunHookSubprocess` scrubs every configured secret name from the child env (`exec_common.go:95-105`, `:133-145`), so `$NAME` prints an empty line.

**Files:** `internal/agent/reactions.go`, `internal/agent/advise_argv_test.go`.

**Tests:** a user-origin `ArgvHandler` advise reaction armed via `cfg.Reactions` with `sh -c 'printf "%s\n" "This advisory message is part of an instrumentation test and contains no information about the current task. No response to it is needed."'` (the arm's frozen sentence, `../apogee-sim/docs/plans/2026-09-08 - 01 - advise-admission-arm-pre-registration.md:79-82`): `Detail` equals the sentence plus its newline and `sha256(Detail)` equals the doc's pinned hash; a script whose `sh -c` text carries a configured secret's literal value yields `[redacted]` in the trailer; `on: [file-changed]` fires on a successful `write_file` with `APOGEE_REACTION_PATH` and not on `list_dir`; a failing script books `failed` and no span; timeout at 10s default; under Bypass nothing runs (the script's side effect file is absent).

**Acceptance:**
```
go build ./... && go vet ./internal/agent/
go test ./internal/agent/ -run 'TestAdviseArgv|TestAdvise'
```

**Commit:** `feat(agent): user argv advise reactions run in the sync lane, redacted, capped and fail-open`

## 7. Agent: the gate stage of the Approver at `pre-tool-exec`

**What:** Recast at the regression check (2026-09-09). Depends on items 1, 4. New `internal/agent/gate.go`: `func (a *Agent) applyGates(ctx, turn int, tool string, call domain.ToolCall, verdict resolution) resolution`, called immediately after both `resolve(` sites (`dispatch.go:386`, `:571`); a `resolveRefuse` verdict skips the gates. It runs every armed reaction of class gate whose `On` holds `pre-tool-exec` in ladder order: Go handlers via `PreToolExecFunc` reading `Outcome.Gate`; argv handlers via `a.runSyncArgv` with a `SeamPayload{event: pre-tool-exec, tool, arguments}`, parsing stdout — first line trimmed must be exactly `allow`/`deny`/`ask`, remaining lines (trimmed, capped at 240 runes) are the reason; anything else, an error, a timeout or a panic ⇒ `ask` with reason `did not answer (<err>)`. Fold: the first `deny` wins and returns a refuse verdict whose tool result is `tool call denied by reaction <id>` (engine-authored; `errorToolResult`, `recordBlocked` as `dispatch.go:804-809`); else any `ask` upgrades the verdict to a forced Gate (`force: true`, the Tier-2 shape at `resolution.go:506-525`) with reason `reaction <id> asks: <reason>` (or `reaction <id> asks` when empty), so the approval cache is skipped and the human prompt shows it; `allow` leaves the verdict untouched. Each gate books `ReactionFiredEvent{Action: verdict word, Detail: reason}`; a failure additionally reports per item 4. Bypass does not skip gates (D9). An unattended Driver installs a denying Approver (`internal/run/run.go:291`), so headless and daemon `ask` ends as the existing `tool call denied by approver` tool result; only an embedder with a nil Approver reaches `finishGate`'s refusal.

**Regression guard.** (a) Gate reactions are fired by `applyGates` ALONE and never by the seam cascade — `fireLeg` skips class gate at every seam, and `acted()` (`internal/agent/reactions.go:321`) counts a non-empty `Gate.Verdict` so a Go gate handler's verdict is booked by `applyGates`, once. (b) An `ask` on a `resolveDelegate` verdict — the timeout/crash escalation included — leaves the verdict untouched (owner call 2026-09-09, header *Ask on a delegation*; the round-one refusal wording is withdrawn): no Gate reaches a delegation (D3/ADR 0013, `resolution.go:242-245`, `dispatch.go:388-390`), so the delegation proceeds and the inherited gate asks on the child's own tool calls — no action runs without the human question; a `deny` still refuses the delegation; the firing books `Action: "ask"` with Detail `deferred to the child's calls`. (c) The `ask` upgrade mutates the resolved verdict in place — `kind`, `force`, `reason`, `remedy`, and `confineOnAllow` when it was a Confine — never rebuilds it as a fresh literal: `resolve()` has already stamped `writeEscapeTarget` (`resolution.go:544`), `auditDecision`/`auditReason` and `box`/`confineChildren` (`:558-559`), which an allowed ask must keep (`dispatch.go:745-748`).

**Files:** `internal/agent/gate.go`, `internal/agent/gate_test.go`, `internal/agent/dispatch.go`, `internal/agent/reactions.go`.

**Tests:** Go gate deny ⇒ tool result exactly `tool call denied by reaction no-force-push`, `IsError`, no execution, firing `Action: "deny"` booked once (the seam cascade does not fire it); argv `printf 'ask\nlooks risky\n'` ⇒ the Approver receives `Reason` containing `reaction warden asks: looks risky` and `force`, the session cache is not consulted, and an allowed ask on an Auto-mode out-of-workspace write still carries its `writeEscapeTarget`; `exit 3` ⇒ ask with `did not answer`; sleeping script ⇒ ask at 5s default; `allow` in Ask-Before mode still prompts; deny under Bypass still denies; the delegation site (`prepareDelegation`) denies a `delegate` call; an argv gate printing `ask` on a `delegate` call spawns the child and the child's first tool call reaches the Approver with `reaction <id> asks`, the delegation's own firing booked `Action: "ask"`, Detail `deferred to the child's calls`; a child agent inherits the gate; a nil-Approver embedder's `ask` reaches `finishGate`'s refusal.

**Acceptance:**
```
go build ./... && go vet ./internal/agent/
go test ./internal/agent/ -run 'TestGate|TestDispatch|TestApprov'
```

**Commit:** `feat(agent): user gate reactions run as an Approver stage at pre-tool-exec — deny, ask or nothing`

## 8. Agent: `SetReactions` arms the `Sync` lane live

**What:** Recast at the regression check (2026-09-09). Depends on items 6, 7. `Agent.SetReactions` (`agent.go:1038`) stores `gen.Sync` beside Floor and Bypass under `genMu`; the armed leg (`reactions.go:159`, `fireLeg`) and `applyGates` iterate the construction-time list (`a.armed`, bench/engine) followed by the generation's Sync list; `Generation()` returns it. `armReactions` (`:473`) stays construction-only. A Sync id colliding with a construction-time id is not checked (a bench arm and a config file never co-exist — writer's call, stated in the doc comment). `inheritedReactions` (`subagent.go:539`) hands children the current Sync list at spawn; a later swap does not reach a running child (same as Floor today). Facade doc for `Generation` updated.

**Regression guard.** (a) `fireLeg` skips class gate (item 6 owns argv advise at its seam); the Sync gate reactions are iterated by `applyGates` alone — stated here and in item 7. Routing an argv gate through `fireLeg` would put it in the pre-tool-exec cascade, where `seamPayload.invoke` asserts `PreToolExecFunc` (`reactions.go:410-412`) and the `wrongHandler` error skips EVERY tool call with `pre-tool-exec reaction failed` (`dispatch.go:260-265`, `:363-368`). (b) No Driver carries a user reaction on both `Config.Reactions` and `Generation.Sync` — `cmd/apogee` never sets `Config.Reactions` (bench/embedder route only); every Driver arms user sync reactions through `SetReactions` (see item 10). (c) Acceptance adds `go test ./cmd/apogee/ -run 'TestSetReactions|TestLateEngine'` since those tests live in `cmd/apogee/wire_engine_test.go:227,301`.

**Files:** `internal/agent/agent.go`, `internal/agent/reactions.go`, `internal/agent/gate.go`, `internal/agent/subagent.go`, `internal/agent/setreactions_test.go`, `apogee.go`.

**Tests:** a swap that adds an advise reaction makes the next tool result carry its trailer; a swap that removes the gate stops the denial; a Floor-only swap leaves Sync alone; a child spawned after the swap carries it; a swapped-in argv gate never reaches `fireLeg` (every tool call still runs, no `pre-tool-exec reaction failed`); `TestSetReactionsSkipsTheRunnerWhenObserveIsUnchanged` and the late-engine replay tests (`cmd/apogee/wire_engine_test.go:227,301`) still pass.

**Acceptance:**
```
go build ./... && go vet ./internal/agent/
go test ./internal/agent/ -run 'TestSetReactions|TestGeneration|TestInherit'
go test ./cmd/apogee/ -run 'TestSetReactions|TestLateEngine'
```

**Commit:** `feat(agent): one generation swap arms the sync lane beside Floor, Bypass and observe`

## 9. Config: the `gate:` key ships; `advise:` stays refused

**What:** Depends on item 1. `internal/config/reactions.go`: `refuseReservedActions` (`:130`) keeps only `advise`; `toReaction` becomes `toReactions(entry) []domain.Reaction` producing one reaction per action key with the entry's id, `Workspace`, `Timeout` (entry `timeout:` or the class default — 30s observe, 5s gate) and the full `on:` list, each validated by `domain.Reaction.Validate` so a Moment no key can take is refused by the domain sentence wrapped as today (`invalid reaction "warden": gate: reacts at pre-tool-exec; "post-tool-result" is not it`). `gate:` must be a sequence of strings: anything else ⇒ `reaction "warden": gate: is an argv list`. `toReactions` keeps `enabled:` handling; `ValidateAll` in `internal/reactions` is applied to the observe half only (`domain.SplitLanes`) and `Generation.Validate` to the sync half; `projectReactions` writes the whole list to `o.Reactions`. The `hooks:` migration is untouched.

**Regression guard.** The list-level `toReactions(list []reactionConfig)` already exists (`internal/config/reactions.go:267`, called at `:295`, `:309` and `configmigrate.go:1255`) and stays the one `verifyReactionsFold` calls; the per-entry mapper is named `entryReactions`, not `toReactions`. The `gate: is spelled` case at `reactions_test.go:179` (`gate: is not yet shipped (ADR 0076 stage 3)`) is replaced by the `gate: is an argv list` case — "every existing sentence byte-identical" holds for every other case. An entry with no action key at all keeps the `run:` sentence (`handler(id)`'s default arm, `reactions.go:169-185`, pinned at `:164`); otherwise each present key maps and an absent one is simply not a reaction.

**Files:** `internal/config/reactions.go`, `internal/config/reactions_test.go`.

**Tests:** extend `TestLoadFileConfigRefusesMalformedReactions` with the two new sentences, replace the `gate: is spelled` case at `:179` with the `gate: is an argv list` case, and keep every other existing sentence byte-identical (`advise:` still refused with `is not yet shipped (ADR 0076 stage 3)`; an entry with neither `run:` nor `gate:` still gets the `run:` sentence); `TestLoadFileConfigResolvesTheReactionsBlock` resolves an entry with `run:` and `gate:` into two reactions of one id with 30s/5s defaults and one with `timeout: 2s` into two 2s reactions.

**Acceptance:**
```
go build ./... && go vet ./internal/config/
go test ./internal/config/ -run 'TestLoadFileConfig|TestReaction|TestRegistry'
```

**Commit:** `feat(config): reactions: entries take gate:, one entry resolving to one reaction per action key`

## 10. Driver: every wiring site splits the lanes, reports sync failures and reloads `Sync`

**What:** Recast at the regression check (2026-09-09). Depends on items 8, 9. Rule: every site that hands `opts.Reactions` to `reactions.New` or `Replace` passes only the observe half of `domain.SplitLanes` and arms the sync half through `Generation.Sync` — grep `reactions.New(\|Observe:` in `cmd/apogee`: `wire_boot.go:189`, `wire_firing.go:554`, `headless.go:565`, `schedule.go:131`, `daemonfire.go:281`, `wire_live.go:195`, `wire_settings.go:2143`. `Config.Report` is wired to the same reporter each site gives the Runner (`bridge.NotifyHook`, the headless/daemon log line). `lateEngine.SetReactions` (`wire_engine.go:475`) compares Sync as it compares Observe. The `/settings` `reactions` row (`registry.go:659`): `Desc` becomes `Commands run when a Moment closes or a seam fires; advise text reaches the model fenced, a gate answers before the Approver does.`; the row's `Read` (`:662`) counts distinct ids. `TestRegistryIsBijectionWithFileConfig` stays green.

**Regression guard.** `cmd/apogee` never sets `Config.Reactions`; the swapping Driver arms the sync half through the lateEngine's seeded `Generation.Sync` (`wire_live.go:195`). The one-shot Drivers hold no Agent — `run.Once` builds it itself (`agent.New(cfg)`, `internal/run/run.go:296`) and `run.Spec` (`run.go:30-80`) has no post-construction hook — so `Spec` gains `Sync []domain.Reaction`, which `Once` applies with `a.SetReactions` right after `agent.New`, before the first Step; `headless.go:844`, `schedule.go:200` and `daemonfire.go:412` fill it from `domain.SplitLanes(opts.Reactions)` — one route per Driver, never both. The rule reaches `s.gen`'s writers too: `liveSettings.setObserve` (`wire_settings.go:703-707`) becomes a lanes setter fed by `SplitLanes(file.Reactions)`, `reloadReactions` (`:2143-2151`) writes both `gen.Observe` and `gen.Sync` rather than the whole re-read list into Observe, and `generationLocked` (`:690-694`) clones Sync as it clones Observe. The rule extends to every writer of `Options.Reactions` (grep `\.Reactions = ` in `cmd/apogee`): `wire_settings.go:904` (`next.Reactions = slices.Clone(s.gen.Observe)`) rebuilds the Options every Firing a live session raises reads (`schedule.go:131`), so it folds `s.gen.Sync` back in. `countSummary` (`registry.go:970`) is the shared helper of nine rows and does not change; the `reactions` row's `Read` (`:662`) counts distinct ids and hands the count to it.

**Files:** `cmd/apogee/wire_boot.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/headless.go`, `cmd/apogee/schedule.go`, `cmd/apogee/daemonfire.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_engine.go`, `cmd/apogee/wire_engine_test.go`, `cmd/apogee/wire_settings_test.go`, `internal/run/run.go`, `internal/run/run_test.go`, `internal/config/registry.go`, `internal/config/registry_test.go`.

**Tests:** the reactions-row reload test (`TestReactionsRowReloadSwapsObserveOnly` family) gains a case where a `gate:` entry is added to the file and the bound agent's `Generation().Sync` carries it; a sync failure reaches the Driver's notice function; the row summary for one entry with `run:` and `gate:` reads `1 reaction`; a Firing raised from a live session whose `Generation.Sync` holds a gate runs with that gate (`wire_settings.go:904`); `run.Once` applies `Spec.Sync` through `SetReactions` before the first Step (`internal/run/run_test.go`, the `TestOnce…` family) and a headless `gate:` entry reaches the Firing's agent; the live reload writes both lanes into `s.gen` and `generationLocked` returns an independent Sync clone; no `cmd/apogee` site sets `Config.Reactions` (`grep -n 'Reactions:' cmd/apogee/*.go | grep -v _test` stays empty, as it is at `7b23f9da`; the `config.Options{Reactions: …}` literals in `_test.go` files stay).

**Acceptance:**
```
go build ./... && go vet ./cmd/apogee/ ./internal/config/
go test ./cmd/apogee/ -run 'TestReactionsRow|TestLateEngine|TestSetReactions' && go test ./internal/run/ -run TestOnce && go test ./internal/config/ -run TestRegistry
```

**Commit:** `feat(driver): the reactions: file arms both lanes, sync failures reach the notice line, /settings counts entries`

## 11. Journey: a `gate:` entry in the user's file denies and asks headless and in the TUI

**What:** Recast at the regression check (2026-09-09). Depends on item 10. In `cmd/apogee/e2e_reactions_test.go`, over `stubllm` scripting `list_dir {"path":"."}` then a reply (`testdata/stubllm/reactions.yaml` shape): (a) headless, `~/.apogee/config.yaml` with `- id: warden / on: [pre-tool-exec] / gate: ["sh", "-c", "echo deny"]` — the tool result the stub receives on its next request is exactly `tool call denied by reaction warden` and the headless line stream carries `reaction_fired` with `"action":"deny"`; (b) headless with `echo ask` — the next tool message the stub receives is `tool call denied by approver` (the unattended denier at `internal/run/run.go:291`), the stream carries `reaction_fired` `"action":"ask"` followed by an `approval` line with `"decision":"deny"`, the run continues to the reply and exits 0 with the stderr summary `denied: 1`; (c) TUI (`tuitest` driver, `TestE2EHooksFireFromTheTUI` shape) with `printf 'ask\nlooks risky\n'` in Ask-Before mode — the approval pop-up frame contains `reaction warden asks: looks risky`.

**Regression guard.** (b) headless `echo ask` ends with the tool result `tool call denied by approver` (the unattended denier at `internal/run/run.go:291`) and books `"action":"ask"` — never `finishGate`'s no-Approver sentence, which headless cannot reach; every journey's temp config sets `confine-to-workspace: false` so the sync handlers spawn under a nil-Confinement permit on any host (the permit row's caps case is item 4's unit test). `reaction_fired` lines exist only under `--format json` (`cmd/apogee/headless.go:390`; text mode prints nothing for a `ReactionFiredEvent`, `:178`) and `headlessHooksAgainst` (`e2e_reactions_test.go:878`) hard-codes text mode, so (a)/(b) run through a helper variant that passes `--format json`, read the JSONL off stdout and the tool message off `stub.Requests()` (`internal/stubllm/log.go:41`).

**Files:** `cmd/apogee/e2e_reactions_test.go`, `cmd/apogee/testdata/stubllm/reactions.yaml` (only if a new turn is needed).

**Tests:** the three journeys above, (a)/(b) under the `--format json` helper variant.

**Acceptance:**
```
go test ./cmd/apogee/ -run 'TestE2EGate'
```

**Commit:** `test(e2e): a gate: entry denies and asks through headless and the TUI`

## 12. GATE: the advise admission arm has passed

**What:** Depends on item 6 (P1, P2 landed) and item 11 (so the gate cell is whole before the wait). Verify-first, no code. Read `../apogee-sim/docs/plans/2026-09-08 - 01 - advise-admission-arm-pre-registration.md`: the item passes only when its status block carries a dated line of the form `Verdict: pass — <results bundle path>` (any other status, `inferior`, `no-evidence` or the absence of the line ⇒ this item is a FOLLOW-UP: stop the run and hand the runbook to the owner, who runs the arm on the host; an `inferior` verdict means stage 3 does not ship advise and the plan is re-opened, per the doc's disposition table). When the line exists, record it as a dated NOTES line here with the bundle path and the rung's model class. No file in this repo changes.

**Files:** none.

**Tests:** none.

**Acceptance:**
```
grep -n '^> .*Verdict: pass' "../apogee-sim/docs/plans/2026-09-08 - 01 - advise-admission-arm-pre-registration.md"
```

**Commit:** none (a NOTES line on this item only).

## 13. Config: the `advise:` key ships — bounded to the arm's verdict

**What:** Depends on items 9, 12. `internal/config/reactions.go`: delete `refuseReservedActions`; `advise:` resolves like `gate:` — a sequence of strings, class advise, default 10s, `on:` validated by the domain (`invalid reaction "coach": advise: reacts at post-tool-result or file-changed; "turn-finished" is neither`); a mapping ⇒ `reaction "coach": advise: is an argv list`. The lint case resolves whole: `on: [file-changed]` with both `run:` and `advise:` yields an observe reaction and an advise reaction of one id.

**Files:** `internal/config/reactions.go`, `internal/config/reactions_test.go`.

**Tests:** the `not yet shipped` expectation is replaced by the two sentences above; the lint-case resolution; the two-lane `Options.Reactions` shape.

**Acceptance:**
```
go build ./... && go vet ./internal/config/
go test ./internal/config/ -run 'TestLoadFileConfig|TestReaction'
```

**Commit:** `feat(config): reactions: entries take advise: — the user advise cell ships`

## 14. Journey: an `advise:` entry's text reaches the model as the trailer and never the session record

**What:** Depends on items 10, 13. In `cmd/apogee/e2e_reactions_test.go`, headless over the same stub script: config `- id: coach / on: [post-tool-result] / advise: ["sh", "-c", "echo remember to run the tests"]`: the request the stub receives after the tool call ends its tool message with the exact `RenderAdvice` string for `{coach, user origin, post-tool-result, turn N}`; the `reaction_fired` line carries `"action":"advise"` and `"detail":"remember to run the tests\n"`; the saved session file's tool message has no `[advice` substring; `--bypass` produces no trailer and no `advise` line; an entry on `file-changed` fires for a scripted `write_file` and not for `list_dir`.

**Regression guard.** Headless defaults to `--mode plan` (`cmd/apogee/headless.go:329`), where `write_file` is refused before it runs (`planRefusalReason`, `internal/agent/resolution.go:46`, `:398`), so the `file-changed` journey runs with `--mode auto` and a `write_file` turn added to `reactions.yaml`; every journey's temp config sets `confine-to-workspace: false` (`--mode auto` is otherwise refused on a host without fs caps, `headless.go:639`, `probe.AutoUnattendedBlocked`), so the sync handlers spawn under a nil-Confinement permit on any host. The `reaction_fired` `"action":"advise"` line needs `--format json`, exactly as item 11 (`headless.go:390`, `:178`; `e2e_reactions_test.go:878`): the journeys use item 11's `--format json` helper variant.

**Files:** `cmd/apogee/e2e_reactions_test.go`, `cmd/apogee/testdata/stubllm/reactions.yaml` (the `write_file` turn).

**Tests:** the four journeys above, under the `--format json` helper variant; the `file-changed` one under `--mode auto`.

**Acceptance:**
```
go test ./cmd/apogee/ -run 'TestE2EAdvise'
```

**Commit:** `test(e2e): an advise: entry lands as the fenced trailer, is stripped from the record and is off under Bypass`

## 15. Manual: `reactions.md` and `configuration.md` describe the two cells

**What:** Depends on item 13. `docs/manual/reactions.md`: rewrite the `advise:`/`gate:` "not yet shipped" row (`:53`) and sentence (`:48`); add `## Advising the model` (Moments, the stdin document and env, redaction, the 8 KiB cap and marker, the fence the model sees — quote `RenderAdvice`'s form, fail-open, the 10s default, Bypass, "return facts, not imperatives", the session-record strip) and `## Gating a tool call` (the stdout protocol, deny text, ask reason, escalation, 5s default, runs under Bypass, allow means nothing); extend `## The payload` with the seam document's keys and `## Where a failure shows` with the sync-lane sentence; state the permit row in one paragraph. `docs/manual/configuration.md` `## Reactions — reactions:` (`:370-419`): the seams sentence at `:379-381` becomes the two keys' summary with one example each; `:377-378` "six moments" corrected to eleven. Every sentence stated matches an item's pinned string. `internal/config/defaults/config.yaml` (its `:467-468` comment saying the keys are refused) and `README.md` (`:224`, "reactions can change nothing the model sees") are rewritten to the shipped state.

**Regression guard.** `**Files:**` gains `internal/config/defaults/config.yaml` (its `:467-468` comment saying the keys are refused) and `README.md` (`:224`, "reactions can change nothing the model sees") — both rewritten to the shipped state; the What names them. `internal/docmap` is the doc.go file-map guard and `internal/doctext` is PDF extraction — neither reads `docs/manual/`; the manual's real guards are `cmd/apogee/docs_settings_test.go:38`, `cmd/apogee/docs_env_test.go:66,439` and `internal/tools/manual_drift_test.go:18` (all read `configuration.md`), and Tests/Acceptance name those.

**Files:** `docs/manual/reactions.md`, `docs/manual/configuration.md`, `internal/config/defaults/config.yaml`, `README.md`.

**Tests:** `go test ./cmd/apogee/ -run 'TestManual|TestDocsEnv' && go test ./internal/tools/ -run TestManualListsEveryKnownToolName` (the manual's real guards) and the sweep grep below returns nothing.

**Acceptance:**
```
go test ./cmd/apogee/ -run 'TestManual|TestDocsEnv' && go test ./internal/tools/ -run TestManualListsEveryKnownToolName
grep -rn -i 'observe-only\|can change nothing\|reserved for a later release\|not yet shipped\|not shipped yet' README.md docs/manual/ internal/config/defaults/config.yaml ; test $? -eq 1
```

**Commit:** `docs(manual): reactions take advise: and gate:`

## 16. Design docs: CONTEXT.md, the confinement contract's §10.4 row, the greenfield and ADR 0076 notes

**What:** Depends on item 13. `CONTEXT.md`: the `Reaction` entry's stale sentences (`:1174-1175` "five sealed per-seam Go func types", `:1179` "non-Go handlers stage 2 adds", `:1191-1192` "reserved keys that refuse the file") describe the shipped handler set and the two keys; the `Reaction surface` entry gains one sentence naming the user cells that ship (observe, advise, gate) and the reserved one; new glossary entries `Advice span` (the ledger record, ephemeral) and `Gate stage` (ADR 0076 D2's Approver stage) in the Reactions section, `_Avoid_` lines included. `docs/design/confinement-execution-contract.md` §10.4 (`:1119-1125`): a row for user-origin sync reactions at `pre-tool-exec` and `post-tool-result`, the ratified permit row verbatim, and §10.5 names `runSyncArgv` as a caller. `docs/design/reaction-core-greenfield.md` §9.4: a `Plan C (stage 3)` line pointing here; §2.4's amended note gains "stage 3 shipped `advise:`/`gate:` (argv only)". ADR 0076: a dated note under D8 recording the redactor and the permit row as this plan's reading. Rule for the sweep: every sentence in these files that says advise or gate is reserved, unshipped or refused is rewritten — `grep -n -i 'not yet shipped\|reserved\|until stage 3' CONTEXT.md docs/design/reaction-core-greenfield.md docs/design/confinement-execution-contract.md` finds them; user shape(view) and `mcp:` stay reserved.

**Regression guard.** The bite check is narrowed to sentences the tree carries at `7b23f9da` — `grep -n -i 'not yet shipped'` over CONTEXT.md and the greenfield already returns nothing (CONTEXT.md:1191 says "reserved keys that refuse the file", `reaction-core-greenfield.md:111` "rejected at load until stage 3"), so Acceptance greps `reserved keys\|until stage 3\|stage 2 adds\|five \*\*sealed\*\*` instead. The sweep is scoped to the three files above: `docs/design/test-drivers.md:609` ("reserved" IPv4 nets) and ADR 0076 `:204`, `:308` are not reaction sentences, and ADR 0076 A7 `:286-287` ("rejected at load in stage 2 … they arrive in stage 3") stays as the stage-2 record beside the dated note under D8 (`:119`). No test reads CONTEXT.md (`internal/docmap` checks doc.go file maps only, `docmap.go:29-32`), so the Tests line is the narrowed grep plus `go build ./...`. The contract's §10.3 "not Auto ⇒ nothing" row (`:1107-1108`) records today's posture and would contradict the new §10.4 row; §10.3's table is scoped in one sentence to the post-response permit (`hookExecutionCtx`) and the sync-lane permit row lives in §10.4 — the ratified permit row supersedes §10.3 for sync reactions (header design call, ADR 0076 D8).

**Files:** `CONTEXT.md`, `docs/design/confinement-execution-contract.md`, `docs/design/reaction-core-greenfield.md`, `docs/adr/0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md`.

**Tests:** the narrowed sweep grep below shows only the shape(view) and `mcp:` reservations (`reaction-core-greenfield.md:5`); `go build ./...`.

**Acceptance:**
```
go build ./...
grep -n -i 'reserved keys\|until stage 3\|stage 2 adds\|five \*\*sealed\*\*' CONTEXT.md docs/design/reaction-core-greenfield.md ; test $? -eq 1
grep -n -i 'post-response only\|get no permit' docs/design/confinement-execution-contract.md ; test $? -eq 1
```

**Commit:** `docs(design): the user advise and gate cells are shipped — glossary, contract §10.4 row, greenfield and ADR 0076 notes`
