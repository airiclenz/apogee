# Refocus follow-ups — security gap, image/pricing bugs, doc drift

**Goal:** Close the open, Linux-fixable beads left by the 2026-10-03 table-stakes plan (the POSIX unconfined-Console send, four image/pricing bugs, two P4s), fix the `apogee doctor` remedy, and bring the docs, glossary and ADRs that lag the code back in line.
**Date:** 2026-10-03
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 0d739da4

**Sources:**
- `docs/plans/archived/2026-10-03 - 00 - table-stakes-and-image-input-plan.md`
- `docs/adr/0059-a-console-is-live-host-state-the-model-drives-across-turns.md`, `docs/adr/0093-*.md`, `docs/adr/0075-*.md`, `docs/adr/0012-*.md`, `docs/adr/0076-*.md`
- `docs/design/confinement-execution-contract.md`
- `AGENTS.md` (beads workflow). The ADR amendment convention (status suffix, top `> Note (date):`, appended `## Amendment (date) — …`) is written down nowhere — it exists only by example: `docs/adr/0013-*.md` (Amendment) and `docs/adr/0018-*.md` (top Note)

**Ratified design calls** (owner, 2026-10-03):
- **Unconfined Console under a box (POSIX):** gate every send exactly as Windows does (approval each send, headless refuses); the tool's error text tells the model to close and reopen the Console; the generic prompt wording stays.
- **Unconfined Console refusal text:** the model's refusal result carries the tool's close-and-reopen hint (dispatch carries the tool's `ErrConfinementUnavailable`-wrapped error text into the fallback refusal/denial result; the approval prompt stays generic).
- **Image mark:** an `attached: <name> (<size>)` row inside the sent user block, same wording as the pending line; persisted as names and sizes only.
- **Resumed currency:** the record's label wins; new calls in that session count as unpriced; a record with no priced call or no label adopts the configured label; ADR 0093 gets a dated amendment.
- **Usage line money keys:** per call `cost`, `priced`; running `cumulative_cost`, `cumulative_unpriced_calls`; one `currency`.
- **Unnamed-server vision refusal:** "this server does not accept images: set vision: true on its servers: entry".
- **ADRs:** one-line Mechanism Note on every un-noted ADR using the term; status suffix + dated Amendment on 0005 and 0071; one-line Note on 0013, 0042, 0079; bodies untouched.
- **Scope extras:** CONTEXT.md new entries, contract §6.3 amendment, layout.md tok/s, `apogee doctor` test fixtures, CONTEXT.md guard list — all in.

**Standing requirements:**
- skills: coding-standards
- No AI attribution in commit messages.
- Tests target one package and one `-run` pattern at a time.

**Out of scope:** `apogee-per-server-idle-timeout` (owner-gated on evidence); Windows/macOS-only beads (`apogee-conpty-final-frame-lost`, `apogee-6ef3`, `apogee-2uh*`); unparked P3 features; parked beads; CHANGELOG release headings; line anchors in old ADRs; a Console-specific approval prompt (follow-up bead, see handoff).

**Regression check (2026-10-03, 0d739da4):**
- 2: guard folded (model never sees the tool's error under a box; CHANGELOG.md:32 POSIX sentence)
- 2: recast (owner decision: dispatch.go carries the tool's error text into the fallback refusal/denial result; CHANGELOG.md off Files, note kept as a closeout instruction)
- 5: guard folded (frameCurrency takes domain.Usage; Writer.SetCurrency after ApplyConfig; priced e2e test; existing usage golden case)
- 7: guard folded (recast the attach-line assertion; addUser keeps its signature; paintKey gains no term)
- 8: guard folded (stripEntry strips image names)
- 9: guard folded (one `session.Meta` label rule; configured fallback read at Save; supersedes the Meta.Currency doc and CONTEXT.md:2028)
- 10: guard folded (label override read live; Cumulative rewritten once per fold; stripped label; commandrun.go; supersedes docs/manual/commands.md:33)
- 11: guard folded (commit-secrets stays listed)
- 12: guard folded (headers-env check scoped to the MCP client entry)
- 13: guard folded (tok/s check scoped to the status-line sections; §9–§11 count as amendment blocks)
- 14: header Sources bullet corrected (amendment convention exists only by example)
- 15: guard folded (Note placement after item 14's; 0079 Acceptance via test-drivers.md); yields to internal/agent/subagent.go:1955-1957 (ADR 0005's tool-subset rule stands)
- 2 (re-check): guard folded (CHANGELOG grep dropped from Acceptance, checked at closeout; dispatch test fake is a Subprocess() tool with no Approver, not unconfinableClaimTool; tools tests assert the POSIX and Windows error texts; open comment/doc rule with sweep grep; demote ErrorEvent stays generic and fires per gated POSIX send); supersedes ADR 0059 Bounds "Amended 2026-10-03" bullet and docs/manual/configuration.md:2547-2549

## 1. Reconcile the bead register and the archived plan's status — ✅ DONE (2026-10-04)

NOTES (2026-10-04): export.auto did not rewrite .beads/issues.jsonl after the three-id `bd update` (AGENTS.md batch-write lag), so it was re-exported with `bd export -o .beads/issues.jsonl`; the diff is exactly the rw6, zwvj and 6ef3 rows, and `.beads/interactions.jsonl` is unchanged, so it is off FILES. The ifrv import changed the DB only (the committed row was already the closed one). Committer: the pre-commit hook re-exports after the index snapshot — if `.beads/issues.jsonl` shows modified after the commit, re-stage it and `git commit --amend --no-edit`.

**What:**
**Goal:** `bd export` and `.beads/issues.jsonl` agree row for row; `apogee-ifrv` is closed with its original close time, reason and note; `apogee-rw6` is titled "MCP OAuth for HTTP MCP servers"; `apogee-rw6`, `apogee-zwvj`, `apogee-6ef3` carry spec-id `docs/plans/archived/2026-10-03 - 00 - table-stakes-and-image-input-plan.md`; that archived plan's header reads `**Status:** done — all 18 items done (2026-10-03)`.
**Approach (assumed at the header base):**
- `grep '"id":"apogee-ifrv"' .beads/issues.jsonl | bd import -` (single-row import; never `bd close`, never a whole-file import).
- `bd update apogee-rw6 --title "MCP OAuth for HTTP MCP servers"`; `bd update apogee-rw6 apogee-zwvj apogee-6ef3 --spec-id "<archived path>"`.
- Edit the archived plan's `**Status:**` header line only.
- Commit `.beads/issues.jsonl` (and `.beads/interactions.jsonl` if changed) with the plan file; the pre-commit hook re-exports after the index snapshot — if `git status` then shows `.beads/issues.jsonl` modified, add it and `git commit --amend --no-edit`.
**Files:** `.beads/issues.jsonl`, `.beads/interactions.jsonl`, `docs/plans/archived/2026-10-03 - 00 - table-stakes-and-image-input-plan.md`
**Read first:** `.beads/issues.jsonl` — apogee-ifrv row (closed, updated_at 2026-10-03T19:45:08Z, newer than the DB's 2026-09-30 so `bd import -` upserts it); `.beads/hooks/pre-commit` — bd hooks run pre-commit; `AGENTS.md` — beads hook bullet (export only when a .beads/ path is staged, re-export after index snapshot); `docs/plans/archived/2026-10-03 - 00 - table-stakes-and-image-input-plan.md` — **Status:** header line
**Tests:** none (register data).
**Acceptance:**
- `bd show apogee-ifrv | grep -i closed`
- `bd show apogee-rw6 | grep "MCP OAuth for HTTP MCP servers"`
- `diff <(bd export | python3 -c 'import sys,json;[print(json.dumps(json.loads(l),sort_keys=True)) for l in sys.stdin]' | sort) <(python3 -c 'import json;[print(json.dumps(json.loads(l),sort_keys=True)) for l in open(".beads/issues.jsonl")]' | sort)` prints nothing
- `grep -n '^\*\*Status:\*\* done' "docs/plans/archived/2026-10-03 - 00 - table-stakes-and-image-input-plan.md"`
**Commit:** `chore(issues): reconcile ifrv, retitle rw6, repoint archived spec-ids`

## 2. Gate a POSIX send to an unconfined Console under a box — ✅ DONE (2026-10-04)

NOTES (2026-10-04): consequential edit — docs/design/confinement-execution-contract.md: made necessary by the D4 fallback refusal/denial now carrying the tool's ErrConfinementUnavailable text after its quoted reason (dispatch.go).

NOTES (2026-10-04): consequential edit — internal/console/registry.go: made necessary by console_send now demoting a send under a box to an unconfined Console on every host (the Console.Confined comment said only Windows demotes).

NOTES (2026-10-04): dispatch carries only what the tool's error says beyond the sentinel's own "apogee: confinement unavailable on this host" (withConfineFailureDetail), so a POSIX refusal does not tell the model the host cannot confine; a bare-sentinel error adds nothing. TestDispatch_ConfineFallbackRefusalCarriesTheToolError covers the denial path too (table: headless, denied).

NOTES (2026-10-04): TestConsoleSend_UnconfinedConsoleIsDemotedUnderABoxOnPOSIX sends the boxed line raw and then presses Enter unboxed (no 42 = nothing was typed), instead of the plan's raw "" follow-up, which writes nothing and so cannot surface a typed-but-unentered line. ADR 0059's Bounds bullet gained a one-line "superseded for POSIX" pointer to the new Amendment.

NOTES (2026-10-04): closeout instruction — in CHANGELOG.md's Unreleased ConPTY entry, replace "Behaviour on macOS and Linux is unchanged." with a sentence saying macOS and Linux now gate each such send too (see this item's entry).

**What:** Recast at the regression check (2026-10-03). Security fix (`apogee-zwvj`): on POSIX, `console_send` to a Console opened without a box runs unfenced after `/mode auto`; item 16 of the archived plan gated only Windows. Also fixes `apogee-console-send-test-echo`: POSIX send tests assert on text the pty echoes back, so they never prove the line ran.
**Regression guard.** Under a box the model never sees `ConsoleSend.Execute`'s error: `executeTool` turns `ErrConfinementUnavailable` into `dispatchConfinementUnavailable` and drops `err.Error()` (`internal/agent/dispatch.go:1593-1594`), so the model gets the approved unconfined run's output or `confineDemoteRefuseReason`. Change `internal/agent/dispatch.go` so that when the confinement fallback refuses a call (headless, no approver) or the user denies the demoted call, the result the model receives includes the tool's `ErrConfinementUnavailable`-wrapped error text (for `console_send` on POSIX: close the Console and reopen it to run it fenced) in addition to the existing refusal/denial reason; the approval prompt wording stays generic; other tools' refusals change only by carrying their own error text if any. Test in `internal/agent/dispatch_test.go`: `TestDispatch_ConfineFallbackRefusalCarriesTheToolError` uses a `Subprocess()==true` fake shaped like `confinePropagatingTool` (`dispatch_test.go:633`) — not the read-only `unconfinableClaimTool` (`:1243`), which resolves to Run with no box and already reaches the model through `executeTool`'s ordinary error branch (`dispatch.go:1596-1597`), so it passes on the base tree — returning a wrapped `ErrConfinementUnavailable` with custom text, under `autoConfig` with a capable `fakeConfiner` and `cfg.Approver = nil`; assert the headless refusal result holds both the custom text and `confineDemoteRefuseReason`. In `internal/tools` (no test asserts `Execute`'s error text today — `console_send_test.go:250` checks `errors.Is` only): the new POSIX test asserts the error names closing and reopening the Console, and the `consoleSendOn(false)` test gains an assertion on "this platform cannot confine a console" (`console_send.go:141`). Rewrite every comment or doc saying a send to an unconfined Console still runs where a Console can be confined, or that the demotion is Windows-only — beyond the comments Approach names, also `consoleSendUnfenced`'s comment (`console_send.go:157-158`), the `host` field comment (`console_send.go:58-59`) and the test comments (`console_send_test.go:206-208, 236-240, 258-261`); sweep with `grep -rn -i "Ask-opened\|can confine\|cannot confine\|Windows host's alone\|nothing changes" internal/tools internal/platform docs/manual docs/adr/0059-*`. This supersedes the ADR 0059 Bounds "Amended 2026-10-03" bullet (`docs/adr/0059-*:93-102`, "POSIX is unchanged") and `docs/manual/configuration.md:2547-2549`. The demote ErrorEvent ("confinement unavailable at run time: demoting subprocess call to Approval", `dispatch.go:1207-1211`) stays generic and now fires on every gated POSIX send — a TUI error row and an `error` Reaction firing per send, on a host that can confine; do not reword or suppress it. The closeout CHANGELOG change also rewrites "Behaviour on macOS and Linux is unchanged." in the Unreleased ConPTY entry (`CHANGELOG.md:32`): POSIX now gates each such send too. CHANGELOG changes ride the sidecar to closeout, so that sentence is checked at closeout, not at item verification.
**Goal:** On every host, a `console_send` with a confinement box in context aimed at a Console whose `Confined` is false returns an error wrapping `domain.ErrConfinementUnavailable` and writes nothing to the Console; the same send with no box, and a send to a confined Console, still run. On POSIX the error text tells the model to close the Console and reopen it to run fenced; on Windows the text is unchanged. Every POSIX send test that claims a line ran asserts on output only execution produces.
**Approach (assumed at the header base):**
- `ConsoleSend.consoleSendUnfenced` keeps only `confinementBox(ctx) != nil && !target.Confined`; `t.host.shell.ConsoleConfines()` now only selects the error wording.
- Dispatch gating is unchanged: `executeConfine` → `executeConfineFallback` already asks every time (`force`) with an approver and refuses headless; only the refusal/denial result gains the tool's error text (see the Regression guard).
- Reword the `ConsoleSend` type, `NewConsoleSend` and `Execute` doc comments and the `Terminal.ConsoleConfines` comment in `internal/platform/platform.go`.
- Tests: replace `TestConsoleSend_AskOpenedConsoleStillSendsUnderABoxOnPOSIX` with `TestConsoleSend_UnconfinedConsoleIsDemotedUnderABoxOnPOSIX` (`consoleSendOn(true)`, box via `withFSConfinement`): the boxed send of `echo $((40+2))` errors and a following raw unboxed `""` send shows no `42`; an unboxed send then shows `42`. Switch `TestConsoleSend_RunsTheLineAndReportsLiveness` and `TestConsoleSend_QuotedIDAddressesTheSameConsole` to the arithmetic or `printf 'mark%s\n' 1` idiom used by `TestConsoleSend_RawSendsNoNewline`.
- ADR 0059: dated Amendment stating POSIX now gates like Windows (supersedes the Bounds "POSIX is unchanged… pre-existing gap" sentence). `docs/manual/configuration.md`: the ConPTY paragraph's closing sentence about POSIX behaviour now states the gate.
**Files:** `internal/tools/console_send.go`, `internal/tools/console_send_test.go`, `internal/platform/platform.go`, `docs/adr/0059-a-console-is-live-host-state-the-model-drives-across-turns.md`, `docs/manual/configuration.md`, `internal/agent/dispatch.go`, `internal/agent/dispatch_test.go`
**Read first:** `internal/agent/dispatch.go` — executeTool, executeConfine, executeConfineFallback; `internal/tools/console_send.go` — ConsoleSend.Execute, consoleSendUnfenced; `internal/agent/resolution.go` — confineFallback, confineDemoteRefuseReason;
`internal/agent/dispatch_test.go` — confinePropagatingTool, autoConfig, TestDisposition_RuntimeConfineUnavailable_DemotesToApproval, unconfinableClaimTool; `internal/tools/console_send_test.go` — consoleSendOn, withFSConfinement, TestConsoleSend_AskOpenedConsoleStillSendsUnderABoxOnPOSIX, TestConsoleSend_RawSendsNoNewline;
`internal/agent/recipe_test.go` — TestRecipe_AScriptStageInPlanModeRunsOnlyConfined, TestRecipe_AnAutoScriptStageKeepsItsBoxAndDemote
**Closes:** apogee-zwvj; apogee-console-send-test-echo
**Tests:** `go test -count=1 -run 'TestConsoleSend_' ./internal/tools/` (the new POSIX test asserts the close-and-reopen error text; the `consoleSendOn(false)` test asserts "this platform cannot confine a console"); `TestDispatch_ConfineFallbackRefusalCarriesTheToolError` in `internal/agent/dispatch_test.go` (a `Subprocess()==true` fake, capable `fakeConfiner`, no Approver; result holds the tool's text and `confineDemoteRefuseReason`).
**Acceptance:**
- `go test -count=1 -run 'TestConsoleSend_' ./internal/tools/`
- `go test -count=1 -run TestDispatch_ConfineFallbackRefusalCarriesTheToolError ./internal/agent/`
- `go test -count=1 -run 'TestConsoleOpen_LiveConfinement' ./internal/tools/`
- `GOOS=windows go vet ./internal/tools/`
- `grep -n "POSIX is unchanged\|On macOS and Linux nothing changes" docs/manual/configuration.md` prints nothing
**Commit:** `fix(tools): gate a POSIX send to an unconfined console under a box`

## 3. Name no blank server in the vision refusal — ✅ DONE (2026-10-04)

**What:** Fixes `apogee-vision-refusal-blank-server`: with `cfg.ServerName == ""` the refusal reads `server "" does not accept images`.
**Goal:** With an empty server name, both the Submit refusal and the `@ref` image refusal read "this server does not accept images: set vision: true on its servers: entry"; with a named server, the text is unchanged.
**Approach (assumed at the header base):** One helper on `Agent` (e.g. `visionRefusal()`) formats `visionRefusalFormat` with the name or returns the unnamed wording; `refImageRefusal` and `checkInputImages` both use it. Wording matches `noVisionNote` in `internal/tui/clipboard.go`.
**Files:** `internal/agent/loop.go`, `internal/agent/loop_test.go`
**Read first:** `internal/agent/loop.go` — visionRefusalFormat, refImageRefusal, checkInputImages; `internal/agent/loop_test.go` — imageAgent (hard-codes ServerName "box"; set a.cfg.ServerName after), submitAndStep, TestFileRefImage_NonVisionServerIgnoresIt, TestSubmitImage_RefusedBeforeAnyRequest; `internal/agent/minilang_test.go` — errorEventContaining; `internal/tui/clipboard.go` — noVisionNote
**Closes:** apogee-vision-refusal-blank-server
**Tests:** a table test over both channels using `imageAgent` with `ServerName` set to "" and to a name; reuse `submitAndStep`, `errorEventContaining`.
**Acceptance:** `go test -count=1 -run 'Image' ./internal/agent/`
**Commit:** `fix(agent): name no blank server in the vision refusal`

## 4. Round-trip a priced server entry through YAML — ✅ DONE (2026-10-04)

NOTES (2026-10-04): no CHANGELOG entry — no production path marshals a priced entry today (renderServerEntry renders unpriced legacy entries only), so no user-observable change; MarshalYAML returns an anonymous struct of *float64 fields with omitempty (field order fixes the input, output, cached-input order) rather than a hand-built yaml.Node.

**What:** Fixes `apogee-price-marshal-yaml`: `config.Price` has an `UnmarshalYAML` but no `MarshalYAML`, so a priced `ServerEntry` does not round-trip.
**Goal:** `yaml.Marshal` of a `ServerEntry` with a price, decoded again, equals the original; an omitted `cached-input` stays omitted (and `CachedInputRate()` still falls back to the input rate); stated zero rates survive; an unpriced entry renders no `price:` key.
**Approach (assumed at the header base):** `func (p Price) MarshalYAML() (any, error)` next to `Price.UnmarshalYAML`, emitting an ordered block mapping of only the rates whose `Has*` flag is set (precedent: `RequestExtra.MarshalYAML`); add `Price.IsZero` returning `!p.IsStated()` so `omitempty` is explicit.
**Files:** `internal/config/config.go`, `internal/config/config_test.go`
**Read first:** `internal/config/config.go` — Price, Price.UnmarshalYAML, Price.IsStated, Price.CachedInputRate, RequestExtra.MarshalYAML; `internal/config/config_test.go` — TestServerEntryRequestExtraRoundTrips, TestServerEntryPriceLoads; `internal/config/configmigrate.go` — renderServerEntry (the one production yaml.Marshal of ServerEntry; legacy entries are unpriced, so IsZero keeps them price-free)
**Closes:** apogee-price-marshal-yaml
**Tests:** `TestServerEntryPriceRoundTrips`, modelled on `TestServerEntryRequestExtraRoundTrips`, cases: all three rates; cached-input omitted; zero rates; unpriced.
**Acceptance:** `go test -count=1 -run 'TestServerEntryPrice|TestServerEntryRequestExtraRoundTrips' ./internal/config/`
**Commit:** `fix(config): round-trip a priced server entry through YAML`

## 5. Price the headless usage line

**What:** Fixes `apogee-usage-frame-no-cost`: the headless per-call `usage` event line carries no money keys; only `run_finished` is priced.
**Regression guard.** the shared currency rule moved out of cmd/apogee/headless.go (`frameCurrency`) into internal/eventjson takes a `domain.Usage` (run.Usage is an alias) — eventjson must not import internal/run. `runHeadless` builds the Writer (`headless.go:572`) before `runHeadlessBody` runs `config.ApplyConfig` (`:765`), so `opts.Currency` is unresolved there: add `Writer.SetCurrency` (`SetSession`'s twin, under `w.mu`) and call it in headless.go's `narrate`/`onID` path after `ApplyConfig`, before `RunStarted`; keep `Options.Currency` for embedders and document it on `EventLinesOptions` in `apogee.go`. The existing "usage" case of `TestEncodeJSONGolden` gains `"cost":0,"priced":false,"cumulative_cost":0,"cumulative_unpriced_calls":0,"currency":""` in its `wantData` (in the struct's key order).
**Goal:** Every `usage` event line carries `cost`, `priced`, `cumulative_cost`, `cumulative_unpriced_calls` and `currency`, always present (zero values when unpriced); costs are exact decimals via `eventjson.CostAmount`; `currency` follows the same rule as the run frame (label only when a priced call exists); `docs/manual/headless.md` documents the keys.
**Approach (assumed at the header base):**
- Extend `usageData` in `internal/eventjson/encode.go` from `UsageEvent.CostMicros`, `UsageEvent.Priced`, `Cumulative.CostMicros`, `Cumulative.UnpricedCalls`.
- `eventjson.Options` gains `Currency`; `Writer.Emit` supplies it; `Encode(ev)` keeps its signature and delegates to an internal encoder that takes the currency.
- Move `frameCurrency` from `cmd/apogee/headless.go` into `eventjson` as the one rule; `usageFrame` and `subAgentFrames` call it.
- `cmd/apogee/headless.go` passes `Currency` into `eventjson.New`. New keys are additive within `v:2` (ADR 0075 D10).
**Files:** `internal/eventjson/encode.go`, `internal/eventjson/writer.go`, `internal/eventjson/encode_test.go`, `internal/eventjson/writer_test.go`, `cmd/apogee/headless.go`, `cmd/apogee/testdata/eventlines/run.jsonl`, `cmd/apogee/e2e_usage_test.go`, `apogee.go`, `docs/manual/headless.md`
**Read first:** `cmd/apogee/headless.go` — runHeadless, runHeadlessBody (narrate, onID, ApplyConfig), usageFrame, frameCurrency, subAgentFrames; `internal/eventjson/encode.go` — Encode, usageData; `internal/eventjson/writer.go` — Options, New, Writer.Emit, Writer.SetSession, CostAmount;
`internal/eventjson/encode_test.go` — TestEncodeJSONGolden; `cmd/apogee/e2e_eventlines_test.go` — TestE2EEventLinesGolden, eventLinesHome; `cmd/apogee/e2e_usage_test.go` — writePricedConfig; `apogee.go` — EventLinesOptions, NewEventLines
**Closes:** apogee-usage-frame-no-cost
**Tests:** priced, unpriced and mixed `UsageEvent` cases in `encode_test.go`; a Writer test proving `Options.Currency` and `SetCurrency` reach the line; the existing "usage" golden case updated; a headless `--format json` test on a priced home (`writePricedConfig`, EUR) in `e2e_usage_test.go`, named to match `Usage`, asserting the usage line's `currency` is "EUR" (the `eventLinesHome` fixture is unpriced and cannot catch it); re-record `TestE2EEventLinesGolden` with `-update`.
**Acceptance:**
- `go test -count=1 ./internal/eventjson/`
- `go test -count=1 -run 'TestE2EEventLinesGolden|TestManualListsEveryEventLineKind|Usage' ./cmd/apogee/`
**Commit:** `fix(eventjson): price the headless usage line`

## 6. Point the daemon's Auto refusal at apogee probe host

**What:** Fix: the daemon refuses Auto with "check `apogee doctor`", a command that does not exist (a name apogee announces but lacks).
**Goal:** No Go source or test names `apogee doctor`; the daemon's Auto refusal names `apogee probe host`, pinned by a test.
**Approach (assumed at the header base):** Reword the `domain.ModeAuto` case in `resolveMode`; add `"apogee probe host"` to the wants of the two Auto cases in `TestLoadNamesEveryDefect`; change the arbitrary `Remedy` fixtures in the three test files to `apogee probe host`, updating each expected JSON literal in step.
**Files:** `internal/daemon/file.go`, `internal/daemon/file_test.go`, `internal/eventjson/encode_test.go`, `internal/reactions/match_test.go`, `internal/reactions/payload_test.go`
**Read first:** `internal/daemon/file.go` — resolveMode; `internal/daemon/file_test.go` — TestLoadNamesEveryDefect (cases "auto on a host that cannot confine", "auto on a host that states no confinement facts at all"); `cmd/apogee/daemon_test.go` — pins only "cannot confine a run to its workspace", keep that clause; `internal/eventjson/encode_test.go` — TestEncodeJSONGolden "approval decided";
`internal/reactions/match_test.go` — TestMatchApproval; `internal/reactions/payload_test.go` — TestPayloadJSONGolden; `cmd/apogee/probe.go` — probeHostCommand
**Depends on item 5** (shares `internal/eventjson/encode_test.go`).
**Tests:** `TestLoadNamesEveryDefect` extended.
**Acceptance:**
- `grep -rn "apogee doctor" --include=*.go .` prints nothing
- `go test -count=1 -run TestLoadNamesEveryDefect ./internal/daemon/`
- `go test -count=1 ./internal/eventjson/`
- `go test -count=1 -run 'Remedy|Payload|Match' ./internal/reactions/`
**Commit:** `fix(daemon): point the auto refusal at apogee probe host`

## 7. Mark a sent image in the user block

**What:** Fixes `apogee-image-send-no-trace` (TUI half): an image-only send leaves an empty `❯` block and nothing marks a sent image.
**Regression guard.** Recast `TestAttachedImagesRideTheSubmitAndClearTheLine`'s `!strings.Contains(plain(m.View()), "attached:")` check (`prompteditor_test.go:649`) to the pending line alone (`len(m.images)==0 && m.pendingImageRow()==""`) and assert the sent block's row separately. Keep `addUser(text, spans)` (~180 callers) and add a sibling only `Model.submit` calls (e.g. `addUserWithImages`). The images go on `paintInput` and into `entry.painted()`'s unkeyed literal only; `paintKey` (compared with `==`, must stay comparable) gains no term — committed content is covered by the append-only rule.
**Goal:** After a send with attached images — with or without text — the sent user block shows one `attached: <name> (<size>)` row (multiple images joined by ` · `), the wording of `promptEditor.pendingImageLine`; a text-only send renders as before.
**Approach (assumed at the header base):** `entry` gains an images list (name, size); `addUser` takes the submitted images from `Model.submit`; `renderUserBlock` paints the row; `paintInput` / `entry.painted()` include it in the paint-cache key (unkeyed literal — list the field); share the formatting with `pendingImageLine` rather than duplicating it. Update the user-block description in `layout.md` and the image paragraph in `docs/manual/commands.md`.
**Files:** `internal/tui/model.go`, `internal/tui/transcript.go`, `internal/tui/paintcache.go`, `internal/tui/userblock.go`, `internal/tui/prompteditor.go`, `internal/tui/prompteditor_test.go`, `layout.md`, `docs/manual/commands.md`
**Read first:** `internal/tui/model.go` — Model.submit; `internal/tui/transcript.go` — transcript.addUser, entry; `internal/tui/paintcache.go` — paintInput, entry.painted, paintKey; `internal/tui/userblock.go` — renderUserBlock; `internal/tui/prompteditor.go` — promptEditor.pendingImageLine, imageSize;
`internal/tui/render.go` — renderEntryLines; `internal/tui/prompteditor_test.go` — TestAttachedImagesRideTheSubmitAndClearTheLine, visionModel
**Tests:** extend `TestAttachedImagesRideTheSubmitAndClearTheLine` (both the "" and "what is this?" cases) to assert `plain(m.View())` shows the attached row in the sent block, with its "attach line gone" check recast to the pending line alone.
**Acceptance:** `go test -count=1 -run 'TestAttachedImages|UserBlock' ./internal/tui/`
**Commit:** `fix(tui): mark a sent image in the user block`

## 8. Persist the image mark with the session

**What:** Fixes `apogee-image-send-no-trace` (persistence half): the mark from item 7 must survive save and resume.
**Regression guard.** `DecodeTranscript` promises to strip every painted string (`stripEntry` in `internal/session/transcript.go`) and `fromWireEntry` strips nothing: strip each image name in `stripEntry` and add it to `TestDecodeTranscriptStripsEscapesEverywhereItCanBePainted` (`internal/session/transcript_test.go:327`).
**Goal:** A saved session restores each user block's attached-image names and sizes; no image bytes enter the transcript blob; older records without the field load unchanged.
**Approach (assumed at the header base):** `session.Entry` gains an omitempty images member (name, size); `toWireEntry` / `fromWireEntry` in `internal/tui/transcriptbridge.go` carry it (precedent: `toWireSkillSpans`); extend the `wantEntry` pin in `TestTranscriptCodecPersistsANamedDelegationAsItsTarget` — this item is the "own decision" that test asks for.
**Files:** `internal/session/transcript.go`, `internal/session/transcript_test.go`, `internal/tui/transcriptbridge.go`, `internal/tui/transcriptbridge_test.go`
**Read first:** `internal/tui/transcriptbridge.go` — toWireEntry, fromWireEntry, toWireSkillSpans; `internal/session/transcript.go` — Entry, stripEntry, DecodeTranscript; `internal/tui/transcriptbridge_test.go` — TestTranscriptCodecPersistsANamedDelegationAsItsTarget (ordered wantEntry), TestTranscriptCodecDecodesALegacyBlobUnchanged, TestTranscriptCodecRoundTrip;
`internal/session/transcript_test.go` — TestDecodeTranscriptStripsEscapesEverywhereItCanBePainted, goldenV1Entries
**Depends on item 7.**
**Closes:** apogee-image-send-no-trace
**Tests:** a codec round-trip case with images; a legacy record without the field; an escape-carrying image name stripped in `TestDecodeTranscriptStripsEscapesEverywhereItCanBePainted`.
**Acceptance:**
- `go test -count=1 -run 'TestTranscriptCodec' ./internal/tui/`
- `go test -count=1 ./internal/session/`
**Commit:** `fix(session): persist the sent-image mark`

## 9. Keep a resumed session's currency label on save

**What:** Fixes `apogee-resume-currency-relabel` (host half): `sessionHost.Save` always writes the configured currency, relabelling a resumed record's amount — against ADR 0093 decision 6.
**Regression guard.** the "effective session label" rule (record carries a priced call AND a non-empty label → the record's label, else the configured label) lives in exactly one function in internal/session (a method on `session.Meta`, e.g. `EffectiveCurrency(configured string) string`, in internal/session/store.go or wherever `Meta` is declared), with a unit test there; item 9 adds it and the host (`newSessionHost`, `Activate`) calls it — add that file and its test to item 9's Files. The configured label reaches the host only after construction (`w.host.currency = w.opts.Currency`, `cmd/apogee/wire_live.go:259-263`), so the host keeps only the record's own label and resolves `sessionLabel || h.currency` inside `Save`; `Rotate` clears it. Supersedes the `Meta.Currency` doc (`internal/session/store.go:172-174`, "the root `currency:` in force when its writer saved") and `CONTEXT.md:2028`; both are rewritten to the new rule.
**Goal:** A resumed or activated record keeps its own `Meta.Currency` on every later save when it carries a priced call; a record with no priced call or no label adopts the configured label; `Rotate` returns to the configured label; the session's label reaches the TUI with the resumed session; `domain.Usage` has an `Unpriced()` helper (zero cost, priced calls folded into unpriced); ADR 0093 records the rule.
**Approach (assumed at the header base):** `sessionHost` holds a session label set in `newSessionHost` (from the resumed record), `Activate` (from `meta.Currency`) and reset in `Rotate`; `Save` writes it. `tui.ResumedSession` gains `Currency`, filled by the builder in `wire_session.go`. Dated `## Amendment` on ADR 0093 stating the record's label wins and later calls count unpriced. Manual: `docs/manual/sessions.md` currency paragraph.
**Files:** `cmd/apogee/wire_session.go`, `cmd/apogee/wire_session_test.go`, `internal/session/store.go`, `internal/session/store_test.go`, `internal/tui/tui.go`, `internal/domain/usage.go`, `internal/domain/usage_test.go`, `docs/adr/0093-*.md`, `docs/manual/sessions.md`, `CONTEXT.md`
**Read first:** `cmd/apogee/wire_session.go` — sessionHost, newSessionHost, sessionHost.Save, sessionHost.Activate, sessionHost.Rotate, resumedSession; `cmd/apogee/wire_live.go` — newSessionHost call site (host.currency); `internal/session/store.go` — Meta.Currency, Usage; `internal/tui/tui.go` — ResumedSession;
`internal/domain/usage.go` — Usage, Sum; `cmd/apogee/wire_session_test.go` — TestResumeCarriesParentIDIntoLaterSaves, TestSessionHostRotateAndLoadActivate
**Tests:** in `wire_session_test.go` beside `TestResumeCarriesParentIDIntoLaterSaves`: a priced EUR record saved by a USD host stays EUR (resume and `Activate`); an unpriced EUR record adopts USD; `Rotate` returns to USD; `ResumedSession.Currency` carries the effective label, not the raw record label. `Unpriced()` unit test; a `session.Meta` effective-label unit test in `store_test.go` (priced+label, unpriced+label, priced+no label).
**Acceptance:**
- `go test -count=1 -run 'Currency|Resume|Activate|Rotate' ./cmd/apogee/`
- `go test -count=1 -run 'Unpriced' ./internal/domain/`
- `go test -count=1 -run 'Currency' ./internal/session/`
**Commit:** `fix(session): keep a resumed session's currency label`

## 10. Show and count a resumed session under its own label

**What:** Fixes `apogee-resume-currency-relabel` (TUI half): the footer and `/usage` label restored amounts with the configured currency and add newly priced calls on top.
**Regression guard.** the TUI never re-derives the label rule — it takes the label item 9 delivers (`ResumedSession.Currency`) or, for an in-session resume in `resumeLoaded`, calls the same `session.Meta` method. The Model holds only the resumed record's label as an override ("" means read `m.opts.Currency` live — tests set it after construction); `stripEscapes` the label where `replayResumed` and `resumeLoaded` seat it. Keep `applyUsage`'s signature: rewrite the `UsageEvent`'s `Cumulative` once in `Model.foldEvent` and in `foldBackgroundEvent` (`workflow.go:336`) before the folds read it. The reset on /clear is `Model.resetSessionView` in `internal/tui/commandrun.go`. Supersedes `docs/manual/commands.md:33` ("the amount in your `currency:` label"), which is rewritten for a resumed session.
**Goal:** After resuming a session whose label differs from the configured one, the footer, `/usage` and workflow usage show the session's label, and calls made in that session add tokens and an unpriced count but no cost; a fresh or cleared session uses the configured label.
**Approach (assumed at the header base):** the model holds a session label set by `replayResumed` (from `ResumedSession.Currency`), by `resumeLoaded` (in-session resume) and reset on clear; `footerLeftText`, `usageColumns`/`spendText` callers and `internal/tui/workflow.go` read it; when it differs from `m.opts.Currency`, apply `Usage.Unpriced()` at each `usageReading` site and in `applyUsage`.
**Files:** `internal/tui/model.go`, `internal/tui/sessions.go`, `internal/tui/usage.go`, `internal/tui/fold.go`, `internal/tui/transcript.go`, `internal/tui/workflow.go`, `internal/tui/commandrun.go`, `internal/tui/usage_test.go`, `docs/manual/commands.md`
**Read first:** `internal/tui/fold.go` — Model.foldEvent, usageReading; `internal/tui/workflow.go` — foldBackgroundEvent; `internal/tui/transcript.go` — transcript.applyUsage; `internal/tui/model.go` — replayResumed, footerLeftText; `internal/tui/sessions.go` — resumeLoaded; `internal/tui/commandrun.go` — resetSessionView;
`internal/tui/usage.go` — usageColumns, spendText; `internal/tui/mode_test.go` — TestFooterStatesTheSessionSpend
**Depends on item 9.**
**Closes:** apogee-resume-currency-relabel
**Tests:** in `usage_test.go`: footer and `/usage` show the record's label after resume; a later priced reading adds no cost; clear restores the configured label; a resumed label carrying an escape paints stripped; `TestFooterStatesTheSessionSpend` stays green.
**Acceptance:** `go test -count=1 -run 'Usage|Resume|Currency|Footer' ./internal/tui/`
**Commit:** `fix(tui): show a resumed session under its own currency`

## 11. List every Tier-1 refusal in the docs

**What:** Docs fix: `docs/manual/configuration.md` § "The dangerous-action guard" omits Tier-1 `overwrite-block-device`; the CONTEXT.md **Dangerous-action guard** entry also omits it, the git control plane, and `sudo` among force-approval rules.
**Regression guard.** The Goal reads "the rules in `DefaultDangerousRules` plus `commit-secrets` (`security.SecretsRuleID`)": `commit-secrets` (`internal/security/secrets.go:14`) is live but sits outside `DefaultDangerousRules` (`rules.go:90`), and stays where both docs place it now.
**Goal:** Every live doc that enumerates guard tiers lists exactly the rules in `DefaultDangerousRules`. Rule to find sites: `grep -rn -i "fork.bomb\|hard-refuse\|tier.1" --include=*.md . | grep -v "/archived/\|skill-runs\|CHANGELOG\|docs/adr/"`.
**Approach (assumed at the header base):** Add "a raw `dd` write to a block device (`dd … of=/dev/sd…`, `nvme`, `hd`, `mmcblk`, `disk`)" to the manual's Tier-1 sentence; bring the CONTEXT.md entry into line with `internal/security/rules.go`.
**Files:** `docs/manual/configuration.md`, `CONTEXT.md`
**Read first:** `internal/security/rules.go` — DefaultDangerousRules (overwrite-block-device, write-git-control-plane, sudo-escalation); `internal/security/secrets.go` — SecretsRuleID; `docs/manual/configuration.md` — "The dangerous-action guard"; `CONTEXT.md` — Dangerous-action guard; `internal/tools/manual_drift_test.go` — TestManualListsEveryKnownToolName
**Tests:** none (docs).
**Acceptance:** `grep -c "block device" docs/manual/configuration.md CONTEXT.md` shows ≥1 for each.
**Commit:** `docs: list every tier-1 refusal of the dangerous-action guard`

## 12. Bring the CONTEXT.md glossary up to date

**What:** CONTEXT.md's `reactions:` Generation paragraph says the runner's `Replace` is called only from the Driver's `SetReactions`; `Agent.SetReactions` now installs the Generation and swaps the Observe runner itself. The glossary also lacks image input / per-server `vision:`, MCP `headers:` / `headers-env:`, and Windows ConPTY under **Console**.
**Regression guard.** `grep -c "headers-env" CONTEXT.md` already passes at base (the reactions webhook shape, `CONTEXT.md:1704-1714`); the Acceptance check is scoped to the MCP client entry: `awk '/^\*\*MCP client\*\*/,/^_Avoid_/' CONTEXT.md | grep -c headers-env` ≥ 1.
**Goal:** The Generation paragraph describes one `Agent.SetReactions` call swapping both halves when a runner is configured; CONTEXT.md has entries (or entry extensions) for image input and `vision:`, MCP `headers:`/`headers-env:`, and the ConPTY Console backend, each consistent with the shipped code.
**Approach (assumed at the header base):** Rewrite from `Agent.SetReactions` / `installGeneration` / `swapObserve` in `internal/agent/agent.go`; new wording from the archived 2026-10-03 plan and the code it names. Keep CONTEXT.md's entry style.
**Files:** `CONTEXT.md`
**Read first:** `internal/agent/agent.go` — Agent.SetReactions, installGeneration, swapObserve; `cmd/apogee/wire_engine.go` — lateEngine.SetReactions; `CONTEXT.md` — reactions Generation paragraph, Console, MCP client; `internal/config/config.go` — Vision, HeadersEnv; `internal/mcp/transport.go` — admitHeaderName; `internal/platform/conpty_windows.go`
**Depends on item 11** (same file).
**Tests:** none (docs).
**Acceptance:**
- `grep -n "called only from the Driver's" CONTEXT.md` prints nothing
- `grep -c "vision:" CONTEXT.md` ≥ 1; `awk '/^\*\*MCP client\*\*/,/^_Avoid_/' CONTEXT.md | grep -c headers-env` ≥ 1; `grep -c "ConPTY" CONTEXT.md` ≥ 1
**Commit:** `docs(context): update reaction generation, add images, MCP headers, ConPTY`

## 13. Fix stale design and layout docs

**What:** Mechanical drift: old tool spellings and the deleted `internal/mechanisms` in the confinement contract, and its §6.3 checklists predating escape tests #8 and #11–#14; a half-corrected naming reference in `mcp-client.md`; two live docs citing pre-archive plan paths; `layout.md` never describes the status line's `· N tok/s` readout.
**Regression guard.** `grep -n "tok/s" layout.md` already matches at base (the `/sub-agents-server` pane, `layout.md:2407-2413`); the check is scoped: `awk '/^## The status line.s spinner/,/^## The footer/' layout.md | grep -c "tok/s"` ≥ 1. The contract's dated amendment sections §9–§11 count as dated amendment blocks and are not rewritten; `grep -n "internal/mechanisms"` on the contract may print only lines inside `> **Amended …**` blockquotes or §9–§11 (at base: 556, 579, 1339).
**Goal:**
- The contract uses current tool names (`python_exec`, `web_fetch`, `http_request`, the `git_*` tools where the tool is meant) and names `internal/mechanisms` only inside dated amendment blocks; §6.3 carries a dated amendment matching the 14-row escape table.
- `mcp-client.md` §3 states the `<name>__<tool>` key and points at its Tool naming bullet.
- `workflow-bench-experiment.md` and `user-questions-layout.md` cite `docs/plans/archived/…`.
- `layout.md`'s status-line section describes the tok/s readout from `throughputSuffix`.
**Approach (assumed at the header base):** prose edits; design docs amend with `> **Amended 2026-10-03 (…)**` blockquotes; a `git` that means the system binary stays.
**Files:** `docs/design/confinement-execution-contract.md`, `docs/design/mcp-client.md`, `docs/design/workflow-bench-experiment.md`, `docs/layout/user-questions-layout.md`, `layout.md`
**Read first:** `docs/design/confinement-execution-contract.md` — §3.3 Who carries it, §4 disposition table, §6.2 battery, §6.3 checklists, §10; `internal/platform/confinetest/confinetest.go` — Probe, ProbeNetwork; `docs/design/mcp-client.md` — §3 Tool naming bullet; `internal/mcp/tool.go` — toolNameSeparator; `internal/tui/model.go` — throughputSuffix, statusLine;
`layout.md` — The status line's spinner, The status line's right slot
**Depends on item 7** (same `layout.md`).
**Tests:** none (docs).
**Acceptance:**
- `grep -nE "python-exec|web-fetch|http-request" docs/design/confinement-execution-contract.md` prints nothing
- `grep -n "actually \`<name>" docs/design/mcp-client.md` prints nothing
- `grep -n "docs/plans/2026" docs/design/workflow-bench-experiment.md docs/layout/user-questions-layout.md` prints nothing
- `awk '/^## The status line.s spinner/,/^## The footer/' layout.md | grep -c "tok/s"` ≥ 1
- `grep -n "internal/mechanisms" docs/design/confinement-execution-contract.md` lists only lines inside `> **Amended …**` blockquotes or §9–§11
**Commit:** `docs: fix stale contract, MCP naming, plan paths and status-line layout`

## 14. Note the retired Mechanism layer on remaining ADRs

**What:** Accepted ADRs still using the retired "Mechanism" vocabulary without the 2026-10-02 Note (e.g. 0008, 0011, 0017, 0022, 0039, 0042, 0046, 0050, 0052, 0056, 0062, 0066, 0067, 0068, 0069).
**Goal:** Every ADR under `docs/adr/` that mentions Mechanism/`mechanisms:` carries the top `> Note (…): the Mechanism layer this ADR refers to was removed; ADR 0076 (Reactions) replaces it…` line; bodies unchanged. Rule: `grep -lis "mechanism" docs/adr/*.md | xargs grep -L "Mechanism layer this ADR refers to was removed"` lists only ADRs where the term is not the retired layer (0076 itself, and any naming a different "mechanism") — the implementer records each such exclusion in a NOTES line.
**Approach (assumed at the header base):** copy the Note from `docs/adr/0018-*.md` verbatim, dated 2026-10-03, placed as in 0018.
**Files:** `docs/adr/` (the ADRs the rule finds)
**Read first:** `docs/adr/0018-context-overflow-recovers-structurally-the-emergency-fold-and-one-retry.md` — top Note (wording, placement after front matter); `docs/adr/0003-*`, `0015-*`, `0070-*` — already-superseded Mechanism ADRs; `docs/adr/0081-*`, `0091-*` — generic or "Nothing here is a Mechanism" uses (exclusion candidates); `docs/adr/0076-*` — the replacing ADR
**Tests:** none (docs).
**Acceptance:** the rule's grep lists only ADRs recorded as exclusions.
**Commit:** `docs(adr): note the retired mechanism layer on remaining ADRs`

## 15. Mark ADR contradictions with later decisions

**What:** ADR 0005 has no amendment yet still requires Confinement for Auto sub-agents, lists the removed audit guardrail and the tool-subset rule; ADR 0071 D3 says the `validated` package stays (ADR 0076 A9 deleted it); ADR 0013's status line lacks its §4 supersession; ADR 0042 D4 cites superseded 0004; ADR 0079 cites a nonexistent "ADR 0062 call 13" (the rule lives in `docs/design/test-drivers.md` § Goldens).
**Regression guard.** items 14 and 15 may both add a top Note to 0013, 0042, 0071 and 0079 — item 15's Note goes on its own line directly below item 14's Mechanism Note (after any existing top Notes), and its `## Amendment (2026-10-03)` sections are appended at the end of 0005 and 0071. The 0005 suffix and amendment supersede only the "Auto sub-agent still requires Confinement" bullet (ADR 0012; the child inherits `ConfineToWorkspace`) and the audit guardrail (0013 Amendment 2026-10-02); the ≤-parent decision and the tool-subset rule stay standing — the item yields to `internal/agent/subagent.go:1955-1957` ("a privilege expansion is structurally impossible — ADR 0005"). 0079's "0062 call 13" sits in its `Amends:` front matter (line 3), above any top Note, so its Acceptance is `grep -c "test-drivers.md" docs/adr/0079-*.md` ≥ 1.
**Goal:** 0005 and 0071 carry a status suffix naming what superseded them and a dated `## Amendment (2026-10-03)` stating the current rule; 0013, 0042 and 0079 carry a one-line top Note correcting the status/citation; no ADR body text is rewritten.
**Approach (assumed at the header base):** derive each current rule from the superseding ADR (0013 Amendment 2026-10-02, 0076 A9, 0012) and the code it names; follow the amendment shape in `docs/adr/0013-*.md`.
**Files:** `docs/adr/0005-*.md`, `docs/adr/0071-*.md`, `docs/adr/0013-*.md`, `docs/adr/0042-*.md`, `docs/adr/0079-*.md`
**Read first:** `docs/adr/0005-*`; `internal/agent/subagent.go` — defaultSubAgentTools, requestedChildTools, newChildAgentOn (ConfineToWorkspace); `docs/adr/0013-*` — Decision 4 "Superseded 2026-09-15", Amendment (2026-10-02); `docs/adr/0076-*` — A9; `docs/adr/0071-*` — front matter status, D3; `docs/adr/0079-*` — front matter Amends;
`docs/design/test-drivers.md` — Goldens; `docs/adr/0004-*` — status line
**Depends on item 14** (0042 shared).
**Tests:** none (docs).
**Acceptance:** `grep -c "Amendment (2026-10-03)" docs/adr/0005-*.md docs/adr/0071-*.md` shows 1 each; `grep -c "test-drivers.md" docs/adr/0079-*.md` ≥ 1.
**Commit:** `docs(adr): mark 0005 and 0071 superseded, fix 0013/0042/0079 citations`
