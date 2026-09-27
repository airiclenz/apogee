// Package workflow is the engine's Workflow layer (ADR 0087): stages of items, each item done by a
// fresh child that hands back a Receipt, the parent reading one line per item plus a report path.
// A workflow has exactly two sources and in both the work is asked for — the model's one-fanout
// `fan_out` call (ValidateModelPlan bounds it to a fanout, then at most one verify, then at most
// one merge) or a human-written Recipe carrying any of the seven stage kinds. The engine plans
// nothing itself.
//
// The boundary. This is a Driver-free library the root facade re-exports for the bench (ADR 0087
// D10, ADR 0031): it imports the standard library and at most internal/domain from the tree, and
// never internal/agent, internal/config, internal/run or internal/tools. What it needs from them —
// spawning a child through the recursion point, running a script, asking the user — arrives as
// interfaces the caller implements, so the dependency points inward.
//
// # The files, one line each
//
// plan.go is the data: Plan, Stage and its seven StageKinds, ItemSource, the Receipt a child
// hands back, the ReceiptSpec that declares its typed fields with ReceiptSpec.Check (the on-the-spot
// check a `finish` call gets, ADR 0087 D3), and the Problem both checks report.
// validate.go is Validate and ValidateModelPlan: shape checks over a Plan whose every Problem names
// the stage and field and says how to fix it, a stage's `when:` included.
// cond.go is ParseCond, Cond.Check and Cond.Eval: the `when:` condition language over receipt
// fields (`field op value` terms, and / or / not, parentheses), type-checked against a ReceiptSpec.
// items.go is Expand, Item and SplitBudget: a fanout's items from a literal list, a `files:` glob
// (`**` included), a file's non-blank `lines:`, or a `split:` directory cut into contiguous parts
// sized to a child's context window, batched N per child.
// store.go is Store, ItemKey and PlanHash: one workflow's folder under `<scratch>/workflows/` —
// plan.json, status.json, items/<key>/ with receipt and transcript, stage outputs — written
// atomically, its items keyed by content so a re-issue found by PlanHash skips finished work.
// runner.go is Runner.Run and the Spawner seam the agent implements: a fanout stage's items run as
// fresh children at most Width at a time, a capped child continued (ItemSpec.Prior) and a faulted
// or receipt-less one retried within configured bounds, each receipt stored as it lands, so a
// cancel keeps finished items and returns a stopped Result.
// stages.go is the verify and merge stages over a fanout's results, on runner.go's wave path: verify
// runs one adversarial child per item its `when:` selects, the engine's briefs/verify.txt leading
// the stage's brief, and folds each verdict into the item; merge runs one child over a manifest of
// every item's output, briefs/merge.txt leading, and has it write report.md in the folder.
// And doc.go this map.
package workflow
