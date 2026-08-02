# Plan — Close the live url-safety gap (the 2026-07-26 audit's findings)

**Date:** 2026-07-26
**Status:** complete — items **1 (H-1)**, **2 (H-2)** and **14 (M-10)**
carry a `**Design call:**` line. The executing coordinator **stops and asks the owner** before
dispatching those three; the other eleven items are pre-decided by the audit and need no
escalation. *(This line describes items 1–14 as the plan was written; all three calls were
answered and all fourteen landed on 2026-07-26 — see the dated **Status** block below, which is
current and also covers the follow-up items 15–20.)*
**Source:** `docs/reviews/archived/2026-07-26 - 00 - url-safety-live-gap-audit.md` — the `/code-audit` on
the *live* url-safety gap, itself the discharge of candidate 02 of
`docs/reviews/archived/2026-07-24 - 00 - architecture-deepening-review.md`. Every finding below
is that audit's, in that audit's own **Recommended Action Order**; nothing here is new analysis.
**Track:** post-`v0.8.0` architecture deepening — the enforcement half of the url-safety choke
point whose *shape* landed 2026-07-25
(`docs/plans/archived/2026-07-25 - 00 - url-safety-choke-point-plan.md`).
**Public API:** expected unchanged. Item 1 may touch the exported facade's *behaviour* (not its
symbols) at `apogee.go:240-246`; item 2's design call could add one; no ADR 0010 version bump is
planned. If any item finds it needs a new exported symbol, that is a stop-and-ask, not a judgement
call.
**Standing requirement:** `/coding-standards` (Go + testing variants) mandatory — invoke
`implement-plan` with **`coding-standards`** forwarded. Pre-production: commit direct to `main`,
no PRs (owner directive; overrides the version-control standard's no-direct-to-main rule).

Per-item green gate — every item must leave all of this green before its commit:

```
gofmt -l .                                              # empty
go vet ./... && go test ./... && go test -race ./...
GOOS=windows go build ./... && GOOS=darwin go build ./...
```

**Deviation rule.** Any authorized departure from an item's text — a different seam, an extra
file, a test that had to move, a wording change — lands as a dated
`NOTES (YYYY-MM-DD): …` line **under that item's heading**, in the house convention the archived
plans use. The plan file is the resume state and the record; an undocumented deviation is a
verification failure.

---

## Status — where this plan stands (2026-07-26)

**Items 1–14: COMPLETE.** Every one is implemented, independently verified and committed — see the
`— ✅ DONE (2026-07-26)` marker and the dated `NOTES` line on each heading (commits `46570a2`
through `6013817`). Items **10** and **12** each failed independent verification once and were
retried; their `NOTES` record the **ratified** fixes and supersede the first attempts' notes.

**Items 15–20: OPEN.** They are the findings this plan's own implementation and verification pass
raised that **no item in 1–14 owned** — items 16, 17, 18 and 20 were each handed forward by an
item's independent verifier, item 15 collects the stale doc and comment claims items 1, 10 and 14
left behind, and item 19 is the owner's deferral. Each names its provenance in a `Provenance:` line
under its heading, in place of the audit finding ID items 1–14 carry. They are in the same house
format as items 1–14 and
carry no `✅` marker. Item **19 is DEFERRED** by the owner (2026-07-26) to an architecture pass: it
is recorded so its reasoning is not lost, **not** queued for execution. None of items 15–20 carries
a `Design call:` line: items 15 and 20 each hold an **in-item adjudication** the implementer
resolves itself and records in its dated `NOTES`, which is not an owner escalation.

**CLOSED OUT 2026-07-26 — the *Whole-plan verification* H2 below has been run end-to-end, and this
plan is archived.** Step by step, with what was actually observed:

1. **Full gate green**, `-race` run **twice** in a row: `gofmt -l .` empty, `go vet ./...` clean,
   `go test ./...` ok in all 22 packages, `go test -race ./...` ok twice, `GOOS=windows go build
   ./...` and `GOOS=darwin go build ./...` both clean.
2. **Tighten-only proof — holds.** The only class/ladder change in the whole plan is item 1's, and
   it is a *reordering*: `classifyTool` now consults every unfakeable marker first and
   `classReadOnly` is the terminal floor, which can only move a tool from the RO row's
   `run | run | run | run | run` **onto a stricter row** — no cell moves toward `run` in any mode,
   and §4's table cells are byte-identical apart from the two `refuse²` footnote markers. Item 14's
   MCP-endpoint exemption moves **no ladder cell** (mcp still gates in Auto) and pairs the
   pre-flight exemption with a *pinned* dial plus a no-follow redirect policy, so it is not a
   ladder loosening. `confine-to-workspace: false` still returns its verdict **before** the class
   switch — `resolution.go`'s `resolveLadderAuto` returns `resolveRun` at its first statement,
   ahead of `switch class`.
3. **§4 has exactly one owner — confirmed.** `git log --oneline 7c35413..HEAD --
   docs/design/confinement-execution-contract.md` returns exactly one commit, `46570a2` (item 1).
4. **The normalisation runway is clear.** Items 8, 9 and 10 are landed and green. The closeout note
   is written into `TODO.md`'s *Dedicated url-safety config key* entry: the key is now a smaller,
   safer change (host matching is already normalised by `security.NormalizeURL`), and the one
   remaining trap — the duplicated `HostTools` composition — is named there as a deferred
   architecture candidate rather than a blocker.
5. **Every deviation is recorded** — items 1–18 and 20 each carry a dated `NOTES (2026-07-26)` line
   under their heading; item 19 is the owner's deferral and lands nothing.
6. **CHANGELOG / CONTEXT sweep done.** Every user-facing item (1, 2, 3, 8, 9, 10, 11, 12, 13, 14,
   16, 17, 18, 20) touched `CHANGELOG.md`; items 4–7 are regression nets for already-shipped
   behaviour and 15 is a doc correction, so their omission is deliberate and recorded in their own
   `NOTES`. **`CONTEXT.md` was swept for prose item 1's classification change makes stale and
   nothing needed changing:** the vouching clause (*"a third-party tool of either kind, whose
   scoping Apogee cannot vouch for, gates"*) and the *Confinement* / safety-guardrails entries
   describe blast-radius classes, never the RO-first check order, so item 1 makes them **more**
   true rather than stale; the *Plan* entry (*"read-only; no writes or command execution"*) is now
   accurate where it was previously overstated, and the two `ReadOnly` mentions are `ask_user`,
   which carries no marker and still runs in Plan. CONTEXT.md's SSRF-floor, MCP-endpoint and rung-1
   sentences were already corrected in-flight by items 10, 14, 15 and 2.
7. **Archived.** The source audit (`docs/reviews/archived/2026-07-26 - 00 - url-safety-live-gap-audit.md`)
   carries a dated `## Disposition (2026-07-26 — close)` block giving every one of its 14 findings'
   dispositions and re-parking action-order entry 8, and moved into `docs/reviews/archived/`; this
   plan moved into `docs/plans/archived/`.

**Still owner-pending — the *Manual verification* H2 at the foot of this file.** The live Auto run
after item 1, item 14's startup check against the real MCP endpoint, and item 2's real-desktop
degrade check need hardware and a running model and were **not** run as part of this closeout. They
are not blockers on the archive: every automated acceptance in this plan is green.

**Retroactive ratifications (owner, 2026-07-26).** Two new exported symbols landed **without** the
stop-and-ask this plan's intro requires — `security.NormalizeURL` (item 8) and
`security.PinnedDialControl` (item 14). The owner **accepted both as they stand**: each lives in an
**internal** package (not importable by an embedder, so not the public API ADR 0010 defines), no
`apogee.go` facade symbol or alias was added, no ADR 0010 version bump is involved, each deviation
is recorded in its item's dated `NOTES`, and item 14's ratified call (B) — an endpoint-aware dial
control — made a cross-package symbol unavoidable. The intro's rule stands **unchanged** for items
15–20: a new exported symbol is still a stop-and-ask.

**Pending OWNER MANUAL CHECK — the live MCP endpoint (not a work item; the owner runs it).** Start
`apogee` against `http://192.168.64.1:7331/mcp` and confirm it **connects** after item 14's
pre-flight exemption and endpoint-aware dial pin. **Caveat, stated plainly:** item 14 also gave the
MCP transports the funnel's **no-follow** `CheckRedirect` policy, so an MCP server that redirects —
`/mcp` → `/mcp/` is the common case — must now be configured at its **final** URL, or the connect
fails on the 3xx itself. That may be a configuration change the owner has to make *before* the
check can pass, and a failure of this shape is not a regression in the exemption.

---

## Precedence rule — the ground truth every item is checked against

This plan's items are **not** the authority for what the code must do. The ground truth is, in
this order:

1. **ADR 0012** — `docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md`
   — authoritative on **policy**, including its **Amendment (2026-07-25)** ("url-safety is
   vouched-for by construction; an unvouched network tool gates") and its **Amendment
   (2026-07-21)** (host-scoped acknowledgement). Its core invariant governs every item here:
   *"Apogee never runs a tool call both unsupervised and unbounded."*
2. **`docs/design/confinement-execution-contract.md` §4** — the per-call **Resolution**, the
   tool-class × mode table, gate reasons and cache keys. **Where §4 and ADR 0012's prose differ on
   a mechanism, §4 is authoritative on the *how***; ADR 0012 stays authoritative on the *policy*.
   ADR 0012's own Consequences section states this precedence, and §4 carries the 2026-07-25
   amendment note in its own header.

Neither the audit report nor **this plan** is a conduct source. Where an item's text and those two
documents disagree, the documents win and the divergence is a stop-and-ask — except for item 1,
which is the **one** item licensed to *change* §4 (see below).

**§4 has exactly one owning item.** H-1's fix contradicts §4's **RO row**
(`RO | run | run | run | run | run`) as written — that row is precisely what licenses today's
RO-first classification. **Item 1 owns the §4 amendment and is the only item that may edit §4.**
No other item touches it; if another item believes it needs a §4 change, it stops and the
coordinator folds the change into item 1's scope.

---

## Context — what the audit found, in one paragraph

The funnel that landed 2026-07-25 holds: `networkTool.do` really is the single path from a tool to
the network, the url-filter marker is unfakeable, the dial-time SSRF control fires on every
connect, and the classic bypasses (userinfo, `::ffff:`-mapped, NAT64 well-known, decimal/octal/hex,
redirects, header injection, decompression bombs) are genuinely shut. **The promise is defeated one
rung up:** `classifyTool` consults the *self-declared* `ReadOnly()` **before** any unfakeable
marker, so a tool declaring `EffectNetwork` **and** `ReadOnly() == true` takes the RO row and
auto-runs in **every** mode, Plan included, with no url filtering and no gate — the exact
"keys on a declaration, not a marker" defect the 2026-07-25 amendment was written to remove, moved
up one level. That ordering already has three live consequences in the shipped tool set
(`git_diff_range`, `diagnostics`, `present_document`), two of them OS-subprocess launches outside
any confinement box. Below that sit a wrong-answer class in the funnel's body read, a missing
regression net for the dial-time floor, three URL-normalisation defects that are **latent only
because no shipped path populates `AllowHosts`/`DenyHosts` yet**, three cheap correctness fixes,
three test gaps, and one documented-behaviour policy question about the MCP endpoint floor.

**Ordering.** Items are in the audit's own Recommended Action Order and are **independent unless
stated**: item 1 discharges two of H-1's three consequences; item 2 handles the third, which item
1's fix cannot reach. Items 8–10 (**M-1, M-2, M-3**) are the normalisation group and **must land
before the parked url-safety config key at `TODO.md:285`** ("Dedicated url-safety config key for
the network tools", parked 2026-06-24) — that key is what converts the latent half of those
findings into live ones, and doing them first also makes the config key a smaller, safer change.
Nothing in this plan lands that config key.

---

## Out of scope

- **The `/improve-codebase-architecture` candidate the audit lists as its 9th action-order entry.**
  The `HostTools` composition exists twice (`internal/agent/construct.go:189-196` and
  `cmd/apogee/wire.go:396-402`, the engine-side one unexported so the binary cannot reuse it), and
  the guarded-client builder is duplicated on the same seam (`internal/tools/network.go:220` /
  `internal/mcp/transport.go:154`). That is a **deepening candidate, not a fix** — it belongs in an
  `/improve-codebase-architecture` pass, not here. It is noted because it bites this plan twice:
  item 5 (M-7) pins the threading that the duplication makes silently droppable, and item 14
  (M-10) names the client-builder duplication as part of its design call. Neither item may
  consolidate the seam.
- **Landing the parked url-safety config key** (`TODO.md:285`). This plan clears its runway; it
  does not build it.
- **Any loosening.** Every item here is tighten-only or neutral. A change that would move a call
  from `gate` toward `run` is out of scope in all modes, and `confine-to-workspace: false` ("I am
  the sandbox") must return its verdict before every class switch, exactly as today.
- **Subprocess network reach** (`terminal` running `curl`, `git fetch`) — out of scope by ADR
  0012's own reasoning (*"a subprocess can already `curl` the same host"*).
- **Re-auditing what the audit cleared.** The funnel's single-`do` shape, the marker's
  unfakeability, the IP classifier, deny-before-allow precedence and the M2 key-scrubbing
  discipline are confirmed good and are not to be "improved" en passant.

---

## 1. H-1 — `classifyTool` consults the unfakeable markers before the self-declaration — ✅ DONE (2026-07-26)

NOTES (2026-07-26): Owner's design call taken as given — (A) reorder so RO is the terminal floor,
(B) both built-ins on the `subproc` row, (C) Plan's menu filter and both `ReadOnly()` declarations
untouched, (D) dated `>` amendment under §4 plus the reworded `Tool-classes:` line. Four
departures from the item's literal text: (1) the "new in `internal/tools`" classifier test lives
in `internal/agent` instead — `classifyTool` is unexported there and `internal/tools` cannot
import `internal/agent` (import cycle), so `TestClassifyTool` gained rows for the REAL
`tools.NewGitDiffRange` / `tools.NewDiagnostics` (exactly the `tools.NewWriteFile(ws)` mirror the
item names), while `internal/tools`' `TestGit_Markers` / `TestDiagnostics_Markers` keep the marker
pins and now point at the agent-side test. (2) `TestDiagnostics_ReadOnlyDoesNotConfine` is renamed
`TestDiagnostics_RunsWithoutAConfinementHandle` — its assertions are unchanged and still valuable
(the tool must never REQUIRE a handle, since every rung below Auto still runs it without one), but
the old name asserted the falsehood the fix removes. (3) `TestDefaultTools_DeclareReadOnlyNature`
keeps every assertion byte-for-byte; only its "runs in Plan" comments are reworded to the
declaration-vs-marker split. (4) Doc comments that stated the old precedence in prose are
corrected where they had become false: `internal/tools/git.go` and `internal/tools/diagnostics.go`
("read-only wins over the subprocess class", "never confines — it is read-only") and
`resolution.go`'s Plan-rung comment. `docs/design/technical-design.md:196` carries the same stale
claim and was deliberately LEFT ALONE — this item's acceptance forbids touching any other doc
under `docs/design/`; it needs a follow-up. Plan-mode behaviour is now: both tools are still
offered in the menu (the filter reads `ReadOnly()`) and a call refuses — the owner's (C) accepted
that, and §4's new footnote ² records it.

**Design call:** the audit gives two shapes and does not choose between them, and the disposition
of the two affected built-ins is an owner call, not an implementer's. Stop and ask:
(a) **which reordering** — check `IsSubprocessTool` and `ExternalEffectTool` *ahead of*
`IsReadOnly` in `classifyTool`, or keep the order and *ignore* `ReadOnly()` on any tool that also
satisfies either marker? (b) **where do `git_diff_range` and `diagnostics` land in Auto** — the
`subproc` row (`confine`, with the caps-insufficient `gate` fallback) or a straight `gate`? (c)
**should either still be offered in Plan mode's read-only menu** (`internal/agent/loop.go:764`),
given they launch a process — and if not, does Plan's tool filter change or do the tools stop
declaring `ReadOnly()`? (d) **exact §4 RO-row wording** (below). Do not decide these in-item.

**Authoritative source:** ADR 0012 **Amendment (2026-07-25)(a)** — *"Classification keys on the
marker… an `EffectNetwork` tool **without** it is a distinct third-party-network class that
gates"* — plus ADR 0012's core invariant (*never unsupervised **and** unbounded*), and contract
**§4**'s class list and ladder table.

**What:** in `classifyTool` (`internal/agent/resolution.go:242-262`), `domain.IsReadOnly(tool)`
returns `classReadOnly` at **`resolution.go:243`**, before `IsWorkspaceScopedWriter`,
`IsURLFilteredNetworker`, `ExternalEffect()` and `IsSubprocessTool` are ever consulted. Every later
marker is unfakeable by construction; `ReadOnly()` is a bare self-declaration — and it wins. Fix
per the design call above so a marker outranks the declaration. Three live consequences share the
root cause; **this item discharges two of them**:

- **The network split is escapable.** `apogee.ReadOnlyTool` (`apogee.go:242`) and
  `apogee.ExternalEffectTool` (`apogee.go:238`) are both exported facade aliases, so a
  host-registered tool that GETs URLs and returns `ReadOnly() == true` takes the RO row and gets
  `run | run | run | run | run` where §4's **3p-net** row says
  `refuse | gate | gate | gate | run` — including the Plan menu (`internal/agent/loop.go:764`).
- **`git_diff_range` launches an unconfined subprocess in every mode.**
  `internal/tools/git.go:443` declares `ReadOnly() == true` and `:448` declares
  `Subprocess() == true`. The RO row yields `resolveRun`, `executeRun` passes `box == nil`
  (`internal/agent/dispatch.go:132-138`), and `runSubprocess` confines only when a handle is on
  ctx (`internal/tools/exec_common.go:137`) — so `git` runs raw.
- **`diagnostics` does the same with the Go toolchain.** `internal/tools/diagnostics.go:81` and
  `:87` declare the same pair; `runGoVet` shells out to `go vet <dir>`
  (`diagnostics.go:186-192`), which compiles attacker-controlled source, resolves modules from
  `GOPROXY`, and honours a `toolchain` directive in the repo's own `go.mod` — all outside the
  fence, in Auto, unattended.

Ladder rows to re-check after the change: `resolution.go:272-277` (Plan) and `:316-317` (Auto).
The change is **tighten-only** and moves no documented cell for the built-ins that carry neither
marker (`read_file`, `grep`, `view_diff`, `open_file`, `list_dir`, `ask_user`).

**This item owns the contract §4 amendment.** §4's **RO row** as written
(`RO | run | run | run | run | run`) is what licenses the current code, so the fix contradicts the
document. Amend §4 in the file's existing amendment style (a dated `>` block under the §4 heading,
matching the 2026-07-02 and 2026-07-25 notes already there) to say that **RO is a floor only for a
tool carrying no other class marker** — a tool that is also subproc / net / 3p-net / mcp /
WS-write takes *that* class's row. Update §4's `Tool-classes:` paragraph (`RO = IsReadOnly`)
correspondingly. **No other item in this plan may edit §4.** ADR 0012 itself needs no amendment —
this makes the code faithful to what the ADR already says — but if the owner's answer to the
design call implies a *policy* change, that is a stop-and-ask for an ADR 0012 amendment, not a
silent edit.

**Tests:**
- `internal/agent/dispatch_test.go:148` `TestClassifyTool` — add rows for a fake declaring
  `ReadOnly() == true` **and** `EffectNetwork` (expect the 3p-net class, not RO), and one
  declaring `ReadOnly() == true` **and** `Subprocess() == true` (expect the subproc class).
- `internal/agent/resolution_test.go:37` `TestResolve_LadderTable` — extend so the RO-plus-marker
  case is pinned across all five columns, in whichever direction the design call settles.
- New in `internal/tools`: assert the two real tools no longer classify RO through the agent seam
  — i.e. a test that `git_diff_range` and `diagnostics` are seen as subprocess tools by the
  classifier, mirroring how `dispatch_test.go:155` already classifies `tools.NewWriteFile(ws)`.
- **Existing tests that pin today's behaviour and must be revisited, not deleted silently:**
  `internal/tools/diagnostics_test.go` `TestDiagnostics_ReadOnlyDoesNotConfine` (line 220) and
  `internal/tools/registry_test.go:120` `TestDefaultTools_DeclareReadOnlyNature`. Any change to
  either is a `NOTES` line.

**Acceptance — a fresh verifier runs:**
```
go test ./internal/agent/... -run 'TestClassifyTool|TestResolve_LadderTable|TestDisposition_' -v
go test ./internal/tools/... -run 'TestGit_Markers|TestDiagnostics_|TestDefaultTools_' -v
go test ./... && go test -race ./...
```
plus: `grep -n "RO" docs/design/confinement-execution-contract.md` shows the dated amendment and
the reworded `Tool-classes:` line; `git diff --stat` for this item touches
`docs/design/confinement-execution-contract.md` and **no other doc under `docs/design/`**; the
diff moves no ladder cell from `gate`/`confine` toward `run` anywhere.

Commit: `fix(agent): classify on the unfakeable marker before the self-declared ReadOnly`

---

## 2. H-2 — `present_document` hands a model-chosen file to the OS handler outside any box — ✅ DONE (2026-07-26)

NOTES (2026-07-26): Owner's design call taken as given — (A) extension allow-list in the opener,
no gating `toolClass`, no §4 row, `docs/design/confinement-execution-contract.md` untouched; (B)
one new exported symbol, `present.OpenerRenderable`, carrying rung 1's own WIDER curated set
(documents, images, text — 40 extensions as shipped; the count corrected 2026-07-26 by item 20,
which then dropped `.doc`/`.xls`/`.ppt`, leaving 37), with `internal/tui`'s `browserRenderableExts` left
exactly as it was; (C) dated `## Amendment (2026-07-26)` on ADR 0019 in the house style ADR 0012's
amendments use. Four departures from the item's literal text: (1) **the bound stops at rung 3** —
`overrideArgv` (a configured `present.command`) is NOT extension-checked, because that template
names ONE application so the extension selects nothing there, narrowing it would refuse the source
files and odd formats the user configured it for, and ADR 0019 §5's "the user's own configuration,
the same standing as their shell" governs that rung; the owner's (B)/(C) name **rung 1** twice and
the item's own citation is `opener.go:119-131`, the OS-table branch, so rung 1 is what is bounded.
The carve-out is stated in the ADR amendment (d→c), in `argv`'s doc comment, and pinned by
`TestOpenerCommandOverrideIsNotExtensionBounded`. (2) A refused extension reports the EXISTING
`ErrNoOpener` rather than a new sentinel — only one new exported symbol was authorized, and the
ladder already degrades that sentinel to rung 0; `ErrNoOpener`'s doc comment now names the third
cause, and the transcript reason stays "no opener on this machine". (3) Two extra tests beyond the
item's list: `TestOpenerRenderableAllowsDocumentsAndRefusesPrograms` (the predicate's rule, both
directions) and — in `internal/tui` — `TestBrowserRenderableIsASubsetOfTheOpenerSet`, which is the
only place decision (B)'s subset invariant can actually be read off the real rung-2 set, plus one
ladder row in `TestPresenterLadderPicksRung` proving a local darwin desktop still refuses a
`.bat`. (4) Docs that the change made incomplete were corrected: `CONTEXT.md`'s rung-1 sentence,
the user-facing rung-1 sentence in `README.md` and in the shipped `cmd/apogee/defaults/config.yaml`
template (one clause each, since `present.auto-open`'s documented behaviour narrowed), and the doc
comments on `Opener` and `browserRenderableExts`. No `docs/design/` file was touched.

**Design call:** the audit gives two alternatives and explicitly calls the disposition *"a separate
owner decision"*. Stop and ask: (a) **extension allow-list in the opener**, degrading to the
baseline transcript rung for anything else — or (b) **give `present_document` its own `toolClass`
that gates outside Plan** — or both? (b) is a §4 table change, which **item 1 owns**, so choosing
it means folding the row into item 1 rather than editing §4 here. (c) If (a): the audit says reuse
`browserRenderableExts` (`internal/tui/presenter.go:142`), but that set is **unexported and lives
in `internal/tui`**, which `internal/present` and `internal/tools` cannot reach — so where does the
shared set live (lift to `internal/present`? a new small shared home?), and does ADR 0019
(*documents are presented, not opened*) need an amendment to record that the launch rung is now
extension-bounded like the doc-server rung already is? Do not pick the home unilaterally — it is a
package-layout call under ADR 0010.

**Authoritative source:** ADR 0012's core invariant (an unattended call must have a bounded blast
radius) and **ADR 0019** (`docs/adr/0019-documents-are-presented-not-opened.md`), which owns the
presentation rung ladder that this fix bounds.

**What:** `internal/tools/present_document.go:66` declares `ReadOnly() == true`, so the same
RO-first ordering as H-1 gives it `resolveRun` in every mode with `box == nil` — and the local rung
then execs `open <path>` (darwin), `cmd /c start "" <path>` (windows) or `xdg-open <path>` (linux)
on the path the model chose: `internal/present/opener.go:119-131` (`argv`) and `:180`
(`launchDetached` → plain `exec.Command`). It is wired on by default at `cmd/apogee/config.go:239`
and `cmd/apogee/wire.go:379`. **Item 1's fix does not reach this** — `present_document` carries no
other marker to reorder ahead of, which is why this is its own item. Path-safety bounds *which*
file is handed over, but the model controls the file's **content and extension**, and the extension
is what selects the handler: in Auto the model can `write_file` in-workspace (auto-runs as a
`workspaceScopedWriter`, `resolution.go:319-322`) and then present `report.command`,
`report.terminal`, `report.bat` or `notes.hta`, executed with the user's full privileges outside
the confinement box. In Plan mode the tool is still ungated, so a checked-in `build.bat` in a
hostile repo suffices there.

**Tests:**
- `internal/present/opener_test.go` — extend `TestOpenerBuildsThePlatformCommand` (line 41) with a
  table over allowed vs refused extensions on each GOOS, asserting a refused extension produces
  **no** `argv` at all rather than a command that is merely not run; keep
  `TestOpenerRejectsAnUnusableOverride` (line 239) and `TestLaunchDetachedReportsWhatHappened`
  (line 335) green.
- `internal/tools/present_document_test.go` — a case asserting the degrade path: a
  non-renderable extension yields the baseline transcript rung wording, not an error and not a
  launch (`TestPresentDocument_OutcomeWordingPerRung` at line 52 is the existing home).
- If the design call picks (b), add the class row test alongside item 1's ladder table rows.

**Acceptance — a fresh verifier runs:**
```
go test ./internal/present/... -run 'TestOpener|TestLaunchDetached' -v
go test ./internal/tools/... -run 'TestPresentDocument_' -v
go test ./... && go test -race ./...
```
plus: a grep proving the launch rung consults an extension set —
`grep -rn "Ext(" internal/present/opener.go` — and, if ADR 0019 was amended, that the amendment is
dated and names the rung it bounds.

Commit: `fix(present): bound the OS-handler launch to a renderable-extension allow-list`

---

## 3. H-3 — the funnel discards the body-read error, so a truncated response reads as complete — ✅ DONE (2026-07-26)

NOTES (2026-07-26): Of the two shapes the item offers for a non-cancellation read error, the
**message shape** was taken (`"response from host … was cut short: …"`) rather than
`truncated = true`: the renderers are out of scope by the item's own text, so the existing marker
would have had to keep saying `[response truncated at 2097152 bytes]` over a body that never
reached the cap — a second wrong answer in place of the first. The cap path is unchanged and still
returns a clean `truncated` with a nil error. Three departures worth naming: (1) the two cut-short
cases live in one new table-driven `TestNetworkFunnel_DoCutShortBodyIsNeverASilentSuccess` rather
than two separate tests, sharing a `writePartialBody` handler helper (headers + a first chunk under
a `Content-Length` that promises more), and they also assert the M2 discipline on the new message
(key-bearing URL in, host-only message out). (2) The ctx-during-body subtest cancels 50 ms after
the handler flushes its headers — the deterministic signal available is server-side, and the
assertion is contract-correct for both interleavings, so an early cancel degrades the subtest to
the already-covered in-flight case rather than flaking. (3) `do`'s three-shape doc comment and
`readCappedBody`'s doc comment were corrected where the change made them false; no other file, and
no doc under `docs/design/`, was touched. **Mutation check (mandatory, performed):** with
`internal/tools/network.go` reverted to the pre-fix code (`git checkout --`), all three new cases
go **red** — `cancelled while the body streams` reports `err = <nil>, want context.Canceled`, and
both cut-short cases report a silent `200 OK` carrying `body:"first chunk" truncated:false`;
restored, all green, including `TestNetworkFunnel_DoCapsBody` and
`go test -race ./internal/tools/... -run 'TestNetworkFunnel_' -count=2`.

**What:** `readCappedBody` drops the read error at `internal/tools/network.go:249-256`
(`data, _ := io.ReadAll(limited)`), and `do` at `:179`
(`body, truncated := readCappedBody(resp.Body)`) performs no `ctx.Err()` re-check after
`client.Do` succeeds. `truncated` is set **only** by the 2 MiB cap, so a body cut short by an error
carries no marker at all. Three concrete triggers: `http.Client.Timeout` covers the body read, so a
server streaming slowly past the resolved timeout yields `HTTP 200 OK` plus the first chunk with no
truncation notice; a mid-body connection reset yields the same silent partial; and a caller ctx
cancelled while the body streams leaves `ctx.Err() != nil` while `do` returns `(resp, "", nil)`, so
`Execute` returns a nil error and the Turn is **not** rolled back. The contract this breaks is
stated in the file itself at `network.go:119-132`.
**Fix:** have `readCappedBody` return `(body, truncated, err)`. In `do`, on a non-nil read error
check `ctx.Err()` **first** and return it as the Go-error shape; otherwise either return the
message shape — `"response from host "+label+" was cut short: "+scrubURLError(err, req.url)` — or
set `truncated = true` so the existing `[response truncated…]` marker fires. Self-contained inside
`network.go`; the three tools' renderers are untouched.

**Authoritative source:** the funnel's own documented three-shape contract at `network.go:119-132`,
and **ADR 0007** (`docs/adr/0007-step-turn-and-the-quiescent-boundary.md`) — *a tool returns a Go
error for nothing else* (cancellation only), which is what the third trigger breaks.

**Tests** (`internal/tools/network_funnel_test.go`):
- a handler that writes a partial body then aborts the connection ⇒ the result is either the
  cut-short message shape or carries the truncation marker, and is **never** a silent success;
- a handler that blocks past the resolved timeout ⇒ same assertion;
- a ctx cancelled *while the body streams* ⇒ a **Go error**, extending
  `TestNetworkFunnel_DoCancelledCtxIsGoError` (line 315), which today covers only
  cancel-before-request and cancel-before-headers;
- the existing `TestNetworkFunnel_DoCapsBody` (line 145) must stay green — the 2 MiB cap path is
  still a clean `truncated`, not an error.

**Acceptance — a fresh verifier runs:**
```
go test ./internal/tools/... -run 'TestNetworkFunnel_' -v
go test -race ./internal/tools/... -run 'TestNetworkFunnel_' -count=2
go test ./... && go test -race ./...
```
plus: the new cancellation test is confirmed to **fail** against the pre-fix code (revert the
`do`/`readCappedBody` change locally, observe red, restore) — record that in the item's `NOTES`.

Commit: `fix(tools): surface the funnel's body-read error instead of a silent partial`

---

## 4. H-4 — pin the dial-time SSRF floor through the funnel's own client — ✅ DONE (2026-07-26)

NOTES (2026-07-26): **Mutation check (mandatory, performed):** the `Control: guard.SafeDialControl()`
line — now at `internal/tools/network.go:234`, not `:223`, after item 3's landing — was deleted and
the new test went **red** on exactly the assertion the item names: *"the handler was reached 1
time(s)"*, with the result a plain `HTTP 200 OK` carrying the private page; restored, green, and
green under `go test -race ./internal/tools/... -run 'TestNetworkFunnel_' -count=2`. Recorded as a
second observation: with the hook deleted, **no other test in `./internal/tools/` fails** — the
audit's claim that the installation was carried by nothing is confirmed, not assumed. Three
departures from the item's literal text, all additive: (1) the rebinding is simulated **without a
rebinding nameserver** — the injected resolver answers a public IP for the pre-flight while the
transport resolves the same name for real, so the check-time/connect-time split is genuine; the
request is addressed `http://localhost:<port>` because an IP-literal host is classified directly and
never reaches the injected resolver, and hermeticity rests on `localhost` resolving through the
hosts file (no DNS, no network). (2) Two assertions beyond the item's list: the message must name
the **SSRF floor** (so an incidental transport failure cannot pass for a floor refusal, which is
what makes the mutation check tight) and must state the block exactly once, matching
`TestBlockedMessage_StatesTheBlockOnce`'s wording pin. (3) One test-only helper, `serverPort`, was
added beside the test to re-address an `httptest` server by name. No production code, and no doc,
was touched — CHANGELOG deliberately omitted: this item adds a regression net for behaviour that
is already correct and already shipped, so there is no user-facing change to record.

**What:** `SafeDialControl` is tested only as a bare function called directly with an address
string (`internal/security/ssrf_test.go:146`, `TestSafeDialControl_RebindClosesTOCTOU`). **No test
drives a URL that *passes* pre-flight and is stopped at connect.** Every real-dial test in
`internal/tools` disables the floor (`network_test.go:20`, `loopbackGuard()`), and every floor-on
test blocks at pre-flight — so the `if networkURLError(err)` branch in `do`
(`internal/tools/network.go:169-173`) never executes in the suite, and the `Control:
guard.SafeDialControl()` installation at `network.go:223` is unguarded. The behaviour is **correct
today** (confirmed by experiment during the audit); nothing defends it. Any refactor that drops the
hook, moves to a shared client, or wraps the transport leaves the whole suite green while a
prompt-injected model regains an IMDS/loopback path in Auto. This item adds the regression net and
changes **no production code**.
**Fix (the audit's own recipe):** one hermetic test in `internal/tools` — a guard whose injected
resolver returns a **public** IP (`security.URLGuard{}.WithResolver(...)`) plus an `httptest`
server addressed as `localhost:<port>`; assert `guard.CheckContext` **passes**, then
`NewWebFetch(guard).Execute` returns a non-Go-error `IsError` result, the handler was **never
reached**, and the message names the host once and carries no URL.

**Authoritative source:** the claim the test backs, stated in code at `internal/tools/network.go:29-32`
and `internal/security/ssrf.go:39-43` — *"the pre-flight Check is the cheap first line; the
dial-time control is **the real bound**"* — resting on ADR 0012 Amendment (2026-07-25)(d).

**Tests:** new `TestNetworkFunnel_DialTimeFloorBlocksAfterPreflightPasses` in
`internal/tools/network_funnel_test.go`. It must assert the **handler-never-reached** counter, not
just the error text — that is the half that catches a dropped `Control` hook.

**Acceptance — a fresh verifier runs:**
```
go test ./internal/tools/... -run TestNetworkFunnel_DialTimeFloorBlocksAfterPreflightPasses -v
go test -race ./internal/tools/... -run TestNetworkFunnel_ -count=2
go test ./... && go test -race ./...
```
plus the **mutation check**, which is this item's real acceptance: temporarily delete
`Control: guard.SafeDialControl()` at `internal/tools/network.go:223`, confirm the new test goes
**red**, restore, confirm green. Record both observations in the item's `NOTES`.

Commit: `test(tools): pin the dial-time SSRF floor through the funnel's own client`

---

## 5. M-7 — pin that the host-supplied `URLGuard` reaches all three network tools — ✅ DONE (2026-07-26)

NOTES (2026-07-26): **Mutation check (mandatory, performed):** each of the three call sites at
`internal/tools/registry.go:101-103` was changed to `security.URLGuard{}` in turn; each time the new
test went **red for exactly that tool's row** and green for the other two. Recorded as a second
observation (item 4's precedent): under the `web_fetch` mutation, **no other test in
`./internal/tools/` fails** — the audit's claim that the threading was carried by nothing is
confirmed, not assumed. Three departures from the item's literal text, all additive: (1) the row
asserts the **deny wording** (`is denied`) on top of "an error result naming url-safety" — this is
the load-bearing half, because the dropped-guard zero value *also* refuses `blocked.example`, via
the floor's `could not resolve host`, so a refusal-only assertion would have stayed green through
the exact regression the item exists to catch. (2) The table is *required* to cover every
`EffectNetwork` tool in the default set (a walk mirroring
`TestDefaultTools_EveryNetworkToolIsURLFiltered`), which turns the item's "a future fourth network
tool is a one-line addition" into a forced one rather than a remembered one. (3) `web_search` takes
no model-supplied URL, so its row reaches the denied host through
`HostTools.WebSearchEndpoint: "https://blocked.example/s"` — the only URL that tool ever requests.
The recipe's `publicStub` resolver is injected as written and keeps the green path hermetic (the
deny fires string-level, ahead of any resolution — the suite makes no DNS lookup; only the mutated
build does). No production code and no doc was touched, CHANGELOG deliberately omitted: like item
4, this adds a regression net for behaviour that is already correct and already shipped.

**What:** `internal/tools/registry.go:101-103` threads `NewWebFetch(host.URLGuard)` and its two
siblings, and **no test asserts it**. `internal/tools/registry_test.go` covers names, menu order,
counts and conditional registration (`TestNewDefaultRegistry_HoldsTheBuiltInTools` line 10,
`TestNewDefaultRegistry_MenuOrderIsDeterministic` line 44,
`TestNewDefaultRegistryWithHost_RegistersAskUserOnlyWithAsker` line 66) — but not this. Because the
**zero** `URLGuard` still has the SSRF floor on — and the composition root currently passes exactly
that (`cmd/apogee/wire.go:398`) — a regression replacing `host.URLGuard` with
`security.URLGuard{}` would drop an embedder's `DenyHosts` policy **silently**, with no failing
test and no visible symptom. This is the registry's public assembly seam, so the property belongs
there rather than in each tool. **No production code changes.**
**Fix (the audit's own recipe):** build the registry with
`HostTools{URLGuard: security.URLGuard{DenyHosts: []string{"blocked.example"}}.WithResolver(publicStub)}`,
look up `web_fetch`, `Execute` against `https://blocked.example/`, assert an error result naming
url-safety; repeat for `http_request` and `web_search`.

**Note (not this item's job):** the duplicated `HostTools` composition
(`internal/agent/construct.go:189-196` / `cmd/apogee/wire.go:396-402`) is what makes this droppable
on one path. That consolidation is **out of scope** (see *Out of scope*) — pin the property, do not
refactor the seam.

**Authoritative source:** ADR 0012 Amendment (2026-07-25)(a) — the marker asserts *"every outbound
URL passed the **host's** `URLGuard`"*; a guard that does not arrive makes the vouching false.

**Tests:** new `TestNewDefaultRegistryWithHost_ThreadsURLGuardIntoNetworkTools` in
`internal/tools/registry_test.go`, table-driven over the three tool names so a future fourth
network tool is a one-line addition.

**Acceptance — a fresh verifier runs:**
```
go test ./internal/tools/... -run 'TestNewDefaultRegistry' -v
go test ./... && go test -race ./...
```
plus the mutation check: temporarily change one of the three `NewWeb*/NewHTTP*` call sites at
`internal/tools/registry.go:101-103` to pass `security.URLGuard{}`, confirm the new test goes red
for **that** tool, restore. Record it in `NOTES`.

Commit: `test(tools): pin the host URLGuard threading into every network tool`

---

## 6. M-8 — pin the fail-closed decision for an erroring Approver — ✅ DONE (2026-07-26)

NOTES (2026-07-26): **Mutation check (mandatory, performed):** the error branch's
`return false, dispatchDone` at `internal/agent/dispatch.go:278` was changed to `return true`; both
new subtests went **red** on exactly the assertions the item names — *"tool ran 1 times after an
erroring Approver, want 0"*, a clean non-error result, and an audit record reading
`IsError:false Result:ran` instead of the refusal; restored, green, and green under
`go test -race ./...`. Recorded as a second observation (items 4/5 precedent): under the mutation
**no other test in the whole repo fails** — the audit's claim that this path was carried by nothing
is confirmed, not assumed. Three departures from the item's literal text, all additive: (1) the
test is table-driven over **two** gated classes — the recipe's MCP tool in Auto/`confine=true`, plus
the `unfiltered network reach` gate the item's own prose names as the one this audit exists to
check — since both hang off the same single Approver and the second row is one table entry. (2) One
assertion beyond the item's list: the `ErrorEvent` carrying `approver: prompt closed` must be
emitted, so the whole branch (`:272-279`) is pinned rather than only its return value — a gate that
could not be obtained must be visible, not silently indistinguishable from a human saying no. (3)
"audit-recorded as blocked" is asserted on the audit **ring record**'s error result (`IsError` plus
the refusal text) rather than on its `Decision`, because a denied gate carries the guard's
pass-through `allowed` decision by design (`resolution.go`'s `finishGate`) — the record's error
result is what distinguishes a blocked call from an executed one. The `fakeApprover`'s `decision` is
set to `ApprovalAllow` beside the error, so the row also proves the error outranks the meaningless
verdict returned with it. No production code and no doc was touched, CHANGELOG deliberately
omitted: like items 4 and 5, this adds a regression net for behaviour that is already correct and
already shipped.

**What:** `internal/agent/dispatch.go:272-279` — an Approver returning an error emits an
`ErrorEvent` and returns `(false, dispatchDone)`, so `executeGate` refuses. Correct — and
**untested**. The scaffolding already exists and is never used: `fakeApprover.err`
(`internal/agent/statemachine_test.go:99`) is declared and set by no test. This is the sole
human-in-the-loop for **every** gated class, including the `unfiltered network reach` gate this
audit exists to check. A refactor treating an Approver error as "no objection" (`return true`) — a
plausible mistake once the UI approver starts erroring on a closed prompt — silently converts every
gate into an unattended auto-run with zero failing tests. The nil-Approver ⇒ Refuse rule is well
covered (`internal/agent/resolution_test.go:493`, `TestResolve_NilApproverGateRefuses`); the
*erroring* Approver is a different path, covered nowhere. **No production code changes.**
**Fix (the audit's own recipe):** drive a gated call (an MCP tool in Auto, `confine=true`) with
`fakeApprover{err: errors.New("prompt closed")}`; assert the tool's `ran` counter is **0**, the
result is `IsError` with "denied by approver", and the call was **audit-recorded as blocked**.

**Authoritative source:** contract **§4** — *"a `Gate` always means the Approver is actually
consulted"*, and the no-Approver ⇒ `Refuse` overlay it states; ADR 0012's *gating is how
`confine=true`'s promise stays honest*.

**Tests:** new `TestDispatch_ApproverErrorRefuses` in `internal/agent/dispatch_test.go`, beside the
existing `TestDisposition_AutoConfineTrue` family (line 193 onward), using the already-present
`fakeApprover.err` field.

**Acceptance — a fresh verifier runs:**
```
go test ./internal/agent/... -run 'TestDispatch_ApproverErrorRefuses|TestResolve_NilApproverGateRefuses' -v
go test ./... && go test -race ./...
```
plus the mutation check: temporarily make the error branch at `internal/agent/dispatch.go:272-279`
return `true`, confirm red, restore. Record it in `NOTES`.

Commit: `test(agent): pin the fail-closed refusal when the Approver errors`

---

## 7. M-9 — adversarial tests for the floor's fail-closed paths and the numeric-encoding family — ✅ DONE (2026-07-26)

NOTES (2026-07-26): **Mutation check (mandatory, performed):** both fail-closed blocks were
mutated to `return nil` in turn. (A) the resolution-failure block (`internal/security/ssrf.go:164-166`)
— the item's own *"improving DX by letting an unresolvable host through"* edit — turned the new
tests **red** on exactly the assertions the item names: `resolution failure is blocked` reported
`Check = nil, want blocked (could not resolve host)`, and **all four** numeric forms went red in the
go-resolver mode (`Check(http://2130706433/) = nil, want blocked`, likewise `0177.0.0.1`,
`0x7f.0.0.1`, `127.1`) — i.e. the classic decimal loopback becomes a pre-flight pass, the exact
consequence the audit predicted. (B) the empty-answer block (`ssrf.go:167-169`) turned
`empty answer is blocked` red. Restored, all green, including `go test -race ./...`. Recorded as a
second observation (items 4/5/6 precedent): under **each** mutation, run over the **whole repo**, no
test outside these two functions fails — the audit's claim that both branches were carried by
nothing is confirmed, not assumed. Four departures from the item's literal text, all additive:
(1) `fixedResolver` was extended by re-expressing it over a new `stubResolver(ips, err)` rather than
by forking a second stub, so there is still exactly one resolver seam and the error/empty modes
reach the floor through it. (2) Every blocked row also asserts the **reason** the model-facing
message names (`could not resolve host` / `resolved to no addresses` / `blocked by the SSRF floor`)
on top of `errors.Is(…, ErrURLBlocked)` — the load-bearing half, since both branches refuse and a
refusal-only assertion could not tell the resolution block from the floor block, nor either from an
incidental failure. (3) A `public answer passes` negative-control row proves the two fail-closed
rows pin branches rather than a blanket refusal. (4) A `net.ParseIP decodes none of these forms`
subtest pins the actual **premise** of the invariant at `ssrf.go:24-31` — the safety rests on
resolution *because* the pre-flight cannot classify these forms directly; if the stdlib ever starts
decoding them, that prose note (not only the test) needs revisiting. No production code and no doc
was touched, CHANGELOG deliberately omitted: like items 4, 5 and 6, this adds a regression net for
behaviour that is already correct and already shipped.

**What:** `internal/security/ssrf.go:163-169` holds the resolution-failure and empty-answer blocks,
and `ssrf.go:24-31` states the numeric-encoding invariant **in the code itself**:
*"the numeric-encoding safety rests on resolution failing, not on the floor"* — an invariant
asserted by nobody. `fixedResolver` (`internal/security/ssrf_test.go:12-14`) never returns an error
and never returns an empty slice, so both `could not resolve host` and `resolved to no addresses`
are uncovered, and no test uses a numeric-encoded host. Someone "improving" DX by letting an
unresolvable host through to the transport turns the classic decimal-encoded loopback
(`http://2130706433/`) into a pre-flight pass; on a host whose resolver *does* decode `inet_aton`
forms (the cgo path, common on macOS) the same edit matters more, because the decoded private IP is
exactly what the floor was meant to catch. **No production code changes.**
**Fix (the audit's own recipe):** table over `{"2130706433", "0177.0.0.1", "0x7f.0.0.1", "127.1"}`
× **two injected resolvers** — one returning `(nil, errNoSuchHost)`, one mimicking `getaddrinfo` by
returning `127.0.0.1` — asserting both yield `errors.Is(err, ErrURLBlocked)` (the second
additionally `ErrSSRFBlocked`); plus a resolver returning `[]net.IP{}` for the empty-answer block.

**Authoritative source:** the invariant stated in code at `internal/security/ssrf.go:24-31`, under
ADR 0012 Amendment (2026-07-25)(d) (*the floor this rests on is unchanged*).

**Tests:** new `TestURLGuard_FloorFailsClosedOnResolution` and
`TestURLGuard_NumericEncodedHostsFailClosed` in `internal/security/ssrf_test.go`, beside
`TestURLGuard_SSRFFloor` (line 16). `fixedResolver` needs an error/empty-capable sibling — extend
it rather than forking a second stub.

**Acceptance — a fresh verifier runs:**
```
go test ./internal/security/... -run 'TestURLGuard_' -v
go test ./... && go test -race ./...
```
plus: `go test ./internal/security/... -run 'TestURLGuard_FloorFailsClosedOnResolution|TestURLGuard_NumericEncodedHostsFailClosed' -count=1 -v` names all four numeric forms and both resolver
modes in its subtest output.

Commit: `test(security): pin the SSRF floor's fail-closed and numeric-encoding paths`

---

## 8. M-1 — normalise the URL once, so the guard checks what the transport dials — ✅ DONE (2026-07-26)

NOTES (2026-07-26): The normal form is defined **once**, in `internal/security` as the exported
`NormalizeURL(raw) (*url.URL, error)`, and is called from both `URLGuard.CheckContext` and the top
of `networkTool.do` — rather than being written inline in `do` as the item's literal text says.
Two things forced it: (1) the item's own required `TestURLGuard_Check` rows demand that the **guard
alone** block `https://example.com.` and a Unicode host, which is only true if `CheckContext`
normalises; (2) `internal/mcp/transport.go:144` checks its endpoint through the same guard and then
dials it through the MCP SDK, so a `internal/tools`-local normaliser would have left that path
divergent. Writing it in both places is exactly the "second parse that can disagree" the item
forbids. **Acceptance greps move accordingly:** `grep -n "TrimSpace\|ToASCII"
internal/security/urlsafety.go` shows the single normalisation site; `grep -n "NormalizeURL"
internal/tools/network.go` shows `do` calling it; `grep -n "req.url" internal/tools/network.go`
shows the request built from `target` (the normalised string), not from `req.url`. Five further
departures: (1) `security.NormalizeURL` is a **new exported symbol in an internal package** — judged
*not* a Public-API change under the plan's intro, since ADR 0010 makes the public API the root
`apogee` facade, `internal/*` is not importable by embedders, and no alias, `apogee.go` edit or ADR
0010 bump is involved. (2) The dependency the item flagged was taken: `golang.org/x/net` is now a
direct requirement, pinned at **v0.55.0** rather than `@latest` (v0.57.0), because v0.57.0 drags
`golang.org/x/sys` from v0.45.0 to v0.47.0 and v0.55.0 is the newest release that leaves every
existing requirement untouched; `golang.org/x/text v0.37.0` comes in indirect. No punycode was
hand-rolled. (3) `idna.Lookup.ToASCII` is applied **only to a non-ASCII host**, and a mapping
failure keeps the original — mirroring `net/http`'s `canonicalAddr`/`idnaASCII` exactly, so the
checked name and the dialled name stay the same string even when IDNA refuses. (4) An unparseable
URL is passed to `CheckContext` **unchanged** instead of getting its own wording in `do`: the guard
already owns that message (and item 9 owns fixing it), so duplicating it in the funnel would have
put item 9's fix out of reach. (5) `safeHost` — the host-only failure **label** — is deliberately
left un-normalised: it feeds no request and no guard call, so it creates no check/dial divergence,
and touching it would move M2 message wording. **Mutation check (performed, both halves):**
(A) reverting `CheckContext` to `url.Parse(strings.TrimSpace(raw))` + `strings.ToLower(u.Hostname())`
turns **four** new `TestURLGuard_Check` rows red (both root-dot rows, both Unicode rows) — the
`upper-case host is denied` row stays green, correctly, because the pre-fix code already
lower-cased; it guards the lowercase step surviving the move into `NormalizeURL`. (B) reverting
`do` to check and build from `req.url` turns `TestNetworkFunnel_DoDialsTheURLTheGuardChecked` red on
exactly the item's whitespace claim — *"could not build request for host LOCALHOST.: parse: first
path segment in URL cannot contain colon"*. Recorded as a second observation:
`TestNetworkFunnel_DoTrailingDotDoesNotEscapeTheDenyList` stays **green** under (B), because the
guard's own normalisation already refuses it — the two tests pin different halves on purpose (the
deny row pins the guard, the observed-`Host`-header row pins the funnel), and neither is redundant.

**Ordering:** this item and items 9–10 are the **normalisation group** and **must land before** the
parked url-safety config key at **`TODO.md:285`** ("Dedicated url-safety config key for the network
tools"). That key is what populates `AllowHosts`/`DenyHosts` and converts the latent half of these
findings into live ones.

**What:** three divergences on one seam, confirmed by experiment in the audit. The guard parses
`strings.TrimSpace(raw)` (`internal/security/urlsafety.go:89`), lowercases `u.Hostname()`
(`:102`) and matches with `hostMatches` (`:135-146`); the request is built from the **untrimmed**
string at `internal/tools/network.go:156`
(`http.NewRequestWithContext(ctx, method, req.url, …)`), and Go's `canonicalAddr` applies
`idna.Lookup.ToASCII`:

| URL | guard matches on | transport dials |
|---|---|---|
| `http://ⓖxample.com/` | `ⓖxample.com` | `gxample.com:80` |
| `http://good.example.com。/` | `good.example.com。` | `good.example.com.:80` |
| `http://evil.com./` | `evil.com.` | `evil.com.:80` |

and separately `hostMatches("evil.com.", ["evil.com"])` is **`false`** — neither `==` nor
`HasSuffix(".evil.com")` — while DNS answers a trailing-dot FQDN identically and virtually every
virtual-host server accepts it. **Appending one dot defeats a `DenyHosts` entry.** The trim
divergence is the third: a leading space passes the guard on the trimmed form and then fails to
build the request on the untrimmed one (fails closed today, but it is the same class of bug — the
string checked is not the string requested). The SSRF floor is **not** affected (it judges resolved
IPs, and Unicode hosts fail closed because the non-ASCII name does not resolve), and both
composition roots pass a zero `security.URLGuard{}` (`cmd/apogee/wire.go:398`,
`internal/agent/construct.go:191`), which is why this is latent **today**.
**Fix:** normalise **once**, at the top of `networkTool.do`, and use the result for both the guard
and the request: `strings.TrimSpace`, `idna.Lookup.ToASCII`, lowercase, strip a single trailing
dot. Better still — and the preferred shape — **parse once in `do` and hand the `*url.URL` to
`http.NewRequestWithContext`**, so no second parse can disagree.

**Note — dependency:** `idna.Lookup.ToASCII` lives in `golang.org/x/net/idna`, which is **not**
currently in `go.mod` (the module has no `golang.org/x/net` requirement at all). Adding it is a new
direct dependency. That is not a design call the owner reserved, but it *is* a surprise: if the
implementer judges the dependency unwelcome, stop and ask rather than hand-rolling a punycode
encoder.

**Authoritative source:** ADR 0012 Amendment (2026-07-25)(a) — the marker asserts *every outbound
URL passed the host's `URLGuard`*, which is false if the guard judged a different string than the
transport dialled; contract **§4**'s **net** row rests on that assertion.

**Tests** (`internal/security/urlsafety_test.go` — `TestURLGuard_Check` at line 18 — and
`internal/tools/network_funnel_test.go`):
- table rows for `https://example.com.` and a Unicode host under `DenyHosts: ["example.com"]`,
  both asserting **blocked**;
- a leading/trailing-whitespace URL asserting guard and request agree (no build failure after a
  guard pass);
- a funnel-level test that the string handed to the transport is the normalised one — assert via
  the `httptest` server's observed `Host` header, not by inspecting internals.

**Acceptance — a fresh verifier runs:**
```
go test ./internal/security/... -run 'TestURLGuard_Check' -v
go test ./internal/tools/... -run 'TestNetworkFunnel_' -v
go test ./... && go test -race ./...
```
plus: `grep -n "TrimSpace\|ToASCII" internal/tools/network.go` shows the single normalisation site
in `do`, and `grep -n "req.url" internal/tools/network.go` shows the request no longer built from a
second, un-normalised string.

Commit: `fix(tools): normalise the request URL once so the guard checks what is dialled`

---

## 9. M-2 — stop `%q` escaping from defeating the URL redaction — ✅ DONE (2026-07-26)

NOTES (2026-07-26): Both halves of the item's fix were taken — `CheckContext` returns a bare
`fmt.Errorf("%w: unparseable url", ErrURLBlocked)` (the parse error's text is not interpolated at
all, the first of the two alternatives the item offers), **and** `redactSubstring` now strips
`strconv.Quote(secret)`'s inner form as well as the raw one. **Mutation check (performed, three
ways):** reverting the `%v` interpolation ALONE leaves every test green, and reverting
`redactSubstring` ALONE leaves every test green — each half independently defeats the leak, so
neither is pinned by the funnel tests on its own; that is why the item's `redactSubstring` unit
case matters, and it is the one thing that goes **red** under the second mutation. Reverting
**both** goes red on exactly the audit's string —
`"url blocked by url-safety (host the requested host): unparseable url: parse
\"http://example.com/search?key=SUPER-SECRET-API-KEY-1234\\x01x\": net/url: invalid control
character in URL"` — in three places: the new funnel row and both `web_fetch` / `http_request` rows.
Four departures from the item's literal text: (1) **the `web_search` configured-endpoint path was
already safe** and the leak is NOT reachable there, contrary to the item's prose —
`web_search.go:141` feeds `scrubURLError` the raw `*url.Error` that `buildSearchURL` returns, so
`errors.As` succeeds and the message is rebuilt from `ue.Op` + cause, which carries no URL; an
unparseable endpoint also never reaches `do` (`buildSearchURL` fails first, and the DDG-provider
branch that skips it requires a parseable endpoint). The live leak is the funnel's `CheckContext`
path, i.e. the model-supplied URL of `web_fetch` / `http_request`. The requested
`web_search_redaction_test.go` case is added anyway as the guardrail it is — it passes before and
after, and pins the `errors.As` reconstruction that keeps it safe. (2)
`TestNetworkFunnel_DoBlockedURLDoesNotLeakKey` became table-driven (floor row + control-character
row) and its assertions moved into a shared `assertNoKeyInAnyForm` helper — key AND request URL,
raw AND `%q`-escaped, which is the "neither raw nor `strconv.Quote`d form" the item asks for — reused
by the `web_search` case. (3) One row beyond the item's list, in `network_test.go`'s
`TestNetworkTools_FailureMessagesDoNotLeakKey`: the end-to-end proof through the real tools' results,
and a test the item's own acceptance command already runs. (4) Doc comments that the change made
false were corrected: `Check`'s "a parse error for a malformed one", `redactRequestURL`'s trimmed-form
rationale (url-safety no longer quotes anything back — the trimmed pass now stands on nested-error
defence in depth alone) and the stale sentence in `TestNetworkTools_FailureMessagesDoNotLeakKey`'s
comment. No doc under `docs/design/` and no ADR was touched; CHANGELOG carries the user-facing entry.

**Ordering:** normalisation group — lands **before** the parked url-safety config key
(`TODO.md:285`), with items 8 and 10.

**What:** `internal/security/urlsafety.go:91` builds
`fmt.Errorf("%w: unparseable url: %v", ErrURLBlocked, err)`. `url.Parse`'s error is a `*url.Error`
whose `Error()` is `fmt.Sprintf("%s %q: %s", …)` — `%q` **escapes** the URL, so the text carries
`http://…?key=SECRET\x01x` with a *literal* backslash-x, while `redactSubstring` searches for the
raw byte sequence: no match, nothing redacted (`internal/tools/network.go:300-330`,
`scrubURLError` → `redactRequestURL` → `redactSubstring`). The `%v` (rather than `%w`) also means
`errors.As(err, &ue)` in `scrubURLError` fails, so the URL-free `ue.Op`+cause reconstruction never
runs on this path either. The audit confirmed the model-facing string by experiment:
`url blocked by url-safety (host the requested host): unparseable url: parse "http://example.com/?key=SECRET\x01x": net/url: invalid control character in URL`.
The trigger is any URL argument carrying an **interior** ASCII control character (`TrimSpace` only
strips the ends), and the key-bearing case is reachable through `internal/tools/web_search.go:141`,
where an unparseable configured endpoint — which may carry an API key — is fed to `scrubURLError`.
**Fix:** stop interpolating the parse error's text at all — return
`fmt.Errorf("%w: unparseable url", ErrURLBlocked)` from `CheckContext` (or wrap with `%w` so
`scrubURLError`'s `errors.As` path drops the URL) — and make `redactSubstring` **also** strip
`strconv.Quote(secret)`'s inner form as defence in depth.

**Authoritative source:** the funnel's own stated **M2 discipline**, verbatim at
`internal/tools/network.go:132` — *"a key-bearing request URL can never ride out to the model"* —
generalized to every network tool by ADR 0012 Amendment (2026-07-25)(d).

**Tests:**
- extend `internal/tools/network_funnel_test.go`'s
  `TestNetworkFunnel_DoBlockedURLDoesNotLeakKey` (line 244) with an **interior control character**
  in a key-bearing URL, asserting the key appears in neither raw nor `strconv.Quote`d form;
- the same case through `web_search`'s configured-endpoint path
  (`internal/tools/web_search_redaction_test.go` is the existing guardrail);
- a `redactSubstring` unit case for the quoted form.

**Acceptance — a fresh verifier runs:**
```
go test ./internal/tools/... -run 'TestNetworkFunnel_Do.*LeakKey|TestNetworkTools_FailureMessagesDoNotLeakKey|TestWebSearch_' -v
go test ./internal/security/... -run 'TestURLGuard_Check' -v
go test ./... && go test -race ./...
```
plus: `grep -n "unparseable url" internal/security/urlsafety.go` shows no `%v` of the parse error.

Commit: `fix(security): keep the unparseable-url error from leaking a key-bearing URL`

---

## 10. M-3 — decode the RFC 8215 NAT64 local-use prefix in the SSRF floor — ✅ DONE (2026-07-26)

NOTES (2026-07-26): **The local-use prefix is DENIED WHOLESALE, not decoded — a ratified departure
from this item's literal "decode the embedded v4 from the low 32 bits" text.** The first attempt
implemented that text and **failed independent verification**: RFC 8215 fixes no translation prefix
length, so a `/64`-style translator inside `64:ff9b:1::/48` puts the v4 at bits 72-103, and the
low-32 read there sees a *public* value while the gateway forwards to the private target — the
verifier demonstrated this item's own IMDS scenario still succeeding via
`http://[64:ff9b:1:abcd:a9:fea9:fe00:0]/latest/meta-data/`. The `/48` and `/56` suffix bits are
likewise caller-controlled, so the first attempt's incidental "zero suffix reads as 0.0.0.0"
coverage was defeated by a crafted non-zero suffix. The owner ratified the wholesale deny: the whole
`/48` is reserved local-use space with no legitimate public destination, so no bit-offset decode is
attempted for it and it joins `floorDeniedV6Nets` beside the three prefixes the item's Fix line
authorises (6to4 `2002::/16`, IPv4-compatible `::/96`, site-local `fec0::/10`). The **well-known**
prefix `64:ff9b::/96` keeps its decode untouched — RFC 6052 *does* fix that one at `/96`, so the low
32 bits are unambiguous and there is no suffix to craft; `nat64WellKnownPrefix` is therefore back to
a single var rather than the first attempt's slice. Consequences: a local-use address embedding a
**public** v4 is now blocked too (the deny is the range), and `ErrSSRFBlocked`'s model-facing
parenthetical names `NAT64-local-use` alongside `NAT64-embedded`/`obsolete-v6` (no test pins the
full sentinel string). **Mutation check (mandatory, performed):** the rows were written and run
**first** against the pre-fix floor — the four new evasion rows went red in *both* tables
(`Check("http://[64:ff9b:1:abcd:a9:fea9:fe00:0]") = nil, want blocked`, plus the crafted-suffix
`/48` `64:ff9b:1:a9fe:a9:fe11:2233:4455` and `/56` `64:ff9b:1:22a9:fe:a9fe:9988:7766` forms and the
flipped public-v4 row), and the boundary controls (`64:ff9b:2::7f00:1`, `2003::1`, the well-known
`64:ff9b::5db8:d822` public passthrough) stayed green throughout; after the fix all are green, as
are `TestURLGuard_FloorIsTightenOnly`, `go test ./...` and `go test -race ./...`. The floor's
self-documentation was corrected everywhere it claimed a decode of the local-use prefix — the
package comment, `ipBlockedByFloor`'s doc comment, `resolveAndCheckFloor`'s literal-forms note, and
`CONTEXT.md`'s url-safety paragraph — and the CHANGELOG entry was rewritten (its earlier "Both
prefixes are now decoded the same way" was untrue). `docs/design/technical-design.md:200` also names
the well-known prefix but is deliberately **left alone**: that cell is a historical record of what
the P3.11 / SEC-01 pass landed, and it remains true of that pass.

**Ordering:** normalisation group — lands **before** the parked url-safety config key
(`TODO.md:285`), with items 8 and 9.

**What:** `internal/security/ssrf.go:67-71` defines `nat64WellKnownPrefix = 64:ff9b::/96` and
`:106-139` (`ipBlockedByFloor`) decodes the embedded v4 of such an address precisely because *"a
NAT64 gateway maps the embedded IPv4 onto a real v4 destination"*. It does **not** decode the RFC
8215 **local-use** prefix `64:ff9b:1::/48`, which exists specifically for translating to
non-global (private) IPv4 space. Verified against the floor's own logic: `64:ff9b::7f00:1` →
blocked; `64:ff9b:1::7f00:1` → **not blocked**. On a network running NAT64 with a local-use prefix
— the realistic case for an IPv6-only enterprise or mobile network — a prompt-injected model in
Auto reaches `http://[64:ff9b:1::a9fe:a9fe]/latest/meta-data/` and the gateway translates it to the
IMDS at 169.254.169.254. The same gap covers 6to4 (`2002::/16`), IPv4-compatible (`::a.b.c.d`) and
deprecated site-local (`fec0::/10`) — those three were confirmed unroutable on the audit host, so
they are **hardening rather than exposure**.
**Fix:** add `64:ff9b:1::/48` to the NAT64 decode and, since the local-use prefix is a `/48`,
decode the embedded v4 from the **low 32 bits** the same way; optionally deny `2002::/16`,
`::/96`-embedded v4 and `fec0::/10` outright, which no coding-agent fetch legitimately targets.
Update the floor's self-documentation, which today claims coverage of *"the embedded v4 inside a
NAT64 well-known-prefix address"* without noting the sibling prefix exists.

**Authoritative source:** ADR 0012 Amendment (2026-07-25)(d) — *"the `URLGuard`'s default-on,
resolved-IP SSRF floor (pre-flight **and** at dial time) is moved into the funnel, not
revisited"* — i.e. the floor is load-bearing policy and must actually cover what it claims.

**Tests:** extend `internal/security/ssrf_test.go`'s `TestIPBlockedByFloor` (line 199) and
`TestURLGuard_SSRFFloor` (line 16) with `64:ff9b:1::7f00:1` (loopback), `64:ff9b:1::a9fe:a9fe`
(IMDS/link-local) and `64:ff9b:1::c0a8:1` (RFC 1918), plus whichever of the three optional prefixes
the implementation adds. Keep `TestURLGuard_FloorIsTightenOnly` (line 118) green — this change is
tighten-only by construction.

**Acceptance — a fresh verifier runs:**
```
go test ./internal/security/... -run 'TestIPBlockedByFloor|TestURLGuard_SSRFFloor|TestURLGuard_FloorIsTightenOnly' -v
go test ./... && go test -race ./...
```
plus: `grep -n "64:ff9b" internal/security/ssrf.go` shows both prefixes and a comment naming RFC
8215.

Commit: `fix(security): decode the RFC 8215 NAT64 local-use prefix in the SSRF floor`

---

## 11. M-4 — stop leaking a transport, an idle socket and two goroutines per network call — ✅ DONE (2026-07-26)

NOTES (2026-07-26): The item's **first** alternative was taken — `defer client.CloseIdleConnections()`
in `do` — not the per-`networkTool` hoisted transport. Reason: `networkTool` is a VALUE struct built
inline (`networkTool{guard: …}`) at several sites, so a transport field would be nil on any
construction path that missed a constructor, and a nil `Transport` makes `http.Client` fall back to
`http.DefaultTransport` — a silently unguarded dial with no `SafeDialControl`. The per-call
`newHTTPClient` cannot fail open that way. Placement is load-bearing: the release is deferred
**before** `resp.Body.Close()` so it runs **after** it (LIFO), and `Transport.CloseIdleConnections`
additionally latches the transport into closing *newly* idle connections, so the connection the
read-loop hands back a moment later closes too rather than racing the drain. `newHTTPClient`'s body
(`Control:`, `CheckRedirect`, the timeout) is untouched; only its doc comment gained the per-call
lifetime note. **Departure on the test:** the assertion is neither of the two the item lists (a
counting `RoundTripper` seam, or a same-pointer transport) — both pin the *mechanism*, and the second
is only available to the shape that was **not** taken.
`TestNetworkFunnel_DoReleasesTheConnectionItOpened` pins the *outcome* where the leak is actually
observable, on the server's own `ConnState` bookkeeping: after 3 sequential `do` calls every
connection the server saw must be closed, waited for against a 5 s deadline rather than a sleep (the
green path costs ~10 ms; a leaked connection sits in the pool for the 30 s idle timeout, far past
it). **Mutation check (mandatory, performed):** deleting the `defer client.CloseIdleConnections()`
line turns the new test **red** on exactly that count — *"0 of 3 connection(s) released after the
calls returned"* — and, run over the **whole repo**, no other test fails, so the audit's claim that
the per-call transport was carried by nothing is confirmed, not assumed; restored, green, including
`go test -race ./internal/tools/... -run 'TestNetworkFunnel_' -count=2`. CHANGELOG carries the
user-facing entry (unlike items 4–7 this changes runtime behaviour); no doc under `docs/design/` and
no ADR was touched.

**What:** `internal/tools/network.go:150` calls `newHTTPClient(...)` **inside `do`**, so every call
allocates a fresh `*http.Transport` (`:220-243`), and nothing calls `CloseIdleConnections()`. Go
keeps each pooled connection — and its `readLoop`/`writeLoop` goroutines — alive until
`IdleConnTimeout` (30s), and those goroutines keep the transport itself from being collected.
Network tools **auto-run unattended in Auto**, so dozens of calls in a turn is the normal case: the
process accumulates an open socket plus two goroutines per call for 30 seconds while also paying a
fresh TCP+TLS handshake every time. `internal/mcp/transport.go:154` builds the identical client but
keeps it **long-lived**, which is correct — the divergence is that the funnel's is per-call.
**Fix:** `defer client.CloseIdleConnections()` in `do` after the body is read (one line), **or**
build the transport once per `networkTool` and set only `Client.Timeout` per call. Do **not**
consolidate the two client builders — that is the out-of-scope deepening candidate.

**Authoritative source:** none named by the audit beyond the funnel's own contract; the comparison
point is `internal/mcp/transport.go:154`. This is a correctness/resource fix, not a policy change —
the `Control` hook, `CheckRedirect` policy and timeout ceiling must survive it byte for byte.

**Tests:** a `runtime.NumGoroutine()`-delta assertion is flaky by nature — instead assert
structurally: after N sequential `do` calls against one `httptest` server, either
`CloseIdleConnections` was called N times (a counting `http.RoundTripper` seam or an explicit
`defer` visible in a focused unit test) or the transport is the **same pointer** across calls.
Existing `TestNetworkFunnel_DoSuccess` (line 92) and `TestNetworkFunnel_TimeoutResolution`
(line 362) must stay green — in particular the per-call timeout must still vary if the transport is
hoisted.

**Acceptance — a fresh verifier runs:**
```
go test ./internal/tools/... -run 'TestNetworkFunnel_' -v
go test -race ./internal/tools/... -run 'TestNetworkFunnel_' -count=2
go test ./... && go test -race ./...
```
plus: `grep -n "CloseIdleConnections\|newHTTPClient" internal/tools/network.go` shows the fix, and
the diff leaves `Control:`, `CheckRedirect` and `clampDuration` untouched.

Commit: `fix(tools): release the funnel's transport instead of leaking one per call`

---

## 12. M-5 — put the pre-flight DNS resolution under the same timeout budget — ✅ DONE (2026-07-26)

NOTES (2026-07-26, second attempt — supersedes the first attempt's per-phase note): the first
attempt read the item's `Fix:` line literally and derived `rctx` for the **pre-flight only**,
leaving `newHTTPClient` to hand the request a *fresh copy* of the same budget. Every phase was
then bounded but the CALL was not: a 1 s ask with a 0.7 s lookup measured **1.703 s**, so
`maxNetworkTimeout` was still no ceiling on a call and the attempt's own CHANGELOG claim ("a
request that asks for one second takes about one second") was false. Independent verification
failed it, and the owner ratified the shared-deadline reading of the item's own words — "so one
budget covers resolve + dial + body". **What landed:** `budget := clampDuration(req.timeout)` is
hoisted once; `rctx, cancelBudget := context.WithTimeout(ctx, budget)` is started BEFORE the
pre-flight; and `rctx` is threaded into **both** `CheckContext` and
`http.NewRequestWithContext` (`network.go:210`), so resolve + dial + body spend ONE deadline
between them. `newHTTPClient` keeps the same budget as its client `Timeout`: its clock starts at
`client.Do`, so it is always the looser bound and can only serve as a backstop — documented as
such rather than left to be read as a second budget. Two further departures from the item's
literal text, both carried over from the first attempt and re-verified under the shared deadline:
(1) on a `CheckContext` error the funnel re-checks the **caller's** `ctx.Err()` first and returns
ADR 0007's Go-error shape for it, keeping the message shape for a spent budget — the item's own
interaction note made mechanical, and *tightening*, since a caller cancellation during the
pre-flight used to return `(msg, nil)` and leave the Turn un-rolled-back; (2) that brings a fourth
subtest to `TestNetworkFunnel_DoCancelledCtxIsGoError` ("cancelled during the pre-flight resolve")
beside the budget subtest in `TestNetworkFunnel_TimeoutResolution`, both driven by one hermetic
`blackHoleResolver` helper. **New test (the item's Tests line could not pin the shared half):**
`TestNetworkFunnel_OneBudgetCoversResolveAndRequest` drives a slow-but-SUCCESSFUL pre-flight
(injected guard resolver, 500 ms, answers public) plus a hanging dial under a 600 ms budget and
asserts wall clock. The hanging dial is hermetic — no network, no privileges — by pointing the Go
resolver's DNS `Dial` at a socket that never answers; that is the one seam the funnel does not
inject, so it is `net.DefaultResolver`, a process global, which is why this test alone is **not**
`t.Parallel()` (Go finishes the serial tests before resuming any parallel one, and nothing else in
the package resolves a name through DNS). It measured **601 ms** for the 600 ms budget.
**Mutation check (mandatory, performed, both halves, each run over the WHOLE repo):** (A) putting
the request back on the caller's ctx (`NewRequestWithContext(ctx, …)`) turns the new test **red**
at **1.103 s** — the per-phase sum, exactly the defect verification caught — and **no other test
in the repo fails**, confirming the bound was carried by nothing before. (B) deleting the
caller-`ctx.Err()` branch turns the pre-flight cancellation subtest **red** on both assertions
(nil error plus `url blocked by url-safety (host black-hole.example): … context canceled`), again
with no other failure repo-wide. Restored and green, including `go test -race
./internal/tools/... -run 'TestNetworkFunnel_' -count=5` (0.60–0.61 s each run, no flake). Doc
comments were corrected to the shared-deadline wording (`defaultNetworkTimeout`, `do`'s
three-shape comment, `newHTTPClient`'s timeout paragraph); no doc under `docs/design/` and no ADR
mentions this bound, so none was touched. The CHANGELOG entry was rewritten to say a single
SHARED deadline and that the two-minute ceiling bounds a whole call — the claim the first
attempt's code did not support.

**What:** `internal/tools/network.go:146` runs `n.guard.CheckContext(ctx, req.url)` on the **raw
caller ctx**, *before* `newHTTPClient` at `:150`. The floor's `LookupIPAddr`
(`internal/security/ssrf.go:163`) is therefore bounded only by the system resolver configuration,
not by the resolved request timeout, and dispatch adds no per-tool deadline
(`internal/agent/dispatch.go:308-348` passes ctx straight through). This contradicts the funnel's
own words at `network.go:43-45` — *"bounds a single network call so a slow/hung endpoint never
wedges a Turn"*. A host delegated to a black-holing nameserver blocks for
`timeout × attempts × nservers` (default ~10s; `options timeout:30 attempts:5` in `/etc/resolv.conf`
makes it **minutes**) **on top of** the HTTP timeout, and `http_request`'s `timeout_seconds: 1` does
not bound it at all.
**Fix:** derive the check's ctx from the resolved timeout —
`rctx, cancel := context.WithTimeout(ctx, clampDuration(req.timeout)); defer cancel()` — and pass
`rctx` to `CheckContext`, so one budget covers resolve + dial + body.

**Authoritative source:** the funnel's stated bound at `internal/tools/network.go:43-45`. Note the
interaction with item 3: a ctx that expires during the *pre-flight* must still produce the blocked
**message** shape, not the Go-error shape, because the *caller's* ctx is not cancelled — ADR 0007's
rule is about caller cancellation. If the two items disagree on which shape wins, that is a
stop-and-ask.

**Tests:** extend `internal/tools/network_funnel_test.go`'s `TestNetworkFunnel_TimeoutResolution`
(line 362) with an injected resolver that blocks past the resolved timeout, asserting `do` returns
within roughly the budget and yields a message (not a Go error) — assert against the *budget*, not
a wall-clock sleep, in line with the existing test's style.

**Acceptance — a fresh verifier runs:**
```
go test ./internal/tools/... -run 'TestNetworkFunnel_TimeoutResolution|TestNetworkFunnel_Do' -v
go test -race ./internal/tools/... -run 'TestNetworkFunnel_' -count=2
go test ./... && go test -race ./...
```
plus: `grep -n "CheckContext" internal/tools/network.go` shows the call taking the derived ctx.

Commit: `fix(tools): bound the pre-flight DNS resolution by the resolved request timeout`

---

## 13. M-6 — show the model where a refused redirect points — ✅ DONE (2026-07-26)

NOTES (2026-07-26): One departure from the item's literal text, additive and tighten-only: the
`Location` is not rendered RAW. It is server-chosen text, and this render is the one place it is
lifted out of the body and into the header block the model reads as fact, so `redirectTarget`
(unexported, in `web_fetch.go`) neuters it first — control (Cc), format (Cf), private-use (Co) and
surrogate (Cs) runes dropped and whitespace runs folded, the same directive-inert shape
`library.SanitizeContent` uses — and caps it at `maxLocationBytes` (2048), marking a cut value. Both
halves were established by experiment, not assumed: `net/http` refuses a C0 byte in a response
header value and Go's client refuses a Location it cannot parse *before* `CheckRedirect` is
consulted, but a bidi override / zero-width character passes both, an obs-fold (or a CRLF the server
maps to spaces) folds injected text into the value, and the header block sits OUTSIDE
`maxNetworkResponseBytes` (net/http accepts a 10 MiB one), so an unbounded Location was a way around
the body cap. Folding rather than cutting at the first space is deliberate — a truncated target
would be a WRONG URL for the servers that emit an unencoded space, and folded text can open neither
a header line nor a body of its own. **Nothing is redacted** out of the value: the Location is
response content like the body beside it, the funnel's M2 rule is about failure messages naming the
REQUEST url, and a canonicalising redirect legitimately echoes that url — blanking it would hand
back a target that cannot be followed. Two additions beyond the item's three required tests, both
demanded by this item's leak surface: `TestWebFetch_HostileRedirectLocationIsRenderedInert` (CRLF
injection, a fake status line in the value, bidi + zero-width, an oversized value) and — in
`web_search_redaction_test.go` — `TestWebSearch_RedirectDoesNotRenderTheLocation`, the credential
half: web_search's endpoint may carry a config'd API key and a search backend answering 3xx with the
request URL echoed back would hand it straight to the model if the rendering had gone into the
FUNNEL instead of into web_fetch's own renderer; its non-2xx policy stays status + host.
**Mutation check (performed, both halves):** (A) with the render deleted, all three required cases go
red plus the extended `TestWebFetch_DoesNotFollowRedirectToPrivate`; (B) with the raw
`resp.header.Get("Location")` rendered instead of `redirectTarget`, the bidi/zero-width row, the
whitespace-fold row and the oversize row go red (`header block is 65604 bytes`) — restored, green,
including `go test -race ./...`. `network.go` is untouched: no `CheckRedirect` code moved.

**What:** `internal/tools/network.go:236-241` justifies the no-follow policy with *"The model sees
the redirect `Location` and can choose to follow it through a fresh, re-checked call"* — but
`renderFetchResult` (`internal/tools/web_fetch.go:79-91`) emits only the status line and
`Content-Type`; `Location` is **dropped**. A plain `http://` → `https://` or trailing-slash
canonicalisation — the common case — leaves a small model holding `HTTP 302 Found` with an empty
body and no way forward, and it is a documented affordance the code does not deliver.
`http_request` renders the full sorted header block and does not have the problem, which is the
divergence. `internal/tools/network_test.go:115-135`
(`TestWebFetch_DoesNotFollowRedirectToPrivate`) pins only that the status is 302, so nothing
notices.
**Fix:** emit `Location` in `renderFetchResult` for any **3xx** status. The redirect **policy** is
unchanged — this item must not make `web_fetch` follow anything.

**Authoritative source:** the funnel's own rationale at `internal/tools/network.go:236-241`; ADR
0012 Amendment (2026-07-25) explicitly leaves the redirect policy alone, so only the *rendering*
moves.

**Tests:** extend `TestWebFetch_DoesNotFollowRedirectToPrivate`
(`internal/tools/network_test.go:115`) to assert the rendered result contains the `Location` value,
plus a case where a 3xx carries **no** `Location` (no stray empty line) and a 200 that carries a
`Location` header (not rendered — 3xx only).

**Acceptance — a fresh verifier runs:**
```
go test ./internal/tools/... -run 'TestWebFetch_' -v
go test ./... && go test -race ./...
```
plus: `grep -n "Location" internal/tools/web_fetch.go` shows the 3xx-guarded render, and the diff
touches no `CheckRedirect` code.

Commit: `fix(tools): render the refused redirect's Location so the model can follow it`

---

## 14. M-10 — the SSRF floor over a user-typed MCP endpoint — ✅ DONE (2026-07-26)

NOTES (2026-07-26): Owner's design call taken as given — (A) EXEMPT the configured endpoint from the
pre-flight resolved-IP floor, keeping scheme/host allow-deny (an `mcp-servers:` endpoint is
config-file-only and never model-supplied, so the anti-model floor is the wrong control there);
(B) the dial-time control becomes ENDPOINT-AWARE rather than blanket or dropped — required, since a
pre-flight exemption alone is a no-op against a blanket `SafeDialControl`; (C) docs AND a dated
ADR 0012 amendment in the same commit, since Amendment (2026-07-25)(d)'s *"stays where it is"* is
the line this contradicts; (D) adopt the funnel's no-follow `CheckRedirect` WITHOUT consolidating
the two client builders; (E) folded in by the owner — `checkEndpoint` validated the normalised form
and handed the SDK the RAW `cfg.Endpoint`, the same check-one-string/dial-another divergence item 8
removed from the funnel. Eleven departures/decisions worth naming: (1) **"refuse everything else" at
dial is implemented as "permit the endpoint's own resolved addresses, and judge every other address
by the floor exactly as before"**, not as pin-only. The purpose clause the owner wrote names *a
DIFFERENT private IP*, which this catches exactly; pin-only would additionally break a public MCP
server whose name rotates across a CDN's addresses mid-session, for no security gain (a public
address is not an SSRF pivot, and the floor never bounded one). The ONLY thing exempted anywhere is
the endpoint's own addresses. (2) One new exported symbol, `security.PinnedDialControl(ctx, host)`,
in an **internal** package — judged *not* a Public-API change on item 8's precedent (ADR 0010 makes
the public API the root `apogee` facade; `internal/*` is not importable by embedders), and no facade
symbol, alias or `apogee.go` edit is involved. It is unit-tested where it lives
(`internal/security/ssrf_test.go`: pinned/unpinned/public/v4-mapped rows, the three fail-closed
no-addresses cases, IP-literal-needs-no-lookup, floor-off) **and** end-to-end through the real built
transport (`internal/mcp/transport_test.go`). (3) The pre-flight exemption needed **no** new API and
**no** new config key (the plan permitted one): it is the existing `DisableIPFloor()` at ONE call
site, whose doc comment now names that single production use. (4) `SafeDialControl` and the new
pinned control share one `dialAddressIP` helper, and `resolveAndCheckFloor`'s resolve block was
extracted as `resolveHost` and shared with pinning — a divergence between the two controls, or
between the check-time and pin-time lookups, would itself be the security bug; behaviour and item
7's pinned reason strings (`could not resolve host` / `resolved to no addresses`) are byte-identical.
(5) `checkEndpoint` now returns the normalised `*url.URL` and reports an unparseable endpoint with
its own constant wording (`mcp: server %q has an unparseable endpoint`) instead of deferring to the
guard as `do` does — a configured endpoint may carry a token in its query and `url.Parse`'s error
quotes the URL back (item 9's reasoning); the outcome, a refusal, is unchanged. (6) An **unresolvable**
endpoint is now refused by the PIN instead of by the pre-flight floor — the same fail-closed
outcome by a different path, pinned by `TestBuildTransport_UnresolvableEndpointFailsClosed`, because
an endpoint whose addresses are unknown has nothing to exempt. (7) Existing test revisited, not
deleted: `TestBuildTransport_HTTPEndpointBlockedBySSRFFloor` → `…BlockedByURLSafety`, now driving a
**denied host** (the half that survives) instead of a loopback endpoint (the half the amendment
reverses); the loopback case reappears as a positive in `transport_test.go`. (8) Pinning is
**address-grain, not address+port** — a second service on the same pinned IP would be dialable if
something pointed the transport at it; nothing does (redirects are not followed), and a port-grain
pin would break a server that legitimately moves ports mid-session. (9) Docs beyond the item's named
set were corrected because they stated the now-false *"rides the same SSRF floor"* claim:
`CONTEXT.md`, `docs/design/mcp-client.md` and the MCP-client row of `docs/design/technical-design.md`
(one clause each). `docs/design/confinement-execution-contract.md` was **not** touched — item 1 owns
§4 and the **mcp** row is unmoved. (10) The two client builders are **not** consolidated (explicitly
out of scope); `newGuardedHTTPClient`'s doc comment now records why it is long-lived where the
funnel's is per-call, and that the seam is a deepening candidate. (11) **Mutation checks (performed,
four):** blanket-permitting the pinned control turns the rebind and unpinned-private rows **red**
(*"a rebound connect succeeded"*, and a 10 s dial timeout instead of a floor refusal); deleting
`CheckRedirect` turns the redirect test red (*"status = 200; want the 302 itself"*, target fetched 1
time); handing the SDK `cfg.Endpoint` again turns the normalisation test red (*"SDK endpoint = "  http://MCP.Example.COM./sse  ""*);
and restoring `guard.CheckContext` on the pre-flight turns all six loopback/LAN rows red with the
floor's own message. Restored, all green, including `go test -race ./...`. The positive pin case
carries its own negative control (*"the blanket floor would have refused it"*), so it cannot pass by
the floor being absent.

**Design call:** the audit reports this as **"warrants revisiting"**, not as a defect — the current
behaviour **is** documented (`cmd/apogee/defaults/config.yaml:89-90`, `internal/mcp/doc.go:22-28`)
and the question is whether the policy is right. Stop and ask: (a) **should a configured MCP
endpoint be exempt from the pre-flight resolved-IP floor** (treated as a host trust decision like
`endpoint:`), or gated behind a **per-server opt-in**, or left as-is with better failure wording?
(b) whichever way it goes, the **dial-time control stays** for the rebinding case — confirm. (c)
does `internal/mcp/doc.go` / the shipped config template need rewording, and does ADR 0012 need an
amendment, or is this purely a config-surface change? (d) the adjacent divergence — should
`internal/mcp/transport.go:154-169` adopt the funnel's `CheckRedirect` policy? Note that "export
one client constructor and have both call sites use it" is the **out-of-scope deepening
candidate**; if the owner wants the redirect policy fixed *now*, it must be done without
consolidating the builders. Do not decide any of this in-item.

**What (the finding, for the owner's decision):** `cmd/apogee/wire.go:241-244` passes
`security.URLGuard{}` into `Connect`, so `checkEndpoint` (`internal/mcp/transport.go:140-147`) runs
the resolved-IP floor over the configured `endpoint:`. `http://127.0.0.1:7331/mcp` or
`http://192.168.64.1:7331/mcp` is refused as loopback/private, `Connect` is all-or-nothing
(`internal/mcp/client.go:66-71`) so the whole set rolls back, and `wire.go` turns that into a fatal
`return err` — **apogee will not start**. There is no config escape: `DisableIPFloor` is
deliberately code-level only (`internal/security/urlsafety.go:54-61`). The asymmetry is sharp: the
LLM `endpoint:` — the same category of user-chosen, config-supplied address, and routinely private
(the shipped template at `cmd/apogee/defaults/config.yaml:15` is `http://192.168.64.1:1111`) — is
dialled with an **unguarded** `&http.Client{}` (`internal/provider/client.go:90`). The floor exists
to stop the **model** pivoting to internal addresses; an MCP endpoint is never model-supplied. The
adjacent divergence: `internal/mcp/transport.go:154-169` reproduces the funnel's client builder
field for field but **drops the `CheckRedirect` policy**, so MCP HTTP transports auto-follow
redirects while Apogee's own tools refuse to — harmless today (the dial-time floor re-checks each
redirected connect) but a string-level allow/deny bypass on that path once the parked config key
(`TODO.md:285`) lands.

**Authoritative source:** ADR 0012 Amendment (2026-07-25)(d) — *"the MCP transport's connect-time
endpoint check is a different lifecycle and **stays where it is**"* — which is exactly the line the
design call may need to amend; contract **§4**'s **mcp** row governs the *per-call* disposition and
is **not** in question here (item 1 owns §4 regardless).

**Tests:** determined by the design call. Whatever is chosen, pin it: a private/loopback MCP
endpoint either connects (exempt path) or fails with a message that names the endpoint and the
escape, and the dial-time control still blocks a rebind on that transport. If (d) is taken, a test
that the MCP transport does not follow a redirect.

**Acceptance — a fresh verifier runs:**
```
go test ./internal/mcp/... -v
go test ./cmd/... -v
go test ./... && go test -race ./...
```
plus: whichever docs the design call touches (`cmd/apogee/defaults/config.yaml:89-90`,
`internal/mcp/doc.go:22-28`, possibly an ADR 0012 amendment) are updated in the **same** commit as
the behaviour, and a manual owner check that `apogee` starts against
`http://192.168.64.1:7331/mcp` (the live llama-launcher endpoint) under the chosen policy.

Commit: `fix(mcp): exempt the configured endpoint from the pre-flight SSRF floor`
*(reword to match the decision — e.g. `docs(mcp): record why the endpoint floor stays` if the owner
keeps today's behaviour.)*

---

## 15. Follow-up (items 1, 10, 14) — correct the doc and comment claims those fixes made stale — ✅ DONE (2026-07-26)

NOTES (2026-07-26): **ADJUDICATION — `technical-design.md:200` is a CURRENT-STATE claim; the four
wholesale denies were ADDED.** Reasoning, on the row's own terms: §5's table header says *"Spine of
the TDD: each component, **what's decided**, what's undesigned"* — present tense — and the table's
own convention is to stay current by APPENDING dated pass markers, never by freezing a cell: item
14 already updated this same table's MCP-client row when its state changed, and this item corrects
the Tools row on exactly that reading. Treating the Security-guardrails row alone as a frozen
historical record would leave the TDD's deny-list understating what the floor denies — the very
prose-behind-the-code defect this item removes. Item 10's NOTES' reading ("true of that pass")
is accurate about sentence attribution but not about the row's job. The addition is one dated
sentence in the existing hardening-sentence style, after the SEC-02 note; per item 10's ratified
outcome it says **wholesale, not decoded** for the `/48` (no decode is claimed) and that only the
well-known `/96` keeps its RFC 6052 decode. §4 is NOT implicated — the contract says nothing about
the floor's range list — so `confinement-execution-contract.md` is untouched. Deviations: (1)
`client.go`'s stdio parenthetical also changed "no URL floor" → "no URL check" — with the endpoint
now exempt from the floor, the old contrast word restated a blanket floor over HTTP that no longer
exists; same sentence, one word. (2) `git.go`'s corrected claim spans the comment's lines 21–27
(the old ":25" clause was mid-sentence; correcting it required rewording the sentence around it).
CHANGELOG deliberately omitted: doc/comment-only, no behaviour. Verifier caveat: the acceptance's
`grep -n "deliberately included"` self-matches its OWN quoted search string at plan line ~1322;
no dated `NOTES` line matches, which is what the check is for.

**Provenance:** item 1's own `NOTES` (which deliberately left `docs/design/technical-design.md:196`
alone, because that item's acceptance forbade touching a second file under `docs/design/`), item
10's verifier (`technical-design.md:200`) and item 14's landing (`internal/mcp/client.go:44`). This
item is **doc and comment only — no behaviour change**, and it must change no test.

**What:** four stale claims in shipped files, plus an inverted word in this plan's own record that
has already been corrected.

- **`docs/design/technical-design.md:196`** (the *Tools (21)* row) still says of `diagnostics`:
  *"`ReadOnly()` (runs in Plan) yet carries the `domain.SubprocessTool` marker (vet shells out) —
  read-only wins in the disposition, so it runs freely and is never confined/gated (same shape as
  `git_diff_range`)"*. Item 1 made every clause of that false: `classifyTool` now consults the
  unfakeable markers **before** `IsReadOnly`, so `git_diff_range` and `diagnostics` take §4's
  **subproc** row — confined in Auto, gated when fs-confinement is unavailable, **refused** in Plan.
  Both are still *offered* in Plan's menu (the filter reads `ReadOnly()`), which §4's new footnote ²
  records; the corrected text has to say both halves or it swaps one wrong claim for another.
- **`internal/tools/git.go:25`** — *"git_diff_range is ReadOnly() so it runs freely"* — same cause.
- **`internal/tools/registry.go:63`** — *"(git_diff_range is read-only and runs freely)"* — same
  cause.
- **`internal/mcp/client.go:43-44`** — `Connect`'s doc comment still says the guard carries *"the
  default-on SSRF floor applied to the HTTP transports"*. Item 14 exempted the **configured
  endpoint** from the pre-flight floor and replaced the blanket dial control with the endpoint-aware
  pin, so the floor is no longer blanket over those transports. **One corrected clause**: the
  comment already defers to `doc.go`, and `internal/mcp/doc.go:22-35` is already correct — do not
  reword it, and do not restate its whole trust boundary here.
- **Already applied on 2026-07-26 — NOTHING LEFT TO DO HERE, recorded so the record is complete:**
  items **4, 5, 6 and 7**'s dated `NOTES` in this file each said the CHANGELOG was deliberately
  *"included"* where every one of them meant *"omitted"* (commits `a6e52ce`, `5cb0003`, `a0988a7`
  and `657041e` touch no CHANGELOG). All four were corrected in place, one word each, with
  **nothing else** in any dated `NOTES` line changed. This bullet is history, not work.

**ADJUDICATE — `docs/design/technical-design.md:200`.** The two verifiers read this line opposite
ways and the item must settle it **before** editing anything:

- item 10's verifier flagged the *Security guardrails* row as **stale**: it describes the SSRF
  deny-list as the P3.11 / SEC-01 pass landed it, naming only the NAT64 **well-known** prefix
  `64:ff9b::/96` as *"decoded and re-checked"*, and says nothing of the wholesale denies item 10
  added (`64:ff9b:1::/48`, `2002::/16`, `::/96`, `fec0::/10`);
- item 10's own `NOTES` took the opposite reading and left it alone deliberately — *"that cell is a
  historical record of what the P3.11 / SEC-01 pass landed, and it remains true of that pass"*;
- item 14's verifier read the same line as making **no MCP claim** and therefore unchanged by item
  14. That is consistent with both readings — item 14's MCP correction landed in the **MCP-client**
  row, not this one — so it settles nothing and must not be mistaken for a third vote.

Decide it on the row's own terms: does §5's status table read as *what is decided today* (⇒ line 200
gains the four new denies, in the existing P3-hardening sentence's style) or as *a dated record of
each phase's landing* (⇒ leave it, and say so in this item's `NOTES` so the question is closed
rather than re-raised a third time)? Either way the **wholesale-deny** wording is load-bearing: item
10 denies the local-use `/48` **outright and does not decode it**, so any text added must not claim
a decode.

**Fix:** correct the four claims above (two prose clauses in `technical-design.md`'s Tools row and
the two Go doc comments) and apply the adjudicated outcome for line 200. The
`included` → `omitted` corrections in this plan file are **already done** — do not redo them. **§4
(`docs/design/confinement-execution-contract.md`) is item 1's and stays shut** — its RO amendment
and footnote ² already say the right thing. Reopen it **only** if the adjudication concludes §4
itself is wrong, and that is a stop-and-ask, not an in-item edit.

**Authoritative source:** ADR 0012 **Amendment (2026-07-25)(a)** and contract **§4** as item 1
amended it (for the three read-only claims); ADR 0012 **Amendment (2026-07-26)** as item 14 added
it, plus `internal/mcp/doc.go:22-35` (for the MCP clause). The **code is right in every case** —
only the prose is behind it, which is what makes this doc-only.

**Tests:** none, deliberately. This item changes no behaviour and must change no test. If a test has
to move, that is the signal the item has strayed out of doc/comment scope — stop and ask.

**Acceptance — a fresh verifier runs:**
```
gofmt -l . && go vet ./... && go test ./... && go test -race ./...
git diff --stat
```
plus: `git diff` touches only doc files and Go **comment** lines (no statement changes, no test
file); `grep -n "runs freely" internal/tools/git.go internal/tools/registry.go docs/design/technical-design.md`
returns nothing about `git_diff_range` or `diagnostics`;
`grep -n "SSRF floor applied to the HTTP transports" internal/mcp/client.go` returns nothing;
`grep -n "deliberately included" "docs/plans/2026-07-26 - 03 - url-safety-live-gap-plan.md"` returns
nothing; `git diff -- docs/design/confinement-execution-contract.md` is **empty** unless the
adjudication licensed otherwise; and the adjudication's verdict and reasoning are written into this
item's dated `NOTES`.

Commit: `docs(tools,mcp): correct the read-only and SSRF-floor claims the url-safety fixes made stale`

---

## 16. Item 13's asymmetric twin — `http_request` renders the whole header block raw and uncapped — ✅ DONE (2026-07-26)

NOTES (2026-07-26): The extracted core is `neuterInert(raw, capBytes, label)` in `web_fetch.go`
beside `redirectTarget` (the item names no file for it); it keeps the cut-and-mark inside — the
marker is `[<label> truncated at N bytes]` — so `redirectTarget`'s output stays byte-identical
with `label="location"`. Caps chosen (the item fixes none): per rendered name/value 4096 bytes
(`maxResponseHeaderValueBytes`, the request side's mirror), whole block 64 KiB
(`maxResponseHeaderBlockBytes`); block marker `[header block truncated at 65536 bytes]`, already-
rendered lines kept. One edge beyond the literal text: a header NAME that neuters away to nothing
drops its line (there is no line to hang a value on; unreachable through net/http, which refuses
such names). **Mutation check (performed by implementer):** re-rendering the raw `resp.header[k]`
value sent `TestHTTPRequest_HostileResponseHeadersAreRenderedInert` and
`TestHTTPRequest_OversizedHeaderBlockIsCappedAndMarked` red while
`TestHTTPRequest_PlainHeadersRenderUnchanged` stayed green; restored, all green.

**Provenance:** flagged by **both** item 13's implementer and its verifier as the twin of the shape
item 13 fixed for `web_fetch`; **no item in this plan owned it**. Item 13's own `NOTES` names the
surface it rests on: the response header block sits **outside** `maxNetworkResponseBytes` (net/http
accepts a 10 MiB one by default), and a header value is server-chosen text lifted out of the body
and into the block the model reads as fact.

**What:** `renderRequestResult` (`internal/tools/http_request.go:174-195`) writes the status line and
then **every** response header, sorted, verbatim —
`fmt.Fprintf(&b, "%s: %s\n", k, strings.Join(resp.header[k], ", "))`. Two consequences, the same two
item 13 closed for `Location`:

- **The 2 MiB body cap is bypassed.** `maxNetworkResponseBytes` (`internal/tools/network.go:42`)
  bounds only `resp.body`; the transport's own header allowance is 10 MiB, so a hostile server that
  answers a one-byte body with a 9 MiB header block hands the model 9 MiB the cap exists to refuse.
  The **request** side is already bounded (`maxRequestHeaders = 32`,
  `maxRequestHeaderValueBytes = 4096`, `http_request.go:50,54`); the response side is not.
- **Hostile header values reach the model unneutered.** Bidi overrides, zero-width characters and
  other Cf/Co/Cs runes, and CRLF- or obs-fold-folded text all survive `net/http`'s parsing — item 13
  established both halves by experiment, not by assumption — and land in a block the model reads as
  the server's own facts, where a folded fake status line can pass for one.

Item 13's *What* named `http_request` as the tool that *"never had the gap"*. That was true of the
one header it was about, and is exactly why nobody looked at the other forty.

**Fix:** apply item 13's treatment here. `redirectTarget` (`internal/tools/web_fetch.go:135-168`) is
unexported in the **same package**, so the machinery already exists: extract its rune filter +
whitespace fold + cut-and-mark core into an unexported helper over a bare string (leaving
`redirectTarget` as the 3xx-gated wrapper that calls it), and run every rendered header **name and
value** through it — the directive-inert shape `library.SanitizeContent`
(`internal/library/store.go:335`) uses, which is the shape item 13 mirrored. Cap the rendered header
block **as a whole** as well as per value, and **mark** a cut block rather than dropping it silently
(item 13's rule: a truncated value must be visibly truncated, never a silent stub). **Redact
nothing** — as in item 13, a response header is response *content*, and the funnel's M2 rule is
about failure messages naming the **request** URL. `internal/tools/network.go` is untouched: this is
a renderer change, and no `CheckRedirect` or cap constant moves.

**Authoritative source:** the cap the funnel states at `internal/tools/network.go:42`, and
web_fetch's own reasoning at `internal/tools/web_fetch.go:79-84` — *"the response HEADER block is
outside `maxNetworkResponseBytes` … so an unbounded value would be a way around the body cap"* —
which is a claim about the **transport**, not about one header. No disposition moves, so ADR 0012
and §4 are not in question.

**Tests** (`internal/tools/network_test.go`, beside the `TestHTTPRequest_` family at line 313):
- a hostile-header case mirroring `TestWebFetch_HostileRedirectLocationIsRenderedInert`
  (`network_test.go:210`) — a bidi override, a zero-width character, and a folded value carrying a
  fake status line — asserting the rendered block carries none of them and that nothing in it opens
  a line of its own;
- an oversized-header-block case: the rendered result is **bounded** and carries the cut marker
  (assert a byte count on the rendered block, the assertion item 13 used, not on the body);
- a plain-header negative control: ordinary values render **unchanged** — this must not mangle
  `Content-Type: text/html; charset=utf-8` or a normal `Date`;
- `TestWebFetch_RendersRedirectLocation` (line 146) and
  `TestWebFetch_HostileRedirectLocationIsRenderedInert` (line 210) stay green byte for byte — the
  extraction of `redirectTarget`'s core must be behaviour-preserving.

**Acceptance — a fresh verifier runs:**
```
go test ./internal/tools/... -run 'TestHTTPRequest_|TestWebFetch_' -v
go test ./... && go test -race ./...
```
plus the **mutation check** this plan's regression items established: render the raw
`resp.header[k]` value again, confirm the hostile and oversize cases go **red** and the plain-header
control stays green, restore, confirm green — record both observations in the item's `NOTES`. And:
`grep -n "resp.header\[k\]" internal/tools/http_request.go` shows the value passing through the
neutering helper, `git diff -- internal/tools/network.go` is **empty**.

Commit: `fix(tools): render http_request's response headers inert and capped`

---

## 17. Item 2's rung, a different vector — the Windows opener argv is command-injectable — ✅ DONE (2026-07-26)

NOTES (2026-07-26): **Shape (b) taken, deliberately over the item's "best first" (a):** the OS
table keeps `cmd /c start ""` and the windows case refuses, via the new unexported `cmdSafe`, any
path carrying a character cmd.exe can read as grammar, degrading through the existing
`ErrNoOpener`. (a) was rejected on its own merits, not skipped: `rundll32
url.dll,FileProtocolHandler` receives its tail as ONE raw string through an undocumented parser —
with the space-carrying paths `EscapeArg` quotes (the suite's canonical path has a space), its
quoted-path behaviour cannot be verified on any harness this repo has (the Windows opener ships
unexercised, the item's own words), and rundll32 exits 0 either way, which silences the
fail-visible half `launchDetached` exists for; the `SysProcAttr.CmdLine` sub-shape is Windows-only
compilation that the argv seam every opener test pins through cannot see. Two departures from the
item's literal text, both additive: (1) the refused set is wider than the item's named
`& | ^ < > %` — also `"` (EscapeArg emits `\"`, which cmd's parser does not honour, so the two
disagree where the quoted region ends), `!` (live under machine-wide delayed expansion, a registry
key not this process's choice), the token delimiters `; , =` (an unquoted path holding one splits
into two `start` arguments, and start resolves its FIRST argument like a command name, PATHEXT and
all), and ASCII control characters; a space and parentheses deliberately stay open-able, each
pinned by a row. (2) One extra test, `TestOpenerCommandOverrideIsNotNameBounded`, pins the item's
rung-3 bound exactly the way item 2's `TestOpenerCommandOverrideIsNotExtensionBounded` pins the
extension twin. ADR 0019 gained its second dated 2026-07-26 amendment (why-now + (a) bound /
(b) space-and-parens / (c) other rungs untouched), and a CHANGELOG Security entry sits beside item
2's. `openerRenderableExts` and `overrideArgv` are untouched, as the acceptance requires.

**Provenance:** raised by item 2's verifier as *worth its own item before Windows ships*. It is the
**same rung** item 2 bounded and the allow-list does not close it, which is why it could not be
folded into item 2.

**What:** `internal/present/opener.go:140-141` returns `[]string{"cmd", "/c", "start", "", path}` on
Windows, and `launchDetached` (`opener.go`, the `exec.Command(name, args...)` at the top of the
function) runs it through a plain `exec.Command`. Go builds a Windows command line by joining the
arguments and quoting one **only when it contains a space or a quote** (`syscall.EscapeArg`);
`cmd.exe` then re-parses that line, where `&`, `|`, `^`, `<`, `>` and `%` are **syntax**. So a
model-written file named `report&calc&.html`, in a workspace path that happens to contain no space,
produces the line `cmd /c start "" C:\ws\report&calc&.html` — which cmd.exe splits into three
commands and runs `calc`. **Item 2's allow-list does not reach it:** the extension is `.html`,
squarely inside `OpenerRenderable`, and the injection rides the **rest of the name**. Windows ships
unexercised today — `opener.go:124` says so in as many words — which is what makes this a *before it
ships* item rather than a live exposure, and what makes it cheap to fix now.

**Fix:** make the Windows rung immune to cmd.exe's grammar rather than trying to sanitise names for
it. Shapes, best first: **(a)** drop `cmd /c start` and hand the path to a launcher that takes an
**argv** rather than a command line — `rundll32 url.dll,FileProtocolHandler <path>`, or a
`SysProcAttr.CmdLine` this package builds itself with the path quoted and `^`-escaped the way *cmd*
(not `EscapeArg`) requires; **(b)** keep `cmd /c start` and refuse any name carrying a cmd
metacharacter, degrading exactly as a refused extension does. Whichever is chosen, three bounds hold:
it must **not weaken item 2's allow-list** (rung 1 stays extension-bounded, ADR 0019 Amendment
(a)/(b) stand); it must **not** extend the bound to rung 3 (amendment (c) — a `present.command`
template is the user's own configuration, with the standing of their shell); and a refusal must
**degrade to the baseline transcript rung**, never surface as an error (amendment (d), §4's degrade)
— reuse the existing `ErrNoOpener`, since a new sentinel would be a new exported symbol and that is
a stop-and-ask.

**Authoritative source:** **ADR 0019** and its **Amendment (2026-07-26)**, whose own words are that
the model chooses the file's **name** and whose (a)–(d) fix what this rung may and may not do; ADR
0012's invariant that an unattended call has a bounded blast radius is what makes an injected `calc`
a defect rather than a curiosity.

**Tests** (`internal/present/opener_test.go`):
- extend `TestOpenerBuildsThePlatformCommand` (line 53) with `GOOS: "windows"` rows over hostile
  names — `report&calc&.html`, `a|b.html`, `x^y.html`, `%TEMP%.html`, plus a name with a space and
  one with a quote — asserting that what is produced cannot be reparsed by cmd.exe into a second
  command, or (shape (b)) that **no argv is produced at all**;
- `TestOpenerRenderableAllowsDocumentsAndRefusesPrograms` (line 188) and
  `TestOpenerCommandOverrideIsNotExtensionBounded` (line 221) stay green — this item touches neither
  the allow-list nor rung 3, and those two tests are what proves it;
- a ladder row in `TestPresenterLadderPicksRung` (`internal/tui/presenter_test.go:117`) mirroring
  item 2's `.bat` row: a hostile **name** on a windows desktop degrades to the transcript rung with
  no launch.

**Acceptance — a fresh verifier runs:**
```
go test ./internal/present/... -run 'TestOpener|TestLaunchDetached' -v
go test ./internal/tui/... -run 'TestPresenterLadderPicksRung' -v
go test ./... && go test -race ./...
GOOS=windows go build ./... && GOOS=darwin go build ./...
```
plus: `grep -n "cmd\|CmdLine\|rundll32" internal/present/opener.go` shows the chosen shape;
`git diff -- internal/present/opener.go` leaves `openerRenderableExts` and `overrideArgv`
**untouched**; and if ADR 0019 needed a clause, the amendment is dated and names the rung it bounds.

Commit: `fix(present): stop a model-chosen file name from injecting a command on Windows`

---

## 18. Item 9's verifier — align the string the funnel scrubs against with the string it dials — ✅ DONE (2026-07-26)

NOTES (2026-07-26): **First offered shape taken** (pass the normalised string; `redactRequestURL`
unchanged): all five `blockedMessage`/`scrubURLError` calls in `do` now receive `target` — which
already falls back to `req.url` when `NormalizeURL` fails — so the string scrubbed is the string
dialled; `grep -n "req.url" internal/tools/network.go` shows only `safeHost`'s label (kept
un-normalised per this item) and the normalisation input. This closes a **latent** divergence —
**no live leak was fixed**: every error reaching those sites today is a `*url.Error` whose URL is
dropped wholesale before redaction. The new table row (a non-`*url.Error` embedding the normalised
form, produced by an injected resolver, plus a `normalized` assertion column) is a **guardrail,
not a regression proof**; it was confirmed to FAIL against the pre-fix call sites.

**Provenance:** raised by item 9's verifier. **Hardening only** — it is *not reachable today*, and
the item's `NOTES` must say so rather than claim a live leak was fixed.

**What:** item 8 made `do` normalise once and build the request from `target`
(`internal/tools/network.go:160-162` and `:210`), but every failure path still scrubs against the
**raw** `req.url`: `network.go:185`, `:212`, `:226`, `:229` and `:243` all hand `req.url` to
`blockedMessage` / `scrubURLError`. `redactRequestURL` (`network.go:396-399`) → `redactSubstring`
(`network.go:411-421`)
strips the string it is **given**, so an error text carrying the *normalised* form but not the raw
one would ride out unredacted. It is unreachable today for exactly the reason the verifier gave:
every error that reaches those lines is a `*url.Error`, and `scrubURLError`'s `errors.As` branch
rebuilds the message from `ue.Op` plus the cause, dropping the URL before redaction is needed at
all. It becomes reachable the moment normalisation diverges further from the raw string **and** a
non-`*url.Error` embeds `target` in its own text — the same check-one-string / use-another shape
item 8 removed from the guard/dial seam and left standing on the message seam.

**Fix:** make the two one string. Pass the **normalised** `target` — falling back to `req.url` when
`NormalizeURL` fails, which is the string the request would carry in that case anyway — to every
`blockedMessage` / `scrubURLError` call in `do`; **or**, the belt-and-braces shape, have
`redactRequestURL` strip **both** forms, the way item 9 made `redactSubstring` strip both the raw
and the `strconv.Quote`d form. Nothing about the M2 wording moves: messages still name the bare host
and nothing else, and `safeHost`'s label (`network.go:152`) stays **un-normalised** for item 8's
stated reason — it feeds no request and no guard call, so it creates no divergence, and touching it
would move message wording.

**Authoritative source:** the funnel's M2 discipline, stated in the file at
`internal/tools/network.go:141-144` — *"a key-bearing request URL can never ride out to the
model"* — and item 8's own rule that the funnel must not hold two strings that can disagree.

**Tests** (`internal/tools/network_funnel_test.go`):
- extend `TestNetworkFunnel_DoBlockedURLDoesNotLeakKey` (line 533, table-driven since item 9) with a
  row whose **normalised** form differs from the raw one (an upper-case host, a trailing dot, or
  leading whitespace) and whose failure is produced by a **non-`*url.Error`**, asserting through the
  existing `assertNoKeyInAnyForm` helper that the key appears in neither form;
- every existing `TestNetworkFunnel_` case stays green — in particular
  `TestNetworkFunnel_DoTrailingDotDoesNotEscapeTheDenyList` (line 445) and
  `TestNetworkFunnel_DoDialsTheURLTheGuardChecked` (line 410), which pin the two halves of item 8
  and must not be disturbed by a message-path change.

**Acceptance — a fresh verifier runs:**
```
go test ./internal/tools/... -run 'TestNetworkFunnel_' -v
go test -race ./internal/tools/... -run 'TestNetworkFunnel_' -count=2
go test ./... && go test -race ./...
```
plus: `grep -n "req.url" internal/tools/network.go` shows no message path building on the
un-normalised string (or shows `redactRequestURL` being given both forms), and the item's `NOTES`
records plainly that this closes a **latent** divergence — no live leak was fixed, and the new row
is a guardrail, not a regression proof.

Commit: `fix(tools): redact the funnel's messages against the URL it actually dialled`

---

## 19. DEFERRED — hoist a per-tool transport and consolidate the two HTTP client builders

**Status: DEFERRED by the owner on 2026-07-26 to an architecture pass. It is NOT next and must not
be dispatched with items 15–18 and 20.** It is written down so the verifier's reasoning and its
counterweight are not lost, and because it is the natural home for the *Out of scope* section's
client-builder deepening candidate once that pass happens. **Nothing below is authorised to land
from this plan.**

**What:** item 11 fixed the per-call transport **leak** with `defer client.CloseIdleConnections()`
in `do` — the first of the two alternatives its own text authorised — but the funnel still builds a
fresh `*http.Transport` on every call (`newHTTPClient`, `internal/tools/network.go:294`) and
therefore still pays a **fresh TCP+TLS handshake per call**, with network tools auto-running
unattended in Auto and dozens of calls in a Turn the normal case. Item 11's verifier judged the
**hoisted per-`networkTool` transport** the better shape and rebutted the reason item 11 gave for
declining it: `networkTool` is **unexported** (`network.go:63-65`) and all **seven** construction
sites are inside `internal/tools` (`web_fetch.go:45`, `http_request.go:89`, `web_search.go:82`,
`:84`, `:95`, plus `network_funnel_test.go:39` and `network_test.go:623`), so a non-optional
transport field behind a constructor cannot be missed by an outside caller; and the claimed
`http.DefaultTransport` fallback risk **is not real**, because item 4's dial-time test
(`TestNetworkFunnel_DialTimeFloorBlocksAfterPreflightPasses`) goes red the moment a lost
`SafeDialControl` lets a connect through — that is precisely the mutation item 4 performed. Item 14
left the second half standing: `internal/mcp/transport.go:219` (`newGuardedHTTPClient`) still
reproduces the funnel's builder **field for field**, only the dial control differing, and its doc
comment now records that the seam is a deepening candidate.

**Fix (for the pass that eventually takes this on — NOT for this plan):** give `networkTool` a
**non-optional** `transport *http.Transport` field populated by a constructor all seven sites call,
build the client per call around that one transport with only `Client.Timeout` varying, and drop
`defer client.CloseIdleConnections()` at the same moment (retaining the pool is the whole point, so
leaving both in place would cancel the change). Then, and only then, fold
`internal/mcp/transport.go`'s `newGuardedHTTPClient` and the funnel's `newHTTPClient` into one
builder taking the dial control as a parameter — the seam this plan's *Out of scope* section rules
belongs to `/improve-codebase-architecture`.

**Counterweight — record it, do not lose it (item 11's verifier).** A **pooled** connection skips
the **dial-time control re-check** on later calls: the control fires per *connect*, not per request.
The pre-flight still runs on every call, so this is **not a regression** against today's guarantees
— but it is a marginally wider TOCTOU window between the pre-flight and a reused connection, and the
architecture pass must weigh it against the handshake cost rather than assume the hoist is free. Any
hoist must also keep `Client.Timeout` **per call** (item 12's shared budget) and leave `Control`,
`CheckRedirect` and `clampDuration` byte-identical.

**Authoritative source:** nothing new — ADR 0012 Amendment (2026-07-25)(d) keeps the floor where it
is, and this plan's *Out of scope* section already rules that consolidating the two client builders
belongs to an `/improve-codebase-architecture` pass and not here. That standing ruling is why this
is **deferred rather than dropped**.

**Tests:** for whichever pass eventually takes this on, not for this plan — the transport is the
**same pointer** across calls on one tool; per-call timeouts still vary
(`TestNetworkFunnel_TimeoutResolution`); item 4's dial-time test stays meaningful; and item 11's
`TestNetworkFunnel_DoReleasesTheConnectionItOpened` (line 178) changes shape if the pool is now
deliberately **retained** — that change is itself a design call, not a test edit.

**Acceptance:** none — this item is not to be executed from this plan. When the architecture pass
picks it up it inherits this plan's per-item green gate and its tighten-only rule.

Commit: none from this plan.

---

## 20. Item 2's residual risk — reconcile the renderable-extension set with the rule it is documented by — ✅ DONE (2026-07-26)

NOTES (2026-07-26): In-item adjudication — the SET moved, the rule stands. Dropped `.doc`/`.xls`/
`.ppt` from `openerRenderableExts` (40 → 37 entries): the rule's own line is `.docx`-vs-`.docm`,
and the pre-2007 binary formats, having no macro-free variant, sit on the `.docm` side of it —
their handler runs document-carried macros on a single Enable-Content click; narrowing costs a
legacy deliverable only the degrade to rung 0 (path still shown, result `shown`, no error), while
rewording would have softened a defensible stated rule into accepted-risk prose, against the
plan's tighten-only boundary. `.csv` ruled explicitly IN: plain text with no container for code,
the residual spreadsheet formula/DDE surface is handler-specific and behind that application's own
security prompts (DDE default-off/prompted since the 2017 mitigations), and `.csv` is a
first-class coding-agent deliverable, which `.doc`/`.ppt` are not. The decision is stated in all
three places: the rule comment on `openerRenderableExts`, a THIRD dated ADR 0019 amendment
(appended in the house style rather than editing amendment (a)/(b) in place — the first amendment
stays the record of what item 2 shipped), and the existing `[Unreleased]` CHANGELOG bullet's
exclusion clause (amended in place, not a new bullet — same unreleased entry). Item 2's NOTES
corrected: it shipped 40 extensions, not 39. Test rows added on both sides
(`TestOpenerRenderableAllowsDocumentsAndRefusesPrograms`); no count assertion; the rung-2 subset
test and `TestPresenterLadderPicksRung`'s `.bat` row are untouched and green.

**Provenance:** logged by item 2's verifier as **residual risk, not a defect** — there is no auto-run
of a macro under default Office / LibreOffice macro security. It is one *"Enable Content"* click from
execution, and `.csv` → Excel **DDE** is the same shape, which is why it is written down rather than
waved through.

**What:** the shipped rung-1 set and its stated rule do not agree, in three ways.

- **Count.** `openerRenderableExts` (`internal/present/opener.go`, the `var` block that follows the
  doc comment at `:158-171`) holds **40** entries; item 2's own `NOTES` in this file says *"39
  extensions"*, and ADR 0019's amendment gives no number. The set is the fact and the `NOTES` is the
  record, so the record is what is wrong.
- **Rule vs. set.** The rule is stated at `internal/present/opener.go:158-166`: an extension earns a
  place *"only when its default handler DISPLAYS the file"*, *"which is what excludes scripts,
  installers, shortcuts and the macro-enabled office formats (.docm/.xlsm/.pptm)"*. The set
  nonetheless admits the **macro-capable legacy** formats `.doc`, `.xls` and `.ppt`, whose handlers
  will run macros carried in the very document the model wrote — the distinction the rule draws is
  `.docx` vs `.docm`, and the pre-2007 formats sit on the wrong side of it. ADR 0019 **Amendment
  (2026-07-26)(a)/(b)** states the same rule in prose (*"the formats whose default handler
  **displays** the file rather than executing it"*) without naming the exclusion, so the ADR is
  where whichever decision is taken has to end up being said.
- **`.csv`.** The same shape by a different mechanism — a `.csv` opened by Excel is a DDE /
  formula-injection surface — and, unlike `.doc`, a format a coding agent's deliverables genuinely
  come in. It needs a ruling, not just a mention.

**Fix:** decide **which side moves** and move exactly that one. **Either** drop `.doc`, `.xls` and
`.ppt` from `openerRenderableExts` (and rule explicitly on `.csv`), **or** reword the rule at
`opener.go:158-166` *and* ADR 0019's amendment so it says what the set actually is — display-by-
default, macros not auto-run, one user click away, an accepted and stated risk. **Do not do half of
each**; the whole point is that the set and its stated rule agree afterwards. The user-facing edge
of the narrowing direction is cheap: a legacy deliverable degrades to the transcript rung, no error,
the path still presented (amendment (a)). Rung 2's subset invariant
(`TestBrowserRenderableIsASubsetOfTheOpenerSet`, `internal/tui/presenter_test.go:345`) must survive
either way, and rung 3 stays unbounded (amendment (c)).

**Authoritative source:** **ADR 0019 Amendment (2026-07-26)(a)** and **(b)** — the allow-list's rule
and its deliberately-wider-than-rung-2 shape — under ADR 0012's bounded-blast-radius invariant. Note
the boundary this plan sets: **widening** what may be presented is a loosening and out of scope by
the *Out of scope* section's own rule, so only the narrowing direction, or a documentation change
that admits the set exactly as it stands, is available here.

**Tests** (`internal/present/opener_test.go`):
- `TestOpenerRenderableAllowsDocumentsAndRefusesPrograms` (line 188) gains rows for whichever way
  the decision goes — `.doc`, `.xls`, `.ppt` and `.csv` asserted on the side they land on, with
  `.docm`, `.xlsm` and `.pptm` still **refused** either way;
- a bare count assertion is **not** wanted: the corrected `NOTES` records the number, and a
  magic-number test would go stale on the next legitimate addition;
- `TestBrowserRenderableIsASubsetOfTheOpenerSet` (`internal/tui/presenter_test.go:345`) stays green,
  and `TestPresenterLadderPicksRung`'s `.bat` row is untouched.

**Acceptance — a fresh verifier runs:**
```
go test ./internal/present/... -run 'TestOpener' -v
go test ./internal/tui/... -run 'TestBrowserRenderableIsASubsetOfTheOpenerSet|TestPresenterLadderPicksRung' -v
go test ./... && go test -race ./...
```
plus: the set and **both** statements of its rule (`internal/present/opener.go:158-166` and ADR
0019's amendment) are read side by side and shown to agree; the item's `NOTES` records **which side
moved and why**; and item 2's *"39 extensions"* in this plan file is corrected to the real count in
the same commit.

Commit: `fix(present): make the renderable-extension set and its stated rule agree`

---

## Whole-plan verification (run after the last item lands, before declaring done)

1. **Full gate green**, plus `go test -race ./...` twice in a row (items 3, 4, 11 and 12 all touch
   the funnel's concurrency-adjacent paths).
2. **Tighten-only proof.** Diff every ladder/class change across the whole plan and confirm no cell
   moved from `gate`/`confine`/`refuse` toward `run` in any mode, and that
   `confine-to-workspace: false` still returns before every class switch.
3. **§4 has exactly one owner.** `git log -p -- docs/design/confinement-execution-contract.md` for
   this plan's commits shows **only item 1's** commit touching it.
4. **The normalisation runway is clear.** Items 8, 9 and 10 are landed and green **before** anyone
   opens the parked config key at `TODO.md:285`; note in the closeout that the key is now a smaller
   change.
5. **Every deviation is recorded.** Each item that departed from its text carries a dated
   `NOTES (YYYY-MM-DD): …` line under its heading.
6. **CHANGELOG.md** carries the fixes; **CONTEXT.md** is checked for prose that item 1's
   classification change makes stale (the vouching clause and the Confinement / Safety-guardrails
   entries).
7. Archive the source audit (`2026-07-26 - 00 - url-safety-live-gap-audit.md`, from `docs/reviews/`
   into `docs/reviews/archived/`) once every finding is landed, accepted or re-parked with its
   disposition written into the report — `docs/reviews/archived/`'s rule is that nothing in it is
   anyone's to-do list. Then archive **this** plan under `docs/plans/archived/`.
   Commit: `docs(plans): archive the url-safety live-gap plan`.

## Manual verification (owner — the automated suite cannot do this)

- **The three design calls** (items 1, 2, 14) are answered by the owner before those items are
  dispatched.
- **A live Auto run** after item 1 confirming `git_diff_range` and `diagnostics` behave as decided
  (confined or gated) and that Plan mode's menu is what the owner intended.
- **Item 14's startup check** against a real private MCP endpoint on the host
  (`http://192.168.64.1:7331/mcp`).
- **Item 2 on a real desktop**: confirm a non-renderable extension degrades to the transcript rung
  rather than launching a handler, on at least the owner's primary OS.
