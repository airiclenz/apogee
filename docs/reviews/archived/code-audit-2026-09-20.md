# Code Review — apogee — 2026-09-20

**Scope:** 932 files across the whole repository — `.agents/`, `.beads/`, `.codex/`, `.github/`, `cmd/`, `graphics/`, `internal/` (all packages), `scripts/`, `Makefile`, `VERSION`, and repo-root Go sources. Independent verification: 71 candidate defects were checked against the code and repository state — 48 confirmed, 23 refuted (the refuted set included a claimed stale beads register, a config-splice truncation, two tuitest hang/race claims, a skills-export error, and a tuitest screen-divergence claim). Machine analysis (`go vet`, golangci-lint, actionlint, govulncheck, coverage) reported no tool-confirmed findings.
**Mission:** An open-source terminal AI coding agent (Go, Bubble Tea v2 TUI) for smaller, locally hosted LLMs — a full agentic tool-use loop over any OpenAI-compatible server or the Anthropic Messages API, with four autonomy modes (the last OS-confined), targeting a single user in a local workspace.
**Files reviewed:** 932

## Executive Summary

The single most serious cluster is memory-exhaustion in untrusted-input handling: a hostile PDF, MCP server, model server, or sessions-dir archive can OOM the agent process through unbounded decompression, unbounded reads, or unbounded accumulators — the process dies with no recovery. Second: the undo journal holds its mutex across filesystem writes, so a slow or wedged restore stalls the whole engine and a revert racing a sub-agent write can deadlock. Third: repo-shipped `.beads/hooks` execute as live git hooks after the documented hydration step (arbitrary local code execution from a cloned repo), and the write-escape approval seam's own CI gate is deterministically red. Otherwise the core is healthy: the dispatch/Approver/resolution folds, the confinement capability matrix, the seven Floor guards, and the event seam held up under verification, and a large share of the loudest candidate findings was refuted with evidence. A systemic theme cuts across the survivors: adversarial-bytes handling (sizes, counts, roles, argument bytes) is applied most places but missing at exactly the seams below.

## Intent & Architecture Findings

### High — a mid-session `git config` write of a command-valued key lands unrefused and runs on the next routine git call `[Security + Intent & Structure]`

- **Where:** `internal/gitexec/gitexec.go:336-339, 367` + `internal/security/shellwrites.go:443-462` (rule at `internal/security/rules.go:162-170`)
- **What:** The shell-write guard's `gitTargets` contributes only operand paths, so `git config core.hooksPath <dir>` / `git config filter.foo.clean …` write `.git/config` without naming it — no `\.git/(?:hooks|config|modules)\b` match, no refusal. The process-lifetime memoised command-config probe (`commandConfigProbes`) is never invalidated by the write, so the next git tool call (the read-only trio in every mode, or `git_commit`'s `add` firing a `filter.<name>.clean` on `.gitattributes`-marked files) executes the planted program unconfined on the operator's host. *(independently verified; the realistic, reliable execution route is `git_commit`'s `add` firing a repo-local `filter.<name>.clean`, still invisible inside the approved call)*
- **Why it matters:** The engine's own rationale names exactly this delivery — "a single in-workspace write into an existing .git/hooks/… the write is the realistic variant" (gitexec.go:84-98) — and the accepted-staleness decision (gitexec.go:336-339) contradicts the dangerous-action rule's own "delayed code execution outside any confinement" wording. Delayed code execution outside a fence is the bundle's in-scope class. The reviewer escalates the gitexec.go:336-339 comment and the ADR-0012 gate rationale it cites as **warrants revisiting**.
- **Fix:** Invalidate the memoised probe entry after any write-capable git call through this package (gitWrite's verbs and the terminal's `git config` operand class) so the next read re-probes and refuses; or, fail-closed, treat every unlisted git verb as writing `.git/config` in `gitTargets`.

## Critical & High Findings

### Critical — PDF extraction memory bombs kill the agent process `[Security + Correctness]`

- **Where:** `internal/doctext/pdf.go:160-223` (budget at :380-397), vendored `github.com/ledongthuc/pdf` (read.go:836-837, :228/:233/:301-305)
- **What:** Two independent unbounded-allocation vectors in one file. (1) The budget charges reads, not decompressed bytes: a ~1 MiB PDF with a ~1000:1 deflate stream is decompressed whole into memory per page (the clamp runs only after a page is fully rendered) — a 515:1 stream measured in this checkout ran unbounded at ~736 MiB TotalAlloc; Go's OOM kill bypasses the `recover` at pdf.go:166-169 and kills the whole agent. (2) The xref-stream path allocates before any budget exists: a crafted `/Size` read past a NUL byte (the `refuseAbsurdObjectCount` regex scan at pdf.go:434-444 misses it — verified: `/Size\x001000000` parses while the regex finds nothing) drives `make([]xref, size)` (~128 GB on 64-bit), and an attacker-controlled `W` array drives `make([]byte, wtotal)` (2 GiB from a ~200-byte file) — all inside `pdf.NewReader` (pdf.go:202), before the read budget exists. *(independently verified; both vectors confirmed; severity raised to Critical on measurement: deterministic process OOM from sub-10-MiB files — the unrecoverable "crash the agent" case the file's own header says is closed)*
- **Why it matters:** A malicious repo ships a crafted PDF; a model reads it via `read_file`/`@file` (10 MiB cap); the whole session dies.
- **Fix:** Bound decompressed bytes, not reads — a counting wrapper around the zlib reader refused at a budget, kept well under 10 MiB; scan the raw bytes with a lexer-faithful state machine that skips NULs/comments/whitespace; parse `/W` and bound each entry (≤ 8) and its sum; bound the xref table size by a sane constant rather than file bytes.

### Critical — the undo journal holds its mutex across filesystem writes `[Concurrency]`

- **Where:** `internal/undo/journal.go:419-435` (via `runStep` :361-389, `apply` :397-414, `snapshot.go:246-265`)
- **What:** The entire revert/redo step — filesystem writes through `SafeWriteFile`/`SafeRemove`, plus blocking git reads in snapshot-backed groups — runs while `j.mu` is held, the same lock the write funnel's `Record` and the engine's `MarkPre`/`Close` snapshots take. A wedged git or slow restore turns every concurrent `Record`/`BeginGroup`/`Save` into a whole-engine wait; a revert racing a sub-agent write deadlocks the engine's single mutex until the filesystem yields. *(independently verified)*
- **Why it matters:** `/undo` is a normal mid-session action; on a slow store it stalls (or deadlocks) the entire agent, and a sub-agent write racing it can wedge forever.
- **Fix:** Drop the lock before `runStep` — snapshot the group pointer + generation under the lock, then walk it lock-free (the journal is append-only while a revert runs, so the group is stable once popped). Candidate for `/improve-codebase-architecture`.

### Critical — repo-shipped `.beads/hooks/*` become live git hooks with the user's full privileges `[Security]`

- **Where:** `.beads/hooks/commit-msg:1-9`, `post-checkout:1-33`, `pre-push:1-33`, `prepare-commit-msg:1-73` (activated by `bd init` / `bd hooks install` pointing `core.hooksPath` at `.beads/hooks`)
- **What:** Committed, executable, repo-controlled shell. bd's marker handling preserves `commit-msg` (marker-free) and every byte of `prepare-commit-msg`'s unmarked tail verbatim; once a user or agent follows the documented hydration recipe, the next `git commit`/`checkout`/`push` runs the repo's shell with the user's full privileges. *(independently verified; the "merely cloned" phrasing slightly overstates — `bd init`/`hooks install` must run first, but that is the documented, expected flow)*
- **Why it matters:** A malicious or compromised repo (an in-scope attacker) ships its own hooks; the trigger is the documented hydration step plus any git command — arbitrary local code execution as the user.
- **Fix:** `bd hooks install` must only activate hooks it can vouch for (hash-pin the marked files, refuse or flag unmarked content and the marker-less `commit-msg`), or the repo must not ship live hooks at all.

### Critical — CI's own gate is deterministically red on the write-escape approval seam `[Critical-Path Tests]`

- **Where:** `.github/workflows/ci.yml` `go test -race -count=1 ./...` step; `internal/agent/writeescape_test.go:240, 299, 301, 318`
- **What:** `TestDispatchMintsTheWriteEscapePermit` fails 3 cells on a clean tree (independently reproduced): with `confineToWorkspace=false` an out-of-workspace write gates and never executes; a declared-writable path gates; a remembered allow-for-session consults the Approver twice (calls=2, want 1). The seam (agent.go/dispatch.go/loop.go) has been touched six times since the 2026-08-14 (`a1b95dde`) change. *(independently verified; severity kept at Critical — a permanently red CI gate blocks all merges to main, a regression per the project's own rule)*
- **Why it matters:** This is the approval gating of the out-of-workspace write path (ADR 0012 D9/D10). Every CI push runs this floor test and cannot pass.
- **Verification note (2026-09-21):** refuted as a shipped defect. CI on `main` is green at `b7fbf8c7` (runs 35595779986, 35537430336); the three cells fail only when `TMPDIR`/`GOTMPDIR` sits under `~/.apogee`, where every write is a forced approval floor (ADR 0049 §4, `internal/security/rules.go`) — the audit box's temp-dir placement, not the seam. The test now skips with that reason (`outsideDir`, `internal/agent/writeescape_test.go`). Action-order step 1 is closed by that hardening; no seam repair is owed.
- **Fix (original):** Reproduce the 3 failing cells and repair the seam (or revert `a1b95dde`/`64b8c9a6` if the regression traces to them) so all 9 cells hold. Note: the separate `GOTMPDIR`-under-`~/.apogee` environment collision that also fails the suite is not a shipped-behaviour defect.

### High — the rung-3 opener (`present.command`) runs a model-chosen program unapproved and unconfined in every mode `[Security]`

- **Where:** `cmd/apogee/wire_present.go:58-65`, `internal/present/opener.go:194-198, 387-412`, `internal/presenter.go:141-152`
- **What:** For a Local session with `auto-open` on, `present_document` is ReadOnly, so rung 3's `Opener.Open(req.Path)` runs in every mode including Plan with no gate. argv[0] is resolved by PATH at exec time with no writable-path fence (unlike rung 1's `security.ResolveProgram`), and `{path}` is a model-chosen path into the workspace. A hostile model can write a script under confinement then `present_document` it, and `sh {path}` / `python3 {path}` executes it as the user. *(independently verified; High stands — the `present:` block is global file-only config requiring a user-set, execution-capable override, which keeps this below Critical despite the Auto fence-escape class)*
- **Why it matters:** Arbitrary local code execution as the user, unapproved and unconfined — a fence escape in Auto (SECURITY.md's top class) and a read-only-boundary crossing in Plan. ADR 0019 §5's "user's own configuration" rationale predates wiring `present.command` into a model-triggerable tool with an arbitrary in-scope path argument.
- **Fix:** Resolve and fence rung 3's argv[0] through `security.ResolveProgram` exactly as rung 1 does, and gate the opener half of `present_document` through the disposition when an execution-capable opener rung is wired (classify the tool as an external effect, or run the opener inside the session's Confiner box).

### High — a forgeable confinement journal drives the victim's next Recover/Retire to NULL-SACL or label-write arbitrary paths `[Security]`

- **Where:** `internal/platform/winlabel/retire.go:210, :257` (`revertibleRoots`/`restorablePriors`), `walk_windows.go:400` (`revertJournal`), `session.go:221`
- **What:** Journal claims are read before `judgePriors` and are never guarded by a per-run secret; `prior_sddl` is attacker-chosen verbatim (only the path's *current* label is checked). Liveness is PID-only (`ProcessAlive(sibling.PID)`; a handoff drops `PID`, so the claim later reads as `pid == 0` → "dead"), and the persisted `RootJudged` flag skips `isVolumeRoot` entirely. A planted `labels-<pid>.json` under `~/.apogee/confinement` can make the next `NewConfiner`'s `Recover` NULL-SACL an arbitrary tree or `SetSDDL` an arbitrary descriptor. *(independently verified; severity Critical → High: the trigger needs same-user write into `~/.apogee/confinement` (a model in an unconfined mode, or a compromised local MCP server — in-scope per bundle), and the damage is integrity-label manipulation / DoS on paths already bearing apogee's Low label — no privilege boundary crossed)*
- **Why it matters:** The malicious/compromised-workspace attacker can relabel or strip labels on victim paths, and crash-recovery can silently destroy a foreign label the journal was built to preserve (the separate `priorRestorable` inversion: only a current Low label makes a foreign prior restorable, so any other label drops it permanently).
- **Fix:** Write a per-run secret into the journal and into the labelled root's own state (or ACL/HMAC) and verify it before any `SetSDDL`/clear; apply the `isVolumeRoot` refusal even to `RootJudged` entries (the flag should skip the label re-read, never the guardrail); pin liveness to a PID-collision-proof value (process creation time via GetProcessTimes).

### High — two confining sessions each spare the shared root, then strand the Low label on disk unrecoverably `[Concurrency]`

- **Where:** `internal/platform/winlabel/retire.go:218`, `walk_windows.go:445-447`, `journal.go:341-344`
- **What:** With two sessions confining one workspace, each `revertibleRoots` gates on the sibling's PID being alive, so each spares the shared root to the other and then deletes its own journal — both files gone while the Low label stays on disk. `Recover` and `ResidueIn` skip journals whose owner is alive, so the residue is unrecoverable and invisible. *(independently verified; severity Critical → High: the double-delete needs the overlapping-close interleave — a sequential teardown clears correctly and a crash between the two closes is healed by the surviving journal's later recovery; the stranded state is an unreported Low fence, not a boundary crossing)*
- **Why it matters:** Overlapping shutdowns of two sessions over one workspace (an anticipated state — the per-PID journal exists for concurrent processes) leave the workspace unwritable by Medium processes with no record and no surface that reports it.
- **Fix:** A root spared for aliveness must keep this journal's root entry alive until the sibling is gone — handed off like `restorablePriors` — and `retire` must not delete a file that still carries an undischarged root.

### High — a network-deny box does not fence UDP or pathname-UNIX-socket egress, and reports no Residual `[Security]`

- **Where:** `internal/platform/landlock_linux.go:298-311, :199-222`; `internal/platform/namespace_linux.go:237-238`
- **What:** Landlock handles only `LANDLOCK_ACCESS_NET_CONNECT_TCP`; unhandled rights are unrestricted, so a confined child keeps full UDP egress and pathname-UNIX-socket egress (`LANDLOCK_ACCESS_FS_RESOLVE_UNIX` is never handled). bwrap's `--unshare-net` cuts UDP and abstract sockets, but a pathname UNIX socket is filesystem-scoped and `/var/run/docker.sock`, `/run/user/<uid>/bus` stay reachable. Meanwhile `Capabilities()` reports `NetworkEgress: true` with empty `Residuals`. *(independently verified — the TCP-deny part is per-contract ("deny-all-TCP"); the silent UDP/UNIX gap with no Residual disclosure is a real capability-honesty violation)*
- **Why it matters:** A hostile model runs inside a net-deny Auto box and exfiltrates over DNS (UDP 53) or talks to local UNIX-socket services (docker, dbus) the user believed fenced — silent defeat of a control the user explicitly turned on, in SECURITY.md's in-scope "Auto-confinement escape" class, with no kernel error ever raised.
- **Fix:** Handle `LANDLOCK_ACCESS_FS_RESOLVE_UNIX` at ABI ≥ 9 and the UDP rights at ABI ≥ 10 when the box opts into deny; disclose whatever an ABI leaves open in `Capabilities().Residuals` exactly as the truncate gap is disclosed; for bwrap, disclose the residual and refuse the tightening or document it. Add a confinetest row driving a UDP send under a net-deny box.

### High — a Reaction reload/close can panic "send on closed channel" on the engine's Emit goroutine `[Correctness + Concurrency]`

- **Where:** `internal/reactions/runner.go:229` (`w.queue <- payload`) vs :363-379 (`close(w.queue)` in `drainSet`)
- **What:** `Emit` loads the active set lock-free (`r.active.Load()`) then sends onto each worker's queue; `Replace`/`Close` swap the set and spawn a drain that closes the queues. An atomic pointer swap gives no happens-before edge, so a firing that loaded the old set before the swap and reaches its send after the close panics on the engine's own goroutine under the tree-wide sink mutex — reproduced empirically even with a `default` drop branch. *(independently verified — triple-lens; severity kept High: unrecovered panic on the engine's goroutine)*
- **Why it matters:** Contradicts the package's own "Emit NEVER BLOCKS and never fails" contract and the doc.go invariant that a Reaction "can neither veto nor delay the loop". Realistic triggers: a `/settings` edit or the config watcher calling `Replace` mid-`Emit`, or `Close` during teardown — the window is widened by seam-closed firings that copy the whole conversation on the emit goroutine.
- **Fix:** Close queues while holding `swapMu` after swapping the active set (a queue is only closed once a later Emit can no longer hold its set), or give each `hookSet` an in-flight-Emit counter that `drainSet` waits on, or have the worker select on a per-set `stop` channel instead of ranging over a closed queue.

### High — an MCP server's response is read with no size cap end to end `[Security]`

- **Where:** `internal/mcp/tool.go:154-177` (`Execute → renderContent`), `internal/mcp/client.go:91-104` (`listServerTools`), with the go-sdk's unbounded `io.ReadAll`/`json.Decoder` readers
- **What:** A malicious or compromised server answers one `CallTool` (or `tools/list`) with a huge payload; the SDK decodes it unbounded, `Execute` flattens every content block verbatim into the model-facing result, and no downstream bound exists — the result is held whole for the Turn. Approval gates the call, not the size of its answer. *(independently verified — the memory-exhaustion half is real; the "stuff the model context" half is bounded by the structural floor, but the unbounded read happens before that clamp)*
- **Why it matters:** The hostile server the threat model names gets reliable in-process memory exhaustion and context stuffing in one call, in Ask-Before and Auto alike.
- **Fix:** Cap transport reads (`io.LimitReader` on the SDK readers, or post-decode size checks), cap flattened result content in `renderContent` with an explicit truncation marker, and bound tool-list pages and per-tool schema size.

### High — a hostile upstream streams unbounded open tool calls and OOMs the client `[Security + Concurrency]`

- **Where:** `internal/provider/stream.go:404, 426, 466` (`openToolCalls`), openai fold `wire_openai.go:324`, anthropic startBlock `wire_anthropic_stream.go:162, 211`
- **What:** `maxToolCallBytes` and `textBytes` cap only argument/content accumulators, never the call count: a stream of `delta.tool_calls` fragments with fresh indexes + empty arguments (or anthropic `content_block_start` tool_use events with distinct indexes) grows `entries` without ever growing the byte budget, and every chunk resets the idle timer so the 10-minute cut does not bound it. *(independently verified — a hostile upstream is an in-scope attacker, and the count is genuinely unbounded)*
- **Why it matters:** A malicious or compromised model server — the agent's own configured endpoint — can grow the process memory without bound and OOM it.
- **Fix:** Bound the number of concurrently open calls (a legitimate parallel reply holds a handful; cap ~32–64) or count each opened call against `maxToolCallBytes`, ending the stream with the same terminal `DeltaError` path the byte cap uses.

### High — a FIFO planted in the workspace wedges read_file and grep indefinitely `[Security]`

- **Where:** `internal/tools/path_read.go:105`, `internal/security/safeio.go:371-387` (SafeOpen → `os.Root.Open`)
- **What:** The read tools open their target with no `O_NONBLOCK` and no regular-file pre-check; `os.Root.Open` on a named pipe blocks forever waiting for a writer (verified empirically), and the size guard runs on the opened descriptor, after the blocking open. The read tools are ReadOnly — unapproved, run in every mode including Plan — so any ordinary read task against a FIFO the repo planted (a file renamed to a FIFO, a grep over the tree) wedges the loop with no read-side deadline. *(independently verified; severity kept High: a workspace file plants an indefinite, approval-free wedge of the whole loop; every read tool is exposed)*
- **Why it matters:** The hostile/prompt-injected repo gets a permanently wedged agent loop; the tool call never returns and the model waits forever.
- **Fix:** In `SafeOpen`, fstat the opened descriptor and refuse non-regular files (the check `openCopySource` already applies) so a FIFO surfaces the existing "not a file" refusal instead of blocking.

### High — model-supplied git pathspecs are glob-interpreted, staging or reading the wrong file `[Security]`

- **Where:** `internal/tools/git.go:1192-1200` (`workspacePathspec`), :565/:1173-1187 (`CommitPathspecs`); reads at `git_diff_range` :697-701 and `git_log` :1031-1037
- **What:** Bare pathspecs reach git without the `:(literal)` magic `git_stage.go:96-99` deliberately applies. With a workspace holding both `a[1]` and `a1`, `git commit` with `files: ["a[1]"]` runs `git add -- a[1]`, which matches `a1` and exits 0 — the commit lands with a file staged the operator never approved; the diff/log reads report a different file's history as success. *(independently verified)*
- **Why it matters:** Silent wrong result with exit 0 in the one git tool the engine's secrets pre-check scans — a model reaching for a bracket-containing filename stages or reads the wrong file with no error.
- **Fix:** Route the pathspec builders through the package's existing `literalPathspec` (`:(literal)`), as `git_stage.go` already does.

### High — session records decode with unbounded unmarshal before any size/version check `[Security]`

- **Where:** `internal/session/transcript.go:32` (`DecodeTranscript`), `internal/session/store.go:299-317` (`Load`/`LoadPath`)
- **What:** `DecodeTranscript` runs unbounded `json.Unmarshal` into an unbounded `[]Entry` before any version or size check, and `Load`/`List` read whole record files (decoding every `.json` in the directory at once). No `LimitReader` exists anywhere in the package. A multi-GB archive in the sessions dir, or a modest tampered blob packing a huge entry count, blows up memory/CPU on `--resume` or browser restore — `--resume <path>` explicitly supports "a repo-shipped session". *(independently verified)*
- **Why it matters:** The malicious-repo attacker gets unbounded memory and CPU use at resume/restore time, in the same class as the PDF and MCP findings.
- **Fix:** Cap the record size at read (`io.LimitReader` before `ReadFile`) and reject a blob whose decoded byte or entry count exceeds a sane bound before parse.

### High — Store.Rename is a stale read-modify-write that can lose a live save or the new title `[Correctness]`

- **Where:** `internal/session/store.go:510-524`, `internal/tui/sessions.go:512-514`
- **What:** `Rename` holds only the per-process mutex and takes no cross-process flock (unlike `Hold`/`Delete`/`Prune`); its Load→retitle→Save overwrites the whole record. A second instance renames while the first is mid-session: if the holder saved between B's read and write, B rolls that Turn back; the holder's next Save reverts the new title. *(independently verified)*
- **Why it matters:** The browser's rename is the one destructive door documented as refused that is not — the rename never sticks, or a live save is silently rolled back.
- **Fix:** Take the hold around Rename's read-modify-write (return `*HeldError` when another instance holds it), or write the new title with a title-only field write that cannot carry stale payload.

### High — a cached skill catalog can exhaust ~2 GiB at startup from a hostile repo `[Security]`

- **Where:** `internal/skills/load.go:442-452`
- **What:** Per-file reads are capped at 1 MiB and the catalog at 1024 skills, but nothing bounds the aggregate: 1024 near-1 MiB SKILL.md bodies are parsed via `yaml.Unmarshal` and retained in `Skill.Body` for the whole session (~2 GiB at the full cap), at startup, on every catalog reload, and via `runSkills` — a hostile repo's payload tree re-triggers it on demand. *(independently verified; severity Critical → High: the trigger needs a ~1 GiB attacker payload tree (1024 near-1 MiB files — not "trivially small"), and the impact is a local DoS (OOM) with no fence escape; most of the memory is heap the runtime may survive on a large machine)*
- **Why it matters:** A `git clone` the user then runs apogee in exhausts memory at startup; the reload path re-triggers on demand.
- **Fix:** Track total bytes read across the walk and cap it (e.g. 16–32 MiB), rejecting at `loadSkillFile`/`walkSkills` with a recorded skip, matching the existing soft-skip precedent.

### High — the streaming thinking-channel hold misses the pre-opened-template case and leaks reasoning as visible text `[Correctness]`

- **Where:** `internal/processing/thinking.go:92-99`, with `internal/agent/loop.go:874`
- **What:** `emitVisibleDelta` gates on `IsThinking`, which only looks for an unclosed opener (`strings.LastIndex`). A pre-opened template whose content starts mid-think (the minimax-m3 shape the file itself documents as "seen live") never reports mid-channel: `IsThinking` is false (no opener anywhere), `Strip` returns the reasoning region as Visible, and it is emitted as live TokenEvents — reasoning the stripper exists to hide scrolls by as visible answer text, then silently vanishes when the closer lands. *(independently verified)*
- **Why it matters:** Breaks the stated invariant ("reasoning the user should not see", thinking.go:12) and loop.go:809's own "never leaks that markup onto a live stream" promise — a correctness defect on the target small-model population, whose templates commonly pre-open the channel.
- **Fix:** Give `ThinkingConfig` a starts-mid-think flag (set by the pre-opened-template profile) and have `IsMidChannel`/`IsThinking` report true from the first content delta until the first closer completes.

## Medium Findings

### Medium — session-snapshot ingestion restores untrusted history as committed conversation `[Security]`

- **Where:** `internal/agent/state.go:157` (restoreState/decodeState)
- **What:** The version check and task-list caps cover shape only; `dropLeadingSystem` strips only a leading run of System messages. A crafted `"role":"assistant"` message reaches the model as genuine history with no fence, and a crafted `pendingInput` reaches it as the user's own next input. *(independently verified; severity High → Medium: the restored content reaches the model as history/next-input (prompt-injection escalation, in scope), but any tool call the model then makes still passes every normal guard/mode/Approver — the leak is into the model's context, not past the confinement/approval fence)*
- **Why it matters:** A hostile repo ships a crafted session export; the user restores it via Resume/RestoreSession; the model acts on injected instructions with the session's live tools and mode.
- **Fix:** On both ingestion paths treat the payload as hostile — validate each role, refuse or sanitise tool-result/tool-call content carrying fence lines or engine-note markers, bound message count and per-message size, refuse unattributable `pendingInput`; surface snapshot provenance before applying history.

### Medium — MCP stdio servers launch with the full inherited environment by default `[Security]`

- **Where:** `internal/mcp/transport.go:196-202` (`buildStdioTransport`)
- **What:** A configured stdio server runs with the full parent environment unless an `env-allowlist:` is set, so a malicious or compromised server holds every API key/token the process has. *(independently verified; severity High → Medium: the full-env default is a deliberate, documented trust decision — you chose the command — so this is a documented-posture gap vs SECURITY.md's blanket "keys are stripped from every tool subprocess" wording, and the disclosure only realises when the server is already compromised/malicious)*
- **Why it matters:** SECURITY.md's claim is overstated for the MCP lane; a default config hands the in-scope compromised-server attacker every credential.
- **Fix:** Make the scrubbed launch the default (platform floor, PATH fenced, per-server `Env` appended last) with full-env as the explicit opt-in — or narrow SECURITY.md's claim to exclude MCP.

## Recommended Action Order

1. **Fix the red CI gate first** (Critical, blocks everything): repair the write-escape approval seam so `TestDispatchMintsTheWriteEscapePermit` passes — every other fix must land through that gate.
2. **Close the memory-bomb class** (PDF, MCP, provider stream, session decode, skills aggregate): bound decompressed bytes, transport reads, open-call counts, record sizes at the read seam. These are the "process dies with no recovery" findings and share one design pattern — an adversarial-bytes budget at every untrusted seam.
3. **Kill the delayed/unapproved execution vectors**: fence `.beads/hooks` activation (hash-pin or refuse), resolve+fence `present.command` rung 3, invalidate the git command-config probe after writes / fail-closed on `git config`.
4. **Fix the undo-journal lock scope** (needs design discussion; candidate for `/improve-codebase-architecture`): snapshot the group under the lock, walk it lock-free.
5. **Fence the winlabel journal** with a per-run secret and PID-collision-proof liveness; hand off spared roots instead of deleting them; disclose the landlock/bwrap UNIX-socket and UDP gaps in `Capabilities().Residuals`.
6. **Remove the fanOut closed-channel panic** in the reactions Runner (close queues under `swapMu` or use a per-set stop channel) — an engine-goroutine panic is a whole-process kill.
7. Quick wins: the FIFO regular-file pre-check, the session-decode size cap, `literalPathspec` on the git pathspec builders, and the pre-opened-template thinking-channel flag.

## What Looked Good

The engine core is genuinely well-built and the security posture is fundamentally sound: the dispatch/Approver/resolution folds (including the allow-for-session cache semantics), the seven Floor guards, the confinement capability matrix with its Residual disclosure discipline, the single-goroutine concurrency contract with the ADR-0011 `p.Send`-only event path, and the bubble-tea value-copy rule all held up under a five-lens audit plus independent verification — the largest share of the loudest candidate findings (the beads stale-export register, the config-splice truncation, the tuitest hang/race pair, the skills export misdirection, the tuitest frame-authority divergence) was refuted with evidence rather than merely downgraded. The provider layer caps every completion/error body, refuses redirects, bounds retries, and redacts keys; the stubllm scripting and tuitest driver are unusually well-instrumented; and the reactions Runner's bounded queues, atomics, and documentation are careful. The three areas a reader should be most wary of touching are exactly the ones named in the Criticals: `internal/doctext`'s PDF path, `internal/undo`'s journal locking, and the `.beads/hooks` trust boundary.