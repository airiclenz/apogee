# Plan: film hero with photon, retire cmd/demorig

**Goal:** apogee's hero clip is filmed only by photon: `cmd/demorig`, its build target, its fonts
and every demorig-era knob in the rig are gone, `storyboards/hero.yaml` is photon's storyboard,
and a photon-rendered review GIF is ready for the owner to compare with `graphics/demo.gif`.
**Date:** 2026-10-17 · **Status:** unexecuted · **sized for:** ~200k-context host
**base:** ae7c5040

**Regression check (2026-10-17, ae7c5040):**
- 1: guard folded (CI filter takes `graphics/demo/stage/**` back for gofmt).
- 2: guard folded (doc link `[ClampMargins]` in `Screen.Resize`; repo-wide `ClampMargins` grep).
- 3: recast (every `APOGEE_DEMO_*` knob goes; bash invocation; `--work` hint rule); supersedes CHANGELOG.md [Unreleased] rig entries.
- 5: guard folded (no `rig.env` / `~/.cache/apogee-demo/` paths; `make install` first in Quick start).
- 3: guard folded (re-check: every `mktemp -d` rig in Acceptance is removed with `rm -rf "$W"`, also on failure; setup.sh/reset.sh fall back to `~/.cache/apogee-demo-photon` when `PHOTON_WORK` is unset, checked without building a rig).
- 4: guard folded (re-check, from item 3's decision: the standing `PHOTON_BIN` temp dir is removed at the item's end).

**Sources:**
- `.capcom/issues/adopt-photon.md`, `.capcom/issues/adopt-photon/film-hero-retire-demorig.md`
- `docs/plans/archived/2026-10-17 - 01 - photon-adoption-prep-plan.md` (prep decisions)
- photon at `9e93ad9` (`github.com/airiclenz/photon`): `README.md`; `docs/plans/archived/2026-10-17 - 00 - photon-v1-plan.md` header and items 5, 6, 14–17, 23

**Ratified design calls** (owner, 2026-10-17):
- **Shipping the GIF:** the run renders a review GIF outside the repo only; `graphics/demo.gif` is overwritten by the owner after a side-by-side look, never by this plan.
- **Rig work dir:** `setup.sh`/`reset.sh` read `PHOTON_WORK`, else `~/.cache/apogee-demo-photon` (photon's default for `name: apogee-demo`); `APOGEE_DEMO_WORK` is gone.
- **Rune guard:** `TestRasterResolvesEveryTUIRune` is dropped with demorig, not ported; the fonts are photon's.
- **EPERM issue:** `demorig-eperm-root-cause` closes as moot: the code is gone, and photon carries the same retry and probe and records one take per process.
- **Demo knobs:** every `APOGEE_DEMO_*` knob and `rig.env` go; host alias, model and port 18181 are fixed in `setup.sh` / `photon.yaml` (owner, 2026-10-17).

**Standing requirements:**
- skills: coding-standards
- **External gate (2026-10-17):** photon's `render` OOM-kills ffmpeg on this 8 GB box (single-pass `split`+`palettegen` buffers every frame, ~4–5 GB for hero). `c8c2cfb545269a9ab194645cfb6c76c2e02ec33c` is the first photon commit whose render runs in bounded memory, tracked as photon issue `render-memory-bound` (filed 2026-10-17, unfixed). Before executing, replace `c8c2cfb545269a9ab194645cfb6c76c2e02ec33c` with that hash; until then item 4 cannot pass and the run stops there.
- photon for any Acceptance: `PHOTON_BIN=$(mktemp -d) && GOBIN="$PHOTON_BIN" go install github.com/airiclenz/photon/cmd/photon@c8c2cfb545269a9ab194645cfb6c76c2e02ec33c` (offline: `go build -o "$PHOTON_BIN/photon" ./cmd/photon` inside `~/repos/photon`). Never edit `~/repos/photon`.
- Never run `photon render` without `-o`: its default output is `graphics/demo.gif`.
- Raspberry Pi box: no `go test -race` over a whole package or pattern; one test run at a time; `GOMEMLIMIT=2GiB`.
- Deviations from item text land as dated NOTES lines under the item.

**Out of scope:**
- Overwriting `graphics/demo.gif`; closing `film-hero-retire-demorig` and `adopt-photon` (the owner does both after the comparison).
- Any change in `~/repos/photon`.
- Historical records: released `CHANGELOG.md` sections, `docs/plans/archived/`, `docs/reviews/`, ADR 0083, closed issues.

## 1. Delete cmd/demorig and its build surface — ✅ DONE (2026-10-17)

NOTES (2026-10-17): ci.yml's filter comment reflowed to keep its lines under the old width after the `graphics/demo/stage/**` wording replaced `graphics/demo/** (cmd/demorig's tests)`.

**What:**
**Goal:** no `cmd/demorig` directory, no `demorig` Makefile target or `.gitignore` entry, no `make demorig` row in `docs/manual/building.md`, no `cmd/demorig` rows in `scripts/test-timings.seed`, `golang.org/x/image` gone from `go.mod`/`go.sum`, and the CI path filter skips `graphics/demo/**`.
**Approach (assumed at the header base):** `git rm -r cmd/demorig`. Drop the `## demorig:` help line, `.PHONY: demorig` and its recipe from `Makefile`. In `.gitignore` drop `/demorig`, `/demorig.exe` and `cmd/demorig` from the binaries comment. Drop the `make demorig` row from the make-targets table in `docs/manual/building.md`. Delete every line naming `cmd/demorig` from `scripts/test-timings.seed` (`grep -v`; it is not regenerated). Run `go mod tidy`; the only expected change is dropping `golang.org/x/image`. In `.github/workflows/ci.yml`, replace the `graphics/demo/**` "(cmd/demorig's tests)" filter comment and add `!graphics/demo/**`, since no Go test reads that tree any more. Drop the now-redundant `!graphics/demo/README.md` and keep `!graphics/demo.gif`. `checkCassetteDir` in `cmd/stubllm` stays as it is; it is no longer a duplicate.
**Regression guard.** `gofmt -l .` (ci.yml's gofmt step, `make check`) still walks `graphics/demo/stage/{task.go,task_test.go}`, so directly after `!graphics/demo/**` add a positive `- 'graphics/demo/stage/**'` line, and in the filter comment name it "graphics/demo/stage/** (gofmt -l . reads stage's Go)" in place of "graphics/demo/** (cmd/demorig's tests)".
**Files:** cmd/demorig/, Makefile, .gitignore, docs/manual/building.md, scripts/test-timings.seed, go.mod, go.sum, .github/workflows/ci.yml
**Read first:** .github/workflows/ci.yml — on.push.paths, gofmt step; Makefile — demorig target, check (gofmt -l .); .gitignore — Go build artifacts block;
scripts/test-timings.seed — cmd/demorig rows; docs/manual/building.md — make-targets table; graphics/demo/stage — task.go
**Closes:** demorig-eperm-root-cause

**Tests:** none new; the deleted package takes its tests with it.

**Acceptance:**
- `test ! -e cmd/demorig && ! grep -n demorig Makefile .gitignore docs/manual/building.md scripts/test-timings.seed`
- `! grep -n 'golang.org/x/image' go.mod go.sum`
- `go build ./... && go vet ./... && GOOS=windows go vet ./...`
- `make -n stubllm sessionexpect >/dev/null && ! make -n demorig 2>/dev/null`

**Commit:** `chore(demo): retire cmd/demorig`

## 2. Repoint code comments that cite demorig — ✅ DONE (2026-10-17)

**What:**
**Goal:** no Go source outside the archived history cites `cmd/demorig` or a demorig test, and `ClampMargins` in `internal/tuitest` is unexported because its only outside caller is gone.
**Approach (assumed at the header base):** Depends on item 1. Rule: every comment in a `.go` file naming `demorig` is reworded to stand on its own, or to name photon where that is the provenance. Find them with `grep -rn demorig --include='*.go' .`; at base they are in `internal/tuitest/screen.go` (`ClampMargins` doc), `internal/tui/clipboard.go` (the byte-slice prefixes cite `TestRasterResolvesEveryTUIRune`), and `cmd/sessionexpect/{main.go,selector.go,judge.go,judge_test.go}` (ported-from notes). A citation of `hero.yaml`'s expects stays, since the file survives. Rename `tuitest.ClampMargins` to `clampMargins` and update its in-package callers.
**Regression guard.** Update its in-package callers and doc links (`grep -n ClampMargins internal/tuitest/`) — the `[ClampMargins]` doc link in `Screen.Resize`'s comment (screen.go:240) is a reference, not a caller — and Acceptance carries `! grep -rn ClampMargins --include='*.go' .`.
**Files:** internal/tuitest/screen.go, internal/tui/clipboard.go, cmd/sessionexpect/main.go, cmd/sessionexpect/selector.go, cmd/sessionexpect/judge.go, cmd/sessionexpect/judge_test.go
**Read first:** internal/tuitest/screen.go — ClampMargins, NewScreen, Screen.Resize; internal/tui/clipboard.go — imageSignatures; cmd/sessionexpect/main.go — package doc;
cmd/sessionexpect/selector.go — selector doc; cmd/sessionexpect/judge.go — clauses; cmd/sessionexpect/judge_test.go — heroEntries

**Tests:** existing `cmd/sessionexpect` and `internal/tuitest` tests.

**Acceptance:**
- `! grep -rn demorig --include='*.go' .`
- `! grep -rn ClampMargins --include='*.go' .`
- `go build ./... && go vet ./internal/tuitest/ ./internal/tui/ ./cmd/sessionexpect/`
- `GOMEMLIMIT=2GiB go test -count=1 ./cmd/sessionexpect/ ./internal/tuitest/`

**Commit:** `refactor(demo): drop demorig citations and unexport clampMargins`

## 3. Rig scripts and photon.yaml without demorig knobs — ✅ DONE (2026-10-17)

NOTES (2026-10-17): photon.yaml's launch command is single-quoted YAML (`'. "$PHOTON_WORK/env.sh" >/dev/null && exec apogee'`), matching the file's other commands; the parsed value is exactly the plan's command.
NOTES (2026-10-17): graphics/demo/README.md still names `rig.env` and the `APOGEE_DEMO_*` knobs; it is item 5's (not yet done), so it was left untouched.

**What:** Recast at the regression check (2026-10-17).
**Goal:** `graphics/demo/setup.sh` and `reset.sh` read only `PHOTON_WORK` (default `~/.cache/apogee-demo-photon`), use the fixed port 18181, write no `rig.env`, and setup's closing hint names `photon record graphics/demo/storyboards/hero.yaml`. `graphics/demo/photon.yaml` runs them bare and mentions no demorig.
**Approach (assumed at the header base):** Depends on item 1. In both scripts, `WORK="${PHOTON_WORK:-$HOME/.cache/apogee-demo-photon}"`. In `setup.sh`: `PORT=18181` with a comment tying it to `photon.yaml`'s `services.model.port`; drop every `APOGEE_DEMO_*` header row, the `rig.env` write and its comment, and the demorig wording in the server and `env.sh` comments. The hint becomes one `echo` of `next (from the repo root): photon record graphics/demo/storyboards/hero.yaml`, with ` --work "$WORK"` appended after the storyboard path whenever `WORK` is not `$HOME/.cache/apogee-demo-photon`, and omitted for the default. In `photon.yaml`: `setup: ./setup.sh`, `reset: ./reset.sh`, launch command `. "$PHOTON_WORK/env.sh" >/dev/null && exec apogee` (photon runs it by `sh -c`; `env.sh` is POSIX), and rewrite the header and setup comments without demorig or `hero.photon.yaml`.
**Regression guard.** setup.sh drops every APOGEE_DEMO_* knob, not only WORK and PORT — APOGEE_DEMO_HOST_ALIAS and APOGEE_DEMO_MODEL become fixed values in the script (the same defaults as at base), with a comment that they must match the committed cassette; record this in the header's Ratified design calls by widening the "Port knob" line to "Demo knobs: every APOGEE_DEMO_* knob and rig.env go; host alias, model and port 18181 are fixed in setup.sh / photon.yaml (owner, 2026-10-17)". Item 5's grep for APOGEE_DEMO then holds too.
Both scripts are bash (`${BASH_SOURCE[0]}`; `/bin/sh` is dash here): check them with `bash -n` and run setup as photon does, `(cd graphics/demo && PHOTON_WORK="$W" ./setup.sh)`. `photon setup --work` exports `PHOTON_WORK`, so the hint names the work dir by comparing `WORK` with the default, never by whether `PHOTON_WORK` was set.
This supersedes CHANGELOG.md [Unreleased] "The demo rig scripts read `PHOTON_WORK`" (the `APOGEE_DEMO_WORK` precedence) and "The demo rig speaks photon" (the `env -u` of `APOGEE_DEMO_WORK`/`APOGEE_DEMO_PORT`); the item's changelog entry says so.
Every Acceptance command that builds a throwaway rig with `mktemp -d` removes it afterwards (`rm -rf "$W"`, also on failure) — /tmp is RAM on this box; item 4's PHOTON_BIN temp dir from the standing requirements likewise is removed at the item's end.
With `PHOTON_WORK` unset, both scripts' `WORK=` line falls back to `$HOME/.cache/apogee-demo-photon` (a hand-run `<work>/reset.sh` relies on it); Acceptance checks that by evaluating each script's `WORK=` line under `env -u PHOTON_WORK HOME=/h`, without building a rig.
**Files:** graphics/demo/setup.sh, graphics/demo/reset.sh, graphics/demo/photon.yaml
**Read first:** graphics/demo/setup.sh — WORK/PORT/HOST_ALIAS/MODEL, rig.env printf, closing hint, server and env.sh heredoc comments; graphics/demo/reset.sh — WORK; graphics/demo/photon.yaml — setup, reset, launch.command, services.model.port;
~/repos/photon/internal/clip/clip.go — workDir ($HOME/.cache/<name>-photon via os.UserHomeDir); ~/repos/photon/internal/rig/setup.go — runCommand, workVars (PHOTON_WORK exported, operator env kept);
~/repos/photon/internal/rig/record.go — terminal.Start(sh -c Launch.Command); CHANGELOG.md — [Unreleased] "The demo rig scripts read PHOTON_WORK", "The demo rig speaks photon"

**Tests:** `bash -n` on both scripts; photon lint in item 4. The Acceptance rig is removed afterwards. The `PHOTON_WORK`-unset default of both scripts is checked from their `WORK=` lines.

**Acceptance:**
- `bash -n graphics/demo/setup.sh && bash -n graphics/demo/reset.sh`
- `! grep -n 'APOGEE_DEMO\|demorig\|rig.env\|hero.photon' graphics/demo/setup.sh graphics/demo/reset.sh graphics/demo/photon.yaml`
- `fail=0; for s in setup reset; do env -u PHOTON_WORK HOME=/h bash -c "$(grep '^WORK=' graphics/demo/$s.sh); echo \$WORK" | grep -qx /h/.cache/apogee-demo-photon || fail=1; done; [ "$fail" -eq 0 ]` (the default work dir, no rig built)
- `W=$(mktemp -d) && { (cd graphics/demo && PHOTON_WORK="$W" ./setup.sh) | grep -F 'photon record graphics/demo/storyboards/hero.yaml' && test ! -e "$W/rig.env" && grep -q 18181 "$W"/home/.apogee/*; ok=$?; rm -rf "$W"; [ "$ok" -eq 0 ]; }` (setup builds the rig; run from `graphics/demo` by its bash shebang, as photon does; the rig is removed pass or fail)

**Commit:** `chore(demo): drop demorig's work-dir and port knobs from the rig`

## 4. hero.yaml becomes photon's storyboard; fonts go; review take filmed

**What:**
**Goal:** `graphics/demo/storyboards/hero.yaml` is photon's storyboard and lints clean, `hero.photon.yaml` and `graphics/demo/fonts/` are gone, and a full photon record and render of it succeeds, leaving the review GIF at `~/.cache/apogee-demo-photon/hero-review.gif` and `graphics/demo.gif` untouched.
**Approach (assumed at the header base):** Depends on item 3 and on the header's external gate (photon `c8c2cfb545269a9ab194645cfb6c76c2e02ec33c`). `git mv -f graphics/demo/storyboards/hero.photon.yaml graphics/demo/storyboards/hero.yaml`. Rewrite the header so it describes the clip and its photon schema, with no "sibling"/"replaces" transition text. Repoint the beat notes that cite `cmd/demorig/compose.go` (900 ms glide) and `cmd/demorig/typing.go` to photon's cursor glide and humanized typing, by behaviour and without repo paths. Correct the frame comment: 135 × 44 cells rasterize to ≈ 4988 × 3296 px at scale 2. Beats, `frame:`, `ship:` and expects stay byte-for-byte. `git rm -r graphics/demo/fonts`.
**Files:** graphics/demo/storyboards/hero.yaml, graphics/demo/storyboards/hero.photon.yaml, graphics/demo/fonts/
**Read first:** graphics/demo/storyboards/hero.photon.yaml — header, frame comment, beat 2 notes (glide), beat 3 notes (typing); ~/repos/photon/internal/compose/cursor.go — cursorGlide (900 ms);
~/repos/photon/internal/compose/compose.go — output height rounding (1250x826); ~/repos/photon/internal/render/render.go — Padding*Scale geometry; graphics/demo/photon.yaml — model service cassette path

**Tests:** photon lint, plus one real take as Acceptance. A failed beat is a retake (up to 3 attempts); after that, stop.

**Acceptance:**
- `test ! -e graphics/demo/storyboards/hero.photon.yaml && test ! -e graphics/demo/fonts && ! grep -n 'demorig\|hero.photon' graphics/demo/storyboards/hero.yaml`
- `"$PHOTON_BIN/photon" lint graphics/demo/storyboards/hero.yaml` prints `ok — clip hero, 11 beat(s)`
- `PATH="$PHOTON_BIN:$PATH"; photon setup graphics/demo/storyboards/hero.yaml && photon record graphics/demo/storyboards/hero.yaml && photon render graphics/demo/storyboards/hero.yaml -o "$HOME/.cache/apogee-demo-photon/hero-review.gif"`: all exit 0, and the check table has no failed row
- `gifsicle --info "$HOME/.cache/apogee-demo-photon/hero-review.gif" | grep -F 'logical screen 1250x826'` and `git diff --exit-code graphics/demo.gif`
- Put the render's `<path> <size> <len>s` line in the item's NOTES, next to the old GIF's 54.0 s / 810 frames.
- At the item's end, pass or fail: `rm -rf "$PHOTON_BIN"` (/tmp is RAM on this box).

**Commit:** `chore(demo): make hero.yaml photon's storyboard and drop the demo fonts`

## 5. Docs point at photon

**What:**
**Goal:** `graphics/demo/README.md` documents the rig as filmed by photon: install, quick start, layout, the model service and its capture profile, the session expects, recording a new clip. It has no demorig command, no `APOGEE_DEMO_*` knob, no fonts section and no "in transition" paragraph, and its History keeps the past tense. `README.md`'s hero alt text describes only what the clip shows.
**Approach (assumed at the header base):** Depends on item 4. Rewrite in place, keeping the section order where a section still applies. Quick start: `make install`, `go install github.com/airiclenz/photon/cmd/photon@latest`, then `photon setup`/`record`/`render` on `storyboards/hero.yaml`, `record --profile capture` (needs `OPENROUTER_API_KEY`), and "`render` writes `graphics/demo.gif`; pass `-o` to review first". The storyboard and config schema is photon's: link photon's README rather than restating it, and keep only apogee's own parts (`photon.yaml`'s areas, the `model` service, `sessionexpect` expects, `strip: ['APOGEE_*']`). Drop "Fonts and licences" (photon embeds them). Drop `APOGEE_DEMO_E2E`. In `README.md`'s `<img alt>`, drop "zooms on its context gauge": the storyboard's zoom beat is cut from the render.
**Regression guard.** Every rig path names the photon work dir and no `rig.env` (README:19,71 name `rig.env`; README:80,122-123 name `~/.cache/apogee-demo/`), and Acceptance 1's forbidden pattern carries `` rig\.env\|cache/apogee-demo[/`] ``. The Quick start's first step stays `make install` — env.sh bakes `command -v apogee`, so the rig films the apogee on your PATH.
**Files:** graphics/demo/README.md, README.md
**Read first:** graphics/demo/README.md — Quick start, "photon (in transition)" paragraph, Layout table, "How a take works" paths, Fonts and licences;
README.md — hero `<img alt>`; graphics/demo/setup.sh — env.sh PATH from `command -v apogee`; ~/repos/photon/README.md — config reference (link target)

**Tests:** grep checks; no doc drift test covers these files.

**Acceptance:**
- `` ! grep -n 'demorig\|APOGEE_DEMO\|hero.photon\|in transition\|fonts/\|rig\.env\|cache/apogee-demo[/`]' graphics/demo/README.md | grep -v -i '^[0-9]*:.*history' `` (History-table rows may name demorig, in the past tense)
- `grep -n 'go install github.com/airiclenz/photon/cmd/photon' graphics/demo/README.md`
- `! grep -n 'zooms on its context gauge' README.md`

**Commit:** `docs(demo): document the rig as filmed by photon`

## Owner step after the run

Compare `~/.cache/apogee-demo-photon/hero-review.gif` with `graphics/demo.gif` side by side. If it is good, run `photon render graphics/demo/storyboards/hero.yaml` (no `-o`), commit the GIF, then run `capcom close film-hero-retire-demorig` and `capcom close adopt-photon`.
