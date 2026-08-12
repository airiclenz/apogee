# Plan: skill files discoverable and readable by the model

- **Goal:** a model with a skill attached can find and read the skill's bundled files
  (refs/, prompts/, scripts) with its ordinary read tools, on every target OS. Today the
  four read-only tools (`read_file`, `list_dir`, `grep`, `find_files`) are hard-fenced to
  the workspace root, so `~/.apogee/skills/...` is refused with a path-escape error, and
  the injected skill block (`internal/agent/loop.go` `resolveSkillRefs`) never names the
  skill's folder — the model has neither the address nor the access. Both halves are fixed
  hard-wired in the harness, never via the user-definable system prompt.
- **Date:** 2026-08-12
- **Status:** not started
- **sized for:** ~200k-context host
- **Authoritative sources:**
  - `internal/tools/path_safety.go` + `internal/security/safeio.go` / `pathsafety.go` —
    the workspace fence the seam extends (read-only, per-root `os.Root` pinning).
  - `internal/skills/skill.go` (`Skill.Dir` — carried exactly for this bundled-resource
    feature, see `internal/skills/doc.go`), `internal/skills/load.go` `sourceDirs`.
  - `internal/agent/loop.go:844` `resolveSkillRefs` — the injection point.
  - ADR 0031 (engine seams stay generic/driver-agnostic); ADR 0012 (confinement policy —
    untouched: OS confinement never blocked reads, `internal/platform/landlock_linux.go:79`).
- **Ratified design calls** (owner, 2026-08-12, grill session):
  1. Scope = (a) read access to skill source dirs through the four read-only tools and
     (b) the skill's folder path injected into the attached-skill block. Explicitly
     deselected: a standing every-turn skills note; an ADR / CONTEXT.md / default-config
     comment update.
  2. Mechanism = a **generic** "extra read-only roots" seam on the engine/tools; the TUI
     wires the skill source dirs into it. The tool layer never learns about skills.
  3. Roots are **live**: resolved per tool call through a `func() []string`, so a
     mid-session `use-project-skills` flip is honored on the next read.
- **Author-resolved calls** (plan author, 2026-08-12 — routine, binding):
  - All four read-only tools get the seam; every write/exec tool stays workspace-fenced.
  - The whole source dirs are readable (e.g. all of `~/.apogee/skills/`), not just
    attached skills' folders.
  - Extra roots are reachable by **absolute path only**; relative paths keep resolving
    against the workspace root alone (no ambiguity, and the injected line hands the model
    an absolute path).
  - Per-root `os.Root` fencing is kept, so a symlink inside a skill dir that escapes that
    dir is still refused. Works identically on Linux/macOS/Windows (in-process Go fence,
    not the kernel).
- **skills:** coding-standards
- **Out of scope:** standing per-turn skills note; ADR or CONTEXT.md/default-config.yaml
  doc updates; any change to exec/confinement (subprocess reads were never blocked); write
  access to skill dirs; MCP tools; /skills UI; version bumps (see closing note).

Every item: add a line under `[Unreleased]` in `CHANGELOG.md` describing the change, and
run the build sanity check before reporting. Any authorized deviation from item text must
land as a dated NOTES line under the item.

## 1. Multi-root read resolution helper in internal/tools

**What:** In `internal/tools/path_safety.go`, add an unexported helper type (suggested
name `readScope`) holding the workspace root and `extra func() []string` (nil ⇒
workspace-only, byte-identical behavior to today). Methods mirror the existing helpers
but try roots in order — workspace first, then each extra root — and return the
**matched root** alongside the resolved path/handle so callers pin all later fenced
operations to it:

- `resolve(input) (root, resolved string, err error)` — workspace via
  `security.ResolveInRoot`; on `ErrPathEscape`, each extra root in turn. Relative input
  is resolved against the workspace root ONLY (author-resolved call above): extra roots
  are tried only for absolute input. All refuse ⇒ the workspace's `ErrPathEscape`
  (existing uniform message, unchanged).
- `open(input) (*os.File, root string, err error)` — same fallback over
  `security.SafeOpen(root, input)`.
- a bounded-read variant that picks the root then delegates to the existing
  `readWorkspaceFileBounded(path, root)`.

A missing/unopenable extra root (e.g. `~/.apogee/skills` not yet created — the
creation-deferred convention) is a per-root refusal that falls through; never an error of
its own. The type is used by READ paths only; no write helper may accept it
(`workspaceScopedWriter` discipline untouched).

**Tests:** in `path_safety_test.go`: workspace paths resolve to the workspace root
(relative and absolute); an absolute path under an extra root resolves with that root
returned; a relative path never resolves against an extra root; a path under no root
returns `ErrPathEscape` with the existing message; nil func and empty slice behave as
today; a symlink under an extra root escaping that root is refused; a func whose return
changes between calls is honored (live-ness); a nonexistent extra root falls through
silently.

**Acceptance:** `go build ./...` && `go test ./internal/tools/ -run 'ReadScope|PathSafety' -v`
(exact test names per the implementation).

**Commit:** `feat(tools): multi-root read resolution seam for the read-only tools`

## 2. read_file and list_dir honor extra read-only roots

Depends on item 1.

**What:**
- `internal/tools/registry.go`: `HostTools` gains `ExtraReadRoots func() []string` with a
  doc comment stating the contract: read-only, absolute-path access, resolved live per
  call, nil ⇒ workspace-only. `DefaultToolsWithHost` threads it into `NewReadFile` and
  `NewListDir` (constructor signatures change; update all callers and tests —
  `NewDefaultRegistry`/zero `HostTools` keeps today's behavior).
- `internal/tools/read_file.go`: route `Execute` through the item-1 helper instead of the
  direct `readWorkspaceFileBounded(args.Path, t.root)` call; the matched root feeds the
  bounded read. Update the doc comment (the fence description gains the extra-roots
  clause) and extend the tool `description` with one short clause, e.g. "; absolute paths
  under a configured read-only root (such as the skills library) are also readable".
- `internal/tools/list_dir.go`: the `resolveInRoot`/`safeOpen` calls (`list_dir.go:67`,
  `:75`, `:158`) go through the helper, pinning the whole listing walk to the matched
  root. Same one-clause description extension.

**Tests:** read_file reads a file under a temp extra root by absolute path; still refuses
an outside path not under any root (same message); list_dir lists an extra root and a
subdir of it; relative paths under the workspace unchanged; an explicit write-tool test:
`write_file` (and one editing tool) aimed at the extra root still refuses with the escape
error.

**Acceptance:** `go build ./...` && `go test ./internal/tools/ -run 'ReadFile|ListDir' -v`

**Commit:** `feat(tools): read_file and list_dir read under configured read-only roots`

## 3. grep and find_files honor extra read-only roots

Depends on item 2 (uses the `HostTools.ExtraReadRoots` field it adds).

**What:** same treatment for the two discovery tools: `DefaultToolsWithHost` threads the
func into `NewGrep`/`NewFindFiles`; the path resolution (`grep.go:131`,
`find_files.go:98`) goes through the item-1 helper; the in-walk containment fences
(e.g. grep's `realRoot` prefix check, `grep.go:91`) are pinned to the MATCHED root, not
unconditionally to the workspace. One-clause description extension on both tools.

**Tests:** grep finds content in a file under an extra root (absolute search path);
find_files finds by name pattern under an extra root; both refuse paths under no root;
workspace-relative behavior unchanged; a symlink escaping the extra root is not followed
by either walk.

**Acceptance:** `go build ./...` && `go test ./internal/tools/ -run 'Grep|FindFiles' -v`

**Commit:** `feat(tools): grep and find_files search under configured read-only roots`

## 4. Engine and composition wiring: skill source dirs become the live read roots

Depends on items 2 and 3.

**What:**
- `internal/domain/config.go`: `Config` gains `ExtraReadRoots func() []string`, documented
  as a generic engine seam (read-only mounts for the read tools; live per call; the
  engine stays skill-agnostic — ADR 0031 spirit). The engine never defaults it.
- `internal/agent/construct.go`: `hostTools(cfg)` threads `cfg.ExtraReadRoots` into
  `tools.HostTools`.
- `internal/skills/provider.go`: exported `SourceDirs() []string` returning
  `sourceDirs(p.sources())` — the CURRENT sources' dirs, so it is live through
  `SetSources` with no extra plumbing.
- `cmd/apogee/wire_tools.go` `registryWithMCP`: pass `cfg.ExtraReadRoots` in its
  `HostTools` literal.
- Composition roots: wherever the TUI assembles `Config` after `wire_boot.go:67` creates
  `w.skillProvider`, set `Config.ExtraReadRoots = w.skillProvider.SourceDirs`; same in
  `cmd/apogee/headless.go` beside its `skills.NewProvider` (headless.go:340).
- Sub-agents need NO extra wiring: their registries `Subset` the parent's tool instances
  (`internal/domain/tools.go:161`), so the same read-roots func rides along. State this
  in a code comment at the wiring site.

**Tests:** provider test — `SourceDirs` reflects a `SetSources` change immediately;
wiring test — a registry built from a `Config` with `ExtraReadRoots` set yields a
read_file that reads under that root (engine-level, `construct` or registry test); a
`Subset` containing read_file still reads under the extra root (the sub-agent
inheritance claim, cheap to pin).

**Acceptance:** `go build ./...` && `go test ./internal/skills/ ./internal/agent/ ./internal/tools/`

**Commit:** `feat(engine): skill source dirs wired as live read-only roots for the read tools`

## 5. Attached-skill block names the skill's folder

Depends on item 4 (the line promises readability; item 4 makes the promise true).

**What:**
- `internal/domain/config.go`: `ResolvedSkill` gains `Dir string` (doc: absolute path of
  the skill's folder; empty when the resolver has none).
- `internal/skills/catalog.go:120` `ResolveSkills`: fill `Dir` from `Skill.Dir`.
- `internal/agent/loop.go` `resolveSkillRefs`: when `Dir` is non-empty, the injected
  block gains ONE fixed line directly after the opening tag — hard-wired harness text,
  independent of the user-definable system prompt. Binding format:

  ```
  <skill: NAME>
  files: DIR — this skill's bundled files; read them with read_file, list_dir, grep or find_files
  BODY
  </skill>
  ```

  Empty `Dir` ⇒ no `files:` line and the block is byte-identical to today (existing test
  resolvers keep passing modulo struct literals).

**Tests:** loop test — a resolver returning `Dir` yields the `files:` line verbatim in
the prepended block; empty `Dir` omits it; catalog test — `Dir` round-trips from
`Skill.Dir` through `ResolveSkills`.

**Acceptance:** `go build ./...` && `go test ./internal/agent/ ./internal/skills/ -run 'Skill'`

**Commit:** `feat(agent): attached-skill block names the skill folder for bundled-file reads`

---

**Suggested version bump:** one micro bump (house convention: VERSION micro-bumps per
shipped feature) once all items land — suggestion only; whether and when to bump is the
owner's call.
