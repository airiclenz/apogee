// Package run performs one Firing: a single headless run of one prompt against a fresh
// Agent, saved as an ordinary session record (ADR 0033).
//
// It is the shared core of every unattended run — the scheduler's Firings and the
// `apogee headless` subcommand alike — which is ADR 0031's first mechanical
// consequence in code: a capability that works only under the TUI breaks visibly here
// rather than silently. The package imports internal/agent, internal/domain and
// internal/session downward only (ADR 0010); it reaches neither the root facade nor
// internal/tui, and it knows nothing about cycles, overlap or deferral — every
// when-and-how decision belongs to the scheduler above it.
//
// A Firing's Prompt is a full apogee message, not a bare string: its @file tokens are parsed
// here through internal/refs and submitted as domain.UserInput.FileRefs, so the loop resolves
// them within the workspace fence exactly as it resolves a session's. Its /skill tokens are
// parsed by the same package and submitted as domain.UserInput.SkillIDs, against the catalog
// Config.Skills already carries — so a Firing whose prompt names a skill reaches the same
// injected body a chat message does, and a Config with no resolver leaves every "/" token
// prose. A missing or escaping ref — and an unknown skill ID — is skipped WITHOUT notice: the
// loop reports one as an ErrorEvent, and a Firing has no event sink that would carry it (the
// daemon leaves Config.Events nil, and headless wires only its own prune-notice sink, which
// renders domain.PruneEvent and nothing else).
//
// # Nothing waits for a human
//
// A Firing has no human to rendezvous with, so Once PINS all three interactive host
// delegates regardless of what the caller's Config carries. The Approver is a fail-safe
// denier: every gated action is refused immediately, the refusal is recorded as the
// engine's ordinary ApprovalEvent, the count lands on Result.Denied, and nothing ever
// parks. The Asker and Presenter are nil, which simply unregisters ask_user and
// present_document — a Firing's deliverable is a file in the workspace with its path in
// the conversation. This is ADR 0031's invariant 2 in code: parking is a Driver's
// composition, and this Driver's composition is "don't".
//
// # Auto-eligibility is the caller's gate
//
// Once trusts a Spec that says Auto exactly as agent.New trusts a Config that says Auto:
// the eligibility ladder (confinement capability plus confine-to-workspace) lives where
// the mode is CHOSEN — the TUI's schedule popup, the headless CLI's flags — not here.
// Once validates only that the mode is one a Firing may run in at all: Plan or Auto.
// Ask-Before and Allow-Edits are refused, because both exist to consult a human
// (decision 2 of ADR 0033).
//
// # The scrollback the record replays from
//
// The saved record carries a transcript blob of its own, folded from the engine's Event
// stream as the run goes (transcript.go), so resuming a Firing from the /sessions browser
// repaints what it actually did instead of taking ADR 0022's no-scrollback degrade path.
// The fold is deliberately narrow: the submitted prompt, committed assistant text, tool
// calls with bounded arguments, tool results, errors and the note a pruning pass leaves
// behind, each at the Depth that emitted it.
// Nothing a PRESENTER decided is in it — a runner paints nothing, so a card carries the
// call's raw name and no invented label — and where the stream lacks a fact an entry needs,
// the entry is omitted rather than synthesized. A blob that could not be encoded degrades
// to a record without one, which replays exactly as a pre-blob record does; the save itself
// never fails over it.
//
// # What v1 deliberately does not do
//
// The record is saved ONCE, at completion, and on a failed run it still saves whatever
// completed. That is a deliberate scoping of ADR 0022's per-Turn cadence to the
// interactive TUI (whose worker owns that crash-safety policy — session.Store itself
// prescribes no cadence): a Firing is bounded and unattended, so a crash loses only that
// one run. The record's id is minted at that same completion unless Spec.RecordID names one
// up front, which is what a Driver does when something it created BEFORE the run — the
// Firing's scratch dir — has to already carry the record's name.
//
// A caller that wants to observe the run supplies Config.Events; a nil sink is a discard.
// A Turn the loop abandoned reports domain.StepResult.Faulted and surfaces its own
// ErrorEvent through that sink — Once does not translate it into a returned error, since
// the Exchange did reach its boundary. It travels on the Result instead, as data no
// observer is needed to catch: Result.Faulted says the final Turn was abandoned and
// Result.Fault carries the reason, the same sentence that ErrorEvent said. So Err stays
// nil, FinalText still carries whatever the run last said, and a caller reads Faulted to
// know that text is not the answer. A CANCELLED run does become Result.Err: it never
// reached an answer, and an unattended caller has no other way to tell.
//
// Context fill IS reported, at two grains, because neither survives the run otherwise. The
// Firing's own fill lands on the record's Meta.CtxUsed, and each SUB-AGENT run's final fill
// lands per run on Result.SubAgents. The second is not a decomposition of the first: a
// delegated run fills a window of its own (it inherits the parent's Config verbatim, so the
// same limit, never the same fill), so its reading is neither the Firing's nor summable into
// it, and a nested run's reading is its own rather than its spawner's.
//
// Token SPEND is reported at those same two grains, for the same reason and by the same
// keying: the Firing's own cumulative totals land on Result.Usage and each delegated run's on
// its Result.SubAgents entry. Spend is not fill — it counts every completion the agent
// accounted for, the Compaction folds included, where a fill says only how full the window
// ended — and it is per-agent, so the two grains are reported apart.
//
// Unlike fill, spend is also SUMMED into the record this package saves: Meta.Usage takes the
// Firing's own totals and Meta.DelegateUsage the sum of every delegated run's. That roll-up is
// taken here rather than left to a caller because an unattended record has no caller holding
// the run heads — a Schedule fires and walks away — while the /sessions browser reads a
// session's spend as Meta.Usage + Meta.DelegateUsage off the record alone, and would otherwise
// report every Firing as having cost nothing. Summing loses no detail a Driver can still use:
// Result.SubAgents keeps the per-run entries for the caller that wants them. The two stay
// separate keys, never one folded figure, so a reader can still tell the Firing's own spend
// from what it delegated.
package run
