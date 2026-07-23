// Package version reports the apogee build version from a single source, so the
// CLI's --version flag, the in-TUI /version command, and the start-up box all
// agree. The value is resolved in one place (String) and threaded to the TUI
// through Options.Version; nothing else derives its own version.
package version

import "runtime/debug"

// Version is the build version, injected at release-build time via
//
//	-ldflags "-X github.com/airiclenz/apogee/internal/version.Version=v1.7.0"
//
// The Makefile passes `git describe --tags --always --dirty`. It is empty in a
// plain `go build`/`go run`; String then derives an honest value from the build
// info the toolchain embeds.
var Version = ""

// String returns the build version. It prefers the ldflags-injected Version; then
// the module version embedded by `go install pkg@tag` (info.Main.Version, when set
// and not the "(devel)" placeholder); then the short VCS revision embedded by
// `go build` in a git checkout (7 hex chars, suffixed "+dirty" on a modified tree);
// and falls back to "dev" when none of those is available.
func String() string {
	if Version != "" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var revision string
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if revision != "" {
		if len(revision) > 7 {
			revision = revision[:7]
		}
		if modified {
			revision += "+dirty"
		}
		return revision
	}
	return "dev"
}
