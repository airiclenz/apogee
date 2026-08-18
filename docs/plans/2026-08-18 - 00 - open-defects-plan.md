# Open-defects plan — the 2026-08-16 run residuals

**Goal:** close the five open defects in `ISSUES.md` ("Run residuals — open (2026-08-16)"), plus
one same-shape defect found while scouting (`response-reserve`).

**Date:** 2026-08-18 · **Status:** ready · **Sized for:** ~200k-context host

**Authoritative sources:**
- `ISSUES.md:26-52` — the five defect entries (marked `[P]` for this plan).
- ADR 0037 (`docs/adr/0037-*.md`) decision 3 — a boundary note is the only deferral wording a
  settings row has; no row ever says "(next launch)". This plan conforms to it; no amendment.
- `internal/tui/doc.go:41-70` and `internal/tui/transcript.go:279-292` — the rail/contiguity
  invariant items 6 relies on.
- `internal/provider/client_test.go:611-615` — the pin that a non-streaming success body is never
  recorded. Item 4 must not touch it.

**Ratified design calls** (owner, via question round, 2026-08-18):
1. `url-safety.allow-hosts` / `deny-hosts` edits apply LIVE via the tools rebuild door
   (`liveTools` → `SwapTools`), exactly as `tools.disabled` does.
2. `ui.inspector` and `response-reserve` edits succeed silently (the `editor`-key precedent);
   the key Description carries the "takes effect at the next start" contract. No row note,
   no failure, no ADR change.
3. The Inspector draws one house-style note row under a request record that has later records
   but no recorded reply; the newest request stays bare (it may still be in flight).
4. A host note (or schedule-firing block) landing while a sub-agent run/group is drawing lands
   AFTER that run's (and its fan-out group's) contiguous stretch, at depth 0. Notes answer the
   human; they are never railed into a delegate's run.

**Standing requirements:**
- skills: coding-standards
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- Each item removes its own `ISSUES.md` entry (the register holds open work only) and provides
  its CHANGELOG `[Unreleased]` entry text via the sidecar. Exception: item 1 leaves the shared
  settings entry to item 2.

**Out of scope:**
- `addInterjected` placement (`internal/tui/transcript.go:468`) — interjections commit at Turn
  boundaries with their own semantics (ADR 0025); not a host note.
- Recording non-streaming success bodies in the provider (pinned design stays).
- A live-apply seam for `ui.inspector` (needs an engine seam + client rebuild — not wanted).
- Any ADR 0037 amendment.
- The engine-side guard construction (`internal/agent/construct.go:409`) — the library-embedder
  path; `/settings` lives only in the TUI, whose registry rebuilds through `wire_live`.

---

## 1. Live-apply the url-safety host lists — ✅ DONE (2026-08-18)

NOTES (2026-08-18): mechanism deviation — the values the live tool set is built from travel as one named `toolSetSpec` struct (endpoint, disabled, allowHosts, denyHosts) instead of two further positional parameters on `newLiveTools`/`build`/`rebuildWith`. Adding them positionally would have put three adjacent `[]string` parameters one argument-order slip away from applying a deny list as an allow list, and made `built()` a four-value return. Behaviour, doors and call sites are otherwise exactly the `tools.disabled` precedent the item names.

**What:** A `/settings` edit of `url-safety.allow-hosts` or `url-safety.deny-hosts` currently
falls to `applySettingFor`'s `default:` (`cmd/apogee/wire_settings.go:733-734`, `cannotApply`)
and the row shows `saved — live apply failed: …`. Make the two keys apply live, following the
`tools.disabled` precedent exactly:

- Add allow/deny host-list fields to `liveTools` (`cmd/apogee/wire_live.go:60-69`) beside
  `endpoint`/`disabled`; `build` already rebuilds the registry from a copy of `w.cfg`, and
  `registryWithMCP` → `security.NewURLGuard` (`cmd/apogee/wire_tools.go:163`) consumes them.
- Add the two cases to `applySettingFor` (`cmd/apogee/wire_settings.go:552`), routing through
  the existing `rebuildWith` → `Agent.SwapTools` door (`cmd/apogee/wire_tools.go:88-120`).
  On success return the existing `toolRosterNote` ("applies to the next request",
  `cmd/apogee/wire.go:287`) — the rebuilt registry takes effect on the next request, same as a
  roster edit. Do not invent new deferral wording (ADR 0037).
- An empty value resolves to the built-in default (empty list ⇒ zero guard tightening), per the
  dispatcher's existing empty-value convention.
- Extend the `unreachable` pre-screen switch (`cmd/apogee/wire_settings.go:749-798`) so a nil
  live-tools member still answers `cannotApply` — this keeps the zero-applier test
  (`TestApplySettingRefusesEveryKeyItCannotReach`, `cmd/apogee/wire_test.go:3109`) green without
  an exemption.

**Files:** `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/wire_tools.go`,
`cmd/apogee/wire_test.go`, `CHANGELOG.md`

**Tests:** Mirror the `tools.disabled` per-key test shape in `cmd/apogee/wire_test.go`: an edit
of `url-safety.deny-hosts` rebuilds the registry and the new guard's list carries the entry; an
edit with an empty value resolves the default. Assert the returned note is `toolRosterNote`.

**Acceptance:** `go build ./... && go test ./cmd/apogee/`

**Commit:** `fix(settings): url-safety host lists apply live through the tools rebuild`

---

## 2. Startup-only keys stop reporting a live-apply failure

Depends on item 1.

**What:** `ui.inspector` (`internal/config/registry.go:346-353`) and `response-reserve`
(`registry.go:285`, same defect found while scouting) are `Editable: true` with no
`applySettingFor` case, so an edit writes the file and the row shows a failure. Both are
genuinely startup-only (`ui.inspector`: the wire observer is fixed at client construction,
`cmd/apogee/wire_boot.go:187-190`; `response-reserve`: file-only, no setter,
`cmd/apogee/wire_settings.go:72-77`). Apply the ratified call 2:

- Add empty-success cases for both keys to `applySettingFor`, exactly like the `editor` case
  (`cmd/apogee/wire_settings.go:644`): return `"", nil` — the write already happened; nothing
  applies live; the row shows no note and no failure.
- `ui.inspector`'s Description already ends "takes effect at the next start." Give
  `response-reserve`'s Description (`internal/config/registry.go:285` area) the same closing
  sentence, so the pane's Description header carries the contract the row no longer misreports.
- Extend `TestApplySettingRefusesEveryKeyItCannotReach`'s exemption list (currently `editor`
  alone, `cmd/apogee/wire_test.go:3109`) with the two keys, and add accept-tests mirroring
  `TestApplySettingAcceptsTheEditorKey` (`wire_test.go:3036`).
- Add the missing guard test that let all four keys drift in silently: every `Editable`
  registry key must either (a) be renderer-owned (the `settingsApplyLocal` set — hardcode the
  known names with a comment naming `internal/tui/settings.go:1414-1459` as the source),
  (b) be pane-intercepted (`server`), or (c) return no error from `applySettingFor` with a
  fully-populated applier. A future Editable key with no case fails this test instead of
  shipping a lying row.
- Remove the settings entry from `ISSUES.md` (lines 30-36 — it covers items 1 and 2 together).

**Files:** `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_test.go`,
`internal/config/registry.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** as listed in What (accept-tests, exemption update, the new every-Editable-key guard
test).

**Acceptance:** `go build ./... && go test ./cmd/apogee/ ./internal/config/`

**Commit:** `fix(settings): startup-only keys save silently instead of reporting a failed apply`

---

## 3. Bracketed IPv6 entries match in the url-safety lists

**What:** `normalizeHostName` (`internal/security/urlsafety.go:243-257`) keeps `[` and `]`,
while the dialled side compares bracket-free hosts (`u.Hostname()`, `urlsafety.go:167`), so a
config entry `[::1]` never matches. Fix:

- Strip one balanced bracket pair at the top of `normalizeHostName` — after the caller's trim,
  BEFORE the IDNA branch and the root-dot loop (a trailing `]` blocks the dot loop). A strip is
  a strict no-op for `NormalizeURL`'s call site (its input never carries brackets; the re-wrap
  at `urlsafety.go:214-216` keys on `:`, not prior brackets).
- Update the normalisation enumerations in the four docs that state the entry format:
  `internal/config/defaults/config.yaml:403-415` (the primary user-facing template comment),
  `internal/config/registry.go:246-248`, `internal/config/config.go:1759-1768`, and
  `internal/domain/config.go:167-181` (gains "or an IPv6 literal in brackets"). Also the doc
  comments on `NormalizeHostPattern`/`normalizeHostName` themselves.
- Test-writing constraint (binding): end-to-end cases must build lists through `NewURLGuard` —
  `hostMatches` (`urlsafety.go:287-298`) re-normalises entries independently, so a hand-built
  `URLGuard{DenyHosts: []string{"[::1]"}}` literal still fails after a correct fix.

**Files:** `internal/security/urlsafety.go`, `internal/security/urlsafety_test.go`,
`internal/config/defaults/config.yaml`, `internal/config/registry.go`,
`internal/config/config.go`, `internal/domain/config.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** a row `{"an ipv6 literal loses its brackets", "[::1]", "::1"}` in
`TestNormalizeHostPattern_MatchesTheDialledForm` (`urlsafety_test.go:172` — its cross-check
against `NormalizeURL(...).Hostname()` is exactly the violated invariant), plus a bracketed
entry in `TestNewURLGuard_BuildsATightenOnlyGuardFromConfig`'s "entries are normalised to the
dialled form" subtest (`urlsafety_test.go:229`).

**Acceptance:** `go build ./... && go test ./internal/security/`

**Commit:** `fix(security): a bracketed ipv6 url-safety entry normalises to the dialled form`

---

## 4. The Inspector names an unrecorded reply instead of implying a missing one

**What:** `inspectorRows` (`internal/tui/inspector.go:284-310`) is a flat log; a request record
with no reply renders as a bare heading plus JSON and the reader infers a missing response from
absence. A non-streaming success body is never recorded by pinned design
(`internal/provider/wire.go:149-154`; pinned at `internal/provider/client_test.go:611-615` —
do not touch the provider). Apply ratified call 3:

- In `inspectorRows`, a request record whose successor in the ring exists and is NOT a response
  record gets one note row appended after its lines (after any elision marker):
  `· no response recorded — a non-streaming reply is decoded off the connection`.
  The last record in the ring never gets the note (its call may still be in flight). Use the
  same row kind the elision marker uses (`popupRowPlain`); no new styling.
- The two zero-state rows (`inspectorDisarmedRow`, `inspectorEmptyRow`) are unaffected.
- Remove the Inspector no-reply entry from `ISSUES.md` (lines 43-46).

**Files:** `internal/tui/inspector.go`, `internal/tui/inspector_test.go`, `ISSUES.md`,
`CHANGELOG.md`

**Tests:** in `internal/tui/inspector_test.go`, beside `TestInspectorRowsHeadEveryRecord`
(`:132`): two request records with no response between them → the note row sits under the first
and not under the second (newest); a request followed by its response gets no note.

**Acceptance:** `go build ./... && go test ./internal/tui/`

**Commit:** `fix(tui): the inspector says when a reply was not recorded instead of implying loss`

---

## 5. `/inspect` gains mouse support

**What:** The inspector pane is shaped after `/usage` but has no mouse handling
(`internal/tui/mouse.go` has no inspector counterpart; `internal/tui/inspector.go:201` is
key-only). Mirror the usage pane's machinery member for member:

- `inspectorPaneRect` mirroring `usagePaneRect` (`mouse.go:1277`): closed pane answers
  `(0,0,false)`; `y0` is the usage formula's above-list PLUS `lipgloss.Height(ov.usage)` — in
  `View` the inspector block is appended after `ov.usage` (`internal/tui/model.go:2586-2604`),
  so the above-list is `{prompt, browser, picker, settings, usage}`.
- `handleInspectorClick` mirroring `handleUsageClick` (`mouse.go:1329-1338`): inside the box the
  click is claimed and swallowed (arms no drag); outside, `dismissInspector()`
  (`inspector.go:231`) and the click falls through.
- `inspectorWindow` + `inspectorWheel` mirroring `usageWindow`/`usageWheel`
  (`mouse.go:1284-1297`, `:1345-1360`), reading the offset back off the painter each notch via
  `inspectorSpec` (`inspector.go:253-274`) — never a cached offset that can drift.
- Wire both in directly after the usage pane's calls: click in `handleMouseClick`
  (`mouse.go:381-396`), wheel in the `tea.MouseWheelMsg` case (`model.go:999-1015`).

**Files:** `internal/tui/mouse.go`, `internal/tui/model.go`, `internal/tui/mouse_test.go`,
`ISSUES.md`, `CHANGELOG.md`

**Tests:** mirror `TestUsageReportUnderTheClick` (`mouse_test.go:3169`) and
`TestUsageWheelScrollsTheReport` (`:3199`) for the inspector: inside-click swallowed and arms no
selection, outside-click dismisses and still seats the caret; one row per notch, clamped both
ends. Remove the `/inspect` mouse entry from `ISSUES.md` (lines 47-49).

**Acceptance:** `go build ./... && go test ./internal/tui/`

**Commit:** `fix(tui): /inspect gains click-to-dismiss and wheel scroll like /usage`

---

## 6. A host note landing mid-run stays outside the run's stretch

**What:** `addNote` and `addEphemeralNote` (`internal/tui/transcript.go:486`, `:506`) append at
depth 0 with no placement rule, and `addFiring` (`internal/tui/schedule.go:425`) is the same
defect class. A note landing while a sub-agent run is drawing truncates `subAgentSpan`
(`internal/tui/subagentblock.go:27`), leaks entries out of a collapsed run's elision
(`render.go:325-347`), and a note parked between sibling delegations splits the fan-out group
permanently (`subAgentGroup`, `transcript.go:1330`). Apply ratified call 4 — the note lands
AFTER the run's and its group's contiguous stretch, at depth 0:

- The invariant to establish (binding): after any fold, no `entryNote` and no Firing block sits
  inside a run's stretch or between members of one fan-out group. Later entries belonging to an
  open run or its group are inserted BEFORE any host note(s) that landed after that run/group
  began — the note block slides to the tail until the run and its group close. The existing
  `place`/`runEnd` path (`transcript.go:293`, `:313`) already does this for entries carrying a
  `spawnCallID`; the gap is entries that do not (a sibling delegation head appended at the
  tail lands AFTER the parked note and the group splits). Close that gap; the mechanism (e.g.
  a trailing-note-aware append, or routing notes through `place` with a past-the-open-stretch
  rule) is the implementer's call — `openSubAgentHead` (`transcript.go:826`) and the 14fed657
  presented-document fix (`addPresented`, `transcript.go:525`) are the prior art. A mechanism
  deviation gets a dated NOTES line.
- Notes stay depth 0 and unrailed — they answer the human (contrast: presented documents carry
  the delegate's identity and go inside the run; notes have none).
- Chronology inside the note text is untouched; only transcript order moves, and only past the
  open stretch.
- The codec (`transcriptcodec.go`) replays recorded order; correct placement at fold time means
  no codec change — verify, and if a change IS needed, that is a NOTES-worthy deviation.
- Remove the depth-0 entry from `ISSUES.md` (lines 50-52).

**Files:** `internal/tui/transcript.go`, `internal/tui/schedule.go`,
`internal/tui/transcript_test.go`, `internal/tui/fanout_test.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests** (the gap the scout confirmed: no test today asserts what a note does to an open span):
- a `· cancelled` note folded while a run is open → `subAgentSpan` of the head still covers the
  whole run after later child entries land (template: `TestPresentedEntryLandsInsideItsOwnRun`,
  `transcript_test.go:1010`, and the helpers `subAgentCall`/`childCall`/`headIndex`).
- a note landing between two sibling delegations of one fan-out → `subAgentGroup` still yields
  ONE group; the note renders after it.
- a Firing block landing mid-run → same span assertion (`addFiring` path).

**Acceptance:** `go build ./... && go test ./internal/tui/`

**Commit:** `fix(tui): a host note landing mid-run lands after the run, never inside it`

---

**Suggested version bump:** patch (defect fixes; item 1's live apply is arguably a small
feature — if the owner reads it that way, minor). No version identifier is changed by this
plan; the bump is the owner's call after the run.
