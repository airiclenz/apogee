package update

import "strings"

// Method is how the running apogee binary was installed. It decides the upgrade command the boot
// notice names and whether `apogee update` may replace the binary itself (only an [Archive]
// install is apogee's own to replace; every other method belongs to its package manager or to
// the user's toolchain).
type Method int

const (
	// Homebrew is a binary installed by the airiclenz/tap formula, under a Homebrew Cellar.
	Homebrew Method = iota
	// Scoop is a binary installed from the airiclenz/scoop-bucket manifest, per-user or global.
	Scoop
	// Winget is a binary installed from the AiricLenz.Apogee winget package, user or machine scope.
	Winget
	// GoInstall is a binary built by `go install …@<version>` from the module proxy, which carries
	// no VCS stamp.
	GoInstall
	// Source is a binary built from a git checkout by `make build`/`make install` or `go build`.
	Source
	// Archive is a release binary unpacked from a `make dist` archive by hand.
	Archive
)

// The path segments that identify a package manager's install tree. Each is matched against the
// lower-cased, forward-slash form of the executable path, so it is spelled that way here.
const (
	// homebrewSegment is the formula's keg directory; Apple Silicon (/opt/homebrew), Intel
	// (/usr/local) and Linuxbrew (/home/linuxbrew/.linuxbrew) Cellars all contain it.
	homebrewSegment = "/cellar/apogee/"
	// scoopAppsSegment is the app directory below a Scoop root. Matching it anywhere covers the
	// per-user root (~/scoop) and the global one (C:\ProgramData\scoop) with no home lookup.
	scoopAppsSegment = "/apps/apogee/"
	// scoopRootSegment is the directory name both default Scoop roots end in.
	scoopRootSegment = "/scoop"
	// wingetSegment is the package directory prefix winget installs portable apps under. It carries
	// no `microsoft/` prefix, so the user-scope tree (%LOCALAPPDATA%\Microsoft\WinGet\Packages) and
	// the machine-scope one (C:\Program Files\WinGet\Packages) both match.
	wingetSegment = "/winget/packages/airiclenz.apogee_"
)

// The upgrade command each Method names — the one the update notice shows and `apogee update`
// prints when it refuses a managed install.
const (
	homebrewUpgradeCommand  = "brew upgrade apogee"
	scoopUpgradeCommand     = "scoop update apogee"
	wingetUpgradeCommand    = "winget upgrade AiricLenz.Apogee"
	goInstallUpgradeCommand = "go install github.com/airiclenz/apogee/cmd/apogee@latest"
	sourceUpgradeCommand    = "git pull && make install"
	archiveUpgradeCommand   = "apogee update"
)

// Inputs is everything [Detect] reads about the running binary, gathered by the caller so Detect
// stays pure: no filesystem, environment or build-info access of its own.
type Inputs struct {
	// ExePath is the running executable's path with symlinks resolved, so a Homebrew or Scoop
	// shim resolves to the real install tree. Either path separator is accepted.
	ExePath string
	// DistBuild reports whether the binary carries the `make dist` release stamp (apogee.DistBuild).
	DistBuild bool
	// HasVCS reports whether the binary's build info carries a `vcs.revision` setting, which a
	// build from a git checkout has and a `go install` from the module proxy lacks.
	HasVCS bool
	// ScoopRoot is the SCOOP environment variable — a custom per-user Scoop root — or empty.
	ScoopRoot string
}

// Detect classifies how the binary in inputs was installed. The executable path is checked first
// — a Homebrew Cellar, then a Scoop apps directory, then a winget package directory, first match
// wins — and only a path no package manager owns falls through to the build stamps: no VCS
// revision is a `go install`, a VCS revision without the release stamp is a source build, and a
// stamped release binary is a hand-unpacked [Archive].
func Detect(inputs Inputs) Method {
	exePath := normalizePath(inputs.ExePath)
	switch {
	case strings.Contains(exePath, homebrewSegment):
		return Homebrew
	case isScoopPath(exePath, inputs.ScoopRoot):
		return Scoop
	case strings.Contains(exePath, wingetSegment):
		return Winget
	case !inputs.HasVCS:
		return GoInstall
	case !inputs.DistBuild:
		return Source
	default:
		return Archive
	}
}

// UpgradeCommand returns the command that upgrades a binary installed by m — the package
// manager's own upgrade, the `go install` or source rebuild, or `apogee update` for an Archive.
// It is empty for a value that is not one of the declared Methods.
func (m Method) UpgradeCommand() string {
	switch m {
	case Homebrew:
		return homebrewUpgradeCommand
	case Scoop:
		return scoopUpgradeCommand
	case Winget:
		return wingetUpgradeCommand
	case GoInstall:
		return goInstallUpgradeCommand
	case Source:
		return sourceUpgradeCommand
	case Archive:
		return archiveUpgradeCommand
	default:
		return ""
	}
}

// isScoopPath reports whether exePath lies in a Scoop apogee app directory: below the custom
// scoopRoot when one is set, or below any directory named scoop otherwise.
func isScoopPath(exePath, scoopRoot string) bool {
	if root := strings.TrimSuffix(normalizePath(scoopRoot), "/"); root != "" &&
		strings.HasPrefix(exePath, root+scoopAppsSegment) {
		return true
	}
	return strings.Contains(exePath, scoopRootSegment+scoopAppsSegment)
}

// normalizePath lower-cases path and turns every backslash into a forward slash, so a Windows
// path matches the same segments on any host OS (filepath.ToSlash only converts the running OS's
// own separator).
func normalizePath(path string) string {
	return strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
}
