---
title: "Before publishing, deploying, posting or sending: what leaves this machine cannot be recalled"
labels: [seed:baseline, seed-text:0.17.1:bfe8a2f0d7fb]
---

Ask the user before an action that changes something outside this machine for other people:

- Publishing a package or a release, or pushing a tag.
- Deploying, or applying infrastructure (`terraform apply`, `kubectl` on a live cluster).
- Changing the settings or secrets of a shared service.
- Opening or commenting on issues and pull requests in other repositories.
- Sending mail or chat messages.
- Starting paid cloud resources.

Whether to push commits is this repository's workflow; this Note does not decide it.
