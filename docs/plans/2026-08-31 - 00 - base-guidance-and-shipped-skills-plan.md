# Base guidance & shipped skills plan

**Goal:** Ship a maintained embedded default system prompt (replacing the stale seeded-once template) and a set of four shipped skills with an on-demand `load_skill` door, closing the headless `/skill` gap and the unclamped skill-body injection.
**Date:** 2026-08-31 · **Status:** unexecuted · **sized for:** ~200k-context host
**Sources:** ADR 0023, 0026, 0027, 0032, 0040, 0061; `internal/skills/doc.go`; `internal/scheme/` (embed pattern); base commit `e00fa144`.

**Ratified design calls (owner, 2026-08-31):**
- **Split by kind:** harness facts → orientation block; steering → embedded default template. Orientation unchanged this wave; the rule is recorded in ADR 0064.
- **Universal base:** one model-agnostic default; per-model stays with Mechanisms + `system-prompt-models`.
- **Default prompt size:** standard ~50 lines; sections bound in item 3.
- **`use-default-prompt` bool** (default true) governs only the nothing-configured case; explicit text/file always replaces the whole prompt.
- **Migration:** silent keep of existing explicit config text; new seeds ship the key commented out.
- **Editing seam:** settings editor pre-fills the embedded default when unset; no export command for the prompt.
- **Tier framing:** the default prompt is config-tier (part of the Bypass floor in both arms), never a Mechanism.
- **Shipped skills:** ids `debugging`, `planning`, `code-review`, `commit-hygiene`; embedded dirs with bundled files (never installed, scheme pattern); user library wins id clashes (ADR 0032); lowest-priority source.
- **`use-shipped-skills` bool** mirrors `use-project-skills`.
- **`/skills` verb:** bare lists the catalog with source labels; `/skills export <id>` copies a shipped skill dir to `~/.apogee/skills/<id>/`, refusing overwrite.
- **`load_skill`:** default-on tool (tools lever, not a Mechanism), adaptive single call — exact id → body; confident gated hit → body + also-matched ids; else id+summary candidates. Supersedes ADR 0061's B2 deferral only; B1 auto-attach stays deferred.
- **Headless/Firing prompts parse `/skill` tokens** via `internal/refs`.
- **Skill bodies get the @file-reference clamp** (shared per-message reference split).

**Regression check (2026-08-31, e00fa144):**
- 1: guard folded — the three→four fix is a count rule over both files; the ADR 0023 amendment supersedes §8's no-compiled-in-fallback rule (:136) and the :166 rejected alternative; prose guard scoped to non-archived docs.
- 3: guard folded — `defaults_test.go` and `settingsrows_test.go` amended in-item, `./cmd/apogee/...` added to acceptance; writer's decision: sweep the two "one active key" prose comments.
- 4: guard folded — seed only when `Global.Text` AND `Global.File` are both unset, at the editor-open seam; the registry row's blank contract stays.
- 5: guard folded — `interject.go:63-68` binds the same shared per-message split.
- 7: guard folded — `Sources` carries the shipped gate (zero = off) in this item; `sourceDirs`/`readRoots` stay host-path-only; `doc.go`'s no-builtins claims reworded (ADR 0002 attribution superseded by ADR 0065).
- 8: guard folded — `wire_firing.go` Sources gains the flag; both settings applies compose the full Sources.
- 9: guard folded — the mount threads domain.Config → hostTools → `registryWithMCP`; the `shipped:` write refusal is named on the write fence; the shipped source label lands in this item.
- 10: recast — the verb, its while-running listing and the manual row exist at BASE; the item extends the existing row (export is the only new code) and yields to `docs/manual/commands.md:29`'s while-busy promise (only the export form refuses mid-run).
- 10 (re-check): guard folded — the row also carries runsBareAtAccept so the "/" menu's accept keeps running bare /skills (autocomplete.go:894); the mid-run export refusal extends safeWhileRunning (command.go:378) with command_test.go:596's `{"/skills", true}` pin green; /confine precedent corrected to command.go:237.
- 13: guard folded — the lookup seam is defined in domain beside `SkillResolver` and threaded like Asker; the clamp-test line dropped (`dispatch.go:1106` is the one seam); `skill.go:27-37`'s host-side-catalog comments superseded.

**Standing requirements:** skills: coding-standards.
**Out of scope:** B1 auto-attach Mechanism; per-model shipped prompt variants; bench arms; suggestion-band changes; any version bump (see closing note).

## 1. ADR 0064 — the system prompt ships an embedded default — ✅ DONE (2026-08-31)

NOTES (2026-08-31): three→four placeholder-count fix applied as the item's rule, at ADR 0023 §4 (heading + the placeholder list, which now names `{{scratch}}` and cites ADR 0056 §3), :83's "listing the known three", §5's "two of the three inputs are live" (now three of four, naming the scratch dir's session-boundary move) and its "All three inputs already sit on the Agent" (now four, adding the lock-guarded `ScratchDir()`), and CONTEXT.md's *System prompt* entry. ADR 0023 :40's "three top-level keys" is a count of config keys, not placeholders, and was left alone; :286's "All three are constant within a session" belongs to the 2026-08-25 orientation-block amendment's three host facts, likewise left alone.
NOTES (2026-08-31): prose guard `grep -rn "no system prompt" docs/ CONTEXT.md` leaves one non-archived hit, `docs/manual/configuration.md:750`, which the item's own text reserves for item 3 (its sentence stays true until item 3's code lands); the other hits are archived plans, excluded by the item's non-archived-docs scoping.
NOTES (2026-08-31): un-batch retry — a prior batched attempt had left item 2's CONTEXT.md *Skill* entry hunk in the tree beside this item's. Per the dispatch DECISION the *Skill* hunk was reverse-applied to HEAD (verified byte-identical to `git show HEAD:CONTEXT.md`), so CONTEXT.md now carries this item's *System prompt* entry alone; item 2's `docs/adr/0065-*.md` and its ADR 0061 amendment were left untouched in the working tree for item 2's own dispatch, and appear in neither this sidecar's FILES nor this item's commit.
NOTES (2026-08-31): ADR 0064:33 and :84 cite ADR 0065 (the placement rule's task-shaped home), whose file lands in item 2's commit — a one-commit-window forward reference inside this wave, resolving as soon as item 2 lands. Left as written: dropping it would strip the fourth home out of the placement rule this item's own text specifies. No apogee-announced path, name or value is involved.

**What:** Author `docs/adr/0064-the-system-prompt-ships-an-embedded-default.md`: embedded default template compiled into the binary (scheme pattern, ADR 0040); resolution order = per-model entry > top-level text/file > embedded default when `use-default-prompt` ≠ false > nothing; the placement decision rule (fact → orientation, steering → template, model-gated → Mechanism, task-shaped → skill); config-tier Bypass framing; silent-keep migration. Add an amendment note to ADR 0023 pointing here, and fix its §4 "three placeholders" drift (four since `{{scratch}}`). Update CONTEXT.md's "System prompt" entry (~line 1018): embedded-default sentence + the same three→four fix.
**Regression guard.** The three→four fix is a rule — every count of the placeholder/input set in the two files — triaged with `grep -n "three" docs/adr/0023-*.md CONTEXT.md` (ADR 0023 :83, :96, :98 are stale beyond §4's heading); the narrow `three.*placeholder` grep stays as acceptance. The ADR 0023 amendment names §8's "no compiled-in fallback" rule (:136) and the :166 rejected-alternative as explicitly superseded by ADR 0064, and notes §6's `""`-seeds-nothing anchor is now reached only via `use-default-prompt: false` or explicit empty text. The prose-guard rule covers non-archived docs only; `docs/manual/configuration.md:750` is rewritten by item 3, not here — its sentence stays true until item 3's code lands.
**Prose guard:** every doc line claiming "delete the key → apogee sends no system prompt" must state the new semantics — rule: any sentence tying key absence to an empty wire prompt; find with `grep -rn "no system prompt" docs/ CONTEXT.md`.
**Files:** `docs/adr/0064-the-system-prompt-ships-an-embedded-default.md` (new), `docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md`, `CONTEXT.md`
**Tests:** none (docs-only).
**Acceptance:** files exist; `grep -c "0064" docs/adr/0023-*.md` ≥ 1; `grep -rn "three.*placeholder" CONTEXT.md docs/adr/0023-*.md` returns nothing.
**Commit:** `docs(adr): the system prompt ships an embedded default (ADR 0064)`

## 2. ADR 0065 — shipped skills and the load_skill door — ✅ DONE (2026-08-31)

NOTES (2026-08-31): un-batch retry — `docs/adr/0065-*.md` and the ADR 0061 amendment were carried
over intact from the earlier batched attempt and verified here (every cross-ADR link resolves to an
existing file; the acceptance grep counts 4 hits of "0065" in ADR 0061). Only the CONTEXT.md *Skill*
entry hunk, reverted to HEAD during item 1's recovery, was re-authored in this dispatch; CONTEXT.md
now carries item 2's changes alone (`git diff CONTEXT.md` touches only the *Skill* entry).
NOTES (2026-08-31): the *Skill* entry's closing `_Avoid_` sentence and its discovery sentence were
re-wrapped where the inserted clauses pushed lines past the file's ~100-column wrap; no wording
outside the item's scope changed, and the file's count of >100-column lines is back at its HEAD
value (82).
NOTES (2026-08-31): ADR 0065's Consequences names `internal/skills/doc.go`'s "no builtins shipped —
ADR 0002" attribution as superseded but does not edit it — that rewording is item 7's own listed
work (per this plan's item-7 regression-check line), so it is left untouched here rather than folded
in as a consequential edit.
NOTES (2026-08-31): the untracked `docs/plans/2026-08-31 - 01 - transcript-codec-hoist-plan.md`
belongs to a concurrent session and was left exactly as found; it is not in this item's FILES.

**What:** Author `docs/adr/0065-shipped-skills-and-the-load-skill-door.md`: the four shipped skills as an embedded lowest-priority source (user library wins, ADR 0032); bundled files served through a virtual read mount; `use-shipped-skills`; the `/skills` verb; `load_skill` default-on with the adaptive single-call shape — this **explicitly supersedes ADR 0061 Decision 4's B2 deferral only**; B1 auto-attach stays deferred pending its own supersede + bench arm. Record the tool-tier framing: `load_skill` rides `tools.enabled/disabled`, is not a Mechanism, and the catalog still never enters the standing prompt. Add the amendment note to ADR 0061. Update CONTEXT.md's "Skill" entry (~line 1093): shipped source, `/skills` verb, `load_skill`.
**Files:** `docs/adr/0065-shipped-skills-and-the-load-skill-door.md` (new), `docs/adr/0061-skill-suggestions-are-driver-side-over-an-engine-matcher.md`, `CONTEXT.md`
**Tests:** none (docs-only).
**Acceptance:** files exist; `grep -c "0065" docs/adr/0061-*.md` ≥ 1.
**Commit:** `docs(adr): shipped skills and the load_skill door (ADR 0065)`

## 3. Embedded default system prompt behind use-default-prompt

**What:** Depends on item 1. New asset `internal/config/defaults/prompt.txt` (~50 lines, `//go:embed`), bound sections: (1) identity + `{{workspace}}`/`{{datetime}}`/`{{mode}}`; (2) workflow discipline — read before edit, small focused edits, verify by running tests, finish the whole task; (3) tool habits — exact paths, targeted reads; (4) output concision (current seeded rules survive); (5) `sub_agent` delegation (current guidance survives, tightened); (6) `ask_user` usage. Only the four placeholders; must pass `prompt.Validate`. `ResolveSystemPrompt` (`internal/config/config.go:3079`) falls back to the embedded default when the whole resolution yields empty and `UseDefaultPrompt` (new `*bool`, default true) allows; per-model entries win as today. Add the `use-default-prompt` key: schema + registry row (beside the system-prompt rows) + settings row; comment out the active `system-prompt-text` block in `internal/config/defaults/config.yaml` and rewrite its surrounding docs (absence → embedded default; `use-default-prompt: false` → nothing). Update `docs/manual/configuration.md`. Call-site signature updates: `cmd/apogee/wire_boot.go:126,215`, `wire_settings.go:1806,1849`, `wire_firing.go:250`.
**Regression guard.** Two pinned tests are amended in-item: `internal/config/defaults_test.go:83` fatals once the seed's `system-prompt-text` block is commented out — rewrite its invariant to "no active prompt key; the embedded `prompt.txt` passes `prompt.Validate` instead"; `cmd/apogee/settingsrows_test.go:323-326` pins `len(want) == len(config.KeyRegistry)` — pin the new `use-default-prompt` row's value. Also sweep the two prose sites recording the seed's "one active key" premise — comments at `cmd/apogee/e2e_announced_test.go:194` and `internal/config/configwrite_scalar_test.go:349` (comments only, both tests stay green) — rewrite them to the embedded-default semantics.
**Files:** `internal/config/config.go`, `internal/config/registry.go`, `internal/config/defaults/config.yaml`, `internal/config/defaults/prompt.txt` (new), `internal/config/config_test.go`, `internal/config/defaults_test.go`, `internal/config/configwrite_scalar_test.go`, `cmd/apogee/wire_boot.go`, `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/settingsrows_test.go`, `cmd/apogee/e2e_announced_test.go`, `docs/manual/configuration.md`
**Tests:** resolution matrix (unset→default; text set→text; file set→file; bool false + unset→empty; per-model entry unaffected); embedded asset passes `prompt.Validate`; `internal/agent` ride-along anchor still holds when resolution is empty (extend the existing anchor test); the rewritten defaults_test invariant holds (no active prompt key, embedded default validates); the settings-rows registry pin covers the new key.
**Acceptance:** `go test ./internal/config/... ./internal/agent/... ./cmd/apogee/... && go build ./...`
**Commit:** `feat(config): resolve an embedded default system prompt behind use-default-prompt`

## 4. Settings editor seeds the embedded default

**What:** Depends on item 3. The `system-prompt-text` multi-line editor (`internal/config/registry.go:202-237` row, editor seam in `cmd/apogee/wire_settings.go`) pre-fills with the embedded default when the key is unset at every level; the seed is display-only until saved, at which point it becomes explicit config (and thus replaces the default as usual).
**Regression guard.** Seed only when the whole global source is empty — `Global.Text` AND `Global.File` both unset (the same emptiness item 3's fallback tests) — never when only the text key is absent: pre-filling beside a set `system-prompt-file` makes ctrl+s hit the both-set error (`internal/config/config.go:123-124`) on a config that works today. Seed at the editor-open seam in `cmd/apogee/wire_settings.go`, not via the registry row's `Text:` func: the row's blank contract stays (`cmd/apogee/settingsrows_test.go:183-189` — Value and Text both empty when unset), and `TestSettingsRowsCarryThePromptTextBesideItsSummary` is the test this item amends deliberately.
**Files:** `internal/config/registry.go`, `cmd/apogee/wire_settings.go`, `cmd/apogee/settingsrows_test.go`, matching tests
**Tests:** editor-open with key unset shows the embedded text; saving persists it; editor-open with key set shows the configured text untouched; editor-open with only `system-prompt-file` set seeds nothing.
**Acceptance:** `go test ./internal/config/... ./cmd/apogee/... && go build ./...`
**Commit:** `feat(settings): seed the system-prompt editor with the embedded default`

## 5. Skill bodies get the reference clamp

**What:** `resolveSkillRefs` (`internal/agent/loop.go:1188-1210`) currently injects `s.Body` verbatim. Clamp each body exactly as `resolveFileRefs` does (`loop.go:1035-1080`): content past its share of the History allocation is elided to the shared head/tail-plus-marker shape via `clampToBound` **before** the `<skill:>` header is added. Binding call: skill blocks and @file blocks of one message share a single per-message reference split.
**Regression guard.** `internal/agent/interject.go:63-68` is a second caller of both resolvers beside `loop.go:97-98`: bind the same shared per-message split at both call sites (e.g. each passes the combined reference count / precomputed bound into the two resolvers), or an interjection carrying one skill + one @file keeps today's per-list arithmetic.
**Files:** `internal/agent/loop.go`, `internal/agent/interject.go`, `internal/agent/loop_test.go` (or the file holding the resolveFileRefs clamp tests)
**Tests:** oversized skill body elides with the marker shape; a message carrying one skill and one @file splits the allocation across both — via an interjection too; a small body passes untouched byte-identical.
**Acceptance:** `go test ./internal/agent/... && go build ./...` — new clamp tests must fail against the pre-item tree (gap-closing fix).
**Commit:** `fix(agent): clamp attached skill bodies like @file references`

## 6. Headless and Firing prompts resolve /skill tokens

**What:** Gap: `internal/run/run.go:239` submits `UserInput` with `FileRefs` only, so a Firing/headless prompt containing `/code-audit` never attaches the body even though `Config.Skills` is wired (`cmd/apogee/wire_firing.go:157-168`). Add `SkillIDs: refs.SkillRefs(spec.Prompt, known)` where `known(id)` probes `Config.Skills.ResolveSkills([]string{id})` for one hit; a nil resolver skips skill parsing. Note the behaviour in `docs/manual/headless.md` and `docs/manual/daemon.md`.
**Files:** `internal/run/run.go`, `internal/run/run_test.go`, `docs/manual/headless.md`, `docs/manual/daemon.md`
**Tests:** a headless prompt with a known `/id` produces `UserInput.SkillIDs=[id]` and the injected user message carries the exact `<skill: ` block spelling the loop emits (announced surface); an unknown token stays plain text; nil resolver unchanged.
**Acceptance:** `go test ./internal/run/... && go build ./...` — the known-token test must fail against the pre-item tree.
**Commit:** `fix(run): headless and firing prompts resolve /skill tokens`

## 7. Shipped skill source + the debugging skill

**What:** Depends on item 2. `internal/skills` gains an embedded source: `internal/skills/shipped/<id>/SKILL.md` via `//go:embed all:shipped`; generalize the walk to load from an `fs.FS` (disk sources keep their existing `os.Root`-fenced path). Priority: shipped is appended **last** in `sourceAnchors` (`load.go:92-116`) so keep-first lets any user/workspace id shadow it, recorded as `ShadowedError` like today. First shipped skill: `shipped/debugging/SKILL.md` — frontmatter (id `debugging`, displayName, summary, description, `triggers:`) + body ≤150 lines (reproduce → isolate → fix → verify protocol), **body-only for now** (`Dir=""`, no `files:` line) — the bundled file arrives with item 9's mount so no announced path is ever unreadable. Update `internal/skills/doc.go` ("no builtin skills" sentence dies).
**Regression guard.** `Sources` gains the shipped gate in THIS item (zero value = off), so `Load(Sources{...})` stays shipped-free — the count-pinned load tests (`load_test.go:37,67,101,257`) and this item's "catalog without shipped source unchanged" test hold; item 8 only wires the config key to it. Keep the embedded source out of the `[]skillAnchor` host-path renderers: `sourceDirs` (`load.go:133`) and `readRoots` (`load.go:162`) must not render a shipped pseudo-anchor as a host path (a phantom dir in the /skills report; a cwd-relative mount on the trusted branch) — load it beside the anchor loop, or teach both consumers an explicit shipped label/skip. Reword every `doc.go` line carrying the no-builtins claim (`grep -n "builtin" internal/skills/doc.go` — :18's "no builtins shipped" ADR 0002 attribution and :51), superseded by ADR 0065 (item 2 names the superseded stance).
**Files:** `internal/skills/load.go`, `internal/skills/provider.go`, `internal/skills/doc.go`, `internal/skills/shipped/debugging/SKILL.md` (new), `internal/skills/load_test.go`
**Tests:** shipped catalog loads and every `shipped/*/SKILL.md` parses; a same-id skill in the home library shadows the shipped one with a `ShadowedError` record; catalog without shipped source unchanged; `sourceDirs`/`readRoots` list no shipped pseudo-path.
**Acceptance:** `go test ./internal/skills/... && go build ./...`
**Commit:** `feat(skills): shipped skills load from an embedded source below every user dir`

## 8. use-shipped-skills gates the embedded source

**What:** Depends on item 7. New `use-shipped-skills` `*bool`, default true, an exact mirror of `use-project-skills` (`internal/config/config.go:1063-1065`, registry row at `registry.go:330-335`, settings row near `wire_settings.go:1034`, template docs in `defaults/config.yaml`, live-appliable via `Provider.SetSources` + `Reload` per ADR 0037). `false` drops the shipped source from the walk. Document in `docs/manual/configuration.md` beside `use-project-skills`.
**Regression guard.** `cmd/apogee/wire_firing.go:163-166` builds a Firing's Sources with only `UseProjectSkills`: pass the resolved `use-shipped-skills` in too, or headless/Firing runs get the zero value false while sessions default true — a Driver-parity break (ADR 0031) that leaves item 6's shipped-id `/token` unresolvable headless. Both settings applies (`wire_settings.go:1038-1053` and the new mirror) compose the full Sources from current state — read-modify-write the provider's last-set Sources (extend the `settingsSkills` seam, `wire.go:294`) or hand the applier both resolved flags — so a mid-session `use-project-skills` flip cannot zero `UseShippedSkills`.
**Files:** `internal/config/config.go`, `internal/config/registry.go`, `internal/config/defaults/config.yaml`, `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_boot.go`, `cmd/apogee/wire_firing.go`, `internal/skills/provider.go`, tests, `docs/manual/configuration.md`
**Tests:** false ⇒ shipped ids absent from the catalog; live toggle reloads; default true; a `use-project-skills` apply preserves `UseShippedSkills` (and the mirror image).
**Acceptance:** `go test ./internal/config/... ./internal/skills/... && go build ./...`
**Commit:** `feat(config): use-shipped-skills gates the embedded skill source`

## 9. Virtual read mount for shipped skill files

**What:** Depends on item 7. Shipped skills' bundled files become readable: the tools layer gains virtual read roots — a `map[prefix]fs.FS` consulted by the shared path resolver **before** disk roots, serving `read_file`/`list_dir`/`grep`/`find_files` and `copy_file` **as source only** (destinations stay disk-fenced). Bound spelling: a shipped skill's announced dir is `shipped:<id>` — set as the skill's `Dir` so `resolveSkillRefs`' `files:` line and `{{SKILL_DIR}}` expand to it. Move a bundled reference file into `shipped/debugging/` (e.g. a checklist) and flip `debugging` to `Dir="shipped:debugging"`. Writes into a virtual root are refused with the standard out-of-root error.
**Regression guard.** The mount rides the ExtraReadRoots path exactly (`wire_boot.go:235` → `construct.go` → `registry.go:193-203` → `wire_tools.go:238`): thread the virtual roots through the domain.Config carrier, `internal/agent`'s hostTools (`construct.go:427-446`) and `cmd/apogee/wire_tools.go`'s `registryWithMCP` (:215-240), or an MCP session's announced `files: shipped:debugging` line read-refuses. The `shipped:` write refusal lives on the write-side fence (`path_safety.go` / the write tools' resolution) — `path_read.go` never sees a write (:137-139), so it cannot be the refusal site. The "shipped" source label lands in this item (`internal/tui/skills.go` `skillSource` + the "/" dropdown's row feed) so a shipped skill is never listed "elsewhere" before item 10.
**Files:** `internal/tools/registry.go`, the shared path-resolution file in `internal/tools/`, `internal/tools/path_safety.go` (or the write tools' resolution site), `internal/skills/load.go`, `internal/skills/shipped/debugging/` (bundled file new), `internal/domain/config.go`, `internal/agent/construct.go`, `internal/tui/skills.go`, `cmd/apogee/wire_boot.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/wire_tools.go`, tests
**Tests:** announced surface — drive `read_file`/`list_dir` with the EXACT `files: shipped:debugging` spelling the loop emits (taken from `resolveSkillRefs` output, not a fixture path); `copy_file` from virtual to workspace works; write into `shipped:` refused; disk-root behaviour byte-identical when no virtual roots are registered; the MCP-path registry (`registryWithMCP`) serves the virtual root; `skillSource` labels a `shipped:` dir shipped.
**Acceptance:** `go test ./internal/tools/... ./internal/skills/... ./internal/agent/... ./internal/tui/... && go build ./...`
**Commit:** `feat(tools): shipped skill directories mount as virtual read roots`

## 10. /skills lists the catalog and exports a shipped skill

**What:** Recast at the regression check (2026-08-31). Depends on item 9. New `/skills` verb mirroring `/color-scheme` (`internal/tui/command.go:235`, `verbGrammar`): bare lists the catalog with source labels (shipped / library / workspace, shadowed noted); `/skills export <id>` copies the whole embedded dir (SKILL.md + bundled files, bytes verbatim, mirror `scheme.Export` `internal/scheme/store.go:125`) to `~/.apogee/skills/<id>/`, refuses to overwrite, rejects non-shipped ids. Idle-only like every config-writing verb. Add the `docs/manual/commands.md` row.
**Regression guard.** The /skills verb, its while-running listing with source labels, and the manual row all EXIST at BASE (internal/tui/command.go:249, skills.go, docs/manual/commands.md:29) — recast the item to extend, not create: the existing row gains takesArgs + verbGrammar(parseSkills); bare keeps the existing catalog report (gaining a shipped-source label); only `/skills export <id>` and its skills-package export func are new code; keep whileRunning on the row and refuse ONLY the export form mid-run (precedent /confine, command.go:237 — extend safeWhileRunning, command.go:378, today hard-coded to confineArgs, so the pinned `{"/skills", true}` at command_test.go:596 stays green); the manual row is updated, not added. Re-check (2026-08-31): the row also carries runsBareAtAccept (bare only reports — command.go:74), so the "/" menu's accept keeps RUNNING bare /skills instead of leaving "/skills " in the box (acceptAutocomplete, autocomplete.go:894); the bare dispatch tolerates the nil verbArgs parse (parseSkills' zero value = the list form); sweep the prose naming the completing/running row sets (autocomplete.go:34, command.go:74-77).
**Files:** `internal/tui/command.go`, `internal/tui/commandrun.go`, `internal/tui/autocomplete.go` (row-set prose sweep), `internal/tui/skillscmd.go` (new — `parseSkills` + the export dispatch, + test), `internal/skills/` (export func beside the embed), `docs/manual/commands.md`
**Tests:** bare keeps today's labeled report (shipped label included) and still answers mid-run; the completion menu's accept still RUNS bare /skills rather than splicing "/skills " (runsBareAtAccept); the safeWhileRunning pin `{"/skills", true}` (command_test.go:596) stays green; export refuses mid-run; export writes verbatim bytes; second export refuses; unknown/non-shipped id errors with the usage line.
**Acceptance:** `go test ./internal/tui/... ./internal/skills/... && go build ./...`
**Commit:** `feat(tui): /skills lists the catalog and exports a shipped skill`

## 11. Shipped skills: planning and code-review

**What:** Depends on item 7. Author `shipped/planning/SKILL.md` (plan-then-implement: restate goal, enumerate steps with files, execute one at a time, verify each) and `shipped/code-review/SKILL.md` (review discipline: correctness first, realistic triggers, no style noise, verify findings against the tree). Each: full frontmatter with `triggers:`, body ≤150 lines, body-only or with bundled files under the item-9 mount at the author's discretion.
**Files:** `internal/skills/shipped/planning/SKILL.md` (new), `internal/skills/shipped/code-review/SKILL.md` (new)
**Tests:** covered by item 7's table test (all shipped skills parse; ids/display names/summaries within parse caps).
**Acceptance:** `go test ./internal/skills/...`
**Commit:** `feat(skills): planning and code-review join the shipped set`

## 12. Shipped skill: commit-hygiene + manual section

**What:** Depends on item 7. Author `shipped/commit-hygiene/SKILL.md` (conventional commit messages, small commits, changelog discipline, pre-commit checks). Add a "Shipped skills" section to the manual naming the four ids, the shadowing rule, `use-shipped-skills`, and `/skills export`.
**Files:** `internal/skills/shipped/commit-hygiene/SKILL.md` (new), `docs/manual/configuration.md` (or the manual page the skills docs live on)
**Tests:** covered by item 7's table test.
**Acceptance:** `go test ./internal/skills/...`
**Commit:** `feat(skills): commit-hygiene joins the shipped set; manual documents shipped skills`

## 13. load_skill searches the catalog on demand

**What:** Depends on item 2. New tool `load_skill(query)` in `internal/tools/load_skill.go`, default-on. `internal/skills` gains `Lookup(query)` reusing the `Suggest` index + evidence gate (`suggest.go:146`): exact catalog id → that body; a confident gated top hit (single gate-passer, or clear margin — threshold constant is implementer latitude) → body + one "also matched: ids" line; otherwise → id + summary candidates for a follow-up exact-id call; no gate-passer → "no match" naming the query. Body returned as a tool result (clamped by the standard tool-result clamp). Tool description one line; catalog contents never enter it. Inject the provider via a narrow interface **defined in `internal/tools`** and satisfied by `*skills.Provider` — the `domain.SkillResolver` seam is untouched. Wire in `cmd/apogee/wire_boot.go` + `wire_firing.go`. Document in the manual's tools list.
**Regression guard.** With `Config.Tools` nil the engine builds its roster from `hostTools(domain.Config)`, and a registry injected via `Config.Tools` is returned VERBATIM (`construct.go:428-430`) — dropping the tools.disabled/tools.enabled/profile roster a Firing applies (`wire_firing.go:240-241`). So define the lookup seam in `internal/domain` beside `SkillResolver` (which itself stays untouched) and thread it Config → hostTools → `tools.HostTools` like Asker, through `registryWithMCP` too. The clamp-test line is dropped: `dispatch.go:1096-1106` already clamps every tool result at the one seam, invisible to this item's packages. `Lookup` reverses the catalog-stays-host-side comments at `internal/skills/skill.go:27-37` (the ADR 0061 stance ADR 0065 supersedes) — reword them in-item.
**Files:** `internal/tools/load_skill.go` (new), `internal/tools/registry.go`, `internal/skills/suggest.go` (or new `lookup.go`), `internal/skills/skill.go`, `internal/domain/config.go`, `internal/agent/construct.go`, `cmd/apogee/wire_boot.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/wire_tools.go`, tests, manual tools page
**Tests:** exact id → body; ambiguous query → candidates; miss → no-match line; absent provider ⇒ tool absent from the menu.
**Acceptance:** `go test ./internal/tools/... ./internal/skills/... && go build ./...`
**Commit:** `feat(tools): load_skill searches the catalog on demand (ADR 0065)`

---

**Suggested version bump:** minor (0.x feature wave — embedded default prompt, shipped skills, load_skill). Owner decides; no item changes VERSION.
