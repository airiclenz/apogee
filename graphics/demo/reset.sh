#!/usr/bin/env bash
# Reset the stage between takes: restore the planted bug and the CHANGELOG stub, and wipe
# session/prompt state so recall or a stale session title can't leak into the next clip.
# The gitignored Go caches survive, so takes stay fast.
set -euo pipefail

WORK="${APOGEE_DEMO_WORK:-$HOME/.cache/apogee-demo}"
DEMO_HOME="$WORK/home"
STAGE="$DEMO_HOME/Repos/taskman"

[ -d "$STAGE/.git" ] || { echo "no stage at $STAGE — run setup.sh first" >&2; exit 1; }

git -C "$STAGE" checkout -- .
git -C "$STAGE" clean -qfd          # -d but not -x: ignored caches are kept warm
rm -rf "$DEMO_HOME/.apogee/sessions" "$DEMO_HOME/.apogee/prompts"

# The stage is supposed to fail, so `go test` exits non-zero here — capture its output
# rather than piping, or pipefail reports the intended red as a reset failure.
out="$(cd "$STAGE" && GOCACHE="$STAGE/.gocache" GOPATH="$STAGE/.gopath" TMPDIR="$STAGE/.gotmp" \
        go test ./... 2>&1 || true)"

if printf '%s' "$out" | grep -q 'Pending returned 1 tasks, want 2'; then
  echo "stage reset: bug present, tests red"
else
  echo "WARNING: stage did not reset to the expected red state" >&2
  printf '%s\n' "$out" >&2
  exit 1
fi
