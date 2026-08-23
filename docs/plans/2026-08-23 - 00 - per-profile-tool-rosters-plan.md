# Per-profile tool rosters — the Model profile's third axis

**Goal:** land ADR 0057 — a `model-profiles:` entry carries a `tools: {disabled, enabled}` delta
axis against the default roster; tools gain a build-level default-off state with a global
`tools.enabled:` lift; precedence is profile > global > build; profile resolution becomes
axis-wise across all three axes; the resolved roster rides Rebind and announces non-empty deltas
in one line at a switch.

**Date:** 2026-08-23 · **Status:** unexecuted · **sized for:** ~200k-context host

**Authoritative sources** (an item that disagrees with these follows these):
- [ADR 0057](../adr/0057-the-tool-roster-is-a-third-model-profile-axis-resolved-axis-wise.md)
  — the ratified decision, all eight calls plus the stated bounds.
- [ADR 0044](../adr/0044-model-profiles-are-per-model-and-mostly-shipped.md) — the profile map,
  match rule and Rebind ride; its decision 4 is superseded axis-wise per ADR 0057.
- [ADR 0031](../adr/0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)
  — the engine never reads config; resolution stays in the composition root.
- `docs/design/tool-surface-findings.md` — the promotion rationale and the bench-arm landing
  zone.

**Ratified design calls (owner, 2026-08-23, grill session via AskUserQuestion):**

- **Home:** third axis on the Model profile — not a separate `tool-rosters:` map, not
  per-server (Q1).
- **Shape:** delta lists `tools: {disabled: [...], enabled: [...]}` against the default roster —
  not disabled-only, not a full replacement list (Q2).
- **Default-off:** build-level default-off flag on tool registration + global `tools.enabled:`
  counterpart; ships with NO tool default-off (Q3).
- **Precedence:** specificity ladder profile > global > build default; a tool in both lists of
  one scope = startup NOTICE, disabled wins; unknown names = NOTICE, never refusal (Q4).
- **Resolution:** axis-wise for ALL three axes — absent axis defers user ▸ shipped ▸ zero,
  explicit zero overrides; supersedes ADR 0044 decision 4 (Q5).
- **Shipped table:** carries no tools axis; a future shipped roster needs its own ratification
  (Q6).
- **Rebind:** the resolved roster is a per-model binding riding every switch (Q7).
- **Notice:** one-liner at a switch when the roster deltas are non-empty; silent otherwise (Q8).
- **Stated bounds:** deltas apply to the DEFAULT tool set only (injected `Config.Tools` is the
  host's authority verbatim); built-in tools only, MCP untouched; plain config, not a Mechanism;
  a sub-agent resolves its own model's roster.

**Standing requirements:** skills: `coding-standards`. `make check` once at closeout; per-item
acceptance below. No `VERSION`/CHANGELOG-release-heading change — close with a
VERSION-SUGGESTION line instead.

**Out of scope (deliberately):** any tool actually shipping default-off (the state ships empty);
shipped-table rosters; MCP tool gating; the unified-git and PTY grill topics; the bench arms
(a)–(f) themselves — this plan builds their landing zone, not their verdicts.

---

## 1. `domain`: the roster axis and the default-off state

**What:** `domain.ModelProfile` gains a third axis, `Tools ToolRosterDelta`
(`Disabled, Enabled []string`; zero value = no deltas). `domain.Tool` (or the registration
surface `internal/tools` feeds, whichever the struct owns) gains a `DefaultOff bool` marking a
tool present in the build but absent from the default menu. `domain.Config` gains
`EnabledTools []string` beside `DisabledTools`, documented with the same default-set-only
contract. Doc comments state the ladder (profile > global > build) and that the zero profile
stays the byte-identical anchor.

**Files:** `internal/domain/config.go` (ModelProfile, Config), the tool type's home, and
`internal/domain/doc.go`'s map lines.

**Tests:** zero-value `ToolRosterDelta` means no deltas; Config field presence pinned by the
existing Config-shape test (`config_test.go`'s field census gains `EnabledTools`).

**Acceptance:** `go test ./internal/domain/...`

**Commit:** `feat(domain): the Model profile carries a tool-roster delta axis`

## 2. `tools`: roster computation over the registry

**What:** one pure function computes the effective tool set: start from the default set minus
build default-off tools, then apply global deltas, then profile deltas — later scope wins per
tool; within one scope disabled wins and the conflict is reported to the caller (for the item-4
NOTICE). Applies to the DEFAULT set only — an injected `Config.Tools` bypasses it untouched.
`NewDefaultRegistry`/`DefaultTools` honour `DefaultOff` (no tool sets it yet, so today's menu is
byte-identical).

**Files:** `internal/tools/registry.go` and tests.

**Tests:** table-driven — default-off tool absent; global `enabled` lifts it; profile `disabled`
beats global `enabled` and vice versa; same-scope conflict yields disabled + a reported
conflict; injected tool set untouched; empty everything = today's exact menu.

**Acceptance:** `go test ./internal/tools/...`

**Commit:** `feat(tools): the effective roster composes build, global and profile deltas`

## 3. `config`: the spellings — `tools.enabled:` and the profile `tools:` axis

**What:** parse the global `tools.enabled:` list and
`model-profiles.<pattern>.tools.{disabled,enabled}`. Unknown tool names in ANY of these lists
produce the existing one-line NOTICE (extend `UnknownToolNames`/`unknownToolNotice` to name the
offending key); a tool in both lists of one scope produces a NOTICE naming the tool and the
winning side (disabled). The YAML layer records axis PRESENCE per entry (tools axis key present
vs absent) for item 5's axis-wise resolver — presence is a file-config fact, never a domain one.
The seeded config template documents both new spellings as commented examples beside
`tools.disabled:`.

**Files:** `internal/config/config.go`, `internal/config/registry.go`,
`cmd/apogee/defaults/config.yaml`, tests.

**Tests:** round-trip both spellings; unknown-name NOTICE fires for each list; same-scope
conflict NOTICE; registry docmap test covers the new key rows.

**Acceptance:** `go test ./internal/config/...`

**Commit:** `feat(config): tools.enabled and the per-profile tools axis are spelled`

## 4. `config`: profile resolution goes axis-wise

**What:** replace the whole-entry pick with axis-wise resolution per ADR 0057 §5: for each of
tool-call format (+pattern), thinking, and tools, take the nearest layer (user ▸ shipped ▸ zero)
whose entry SPELLS that axis; an explicitly spelled zero (`tool-call-format: native`,
`thinking: {style: none}`, `tools: {}` with a key present) overrides deeper layers. Pattern
matching (case-insensitive substring, longest-wins, user-beats-shipped) is unchanged. The
resolver still hands the engine one resolved `domain.ModelProfile` — the engine sees no
layering. The shipped shape table gains no tools axis.

**Files:** wherever the resolver lives today (composition-root side, `internal/config` /
`cmd/apogee`), tests.

**Tests:** the ratifying trap — a tools-only user entry over a shipped gpt-oss match keeps
harmony thinking; explicit `style: none` user axis still silences a shipped one; absent axes
fall through to zero exactly as before; existing resolution tests updated where rule-4
whole-replacement was pinned.

**Acceptance:** `go test ./internal/config/... ./cmd/...`

**Commit:** `feat(config): model-profile resolution is axis-wise across the three layers`

## 5. `agent`: the roster rides Rebind

**What:** `RebindSpec` gains the resolved roster (spelled as the per-model deltas or the
composed effective set — follow the profile field's idiom); a model switch applies it atomically
at the ADR 0024 boundary through the same one-internal-swap shape as `applyProfile`. Rewrite
`RebindSpec`'s exclusion doc line: the ROSTER is per-model; mode, approvals, confinement and the
conversation remain session state. Construction composes the same way (item 2's function), so
startup and switch agree. Sub-agents resolve against their own model by construction — pin it
with a test, not new code.

**Files:** `internal/agent/rebind.go`, `internal/agent/construct.go`, `internal/agent/agent.go`
doc comments, tests.

**Tests:** a rebind to a profiled model swaps the tool set at the boundary; switching back
restores; injected `Config.Tools` never rebinds; a sub-agent with a different model gets its own
roster.

**Acceptance:** `go test ./internal/agent/...`

**Commit:** `feat(agent): the resolved tool roster is a per-model rebind binding`

## 6. `tui`: the switch notice

**What:** a switch whose roster deltas are non-empty renders one line —
`tools: +a +b −c (profile)` — through the same notice surface as the shipped-profile line;
silent when the entry has no tools axis or the deltas resolve empty. Names sorted for
determinism, additions before removals.

**Files:** the notice path the `model profile: … (built-in)` line uses (`internal/tui` /
composition root), tests.

**Tests:** golden line for mixed deltas; silence for no-axis and empty-delta switches.

**Acceptance:** `go test ./internal/tui/...`

**Commit:** `feat(tui): a model switch announces non-empty roster deltas`
