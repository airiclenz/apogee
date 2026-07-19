# Validated-set runtime surface — implementation plan

**Date:** 2026-07-19. Design fixed by the grill recorded in ADR 0016's
"Runtime-surface realisation (2026-07-19)" section; glossary semantics in CONTEXT.md
("Validated set"). This plan is the implementation detail those documents defer.

## Decisions this plan implements (summary)

1. **Confidence-graded application.** Direct label match: auto-apply at ≥ medium
   fingerprint confidence (per ADR 0016 §5); **offer** at low via the per-session notice
   naming the one-line alias to paste. Aliased match: apply at any confidence (the human
   decision replaces the gate). No match: silence (the D1 floor needs no banner).
2. **Whole-set-or-nothing.** A non-empty `mechanisms:` config block = manual control —
   the set is not applied and one notice says so (uniform for would-be applies AND
   offers). Bypass suppresses everything, notices included. A defective entry is
   skipped with a warning, never partially applied.
3. **Placement: `cmd/apogee` wire time.** The engine, `Config`, and the bench are
   untouched; the applying set folds into `cfg.EnableMechanisms` (ADR 0015's single
   enable path). Notices are pre-TUI stderr lines (the unconfined-Auto precedent).
4. **Config surface** (file-only, like `mechanisms:`):

   ```yaml
   validated-sets:
     enable: true        # off-switch (*bool, default on)
     alias:              # runtime fingerprint label -> entry key
       gemma-4-e4b-it-qat: gemma-4-e4b-it-qat   # identity = low-confidence confirm
       gemma4:latest: gemma-4-e4b-it-qat        # transfer = ADR 0016 §3 carry-over
   ```

   A dangling alias (no such entry key) is a **loud** startup error (ADR 0015's
   removed-ID posture — it is the user's own config). Everything else is **soft**.
5. **Storage: one JSON schema, two sources.** Shipped = one embedded JSON
   (`//go:embed`, array of entries) pinned by unit tests; user-local =
   `~/.apogee/validated/*.json`, one entry per file (drop-in write model for future
   validation tooling). User-local wins a key collision with shipped; within the user
   dir, files are read in sorted order, later wins, with a warning.

## Entry schema (version 1)

```json
{
  "version": 1,
  "key": "gemma-4-e4b-it-qat",
  "set": ["autofix", "cached_content_intercept", "..."],
  "evidence": {
    "campaign": "gemma-4-e4b-it-qat-20260714-minus-truncate-history",
    "note": "NI within fresh delta=0.4643, W+=102.0, p=0.0003, N=14, engagement verified"
  },
  "entered": "2026-07-19"
}
```

Defects that soft-skip an entry (one stderr line naming file/key + reason): malformed
JSON, version > 1, empty key, empty set, unknown Mechanism ID, set invalid under the
current descriptor relations (a `Requires` not inside the set, an `IncompatibleWith`
pair inside it — the checks `New()` would otherwise fail loudly on).

## Items

- **I1 — `internal/validated` (new package; imports `internal/domain` only).**
  `Entry` (+ `Source` field stamped at load: `shipped` / `user`), versioned decode,
  `Shipped()` from the embedded JSON, `LoadUserDir(dir)` (missing dir fine; per-file
  soft-skip with collected warnings), `Merge` (user wins), `Validate(entry,
  []domain.MechanismDescriptor)` (whole-set stacking check), `Match(label, confidence,
  alias, entries)` → `Decision{Kind: Applied|Offered|None, Entry, ViaAlias}` with a
  typed dangling-alias error. Descriptors are a parameter — the package stays
  mechanism-catalogue-agnostic; `cmd` passes `mechanisms.Descriptors()`.
- **I2 — shipped entry.** `internal/validated/shipped.json`: the gemma-4-e4b-it-qat
  pruned 16, verbatim from the catalogue table (recorded from the Probe manifest —
  not derivable from the catalogue alone).
- **I3 — config.** `fileConfig.ValidatedSets *validatedSetsConfig`
  (`enable *bool`, `alias map[string]string`), threaded through `layer`/`settings`/
  `options` file-only; default enable=true, alias=nil. Commented block in
  `cmd/apogee/defaults/config.yaml`.
- **I4 — wire.** `resolveRoots` gains `roots.validated = <home>/validated`. In
  `runRoot`, after `mechanismIDs(...)`: skip when `opts.bypass` or `enable: false` or
  the fingerprint is zero; else load + merge (warnings → stderr), `Match` on
  `library.ResolveFingerprint(opts.model)` (post-discovery, so the label is the served
  model id for a no-model config), then: manual control (`len(opts.mechanisms) > 0`) →
  suppressed notice; Applied → `Validate`, fold sorted IDs into
  `cfg.EnableMechanisms`, applied notice; Offered → offer notice with paste-ready
  YAML. Notice builders are pure funcs (the `contextWindowNotice` pattern) for table
  tests.
- **I5 — notices** (exact wording):
  - applied: `apogee: Validated set for <key> applied — <N> mechanisms on (campaign
    <campaign>; <source>). Turn off with validated-sets: enable: false.` (aliased:
    `… applied via alias <label> -> <key> — …`)
  - offered: `apogee: a Validated set exists for '<key>' but the model identity is
    name-only (low confidence). To apply it, add to ~/.apogee/config.yaml:` + indented
    `validated-sets: / alias: / "<label>": "<key>"` block.
  - suppressed: `apogee: a Validated set matches <key> but your explicit mechanisms:
    config takes precedence; set not applied.`
  - skipped entry: `apogee: skipping validated-set entry <where>: <reason>`
- **I6 — tests.** Package: decode/version/merge/match/validate tables; shipped pins
  (decodes, unique keys, every ID ∈ `mechanisms.KnownIDs()`, whole set passes
  `Validate` against `mechanisms.Descriptors()` — the CI drift guard; gemma set == the
  catalogue's 16 exactly). cmd: config-block parsing (absent / present / `enable:
  false` / alias), notice builders, match-fold behaviour incl. manual-control and
  bypass suppression.
- **I7 — docs.** Catalogue "Validated sets" intro: surface now built, shipped JSON
  mirrors the table (dual-maintenance: table row + shipped.json + the pin test).
  CHANGELOG entry.

## Non-goals (explicit)

- No engine/`Config` change, no public embedder API (deferred until asked for).
- No behavioral-probe (medium-confidence) resolver — D8 stands.
- No writer for `~/.apogee/validated/` — user-run validation tooling is future work
  (the ADR's product-prerequisite note on the engagement guard).
- No TUI in-transcript banner in v1 (revisit if the stderr line proves easy to miss).
