# Bugfix note — streamed-reply crash is FIXED on main

**This is an out-of-band note to the session implementing P2.5, not a phase handoff.**

## What was broken

`apogee` panicked on **every multi-token streamed assistant reply** (e.g. "what skills
are available"):

```
strings: illegal use of non-zero Builder copied by value
  internal/tui/transcript.go:112  (*transcript).appendToken
```

Root cause: `transcript.pending` was a `strings.Builder` held **by value**. `Model` embeds
`transcript` by value and `Update` has a value receiver, so Bubble Tea copies the whole
Model — Builder included — on every `Update` (ADR 0011). A `strings.Builder` records a
self-pointer on first write and panics on the next write after the struct moves. The first
`TokenEvent` wrote the buffer; the second arrived on a later `Update` (a fresh Model copy)
and tripped `copyCheck`.

## The fix (commit `1baec7d` on `main`)

- `transcript.pending` is now a plain **`string`** (append with `+=`, clear with `= ""`),
  keeping `Model` a true deep-copyable value type.
- New structural guard `TestModelNoBuilderByValue` walks the Model's value-reachable type
  graph and fails on any value `strings.Builder` (the panic is address-dependent, so a
  behavioural test can't reliably catch it — that's why the existing lifecycle test stayed
  green while the app crashed every time).
- Package-wide invariant recorded in `internal/tui/doc.go`: never hold a `strings.Builder`
  (or any self-pointer no-copy type) by value anywhere the Model reaches.

Three files only: `internal/tui/transcript.go`, `internal/tui/model_test.go`,
`internal/tui/doc.go`.

## What you need to know (your working tree)

- `main` advanced under your uncommitted changes — rebase/account for `1baec7d`.
- `transcript.go` and `doc.go` in your working tree now **match HEAD** (clean).
- `model_test.go` was entangled: it held both my guard test and **your** in-flight
  snapshot-on-quit tests (`recordingSaver`, `newSavingModel`, `TestModelSavesOnCleanQuit`,
  `TestModelDoesNotSave…`, `TestModelQuitWithoutSaver`). I committed **only** my guard-test
  hunk. **Your snapshot-on-quit tests are intentionally NOT committed and remain in your
  working tree** — commit them with your P2.5 work.
- If any of your P2.5 code assumed `transcript.pending` was a `strings.Builder`, it's a
  `string` now.
