#!/usr/bin/env bash
# release-smoke.sh — prove a PUBLISHED release is usable, from the outside.
#
# This is the post-publish half of checklist T-21. `make check` proves the tree builds; this
# proves what the tree BECAME: the tag is annotated and remote, `make dist` still packs the
# six archives, the published assets download and their SHA256SUMS verify, every published
# binary carries the tagged commit's build stamp, the host's own archive unpacks to a
# binary that reports the released version, and — where Homebrew is
# installed — `brew upgrade apogee` moves this machine onto it. None of that can be asserted
# before a release exists, which is why it is a target of its own and never part of `make check`.
#
# Usage:
#   make release-smoke VERSION=v0.18.0     # or: VERSION=v0.18.0 scripts/release-smoke.sh
#   make release-smoke                     # takes the version from the VERSION file
#
# Needs: curl, tar, a sha256 tool, a Go toolchain (for the `make dist` pre-check and for
# reading the published binaries' build stamp), and `unzip` for the two Windows archives'
# stamp check.
# Uses `gh` and `git ls-remote` for the tag pre-checks when they are available, and `brew`
# for the upgrade check when it is installed; each of those is skipped, loudly, when absent.

set -euo pipefail

cd "$(dirname "$0")/.."

REPO="${REPO:-airiclenz/apogee}"
VERSION="${VERSION:-}"
if [ -z "$VERSION" ]; then
	VERSION="$(tr -d ' \t\r\n' < VERSION)"
fi
case "$VERSION" in
v*) ;;
*) VERSION="v$VERSION" ;;
esac
BARE="${VERSION#v}"

# The six archives `make dist` packs, and the one this machine can actually run.
TARGETS="linux_amd64 linux_arm64 darwin_amd64 darwin_arm64 windows_amd64 windows_arm64"

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then sha256sum "$@"; else shasum -a 256 "$@"; fi
}

sha256check() {
	if command -v sha256sum >/dev/null 2>&1; then sha256sum -c "$@"; else shasum -a 256 -c "$@"; fi
}

# The build stamp `-buildvcs` embeds in every `make dist` binary: which commit it was built
# from, and whether that tree was dirty. `go version -m` reads it with the host toolchain for
# any GOOS/GOARCH, and it — not the archive bytes, whose mtimes never re-pack alike — is what
# ties a published asset to the tag. Both readers print nothing when the stamp is absent.
binary_revision() {
	go version -m "$1" 2>/dev/null |
		awk '$1 == "build" && $2 ~ /^vcs\.revision=/ { sub(/^vcs\.revision=/, "", $2); print $2; exit }' || true
}

binary_modified() {
	go version -m "$1" 2>/dev/null |
		awk '$1 == "build" && $2 ~ /^vcs\.modified=/ { sub(/^vcs\.modified=/, "", $2); print $2; exit }' || true
}

step() { printf '\n==> %s\n' "$1"; }
skip() { printf '    SKIP: %s\n' "$1"; }

failures=0
fail() {
	printf '    FAIL: %s\n' "$1" >&2
	failures=$((failures + 1))
}

printf 'release-smoke: %s %s\n' "$REPO" "$VERSION"

# ---------------------------------------------------------------------------------------
# Pre-check A (T-21 step 5) — the tag exists remotely, is ANNOTATED, and nothing re-pointed it.
# ---------------------------------------------------------------------------------------
step "the tag $VERSION is remote and annotated"
if command -v git >/dev/null 2>&1 && git rev-parse --git-dir >/dev/null 2>&1; then
	if git ls-remote --tags origin 2>/dev/null | grep -qE "refs/tags/${VERSION}$"; then
		echo "    remote tag present"
	else
		fail "no refs/tags/$VERSION on origin — the release was published without its tag"
	fi
else
	skip "not a git checkout with a remote"
fi
if command -v gh >/dev/null 2>&1; then
	kind="$(gh api "repos/$REPO/git/ref/tags/$VERSION" --jq .object.type 2>/dev/null || true)"
	case "$kind" in
	tag) echo "    annotated" ;;
	commit) fail "$VERSION is a LIGHTWEIGHT ref — the tag job is supposed to annotate it" ;;
	*) skip "gh could not read the ref (not authenticated for $REPO?)" ;;
	esac
else
	skip "gh is not installed — cannot tell an annotated tag from a lightweight one"
fi

# ---------------------------------------------------------------------------------------
# Pre-check B (T-21 step 9) — `make dist` still packs six archives that verify, and the
# binary in the host's own archive reports this version.
# ---------------------------------------------------------------------------------------
step "make dist packs the six archives"
if [ "$(tr -d ' \t\r\n' < VERSION)" != "$VERSION" ]; then
	skip "the VERSION file says $(tr -d ' \t\r\n' < VERSION), not $VERSION — dist would name the archives for the file"
else
	make --no-print-directory dist >/dev/null
	built="$(ls dist/*.tar.gz dist/*.zip 2>/dev/null | wc -l | tr -d ' ')"
	if [ "$built" -ne 6 ]; then
		fail "make dist produced $built archives, want 6"
	fi
	(cd dist && sha256check SHA256SUMS >/dev/null) || fail "dist/SHA256SUMS does not verify"
	echo "    6 archives + SHA256SUMS verified"
fi

# ---------------------------------------------------------------------------------------
# The published assets (T-21 step 8's precondition, and the archive path of T-23 step 2).
# ---------------------------------------------------------------------------------------
step "the published assets download and verify"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
base="https://github.com/$REPO/releases/download/$VERSION"

if curl -fsSL "$base/SHA256SUMS" -o "$work/SHA256SUMS"; then
	missing=0
	for t in $TARGETS; do
		name="apogee_${BARE}_${t}"
		case "$t" in
		windows_*) name="$name.zip" ;;
		*) name="$name.tar.gz" ;;
		esac
		if grep -q "$name" "$work/SHA256SUMS"; then
			curl -fsSL "$base/$name" -o "$work/$name" || { fail "asset $name is listed but does not download"; missing=1; }
		else
			fail "asset $name is missing from the release's SHA256SUMS"
			missing=1
		fi
	done
	if [ "$missing" -eq 0 ]; then
		# Verify only what was downloaded, in the directory holding it.
		(cd "$work" && sha256check SHA256SUMS >/dev/null) || fail "a downloaded asset does not match SHA256SUMS"
		echo "    6 assets downloaded, all checksums match"

		# Checksums only prove the assets match the list the release itself published; the
		# build stamp is what ties them to the tree. Archive bytes cannot do it — `make dist`
		# re-`cp`s LICENSE/README with fresh mtimes into every tar/zip, so no two runs hash
		# alike — so read the commit each binary was built from instead.
		want="$(git rev-parse -q --verify "${VERSION}^{}" 2>/dev/null || true)"
		if [ -z "$want" ]; then
			skip "vcs cross-check: tag $VERSION is not in the local clone — git fetch --tags"
		else
			stamped=0
			for t in $TARGETS; do
				name="apogee_${BARE}_${t}"
				case "$t" in
				windows_*) name="$name.zip" ;;
				*) name="$name.tar.gz" ;;
				esac
				# One directory per asset, under stamp/ so it cannot collide with the
				# downloaded archive of the same name (or with the host unpack below).
				into="$work/stamp/$name"
				mkdir -p "$into"
				case "$t" in
				windows_*)
					bin="$into/apogee.exe"
					if ! command -v unzip >/dev/null 2>&1; then
						skip "unzip is not installed — cannot read the build stamp of $name"
						continue
					fi
					if ! unzip -p "$work/$name" "apogee_${BARE}_${t}/apogee.exe" >"$bin" 2>/dev/null; then
						fail "asset $name does not hold apogee_${BARE}_${t}/apogee.exe"
						continue
					fi
					;;
				*)
					bin="$into/apogee"
					if ! tar -xzf "$work/$name" -C "$into" --strip-components=1 "apogee_${BARE}_${t}/apogee" 2>/dev/null; then
						fail "asset $name does not hold apogee_${BARE}_${t}/apogee"
						continue
					fi
					;;
				esac
				rev="$(binary_revision "$bin")"
				if [ -z "$rev" ]; then
					fail "asset $name carries no build stamp — was it built with -buildvcs=false?"
					continue
				fi
				if [ "$rev" != "$want" ]; then
					fail "asset $name was built from $rev, not the tagged commit $want"
					continue
				fi
				# Untracked files flip vcs.modified, and an unstaged plan doc at cut time is
				# routine — say so, never fail on it.
				if [ "$(binary_modified "$bin")" = "true" ]; then
					echo "    warning: asset $name was built from a modified tree"
				fi
				stamped=$((stamped + 1))
			done
			if [ "$stamped" -eq 6 ]; then
				echo "    6 assets carry vcs.revision ${want:0:12} = the tag"
			fi
		fi
	fi
else
	fail "no SHA256SUMS asset on the $VERSION release — is it published?"
fi

# ---------------------------------------------------------------------------------------
# The host's own binary reports the released version.
# ---------------------------------------------------------------------------------------
step "the host's archive unpacks to a binary reporting $VERSION"
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) arch="" ;;
esac
host="apogee_${BARE}_${os}_${arch}.tar.gz"
if [ -z "$arch" ] || [ ! -f "$work/$host" ]; then
	skip "no downloaded archive for this host ($os/$(uname -m))"
else
	tar -xzf "$work/$host" -C "$work"
	got="$("$work/apogee_${BARE}_${os}_${arch}/apogee" --version)"
	echo "    $got"
	case "$got" in
	*"$VERSION"*) ;;
	*) fail "the released binary reports $got, not $VERSION" ;;
	esac
fi

# ---------------------------------------------------------------------------------------
# Homebrew (T-21 step 8) — only where brew is installed; it is the tap's half of the claim.
# ---------------------------------------------------------------------------------------
step "brew upgrade moves this machine onto $VERSION"
if ! command -v brew >/dev/null 2>&1; then
	skip "Homebrew is not installed on this host — the tap half stays a human step"
elif ! brew list --formula apogee >/dev/null 2>&1; then
	skip "apogee is not installed via Homebrew here — nothing to upgrade"
else
	brew update >/dev/null
	brew upgrade apogee
	got="$(brew --prefix)/bin/apogee"
	reported="$("$got" --version)"
	echo "    $reported"
	case "$reported" in
	*"$VERSION"*) ;;
	*) fail "brew still reports $reported after the upgrade — is the tap formula pointing at $VERSION?" ;;
	esac
fi

printf '\n'
if [ "$failures" -ne 0 ]; then
	echo "release-smoke: $failures check(s) FAILED for $VERSION" >&2
	exit 1
fi
echo "release-smoke: $VERSION OK"
