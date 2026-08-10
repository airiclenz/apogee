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
// What crosses the seam is an argv and how to start it on the way out, and a list of changed keys on
// the way back — no paths, no YAML, no precedence. Applying those keys is deliberately NOT here: the pane routes each
// one through the two homes an in-pane commit already uses (its own display keys, and the live-apply
// dispatcher), so a key edited in the file and a key edited on its row land by exactly one path.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/airiclenz/apogee/internal/config"
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

// terminalEditors are the editors that must OWN the terminal: they draw a full-screen interface on
// the tty they are started from, so the pane suspends for them, runs them in the foreground and
// applies what they wrote when they exit (ADR 0041 decision 6). Everything else — a GUI editor, a
// desktop opener stub — is started detached, and the file watcher supplies the apply.
//
// It holds the same nine names as lineJumpEditors and is a separate table on purpose: one answers
// "does this program understand `+<line>`" and the other "does this program need the terminal", and
// the day a program answers the two questions differently the tables part without either being
// re-derived from the other.
var terminalEditors = map[string]bool{
	"vi": true, "vim": true, "nvim": true, "nano": true, "pico": true,
	"emacs": true, "micro": true, "hx": true, "kak": true,
}

// spawnMode is how a resolved editor has to be started, which is a fact about the PROGRAM and so is
// answered here rather than at the renderer that runs it (ADR 0011's thin renderer).
type spawnMode int

const (
	// spawnForeground: suspend the TUI, hand the editor this terminal, and wait for it — the only way
	// a terminal editor is usable at all, since one drawn over a live alt-screen TUI is broken.
	spawnForeground spawnMode = iota
	// spawnDetached: start the program without the terminal and do not wait for it, so the pane stays
	// up while a GUI editor is open and an opener stub returning instantly means nothing.
	spawnDetached
)

// editorCommand is one walk down the editor ladder: the command line to run, and how to run it.
type editorCommand struct {
	argv  []string
	spawn spawnMode
}

// externalEdit is the pair of seams the pane drives across one round trip, and the baseline they
// share. The baseline is the config as the FILE alone projected it at the moment the editor was
// launched: the return trip diffs against that rather than against the session's resolution, so what
// comes back is the human's edit and not an environment variable outranking a key (which it still
// does at the next launch — ADR 0037 decision 3) or an in-pane edit made an hour ago.
//
// The mutex guards the baseline for the liveSettings reason: both seams are called from the Update
// goroutine today, and a holder that is only accidentally single-threaded is a holder that breaks
// quietly the first time it is not.
type externalEdit struct {
	mu       sync.Mutex
	baseline fileProjection

	// opts is the session's resolved snapshot, for the two fields a re-resolution needs to answer
	// from the same home and workspace this run does.
	opts config.Options
	// configPath is the file both halves work on — the one this session resolved.
	configPath string
	// getenv and goos are injected for the same reason every resolution seam in this binary injects
	// them: the editor ladder is a table test, not a machine. look is injected beside them and
	// answers the one question the ladder cannot — whether this machine actually has the program the
	// ladder named — so a test about which argv the pane is handed is not also a test of which
	// editors the machine running it has installed.
	getenv func(string) string
	goos   string
	look   func(string) (string, error)
}

// fileProjection is one reading of the config file, in the two spellings the round trip needs: the
// rows the pane paints, and the resolved values behind them.
//
// Both, rather than the rows alone, because a row is a DISPLAY of a key and six of them display a
// summary — "1 server", "on, 2 aliases", "harmony, thinking delimited". A summary is the right thing
// to paint and the wrong thing to diff: repointing the one `mcp-servers:` entry at another machine
// leaves it character-for-character identical. So the rows say what a changed key carries back
// (appliedValue) and the values say WHETHER it changed (settingChanged).
type fileProjection struct {
	rows []tui.SettingRow
	opts config.Options
}

// newExternalEdit seeds the baseline from the file as it stands at launch. A projection that cannot
// be made — a config that has since become unreadable — falls back to the session's own resolution,
// whose rows are never nil: a baseline that exists and is a little stale reports one extra key,
// where a missing baseline would report none at all and swallow the human's edit whole.
func newExternalEdit(opts config.Options, getenv func(string) string) *externalEdit {
	e := &externalEdit{
		opts:       opts,
		configPath: config.FilePath(opts.ConfigDir),
		getenv:     getenv,
		goos:       runtime.GOOS,
		look:       exec.LookPath,
	}
	projected, err := e.projection()
	if err != nil {
		projected = fileProjection{rows: settingsRows(opts), opts: opts}
	}
	e.baseline = projected
	return e
}

// spec answers [tui.Options.ExternalEditSpec]: the command line that opens the config file at key's
// own line, how that command has to be started, and the moment the return trip's baseline is taken.
//
// The spawn mode crosses the seam because it is a fact about the program the ladder just named
// (ADR 0041 decision 6), and the renderer that runs the command is the one surface that must not be
// deciding which editors need a terminal.
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
func (e *externalEdit) spec(key string) (tui.EditorCommand, error) {
	// The read comes FIRST because it is also the seed: a home whose config.yaml is not there yet gets
	// the documented template written into it, and a baseline taken before that would report the whole
	// template back as the human's own edit.
	data, err := config.ReadConfigForWrite(e.configPath)
	if err != nil {
		return tui.EditorCommand{}, err
	}
	projected, perr := e.projection()
	e.mu.Lock()
	if perr == nil {
		e.baseline = projected
	}
	// The `editor` key comes off that same fresh projection rather than off e.opts, which is the
	// STARTUP snapshot: a key set on its row a minute ago — or by another window — has to open the
	// next edit, not the next run.
	configured := e.baseline.opts.Editor
	e.mu.Unlock()

	cmd, err := e.resolveEditor(configured)
	if err != nil {
		return tui.EditorCommand{}, err
	}
	argv := cmd.argv
	if line := settingKeyLine(data, key); line > 0 && lineJumpEditors[editorName(argv[0])] {
		argv = append(argv, "+"+strconv.Itoa(line))
	}
	return tui.EditorCommand{
		Argv:     append(argv, e.configPath),
		Detached: cmd.spawn == spawnDetached,
	}, nil
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
	after, err := e.projection()
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	before := e.baseline
	e.baseline = after
	e.mu.Unlock()

	var applied []tui.AppliedSetting
	for i, k := range config.KeyRegistry {
		if k.GlobalOnly || k.Path == settingKeyServer {
			continue
		}
		if i >= len(before.rows) || i >= len(after.rows) {
			break // a registry that changed shape under a running process cannot happen; do not guess
		}
		if !settingChanged(k, before, after, i) {
			continue
		}
		applied = append(applied, tui.AppliedSetting{Path: k.Path, Value: appliedValue(after.rows[i])})
	}
	return applied, nil
}

// refresh re-takes the baseline from the file as it stands NOW — the whole of ADR 0041 decision 8 on
// this side of the seam. The pane persists a key and applies it in the same keypress, so a watcher
// that then reported that same key would apply it a SECOND time: harmless for most keys, and a second
// dial of every server for `mcp-servers:`. Refreshing here means a change apogee itself made is not a
// change at all by the time the watcher looks.
//
// It is called after a write has LANDED, so what it projects is the file the human's next edit will
// be diffed against. A projection that fails leaves the previous baseline standing, for spec's reason:
// a baseline that is a little stale reports one extra key, where a missing one would swallow the next
// real edit whole.
func (e *externalEdit) refresh() {
	projected, err := e.projection()
	if err != nil {
		return
	}
	e.mu.Lock()
	e.baseline = projected
	e.mu.Unlock()
}

// settingStructures is the LOSSLESS projection of the keys whose row shows only a SUMMARY of what
// they hold. It is what the reload diff compares them by, because two different structures summarize
// alike: repoint the one `mcp-servers:` entry at another machine and the row still reads "1 server";
// change a model profile's thinking delimiters and it still reads "harmony, thinking delimited". A
// diff over summaries reports neither, so neither applies, and the edit sits in the file waiting for
// a relaunch — the deferral ADR 0037 exists to abolish.
//
// It is keyed by registry path and covers every structured key, `unconfined-hosts` included even
// though the diff never reaches it (binding G skips the confinement pair before comparing): the table
// answers "what does this key hold", which is a fact about the schema and not about who asks.
// TestSettingStructuresCoverEveryStructuredKey pins it to the registry, so a structured key added
// there is diffed properly the day it is added rather than the day someone remembers.
//
// The values are compared, never rendered — a `servers:` entry carries an api-key, and it stays as
// far from a row as it has always been (maskedSettingValue).
var settingStructures = map[string]func(config.Options) any{
	"servers":              func(o config.Options) any { return o.Servers },
	"system-prompt-models": func(o config.Options) any { return o.SystemPrompt.Models },
	"unconfined-hosts":     func(o config.Options) any { return o.UnconfinedHosts },
	"mcp-servers":          func(o config.Options) any { return o.MCPServers },
	"mechanisms":           func(o config.Options) any { return o.Mechanisms },
	// The block's two resolved facts together: the off-switch decides whether the aliases do
	// anything, and it is the pair that `validated-sets` applies.
	"validated-sets": func(o config.Options) any { return []any{o.ValidatedSetsEnable, o.ValidatedSetsAlias} },
	"model-profile":  func(o config.Options) any { return o.Profile },
}

// settingChanged reports whether the key at registry index i came back holding something else.
//
// A key whose row shows its whole value is answered by the row — its displayed value, and the prose
// a text row carries beside it — so it is compared in the same spelling it is displayed in. A key
// whose row shows a summary is answered by its structure as well (settingStructures), and the two
// tests are OR-ed rather than exclusive: the row still catches everything it ever caught, and the
// structure catches what a summary cannot say.
func settingChanged(k config.Key, before, after fileProjection, i int) bool {
	if structure, ok := settingStructures[k.Path]; ok &&
		!reflect.DeepEqual(structure(before.opts), structure(after.opts)) {
		return true
	}
	return before.rows[i].Value != after.rows[i].Value || before.rows[i].Text != after.rows[i].Text
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

// projection reads the config file — and only the file — into the two views the diff needs. It runs
// the STARTUP resolution (ApplyConfig) with no flags and no environment, which is what makes the two
// sides of the diff comparable and the validation the real one rather than a second, weaker copy of
// it; the rows come off that same resolution (settingsRows), so a key added to the registry is
// diffed the day it is added rather than the day someone remembers.
//
// The undetermined-startup refusal is held rather than returned, exactly as the TUI's own start-up
// holds it (root.go): resolution succeeded, it simply could not name a server, and a config being
// edited towards its first `servers:` entry is in that state by definition.
func (e *externalEdit) projection() (fileProjection, error) {
	next := config.Options{
		ConfigDir:       e.opts.ConfigDir,
		Workspace:       e.opts.Workspace,
		ServerFlagBound: e.opts.ServerFlagBound,
	}
	err := config.ApplyConfig(&next, noFlagChanged, noEnvironment, os.ReadFile, func(string) {})
	var undetermined *config.StartupUndetermined
	if err != nil && !errors.As(err, &undetermined) {
		return fileProjection{}, err
	}
	return fileProjection{rows: settingsRows(next), opts: next}, nil
}

// The two stubs that make ApplyConfig resolve from the FILE alone: no flag was set on this
// invocation, and no variable is in the environment. They are the file layer's own view of the
// config, which is the only view two reads of the same file can be compared in.
func noFlagChanged(string) bool   { return false }
func noEnvironment(string) string { return "" }

// editorArgv is the editor an edit opens in, resolved down the four rungs of ADR 0041 decision 2 —
// the `editor` key, then $VISUAL, then $EDITOR, then the platform's own default opener — and
// classified by how it has to be started (decision 6).
//
// Every rung is split into a program and its arguments, so a value carrying flags ("code -w",
// "emacsclient -nw") is honoured as the command line it is rather than looked up as a program with a
// space in its name; a blank or whitespace-only rung is no answer at all and the ladder walks on.
//
// The config key outranks the environment because it is the only rung the user set FOR APOGEE: an
// inherited $VISUAL is an ambient answer to a different question, and a row reading `code -w` while
// something else opened would be a row that lies.
func editorArgv(configured string, getenv func(string) string, goos string) editorCommand {
	argv := osOpener(goos)
	for _, rung := range []string{configured, getenv("VISUAL"), getenv("EDITOR")} {
		if fields := strings.Fields(rung); len(fields) > 0 {
			argv = fields
			break
		}
	}
	if terminalEditors[editorName(argv[0])] {
		return editorCommand{argv: argv, spawn: spawnForeground}
	}
	return editorCommand{argv: argv, spawn: spawnDetached}
}

// osOpener is the ladder's last rung (ADR 0041 decision 4): the desktop's own answer to "what opens
// a .yaml", which it knows and apogee does not. Anything that is neither darwin nor windows is
// offered the freedesktop opener, which is the right guess for every platform that has one and no
// worse than a guess at a text editor for the platforms that do not.
//
// The empty string on Windows is `start`'s window-TITLE argument: without it, a quoted path is read
// as the title and nothing opens.
func osOpener(goos string) []string {
	switch goos {
	case "darwin":
		return []string{"open"}
	case "windows":
		return []string{"cmd", "/c", "start", ""}
	default:
		return []string{"xdg-open"}
	}
}

// resolveEditor walks the ladder and then answers the question no resolution can: whether this
// machine has that program at all. A program nobody can run is refused HERE — before the pane
// suspends into it, or starts a detached child that dies unwatched — and the refusal names all three
// ways to set an editor (ADR 0041 decision 4).
//
// It deliberately does not carry Go's own "executable file not found in $PATH" through: on a box
// without xdg-utils the ladder ends at an opener the user never chose, and naming that program at
// them explains nothing they can act on.
func (e *externalEdit) resolveEditor(configured string) (editorCommand, error) {
	cmd := editorArgv(configured, e.getenv, e.goos)
	look := e.look
	if look == nil {
		look = exec.LookPath
	}
	if _, err := look(cmd.argv[0]); err != nil {
		return editorCommand{}, fmt.Errorf("cannot run editor %q: name one in the `editor` key of "+
			"config.yaml, or in $VISUAL or $EDITOR, or install this platform's default opener (%s)",
			cmd.argv[0], osOpener(e.goos)[0])
	}
	return cmd, nil
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
	k, ok := config.LookupKey(key)
	if !ok {
		return 0
	}
	doc, err := config.Document(data)
	if err != nil {
		return 0
	}
	if t, err := config.ScalarTargetIn(doc, k); err == nil && t.IsSet() {
		return t.KeyNode.Line
	}
	head, _, _ := strings.Cut(k.Path, ".")
	line, err := config.CommentedExampleLine(config.SplitConfigLines(data), head)
	if err != nil {
		return 0
	}
	return line
}
