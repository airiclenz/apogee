package tools

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestShellCommandMarkerOnTheRealShellTools pins the declaration the shell write view rests on
// (apogee-t74): the registry's REAL `terminal` and `console_open` tools — built as the composition
// root builds them, not a test double — are domain.ShellCommandTool, and each names `command` as
// its one shell command line. internal/security's `write-git-control-plane` rule judges what a
// command WRITES through exactly this declaration; a tool that lost it would silently fall back to
// the text floor, where `ls -la .git/hooks` reads as a write.
func TestShellCommandMarkerOnTheRealShellTools(t *testing.T) {
	t.Parallel()

	// console_open is default-off (the Console family, ADR 0059), so the roster has to lift it.
	roster := DefaultToolsWithHost(t.TempDir(), HostTools{
		Asker: stubAsker{}, Presenter: stubPresenter{}, SkillLookup: &stubLookup{},
		Enabled: []string{"console_open"},
	})
	byName := make(map[string]domain.Tool, len(roster))
	for _, tool := range roster {
		byName[tool.Name()] = tool
	}

	for _, name := range []string{"terminal", "console_open"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tool, ok := byName[name]
			if !ok {
				t.Fatalf("the roster does not carry %q", name)
			}
			if _, ok := tool.(domain.ShellCommandTool); !ok {
				t.Fatalf("%T does not implement domain.ShellCommandTool", tool)
			}
			keys := domain.ShellCommandArgKeys(tool)
			if len(keys) != 1 || keys[0] != "command" {
				t.Errorf("ShellCommandArgKeys(%s) = %q, want [command]", name, keys)
			}
		})
	}
}
