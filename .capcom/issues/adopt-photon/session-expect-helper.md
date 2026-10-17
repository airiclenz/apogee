---
title: "cmd/sessionexpect: judge a take's session entries from a photon run: expect"
status: open
type: task
priority: 3
plan: 2026-10-17 - 01 - photon-adoption-prep-plan.md
created: 2026-10-17T10:34:00Z
---

photon judges a take only by screen (`seen`) and by `run:` commands; it knows nothing of apogee
sessions. hero's entry expects (`entry`, `contains`, `before`, `after`) move into a small dev
tool, `cmd/sessionexpect` (built like `stubllm`, never a release asset), that a `run:` expect
calls.

It reads the newest session under `$PHOTON_WORK/home/.apogee/sessions`, judges one clause set,
and exits 0 on a pass or 1 with a one-line detail. Selectors keep demorig's grammar (`kind`,
`text`, `tool`, `target` prefix matches, `nth` incl. `last`), written inline:

    sessionexpect --entry 'kind=toolCall,tool=Sub-Agent' --contains Sub-Agent
    sessionexpect --entry 'kind=toolCall,target=CHANGELOG.md' --after 'kind=interjected'

`before`/`after` take a second selector inline (no `contains` in a selector), since a `run:` cannot see another beat's expect.

Port `cmd/demorig/{anchors,session}.go`, the entry clauses of `check.go`, and their tests and
fixture.

## Acceptance

- Every entry expect in `graphics/demo/storyboards/hero.yaml` has a `sessionexpect` equivalent
  that passes on the fixture session and fails, with its detail, on a mutated one.
- `make sessionexpect` builds it; it is not a release asset.
