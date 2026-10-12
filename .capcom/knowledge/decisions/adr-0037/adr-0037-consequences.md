---
title: ADR 0037 consequences
---

- **ADR 0035 decision 9 is superseded and decision 3 is widened; everything else in that record
  stands** — decision 2's persistence contract above all, along with the registry-as-source-of-truth
  bijection guard, the insert-below-example rule, reset-by-deletion, the idle-only `/settings` policy,
  the masked `api-key`, and decision 8's confinement single-homing. Its consequence line about "most
  edits are honest about applying at next launch" no longer describes the surface.
- **The engine grows a live-mutability surface**, and `Agent`'s call-class doc comment is the authority
  for it: every new setter names its class (anytime-safe or idle-only) and its consumption boundary in
  its own godoc. The anytime-safe set grows past `SetMode` / `SetConfineToWorkspace` — each new member
  guarded by its own mutex, none of them sharing one.
- **Every Driver gains live reconfiguration, not just the TUI.** The seams are engine methods over
  resolved values, so a bench, a daemon or a future remote Driver reconfigures a running agent through
  the same doors — ADR 0031's tripwire holds, and a capability welded into the pane would now fail
  visibly at those call sites.
- **ADR 0035's authorized write set gains three keys' worth of shape** (`system-prompt-file`,
  `context-files.names`, `system-prompt-text`), all of them ordinary deliberate edits through the same
  verified splice. The set's membership rule is unchanged: nothing apogee decides on its own initiative
  may join it.
- **`layout.md` and `docs/layout/settings-screen-layout.md` own the pane's chrome** — the two-line
  fixed-height description header, white section headers with spacer lines, the highlighted edit row,
  the single-line and multiline editor states with their hint lines, the picker, and the
  `⏎ opens $EDITOR` affordance. This record fixes the behaviour; the grammar is documented where the
  pane's grammar lives.
- **`CONTEXT.md`'s Settings surface term changes shape**: "`mode` is the only live apply" and the
  "(next launch)" marker leave it; live apply on commit, the session edit journal behind ` *`, and the
  `$EDITOR` round-trip enter it.
- **Changing `present.port` or `present.host` closes the docs listener** if it is already bound, and the
  next presentation binds on the new address. URLs already handed out stop resolving — inherent to a
  port change, and the alternative (serving the old address until restart) is the "(next launch)" this
  record removes.
- **An edit made outside apogee still does not reload.** Only the round-trip apogee itself launched
  triggers a reload, per the rejected file watcher; the file remains the user's document, read at the
  moments apogee has a reason to read it.
- This is a user-visible feature wave, and it **warrants a minor bump** when the next version is cut.
  That call is the owner's, and no item of the implementing plan touches a version identifier.
