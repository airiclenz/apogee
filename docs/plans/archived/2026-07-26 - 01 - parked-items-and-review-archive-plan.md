# Plan — Resolve the 2026-07-24 review's two parked items, then archive the review

**Date:** 2026-07-26
**Status:** complete
**Source:** the two items the (now otherwise empty) ledger of
`docs/reviews/2026-07-24 - 00 - architecture-deepening-review.md` deliberately parks: the
`/code-audit` on the **live** url-safety gap (card 02's own note — the 2026-07-25 funnel fixed
the *shape*; the hole itself was never independently confirmed closed) and the un-grilled
`Request.InjectContext` placement question (card at L435–438: *Speculative — reopens an
ADR-0010 line … not recommended without a grill; the current placement is defensible*). The
review is the last live doc in `docs/reviews/` and cannot honestly move to `archived/` while it
is the only record of open work — `docs/reviews/archived/`'s rule is that nothing in it is
anyone's to-do list.
**Track:** post-`v0.8.0` architecture deepening — final housekeeping of the 2026-07-24 cycle.
**Public API:** none. No Go file changes anywhere in this plan — items 1–3 produce one new
review doc, one `TODO.md` entry, and two `git mv`s. (If item 1's audit *finds* defects, fixing
them is explicitly follow-on work the owner dispatches per finding — see D1 and Non-goals.)
**Standing requirement:** invoke `implement-plan` with **`coding-standards` and `code-audit`**
forwarded — item 1's implementer must follow the code-audit skill. Pre-production: commit
direct to `main`, no PRs (owner directive; overrides the version-control standard's
no-direct-to-main rule).

Per-item green gate (docs-only plan — the gate must stay trivially green, proving no code
was touched):

```
gofmt -l .                                              # empty
go vet ./... && go test ./... && go test -race ./...
GOOS=windows go build ./... && GOOS=darwin go build ./...
```

**Items 1 and 2 are independent; item 3 depends on both** (it archives the review only once
neither parked item lives solely inside it).

---

## Context

Everything else from the 2026-07-24 review is landed and recorded (see the review's dated
close and `docs/plans/archived/2026-07-26 - 00 - land-item8-close-size-window-plan.md`).
What is left is exactly the two parked entries, and they are of different kinds:

- **The url-safety audit is runnable work with a concrete deliverable** (a report). Card 02
  landed the *funnel* variant on 2026-07-25 (`networkTool` embedded by the three built-ins,
  unexported url-filter marker, `classNetwork` vs `classThirdPartyNetwork` ladder split, ADR
  0012 amendment 2026-07-25 + `confinement-execution-contract.md` §4) — "url-filtered" is now
  true *by construction*, but whether the filter itself closes the live hole was never
  independently audited. The funnel means there is now one place to look.
- **The `InjectContext` question is not runnable work** — it is a design decision the review
  explicitly bars without an owner grill, and a grill is an interactive session no plan
  sub-agent can hold. The honest disposition is a proper parking: a `TODO.md` entry carrying
  enough design that a later grill session does not re-derive it (that is `TODO.md`'s stated
  purpose).

---

## Decisions

- **D1 — the audit runs as a plan item; its findings do not get fixed in this plan.** Rejected:
  re-parking the audit into `TODO.md` — running it is the review's own recommendation and
  empties the parked list for real instead of moving it. The item's deliverable is the
  **report only**, in the report-findings-don't-fix discipline: every confirmed finding is a
  separate owner decision (fix now via a new plan, accept, or defer), surfaced at closeout —
  never silently spawned into fixes here.
- **D2 — `InjectContext` is parked, not decided.** Any code or ADR change here is out of
  scope by the review's own bar. The `TODO.md` entry is a grill brief; the grill itself is a
  future owner session (`grill-with-docs`).
- **D3 — the review archives whole: `.md` and `.html` together, unregenerated.** The `.html`
  is a dated render of the original review session and moves as-is (the ledger annotations
  live in the `.md` only — same asymmetry the archived 2026-06/07 reviews already show).
- **D4 — the audit report lives in `docs/reviews/`, not `archived/`.** It is the new live
  actionable doc until its findings are dispatched; archiving it is whoever dispatches them.
- **D5 — item 2 writes the review's *archived* path.** Item 2 runs before item 3 moves the
  file, so its `TODO.md` pointer (`docs/reviews/archived/2026-07-24 - 00 - …`) dangles for
  the minutes between the two commits. Accepted: the alternative (live path now, item 3
  re-edits `TODO.md`) spreads one fact over two commits; item 3's acceptance instead
  confirms the pointer resolves.

---

## Explicit non-goals

- **Fixing anything the audit finds** (D1) — each confirmed finding is reported to the owner
  at closeout and dispatched separately.
- **Taking the `InjectContext` decision**, amending ADR 0010, or moving any code (D2).
- **Regenerating the review's `.html`** (D3).
- **`VERSION`** — the outstanding minor-bump owner call recorded in the archived close-out
  plan is untouched by a docs-only plan.
- **The two remaining nit-grade doc findings** recorded in the archived 2026-07-25 close-out
  plan (`toolsummary.go`'s `Total` comment; `workspace_scoped_test.go`'s notes) — they stay
  on that record.

---

## 1. Run the `/code-audit` on the live url-safety gap — ✅ DONE (2026-07-26)

NOTES (2026-07-26): the audit's root-cause finding lands in an in-scope file
(`internal/agent/resolution.go`'s `classifyTool` — the `classNetwork`/`classThirdPartyNetwork`
seam this item names), but three of its live consequences sit in files **outside** the item's
literal scope list (`internal/tools/git.go`, `diagnostics.go`, `present_document.go` +
`internal/present/opener.go`), and one adjacent finding is in `internal/mcp`. They are reported
rather than dropped — the item's own "classic bypass classes are in scope wherever the code
actually makes them possible" — and every such finding is tagged in the report as outside the
audited path, so the owner can dispatch or dismiss it separately per D1. No file outside
`docs/reviews/` was changed.

**What:** following the forwarded **code-audit** skill, audit the url-safety enforcement
path — does the filter actually close the hole the Auto ladder's "url-filtered" promise
assumes closed? Scope (card 02's file list plus what the funnel landed):
`internal/security/urlsafety.go`; the `networkTool` funnel and its unexported url-filter
marker in `internal/tools`; the three built-ins that embed it (`web_fetch.go`,
`http_request.go`, `web_search.go`); the `classNetwork` / `classThirdPartyNetwork` seams in
`internal/agent/resolution.go` and `dispatch.go`. The authoritative sources the audit checks
conduct against: **ADR 0012 (amendment 2026-07-25)** and
`docs/design/confinement-execution-contract.md` **§4**. The audit judges the *enforcement*,
not the shape (the shape landed and is pinned by its own plan): classic bypass classes are in
scope wherever the code actually makes them possible — but the skill's bar holds: no
speculative findings, every finding carries `file:line` evidence and a concrete failure
scenario, noise aggressively filtered.

**Deliverable:** `docs/reviews/archived/2026-07-26 - 00 - url-safety-live-gap-audit.md` — the
code-audit skill's report format, opening with a one-paragraph verdict (hole confirmed
closed / findings enumerated), citing the review card it discharges by its archived path
(`docs/reviews/archived/2026-07-24 - 00 - …`, per D5).

**Tests:** none — **no Go file may change**; the gate proves it.

**Acceptance:** gate green; the report file exists and `git status --porcelain` shows only
it (plus plan-file marker edits); the report names ADR 0012 and contract §4 as the conduct
sources and gives every finding `file:line` evidence — or states the closed verdict against
them. Commit: `docs(reviews): audit the live url-safety gap`.

---

## 2. Park `Request.InjectContext` in `TODO.md` as a grill brief — ✅ DONE (2026-07-26)

**What:** a new `TODO.md` section in the file's existing entry style (H2 title, `**Status:**
parked 2026-07-26`, then the design substance). It must record enough that a later grill
session re-derives nothing: the question (a `domain` data type encodes chat-template
role-safety policy, while the engine/`context` layer owns role-alternation — which layer
should own the placement?); what it reopens (an ADR-0010 public-surface line); the review's
verdict verbatim in substance (*Speculative*; **the current placement is defensible**; any
change requires an owner grill first — `grill-with-docs`); and the pointer to the full card
at `docs/reviews/archived/2026-07-24 - 00 - architecture-deepening-review.md` (archived path,
D5).

**Tests:** none — docs only; gate proves it.

**Acceptance:** gate green; `grep -n "InjectContext" TODO.md` hits the new section and
`grep -n "grill" TODO.md` hits inside it; no file besides `TODO.md` (plus plan-file marker
edits) changed. Commit: `docs(todo): park Request.InjectContext with its grill brief`.

---

## 3. Archive the 2026-07-24 review — ✅ DONE (2026-07-26)

NOTES (2026-07-26): three judgment calls beyond the item's literal text. (a) The dispositions were
written into **four** places, not just the dated close: the header ledger line (L34–36), the dated
close under *Recommended next step*, **candidate 02's card note** ("the separate `/code-audit` … is
still worth running") and the **`InjectContext` smaller-deepening card**, plus the *Suggested
skills* `/code-audit` bullet — each of those named a parked item as open, and whole-plan
verification 2 requires the archived doc to name no open item without its disposition. (b) The
review's own **companion-artifact line** (L7, pointing at its `.html`) was repointed to the
archived path so it still resolves after the `git mv`; the `.html` itself is untouched (D3).
(c) The only surviving un-archived-path spellings outside `archived/` are in **this plan file** —
its `Source:` line (a provenance record, exactly like the archived plans' own `Source:` lines) and
the two places that quote the grep pattern itself, which cannot be rewritten. This file lands in
`docs/plans/archived/` at whole-plan verification step 4, after which the acceptance grep is
literally clean.

Depends on items 1 and 2 — the review archives only once neither parked item lives solely
inside it.

**What:** in the review's dated close ("Recommended next step" / the parked-items lines),
replace "the only parked items are …" with the dispositions: the url-safety audit **ran** —
name item 1's report path and its verdict in one line; `InjectContext` is **parked in
`TODO.md`** with its grill brief. Then `git mv` both files into `docs/reviews/archived/`
(`.md` and `.html`, unregenerated — D3). Repoint any **live** repo references to the
un-archived path (`grep -rn "docs/reviews/2026-07-24" --include="*.md" .` — references
*inside* `docs/plans/archived/` and `docs/reviews/archived/` are historical records and stay;
the item 1 report and `TODO.md` already carry the archived path per D5).

**Tests:** none — docs only; gate proves it.

**Acceptance:** gate green; `docs/reviews/` contains exactly `archived/` and item 1's report;
both review files exist under `docs/reviews/archived/`; the D5 pointers in `TODO.md` and the
report now resolve; `grep -rn "docs/reviews/2026-07-24" --include="*.md" .` returns only
archived-path spellings or hits inside `archived/` directories. Commit:
`docs(reviews): the 2026-07-24 review is archived`.

---

## Whole-plan verification (run after item 3, before declaring done)

1. **Full gate** — trivially green, and `git log --stat` for the three commits shows **no
   `.go` file** anywhere in this plan.
2. **The parked list is really empty:** the review under `archived/` no longer names any open
   item without naming its disposition; `TODO.md` carries the grill brief; the audit report
   exists in `docs/reviews/`.
3. **Surface the audit outcome straight:** the closeout report to the owner leads with item
   1's verdict — a closed-verdict line, or the confirmed findings list verbatim, each awaiting
   an owner decision (fix now / accept / defer). Findings must not be buried in the ledger.
4. Archive **this** plan under `docs/plans/archived/`. Commit:
   `docs(plans): archive the parked-items plan`.

## Manual verification (owner — the automated suite cannot do this)

Read the audit report and decide per finding (fix now via a new plan / accept / defer).
Schedule the `InjectContext` grill session (`grill-with-docs`) whenever the question becomes
worth deciding — the brief in `TODO.md` is written so nothing needs re-deriving first.
