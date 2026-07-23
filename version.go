package apogee

import (
	_ "embed"
	"runtime/debug"
	"strings"
)

// versionFile is the verbatim contents of the repository's top-level VERSION file,
// embedded into the binary at build time. It is the project's SINGLE SOURCE OF TRUTH
// for the release version: every build path — `make build`, `go build`, `go run`,
// `go install` — carries this exact file, so there is no -ldflags override that could
// drift from it. To cut a release, edit VERSION and nothing else.
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

// Version returns the full version string: the release version from the top-level VERSION
// file (the single source of truth), optionally followed by build provenance derived at
// runtime from the VCS stamp Go embeds for a repository build — the short commit id and a
// ".dirty" marker when the working tree was modified at build time. No -ldflags are used;
// the provenance is observed build metadata, never a second source for the version number.
//
// Examples: "1.7.1+g28b6f838e6e1", "1.7.1+g28b6f838e6e1.dirty", or bare "1.7.1" when the
// binary was built outside a VCS tree (or with -buildvcs=false). The CLI --version flag,
// the in-TUI /version command, and the start-up box all display this one value.
func Version() string {
	base := baseVersion()
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return base
	}
	if meta := buildMetadata(info.Settings); meta != "" {
		return base + "+" + meta
	}
	return base
}

// baseVersion is the release version: the trimmed VERSION file, or "dev" if it is empty or
// whitespace-only so a broken checkout still reports something. The file is the only real source.
func baseVersion() string {
	if v := strings.TrimSpace(versionFile); v != "" {
		return v
	}
	return "dev"
}

// buildMetadata composes the build-provenance suffix from the VCS settings Go stamps for a
// repository build (vcs.revision, vcs.modified) — no -ldflags required. It returns e.g.
// "g28b6f838e6e1" or "g28b6f838e6e1.dirty", or "" when no revision was stamped (the binary
// was built outside a repo, or with -buildvcs=false). The leading "g" mirrors `git describe`.
func buildMetadata(settings []debug.BuildSetting) string {
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
	if modified {
		meta += ".dirty"
	}
	return meta
}
