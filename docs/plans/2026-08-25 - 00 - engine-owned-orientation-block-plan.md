# Engine-owned orientation block on the standing system message

**Goal:** the model always learns the host facts it needs to get oriented — the workspace
path, its session scratch dir, the `/tmp` caveat, the read-only library roots, and the
terminal's fail-fast behaviour — from harness text the engine composes itself, never from
the user-editable persona prompt. Today those facts ride only in the shipped default
`system-prompt-text`, which `~/.apogee/config.yaml` seeds once and never refreshes: every
install seeded before `75f166c2` (2026-08-22) has a prompt without the `{{scratch}}`
guidance, and the reviewed session `20260825T164640Z-ca180f38` (Qwen3.6-35B, `/code-audit`)
shows the cost — the model wrote to `/tmp`, was killed by confinement, was told only that
"the session scratch dir" is writable with no path, and spent 6 of 14 calls re-running a
script `set -e` had silently aborted.

**Date:** 2026-08-25
**Status:** ready — unexecuted
**sized for:** ~200k-context host

**Authoritative sources**
- ADR 0023 — the system prompt is a configured template rendered per request; §6 fixes the
  seeding position and the wire order (prompt → mechanism directives → tool block).
- `internal/agent/loop.go:786-799` (`standingSystem`) at `996cd24b` — the two-part
  composition (rendered template, then workspace context-file blocks) the block joins.
- `internal/agent/contextfiles.go:165` (`contextBlocks`) — the precedent for an
  engine-composed part of the standing system message.
- `internal/agent/loop.go:1025-1050,1085` — the hard-wired skill `files:` line: the
  precedent for harness-owned orientation text that the user's template cannot lose.
- `internal/agent/compact.go:222-238` (`mustPrompt`, `internal/agent/prompts/`) — where
  fixed engine prompt text lives as an embedded asset.
- `internal/domain/config.go:56-65` (`ScratchDir`), `:122-149` (`WorkspaceDir`,
  `ExtraReadRoots func() []string` — may be nil).
- `internal/platform/host.go:537-548` (`FailFastPreamble`: `set -e`, plus `set -o pipefail`
  where the host `sh` accepts it; POSIX only — `cmd.exe` has no analogue).
- `CONTEXT.md` "Scratch dir" entry (≈ line 565) — states the dir is "named to the model via
  `{{scratch}}` and one guidance line in the shipped default prompt".
- `docs/manual/configuration.md` ≈ line 482 — "delete it or comment it out to send no
  system prompt".

**Ratified design calls** (owner, 2026-08-25, via AskUserQuestion during the session review)
1. Orientation facts reach the model through an **engine-owned block** appended to the
   standing system message — not by detecting and refreshing a stale seeded
   `system-prompt-text`. The template stays persona-only.
2. **Ride-along only:** the block is appended only when a standing system message exists
   anyway (a rendered template and/or context-file blocks). With neither configured the
   documented "send none" native anchor stays byte-identical, and so does the Bypass floor.
3. The fail-fast note lives where its truth lives — the terminal tool's own description
   (`internal/tools/terminal.go`), not the engine block (the engine does not know the shell).
   Scope of this plan was ratified as "orientation block (workspace, scratch, /tmp, skills
   root, fail-fast)"; the other review findings are out of scope below.
4. (second ratification, same day, on the plan-review question) The shipped default template
   drops its own scratch/`/tmp` guidance lines once the block carries them — the
   `{{scratch}}` placeholder stays supported for user prose (item 1).
5. Point-of-failure signals are in scope, because a small model acts on the last tool
   result, not on a system-prompt line from twelve calls earlier: the `[exit code N]` line
   says fail-fast stopped the script (item 2), and both confinement denial labels name the
   writable roots by path (item 3).
6. `cmd/apogee` tests get a `TestMain` that points the home at a temp dir (item 4) —
   accepted into this plan rather than left for its own.

**Standing requirements**
- `skills: coding-standards`
- Any authorized deviation from item text lands as a dated `NOTES:` line under the item.
- No version identifier changes (VERSION, CHANGELOG release heading, tags) — see the
  closing note.

**Out of scope** (recorded in the review, deferred by the owner on 2026-08-25)
- `APOGEE_WORKSPACE` / `APOGEE_SCRATCH` / `APOGEE_SKILLS` in tool subprocess environments —
  the skill-finding fix is skill-side (see next bullet), and env vars would route around the
  `write-apogee-control-plane` text guard.
- Documenting or migrating a frozen seeded `system-prompt-text`.
- Skill-side rewrites (`{{SKILL_DIR}}`, `copy_file` instead of terminal `cp`) — skills repo.
- The in-flight sub-agent / unsaved-session-record question the owner raised — separate
  investigation.

---

## 1. Engine-owned orientation block in `standingSystem` — ✅ DONE (2026-08-25)

NOTES (2026-08-25): the item's text says `a.cfg.WorkspaceDir` is "always present"; the renderer
nil-guards it anyway (an empty workspace omits the bullet, and a block with no bullets at all
renders "" rather than a bare header), because `Config.WorkspaceDir` is documented as optional
("Empty ⇒ no default tools are wired") and an empty path in the bullet would mis-state a host fact.
NOTES (2026-08-25): the plan's Files list named only the `internal/config` template test; the
ride-along block also broke eleven existing exact-equality assertions on the seeded system message
in `internal/agent/promptseam_test.go` and `contextfiles_test.go`. They are updated through one new
test helper (`withOrientation`, in `orientation_test.go`) so each seam test still asserts the exact
bytes of the part it owns; `internal/config/defaults_test.go` no longer requires `{{scratch}}` in
the shipped template.
NOTES (2026-08-25): `internal/agent/doc.go`'s package map gained `orientation.go` — the package's
structural test (`TestDocMapNamesEveryFile`) fails on an unmapped file.
NOTES (2026-08-25): CONTEXT.md's *System prompt* and *Context files* terms each stated the merged
wire order, which this item's third part made incomplete; both order phrases were updated with it.

**What:** add a third, engine-composed part to the standing system message — after the
rendered template and the context-file blocks, so the wire order becomes
prompt → context files → **orientation** → mechanism directives → tool block.

- New file `internal/agent/orientation.go` with `func (a *Agent) orientationBlock() string`.
  It renders the embedded asset `internal/agent/prompts/orientation.txt` (loaded through the
  existing `mustPrompt`, like `overflow-bridge.txt`; keep the asset's one-trailing-newline
  convention) with these inputs, each read fresh per request:
  - `a.cfg.WorkspaceDir` — always present.
  - `a.ScratchDir()` — the lock-guarded live value; **omit the scratch line when it is `""`**
    (CONTEXT.md: "advertised writable only once it actually exists").
  - `a.cfg.ExtraReadRoots()` — nil-guard the func; **omit the library line when there are no
    roots**; list them comma-separated in the order returned.
- The block's text is binding (wording may be tightened, facts and structure may not):

  ```
  Host orientation (harness facts, independent of the prompt above):
  - Workspace: <WorkspaceDir> — the project's own files; relative paths resolve here.
  - Scratch dir: <ScratchDir> — writable and outside the workspace; put temp, probe and test-scaffold files here. /tmp may be denied by workspace confinement.
  - Read-only library roots: <root>, <root> — read them with read_file, list_dir, grep, find_files or copy_file, never through terminal commands.
  ```

- `standingSystem()` (`internal/agent/loop.go`) appends `orientationBlock()` as the LAST
  part **only when `parts` is already non-empty** (ratified call 2). With no template and
  no context files the function still returns `""` and nothing is seeded — the native
  anchor is untouched. Update the function's doc comment (it currently says "two
  INDEPENDENT sources") and the wire-order sentence.
- Sub-agents inherit `cfg.ScratchDir` / `ExtraReadRoots` and render their own
  `standingSystem`, so they get the block for free — no change in `subagent.go`; add a test
  proving it.
- Rendering uses plain `fmt`/`strings` composition, not `text/template` — the asset is a
  header line plus fixed bullet templates; keep it the shape `mustPrompt` already serves.
- KV-cache: every input is per-session constant (workspace, scratch, roots), so the block is
  prefix-cache-stable like `{{scratch}}`; note this in the doc comment.
- Docs, same item (each has exactly this one owner):
  - `CONTEXT.md` "Scratch dir" entry: the dir is named to the model "via the `{{scratch}}`
    placeholder, the shipped default prompt's guidance line, **and the engine-owned
    orientation block that rides on every standing system message**"; add an
    **Orientation block** term (definition, ride-along rule, wire position) beside it.
  - ADR 0023: append a dated amendment note (2026-08-25) under §6 stating the third
    engine-composed part, its position, and the ride-along rule — the ADR's decision
    stands; only the composition list grows.
  - `docs/manual/configuration.md` ≈ line 482: one sentence after "send no system prompt"
    — when a prompt (or context files) is sent, apogee appends its own short orientation
    block naming the workspace, the scratch dir and the read-only library roots; that block
    is not part of `system-prompt-text` and cannot be edited out of it.
- Standards that shape this item: one deep function (`orientationBlock`) behind
  `standingSystem`, no new interface; the asset text is data, not code; the nil-guard on
  `ExtraReadRoots` is mandatory (construct.go: "nil ⇒ workspace-only").
- **De-duplicate the shipped template** (ratified call 4): delete the two lines
  `Scratch and test files go in {{scratch}} — it is writable; the workspace is` /
  `for the project's own files only. /tmp may not be writable.` from
  `internal/config/defaults/config.yaml:242-243`. The placeholder comment block
  (`:226-236`) stays — `{{scratch}}` remains a supported placeholder — but its `{{scratch}}`
  entry gains half a sentence: "the orientation block already names it; use the placeholder
  only when your own prose wants the path". Adjust any test that pins the template's text
  (`internal/config` — search for `Scratch and test files`; `internal/agent/promptseam_test.go:517`
  uses its own template and is unaffected).

**Files:** `internal/agent/orientation.go` (new), `internal/agent/orientation_test.go` (new),
`internal/agent/prompts/orientation.txt` (new), `internal/agent/loop.go`,
`internal/config/defaults/config.yaml`, `CONTEXT.md`,
`docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md`,
`docs/manual/configuration.md`

**Tests** (`internal/agent/orientation_test.go`, table-driven where natural):
- template configured, scratch set, two read roots → the seeded system message ends with the
  block; all three bullets present with the exact paths.
- template configured, `ScratchDir == ""`, `ExtraReadRoots == nil` → block has the
  workspace bullet only.
- no template, no context files → `standingSystem()` returns `""` (native anchor pinned).
- context files only, no template → block still rides along.
- a sub-agent spawned from a parent with a scratch dir renders the same scratch bullet.
- `SetScratchDir` between two requests → the second request's block carries the new path.

**Acceptance:**
- `go build ./...`
- `go test ./internal/agent/ -run 'Orientation|StandingSystem|PromptSeam' -count=1`
- `go test ./internal/agent/ ./internal/prompt/ ./internal/config/ -count=1`
- `go vet ./internal/agent/ ./internal/config/`
- `grep -c 'Scratch and test files' internal/config/defaults/config.yaml` → `0`

**Commit:** `feat(agent): append an engine-owned orientation block to the standing system message`

---

## 2. Terminal discloses fail-fast — in its description and on the exit-code line

**What:** two halves, both about the POSIX `set -e` preamble the terminal prepends
(`internal/tools/terminal.go:125`, `platform.FailFastPreamble`).

1. `terminalSpec.description` (`terminal.go:18`) gains one sentence:

   > On POSIX the line runs fail-fast (`set -e`, and `pipefail` where the shell supports it):
   > the first command that exits non-zero stops the rest of the line, so guard expected
   > non-zero exits (`grep … || true`).

   The `command` argument description (`terminal.go:23`) is unchanged. Static text is correct
   on every host because it is scoped "On POSIX" — do not make the description
   platform-conditional.
2. The exit-code line says so at the point of failure (ratified call 5). `subprocessResult`
   (`internal/tools/exec_common.go:≈180-200`) gains `failFast bool`; the Terminal sets it on
   the result exactly when it prepended the preamble (the POSIX branch at `terminal.go:125`
   — a `subprocessSpec` field `failFast` carried through `runSubprocess` onto the result is
   the straight route; `python_exec`, `git` and the Console family never set it).
   `subprocessToolResult` (`terminal.go:173-194`) renders, for `exitCode != 0 && failFast
   && !timedOut`:

   ```
   [exit code N — fail-fast: the line stopped at the first command that failed; guard expected non-zero exits with `|| true`]
   ```

   and the plain `[exit code N]` otherwise. The confinement labels of item 3 still follow on
   their own line, unchanged in order. Update the `Terminal` doc comment (`terminal.go:36-60`)
   where it now contradicts the description.

**Files:** `internal/tools/terminal.go`, `internal/tools/exec_common.go`,
`internal/tools/terminal_test.go` (existing or new)

**Tests:**
- spec test: the description names fail-fast and `set -e`.
- `subprocessToolResult` table: `{exitCode:1, failFast:true}` → the fail-fast line;
  `{exitCode:1, failFast:false}` → plain `[exit code 1]`; `{exitCode:0, failFast:true}` →
  no exit-code line; `{exitCode:1, failFast:true, timedOut:true}` → "command timed out" and
  the plain line (a timeout is not a fail-fast stop).
- POSIX-only (`//go:build !windows`): a real `terminal` run of `false; echo after` reports
  the fail-fast line and no `after`.

**Acceptance:**
- `go build ./...`
- `go test ./internal/tools/ -run 'Terminal|Subprocess' -count=1`
- `go vet ./internal/tools/`

**Commit:** `feat(tools): say on the exit-code line and in the description that a POSIX terminal line runs fail-fast`

---

## 3. Confinement denial labels name the writable roots by path

**What:** both labels in `internal/tools/exec_common.go:204-219` stop saying "the session
scratch dir" abstractly and name the paths (ratified call 5). Replace the two string
constants with two functions over the box the run was confined by:

```go
func confinementDenialLabel(box domain.ConfinementBox) string
func confinementDenialStopLabel(box domain.ConfinementBox) string
```

Each renders its existing sentence with the tail
`writes are allowed only inside the workspace <box.WorkspaceRoot> and <box.WritablePaths joined by ", ">`
— and, when `WritablePaths` is empty, `… only inside the workspace <box.WorkspaceRoot>`.
The box is already in hand where the label is decided:

- `runSubprocess` (`exec_common.go:318-327`) has `conf.Box`; store it on the result
  (`subprocessResult.box domain.ConfinementBox`, set beside `confined = true`) and
  `subprocessToolResult` (`terminal.go:186-192`) passes it to both label functions.
- `renderConsoleTail` (`internal/tools/console_common.go:200-203`) gains a
  `box *domain.ConfinementBox` parameter (nil ⇒ unconfined; the stop label cannot fire
  unconfined, so nil only reaches the plain path). Its three wrappers in the same file
  (`console_common.go:129,138,146`) take a `ctx` and pass `confinementBox(ctx)`
  (`internal/tools/path_safety.go:47`, returns the pointer) — the Console tools that call
  those wrappers already hold the ctx.

`platform.LooksLikeConfinementDenial` and the kill-on-denial watch are untouched — only
the wording changes. The doc comments above both labels (`exec_common.go:200-217`) are
rewritten to say the label names the roots so the model can route the write, not merely
learn the fence exists. `ConfinementBox.WritablePaths` already folds `ScratchDir` in
(`internal/domain/confinement.go:76-84`), so no new field is needed anywhere.

**Files:** `internal/tools/exec_common.go`, `internal/tools/terminal.go`,
`internal/tools/console_common.go`, plus whichever of `internal/tools/console_open.go`,
`console_send.go`, `console_read.go`, `console_close.go` call the three wrappers and must
now hand them a ctx (enumerate with `grep -n 'renderConsole' internal/tools/*.go`), and
the existing label tests in `internal/tools/*_test.go`

**Tests:**
- label table: workspace + scratch → both paths appear; workspace only → the "and …" tail
  is absent.
- existing denial-label tests (`grep -rn 'blocked by workspace confinement' internal/tools/*_test.go`)
  updated to assert the path-bearing wording.
- one Console tail test with `DenialStopped()` true asserts the paths.

**Acceptance:**
- `go build ./...`
- `go test ./internal/tools/ -count=1`
- `go vet ./internal/tools/`

**Commit:** `feat(tools): name the writable roots in the confinement denial labels`

---

## 4. `cmd/apogee` tests never touch the real `~/.apogee`

**What:** `go test ./cmd/apogee` currently creates 15 empty `~/.apogee/scratch/<id>/` dirs
per run (1,139 accumulated on the owner's machine as of 2026-08-25), because tests that wire
with `ConfigDir: ""` resolve `config.ApogeeHome("")` → `os.UserHomeDir()/.apogee`
(`internal/config/config.go:2818-2828`). Add `cmd/apogee/main_test.go` with a `TestMain`
that, before `m.Run()`, creates a temp dir and sets `HOME` (and `USERPROFILE`, for
Windows — `os.UserHomeDir` reads that there) to it, removing the dir after the run.
`t.TempDir` is unavailable in `TestMain`, so use `os.MkdirTemp` + `os.RemoveAll`. Tests
that already set `HOME` themselves (`launcher_test.go`, `probe_test.go`) keep working —
`t.Setenv` restores to the TestMain value.

Add one guard test, `TestNoTestWritesTheRealApogeeHome`, that asserts `os.UserHomeDir()`
does not equal the process's original home (captured before `TestMain` overrides it —
store it in a package var) — so a future test file that resets `HOME` to the real one trips
it. The GC of the already-accumulated dirs is the existing 14-day sweep's job; this item
does not delete anything under the real home.

Precedent for `TestMain` shape: `internal/config/keyresolve_test.go`,
`internal/keystore/keystore_test.go`.

**Files:** `cmd/apogee/main_test.go` (new)

**Tests:** the guard test above; and the acceptance below proves the pollution stopped.

**Acceptance:**
- `go vet ./cmd/apogee/`
- `before=$(ls ~/.apogee/scratch | wc -l); go test ./cmd/apogee/ -count=1; after=$(ls ~/.apogee/scratch | wc -l); test "$before" -eq "$after" && echo CLEAN` → `CLEAN`

**Commit:** `test(cmd): point every cmd/apogee test at a temporary apogee home`

---

**Suggested version bump:** micro — `v0.17.1 → v0.17.2` — a shipped, model-visible feature
(the orientation block) per the VERSION micro-bump convention. Not performed by this plan;
the owner decides.
