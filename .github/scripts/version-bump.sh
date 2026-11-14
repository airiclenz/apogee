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

# The burned v1.x versions (internal/update's RetractedVersions, which go.mod's retract block
# lists and cmd/release refuses; TestRepoVersionBumpScriptBurnsTheSameList fails when this line
# drifts from it). A tag on a burned number ships already retracted, and Go reads retractions from
# the highest version's go.mod, so a tag whose go.mod retracts anything but exactly this list
# either brings a burned number back or retracts a releasable one. Both are refused here, as
# `go run ./cmd/release prepare` refuses them before the push. A refusal is an `::error::`
# annotation, not a failed step, so the other bumps in the same push are still tagged;
# TestRepoKeepsTheRetractLineAndAReleasableVersion turns CI red on that commit.
burned='v1.1.0 v1.7.0 v1.8.0 v1.8.1'

is_burned() {  # $1 is vMAJOR.MINOR.PATCH
  case " $burned " in *" $1 "*) return 0 ;; esac
  return 1
}

# Every entry the go.mod at commit $1 retracts, one per line in file order: a version or a range
# as written. It reads the forms prepare's checkRetractKept reads — one-line `retract vX`
# directives and `retract ( … )` blocks, comments stripped first — so a go.mod prepare accepts is
# never refused here. A missing go.mod retracts nothing.
retractions() {
  git show "$1:go.mod" 2>/dev/null | awk '
    { sub(/\/\/.*/, ""); sub(/^[ \t\r]+/, ""); sub(/[ \t\r]+$/, "") }
    in_block { if ($0 == ")") in_block = 0; else if ($0 != "") print; next }
    /^retract([[ \t(]|$)/ {
      argument = substr($0, length("retract") + 1)
      sub(/^[ \t]+/, "", argument)
      if (argument == "(") in_block = 1
      else if (argument != "") print argument
    }
  ' || true
}

retracts_the_burned_list() {  # $1 is a commit; the same set as $burned, ignoring order and repeats
  [ "$(retractions "$1" | sort -u)" = "$(tr ' ' '\n' <<< "$burned" | sort -u)" ]
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
  elif is_burned "$new"; then
    echo "::error::VERSION at ${target} reads ${new}, a burned version (${burned}) — refusing to tag"
  elif ! retracts_the_burned_list "$target"; then
    found="$(retractions "$target" | awk 'NR > 1 { printf ", " } { printf "%s", $0 }')"
    echo "::error::go.mod at ${target} retracts ${found:-nothing}, not exactly the burned versions (${burned}) — refusing to tag ${new}"
  else
    bumps="${bumps:+$bumps }${target}=${new}"
  fi
done

echo "bumps=$bumps" >> "${GITHUB_OUTPUT:-/dev/stdout}"
