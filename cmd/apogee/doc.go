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
// disk, which server a run starts on, what a config key may contain, which of those a
// live session may change, and what an out-of-band call — a naming completion, a
// scheduled Firing — is built against. Each of them crosses to the renderer or the
// engine as plain data behind a seam, which is what keeps internal/tui a thin renderer
// (ADR 0011) and keeps internal/* off the root module path (ADR 0010).
//
// The files, one line each.
//
// Entry and command surface: main.go is the process entry — dispatch the
// __confined-exec sentinel, build the root, turn an error into an exit code; root.go
// the root command itself, its flag set, the [options] value the flags bind to, and the
// `launcher` seam main backs with tui.Run; subcommands.go the one registration seam
// naming the children the shipped binary carries; wire.go the composition proper —
// runRoot assembling roots, config, tools, MCP, sessions, recall and engine into a
// running TUI, plus the live-apply machinery a /settings edit or a heartbeat rebind
// drives (liveSettings, liveTools, liveMCP, livePresentation, settingsApplier, and the
// lateEngine that stands in until a server is picked).
//
// The config cluster: config.go the resolved [settings], the on-disk schema
// (fileConfig), and the flag > env > file > default precedence between them;
// registry.go the declarative table that describes every schema key exactly once,
// guarded as a bijection with fileConfig; defaults.go the embedded starter config
// seeded on first run; configwrite.go the textual splice writer that persists one key —
// or one host acknowledgement — without disturbing a comment; configmigrate.go the
// one-time fold of the retired top-level upstream keys into `servers:` (ADR 0036
// decision 9); configwatch.go the one-goroutine poller that reports the file changed,
// whoever changed it (ADR 0041).
//
// The /settings seams: settingsrows.go projects the key registry plus the values THIS
// run resolved onto the renderer's plain rows, masking what must never be shown;
// settingsedit.go is the `$EDITOR` round trip for the keys no row can express — the
// argv out, the list of changed keys back (ADR 0037 decision 5).
//
// The session's wiring: upstream.go the holder owning the CURRENT heartbeat Monitor and
// binding — so a `/server` switch is a composition-root move the renderer never sees —
// plus the server choices and the parallel-agents cap; title.go the out-of-band
// session-naming completion behind [tui.Options.GenerateTitle]; validatedsets.go the
// startup ladder that decides what the Validated-set surface applies and says this
// session; launcher.go the only file importing the llama-launcher facade, kept behind
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
