---
title: ADR 0008 context
---

The bench forks a run by deep-copying conversation state and copying the sandbox directory,
then driving two branches from byte-identical state
(ADR 0001). That
only works if a tool holds no live state across the boundary the fork copies — otherwise a
branch would inherit a dangling handle, an open REPL, or a half-open connection. And some
effects (a call to an MCP server, a network request) cannot be copied at all: forking the
conversation does not fork the remote.

apogee-code's `terminal` and `python-exec` tools are already one-shot/stateless (a fresh
process per call, process-group kill, no persistent shell or REPL), so this is a contract to
*port and make explicit*, not a change.
