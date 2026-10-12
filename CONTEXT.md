# The glossary has moved

apogee's domain glossary now lives in capcom Knowledge as the `glossary` Note tree,
one plain-markdown Note per term under `.capcom/knowledge/glossary/` (root Note: [glossary.md](.capcom/knowledge/glossary.md)).

- Read a term: `capcom know show "<Term>"`; list the sections: `capcom know tree glossary --depth 1`.
- Edit a term in place in its Note file; `capcom check` and `capcom fmt --check` must stay clean.
- Code comments cite a term as `CONTEXT: <Term>`; `capcom know show "<Term>"` resolves that citation.
