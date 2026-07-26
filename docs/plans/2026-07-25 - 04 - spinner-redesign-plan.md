# Plan — Redesign the status-line spinner: snake / glitter styles with a soft colour loop

**Date:** 2026-07-25
**Status:** READY (the three shape decisions resolved with the owner 2026-07-25 — see *Decisions taken*).
**Source:** owner request, 2026-07-25 — three spinner ideas (density-sorted braille "glitter"
that breathes; a 6-dot snake around a 4×4 dot grid built from two braille cells; a soft ~8s
colour loop), plus the follow-up decisions below.
**Track:** post-`v0.8.0` TUI presentation. Independent of
`docs/plans/2026-07-25 - 03 - architecture-review-closeout-plan.md`: nothing here touches
`internal/tui/activity.go` or the Event folds that plan consolidates.
**Public API:** **none.** Everything lands in `internal/tui` and `cmd/apogee`; no exported
name on the `apogee` root facade changes (ADR 0010). The `config.yaml` schema gains one
**additive**, fully-optional `ui:` block — absent, every run resolves exactly as it does today.
**Standing requirement:** `/coding-standards` (Go + testing variants) mandatory — invoke
`implement-plan` with `coding-standards` forwarded. Pre-production: commit direct to `main`,
no PRs (owner directive).

Per-item green gate:

```
gofmt -l .                                              # empty
make check                                              # vet + lint + go test -race -count=1 ./...
GOOS=windows go build ./... && GOOS=darwin go build ./...
```

**Dependencies.** Item 1 is the foundation — 2, 3 and 4 each require it and are otherwise
independent of one another. Item 5 requires 2, 3 and 4 (it selects between them). Item 6
requires 5. Item 7 is **optional** and requires 2, 3 and 4. `/implement-plan` may stop after
any completed item and the tree is coherent (items 2–4 land their styles unreachable from
config until 5; that is intentional and not a defect).

**Deviations leave a trail.** Any authorized deviation from an item's text must land as a
dated `NOTES:` line under that item's heading in this file, per the sub-agent templates.

**Authoritative sources**, in precedence order, for every item:
1. This document.
2. `docs/adr/0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md` — the Model is a
   value type with value-receiver `Init`/`Update`/`View`; the whole Model is copied on every
   `Update`. This is what forbids an RNG handle or any self-referential no-copy type in the
   animation state.
3. `internal/tui/doc.go` L148-157 — the value-copy invariant, restated with its guard test.
4. `layout.md` — the rendered shape of the status line.
5. `cmd/apogee/config.go` — the `present:` block (`presentConfig` → `presentSettings` →
   `layer` → `settings` → `wire.go`) is the precedent the `ui:` block copies exactly.

---

## The problem (grounded, verified 2026-07-25 against the working tree)

The status-line spinner is a fixed 8-frame braille rotation:

```go
// internal/tui/theme.go:87-97
var brailleFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

func newBrailleSpinner() spinner.Model {
	return spinner.New(spinner.WithSpinner(spinner.Spinner{
		Frames: brailleFrames,
		FPS:    time.Second / 10,
	}))
}
```

It is a `charm.land/bubbles/v2/spinner` widget held on the Model (`internal/tui/model.go:78`),
styled once at construction with a background and **no foreground** (`model.go:138-139`), so
the glyph inherits the terminal's default colour:

```go
sp := newBrailleSpinner()
sp.Style = lipgloss.NewStyle().Background(colBlack) // match the status bar's black field
```

and rendered in `statusLine`'s running branch (`model.go:1536`):

```go
left += m.spinner.View() + m.th.statusBar.Render(" "+m.runningPhrase(time.Now())) + m.throughputSuffix()
```

giving `  ⣻ reading · main.go · 3s` (sketched at `layout.md:44`).

**Why the widget has to go.** `spinner.Model.View()` renders `frames[i]` cyclically through
one fixed `Style`. It has no room for a glyph chosen per frame (glitter) or a colour computed
per frame (the loop). Both requested ideas need the frame index to be an *input to a function*,
not an index into a literal. The animation therefore moves into an owned, pure module.

**What the widget does give us for free, and must be replaced.** Its `TickMsg` carries an id
and a tag so a re-arm cannot leave two chains running (`spinner.go` in bubbles v2.1.0
L174-183). The TUI re-arms at six sites — `model.go:583` (approval decision), `624` (`submit`),
`735`/`749`/`780` (`runCommand`: `/continue`, the canned turn, `/compact`), `816`
(`submitAnswer`) — and lets the chain die when idle (`model.go:369-378`):

```go
case spinner.TickMsg:
	// Keep the chain alive only while running; dropping the tick when idle lets it
	// die naturally (the spinner's tag mechanism prevents a doubled chain on restart).
	if m.state == stateRunning {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
```

An owned tick chain must carry an equivalent guard or the spinner silently runs at 2× after an
approval prompt. Item 1 reproduces it as a generation counter.

**No test pins the current glyphs.** `git grep` for `brailleFrames` / `newBrailleSpinner` /
`⣾` hits only `theme.go` and a doc comment at `model.go:1525`. The tests that constrain this
territory are geometric, not cosmetic: `TestStatusLineAlignsWithTranscriptText`
(`model_test.go:1697`, the spinner opens in the transcript's text column),
`TestStatusLineIndentFitsNarrowWindow` (`:1716`, the line clips rather than wraps), and the two
`cmd != nil` "spinner tick not re-armed" assertions (`:702`, `:922`).

**Decisions taken with the owner, 2026-07-25.**
- Both glyph animations ship; the spinner style is a **`config.yaml` setting**, not a
  compile-time constant.
- Colour: a **soft loop through existing palette stops**, not a raw hue circle — the footer's
  mode markers and the orange code accent are already on screen and a full hue sweep collides
  with them.
- Glitter timing: the **breath is slow (~6s)**, the **glyph re-rolls fast** — that contrast is
  what makes it read as glitter rather than as a pulse.
- **The glyph animation and the colour loop are two orthogonal settings** (owner, 2026-07-25).
  The colour loop is *not* a property of a style: it is a separate key that applies to whichever
  style is selected. Three styles × colour on/off = **six valid combinations**, and every one of
  them must work. Nothing in the code may key colour off the style, and no style may hard-code
  its own colour behaviour.
- **The spinner shipping today stays available** (owner, 2026-07-25). `classic` is a
  first-class, permanently supported style — not a deprecated fallback — and
  `spinner: classic` + `spinner-color: false` must reproduce the **exact** pre-plan look:
  the eight frames `⣾⣽⣻⢿⡿⣟⣯⣷` at 10 fps, faint on the status bar's black field, one column
  wide. Item 1 pins that with a test and item 5's acceptance re-checks it end to end.

---

## 1. Own the spinner animation — drop `bubbles/spinner`, no visible change — ✅ DONE (2026-07-26)

NOTES (2026-07-26): three deviations from the literal item text, all additive.
(a) `TestParseSpinnerStyle` — listed under item 5's tests — landed here in `spinner_test.go`
instead, because item 1 is where `ParseSpinnerStyle` is introduced and an exported,
error-returning function ships with its test; item 5 must NOT re-add it (duplicate test name).
(b) The styles are a registry (`spinnerSpecs`, keyed by `SpinnerStyle`, holding
`{interval, width, glyph func(frame int) string}`) rather than a switch: it gives item 1's
`TestSpinnerFrameWidth` the "declared width" it asserts against, and items 2/3 add one entry
each. A style the vocabulary knows but the registry has no animation for — snake and glitter
until 2/3 land, and the zero value — resolves to classic (`spinnerAnim.spec`), so the status
line can never render a blank column; `TestSpinnerStyleFallsBackToClassic` pins that.
`ParseSpinnerStyle` still accepts all three names (the const block is the vocabulary), and
`ParseSpinnerStyle("")` resolves to `snake`, the plan's stated default.
(c) `view` renders through `th.spinnerBase` (background only, no foreground) for now, which is
what makes the byte-identical pin hold. **Item 4 take note:** its sketched `!s.color` branch
renders through `th.statusBar`, which ADDS a faint foreground and would break
`TestSpinnerClassicUncolouredIsUnchanged` and item 5's "exactly the pre-plan look" row — keep
`spinnerBase` for the uncoloured branch, or that acceptance criterion has to be renegotiated
with the owner.

**What.** New `internal/tui/spinner.go` + `internal/tui/spinner_test.go` holding the animation
as pure functions plus a copy-safe value type. **Classic frames only in this item.** The swap
is behaviour-identical by design, so a regression in the tick plumbing surfaces here, isolated
from the new styles.

```go
// SpinnerStyle names a status-line animation. internal/tui owns the vocabulary so the config
// layer validates against one source of truth rather than duplicating the name list.
type SpinnerStyle string

const (
    SpinnerSnake   SpinnerStyle = "snake"
    SpinnerGlitter SpinnerStyle = "glitter"
    SpinnerClassic SpinnerStyle = "classic"
)

// ParseSpinnerStyle maps a config value onto a style. "" ⇒ the default; an unknown value is an
// error naming the styles this build knows.
func ParseSpinnerStyle(s string) (SpinnerStyle, error)

// spinnerAnim is the animation state carried on the value-copied Model (ADR 0011): plain ints,
// no RNG handle, no self-referential type. Every frame is a pure function of `frame`, which is
// what keeps it safe under the copy and testable without a clock.
type spinnerAnim struct {
    style SpinnerStyle
    color bool // the colour loop runs (item 4; false until then)
    frame int  // frames elapsed since the chain was armed
    gen   int  // chain generation — drops a tick left over from a previous arm
}

func newSpinnerAnim(style SpinnerStyle, color bool) spinnerAnim
func (s spinnerAnim) interval() time.Duration // per-style frame rate
func (s spinnerAnim) glyph() string           // this frame's braille cell(s)
func (s spinnerAnim) view(th theme) string    // the glyph, painted
func (s *spinnerAnim) arm() tea.Cmd           // gen++, frame = 0, schedule the first tick
func (s spinnerAnim) tick() tea.Cmd           // tea.Tick(s.interval(), → spinnerTickMsg{s.gen})

type spinnerTickMsg struct{ gen int }
```

The `Update` case replacing `model.go:369-378`. The generation check is the guard bubbles gave
for free — without it, re-arming while a tick is still in flight leaves two chains running:

```go
case spinnerTickMsg:
    // Keep the chain alive only while running, and only for the current generation: a tick
    // from a previous arm is dropped, so a re-arm (an approval answered, an ask replied to)
    // cannot leave two chains running and double the frame rate.
    if m.state != stateRunning || msg.gen != m.spin.gen {
        return m, nil
    }
    m.spin.frame++
    return m, m.spin.tick()
```

Wiring, all mechanical:
- `Model.spinner spinner.Model` (`model.go:78`) → `Model.spin spinnerAnim`.
- `statusLine` (`model.go:1536`): `m.spinner.View()` → `m.spin.view(m.th)`.
- The six arm sites listed in *The problem*: `m.spinner.Tick` → `m.spin.arm()`. Each already
  operates on the local `m` copy that is returned, so the `gen++` lands.
- `theme.go`: delete `brailleFrames` and `newBrailleSpinner`, drop the
  `charm.land/bubbles/v2/spinner` import (bubbles stays in `go.mod` — viewport and textarea use
  it), add a `spinnerBase lipgloss.Style` field built as `lipgloss.NewStyle().Background(colBlack)`,
  and delete the one-off style construction at `model.go:139`. **Correct theme.go's header
  comment**, which claims the spinner frames live there (this item owns that amendment).
- Classic keeps `["⣾","⣽","⣻","⢿","⡿","⣟","⣯","⣷"]` at `time.Second / 10`. **This style is
  permanent, not transitional** — it is the spinner the owner has today and it stays selectable
  forever. With `color: false` its rendered output is byte-identical to the pre-plan spinner;
  that equivalence is the item's whole acceptance criterion, so do not "tidy" the frame list,
  the FPS, or the black-background style while moving them.
- Until item 5 lands, `newModel` constructs `newSpinnerAnim(SpinnerClassic, false)`.

**Tests.** In `internal/tui/spinner_test.go`:
- `TestSpinnerClassicMatchesLegacyFrames` — the eight glyphs, in order, cycling at 8, at
  `time.Second / 10`, one column wide.
- `TestSpinnerClassicUncolouredIsUnchanged` — `newSpinnerAnim(SpinnerClassic, false).view(th)`
  is byte-identical to what the deleted bubbles widget rendered: pin the expected string as
  `lipgloss.NewStyle().Background(colBlack).Render("⣾")` and friends. This is the regression
  guard for "the current spinner stays available".
- `TestSpinnerFrameWidth` — `ansi.StringWidth` of every frame of every implemented style equals
  that style's declared width (1 for classic).
- `TestSpinnerTickChainGeneration` — after a re-arm, a `spinnerTickMsg` carrying the previous
  `gen` returns a nil `Cmd` and does not advance `frame`; one carrying the current `gen` returns
  a non-nil `Cmd` and advances it by one. Also: a current-`gen` tick outside `stateRunning`
  returns nil.

**Acceptance.** The green gate above. `TestStatusLineAlignsWithTranscriptText`,
`TestStatusLineIndentFitsNarrowWindow` and `TestModelStatusLineActivity`
(`model_test.go:1644-1730`) pass **untouched**; the two "spinner tick not re-armed" assertions
(`model_test.go:702`, `:922`) pass with only their field name updated.
`git grep -n 'bubbles/v2/spinner' internal/` returns nothing. A live run looks exactly as it
does today.

**commit.** `refactor(tui): the status-line spinner owns its animation instead of bubbles`

---

## 2. The snake style (owner idea 2) — ✅ DONE (2026-07-26)

**What.** Two braille cells side by side form a 4-column × 4-row dot grid; a 6-dot snake walks
the 12 positions of its outer ring clockwise, one position per frame.

**Derive the frames in code from a ring table — do not hand-write them** — and pin the derived
output in the test. The derivation is the documentation; the literals are the guard.

Braille dot bits inside one cell (the rune is `U+2800 + mask`), local column `c` ∈ {0,1},
row `r` ∈ {0..3}:

| | r0 | r1 | r2 | r3 |
|---|---|---|---|---|
| **c0** | `0x01` | `0x02` | `0x04` | `0x40` |
| **c1** | `0x08` | `0x10` | `0x20` | `0x80` |

The ring, clockwise from the top-left, as global `(col, row)` with cols 0–1 in the left cell and
cols 2–3 in the right:

```
(0,0) (1,0) (2,0) (3,0) (3,1) (3,2) (3,3) (2,3) (1,3) (0,3) (0,2) (0,1)
```

Frame *k* lights ring positions *k* … *k+5* (mod 12). That yields exactly:

```
f0  ⠉⠹     f3  ⢀⣸     f6  ⣆⣀     f9   ⡏⠁
f1  ⠈⢹     f4  ⣀⣰     f7  ⣇⡀     f10  ⠏⠉
f2  ⠀⣹     f5  ⣄⣠     f8  ⣏⠀     f11  ⠋⠙
```

Rate: `time.Second / 12` — twelve frames, so exactly one lap per second.

Two notes for the implementer:
- `f2` and `f8` contain `U+2800` (blank braille). That is a **painted cell, not a space**, so
  `leadingColumns` (`model_test.go:1686`, which trims `" "`) is unaffected and the alignment
  test keeps holding.
- The style is **two columns wide** where classic is one, so selecting it shifts the activity
  phrase one column right. That is expected; `statusLine` composes by concatenation and
  `TestStatusLineIndentFitsNarrowWindow` covers the clipping.

**Tests.**
- `TestSnakeFrames` — the code-derived frames equal the twelve literals above, in order.
- `TestSnakeIsSixDotsOnTheRing` — for every frame, the popcount across both cells is exactly 6,
  and every lit dot is a ring position (no interior dot is ever lit).
- `TestSnakeCycles` — frame 12 equals frame 0, and all twelve frames are distinct.
- `TestSpinnerFrameWidth` (item 1) extends to cover snake at width 2.

**Acceptance.** The green gate. The style is selectable by constructing
`newSpinnerAnim(SpinnerSnake, false)` in a test and rendering a frame sweep.

**commit.** `feat(tui): a six-dot snake spinner runs the outer ring of a 4x4 braille grid`

---

## 3. The glitter style (owner idea 1) — slow breath, fast sparkle — ✅ DONE (2026-07-26)

NOTES (2026-07-26): three deviations from the literal item text, none behavioural.
(a) The 20 fps constant is named `glitterInterval`, not `glitterFPS`: it holds a
`time.Duration` (`time.Second / 20`) and sits beside the existing `classicInterval` /
`snakeInterval`, which `spinnerSpecs` reads as `interval:` — a duration named `FPS` reads as
the number 20. Its comment still states the rate and that backing it off is a one-line edit.
(b) The sketch's literal `3.5` is the named constant `glitterDensityScale =
float64(brailleDots-1) / 2` (numerically identical, single rounding either way), so the
mapping is tied to the block's eight dots rather than to a bare number; the new
`brailleDots = 8` sits with `brailleBase`/`brailleDotBits` as a block fact.
(c) `TestGlitterDensityBreathes` checks the swell/fall monotonicity **from the trough**
(three quarters of a breath in): the sketch's phase starts at a zero crossing, so frame 0 is
mid-rise and a "rises then falls" check anchored at frame 0 would be rise-fall-rise. The test
pins the trough (1 dot), the peak (8 dots), one direction per arc, and periodicity.

**What.** Sort the whole `U+2800`–`U+28FF` block by density (dots lit) into buckets, then paint
two cells per frame with a *pseudo-random* member of the bucket the breath currently calls for.
The breath is slow — **6 s for a full swell-and-fall** — while the glyph re-rolls **every frame
at 20 fps**. That contrast is the requirement: a slow breath with a slow glyph is a pulse, not
glitter.

```go
// brailleByDots buckets U+2800..U+28FF by how many of the eight dots are lit — the density
// sort. Bucket sizes are the binomials: 1, 8, 28, 56, 70, 56, 28, 8, 1.
var brailleByDots = buildBrailleByDots()

// glitterDensity is the breath: a sine over one glitterBreath period, mapped onto 1..8 dots.
// It never reaches 0 — a blank cell mid-run reads as a stalled spinner, not as a dim one.
func glitterDensity(frame, framesPerBreath int) int {
	phase := 2 * math.Pi * float64(frame%framesPerBreath) / float64(framesPerBreath)
	return 1 + int(math.Round(3.5*(1+math.Sin(phase))))
}

// glitterCell picks this frame's glyph for one cell. The pick is a hash of (frame, cell), NOT a
// *rand.Rand: the Model is value-copied on every Update (ADR 0011), so an RNG handle would be
// shared across copies and advance from discarded ones. A hash keeps every frame a pure
// function of `frame` — reproducible in a test, identical no matter how often View runs.
func glitterCell(frame, cell, dots int) rune
```

`glitterCell` indexes `brailleByDots[dots]` modulo its length with a splitmix64 finaliser over
`uint64(frame)*0x9E3779B97F4A7C15 + uint64(cell)*0xBF58476D1CE4E5B9`.

Constants: `glitterBreath = 6 * time.Second`, `glitterFPS = time.Second / 20` ⇒ 120 frames per
breath. At peak density the 8-dot bucket holds only `⣿`, so the breath tops out on a solid
`⣿⣿` and falls away again — intended, and the visual anchor of the effect.

The 20 fps repaint is up from 10. `View()` composes pre-rendered viewport rows, so the per-tick
cost is small — but keep `glitterFPS` a named constant so backing off is a one-line edit, and
say so in its comment.

**Tests.**
- `TestGlitterDensityBreathes` — across one breath the density reaches both 1 and 8, rises then
  falls monotonically (allowing rounding plateaus), and `frame` and `frame+framesPerBreath`
  agree.
- `TestGlitterCellMatchesDensity` — across a sweep of frames, each rendered cell's dot count
  equals that frame's density.
- `TestGlitterIsPure` — the same frame rendered twice yields the same glyph.
- `TestGlitterSparkles` — over 20 consecutive mid-density frames at least 10 distinct glyphs
  appear. This is the test that fails if the effect ever regresses to a static or
  slowly-changing cell, which is the specific thing the owner asked to avoid.
- `TestSpinnerFrameWidth` (item 1) extends to cover glitter at width 2.

**Acceptance.** The green gate.

**commit.** `feat(tui): a breathing glitter spinner picks braille cells by density`

---

## 4. The soft colour loop (owner idea 3) — ✅ DONE (2026-07-26)

NOTES (2026-07-26): four deviations from the literal item text.
(a) The uncoloured branch of `view` keeps **`th.spinnerBase`**, not the sketch's `th.statusBar`:
`statusBar` adds a faint foreground, which would break item 1's byte-identical pin
(`TestSpinnerClassicUncolouredIsUnchanged`) and item 5's "exactly the pre-plan look" row. This
was raised by item 1's implementer, confirmed independently, and authorized before this item ran.
(b) Consequently `TestSpinnerColorOffIsFaint` landed as **`TestSpinnerColorOffPaintsNoForeground`**
— with `spinnerBase` there is no faint grey to assert, only the absence of any per-frame
foreground — and the orthogonality table's "colour off ⇒ faint grey" row asserts the bare field
instead.
(c) The stops are read off the palette **variables** (`colGauge`, `colModePlan`,
`colModeAllowEdits`) through `colorful.MakeColor`, rather than re-typed as hex strings for
`colorful.Hex`: same three colours, but one source of truth, so a palette edit moves the loop
with it instead of silently diverging from it.
(d) `TestSpinnerColorPeriodIsEightSeconds` asserts the lap is within a **microsecond** of 8 s
rather than exactly equal, and additionally pins the exact frame counts (96 / 160 / 80):
`snakeInterval` is `time.Second / 12` = 83.333333 ms after integer-nanosecond truncation, so
snake's 96 frames land 32 ns short of 8 s and a literal `==` is unsatisfiable. A loop that really
drifted with the frame rate would miss by whole seconds.

**What.** An 8-second closed loop through three colours **already in the palette**
(`internal/tui/theme.go:24-47`), blended in Oklch:

`colGauge #7c7cf0` (periwinkle) → `colModePlan #2ee6c5` (turquoise) →
`colModeAllowEdits #58a6ff` (blue) → back to periwinkle.

```go
// spinnerColor is the frame's foreground: a closed loop through spinnerStops, blended in Oklch.
// BlendOkLch keeps chroma up across the arc where an sRGB lerp desaturates the midpoints into
// mud. Palette stops rather than a raw hue circle: the footer's autonomy-mode markers and the
// orange code accent are already on screen, and a full hue sweep collides with them.
func spinnerColor(frame, framesPerLoop int) color.Color
```

**The loop is orthogonal to the glyph animation** — it is a separate `spinnerAnim.color` flag,
never a property of a style. `spinnerAnim.view` is the single place the two compose:

```go
func (s spinnerAnim) view(th theme) string {
    if !s.color {
        return th.statusBar.Render(s.glyph()) // faint grey — the status bar's own tone
    }
    return th.spinnerBase.Foreground(spinnerColor(s.frame, s.framesPerColorLoop())).Render(s.glyph())
}
```

Nothing else may branch on `s.style` to decide colour, and no style may carry a colour of its
own. All six style × colour combinations are valid and must render.

`framesPerLoop = int(spinnerColorPeriod / s.interval())` — computed per style, because the
rates differ: 96 frames at 12 fps (snake), 160 at 20 fps (glitter), 80 at 10 fps (classic).
The **period is 8 s for all three**; only the frame count differs.
`spinnerColorPeriod = 8 * time.Second`.

`github.com/lucasb-eyer/go-colorful` is **already in the module graph** as an indirect
dependency via lipgloss (`go.mod`, v1.4.0, verified to expose `BlendOkLch`). Using it directly
promotes it to a direct `require` — run `go mod tidy` and commit the `go.mod` move.
Implementation: `colorful.Hex(stop)` → `BlendOkLch(next, t)` → `lipgloss.Color(c.Hex())`.

Record this caveat in the code comment: this codebase does **no** terminal-profile detection
(quantisation happens downstream in `colorprofile`), so on a 256-colour terminal the gradient
steps visibly and on a 16-colour one it collapses to a couple of tones. The
`ui.spinner-color: false` key from item 5 is the answer for those terminals.

**Tests.**
- `TestSpinnerColorLoops` — frame 0 and frame `framesPerLoop` are the same colour, and the loop
  passes within a small distance of each of the three stops.
- `TestSpinnerColorIsSoft` — the Oklab distance between consecutive frames is under a small
  bound for **every** frame of the loop. This is the "soft" in the requirement, and it is what
  catches a stop list with a discontinuity or a wrap seam.
- `TestSpinnerColorOffIsFaint` — `color: false` renders through the status bar's grey and emits
  no per-frame foreground, **for every style including classic**.
- `TestSpinnerColorIsOrthogonalToStyle` — table over all **six** combinations (snake, glitter,
  classic × colour on, off): with colour on, each style's frame 0 and frame `framesPerLoop/3`
  carry *different* foregrounds and the glyph matches that style's own frame sequence
  unchanged; with colour off, each renders the faint grey. This is the test that fails if
  anyone later keys colour off the style.
- `TestSpinnerColorPeriodIsEightSeconds` — for each style,
  `framesPerLoop * interval() == 8 * time.Second`, so the styles' differing frame rates do not
  drift the loop's wall-clock period.

**Acceptance.** The green gate, plus a live run: the spinner drifts through the three tones over
8 s with no visible seam at the wrap, and `classic` + colour on shows the **old glyphs in the
new colours** (proof the two axes are independent).

**commit.** `feat(tui): the spinner drifts through a soft eight-second palette loop`

---

## 5. Config plumbing — the `ui:` block

**What.** A new **file-only** `ui:` block — no flag, no env, per the newer-key convention
documented on `autoCompact` / `contextWindow` / `mcpServers` / `present` in
`cmd/apogee/config.go`. It follows the `present:` block precedent end to end; that block is the
authoritative shape to copy.

```yaml
# ui:
#   spinner: snake         # the in-progress spinner: snake | glitter | classic
#   spinner-color: true    # slow 8s colour loop through the palette; false = the status bar's faint grey
```

**Two independent keys, deliberately.** `spinner-color` applies to whichever style `spinner`
names — it is not a property of a style and it is not folded into the style name. `classic` is
the spinner apogee ships today and stays a first-class choice; `spinner: classic` with
`spinner-color: false` is the exact pre-plan look, and `spinner: classic` with
`spinner-color: true` is the old glyphs in the new colours. The default resolves to
`snake` + colour on; a config file that sets only one key leaves the other at its default.
The template prose must say all of this — someone reading `config.yaml` should not have to
guess whether picking `classic` also turns colour off.

| Step | File | Change |
|---|---|---|
| on-disk template | `cmd/apogee/defaults/config.yaml` | the block above, fully commented, in the house prose style, placed near the `present:` block (~L147) — what each style looks like, that it is config-file only, and the 256-colour caveat for `spinner-color` |
| parse | `cmd/apogee/config.go` `fileConfig` (~L415) | `UI *uiConfig \`yaml:"ui"\`` plus a `uiConfig` struct and a `toUISettings()` converter, mirroring `presentConfig` / `toPresentSettings` (L440-459) |
| resolve | `config.go` `layer` (~L220) and `settings` (~L114) | `ui *uiSettings` on the layer, `ui uiSettings` on settings; base default in the `resolveSettings` literal (L238-239) = `{spinner: "snake", spinnerColor: true}`; a file-only branch beside the `file.present != nil` one (L266-268) |
| validate | `config.go` (L711, beside `s.present.validate()`) | `uiSettings.validate()` calls `tui.ParseSpinnerStyle` and returns an error naming `ui.spinner` and listing the styles this build knows — one source of truth for the valid set, no duplicated string list |
| composition root | `cmd/apogee/wire.go:306` | `Spinner: s.ui.spinner, SpinnerColor: s.ui.spinnerColor` added to the `tui.Options{…}` literal |
| renderer seam | `internal/tui/tui.go` `Options` (L130-202) | `Spinner SpinnerStyle` and `SpinnerColor bool`, each documenting its zero value: an empty style resolves to the default (snake); `false` means no colour loop. `wire.go` always sets both, so the zero value only reaches hand-built test Options — where a still, uncoloured spinner is the useful default |
| construction | `internal/tui/model.go` `newModel` (L133-139) | `spin: newSpinnerAnim(opts.Spinner, opts.SpinnerColor)` |

**`newTheme()` keeps its zero-argument signature.** The spinner style is animation state on the
Model, not a lipgloss style, so none of the ~44 `newTheme()` call sites across the test suite
move. Do not thread config into the theme.

**Tests.**
- `cmd/apogee/config_test.go` — a `resolveSettings` table row for a file layer carrying the
  block; one for the block absent (the defaults hold); **one for each key set alone** (only
  `spinner:` → colour stays at its default `true`; only `spinner-color: false` → the style
  stays at its default `snake`), which is what pins the two keys as independent at the config
  layer; one asserting an unknown style is a startup error whose message names `ui.spinner` and
  lists the three styles.
- `cmd/apogee/defaults_test.go` — the existing assertion that the embedded template parses to an
  **empty** layer must still pass, so the new block must be fully commented out.
- `cmd/apogee/wire_test.go` — the `recordingLauncher` sees the resolved style and colour flag in
  `tui.Options`.
- `internal/tui` — `TestParseSpinnerStyle` over the three names, `""`, and a bogus value.

**Acceptance.** The green gate, plus a real run walking the grid in `~/.apogee/config.yaml`:

| `spinner` | `spinner-color` | expected |
|---|---|---|
| *(absent)* | *(absent)* | snake, colour cycling — the new default |
| `glitter` | `true` | fast sparkle, 6 s breath, colour cycling |
| `glitter` | `false` | fast sparkle, 6 s breath, faint grey |
| `classic` | `true` | the **today** glyphs `⣾⣽⣻…`, colour cycling |
| `classic` | `false` | **exactly** what apogee renders today — the pre-plan look, restored |
| `sparkle` | — | startup fails, naming `ui.spinner` and the three valid styles |

**commit.** `feat(config): a ui block selects the status-line spinner and its colour loop`

---

## 6. Documentation

**What.** The cross-cutting doc amendments this plan owes, gathered under one owning item
(theme.go's own stale header comment is **not** here — item 1 owns it, next to the code it
describes):

- `layout.md:44` — the status-line sketch still shows the single-cell `⣻`. Update it to the
  default style's two-cell frame and note in the surrounding prose that `ui.spinner` selects
  the animation.
- `internal/tui/doc.go` — add `spinner.go` to the module narration (the file list around
  L14-17, where `theme.go` is currently described as holding "the spinner frames"). State the
  ADR-0011 reason the animation is a pure function of a frame counter rather than an RNG
  handle, and that the `gen` counter is what keeps a re-arm from doubling the tick chain.
- **No `CONTEXT.md` change** — the spinner introduces no domain vocabulary. Do not add one.
- No ADR: this is a presentation choice inside the shape ADR 0011 already fixes, not a new
  architectural decision.

**Tests.** None — documentation only.

**Acceptance.** The green gate. `internal/tui/doc.go`'s narration names every non-test file in
the package (check with `ls internal/tui/*.go`).

**commit.** `docs(tui): the spinner styles and the ui config block`

---

## 7. (OPTIONAL) `apogee spinner` previews the animations

**What.** **Optional — the owner may skip this item outright**, and `/implement-plan` should
treat a decision to skip it as a normal outcome, not a failure. It exists only because the two
animations cannot be judged from a plan document, and comparing them otherwise means editing
`config.yaml` and starting a real turn per style.

A `spinner` subcommand registered from `cmd/apogee/subcommands.go:18` (today
`return []*cobra.Command{newProbeCommand()}`), running a small bubbletea program that paints
all three styles, labelled, one per row, live, until a key is pressed. Keep it under ~80 lines
and in its own file; it reuses the `internal/tui` styles only through exported names, or —
simpler and preferred — lives in `internal/tui` as an exported `RunSpinnerPreview(ctx)` that
`cmd/apogee` calls, so nothing about `spinnerAnim` needs exporting.

**Tests.** A `cmd/apogee/root_test.go`-style assertion that the command registers and appears
under `--help`. No TUI test — it is a manual preview by definition.

**Acceptance.** `go run ./cmd/apogee spinner` shows three labelled live rows and exits on any
key.

**commit.** `feat(cli): apogee spinner previews the status-line animations`

---

## Verification (whole plan)

1. **Per item**, the green gate at the top of this document.
2. **Targeted**: `go test ./internal/tui/ -run 'Spinner|Snake|Glitter|StatusLine'` and
   `go test ./cmd/apogee/ -run 'Resolve|Defaults|Wire'`.
3. **Live, in a real terminal** — the animations cannot be judged from tests. `go run ./cmd/apogee`,
   send a prompt, watch the status line through a full turn:
   - the snake laps once per second and reads as one continuous 6-dot arc, not as two cells;
   - the colour drifts through periwinkle → turquoise → blue over 8 s with no seam at the wrap;
   - the spinner stays aligned with the transcript's text column above it;
   - it stops cleanly when the turn ends, leaving the idle status line empty.
   Then set `ui:` / `spinner: glitter`, restart, and confirm the sparkle is fast while the
   breath takes ~6 s to swell to a solid `⣿⣿` and fall back. Finally set `spinner: classic`
   with `spinner-color: false` and confirm the status line is indistinguishable from the
   pre-plan build (keep a screenshot of `main` before item 1 lands to compare against).
   Walk the full six-cell grid from item 5's acceptance table — the two axes are independent
   and the live run is where that is actually judged.
4. **The re-arm paths** (this is what the `gen` guard from item 1 protects): trigger an approval
   prompt and answer it, then answer an `ask_user` question. The spinner must resume at its
   normal rate, not a doubled one.
5. **Narrow window**: shrink the terminal to ~20 columns mid-turn; the status line clips, never
   wraps.
6. **Degraded colour**: run once with `TERM=xterm-256color` and once with `TERM=xterm` to see
   the quantised and collapsed gradients, confirming `spinner-color: false` is a useful escape
   hatch rather than a theoretical one.
