#!/usr/bin/env bash
# release-winget.sh — submit a PUBLISHED release's winget manifest update with komac.
#
# Runs `komac update AiricLenz.Apogee` for $VERSION with the two Windows archives' release URLs
# and `--submit`, which opens the manifest pull request against microsoft/winget-pkgs. It only
# submits: the pull request merges later, after winget's validation and review, so the release
# reaches `winget upgrade` some time after this script returns. Run it once the release is
# published — komac downloads both archives to hash them — and after the Scoop step;
# `/cut-release` calls it.
#
# Usage:
#   make release-winget VERSION=v0.25.0          # or: VERSION=v0.25.0 scripts/release-winget.sh
#   make release-winget                          # takes the version from the VERSION file
#   DRY_RUN=1 scripts/release-winget.sh          # print the komac command (token redacted); run nothing
#
# Environment:
#   REPO          the release repository (default airiclenz/apogee)
#   GITHUB_TOKEN  the token komac opens the pull request with; `gh auth token` when unset
#   DRY_RUN       any non-empty value prints the command to stdout and stops
#
# Needs: komac (https://github.com/russellbanks/Komac), and gh when GITHUB_TOKEN is unset.
# The token reaches komac through its environment, never its argv, so no process listing shows
# it. DRY_RUN looks up neither komac nor the token, so it runs where neither is installed.
# Progress banners go to stderr, so a dry run's stdout is the command alone.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

REPO="${REPO:-airiclenz/apogee}"
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

PACKAGE_ID="AiricLenz.Apogee"
RELEASE_BASE="https://github.com/$REPO/releases/download/$VERSION"

step() { printf '\n==> %s\n' "$1" >&2; }
die() {
	printf 'release-winget: %s\n' "$1" >&2
	exit 1
}

# archive_url prints the release URL of the Windows archive for one `make dist` arch.
archive_url() { printf '%s/apogee_%s_windows_%s.zip' "$RELEASE_BASE" "$BARE" "$1"; }

komac_cmd=(
	komac update "$PACKAGE_ID"
	--version "$BARE"
	--urls "$(archive_url amd64)" "$(archive_url arm64)"
	--submit
)

step "release-winget: $REPO $VERSION -> $PACKAGE_ID"

if [ -n "$DRY_RUN" ]; then
	step "DRY_RUN: the komac command, not run"
	printf 'GITHUB_TOKEN=<redacted> %s\n' "${komac_cmd[*]}"
	exit 0
fi

command -v komac >/dev/null 2>&1 ||
	die "komac is not installed — install it with \`cargo install --locked komac\`, \`brew install komac\` or \`winget install komac\` (https://github.com/russellbanks/Komac)"

token="${GITHUB_TOKEN:-}"
if [ -z "$token" ] && command -v gh >/dev/null 2>&1; then
	token="$(gh auth token 2>/dev/null || true)"
fi
[ -n "$token" ] || die "no GitHub token — set GITHUB_TOKEN or sign in with \`gh auth login\`"

step "submit the $PACKAGE_ID $BARE manifest update"
GITHUB_TOKEN="$token" "${komac_cmd[@]}"

printf '\nrelease-winget: %s submitted — the pull request merges later in microsoft/winget-pkgs, after its validation and review\n' "$VERSION" >&2
