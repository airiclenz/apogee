# A session is never saved to history before its first prompt

- **Goal:** A completely new/empty session must not appear in the session history until a prompt has been sent. Today any non-ephemeral transcript entry — a `/confine` status note, a `/skills` catalogue, a `/model` actuation note, a "no saved sessions" browser note, an error note — flips the save gate, so quitting (or `/clear`) after merely poking at slash commands files a record titled "Session YYYY-MM-DD" with `UserMsgs: 0`. The save gate must become: **a committed user prompt exists**.
- **Date:** 2026-08-01
- **Status:** TODO
- **Authoritative sources (ground truth — if an item disagrees with these, follow these):**
  - The owner's directive (2026-08-01): "a completely empty / new session should not be saved in the session history until a prompt was sent off."
  - `docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md` — governs the record shape, the per-Turn save cadence, and the ephemeral-entry mechanism (as amended 2026-08-01).
  - `internal/tui/model.go` §"Per-Turn session save pipeline" — the three gates all saves funnel through.
- **Standing requirements:** forward the `coding-standards` skill to implementer and verifier sub-agents. Any authorized deviation from item text must land as a dated NOTES line under the item.
- **Working-tree note (for the Phase-0 dirty-tree consult):** the tree carries an uncommitted diff on `internal/tui/transcript.go` + `internal/tui/transcript_test.go` — a tightening of `hasConversation` to ignore `ephemeral` entries, plus `TestTranscriptHasConversationIgnoresEphemeral`. Item 1 **supersedes** that diff (it deletes `hasConversation` outright and replaces the test), so do not commit it separately: fold it into item 1's working set and let the implementer rewrite both hunks. Discarding it first (`git checkout -- internal/tui/transcript.go internal/tui/transcript_test.go`) is an equally valid resolution.
- **Diagnosis (context for all items):**
  - All session saves funnel through exactly three gates in `internal/tui/model.go`, each reading `m.sessions == nil || !m.transcript.hasConversation()`: `saveSession` (~line 1416; callers: clean quit at `quit`, and `startNewSession` on `/clear`|`/new`), `persist` (~line 1469; caller: the per-Turn `turnSnapshotMsg` fold), and `saveAtIdle` (~line 1484; callers: idle finishers). The store (`internal/session`) and host (`cmd/apogee/wire.go` `sessionHost`) are deliberately policy-free — the first `Save` mints the id and creates the file — so the fix belongs entirely at the TUI gate; no store/host change is needed.
  - `hasConversation` (`internal/tui/transcript.go` ~line 238) returns true for **any** entry beyond the start-up box (and, in the uncommitted tightening, beyond ephemeral entries). But plenty of non-ephemeral entries legitimately precede any prompt: `addNote` sites reachable pre-prompt include `/confine` (`confine.go:37`), `/skills` (`skills.go:51`), actuation/`/load`/`/model` notes (`actuation.go`), `/sessions` browser notes (`sessions.go:86,112,118`), `/rename` notes (`autotitle.go`), `/version`, and error notes. Quit or `/clear` then saves a zero-prompt record via `saveSession`.
  - The per-Turn paths (`persist`, `saveAtIdle`) only run after a Turn, which only a prompt opens — gating them on a prompt changes nothing in normal flows; switching them keeps the "worth saving?" rule in one predicate.
  - Interjections (`entryInterjected`) cannot precede a prompt: they are delivered into a running Exchange, and the Exchange was opened by an `entryUser` (on a restore, the stored scrollback carries that opening `entryUser`). So "a prompt exists" ⇔ "an `entryUser` entry exists".
  - Consequence to accept (and document): resuming a **legacy record with no transcript blob** and quitting without prompting no longer performs the final quit-flush (the scrollback holds no `entryUser`). Nothing is lost — the record is already on disk; only a cosmetic `ctxUsed`/`UpdatedAt` refresh is skipped.
- **Out of scope:**
  - Any change to `internal/session` (store) or `cmd/apogee/wire.go` (`sessionHost`) — the id-minting/first-save policy stays where it is.
  - The transcript wire format and codec (`encodeTranscript` already skips ephemeral entries; nothing new is encoded or skipped).
  - Autotitle/`/rename` behavior (the pending-title flush already waits for the first successful save; it simply now waits until after the first prompt).
  - Retention/pruning of already-saved empty records from earlier builds (owner can delete them in the `/sessions` browser).
  - Version bump / release (see closing note).

## 1. Save gate becomes "a prompt was sent": `hasPrompt` replaces `hasConversation`

**What:** Replace the transcript's save-gate predicate so no save can happen before the first committed user prompt.

- `internal/tui/transcript.go`: delete `hasConversation` and add `hasPrompt() bool` — true iff the transcript holds at least one `entryUser`. Doc comment must state: (a) this is **the** save-gate predicate (`saveSession`, `persist`, `saveAtIdle`) — a session earns a history record only once a prompt was sent, so slash-command notes, error notes, and re-derived chrome never file one; (b) `entryInterjected` is deliberately excluded because an interjection rides an Exchange that an `entryUser` opened (and a restored scrollback carries that opening entry), mirroring the exclusion rationale on `userTexts`; (c) the accepted consequence for legacy no-blob resumes (quit-flush skipped; the record is already on disk).
- `internal/tui/model.go`: switch all three gates (`saveSession` ~1416, `persist` ~1469, `saveAtIdle` ~1484) from `!m.transcript.hasConversation()` to `!m.transcript.hasPrompt()`. Update the `saveSession` doc comment ("when the transcript holds no conversation … nothing worth resuming" → the prompt-gate wording) and any other comment in the package naming `hasConversation`.
- This item **supersedes the uncommitted working-tree diff** (see header note): the ephemeral-awareness it added to `hasConversation` dies with the method — ephemerality stays load-bearing only at the codec (`encodeTranscript`, already committed) — and its `TestTranscriptHasConversationIgnoresEphemeral` is replaced by the tests below.

**Tests:**
- `internal/tui/transcript_test.go`: `TestTranscriptHasPrompt` (table-driven): empty transcript → false; start-up box only → false; persisted note (`addNote("cancelled")`) → false; ephemeral notes (`addEphemeralNote`) → false; error entry → false; `addInterjected` without a prior user entry → false (documents the exclusion); `addUser` → true; user message after a pile of notes → true. Remove `TestTranscriptHasConversationIgnoresEphemeral` if present in the working tree.
- `internal/tui` model-level regression using the existing `fakeSessionHost` (`seam_test.go:346`): a Model whose transcript holds the start-up box plus a persisted pre-prompt note (e.g. the `/confine` status note or a plain `addNote`) makes **zero** `Save` calls through `saveSession()` (the quit path) and gets `nil` from `saveAtIdle()`; after `addUser("…")` the same boundaries do save. Assert via the fake's recorded calls.
- Existing suite: `go test ./internal/tui/` passes with no other test rewritten — resume/browser tests replay scrollbacks that contain user entries, so the stricter gate must not disturb them.

**Acceptance:**
- `go test ./internal/tui/` passes.
- `grep -rn "hasConversation" internal/ cmd/` returns nothing.
- Manual sanity (verifier may reason from tests if a TTY is unavailable): fresh session → `/confine` → quit files nothing new under the sessions dir; fresh session → one prompt → record appears.
- `make check` passes.

**Commit:** `fix(tui): never save a session before its first prompt`

## 2. Documentation: ADR 0022 amendment and changelog

Depends on item 1.

**What:** Record the sharpened save gate where the persistence design lives.

- `docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md`: fix the now-stale `hasConversation` mention (~line 238, "…rendering and `hasConversation` need no per-kind duplication") to name the current seam, and add a short dated amendment note (matching the ADR's existing amendment style, if any; otherwise a one-paragraph "Amended 2026-08-01" block): the save gate is now `hasPrompt` — a record is created only once the first user prompt is committed, so slash-command/error notes alone never file a session; legacy no-blob resumes skip the quit-flush by design.
- `CHANGELOG.md` under `## [Unreleased]` → `### Fixed`: user-facing bullet, e.g. "An untouched session no longer lands in history — launching apogee, running a few slash commands (`/confine`, `/skills`, `/model` …) and quitting used to file an empty 'Session YYYY-MM-DD' record with 0 messages; a session is now saved only once you have actually sent a prompt." Do **not** add a release heading and do not touch `VERSION`.

**Tests:** none — documentation-only item.

**Acceptance:**
- `grep -n "hasPrompt" docs/adr/0022-*.md` shows the amendment; `grep -n "hasConversation" docs/adr/0022-*.md` returns nothing.
- `grep -n "sent a prompt" CHANGELOG.md` hits inside the `[Unreleased]` block; `git diff` shows no change to `VERSION` or any release heading.
- `make check` passes.

**Commit:** `docs(sessions): record the first-prompt save gate`

---

**Suggested version bump:** none now — the fix rides the existing `[Unreleased]` changelog block; cutting a release (and any `VERSION` change) remains the owner's call.
