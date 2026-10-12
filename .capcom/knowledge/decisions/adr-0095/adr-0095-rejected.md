---
title: ADR 0095 considered options
---

- **Keep tokens in memory, as the SDK does.** Rejected: a browser login on every start and every
  reconnect, which no one keeps using.
- **Store tokens in the OS keystore (ADR 0047's route).** Rejected: no store on Windows or headless
  Linux, so OAuth would fail on the hosts this project targets most. A token that rotates on every
  refresh would also mean a keystore write per refresh, through a CLI.
- **Store tokens in `config.yaml`.** Rejected for ADR 0047's reasons: that file is hand-edited,
  watched, copied between machines and pasted into bug reports.
- **Enable OAuth on any server that answers 401.** Rejected: a login nobody configured, triggered
  by a server's reply. `auth: oauth` keeps it a deliberate choice in the file.
- **Prompt for login mid-session.** Rejected: the TUI owns the terminal, a login pane is new UI for
  a rare event, and a refresh failing during a turn is the worst moment to ask.
- **A fixed callback port for preregistered clients.** Rejected: a fixed port collides and needs a
  config key. RFC 8252 §7.3 already requires authorization servers to accept any loopback port.
