---
id: audit
displayName: Audit
summary: Audit code for the issues that matter — split the scope to fit, review it through five lenses, verify every claim, write one report.
description: >-
  Use when you want a senior-engineer-level audit of a package, a folder or the whole
  workspace: real bugs with realistic triggers, security holes, drift from the project's own
  rules, dead or duplicated code, and missing tests on critical paths. A recipe — the engine
  runs it as a workflow, each piece of work in a fresh child — so the scope is never read into
  one context. Noise is filtered and every claim that reaches the report was checked by an
  adversarial verifier first.
inputs:
  - name: scope
    required: true
    description: the files or folders to audit, workspace-relative — several in quotes, or . for the whole workspace
  - name: focus
    description: all, bugs, security, tests or standards — asked when left out
recipe:
  - name: split
    kind: script
    run: "sh {{SKILL_DIR}}/split.sh {workflow_dir} {scope} {focus} {part_bytes}"
    returns:
      files: int
      src: int
      tests: int
      parts: int
      groups: int
      conc_parts: int
      part_lines: int
      focus: "none|all|bugs|security|tests|standards"
  - name: focus
    kind: ask
    when: "split.focus == none"
    question: "Which areas should this audit cover? all is a full audit; bugs includes concurrency where the code warrants it; standards covers the project's own rules, drift and dead code."
    options: [all, bugs, security, tests, standards]
    default: all
  - name: run-dir
    kind: pick
    when: "split.status == ok"
    file: run-dir.txt
  - name: ground-truth
    kind: fanout
    over:
      stage: run-dir
    prompt: prompts/ground-truth.md
    out: "{item}/bundle.md"
  - name: machine-checks
    kind: fanout
    over:
      stage: run-dir
    prompt: prompts/machine-checks.md
    out: "{item}/tools.md"
  - name: flags
    kind: script
    when: "split.status == ok"
    run: "sh {{SKILL_DIR}}/split.sh --flags {workflow_dir}"
    returns:
      concurrency: "yes|no"
  - name: parts
    kind: pick
    when: "split.status == ok"
    file: parts.txt
  - name: lens-intent
    kind: fanout
    when: "split.focus == all or split.focus == standards or focus.answer == all or focus.answer == standards"
    over:
      stage: parts
    prompt: prompts/lens-intent.md
    out: "{item}/findings-intent.md"
    returns:
      critical: int
      high: int
      medium: int
  - name: lens-correctness
    kind: fanout
    when: "split.focus == all or split.focus == bugs or focus.answer == all or focus.answer == bugs"
    over:
      stage: parts
    prompt: prompts/lens-correctness.md
    out: "{item}/findings-correctness.md"
    returns:
      critical: int
      high: int
      medium: int
  - name: lens-security
    kind: fanout
    when: "split.focus == all or split.focus == security or focus.answer == all or focus.answer == security"
    over:
      stage: parts
    prompt: prompts/lens-security.md
    out: "{item}/findings-security.md"
    returns:
      critical: int
      high: int
      medium: int
  - name: lens-tests
    kind: fanout
    when: "split.focus == all or split.focus == tests or focus.answer == all or focus.answer == tests"
    over:
      stage: parts
    prompt: prompts/lens-tests.md
    out: "{item}/findings-tests.md"
    returns:
      critical: int
      high: int
      medium: int
  - name: conc-parts
    kind: pick
    when: "flags.concurrency == yes and split.conc_parts > 0"
    file: conc-parts.txt
  - name: lens-concurrency
    kind: fanout
    when: "split.focus == all or split.focus == bugs or focus.answer == all or focus.answer == bugs"
    over:
      stage: conc-parts
    prompt: prompts/lens-concurrency.md
    out: "{item}/findings-concurrency.md"
    returns:
      critical: int
      high: int
      medium: int
  - name: groups
    kind: pick
    when: "split.status == ok"
    file: groups.txt
  - name: rollup
    kind: fanout
    when: "split.parts > 1"
    over:
      stage: groups
    prompt: prompts/rollup.md
    out: "{item}/merged.md"
    returns:
      critical: int
      high: int
      medium: int
  - name: enumerate
    kind: fanout
    over:
      stage: groups
    prompt: prompts/enumerate.md
    out: "{item}/claims.md"
    returns:
      claims: list
  - name: claims
    kind: pick
    from: enumerate
    field: claims
    cap: 60
  - name: verify
    kind: fanout
    over:
      stage: claims
    prompt: prompts/verify.md
    returns:
      verdict: "confirmed|refuted|unclear"
  - name: report
    kind: merge
    from: verify
    prompt: prompts/report.md
---

# Audit

A high-signal code audit, run as a **recipe**: the engine runs the stages below as a workflow,
each piece of work in a fresh child with a context of its own, and hands back one line per item
plus the path of the report. Nobody — not you, not any one child — reads the whole scope.

Start it with `/audit <scope> [focus]`, for example `/audit internal/` or
`/audit "cmd/ internal/" focus=security`. The scope is one or more workspace-relative files or
folders (`.` for the whole workspace); the focus is `all`, `bugs`, `security`, `tests` or
`standards`, and the audit asks for it when you leave it out.

## What it looks for

Issues that matter: security holes, drift from the project's own stated rules, code that
contradicts its own purpose, real bugs with a realistic trigger, dead or duplicated systems, and
missing tests on critical paths. A review with 200 findings is a review nobody reads; eight sharp,
verified findings get fixed. Style, spelling and speculation stay out of the report.

Scope is source only — the code, its build manifests and the config it depends on. Docs, images,
lockfiles, changelogs, vendored or generated code, fixtures and build output are never audited.

## The stages

1. **split** — the split script (`{{SKILL_DIR}}/split.sh`, readable for reference) lists the
   source files under the scope and cuts them into parts sized to a child's context window:
   one part when the scope fits, otherwise parts by folder, each under its line and file bounds,
   with the test files filed next to the part they test and the parts gathered into rollup
   groups. Its receipt counts the files, parts and groups and says whether the focus was given.
2. **focus** — asked only when the focus was left out; the default is `all`.
3. **ground-truth** and **machine-checks** — one child reads the project's own documents and
   distils its mission, invariants, standards and threat model into the bundle every later
   child reads, noting whether the code uses concurrency; another runs the project's own
   analysis tools (linters, vulnerability scan, race detector, coverage) and distils their
   output. A tool-confirmed finding is already proven.
4. **the lenses** — one child per part and lens, each lens gated by the focus:
   intent (standards), correctness (bugs), security, tests (with the part's test files), and
   concurrency (bugs), which runs only over the parts that hold a concurrency primitive and only
   when the ground truth says the code uses concurrency. Each writes its findings next to its
   part.
5. **rollup** — on a scope of several parts, one child per group folds its parts' findings into
   one deduplicated file.
6. **enumerate** — one child per group picks the claims worth verifying: every Critical, and the
   Highs and uncertain Mediums up to the group's cap.
7. **claims** and **verify** — the picked claims, at most sixty, each go to an adversarial child
   whose job is to refute it: confirmed, refuted or unclear.
8. **report** — one child deduplicates across lenses, drops what was refuted and the noise,
   keeps about twenty findings, and writes the report the workflow's `report:` line names.

Each stage's brief is the `prompt:` file the recipe names beside it. Everything a run writes stays
in its workflow folder, so a run interrupted part-way resumes where it stopped when started again with the same
scope and focus.

## When a recipe cannot run

Where no workflow can run, audit by hand in the same order: settle the scope, read the project's
own rules first, review one lens at a time over a slice small enough to read whole, then try to
refute every significant finding before you report it.
