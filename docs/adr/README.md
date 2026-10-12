# The decision records have moved

apogee's decision records now live in capcom Knowledge as the `decisions` Note tree:
one `adr-NNNN` Note per record under `.capcom/knowledge/decisions/` (root Note: [decisions.md](../../.capcom/knowledge/decisions.md)),
each of its `##` sections a child Note (`adr-NNNN-context`, `adr-NNNN-decision`, …).

- Read a record: `capcom know show "ADR NNNN"` (for example `capcom know show "ADR 0076"`).
- A new record takes the next number (`adr-0099`) in capcom's decision-record shape — a record
  Note plus one child per section: `capcom know add --id adr-NNNN --parent decisions`.
