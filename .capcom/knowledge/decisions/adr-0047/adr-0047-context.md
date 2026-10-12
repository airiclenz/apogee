---
title: ADR 0047 context
---

A `servers:` entry that talks to anything but a bare local llama.cpp needs a credential, and until
now the only place to put one was `api-key:` — the key itself, in `~/.apogee/config.yaml`. That
file is the worst home a secret can have in this project specifically. It is **hand-edited** (the
settings surface splices into it, ADR 0035,
and the user edits the rest by hand), it is **watched and re-read** every second
(ADR 0041), it is the **single definition of what servers
exist** (ADR 0036)
so it is the file a user copies to a second machine, and it is exactly the kind of dotfile that ends
up in a backup, a screen-share or a pasted bug report. Nothing about apogee makes that safe, and a
key that lives in it stays there for as long as the entry does.

The obvious answer — "apogee should use the keychain" — does not survive contact with the machines
this agent runs on. Small models are hosted on **headless boxes**: a devbox over SSH, a container, a
GPU machine with no session bus. Linking a keychain library would put a D-Bus client and a Keychain
framework into a binary that must stay CGO-free and cross-compile six ways
(ADR 0042), to reach a
store that half those hosts do not have — and it would make apogee the *keeper* of somebody's
secret, with its own storage format, its own recovery story, and its own bugs.

The users who have already solved this solved it with a **command**: `pass show`, `op read`,
`security find-generic-password`, `gcloud secrets versions access`, a wrapper script. Every one of
those is a program on PATH that prints a secret to stdout — the shape apogee already deals with
everywhere else. What was missing was a place in the config file to say so, a moment to run it, and
a way to get an existing plaintext key out of the file it is already sitting in.
