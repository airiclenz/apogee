#!/usr/bin/env bash
# check-pins.sh — every third-party GitHub Action this repo runs must be pinned to a
# 40-character commit SHA, with the release tag it corresponds to in a trailing comment.
#
# A tag reference (`actions/checkout@v4`) is a moving target: whoever controls the tag
# controls what runs in CI, with the repository's token in scope. A SHA is immutable, and
# the `# vX.Y.Z` comment beside it is what makes a bump reviewable — Dependabot rewrites
# both together, so a stale comment beside a fresh SHA is itself the finding (checklist
# T-21). This script is the static half of that item: it runs from `make check`, so a pin
# can never regress between one CI run and the next.
#
# Usage: scripts/check-pins.sh [workflow-file ...]   (default: .github/workflows/*.yml)

set -euo pipefail

cd "$(dirname "$0")/.."

files=("$@")
if [ ${#files[@]} -eq 0 ]; then
	# Nullglob so an empty .github/workflows is "nothing to check", not a literal pattern.
	shopt -s nullglob
	files=(.github/workflows/*.yml .github/workflows/*.yaml)
	shopt -u nullglob
fi

if [ ${#files[@]} -eq 0 ]; then
	echo "check-pins: no workflow files found" >&2
	exit 1
fi

# The one shape a `uses:` may take. Local actions (`./.github/actions/...`) and reusable
# workflows in this same repository are not third-party and carry no SHA, so they are
# exempt — everything else must be owner/repo[/path]@<40 hex> # vX.Y.Z.
pinned='^[^[:space:]]+@[0-9a-f]{40}[[:space:]]+#[[:space:]]*v[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$'

status=0
checked=0

for f in "${files[@]}"; do
	# Every `uses:` line, with its line number, however it is indented and whether or not
	# it sits on a `- uses:` list item.
	while IFS=: read -r lineno line; do
		ref="${line#*uses:}"
		# Strip the surrounding whitespace and any quoting around the reference.
		ref="$(printf '%s' "$ref" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e "s/^['\"]//" -e "s/['\"]\([[:space:]]*#\)/\1/")"
		checked=$((checked + 1))

		case "$ref" in
		./* | docker://*)
			# A local action or a pinned container image: no SHA to check here.
			continue
			;;
		esac

		if ! printf '%s' "$ref" | grep -Eq "$pinned"; then
			printf '%s:%s: unpinned action: %s\n' "$f" "$lineno" "$ref" >&2
			status=1
		fi
	done < <(grep -nE '^[[:space:]]*(-[[:space:]]+)?uses:' "$f" || true)
done

if [ "$status" -ne 0 ]; then
	echo "" >&2
	echo "Every action must read: owner/repo@<40-hex-sha> # vX.Y.Z" >&2
	echo "Resolve the tag to its commit and write both — never the tag alone, and never a" >&2
	echo "new SHA beside the old version comment." >&2
	exit 1
fi

echo "check-pins: $checked action reference(s) pinned by SHA in ${#files[@]} workflow file(s)"
