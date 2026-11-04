#!/usr/bin/env bash
# Decide whether a pushed range changed VERSION, and which commit(s) to tag for it.
# Usage: version-bump.sh <before-sha> <after-sha>
#
# Writes one output, `bumps`, to $GITHUB_OUTPUT (stdout when unset, so the decision
# is runnable by hand): a space-separated list of `<sha>=<tag>` pairs, oldest first,
# one per commit in the range that changed VERSION to a vX.Y.Z value. Empty when
# there is nothing to tag.
set -euo pipefail

before="${1:?before sha}"
after="${2:?after sha}"

zero='0000000000000000000000000000000000000000'
# Branch creation, or a history rewrite the clone no longer holds: fall back to the
# parent of the pushed head so a single commit is still comparable.
if [ "$before" = "$zero" ] || ! git cat-file -e "${before}^{commit}" 2>/dev/null; then
  before="$(git rev-parse --verify --quiet "${after}^" || true)"
fi
if [ -n "$before" ]; then range="${before}..${after}"; else range="$after"; fi

read_version() {  # missing file or missing commit yields empty, never a failed step
  git show "$1:VERSION" 2>/dev/null | tr -d ' \t\r\n' || true
}

# The retracted v1.0.0–v1.8.0 series (go.mod's `retract [v1.0.0, v1.8.0]`; cmd/release
# keeps the same bounds). A tag inside it ships already retracted, and Go reads the
# retraction from the highest version's go.mod, so a tag whose go.mod has lost the line
# brings the series back. Both are refused here, as `go run ./cmd/release prepare` refuses
# them before the push. A refusal is an `::error::` annotation, not a failed step, so the
# other bumps in the same push are still tagged; TestRepoKeepsTheRetractLineAndAReleasableVersion
# turns CI red on that commit.
retract_line='retract [v1.0.0, v1.8.0]'

is_retracted() {  # $1 is vMAJOR.MINOR.PATCH
  local major minor patch
  IFS=. read -r major minor patch <<< "${1#v}"
  [ "$major" = 1 ] && { [ "$minor" -lt 8 ] || { [ "$minor" = 8 ] && [ "$patch" = 0 ]; }; }
}

keeps_retract_line() {  # $1 is a commit
  git show "$1:go.mod" 2>/dev/null | sed 's/^[[:space:]]*//; s/[[:space:]]*$//' | grep -qxF "$retract_line"
}

# EVERY commit in the pushed range that changed VERSION, oldest first — not the push
# head, and not only the last one. A push here is routinely a burst of commits; the
# bump is a standalone commit that later commits sit on top of, and a burst can carry
# two bumps.
bumps=''
for target in $(git log --first-parent --reverse --format=%H "$range" -- VERSION || true); do
  new="$(read_version "$target")"
  old="$(read_version "${target}^")"
  [ -n "$new" ] && [ "$new" != "$old" ] || continue
  if ! printf '%s' "$new" | grep -qE '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'; then
    echo "::error::VERSION at ${target} reads '${new}', not a vX.Y.Z value — refusing to tag"
  elif is_retracted "$new"; then
    echo "::error::VERSION at ${target} reads ${new}, inside the retracted v1.0.0–v1.8.0 series — refusing to tag"
  elif ! keeps_retract_line "$target"; then
    echo "::error::go.mod at ${target} has lost the '${retract_line}' line — refusing to tag ${new}"
  else
    bumps="${bumps:+$bumps }${target}=${new}"
  fi
done

echo "bumps=$bumps" >> "${GITHUB_OUTPUT:-/dev/stdout}"
