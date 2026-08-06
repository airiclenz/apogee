# Windows TUI ghosting — diagnosis and fix implementation plan

- **Goal:** apogee's TUI paints correctly on Windows. Today, fragments of earlier frames stay
  on screen, text is eaten mid-line, and only a terminal resize (a forced full repaint) cleans
  it up. macOS is unaffected. This plan lands a permanent diagnostic seam, takes the
  measurements that separate four live hypotheses, fixes the cause, and pins the fix with a
  regression test that runs headless on Windows CI.
- **Date:** 2026-08-06
- **Status:** in progress — items 1, 2 and 4 (the diagnosis) ✅ done 2026-08-06; items 3 and 5-8 open
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
- **H2 — `noCaps` makes any column error permanent (finding 3).** With no `CHA`/`HPA`, the
  renderer cannot re-anchor a column within a row; it counts relative moves from its own model.
  One bad advance poisons `curbuf` for that line, the diff renderer then believes those cells are
  already correct, and nothing repaints them until a full redraw. **This is the mechanism that
  explains "only a resize fixes it" no matter which of H1/H3/H4 starts the error** — and on its
  own it is enough to turn a rare glitch into the constant ghosting seen.
- **H3 — hard tabs (finding 4).** The renderer moves the cursor with `\t` against an assumed
  every-8 tab-stop model and emits `DECST8C` (`CSI ?5W`) at start to enforce it. If conhost
  ignores `DECST8C`, or fills tab-skipped cells rather than just moving over them, both eaten
  text and stale cells follow.
- **H4 — terminal-side width disagreement.** WT's `textMeasurement` and its font-fallback path
  may measure braille or ambiguous-width glyphs differently from the painter, even though the
  painter's own tables are self-consistent (finding 6).

H2 is a severity amplifier and a plausible primary cause; it is worth fixing on its own merits
regardless of which of H1/H3/H4 the measurements implicate.

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

## 3. A terminal measurement probe — `apogee probe terminal`

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

## 5. Give the painter a real capability set on Windows

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

## 6. DESIGN CALL — the root-cause fix

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
negotiated by `DECRQM` and differs per path, and unlike the other knobs the kit has no flag for it.
It is recorded because it was invisible to the whole hypothesis list, **but the replay below shows
it is not the cause**: mode 2026 is a batching hint that changes when cells are presented, not which
cells are written, and the stream is correct with or without it.

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
| **H1 — last-column / pending wrap** | **CONFIRMED — primary cause** | findings 19-20: the same trace replays clean under `deferred` and ghosts under `immediate`; first divergence at write 2, the full-width box rule |
| **H2 — `noCaps`** | **CONFIRMED as amplifier, ruled out as trigger** | finding 9 (same `noCaps` stream paints correctly in conhost); finding 20 (why it never recovers) |
| **H3 — hard tabs** | **RULED OUT** | finding 13 (row 3), finding 15 (C1), finding 16 (C2) |
| **H4 — width disagreement** | **RULED OUT** | finding 14 (row 4 + run B), finding 17 (row 5) |

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

**For item 6:** take the H1 branch as written — repro and issue against `charmbracelet/ultraviolet`
and possibly `microsoft/terminal`. The plan already flags that the local mitigation (keeping
apogee's rows one column short of the terminal width) changes layout and needs the owner's
agreement **before** it is written. Finding 19 is the artifact to attach to any upstream report: the
same bytes, clean under one wrap model and corrupt under the other.
