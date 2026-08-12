# Per-model profiles — shipped shape table, pattern-keyed config, profile rides Rebind

- **Goal:** stop thinking-tag leaks (live case: minimax-m3's orphan `</mm:think>`) without
  per-switch config surgery: apogee ships a small table of known model shapes that
  auto-applies by model name, the config key becomes a per-model pattern map, and the
  profile joins the bindings a model switch re-resolves.
- **Date:** 2026-08-11
- **Status:** unexecuted
- **sized for:** ~200k-context host
- **Authoritative sources:**
  - Grill session 2026-08-11 (this plan's ratified calls below).
  - `CONTEXT.md` "Model profile" — the two orthogonal axes (tool-call format, thinking
    channel); zero profile = native default. Item 2 amends it.
  - `internal/agent/rebind.go:45-67` — RebindSpec's doc EXCLUDES the profile today
    ("global, not per-model"); this plan reverses that stance, and item 2's ADR says so.
  - `internal/validated/` — the precedent package shape (entries, match, shipped set,
    notice posture) items 3 and 6 mirror. NOTE: profiles deliberately do NOT reuse its
    fingerprint-confidence gate (ratified call 2).
  - ADR 0010 (profile = declarative domain data, translated at the seam), ADR 0023
    (system-prompt-models precedent), ADR 0024 (rebind at the boundary), ADR 0037 +
    ADR 0041 (settings apply live; watcher).
- **Ratified design calls** (owner, 2026-08-11, via AskUserQuestion):
  1. **Scope:** shipped shape table + per-model config keying + profile rides Rebind.
     The seeded template keeps NOT documenting profiles.
  2. **Match rule (shipped table):** case-insensitive substring on the advertised /
     resolved model name; NO fingerprint-confidence gate (medium needs the explicit
     probe battery and would never fire on a remote server — kills out-of-box value).
     The tag shape is a chat-template/family fact that survives quants and provider
     spellings.
  3. **Global `model-profile:` block is retired** — a config that still spells it gets
     a LOUD startup error whose message shows the new map spelling. No back-compat
     layer (pre-production).
  4. **A matching user entry replaces the WHOLE profile** (both axes) — the
     system-prompt-models rule; no per-axis merge (absent style already means
     native/none, merge would need an 'inherit' spelling).
  5. **User map keys use the same pattern rule** as the shipped table; longest pattern
     wins among user entries; ANY user match beats ANY shipped match.
  6. **Both engine doors stay:** RebindSpec gains a Profile field (model switch applies
     it atomically with prompt/mechanisms); SetProfile remains the config-edit
     same-model door; both funnel into one internal swap.
  7. **One-line notice** when a SHIPPED entry auto-applies (first debugging clue when a
     match is wrong). A user-map match applies silently — the user wrote it.
  8. **Shipped entries launch as the verified trio only:** gemma-family `<think>`
     (ported oracle), gpt-oss harmony (live runs), minimax-m3 `<mm:think>` (session
     2026-08-11). Grow per sighting; unmatched models stay zero-profile pass-through.
- **Plan-writer calls** (recorded, not owner-ratified; veto by NOTES line):
  - New package `internal/profiles` mirroring `internal/validated`'s shape; the shipped
    table is a Go literal (3 entries — no JSON machinery needed).
  - Escape hatch for a false-positive shipped match: a user entry with
    `thinking: {style: none}` — no separate off-switch key.
  - Shipped entries carry full profiles (both axes); the trio all use native tool
    calls, so only the thinking axis is non-zero today.
- **Standing requirements:**
  - skills: coding-standards
  - Run `make check` before every commit.
  - Each user-visible item adds one bullet to `CHANGELOG.md` under `[Unreleased]`.
  - Never change `VERSION` or add a CHANGELOG release heading (VERSION-SUGGESTION at
    the end instead).
  - Any authorized deviation from item text lands as a dated NOTES line under the item.
- **Out of scope:**
  - Documenting profiles in the seeded template (stays the one undocumented key —
    `internal/config/registry.go:139` note updates its spelling only).
  - Fingerprint/probe integration and validated-sets changes.
  - Emit-side profile behaviour (menu rendering / tools suppression) beyond riding the
    new resolution — the mechanism itself is untouched.
  - Per-axis merge semantics; a future 'inherit' spelling if ever wanted.

## 1. Commit the orphan-closer stripper fix (already in the working tree) — ✅ DONE (2026-08-12)

NOTES (2026-08-12): the code+tests were already committed before this run as `92b0e52`
(subject `feat(tests): add tests for orphan closer handling in StripThinking function`,
only `internal/processing/thinking.go` + `thinking_test.go`), so no new code commit was
made and the planned commit subject was not used. Verified: `92b0e52` is an ancestor of
HEAD, both files are unchanged since it, `TestStripThinking_OrphanCloser_ImplicitLeadingSpan`
carries all 5 cases, `go test ./internal/processing/` and `make check` pass. The acceptance
line `git show --stat HEAD` no longer applies (HEAD has moved on); it was checked against
`git show --stat 92b0e52` instead. That commit carried no CHANGELOG bullet, so this run adds
one under `[Unreleased] → Fixed` per the plan's standing requirement — the item's "exactly
this pair of files" wording guarded against the unrelated `compact.go` change, which is not
in the tree.

**What:** The fix and its tests are already implemented (session 2026-08-11):
`internal/processing/thinking.go` — StripThinking treats an EndToken that appears
before any StartToken as closing an implicit span opened at position 0 (the pre-opened
chat-template case; live shape: content that begins `</mm:think>`), plus
`TestStripThinking_OrphanCloser_ImplicitLeadingSpan` in
`internal/processing/thinking_test.go`. Review the diff, run the checks, commit exactly
this pair of files. NOTE: `internal/agent/compact.go` has an UNRELATED in-flight owner
change — do not include it.

**Tests:** the new test table (5 cases: bare closer, closer+answer, pre-opened
reasoning, orphan-then-normal-span, non-orphan control); existing oracle vectors
unchanged.

**Acceptance:** `go test ./internal/processing/` passes; `make check` passes; `git show
--stat HEAD` lists only thinking.go and thinking_test.go.

Commit: `fix(processing): strip an orphan thinking closer as an implicit leading span`

## 2. ADR 0044 + CONTEXT.md: the profile is per-model and mostly shipped — ✅ DONE (2026-08-12)

NOTES (2026-08-12): two departures from the literal text. (a) Beyond the "Model profile" section,
one clause of CONTEXT.md's neighbouring **Launch profile** entry was edited — it asserted "the
Model profile is global and stands through it", the exact claim this item retires, and no other
plan item owns that line; the edit is the minimal reword to "re-resolved for that model when the
load lands". (b) No CHANGELOG bullet: this item is docs-only (ADR + concept map) with no
user-visible behaviour change — the standing one-bullet requirement is scoped to user-visible
items, and the user-visible bullets land with items 4 and 6. The stale `internal/agent` doc
comments were left to item 5 per the item's own instruction (item 5 has not landed).

**What:** Write `docs/adr/0044-model-profiles-are-per-model-and-mostly-shipped.md`:
context (the minimax leak; the global block's churn-on-every-switch; rebind.go's
documented exclusion), decision = ratified calls 1-8 above, consequences (rebind.go's
"global, not per-model" note reversed — quote it; SetProfile's narrowed role;
validated-sets' confidence gate deliberately NOT reused, and why that is safe here:
a wrong profile is visible and reversible, no bench-honesty claim rides on it).
Update `CONTEXT.md` "Model profile" (~line 202): per-model resolution
(user pattern map ▸ shipped table ▸ zero), the notice, and the retired global key;
keep the Avoid list, add _Avoid_: "global profile". Update the stale doc comments that
item 5 will touch anyway ONLY if item 5 lands in a different session — otherwise leave
them to item 5.

**Tests:** none (docs).

**Acceptance:** ADR references 0010/0023/0024/0037/0041; CONTEXT.md section states the
three-layer precedence in one sentence; `make check` passes.

Commit: `docs(adr): ADR 0044 — per-model profiles with a shipped shape table`

## 3. `internal/profiles`: entries, pattern match, shipped trio — ✅ DONE (2026-08-12)

NOTES (2026-08-12): three recorded calls. (a) No CHANGELOG bullet — the package is internal and
wires to nothing yet, so nothing is user-visible until items 4/6 (same reading of the standing
requirement item 2 took). (b) The shipped table is reached through `Shipped() []Entry`, which
returns a COPY, rather than an exported slice var — an exported mutable table is editable by any
caller; item 6's snippet `profiles.Shipped` therefore needs parens. (c) Two details the item text
left open: an EMPTY pattern never matches (it is a substring of every name and would profile every
model), and an equal-length tie gives the lexicographically SMALLER pattern.

**What:** New package `internal/profiles` (shape mirrors `internal/validated`):
`Entry{Pattern string; Profile domain.ModelProfile; Note string}`;
`Resolve(model string, user, shipped []Entry) Decision` where `Decision{Profile
domain.ModelProfile; Source Source; Entry Entry}` and `Source` ∈ {SourceNone,
SourceUser, SourceShipped}. Matching: case-insensitive substring of Pattern in model;
longest pattern wins within a tier; equal-length tie breaks lexicographically (stable,
testable); any user match beats any shipped match; no match ⇒ zero profile +
SourceNone. Shipped table (Go literal): `gemma` → delimited `<think>`/`</think>`;
`gpt-oss` → harmony; `minimax-m3` → delimited `<mm:think>`/`</mm:think>`. All three
native tool-call format (zero value).

**Tests:** table-driven Resolve tests: the live spellings `minimax/minimax-m3:exacto`,
a gguf-ish `minimax-m3-Q4_K_M`, `gpt-oss-20b`, `gemma-4-e4b-it-qat`; case-insensitivity;
longest-wins within a tier; user-beats-shipped even when the shipped pattern is longer;
`thinking: none` user entry overriding a shipped match to zero thinking; no-match ⇒
zero profile.

**Acceptance:** `go test ./internal/profiles/` passes; `make check` passes; package has
a doc.go stating the match rule and the no-confidence-gate call (ADR 0044).

Commit: `feat(profiles): pattern-matched model-profile resolution with a shipped shape table`

## 4. Config: `model-profiles:` pattern map; retire `model-profile:` — ✅ DONE (2026-08-12)

NOTES (2026-08-12): the item is scoped "In `internal/config`", but the registry-row rename forces
edits outside it; five calls recorded. (a) cmd/apogee's three registry-PINNED tables follow the
rename mechanically (`settingSections`, `settingValues`, `settingStructures`) — their coverage tests
fail otherwise and `make check` cannot pass. The row now shows the entry COUNT ("2 model profiles")
like every other block of entries, since no single line can name the profile in force without
knowing which model is bound; `profileSummary` went with its only call site. (b) The dead
global-profile plumbing is LEFT standing for item 6, which owns deleting it: `Layer.Profile`,
`Settings.Profile`, `Options.Profile`, the applier's `case "model-profile"` and `reloadProfile` all
survive, now fed by nothing. Consequence until item 6 lands: a pane edit of `model-profiles:` reports
cannot-apply (pinned by `TestApplySettingRefusesEveryKeyItCannotReach`), and the two wire_test.go
tests of the old door (`TestApplySettingModelProfileSwapsTheDialect`,
`…RefusalIsReported`) were DELETED — their fixtures spell the retired key, which now hard-errors at
load, and the door they cover is item 6's to re-home. (c) `internal/probe.ProfileYAML` gained the
probed model name and emits a `model-profiles:` entry keyed by it: no item owns the probe, but
leaving it would have `apogee probe model` printing a paste-ready block that hard-errors at the next
launch. (d) One README line and one `internal/tui` doc comment were respelled to the new key (the
`internal/agent` comments stay with item 5). (e) The retired-key refusal lives in configmigrate.go
beside the `llama-launcher:` one — same posture, same refuse-before-any-write ordering.

**What:** In `internal/config`: add `ModelProfiles map[string]modelProfileConfig
`yaml:"model-profiles"`` (key = pattern; reuse the existing block schema at
`config.go:1304-1331` unchanged) surfacing as ordered `[]profiles.Entry` (sort keys for
determinism). A present `model-profile:` key (`config.go:909`) becomes a LOUD startup
error: "model-profile: was replaced by the per-model map — spell it: model-profiles:
{\"<pattern>\": {...}}" with the user's own block echoed as the example. Update the
registry row (`registry.go:332`) to `model-profiles` and the `registry.go:139` comment;
it stays the one key the template does not document. Layering: the map participates in
config-layer precedence like `mechanisms` (a present map in a nearer layer replaces the
farther one whole).

**Tests:** parse a two-entry map; the retired-key error message contains the new
spelling; layer precedence (project map over global map); registry test updates.

**Acceptance:** `go test ./internal/config/` passes; `make check` passes; a config
containing the old key fails startup with the migration message.

Commit: `feat(config): model-profiles pattern map replaces the global model-profile block`

## 5. Engine: RebindSpec carries the profile; one internal swap

**What:** In `internal/agent`: add `Profile domain.ModelProfile` to `RebindSpec`
(`rebind.go:55-67`) and apply it in Rebind through the SAME internal swap SetProfile
uses (extract the parser/stripper replacement from `setprofile.go:58` into an
unexported `applyProfile`; both doors call it). Rewrite the rebind.go:51-54 doc
paragraph that excludes the profile (quote-reverse per ADR 0044) and the stale half of
`setprofile.go:7`'s "leaves it alone when the Upstream's loaded model changes" comment.
Rebind stays atomic and idle-only; SetProfile's mid-Exchange refusal is unchanged.

**Tests:** Rebind with a delimited profile swaps the stripper (a subsequent parse
strips); Rebind with a zero profile resets to no-op parsers; SetProfile behaviour
unchanged (existing tests); mid-Exchange Rebind/SetProfile refusals unchanged.

**Acceptance:** `go test ./internal/agent/` passes; `make check` passes; no remaining
comment in internal/agent claims the profile is global.

Commit: `feat(engine): RebindSpec carries the model profile through one internal swap`

## 6. Root wiring: resolve at startup, switch, and config edit — with the notice

**What:** In cmd/apogee (composition root — the engine never reads config, per
RebindSpec's contract): resolve `profiles.Resolve(model, userEntries, profiles.Shipped)`
(a) at startup for the bound model, (b) wherever RebindSpec is computed for an observed
model change (the ADR 0024 seam), filling the new Profile field, and (c) on a watcher /
settings edit of `model-profiles:` — re-resolve for the CURRENT model and apply via
SetProfile (the ADR 0037 per-key door). On SourceShipped emit the one-line notice at
startup/switch: `model profile: <pattern> (built-in) — thinking: <style>`; SourceUser
and SourceNone are silent. Delete the now-dead global-profile plumbing the retired key
fed.

**Tests:** resolution-seam unit tests where cmd/apogee's wiring is testable (rebind
spec builder gets Profile filled per map+table; notice emitted only for SourceShipped);
a live check against the owner's setup is manual (closing note below).

**Acceptance:** `make check` passes; switching models mid-session re-resolves the
profile (verified by the rebind-spec test); CHANGELOG bullet added; grep finds no
consumer of the retired global block.

Commit: `feat(apogee): per-model profile resolution wired at startup, rebind, and config edit`

---

**Closing notes:**

- **Owner migration (out of repo):** `~/.apogee/config.yaml` gained a global
  `model-profile:` block for minimax on 2026-08-11 — after item 4 it errors at startup.
  Delete it (the shipped table now covers minimax-m3), or respell as
  `model-profiles:` if the owner wants an explicit override. NOTE: another live apogee
  session may be running — the watched config applies edits live; coordinate with the
  owner before touching it.
- **Live verification (manual, owner-triggered):** run a minimax-m3 session on the new
  binary; the reply must carry no `</mm:think>`, and the notice line must show the
  built-in match.
- **VERSION-SUGGESTION:** minor bump (new config key + retired key + shipped table) —
  suggest `v0.13.0`; do not apply unasked.
