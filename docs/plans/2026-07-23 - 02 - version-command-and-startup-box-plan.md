# Plan — `/version` command, a single build-version source, and a one-time start-up box

**Date:** 2026-07-23. **Status: PLAN — not started.** Execute with `/implement-plan` in a fresh
session, forwarding skills: `coding-standards`. Three owner-requested additions:

1. **A `/version` command** in the TUI that prints the current version number into the chat area.
2. **A one-time start-up box** printed once in the chat area at launch, carrying a logo plus base
   session info (connected host, model, version). It uses the **same border characters as the
   prompt box** (`lipgloss.RoundedBorder()` — `╭ ╮ ╰ ╯ ─ │`) but **without the black background**.
3. Both need a version string, and today **there is none in the code** — only git tags. So the
   plan first establishes a single build-version source and threads it to the TUI's display seam.

Target look of the start-up box (a self-closing rounded card, no black fill, at the very top of
the transcript above the first message). The box needs to span over the complete allowed width. The logo characters need to be painted in white:

```
╭─────────────────────────────────────────────────────╮
│   ▀▀█▄ ████▄ ▄███▄ ▄████ ▄███▄ ▄███▄                │
│  ▄█▀██ ██ ██ ██ ██ ██ ██ ██▄█▀ ██▄█▀                │
│  ▀█▄██ ████▀ ▀███▀ ▀████ ▀█▄▄▄ ▀█▄▄▄                │
│       ██           ▄▄█▀                             │
│                                                     │
│  host     192.168.64.1:1111                         │
│  model    gpt-oss-20b                               │
│  version  v1.7.0                                    │
╰─────────────────────────────────────────────────────╯
```

## Where things stand (grounded, verified 2026-07-23)

**Version — nothing in the binary reports it.**
- Git tags run `v1.0.0 … v1.7.0`; `git describe --tags` = `v1.7.0-62-g04843f3`. The `CHANGELOG.md`
  has `## [Unreleased]` then `## [1.7.0] — 2026-07-21`.
- No `--version` flag, no `version` subcommand, no ldflags injection. `cmd/apogee/root.go`'s
  `newRootCommand` (line 108) declares only `--endpoint/--model/--mode/--workspace/--bypass/--resume/--config`
  (lines 172-184). `cmd/apogee/subcommands.go` registers only `newProbeCommand()`. The `Makefile`
  build target (line 36) is a bare `go build -o $(BINARY) $(PKG)` — no `-ldflags -X`.
- The only `"v1.0.0"` literal in code is the **MCP handshake identifier** (`internal/mcp/client.go:26-29`,
  `clientVersion`), unrelated to the app version and deliberately left alone here (see NOT-in-plan).

**Commands — a thin parse/route/dispatch layer, no `commands` package.**
- Parser registry: `knownCommands` at `internal/tui/command.go:52`
  (`{"clear", "new", "compact", "continue", "confine"}`). Any `/verb` not in this set is treated as
  an ordinary agent message (`matchCommand`, command.go:75).
- Autocomplete dropdown (a superset with summaries): `commandMenu` at `internal/tui/autocomplete.go:138`.
- Dispatch: `Model.runCommand` switch at `internal/tui/model.go:557`. The synchronous, note-printing
  template is `/clear` (model.go:573): mutate, `m.transcript.addNote(...)`, `m.layout()`, `return m, nil`
  (no `tea.Cmd` — it does no upstream work). `runCommand` already resets the input box and clears the
  autocomplete overlay at its top (model.go:548-549), so a new case need not.

**Host / model / version display seam.**
- `tui.Options` (`internal/tui/tui.go:84`) carries the binary-resolved display values —
  `Model`, `Endpoint`, `HostAlias`, `ContextWindow` — and is populated in `cmd/apogee/wire.go:265-272`.
  There is **no** `Version` field yet.
- The footer already renders host/model: `footerContent` (`internal/tui/model.go:1014-1019`) does
  `host := m.opts.HostAlias; if host == "" { host = m.opts.Endpoint }`, then joins
  `host ✦ displayModel(model) ✦ ctx`. `displayModel` (model.go:1054) strips a path/weight-file
  extension for display only. Reuse both so the box and footer never drift.

**Prompt box chrome and the render seam.**
- Prompt box style `inputBorder` (`internal/tui/theme.go:160-166`): `lipgloss.RoundedBorder()`,
  `BorderBottom(false)`, `BorderForeground(colDarkGray)`, `BorderBackground(colBlack)`,
  `Background(colBlack)`, `Padding(0, 1)`. The **black comes from `BorderBackground(colBlack)` +
  `Background(colBlack)`**; dropping both (and keeping `BorderBottom` on so the card self-closes) is
  exactly "same characters, no black background". Palette: `colDarkGray = #4a4a4a`,
  `colBlack = #000000`, `colFaint = #8a8a8a` (theme.go:26-29).
- Transcript = an append-only `entries []entry` (`internal/tui/transcript.go:23`). `entryKind`
  constants at transcript.go:36. `entry` carries per-kind facts (e.g. `presented presentedView`,
  transcript.go:52-61). `renderEntryLines` (`internal/tui/render.go:108-130`) is the per-kind
  render switch. **`entryPresented` / `renderPresentedBlock` (render.go:206) is the exact pattern to
  mirror: the entry holds facts, render.go composes the lines at the current width (ADR 0019 §2).**
- Startup seed point: `newModel` (`internal/tui/model.go:115-135`) builds the Model with an empty
  transcript; nothing is seeded today. `Init()` (model.go:154) only focuses the input. There is **no**
  existing welcome/banner (the only startup string is the pre-sizing placeholder `"apogee — starting…"`
  at model.go:836). Seeding one entry in `newModel` makes it `entries[0]` — rendered fresh at the live
  width on every `refreshViewport`, so it reflows on resize with no "already shown" guard needed, and
  it survives `/clear` (which resets the engine's memory but never touches the transcript scrollback,
  model.go:573-588).
- The logo art lives in the untracked repo-root file `logo.md` (the block-art wordmark "APOGEE",
  4 lines, ~37 cols wide).

### Decisions locked (recommended defaults — alternatives noted, do not re-litigate silently)

- **Version source is a new `internal/version` package** with a `var Version` overridable by
  `-ldflags -X` and a `String()` fallback chain (ldflags → `debug.ReadBuildInfo` → `"dev"`), so
  `apogee --version` is honest in a release build, a `go install`, AND a bare `go run`. The single
  string is threaded to the TUI via a new `Options.Version` (the same seam Model/Endpoint use), so
  the TUI never imports the version package and both `/version` and the box read one value.
- **`apogee --version` (CLI) is included** — it is one line on the same source (Cobra's `cmd.Version`)
  and is the standard way to expose a build version. It complements, and does not replace, the
  in-TUI `/version` the owner asked for.
- **The start-up box is a content-sized card**, not full window width — a left-aligned logo in a
  full-width box reads as mostly empty space. (Alternative: `.Width(m.width)` like the input box —
  a one-line change if the owner prefers it to span the window.)
- **The logo renders in the terminal's default foreground** (maximum legibility in light and dark
  themes); the border is `colDarkGray` to match the prompt box; the info labels are dim (`colFaint`).
  (Colouring the logo is a one-line style tweak later.)
- **Info rows are `host` / `model` / `version`**, label-aligned in a column, in that order — the
  three fields the owner named.

---

## 1. Single build-version source + `apogee --version` + `Options.Version` seam — ✅ DONE (2026-07-23)

**What:** Create `internal/version/version.go` (package `version`):
```go
// Version is the build version, injected at release-build time via
//   -ldflags "-X github.com/airiclenz/apogee/internal/version.Version=v1.7.0"
// It is empty in a plain `go build`/`go run`; String() then derives an honest value.
var Version = ""

// String returns the build version: the ldflags value if set, else the module version
// embedded by `go install pkg@tag`, else the short VCS revision embedded by `go build`
// in a git checkout, else "dev".
func String() string { ... debug.ReadBuildInfo() fallback ... }
```
Use `runtime/debug.ReadBuildInfo`: prefer `info.Main.Version` when it is set and not `"(devel)"`,
else the `vcs.revision` setting shortened to 7 hex chars (suffix `+dirty` when `vcs.modified == "true"`),
else `"dev"`. In the `Makefile` (line 8 area) add
`VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)` and change the
`build` (line 36) and `install` (line 46) targets to pass
`-ldflags "-X github.com/airiclenz/apogee/internal/version.Version=$(VERSION)"`. In
`cmd/apogee/root.go` `newRootCommand` (line 111 `cmd := &cobra.Command{`), set
`Version: version.String()` on the command struct (Cobra auto-adds the `--version` flag and template).
Add `Version string` to `tui.Options` (`internal/tui/tui.go:84`, documented like the sibling display
fields) and populate it in `cmd/apogee/wire.go` at the `tui.Options{...}` literal (lines 265-272) with
`Version: version.String()`.

**Tests:** `internal/version/version_test.go` — `String()` returns `Version` verbatim when the var is
set (set it in the test, restore with `t.Cleanup`), and a non-empty fallback (`"dev"` or a revision)
when it is empty. In `cmd/apogee/root_test.go`, assert `newRootCommand(...).Version` is non-empty (the
CLI `--version` is wired). In `cmd/apogee/wire_test.go`, assert the constructed `tui.Options.Version`
is non-empty (add to the existing Options-population assertions).

**Acceptance:** `go build ./... && go vet ./... && go test ./...` green. `go build -o /tmp/apg
-ldflags "-X github.com/airiclenz/apogee/internal/version.Version=v9.9.9-test" ./cmd/apogee &&
/tmp/apg --version` prints `v9.9.9-test`; `make build && ./apogee --version` prints the
`git describe` value. Commit: `feat(version): single build-version source, apogee --version, and Options seam`.

---

## 2. `/version` command in the TUI

**What:** (depends on item 1 for `Options.Version`.) Register the verb in the parser —
`internal/tui/command.go:52`:
```go
var knownCommands = []string{"clear", "new", "compact", "continue", "confine", "version"}
```
Add it to the autocomplete dropdown — `internal/tui/autocomplete.go:138`:
```go
{name: "version", summary: "show the apogee version"},
```
Add a dispatch case in the `switch parsed.command` in `Model.runCommand`
(`internal/tui/model.go:557`, alongside the synchronous `/clear` case), following the
note-printing template exactly:
```go
case "version":
    m.transcript.addNote("apogee " + m.opts.Version)
    m.layout()
    return m, nil
```
(No `tea.Cmd`; `runCommand` already reset the input and cleared the overlay at its top.) Update the
`runCommand` doc comment (model.go:536-546) to list `/version` among the synchronous, note-recording
verbs.

**Tests:** In `internal/tui/command_test.go`, add a table row: `parseInput("/version")` yields
`kind == kindCommand`, `command == "version"`, no error (and `"/version now"` still parses as the
command, surplus args ignored, mirroring the existing `/clear`-with-args rows). In
`internal/tui/model_test.go`, add a dispatch test: a Model built with `Options{Version: "v1.2.3"}`,
after routing `/version`, has a transcript `entryNote` whose text contains `"v1.2.3"` and stays
`stateIdle` with a nil worker cmd (assert against the transcript entries the way the existing
`/clear` dispatch test does). If `autocomplete_test.go` asserts the menu contents, extend it to
include `version`.

**Acceptance:** `go build ./... && go vet ./... && go test ./...` green. Commit:
`feat(tui): /version command`.

---

## 3. One-time start-up box (logo + host / model / version)

**What:** (depends on item 1 for `Options.Version`.)

*Logo asset.* Move the repo-root `logo.md` into the TUI package as `internal/tui/logo.txt` (it is
art, not markdown) and embed it: add `import _ "embed"` and
```go
//go:embed logo.txt
var apogeeLogo string
```
Trim a single trailing newline at use so the art has no blank last line. (Embedding preserves the
exact leading/trailing spaces the block art relies on.)

*Box style.* In `internal/tui/theme.go` add a `startupBorder lipgloss.Style` field (near
`inputBorder`, line 121) and build it in `newTheme` (near line 160) with the **same border
characters, no black background**:
```go
startupBorder: lipgloss.NewStyle().
    Border(lipgloss.RoundedBorder()).   // same glyphs as the prompt box: ╭ ╮ ╰ ╯ ─ │
    BorderForeground(colDarkGray).      // same border tone
    Padding(0, 1),                      // no Background / no BorderBackground → transparent, self-closing card
```
Reuse `th.noteText` (dim, `colFaint`) for the info labels.

*Transcript fact-entry.* In `internal/tui/transcript.go`: add `entryStartup` to the `entryKind`
constants (line 36); add a `startupView struct { Logo, Host, Model, Version string }` (next to
`presentedView`, line 70) and a `startup startupView` field on `entry` (line 52); add
`func (t *transcript) addStartup(v startupView)` that appends `entry{kind: entryStartup, startup: v}`.
Escape-strip the untrusted `Host`/`Model` values (they trace to config/CLI) with `stripEscapes`, as
`addPresented` does — defence in depth even though they are not model output.

*Renderer.* In `internal/tui/render.go` add `case entryStartup: return railLines(th,
renderStartupBox(th, e.startup, inner), e.depth)` to `renderEntryLines` (line 110), and implement
`renderStartupBox(th theme, v startupView, width int) []string`: build the inner content as the logo
lines, a blank line, then the three label-aligned info rows (`host` / `model` / `version`, labels
dim via `th.noteText`, values plain; pad labels to a common width); render it through
`th.startupBorder` and return `strings.Split(rendered, "\n")`. Do **not** set `.Width()` — the card
sizes to the logo (its widest line). (On a window narrower than the logo the viewport soft-wraps the
card; acceptable for a ~37-col wordmark. Note it in the doc comment.)

*Seam helper (DRY).* Extract `func hostDisplay(opts Options) string` returning
`opts.HostAlias` or, when empty, `opts.Endpoint`, and use it both in `footerContent`
(model.go:1015-1017, replacing the inline fallback) and the seed below, so the box's host and the
footer's host can never diverge.

*Seed once.* In `newModel` (`internal/tui/model.go:124-134`), build the Model into a local `m`, then
seed exactly one entry before returning:
```go
m.transcript.addStartup(startupView{
    Logo:    strings.TrimRight(apogeeLogo, "\n"),
    Host:    hostDisplay(opts),
    Model:   displayModel(opts.Model),
    Version: opts.Version,
})
return m
```
This makes the box `entries[0]` — rendered once at construction, reflowed on resize by the normal
render path, and untouched by `/clear`.

**Tests:** In `internal/tui/render_test.go`, add a `renderStartupBox` (or `renderEntryLines` with an
`entryStartup`) test asserting the ANSI-stripped output (a) contains a distinctive logo fragment,
(b) contains the host, `displayModel`-ed model, and version strings, (c) contains the rounded corner
runes `╭ ╮ ╰ ╯` (same as the prompt box), and (d) does **NOT** contain the black-background SGR that
`inputView()`/`inputBorder` emits (render `inputBorder.Render("x")`, extract its background SGR, and
assert the startup box's raw output lacks it) — the "no black background" acceptance, checked
mechanically. In `internal/tui/model_test.go`, assert `newModel(...)` seeds exactly one entry and it
is `entryStartup` at `entries[0]`, carrying the `Options` host/model/version; and that after routing
`/clear` the `entryStartup` is still present (printed once, survives a context reset).

**Acceptance:** `go build ./... && go vet ./... && go test ./...` green;
`git ls-files logo.md` returns nothing and `internal/tui/logo.txt` is embedded (the repo-root
`logo.md` no longer exists). Commit: `feat(tui): one-time start-up box with logo and session info`.

## Explicitly NOT in this plan

- **No change to the prompt box, footer, or their chrome** — the start-up box only *reuses* the
  `RoundedBorder()` glyphs; `inputBorder`/`chromeRule`/`footerView` are untouched.
- **No change to `internal/mcp/client.go`'s `clientVersion`** — that literal is the MCP *handshake*
  identity, a separate protocol-facing concern from the app version; unifying it is a deliberate
  non-goal here (a possible later follow-up, out of scope).
- **No `version` *subcommand*** (`apogee version`) — Cobra's `--version` flag from item 1 covers the
  CLI need; a subcommand would duplicate it.
- **No persistence, no network, no new config keys.** The box reads only already-resolved `Options`.

## Critical files

- `internal/version/version.go` (new) — the build-version source
- `Makefile` — `VERSION` var + `-ldflags -X` on `build`/`install`
- `cmd/apogee/root.go` — `cmd.Version` (CLI `--version`); `cmd/apogee/wire.go` — populate `Options.Version`
- `internal/tui/tui.go` — `Options.Version`
- `internal/tui/command.go` — `knownCommands`; `internal/tui/autocomplete.go` — `commandMenu`
- `internal/tui/model.go` — `runCommand` `/version` case, `newModel` seed, `hostDisplay` extract
- `internal/tui/transcript.go` — `entryStartup`, `startupView`, `addStartup`
- `internal/tui/render.go` — `renderStartupBox`, `renderEntryLines` case
- `internal/tui/theme.go` — `startupBorder`
- `internal/tui/logo.txt` (moved from repo-root `logo.md`) — the embedded wordmark

## Owner-run checklist (after implementation)

- [ ] `apogee --version` (built via `make build`) prints the `git describe` version; a plain
  `go run ./cmd/apogee --version` prints a sensible fallback (`dev` or a short revision), not empty.
- [ ] Launch `apogee` against the live endpoint: the start-up box sits at the very top of the chat
  as a rounded card with **no black fill**, the border glyphs match the prompt box, and the logo +
  `host` / `model` / `version` rows read correctly (host and version match the footer's host and
  `apogee --version`).
- [ ] The box prints **once**: scroll the transcript and run `/clear` — the box stays put and is not
  re-drawn; resize the window — it reflows without duplicating.
- [ ] Type `/version` — it appears in the `/` autocomplete dropdown with its summary, and submitting
  it prints `apogee <version>` as a note in the chat.
