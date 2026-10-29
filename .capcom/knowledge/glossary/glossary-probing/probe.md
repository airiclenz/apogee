---
title: "Probe (`apogee probe`): the diagnosis command for what this machine and model can do"
---

**Probe** (`apogee probe`):
The **diagnosis command** — what this machine and this model can actually do, answered **without
running an agent**. Two halves with deliberately asymmetric cost
(ADR 0021):
the **host report** (`apogee probe`, and `apogee probe host` for scripts) is free, offline and
read-only — OS/arch, the Confiner backend and its capability matrix, the `AutoEligible()` verdict,
the effective `confine-to-workspace` after the Host acknowledgement,
workspace root, config home, endpoint reachability and model discovery — *reporting* ADR 0012's
verdict, never re-deciding it, and sharing its logic with `/confine status` so the two cannot
drift. The **model battery** (`apogee probe model`) is an **explicit act**: it spends live model
calls (native tool call, JSON/structured output, multi-step tool chain) and **writes** a probe
record, so it never fires as a side effect of the bare noun. It prints suggested **model
profile** knobs as paste-ready YAML and never edits the user's config; `--no-save` runs the
battery and writes nothing. Three more children sit on the free side of that split, read-only like
the host report: `apogee probe terminal` (measures the terminal by painting on the alternate screen,
so it must be asked for), `apogee probe config` (what config.yaml says: notices, a pending migration
and every key's resolved value, never migrating the file) and `apogee probe context` (what apogee
itself puts in front of the model at Turn 1, ADR 0079, with no request sent).
_Avoid_: "benchmark" (the battery measures capability, not quality — scoring is the
bench's job), "health check" (it diagnoses, it does not monitor —
monitoring is the Heartbeat's job), "auto-configure" (nothing is
written into config).
