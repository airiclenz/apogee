---
title: ADR 0044 deferred (dated 2026-08-11, not denied)
---

- **An 'inherit' spelling** enabling per-axis overlay of a user entry on a shipped one *(closed
  2026-08-23 as obsolete by ADR 0057:
  axis-wise resolution makes absence itself the inherit spelling)* — wanted
  only once a shipped entry sets both axes and a user disagrees with one.
- ~~**Profiles in the seeded config template.** `model-profiles:` stays the one key the seeded
  template does not document, deliberately: the shipped table is meant to make it unnecessary.~~
  *(**Reversed 2026-08-12 on the owner's report** that the seeded template had not been updated.
  The shipped table removes the need to WRITE an entry, not the need to know the key exists: it is
  what a reader reaches for when a dialect is wrong or a built-in matched the wrong model, and it
  is the spelling the retirement error tells them to paste. The template now documents it as a
  commented example like every other key, and names the retired global key as retired. Nothing
  else in this record changes.)*
