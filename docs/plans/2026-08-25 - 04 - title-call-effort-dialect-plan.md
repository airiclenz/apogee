# Plan: the out-of-band title/naming call follows the bound server's effort dialect

**Goal:** the session-naming completion (`internal/title`, wired by `cmd/apogee/title.go`) carries
the bound server's thinking-effort **dialect** the way the conversational path does
(`internal/agent/wire.go:48`), so its "answer without thinking" ask (`provider.EffortOff`) reaches
an OpenRouter (`reasoning`) or OpenAI/Groq (`openai`) server in the shape that server reads — and a
thinking model there no longer reasons to the token cap on an eight-word title
(`title.ErrTruncated`). Today `title.Prompt` states no dialect (`internal/title/title.go:166`), so
the call always emits the zero `provider.EffortDialectNone` = the historical `chat_template_kwargs`
shape, which a non-llama.cpp server ignores.

**Date:** 2026-08-25
**Status:** ready to execute
**Sized for:** ~200k-context host
**Authoritative sources:**

- ISSUES.md entry "The out-of-band title/naming call is dialect-blind" (open defect, 2026-08-25) —
  the defect statement; this plan closes it.
- [ADR 0060](../adr/0060-effort-is-detected-passively-dialected-per-server-and-picked.md) — the
  dialect model: detection is passive and belongs to the beat; the dialect rides the rebind spec
  to the engine; the zero dialect reproduces the historical bytes. Its "Considered and rejected"
  list carries the deferral this plan resolves (line ~147).
- `internal/provider/client.go:537` `applyEffort` — the one mapping from (dialect, effort) to wire
  shape. The naming call must NOT re-implement any of it; it only states the dialect.
- `internal/agent/wire.go:14-48` — the precedent: intent (`ThinkingEffort`) and dialect
  (`EffortDialect`) are stamped together where the request is built.
- `cmd/apogee/wire_settings.go:294` `liveSettings.observe` / `observedDialect` — the one latch of
  the observed dialect the composition root already writes on every rebind
  (`cmd/apogee/wire_verbs.go:41`). It is the source the naming call reads.
- ADR 0022 addendum (2026-07-31): the naming call is not a Mechanism and not a Turn; it emits no
  events and touches no engine state. This plan keeps that posture — nothing here goes through the
  Agent.

**Ratified design calls** (owner, 2026-08-25, via AskUserQuestion at write time):

1. **The dialect enters the request as a `title.Prompt` parameter** —
   `Prompt(prompts []string, workspaceBase string, date time.Time, dialect provider.EffortDialect)`
   stamps `EffortDialect` beside `ThinkingEffort`, mirroring `internal/agent/wire.go:48`. Rejected:
   the wiring stamping `req.EffortDialect` after `Prompt` returns (leaves `Prompt`'s "builds the
   request" doc half-true).
2. **`titleWiring` learns the dialect through a closure over `liveSettings.observedDialect`** —
   `newTitleWiring(binding func() upstreamBinding, dialect func() provider.EffortDialect, workspace string)`,
   wired at `cmd/apogee/wire_live.go:290` to `w.live.observedDialect`. The zero before the first
   beat lands keeps the historical `chat_template_kwargs` shape, exactly as the engine's
   `a.effortDialect` does. Rejected: widening `upstreamBinding` with an `EffortDialect` field (a
   second latch of a fact `liveSettings` already owns, needing a new writer on the beat path);
   widening `tui.Options.GenerateTitle` to carry it from `m.hb.observedDialect` (moves a server
   fact the root owns into the TUI seam).
3. **The 4xx fallback re-send skips a server whose dialect is `off`** — `applyEffort` emits
   nothing on `EffortDialectOff`, so a re-send with the effort dropped is byte-identical and would
   only take a queue slot ahead of the user's next Exchange. Every other dialect keeps the one
   re-send with `ThinkingEffort` cleared, unchanged. Rejected: leaving the fallback dialect-blind.

**Standing requirements:**

- `skills: coding-standards`
- Any authorized deviation from an item's text lands as a dated `NOTES:` line under that item.
- No version identifier changes (VERSION, CHANGELOG release heading, tags) — see the closing note.
- The naming call stays wire-shape-agnostic: it states a dialect and an intent, and
  `provider.applyEffort` alone decides the bytes. No item adds a dialect `switch` outside
  `internal/provider`.

**Out of scope:**

- The engine's own construction seed for `a.effortDialect` (the Firing-block Driver gap — ISSUES.md
  "Effort detection and the effort picker" residual 2). Different seam, different plan.
- The enriched turn-error hint's kwargs-only gate (residual 1 of the same entry).
- Any change to `provider.applyEffort`, the dialect vocabulary, detection, or `effort-dialect:`.
- The TUI's `GenerateTitle` seam signature and `autotitle.go` — untouched.
- The bench / headless drivers — none names sessions.

---

## 1. `title.Prompt` states the dialect and the wiring reads it from the live latch — ✅ DONE (2026-08-25)

NOTES (2026-08-25): the root-wiring assertion took the item's second option — `runRoot`'s recording seam records only `tui.Options` and never exposes the `rootWiring`, so `TestRunRootTitleSeamReadsTheObservedDialect` builds one the way `wire_test.go`'s `rosterSwitchWiring` does and pins `w.titles.dialect` against `w.live.observedDialect()` after an `observe`; `TestRunRootWiresTheTitleSeam` is left unchanged.

**What:**

- `internal/title/title.go`: widen `Prompt` to
  `Prompt(prompts []string, workspaceBase string, date time.Time, dialect provider.EffortDialect) provider.Request`
  and set `EffortDialect: dialect` in the returned `provider.Request`, beside `ThinkingEffort:
  provider.EffortOff`. Amend `Prompt`'s doc comment: after the "asks for no reasoning pass"
  sentence, state that the ask reaches the server in whichever dialect the caller names — the
  bound server's, as the beat observed it (ADR 0060) — and that the zero dialect keeps the
  historical `chat_template_kwargs` shape; the mapping to bytes is `provider.Client`'s, never this
  package's. The package doc's "two pure functions" framing is unchanged — `Prompt` stays pure; the
  dialect is one more input.
- `cmd/apogee/title.go`: add a `dialect func() provider.EffortDialect` field to `titleWiring`
  (doc: "reads the effort wire dialect the last beat observed for the bound server; wired to
  `liveSettings.observedDialect`. Read at call time for the reason `binding` is: a `/server`
  switch or a rebind between two namings must name in the dialect of the server the session is
  on NOW"). Widen `newTitleWiring(binding func() upstreamBinding, dialect func() provider.EffortDialect, workspace string)`.
  In `generate`, call `title.Prompt(prompts, w.workspaceBase, w.now(), w.dialect())`. Update the
  `titleWiring` type doc's "everything the CALL is made of" sentence to list the dialect beside the
  endpoint, model and key.
- `cmd/apogee/wire_live.go:290`: `w.titles = newTitleWiring(w.holder.Binding, w.live.observedDialect, w.roots.workspace)`.
  Extend the comment above it: the dialect is read off the same live latch the rebind path writes,
  so the naming call and the conversational path can never disagree about the server's dial.
- Every existing caller of `title.Prompt` and `newTitleWiring` in tests passes
  `provider.EffortDialectNone` / `func() provider.EffortDialect { return provider.EffortDialectNone }`
  unless the test is about the dialect.

Standards that bind this item: keep `Prompt` a pure function (no reads of package or global state
for the dialect); the closure-over-latch shape is the one the `binding` field already uses — do not
introduce a second latch or a struct copy of `liveSettings`.

**Files:** `internal/title/title.go`, `internal/title/title_test.go`, `cmd/apogee/title.go`,
`cmd/apogee/title_test.go`, `cmd/apogee/wire_live.go`

**Tests:**

- `internal/title/title_test.go`: `TestPromptCarriesTheDialectItIsHanded` — a table over the five
  dialects (`EffortDialectNone`, `Kwargs`, `Reasoning`, `OpenAI`, `Off`): `Prompt(...).EffortDialect`
  equals the input and `ThinkingEffort` stays `provider.EffortOff` on every row. Existing `Prompt`
  tests updated for the new parameter with `provider.EffortDialectNone`;
  `TestPromptLeavesModelToTheClient` and `TestPromptSetsSamplingConstants` still pass unchanged in
  intent.
- `cmd/apogee/title_test.go`: `TestTitleGeneratorFollowsTheObservedDialect` — one wiring whose
  `dialect` closure reads a variable; three `generate` calls against `scriptedTitleServer`
  (one `{content, finishReason: "stop"}` attempt each), capturing bodies:
  - dialect `EffortDialectReasoning` → body contains `"reasoning"` with `"enabled":false` and does
    NOT contain `chat_template_kwargs`;
  - dialect `EffortDialectKwargs` → body contains `chat_template_kwargs` with
    `"enable_thinking":false` and no `"reasoning"` key;
  - dialect `EffortDialectOff` → body contains neither `chat_template_kwargs`, `"reasoning"` nor
    `reasoning_effort`.
  The assertion is on substrings of the raw body, the way
  `TestTitleGeneratorFallsBackWhenTheThinkingKwargIsRejected` already asserts.
- `cmd/apogee/title_test.go`: `TestTitleGeneratorFollowsTheCurrentBinding` gains nothing new but
  must keep passing with the widened constructor — the binding-follow property is separate from the
  dialect-follow property and stays pinned on its own.
- A root-wiring assertion: extend `TestRunRootWiresTheTitleSeam` OR add
  `TestRunRootTitleSeamReadsTheObservedDialect` — after `runRoot`, drive the recorded wiring's
  `live.observe(0, provider.EffortDialectReasoning)` (or the equivalent seam the test harness
  exposes — the implementer picks the existing recording seam; if none reaches `liveSettings` from
  the test, pin it instead at the `newTitleWiring` call by asserting `w.titles.dialect` is non-nil
  and returns what `w.live.observedDialect()` returns after an `observe`, in a unit test that
  builds a `rootWiring` the way neighbouring `wire_*_test.go` tests do). The property under test:
  the closure the root wires IS `liveSettings.observedDialect`, not a constant.

**Acceptance:**

```
go build ./... && go vet ./internal/title/ ./cmd/apogee/
go test -count=1 ./internal/title/ -run 'TestPrompt'
go test -count=1 ./cmd/apogee/ -run 'TestTitleGenerator|TestRunRoot'
grep -n "EffortDialect: *dialect" internal/title/title.go
grep -n "w.live.observedDialect" cmd/apogee/wire_live.go
```

**Commit:** `feat(title): name the session in the bound server's effort dialect`

---

## 2. The fallback re-send knows the dialect it is dropping

Depends on item 1.

**What:**

- `cmd/apogee/title.go` `respondDroppingThinkingOff`: the re-send guard becomes
  `err != nil && req.ThinkingEffort == provider.EffortOff && req.EffortDialect != provider.EffortDialectOff && rejectedOutright(err)`.
  On `EffortDialectOff` nothing effort-related was on the wire (`applyEffort`'s first case), so the
  4xx cannot be about it and a re-send would be byte-identical (ratified design call 3).
- Rewrite the function's doc comment so it no longer names one dialect's field. Today it says the
  intent "rides as a chat_template_kwargs object, which llama.cpp accepts and a stricter
  OpenAI-compatible server may reject". Replace with: the intent rides in whichever dialect the
  request states (ADR 0060) — llama.cpp's `chat_template_kwargs`, OpenRouter's `reasoning` object,
  OpenAI/Groq's `reasoning_effort` (spelled `minimal` for off) — and a server that does not read
  that shape may reject the field outright; the one re-send drops the ask and falls back on the
  raised token cap. Keep the paragraph that limits the re-send to `EffortOff` and the paragraph on
  the retries-OFF contract; add one sentence naming the `off`-dialect exclusion and why.
- The `generate` doc's "a thinking model on a server whose template ignored the switch" sentence
  stays: with the dialect threaded, that is now precisely the un-dialected (zero) case and the
  `ErrTruncated` naming is still correct for it.

**Files:** `cmd/apogee/title.go`, `cmd/apogee/title_test.go`

**Tests:**

- `cmd/apogee/title_test.go`: extend `TestTitleGeneratorDropsOnlyTheThinkingOffKwarg`'s table
  (rename it `TestTitleGeneratorDropsOnlyTheThinkingOffAsk` — it is no longer about one kwarg) with
  an `dialect provider.EffortDialect` column; existing rows carry `EffortDialectNone`. Add rows:
  - `"thinking off on the reasoning dialect is dropped and re-sent"` — `EffortOff`,
    `EffortDialectReasoning`, script rejection + reply, `wantPosts: 2`; additionally assert the
    first body contains `"reasoning"` and the second does not.
  - `"thinking off on the openai dialect is dropped and re-sent"` — `EffortOff`,
    `EffortDialectOpenAI`, `wantPosts: 2`; first body contains `reasoning_effort`, second does not.
  - `"thinking off on the off dialect has nothing on the wire to drop"` — `EffortOff`,
    `EffortDialectOff`, script rejection only, `wantPosts: 1`, `wantErr: true`.
- `TestTitleGeneratorFallsBackAtMostOnce` and
  `TestTitleGeneratorFallsBackWhenTheThinkingKwargIsRejected` keep passing unchanged (both are the
  zero-dialect case).

**Acceptance:**

```
go build ./cmd/apogee/ && go vet ./cmd/apogee/
go test -count=1 ./cmd/apogee/ -run 'TestTitleGenerator'
grep -n "EffortDialectOff" cmd/apogee/title.go
! grep -n "rides as a chat_template_kwargs object" cmd/apogee/title.go
```

**Commit:** `fix(title): skip the thinking-off re-send when the dialect put nothing on the wire`

---

## 3. Record the closure: ADR 0060 amendment and the ISSUES.md entry

Depends on item 2.

**What:**

- `docs/adr/0060-effort-is-detected-passively-dialected-per-server-and-picked.md`: the
  "Considered and rejected" bullet **Threading the dialect into the out-of-band title/naming
  call** gains a dated resolution sentence at its end: "*Resolved 2026-08-25* — the namer now
  states the dialect the beat observed (`title.Prompt` takes it; the composition root reads it off
  the same live latch the rebind path writes), so `off` reaches every dialect in its own shape and
  the fallback re-send is skipped where the dialect put nothing on the wire." Add one bullet under
  **Consequences**: the out-of-band naming call carries the observed dialect, read from the
  composition root's live latch, never from the engine (ADR 0022 addendum — it is not a Turn).
- `ISSUES.md`: REMOVE the whole "### The out-of-band title/naming call is dialect-blind" entry
  (the heading, its Status line, its one `[ ]` bullet, and the `---` separator that precedes it),
  per the file's convention — a resolved item leaves ISSUES.md and lives in `CHANGELOG.md` only.
  Do not touch the "Effort detection and the effort picker" entry above it; its residuals are
  out of scope here.
- `CONTEXT.md` Thinking-effort entry (line ~334) and `docs/manual/` need no change: neither
  states which dialect the naming call sends. The verifier confirms this by grep rather than by
  edit.

**Files:** `docs/adr/0060-effort-is-detected-passively-dialected-per-server-and-picked.md`,
`ISSUES.md`

**Tests:** none — docs only.

**Acceptance:**

```
grep -n "Resolved 2026-08-25" docs/adr/0060-effort-is-detected-passively-dialected-per-server-and-picked.md
! grep -n "title/naming call is dialect-blind" ISSUES.md
! grep -rn -i "naming call.*chat_template_kwargs\|chat_template_kwargs.*naming call" CONTEXT.md docs/manual/
go build ./...
```

**Commit:** `docs(effort): close the title-call dialect deferral in ADR 0060 and ISSUES.md`

---

## Suggested version bump

Micro (`0.x.y` → `0.x.y+1`): one shipped user-facing fix (titles on OpenRouter/OpenAI-dialect
thinking models no longer truncate) with no API break — `title.Prompt` is an internal package. The
owner decides; no item here changes `VERSION`.
