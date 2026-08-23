# Standing schedules — `apogee daemon`

`/schedule` keeps a prompt on a cycle for as long as apogee is open. `apogee daemon`
is the same idea with the TUI taken away: a foreground process that reads
`~/.apogee/daemon/schedules.yaml`, puts every entry in it on the clock, and runs each
one's prompt through the same shared runner a `/schedule` firing and `apogee headless`
run through. Same binary, same engine — a third front-end onto it, not a second agent —
and every firing is saved to `~/.apogee/sessions`, so `/sessions` inside apogee is the
results window.

It never detaches. The daemon stays in the foreground and writes plain timestamped
lines to stdout; the host's own supervisor owns restarts and log retention, and
[`apogee daemon install`](#the-unit-that-runs-it--apogee-daemon-install) writes the unit
that does it.

## The file is the whole contract

There is no `apogee daemon add` and no hidden state: `~/.apogee/daemon/schedules.yaml`
says what runs. The first run of `apogee daemon` creates it from a commented template
compiled into the binary — every line commented out, which is a valid *empty* file, so a
fresh daemon starts, says it has 0 schedules and watches — and an existing file is never
overwritten.

```yaml
# ~/.apogee/daemon/schedules.yaml
shutdown-grace: 10m            # optional; 10m is the default

schedules:
  - name: nightly-audit        # required, and unique in this file
    on:
      cycle: 24h               # required; a Go duration, 30s floor
    run:
      prompt: "/code-audit internal/tui"  # required — what you would type into apogee
      workspace: ~/repos/apogee           # required; `~` expands, must exist
      mode: plan                          # optional: plan (default) or auto
      server: workstation                 # optional: a `servers:` entry by name
      model: qwen3-coder-30b              # optional; only where that server serves several
                                          # (never on one llama-launcher fronts)
```

The daemon watches the file and picks up every saved edit within a second or two — no
restart. Entries are matched across an edit by their `name:`, so a schedule you did not
touch is left strictly alone and keeps its place in its own cycle; only what actually
changed is stopped and re-added, and a renamed entry counts as a new one. An edit that
does not parse, or that breaks any rule above, is refused **whole**: every defect is
logged and the schedules already on the clock keep running. Nothing is half-applied.

`cycle:` is how often an entry runs, and its first run lands one full cycle after the
daemon adopts it — nothing fires the moment the daemon starts, so a supervisor's restart
loop can never become a firing storm, and restarting the daemon restarts every cycle.
If a run is still going when the next tick comes due, that tick is skipped, not queued.

## What a firing leaves behind

A firing is an unattended run, so it never asks: every gated action is refused rather
than parked, `ask_user` and `present_document` are not registered, and no MCP server is
contacted. Only the two modes that need nobody are accepted — `ask-before` and
`allow-edits` exist to consult a human, so no schedule may use them.

| `mode:` | What the run leaves behind |
|---|---|
| `plan` (the default) | read-only: the deliverable **is** the recorded answer — open the run in `/sessions` to read it. Reviews, audits, digests. |
| `auto` | confined and unattended: the deliverable is the **state of the workspace** afterwards. Point it at a git repository, so you can read the diff and throw it away. |

`auto` is refused outright on a host whose confinement backend cannot fence the
filesystem — there the interactive fallback is approval, and a firing has nobody to
approve (see [Auto mode's blast radius](configuration.md#auto-modes-blast-radius), and `apogee probe`
for what this host reports). Because the file is all-or-nothing, one ineligible `auto`
entry rejects the whole file, naming that entry in the log.

## Which server a schedule talks to

A schedule binds to a server **by name**, out of the `servers:` list in
`~/.apogee/config.yaml` — so the endpoint and its key stay in one file and the schedules
file carries no secrets. Leave `server:` out and the schedule runs on whatever `server:`
in `config.yaml` names as the startup default.

`config.yaml` is read **once**, at startup: changing your servers means restarting the
daemon. `schedules.yaml` is the only live surface.

The daemon **never loads a model**. On a server llama-launcher fronts, `model:` would be
a request to *load* that model, so it is refused there — drop the key and the firing
sends to whatever that server is already serving. If nothing is serving, the firing
fails visibly in its own record and the next cycle behaves normally.

## Starting and stopping

Only one daemon may run per apogee home. An advisory OS lock on
`~/.apogee/daemon/schedules.lock` is held for the process's lifetime and a second daemon
refuses to start, because two daemons over one schedules file would double-fire every
entry in it; the kernel drops the lock with the process, so a crash leaves no stale
state to clean up.

The first `SIGTERM` or `Ctrl-C` stops the clock and gives a firing already in flight up
to `shutdown-grace` (10m by default) to finish; a second one cancels it immediately, and
whatever the run completed is still saved. Either way the daemon exits `0`.

```console
$ apogee daemon
2026-08-22T21:00:00+02:00 created   nightly-audit — on the clock
2026-08-22T21:00:00+02:00 watching /home/you/.apogee/daemon/schedules.yaml — 1 schedule on the clock
2026-08-23T21:00:00+02:00 fired     nightly-audit — /code-audit internal/tui
2026-08-23T21:07:41+02:00 completed nightly-audit in 7m41s — 9 turns, 0 denied, saved as 20260823-210000-3b7d
```

## The unit that runs it — `apogee daemon install`

`apogee daemon install` writes the unit that makes this host run `apogee daemon` for
you, then prints the one command that activates it. It never runs that command itself:
putting a standing process on a machine is the machine owner's sentence to type, and a
generated unit is worth reading before it is trusted.

| OS | What is written | Activate it with |
|---|---|---|
| Linux | a systemd **user** unit, `~/.config/systemd/user/apogee-daemon.service` | `systemctl --user enable --now apogee-daemon` |
| macOS | a launchd agent, `~/Library/LaunchAgents/com.airiclenz.apogee.daemon.plist` | `launchctl load -w <that path>` |
| Windows | a Task Scheduler definition, `~/.apogee/daemon/apogee-daemon-task.xml` | `schtasks /create /tn "apogee-daemon" /xml "<that path>"` |

The unit names the absolute path of the apogee binary that generated it and derives the
supervisor's kill escalation from `shutdown-grace` (grace + 30s), so the supervisor
never kills a firing the daemon is still letting finish. Re-run `apogee daemon install`
after changing `shutdown-grace` or moving the binary: it regenerates the file and says
whether anything changed. `--config` is the only apogee home a unit records — a home set
through `APOGEE_CONFIG` is the shell's, not the supervisor's, so pass it as a flag here
if the daemon should use it.

