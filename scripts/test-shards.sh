#!/usr/bin/env bash
# test-shards.sh — run the full race-enabled test suite as several concurrent `go test`
# processes, so `make check` is bounded by the slowest SHARD rather than by the slowest PACKAGE.
#
# Why sharding rather than `t.Parallel()`. Nearly all of the suite's wall time is in two
# packages, and in both of them it is in tests that CANNOT be parallel tests:
#
#   * every e2e launch helper goes through `assertNoAmbientApogeeConfig`
#     (cmd/apogee/e2e_support_test.go), which calls `t.Setenv` — and the testing package
#     panics if a test calls both `t.Setenv` and `t.Parallel`;
#   * `tuitest.CheckLeaks` (internal/tuitest/leak.go) diffs the PROCESS-WIDE goroutine dump,
#     so under parallel tests one test's cleanup would read another test's live goroutines as
#     leaks.
#
# Both constraints are per-process, so giving each shard its own `go test` process keeps them
# exactly as they are today: no test is skipped, weakened, reordered within its shard, or run
# with different flags. Measured on a 9-core box: `go test -race ./...` 212s, sharded 82s cold
# (no timing cache) and around 55s warm.
#
# Balance comes from the timings of the LAST run, cached in .test-timings (gitignored, rebuilt
# on every run). Without it the packer falls back to equal costs, which still shards correctly —
# just less evenly, since the expensive tests cluster by name prefix. A stale or missing cache
# is therefore a SPEED regression and never a correctness one, which is the failure mode this
# whole file is allowed to have.
#
# Usage: scripts/test-shards.sh [extra go test flags ...]
#   APOGEE_TEST_SHARDS=n   override the per-heavy-package shard count (default: sized off nproc)

set -uo pipefail

cd "$(dirname "$0")/.."

# The packages worth splitting, with the share of the shard budget each gets. Everything else
# runs in one `go test` invocation: 46 packages that finish in about ten seconds between them,
# where a second process would buy nothing and cost a compile. The list is a PERFORMANCE knob —
# if a third package grows heavy and is not named here the suite still runs, just slower — so it
# is deliberately explicit rather than inferred from a threshold nothing measures.
HEAVY_PKGS=(./cmd/apogee ./internal/tui)
HEAVY_WEIGHT=(4 3) # cmd/apogee is ~1.7x internal/tui by total test time

TIMINGS=.test-timings
GOFLAGS_TEST=(-race -count=1)

# The shard budget. One process per core minus one leaves the box a core for the `go build`
# work every shard triggers; the floor of 2 keeps the split meaningful on a small machine, and
# the ceiling of 7 is where the race detector's memory cost starts to dominate on the hardware
# this is tuned for. APOGEE_TEST_SHARDS overrides the per-package count outright.
ncpu=$( (command -v nproc >/dev/null 2>&1 && nproc) || sysctl -n hw.ncpu 2>/dev/null || echo 4)
budget=$((ncpu - 1))
[ "$budget" -ge 2 ] || budget=2
[ "$budget" -le 7 ] || budget=7

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

		# `=== PAUSE` is pure scheduling noise. `=== RUN` / `=== CONT` name the test whose output
		# follows — the interleaving a parallel run produces means the LAST such marker wins.
		/^=== PAUSE[ \t]/ { next }
		/^=== (RUN|CONT)[ \t]/ { cur = $3; if (root(cur) in failed) print; next }

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
	n=${APOGEE_TEST_SHARDS:-$(( (budget * HEAVY_WEIGHT[idx] + 6) / 7 ))}
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

	# Cost each test from the cache, defaulting unknown ones to a tenth of a second: high enough
	# that a shard of brand-new tests is not assumed free, low enough not to outweigh a measured
	# heavyweight. Then greedy bin-pack longest-first, which is what turns a 79s worst shard into
	# an evenly loaded one.
	awk -F'\t' -v pkg="$pkgpath" '
		FILENAME == ARGV[1] { if ($1 == pkg) cost[$2] = $3; next }
		{ printf "%.3f\t%s\n", ($0 in cost ? cost[$0] : 0.1), $0 }
	' "$TIMINGS" "$list" 2>/dev/null >"$work/costed.$idx" ||
		awk '{ printf "0.100\t%s\n", $0 }' "$list" >"$work/costed.$idx"
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
mapfile -t rest < <(
	go list ./... | grep -vxF -f <(printf '%s\n' "${HEAVY_PKGS[@]}" | sed 's|^\./|github.com/airiclenz/apogee/|')
)

# ---------------------------------------------------------------------------------------------
# Run every shard at once.
# ---------------------------------------------------------------------------------------------
echo "==> go test -race: ${#shard_run[@]} shards + ${#rest[@]} other packages"

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

launch "$work/rest.log" "the remaining ${#rest[@]} packages" -- "$@" "${rest[@]}"

for s in "${!shard_run[@]}"; do
	launch "$work/shard.$s.log" "${shard_pkg[$s]} shard $s" -- "$@" -run "${shard_run[$s]}" "${shard_pkg[$s]}"
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
