# Plan: photon adoption, the apogee side (prep)

**Goal:** give apogee everything photon needs to film the hero clip — `stubllm` cassette services,
a `sessionexpect` judge for `run:` expects, rig scripts that read `PHOTON_WORK`, and a linting
`photon.yaml` + `hero.photon.yaml` — while `cmd/demorig` keeps filming from `hero.yaml`.
**Date:** 2026-10-17 · **Status:** unexecuted · **sized for:** ~200k-context host
**base:** 2a687386

**Sources:**
- capcom issues `adopt-photon` and its children (`capcom show <id>`)
- `docs/handoffs/2026-10-17 - 00 - photon-adoption.md`
- `~/repos/photon/docs/plans/2026-10-17 - 00 - photon-v1-plan.md` — header and items 5, 6, 9, 14–17
  (read only; never edit `~/repos/photon`), `~/repos/photon/internal/config/` (schema as built)
- `cmd/demorig/` at the base — the behaviour every port reproduces

**Ratified design calls** (owner, 2026-10-17):
- **Helper home:** `cmd/sessionexpect`, a standalone dev tool with its own `make` target, never a release asset.
- **Fonts:** `graphics/demo/fonts/` is deleted in `film-hero-retire-demorig`, not here; `photon.yaml` sets no `fonts:`.
- **Tool binaries:** `setup.sh` builds `stubllm` and `sessionexpect` into `$WORK/bin`; `photon.yaml` calls them there.
- **Storyboard bridge:** the photon storyboard is a sibling `storyboards/hero.photon.yaml`; `hero.yaml` stays demorig's until retirement renames the sibling over it.
- **Selectors:** `kind=…,text=…,tool=…,target=…,nth=N|last` — demorig's `EntrySelector`, no `contains` key; `--before`/`--after` take a selector.
- **Port:** the model service is fixed at 18181 (`setup.sh` bakes it into apogee's config); stubllm takes it as `--listen`, no new `--port` flag.
- **Work-dir precedence:** `APOGEE_DEMO_WORK` before `PHOTON_WORK` in the rig scripts until retirement; `photon.yaml` runs them with it unset (writer, 2026-10-17, from regression check).

**Standing requirements:**
- skills: coding-standards
- This box: `GOMEMLIMIT=2GiB`, one package per `go test`, never a package pattern, never two runs at once; `cmd/apogee` and `internal/tui` only with `-run`.
- `cmd/demorig` is untouched; `demorig lint graphics/demo/storyboards/hero.yaml` still passes after every item.

**Out of scope:** filming, rendering, deleting `cmd/demorig` or `graphics/demo/fonts/`
(`film-hero-retire-demorig`); any change to photon.

**Regression check (2026-10-17, 2a687386):**
- 1: guard folded (replay test cassette built by a capture round-trip or `hero.cassette`).
- 2: guard folded (stub `main.go` per writer decision; Sub-Agent entry position and re-derived indices).
- 3: guard folded (selector and required-flag checks before `runE`; unset `PHOTON_WORK` is a usage error).
- 4: recast (writer decision: `APOGEE_DEMO_WORK` before `PHOTON_WORK`; Acceptance covers both scripts and the README).
- 5: guard folded (writer decision: `env -u APOGEE_DEMO_WORK` setup/reset, photon work dir in README; `go -C` build, `ready: {tcp: true}`, Acceptance for the expects and the tool builds).
- 4 (round 2): guard folded (setup.sh's closing hint passes `--work "$WORK"` when `APOGEE_DEMO_WORK` is unset, README says demorig reads only `APOGEE_DEMO_WORK`/`--work`; Acceptance adds the default-fallback case); yields to `cmd/demorig/main.go:66-68` (cmd/demorig untouched).
- 5 (round 2): guard folded (writer decision: photon.yaml's setup/reset also unset `APOGEE_DEMO_PORT`, keeping setup.sh's port at the service's fixed 18181).

## 1. stubllm: `replay` and `capture` cassette subcommands

**What:**
**Goal:** `stubllm replay --cassette <file> [--listen]` serves a cassette; `stubllm capture --upstream <url> [--key-env <VAR>] --out <file> [--listen]` proxies to a live model and writes the recorded cassette when its context ends (SIGINT or SIGTERM); both print `listening <url>` once bound; `docs/design/test-drivers.md` documents both.
**Approach (assumed at the header base):** extend `cmd/stubllm/main.go` beside `newServeCommand`/`newRecordCommand`, reusing `runE`, `interruptible`, `announce` and `defaultListen`. `replay`: `stubllm.LoadCassette` → `stubllm.NewReplayer(c, 1)` (pace 1, as demorig's `newRecordCommand`) behind an `http.Server`. `capture`: modelled on `newRecordCommand` — `stubllm.NewCassetteRecorder(upstream, keyEnv)`, serve, on `ctx.Done()` `Shutdown(shutdownGrace)` then `recorder.Cassette().Save(out)` and `cassette: <path>` on stderr. Pre-check `--out`'s directory before listening (port demorig's `checkCassetteDir`). No new package; the shared listen/serve/shutdown sequence becomes one helper if three commands would repeat it.
**Regression guard.** A replay test never hand-builds a cassette: `Exchange.Key` must equal the unexported `cassetteKey` (internal/stubllm/cassette.go), so a made-up key is refused with HTTP 500 (`refuseUnknown`). Build the replay cassette by a capture round-trip (`stubllm.NewCassetteRecorder` over an in-process upstream's handler, or the new `capture` subcommand) then `Save`, or serve `graphics/demo/storyboards/hero.cassette`.
**Files:** cmd/stubllm/main.go, cmd/stubllm/main_test.go, docs/design/test-drivers.md
**Read first:** cmd/stubllm/main.go — newRecordCommand, newServeCommand, runE, interruptible, announce, shutdownGrace; internal/stubllm/capture.go — NewCassetteRecorder, Cassette; internal/stubllm/replay.go — NewReplayer, refuseUnknown;
internal/stubllm/cassette.go — LoadCassette, Save; cmd/demorig/record.go — newCaptureCommand, checkCassetteDir; cmd/stubllm/main_test.go — run, waitForAddress, postStream;
docs/design/test-drivers.md — "The binary: cmd/stubllm", "Recording a fixture"
**Closes:** stubllm-cassette-subcommands
**Tests:** in `main_test.go`, using its `run`/`waitForAddress` helpers: replay answers a chat request with the bytes of an exchange from a cassette made by a capture round-trip (or `hero.cassette`), never a hand-keyed one; capture against an in-process upstream writes a cassette `LoadCassette` reads back after the context is cancelled; capture with a missing `--out` dir exits 1 before listening; `--key-env` naming an empty variable exits 1.
**Acceptance:**
- `go build ./cmd/stubllm/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./cmd/stubllm/`
**Commit:** `feat(stubllm): add replay and capture cassette subcommands`

## 2. sessionexpect: the entry judge

**What:**
**Goal:** package `main` under `cmd/sessionexpect` holds a tested judge: parse a selector string, load a session file's entries, locate the selected entry, and judge `contains`, `before` and `after` against it, returning pass/fail and a one-line detail with demorig's wording (`no entry matches {…}`, `entry N <kind> …: contains "…"`).
**Approach (assumed at the header base):** port `cmd/demorig/anchors.go` (`located`, `findEntry`, `EntrySelector.matches`/`String`), `session.go` (`loadEntries`) and from `check.go` `entryContains`, `describeEntry`, `headOf`, `containsClause`, `orderClause` and the entry half of `Expect.judge`. `orderClause` details name the other selector (`{kind: interjected}`), not a beat id. `EntrySelector`, `Nth`/`NthLast` and `sessionKinds` move over without YAML tags; the parser takes `key=value` pairs split on `,`, `kind` required and checked against the session kinds (add `workflow`, `workflow_item` from `internal/session/transcript.go`), unknown keys refused. A copy, not a shared package: demorig is deleted by `film-hero-retire-demorig`.
**Regression guard.** Item 2 also adds a minimal cmd/sessionexpect/main.go (a `func main()` that prints a one-line usage to stderr and exits 2) so the package builds and vets at every commit; item 3 replaces it. The fixture's `Sub-Agent` `toolCall` sits between the user prompt and beat 5's `interjected` entry, and the ported `TestFindEntry` indices and `check_test.go` `entry N …` details (demorig pins 2/5/7/8/9) are re-derived for that position, never copied verbatim.
**Files:** cmd/sessionexpect/main.go, cmd/sessionexpect/judge.go, cmd/sessionexpect/selector.go, cmd/sessionexpect/judge_test.go, cmd/sessionexpect/selector_test.go, cmd/sessionexpect/testdata/session-hero.json
**Read first:** cmd/demorig/anchors.go — findEntry, EntrySelector.matches, EntrySelector.String, located, noEntry; cmd/demorig/check.go — Expect.judge, containsClause, orderClause, entryContains, describeEntry, headOf;
cmd/demorig/storyboard.go — EntrySelector, Nth, NthLast, sessionKinds, Expect.validate; cmd/demorig/session.go — loadEntries; internal/session/transcript.go — EntryKind* consts, DecodeTranscript, EncodeTranscript;
cmd/demorig/anchors_test.go — heroEntries, heroRecord, TestHeroFixtureIsCurrent, TestFindEntry; cmd/demorig/check_test.go — TestCheckTake_Order, TestCheckTake_ContainsAndLocation; graphics/demo/storyboards/hero.yaml — beats 4, 5, 8, 10 expect
**Tests:** port `TestFindEntry`, `TestEntrySelectorString`, `TestEntryContains_ReadsTheToolSummary`, `TestHeadOf`, the order and contains cases of `check_test.go` (indices and details re-derived for the inserted `Sub-Agent` entry); selector parse table (each key, `nth=last`, missing kind, unknown key, bad nth). The fixture is demorig's `session-hero.json` plus a `toolCall` entry labelled `Sub-Agent`, regenerated by a ported `TestHeroFixtureIsCurrent` (env `SESSIONEXPECT_UPDATE_FIXTURES`). Every hero entry expect (handoff list, beats 4, 5, 8, 10) passes on it and fails on a mutated copy.
**Acceptance:**
- `go build -o /dev/null ./cmd/sessionexpect/ && go vet ./cmd/sessionexpect/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./cmd/sessionexpect/`
**Commit:** `feat(sessionexpect): port demorig's session-entry judge`

## 3. sessionexpect: the command and its build wiring

**What:**
**Goal:** `sessionexpect --entry <selector> [--contains <s>] [--before <selector>] [--after <selector>] [--sessions <dir>]` judges the newest session and exits 0 on a pass, 1 with the one-line detail on stdout on a fail, 2 on bad usage; `--sessions` defaults to `$PHOTON_WORK/home/.apogee/sessions`; `make sessionexpect` builds `./sessionexpect`.
**Approach (assumed at the header base):** cobra root in `cmd/sessionexpect/main.go` with stubllm's `runE`/`exitCodeFor` shape. Newest session: `*.json` by newest mtime, as demorig's `newestSession`, with its `no session saved under …` error. At least one of `--contains`/`--before`/`--after` required with `--entry` (demorig's `Expect.validate` rule). Add the Makefile target and `##` help line beside `stubllm`, `/sessionexpect` and `/sessionexpect.exe` to `.gitignore` (and its comment), a row to the make table in `docs/manual/building.md`.
**Regression guard.** `runE` turns every error returned inside it into a `runError` (exit 1), so the selector parse, the `--contains`/`--before`/`--after`-required rule and the `--sessions` default run before `runE` (`PreRunE`, or a `pflag.Value` type for the selector flags) and surface as usage errors (exit 2). With `PHOTON_WORK` unset and no `--sessions`, the command is a usage error naming `PHOTON_WORK` (exit 2), never a cwd-relative `home/.apogee/sessions`.
**Files:** cmd/sessionexpect/main.go, cmd/sessionexpect/main_test.go, Makefile, .gitignore, docs/manual/building.md
**Read first:** cmd/stubllm/main.go — main, runE, runError, exitCodeFor, must; cmd/demorig/record.go — newestSession, rig.sessionsDir; cmd/demorig/storyboard.go — Expect.validate; Makefile — stubllm, demorig targets, help;
.gitignore — Go build artifacts block; docs/manual/building.md — make targets table; cmd/stubllm/main_test.go — run, syncBuffer
**Closes:** session-expect-helper
**Depends on item 2.**
**Tests:** driven through the root command against a temp sessions dir holding the fixture: pass exits 0; fail exits 1 with the detail; `PHOTON_WORK` default resolves; `PHOTON_WORK` unset without `--sessions` exits 2 naming `PHOTON_WORK`; empty dir reports `no session saved under`; `--entry` alone and a bad selector exit 2; newest-of-two picks the later mtime.
**Acceptance:**
- `make sessionexpect && ./sessionexpect --help`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./cmd/sessionexpect/`
**Commit:** `feat(sessionexpect): add the command and its make target`

## 4. Rig scripts read `PHOTON_WORK`

**What:** Recast at the regression check (2026-10-17).
**Goal:** `graphics/demo/setup.sh` and `reset.sh` resolve the work dir as `$APOGEE_DEMO_WORK`, else `$PHOTON_WORK`, else `~/.cache/apogee-demo`; `graphics/demo/README.md` names both variables.
**Approach (assumed at the header base):** change each script's `WORK=` default line to `${APOGEE_DEMO_WORK:-${PHOTON_WORK:-$HOME/.cache/apogee-demo}}` and the usage comment in `setup.sh`; the README line naming `APOGEE_DEMO_WORK`. demorig's `runTake` still sets `APOGEE_DEMO_WORK`, so its takes are unchanged.
**Regression guard.** The scripts resolve `${APOGEE_DEMO_WORK:-${PHOTON_WORK:-$HOME/.cache/apogee-demo}}` (demorig always sets APOGEE_DEMO_WORK, photon never does), so demorig takes are unchanged even with PHOTON_WORK exported; Acceptance runs the env loop over the `WORK=` line of BOTH setup.sh and reset.sh (APOGEE_DEMO_WORK wins when both are set; PHOTON_WORK alone resolves) and adds `grep -q PHOTON_WORK graphics/demo/README.md && grep -q APOGEE_DEMO_WORK graphics/demo/README.md`.
demorig reads only `APOGEE_DEMO_WORK`/`--work` (`workDir`), so setup.sh's closing "next" hint adds `--work "$WORK"` when `APOGEE_DEMO_WORK` is unset, and the README work-dir sentence says a `PHOTON_WORK`-only rig needs `--work`; the env loop gains a fourth, default-fallback case (`HOME=/h` → `/h/.cache/apogee-demo`).
The item yields to `cmd/demorig/main.go:66-68` ("exactly as the rig's scripts resolve it"): cmd/demorig stays untouched (Standing requirements), and that comment holds whenever `APOGEE_DEMO_WORK` is set or `PHOTON_WORK` is unset.
**Files:** graphics/demo/setup.sh, graphics/demo/reset.sh, graphics/demo/README.md
**Read first:** graphics/demo/setup.sh — WORK, closing echo "next"; graphics/demo/reset.sh — WORK; cmd/demorig/main.go — workDir, workDirEnv;
cmd/demorig/record.go — runTake (reset.Env sets APOGEE_DEMO_WORK), openRig; graphics/demo/README.md — work-dir paragraph
**Closes:** rig-scripts-photon-work
**Tests:** none new; the script check is the Acceptance.
**Acceptance:**
- `bash -n graphics/demo/setup.sh && bash -n graphics/demo/reset.sh`
- `for s in setup reset; do l=$(grep '^WORK=' graphics/demo/$s.sh); env -u PHOTON_WORK APOGEE_DEMO_WORK=/a bash -c "$l; echo \$WORK"; env -u APOGEE_DEMO_WORK PHOTON_WORK=/p bash -c "$l; echo \$WORK"; PHOTON_WORK=/p APOGEE_DEMO_WORK=/a bash -c "$l; echo \$WORK"; env -u PHOTON_WORK -u APOGEE_DEMO_WORK HOME=/h bash -c "$l; echo \$WORK"; done` prints `/a`, `/p`, `/a`, `/h/.cache/apogee-demo` for each script
- `grep -q PHOTON_WORK graphics/demo/README.md && grep -q APOGEE_DEMO_WORK graphics/demo/README.md`
- `grep -q -- '--work "\$WORK"' graphics/demo/setup.sh` (the closing hint)
- `GOMEMLIMIT=2GiB go test -count=1 ./cmd/demorig/`
**Commit:** `chore(demo): let the rig scripts read PHOTON_WORK`

## 5. `photon.yaml`, `hero.photon.yaml` and the rig's tool builds

**What:**
**Goal:** `photon lint graphics/demo/storyboards/hero.photon.yaml` passes with `graphics/demo/photon.yaml` found by walk-up; every demorig expect in `hero.yaml` has a photon equivalent in the sibling; `setup.sh` builds `stubllm` and `sessionexpect` into `$WORK/bin`.
**Approach (assumed at the header base):** `photon.yaml`: `name: apogee-demo`; `setup: env -u APOGEE_DEMO_WORK -u APOGEE_DEMO_PORT ./setup.sh`, `reset: env -u APOGEE_DEMO_WORK -u APOGEE_DEMO_PORT ./reset.sh`; `launch.command: bash -c '. "$PHOTON_WORK/env.sh" >/dev/null && exec apogee'`, `launch.strip: ['APOGEE_*']`, `launch.env: {APOGEE_NO_UPDATE_CHECK: "1"}` (demorig's `ambientApogeeEnv`/`takeNoUpdateCheck`); `end.keys: [ctrl+c, ctrl+c]`; no `theme` (photon's defaults equal demorig's Mocha set), no `fonts`. Service `model`: `port: 18181`, `ready: {tcp: true}`, command `"$PHOTON_WORK/bin/stubllm" replay --cassette storyboards/hero.cassette --listen "127.0.0.1:$PHOTON_PORT"`; profile `capture` overrides it with `… capture --upstream https://openrouter.ai/api --key-env OPENROUTER_API_KEY --out "$PHOTON_WORK/hero.cassette" --listen …` and `on_success: cp "$PHOTON_WORK/hero.cassette" storyboards/hero.cassette`. Areas from demorig's `areaRows`/`lowestRowOpeningWith`, as nested anchors: `footer` = the row above the last `^ *▁`; `status` = the row below the last `^ *▔` above the last `^ *╭`; `transcript` = everything above that `▔`. `hero.photon.yaml`: `hero.yaml` minus `cassette`/`fonts`; `expect: {stage: dirty}` → take-level `run: test -n "$(git -C "$PHOTON_WORK/home/Repos/taskman" status --porcelain)"`; each entry expect → `run: "$PHOTON_WORK/bin/sessionexpect" …` (beat 10's `after: 5` → `--after 'kind=interjected'`); a header comment says it replaces `hero.yaml` at retirement. `setup.sh` adds `go -C "$REPO" build -o "$WORK/bin/" ./cmd/stubllm ./cmd/sessionexpect` from the repo root it already locates. README: one short "photon (in transition)" paragraph.
**Regression guard.** photon.yaml also unsets APOGEE_DEMO_PORT for both scripts — `setup: env -u APOGEE_DEMO_WORK -u APOGEE_DEMO_PORT ./setup.sh` and `reset: env -u APOGEE_DEMO_WORK -u APOGEE_DEMO_PORT ./reset.sh` — so an exported demorig port never reaches the photon rig's config (setup.sh's `PORT=${APOGEE_DEMO_PORT:-18181}` must match the service's fixed port 18181). The README paragraph names photon's default work dir `~/.cache/apogee-demo-photon` (photon.yaml sets no `work:`), separate from demorig's `~/.cache/apogee-demo`. setup.sh runs from any cwd (demorig's `setupRig` execs it from `cmd/demorig`) and `-o` with two packages needs a directory, so the build is `go -C "$REPO" build -o "$WORK/bin/" …` (trailing slash). stubllm's `announce` prints on stderr (cobra `Printf` → `OutOrStderr`) and photon's line readiness is unbuilt, so the model service uses `ready: {tcp: true}` on the fixed port 18181.
**Files:** graphics/demo/photon.yaml, graphics/demo/storyboards/hero.photon.yaml, graphics/demo/setup.sh, graphics/demo/README.md
**Read first:** ~/repos/photon/internal/config/config.go — Config, Launch, End, Load; ~/repos/photon/internal/config/service.go — Service, Ready, Profile, ServiceOverride; ~/repos/photon/internal/config/area.go — Area, Anchor;
~/repos/photon/internal/storyboard/beat.go — Beat, Expect, Target, Nth; ~/repos/photon/internal/clip/clip.go — workDir, crossCheck; cmd/demorig/engine.go — areaRows, lowestRowOpeningWith;
cmd/demorig/record.go — ambientApogeeEnv, takeNoUpdateCheck, quitKeys, runTake; cmd/stubllm/main.go — announce, defaultListen
**Closes:** hero-photon-config
**Depends on items 1, 3, 4.**
**Tests:** none in Go; lint is the check. Build photon from its checkout first (`cd ~/repos/photon && go build -o ~/go/bin/photon ./cmd/photon`).
**Acceptance:**
- `photon lint graphics/demo/storyboards/hero.photon.yaml` exits 0
- `go run ./cmd/demorig lint graphics/demo/storyboards/hero.yaml` exits 0
- `bash -n graphics/demo/setup.sh`
- `H=graphics/demo/storyboards/hero.photon.yaml; test "$(grep -c '/bin/sessionexpect' $H)" -eq 5 && grep -q 'status --porcelain' $H && grep -q 'seen:' $H` (beats 4, 5, 8, 10×2; the take-level stage run; beat 2's `seen`)
- `W=$(mktemp -d); APOGEE_DEMO_WORK=$W graphics/demo/setup.sh && test -x $W/bin/stubllm -a -x $W/bin/sessionexpect`
- `grep -q 'apogee-demo-photon' graphics/demo/README.md`
- `grep -qF 'env -u APOGEE_DEMO_WORK -u APOGEE_DEMO_PORT ./setup.sh' graphics/demo/photon.yaml && grep -qF 'env -u APOGEE_DEMO_WORK -u APOGEE_DEMO_PORT ./reset.sh' graphics/demo/photon.yaml`
**Commit:** `feat(demo): add photon.yaml and the hero storyboard in photon's schema`
