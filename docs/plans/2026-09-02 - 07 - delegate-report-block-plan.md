# Delegate report block — a child is told what its final reply is for

**Goal:** a delegated sub-agent carries one engine-owned standing block telling it that its FINAL
reply is the only thing the delegating agent receives, and asking it to report what it found,
changed and left unfinished by citing `path:line` rather than pasting file bodies. Structural: no
config key, no Mechanism, on under Bypass, every depth > 0 wherever a standing system message is
sent at all, routed and unrouted alike.

**Date:** 2026-09-02 · **Status:** unexecuted · **Base:** `abb09e5a`
**sized for:** ~200k-context host

**Sources**
- `docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md` (§6 anchor, the
  2026-08-25 and 2026-08-26 addenda this plan follows)
- `docs/handoffs/2026-09-02 - 00 - harness-over-mechanisms-parked-items.md` (parked item 4)
- `internal/agent/orientation.go` — the ride-along idiom this block copies
- `internal/agent/subagent.go:74-96` — `wrapUpDirectiveFormat`, the precedent for engine-owned
  model-facing delegate text with no config key

**Ratified design calls** (owner, 2026-09-02)
- **Leaf concurrency:** DROPPED from parked item 4. `domain.IsReadOnly` marks only millisecond-local
  tools plus two that are not concurrency-safe (`ask_user`, `console_close`); every slow tool
  (`EffectNetwork`, `EffectMCP`) is not marked and gates. No leaf-concurrency work in this plan.
- **Timing:** the directive rides STANDING on every child request, not a generalized wrap-up Turn.
- **Home:** its own engine-owned block composed in `standingSystem`, not an `orientation.txt` bullet.
- **Position:** prompt → orientation → **delegate block** → context files → directives → tool block.
- **Ride-along:** same rule as the orientation block — composed in only when a configured source
  already seeded the message, so `standingSystem()` still returns `""` for no-prompt-AND-no-context-
  files and ADR 0023 §6's anchor plus the Bypass floor stay byte-identical. The RULE is unchanged and
  still binding; what changed is when it bites. Per ADR 0023's own 2026-08-31 amendment, ADR 0064
  ships the default template embedded and `use-default-prompt` defaults true, so a stock run ALWAYS
  seeds a system message: §6's seeds-nothing anchor is now reached only through
  `use-default-prompt: false` or an explicitly empty configured template, not as an ordinary posture.
- **Record:** a THIRD addendum to ADR 0023, not a new ADR.
- **Example path:** the `path:line` illustration is the literal form `path:line`, not an apogee
  path — the block ships to every workspace, and `internal/agent/loop.go:219` names a file a
  Python repo does not have (review, 2026-09-02).
- **Storage:** a Go const, not a `prompts/` asset — the text takes no placeholder, so it needs
  neither the positional loader nor `mustPrompt` (`wrapUpDirectiveFormat`'s idiom).

**Regression check (2026-09-02, `abb09e5a`):**
- 1: guard folded — the guard now binds every assertion over a depth > 0 Agent's `standingSystem()`
  (the failing one is `promptseam_test.go:868` via `withOrientation`), every in-code statement of the
  wire order in `internal/agent` (`loop.go:864` omits the orientation block today), and the const is
  exported so item 2 can reach it; `internal/agent/orientation.go:38-40`'s "nothing between the
  context-files bullet and its blocks" note is superseded in place by the ratified **Position**.
- 2: guard folded — `cmd/apogee` is package `main` and imports no `internal/agent`, so the assertion
  reads `apogee.DelegateReportBlock` through the root facade (`SeatFallbackNote`'s precedent); the
  fixture's standing prompt is restated as determinism, not as "no source ⇒ no system message".
- 3: guard folded — acceptance grep scoped to live prose (over `docs/` it reports 5 today and 3
  after the two named sites — ADR 0026, an archived plan and this plan itself — so `docs/` was
  unsatisfiable); locator widened to catch `CONTEXT.md:1192`; the new
  term moves to the **Orientation block** entry at `CONTEXT.md:768`; yields to `docs/adr/0023-…:303-312`
  and `docs/adr/0026-…:228` as dated records — the third addendum states the new order rather than
  rewriting them; the stale `docs/manual/configuration.md` sentence is corrected in the same edit.

**Regression check (2026-09-02, second pass at `323738c7`, three independent reviewers):**
- 1: guard folded — the forgery fence `forgesStandingStructure` (`contextfiles.go:180-184`) is a
  closed list that does not know the block, so its opening sentence is fenced beside the const;
  `internal/agent/doc.go` names every file and `TestDocMapNamesEveryFile` goes red on a new one, so
  `delegatereport.go` gains its half-line; guard (c) recast as a rule plus grep — the "last bullet
  bridges to the blocks" claim recurs at `orientation.go:88-90` and, model-facing, at
  `prompts/orientation.txt:6`. Asset left byte-identical (see item 1 NOTES).
- 2: guard folded — the acceptance filter `-run 'Delegat'` selects by name, so the new test's name
  must carry the `TestE2EDelegation` stem or the line reads green without running it.
- 3: guard folded — the block is child-only, so every rewritten order sentence qualifies it
  "(delegations only)" and the depth gate is stated once in the new entry; the edit is an INSERTION
  into each site's own wording ("tool menu", "**Mechanism directives**"), never one canonical string
  pasted over three sites; guard (b) widened from the manual to every live doc with a grep, which is
  what catches `CONTEXT.md:778`; the `docs/` count scoped to `CONTEXT.md docs/`.

**Standing requirements**
- `skills: coding-standards`
- Deviations land as a dated NOTES line under the item.

**Out of scope**
- Concurrent leaf dispatch (dropped above). Any new cap on a delegation result — `clampToolResult`
  is the floor and this plan adds none. Any change to the child's first user message, its tool set,
  its context-file inheritance or its orientation block: children already inherit all of these.

## 1. The delegate report block — ✅ DONE (2026-09-03)

NOTES (2026-09-02): the plan places the new fence test "in `contextfiles_test.go`, beside the
existing forged-orientation-header case" — those are two different files: the existing seam case is
`TestContextSeam_ContentCannotForgeAHeaderOrTheOrientation` in `promptseam_test.go`. Wrote
`TestFenceContentFencesTheDelegateReportBlock` in `contextfiles_test.go` (the file the plan's FILES
list names), driving `fenceContent` directly and taking its prefix from `delegateReportFence`.
NOTES (2026-09-02): `promptseam_test.go` is in the plan's FILES list but needed no edit. Regression
guard (a) is satisfied at the helper instead: `withOrientation` (`orientation_test.go`) now composes
the delegate block for a depth > 0 Agent, which is the plan's own first-named remedy, so
`promptseam_test.go:868`'s child comparison passes unchanged and every other `withOrientation` call
site stays correct.
NOTES (2026-09-02): `delegateReportFence` is DERIVED from `DelegateReportBlock` (first sentence) via
a panicking `mustFirstSentence` rather than a slice expression — a `""` prefix would make
`strings.HasPrefix` true of every line and fence whole context files, so the failure mode is worth
the guard.
NOTES (2026-09-02): `doc.go`'s stale "Twenty-four files" corrected to "Twenty-six" (25 today plus
`delegatereport.go`), as the item instructs.

**What.** New `internal/agent/delegatereport.go`: an EXPORTED package const `DelegateReportBlock`
holding the ratified text VERBATIM (below) — re-exported in `apogee.go` as
`const DelegateReportBlock = agent.DelegateReportBlock`, `SeatFallbackNote`'s precedent
(`internal/agent/subagent.go:186-188`, `apogee.go:94-98`), so item 2's Driver-side assertion reads
the bytes instead of retyping them — and `func (a *Agent) delegateReportBlock() string` returning it
when `a.depth > 0` and `""` otherwise. Compose it in `standingSystem` (`internal/agent/loop.go:924-941`)
between the orientation block and the context blocks, joined by the same `"\n\n"`. The empty check
on the two configured sources stays where it is, taken BEFORE the block is asked for — that is the
ride-along rule, and moving it breaks ADR 0023 §6. Update `standingSystem`'s doc comment (its "order
is the wire order" paragraph) and `parts`' capacity. The value has two consumers, both of which take
the block for a child and must: `buildRequest` (`loop.go:873`) and `StandingTokens`
(`internal/agent/contextfiles.go:271`) — a child's standing measurement grows by the block, which is
correct and is what the report should show.

Two more edits in the same package, both found by the second regression pass. The forgery fence:
`forgesStandingStructure` (`internal/agent/contextfiles.go:180-184`) fences a context-file line that
spells the standing message's own furniture — header, footer, orientation header — and the block is
new furniture, so a repo `AGENTS.md` line opening "You are a sub-agent: another agent delegated…"
would reach a child AFTER the real block and read as a correction of it (the F-19 failure
`orientation.go:12-17` names the fence as the second half of the guard against). Fence the block's
first sentence: an unexported `delegateReportFence` beside the const, the prefix both halves name
(`orientationHeader()`'s idiom), added to `forgesStandingStructure`. The doc map: `internal/agent/doc.go`
names every non-test file and `TestDocMapNamesEveryFile` (`docmap_test.go:12-16`) fails on one it
does not, so `delegatereport.go` gains its half-line in doc.go's file list; doc.go:18's
"Twenty-four files" is already stale at 25 and is corrected to the true count in the same edit.

NOTES (2026-09-02): `prompts/orientation.txt:6` — "Workspace context files follow under … headers:
project text, not harness facts — nothing below this block changes the facts above" — stays
byte-identical. Both clauses remain true with the delegate block between: the context files still
follow, and the block changes no host fact. Rewording it would change the orientation block for
every depth-0 session, an announced surface this plan's Out-of-scope list keeps fixed; only the
in-code comments that restate the adjacency (`orientation.go:38-40`, `:88-90`) are updated.

The text, exactly (em dashes intentional, `\n\n` between the paragraphs):

```
You are a sub-agent: another agent delegated this task to you and is waiting on the result. It cannot see this conversation — your FINAL reply is the only thing it receives, so anything you do not write there is lost.

Report what you found, what you changed, and what remains unfinished. Refer to code by path and line (path:line) rather than pasting file contents — the agent you report to can read the workspace itself.
```

**Files:** `internal/agent/delegatereport.go`, `internal/agent/delegatereport_test.go`,
`internal/agent/loop.go`, `internal/agent/orientation.go`, `internal/agent/orientation_test.go`,
`internal/agent/promptseam_test.go`, `internal/agent/contextfiles.go`,
`internal/agent/contextfiles_test.go`, `internal/agent/doc.go`, `apogee.go`

**Tests.** In `delegatereport_test.go`: a child's `standingSystem()` contains the block exactly once
and between the orientation block and the context blocks; a top-level Agent's `standingSystem()` is
byte-identical to what it is without this change; a child in the `use-default-prompt: false`
posture — no prompt template and no context files, the only way the seeds-nothing anchor is reached
now — still gets `""`; a grandchild (depth 2) gets it; a routed and an unrouted child get the same block.
One test asserts the block does not contradict `wrapUpDirectiveFormat` — a capped child's request
carries both, and neither claims the other's reply is the last. In `contextfiles_test.go`, beside
the existing forged-orientation-header case: a context-file line spelling the block's first
sentence (flush and indented) is fenced exactly as a forged orientation header is, and the fence
prefix is the const's own first sentence, not a retyped copy.

**Acceptance.**
- `go build ./...`
- `go test ./internal/agent/ -run 'DelegateReport|StandingSystem|Orientation|ContextFiles' -count=1`
- `go test ./internal/agent/ -count=1`

**Regression guard.** Three families, each wider than the sites named here. (a) EVERY assertion over
a depth > 0 Agent's `standingSystem()` or seeded system message must expect the block — that grep,
never `orientationBlock()`, which reaches the helper's body but not the assertion that fails:
`TestContextSeam_SubAgentRequestCarriesParentBlocks` (`promptseam_test.go:866-870`) compares a
CHILD's seeded message to `withOrientation(child, "", …)`, and `withOrientation`
(`orientation_test.go:279-291`) composes prompt + orientation + blocks only, so it goes red until it
inserts the block for a depth > 0 Agent or that one call site expects it; `contextfiles_test.go:468`
computes its length from `systemPrompt` + `orientationBlock` on a depth-0 Agent and is unaffected.
(b) EVERY in-code statement of the wire order in `internal/agent`, found by grepping the order prose
and never a closed list — `loop.go:864`'s comment already reads "prompt → context files → directives
→ tool block" and omits the orientation block, so it must gain BOTH the orientation block and the
delegate block; `standingSystem`'s own comment is only one of the sites. (c) EVERY statement in
`internal/agent` that the context-files bullet is ADJACENT to the blocks it bridges to — a rule, not
a list: `grep -rn "bridge\|follow" internal/agent/orientation.go internal/agent/prompts/` finds
`orientation.go:38-40` and `:88-90` today, plus the model-facing bullet at `prompts/orientation.txt:6`.
The ratified **Position** puts an engine-owned block in exactly that gap, so each in-code note is
superseded in place — the adjacency rule binds bullets inside the orientation asset, and the
delegate block is the one engine-owned part ratified into the gap (owner, 2026-09-02); the asset
line itself is left as written (item NOTES above). (d) `internal/agent/doc.go` is a docmap-enlisted
package doc: every new file in the package gains its half-line there, or the package's own test
suite goes red.

commit: `feat(agent): tell a delegate what its final reply is for`

## 2. The block on the wire, end to end

**What.** Depends on item 1. Pin the block as an ANNOUNCED surface: a scripted run where the model
delegates, and the child's own request is asserted to carry the block's exact text. Add
`cmd/apogee/testdata/stubllm/delegate-report.yaml` and a test in `cmd/apogee/e2e_delegation_test.go`
beside the cases that own `delegate-cap.yaml` — that file already discriminates a child's request
from its parent's, whereas `e2e_announced_test.go` is the announced-PATHS floor (paths plus
approval-pane counting) and a system block is not a path. Discriminate the child's request from the parent's
with the `when.system:` regexp — the child's system text carries the block and the parent's does not,
which is the only difference between the two at that point. The assertion compares against the
constant the engine emits, reached through the root facade as `apogee.DelegateReportBlock` (item 1
exports it; `cmd/apogee` is package `main` and imports no `internal/agent` — `e2e_seat_test.go:59-63`
is the idiom), never a copy retyped into the fixture. The fixture pins its own `system-prompt-text`
(`announcedStandingPrompt`'s idiom in `e2e_announced_test.go`) so the announced surface under assertion is deterministic and
does not depend on the embedded default's wording changing.

**Files:** `cmd/apogee/e2e_delegation_test.go`, `cmd/apogee/testdata/stubllm/delegate-report.yaml`

**Tests.** The new e2e case itself, named with the file's stem — `TestE2EDelegation…` — because
the acceptance filter below selects by name and a case named otherwise compiles, never runs, and
reads green: the child's request carries the block verbatim; the parent's request does not; the
delegation still returns its result.

**Acceptance.**
- `go test ./cmd/apogee/ -run 'Delegat' -count=1`
- `go test ./cmd/apogee/ -count=1`

**Regression guard.** `e2e_delegation_test.go` holds several cases sharing helpers and fixtures; add
to them rather than reshaping them. The `when.system:` value is a regexp: match a substring of the
block without its parentheses, or escape them — and if the discriminator cannot separate the two
requests in practice, the fix is a sharper regexp, never asserting on request order. The block's text
is never retyped on this side of the boundary: `cmd/apogee` cannot import `internal/agent`, so an
assertion "taken from `internal/agent`" does not compile — it is read as `apogee.DelegateReportBlock`,
and if that identifier does not exist yet, item 1 exports it rather than the fixture holding a copy.

commit: `test(e2e): pin the delegate report block on a child's request`

## 3. Record the block

**What.** Depends on item 1. Three documents, one owning item.
- `docs/adr/0023-…md`: a third addendum, dated 2026-09-02, in the shape of the two before it — the
  composition list grows by a child-only delegate block; it rides along under the same rule so §6's
  seeds-nothing anchor is untouched; it sits after the orientation and before the context files on
  the same F-19 reasoning; it is engine-owned because a delegate cannot learn from any configured
  source that its final reply is all its parent receives. It STATES the new order rather than
  rewriting the 2026-08-26 addendum: `docs/adr/0023-…:303-312` and `docs/adr/0026-…:228` are dated
  records and are left as written.
- `CONTEXT.md`: a new **Delegate report block** term placed after the **Orientation block** entry
  (`CONTEXT.md:768`ff — line 1171 is inside the **System prompt** entry, not beside the block), and
  the wire order updated in every LIVE prose statement of it — that entry's own order sentence
  (~line 780), the **System prompt** entry (`CONTEXT.md:1171`) and the **Context files** entry
  (`CONTEXT.md:1192`, which spells it "prompt → **Orientation block** → context files → **Mechanism
  directives** → tool menu" and so is missed by a lowercase grep). The edit at each site is an
  INSERTION of `delegate block (delegations only)` between its orientation and context-files terms,
  keeping that site's own spelling and bolding ("tool menu", "**Mechanism directives**",
  "**Orientation block**") — never one canonical order string pasted over all three, and never
  unqualified: the block is child-only (`a.depth > 0`), so an unconditional order sentence would
  claim every session's first system message carries it. The new **Delegate report block** entry
  states the depth gate once, in words. Find the sites with
  `grep -rin "→ context files" CONTEXT.md docs/manual/`, never a closed list. The **Orientation
  block** entry also carries the stale conditioning guard (b) corrects (`CONTEXT.md:778`, "so
  "delete the prompt to send none" stays byte-identical on the wire"): fix it in the same edit.
- `docs/manual/configuration.md` (~line 1118, where the orientation block is described): one short
  paragraph saying a delegated sub-agent additionally carries a block naming what its report is for,
  that it is not part of `system-prompt-text` and cannot be edited out, and that it follows the same
  send-only-when-a-system-message-exists rule. The sentence it sits beside (~line 1123: "…and is not
  sent when you have configured neither a prompt nor context files") predates ADR 0064's embedded
  default and is corrected in the same edit — otherwise the new paragraph inherits its staleness.

**Files:** `docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md`,
`CONTEXT.md`, `docs/manual/configuration.md`

**Tests.** None (docs only).

**Acceptance.**
- `grep -n "delegate block" CONTEXT.md docs/manual/configuration.md docs/adr/0023-*.md`
- `grep -rn "orientation → context files" CONTEXT.md docs/manual/` reports 0 — live prose only; it
  reports 2 today (both in `CONTEXT.md`); over `CONTEXT.md docs/` it reports 5 today and 3 after
  this item, the rest being dated records plus this plan

**Regression guard.** Two rules, neither a list of sites. (a) Every LIVE prose statement of the
composition order is updated — each copy is load-bearing for a reader deciding where new guidance
belongs (ADR 0064's placement rule) — found by `grep -rin "→ context files" CONTEXT.md docs/manual/`,
which is what catches `CONTEXT.md:1192`; the acceptance grep below does not. ADR addenda and archived
plans are dated records and stay as written. (b) Every LIVE-doc sentence — manual AND
`CONTEXT.md`, not the manual alone — conditioning engine-owned prompt text on "no prompt
configured" is stale since ADR 0064 and is corrected, not only the one this item names; found by
`grep -rn "send none\|neither a prompt\|nothing configured" CONTEXT.md docs/manual/`, which is
what catches `CONTEXT.md:778` inside the very entry this item rewrites; the hits that DESCRIBE
ADR 0064's default-prompt rule (`docs/manual/configuration.md:1061`, `:1074`, `:1113`) are correct
and stay. (c) Every rewritten order
sentence stays true of a depth-0 session: the delegate block is qualified at each site, never
stated as unconditional.

commit: `docs(prompt): record the delegate report block`

---

**Suggested version bump:** patch (`VERSION` micro-bump) once all three items land — this adds
model-facing engine behaviour with no config surface. Not performed by this plan; the owner decides.
