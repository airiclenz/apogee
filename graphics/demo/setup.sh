#!/usr/bin/env bash
# Build the demo rig: an isolated apogee home, the taskman stage repo with its planted
# bug, and the env.sh that tapes source. Idempotent — re-run it any time; it rebuilds
# the stage from the templates in stage/ and leaves the warm Go cache alone.
#
#   ./setup.sh
#
# Overridable:
#   APOGEE_DEMO_WORK        where the rig is built    (default ~/.cache/apogee-demo)
#   APOGEE_DEMO_ENDPOINT    LLM server URL            (default https://openrouter.ai/api — apogee appends /v1/…)
#   APOGEE_DEMO_HOST_ALIAS  the name in the footer    (default openrouter)
#   APOGEE_DEMO_MODEL       the model in the footer   (default ~deepseek/deepseek-v4-flash-latest)
#   APOGEE_DEMO_KEY_ENV     env var holding the key   (default OPENROUTER_API_KEY; empty = keyless)
#
# Both the alias and the model id are ON CAMERA in the footer for the whole clip. The key is
# never written anywhere: the config carries `api-key-env: <name>` and apogee reads the variable
# from the environment the recording runs in (record.sh refuses to start without it).
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"

WORK="${APOGEE_DEMO_WORK:-$HOME/.cache/apogee-demo}"
ENDPOINT="${APOGEE_DEMO_ENDPOINT:-https://openrouter.ai/api}"
HOST_ALIAS="${APOGEE_DEMO_HOST_ALIAS:-openrouter}"
MODEL="${APOGEE_DEMO_MODEL:-~deepseek/deepseek-v4-flash-latest}"
KEY_ENV="${APOGEE_DEMO_KEY_ENV-OPENROUTER_API_KEY}"

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
# `~`. Isolation is via HOME rather than --config so the on-screen command stays a bare
# `apogee …` and sessions never land in the real ~/.apogee.
key_line=""
[ -n "$KEY_ENV" ] && key_line="$(printf '    api-key-env: %s\n' "$KEY_ENV")"
{
  cat "$REPO/internal/config/defaults/config.yaml"
  printf '\nservers:\n  - name: %s\n    endpoint: %s\n    model: "%s"\n' \
      "$HOST_ALIAS" "$ENDPOINT" "$MODEL"
  [ -n "$key_line" ] && printf '%s\n' "$key_line"
  printf '\nserver: %s\n' "$HOST_ALIAS"
} > "$DEMO_HOME/.apogee/config.yaml"

# env.sh is generated (not checked in) because it bakes in machine-specific absolute paths.
# Tapes source it from a Hide block so none of this appears on camera. The API key is NOT
# baked in: VHS's shell inherits the environment record.sh runs in, and api-key-env reads it
# from there.
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

clear
ENV

printf 'KEY_ENV=%s\n' "$KEY_ENV" > "$WORK/rig.env"

cp "$HERE/reset.sh" "$WORK/reset.sh"
chmod +x "$WORK/reset.sh"

# Warm the build cache so the first on-camera `go test` is fast rather than a cold compile.
( set -a; . "$WORK/env.sh" >/dev/null 2>&1; set +a; go test ./... >/dev/null 2>&1 || true )

echo "demo rig ready"
echo "  work dir : $WORK"
echo "  stage    : $STAGE"
echo "  endpoint : $ENDPOINT   server: $HOST_ALIAS   model: $MODEL"
if [ -n "$KEY_ENV" ]; then
  if [ -n "${!KEY_ENV:-}" ]; then
    echo "  api key  : from \$$KEY_ENV (set)"
  else
    echo "  api key  : from \$$KEY_ENV — NOT SET in this shell; record.sh will refuse until it is"
  fi
fi
echo
echo "next: ./record.sh hero    (or any tape name under tapes/)"
