// Command apogee is the terminal coding agent for small local LLMs.
//
// It is the product binary and the composition root (phase-2 detail plan §3 C5): it
// resolves the state roots (no implicit ~/.apogee in the library — ADR 0001 / C7),
// builds a Config, constructs the Agent through the public apogee package (dogfooding
// the shipped surface), and hands it to the internal/tui renderer. The root command
// launches the TUI; the subcommands it also carries are assembled here, from the
// subcommands() registration seam (`apogee probe` and `apogee headless` today, phase-2
// detail plan §6).
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
// tui.Options, the renderer's whole view of this host.
//
// The seams those phases build, split off by concern (ADR 0043): wire_settings.go the
// live settings holder, the dispatcher a committed /settings key is applied through,
// and the per-model re-resolution a heartbeat rebind drives; wire_tools.go the live
// tool registry and the builders that assemble one — built-ins plus MCP — and validate
// the `mechanisms:` block against the catalogue; wire_mcp.go the connected MCP sessions
// and the validate-then-commit reconnect that moves a session onto another set;
// wire_present.go the presentation ladder this host can walk and the holder that
// rebuilds and re-installs it; wire_session.go the session-persistence and
// prompt-recall hosts plus the resume resolution a --resume/--continue start goes
// through; wire_engine.go Agent construction through the public surface and the
// late-bound engine that stands in until a server is picked; wire_server.go the entry a
// startup selection collapses to, the one step that binds any entry to a session, and
// the config-change wait the reload chain parks on.
//
// The config cluster is no longer here: the schema, the precedence, the key registry, the
// splice writer, the legacy fold, the watcher and the [config.Options] the flags bind to all
// live in internal/config (ADR 0043) — a fact about the config file is not a fact about this
// Driver. This binary calls that package and keeps only its own display projection of it.
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
// session; modelprofile.go its per-model twin — which shape a model speaks the wire in,
// matched out of the `model-profiles:` map and the shipped table, plus the one-line
// notice a built-in match announces itself with (ADR 0044); delegation.go the Sub-agent
// server — the second heartbeat on the `sub-agents:`-flagged entry, the Delegation
// target each of its beats resolves for the engine to route spawns against, the one
// notice each change of routing state is worth, and the re-point a live `servers:` edit
// drives (ADR 0045);
// launcher.go the only file
// importing the llama-launcher facade, kept behind
// the nil-degrading actuation seams (ADR 0029); schedule.go the scheduler's three
// composition seams — what a Firing runs against, when it may start, where it narrates
// (ADR 0033).
//
// The subcommands: headless.go is `apogee headless`, one prompt run to completion with
// nobody watching, and the binary's only distinct exit codes; probe.go `apogee probe`
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
