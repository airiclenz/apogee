package main

// The boot update check (ADR 0097): the one [tui.UpdateHost] this binary hands the interactive
// TUI, and the gate that decides whether it hands one over at all.
//
// It is wired by (*rootWiring).options() and nowhere else, so headless, a daemon Firing, probe and
// undo never construct one and never send the request. The renderer stays wire-silent (ADR 0031):
// every network byte goes through internal/update, reached from here.

import (
	"context"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/tui"
	"github.com/airiclenz/apogee/internal/update"
)

// devBaseVersion is what apogee.BaseVersion answers for a build whose VERSION file is empty: not
// a release, so there is nothing to compare a published tag against.
const devBaseVersion = "dev"

// dirtyVersionSuffix ends apogee.Version for a binary built from a modified working tree — a
// developer's own build, which a published release must never be advertised over.
const dirtyVersionSuffix = ".dirty"

// vcsRevisionSetting is the build-info key Go stamps with the commit a binary was built from.
const vcsRevisionSetting = "vcs.revision"

// scoopRootEnv is the variable a custom per-user Scoop root is configured through.
const scoopRootEnv = "SCOOP"

// updateBaseURL is the repository the boot check asks for its latest release. It is a package var
// so a serial test can point it at a stand-in server; production never reassigns it.
var updateBaseURL = update.DefaultBaseURL

// installInputs is the one seam the install-method detection inputs are read through — the
// executable path, the `make dist` stamp, the VCS stamp and the Scoop root — so a test supplies a
// Method without the real build stamps. Production never reassigns it.
var installInputs = readInstallInputs

var _ tui.UpdateHost = updateHost{}

// updateHost answers the renderer's one boot question: is a release newer than this binary
// published, and what does this install run to get it?
type updateHost struct {
	// current is the running binary's release version (apogee.BaseVersion).
	current string
	// baseURL is the repository the latest release is looked up under.
	baseURL string
	// inputs reads what install-method detection needs, called only once a newer release exists.
	inputs func() update.Inputs
}

// CheckForUpdate looks up the latest published release and, when it is newer than the running
// binary, names it with the upgrade command for this install. Every failure — offline, a timeout,
// an unexpected answer, a release that is not newer — is ok=false: the notice is a courtesy and
// never fails loudly.
func (h updateHost) CheckForUpdate(ctx context.Context) (latest, command string, ok bool) {
	latest, err := update.Latest(ctx, update.NewClient(h.baseURL))
	if err != nil || !update.Newer(h.current, latest) {
		return "", "", false
	}
	command = update.Detect(h.inputs()).UpgradeCommand()
	if command == "" {
		return "", "", false
	}
	return latest, command, true
}

// updateHostFor returns the update host the interactive TUI is handed, or nil — no check at all —
// when updateCheckEnabled refuses. updateCheck is the resolved `update-check:` key, which
// APOGEE_NO_UPDATE_CHECK has already forced off (config.ApplyConfig).
func updateHostFor(updateCheck bool, baseVersion, fullVersion string) tui.UpdateHost {
	if !updateCheckEnabled(updateCheck, baseVersion, fullVersion) {
		return nil
	}
	return updateHost{current: baseVersion, baseURL: updateBaseURL, inputs: installInputs}
}

// updateCheckEnabled is the enable gate: the key is on, and the binary is a release — a VERSION
// to compare against (not "dev") and a clean build (its full version carries no ".dirty").
func updateCheckEnabled(updateCheck bool, baseVersion, fullVersion string) bool {
	return updateCheck && baseVersion != devBaseVersion && !strings.HasSuffix(fullVersion, dirtyVersionSuffix)
}

// readInstallInputs gathers the detection inputs from the running process: the executable with
// its symlinks resolved (a Homebrew or Scoop shim leads to the real install tree; an unresolvable
// one is used as it stands), the `make dist` stamp, whether the build info carries a VCS revision,
// and the Scoop root.
func readInstallInputs() update.Inputs {
	inputs := update.Inputs{DistBuild: apogee.DistBuild(), ScoopRoot: os.Getenv(scoopRootEnv)}
	if exePath, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
			exePath = resolved
		}
		inputs.ExePath = exePath
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == vcsRevisionSetting && setting.Value != "" {
				inputs.HasVCS = true
				break
			}
		}
	}
	return inputs
}
