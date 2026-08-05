---
Status: accepted
---

# The settings surface persists one key per deliberate edit

## Context

`~/.apogee/config.yaml` is a **documented, hand-edited document the user owns**: seeded once from
the embedded template (`cmd/apogee/defaults/config.yaml`) and never overwritten again — an
invariant pinned by a test (`cmd/apogee/defaults_test.go:45`) and restated as a promise in the
README ("an upgrade never touches it") and in
[ADR 0023](0023-the-system-prompt-is-a-configured-template-rendered-per-request.md) §8. Two more
records draw the same line from the other side:
[ADR 0012](0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md)'s amendment
(2026-07-21) allows exactly **one** config write, `/confine off --save`, and only because it is a
distinct affirmative user act; [ADR 0021](0021-probe-is-two-halves-the-host-report-is-free-the-model-battery-is-an-explicit-act.md)
§5 refuses to write even a suggested `model-profile`, printing paste-ready YAML instead, because
"a probe produces *evidence*, and turning evidence into a preference is the user's move".

Against that, the config surface has outgrown a text editor: roughly forty leaf keys across three
resolution sources (flag over `APOGEE_*` env over file over built-in default), a schema whose
source metadata lives as parallel string literals inside `resolveSettings` (`cmd/apogee/config.go`),
and a file that — because seeding never re-runs — is missing every key added after the day it was
written. `ISSUES.md` carried the request ("a full screen menu for all available settings when
running the slash command `/settings`. This needs grilling."). A 2026-08-05 grill settled that
surface; this record is its ratification.

The constraints it lands inside: the TUI is a thin renderer over a worker engine
([ADR 0011](0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md) — no YAML in the
renderer, no no-copy types on the value Model), one width authority
([ADR 0030](0030-the-tui-has-one-width-authority-and-it-mirrors-the-painter.md)), one slash
namespace ([ADR 0027](0027-one-slash-namespace-with-inline-skill-tokens.md)), `layout.md`'s row
budget where every pane's rows come out of the transcript and the frame floors are non-negotiable,
and [ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)'s
door-keeping: nothing safety-relevant may exist only in one Driver's surface.

## Decision

**1. The surface is a full-height pane, not a screen takeover.** `/settings` opens a pane that
claims the **entire transcript row budget** — the transcript gives way completely — while the
status line, input box and footer (`layout.md`'s frame floor) stay drawn. It is neither an
alternate-screen takeover (which would put apogee's chrome and the user's scrollback out of reach
and need its own input plumbing) nor the eight-row picker window (too small for a forty-key
schema). The existing floors are untouched: a pane still gets its four-row minimum, no pane opens
below a twelve-row window, and **a pane that gives way entirely leaves its fact on the status
line** (`layout.md:184`). This is a new *pane class* — the first pane whose budget rule is "all of
it" — and `layout.md` documents it where the class is implemented.

**2. Persist per edit, through the comment-preserving splice writer.** Every committed edit is
spliced into `config.yaml` immediately: guided by parsed `yaml.Node` positions, applied as a
textual line splice, re-parsed and compared against the original `fileConfig` apart from the
target key, then written atomically (temp + rename). No re-marshal, so not one word of the user's
comments, key order or indentation is lost — the whole safety contract that `configwrite.go`
already carries for `unconfined-hosts` applies unchanged.

This **extends ADR 0012's authorized write set** from that single key to *registry-declared
editable scalars*, on the same reasoning that authorized the first one: a keystroke in a screen
the user opened, aimed at a row they selected, is the deliberate act the fence asks for — there is
no default-yes, no remembered "always", and nothing writes on apogee's own initiative. The
amendment's obligation travels with the permission: **each write names the file it changed and the
entry it set**, exactly as `/confine off --save` does.

**3. v1 edits simple keys only.** Bools, ints, strings and enums are editable. Structured blocks —
`servers`, `mcp-servers`, `mechanisms`, `validated-sets`, `unconfined-hosts`,
`system-prompt-models`, `context-files.names`, `model-profile`, and the `system-prompt-text` /
`system-prompt-file` entries — render **read-only with an "edit in `config.yaml`" pointer**. A
list/map editor is a different design problem (ordering, deletion, nested validation, multi-line
text) and shipping a half-built one over a file the user hand-edits is worse than pointing at the
file.

**4. A declarative key registry is the screen's source of truth, and the schema's.** One table in
`cmd/apogee` carries a row per config key — path, kind, default, env-var and flag names,
global-only, restart-required, editability, masking, one-line description — and **both** the pane
and `resolveSettings`' multi-source precedence read their metadata from it. A reflection guard
asserts a **bijection between registry paths and `fileConfig`'s yaml tags** (structured rows
terminate descent), so a key added to the schema without a registry row fails the build's tests
rather than silently going missing from the screen. Drift between a settings UI and its schema is
the standard failure mode of hand-maintained settings screens; this closes it mechanically.

**5. An inserted key lands below its commented example.** When a key has no active line, the
splice finds the `# key: …` example block the seeded template documents it with and inserts the
active line **directly after it**, so the value the user set sits under the prose explaining it.
Append-at-end is the fallback when no example matches (an older or heavily edited file); a nested
key whose parent mapping is absent gets the parent line created.

**6. Reset-to-default deletes the key's active line.** Reset is not "write the default value" —
it removes the line through the same verified splice, so the file returns to *not expressing an
opinion* and the built-in default (or a future changed default) governs again. When deleting the
last active child of a parent mapping, the now-empty parent line goes too: an empty mapping key
would change the parse.

**7. `/settings` is idle-only and not recalled.** `whileRunning: false` (the default command
policy) — a live turn holds resolved settings, and editing the file underneath it would invite the
belief that the running turn changed. `noRecall: true`, the `/new` and `/clear` precedent for
pure-UI verbs.

**8. Confinement stays single-homed in `/confine`.** `confine-to-workspace` and
`unconfined-hosts` render **display-only** in the pane with a "use `/confine`" pointer. ADR 0012's
acknowledgement interlock — a distinct affirmative act, session scope offered ahead of
persistence, wording that states the blast radius and never reads as repairing a malfunction — is
a *flow*, not a value, and a second entrance to it would be a second place for that wording to
drift out of date. The pane can loosen nothing.

**9. `mode` is the only live apply; the pane never rebinds.** After a successful write, `mode`
also applies to the running session through the existing `SetMode` seam the Shift+Tab handler
already uses, and shows no marker. Every other edited row renders a **"(next launch)"** marker,
and a row whose value is currently overridden by an env var or flag says so instead of pretending
the write took effect this run. Live model/server switching stays with `/model` and `/server`
(ADR 0024/0028's beat-completes-the-move machinery): editing `endpoint` or `model` here
**persists only**. Honest markers beat a settings screen that quietly does half of what it looks
like it did.

**10. `api-key` is masked in the list, visible while editing.** `••••` on the row (the pane is
on-screen in a terminal that gets shared and screenshotted); the characters show while the user is
typing them, because a masked edit buffer is unusable.

## Considered options

- **Boot-time auto-sync of missing config keys into an existing `config.yaml` — REJECTED, not
  deferred.** The idea (an upgrade appends newly added keys, commented or active, to the user's
  file) motivated the original request, since a config seeded long ago silently lacks every later
  key. It is refused on grounds that are already ratified: it contradicts ADR 0023 §8's
  "an upgrade therefore changes nothing for anyone already running", `seedConfig`'s pinned
  never-overwrite invariant, the README's "an upgrade never touches it", and ADR 0021's
  evidence-not-preference posture — and it is precisely a write **apogee initiates**, the one class
  ADR 0012's fence exists to forbid. Decisions 4 and 2 dissolve the motivation instead: the pane
  renders the **whole schema from the registry**, so a key absent from the file still appears with
  its default and its documentation, and the first edit inserts it (below its example, decision 5).
  Discoverability was the real need; rewriting the user's document was never the only way to meet
  it. (Swept before rejecting: no such code exists in the repo, and `git log -S` finds none ever
  removed — nothing is being taken away.)
- **A full-screen (alternate-screen) takeover** — rejected: it drops apogee's status line and
  input box, orphans the transcript the user was reading, and needs its own key plumbing for
  everything the frame already provides. A pane that takes all the transcript rows delivers the
  same reading area with none of that.
- **Reusing the eight-row picker window** — rejected: forty keys through an eight-row viewport is
  a scroll-hunting exercise, and the picker's shape assumes a short flat list.
- **Batch the edits and save on close (or behind an explicit Save action)** — rejected: it invents
  a dirty-state model (what happens on esc, on a crash, on a second `/settings`), and it makes the
  write a *bulk* rewrite whose blast radius is the whole file rather than one named line. Per-edit
  writes keep every write small, individually named, and individually reversible.
- **Unmarshal → mutate → re-marshal through yaml.v3** — rejected for the reason
  `configwrite.go`'s header already records: it hands the user back a file with a couple of
  settings in it, having silently deleted every word of documentation they started with.
- **Structured-block editors in v1** (lists, maps, the multi-line system-prompt text) — rejected
  for scope; the read-only pointer is honest about where that editing happens today.
- **Editing `confine-to-workspace` / `unconfined-hosts` from the pane** — rejected per decision 8.
- **Live-applying `endpoint` / `model` edits by triggering a rebind** — rejected: it makes the
  settings pane a second, less-informed entrance to the rehoming flow ADRs 0024/0028 own, where
  `/model` and `/server` already carry the beat-completes-the-move semantics.
- **Reset by writing the default value explicitly** — rejected: it freezes today's default into
  the user's file, so a future changed default would not reach a user who once pressed "reset";
  deleting the line preserves the "no opinion expressed" state.
- **A read-only settings *viewer*** (render everything, edit nothing) — rejected: it is most of
  the work (registry, rows, pane, budget rule) for none of the payoff, and the write fence permits
  the deliberate edit, so declining it would be caution theatre. It survives as the first
  implementation step, not as the destination.
- **Rewriting the typed `layer`/`settings` copy chain into full table-driven resolution now** —
  rejected for scope (the 2026-08-01 architecture-review follow-up). The registry plus its
  bijection guard is the step toward it; the rewrite is its own effort.

## Consequences

- ADR 0012's authorized-write set is now "`/confine off --save`, plus registry-declared editable
  scalars edited in the settings surface" — every member of it still a distinct affirmative user
  act that names the file and entry it changed. Nothing apogee decides on its own initiative may
  join that set without amending this record.
- `CONTEXT.md` gains **Settings surface** as a canonical term, and it carries the reconciliation:
  the standing "apogee never writes your config" claims mean never **unprompted**. A
  settings-screen edit is user-initiated and names its entry; probe still prints rather than
  writes (ADR 0021 untouched); seeding still never overwrites (ADR 0023 §8 untouched).
- The key registry becomes the single home for config **source metadata**, and the bijection guard
  makes "the schema grew a key" a test failure rather than a screen that quietly omits it. It is
  also the groundwork the deferred table-driven-resolution rewrite will build on.
- `layout.md` gains a **full-height pane class** with its own budget rule, joining the frame's
  give-way order — the first surface allowed to take the transcript's entire budget.
- Most edits are honest about applying at **next launch**. That marker is the visible cost of
  decision 9, and it is the correct cost: the alternative is a screen that appears to change a
  running session and does not.
- The seeded template stays the documentation of record for every key (the registry's descriptions
  are condensed from it), so template comments and registry rows must be kept in step by whoever
  adds a key — the guard pins the *paths*, not the prose.
- `ISSUES.md`'s settings-screen entry closes: the grilling it asked for is this record.
