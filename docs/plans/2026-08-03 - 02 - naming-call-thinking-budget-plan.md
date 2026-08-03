# Fix: the naming call spends its whole reply budget thinking

- **Goal:** a bare `/rename` on a big session must produce a title instead of "could not
  name this session". The naming completion caps the reply at `titleMaxTokens = 1024`,
  and on a thinking model (qwen3.6-35B-A3B live) `max_tokens` bounds reasoning tokens
  too — the multi-request window prompt makes the model burn the entire budget in
  `reasoning_content` and return `finish_reason: "length"` with an EMPTY `content`. The
  wiring returns that empty string with a nil error, `title.Sanitize("")` reports
  failure, and `foldManualTitle` prints the note. Fix: turn thinking off for the naming
  call, quadruple the token cap as the backstop, and report the truncation case
  distinctly instead of the generic refusal.
- **Date:** 2026-08-03 · **Status:** not started
- **Fix shape ratified by the owner** (this session, 2026-08-03): both levers — thinking
  off where the server supports it, cap raise as the universal backstop — plus a
  distinct truncation note.

## Authoritative sources

- Live repro, 2026-08-03, against `http://192.168.64.1:1111` (qwen3.6-35B-A3B-Q4_K_M),
  exact production sampling (temp 0.2, max_tokens 1024, the `systemInstruction` verbatim):
  - single-request form (165-char user message): `finish_reason: stop`, title produced,
    **616 completion tokens** — 2,298 chars of reasoning before an 5-word answer.
  - multi-request window form (6,446-char user message, the shape a big session's bare
    `/rename` sends): `finish_reason: length`, **`content: ""`**, 4,045 chars of
    reasoning, all 1,024 tokens consumed.
  So the failure is deterministic on big sessions, and even the smallest possible
  prompt leaves only ~1.7× headroom — the silent automatic call at first-prompt submit
  is very likely also failing quietly on meaty opening prompts.
- The naming-call design: ADR 0022 addendum
  (`docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md`) and the
  package comments in `internal/title/title.go` and `internal/tui/autotitle.go`.
- Failure path: `cmd/apogee/title.go` `generate` returns `resp.Content` only (discards
  `Thinking`, never reads `FinishReason`) → `internal/tui/autotitle.go`
  `foldManualTitle` → the note at `autotitle.go:148`.
- llama.cpp accepts a per-request `chat_template_kwargs` object; for Qwen-family chat
  templates `{"enable_thinking": false}` suppresses the thinking block. The kwarg is
  template-dependent and a strict OpenAI-compatible server may reject the unknown
  field — which is why the cap raise stays and item 3 adds a fallback.

## Standing requirements

- Forward skills at invocation: `coding-standards`.
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- Version policy: no item changes VERSION / CHANGELOG release headings / tags; see the
  closing note.

## Precondition — a clean tree

At the time of writing, the working tree carries uncommitted TUI work (`model.go`,
`render.go`, `theme.go`, tests, `layout.md`, `CHANGELOG.md`, `VERSION`, plus an
untracked `zz_visual_check_test.go`). Execute mode stops on a dirty tree: commit or
stash that in-flight work first. No item of this plan may sweep unrelated dirty files
into its commit.

## Out of scope

- The automatic call's silent-failure posture (ADR 0022 addendum, "Ratified design 9")
  — it stays silent; this plan only makes its success likely.
- Server-side mitigations (`--reasoning-budget`, profile changes in llama-launcher).
- Streaming the naming call, retry-policy changes beyond item 3's single fallback
  re-send, window-size (`windowBudgetRunes`) tuning.
- Any version bump (see closing note).

## 1. Provider seam: per-request thinking switch and a typed status error — ✅ DONE (2026-08-03)

**What:** two small, independent additions to `internal/provider`, both needed by the
wiring items downstream.

- `Request` (`internal/provider/wire.go`): add `DisableThinking bool` — a semantic
  seam field ("this call wants no reasoning"), not a wire-shaped one.
- `buildBody` (`internal/provider/client.go`): when set, emit
  `"chat_template_kwargs": {"enable_thinking": false}` via a new
  `ChatTemplateKwargs map[string]any \`json:"chat_template_kwargs,omitempty"\`` field
  on `chatRequest` (`internal/provider/wirejson.go`). Unset → the key is absent and
  every existing caller's request stays **byte-identical** on the wire (the anchor the
  `chatRequest` comments already enforce for `logprobs`).
- `statusError` (`internal/provider/client.go:240`): the non-overflow branch currently
  returns an unstructured `fmt.Errorf`. Introduce an exported
  `StatusError struct { Code int; Body string }` (with an `Error()` matching today's
  text) and return `*StatusError` from that branch so callers can detect an HTTP class
  with `errors.As` instead of string-matching. The `ErrContextOverflow` branch is
  untouched — `errors.Is(err, ErrContextOverflow)` must keep working for every
  existing caller.

**Tests:** extend the existing provider wire/body tests: (a) `DisableThinking` set →
marshalled body contains the exact kwarg object; (b) unset → the key is absent
(byte-identical anchor); (c) a non-2xx, non-overflow response yields a `*StatusError`
carrying the status code, and a 400-overflow still satisfies
`errors.Is(err, ErrContextOverflow)`.

**Acceptance:** `go test ./internal/provider/`; `go vet ./internal/provider/`.

**Commit:** `feat(provider): per-request thinking disable and typed upstream status errors`

## 2. Title package: thinking off, budget quadrupled, truncation named — ✅ DONE (2026-08-03)

**What:** in `internal/title/title.go` (depends on item 1):

- `Prompt` sets `DisableThinking: true` on the request it builds — an eight-word title
  needs no chain-of-thought, and suppressing it also makes the call fast (~600 reasoning
  tokens even on the smallest prompt today).
- `titleMaxTokens` 1024 → **4096**. Rewrite the constant's comment: the current
  "deliberately generous" claim is false for qwen3.6-class thinkers (the live repro
  numbers above); the cap is now the backstop for servers whose template ignores
  `enable_thinking`, sized so the big-window repro (~1.5–2k tokens of reasoning plus
  the answer) fits with room.
- Export `var ErrTruncated` — the error meaning "the completion hit the token cap and
  carried no answer". It lives here because both sides of the seam need the one
  definition: the wiring (`cmd/apogee`) returns it, the fold (`internal/tui`) tests for
  it, and both already import this package.

**Tests:** `internal/title/title_test.go` — extend the `Prompt` tests to assert
`DisableThinking` is set and `MaxTokens` is 4096 for both the single-request and
window forms.

**Acceptance:** `go test ./internal/title/`.

**Commit:** `fix(title): turn thinking off and quadruple the naming call's reply budget`

## 3. Wiring: detect the truncated-empty reply, survive a rejected kwarg — ✅ DONE (2026-08-03)

**What:** in `cmd/apogee/title.go` `generate` (depends on items 1–2):

- After a successful `Respond`: if `resp.FinishReason == "length"` and
  `strings.TrimSpace(resp.Content) == ""`, return `("", title.ErrTruncated)` — today
  this case returns `("", nil)` and is indistinguishable from a garbage reply.
- Fallback for strict servers: if `Respond` fails with a `*provider.StatusError` in the
  4xx class (and NOT `errors.Is(err, ErrContextOverflow)` — resending an oversized
  prompt cannot help), and the request had `DisableThinking` set, re-send ONCE with the
  flag cleared. Rationale to keep in the comment: a strict OpenAI-compatible server may
  reject the unknown `chat_template_kwargs` field outright, and without the fallback
  this plan would turn "naming fails on big sessions" into "naming fails always" on
  such a server. The existing "retries OFF" note stays true in spirit — a 4xx returns
  instantly without generation, so the one fallback re-POST occupies no meaningful
  queue time; state that in the NOTES-style comment beside it.

**Tests:** `cmd/apogee/title_test.go` — using the existing test-server seam: (a) a
reply with `finish_reason: "length"` and empty content yields `title.ErrTruncated`;
(b) a first attempt answered 400 (non-overflow) is re-sent once without
`chat_template_kwargs` and the second reply's title is returned; (c) a 400 on the
retry (or an overflow) escapes as the error with no third attempt.

**Acceptance:** `go test ./cmd/apogee/`.

**Commit:** `fix(cmd): detect truncated naming replies and fall back when the thinking kwarg is rejected`

## 4. TUI: say what actually happened on a truncated bare /rename — ✅ DONE (2026-08-03)

**What:** in `internal/tui/autotitle.go` (depends on item 2; meaningful after item 3):

- `foldManualTitle`: when `errors.Is(msg.err, title.ErrTruncated)`, the note reads
  `"the model spent its whole reply thinking and never wrote the title — " +
  renameUsage` (wording adjustable; it must name the cause and keep `renameUsage` as
  the closer). Every other failure keeps today's generic note.
- The automatic fold (`foldAutoTitle`) stays silent for this error like any other —
  do not touch it.
- Update the file-head comment block where it describes the failure postures, if the
  new distinction makes its wording stale.

**Tests:** `internal/tui/autotitle_test.go` — a `manualTitleMsg` carrying
`title.ErrTruncated` produces the truncation note; a generic error still produces the
generic note; an `autoTitleMsg` with `ErrTruncated` stays silent.

**Acceptance:** `go test ./internal/tui/`.

**Commit:** `fix(tui): tell the user when the naming reply ran out of budget`

## 5. Changelog and doc sweep — ✅ DONE (2026-08-03)

NOTES (2026-08-03): no ADR change needed — the ADR 0022 addendum (2026-07-31) describes the naming
call's category, firing point, seam, writer and gate, and its only "budget" is the transcript
window measured in runes; it says nothing about the call's sampling and nowhere implies the reply
may think freely, so it was left untouched.

**What** (depends on items 1–4; sole owner of cross-cutting doc changes):

- `CHANGELOG.md`: one entry under the unreleased section describing the fix (naming
  call: thinking disabled, budget 1024 → 4096, truncated replies reported distinctly,
  4xx kwarg fallback). Do NOT add or alter any release heading or version.
- ADR 0022 addendum: amend only if its prose describes the naming call's sampling or
  implies the reply may think freely; otherwise leave it untouched and record "no ADR
  change needed" in this item's NOTES line.

**Acceptance:** `make check` (whole-repo backstop for the plan's code items);
`grep -n "4096" internal/title/title.go` confirms the cap the entry describes.

**Commit:** `docs: record the naming-call thinking-budget fix`

## 6. Close out the predecessor plan (housekeeping, independent)

**What:** `docs/plans/2026-08-03 - 01 - session-name-on-the-top-rule-plan.md` is fully
done (all three items marked ✅ DONE 2026-08-03) but was never closed out. Re-check
every item in that file is marked done — if any is not, report BLOCKED instead of
archiving. Then run `make check` as the backstop, `git mv` the file into
`docs/plans/archived/`, and commit the move alone — no unrelated dirty files in the
commit.

**Tests:** none (process item).

**Acceptance:** `make check` passes; `git log -1 --stat` shows only the plan-file
rename; the file exists under `docs/plans/archived/` and no longer at its old path.

**Commit:** `chore(plans): archive completed session-name-on-the-top-rule plan`

## Suggested version bump

This is a user-visible bug fix (bare `/rename` reliably fails on big sessions with a
thinking model). Suggest a **patch** bump when it lands; note that plan 01's work may
already be carrying an uncommitted bump in the dirty tree — reconcile the two rather
than bumping twice. The bump itself is the owner's call; no item performs it.
