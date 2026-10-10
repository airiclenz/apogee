#!/usr/bin/env bash
# release-scoop.sh — publish a PUBLISHED release's Scoop manifest to the bucket repository.
#
# Writes bucket/apogee.json for $VERSION into a fresh clone of $BUCKET_REPO, commits it as
# `apogee <bare version>` and pushes. The two Windows archives' hashes come from the
# release's own SHA256SUMS, so run this only once the release is published — it is the
# Scoop counterpart of the Homebrew tap step, and `/cut-release` calls it after that step.
#
# Usage:
#   make release-scoop VERSION=v0.25.0           # or: VERSION=v0.25.0 scripts/release-scoop.sh
#   make release-scoop                           # takes the version from the VERSION file
#   DRY_RUN=1 scripts/release-scoop.sh           # print the manifest; clone, commit and push nothing
#
# Environment:
#   REPO         the release repository (default airiclenz/apogee)
#   BUCKET_REPO  the Scoop bucket repository (default airiclenz/scoop-bucket)
#   BUCKET_URL   clone this URL with plain git instead of `gh repo clone $BUCKET_REPO`
#   SUMS_FILE    read the hashes from this local SHA256SUMS instead of downloading the release's
#   DRY_RUN      any non-empty value prints the manifest to stdout and stops before any clone
#
# Needs: curl (unless SUMS_FILE is set) and awk; git and, without BUCKET_URL, gh for the
# publish half. DRY_RUN with SUMS_FILE touches no network and runs neither gh nor git.
# Progress banners go to stderr, so a dry run's stdout is the manifest alone.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

REPO="${REPO:-airiclenz/apogee}"
BUCKET_REPO="${BUCKET_REPO:-airiclenz/scoop-bucket}"
BUCKET_URL="${BUCKET_URL:-}"
SUMS_FILE="${SUMS_FILE:-}"
DRY_RUN="${DRY_RUN:-}"
VERSION="${VERSION:-}"
if [ -z "$VERSION" ]; then
	VERSION="$(tr -d ' \t\r\n' < "$ROOT/VERSION")"
fi
case "$VERSION" in
v*) ;;
*) VERSION="v$VERSION" ;;
esac
BARE="${VERSION#v}"

# Scoop's `checkver: "github"` resolves the latest release from the homepage, so it must be
# the release repository's GitHub page.
HOMEPAGE="https://github.com/$REPO"
DESCRIPTION="A terminal AI coding agent built for smaller, locally hosted LLMs"
LICENSE_ID="MIT"
MANIFEST_PATH="bucket/apogee.json"

# Scoop's architecture keys, and the `make dist` arch each one installs.
SCOOP_ARCHES="64bit:amd64 arm64:arm64"

step() { printf '\n==> %s\n' "$1" >&2; }
die() {
	printf 'release-scoop: %s\n' "$1" >&2
	exit 1
}

# archive_name prints the release asset name of the Windows archive for one `make dist` arch.
archive_name() { printf 'apogee_%s_windows_%s.zip' "$BARE" "$1"; }

# sums_hash prints the SHA-256 the SHA256SUMS file $1 lists for asset $2, or nothing. Both the
# text (`hash  name`) and binary (`hash *name`) line forms are accepted.
sums_hash() {
	awk -v name="$2" '$2 == name || $2 == "*" name { print tolower($1); exit }' "$1"
}

# fetch_sums leaves the release's SHA256SUMS at $1: copied from SUMS_FILE when set, else
# downloaded from the published release.
fetch_sums() {
	if [ -n "$SUMS_FILE" ]; then
		[ -f "$SUMS_FILE" ] || die "SUMS_FILE $SUMS_FILE does not exist"
		cp "$SUMS_FILE" "$1"
		return
	fi
	local url="https://github.com/$REPO/releases/download/$VERSION/SHA256SUMS"
	curl -fsSL "$url" -o "$1" || die "cannot download $url — is $VERSION published?"
}

# arch_block prints one `architecture` entry: the Scoop key $1 for `make dist` arch $2, whose
# archive hashes to $3.
arch_block() {
	local name
	name="$(archive_name "$2")"
	printf '        "%s": {\n' "$1"
	printf '            "url": "%s/releases/download/%s/%s",\n' "$HOMEPAGE" "$VERSION" "$name"
	printf '            "hash": "%s",\n' "$3"
	printf '            "extract_dir": "%s"\n' "${name%.zip}"
	printf '        }'
}

# autoupdate_block prints one `autoupdate.architecture` entry. `$version` and `$baseurl` are
# Scoop's own placeholders, expanded by its autoupdater — never by this shell.
autoupdate_block() {
	printf '            "%s": {\n' "$1"
	printf '                "url": "%s/releases/download/v$version/apogee_$version_windows_%s.zip",\n' "$HOMEPAGE" "$2"
	printf '                "hash": {\n'
	printf '                    "url": "$baseurl/SHA256SUMS"\n'
	printf '                },\n'
	printf '                "extract_dir": "apogee_$version_windows_%s"\n' "$2"
	printf '            }'
}

# write_manifest prints the whole manifest for $VERSION, reading the hashes from SHA256SUMS $1.
write_manifest() {
	local sums="$1" pair key arch hash separator=""
	printf '{\n'
	printf '    "version": "%s",\n' "$BARE"
	printf '    "description": "%s",\n' "$DESCRIPTION"
	printf '    "homepage": "%s",\n' "$HOMEPAGE"
	printf '    "license": "%s",\n' "$LICENSE_ID"
	printf '    "architecture": {\n'
	for pair in $SCOOP_ARCHES; do
		key="${pair%%:*}"
		arch="${pair#*:}"
		hash="$(sums_hash "$sums" "$(archive_name "$arch")")"
		[ -n "$hash" ] || die "SHA256SUMS lists no hash for $(archive_name "$arch")"
		case "$hash" in
		*[!0-9a-f]*) die "SHA256SUMS lists a malformed hash for $(archive_name "$arch")" ;;
		esac
		[ "${#hash}" -eq 64 ] || die "SHA256SUMS lists a malformed hash for $(archive_name "$arch")"
		printf '%s' "$separator"
		arch_block "$key" "$arch" "$hash"
		separator=$',\n'
	done
	printf '\n    },\n'
	printf '    "bin": "apogee.exe",\n'
	printf '    "checkver": "github",\n'
	printf '    "autoupdate": {\n'
	printf '        "architecture": {\n'
	separator=""
	for pair in $SCOOP_ARCHES; do
		printf '%s' "$separator"
		autoupdate_block "${pair%%:*}" "${pair#*:}"
		separator=$',\n'
	done
	printf '\n        }\n'
	printf '    }\n'
	printf '}\n'
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

step "release-scoop: $REPO $VERSION -> $BUCKET_REPO"

step "read the Windows archive hashes from SHA256SUMS"
fetch_sums "$work/SHA256SUMS"
write_manifest "$work/SHA256SUMS" > "$work/apogee.json"

if [ -n "$DRY_RUN" ]; then
	step "DRY_RUN: the manifest, unpublished"
	cat "$work/apogee.json"
	exit 0
fi

step "clone $BUCKET_REPO"
if [ -n "$BUCKET_URL" ]; then
	git clone --quiet "$BUCKET_URL" "$work/bucket"
elif command -v gh >/dev/null 2>&1; then
	gh repo clone "$BUCKET_REPO" "$work/bucket" -- --quiet
else
	git clone --quiet "https://github.com/$BUCKET_REPO.git" "$work/bucket"
fi

step "write $MANIFEST_PATH, commit and push"
mkdir -p "$work/bucket/$(dirname "$MANIFEST_PATH")"
cp "$work/apogee.json" "$work/bucket/$MANIFEST_PATH"
git -C "$work/bucket" add "$MANIFEST_PATH"
if git -C "$work/bucket" diff --cached --quiet; then
	printf '    %s already describes %s — nothing to publish\n' "$MANIFEST_PATH" "$VERSION" >&2
	exit 0
fi
git -C "$work/bucket" commit --quiet -m "apogee $BARE"
git -C "$work/bucket" push --quiet origin HEAD
printf '\nrelease-scoop: %s published to %s\n' "$VERSION" "$BUCKET_REPO" >&2
