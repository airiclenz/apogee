---
title: "Windows CI: golden files checked out with CRLF fail byte-identical goldens (contextcost.golden)"
status: done
type: bug
priority: 1
plan: 2026-09-17 - 02 - ci-flake-causes-plan
aliases: [apogee-80n]
created: 2026-09-17T10:46:21Z
closed: 2026-09-19T16:00:09Z
reason: "Implemented by docs/plans/archived/2026-09-17 - 02 - ci-flake-causes-plan.md (items ✅ DONE 2026-09-17); CHANGELOG [Unreleased] carries the entries. Closed late: bd had no database on this host at closeout."
---

Since 91d343bf (2026-09-17) every 'windows tests' job fails TestContextCostReportGolden / TestContextCostReportMeasuredGolden in internal/probe with a want/got diff that is byte-identical on screen. Cause: the windows-latest runner checks out with core.autocrlf=true and .gitattributes only pins internal/tui/logo.txt and graphics/apogee-logo.md, so testdata/*.golden arrive with CRLF while tuitest.compareGolden (internal/tuitest/golden.go) compares raw bytes. internal/probe is one of the two trees the Windows job runs (ci.yml test-windows). Fix: .gitattributes '*.golden text eol=lf' and '**/testdata/** text eol=lf' (38 golden/frame files tracked); optionally normalise CRLF->LF in compareGolden's read as a second guard. Deterministic, not a flake.

## Acceptance

windows tests job green on internal/probe; git ls-files --eol shows lf for every testdata file.
