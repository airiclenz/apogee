A: Activated / Active
P: Planned
X: Executed

- [ ] all hard coded prompts need to be plain files (e.g. internal/agent/loop.go 887)

- [ ] Copying selected text does not put anything into the clipboard.

- [ ] when calling the slash command /settings I can do this: type `/sett`, make sure that the correct menu item in the pop up is selected, then press tab or enter to select the command. This automatically opend the settings page. This does not work for the /server command nor for the /model command. pressing tab/enter just complets the term in the prompt editor - I still need to press enter another time to open the popup menu. Please update this so that /server and /model behave the same way as /settings

- [ ] an *approved* out-of-workspace write still errors at Execute — the confinement contract's §4 "WS-write, target out of workspace → gate" row is now half-landed: dispatch classifies the target with `resolveTargetUnbounded` (`internal/tools/workspace_scoped.go:102`), so an out-of-workspace write reaches the approval Gate instead of being pre-rejected, but the write tool never learned to honour that approval — `internal/tools/write_file.go:82` writes through the os.Root fence pinned at the workspace root (`safeWriteFile` → `security.SafeWriteFile`), which refuses the escape regardless of the verdict, so the human approves and then gets an error result. Contract §4 says the same thing in its "Realisation gap — half-landed" note: the row is no longer unreachable, and the `Execute` half is the part still open. Decision pending, and it is an owner call either way: land the P3.7 reconciliation the contract promises (resolve against `WorkspaceRoot ∪ box.WritablePaths` and honour a dispatch-approved target) or ratify strict fencing as the permanent answer and amend §4 to say the Gate's allow is advisory for writes. Surfaced by the 2026-08-10 doc-landscape audit (`docs/reviews/2026-08-10 - 00 - doc-landscape-audit.md`, Flag 1); tracked nowhere before this line.



## Hostile-bytes hardening run — open follow-ups (2026-08-12)

Raised while executing `docs/plans/private/2026-08-11 - 06 - hostile-bytes-hardening-plan.md` (all
20 items now ✅ done): residuals the plan's items deliberately did not reach, plus two doc claims
those items' own fixes falsified. Same threat model as the section above — the operator is trusted,
the bytes they operate on are not, and neither is the model. Every citation was re-read against the
working tree on 2026-08-12.

- [ ] A non-JSON `Arguments` blob bypasses `orderedArgs` (`internal/tui/toolpresent.go:2305`, which returns false for anything that is not a JSON object) and falls through to `prettyJSONDetails` (`:2181`), which emits every line verbatim as an unindented body row — so a blob can still paint a forged `Reason:` row on the approval pane, the very row item 6 flattened the labelled path to stop. Nothing validates the blob upstream: `internal/provider/stream.go:201` carries `frag.Function.Arguments` through as-is.

- [ ] The approval pane title is `"Approve " + stripEscapes(req.Tool) + "?"` (`internal/tui/approval.go:231`) with no `flattenField`, and `popupTitleLine` (`internal/tui/popup.go:1245`) does not fold either — so a newline in a tool NAME paints a second, unindented row above the pane's own body. Reachable via an MCP-supplied tool name, which apogee does not author.

- [ ] The result-string half of item 8's resolved-path disclosure reaches only four of the seven workspace-scoped writers: `resolvedTargetNote` is called from `internal/tools/write_file.go:81`, `file_edit.go:90` and `find_replace.go:107,229`, while `copy_file`, `move_file` and `delete_file` (`internal/tools/file_ops.go`) still echo the literal argument in their success sentences for a write that landed somewhere else.

- [ ] `go_vet`'s package-directory scope is disclosed on the tool description (`internal/tools/diagnostics.go:61`) and on both vet result strings (`:192`, `:194`, via `vettedPackageLine` `:326`) but NOT on the approval pane — the one surface the human actually decides on. `domain.ApprovalRequest` (`internal/domain/approval.go:33`) carries no scope field, so a truthful pane line needs a new engine field plus a TUI renderer.

- [ ] `SafeRename` (`internal/security/safeio.go:517`), `SafeRemove` (`:549`) and `SafeCopyFileFrom` (`:403`) still follow symlinked parents inside the root: item 13's `refuseSymlinkedParents` (`:177`) is applied by `SafeWriteFile` alone (`:120`), so `move_file` / `delete_file` / `copy_file` keep the `docs → .git` redirection the write path closed. Gating them requires first fixing `MoveFile.move`'s copy-then-remove fallback (`internal/tools/file_ops.go:200`), which would otherwise trip the new refusal on the rename-failed path.

- [ ] `read_file` carries no resolves-to disclosure: `internal/tools/read_file.go:94` returns `okSummary` naming the literal argument, and the tool is not a `workspaceScopedWriter`, so none of item 8's plumbing reaches it. Ratified design call 2 — "symlinked reads: follow, but show the resolution" — is therefore only half-landed; the write path got the line, the read path did not.

- [ ] The `write-ssh-keys` and `write-credential-persistence` dangerous-action rules (`internal/security/rules.go:50,60`) anchor on `(?:~|/home/[^/\s]+|/root|\$home)` with no macOS `/users/<name>` alternative, so `/Users/<name>/.ssh/id_rsa` and `/Users/<name>/.aws/credentials` never match — confirmed inert on the desktop persona. The newer `write-apogee-control-plane` rule (`:101`) already spells the macOS home; these two were not updated alongside it.

- [ ] `terminal` (`internal/tools/terminal.go:98`) and `python_exec` (`internal/tools/python_exec.go:241`) still inherit an unscoped `PATH` through `subprocessEnv` (`internal/tools/exec_common.go:80`), which strips only apogee's own credentials. PATH scoping — `shellHost.ScopeEnv`, which drops the entries that live inside the workspace — landed for git (`internal/tools/git.go:81`) and the Go toolchain (`internal/tools/diagnostics.go:307`) only.

- [ ] `run_tests` is the one execution tool that inherits the environment whole: it leaves `spec.env` nil (`internal/tools/run_tests.go:247`), so `os/exec` hands the child the parent's full environment including `APOGEE_API_KEY` — the credential `terminal` and `python_exec` deliberately withhold. A repo-authored test suite is untrusted bytes under this threat model, so it is the widest of the three environments.

- [ ] `/skills` still re-walks the skill source dirs synchronously on the Bubble Tea update goroutine: `runSkills` calls `m.opts.ReloadSkills()` inline (`internal/tui/skills.go:44`). Item 12 moved the merged `/` menu's re-scan off that goroutine but left this trigger on it, so the same render-loop block (ADR 0011) survives behind a different key.

- [ ] `promptEditor.reset()` (`internal/tui/prompteditor.go:223`) clears the textarea and the autocomplete overlay but not `skillRegion` (`:61`), so submitting on an exact `/skill` token leaves the edge-trigger true and the next `/` menu opens with no re-scan — listing a stale catalog. `dismissAutocomplete` (`internal/tui/autocomplete.go:668`) clears both, which is the shape `reset` should share.

- [ ] `internal/domain/events.go:122` still states the wire-silent invariant as "nothing is added to a tool's arguments or its result", which item 8 falsified: `internal/tools/workspace_scoped.go:109` appends ` → resolves to <path>` to the result string of every write whose target differs from its argument. The arguments half of the invariant is intact; the comment needs to name result strings as the exception.

- [ ] `internal/present/server.go:61` and `internal/present/server_test.go:180` both list `.xhtml` among what rung 2 shows, but `.xhtml` is in neither `browserRenderableExts` (`internal/tui/presenter.go:181` — `.html`, `.htm`, `.svg`, `.pdf`) nor `openerRenderableExts` (`internal/present/opener.go:297`). Item 4 dropped it from rung 1 without ever adding it to rung 2, so both comments — and the CSP rationale one of them carries — name a format no rung serves.
