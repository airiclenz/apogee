# Sub-agent prompt guard exemption — and honest exit-code error slots

- **Goal:** stop the dangerous-action guard from hard-refusing `sub_agent` dispatches whose
  task prompt merely mentions a guarded path literal, and make a failed subprocess call's
  summary slot name its exit code instead of its first output line.
- **Date:** 2026-08-13 · **Status:** ready · **sized for:** ~200k-context host
- **Authoritative sources:**
  - `internal/security/dangerous.go` at `db11c6e` — `Inspect` (:135), `inspectableText`
    (:225), `payloadKeys` (:190, doc comment :173–189).
  - `internal/domain/tools.go` — the read-class seam this plan mirrors: `ReadOnlyTool`
    (:47), `ReadSourceTool` (:68, method `ReadSourceKeys()` :71), helper
    `ReadSourceArgKeys` (:77).
  - Live repro: session `~/.apogee/sessions/20260813T055029Z-0b576ba4.json`, transcript
    entries 187–188 — the `audit-secret-exposure-config` delegation hard-refused by
    `write-git-control-plane` because its task prompt contains "…the readable git
    surfaces — `.git/logs/HEAD`, `.git/config`, `.git/packed-refs`…".
  - Precedent: commit `dc8b5fa` (write-shaped rules respect the tool's read class) — this
    plan closes the dispatch-tool half of the same false-refusal class. ADR 0012 names
    the failure mode ("precision-over-recall inverted").
  - Terminal fix ground truth: `internal/tools/terminal.go` `subprocessToolResult`
    (:140–157, appends `\n[exit code N]`), `internal/tui/toolpresent.go` error summary
    (:1062, `errorSummaryPrefix + firstLine(result.Content)`), `exitCodeStat` (:1289),
    subprocess presenter rows (:555, :562).
- **Ratified design calls** (owner, via AskUserQuestion, 2026-08-13):
  1. Scope = guard fix + terminal error-summary fix. No ISSUES.md entry.
  2. Mechanism = a declared prompt-keys tool class in `internal/domain` (sibling of
     `ReadSourceTool`), declared by `sub_agent` for `task` and `name` — NOT a global
     `payloadKeys` addition, so an MCP tool with a coincidental `task` argument stays
     fully inspected.
  3. Breadth = ALL rules skip a declared prompt key's text, not only `WritesOnly` ones:
     a delegation prompt is never an action the host performs; the child's own tool
     calls are each inspected at the action site.
  4. A non-zero-exit subprocess call's summary slot reads `error: exit N` (mirroring the
     clean-exit `exit 0` slot); the full output stays in the details body.
- **Standing requirements:** `skills: coding-standards`. Any authorized deviation from
  item text lands as a dated NOTES line under the item.
- **Out of scope:** the uncapped-reply and streaming-preview issues (owned by
  `docs/plans/2026-08-12 - 00 - reply-output-cap-plan.md`, in flight in a parallel
  session — do not touch its files: `internal/agent/loop.go`, `subagent.go`,
  `internal/config/`); the remaining hostile-bytes follow-ups in ISSUES.md (rule anchor
  gaps, PATH scoping, read-path disclosure); any change to the rule patterns themselves
  or to `terminal.go`'s result content format.

## 1. Domain prompt-keys class, declared by sub_agent — ✅ DONE (2026-08-13)

NOTES (2026-08-13): added `internal/domain/doc.go` beyond the item's Files list — its package
map enumerates the marker interfaces tools.go carries, so the new `PromptTool` goes in the same
sentence (one-word upkeep; the repo's package-map convention would otherwise go stale). No other
plan item owns that file — item 3 reconciles `internal/security/doc.go` and the confinement
contract only.
NOTES (2026-08-13): the compile-time proof comment in `sub_agent.go` said sub_agent "is a plain
domain.Tool"; reworded to say its only declaration is the prompt-key one (an inspection hint, not
a disposition marker), since that sentence would otherwise be false. The assertion list itself is
unchanged — no disposition marker was added.
NOTES (2026-08-13): no CHANGELOG entry — this item adds an unread seam and changes no observable
behaviour; the user-visible fix lands with item 2.

**What:** In `internal/domain/tools.go`, add a `PromptTool` interface — `Tool` plus
`PromptArgKeys() []string` — and a nil-tolerant helper
`func PromptArgKeys(t Tool) []string`, mirroring the shape, placement and comment style
of `ReadSourceTool` / `ReadSourceArgKeys` (:68–77): the keys name arguments whose value
is instruction PROSE handed to another agent, never something this host acts on. In
`internal/tools/sub_agent.go`, `SubAgent` (:62) declares
`PromptArgKeys() []string { return []string{"task", "name"} }`.

**Files:** `internal/domain/tools.go`, `internal/tools/sub_agent.go`,
`internal/tools/sub_agent_test.go`

**Tests:** interface assertion `var _ domain.PromptTool = (*SubAgent)(nil)`; helper
returns the two keys for `SubAgent` and nil for a tool that does not implement the
interface.

**Acceptance:** `go build ./... && go test ./internal/domain/ ./internal/tools/`

**Commit:** `feat(domain): tools declare prompt-carrying argument keys`

## 2. Guard drops prompt-key text for every rule

Depends on item 1.

**What:** In `internal/security/dangerous.go`, `Inspect` (:135) obtains
`domain.PromptArgKeys(tool)` and drops those keys' values from BOTH inspectable views —
the full view (currently built with `dropKeys` nil) and the write-shaped view (union
with the read-source keys). `inspectableText` (:225) needs no signature change — pass
the prompt keys as `dropKeys` for the full view and the union for the writes view.
Update the `payloadKeys` doc comment (:173–189): remove `task` from the "deliberately
still inspected" list and state the new rule — a tool-declared prompt key is exempt
because the delegated child's own calls are inspected when it acts; `command`, `code`,
`path` and `url` remain inspected.

**Files:** `internal/security/dangerous.go`, `internal/security/dangerous_test.go`

**Tests** (extend `dangerous_test.go` beside `TestWritesOnlyRulesSkipADeclaredReadOnlyTool`
:326): a `sub_agent`-shaped tool declaring prompt keys, whose `task` contains
`.git/config`, `~/.apogee`, and `~/.ssh/id_rsa`, gets `TierNone` — include the live
repro sentence ("the readable git surfaces — .git/logs/HEAD, .git/config,
.git/packed-refs") verbatim as one case; a `terminal` command heredoc writing to
`~/.ssh` is still `TierHardRefuse` (command-shaped text stays inspected); a tool with a
`task` argument that does NOT declare the interface is still matched on that text; a
command-shaped (non-WritesOnly) rule also ignores declared prompt-key text.

**Acceptance:** `go test ./internal/security/`

**Commit:** `fix(security): dangerous rules no longer match a declared delegation prompt`

## 3. Reconcile the guard documentation

Depends on item 2.

**What:** `internal/security/doc.go` — the payload-key contract paragraph (:32–39) and
the per-file map entry for dangerous.go (:58–66) gain the prompt-keys class alongside
the read class. `docs/design/confinement-execution-contract.md` — the guard-floor step
of §4 ("Guard floor first (tighten-only, ADR 0012 / P3.6)", :430) records that a
tool-declared prompt key's text is outside every rule's sight, with the
child-guarded-at-the-action-site rationale, dated 2026-08-13.

**Files:** `internal/security/doc.go`, `docs/design/confinement-execution-contract.md`

**Tests:** none (documentation).

**Acceptance:** `go vet ./internal/security/` passes;
`grep -l "PromptArgKeys" internal/security/doc.go docs/design/confinement-execution-contract.md`
lists both files.

**Commit:** `docs(security): record the prompt-key exemption on the guard contract`

## 4. Failed subprocess calls name their exit code in the slot

Independent of items 1–3.

**What:** In `internal/tui/toolpresent.go`, the error-summary path (:1062) words the
slot for the two subprocess-running tools (the presenter rows using `exitCodeStat`,
:555 and :562) from the trailing `[exit code N]` marker that
`subprocessToolResult` appends (`internal/tools/terminal.go:153`): the summary becomes
`error: exit N`. When the marker is absent (timeout/wedged-drain shapes or an
unexpected content), fall back to the existing first-line wording. Every other tool's
error summary keeps the `error: <first line>` behavior — for those, the first line IS
the error message. `terminal.go`'s result content format does not change (the model
still receives full output plus the marker). Extend the `exitCodeStat` seam or add an
error-side sibling — implementer's choice under the coding standards; the slot wording
above is the binding part.

**Files:** `internal/tui/toolpresent.go`, `internal/tui/toolpresent_test.go`

**Tests:** an errored terminal result with content
`"total 20760\nsome output\n[exit code 2]"` renders summary `error: exit 2`; an errored
non-subprocess tool result keeps `error: <first line>`; an errored terminal result
without the marker falls back to the first line.

**Acceptance:** `go test ./internal/tui/`

**Commit:** `fix(tui): a failed subprocess call's slot names its exit code, not its first output line`

---

**Suggested version bump:** patch (`v0.13.10`) — two user-visible fixes to shipped
behavior; whether and when to bump is the owner's call.
