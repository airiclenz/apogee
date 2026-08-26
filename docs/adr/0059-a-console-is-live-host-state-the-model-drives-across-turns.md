---
Status: accepted
---

# A Console is live host state the model drives across Turns, and its tools split along the classification line

## Context

The 2026-08-16 tool-surface poll round's single top pick from Qwen3.8-27B was a persistent
terminal — attach to a running dev server, drive a REPL, send keystrokes to an interactive
program — recorded in `docs/design/tool-surface-findings.md` as needing a grill because it
collides with two settled contracts. [ADR 0008](0008-stateless-tools-and-non-forkable-external-effects.md)
makes tools **stateless across Turns** so the bench can fork a run at the quiescent boundary,
and names `terminal`/`python_exec` one-shot by contract. The confinement execution contract
(§2.4) makes every subprocess run a container the call tears down on every exit path. A
persistent process is exactly what both forbid. The first round's `concurrent terminal`
denial does not cover it — that was parallelism (ADR 0039); this is interactivity. A grill on
2026-08-25 settled the shape; this record is that ratification.

## Decision

**1 — A Console is live host state, never Session state.** A Console — a persistent
interactive program the model opens and drives — lives until an explicit close, `/new`, a
session restore (`/sessions`), or engine exit. It is the same class as the undo journal
(ADR 0022 §8, ADR 0051): per process, dies with it. It is **not** an exception to ADR 0008's
forkability clause but an instance of its resume clause — "production resume reconnects fresh and makes no server-side-state promise":
a snapshot, fork, or resume inherits **no** Console; a later send/read on a handle the process
no longer holds returns "console gone", and the model reopens it. A bench task that needs a
Console falls out of the frozen discriminating suite the way a live-network task does
(ADR 0009). ADR 0008's stateless-across-Turns clause is amended to name this one deliberate
exception; `terminal` and `python_exec` stay one-shot.

*Rejected: Exchange-lived* — torn down at the final no-tool reply. Simpler boundaries, but a dev
server opened in one Exchange is dead by the next user message, which is the headline use case.

**2 — Four tools, split along the classification line.** Classification is per **tool**, not
per action (contract §4, amended 2026-07-26: a marker is a structural fact, and a self-declared
per-call read-only can never outrank it). So the family is `console_open` and `console_send`,
which carry the Subprocess marker — Confined in Auto with confine-to-workspace on, the
caps-insufficient `gate` fallback, gated on the middle rungs, refused in Plan — and
`console_read` and `console_close`, which sit on the read-only floor: polling a dev server never
prompts in Ask-Before, and both are admitted in Plan (a Console cannot be *opened* there, but one
left from before the switch can be drained and closed).

*Rejected: one `console` tool with an `action` enum* — one roster slot, but the whole tool rides
the subproc row, so every poll gates in Ask-Before and a REPL loop becomes an approval per step.
Making poll gate-free would need per-call classification, which the contract deliberately does
not have. *Rejected: three tools with read folded into send* — reading as "send with empty
input" is a parameter-level trick small models miss (method lesson 2: discovery is by name).

**3 — The family ships default-off, profile-enabled.** ADR 0057 decision 3 built the
default-off state for exactly this case and no tool used it yet; this is the first. The ask came
from above the small class, and four slots on every small model's menu with no bench evidence
violates the roster rule. Shipped profiles for the models that asked enable it; global
`tools.enabled:` lifts it everywhere.

**4 — Each send is its own Resolution; a mode or `/confine` change never touches the live
process.** The fence set at open (landlock / seatbelt pre-`execve`) is a property of the process
and cannot change — the same stance as the ratified `/confine` boot-fence call (2026-08-25).
Every `console_send` runs through `resolve()` under the mode at that moment: a downgrade to
Ask-Before makes sends gate, Plan refuses them, read/close stay admitted. *Rejected: kill all
Consoles on a downgrade* — throws away the server the human just asked to keep working with and
buys no safety the per-send resolution does not already give.

**5 — The kill-on-denial watch applies.** A Confined Console is wired through
`platform.DenialKillWriter` like every other Confined subprocess run (ADR 0056 §2): the first
streamed OS-denial signature kills the Console's process group, and the next `console_read`
reports the definitive blocked label with the buffered output. A program that hit the fence
otherwise keeps running its next lines — the half-done-state class the watch exists to stop. A
divergent rule for one funnel was rejected; a miss still leaves the fence intact.

**6 — Delegation-scoped ownership, a fixed cap, no idle TTL.** A delegation's end closes the
Consoles it opened: its result is text, and a live orphan is not a result (the one-shot spirit of
contract §2.4's teardown-on-every-path). Top-level Consoles live per decision 1. A Console is
addressable by the run that opened it and by no other: a sibling or a parent delegation naming its
id is refused as if the id did not exist. The engine holds
at most a fixed number of open Consoles (a constant, not a config knob); there is no idle
timeout — a dev server idles by nature. *Rejected: engine-wide handles with an idle TTL* — an
orphaned child process and a dead server mid-Exchange are both worse failure modes.

## Bounds (stated, not separately ratified)

- `console_open` takes a command line run under the platform shell inside a pseudo-terminal,
  with `terminal`'s environment scrub and PATH scoping (`subprocessEnvScopedPath`) and the
  `argv[0]`-inside-the-box refusal; **no** fail-fast preamble (the shell is interactive).
- Output is a bounded ring buffer per Console; `console_read` returns the bytes since the last
  read, capped like `terminal`'s output, plus whether the process is still alive and its exit
  code once it is not. Sizes and the settle/wait parameters are plan-level.
- Windows: ConPTY creates the process itself and cannot take the restricted token, so the
  contract's "confine if you can, gate if you can't" row makes `console_open` gate in Auto there
  — a consequence of the existing table, not a new cell. The Job Object container (§2.4) still
  applies.
- The bench needs no stub: a Console is not an `ExternalEffectTool`; a fork simply inherits none.

## Consequences

- ADR 0008 gains a dated amendment pointer; its `Tool` interface clause reads "stateless across
  Turns, except a Console, which is live host state and never survives a snapshot".
- CONTEXT.md gains **Console** under *Safety and autonomy*.
- `docs/design/tool-surface-findings.md` closes its "persistent terminal / PTY session" grill
  line against this record.
- A saved implementation plan follows (house format, `docs/plans/`); nothing ships from this
  record alone.
