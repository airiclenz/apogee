# Hero GIF v2: a Go recorder with capture/replay, fixed-length sections, a virtual cursor — plan

**Goal:** `demorig` records, judges and renders the README hero clip end to end without VHS: a captured model cassette is replayed into the real `apogee` binary in a pty, the storyboard alone drives typing, keys, waits and real mouse clicks, and the render composes fixed-length sections, zooms on on-screen targets and a drawn cursor into `graphics/demo.gif`. The new clip shows the v0.23 surface: mode click, sub-agent run view, queued message, split-diff card, context gauge.
**Date:** 2026-09-24
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 34d1d724

**Sources:**
- `graphics/demo/README.md` (rig spec), `graphics/demo/storyboards/hero.yaml`, `graphics/demo/tapes/hero.tape`, `graphics/demo/type.sh`
- `docs/design/test-drivers.md`; `internal/stubllm/` (script, record, server); `internal/tuitest/` (screen, pty, frame)
- `docs/plans/2026-09-24 - 01 - run-view-header-band-and-child-gauge-plan.md`; `docs/adr/0063-*.md`; `docs/adr/0013-*.md`; `layout.md`

**Ratified design calls** (owner, 2026-09-24):
- **Recorder:** VHS is retired; `demorig record` runs `apogee` in a pty over `charmbracelet/x/vt`, timestamps every screen change, and rasterizes frames itself (option B).
- **Model source:** capture once, replay — `demorig capture` proxies a live model into a cassette; `demorig record` replays it deterministically. Hand-scripted replies are not used for the clip.
- **Font:** Source Code Pro, committed under `graphics/demo/fonts/` with its OFL licence; Noto Sans Symbols 2 and Noto Sans Math (OFL) as fallbacks; box-drawing and block-element glyphs drawn procedurally.
- **Chrome:** plain padded terminal on the theme background, no window bar; theme Catppuccin Mocha.
- **Cursor:** a translucent dot that glides to each click target and grows a ring pulse on the click.
- **Sections:** every beat carries a target `duration:`; the render derives the speed, freezes the last frame when a beat runs short.
- **Scenario:** 1 open · 2 click mode marker → Auto (before the prompt: a child keeps its spawn mode, ADR 0013) · 3 prompt "tests are failing — send a sub-agent to find out why, then fix it" · 4 sub-agent fan-out · 5 queued "also add a CHANGELOG entry for the fix" · 6 click into the run view and back · 7 click the Replace card open · 8 zoom on the gauge · 9 CHANGELOG + green tests · 10 hold. `/undo` dropped.
- **Clicks are real:** every on-camera click is an SGR press+release delivered to apogee at the target's computed cell; nothing is faked by keyboard.

**Standing requirements:**
- skills: coding-standards
- Stack: Bubble Tea v2 (`tea.KeyPressMsg`, `msg.String()`); apogee enables mouse mode 1002 + SGR 1006 and toggles blocks on RELEASE.
- The on-screen minus in diffstats is U+2212; every target pattern spells it so.

**Out of scope:**
- Other clips (only `hero` is authored); a window bar; keeping any VHS path alive.
- Changing apogee's TUI to suit the recorder (the recorder adapts to apogee, never the reverse).
- Release, tag or version work.

**Regression check (2026-09-24, 34d1d724):**
- 1: guard folded
- 2: guard folded
- 3: guard folded
- 4: guard folded
- 5: guard folded
- 6: recast
- 7: guard folded
- 8: guard folded
- 9: recast
- 10: recast
- 11: guard folded
- 12: recast
- 13: guard folded
- 14: guard folded
- 15: guard folded
- 16: guard folded
- 6 (second pass): guard folded — Goal names `LoadV2`, target `nth` scalar, hold rule, `fonts:` header key
- 9 (second pass): guard folded — Noto Sans Symbols in the Goal chain, `fonts:` locator, arc-free golden hash, depends on item 6; x/image ≥ v0.45.0 (decision)
- 10 (second pass): guard folded — source → output as the continuous inverse
- 12 (second pass): guard folded — rename grep scope, fonts path, summary secs; lands with item 13 in one run (decision)

## 1. Verify the run-view band and child-gauge plan has shipped — ✅ DONE (2026-09-25)

NOTES (2026-09-25): verification only — archived plan 01 (run-view-header-band-and-child-gauge) exists, archived at b6e02d6f; both `## N.` items carry ✅ DONE (2026-09-24); Acceptance exits 0. No files changed, nothing to commit. Its `**Status:**` header line still reads "unexecuted" despite both items being done (not part of this item's Acceptance).

**What.**
**Goal:** `docs/plans/archived/2026-09-24 - 01 - run-view-header-band-and-child-gauge-plan.md` exists and every item in it is marked done; the hero scenario's beat 6 films the three-row band and the viewed run's own gauge.
**Approach (assumed at the header base):** check only; if the plan is not archived, stop the run — do not execute that plan from here.
**Regression guard.** The Goal this item is held to is exactly: the archived plan file exists and every `## N.` heading in it carries DONE — what the Acceptance checks. The beat-6 clause is context, not acceptance; items 13 and 16 own it. At the base, plan 01 is unarchived and mid-run on the checked-out branch, so this Acceptance fails by design until that run's closeout archives it.
**Files:** none
**Read first:** docs/plans/2026-09-24 - 01 - run-view-header-band-and-child-gauge-plan.md — items 1, 2 headings, Status; docs/plans/archived/2026-09-24 - 00 - review-findings-follow-through-plan.md — "✅ DONE (date)" heading marker; docs/plans/archived/2026-09-24 - 01 - subagent-premature-done-plan.md — same-date "01" sibling (distinct basename)
**Tests.** none.
**Acceptance.** `test -f "docs/plans/archived/2026-09-24 - 01 - run-view-header-band-and-child-gauge-plan.md" && ! grep -E '^## [0-9]+\.' "docs/plans/archived/2026-09-24 - 01 - run-view-header-band-and-child-gauge-plan.md" | grep -v DONE`
**Commit:** none (verification only).

## 2. stubllm cassette: capture a live upstream keyed by conversation, not bytes — ✅ DONE (2026-09-25)

NOTES (2026-09-25): on-disk format is one indented JSON document (`version`, `exchanges`, `probes`), not JSON-lines; a chunk is written as `text` when valid UTF-8 and as `base64` otherwise, so a read that splits a rune loses no byte.
NOTES (2026-09-25): `Exchange` gains `Truncated` (reply ended before EOF) — the retry rule needs it and replay (item 3) can read it; a body that decodes as neither wire's request is still recorded, under a `raw:`-prefixed digest of its bytes.
NOTES (2026-09-25): the proxy drops the client's `Accept-Encoding` upstream so the cassette holds uncompressed bytes; `NewCassetteRecorder` refuses a named key variable that is unset or empty.
NOTES (2026-09-25): capture_test.go uses a raw `httptest` upstream (as the item's Tests line specifies) rather than a stubllm Script — the tests pin exact chunk boundaries and the auth header spelling, which a Script cannot place and the Server's request log does not record.

**What.**
**Goal:** `internal/stubllm` has a cassette format and a recording proxy: each proxied request is stored with its raw response bytes and per-chunk arrival offsets under a stable key, and the latest `GET /v1/models` and `GET /props` bodies are stored verbatim. The proxy adds `Authorization: Bearer <key>` from an env var it is given, so apogee's config stays keyless.
**Approach (assumed at the header base):** new `cassette.go` (types `Cassette`, `Exchange{Key, Status, Header, Chunks[]{Offset, Bytes}}`, YAML or JSON-lines on disk, one deep module) and `capture.go` (an `http.Handler` wrapping a reverse proxy; `NewRecorder` in `record.go` is the model for tee-ing a stream). The key is a hash of: wire (`chat` | `messages`), `stream` flag, the sorted tool-name set, the first user message with ISO dates normalized to `<date>`, and the ordered assistant turns (content + tool-call names + arguments). System text and tool-result content are excluded — both vary run to run (scratch dir, session id, `go test` timings). A key seen twice queues both responses in capture order. Retried POSTs are recorded once (a retry repeats the key; store only the final response).
**Regression guard.** Auth is injected per wire: `x-api-key` on `/v1/messages` and on a probe carrying `anthropic-version` (`server.go` `anthropicVersionHeader`; `provider/wire_anthropic.go` `headers`), `Authorization: Bearer` otherwise. Retry rule (the provider retries 5xx/429/transport faults and one 4xx, `client.go`): a later exchange under a key REPLACES the earlier one when the earlier ended non-2xx or before EOF, else APPENDS to the key's queue. Each probe (`/v1/models`, `/props`) is stored with its status and Content-Type, so a 404 `/props` replays as a 404 (as `record.go` `fileProbe` does). `Capture` (script.go), `capture`, `captureKey`, `captureOf` (record.go) are taken in package stubllm: the handler is `CassetteRecorder` / `NewCassetteRecorder`.
**Files:** internal/stubllm/cassette.go, internal/stubllm/capture.go, internal/stubllm/cassette_test.go, internal/stubllm/capture_test.go, internal/stubllm/doc.go
**Read first:** internal/stubllm/record.go — NewRecorder, Recorder.ServeHTTP, Recorder.capture, recordingBody, fileProbe, settleInflight; internal/stubllm/wire.go — chatRequest.messages, chatRequest.toolNames; internal/stubllm/wire_anthropic.go — anthropicRequest.messages, anthropicRequest.toolNames, userMessages; internal/stubllm/log.go — Message;
internal/stubllm/server.go — modelsPath, propsPath, anthropicVersionHeader; internal/provider/wire_anthropic.go — anthropicCodec.headers; internal/stubllm/record_test.go — recorderProxy, observe; internal/stubllm/doc.go — Files list
**Tests.** Key stability: two requests differing only in system text and tool-result content hash equal; differing assistant tool-call args hash differ; a child (different tool set) and parent with the same first user message hash differ. Capture round-trip through an `httptest` upstream streaming three SSE chunks with delays: cassette holds three chunks with increasing offsets; `/v1/models` body stored. Auth: the upstream sees `x-api-key` on a `/v1/messages` POST and `Authorization: Bearer` on a chat POST. Retry rule: a 500 then a 200 under one key stores only the 200 (replace); two 200s under one key queue both (append). A 404 `/props` is stored with its 404 status.
**Acceptance.** `go test -race -count=1 ./internal/stubllm/`
**Commit:** `feat(stubllm): cassette capture — a keyed recording proxy with raw chunks and timing`

## 3. stubllm cassette replay — ✅ DONE (2026-09-25)

NOTES (2026-09-25): `NewReplayer` returns `*Replayer` (an `http.Handler`) rather than the bare interface the approach names, so a test can capture the unknown-key log line through its unexported `logf`; a negative pace is taken as 0.
NOTES (2026-09-25): an exchange captured `Truncated` replays by dropping the connection after its last chunk (reusing `kill`), as the upstream did; a probe the cassette never captured answers 404.
NOTES (2026-09-25): consequential edit — internal/stubllm/doc.go: replay.go joins the package Files list.

**What.** Depends on item 2.
**Goal:** a cassette replays as an upstream: a request whose key is in the cassette receives the recorded status, headers and chunks at the recorded offsets (scaled by a pace factor); a key served past its recorded count re-serves its last response (retries); an unknown key answers 500 with the key and the request's first user message in the body and a log line; `/v1/models` and `/props` always answer from the cassette.
**Approach (assumed at the header base):** `replay.go` with `NewReplayer(c *Cassette, pace float64) http.Handler`, independent of `Script`/`Turn` matching; concurrent requests on different keys stream concurrently.
**Regression guard.** Every Goal clause has a test: a key requested N+1 times gets its last response; GET `/v1/models` and `/props` return the stored bodies and statuses; the unknown-key 500 body names the first user message — posted raw, since the provider retries 5xx twice (`client.go` `defaultMaxRetries`); a non-zero pace scales the chunk offsets.
**Files:** internal/stubllm/replay.go, internal/stubllm/replay_test.go
**Read first:** internal/stubllm/record_test.go — observe, replayed.same, recorderProxy, postTo; internal/stubllm/server_test.go — streamThroughProvider (takes *Server, not a URL — use observe), New/Script as the capture upstream; internal/stubllm/server.go — modelsPath, propsPath, awaitLimit (per-request hold pattern); internal/provider/client.go — defaultMaxRetries; internal/stubllm/doc.go — Files list
**Tests.** Capture→replay round trip over a real `provider` client stream yields the same text and usage; two concurrent keys interleave without blocking; unknown key → 500 naming the key and the first user message (posted raw); pace 0 streams without delay; a non-zero pace scales offsets; a key requested one past its recorded count re-serves its last response; `/v1/models` and `/props` answer with the stored bodies and statuses.
**Acceptance.** `go test -race -count=1 ./internal/stubllm/`
**Commit:** `feat(stubllm): cassette replay — keyed, paced, concurrent`

## 4. demorig: the humanized-typing profile in Go

**What.**
**Goal:** `cmd/demorig` generates per-character keystroke delays byte-for-byte equivalent to `graphics/demo/type.sh`: MINSTD (`s=16807*s mod 2147483647`, seed 4242, first draw discarded, `draw(lo,hi)=lo+s%(hi-lo+1)`), bands letter 25–45 ms, after space 60–90, after `.,-!` 90–140, thinking pause 300–500 replacing a space gap when `draw(1,8)==1` and fewer than 2 pauses so far (the draw is consumed on every space), band chosen by the character just typed, no gap after the last character, `/` a letter.
**Approach (assumed at the header base):** `typing.go` exposing `Humanize(s string, seed int64) []time.Duration`; one RNG per string, as `type.sh` does.
**Regression guard.** `Humanize` iterates runes (`[]rune(s)`), ASCII-identical to `type.sh`; the punctuation set stays `.,-!` (the em dash in item 13's prompt takes the letter band); the new hero goldens are pinned rune-wise. The four `type.sh` strings and totals 775/3382/1791/135 are copied into `typing_test.go` as Go literals, never read from the file — item 13 deletes `type.sh`.
**Files:** cmd/demorig/typing.go, cmd/demorig/typing_test.go
**Read first:** graphics/demo/type.sh — advanceState, draw, the BEGIN loop, HERO_STRINGS, GOLDEN_TOTALS_MS, check_profile; cmd/demorig/main.go — package doc
**Tests.** The four `type.sh --check` goldens: totals 775, 3382, 1791, 135 ms for its four strings (copied into `typing_test.go` as literals); pooled letter mean 35 ± 2 ms; ≤ 2 pauses per string; plus goldens for the two new hero strings (item 13's prompt and queued text), computed once and pinned.
**Acceptance.** `go test -count=1 -run TestHumanize ./cmd/demorig/`
**Commit:** `feat(demorig): humanized typing profile ported from type.sh`

## 5. demorig: pty terminal session and the take file

**What.**
**Goal:** `demorig` can launch a command in a pty of a given cols×rows, emulate it with `x/vt`, keep the emulator's reply pipe drained, and write a **take**: a timestamped stream of full-style cell-grid snapshots (rune, width, fg, bg, bold, faint, italic, underline, reverse; cursor position and visibility) coalesced to at most the storyboard fps, plus an event log. A take file round-trips through write/read.
**Approach (assumed at the header base):** `term.go` (session: `pty.StartWithAttrs` with Setsid/Setctty, `TERM=xterm-256color`, `COLORTERM=truecolor`, output pump into `vt.Emulator`, reply drain as `tuitest.Screen`'s `pump`, cursor visibility via `vt.Callbacks.CursorVisibility`) and `take.go` (gzip JSON-lines; snapshot dedupe when the grid is unchanged). Reuse the margin-clamp fix: export `tuitest.ClampMargins` and call it — one workaround, one owner. `tuitest` constructors take `testing.TB`; do not route through them.
**Regression guard.** all pty/terminal code (term.go and its tests) carries `//go:build !windows`, with a windows stub that returns an "unsupported on windows" error, as internal/tuitest/pty.go is split; Acceptance adds `GOOS=windows go build ./cmd/demorig/`
**Files:** cmd/demorig/term.go, cmd/demorig/term_windows.go, cmd/demorig/take.go, cmd/demorig/term_test.go, cmd/demorig/take_test.go, internal/tuitest/screen.go
**Read first:** internal/tuitest/screen.go — clampMargins, clampParam, NewScreen, Screen.pump, closeSentinel; internal/tuitest/pty.go — NewPTYDriver (StartWithAttrs, Setsid/Setctty), TTYState; internal/tuitest/pty_windows.go — PTYDriver stub; internal/tuitest/frame.go — newFrame, convertCell;
Makefile — cross; .github/workflows/ci.yml — cross job
**Tests.** Launch `sh -c 'printf "\033[1;31mhi\033[0m"; sleep 0.2'` at 40×5: the take's last snapshot holds `h`,`i` at row 0 with a red foreground; a DA1 query from the child is answered (no hang, bounded by a 5 s test timeout); take write→read equality; an unchanged screen emits no new snapshot.
**Acceptance.** `go test -race -count=1 ./cmd/demorig/ ./internal/tuitest/ && GOOS=windows go build ./cmd/demorig/`
**Commit:** `feat(demorig): pty terminal session writing a timestamped take`

## 6. demorig: storyboard v2 schema and lint

**What.** Recast at the regression check (2026-09-24).
**Goal:** a storyboard is the single source of a clip: header `clip`, `ship`, `cassette`, `frame{cols, rows, padding, font_size, line_height, scale, width, fps, max_colors}` (width = shipped GIF width), top-level `expect{stage}`; beats `{id, title, why, notes, do[], duration, hold, cut, zoom{target, factor, in, out}, expect[]}`. Actions in `do`: `type{text, humanize (default true)}`, `key{name, repeat}`, `click{target, times}`, `wait{screen, gone, timeout}`, `pause{for}`. A **target** is `{text: <regex>, nth: first|last|N, area: footer|status|transcript|any}`. `LoadV2` validates strictly and reports every problem; `lint` switches to it in item 12.
**Approach (assumed at the header base):** the v2 schema carries none of `Align`, `PaintLag`, `Regions`, `Tape`, `Beat.Tape`, video/beat anchors, `Framing.Speed`, `tapeHeaders`, `validateBookends` (item 12 deletes them from v1); keep the session-anchor grammar (`kind`, `text`, `tool`, `target`, `nth`) for expects only. Validation: `duration` > 0 and 0 ≤ `hold` < `duration`, unless `cut`; `zoom.factor` in [1, 3] when set; `wait.timeout` ≤ 180 s; regexes compile; beat ids unique ascending.
**Regression guard.** v2 lands BESIDE v1 so cmd/demorig stays green — new types and a `LoadV2` in a new file (cmd/demorig/storyboard_v2.go + storyboard_v2_test.go, fixtures testdata/v2/good.yaml and testdata/v2/bad-*.yaml); storyboard.go, tiny.tape, TestLoadHeroStoryboard and every v1 caller stay untouched in item 6; `demorig lint` is unchanged in item 6; Acceptance selects only the v2 tests. The frame block is `frame{cols, rows, padding, font_size, line_height, scale, width, fps, max_colors}` (width = shipped GIF width); drop "cols/rows derive from font metrics" — pixel size is derived in item 9. The target's `nth` is its own scalar in storyboard_v2.go accepting `first`, `last` or a positive N (v1 `Nth` refuses `first`); expects keep v1 `Nth` (`last`/N). A beat with no `hold:` has hold 0. The header also carries a required `fonts:` (the font directory, resolved against the storyboard's directory like `ship:`; item 9 reads it; lint reports `fonts: missing`).
**Files:** cmd/demorig/storyboard_v2.go, cmd/demorig/storyboard_v2_test.go, cmd/demorig/testdata/v2/good.yaml, cmd/demorig/testdata/v2/bad-*.yaml
**Read first:** cmd/demorig/storyboard.go — Load, Nth.UnmarshalYAML, ValidationError, validateIDs, sessionKinds; cmd/demorig/storyboard_test.go — TestLoadRejectsBadFixtures; cmd/demorig/main.go — newLintCommand;
cmd/demorig/testdata — bad-expect-without-entry.yaml
**Tests.** `testdata/v2/good.yaml` loads through `LoadV2`; one `testdata/v2/bad-*.yaml` per rule fails naming its field; retired v1 fields are rejected by strict decoding; a target `nth: first` decodes; a beat without `hold:` loads; a storyboard without `fonts:` fails naming it. The tests are named `TestLoadV2…`.
**Acceptance.** `go test -count=1 -run 'TestLoadV2' ./cmd/demorig/ && go vet ./cmd/demorig/`
**Commit:** `feat(demorig)!: storyboard v2 — actions, targets and section durations`

## 7. demorig: the action engine — targets, real clicks, waits

**What.** Depends on items 4, 5, 6.
**Goal:** a storyboard's beats run against a live session: `type` sends humanized keystrokes (item 4); `key` sends the named key; `click` resolves its target on the current snapshot and writes an SGR press `ESC[<0;col+1;row+1M` then release `…m` (≥ 40 ms apart, `times` pairs ≥ 250 ms apart); `wait` blocks until the regex matches (or, with `gone`, no longer matches) or fails the beat at its timeout. Every beat start, action start/end, click cell and resolved target box (cells) is appended to the take's event log.
**Approach (assumed at the header base):** `engine.go`; target search scans snapshot rows as strings in terminal columns (wide cells counted once), `area` maps to row ranges (footer = last row, status = the row above the prompt box, transcript = the rest); `nth: last` scans bottom-up. ESC-prefixed writes are one `Write` call so apogee never reads a lone ESC (a lone ESC mid-run cancels the run).
**Regression guard.** engine code follows item 5's `!windows` split; Acceptance adds `GOOS=windows go build ./cmd/demorig/`. Areas: `footer` is the row directly above the bottom `▁` hairline (the last non-hairline row — apogee's last row is the hairline, layout.md "The frame's own floor"); `status` is the row directly below the lowest `▔` hairline above the prompt box (tuitest `Frame.PromptBox` locates the box) — the staged band sits between status and box. The fake TUI puts its tty in raw mode first (`x/termios` as tuitest does, or `stty raw -echo`) and is the re-executed test binary (`internal/subprocess/teardown_test.go` pattern), not a standalone `go build`.
**Files:** cmd/demorig/engine.go, cmd/demorig/engine_test.go
**Read first:** internal/tui/mouse.go — handleMouseClick, handleFooterModeClick, handleMouseRelease, handleMouseMotion; internal/tuitest/frame.go — Frame.Find, Frame.PromptBox, Frame.Row; layout.md — "The frame's own floor", "Run view", staged band row; cmd/apogee/testdata/frames/t17-run-view.txt — floor rows;
internal/tuitest/pty.go — TTYState, Press; internal/subprocess/teardown_test.go — re-exec helper
**Tests.** Against a scripted fake TUI (the re-executed test binary in raw mode, printing a known screen and echoing input bytes to a file): click on `last` of a repeated word hits the lower row's cell; press and release bytes land as two complete sequences; wait times out with a beat-naming error; events carry monotonically increasing timestamps; a `footer` target resolves on the row above a screen's closing `▁▁▁` row; a `status` target resolves below the `▔` hairline with a staged `⧖` row present between it and the box.
**Acceptance.** `go test -race -count=1 -run TestEngine ./cmd/demorig/ && GOOS=windows go build ./cmd/demorig/`
**Commit:** `feat(demorig): action engine with real SGR clicks and screen waits`

## 8. demorig record and capture subcommands

**What.** Depends on items 3, 7.
**Goal:** `demorig capture <storyboard> --upstream <url> --key-env <VAR>` and `demorig record <storyboard>` each: run `reset.sh` in the work dir (fail on non-zero), start the cassette proxy (capture) or replayer (record) on `127.0.0.1:<port from rig.env>`, launch `apogee` under the rig's `env.sh` (HOME remap, confinement-safe Go caches) in the pty at the storyboard geometry, run the beats, write `<work>/<clip>.take` and, for capture, the storyboard's `cassette` path; then run `check` (item 12) and exit with its status. The shell prompt never reaches the take: recording starts at apogee's first paint.
**Approach (assumed at the header base):** `record.go`; launch via `bash -c 'source <work>/env.sh >/dev/null && exec apogee'`; session JSON = newest under the demo home's sessions dir, recorded by path in the take header.
**Regression guard.** the pty size is the storyboard's `frame.cols`×`frame.rows` (item 6) — no dependency on item 9; follows the `!windows` split; Acceptance adds `GOOS=windows go build ./cmd/demorig/`. The closing check is the existing one — `checkTake` over the take's session JSON with `--stage <stage>`, exiting with its status; item 12 re-points it at the take. `record` validates the cassette path before running `reset.sh`, and its error names that path.
**Files:** cmd/demorig/record.go, cmd/demorig/record_windows.go, cmd/demorig/record_test.go, cmd/demorig/main.go
**Read first:** cmd/demorig/main.go — newRootCommand, runE, exitCodeFor; cmd/demorig/check.go — newCheckCommand, checkTake, loadEntries; graphics/demo/record.sh — KEY_ENV refusal, newest-session lookup; graphics/demo/setup.sh — env.sh heredoc, rig.env (KEY_ENV only at base); graphics/demo/reset.sh;
internal/tui/tui.go — claimAltScreen (the first-paint signal: ESC[?1049h before the first frame); internal/tuitest/pty.go — StartWithAttrs Setsid/Setctty, pty_windows.go
**Tests.** Usage errors exit 2; a record against a missing cassette fails before launching and before `reset.sh` runs, with an error naming the cassette path; the first snapshot in a take is post-first-paint (fake TUI clearing the screen after a shell banner).
**Acceptance.** `go test -race -count=1 -run 'TestRecord|TestCapture' ./cmd/demorig/ && GOOS=windows go build ./cmd/demorig/`
**Commit:** `feat(demorig): record and capture — replay or proxy a model into a take`

## 9. demorig: rasterizer with embedded fonts

**What.** Recast at the regression check (2026-09-24). Depends on items 5, 6.
**Goal:** a take snapshot rasterizes to an RGBA frame at the storyboard geometry: Source Code Pro Regular/Bold/Italic from `graphics/demo/fonts/`, fallback to Noto Sans Symbols 2, then Noto Sans Math, then Noto Sans Symbols per missing rune, box-drawing (U+2500–257F) and block elements (U+2580–259F, incl. eighth blocks the gauge uses) drawn procedurally to fill the cell exactly, Catppuccin Mocha for default fg/bg and the 16 ANSI colours, truecolor passed through, faint at 60 % alpha, reverse swaps, wide cells span two columns. Every rune apogee's TUI paints resolves to a glyph.
**Approach (assumed at the header base):** `raster.go`; new direct dependency `golang.org/x/image` (`font/opentype`, `font`, `math/fixed`, `vector`); glyph cache keyed by (rune, style); fonts read from disk at render time (not `go:embed`, so the binary stays small); `fonts/OFL.txt` per family.
**Regression guard.** pixel geometry is derived: width = 2·padding + cols·cellW, height = 2·padding + rows·lineH, cellW = Source Code Pro advance at font_size, lineH = round(font_size·line_height). Noto Sans Symbols (v1, OFL) joins the chain as the third fallback — U+2303 `⌃` (`prompteditor.go` `idlePlaceholder`, `help.go` `helpKeyQuit`) has no glyph in Source Code Pro, Noto Sans Symbols 2 or Noto Sans Math. The golden hash covers only axis-aligned integer fills (straight box lines, blocks, eighths, spaces, colours); rounded corners ╭╮╰╯ (U+256D–2570, lipgloss.RoundedBorder), other arcs, diagonals and font glyphs are checked by coverage, not bytes — `x/image/vector`'s `lerp` fuses to FMA on arm64 and not on amd64. Require golang.org/x/image at v0.45.0 or later (v0.43.0 carries GO-2026-6222, fixed in v0.45.0). Fonts are located by item 6's `fonts:` header key (resolved against the storyboard's directory); `raster.go` takes that directory, and its tests pass `../../graphics/demo/fonts` (cwd cmd/demorig). The coverage test collects `token.STRING` and `token.CHAR` literals, decoded via `strconv.Unquote`/`UnquoteChar` (the gauge's `gaugeEighths` are rune literals).
**Files:** cmd/demorig/raster.go, cmd/demorig/raster_test.go, graphics/demo/fonts/*, go.mod, go.sum
**Read first:** internal/tui/help.go — helpKeyQuit; internal/tui/prompteditor.go — idlePlaceholder; internal/tui/model.go — gaugeEighths; internal/tui/theme.go — RoundedBorder styles; cmd/demorig/storyboard_v2.go — v2 frame type (item 6);
graphics/demo/tapes/hero.tape — FontSize/Padding/Theme; go.mod — require block (x/sys v0.47.0, x/text v0.39.0)
**Tests.** Coverage: every non-ASCII rune in string and rune literals (`token.STRING`, `token.CHAR`, unquoted) under `internal/tui/*.go` (non-test) resolves in the chain or the procedural set; `⌃` resolves; `█` fills its cell edge to edge; `╭` is checked by coverage; a golden PNG hash for a fixed 20×4 snapshot of axis-aligned integer fills only (straight lines, blocks, eighths, spaces, colours); the derived pixel size for a given cols/rows/padding/font_size/line_height matches the formula.
**Acceptance.** `go test -count=1 -run TestRaster ./cmd/demorig/ && go build ./cmd/demorig/`
**Commit:** `feat(demorig): rasterize takes with Source Code Pro and procedural box glyphs`

## 10. demorig: section timing — durations to a frame schedule

**What.** Recast at the regression check (2026-09-24). Depends on item 6.
**Goal:** a pure function maps a take's beat boundaries and each beat's `duration`/`hold`/`cut` to an output frame schedule (output time → source time): a beat of real length L, hold H, target D plays 1× for H then at speed (L−H)/(D−H); when L ≤ D it plays 1× and freezes its last frame for D−L; `cut` beats vanish; the clip's total length equals the sum of the non-cut durations to within one frame. A speed above 6× is reported as a warning naming the beat.
**Approach (assumed at the header base):** `schedule.go`; beat boundaries come from the take's beat-start events; the last beat ends at the take's end.
**Regression guard.** the schedule exposes both directions — output time → (source time, beat) and source time → output time — item 11 times zoom ramps and cursor glides/pulses through the latter. Source → output is the continuous piecewise-linear inverse (1× over the hold, then ÷speed, a freeze maps to the beat's last instant), absent only inside `cut` beats; an instant no sampled output frame lands on still has an output time.
**Files:** cmd/demorig/schedule.go, cmd/demorig/schedule_test.go
**Read first:** cmd/demorig/filtergraph.go — segmentsFrom, Segment.pieces, checkSegments; cmd/demorig/storyboard.go — Framing, Framing.Rate, Zoom.Hold; cmd/demorig/beats.go — locateBeats; cmd/demorig/storyboard_v2.go — v2 beat duration/hold/cut (item 6)
**Tests.** Table: slow beat compressed, short beat frozen, hold respected, cut removed, total length exact at 24 fps, >6× warning; source → output inverts output → source on played instants and is absent for a source instant inside a cut beat; a click at source 5 s inside a 3× beat has an output time.
**Acceptance.** `go test -count=1 -run TestSchedule ./cmd/demorig/`
**Commit:** `feat(demorig): fixed-length sections from per-beat durations`

## 11. demorig: compositor — zoom on targets and the cursor

**What.** Depends on items 9, 10.
**Goal:** each output frame is composed from its scheduled snapshot: zoom centres on the beat's target box (from the take's resolved-target events, else resolved on the snapshot) with the factor given or auto-fit (box + 2 cells margin fills 70 % of the frame width, clamped to [1.25, 2.5]), eased in/out over `in`/`out` (default 400 ms), the crop clamped inside the frame. The cursor: a white dot at 55 % opacity with a 2 px dark outline, 28 px across at 2× scale; it fades in 300 ms before the beat's first click at the previous click point (first ever: frame centre-bottom), glides to each click cell's centre over 450 ms ease-in-out ending at the press, pulses a ring 28 → 72 px fading from 80 % to 0 over 350 ms, and fades out 1.2 s after the beat's last click. It is drawn after the zoom at constant size, its position mapped through the zoom transform.
**Approach (assumed at the header base):** `compose.go`; `golang.org/x/image/draw` CatmullRom for the zoom scale; output downscaled to `frame.width`.
**Regression guard.** The ring's 28 → 72 px is its diameter, grown linearly. Cursor timings (fade-in, glide, pulse, fade-out) run on the output clock: each click's source time is mapped through item 10's source → output map, and a click with no output time (inside a `cut` beat) is dropped.
**Files:** cmd/demorig/compose.go, cmd/demorig/compose_test.go
**Read first:** cmd/demorig/filtergraph.go — zoomFilters, zoomFactor, Zoom.span (today's zoom ramps and clamping); cmd/demorig/storyboard.go — Zoom, Frame (width, scale); cmd/demorig/render.go — renderTake (the render the compositor feeds in item 12)
**Tests.** Zoom at factor 2 on a box at the frame edge keeps the crop inside; cursor position mid-glide lies on the segment; ring diameter at press+175 ms ≈ 50 px; zoomed cursor maps to the zoomed target cell; a click inside a cut beat draws no cursor.
**Acceptance.** `go test -count=1 -run TestCompose ./cmd/demorig/`
**Commit:** `feat(demorig): compositor with target zoom and a gliding click cursor`

## 12. demorig render and check on the take; retire the VHS pipeline

**What.** Recast at the regression check (2026-09-24). Depends on items 8, 11.
**Goal:** `demorig render <storyboard> [<take>] [-o out.gif] [--dry-run]` streams composed frames as rawvideo into `ffmpeg` (palettegen/paletteuse at `max_colors`) then `gifsicle -O3 --lossy=80` when present, writing `ship:` by default and printing `<path>  <size>  <secs>s` plus per-beat effective speeds. `demorig check <storyboard> [<take>] [--stage dir]` judges session expects (`contains`, `before`, `after`, `entry`) and a new `seen: <regex>` expect (matched on any snapshot inside the beat) plus `expect.stage`, one `beat N | PASS/FAIL | detail` row each, exit 1 on any FAIL. `align.go`, `filtergraph.go`, `beats.go`, the `beats` subcommand and their tests and goldens are deleted. Items 12 and 13 land in the same execution run — no run closeout between them, because between them the v1 hero.yaml and record.sh's `check` call no longer work with the v2 demorig.
**Approach (assumed at the header base):** `render.go` rewritten around `compose.go`; `check.go` gains `seen`; `session.go`/`findEntry` stay.
**Regression guard.** item 12 deletes v1 (the v1 types/Load in storyboard.go, align.go, anchors.go video/beat resolution, beats.go and the `beats` subcommand, filtergraph.go, the v1 render path, tiny.tape and v1-only fixtures/goldens), renames the v2 types and `LoadV2` to the canonical names, switches `lint`/`check`/`render` to v2, and deletes TestLoadHeroStoryboard and every test that loads graphics/demo/storyboards/hero.yaml (item 13 re-adds a hero load test against the v2 file). Ordering rule: `before/after N` orders against the entry that beat N's first `entry:` expect locates, and gives a FAIL row naming beat N when it has none; an expect with neither `entry` nor `seen` is a lint error (the v2 `bad-expect-without-entry` fixture pins it). The default `[<take>]` is `${APOGEE_DEMO_WORK:-$HOME/.cache/apogee-demo}/<clip>.take`, resolved by one helper that `record.go` also uses. The rename's scope is every file `git grep -nE 'LoadV2|<each v2 type name>' cmd/demorig` hits (schedule.go, compose.go, raster.go and item 7's action engine included), not a closed list. The summary's `<secs>` is item 10's schedule total; `number` moves into render.go, `takeDuration` goes with align.go. The render test's testdata storyboard names `fonts:` relative to its own directory (`../../../graphics/demo/fonts` from cmd/demorig/testdata/).
**Files:** cmd/demorig/render.go, cmd/demorig/check.go, cmd/demorig/check_test.go, cmd/demorig/main.go, cmd/demorig/record.go, cmd/demorig/storyboard.go, cmd/demorig/storyboard_test.go, cmd/demorig/storyboard_v2.go, cmd/demorig/storyboard_v2_test.go, cmd/demorig/align.go, cmd/demorig/align_test.go, cmd/demorig/filtergraph.go, cmd/demorig/filtergraph_test.go, cmd/demorig/beats.go, cmd/demorig/beats_test.go, cmd/demorig/anchors.go, cmd/demorig/anchors_test.go, cmd/demorig/testdata/, and every file the rename grep hits
**Read first:** cmd/demorig/check.go — checkTake, Expect.judge, orderClause; cmd/demorig/render.go — writeRenderSummary; cmd/demorig/main.go — newRootCommand, newLintCommand;
cmd/demorig/anchors.go — findEntry; graphics/demo/record.sh — check/render calls
**Tests.** `--dry-run` prints the ffmpeg line; a 2-beat synthetic take renders a GIF of the summed duration, its summary `<secs>` equal to the schedule total (skip when `ffmpeg` absent); check: `seen` PASS/FAIL rows; stage row SKIP without `--stage`; `after N` orders against beat N's first `entry:` expect and FAILs naming beat N when it has none; an expect with neither `entry` nor `seen` fails lint; the default take path honours `APOGEE_DEMO_WORK`. Check and render tests load only testdata storyboards.
**Acceptance.** `go test -race -count=1 ./cmd/demorig/ && go vet ./cmd/demorig/ && ! ls cmd/demorig/filtergraph.go cmd/demorig/align.go 2>/dev/null`
**Commit:** `feat(demorig)!: render and check from the take; retire the VHS alignment pipeline`

## 13. The hero v2 storyboard and rig scripts

**What.** Depends on item 12.
**Goal:** `graphics/demo/storyboards/hero.yaml` encodes the ratified ten-beat scenario (header "Scenario") with durations 2, 3, 4, 4, 4, 6, 6, 3, 4, 3 s, and `demorig lint` passes on it. Targets: footer `◐ ask before`; picker row `⏵⏵ auto` clicked twice; wait `auto · confined`; wait `Sub-Agent` (no ✦ — it blinks) and `tool calls`; queued `⧖`; the sub-agent member row `┕ .*tool calls` (last); the run-view band `← main ›` (zoom, then click it to go back); the Replace row matched on `\+1 −1` (last) opened by click, then the viewport re-attached to the live tail as `layout.md` prescribes; gauge `\d+k/\d+k \d+%` (area status). Expects: beat 2 `seen: auto · confined`; beat 4 a `toolCall` `sub_agent` entry; beat 5 an `interjected` entry; beat 7 `contains: "+1 −1"`; beat 9 a CHANGELOG edit `after: 5` and `go test` output containing `ok`; stage dirty. `setup.sh` writes a keyless server entry at `http://127.0.0.1:<port>` with `parallel-agents: 4` pinned and the model id and alias unchanged, writes the port to `rig.env`, and no longer checks for `vhs`. `record.sh`, `gen.sh`, `type.sh` and `tapes/` are deleted.
**Approach (assumed at the header base):** `why`/`notes` per beat carry the reasoning, as today.
**Regression guard.** hero frame values are cols 135, rows 44 (owner, 2026-09-25: ≈ 2494×1648 px at 2×, 1.5 : 1), font_size 30, line_height 1.2, padding 32, scale 2, width 1250, fps 24, max_colors 192; item 13 adds a test in cmd/demorig loading graphics/demo/storyboards/hero.yaml with the v2 loader and names it in Files/Tests. In place of the Goal's expects and gauge target: beat 4 `{entry: {kind: toolCall, tool: Sub-Agent}}` (the card label, never the tool name — `Anchor.matches` prefix-matches `ToolView.Label`); beat 7 `{entry: {kind: toolCall, tool: Replace, target: task.go}, contains: "+1 −1"}`; beat 9 `{entry: {kind: toolCall, tool: Tests, nth: last}, contains: PASS}` (`entryContains` reads Text/Label/Stat; the run_tests stat is the bare verdict) and its `after: 5` rests on item 12's ordering rule; gauge `\d+(\.\d)?[kMG]?/\d+(\.\d)?[kMG] \d+%` (layout.md "How a size is spelled"). Afterwards `grep -rnE 'record\.sh|gen\.sh|type\.sh|tapes/|\bvhs\b' graphics/demo/*.sh graphics/demo/storyboards/` prints nothing (setup.sh's closing "next: ./record.sh hero" included).
**Files:** graphics/demo/storyboards/hero.yaml, graphics/demo/setup.sh, graphics/demo/reset.sh, graphics/demo/record.sh, graphics/demo/gen.sh, graphics/demo/type.sh, graphics/demo/tapes/hero.tape, cmd/demorig/storyboard_test.go
**Read first:** graphics/demo/storyboards/hero.yaml — beats, expect; graphics/demo/setup.sh — config heredoc, env.sh, rig.env; internal/tui/toolregistry.go — "sub_agent", "run_tests", testVerdictStat; cmd/demorig/check.go — entryContains, orderClause; internal/tui/model.go — modeMarker, confinementWord, footerModeText, statusRight;
internal/tui/subagentblock.go — subAgentSummaryLine; cmd/demorig/storyboard_test.go — TestLoadHeroStoryboard; layout.md — "The status line's right slot"
**Tests.** `demorig lint` on the new file; `TestLoadHeroStoryboard` in `storyboard_test.go` re-added against the v2 hero file (10 beats, the durations, the frame values); `bash -n` on `setup.sh` and on `reset.sh`.
**Acceptance.** `go run ./cmd/demorig lint graphics/demo/storyboards/hero.yaml && bash -n graphics/demo/setup.sh && bash -n graphics/demo/reset.sh && ! test -e graphics/demo/tapes && ! grep -rnE 'record\.sh|gen\.sh|type\.sh|tapes/|\bvhs\b' graphics/demo/*.sh graphics/demo/storyboards/ && go test -count=1 ./cmd/demorig/`
**Commit:** `feat(demo): hero v2 storyboard — mode click, run view, queued message, gauge`

## 14. End-to-end smoke: record → check → render against a fixture cassette

**What.** Depends on item 13.
**Goal:** a test builds `apogee`, sets up a throwaway rig in `t.TempDir()`, and records a three-beat storyboard (open, click the mode marker to Auto, type a prompt answered by a two-turn fixture cassette with one tool call) through replay; `check` passes and `render --dry-run` succeeds. It proves the whole pipeline without a network or key.
**Approach (assumed at the header base):** fixture cassette hand-assembled with `stubllm` types in the test; binary build as `cmd/apogee/main_test.go` does; gated by `testing.Short()`.
**Regression guard.** The fixture cassette is built in the test by running `demorig capture` against a `stubllm` Script upstream (`stubllm.New`/`Serve`), then recorded from — never hand-assembled keys that restate apogee's tool menu. The freshly built binary's directory leads PATH for `setup.sh` and `record` (else env.sh bakes a missing or stale `apogee`). The test is gated on an opt-in env var (`APOGEE_DEMO_E2E=1`), as the `APOGEE_LIVE_ENDPOINT` tests are, not on `testing.Short()` (`scripts/test-shards.sh` passes no `-short`). `config.EnvConfig`, `EnvServer`, `EnvEndpoint`, `EnvModel`, `EnvMode`, `EnvBypass` and `EnvWorkspace` are removed from the apogee child's environment, in the test and in `record.go`'s launch.
**Files:** cmd/demorig/e2e_test.go, cmd/demorig/testdata/e2e/, cmd/demorig/record.go
**Read first:** cmd/apogee/main_test.go — buildE2EBinary, ambientApogeeEnv, TestMain; internal/stubllm/doc.go — Script, New, Serve; graphics/demo/setup.sh — env.sh PATH line, warm step, rig.env; graphics/demo/reset.sh; scripts/test-shards.sh — HEAVY_PKGS; cmd/demorig/check.go — checkTake
**Tests.** The test itself; it skips without `APOGEE_DEMO_E2E=1`.
**Acceptance.** `APOGEE_DEMO_E2E=1 go test -race -count=1 -run TestE2E ./cmd/demorig/`
**Commit:** `test(demorig): end-to-end record, check and render over a fixture cassette`

## 15. Rewrite the rig README

**What.** Depends on item 13.
**Goal:** `graphics/demo/README.md` documents the v2 rig: quick start (`setup.sh`, `demorig capture` once with the key, `demorig record`, `demorig render`), the storyboard v2 schema with hero beat 6 as the worked example, targets and areas, capture/replay and the cassette key (when to re-capture: an unknown-key 500), section durations, zoom and cursor, fonts and licences, settled facts that still hold (endpoint without `/v1`, Go caches in the workspace, prompt wording is load-bearing, mode before prompt). Every VHS section is gone; the history table stays.
**Regression guard.** Keep a `## Storyboards` heading — `cmd/demorig/main.go` (`newRootCommand` Long), `cmd/demorig/storyboard.go` (`Storyboard` doc) and `docs/manual/building.md` (`make demorig` row) point at it; if it is renamed, update every hit of `git grep -n 'graphics/demo/README'` outside CHANGELOG, docs/plans and .beads.
**Files:** graphics/demo/README.md
**Read first:** graphics/demo/README.md — Quick start, Storyboards, History; docs/manual/building.md — `make demorig` row; cmd/demorig/main.go — newRootCommand; cmd/demorig/storyboard.go — Storyboard
**Tests.** none.
**Acceptance.** `! grep -niE '\bvhs\b|\.tape\b|type\.sh|gen\.sh|record\.sh' graphics/demo/README.md | grep -v 'history/' && grep -q '^## Storyboards' graphics/demo/README.md`
**Commit:** `docs(demo): the v2 recording rig`

## 16. Capture, record and ship the new hero clip

**What.** Depends on items 14, 15. Needs `OPENROUTER_API_KEY`, `ffmpeg`, `gifsicle` on the host; missing any → stop and report, never substitute.
**Goal:** `graphics/demo/storyboards/hero.cassette` holds a capture whose `demorig check` passes; `graphics/demo.gif` is the render of a replayed take (1250 px wide); `graphics/demo/history/<today>-hero/` holds `demo.gif` and `NOTES.md` (model, alias, endpoint, apogee commit, storyboard, cassette, render line, capture attempts); the README history table gains the row.
**Approach (assumed at the header base):** `./setup.sh`; `until go run ./cmd/demorig capture …; do :; done` (budget 8); `demorig record`; `demorig render`.
**Regression guard.** Build `apogee` from HEAD into the work dir and put it first on PATH through `setup.sh` before capture and record — the host's PATH `apogee` is brew v0.23.0, which predates plan 01 and would film the old one-row header; `NOTES.md` records that binary's `apogee --version`. Rewrite the hero `<img alt>` in `README.md` for the v2 scenario (the current alt describes the dropped `/undo` close).
**Files:** graphics/demo/storyboards/hero.cassette, graphics/demo.gif, graphics/demo/history/, graphics/demo/README.md, README.md
**Read first:** graphics/demo/setup.sh — env.sh PATH line; README.md — hero `<img>` alt; graphics/demo/README.md — History table; graphics/demo/history/2026-08-24-hero/NOTES.md — table shape; cmd/demorig/render.go — optimizeGIF
**Tests.** none.
**Acceptance.** `go run ./cmd/demorig check graphics/demo/storyboards/hero.yaml && ffprobe -v error -show_entries stream=width -of csv=p=0 graphics/demo.gif | grep -qx 1250`
**Commit:** `docs(readme): ship the v2 hero clip and record its history entry`
