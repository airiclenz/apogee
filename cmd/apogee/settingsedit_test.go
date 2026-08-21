package main

// The `$EDITOR` round trip's root half (ADR 0037 decision 5): which command line the pane is handed
// on the way out, and which keys it is told changed on the way back.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/tui"
)

// The editor ladder has four rungs (ADR 0041 decision 2): the `editor` key, then $VISUAL, then
// $EDITOR, then the platform's own default opener. The key outranks the environment because it is
// the only rung set for apogee, a rung carrying flags is honoured as the command line it is rather
// than looked up as a program with a space in its name, and a blank or whitespace-only rung is no
// answer at all.
func TestEditorArgvFollowsTheLadder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		configured string
		env        map[string]string
		goos       string
		want       []string
	}{
		{name: "the config key outranks VISUAL", configured: "code -w",
			env: map[string]string{"VISUAL": "hx", "EDITOR": "vi"}, goos: "linux", want: []string{"code", "-w"}},
		{name: "VISUAL outranks EDITOR", env: map[string]string{"VISUAL": "hx", "EDITOR": "vi"}, goos: "linux", want: []string{"hx"}},
		{name: "EDITOR when VISUAL is unset", env: map[string]string{"EDITOR": "nvim"}, goos: "darwin", want: []string{"nvim"}},
		{name: "a command with flags", env: map[string]string{"EDITOR": "code -w"}, goos: "linux", want: []string{"code", "-w"}},
		{name: "an empty variable is no answer", env: map[string]string{"VISUAL": "   ", "EDITOR": "nano"}, goos: "linux", want: []string{"nano"}},
		{name: "a whitespace-only key is no answer either", configured: "  \t ",
			env: map[string]string{"EDITOR": "nano"}, goos: "linux", want: []string{"nano"}},
		{name: "the darwin opener", goos: "darwin", want: []string{"open"}},
		{name: "the linux opener", goos: "linux", want: []string{"xdg-open"}},
		{name: "the windows opener", goos: "windows", want: []string{"cmd", "/c", "start", ""}},
		{name: "an unknown platform is offered the freedesktop opener", goos: "plan9", want: []string{"xdg-open"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := editorArgv(tt.configured, func(k string) string { return tt.env[k] }, tt.goos)
			if !slices.Equal(got.argv, tt.want) {
				t.Errorf("editorArgv = %v, want %v", got.argv, tt.want)
			}
		})
	}
}

// How a resolved editor is started is a fact about the program (ADR 0041 decision 6): the nine
// terminal editors need the tty and are run in the foreground, and everything else — a GUI editor, a
// desktop opener stub, a program apogee has never heard of — is started detached, because a GUI
// editor held open by the pane blanks the session for as long as somebody is editing and an opener
// returns before the editor is even on screen.
func TestEditorArgvClassifiesTheSpawnMode(t *testing.T) {
	t.Parallel()
	for _, program := range []string{"vi", "vim", "nvim", "nano", "pico", "emacs", "micro", "hx", "kak"} {
		if got := editorArgv(program, noEnvironment, "linux"); got.spawn != spawnForeground {
			t.Errorf("%s resolved detached; a terminal editor drawn over a live alt-screen TUI is broken", program)
		}
	}
	// A terminal editor spelled as a path is still recognised by its name, exactly as the line jump is.
	if got := editorArgv(filepath.Join("/usr", "bin", "vim"), noEnvironment, "linux"); got.spawn != spawnForeground {
		t.Error("a path-spelled vim resolved detached; the classification asks for the program's NAME")
	}
	for _, program := range []string{"code -w", "open", "xdg-open", "some-editor-nobody-has-heard-of"} {
		if got := editorArgv(program, noEnvironment, "linux"); got.spawn != spawnDetached {
			t.Errorf("%s resolved foreground; only the nine terminal editors take the terminal", program)
		}
	}
	for _, goos := range []string{"darwin", "linux", "windows", "plan9"} {
		if got := editorArgv("", noEnvironment, goos); got.spawn != spawnDetached {
			t.Errorf("the %s opener resolved foreground; an opener is a launcher stub, not a terminal editor", goos)
		}
	}
}

// `+<line>` goes to the editors known to take it and to nothing else: an editor that does not know
// the argument takes it as a FILENAME and opens an empty buffer called "+37", which is worse than
// landing at the top of the file.
func TestExternalEditPassesTheLineJumpOnlyToEditorsThatTakeOne(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	writeSettingsFixture(t, path, "mode: auto\nservers:\n  - name: local\n    endpoint: http://127.0.0.1:1111\n")

	// `servers:` is on the second line of that fixture, and it is the line the jump names.
	withVim := specFor(t, home, map[string]string{"EDITOR": "vim"}, "servers")
	if want := []string{"vim", "+2", path}; !slices.Equal(withVim.Argv, want) {
		t.Errorf("argv = %v, want %v", withVim.Argv, want)
	}
	withCode := specFor(t, home, map[string]string{"EDITOR": "code -w"}, "servers")
	if want := []string{"code", "-w", path}; !slices.Equal(withCode.Argv, want) {
		t.Errorf("argv = %v, want %v — an editor outside the allowlist is handed the file alone", withCode.Argv, want)
	}
	// A path-spelled editor is still recognised by its NAME, and a key the file does not set has no
	// active line to jump to (this fixture documents no commented example either).
	byPath := specFor(t, home, map[string]string{"EDITOR": filepath.Join("/usr", "bin", "nvim")}, "mcp-servers")
	if want := []string{filepath.Join("/usr", "bin", "nvim"), path}; !slices.Equal(byPath.Argv, want) {
		t.Errorf("argv = %v, want %v — no line to point at, so no jump", byPath.Argv, want)
	}
	// The OS opener is the rung nobody chose, and it is the one that must never see a `+<line>`: it
	// would hand the argument to the desktop as a second FILE to open.
	opener := specFor(t, home, nil, "servers")
	if want := []string{"xdg-open", path}; !slices.Equal(opener.Argv, want) {
		t.Errorf("argv = %v, want %v — an opener takes the file alone", opener.Argv, want)
	}

	// The ladder's first rung follows the same rule as the rest: the jump goes with the program the
	// `editor` key names, and `servers:` sits one line lower in a file that sets it.
	keyed := t.TempDir()
	keyedPath := filepath.Join(keyed, "config.yaml")
	writeSettingsFixture(t, keyedPath, "editor: nvim\nmode: auto\nservers:\n"+
		"  - name: local\n    endpoint: http://127.0.0.1:1111\n")
	withKey := specFor(t, keyed, map[string]string{"EDITOR": "code"}, "servers")
	if want := []string{"nvim", "+3", keyedPath}; !slices.Equal(withKey.Argv, want) {
		t.Errorf("argv = %v, want %v — the config key outranks $EDITOR, jump included", withKey.Argv, want)
	}
}

// specFor builds the seam over a temp home and asks it for one key's launch. The platform is pinned
// and every program is found: these are assertions about the command the pane is handed, not about
// which editors the machine running the test has installed.
func specFor(t *testing.T, home string, env map[string]string, key string) tui.EditorCommand {
	t.Helper()
	e := newExternalEdit(config.Options{ConfigDir: home}, func(k string) string { return env[k] })
	e.goos = "linux"
	e.look = editorAlwaysFound
	launch, err := e.spec(key)
	if err != nil {
		t.Fatalf("spec(%s): %v", key, err)
	}
	return launch
}

// editorAlwaysFound is the lookup for the tests whose subject is the resolution rather than the
// machine: every program the ladder names exists.
func editorAlwaysFound(program string) (string, error) { return program, nil }

// The return trip reports what the human CHANGED and nothing else: the baseline is taken when the
// editor is launched, so a key they left alone is silent however this session resolved it. Two kinds
// of key are never reported — the confinement pair, whose interlock stays single-homed in /confine
// (ADR 0012), and `server:`, whose live move is a deliberate act at the picker (ADR 0037 decision 4).
func TestExternalEditReloadReportsTheKeysTheFileChanged(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	writeSettingsFixture(t, path, "server: local\nmode: ask-before\nservers:\n"+
		"  - name: local\n    endpoint: http://127.0.0.1:1111\n")

	e := newExternalEdit(config.Options{ConfigDir: home}, func(string) string { return "" })
	e.look = editorAlwaysFound                   // the subject is the return trip, not this machine's editors
	if _, err := e.spec("servers"); err != nil { // the baseline the return trip diffs against
		t.Fatalf("spec: %v", err)
	}

	writeSettingsFixture(t, path, "server: other\nmode: auto\nconfine-to-workspace: false\nservers:\n"+
		"  - name: local\n    endpoint: http://127.0.0.1:1111\n"+
		"  - name: other\n    endpoint: http://127.0.0.1:2222\n"+
		"system-prompt-text: |\n  hand written\n")

	applied, err := e.changed()
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	got := map[string]string{}
	for _, a := range applied {
		got[a.Path] = a.Value
	}
	want := map[string]string{"mode": "auto", "servers": "2 servers", "system-prompt-text": "hand written\n"}
	for path, value := range want {
		if got[path] != value {
			t.Errorf("reload reported %s = %q, want %q", path, got[path], value)
		}
	}
	for _, skipped := range []string{"server", "confine-to-workspace", "unconfined-hosts"} {
		if v, ok := got[skipped]; ok {
			t.Errorf("reload reported %s = %q; that key is never applied from a re-read", skipped, v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("reload reported %v, want exactly %v", got, want)
	}

	// A second reload over an unchanged file reports nothing: the baseline moved with the first one.
	if again, err := e.changed(); err != nil || len(again) != 0 {
		t.Errorf("second reload = (%v, %v), want nothing changed", again, err)
	}
}

// A text key is reported as the PROSE itself, not as the summary its row shows — the value an
// in-pane commit of the same key would have carried, so the pane journals and applies one spelling.
func TestExternalEditReloadCarriesProseForATextKey(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	writeSettingsFixture(t, path, "system-prompt-text: one\n")
	e := newExternalEdit(config.Options{ConfigDir: home}, func(string) string { return "" })
	writeSettingsFixture(t, path, "system-prompt-text: |\n  first line\n  second line\n")

	applied, err := e.changed()
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if len(applied) != 1 || applied[0].Path != "system-prompt-text" {
		t.Fatalf("reload = %+v, want the one prompt key", applied)
	}
	if want := "first line\nsecond line\n"; applied[0].Value != want {
		t.Errorf("value = %q, want the prose %q", applied[0].Value, want)
	}
}

// A structured block whose SUMMARY did not move is still a change. Repointing the single
// `mcp-servers:` entry at another machine leaves the row reading "1 server" character for character,
// and a diff over row summaries alone reports nothing — so nothing reconnects and the edit waits for
// a relaunch, which is the deferral ADR 0037 abolishes.
func TestExternalEditReloadReportsAnMCPServerRepointedUnderTheSameSummary(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	writeSettingsFixture(t, path, "mcp-servers:\n  - name: files\n    transport: streamable-http\n"+
		"    endpoint: http://127.0.0.1:7331/mcp\n")
	e := newExternalEdit(config.Options{ConfigDir: home}, func(string) string { return "" })

	writeSettingsFixture(t, path, "mcp-servers:\n  - name: files\n    transport: streamable-http\n"+
		"    endpoint: http://192.0.2.1:7331/mcp\n")
	applied, err := e.changed()
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if len(applied) != 1 || applied[0].Path != "mcp-servers" {
		t.Fatalf("reload = %+v, want the repointed mcp-servers block", applied)
	}
	if applied[0].Value != "1 server" {
		t.Errorf("value = %q, want the row's own summary %q", applied[0].Value, "1 server")
	}

	// The baseline moved with it, so the same file re-read reports nothing.
	if again, err := e.changed(); err != nil || len(again) != 0 {
		t.Errorf("second reload = (%v, %v), want nothing changed", again, err)
	}
}

// The same blind spot on the other proved key: the one entry of a `model-profiles:` map keeps its
// pattern — so its row keeps reading "1 model profile" — while the delimiters the stripper actually
// matches on are replaced. Nothing about the summary can say so.
func TestExternalEditReloadReportsThinkingDelimitersUnderTheSameSummary(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	profile := func(start, end string) string {
		return "model-profiles:\n  minimax-m3:\n    tool-call-format: markdown-fenced\n    thinking:\n" +
			"      style: delimited\n      start: \"" + start + "\"\n      end: \"" + end + "\"\n"
	}
	writeSettingsFixture(t, path, profile("<think>", "</think>"))
	e := newExternalEdit(config.Options{ConfigDir: home}, func(string) string { return "" })

	writeSettingsFixture(t, path, profile("<|channel|>", "<|message|>"))
	applied, err := e.changed()
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if len(applied) != 1 || applied[0].Path != "model-profiles" {
		t.Fatalf("reload = %+v, want the re-delimited model-profiles entry", applied)
	}
	if want := "1 model profile"; applied[0].Value != want {
		t.Errorf("value = %q, want the row's own summary %q", applied[0].Value, want)
	}
}

// A file the startup resolution refuses is refused HERE, with nothing reported: the human's own text
// is left exactly as they wrote it and the pane puts the reason on the row they launched from. The
// baseline stands, so fixing the file and coming back reports the fix against what was there before.
func TestExternalEditReloadRefusesAConfigItCannotResolve(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	writeSettingsFixture(t, path, "mode: ask-before\n")
	e := newExternalEdit(config.Options{ConfigDir: home}, func(string) string { return "" })

	writeSettingsFixture(t, path, "mode: [this is not a mode]\n")
	applied, err := e.changed()
	if err == nil {
		t.Fatalf("changed over a malformed config = %+v, want the refusal", applied)
	}
	if len(applied) != 0 {
		t.Errorf("a refused reload reported %+v; nothing may be applied from a file that did not resolve", applied)
	}

	writeSettingsFixture(t, path, "mode: auto\n")
	fixed, err := e.changed()
	if err != nil {
		t.Fatalf("changed after the fix: %v", err)
	}
	if len(fixed) != 1 || fixed[0].Path != "mode" || fixed[0].Value != "auto" {
		t.Errorf("reload after the fix = %+v, want the mode change against the surviving baseline", fixed)
	}
}

// A config that resolves but names no startup server is NOT a failure: it is the pre-bound start
// ADR 0036 answers with a picker, and it is exactly the state a config is in halfway through having
// its first `servers:` entry added by hand.
func TestExternalEditReloadAcceptsAConfigWithNoStartupServer(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	writeSettingsFixture(t, path, "mode: ask-before\n")
	e := newExternalEdit(config.Options{ConfigDir: home}, func(string) string { return "" })

	writeSettingsFixture(t, path, "mode: ask-before\nauto-compact: false\n")
	applied, err := e.changed()
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if len(applied) != 1 || applied[0].Path != "auto-compact" || applied[0].Value != "false" {
		t.Errorf("reload = %+v, want the one changed key", applied)
	}
}

// The `servers:` list reaches no engine seam: the picker, the switch and the choice recording all
// resolve names against the holder, so installing the re-read list IS the apply — and the popup
// provider, which projects that same list, offers the new entry the moment it lands.
func TestApplySettingServersInstallsTheReReadList(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	launchOpts := config.Options{Servers: []config.ServerEntry{{Name: "local", Endpoint: "http://127.0.0.1:1111"}}}
	live := newLiveSettings(launchOpts, nil)
	apply := applySettingFor(settingsApplier{engine: &applySettingSpy{}, live: live, configPath: path})

	writeSettingsFixture(t, path, "servers:\n"+
		"  - name: local\n    endpoint: http://127.0.0.1:1111\n"+
		"  - name: box\n    endpoint: http://192.0.2.1:1111\n")
	if _, err := apply("servers", "2 servers"); err != nil {
		t.Fatalf("apply servers: %v", err)
	}
	names := []string{}
	for _, c := range live.choices(launchOpts) {
		names = append(names, c.Name)
	}
	if want := []string{"local", "box"}; !slices.Equal(names, want) {
		t.Errorf("switchable servers = %v, want %v", names, want)
	}

	// Validate-then-commit: a list startup would refuse never displaces one that works.
	writeSettingsFixture(t, path, "servers:\n  - endpoint: http://127.0.0.1:1111\n")
	if _, err := apply("servers", "1 server"); err == nil {
		t.Fatal("apply of a nameless server entry: want the refusal, got none")
	}
	if got := len(live.serverList()); got != 2 {
		t.Errorf("server list holds %d entries, want the last good 2", got)
	}
}

// `mechanisms:` and `validated-sets:` are inputs to the per-model resolution rather than values the
// engine holds, so both land in the holder and are committed by the rebind — the one door a model
// change and a config change share.
func TestApplySettingMechanismBlocksRideTheRebind(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	live := newLiveSettings(config.Options{ValidatedSetsEnable: true}, nil)
	probe := &rebindProbe{}
	apply := applySettingFor(settingsApplier{
		engine:     &applySettingSpy{},
		live:       live,
		binding:    func() upstreamBinding { return upstreamBinding{Model: "bound-model"} },
		rebind:     probe.rebind,
		configPath: path,
	})

	writeSettingsFixture(t, path, "validated-sets:\n  enable: false\n")
	if _, err := apply("validated-sets.enable", "false"); err != nil {
		t.Fatalf("apply validated-sets.enable: %v", err)
	}
	base, _, _, _ := live.rebindInputs(config.Options{}, upstreamBinding{})
	if base.ValidatedSetsEnable {
		t.Error("validated-sets stayed on; the re-read block must reach the resolution inputs")
	}
	if len(probe.calls) != 1 {
		t.Errorf("rebind drives = %+v, want the one re-drive that commits the block", probe.calls)
	}

	// The block's two rows are one apply: an alias map edited in the human's own editor comes back as
	// `validated-sets.alias` and has to reach the same re-read, or a carry-over would sit in the file
	// until the next launch.
	writeSettingsFixture(t, path, "validated-sets:\n  alias:\n    my-gemma: gemma-4\n")
	if _, err := apply("validated-sets.alias", "1 alias"); err != nil {
		t.Fatalf("apply validated-sets.alias: %v", err)
	}
	base, _, _, _ = live.rebindInputs(config.Options{}, upstreamBinding{})
	if base.ValidatedSetsAlias["my-gemma"] != "gemma-4" {
		t.Errorf("alias map = %v, want the re-read carry-over", base.ValidatedSetsAlias)
	}
	if len(probe.calls) != 2 {
		t.Errorf("rebind drives = %+v, want the alias edit to ride the rebind too", probe.calls)
	}

	// A `mechanisms:` block naming an id this build does not have is refused by the startup producer,
	// before it can replace a list that arms something.
	writeSettingsFixture(t, path, "mechanisms:\n  no-such-mechanism: true\n")
	if _, err := apply("mechanisms", "1 mechanism"); err == nil {
		t.Fatal("apply of an unknown mechanism id: want the refusal, got none")
	}
	if len(probe.calls) != 2 {
		t.Errorf("rebind drives = %+v, want no drive for a block that never installed", probe.calls)
	}
}

// The composition root wires both halves of the round trip, and the spec it hands back names the
// config file this session resolved.
func TestRunRootWiresTheExternalEditSeams(t *testing.T) {
	t.Parallel()
	rec := &recordingLauncher{}
	home := t.TempDir()
	// The composition root looks the resolved editor up for real, so the config names one that exists
	// on every machine this suite runs on: the test binary itself.
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	writeSettingsFixture(t, filepath.Join(home, "config.yaml"), "editor: "+self+"\nmode: ask-before\n")
	opts := config.Options{
		Endpoint:  "http://127.0.0.1:1111",
		Model:     "fake",
		Mode:      "ask-before",
		Workspace: t.TempDir(),
		ConfigDir: home,
	}
	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if rec.opts.ExternalEditSpec == nil || rec.opts.ReloadConfig == nil {
		t.Fatal("the composition root left an external-edit seam unwired")
	}
	launch, err := rec.opts.ExternalEditSpec("servers")
	if err != nil {
		t.Fatalf("ExternalEditSpec: %v", err)
	}
	if want := filepath.Join(home, "config.yaml"); launch.Argv[len(launch.Argv)-1] != want {
		t.Errorf("argv ends with %q, want the session's config %q", launch.Argv[len(launch.Argv)-1], want)
	}
	// Nothing has touched the file since the spec read it, so the return trip over it reports nothing.
	applied, err := rec.opts.ReloadConfig()
	if err != nil {
		t.Fatalf("ReloadConfig: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("ReloadConfig over an untouched config reported %+v, want nothing", applied)
	}
}

// The value a reload carries for a plain row is the row's own displayed value, so what the pane
// journals reads exactly as the row would have read had the run started with it.
func TestAppliedValuePrefersProseOverASummary(t *testing.T) {
	t.Parallel()
	if got := appliedValue(tui.SettingRow{Value: "8 lines", Text: "the prose"}); got != "the prose" {
		t.Errorf("appliedValue = %q, want the prose a text row carries beside its summary", got)
	}
	if got := appliedValue(tui.SettingRow{Value: "3 servers"}); got != "3 servers" {
		t.Errorf("appliedValue = %q, want the row's own value", got)
	}
}

// settingKeyLine points at the ACTIVE line a key is set on, else the commented example the seeded
// template documents it with — which is where a human goes to set a key their file does not carry.
func TestSettingKeyLineFallsBackToTheCommentedExample(t *testing.T) {
	t.Parallel()
	data := []byte(strings.Join([]string{
		"# mcp-servers:",
		"#   - name: files",
		"mode: auto",
		"",
	}, "\n"))
	if got := settingKeyLine(data, "mode"); got != 3 {
		t.Errorf("active key line = %d, want 3", got)
	}
	if got := settingKeyLine(data, "mcp-servers"); got != 1 {
		t.Errorf("commented example line = %d, want 1", got)
	}
	if got := settingKeyLine(data, "not-a-key"); got != 0 {
		t.Errorf("unknown key line = %d, want 0", got)
	}
}

// A malformed document still opens — at the top, with no jump. "Your config is malformed" is a
// reason to hand somebody the file, not to keep it from them.
func TestSettingKeyLineIsSilentAboutADocumentItCannotParse(t *testing.T) {
	t.Parallel()
	if got := settingKeyLine([]byte("mode: [unterminated\n"), "mode"); got != 0 {
		t.Errorf("line = %d, want 0 for a document the parse refuses", got)
	}
}

// The baseline seeded at construction is the FILE's own view, so a key an environment variable
// outranks this run is not reported as changed the first time the pane opens an editor.
func TestExternalEditBaselineIsTheFileNotTheResolution(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	writeSettingsFixture(t, filepath.Join(home, "config.yaml"), "mode: plan\n")
	// The session resolved `auto` (an APOGEE_MODE override would do this); the file still says plan.
	e := newExternalEdit(config.Options{ConfigDir: home, Mode: "auto"}, func(string) string { return "" })

	applied, err := e.changed()
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("reload reported %+v over an unedited file; the baseline must be the file's own view", applied)
	}
}

// The `editor` key is read from the FILE at every launch, not from the session's startup snapshot:
// a key set on its row a minute ago — or by another window — opens the next edit, not the next run.
func TestExternalEditSpecReadsTheEditorTheFileNamesNow(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	writeSettingsFixture(t, path, "mode: auto\n")
	// The startup snapshot names an editor the file no longer does; it must not be consulted.
	e := newExternalEdit(config.Options{ConfigDir: home, Editor: "stale-editor"}, func(string) string { return "" })
	e.goos = "linux"
	e.look = editorAlwaysFound

	launch, err := e.spec("mode")
	if err != nil {
		t.Fatalf("spec: %v", err)
	}
	if want := []string{"xdg-open", path}; !slices.Equal(launch.Argv, want) {
		t.Errorf("argv = %v, want %v — the file sets no editor, and the startup snapshot is not the file", launch.Argv, want)
	}

	writeSettingsFixture(t, path, "editor: micro\nmode: auto\n")
	launch, err = e.spec("mode")
	if err != nil {
		t.Fatalf("spec after the key was set: %v", err)
	}
	if want := []string{"micro", "+2", path}; !slices.Equal(launch.Argv, want) {
		t.Errorf("argv = %v, want %v — an editor set in this session opens the next edit", launch.Argv, want)
	}
}

// The spawn mode crosses the seam with the argv (ADR 0041 decision 6): the renderer is told whether
// this program takes the terminal, because which editors need a tty is a fact about the programs and
// the thin renderer holds no table of them. The argv is the same either way — the classification
// changes how it is STARTED, not what is run.
func TestExternalEditSpecCarriesTheSpawnModeAcrossTheSeam(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")

	writeSettingsFixture(t, path, "editor: vim\nmode: auto\n")
	foreground := specFor(t, home, nil, "mode")
	if want := []string{"vim", "+2", path}; !slices.Equal(foreground.Argv, want) {
		t.Errorf("argv = %v, want %v", foreground.Argv, want)
	}
	if foreground.Detached {
		t.Error("a terminal editor crossed the seam detached; the pane has to suspend for it")
	}

	// Nothing set: the ladder ends at the platform's opener, which returns before the editor is on
	// screen and must never hold the pane.
	opener := t.TempDir()
	openerPath := filepath.Join(opener, "config.yaml")
	writeSettingsFixture(t, openerPath, "mode: auto\n")
	detached := specFor(t, opener, nil, "mode")
	if want := []string{"xdg-open", openerPath}; !slices.Equal(detached.Argv, want) {
		t.Errorf("argv = %v, want %v", detached.Argv, want)
	}
	if !detached.Detached {
		t.Error("the OS opener crossed the seam foreground; the pane would blank on a stub that returns at once")
	}
}

// A program this machine cannot run is refused before the pane suspends into it, and the refusal
// names all three ways to set an editor (ADR 0041 decision 4): with nothing set the ladder ends at an
// opener the user never chose, and "executable file not found in $PATH" names that program at them.
func TestExternalEditSpecRefusesAnEditorThisMachineCannotRun(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	writeSettingsFixture(t, filepath.Join(home, "config.yaml"), "mode: auto\n")
	e := newExternalEdit(config.Options{ConfigDir: home}, func(string) string { return "" })
	e.goos = "linux"
	e.look = func(string) (string, error) { return "", errors.New("executable file not found in $PATH") }

	launch, err := e.spec("mode")
	if err == nil {
		t.Fatalf("spec over an editor nobody can run = %+v, want the refusal", launch)
	}
	for _, want := range []string{"xdg-open", "editor", "$VISUAL", "$EDITOR"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name %s; all three ways to set an editor belong in it", err, want)
		}
	}
}

// A build with no config path to open refuses rather than launching an editor on a promise it
// cannot keep — the same refusal every write in this package makes for the same reason.
func TestExternalEditSpecRefusesWhenThereIsNoConfigPath(t *testing.T) {
	t.Parallel()
	e := &externalEdit{getenv: func(string) string { return "" }, goos: "linux"}
	if launch, err := e.spec("servers"); err == nil {
		t.Errorf("spec with no config path = %+v, want the refusal", launch)
	}
}
