# Session auto-titling and /rename — implementation plan

- **Goal:** on the first prompt of a new session, fire a small out-of-band completion
  that names the Session record from the prompt (workspace and date as context), applied
  through the existing Rename path; add a `/rename` command — bare form regenerates via
  the LLM, `/rename <text>` sets the title manually; the automatic call is gated by a new
  `auto-title` config key (default on).
- **Date:** 2026-07-31 · **Status:** not started
- **Authoritative sources:**
  - The **Ratified design** block below — settled with the owner in the 2026-07-31 grill
    session. Where any other document (including the ADR addendum item 1 writes)
    disagrees with that block, the block governs.
  - ADR 0022 (`docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md`)
    — the session-record architecture this feature extends; its Considered-options
    rejection of LLM titles (":142-144") is the decision item 1 flips.
  - ADR 0011 — the Agent is single-goroutine; the naming call must NOT go through the
    Agent. ADR 0024 — single-slot server posture (concurrent HTTP is established
    practice; a queued completion is expected, not an error).
  - CONTEXT.md terms: *Session record*, *Turn*, *Exchange*. The naming call is not a
    Turn and not a Mechanism (see Ratified design 1).
- **Standing requirements:** run `make check` before every commit; never touch `VERSION`
  or any CHANGELOG release heading (version bumps are the owner's call — see closing
  note); no AI-attribution trailers in commits; the Bubble Tea `Model` is copied by
  value — never let a `strings.Builder` or other no-copy type reach it (ADR 0011,
  `internal/tui/doc.go`); any authorized deviation from an item's text must land as a
  dated NOTES line under that item.
- **Out of scope:** a separate naming endpoint/model (naming always uses the session's
  current server + model); any new UI chrome for titles (no spinner, no live title in
  the transcript or footer — titles surface only where they already do: the session
  browser and the resume note); changes to the browser's `r` rename UX; retro-titling
  existing session records; the other ADR 0022 non-goals (session search, export,
  sub-agent persistence) stay non-goals; new CONTEXT.md terminology sections (the ADR
  addendum is the record); no version identifier changes.

## Ratified design (grilled 2026-07-31 — items below implement it)

1. **Call category.** The naming call is a *cosmetic* out-of-band completion — neither a
   Mechanism (fires at no Hook point, never shapes the primary call) nor structural like
   compaction (nothing breaks without it). It is exempt from Bypass reasoning, is not a
   Turn, emits no TokenEvent/UsageEvent, and never enters the transcript or moves the
   context gauge. It lives entirely in the TUI + composition root, so the bench/embedder
   path (which never constructs `tui.Options`) is untouched.
2. **Timing.** Fired at first-prompt submit, in parallel with the main call. On a
   single-slot server it queues behind Turn 1 and runs between Turns 1 and 2 — the
   cheapest possible KV-eviction point (context is at its smallest). The request timeout
   is therefore generous (it waits in queue for the whole first stream).
3. **Transport.** Never through the Agent (single-goroutine contract). A nil-able func
   seam on `tui.Options` — `GenerateTitle func(ctx, firstUserText) (string, error)` —
   backed at the composition root by its own `provider.Client` over the session's
   *current* server + model binding (the `probe.Chat` pattern). Nil seam ⇒ automatic
   naming never fires and bare `/rename` reports unavailability; never an error.
4. **Apply path.** The heuristic `sessionTitle` still stamps the first Save (unchanged);
   the generated title lands afterwards via the existing `Rename` path — `sessionHost.
   Save` ignores the title argument after the first call, so Rename is the only correct
   writer. A result arriving before the first Save mints an ID is stashed and applied on
   the first save-complete.
5. **Never clobber.** Any user-initiated rename (browser `r` or `/rename`) sets a
   `titleTouched` flag; a late-landing *automatic* title is dropped once the flag is
   set. Bare `/rename` is an explicit request, so its result applies even when touched
   (and leaves the flag set). Auto-naming fires once per new Session record — including
   after `/clear` / `/new` rotation — and never on a resumed session.
6. **Title contract.** Task description only, roughly 3–8 words; workspace basename and
   date are model *context*, never title text (the browser row already shows time and
   workspace). Sanitizer (single pipeline for generated *and* manual titles): strip a
   leading `<think>…</think>` block, take the first non-empty line — treating code-fence
   marker lines (` ``` ` with an optional language tag) as noise, since small models wrap
   output in fences even when told not to — strip ANSI/control escapes, surrounding
   quotes/backticks, a leading comment/heading marker (`//`, `#`) and a leading
   case-insensitive `Title:` label, collapse inner whitespace, trim a trailing period,
   word-boundary truncate to ≤50 runes with `…` (same caps as the heuristic). An empty
   result after sanitizing = failure.
7. **Config.** Flat `auto-title: *bool` in the config file only (no flag, no env), nil ⇒
   **true**. The toggle gates only the *automatic* firing; the seam stays wired so bare
   `/rename` regenerates on demand even with `auto-title: false`.
8. **`/rename` grammar.** `takesArgs`, idle-only (`whileRunning: false` — the bare form
   issues a completion that would contend with a live Exchange). With args: join with
   single spaces, sanitize, apply. Bare: regenerate via the seam. Before any prompt
   exists, bare form notes "nothing to name yet"; a *manual* `/rename <text>` before the
   first Save stashes the title (and sets `titleTouched`) for application at the first
   save-complete, overriding the heuristic.
9. **Failure posture.** Automatic path fails silently to the heuristic title (a
   maintenance nicety must never nag). Bare `/rename` failures get a quiet note
   suggesting `/rename <name>`.

## 1. ADR 0022 addendum and TODO.md non-goal removal — ✅ DONE (2026-07-31)

**What:** Add a dated (2026-07-31) addendum to `docs/adr/0022-sessions-persist-per-turn-
as-dual-representation-records.md` under its Considered-options section: the v1
rejection of LLM-generated titles is reversed; record the Ratified-design decisions that
belong to the architectural record — the cosmetic-call category (not a Mechanism, not
structural, exempt from Bypass, not a Turn, no events, bench path untouched), the
fire-at-submit-on-single-slot reasoning (queues behind Turn 1; KV eviction at minimum
context), same-server-same-model via an out-of-band `provider.Client` (never the Agent),
Rename as the only writer with stash-until-ID, the never-clobber rule, and the
`auto-title` default-on gate. In `TODO.md` (lines ~140-143, "Session system
follow-ons"): remove only the LLM-generated-titles entry from the deliberate-non-goals
list — session search, session export, and sub-agent session persistence stay.

**Tests:** none (docs only).

**Acceptance:** `grep -n "2026-07-31" docs/adr/0022-*.md` hits the addendum;
`grep -i "llm-generated titles" TODO.md` returns nothing; `make check` passes.

**Commit:** `docs(adr): adopt LLM session titles as a cosmetic out-of-band call`

## 2. internal/title: prompt builder and sanitizer — ✅ DONE (2026-07-31)

NOTES (2026-07-31): two additions inside the specified sanitize pipeline, both supersets of the
item's text rather than departures from it — (a) the affix strips (quotes/backticks, comment or
heading marker, `Title:` label) repeat until the string stops changing (bounded at 4 passes), so
`"Title: X"` and `Title: "X"` both reduce to `X`; (b) a fence opener *glued* to the text
(` ```fix the parser `, which never gets its own line and so cannot be skipped as a marker line)
is stripped with the other leading markers. Also: whitespace control characters (tab) are exempt
from the control-escape strip, since they collapse to a single space one step later and dropping
them would weld two words together.

**What:** New package `internal/title` with two pure, dependency-light pieces. (a)
`Prompt(firstPrompt, workspaceBase string, date time.Time)` returning the messages for
the naming completion: a short system prompt ("you name coding sessions; reply with the
title only, one line, 3–8 words, no quotes"), and a user message carrying the workspace basename,
the date, and the first prompt truncated to a bounded excerpt (~1500 runes) — context
only, per Ratified design 6. Return whatever message/request type the provider layer
consumes (mirror what `internal/agent/compact.go`'s completer builds); include the
sampling constants here: temperature 0.2 (compaction precedent), generous max_tokens
(≥512, to survive small models that think inline before answering). (b)
`Sanitize(raw string) (string, bool)` implementing the full pipeline of Ratified
design 6, ok=false on empty. The existing heuristic `sessionTitle`
(`internal/tui/model.go:1465-1492`) stays untouched — it remains the fallback stamped at
first Save.

**Tests:** `internal/title/title_test.go` — prompt: excerpt cap honored, workspace/date
present, instruction present. Sanitizer table: leading `<think>…</think>` stripped;
multiline reply → first non-empty line; code-fenced reply → fence marker lines (bare
` ``` ` and ` ```lang `) skipped as noise, inner line taken; reply that is only a fence
pair → ok=false; ANSI/control escapes stripped; surrounding quotes/backticks stripped;
leading `Title:` label stripped (case-insensitive); leading comment/heading marker
stripped (`// title: X`, `# X`); trailing period trimmed; inner whitespace collapsed;
50-rune word-boundary truncation with `…`; CJK/multibyte runes counted as runes;
all-noise input → ok=false.

**Acceptance:** `go test ./internal/title/` passes; `make check` passes.

**Commit:** `feat(title): session-title prompt builder and sanitizer`

## 3. Config: auto-title toggle, default on — ✅ DONE (2026-07-31)

**What:** In `cmd/apogee/config.go`: add `AutoTitle *bool \`yaml:"auto-title"\`` to
`fileConfig` (precedent: `AutoCompact`, config.go:711) and `autoTitle bool` to
`settings` (precedent: `settings.autoCompact`, :96-100); resolve nil ⇒ true in
`resolveSettings`; config-file only — no flag, no env — matching the newer-key house
rule; thread through `applyConfig`. In `cmd/apogee/defaults/config.yaml`: add a banner
section in house style (prose comment explaining the out-of-band naming call, that it
uses the session's server/model, queues behind the first response on single-slot
servers, and that `/rename` still works when disabled) with the single commented example
`# auto-title: false`.

**Tests:** extend the existing config resolution tests in `cmd/apogee`: absent key ⇒
true; explicit `auto-title: false` ⇒ false; explicit true ⇒ true; the seeded default
template still parses.

**Acceptance:** `go test ./cmd/apogee/` passes; `grep -n "auto-title"
cmd/apogee/defaults/config.yaml` hits the new banner; `make check` passes.

**Commit:** `feat(config): auto-title toggle, default on`

## 4. Composition root: GenerateTitle seam on tui.Options — ✅ DONE (2026-07-31)

Depends on items 2 and 3.

NOTES (2026-07-31): building the client from the *current* binding needed one thing the composition root
kept nowhere readable — the session's api key and its BOUND model (`upstreamHolder` held only the endpoint
and the Monitor). Rather than grow a second, quietly divergent notion of "the current Upstream", the holder
was extended to own all three (`Binding()`, `Swap(endpoint, apiKey, monitor)`, and `SetModel` now recording
as well as forwarding the hint), so `cmd/apogee/upstream.go` and its two test call sites are touched
alongside `wire.go` and the new `title.go`. A server switch CLEARS the bound model, mirroring what the same
move already does to the session record's stamped model.

**What:** In `internal/tui/tui.go`: add to `Options` — `GenerateTitle func(ctx
context.Context, firstUserText string) (string, error)` (nil-able, documented degrade
per Ratified design 3; structural twin: `SaveHostAcknowledgement`) and `AutoTitle bool`.
In `cmd/apogee` (wire.go, or a small `title.go` beside `probemodel.go`): build the
generator — construct a `provider.Client` from the *current* server + model binding
(the same state `SwitchServer` rebinds, so a per-call construction from that state is
the expected shape), `WithRequestTimeout` set from a named constant of ~5 minutes
(queue-tolerant per Ratified design 2), call `Respond` with `title.Prompt(...)` (date =
time.Now() at call time, workspace = the resolved workspace base), return the raw text —
sanitizing stays TUI-side so both title sources share it. Wire `AutoTitle` from
`settings.autoTitle`. The generator emits no events and touches no engine state.

**Tests:** in `cmd/apogee`: an `httptest`-backed unit test — generator returns the
completion text of a stubbed OpenAI-compatible response; a stub that exceeds the
deadline surfaces a context error. A live-gated `TestLiveGenerateTitle` (skips without
`APOGEE_LIVE_ENDPOINT`, precedent `TestE2ELiveModel`) that generates a title for a
canned prompt and asserts `title.Sanitize` accepts it.

**Acceptance:** `go test ./cmd/apogee/ ./internal/tui/` passes; `make check` passes.

**Commit:** `feat(tui): GenerateTitle seam wired to an out-of-band title client`

## 5. TUI: automatic naming state machine — ✅ DONE (2026-07-31)

Depends on item 4.

NOTES (2026-07-31): three departures from the item's literal text, all in the direction the Ratified
design points. (a) The apply path does NOT reuse `m.renameSession`: that Cmd re-lists, and
`foldSessionList` opens the /sessions overlay over every list it folds, so a generated title would pop
the browser open mid-answer — against the plan's own "no new UI chrome" scope. A quiet twin,
`setSessionTitle` (sessions.go, beside `renameSession`), renames off the loop and reports nothing.
(b) `resumeLoaded` — the `/sessions` browser's resume — also latches `autoTitleFired` and drops
`pendingTitle`; the item names only the `ResumedSession` replay, but Ratified design 5 says auto-naming
never fires on a resumed session, and without this a fresh launch that resumes an old record before its
first prompt would rename that record. (c) `maybeAutoTitle` also requires a wired `SessionHost`: with no
persistence there is no Session record to name, so the call would spend a queue slot on a result with
nowhere to go. Also: `internal/tui/doc.go`'s file-by-file narration gained the new file, since it claims
to name every file in the package.

**What:** In `internal/tui` (model.go + a focused new file if cleaner): on user-prompt
submit, when `opts.AutoTitle && opts.GenerateTitle != nil` and this is the first prompt
of a fresh (non-resumed) Session record, batch a `tea.Cmd` that calls `GenerateTitle`
with the submitted text and delivers an `autoTitleMsg{title string, err error}`. Track
three fields on the Model: `autoTitleFired`, `titleTouched`, `pendingTitle string`. On
`autoTitleMsg`: drop silently on error, failed `title.Sanitize`, or `titleTouched`;
otherwise apply via the existing `m.renameSession(id, …)` Cmd (`internal/tui/
sessions.go:279-286`) when `ActiveID` exists, else stash in `pendingTitle`. On the first
successful save-complete (`saveComplete`, model.go:1445), apply a non-empty
`pendingTitle` via Rename and clear it. Set `titleTouched` when the browser's `r` rename
commits. Reset all three fields in `startNewSession` (rotation ⇒ the next session
auto-fires again); a resumed session (`ResumedSession` replay) marks `autoTitleFired`
so it never fires. The title never enters the transcript and no Token/Usage accounting
moves (Ratified design 1).

**Tests:** `internal/tui` model tests — fires exactly once per session on first submit
and not on the second prompt; does not fire when `AutoTitle` is false, when the seam is
nil, or on a resumed session; result applies via Rename when the ID exists; result
before the ID is stashed and applied on the first save-complete; a browser rename before
arrival wins (auto result dropped); error and sanitize-fail paths are silent (no note,
title unchanged); after `/new`, the next first prompt fires again; the transcript is
byte-identical with and without a landed auto-title.

**Acceptance:** `go test ./internal/tui/` passes; `make check` passes.

**Commit:** `feat(tui): auto-title new sessions on first prompt`

## 6. /rename command — ✅ DONE (2026-07-31)

Depends on item 5.

NOTES (2026-07-31): four additions to the item's literal text. (a) The carried-over never-clobber gap is
closed with provenance rather than a bare check: a `titleSource` rides with the stash (`Model.pendingSource`,
a fourth naming field written only by `applyTitle`), and `flushPendingTitle` drops an AUTOMATIC stash when
`titleTouched` is set while flushing a manual one regardless — a generated title waiting for an id is the one
way an automatic title outlives `foldAutoTitle`'s check, and `/rename <text>` pre-Save now stashes too.
(b) With no SessionHost wired, both forms note "this session is not being saved…" instead of reporting a
title nothing would store (the item names the seam-nil and no-prompt refusals but not this one).
(c) `autoTitleCmd` generalized into `titleCmd(firstUserText, wrap)`, since the manual path needs the identical
call under `manualTitleMsg`. (d) The spec row omits `whileRunning: false`: every other row in `commandSpecs`
omits its false flags, and an explicit zero value reads as a mistake there — the behaviour is unchanged and
pinned by `TestSafeWhileRunningReadsTheLine`. Also: `doc.go`'s file-by-file narration and the `wantParsed`
list in `TestCommandTableDrivesParserAndMenu` gained the new verb.

**What:** In `internal/tui/command.go`: add the spec row `{name: "rename", summary:
"rename this session (bare = ask the model)", takesArgs: true, whileRunning: false}` —
alphabetical slot between `new` and `server` (pinned by
`TestCommandSpecsReadAlphabetically`). In `Model.runCommand` (model.go:1035+): with args
— re-join with single spaces, `title.Sanitize`, on ok set `titleTouched` and apply via
`renameSession` when `ActiveID` exists, else stash in `pendingTitle` (applied at first
save-complete per item 5; this covers naming a session before its first prompt); note
the new title. Bare — seam nil ⇒ note "title generation not available"; no first user
text yet ⇒ note "nothing to name yet"; otherwise note that generation started and batch
the same `GenerateTitle` Cmd delivering a distinct `manualTitleMsg`: its result applies
even when `titleTouched` (explicit request, Ratified design 5) and sets `titleTouched`;
failure ⇒ quiet note suggesting `/rename <name>`. Add the command to the README table
(`README.md:138-153`).

**Tests:** `internal/tui` — spec-table alphabetical test passes with the new row;
`/rename my new name` parses to args and sets the joined sanitized title + touched flag;
`/rename` with nil seam notes unavailability; bare with no prompt notes "nothing to name
yet"; bare success applies over a prior manual rename; bare failure notes and leaves the
title; manual `/rename` before any Save stashes and applies at first save-complete; the
verb is rejected while the worker runs.

**Acceptance:** `go test ./internal/tui/` passes; `grep -n "rename" README.md` hits the
command table; `make check` passes.

**Commit:** `feat(tui): /rename sets or regenerates the session title`

## 7. Changelog

Depends on items 1–6.

**What:** Add a CHANGELOG.md entry under the current unreleased section describing
session auto-titling (out-of-band naming call, `auto-title` config key default-on) and
`/rename` (manual set / bare regeneration). Do not add or modify any release heading and
do not touch `VERSION`.

**Tests:** none (docs only).

**Acceptance:** `grep -in "auto-title" CHANGELOG.md` hits the unreleased section;
`make check` passes.

**Commit:** `docs(changelog): record session auto-titling and /rename`

## Suggested version bump

A user-visible feature (new config key, new command, new upstream call): a minor bump
(0.11.0) at the owner's next release cut seems warranted. No version identifier is
changed by this plan — whether and when to bump is the owner's decision.
