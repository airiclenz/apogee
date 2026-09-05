#!/usr/bin/env bash
# type.sh — expand one typed string into a humanized VHS typing block.
#
#   ./type.sh 'apogee --mode auto'           # the block for one string, default seed
#   ./type.sh --seed 99 'apogee --mode auto' # the same string, a different rhythm
#   ./type.sh --strings                      # the hero tape's typed strings, one per line
#
# VHS types on a fixed metronome (`Set TypingSpeed 40ms`), which reads on camera as a machine
# at the keyboard. This prints an equivalent block that drops the metronome to 0ms and carries
# its own `Sleep` between characters instead, so the take shows a human rhythm. `gen.sh`
# splices these blocks into the work-dir copy of a tape at record time; the source tape keeps
# its plain `Type "…"` lines so it stays readable and diffable.
#
# The rhythm is a fixed profile of four bands — per-letter, after a space, after punctuation,
# and an occasional thinking pause at a word boundary that replaces the space gap. Those four
# bands are deliberately the only Sleep values emitted, so the profile can be asserted.
#
# The rhythm must also be byte-stable across machines, or two people recording the same tape
# would get different takes. That is why the seed is fixed and the generator carries its own
# MINSTD rather than calling awk's `srand()`/`rand()`, whose sequences differ between BSD awk,
# gawk and mawk. Every product stays under 2^53, so the arithmetic is exact in awk's doubles
# on all three.
#
# This script is the single owner of the hero tape's typed strings: `--strings` publishes the
# table so nothing downstream has to keep a second copy that could drift out of step with it.
set -euo pipefail

readonly DEFAULT_SEED=4242

# The typing profile, in milliseconds. Ratified with the plan — edit only deliberately, since
# every band change moves the recorded duration of every typed line.
readonly LETTER_GAP_MIN=25
readonly LETTER_GAP_MAX=45
readonly SPACE_GAP_MIN=60
readonly SPACE_GAP_MAX=90
readonly PUNCTUATION_GAP_MIN=90
readonly PUNCTUATION_GAP_MAX=140
readonly THINKING_PAUSE_MIN=300
readonly THINKING_PAUSE_MAX=500
readonly PUNCTUATION_CHARACTERS='.,-!'

# A thinking pause replaces the gap after a space one time in eight, and never more than twice
# in one string — a third would read as the tool having stalled rather than the user thinking.
readonly THINKING_PAUSE_ODDS=8
readonly THINKING_PAUSE_LIMIT=2

# The metronome the block restores on the way out, matching the tape's own default.
readonly TAPE_TYPING_SPEED_MS=40

# The hero tape's human-typed strings, in tape order. The two machine-typed lines it also
# contains — the hidden `source ./env.sh` and the `[1;3A` cursor-up escape — are deliberately
# absent: they are not a person at a keyboard and must keep their exact recorded speeds.
readonly -a HERO_STRINGS=(
  'apogee --mode auto'
  'the test suite is failing - find the bug, fix it, and prove the tests pass'
  'also add a CHANGELOG entry for the fix'
  '/undo'
)

usage() {
  sed -n '2,6p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

fail() {
  echo "type.sh: $1" >&2
  exit 2
}

seed="$DEFAULT_SEED"
print_strings=false

while [ $# -gt 0 ]; do
  case "$1" in
    --seed)
      [ $# -ge 2 ] || fail "--seed needs a value"
      seed="$2"
      shift 2
      ;;
    --strings)
      print_strings=true
      shift
      ;;
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

# MINSTD's state must be a non-zero residue: 0 is a fixed point and the modulus itself maps to
# 0, so either seed would type the whole string on one flat interval.
case "$seed" in
  ''|*[!0-9]*) fail "--seed must be a positive integer, got: $seed" ;;
esac
[ "$seed" -gt 0 ] && [ "$seed" -lt 2147483647 ] || fail "--seed must be in 1..2147483646, got: $seed"

if [ "$print_strings" = true ]; then
  [ $# -eq 0 ] || fail "--strings takes no string argument"
  printf '%s\n' "${HERO_STRINGS[@]}"
  exit 0
fi

[ $# -eq 1 ] || fail "expected exactly one string to type, got $# (usage: type.sh [--seed N] <string>)"
text="$1"

# The block is spliced into a tape as `Type "c"` lines, so a character that would close or
# escape that quote — or one a terminal would not render as itself — has to be refused here
# rather than producing a tape VHS mis-parses at record time.
[ -n "$text" ] || fail "the string to type is empty"
case "$text" in
  *'"'*)  fail 'the string to type contains a double quote, which Type "…" cannot carry' ;;
  *'\'*)  fail 'the string to type contains a backslash, which Type "…" cannot carry' ;;
esac
if LC_ALL=C grep -q '[^ -~]' <<<"$text"; then
  fail 'the string to type contains a non-printable or non-ASCII character'
fi

# The per-letter band is overridable only so the profile check can drive its own failure path;
# nothing in the recording rig sets these, and they are deliberately undocumented in README.md.
letter_gap_min="${APOGEE_DEMO_JITTER_MIN:-$LETTER_GAP_MIN}"
letter_gap_max="${APOGEE_DEMO_JITTER_MAX:-$LETTER_GAP_MAX}"

TYPE_SH_TEXT="$text" awk \
  -v seed="$seed" \
  -v letterMin="$letter_gap_min" -v letterMax="$letter_gap_max" \
  -v spaceMin="$SPACE_GAP_MIN" -v spaceMax="$SPACE_GAP_MAX" \
  -v punctuationMin="$PUNCTUATION_GAP_MIN" -v punctuationMax="$PUNCTUATION_GAP_MAX" \
  -v thinkingMin="$THINKING_PAUSE_MIN" -v thinkingMax="$THINKING_PAUSE_MAX" \
  -v thinkingOdds="$THINKING_PAUSE_ODDS" -v thinkingLimit="$THINKING_PAUSE_LIMIT" \
  -v punctuation="$PUNCTUATION_CHARACTERS" \
  -v tapeSpeed="$TAPE_TYPING_SPEED_MS" '
# MINSTD (Lehmer, 16807 / 2^31-1). Spelled out rather than delegated to rand() so the sequence
# is identical under BSD awk, gawk and mawk; see the file header.
function advanceState() {
  minstdState = (16807 * minstdState) % 2147483647
  return minstdState
}

function draw(low, high) {
  return low + (advanceState() % (high - low + 1))
}

BEGIN {
  text = ENVIRON["TYPE_SH_TEXT"]
  minstdState = seed
  advanceState()        # discard the first value: a small seed would open on a small gap

  printf "# generated by type.sh --seed %d — source: \"%s\"\n", seed, text
  print "Set TypingSpeed 0ms"

  characterCount = length(text)
  thinkingPauses = 0

  for (position = 1; position <= characterCount; position++) {
    character = substr(text, position, 1)
    printf "Type \"%s\"\n", character

    # The gap belongs between two characters, so the last one does not get one: the hover
    # before Enter is the tape own Sleep, and a trailing gap here would double-count it.
    if (position == characterCount) continue

    if (character == " ") {
      if (draw(1, thinkingOdds) == 1 && thinkingPauses < thinkingLimit) {
        gap = draw(thinkingMin, thinkingMax)
        thinkingPauses++
      } else {
        gap = draw(spaceMin, spaceMax)
      }
    } else if (index(punctuation, character) > 0) {
      gap = draw(punctuationMin, punctuationMax)
    } else {
      gap = draw(letterMin, letterMax)
    }

    printf "Sleep %dms\n", gap
  }

  printf "Set TypingSpeed %dms\n", tapeSpeed
}
'
