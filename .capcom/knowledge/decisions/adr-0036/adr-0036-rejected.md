---
title: ADR 0036 considered options
---

- **Keep the quadruple as the startup server and the list as "the alternatives"** (the status quo)
  — rejected: it is two definitions of one thing, and every consumer pays for the seam. The
  synthesized picker row, its duplicate-name edge, `APOGEE_API_KEY`'s "belongs to the startup
  server" carve-out and `host-alias:`' existence are all bridge code between two shapes of the same
  four facts.
- **Keep the switch session-scoped and add `--save`** (ADR 0028's deferred option, taken literally)
  — rejected: a human who opened a picker and chose a machine has already expressed the intent the
  flag would ask them to repeat, and the failure mode of forgetting is silent and recurring, while
  the failure mode of recording is one visible line and a key they can delete. `/confine off`, the
  precedent that motivated session scope, is a **loosening of a safety fence** — the asymmetry is
  the whole point of that flag, and it does not transfer to "which box do I talk to".
- **Persist the switched endpoint in the session record so `--resume` returns to it** — rejected
  again, unchanged from ADR 0028: a session record describes a conversation, and which machine
  served it is not part of what is resumed. `server:` is machine state, and it lives in the config.
- **Auto-select the first entry when `server:` is unset** — rejected: it is a guess presented as a
  decision, and on a multi-server file it silently binds a machine the user did not pick. First run
  is exactly when asking is cheapest.
- **Hard-error on a stale `server:`** — rejected per decision 4: the config is stale, not corrupt,
  and the picker is a keystroke.
- **Make `server:` an enum over the configured names** — rejected: registry `EnumValues` are static
  and guard-pinned to parse sites, and a config-dependent vocabulary would either break that guard
  or invent a second kind of enum. Selection-time validation already exists and already has a
  defined failure behaviour.
- **Keep `--endpoint` / `APOGEE_ENDPOINT` as registry rows over a hidden key** — rejected: registry
  rows describe config-file keys and the bijection guard pins them to `fileConfig` tags; a row whose
  file half no longer exists would either weaken the guard or lie about the schema. The override
  resolver owns them, with its own binding test.
- **Persist the ephemeral override entry into `servers:`** (learn the endpoints you were pointed at)
  — rejected: an override is a statement about this run, its key may come from an environment the
  user does not want written down, and writing it would make `--endpoint` a config editor.
- **Refuse legacy configs with the paste-able error only, no auto-rewrite** — rejected as the
  default while kept as the fallback: the schema change is apogee's, not the user's, and handing
  every existing installation a manual edit to keep working is a cost this project imposed. The
  refusal path survives for exactly the cases where the rewrite cannot prove itself (decision 9).
- **Migrate by unmarshal → mutate → re-marshal** — rejected for the reason `configwrite.go` records
  and ADR 0035 restated: it hands the user back a file with a couple of settings in it, having
  silently deleted every word of documentation they wrote.
- **Tolerate the legacy keys silently for a release** (ignore unknown keys, let the list win) —
  rejected: a plain unmarshal already ignores them, which is precisely the trap — the old
  `endpoint:` would vanish without a word and the session would start somewhere the file never
  said. Silence is the one behaviour this change cannot afford.
- **A structured server add-form in the settings pane for the empty-list case** — rejected for
  scope, as ADR 0035 D3 rejected it: a list editor is its own design problem (ordering, deletion,
  nested validation) and belongs to its own plan.
- **A live `servers:` rescan mid-session** so an added entry appears without restart — rejected for
  scope: the resolved list is wire-time state, and a second load path would need its own answer for
  the entry you are currently bound to.
