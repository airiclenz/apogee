#!/usr/bin/env bash
# Build the demo rig: an isolated apogee home, the taskman stage repo with its planted
# bug, the env.sh a take launches apogee under, and rig.env. Idempotent — re-run it any
# time; it rebuilds the stage from the templates in stage/ and leaves the warm Go cache alone.
#
#   ./setup.sh
#
# Overridable:
#   APOGEE_DEMO_WORK        where the rig is built    (default ~/.cache/apogee-demo)
#   APOGEE_DEMO_PORT        the local model port      (default 18181)
#   APOGEE_DEMO_HOST_ALIAS  the name in the footer    (default openrouter)
#   APOGEE_DEMO_MODEL       the model in the footer   (default ~deepseek/deepseek-v4-flash-latest)
#
# apogee never talks to a live server directly: its one server entry points at
# http://127.0.0.1:<port>, keyless, where `demorig record` serves the clip's cassette and
# `demorig capture` serves a recording proxy to the live model (the proxy holds the key).
# Both the alias and the model id are ON CAMERA in the footer for the whole clip, and the
# cassette is keyed by conversation, model id included — change either and re-capture.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"

WORK="${APOGEE_DEMO_WORK:-$HOME/.cache/apogee-demo}"
PORT="${APOGEE_DEMO_PORT:-18181}"
HOST_ALIAS="${APOGEE_DEMO_HOST_ALIAS:-openrouter}"
MODEL="${APOGEE_DEMO_MODEL:-~deepseek/deepseek-v4-flash-latest}"

DEMO_HOME="$WORK/home"
STAGE="$DEMO_HOME/Repos/taskman"

# The stage repo lives at $DEMO_HOME/Repos/taskman on purpose: HOME is remapped during a
# recording, so the footer renders the workdir as the clean "~/Repos/taskman" rather than
# a long absolute path that would give the staging away.
mkdir -p "$STAGE" "$DEMO_HOME/.apogee"

cp "$HERE/stage/go.mod" "$HERE/stage/task.go" "$HERE/stage/task_test.go" \
   "$HERE/stage/CHANGELOG.md" "$STAGE/"
cp "$HERE/stage/gitignore" "$STAGE/.gitignore"

# A git repo makes resets trivial (git checkout) and makes the workspace realistic on camera.
if [ ! -d "$STAGE/.git" ]; then
  git -C "$STAGE" init -q
fi
git -C "$STAGE" add -A
git -C "$STAGE" -c user.email=demo@local -c user.name=demo \
    diff --cached --quiet || git -C "$STAGE" -c user.email=demo@local -c user.name=demo \
    commit -qm "taskman: stage with the planted Pending() bug"

# Seed the isolated apogee config from the binary's own template, then append the one server the
# clip runs on. The `servers:` list is the single definition of what apogee can talk to and
# `server:` names the entry a session starts on (ADR 0036); the entry's own name IS the alias the
# footer shows, so one value does both jobs. `model:` pins what goes on the wire so no picker
# beat is needed on camera, and it is quoted because an OpenRouter "latest" alias starts with
# `~`. `parallel-agents: 4` pins the sub-agent width, so the fan-out does not depend on what the
# local port advertises. The entry carries no key: the cassette replayer needs none, and the
# capture proxy authenticates upstream itself. Isolation is via HOME rather than --config so
# sessions never land in the real ~/.apogee.
{
  cat "$REPO/internal/config/defaults/config.yaml"
  printf '\nservers:\n  - name: %s\n    endpoint: http://127.0.0.1:%s\n    model: "%s"\n    parallel-agents: 4\n' \
      "$HOST_ALIAS" "$PORT" "$MODEL"
  printf '\nserver: %s\n' "$HOST_ALIAS"
} > "$DEMO_HOME/.apogee/config.yaml"

# env.sh is generated (not checked in) because it bakes in machine-specific absolute paths.
# `demorig record` and `demorig capture` source it before they exec apogee in the take's pty,
# so none of this appears on camera.
cat > "$WORK/env.sh" <<ENV
export HOME=$DEMO_HOME
export PATH=$(dirname "$(command -v apogee || echo /usr/local/bin/apogee)"):\$PATH
cd "\$HOME/Repos/taskman"

# Go's build cache and temp dir normally live outside the workspace, which apogee's
# seatbelt confinement fences off in auto mode. Without this the model burns ~7 tool calls
# rediscovering it (GOCACHE/GOPATH/TMPDIR/mkdir/whoami) before \`go test\` ever works.
export GOCACHE="\$PWD/.gocache"
export GOPATH="\$PWD/.gopath"
export GOMODCACHE="\$PWD/.gopath/pkg/mod"
export TMPDIR="\$PWD/.gotmp"
mkdir -p "\$GOCACHE" "\$GOPATH" "\$TMPDIR"
ENV

# rig.env is read by demorig: PORT is where a take serves the model apogee's server entry names.
printf 'PORT=%s\n' "$PORT" > "$WORK/rig.env"

cp "$HERE/reset.sh" "$WORK/reset.sh"
chmod +x "$WORK/reset.sh"

# Warm the build cache so the first on-camera `go test` is fast rather than a cold compile.
( set -a; . "$WORK/env.sh" >/dev/null 2>&1; set +a; go test ./... >/dev/null 2>&1 || true )

echo "demo rig ready"
echo "  work dir : $WORK"
echo "  stage    : $STAGE"
echo "  server   : $HOST_ALIAS at http://127.0.0.1:$PORT   model: $MODEL"
echo
echo "next (from the repo root): go run ./cmd/demorig record graphics/demo/storyboards/hero.yaml"
