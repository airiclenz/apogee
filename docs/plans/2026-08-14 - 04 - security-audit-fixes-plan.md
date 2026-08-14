# Security-audit fixes — S-1, SP-2, DS-1, RE-1/RE-2, PL-1

- **Goal:** land the accepted fixes from the 2026-08-14 security audit: scrub configured
  `api-key-env` credentials from exec-tool subprocesses (S-1), refuse repo-local git filter
  drivers (SP-2), disclose the server-grain session grant on the MCP approval pane (DS-1),
  cap provider error-body reads (RE-1/RE-2), and correct the process-group teardown claim
  (PL-1).
- **Date:** 2026-08-14 · **Status:** TODO
- **Sized for:** ~200k-context host
- **Authoritative sources:** `docs/skill-runs/security-audit/2026-08-14/report.md` (the
  audit report; lives under the gitignored `docs/skill-runs/` — each item below restates
  its defect and is self-contained, so the report's absence never blocks an item; where
  item text and current code line numbers disagree, the named functions/symbols are the
  ground truth, not the line numbers), ADR 0012 (confinement policy), ADR 0047 (key
  sources), `internal/security/doc.go` (guard vs boundary),
  `docs/plans/private/archived/2026-08-11 - 06 - hostile-bytes-hardening-plan.md` item 3
  note (f) (why the scrub list held only `APOGEE_API_KEY`: at that time configured keys
  were file-only; `api-key-env` landed 2026-08-13 and reopened the surface).
- **Ratified design calls:**
  1. SP-2 refusal sits at the `runGit` choke point — every git tool refuses a repo whose
     repo-local config defines a filter driver, closing the read paths (clean filters run
     on plain `git diff`/`git status`) as well as the write paths (owner, 2026-08-14).
  2. SP-2 covers `filter.*.clean`, `filter.*.smudge`, and `filter.*.process`, sourced from
     repo-local configuration only (`.git/config` and, when present, the worktree config);
     drivers configured in the operator's global/system config never refuse (owner-trust
     boundary per `internal/tools/git.go` hardening comments) (owner, 2026-08-14).
  3. DS-1 wording is the grant-scope Note sentence:
     `Note: "Always allow" covers every tool of MCP server "<alias>" for this session`,
     with `this MCP server` replacing the quoted alias for the single unnamed server
     (owner, 2026-08-14, wording preview approved).
  4. S-1 scrub set is the union of every configured `api-key-env` variable name — all
     `Options.Servers[].APIKeyEnv` plus the top-level `Options.APIKeyEnv` — not just the
     active server's, because a session can switch servers mid-run (plan author,
     2026-08-14, from the audit remediation pointer).
  5. RE-1/RE-2 cap is 64 KiB via `io.LimitReader`; a truncated body flows through the
     existing sanitize/truncation path with no new error classification (plan author,
     2026-08-14).
  6. PL-1 is a documentation fix only — state the setsid-escape residual; the subreaper
     enforcement alternative was rejected because deliberately backgrounding a process is
     legitimate `terminal` use (owner, accepted in the 2026-08-14 audit review
     conversation).
- **Standing requirements:** skills: coding-standards. Any authorized deviation from item
  text lands as a dated NOTES line under the item.
- **Out of scope:** W-2 (workspace-skill steering — accepted as design; the workspace is
  the operator-chosen trust root, same stance as ADR 0026), DS-2 (CI action SHA-pinning —
  owner left it out of this wave), the stdio-MCP env scrub (ISSUES.md L4, deliberately
  deferred), any PL-1 enforcement change (subreaper/descendant tracking), any version
  identifier change.

## 1. Exec tools scrub caller-named secret env vars (tools side) — ✅ DONE (2026-08-14)

NOTES (2026-08-14): three test files outside the item's Files list — `exec_fence_test.go`,
`sub_agent_test.go`, `terminal_windows_test.go` — took the mechanical `nil` second argument at the
changed constructors, without which the package does not compile.
NOTES (2026-08-14): the python interpreter-version probe (`pythonVersionSpec` /
`interpreterVersion`) takes the same list, so the probe's environment stays identical to the
snippet's as its doc comment states; both signatures gained a `secretEnv []string` parameter.
NOTES (2026-08-14): `isApogeeSecretEnv` is kept unrenamed as the fixed half of the scrub; a new
sibling `isSecretEnv(entry, configured)` is the full predicate the env builders call.

**What:** `internal/tools/exec_common.go` scrubs only the literal `APOGEE_API_KEY`
(`apogeeSecretEnvVars`, `subprocessEnv`, `subprocessEnvScopedPath`, `isApogeeSecretEnv`) —
an operator-exported `api-key-env` variable is inherited by every `terminal` /
`python_exec` / `run_tests` subprocess, where a steered model can read and exfiltrate the
live bearer credential. Make the scrub extensible: add a `SecretEnvVars []string` field to
`domain.Config` (beside `DisabledTools` in `internal/domain/config.go`) and to
`tools.HostTools` (`internal/tools/registry.go`); thread it through
`DefaultToolsWithHost` into `NewTerminal`, `NewPythonExec`, and `NewRunTests` (constructor
signature + struct field on each); the scrub helpers drop the configured names with the
same case-insensitive comparison `isApogeeSecretEnv` uses today. `internal/tools` must
not import `internal/config` (the dependency points the other way), so the names arrive
as plain strings. Update the now-stale comment at `exec_common.go` ("The CONFIGURED
server keys need no entry here: they are file-only by design") to state the `api-key-env`
exception per ADR 0047. Wiring the names from configuration is item 2's job — this item
ends with the mechanism in place and exercised by tests, with an empty list behaving
exactly as today.

**Files:** `internal/domain/config.go`, `internal/tools/registry.go`,
`internal/tools/exec_common.go`, `internal/tools/terminal.go`,
`internal/tools/python_exec.go`, `internal/tools/run_tests.go`,
`internal/tools/exec_common_test.go`, `internal/tools/terminal_test.go`,
`internal/tools/python_exec_test.go`, `internal/tools/run_tests_test.go`

**Tests:** a configured name (mixed case) is absent from `subprocessEnv` output while
unrelated inherited vars survive; `APOGEE_API_KEY` stays scrubbed with an empty configured
list; each of the three exec tools passes its configured list through to the subprocess
env (via the existing argv/env test seams, e.g. `runPythonSubprocess`).

**Acceptance:** `go build ./... && go test ./internal/domain/ ./internal/tools/`

**Commit:** `feat(tools): exec subprocesses drop configured secret env names`

## 2. Wire configured api-key-env names into the scrub — ✅ DONE (2026-08-14)

NOTES (2026-08-14): `README.md` is edited outside the item's Files list — the `api-key-env:`
paragraph describes what a configured key variable is readable from, and this item is what makes it
unreadable to a model-chosen subprocess, so the user-facing sentence lands with the behaviour rather
than a wave later.
NOTES (2026-08-14): the item's construct-level test asserts on `hostTools(cfg).SecretEnvVars` (the
"registry's HostTools") rather than on the built registry — `internal/agent` cannot import
`internal/config`, so the "two servers naming distinct api-key-env vars" half of that case is
covered in `internal/config`'s own union test, and the tools' `secretEnv` field is unexported, which
leaves HostTools the only assertable seam on the agent side.

Depends on item 1.

**What:** source the names and deliver them to the tools. New exported helper in
`internal/config` (e.g. `APIKeyEnvNames`) returning the deduplicated union of
`Options.Servers[].APIKeyEnv` and the top-level `Options.APIKeyEnv` (ratified call 4),
empty-safe. Populate `domain.Config.SecretEnvVars` at both wiring sites that build the
tool registry — `internal/agent/construct.go` (`hostTools`) and
`cmd/apogee/wire_tools.go` (`registryWithMCP`) — from the config assembled in
`cmd/apogee/wire_boot.go` and `cmd/apogee/headless.go`, so the TUI and headless paths
cannot diverge.

**Files:** `internal/config/keyresolve.go` (or a sibling file in `internal/config`),
`internal/config/keyresolve_test.go`, `internal/agent/construct.go`,
`internal/agent/construct_test.go`, `cmd/apogee/wire_boot.go`, `cmd/apogee/wire_tools.go`,
`cmd/apogee/headless.go`

**Tests:** `APIKeyEnvNames` unions across entries, dedupes, drops empties; construct-level
test that a config with two servers naming distinct `api-key-env` vars yields both names
in the registry's `HostTools`.

**Acceptance:** `go build ./... && go test ./internal/config/ ./internal/agent/ ./cmd/apogee/`

**Commit:** `feat(config): configured api-key-env names reach the exec scrub`

## 3. runGit refuses repo-local filter drivers — ✅ DONE (2026-08-14)

NOTES (2026-08-14): the refusal is signalled in-band — `runGit` returns a synthesized failed
`subprocessResult` (exit 1, the refusal sentence as its output) — so all eight existing call
sites surface it through their present "git … failed" branch with no signature change and nothing
for a future git tool to forget; `runGitUnchecked` is the unprobed inner call, used only by
`runGit` and by the probe itself.
NOTES (2026-08-14): the probe costs two extra `git config` subprocesses per `runGit` call (the
`--local` and `--worktree` scopes), so the existing `TestGitBranch_RunsUnderConfine` assertion —
an existing test the item's Files list covers — moved from `want 1` to `want 3` Confine calls,
matching how `python_exec`'s version probe is already counted.

**What:** a checkout delivered with its own `.git/config` naming a
`filter.<driver>.clean/smudge/process` command executes that command as the calling user
on default git operations (`git add` runs clean; `git checkout` runs smudge; plain
`git diff`/`git status` run clean too) — the existing hardening
(`gitHardeningOptions`/`gitHardeningEnv`/`gitDiffHardeningArgs` in
`internal/tools/git.go`) closes hooks and diff drivers but not filters. Add a helper
beside `runGit` that lists repo-local filter-driver config by running git itself
(`git config --local --name-only --get-regexp` over
`^filter\..*\.(clean|smudge|process)$`, plus the `--worktree` scope when applicable,
through the same hardened env/argv path — config listing never executes a driver; a
non-zero "nothing matched" exit is the pass case). `runGit` refuses before running the
real command when any name matches, with a model-facing refusal in the style of the
existing fence refusals: it names the matching config key(s) and states the rule (repo-local
filter drivers are refused; global config is the operator's and still applies). Update the
hardening comment block (`git.go` ~lines 106-112) — the "git offers no global switch to
refuse configured filters" residual is now closed for repo-local config. Per ratified
calls 1–2 this guards every git tool, and only repo-local scopes.

**Files:** `internal/tools/git.go`, `internal/tools/git_test.go`

**Tests:** a repo whose `.git/config` defines `filter.x.clean` → `git_commit`,
`git_branch`, and a read tool (`git_diff_range` or `git_log`) all refuse, message naming
`filter.x`; a clean repo passes unchanged; a driver present only in global config
(injected via the test's HOME) does not refuse; `filter.*.process` and `.smudge` also
trigger.

**Acceptance:** `go build ./... && go test ./internal/tools/ -run 'TestGit'`

**Commit:** `feat(tools): git tools refuse repo-local filter drivers`

## 4. Approval pane discloses the MCP server-grain session grant — ✅ DONE (2026-08-14)

NOTES (2026-08-14): a FORCED gate discloses no grant even for an aliased MCP tool — `approve` blanks
the session key there, so an "allow for session" authorises that one call, and claiming the server
grain would over-state the yes. The item's text names only the non-`serverAliaser` degradation; this
adds the unrememberable-answer one, pinned by a `dispatch_test.go` sub-test.
NOTES (2026-08-14): `layout.md` is edited outside the item's Files list — it is the prose spec for
this pane and already documents each of its body lines (`Scope:`, `→ resolves to`, `Sub-agent:`), so
the new Note line lands with the behaviour rather than a wave later.

**What:** for MCP-class tools the allow-for-session grant is keyed at server grain
(`gateCacheKey`, `internal/agent/resolution.go` — `mcpServerCacheKeyPrefix +
sa.ServerAlias()`), but the approval pane (`internal/tui/approval.go`) shows only the one
call — a human approving one tool silently authorises every sibling tool of that server
for the session. Thread the fact to the pane: add `MCPServerGrant bool` and
`MCPServerAlias string` to `domain.ApprovalRequest` (`internal/domain/approval.go`, with
contract doc); in `internal/agent`, a small helper beside `classifyTool`/`serverAliaser`
(e.g. `mcpServerAlias(tool) (string, bool)`) reused by `gateCacheKey` and by a new
`(*Agent).grantScope`-style helper in `dispatch.go` (same pattern as `resolvedPath` /
`approvalScope`) that fills the new fields at the single `ApprovalRequest` construction
site. An MCP tool that does not implement `serverAliaser` degrades to a tool-grain key
today — those set `MCPServerGrant` false and get no note. In the TUI, append the ratified
Note line (call 3) to the pane parts, alias passed through
`flattenField(stripEscapes(...))` like the existing fields; empty alias renders the
unnamed-server variant. Do not derive the alias by string-prefixing `CacheKey` — the
forced-gate path blanks it.

**Files:** `internal/domain/approval.go`, `internal/agent/resolution.go`,
`internal/agent/dispatch.go`, `internal/agent/resolution_test.go`,
`internal/agent/dispatch_test.go`, `internal/tui/approval.go`,
`internal/tui/approval_test.go`

**Tests:** an MCP tool's ApprovalRequest carries grant=true + alias, a native tool's
carries false; pane snapshot shows the Note line for MCP (named and unnamed variants) and
omits it for native tools; a hostile alias with escapes/newlines renders flattened.

**Acceptance:** `go build ./... && go test ./internal/domain/ ./internal/agent/ ./internal/tui/`

**Commit:** `feat(tui): approval pane discloses MCP server-grain session grant`

## 5. Cap provider error-body reads

**What:** both non-2xx error paths buffer the whole upstream body before any cap:
`(*Client).statusDelta` (`internal/provider/stream.go`, `io.ReadAll(resp.Body)`) and
`(*Client).statusError` (`internal/provider/client.go`, same shape) — with default
timeouts of 0 a hostile upstream answering multi-GB error bodies OOMs the agent process.
Add a package const (e.g. `maxErrorBodyBytes = 64 << 10`, ratified call 5) and wrap both
reads in `io.LimitReader`. The `isContextOverflow` sniff and `sanitize` operate on the
capped bytes unchanged; no new error kind.

**Files:** `internal/provider/stream.go`, `internal/provider/client.go`,
`internal/provider/stream_test.go`, `internal/provider/client_test.go`

**Tests:** an error response whose body exceeds the cap yields a bounded, sanitized
message on both the streaming and unary paths (server writes > cap bytes; assert the
delta/error arrives and the read stopped at the cap); existing context-overflow detection
still fires when the marker sits within the cap.

**Acceptance:** `go test ./internal/provider/`

**Commit:** `fix(provider): cap error-body reads at 64 KiB`

## 6. State the setsid-escape residual in the teardown claims

**What:** the docs claim the process group "always holds the whole tree"
(`internal/tools/exec_pgroup_unix.go` — the `setProcessGroupTeardown` doc comment and the
`treeHeld` sentence in `killProcessGroup`; `internal/tools/exec_teardown.go` —
`planTreeKill` doc; `internal/tools/doc.go` package overview;
`docs/design/confinement-execution-contract.md` backend-obligation section). That is
false for a descendant that calls `setsid`/`setpgid(0,0)`: it leaves the group, escapes
both `cmd.Cancel` and the clean-exit reap, and outlives the call while the tool reports
the leader's exit 0 as success. Weaken each claim consistently: the group holds every
descendant that has not deliberately left it; a self-escaped descendant survives the call,
unsupervised but still inside any confinement write-fence. Note the platform asymmetry
where the contrast is already drawn: a Windows Job Object holds unless breakaway is
permitted, so the residual is POSIX-specific. Documentation only — no code change
(ratified call 6).

**Files:** `internal/tools/exec_pgroup_unix.go`, `internal/tools/exec_teardown.go`,
`internal/tools/doc.go`, `docs/design/confinement-execution-contract.md`

**Tests:** none (comment/doc change).

**Acceptance:** `go build ./internal/tools/ && ! grep -rn "always holds the whole tree" internal/tools/ docs/design/`

**Commit:** `docs(tools): state the setsid-escape residual in pgroup teardown claims`

## Suggested version bump

The plan closes one credential-exposure gap, adds two hardening refusals/disclosures, and
two robustness caps — plausibly worth one micro bump under the per-feature scheme once all
items land. Suggestion only; whether and when to bump is the owner's call.
