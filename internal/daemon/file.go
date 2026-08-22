package daemon

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/schedule"
)

// DefaultShutdownGrace is how long a stopping daemon lets an in-flight Firing finish when the
// file names no `shutdown-grace:` — long enough for a real coding run to reach its own end, short
// enough that a supervisor's own kill timer is not the thing that ends the process.
const DefaultShutdownGrace = 10 * time.Minute

// File is one parsed, validated schedules.yaml with its defaults applied. A zero File — no
// schedules at all — is legal: the daemon adopts nothing, says so, and keeps watching the file,
// which is what a first run against the seeded template does.
type File struct {
	// ShutdownGrace is how long an in-flight Firing gets to finish after the first stop signal.
	// Always positive: the schema's default stands in where the file said nothing.
	ShutdownGrace time.Duration
	// Schedules is the adopted set, in file order. Names are unique across it.
	Schedules []Entry
}

// Entry is one schedule: what fires it, and what it runs. Its fields are all comparable, so two
// entries are equal exactly when every field matches — which is the identity the reload diff
// decides "unchanged, keep its phase" by (ADR 0034).
type Entry struct {
	// Name identifies the entry across a reload and labels its Firings in the log and in every
	// saved record. Required, unique within the file.
	Name string `yaml:"name"`
	// On is the trigger. Cycle is the only one v1 speaks; ADR 0034 keeps the key a map so `at:`
	// and a webhook can join it without moving anything.
	On Trigger `yaml:"on"`
	// Run is the action.
	Run Action `yaml:"run"`
}

// Trigger is what makes an Entry fire.
type Trigger struct {
	// Cycle is how often the entry fires: at least [schedule.MinCycle]. The first Firing lands one
	// full cycle after the entry is adopted, so nothing fires at daemon startup.
	Cycle time.Duration `yaml:"cycle"`
}

// Action is what an Entry runs when it fires.
type Action struct {
	// Prompt is the single user message every Firing submits. Required.
	Prompt string `yaml:"prompt"`
	// Workspace is the directory the Firing runs in, `~`-expanded and checked to exist at
	// validation. Required: a Firing that runs nowhere in particular is a footgun, not a default.
	Workspace string `yaml:"workspace"`
	// Mode is the autonomy mode: plan (the default) or auto. Nothing else — a Firing has no human
	// to consult, and the two middle rungs of the ladder exist to consult one (ADR 0033).
	Mode domain.Mode `yaml:"mode,omitempty"`
	// Server names a `servers:` entry in config.yaml. Empty binds the Firing to the same startup
	// default a fresh session on this host gets (ADR 0055).
	Server string `yaml:"server,omitempty"`
	// Model overrides the bound server's model for this entry's Firings. Legal only where a model
	// name is a per-request selection: on a llama-launcher-fronted server it would be a request to
	// LOAD one, and the daemon never actuates the launcher (ADR 0055).
	Model string `yaml:"model,omitempty"`
}

// ServerFacts is everything validation needs to know about one `servers:` entry. Deliberately not
// the entry itself: the schedules file never carries an endpoint or a key (ADR 0055 decision 4),
// so this package has no business seeing either.
type ServerFacts struct {
	// IsLauncherFronted reports that llama-launcher owns the server's model slot, which is what
	// makes a `model:` on the entry a request to actuate rather than a per-request selection.
	IsLauncherFronted bool
}

// Host carries the facts about this machine and its config that the schedules file cannot state
// about itself. The caller already holds all of them; injecting them keeps every rule in this
// package testable without a config file, a launcher or a kernel.
type Host struct {
	// Home is the directory a leading `~` in a `workspace:` expands against. A file that uses `~`
	// with no Home set is a defect naming the entry, never a silent literal `~` directory.
	Home string
	// LookupServer answers for one `servers:` name: whether an entry answers to it at all, and
	// what validation needs to know about it. It is called with the entry's `server:` value
	// verbatim; an EMPTY name asks for the host's startup default, and a lookup that has no answer
	// for that reports false — which skips the launcher rule for a default-bound entry rather than
	// inventing a defect. A nil LookupServer answers "unknown" to everything, so every entry that
	// names a server is refused; the caller is expected to supply one.
	LookupServer func(name string) (ServerFacts, bool)
	// AutoEligible is this host's Auto-eligibility verdict (ADR 0012: workspace confinement is
	// available). False refuses every `mode: auto` entry at validation, where the refusal can name
	// the entry, rather than at the Firing that would have run unconfined.
	AutoEligible bool
}

// Load decodes and validates a schedules file's bytes, returning the file with its defaults
// applied — [DefaultShutdownGrace], `mode: plan`, and every `workspace:` expanded against
// Host.Home.
//
// Parsing is STRICT: an unknown key is a defect naming it, because this file is hand-edited and a
// typo that parses is a schedule that silently never does what it was edited to do. Empty bytes —
// or bytes holding nothing but comments — are a valid empty set, not a defect.
//
// Every defect is reported, joined into one error (see the package doc), with one exception: when
// the bytes do not parse, the parse defects are returned alone. Validating a half-decoded document
// would report absences the file does not have.
func Load(data []byte, host Host) (File, error) {
	document, err := parse(data)
	if err != nil {
		return File{}, err
	}
	return validate(document, host)
}

// fileDocument is the file's YAML shape. It differs from [File] in one place: `shutdown-grace:`
// decodes into a pointer so that ABSENT (take the default) and a written `0s` (a defect: a grace
// of nothing is not what anyone means to write) stay distinguishable.
type fileDocument struct {
	ShutdownGrace *time.Duration `yaml:"shutdown-grace"`
	Schedules     []Entry        `yaml:"schedules"`
}

// parse decodes the file's single YAML document with unknown-key checking on. A second document is
// refused on internal/config's Document reasoning: the decoder reads only the first, so entries
// appended after a `---` would be edited and never fire.
func parse(data []byte) (fileDocument, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var document fileDocument
	switch err := decoder.Decode(&document); {
	case errors.Is(err, io.EOF):
		return fileDocument{}, nil
	case err != nil:
		return fileDocument{}, parseDefects(err)
	}
	var second yaml.Node
	switch err := decoder.Decode(&second); {
	case err == nil:
		return fileDocument{}, errors.New("apogee: schedules.yaml: it holds more than one YAML document and apogee reads only " +
			"the first — an entry after the `---` would never fire, so keep every entry under the one `schedules:` list")
	case !errors.Is(err, io.EOF):
		return fileDocument{}, parseDefects(err)
	}
	return document, nil
}

// unknownFieldMessage matches the one yaml.TypeError line worth rewording: the file's most likely
// defect deserves the file's vocabulary rather than the decoder's Go type name.
var unknownFieldMessage = regexp.MustCompile(`^line (\d+): field (\S+) not found in type \S+$`)

// parseDefects turns a decode failure into the run's defects. A yaml.TypeError already collects
// every field-level complaint in the document, so this reports them all rather than the first.
func parseDefects(err error) error {
	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		return fmt.Errorf("apogee: schedules.yaml: %w", err)
	}
	defects := make([]error, 0, len(typeErr.Errors))
	for _, message := range typeErr.Errors {
		if parts := unknownFieldMessage.FindStringSubmatch(message); parts != nil {
			defects = append(defects, fmt.Errorf("apogee: schedules.yaml: line %s: unknown key %q — apogee refuses a key it does "+
				"not know rather than ignoring it, because an ignored key is a setting that silently never applies; check the "+
				"spelling against the commented template", parts[1], parts[2]))
			continue
		}
		defects = append(defects, fmt.Errorf("apogee: schedules.yaml: %s", message))
	}
	return errors.Join(defects...)
}

// validate applies the schema's defaults and collects every defect in the document.
func validate(document fileDocument, host Host) (File, error) {
	file := File{ShutdownGrace: DefaultShutdownGrace}
	var defects []error
	if grace := document.ShutdownGrace; grace != nil {
		if *grace <= 0 {
			defects = append(defects, fmt.Errorf("apogee: schedules.yaml: shutdown-grace: %s — the grace is how long an "+
				"in-flight Firing gets to finish when the daemon is asked to stop, so it has to be positive; remove the key "+
				"to take the default of %s", *grace, DefaultShutdownGrace))
		} else {
			file.ShutdownGrace = *grace
		}
	}
	if len(document.Schedules) > 0 {
		file.Schedules = make([]Entry, 0, len(document.Schedules))
	}
	named := make(map[string]struct{}, len(document.Schedules))
	for i, entry := range document.Schedules {
		resolved, entryDefects := validateEntry(i, entry, host, named)
		defects = append(defects, entryDefects...)
		file.Schedules = append(file.Schedules, resolved)
	}
	if len(defects) > 0 {
		return File{}, errors.Join(defects...)
	}
	return file, nil
}

// validateEntry checks one entry against every rule and returns it with the schema's defaults
// applied. It runs all the rules rather than returning at the first defect, so one edit fixes an
// entry rather than one round of edit-and-rerun per mistake. named accumulates the names already
// taken, and this entry's own name joins it.
func validateEntry(index int, entry Entry, host Host, named map[string]struct{}) (Entry, []error) {
	label := entryLabel(index, entry.Name)
	var defects []error

	entry.Name = strings.TrimSpace(entry.Name)
	if defect := nameDefect(label, entry.Name, named); defect != nil {
		defects = append(defects, defect)
	}
	if defect := cycleDefect(label, entry.On.Cycle); defect != nil {
		defects = append(defects, defect)
	}

	entry.Run.Prompt = strings.TrimSpace(entry.Run.Prompt)
	if entry.Run.Prompt == "" {
		defects = append(defects, fmt.Errorf("%s has no run: prompt: — the prompt is the whole instruction a Firing "+
			"submits, and there is no session for anyone to type one into", label))
	}

	workspace, defect := resolveWorkspace(label, entry.Run.Workspace, host.Home)
	entry.Run.Workspace = workspace
	if defect != nil {
		defects = append(defects, defect)
	}

	mode, defect := resolveMode(label, entry.Run.Mode, host.AutoEligible)
	entry.Run.Mode = mode
	if defect != nil {
		defects = append(defects, defect)
	}

	entry.Run.Server = strings.TrimSpace(entry.Run.Server)
	entry.Run.Model = strings.TrimSpace(entry.Run.Model)
	return entry, append(defects, bindingDefects(label, entry.Run, host)...)
}

// entryLabel names an entry the way the user can find it: its position in the list, and the name
// it gave itself when it has one.
func entryLabel(index int, name string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return fmt.Sprintf("apogee: schedules.yaml: entry %d (%q):", index+1, trimmed)
	}
	return fmt.Sprintf("apogee: schedules.yaml: entry %d:", index+1)
}

// nameDefect refuses an unnamed entry and a repeated name, and records the name as taken.
func nameDefect(label, name string, named map[string]struct{}) error {
	if name == "" {
		return fmt.Errorf("%s has no name: — the name is what a reload recognises this schedule by across an edit, "+
			"and what every log line and saved record calls it", label)
	}
	_, duplicate := named[name]
	named[name] = struct{}{}
	if duplicate {
		return fmt.Errorf("%s an earlier entry already has that name — a reload matches entries BY name, so two of them "+
			"leave it with no way to tell which schedule an edit belongs to; give this one its own", label)
	}
	return nil
}

// cycleDefect refuses a missing cycle and one under the library's floor.
func cycleDefect(label string, cycle time.Duration) error {
	switch {
	case cycle <= 0:
		return fmt.Errorf("%s has no on: cycle: — a schedule that never comes round is not a schedule; give it one, "+
			"for example 24h", label)
	case cycle < schedule.MinCycle:
		return fmt.Errorf("%s on: cycle: %s is under the %s floor — a cycle that short hammers the one model slot an "+
			"interactive session is also using, so the floor is policy rather than a suggestion", label, cycle, schedule.MinCycle)
	}
	return nil
}

// resolveWorkspace expands the entry's workspace and checks it is a directory that exists. The
// check is deliberate host contact: a workspace that is gone is exactly the defect worth catching
// at adoption rather than in a saved record hours later.
func resolveWorkspace(label, raw, home string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("%s has no run: workspace: — a Firing runs somewhere, and the daemon will not pick that "+
			"somewhere for you; name the repository or directory this schedule works in", label)
	}
	path, err := expandHome(trimmed, home)
	if err != nil {
		return trimmed, fmt.Errorf("%s run: workspace: %q: %w", label, trimmed, err)
	}
	switch info, err := os.Stat(path); {
	case err != nil:
		return path, fmt.Errorf("%s run: workspace: %q does not exist — a Firing cannot run in a directory that is not "+
			"there, and a schedule that fails every cycle is worse than one that was never adopted (%v)", label, path, err)
	case !info.IsDir():
		return path, fmt.Errorf("%s run: workspace: %q is a file — the workspace is the DIRECTORY a Firing runs in", label, path)
	}
	return path, nil
}

// expandHome resolves a leading `~` against the injected home. config.ExpandUserPath is the same
// rule against the process's real home; validation takes the home as a fact instead, so the rule
// is testable and the package stays free of the host.
func expandHome(path, home string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	if home == "" {
		return "", errors.New("this host has no home directory to expand the leading ~ against; write the path in full")
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[len("~/"):]), nil
}

// resolveMode defaults an absent mode to plan and refuses every rung a Firing may not run in —
// including auto on a host that cannot confine it.
func resolveMode(label string, mode domain.Mode, autoEligible bool) (domain.Mode, error) {
	switch mode {
	case "":
		return domain.ModePlan, nil
	case domain.ModePlan:
		return mode, nil
	case domain.ModeAuto:
		if !autoEligible {
			return mode, fmt.Errorf("%s run: mode: auto, but this host cannot confine a run to its workspace — an "+
				"unattended autonomous Firing is not something apogee will start for you unconfined; run this entry in "+
				"plan mode, or check `apogee doctor` for what this host is missing", label)
		}
		return mode, nil
	default:
		return mode, fmt.Errorf("%s run: mode: %q — a Firing has nobody to ask, and %s and %s both exist to ask; a "+
			"schedule runs in %s or %s only (ADR 0033)", label, mode, domain.ModeAskBefore, domain.ModeAllowEdits,
			domain.ModePlan, domain.ModeAuto)
	}
}

// bindingDefects checks the entry's server binding against the host's config: a name no `servers:`
// entry answers to, and a `model:` where a model name would be a request to actuate the launcher
// rather than a per-request selection (ADR 0055).
func bindingDefects(label string, run Action, host Host) []error {
	facts, known := host.lookupServer(run.Server)
	var defects []error
	if run.Server != "" && !known {
		defects = append(defects, fmt.Errorf("%s run: server: %q — no servers: entry in config.yaml answers to that "+
			"name; a schedule binds to a server BY name so the endpoint and its key stay in one file, so add the entry "+
			"there or point this one at an existing name", label, run.Server))
	}
	if run.Model != "" && known && facts.IsLauncherFronted {
		defects = append(defects, fmt.Errorf("%s run: model: %q on a server llama-launcher fronts — there a model name "+
			"is a request to LOAD that model, and the daemon never actuates the launcher (ADR 0055); drop the key and "+
			"the Firing sends to whatever that server is serving", label, run.Model))
	}
	return defects
}

// lookupServer answers for a nil seam so validation never panics on a caller that forgot one: an
// unanswerable lookup refuses every entry that names a server, loudly, which is the safe direction.
func (h Host) lookupServer(name string) (ServerFacts, bool) {
	if h.LookupServer == nil {
		return ServerFacts{}, false
	}
	return h.LookupServer(name)
}
