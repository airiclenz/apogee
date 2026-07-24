# Plan — `/clear` and `/new` reset the session view and reprint the start-up box

**Date:** 2026-07-24. **Status: PLAN — not started.** Execute with `/implement-plan` in a fresh
session, forwarding skills: `coding-standards`.

Owner request: when `/clear` or `/new` is submitted, the **session view is completely cleared** and
the **start-up box is reprinted** — the two verbs "basically start a new session". The change must
**not block later session handling**: the forthcoming session system is one of the next steps, so
this plan funnels the whole reset through **one seam** the session manager can wrap, and writes
nothing to any session store.

This **reverses** the behaviour a prior plan deliberately built. `docs/plans/archived/2026-07-23 - 02
- version-command-and-startup-box-plan.md` item 3 seeded the box as `entries[0]` specifically so it
would *survive* `/clear` ("run `/clear` — the box stays put and is not re-drawn"). The owner now wants
the opposite: `/clear`/`/new` wipe the scrollback and **re-seed** the box, leaving the view identical
to a fresh launch.

Target behaviour after `/clear` (or `/new`):

```
╭─────────────────────────────────────────────────────╮
│   ▀▀█▄ ████▄ ▄███▄ ▄████ ▄███▄ ▄███▄                │
│  ▄█▀██ ██ ██ ██ ██ ██ ██ ██▄█▀ ██▄█▀                │
│  ▀█▄██ ████▀ ▀███▀ ▀████ ▀█▄▄▄ ▀█▄▄▄                │
│       ██           ▄▄█▀                             │
│                                                     │
│  host     192.168.64.1:1111                         │
│  model    gpt-oss-20b                               │
│  context  32k                                       │
│  version  v0.8.1                                    │
╰─────────────────────────────────────────────────────╯
```

— and **nothing else**: no prior user/assistant/tool blocks, and **no "context cleared" note**. The
reprinted box is itself the signal that a new session began.

## Where things stand (grounded, verified 2026-07-24)

**`/clear` + `/new` are already parsed and routed; only their effect changes.**
- Parser: `knownCommands` (`internal/tui/command.go:52`) already lists `"clear"` and `"new"`; both
  route to the same `case "clear", "new":` in the dispatch switch. `/new` is documented as an alias
  of `/clear`. **No parser or autocomplete change is needed** — this plan touches only the effect.
- Dispatch: `Model.runCommand` (`internal/tui/model.go:561`), the `case "clear", "new":` at
  `model.go:587-602`. Today it: drops `pendingSkills`, calls `m.eng.ClearContext()`, and **on success**
  zeroes `m.ctxUsed`/`m.tokPerSec` and records the note `"context cleared — the model's memory of this
  session is reset"`; **on error** records `"could not clear context: <err>"`. Either way it calls
  `m.layout()` and stays `stateIdle` with a nil `tea.Cmd` (no worker). `runCommand` is reached only
  from `submit` (`model.go:501`), which fires only in `stateIdle` (`handleKey`, `model.go:396-397`) —
  so **no worker owns the engine** when the case runs.

**`ClearContext` is fallible only mid-Exchange.**
- `(*Agent).ClearContext` (`internal/agent/agent.go:277-283`) returns `domain.ErrInputPending` iff
  `a.turns.inExchange`, else replaces `a.conv` and returns nil. Since `/clear` runs only at idle,
  `inExchange` is false and it does not error in practice — but the error path is real and this plan
  **preserves** it (a fresh-looking view must never lie about an engine that still remembers).

**The start-up box is a normal transcript entry, seeded once.**
- `transcript` (`internal/tui/transcript.go:23-30`) is `entries []entry` + a `pending`/`streaming`
  in-progress assistant buffer + a hidden `debug` flag. `entryStartup` + `startupView`
  (transcript.go:44, 90-96) hold the box's facts (`Logo`/`Host`/`Model`/`Context`/`Version`);
  `addStartup` (transcript.go:139-143) appends the entry, escape-stripping `Host`/`Model`.
- `newModel` (`internal/tui/model.go:115-148`) builds the Model, then seeds exactly one entry with an
  **inline `startupView{…}` literal** (model.go:140-146): `Logo: strings.TrimRight(apogeeLogo,"\n")`,
  `Host: hostDisplay(opts)`, `Model: displayModel(opts.Model)`, `Context: formatTokens(opts.ContextWindow)`,
  `Version: opts.BaseVersion`. `hostDisplay` (model.go:1057), `displayModel`, `formatTokens` are free
  helpers taking values. **Note `Version` reads `opts.BaseVersion`** (the clean release version), *not*
  `opts.Version` — `TestStartupBox…` (model_test.go:160-164) guards exactly this.

**The renderer already reflows the box at the live width.**
- `renderEntryLines`/`renderStartupBox` (`internal/tui/render.go:127-128, 234-314`) render the box
  fresh on every repaint, so a re-seeded box needs no special handling.

**Viewport / scroll reset is already handled by the normal render path.**
- `refreshViewport` (`internal/tui/model.go:820-844`) rebuilds `m.lines`/`m.userBlocks`, zeroes
  `m.transcriptSel`, and — when `userScrolled` is false and there is no user block
  (`lastUserStart < 0`) — `SetContentLines` + `GotoBottom`. With only the start-up box present this is
  exactly the fresh-launch path, so **resetting `userScrolled=false` and calling `layout()` reproduces
  the launch view** with no bespoke scroll code.

**Session persistence is quit-only and untouched by this plan.**
- `saveSession` (`model.go:726-735`) snapshots through the host saver **only on a clean quit**, gated
  by `transcript.hasConversation()` (transcript.go:149-156, true iff any entry is not the start-up
  box). `/clear` today already discards the engine conversation unsaved; this plan keeps that parity.
  The single new seam (`startNewSession`, item 2) is where the future session manager will insert
  *save-the-outgoing-conversation* + *allocate-a-new-session-id* — see **Explicitly NOT in this plan**.

**Tests that assert today's behaviour (must be updated in item 2):**
- `internal/tui/minilang_test.go:41` `TestClearCommandClearsEngineKeepsTranscript` — asserts the
  `"context cleared"` note; its very name ("KeepsTranscript") becomes wrong.
- `internal/tui/minilang_test.go:64` `TestNewCommandAliasesClear` — asserts the `"context cleared"`
  note (plus `clearCalls == 1`, which stays true).
- `internal/tui/minilang_test.go:87` `TestClearCommandSurfacesEngineError` — asserts the
  `"could not clear context"` note on a `ClearContext` error. This path is **preserved**, so the test
  **stays green** and needs no change (it pins the error branch this plan keeps).
- `internal/tui/model_test.go:172` `TestStartupBoxSurvivesClear` — asserts `entries[0]` is still the
  box after `/clear`; must be **inverted** to prove the transcript is reset to *only* the box.

### Decisions locked (recommended defaults — alternatives noted, do not re-litigate silently)

- **One seam: a `startNewSession()` Model method.** `/clear` and `/new` become the one-liner
  `return m.startNewSession()`. All the reset lives in this method so the future session manager wraps
  a single call. (Alternative: expand the inline `case`; rejected — it scatters the reset and gives the
  session system no hook, contradicting "don't block later session handling".)
- **Fresh-launch-identical contract.** After `/clear`, the view equals a fresh launch at the same
  window size: transcript = `[entryStartup]`, `userScrolled=false`, live gauge/throughput at zero. This
  is the cleanest, testable definition of "start a new session" and reuses the existing render path (no
  custom `GotoTop`).
- **No success note.** The reprinted box is the signal; the `"context cleared …"` note is removed. The
  **error** note is kept.
- **On `ClearContext` error, do not reset the view.** Report the failure and leave the scrollback
  intact — a fresh view over an engine that still remembers is worse than a visible error.
- **DRY the box facts into `newStartupView(opts Options) startupView`** so `newModel`'s seed and the
  re-seed read one source and cannot drift (the `BaseVersion`-vs-`Version` distinction especially).
- **Preserve `transcript.debug`** across the reset (a hidden view toggle, not conversation).
- **Lifecycle fields are not touched** (`state`/`cancel`/`pending`/`pendingAsk`/`lastErr`): `/clear`
  is idle-only, so there is no worker, approval, or error-display state to unwind.

---

## 1. `transcript.reset()` primitive + `newStartupView` helper (no behaviour change) — ✅ DONE (2026-07-24)

NOTES (2026-07-24): `TestNewStartupViewMatchesSeed` uses a local `opts` with distinct
`Version`/`BaseVersion` (mirroring `TestNewModelSeedsStartupBox`) rather than the literal `testOpts`
the plan named: `testOpts` leaves both version fields empty (`"" == "" == testOpts.Version`), so the
plan's `Version != testOpts.Version` assertion could never hold. The drift guard (equals seed) and
both version assertions all pass against the local opts.

**What:** Two small, behaviour-neutral extractions that item 2 composes.

*Reset primitive.* In `internal/tui/transcript.go`, add next to `addStartup`:
```go
// reset returns the transcript to its empty state — no committed entries and no in-progress
// assistant buffer — while preserving the debug flag (a hidden view toggle, not conversation).
// It is the /clear + /new "start a new session" primitive: the caller re-seeds the one-time
// start-up box with addStartup so the fresh view matches a launch. It does NOT touch the engine's
// memory (ClearContext) — that is the caller's separate, fallible step (model.startNewSession).
func (t *transcript) reset() {
	t.entries = nil
	t.pending = ""
	t.streaming = false
	// t.debug is deliberately preserved across a session reset.
}
```

*Box-facts helper (DRY).* In `internal/tui/model.go`, extract the inline `startupView{…}` literal
`newModel` uses (model.go:140-146) into a free function near `hostDisplay`:
```go
// newStartupView builds the one-time start-up box's facts from the resolved display Options — the
// single source both newModel's seed and /clear's re-seed (startNewSession) read, so the fresh-launch
// box and the post-/clear box can never drift. Version is BaseVersion (the clean release version), not
// the full provenance-tagged Options.Version the footer shows.
func newStartupView(opts Options) startupView {
	return startupView{
		Logo:    strings.TrimRight(apogeeLogo, "\n"),
		Host:    hostDisplay(opts),
		Model:   displayModel(opts.Model),
		Context: formatTokens(opts.ContextWindow),
		Version: opts.BaseVersion,
	}
}
```
Then replace `newModel`'s inline seed (model.go:140-146) with:
```go
m.transcript.addStartup(newStartupView(opts))
```
Leave the surrounding comment (model.go:136-139) in place.

**Tests:** In `internal/tui/transcript_test.go`, add `TestTranscriptReset`: build a transcript, append
a startup box + a user entry + stream a token into `pending`, set `debug = true`, call `reset()`, then
assert `len(entries) == 0`, `pending == ""`, `streaming == false`, and `debug == true` (preserved). In
`internal/tui/model_test.go`, add `TestNewStartupViewMatchesSeed`: assert `newStartupView(testOpts)`
equals the `startupView` `newModel(...)` seeded at `entries[0].startup` (guards the extraction against
drift), and that its `Version == testOpts.BaseVersion` and `!= testOpts.Version`.

**Acceptance:** `go build ./... && go vet ./... && go test ./...` green (no behaviour change — the
existing `/clear` and start-up-box tests still pass). Commit:
`refactor(tui): add transcript.reset() and newStartupView() seam`.

---

## 2. `/clear` and `/new` start a new session (view wipe + box re-seed) — ✅ DONE (2026-07-24)

NOTES (2026-07-24): Also updated three doc comments the reversal makes stale — `newModel`'s seed
comment (it no longer "survives /clear … never the transcript scrollback"; now "/clear re-seeds it
through startNewSession"), and the `addStartup` + `startupView` doc comments in transcript.go that
said the box is "seeded once by newModel" / "survives /clear" (now note the startNewSession re-seed).
Item 1 had said "leave the surrounding comment in place", but that predated the behaviour change;
leaving them would assert the opposite of item 2's code. Tests use a shared `seedConversation` helper
+ `seededAssistantText` const (in minilang_test.go) rather than inlining the seed in each test.

**What:** (depends on item 1.) Add the seam method and route both verbs through it.

*The seam.* In `internal/tui/model.go`, add near `runCommand`:
```go
// startNewSession resets the TUI to a fresh session: it drops the engine's conversation memory
// (ClearContext), wipes the transcript scrollback, and re-seeds the one-time start-up box so the view
// is byte-identical to a fresh launch at this window size. /clear and its alias /new both route here —
// "start a new session" is exactly what they mean.
//
// This is the single seam the forthcoming session system extends: saving the outgoing conversation and
// allocating a new session id will wrap THIS call, so the view/state reset lives in one place and
// nothing here writes to a session store or assumes a session model.
//
// Reached only from runCommand at stateIdle (no worker owns the engine), so ClearContext is safe. On a
// ClearContext error the view is left untouched and the failure is noted — a fresh-looking view must
// never lie about an engine that still remembers the old conversation.
func (m Model) startNewSession() (tea.Model, tea.Cmd) {
	if err := m.eng.ClearContext(); err != nil {
		m.transcript.addNote("could not clear context: " + err.Error())
		m.layout()
		return m, nil
	}
	m.transcript.reset()
	m.transcript.addStartup(newStartupView(m.opts))
	m.pendingSkills = nil   // the staged chips belonged to the abandoned session
	m.userScrolled = false  // re-arm sticky-to-top: the fresh box pins to the top like a launch
	m.ctxUsed = 0           // the gauge and throughput fall with the discarded conversation…
	m.tokPerSec = 0         // …the same reason compactDoneMsg zeroes them on a fold
	m.genStart = time.Time{}
	m.flash = ""            // drop any transient copy note; a new session shows nothing stale
	m.layout()
	return m, nil
}
```

*Route both verbs.* Replace the `case "clear", "new":` body (model.go:587-602) with:
```go
case "clear", "new":
	// /new is an alias of /clear: both start a fresh session — wipe the view, reset the engine's
	// memory, and reprint the start-up box (startNewSession).
	return m.startNewSession()
```

*Doc comment.* Update `runCommand`'s doc (model.go:549-560): `/clear` (and `/new`) no longer "records
a transcript note" — it "resets the session view and reprints the start-up box". Keep `/version` and
`/confine` described as the synchronous, note-recording/idle verbs.

**Tests:**
- Rework `internal/tui/minilang_test.go:41` `TestClearCommandClearsEngineKeepsTranscript` →
  `TestClearResetsSessionView`: after `/clear`, assert `eng.clearCalls == 1`, `state == stateIdle`,
  nil `cmd`, input empty, and `len(m.transcript.entries) == 1` with `entries[0].kind == entryStartup`
  and the view **no longer contains** `"context cleared"`. To prove the wipe, seed conversation first
  (route a user message, or append a user + assistant entry) so the pre-clear transcript has > 1 entry.
- Update `internal/tui/minilang_test.go:64` `TestNewCommandAliasesClear`: keep `clearCalls == 1`,
  idle, nil cmd, input empty; replace the `"context cleared"` assertion with the same
  reset-to-`[entryStartup]` assertion (proving `/new` shares the seam).
- Invert `internal/tui/model_test.go:172` `TestStartupBoxSurvivesClear` →
  `TestClearResetsToStartupBox`: seed a user + assistant entry, submit `/clear`, assert
  `len(entries) == 1 && entries[0].kind == entryStartup` and that the prior text is gone from the view.
- `internal/tui/minilang_test.go:87` `TestClearCommandSurfacesEngineError` is **unchanged** — assert
  in the reworked suite that on a `ClearContext` error the transcript is **not** reset (the seeded
  conversation entries survive) alongside the existing `"could not clear context"` note, pinning the
  "don't reset the view on error" decision.

**Acceptance:** `go build ./... && go vet ./... && go test ./...` green. Manual (owner checklist):
launching, sending a couple of messages, then `/clear` (and separately `/new`) leaves only the
start-up box — no prior messages, no note — identical to a fresh launch. Commit:
`feat(tui): /clear and /new reset the session view and reprint the start-up box`.

---

## Sequencing — why this ships before the session system

The session system (persist/list/resume sessions under `~/.apogee/sessions/`) is a **separate,
later plan**, not folded in here. Deliberate:

- **The seam is the point of the split.** This plan's real deliverable is `startNewSession()` — the
  single hook the session manager wraps. Landing `/clear`/`/new` against it first validates the seam
  shape cheaply, before the session system commits to it. Folding the two together would turn the
  boundary into internal plumbing and forfeit that check.
- **Asymmetric risk.** The view-reset is small, fully specified, and usable the moment it lands. The
  session system has genuinely open design questions (persistence format, storage location, resume/list
  UX, failure handling). Coupling a done-able change to an undesigned one only stalls the done-able one.

**Contract the session plan inherits (do not re-derive):**
- The session manager's `/new` composes as: `Snapshot()` → persist the outgoing conversation →
  allocate a new session id → `startNewSession()`. The save **wraps** the seam call, so the one
  ordering rule — snapshot *before* the `ClearContext()` that lives inside `startNewSession()` — falls
  out for free. No rework of the `/clear`/`/new` wiring is needed.
- **Open decision the session plan owns:** whether `/clear` and `/new` **diverge** once sessions exist
  (e.g. `/new` = save-then-start-fresh, `/clear` = discard-without-saving) or stay aliases. This plan
  keeps them aliased — the honest pre-session behaviour — and does not pre-empt that call.

## Explicitly NOT in this plan

- **No session system.** No session ids, no `~/.apogee/sessions/` writes, no multi-session store, no
  save-before-clear. `/clear`/`/new` discard the outgoing conversation unsaved — the same as `/clear`
  does today. `startNewSession()` is deliberately the **one place** the future session manager wraps to
  add "snapshot + persist the outgoing conversation, then allocate a new session id" before the reset.
- **No change to `saveSession` / `hasConversation` / the quit-snapshot path** (`model.go:710-735`,
  `transcript.go:149-156`). A `/clear` immediately before quit still snapshots nothing (the box-only
  transcript is not a conversation), exactly as now.
- **No parser or autocomplete change** — `/clear` and `/new` are already recognised and menu-listed.
- **No renderer change** — `renderStartupBox` already reflows the re-seeded box at the live width.
- **No change to the other synchronous verbs** (`/version`, `/confine`) or the worker verbs
  (`/continue`, `/compact`).

## Critical files

- `internal/tui/transcript.go` — new `transcript.reset()` primitive
- `internal/tui/model.go` — new `newStartupView(opts)` helper; `newModel` seed rewired to it; new
  `startNewSession()` seam; `case "clear", "new"` routes to it; `runCommand` doc updated
- `internal/tui/minilang_test.go` — rework the two `/clear`/`/new` success tests; extend the error test
  to assert no view reset
- `internal/tui/model_test.go` — invert `TestStartupBoxSurvivesClear`; add `TestNewStartupViewMatchesSeed`
- `internal/tui/transcript_test.go` — add `TestTranscriptReset`

## Owner-run checklist (after implementation)

- [ ] Launch `apogee`; send two or three messages (let one stream a reply and run a tool). Submit
  `/clear` — the whole scrollback disappears and only the start-up box remains, at the top, with **no
  "context cleared" note**; the view looks like a fresh launch. The context-usage gauge and tok/s in
  the chrome read empty again.
- [ ] Repeat with `/new` — identical result (it aliases `/clear`).
- [ ] After a `/clear`, send a new message — the model has no memory of the pre-clear conversation
  (the engine context really was reset), and the new exchange renders normally beneath the box.
- [ ] Resize the window after a `/clear` — the re-seeded box reflows without duplicating.
- [ ] (If reachable) force a `ClearContext` failure — the view is **not** wiped and the
  `"could not clear context"` note appears, so the view never disagrees with the engine.
