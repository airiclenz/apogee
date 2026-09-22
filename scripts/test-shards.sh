#!/usr/bin/env bash
# test-shards.sh — run the full race-enabled test suite as several concurrent `go test`
# processes, so `make check` is bounded by the slowest SHARD rather than by the slowest PACKAGE.
#
# Why sharding as well as `t.Parallel()`. Nearly all of the suite's wall time is in two
# packages, and both are parallel inside. cmd/apogee's e2e tests are parallel tests by default —
# `tuitest.CheckLeaks` (internal/tuitest/leak.go) attributes goroutines to the test that started
# them, and the launch helpers neither `t.Setenv` nor swap a package-level seam. internal/tui's
# driver tests are parallel too (each builds its own Model and stub upstream). In both, only a
# test that reaches `t.Setenv` or a seam itself — directly or through a helper — stays serial,
# and a guard in each package (seams_guard_test.go) fails a parallel test that swaps one. What
# `t.Parallel()` cannot do is balance ACROSS packages: the race-enabled processes each heavy
# package gets, against the one process the rest share, and the `-parallel` bound each is
# handed so the shards spend the box rather than multiply the load — that is sharding's job.
#
# A shard is a `go test` process of its own, so every test runs exactly as it does today: none
# is skipped, weakened, reordered within its shard, or run with different flags — with ONE
# flag this script sets itself, `-parallel` (see "the parallel bound" below), which changes how
# many of a process's parallel tests run at once and nothing about any test. Measured on a
# 9-core box before the cmd/apogee sweep: `go test -race ./...` 212s, sharded 82s cold (no
# timing cache) and around 55s warm.
#
# Balance comes from the timings of the LAST run, cached in .test-timings (gitignored, rebuilt
# on every run). Without it the packer reads the committed scripts/test-timings.seed — the same
# format, refreshed by `make test-timings-seed` — so a fresh checkout (CI's runner every time)
# packs by measured costs rather than the equal-cost fallback, which still shards correctly —
# just less evenly, since the expensive tests cluster by name prefix. A stale or missing cache
# is therefore a SPEED regression and never a correctness one, which is the failure mode this
# whole file is allowed to have.
#
# Usage: scripts/test-shards.sh [extra go test flags ...]
#   APOGEE_TEST_SHARDS=n   override the per-heavy-package shard count (default: sized off nproc)
#   APOGEE_TEST_RACE=0     drop -race (announced on stderr and in the ==> line); any other value keeps it
#   APOGEE_TEST_SLOW=1     slow box: 1 shard per heavy package (APOGEE_TEST_SHARDS still wins), every process -parallel 1, the rest -p 2

set -uo pipefail

cd "$(dirname "$0")/.."

# The packages worth splitting, with the share of the shard budget each gets. Everything else
# runs in one `go test` invocation: 46 packages that finish in about ten seconds between them,
# where a second process would buy nothing and cost a compile. The list is a PERFORMANCE knob —
# if a third package grows heavy and is not named here the suite still runs, just slower — so it
# is deliberately explicit rather than inferred from a threshold nothing measures.
HEAVY_PKGS=(./cmd/apogee ./internal/tui)
# Re-measured after the internal/tui `t.Parallel` sweep (2026-09-17, 9-core box, top-level
# tests' durations summed): cmd/apogee 752 tests, 261 s under `go test -race -count=1 -p 1
# -json` and 269 s in the shards' own cache; internal/tui 1880 tests, 85 s and 46 s — 3:1 to
# 5:1, where the cache had read 1.7:1 (263 s against 150 s) before the sweep. Two tui shards
# now finish (56–61 s each) under the four cmd/apogee ones (78–105 s), so the tui share drops
# from three to two: at the default budget of 7 the split below is 4 + 2 shards plus the rest,
# and one process fewer — each race-enabled tui.test holds over 1 GB — for the same critical
# path. Moving that share to cmd/apogee instead (5 + 2) was tried and rejected: a fifth driven
# e2e test running at once is exactly the fan-out the `-parallel` bound below exists to cap.
HEAVY_WEIGHT=(4 2) # cmd/apogee is 3–5x internal/tui by total test time; see above

TIMINGS=.test-timings
TIMINGS_SEED=scripts/test-timings.seed
# APOGEE_TEST_RACE=0 drops -race for a kernel that cannot run the race detector — the 39-bit-VA
# arm64 rpi kernels, where a race binary aborts at startup with `FATAL: Found 39 - Supported 48`
# (the roster `-list` calls below included). A developer convenience, never a gate: `make check`
# and CI stay raced, and the run says so on stderr and in its `==>` line.
if [ "${APOGEE_TEST_RACE:-}" = 0 ]; then
	GOFLAGS_TEST=(-count=1)
else
	GOFLAGS_TEST=(-race -count=1)
fi

# Which timings the packer reads: the last run's cache when there is one, else the committed
# seed. Only the READ side falls back — the harvest at the end always rewrites .test-timings and
# never the seed, so a stale seed cannot creep in through an ordinary run. The source is named
# on stderr because it is the one thing that distinguishes a balanced plan from the equal-cost
# fallback: the rosters themselves live in $work and are gone by the time anyone looks.
if [ -f "$TIMINGS" ]; then
	TIMINGS_SOURCE=$TIMINGS
elif [ -f "$TIMINGS_SEED" ]; then
	TIMINGS_SOURCE=$TIMINGS_SEED
else
	TIMINGS_SOURCE=""
fi
echo "test-shards: timings: ${TIMINGS_SOURCE:-none (equal costs)}" >&2
if [ "${APOGEE_TEST_RACE:-}" = 0 ]; then
	echo "test-shards: race detector OFF (APOGEE_TEST_RACE=0) — this run does not stand in for make check" >&2
fi

# The shard budget. One process per core minus one leaves the box a core for the `go build`
# work every shard triggers; the floor of 2 keeps the split meaningful on a small machine, and
# the ceiling of 7 is where the race detector's memory cost starts to dominate on the hardware
# this is tuned for. APOGEE_TEST_SHARDS overrides the per-package count outright.
ncpu=$( (command -v nproc >/dev/null 2>&1 && nproc) || sysctl -n hw.ncpu 2>/dev/null || echo 4)
budget=$((ncpu - 1))
[ "$budget" -ge 2 ] || budget=2
[ "$budget" -le 7 ] || budget=7

# The parallel bound. `go test` runs a package's `t.Parallel` tests `-parallel` at a time, and
# the flag defaults to GOMAXPROCS — the whole box — because it assumes it is the only process
# on it. Here it is not: the budget above is already spent on shards, and a shard that also
# fanned its tests out GOMAXPROCS-wide would put shards × GOMAXPROCS driven e2e tests on the
# box at once (four cmd/apogee shards × 9 = 36 on the 9-core box this is tuned for, beside the
# tui shards and the rest), which is what turned the sharded suite red after the cmd/apogee
# sweep — the kit's waits timing out, leak checks finding goroutines still unwinding, PTY frames
# arriving late: load, not logic. So the budget is divided among the processes it launches,
# and each shard runs that many tests at once — 1 whenever the plan already fills the budget
# with processes, more only when APOGEE_TEST_SHARDS leaves slots over.
# The same bound holds under CI's APOGEE_TEST_SHARDS=2 on a 4-vCPU runner. The remaining
# packages' process takes it too, with a floor of 2. `go test` already limits how many of those
# packages run at once (GOMAXPROCS), but each of them fanned its own tests out GOMAXPROCS-wide
# on top of that — on the 4-vCPU runner up to sixteen tests beside four shards, which is the
# load that stretched a 2 s CPU-bound walk in internal/doctext to 14 s and timed out the
# shards' waits. The floor is there because those packages' tests mostly sleep rather than
# compute: one at a time put the rest on the critical path (99 s against 69 s shards on the
# 9-core box), two at a time cost nothing measurable, and either is a fraction of the old
# fan-out. Its package-level concurrency (-p) is left alone: the process is dominated by
# linking fifty race binaries, and -p throttles the build too. The isolated
# `go test -race ./cmd/apogee/` keeps `go test`'s default and the whole box.
# The slow-box floor. APOGEE_TEST_SLOW=1 is an explicit knob rather than a bound sized off
# nproc because core count cannot tell the boxes apart: a Raspberry Pi 4 and CI's 4-vCPU runner
# both report 4 cores and differ 3–5x per core, and CI keeps the plan above. On the Pi the
# default plan's ≈11 concurrent driven tests turn `submit`'s echo wait and the leak
# grace red — load, not logic — where 4 at once (1 shard per heavy package, every process
# -parallel 1, the rest -p 2) is the mix measured green. The rest's floor of 2 is overridden too.
is_slow_box() { [ "${APOGEE_TEST_SLOW:-}" = 1 ]; }
parallel_bound() { # jobs -> the -parallel value for each shard
	if is_slow_box; then echo 1; return; fi
	local p=$((budget / $1))
	[ "$p" -ge 1 ] || p=1
	echo "$p"
}
rest_parallel_bound() { # jobs -> the -parallel value for the remaining packages' process
	if is_slow_box; then echo 1; return; fi
	local p
	p=$(parallel_bound "$1")
	[ "$p" -ge 2 ] || p=2
	echo "$p"
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# ---------------------------------------------------------------------------------------------
# render_failures reads a `go test -v` log and writes the parts of it a reader needs: the full
# output of every test that FAILED, plus every line that belongs to no test at all (build errors,
# the trailing FAIL line, a panic's stack). A passing test's transcript is dropped — `-v` is on
# only because it is the one format carrying a per-test duration, and printing hundreds of
# passing transcripts would bury the failure this function exists to surface.
#
# Attribution has to follow `go test`'s own interleaving: a parallel test emits `=== RUN name`,
# yields with `=== PAUSE name`, and resumes under `=== CONT name`, so the test a line belongs to
# is whichever name the most recent RUN or CONT marker named — not whichever RUN came first. Two
# passes, because a test's output precedes the `--- FAIL:` line that condemns it.
#
# From a `panic:` or `fatal error:` onwards everything is printed unconditionally: the process is
# dying, the remaining lines are the stack, and no attribution they carry can be trusted.
# ---------------------------------------------------------------------------------------------
render_failures() {
	awk '
		# The top-level test a name belongs to: `go test` reports a subtest as `Parent/sub`, and
		# a subtest failure always condemns its parent, so the parent is the unit of attribution.
		function root(n) { return (index(n, "/") > 0) ? substr(n, 1, index(n, "/") - 1) : n }

		FNR == 1 { pass++ }
		pass == 1 { if ($0 ~ /^--- FAIL: /) failed[$3] = 1; next }

		/^panic:|^fatal error:/ { dying = 1 }
		dying { print; next }

		# Package-level lines belong to no test and end whatever group was in progress. Only the
		# failing ones are worth printing: this is a failure report, and "ok" for the 47 packages
		# that passed is what the summary at the end of the run already says.
		/^(PASS|FAIL)$/ || /^(ok|FAIL|\?)[ \t]/ {
			cur = ""
			if ($0 ~ /^FAIL/) print
			next
		}

		# `=== PAUSE` is pure scheduling noise. `=== RUN` / `=== CONT` / `=== NAME` name the test
		# whose output follows — the interleaving a parallel run produces means the LAST such
		# marker wins. `=== NAME` is the one `go test` prints when the output of a test resumes
		# after lines from another test cut in, which is how the failure message of a parallel
		# test and its `--- FAIL` line usually arrive: without the rule the message is charged to
		# whichever test the previous marker named and the report shows a bare `--- FAIL` line.
		/^=== PAUSE[ \t]/ { next }
		/^=== (RUN|CONT|NAME)[ \t]/ { cur = $3; if (root(cur) in failed) print; next }

		# A result line, at any indent. `go test` buffers a tree and prints the parent verdict
		# FIRST, then its children indented beneath it, so this must set the group rather than
		# clear it — the lines that follow are the children and their logs.
		/^[ \t]*--- (PASS|FAIL|SKIP): / { cur = $3; if (root(cur) in failed) print; next }

		{ if (cur == "" || root(cur) in failed) print }
	' "$1" "$1"
}

# harvest_timings appends `package<TAB>test<TAB>seconds` for every top-level test the log
# resolved. Sub-tests are skipped (their `--- PASS:` lines are indented): the packer schedules
# whole top-level tests, because that is the grain `-run` selects at.
#
# A log can hold several packages — the non-sharded invocation runs 47 of them — and `go test`
# buffers each package's output as a block terminated by its own `ok`/`FAIL` line, so the
# timings are held until that line names the package they belong to.
harvest_timings() {
	awk '
		BEGIN { n = 0 }
		/^--- (PASS|FAIL|SKIP): / {
			if (match($0, /\(([0-9.]+)s\)$/)) {
				pending[n] = $3 "\t" substr($0, RSTART + 1, RLENGTH - 3)
				n++
			}
			next
		}
		/^(ok|FAIL|---)[ \t]/ && ($1 == "ok" || $1 == "FAIL") {
			for (i = 0; i < n; i++) printf "%s\t%s\n", $2, pending[i]
			n = 0
			next
		}
	' "$1"
}

# ---------------------------------------------------------------------------------------------
# Build the shard plan. `go test -list` is the authoritative roster — a name grepped out of the
# sources would miss whatever a generated or cross-file declaration adds, and a shard plan that
# silently omits a test is the one bug this script must not have. Benchmarks are excluded
# because `go test` does not run them; Fuzz targets are included because their seed corpus does
# run as an ordinary test under `-run`.
# ---------------------------------------------------------------------------------------------
declare -a shard_pkg shard_run
plan_failed=0

for idx in "${!HEAVY_PKGS[@]}"; do
	pkg=${HEAVY_PKGS[$idx]}
	# The explicit override first, then the slow-box floor, then the budget formula.
	if [ -n "${APOGEE_TEST_SHARDS:-}" ]; then
		n=$APOGEE_TEST_SHARDS
	elif is_slow_box; then
		n=1
	else
		n=$(( (budget * HEAVY_WEIGHT[idx] + 6) / 7 ))
	fi
	[ "$n" -ge 1 ] || n=1

	# The cache is keyed by the package's IMPORT path, because that is what `go test` prints on
	# its own ok/FAIL lines; HEAVY_PKGS spells the same packages as relative patterns.
	pkgpath=$(go list "$pkg")

	list="$work/list.$idx"
	if ! go test "${GOFLAGS_TEST[@]}" -list '.*' "$pkg" >"$list.raw" 2>"$list.err"; then
		echo "test-shards: could not list the tests in $pkg:" >&2
		cat "$list.err" >&2
		exit 1
	fi
	grep -E '^(Test|Example|Fuzz)' "$list.raw" | sort >"$list"
	total=$(wc -l <"$list" | tr -d ' ')
	if [ "$total" -eq 0 ]; then
		echo "test-shards: $pkg lists no tests — refusing to shard a package that reports none" >&2
		exit 1
	fi

	# Cost each test from the timings source (cache or seed — resolved above), defaulting unknown
	# ones to a tenth of a second: high enough that a shard of brand-new tests is not assumed
	# free, low enough not to outweigh a measured heavyweight. A `#` line in the source costs
	# nothing and matches no package, which is what lets the seed carry a header. Then greedy
	# bin-pack longest-first, which is what turns a 79s worst shard into an evenly loaded one.
	if [ -n "$TIMINGS_SOURCE" ]; then
		awk -F'\t' -v pkg="$pkgpath" '
			FILENAME == ARGV[1] { if ($1 == pkg) cost[$2] = $3; next }
			{ printf "%.3f\t%s\n", ($0 in cost ? cost[$0] : 0.1), $0 }
		' "$TIMINGS_SOURCE" "$list" 2>/dev/null >"$work/costed.$idx" ||
			awk '{ printf "0.100\t%s\n", $0 }' "$list" >"$work/costed.$idx"
	fi
	[ -s "$work/costed.$idx" ] || awk '{ printf "0.100\t%s\n", $0 }' "$list" >"$work/costed.$idx"

	sort -k1,1rn "$work/costed.$idx" | awk -F'\t' -v n="$n" '
		{
			pick = 0
			for (b = 1; b < n; b++) if (load[b] < load[pick]) pick = b
			load[pick] += $1
			bin[pick] = (bin[pick] == "" ? $2 : bin[pick] "|" $2)
			count[pick]++
		}
		END { for (b = 0; b < n; b++) printf "%s\t%d\t%s\n", b, count[b], bin[b] }
	' >"$work/bins.$idx"

	# The guard the whole plan rests on: every listed test lands in exactly one shard.
	packed=$(awk -F'\t' '{ s += $2 } END { print s + 0 }' "$work/bins.$idx")
	if [ "$packed" -ne "$total" ]; then
		echo "test-shards: shard plan for $pkg covers $packed of $total tests — refusing to run a partial suite" >&2
		plan_failed=1
	fi

	while IFS=$'\t' read -r b _count names; do
		[ -n "$names" ] || continue
		shard_pkg+=("$pkg")
		shard_run+=("^($names)\$")
	done <"$work/bins.$idx"
done

[ "$plan_failed" -eq 0 ] || exit 1

# Everything not sharded, computed from `go list` so a new package joins the run by existing.
# A read loop rather than `mapfile`: macOS ships bash 3.2, where the builtin does not exist and
# the plan would launch a rest process with no packages in it.
rest=()
while IFS= read -r p; do
	rest+=("$p")
done < <(
	go list ./... | grep -vxF -f <(printf '%s\n' "${HEAVY_PKGS[@]}" | sed 's|^\./|github.com/airiclenz/apogee/|')
)

# ---------------------------------------------------------------------------------------------
# Run every shard at once.
# ---------------------------------------------------------------------------------------------
if [ "${APOGEE_TEST_RACE:-}" = 0 ]; then
	echo "==> go test (unraced): ${#shard_run[@]} shards + ${#rest[@]} other packages"
else
	echo "==> go test -race: ${#shard_run[@]} shards + ${#rest[@]} other packages"
fi

pids=() ; labels=() ; logs=()

# Each job records its own wall time beside its log. `go test`'s own `ok <pkg> <time>` lines
# report per-PACKAGE time, which for the 47-package invocation is a sum of things that ran
# concurrently inside it — not the number that says which job is the critical path.
launch() { # log label -- go test args...
	local log=$1 label=$2 ; shift 3
	{
		local t0 t1
		t0=$(date +%s)
		go test "${GOFLAGS_TEST[@]}" -v "$@" >"$log" 2>&1
		local jrc=$?
		t1=$(date +%s)
		echo $((t1 - t0)) >"$log.wall"
		exit "$jrc"
	} &
	pids+=($!) ; labels+=("$label") ; logs+=("$log")
}

parallel=$(parallel_bound $(( ${#shard_run[@]} + 1 )))
rest_parallel=$(rest_parallel_bound $(( ${#shard_run[@]} + 1 )))
echo "    each shard runs $parallel test(s) at a time, the rest $rest_parallel (-parallel: budget $budget over $(( ${#shard_run[@]} + 1 )) processes)"

# Only the rest process takes -p: a heavy shard is a single-package process, and -p bounds
# how many PACKAGES `go test` builds and runs at once. Expanded below with the `[@]+` guard
# because bash 3.2 (macOS) treats an empty array as unset under `set -u`.
rest_package_bound=()
if is_slow_box; then
	echo "test-shards: slow box (APOGEE_TEST_SLOW=1): 1 shard per heavy package, every process -parallel 1, the rest -p 2" >&2
	rest_package_bound=(-p 2)
fi

launch "$work/rest.log" "the remaining ${#rest[@]} packages" -- "$@" ${rest_package_bound[@]+"${rest_package_bound[@]}"} -parallel "$rest_parallel" "${rest[@]}"

for s in "${!shard_run[@]}"; do
	launch "$work/shard.$s.log" "${shard_pkg[$s]} shard $s" -- "$@" -parallel "$parallel" -run "${shard_run[$s]}" "${shard_pkg[$s]}"
done

rc=0
for i in "${!pids[@]}"; do
	wait "${pids[$i]}" || rc=1
done

# Rebuild the timing cache from this run before anything can exit on a failure: a failing run
# still measured every test that finished, and throwing that away would leave the next run
# unbalanced for no reason. A partial cache is fine — unknown tests just take the default.
: >"$TIMINGS.new"
for i in "${!logs[@]}"; do
	harvest_timings "${logs[$i]}" >>"$TIMINGS.new"
done
if [ -s "$TIMINGS.new" ]; then mv "$TIMINGS.new" "$TIMINGS"; else rm -f "$TIMINGS.new"; fi

for i in "${!logs[@]}"; do
	if grep -qE '^(FAIL|---[ \t]*FAIL|panic:)' "${logs[$i]}"; then
		echo ""
		echo "--- ${labels[$i]} ---"
		render_failures "${logs[$i]}"
		rc=1
	fi
done

# The per-shard durations, which are what a tuning pass reads: a shard well above the others is
# the critical path, and either the shard counts or HEAVY_WEIGHT is what moves it.
if [ "$rc" -eq 0 ]; then
	for i in "${!logs[@]}"; do
		printf '    %-28s %ss\n' "${labels[$i]}" "$(cat "${logs[$i]}.wall" 2>/dev/null || echo '?')"
	done
	echo "    all packages passed"
fi
exit "$rc"
