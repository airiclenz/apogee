A: Activated / Active
P: Planned
X: Executed

- [P] the (multiple option) question tool should print out a well formatted summary of the question/options and given answer(s) in the collapsable tool after the question(s) have been answered. The tool's description for the model should also briefly state to no print the questions beforehand - as this uses up tokens unnecessaryly.

- [P] A `/server` switch never reaches the rebind resolver's inputs. `rebindSpecFor` (cmd/apogee/wire.go:689) is called from the rebind closure (wire.go:365) with the LAUNCH `opts`, captured once at wiring time and never updated by `switchServer` (wire.go:402) — and `startupSetDecision` (cmd/apogee/validatedsets.go:88-93) keys the validated-set lookup on `opts.model` **and** `opts.endpoint`, so after a move the per-model set is matched against the endpoint the session STARTED on rather than the one it now talks to. `scheduleWiring.fire` (cmd/apogee/schedule.go:73) carries the same split from the other side: the wire's endpoint and key come from the live `binding()`, the spec from `w.opts`, so a Firing after a switch runs against the new server with the old server's validated-set decision. Pre-existing — both predate the servers-single-definition plan (`docs/plans/2026-08-05 - 05`, which named them explicitly out of scope) — but more visible now that a switch is persisted as `server:` and the next launch starts where the last one ended. Fix shape: carry the bound upstream into the rebind inputs instead of the launch `opts`, one seam serving both callers.

- [X] Currently I cannot see how much of it's context a sub agent has used.

- [ ] keyboard path for collapse/expand: a block-cursor mode (↑/↓ move a highlighted block, enter toggles, esc leaves). Deliberately deferred from the collapse wave — layout.md "Collapsed and expanded blocks" keeps toggling mouse-only for now, on the same precedent that keeps transcript selection mouse-only.
