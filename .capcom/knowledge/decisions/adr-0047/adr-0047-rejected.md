---
title: ADR 0047 considered and rejected
---

- **A keychain library (`go-keyring` or equivalent) and a fourth `api-key-keychain:` source** —
  rejected: it links a D-Bus client and a Keychain framework into a CGO-free six-target binary for a
  store that a command already reaches (ADR 0042), it breaks or hangs on the headless hosts small
  models actually run on, and it would make apogee the keeper of the secret rather than a reader of
  the user's own store. The `api-key-cmd:` a migration writes is strictly more portable than a
  library call, because the user can run it themselves.
- **Encrypting the config file (or the key field) with a passphrase** — rejected: it buys a
  passphrase-management problem, a prompt on a file that is read every second by the watcher, and a
  recovery story, all for one field that the OS already has a place for.
- **A fixed precedence or a fallback chain across sources** (`api-key-cmd:` first, fall back to
  `api-key:`) — rejected: it makes a configured key silently ignored, or a stale one silently used,
  and either way the file no longer says what happens. Exactly-one means the answer to "where does
  this key come from" is always the entry itself.
- **Silent auto-migration of every plaintext key** — rejected: it writes into the user's file
  without an answer, against ADR 0035's deliberate-edit grain, and it takes a key out of a file the
  user may be copying to another machine on purpose. "Never for this entry" exists for the same
  reason: an answer that is not "yes" must be able to be final, or the question becomes a nag the
  human learns to dismiss unread.
- **Resolving at load, or running the command in `ValidateServers`** — rejected: validation would
  become a keychain storm on every start-up and every watched re-read of the file, and a config check
  would depend on the state of an unlocked keyring.
- **Running `api-key-cmd:` through a shell** — rejected for the reason the exec tools already give:
  it is a quoting-and-injection surface on every platform and there is no shell to assume on Windows.
  A wrapper script is the user's own, and is visible in the file.
- **Windows migration** — deferred, not denied: there is no built-in generic-secret CLI to make the
  write+read pair from, so Windows takes the notice. Revisiting it needs a decision superseding
  point 8.
