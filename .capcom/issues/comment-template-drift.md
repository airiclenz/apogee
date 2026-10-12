---
title: Code-comment and template drift found by the 2026-09-16 docs fact-check
status: done
type: task
priority: 3
aliases: [apogee-bsg]
created: 2026-09-16T06:55:45Z
closed: 2026-09-16T21:32:10Z
reason: prose half (e35ae57a) + order half (25b6e93a) landed
---

Stale comments and asset text found while fact-checking README + manual against v0.21.8; none user-visible in the manual after that review. Fix in one sweep:
- internal/config/registry.go:170-173 claims the registry follows the starter template's order; it diverges (reactions, use-default-prompt, the seven Floor-guard rows). Either reorder or fix the comment; no order test exists.
- internal/config/defaults.go:16-18 says every template key is commented out (ADR 0064 §4); the template has 17 active scalar keys plus context-files: and ui: blocks.
- internal/config/defaults/config.yaml:354 says 'the three placeholders above' — there are four (internal/prompt/prompt.go:29-37; the template itself lists four at :270-286 and says 'four' at :327).
- internal/security/rules.go:250-263 and dangerous.go:50-53 describe a MergeDangerousRules config merge with no callers and no key.
- internal/eventjson/writer.go:103 says an opted-in seam_closed 'consumes no sequence number' but Emit → writeLine increments seq for it (:107-110,161). The manual's 'seq counts every line' is right.
- docs/adr/0075 §4 says 'Nineteen line kinds' with mechanism_fired/floor_guard; code is v2, twenty kinds, reaction_fired (internal/eventjson/writer.go:19, encode.go Kinds()). Add an amendment note.
- docs/layout/split-diff-layout.md:107 says lines are never clipped; the stacked reading clips at 160 runes (internal/tui/diffbody.go:780, textutil.go:46).

## Log

- 2026-09-16 beads: Also: cmd/apogee/probemodel.go:70-71 Long text says the record path is printed either way; an incomplete battery prints no path (internal/probe/model.go:209-212).
