---
title: Confirm that served PDFs render in Chrome and Firefox under the document CSP's bare sandbox
status: open
type: task
priority: 2
created: 2026-11-10T19:36:31Z
---

Source: docs/reviews/code-audit-2026-11-10.md ("The document CSP may stop browsers rendering served PDFs (uncertain)"). Verified PARTLY (uncertain; needs a browser check) 2026-11-10 at a5c4f2db.

**Where:** `internal/present/server.go` `documentCSP` (:81), applied to every served document at :372, `.pdf` included.

**What:** rung 2 (the doc server) serves `.pdf` with `default-src 'none'; …; frame-ancestors 'none'; sandbox`. The policy exists for active formats (.html/.htm/.svg, ADR 0019 amendment 2026-08-12); its effect on built-in PDF viewers was never checked, and `server_test.go:242-250` checks only header text and content type.

**Why it matters:** Chromium has a history of refusing to render a PDF whose response carries a CSP `sandbox` (crbug 413851 "Sandbox breaks PDF rendering"; crbug 1187122, reported fixed in M91 but seen again on Chrome 108; Atlassian KB on blank Jira PDF attachments in Chrome). `default-src 'none'` also implies `object-src 'none'`. `.pdf` is the one format both rungs show, and on an SSH devbox rung 2 is the only one that reaches the user — a blank page there is invisible to apogee.

**Suggested fix:** serve a test PDF and open it in current Chrome/Edge, Firefox and Safari. If any fails, give non-active types such as `application/pdf` a policy without `sandbox` (and without an `object-src` restriction); a PDF cannot carry the meta refresh `sandbox` exists to stop, and `nosniff` pins the type. Record the result as a dated ADR 0019 amendment.

## Acceptance

- The result of opening a served PDF in current Chrome, Firefox and Safari is recorded (ADR 0019 amendment or design note).
- If any browser fails, .pdf is served with a policy it renders under, and a server test pins the per-type policy.
- The active formats (.html/.htm/.svg) keep the bare sandbox.
