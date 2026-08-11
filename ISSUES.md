A: Activated / Active
P: Planned
X: Executed

- [ ] +N more lines needs to be part of the <tool-top-level-details>. Currently it is still printed in a new line (e.g. for non grouped `Ask User`, `Run`)

- [ ] <tool-top-level-details> is not currently printed in its defined color `tool-marker`

- [ ] The dotted lined that are used for tools (`⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯`) must have their own color code in a schema. The used symbol/character `⋯` should also be globally definable (for easy changing)


- [P] `sub_agent`'s tool description never tells the model it MAY emit several calls in one reply and that siblings run concurrently (`internal/tools/sub_agent.go:23`) — so the ADR 0039 fan-out sits unexercised under models that don't batch tool calls on their own: a live 2026-08-10 code-audit run on deepseek-v4-pro (OpenRouter Balanced) dispatched every phase one-call-per-message despite the skill prompt ordering "all in ONE message"; engine cap (`parallel: 5`) never engaged. One added sentence in the description is the cheap Mechanism-flavoured lever; deferred until the tool-display overhaul (`docs/plans/2026-08-10 - 04`) lands to avoid simultaneous edits near internal/tools.

- [ ] an *approved* out-of-workspace write still errors at Execute — the confinement contract's §4 "WS-write, target out of workspace → gate" row is now half-landed: dispatch classifies the target with `resolveTargetUnbounded` (`internal/tools/workspace_scoped.go:102`), so an out-of-workspace write reaches the approval Gate instead of being pre-rejected, but the write tool never learned to honour that approval — `internal/tools/write_file.go:82` writes through the os.Root fence pinned at the workspace root (`safeWriteFile` → `security.SafeWriteFile`), which refuses the escape regardless of the verdict, so the human approves and then gets an error result. Contract §4 says the same thing in its "Realisation gap — half-landed" note: the row is no longer unreachable, and the `Execute` half is the part still open. Decision pending, and it is an owner call either way: land the P3.7 reconciliation the contract promises (resolve against `WorkspaceRoot ∪ box.WritablePaths` and honour a dispatch-approved target) or ratify strict fencing as the permanent answer and amend §4 to say the Gate's allow is advisory for writes. Surfaced by the 2026-08-10 doc-landscape audit (`docs/reviews/2026-08-10 - 00 - doc-landscape-audit.md`, Flag 1); tracked nowhere before this line.
