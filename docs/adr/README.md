# The decision records have moved

apogee's decision records now live in capcom Knowledge as the `decisions` Note tree:
one `adr-NNNN` Note per record under `.capcom/knowledge/decisions/` (root Note: [decisions.md](../../.capcom/knowledge/decisions.md)),
each of its `##` sections a child Note (`adr-NNNN-context`, `adr-NNNN-decision`, …).

- Read a record: `capcom know show "ADR NNNN"` (for example `capcom know show "ADR 0076"`).
- A new record takes the next number (`adr-0105` on 2026-10-29; `capcom know tree decisions --depth 1` shows the last one) in capcom's decision-record shape — a record
  Note plus one child per section: `capcom know add --id adr-NNNN --parent decisions`.

capcom's own documentation cites *capcom's* ADR 0010 for this shape; inside this checkout `capcom know show "ADR 0010"` returns apogee's ADR 0010 (package layout), so never cite apogee's ADR 0010 for the record shape. (2026-10-12, docs/plans/archived/2026-10-12 - 01 - capcom-knowledge-migration-plan.md)
