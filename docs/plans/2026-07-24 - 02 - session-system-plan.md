# Plan — the session system: per-Turn persistence, history browser, in-TUI resume

**Date:** 2026-07-24. **Status: PLAN — not started.** Execute with `/implement-plan` in a fresh
session, forwarding skills: `coding-standards`.

Owner request: build the session handling system — the TODO.md P1 item ("in-TUI *new session*
… and a *history browser* overlay; today only `--resume <path>` exists") and the successor the
`/clear`-reset plan explicitly deferred to. Sessions must persist continuously (not quit-only),
be listable and resumable from inside the TUI, and replay their scrollback on resume. The
original implementation to mirror where it helps is `../apogee-code` (`src/sessions/
session-manager.ts`, `src/orchestrator/conversation-state.ts`); its VS-Code-specific plumbing
(webview messages, globalState) does not carry over.

## Where things stand (grounded, verified 2026-07-24)

**The engine half already exists and is untouched in shape.**
- `domain.Session` (`internal/domain/session.go:26`) is the versioned envelope `{Version,
  State json.RawMessage}`; the engine owns the opaque `State` = conversation + `turnIndex` +
  `inExchange` + `exchangeStart` + pending input (`internal/agent/state.go:43`,
  `restoreState` at state.go:79). `SessionVersion = 1`; a future version is rejected
  (`ErrSessionVersion`). The allow-for-session approval cache is deliberately NOT serialized
  (re-confirmed on resume) and MCP reconnects fresh (ADR 0008) — both stand.
- `Agent.Snapshot()` (`internal/agent/agent.go:258`) is valid at any quiescent boundary;
  between Steps the worker goroutine is the single driver, so IT may snapshot mid-Exchange.
  `apogee.Resume(cfg, snap)` (apogee.go:60) reconstructs an Agent at startup. There is **no
  live-restore method** — resuming without relaunching has no engine surface yet.
- A mid-Exchange snapshot round-trips `InExchange: true`; a resumed Agent **rejects Submit**
  (`ErrInputPending`, agent.go:121) — the bench re-Steps to continue, but the TUI has **no
  step-without-Submit drive**, so today a mid-Exchange resume would be stuck until `/clear`.
  `AbortExchange` (agent.go:171) rolls back to `exchangeStart`, dropping ALL the interrupted
  Exchange's committed turns.

**The store is a quit-only, write-only stub.**
- `internal/session.Store` (store.go:34) has exactly one method: `Save(domain.Session)` →
  a NEW `<UTC-timestamp>.json` (bare envelope, no metadata) under `~/.apogee/sessions/`
  (0700/0600). No IDs, no List/Load/Delete, no update-in-place. Real legacy files exist in
  `~/.apogee/sessions/` and must stay readable.
- The TUI saves ONLY on clean quit (`saveSession`, `internal/tui/model.go:738`), gated by
  `transcript.hasConversation()`, through the narrow `Options.Save func(domain.Session) error`
  seam (tui.go:146) → `sessionSaver` (cmd/apogee/wire.go:439). `--resume <path>` (root.go:187)
  reads the file directly in `buildAgent` (wire.go:557). After quit, wire.go:330 prints the
  resume hint.
- On `--resume` the engine remembers but **the view starts empty** — nothing replays the
  scrollback. The transcript (`internal/tui/transcript.go:23`) is `entries []entry` with kinds
  user/assistant/toolCall/toolResult/error/note/presented/startup; `entry` (transcript.go:53)
  carries `kind, text, depth, callID, tool toolView, done, skills, presented presentedView,
  startup startupView`; `toolView` (toolpresent.go:62) is `{Label, Verb, Target, Summary
  detailLine, Details []detailLine, name string}`, `detailLine` is `{Kind detailKind, Text}`.
  None of this serializes today.

**The seams the previous plan built for exactly this system.**
- `startNewSession()` (model.go, routed from `case "clear", "new"`) is the single reset seam;
  its doc comment says the session system will wrap THIS call with "save the outgoing
  conversation + allocate a new session id". `transcript.reset()` (transcript.go:152) and
  `newStartupView(opts)` exist. Inherited contract: snapshot-before-`ClearContext` ordering.
- The worker (`internal/tui/worker.go:65` `driveExchange`) Submits then loops Step; on
  `StatusTurnComplete` it continues — that boundary is the per-Turn snapshot point. All of a
  Turn's events were already `program.Send`-delivered by the teaSink *during* the Step (the
  sink is called synchronously inside the loop), so anything the worker sends *after* Step
  returns is ordered AFTER that Turn's events — the transcript the Model holds when such a
  message folds is consistent with the snapshot. The existing code already relies on this
  ordering (events before `exchangeDoneMsg`).
- `Run` (tui.go:173) builds `newModel(ctx, eng, opts)` and holds the `*Bridge`; Bridge and
  Model are the same package, so the Model can be handed the bridge's late-bound program
  sender without any exported API.

**apogee-code parity notes (the oracle, explored 2026-07-24).**
One global `~/.apogee-code/sessions/<id>.json` per session, saved after every completed turn
(never on quit — in-flight turns were lost), ID minted at first-turn completion, **dual
representation persisted** (UI entries + provider messages) so resume replays the scrollback,
title = first-user-message heuristic (50 chars, word-boundary truncate) with inline rename,
history list = title · relative time · message count with click-to-resume and delete, corrupt
files skipped silently, no workspace scoping, no pruning, no locking. The agent mode, server,
and approvals were deliberately NOT in the session file — apogee keeps that too.

### Decisions locked (owner-ratified 2026-07-24 — do not re-litigate)

- **Save cadence: every Turn.** The worker snapshots between Steps and the Model persists
  asynchronously; a crash loses at most one Turn. (Owner picked max crash-safety over the
  cheaper per-Exchange cadence.)
- **`/clear` and `/new` stay aliases — both KEEP.** Both close the current session (it remains
  in history — with per-Turn saves it is already on disk) and start a fresh one. Neither verb
  deletes; discarding is the browser's `d`.
- **Dual persistence.** The TUI transcript is serialized alongside the engine envelope, so
  resume repaints the scrollback exactly (including sub-agent `depth`, tool cards, notes).
  The transcript schema is TUI-owned and versioned independently, opaque to the store —
  mirroring how `Session.State` is engine-owned and opaque to domain.
- **Workspace-scoped browsing.** `Meta.Workspace` records the resolved workspace root; the
  browser lists the current workspace by default with a toggle to all workspaces; resuming a
  foreign-workspace session is allowed and labelled.
- **Defaults accepted with the above:** first-user-message title heuristic (no LLM call) +
  rename in the browser; `/sessions` overlay (resume/delete/rename); keep `--resume` (now id
  or path) and add `--continue`; in-TUI restore via a new engine method (no Agent rebuild);
  store record = metadata wrapper around the untouched engine envelope with backward-compat
  reads of legacy bare files; no auto-pruning (manual delete only; retention → TODO.md).
- **Layer ownership of versions:** `session.RecordVersion` (the wrapper) is owned by
  `internal/session`; the transcript blob's version by `internal/tui`; `domain.SessionVersion`
  by the engine. Each layer rejects/degrades only its own.
- **On any restore error the view is left untouched** and the failure is noted — the same
  "a fresh-looking view must never lie about the engine" rule `/clear` pins.
- **Save failures are soft.** A per-Turn save error must never interrupt the conversation: note
  it once on the ok→fail transition (and note recovery on fail→ok), keep running. Parity with
  apogee-code's swallowed `session_save_failed`.
- **Concurrent instances stay last-write-wins per file** (IDs are per-instance unique, so
  clobbering needs the same session resumed twice). Documented, not locked; a lock is TODO.

---

## 1. `internal/session`: the Record schema and the id-addressed Store — ✅ DONE (2026-07-24)

NOTES (2026-07-24): The old `Save(domain.Session) (string, error)` was NOT deleted this
item — it is renamed to `SaveEnvelope` (freeing the `Save` name for `Save(Record)`) and its
callers point at the new name. Deleting it now would break item 1's own green-build
acceptance: the plan's "grep confirms no other caller" is inaccurate — besides item-5's
`cmd/apogee` sessionSaver, it is called directly by `internal/tui/e2e_test.go` (item 4) and
the root `benchreadiness_test.go`, none rewritten yet. Item 5 deletes `SaveEnvelope` + its
callers. NewID's random suffix is 4 bytes = 8 hex chars (the parenthetical "4 hex random
bytes"), so ids read `...Z-xxxxxxxx`, not the 4-char `-xxxx` schematic placeholder.

**What:** Replace the write-only timestamp Store with the session system's storage layer.

*Types* (new, in `internal/session`):
```go
// Meta is the browsable summary of one stored session — everything the history
// browser shows without decoding the conversation.
type Meta struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Workspace string    `json:"workspace,omitempty"` // resolved root; "" on legacy records
	Model     string    `json:"model,omitempty"`
	UserMsgs  int       `json:"userMsgs"`
	CtxUsed   int       `json:"ctxUsed,omitempty"` // last observed context fill, for the gauge relight
}

// Record is the on-disk shape: the metadata wrapper around the two opaque payloads.
// Transcript is the TUI's versioned scrollback blob (opaque here, exactly as
// Session.State is opaque to domain); Session is the untouched engine envelope.
type Record struct {
	RecordVersion int             `json:"recordVersion"`
	Meta          Meta            `json:"meta"`
	Transcript    json.RawMessage `json:"transcript,omitempty"`
	Session       domain.Session  `json:"session"`
}
```
`const RecordVersion = 1`; decoding a higher version fails with a new sentinel
`ErrRecordVersion` (same reject-forward rule as `domain.SessionVersion`).

*Store rework* (`store.go`):
- `NewID(now)` → `"20060102T150405Z-xxxx"` (UTC second stamp + 4 hex random bytes): lexically
  chronological like today's filenames, collision-safe across instances. The ulid deferral
  (phase-2 plan §2) is hereby resolved with no new dependency.
- `Save(rec Record) error` — writes `<dir>/<rec.Meta.ID>.json` **atomically** (temp file in
  the same dir + `os.Rename`), creating the dir lazily as today (0700/0600). Update-in-place:
  the same ID overwrites its own file. `Save` stamps `RecordVersion`; it never mints IDs or
  touches `Meta` otherwise — cadence and metadata policy live with the caller (item 4/5).
- `List() ([]Meta, error)` — read every `*.json`, decode, return `Meta`s sorted `UpdatedAt`
  descending. A file that fails to decode is skipped (soft — corrupt or foreign files must
  not kill the browser; parity with apogee-code).
- `Load(id string) (Record, error)` and `LoadPath(path string) (Record, error)`.
- `Delete(id string) error` (`os.Remove`), `Rename(id, title string) error` (load → patch
  `Meta.Title` + `UpdatedAt` → atomic save).
- **Legacy sniff** in the decode path both `List` and `Load*` share: a JSON object with no
  `recordVersion` key but a plausible envelope (`Version`/`State` fields) is a pre-plan bare
  `domain.Session` file. Wrap it: synthetic `Meta{ID: <filename stem>, Title: "Session " +
  <stem>, CreatedAt/UpdatedAt: <file mtime>}`, empty `Transcript`. Legacy sessions therefore
  list (workspace-unknown), load, and resume (with no scrollback replay — item 5's note).
- Keep `now func() time.Time` injectable; add an injectable `rand` reader for `NewID` tests.

The old `Save(domain.Session) (string, error)` is DELETED (its only caller, `sessionSaver`,
is rewritten in item 5 — grep confirms no other caller; pre-production, no compat shim).

**Tests** (`store_test.go`): round-trip Save/Load/List; update-in-place (two Saves, one file,
List shows the later `UpdatedAt`); List ordering; corrupt-file skip; legacy bare-envelope
sniff (write a v1 envelope with today's code path shape, assert List+Load wrap it and
`Load` returns a resumable `Record.Session`); `ErrRecordVersion` on a future wrapper;
Delete; Rename persists; atomicity smoke (temp file never left behind on success).

**Acceptance:** `go build ./... && go vet ./... && go test ./...` green. Commit:
`feat(session): id-addressed session records — metadata, listing, legacy reads`.

---

## 2. Engine surface: `RestoreSession` + `InExchange` — ✅ DONE (2026-07-24)

NOTES (2026-07-24): No facade delegators were added to `apogee.go`. `Agent` is a type ALIAS
(`type Agent = agent.Agent`), so the new `RestoreSession`/`InExchange` methods are already part
of the public surface exactly as `Snapshot`/`ClearContext`/`AbortExchange` are — none of those
have delegators either, and Go forbids defining methods on an alias from another package. The
completeness guard (`example_test.go`) names the `Agent` TYPE, not its methods, so it needs no
change. The `restoreState` reuse was done by extracting a shared `(*Agent).restoreSnapshot`
(version-check + `restoreState`) used by BOTH `resumeAgent` and `RestoreSession`; `resumeAgent`'s
version check now runs after `newAgent` instead of before (behaviour-equivalent — the only
observable path, a future-version snapshot, still returns `ErrSessionVersion` on any valid config).

**What:** The in-TUI resume primitive — restore a snapshot into the LIVE Agent (no rebuild,
so tools/mechanisms/MCP wiring stand), plus the probe the interrupted-resume UX needs.

- `(*Agent).RestoreSession(snap domain.Session) error` (`internal/agent/agent.go`, next to
  `ClearContext`): refuse mid-Exchange (`ErrInputPending`) exactly like `ClearContext`;
  version-check like `Resume`; decode into a TEMPORARY state and swap only on full success,
  so a corrupt snapshot leaves the live conversation untouched. Reuse the `restoreState`
  path `resumeAgent` uses (extract a shared helper if needed rather than duplicating).
  Doc comment states the contract: valid only at a quiescent boundary with no worker driving
  (the TUI calls it at idle), replaces conversation + loop counters, does NOT touch the
  allow-for-session cache, mode, or confinement (they are live host state, not session state).
- `(*Agent).InExchange() bool` — reports `a.turns.inExchange`; doc: boundary-only read, for
  the host to detect a restored interrupted Exchange.
- Facade delegators in `apogee.go` (`RestoreSession`, `InExchange`) — the public surface grows
  by the live-restore variant of the ADR 0001 snapshot/resume feature.
- `tui.Engine` (tui.go:34) gains both methods; update `fakeEngine` in the tui tests
  (restore recorded + scripted error; settable inExchange).

**Tests** (`internal/agent`): restore-at-idle round-trips (Snapshot → mutate → RestoreSession
→ conversation equals snapshot; drive an Exchange after to prove liveness); refuse
mid-Exchange; corrupt/future-version snapshot → error AND live state unchanged; InExchange
true for a mid-Exchange snapshot restored, false after `AbortExchange`.

**Acceptance:** build/vet/test green. Commit:
`feat(agent): RestoreSession and InExchange — live restore for in-TUI session switching`.

---

## 3. TUI transcript codec (versioned scrollback serialization) — ✅ DONE (2026-07-24)

NOTES (2026-07-24): Two clarifications where the literal text was silent. (1) The forward-reject
is a named sentinel `ErrTranscriptVersion` (not just "an error"), mirroring item 1's
`ErrRecordVersion` and the engine's `ErrSessionVersion` — the layer-ownership-of-versions rule,
so item 5/7 can `errors.Is` it. (2) The plan says "malformed → error" but does not address an
UNKNOWN entry-kind STRING inside an otherwise valid, supported-version blob: `decodeTranscript`
SKIPS such an entry (and keeps the rest) rather than failing the whole replay — symmetric with
the encode side, which skips `entryStartup` because it has no wire name, and consistent with
`transcript.apply`'s tolerate-unknown default. Genuinely malformed JSON and a future `version`
still error, exactly as written.

**What:** `internal/tui/transcriptcodec.go` — the TUI-owned, versioned wire form of the
scrollback, the `Record.Transcript` blob.

- `const transcriptVersion = 1`; wire envelope `{ "version": 1, "entries": [...] }`.
- Exported-field wire structs mirroring `entry` and its views: kind (as a STRING enum —
  `"user"`, `"assistant"`, `"toolCall"`, `"toolResult"`, `"error"`, `"note"`, `"presented"` —
  not the iota, so a reorder can't corrupt old files), `text`, `depth`, `callID`, `done`,
  `skills`, `tool` (`toolView` incl. its unexported `name` — needed so `enrichWithResult`
  keeps working on replayed entries), `presented` (`presentedView` with `Method` as its
  domain string). `detailLine.Kind` serializes as its underlying value with a doc note
  pinning the constants.
- `encodeTranscript(t *transcript) ([]byte, error)`: serializes committed entries ONLY —
  `entryStartup` is skipped (opening chrome, re-seeded fresh on resume) and the in-progress
  `pending` buffer is dropped (never committed = never persisted).
- `decodeTranscript(data []byte) ([]entry, error)`: future version or malformed → error (the
  caller degrades to no-replay + note, item 5/7); empty/nil data → `(nil, nil)` (the legacy
  case). Decoded text passes through `stripEscapes` again — defence in depth, a session file
  is untrusted disk input.

**Tests** (`transcriptcodec_test.go`): round-trip every entry kind incl. an enriched
tool-call card (Summary + Details + done), a presented entry, depth>0 entries, skills chips;
startup + pending excluded; future-version rejected; escape bytes in a tampered file
stripped on decode; golden JSON for one mixed transcript (pins the wire shape v1).

**Acceptance:** build/vet/test green. Commit:
`feat(tui): versioned transcript codec for session persistence`.

---

## 4. Per-Turn saves through the new `SessionHost` seam (TUI side) — ✅ DONE (2026-07-24)

NOTES (2026-07-24): Three item-scoped deviations, all forced by deleting `Options.Save` while
keeping the build green (item 5 still owns the store-backed host). (1) `cmd/apogee/wire.go` got a
minimal touch: the `Save: saver.save` Options field was removed and `Sessions` left nil (persistence
unwired until item 5); the quit-only `sessionSaver`/`SaveEnvelope` and the resume-hint print are
retained untouched for item 5 to rewrite. (2) `internal/tui/e2e_test.go`'s
`TestE2ESnapshotResumeContinues` no longer round-trips through a disk file (that path was
`Options.Save`→`SaveEnvelope`, both item-5 territory); it now captures the quit-flush snapshot in
memory via the new fake host and resumes from it — same assertion (resumed Exchange continues at
turn 2) — and its now-unused `internal/session` import was dropped. (3) The idle final save lives in
`finishWorker`, gated on `next == stateIdle`, so it fires for exchangeDoneMsg / the cancel path /
compactDoneMsg (all → stateIdle) but NOT the errMsg path (→ stateErrored) — matching the plan's
"final saves *at idle*". The truncation ellipsis is the single rune "…" per the plan's literal text
(apogee-code uses "..."). Title fallback date is `time.Now().Format("2006-01-02")`; the pure
`sessionTitle(text)` signature is kept (no clock injected), so the date-fallback test asserts the
"Session " prefix rather than an exact date.

**What:** Replace the quit-only `Options.Save` func with the session-system seam and the
per-Turn save pipeline. (Depends on 1–3.)

*The seam* (tui.go; the host implements it in item 5 — defined in tui like `SkillCatalog`,
typed against `internal/session` like `skills.Skill`, which sets the import precedent):
```go
type SessionHost interface {
	// Save persists the active session's current state, minting its ID on first call.
	Save(sess domain.Session, transcript []byte, title string, userMsgs, ctxUsed int) error
	// Rotate closes the active session; the next Save mints a fresh ID.
	Rotate()
	List() ([]session.Meta, error)
	// Load returns a stored record AND makes it the active session (Saves go to it).
	Load(id string) (session.Record, error)
	Delete(id string) error
	Rename(id, title string) error
	// ActiveID reports the active session's ID; "" before the first Save.
	ActiveID() string
}
```
`Options.Save` is deleted; `Options.Sessions SessionHost` (nil ⇒ persistence unwired — every
caller guards, as with `Skills`).

*Per-Turn snapshot plumbing:*
- `startExchange` (worker.go:25) gains `notify func(tea.Msg)`; `driveExchange` after each
  `StatusTurnComplete` Step does `snap, err := eng.Snapshot(); if err == nil && notify != nil
  { notify(turnSnapshotMsg{Sess: snap}) }` and keeps stepping. Terminal statuses send nothing
  extra — the Model saves at idle itself. The ordering argument (events-before-notify, see
  "Where things stand") goes in the doc comment.
- `newModel` gains the notify func; `Run` (tui.go:173) builds it over the Bridge's late-bound
  program sender (same package — no exported API). Both `startExchange` call sites
  (model.go:510, model.go:604) pass it.
- New msgs (messages.go): `turnSnapshotMsg{Sess domain.Session}`, `saveDoneMsg{Err error}`.

*The Model's save pipeline* (model.go):
- `snapshotPayload()` helper: encodes the transcript (item 3), derives `title` from the first
  `entryUser` via a `sessionTitle(text string) string` heuristic ported from apogee-code
  (first line, ≤50 chars, word-boundary truncate + "…"; fallback `"Session <date>"`), counts
  `entryUser` entries, reads `m.ctxUsed`.
- On `turnSnapshotMsg`: build payload, dispatch through a **single-flight** async save: if a
  save Cmd is in flight (`m.saveBusy`), stash as `m.pendingSave` (latest wins — coalescing);
  else `m.saveBusy = true` and return a `tea.Cmd` that calls `Sessions.Save` and yields
  `saveDoneMsg`. On `saveDoneMsg`: clear busy, dispatch any pending; on the ok→fail
  transition add the note `"session save failed: <err> — will keep retrying"`, on fail→ok
  add `"session saving recovered"` (track `m.saveFailing bool`).
- Final saves at idle: `finishWorker` (both `exchangeDoneMsg` and the post-`AbortExchange`
  cancel path) and the `compactDoneMsg` fold trigger one more save — at idle the Model owns
  the engine, so it takes its own `Snapshot()`. Gate every save on
  `transcript.hasConversation()` (empty sessions are never written — parity).
- `saveSession` (the quit flush, model.go:738) is rewritten onto the same payload/seam,
  still synchronous and best-effort — it now captures post-last-turn transcript changes
  (notes, /confine output) too. First-turn-crash leaves no file; accepted parity.

**Tests** (model/minilang/worker tests, fake `SessionHost` recording calls): a scripted
exchange with two `StatusTurnComplete` turns produces per-Turn `Save` calls whose payload
transcript decodes to the entries so far; single-flight coalescing (a save in flight + two
turnSnapshotMsgs → exactly one pending dispatched after `saveDoneMsg`); title heuristic
table (short, long-truncate-at-word, code-fence fallback, date fallback); ok→fail→ok note
transitions (exactly two notes); empty-transcript never saves; quit flush goes through the
seam; `driveExchange` with nil notify unchanged (existing worker tests keep passing).

**Acceptance:** build/vet/test green. Commit:
`feat(tui): per-Turn session saves through the SessionHost seam`.

---

## 5. Bind the binary: the store-backed host, `--resume` by id-or-path, `--continue`, startup replay — ✅ DONE (2026-07-24)

NOTES (2026-07-24): Three interpretation calls where the literal text was silent. (1) `buildAgent`
now takes `*session.Record` (nil ⇒ fresh start) rather than a bare record — the resume value has to
carry the no-resume case, and `resolveResume` (the new id-or-path/`--continue` resolver) returns the
same `*session.Record`. (2) Mutual exclusion is enforced in BOTH places: a cobra
`MarkFlagsMutuallyExclusive("resume","continue")` marker for the real CLI flag error AND a guard in
`resolveResume` for the direct-`runRoot` (test) path — the plan said "(flag error)" without saying
which layer, so both are covered. (3) Deleting `SaveEnvelope` (item 1 deferred it here) forced two
test rewrites beyond `cmd/apogee`: the root `benchreadiness_test.go` now builds a `session.Record`
and calls `Save`, and `internal/session/store_test.go`'s legacy-sniff test writes the bare-envelope
bytes directly (`Session.Encode` + `os.WriteFile`) instead of via `SaveEnvelope`. The two moved
`buildAgent` file-read tests were renamed to `resolveResume`-named tests, since that logic left
`buildAgent`. CHANGELOG/ADR/CONTEXT are left to item 9 (the docs item), as the plan assigns.

**What:** cmd/apogee wiring onto items 1–4.

- Rewrite `sessionSaver` (wire.go:439) as `sessionHost`: implements `tui.SessionHost` over
  `*session.Store` + the facts only the binary knows (workspace root, resolved model). It
  owns ID minting (`session.NewID`) on first `Save`, assembles `Meta` (Title set at CREATE
  and never overwritten by later Saves — `Rename` is the only later writer, so a user rename
  sticks; `UpdatedAt` stamped every Save; `Workspace`/`Model` from wiring), `Rotate`/
  `ActiveID`, and delegates List/Load/Delete/Rename. `Load` sets the active ID so subsequent
  Saves update the loaded session's file.
- `--resume` (root.go:187): value resolves as a session ID in the store first, else a path
  (`LoadPath`) — help text: `"resume a saved session (id from /sessions, or a file path)"`.
  `buildAgent` (wire.go:557) now takes the loaded `session.Record`, resumes off
  `rec.Session`, and the host starts ACTIVE on that record (continuing it in place, not
  forking a new file — per-Turn saves make fork-on-resume silent data sprawl).
- New `--continue` flag: resume the most recent session whose `Meta.Workspace` equals the
  resolved workspace; friendly error when none ("no saved sessions for this workspace — see
  /sessions or --resume"). Mutually exclusive with `--resume` (flag error).
- **Startup replay:** `tui.Options` gains `Resumed *ResumedSession { Transcript []byte;
  Title string; CtxUsed int; UserMsgs int }` (nil on a fresh start). `newModel` seeds the
  startup box as today, then — when `Resumed` is set — decodes and appends the replayed
  entries plus the note `"resumed: <title>"`, and relights `m.ctxUsed` from `CtxUsed`. A
  decode error (or the legacy empty blob) degrades to the note
  `"resumed: <title> (no scrollback recorded — the model still remembers)"` with the view
  otherwise fresh — never a fatal error.
- Quit hint (wire.go:330) reworked: `"Session saved · resume with: apogee --continue   (or
  /sessions inside apogee)"`, printed when `ActiveID() != ""`. Root long-help line 127
  ("A clean quit snapshots…") updated to describe continuous saves.

**Tests:** `wire_test.go`-style table tests for the host adapter (ID minted once; title
sticky across Saves; Load activates; Rotate re-mints; Meta workspace/model populated);
`--resume` id-vs-path resolution incl. a legacy bare file (resumes, `Resumed.Transcript`
empty); `--continue` picks the right workspace's newest and errors helpfully when none;
mutual-exclusion error; a `newModel` test proving replayed entries render and the degrade
note appears on a corrupt blob.

**Acceptance:** build/vet/test green. Manual: quit a conversation, `apogee --continue`
repaints the scrollback and the model remembers. Commit:
`feat(cmd): store-backed session host — --resume by id or path, --continue, startup replay`.

---

## 6. `/clear` + `/new` close into history and rotate — ✅ DONE (2026-07-24)

NOTES (2026-07-24): Rotate is placed AFTER the ClearContext success guard, not before the reset body
as the prose "final Save → Rotate → the existing reset body (ClearContext → …)" reads literally —
the item's own "`ClearContext`-error test: no Rotate" requires the rotate to be skipped when
ClearContext refuses, which is only possible with the order Save → ClearContext(guard) → Rotate →
rest-of-reset. The final Save reuses the existing synchronous `saveSession()` quit-flush (its doc was
generalised to name both callers) — synchronous-before-Rotate is what guarantees the outgoing file is
written under the old id before Rotate clears it. The nil-host `/clear`//`/new` view-reset tests are
KEPT (they pin the degrade-to-today path); the host-wired assertions were ADDED alongside rather than
rewriting them. The shared `fakeSessionHost` gained an id lifecycle (mint-on-first-Save/reuse,
clear-on-Rotate) so "next Turn opens a fresh id" is provable. No CHANGELOG/doc edits — item 9 owns docs.

**What:** Wrap the seam exactly as the previous plan's contract specified. (Depends on 4/5.)

`startNewSession()` (model.go) becomes: final `Save` through the seam when
`transcript.hasConversation()` (the outgoing session's last state, including post-turn
notes) → `Sessions.Rotate()` → the existing reset body unchanged (ClearContext → reset →
re-seed box → zero gauge). Both verbs stay aliases (owner-ratified: both KEEP). The save
runs BEFORE `ClearContext` — the inherited ordering falls out. On a `ClearContext` error
the existing untouched-view error path stands; the completed save is harmless (the session
was closing anyway). Nil `Sessions` degrades to today's behaviour exactly.

Update the `startNewSession` doc comment: it no longer promises a future wrap — it IS the
wrap. Doc `/new`'s help wording in `commandHelp` if it names quit-only saving.

**Tests:** rework the minilang `/clear`//`/new` tests: fake host asserts one final Save
(payload transcript contains the seeded conversation) then Rotate, and the next exchange's
per-Turn save mints a NEW ID (fake host counts). `/clear` with nothing beyond the box does
not Save, but DOES Rotate — Rotate is idempotent on an inactive session, and calling it
unconditionally means a stale active ID can never leak into the next conversation; assert
both halves. `ClearContext`-error test: no Rotate, view untouched (extends the existing
pinned error test).

**Acceptance:** build/vet/test green. Manual: converse → `/new` → converse → `/sessions`
(item 7) shows BOTH sessions. Commit:
`feat(tui): /clear and /new close the session into history and rotate`.

---

## 7. The `/sessions` history browser overlay — ✅ DONE (2026-07-24)

NOTES (2026-07-24): Four item-scoped calls where the literal text was silent or in tension with the
shipped code. (1) The interrupted-note wording ("wording in item 8") is defined NOW as a shared
package const `interruptedNote` in `sessions.go` (item 7 appends it on a mid-Exchange browser resume,
per this item's own bullet); item 8 reuses the same const for its startup/`/continue` paths rather
than redefining it. (2) The inline rename edit PRE-FILLS the current title (an edit/tweak) instead of
starting empty — the plan says only "minimal line edit on the row: type/backspace"; enter with an
empty/whitespace title is a no-op. (3) Opening is gated on the RAW `List()` result being non-empty,
not the workspace-filtered view: a store holding sessions only in OTHER workspaces still opens (with a
"press a to see all" placeholder row) so `a` is reachable — only a totally empty store degrades to the
"no saved sessions" note. (4) The restore-ERROR path leaves the VIEW and the LIVE ENGINE conversation
untouched + notes the failure (the load-bearing half of the locked error rule), but it CANNOT leave
the host's active id untouched: item 5's shipped `Load` (✅ done) activates the loaded session
unconditionally before handing back the record, and the `SessionHost` interface exposes no setter to
revert it. So the browser-resume error test asserts view+engine untouched + the note, not active-id
untouched. See FOLLOW-UP. [RESOLVED 2026-07-24 by follow-up fix: `Load` no longer activates — a new
`SessionHost.Activate(Meta)` seam is called by the resume path only AFTER `RestoreSession` succeeds,
so a failed restore now leaves the active id untouched; `TestSessionBrowserResumeErrorLeavesActiveSessionUntouched`
pins it.] Also: `TestCommandDropdownOffersSkill` was retargeted from `/s` to `/sk`
(now that `/sessions` shares the `s` prefix) — same intent, still asserts the `/skill` suggestion.

**What:** The in-TUI list/resume/delete/rename surface. (Depends on 1–5.)

- Parser: add `"sessions"` to `knownCommands` (command.go:52) + autocomplete description.
  Idle-only, like the other synchronous verbs.
- New overlay state (own file `internal/tui/sessions.go` + `sessionbrowser` struct on the
  Model, rendered like the approval/ask prompts — a boxed pane above the input, capped rows
  with scroll): rows `title · relative time · N msgs`, foreign-workspace rows suffixed
  `· <workspace base>` in the all-workspaces view. Opening runs `Sessions.List()` through a
  `tea.Cmd` (disk I/O off the Update loop) → `sessionListMsg`; empty list → note
  `"no saved sessions"` and no overlay.
- Keys: `↑/↓` select, `enter` resume, `a` toggle current-workspace ⇄ all (default: current;
  legacy workspace-less records appear only under all), `d` arms an inline
  `"delete? y/n"` confirm on the row (`y` deletes via a Cmd, list refreshes; deleting the
  ACTIVE session also Rotates + notes `"current session's file deleted — it lives on in
  memory; the next turn saves it as a new session"`), `r` inline-renames (minimal line edit
  on the row: type/backspace, enter commits `Sessions.Rename`, esc cancels), `esc` closes.
- Resume flow: `enter` → Cmd `Sessions.Load(id)` → `sessionLoadedMsg{Record}` → at idle:
  `eng.RestoreSession(rec.Session)`; on success `transcript.reset()` + re-seed box + decode
  & append `rec.Transcript` (decode failure → the no-scrollback note, view otherwise fresh)
  + `"resumed: <title>"` note; `m.ctxUsed = rec.Meta.CtxUsed`, `tokPerSec = 0`,
  `pendingSkills` dropped, `userScrolled` re-armed — the same field set `startNewSession`
  resets. On `RestoreSession` error: note `"could not restore session: <err>"`, view and
  active session untouched (the locked error rule). The current conversation needs no save
  prompt — per-Turn saves mean it is already on disk (Rotate happens implicitly via Load's
  activation switching the ID).
- Mid-Exchange record resumed (`eng.InExchange()` true after restore): append the
  interrupted note (wording in item 8) — item 8 supplies the drive.

**Tests:** browser open lists sorted metas filtered to the workspace; `a` widens; resume
happy-path (fake engine records RestoreSession; transcript equals replay + note; gauge
relit; subsequent saves target the loaded ID via fake host); restore-error leaves view +
active ID untouched; delete confirm + active-delete rotate; rename commits; `esc` layers
(rename-edit → row → closed); overlay refuses to open while busy.

**Acceptance:** build/vet/test green. Commit:
`feat(tui): /sessions history browser — list, resume, delete, rename`.

---

## 8. Interrupted-Exchange resume: the step-only drive — ✅ DONE (2026-07-24)

NOTES (2026-07-24): Three item-scoped deviations. (1) `driveResume` shares `driveExchange`'s loop via
a new unexported `stepToBoundary(ctx, eng, notify)` helper rather than duplicating it — `driveExchange`
now Submits then delegates to it, and `driveResume` calls it straight (DRY over two copies of the
subtle events-before-notify ordering; behaviour-identical, existing worker tests unchanged). (2) The
interrupted `/continue` path leaves `pendingSkills` AND `userScrolled` untouched (the literal
"note-free transcript untouched" reading) — unlike the canned `/continue`/submit paths which clear
them; a step-only resume has no Submit to carry staged skills into, and touching neither is the most
faithful "untouched". (3) Test infra: the shared `fakeEngine.AbortExchange` now flips its `inExchange`
to false (modelling the real Agent returning to a clean boundary), so the abort-then-submit /
abort-then-clear / live-cancel-stays-canned tests read InExchange faithfully; no existing test relied
on it staying true after an abort. No docs touched — item 9 owns CHANGELOG/ADR/CONTEXT.

**What:** Make per-Turn saves actually pay off — a session that died mid-task continues.
(Depends on 2, 5, 7.)

- New worker variant (worker.go): `startResume(parent, eng, notify)` → `driveResume`, which
  is `driveExchange` minus the Submit — Step to the boundary with the same per-Turn notify,
  cancel, and terminal handling. Doc: the TUI counterpart of the bench's re-Step path
  (agent.go:164's comment).
- `/continue` (model.go:596) becomes context-sensitive: when `eng.InExchange()` (only ever
  true right after an interrupted restore — the TUI aborts on every live cancel), it starts
  the step-only worker with the note-free transcript untouched; otherwise the existing
  canned "Please continue" Submit. The interrupted note (items 5/7) reads:
  `"this session was interrupted mid-task — /continue picks up where it left off; sending a
  new message discards the unfinished work"`.
- Plain submit while `InExchange()`: `AbortExchange()` first, then the normal submit path,
  with the note `"discarded the interrupted work — continuing fresh from your message"`.
  (Esc-parity: the user's new intent supersedes the stale half-Exchange. Alternative —
  refusing until an explicit verb — rejected as a dead-end prompt; the note keeps it
  honest.) `/clear`//`/new` on an interrupted session: `startNewSession` already handles it —
  `ClearContext` would refuse mid-Exchange, so it calls `AbortExchange()` first when
  `InExchange()` (one extra guarded line), then proceeds.
- `--resume`/`--continue` of a mid-Exchange record at startup: newModel appends the same
  interrupted note (Options.Resumed gains `InExchange bool`, set by wire from
  `agent.InExchange()` after build).

**Tests:** fake-engine: interrupted restore + `/continue` drives Step without Submit to
completion (worker test with scripted step results); plain submit aborts then submits (order
asserted) with the note; `/clear` on interrupted aborts then clears; startup note appears;
live-cancel path unchanged (Esc still aborts — `InExchange()` false afterwards, `/continue`
stays the canned submit).

**Acceptance:** build/vet/test green. Manual (owner): kill -9 apogee mid-tool-loop; relaunch
`apogee --continue`; scrollback repaints up to the last completed Turn, the note shows,
`/continue` finishes the task. Commit:
`feat(tui): interrupted sessions resume — step-only /continue drive`.

---

## 9. Docs: ADR, CONTEXT, TODO, CHANGELOG — ✅ DONE (2026-07-24)

NOTES (2026-07-24): Two item-scoped calls. (1) The listed TODO follow-up "`Snapshot`-mid-run for the
bench" was NOT added — the plan conditions it on "only if genuinely deferred", and the "Explicitly
NOT in this plan" section records the bench as a deliberate non-goal ("No bench changes … keeps
composing `Snapshot`/`Encode` directly (ADR 0001)"), i.e. not a deferral. Only retention/pruning and
the cross-instance lock (both named as recorded TODOs) were added, in a new `## Session system
follow-ons` section; the non-goals (incl. the bench) are listed there as explicit non-TODOs so they
are not re-opened. (2) CONTEXT gained a combined **Session / Session record** entry (the bare Session
was never defined) placed right after the Sub-agent entry per the plan's "near the Sub-agent entry's
Session mention". CHANGELOG entry went under a new `### Added` block (Keep-a-Changelog order).

**What:** Record the ratified design.

- **ADR 0022 — "Sessions persist per-Turn as dual-representation records"**: the four
  owner-ratified decisions + the layer-ownership-of-versions rule + last-write-wins
  concurrency posture + what is deliberately NOT session state (mode, approvals, confinement,
  MCP connections — with the ADR 0001/0008 cross-refs). Supersedes nothing; extends ADR 0001.
- CONTEXT.md: add a **Session record** glossary entry (store wrapper vs engine Session vs
  transcript blob; the browser; per-Turn cadence) near the Sub-agent entry's Session mention.
- TODO.md: the P1 "Session management UI" item → done-with-pointer (like earlier shipped
  items); add follow-ups under their priority: retention/pruning policy, cross-instance file
  lock, `Snapshot`-mid-run for the bench… only if genuinely deferred.
- CHANGELOG.md: user-facing entry (per-Turn autosave, `/sessions`, `--continue`, resume
  replay, interrupted-task continue).
- ISSUES.md: strike "session-management UI" from the parity gap line.

**Tests:** none (docs). **Acceptance:** docs read true against the landed code; `make check`
green. Commit: `docs(adr,context,changelog): record the session system`.

---

## Explicitly NOT in this plan

- **No retention/pruning** — manual `d` only; policy is a recorded TODO.
- **No cross-instance locking** — last-write-wins documented (TODO).
- **No LLM-generated titles**, no session search, no session export.
- **No sub-agent session persistence** — child Sessions stay ephemeral (their effects live in
  the parent's conversation and transcript, which DO persist).
- **No serialization of mode / approvals / confinement / MCP state** — re-confirmed live per
  ADR 0008 and the P1.6 decision; the ADR restates it.
- **No bench changes** — `session.Store`'s new API stays embeddable; the bench keeps composing
  `Snapshot`/`Encode` directly (ADR 0001).
- **No mid-Turn persistence** — the quiescent boundary (ADR 0007) remains the only snapshot
  point; "max crash-safety" means per-Turn, not per-token.

## Critical files

- `internal/session/store.go` (+`doc.go`) — Record/Meta, id-addressed Store (item 1)
- `internal/agent/agent.go`, `internal/agent/state.go`, `apogee.go` — RestoreSession/InExchange (item 2)
- `internal/tui/transcriptcodec.go` (new) — the versioned scrollback blob (item 3)
- `internal/tui/tui.go`, `worker.go`, `messages.go`, `model.go` — SessionHost seam, per-Turn
  pipeline, startNewSession wrap, interrupted drive (items 4, 6, 8)
- `internal/tui/sessions.go` (new), `command.go`, `autocomplete.go` — the browser (item 7)
- `cmd/apogee/wire.go`, `cmd/apogee/root.go` — host adapter, flags, replay wiring (item 5)
- `docs/adr/0022-…`, `CONTEXT.md`, `TODO.md`, `CHANGELOG.md`, `ISSUES.md` (item 9)

## Owner-run checklist (after implementation)

- [ ] Converse a few turns; `kill -9` the process mid-tool-run. `apogee --continue`: the
  scrollback repaints up to the last completed Turn, the interrupted note shows, `/continue`
  finishes the task where it stopped.
- [ ] `/new`, converse again, `/sessions`: both sessions listed (titles = first messages,
  newest first); resume the older one — full scrollback repaints, the model remembers it,
  the context gauge relights near its old fill.
- [ ] Rename a session in the browser; quit; `/sessions` next launch shows the new title
  (and later turns in it don't revert the title).
- [ ] `d` on a session deletes after `y`; deleting the ACTIVE one keeps the conversation
  alive and the next turn re-saves it under a fresh id.
- [ ] From a second workspace, `/sessions` hides the first workspace's sessions until `a`.
- [ ] `--resume` with one of the OLD pre-plan timestamp files still works (no scrollback,
  honest note, model remembers).
- [ ] Watch `~/.apogee/sessions/` during a long exchange: the active file's mtime ticks
  every Turn; pulling the plug never leaves a `*.tmp` or truncated JSON behind.
