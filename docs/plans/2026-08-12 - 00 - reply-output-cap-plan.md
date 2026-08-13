# Reply output cap — plan

**Goal.** Stop an unbounded reply, and stop an unbounded reply from freezing the TUI. Today apogee
sends every agent and sub-agent turn with no `max_tokens`, so a thinking model can generate until
the server's context wall and the turn then fails wearing the wrong error. Give the engine a
per-turn output-token cap derived from the reply budget it already computes, a per-server pin to
override it, and an honest failure when a reply is cut off at that cap. The same incident exposed a
TUI defect the cap would only mask: the streaming preview re-renders the whole in-flight reply on
every repaint — O(N²) over a turn — starving the Bubble Tea event loop until mouse clicks queue for
seconds; item 6 bounds that render by the viewport.

**Date.** 2026-08-12
**Status.** Ready to execute.
**Sized for.** ~200k-context host.
**Skills.** `coding-standards`

## The incident this plan closes

A `/security-audit` run on qwen3.6-35B-A3B (session `20260812T152410Z-0d77f6b4`, server `apollo-2`,
per-slot `n_ctx` 98,304) put a Phase 2 sub-agent on one request that generated **20,653 reasoning
tokens over ~50 minutes and was still going**, at ~6.2 tok/s, with the server reporting
`max_tokens: -1, n_predict: -1`. Nothing in apogee would have stopped it before the 98,304-token
context wall (~81 further minutes), and at that wall the reply — reasoning-only, no visible text —
would have failed through `reviewedOutcome` as `upstream returned an empty reply (finish: length)`,
which names neither the cap nor the 20k tokens burned.

The engine had already decided how big that reply was allowed to be: `budget()` allocates a
`ResponseReserve` (20% of the window by default) and sizes the prompt around it. It just never told
the server. This plan makes the engine state the number it is already budgeting for.

The same session surfaced the plan's second defect: while that reply streamed, expand/collapse
clicks stopped responding, and a restart-plus-resume cured them. Diagnosis (2026-08-12, measured on
an instrumented PTY harness at HEAD): the in-flight buffer `transcript.pending` is the one
transcript block the paint cache cannot serve — the cache is keyed by entry index
(`internal/tui/paintcache.go:204`) and the live buffer is not an entry — so `paintPreview`
(`internal/tui/render.go:240`) re-renders the entire buffer through `renderMarkdownBody` on every
repaint, and repaints fire per 30 ms sink flush (`internal/tui/model.go:646`,
`internal/tui/sink.go:62`) plus at 2 Hz for the star blink while a tool call is open
(`internal/tui/model.go:884`) — the whole duration of a delegation. O(len(pending)) per repaint is
O(N²) per turn: measured 95% CPU and a 0.48 s click round-trip after only 180 s of streaming, still
climbing, against a flat 0.05–0.07 s once the same reply is committed (cache-served) — which is why
the restart cured it. Clicks are queued behind the renders, never dropped (Bubble Tea's single
update goroutine drains an unbuffered channel that terminal input also feeds), and a toggle fires
on mouse release, so an even number of impatient clicks drains to a visible no-op.

## Authoritative sources

Per item, an implementer follows these over this plan's prose where they disagree:

- `internal/context/budget.go` — `Allocate` / `Allocation`, `defaultReserveFraction = 0.20`, and
  the contract that a zero `Allocation` (unknown window) must NOT be read as "unbounded", the same
  defect that "wedged an unbudgeted session (audit 2026-08-01)".
- `internal/config/config.go` — the `ServerEntry` doc block's three-state idiom for
  `context-window:` and `parallel-agents:` (absent or 0 ⇒ discover/derive, N ≥ 1 ⇒ pin, negative ⇒
  refused by `ValidateServers`). The new key copies that idiom rather than inventing one.
- `internal/agent/loop.go` — `budget()`, the two `domain.NewRequest` sites, and `reviewedOutcome`
  with its stated reasoning that failure honesty is engine correctness and must hold in Bypass.
- `internal/domain/hooks.go` — `SamplingParams` and its "a nil field leaves the loop's value
  untouched" contract.
- ADR 0018 (context overflow recovers structurally), ADR 0039 (`parallel-agents`), ADR 0045
  (`context-window:` per entry, sub-agent routing).
- `internal/tui/render.go` — the transcript render walk, `paintPreview`, and the five `paintBlock`
  cache call sites; `internal/tui/paintcache.go` — the entry-index cache key that excludes the live
  buffer by construction.
- ADR 0011 and `internal/tui/doc.go` — the value-copied Model rule (no `strings.Builder` or other
  no-copy type held by value anywhere it reaches), binding on item 6's edits.

## Ratified design calls

Decided by the owner (Airic Lenz) on 2026-08-12, in session; binding on every item below.

1. **Scope is all three strands** — the `ISSUES.md` record, the per-turn cap, and distinct handling
   for a reply cut off at the cap.
2. **The cap's source is a per-server config key**, `max-output-tokens:` on the server entry,
   following the `context-window:` idiom exactly, with a built-in default when it is absent. The
   ceiling is a property of the slot, so it belongs to the server entry.
3. **The default is derived from the window, not a fixed constant.** Refinement adopted at write
   time, within that call: the derived value is the Budget's own `ResponseReserve` rather than an
   ad-hoc fraction — that is the number the engine already reserves for the reply, so the request
   and the budget stop disagreeing. Clamped to [4096, 32768]; an unknown window takes the floor.
4. **At the cap the turn fails, honestly and at engine level** — its own message naming the cap and
   the token count, no partial-reply salvage and no retry Mechanism.
5. **The TUI fix is the tail-render alone.** `paintPreview` is bounded to the lines that can be on
   screen; the diagnosed stopgaps — skipping the 2 Hz blink repaint while streaming, an adaptive
   `tokenCoalesceWindow` — are NOT taken: once the render is bounded by the viewport, every repaint
   is cheap and both stopgaps add machinery for nothing.

## Out of scope

- Any Mechanism, gated or otherwise. The cap is engine behaviour and must hold in Bypass.
- Retrying, resuming or nudging a reply that hit the cap.
- Salvaging partial visible text from a cut-off reply.
- Changing `defaultReserveFraction`, the `Allocate` split, or the emergency fold (ADR 0018).
- The rejected TUI stopgaps (ratified call 5): the 2 Hz blink repaint (`internal/tui/model.go:884`)
  and `tokenCoalesceWindow` (`internal/tui/sink.go:62`) stay exactly as they are.
- Extending the paint cache to cover the live buffer (the tail bound makes the cache question moot
  for the preview).
- Any change to `VERSION`, the `CHANGELOG` release heading, or a release tag.
- The `/security-audit` skill itself (it lives outside this repo).

---

## 1. Record the two 2026-08-12 defects in ISSUES.md — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the placement anchor named by the item — the `## External security audit — confirmed findings (2026-08-11)` heading — no longer exists; it was removed from ISSUES.md by commit 2f0d4d7 (current HEAD). Both entries were placed at the end of the top undated list, immediately above the file's first section heading (`## Hostile-bytes hardening run — open follow-ups (2026-08-12)`), which is where that instruction points now.

NOTES (2026-08-13): two citations corrected against the working tree — `SamplingParams` is declared at `internal/domain/hooks.go:621` (the item's `:622` is its `Temperature` field), and the 30 ms sink flush is folded at `internal/tui/model.go:648` (the item's `:646` is the `ctrlCResetMsg` return). The blink citation `internal/tui/model.go:884` is accurate and was kept; `:904` (the conditional repaint) and `internal/tui/spinner.go:347` (`starBlinkHalfPeriod = 500ms`, the 2 Hz claim's source) were added so the "2 Hz" figure is checkable.

**What.** Add two entries to `ISSUES.md`, so both defects are tracked independently of this plan's
execution. Place each as a `- [ ]` bullet in the top (undated) list, above the
`## External security audit — confirmed findings (2026-08-11)` heading, in that list's existing
voice: one paragraph per entry, evidence cited as `path:line`, no speculation.

The first entry records the uncapped reply. It must state that no agent or sub-agent turn sets
`MaxTokens` (only `internal/title/title.go:45` and `internal/agent/compact.go:18` do, both 4096),
that `internal/agent/wire.go:120` forwards only the sampling knobs a hook may set while
`internal/domain/hooks.go:622` `SamplingParams` has no Mechanism setting them, and that the reply
therefore runs to the server's context wall — with the 2026-08-12 qwen3.6-35B observation (20,653
reasoning tokens in ~50 minutes at `n_predict: -1`) as the trigger. Close the entry with
`Owned by the plan docs/plans/2026-08-12 - 00 - reply-output-cap-plan.md (item 4).`

The second entry records the unbounded preview render, as the second member of the family the
existing `ReloadSkills` entry already names (synchronous O(transcript) work on the Bubble Tea
update goroutine, ADR 0011). It must state that `paintPreview` (`internal/tui/render.go:240`)
re-renders the whole of `transcript.pending` (`internal/tui/transcript.go:851`) on every repaint
because the paint cache is keyed by entry index (`internal/tui/paintcache.go:204`) and the live
buffer is not an entry; that repaints fire per 30 ms sink flush and at 2 Hz while a tool call is
open (`internal/tui/model.go:646`, `:884`; `internal/tui/sink.go:62`), making a streaming turn
O(N²); and the measured effect — 95% CPU and a 0.48 s click round-trip after 180 s of streaming,
flat 0.05–0.07 s once the reply is committed, clicks queued (never dropped) behind the renders.
Close it with `Owned by the plan docs/plans/2026-08-12 - 00 - reply-output-cap-plan.md (item 6).`

**Files:** `ISSUES.md`

**Tests.** None — documentation only.

**Acceptance.**
- `grep -n "n_predict" ISSUES.md` prints the first new entry.
- `grep -n "paintPreview" ISSUES.md` prints the second new entry.
- `git status --porcelain` lists `ISSUES.md` and nothing else.

**Commit.** `docs(issues): record the uncapped reply and the unbounded preview render`

---

## 2. ADR 0046 — the engine bounds every reply — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the item asks for the sibling's "status/date header"; ADR 0045 — and every other ADR in `docs/adr/` — carries only `Status: accepted` in its front matter, with no `Date:` field. The record follows the sibling verbatim and carries the 2026-08-12 ratification date in the Context prose instead, rather than inventing a header field no ADR uses.

NOTES (2026-08-13): the item's third acceptance line (`git status --porcelain` lists only the new ADR) reads clean for this item's own files, but the tree also shows item 1's `ISSUES.md` because both items were implemented in one batched dispatch. No existing ADR was renumbered or amended; the new record supersedes nothing.

**What.** Write `docs/adr/0046-the-engine-bounds-every-reply-with-an-output-cap.md`, recording the
four ratified design calls above as an accepted decision. Follow the structure and voice of
`docs/adr/0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md` — the sibling that
introduced the last per-server key — including its status/date header and its numbered-decision
shape. The record must carry: the context (the engine reserves reply room in `Allocate` but never
tells the server, so a thinking model runs to the context wall — with the 2026-08-12 incident as
the evidence); decision 1, the cap is engine-level and holds in Bypass, on `reviewedOutcome`'s
stated reasoning; decision 2, the per-server `max-output-tokens:` pin in the established
three-state idiom; decision 3, the default is the Budget's `ResponseReserve` clamped to
[4096, 32768] with an unknown window taking the floor, citing `Allocation`'s rule that unknown must
never read as unbounded; decision 4, a reply cut off at the cap fails with its own message rather
than through the empty-reply path. Note the consequence for the north star (ADR 0031): the cap is
engine behaviour, so every Driver inherits it, and no Driver needs to bound replies itself. Do not
renumber or amend any existing ADR — this one supersedes nothing.

**Files:** `docs/adr/0046-the-engine-bounds-every-reply-with-an-output-cap.md`

**Tests.** None — documentation only.

**Acceptance.**
- `ls docs/adr/ | tail -2` prints `0045-…` then `0046-…`, with no gap in the sequence.
- `grep -c "max-output-tokens" docs/adr/0046-the-engine-bounds-every-reply-with-an-output-cap.md`
  is at least 1.
- `git status --porcelain` lists only the new ADR file.

**Commit.** `docs(adr): 0046 — the engine bounds every reply with an output-token cap`

---

## 3. Add the per-server `max-output-tokens:` pin — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the template placement the item names — "next to the existing `context-window:` comment block (around line 383)" — is the TOP-LEVEL `context-window:` key's block; the per-entry `context-window:` pin ADR 0045 added is not documented in the template at all. The new block sits immediately after line 383 as instructed, but its example is written NESTED (`#   - name: workstation` / `#     max-output-tokens: 8192`) with prose saying the key lives on a `servers:` entry, because a `# max-output-tokens:` example at indent 0 would teach a top-level key that does not resolve, and repeating a `# servers:` header at indent 0 would break `CommentedExampleLine`/`TestEmbeddedDefaultConfigTeachesTheServersSchema`, which refuse a second commented example of one key.

NOTES (2026-08-13): no CHANGELOG entry for this item — the plan's own "Suggested version bump" section assigns items 3–5 ONE shared `[Unreleased]` feature entry, and nothing reads the key until item 4, so an entry written here would describe behaviour that does not exist yet. Item 4 (or 5) writes the entry covering all three.

NOTES (2026-08-13): the template block deliberately does NOT mention that a reply cut off AT the cap fails the turn — that behaviour is item 5's, whose Files list does not include this template. Worth adding there.

NOTES (2026-08-13): `gofmt` re-aligned the whole `ServerEntry` field block (the new field name is longer than any existing one). Field order and every existing tag are unchanged; the realignment is the formatter's, not a restyle.

**What.** Add the config surface only — nothing reads it yet; item 4 wires it in.

- Add `MaxOutputTokens int \`yaml:"max-output-tokens,omitempty"\`` to `ServerEntry` in
  `internal/config/config.go`, positioned beside `ContextWindow`.
- Extend the `ServerEntry` doc block with the key's paragraph, written in the same voice and the
  same three states the `ContextWindow` paragraph uses verbatim: absent (or 0, which yaml cannot
  tell from absent) ⇒ the engine derives the cap from the reply budget; N ≥ 1 ⇒ that ceiling
  whatever the window says; negative ⇒ refused by `ValidateServers`. Say why it is per entry — the
  ceiling is a property of the slot, like the window it is derived from — and why it earns its keep:
  a cloud endpoint that advertises no window derives from an unknown window and takes the clamp
  floor, so the pin is the only way to let such a server answer at length.
- Extend `ValidateServers` with the negative-value check, on exactly the reasoning its
  `parallel-agents:` and `context-window:` checks already state (a negative number is the one value
  with nothing to mean), and in the same message shape those two use.
- Document the key in `internal/config/defaults/config.yaml` next to the existing
  `context-window:` comment block (around line 383), in that file's commented-example style, naming
  the runaway it prevents in one clause.

**Files:** `internal/config/config.go`, `internal/config/config_test.go`,
`internal/config/defaults/config.yaml`

**Tests.** In `internal/config/config_test.go`, following that file's existing table style:
- `ValidateServers` refuses a negative `max-output-tokens`, with a message naming the entry.
- Absent and an explicit `0` both survive validation and are indistinguishable (the derive state).
- A value of N ≥ 1 round-trips through YAML unmarshal onto `ServerEntry.MaxOutputTokens`.

**Acceptance.**
- `go build ./internal/config/...`
- `go test ./internal/config/...`

**Commit.** `feat(config): add the per-server max-output-tokens pin`

---

## 4. Cap every turn's reply at the reply budget — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the item's Files list names four files, but the pin cannot reach the engine from those four alone. `ContextConfig` lives in `internal/domain/config.go`, not in the listed `internal/agent/agent.go` (which needed no change), and "carry the pin from the selected server entry" crosses the composition root: `internal/config/options.go` + `internal/config/config.go` flatten the startup entry's value the way `StartupParallelAgents` is flattened, `cmd/apogee/wire_server.go` reconstitutes it into the bound `ServerEntry` and applies it at the one bind seam, `cmd/apogee/wire_boot.go` seeds the base Config a scheduled Firing copies, `cmd/apogee/headless.go` gives the headless Driver the same bound, `cmd/apogee/delegation.go` fills the new `DelegationTarget` field, and `internal/agent/subagent.go` reads it onto the child. All eight are additive; no existing behaviour moved.

NOTES (2026-08-13): a routed child takes the target's `MaxOutputTokens` UNCONDITIONALLY, which is deliberately not the treatment `ContextWindow` gets beside it (0 there keeps the parent's). A zero cap is not a broken child — it derives from the window it inherited, which by then is the routed server's — whereas keeping the parent's pin would bound a reply from the sub-agent server by a number describing the orchestrator's. This is the item's "derives from that server's numbers rather than the parent's" read literally.

NOTES (2026-08-13): the second `domain.NewRequest` site the item names (`loop.go:959`) is `loopView`, which builds a read-only `LoopView` for the tool-stage hooks and reaches no server — `LoopView` does not even expose sampling. The stamp was added there as instructed and is documented in place as a consistency stamp rather than a wire bound; the wire effect is entirely `buildRequest`'s.

NOTES (2026-08-13): `README.md` is not in the item's Files list but the cap is user-visible behaviour (a reply now stops at a bound apogee chose), so a paragraph was added after the existing context-window one, in that section's voice, naming the derivation, the clamp and the `max-output-tokens:` pin. It also closes item 3's own follow-up note that the key was documented only in the config template.

NOTES (2026-08-13): a mid-session `/server` switch does NOT re-push the new entry's cap — `Agent.SwitchUpstream` carries no window or cap today, and the per-entry `context-window:` pin has the same gap (ADR 0045: "only the Delegation target reads it"). The cap follows the entry at bind time and at every routed spawn; a live switch-time push would need a new engine setter, which this item does not authorise.

NOTES (2026-08-13): no VERSION, CHANGELOG release heading or tag touched. The CHANGELOG text above is the shared items 3–5 feature entry the plan's "Suggested version bump" section calls for, written here because this is where the behaviour lands; item 5 amends it with the cut-off failure message.

Depends on item 3.

**What.** Make the engine send the ceiling it already budgets for, on the primary loop and on every
delegation.

- Carry the pin from the selected server entry into the agent's context configuration beside
  `MaxContextTokens` (the field `ContextConfig.MaxContextTokens` is fed the same way today —
  `internal/config/config.go:112` names that path), and into `DelegationTarget` in
  `internal/agent/delegationtarget.go` beside its `ContextWindow` pin, so a child agent on the
  flagged sub-agent server derives from that server's numbers rather than the parent's.
- Add `func (a *Agent) maxOutputTokens() int` beside `budget()` in `internal/agent/loop.go`:
  return the pin when it is > 0; otherwise take `ResponseReserve` from the same `Allocate` call
  `budget()` makes and clamp it to [4096, 32768]; when the `Allocation` is zero — the window is
  unknown — return the floor, 4096, because `Allocation`'s contract forbids reading unknown as
  unbounded and the pin from item 3 is the escape hatch for that case. Name the two clamp bounds
  as package constants with a doc comment giving the reasoning, in the style of
  `defaultReserveFraction`.
- At BOTH `domain.NewRequest` sites (`internal/agent/loop.go:644` and `:959`), set the request's
  sampling from `maxOutputTokens()` immediately after construction and BEFORE the pre-request hooks
  run, so a hook that sets `MaxTokens` still wins — which is what makes `SamplingParams`'s "a nil
  field leaves the loop's value untouched" true for this field for the first time.
- Leave `Temperature` untouched (still the server's default), and leave `internal/title/title.go`
  and `internal/agent/compact.go` on their own 4096 caps — both already bound themselves.

**Files:** `internal/agent/loop.go`, `internal/agent/agent.go`,
`internal/agent/delegationtarget.go`, `internal/agent/budget_test.go`

**Tests.** Extend `internal/agent/budget_test.go`:
- window 98,304, no pin, default reserve ⇒ cap 19,660 (the incident's server: the runaway would
  have ended at ~53 minutes instead of ~131).
- window 200,000, no pin ⇒ 40,000 derived, clamped to the ceiling 32,768.
- window 0 (unknown) ⇒ the floor, 4,096 — never unbounded.
- pin 500 with a 98,304 window ⇒ 500: the pin beats the derivation.
- a pre-request hook setting `MaxTokens` beats the loop's default.
- the provider request built from a turn carries a non-nil `MaxTokens` — the regression guard for
  the defect itself.

**Acceptance.**
- `go build ./...`
- `go test ./internal/agent/... ./internal/context/... ./internal/config/...`

**Commit.** `feat(agent): bound every reply with an output-token cap`

---

## 5. Name the cap when a reply is cut off — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the reasoning spend the new message reports is an ESTIMATE, through the Agent's calibrated chars→token estimator (`a.tokens.EstimateTokens`), and the message says "roughly" so it reads as one. `domain.Response` carries the reasoning TEXT, never the server's count of it, and the per-call usage report is not threaded to `reviewedOutcome` today — threading it would be the control-flow change the item forbids ("a message change and a branch").

NOTES (2026-08-13): the cap the message names is `maxOutputTokens()` — the loop's own value — not the number actually stamped on that one request, so a pre-request hook that overrode `MaxTokens` for it would leave the message naming the engine's ceiling rather than the hook's. Documented at the helper: the engine's number is the one an operator can act on with `max-output-tokens:`, and `domain.Response`'s view exposes no sampling to read the other from.

NOTES (2026-08-13): the existing table row "the finish reason rides along for diagnosis" scripted finish `length`, which the new branch now answers, so its expectation was false the moment the branch landed. The row keeps its purpose with an arbitrary open-set reason (`content_filter`) instead; the `length` shape moved to the new `TestCutOffReplyNamesTheOutputCap`, which covers it both with and without reasoning.

NOTES (2026-08-13): two files outside the item's Files list were touched, both documentation for now-user-visible behaviour (procedure step 6). `README.md` gained one sentence after item 4's cap paragraph, and the `max-output-tokens:` block in `internal/config/defaults/config.yaml` gained one clause — which closes item 3's own recorded follow-up that the template "deliberately does NOT mention that a reply cut off AT the cap fails the turn ... Worth adding there". Neither edit changes an example line, so `TestEmbeddedDefaultConfigTeachesTheServersSchema` stays green.

NOTES (2026-08-13): the CHANGELOG text above is a standalone bullet meant to sit under the same `### Added` block, immediately after item 4's cap entry, rather than an in-place amendment of that paragraph as the plan's "items 3–5 as one feature entry" would suggest: the sidecar's single-writer flow applies text, it cannot express an edit to an already-committed paragraph. The behaviour is part of the cap feature, not a fix to previously-shipped behaviour, which is why `### Added` and not `### Fixed`.

NOTES (2026-08-13): `internal/tui/render.go` and `internal/tui/render_test.go` are dirty in the working tree from item 6 being implemented concurrently. They are untouched by this item and are deliberately absent from FILES.

NOTES (2026-08-13): no VERSION, CHANGELOG release heading, package-manifest version or tag was touched.

Depends on item 4.

**What.** Give a reply that ended at the cap its own failure, instead of routing it through the
empty-reply message that calls a 20k-token reply "empty".

- In `internal/agent/loop.go`, split the `reviewedOutcome` failure path by finish reason: when the
  reply has no visible text and no tool calls AND `resp.FinishReason()` is `domain.FinishLength`,
  emit an error naming the cap and what was spent — the cap in tokens, and the reasoning-token
  count when the reply carried reasoning — rather than `emptyReplyErrFmt`. Every other empty reply
  keeps today's message and today's behaviour exactly.
- Add the new format string beside `emptyReplyErrFmt` with a doc comment in that constant's voice,
  explaining what the reader is being told: the model did answer, at length, and the engine's own
  ceiling stopped it — so the fix is a bigger `max-output-tokens:` or a shorter task, not a retry.
- The turn still fails, with the same `ErrorEvent` source and the same `turnFailed` outcome. This
  is a message change and a branch, not a control-flow change: no retry, no salvage, and the guard
  stays engine-level so it holds in Bypass.

**Files:** `internal/agent/loop.go`, `internal/agent/emptyreply_test.go`

**Tests.** Extend `internal/agent/emptyreply_test.go`:
- a reasoning-only reply with finish reason `length` produces the new message, and that message
  contains the cap value.
- a reasoning-only reply with finish reason `stop` still produces `emptyReplyErrFmt` unchanged.
- both still fail the turn, with source `loop` and no committed assistant message.

**Acceptance.**
- `go build ./...`
- `go test ./internal/agent/...`

**Commit.** `fix(agent): name the output cap when a reply is cut off at it`

---

## 6. Bound the streaming preview render by the viewport

**What.** Make `paintPreview`'s cost a function of the screen, not of the reply. In
`internal/tui/render.go`, inside `paintPreview` (line 240), slice the text handed to
`renderEntryLines` to the TAIL of `trimTrailingBlankLines(t.pending)` — the last K raw
(newline-delimited) lines — instead of the whole buffer. The preview contributes at most one
viewport of visible rows at the bottom of the frame, so everything above the tail is wrapped,
styled and then thrown away; after this change a repaint costs O(viewport) whatever the reply's
length, which removes the O(N²) term entirely and is why ratified call 5 takes no other lever.

Binding details:

- K must safely exceed any plausible terminal height. When the render seam already has the
  transcript's row count in reach, use it plus a margin; otherwise a named package constant
  (e.g. 256 raw lines) with a doc comment stating the bound's reasoning — in the style of
  `defaultReserveFraction` — is the right shape. Which of the two is a mechanical choice; the
  constraint that is NOT negotiable is that the bound is O(1) in the reply's length and at least
  double any realistic viewport. Every raw line renders to one or more screen rows, so K raw lines
  can never underfill a K-row window; markdown constructs that JOIN source lines (a wrapped
  paragraph) are covered by the doubling.
- Accepted trade, decided here: a markdown construct opened above the slice point (an unclosed
  code fence, a list) may render unstyled in the preview's tail. The preview is transient — the
  committed entry re-renders the full text through the cache and heals it — and mid-stream
  markdown is best-effort already. No compensation logic (fence scanning, state carry-over) is in
  scope.
- The slice is display-only: `t.pending` itself keeps every byte, exactly as the existing
  trailing-blank-lines trim already models (`render.go:236` comment). Slice by scanning bytes for
  the last K newlines — never split the whole buffer into a `[]string`, which is itself O(N) in
  allocations per repaint.
- ADR 0011 stands: no `strings.Builder` or other no-copy type held by value anywhere the Model
  reaches (`internal/tui/doc.go`); `TestModelNoBuilderByValue` must stay green.

**Files:** `internal/tui/render.go`, `internal/tui/render_test.go`

**Tests.** Extend `internal/tui/render_test.go`, in its existing style:

- a streaming transcript whose `pending` holds many more raw lines than K paints a preview
  containing the buffer's LAST lines and none of its first — the tail is what's on screen.
- the preview's painted output for a pending buffer of 10,000 raw lines and of K+1 raw lines has
  the same row count — the bound, stated as behaviour.
- a pending buffer smaller than K paints exactly what it paints today (byte-identical frame) — the
  common case is untouched.
- an empty buffer still renders its lone marker line (the existing contract at `render.go:238`).

**Acceptance.**
- `go build ./internal/tui/...`
- `go test ./internal/tui/...` (includes `TestModelNoBuilderByValue` and the paint-cache suite).

**Commit.** `fix(tui): bound the streaming preview render by the viewport`

---

## Suggested version bump

No item changes `VERSION` — whether and how to bump is the owner's call. This is a new operator-
visible config key plus an engine behaviour change that bounds every reply, so a **minor** bump
(0.12.x → 0.13.0) is the level that fits it once the plan lands, with a `CHANGELOG.md`
`[Unreleased]` entry covering items 3–5 as one feature and item 6 as its own fix line.
