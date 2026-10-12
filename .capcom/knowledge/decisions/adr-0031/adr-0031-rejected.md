---
title: ADR 0031 considered options
---

- **Candidate direction (no force)** — rejected: door-keeping without tiebreaker force
  is advisory, and silent door-closing is the exact failure mode.
- **Product-centric subject** (the binary becomes the platform) — rejected: front-loads
  an identity change and strains the single-binary promise before any platform code
  exists.
- **Engine grows a versioned wire API** — rejected: re-opens what ADR 0001 closed for
  reasons that still hold; Drivers can expose protocols without taxing the engine.
- **Curated first-party connector set** (SQL, S3, IMAP) — rejected: the first step onto
  the maintenance treadmill, and naming the exception invites it.
- **Engine-only bench discipline** — rejected: recreates the proxy-era unmeasurable-
  scaffolding problem one layer up, precisely where unattended operation makes it most
  dangerous.
