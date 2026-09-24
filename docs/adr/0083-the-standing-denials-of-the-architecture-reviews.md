---
Status: accepted
---

# The standing denials of the architecture reviews

## Context

An architecture review (`/improve-codebase-architecture`, saved under `docs/reviews/`) reads the
tree afresh each time, and a candidate the owner has already declined is found again by the next
one: the shape is still there, the explorers still see it, and nothing in the tree says it was
weighed and refused. The 2026-09-14 review raised a live *Posture* value, a host-side bound entry,
a host-boot module and a Client-owned effort dialect; the owner ruled on each in the two
2026-09-15 plans (`docs/plans/archived/2026-09-15 - 00 - engine-shape-deepening-plan.md`,
`… - 01 - host-config-and-tools-deepening-plan.md`), where a denied row was "recorded here only" —
a verdict table and an *Out of scope* line in a plan that was archived when its items landed. The
2026-09-16 review found the delegate's flat fields again and listed the bound entry's residue
under *Adjudicated earlier*; the 2026-09-20 review (`docs/reviews/architecture-review-2026-09-20.html`)
listed all five under the same heading, added a sixth candidate that contradicts two ADRs, and
said the quiet part: "None has an ADR — if a ruling is meant to be permanent, an ADR is the
cheapest way to stop the next review re-finding it."

A plan is a design artefact for the work it contains, not a register of what was refused; once
archived, a denial in it is invisible to the next reviewer and to the next agent, and the
refusal's *reason* — the part that decides whether new evidence reopens it — is a table cell.
This record moves the six standing denials into the place settled questions live. It ratifies
the owner's call of 2026-09-20 (plan `2026-09-20 - 02`, *Ratified design calls*: "the denial and
the five *Adjudicated earlier* rulings become one ADR").

## Decision

Each of the six sections below is a **standing denial**: a candidate the owner has declined, with
the date it was decided, the review that raised it, the reason it was refused and the evidence
that would reopen it. A review may list a standing denial under *Adjudicated earlier* — with any
new measurement of the residue it leaves — but not as a card; a plan may name one under *Out of
scope* without re-arguing it. Reopening one is an owner's call on the evidence its section names,
recorded as a dated amendment to that section, never by a plan that quietly includes the work.

## 1. The session record keeps presenter verdicts on the wire (2026-09-20, review 09-20 #11)

**Proposed.** One Driver-neutral fold in `internal/session` — `session.Fold(Event) → Entry`,
facts only (kind, text, depth, spawn id, tool name, bounded args, chars, Edit regions, prune and
abort marks) — with the TUI's presenter deriving label, verb, target, stat and solo at paint or
decode time; the eight presenter verdicts `session.ToolView` carries (`Label`, `Verb`, `Target`,
`Solo`, `Stat`, `StatValue`, `Summary.Quoted`, `Details[].Kind`) would leave the wire, and
`run.transcriptFold`'s raw-name cards and the TUI's friendly ones would stop being two shapes of
one blob. The 2026-09-14 review's candidate 12 ("session-record rules live in `internal/session`")
was the same proposal; plan `2026-09-15 - 01` denied it ("the `contentArgs` divergence is
documented design") and the 2026-09-20 review re-raised it on new evidence — verdicts on the
wire, three consumers (`internal/tui/transcriptbridge.go`, `internal/run/transcript.go`,
`cmd/demorig`), and a Firing's record replaying with raw-name cards where a TUI record replays
with friendly ones.

**Denied** (owner, 2026-09-20). [ADR 0022](0022-sessions-persist-per-turn-as-dual-representation-records.md)
decision 2 (as amended 2026-08-31: the blob is `internal/session`'s, Driver-neutral, so that any
Driver can write and replay a scrollback) and [ADR 0052](0052-diff-bodies-render-as-split-diffs-fed-by-tool-recorded-edit-regions.md)
decision 5 (as amended 2026-08-24: the codec stores the RENDERED tool view and nothing on the
replay path re-runs a presenter) stand. The record is a scrollback, not an event log: resume
repaints *exactly* what the session showed, and a record that came back without verdicts would
replay as a scrollback that changed shape across a restart — the wording an older build chose is
the wording that session showed, and a replay that re-decides it is a replay that lies about
what the user saw. `fromWireToolView` re-deriving `Solo` from the live registry
(`internal/tui/transcriptbridge.go`) is the **documented exception, not a precedent**: it runs in
the one direction that can ADD solo, for a blob written before `Solo` rode the wire, so that two
span-less heads do not fold into one counted block — a repair of a record that predates the
fact, not a preference for the current word over the recorded one. That a Firing's record
carries raw names where a TUI record carries friendly ones is the documented `contentArgs`
divergence: `run.transcriptFold` "does NOT fold anything a PRESENTER decided", by design.

**Reopens on.** A second Driver that must *paint* a record (not merely write one) and cannot
carry the TUI's presenter — a daemon or web scrollback that needs the verdicts re-derived
because it has no rendered form to replay; or a codec version bump that has to migrate the
verdicts anyway. The three-consumer count and the Firing-replay divergence do not reopen it on
their own; they are what the design chose.

## 2. The ten live settings stay ten fields, not one Posture value (2026-09-15, review 09-14 #5)

**Proposed.** One `Posture` value on the Agent — mode, confinement, scratch, generation,
compaction, prune, context files, parallel cap, seat, effort — swapped whole through
`SetPosture` / `Posture()`, the shape `Generation()` already has; `lateEngine` would hold one
pending value and replay it whole at `Bind`, `run.Spec` would hand one field over, and the child
spawn would copy `parent.Posture()`.

**Denied** (owner, 2026-09-15; plan `2026-09-15 - 00`, verdict table and *Out of scope*: "a
Posture value"). It contradicts [ADR 0037](0037-every-settings-edit-applies-to-the-running-session.md)
decision 2's consequence text — a live seam is **anytime-safe** (one mutex, one field, consumed at
a boundary the loop crosses constantly) or **idle-only validate-then-commit**, and there is no
third class — and a value swapped whole invites lost updates: two anytime-safe setters that
today touch two fields under two locks would, on one value, race to overwrite each other's field
with a stale copy. The 2026-09-20 review measured the residue at ten settings × six places
(field and mutex, getter, setter, cfg seed, the child re-copy in `subagent.go`, the `lateEngine`
pending-plus-`Bind`-plus-apply ladder); that cost is accepted. One exception already exists and
is recorded where it lives: `internal/agent/agent.go` states that ADR 0037 D2's "one mutex, one
field" is SUPERSEDED for the Generation by [ADR 0076](0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md)
A8, because Bypass and the Floor enable set must swap together — a value whose halves *must*
move on one clock is the one case a whole-value swap is the safe shape, and it stays the only one.

**Reopens on.** A second pair of settings that must move on one clock (the Generation argument
applying to another pair), or a demonstrated lost-update or torn-read across today's per-field
locks that a single value would close rather than open.

> **Note 2026-09-24 (review 09-20 #15; plan `2026-09-24 - 00`, *Ratified design calls*).** The
> 2026-09-20 review proposed the `lateEngine` half of this residue again in a smaller shape: its
> typed `pending*` fields folded into one ordered replay list of `func(*Agent) error`, refusable
> entries last. **Declined** (owner, 2026-09-24): the typed pending fields are kept, as the
> `lateEngine` pending-plus-`Bind`-plus-apply ladder this section already accepts as residue. Only
> the card's defect was taken: `Bind` replayed the delegation target before the seat, so the seat
> forgot the far width the target had just stated; it now replays the seat first ([ADR 0069](0069-the-top-level-model-picks-the-delegation-seat.md)
> decision 6, 2026-09-24 note).

## 3. A delegate's runtime state is not one value; delegation is not the whole spawn (2026-09-16, review 09-16 #26)

**Proposed.** `Agent.delegate *delegateState` (nil at depth 0) holding the delegate-only fields
`internal/agent/agent.go` keeps flat — depth, call id, console owner, task, seat fallback, cap
requested, name, mailbox, steered, step cap, token and time caps, output path and target,
mid-Exchange compaction, step notice, live mode — with `isDelegate` reading "set", and
`Run`/`finishAtStepCap`/`delegationResult` reading the value whole; a follow-on to plan
`2026-09-15 - 00` item 8, which made a child be *built from* a `delegation` value.

**Denied** (owner, 2026-09-15, reaffirmed 2026-09-16 and 2026-09-20). Item 8's regression guard
kept the fields flat deliberately, so that the test files that fake a child by setting a field
keep compiling; the 2026-09-16 review itself filed the candidate *Speculative* — "the owner
deferred this knowingly on 09-15 for test-churn reasons … raise only if the test-surface
argument outweighs the churn" — and noted it is not the denied Posture value, since nothing in
it is a live setter. The construction half is done and stays: the `delegation` value is the
constructor's INPUT, and `newChildAgentOn` makes no post-construction writes. What is declined is
the second half — making that value the child's *runtime* state — because it moves nothing the
loop reads on one clock, and `runSubAgent` still writes call-derived facts after construction
and reads the child's fields for the report because those facts arrive at different moments of
the spawn.

**Reopens on.** A test-surface count that outweighs the churn — for instance, a child fake that
has to set more fields than the constructor takes, or a delegate-only field added that a flat
layout leaves readable at depth 0 and a bug results from it.

## 4. The bound entry stays six host-side assembly sites — `Options.StartupEntry` only (2026-09-15, review 09-14 #7)

**Proposed.** "The Upstream this session is on" as one value in `internal/config` — the entry plus
its ranked bindings — computed by one function and handed to the engine, the heartbeat Monitor,
the settings holder and the Firing composer, replacing `ServerEntry`, the flattened `Options`
fields, `upstreamBinding`, the `liveSettings.entry*` fields and the engine's copy, and the ranking
ladder re-run at each arrival site.

**Ratified "IN, reduced"** (owner, 2026-09-15; plan `2026-09-15 - 01`, item 6): `Options.StartupEntry`
replaced the fourteen flattened fields, and that is the whole of it — the three `Resolve*`
ladders are not changed and `liveSettings.entry*` is untouched. The residue the 2026-09-16 review
listed under *Adjudicated earlier* (the per-entry ranking ladder still called at every arrival —
`serverBinder.bind`, `sessionMover.move`, `firingConfig`, `rebindInputs`) and the 2026-09-20 review
measured (six assembly sites in `cmd/apogee`; `liveSettings.firingBinding` in
`cmd/apogee/wire_settings.go` hand-zeroing the parallel cap, the key-source fields, the
description and the forced `effort-dialect:` for an in-session Firing) is accepted as it stands.
The reduction stands because the arrival sites do not agree on purpose: the boot binding, a
`/server` move, a Firing's composed entry and a rebind each rank the top-level keys against a
different observed state (what the beat saw, what the session already resolved), and one function
that "turns the entry into engine-facing facts" would have to take that state as a parameter and
grow the same branches inside. `firingBinding`'s comment names the road not taken — the
Driver-parity alternative of [ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)
— as "a decision not taken here", and that is the standing form of it. The engine-side half of the
same review's candidate 6 ("one server binding with one zero rule — engine side") is new and is
not covered by this section.

**Reopens on.** A fifth arrival site that re-spells the ladder, or an entry key added that
needs an edit at every one of the six — the "adding an entry key: 12 edits → 3" leverage the
candidate claimed, once it is measured against a key that actually arrives.

## 5. Host boot stays three Drivers, not one module (2026-09-15, review 09-14 #18)

**Proposed.** `hostBoot(mode, confine, narrator) → {roots, store, keys, confiner, sweeps, close()}`
in `cmd/apogee`, booted once per subcommand, with `runRoot`, `runHeadlessBody` and the daemon as
three adapters of it and the fifteen function-valued globals gone.

**Denied** (owner, 2026-09-15; plan `2026-09-15 - 01`, verdict table: "~6 truly shared steps,
sentences differ in kind"; *Out of scope*: `hostBoot`). Of the eleven steps the candidate drew,
about six are genuinely shared, and the ones that differ differ in *kind*, not detail: the TUI
degrades on a confinement sentence, headless refuses, the daemon latches it per workspace (the
wording of the sentences is already shared; the calling deliberately does not share the
verdict); the key resolver's
exec-fence root is the workspace for the TUI and headless and `""` for the daemon and probe on
purpose (a bead records the narrow `api-key-cmd`-in-Auto gap). One module would carry a mode
switch inside every step that differs, which is the three Drivers written once with more
branches, not less. The one drift the candidate exposed — the snapshot-directory sweep running at
TUI boot only — was a defect and was taken in on its own. The 2026-09-20 review's count (the three
bodies re-spelling roots, sweeps, confiner, eligibility ladder and notices, each "for the reason
`runRoot` runs it"; fifteen function-valued globals) is the accepted residue; the cross-reference
comments are the contract.

**Reopens on.** A fourth Driver in `cmd/apogee` — a fourth spelling of the shared six is the
point at which the sentence-in-kind argument stops paying — or a second boot drift of the
snapshot-sweep kind, where a step one Driver runs and another silently does not.

## 6. Wire traits stay inside the codec; the `== WireAnthropic` branches outside the adapters are tolerated (2026-09-15, review 09-14 #8)

**Proposed.** The provider Client owns the effort dialect end-to-end — dialled with the pin,
latching its own detection, answering "can I ask this server for no reasoning?" — with the
request stating intent only; the `domain` mirror enum, the two mappers in `internal/agent/wire.go`
and the agent, spec and target dialect fields would disappear.

**Denied** (owner, 2026-09-15; plan `2026-09-15 - 01`, verdict table: "[ADR 0060](0060-effort-is-detected-passively-dialected-per-server-and-picked.md)
D9 ratifies the channel; detection happens in the Monitor's Client"). The dialect is detected
passively by the heartbeat Monitor's Client, not the completion Client, so it *has* to travel:
ADR 0060 decision 9 makes the dialect the one fact that reaches the engine and names the channel
it rides; a completion Client that latches its own detection would either detect twice or dial
the Monitor's, which is the ferrying the candidate objected to under a different name. What did
land — later, and on its own record — is [ADR 0078](0078-a-servers-wire-is-a-per-entry-codec-inside-the-provider-client.md)
(2026-09-16): the *wire* (`openai | anthropic`) is an unexported `wireCodec` inside the one
`provider.Client`, with "no engine-visible branch", and it implies the effort mapping (its
decision 3). That is where wire traits live. The branches on the wire that sit OUTSIDE the two
adapters — the 2026-09-20 review counted seven at `f30c37e0` (`provider/discovery.go`,
`provider/stream.go`, `config.go`, `probe/host.go`); at this record's HEAD they are the Client's
own codec selection and discovery arm (`selectCodec`, `WireFor`, `discovery.go`), the config's
`"anthropic"` rules (the `effort-dialect:` composition refusal, the wire enum, the keyed
parallel-agents default in `DefaultParallelAgents`) and `probe/host.go`'s two — are
**tolerated**: each is a validation or a reporting rule about the entry, or the one place the
codec is chosen, not a codec decision leaking out, and a count that drifts with the tree is not
the rule.

**Reopens on.** A third wire. Two arms are a branch; three are a table, and the seven tolerated
branches become fourteen at the same time — at which point the codec's trait table (ADR 0078
decision 7's seam) grows the answers those sites ask for and the branches fold into it. Or: a
routed child inheriting the wrong dialect again, the incident that motivated the candidate,
recurring after ADR 0078's dial seam.

## Considered and rejected

- **Leaving the denials in the plans' verdict tables.** The archived plan is where they were, and
  the 2026-09-16 and 2026-09-20 reviews found every one of them again; a plan is a design artefact
  for the work it contains, and its *Out of scope* line is read by the run that executes it, not
  by the next review.
- **One ADR per denial.** Six records that each say "a review proposed X; the owner said no, and
  here is why" is the same content behind six file names; the reason a reader opens any of them
  is "has this been ruled on?", which one list answers.
- **A `denied:` marker in `CONTEXT.md`.** The concept map defines the domain language; a ruling on
  a candidate is an architectural decision, and settled decisions live under `docs/adr/`
  (`AGENTS.md`, *Where knowledge lives*). `CONTEXT.md` links here only from the entries that
  already cite the topic a denial protects.
- **Recording each denial as a bead.** A bead is open work or parked work; a standing denial is
  neither — it is the decision that there is no work.
- **Writing the six as amendments to the ADRs they protect** (0022, 0037, 0052, 0060). Three of
  the six protect no single ADR (the delegate value, the bound entry, host boot), and a reviewer
  looking for "was this ruled on?" would have to know which ADR to open before finding out.

## Consequences

- A review's *Adjudicated earlier* section cites this record and lists the residue it measures;
  a candidate that matches a section here is not raised as a card. A plan that names one of the
  six under *Out of scope* cites the section rather than re-arguing it.
- `CONTEXT.md`'s *Tool summary* and *Edit regions* entries, which cite ADR 0052 §5, point here for
  the standing denial that keeps the verdicts on the wire; no other `CONTEXT.md` entry cites a
  denied topic, so no other link is added.
- The residues are accepted, and their sizes are on record: ten settings × six places; the
  delegate-only fields flat on `Agent`; six assembly sites and three ranking ladders for the bound
  entry; three boot bodies with their cross-reference comments; the wire branches outside the
  adapters. A change to one of those numbers is a fact for the next review to report, not a
  reason to reopen.
- Reopening any section is a dated amendment to it, made on the evidence the section names; a
  denial that is reversed gets its own ADR for the design that replaces it, and this record's
  section is marked superseded with a pointer.
