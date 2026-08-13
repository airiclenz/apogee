# In-band retry and prompt-asset sweep — one re-stream per Turn, the literal sweep, and doc hygiene

**Goal:** close five open `ISSUES.md` entries: a transient in-band provider error mid-stream
faults the whole exchange with no retry (the anchor defect, `ISSUES.md:52`); the ~9 prompt
literals still hard-coded in Go rather than embedded assets (`:185`); the missing
`internal/title/prompts/` README pinning the wording-drift rule (`:181`); the un-swept stale
command-registry flag prose predating `runsBareAtAccept` (`:173`); and the misplaced
`## Conventions` heading in `ISSUES.md` itself (`:193`).

**Date:** 2026-08-13
**Status:** unexecuted
**Sized for:** ~200k-context host
**Skills:** coding-standards

**Authoritative sources:**
- `ISSUES.md:52-66` — the mid-stream defect entry; its own text ratifies the retry policy
  (retryable classes = the client's HTTP classes: 429 / 5xx / `provider_unavailable`; in-band
  4xx stays terminal; exactly one re-stream of the Turn). Observed failure: session
  `20260813T100440Z-104eaf7a`, OpenRouter in-band
  `{"error":{"code":502,…,"error_type":"provider_unavailable"}}`.
- Explore scout reports, 2026-08-13 (this plan's write session) — every file:line below was
  verified there.
- `internal/provider/client.go` retry loop (`send`, `:220-263`; `isRetryableStatus` `:470`) —
  the class policy the in-band classification mirrors.
- The embed-pattern precedent: `internal/mechanisms/toolloop.go:116-138` (`promptFS` +
  `mustPrompt`, CRLF-normalised, one trailing newline stripped, sentence-joining spaces stay
  in Go) and `internal/probe/prompts/README.md` (the wording-drift README template).
- [ADR 0031] door-keeping invariants — events are the loop's to emit, so the re-stream (and
  its `StreamResetEvent`) is loop-driven; the provider never retries mid-stream itself.

**Ratified design calls (owner + plan author, 2026-08-13):**
1. **Scope** (owner, via AskUserQuestion at plan-write time): the mid-stream retry defect
   (owner instruction), the prompt-literals embed sweep, and the doc/hygiene trio (flag-prose
   sweep, title README, Conventions heading). The `configwrite.go` split was offered and NOT
   selected — it stays parked for its own plan.
2. **Retry policy** (owner, via the ISSUES entry text): an in-band mid-stream error retries
   iff its class would retry at the HTTP layer — integer code 429 or ≥ 500, or
   `error_type == "provider_unavailable"`. An in-band 4xx, a non-numeric/unparseable code
   without that error_type, and `DeltaContextOverflow` stay terminal. Exactly ONE re-stream
   per Turn, then the fault surfaces exactly as today.
3. **Layering** (plan author, binding): classification lives in the provider (it owns the wire
   retry policy; the classifier reuses `isRetryableStatus`), carried outward as a new
   `Retryable bool` on `provider.Delta`. The re-stream itself is loop-driven: only the loop
   emits events (`StreamResetEvent`), and the provider's `Stream` contract is unchanged apart
   from the new field.
4. **Re-stream mechanics** (plan author, binding): a one-shot per-Turn latch beside `foldSpent`
   (`internal/agent/turn.go:34-41` precedent), independent of `maxPostResponseRetries`
   (which bounds hook-driven ActionRetry). On a retryable failed reply with the latch unspent:
   spend the latch, emit `StreamResetEvent` (the `loop.go:359` precedent — the TUI already
   discards the run's pending buffer and shows retrying), wait a fixed 1s context-cancellable
   delay, and re-stream the SAME `t.req` — never rebuild via `armRequest`/`buildRequest`,
   which would re-drain the deferred queue. No `ErrorEvent` for the recovered attempt (the
   overflow-recovery silence precedent, `loop.go:322-326`); the retry's own failure surfaces
   exactly as today. The double `usage.record`/`tokens.Calibrate` from two attempts is
   accepted — both attempts genuinely consumed tokens; recording both is honest.
5. **Asset wording is frozen** (plan author, binding): the sweep moves every literal
   byte-for-byte (the `@pin` sim-provenance comments require the A/B-measured wording); the
   `@pin`/rationale comments stay in Go beside the `mustPrompt` vars; assets carry no comments
   (house rule — a comment in a `.txt` would reach the model). Marker consts stay in Go, and a
   new pin test asserts each marker remains a substring of its asset-loaded directive — the
   coupling `AppendToSystem` idempotency depends on must not become an unchecked cross-file
   invariant.
6. **The tenth literal** (plan author, binding): the library injection-block header at
   `internal/mechanisms/library.go:493` (which embeds `libraryInjectionMarker`) joins the
   sweep — same kind of prompt text, flagged un-swept by the write-time scout; the ISSUES
   entry's "~9" is approximate.
7. **ADR 0028 handling** (plan author, binding): decision 7's stale accept-behaviour sentence
   gets a dated inline amendment note (matching the ADR-0029 note already inside that same
   decision block); the body is otherwise left as written.
8. **Conventions heading** (plan author, binding): the `## Conventions` heading moves above
   the intro paragraph, so the "Two sections:" sentence and the bullets it introduces are
   contiguous under it.
9. **Title pins** (plan author, binding): the new README mirrors
   `internal/probe/prompts/README.md`'s structure with the obligation adapted (no version
   constant — the pin tests ARE the gate), and the two identity-only-guarded assets
   (`user-instruction.txt`, `window-header.txt`) gain literal text pins.

**Out of scope:**
- Mid-stream TRANSPORT faults (a dropped connection while reading the SSE body) — the ISSUES
  entry names in-band error members only.
- Honoring a Retry-After equivalent inside in-band error metadata.
- `ReasoningEvent`/`UsageEvent` carrying no retraction signal on a re-stream — pre-existing
  for every `StreamResetEvent` producer (ActionRetry included), not this plan's.
- The empty-reply sibling (`finish: stop`) — `empty_response_recovery` has first claim
  (`ISSUES.md:64-66`).
- The `configwrite.go` split (design call 1), the approved-out-of-workspace-write Execute gap
  (pending owner call), `doc.go` file maps for `internal/context`/`internal/title`
  (`ISSUES.md:190` — deliberately under the docmap threshold).
- Version bumps (see the closing note).

---

## 1. Verify the residuals fix-wave plan is archived — ✅ DONE (2026-08-13)

NOTES (2026-08-13): gate passed — `docs/plans/archived/2026-08-13 - 04 - residuals-fix-wave-plan.md` exists, `docs/plans/2026-08-13 - 04 - residuals-fix-wave-plan.md` does not. Working tree clean and `go build ./...` succeeds, so the residuals run left no transient breakage.

**What:** Confirm `docs/plans/2026-08-13 - 04 - residuals-fix-wave-plan.md` no longer exists
at that path and `docs/plans/archived/2026-08-13 - 04 - residuals-fix-wave-plan.md` does —
that plan is executing as this one is written, edits `ISSUES.md` and `README.md` (files this
plan also touches, items 7–8), and leaves the repo-wide build transiently broken between its
items. If it is not yet archived, report BLOCKED — this plan waits.

**Files:** none (verification only).

**Tests:** none.

**Acceptance:** `ls "docs/plans/archived/2026-08-13 - 04 - residuals-fix-wave-plan.md"`
succeeds and `ls "docs/plans/2026-08-13 - 04 - residuals-fix-wave-plan.md"` fails.

**Commit:** none (the item is a gate).

---

## 2. The provider classifies retryable in-band stream errors — ✅ DONE (2026-08-13)

Depends on item 1.

**What:** `wireError` (`internal/provider/wirejson.go:111-115`) parses only `message`, `code`,
`metadata` — `error_type` is dropped, so `provider_unavailable` survives only as raw text
inside `Delta.Err`. Add `ErrorType string` to `wireError`. In `inBandErrorDelta`
(`internal/provider/stream.go:97-104`), classify the error per design call 2 —
`isRetryableStatus(code)` (`client.go:470`) OR `error_type == "provider_unavailable"` — and
set a new `Retryable bool` field on `Delta` (`stream.go:39-47`, documented: meaningful only on
`DeltaError`; `DeltaContextOverflow` is never retryable). The rendered `Err` text and the
existing kind selection are unchanged.

**Files:** `internal/provider/wirejson.go`, `internal/provider/stream.go`,
`internal/provider/stream_test.go`

**Tests:**
- Extend the `TestStream_InBandError` table (`stream_test.go:182`) with `wantRetryable`
  expectations: the existing 502-after-content and 429-with-metadata cases → retryable; the
  non-numeric-code case → NOT retryable; new rows for an in-band 400 (not retryable) and a
  4xx/absent code carrying `"error_type":"provider_unavailable"` (retryable — the observed
  OpenRouter shape from session `20260813T100440Z-104eaf7a`).
- `TestStream_ContextOverflow` (`:146`) stays green — overflow kind unchanged, never
  retryable.

**Acceptance:** `go build ./... && go test ./internal/provider/`

**Commit:** `feat(provider): in-band stream errors carry the retryable classification`

---

## 3. The loop re-streams a retryable in-band fault once per Turn — ✅ DONE (2026-08-13)

NOTES (2026-08-13): `respondAndReview` now takes the `*turnRun` instead of `(turn int, req *domain.Request)` — it must read and write the Turn's latch — and the ActionRetry counter moved out of the loop header into the retry branch, so a re-stream cannot spend a hook's retry budget (the item's "independent of `maxPostResponseRetries`"); the cap's own behaviour is unchanged (3 retries, 4 streams).

NOTES (2026-08-13): the 1s hold-off is a package `var restreamHoldoff` rather than a const, solely so the new tests need not sit through it (they shrink it serially and restore it via `t.Cleanup`); the production wait is the fixed second the item specifies.

Depends on item 2.

**What:** Carry `Delta.Retryable` into the loop's `reply` (`internal/agent/loop.go:495-503`;
folded at `:563-570`) and implement the one-shot re-stream per design call 4. In
`respondAndReview`'s attempt loop (`:328`), where a failed non-overflow reply currently
surfaces one `ErrorEvent` and returns `turnFailed` (`:333-339`): when the reply is retryable
and the Turn's new latch (beside `foldSpent`, `internal/agent/turn.go:34-41`) is unspent —
spend it, emit `StreamResetEvent` (`:359` precedent), wait 1s (context-cancellable; a
cancelled wait surfaces the fault as today), and continue the loop re-streaming the same
`t.req`. A non-retryable fault, or a second fault of any class, takes today's path unchanged —
so a delegated exchange (`internal/agent/subagent.go:109-121`, unchanged) survives one blip
because its own loop recovers before `Faulted` is ever set.

**Files:** `internal/agent/loop.go`, `internal/agent/turn.go`,
`internal/agent/overflow_test.go`, `internal/agent/subagent_test.go`

**Tests:**
- New (beside `TestRespondAndReviewSplitsOverflowFromPlainFault`, `overflow_test.go:55`, using
  its `faultResponder`/`recoveryResponder` shapes — the per-request fault-then-success script
  is `internal/agent/overflowrecovery_test.go:27`): a retryable fault then success → `turnOK`,
  exactly one `StreamResetEvent`, NO `ErrorEvent`; a retryable fault twice → `turnFailed`, one
  `StreamResetEvent`, one `ErrorEvent`; a non-retryable fault → `turnFailed` immediately, no
  reset event; the latch is per-Turn (a later Turn in the same session retries again).
- New in `subagent_test.go` (beside `TestSubAgent_FaultedDelegationReportsAsError`, `:495`): a
  delegated exchange whose child hits one retryable blip completes with its result — no
  "sub-agent faulted" tool result.
- Existing stays green: `TestRespondAndReviewSplitsOverflowFromPlainFault`,
  `TestStep_RetryEmitsStreamReset` (`statemachine_test.go:710`), the
  `internal/agent/emptyreply_test.go` suite.

**Acceptance:** `go build ./... && go test ./internal/agent/`

**Commit:** `feat(agent): one bounded re-stream per Turn recovers a transient in-band fault`

---

## 4. The mechanisms' prompt literals move into embedded assets — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the item's Tests list did not name `toolloop_test.go`, but its `TestEmbeddedDirectivePromptsLoad` is the prompts/ roster (it fails any embedded asset it does not pin), so its table gained the nine new assets (0 `%s` verbs each) and its comment now says "this package's prompt assets" rather than "the directive's".

NOTES (2026-08-13): `library.go`'s injection-block header is the whole header line as one asset (`library-injection-header.txt`), including the marker text, so the pin test's `libraryInjectionMarker ⊂ header` assertion has something to hold; the new `libraryInjectionHeader` var replaces the `marker + " for this model…"` concatenation at the call site and the `"\n"` is appended in Go, per the item's trailing-newline instruction. Rendered output is byte-identical.

NOTES (2026-08-13): the assets were generated by dumping the live const values from a throwaway in-package test (deleted after the dump) rather than retyped, so the move is byte-for-byte by construction.

Depends on item 1.

**What:** Move the eight `internal/mechanisms` literals plus the tenth (design call 6) into
`internal/mechanisms/prompts/` via the existing `promptFS`/`mustPrompt`
(`toolloop.go:116-138`), each const becoming `var … = mustPrompt("<name>.txt")`, wording
byte-for-byte (design call 5), `@pin` comments staying beside the vars. Assets and names:
`cot-tool-use-directive.txt` (`cot.go:69`), `cot-stall-directive.txt` (`:75`),
`cot-list-nudge-directive.txt` (`:79`), `decompose-focus-directive.txt` (`decompose.go:38`),
`decompose-continuation-directive.txt` (`:44`), `library-tool-use-note.txt` (`library.go:97`),
`library-shallow-note.txt` (`:101`), `completion-check-nudge.txt` (`emptyresponse.go:56`),
`library-injection-header.txt` (the block header at `library.go:493` — keep its trailing
newline semantics by appending `"\n"` in Go, per the sentence-joining rule). Marker consts
(`cot.go:82-84`, `decompose.go:47-48`, `library.go:64`) stay in Go. Update
`internal/mechanisms/doc.go`: the `# The prompt assets` section (`:76-84`) drops its
"follow-up sweep" closing sentence and states the marker-substring rule + names the new pin
test; the "one file here that embeds prompt assets" sentence (`:53-54`) is corrected.

**Files:** `internal/mechanisms/cot.go`, `internal/mechanisms/decompose.go`,
`internal/mechanisms/library.go`, `internal/mechanisms/emptyresponse.go`,
`internal/mechanisms/doc.go`, `internal/mechanisms/prompts/` (nine new `.txt` assets),
`internal/mechanisms/prompts_test.go` (new)

**Tests:**
- New `prompts_test.go`: every marker is a substring of its asset-loaded directive —
  `cotToolUseMarker` ⊂ `cotToolUseDirective`, `cotStallMarker` ⊂ `cotStallDirective`,
  `cotListNudgeMarker` ⊂ `cotListNudgeDirective`, `decomposeFocusMarker` ⊂
  `decomposeFocusDirective`, `decomposeContinuationMarker` ⊂
  `decomposeContinuationDirective`, `libraryInjectionMarker` ⊂ the loaded injection header —
  with a failure message saying a re-worded asset that drops its marker makes the directive
  inject twice.
- Existing stays green unmodified: `cot_test.go:92/:151/:186`,
  `readlistfamilies_test.go:34`, `decompose_test.go:94-178`, `emptyresponse_test.go:26`,
  `library_test.go`, and — the cross-package verbatim pin —
  `internal/agent/wave1delivery_test.go:26` (byte-for-byte move keeps it green; if it fails,
  the asset text drifted).

**Acceptance:** `go build ./... && go test ./internal/mechanisms/ ./internal/agent/`

**Commit:** `refactor(mechanisms): the remaining prompt literals become embedded assets`

---

## 5. `overflowBridge` becomes an embedded asset — ✅ DONE (2026-08-13)

Depends on item 1.

**What:** Move `overflowBridge` (`internal/agent/compact.go:212`) to
`internal/agent/prompts/overflow-bridge.txt` — the package's first assets dir. Embed in
`compact.go` itself (the `internal/context` precedent embeds in its `compact.go:94-116`; no
new `.go` file, so the docmap count is untouched): the same `promptFS` + `mustPrompt` idiom,
byte-for-byte wording, the role-rationale comment (`:205-211`) staying beside the var. Update
`internal/agent/doc.go`'s `compact.go` narration (`:44-46`) to name the asset.

**Files:** `internal/agent/compact.go`, `internal/agent/prompts/overflow-bridge.txt`,
`internal/agent/doc.go`

**Tests:** existing identity pins stay green unmodified —
`internal/agent/overflowrecovery_test.go:161/:175/:379`, `predictiveguard_test.go:123`,
`emergencyfold_test.go:70`; `TestDocMapNamesEveryFile` (`docmap_test.go`) stays green.

**Acceptance:** `go build ./... && go test ./internal/agent/`

**Commit:** `refactor(agent): the overflow bridge line becomes an embedded asset`

---

## 6. `internal/title/prompts/` gets its wording-drift README and the missing text pins

Depends on item 1.

**What:** Add `internal/title/prompts/README.md` mirroring
`internal/probe/prompts/README.md`'s structure (design call 9): an H1 stating the consequence
of an edit, why the wording is load-bearing (the title prompt shapes every session name), the
concrete obligation — here, updating the named pin tests, since title has no version
constant — the "no comments in a `.txt`; this README is not embedded" rule, and the pin
tests by name. Then make the README's claim true: `title_test.go`'s pins at `:102` and `:246`
compare by identity (`userInstruction`, `windowHeader`) and survive a re-wording — add literal
text pins for `user-instruction.txt` ("Reply with the title only.") and `window-header.txt`
("The user's requests in this session, oldest first:") beside the existing
`TestSystemInstructionAsksForTheDominantThreadBiasedRecent` (`:426-442`) pattern.

**Files:** `internal/title/prompts/README.md` (new), `internal/title/title_test.go`

**Tests:** the item IS the pins; the whole `internal/title` suite stays green.

**Acceptance:** `go test ./internal/title/`

**Commit:** `test(title): the prompt assets get a drift README and literal pins`

---

## 7. Sweep the stale pre-`runsBareAtAccept` accept prose

Depends on item 1.

**What:** Correct the four stale sites the write-time sweep found (and close the
`ISSUES.md:173` question — no others remain; `layout.md`, `CONTEXT.md`, ADR 0027's frozen
body, and the corrected `internal/tui` comments were checked clean):
- `docs/adr/0028-…-first-beat-completes-it.md:148-150` — decision 7 says the dropdown
  "completes" `/model`/`/server` "rather than running them"; add a dated inline amendment note
  (design call 7) stating both now run bare at accept per `runsBareAtAccept`
  (`internal/tui/command.go:94`), body otherwise untouched.
- `internal/tui/doc.go:103-104` — "accepting a command row RUNS the command" gains the
  carve-out: an argument-taking row completes instead unless it is `runsBareAtAccept`.
- `internal/tui/doc.go:119-120` — "the one verb that takes arguments" is stale (six rows
  now); reword to match its own paragraph at `:116-118`. The compressed "accept-executes"
  label at `:124` reads as the ADR-0027 decision name and stays.
- `internal/tui/minilang_test.go:765` — "The one arg-taking verb" comment corrected to the
  rule its sibling at `:882` already states.
- `README.md:233-234` — "Accepting a command from the menu **runs it and keeps the rest of
  your draft**" gains one clause naming the exception (argument-taking commands complete;
  `/model` and `/server` run bare).

**Files:**
`docs/adr/0028-a-server-switch-rehomes-the-session-and-the-first-beat-completes-it.md`,
`internal/tui/doc.go`, `internal/tui/minilang_test.go`, `README.md`

**Tests:** `go test ./internal/tui/` stays green (comment/doc edits only).

**Acceptance:** `go build ./internal/tui/ && go test ./internal/tui/`

**Commit:** `docs(tui): sweep the stale pre-runsBareAtAccept accept prose`

---

## 8. `ISSUES.md`: remove the five closed entries and seat the Conventions heading

Depends on items 2–7.

**What:** Remove from `ISSUES.md` the five entries this plan closed: the mid-stream in-band
retry defect (`:52-66`, items 2–3), the prompt-literals sweep entry (`:185-188`, items 4–5),
the `internal/title/prompts/` README entry (`:181-183`, item 6), the flag-prose sweep entry
(`:173-176`, item 7), and the Conventions-heading entry (`:193-195`) — closing that last one
in the same edit by moving the `## Conventions` heading above the intro paragraph (design
call 8). Residual-section headers left empty by these removals are removed with their
entries. Nothing else moves; the closed trail is the per-item `CHANGELOG.md` entries the
run's verifiers wrote (house rule: `ISSUES.md` holds open work only).

**Files:** `ISSUES.md`

**Tests:** none (docs-only).

**Acceptance:** `grep -c "inBandErrorDelta" ISSUES.md` returns 0,
`grep -c "runsBareAtAccept" ISSUES.md` returns 0, `grep -n "## Conventions" ISSUES.md` shows
the heading above the "Two sections:" sentence, and `git diff --stat` for the item touches
only `ISSUES.md`.

**Commit:** `docs(issues): close the in-band-retry and prompt-asset-sweep entries`

---

**Suggested version bump:** items 2–3 are a user-visible resilience feature (a transient
upstream blip no longer kills an exchange or a delegated sub-agent run); per the house
per-feature micro-bump policy one micro-bump after execution is warranted — the owner
decides; no plan item changes `VERSION`.
