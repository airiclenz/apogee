---
title: "ADR 0019 amendment of 2026-07-26: the allow-list refuses the pre-2007 office formats, and `.csv` is ruled in"
---

**Why now.** Raised by the url-safety plan's item 2 verifier as residual risk (carried forward as
that plan's item 20): the first amendment's rule — the formats whose default handler **displays**
the file rather than executing it — and the shipped set disagreed. The set admitted `.doc`, `.xls`
and `.ppt`, the pre-2007 binary office formats, whose single container carries macros the handler
offers to run on one *Enable Content* click. The distinction the rule draws is `.docx` vs `.docm` —
OOXML split the macro-free formats from the macro-carrying ones precisely so the extension states
which one it is — and the legacy trio, which has **no macro-free variant**, sits on the `.docm`
side of that line. The available moves were to narrow the set or to soften the rule into "macros
are not auto-run, one click away, an accepted risk"; the **set moves and the rule stands**, because
the rule as stated is the defensible one and the cost of narrowing is close to nil.

**(a) `.doc`, `.xls` and `.ppt` are out.** A deliverable in a pre-2007 format degrades exactly as
any refused extension does (the first amendment's (a)): no argv at all, `ErrNoOpener`, rung 0 — the
path still presented, the tool result still `shown`, never an error. The user-facing cost is
negligible: a coding agent's office deliverables arrive in the post-2007 formats, which stay in the
set.

**(b) `.csv` stays in, ruled explicitly rather than waved through.** A CSV is plain text with no
container for code — there is nothing in the file its handler can be asked to *run*, which is the
rule's own line. The residual surface (spreadsheet formula / DDE injection) exists only when the
default handler happens to be a spreadsheet, and even there nothing reaches the OS without the user
clicking through that application's own security prompts — DDE launch has shipped default-off or
prompted in both Excel and LibreOffice since the 2017 mitigations. Set against that, `.csv` is a
format a coding agent's deliverables genuinely arrive in, which `.doc` and `.ppt` are not.

**(c) Nothing else moves.** Rung 2's four browser extensions were never in the removed trio, so the
subset invariant holds unchanged; rung 3 stays unbounded (the first amendment's (c)); the Windows
name bound (the second amendment) is untouched.
