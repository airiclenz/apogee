---
title: "stubllm: replay and capture cassette subcommands for photon services"
status: open
type: task
priority: 3
plan: 2026-10-17 - 01 - photon-adoption-prep-plan.md
created: 2026-10-17T10:34:00Z
---

photon runs the hero clip's model as a **service** chosen by profile (`replay` by default,
`capture` to re-record), so `cmd/stubllm` must serve a cassette and proxy-record one from the
command line. Today it has only `serve --script` and `record --out`, which speak script YAML;
the cassette path lives inside `cmd/demorig` (`internal/stubllm` `LoadCassette`/`NewReplayer`,
`NewCassetteRecorder`/`Cassette().Save`).

Wanted:
- `stubllm replay --cassette <file>`: serves the cassette.
- `stubllm capture --upstream <url> --key-env <VAR> --out <file>`: proxies to a live model and
  records the exchange. photon runs `on_success` **after** the services stop, so `capture` saves
  the cassette when it gets SIGTERM, not only on a clean exit.
- Both print the same `listening <url>` ready line as `serve` and take `--listen` like it
  (hero uses 18181: `setup.sh` writes the port into apogee's config before the services start).

## Acceptance

- `stubllm replay` serves a cassette that `demorig record` replays today, byte for byte.
- `stubllm capture` writes a loadable cassette on SIGTERM; a test drives that.
- Each prints `listening <url>` once it accepts connections.
