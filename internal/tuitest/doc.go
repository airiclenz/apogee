// Package tuitest is apogee's TUI driver kit: the machinery a `go test` run uses to launch the
// real terminal UI, type into it, and assert on what a terminal would actually have shown.
//
// It exists because "manual" was, for most of apogee's release checklist, not a judgement about
// the claim but an admission about the tooling: there was no way to look at the screen. ADR 0062
// settles what a look is worth — a claim about the screen is settled against CELLS an emulator
// laid out, never against a View() string, a substring of the raw bytes, or a screenshot nobody
// diffs. github.com/charmbracelet/x/vt is that emulator, and it is the frame authority for both
// drivers: the in-process one, which runs the whole composition inside the test binary, and the
// black-box one, which drives the shipped binary through a pty. Whatever produced the bytes, the
// picture is reconstructed the same way and asserted the same way.
//
// The drivers are Drivers in the ADR 0031 sense. They enter through the composition seam —
// cmd/apogee's launcher and internal/tui.Build — and add no test-only hook to the engine, the
// provider or the tools. What a driver can see is what any host of the engine can see.
//
// The file map:
//
//   - doc.go — this narration.
//   - frame.go — [Frame], [Cell], [Run] and [Style]: one immutable snapshot of a terminal, and the
//     cell/colour primitives every screen assertion is written against.
//   - screen.go — [Screen]: the emulator behind a mutex, the renderer's [io.Writer], the terminal's
//     own answers ([Screen.Answers]), and the settle and flicker counters.
//   - wait.go — [WaitFor] and its shorthands: bounded polling with the failing frame printed plain
//     and styled. A driver test never sleeps.
//   - golden.go — [Golden]: byte-for-byte frames under testdata/frames/, with redactions so a
//     golden does not churn, and -update to record them.
//   - leak.go — [CheckLeaks]: the guard that a driver test's goroutines stop when it does.
//   - driver.go — [Driver]: the in-process terminal. It hands a program its input, its output and
//     the terminal's own answers, and the test its keys, its resize and its quit.
//   - keys.go — [Key]: the byte sequences a terminal sends for the keys apogee binds.
//   - pty.go — [PTYDriver]: the black-box terminal. It starts the SHIPPED binary under a real
//     pseudo-terminal and reads back what only a real terminal knows — colour, window size, a
//     pid, and the state the terminal was left in.
//   - pty_windows.go — the Windows stand-in for it: the type, and a skip in every method.
//
// Documented in docs/design/test-drivers.md; the "which driver observes which claim" table there
// decides whether a test step may be manual at all.
package tuitest
