#!/usr/bin/env bash
# test-shards.sh — run the full race-enabled test suite as a queue of short-lived test processes
# spread over a fixed number of lanes, so `make check` is bounded by the busiest LANE rather than
# by the slowest PACKAGE, and no single process lives long enough to grow without bound.
#
# Why split the suite at all, when the packages are `t.Parallel()` inside. Nearly all of the
# suite's wall time is in two packages, and both are parallel inside. cmd/apogee's e2e tests are
# parallel tests by default — `tuitest.CheckLeaks` (internal/tuitest/leak.go) attributes
# goroutines to the test that started them, and the launch helpers neither `t.Setenv` nor swap a
# package-level seam. internal/tui's driver tests are parallel too (each builds its own Model and
# stub upstream). In both, only a test that reaches `t.Setenv` or a seam itself — directly or
# through a helper — stays serial, and a guard in each package (seams_guard_test.go) fails a
# parallel test that swaps one. What `t.Parallel()` cannot do is balance ACROSS packages, bound
# the load the processes put on the box together, or bound how big one process gets — that is
# this script's job.
#
# The shape. Each heavy package's test binary is built ONCE (`go test -c`), its roster is read
# from the binary (`-test.list`), and its tests are packed into CHUNKS of about CHUNK_SECS of
# measured test time each. Every other package is packed whole into BATCHES by the same measure.
# Chunks and batches form one queue, longest first, and LANES worker loops drain it: each takes
# the next item, runs it as a fresh process, and takes another when it exits. A chunk is the
# prebuilt binary exec'd in its package directory — exactly what `go test` itself runs — with
# `-test.run '^(A|B|…)$'`; a batch is `go test -p 1` over its packages.
#
# Why fresh processes. A test process's memory is not the sum of what each test needs but
# grows with how many tests it holds: testing parks every parallel test at its t.Parallel call
# until the serial ones are done, each parked goroutine keeps the stack its test function's
# frame needs (internal/tui's frames run to hundreds of KB, since a Model is ~120 KB by value),
# and the race detector shadows all of it again. One race-enabled internal/tui process grew to
# ~4 GB and was OOM-killed on an 8 GB box; one cmd/apogee process grew from 1.5 to 3 GB over a
# long serial run. A chunk of a few hundred tests holds a fraction of that, and gives it all back
# when it exits.
#
# Every test runs exactly as `go test` would run it: none is skipped, weakened, or run with
# different flags — with THREE flags this script sets itself: `-parallel` (see "the parallel
# bound" below), which changes how many of a process's parallel tests run at once and nothing
# about any test; `-timeout` (see "the timeout" below), which moves the binary's hang alarm; and
# `-run`, which only picks the chunk's tests. Order within a package can differ, which a test
# may not depend on anyway. One thing `go test` does that a prebuilt binary does not: run its
# vet subset first. golangci-lint's govet covers that in `make check` and in CI.
#
# Balance comes from the timings of the LAST run, cached in .test-timings (gitignored, rebuilt
# on every run). Without it the packer reads the committed scripts/test-timings.seed — the same
# format, refreshed by `make test-timings-seed` — so a fresh checkout (CI's runner every time)
# packs by measured costs rather than the flat fallback. A stale or missing cache is therefore a
# SPEED regression and never a correctness one, which is the failure mode this whole file is
# allowed to have.
#
# Usage: scripts/test-shards.sh [extra go test flags ...]
#   APOGEE_TEST_LANES=n    how many test processes run at once (default: sized off nproc; 3 in slow mode)
#   APOGEE_TEST_SHARDS=n   the older knob, read as APOGEE_TEST_LANES = n per heavy package + 1
#                          (the process count the per-package shards it used to set came to)
#   APOGEE_TEST_RACE=0     drop -race (announced on stderr and in the ==> line); any other value keeps it
#   APOGEE_TEST_SLOW=1     slow box: 3 lanes (an explicit lane count still wins), every process -parallel 1
#
# The extra flags reach every process. A batch's `go test` takes them as given. A chunk's binary
# takes the test flags among them in their binary spelling (`-timeout=1h` becomes
# `-test.timeout=1h`), and every other flag (`-tags`, `-gcflags`, …) goes to the `go test -c`
# that builds it.

set -uo pipefail

cd "$(dirname "$0")/.."

# The packages worth splitting into chunks. Everything else runs whole, packed into batches: 46
# packages whose tests are short, where a process per chunk would buy nothing. The list is a
# PERFORMANCE knob — if a third package grows heavy and is not named here the suite still runs,
# just less evenly — so it is deliberately explicit rather than inferred from a threshold.
HEAVY_PKGS=(./cmd/apogee ./internal/tui)

# The chunk size, in the timings' own seconds (the box that measured them). About two minutes:
# long enough that a process's startup — and cmd/apogee's TestMain, which builds the binary its
# PTY tests drive — is a small share of it, short enough that the queue's tail is short and no
# process lives long. CHUNK_MAX_TESTS bounds memory where time does not: internal/tui runs
# hundreds of fast tests a minute, and every one of them is parked in the process at once.
CHUNK_SECS=120
CHUNK_MAX_TESTS=400
# The least a test is assumed to cost, so the many tests the timings record at 0.00 s still
# spread across chunks instead of all landing in whichever is lightest; and what a batch is
# charged per package on top of its tests, for building and starting its test binary.
MIN_TEST_COST=0.05
PKG_OVERHEAD=3

TIMINGS=.test-timings
TIMINGS_SEED=scripts/test-timings.seed
# APOGEE_TEST_RACE=0 drops -race for a kernel that cannot run the race detector — the 39-bit-VA
# arm64 rpi kernels, where a race binary aborts at startup with `FATAL: Found 39 - Supported 48`
# (the roster listing below included). A developer convenience, never a gate: `make check` and
# CI stay raced, and the run says so on stderr and in its `==>` line.
#
# The timeout. `go test`'s default of 10m per test binary assumes the binary fans its tests out
# GOMAXPROCS-wide; a process here runs them `-parallel 1` on a loaded box by design (see "the
# parallel bound" below), and its chunk was sized by timings from whatever box measured them —
# CI packs by a seed a slower box may have written. 30m covers that with room to spare and
# still ends a real hang with a goroutine dump long before CI's job limit would kill it without
# one. A caller's own `-timeout` wins: the extra flags follow these on every line, and the last
# value of a flag is the one used.
TEST_TIMEOUT=30m
if [ "${APOGEE_TEST_RACE:-}" = 0 ]; then
	RACE_FLAG=()
else
	RACE_FLAG=(-race)
fi

# Which timings the packer reads: the last run's cache when there is one, else the committed
# seed. Only the READ side falls back — the harvest at the end always rewrites .test-timings and
# never the seed, so a stale seed cannot creep in through an ordinary run.
if [ -f "$TIMINGS" ]; then
	TIMINGS_SOURCE=$TIMINGS
elif [ -f "$TIMINGS_SEED" ]; then
	TIMINGS_SOURCE=$TIMINGS_SEED
else
	TIMINGS_SOURCE=/dev/null
fi
echo "test-shards: timings: $([ "$TIMINGS_SOURCE" = /dev/null ] && echo 'none (flat costs)' || echo "$TIMINGS_SOURCE")" >&2
if [ "${APOGEE_TEST_RACE:-}" = 0 ]; then
	echo "test-shards: race detector OFF (APOGEE_TEST_RACE=0) — this run does not stand in for make check" >&2
fi

# ---------------------------------------------------------------------------------------------
# The extra flags, split for the two kinds of process. A test binary spells its flags
# `-test.name`; `go test` spells them `-name` and hands every other flag to the build.
# ---------------------------------------------------------------------------------------------
TEST_BOOL_FLAGS=" benchmem failfast fullpath short v "
TEST_VALUE_FLAGS=" bench benchtime blockprofile blockprofilerate count coverprofile cpu cpuprofile fuzz fuzzcachedir fuzzminimizetime fuzztime list memprofile memprofilerate mutexprofile mutexprofilefraction outputdir parallel run shuffle skip timeout trace "
in_words() { case "$2" in *" $1 "*) return 0 ;; esac; return 1; } # word list

bin_args=()
build_args=()
set_args=("$@")
i=0
while [ "$i" -lt "${#set_args[@]}" ]; do
	arg=${set_args[$i]}
	name=${arg#-}
	name=${name#-}
	base=${name%%=*}
	case "$arg" in
	-*)
		if [ "${base#test.}" != "$base" ]; then
			bin_args+=("-$name")
		elif in_words "$base" "$TEST_BOOL_FLAGS"; then
			bin_args+=("-test.$name")
		elif in_words "$base" "$TEST_VALUE_FLAGS"; then
			if [ "$name" != "$base" ]; then
				bin_args+=("-test.$name")
			else
				i=$((i + 1))
				bin_args+=("-test.$base=${set_args[$i]:-}")
			fi
		else
			build_args+=("$arg")
			next=${set_args[$((i + 1))]:-}
			if [ "$name" = "$base" ] && [ -n "$next" ] && [ "${next#-}" = "$next" ]; then
				i=$((i + 1))
				build_args+=("$next")
			fi
		fi
		;;
	*) build_args+=("$arg") ;;
	esac
	i=$((i + 1))
done

# ---------------------------------------------------------------------------------------------
# Lanes and the parallel bound.
#
# The process budget. One process per core minus one leaves the box a core for the builds and
# the processes' own startup; the floor of 2 keeps the split meaningful on a small machine, and
# the ceiling of 7 is where the race detector's memory cost starts to dominate on the hardware
# this is tuned for. By default every lane is one slot of that budget.
#
# The parallel bound. `go test` runs a package's `t.Parallel` tests `-parallel` at a time, and
# the flag defaults to GOMAXPROCS — the whole box — because it assumes it is the only process
# on it. Here it is not: the budget is already spent on lanes, and a process that also fanned
# its tests out GOMAXPROCS-wide would put lanes × GOMAXPROCS driven e2e tests on the box at once,
# which is what turned the old sharded suite red after the cmd/apogee sweep — the kit's waits
# timing out, leak checks finding goroutines still unwinding, PTY frames arriving late: load,
# not logic. So the budget is divided among the lanes, and each process runs that many tests at
# once — 1 whenever the lanes already fill the budget, more only when an explicit lane count
# leaves slots over. A batch of light packages takes a floor of 2: their tests mostly sleep
# rather than compute, and one at a time cost the old plan its critical path for nothing.
#
# The slow-box plan. APOGEE_TEST_SLOW=1 is an explicit knob rather than a bound sized off nproc
# because core count cannot tell the boxes apart: a Raspberry Pi and CI's 4-vCPU runner both
# report 4 cores and differ 3–5x per core, and CI keeps the plan above. Slow mode is three lanes,
# every process -parallel 1: three tests on the box at once, a core left for the builds and the
# processes' startup, and three race-enabled processes' memory rather than four.
# ---------------------------------------------------------------------------------------------
ncpu=$( (command -v nproc >/dev/null 2>&1 && nproc) || sysctl -n hw.ncpu 2>/dev/null || echo 4)
budget=$((ncpu - 1))
[ "$budget" -ge 2 ] || budget=2
[ "$budget" -le 7 ] || budget=7

is_slow_box() { [ "${APOGEE_TEST_SLOW:-}" = 1 ]; }

if [ -n "${APOGEE_TEST_LANES:-}" ]; then
	lanes=$APOGEE_TEST_LANES
elif [ -n "${APOGEE_TEST_SHARDS:-}" ]; then
	lanes=$((APOGEE_TEST_SHARDS * ${#HEAVY_PKGS[@]} + 1))
	echo "test-shards: APOGEE_TEST_SHARDS=$APOGEE_TEST_SHARDS is read as APOGEE_TEST_LANES=$lanes (n per heavy package + 1)" >&2
elif is_slow_box; then
	lanes=3
else
	lanes=$budget
fi
case "$lanes" in '' | *[!0-9]*) lanes=1 ;; esac
[ "$lanes" -ge 1 ] || lanes=1

if is_slow_box; then
	parallel=1
	batch_parallel=1
	echo "test-shards: slow box (APOGEE_TEST_SLOW=1): $lanes lanes, every process -parallel 1" >&2
else
	parallel=$((budget / lanes))
	[ "$parallel" -ge 1 ] || parallel=1
	batch_parallel=$parallel
	[ "$batch_parallel" -ge 2 ] || batch_parallel=2
fi

# Not `tmp.*`: a work dir under the system temp dir's default mktemp name is what a blanket
# clean-up of stray temp dirs sweeps away mid-run.
work="$(mktemp -d "${TMPDIR:-/tmp}/apogee-test-shards.XXXXXX")"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/q" "$work/claim"

# ---------------------------------------------------------------------------------------------
# render_failures reads a `-v` log and writes the parts of it a reader needs: the full output of
# every test that FAILED, plus every line that belongs to no test at all (build errors, the
# trailing FAIL line, a panic's stack). A passing test's transcript is dropped — `-v` is on only
# because it is the one format carrying a per-test duration, and printing hundreds of passing
# transcripts would bury the failure this function exists to surface.
#
# Attribution has to follow the testing package's own interleaving: a parallel test emits
# `=== RUN name`, yields with `=== PAUSE name`, and resumes under `=== CONT name`, so the test a
# line belongs to is whichever name the most recent RUN or CONT marker named — not whichever RUN
# came first. Two passes, because a test's output precedes the `--- FAIL:` line that condemns it.
#
# From a `panic:` or `fatal error:` onwards everything is printed unconditionally: the process is
# dying, the remaining lines are the stack, and no attribution they carry can be trusted.
# ---------------------------------------------------------------------------------------------
render_failures() {
	awk '
		# The top-level test a name belongs to: the testing package reports a subtest as
		# `Parent/sub`, and a subtest failure always condemns its parent, so the parent is the
		# unit of attribution.
		function root(n) { return (index(n, "/") > 0) ? substr(n, 1, index(n, "/") - 1) : n }

		FNR == 1 { pass++ }
		pass == 1 { if ($0 ~ /^--- FAIL: /) failed[$3] = 1; next }

		/^panic:|^fatal error:/ { dying = 1 }
		dying { print; next }

		# Package-level lines belong to no test and end whatever group was in progress. Only the
		# failing ones are worth printing: this is a failure report, and "ok" for the packages
		# that passed is what the summary at the end of the run already says.
		/^(PASS|FAIL)$/ || /^(ok|FAIL|\?)[ \t]/ {
			cur = ""
			if ($0 ~ /^FAIL/) print
			next
		}

		# `=== PAUSE` is pure scheduling noise. `=== RUN` / `=== CONT` / `=== NAME` name the test
		# whose output follows — the interleaving a parallel run produces means the LAST such
		# marker wins. `=== NAME` is the one printed when the output of a test resumes after
		# lines from another test cut in, which is how the failure message of a parallel test
		# and its `--- FAIL` line usually arrive: without the rule the message is charged to
		# whichever test the previous marker named and the report shows a bare `--- FAIL` line.
		/^=== PAUSE[ \t]/ { next }
		/^=== (RUN|CONT|NAME)[ \t]/ { cur = $3; if (root(cur) in failed) print; next }

		# A result line, at any indent. The testing package buffers a tree and prints the parent
		# verdict FIRST, then its children indented beneath it, so this must set the group
		# rather than clear it — the lines that follow are the children and their logs.
		/^[ \t]*--- (PASS|FAIL|SKIP): / { cur = $3; if (root(cur) in failed) print; next }

		{ if (cur == "" || root(cur) in failed) print }
	' "$1" "$1"
}

# running_tests names the tests a log shows started and never finished: what a process was
# running when something outside it — the OOM killer, a signal — ended it without a verdict.
# A test parked at t.Parallel (`=== PAUSE`, no `=== CONT` after it) was waiting, not running.
running_tests() {
	awk '
		/^=== (RUN|CONT)[ \t]/ { state[$3] = "running"; next }
		/^=== PAUSE[ \t]/ { state[$3] = "parked"; next }
		/^[ \t]*--- (PASS|FAIL|SKIP): / { state[$3] = "done"; next }
		END { for (n in state) if (state[n] == "running") print n }
	' "$1" | sort | paste -sd ' ' -
}

# harvest_timings writes `package<TAB>test<TAB>seconds` for every top-level test the log
# resolved. Sub-tests are skipped (their result lines are indented): the packer schedules whole
# top-level tests, because that is the grain `-run` selects at.
#
# With a package argument the log is one chunk's (a test binary's own output, which names no
# package), and every result in it is that package's. Without one it is a batch's `go test`
# log, which can hold several packages, each a block ended by its own `ok`/`FAIL` line naming
# the package — so the results are held until that line arrives.
harvest_timings() { # log [import path]
	awk -v pkg="${2:-}" '
		function seconds(line) { return match(line, /\(([0-9.]+)s\)$/) ? substr(line, RSTART + 1, RLENGTH - 3) + 0 : -1 }
		function flush(p,    n) {
			for (n in own) printf "%s\t%s\t%.2f\n", p, n, own[n]
			split("", own)
		}
		/^--- (PASS|FAIL|SKIP): / { if ((s = seconds($0)) >= 0) own[$3] = s; next }
		/^(ok|FAIL)[ \t]/ && pkg == "" { flush($2); next }
		END { if (pkg != "") flush(pkg) }
	' "$1"
}

# ---------------------------------------------------------------------------------------------
# Build the heavy packages' test binaries, both at once. The other packages' `go test` runs
# start from the build cache these fill.
# ---------------------------------------------------------------------------------------------
build_pids=()
for idx in "${!HEAVY_PKGS[@]}"; do
	go test "${RACE_FLAG[@]+"${RACE_FLAG[@]}"}" -c -o "$work/bin/$idx.test" \
		${build_args[@]+"${build_args[@]}"} "${HEAVY_PKGS[$idx]}" >"$work/build.$idx.log" 2>&1 &
	build_pids+=($!)
done
build_failed=0
for idx in "${!HEAVY_PKGS[@]}"; do
	if ! wait "${build_pids[$idx]}"; then
		echo "test-shards: could not build the tests in ${HEAVY_PKGS[$idx]}:" >&2
		cat "$work/build.$idx.log" >&2
		build_failed=1
	fi
done
[ "$build_failed" -eq 0 ] || exit 1

# ---------------------------------------------------------------------------------------------
# Plan. A queue item is a handful of files under $work/q/<n>: kind (chunk|batch), label, cost,
# and either the binary, package dir and -run pattern of a chunk or the package list of a batch.
#
# The heavy rosters come from the binaries' own `-test.list` — a name grepped out of the sources
# would miss whatever a generated or cross-file declaration adds, and a plan that silently omits
# a test is the one bug this script must not have. Benchmarks are excluded because `go test`
# does not run them; Fuzz targets are included because their seed corpus does run as an ordinary
# test under `-run`.
# ---------------------------------------------------------------------------------------------
nitems=0
add_item() { # kind label cost -> sets $item to the new item's directory
	item="$work/q/$nitems"
	mkdir -p "$item"
	echo "$1" >"$item/kind"
	echo "$2" >"$item/label"
	echo "$3" >"$item/cost"
	nitems=$((nitems + 1))
}

# pack reads `cost<TAB>name` lines and writes `bin<TAB>count<TAB>cost<TAB>name|name|…` for k
# bins: longest first, each into the lightest bin that still has room for it under maxn names.
pack() { # k maxn < costed
	sort -t$'\t' -k1,1rn | awk -F'\t' -v k="$1" -v maxn="$2" '
		{
			pick = -1
			for (b = 0; b < k; b++) if (count[b] < maxn && (pick < 0 || load[b] < load[pick])) pick = b
			load[pick] += $1
			names[pick] = (names[pick] == "" ? $2 : names[pick] "|" $2)
			count[pick]++
		}
		END { for (b = 0; b < k; b++) if (count[b] > 0) printf "%d\t%d\t%.1f\t%s\n", b, count[b], load[b], names[b] }
	'
}

# bins_for is how many bins a costed list needs: enough that none is over CHUNK_SECS of cost
# and none over maxn names.
bins_for() { # maxn < costed
	awk -F'\t' -v secs="$CHUNK_SECS" -v maxn="$1" '
		{ total += $1; n++ }
		END {
			k = int((total + secs - 1) / secs); if (k < 1) k = 1
			kn = int((n + maxn - 1) / maxn); if (kn > k) k = kn
			print k
		}
	'
}

plan_failed=0
for idx in "${!HEAVY_PKGS[@]}"; do
	pkg=${HEAVY_PKGS[$idx]}
	# The cache is keyed by the package's IMPORT path, because that is what `go test` prints on
	# its own ok/FAIL lines; HEAVY_PKGS spells the same packages as relative patterns.
	pkgpath=$(go list "$pkg")
	bin="$work/bin/$idx.test"

	list="$work/list.$idx"
	if ! (cd "$pkg" && exec "$bin" -test.list '.*') >"$list.raw" 2>"$list.err"; then
		echo "test-shards: could not list the tests in $pkg:" >&2
		cat "$list.err" >&2
		exit 1
	fi
	grep -E '^(Test|Example|Fuzz)' "$list.raw" | sort >"$list"
	total=$(wc -l <"$list" | tr -d ' ')
	if [ "$total" -eq 0 ]; then
		echo "test-shards: $pkg lists no tests — refusing to run a package that reports none" >&2
		exit 1
	fi

	# Cost each test from the timings source, defaulting an unknown one to a tenth of a second:
	# high enough that a chunk of brand-new tests is not assumed free, low enough not to outweigh
	# a measured heavyweight. A `#` line in the source matches no package, which is what lets the
	# seed carry a header.
	awk -F'\t' -v pkg="$pkgpath" -v floor="$MIN_TEST_COST" '
		FILENAME == ARGV[1] { if ($1 == pkg) cost[$2] = $3; next }
		{ c = ($0 in cost) ? cost[$0] : 0.1; if (c < floor) c = floor; printf "%.3f\t%s\n", c, $0 }
	' "$TIMINGS_SOURCE" "$list" >"$work/costed.$idx"

	k=$(bins_for "$CHUNK_MAX_TESTS" <"$work/costed.$idx")
	pack "$k" "$CHUNK_MAX_TESTS" <"$work/costed.$idx" >"$work/bins.$idx"

	# The guard the whole plan rests on: every listed test lands in exactly one chunk.
	packed=$(awk -F'\t' '{ s += $2 } END { print s + 0 }' "$work/bins.$idx")
	if [ "$packed" -ne "$total" ]; then
		echo "test-shards: the plan for $pkg covers $packed of $total tests — refusing to run a partial suite" >&2
		plan_failed=1
	fi

	nchunks=$(wc -l <"$work/bins.$idx" | tr -d ' ')
	while IFS=$'\t' read -r b count cost names; do
		add_item chunk "${pkg#./} chunk $((b + 1))/$nchunks ($count tests)" "$cost"
		echo "$bin" >"$item/bin"
		echo "$pkg" >"$item/dir"
		echo "$pkgpath" >"$item/pkgpath"
		echo "^($names)\$" >"$item/run"
	done <"$work/bins.$idx"
done

[ "$plan_failed" -eq 0 ] || exit 1

# Everything not split, computed from `go list` so a new package joins the run by existing, and
# packed whole into batches by the summed cost of its tests plus PKG_OVERHEAD.
go list ./... | grep -vxF -f <(printf '%s\n' "${HEAVY_PKGS[@]}" | sed 's|^\./|github.com/airiclenz/apogee/|') >"$work/rest"
nrest=$(wc -l <"$work/rest" | tr -d ' ')
if [ "$nrest" -gt 0 ]; then
	awk -F'\t' -v over="$PKG_OVERHEAD" '
		FILENAME == ARGV[1] { if ($1 !~ /^#/) cost[$1] += $3; next }
		{ printf "%.3f\t%s\n", cost[$0] + over, $0 }
	' "$TIMINGS_SOURCE" "$work/rest" >"$work/costed.rest"
	k=$(bins_for "$nrest" <"$work/costed.rest")
	pack "$k" "$nrest" <"$work/costed.rest" >"$work/bins.rest"
	nbatches=$(wc -l <"$work/bins.rest" | tr -d ' ')
	while IFS=$'\t' read -r b count cost names; do
		add_item batch "batch $((b + 1))/$nbatches ($count packages)" "$cost"
		echo "$names" | tr '|' '\n' >"$item/pkgs"
	done <"$work/bins.rest"
fi

# The queue order: longest first, so the short items fill the lanes' tails.
for ((n = 0; n < nitems; n++)); do
	printf '%s\t%s\n' "$(cat "$work/q/$n/cost")" "$n"
done | sort -t$'\t' -k1,1rn | cut -f2 >"$work/order"

# ---------------------------------------------------------------------------------------------
# Run. Each lane walks the queue in order and claims an item by creating its claim directory —
# `mkdir` is atomic, so exactly one lane gets each item — then runs it and moves on.
# ---------------------------------------------------------------------------------------------
if [ "${APOGEE_TEST_RACE:-}" = 0 ]; then
	echo "==> go test (unraced): $nitems processes over $lanes lanes"
else
	echo "==> go test -race: $nitems processes over $lanes lanes"
fi
echo "    heavy-package chunks of ~${CHUNK_SECS}s and at most $CHUNK_MAX_TESTS tests, the other $nrest packages in batches; each runs $parallel test(s) at a time, a batch $batch_parallel"

run_item() { # n [extra go test flags ...]
	local dir="$work/q/$1" log="$work/q/$1/log" t0 rc
	shift
	t0=$(date +%s)
	if [ "$(cat "$dir/kind")" = chunk ]; then
		# The outer redirect only silences bash's own "Killed" notice for a process that died of a
		# signal; the report below says so, with the tests it was running.
		{ (cd "$(cat "$dir/dir")" && exec "$(cat "$dir/bin")" -test.paniconexit0 -test.v=true \
			-test.count=1 -test.timeout="$TEST_TIMEOUT" -test.parallel="$parallel" \
			-test.run="$(cat "$dir/run")" ${bin_args[@]+"${bin_args[@]}"}) >"$log" 2>&1; } 2>/dev/null
		rc=$?
	else
		# shellcheck disable=SC2046 # one package per line, import paths carry no spaces
		go test "${RACE_FLAG[@]+"${RACE_FLAG[@]}"}" -count=1 -timeout "$TEST_TIMEOUT" -v -p 1 \
			-parallel "$batch_parallel" "$@" $(cat "$dir/pkgs") >"$log" 2>&1
		rc=$?
	fi
	echo $(($(date +%s) - t0)) >"$dir/wall"
	echo "$rc" >"$dir/rc"
}

run_lane() {
	local n
	while read -r n; do
		mkdir "$work/claim/$n" 2>/dev/null || continue
		run_item "$n" "$@"
	done <"$work/order"
}

t_start=$(date +%s)
lane_pids=()
for ((l = 0; l < lanes; l++)); do
	run_lane "$@" &
	lane_pids+=($!)
done
for pid in "${lane_pids[@]}"; do
	wait "$pid"
done
t_wall=$(($(date +%s) - t_start))

# Rebuild the timing cache from this run before anything can exit on a failure: a failing run
# still measured every test that finished, and throwing that away would leave the next run
# unbalanced for no reason. A partial cache is fine — unknown tests just take the default.
: >"$TIMINGS.new"
for ((n = 0; n < nitems; n++)); do
	dir="$work/q/$n"
	[ -f "$dir/log" ] || continue
	if [ "$(cat "$dir/kind")" = chunk ]; then
		harvest_timings "$dir/log" "$(cat "$dir/pkgpath")" >>"$TIMINGS.new"
	else
		harvest_timings "$dir/log" >>"$TIMINGS.new"
	fi
done
if [ -s "$TIMINGS.new" ]; then mv "$TIMINGS.new" "$TIMINGS"; else rm -f "$TIMINGS.new"; fi

# Report every item that did not pass. A process that died of a signal printed no verdict of its
# own, so the report names the signal and the tests it was running — the OOM killer's SIGKILL is
# otherwise a silent FAIL.
rc=0
for ((n = 0; n < nitems; n++)); do
	dir="$work/q/$n"
	item_rc=$(cat "$dir/rc" 2>/dev/null || echo missing)
	if [ "$item_rc" = 0 ] && ! grep -qE '^(FAIL|---[ \t]*FAIL|panic:)' "$dir/log"; then
		continue
	fi
	rc=1
	echo ""
	echo "--- $(cat "$dir/label") ---"
	if [ "$item_rc" = missing ]; then
		echo "test-shards: this item never ran to completion — no exit status was recorded"
	elif [ "$item_rc" -gt 128 ]; then
		sig=$((item_rc - 128))
		echo "test-shards: killed by SIG$(kill -l "$sig" 2>/dev/null || echo "$sig") after $(cat "$dir/wall")s, with no verdict of its own$([ "$sig" = 9 ] && echo ' (SIGKILL: on Linux usually the kernel OOM killer — see dmesg)')"
		echo "test-shards: running when it died: $(running_tests "$dir/log")"
	elif grep -q 'signal: killed' "$dir/log"; then
		echo "test-shards: a test binary in this batch was killed (SIGKILL: on Linux usually the kernel OOM killer — see dmesg)"
	fi
	render_failures "$dir/log"
	if [ "$item_rc" != 0 ] && ! grep -qE '^(FAIL|---[ \t]*FAIL|panic:)' "$dir/log"; then
		echo "test-shards: exit status $item_rc; the log's last lines:"
		tail -n 20 "$dir/log"
	fi
done

# What a tuning pass reads: per heavy package and for the batches, how many processes and the
# slowest of them, and the run's wall time against the lanes' — a lane well over the others is
# the critical path, and CHUNK_SECS or the lane count is what moves it.
if [ "$rc" -eq 0 ]; then
	for ((n = 0; n < nitems; n++)); do
		dir="$work/q/$n"
		label=$(cat "$dir/label")
		group=${label%% chunk *}
		[ "$(cat "$dir/kind")" = batch ] && group="other packages"
		printf '%s\t%s\n' "$group" "$(cat "$dir/wall")"
	done | awk -F'\t' '
		{ n[$1]++; s[$1] += $2; if ($2 > m[$1]) m[$1] = $2 }
		END { for (g in n) printf "    %-20s %2d processes, %5ds in all, slowest %4ds\n", g, n[g], s[g], m[g] }
	' | sort
	echo "    wall ${t_wall}s over $lanes lanes"
	echo "    all packages passed"
fi
exit "$rc"
