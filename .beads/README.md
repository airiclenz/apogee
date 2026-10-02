# .beads — apogee's issue register

apogee tracks open defects and parked work in [beads](https://github.com/steveyegge/beads) (`bd`), not in a
markdown file. The rules for using it here live in [`AGENTS.md`](../AGENTS.md); this note only points at them.

- **bd usage** — `bd ready`, `bd show <id>`, `bd create`; a bead a plan owns is labelled `planned`, never
  re-statused. See *Where knowledge lives* and *Conventions not derivable from the code* in `AGENTS.md`.
- **Spoken ids** — every new bead is created with `bd create --id apogee-<slug>`; ids are permanent, and the
  older hash ids (`apogee-qi3`) stay as they are.
- **`issues.jsonl`** — `.beads/issues.jsonl` is a passive export, committed so a clone without `bd` can read the
  register. The local Dolt DB is the source of truth; the export can lag it after a batch write, so re-export
  with `bd export -o .beads/issues.jsonl` when it disagrees with `bd list`.
- **Hooks** — `.beads/hooks/` is the live git hooks path once `bd hooks install` has run; read `AGENTS.md`
  before installing them in a checkout you do not trust.
