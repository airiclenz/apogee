# Diagnosing a host — `apogee probe`

`apogee probe` answers "what would Auto do on this machine?" without running an agent.
It reads `config.yaml` and the `APOGEE_*` environment exactly as a session would, and
reports the OS/arch, the confinement backend and what it can *actually* enforce here,
the Auto verdict, the effective `confine-to-workspace` after any host acknowledgement,
the workspace root and config home, and whether the configured endpoint answers
(`/v1/models`, plus llama.cpp's `/props`). It is free, offline and **read-only** — no
model is called, no starter config is seeded, nothing is written. `apogee probe host`
is the same report under a named child, for scripts.

```console
$ apogee probe
apogee probe — host report
  (no agent runs, no model is called, nothing is written)

host
  os/arch:       windows/arm64
  ...
confinement (ADR 0012)
  backend:       token (fs-write: available · network: unavailable)
  auto:          eligible — the backend can fence terminal commands, so auto runs them confined
```

The `backend:` line gains a third field where the fence is real but incomplete —
`landlock (fs-write: available · network: unavailable · unfenced: truncate(2))` — naming each
access the backend knows it cannot cover on this host. On Linux that is truncation on a kernel
older than 6.2 (landlock ABI 1–2: Ubuntu 22.04, Debian 12, RHEL 9), where a confined command
cannot create or write outside the workspace but can still *empty* a file that is already there.
Auto stays eligible; the field exists so the report never claims a fence it does not have.

`apogee probe model` is the other half, and it is deliberately an **explicit act**
rather than something the bare noun triggers, because it costs live model calls *and*
writes. It runs a three-part capability battery — a native tool call, JSON/structured
output, and a multi-step tool chain — then prints what it observed, an ordinal
capability tier, and the `model-profiles:` entry the findings suggest — keyed by the
model it probed, and paste-ready as YAML
(your `config.yaml` is never edited). It also records a **behavioral fingerprint**: the
model keeps its advertised name — probing never renames it, so Validated-set entries,
aliases and Library observations keyed on that name keep matching — but its identity
rises from *low* to *medium* confidence, which is what promotes a matching Validated
set from offered to auto-applied on later runs. `--no-save` runs the whole battery and
records nothing; the record's path is printed either way, so deleting that file undoes
it.

`apogee probe terminal` is the third subject, and it is free like the host report. It
**measures** the terminal instead of trusting it: it writes real escape sequences to your
terminal and reads the answers back, then prints what it found — how it answers about
synchronized output and grapheme clustering (modes 2026 and 2027), how many cells it
really advances for an emoji or a combining sequence with that mode off and on, where its
tab stops are and whether a tab erases what it passes over, what happens when a glyph
lands in the last column (a pending wrap or an immediate one), and the capabilities it
actually has beside the ones apogee's renderer assumes from `TERM`. A section whose answer
disagrees with what was assumed is marked `MISMATCH` and
sets the exit status, so the report can be checked by a script and not only read. It needs
a real terminal on both stdin and stdout — a redirect or a pipe leaves nobody to answer —
and it calls no model and writes nothing.

```console
$ apogee probe terminal
apogee probe terminal — measured, not assumed
  (nothing is written; the screen is restored)

  size:          120 columns × 30 rows
  TERM:          (unset)
  ...
last-column wrap
    step                    cursor (CPR)  console API  deferred wrap would be
    wrote the final column  6,120         6,120        6,120 (pending)
    wrote one more          7,2           7,2          7,2
  OK — the terminal holds a pending wrap at the last column — the semantics the renderer emits against
```

All three reports are printed with terminal control characters and bidi overrides removed: a
server you are probing *because* you distrust it — or a terminal that answers the measurement
with escape sequences — must not be able to repaint the diagnostic that judges it.

**When a frame comes out wrong**, two hidden flags on `apogee` itself —
they sit on the root command, not on `apogee probe` — record the evidence a rendering bug is
argued from — `--tui-trace <file>` writes the exact bytes the renderer emitted, one quoted
string per write, so a corrupted frame can be replayed rather than only described, and
`--tui-diag <file>` writes what the terminal told apogee about itself: the environment the
renderer read, the width method it started on, the window size, the colour profile, and
every mode report the terminal sent — each written once and again only when it changes, so
the file stays short enough to paste into a bug report.
Both default off and cost nothing unless you name a file, both work on every OS, and
neither appears in `--help`: they are for a bug report, not for a session. `⌃l` is the
in-session counterpart — it forces a full repaint and is usually all a smeared frame needs.

