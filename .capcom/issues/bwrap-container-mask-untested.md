---
title: bwrap's container-socket mask has no live test, only argv tests
status: done
type: task
priority: 3
labels: [testing]
created: 2026-11-11T14:10:30Z
closed: 2026-11-11T18:50:19Z
reason: "Resolved by plan '2026-11-11 - 02 - fence-test-gaps-and-landlock-udp-plan' item 2 (archived at c48693ef): live TestNamespaceMasksContainerSockets shows a confined connect to a masked container socket reaches no listener, with an unmasked control arm."
---

Item 8 masks `/run/docker.sock` and `/run/podman/podman.sock` with `--ro-bind /dev/null` under bwrap (internal/platform/namespace_linux.go:362). The only coverage is hermetic argv assertions (internal/platform/namespace_linux_test.go:97-150); no test runs a bwrap box and confirms a connect to a masked container socket reaches no daemon. Deferred out of the closeout of `docs/plans/archived/2026-11-11 - 01 - fence-escape-and-open-defects-plan.md` (item 8).

## Acceptance

- A live bwrap test (skipping where bwrap is absent) shows a confined connect to a masked container-socket path reaches no listener.
