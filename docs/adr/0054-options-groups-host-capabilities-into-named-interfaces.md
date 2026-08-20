---
Status: accepted
---

# Options groups host capabilities into named interfaces, one per family

## Context

`internal/tui.Options` is the whole interface between the composition root (`cmd/apogee`) and the
renderer. The 2026-08-19 TUI architecture review
(`docs/reviews/2026-08-19 - 00 - tui-architecture-review.md`, Candidate 9) measured what it had
become: **63 fields, about 30 of them one-purpose bare `func` values** — the settings pair-plus-two,
the server family, the launcher family, the scheme family, the heartbeat pair, and more. The seam
grew one field per host capability, exactly as fast as the implementation behind it. That is the
definition of a **shallow** module: the interface a caller must learn is as large as the thing it
hides.

Two facts bounded the design.

**The deep shape was already in the same file.** `Engine`, `SkillCatalog`, `SessionHost`,
`RecallHost` and `Scheduler` are named interfaces, each carrying a family of methods with one doc
comment stating the contract the family shares — nil means unwired, who owns what, which goroutine
calls. Nothing about the func families was different in kind; they were simply never named.

**Nothing could be faked alone.** `m.opts` is read in 19 non-test files, and a test that wanted to
exercise one family had to build a whole `Options` and a whole `Model` around it. There was no value
to hand a test that meant "the settings seam".

The func families also carried a contract nobody had written down: **a nil member means that act is
unavailable, and the pane says so** — the renderer never treats an unwired seam as an error and
never papers over it. That contract had to survive the regrouping, because the degrades it produces
are what a bench or headless Driver composes deliberately
([ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)).

This record ratifies the shape introduced by
`docs/plans/2026-08-19 - 04 - tui-architecture-deepening-plan.md` item 25. It supersedes nothing.

## Decision

**A host capability that is a FAMILY of acts joins `Options` as one named interface, not as N bare
funcs.** A family is a set of acts over one subject that a host either has or does not have: the
`/settings` pane's four acts over one config file, the three things a program does with a schemes
folder. The first two are `SettingsHost` and `SchemeHost`, declared in `tui.go` beside the five
interfaces that were already there.

**1 — The interface is the contract, and it is stated once.** What used to be a doc comment per
field becomes a doc comment on the interface (what the family is, who owns what behind it, what a
nil host means) plus one per method (what that act does and how it answers). No knowledge is lost in
the regrouping: the contract a reader needed to assemble from four adjacent fields is now one
declaration they can read top to bottom.

**2 — A nil interface is the family's unwired degrade.** `m.opts.Settings == nil` replaces four
`m.opts.WriteSetting == nil` checks and means the same thing at the family's granularity: the pane
has nothing to show and says so. Every call site was audited; no nil check was dropped.

**3 — A wired host that cannot perform ONE act says so in that act's own answer.** The per-member
degrade is real — a Driver may persist without applying, or list the schemes without switching to
one — so it is expressed where the act is: `Rows` answers with no rows, `Write` and `Reset` answer
with an error, `Apply` answers with no note and no error (the write stands on its own), `Export`
answers with an error. `Resolve` is the one act whose signature had no room to say it, so it gained
an `ok bool`: false is "this host cannot resolve live", the caller keeps the palette it has and the
row says the new scheme applies at the next start. Nothing about the sentences the human reads
changed.

**3a — An act the renderer decides ABOUT is reported, not attempted.** Some acts cannot say it in
their own answer at all, because the caller has to know before it calls: whether anything observes
the Upstream decides whether a tick chain opens, whether a rebind is possible decides whether an
observed change is captured, and whether a switch or a bind exists decides whether a picker is
raised. Calling to find out would BE the act. Such a family carries one report of what it performs —
`ServerHost.Acts() ServerActs`, four bools, zero value = unwired — asked wherever the per-func nil
check was. Its zero value is also what a nil host answers, so one shape covers the family and the
member and no caller writes two checks. A host claiming an act is taken at its word, exactly as
decision 3 takes `Apply`'s silent success. Acts whose own answer already IS the degrade — a list
that names nothing, a recording that says it wrote nothing — get no flag.

**4 — The composition root implements the interface with a value that holds what the acts need.**
`settingsHost` holds the resolved options, the config path, the external-edit baseline and the apply
dispatcher; `schemeHost` holds the schemes folder. They are adapters with behaviour, not structs of
closures: the splice, the baseline refresh and the folder read live in their methods. Both are in
`wire_options.go`, beside the projection that hands them over
([ADR 0043](0043-files-split-by-concern-and-config-gets-a-package.md): the seams line up with the
`wire_<seam>.go` split).

**5 — They stay reference headers safe in a value-copied `Model`.** An interface value is a header,
so the `Model` that is copied on every `Update` carries 16 bytes per family instead of one word per
func ([ADR 0011](0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md)). The concrete host
behind it is boxed once, at the launch call.

**6 — Tests get one fake per family, wired one member at a time.** `fakeSettingsHost` and
`fakeSchemeHost` hold a func per act and answer the documented unwired degrade for any member a test
leaves nil. That is what keeps the per-member degrades provable — "the pane has rows but nothing to
write with" is one literal — without a fake per combination of wired halves.

**7 — What is NOT a family stays a field.** A resolved value (`ColorScheme`, `Workspace`,
`StallAfter`), a single act with no siblings (`SaveHostAcknowledgement`), a lone provider — these are
not capabilities a host has or lacks as a set, and wrapping one in an interface would be indirection
with a single adapter. The test is whether the acts share a subject AND a wiring decision.

## Consequences

- The seam stops growing one field per capability. A new act on an existing family is a method on an
  interface that already exists, and its contract is stated beside its siblings.
- A renderer test can say "the settings seam" and hand over one value. Whole-`Options` construction
  is still how a pane test opens a pane, but the family it is exercising is now one field in it.
- `Options` lost seven fields and gained two, 63 down to 58; the ~120 lines of field documentation
  they carried moved onto the two interfaces without losing a sentence of it.
- The remaining func families named by the review — server, launcher, heartbeat — are the same
  refactor and are sequenced as later items of the same plan. This record is what they follow. The
  first of them, `ServerHost` (server ×4 + heartbeat ×2), is what decision 3a was written from.
- Behaviour is unchanged by construction: every degrade the nil funcs produced is produced by a nil
  host or by the act's own answer, and the whole existing suite passes with no expectation changed.
