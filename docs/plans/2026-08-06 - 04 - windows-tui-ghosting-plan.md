# Windows TUI ghosting — diagnosis and fix implementation plan

- **Goal:** apogee's TUI paints correctly on Windows. Today, fragments of earlier frames stay
  on screen, text is eaten mid-line, and only a terminal resize (a forced full repaint) cleans
  it up. macOS is unaffected. This plan lands a permanent diagnostic seam, takes the
  measurements that separate four live hypotheses, fixes the cause, and pins the fix with a
  regression test that runs headless on Windows CI.
- **Date:** 2026-08-06
- **Status:** in progress — items 1-4 ✅ done 2026-08-06, items 5-6 ✅ done 2026-08-07; items 7-8 open.
  The cause is found, fixed and confirmed by the owner in Windows Terminal — see **"Item 6 — THE
  ANSWER"** at the end of this file.
- **Predecessor:** `docs/handoffs/2026-08-06 - 00 - windows-tui-ghosting-debug.md` — the symptom
  report and the first (unsuccessful) dependency-bump attempt. This plan supersedes its debug
  plan; the symptom description there is still ground truth.

## Authoritative sources

- `github.com/charmbracelet/ultraviolet@v0.0.0-20260803092147-8b693049ce2a` (module cache) —
  `terminal_renderer.go` is the painter. `xtermCaps` (L1590), `moveCursor` (L1516),
  `relativeCursorMove` (L1363), `el0Cost` (L804).
- `charm.land/bubbletea/v2@v2.0.8` (module cache) — `cursed_renderer.go` (the renderer bubbletea
  actually uses; note L606 and L690-701), `termios_windows.go`, `tty_windows.go` (L55),
  `tea.go` L786-798 (the mode-2027 reaction) and L1076 (`setOptimizations`).
- `internal/tui/width.go` — `widthAuthority`, the app-side mirror of the painter's measure. Its
  header comment is the design record for the layout-vs-paint width problem.
- `internal/tui/tui.go:852-893` — `Run`, `claimAltScreen`, and the three raw sequences apogee
  sends on its own behalf.
- ADR 0011 / `internal/tui/doc.go` — the value-copied `Model`. Anything new on the Model is a
  plain value.
- The **Established findings** below — measured on the owner's machine on 2026-08-06 during the
  planning session. Where an item's assumption disagrees with a finding, the finding wins.

## Established findings (measured, not assumed)

1. **Environment.** Windows 11 Pro 25H2, build 26200.8894. Windows Terminal 1.24.11911.0. The
   WT `settings.json` has **no `compatibility` block**, so `textMeasurement` sits at its
   documented default (`graphemes`). Not yet checked: the same run inside conhost (`cmd.exe` in
   a classic console window) and inside VS Code's integrated terminal (xterm.js).
2. **`TERM` is unset on Windows.** Verified empty in a PowerShell session on the machine. This
   is not a misconfiguration — Windows shells do not follow the Unix `TERM` convention.
   (`COLORTERM=truecolor` was present, but that session was VS Code's integrated terminal, which
   sets it; a bare Windows Terminal PowerShell may not. Item 4 measures this per terminal.)
3. **An unset `TERM` gives the painter ZERO capabilities.** `xtermCaps` switches on the first
   `-`-separated field of `TERM`; `""` matches no case and returns `0` (`noCaps`). So on Windows
   the renderer believes the terminal has no `VPA`, no `HPA`, no `CHA`, no `ECH`, no `ICH`, no
   `REP`, no `SD`/`SU`. On macOS, `TERM=xterm-256color` yields `allCaps &^ capHPA &^ capCHT &^
   capREP` — i.e. VPA, CHA, CBT, ECH, ICH, SD and SU are all available. **This is a real,
   Windows-only divergence in the renderer's emission strategy and it was not on the handoff's
   list.** Absolute `CUP` is still reachable (`moveCursor` L1519-1535), but only when the move is
   "not local" or when `CUP` happens to be the shortest candidate; short within-line moves are
   emitted as relative movement only.
4. **bubbletea hardcodes the Windows cursor-movement optimizations.** `termios_windows.go` sets
   `useHardTabs = true` and `useBackspace = true` unconditionally, next to a literal upstream
   `TODO: check if we can optimize cursor movements on Windows` (`tty_windows.go:55`). On Unix
   both are derived from termios flags (`termios_unix.go:12-13`). There is no public bubbletea
   option to turn either off.
5. **Upstream already distrusts Windows terminals in this exact area.** `cursed_renderer.go:606`
   disables scroll optimization on Windows only, "due to bugs in some terminals".
6. **The painter's width tables are NOT the problem.** Measured against the repo's pinned
   `x/ansi v0.11.7` and `runewidth v0.0.23`, every glyph apogee paints measures identically
   under `WcWidth`, `GraphemeWidth` and `runewidth`: the snake spinner pair `⣿⣿` = 2, the classic
   spinner = 1, `·` `✦` `…` `─` `│` `✓` `•` `→` `█` = 1, `✨` and `中` = 2. The **only**
   mismatches are VS16 sequences — `⚠️` (U+26A0 U+FE0F) is Wc 1 / Grapheme 2, and `ℹ️` the same.
   The handoff's "braille width disagreement" hypothesis is therefore **not** supported on the
   painter side. If a width disagreement exists it is the terminal's, not the library's.
7. **bubbletea does not merely adopt grapheme measurement — it switches the terminal into it.**
   `setWidthMethod(GraphemeWidth)` writes `ansi.SetModeUnicodeCore` (`CSI ?2027h`) to the
   terminal (`cursed_renderer.go:690-701`). A terminal that answers the `DECRQM` query but does
   not honour the mode desyncs the two sides.
8. **Nothing in apogee writes to the terminal behind the renderer's back during a session.** The
   only Windows-only raw write is the pre-warm notice at `cmd/apogee/wire.go:241`, which goes to
   stderr *before* the alt screen is claimed, by design. Ruled out.

## Live hypotheses, ranked

- **H1 — last-column / pending-wrap disagreement.** apogee paints full-width rows (the status
  bar spans the terminal), so every frame writes the last column of a row. conhost's
  delayed-EOL-wrap semantics have historically differed from xterm's, and `DISABLE_NEWLINE_AUTO_RETURN`
  is set on the output handle (`tty_windows.go:48-52`). A pending-wrap disagreement puts the
  cursor a whole row off — which is exactly the handoff's "ghost spinner dots appear one row
  above".
  **FALSIFIED (2026-08-07)** by finding 28: measured in all three terminals with
  `apogee probe terminal`, the wrap is *deferred* — including in the two that ghost. The
  disagreement this bullet supposes does not exist on any measured path.
- **H2 — `noCaps` makes any column error permanent (finding 3).** With no `CHA`/`HPA`, the
  renderer cannot re-anchor a column within a row; it counts relative moves from its own model.
  One bad advance poisons `curbuf` for that line, the diff renderer then believes those cells are
  already correct, and nothing repaints them until a full redraw. **This is the mechanism that
  explains "only a resize fixes it" no matter which of H1/H3/H4 starts the error** — and on its
  own it is enough to turn a rare glitch into the constant ghosting seen.
- **H3 — hard tabs (finding 4).** The renderer moves the cursor with `\t` against an assumed
  every-8 tab-stop model. If the terminal's stops are elsewhere, or a tab fills the cells it skips
  rather than just moving over them, both eaten text and stale cells follow.
  **Correction (2026-08-07):** this bullet originally credited the `DECST8C` (`CSI ?5W`) emission to
  ultraviolet. That is the wrong layer — ultraviolet's *renderer-level* emission is commented out
  upstream behind a TODO (`terminal_renderer.go:1200-1205`) — but bubbletea still sends it, once
  before the first frame whenever hard tabs are on (`cursed_renderer.go:282-284`), and hard tabs are
  on unconditionally on Windows (`termios_windows.go:9`). So every apogee TUI run on Windows does
  ask for every-8 stops before it paints, and the "after DECST8C" reading is the state the painter
  faces there. Finding 30 measures the stops in all three terminals: they were already every 8 and
  DECST8C moved nothing. The hypothesis stays falsified (finding 13), now with a direct measurement
  behind it as well as the A/B.
- **H4 — terminal-side width disagreement.** WT's `textMeasurement` and its font-fallback path
  may measure braille or ambiguous-width glyphs differently from the painter, even though the
  painter's own tables are self-consistent (finding 6).

H2 is a severity amplifier and a plausible primary cause; it is worth fixing on its own merits
regardless of which of H1/H3/H4 the measurements implicate.

**ALL FOUR ARE RETIRED, and the cause was none of them (2026-08-07).** H1, H3 and H4 are falsified by
direct measurement (findings 13, 14, 23, 28, 31) and H2 turned out to be an amplifier on the wrong
axis (finding 23). A fifth candidate the plan added later — mode 2026's degenerate synchronized
window — is falsified too (finding 39). The actual cause is an ordering defect in apogee's own
start-up that no hypothesis here named: the alternate screen is claimed before bubbletea sets the
console mode, so `DISABLE_NEWLINE_AUTO_RETURN` lands on the wrong screen buffer and the console
rewrites the renderer's bare `LF` as `CR LF`. See findings 40-47 and **"Item 6 — THE ANSWER"** at the
end of this file; the fix is in the tree.

## Standing requirements

- skills: `coding-standards`.
- Any authorized deviation from item text lands as a dated `NOTES (<YYYY-MM-DD>):` line under the
  item.
- `makeWin.bat check` (Windows) / `make check` (macOS) before every commit. Per the known
  baseline, three tests fail on the Windows host for environment reasons and `gofmt -l .` is
  useless there because of `autocrlf` — judge those two gates on macOS or in CI, not locally.
- ADR 0011: anything added to the `Model` is a plain value.
- Cross-build must stay green for all six release targets; every item below must compile on
  non-Windows even when its behaviour is Windows-only (`_windows.go` / `_other.go` pairs, the
  house pattern used by `internal/platform/prewarm_*.go`).
- No version identifier changes (VERSION, CHANGELOG release heading, tags).

## Out of scope

- Forking or vendoring the renderer stack in the committed tree. A patched bubbletea is a
  **scratch-only** experiment (see item 4); if the fix must live upstream, this plan reports it
  upstream and lands a local mitigation, it does not carry a fork.
- Re-litigating the mode-2027 coupling in `internal/tui/width.go`. It was re-verified against
  v2.0.8 during the handoff and finding 6 clears the painter's tables.
- Any change to apogee's glyph vocabulary as a *first* resort. Dropping the braille spinner is a
  fallback the plan reaches for only if item 5 proves the terminal measures it wrongly and no
  protocol-level fix exists.
- macOS/Linux rendering behaviour, which is not reported as faulty.

## Working artifacts already prepared

The planning session left a ready-to-run debug kit at **`C:\Users\airic\apogee-ghosting-debug\`**,
deliberately outside the repo. See its `README.md` for the full contract. In short:

- `apogee-dbg.exe` — apogee at `25ea817` built against a patched bubbletea v2.0.8 carrying five
  env-gated hooks (`APOGEE_DBG_TRACE`, `APOGEE_DBG_LOG`, `APOGEE_DBG_NO_HARDTABS`,
  `APOGEE_DBG_NO_BACKSPACE`, `APOGEE_DBG_NO_GRAPHEME`). **With no env vars set it behaves as stock
  bubbletea**, so it reproduces the ghost normally.
- `bubbletea-dbg/` — those patched sources, with rebuild instructions.
- `termprobe/` — a pinned `go.mod` for the standalone form of item 3. `main.go` is not written yet.

**Item 4 does not depend on items 1-3 landing in the repo.** Items 2 and 3 make these capabilities
supported and permanent; the kit above already provides them well enough to take the measurements.
This matters because item 4 can then run while another session is editing the codebase.

## 1. `ctrl+l` forces a full repaint — relief that does not wait for the diagnosis — ✅ DONE (2026-08-06)

NOTES (2026-08-06): two departures from the item's literal text, both forced by the codebase.
(a) **There is no `/help` surface.** apogee's TUI has no `/help` verb — `commandSpecs`
(`internal/tui/command.go:133`) is the whole registry of "/" verbs and it has no help row, and no
overlay lists the chords either. The surface where the *other* global chords (`⇧⇥`, `PgUp`/`PgDn`,
`esc`, `⌃c`) are listed is the README's keys paragraph, so `⌃l` was documented there
(`README.md:257-263`) and in `handleKey`'s doc comment. If a `/help` verb is ever added, that is
where this line belongs too.
(b) **The hint line was checked and left alone, as instructed.** `idlePlaceholder` measures **60
cells**; the shortest honest addition (`· ⌃l redraw`) takes it to **72**, which with the input box's
borders and padding overruns an 80-column terminal. It was not added and nothing else was shrunk.
The running legend (47 cells) has room, but a chord advertised in only one of the two states is
worse than one advertised in neither.
Also: no CHANGELOG entry — item 8 explicitly owns the user-visible CHANGELOG text for `ctrl+l`, and
item 8 is not yet done.

**What:** Bind `ctrl+l` in the global key switch (`internal/tui/model.go`, alongside
`case "ctrl+c":` at L909) to return `tea.ClearScreen`, which drives bubbletea's
`clearScreen()` → `MoveTo(0,0)` + `Erase()` and forces the next frame to be a full redraw —
the same resync a terminal resize performs today, without resizing. `ctrl+l` is currently
unbound anywhere in `internal/tui` (verified). This is the conventional readline/terminal
meaning of the key, so it earns its place permanently regardless of the root cause. Mention it
in the `/help` surface where the other global chords are listed; do not add it to the footer
hint line (that line is already at its width budget — check before deciding, and if it does not
fit, say so in NOTES rather than shrinking another segment).

**Tests:** `internal/tui`: `ctrl+l` from the running state returns a command that produces
bubbletea's clear-screen message and leaves the Model otherwise unchanged; it does not steal the
key from the prompt editor's own bindings; it is swallowed (not forwarded) while an overlay is
open, consistent with the modal contract.

**Acceptance:**
- `go test ./internal/tui/ -run 'Key|Clear'` passes.
- `go build ./...` passes.
- `makeWin.bat check` (or `make check`) passes.

**Commit:** `feat(tui): ctrl+l forces a full repaint`

## 2. A supported trace seam — `--tui-trace` and `--tui-diag` — ✅ DONE (2026-08-06)

NOTES (2026-08-06): no deviation from the item's text; two things worth recording.
(a) **No user-facing docs and no CHANGELOG entry** — item 8 explicitly owns "document the two
hidden flags … where the other diagnostics are documented" and the CHANGELOG line, and item 8 is
not yet done. Same call item 1 made for `⌃l`.
(b) **`github.com/charmbracelet/x/term` moves from indirect to direct in `go.mod`** (one line, via
`go mod tidy`; `go.sum` unchanged). The compile-time `var _ term.File = (*tracedOutput)(nil)`
assertion the item asks for is only worth having against the real interface, which means importing
it.

**What:** Give apogee its own diagnostic seam so no future Windows rendering bug needs a patched
bubbletea.

- `--tui-trace <path>` (hidden flag, `cmd/apogee/root.go`): every byte the renderer writes to the
  terminal is appended to `<path>` as one quoted Go string per write.
- `--tui-diag <path>` (hidden flag): a small text log recording, at startup and on change, the
  resolved `TERM`, `COLORTERM`, `WT_SESSION`, `TERM_PROGRAM`, the terminal size, the resolved
  `colorprofile.Profile`, every `tea.ModeReportMsg` (mode number and value — this is the
  mode-2027 answer the handoff wanted surfaced), and the width method `widthAuthority` ends up
  on.

**The one constraint that decides the design:** the tracer must be installed via
`tea.WithOutput`, and bubbletea only treats its output as a terminal when it type-asserts to
`term.File` (`tty_windows.go:36`, `tea.go:1083`). A plain `io.Writer` wrapper silently disables
raw mode, VT processing, size detection and the optimizations under test — which would make the
trace a trace of a different program. The wrapper must therefore satisfy `term.File` in full,
delegating `Fd()` (and whatever else the interface requires at the pinned `x/term v0.2.2`) to
`os.Stdout` and teeing only `Write`. Pin that with a test; it is the single thing most likely to
be got wrong here.

The mode-report half of `--tui-diag` needs the messages that reach `Update`. bubbletea forwards
`ModeReportMsg` to the model after acting on it (`tea.go:786-798` does not `continue`), and
`widthAuthority.observe` already receives it — so this is an observation point that exists, not a
new one to create.

Both flags are hidden, default off, and cost nothing when unset. Off Windows they behave
identically; this is a portable seam, not a Windows one.

**Tests:** `cmd/apogee` and `internal/tui`: the wrapper satisfies `term.File` (compile-time
assertion plus a test that `Fd()` returns stdout's descriptor); with tracing on, bytes written
reach both the terminal and the trace file, in order and unmodified; with the flags unset,
`tea.NewProgram` is constructed with exactly today's options (guard against an accidental
always-on wrapper); the diag log records a `ModeReportMsg` fed through `Update`.

**Acceptance:**
- `go test ./cmd/apogee/ ./internal/tui/ -run 'Trace|Diag|Flag'` passes.
- `go build ./...` and the six-target cross-build pass.
- `makeWin.bat check` (or `make check`) passes.

**Commit:** `feat(tui): hidden --tui-trace and --tui-diag seams`

## 3. A terminal measurement probe — `apogee probe terminal` — ✅ DONE (2026-08-06)

NOTES (2026-08-06): three departures from the item's literal text, plus one addition.
(a) **The parser's tests are in `internal/probe`, not `cmd/apogee`.** The item's acceptance names
`go test ./cmd/apogee/ -run 'ProbeTerminal'`, but ADR 0010's thin-root rule puts the logic where
the host and model probes already live (`internal/probe/terminal.go`), so the table-driven CPR /
DECRQM tests — clean, interleaved, out-of-order, truncated, and one report delivered a byte at a
time — run as `go test ./internal/probe/ -run Terminal`. The acceptance command still covers what
it names: the command's wiring, the two distinct failures, and the not-a-terminal degradation.
(b) **Section 3's erase check and section 5's ECH/ICH are answered by an OS-level screen
read-back, and only on Windows.** No VT sequence reports what is *in* a cell — DSR-CPR answers
where the cursor is and nothing else — so "re-read the row to detect whether the tab erased what
it passed over" is not portably possible. `internal/probe/terminal_{windows,other}.go` reads the
console buffer with `ReadConsoleOutputCharacterW` where there is one and the report prints
`unverified` where there is not, rather than guessing. Everything else in the probe is pure
DSR-CPR and runs everywhere.
(c) **The refusal covers stdin as well as stdout, and a terminal smaller than 40×8.** The probe
reads its replies from stdin, so `apogee probe terminal < /dev/null` has nothing to measure even
with stdout on a terminal; and the wrap section needs a row below the one it writes. Both refuse
with a message naming the cause, both exit non-zero.
(d) **Addition, for item 4's open question 1.** Section 4 prints the `GetConsoleScreenBufferInfo`
cursor beside the DSR-CPR answer on Windows — the exact number that item's findings say a
maintainer will ask for — so the two can be compared rather than one trusted.
Also: no CHANGELOG entry and no user-facing docs. Item 8 explicitly owns "document … the probe
command where the other diagnostics are documented" and the CHANGELOG line naming
`apogee probe terminal`, and item 8 is not yet done — the same call items 1 and 2 made.

Depends on item 2 only for convention, not for code.

**What:** Add a `terminal` sub-command under the existing `probe` command group (`cmd/apogee/probe.go`,
`probemodel.go` establish the pattern). It puts stdin/stdout in raw mode, then **measures the
terminal instead of trusting it**, and prints a verdict table. Every measurement uses DSR-CPR
(`CSI 6n`) to read where the cursor actually landed, so each line is an observation, not an
assumption:

1. **Mode negotiation.** `DECRQM` for 2026 and 2027; report the raw answers. Then set
   `CSI ?2027h`, re-query, and report whether the mode actually stuck.
2. **Glyph advance.** For each glyph in apogee's vocabulary (the snake spinner pair, the classic
   spinner, `·`, `✦`, `…`, `─`, `│`, `✓`, `•`, `→`, `█`, `⚠️`, `ℹ️`, `✨`, `中`): write it at a
   known column, read the cursor back, and print `terminal advance` beside `WcWidth` and
   `GraphemeWidth`. Any row where they differ is flagged. Run the whole sweep twice — mode 2027
   off, then on — because a difference between those two runs is itself the finding. **This is
   the decisive test for H4.**
3. **Hard tabs.** Emit `DECST8C`, then from column 0 emit one `\t` and read the landing column;
   repeat across the row. Report the terminal's real tab stops against the renderer's assumed
   every-8 model, and whether `DECST8C` had any effect. Then write a known string, tab back over
   it, and re-read the row to detect whether the tab *erased* what it passed over. **Decisive for
   H3.**
4. **Last-column wrap.** Write a character in the final column and read the cursor position
   before and after writing one more character; report whether the terminal holds a pending wrap
   (xterm semantics) or has already wrapped. **Decisive for H1.**
5. **Capabilities actually present.** Probe `CHA`, `VPA`, `ECH`, `ICH`, `REP` by using each and
   reading back the result, and print them against what `xtermCaps(TERM)` currently claims. On
   Windows this is expected to show a fully capable terminal against a `noCaps` verdict —
   **the direct evidence for H2/finding 3.**

Output is a plain table plus a one-line summary per section (`OK` / `MISMATCH`), so pasting it
into an issue is enough. The command exits non-zero if any section reports `MISMATCH`, which is
what lets item 4's acceptance be checked mechanically.

**Tests:** the response parser is the testable core — table-driven tests over recorded CPR/DECRQM
byte strings, including truncated and interleaved replies, so a terminal that answers slowly or
noisily cannot hang or mis-parse. The command must degrade cleanly (clear message, non-zero exit)
when stdout is not a terminal; pin that, since it is how CI will hit it. Do not attempt to unit
test the interactive path itself.

**Acceptance:**
- `go test ./cmd/apogee/ -run 'ProbeTerminal'` passes.
- `apogee probe terminal < /dev/null` (or the Windows equivalent) exits non-zero with the
  not-a-terminal message rather than hanging.
- `go build ./...` and the six-target cross-build pass.
- `makeWin.bat check` (or `make check`) passes.

**Commit:** `feat(probe): apogee probe terminal measures the terminal's real behaviour`

## 4. DESIGN CALL — capture the evidence and pick the cause — ✅ DONE (2026-08-06)

> **Answer: H1 (last-column / pending wrap), amplified by H2. H3 and H4 ruled out.** Full evidence
> in "Item 4 findings" at the end of this file; the verdict table is under "Item 4 — ANSWER".
>
> **SUPERSEDED IN PART (2026-08-07): H1 is falsified.** The owner measured the last-column wrap in
> all three terminals with `apogee probe terminal` and it is *deferred* everywhere, including in
> both terminals that ghost — findings 28-29. This item's answer named a primary cause that the
> direct measurement contradicts; the corrected verdict table is under "Item 4 — ANSWER" and the
> measurements are under "Item 6 findings — the owner's real-terminal probe runs". **No cause is
> named today.** The observations recorded in this section all stand; the causal reading built on
> finding 20's replay does not.

**Runnable now — does NOT depend on items 1-3.** Items 2 and 3 turn these capabilities into
supported repo features; the prebuilt kit at `C:\Users\airic\apogee-ghosting-debug\` already
supplies them. The only piece missing is `termprobe/main.go`, which this item builds **inside the
kit directory, not in the repo** (item 3 later ports it to `apogee probe terminal`). Executed this
way the whole item touches nothing under `C:\Users\airic\Repos\apogee\` except appending its
findings to this plan file — so it is safe to run alongside another session editing the code.

**This item cannot be completed by an agent alone.** It requires the owner in front of a real
Windows Terminal, reproducing the ghost by eye. An agent can build the probe and analyse the
traces; it must not invent an outcome for any row of the run sheet it did not watch a human
perform. Report `QUESTION` and hand back the run sheet rather than guessing.

**Q:** Which hypothesis do the measurements implicate — H1 (wrap), H2 (`noCaps`), H3 (hard tabs),
or H4 (terminal width)?

**Run sheet** (each row is a separate run; the whole sheet is ~15 minutes):

Let `KIT` = `C:\Users\airic\apogee-ghosting-debug`. Run row 1 first — it is the cheapest
discriminator in the sheet and it costs nothing to build.

| # | Where | What to run | What to record |
|---|---|---|---|
| 1 | conhost, VS Code, Windows Terminal | the normal `apogee`; reproduce a turn in each | does it ghost? yes/no **per terminal** |
| 2 | Windows Terminal | `$env:APOGEE_DBG_TRACE="$env:TEMP\t1.txt"; $env:APOGEE_DBG_LOG="$env:TEMP\d1.txt"; & $KIT\apogee-dbg.exe` — one turn until it ghosts, then quit | both files, plus a screenshot of the ghost |
| 3 | Windows Terminal | `$env:APOGEE_DBG_NO_HARDTABS="1"; & $KIT\apogee-dbg.exe` | does it still ghost? |
| 4 | Windows Terminal | `$env:APOGEE_DBG_NO_GRAPHEME="1"; & $KIT\apogee-dbg.exe` | does it still ghost? |
| 5 | Windows Terminal | normal `apogee` with `ui.spinner = classic` in the config | does it still ghost? |
| 6 | all three terminals | `go run .` in `$KIT\termprobe` (build it first) | the full table from each |

Start a **fresh shell between rows** — `$env:` assignments persist and would silently contaminate
the next run. That is the single easiest way to get a wrong answer out of this sheet.

Row 1 splits the field before any tooling exists: conhost, Windows Terminal and VS Code's xterm.js
all sit on ConPTY but are three different emulators. Ghosting in all three points at ConPTY/conhost
or at the renderer's Windows configuration (H2/H3); ghosting only in Windows Terminal points at WT
itself (H1/H4).

Rows 3 and 4 are single-variable A/B tests against the prebuilt kit and each falsify one hypothesis
outright — no build required. Row 5 is the visual check for H4 that the probe structurally cannot
make (the probe sees conhost's buffer, not WT's rendering — see the kit README).

Row 6 needs `termprobe/main.go` written first, per item 3's specification, **in the kit directory**.
Do not add it to the repo; item 3 owns the ported version.

**What lands in the repo for this item:** nothing but a findings section appended to this plan
under this heading, recording the tables, which rows ghosted, and the chosen cause with its
evidence. That section becomes the authoritative source for items 5 and 6.

**Acceptance:** the findings section names one primary cause, cites the specific probe rows or
trace bytes that establish it, and states explicitly which of H1-H4 are ruled out and which
remain open.

**Commit:** `docs(plans): record Windows ghosting measurements and root cause`

## 5. Give the painter a real capability set on Windows — ✅ DONE (2026-08-07)

NOTES (2026-08-06): the TERM-stack survey the item asks for, plus three departures from its
literal text.
(a) **Nothing else in the stack branches on `TERM` in a way this changes.** bubbletea's
`termcap.go` holds only `RequestCapability`/`CapabilityMsg` — an opt-in XTGETTCAP command apogee
never issues — and reads no environment at all. ultraviolet's decoder takes `TERM` for exactly one
thing, `buildKeysTable(flags, term, useTerminfo)`, and `TerminalReader.UseTerminfo` is the zero
value (bubbletea's `initInputReader` never sets it), so `buildTerminfoKeys` is unreachable and the
key table is `TERM`-independent. The two remaining readers are also unmoved: `tea.go:972`
`shouldQuerySynchronizedOutput` already returns true on all three Windows paths for reasons that
never involve `TERM` (no `TERM_PROGRAM` and no `SSH_TTY` in conhost and Windows Terminal, the
`TERM_PROGRAM`-is-not-Apple branch in VS Code), and `xterm-256color` matches none of its
ghostty/wezterm/alacritty/kitty/rio substrings, so the mode-2026/2027 query goes out exactly as
before; and `ultraviolet/terminal_renderer.go:245` branches on `HasPrefix(term, "linux")`, false
before and after.
(b) **`programOptions` gained an `environ []string` parameter** rather than calling the builder
inside itself, and item 2's three test call sites pass `nil`. `go test` gives the binary a piped
stdout, so a builder read in-place would take its "not a terminal" branch in every test and the
injecting branch would be unreachable from a unit test; as a parameter both branches are pinned.
The nil case still yields byte-identical options, which is what item 2's guard test asserts.
(c) **"`TERM` is unset" is implemented as unset OR empty.** `TERM=` names no terminal type and is
precisely the `xtermCaps("") → noCaps` case this item exists to remove, so it is treated as
absent rather than as a description to respect. The lookup is `uv.Environ`'s, so the decision is
taken on the same read the painter makes.
(d) **`github.com/charmbracelet/colorprofile` moves from indirect to direct in `go.mod`** (one
line, via `go mod tidy`; `go.sum` unchanged). The anti-regression test compares resolved
`colorprofile.Profile` values, which means importing the package — the same call item 2 made for
`x/term`.
Still open, and the owner's to close: the two acceptance rows an agent cannot take — `apogee probe
terminal` reporting the capability section `OK`, and colours unchanged by eye in Windows Terminal.
The colour claim has a mechanical stand-in in the meantime:
`TestInjectedEnvironmentResolvesTheSameColorProfile` was watched fail (`TrueColor -> ANSI256`,
conhost row) with the `COLORTERM` clause disabled, so the clause is pinned by a test that has been
seen red. Also: no CHANGELOG entry and no user-facing docs — item 8 owns both, the same call items
1-3 made.

Depends on item 4 (evidence), but is justified independently by finding 3 and should land even
if H2 turns out not to be the primary cause — a renderer that believes the terminal cannot
address a column is wrong on every modern Windows terminal, and it is what makes any other error
unrecoverable.

**What:** When apogee builds the program (`internal/tui/tui.go:879`), pass
`tea.WithEnvironment(env)` with a `TERM` the painter can act on, and only then. The rule, in a
Windows-only file with a no-op `_other.go` twin (the `internal/platform/prewarm_*.go` pattern):

- Only when `stdoutIsTerminal()` and `TERM` is **unset** — never override a `TERM` the user or a
  WSL/Cygwin/MSYS shell already set, and never touch the process environment; the injected slice
  is bubbletea's alone.
- Set `TERM=xterm-256color`. That yields exactly the capability set macOS already runs with
  (`allCaps &^ capHPA &^ capCHT &^ capREP`) — VPA, CHA, CBT, ECH, ICH, SD, SU — which is the
  proven-good configuration rather than a new one.
- Add `COLORTERM=truecolor` only when `COLORTERM` is unset.

**The regression this item must not cause:** `colorprofile.Detect(p.output, p.environ)`
(`tea.go:1083`) reads that same environment. Today, with `TERM` empty, detection resolves through
a Windows path; with `TERM=xterm-256color` and no `COLORTERM` it would resolve to 256 colours and
visibly flatten the palette and the spinner gradient. The `COLORTERM` clause above is what
prevents that, and the acceptance below pins it. Also confirm nothing else in the stack branches
on `TERM` in a way this changes — bubbletea's `termcap.go` (XTGETTCAP) and ultraviolet's decoder
are the two places to check; record the result in NOTES.

**Tests:** `internal/tui`: the env builder injects both keys when neither is set; injects neither
when `TERM` is already set; injects only `TERM` when `COLORTERM` is already set; injects nothing
when stdout is not a terminal; the process environment is never mutated. Assert the resolved
`colorprofile.Profile` for the injected environment equals the profile resolved today for the
same terminal — that is the anti-regression test, and it must compare profiles, not just assert
"truecolor". Off Windows the builder returns nil and the `tea.NewProgram` options are unchanged.

**Acceptance:**
- `go test ./internal/tui/ -run 'Environ|Term|ColorProfile'` passes.
- `apogee probe terminal` (item 3) reports the capability section as `OK` — the terminal's real
  capabilities now agree with `xtermCaps(TERM)`.
- Colours are unchanged by eye in Windows Terminal (owner confirms; record in NOTES).
- `go build ./...`, six-target cross-build, and `makeWin.bat check` / `make check` pass.

**Commit:** `fix(tui): give the painter a real TERM on Windows`

## 6. DESIGN CALL — the root-cause fix — ✅ DONE (2026-08-07)

NOTES (2026-08-07): the owner answered this item's design call with **"measure first, then ask
again"** — re-run the item 4 evidence sheet against the post-item-5 build, land only the parts of
this item that change no visible layout (the upstream reproduction/report work and any purely
internal fix), and stop with `BLOCKED` rather than write the one-column-short mitigation or any
other user-visible layout change. The measurements are in "Item 6 findings" at the end of this
file. They **do not close the design call either way**: the post-item-5 build paints correctly in
every configuration that could be measured without a human, but none of those configurations is one
of the three terminals the ghost was seen in, so the item's acceptance row ("the item 4 run sheet no
longer ghosts in Windows Terminal, owner confirms") is untouched. Nothing was changed in
`internal/`; the item is left open with a sharper question and a two-command run sheet for the
owner.

NOTES (2026-08-07, second pass): the owner ran command 1 of that run sheet — `apogee probe terminal`
in conhost, Windows Terminal and VS Code — and **the answer falsifies H1**: the last-column wrap is
deferred on every path, including both that ghost (findings 28-31). Per the owner's standing
instruction that branch forbids the two things this item's H1 bullet calls for, so neither was done:
no issue was written against `charmbracelet/ultraviolet`, because the one direct measurement
contradicts the claim it would make, and no one-column-short mitigation, which was never authorized
and is now also unmotivated. What landed instead is layout-neutral and documentary: item 4's ANSWER
and the H1/H3 hypothesis text corrected against the measurements, the DECST8C claim corrected in
`internal/probe/terminal.go` (that first correction was itself wrong — see the third-pass NOTES
below), and the measurements transcribed. Nothing in `internal/tui/` changed. Command 2 of the run
sheet — does the post-item-5 build still ghost in Windows Terminal — is **still not performed**, and
it is now the gate. The item stays open.

NOTES (2026-08-07, third pass): the second pass's DECST8C correction was itself wrong, and this pass
fixes only that. It checked `charmbracelet/ultraviolet` and stopped there, concluding "the renderer
never sends DECST8C" — but the emitter in this stack is bubbletea. `cursed_renderer.go:282-284`
writes `ansi.SetTabEvery8Columns` when `s.starting && s.hardTabs`, `termios_windows.go:9` sets
`useHardTabs = true` unconditionally on Windows, and apogee takes the default renderer
(`internal/tui/tui.go:1023`, no `WithRenderer`), so **every apogee TUI run on Windows sends DECST8C
before its first frame**. Only ultraviolet's *renderer-level* emission is commented out
(`terminal_renderer.go:1200-1205`). All four sites that carried the false claim are reworded — the
H3 bullet, finding 30, `measureTabs`/`everyEightStops`, and the DECST8C row's printed cell, which is
restored to `every 8 — bubbletea sends DECST8C at start`. Every cited line was re-read in the
versions pinned by `go.mod` (`charm.land/bubbletea/v2 v2.0.8`, `ultraviolet
v0.0.0-20260803092147-8b693049ce2a`). The falsification of H3 is unaffected: the stops were already
every 8, so DECST8C is a no-op on all three terminals. No layout mitigation was written, nothing was
filed upstream, and `internal/tui/` was not touched. **The item stays open**, still gated on command
2 of the run sheet.

NOTES (2026-08-07, fourth pass): the owner ran command 2 — the gate — and **the post-item-5 build
still ghosts in Windows Terminal, the default activity spinner included** (finding 32). On that
result the owner funded the ranked list's top entry, the mode-2026 A/B, and authorized the sixth
`APOGEE_DBG_*` kit flag it needs. `APOGEE_DBG_NO_SYNC` was added to `bubbletea-dbg/` and documented
alongside the other five, `apogee-dbg.exe` was rebuilt from this commit, and the A/B's *mechanics*
were measured end to end in the headless pseudoconsole (findings 33-34). Two deviations from the
item's literal text, both recorded here because that text predates the reopened diagnosis:
(a) **nothing was landed in `internal/`.** The A/B's visual half is an owner run by definition, so the
diagnosis is not closed and the fix finding 34 points at is not written. That fix *is* layout-neutral,
so if the owner's A/B comes back positive it can be landed without revisiting the layout
authorization.
(b) **no seventh flag.** Finding 33 shows the A/B varies two things at apogee's layer, which would
normally need a second flag to separate them, but finding 34 measures ConPTY collapsing the second one
away before it reaches any emulator, so the extra flag would have bought nothing. The reasoning is
written down instead of the code.
No layout change, nothing filed upstream, `internal/` untouched, and finding 28's stale
"left untracked in the repo root" is corrected in passing. **The item stays open.**

NOTES (2026-08-07, fifth pass): the owner answered the reopened design call with **"Land it either
way"** — land the mode-2026 filter now, on the strength of finding 34 alone, without waiting for the
visual A/B. It is landed (findings 36-38): a Windows-only `tea.WithOutput` wrapper that removes
`CSI ?2026$p` from bubbletea's single DECRQM write and leaves `CSI ?2027$p` on the wire, installed
through item 2's existing `programOptions` seam. **The justification is the measured one and only
that** — synchronized output is dead weight on Windows because ConPTY forwards the window empty
(finding 34), and apogee pays for it with bubbletea's cursor-hide flicker mitigation (finding 33).
**It is NOT claimed to fix the ghosting**, which stays unexplained; the owner runs the visual A/B
separately and nothing here waits on it or predicts it. Four departures from the item's literal
text, all of them because that text predates the reopened diagnosis:
(a) **No H1/H2/H3/H4 branch was taken**, because all four are falsified or retired (findings 13, 14,
23, 28, 31). What landed is the layout-neutral fix this item's own "If arm B is clean" paragraph
sketches, landed ahead of arm B on the owner's instruction rather than after it.
(b) **The byte filter itself is in a portable file** (`internal/tui/syncoutput.go`); only the
decision to install it is in the `_windows.go` / `_other.go` pair the standing requirements ask for.
Stripping a fixed substring out of a write is platform-independent, so putting the mechanism behind
`//go:build windows` would have hidden its tests from five of the six cross targets while adding
nothing. The behaviour is Windows-only exactly as specified: `programDeclinesSyncOutput()` returns
`stdoutIsTerminal()` on Windows and `false` everywhere else.
(c) **`programOptions` gained a fourth parameter and `programOutput` was factored out of it** — the
same seam, not a new one, and the same reasoning item 5's NOTES (b) recorded: read in place, the
decision would take its "not a terminal" branch in every test and the installing branch would be
unreachable from a unit test. `programOutput` exists so the *stacking order* of the two wrappers is
a value a test can inspect rather than a fact buried inside a `tea.ProgramOption` closure.
(d) **No upstream text, no CHANGELOG, no user-facing docs.** Nothing is filed anywhere, per the
standing constraint; the two upstream write-ups this item's text mentions still wait on the A/B.
Item 8 owns the CHANGELOG line and the docs, the same call items 1-5 made.
No layout change of any kind. **The item stays open** — the ghost is not explained, so the ranked
next-step list is updated rather than retired.

NOTES (2026-08-07, sixth pass): **the diagnosis is closed.** The owner ran the mode-2026 A/B and both
arms ghosted, which eliminates mode 2026 as the cause (finding 39); the owner then funded the ranked
list's entry (1), capturing what Windows Terminal's own `OpenConsole.exe` re-emits. That capture
reproduced the ghost on the first attempt — the first reproducing capture this plan has ever had —
and bisecting it gave a complete, measured mechanism: **a Windows console buffer without
`DISABLE_NEWLINE_AUTO_RETURN` rewrites the renderer's bare `LF` as `CR LF`, which destroys the column
the renderer was preserving**, and apogee puts the console in that state by claiming the alternate
screen *before* bubbletea sets the mode (findings 40-46). The fix is layout-neutral, three lines of
mechanism in a `_windows.go` / `_other.go` pair, and is landed under the dispatch's "any
layout-neutral fix you can prove IS authorized to land". Deviations from the item's literal text,
all because that text predates the closed diagnosis:
(a) **None of the H1/H2/H3/H4 branches was taken.** All four were already falsified or retired
(findings 13, 14, 23, 28, 31). The cause is none of them; it is an ordering defect in apogee's own
start-up, which no hypothesis on the list named.
(b) **Nothing is filed upstream and no upstream write-up is warranted.** The defect is apogee's, not
`microsoft/terminal`'s and not `charmbracelet`'s — finding 47 says why, and why the two write-ups
the fifth pass parked are now withdrawn rather than pending.
(c) **The mitigation has no unit test of its own**, which the item's acceptance asks for. What it
does is a two-call Win32 sequence on a real console handle; there is nothing in it a portable test
can observe, and a test that asserted `SetConsoleMode` was called would pin the implementation
rather than the behaviour. The mechanism is pinned by the measurements in findings 43-46 instead,
each reproducible from the harness, and the acceptance's real gate — the owner's eye in Windows
Terminal — is unchanged and still outstanding.
(d) **The CHANGELOG and user-facing docs are untouched**, the same call items 1-5 made; item 8 owns
them.
**The item stays open on one thing only**: the owner confirming by eye that a build of this tree no
longer ghosts. Everything else is done.

NOTES (2026-08-07, seventh pass): **the item is complete.** Two things closed it, both recorded as
findings continuing the sequence. (1) The owner ran a build of the sixth pass's fix in Windows
Terminal and confirmed it — no corrupted streaming, no ghost in the activity spinner, scrolling
correct (finding 48). That is this item's acceptance row, the one thing the sixth pass left open, and
with it the diagnosis of findings 40-47 is confirmed rather than merely consistent. (2) Review of that
fix found a real defect in it, new with the change: the console mode was never given back, and
bubbletea's own save/restore cannot give it back because it samples the mode *after* `Run` has already
changed it — so every Windows session would have handed the shell a console with newline-auto-return
disabled, staircasing the next bare-LF writer. `prepareAltScreenConsole` now returns a restore closure
and `Run` defers it immediately after the call, which puts it after the alt-screen exit write, after
bubbletea's restore, and on every error and panic path (finding 49). Two deviations, both small:
(a) the closure is `func()` rather than the probe's `(func(), error)` — this function deliberately
cannot fail the run (a console that refuses the flag leaves apogee where it was), so an error return
the caller must ignore would be a worse shape than none; (b) the item's acceptance asks for unit
coverage of the mechanism and there is still no portable seam for the two Win32 calls, unchanged from
the sixth pass's NOTES (c). What IS newly testable is the contract `Run` now depends on — the restore
is never nil on any platform — and `altscreen_test.go` pins that on all six targets. The mechanism
stays pinned by the measurements in findings 43-46. Still nothing filed upstream (finding 47),
no layout change, no CHANGELOG and no user-facing docs — item 8 owns those, the same call items 1-6
made.

Depends on items 4 and 5.

**Q:** After item 5, does the ghosting survive — and if so, does the fix belong upstream, in a
local mitigation, or both?

Re-run the item 4 run sheet against the item 5 build before doing anything here. If ghosting is
gone, this item becomes "confirm and record", and items 7-8 close the plan.

If it survives, the fix follows the cause item 4 named. The branches, with the shape each takes:

- **H3 (hard tabs).** No public bubbletea option exists (finding 4) and this plan does not carry
  a fork (out of scope). So: a minimal standalone repro against stock bubbletea, an upstream
  issue on `charmbracelet/bubbletea` pointing at its own `TODO` at `tty_windows.go:55` and at the
  `SetScrollOptim` precedent at `cursed_renderer.go:606`, and a local mitigation until it lands.
  Prefer a mitigation with a real mechanism over a cosmetic one.
- **H1 (wrap).** Same shape, but the repro and the issue belong against
  `charmbracelet/ultraviolet` (the renderer decides when to write the last column) and possibly
  `microsoft/terminal`. A local mitigation that keeps apogee's rows one column short of the
  terminal width is available and cheap, but it changes layout — it needs the owner's agreement
  before it is written, not after.
- **H4 (terminal width).** If mode 2027 is acknowledged but not honoured, the honest local fix is
  to stop switching the painter to `GraphemeWidth` on that terminal — which `internal/tui/width.go`
  is already designed to mirror, so both sides move together. Report upstream to
  `microsoft/terminal`. Only if that fails does the glyph vocabulary change, and that is a
  separate decision with the owner.
- **H2 alone.** Item 5 was the fix; record that and move on.

**What lands:** the chosen mitigation with its tests, plus a `NOTES` line under this item naming
the upstream issue URL where one was filed.

**Acceptance:** the item 4 run sheet no longer ghosts in Windows Terminal (owner confirms), the
mitigation has unit coverage for its mechanism, and `makeWin.bat check` / `make check` pass.

**Commit:** decided when the branch is known; conventional-commit form, `fix(tui):` scope.

## 7. A headless ConPTY regression test

Depends on item 6.

**What:** A Windows-only test (`//go:build windows`) that opens a real pseudoconsole
(`CreatePseudoConsole`), runs a small in-repo bubbletea program through it — a status line with
the snake spinner over full-width rows, i.e. the tight repro from the handoff — drives it through
enough frames to trigger the bug, reads back what the console buffer actually contains, and
asserts it matches the intended frame. Without ConPTY in the loop the test cannot see this class
of bug at all, which is precisely why it is worth the setup cost.

Keep the harness in one file with the Win32 calls isolated behind a couple of helpers, and skip
(not fail) when a pseudoconsole cannot be created, so it degrades cleanly on hosts that forbid
it. The assertion must be on the *rendered buffer*, not on the byte stream — a byte-stream
assertion would re-encode the renderer's own model and pass while the screen is wrong.

Add the failing case first and watch it fail against a pre-item-6 build; a regression test that
was never seen red is not a regression test.

**Tests:** the test *is* the deliverable. It must fail on a build with item 6 reverted and pass
with it applied — record both results in the verifier's `VERIFIED:` line.

**Acceptance:**
- `go test ./internal/tui/ -run 'ConPTY'` passes on Windows and skips cleanly off Windows.
- The six-target cross-build passes (the file must not break non-Windows compilation).
- `makeWin.bat check` passes.

**Commit:** `test(tui): ConPTY regression harness for Windows rendering`

## 8. Close the record

Depends on items 1-7.

**What:**
- ADR `docs/adr/0038-*.md` (next free number; 0037 is the highest today) recording the decision
  this plan reached — most likely "apogee names the terminal it is talking to on Windows", with
  finding 3 as its motivation and the `xtermCaps`/`noCaps` mechanism written down so the next
  person does not rediscover it. Word it around the decision and its consequences, following the
  existing ADRs' voice.
- `CHANGELOG` entry for the user-visible parts: the Windows rendering fix, `ctrl+l`, and
  `apogee probe terminal`. No version identifier changes.
- Document the two hidden flags and the probe command where the other diagnostics are documented.
- Archive the predecessor handoff (`docs/handoffs/2026-08-06 - 00 - windows-tui-ghosting-debug.md`)
  into `docs/handoffs/archived/`, since this plan and its item 4 findings supersede it.

**Tests:** none beyond the docs gates the repo already runs.

**Acceptance:**
- `makeWin.bat check` / `make check` passes.
- The ADR-0010 invariant gate passes.
- Every numbered item above carries a ✅ marker.

**Commit:** `docs(adr): record the Windows terminal-capability decision`

## A note on sequencing

Items 1-3 are independent of the diagnosis and can land immediately — they are the instruments
and the escape hatch. Item 4 is the measurement gate and needs the owner. Items 5-6 are the fix.
Items 7-8 close it. If the plan is executed with `implement-plan`, items 4 and 6 will stop the
run and consult, which is intended: they are the two points where guessing would produce a
confident-sounding wrong answer, which is the specific failure mode that cost the previous
session its dependency-bump attempt.

## Item 4 findings — ✅ COMPLETE (2026-08-06)

Every row was run by the owner on the Windows host; nothing here is inferred from a row nobody
performed. The verdict is under "Item 4 — ANSWER" at the end of this section, and that verdict is
the authoritative source for items 5 and 6.

| Row | Status |
|---|---|
| 1 | done — below (plus row 1b, a re-run added during the session) |
| 2 | done — traced in Windows Terminal **and** conhost (findings 18-21) |
| 3 | done — ghosts (finding 13) |
| 4 | done — ghosts (finding 14) |
| 5 | done — ghosts (finding 17) |
| 6 | not run — the trace replay settled the question first; still wanted as independent confirmation |
| A / B | added during the session — the mode-2027 answer per path (finding 12) |
| C1 / C2 | added during the session — backspace, and all knobs at once (findings 15-16) |

### Row 1 — does it ghost, per terminal?

Binary: `C:\Users\airic\apogee-ghosting-debug\apogee-stock.exe`, a frozen copy of the installed
`~\go\bin\apogee.exe` (stock bubbletea, built 2026-08-06 17:05 from the `25ea817` tree). Frozen so
a concurrent session's `go install` could not swap the binary mid-sheet. `ui.spinner` was at its
default `snake` — `~/.apogee/config.yaml` has no `ui:` block — so the two-braille-cell tight repro
was live in every row.

| Terminal | `WT_SESSION` | `TERM_PROGRAM` | `TERM` | Ghosts? | What was seen |
|---|---|---|---|---|---|
| conhost (classic console, launched `conhost powershell`) | empty | empty | empty | **NO** | formatting correct throughout. A few glyphs drew as tofu (`?`-in-a-box) — font fallback, not a paint error |
| Windows Terminal 1.24.11911.0 | GUID | empty | empty | **YES** | (a) letters eaten from the status line, (c) fragments of earlier frames, degrading continuously; snake spinner and prompt cursor misaligned; **the real cursor jumps to column 0 and back to column 2 while the model streams**. No tofu |
| VS Code integrated terminal (xterm.js) | empty | `vscode` | empty | **YES** | identical to Windows Terminal, cursor jump included. No tofu |

**Finding 9 — `TERM` is empty in all three terminals.** Finding 2 had verified this only in one VS
Code session. It is now measured in conhost, Windows Terminal and VS Code. So `xtermCaps("")` →
`noCaps` (finding 3) and bubbletea's hardcoded `useHardTabs`/`useBackspace` (finding 4) were in
force **identically in all three runs**. Neither is terminal-dependent, so neither can by itself
explain a result that differs per terminal.

**Finding 10 — the ghost follows ConPTY, not the front-end emulator.** `conhost powershell` gives
the app a classic console host: the app's VT is parsed and drawn by one process, with no
pseudoconsole in the path. Windows Terminal and VS Code both attach the shell to a **pseudoconsole**
— conhost running headless, which parses the app's VT into a text buffer and re-emits VT to the
front end. The ghost appears in exactly the two configurations carrying that parse-and-re-emit
layer and is absent from the one without it. The tofu in the conhost row corroborates that it
really was conhost's own renderer (Windows Terminal draws these glyphs without fallback).

This contradicts the row-1 decision rule stated in item 4, which assumed all three terminals sit on
ConPTY and therefore read "ghosts everywhere" as pointing at ConPTY *or* the renderer's Windows
configuration. With conhost-direct as a genuine third path, the discriminator is sharper than the
rule anticipated.

**What row 1 does to the hypotheses:**

- **H2 (`noCaps`) — not sufficient on its own.** By finding 9 the identical `noCaps`, hard-tabbed
  stream paints *correctly* in conhost. H2 survives in exactly the role the plan gave it: the
  amplifier that makes a column error permanent, not the trigger. Item 5 remains justified on its
  own merits.
- **H3 (hard tabs) — open, but the target moves.** Hard tabs were on in all three runs and conhost
  handled them. If tabs are the trigger, the fault is in ConPTY's re-emission of tab-moved cursor
  positions, not in conhost's own tab-stop model. Row 3 falsifies this outright.
- **H1 (wrap) — open, target moves the same way.**
- **H4 (terminal-side width) — substantially weakened, not closed.** Windows Terminal (DirectWrite
  plus its own `textMeasurement`) and xterm.js (JavaScript width tables, different font stack)
  share no measurement code, yet produced the same symptom set down to the cursor jump. A width bug
  private to one emulator cannot explain both; what they *do* share is the ConPTY buffer upstream.
  A caveat recorded here first — that the tofu meant conhost never exercised the same glyphs — was
  **retired by row 1b below**.
- **New symptom worth its own line.** The cursor jumping to column 0 and back to column 2 during
  streaming is a *live* cursor error visible on screen, not merely stale cells. Whatever
  mis-accounts position does so for the cursor bubbletea parks at the prompt.

**Not controlled in row 1:** terminal width was not held equal across the three runs, and both wrap
and tab-stop behaviour depend on it. And one negotiated input can still differ per terminal — under
ConPTY the `DECRQM` mode-2027 reply comes from headless conhost, not from Windows Terminal or
xterm.js. That answer is not yet measured; `APOGEE_DBG_LOG` measures it.

### Row 1b — conhost re-run with a glyph-complete font

The tofu in row 1 was a font gap, not a paint error: switching conhost's font (to Cascadia Code;
the earlier Consolas setting was the one that dropped glyphs) made every symbol render. The owner
then repeated a **complete streaming turn** in conhost on that font.

**Finding 11 — conhost paints apogee's full glyph vocabulary correctly through an entire turn.**
No eaten letters, no fragments, no cursor jump. This retires the row-1 caveat: conhost is now a
clean control that exercises the same glyphs as the two ghosting configurations.

This tightens finding 10 considerably. conhost-direct and ConPTY **share the same VT parser** — the
difference between them is the output engine: conhost-direct draws its buffer with the DX/GDI
renderer, while ConPTY re-serializes its buffer back out as VT for a downstream emulator. Row 1b
shows apogee's byte stream is parsed into a correct buffer by that shared parser. So the corruption
enters at or after the step that is not shared, which is **ConPTY's re-serialization of the buffer**.

The hypotheses relocate accordingly, and all three surviving ones become the same shape — "what in
apogee's stream makes ConPTY's re-serializer emit a wrong frame":

- **H4** now has to mean a width disagreement between conhost's buffer and the downstream emulator,
  not between the painter and one terminal — because the painter's stream demonstrably lands
  correctly in conhost's buffer. Rows 4 and 5 remain the tests.
- **H1 / H3** relocate identically: conhost's parser handles the wrap and the hard tabs (finding
  11), so the failure would be in how the re-serializer represents those cursor positions
  downstream.

**The one assumption this reasoning still rests on** is that apogee emits materially the same stream
in all three paths. Finding 9 establishes that for the two Windows-specific renderer settings, but
not for the mode-2027 answer, which under ConPTY comes from headless conhost. Runs A and B measure
exactly that, and if the answer differs between the paths, the argument above needs revisiting.

> **It did differ. Runs A/B below falsify that assumption, and with it the inference drawn in
> finding 10 and row 1b that the corruption must enter at ConPTY's re-serialization.** The
> observations in findings 10 and 11 stand as recorded; the causal reading built on them does not.

### Runs A and B — the mode-2027 answer, per path

Launch-and-quit runs of `apogee-dbg.exe` with `APOGEE_DBG_LOG` set, one per path.

| Path | `checkOptimizedMovements` | mode 2027 report | Painter ends on |
|---|---|---|---|
| conhost (direct) | `useHardTabs=true useBackspace=true` | **`value=0`** — not recognised | **`WcWidth`** (no `setWidthMethod` line) |
| Windows Terminal (ConPTY) | `useHardTabs=true useBackspace=true` | **`value=3`** — permanently set | **`GraphemeWidth`** (`setWidthMethod: 0 -> 1`) |

**Finding 12 — apogee runs a different painter down the two paths, and the split lines up exactly
with the ghost.** conhost does not recognise mode 2027, so bubbletea never calls `setWidthMethod`
and the painter keeps ultraviolet's `WcWidth` default. Windows Terminal answers `3` (permanently
set), so bubbletea switches the painter to `GraphemeWidth` (`cursed_renderer.go:690-701`) and
`widthAuthority.observe` mirrors it. Every configuration measured so far that ghosts is a
configuration where the painter adopted `GraphemeWidth`; the one that does not ghost is the one that
stayed on `WcWidth`.

This puts **H4 back in front**, in a sharper form than the plan stated it. Not "Windows Terminal
measures glyphs differently from the painter", but: *the terminal reports mode 2027 as permanently
set, the painter believes it and switches measurement, and something downstream does not in fact
measure that way.* Finding 6 already says where that can bite — across apogee's whole glyph
vocabulary the **only** `WcWidth`/`GraphemeWidth` disagreements are the VS16 sequences `⚠️`
(U+26A0 U+FE0F) and `ℹ️`, each Wc 1 / Grapheme 2. One such glyph painted at the wrong width drifts
the row by a column, and with `noCaps` (finding 3) there is no `CHA`/`HPA` to re-anchor it — H2, the
amplifier, turns that single column error into the permanent corruption actually seen.

It also demotes, without closing, the ConPTY reading: ConPTY is still the only path in which mode
2027 gets answered at all, so "ConPTY is in the path" and "the painter switched to `GraphemeWidth`"
are perfectly confounded across rows 1, 1b, A and B. **Row 4 is what separates them** — it puts
Windows Terminal on `WcWidth` while leaving ConPTY in the path.

**Not measured:** VS Code's mode-2027 answer. It ghosted (row 1) and it is a ConPTY path, so it is
*expected* to report a non-zero value, but no log was taken there. Worth one launch-and-quit run
before the conclusion is written.

### Rows 3 and 4 — flag verification

Both A/B runs flipped exactly one variable, confirmed in the logs:

| Row | Flag | Log line | Everything else |
|---|---|---|---|
| 3 | `APOGEE_DBG_NO_HARDTABS=1` | `useHardTabs=false useBackspace=true` | 2027 `value=3`, painter on `GraphemeWidth` (stock) |
| 4 | `APOGEE_DBG_NO_GRAPHEME=1` | `mode 2027: IGNORED, painter stays on WcWidth` | `useHardTabs=true useBackspace=true` (stock) |

**Visual outcome, reported by the owner: run B (stock), row 3 and row 4 all ghosted, with the same
symptoms as row 1.** Each in its own fresh Windows Terminal PowerShell.

**Finding 13 — H3 is falsified.** Hard-tab cursor movement off, everything else stock: still ghosts.
Tabs are not the trigger.

**Finding 14 — the painter-side half of H4 is falsified.** Row 4 put the painter on `WcWidth` —
byte-for-byte the width configuration conhost runs (finding 12) — with hard tabs on and `noCaps` in
force, i.e. **the identical renderer configuration to the path that does not ghost.** Windows
Terminal ghosted anyway. No assignment of width method to the painter changes the outcome under
ConPTY.

Note what row 4 does and does not control. Windows Terminal answers mode 2027 with `3`
(*permanently* set), so the terminal side stays on grapheme measurement whatever the painter does.
Row 4 therefore does not create a matched pair — it inverts the mismatch. But run B is the nominally
*matched* pair (painter `GraphemeWidth`, terminal grapheme) and it ghosts too. Matched and
mismatched both ghost; conhost, which never negotiates 2027 at all, does not. Width negotiation does
not separate the outcomes.

**This reinstates the finding-10 reading, now on direct evidence rather than on the assumption runs
A/B falsified.** Row 4 makes the renderer's configuration identical to conhost's and the only
remaining difference is the consumer: conhost drawing its own buffer versus ConPTY re-serializing it
for a downstream emulator. That is where the corruption enters.

**Standing after rows 1, 1b, A, B, 3, 4:**

| Hypothesis | State | Established by |
|---|---|---|
| H1 — last-column / pending wrap | **open** — the only original hypothesis still standing | not yet tested; needs row 2 or row 6 |
| H2 — `noCaps` makes any column error permanent | **amplifier, confirmed as such; not the trigger** | finding 9 (identical `noCaps` paints correctly in conhost) |
| H3 — hard tabs | **falsified** | finding 13 (row 3) |
| H4 — width disagreement | **falsified on the painter side** | finding 14 (row 4 + run B) |
| *new* — ConPTY's re-serialization of the buffer | **open, and now the leading candidate** | finding 14 + findings 10/11 |
| *new* — backspace cursor movement | **open, never tested** | finding 4's untested twin |

**H4 has a remnant that rows 3 and 4 do not touch.** Per finding 6 the braille cells measure 1
under *both* `WcWidth` and `GraphemeWidth`, so the painter says 1 in every configuration tested. If
Windows Terminal or ConPTY renders braille as 2, every row measured so far ghosts and conhost — which
agrees at 1 — does not. That is consistent with all six runs. So "ConPTY re-serialization" and
"braille is 2 cells downstream" are currently **observationally equivalent**, and row 5 is the run
that separates them.

Two things weigh against the braille remnant, neither conclusive. First, the predecessor handoff's
symptom 1 is stale *ASCII* half-lines ("d to update it and sync the submodule") far from the
spinner, and no width table disagrees about ASCII — that only follows from a glyph-width cause via
drift plus H2 amplification, which is strained. Second, all three spinner styles are braille
(`classic` is one cell, `snake` two, `glitter` two — `internal/tui/spinner.go:78,246-248`), so
**row 5 is not a braille/non-braille A/B** as the plan's framing implies; it is a one-cell versus
two-cell test. A clean row 5 would still be strong evidence, since a downstream braille width of 2
would drift `classic` too.

**Untested knob:** finding 4 records that bubbletea hardcodes `useHardTabs` **and** `useBackspace` on
Windows. The plan's H3 named only hard tabs; row 3 tested only hard tabs. Backspace-based cursor
movement is the other half of that finding and has never been varied. `APOGEE_DBG_NO_BACKSPACE=1`
exists in the kit for exactly this and is the cheapest untried discriminator left.

### Runs C1 and C2 — the rest of the app-side surface

Two further runs added during the session, Windows Terminal, fresh shell and one full turn each.

| Run | Flags | Log confirms | Ghosts? |
|---|---|---|---|
| C1 | `NO_BACKSPACE=1` | `useHardTabs=true useBackspace=false`, painter `GraphemeWidth` | **YES** |
| C2 | `NO_HARDTABS=1 NO_BACKSPACE=1 NO_GRAPHEME=1` | `useHardTabs=false useBackspace=false`, `mode 2027: IGNORED`, painter `WcWidth` | **YES** |

**Finding 15 — backspace cursor movement is not the trigger either** (C1). Finding 4's untested twin
is now tested.

**Finding 16 — the renderer's entire Windows-side configuration is exonerated.** C2 runs with hard
tabs off, backspace off and the painter on `WcWidth`. That is a *more conservative* emission
strategy than the one conhost renders correctly, which has both optimizations **on** (finding 12's
log line) and the same `WcWidth`. With every exotic cursor optimization disabled and the width
method matched to the known-good path, Windows Terminal still ghosts. No combination of the three
knobs the kit can vary makes any difference, so the trigger is not in how bubbletea is configured on
Windows.

Combined with finding 11 — conhost's parser turning apogee's stream into a correct buffer through a
whole turn — what remains is: **something about how the stream is consumed once ConPTY is in the
path, independent of which cursor-movement strategy produced it.** H1 (last-column / pending wrap)
is the one original hypothesis that survives this, because it concerns *where* the renderer writes
rather than *how* it moves, and none of rows 3, 4, C1 or C2 varied that.

### Row 5 — the classic spinner

`ui.spinner = classic` (one braille cell instead of snake's two), Windows Terminal, normal build.

**Finding 17 — H4 is finished.** The classic spinner ghosts too, and it does so in the same shape:
the owner reports it painted **in two places at once, at x=0 and x=2**. A single-cell glyph
misplaces exactly like the two-cell pair, so no braille width disagreement — the last remnant of H4
after finding 14 — can be the trigger. The 2-column offset is the same one seen in row 1, where the
prompt cursor jumped between x=0 and x=2 during streaming.

### Row 2 — the traces, and what replaying them proves

Captured with `APOGEE_DBG_TRACE` in **both** Windows Terminal and conhost, both with
`APOGEE_DBG_NO_GRAPHEME=1` so that each path ran the painter on `WcWidth` and the two byte streams
are directly comparable. Both runs streamed the same prompt. Geometry 120 columns; Windows Terminal
29 rows, conhost 40. `trace-wt.txt` = 1125 writes, `trace-conhost.txt` = 32.

**Finding 18 — a third, previously unnoticed divergence: synchronized output.** In Windows Terminal
1123 of 1125 writes are wrapped in `CSI ?2026h` … `CSI ?2026l`. In conhost the count is **zero** —
it falls back to `CSI ?25l` / `CSI ?25h` around each frame instead. Like mode 2027, this is
negotiated by `DECRQM` and differs per path, and unlike the other knobs the kit had no flag for it
(**one was added 2026-08-07 — `APOGEE_DBG_NO_SYNC`, findings 33-35**).
It is recorded because it was invisible to the whole hypothesis list, **but the replay below shows
it is not the cause**: mode 2026 is a batching hint that changes when cells are presented, not which
cells are written, and the stream is correct with or without it. **Read that dismissal narrowly.**
Finding 35 confirms the cells are unchanged, and finding 34 explains why; what neither the replay
here nor the one there can see is *presentation*, which is the only thing mode 2026 controls. The
hypothesis is live again and it is the one the owner's A/B tests.

**The decisive test — replay apogee's own bytes through a virtual terminal.** The trace holds
exactly what the renderer emitted, so the question "is the emitted stream right?" can be answered
offline with nobody watching a screen. `C:\Users\airic\apogee-ghosting-debug\tracereplay\` (written
for this item; zero dependencies) parses the trace, feeds it to a VT model, and prints the resulting
screen. It implements the last-column question as a switch:

- `-wrap deferred` — xterm semantics. Writing the final column leaves the cursor there with a
  pending-wrap flag; the wrap happens when the *next* printable arrives.
- `-wrap immediate` — the cursor moves to column 0 of the next row as soon as the final column is
  written.

**Finding 19 — apogee's emitted byte stream is correct.** Replayed under `deferred`, the Windows
Terminal trace produces a **completely clean screen**: the story text, the markdown table, the
separator, the prompt box and the status line all render exactly as intended, with every phrase
appearing exactly once. The same replay of the conhost trace also renders cleanly, which validates
the model against the path known to work. So the renderer did not emit a corrupt frame — the bytes
that produced the ghosted screen would produce a correct screen on an xterm-semantics terminal.

**Finding 20 — H1 is confirmed, and it reproduces the exact symptom.** Replaying the *same* Windows
Terminal trace under `-wrap immediate` reproduces the fault, in the same shape as the screenshot:
left-truncated tails of earlier lines superimposed on the right-hand part of rows.

| Phrase | `deferred` | `immediate` |
|---|---|---|
| `whispered, overriding` | 1 | 2 |
| `absolute limit` | 1 | 3 |
| `Contained Exotic` | 1 | 2 |
| `stabilized negative mass` | 1 | 1 |
| `elegant and terrifying` | 1 | 1 |

The owner's screenshot shows the same artifact independently: `whispered, overriding the safety
lockout. The system began streaming` repeated down the right-hand side, `rhaps stabilized negative
mass` (`perhaps…` with two cells eaten) and `re elegant and terrifying than` (`more…` with two cells
eaten). Same artifact, same left truncation, same 2-cell bite as the `immediate` replay.

**Provenance, so the two are not over-coupled:** that screenshot is *not* the final state of
`trace-wt.txt`. It was taken from a **resumed** session — the screen shows `· resumed: Hard Sci-Fi
Short Story Generation` — after the owner scrolled up and down a few times, whereas the traced run
ended with a `ctrl+c` and its own final frame carries the `press ctrl+c again to quit` hint. So the
screenshot corroborates the *symptom class* and nothing more; finding 20 rests on the traced bytes
alone, which is what makes it checkable by anyone with the file.

**Finding 21 — scrolling reproduces it too, with no streaming involved.** The owner reports that
scrolling the transcript up and down also breaks the content. That matters in three ways: the bug is
not specific to the streaming path, so it is not about update frequency or synchronized output
(finding 18); a scroll repaints full-width rows and so re-triggers exactly the last-column write
H1 turns on; and it gives item 7's ConPTY regression harness a **cheaper and more deterministic
repro than streaming** — scroll a full-width viewport, read the buffer back.

**The bytes that establish it.** A binary search for the first write at which the two wrap models
diverge returns **write 2** — the first full frame, which draws apogee's box rule
`╭` + 118 × `─` + `╮`. At 120 columns that `╮` lands in the final column. The disagreement is
therefore present from the very first frame apogee paints, and the trace never explicitly addresses
column 120 (max `CUP` column across the whole trace is 119): the cursor *arrives* at the last column
by writing there, which is exactly the state the two models disagree about.

Why it then never recovers is H2, exactly as the plan predicted. 689 of the 1125 Windows Terminal
writes contain no absolute `CUP` at all — they depend on the cursor being where the previous write
left it — and the trace navigates with 1596 `CR`, 1643 tabs and 616 relative `CUU`. With `noCaps`
(finding 3) there is no `CHA`/`HPA` with which to re-anchor a column, so one wrap disagreement in
frame 1 poisons the renderer's model of the screen and nothing but a full redraw resyncs it. That is
the "only a resize fixes it" clue from the predecessor handoff, mechanically explained.

## Item 4 — ANSWER

**Primary cause: H1, the last-column / pending-wrap disagreement.** apogee paints full-width rows —
the box rule, the `▔`/`▁` separators and the status line all span the terminal — so nearly every
frame writes the final column. ultraviolet emits that stream assuming xterm's deferred-wrap
semantics. On the ConPTY path the wrap is taken immediately, putting the cursor a row down and at
column 0, and every subsequent relative move inherits the error.

**Severity amplifier: H2**, confirmed in the role the plan assigned it — `noCaps` from an empty
`TERM` leaves the renderer no absolute column addressing with which to recover.

| Hypothesis | Verdict | Evidence |
|---|---|---|
| **H1 — last-column / pending wrap** | ~~**CONFIRMED — primary cause**~~ → **FALSIFIED (2026-08-07)** | findings 19-20 (replay) said confirmed; finding 28 measures the wrap directly in all three terminals and it is *deferred* in every one, the two that ghost included. See the correction below the table |
| **H2 — `noCaps`** | **CONFIRMED as amplifier, ruled out as trigger** | finding 9 (same `noCaps` stream paints correctly in conhost); finding 20 (why it never recovers) |
| **H3 — hard tabs** | **RULED OUT** | finding 13 (row 3), finding 15 (C1), finding 16 (C2) |
| **H4 — width disagreement** | **RULED OUT** | finding 14 (row 4 + run B), finding 17 (row 5) |

**Correction (2026-08-07) — what falsifying H1 does to the rest of this answer.** Open question 1
below asked for exactly the measurement that has now been taken, and it came back the other way.
The two paragraphs above are wrong where they say the wrap "is taken immediately" on the ConPTY
path; findings 28-29 measure it as deferred in conhost, Windows Terminal *and* VS Code. Three
consequences, and they are not all bad news:

- **Finding 19 gets stronger, not weaker.** It said apogee's emitted bytes render a clean screen
  under deferred semantics. The terminals are now measured to *have* deferred semantics, so the
  stream apogee emits is correct for the terminals it is emitted to. The corruption therefore
  enters after the bytes leave apogee.
- **Finding 20 changes what it proves.** Replaying under `immediate` reproduces the symptom, so the
  stream is *sensitive* to a one-row cursor error and that error's signature is the artifact seen.
  That remains a useful fingerprint of the fault class. It is no longer evidence for where the row
  error comes from, because the mechanism it proposed has been measured absent.
- **H2's verdict is untouched.** It was never the trigger and finding 23 already showed it is the
  amplifier of the wrong axis.

**No hypothesis on the H1-H4 list is now a live primary cause.** The diagnosis is reopened, and
item 6 carries the reopened question.

**Open, and honest about it:**

1. The replay shows apogee's stream is *sensitive* to the wrap model and that the immediate model
   reproduces the symptom. That is strong, but it is inference from a model I wrote, not a direct
   measurement of what ConPTY does. **Row 6 / item 3's section 4 is the confirmation**: write a
   character in the final column, read the cursor back with `GetConsoleScreenBufferInfo`, and see
   which semantics the path really has. Worth doing before an upstream issue is filed, because it is
   the one number a maintainer will ask for.
2. Which component actually resolves the wrap early is not yet established — ConPTY's parser,
   ConPTY's VT re-serializer, or an interaction with `DISABLE_NEWLINE_AUTO_RETURN`, which bubbletea
   sets on the output handle (`tty_windows.go:48-52`). This decides whether the upstream issue
   belongs on `charmbracelet/ultraviolet`, `microsoft/terminal`, or both.
3. Finding 18's synchronized-output divergence is unexplained and unused. It is not the cause, but
   it is a per-path behaviour difference nobody knew about and it should be written down before it
   surprises someone again.

**For item 5:** unchanged and still justified — H2 is confirmed as the amplifier, so giving the
painter a real `TERM` is worth landing on its own merits. Note it will **not** fix the ghost on its
own; it removes the mechanism that makes the error permanent, which may well be enough to make the
symptom invisible, but the trigger stays.

**For item 6:** ~~take the H1 branch as written — repro and issue against
`charmbracelet/ultraviolet` and possibly `microsoft/terminal`.~~ **Withdrawn 2026-08-07.** Open
question 1 was answered against this instruction: the wrap is deferred on the paths that ghost, so
neither the ultraviolet issue nor the one-column-short mitigation has a measured defect to point
at. Both would have been filed and written on an inference. See "Item 6 — WHERE IT STANDS".

## Item 6 findings — re-running the sheet against the post-item-5 build (2026-08-07)

The owner's answer to the item 6 design call was *measure first, then ask again*. What follows is
what could be measured **without a human at a screen**, and — stated as plainly as the rest of this
file — what could not. No row of the item 4 sheet that needs an eye was performed, and none is
reported here as though it had been.

### The instrument: a headless pseudoconsole

Everything below runs apogee inside a **real Windows pseudoconsole** created by the session with
`CreatePseudoConsole`, which is what makes the child's stdout a character device — the precondition
for item 5's `TERM` injection to fire at all (`stdoutIsTerminal()`, `tui.go:1092`). The harness lives
at **`C:\Users\airic\apogee-ghosting-debug\conptyrun\`** (outside the repo, the same call item 4 made
for `tracereplay`; item 7 still owns the in-repo version) and gives three things at once:

- `--tui-trace` — exactly the bytes apogee emitted, in the format `tracereplay` already reads;
- the pseudoconsole's **re-serialized output** — what a downstream emulator would receive, which
  `bin2trace` converts into the same format so the same VT model renders both;
- a scripted keyboard, so a run is deterministic and repeatable.

Every run below: 120×30, `--resume 20260806T155434Z-b82d2800.json` (a real transcript, so the screen
is full of the full-width rows this bug lives on), then typed keys and `PgUp`/`PgDn` scrolling —
finding 21's repro, which needs no model call. Captures are kept in
`C:\Users\airic\apogee-ghosting-debug\item6-evidence\`.

**Finding 22 — item 5 does what it claimed, measured on emitted bytes.** One binary, one script, one
variable: `TERM` unset (injection fires) against `TERM=vt100` + `COLORTERM=truecolor` (injection
declines, per its own rule, and `xtermCaps("vt100")` is `noCaps` — the pre-item-5 capability set
exactly). Both runs resolved `color-profile: TrueColor`, so colour is held equal.

| Run | `TERM` seen by the painter | CHA | VPA | ECH | writes |
|---|---|---|---|---|---|
| control (pre-item-5 equivalent) | `vt100` → `noCaps` | 0 | 0 | 0 | 7 |
| post-item-5 | injected `xterm-256color` | **38** | **2** | **40** | 7 |

And the screens the two streams intend are **byte-identical**. Item 5 changed how the painter
navigates and changed nothing about what it draws — which is the anti-regression claim, now measured
on output rather than on a profile value.

**Finding 23 — item 5 does not reduce the wrap sensitivity at all, and the reason is structural.**
Replaying each trace under `tracereplay -wrap deferred` against `-wrap immediate`: **28 of 30 rows
differ, before and after**, identically. The `immediate` screen reproduces finding 20's artifact in
the post-item-5 trace too — left-truncated tails of earlier lines superimposed on the right of rows
(`atures are defined as numbered tasks` landing across row 1).

The reason absolute addressing does not help is worth writing down, because it corrects an
expectation the plan carried into this item. An early wrap is a **row** error, not a column error:
the cursor gains a line, not a column. `CHA` re-anchors a column and is the capability item 5 mostly
bought (38 uses); `VPA` re-anchors a row and appears twice. So H2's amplifier is real but is the
amplifier of the wrong axis — item 5 removes the mechanism that makes a *column* error permanent,
and the trigger H1 names produces a *row* error. Item 5 remains right on its own merits
(finding 22) and is not a fix for this.

**Finding 24 — the upstream defect is now located in source, not inferred from a replay.** In
`ultraviolet/terminal_renderer.go` at the pinned `v0.0.0-20260803092147-8b693049ce2a`:

- `putCell` (L494-501) sends a cell to `putCellLR` **only** when the renderer is fullscreen *and* the
  cursor is at `width-1, height-1` — the lower-right corner of the whole screen. `putCellLR`
  (L552-563) writes `CSI ?7l` … cell … `CSI ?7h`, i.e. it disables autowrap for that one write. So
  ultraviolet already knows the DECAWM technique for the last-column problem and applies it to
  exactly one cell per screen. Both figures are visible in the traces: `?7l` appears **2 times** in
  every capture taken for this plan, the item 4 ones included.
- Every *other* last-column write takes `putAttrCell`, which sets `s.atPhantom = true` (L543-545) —
  the renderer's belief that the terminal is holding a pending wrap.
- `wrapCursor` (L505-514) then resolves that belief when the next cell arrives, and it
  **emits no bytes at all**: `const autoRightMargin = true` with the literal comment *"Assume we
  have auto wrap mode enabled"*, and a dead `else` branch. It updates `cur.X = 0; cur.Y++` and lets
  the terminal's own autowrap perform the move.

That is H1's mechanism in the renderer's own words. Against a terminal that takes the wrap when the
last column is written rather than when the next printable arrives, the cursor is one row past where
the renderer models it, for every full-width row, from the first frame on. `const autoRightMargin =
true` is a hardcoded terminfo `am`, and the fix upstream is the technique `putCellLR` already
carries, applied to every last-column write and not just the corner.

**Finding 25 — the obvious local mitigation that changes no layout is ruled out by that same code.**
Turning autowrap off for the whole session (`CSI ?7l` next to `claimAltScreen`) looks like a
layout-free way to make both wrap models agree — with no autowrap there is no wrap to disagree
about. It does not work, and finding 24 says why: `wrapCursor` emits nothing, so with autowrap off
the row advance it models **never happens on the terminal**, and every subsequent cell piles into the
last column. Recorded so nobody spends an afternoon on it.

**Finding 26 — the last-column semantics are now measured directly, and on this path they are
DEFERRED.** `apogee probe terminal` (item 3) was run inside the harness — row 6 of the item 4 sheet,
which that sheet records as *not run*, executed headlessly against a pseudoconsole. Its section 4:

| step | cursor (CPR) | console API | deferred wrap would be |
|---|---|---|---|
| wrote the final column | 6,120 | 6,120 | 6,120 (pending) |
| wrote one more | 7,2 | 7,2 | 7,2 |

`OK — the terminal holds a pending wrap at the last column`. DSR-CPR and
`GetConsoleScreenBufferInfo` agree, which is exactly the number item 4's open question 1 said a
maintainer would ask for. The other sections: capabilities `MISMATCH` (CHA, VPA, ECH, ICH and REP all
present against `xtermCaps(unset) = noCaps` — the direct evidence for finding 3 that item 5's
acceptance wanted, though note the probe reports the *process* environment, which item 5 correctly
never touches); hard tabs `OK`; glyph advance `MISMATCH` on the two VS16 sequences only, precisely
finding 6 and nothing more.

**Finding 27 — and consistently with finding 26, the ghost did not reproduce headlessly, in either
configuration.** Rendering the pseudoconsole's re-serialized output and diffing it against the screen
apogee intended:

| Run | writes | ConPTY re-serialization vs intended screen |
|---|---|---|
| post-item-5 | 7 | **exact match** |
| control (`noCaps`, pre-item-5 equivalent) | 7 | **exact match** |
| post-item-5, 36 keystrokes + 5 scrolls | 45 | **exact match** |

The control matching is the load-bearing row. **A harness in which the pre-item-5 configuration
paints correctly cannot be used to argue that the post-item-5 configuration is fixed** — it is not
reproducing the bug, so it is not measuring the bug. Item 5's contribution is finding 22; the clean
screens above are the harness's verdict on itself.

### Why this harness is not the ghosting path

Three differences from the two configurations that ghost, in the order they are worth attacking:

1. **It is the system pseudoconsole.** `CreatePseudoConsole` starts `conhost.exe --headless`.
   Windows Terminal ships and launches **its own `OpenConsole.exe`**, and VS Code drives ConPTY
   through node-pty. "ConPTY" is not one implementation, and finding 26 measures the one the ghost
   was never seen on. Note that this also means finding 26 does **not** answer item 4's open
   question 1 for the terminals that matter.
2. **Nothing negotiated modes 2026 or 2027.** The harness has no emulator downstream to answer
   `DECRQM`, and the pseudoconsole answered `not recognized (0)` for both rather than forwarding the
   query — so the painter stayed on `wcwidth` with no synchronized output. That is run C2's
   configuration, which ghosted on the real machine (finding 16), so it is a *ghosting-capable*
   configuration; but it was ghosting-capable there with WT's ConPTY, not this one.
3. **It is 45 writes, not 1125.** No streaming turn, because that needs a model. Finding 20 puts the
   first wrap divergence at write 2, so frame count should not matter — but it has not been shown not
   to.

The pseudoconsole's **re-emitted** stream is itself wrap-sensitive (23 of 30 rows differ between the
two models), so the disagreement, wherever it is resolved, survives all the way to the downstream
emulator. That keeps a second candidate alive that item 4 could not separate: the corruption may be
in how Windows Terminal or xterm.js consumes ConPTY's re-emission, not in ConPTY's buffer at all.

## Item 6 findings — the owner's real-terminal probe runs (2026-08-07)

The previous pass ended by asking for one thing: `apogee probe terminal`, run by the owner in the
three terminals of item 4 row 1, with the `last-column wrap` section as the discriminator. The owner
ran it. The raw captures were `windows-terminal-proble-results.md` (Windows Terminal and VS Code, as
text) and `windows-terminal-probe-conhost.png` (conhost, a screenshot), dropped untracked in the repo
root and **deleted by the owner on 2026-08-07 once they had been transcribed**. Everything
load-bearing in them is transcribed below; no agent lost them and nothing is missing.

Two properties of the instrument have to be read before the numbers are:

- **The console mode is bubbletea's.** `prepareTerminalOutput` (`cmd/apogee/probeterminal_windows.go`)
  sets `ENABLE_VIRTUAL_TERMINAL_PROCESSING | DISABLE_NEWLINE_AUTO_RETURN`, matching
  `tty_windows.go:36-53`. The second flag is precisely the one that adds the delayed end-of-line
  wrap state — item 4's open question 2 named it as a suspect. So the wrap below was measured in the
  console configuration a bubbletea session actually runs in, not in a default one.
- **The probe reads the PROCESS `TERM`** (`cmd/apogee/probeterminal.go:119`), and item 5 injects
  `TERM` into bubbletea's environment slice only, never into the process
  (`internal/tui/environ_windows.go`, `tui.go:1016`). Finding 31 says what that does to the
  capability section and why it is not a verdict on the post-item-5 painter.

**Finding 28 — the last-column wrap is DEFERRED in all three terminals, the two that ghost included.
H1 is falsified.** Each run was in its own window, hence the three different widths; the probe
measures against whatever width `term.GetSize` reports.

| Terminal | Ghosts? (row 1) | width | cursor after the final column (CPR) | console API | probe verdict |
|---|---|---|---|---|---|
| conhost | **NO** | 120 | 6,120 | 6,120 | `OK` — pending wrap held |
| Windows Terminal | **YES** | 130 | 6,130 | 6,130 | `OK` — pending wrap held |
| VS Code (xterm.js) | **YES** | 148 | 6,149 | 6,148 | `MISMATCH` — "neither a pending wrap nor an immediate one" |

An immediate wrap has exactly one signature in this section, and the probe tests for it literally:
`afterX.Row == wrapRow+1 && afterX.Col == 1`, i.e. **`7,1`**. Not one of the six readings above is
`7,1`. Every one of them leaves the cursor on row 6. Item 4's ANSWER — "on the ConPTY path the wrap
is taken immediately, putting the cursor a row down and at column 0" — describes behaviour that no
measured terminal exhibits.

The load-bearing comparison is not any single row but the *pair*: conhost, which does not ghost, and
Windows Terminal, which does, return **byte-identical** wrap behaviour. A property that is the same
in the clean path and the broken path cannot be what separates them.

**Finding 29 — VS Code's `MISMATCH` is a reporting convention for the pending wrap, not a third
semantics.** Three things pin it:

- The width really is 148. `term.GetSize` said so, and the second row of the section confirms it
  independently: writing one more character put the cursor at `7,2`, which requires that character
  to have landed in row 7 column 1, which requires row 6 to have been full at column 148.
- `GetConsoleScreenBufferInfo` reports `6,148` — the deferred model exactly, the same answer conhost
  and Windows Terminal give.
- `6,149` is that same state reported as *one past the last column*, which is how an emulator that
  models a pending wrap as an `x == width` cursor reports its position. It is a different spelling
  of "pending", not a different behaviour: an emulator that had already taken the wrap would say
  `7,1`.

What is genuinely new here is the **disagreement between the two readings**, which appears on the VS
Code path and on neither of the others. CPR and the console buffer are not being answered by the same
component there — the reply the application receives comes from somewhere that counts the phantom
column, while the buffer behind it does not. It is a one-column, same-row difference and so cannot
produce the row error the ghost's artifact needs, which is why it is recorded rather than pursued.
It is also a caution for any future probe row that trusts CPR alone on a ConPTY path.

**Finding 30 — tab stops are every 8 in all three terminals, DECST8C moves nothing, and a tab erases
nothing.** All three `hard tabs` sections read `OK`, with identical observed stops
`9 17 25 33 41 49 57 65` before and after DECST8C, `no change` from DECST8C itself, and
`moved over the cells, left them intact` for the erase test. Folded in with this pass, a correction
to how H3 was written down: the DECST8C emission had been credited to the wrong layer.
**ultraviolet's renderer-level emission is commented out** upstream behind a TODO
(`terminal_renderer.go:1200-1205`), but **bubbletea still sends DECST8C** — once before the first
frame whenever hard tabs are on (`cursed_renderer.go:282-284`) — and hard tabs are on
unconditionally on Windows (`termios_windows.go:9`). apogee takes bubbletea's default renderer
(`internal/tui/tui.go:1023`, no `WithRenderer` anywhere in the tree), so every Windows run asks for
every-8 stops before its first frame; the stops were already there, so the request changes nothing.
H3 stays falsified (finding 13) and now has a direct measurement behind it as well as an A/B. The
layer mix-up is corrected in the plan's H3 bullet and in `internal/probe/terminal.go`
(`measureTabs`, `everyEightStops`, and the DECST8C row's "renderer assumes" cell, which printed it
to the user).

**Finding 31 — the capability `MISMATCH` in all three reports is the probe reading the process
environment, and it is evidence *for* item 5 rather than against it.** All three terminals answer
`yes` to CHA, VPA, ECH, ICH and REP, and all three reports print `xtermCaps(TERM=(unset))` saying
`no` to each. Read carefully, that says two separate things:

- **It is the direct real-terminal evidence finding 3 never had.** conhost, Windows Terminal and VS
  Code all support the five sequences an empty `TERM` denies the painter. That is exactly the premise
  item 5 was built on, now measured in the three terminals rather than argued from `xtermCaps`.
- **It is not a measurement of the post-item-5 painter, and it cannot become one.** The probe is a
  separate subcommand that never builds a tea program, so item 5's injection does not reach it, and
  item 5 correctly refuses to mutate the process environment. Item 5's acceptance row *"`apogee probe
  terminal` reports the capability section as `OK`"* is therefore **unsatisfiable as written** on
  Windows: the probe will print `MISMATCH` there for as long as it names the terminal from
  `os.Getenv("TERM")`. Making it agree with the painter means giving the probe the same naming rule
  the painter got — a real change with a real design question in it (report what the process sees,
  what the painter sees, or both), and it belongs to whoever revisits items 3 and 5, not here.

**What none of the three sections does is separate the ghost from the clean path.** Tabs: identical
`OK` in all three. Wrap: identical `OK` in conhost and Windows Terminal. Capabilities: identical
`MISMATCH` in all three, for a reason that is the same in all three. The probe was built to
discriminate and it has come back with no discriminating row at all — which is itself the finding,
because it retires the last hypothesis on the H1-H4 list that was still standing as a cause.

## Item 6 findings — the gate, and the mode-2026 A/B (2026-08-07)

**Finding 32 — THE GATE, run by the owner: the post-item-5 build STILL GHOSTS in Windows Terminal,
and the default activity spinner ghosts with it.** Item 4 row 1, stock binary, no debug kit, no
flags — the run the previous pass named as the condition on everything else. Two things come out of
it, and the second was not previously on record anywhere in this plan:

- **Item 5 is not the fix.** Finding 23 predicted this on mechanism (item 5 removes the amplifier of
  a *column* error; the artifact needs a *row* error) and the gate confirms it by eye. Item 5 keeps
  its own justification (finding 22) and loses nothing else.
- **The spinner ghosts, and that is a hard constraint on every surviving hypothesis.** The default
  activity spinner is a **small, localised, low-content repaint**: a couple of cells inside the
  status line, no long lines, no wrapped rows, no scroll, no streaming volume. Any explanation that
  needs long lines, full-width writes, the last column, or a heavy write count **cannot be right**,
  because the ghost appears without any of them. Read together with findings 19 and 28 — apogee's
  bytes are correct for the deferred semantics the ghosting terminals were measured to have — what
  survives is a fault that is **per-frame rather than per-content**, and that is triggered
  downstream of apogee's byte stream. The remaining candidates have to be properties every frame
  carries, however small the frame is.

**Finding 33 — the sixth kit flag exists and works, and the A/B varies exactly two things at
apogee's layer.** `APOGEE_DBG_NO_SYNC=1` was added to `bubbletea-dbg/` in the shape of the other
five: one line in the `zz_apogee_debug.go` header table, one guard in `tea.go`'s
`case ansi.ModeSynchronizedOutput:` that logs and `break`s before `setSyncdUpdates(true)`, mirroring
`APOGEE_DBG_NO_GRAPHEME`'s guard in the sibling `case`. `apogee-dbg.exe` was rebuilt from this
commit (post-item-5), not from `25ea817`. Both arms ran in `conptyrun` at 120×30 against
`--resume …155434Z-b82d2800.json` with an identical scripted keyboard. The system pseudoconsole
answers `not recognized (0)` to `DECRQM 2026` (finding 27), so the harness **injects the reply**
`CSI ?2026;2$y` on the child's input three seconds in; the `APOGEE_DBG_LOG` confirms bubbletea
received it as `value=2` in both arms, and honoured it in one:

| Run | mode-2026 report | writes | `?2026h` | `?2026l` | `?25l` | `?25h` |
|---|---|---|---|---|---|---|
| A — flag unset | `value=2` → `setSyncdUpdates(true)` | 6 | 2 | 2 | 1 | 2 |
| B — `APOGEE_DBG_NO_SYNC=1` | `value=2` → `IGNORED` | 6 | **0** | **0** | 3 | 4 |

Diffing the two traces write-for-write, the entire difference is one substitution on the two
steady-state frames, with the frame bytes between the wrappers **byte-identical**:

```
A: "\x1b[?2026h\x1b[48;2;0;0;0mhi\x1b[58X\x1b[m\x1b[?2026l"
B: "\x1b[?25l\x1b[48;2;0;0;0mhi\x1b[58X\x1b[m\x1b[?25h"
```

So the A/B is not a one-variable test at apogee's layer. It swaps an atomic-frame hint for cursor
hiding, because `cursed_renderer.go:526-556` treats the two as alternatives: with synchronized
updates on, `shouldUpdateCursorVis` is false in steady state and **the cursor is never hidden while
the renderer repositions it and writes cells**; with them off, every frame that has updates and a
visible cursor is wrapped in `CSI ?25l` … `CSI ?25h`. That second half is a per-path divergence in
its own right — **Windows Terminal paints with the cursor live and conhost does not** — which
finding 18 recorded as a one-clause aside ("it falls back to `CSI ?25l` / `CSI ?25h` instead")
without drawing out that it is a second behavioural difference riding the same switch. Finding 34 is
what stops it from spoiling the experiment.

**Finding 34 — ConPTY does not preserve the synchronized window. It forwards `BSU`/`ESU` as an
immediately-closed EMPTY pair and re-serializes the frame's cells outside it.** Both arms' captures
are the pseudoconsole's re-serialized output — what a downstream emulator actually receives. They
are **9260 and 9228 bytes**, and deleting the literal string `ESC[?2026h ESC[?2026l` from the first
makes the two **byte-identical**: 2 pairs, 16 bytes each, 32 bytes, nothing else. The order is
visible in the bytes (spaces added between the sequences for readability; there are none in the
capture):

```
… \x1b[27;3H \x1b[?25h \x1b[?2026h \x1b[?2026l \x1b[m\x1b[48;2;0;0;0mhi\x1b[58X\x1b[m \x1b[?2026h \x1b[?2026l \x1b[?25l …
      (1)        (2)        (3)         (4)                    (5)                        (6)         (7)        (8)
```

(3) and (4) are apogee's `BSU`/`ESU` for the frame whose cells are (5) — forwarded back to back with
nothing between them, and closed **before** the content they were meant to enclose is re-serialized.
(6) and (7) are the same thing for the next frame, whose cells follow (8). ConPTY passes the mode
changes through as it parses them and defers the buffer diff to its own render pass, so the window
is *always* empty; this is not a timing accident of one run but the same shape in both pairs and
both arms.

Three consequences, in ascending order of importance:

- **The A/B is single-variable downstream after all.** ConPTY emits its own `?25l`/`?25h` around its
  own re-serialized frames, **identically in both arms** (7 and 7). apogee's cursor-hiding difference
  never reaches the emulator; ConPTY normalises it away. So the seventh flag that finding 33 seemed
  to demand is unnecessary, and the owner's A/B tests one thing: the empty pairs.
- **Synchronized output does nothing for apogee on Windows except cost it the flicker mitigation.**
  The window is always empty, so the frame is never presented atomically. bubbletea gave up
  `?25l`/`?25h` in exchange for atomicity it does not receive on this path. That is a defect worth
  writing down whatever causes the ghost.
- **It supplied a candidate mechanism that fit finding 32's spinner — and the candidate is now dead.**
  **FALSIFIED (2026-08-07) by finding 39:** the owner's A/B ghosted in both arms, so the degenerate
  synchronized window is not the ghost. The paragraph below is left as written because the *shape*
  argument in it was sound and is what motivated the A/B; only its conclusion was wrong. The real
  cause has the same per-frame shape and is finding 45. What the emulator receives,
  per frame, is `ESU` — a *present* — immediately **before** that frame's own cells, and nothing
  forcing a present after them. That is a per-frame property, carried by a two-cell spinner
  tick exactly as much as by a 1125-write streaming turn, which is the shape finding 32 says the
  cause must have. It is a **candidate, not a conclusion**: whether Windows Terminal mishandles a
  degenerate window is exactly what the owner's A/B measures, and this capture is from the *system*
  pseudoconsole, not from the `OpenConsole.exe` Windows Terminal ships (finding 27's caveat 1 still
  stands, and the collapse-to-empty-pair behaviour is structural to ConPTY's re-serializing design
  rather than specific to one build, but that is reasoning, not a measurement).

**Finding 35 — with mode 2026 genuinely negotiated, the headless harness still does not reproduce,
and that is NOT an exoneration of mode 2026.** (Mode 2026 *was* exonerated, one pass later and by the
owner's A/B rather than by this: see finding 39. The caution below stays correct about what a
`tracereplay` screen can and cannot show.) The DECRPM injection of finding 33 removes one of the
three gaps finding 27 listed between the harness and the ghosting path — caveat 2, "nothing
negotiated modes 2026 or 2027" — at least for 2026. Rendering both arms' pseudoconsole output
against the screen apogee intended (`bin2trace` → `tracereplay`, truncated at the alt-screen exit,
120×30, `-wrap deferred`) gives **four identical screens**: arm A intended, arm A re-serialized, arm
B intended, arm B re-serialized, differing only in the header line naming the file.

Two reasons that does not clear the hypothesis, and both matter:

- **`tracereplay`'s VT model ignores mode 2026 entirely**, which finding 18 already said. A candidate
  whose mechanism is "the emulator presents at the wrong moment" is invisible to a model that has no
  notion of presentation. The four identical screens say the *cells* agree; the ghost is about which
  cells are *shown*.
- It is still the system pseudoconsole, not `OpenConsole.exe` (finding 27, caveat 1).

What finding 35 does establish is narrower and still useful: enabling synchronized output changes
neither apogee's intended screen nor ConPTY's re-serialized buffer. Whatever mode 2026 does on the
ghosting path, it does not do it by changing which cells get written — exactly as finding 18 argued,
now measured rather than asserted, with the empty-window mechanism of finding 34 as the reason.

Captures for both arms are in `C:\Users\airic\apogee-ghosting-debug\item6b-evidence\`
(`sync{on,off}-{trace.txt,conpty.bin,dbg.log,diag.txt}` plus the rendered `*-screen.txt`).

## Item 6 findings — the mode-2026 filter lands (2026-08-07)

**Finding 36 — apogee no longer asks Windows terminals about synchronized output, and the reason is
finding 34, not the ghost.** `internal/tui/syncoutput.go` adds `syncQueryStripper`, a `term.File`
that removes every occurrence of `CSI ?2026$p` from what bubbletea writes and passes everything else
through byte for byte. It is installed by `programDeclinesSyncOutput()` —
`stdoutIsTerminal()` on Windows (`syncoutput_windows.go`), `false` everywhere else
(`syncoutput_other.go`) — through the `programOptions` seam item 2 built.

The mechanism is a byte filter because bubbletea offers nothing else: `options.go` has no
`WithSynchronizedOutput`, `setSyncdUpdates` is unexported, `shouldQuerySynchronizedOutput` is
private, and this plan does not carry a fork. Remove the question and the answer never comes, so
`tea.go`'s `case ansi.ModeSynchronizedOutput:` is never reached and `setSyncdUpdates(true)` is never
called. Three properties make that safe rather than clever, and each is pinned by a test:

- **The mode-2027 question survives.** bubbletea emits both DECRQM requests as one string through a
  single `execute` (`tea.go:1109-1114`) that a single `flush` writes (`tea.go:1221-1237`), so the two
  can only be separated at the byte level. 2027 must live: `widthAuthority` exists to mirror
  bubbletea's reaction to that report (`width.go:73`), and silencing one side alone would desync the
  layout's measure from the painter's.
- **`CSI ?2026h` / `CSI ?2026l` are not touched.** The BSU/ESU frame wrappers differ from the request
  by their last byte. With the mode never enabled the renderer never emits them anyway, but a filter
  that ate them would corrupt any frame that carried them.
- **A probe split across two writes is still removed.** bubbletea cannot split it today — one
  `execute`, one `flush`, one `Write` — so this is belt and braces, and it is implemented as a carry
  of the trailing bytes that form a *proper prefix* of the request. Holding those back can lose
  nothing: every proper prefix of `ESC [ ? 2 0 2 6 $` is an incomplete escape sequence, so a write
  ending in one is by construction a write that stopped mid-sequence and the remainder must follow.
  `TestSyncQueryStripperReleasesAHeldPrefixTheNextWriteRefutes` pins the release path with
  `CSI ?25l` arriving in two pieces, which is the sharpest near-miss in the stream.

**What the change is claimed to buy, and it is finding 34 verbatim.** On Windows the synchronized
window is forwarded EMPTY — ConPTY closes it before re-serializing the frame's cells — so apogee has
been trading bubbletea's per-frame `CSI ?25l` / `CSI ?25h` flicker mitigation (finding 33) for an
atomicity it never receives. Declining the question puts the renderer in the same configuration
conhost already runs in, which is the one measured Windows path that has never ghosted (finding 12).
**No claim is made that this fixes the ghosting.** The ghost is unexplained, the visual A/B has not
been run, and this finding does not anticipate it.

**Finding 37 — the composition with items 2 and 5 is verified, not assumed.** Three seams now meet in
`programOptions`, and the interactions were the risk:

- **Stacking order (item 2).** `programOutput` builds bubbletea → stripper → tracer → `os.Stdout`,
  with the stripper NEAREST bubbletea, so `--tui-trace` records the bytes that actually reach the
  terminal rather than the ones bubbletea offered. That direction is load-bearing for this
  investigation specifically: findings 33-34 are diffs of a trace against a pseudoconsole capture,
  and a trace that disagreed with the wire would make every such comparison lie.
  `TestSyncQueryStripperOverTracedOutputTracesTheStrippedStream` asserts the trace and the terminal
  receive the same stripped bytes.
- **`term.File` in full, again.** Item 2's NOTES record that a plain `io.Writer` here silently
  disables raw mode, VT processing, size detection and the cursor optimizations. The same applies to
  a second wrapper stacked above the first, so `syncQueryStripper` carries the same
  `var _ term.File = ...` assertion and answers `Fd()` with the wrapped terminal's descriptor —
  `os.Stdout`'s however many layers are stacked. `TestSyncQueryStripperIsATermFileOverStdout` is the
  pin, and it is item 2's test repeated deliberately.
- **Independence from item 5 (`programEnviron`).** The two rules are separate `tea.ProgramOption`s
  that share only the function that appends them. A Windows terminal run switches both on:
  `TestProgramOptionsComposeTheEnvironmentWithTheStripper` asserts three options where item 2's guard
  test still asserts one with everything off.
- **Off-switch preserved.** `TestProgramOptionsInstallNoTraceWhenTheFlagIsUnset` — item 2's guard
  against an always-on wrapper — still asserts exactly one option, now with the new parameter false.

**Finding 38 — the change is layout-neutral by construction, and its blast radius is one substring.**
Nothing in apogee reads a mode-2026 report: `widthAuthority.observe` returns unchanged for any mode
other than 2027 (`width.go:74`), and the only other reader is the `--tui-diag` log, which will simply
have no 2026 line to record on Windows. No glyph, no width, no column budget and no frame content is
touched — the only bytes that change are the nine removed from one startup write, plus the
`CSI ?25l` / `CSI ?25h` pairs bubbletea's own renderer resumes emitting as a consequence. Verified on
this tree: `go build ./...`, `go vet` under `GOOS=windows|linux|darwin`, the six-target cross-build,
and `go test ./internal/tui/ ./cmd/apogee/ -count=1` all green apart from the Windows host's
pre-existing environment failures, which were measured at `ebf955a` before the change and are
unchanged by it.

## Item 6 — WHERE IT STOOD before the diagnosis closed (2026-08-07, SUPERSEDED)

> **Superseded on 2026-08-07 by findings 39-47 and by "Item 6 — THE ANSWER" at the end of this
> file.** Two claims below are now known to be wrong and are left in place only so the reasoning
> that produced them is legible: mode 2026 is **not** a candidate cause (the owner's A/B ghosted in
> both arms — finding 39), and the ranked list's entry (1) has been run and did not merely
> "discriminate" — it reproduced the bug and identified it. Read the closing section, not this one.

**The design call still cannot be answered, but the question has narrowed twice more.** The gate is
no longer outstanding: the post-item-5 build ghosts, and the spinner ghosts with it (finding 32). The
branch item 4 selected stays *disproven* — the wrap is deferred on the paths that ghost (finding 28),
so neither an `ultraviolet` issue nor the one-column-short mitigation has a measured defect to point
at. What is new is that finding 32's spinner rules out every remaining "long lines / full-width rows /
heavy streaming" story, and findings 33-34 have loaded the one candidate that has the right shape
into a switch the owner can flip in one command. On the fourth pass the owner chose to **land that
switch permanently either way** — see findings 36-38 — so what the A/B now decides is not whether
apogee keeps mode 2026 on Windows (it does not) but whether losing it explains the ghost.

What is established, after four passes:

- Item 5 is measured working and measured layout-neutral (finding 22). It is not a fix (finding 23).
- The cheapest layout-free mitigation is eliminated on mechanism (finding 25).
- **apogee's emitted stream is correct for the semantics the ghosting terminals actually have.**
  Finding 19 replayed the traces clean under `deferred`; finding 28 measures the terminals as
  deferred. Those two together say the fault is introduced **downstream of apogee's byte stream** —
  which is the single most useful thing this pass produced, because it moves the search out of the
  renderer entirely.
- Finding 24 stands as a correct reading of ultraviolet's source (`wrapCursor` emits nothing and
  assumes `am`), and it is still the right description of a *latent* fragility. It is not a defect
  report, because every terminal measured has `am` and honours it the way the renderer assumes.
- Finding 20's `immediate` replay keeps its value as the *fingerprint* of the fault class — a
  one-row cursor error produces exactly the observed artifact — and loses its value as an
  identification of the cause. Something downstream is producing that row error by another route.
- **The gate is answered and the fault is per-frame, not per-content** (finding 32). A two-cell
  spinner tick ghosts. That retires, permanently, every candidate whose mechanism needs a long line,
  a full-width row, the last column, or write volume.
- **Synchronized output is measured to be a pure loss on Windows** (finding 34), independent of
  whether it is the ghost: ConPTY collapses the window to an empty pair, so apogee trades
  bubbletea's cursor-hide flicker mitigation for an atomicity it never receives.
- **That loss is now fixed in the shipping build** (findings 36-38): apogee strips the mode-2026
  DECRQM request on Windows, so bubbletea never enables the mode and the renderer keeps its
  `CSI ?25l` / `CSI ?25h` flicker mitigation. Layout-neutral, `TERM`-independent, and justified by
  finding 34 alone. **It is not a claim about the ghost.**

**The one thing left to run, and it is one command.** Everything an agent can do for the mode-2026
A/B is done: the flag exists, `apogee-dbg.exe` is rebuilt from this commit, the mechanics are
verified in both arms (finding 33), and the confound the A/B seemed to carry is measured away
(finding 34). What remains is the half that needs an eye:

> **Reproduce item 4 row 1 in Windows Terminal twice with
> `C:\Users\airic\apogee-ghosting-debug\apogee-dbg.exe`** (rebuilt 2026-08-07 from commit `efec920`,
> i.e. post-item-5) — one streaming turn each, watching the **activity spinner** as much as the text,
> since finding 32 makes the spinner the sharper symptom:
>
> ```powershell
> # arm A — stock behaviour, expected to ghost
> C:\Users\airic\apogee-ghosting-debug\apogee-dbg.exe
>
> # arm B — same binary, synchronized output declined
> $env:APOGEE_DBG_NO_SYNC = '1'; C:\Users\airic\apogee-ghosting-debug\apogee-dbg.exe
> Remove-Item Env:\APOGEE_DBG_NO_SYNC
> ```
>
> **Use the kit binary, not a fresh build.** `apogee-dbg.exe` was built from `efec920`, which
> predates the mode-2026 filter, so arm A still enables the mode and the A/B still has two arms. A
> stock build from `HEAD` is now permanently in arm B's configuration on Windows and cannot serve as
> arm A. If `apogee-dbg.exe` is ever rebuilt from a later commit, `APOGEE_DBG_NO_SYNC` becomes a
> no-op and the experiment silently loses its control.
>
> **What each outcome would tell us**, now that the fix is landed either way (findings 36-38):
>
> - **arm A ghosts and arm B is clean** — the diagnosis is closed. The empty synchronized window is
>   the ghost; the change already in the tree is the fix as well as the correctness cleanup it was
>   landed as, and item 7's regression test finally has a case that can be seen red (run arm A).
> - **both ghost** — mode 2026 is eliminated as the cause. The landed change keeps its own
>   justification (finding 34) and loses nothing, and the ranked list below drops to its first entry.
> - **both are clean** — the debug binary is not reproducing, which is its own (unwelcome) finding.
>   Re-run arm A until it ghosts before trusting arm B: a negative A/B against a binary that cannot
>   reproduce says nothing at all.
>
> Add `APOGEE_DBG_LOG=<path>` to either arm to confirm which way the mode-2026 report was handled;
> both arms should log a report, and only arm B should log `IGNORED`.

**The fix is landed, ahead of the A/B and on the owner's instruction (findings 36-38).** apogee no
longer lets bubbletea enable synchronized output on Windows. bubbletea has no option for it
(`options.go` has no `WithSynchronizedOutput`; `setSyncdUpdates` is unexported;
`shouldQuerySynchronizedOutput` is private), and this plan does not carry a fork, so the seam is the
one item 2 already built: the `term.File`-preserving `tea.WithOutput` wrapper. bubbletea emits the
two `DECRQM` probes in a single write (`tea.go:1109-1114` at the pinned v2.0.8,
`RequestModeSynchronizedOutput + RequestModeUnicodeCore`); the wrapper
(`internal/tui/syncoutput.go`) drops the `CSI ?2026$p` substring from that write, leaves the 2027
negotiation intact, never produces a `ModeReportMsg` for 2026, and lands the renderer in exactly the
`?25l`/`?25h` configuration conhost already runs in and does not ghost. No layout changes, no glyph
changes, no column budget touched. **Its justification is finding 34 — dead weight paid for with a
real mitigation — and not a claim about the ghost.** Two upstream write-ups would follow a positive
A/B — a `charmbracelet/bubbletea` request for a public option so the wrapper can be retired, and a
`microsoft/terminal` report of the empty-window re-serialization with finding 34's capture as its
whole content — both as documents in this repo, not filed (see below).

**If arm B still ghosts, the ranked next measurements.** These are the candidates that survive
findings 28-38, in the order their cost/discrimination ratio favours. **None of them is funded**; (1)
in particular is the one the last three passes have kept pointing at and the one nobody has paid for
yet:

1. **ConPTY's re-serialization, measured on the ConPTY that actually ghosts.** Finding 19 plus
   finding 28 put the fault downstream of apogee's bytes, and finding 27 has already cleared the
   *system* pseudoconsole — but Windows Terminal ships its own `OpenConsole.exe` and VS Code drives
   node-pty, so neither of the ghosting paths has ever had its re-emitted stream captured. That
   capture is both the discriminator and, if it shows corruption, the entire content of a
   `microsoft/terminal` issue. `conptyrun` supplies the mechanics; pointing it at WT's
   `OpenConsole.exe` is the work. Finding 34 sharpens what to look for: does OpenConsole collapse
   the synchronized window the same way the system pseudoconsole does, and does its re-serialized
   diff for a *spinner-sized* frame differ from apogee's intended one?
2. **The remaining per-frame properties of a small repaint.** Finding 32 says the cause rides on
   every frame. After mode 2026, the per-frame constants left in the stream are the SGR reset/re-set
   pairs, the `CSI <n> X` (ECH) erases item 5 introduced (40 of them — new since the pre-item-5
   stream, and the ghost predates item 5, so this is a weak candidate but a cheap one: run the gate
   against `TERM=vt100`, which declines item 5's injection, and see whether the ghost changes shape),
   the relative cursor moves bubbletea's hard tabs and backspaces produce (already A/B'd clean,
   findings 15-16, but never with the spinner as the thing being watched), and — new since finding 36
   — the `CSI ?25l` / `CSI ?25h` pair the renderer resumed emitting around every frame once the mode
   was declined. That last one is only a candidate if arm A and arm B *both* ghost, since arm B is
   already running it.
3. **The downstream emulator's consumption of that re-emission.** If (1) shows ConPTY re-emits a
   correct stream, what remains is Windows Terminal and xterm.js both mishandling it — two
   independent codebases, so the shared input is the suspect.

1 is the plan's standing option (b) and it is where the evidence points hardest: findings 19, 28 and
32 together say the fault enters downstream of apogee's bytes, is per-frame rather than per-content,
and has never once been observed in a capture of a ghosting path — because no such capture exists.
Every ConPTY byte in this plan came from the *system* pseudoconsole, which does not ghost.

**Nothing has been filed upstream, against anyone, and nothing should be until arm B is run.**
Against `ultraviolet`, the one direct measurement contradicts the claim (finding 28). Against
`microsoft/terminal`, finding 34 is now a real, reproducible, byte-level observation — the
synchronized-output window is forwarded empty and the frame is re-serialized outside it — so unlike
last pass there *is* something honest to write. But it is measured on the system pseudoconsole and
its consequence is still unproven, so the report waits for arm B and, ideally, for next-step (1). If
one is written, it lands as a document in this repo first; this plan does not file to external
services on its own authority.

**A warning item 7 should read before it is started.** Item 7 specifies exactly the harness built
here — `CreatePseudoConsole`, a full-width repro, read the buffer back — and finding 27 says that
harness **paints correctly on a pre-item-5 build**, which is the one thing item 7's own text forbids
("add the failing case first and watch it fail … a regression test that was never seen red is not a
regression test"). Whatever item 7 becomes, it cannot be written against the system pseudoconsole
until something makes the bug appear there. `conptyrun` is reusable for the mechanics and saves the
Win32 setup cost; it is the *premise* that needs re-checking, not the plumbing. Finding 28 sharpens
this rather than softening it: the system pseudoconsole's wrap behaviour is now known to match the
real terminals', so the gap between the harness and the ghosting path is *not* the wrap — the
premise gap is still there and still unlocated. Finding 35 closes one more of finding 27's three
gaps (mode 2026 can now be negotiated in the harness, by injecting the `DECRPM` reply on the child's
input) and the harness *still* paints clean, so the premise gap is narrower and no closer to being
found. Note also finding 32: whatever item 7 eventually asserts on, a **spinner-sized** repaint is
now known to ghost, so the harness does not need the full-width streaming repro its text specifies —
the smaller case is the sharper one.

**Not written, deliberately:** the one-column-short mitigation, and any other user-visible layout
change — the owner's decision withholds authorization for both, and finding 28 has now removed the
defect they were meant to work around, so they are unmotivated as well as unauthorized. Also not
written: any upstream issue text, for the reason two paragraphs up; and a seventh kit flag to
separate the cursor-hiding half of the A/B, because finding 34 measures ConPTY removing that half
before any emulator sees it. The mode-2026 fix is no longer on this list — the owner's "land it
either way" call put it in the tree on finding 34's evidence (findings 36-38), ahead of the A/B and
without borrowing the A/B's conclusion.

## Item 6 findings — the ghosting path is captured, and the cause is found (2026-08-07)

**Finding 39 — THE OWNER'S A/B: mode 2026 is NOT the cause.** The owner ran item 4 row 1 in Windows
Terminal against a build of `244b2c7` — the commit that declines synchronized output on Windows —
and **the ghosting persists: both the default activity spinner and streaming output still ghost with
synchronized output declined.** That is the "both ghost" branch the fifth pass wrote down in advance.
Two consequences, exactly as that branch specified:

- **Mode 2026 is eliminated.** The empty-window mechanism of finding 34 is real but it is not the
  ghost. Every sentence in this plan that treated it as a candidate *cause* is withdrawn; the
  superseded section above is retitled rather than rewritten so the reasoning stays legible.
- **`244b2c7` stands and is not reverted.** Its justification was never the ghost — it is finding 34
  alone: on Windows the synchronized window is forwarded empty, so apogee was paying bubbletea's
  `CSI ?25l` / `CSI ?25h` flicker mitigation for an atomicity it never received. That measurement is
  untouched by this result.

**Finding 40 — the instrument: a pseudoconsole hosted by a console host of our choosing.**
`kernel32!CreatePseudoConsole` always launches the *system* `conhost.exe --headless`, which is why
every ConPTY byte in this plan up to now came from the one path that has never ghosted (finding 27,
caveat 1). `ocrun` (**`C:\Users\airic\apogee-ghosting-debug\ocrun\`**, outside the repo) reimplements
what `winconpty.cpp` does inside `CreatePseudoConsole` — `NtCreateFile` on `\Device\ConDrv\Server`,
a signal pipe, `CreateProcess` of the host as `--headless --width W --height H --signal 0xS --server
0xV`, `\Reference` relative to the server handle, then a hand-built `PseudoConsole` struct passed to
the child as `PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE` — with the **host binary as a parameter**. Windows
Terminal's and VS Code's own hosts were extracted to
**`C:\Users\airic\apogee-ghosting-debug\hosts\`** as `OpenConsole-wt-1.24.exe` and
`OpenConsole-vscode-1.25.exe`. A second scratch tool, `tracefeed\`, replays a recorded `--tui-trace`
into such a pseudoconsole preserving the original write boundaries, so the *same bytes* can be put
through different hosts and different console configurations — which is what turns every comparison
below into a single-variable one.

**Finding 41 — the first reproducing capture in this plan, on the first attempt.** apogee at
`244b2c7`, 120x30, `--resume ...155434Z-b82d2800.json`, a typed line, run through each host and its
re-emitted stream rendered against the screen apogee intended:

| Host | re-emitted stream vs apogee's intended screen |
|---|---|
| system `conhost.exe` | **exact match** |
| `OpenConsole-wt-1.24.exe` (Windows Terminal) | **row 10 differs** |
| `OpenConsole-vscode-1.25.exe` (VS Code) | **row 10 differs — identically** |

Row 10 should be blank. What the two OpenConsole hosts forward is 32 blank columns followed by a
left-truncated tail of an earlier line:

```
intended: 10|                                                                                    |
captured: 10|                                |irectly to the documented persistence in docs/design/.|
```

That is the handoff's symptom exactly, and finding 20's fingerprint exactly. It is **not** a timing
flake: the two hosts were separate live runs whose byte streams differ, and both produced the same
fragment on the same row. Captures are in
`C:\Users\airic\apogee-ghosting-debug\item6c-evidence\`.

**Finding 42 — the OpenConsole builds FORWARD apogee's stream; the system conhost RE-SERIALIZES it.**
Feeding one fixed trace through each host with `tracefeed` (identical bytes in, so any difference is
the host's): the two OpenConsole captures are apogee's own bytes plus a 39-byte host preamble, with
`CSI ?2027$p`, `CSI ?5W` and `CSI 97C` all intact; the system conhost's capture contains none of
them and is rebuilt from its buffer (split SGR, `CSI K` + CRLF runs, a title sequence). This is why
every earlier capture was clean *by construction* — a re-serializing host cannot forward a defect in
how the client's cursor moves, because it never forwards the client's cursor moves at all.

**Finding 43 — the transformation, isolated to one byte: the renderer's bare `LF` arrives as
`CR LF`.** Walking apogee's emitted stream against the Windows Terminal capture byte for byte, there
is exactly one class of divergence and no other — the alignment consumes both streams to the end
without an unexpected difference:

```
apogee : ... ESC[m  \n    ESC[38;2;74;74;74;48;2;0;0;0m  (box top)
capture: ... ESC[m  \r\n  ESC[38;2;74;74;74;48;2;0;0;0m  (box top)
```

**10 of the 16 bare LFs apogee wrote were rewritten; the other 6 were forwarded untouched.** The
distinction matters because the renderer *depends* on it: ultraviolet emits `\r\n` when it wants the
next row at column 1 and a bare `\n` when it wants the next row at the **same column**, and both
spellings are present in the same stream. `DISABLE_NEWLINE_AUTO_RETURN` is what makes the second
spelling mean what it says — which is precisely why bubbletea sets it (`tty_windows.go:48-52`).

The column arithmetic then produces the observed artifact exactly. The write that paints row 10 is:

```
...highly formalized agentic framework ESC[m . ESC[30X   \n   ESC[31C ESC[1K ...
```

Intended: erase 30 cells, drop one row **keeping column C**, move 31 right, then `EL1` erase
everything from the line start up to there. Delivered with `CR LF`: the row is entered at **column
1**, `CUF 31` reaches **column 32**, and the `EL1` erases columns 1-32 only — leaving column 33
onward holding whatever the previous frame left there. The measured artifact is 32 blanks followed by
stale cells. That is not a family resemblance; it is the same number.

**Finding 44 — the transformation is sufficient on its own, proved by substitution.** Taking
apogee's own trace and replacing every bare LF with CRLF — one transformation, nothing else — then
replaying it: the resulting screen is **identical to the Windows Terminal capture's screen**, and it
carries the row-10 ghost against the intended screen. Nothing about the host, the wrap model, mode
2026, mode 2027, hard tabs, glyph widths or write volume is needed to produce the bug; that one
substitution accounts for all of it. (`crlfify.py`, `lfmap.py` in the debug kit root.)

**Finding 45 — THE TRIGGER, and it is apogee's: the alternate screen is claimed BEFORE the console
mode is set.** A 2x2 through Windows Terminal's `OpenConsole`, identical input bytes in every cell,
varying only what the child does to its console before writing: `dnar` = set
`DISABLE_NEWLINE_AUTO_RETURN` as bubbletea does; `prealt` = write `CSI ?1049h` `CSI 3J` **before**
setting the mode, as `internal/tui/tui.go` does at `Run`.

| arm | LFs rewritten | rows differing from intended |
|---|---|---|
| `dnar=true, prealt=false` | **0** | **0 — clean** |
| `dnar=true, prealt=true` | **10** | **1 — the ghost** |
| `dnar=false, prealt=false` | 10 | 1 — the ghost |
| `dnar=true, prealt=true, mode set first` | **0** | **0 — clean** |

Row 2 is the finding. bubbletea *does* set the flag, and setting it is *still* ineffective, because
the console mode word is **per screen buffer**: by the time `initTerminal` runs, apogee has already
switched to the alternate buffer, so the flag lands on the buffer nobody writes to again and the live
alternate buffer keeps the translating default. Row 4 is the fix — set the flag on the primary buffer
*before* the switch and the alternate buffer inherits it.

**Finding 46 — the fix, measured on the shipping binary through the host that ghosts.** apogee at
`244b2c7` against a build of this tree, same scenario, same `OpenConsole-wt-1.24.exe`:

| build | bare LFs apogee wrote | bare LFs the host forwarded | rewritten |
|---|---|---|---|
| `244b2c7` (before) | 6 | 4 | **2** |
| this tree (after) | 6 | 7 (6 + the host preamble's own) | **0** |

The change is `internal/tui/altscreen_windows.go`: a `GetConsoleMode` / `SetConsoleMode` pair that
ORs `ENABLE_VIRTUAL_TERMINAL_PROCESSING | DISABLE_NEWLINE_AUTO_RETURN` onto stdout's existing mode,
called from `Run` immediately before `claimAltScreen`, with an `altscreen_other.go` no-op off
Windows. It is additive (nothing not named is changed), it cannot fail the run (a console that
refuses leaves apogee exactly where it was), and it is **layout-neutral by construction**: no glyph,
no width, no column budget, no frame content and no emitted byte of apogee's own changes. The early
claim itself is untouched, so the macOS Terminal.app scroll-bar behaviour `claimAltScreen` exists for
is unaffected.

**Finding 47 — what this retires, and why nothing goes upstream.** The cause satisfies every
constraint the surviving hypothesis had to meet, which is the strongest check available on it:

- **The spinner constraint (finding 32).** The defect rides every bare LF, so it rides every
  differential repaint however small. A two-cell spinner tick moves rows with `\n` exactly as a
  1125-write streaming turn does. No long line, no full-width row, no last column and no write volume
  is required — which is what finding 32 said the cause must look like, and what nothing else on the
  list managed.
- **The conhost/Windows Terminal discriminator (finding 12).** Both hosts corrupt identically once
  the flag is missing — the system conhost with `dnar=false` produces the *same* row-10 ghost — so
  the host was never the discriminator. What differs in the field is whether apogee's early switch is
  honoured at all: under a pseudoconsole VT processing is on from the start, so it is; in a classic
  console window it is off until bubbletea turns it on, so the early switch does nothing and the
  alternate buffer is entered later, by the renderer, after the mode is already set. **This last link
  is the one step reasoned rather than measured** — it needs a real console window, which the harness
  by definition is not — and the owner's confirmation run tests it directly.
- **apogee's bytes were always correct (findings 19, 28).** They were. The corruption is applied to
  them in transit by the console's own write path, which is exactly the "downstream of apogee's byte
  stream" conclusion those two findings forced, and it is why re-reading the renderer never found it.
- **H1-H4 are all irrelevant**, as findings 13, 14, 23, 28 and 31 had already concluded separately.

**Nothing is filed upstream and nothing should be.** The two write-ups the fifth pass parked are
**withdrawn, not pending**: `microsoft/terminal` has no defect here — a per-buffer console mode
behaving per-buffer is not a bug, and the LF translation is `ENABLE_PROCESSED_OUTPUT` doing its
documented job on a buffer nobody cleared it on — and `charmbracelet/bubbletea` sets the flag
correctly for a program that lets it own the alternate screen. The ordering that broke it is
apogee's, and apogee has fixed it. Finding 24's reading of `wrapCursor` remains a correct description
of a latent fragility upstream and remains not a defect report.

Evidence for this pass: `item6c-evidence\` (the reproducing capture, all three hosts),
`item6d-evidence\` (the fixed-bytes host comparison), `item6f-evidence\` (a live replication),
`item6g-evidence\` (the 2x2 and the fix arm), `item6h-evidence\` (before/after on real binaries), all
under `C:\Users\airic\apogee-ghosting-debug\`.

## Item 6 findings — the owner confirms, and the console mode is given back (2026-08-07)

**Finding 48 — the acceptance row is satisfied: the owner sees no ghost.** The owner built this tree
and ran it in **Windows Terminal**, the configuration item 4 row 1 recorded as ghosting and finding 32
recorded as *still* ghosting after item 5. Verbatim: *"the latest build is NOT corrupting the
streaming anymore. The spinner looks fine too! Scrolling is also working!"*

That closes the one thing finding 47 left open. All three symptoms named across the plan are gone
together — the streamed text (item 4 row 1), the **activity spinner** (finding 32's sharper symptom,
the one no other candidate could explain) and scrolling. It is the first build in this plan's history
to come back clean by eye, and it is the build carrying `prepareAltScreenConsole` and nothing else new.
The diagnosis of findings 40-47 is therefore **confirmed, not merely consistent**: the mechanism was
measured in the harness, and the fix it predicted works on the real terminal at full scale.

**Finding 49 — the fix leaked the console mode into the shell, and now restores it.** Reviewing the
change turned up a real defect in it, new with the change and independent of the ghosting:
`prepareAltScreenConsole` OR'd `ENABLE_VIRTUAL_TERMINAL_PROCESSING | DISABLE_NEWLINE_AUTO_RETURN`
onto the primary buffer and never put it back. bubbletea cannot cover that. It saves the output
console mode in `initInput` (`tty_windows.go:36-41`) and restores it in `restoreInput`
(`tty.go:47-51`), but it samples the mode **after** `Run` has already changed it — verified in the
pinned `charm.land/bubbletea/v2 v2.0.8` — so what it restores is apogee's mode, not the shell's. Its
save/restore is defeated by the same ordering the fix depends on.

The consequence is the mirror image of the bug just fixed: every Windows TUI session would hand the
shell back a console with newline-auto-return **disabled**, so the next program to write a bare `LF`
— any Go binary, apogee's own multi-line CLI output included — would staircase down the screen. The
ghost would be gone and a new one would be left behind it.

The remedy is the shape `cmd/apogee/probeterminal_windows.go:19-34` already uses for the identical
flag pair: `prepareAltScreenConsole` now returns a restore closure holding the mode word as it was
before apogee touched anything, and `Run` defers it **immediately after the call** — before
`claimAltScreen`, and so before the exit-alt-screen defer. Three properties come out of that one
placement, and all three are the point:

- **It runs on every exit path.** Registered before `claimAltScreen`, it covers that call's error
  return, the two later error returns, the normal return, and every panic, with one `defer`.
- **It runs *last*.** Defers are LIFO, so the exit-alt-screen sequence is written while the VT
  processing this enabled is still in force — restoring first could leave `CSI ?1049l` printed
  literally on a console that did not have VT processing on to begin with — and the mode is put back
  on the primary buffer the exit returned to, which is the buffer that was modified.
- **It runs after bubbletea's own restore**, which happens inside `program.Run()`. bubbletea puts
  back the mode it sampled; this then puts back the one it could not see.

The `_other.go` twin keeps the same signature and returns a no-op closure, so the six-target
cross-build is unaffected. The closure is never nil on any platform — `Run` defers it
unconditionally, and that contract is the one part of this a portable test can reach
(`TestPrepareAltScreenConsoleReturnsACallableRestore`). The console path itself still has no portable
seam: it is a two-call Win32 sequence whose only effect is in the console's own mode word, and a
`go test` run has no console handle to observe it on. It stays pinned by findings 43-46 instead.

## Item 6 — THE ANSWER

**The ghost is apogee's own start-up ordering, and the fix is in the tree.**

`internal/tui/tui.go`'s `Run` claims the alternate screen before bubbletea initialises the terminal.
On Windows the console mode word is per screen buffer, so bubbletea's `DISABLE_NEWLINE_AUTO_RETURN`
then lands on the primary buffer while every frame is written to the alternate one — which, without
that flag, rewrites the renderer's bare `LF` into `CR LF`. ultraviolet uses bare `LF` to mean *next
row, same column* and `\r\n` when it means *next row, column 1*; collapsing the two paints every such
row 1-based instead of column-relative, so the cells the renderer believes it has just overwritten
are never touched and survive as fragments of an earlier frame. Under a pseudoconsole — Windows
Terminal, VS Code — VT processing is on from the start, so the early switch is honoured and the bug
fires; in a classic conhost window it is not, so it does not.

The fix sets the flag on the primary buffer *before* the switch, so the alternate buffer inherits it:
`prepareAltScreenConsole` in `internal/tui/altscreen_windows.go`, a no-op in `altscreen_other.go`,
called from `Run` immediately before `claimAltScreen` and paired with a deferred restore that gives
the shell its own console mode back (finding 49). Layout-neutral, `TERM`-independent, additive to the
existing console mode, and it changes no byte apogee emits.

**Confirmed by the owner in Windows Terminal (finding 48).** The acceptance run this section used to
ask for has been performed on a build of this tree: no corruption in the streamed text, no ghost in
the activity spinner, and scrolling correct. Nothing about the diagnosis is outstanding.
