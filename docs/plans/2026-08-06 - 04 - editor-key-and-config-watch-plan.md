# The `editor` key and the watched config file

**Goal.** Make the settings pane's external-edit jump open the editor the user actually
wants — including the desktop's default `.yaml` application — by replacing the
"block on the editor, diff when it exits" contract with a watched config file. A new
`editor` config key names the command; the watcher, not the child process's exit,
is what tells apogee that editing finished.

**Date.** 2026-08-06
**Status.** Ready to execute.

## Authoritative sources

- `docs/adr/0037-every-settings-edit-applies-to-the-running-session.md` — the ratified
  live-apply contract. This plan **supersedes its binding B** (the `$VISUAL`→`$EDITOR`→`vi`
  ladder) and **its diff-on-exit trigger**; item 1 records that supersession. Everything
  else in 0037 — the apply seam, the key classes, the ` *` marker, the idle-only
  mutators — stands unchanged and governs this plan.
- `docs/adr/0035-the-settings-surface-persists-one-key-per-deliberate-edit.md` —
  the persistence contract; the `editor` key obeys it like any other scalar.
- `docs/adr/0036-...` decision 3 — the unset-key posture this plan follows for `editor`.
- `docs/layout/settings-screen-layout.md` — the pane's rendering spec. **If it disagrees
  with an item, follow the spec**, and amend it in item 7.
- `internal/launcher/menu.go:448` and `internal/launcher/config.go:348` in the sibling
  repo `/workspace/repos/llama-launcher` — the prior art this plan adapts: an editor
  launched without waiting, plus an unconditional reload that silently keeps the last
  good config when the file does not parse. Read them before item 4; do not copy the
  hardcoded `open`, which is why that implementation is macOS-only.

## Ratified design calls

All decided by the owner on 2026-08-06, in session, before this plan was saved.

1. **The key is named `editor`** — top-level, not namespaced. It sits beside `server` and
   `llama-launcher` as a top-level scalar.
2. **Precedence: config beats environment.** Resolution is
   `editor` (config) → `$VISUAL` → `$EDITOR` → the OS default opener. An explicit setting
   outranks an ambient one, so the pane row always shows the command that will really run.
3. **The completion signal is a file watcher, not process exit.** Apogee watches
   `config.yaml` and applies whatever changed when it changes. This supersedes an earlier
   call in the same session to auto-append blocking flags (`code` → `code -w`); that
   approach is **not** to be implemented — the watcher makes wait flags unnecessary.
4. **The unset fallback is the OS default opener** — `open` on darwin, `xdg-open` on
   linux, `cmd /c start` on windows. This is a deliberate behaviour change: unset
   currently means `vi`. When no opener is present the failure must name all three ways
   to set an editor.
5. **The watcher runs for the whole session.** Any external change to `config.yaml`
   applies live — an editor jump from the pane, a GUI editor left open in another window,
   or a `vim ~/.apogee/config.yaml` in a second terminal. This is the honest reading of
   ADR 0037's "every settings edit applies to the running session".

Two consequences the plan author derived from those calls, binding on implementers:

- **Terminal editors keep the blocking, TUI-suspending path.** A terminal editor drawn
  over a live alt-screen TUI is broken, so `vi`/`vim`/`nvim`/`nano`/`pico`/`emacs`/
  `micro`/`hx`/`kak` — the existing `lineJumpEditors` set at `cmd/apogee/settingsedit.go:38`
  — still suspend the TUI and run in the foreground. Everything else is launched
  detached and the watcher does the rest. Both paths end in the same apply.
- **A config file that does not parse is ignored, and the last good config is kept**
  (llama-launcher's rule at `config.go:348`), because a watcher will inevitably read a
  half-written file. Unlike llama-launcher, apogee has somewhere to say so: a parse
  failure that persists across three consecutive ticks surfaces once as a transcript
  note, and is not repeated until the file parses again.

## Standing requirements

- `skills: coding-standards`
- Run `make check` before every commit; item 7 additionally runs `go test ./...`.
- No new third-party dependency. The watcher polls (mtime + size) on a ticker; `fsnotify`
  is not in `go.mod` and adding it is out of scope.
- **No version identifier changes in any item** — not `VERSION`, not a CHANGELOG release
  heading, not a tag. CHANGELOG edits go under the existing `[Unreleased]` section only.
- Any authorized deviation from item text lands as a dated NOTES line under that item.

## Out of scope

- Auto-appending wait flags to GUI editor commands (superseded by ratified call 3).
- An `$EDITOR` jump for keys that are not already `ExternalEdit` rows.
- Watching any file other than `config.yaml` (skills, MCP manifests, context files).
- Reworking the per-key apply dispatcher itself — items here feed the existing
  `applySettingFor` seam and must not change its signature.
- Any `fsnotify`/inotify dependency; any daemon or out-of-process watcher.

---

## 1. ADR 0038 — the config file is watched — ✅ DONE (2026-08-08)

NOTES (2026-08-08): the record was written as **ADR 0041**, not 0038 — 0038 (windows console), 0039
and 0040 were all taken between this plan being saved and its execution. Ratified by the owner
2026-08-08: file `docs/adr/0041-the-config-file-is-watched.md`, and every cross-reference (including
the blockquote added to ADR 0037) says 0041. No existing ADR was renumbered or modified apart from
that blockquote. Its H1 stays in house style — `# The config file is watched`, no `ADR 0041 —`
prefix — because no ADR in `docs/adr/` carries its number in the H1 and the item asks for the house
format of 0035/0036/0037; the number lives in the filename and in the cross-references.

**What.** Write `docs/adr/0038-the-config-file-is-watched.md` in the house format used by
0035/0036/0037 (Context / Decision / Considered options / Consequences). It records the
five ratified design calls above and the two derived consequences, and states precisely
what it supersedes in ADR 0037: **binding B** (the `$VISUAL`→`$EDITOR`→`vi` ladder, now a
four-rung ladder starting at the config key and ending at the OS opener) and **the
diff-on-exit trigger** (`externalEdit.spec` refreshing a baseline that `changed()` reads
when the child exits). State explicitly that everything else in 0037 stands — the
`ApplySetting` seam, the key classes, the idle-only mutators, the ` *` marker, and the
rule that a key that cannot be applied refuses on its row.

The Context section must explain *why* the trigger changed, in terms a reader without
this session can follow: a launcher stub such as `open` or `xdg-open` returns before the
editor is on screen, so an exit-triggered diff reads unchanged bytes and concludes the
user edited nothing. Cite llama-launcher's `doEditConfig` (`internal/launcher/menu.go:448`)
as the prior art and its polling `Reload` (`internal/launcher/config.go:348`) as the
reason it gets away with a non-blocking opener.

Add a "Superseded in part by 0038" blockquote under ADR 0037's title, following the
precedent 0037 itself set over 0035 (frontmatter `Status` stays `accepted`).

Verify every intra-ADR link target exists before writing it.

**Tests.** None — documentation only.

**Acceptance.**
- `ls docs/adr/ | grep 0038` finds the new file.
- `grep -c "0038" docs/adr/0037-every-settings-edit-applies-to-the-running-session.md`
  is at least 1.
- Every markdown link in the new ADR resolves to a file that exists.
- `make check` passes.

**Commit.** `docs(adr): 0038 — the config file is watched`

---

## 2. The `editor` config key — ✅ DONE (2026-08-08)

*Depends on item 1.*

NOTES (2026-08-08): the item's touch-point line reads "beside `WebSearch` at ~:809", but ~:809 is
`LlamaLauncher`'s line, not `WebSearch`'s (824). The field was placed directly after `LlamaLauncher`
— the cited line, and what ratified call 1 asks for ("it sits beside `server` and `llama-launcher`
as a top-level scalar") — with the same non-empty guard the `LlamaLauncher`/`WebSearch` pair uses.
`layer.editor` and `settings.editor` follow the same neighbour for consistency. The ADR the new doc
comments cite is **0041**, not 0038, per item 1's own renumbering note. The registry `Desc` and the
template paragraph describe the four-rung ladder item 3 implements, since the key has no other
meaning; nothing in this item changes `editorArgv`.

**What.** Add `editor` as a first-class editable string key, end to end through the
schema. It carries no live-apply seam of its own — it is read at editor-launch time, so
it needs no dispatcher case (item 3 consumes it).

Touch points, each small:

- `cmd/apogee/config.go` — `Editor string \`yaml:"editor"\`` on `fileConfig` (beside
  `WebSearch` at ~:809); `editor *string` on `layer` (~:456); `editor string` on
  `settings`; the non-empty guard in the `fileConfig`→`layer` conversion (~:1204,
  matching the `LlamaLauncher`/`WebSearch` pair); the `resolveSettings` copy (~:646);
  and the `applyConfig` copy into `options` (~:1584).
- `cmd/apogee/root.go` — `editor string` on `options` (beside `llamaLauncher` at ~:91).
- `cmd/apogee/registry.go` — one `configKey` literal, `Kind: kindString`,
  `Editable: true`, with a `Desc` that says what it is and that unset means the OS
  default opener. **Position it inside the Interface section's run** — after
  `cursor-shape` and before `bypass` — so it inherits that section with no edit to
  `settingSections` (`cmd/apogee/settingsrows.go:59`). Do **not** insert it at a
  section opener's index.
- `cmd/apogee/defaults/config.yaml` — one `# ===` divider block, a short prose
  paragraph, and **exactly one** commented example line at column 0:
  `# editor: code -w`. The line must stay commented (`TestEmbeddedDefaultConfigSetsOnlyTheSystemPrompt`
  fails otherwise), and `editor:` must not appear a second time at column 0 anywhere in
  the file — `commentedKey` (`cmd/apogee/configwrite.go:1219`) would then reject every
  write of the key. Place the block at the position matching the registry index.
- `cmd/apogee/settingsrows.go` — one `settingValues` entry:
  `"editor": func(o options) string { return o.editor }`. Blank when unset; do **not**
  substitute a word for emptiness, since the row's value seeds the edit field.
- **No validator.** `editor` is a free-text command line, exactly like `present.command`.
  Add `"editor"` to the `unchecked` allowlist in
  `cmd/apogee/registry_test.go:381` with a one-line reason, matching how
  `present.command` and `present.host` are handled there.

**Tests.** The registry-driven invariants pick the key up automatically, but two tables
are size-pinned and must be hand-edited:
- `cmd/apogee/settingsrows_test.go:221` `TestSettingsRowsFormatEffectiveValues` — its
  `want` map is length-checked against `len(keyRegistry)`; add the `editor` expectation.
- `cmd/apogee/settingsrows_test.go:18` `fabricatedSettings()` — give the new field a
  distinct value so the row assertion has something to check.
Add a row to `TestSpliceScalarSettingInsertsBelowTheTemplateExample`
(`cmd/apogee/configwrite_scalar_test.go:154`) proving the first write lands under the
commented example.

**Acceptance.**
- `go test ./cmd/apogee/` passes, including `TestRegistryIsBijectionWithFileConfig`,
  `TestRegistryRowInvariants`, `TestRegistryValidateHooksSitOnEditableKeys`,
  `TestSettingValuesCoverEveryRegistryKey`, `TestSettingsRowsCarryTheirSection`,
  `TestApplySettingRefusesEveryKeyItCannotReach` and
  `TestSpliceScalarSettingRoundTripsEveryEditableKey`.
- `grep -c '^# editor:' cmd/apogee/defaults/config.yaml` is exactly 1.
- The pane shows `editor` in the Interface section: assert it in the settings-rows test.
- `make check` passes.

**Commit.** `feat(config): editor key names the external editor command`

---

## 3. Editor resolution: the four-rung ladder and the spawn mode

*Depends on item 2.*

**What.** Replace `editorArgv` in `cmd/apogee/settingsedit.go:255` with a resolver that
implements ratified calls 2 and 4 and classifies the result.

- Signature becomes `editorArgv(configured string, getenv func(string) string, goos string)`.
  Ladder: a non-blank `configured` (split with `strings.Fields`, as today, so
  `editor: code -w` yields `["code","-w"]`) → `$VISUAL` → `$EDITOR` → the OS opener.
- The OS opener per `goos`: `open` (darwin), `xdg-open` (linux), `cmd /c start ""`
  (windows). Any other `goos` falls back to `xdg-open`.
- Add a `terminalEditors` set beside the existing `lineJumpEditors`
  (`settingsedit.go:38`) — same nine names — and a resolver result carrying a spawn
  mode: **foreground** (suspend the TUI, run in the foreground, wait) for a program whose
  `editorName` is in that set, **detached** for everything else. The line-jump `+<line>`
  argument stays bound to `lineJumpEditors` exactly as today; do not widen it.
- `spec` (`settingsedit.go:115`) reads the configured editor from the fresh file
  projection it already computes at :123 — `e.opts` is a startup snapshot and must not be
  used, or an editor set in this session would not take effect until relaunch.
- When the resolved program does not exist, the error must name all three ways to set an
  editor (the `editor` config key, `$VISUAL`/`$EDITOR`, and the OS opener), not just
  repeat Go's `executable file not found in $PATH`.

**Tests.** In `cmd/apogee/settingsedit_test.go`:
- Extend `TestEditorArgvFollowsTheLadder` (:19) for the new signature: config outranks
  `$VISUAL`; `$VISUAL` outranks `$EDITOR`; all three blank yields the right opener for
  each of darwin/linux/windows; a whitespace-only config value counts as unset.
- A new test pinning the spawn mode: each of the nine terminal editors resolves
  foreground; `code -w`, `open`, `xdg-open` and an unknown program resolve detached.
- Extend `TestExternalEditPassesTheLineJumpOnlyToEditorsThatTakeOne` (:48) with a
  config-sourced editor, and assert the OS opener never receives a `+<line>` argument.
- A test that a configured editor written into config.yaml is picked up by `spec`
  without a restart (proving the projection is read, not `e.opts`).

**Acceptance.**
- `go test ./cmd/apogee/ -run 'TestEditor|TestExternalEdit'` passes.
- `grep -n '"vi"' cmd/apogee/settingsedit.go` shows `vi` only inside the
  terminal/line-jump tables — never as a fallback.
- `make check` passes.

**Commit.** `feat(settings): resolve the editor from config, env, then the OS opener`

---

## 4. The config file watcher

*Depends on item 2.*

**What.** A new `cmd/apogee/configwatch.go` holding a self-contained, dependency-free
watcher over one path.

- Poll `os.Stat` on a ticker; treat a change in **either** mtime or size as a change, so
  a same-second rewrite of equal length is not missed. Default interval 1s; make it a
  field so tests drive it directly.
- Report changes on a channel, coalescing bursts (an editor that writes, truncates and
  renames must produce one report, not three) with a short settle delay.
- A `Stat` error (file temporarily absent during an atomic rename) is not a change and
  not an error — skip the tick and retry.
- Start/Stop are safe to call once each; Stop must not leak the goroutine. The type owns
  no config parsing — it reports "the file changed", nothing more. Parsing and the
  last-good rule belong to item 6.
- Nothing wires it yet; this item ships the mechanism and its tests only.

**Tests.** `cmd/apogee/configwatch_test.go`, all against a `t.TempDir()` file with a
short interval:
- A write is reported once.
- Three rapid writes coalesce into one report.
- A same-mtime, different-size write is reported (pin the size half of the check).
- Deleting and recreating the file reports a change and does not error.
- A file that never changes produces no report.
- Stop terminates the goroutine — assert with `-race` and a leak check that no send
  happens after Stop returns.

**Acceptance.**
- `go test -race ./cmd/apogee/ -run TestConfigWatch` passes.
- `grep -rn fsnotify go.mod` finds nothing.
- `make check` passes.

**Commit.** `feat(config): a polling watcher over config.yaml`

---

## 5. The spawn mode reaches the TUI

*Depends on item 3.*

**What.** Carry item 3's spawn mode across the `ExternalEditSpec` seam so the renderer
knows whether to suspend.

- Widen the seam in `internal/tui/tui.go` (declared at `tui.go:889` region, wired at
  `cmd/apogee/wire.go:682`) so `spec` returns the argv **and** the mode. Keep it a plain
  data return — no callbacks, no engine dependency; ADR 0031's wire-silent rule applies.
- `internal/tui/settings.go:602` currently always uses `tea.ExecProcess`. Split:
  **foreground** keeps `tea.ExecProcess` exactly as today, including the existing
  `settingsEditedMsg` round trip. **Detached** starts the process without taking the
  terminal — no `ExecProcess`, no alt-screen release, the pane stays up — and does not
  wait on it. A detached start must not inherit the TUI's stdin/stdout.
- A detached launch that fails immediately (program not found) must still surface the
  item-3 error message on the launching row through the existing failure slot.
- A detached launch produces **no** diff on its own. Nothing applies here; item 6 supplies
  the apply. A detached launch therefore leaves the pane exactly as it found it, plus a
  note on the row (item 6 finalizes that note's wording against the layout spec).

**Tests.**
- `cmd/apogee`: `spec` returns foreground for a terminal editor and detached for the OS
  opener, with the argv unchanged in both cases.
- `internal/tui`: a foreground spec still produces a `tea.ExecProcess` command; a
  detached spec does not, and leaves the pane open. A detached launch of a nonexistent
  program lands the error on the launching row.
- `TestModelNoBuilderByValue` must still pass (ADR 0011 — the `Model` is value-copied).

**Acceptance.**
- `go test ./cmd/apogee/ ./internal/tui/` passes.
- `make check` passes.

**Commit.** `feat(settings): detached editors leave the TUI up`

---

## 6. The watcher drives the live apply

*Depends on items 4 and 5.*

**What.** Wire the watcher to the existing apply path so a saved file applies itself,
whoever wrote it — ratified call 5.

- Start the watcher in `cmd/apogee/wire.go` for the life of the TUI, over
  `configFilePath(opts.configDir)`, and stop it on teardown beside the other run-scoped
  closers.
- On a reported change: re-read and re-project the file, diff it against the baseline
  with the **existing** `externalEdit.changed()` (post-`b5e43fd` it already compares
  structured values, not row summaries), and push each changed key through the existing
  `applySettingFor` dispatcher. Do not change the dispatcher's signature or add cases.
- **Last-good rule (derived consequence 2).** A file that does not parse is ignored, the
  previous projection is kept as the baseline, and the next tick retries. Three
  consecutive failures surface one transcript note; the note is not repeated until the
  file parses again.
- **Self-writes must not double-apply.** The pane's own `WriteSetting` already applies
  the key live; refresh the baseline on every apply — from both paths — so the watcher
  sees no change for a write apogee just made. A key applied by the watcher journals its
  ` *` marker exactly as a pane edit does (ADR 0037 decision 8).
- Every key that changes must reach its live home or refuse on its row, with the same
  per-key error handling `ReloadConfig` uses today. `server` is still never reported.
- Retire the exit-triggered diff for **detached** launches only: the foreground path
  keeps working as it does today, so a terminal editor still applies on exit without
  waiting for a tick.

**Tests.** In `cmd/apogee`, driving the real watcher against a temp config with a short
interval:
- Writing a changed `auto-title` to the file applies it live with no pane interaction.
- Writing an unparseable file applies nothing, keeps the previous values, and the
  following good write applies normally.
- A pane write does not produce a second apply from the watcher (assert the applier is
  called once for that key).
- Repointing an `mcp-servers` entry via the file alone triggers the reconnect —
  the regression case from commit `b5e43fd`, now through the watcher.
- In `internal/tui`: a watcher-applied key renders its ` *` marker on the row.

**Acceptance.**
- `go test -race ./cmd/apogee/ ./internal/tui/` passes.
- A test proves an external write applies without any key press.
- `make check` passes.

**Commit.** `feat(settings): external config edits apply live through the watcher`

---

## 7. Docs sweep

*Depends on items 1–6.*

**What.** Bring the prose in line with what shipped. One owning item for every
cross-cutting amendment:

- `README.md` — document the `editor` key, the four-rung ladder, and that saving
  `config.yaml` in any editor applies live. Correct any claim that an external edit
  needs a restart or that `$EDITOR` is the only mechanism.
- `layout.md` and `docs/layout/settings-screen-layout.md` — the `⏎ opens $EDITOR`
  affordance's wording and behaviour for detached editors (the pane stays up), plus the
  `editor` row in the Interface section. The spec outranks item text; if it and this plan
  disagree, follow the spec and note it.
- `CONTEXT.md` — add the watcher to the concept map if the domain language needs a term
  for it; keep the registry field list accurate.
- `CHANGELOG.md` — one entry under the existing `[Unreleased]` section only. **No release
  heading, no VERSION, no tag.**
- `ISSUES.md` / `TODO.md` — remove any entry this plan resolves; add none.

**Tests.** None beyond the full suite.

**Acceptance.**
- `go test ./...` passes and `make check` passes.
- `grep -rn 'docs/design/.*-layout' .` finds nothing that does not exist at that path.
- Every command, key name and on-screen string quoted in the touched docs exists verbatim
  in the build — check each by grep.
- `git diff --stat` shows no change to `VERSION` and no new CHANGELOG release heading.

**Commit.** `docs: the editor key and the watched config file`

---

## Suggested version bump

Not performed by any item — the owner decides. This plan is user-visible (a new config
key, a changed default editor, external edits applying live) and would warrant a **minor**
bump. Note that the settings-live-apply wave already suggested v0.12.0 from v0.11.8 and
was not bumped either; if both land together, one minor bump covers them.
