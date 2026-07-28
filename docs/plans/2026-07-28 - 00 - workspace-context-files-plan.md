# Plan — Workspace context files (AGENTS.md into the system prompt)

**Date:** 2026-07-28
**Status:** READY (grilled with the owner 2026-07-28 — see *Owner decisions*; the mechanical
design below is grounded against the working tree at `3893639`).
**Source:** owner request 2026-07-28 — "make certain files part of the initial system prompt
when a new session is started (the way CLAUDE.md is consumed by Claude Code), configurable
on/off plus a list of file names, default AGENTS.md".
**Track:** post-`v0.9.2`. One `### Added` CHANGELOG entry under `## [Unreleased]`; rides the
next minor cut (`Config` gains one **additive** field ⇒ minor; this plan bumps nothing).
**Public API:** `apogee.Config` (= `domain.Config`) gains ONE additive field,
`ContextFiles []string` (the resolved name list; nil ⇒ feature off). `internal/domain` gains
the small report types `ContextFileNote` / `ContextFilesReport`, and `Agent` gains one
read-only method `ContextFilesReport()`. The TUI's `Engine` interface
(`internal/tui/tui.go`) grows the matching method. No exported name changes (ADR 0010).
**Standing requirement:** `/coding-standards` (Go + testing variants) mandatory — invoke
`implement-plan` with `coding-standards` forwarded. Pre-production: commit direct to `main`,
no PRs (owner directive).

Per-item green gate:

```
gofmt -l .                                              # empty
make check                                              # vet + lint + go test -race -count=1 ./...
GOOS=windows go build ./... && GOOS=darwin go build ./...
```

**Dependencies.** Strictly linear: 1 → 2 → 3 → 4 → 5. Item 1 is the loader + engine cache
(inert — nothing reads it onto the wire yet); 2 seeds the content into the first system
message (feature exists for Go embedders); 3 plumbs the `context-files:` config block
(feature exists for config users); 4 surfaces the session notice and the Budget warn; 5 is
the documentation close-out. `/implement-plan` may stop after any completed item and the
tree is coherent.

**Deviations leave a trail.** Any authorized deviation from an item's text must land as a
dated `NOTES:` line under that item's heading in this file, per the sub-agent templates.

**Authoritative sources**, in precedence order, for every item:

1. This document.
2. `internal/agent/promptseam_test.go:104-129` — `TestPromptSeam_NativeProfileByteIdentical`:
   with no prompt AND no context files the request must stay byte-identical. It must pass
   UNTOUCHED through every item.
3. ADR 0023 (`docs/adr/0023-…`) — the system-prompt seam this feature rides: seeding at
   `buildRequest` position 0, one merged system message (prompt → mechanism directives →
   tool block), per-request projection that never enters history or the snapshot.
4. `cmd/apogee/config.go` — the `validated-sets:` block (`Enable *bool` at `:644`, the
   file-only layer copy at `:420-421`, the defaults at `:400`) is the precedent the new
   config block copies exactly.
5. `internal/context/budget.go:28-83` — the Allocation split (`systemPromptFraction = 0.15`
   of working room) the oversize warn reuses; `internal/agent/loop.go:773` (`budget()`)
   exposes it per-Agent as `domain.Budget.SystemPrompt`.

---

## Owner decisions (grill, 2026-07-28)

- **List logic — all found, in list order.** Every listed name that exists in the workspace
  is included, each under a header naming the file. The list is not a priority chain.
- **Lookup scope — workspace root only.** Names resolve against `Config.WorkspaceDir` only:
  no parent-directory walk-up, no global `~/.apogee` context file (global standing text is
  what `system-prompt-text/-file` already are).
- **Config shape — a `context-files:` block**, config-file only (no flag, no env), living in
  the system-prompt region of `config.yaml`:

  ```yaml
  # context-files:
  #   enable: true          # default: true
  #   names: [AGENTS.md]    # default; looked up in the workspace root
  ```

- **Read timing — every new session.** Files are read at construction AND re-read when
  `/clear`|`/new` starts a fresh session (and on a `/sessions` restore — the resolved-live
  posture of ADR 0023 §6: cfg is not serialized, a resumed session gets the CURRENT
  standing content). Mid-conversation edits never swap content under a running session —
  KV-cache stability and coherence.
- **Oversize — notice + warn, threshold = the Budget's own SystemPrompt share.** The
  session notice names each loaded file with its size; a warning line fires when the
  estimated tokens of the standing system content (rendered prompt + context files) exceed
  `Budget.SystemPrompt` — the Allocation share that already exists (15% of working room ≈
  12% of the total window at the default reserve). No new constant, never a cap, never
  truncation. Window unknown ⇒ zero Allocation ⇒ no warn (the notice still shows sizes).
- **Placeholders — content is data, verbatim.** File content is NEVER run through
  `prompt.Validate`/`Render`: `{{anything}}` in a repo's AGENTS.md passes through untouched
  and can never fail apogee's startup. Consequence: content is carried beside the template
  (the engine cache), never concatenated into `Config.SystemPrompt`.
- **No-prompt case — files seed alone.** Context files are an independent source: found
  content seeds a system message even when no prompt template is configured. The zero-byte
  native anchor narrows to "no prompt AND no context files".
- **Sub-agents inherit** the content (ADR 0023 §7's posture): `newChildAgent` children carry
  the SAME bytes the parent's session loaded — copied, not re-read.
- Settled by the grill's framing, stated for the record: missing files are skipped silently
  (the default name will not exist in every repo); a present-but-unreadable file is skipped
  LOUDLY (a notice, never a startup failure — discovery-based, unlike the config-named
  `system-prompt-file`); empty/whitespace-only files are skipped (no header for nothing);
  per-model prompt selection is orthogonal (context files ride whichever template is
  selected, and a model rebind leaves the cache untouched); Bypass does not disable them
  (standing config content, not a Mechanism); content never enters history or the session
  snapshot (per-request projection, ADR 0023 §6).

---

## The ground (verified 2026-07-28 against the working tree)

**Seeding site.** `internal/agent/loop.go:569-571` (`buildRequest`): `if sys :=
a.systemPrompt(); sys != ""` prepends the ONE `RoleSystem` message. `systemPrompt()`
(`loop.go:592-601`) renders `a.cfg.SystemPrompt` through `prompt.Render` with live inputs.
Mechanism directives (`AppendToSystem`, `internal/domain/hooks.go`) and the wire seam's tool
block (`internal/agent/wire.go` → `injectSystemInstructions`) both append to the FIRST
system message, so anything seeded at position 0 stays one merged message —
`TestPromptSeam_AppendsToSeededSystemMessage` (`promptseam_test.go:134`) pins the shape.

**Session boundaries.** `/clear` and `/new` route to `startNewSession`
(`internal/tui/model.go:863`), which calls `eng.ClearContext()`
(`internal/agent/agent.go:304`) — idle-only, refuses mid-Exchange. `/sessions` restore calls
`RestoreSession` (`agent.go:325`), same boundary. Both are on the TUI's `Engine` interface
(`internal/tui/tui.go:87-104`). Construction is `newAgent`
(`internal/agent/construct.go:31`), which already re-validates `cfg.SystemPrompt`
(`construct.go:78`) — the defense-in-depth gate this plan's name validation copies.
Sub-agents: `newChildAgent` (`internal/agent/subagent.go:109`) copies the parent cfg.

**Budget.** `internal/agent/loop.go:773` (`budget()`) builds `domain.Budget` from
`apogeectx.Allocate(window, reserve)`; `Budget.SystemPrompt` is the allocation share and
`Budget.EstimateTokens(chars)` (`internal/domain/budget.go:16`) the calibrated estimator.
Window unknown ⇒ zero Allocation (`internal/context/budget.go:64-66`) ⇒ share 0. The window
may bind seconds AFTER a cold start (first heartbeat), so a cold start typically cannot
warn; a `/new` after bind can — a documented limit, not a defect.

**Config precedent.** `cmd/apogee/config.go`: `fileConfig` holds yaml keys (`:530-710`);
`validatedSetsConfig`'s `Enable *bool` (`:644`) is the default-true switch precedent; the
file-only layer copy is `applyConfig` (`:420-431`); resolved defaults sit at `:400`. The
composition root builds `apogee.Config` at `cmd/apogee/wire.go:147-180`.

**Notices.** Engine-side there is NO note/notice event type (`internal/domain/events.go`);
session notes are TUI-owned via `transcript.addNote` (used across
`model.go`/`sessions.go`/`confine.go`). So the notice path is: engine exposes a report
method, the TUI formats and prints it at the three boundaries it already owns (startup seed,
`startNewSession`, restore).

---

## 1. The loader and the engine's session cache — ✅ DONE (2026-07-28)

NOTES (2026-07-28): two implementation details worth recording. (a) `newChildAgent` assigns the
parent's cache AFTER its `newAgent` call, so a child performs one discarded construction-time
read before the parent's slice overwrites it — "no re-read" holds semantically (a child can
never observe a mid-session edit or deletion, pinned by a test) rather than as a skipped
syscall; clearing `childCfg.ContextFiles` to avoid it would have left the child's cfg
disagreeing with the parent's. (b) The existing shared test helper `writeWorkspaceFile`
(`minilang_test.go`) gained a `MkdirAll` so a nested name works, instead of adding a second
near-identical helper to `contextfiles_test.go`.

**What.** The engine learns to discover and hold context-file content, re-read at every
session boundary. Nothing reaches the wire yet (that is item 2), so this item is inert.

- `internal/domain/config.go`: add `ContextFiles []string` to `Config` — the RESOLVED
  workspace-relative names to look for, in inclusion order; nil/empty ⇒ the feature is off.
  Doc comment states the contract: names are workspace-relative (joined to `WorkspaceDir`),
  must be relative and must not escape the workspace, content is data (never template), read
  at session boundaries only.
- `internal/agent/contextfiles.go` (new): `type contextFile struct { name string; content
  string; size int; err error }` and `loadContextFiles(workspaceDir string, names []string)
  []contextFile`. Behaviour: resolves each name via `filepath.Join(workspaceDir, name)`;
  a missing file is SKIPPED with no trace (no entry); a present-but-unreadable file yields
  an entry with `err` set and no content; an empty or whitespace-only file is skipped with
  no trace; otherwise the entry carries the verbatim bytes as string and the byte size.
  Inert (returns nil) when `workspaceDir == ""` — an embedder without a workspace has no
  basis to resolve against. `os.ReadFile` directly; tests use `t.TempDir()`.
- Name validation in `newAgent` (`construct.go`, beside the `prompt.Validate` gate at
  `:78`): an empty name, an absolute path, or a name whose cleaned join escapes the
  workspace (`..`) fails construction naming the offending entry — the defense-in-depth
  posture of ADR 0023 §3 (the host names the config key in item 3; this gate catches a Go
  embedder's typo). The loader itself assumes validated names.
- Cache on the Agent: `contextFiles []contextFile`, loaded in `newAgent`, RE-loaded at the
  top of a successful `ClearContext` (`agent.go:304` — after the `inExchange` guard) and
  after a successful `RestoreSession` (`agent.go:325` — only when `restoreSnapshot`
  succeeded; a refused restore changes nothing). `newChildAgent` (`subagent.go:109`) copies
  the parent's slice — same session, same bytes, no re-read.

**Tests.** Loader: found/missing/unreadable/empty/order-follows-list (table, `t.TempDir`).
Construction: absolute name, `..` escape, empty name each fail with the entry named.
Boundaries: edit the file, `ClearContext`, cache holds the new bytes; `RestoreSession`
re-reads; a refused `ClearContext` (mid-Exchange) leaves the cache untouched; child agent
holds the parent's content even after the file is deleted from disk.

**Acceptance.** Green gate passes; `TestPromptSeam_NativeProfileByteIdentical` untouched;
no request-visible change anywhere (the cache is dark until item 2).

**Commit.** `feat(agent): discover workspace context files at session boundaries`

## 2. Seeding — content rides the first system message — ✅ DONE (2026-07-28)

NOTES (2026-07-28): two literal-text deviations. (a) The composition lives in a named
`(a *Agent) standingSystem()` beside `systemPrompt()` in `loop.go` and `buildRequest` calls it,
rather than being inlined at `loop.go:569` — the plan's own item 4 needs "the SAME composed seed
item 2 sends" for `StandingTokens`, and a second inline copy of the join would be the thing that
drifts. (b) Trailing newlines are trimmed with the `"\r\n"` cutset, not `"\n"`, so a CRLF file
(the repo cross-builds for Windows) does not leave a stray `\r` before the block separator.

**What.** The cached content joins the rendered prompt in the ONE seeded system message.

- `internal/agent/contextfiles.go`: `(a *Agent) contextBlocks() string` — for each cached
  entry WITH content (error entries contribute nothing), a block

  ```
  ## Workspace context: <name>

  <verbatim content, trailing newlines trimmed>
  ```

  blocks joined by `"\n\n"`. Content bypasses `prompt.Validate`/`Render` entirely — braces
  pass through verbatim (owner decision: data, not template).
- `buildRequest` (`loop.go:569`): compose the seed from `a.systemPrompt()` and
  `a.contextBlocks()`, non-empty parts joined by `"\n\n"`; seed the `RoleSystem` message
  when the RESULT is non-empty. So: prompt alone, files alone, or both — and with neither,
  nothing is seeded (the native anchor narrows exactly as decided). Wire order stays
  prompt → context files → mechanism directives → tool block, falling out of position-0
  seeding with no other change.
- The projection is per-request (ADR 0023 §6): content re-joins on every request but the
  bytes only change at a session boundary, so the seeded message is KV-cache-stable within
  a session. It never enters history or the snapshot — nothing about ADR 0022 changes.

**Tests** (promptseam-style, in `internal/agent`): files-only seeds a system message;
prompt+files order (prompt first, blocks in list order, headers name the files); mechanism
directives and the tool block merge AFTER the content into the same single system message
(`TestPromptSeam_AppendsToSeededSystemMessage` precedent); `{{workspace}}` inside file
content survives verbatim while the same token in the template renders; two consecutive
requests carry byte-identical seeded content; after `ClearContext` with an edited file the
next request carries the new bytes; a child agent's request carries the parent's blocks.
`TestPromptSeam_NativeProfileByteIdentical` still passes untouched (no prompt, no files).

**Acceptance.** Green gate; the anchor test untouched; a Go embedder setting
`Config.ContextFiles` gets the full feature.

**Commit.** `feat(agent): seed context-file content into the first system message`

## 3. The config surface — the `context-files:` block — ✅ DONE (2026-07-28)

NOTES (2026-07-28): four points on the validation, which is wider than the item's four literal
cases. (a) Item 1's verifier carried a finding to this item — a Windows drive-relative name
(`C:AGENTS.md`) passes `filepath.IsAbs` — so "an absolute path" is implemented as
`workspaceRelative()`: rooted (`/x`, `\x`), drive-scoped (`C:x`), or `IsAbs` all fail as "not
workspace-relative". (b) The `..` check slash-normalises first (`path.Clean` over the
backslash-replaced name), so `..\secrets.md` is refused on Linux too — the checks are
machine-independent, as the item requires, at the cost of a false positive for a unix filename that
genuinely contains a backslash. (c) The duplicate check keys on that same cleaned form, so
`AGENTS.md` and `./AGENTS.md` collide. (d) Names are validated whatever `enable` says (the
`systemPromptSettings.validate` posture: a defect in the file outlives the day the block is
switched back on). Two test-shape notes: the file-only precedence case went into the existing
`TestResolveSettingsPrecedence` table beside the `ui`/`present` ones (whose 30 `want` literals
gained the new default field), and `wire_test.go` proves the threading through the ENGINE's
construction gate — a `..` name must fail startup — since no accessor for the list exists until
item 4.

**What.** Config-file-only plumbing, copying the `validated-sets:` block precedent end to
end (`fileConfig` → layer → `settings` → `applyConfig` → `validate`).

- `cmd/apogee/config.go`: `fileConfig` gains `ContextFiles *contextFilesConfig
  yaml:"context-files"` with `Enable *bool` and `Names []string`. Resolution: `enable`
  defaults true; `names` defaults `["AGENTS.md"]` ONLY when the key is absent (nil) — an
  explicit `names: []` means "no names" and effectively disables (yaml distinguishes absent
  from empty). Collapse at resolution: `enable: false` OR an empty resolved list ⇒
  `Config.ContextFiles` nil. File layer only — no flag, no env, like the rest of the
  system-prompt region.
- Validation (in the settings `validate` pass, machine-independent per ADR 0023 §3): an
  empty-string name, an absolute path, a name escaping the workspace via `..`, or a
  DUPLICATE name is a startup error naming `context-files.names` and the offending value.
  Whether the files exist is deliberately NOT checked — discovery is the feature.
- `cmd/apogee/wire.go:147`: `cfg.ContextFiles` = the resolved list from opts.

**Tests.** `config_test.go`: block absent ⇒ enabled + `["AGENTS.md"]`; `enable: false` ⇒
nil; `names: []` ⇒ nil; partial block (`enable` only / `names` only); custom list preserved
in order; each invalid-name case errors naming the key; precedence is trivially file-only.
`wire_test.go`: the resolved list lands on `apogee.Config.ContextFiles`.

**Acceptance.** Green gate; a config user gets the feature with zero configuration in any
repo carrying an AGENTS.md, and `enable: false` turns it off.

**Commit.** `feat(config): context-files block — enable switch and name list`

## 4. The session notice and the Budget warn

**What.** The user sees what was loaded and what it costs, at every session boundary.

- `internal/domain/contextfile.go` (new): `ContextFileNote{ Name string; Bytes int; Err
  string }` and `ContextFilesReport{ Files []ContextFileNote; StandingTokens int;
  SystemShare int }`.
- `internal/agent`: `(a *Agent) ContextFilesReport()` — one note per cache entry (loaded or
  errored); `StandingTokens` = `a.budget().EstimateTokens(len(seed))` over the SAME
  composed seed item 2 sends (rendered prompt + blocks); `SystemShare` =
  `a.budget().SystemPrompt` (0 when the window is unknown). Computed at CALL time, so a
  `/new` after the window has bound can warn where a cold start could not. Idle-only read,
  like the boundary methods beside it.
- `internal/tui/tui.go`: the `Engine` interface gains `ContextFilesReport()
  domain.ContextFilesReport` (doc comment: called only at idle, after a boundary).
- TUI notes (one helper, e.g. `noteContextFiles()` in `model.go`), called at the three
  boundaries the TUI owns: the startup seed, `startNewSession` after `addStartup`
  (`model.go:881`), and the `/sessions` restore path (`sessions.go`). Wording (implementer
  may polish, intent is fixed):
  - loaded: `context: AGENTS.md (3.1 KiB)` — several files comma-joined, list order;
  - errored: `context: <name> unreadable — <err>` as its own note;
  - warn (only when `SystemShare > 0 && StandingTokens > SystemShare`): `standing system
    content ~4.2k tokens exceeds its Budget share (~3.9k) — trim context files or the
    system prompt`;
  - nothing loaded and nothing errored ⇒ silent (no note at all — the common case in a repo
    with no AGENTS.md must stay noise-free).

**Tests.** Report: entries mirror the cache; token/share math with a pinned window; zero
share when the window is unknown. TUI: notes emitted at all three boundaries with the
expected wording; silence when the report is empty; the warn line appears exactly when the
report says over-share (table over the two ints).

**Acceptance.** Green gate; `/new` in this very repo prints `context: AGENTS.md (…)` once
the block lands in item 5's shipped template (until then, exercised via a hand-written
config).

**Commit.** `feat(tui): session notice and Budget warn for context files`

## 5. Shipped template and documentation close-out

**What.**

- `cmd/apogee/defaults/config.yaml`: a fully COMMENTED `context-files:` block in the
  system-prompt region (after `system-prompt-models:`), documenting: what it does (named
  workspace-root files folded into the system prompt at session start, the CLAUDE.md
  behaviour), both defaults (`enable: true`, `names: [AGENTS.md]`), all-found-in-list-order,
  re-read on `/clear`|`/new`, verbatim content (no placeholders), and the Budget-share warn.
  `TestEmbeddedDefaultConfigSetsOnlyTheSystemPrompt` (`defaults_test.go:63`) must still pass
  — the block is comment-only, `system-prompt-text:` stays the single active key.
- New ADR `docs/adr/0026-workspace-context-files-are-session-scoped-prompt-data.md`:
  records the decisions above (discovery not configuration-naming, data not template,
  session-boundary read, all-found list, root-only scope, files-seed-alone, inherit,
  Budget-share warn, loud-skip on unreadable). Supersedes nothing; amends ADR 0023 §6's
  zero-byte anchor with a dated note in 0023 pointing here ("no prompt AND no context
  files").
- `CONTEXT.md`: a **Context files** term entry in the domain language (what they are, when
  read, where they sit in the merged system message).
- `README.md`: a short paragraph in the configuration section beside the system prompt.
- `CHANGELOG.md`: `### Added` under `## [Unreleased]`. `VERSION` untouched.

**Tests.** The two `defaults_test.go` invariants pass; `make check` green.

**Acceptance.** A fresh install's config documents the feature; `apogee` started in this
repo with a default config loads `AGENTS.md` and says so; the docs map (`AGENTS.md` §
"Where knowledge lives") holds without edits.

**Commit.** `docs: ADR 0026, defaults template, CONTEXT.md, README, CHANGELOG for context files`
