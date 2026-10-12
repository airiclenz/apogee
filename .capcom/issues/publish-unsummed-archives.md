---
title: publish cannot check archives already on a release that has no SHA256SUMS
status: open
type: bug
priority: 3
aliases: [apogee-publish-unsummed-archives]
created: 2026-10-11T11:59:46Z
---

cmd/release/publish.go:376-381 (convergeRelease): when the GitHub release already exists but carries no SHA256SUMS (e.g. a create that failed mid-upload), the archives already uploaded are never checked against the rebuild — checkPublishedSums (cmd/release/publish.go:463) runs only when SHA256SUMS is among the uploaded assets. publish relies on dist's byte-identical rebuild from the tag and uploads its own SHA256SUMS beside them. Deferred from the release-pipeline-in-repo plan (item 6, docs/plans/archived/2026-10-11 - 00 - release-pipeline-in-repo-plan.md).
