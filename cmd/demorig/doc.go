// Command demorig is the developer-facing tool behind graphics/demo: it records a clip from its
// storyboard (`graphics/demo/storyboards/<clip>.yaml`), judges the take, and renders the shipped
// GIF from it, so a re-record is a repeatable loop rather than a hand-tuned session.
//
//	demorig lint graphics/demo/storyboards/hero.yaml
//	demorig record graphics/demo/storyboards/hero.yaml [--work <dir>]
//	demorig capture graphics/demo/storyboards/hero.yaml --upstream <url> [--key-env <VAR>] [--work <dir>]
//	demorig check graphics/demo/storyboards/hero.yaml [<take>] [--stage <dir>]
//	demorig render graphics/demo/storyboards/hero.yaml [<take>] [-o out.gif] [--dry-run]
//
// `lint` checks the storyboard against its schema, printing every problem and exiting 1 on any.
// `record` resets the rig's stage, replays the storyboard's cassette as the model, runs apogee in
// a pty through every beat, writes <work>/<clip>.take and checks it; `capture` does the same
// against a live model behind a recording proxy and saves the cassette. Both record on unix only.
// `check` judges a take by the session it saved, the screens it recorded and the stage repo:
// every expect as a PASS/FAIL row, exit 1 on any FAIL. `render` lays the take's beats onto the
// storyboard's section durations, rasterizes and composes every frame — zoom and click cursor
// included — and encodes the GIF through ffmpeg, then gifsicle when it is on PATH. A take
// argument left off defaults to <work>/<clip>.take, the file `record` writes.
//
// It is a dev tool, not a release asset: `make demorig` builds it, and `make dist` does not
// ship it.
//
// # Files
//
// The commands:
//
//   - main.go — the root command, `lint`, and the exit-code mapping a script driving the binary
//     reacts to.
//   - check.go — `check`: the PASS/FAIL table a take is judged by, one row per expect plus the
//     stage row.
//   - render.go — `render`: a take to the shipped GIF, through ffmpeg's palette encode and
//     gifsicle when it is on PATH.
//   - record.go — `record` and `capture` on unix: the rig's stage reset, the cassette replayer or
//     recording proxy the take's model answers through, apogee launched in a pty, and how a take
//     ends it.
//   - record_windows.go — `record` and `capture` on Windows: the same command lines, refusing to
//     run because recording needs a unix pty.
//
// The storyboard and the take:
//
//   - storyboard.go — the Storyboard a clip is defined by (graphics/demo/README.md,
//     "Storyboards"), its beats, and Load, the validation every storyboard passes through.
//   - take.go — the Take a recording produces: every distinct screen snapshot with its time, and
//     the event log of what the rig did.
//   - term.go — the unix Terminal a take records in: a pty pair, the vt emulator reading it and
//     the snapshots it emits.
//   - term_windows.go — the Windows stand-in: TermOptions and Terminal as types only, every
//     recording entry point answering errNoPTY.
//   - engine.go — the Engine that drives a beat's actions into the terminal, paced, and locates
//     a target's cell box on screen by the frame's chrome glyphs.
//   - typing.go — the humanized-typing profile ported from graphics/demo/type.sh, with its
//     seeded MINSTD generator.
//
// Judging a take:
//
//   - anchors.go — the entry selectors an expect uses to locate a transcript entry, and the
//     located index an ordering expect compares.
//   - session.go — loadEntries: a saved session record decoded into the committed transcript
//     entries the anchors match against.
//
// Rendering a take:
//
//   - schedule.go — the Schedule that lays the take's beats onto the storyboard's section
//     durations, holds and cuts, warning when a section plays too fast.
//   - raster.go — the Rasterizer: a snapshot's cells to an image in Source Code Pro with the
//     Noto fallback chain, box-drawing glyphs drawn rather than looked up.
//   - compose.go — the Compositor: each rasterized frame zoomed onto its target and overlaid
//     with the click cursor.
//
// And doc.go this map.
package main
