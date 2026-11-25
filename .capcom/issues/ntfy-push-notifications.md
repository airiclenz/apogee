---
title: Ship a ready-to-use ntfy push-notification Reaction (and the question-asked notice it needs)
status: done
type: feature
priority: 3
created: 2026-11-24T20:38:54Z
closed: 2026-11-25T07:11:24Z
reason: "Shipped: question-asked notice (QuestionEvent from ask_user, recipe missing-input prompts and workflow ask stages) with a question journal line; denied count on exchange-finished; unattended runs (headless/daemon//schedule) no longer fire approval-requested Reactions; ready-to-use ntfy example in the starter config and docs/manual/reactions.md 'Push notifications'; adr-0076-amend-1124, adr-0075-amend-1124. make check green; live ntfy push confirmed by the owner."
---

Let a user get a phone or desktop push from [ntfy](https://ntfy.sh) when apogee needs them —
an approval or a question is waiting — and, by choice, when a run ends, in the TUI, `apogee
headless` and `apogee daemon` alike. Setup is copy, uncomment, fill in a topic: apogee ships a
ready-to-use example, and the same example shows how to point it at another notification
target.

## Why

apogee runs long turns against slow local models. The user walks away, and the run then sits
parked on an approval or a question nobody sees, or finishes unnoticed. Headless and daemon runs
are unattended by design, so their outcome is only seen if someone reads a log.

## Design: ntfy is a Reaction, not a new key

No `ntfy:` or `notify:` config key and no ntfy-specific code. A `run:` webhook entry already
does everything needed (`docs/manual/reactions.md`):

- **Which events push** is the entry's `on:` list.
- **The token** goes through `headers-env:`, so it never sits in the config file.
- **The message text** comes from ntfy's own *message templating*: with `X-Template: yes`, ntfy
  formats title and message (Go templates, `{{.field}}`, `{{if …}}`) from the JSON body apogee
  already POSTs. apogee's payload does not change shape.
- **Priority per event** means one entry per priority (`X-Priority` header).
- **What the push shows** is whatever the user's template quotes: the shipped example quotes the
  event, workspace, schedule name and `denied` count. **What leaves the machine** is the whole
  payload, question text and approval reason included, since a webhook POSTs it as its body;
  the manual says so and recommends a self-hosted ntfy server when that matters.
- **Headless and daemon** already fire user Reactions, so the same entries work there.

This keeps the change additive and settles ADR 0031 clause 3 ("no first-party connectors"):
apogee ships an example config, not a connector.

Only one gap needs engine work: **a model's question fires no notice.** `ask_user` goes through
the `Asker`, never the Approver (`internal/tools/ask_user.go`), so `approval-requested` does not
fire for it.

## Work

1. **New notice Moment `question-asked`**: fires when `ask_user` raises a question, before it is
   answered, at any depth (like `approval-requested`). Payload: the question and its choices.
   The Moment set grows additively (`capcom know show "Moment"`): the notice count goes from
   thirteen to fourteen, and the glossary Note, `docs/manual/reactions.md`, the starter config's
   Moment list and the count drift test (`TestManualStatesTheMomentCount`) follow. Whether a
   matching `question-answered` is worth adding is a design call; it is not needed here.
2. **Outcome fields for unattended runs**: `exchange-finished` carries `faulted` and
   `step_capped` already. Add, if missing, the run's `denied` count, and from the daemon the
   schedule's name, so a push can say "nightly-audit failed" or "3 actions refused". New JSON
   members only; existing scripts ignore them.
3. **The shipped example**, in two places:
   - a commented `ntfy` block in the starter config (`internal/config/defaults/config.yaml`,
     the Reactions section), next to the existing `notify` / `ci-bell` examples;
   - a "Push notifications" section in `docs/manual/reactions.md` with the same block, how to
     pick a topic, how to set the token, and what each entry sends.

   The starter config is written only on first run and never overwritten, so existing users get
   the example from the manual and a `CHANGELOG.md` line that points at it.

The shipped block, roughly (exact template syntax to be checked against a real ntfy server):

```yaml
# reactions:
#   - id: ntfy-waiting                   # you are needed: high priority
#     on: [approval-requested, question-asked]
#     run:
#       url: https://ntfy.sh/<your-topic>  # or your own ntfy server
#       headers:
#         X-Template: "yes"
#         X-Title: "apogee needs you"
#         X-Message: "{{.event}} in {{.workspace}}"
#         X-Priority: "high"
#       # headers-env:
#       #   Authorization: NTFY_AUTH     # a variable holding "Bearer tk_…"; needed for a protected topic
#   - id: ntfy-done                      # a run ended
#     on: [exchange-finished]
#     run:
#       url: https://ntfy.sh/<your-topic>
#       headers:
#         X-Template: "yes"
#         X-Title: "apogee"
#         X-Message: "{{if .faulted}}run failed{{else if .step_capped}}run hit the step cap{{else}}run finished{{end}} in {{.workspace}}"
```

The `Authorization` line stays commented out: an unset `headers-env:` variable fails the
request, and a public ntfy.sh topic needs no token. The manual says why a guessable public topic
is readable by anyone and recommends a long random name or a token.

## Other notification targets

The manual section ends with how to change the example for other targets:

- **Desktop only:** an argv `run:` (`notify-send`, `osascript -e 'display notification …'`,
  `terminal-notifier`), as in the existing `notify` example.
- **Any target that takes apogee's JSON as it is**, or can template it like ntfy: change `url:`
  and the headers.
- **Targets that need their own body shape** (Slack, Discord, Pushover, Gotify): apogee's webhook
  cannot reshape its body, so use an argv `run:` that reads the payload from stdin and
  calls the service (a short script with `curl` and `jq`). Shaping the body in apogee is out of
  scope; file it separately if users ask.

## Events covered

| Event | Moment | In the example |
|---|---|---|
| Approval waiting | `approval-requested` | yes, high priority |
| Model asks a question | `question-asked` (new) | yes, high priority |
| Run finished / failed / step-capped | `exchange-finished` (`faulted`, `step_capped`) | yes |
| Turn cancelled | `turn-finished`, `status: cancelled` | mentioned only |
| Engine error | `error` | mentioned only |

In headless and daemon runs nobody is asked: gated actions are refused (the `denied` count) and
`ask_user` is not registered (`docs/manual/headless.md`, `docs/manual/daemon.md`). There only
`ntfy-done` fires, and the outcome fields from item 2 carry the news.

## Out of scope

- A first-class `ntfy:` / `notify:` key. Add it only if hand-written entries prove painful.
- "Push only if the run took longer than N minutes": neither ntfy nor Reactions can filter on
  that. It needs a condition on Reactions, which is a separate feature.
- Answering an approval from the phone (ntfy action buttons). ADR 0031 makes that a Driver's
  composition; nothing here should rule it out.

## Acceptance

- `question-asked` fires when `ask_user` raises a question, at any depth, and a `run:` entry can
  subscribe to it; the glossary, the manual, the starter config and the Moment-count drift test
  state fourteen notices.
- `exchange-finished` carries the `denied` count, and a daemon firing's payload names its
  schedule.
- The starter config carries the commented ntfy block; uncommented with a real topic, it pushes
  on an approval, a question and a run's end, against a real ntfy server (manual check, recorded
  in the commit).
- `docs/manual/reactions.md` has the "Push notifications" section: the block, topic and token
  setup, the privacy note on public topics, and how to change the example for other targets.
- No existing config, Reaction entry or payload field changes meaning; a config written for the
  previous release loads unchanged.
- `CHANGELOG.md` names the new notice and points at the manual section.

## Log

- 2026-11-24 Airic Lenz: Implemented (uncommitted): question-asked notice + QuestionEvent + question journal line, denied on exchange-finished, unattended runs drop approval-requested Reactions, ntfy example in starter config + manual, adr-0076-amend-1124, adr-0075-amend-1124. make check green (866s). Live ntfy push not yet checked.
