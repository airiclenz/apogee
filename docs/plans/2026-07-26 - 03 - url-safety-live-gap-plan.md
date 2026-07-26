# Plan — Close the live url-safety gap (the 2026-07-26 audit's findings)

**Date:** 2026-07-26
**Status:** READY *with three open design calls* — items **1 (H-1)**, **2 (H-2)** and **14 (M-10)**
carry a `**Design call:**` line. The executing coordinator **stops and asks the owner** before
dispatching those three; the other eleven items are pre-decided by the audit and need no
escalation.
**Source:** `docs/reviews/2026-07-26 - 00 - url-safety-live-gap-audit.md` — the `/code-audit` on
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
(documents, images, text — 39 extensions), with `internal/tui`'s `browserRenderableExts` left
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

## 4. H-4 — pin the dial-time SSRF floor through the funnel's own client

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

## 5. M-7 — pin that the host-supplied `URLGuard` reaches all three network tools

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

## 6. M-8 — pin the fail-closed decision for an erroring Approver

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

## 7. M-9 — adversarial tests for the floor's fail-closed paths and the numeric-encoding family

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

## 8. M-1 — normalise the URL once, so the guard checks what the transport dials

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

## 9. M-2 — stop `%q` escaping from defeating the URL redaction

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

## 10. M-3 — decode the RFC 8215 NAT64 local-use prefix in the SSRF floor

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

## 11. M-4 — stop leaking a transport, an idle socket and two goroutines per network call

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

## 12. M-5 — put the pre-flight DNS resolution under the same timeout budget

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

## 13. M-6 — show the model where a refused redirect points

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

## 14. M-10 — the SSRF floor over a user-typed MCP endpoint

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

## Whole-plan verification (run after item 14, before declaring done)

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
7. Archive the source audit (`docs/reviews/2026-07-26 - 00 - url-safety-live-gap-audit.md` →
   `docs/reviews/archived/`) once every finding is landed, accepted or re-parked with its
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
