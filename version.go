package apogee

import (
	_ "embed"
	"runtime/debug"
	"strings"
)

// versionFile is the verbatim contents of the repository's top-level VERSION file,
// embedded into the binary at build time. It is the project's SINGLE SOURCE OF TRUTH
// for the release version: every build path — `make build`, `go build`, `go run`,
// `go install` — carries this exact file, so there is no -ldflags override of the version
// NUMBER that could drift from it. (A release build does inject one optional piece of build
// provenance — the commit-count build number — via -ldflags; see buildCount below. That is
// provenance only and is never the version number.) To cut a release, edit VERSION and nothing else.
//
// It lives in the root facade because go:embed cannot reach a parent directory and the
// ADR-0010 invariant forbids internal/* from importing the root, so this is the only
// package that can both embed a root-level file and be imported by cmd/apogee.
//
//go:embed VERSION
var versionFile string

// commitShortLen is how many hex chars of the VCS revision the build-provenance suffix
// carries — long enough to stay collision-free in an active repo, short enough to read.
const commitShortLen = 12

// buildCount is the build number — the repository's commit count (`git rev-list --count HEAD`)
// at build time. It is the ONE value the version string cannot derive at runtime: Go's VCS stamp
// (debug.ReadBuildInfo) carries the revision and dirty flag but no count, so a release build
// injects it via `-ldflags -X github.com/airiclenz/apogee.buildCount=<n>` (see the Makefile). A
// build that does not pass it (a bare `go build`/`go run`) leaves it empty and the suffix simply
// omits the number — the version NUMBER itself is unaffected, always the embedded VERSION file.
// This is build provenance, never a second source for the version.
var buildCount string

// Version returns the full version string: the release version from the top-level VERSION
// file (the single source of truth), optionally followed by build provenance — a commit-count
// build number, the short commit id, and a ".dirty" marker when the working tree was modified
// at build time. The commit id and dirty flag come from the VCS stamp Go embeds for a repository
// build (debug.ReadBuildInfo); the build number is the one field the runtime cannot observe, so
// a release build injects it via -ldflags (see buildCount). The provenance is build metadata,
// never a second source for the version number.
//
// Examples: "1.7.1+436.g28b6f838e6e1" (make build), "1.7.1+436.g28b6f838e6e1.dirty" (dirty
// tree), "1.7.1+g28b6f838e6e1" (a bare `go build`, no build number), or bare "1.7.1" when the
// binary was built outside a VCS tree (or with -buildvcs=false). The CLI --version flag, the
// in-TUI /version command, and the start-up box all display this one value.
func Version() string {
	base := BaseVersion()
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return base
	}
	if meta := buildMetadata(buildCount, info.Settings); meta != "" {
		return base + "+" + meta
	}
	return base
}

// BaseVersion is the release version alone: the trimmed VERSION file (or "dev" if it is empty
// or whitespace-only), with NONE of the "+<count>.g<rev>[.dirty]" build provenance Version()
// appends. It is what the start-up box displays; --version and /version show Version().
func BaseVersion() string {
	if v := strings.TrimSpace(versionFile); v != "" {
		return v
	}
	return "dev"
}

// buildMetadata composes the build-provenance suffix. The commit id and dirty flag come from the
// VCS settings Go stamps for a repository build (vcs.revision, vcs.modified); count is the
// commit-count build number a release build injects via -ldflags (empty on a bare `go build`).
// It returns e.g. "436.g28b6f838e6e1", "436.g28b6f838e6e1.dirty", "g28b6f838e6e1" when no count
// was injected, or "" when no revision was stamped (built outside a repo, or with
// -buildvcs=false). The build number decorates the git anchor, so it is dropped when there is no
// revision. The leading "g" mirrors `git describe`.
func buildMetadata(count string, settings []debug.BuildSetting) string {
	var revision string
	var modified bool
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if revision == "" {
		return ""
	}
	if len(revision) > commitShortLen {
		revision = revision[:commitShortLen]
	}
	meta := "g" + revision
	if count = strings.TrimSpace(count); count != "" {
		meta = count + "." + meta
	}
	if modified {
		meta += ".dirty"
	}
	return meta
}
