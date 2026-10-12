# Notes format — the prose the user publishes

A shared include; nothing writes to it. It specifies **one artifact**: the
release notes. They are self-contained prose — a reader learns what changed
without discovering that a run, an inventory, a row or a sub-agent existed.

## The file

```markdown
# apogee — <version, or the release this covers> — <YYYY-MM-DD>

<Optional lead, 1–3 plain sentences: what this release is for. Omit rather
than write filler.>

## Breaking changes
- **<what changed>** — <what someone relying on the old behaviour now sees>.
  **What to do:** <the concrete action — the new flag, the renamed setting>.

## New
- **<name>** — <what the user can now do>.

## Improved
- **<name>** — <what is better, and how the user notices>.

## Fixed
- **<name>** — <what no longer breaks, as the symptom the user hit>.

## Docs & install
- **<name>** — <what changed about installing, packaging or the docs>.

## Internal
- **<name>** — <no visible effect, but a user still gains from knowing>.
```

The title line is the only line a publisher may drop: the GitHub release is
titled with its tag and takes the body from the first blank line down.

## Ordering and omission

- **Breaking changes lead, always**, and every bullet there carries its **What
  to do** clause. Then New, Improved, Fixed, Docs & install, Internal.
- **An empty group is omitted, heading and all** — never write "None."
- **Internal is dropped when it is noise for users.** Keep only what a user
  would want to know (a rewrite they feel as speed, a dependency they no longer
  install); refactors, test moves and build plumbing stay in the inventory.
- Within a group, the change most users care about comes first.

## One bullet, one change

One bullet per user-visible change, in layman's terms, for someone who uses
apogee and has never read its source.

- Lead with the thing, not the mechanism: "Sessions now survive a restart",
  not "the session store moved to SQLite".
- Say the effect concretely; "faster" is not a bullet.
- One sentence, two at most — a bullet that needs three is two changes.
- Name commands, flags, settings and keys exactly as the user types or sees
  them: those are user surface, not jargon.
- Every bullet is written **fresh**; authored text elsewhere is never copied.

## What never appears in these notes

- **No row ids** (`R-01`, `CL-07`, `S-12`, …), **no commit hashes**, no branch
  names, **no PR or issue ids**.
- **No file paths**, unless the path *is* the feature (a config file the user
  edits, an install location, a folder apogee tells them about).
- **No internal identifiers** — function, package, type and schema names, or
  the project's own shorthand. A change that cannot be stated without one is an
  Internal bullet or no bullet.
- **No AI attribution of any kind**, in the prose or a trailer.
- **No process language** (phases, agents, runs, scouts, verdicts,
  inventories) and **no statistics** (commit or file counts).
