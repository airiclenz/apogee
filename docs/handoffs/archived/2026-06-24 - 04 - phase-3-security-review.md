# Phase-3 `/security-review` checkpoint report

**Date:** 2026-06-24 · **Reviewer:** adversarial security pass (Opus 4.8, fresh context) ·
**Scope:** the P3.2–P3.15 guardrails + confinement + dispatch + network/MCP/sub-agent/config surface ·
**Branch:** `main` · **Verdict:** review-only, no code changed.

## Executive summary

Overall posture is **strong**. The blast-radius disposition, the tighten-only guard ordering
(`PreExecute` first, can only make a call stricter), the fail-closed confinement re-exec
(`maybeDispatchConfinedExec` → non-zero exit on any error; `runSubprocess` propagates
`ErrConfinementUnavailable` rather than running unconfined), the default-on resolved-IP SSRF floor
with a dial-time re-check, and the sub-agent privilege model (`ForSubAgent` isolates live state +
shares the read-only floor; depth bound enforced in two places) are all sound and match ADR 0012 /
the confinement-execution-contract. The `confine-to-workspace` loosening lever is correctly
**file-only** (no flag/env), so a hostile repo's invocation environment cannot loosen Auto's blast
radius. No path was found where a tool that the disposition says MUST be confined runs unconfined
instead of routing to Approval, and no project-config or env seam can dissolve a floor (because the
project-config merge path is **not wired at all** — see L1).

Counts: **Critical 0 · High 1 · Med 3 · Low 4.** The one High is a symlink-swap TOCTOU in the
in-process write family that a confined-but-write-capable subprocess can race. Everything else is a
hardening/observability gap or an intended-design exfiltration surface worth recording before v1.0.0.

---

## Findings

### High

**[HIGH] H1 — Symlink-swap TOCTOU in the in-process write family lets a confined subprocess redirect a workspace write outside the fence**
`internal/security/pathsafety.go:28-46` (`ResolveInRoot`) + `internal/tools/write_file.go:86-95`,
`internal/tools/find_replace.go:102-121`, `internal/tools/file_edit.go:83-112`.

- Trust boundary: in Auto with `confine-to-workspace=true`, a `terminal`/`python_exec` subprocess is
  landlock/seatbelt-confined but **may freely write *inside* the workspace**, including creating
  symlinks (`LANDLOCK_ACCESS_FS_MAKE_SYM` is in `landlockFSWriteAccess` and is re-granted beneath the
  workspace root). The in-process write tools (`write_file` / `single|multi_find_and_replace` /
  `edit_existing_file`) are `classWorkspaceWrite` and auto-run in Auto when the target classifies
  in-workspace, then call `ResolveInRoot` and `os.WriteFile`.
- Attack: `ResolveInRoot` resolves symlinks at **check** time, but the subsequent
  `os.MkdirAll`/`os.WriteFile` re-walk the path at **use** time with no `O_NOFOLLOW` and no fd-pinning.
  A confined subprocess running concurrently (or a prior subprocess turn) swaps an intermediate
  workspace component to a symlink pointing outside the workspace **after** the check passes — e.g.
  create `<ws>/build` as a real dir, pass the check, then `ln -sf /home/user/.ssh <ws>/build`; the
  next `write_file <ws>/build/authorized_keys` re-resolves through the now-symlinked `build` and
  lands in `~/.ssh`. The single-shot non-existing-target case
  (`EvalRealPath` climbs to nearest existing ancestor) widens the window: the leaf and any
  not-yet-created ancestors are never re-checked at write time.
- Note on the simpler variant: a symlink that **already exists at check time** is correctly caught
  (`EvalSymlinks` follows it and the prefix test rejects the outside target). The hole is specifically
  the check/use race and the unchecked not-yet-created tail.
- Fix: resolve once to a real, containment-checked **directory fd** and write relative to it with
  `openat(..., O_NOFOLLOW|O_CREAT)` (or `os.OpenFile` on the verified absolute path with `O_NOFOLLOW`
  on the final component plus an `openat`-style ancestor walk). At minimum, re-stat the final
  resolved path's `Lstat` immediately before write and reject if any component is a symlink; better,
  pin the workspace root fd at construction and resolve every write beneath it with `openat2(...,
  RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS)` on Linux. This closes the only adversary (the confined
  subprocess) that can win the race.

### Medium

**[MED] M1 — Audit log is constructed but never threaded to where snapshots/observers can read it; “unaudited slip” is not provable from the trail consumers**
`internal/agent/loop.go:75` (`guards: security.NewDefaultGuards()`), `internal/security/audit.go`.

- The dispatch path records correctly: refusals, denials, force-approvals, circuit trips, and executed
  calls all call `RecordBlocked`/`RecordExecution` with the right `AuditDecision`
  (`dispatch.go:80,96,116,139,315`). So the in-memory trail is complete **per the code paths I could
  trace**. The gap is that nothing exports or persists `a.guards.Audit` — no `Records()`/`Dropped()`
  consumer exists outside tests, and the audit log is per-Agent live state that is dropped on Close
  with no flush. Consequence: a denied/gated/dangerous action is recorded only in a volatile ring that
  no observer or snapshot reads, so the audit trail cannot actually be inspected after the fact — the
  observability spine the package doc claims is not wired to an observer.
- Also: the sub-agent gets a **fresh** audit log (`ForSubAgent`), and its trail is never merged back
  into the parent. A sub-agent's denied/dangerous calls therefore vanish entirely when the child Agent
  is discarded after `runSubAgent`. Only the parent-facing *result text* survives.
- Fix: thread `Audit.Records()` into the session snapshot (or an EventSink audit event) so the trail is
  persisted/observable, and fold a sub-agent's audit records (or at least its `Dropped()` + a summary)
  back into the parent trail before discarding the child.

**[MED] M2 — `web_search` appends the query to a config'd endpoint that may carry a secret API key; the SSRF floor is checked on the *constructed* URL but the key rides in the URL and into model-facing results on redirect-less error paths**
`internal/tools/web_search.go:81-108`, `buildSearchURL:113-122`.

- The endpoint is host-config (`web-search-endpoint`, file-only) and "preserves any parameters the
  endpoint already carries (e.g. an API key)" — so the key is in `reqURL`. That URL is only ever used
  for the request (good), but note two smaller exposures: (a) the URLGuard error on a blocked endpoint
  is returned verbatim to the model — `"search endpoint blocked by url-safety: "+err.Error()` — and the
  wrapped error embeds the resolved host but not the full query string, so the key is *not* currently
  leaked here; (b) there is no `AllowHosts` pin on the search endpoint, so if the configured endpoint
  is later changed to a host that 30x-redirects, the redirect is *not* followed (CheckRedirect returns
  `ErrUseLastResponse`) — good. Net: this is a latent exposure, not a live leak. Record it so a future
  change that starts echoing `reqURL` (or that logs it) is flagged.
- Fix: keep the API-key-bearing query out of any model-facing or logged string; render only the bare
  endpoint host in errors. Consider a dedicated `search-allow-hosts` pin.

**[MED] M3 — `http_request` forwards arbitrary caller-supplied headers, including `Host`/`Authorization`/`Cookie`, to any allowed external host**
`internal/tools/http_request.go:102-104` (`for k,v := range args.Headers { req.Header.Set(k,v) }`).

- The model fully controls request headers. The SSRF floor bounds the *destination IP*, but a `Host:`
  header override does not change the dialed IP (Go sets `req.Host` from the URL, and `Header.Set("Host",
  …)` is ignored by the transport for routing) so virtual-host smuggling to an internal vhost behind a
  *public* IP is the residual risk: model fetches `https://public-cdn.example/` (passes floor) with
  `Host: internal-admin.corp` to hit an internal vhost colocated on that public IP. Also a model can
  attach a stolen `Authorization`/`Cookie` value it saw in earlier tool output to an attacker-named
  host (credential relay). This is bounded because the model is the only "attacker" and Auto auto-runs
  network — but it is exactly the kind of reach a prompt-injected model would attempt.
- Fix: drop hop-by-host-sensitive headers from caller input (`Host`, and consider gating
  `Authorization`/`Cookie`/`Proxy-*` behind an allowlist), or at least strip an explicit `Host` override
  so it cannot diverge from the dialed authority.

### Low

**[LOW] L1 — The project-config dangerous-rule merge (`MergeDangerousRules`, `projectAdd` tighten-only) is dead code: it is never called, so its security guarantee is untested in situ and the floor is currently fixed**
`internal/security/rules.go:118-164`; only caller is `rules_test.go`. `guards` is always
`security.NewDefaultGuards()` (`loop.go:75`), which uses the fixed `DefaultDangerousRules()`. This is
*safe today* (no config can loosen what no config can touch), but the tighten-only invariant the report
brief asks about lives only in unexercised code. When this is wired (P3.x), re-verify the
project/global split end-to-end. No action required now beyond noting that the "project cannot loosen
the floor" property is currently true by absence, not by the merge logic.

**[LOW] L2 — Dangerous-action guard normalizes only whitespace+case; trivially evadable, and the force-approval `sudo`/`curl|bash` tier is a speed-bump, not a control**
`internal/security/dangerous.go:178-180`, `rules.go:81,88`. By design (ADR 0012: "this is not the
adversary game"), but worth stating the limit explicitly: `rm -r -f /` with a comment, `r""m -rf /`,
`$(printf rm) -rf /`, base64-decoded payloads, or writing the destructive command into a file and
`sh file` all bypass the regexes. The floor catches an *honest small-model mistake*, not a
prompt-injected adversary. Acceptable given the Confiner is the real boundary — but ensure no doc or UI
ever describes this guard as a security boundary (ADR 0012 already insists on this).

**[LOW] L3 — Confined subprocess can read any file on the host and the network is open by default → secret exfiltration is in-design**
`internal/platform/landlock_linux.go:75-85` (only WRITE accesses handled; read/exec unrestricted) +
`networkDenyDecision` (empty `NetworkAllow` ⇒ network open). A confined Auto subprocess can
`cat ~/.ssh/id_rsa` / cloud creds and POST them to the internet, or write them into the workspace
where an in-process tool surfaces them. This is **intended** per ADR 0012 (the box bounds *writes*,
network is open by default, `confine=false` is the only blanket loosen). Recording it so it is a
conscious v1.0.0 acceptance, not a surprise: the fs fence stops *clobbering* the host, not
*reading/exfiltrating* from it. If read-confinement or default-deny egress is ever wanted, it is an
additive box tightening (landlock read-handling + a real per-host network filter).

**[LOW] L4 — stdio MCP server inherits the full parent environment (all process secrets) plus config'd `Env`**
`internal/mcp/transport.go:95-98` (`cmd.Env = append(cmd.Environ(), cfg.Env...)`). The launched MCP
server process sees every env var Apogee holds (API keys, tokens). This is a **trusted launch** (the
host chose the command in global config), so it is not a boundary violation, but it is broader than the
git tool's allowlisted env (`git.go:43-65`) and worth a deliberate decision. Fix if desired: scrub to
an allowlist for stdio MCP launches the way `safeGitEnv` does, or document that a configured stdio MCP
command is fully trusted with the process environment.

---

## No-issue areas reviewed (checked and cleared)

- **Guard ordering / tighten-only.** `PreExecute` runs the breaker then the dangerous guard *before*
  the disposition (`dispatch.go:77`); `GuardRefuse` short-circuits in every mode; `GuardForceApproval`
  upgrades any non-refuse disposition to a forced gate (`dispatch.go:108-110`); a forced gate ignores
  the allow-for-session cache (`approve` `!force && a.approved[...]`, `dispatch.go:182`). A nil Approver
  while a gate is required **refuses** (`dispatch.go:185-194`). No path loosens a call.
- **Confine-or-gate net, both at construction and runtime.** Construction refuses Auto only for a NIL
  Confiner; a present-but-incapable one enters Auto and the subprocess surface gates
  (`loop.go:61-68`, `disposeAuto` `classSubprocess` caps check). Runtime `ErrConfinementUnavailable`
  from `runSubprocess` demotes to forced Approval and re-runs only if the human allows
  (`dispatch.go:124-134,296-321`). The subprocess is never run unconfined when confinement was required
  and failed — verified in `exec_common.go:120-124` (error propagated, no fallthrough).
- **Re-exec fail-closed.** `maybeDispatchConfinedExec` exits non-zero on malformed argv / decode
  failure / landlock-apply failure (`confined_exec_linux.go:23-41`); `applyLandlock` re-probes ABI and
  errors if landlock vanished; `networkDenyDecision` fail-closes a requested net-deny on ABI<4
  (`landlock_linux.go:265-274`). Confine carries the resolved `cmd.Path` (not bare `Args[0]`) so the
  child's `syscall.Exec` (no PATH lookup) does not silently ENOENT into an unconfined state
  (`landlock_linux.go:160-166`).
- **SSRF floor.** Default-on (zero `URLGuard` has it on); only `DisableIPFloor()` (code-level, no config
  key) turns it off; `ipBlockedByFloor` covers loopback/IMDS/link-local/RFC-1918/ULA/unspecified and
  normalizes IPv4-mapped v6; pre-flight `resolveAndCheckFloor` blocks if **any** resolved IP is private;
  dial-time `SafeDialControl` re-validates the actual connected IP (DNS-rebinding closed) and is wired
  into every network client (`network.go:48-71`) and both HTTP MCP transports
  (`transport.go:144-159`); redirects are not auto-followed (`network.go:67`). IP-literal and
  decimal/hex/octal encodings reduce to the same `net.IP`.
- **Sub-agent privilege bound.** `ForSubAgent` shares the read-only Dangerous floor by pointer and
  gives a fresh breaker/audit (`guard.go:58-67`); the child inherits Mode/Confiner/ConfineToWorkspace
  verbatim (no loosen seam — `newChildAgent:106-117`); the tool set is a `Subset` of the parent's, so
  privilege expansion is structurally impossible (`subagent.go:130-146`); depth is bounded at 2 in
  **two** places (menu withholding + defensive refuse, `subagent.go:57-62,140`). A Tier-1 delegation
  task is refused by the floor on the `sub_agent` call itself (`dispatch.go:77` runs before the
  recursion branch at `:91`).
- **`confine-to-workspace` un-loosenable by env/flag.** Resolved from the FILE layer only
  (`config.go:85-89`, env/flag leave it nil); defaults true; a startup WARNING prints when Auto runs
  unconfined (`wire.go:95-99`).
- **Mode disposition table.** Plan refuses all non-read; Ask-Before/unknown-mode gates all
  write/exec/external; AllowEdits auto-approves only in-workspace own-writes; Auto routes per class with
  the caps check (`disposition.go:78-151`). `writeTargetInWorkspace` ok==false (undecodable args) treats
  as in-bounds but Execute re-enforces `ResolveInRoot`, so an out-of-workspace write still errors at
  Execute (it does not silently escape — separate from the H1 race, which is about *concurrent symlink
  swap*, not undecodable args).
- **MCP untrusted data is never executed.** `serverTool` description/schema/result are surfaced to the
  model verbatim and rendered as text (`tool.go:98-159`, `renderContent`); no eval, no command
  construction. `<server>__<tool>` qualification prevents collision; empty/duplicate server names
  rejected pre-connect; Connect is all-or-nothing with rollback; connect failure is fatal at the host
  (`wire.go:106-109`).
- **Circuit-breaker.** Hashed (tool, args) signature; trips at 3 identical consecutive failures;
  success clears; trip reported once; checked before execute so a tripped sig short-circuits. Bounded
  key. No DoS via unbounded map growth beyond distinct signatures (acceptable for a single session).
- **Audit ring bounded.** 10k cap, oldest-evicted, `Dropped()` counter, 4 KiB per-result cap — memory
  bounded, eviction observable (`audit.go`). (The *consumer* gap is M1, not the ring itself.)
- **git tools.** Scrubbed allowlisted env (`safeGitEnv`); `validRef` regex rejects option/metachar
  smuggling in diff refs; `--` separators before paths; path-scoped file args via `resolveInRoot`;
  protected-branch delete block; amend-on-published block; argv (not shell), so no shell-metachar
  injection. (The H1 symlink race applies to git's staged paths too, but git's own `add --` resolves
  to absolute checked paths, narrowing it.)
- **terminal / python_exec.** `sh -c <model-string>` / interpreter-on-stdin is the *intended* full-shell
  surface — the control is confine-or-gate, not arg-escaping, which is correct for a terminal tool.
  Output capped (256 KiB), timeout-bounded (max 600 s), process-group teardown (`Setpgid` + negative-PID
  kill + `WaitDelay`) reaps children. No temp file for python (stdin). Bypass mode does **not** disable
  the security guards (it only toggles Mechanisms — `config.go:21`, `mechanism.go:113-120`), so the
  dangerous floor + breaker + disposition stay on under Bypass.
- **Read tools (read_file/list_dir/grep).** Path-scoped to the workspace root via `resolveInRoot`
  (symlink-following) — a read cannot escape via an existing symlink. (Confined-subprocess read-anywhere
  is L3, an orthogonal in-design surface, not these in-process read tools.)

## Owner/CI-only (cannot verify on this host: landlock-off, no macOS)

- **Live landlock enforcement.** `confinetest.Probe` skips when `FSWrite==false`; this dev host reports
  no enforceable landlock, so the OS-denial battery (write-outside-box denied, `~/.ssh` denied, domain
  inherited across `execve`, parent-stays-unrestricted, network-deny connect denied) did **not** run
  here. Must be run on a kernel ≥5.13 (fs) / ≥6.7 (net) CI runner. The H1 symlink race in particular
  should get a dedicated enforcement test: confined subprocess creates an in-workspace symlink to
  outside, then an in-process `write_file` through it — assert the write is rejected after the fix.
- **macOS seatbelt enforcement.** `seatbeltProfile` is unit-tested hermetically, but the real
  `sandbox-exec -p <profile>` denial behaviour (file-write fence + `(deny network*)` when
  `NetworkAllow` non-empty) needs a macOS runner. Verify the TinyScheme `(subpath …)` quoting holds for
  workspace paths containing spaces/quotes (`seatbeltQuote`) under a real profiler.
- **`__confined-exec` re-exec round-trip** end-to-end on a landlock host (the dispatch happens before
  Cobra; confirm a real subprocess tool call in Auto re-execs the product binary and the box decodes).
- **DNS-rebinding dial-time block against a real rebinding resolver** (the unit tests inject a
  deterministic resolver; confirm `SafeDialControl` fires on a genuinely rebinding name on a networked
  runner).

---

## Fix-pass disposition (remediation, 2026-06-24)

Outcome of the fix pass. Each finding was re-confirmed against the cited code before any change;
every fix carries a regression test that fails before / passes after. All guard floors stayed
**tighten-only** (no fix introduced a new way to loosen a floor); no change contradicts ADR 0012.

### High

- **[H1] Symlink-swap TOCTOU — FIXED.** Confirmed real (and a hermetic test proved a plain
  `os.WriteFile` through a symlinked workspace component leaks outside the fence). The write family
  (`write_file`, `single|multi_find_and_replace`, `edit_existing_file`) now performs every read and
  write through an `os.Root` pinned at the workspace root (Go 1.26 stdlib): the path that is
  validated *is* the path that is operated on (no re-resolution gap), and an escaping-symlink
  component — including one swapped in concurrently — is **refused** ("path escapes from parent"),
  not followed. New `internal/security/safeio.go` (`SafeWriteFile`/`SafeReadFile`); tools call them
  via `internal/tools/path_safety.go`. `os.Root` is stdlib, so all 6 cross-builds stay green (no
  build tags needed). Tests: `internal/security/safeio_test.go` (swapped-intermediate, final-symlink,
  read-side, traversal, positive controls). The fence stays tighten-only — the helpers refuse
  strictly more than the old string-path I/O.

### Medium

- **[M1] Audit trail not threaded to an observer — FIXED.** Added `domain.AuditEvent` and emit it
  from dispatch wherever an audit record is appended (`recordExecuted`/`recordBlocked` in
  `internal/agent/dispatch.go`), so the trail is now observable on the `EventSink`, not only in the
  volatile ring. The sub-agent half is closed for free: a child Agent shares the parent's `EventSink`
  and emits at `Depth>0`, so a delegated call's audit record reaches the same observer instead of
  vanishing with the discarded child (it no longer needs folding back into the parent ring — it is
  streamed live with its nesting depth). Tests: `internal/agent/audit_event_test.go` (executed,
  refused, and sub-agent-at-depth cases).

- **[M2] `web_search` API-key in model-facing error — FIXED (assessed MORE severe than the review).**
  The review judged this "latent, not a live leak." On the transport-error path it **was a live
  leak**: `client.Do`'s `*url.Error` stringifies the full request URL (host + query + the config'd
  API key), and the old code returned `err.Error()` verbatim to the model (proven by a repro test:
  the key appeared in the result). Fixed: every model-facing error now renders only the bare endpoint
  **host**, and any transport error is URL-scrubbed (`scrubURLError`/`endpointHost` in
  `internal/tools/web_search.go`). Tests: `internal/tools/web_search_redaction_test.go`
  (transport-error + floor-block, asserting the key never appears, the host does).

- **[M3] `http_request` forwards `Host`/hop-by-hop headers — FIXED.** A caller-supplied `Host` (and
  the hop-by-hop / framing family `Content-Length`/`Transfer-Encoding`/`Connection`/`Proxy-*`/…) is
  now rejected as a result error before the request goes out, with a header count + per-value size
  cap (`applyRequestHeaders`/`deniedRequestHeaders` in `internal/tools/http_request.go`). Tighten-only
  (it only removes a model's reach). Tests in `internal/tools/network_test.go`
  (`TestHTTPRequest_RejectsDeniedHeaders`/`_HeaderCountCapped`/`_HeaderValueCapped`). *(This fix, the
  SSRF-floor v4/NAT64 hardening, and the git option-injection guard were already present as
  in-progress uncommitted working-tree work when the fix pass began; verified real, complete, and
  green, and folded into this remediation commit.)*

### Low

- **[L1] `MergeDangerousRules` dead code — DEFERRED (TODO.md).** Floor is fixed-by-absence today;
  re-verify when the config merge is wired. Nothing to change now.
- **[L2] Dangerous-action guard trivially evadable — NO ACTION (already satisfied / by-design).**
  ADR 0012 "not the adversary game"; `internal/security/doc.go` already states the guard is "NOT a
  security boundary" and no doc/UI describes it as one — exactly what L2 asks. Not a false positive,
  just already-met.
- **[L3] Confined subprocess read-anywhere + open egress — DEFERRED (TODO.md), INTENDED per ADR 0012.**
  Conscious v1.0.0 acceptance; read-confinement / default-deny egress would be an additive box
  tightening.
- **[L4] stdio MCP inherits full parent env — FIXED (documented trust) + enhancement DEFERRED
  (TODO.md).** Made the full-environment trust explicit in `internal/mcp/transport.go` (a blanket
  scrub would break MCP servers needing inherited PATH/HOME/runtime); an optional per-server
  env-allowlist scrub is parked for a less-trusted stdio server.
