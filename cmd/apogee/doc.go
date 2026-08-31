// Command apogee is the terminal coding agent for small local LLMs.
//
// It is the product binary and the composition root (phase-2 detail plan §3 C5): it
// resolves the state roots (no implicit ~/.apogee in the library — ADR 0001 / C7),
// builds a Config, constructs the Agent through the public apogee package (dogfooding
// the shipped surface), and hands it to the internal/tui renderer. The root command
// launches the TUI; the subcommands it also carries are assembled here, from the
// subcommands() registration seam (`apogee probe`, `apogee headless` and `apogee daemon`
// today, phase-2 detail plan §6 and ADR 0034).
//
// Everything this layer owns is a fact nothing above it holds: where state lives on
// disk, which server a run starts on, how this host draws and edits a config key, which
// of them a live session may change, and what an out-of-band call — a naming completion,
// a scheduled Firing — is built against. Each of them crosses to the renderer or the
// engine as plain data behind a seam, which is what keeps internal/tui a thin renderer
// (ADR 0011) and keeps internal/* off the root module path (ADR 0010).
//
// The files, one line each.
//
// Entry and command surface: main.go is the process entry — dispatch the
// __confined-exec sentinel, build the root, turn an error into an exit code; root.go
// the root command itself, its flag set, the [config.Options] value the flags bind to,
// and the `launcher` seam main backs with tui.Run; subcommands.go the one registration seam
// naming the children the shipped binary carries; wire.go the composition proper —
// runRoot walking four named phases over one wiring value, that wiring and the single
// teardown that ends it, the state-root resolution behind it, the narrow surfaces its
// seam files share, and the map naming them all.
//
// The phases runRoot walks, one file each (ADR 0043): wire_boot.go the facilities one
// run owns before a session exists — skills, Bridge, presentation ladder, Confiner —
// the base Config built from them, and what this host's confinement posture says for
// itself on stderr; wire_live.go the live-session assembly, from the MCP connections
// and the tool registry through the engine and Upstream holders and the bind that fills
// them to the config watcher and the out-of-band work; wire_verbs.go the composition
// root's own verbs — the rebind, the beat wrapper, and the three ways a session arrives
// on or records an Upstream; wire_options.go the projection of all of it onto
// tui.Options, the renderer's whole view of this host, and the two host capabilities
// that view names as interfaces rather than as bare funcs (ADR 0054).
//
// The seams those phases build, split off by concern (ADR 0043): wire_settings.go the
// live settings holder, the dispatcher a committed /settings key is applied through,
// and the per-model re-resolution a heartbeat rebind drives; wire_tools.go the live
// tool registry and the builders that assemble one — built-ins plus MCP;
// wire_mcp.go the connected MCP sessions
// and the validate-then-commit reconnect that moves a session onto another set;
// wire_present.go the presentation ladder this host can walk and the holder that
// rebuilds and re-installs it; wire_session.go the session-persistence and
// prompt-recall hosts plus the resume resolution a --resume/--continue start goes
// through; wire_engine.go Agent construction through the public surface and the
// late-bound engine that stands in until a server is picked; wire_server.go the entry a
// startup selection collapses to, the one step that binds any entry to a session, the
// config-change wait the reload chain parks on, and the [tui.ServerHost] the six Upstream
// acts cross to the renderer as; wire_firing.go the ONE composer every unattended run's
// Config comes out of — the inputs a Driver decides and the twenty fields it does not, so
// headless, a daemon Firing and a session's Schedule cannot read one configuration three
// ways (ADR 0031, ADR 0033).
//
// The config cluster is no longer here: the schema, the precedence, the key registry, the
// splice writer, the legacy fold and the [config.Options] the flags bind to all
// live in internal/config (ADR 0043) — a fact about the config file is not a fact about this
// Driver. The poll that reports the file changed sits one package over in internal/filewatch,
// which knows nothing about YAML and which the daemon's schedules watch shares (ADR 0041). This
// binary calls both packages and keeps only its own display projection of them.
//
// The /settings seams: settingsrows.go projects the key registry plus the values THIS
// run resolved onto the renderer's plain rows, masking what must never be shown;
// settingsedit.go is the `$EDITOR` round trip for the keys no row can express — the
// argv out, the list of changed keys back (ADR 0037 decision 5).
//
// The session's wiring: upstream.go the holder owning the CURRENT heartbeat Monitor and
// binding — so a `/server` switch is a composition-root move the renderer never sees —
// plus the server choices, the parallel-agents cap, and the hint-resolution observer
// behind the "model not advertised" notice; title.go the out-of-band
// session-naming completion behind [tui.Options.GenerateTitle]; validatedsets.go the
// startup ladder that decides what the Validated-set surface applies and says this
// session; modelprofile.go its per-model twin — which shape a model speaks the wire in
// and which tools it is offered, matched axis-wise out of the `model-profiles:` map and
// the shipped table, plus the two one-line notices that resolution produces: a built-in
// match announcing itself (ADR 0044) and a switch's non-empty roster deltas (ADR 0057);
// delegation.go the Sub-agent
// server — the second heartbeat on the entry `sub-agents-server:` names, the Delegation
// target each of its beats resolves for the engine to route spawns against, the one
// notice each change of routing state is worth, and the re-point a live `servers:` edit
// drives (ADR 0045);
// keymigrate.go the start-up key migration (ADR 0047) — which `servers:` entries still
// hold their API key in the file, the notice that goes out instead of an offer and the two
// reasons it carries — this machine has no secret store, or a headless run cannot prompt to
// offer one — and the consented move itself: store write, read-back through the very
// command about to be persisted, then the entry rewrite;
// launcher.go the only file
// importing the llama-launcher facade, kept behind
// the nil-degrading [tui.LauncherHost] the seven launcher acts cross to the renderer as
// (ADR 0029, ADR 0054); schedule.go the scheduler's three
// composition seams — what a Firing runs against, when it may start, where it narrates
// (ADR 0033); daemonfire.go that same composition for the daemon — the adopted
// name→Entry set a Firing resolves its schedule against, the `servers:` entry it binds to
// by name, and the unattended run composed over the pair (ADR 0034, ADR 0055).
//
// The subcommands: headless.go is `apogee headless`, one prompt run to completion with
// nobody watching, and the binary's only distinct exit codes; daemon.go `apogee daemon`,
// the standing process behind the schedules file — seed, single-instance lock, load,
// adopt, live-reload, shut down — plus the timestamped stdout log that is its whole user
// interface (ADR 0034); daemoninstall.go `apogee daemon install`, which renders the host
// supervisor's unit from an embedded template and prints the one command that activates
// it, generating for any of the three OSes from a plan the caller resolved;
// probe.go `apogee probe`
// and the free host report its bare noun prints (ADR 0021); probemodel.go
// `apogee probe model`, the half that spends live tokens and records a fingerprint;
// probeterminal.go `apogee probe terminal`, which measures the terminal by painting on
// it and reading the cursor back, with probeterminal_windows.go putting the console
// into the mode a bubbletea session runs it in and probeterminal_other.go its POSIX
// no-op twin.
//
// The platform helper: confined_exec_linux.go intercepts the __confined-exec sentinel
// before Cobra, so the landlock backend can confine a subprocess by re-invoking this
// binary, and confined_exec_other.go is the no-op twin everywhere else — the macOS and
// Windows backends need no helper (confinement-execution-contract §2.3 / §2.6).
//
// And doc.go this map.
package main
