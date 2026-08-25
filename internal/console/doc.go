// Package console holds the process mechanics behind a CONSOLE: a persistent interactive
// program — a REPL, a dev server, a shell — running under a pseudo-terminal that a model drives
// across Turns instead of restarting per tool call (ADR 0059).
//
// Why a pseudo-terminal rather than pipes. An interactive program decides what it is by asking
// whether its output is a terminal: a REPL prints its prompt and echoes what was typed, a shell
// turns on job control, a test runner keeps its progress line. Behind a pipe all of that
// disappears and the program often buffers its output until it exits, which is exactly the
// output a Console exists to hand back mid-run. So the child gets a real terminal — a fixed
// 160x40 window, since no human is looking at it — and the escape sequences it paints with are
// stripped on the way out, leaving the text a human would have seen.
//
// The shape of the thing. One [Console] is one [Process]: a command, a terminal, a goroutine
// draining that terminal into a bounded [ring] of unread output, and a goroutine reaping the
// exit. Reads are drain-on-read and may wait for the first new bytes, which is what lets a tool
// collect a window of output without polling; when the buffer overflows the OLDEST bytes go and
// the count of them is reported, so the model learns its output has a hole.
//
// Teardown is by process GROUP, on every exit path. The child is started as a session leader, so
// its group id is its pid and a kill of the negative pid reaches everything it spawned; the kill
// runs when a Console is closed, when its context is cancelled, AND after a clean exit, because a
// command that backgrounded a child and returned still leaves that child holding the terminal
// (confinement execution contract §2.4). A descendant that deliberately left the group with a
// setsid of its own is outside that reach — the same accepted residual the one-shot subprocess
// path documents.
//
// How many, and whose. Above the process sits a [Registry]: the set of Consoles one engine holds,
// each under a small id that is issued in order and never reused, so a stale id in a model's
// context cannot come back pointing at a different process. It is live host state — built with the
// engine, shared by pointer with every delegation, never written into a session — and a tool call
// reaches it through the context seam rather than by holding it, because tool instances are
// rebuilt when the roster changes mid-session while the processes must not be. The registry is
// also where a Console's life ends: it caps how many are open at once, closes the ones a
// delegation opened when that delegation ends, and closes every one of them at /new and at exit
// (ADR 0059 §1, §6).
//
// What this package deliberately is NOT. It does not confine anything: the caller fences the
// command and hands it in through [Spec.Prepare], and all this package does with that fact is
// put the kill-on-denial watch on the output path when [Spec.Confined] says the command was
// confined (ADR 0056 §2). It knows nothing about tools, models or the engine's exchange — it
// imports internal/platform and the pseudo-terminal dependency and nothing else, which is what
// keeps the file boundary at the process; an owner is an opaque string it matches and never reads
// meaning into. Windows has no backend here yet: the build-tag pair keeps the whole exported
// surface and [Start] returns [ErrUnsupported], because the tools above it are registered on
// every platform.
//
// Files:
//   - doc.go — this map and the package's rationale.
//   - process.go — the POSIX Process: start under a pseudo-terminal, read, write, kill, reap.
//   - process_other.go — the Windows stand-in, whose Start reports ErrUnsupported.
//   - ring.go — the bounded drain-on-read buffer of unread output.
//   - ansi.go — stripping the terminal control sequences out of what the model reads.
//   - registry.go — the engine's open Consoles: ids, owners, the cap, closing by owner.
//   - context.go — the context seam a tool call reaches the registry through.
package console
