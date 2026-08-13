# remember-model: persist the model choice per server and restore it at startup

- **Goal:** when the user switches a model, apogee records that choice in its own
  config so the next TUI startup comes back on the same model — writing the picked
  model id into the server entry's existing `model:` key on plain servers, and a new
  per-server `launch-profile:` pointer (actuated at boot through the existing
  actuation latch) on launcher-fronted servers. Gated by a new top-level
  `remember-model` toggle, off by default.
- **Date:** 2026-08-13
- **Status:** unexecuted
- **sized for:** ~200k-context host
- **Authoritative sources:** ADR 0029 (launcher actuates, beat binds; D4 launcher
  config is read-only to apogee), ADR 0036 D2 (a profile's server is not a `servers:`
  entry; unlisted endpoints record nothing), ADR 0024 (single rebind path),
  CONTEXT.md "Launch profile" section, and the ratified design calls below.
- **skills:** coding-standards
- **Ratified design calls** (owner, grill session 2026-08-13):
  1. Persist-the-pointer in apogee's own config; the launcher's YAML is never
     written (ADR 0029 D4 stands).
  2. Two server classes: plain multi-model servers reuse the existing per-server
     `model:` key (startup restore already exists — `cmd/apogee/wire_server.go:99,133`
     binds it); launcher-fronted servers get a new per-server `launch-profile:`
     pointer, actuated at startup through the existing actuation latch (beat binds,
     move commits — pure ADR 0029 reuse).
  3. One top-level toggle `remember-model` (`*bool`, nil = off) gates both the
     config write and the boot restore; it gets a /settings row, live-applied.
  4. Only explicit user `/model` picks record. Passive heartbeat-observed rebinds
     and the `--model` / `APOGEE_MODEL` one-shot overrides never write config.
  5. Launcher-fronted entries never get `model:` written — on those entries it is a
     deliberately-empty discovery hint (`cmd/apogee/upstream.go:224`).
  6. `/unload-model` and `/stop-server` leave the pointer in place — they mean
     "free the GPU now", not "forget my model".
  7. Startup restore is TUI-only and yields to a running server: auto-load only
     when nothing is serving; otherwise join what runs and note it. A recorded
     profile missing from the launcher's config → skip with a note.
  8. The pointer's home on a load commit is the **actuating entry** — the entry
     whose `llama-launcher:` key the session's launcher path follows. A load that
     moves the session to an unlisted endpoint still records onto that entry; if
     the actuating entry cannot be identified, nothing is recorded.
  9. The boot-restore "yield to running" check is **any running instance** under
     the entry's launcher (any profile, any port) — never stack a second model
     onto the GPU or clobber a manual start.
  10. `SaveConfigSetting` is depth-2-scalar only and cannot address `servers[]`
      entries (`internal/config/configwrite.go:484`), so a new per-server-entry
      splice is required (item 2).
- **Out of scope / deferred (record nowhere else — these are decided, not open):**
  - Headless/bench startup restore — headless runs never actuate a server.
  - A per-server override for the toggle (top-level only for now).
  - Launcher-side boot autostart — that is a llama-launcher-repo feature.
  - Any version bump (see the closing note).

Precedents to imitate, per item: `/server`'s choice recording
(`RecordServerChoice` seam, `internal/tui/tui.go:662-679`, implemented at
`cmd/apogee/wire_verbs.go:145-155`, wired at `cmd/apogee/wire_options.go:80`) and
the actuation latch (`internal/tui/actuation.go`).

## 1. Config schema: `remember-model` toggle and `launch-profile` pointer — ✅ DONE (2026-08-13)

NOTES (2026-08-13): the registry row and the `/settings` value formatter for `remember-model` — item 6's first two bullets — had to land WITH the schema key, not after it: `TestRegistryIsBijectionWithFileConfig` fails the moment a `fileConfig` field has no registry row (so item 1's own `go test ./internal/config/` acceptance could not pass without it), and `TestSettingValuesCoverEveryRegistryKey` plus the pinned value table in `cmd/apogee/settingsrows_test.go` fail the moment a registry row has no formatter (which would have left `./cmd/apogee/` red for items 3–5). That formatter needs a value to read, so the ordinary resolution chain for a file-only toggle came with it: `fileConfig` → `Layer` → `Settings` → `Options`. Item 6 is left with its live-apply check and its own tests; its registry and pane rows are already in place. Out of item-1 scope by the plan's own Files line: `internal/config/registry.go`, `internal/config/options.go`, `cmd/apogee/settingsrows.go`, `cmd/apogee/settingsrows_test.go`.
NOTES (2026-08-13): `launch-profile:` is documented in the per-server key documentation block (with an inline `launch-profile: gpt-oss-20b` example) rather than added to the commented `# servers:` example entry — that entry is launcher-fronted AND carries `model:`, and showing a pointer beside it would teach the pairing design call 5 refuses.

**What:** In `internal/config/config.go`:

- Add `RememberModel *bool \`yaml:"remember-model"\`` to the top-level config
  struct, next to the other feature toggles (`AutoCompact`/`AutoTitle`,
  `config.go:885-892`), nil = off. Doc comment states it gates BOTH halves of the
  feature: the write on an explicit `/model` pick and the TUI startup restore.
- Add `LaunchProfile string \`yaml:"launch-profile,omitempty"\`` to `ServerEntry`
  (`config.go:1110-1123`), placed next to `LlamaLauncher`. Doc comment: written by
  apogee on a profile-load commit when `remember-model` is on; also hand-settable;
  names the Launch profile the TUI restores at startup when nothing is serving.
- Extend `ValidateServers` (`config.go:1213-1228`): a `launch-profile` value on an
  entry with no `llama-launcher:` key is refused at startup (a pointer with nothing
  to actuate it is a defect in the file, same stance as the existing
  `llama-launcher` checks); a whitespace-only value is refused. Whether the named
  profile exists is deliberately NOT checked here — the launcher's config is read
  fresh at use time (ADR 0029 D4), and boot restore handles a missing profile with
  a note (item 5).

In `internal/config/defaults/config.yaml`: document both keys as commented
examples in house voice — `remember-model` beside the other top-level toggles;
`launch-profile` in the per-server key documentation next to the `llama-launcher`
block (`defaults/config.yaml:104-127`). State plainly: plain servers persist into
the existing `model:` key instead; the launcher's own YAML is never written.

**Files:** `internal/config/config.go`, `internal/config/defaults/config.yaml`,
the config package's existing validation/parse test files (extend in place).

**Tests:** YAML round-trip for both keys; `ValidateServers` cases: `launch-profile`
without `llama-launcher` refused, with it accepted, whitespace-only refused, absent
fine.

**Acceptance:** `go build ./... && go test ./internal/config/`

**Commit:** `feat(config): add remember-model toggle and per-server launch-profile pointer`

## 2. Per-server-entry config writer

Depends on item 1.

**What:** In `internal/config/configwrite.go`, add an exported writer for one key
inside one named `servers:` entry — signature in the spirit of
`SaveServerEntrySetting(path, serverName, key, value string) error` (final name to
house taste). Contract, mirroring `SaveConfigSetting` (`configwrite.go:479-505`):

- Allowed keys are exactly `model` and `launch-profile`; any other key is refused
  before the file is opened (same stance as `validateSettingValue` — a writer that
  trusted its caller is one refactor from corrupting a file).
- Locates the entry by its `name:` in the `servers:` sequence; unknown name →
  error naming the configured entries.
- Sets or replaces the entry's `<key>: <value>` line in place, preserving the
  file's comments, ordering, and the entry's other lines; a re-set to the value
  the file already holds writes nothing.
- Set-only — no delete form. Pointers are never cleared by apogee (design call 6);
  removal is a manual edit.
- Absent config seeds from the embedded template first, and the write is atomic
  and mode-preserving (reuse `ReadConfigForWrite` / `writeConfigAtomically`).

**Files:** `internal/config/configwrite.go`, new
`internal/config/configwrite_server_test.go`.

**Tests:** replace an existing key line; insert the key into an entry that lacks
it; comments and sibling entries byte-identical around the splice; idempotent
re-set writes nothing (file mtime/content unchanged); unknown server name errors;
disallowed key errors; seeding path.

**Acceptance:** `go build ./... && go test ./internal/config/`

**Commit:** `feat(config): per-server-entry setting writer for model and launch-profile`

## 3. Plain servers: record the explicit /model pick

Depends on items 1 and 2.

**What:** A new Options seam modeled on `RecordServerChoice`
(`internal/tui/tui.go:662-679`):

- `internal/tui/tui.go`: `RecordModelChoice func(model string) (recorded bool, err error)`
  with a doc comment in the house register: persists the picked model id into the
  session's current server entry's `model:` key for next startup; `recorded=false`
  (no error) when `remember-model` is off, when the current entry is
  launcher-fronted (design call 5 — its `model:` stays an empty discovery hint),
  or when the session is not on a listed `servers:` entry.
- `cmd/apogee/wire_verbs.go` (beside `recordServerChoice`,
  `wire_verbs.go:145-155`): implement it with item 2's writer; wire it in
  `cmd/apogee/wire_options.go`.
- `internal/tui/picker.go`: call the seam at the explicit-pick site —
  `bindPickedModel` (`picker.go:684`), which both the picker accept
  (`acceptPicker`, `picker.go:646`) and the `/model <id>` argument path reach.
  Verify that claim while implementing; if the argument path binds elsewhere, call
  at both sites. NEVER call it from `applyRebind`/heartbeat observation
  (`internal/tui/model.go:1864`) — passive rebinds do not record (design call 4) —
  and the `--model`/`APOGEE_MODEL` startup override records nothing.
- Surface the outcome the way `RecordServerChoice` does (note on first record,
  silence or note on error — follow the existing pattern exactly; read its call
  site before writing).

**Files:** `internal/tui/tui.go`, `internal/tui/picker.go`,
`cmd/apogee/wire_verbs.go`, `cmd/apogee/wire_options.go`, plus the existing test
files beside each.

**Tests:** TUI side (fake seam): an accepted pick calls the seam once with the
picked id; a nil seam and a `recorded=false` return are both tolerated; a passive
rebind never calls it. cmd side: toggle off → `recorded=false`, no write;
launcher-fronted entry → `recorded=false`, no write; plain entry + toggle on →
writer invoked with the entry name and id.

**Acceptance:** `go build ./... && go test ./internal/tui/ ./cmd/apogee/`

**Commit:** `feat(tui): record explicit model picks into the server entry when remember-model is on`

## 4. Launcher servers: record the loaded profile on commit

Depends on items 1 and 2 (shares files with item 3 — runs serial to it).

**What:** The launcher-class twin of item 3:

- `internal/tui/tui.go`: `RecordLaunchProfile func(profile string) (recorded bool, err error)` —
  persists the profile name into the **actuating entry**'s `launch-profile:` key
  (design call 8): the entry whose `llama-launcher:` key the session's launcher
  path currently follows (`launcherPath.follow`, `cmd/apogee/launcher.go:187`).
  `recorded=false` when `remember-model` is off or the actuating entry cannot be
  identified. Never writes `model:` (design call 5).
- `cmd/apogee/` implementation beside item 3's, using item 2's writer; the
  actuating entry's name must come from what the launcher path was followed from —
  extend the followed state to carry the entry name if it does not already.
- Call sites in `internal/tui/actuation.go`: the two load-commit outcomes of a
  successful profile load — the same-server completion (`foldActuationDone`,
  `actuation.go:344`, the `Move: nil` result) and the move commit
  (`actuation.go:366-380`, where the fold hands off to `foldServerSwitch`). Record
  at commit, not at request — a failed or timed-out load records nothing.
  `/unload-model` and `/stop-server` folds do not touch the pointer (design
  call 6).

**Files:** `internal/tui/tui.go`, `internal/tui/actuation.go`,
`cmd/apogee/launcher.go`, `cmd/apogee/wire_verbs.go`,
`cmd/apogee/wire_options.go`, plus the existing test files beside each.

**Tests:** TUI side (fake seam): a completed same-server load records the profile
name once; a completed move records once; a failed load records nothing; unload
and stop record nothing. cmd side: toggle off → no write; toggle on → writer
invoked with the actuating entry's name and the profile; unidentifiable entry →
`recorded=false`, no error surfaced as failure.

**Acceptance:** `go build ./... && go test ./internal/tui/ ./cmd/apogee/`

**Commit:** `feat(tui): record the committed launch profile on the actuating server entry`

## 5. TUI startup restore through the actuation latch

Depends on items 1 and 4.

**What:** At interactive-TUI startup only — the headless driver
(`cmd/apogee/headless.go`) is untouched — when `remember-model` is on AND the
starting entry carries both `llama-launcher:` and `launch-profile: X`, run one
restore check after wiring, as an initial Bubble Tea command through the existing
seams (nothing new in the engine — it stays wire-silent, ADR 0031):

- Fresh-read the launcher's config (ADR 0029 D4, as `launchProfiles` does at
  `cmd/apogee/launcher.go:224`). `X` not among the defined profiles → transcript
  note that the recorded profile is gone and restore was skipped; done.
- Discover running instances once. ANY instance running under this launcher (any
  profile, any port — design call 9) → yield: join the session's normal startup
  binding and note that the server is already serving and restore was skipped
  (name what it serves when the launcher attributes it); done. If what serves is
  already `X`, the ordinary startup bind is the restore — no actuation, no
  skip-note needed.
- Nothing running → actuate the load of `X` through the existing actuation latch
  (`startProfileLoad`, `internal/tui/actuation.go:226`), exactly as if the user
  had picked it: the latch blocks the same commands, the beat binds a same-server
  load, the completion fold commits a move. No new binding machinery.
- The restore does not re-record the pointer (item 2's idempotent writer makes an
  accidental re-write harmless, but the record seams are for user picks and load
  commits; if the restore reaches item 4's commit fold naturally, that is
  acceptable — the value is unchanged).
- Toggle off, key absent, or launcher disabled → no discovery, no launcher I/O at
  startup at all.

Design the trigger so it cannot race the first heartbeat generation into a double
bind: reuse the latch's existing "beats in its shadow are ignored" rule (ADR 0029
D5) rather than inventing ordering.

**Files:** `cmd/apogee/launcher.go`, `cmd/apogee/wire_live.go`,
`cmd/apogee/wire_options.go`, `internal/tui/tui.go`, `internal/tui/model.go`,
`internal/tui/actuation.go`, plus the existing test files beside each.

**Tests:** with a fake launcher seam: nothing running → load actuated for `X`;
another profile running → no actuation, skip-note emitted; `X` already serving →
no actuation, no skip-note; `X` missing from launcher config → note, no
actuation; toggle off → the seam is never consulted. Headless: no restore path is
reachable (assert by construction or test).

**Acceptance:** `go build ./... && go test ./internal/tui/ ./cmd/apogee/`

**Commit:** `feat(tui): restore the recorded launch profile at startup when the server is idle`

## 6. /settings row for remember-model

Depends on item 1.

**What:** Give `remember-model` the standard top-level-toggle settings surface:

- `internal/config/registry.go`: a bool-kind registry row for `remember-model`
  (making it writable through `SaveConfigSetting` — it is a depth-1 scalar, in
  range).
- `cmd/apogee/settingsrows.go`: a value row beside `auto-title`
  (`settingsrows.go:121`), and the live-apply case following the same pattern the
  other toggles use — a flipped value takes effect for the NEXT record/restore
  decision (the record seams and the boot check read current Options; no
  re-wiring needed).

**Files:** `internal/config/registry.go`, `cmd/apogee/settingsrows.go`, plus the
existing test files beside each.

**Tests:** registry row present, bool vocabulary enforced on write; settings pane
row renders the tri-state (unset/on/off) the way the sibling toggles do;
live-apply flips the Options value.

**Acceptance:** `go build ./... && go test ./internal/config/ ./cmd/apogee/`

**Commit:** `feat(settings): remember-model row, live-editable`

## 7. ADR and CONTEXT.md

Depends on items 1–6 (records what shipped).

**What:** A short ADR at the next free number (expected `docs/adr/0048-…`), titled
in house voice (suggestion: "apogee remembers the model choice per server"),
recording: the persist-the-pointer decision and the rejected alternative (writing
the launcher's YAML — refused on ADR 0029 D4 ownership and preset-vs-state
grounds); the two server classes and their keys; the toggle and its off default;
record-on-explicit-pick only; the actuating-entry pointer home (design call 8);
the any-instance yield rule (design call 9); TUI-only restore. Cross-reference
ADR 0029 (D4 unchanged; restore reuses latch + beat), ADR 0036 D2, ADR 0024.
Then one short paragraph in CONTEXT.md wherever the Launch-profile section
already discusses the session/launcher relationship (CONTEXT.md "Launch profile",
~lines 271-294), defining **remember-model** in the domain language and keeping
the "server profile" avoid-term intact.

**Files:** `docs/adr/0048-apogee-remembers-the-model-choice-per-server.md` (number
to be confirmed free at implement time), `CONTEXT.md`.

**Tests:** none (docs).

**Acceptance:** the ADR file exists, names its status and the cross-referenced
ADRs; `grep -n "remember-model" CONTEXT.md` finds the new paragraph.

**Commit:** `docs(adr): record the remember-model persist-the-pointer decision`

---

**Suggested version bump:** a user-visible feature — after execution, a micro
feature bump (next free 0.13.x, or 0.14.0 if the owner reads it as a minor) with
its `[Unreleased]` entries collected per item. No version identifier is changed by
this plan; the bump is the owner's call.
