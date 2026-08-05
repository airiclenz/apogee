#!/usr/bin/env bash
# Turn a raw take into the shipping GIF: optional head trim, optional time-compression of
# the waiting stretches, then a palette-based GIF encode (much cleaner than ffmpeg's default
# 256-colour quantiser on flat terminal colours).
#
#   ./render.sh <in.mp4> <out.gif> [speed] [start-seconds]
#
#   ./render.sh ~/.cache/apogee-demo/hero.mp4 ../demo.gif 1.8 3.8
#     -> drops the first 3.8s (the shell + launch, so the clip opens already inside apogee)
#        and compresses 1.8x, taking a ~77s take to ~40s.
#
# Recording deliberately runs at real speed with generous Sleeps; pace is decided here, so
# a re-pace never costs another take of a nondeterministic model.
set -euo pipefail

IN="${1:?usage: render.sh <in.mp4> <out.gif> [speed] [start-seconds]}"
OUT="${2:?usage: render.sh <in.mp4> <out.gif> [speed] [start-seconds]}"
SPEED="${3:-1.0}"
START="${4:-0}"

[ -f "$IN" ] || { echo "no such take: $IN" >&2; exit 1; }

ss=()
[ "$START" != "0" ] && ss=(-ss "$START")

ffmpeg -y -loglevel error "${ss[@]}" -i "$IN" -filter_complex \
  "[0:v]setpts=PTS/$SPEED,fps=24,scale=1250:-1:flags=lanczos,split[a][b];\
   [a]palettegen=max_colors=192[p];[b][p]paletteuse=dither=bayer:bayer_scale=3" \
  "$OUT"

# gifsicle is optional; it typically buys another 20-40% with no visible loss.
if command -v gifsicle >/dev/null 2>&1; then
  gifsicle -O3 --lossy=80 -o "$OUT.opt" "$OUT" && mv "$OUT.opt" "$OUT"
fi

printf '%s  %s  %ss\n' "$OUT" \
  "$(du -h "$OUT" | cut -f1)" \
  "$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$OUT")"
