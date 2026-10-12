---
title: ADR 0054 decision
---

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
(ADR 0043: the seams line up with the
`wire_<seam>.go` split).

**5 — They stay reference headers safe in a value-copied `Model`.** An interface value is a header,
so the `Model` that is copied on every `Update` carries 16 bytes per family instead of one word per
func (ADR 0011). The concrete host
behind it is boxed once, at the launch call.

**6 — Tests get one fake per family, wired one member at a time.** `fakeSettingsHost` and
`fakeSchemeHost` hold a func per act and answer the documented unwired degrade for any member a test
leaves nil. That is what keeps the per-member degrades provable — "the pane has rows but nothing to
write with" is one literal — without a fake per combination of wired halves.

**7 — What is NOT a family stays a field.** A resolved value (`ColorScheme`, `Workspace`,
`StallAfter`), a single act with no siblings (`GenerateTitle`), a lone provider — these are
not capabilities a host has or lacks as a set, and wrapping one in an interface would be indirection
with a single adapter. The test is whether the acts share a subject AND a wiring decision. *(Amended
2026-09-17: this example first read `SaveHostAcknowledgement`, which had siblings all along — the
seven other acts over the same config file — and is now a member of `ConfigHost`; see the Amendment
section below.)*
