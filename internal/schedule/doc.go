// Package schedule owns every when-and-how decision behind a Schedule: the cycle, the
// skip-on-overlap policy, the wait for a quiescent host, and the lifetime of the whole set
// (ADR 0033).
//
// It is deliberately RUNNER-AGNOSTIC. The thing that actually performs a Firing arrives as
// an injected seam, so the TUI can compose run.Once while a future daemon composes its own
// runner without inheriting the TUI's — the package therefore does not import internal/run,
// and it reaches nothing TUI-side at all. Its only downward import is internal/domain, for
// the autonomy mode a Schedule is created in (ADR 0010).
//
// # The policy this package owns
//
//   - A Schedule's Firings are strictly serial. A tick that lands while that Schedule's
//     Firing is still in flight is SKIPPED with an event; the next tick fires normally.
//     Nothing is ever queued and one Schedule never runs two Firings at once (decision 1).
//   - The same rule covers the quiescence wait: a due Firing first waits on Gate, and ticks
//     arriving during that wait are skipped exactly the same way, so at most one Firing is
//     ever pending per Schedule.
//   - Gating is PER SCHEDULE: one Schedule's blocked Gate never delays another's Firing.
//   - A Schedule runs in Plan or Auto only, refused at creation otherwise (decision 2). The
//     Auto-eligibility ladder is the CALLER's gate, exactly as it is for run.Once: the mode
//     is authorized where it is chosen (the TUI's popup, a headless CLI's flags), and this
//     package only checks that the mode is one a Firing may run in at all.
//   - The cycle floor is MinCycle, enforced here rather than at the surface, because a typo
//     hammering a single-slot local server is a policy problem and policy lives in the
//     library.
//
// # Lifetime
//
// Nothing persists. A Scheduler holds its Schedules in memory for as long as its Driver
// lives, which for the TUI means a Schedule DIES WITH THE TUI — "while apogee is open,
// re-run this every N minutes" is the whole v1 promise (ADR 0033). Durability across quit
// is the future daemon's value-add over this same library, not a missing feature here.
//
// # The seams and their contract
//
// Fire, Gate and Notify are called from the Scheduler's own goroutines, one set per
// Schedule, so all three must be safe for concurrent use. Notify is called synchronously
// and must not block for long — the TUI's implementation hands the Event to the program's
// send route and returns. A seam must never call Close, which waits for the very goroutine
// that would be calling it.
//
// # The clock
//
// Now and the ticker factory are bound into ONE Clock seam rather than injected separately,
// because a fake clock paired with a real ticker (or the reverse) is a bug rather than a
// configuration: every policy test in this package runs on a clock whose halves agree.
package schedule
