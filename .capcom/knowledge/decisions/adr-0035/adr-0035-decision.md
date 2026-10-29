---
title: ADR 0035 decision
---

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
`cmd/apogee` *(amended 2026-09-30: now `KeyRegistry` in `internal/config/registry.go`, moved by
ADR 0043 decision 3; its restart-required
column retired with ADR 0037 decision 8)* carries a row per config key — path, kind, default, env-var and flag names,
global-only, restart-required, editability, masking, one-line description — and **both** the pane
and `resolveSettings`' (since 2026-08-10 `resolveOptions` in `internal/config/config.go`)
multi-source precedence read their metadata from it. A reflection guard
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
typing them, because a masked edit buffer is unusable. (Amended 2026-10-29: since ADR 0036 retired
the top-level `api-key:` on 2026-08-05 no row is masked — the one secret is a `servers:` entry's
api-key inside a structured block. The `Masked` column survives unused, and a masked row's edit
buffer starts empty, so the secret is typed whole.)
