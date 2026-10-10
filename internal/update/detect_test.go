package update

import (
	"os"
	"strings"
	"testing"
)

// goModPath is the module file the GoInstall command's package path must agree with.
const goModPath = "../../go.mod"

func TestDetect_ExePathAndStamps_ReturnsInstallMethod(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		inputs Inputs
		want   Method
	}{
		{
			name:   "Apple Silicon Homebrew Cellar",
			inputs: Inputs{ExePath: "/opt/homebrew/Cellar/apogee/0.24.11/bin/apogee", HasVCS: true, DistBuild: true},
			want:   Homebrew,
		},
		{
			name:   "Linuxbrew Cellar",
			inputs: Inputs{ExePath: "/home/linuxbrew/.linuxbrew/Cellar/apogee/0.24.11/bin/apogee", HasVCS: true, DistBuild: true},
			want:   Homebrew,
		},
		{
			name:   "per-user Scoop root with no SCOOP set",
			inputs: Inputs{ExePath: `C:\Users\x\scoop\apps\apogee\current\apogee.exe`, HasVCS: true, DistBuild: true},
			want:   Scoop,
		},
		{
			name:   "global Scoop root",
			inputs: Inputs{ExePath: `C:\ProgramData\scoop\apps\apogee\current\apogee.exe`, HasVCS: true, DistBuild: true},
			want:   Scoop,
		},
		{
			name: "custom SCOOP root",
			inputs: Inputs{
				ExePath:   `D:\Tools\Pkgs\apps\apogee\current\apogee.exe`,
				ScoopRoot: `D:\Tools\Pkgs\`,
				HasVCS:    true,
				DistBuild: true,
			},
			want: Scoop,
		},
		{
			name: "user-scope winget package",
			inputs: Inputs{
				ExePath:   `C:\Users\x\AppData\Local\Microsoft\WinGet\Packages\AiricLenz.Apogee_Microsoft.Winget.Source_8wekyb3d8bbwe\apogee.exe`,
				HasVCS:    true,
				DistBuild: true,
			},
			want: Winget,
		},
		{
			name: "machine-scope winget package",
			inputs: Inputs{
				ExePath:   `C:\Program Files\WinGet\Packages\AiricLenz.Apogee_Microsoft.Winget.Source_8wekyb3d8bbwe\apogee.exe`,
				HasVCS:    true,
				DistBuild: true,
			},
			want: Winget,
		},
		{
			name:   "module-proxy go install carries no VCS revision",
			inputs: Inputs{ExePath: "/home/x/go/bin/apogee"},
			want:   GoInstall,
		},
		{
			name:   "checkout build without the release stamp",
			inputs: Inputs{ExePath: "/home/x/go/bin/apogee", HasVCS: true},
			want:   Source,
		},
		{
			name:   "hand-unpacked release archive",
			inputs: Inputs{ExePath: "/usr/local/bin/apogee", HasVCS: true, DistBuild: true},
			want:   Archive,
		},
		{
			name:   "hand-unpacked Windows release archive",
			inputs: Inputs{ExePath: `C:\Tools\apogee\apogee.exe`, HasVCS: true, DistBuild: true},
			want:   Archive,
		},
		{
			name:   "an apps/apogee directory outside any Scoop root",
			inputs: Inputs{ExePath: "/srv/apps/apogee/apogee", HasVCS: true, DistBuild: true},
			want:   Archive,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Detect(tc.inputs)

			if got != tc.want {
				t.Errorf("Detect(%+v) = %d; want %d", tc.inputs, got, tc.want)
			}
		})
	}
}

func TestMethodUpgradeCommand_EachMethod_ReturnsChannelCommand(t *testing.T) {
	t.Parallel()
	cases := []struct {
		method Method
		want   string
	}{
		{method: Homebrew, want: "brew upgrade apogee"},
		{method: Scoop, want: "scoop update apogee"},
		{method: Winget, want: "winget upgrade AiricLenz.Apogee"},
		{method: GoInstall, want: "go install github.com/airiclenz/apogee/cmd/apogee@latest"},
		{method: Source, want: "git pull && make install"},
		{method: Archive, want: "apogee update"},
		{method: Archive + 1, want: ""},
	}
	for _, tc := range cases {
		if got := tc.method.UpgradeCommand(); got != tc.want {
			t.Errorf("Method(%d).UpgradeCommand() = %q; want %q", tc.method, got, tc.want)
		}
	}
}

func TestMethodUpgradeCommand_GoInstall_NamesTheModulesCommandPackage(t *testing.T) {
	t.Parallel()
	modulePath := readModulePath(t)

	got := GoInstall.UpgradeCommand()

	if want := "go install " + modulePath + "/cmd/apogee@latest"; got != want {
		t.Errorf("GoInstall.UpgradeCommand() = %q; want %q (module path from %s)", got, want, goModPath)
	}
}

// readModulePath returns the module path declared on go.mod's `module` line.
func readModulePath(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("read %s: %v", goModPath, err)
	}
	for line := range strings.Lines(string(contents)) {
		if modulePath, isModuleLine := strings.CutPrefix(strings.TrimSpace(line), "module "); isModuleLine {
			return strings.TrimSpace(modulePath)
		}
	}
	t.Fatalf("%s declares no module line", goModPath)
	return ""
}
