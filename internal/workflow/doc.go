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
// plan.json, status.json, items/<key>/ with receipt and transcript, results/<stage>/ with the script
// and ask outcomes a resume replays, stage outputs, the items.md listing a run ends with — written
// atomically, its items keyed by content so a re-issue found by PlanHash skips finished work.
// runner.go is Runner.Run and the Spawner seam the agent implements: a fanout stage's items run as
// fresh children at most Width at a time, a capped child continued (ItemSpec.Prior) and a faulted
// or receipt-less one retried within configured bounds, each receipt stored as it lands, so a
// cancel keeps finished items and returns a stopped Result.
// stages.go is every stage kind beyond the fanout. verify and merge work over a fanout's results on
// runner.go's wave path: verify runs one adversarial child per item its `when:` selects, the
// engine's briefs/verify.txt leading the stage's brief, and folds each verdict into the item; merge
// runs one child over a manifest of every item's output, briefs/merge.txt leading, and has it write
// report.md in the folder. The recipe-only kinds run no child: pick turns a receipt list or a
// folder file into the next fanout's items, script runs through the ScriptRunner seam, ask through
// the Asker seam (its default taken when there is none) — each outcome recorded, so the resume of
// an unfinished workflow replays it rather than running the script or asking again — and repeat
// re-runs a stage in rounds keyed apart; any stage's `when:`, read off earlier stages, skips it.
// inputs.go is InputDecl, ValidateInputs and BindInputs: the inputs a Recipe declares in its
// skill's header (name, required, default, description), and the binding of the user's text to
// them — `key=value` by name, the rest in declared order, quotes allowed — that starts it.
// recipe.go is Recipe and the RecipeSource port: a skill's recipe — plan, inputs, folder address
// and files — as the skill catalog serves it to the agent that starts it.
// format.go is Format: the result lines the parent reads — one `#<n> <item> — <status> — <summary>`
// line per item, a totals line, the stages' notes, `report:` — listing only the non-ok items past
// 40 and pointing to the full items.md the Store writes as the run ends.
// And doc.go this map.
package workflow
