# Demo storyboard rig — reusable storyboard, director's notes, beat-aware render

**Goal:** make re-recording `graphics/demo.gif` a repeatable loop: a per-clip storyboard file
holds the beats, their session anchors and the director's framing (speed, hold, zoom, cut) plus
free-text notes; a Go dev tool `cmd/demorig` lints the storyboard, locates each beat in a raw
take from the saved session's timestamps, validates the take, and renders the GIF from the
storyboard instead of hand-tuned `render.sh` arguments.

**Date:** 2026-09-16 · **Status:** unexecuted · **sized for:** ~200k-context host

**Authoritative sources:**
- `graphics/demo/README.md`, `graphics/demo/tapes/hero.tape`, `graphics/demo/render.sh`, `graphics/demo/record.sh` — the rig as it stands
- `docs/plans/archived/2026-08-24 - 00 - hero-gif-refresh-plan.md` — the storyboard table (beats 1–8) this plan makes durable
- `internal/session/transcript.go` (`Entry`, `EntryKind*`, `ToolView`), `internal/session/store.go` (`Record`, `Meta`) — the session JSON shape
- `cmd/stubllm` + `Makefile` `stubllm` target — the dev-tool precedent `cmd/demorig` mirrors
- VHS renders the viewport at device scale 1 (`vhs.go`: `MustSetViewport(width, height, 0, false)`, read 2026-09-16); `Set` has no scale option

**Ratified design calls (owner, 2026-09-16):**
- **Tooling:** Go dev tool `cmd/demorig` (`make demorig`, never a release asset), importing `internal/session` for the entry types; YAML via `gopkg.in/yaml.v3` already in `go.mod`. `setup.sh`/`record.sh`/`reset.sh`/`gen.sh`/`type.sh` stay bash.
- **Storyboard:** one YAML per clip at `graphics/demo/storyboards/<clip>.yaml` — beats, anchors, framing, expects and prose director notes in one file, read by humans and by `demorig`.
- **Alignment:** ffmpeg scene-change on the first seconds of the take finds the shell→TUI first-paint frame (used only by the `first-paint` anchor / head trim); the first `user` entry's `at` is pinned to the tape's fixed video time of the prompt Enter (`align.first_prompt_at`), and every later `at` maps through that offset plus a per-storyboard `paint_lag`. Session `createdAt` is first-save time, not launch (`cmd/apogee/wire_session.go`), so it is not the pin. Amended at the regression check 2026-09-16 (superseding "first-paint + declared lead").
- **Zoom source:** guaranteed ≥2× — `hero.tape` records at doubled geometry (Width/Height/FontSize/Padding), `demorig render` downscales to the storyboard's shipped width; VHS is 1× natively so the doubling is unconditional.
- **`render.sh` is retired** once `demorig render` lands (item 5); `README.md` quick start uses the tool.

**Regression check (2026-09-16, 3f365d6d):**
- 1: recast — the anchor grammar gains `{video: …}` / `{beat: …}` forms and an `offset`; beat 7 anchors `{video: end, offset: -6.5s}` (the `/undo` preview note is never persisted), beat 4 `{beat: 2, offset: 10.3s}`; yields to CHANGELOG.md:1199 (the four `# Generated rhythm:` markers stay in the tape).
- 2: recast — the type list gains `Frame` and `Align`; validation covers the video/beat anchor rules.
- 3: recast — resolution order session → video → beat, `offset` last; the fixture mirrors a real take (no `/undo` note, `interjected` at delivery).
- 4: recast — `stage: dirty` reports `SKIP` without `--stage`; beat 7 carries no expect.
- 5: recast — zoom is `scale…eval=frame` + a fixed-size `crop` with `t`-only x/y; `record.sh` and `hero.tape` lose every `render.sh` mention.
- Second round (2026-09-16, 3f365d6d), on regression-3 / regression-4:
- 1: recast — beat 4 expect `before: 6` (delivery lands AFTER the fix card); `align: {scene_threshold, first_prompt_at: 9.96s}` replaces `lead_to_first_prompt` (`paint_lag` is commit→paint only); `Output hero.gif` leaves the tape; `tape:`/`ship:` relative to the storyboard's directory; README Layout row + knob sentences re-pointed + new-clip sentence; still yields to CHANGELOG.md:1199.
- 2: guard folded — `Align` is `{scene_threshold, first_prompt_at}`, the loader resolves `tape:`/`ship:` against the storyboard file's directory, `.gitignore` gains `/demorig` + `/demorig.exe`, `Nth` is a custom scalar (`UnmarshalYAML`) so `nth: last` reaches the validator.
- 3: recast — pin is `t(at) = align.first_prompt_at + (at − firstUser.At) + paint_lag` (the header's *Alignment* call's "first-paint + lead" wording is superseded by this round; scene detection serves only the `{video: first-paint}` anchor); fixture has no toolResult entries and places `interjected` right after the `Replace task.go` toolCall.
- 4: recast — `record.sh` runs `check` only when `$HERE/storyboards/$TAPE.yaml` exists (one-line notice + exit 0 otherwise); tests gain the `before: 6` ordering case.
- 5: guard folded — the What's zoom clause now states the guard's `scale…eval=frame` + fixed-size `crop` shape (probed on ffmpeg 7.0.2).

**Standing requirements:** skills: `coding-standards`. No VERSION bump. Recording a take needs macOS + `vhs` and is owner-run; every item here is verifiable without one (fixtures + `--dry-run`).

**Out of scope:** new clips (`/sessions`, `/model`, sub-agents, MCP); changes to apogee itself (`cmd/apogee`, `internal/`); the stage repo's planted bug; captions/title cards; re-recording the hero clip (a follow-up owner run once this plan is archived).

---

## 1. `storyboards/hero.yaml` — the durable storyboard, beat headers in the tape, 2× geometry — ✅ DONE (2026-09-16)

NOTES (2026-09-16): hero.tape's header still says "trimmed and compressed afterwards by render.sh" — true until item 5 retires render.sh, whose regression guard owns removing every render.sh mention from the tape and record.sh; render.sh's existing `scale=1250` keeps the Quick start correct against the 2× take meanwhile.
NOTES (2026-09-16): the README's Storyboards section names `demorig` and its `lint`/`beats`/`check`/`render` verbs before items 2–5 land them, as the item's mandated "add `storyboards/<name>.yaml` to get `check`/`render`" sentence already does; the Quick start keeps `render.sh` until item 5 re-points it.
NOTES (2026-09-16): beat 3 anchors `{kind: toolCall, tool: Tests}` with expect `contains: FAIL` and beat 6 `{kind: toolCall, tool: Tests, nth: last}` with `[{contains: PASS}, {after: 5}]` — run_tests' label is "Tests" and testVerdictStat words its Stat as the bare PASS/FAIL (internal/tui/toolregistry.go); beat 2 anchors `{kind: user}`. These are the author's first cut the plan leaves to the author; the binding beats 1/4/5/7/8 are as the plan states.

**What:** Recast at the regression check (2026-09-16). Create `graphics/demo/storyboards/hero.yaml` holding the eight hero beats from the archived 2026-08-24 plan's storyboard table, and move the *why* / director's notes from `hero.tape`'s KNOB comments into it (the tape keeps only VHS mechanics). Binding schema (item 2 parses exactly this):

```yaml
clip: hero
tape: ../tapes/hero.tape         # relative to this storyboard file's own directory
ship: ../../demo.gif             # likewise
frame: {width: 1250, scale: 2, fps: 24, max_colors: 192}
# first_prompt_at is the VIDEO time of the first prompt's Enter, fixed by the tape's head: VHS records
# from `Show`; launch Enter at 1.775s (Sleep 500ms + 775ms typing + Sleep 500ms), + Sleep 4s + 3.382s
# prompt typing (type.sh GOLDEN_TOTALS_MS) + Sleep 800ms. Re-derive when the head or the typing profile changes.
align: {scene_threshold: 0.4, first_prompt_at: 9.96s}
paint_lag: 300ms                 # commit→paint only
regions: {edit-card: [0, 0.45, 1, 0.55]}                      # fractional x,y,w,h of the frame
beats:
  - id: 5
    title: the fix lands — split diff on camera
    why: biggest visual upgrade since the last clip
    tape: beat 5                                               # `# beat 5` section header in the tape
    anchor: {kind: toolCall, tool: Replace, target: task.go}   # first match unless nth: N | last
    frame: {hold: 4s, zoom: {region: edit-card, factor: 1.5, in: 400ms, hold: 2500ms, out: 400ms}, speed: 1.5}
    expect: [{contains: "+1 −1"}]                              # U+2212, as the TUI spells it
    notes: |
      Keep the card open and on screen; the PageDowns re-attach the viewport (layout.md).
```
Anchor grammar: `{kind, text, tool, target, nth}` — `kind` is an `EntryKind*` string, `text`/`tool`/`target` are prefix matches on `Entry.Text` / `ToolView.Label` / `ToolView.Target`, `nth` is an integer or `last`; the two video anchors are `first-paint` (beat 1) and `end` (beat 8). Framing verbs: `speed` (segment multiplier, default 1), `hold` (leading seconds at 1×), `zoom` (as above), `cut: true` (drop the segment). Expect verbs: `contains` (on the anchored entry's Text, Label or Stat), `before: <id>`, `after: <id>`; a top-level `expect: {stage: dirty}` asserts the stage repo has uncommitted changes after the take. The non-session anchors, binding: beat 1 `{video: first-paint}`; beat 4 `{beat: 2, offset: 10.3s}` (the interjection's Enter: Sleep 8s + its generated typing total from type.sh's `GOLDEN_TOTALS_MS` + Sleep 500ms; re-derive when the tape or the typing profile changes) with expect `[{entry: {kind: interjected}, before: 6}]` (delivery happens at the Step boundary AFTER the fix card paints, so never `before: 5`); beat 7 `{video: end, offset: -6.5s}`, no expect; beat 8 `{video: end}`. Framing values for beats 1–8 are the author's first cut and are tuned later; the hold/zoom on beat 5 and `hold: 3s` on beat 7 are binding.

Insert `# beat N — <title>` header comments into `graphics/demo/tapes/hero.tape` at the eight beat boundaries, and double its geometry: `Set Width 2500`, `Set Height 1360`, `Set FontSize 30`, `Set Padding 32` (the `Wait+Screen` pattern and every key sequence are unchanged); remove `Output hero.gif` from `hero.tape` in the same edit (the GIF is `demorig render`'s job; `Output hero.mp4` stays). In `graphics/demo/README.md`: add a **Storyboards** section documenting the schema above, the anchor/framing/expect vocabulary, the 2× rule, and that a re-record starts by editing the storyboard, not the tape; add a `storyboards/<clip>.yaml` row to the Layout table; re-point every sentence that locates knob reasoning in the tape (rule: `grep -n 'knob' graphics/demo/README.md`) at the storyboard's `why`/`notes`; and give the "Recording a new clip" section the sentence "add `storyboards/<name>.yaml` to get `check`/`render`".

**Regression guard.** Anchor grammar gains two non-session forms and an `offset`: `{video: first-paint|end, offset: <duration>}` and `{beat: <id>, offset: <duration>}` (relative to that beat's resolved time); `offset` is also allowed on session anchors. Beat 7 anchors `{video: end, offset: -6.5s}` (the tape's fixed tail: /undo Enter → 4s → Escape → 2s; the /undo preview note is committed live and NEVER persisted — internal/tui/undo.go noteRevert — so no session anchor exists for it) and carries no session expect; the top-level `expect: {stage: dirty}` is the "nothing reverted" proof. Beat 4 anchors `{beat: 2, offset: 10.3s}` (the queued row appears at the interjection's Enter, 8s + 1.8s typing + 0.5s after the prompt Enter, per the tape) because the `interjected` entry is stamped at DELIVERY (transcript.go addInterjected), not at the keypress; its expect is `[{entry: {kind: interjected}, before: 6}]` — delivery happens at the Step boundary AFTER the fix's Replace card paints (hero.tape knob 3, worker.go stepToBoundary → deliverInterjections), so `before: 5` fails every keeper take and is never written. An expect may carry its own `entry:` selector (same grammar as a session anchor); it defaults to the beat's anchor entry and is required when the beat's anchor is a video/beat anchor. Beat 1 = `{video: first-paint}`, beat 8 = `{video: end}`. Second round: `align:` is `{scene_threshold: 0.4, first_prompt_at: 9.96s}` — `first_prompt_at` is the VIDEO time of the first prompt's Enter, fixed by the tape's head (VHS records from `Show`; launch Enter at 1.775s = Sleep 500ms + 775ms typing + Sleep 500ms; + Sleep 4s + 3.382s prompt typing from type.sh `GOLDEN_TOTALS_MS` + Sleep 800ms), the storyboard comment says so and says to re-derive when the head or profile changes; `lead_to_first_prompt` and "which `paint_lag` absorbs" are struck — `paint_lag` covers commit→paint only. `Output hero.gif` is removed from hero.tape in the same edit (`Output hero.mp4` stays). `tape:`/`ship:` are relative to the storyboard file's own directory (`../tapes/hero.tape`, `../../demo.gif`). README: the Layout table gains a `storyboards/<clip>.yaml` row, every sentence locating knob reasoning in the tape (`grep -n 'knob' graphics/demo/README.md`) is re-pointed at the storyboard's `why`/`notes`, and "Recording a new clip" gains "add `storyboards/<name>.yaml` to get `check`/`render`". The item yields to CHANGELOG.md:1199 (hero.tape "says which of its lines are generated"): the four `# Generated rhythm:` marker comments (hero.tape:24,31,48,142) and the file header stay in the tape as VHS mechanics.

**Files:** `graphics/demo/storyboards/hero.yaml`, `graphics/demo/tapes/hero.tape`, `graphics/demo/README.md`
**Read first:** graphics/demo/tapes/hero.tape — KNOB 1/2/3 comments, `Output hero.gif`, Wait+Screen line, Set Width/Height/FontSize/Padding; graphics/demo/type.sh — HERO_STRINGS, GOLDEN_TOTALS_MS; graphics/demo/gen.sh — blockNumberFor guard (exact `Type "…"` line match, comments pass through);
graphics/demo/README.md — Quick start, Layout table, "The knobs are marked", "Pace is decided in post" (from 3.8s), "Humanized typing"; internal/tui/worker.go — stepToBoundary, deliverInterjections (delivery between Turns, after the Turn's toolCall entries);
internal/tui/undo.go — noteRevert, undoPreviewNote, revertNote; internal/session/transcript.go — EntryKindToolCall, EntryKindInterjected, EntryKindUser, ToolView.Label/Target/Stat; internal/tui/toolregistry.go — single_find_and_replace label "Replace", stringArg("path")

**Tests:** none (data + docs); item 2's loader test parses this file.

**Acceptance:**
- `grep -c '^# beat [1-8]' graphics/demo/tapes/hero.tape` → `8`
- `grep -cE '^\s*- id: [1-8]$' graphics/demo/storyboards/hero.yaml` → `8`
- `grep -E '^Set (Width 2500|Height 1360|FontSize 30|Padding 32)$' graphics/demo/tapes/hero.tape | wc -l` → `4`
- `! grep -q '^Output hero.gif' graphics/demo/tapes/hero.tape && grep -q '^Output hero.mp4' graphics/demo/tapes/hero.tape`
- `grep -n '## Storyboards' graphics/demo/README.md && grep -n 'storyboards/<clip>.yaml' graphics/demo/README.md`

**Commit:** `docs(demo): hero storyboard with director's notes, beat headers in the tape, 2× geometry`

## 2. `cmd/demorig` — storyboard loader and `lint` — ✅ DONE (2026-09-16)

NOTES (2026-09-16): bookend rule read as: the FIRST beat anchors bare `{video: first-paint}` and the LAST bare `{video: end}`, each unique; a video anchor carrying an `offset` (hero beat 7, `{video: end, offset: -6.5s}`) is an ordinary anchor, not a bookend — the only reading under which the item-1 hero storyboard lints clean.
NOTES (2026-09-16): validation goes a little past the item's list where the schema makes a value meaningless: `frame.width`/`fps` ≥1, `max_colors` 2..256, `align.scene_threshold` in (0,1], `first_prompt_at` >0, durations ≥0, regions exactly four fractions in [0,1], `zoom.factor` ≥1, `expect.stage` only `dirty`, an expect asserting at least one of contains/before/after; an unknown session `kind` is rejected against `internal/session`'s EntryKind* constants.
NOTES (2026-09-16): `Framing.Speed` is `*float64` (nil = default 1×, `Rate()` reads it) so an explicit `speed: 0` is a named error instead of a silent default; `TakeExpect` is the top-level `expect: {stage: dirty}` type, beside the per-beat `Expect`.
NOTES (2026-09-16): consequential edit — docs/manual/building.md: made necessary by the new `demorig` Makefile target (the manual's Make-targets table enumerates every target).
NOTES (2026-09-16): `storyboard.go` is ~510 lines (the coding-standards ~400 soft limit) because the item names it as the one module owning schema + validation; a later item may split validation into its own file if it grows further.

**What:** Recast at the regression check (2026-09-16). New dev tool `cmd/demorig` (package `main`, subcommand dispatch like `cmd/stubllm`). This item ships the storyboard types (`Storyboard`, `Beat`, `Anchor`, `Framing`, `Expect`) with a `Load(path) (*Storyboard, error)` that decodes the item-1 schema with `yaml.v3` strict (`KnownFields(true)`), parses durations, and validates: ids unique and ascending, exactly one `first-paint` and one `end` anchor (first and last), every `tape:` header present in the tape file (`# beat N`), every `zoom.region` declared, `nth` ≥1 or `last`, `speed` >0, `frame.scale` ≥1. `demorig lint <storyboard>` prints the errors and exits 1. One deep module: `storyboard.go` owns the schema and validation; `main.go` only dispatches. Add `Makefile` target `demorig` mirroring `stubllm` (dev tool, never a release asset).

**Regression guard.** The type list is `Storyboard`, `Frame` (top-level width/scale/fps/max_colors), `Align`, `Beat`, `Anchor`, `Framing` (per-beat speed/hold/zoom/cut), `Zoom`, `Expect`; validation additionally requires: exactly one `first-paint` and one `end` video anchor (first and last beat), `beat:` references resolve to an earlier id, an expect on a non-session anchor names its `entry:`. Second round: `Align` is `{scene_threshold, first_prompt_at}`; the loader resolves `tape:`/`ship:` against the storyboard file's directory (`filepath.Dir(path)`), so `go test ./cmd/demorig/` (cwd `cmd/demorig`) and `go run ./cmd/demorig lint …` (cwd repo root) both find the tape. `Nth` is a custom scalar type with `UnmarshalYAML` accepting a positive integer or the string `last` — a plain `int` under `yaml.v3` strict decode rejects `nth: last` before validation runs. Files gain `.gitignore`: `/demorig` and `/demorig.exe` are added beside `/stubllm` and `/stubllm.exe` (extend the comment at .gitignore:5) so `make demorig` leaves no untracked binary.

**Files:** `cmd/demorig/main.go`, `cmd/demorig/storyboard.go`, `cmd/demorig/storyboard_test.go`, `cmd/demorig/testdata/`, `Makefile`, `.gitignore`
**Read first:** cmd/stubllm/main.go — newRootCommand, runE, exitCodeFor, must; Makefile — stubllm target, check (gofmt -l ., go build ./..., golangci-lint, sharded go test); .gitignore — /stubllm, /stubllm.exe; go.mod — gopkg.in/yaml.v3 (direct require);
internal/session/transcript.go — EntryKind*, Entry.At, ToolView; graphics/demo/tapes/hero.tape — `# beat N` headers (item 1); scripts/test-shards.sh — `go list ./...` sweep picks the new package up

**Tests:** `TestLoadHeroStoryboard` opens `../../graphics/demo/storyboards/hero.yaml` (`go test` runs with cwd `cmd/demorig`) and asserts eight beats, the beat-5 zoom, the resolved tape path ending `graphics/demo/tapes/hero.tape`, `align.first_prompt_at` = 9.96s, and the item-1 anchor forms (beat 1 `{video: first-paint}`, beat 8 `{video: end}`, beat 4 `{beat: 2, offset: 10.3s}` with expect `before: 6`, beat 7 `{video: end, offset: -6.5s}` with no expect); `nth: last` and `nth: 2` decode into `Nth`, `nth: 0` and `nth: foo` are named errors; table test over `testdata/bad-*.yaml` (each fixture's `tape:` is relative to its own file, pointing at a tiny tape under `testdata/`: unknown field, missing tape header, undeclared region, duplicate id, missing `first-paint`/`end` anchor, `beat:` naming a later id, expect on a video anchor without `entry:`) asserting each named error.

**Acceptance:** `go build ./cmd/demorig && go test ./cmd/demorig/ && go run ./cmd/demorig lint graphics/demo/storyboards/hero.yaml && grep -q '^/demorig$' .gitignore`

**Commit:** `feat(demorig): storyboard loader and lint for the demo rig`

Depends on item 1.

## 3. `demorig beats` — beat times from the session JSON — ✅ DONE (2026-09-16)

NOTES (2026-09-16): added `cmd/demorig/beats_test.go` (not in the item's file list) covering the table and JSON writers; `main.go` gained the `beats` wiring and a doc line — a consequential edit of adding the subcommand.
NOTES (2026-09-16): `FirstPainter` binds its take and threshold at construction (`FirstPaint(ctx)`), so `resolveBeats` calls it lazily and only for a `{video: first-paint}` anchor — tested; the ffmpeg/ffprobe adapter test synthesises a black→white clip via lavfi and is skipped here (neither tool on PATH), so it is unverified against a live ffmpeg.
NOTES (2026-09-16): the fixture is a golden: `TestHeroFixtureIsCurrent` compares it byte-for-byte to `heroEntries()` and `DEMORIG_UPDATE_FIXTURES=1 go test ./cmd/demorig/ -run TestHeroFixtureIsCurrent` rewrites it.

**What:** Recast at the regression check (2026-09-16). `demorig beats <storyboard> <take.mp4> <session.json> [--json]` prints one row per beat: id, title, seconds into the take. Three parts, each its own file: `session.go` decodes a `session.Record` and its `Transcript` raw message into `[]session.Entry` (import `internal/session`; never redeclare the shapes); `anchors.go` resolves each `Anchor` over the entries in list order (prefix matches, `nth`/`last`), returning the entry and its `At`; `align.go` maps `At` to video seconds: `t(at) = align.first_prompt_at + (at − firstUser.At) + paint_lag`, where `firstUser` is the first `EntryKindUser` entry; `firstPaint` (scene detection) comes from a `FirstPainter` interface whose ffmpeg adapter runs `ffmpeg -t 12 -i <take> -vf "select='gt(scene,<threshold>)',showinfo" -f null -` and takes the first `pts_time:` on stderr, and it is used ONLY by the `{video: first-paint}` anchor, never in the pin. `first-paint` resolves to `firstPaint`, `end` to the take's duration (`ffprobe`). An unresolved anchor is an error naming the beat.

**Regression guard.** Resolution order: session anchors first, then video anchors (`first-paint` = firstPaint, `end` = duration), then `beat:` anchors in id order against already-resolved beats; `offset` added last. The pin is `t(at) = align.first_prompt_at + (at − firstUser.At) + paint_lag`; `first-paint` (scene detection) is used only by the `{video: first-paint}` anchor, never in the pin. The committed fixture `testdata/session-hero.json` must mirror a REAL take: it carries NO `/undo` note entry and NO toolResult entries (a paired result enriches its toolCall entry — internal/tui/transcript.go enrichWithResult — so Stat `+1 −1` rides the `Replace task.go` toolCall entry), and the `interjected` entry sits immediately AFTER the `Replace task.go` toolCall and before the CHANGELOG toolCall — the delivery point hero.tape's knob 3 documents (delivery order of a keeper take). The anchors test asserts beat 4's expect `before: 6` passes while `before: 5` would fail (the `interjected` entry's index lies between beat 5's and beat 6's entries).

**Files:** `cmd/demorig/session.go`, `cmd/demorig/anchors.go`, `cmd/demorig/anchors_test.go`, `cmd/demorig/align.go`, `cmd/demorig/align_test.go`, `cmd/demorig/beats.go`, `cmd/demorig/testdata/session-hero.json`
**Read first:** internal/session/transcript.go — Entry, ToolView, EntryKindUser, EntryKindInterjected, DecodeTranscript, EncodeTranscript; internal/session/store.go — Record, Meta, Store.LoadPath, validateID (a fixture loaded through LoadPath needs a non-empty Meta.ID);
internal/tui/transcript.go — stamp, commit, addInterjected (At = delivery), addToolResult's enrichWithResult branch (no toolResult entry on a paired result); internal/agent/interject.go — Interject (commits only at the Step boundary);
internal/tui/toolview.go — diffCounts (U+2212); internal/tui/interject.go — foldInterjected; graphics/demo/tapes/hero.tape — knob 3 "WHAT IT WAITS ON" (delivery lands after the Replace card)

**Tests:** `testdata/session-hero.json` is built once by a test helper from `session.Entry` values through the package's own encoder (`EncodeTranscript`) and a `session.Record` with a non-empty `Meta.ID`, then committed, with no `/undo` note entry, no toolResult entries, and the `interjected` entry immediately after the `Replace task.go` toolCall — the eight hero anchors resolve against it in order given a fake `FirstPainter` and duration (beat 1 = firstPaint, beat 8 = duration, beat 7 = duration − 6.5s, beat 4 = beat 2 + 10.3s; beats 2–6 are independent of firstPaint); the resolved `interjected` entry's index is greater than beat 5's and less than beat 6's (so beat 4's `before: 6` passes and `before: 5` would fail); `nth`/`last`/no-match cases; `offset` on a session anchor; a `beat:` anchor naming an unresolved beat errors; alignment arithmetic `t(at) = first_prompt_at + (at − firstUser.At) + paint_lag` against fixed numbers; the ffmpeg adapter test is skipped unless `ffmpeg` is on `PATH`.

**Acceptance:** `go test ./cmd/demorig/ && go vet ./cmd/demorig/`

**Commit:** `feat(demorig): beats — locate storyboard beats in a take from the session timestamps`

Depends on item 2.

## 4. `demorig check` — take validation, wired into `record.sh`

**What:** Recast at the regression check (2026-09-16). `demorig check <storyboard> <session.json> [--stage <dir>]` resolves every anchor (item 3's resolver; no video needed) and evaluates the expects: `contains` on the anchored entry's `Text`, `Tool.Label` or `Tool.Stat`; `before`/`after` on entry list order; top-level `stage: dirty` runs `git -C <stage> status --porcelain` and requires output. Prints a `beat | PASS/FAIL | detail` table, exits 1 on any FAIL. `record.sh` runs it after `vhs` returns, against the newest `$WORK/home/.apogee/sessions/*.json` and `--stage $WORK/home/Repos/taskman`, via `go run ./cmd/demorig` from the repo root (`$HERE/../..`), and exits with its status so a retake loop can key off it; the storyboard path is `$HERE/storyboards/$TAPE.yaml`. Document the loop in `README.md` (`Quick start` and a **Checking a take** paragraph replacing the python one-liner as the first resort — keep the one-liner as the deep-dive).

**Regression guard.** With no `--stage` the `stage: dirty` expect is reported `SKIP` and does not affect the exit status; `record.sh` always passes `--stage`. The item's test mutates the fixture's `interjected` entry to `user` and asserts beat 4's expect FAILs naming the kind; beat 7 has no expect and resolves without the session. Second round: `record.sh` runs `demorig check` only when `$HERE/storyboards/$TAPE.yaml` exists; otherwise it prints one line `no storyboard for <tape> — take not checked` and exits 0 after the "raw take:" lines, so the documented new-clip path (`tapes/<name>.tape` then `./record.sh <name>`) keeps working (the README sentence "add `storyboards/<name>.yaml` to get `check`/`render`" lands in item 1). Tests: the mutated-fixture case stays; add the ordering case — beat 4's `before: 6` PASSes on the fixture.

**Files:** `cmd/demorig/check.go`, `cmd/demorig/check_test.go`, `graphics/demo/record.sh`, `graphics/demo/README.md`
**Read first:** graphics/demo/record.sh — WORK, HERE, `cd "$WORK"` before `vhs` (`go run -C "$HERE/../.."` or cd back), closing echo; graphics/demo/reset.sh — `rm -rf $DEMO_HOME/.apogee/sessions` (exactly one `<id>.json` after a take), `git checkout -- .` + `git clean -qfd` (stage clean before the take);
graphics/demo/setup.sh — DEMO_HOME, STAGE (`$WORK/home/Repos/taskman`), stage/gitignore (caches ignored, so `status --porcelain` is empty on a clean stage); cmd/apogee/wire.go — sessions root `<home>/sessions`; internal/tui/undo.go — noteRevert (no save Cmd);
internal/tui/sessionsave.go — persist, saveAtIdle, scheduleSave; graphics/demo/README.md — "Expect 3–5 takes" paragraph (python one-liner), "Recording a new clip for a different feature"

**Tests:** expects over `testdata/session-hero.json` all PASS, including beat 4's ordering expect `before: 6` (and a copy of the storyboard's expect rewritten to `before: 5` FAILs on the same fixture); a mutated copy where the `interjected` entry is `user` FAILs beat 4 with a message naming the kind; beat 7 (no expect) passes without any session entry; `stage: dirty` against a temp git repo clean vs dirty, and `SKIP` with exit 0 when no `--stage` is given; `bash -n graphics/demo/record.sh` plus a shell read of the guard: the `check` call is inside an `if [ -f "$HERE/storyboards/$TAPE.yaml" ]` branch whose else prints `no storyboard for <tape> — take not checked`.

**Acceptance:** `go test ./cmd/demorig/ && bash -n graphics/demo/record.sh && grep -q 'no storyboard for' graphics/demo/record.sh && go run ./cmd/demorig check graphics/demo/storyboards/hero.yaml cmd/demorig/testdata/session-hero.json`

**Commit:** `feat(demorig): check — validate a take against the storyboard's expects`

Depends on item 3.

## 5. `demorig render` — storyboard-driven GIF; retire `render.sh`

**What:** Recast at the regression check (2026-09-16). `demorig render <storyboard> <take.mp4> <session.json> [-o out.gif] [--dry-run]`. `filtergraph.go` is a pure builder `Build(segments []Segment, frame Frame) (string, error)`: beats sorted by resolved time form segments `[t_i, t_{i+1})` (head before beat 1 dropped, last segment to `end`); `speed` → `trim=start=..:end=..,setpts=(PTS-STARTPTS)/s`; `hold` splits off the leading seconds at 1×; `zoom` is `scale=w='iw*f(t)':h='ih*f(t)':eval=frame` followed by a fixed-size `crop=<src_w>:<src_h>:x='cx*(f(t)-1)':y='cy*(f(t)-1)'` where f(t) ramps 1→factor→1 over `in`/`hold`/`out` and cx,cy are the region's centre in source pixels (no scale-back; crop's out_w/out_h are configure-time only) — never `zoompan`; `cut: true` omits the segment; segments `concat`, then today's tail verbatim: `fps=<fps>,scale=<width>:-1:flags=lanczos,split;palettegen=max_colors=<max_colors>;paletteuse=dither=bayer:bayer_scale=3`. `render.go` ffprobes the take, warns when source width < `frame.scale × frame.width` and a zoom is requested, runs ffmpeg, then `gifsicle -O3 --lossy=80` if present, and prints path/size/duration as `render.sh` does. `--dry-run` prints the ffmpeg argv and exits 0. `git rm graphics/demo/render.sh`; `README.md` quick start and layout table name `demorig render`; the rule "pace is decided at render, never by re-taking" stays.

**Regression guard.** Zoom is built as `scale=w='iw*f(t)':h='ih*f(t)':eval=frame` followed by a FIXED-size `crop=<src_w>:<src_h>:x='cx*(f(t)-1)':y='cy*(f(t)-1)'` where f(t) ramps 1→factor→1 over in/hold/out and cx,cy are the region centre in source pixels — crop's out_w/out_h are evaluated once at configure time (ffmpeg crop docs; vf_crop.c config_input) so a crop-size ramp cannot work; x/y are written in t only. The golden pins that shape. Second round: the What's zoom clause now states this same shape (it had still described a crop-size ramp); the shape was probed on ffmpeg 7.0.2 (scratch, testsrc) and runs, zooming about the region centre. Files gain `graphics/demo/record.sh` and `graphics/demo/tapes/hero.tape`: record.sh's closing echo names the `demorig render` line instead of render.sh, and every comment naming render.sh is reworded; acceptance becomes the rule `! grep -rn 'render\.sh' graphics/demo --exclude-dir=history` (history/*/NOTES.md and CHANGELOG stay historical).

**Files:** `cmd/demorig/render.go`, `cmd/demorig/filtergraph.go`, `cmd/demorig/filtergraph_test.go`, `cmd/demorig/testdata/filtergraph-hero.txt`, `graphics/demo/render.sh`, `graphics/demo/record.sh`, `graphics/demo/tapes/hero.tape`, `graphics/demo/README.md`
**Read first:** graphics/demo/render.sh — the filter_complex tail, gifsicle `-O3 --lossy=80` pass, closing printf (path/size/duration); graphics/demo/record.sh — closing echo naming render.sh; graphics/demo/tapes/hero.tape — header comment (line 3) and KNOB 2 comment naming render.sh;
graphics/demo/README.md — Quick start, Layout table, "Pace is decided in post", "`render.sh` cannot cut from the middle" (the `[0:v]` twice + concat precedent), knobs paragraph, Humanized typing, History;
graphics/demo/history/2026-08-24-hero/NOTES.md — render row (historical, keep); docs/plans/archived/2026-08-24 - 00 - hero-gif-refresh-plan.md — storyboard table (beat 6 = CHANGELOG + green PASS)

**Tests:** golden `filtergraph-hero.txt` from the hero storyboard over fixed beat times (speed, hold, zoom, cut and the palette tail all exercised; the zoom pins the `scale…eval=frame` + fixed-size `crop` shape with `t`-only x/y); `Build` rejects overlapping/unsorted segments; `--dry-run` end-to-end with a fake prober.

**Acceptance:** `go test ./cmd/demorig/ && test ! -e graphics/demo/render.sh && ! grep -rn 'render\.sh' graphics/demo --exclude-dir=history`

**Commit:** `feat(demorig): render — storyboard-driven GIF with per-beat speed, hold, zoom and cut; retire render.sh`

Depends on item 3.
