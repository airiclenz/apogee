A: Activated / Active
P: Planned
X: Executed


- [ ] keyboard path for collapse/expand: a block-cursor mode (↑/↓ move a highlighted block, enter toggles, esc leaves). Deliberately deferred from the collapse wave — layout.md "Collapsed and expanded blocks" keeps toggling mouse-only for now, on the same precedent that keeps transcript selection mouse-only.

- [ ] an *approved* out-of-workspace write still errors at Execute — the confinement contract's §4 "WS-write, target out of workspace → gate" row is now half-landed: dispatch classifies the target with `resolveTargetUnbounded` (`internal/tools/workspace_scoped.go:102`), so an out-of-workspace write reaches the approval Gate instead of being pre-rejected, but the write tool never learned to honour that approval — `internal/tools/write_file.go:82` writes through the os.Root fence pinned at the workspace root (`safeWriteFile` → `security.SafeWriteFile`), which refuses the escape regardless of the verdict, so the human approves and then gets an error result. Contract §4 still describes the row as "unreachable", which is no longer the whole truth. Decision pending, and it is an owner call either way: land the P3.7 reconciliation the contract promises (resolve against `WorkspaceRoot ∪ box.WritablePaths` and honour a dispatch-approved target) or ratify strict fencing as the permanent answer and amend §4 to say the Gate's allow is advisory for writes. Surfaced by the 2026-08-10 doc-landscape audit (`docs/reviews/2026-08-10 - 00 - doc-landscape-audit.md`, Flag 1); tracked nowhere before this line.
