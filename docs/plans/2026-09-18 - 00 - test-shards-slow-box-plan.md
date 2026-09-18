# Plan — test-shards.sh runs on a slow box and on a kernel without TSan

**Goal:** `make test` (scripts/test-shards.sh) becomes runnable on a Raspberry Pi 4 — a box whose kernel cannot run the race detector (39-bit VA, TSan needs 48) and whose cores are 3–5× slower than the 9-core box the shard plan is tuned for — through two explicit opt-in knobs, without changing the default plan CI and dev boxes run.
**Date:** 2026-09-18
**Status:** unexecuted
**Sized for:** ~200k-context host
**Base commit:** d76457415aaf4ac6b8a00119a8f9fdef3ac39a01

**Sources:**
- `docs/handoffs/2026-09-17 - 00 - pi-test-suite-timing.md` — the measurements (Pi: `go test -parallel 2 -p 2 ./...` green; the default shard plan ≈11 concurrent tests, 8 fail; TSan `FATAL: Found 39 - Supported 48`).
- `scripts/test-shards.sh` — the shard plan, budget and `-parallel` bound.
- `docs/manual/building.md` §"Testing" (lines 88–117 at the base commit) — the prose contract for the script's knobs.

**Ratified design calls** (user, 2026-09-18):
- **Race opt-out:** `APOGEE_TEST_RACE=0` drops `-race`; announced on stderr and in the `==>` summary line. Any other value (unset, `1`, …) keeps `-race`.
- **Slow-box floor:** an explicit `APOGEE_TEST_SLOW=1` knob, never keyed on core count — a Pi and CI's 4-vCPU runner have the same `nproc`, and CI keeps today's plan.
- **Floor mix:** under `APOGEE_TEST_SLOW=1` each heavy package gets 1 shard (an explicit `APOGEE_TEST_SHARDS` still wins), every process runs `-parallel 1`, and the rest process also gets `-p 2` — at most 4 driven tests on the box at once, the mix measured green on the Pi.
- **Already done, not in this plan:** the handoff's step 1 (`TestE2EHostileWrapsUnderItsOwnIndent` on a host without landlock) landed in `ebc71ee8` and passes on the Pi.

**Regression check (2026-09-18, d76457415aaf4ac6b8a00119a8f9fdef3ac39a01):**
- 1: guard folded — the raced `-list` checks are TSan-capable-box only; a Pi-runnable `bash -x` pair proves the `-race` flag and the single `==> go test -race:` echo.
- 3: recast — `make check` refuses `APOGEE_TEST_RACE=0` so its `==> go test -race (sharded …)` echo stays truthful.
- 3 (re-check round): guard folded — the refusal test's leading `!` inverted the check; it now greps the refusal line (exits 0) and a separate `! APOGEE_TEST_RACE=0 make check` line proves the non-zero exit.

**Standing requirements:**
- skills: coding-standards
- The default plan (no knob set) is byte-for-byte the plan the script computes today: same shard counts, same `-parallel` and `-p`, same `-race`. Every item's acceptance proves that.
- The script's own header comment ("Usage:" block) documents each knob in the item that adds it; `docs/manual/building.md` and the Makefile `## test:` comment are item 3's alone.
- Post-closeout manual check (Pi only, not an acceptance): `APOGEE_TEST_RACE=0 APOGEE_TEST_SLOW=1 make test` is green on the Raspberry Pi 4 that motivated this plan.

**Out of scope:**
- Making TSan work on a 39-bit-VA kernel (not fixable from userland).
- Any change to test timeouts, `submit`'s 5 s wait, the leak-check grace, or any test.
- CI configuration (`.github/workflows/ci.yml` keeps `APOGEE_TEST_SHARDS=2` and nothing else).
- Re-seeding `scripts/test-timings.seed`.

## 1. `APOGEE_TEST_RACE=0` runs the sharded suite without the race detector — ✅ DONE (2026-09-18)

NOTES (2026-09-18): implemented on the Raspberry Pi 4 (39-bit VA) — the raced `-list` checks and the raced Acceptance count are TSan-capable box only and were not run here; the raced side is proven by the `bash -x` pair (first `+ go test` line carries `-race` unset and at `=1`) and the single `==> go test -race:` echo.
NOTES (2026-09-18): pre-existing, not touched — on a TSan-incapable kernel the roster failure prints an empty diagnostic: `go test -race -list` writes `FATAL: ThreadSanitizer: unsupported VMA range` to stdout (captured in `list.raw`), and the script only cats `list.err`.

**What:** In `scripts/test-shards.sh`, `GOFLAGS_TEST=(-race -count=1)` becomes conditional: when `APOGEE_TEST_RACE` is exactly `0`, `GOFLAGS_TEST=(-count=1)`; otherwise unchanged. The knob is announced twice, so an unraced run can never pass for a raced one in a log: a stderr line `test-shards: race detector OFF (APOGEE_TEST_RACE=0) — this run does not stand in for make check` printed right after the `test-shards: timings:` line, and the summary line `==> go test -race: N shards + M other packages` reads `==> go test (unraced): N shards + M other packages` in that mode. The `-list` roster call uses the same `GOFLAGS_TEST`, so it too runs unraced (a race binary aborts before listing on the kernel this is for). Timings are harvested as usual — the packer ranks, it does not compare absolutes (header comment already says so). Add the knob to the script's `Usage:` header block beside `APOGEE_TEST_SHARDS`, one line, and a ≤ 4-line comment at the conditional stating the kernel it exists for (39-bit VA arm64 rpi kernels: TSan `FATAL: Found 39 - Supported 48`) and that it is a developer convenience, never a gate — `make check` and CI stay raced.

**Regression guard.** On the Pi this plan runs on, every raced `-list` run aborts before the `==>` line: the roster call `go test "${GOFLAGS_TEST[@]}" -list '.*'` (scripts/test-shards.sh:217 at the base commit) hits `FATAL: Found 39 - Supported 48` and the script exits 1 at line 220. So the raced checks below (the default and `APOGEE_TEST_RACE=1` `-list` runs, and the raced count in Acceptance) are **TSan-capable box only**; the box this plan is executed on proves the raced side with the `bash -x` pair instead — the first `+ go test` line carries `-race` when the knob is unset and when it is `1` — and `grep -c '==> go test -race:' scripts/test-shards.sh` prints `1` (the echo is edited in place, never duplicated).

**Files:** `scripts/test-shards.sh`
**Read first:** scripts/test-shards.sh — GOFLAGS_TEST, the HEAVY_PKGS roster loop (`go test -list`), launch, the `==> go test -race:` echo, the `test-shards: timings:` echo, the Usage header; Makefile — test, check (its own `==> go test -race (sharded` echo)

**Tests:** No shell test harness exists in the repo; the checks are the script driven with `-list '.*'` (go test lists instead of running, so the whole plan is exercised in seconds and nothing is harvested):

```
bash -n scripts/test-shards.sh
# TSan-capable box only — a raced -list run aborts on the Pi before the ==> line (see the regression guard)
scripts/test-shards.sh -list '.*' 2>&1 | grep -F '==> go test -race:'                      # default unchanged
APOGEE_TEST_RACE=1 scripts/test-shards.sh -list '.*' 2>&1 | grep -F '==> go test -race:'   # only "0" opts out
# runs on any box, the Pi included
APOGEE_TEST_RACE=0 scripts/test-shards.sh -list '.*' 2>&1 | tee /tmp/unraced.log | grep -F '==> go test (unraced):'
grep -F 'test-shards: race detector OFF (APOGEE_TEST_RACE=0)' /tmp/unraced.log
bash -x scripts/test-shards.sh -list '.*' 2>&1 | grep -E '^\+ go test ' | head -1 | grep -F -- '-race'                      # default: first go test is raced
APOGEE_TEST_RACE=1 bash -x scripts/test-shards.sh -list '.*' 2>&1 | grep -E '^\+ go test ' | head -1 | grep -F -- '-race'   # =1: still raced
grep -c '==> go test -race:' scripts/test-shards.sh | grep -x 1
```

**Acceptance:** the commands above exit 0 (the two marked TSan-capable box only are run where a `-race` binary starts; the rest everywhere); additionally `APOGEE_TEST_RACE=0 bash -x scripts/test-shards.sh -list '.*' 2>&1 | grep -E '^\+ go test ' | grep -c -- '-race'` prints `0`, and — TSan-capable box only — the same pipeline without the env var prints a count equal to the number of `go test` invocations (roster lists + rest + shards). `git status --porcelain scripts/test-timings.seed` is empty after every run.

**Commit:** `build(test-shards): APOGEE_TEST_RACE=0 runs the sharded suite unraced, announced on every line that names the run`

## 2. `APOGEE_TEST_SLOW=1` caps the plan at four driven tests on the box — ✅ DONE (2026-09-18)

NOTES (2026-09-18): the slow-box checks (the `is_slow_box` helper) sit in `parallel_bound`/`rest_parallel_bound` and the shard-count branch, and the rest's `-p 2` rides an array (`rest_package_bound`) expanded on the one launch line rather than a duplicated launch line — the plan's literal "gains `-p 2` before `-parallel`" holds in the traced invocation.
NOTES (2026-09-18): default plan proven byte-identical to HEAD's on this box by diffing the `bash -x` `+ go test` traces of the HEAD script and the edited one under `APOGEE_TEST_RACE=0 -list '.*'`.

**What:** Depends on item 1. In `scripts/test-shards.sh`, when `APOGEE_TEST_SLOW` is exactly `1`: (a) each heavy package's shard count `n` is 1 unless `APOGEE_TEST_SHARDS` is set (the explicit override keeps precedence — evaluate it first, then the slow floor, then the budget formula); (b) `parallel_bound` and `rest_parallel_bound` both return 1 (the rest process's floor of 2 is overridden, not kept); (c) the rest process's `launch` line gains `-p 2` before `-parallel` — heavy shards do not get `-p` (they are single-package processes). The knob is announced on stderr, right after the `each shard runs …` line: `test-shards: slow box (APOGEE_TEST_SLOW=1): 1 shard per heavy package, every process -parallel 1, the rest -p 2`. Add the knob to the `Usage:` header, one line, and a ≤ 6-line comment beside the parallel-bound comment stating why the floor is explicit rather than sized off `nproc` (a Pi 4 and a 4-vCPU CI runner report the same core count and differ 3–5× per core; the default plan's ≈11 concurrent driven tests on the Pi turn `submit`'s 5 s echo wait and the 2 s leak grace red — load, not logic — where 4 at once is green). Under any other value the script computes exactly what it computes today.

**Files:** `scripts/test-shards.sh`
**Read first:** scripts/test-shards.sh — ncpu/budget, parallel_bound, rest_parallel_bound, the HEAVY_PKGS loop's `n=${APOGEE_TEST_SHARDS:-…}`, the rest `launch` line, the `each shard runs` echo, the Usage header; .github/workflows/ci.yml — `APOGEE_TEST_SHARDS: 2`

**Tests:**

```
bash -n scripts/test-shards.sh
# a stub nproc pins the budget to 3 on any box (the script prefers nproc over sysctl)
mkdir -p /tmp/ncpu4 && printf '#!/bin/sh\necho 4\n' >/tmp/ncpu4/nproc && chmod +x /tmp/ncpu4/nproc
PATH=/tmp/ncpu4:$PATH APOGEE_TEST_RACE=0 scripts/test-shards.sh -list '.*' 2>&1 | tee /tmp/default.log \
  | grep -F 'each shard runs 1 test(s) at a time, the rest 2 (-parallel: budget 3 over 4 processes)'
! grep -q 'slow box' /tmp/default.log
PATH=/tmp/ncpu4:$PATH APOGEE_TEST_RACE=0 APOGEE_TEST_SLOW=1 bash -x scripts/test-shards.sh -list '.*' 2>&1 | tee /tmp/slow.log \
  | grep -F 'test-shards: slow box (APOGEE_TEST_SLOW=1): 1 shard per heavy package, every process -parallel 1, the rest -p 2'
grep -F 'each shard runs 1 test(s) at a time, the rest 1 (-parallel: budget 3 over 3 processes)' /tmp/slow.log
grep -E '^\+ go test .* -p 2 -parallel 1 ' /tmp/slow.log | grep -vc -- '-run ' | grep -x 1   # exactly the rest process carries -p 2
PATH=/tmp/ncpu4:$PATH APOGEE_TEST_RACE=0 APOGEE_TEST_SLOW=1 APOGEE_TEST_SHARDS=2 scripts/test-shards.sh -list '.*' 2>&1 \
  | grep -F 'budget 3 over 5 processes'                                                    # explicit shard count still wins
```

**Acceptance:** every command above exits 0. `grep -c 'APOGEE_TEST_SLOW' scripts/test-shards.sh` ≥ 3 (usage line, the conditional, the announcement).

**Commit:** `build(test-shards): APOGEE_TEST_SLOW=1 is the slow-box plan — one shard per heavy package, four driven tests at once`

## 3. The manual and the Makefile name both knobs and the kernel that needs them — ✅ DONE (2026-09-18)

**What:** Recast at the regression check (2026-09-18). Depends on items 1 and 2. `docs/manual/building.md` §"Testing": after the paragraph ending "…capped because a hosted runner is a 4 vCPU box where each race-enabled shard carries its own memory cost." add one paragraph (≤ 10 lines) covering: `APOGEE_TEST_SLOW=1` (what it sets, that it is explicit because core count cannot tell a slow box from CI's runner, that it is for a Raspberry-Pi-class box); `APOGEE_TEST_RACE=0` (drops the race detector; exists because the Raspberry Pi OS arm64 kernel has 39-bit virtual addresses and TSan requires 48 — `FATAL: Found 39 - Supported 48` — so no `-race` binary runs there; an unraced run is announced as such and is not the `make check` gate: race-enabled verification needs another box). Makefile `## test:` comment block: add two lines naming the two knobs and pointing at the script header for their meaning. `make check` is the raced gate by definition, so the Makefile `check` target refuses to run when `APOGEE_TEST_RACE=0` is set in the environment — before its first step, it exits 1 with the stderr line `make check: APOGEE_TEST_RACE=0 is set; the gate runs raced — unset it, or run make test for an unraced pass` — and its `==> go test -race (sharded — scripts/test-shards.sh)` echo stays truthful as a result. `.github/workflows/ci.yml` is not touched.

**Regression guard.** `make check` is the raced gate by definition, so the Makefile `check` target refuses to run when `APOGEE_TEST_RACE=0` is set in the environment — before its first step, it exits 1 with the stderr line `make check: APOGEE_TEST_RACE=0 is set; the gate runs raced — unset it, or run make test for an unraced pass` — and its `==> go test -race (sharded — scripts/test-shards.sh)` echo stays truthful as a result. `make test` alone is not refused.

**Files:** `docs/manual/building.md`, `Makefile`
**Read first:** Makefile — check (first `@echo "==> gofmt"` line is where the refusal goes), test, help (`grep -E '^## '`); docs/manual/building.md — §Testing closing paragraph ("CI runs `make test` under `APOGEE_TEST_SHARDS=2`…memory cost."), targets table row `make check` (names "race tests" — stays truthful);
scripts/test-shards.sh — Usage header (`APOGEE_TEST_SHARDS=n` line, where items 1–2 add the two knobs); .github/workflows/ci.yml — `run: make test` step (untouched, `APOGEE_TEST_SHARDS=2` only)

**Tests:** prose, plus the `check` refusal.

```
grep -c 'APOGEE_TEST_SLOW=1' docs/manual/building.md | grep -vx 0
grep -c 'APOGEE_TEST_RACE=0' docs/manual/building.md | grep -vx 0
grep -F 'Found 39 - Supported 48' docs/manual/building.md
sed -n '/^## test:/,/^test:$/p' Makefile | grep -F 'APOGEE_TEST_SLOW'
sed -n '/^## test:/,/^test:$/p' Makefile | grep -F 'APOGEE_TEST_RACE'
git diff --quiet HEAD -- .github/workflows/ci.yml
make -n test
APOGEE_TEST_RACE=0 make check 2>&1 | grep -F 'make check: APOGEE_TEST_RACE=0 is set'   # exits 0 when the refusal line is printed
! APOGEE_TEST_RACE=0 make check >/dev/null 2>&1                                       # the refusal exits non-zero
APOGEE_TEST_RACE=0 make -n test                                                       # exits 0 — `test` alone is not refused
```

**Acceptance:** every command above exits 0.

**Commit:** `docs(building): the shard script's slow-box and unraced knobs, and the 39-bit-VA kernel that needs them`
