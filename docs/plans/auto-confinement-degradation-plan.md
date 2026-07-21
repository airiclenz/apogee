# Auto-mode confinement degradation — notice + in-place offer — implementation plan

**Date:** 2026-07-21. Origin: `ISSUES.md` (owner report, "Auto mode is still asking for terminal
commands") triaged the same day in commit `56da4dc`; scope extended from a passive notice to a
notice **plus an offer to apply the fix** in `c8ab70b`. The parked design and its constraints live
in `TODO.md → "Auto-mode confinement degradation is silent"` — **that section is this plan's
ground truth for the constraints**; ADR 0012 (as amended by item 1) is the ground truth for the
policy. Where an item below disagrees with ADR 0012-as-amended or the TODO constraint list, those
documents win.

## The problem in one paragraph

`resolveLadderAuto` (`internal/agent/resolution.go:317-322`) sends a subprocess tool to `Confine`
when the backend can fence it and to `Gate` when it cannot — ADR 0012's *"confine if you can, gate
if you can't"*. On a host where `Confiner.Capabilities().FSWrite == false` that is an Approval
prompt on **every** terminal command. This is correct behaviour, but nothing tells the user it is
happening or why, so Auto reads as broken. It is not an edge case: `landlock_create_ruleset`
returns **`ENOSYS`** in most containers regardless of kernel version (verified 2026-07-21 on
kernel 6.18.15 — well past the 5.13 floor — `NewConfiner()` → `*landlockConfiner`,
`FSWrite=false`, errno 38), so the degraded path is the *common* path for containerised users.

## Decisions this plan implements (summary)

1. **The ladder does not change.** No item may make `resolveLadderAuto` auto-run unconfined
   subprocesses on its own initiative. The fix is *visibility* plus an easier route to the user's
   own sanctioned decision — never the tool deciding to loosen. (TODO "Explicitly NOT the fix".)
2. **A capability-aware startup notice** when Auto is entered on a backend that reports
   `FSWrite == false`, at the existing notice site in `cmd/apogee/wire.go` — the mirror branch of
   the unconfined-Auto warning already at `wire.go:139-143`.
3. **The accept path is a slash command** (owner decision, 2026-07-21): `/confine off` for this
   session, `/confine off --save` to persist. Chosen over a startup y/N prompt or an extra choice
   on the Approval prompt because it is a distinct affirmative action taken *after* the frustration
   moment, reuses the existing chat mini-language, and invents no new interaction surface.
   It satisfies TODO constraint 2 (no default-yes, no enter-to-accept, no remembered "always").
4. **The persisted acknowledgement is host-scoped** (owner decision, 2026-07-21), resolving the
   open question the TODO flagged. `confine-to-workspace` stays a global key with its current
   meaning; a new file-only `unconfined-hosts:` list records *which machines* the user has
   acknowledged as disposable:

   ```yaml
   confine-to-workspace: true      # global default, unchanged

   unconfined-hosts:               # explicit per-host acknowledgement
     - id: "devbox-a1b2c3"
       acknowledged: "2026-07-21"
       note: "disposable container, landlock unavailable"
   ```

   A throwaway container's acknowledgement therefore never silently follows the config onto a
   laptop. The existing global `confine-to-workspace: false` still works and still means "every
   host" — it is not deprecated by this plan.
5. **Session scope is offered ahead of persistence** (TODO constraint 3): `/confine off` alone is
   session-only and writes nothing.
6. **Truthful wording** (TODO constraint 1): the notice and the command's confirmation state the
   blast radius and must not be phrased as repairing a malfunction.

## Host identity — the interlock, and its honest limits

Item 2 adds `platform.HostID()`. It is a **safety interlock, not an authentication mechanism**:
its job is to stop an acknowledgement silently travelling between machines, not to resist forgery.
Anyone who can edit `config.yaml` can write any id they like — that is fine and expected, exactly
as `internal/security/doc.go` says the dangerous-action guard "is NOT a security boundary."

Composition: `<sanitized hostname>-<first 6 hex of sha256(machine identifier)>`, where the machine
identifier is the first available of `/etc/machine-id`, `/var/lib/dbus/machine-id`, else the
hostname itself. **Accepted trade-off, to be stated in the doc comment:** an ephemeral container
that gets a fresh machine-id per run will not match a stored acknowledgement and will re-prompt.
That fails *closed* (safe direction), and `/confine off` without `--save` is always available for
those users, so the annoyance has a one-word answer.

## Verify gate (every item)

`make check` — gofmt-clean, `go vet`, `go build ./...`, `go test -race -count=1 ./...`. An item is
not done until it is green. Each item also carries its own targeted test, named in its Acceptance.

Every item that changes user-visible behaviour adds its own CHANGELOG entry under a new
`## [Unreleased]` block (the `[1.4.0]` section was cut 2026-07-21 and must not be edited).

Any authorized deviation from an item's text lands as a dated `NOTES:` line under that item.

---

## 1. ADR 0012 amendment — the host-scoped acknowledgement and the offer — ✅ DONE (2026-07-21)

NOTES (2026-07-21): CONTEXT.md gained the **Host acknowledgement** term; the neighbouring
`confine-to-workspace` entry also gained one clause so its "the only blanket *loosen*" claim stays
true beside the new term (the host acknowledgement is that same loosen, host-scoped) — an edit
inside this item's CONTEXT.md allowance, recorded here because it touches an existing entry.

**This is the design of record; every later item cites it.** It lands first so no implementation
item is written against a document that contradicts it.

Amend `docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md` with a
dated addendum recording: (a) `confine-to-workspace` remains the
global blanket loosen with unchanged meaning; (b) a new **host-scoped acknowledgement**
(`unconfined-hosts:`) expresses "this machine is disposable" without making that claim globally,
because the flag is global while the claim is host-specific; (c) the tool may now *offer* the
loosen via `/confine off`, and why that does not weaken the ADR's "explicit acknowledgement"
posture — the offer lowers the friction of *finding* the decision, never the deliberateness of
*making* it; (d) the ladder itself is untouched, and auto-loosening remains forbidden.

If CONTEXT.md gains a term here (candidate: **Host acknowledgement**), this item owns that edit —
no later item may add it.

**Acceptance:** gates green; `docs/adr/` + optionally `CONTEXT.md` only; no code change in the
diff. Commit: `docs(adr): host-scoped confinement acknowledgement and the in-place offer (ADR 0012)`.

---

## 2. `platform.HostID()` — the machine interlock — ✅ DONE (2026-07-21)

NOTES (2026-07-21): three implementation choices beyond the item's literal text, all inside its
`internal/platform` diff allowance. (a) `readMachineID(paths []string)` takes its paths as a
parameter — a second injectable seam beside `hostIDFrom`, so the *file* fallback chain
(`/etc/machine-id` → `/var/lib/dbus/machine-id` → none) is tested against temp files rather than
whatever the test machine happens to have. (b) `HostID` memoizes through `sync.OnceValue`, making
"deterministic within a process" structural rather than a property of the files staying put.
(c) `sanitizeHostLabel` trims leading/trailing dashes after the `[A-Za-z0-9_.-]` substitution, so
the id can never open with `-`; a label that sanitizes away entirely reads `unknown`, and the
hash still carries the identity. `internal/platform/doc.go` gained one paragraph saying why
HostID lives in this package — package godoc for a file this item creates.

New file `internal/platform/hostid.go` (+ `hostid_test.go`). `HostID() string` per the composition
above, with the doc comment stating the interlock-not-authentication framing and the ephemeral-
container trade-off verbatim from this plan's section.

Requirements: deterministic within a process and across runs on the same machine; never returns
empty (fall back to `"unknown-"+hash` if `os.Hostname()` itself errors); sanitize the hostname to
`[A-Za-z0-9_.-]` so the value is safe as a YAML scalar without quoting surprises; no shelling out
to `ioreg`/`hostnamectl` (keep it dependency-free and Windows-safe for Phase 5).

Make the identifier sources injectable (unexported `hostIDFrom(hostname string, machineID []byte)`)
so tests pin the composition without depending on the test machine.

**Acceptance:** gates green; new test proves determinism, the fallback chain, sanitization, and
non-empty on error. Diff confined to `internal/platform` + CHANGELOG.
Commit: `feat(platform): HostID — a stable per-machine interlock for confinement acknowledgement`.
**Depends on:** item 1.

---

## 3. Config — the `unconfined-hosts:` list and its resolution — ✅ DONE (2026-07-21)

NOTES (2026-07-21): three shaping choices the item's text left open, all inside its `cmd/apogee`
diff allowance. (a) The ladder stays *in* `settings` as required, but as a named pure function
`resolveConfineToWorkspace(explicit *bool, hosts []unconfinedHost, hostID string) (bool, []string)`
that `resolveSettings` calls — so the three-way order is table-testable on its own. That required
two signature changes: `resolveSettings` takes the current `hostID` (injected, so the ladder is
pinned off whatever machine the tests run on — `applyConfig` passes `platform.HostID()`) and
returns the soft notices beside the settings; `applyConfig` takes a `notify func(string)` seam for
them, mirroring `resolveContextWindow`'s existing notify parameter in the same file rather than
writing to `os.Stderr` from a dependency-injected function. `root.go` passes `cmd.PrintErrln`, so
the notice still lands on stderr pre-alt-screen. (b) An explicit `confine-to-workspace: true`
does **not** veto a matching acknowledgement — that is the ADR's literal order (only an explicit
*false* short-circuits), and it is what makes the list usable beside the default-true flag; the
behaviour is spelled out in the function's doc comment and pinned by a test case. (c) An entry
whose `id` is whitespace-only is treated as the item's "empty `id`" (the value is trimmed before
both the malformed check and the match), so a blank id can never match a degenerate empty host id.

In `cmd/apogee/config.go`: add a `UnconfinedHosts []unconfinedHost` field to `fileConfig` (yaml tag
`unconfined-hosts`; entry fields `id`, `acknowledged`, `note`), threaded through
`layer` → `settings` → `options`
**file-only**, exactly like `confine-to-workspace` (global-config-only per ADR 0012 —
see the existing comments at `config.go:38-42` and `config.go:109-111`).

Resolution order in `settings`, to be pinned by test:

1. explicit `confine-to-workspace: false` → effective `false` (global loosen, unchanged);
2. else current `platform.HostID()` matches an `unconfined-hosts[].id` → effective `false`;
3. else `true` (the default).

A malformed entry (empty `id`) is a **soft** skip with one stderr notice naming the problem —
never a blocked startup (the ADR 0016 posture the validated-set surface already established;
`cmd/apogee/validatedsets.go:19-33` is the precedent). Unknown ids are simply "not this host", not
errors — the list is expected to accumulate machines.

Add the commented block to `cmd/apogee/defaults/config.yaml` beside the existing
`# confine-to-workspace: true` (line ~45), with the same explanatory density as its neighbours.

**Acceptance:** gates green; table test over the three-way resolution including a matching and a
non-matching host id and a malformed entry; a test asserting the key is file-only (not settable by
flag or env, matching the `confine-to-workspace` precedent). Diff confined to `cmd/apogee` +
CHANGELOG. Commit: `feat(config): host-scoped unconfined-hosts acknowledgement`.
**Depends on:** items 1, 2.

---

## 4. The capability-aware startup notice

In `cmd/apogee/wire.go`: hoist the inline `platform.NewConfiner()` at line 120 into a local so its
`Capabilities()` can be read, then add the mirror branch beside the existing unconfined warning at
`wire.go:139-143`:

> Auto **and** effective `confineToWorkspace == true` **and** `caps.FSWrite == false`
> → print the degradation notice.

The message must name the active backend, say plainly that commands cannot be fenced on this host
and will therefore ask for approval, and point at `/confine off` (session) and `/confine off --save`
(persist). It must **not** be worded as fixing a malfunction (TODO constraint 1). Suggested text —
the implementer may improve the prose but not the semantics:

```
apogee: auto mode is gating terminal commands — the landlock backend on this host reports no
  filesystem confinement (ENOSYS), so commands cannot be fenced and fall back to approval.
  To run unconfined instead (safe ONLY on a disposable machine):
    /confine off          — this session
    /confine off --save   — and remember this host in ~/.apogee/config.yaml
```

Build it as a **pure function** (`confinementDegradedNotice(backendName string, caps
domain.ConfinementCaps, mode, confine bool) string` returning `""` when not applicable) so it is
table-testable without capturing `os.Stderr` — the `contextWindowNotice` / `appliedNotice` pattern
(`wire.go:58`, `validatedsets.go:86-89`).

Notice only when Auto is actually selected (TODO constraint 6) — never in the other three modes,
which make no confinement promise.

**Acceptance:** gates green; table test covering the matrix {mode auto/non-auto} × {FSWrite
true/false} × {confine true/false} asserting exactly one cell produces a notice, and that the
existing unconfined-Auto warning still fires on its own cell. Diff confined to `cmd/apogee` +
CHANGELOG. Commit: `feat(cli): notice when auto mode degrades to approval on an unfenceable host`.
**Depends on:** items 1, 3.

---

## 5. Runtime toggle — `Agent.SetConfineToWorkspace`

Mirror the existing `SetMode` precedent **exactly** (`internal/agent/agent.go:43-47`, `:189-202`):
`Mode` is already a live field seeded from `cfg.Mode` and swapped from the UI under `modeMu`.

- Add a live `confineToWorkspace bool` field to `Agent`, seeded from `cfg.ConfineToWorkspace` at
  construction.
- Add `SetConfineToWorkspace(bool)` and a matching accessor, guarded by the same mutex that guards
  `mode` (or a sibling — follow whatever the existing code does), documented as safe to call from
  the UI goroutine while the worker drives a Step.
- Change `internal/agent/dispatch.go:113` to read the **live** value instead of
  `a.cfg.ConfineToWorkspace`, so the change takes effect on the next tool call.
- `apogee.Agent` is a type alias (`apogee.go:52`), so the method reaches the public surface with no
  facade edit — **additive → minor**, same shape as the `Budget` methods in `v1.4.0`. Note this in
  the CHANGELOG entry.
- Sub-agents: confirm a child spawned *after* a toggle inherits the live value, and pin it with a
  test (`internal/agent/subagent.go` is the relevant seam).

**Acceptance:** gates green; a race-detector test toggling from one goroutine while another
dispatches, a test proving the next tool call observes the new value, and the sub-agent inheritance
test. Diff confined to `internal/agent` + CHANGELOG.
Commit: `feat(agent): SetConfineToWorkspace — runtime toggle mirroring SetMode`.
**Depends on:** item 1.

---

## 6. The `/confine` command — parse, autocomplete, docs

Follow commit `5f6209a` (`/new`) as the worked example of adding a verb; it touched exactly
`command.go`, `autocomplete.go`, `doc.go`, and the three test files.

Grammar:

| Input | Meaning |
| --- | --- |
| `/confine` or `/confine status` | report backend, capabilities, effective setting, host id |
| `/confine off` | unconfined **for this session only**; writes nothing |
| `/confine off --save` | as above **and** persist this host to `unconfined-hosts` |
| `/confine on` | re-enable confinement for this session |

Add `SetConfineToWorkspace(bool)` to the `Engine` interface (`internal/tui/tui.go:34-61`) beside
`SetMode`, with a doc comment in the same register. Extend the `/` autocomplete menu and
`internal/tui/doc.go`'s mini-language list.

This item is **parse + surface only** — routing behaviour is item 7. An unknown subcommand
(`/confine sideways`) is a parse error carrying the usage line, never a silent no-op.

**Acceptance:** gates green; parser table test over every form above plus the error cases (unknown
subcommand, unknown flag, `--save` without `off`); autocomplete test asserting `/confine` is
offered. Diff confined to `internal/tui` + CHANGELOG.
Commit: `feat(tui): /confine command surface in the chat mini-language`.
**Depends on:** items 1, 5.

---

## 7. `/confine` routing and the session toggle

Route the parsed command in `runCommand`: `off`/`on` call `Engine.SetConfineToWorkspace`;
`status` renders the report. All are synchronous and idle-safe (no worker), like `/clear` — see
the `Engine` interface's single-driver note at `tui.go:30-33`.

The confirmation the user sees after `/confine off` must state the blast radius in one line — Auto
will now run every command unfenced with the user's full privileges — and say whether it was
session-only or persisted. Reuse the transcript notice rendering the validated-set banner work
uses if it has landed; otherwise a plain system line is fine (do **not** build a banner framework
here).

`/confine off` when confinement is already off, or on a host where `FSWrite == true`, is allowed
but says so plainly (it is a legitimate choice, just not the degraded case).

**Acceptance:** gates green; TUI-level tests driving each form through a fake Engine and asserting
the Engine call and the rendered line, including the already-off and capable-host cases. Diff
confined to `internal/tui` + CHANGELOG.
Commit: `feat(tui): route /confine — session toggle and status report`.
**Depends on:** item 6.

---

## 8. The comment-preserving config writer (`--save`)

New `cmd/apogee/configwrite.go` (+ test). Appends an `unconfined-hosts` entry for the current host
to `~/.apogee/config.yaml`.

Requirements (TODO constraint 4 — this is the fiddly item, budget accordingly):

- **Preserve comments and formatting.** `gopkg.in/yaml.v3` is already a dependency; round-trip
  through `yaml.Node` rather than unmarshal→marshal, which would flatten the template's extensive
  explanatory comments.
- **The key ships commented out.** In the seeded template `confine-to-workspace` is
  `# confine-to-workspace: true` and `unconfined-hosts` (item 3) will likewise be commented. The
  writer must therefore **insert** the block when absent, not merely substitute a value, and must
  stay correct against a config the user has since reordered or rewritten by hand.
- **Idempotent.** Saving twice for the same host id must not duplicate the entry.
- **Atomic and safe.** Write temp + rename in the same directory; preserve the existing file mode
  (config may hold endpoint details; do not widen permissions).
- **Absent file.** Seed from the embedded template first via the existing path in
  `cmd/apogee/defaults.go`, then append — never write a bare fragment.
- **Report what changed.** Return the file path and the added entry so item 7's confirmation can
  name them (TODO constraint 4: visible and reversible).

**Acceptance:** gates green; golden-file tests proving comments survive a round-trip, insertion
when the key is absent/commented, idempotence on repeat save, correct behaviour against a
hand-reordered config, mode preservation, and that a write failure surfaces as an error rather
than a silent success. Diff confined to `cmd/apogee` + CHANGELOG.
Commit: `feat(cli): comment-preserving config writer for the host acknowledgement`.
**Depends on:** item 3.

---

## 9. End-to-end acceptance — the whole loop on a simulated incapable host

One test proving the user journey the issue describes, with a fake `Confiner` reporting
`FSWrite=false` (the `denyConfiner` at `internal/platform/platform.go:57-63` is exactly this, or
`confinetest` helpers if they fit):

1. Auto on an incapable backend → a terminal call gates **and** the degradation notice was
   produced (item 4's pure builder, asserted directly — do not scrape stderr).
2. `/confine off` → the next terminal call runs without gating.
3. `/confine off --save` → the config file now contains this host's entry.
4. A fresh resolution with that config on the same host id → effective `confineToWorkspace=false`
   with no notice; **on a different host id → confined again and the notice returns.** This is the
   load-bearing assertion for decision 4 — it is the whole reason the acknowledgement is
   host-scoped.

**Acceptance:** gates green; the test fails if any single item's wiring is reverted. Diff confined
to test files + CHANGELOG. Commit: `test: end-to-end auto-degradation notice, /confine, and the host-scoped save`.
**Depends on:** items 2–8.

---

## 10. Docs closeout (owning item for the residue)

The single owner of every remaining doc edit — no earlier item may do these:

- **`README.md`** — document `/confine` in the command list and `unconfined-hosts:` in
  Configuration. Check the Auto-mode paragraph corrected in `ac3b856` still reads true afterwards.
- **`ISSUES.md`** — close the Auto-mode entry (it points at the TODO section this plan implements).
- **`TODO.md`** — remove the now-implemented "Auto-mode confinement degradation is silent" section,
  or reduce it to any deliberately-deferred residue (e.g. `apogee probe`, which stays Phase 5).
- **`CHANGELOG`** — verify the `[Unreleased]` block reads as one coherent feature rather than ten
  disconnected item lines; consolidate if not.
- **`cmd/apogee/defaults/config.yaml`** — final read-through for accuracy against shipped behaviour.

**Acceptance:** gates green (docs-only otherwise); `git status` clean after commit; a grep proving
no doc still tells the user to hand-edit `confine-to-workspace` as the *only* route.
Commit: `docs: close out the auto-confinement-degradation work`.
**Depends on:** items 1–9.

---

## Explicitly NOT in this plan

- **Any change to `resolveLadderAuto` or the mode ladder.** The gating behaviour is correct; this
  plan makes it visible and gives the user a sanctioned route to their own decision. Auto-loosening
  without an explicit user act stays forbidden (ADR 0004/0012).
- **A y/N prompt at startup or an extra choice on the Approval prompt.** Both were considered and
  rejected (decision 3) — they put the acknowledgement at the moment of peak frustration, which is
  the click-through-consent trap TODO constraint 2 exists to prevent.
- **`apogee probe`** — reporting the confinement backend as a subcommand remains merge-plan
  Phase 5 work. Item 7's `/confine status` covers the diagnostic need from inside the TUI; the
  standalone subcommand needs the Phase 5 CLI structure that does not exist yet.
- **A TUI banner framework.** Item 7 uses whatever transcript-notice rendering exists. The
  validated-set in-transcript banner (deferred follow-up 04) is the item that owns building one.
- **Deprecating the global `confine-to-workspace` key.** It keeps its current meaning; the
  host-scoped list is additive beside it.
- **Fixing the landlock unavailability itself.** Nothing to fix — the probe is correct and the
  kernel facility is genuinely absent in containers. Any attempt to "enable landlock" is out of
  scope and almost certainly not possible from inside the process.
- **The separately-filed context-size / usage-gauge report** in `ISSUES.md` — untriaged and
  unrelated.
