---
title: "ADR 0083 part 5: Host boot stays three Drivers, not one module (2026-09-15, review 09-14 #18)"
---

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

> **Note 2026-10-29 (knowledge inventory).** The key-resolver parenthetical above no longer
> describes the tree. The daemon's `api-key-cmd`-in-Auto gap the bead recorded was closed on
> 2026-09-16 (e0650310; issue `daemon-api-key-unfenced`, alias apogee-add): the daemon still builds
> its resolver rootless, but every resolution is fenced against the Firing's own workspace
> (`KeyResolver.ResolveWithin`, `cmd/apogee/daemonfire.go`). The probe's root has been the
> workspace since 2026-09-27 (7b9d9469; `cmd/apogee/probe.go`, `cmd/apogee/probemodel.go`), so
> only the daemon now differs, because it has no one boot-time root. The difference in kind this
> section relies on still holds for the daemon.
