---
title: Confirm that served PDFs render in Chrome, Firefox and Safari under the PDF policy
status: open
type: task
priority: 2
created: 2026-11-10T19:36:31Z
---

Source: docs/reviews/code-audit-2026-11-10.md ("The document CSP may stop browsers rendering served PDFs (uncertain)"). Verified PARTLY (uncertain; needs a browser check) 2026-11-10 at a5c4f2db. Narrowed 2026-11-11 by the commit `fix(present): serve PDFs under a policy without the bare sandbox` (plan `2026-11-11 - 01 - fence-escape-and-open-defects-plan`, item 3).

**Where:** `internal/present/server.go` — `pdfCSP`, chosen by `documentPolicy` for `application/pdf`; every other type keeps the bare-sandbox `documentCSP`.

**What is already done:** rung 2 (the doc server) now serves `.pdf` with `Content-Security-Policy: frame-ancestors 'none'; form-action 'none'; base-uri 'none'` — no `sandbox`, no `default-src` — beside `nosniff`, on the strength of Chromium's record of blank PDFs under a CSP `sandbox` (crbug 413851; crbug 1187122). `.html`, `.htm` and `.svg` keep the bare sandbox. The reasoning is ADR 0019's amendment of 2026-11-11 (`capcom know show adr-0019-amend-1111`), and `server_test.go` pins the per-type policy.

**What is left:** the change was never confirmed in a browser. Serve a test PDF over rung 2 (an SSH session, so the doc server is the rung that answers) and open the printed URL in current Chrome (or Edge), Firefox and Safari.

**Why it matters:** `.pdf` is the one format both rungs show, and on an SSH devbox rung 2 is the only one that reaches the user — a blank page there is invisible to apogee.

## Acceptance

- The result of opening a served PDF in current Chrome, Firefox and Safari is recorded as a dated note on ADR 0019 (or its 2026-11-11 amendment).
- If any browser still renders it blank, the PDF policy is changed until it renders, and `TestDocServerServesAPDFWithoutTheBareSandbox` follows.
- The active formats (.html/.htm/.svg) keep the bare sandbox.
