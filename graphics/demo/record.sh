#!/usr/bin/env bash
# Record one take. Resets the stage first, then runs the named tape in the work dir.
#
#   ./record.sh hero          # runs tapes/hero.tape -> <work>/hero.mp4
#
# The tape is copied into the work dir before running: VHS resolves `source ./env.sh` and
# its Output paths relative to the working directory, and its parser has been seen to choke
# on very long Output paths — short and relative keeps both problems away.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORK="${APOGEE_DEMO_WORK:-$HOME/.cache/apogee-demo}"

TAPE="${1:-hero}"
SRC="$HERE/tapes/$TAPE.tape"

[ -f "$SRC" ] || { echo "no such tape: $SRC" >&2; ls "$HERE/tapes/" >&2; exit 1; }
[ -f "$WORK/env.sh" ] || { echo "no rig at $WORK — run setup.sh first" >&2; exit 1; }

# The config names an env var as the key source (api-key-env); apogee reads it from the
# environment VHS's shell inherits from this one. A missing key would only surface as the
# first request failing on camera, so refuse here instead.
KEY_ENV=""
[ -f "$WORK/rig.env" ] && . "$WORK/rig.env"
if [ -n "$KEY_ENV" ] && [ -z "${!KEY_ENV:-}" ]; then
  echo "\$$KEY_ENV is not set — export it before recording (the config reads the key from it)" >&2
  exit 1
fi

"$HERE/reset.sh"

cp "$SRC" "$WORK/$TAPE.tape"
cd "$WORK"
echo "recording $TAPE …"
time vhs "$TAPE.tape"

echo
echo "raw take: $WORK/$TAPE.mp4"
echo "post-process with: $HERE/render.sh $WORK/$TAPE.mp4 <out.gif> [speed] [start]"
