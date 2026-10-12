#!/usr/bin/env bash
# ci-flakes.sh — tally the failures across this repository's failed CI runs, so "is it a flake?"
# is a lookup instead of a guess.
#
# It lists the workflow's failed runs with `gh`, fetches the log of every failed job in each, and
# reads the failing tests out of it: the `--- FAIL:` lines, the `FAIL<TAB>package` lines that
# close a package's output, a `WARNING: DATA RACE` (the tests of that package are marked
# `[race]`), a `panic: test timed out` (the tests it was running are marked `[timeout]`), and the
# `--- <package> chunk i/n ---` headers scripts/test-shards.sh prints before a failed chunk's
# output, which carries no package line of its own. A subtest that failed stands for its parents.
# A package that failed with no test to blame is `(package)`; one that did not build is
# `(build failed)`. A failed job with no test failure in its log — a lint, a vet, a pin check,
# a cross-compile — is named by its failing step, `step: <name>`.
#
# It prints two tables: one row per failed job (date, commit, job, what failed), newest run
# first; then one row per failure (test, package, platform, how many jobs it failed, on which
# dates), most frequent first. A test that fails on more than one commit, on code that did not
# touch it, is the flake.
#
# A run that failed and was then re-run to green is no longer a failed run, so its first attempt
# is not counted. GitHub keeps job logs for a limited time; an expired one is reported as
# `(log unavailable)`.
#
# Job logs are fetched once and cached under ${TMPDIR:-/tmp}/apogee-ci-flakes/ — a finished
# job's log never changes, so a re-run reads the cache instead of the API.
#
# Needs `gh`, authenticated for this repository (`gh auth status`).
#
# Usage: scripts/ci-flakes.sh [--since YYYY-MM-DD] [--workflow ci.yml] [--limit N]
#   --since     only runs created on or after this date (default: no bound)
#   --workflow  the workflow file to read (default: ci.yml)
#   --limit     at most this many failed runs, newest first (default: 100)

set -euo pipefail

cd "$(dirname "$0")/.."

DEFAULT_WORKFLOW=ci.yml
DEFAULT_RUN_LIMIT=100

usage() {
	sed -n '/^# Usage:/,/^$/s/^# \{0,1\}//p' "$0" >&2
	exit 2
}

since=""
workflow=$DEFAULT_WORKFLOW
run_limit=$DEFAULT_RUN_LIMIT
while [ $# -gt 0 ]; do
	case "$1" in
	--since)
		[ $# -ge 2 ] || usage
		since=$2
		shift 2
		;;
	--workflow)
		[ $# -ge 2 ] || usage
		workflow=$2
		shift 2
		;;
	--limit)
		[ $# -ge 2 ] || usage
		run_limit=$2
		shift 2
		;;
	-h | --help) usage ;;
	*)
		echo "ci-flakes: unknown argument: $1" >&2
		usage
		;;
	esac
done

case "$since" in
"" | [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]) ;;
*)
	echo "ci-flakes: --since wants YYYY-MM-DD, got: $since" >&2
	exit 2
	;;
esac
case "$run_limit" in
'' | *[!0-9]* | 0)
	echo "ci-flakes: --limit wants a positive number, got: $run_limit" >&2
	exit 2
	;;
esac

if ! command -v gh >/dev/null 2>&1; then
	echo "ci-flakes: needs the GitHub CLI (gh) — https://cli.github.com" >&2
	exit 1
fi

repository=$(gh repo view --json nameWithOwner --jq .nameWithOwner)
# The Go module path is the repository's GitHub path; stripping it leaves `internal/tui`, the
# same spelling test-shards.sh's chunk headers use, so a test counts once however it failed.
module_prefix="github.com/$repository/"

cache_dir="${TMPDIR:-/tmp}/apogee-ci-flakes/$(printf '%s' "$repository" | tr '/' '_')"
mkdir -p "$cache_dir"
work=$(mktemp -d "${TMPDIR:-/tmp}/ci-flakes.XXXXXX")
trap 'rm -rf "$work"' EXIT

# fetch_job_log job-id -> prints the cached path of the job's log, fetching it on a miss; fails
# when the API no longer has it (expired, or the job never started).
fetch_job_log() {
	local cached="$cache_dir/job-$1.log"
	if [ ! -s "$cached" ]; then
		# Into a scratch file first, so an interrupted fetch never leaves a truncated log behind
		# for the next run to trust.
		gh api "repos/$repository/actions/jobs/$1/logs" >"$work/fetch.log" 2>/dev/null || return 1
		mv "$work/fetch.log" "$cached"
	fi
	printf '%s\n' "$cached"
}

# parse_job_log log module-prefix -> `package<TAB>test` per failure found in a job's log.
parse_job_log() {
	awk -v prefix="$2" '
		function short(package) {
			return (index(package, prefix) == 1) ? substr(package, length(prefix) + 1) : package
		}
		function remember(test, tag) {
			if (test in seen) return
			seen[test] = 1
			pending[++npending] = test
			tags[test] = tag
		}
		# Attribute the tests gathered since the last package boundary to package, keeping only
		# the deepest failing name of each subtest chain, then start a new group.
		function flush(package, failed,    i, j, test, deeper, tag) {
			if (npending == 0 && (failed || race || timeout)) remember("(package)", "")
			for (i = 1; i <= npending; i++) {
				test = pending[i]
				deeper = 0
				for (j = 1; j <= npending; j++)
					if (index(pending[j], test "/") == 1) deeper = 1
				if (deeper) continue
				tag = tags[test]
				if (race) tag = tag " [race]"
				printf "%s\t%s%s\n", (package == "" ? "?" : short(package)), test, tag
			}
			npending = 0
			race = timeout = running = 0
			split("", seen)
			split("", tags)
		}

		# A GitHub log line is `<RFC 3339 timestamp> <line>`; Windows runners add a CR.
		{
			sub(/\r$/, "")
			sub(/^[0-9-]+T[0-9:.]+Z ?/, "")
		}

		/^--- .* chunk [0-9]+\/[0-9]+ .*---$/ {
			flush(chunk, 0)
			chunk = $2
			next
		}
		/^--- batch [0-9]+\/[0-9]+ .*---$/ { flush(chunk, 0); chunk = ""; next }
		/^[ \t]*--- FAIL: / { remember($3, ""); next }
		/WARNING: DATA RACE/ { race = 1; next }
		/^panic: test timed out/ { timeout = 1; next }
		/^running tests:/ { running = timeout; next }
		running && /^[ \t]+Test/ { remember($1, " [timeout]"); next }
		/^ok[ \t]/ { npending = 0; race = timeout = running = 0; split("", seen); next }
		/^FAIL[ \t]+[^ \t]/ {
			if ($3 ~ /^\[/) {
				reason = $0
				sub(/^[^[]*\[/, "", reason)
				sub(/\].*$/, "", reason)
				remember("(" reason ")", "")
			}
			flush($2, 1)
			next
		}
		END { flush(chunk, 0) }
	' "$1"
}

# A record is one failure in one failed job:
#   date<TAB>commit<TAB>job<TAB>platform<TAB>package<TAB>test
records="$work/records"
: >"$records"

created_filter=()
[ -n "$since" ] && created_filter=(--created ">=$since")
gh run list --workflow "$workflow" --status failure --limit "$run_limit" \
	${created_filter[@]+"${created_filter[@]}"} \
	--json databaseId,createdAt,headSha \
	--jq '.[] | [.databaseId, .createdAt[0:10], .headSha[0:8]] | @tsv' >"$work/runs"

run_count=$(wc -l <"$work/runs" | tr -d ' ')
if [ "$run_count" -eq 0 ]; then
	echo "ci-flakes: no failed $workflow runs${since:+ since $since}"
	exit 0
fi
echo "ci-flakes: reading $run_count failed $workflow run(s) of $repository${since:+ since $since}" >&2

while IFS="$(printf '\t')" read -r run_id run_date commit; do
	# The runner label names the platform; the failing steps name a job that failed outside
	# any test.
	gh api --paginate "repos/$repository/actions/runs/$run_id/jobs" --jq '
		.jobs[]
		| select(.conclusion == "failure" or .conclusion == "timed_out")
		| (.labels | join(" ")) as $labels
		| [
			.id,
			.name,
			(if ($labels | test("windows")) then "windows"
			 elif ($labels | test("macos")) then "macos"
			 elif ($labels | test("ubuntu|linux")) then "linux"
			 else $labels end),
			([.steps[]? | select(.conclusion == "failure") | .name] | join("; ")
			 | if . == "" then "(timed out)" else . end)
		]
		| @tsv' >"$work/jobs"

	while IFS="$(printf '\t')" read -r job_id job_name platform failed_steps; do
		prefix="$run_date	$commit	$job_name	$platform"
		if ! log=$(fetch_job_log "$job_id"); then
			printf '%s\t-\t(log unavailable)\n' "$prefix" >>"$records"
			continue
		fi
		parse_job_log "$log" "$module_prefix" >"$work/failures"
		if [ -s "$work/failures" ]; then
			awk -v prefix="$prefix" '{ print prefix "\t" $0 }' "$work/failures" >>"$records"
		else
			printf '%s\t-\tstep: %s\n' "$prefix" "$failed_steps" >>"$records"
		fi
	done <"$work/jobs"
done <"$work/runs"

echo "Failed jobs, newest run first"
echo ""
awk -F '\t' '
	{
		key = $1 "\t" $2 "\t" $3
		if (!(key in failures)) { order[++n] = key; failures[key] = $6 }
		else failures[key] = failures[key] ", " $6
		if (length($3) > width) width = length($3)
	}
	END {
		# The width goes into the format string: not every awk takes a `*` width.
		format = "%-10s  %-8s  %-" width "s  %s\n"
		printf format, "DATE", "COMMIT", "JOB", "FAILED"
		for (i = 1; i <= n; i++) {
			split(order[i], field, "\t")
			printf format, field[1], field[2], field[3], failures[order[i]]
		}
	}
' "$records"

echo ""
echo "Failures by test, most frequent first"
echo ""
# Sorted on the count, then the test, so equal counts read alphabetically and the order is
# stable from one run to the next.
awk -F '\t' '
	{
		key = $6 "\t" $5 "\t" $4
		count[key]++
		if (index("," dates[key] ",", "," $1 ",") == 0)
			dates[key] = (dates[key] == "" ? $1 : dates[key] "," $1)
	}
	END { for (key in count) printf "%d\t%s\t%s\n", count[key], key, dates[key] }
' "$records" | sort -t "$(printf '\t')" -k1,1nr -k2,2 -k3,3 -k4,4 | awk -F '\t' '
	{
		rows[NR] = $0
		if (length($2) > test_width) test_width = length($2)
		if (length($3) > package_width) package_width = length($3)
		if (length($4) > platform_width) platform_width = length($4)
	}
	END {
		if (test_width < 4) test_width = 4
		if (package_width < 7) package_width = 7
		if (platform_width < 8) platform_width = 8
		format = "%-" test_width "s  %-" package_width "s  %-" platform_width "s  %5s  %s\n"
		printf format, "TEST", "PACKAGE", "PLATFORM", "COUNT", "DATES"
		for (i = 1; i <= NR; i++) {
			split(rows[i], field, "\t")
			printf format, field[2], field[3], field[4], field[1], field[5]
		}
	}
'
