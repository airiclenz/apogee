package main

// The `$EDITOR` round trip: the composition root's half of ADR 0037 decision 5.
//
// Six keys of the schema hold a structure no row can express — `servers:`, `system-prompt-models:`,
// `mcp-servers:`, `mechanisms:`, `validated-sets:` and `model-profile:`. The settings pane edits them
// by handing the human the file itself, opened in their own editor on that key's line, and re-reading
// it when they come back. Both halves are the binary's, for the reason every other settings seam is
// (ADR 0011's thin renderer): where the config lives, which line a key sits on, which editor this
// environment names and what the file's new text RESOLVES to are all the schema's business, and the
// renderer that runs the command holds none of it.
//
// What crosses the seam is an argv on the way out and a list of changed keys on the way back — no
// paths, no YAML, no precedence. Applying those keys is deliberately NOT here: the pane routes each
// one through the two homes an in-pane commit already uses (its own display keys, and the live-apply
// dispatcher), so a key edited in the file and a key edited on its row land by exactly one path.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/airiclenz/apogee/internal/tui"
)

// settingKeyServer is the one key a reload deliberately never reports. `server:` is the RECORDED
// startup binding (ADR 0036 decision 2), and moving a live session onto another upstream is a
// deliberate act at the picker (ADR 0037 decision 4) — a mover, a heartbeat rebind and a restated
// start-up box. Re-reading a file is not that act, so a hand-edited `server:` does what it has always
// done: it names where the NEXT session starts.
const settingKeyServer = "server"

// lineJumpEditors are the editors known to take `+<line>` as "open here" (ADR 0037 binding B). The
// argument is passed to these and to nothing else on purpose: an editor that does not know it takes
// it as a FILENAME and opens an empty buffer called "+37", which is a far worse outcome than landing
// at the top of the file.
var lineJumpEditors = map[string]bool{
	"vi": true, "vim": true, "nvim": true, "nano": true, "pico": true,
	"emacs": true, "micro": true, "hx": true, "kak": true,
}

// externalEdit is the pair of seams the pane drives across one round trip, and the baseline they
// share. The baseline is the config as the FILE alone projected it at the moment the editor was
// launched: the return trip diffs against that rather than against the session's resolution, so what
// comes back is the human's edit and not an environment variable outranking a key (which it still
// does at the next launch — ADR 0037 decision 3) or an in-pane edit made an hour ago.
//
// It is projected through settingsRows — the very rows the pane paints — rather than through a
// second per-key table: the values are then compared in the same spelling they are displayed in, and
// a key added to the registry is diffed the day it is added rather than the day someone remembers.
//
// The mutex guards the baseline for the liveSettings reason: both seams are called from the Update
// goroutine today, and a holder that is only accidentally single-threaded is a holder that breaks
// quietly the first time it is not.
type externalEdit struct {
	mu       sync.Mutex
	baseline []tui.SettingRow

	// opts is the session's resolved snapshot, for the two fields a re-resolution needs to answer
	// from the same home and workspace this run does.
	opts options
	// configPath is the file both halves work on — the one this session resolved.
	configPath string
	// getenv and goos are injected for the same reason every resolution seam in this binary injects
	// them: the editor ladder is a table test, not a machine.
	getenv func(string) string
	goos   string
}

// newExternalEdit seeds the baseline from the file as it stands at launch. A projection that cannot
// be made — a config that has since become unreadable — falls back to the session's own rows, which
// are never nil: a baseline that exists and is a little stale reports one extra key, where a missing
// baseline would report none at all and swallow the human's edit whole.
func newExternalEdit(opts options, getenv func(string) string) *externalEdit {
	e := &externalEdit{
		opts:       opts,
		configPath: configFilePath(opts.configDir),
		getenv:     getenv,
		goos:       runtime.GOOS,
	}
	rows, err := e.fileRows()
	if err != nil {
		rows = settingsRows(opts)
	}
	e.baseline = rows
	return e
}

// spec answers [tui.Options.ExternalEditSpec]: the command line that opens the config file at key's
// own line, and the moment the return trip's baseline is taken.
//
// The baseline is refreshed HERE, immediately before the human starts editing, so that a key the
// pane itself persisted earlier in the session is not re-reported as something they just changed. A
// refresh that fails leaves the previous baseline standing rather than refusing the edit: a file that
// no longer resolves is exactly the file somebody needs an editor for.
//
// The line is resolved the same way, and fails the same way. A document the splice writer's parse
// will not risk still opens — at the top, with no jump — because "your config is malformed" is a
// reason to hand somebody the file, not to keep it from them. Only a file that cannot be READ at all
// refuses, and then the row says so.
func (e *externalEdit) spec(key string) ([]string, error) {
	// The read comes FIRST because it is also the seed: a home whose config.yaml is not there yet gets
	// the documented template written into it, and a baseline taken before that would report the whole
	// template back as the human's own edit.
	data, err := readConfigForWrite(e.configPath)
	if err != nil {
		return nil, err
	}
	if rows, err := e.fileRows(); err == nil {
		e.mu.Lock()
		e.baseline = rows
		e.mu.Unlock()
	}
	argv := editorArgv(e.getenv, e.goos)
	if line := settingKeyLine(data, key); line > 0 && lineJumpEditors[editorName(argv[0])] {
		argv = append(argv, "+"+strconv.Itoa(line))
	}
	return append(argv, e.configPath), nil
}

// changed answers [tui.Options.ReloadConfig]: the keys whose value came back different from the
// baseline, in registry order, with the value the pane is to journal and apply for each.
//
// The whole file is re-resolved through the startup path, so a parse error, a port out of range or a
// system-prompt block that contradicts itself is refused HERE, with nothing reported and nothing
// touched — the human's text is left exactly as they wrote it and the pane puts the reason on the row
// they launched from. A config that resolves but names no startup server is NOT one of those
// failures: it is the pre-bound start ADR 0036 answers with a picker, and it is a perfectly ordinary
// state to leave a config in halfway through adding a server.
//
// Two kinds of key are never reported: the confinement pair, whose interlock stays single-homed in
// /confine (ADR 0012 — binding G), and `server:` (settingKeyServer).
func (e *externalEdit) changed() ([]tui.AppliedSetting, error) {
	after, err := e.fileRows()
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	before := e.baseline
	e.baseline = after
	e.mu.Unlock()

	var applied []tui.AppliedSetting
	for i, k := range keyRegistry {
		if k.GlobalOnly || k.Path == settingKeyServer {
			continue
		}
		if i >= len(before) || i >= len(after) {
			break // a registry that changed shape under a running process cannot happen; do not guess
		}
		if before[i].Value == after[i].Value && before[i].Text == after[i].Text {
			continue
		}
		applied = append(applied, tui.AppliedSetting{Path: k.Path, Value: appliedValue(after[i])})
	}
	return applied, nil
}

// appliedValue is the value a changed row carries back: the prose itself for a text key, whose row
// shows only a summary of it, and the displayed value for every other kind. It is the same choice
// [tui.SettingRow] makes between Value and Text, made once more from the other side, so the pane
// journals and applies exactly what an in-pane commit of that key would have.
func appliedValue(row tui.SettingRow) string {
	if row.Text != "" {
		return row.Text
	}
	return row.Value
}

// fileRows projects the config file — and only the file — onto the pane's rows. It runs the STARTUP
// resolution (applyConfig) with no flags and no environment, which is what makes the two sides of the
// diff comparable and the validation the real one rather than a second, weaker copy of it.
//
// The undetermined-startup refusal is held rather than returned, exactly as the TUI's own start-up
// holds it (root.go): resolution succeeded, it simply could not name a server, and a config being
// edited towards its first `servers:` entry is in that state by definition.
func (e *externalEdit) fileRows() ([]tui.SettingRow, error) {
	next := options{
		configDir:       e.opts.configDir,
		workspace:       e.opts.workspace,
		serverFlagBound: e.opts.serverFlagBound,
	}
	err := applyConfig(&next, noFlagChanged, noEnvironment, os.ReadFile, func(string) {})
	var undetermined *startupUndetermined
	if err != nil && !errors.As(err, &undetermined) {
		return nil, err
	}
	return settingsRows(next), nil
}

// The two stubs that make applyConfig resolve from the FILE alone: no flag was set on this
// invocation, and no variable is in the environment. They are the file layer's own view of the
// config, which is the only view two reads of the same file can be compared in.
func noFlagChanged(string) bool   { return false }
func noEnvironment(string) string { return "" }

// editorArgv is the editor this environment names, split into a program and its arguments so a
// $EDITOR carrying flags ("code -w", "emacsclient -nw") is honoured rather than looked up as a
// program with a space in its name. The ladder is ADR 0037 binding B: $VISUAL, then $EDITOR, then the
// platform's own fallback — vi everywhere a POSIX shell lives, notepad on Windows.
func editorArgv(getenv func(string) string, goos string) []string {
	for _, name := range []string{"VISUAL", "EDITOR"} {
		if fields := strings.Fields(getenv(name)); len(fields) > 0 {
			return fields
		}
	}
	if goos == "windows" {
		return []string{"notepad"}
	}
	return []string{"vi"}
}

// editorName is the editor's own name, out of whatever path or command spelled it — the key the
// line-jump allowlist is asked with. Both separators are cut because the program may have been
// written with either: a Windows path reaches this on a build that knows nothing of backslashes only
// through a hand-set $EDITOR, and answering "vim" for `C:\tools\vim.exe` is the honest answer.
func editorName(program string) string {
	name := filepath.Base(program)
	if cut := strings.LastIndexAny(name, `\/`); cut >= 0 {
		name = name[cut+1:]
	}
	return strings.TrimSuffix(strings.ToLower(name), ".exe")
}

// settingKeyLine is the line the key's own text sits on, or 0 when there is no one line to point at.
// Three answers, in order: the ACTIVE line the key is set on, the commented example the seeded
// template documents it with (which is where a human goes to set a key their file does not carry),
// and nothing at all.
//
// Every failure below answers 0 rather than an error, and deliberately: the caller opens the file
// either way, and a document too malformed to locate a key in is the document most in need of being
// opened.
func settingKeyLine(data []byte, key string) int {
	k, ok := lookupKey(key)
	if !ok {
		return 0
	}
	doc, err := configDocument(data)
	if err != nil {
		return 0
	}
	if t, err := scalarTargetIn(doc, k); err == nil && t.isSet() {
		return t.keyNode.Line
	}
	head, _, _ := strings.Cut(k.Path, ".")
	line, err := commentedExampleLine(splitConfigLines(data), head)
	if err != nil {
		return 0
	}
	return line
}
