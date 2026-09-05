#!/usr/bin/env bash
# gen.sh — expand a tape's human-typed lines into humanized typing blocks.
#
#   ./gen.sh tapes/hero.tape /tmp/hero.tape    # the hero take, with humanized typing
#   ./gen.sh tapes/other.tape /tmp/other.tape  # any other tape, copied through verbatim
#
# `record.sh` copies the tape it is about to run into the work dir; this is that copy with the
# hero tape's typing rewritten on the way through. Every line is passed along untouched except
# the four lines the hero tape types as a person, each of which is replaced by the `type.sh`
# block for the same string — so the take shows a human rhythm instead of VHS's 40ms metronome.
# The source tape keeps its plain `Type "…"` lines, which is what keeps it readable and
# diffable; the expansion exists only in the work-dir copy.
#
# Only the hero tape is expanded, because it is the only tape whose typed strings are a person
# at a keyboard. The two machine-typed lines the hero tape also carries pass through this gate
# untouched by construction: the hidden `Type "source ./env.sh"` keeps the tape's 40ms, and the
# `Type "[1;3A"` cursor-up escape keeps its 0ms — splitting that ESC from its CSI tail reaches
# apogee as a bare Escape, which mid-run means "stop the run" (README.md, VHS pitfalls).
#
# Matching is exact whole-line equality, never a regex: `/undo` carries a `/` and the task
# prompt carries `.` and `-`, so a pattern language would need escaping this does not need.
# Line numbers are not used either — the tape's comments move as it is edited.
#
# The strings themselves are read from `type.sh --strings` rather than repeated here: a second
# copy of the table would let a reworded hero string drift, leaving this script pinning a line
# the tape no longer contains. That is exactly the failure the guard below refuses to record.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly HERE
readonly TYPE_SH="$HERE/type.sh"

# The hero take's rhythm is pinned here rather than left to type.sh's default: a fixed seed is
# what makes two people recording the same tape produce byte-identical typing.
readonly HERO_SEED=4242

# The one tape whose typed strings are humanized. Every other tape is copied through as is.
readonly HERO_TAPE_BASENAME='hero.tape'

usage() {
  sed -n '2,5p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

fail() {
  echo "gen.sh: $1" >&2
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      break
      ;;
    *)
      break
      ;;
  esac
done

[ $# -eq 2 ] || fail "expected exactly two arguments (usage: gen.sh <src.tape> <dst.tape>), got $#"
source_tape="$1"
destination_tape="$2"

[ -f "$source_tape" ] || fail "no such tape: $source_tape"

# Every other tape is a plain copy: nothing in it is typed by a person, so there is nothing to
# humanize and no reason to make the rig depend on type.sh for it.
if [ "$(basename "$source_tape")" != "$HERO_TAPE_BASENAME" ]; then
  cp "$source_tape" "$destination_tape"
  exit 0
fi

[ -x "$TYPE_SH" ] || fail "type.sh is missing or not executable: $TYPE_SH"

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

strings_file="$work_dir/strings"
"$TYPE_SH" --strings >"$strings_file"
[ -s "$strings_file" ] || fail "type.sh --strings returned nothing"

string_number=0
while IFS= read -r hero_string; do
  string_number=$((string_number + 1))
  "$TYPE_SH" --seed "$HERO_SEED" "$hero_string" >"$work_dir/block.$string_number"
done <"$strings_file"

# The expanded tape is built aside and moved into place only once the guard has passed, so a
# refused expansion never leaves a half-written tape behind for VHS to run.
expanded_tape="$work_dir/expanded.tape"

# awk reports its complaints on stdout — the tape goes to a file — so a guard failure can be
# relayed to stderr here without depending on /dev/stderr, which the three awks disagree about.
if ! guard_report="$(
  awk \
    -v workDir="$work_dir" \
    -v outputFile="$expanded_tape" \
    -v tapePath="$source_tape" '
# First file: the table published by type.sh --strings. Each string becomes the exact tape line
# it is expected to appear as, so the copy pass below is a lookup rather than a match.
NR == FNR {
  heroString[FNR] = $0
  blockNumberFor["Type \"" $0 "\""] = FNR
  heroStringCount = FNR
  next
}

$0 in blockNumberFor {
  blockNumber = blockNumberFor[$0]
  matchCount[blockNumber]++
  blockFile = workDir "/block." blockNumber
  while ((getline blockLine < blockFile) > 0) {
    print blockLine >outputFile
  }
  close(blockFile)
  next
}

{
  print >outputFile
}

END {
  close(outputFile)

  # A string that matches no line means the tape was reworded without the table; one that
  # matches several means the block would be spliced twice. Either way the take would be wrong
  # in a way nobody notices until the GIF is watched, so refuse it now and name the string.
  for (blockNumber = 1; blockNumber <= heroStringCount; blockNumber++) {
    if (matchCount[blockNumber] != 1) {
      printf "%s matches %d lines of %s, expected exactly 1: \"%s\"\n",
        "string " blockNumber, matchCount[blockNumber] + 0, tapePath, heroString[blockNumber]
      guardFailed = 1
    }
  }
  if (guardFailed) {
    exit 1
  }
}
' "$strings_file" "$source_tape"
)"; then
  [ -z "$guard_report" ] || printf '%s\n' "$guard_report" | sed 's/^/gen.sh: /' >&2
  exit 1
fi

mv "$expanded_tape" "$destination_tape"
