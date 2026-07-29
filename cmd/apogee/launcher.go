package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/tui"
	llamalauncher "github.com/airiclenz/llama-launcher/launcher"
)

// This file is the ONLY place in apogee that imports the llama-launcher facade (ADR 0029 D1):
// the dependency stops at the composition root, and everything above it — the engine, the
// renderer — sees closures behind nil-degrading seams instead of a launcher type. Keeping the
// import to one file is what makes that boundary checkable by reading rather than by trust.
//
// Module mechanics, settled 2026-07-28 (ADR 0029 D7): the requirement is a TAGGED release
// (≥ v1.6.1, the first that compiles on linux and windows as well as darwin) and never a
// `replace` to a sibling checkout — a bare clone of this repo has to build. Cross-repo work on
// both trees at once is done with a go.work that stays OUT of git:
//
//	go work init . ../llama-launcher     # untracked; delete it before committing
//
// so the local override lives in the developer's working tree and nothing in the module graph
// records it.
//
// The facade is imported as llamalauncher because this package already owns the identifier
// `launcher` — root.go's function type for "the thing that opens the TUI" (main passes tui.Run).
// The alias is also the honest spelling here: it matches the `llama-launcher:` config key the
// user writes, and it keeps the two senses of the word apart at every use site.
//
// The facade's contract shapes every signature below (its package doc is the authority): read
// verbs are concurrency-safe, the LIFECYCLE verbs block — up to ~30 s waiting for health, plus
// the ~20 s stop escalation, and minutes for a large model load — and must be serialized per
// address BY THE CALLER, which for this program is the TUI's actuation latch. Progress and
// notices arrive as synchronous callbacks on the calling goroutine, and the config is re-read
// fresh for every operation (never cached, never Config.Reload) so edits made in the launcher's
// own TUI are live by the next command.

// launcherOps is the seam this bridge drives the facade through — the launcher's own
// `activationOps` discipline turned around and applied on the client side. Production wires
// realLauncher, whose methods are one-line delegations; tests wire an in-memory fake, so row
// assembly and the closures built on it are exercised without a process, a socket, or a config
// file on disk.
//
// The methods mirror the facade one-for-one and add nothing: this interface exists to be faked,
// not to reinterpret. Callbacks are plain `func(string)` rather than the facade's named
// ProgressFunc/NoticeFunc types, so a fake can be written without naming a launcher type at all.
type launcherOps interface {
	// loadConfig reads and validates a launcher config file, delivering each non-fatal
	// warning to notice as raw text (nil discards).
	loadConfig(path string, notice func(string)) (*llamalauncher.Config, error)
	// discover probes every backend address the config implies and returns one instance per
	// reachable server, including ones still starting up.
	discover(cfg *llamalauncher.Config) []*llamalauncher.RunningInstance
	// loadProfile activates a resolved Launch profile at its target address, reporting the
	// instance and whether a server was actually started. It BLOCKS; see the file header.
	loadProfile(cfg *llamalauncher.Config, p *llamalauncher.ResolvedProfile, restart bool,
		progress func(string), notice func(string)) (*llamalauncher.RunningInstance, bool, error)
	// stop stops whatever instance is listening at addr, whether or not the launcher started
	// it (launcher ADR-0001). It BLOCKS for the stop escalation.
	stop(addr string) (*llamalauncher.StopResult, error)
	// unload frees the model of the named backend at addr. On a MANAGED backend that means
	// stopping the server; StopResult.ServerStopped tells the two outcomes apart.
	unload(backend, addr string) (*llamalauncher.StopResult, error)
}

// realLauncher is the production launcherOps adapter: every method delegates straight to the
// facade and holds no state, so nothing here can drift from what the library does.
type realLauncher struct{}

// Compile-time proof that the delegating adapter still IS the seam — so a facade signature that
// moves under us fails the build here, at the one file that names the library, rather than in
// whichever closure happened to call the changed verb.
var _ launcherOps = realLauncher{}

func (realLauncher) loadConfig(path string, notice func(string)) (*llamalauncher.Config, error) {
	return llamalauncher.LoadConfig(path, notice)
}

func (realLauncher) discover(cfg *llamalauncher.Config) []*llamalauncher.RunningInstance {
	return llamalauncher.DiscoverRunningInstances(cfg)
}

func (realLauncher) loadProfile(cfg *llamalauncher.Config, p *llamalauncher.ResolvedProfile, restart bool,
	progress func(string), notice func(string)) (*llamalauncher.RunningInstance, bool, error) {
	return llamalauncher.LoadProfile(cfg, p, restart, progress, notice)
}

func (realLauncher) stop(addr string) (*llamalauncher.StopResult, error) {
	return llamalauncher.Stop(addr)
}

func (realLauncher) unload(backend, addr string) (*llamalauncher.StopResult, error) {
	return llamalauncher.Unload(backend, addr)
}

// launcherConfigPath resolves the `llama-launcher:` key's three values into the one question the
// rest of the program asks — is the integration on, and which config file does it read (ADR 0029
// D4). The key is carried through config resolution exactly as written precisely so this decision
// lands HERE, at the composition root: only the root knows the launcher, so only the root can ask
// where its default config lives.
//
//   - `off` (any casing, surrounding space ignored — the web-search sentinel's posture) ⇒ off,
//     even on a machine that has a launcher config.
//   - empty or absent ⇒ AUTO-DETECT: the launcher's own default config path, and only if that
//     file is actually there. A machine without the launcher therefore simply has no local-server
//     verbs — nothing is reported, because nothing was asked for.
//   - anything else ⇒ that path, `~` expanded, and on regardless of whether the file exists. A
//     user who NAMED a config gets told at the first verb that it is missing (item 4's ladder);
//     silently disabling the commands they configured would be the unhelpful answer.
func launcherConfigPath(opts options) (string, bool) {
	v := strings.TrimSpace(opts.llamaLauncher)
	if strings.EqualFold(v, "off") {
		return "", false
	}
	if v != "" {
		path, err := expandUserPath(v)
		if err != nil {
			// No home to expand against is not a reason to hide a configured integration: keep
			// the value as written so the first verb fails naming the path the user typed.
			return v, true
		}
		return path, true
	}
	// Auto-detect. The facade's DefaultConfigPath swallows a home-directory lookup failure and
	// returns a RELATIVE path when it hits one, so an absoluteness check is what keeps a stray
	// .config/llama-launcher/config.yaml under the current workspace from lighting up the verbs.
	path := llamalauncher.DefaultConfigPath()
	if !filepath.IsAbs(path) {
		return "", false
	}
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
}

// launchProfile is one row of `/load`'s picker as this bridge assembles it — a Launch profile
// (CONTEXT.md) reduced to the facts the choice is made on. It is the composition root's own type,
// projected onto the renderer's LaunchProfileChoice at the seam, so no launcher type ever reaches
// internal/tui (the heartbeat.Beat precedent).
type launchProfile struct {
	// Name is the profile's key in the launcher config — the label, and the identity `/load`
	// activates by.
	Name string
	// Backend is the server the profile runs on: llamacpp, ollama, lmstudio.
	Backend string
	// Addr is the resolved host:port the profile would serve at, after the launcher's merge of
	// profile over defaults over backend fallback. Empty only if that merge left it unstated.
	Addr string
	// ContextWindow is the profile's merged context_size, or 0 when it is unset — the launcher
	// leaves the server's own default in place there, so 0 means UNKNOWN, not zero tokens.
	ContextWindow int
	// Running marks a profile that discovery attributes to a live instance right now.
	Running bool
}

// launchProfiles assembles the picker's rows from a FRESH read of the launcher config (ADR 0029
// D4 — never a cached Config, so a profile added in the launcher's TUI a moment ago is offered
// here). Rows come back in the launcher's own display order, favourites first, because the order
// a user arranged in their launcher is the order they expect to browse.
//
// It returns the rows, the warnings collected along the way, and the one error that sinks the
// list: the config could not be read at all. Everything smaller is a WARNING against a skipped
// row — a single profile whose model file has moved must not cost the user the other nine — and
// the caller prints them as transcript notes beside the picker.
func launchProfiles(ops launcherOps, path string) ([]launchProfile, []string, error) {
	var warnings []string
	cfg, err := ops.loadConfig(path, func(notice string) {
		warnings = append(warnings, notice)
	})
	if err != nil {
		return nil, warnings, err
	}

	names := cfg.ProfileNames()
	rows := make([]launchProfile, 0, len(names))
	for _, name := range names {
		profile, err := cfg.ResolveProfile(name)
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		rows = append(rows, launchProfile{
			Name:          name,
			Backend:       profile.Backend,
			Addr:          profileAddr(profile),
			ContextWindow: intValue(profile.ContextSize),
		})
	}
	if len(rows) == 0 {
		return rows, warnings, nil
	}

	// ONE discovery sweep for the whole list: it probes every address the config implies, in
	// parallel, and marking N rows from it costs nothing more than marking one.
	//
	// Attribution is the launcher's own — RunningInstance.ActiveProfile, which it fills by
	// matching a live instance's address and loaded model against the profiles and leaves EMPTY
	// when more than one candidate fits. So an ambiguous instance marks nothing, for free: this
	// side only has to ignore the empty name rather than re-derive the match and disagree.
	running := make(map[string]struct{})
	for _, inst := range ops.discover(cfg) {
		if inst == nil || inst.ActiveProfile == "" {
			continue
		}
		running[inst.ActiveProfile] = struct{}{}
	}
	for i := range rows {
		if _, live := running[rows[i].Name]; live {
			rows[i].Running = true
		}
	}
	return rows, warnings, nil
}

// profileAddr renders a resolved profile's host:port. The launcher's own merge fills both fields
// from the backend's defaults when the profile states neither, so an empty answer here means the
// address genuinely could not be resolved — reported as unknown rather than as a bogus ":0".
//
// It is built the same way endpointAddr builds its answer, because the two are COMPARED: `/load`
// decides whether it has to move the session by asking whether the profile's address is the one
// the session is already on, and a comparison between two spellings of one address would answer
// that wrongly.
func profileAddr(profile *llamalauncher.ResolvedProfile) string {
	if profile == nil || profile.Host == nil || profile.Port == nil {
		return ""
	}
	return net.JoinHostPort(*profile.Host, strconv.Itoa(*profile.Port))
}

// intValue reads one of the launcher's optional numeric params, where a nil pointer is the
// "not set" that must stay distinct from a configured zero. Unset reads as 0, which every caller
// already spells "unknown".
func intValue(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// endpointAddr reduces an apogee endpoint URL to the host:port the launcher speaks in — the
// translation that lets a session say "act on the server I am talking to" (`/unload`, `/stop`)
// and lets a profile's address be compared against the current one (`/load`).
//
// A URL with no explicit port resolves to its scheme's default, because that IS the address the
// wire connects to; a value with no host, an unparseable one, or a portless one whose scheme has
// no default is refused, since guessing an address to act on is the one mistake here that could
// stop the wrong server.
func endpointAddr(endpoint string) (string, error) {
	raw := strings.TrimSpace(endpoint)
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("apogee: %q is not a usable endpoint URL: %w", endpoint, err)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("apogee: %q names no host — an endpoint the launcher can act on "+
			"looks like http://127.0.0.1:8080", endpoint)
	}
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			return "", fmt.Errorf("apogee: %q names no port — give the endpoint the port its "+
				"server listens on", endpoint)
		}
	}
	return net.JoinHostPort(host, port), nil
}

// addrEndpoint is the other direction: the endpoint URL for a launcher address. Plain http://,
// because the launcher manages servers on the LOCAL machine and speaks to them over http itself —
// a locally started llama.cpp, Ollama or LM Studio server terminates no TLS.
func addrEndpoint(addr string) string {
	return "http://" + addr
}

// instanceAddr renders a discovered instance's host:port the way profileAddr and endpointAddr render
// theirs — through net.JoinHostPort, so an IPv6 host is bracketed exactly once. RunningInstance's own
// Addr formats with %s:%d and would leave a v6 host bare, and these three values are COMPARED against
// each other: one spelling for one address is the property the matching rests on.
func instanceAddr(instance *llamalauncher.RunningInstance) string {
	if instance == nil {
		return ""
	}
	return net.JoinHostPort(instance.Host, strconv.Itoa(instance.Port))
}

// ----------------------------------------------------------------------------
// The four TUI-facing seams (ADR 0029 D1/D2)
// ----------------------------------------------------------------------------

// launcherWiring is the composition root's half of the launcher seams: the bridge this file owns
// (the ops and the resolved config path) plus the session move, because `/load` is the one verb that
// can end in one. wire.go builds a single value and hands its four methods to internal/tui as
// closures, so the renderer sees func values and never the library.
//
// The bodies live here rather than in wire.go because they name facade types — RunningInstance,
// StopResult, ResolvedProfile — and this file is the only place in apogee that may (see the header).
// What wire.go keeps is the WIRING decision: whether the integration is on at all, and therefore
// whether the four members are func values or nil.
type launcherWiring struct {
	sessionMover
	ops  launcherOps
	path string
}

// profiles is [tui.Options.LaunchProfiles]: the row assembly above, projected onto the renderer's
// type. A fresh config read every call is the point (ADR 0029 D4) — the picker opens on what the
// launcher's config says now, not on what it said at launch.
//
// The warnings launchProfiles collects stop here, because the seam the plan fixed carries rows and
// the one error that sinks the list. Nothing is lost that the human needs: a profile that would not
// resolve is already reported by the ROW'S ABSENCE, and the launcher's config warnings reach the
// transcript through ProfileLoadResult.Notices — on the verb that actually acts on that config,
// where they are news rather than noise repeated on every browse.
func (w launcherWiring) profiles() ([]tui.LaunchProfileChoice, error) {
	rows, _, err := launchProfiles(w.ops, w.path)
	if err != nil {
		return nil, err
	}
	choices := make([]tui.LaunchProfileChoice, len(rows))
	for i, row := range rows {
		choices[i] = tui.LaunchProfileChoice{
			Name:          row.Name,
			Backend:       row.Backend,
			Addr:          row.Addr,
			ContextWindow: row.ContextWindow,
			Running:       row.Running,
		}
	}
	return choices, nil
}

// load is [tui.Options.LoadProfile], the composite verb of ADR 0029 D2: activate the Launch profile,
// then decide whether the session has to follow it.
//
// It BLOCKS — the facade's contract, and the reason the TUI runs it on a Cmd goroutine under the
// actuation latch. Everything it reads it reads fresh: the config, so a profile added seconds ago in
// the launcher's own TUI resolves, and the session's CURRENT endpoint, so the same-server question is
// answered about where the session is rather than where it launched.
//
// The notices channel is deliberately one collector across both calls: LoadConfig's warnings and
// LoadProfile's drift notice are the same kind of thing to the human — the launcher speaking in its
// own words — and they travel out even beside an error, because a load that failed after warning
// about the config still warned.
func (w launcherWiring) load(name string, progress func(string)) (tui.ProfileLoadResult, error) {
	var notices []string
	collect := func(notice string) { notices = append(notices, notice) }

	cfg, err := w.ops.loadConfig(w.path, collect)
	if err != nil {
		return tui.ProfileLoadResult{Notices: notices}, err
	}
	profile, err := cfg.ResolveProfile(name)
	if err != nil {
		return tui.ProfileLoadResult{Notices: notices}, err
	}

	// restart=false is the launcher's idempotent activation (its ADR-0007): a server already serving
	// this profile is left alone, and drifted parameters produce a notice rather than a silent
	// restart of a server the human may be mid-conversation with. `/load` means "make the world serve
	// this", and a world already serving it needs nothing done to it.
	instance, _, err := w.ops.loadProfile(cfg, profile, false, progress, collect)
	if err != nil {
		return tui.ProfileLoadResult{Notices: notices}, projectStartupTimeout(err)
	}

	// Where the profile now serves. The resolved profile is the authority — it is what was asked for,
	// and it answers even when a health-wait left no instance behind — with the discovered instance
	// as the fallback for a merge that stated no address.
	addr := profileAddr(profile)
	if addr == "" {
		addr = instanceAddr(instance)
	}
	// The comparison is against the endpoint the session is on RIGHT NOW, read from the holder rather
	// than captured at wire time, because a `/server` switch may have moved it since. An endpoint that
	// will not reduce to an address cannot be shown equal to anything, so it reads as "somewhere else"
	// and the load follows the profile — the direction that ends with the session pointed at the
	// server it just asked for.
	current := ""
	if reduced, err := endpointAddr(w.holder.Endpoint()); err == nil {
		current = reduced
	}
	if addr == "" || addr == current {
		// Nothing moves. The load landed on the very server this session talks to (or the launcher
		// could not say where it landed, which is not a licence to re-point the wire), and the next
		// beat observes the model change and rebinds through the ordinary path — ADR 0029 D1's "only
		// a Beat binds", one code path with the cold start.
		return tui.ProfileLoadResult{Notices: notices}, nil
	}

	// The api key the launcher's config carries for THIS profile's server, so the session — and the
	// Monitor that beats at it — authenticate against a keyed local server exactly as the launcher
	// does. Empty when unset, which is the common keyless local case and sends no header at all.
	// APIKeyFor is one of the accessors the facade's type aliases expose without a compatibility
	// promise; it is the only such call in apogee, and it is confined to this file with the rest of
	// the library's vocabulary.
	switched, err := w.move(addrEndpoint(addr), name, "", cfg.APIKeyFor(profile.Backend))
	if err != nil {
		return tui.ProfileLoadResult{Notices: notices}, err
	}
	return tui.ProfileLoadResult{Moved: true, Switch: switched, Notices: notices}, nil
}

// projectStartupTimeout marks the launcher's health-wait timeout so the renderer can recognise it
// without ever naming the library. internal/tui holds a sentinel of its own ([tui.ErrStartupTimeout])
// precisely because ADR 0029 D1 keeps facade types AND facade sentinels out of it — the heartbeat.Beat
// posture, applied to an error — and this is the one place that can bridge the two, being the only
// file that imports the facade.
//
// The launcher's own message and chain are kept intact: the wrapper adds no text, so the transcript
// still prints exactly what the library said (the PID and log path it names are the point), and
// errors.Is finds BOTH sentinels through it.
func projectStartupTimeout(err error) error {
	if err != nil && errors.Is(err, llamalauncher.ErrStartupTimeout) {
		return startupTimeout{err}
	}
	return err
}

// startupTimeout is that marker: an error that answers to [tui.ErrStartupTimeout] as well as to
// everything the wrapped error already answered to.
type startupTimeout struct{ error }

// Is answers for the projected sentinel; Unwrap keeps the launcher's own chain reachable, so
// errors.Is(err, llamalauncher.ErrStartupTimeout) still holds through the wrapper.
func (startupTimeout) Is(target error) bool { return target == tui.ErrStartupTimeout }

func (e startupTimeout) Unwrap() error { return e.error }

// unload is [tui.Options.UnloadServer]: free the model of the server this session is talking to. On a
// MANAGED backend the launcher's own semantic is a full stop (the model is baked into the process
// arguments), which is why the projected result keeps ServerStopped — the note has to be able to say
// which of the two things happened.
func (w launcherWiring) unload(endpoint string) (tui.ActuationResult, error) {
	instance, err := w.managedInstance(endpoint)
	if err != nil {
		return tui.ActuationResult{}, err
	}
	res, err := w.ops.unload(instance.Backend, instanceAddr(instance))
	return actuationResult(instance, res, err)
}

// stop is [tui.Options.StopServer]: stop the server at the session's endpoint outright, whether or
// not the launcher started it (its ADR-0001). Afterwards the heartbeat's existing offline handling
// owns the display — this verb reports only what it did.
func (w launcherWiring) stop(endpoint string) (tui.ActuationResult, error) {
	instance, err := w.managedInstance(endpoint)
	if err != nil {
		return tui.ActuationResult{}, err
	}
	res, err := w.ops.stop(instanceAddr(instance))
	return actuationResult(instance, res, err)
}

// managedInstance answers the question both actuation verbs have to ask first: is the server this
// session is talking to one the launcher's config implies? It reduces the endpoint to an address,
// re-reads the config fresh, and matches that address against a discovery sweep.
//
// No match is an error naming the endpoint rather than a best guess. The verbs act on a REAL address
// or on nothing: an unmanaged endpoint is usually a remote server (whose local half is the launcher's
// MCP adapter, ADR 0029 D4), and acting on the nearest thing found instead would stop a server nobody
// asked about.
func (w launcherWiring) managedInstance(endpoint string) (*llamalauncher.RunningInstance, error) {
	addr, err := endpointAddr(endpoint)
	if err != nil {
		return nil, err
	}
	cfg, err := w.ops.loadConfig(w.path, nil)
	if err != nil {
		return nil, err
	}
	for _, instance := range w.ops.discover(cfg) {
		if instance != nil && instanceAddr(instance) == addr {
			return instance, nil
		}
	}
	return nil, fmt.Errorf("the launcher doesn't manage %s", endpoint)
}

// actuationResult projects the launcher's StopResult onto the renderer's ActuationResult, error and
// all. The facade returns a NON-NIL result even on failure, carrying the steps completed before it —
// so the steps travel out beside the error rather than being discarded with it: how far a stop got
// before it failed is exactly what the human needs, and it is the only place that information exists.
//
// on is the instance the verb was aimed at — discovery's own answer for the session's endpoint —
// and it, rather than the result's own Instance, is what names the backend and address the renderer
// narrates with: StopResult carries no instance when the operation failed, and a stop that failed is
// precisely when saying WHAT it was stopping matters.
func actuationResult(on *llamalauncher.RunningInstance, res *llamalauncher.StopResult, err error) (tui.ActuationResult, error) {
	out := tui.ActuationResult{Addr: instanceAddr(on)}
	if on != nil {
		out.Backend = on.Backend
	}
	if res != nil {
		out.Steps, out.ServerStopped = res.Steps, res.ServerStopped
	}
	return out, err
}
