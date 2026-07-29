package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
