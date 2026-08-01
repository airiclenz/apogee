# Resume notes must not persist; resume must never double the system prompt

- **Goal:** Fix ISSUES.md line 16 — (a) the "resumed: <title>" notice (and its siblings emitted at resume time) must be display-only, never saved into the session record, where today they accumulate on every resume; (b) harden the already-designed guarantee that a resumed session never receives the system prompt twice.
- **Date:** 2026-08-01
- **Status:** TODO
- **Authoritative sources (ground truth — if an item disagrees with these, follow these):**
  - `ISSUES.md:16` — the issue text.
  - `docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md` — governs what the transcript blob contains and the per-Turn save cadence.
  - `docs/adr/0023-*.md` (system prompt is a configured template, rendered per request) — governs the system-prompt posture: request-scoped, never committed to the conversation, never in the session record.
  - `CONTEXT.md` §"Session / Session record" (~line 61) and §"System prompt" (~line 495).
- **Standing requirements:** forward the `coding-standards` skill to implementer and verifier sub-agents. Any authorized deviation from item text must land as a dated NOTES line under the item.
- **Diagnosis (context for all items):**
  - Resume emits notes via `addNote` at two entry points: in-TUI browser resume, `resumeLoaded` (`internal/tui/sessions.go`, "resumed: …" plus optional interrupted note), and CLI `--resume`/`--continue` replay, `replayResumed` (`internal/tui/model.go`). `entryNote` is mapped in `entryKindNames` (`internal/tui/transcriptcodec.go`), so `encodeTranscript` persists these notes into `session.Record.Transcript` on the next save — resume five times and the record holds five "resumed:" notes. The "context: …" notices from `noteContextFiles` (`internal/tui/model.go`) have the same accumulation bug: re-derived and re-emitted on every startup/resume, yet persisted each time.
  - The existing precedent for display-only entries is `entryStartup`: deliberately absent from `entryKindNames`, skipped by `encodeTranscript`, ignored by `hasConversation`. Unknown kinds are skipped on decode, so persistence changes are backward/forward safe.
  - The system prompt is already never re-sent into history: `buildRequest` (`internal/agent/loop.go`) prepends exactly one `RoleSystem` message onto the request projection per request and never writes it back; `agentState` does not serialize the prompt (verified by `TestPromptSeam_ConfiguredPromptNeverEntersHistoryOrSnapshot`). The one hole: a legacy or hand-edited snapshot whose stored conversation already begins with a `RoleSystem` message — `restoreState` (`internal/agent/state.go`) restores it wholesale and `buildRequest` then prepends a second system message, so the wire carries two.
- **Out of scope:**
  - The other ISSUES.md entries (scrollback while streaming, work-dir display, expandable tool cards, inline skill tags, `/skill` idle-only tag).
  - Changing `Conversation.PrefixEnd()` (`internal/domain/hooks.go`) — it may keep tolerating leading system messages in requests; hooks legitimately create them there.
  - Any change to the transcript wire format itself (no new persisted kinds, no envelope version bump — ephemeral entries are simply not encoded).
  - Version bump / release (see closing note).

## 1. Ephemeral transcript entries; resume-time notes use them — ✅ DONE (2026-08-01)

NOTES (2026-08-01): added a user-facing `### Fixed` entry under `## [Unreleased]` in `CHANGELOG.md`
beyond the files the item names — the fix is user-visible and the repo keeps that section current.
No release heading, version field or `VERSION` value was touched. Item 2 can extend the same bullet
when the context-files notice follows.

**What:** Introduce display-only ("ephemeral") transcript entries and route every resume-time note through them.

- `internal/tui/transcript.go`: add an `ephemeral bool` field to `entry` and an `addEphemeralNote(text string)` appender beside `addNote` (same rendering, kind stays `entryNote`; only persistence differs). This generalizes the existing `entryStartup` skip rather than minting a new kind, so rendering and `hasConversation` semantics need no per-kind duplication.
- `internal/tui/transcriptcodec.go`: `encodeTranscript` skips entries with `ephemeral == true` (alongside the existing `entryStartup` skip). Decode needs no change — ephemeral entries never reach the wire.
- Switch the emitters: in `resumeLoaded` (`internal/tui/sessions.go`) and `replayResumed` (`internal/tui/model.go`), the "resumed: <title>" note, the "(no scrollback recorded — the model still remembers)" degrade variant, and the interrupted-mid-exchange note all become `addEphemeralNote`. These are all re-derived at resume time from live state, so persisting them is pure duplication.

**Tests:**
- New codec test mirroring `TestTranscriptCodecExcludesStartupAndPending` (`internal/tui/transcriptcodec_test.go`): an ephemeral note is present in the in-memory transcript but absent from `encodeTranscript` output; a plain `addNote` still round-trips.
- Extend `TestSessionBrowserResumeHappyPath` (`internal/tui/sessions_test.go`) and `TestNewModelReplaysResumedScrollback` (`internal/tui/model_test.go`): after resume, the "resumed:" note is visible via `hasEntry` (display unchanged) **and** `encodeTranscript`/`snapshotPayload` output does not contain the string `"resumed:"`.
- New regression test: resume → save → load → resume again does not accumulate — the reloaded transcript contains zero "resumed:" entries before the second resume replays it.
- `TestTranscriptCodecGoldenV1` must pass **unchanged** — this item removes entries from encoding, never alters the wire shape of what still encodes.

**Acceptance:**
- `go test ./internal/tui/ -run 'TranscriptCodec|Resume|Session'` passes.
- `grep -n 'addNote("resumed' internal/tui/` returns nothing (all resume notes go through the ephemeral appender).
- `make check` passes.

**Commit:** `fix(tui): make resume-time notes display-only, not persisted`

## 2. Context-file notices become ephemeral — ✅ DONE (2026-08-01)

NOTES (2026-08-01): extended item 1's `### Fixed` bullet in `CHANGELOG.md` (invited by item 1's own
NOTES) to cover the context-files notice, and extended `addEphemeralNote`'s doc comment in
`internal/tui/transcript.go` to list it among the re-derived notices — both beyond the files this
item names. All three notes `noteContextFiles` can emit (loaded, unreadable, Budget warn) were
switched, since all three are re-derived from the same report. No release heading, version field or
`VERSION` value was touched.

Depends on item 1.

**What:** `noteContextFiles` (`internal/tui/model.go`) emits the "context: …" notice on every startup and every resume (`newModel` and `resumeLoaded` both call it), and each emission is persisted — the same accumulation bug as item 1, one call site over. Switch its note(s) to `addEphemeralNote`. The notice is always re-derived from the live workspace state (ADR 0026: context files are session-scoped prompt data, re-read on restore), so nothing is lost from the record.

**Tests:** extend an existing resume test (or add one beside `TestNewModelReplaysResumedScrollback`) asserting the encoded transcript after startup + save contains no "context:" note, while the display transcript does.

**Acceptance:**
- `go test ./internal/tui/` passes.
- Resume an existing session twice in a row (unit-level: load → `resumeLoaded` → `snapshotPayload` ×2); the encoded transcript gains zero note entries across the cycle.
- `make check` passes.

**Commit:** `fix(tui): stop persisting the re-derived context-files notice`

## 3. Restored conversations are normalized to zero leading system messages — ✅ DONE (2026-08-01)

NOTES (2026-08-01): dropping the leading system run shifts every later message down, so
`restoreState` also shifts the restored `exchangeStart` by the dropped count (clamped at 0), with a
second test pinning it — beyond the item's literal "drop … before installing it", but required for
`AbortExchange` to still roll back to that Exchange's own boundary on a normalized snapshot. Also
added a `### Fixed` bullet under `## [Unreleased]` in `CHANGELOG.md`, a file this item does not name
(items 1–2 set that precedent for this plan). No release heading, version field or `VERSION` value
was touched.

Independent of items 1–2.

**What:** Close the only real hole in the issue's second half. Per ADR 0023 the configured system prompt is request-scoped and no committed message may be `RoleSystem`; enforce that invariant at the restore seam. In `restoreState` (`internal/agent/state.go`), after decoding the snapshot, drop any leading `RoleSystem` messages from the restored conversation before installing it. A well-formed apogee snapshot never contains them (this is a no-op on the happy path); a legacy or hand-edited snapshot that does would otherwise yield two system messages on the wire, because `buildRequest` unconditionally prepends the freshly rendered one and the wire seam folds the tool block into the first system message only.

**Tests:**
- New test beside `internal/agent/restoresession_test.go` (or in `promptseam_test.go`, matching its style): hand-craft a snapshot whose conversation begins with a `RoleSystem` message, restore it, run a step, and assert the outgoing wire request contains **exactly one** system message — the freshly rendered configured prompt, not the stored one.
- Companion assertion: the restored in-memory conversation contains no `RoleSystem` messages (invariant holds post-restore, so re-snapshotting is clean).
- Existing `TestPromptSeam_ConfiguredPromptNeverEntersHistoryOrSnapshot` and all `TestRestoreSession_*` tests still pass.

**Acceptance:**
- `go test ./internal/agent/ -run 'RestoreSession|PromptSeam|Resume'` passes.
- `make check` passes.

**Commit:** `fix(agent): normalize restored conversations to zero leading system messages`

## 4. Documentation and issue closure — ✅ DONE (2026-08-01)

Depends on items 1–3. This item owns every cross-cutting doc amendment; no other item touches these files.

**What:**
- `CONTEXT.md` §"Session / Session record": the sentence enumerating the transcript blob's contents ("user/assistant text, tool cards, notes, sub-agent Depth") gains the ephemeral distinction — resume/context notices are display-only and never persisted.
- `docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md`: dated addendum recording the ephemeral-entry decision (what qualifies: any entry re-derived at startup/resume time; mechanism: skipped by `encodeTranscript`, mirroring the `entryStartup` precedent) and noting §Decision 6's degrade wording is now emitted ephemerally.
- `docs/adr/0023-*.md` (system prompt ADR): dated addendum recording the restore-seam normalization from item 3 (leading `RoleSystem` messages in a snapshot are dropped on restore; the invariant "no system message in committed history" is now enforced, not just maintained).
- `ISSUES.md:16`: mark the item done (`- [x]`) with a one-line resolution note, matching however the file marks completed issues (if completed issues are simply deleted there, delete the line instead).

**Tests:** none (docs only).

**Acceptance:**
- `grep -n 'ephemeral' CONTEXT.md docs/adr/0022*.md` shows the amendments.
- `grep -n 'resumed:' ISSUES.md` shows the issue line closed or gone.
- `make check` passes.

**Commit:** `docs(sessions): record ephemeral transcript entries and restore-seam prompt normalization`

---

**Suggested version bump (no bump performed):** patch, v0.10.7 → v0.10.8 — user-visible bug fix (session records no longer accumulate resume/context notes) plus a defensive engine fix; no new features, no format change. The owner decides whether and when; the bump commit would also carry the CHANGELOG heading.
