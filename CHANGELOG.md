# Changelog

All notable changes to Apogee are recorded here. The public Go API follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html) from `v1.0.0`
onward (ADR 0001 §consequences, as amended at the Phase-3 cut): Events and
hook points stay **additively extensible**, so a new Event variant or hook
point is a **minor** bump, not a breaking change.

## [Unreleased]

### Added

- **Two new colour roles carry the diff band.** `diff-add-bg` and `diff-del-bg` join the scheme
  vocabulary (ADR 0040), taking it to 31 roles: the background a diff body line sits on, shipped
  as dark `#0e3b34` / `#42181d` and light `#d9f2ec` / `#fbe4e6` — quiet washes out of the same
  turquoise and red families as the `diff-add` / `diff-del` foreground pair, so the pairing still
  survives red-green-weak vision once the band rather than the text carries the signal. A scheme
  file of your own that names neither new key stays exactly as valid as it was: an omitted role has
  always kept its default, silently.

- **A split diff survives a reload.** A resumed session used to bring a diff-bodied block back as
  stacked rows even where the live one had painted two panes: the record carried the rendered rows
  and not the change they were rendered from, and the split reading is composed at paint time, from
  the change itself. The Edit regions a block was painted from now travel with it — the removed and
  the inserted lines, the unchanged context bracketing them, and the line each region sits on in the
  before and in the after file — and beside them the file each region was cut from, which is what a
  multi-file `git_diff_range` body keeps its per-file header rows by. Both are additive on the
  transcript's own rule (ADR 0052 §5): no version bump, a block with no regions writes not one extra
  byte, an older build ignores what it cannot place, and a record written before this decodes with no
  regions and paints the stacked rows its body always carried.

- **`git_diff_range` bodies are the change, file by file.** The diff-range tool applies nothing
  either, and what it prints is git's own unified diff — which, unlike `view_diff`'s whole-file
  body, elides everything between its hunks. The renderer now walks that output, taking each
  region's numbers from the `@@ -a,b +c,d @@` header its hunk opens with and each file from the
  `diff --git` line above it, and cuts exactly the regions an edit tool records at apply time: the
  changed lines with up to three unchanged lines of context each side, neighbouring changes left as
  separate regions whose context tiles the lines between them, and the `⋯` rule standing only where
  lines are genuinely left uncovered — which, between two hunks, is always. A body spanning several
  files paints as several sections, each under one muted row naming its file (ratified call 10) and
  each numbering its own lines, in both readings: stacked where the block is narrow, two panes where
  the width allows. That block's body used to be plain uncoloured output. The reading is total and
  all-or-nothing — a binary or rename-only section, a `--stat` or `--name-only` call, git's "No
  differences found", anything the walk cannot place leaves the WHOLE body rendering as the plain
  output it always did, never a mix of parsed and quoted sections — and the branch slot's `+A −R` is
  untouched throughout, still counted off every tagged line the tool printed.

- **`view_diff` bodies are the change, not the file.** The diff-preview tool applies nothing, so no
  tool records its Edit regions — but what it prints is a whole-file diff, and the renderer now walks
  that output once, counting each file's lines from 1, and cuts it into exactly the regions an edit
  tool records at apply time (ADR 0052 §2): the changed lines with up to three unchanged lines of
  context each side, neighbouring changes left as separate regions whose context tiles the lines
  between them without overlap, and both files' absolute numbers carried so a region reads correctly
  wherever an insertion has drifted the after file past the before one. An expanded `view_diff` block
  therefore stops painting the whole file as context and shows the same numbered body every other
  diff block shows — stacked where the block is narrow, two panes where the width allows — with the
  `⋯` rule standing only where lines of the file are genuinely left uncovered. The recovery is
  all-or-nothing: output carrying none of the diff's tags is not a rendered diff, so the "No changes
  detected" sentinel and the over-budget diffstat-only sentence keep the plain rendering they always
  had, and the branch slot's `+A −R` is untouched throughout — it is still the tool's own typed
  diffstat, counted over the whole diff rather than over what the body shows.

- **Expanded diff bodies paint as two panes where the width allows.** The Split diff composer that
  landed pure and unpainted is now wired into every expanded body path a diff-bodied block can
  reach: the ungrouped call's body under its branch, the TARGETLESS block whose lines are its own
  branches (a bare `git_diff_range`, which names neither base nor head and so has no target to lead
  a row), and the open member of a grouped block — which carries the unspanned sub-agent group
  member with it. One seam makes the choice for all three (`splitBody`): a body whose tool recorded
  Edit regions paints as panes when `splitDiffFits` says each pane can still give the code 40
  columns after its gutters, and as the numbered Stacked rows the same regions already built when
  it cannot. The question is asked at PAINT time against the width that path holds — the block's
  width less the indent, the member's room less its `│` gutter — and the answer is kept nowhere, so
  a resize re-flows the wrap and can flip the reading with no state to keep in step. A block whose
  result recorded no regions paints exactly as it did before, at every width, and the framing
  around a split body is untouched: the targetless shape's summary still closes its branch list, an
  open member still hangs its panes under its own gutter and still closes with `see less…`, and
  collapsed blocks paint no body at all. The spanned sub-agent member loop is deliberately not
  wired — it paints the `sub_agent` call's own report, and no Edit region can reach it.

- **The Split diff composer: recorded Edit regions as two panes.** `internal/tui` can now arrange
  the Edit regions a tool recorded into the wide reading the layout spec draws
  (`docs/layout/split-diff-layout.md`): the before file down the left pane and the after file down
  the right, each numbering its own file, removed lines behind `-` and inserted lines behind `+`
  starting on the SAME row with the shorter side padding, context in both panes, and one damped `⋯`
  rule only where two regions do not meet in the file's numbering — the very predicate the stacked
  reading elides by, so the two arrangements of one body claim the same elisions. A line too wide
  for its pane wraps rather than clips, its continuation rows carrying no number and no marker while
  the other pane pads, so the divider stands in one column down the whole body. The width rule is a
  property of the body rather than a terminal threshold: `splitDiffFits` measures the number gutter
  from the regions themselves and reports whether each pane can still give the code
  `splitPaneMinCols` (40) columns, asked again at every paint so a resize can flip the reading. The
  module is pure composition — regions and a width in, styled rows out, wrapped through the one
  width authority — and every expanded paint path now reads through it.

- **Edit blocks paint the change that LANDED, numbered.** A card whose tool recorded Edit regions
  (`domain.EditRegions`) now shows them as the numbered **Stacked diff** the layout spec draws
  (`docs/layout/split-diff-layout.md`): per region its context, its removed lines behind `-` at
  their before-file numbers, its inserted lines behind `+` at their after-file numbers, one
  right-aligned number gutter for the whole body, and a damped `⋯` rule only where two regions do
  not meet in the file's numbering — regions that meet paint end to end, exactly as one merged
  region would. `stackedDiffLines` is the one builder of those rows, which the two diff tools will
  render through as well. The body a call was PRESENTED with — the `-`/`+` list read off its own
  arguments — is replaced when the recorded regions arrive and kept verbatim when none do, so a
  tool that recorded nothing renders exactly as it did before. The three edit tools' outcome slot
  now words its `+A −R` from `EditRegions.Stat()` where a result carried regions, falling back to
  the argument-derived phrase where it did not, and it carries that diffstat TYPED beside the text
  so a run of edit blocks sums its aggregate from the numbers rather than parsing its own wording
  back out of them. Region lines are tool-recorded file content and cross the display seam
  escape-stripped like every other producer string, with a new structural guard test holding the
  strip to every string-carrying member of the tool card. The cross-package pin that runs real tools
  through the presenter now covers the three edit tools too, so their `+A −R` is held from the apply
  that records the regions all the way to the line the card paints.

- **`edit_existing_file` records its Edit regions at apply time.** Both forms of the tool — the
  `*** Begin Patch` hunk form and the full-content replacement — now attach the
  `domain.EditRegions` summary of what they actually wrote, through the same shared
  `okEditRegions` helper the find-and-replace pair returns through. The regions are cut from the
  file as it was READ against the file as WRITTEN, so a patch whose hunks the text-locating
  applier placed somewhere other than where the model pictured them reports where they actually
  landed, and a full-content replacement reports only the lines that differ rather than the whole
  file. The prose sentence the model reads is byte-for-byte unchanged, a refused patch still
  carries no summary, and content that writes the same bytes back attaches none either — the
  signal a host reads to keep its argument-derived list (ADR 0052). No view consumes it yet.

- **The two find-and-replace tools record their Edit regions at apply time.**
  `single_find_and_replace` and `multi_find_and_replace` now attach the `domain.EditRegions`
  summary of what they actually wrote — the changed regions with their line numbers and up to
  three unchanged context lines each side, cut by the shared `editRegions` builder from the file
  as it was against the file as written. `multi_find_and_replace` summarises the WHOLE applied
  edit rather than one summary per replacement, so two replacements landing a few lines apart
  read as the one change they are on the card. The prose sentence each tool returns to the model
  is byte-for-byte unchanged, a failed call still carries no summary, and a pair with no regions
  to cut — identical texts, or one over the diff budget — attaches none either, which is the
  signal a host reads to keep its argument-derived list (ADR 0052). No view consumes it yet.

- **`editRegions`, the one Edit-region builder the three edit tools share.** `internal/tools` can
  now cut a before/after pair into the Edit regions `domain.EditRegions` carries, derived from the
  line diff's OPERATIONS rather than from rendered diff text — one builder for all three edit
  tools, so two edit blocks can never disagree about what a region is while painting into the same
  body. Each run of consecutive changed lines becomes one region with up to three unchanged context
  lines each side, and neighbouring regions TILE the lines between them rather than overlap or
  merge: the earlier takes up to three of them as its trailing context, the later takes what is
  left as its leading context. A gap of at most six lines is therefore covered end to end and the
  two regions come out adjacent in line numbering, which is what a renderer reads to decide that no
  elision separator belongs between them. `Removed` and `Inserted` hold changed lines only, so
  `EditRegions.Stat()` equals the diffstat the same pair's unified diff counts, line for line.
  Identical inputs and a pair whose LCS table would exceed the diff budget both yield no regions —
  the renderer's argument-derived fallback — and the over-budget pair allocates nothing
  proportional to the table it refuses. Nothing calls it yet.

- **`domain.EditRegions`, the Tool summary variant the coming Split diff is painted from.**
  An edit tool can now record what it applied as data rather than leave a view to guess it from
  the call's arguments: each `EditRegion` carries the removed and inserted lines, up to three
  unchanged context lines each side, and the 1-based line the region starts on in the before and
  after file. Neighbouring changes stay SEPARATE regions whose context tiles the lines between
  them — the earlier takes up to three of them as its trailing context, the later takes what is
  left as its leading context — so a gap of at most six lines is covered end to end, the two
  regions come out adjacent in line numbering, and no line is context for two regions at once.
  `EditRegions.Stat()` is the one derivation of the `+A −R` pair — context lines never count —
  so every consumer reads the same number instead of recounting. The variant is sealed like the
  six before it (unexported marker method, additive, re-exported from the root facade) and rides
  the Tool summary contract unchanged: display data, never sent to the model, never persisted in
  the session record (ADR 0052). Nothing produces or consumes it yet.

- **A per-exchange pre-image journal (`internal/undo`), the mechanism behind the coming `/undo`.**
  Every mutation the write funnel performs can now be recorded as the bytes it replaced (the
  pre-image) plus a SHA-256 of what it left, grouped per Exchange — one instruction to the agent,
  however many tool calls it took. Groups materialize lazily, so an Exchange that wrote nothing
  never becomes a step the human has to walk past, and one entry per path per group (first
  pre-image wins, last post-state wins) encodes create, overwrite, delete and both halves of a
  move without a per-verb case. `Preview` classifies the top group against the filesystem as it is
  now — restore, delete, or skip — and `Revert` executes it through the same fenced primitives the
  tools write through (`security.SafeWriteFile` / `SafeRemove`), so an undo can never reach further
  than the write it reverses. A file the human edited after the agent wrote it is skipped with a
  reason rather than overwritten; a generation stamp lets a caller prove the journal has not moved
  between a preview and its confirmation. The journal is per process and in memory only, holds no
  redo, and is not yet wired to the engine or the TUI.

- **Undo journal capture: the engine now owns one in-memory `undo.Journal` for its lifetime**
  and opens a new group at every Exchange boundary (an interjection joins the group already
  open). The shared write funnel records a pre-image before each mutation and commits the
  record only after the write succeeds, so `write_file`, `edit_existing_file`,
  `single_find_and_replace` and `multi_find_and_replace` are all covered at one seam. The
  journal is live host state and is never serialized into a session snapshot.

- **Undo journal capture reaches the byte-moving verbs, and delegations share the parent's journal.**
  `copy_file` now records its destination (the clobbered pre-image with `overwrite: true`, pre-absent
  otherwise), `move_file` records both ends as two entries — the source with its pre-image bytes and
  post-absent, the destination with whatever it replaced — identically on the rename fast path and the
  copy-then-remove fallback, and `delete_file` records the bytes it is about to unlink together with the
  mode the file carried, so a restored script keeps its executable bit. Each half commits only after that
  half of the mutation actually landed, so a refusal journals nothing and a split move (the copy landing
  while the removal is refused) records the destination alone. A `sub_agent` child is handed the PARENT's
  journal instead of one of its own and opens no group of its own, so delegated writes join the Exchange
  the human started and a single `/undo` takes back the whole instruction, however wide the fan-out.

- **Engine surface for undo: `UndoPreview` / `UndoRevert` on the Agent (and on the TUI's `Engine`
  seam)**, with a stale-generation refusal that leaves the journal and the workspace untouched.

- **The two-step `/undo` command.** Bare `/undo` PREVIEWS what putting the last exchange's file
  writes back would do — the exchange ordinal and every recorded path at its resolved spelling,
  classified restore / delete / skip-with-reason — and `/undo confirm` executes exactly that
  preview, reporting the counts and naming every path it left alone. The generation stamped on the
  preview travels back with the confirmation, so a journal that moved in between (another exchange
  wrote files) refuses the revert and prints a fresh preview to confirm instead. It is idle-only,
  because it writes to the workspace and the group it would revert is the one a running Step is
  still filling; with nothing recorded it says so and states that the journal is per-process, so an
  empty answer on a resumed session cannot be mistaken for a lost one.

- **`/undo` is documented and the decision recorded.**
  [ADR 0051](docs/adr/0051-undo-is-a-per-exchange-in-memory-pre-image-journal.md) ratifies the
  design: the per-Exchange stack, the pre-image + post-hash record shape, capture at the shared
  write funnel as the coverage boundary itself, skip-on-conflict (the human's own later edit
  outranks the undo), the human-initiated restore that includes an approved out-of-workspace path,
  the two-step confirm with its generation guard, and in-memory-per-process with no redo — plus the
  rejected alternatives (an on-disk journal, reconstruction from the session record, a git-based
  revert, one whole-session revert, an undo tool for the model) and why each was refused. README
  gains the `/undo` command row and a section saying in the human's own words what it covers, what
  a skip means, and what is NOT undone (subprocess writes, git checkouts, MCP and embedder tools,
  and anything written before this process started). `CONTEXT.md` gains the **Undo journal** term.
  The `[P2] Undo all agent changes` entry is removed from `ISSUES.md` — this plan shipped it.

### Changed

- **The session title has one owner (`internal/tui/autotitle.go`).** A title that resolves before
  the first Save has minted an id to rename is held until one exists, and that wait used to be a
  pair of bare `Model` fields written at eight sites across four files — the naming fold, the
  save-complete flush, the failed-rename retry, `/clear` and a `/sessions` restore — with the rule
  they all had to keep living in a comment block and nowhere in the code. `pendingTitle` is a
  `titleStash` now: two fields and five verbs — `adopt` a title decided before there was a record,
  `flush` it to the save that minted the id, `restash` one the rename could not write, `drop` one
  the session it was made for has outlived, and `clobbered`, which is the never-clobber rule itself
  — so an automatic title a human has since overruled is thrown away by the same line wherever the
  stash is about to be honoured, rather than by a check re-typed at each site. The invariant is now
  testable on its own, without a whole `Model`, and is. Nothing the human sees changes: every
  existing naming test passes with no expectation touched.

- **Per-lifetime state resets itself, and a resumed session is repainted in one place**
  (`internal/tui/model.go`). "Reset the session" was a hand-kept checklist: `finishWorker` spelled
  out eleven fields of eight concerns, and `/clear` and a `/sessions` restore each kept their own
  run of assignments for the same live reading. The two groups that fall together now say so
  themselves — `liveStats` (the context gauge's fill, the generation clock, the last completion's
  throughput) and `pendingDecision` (the in-flight Approval, the in-flight `ask_user` question and
  the ticked set that rides with it), each a small value with its own `reset()`, embedded
  anonymously so every existing reader (`m.ctxUsed`, `m.pending`, `m.askChecked`, …) reads exactly
  as it did. The Exchange boundary and the two session boundaries call those resets instead of
  listing fields, so a payload added to either group is dropped by the line that already drops the
  rest. The byte-for-byte scrollback replay written twice — once for `--resume` at construction,
  once for the `/sessions` restore — is one shared `replayScrollback` now, so both ways into a
  stored session word the resume note, the no-scrollback degrade and the interrupted note
  identically by construction rather than by hand. Nothing the human sees changes: the whole suite
  passes with no test line and no expectation touched. The two asymmetries between the boundaries
  are preserved exactly and each is now documented where the reset happens — `/clear` still leaves
  the session's cumulative token accounting standing (recorded as a deferred defect, not endorsed),
  and a restore still leaves `titleTouched` as it found it, which is what keeps the outgoing
  session's late automatic title from naming the record just reopened.

- **The llama-launcher seam is one named host interface (`internal/tui/tui.go`, ADR 0054).**
  `Options` carried seven one-purpose bare `func` fields for what is one thing a machine either has
  or has not — `LaunchProfiles` / `LoadProfile` / `UnloadServer` / `StopServer` /
  `LauncherEnabled` / `RecordLaunchProfile` / `RestoreProfile` — so `/model`'s choice of offering,
  both actuation verbs, the load recording and the boot restore each read their own nil check for
  the one question "is there a launcher here". They are `LauncherHost` now, declared beside
  `ServerHost`, `SettingsHost` and `SchemeHost`: `Enabled`, `Profiles`, `Load`, `Unload`, `Stop`,
  `RecordProfile`, `Restore`, with one doc comment stating what the family is and who owns what
  behind it, and `Options` down from 53 fields to 47. The composition root implements it with one
  adapter over the verbs that already existed (`cmd/apogee/launcher.go`), and the renderer's tests
  get one fake for the family, wired one act at a time, in place of seven fields set individually.
  The nil-means-unwired contract is preserved whole: a nil host is a machine with no launcher —
  `/model` offers what the server itself advertises, both actuation verbs answer the one sentence
  naming the key, nothing is recorded and no boot restore is attempted — a wired host says the
  integration being switched off from inside each verb (`ErrNoLauncher`, ADR 0037), and the one act
  the renderer decides ABOUT before it performs one — does this host answer the start-up restore at
  all — is reported by `Acts() LauncherActs` rather than attempted, because issuing the Cmd would BE
  the act (ADR 0054 decision 3a). Behaviour is unchanged: every degrade the seven nil funcs produced
  is produced by the nil host, by an act's own answer, or by that flag, and the whole suite passes
  with no expectation changed.

- **The Upstream seam is one named host interface (`internal/tui/tui.go`, ADR 0054).** `Options`
  carried six one-purpose bare `func` fields for what is one thing a host either has or has not —
  `Heartbeat` / `Rebind` and `Servers` / `SwitchServer` / `BindServer` / `RecordServerChoice` — so
  the `/server` verb, the pre-bound ask and the heartbeat's whole tick chain each read their own
  nil check for the same question. They are `ServerHost` now, declared beside `SettingsHost` and
  `SchemeHost`: `Beat`, `Rebind`, `List`, `Switch`, `Bind`, `RecordChoice`, with one doc comment
  stating what the family is and who owns what behind it, and `Options` down from 58 fields to 53.
  The composition root implements it with one adapter over the verbs that already existed
  (`cmd/apogee/wire_server.go`), and the renderer's tests get one fake for the family, wired one act
  at a time, in place of five fields set individually. The nil-means-unwired contract is preserved
  whole: a nil host is the pre-heartbeat, pre-ADR-0036 renderer, `List` says a wired host has
  nothing to offer by naming nothing and `RecordChoice` by answering false, and the four acts the
  renderer decides ABOUT before it performs one — is anything observing, can a change be applied,
  can this session switch, can a pre-bound session bind — are reported by `Acts() ServerActs`
  rather than attempted, because calling to find out would BE the act (ADR 0054 decision 3a).
  Behaviour is unchanged: every degrade the six nil funcs produced is produced by the nil host, by
  an act's own answer, or by a flag, and the whole suite passes with no expectation changed.

- **The settings and colour-scheme seams are named host interfaces (`internal/tui/tui.go`, ADR
  0054).** `Options` carried seven one-purpose bare `func` fields for two families — `SettingsRows`
  / `WriteSetting` / `ResetSetting` / `ApplySetting`, and `ListSchemes` / `ResolveScheme` /
  `ExportScheme` — so the seam between the binary and the renderer grew one field per host
  capability, exactly as fast as the implementation behind it. They are `SettingsHost` and
  `SchemeHost` now, declared beside the five interfaces (`Engine`, `SkillCatalog`, `SessionHost`,
  `RecallHost`, `Scheduler`) that already proved the shape: one doc comment stating what the family
  is and who owns what behind it, one per method, and `Options` down from 63 fields to 58. The
  composition root implements them with two adapters that hold what the acts need — the resolved
  options, the config path, the external-edit baseline and the apply dispatcher for the settings
  side; the schemes folder for the other — instead of seven closures over the wiring
  (`cmd/apogee/wire_options.go`), and the renderer's tests get one fake per family, wired one half
  at a time, in place of seven fields set individually. The nil-means-unwired contract is preserved
  whole: a nil host is the family unwired, and a wired host that cannot do one of the acts says so
  in that act's own answer, so every degrade a `/settings` row or a `/color-scheme` line used to
  print it still prints, word for word. Behaviour-preserving throughout; the whole existing suite
  passes with no expectation changed.

- **The sub-agent run head is one question with a name (`internal/tui/transcript.go`).** "Is this
  entry the call block a delegation's run hangs off" was spelled inline twelve times across four
  files — the kind, the retained tool name, and whichever of `done` or the spawning call id that
  site also cared about — while the one predicate that had been extracted had a single caller. Three
  named questions replace them all: `headsRun` (the head itself), `opensRun` (a head whose result
  has not been paired in yet) and `headsRunFor` (the head one spawning call id names), with the
  name half of the rule sitting on the tool card as `toolView.headsRun` — which is the form the
  transcript codec re-derives a replayed record's solo mark through. The `done`-versus-phase
  distinction each `!done` site had to remember — a delegate that reported first is still an OPEN
  head until its siblings' results burst in beside its own (ADR 0039) — is written once now, on
  `opensRun`, beside the pointer to `subAgentReported`, which is the question that reads the phase
  instead. New unit tests pin all three predicates against the conjuncts the sites used to spell.
  Behaviour-preserving throughout; nothing the user sees changed.

- **`entryKind` answers for itself (`internal/tui/entrykind.go`).** Six kind-keyed rules lived in
  four files and had to be remembered together: the wire name map and its inverse in the transcript
  codec, the block-state gate and the host-note test in the scrollback model, the paint cache's
  cacheability gate, and the renderer's two inline comparisons for the live star and the prompt
  stop. They are one behaviour table on the kind now — `persistedName`, `carriesBlockState`,
  `isHostNote`, `cacheable`, `hasLiveStar`, `isUserPrompt` — and each site asks the kind rather than
  comparing against it. The enum moved into that same file, so a new entry kind is a const row with
  its table row beside it plus a case in the paint switch, which stays a switch because a painter is
  code and not a fact. A new structural test parses the const block off disk and fails on any kind
  with no row, the way `TestFoldEventCoversEveryEventVariant` already did for Event variants, and a
  second pins that no two kinds claim the same wire string. The codec's documented unknown-kind
  degrade path is untouched and nothing the user sees changed.

- **The "/" table carries its own behavioural policies (`internal/tui/command.go`).** Three facts
  about a verb lived as string lists somewhere else: which verbs open an Exchange (a
  `continue`/`compact` comparison in the command runner), which verbs touch the server (a hardcoded
  six-name list in the actuation latch), and which verbs have an argument grammar of their own (a
  second switch beside the parser, keyed by name all over again). All three are now declared on the
  verb's own row — `opensExchange`, `touchesServer`, and a `parseArgs` hook — and each satellite
  reads the table instead of naming verbs. The latch reads the two flags together, so a future verb
  that opens an Exchange is refused mid-restart by declaring the one flag it already needs rather
  than by being remembered in a second place. `parsedInput` carries ONE opaque argument value in
  place of one typed field per arg-taking verb: the four verbs with a grammar (`/confine`,
  `/color-scheme`, `/effort`, `/undo`) put their parse on it whole, and the runner reads it back as
  its own type. Adding a verb that opens an Exchange or moves the server used to cost nine or ten
  edits and failed silently when the ninth was forgotten — the verb ran into a dead upstream or a
  held latch and nothing said so; it costs three now, and three new tests pin each set by name where
  nothing pinned them before. Behaviour of all 21 verbs is unchanged.

- **Every substantial `Update` arm now delegates, and the key-claim order is a list rather than a run
  of guards (`internal/tui/model.go`).** Six arms already handed their message to a named fold in the
  file that owns the concern; twelve more inlined twenty to thirty lines of state machine in the
  switch itself. Those twelve now read `return m.foldX(msg)` too, with the fold — and the reasoning
  that explains it — living beside the code it belongs to: the mode report with the width authority,
  the paste, the widget fall-through and the keyboard-enhancement report with the prompt editor, the
  approval request with the approval pane, the beat and the routing notice with the heartbeat, the
  compaction with the command runner, the spinner tick with the spinner, the wheel with the pointer
  code, and the cancel and the loop fault beside the worker lifecycle they close. `Update` is 262
  lines rather than 459, and every arm now reads as what it is: a statement of which fold owns this
  message. One rung down, `handleKey`'s seven sequential "does
  overlay X claim this key?" guards — whose order is load-bearing and was stated only across seventy
  lines of comment — became one ordered list of claimants (`keyClaimOrder`), each entry carrying the
  reason it sits where it does, walked by one loop (272 → 192). A pane that does not claim still
  keeps nothing, and the block cursor still keeps the walk it leaves behind, exactly as the guards
  arranged; a new test pins the sequence, so a reorder can no longer happen by accident.

- **The frame publishes where each pane landed (`internal/tui/model.go`).** `View`'s composer knew
  every block's row span while stacking it and threw it away, so three near-identical prefix sums in
  the pointer code rebuilt it — one per pane, on every click and every wheel notch, each re-rendering
  every overlay of the frame to do so. The transcript-side slot is now stacked ONCE, in the single
  order the frame states for it (`transcriptSlotPanes`, now beside `View` with the placement reasons
  that were scattered through its appends), and that walk publishes each pane's `[y0, y0+h)` span.
  The /settings pane's rectangle and both reports' are lookups into it rather than three sums that
  merely agreed with each other. A pointer gesture also composes the frame once instead of once per
  rectangle: the pre-click snapshot carries the geometry it was aimed at — a plain bool and a
  fixed-size array of ints, so the value-copied `Model` needs no exception to ADR 0011 — and the
  chain reads it thereafter. Behaviour-preserving: every mouse hit-test passes unchanged.

- **`settings.go` is three files along its own seams.** The /settings pane was 2,482 lines because
  five clusters with nothing in common but the pane struct hung off it. Two files' worth moved out.
  `settingswatcher.go` is now the two ways the config file changes from OUTSIDE the pane — a row's ⏎
  opening the human's own editor on that key's line, foreground or detached, and the binary's watcher
  reporting a save made anywhere else — one round trip with two triggers, both re-reading through the
  same seam and both landing through the same apply loop. `settingsapply.go` is what a committed key
  then does: the armed reset, the write, the live-apply router that turns a persisted value into an
  effect, and the session's edit journal a row's ` *` marker is read off. What stays in `settings.go`
  is the pane itself — its kinds, its keys, its targets and its rendering — and the display projection
  stays in `cmd/apogee` (ADR 0035/0037), untouched. A byte-identical move: the two moved spans were
  diffed line-for-line against the file they left, no call site changed, no test changed, and only
  the two new files' header banners and `doc.go`'s two new map lines are new text.

- Merged the /settings pane's two twin pairs (`internal/tui/settings.go`). The enum vocabulary and the
  Mechanism catalogue paint through one sub-list painter that states the pane, the title, the body
  naming the key, the menu shape and the row window once — each content passes only its rows and its
  legend, which is the whole of what they disagree on. The one-line value buffer and the multi-line
  prose field route their keys through one edit contract (esc abandons; everything else goes to the
  field) and persist through one commit. Each typed state keeps its own commit key and its own trim
  exactly as they were — ⏎ with TrimSpace for a scalar token, ctrl+s with TrimRight of newlines for
  prose, including the two different verdicts on what counts as an empty edit — and each is claimed
  and documented at the call site rather than folded into the shared body. Behaviour-preserving:
  nothing either sub-list paints and nothing either edit commits changed.

- Adopted the shared list cursor in the two decision panes — the approval menu and the ask_user
  offering (`internal/tui/listsurface.go`, ADR 0053): the last two non-wrapping arrow idioms the
  review counted are gone, and with them five hand-written clamps around the highlight. Both
  selections are `listCursor` values now, walked with `listStopsAtEnds` — the stated argument that
  says, at the call site, why a security surface does not let ↑ on the first row jump to Cancel.
  They take the walk and the clamp WITHOUT the module's key contract, because both panes are
  soft-modal: the transcript stays scrollable under the approval prompt and the answer box stays
  typeable under the question, so which keys each claims stays its own switch's. Behaviour-preserving
  — every key both panes answered, they answer exactly as before, and no key they ignored became
  theirs.

- Adopted the shared list surface in the /settings pane (its key list and both value sub-lists) and in
  the "/" | "@" autocomplete dropdown (`internal/tui/listsurface.go`, ADR 0053): the wrap-arrow idiom
  written four more times, a third `clampSelection`, a second clamp-to-highlight and three more copies
  of the budget→render boilerplate all collapse into the one set. The surface split into a
  `listCursor` every list embeds and the `listSurface` the two filtering panes embed instead, so a
  pane that never types into a filter no longer carries a text widget it does not use — the `Model`
  measured 104,592 bytes before and after. Behaviour-preserving: every pane answers every key exactly
  as it did.

- Introduced `listSurface`, the one list state and key contract every filtering overlay is built on
  (`internal/tui/listsurface.go`, ADR 0053), and adopted it in the /model | /server picker (all seven
  kinds) and the /sessions browser (all three modes). The clamp written twice, the wrap-arrow idiom
  written four times, the type-to-filter block written twice byte-for-byte and the budget→render
  boilerplate written twice collapse into one set; what ↑/↓ do at the ends of a list is now a stated
  parameter instead of an idiom re-spelled per pane, and "↓ at the bottom of a filtered list" is
  directly unit-testable. Each pane keeps its rows, its accept and its wording. Behaviour-preserving —
  every pane answers every key exactly as it did.

- Routed the picker's filter, the /sessions browser's filter and its inline rename buffer through
  `lineEditor`, the package's one text field: the hand-written rune backspace/append written five
  times is gone, and the caret glyph is now a per-field construction parameter (the filter line's
  `▌`, the rename row's and the /settings value row's `▏`) instead of a string each painter appended.
  Behaviour-preserving — every pane draws the caret it drew before.

- **The /usage report and the /inspect pane are one pane now.** Both are the same read-only overlay —
  a scrolled row list, esc closes it, a click outside dismisses it, a click inside is swallowed, the
  wheel scrolls it — and it was written twice: two state structs, two key contracts, two dismisses,
  two budget→render paths, and in `mouse.go` two copies each of the pane rectangle, the visible
  window, the click and the wheel, whose comments said so ("It is usagePaneRect one slot further
  down"). One `reportPane` value and one set of functions (`reportpane.go`) now answer for both,
  parameterised by which report is being asked; `usage.go` and `inspector.go` keep what only they
  know — their rows, their titles and their verbs — and name the shared body for their own pane. The
  frame's transcript-side stacking order, which the two copies each hardcoded as an `above` slice
  that already differed by one element, is stated once as the list every rectangle in that slot
  walks, so the next pane to join the slot joins one list rather than being remembered into several.
  Nothing on screen or under the pointer changes.

- **A tool call's outcome stat is carried as a typed value.** The stat a card's outcome slot shows —
  a counted noun or a diffstat (`statValue`) — now travels beside the phrase it spells, so a group's
  type row ADDS its members' stats up instead of parsing the wording back out of their slots.
  `parseDiffCounts`, `sumDiffCounts`, `sumCountPhrases` and `statPhrase` are gone, the registry's
  stat hooks answer in values, and the value rides the session record so a resumed transcript totals
  its runs exactly as the live one did. Rendered text is byte-identical. The record member is
  additive on the transcript's own rule — no version bump, a slot with no arithmetic writes not one
  extra byte — and a session written before this carries the phrases alone, so its grouped type rows
  reopen with a blank total rather than one read back out of prose.

- **One painter draws all five tool-body frames.** The rows a call's detail lines become were laid
  out by five hand-written loops — the targetless shape's ┝/┕ branch list open (`renderDetails`) and
  collapsed (`clipDetails`), an ungrouped call's body at its branch marker's indent
  (`renderSubDetails`), an open group member's under its │ gutter (`renderExpandedMember`) and an
  open sub-agent member's under the same (`renderSubAgentMemberRows`) — each spending the same three
  wrap primitives in its own way, and three of them separately deciding whether the call's recorded
  Edit regions should be read as two panes instead (ADR 0052). Each frame is now a value
  (`bodyFrame`) stating what leads a detail line, what continues it, which tone it takes and how many
  rows one line may spend, and one painter behind them spends the primitives in that shape; the
  split-vs-stacked choice is made once in front of that painter (`paintToolBody`) rather than at each
  path that can reach it, so a sixth path cannot arrive with a sixth answer. Nothing on screen
  changes — every frame keeps its exact lead, gutter, indent, clip and tone, and a new per-frame test
  pins each of the five against the primitive calls its own loop used to make.

- **`doc.go` stops claiming the tool cards read no prose at all.** The narration of the post-v0.8
  presenter deepening — facts arrive as data, only the wording is the view's — read as if the
  free-text parsing it replaced had gone entirely, while `toolregistry.go` has always been honest
  beside the hooks that it did not: six stat hooks (`testVerdictStat`, `foundFilesStat`,
  `changedFilesStat`, `commitCountStat`, `commitHashStat`, `diffLinesStat`) still word their slot
  off a fixed header the tool writes into its own output, because design call 14 rules out growing
  the engine for presentation. The package narration now says so in both places it summarises the
  stat hooks, and says what makes the residue safe: each reading is anchored on a token the tool
  formats deliberately and each hook is total, so an unrecognised shape returns false and leaves
  that tool's prose floor in the slot rather than a wrong number. Documentation only — not one
  hook, regex or rendered byte changed.

- **The escape-stripping security seam is its own file, `internal/tui/sanitize.go`.** `stripEscapes`,
  the batch form `stripEscapesAll` and the `bidiControl` set they drop beside the control characters
  were filed under `transcript.go`'s "Formatting helpers" banner, which is where they grew up rather
  than where they belong: nineteen files in the package call them, the scrollback is only one of
  those callers, and nothing in the three functions knows a transcript, an entry or a `Model`. They
  now sit in a file of their own, as the package's second invariant — untrusted text is
  escape-stripped at the SEAM it enters the view through, never at each producer — deserves. A
  byte-identical move: only the new file's header banner is new text, no call site changed, the
  tests stay where they are, and `doc.go`'s file map and that invariant both point at the new home.

- **`toolpresent.go`'s tail splits into `internal/tui/toolargs.go` and
  `internal/tui/textutil.go`, and the file is deleted.** The last two modules in it had nothing to
  do with each other: the JSON-argument display module — `argumentDetails`' labelled lines, the
  last-wins duplicate-key reading, the per-value line cap and `resolvedPathNote` — is now
  `toolargs.go`, the one rendering both the approval prompt and an unregistered call's transcript
  block read; the generic text helpers (`clipDetail`/`clipRunes` and the flood bound
  `detailClipRunes` states, `plural`, `firstLine`, `splitLines`), called from seven files that
  share nothing else, are now `textutil.go`. Both spans are byte-identical moves; only each new
  file's header banner is new text. That closes the four-seam split ADR 0043 asked for: the 2,514
  lines that were `toolpresent.go` are now `toolview.go`, `toolregistry.go`, `diffbody.go`,
  `toolargs.go` and `textutil.go`, and `doc.go`'s file map names the two new ones.
- **Every prose pointer that still named the deleted file now names its real home.** The file's
  deletion staled pointers written long before this plan, so they are corrected in the same commit:
  `approval.go` and `paint_test.go` at `detailLine` and the promoted outcome slot (→ `toolview.go`),
  `toolsummary_pin_test.go`, `activity.go`, `markdown.go` and three `doc.go` sentences at the stat
  hooks, the registry and their pure/table-testable posture (→ `toolregistry.go`), `transcript.go`,
  `popup.go` and `internal/agent/resolution_test.go` at the argument label, the value cap and the
  duplicate-key reading (→ `toolargs.go`), `paint_test.go` at `detailClipRunes` (→ `textutil.go`),
  `toolshape_test.go` at the files that decide a call's on-screen shape, and the four `ISSUES.md`
  pointers into the git-diff walk and the stacked row (→ `diffbody.go`, with fresh line numbers).
  ADR 0052 gains a dated amendment recording that its `changedLines` pointer now reads
  `diffbody.go`; ADR 0011's 2026-07-25 note says inline that the presenter it names was split and
  deleted. Historical records — the changelog's own past entries, archived plans, and the saved
  reviews pinned at a commit — keep the name they were written with.

- **The diff-body cluster moves out of `toolpresent.go` into `internal/tui/diffbody.go`.** The
  last of the four modules that shared one file name (ADR 0043), moved verbatim beside
  `splitdiff.go`, the composer that reads the same regions the wide way. What moves is every BODY
  a change-shaped call renders: the three edit tools' bodies derived from the call's own arguments
  (`changedLines` over `editPair`s — `singleReplacementBody`, `multiReplacementBody`,
  `fileEditBody`, `writtenLines`, and the `*** Begin Patch` reader under them), the two diff
  tools' regions walked back out of the output they print instead (`viewDiffRegions` through
  `diffRegionCutter`; `gitDiffRangeRegions` through `gitDiffWalk`, which reads its numbers off the
  hunk headers git elides the gaps between), `stackedDiffLines` and the `stackedRow` family that
  build the narrow reading both readings share their regions with, and `viewDiffBody`, the
  coloured prose that stays view_diff's floor for output carrying no tags. `toolpresent.go` is
  left with its tail alone — the JSON-argument display module and the generic text utilities — at
  336 lines, down from 1082. No signature changed, no call site moved, and the test files stay
  where they are. `doc.go`'s file map gains the new file's line. No behaviour change.

- **The presenter registry and the per-tool hooks move out of `toolpresent.go` into
  `internal/tui/toolregistry.go`.** The third and fourth of the four modules that shared one file
  name (ADR 0043), moved verbatim and never reshaped — the registry is the package's one open,
  name-keyed table and adding a tool stays a single edit. What moves is the presentation
  vocabulary the card lifecycle reads: the file's own opening note, the `toolPresenter` type with
  its eight hooks, the `toolRegistry` table itself, the outcome-slot stat hooks that word each
  tool's right-hand slot off the typed `domain.ToolSummary` it already reports, the target
  extractors that read the call's own arguments, and the detail and body extractors that stay the
  floor for a result carrying no summary. The diff-body cluster stays behind for the next move,
  and so does the tail — the JSON-argument display module and the generic text utilities — leaving
  `toolpresent.go` at 1082 lines, down from 2291. No signature changed, no call site moved, and
  the test files stay where they are. `doc.go`'s file map gains the new file's line. No behaviour
  change.

- **The tool card and the lifecycle that fills it move out of `toolpresent.go` into
  `internal/tui/toolview.go`.** The first two of the four modules that shared one file name (ADR
  0043), moved verbatim as two contiguous spans anchored on the file's own section banners. The
  card value type comes first — `toolView` itself, the `detailLine`s a `detailKind` colours, the
  `toolBody` that carries them, the `branchSummary` that says whether the outcome slot holds the
  view's own wording or a line the tool printed (quoted), and the `toolOutcome` a prose extractor
  returns. The view lifecycle follows: `presentToolCall`, which builds the header the moment a
  call is seen (and, for the tools whose arguments already say what the call will change, its body
  too), `enrichWithResult`, which absorbs the result when it lands, the `finishDisplay` pair both
  leave through — `sanitize` for the escape-strip and `shortenPaths` for the workspace-relative
  spelling of the paths a card names — and the run aggregation above them (`runAggregate` and the
  sums under it). `toolpresent.go` keeps the presentation vocabulary the lifecycle reads: the
  presenter registry and the per-tool stat/target/body hooks, and finishes at 2291 lines, down from
  3313. No signature changed, no call site moved, and the test files stay where they are.
  `doc.go`'s file map gains the new file's line. No behaviour change.

- **The box and join paint primitives move out of `model.go` into `internal/tui/boxdraw.go`.** The
  sixth concern carved off the coordinator file (ADR 0043), and the one that sits beside `wrap.go`
  because it is the same kind of thing: the low-level primitives every painted surface finishes in.
  Six functions moved verbatim as one contiguous span — `squareLine` and `squareOnField`, which
  square a composed line to an exact painted width in the width authority's measure rather than
  lipgloss's (ADR 0030); `drawBox` and `drawTitledBox`, the one border assembly the startup card and
  the popup pane are both drawn in, title-in-border or not; and `joinScrollbar` and `joinFrame`, the
  two joins that hang the transcript's gutter off its right edge and stack the frame's blocks into
  the one string `View` hands bubbletea — each standing in for the lipgloss join it replaced, for
  that same measure reason. Signatures are unchanged and no caller moved: `popup.go`,
  `userblock.go`, `startupbox.go` and `View` call exactly what they called before. `model.go`
  finishes at 3027 lines, down from 3187. `doc.go`'s file map gains the new file's line. No
  behaviour change.

- **The `ask_user` pane moves out of `model.go` into `internal/tui/ask.go`.** The fifth concern
  carved off the coordinator file (ADR 0043), shaped after `approval.go` next door: both halves of
  the surface in one file, so a choice can never be paintable and unreachable. The state half is
  the fold that borrows the input box for a question (`foldAskRequest`, the `askReqMsg` arm's body
  now named, with the arm reduced to the delegation the other folds already use), the keys the
  offering claims while that box is still empty (`askChoiceKey` — ↑/↓ over the choices and ␣ on a
  multi-select row, previously written inline in `handleKey`), and the reply path that hands the
  answer back and gives the box up (`submitAnswer`, `checkedLabels`, `restoreAskDraft`). The paint
  half is the pane itself (`askPrompt`, `askChoiceRows`, the checkbox glyphs, and the row/line
  budgeting around them — `maxAskChoiceRows`, `askRowGap`, `askQuestionFloor`,
  `askAnchorRowLines`). Everything moved verbatim except the two call sites, which had to become
  named methods for the move to happen at all: `askChoiceKey` states its guard as an early return
  and reports whether it CLAIMED the key, the way `usageKey` and `inspectorKey` already do, so
  `handleKey` reads the ask pane's claim in the same shape as its neighbours'. The `Model`'s own
  fields (`pendingAsk`, `askSel`, `askChecked`, `askDraft`) stay in `model.go`. `model.go` finishes
  at 3187 lines, down from 3537. `doc.go`'s file map gains the new file's line. No behaviour
  change.

- **The upstream-heartbeat cluster moves out of `model.go` into `internal/tui/heartbeat.go`.**
  The fourth concern carved off the coordinator file (ADR 0043), after the record-write, approval
  and command-running clusters before it: the 492 contiguous lines that hold the heartbeat end to
  end (ADR 0024) — `heartbeatState` and `rebindIntent`, the offline-debounce threshold and the
  notes it words (`offlineNote`, `onlineNote`, `rebindNote`, `rebindFailNote`, `windowWord`,
  `serverSwitchNote`, `unknownWindowNote`), the tick chain (`heartbeatLive`, `beatCmd`, `armBeat`,
  `beatTick`), the folds a beat, a failure or a `/server` switch lands in (`foldBeat`,
  `foldBeatFailure`, `foldServerSwitch`), the re-binding an advertised model earns at a quiescent
  boundary (`observeBinding`, `applyRebind`, `applyPendingRebind`) and the send gate the offline
  state is spent on (`blockedUpstream`, `upstreamBlockNote`) — now sit in one file named for what
  they are. A pure same-package move: not one line of the cluster is reworded or reordered, the
  section banner travels with it, every call site is untouched, and `model.go`'s import of
  `internal/heartbeat` goes with the code that used it. `model.go` finishes at 3537 lines, down
  from 4031. `doc.go`'s file map gains the new file's line. No behaviour change.

- **A split diff's band now reaches the edge of its pane.** The two-pane reading was the one frame
  the full-width rule did not touch: it paints its own rows rather than going through a wrap rail,
  so a changed line's tint still stopped at the last glyph while every stacked and flat body around
  it had already squared up. Each pane now fills a line out to the pane's edge from INSIDE the
  style, on the first row and on every wrapped continuation alike, and the band opens at the marker
  column on both — so a wrapped line is one unbroken block of colour instead of a band that steps
  right on every row after the first. The number gutter beside it stays chrome, outside the tint,
  as it does in every other frame. Filling is now the cell's own business rather than the row's:
  the right pane squares up exactly as the left one always did, which is what lets its band reach
  the edge, and the divider between them stands in the column it always stood in — measured, as
  before, in the width authority's measure (ADR 0030), so a wide glyph in the code cannot walk it
  sideways.

- **A diff line's band now runs the full width of its block, and only under the line's own text.**
  The tint used to stop at the last glyph, so a body of unequal lines came out with a ragged right
  edge and a short line said nothing at all in the columns past its text — the very columns the
  signal had just been moved onto. Every wrap rail in the transcript now fills a line out to its
  rail INSIDE the style whenever the style carries a background: the first row and every wrapped
  continuation of it reach the same column, under trailing space and continuation text alike. The
  chrome that leads the row is held OUTSIDE the band — an open member's `│` gutter, a branch list's
  `┝`/`┕` elbow, the blank indent a continuation row hangs under — so the band starts where the text
  does and reads as the text's field rather than the frame's. The pad is counted in the width
  authority's measure (ADR 0030), so a wide glyph costs the two cells the painter spends on it, and
  it is laid down before the style is past the line, so it sits inside the colour run rather than
  showing the terminal's own background through the gap. A style with no background renders the
  bytes it always did, which is what keeps every other wrapped surface in the transcript out of this.

- **A diff line's colour moved to the background.** An added or a removed line no longer paints its
  text turquoise or red: it wears the same detail tone as every other line of its block — the
  collapsed dim, the brighter step once you open it — and sits on a quiet BAND that says which way
  it went (`diff-add-bg` / `diff-del-bg`). The band is the same in both states, because which way a
  line went is not a thing an opened block says more loudly; the tone step and the direction now
  have a surface each instead of sharing one. The `-`/`+` markers are unchanged and still carry the
  change on their own, glyphs riding the band, so nothing is lost on a monochrome pipe or a
  copy-paste (ADR 0052's 2026-08-19 amendment supersedes the split-diff plan's "the marker travels
  with the text's colour"). The band runs the full row width in a following change; this one moves
  the signal.

- **A "+" line is turquoise now, and the ✓ beside it.** Both shipped schemes move `diff-add` out of
  green — dark `#2dd4bf`, light `#0f766e` — and leave `diff-del` red, so the one pairing a diff body
  leans on hardest survives red-green-weak vision (ADR 0052 §4; the markers still carry the change on
  their own, so the color is never the only signal). `success`, the ✓ on a finished sub-agent and
  every other "this came off" marker, follows `diff-add` into the turquoise family and keeps the
  visible step its comment always promised — dark `#5eead4`, a step brighter, light `#115e59`, a rung
  darker — so a marker still never reads as a diff line, and the whole UI now speaks one pairing:
  turquoise for came-in and came-off, red for went-out and failed. No new scheme keys, no other role
  retuned, and a scheme of your own that names `diff-add` or `success` keeps overriding both exactly
  as before.

### Fixed

- **A click in the band the `/inspect` pane grows into now falls through instead of being swallowed.**
  The `/usage` report and the `/inspect` pane are the one pair that can be up together, and they share
  one bottom-anchored slot: dismissing the report under a click grew the pane UPWARD — past the rows
  the report was drawn on, whenever it was drawn shorter than its grant — before the pane resolved its
  own rectangle, so the regrown box claimed a point the human had aimed at the blank gap row above the
  report. The whole `handleMouseClick` dispatch chain now answers every geometry question from ONE
  pre-click frame — the Model value as it stood when the button went down (the Model is a value,
  ADR 0011, so the copy is the snapshot) — while every state predicate and every mutation stays on the
  live model. A click landing where a dismissed pane was drawn now only dismisses: it is never claimed
  by the box that grew there, and it names no transcript row the pre-click frame did not show at that Y.

- **The Inspector's "no response recorded" note no longer lands under a request whose reply the
  ring did record.** The note's successor rule now applies within one wire stream — the
  `(depth, callID)` pair every event carries — so a parallel fan-out's interleaved records are no
  longer braided into one sequence. Unrouted concurrent sub-agents share their parent's client and
  wire tap and therefore its stream key; that residual is documented at the pairing function.

- **`TestResolvedPathRidesTheCallAndTheApproval`'s "a path that names its own target discloses
  nothing" case no longer fails on macOS.** The case asserts that an ordinary write — one whose
  argument names its own target — carries no `ResolvedPath` disclosure, but it took its workspace
  root straight from `t.TempDir()`, which on macOS hands back a `/var/…` path reached through the
  `/var` -> `/private/var` link. The resolver faithfully disclosed that redirection, so the case
  failed on an artefact of the host's temp dir rather than on anything the call did. The root is now
  resolved once in the case's setup (`realPath`, the helper the sibling cases already use), so the
  no-symlink condition the case is named for is actually true on every host. Production disclosure
  logic is untouched and the assertion stays exact: dropping `ResolvedWriteTarget`'s
  `Real == Named` guard still fails the case.

- **A `/settings` edit of `url-safety.allow-hosts` or `url-safety.deny-hosts` now reaches the running
  session.** Both keys fell to the dispatcher's default refusal, so the row reported
  `saved — live apply failed: …` over a value the file already carried. They now take the same door
  `tools.disabled` takes: the two host lists ride the live tool set's build spec, an edit rebuilds
  the registry — and with it the `security.URLGuard` every network tool is handed — and hands it to
  the engine through `Agent.SwapTools`, so the row reports the roster's own boundary
  ("applies to the next request") instead of a failure. An emptied key resolves the built-in default
  a fresh start resolves (the empty list, no tightening), and the guard's SSRF floor is unchanged
  either way — it is not reachable from configuration.

- **A `/settings` edit of `ui.inspector` or `response-reserve` no longer reports a failed apply.**
  Both keys are `Editable` with nothing behind them to move — the Inspector's wire observer is
  installed while the provider client is constructed, and the reply share is read into the budget
  the session opens with — so an edit fell to the dispatcher's default refusal and the row said
  `saved — live apply failed: …` over a write that had done exactly what the key promises. They now
  take the `editor` key's answer instead: the write is the whole of the apply, the row shows no note
  and no failure, and the deferral contract ("takes effect at the next start") is stated in each
  key's own `/settings` Description — `response-reserve`'s gained the sentence `ui.inspector`
  already carried. A new guard test drives every `Editable` registry key and fails on one that is
  neither renderer-owned, pane-intercepted, nor accepted by the dispatcher, so a future key cannot
  ship a lying row.

- **A `url-safety:` allow-hosts / deny-hosts entry written as a bracketed IPv6 literal (`[::1]`) now
  matches.** `normalizeHostName` strips one surrounding bracket pair before the IDNA mapping and the
  root-dot loop, so a configured entry lands in the same normal form as the bracket-free host the
  transport dials; the unbracketed spelling is unchanged, and the strip is a no-op for
  `NormalizeURL`, whose input never carries brackets. The four places that state the entry format
  (the `config.yaml` template comment, the settings registry, `urlSafetyConfig` and `domain.Config`)
  now say an IPv6 address is written in brackets.

- **The Inspector now says when a reply was not recorded instead of leaving the reader to infer one
  that was lost.** `/inspect` is a flat log, so a request record with no response under it read as a
  response that never came — while a non-streaming success body is decoded straight off the
  connection and never captured, by the provider's pinned design. A request record the ring went on
  PAST without recording an answer now carries one note row under its payload,
  `· no response recorded — a non-streaming reply is decoded off the connection`, in the same plain
  row kind the elision marker uses. The newest record never carries it, whatever it is: its call may
  still be in flight.

- **`/inspect` answers the mouse, as the `/usage` report it is shaped after already did.** The pane
  had keys and nothing else, so the one pointer gesture it understood was the transcript scroll
  behind it. A click **outside** the box now dismisses it — and, the pane being non-modal, that click
  still lands where it was aimed, seating the caret in the prompt or starting a transcript selection
  exactly as it would with no pane up — a click **inside** does nothing and is swallowed, so a press
  on the raw-protocol view cannot drag a selection across the transcript drawn under it, and the
  **wheel** scrolls the record list one row per notch, clamped at the first row and the last full
  window: the same two ends `↑`/`↓` and `PgUp`/`PgDn` stop at. Every notch reads the window the
  painter actually drew rather than a counted-up offset, so a stale scroll — a ring that gained a
  record, a grant that shrank — corrects itself on the first turn of the wheel instead of drifting.

- **A host note landing while a sub-agent is working no longer cuts that run in two.** A note —
  `· cancelled`, a resume notice, an approval record — and a Schedule's Firing block are worded the
  moment they happen and appended at the end of the transcript, which is routinely the middle of a
  delegation's run. Everything the run still had to say then landed BEHIND the note: the sibling
  delegation announced next was no longer adjacent to its own fan-out, so one group of delegates
  rendered as two, and a delegated entry whose event carries no call id fell outside its head's span
  and so outside a collapsed run's elision, railed to nothing. `transcript.place` now steps a
  tail-bound entry over the notes parked at the end when that entry continues the run or fan-out
  group they interrupted, so the notes slide to the tail until the run and its group close. The note
  itself is unchanged — same depth-0 unrailed block, same text, same chronology inside it: only its
  position in the list moves, and only past the open stretch. A note answers the human, so it stands
  after the work it interrupted and is never railed into a delegate's run (contrast a presented
  document, which carries the delegate's identity and goes inside).

- Documentation: corrected the undo journal's path rule everywhere the docs claimed the opposite of
  the code. `Mutation.Path`, `Journal.Preview`, the TUI's `/undo` listing and package header, and
  ADR 0051 now state that a record's identity is the path the argument NAMED (root-joined, cleaned,
  nothing followed) for an ordinary write, and only an approved escape records the permit's resolved
  target (ADR 0049). ADR 0051 gains a dated Amendment (2026-08-19) recording the correction and the
  lexical-fence rationale; the rationale itself stays at `journalTarget`'s doc comment. No behavior
  change.

- Tests: `move_file`'s copy-then-remove fallback is now pinned on both routes — an approved
  out-of-workspace escape (ADR 0049) journals the same two records the rename path leaves, and a
  split failure (copy landed, removal refused) journals the destination alone.

- Docs: moved the 2026-08-11 audit-triage note and the 2026-08-12 hostile-bytes closeout narration
  out of `ISSUES.md` into `docs/reviews/archived/2026-08-11 - 01 - external-audit-triage.md`
  (new "Addendum" section), leaving a five-line pointer in the *Deferred security-review Lows*
  entry and correcting the stale triage-doc path (missing `archived/` segment).

- Docs: moved the tool-surface poll record — the 2026-08-10 four-poll round and its 2026-08-16
  second round, with the bench arms, deferred candidates, engine-level notes, denials and method
  lessons — out of `ISSUES.md` into the new `docs/design/tool-surface-findings.md` (verbatim, ADR
  links repointed for the new location). The `ISSUES.md` entry is now a pointer in the
  mechanism-catalogue shape, carrying only the live gates: the six bench arms, arm (c)'s
  watch-item, the three grill topics, and the standing rule that nothing leaves the roster on poll
  evidence alone.

## [0.15.0] — 2026-08-16

### Added

- Tool-built host requests now carry the identity of the run that raised them: `domain.PresentRequest` gained `Depth` and `SpawnCallID`, `domain.AskRequest` gained `Depth`, filled at the dispatch seam from one new ctx carrier set (`WithSubAgentDepth` / `WithSpawnCallID`) installed for every tool call — depth 0 and an empty spawn id being the honest identity of the top-level agent. A Driver can now draw a sub-agent's presentation at its own depth and inside its own run instead of as the top-level agent's.

- **`/inspect` shows the raw wire traffic behind `ui.inspector`.** The TUI half of the Inspector: the
  `domain.WireEvent`s the engine reports while the key arms the capture now land in a bounded ring on
  the renderer — the twenty most recent halves of an Upstream round-trip, escape-stripped and
  pretty-printed once as they are folded — and `/inspect` opens a non-modal, scrollable pane over
  them, newest last, each record headed by its direction and the Turn (and the depth of a delegated
  run) that made the call. The ring sits BESIDE the transcript and never in it: a wire record is not
  a conversation entry, so nothing about the scrollback, the gauge or the status phrase moves when
  one arrives, and the records outlive a `/clear` the way the hidden debug view does. The verb is
  offered like every other, safe while the agent works, and withheld from prompt recall like
  `/settings` and `/usage`; the pane opens on the newest record, answers `esc` and the four scroll
  keys and claims nothing else, and takes its rows in the frame's one allocation between the
  dropdown and the `/usage` report. With nothing captured it draws one row rather than an empty box —
  naming `ui.inspector` where the capture is off, and saying it is armed and waiting where it is on.
  A record longer than a hundred lines keeps its head and closes with the same `… (+N more lines)`
  every other elided block carries, so nothing is ever cut silently. `layout.md` gains the pane, its
  give-way position and the popup's own section; the README gains the verb.

- **`ui.inspector` now arms wire capture, and the engine reports it as events.** A new editable
  bool key — `ui:` / `inspector:`, default false, file-only like every key in the block — travels the
  config pipeline to `Options.UI.Inspector`, onto `apogee.Config.Inspector`, and from there into the
  engine's construction: with it on, `internal/agent` installs the provider's wire observer and every
  model call reports its request body and its response payload through the session's existing
  `EventSink` as a new `domain.WireEvent` (`Direction` + `Payload`, stamped with the emitting agent's
  Turn, depth and spawning call id like every other Event), re-exported on the public surface as
  `apogee.WireEvent` with its two `apogee.WireDirection…` values. Both drivers plumb it, so a headless
  run observes the same stream an interactive session does — the event crosses the engine seam as data,
  never as a control surface (ADR 0031). Credentials cannot travel: the capture is bodies only, never
  headers. With the key off, no observer is installed at all — nothing is captured, accumulated or
  emitted, and the session is byte-identical to one built before the key existed. Arming is read once,
  at startup: a mid-session edit applies from the next run, which the key's own description and its
  template documentation both say. `SwitchUpstream` re-arms the client it rebuilds, so a `/server`
  switch does not silently blind a capturing session, and a routed sub-agent spawn arms its own client
  at the child's depth. This is the engine half of the Inspector; `/inspect` is still to come.

- **The provider client can now report its own wire traffic.** `internal/provider` gained an
  opt-in observer — `provider.WithWireObserver(func(WireRecord))` — that is handed the bytes the
  client already builds and parses: one `WireRequest` record per `Respond`/`Stream` call carrying
  the marshalled body exactly as posted (once per call, not once per retry attempt), and one
  `WireResponse` record carrying the raw SSE `data:` payloads newline-joined in arrival order,
  delivered once when the stream ends however it ends — or, on a non-2xx reply, the error body
  after the existing redaction and length cap. Credentials cannot reach an observer by
  construction: a record is a body, never a header. Nothing is retained in the provider, and with
  no observer installed — the default — not a byte is accumulated and the streaming path is
  unchanged. This is the capture half of the Inspector; nothing yet arms it.

- **`url-safety:` hosts now reach the network tools.** The `allow-hosts` / `deny-hosts` lists
  resolved from `config.yaml` are threaded onto `apogee.Config` and built into the url-safety guard
  that `web_fetch`, `http_request` and `web_search` filter every URL through, on both composition
  paths (the engine's own assembly and the CLI's MCP-aware one). Entries are normalised to the
  dialled host form at guard construction — `Example.COM.` blocks `example.com` — through the new
  `security.NewURLGuard` / `security.NormalizeHostPattern`. The lists can only ever tighten: the
  always-on, resolved-IP SSRF floor stays unreachable from configuration, and MCP endpoints are
  deliberately not covered. The key is read at startup; editing it applies from the next run.

- **A file-only `url-safety:` block now carries host allow/deny lists through the config pipeline.**
  The network tools' url-safety guard has always had `AllowHosts`/`DenyHosts` fields and no way for a
  user to reach them: the only host policy a config could express was the always-on SSRF floor. The
  schema now has a `url-safety:` block with `allow-hosts:` and `deny-hosts:`, resolved through every
  stop of the six-stop pipeline onto `Options.URLAllowHosts` / `Options.URLDenyHosts`, and described
  by two `KindStringList` registry rows (`url-safety.allow-hosts`, `url-safety.deny-hosts`) so
  `/settings` shows and writes them like the `tools.disabled` roster beside them. Both keys are
  file-only in this codebase's sense — no flag, no env — because which hosts a machine may reach is a
  per-machine fact, not an invocation one, and the block is HOSTS only: the scheme allow-set stays
  code-level, since widening it is exactly the loosening this layer must not be able to do. The
  layer is tighten-only by construction — the floor lives behind an unexported field no config value
  can reach — and the entries are carried verbatim here, to be normalised where the guard is built.
  The seeded template documents the block commented out, and the lists reach the running guard in the
  next change.

- **A `response-reserve:` edited on the bound `servers:` entry now rides the rebind, in force the
  moment it commits.** The re-read already installed the entry's share on the live latch, but the
  ride a `servers:` commit drives asked only whether the window or the reply ceiling had moved — so
  a share edited on the server this session is on described the session only from the next bind,
  `/server` move or scheduled Firing onwards, seconds or minutes away, while the two bounds edited
  in the same block of the same file were live at once. `liveSettings.setServers` now reports a
  moved share as the third arm of that condition, and the composition root's `rebindSpecFor` states
  the resolved share on every spec it builds (`RebindSpec.ResponseReserveFraction`, the field the
  engine half added) — so dropping the override is the same act as stating one: the stated `0` hands
  the split back to apogee's own default share without waiting for a bind. The TOP-LEVEL
  `response-reserve:` key stays file-only for a running session by design, and a `/set` of it is
  still refused by name.

- **A `RebindSpec` can now carry the response-reserve share, so a live edit of it reaches the
  engine.** The reply ceiling already rode the rebind's atomic commit because `max-output-tokens:`
  has no engine setter of its own; `response-reserve:` has exactly the same gap and had no field to
  ride. `RebindSpec` now carries `ResponseReserveFraction *float64` on the ceiling's contract — nil
  ⇒ the spec says nothing about the split and whatever share is in force stands (so a caller that
  re-resolved only the per-model bindings can never re-divide a window an entry pinned), a stated
  value replaces it, and a stated `0` is the operator dropping the pin, which hands the split back
  to `internal/context.Allocate`'s own built-in default rather than to the departed entry's number.
  `Rebind` writes it onto the `next` copy with the rest of the bindings, so a spec that fails a
  validation gate moves it no more than it moves the others.

- **A `response-reserve:` config key sets how much of the context window is held back for the
  model's reply.** apogee splits every request's window between the prompt and the room the model
  answers into, and that split was a hardcoded fifth. It is now a config key taking a fraction —
  `response-reserve: 0.35` — top-level for the run and, like `context-window:` beside it, on a
  `servers:` entry for one server, where the entry's own share outranks the top-level key
  (`config.ResolveResponseReserve`). The engine's `Allocate` follows one precedence — an explicit
  reserve in TOKENS wins, else a configured fraction in (0, 1) of the window, else the built-in
  0.20 — and both scopes refuse anything that is not a share (negative, 1 or more, NaN) at load,
  where the file and the number the user wrote can still be named. The per-entry share rides every
  arrival on a server the two token bounds ride: the startup bind, a `/server` move (a new
  `UpstreamSpec.ResponseReserveFraction`), a scheduled Firing, a headless run, and a delegation onto
  the Sub-agent server (a new `DelegationTarget.ResponseReserveFraction`, where an entry stating no
  share leaves the child on the parent's resolved one). Both keys are documented in the seeded
  config template.

- **`ThinkingEffort` and its four levels are re-exported at the root.** `apogee.go` already aliased
  `ThinkingProfile` / `ThinkingStyle` with their constants but not the effort type, so an
  out-of-module Driver had to hand `Agent.SetEffortOverride` untyped strings — an ADR 0031
  Driver-sufficiency gap. The facade now carries `apogee.ThinkingEffort` alongside the thinking
  aliases, plus `EffortOff` / `EffortLow` / `EffortMedium` / `EffortHigh`, and `example_test.go`'s
  completeness guard pins all five so a dropped re-export fails the build.

- **A new `ui.stall-after` config key carries the stall guard's quiet threshold.** It is a duration
  written the way Go spells one (`90s`, `2m`, `1m30s`), default `90s`, with `0` turning the guard
  off — long enough that ingesting a large prompt, legitimately silent for a minute or two on a
  local model, never trips it. The on-disk key is a pointer (`uiConfig.StallAfter *string`) so an
  explicit `0` is distinguishable from an absent key, it resolves to a `time.Duration` on
  `UISettings`, and a negative or unparseable value is a loud startup error naming the key and
  quoting what was written. It is a registry row like every other key — editable in `/settings`,
  documented in the seeded template, and applied live in the running session
  (`settingsApplyLocal`) — and it is threaded to the renderer as `tui.Options.StallAfter`, whose
  zero value is the guard off. Nothing reads it on screen yet; the status-line suffix it drives
  lands with the stall guard itself.

- **A `warning` colour role, and the status bar's second voice.** The scheme vocabulary gains a 29th
  role, `warning` — the rung between `muted` and `error`: a condition apogee wants noticed that it
  has not called a fault. Both shipped schemes state it as an amber pitched between their own `muted`
  and `error` (`#d7af5f` dark, `#9a6700` light), and a user scheme that omits it inherits the dark
  value like every other key. The theme threads it to a new `statusWarning` style — the warning tone
  on the status line's black field and deliberately NOT bold, so a qualifier tints a running phrase
  instead of announcing a failure the way `statusError` does. Nothing paints with it yet; it is the
  tint the stall guard's quiet qualifier will wear.

- **The status line stops claiming "thinking" when nothing is coming.** A running phrase now gains a
  warning-tinted `quiet` qualifier in front of its clock — `thinking · quiet · 21m 03s` — once the
  engine has said nothing for longer than `ui.stall-after`. The row keeps ONE clock and it is the
  activity's: in the shape this was built for nothing arrives after the request goes out, so the
  silence and the phrase are the same span, and a second duration behind the word would state that
  one fact twice. The clock the guard reads (`Model.lastEvent`) is stamped by every engine Event, at
  any depth and of any variant, `ReasoningEvent` included (a reasoning stream is life; the incident
  that motivated this had *no* events at all), and by the launch of a worker whose request is away,
  so a fresh exchange never inherits the silence of the one before it. Nothing on a timer touches
  it: a heartbeat or a spinner frame proves the TUI is alive, which was never the question. It
  reports a fact and never a verdict — that nothing has arrived, no "stalled?" wording — and it
  disappears by itself the moment an Event lands. Only `thinking` and `responding` carry it: a
  silent tool call is the tool taking its time, a stopping worker already says what it is doing, and
  the states waiting on the human (an open question, an approval) never show it, the silence there
  being the human's own. It costs no new tick — the spinner already repaints the row every frame —
  and it is the first thing the left slot gives up on a narrow window, dropped whole rather than
  truncated.

- **The TUI now retains the model's reasoning, and renders none of it.** A `reasoningTail`
  (`internal/tui/reasoning.go`) keeps the current Turn's reasoning chunks as the seam a future
  reasoning display will read: nothing in the view touches the buffer, `/settings` gains no key for
  it, and the status line's "thinking" still comes from the *arrival* of a `ReasoningEvent` rather
  than from these bytes. Landing the retention rules ahead of any display is the point of it —
  they are the rules such a display would live or die by, and they now sit under tests instead of
  inside a renderer. Text is escape-stripped at this one seam (a `ReasoningEvent`'s `Text` is raw
  model output that may carry ESC bytes, so an OSC 8 opener is dead before it could reach a cell
  buffer); the buffer is bounded to the last 4096 bytes, dropped from the front on a rune boundary,
  because the Model is copied on every Update and a Turn may reason for an hour; and it holds one
  agent's reasoning at a time, keyed on the same depth-and-spawn identity the activity line uses,
  since a fan-out's delegates interleave their chunks in one stream. `Model.foldReasoning` is the
  fourth fold `foldEvent` runs and the only writer — a `StreamResetEvent` (the Turn is superseded)
  and a `MessageEvent` (the Turn is committed, and its `reasoning_content` is the canonical copy)
  each end a tail, as do the two worker boundaries no Event announces, a launched Exchange and an
  unwound worker.

- The alias synthesized for a raw `--endpoint`/`APOGEE_ENDPOINT` override no longer collides with a
  configured `servers:` entry name: when the endpoint's host equals one, the label takes a
  `" (endpoint)"` suffix (e.g. `workstation (endpoint)`), so the switch list never draws two rows
  `· current` and name-keyed lookups resolve the row the user picked. Only the synthesized fallback
  is affected — an explicit `--server-name`/configured alias is untouched.

- The `mechanisms` settings row now tells a refused write apart from one that saved but could not be
  put in force: `tui.Options.WriteMechanism` answers `(saved bool, err error)`, and a flip whose
  splice landed under a failed live apply reads "saved — live apply failed: …" on the row — the same
  sentence every other persisted-but-unapplied key gets — while a refused splice keeps the plain
  refusal. The binary's write chain is pinned directly for the first time
  (`cmd/apogee/wire_options_test.go`).

- **The POSIX setsid-escape teardown residual is now pinned by a test.** The §2.4 process-group
  teardown's documented limit — a descendant that calls `setsid`/`setpgid(0,0)` leads a new group,
  so no negative-PID kill aimed at the run's group reaches it — was prose in three places
  (`setProcessGroupTeardown`, `planTreeKill`, `doc.go`) and asserted nowhere.
  `internal/tools/exec_teardown_unix_test.go` (build-tagged `!windows`, skipped when `setsid` is not
  on `PATH`) drives a real escapee through BOTH teardown paths: the clean-exit reap and
  `cmd.Cancel`. The leader and the escapee rendezvous over a FIFO, so the escape has provably
  already happened before teardown fires and the test cannot race what it observes; each half then
  checks the escapee leads its own process group — without which a `setsid` that silently did
  nothing would leave the survival check proving nothing — and that it is still alive afterwards,
  killing it in cleanup so the suite leaks no straggler. Behavior confirmed as documented: the
  escapee survives both paths while the tool reports the leader's own exit status.

- **The exec tools' credential scrub takes host-named variables.** `terminal`, `python_exec` and
  `run_tests` hand a subprocess the operator's inherited environment minus apogee's own
  `APOGEE_API_KEY`; that fixed list was written when a configured server key could only live in a
  file, and `api-key-env:` (ADR 0047) reopened the surface — an exported provider key was inherited
  by every child whose contents the MODEL chose. The names to drop now arrive from the host:
  `domain.Config.SecretEnvVars` → `tools.HostTools.SecretEnvVars` → each of the three constructors,
  compared case-insensitively like apogee's own names, blank entries ignored, and applied to
  python's interpreter-version probe as well as to the snippet. Empty/nil ⇒ the scrub is
  byte-identical to what it was. Sourcing the configured `api-key-env:` names into that field
  follows in the next change.

- **The configured `api-key-env:` variables now reach that scrub.** `config.APIKeyEnvNames` returns
  the deduplicated union of every `servers:` entry's `api-key-env:` plus the startup entry's own,
  trimmed the way the resolver trims a name before the lookup, and both Drivers fold it onto
  `domain.Config.SecretEnvVars` — the session (`wire_boot.go`) and `apogee headless` — so a key the
  operator exported is dropped from every `terminal` / `python_exec` / `run_tests` subprocess on
  either path. The union spans ALL configured entries rather than the bound one: `/server` switches
  mid-session, and a scrub that followed the binding would leave the other entries' keys readable in
  every child until the switch happened. The MCP registry hand-assembly (`registryWithMCP`) carries
  the same field, so connecting an MCP server cannot re-open what a session without one closes.

- **The git tools refuse a repository that configures its own filter driver.** A checkout
  delivered with its own `.git/config` naming `filter.<driver>.clean`, `.smudge` or `.process`
  hands git a command to execute as the operator on ordinary operations — `git add` runs clean,
  `git checkout` runs smudge, and a plain `git diff`/`git status` runs clean too — and git offers
  no switch that refuses configured filters, so the emptied `core.hooksPath` and the
  `--no-textconv --no-ext-diff` read-path refusals left that half open. `runGit` now asks git
  itself for the repo-local filter config (`git config --local|--worktree --name-only
  --get-regexp`, a listing that executes no driver and, with `--name-only`, never echoes an
  attacker-chosen value) before every invocation, and refuses the call when anything matches: the
  model gets a refusal naming the offending key(s) instead of a git run. Because the probe sits at
  the one choke point every git tool already goes through, the read tools are covered as
  completely as the write ones, and a future git tool inherits it by construction. Only the
  REPOSITORY's scopes refuse — a filter driver in the operator's own global/system config is
  theirs and still applies, the same trust boundary `HOME` sits on in the git env allowlist.

- Approval prompts now disclose the MCP server-grain session grant: an MCP tool's pane states that
  "Always allow" covers every tool of that server for the session, so approving one tool no longer
  silently authorises its siblings unannounced. Native tools, MCP tools whose server cannot be
  named, and forced dangerous-action gates carry no note — their allow authorises only the call on
  the screen. `domain.ApprovalRequest` gained `MCPServerGrant` / `MCPServerAlias` for Drivers.

- **`/settings` switches individual Mechanisms in a sub-list of its own.** `⏎` on the `mechanisms`
  row no longer opens `$EDITOR` — it opens the catalogue, every id this build carries in canonical
  order with `on`/`off` beside it, read from the config FILE's own block on every frame (an id the
  block does not name is off, and an edit made in another window shows up in an open list). `⏎` and
  `space` each flip the highlighted one: the line is spliced, the baseline re-taken and the block
  re-read into the running session on that same keypress (ADR 0035, ADR 0037 decision 1), and the
  list STAYS OPEN with the new state showing, because setting a posture is usually several switches.
  `esc` returns to the key list; a refusal lands on the `mechanisms` row and the block is untouched.
  Switching one off writes `<id>: false` rather than removing the line, so the file records the
  decision — with the pre-existing ADR 0016 consequence that a non-empty `mechanisms:` block means
  manual control and the Validated set measured for the bound model is no longer applied on top. The
  row's pointer now reads `· ⏎ opens toggle list`, and the renderer gained two seams for it
  (`Options.ListMechanisms` / `Options.WriteMechanism`, both nil-degrading to "the row opens
  nothing" / "the pane says so"). Raw block edits — comments, ordering, deletion — are still made in
  `config.yaml` by hand.

- Added `domain.WriteEscapePermit` with `WithWriteEscapePermit` / `WriteEscapePermitFrom` — the
  context carrier that will authorise one approved out-of-workspace write target for the duration
  of one tool execution (ADR 0049). No consumer yet; absence keeps the workspace fence as the sole
  rule.

- The workspace write fence now honours one **approved escape target**: `security.SafeWriteFile`,
  `SafeRemove`, `SafeCopyFile` and `SafeCopyFileFrom` take the resolved path an approval disclosed
  (empty = no permit, which is every call today). With no permit, or for a target inside the
  workspace root, the fence is byte-for-byte what it was; with one, an argument that re-resolves to
  exactly that path is written through an `os.Root` pinned at the target's deepest existing
  ancestor — missing parents created inside it, the final name acted on rather than followed — and
  any divergence (a re-resolution mismatch, a symlinked target, a non-directory in the chain) is
  refused with nothing touched. The read primitives take no permit and none can be given to them
  (ADR 0049).

- Dispatch now **mints** the write-escape permit ADR 0049 defined: a workspace-scoped write whose
  target resolves outside the workspace root carries that resolved path on its Resolution, and the
  one execution tail every unconfined call passes through installs it as a
  `domain.WriteEscapePermit` for exactly that tool execution. Three cells authorise one — an
  approved Gate (the ladder's out-of-workspace row, a dangerous-action forced look, and a
  remembered allow-for-session alike, since approval is final), the Auto · `confine-to-workspace:
  false` run, and a target inside the confinement box's declared writable paths. Classification
  learned the union at the same time: `WorkspaceRoot ∪ box.WritablePaths` is in-fence, so a session
  that declared a writable path outside the workspace is no longer gated on the very path it
  declared writable. A refused or denied call never reaches a permit, and a call that needs none
  runs on a context byte-for-byte identical to today's.

- An approved out-of-workspace write now executes across the whole WS-write family: `write_file`,
  `edit_existing_file` (patch and full), `single_find_and_replace`, `multi_find_and_replace`, and
  `file_ops`' copy/move destination and delete all carry the execution context's write-escape
  permit into the shared TOCTOU-safe core, so the Gate's "Allow" lands on exactly the resolved
  path the approval pane disclosed. A call with no permit keeps today's workspace fence
  byte-for-byte, `move_file`'s undisclosed source keeps its unconditional in-workspace refusal,
  and no read tool ever takes a permit (ADR 0049).

- The shared popup painter can now paint a **scroll bar** down a row window it cannot seat whole:
  `popupSpec.scrollbar` opts a pane in, and while — and only while — the seated window is shorter
  than the list, the row block gives up its last column and paints the transcript's own glyphs
  (`glyphScrollThumb` / `glyphScrollTrack`, `th.scrollThumb` / `th.scrollTrack`) down it. The rows
  are COMPOSED one column narrower rather than painted under the bar, so nothing is hidden by it,
  and a window that seats the whole list keeps the full inner width and renders byte-for-byte as it
  did before. The thumb is sized and placed from the ROW counts (the seated window over the whole
  list) but drawn in the block's painted LINES, so a pane whose rows wrap gets one unbroken stroke
  spanning every line of every seated row. Which panes opt in is the entry below.

- **Every overflowing popup now paints the scroll bar.** The `/settings` key list, its text field
  and its enum sub-list, `/usage`, the `/sessions` browser, the `/model` · `/server` pickers, the
  `/` dropdown and the approval and ask prompts all stamp the human's `ui.show-scrollbar` into
  their popup spec (`Model.popupScrollbarOn`, the one place `Options.HideScrollbar` is read for a
  pane), so a pane whose list is longer than the window it was granted shows the same two weights
  down the column inside its border that the transcript has always used. The column is reserved
  only while the list overflows — a pane whose rows all fit keeps its full inner width and renders
  exactly as it did before — and switching `ui.show-scrollbar` off takes every popup's bar away
  with the transcript's, which is what the key's `/settings` description now says.

- **`/usage` now scrolls from the keyboard.** While the report is up, `↑`/`↓` move its window one
  row and `PgUp`/`PgDn` a whole window, clamped at the first row and at the last FULL window exactly
  as the wheel is — both read the window the frame DREW (`usageWindow`), so a key and a notch can
  never disagree about which rows are on the screen. The page keys are the report's for as long as
  it is open, claimed ahead of the transcript's own `PgUp`/`PgDn` interception, so a page key never
  scrolls the conversation hidden behind the pane. The pane stays non-modal — `esc` still closes it
  and every other key, a printable one included, still reaches the input box — and its hint now
  reads `↑/↓ scroll · esc close`.

- The `validated-sets:` block is now TWO `/settings` rows instead of one structured summary.
  `validated-sets.enable` is an editable bool row — the surface's off-switch is flipped on the row
  and applied to the running session (ADR 0037) rather than sending you to `$EDITOR` to change one
  true/false — and `validated-sets.alias` keeps the `· ⏎ opens $EDITOR` pointer with an alias count
  for its value, since a map of runtime labels to entry keys is a shape no row holds. Both rows
  apply through the same re-read of the whole block: the off-switch and the alias map are one input
  to the per-model resolution (ADR 0016), so an alias edited in your own editor still reaches the
  session on the same door the row uses.

- `config.SaveMechanismSetting` writes one catalogued Mechanism's line into the top-level
  `mechanisms:` block — the first config writer addressed by CATALOGUE ID rather than by registry
  path, since that block's children are the Mechanism catalogue's ids and not the schema's, so
  which ids exist stays the caller's question. It meets the four shapes the block can be in (no
  block at all, a bare `mechanisms:`, one already open, a line already there), creating an absent
  block directly under the commented example the seeded template documents it with (ADR 0035), and
  turning a Mechanism off writes `<id>: false` rather than removing the line: an explicit "off" is
  a decision where an absent key is only the default, and a non-empty block is manual control (ADR
  0016). Every comment, key order and neighbouring setting comes back byte-identical, an id the
  file could not carry as a plain key is refused before the config is opened, and a splice that
  moved anything but this one id is refused with the writer's "edit the file by hand" idiom. No
  surface calls it yet.

- **The dangerous-action guard recognises a Windows home.** `normalize` now folds `\` to `/`
  alongside its existing lower-casing and whitespace collapse, so `C:\Users\alice\.ssh` arrives as
  `c:/users/alice/.ssh` and matches the `/users/<name>` anchor the home-anchored rules already
  carried — every path rule gains separator robustness from one place instead of each pattern
  spelling both separators. `%userprofile%`, the one home form the fold cannot produce, is spelled
  out in the anchors. The result: writes under `C:\Users\<name>\.ssh`, `%USERPROFILE%\.npmrc` and
  `C:\Users\<name>\.apogee`, and `rm -rf C:\Users\<name>` / `rm -rf %USERPROFILE%`, now hard-refuse
  the same way their POSIX spellings do. The two recursive-delete rules also accept a Windows drive
  root (`c:/…`) as an absolute target, keeping their precision boundary exactly where it was —
  relative-vs-absolute, so `rm -rf build\out` is still an ordinary step. The guard stays a
  footgun-guard rather than a boundary (ADR 0012): the unconditional fold makes a non-path
  backslash escape read like a path segment, which is accepted imprecision, not obfuscation
  resistance.

- **A model profile's thinking block gains a validated `effort:` leaf.** The per-model dial for how
  hard a model is asked to think (`off | low | medium | high`) is now part of the domain profile:
  `domain.ThinkingEffort` with the four constants, a `Valid()` gate, and an `Effort` field on
  `domain.ThinkingProfile`. It is ORTHOGONAL to `style:` beside it — style says how the reasoning
  arrives in the reply, effort says how much of it to produce — and its ZERO value is the wire
  anchor: absent means nothing is emitted for it, so a config written before the key existed still
  produces byte-identical requests and the model's own template default stands (ADR 0050). The
  on-disk `model-profiles: <pattern>: thinking: effort:` key maps straight across at
  `toModelProfile`, and a value outside the four levels is a LOAD error naming the offending
  pattern and spelling the vocabulary out — the failure is otherwise invisible, since a model sent
  no effort and a model sent an unmapped one answer alike. The seeded template documents the axis
  and shows it in the `model-profiles:` example. Nothing sends it on the wire yet; the request seam
  and the `/effort` session override land with the items that follow.

- **The request seam carries a semantic thinking effort instead of an on/off thinking switch.**
  `provider.Request.DisableThinking` is gone; in its place `ThinkingEffort provider.Effort` says how
  hard a call asks the Upstream to think, with a provider-local vocabulary (`off`, `low`, `medium`,
  `high`) that mirrors the domain's without importing it — the provider package stays domain-free
  and the agent maps at the boundary, the way it already maps sampling. The provider Client owns the
  ONE wire mapping (ADR 0050): `off` emits `chat_template_kwargs: {"enable_thinking": false}` — byte
  for byte what the deleted switch emitted — and a level emits `{"reasoning_effort": "<level>"}`
  verbatim, with no per-family translation table until a second family is live-verified. The zero
  value stays the wire anchor: a request that asks for nothing carries no `chat_template_kwargs` key
  at all, and an unrecognised non-empty value emits nothing rather than putting a word the template
  cannot read on the wire — the config loader's enum is the place typos are caught, and the Client
  stays total. The naming call (`internal/title`) now asks for `off` and puts the same bytes on the
  wire it always did, including its one re-send without the kwarg for a server that rejects the
  unknown field outright.

- **Every request now carries the session's resolved thinking effort, and a Driver can override it.**
  The wire projection resolves the effort in the one order ADR 0050 fixes — session override ▸ the
  bound model profile's `thinking.effort:` ▸ nothing — and maps the domain value onto the provider
  vocabulary at the same boundary that already maps sampling, so the provider package stays
  domain-free and a value outside the four levels emits nothing rather than a word the template
  cannot read. The foot of that ladder is the wire anchor: a session with no override and a profile
  with no effort sends no `chat_template_kwargs` at all, byte-identical to the pre-effort loop. The
  override arrives through a new engine door, `Agent.SetEffortOverride` — the zero value clears it,
  nothing is persisted, and `Agent.ThinkingEffort()` reports the override and the profile separately
  so a Driver can show the layering rather than just the winner. It is configuration, not a
  Mechanism, so it holds under Bypass; it is read once per request under its own lock, so setting it
  mid-run lands on the next request; it survives a model switch while the profile half re-resolves
  from the new model; and it is PRIMARY-loop state — a delegated child is built from the parent's
  Config, which carries no override, so a sub-agent still thinks at its own profile's effort.

- **A failed request that carried chat-template kwargs now says so.** When the upstream answers a
  non-2xx status to a request that put `chat_template_kwargs` on the wire, the surfaced error gains
  one parenthetical: `(this request carried chat_template_kwargs — an unsupported thinking effort
  for this model's template? check model-profiles thinking.effort or the /effort override)`. That is
  the shape this failure actually takes — a chat template that rejects an effort value raises inside
  Jinja and the server answers HTTP 500 with a template traceback that never names the field it
  choked on — so the hint names both doors the value can have come through instead of leaving a
  mid-turn 500 unexplained. It rides both surfaces: the unary error, where it is appended by
  wrapping so `errors.As(*StatusError)` and the status code still reach callers and the server's own
  body stays unedited, and the streaming error delta, which is where a turn actually fails. A
  request that carried no kwargs — every out-of-the-box one — gets exactly the error it got before,
  and a classified context overflow is never hinted.

- `/effort` sets this session's thinking effort — `off`, `low`, `medium`, `high` to layer a level
  above the bound model profile's `thinking.effort:`, `auto` to drop the override, and a bare
  `/effort` to state the resolution (`thinking effort: <effective> (session override: …;
  profile: …)`). It runs while a turn is in flight, since the level reaches the model on the next
  request, and is never persisted.

- Documented the model-profile `thinking.effort:` key and the `/effort` session override in `README.md` — the slash-command table gained an `/effort` row, and the Configuration section gained a paragraph covering the four levels, the absent-means-nothing default, the Qwen3.8 `xhigh` motivation, and the override's session-only, primary-loop-only scope.

### Changed

- **A parked sibling's stream reset is now pinned to drop only its own text.** `discardPending`
  unparks the resetting run BEFORE the early return that protects another run's live buffer, so a
  `StreamResetEvent` from a delegate that lost the shared slot mid-alternation (ADR 0039) discards
  the text it had parked while the sibling holding the slot streams on untouched. The behaviour is
  owned by the function's doc comment but no test staged parked text for the resetting run —
  `TestStreamResetOnlyDiscardsItsOwnDepth` only proves the buffer of another run survives — so a
  refactor moving the unpark below the early return would have changed the semantic silently.
  `TestStreamResetDropsOnlyTheParkedSiblingsOwnText` now streams sibling A into the slot, parks B's
  words behind it, resets B, and continues A: A's answer commits whole, `parked` comes back empty,
  and neither exit that commits a displaced delegate's residue — A's own `MessageEvent` fallback or
  B's run-ending report — surfaces the superseded tokens. Both assertions were checked against
  mutated production lines (unpark moved after the early return; the early return removed), so
  neither passes vacuously.

- **The `CircuitBreaker`'s concurrency guarantee and its post-trip recovery are now pinned by
  tests.** The type's doc comment claims it is "safe for concurrent use", but no test had ever
  spawned a goroutine against it: `TestCircuitBreaker_ConcurrentUse` runs eight goroutines
  interleaving `Record` and `Tripped` over one signature they all share (mixed success and
  failure, final state deliberately non-deterministic) and one signature each of them alone
  drives to the trip edge — under `-race` the run itself proves the guarantee, and the private
  signatures carry the deterministic assertion that concurrent traffic never leaks across
  signatures. The recovery branch — the `delete(b.tripped, sig)` a succeeding call performs —
  had likewise never been reached, because the existing streak-reset test records only two
  failures against a threshold of three: `TestCircuitBreaker_SuccessClearsATrippedSignature`
  now trips a signature with three identical failures, records a success, and asserts the
  signature is no longer tripped and that its streak restarted (the trip edge returns only on a
  fresh run of three, never on the first failure after the success) — so a regression leaving
  `tripped` stale, which would block the call forever, fails the suite. Each assertion was
  checked against a mutated production line, so none of them passes vacuously.

- **The dangerous-action guard's dead `Tier.String()` is gone.** The method was exported, rendered
  the two tiers as `"force-approval"` / `"hard-refuse"` for audit and log lines, and had no caller
  anywhere in the repo — the audit trail states its own decision strings — so it sat at 0% coverage
  on the guard path, the one path where uncovered code is least welcome. The only thing that ever
  reached it was implicit `Stringer` dispatch from `%v` verbs inside test FAILURE messages; those
  verbs now read `%d`, so a failing assertion prints the numeric tier beside the `TierHardRefuse` /
  `TierForceApproval` constant its message already names. No production code ever formatted a
  `Tier`, so no user-visible or audit output changed, and re-adding the method is a five-line change
  if a caller ever appears.

- **Every `response-reserve:` arrival site now has the reserve-specific test its
  `context-window:` / `max-output-tokens:` analogue already had.** The share reached four sites on
  the strength of the neighbouring bounds' coverage alone: a scheduled Firing
  (`TestScheduleFiringSplitsTheWindowTheEntryTheSessionMovedOntoStates` pins that it divides the
  window of the entry the session MOVED ONTO, never the launch server's, and that an entry stating
  nothing resolves to the honest `0` rather than to the launch number), the Delegation target
  (`TestResolveDelegationTargetCarriesTheEntrysResponseReserve` pins the deliberately rank-free
  copy — the entry's share as written, `0` when it states none), the headless Driver
  (`TestHeadlessBudgetsAgainstTheBoundEntrysPins` gained a `wantReserve` column asserting the
  entry's override over the top-level key), and the routed sub-agent spawn
  (`TestRoutedSpawnResponseReserveShare` pins both arms of the `> 0` guard: a stated share splits
  the target's own window, a target stating none leaves the parent's share standing rather than
  falling to the built-in fifth). Each new assertion was checked against a mutated production line,
  so none of them passes vacuously.

- **Settings kind projection pinned for every kind.** `TestSettingsRowsProjectRegistryMetadata`
  now states all ten `config.Kind` edges as literals — `KindFloat → SettingInt`,
  `KindText → SettingText` and `KindScheme → SettingEnum` joined the seven already there — and
  guards them against the registry, so a new kind cannot silently join the uncovered set. A direct
  row-level assertion pins `response-reserve` (the one float row) to `tui.SettingInt`, which the
  row-level loop cannot do: its kind clause computes the expectation with `settingKind` itself.

- `internal/tui/toolshape_test.go` and `internal/tui/blocktarget_test.go` now open with a header
  comment recording their subject names as a ratified exception to the `{source}_test.go` rule,
  naming the sources each suite spans and why no single one can lend it its name.

- Docs: the command-menu prose in `README.md` now names every busy-safe command. It listed four —
  `/version`, `/skills`, `/usage` and `/confine`'s status report — while the table below it marks
  `/effort`, `/schedule` and `/schedule-stop` ✅ too, so the sentence understated what answers while
  the model works. It now enumerates all seven. `/<skill-id>` and `@<path>` stay out of that
  sentence deliberately: they ride the queued message rather than answering immediately, and the
  table already says so. The `ISSUES.md` bullet is removed. Prose only — no behaviour change.

- Docs: the fold-coverage prose no longer counts Events. `internal/tui/doc.go` said "so a twelfth
  Event has to be answered for" and `foldCases`'s comment said the table asserts "there is no
  twelfth" — both stale (`internal/domain/events.go` declares 12 variants and the table carries 13
  rows, `TokenEvent` appearing at depth 0 and 1). They now read "a new Event variant has to be
  answered for — including with 'deliberately nothing'" and "the assertion that there is no
  unanswered variant": no numeral survives in either sentence, so neither can go stale on the next
  Event addition. `TestFoldEventCoversEveryEventVariant` is what actually enforces the coverage and
  is untouched. The `ISSUES.md` bullet is removed. Comments only — no behaviour change.

- Docs: `Scheme.Warning`'s doc comment now describes the consumer that shipped. It named "the status
  line's quiet-time suffix", a form that never landed — the stall guard renders as a `quiet`
  qualifier *before* the single activity clock — so the clause now reads "the status line's quiet
  qualifier — rendered before the activity clock". The rest of the comment (the `muted`/`error` rung
  framing and "named for the meaning rather than that qualifier") is unchanged. The `ISSUES.md`
  section holding the finding is removed, it being that section's only bullet. Comment only — no
  behaviour change.

- The scheme role count stated in prose is now pinned to the struct.
  `TestRoleTableCoversEveryRole` gained an assertion that `len(roleKeys)` — derived from `Scheme`'s
  `yaml:` tags by reflection — equals the 29 the prose states, with a failure message naming the
  three sites to update on drift: `README.md:187`, `layout.md:94` and `newTheme`'s comment
  (`internal/tui/theme.go:267`). The agreement was held by hand until now, and `newTheme`'s count
  sat silently stale at 26 across three role additions before the stall-guard run corrected it; the
  next role addition fails the suite instead. The `ISSUES.md` bullet asking for the pin is removed.
  Tests only — no behaviour change.

- Docs: ADR 0040 gains an `Amendment (2026-08-15)` section recording the four colour roles that
  landed after `tool-header`, whose trail stopped at the 25th key. One entry each, in landing order:
  `tool-leader` (the dotted run to the outcome slot, split off `muted`, 26th key, `ab53c09`, landed
  `#8a8a8a` and ships `#353535` per `ad2fdb1`), `tool-marker-bright` (`tool-marker`'s open step,
  27th key, `8d96941`, landed `#E6C099` and ships `#C0D0F0` per `85affd4`), `success` (`error`'s
  counterpart, first worn by the finished sub-agent `✓`, 28th key, `a0072a0`, `#56d364` / `#116329`)
  and `warning` (the rung between `muted` and `error`, first worn by the stall guard's `quiet`
  qualifier, 29th key, `4299b97`, `#d7af5f` / `#9a6700`). All four are additive keys, so the
  compatibility surface the record decides is untouched — a *rename* would still need its own
  amendment. The 2026-08-08 amendment's two "25th key" references now say what the vocabulary stands
  at today (29) and point forward to the new section. The `ISSUES.md` bullet asking for the record to
  catch up is removed. Docs only — no behaviour change.

- **The block-target, start-up-box and prompt-box-sizing suites move to
  `internal/tui/blocktarget_test.go`, `internal/tui/startupbox_test.go` and
  `internal/tui/chromelayout_test.go`, and `render_test.go` settles as the split's shared core.**
  The final carve lifts the click-surface suite — the `blockMark` type and the `blockMarks`,
  `headerStar` and `branchIndicator` helpers, whole-block marking, the header indicator's state and
  styling, the remainder count in the outcome slot, the one-click prompt block, the marks-versus-mouse
  agreement and the live star's blink table — into `blocktarget_test.go`; the wide and stacked
  start-up-box paints with their `lineWithLogoAnd` discriminator into `startupbox_test.go`; and the
  `inputContentRows` sizing suite — the wrap-boundary count, the zero-width edge, the
  `widgetContentRows` mirror on hand-written and generated drafts, and the prompt-editor row clamp —
  into `chromelayout_test.go`. What stays in `render_test.go` is exactly the shared core the other
  ten files reach across the package: the `readCall` folder and the golden-row builders, the firing
  block's three tests with their `firingBlock` fixture, the whole-scrollback
  `TestTranscriptLayoutGolden`, and the streaming-preview tail suite. Pure moves: no test renamed,
  reordered or edited, each new file carrying only the imports it needs and the residual's import
  block trimmed to what remains. `render_test.go` finishes at 459 lines, down from the 5520 it
  carried before the split began, with the ten carved files sitting beside the sources they
  exercise. The `ISSUES.md` bullet asking for the split is removed. Tests only — no behavior
  change.

- **The tool detail/diff, ask-user and tool-shape suites move to
  `internal/tui/toolbranch_test.go` and `internal/tui/toolshape_test.go`.** The fourth carve of the
  `render_test.go` split lifts the detail/diff suite (the in-flight member, the lone call sharing
  the group shape, the multi-detail and diff standalones, the layout sketch, the diffstat surviving
  the body cap, the collapsed truncation and expanded whole-body paints, the detail tone step, diff
  colour in both block states, the two-row collapsed cap and the clipped target that is no toggle
  target) plus the answered Ask User suite with its `askUserCall` fixture into
  `toolbranch_test.go`, and the tool-shape suite (one-line output on the branch and its grouping,
  the in-flight and targetless standalones, the targetless row budget, the cross-cutting
  every-shape collapse budget, unregistered-call argument labelling and the group breakers) into
  `toolshape_test.go`. The helpers these suites call — `readCall`, the golden-row builders,
  `blockMarks`, `firingBlock` — stay where they are, their cross-file callers reaching them in the
  same package. Pure moves: no test renamed, reordered or edited, each new file carrying only the
  imports it needs; `render_test.go` drops from 2715 to 1527 lines. Tests only — no behavior
  change.

- **The tool leader-row and tool-block grouping suites move to `internal/tui/toolleader_test.go`
  and `internal/tui/toolblock_test.go`.** The third carve of the `render_test.go` split lifts the
  header-label styling test and the leader-row arithmetic suite (the outcome slot reserved first,
  the promote guard's fifteen cells of target, the demoted line's spelling, the git-commit short
  hash at every width) into `toolleader_test.go`, and the same-label grouping suite plus the
  group-member suite with its `runGroup` fixture (body-carrying calls, the faint header count,
  clipped member targets, the expanded member's sketch shape and see-less footer, the member
  gutter that is not the sub-agent rail) into `toolblock_test.go`. The widely shared `readCall`
  fixture and the golden-row builders
  (`groupMemberLine`/`leaderEdgeRow`/`memberEdgeRow`/`seeLessFooterLine`) stay in `render_test.go`,
  where their cross-file callers already reach them. Pure moves: no test renamed, reordered or
  edited, each new file carrying only the imports it needs; `render_test.go` drops from 3532 to
  2715 lines. Tests only — no behavior change.

- **The sub-agent block suites move to `internal/tui/subagentblock_test.go`.** The second carve of
  the `render_test.go` split lifts the sub-agent framing reflow-safety test (P3.14), the sub-agent
  group sketch-state suite with its `targetedRender`/`rowWith` click-surface helpers, the
  delegation suite with `loneDelegation`/`delegateWithPrompt`/`delegateAsked` (the `┊` closer rule,
  the lone run's group frame, and the expanded delegation's details/prompt framing), and the
  spanless-delegation grouping test out of `render_test.go`. Pure moves: no test renamed, reordered
  or edited, the new file carrying only the imports it needs; `render_test.go` drops from 4466 to
  3532 lines. Tests only — no behavior change.

- **`internal/tui/render_test.go` begins splitting into per-subject test files.** The first carve
  lifts the wrap and rail suites into `internal/tui/wrap_test.go` (`railedWidth`'s floor, the
  absolute width cap, the painter's-measure break, the `clipWrap` row budget, and the sub-agent
  rail through the inter-block spacers) and the user-block suites into
  `internal/tui/userblock_test.go` (square user-block rows, the collapsed/expanded prompt paint,
  and the skill-token accents). Pure moves: no test renamed, reordered or edited, each new file
  carrying only the imports it needs; `render_test.go` drops from 5520 to 4466 lines. Tests only —
  no behavior change.

- **The `internal/tools` companion test suites now sit beside the sources they cover.** When
  `delete_file.go` and `path_read.go` were split out of `file_ops.go` and `path_safety.go`, their
  tests stayed behind in the old files. The `delete_file` suite (seven tests, from the positive
  control through the dangerous-action classification and the resolved-target disclosure) moves to
  `internal/tools/delete_file_test.go`, and the read-half suite (`readAllBounded`,
  `readWorkspaceFileBounded`'s oversize refusal and the seven `readScope` tests with their
  `scopeFixture`) moves to `internal/tools/path_read_test.go`. Pure moves: no test logic changed and
  the identical set of test functions runs. The package-wide fixtures every write- and read-side
  suite reaches for — `writeFixtureFile`, `realPath` and `tempRoot` with its package rule — stay put
  in `path_safety_test.go`, which is now their home; `writeFixture`, `runFileOp` and
  `extraRootFixture` likewise stay in `file_ops_test.go`.

- Docs: ADR 0047 §6 now carries a dated note (2026-08-14) saying that the `APOGEE_API_KEY` overlay
  is deliberately dropped when `/server` switches away from a configured start-up entry and back
  onto it. "Before resolution" is scoped to the start-up bind alone: the picker's rows are
  `opts.Servers` verbatim (`upstreamChoices`, `cmd/apogee/upstream.go`) and the move resolves the
  picked entry through the run's resolver (`sessionMover.move` → `m.keys.Resolve(entry)`), which
  sees the file's own source rather than the overlay. Owner-ratified as intended, not a defect; the
  synthesized ephemeral start-up row is the one row that does carry the overlaid key, because it
  exists nowhere in the file to be re-resolved from. No behavior change.

- Docs: `runSubprocess`'s teardown bullet (`internal/tools/exec_common.go`) no longer claims a
  cancelled or timed-out command "never orphans its children" — it now states what the teardown
  docs it summarises state: the group takes down every descendant that has not deliberately left
  it, and a descendant that calls `setsid`/`setpgid(0,0)` escapes the kill and survives
  unsupervised, still inside whatever fence the Confiner installed (an accepted residual, not an
  enforcement gap; Windows' Job Object denies breakaway and has no counterpart). The 2026-08-12
  security-audit closeout narration in `ISSUES.md` is likewise updated: the configured-filter
  residual now survives only for the operator's global config, since `runGit` refuses a call whose
  repo-local config defines a `filter.*.clean/smudge/process` driver.

- `ReadConfigForWrite`'s doc comment now lists its non-splicing caller (`externalEdit.spec`,
  `cmd/apogee/settingsedit.go`) alongside the four splicing writers, so the caller list is
  exhaustive again.

- Docs: `internal/security/doc.go`'s `openMutationRoot` summary now describes both routing cases
  since the lexical/resolved split (ADR 0049) — the permit question is asked first and on the
  resolved path (so a workspace-internal symlink pointing at the approved target lands on that
  target's own ancestor), and every other call keeps the unchanged lexical branch: the workspace
  root for a path spelled inside it, a refusal for one that is not.

- **The `show-scrollbar` prose covers the popup panes.** The default config described the bar as the
  transcript's alone, which stopped being true once every overflowing popup started painting one:
  the key's block comment and its commented example now say the bar is painted down the right-hand
  edge of the transcript AND of every popup pane — `/settings` and its sub-lists, `/usage`, the
  `/sessions` browser, the `/model` · `/server` pickers, the `/` dropdown, the approval and ask
  prompts — appearing only where there is more content than window, and that turning the key off
  takes the bar and its column away everywhere at once. The scroll-bar entry above no longer ends
  "No pane opts in yet.", a claim the entry directly below it contradicts. Prose only — no behavior
  change.

- Documentation: the process-group teardown claims now state the POSIX setsid-escape residual —
  the group holds every descendant that has not deliberately left it, and a descendant that calls
  `setsid`/`setpgid(0,0)` survives the call unsupervised while staying inside any confinement
  write-fence. Windows is unaffected (its Job Object denies breakaway). No behaviour change.

- **`internal/tools` splits its two over-length files by concern.** `path_safety.go` (407 lines)
  keeps the security aliases, the fenced primitives and the ADR-0049 approved-escape section; its
  READ half — `workspaceRelative`, `readWorkspaceFileBounded`, `readAllBounded`,
  `readFileErrorMessage`, `escapeOrMessage`, the `readScope` family, `matchRoot` and `rootUsable` —
  moves verbatim into a new `path_read.go`. `file_ops.go` (418) keeps `copy_file` and `move_file`
  with the schema builder, argument shape and pre-flight they share; the delete family
  (`deleteFileSpec`, `deleteFileArgs`, `DeleteFile`, `checkDeletePath`) moves into a new
  `delete_file.go`, matching the tool-per-file convention beside `write_file.go` and
  `file_edit.go`, and the trailing compile-time assertion block splits per type. Pure moves — no
  signature, wording or behaviour change — leaving all four files under the ~400-line smell
  threshold (ADR 0043). `doc.go`'s file map names both new files and their concerns.

- Tests: the write-family suites in `internal/tools` now take their workspace from `tempRoot`
  instead of raw `t.TempDir()` — `path_safety_test.go`, `write_permit_test.go`,
  `write_file_test.go`, `file_edit_test.go`, `find_replace_test.go`, `file_ops_test.go` and
  `read_file_test.go`, 73 roots in all. These are the suites whose paths can reach a tool's bare
  success sentence or the safety fence, and a root reached through a symlink (macOS `/tmp`) breaks
  both there: every path under it carries a resolution note, and every fence comparison is made
  against a name the fence never produces. `tempRoot`'s doc comment now states that as the package
  rule — the write family resolves; suites whose workspace is incidental (registry, terminal, git,
  python, grep, find_files, list_dir, diff, network, exec, present_document, workspace-scoped,
  sub-agent) stay raw, because their assertions never depend on the root's spelling.
  Behaviour-neutral: the suites assert exactly what they asserted before.

- Docs: the four positional cross-file references left stale by earlier file splits now name their
  file and function — `ReadConfigForWrite`'s "every splice below" names the setting writers that
  actually start from it (and the server-entry exception that does not), `ScalarTargetIn`'s
  "flow-style list refusal above" names `spliceHostAcknowledgement` in `configwrite.go`,
  `validateSettingValue`'s "seeding read below" names `ReadConfigForWrite` in `configsplice.go`, and
  the confinement contract's §4 "reads are not widened" sentence now also names the read an approved
  write does perform through the permitted parent (`readWriteTarget` / `statWriteTarget`).

- **`verifiedEntrySplice` no longer takes the config's original bytes.** The entry-splice gate's
  first parameter `data []byte` was vestigial: the body compares the re-parse of `updated` against
  the parsed `before` its caller already hands it, and never read those bytes. The parameter is gone
  from the signature, from its three call sites (`setEntryKeyCommand`, `setEntryPlaintextKeyOK`,
  `setEntrySetting`) and from the direct test call, leaving the gate the same shape as
  `verifiedMechanismSplice`. Its doc comment now says both sides of every comparison are PARSED
  states, so the original bytes stay the caller's business. No behaviour change.

- The confinement execution contract now describes the approved escape as **landed**, not as a
  realisation gap: §4's note states the whole mechanism — the write-escape permit pinned to the
  disclosed `writeTarget.Real`, `openMutationRoot` as the single place the fence picks a root,
  Execute re-resolving rather than trusting dispatch, the three verdicts that mint a permit
  (approved Gate including a remembered allow, the Auto · `confine=false` run, a `WritablePaths`
  in-fence run), the uniform family with `move_file`'s source excepted, and reads unwidened — with
  its code references refreshed to the landed seams. §10 now names `WriteEscapePermit` as the
  second permit riding the `SubprocessPermit` idiom, and says how the two differ: an absent
  subprocess permit refuses, an absent write-escape permit leaves the workspace fence governing.
  With the work landed, the `ISSUES.md` defect *an approved out-of-workspace write still errors at
  Execute* is removed from the register (ADR 0049).

### Fixed

- **A presented document now renders at its run's depth, inside its run.** The presentation entry
  was built at depth 0 and appended at the tail whoever raised it, so a delegate's document surfaced
  as if the human's own agent had shown it — and, worse, dropped a top-level block into the middle
  of a running delegation: `subAgentSpan` ends at the first entry standing at the parent's depth, so
  the rail framing that run stopped above the presentation and picked up again below it, one run
  reading as two railed stretches with an unframed gap between them. The `Depth` and `SpawnCallID`
  that `domain.PresentRequest` now carries ride the presenter's `presentedMsg` through to the entry,
  and `transcript.addPresented` commits it through `place` like every other delegated entry: the
  block is railed at the level the rest of its run is drawn at, and it lands INSIDE that run's
  stretch — under a concurrent fan-out the spawning call id is what picks the right sibling, since
  siblings share a depth. The record needed nothing: depth and run identity are generic `wireEntry`
  members, so a resumed scrollback replays the presentation railed exactly as the live one drew it.
  With the work landed, the `ISSUES.md` entry *A presented document carries no sub-agent depth* is
  removed from the register — item 9 closed its `AskRequest` half.

- **A prompt click below a phantom-wrapped line now seats the caret exactly.** bubbles' `wrap`
  appends a PHANTOM trailing sub-line to a logical line whose content reaches the width — the seat
  it keeps for a caret past a full line — and its `CursorDown` can never enter it: the step's column
  guess clamps at `len(line)-1` while that sub-line begins at `len(line)`. The mouse path's
  `reseatCaret` was a bare run of those steps, so it stood still on such a line and a click on the
  row BELOW it landed a row short, on the wrong logical line entirely, with the drag-selection and
  the clipboard following the caret there. It is now the Height-aware walk `seatCaret` already
  expressed for logical targets, aimed at a visual one: it steps whole logical lines (`CursorEnd`,
  which IS the last sub-row phantom included, then `CursorDown`, which therefore always reaches the
  next line) while accumulating each line's visual row count from the widget's own
  `LineInfo().Height`, then seats the residual sub-row inside the line the target falls in. The
  phantom row is itself clickable and seats the caret at that line's END, where `CursorEnd` puts the
  keyboard's — never skipped. Every count comes off the widget, which is the wrap oracle (ADR 0030
  §6), so nothing here re-derives a geometry the terminal did not draw; the walk closes with the
  same no-op `SetHeight` re-clamp `seatCaret` ends with, so a click deep in a grown box no longer
  leaves the view on a stale downward offset either.

- **A hang the block cannot hold now collapses to zero instead of overrunning the width cap.**
  `hangingPrefixes` floored its wrap at one column and then prepended the marker anyway, so a
  two-column bullet in a one- or two-column block composed a three-cell line — layout.md's absolute
  width cap broken by the very glyph decorating what it capped, and the same floor was repeated in
  `gutteredWrap` and in the user block's accent mapping. A block too narrow to seat the marker AND
  one column of text now sheds the marker and the continuation indent WHOLE and wraps the text flat
  at the block's full width; at every width that seats both, nothing changes — `clipWrap` still
  returns `hangingWrap`'s own lines for fitting text and the markerless `clipCells` path is a no-op
  by construction. The decision has one name, `hangCollapses`, that all three sites ask. `layout.md`
  gains the rule beside its hanging-indent doctrine: markers are shed, never squeezed, the same
  ladder a pane title spends its width by.

- **The `internal/security` file map now names `ErrRootInaccessible` on its `pathsafety.go` line.**
  The package doc's map names the sentinels each file owns — `ErrPathEscape` for `pathsafety.go`,
  `ErrSymlinkedParent` for `safeio.go` — so a reader looking for the error a surface must match
  finds it without opening the file. `pathsafety.go` gained a second sentinel for the root itself
  being deleted, renamed or not a directory, and the map had not followed: the one error whose
  whole point is that it is *not* an escape was the one the map left unnamed. It is now named
  beside `ErrPathEscape`, carrying that distinction in half a line.

- **A session record stamped ahead of the wall clock now reads "just now" by decision rather than
  by accident.** `relativeTime` computed `now.Sub(t)` and handed the raw duration to its switch,
  whose first arm (`d < time.Minute`) swallows any negative value — so clock skew, an NTP step
  back or a restored snapshot rendered correctly only because the arms happen to be ordered that
  way. A negative duration is now clamped to zero before the switch, mirroring `formatElapsed`'s
  own clamp on the status-line clock, so the answer for a future timestamp is stated in one place
  and no later rearrangement of the coarse arms can turn skew into garbage. `TestRelativeTime`
  gained the future-timestamp row.

- **A `/settings` reset of a key whose default is UNSET now reaches the running session instead of
  stopping at the file.** `settingsApplied` guarded the live apply on a non-empty value, so
  resetting one of the keys that default to nothing — `web-search-endpoint`, `editor`,
  `present.command`, `present.host`, `system-prompt-text`, `system-prompt-file`, `tools.disabled` —
  removed the key's line from `config.yaml` and told the engine nothing: the session went on running
  the old value until a restart, and the config watcher could not heal it because `ResetSetting`
  refreshes its self-write baseline the moment the line goes. The guard now asks about the EDIT
  rather than its value — a reset applies whatever default it recorded, and the empty string is how
  the dispatcher is told "the file no longer sets this key", which every one of those keys already
  answers with the built-in default a fresh start would have resolved (the built-in search provider,
  the `$VISUAL`/`$EDITOR`/OS-opener ladder, a presentation ladder rebuilt from a cleared field, the
  empty disabled-tool roster, a system-prompt block re-read from the file that no longer carries the
  key). That contract is now stated on the dispatcher itself and pinned from both sides. An empty
  value that is NOT a reset still skips: the pane never writes one, so the only source is a re-read
  that found a key gone, and that case is left exactly as it was.

- **A workspace root that will not open is now reported as `security: workspace root is not
  accessible`, not as a path escape.** Every Safe I/O primitive pins an `os.Root` at the root
  before it touches anything, and a failure to pin it — the root deleted or renamed under a live
  session, its permissions changed, a configured root that is not a directory — was wrapped as
  `ErrPathEscape`, so the operator and the model were told that a path resolving perfectly well
  inside the workspace "resolves outside the workspace root", and the one thing that had actually
  broken went unsaid. The new `security.ErrRootInaccessible` sentinel wraps exactly the
  could-not-open-the-root branch (`SafeReadFile`, `SafeOpen`, `SafeCopyFileFrom`'s source root,
  `SafeRename`, and the mutation-root pins `SafeWriteFile` / `SafeRemove` / the approved-escape
  anchor go through); every path-escape wrap, permit resolution included, stays on `ErrPathEscape`
  byte-for-byte. The caller sites that key off the escape gained a distinct arm ahead of it:
  `move_file` stops retrying an unopenable root as copy-then-remove, the copy/move pre-flight no
  longer reads a stat that never happened as "free to land", a read tool says the root's own words
  instead of "file not found", the multi-root read scope no longer falls through to an extra root
  when the workspace could not answer at all, and a workspace context file reports the lost root
  rather than blaming the file. Tests pin the split in both directions, so the two sentinels
  cannot be collapsed back into one.

- **`NormalizeURL` now strips the whole trailing run of root dots, so a `DenyHosts` entry cannot be
  dotted around.** The normaliser removed exactly one trailing dot, so `https://denied.example.com../x`
  normalised to the host `denied.example.com.` — a residual dot that matches no deny entry
  (`hostMatches` asks for an exact host or a `"."+entry` suffix), while the dial path rebuilds the
  request from that same normalised URL (`internal/tools/network.go`) and DNS reaches the denied host
  anyway. The strip is now a loop, still guarded so a bare `"."` host is left alone, and the doc
  states the wider normal form. The IDNA fallback beside it — a non-ASCII host that
  `idna.Lookup.ToASCII` refuses is handed back unchanged, exactly as `net/http.canonicalAddr` dials
  it — gained the test it never had, asserting both that the profile really rejects the chosen host
  and that the guard gets the caller's own bytes back rather than an error or a half-mapped string.

- **A headless run's `RebindSpec` no longer states a different `response-reserve:` share than the
  run's own `Config` divides by.** `rebindSpecFor` reads the share off the `config.Options` copy it
  is handed, and a session's copy arrives already overlaid by `liveSettings.rebindInputs` — a seam
  `apogee headless` never passes through. So the spec it built carried the TOP-LEVEL share while the
  `Config` beside it divided the window by the bound entry's own override: two numbers for one
  split, latent only because nothing read the spec's field on that path. The headless path now
  resolves the share once (`config.ResolveResponseReserve`), writes it onto the copy the resolver
  reads, and composes the `Config` from the share the resulting spec states — so the two cannot
  drift apart, whatever comes to read the spec later.
- A NaN reserve fraction reaching `internal/context.Allocate` now falls to the built-in 0.20 default
  like every other non-share value. The unset guard compared the fraction against both bounds
  (`fraction <= 0 || fraction >= 1`), and NaN compares false to everything, so it slipped past into
  `int(float64(window) * fraction)` — implementation-dependent garbage: a silent zero reserve on
  arm64, `math.MinInt64` on amd64, which made `working` negative and every split field nonsense,
  among other things disarming the automatic compaction trigger (negative History reads as "no
  basis"). The guard is now stated positively, `!(fraction > 0 && fraction < 1)`, matching
  `config.isResponseReserveShare`'s treatment of NaN on the config path. The config layer already
  refused NaN at load; this closes the same hole for a non-config caller and makes the defensive-floor
  claim in `Allocate`'s own doc comment true again.
- The two shipped scheme files' `warning` comments now describe its first consumer the way the
  status line renders it — the quiet *qualifier* before the activity clock, the form
  `internal/scheme/scheme.go` carries — instead of the stale "'quiet' suffix" wording
  (`internal/scheme/schemes/dark.yaml`, `light.yaml`). With that fixed, the 2026-08-15 residuals
  register was pruned to the new actionability bar: the pre-existing findings (TUI test-file size
  debt, the missing `blockstate_test.go`) left `ISSUES.md`, and the Windows job-object breakaway
  assertion folded into its Phase-5 owner-run leftovers entry.
- The thinking-effort hint now rides the in-band error path too. A server that wraps its failure
  in an HTTP 200 — an aggregator's usual shape — produced a bare error where the very same failure
  arriving as a 500 got the "this request carried `chat_template_kwargs`" explanation, so a chat
  template that rejects an effort value was self-explaining on one framing and silent on the other.
  `inBandError` and its streaming twin `inBandErrorDelta` now take the same `hasTemplateKwargs`
  flag the status paths do; the hint rides the wrapping error and never `StatusError.Body`, and a
  classified context overflow stays unhinted on both framings — no thinking effort caused it.

- The session-naming call's drop-the-flag fallback now fires only for the `enable_thinking:false`
  kwarg it actually sets: a naming request that asks for a reasoning LEVEL is no longer silently
  stripped and re-sent when an Upstream rejects it with a 4xx. Behaviour-preserving today
  (`title.Prompt` only ever asks for effort `off`), and the guard now says what it means.

- The TUI's stall guard now restamps its quiet clock only for the activity kinds it actually
  watches. `moveActivity` used to restamp on every move — `compacting` and `stopping` included —
  while `quiet` reported only for `thinking`/`responding`, so the two seats were coupled by hand and
  a future watched kind would have inherited the restamp silently. Both now read one shared
  `activityKind.isQuietWatched` predicate, pinned by a table covering every kind in the vocabulary.

- The `upstreamChoices` overview now credits the helper that actually builds the synthesized
  ephemeral row's label. Its second paragraph still named `hostFromEndpoint` as the source of "the
  endpoint's host as its label", but since the alias gained its collision suffix that label comes
  from `config.aliasFromEndpoint` (`opts.HostAlias`) — the same helper the paragraph below already
  credits for keeping the synthesized label distinct from a configured `name`. Comment only — no
  behavior change.

- The `tempRoot` package-rule comment no longer names a suite that is not there. Its rule sentence
  ended "plus this file's own fixtures", but `internal/tools/path_safety_test.go` holds no `Test`
  function at all — it is the shared fixture home (`writeFixtureFile`, `realPath`, `tempRoot`) the
  write-family suites take those helpers from. The trailing clause now says that instead of
  claiming a local suite. Comment only — no behavior change.

- **The `setProcessGroupTeardown` overview no longer claims an absolute the same file refutes.** Its
  `cmd.Cancel` bullet said a cancelled or timed-out command "never orphans its children (or, when
  confined, an orphaned sandbox-exec wrapper)", which the setsid escape documented a few lines below
  contradicts — a descendant that calls `setsid`/`setpgid(0,0)` leads a new group that no
  negative-PID kill aimed at this one reaches. The bullet now scopes the promise to what the group
  still holds (its children, and when confined the sandbox-exec wrapper) and points at the escape
  note for what leaves it. Comment only — no behavior change.

- **ADR 0049 now records the resolved-first permit routing its own fix introduced.** The decision
  text describes the write-escape permit as the branch a mutation takes once the workspace fence has
  refused it; the landed code asks the permit question FIRST and on the RESOLVED path
  (`openMutationRoot` → `namesPermittedTarget`), with the in-workspace fallback staying lexical
  (`rootRelative`, `internal/security/safeio.go:629`) and `os.Root` enforcing the symlink-component
  half at use time. A dated amendment note (2026-08-14) appended to the ADR states both that
  ordering and the case it makes executable — an argument spelled inside the workspace that resolves
  outside it through a disclosed link takes the permitted branch and is pinned to the disclosed
  target's own deepest-existing ancestor, rather than being refused as the one call the operator
  actually read. The original decision text is untouched; the amendment's claims are derived from
  `internal/security/doc.go:103-104` and `internal/security/writepermit.go`. Documentation only — no
  behavior change.

- The `show-scrollbar` doc comments now state the scope the key actually has. `ui.show-scrollbar`
  has gated the scroll bar in the transcript AND in every popup pane since the popup panes gained
  one, but two summaries still framed the bar as the transcript's alone: `uiConfig.ShowScrollbar`
  ("gates the transcript's scroll bar and the column it hangs in") and `tui.Options.HideScrollbar`
  ("takes the transcript's scroll bar away"). Both now name the popup panes alongside the
  transcript, matching the key's own prose in the shipped `config.yaml`. Comments only — no
  behavior change.

- **A cancel inside the re-stream hold-off now leaves a resumable Turn.** A transient in-band fault
  makes the Turn wait `restreamHoldoff` (1s) before re-sending the same request, and a cancel
  arriving inside that window fell through to the give-up path: an Esc a moment too late ended the
  Turn `endAbandoned` — Exchange closed, deferred queue cleared, and an `ErrorEvent` blaming the
  upstream for the user's own cancel — where the same Esc 100ms earlier gave the resumable
  `endCancelled`. `respondAndReview` now re-checks `ctx.Err()` when the hold-off returns early and
  routes it as `turnCancelled` with no `ErrorEvent`, so cancel semantics are uniform wherever the
  cancel lands: the Turn rolls back to its pre-request boundary, the drained corrections go back on
  the deferred queue, the Exchange stays open, and a re-`Step` re-attempts it. The
  `StreamResetEvent` already emitted stays consistent with that rollback — the partial reply it
  tells observers to discard is exactly what the rollback drops. Both halves of the branch are
  pinned in the new `internal/agent/loop_test.go`: the cancelled hold-off, and the hold-off that
  merely elapses and still re-streams.

- **The prompt box sizes a value carrying a bare CR the way the widget draws it.**
  `inputContentRows` — the mirror of the textarea's own wrap that gives the input box its height —
  split the value on `"\n"` alone, while the widget's sanitizer rewrites every `'\r'` AND every
  `'\n'` as one newline before it splits into logical rows
  (`bubbles/v2@v2.1.0/internal/runeutil/runeutil.go`, `textarea.go:504`), so a CR the widget turned
  into a row boundary was measured as a rune inside a row and the box came out a row short. The
  split now folds a bare `'\r'` too, and folds `"\r\n"` as TWO boundaries rather than one, because
  that per-rune rewrite is what the widget actually does: its own answer for `"a\r\nb"` is three
  rows. No draft reaches this today — every caller hands over the widget's already-sanitised value
  — so this is width-mirror fidelity of the kind ADR 0030 §6 governs, pinned by new cases in both
  `inputContentRows` suites (the deterministic table and the one that asks a real textarea).

- **A hostile upstream's error body can no longer exhaust memory.** Both non-2xx paths —
  `(*Client).statusError` on the unary round-trip and `(*Client).statusDelta` on the stream — read
  the upstream body with `io.ReadAll` before anything caps it, and the request timeouts default to
  0, so a server answering a multi-GB error body was buffered whole into the agent process. Each
  read is now wrapped in an `io.LimitReader` at `maxErrorBodyBytes` (64 KiB), far past any genuine
  error payload. What is read flows through the same path as before — the context-overflow sniff
  and `sanitize`'s key redaction plus 500-byte truncation — so a truncated body needs no error kind
  of its own: it surfaces as the same `ErrContextOverflow` / `*StatusError` / `DeltaError` it
  always did. A marker that would have classified a reply as a context overflow but sits past the
  cap is simply not read, which is what the new tests assert on both surfaces.

- **`/server` identifies the session's entry by NAME, not by endpoint.** The five sites that decide
  "which configured entry is this session on" — the picker's already-on branch, the row it opens on,
  the `· current` mark, and `/settings`' server-row twin of the first and its `(current)` mark — now
  compare the entry's name against the bound alias (`Options.HostAlias`, which is exactly the bound
  entry's name on every start shape) instead of comparing URLs. Two `servers:` entries pointing at
  one endpoint are therefore told apart: the mark sits on the bound entry alone, and picking its
  sibling performs a real switch, so that entry's key source rebinds and the recorded `server:` pin
  names the entry the human actually chose (ADR 0036 decision 1). Sessions with one entry per
  endpoint — every ordinary config — see no change.

- **An approved escape through a workspace-internal symlink now executes.** `openMutationRoot`
  picks its branch by RESOLVED target and asks that question first, so an argument spelled inside
  the workspace that reaches an outside target through a link runs through the permitted ancestor
  root — the path the approval pane disclosed — instead of being refused by the lexical fence after
  the operator already said yes (ADR 0049; the Gate's Allow was unexecutable for this shape). The
  whole write family lands on the disclosed target uniformly, never through the link: `delete_file`
  removes the resolved outside target and leaves the workspace link dangling. The never-worse floor
  is unchanged by construction — permits are minted only for disclosed escape targets, so with no
  permit, or with one naming a different path, every call behaves byte-for-byte as before.

- **The prompt box's width mirror sanitises a line exactly as the widget does.** `wrapRowStarts`
  (`internal/tui/inputaccent.go`) — the mirror of the textarea's own soft-wrap that both sizes the
  prompt box and seats the inline token accents — expanded TABs the way the widget's sanitizer does
  but kept the two rune classes that sanitizer DROPS: `utf8.RuneError` and every non-tab control
  rune. A line reaching the mirror from outside the widget carrying one of those was measured one
  rune longer than the value the widget holds, so an accent past it was seated on the wrong run of
  cells, and a `U+FFFD` — one cell wide to the ruler, absent from the widget — could move the wrap
  itself and the box's row count with it. The step is now the whole per-line rule
  (`sanitizeInputLine`): a `utf8.RuneError` dropped, a TAB expanded to four spaces, every other
  control rune dropped, anything else kept. `\r`/`\n` need no per-line handling because the widget
  folds them into row boundaries before it ever splits its input into lines — an argument now
  recorded at the mirror's own contract rather than only on the caret path. The two widget-oracle
  tables (`TestWrapRowStartsMirrorsTheWidget`, `TestInputContentRowsMirrorsTheWidget`) gained cases
  for both dropped classes. A draft typed into the box can hold none of these runes, so ordinary
  sessions see no change.

## [0.14.0] — 2026-08-14

### Added

- **Re-selecting the model or the server you are already on now records the pin.** Both accept
  paths answered the re-selection with a note and returned before the recording seam —
  `bindPickedModel`'s "already bound to …" and `switchToServer`'s "already on …" — so the one
  choice a human could NOT pin was the one the heartbeat, or start-up, had already made for them:
  reaching `model:` / `server:` meant moving away and back. Both branches now offer the name to
  the same seam the moving path uses and state what came back: `model: saved — this server starts
  on it next time`, and for the server twin a new `server: saved — this entry starts the next
  session` line — a line of its own, because there is no move note to carry the ` · server: saved`
  clause. A failed write stays a footnote under the answer, an unwired seam stays silent, and every
  other skip (remember off, an unlisted entry, a launcher-fronted one) stays the binary's to make.
  The `/settings` server row reaches `switchToServer` by the same door, so it pins too.

- **The input fold is now pinned at the replacer, not only at the widget's output.**
  `lineEditor.flattenLine` folds a pasted newline, tab or carriage return to a space before a
  one-line field can hold one, but no in-package door reaches the `\t` and `\r` branches with the
  character intact — every write into a bubbles textarea runs through the widget's own rune
  sanitizer first, which spends a tab as four spaces and a carriage return as a newline — so the
  only coverage was `TestSettingsPasteLandsInTheOpenField`, which pins that whole pipeline's end
  state rather than this substitution. A new `internal/tui/lineeditor_test.go` exercises the
  package-level `lineBreaks` replacer directly: each of the three folds to a single space, the fold
  is one rune for one rune (a `\r\n` is two spaces, which is what the caret arithmetic around
  `caretRune`/`caretToRune` rests on), and a folded value survives a second pass unchanged. The
  field's own invariant is now pinned where it is decided, so it stays pinned if that sanitizer is
  ever reconfigured or replaced.

- **A Mechanism row without an ID is now refused at registration.** `MechanismRegistry.Add`
  already turned away the reserved experimental sentinel, a duplicate `MechanismID` and a hook
  implementing no hook interface; it now also turns away a row whose `Descriptor.ID` is empty,
  which used to become a catalogued Mechanism with a blank canonical ID — attributing its
  `MechanismFiredEvent`s to nothing and sorting first in the ordering's stable tiebreak, silently.
  The curated catalogue was never reachable this way (`register` panics at `init()` on an empty
  ID), so this is a new guard for embedder- and test-built rows.

- **The remember-model decision now has a written record.**
  [ADR 0048](docs/adr/0048-apogee-remembers-the-model-choice-per-server.md) states why the model
  choice is persisted in apogee's own config rather than in llama-launcher's — that file is a curated
  library of presets a human copies between machines and reads with more clients than this one, while
  which profile a session happened to load last is apogee's session state (ADR 0029 decision 4 stands
  untouched) — why the two server classes remember in two different keys (a wire model id in a plain
  entry's `model:`, a Launch profile name in a launcher-fronted entry's `launch-profile:`, and never
  both on one entry), why only an explicit pick or a committed load records, why the pointer lands on
  the **actuating** entry even when the load moved the session to an endpoint no entry names, and why
  the start-up restore yields to ANY instance already running under that launcher instead of stacking
  a second model onto the GPU. It records what was turned down on the way: an "active profile" key in
  the launcher's own YAML, one key for both server classes, validating the recorded profile's
  existence at config load, insisting on the restore by unloading what runs, restoring in
  headless/bench runs, and recording heartbeat-observed rebinds. A per-server override of the toggle
  is recorded as deferred rather than denied. `CONTEXT.md` gains **remember-model** in the Launch
  profile section of the language, defined against the actuating entry and the yield rule.

- **Switching `remember-model:` now takes effect immediately.** Turning the toggle on in `/settings`
  — or in your config file, which apogee watches for the whole session — makes the very next explicit
  `/model` pick and the very next Launch profile that commits get recorded, instead of the setting
  sitting inert until you restart. Turning it back off stops the recording just as immediately, so a
  session you would rather not have written down stays unwritten from that keystroke onward.

- **A launcher-fronted server now comes back on the Launch profile you left it on.** With
  `remember-model:` on, an interactive session that starts on an entry carrying both
  `llama-launcher:` and `launch-profile:` asks the launcher once, at start-up, whether that profile
  should be loaded — and loads it exactly as if you had picked it from `/model`, with the same
  progress narration and the same completion. It yields rather than insists: ANY server already
  running under that launcher — any profile, any port — leaves the restore skipped with a line saying
  what is serving, so a model you started by hand is joined instead of stacked on top of, and a
  recorded profile the launcher no longer defines is a note rather than a failure. When the profile
  you recorded is already what the launcher serves, the ordinary start-up bind is the restore and
  nothing is said at all. With the toggle off (the default), with no pointer recorded, or on a server
  no launcher fronts, start-up reads no launcher config and probes for no servers. Headless runs
  never restore anything: they actuate no servers at all.

- **A Launch profile you load is now remembered by the server that loaded it.** With
  `remember-model:` on, a profile load that COMMITS — the one that landed on the server your session
  is already talking to, and the one your session had to follow onto another server — writes that
  profile's name into the `launch-profile:` key of the entry apogee drives the launcher through, so
  the server can come back on the same profile. The pointer lands on that entry even when the load
  moved your session to an address your `servers:` list does not name, because the entry whose
  `llama-launcher:` key you follow is the one that can act on the launcher next time. Only a commit
  records: a load that failed, one whose health wait timed out, and one your session could not follow
  all leave the key alone — and so do `/unload-model` and `/stop-server`, because freeing the GPU now
  is not the same as forgetting which model this server runs. Nothing is recorded with the toggle off
  (the default) or when no launcher-fronted entry can be named, and a write that could not land is a
  note rather than an undo: the profile is loaded either way. When the pointer is written, the
  transcript says so on a line of its own.

- **An explicit `/model` pick is now remembered by the server you made it on.** With
  `remember-model:` on, picking a model — from the `/model` overlay or by naming it as
  `/model <id>` — writes that id into the `model:` key of the `servers:` entry your session is on,
  so the next session on that server comes back bound to it without being asked. Only an explicit
  pick counts: a model change the heartbeat merely observed is news about the server rather than a
  choice, and the `--model` / `APOGEE_MODEL` startup overrides are facts about one invocation, so
  neither writes anything. Nothing is recorded when there is nothing the key can honestly carry
  either — the toggle off (the default), a session on a server your config does not list, or a
  launcher-fronted entry, whose `model:` is a deliberately empty discovery hint and which will
  remember its choice as a `launch-profile:` instead. A pick that failed to bind records nothing, so
  the file never describes a model you are not on, and a write that could not land is a note under
  the change rather than an undo of it: the session stays on the model you picked either way. When
  the choice is written, the transcript says so on a line of its own.

- **The config writer can now set one key inside one `servers:` entry.** It is the plumbing the
  remember-model feature will write through: `model:` or `launch-profile:`, on the entry picked out
  of the list by its `name:`, either rewritten in place — keeping your indentation, your alignment
  and your end-of-line note — or appended to that entry's block, with every comment, every other line
  of the entry and every sibling entry coming back byte-identical. A re-set of what the file already
  says writes nothing at all, not even a new timestamp. Exactly those two keys may be addressed and a
  caller naming any other is refused before the file is even opened, so a key apogee does not write
  cannot so much as seed a config on its way to being refused; there is no delete form, because
  forgetting a recorded choice is an edit of your own file rather than something apogee does on your
  behalf. Anything the edit cannot do surgically is refused with the file left exactly as it was —
  including a rewritten list that would no longer load, so a `launch-profile:` on an entry with no
  `llama-launcher:` key never reaches the disk. A refusal about a `servers:` entry name the file does
  not carry now also names the entries it does carry, which the key-source writers say too. Nothing
  calls the new writer yet.

- **apogee can be told to remember which model you were on.** The schema half of it lands first: a
  top-level `remember-model:` toggle, off by default, which gates both halves of the feature — the
  write apogee makes back into this file when you pick a model explicitly, and the restore the
  interactive TUI does at the next start — and a per-server `launch-profile:` pointer, the key a
  launcher-fronted entry records its Launch profile in (a plain multi-model server keeps recording
  into the `model:` key it already has, and llama-launcher's own config is never written). Both keys
  are hand-settable too, and the pointer is checked at startup like every other entry key: one on an
  entry with no `llama-launcher:` key is refused, because a profile is loaded THROUGH the launcher
  and an entry apogee cannot launch has nothing to actuate it, and a whitespace-only value is
  refused on the reasoning `llama-launcher:` already carried. Whether the named profile still exists
  is deliberately not asked here — the launcher's config is read fresh at use time. Nothing records
  or restores yet; the toggle shows up in `/settings` and the keys parse.

- **The naming call's prompt assets now state what an edit to them costs.**
  `internal/title/prompts/` gained the wording-drift README the probe battery's assets already
  carry: re-word one of these files and every session named from then on is named by a different
  instruction, there is no version constant to bump here — a title is a maintenance nicety, not a
  comparable record — so the pin tests ARE the gate, and a `.txt` in that directory carries no
  comments of its own, because a comment line would be sent to the model as part of the prompt. The
  README's claim is now true as well: `user-instruction.txt` ("Reply with the title only.") and
  `window-header.txt` ("The user's requests in this session, oldest first:") were guarded only by
  assertions that compared against the loaded variables themselves — they passed whatever the assets
  said — and are now pinned byte-for-byte by a new test beside the system prompt's existing phrase
  pin, so a silent re-wording fails the suite instead of quietly re-shaping every session name.

- **A transient upstream blip mid-stream no longer kills the exchange.** When a reply faults on an
  in-band error whose class the provider marked retryable — a 429, a 5xx, or an aggregator's
  `provider_unavailable`, delivered inside an HTTP 200 partway through the stream, past every retry
  the HTTP client could still make — the Turn now re-sends the same request ONCE: it emits the same
  `StreamResetEvent` an ActionRetry does (so a streaming Driver discards the partial reply), waits a
  fixed second, and streams again. A re-stream that lands is silent, exactly as a recovered overflow
  fold is — no `ErrorEvent`, no wasted Turn — and the recovery is bounded by a per-Turn latch of its
  own, independent of the ActionRetry cap and of the one-fold-per-Turn budget: a second fault of any
  class, a fault that was never transient, and a cancel during the wait all surface exactly as they
  did before. The reach that matters most is delegated work: a sub-agent's Turn is the parent's tool
  call, so a blip used to abandon the child's Exchange and hand the parent model "sub-agent faulted"
  in place of the result — the child's own loop now recovers before `Faulted` is ever set.

- **An in-band stream error now says whether its class is worth another attempt.** The wire error
  member an OpenAI-compatible aggregator delivers inside an HTTP 200 gained its `error_type` slug
  (it was parsed away before, surviving only as raw text), and the terminal `Delta` it becomes
  carries a new `Retryable` flag: set when the error's own code is one the client already retries at
  the HTTP layer — 429 or any 5xx, via the same `isRetryableStatus` — or when the aggregator typed
  the failure `provider_unavailable`, which is the observed OpenRouter shape where the code alone
  (a 404, a non-numeric slug, nothing at all) reads as terminal while the upstream is merely gone.
  An in-band 4xx stays terminal, and a `context_overflow` is never retryable — a prompt too long
  stays too long. The rendered error text and the kind selection are byte-for-byte unchanged, and
  the provider still never retries mid-stream itself: it classifies, and the loop decides.

- **The key-source decision now has a written record.**
  [ADR 0047](docs/adr/0047-api-keys-resolve-through-a-per-entry-key-source.md) states why a
  `servers:` entry names ONE key source instead of always carrying the secret itself (the config
  file is hand-edited, watched every second, and the file a user copies to a second machine), why
  the source runs at first use of that entry rather than at load, why an empty answer is a hard
  error rather than "no auth", and why a plaintext key earns a consented offer rather than a silent
  move — together with what was turned down on the way: a keychain library and a fourth
  `api-key-keychain:` source (a D-Bus client and a Keychain framework inside a CGO-free six-target
  binary, to reach a store a command already reaches, on hosts that half the time have no store at
  all — ADR 0042), an encrypted config (passphrase management for one field), precedence or fallback
  chains between sources (a configured key silently ignored, or a stale one silently used), running
  the command through a shell, and silent auto-migration (against ADR 0035's deliberate-edit grain).
  Windows migration is recorded as deferred rather than denied — there is no built-in generic-secret
  CLI to build the write-and-read-back pair from. `CONTEXT.md` gains **Key source** as a term of the
  language beside **Upstream**, and the thirteen comments across the code that were already citing
  ADR 0047 now cite a document that exists.

- **A plaintext `api-key:` now earns an offer to move it into the machine's own secret store.** At
  start-up, every `servers:` entry whose key is written out in the config file is collected; where
  the machine has a store apogee can both write to and read back from — the macOS Keychain through
  `security`, a Secret Service keyring through `secret-tool`, probed live so a headless box carrying
  the tool without a keyring is not mistaken for one that works — the session opens one pane per
  entry: move it, not now, or never for this entry. Taking the move stores the key under service
  `apogee` with the entry's name as the account (handed to the tool on STDIN, never in an argv where
  `ps` could read it), then READS IT BACK by running the very `api-key-cmd:` line it is about to
  persist and comparing the result to the key that went in — a mismatch or a failed read aborts with
  the config file untouched, so a migration can never leave an entry pointing at a key nobody
  stored. Only then is the entry rewritten, surgically: the `api-key:` line becomes an
  `api-key-cmd:` line and every comment, indent and neighbouring key stays byte-identical, with the
  writer refusing rather than guessing on any file shape it cannot edit exactly. What the file holds
  afterwards is an ORDINARY key source, read by the same resolver as any other. "Not now" persists
  nothing and is asked again next start-up; "never for this entry" writes `plaintext-key-ok: true`
  on that entry alone and is never asked again. Nothing is ever migrated without that answer, and no
  offer is made where the whole move could not be completed: a machine with no usable store — and
  every unattended `apogee headless` run, which has nobody to consent — gets a notice instead,
  naming the entries and the alternatives its owner can reach by hand (`api-key-env:`, an
  `api-key-cmd:` wrapper script, or at least `chmod 600` on the file).

- **Every place a session reaches for a server's API key now resolves that entry's key source.** The
  startup bind, a `/server` switch, a Sub-agent server's heartbeat, `apogee headless`, `apogee probe`
  and `apogee probe model` all ask the run's ONE resolver, so an entry whose key comes from a command
  or from a variable works wherever a literal `api-key:` worked — and pays for its source once per
  session, which is why switching back onto a server this session has already been on prompts no
  keychain a second time. A source that refuses fails the thing the user was doing rather than
  degrading the request: startup exits with the resolver's message, a `/server` switch is refused
  with the entry named and the session stays where it was, an unattended run stops before spending a
  token, and a Sub-agent server whose key could not be produced takes no delegations — they run on
  the session's own server and the reason is said once, instead of surfacing as a 401 inside a child.
  `APOGEE_API_KEY` still overlays the startup entry over whichever source the file named for it.

- **A key source now runs at first use of the server it belongs to, once per session.** The entry
  a session never moves onto never runs its command, so a config listing six servers no longer pays
  six keychain prompts at startup; the entry it does use asks its source once and every later seam
  reads the cached answer. An `api-key-cmd:` line is split the way a POSIX shell splits one and then
  executed DIRECTLY — no shell, no stdin, none of apogee's terminal, a 60-second bound and the
  trailing newline trimmed off — so a pipeline needs a wrapper script of your own and a backend that
  must ask you to unlock has to prompt through a GUI agent (pinentry-mac, the Keychain dialog) rather
  than over the frame apogee is drawing. An empty answer is a hard error naming the entry and quoting
  what the command said, never a silent unauthenticated request: a command that exits non-zero, times
  out or prints nothing, and a variable that is unset or empty, are all broken sources, because
  "this server takes no key" is spelled by naming no source at all. The cached answer is held against
  the entry's name AND its key fields, so editing a key source in the watched config re-resolves it
  while every other edit keeps the answer already paid for, and concurrent first uses — parallel
  sub-agents — share one run of the command instead of racing to prompt the same keychain twice.

- **A server entry can now say WHERE its API key comes from instead of carrying it in plain text.**
  Beside the literal `api-key:`, a `servers:` entry takes `api-key-cmd:` — a command whose standard
  output IS the key (`pass show …`, `op read …`, `security find-generic-password …`) — or
  `api-key-env:`, the name of an environment variable holding it. An entry names at most ONE of the
  three: two sources for one value is the duplicate-name defect wearing another key, so startup
  refuses the combination naming every source the entry set rather than inventing a precedence that
  would leave a configured key silently ignored. Naming none is still the keyless local-server
  default. A whitespace-only `api-key-cmd:`/`api-key-env:` is refused on the `llama-launcher:`
  reasoning (configured while naming nothing), and the per-entry `plaintext-key-ok: true` marker —
  the "never ask me again" answer to the coming startup migration offer — is legal only beside a
  literal `api-key:`. Validation stays offline: it never runs the command or reads the variable,
  because the key is resolved at first use of that entry, the way an endpoint is only ever asked by
  the live heartbeat. The seeded config template documents all three sources, the exactly-one rule,
  and the GUI-prompt note an interactive backend needs (apogee gives the command no terminal).

- **The approval pane now says when a call reaches wider than the file it names.** `diagnostics`
  takes one filename and its `go vet` half reads every `.go` file in that file's package directory:
  the tool's description said so and both of its result strings said so, but the approval prompt —
  the one surface a human actually decides on — quoted the model's `path` argument and nothing
  else, so "I approved `diagnostics.go`" and "it read every file beside `diagnostics.go`" were two
  different sentences and only one of them was on the screen. A tool can now state that widening in
  one line of its own words (the new optional `domain.ApprovalScoper` marker), the engine reads the
  marker at the single site it builds an approval request and carries the line on
  `ApprovalRequest.Scope` (an additive field, so no embedder's Approver breaks), and the TUI paints
  it as `Scope: …` under the reason and above the arguments it widens — stripped and flattened like
  its `Reason:`/`Fix:` neighbours, so a scope carrying a newline cannot paint a forged row of its
  own. The pane and the result string derive that sentence from the same clause, so the surface you
  approve on and the surface that records the run cannot describe one call differently. It is
  disclosure and nothing else — what the call may do is still the gate's decision — and every tool
  that declares no scope (all of them but `diagnostics`) raises exactly the prompt it always did.

- **Every reply is now bounded: apogee tells the server how big one answer may be.** Every agent and
  sub-agent turn went out with no `max_tokens` at all, so a thinking model could generate until the
  server's context wall and the turn then failed wearing the wrong error — a `/security-audit`
  sub-agent was measured spending 20,653 reasoning tokens over ~50 minutes, still going, with the
  server reporting `n_predict: -1`. The engine had already decided how big that reply was allowed to
  be — its Budget reserves 20% of the window for the answer and sizes the prompt around it — it just
  never told the server. It does now: every request states a ceiling derived from that same reserve,
  clamped to between 4,096 and 32,768 tokens, and an unknown window takes the floor rather than
  going uncapped (an unknown window must never read as "unbounded"). A routed sub-agent derives
  its own from the server it actually runs on, not from its parent's. Nothing about the ceiling is
  a Mechanism — it is engine behaviour, so it holds under `--bypass` too — and a pre-request hook
  that sets `MaxTokens` still overrides it, which is what makes `SamplingParams`'s "a nil field
  leaves the loop's value untouched" true of that field for the first time. Set `max-output-tokens:`
  on a `servers:` entry to pin your own ceiling for that server whatever its window says: it is the
  only way to let a cloud endpoint that advertises no window answer at length, and the compaction
  summarizer and the session namer keep the 4,096 caps they already bounded themselves with.

- **A reply stopped by that ceiling no longer reports itself as empty.** A thinking model that
  reasons all the way to the cap without emitting one visible word used to fail the turn as
  `upstream returned an empty reply (finish: length)` — a message naming neither the cap that
  stopped it nor the tokens burned reaching it (20,653 of them in the incident), and one that reads
  like an upstream fault worth retrying. That one case now gets its own failure: it names the
  ceiling apogee sent, roughly what the reasoning cost, and the key that raises it, so the remedy
  reads as a bigger `max-output-tokens:` or a smaller task rather than a retry into the same wall.
  Every other empty reply keeps exactly the message it had, and a cut-off turn still fails exactly
  as it did — the change is what you are told, not what happens, and being engine-level it holds
  under `--bypass` too.

- **The `/` menu and `/skills` now say where each skill came from.** A loaded skill's row carried
  only fields the `SKILL.md` writes for itself — the id, the display name, the summary — so a skill
  a cloned repo shipped and one from your own library read exactly alike. That is the residual the
  id refusal leaves behind: a repo-supplied skill may still call itself `Confine`, describe itself
  as the verb it imitates, and — matching a typed partial exactly — sort ABOVE the genuine row, so a
  habituated `/conf`, tab, enter takes the repo's. Every row now names its source beside the id
  (`✦ /clean-code · workspace`, `· library` for the global library, `· elsewhere` for a dir neither
  root accounts for), and the `/skills` report labels its rows from the same renderer. The label
  sits BEFORE the description on purpose: the description is the untrusted half and it is long, so a
  source rendered after it would be the first thing a padded summary pushed off the pane's edge.
  What a row renders of an id is bounded to match — folded onto one line, its whitespace runs
  collapsed, and clipped at 32 runes with a trailing `…` — so a padded id can no longer paint as a
  short innocent token whose payload is clipped away at the pane edge, where nothing tells the
  reader anything was cut. The skill's display name and summary are flattened at both surfaces for
  the same reason: a newline kept in either used to paint further rows the pane never authored,
  which is how a forged row would have carried a false source label of its own.

- **`copy_file` can now copy FROM a read-only root, such as your skills library.** A skill that
  ships a template or a checklist beside its `SKILL.md` could be READ by the model — `read_file`,
  `list_dir`, `grep` and `find_files` have reached the configured read-only roots for a while — but
  copying one into the workspace was refused as a path escape, so the model had to read the file and
  re-type it into a `write_file`, badly and at the cost of its context. `copy_file`'s SOURCE now
  resolves over the same roots those four read tools use: an ABSOLUTE path the workspace refuses is
  tried against each configured root in turn and read through an `os.Root` pinned at the one that
  accepts it. Nothing about the write moves. The DESTINATION is workspace-fenced exactly as before,
  a destination naming a read-only root is refused, a RELATIVE source still resolves against the
  workspace alone (so no one name can mean two files), a source under no root keeps the same
  uniform refusal, and `move_file` — whose removal of its source IS a write — is unchanged and still
  refuses a source it does not own. The approval prompt and the blast-radius classification keep
  reading the destination, which is where the bytes land.

- **The engine now reports each delegation starting and finishing, one child at a time.** A new
  `domain.SubAgentPhaseEvent` brackets every `sub_agent` run with a `started` and a `finished`
  phase, the finished one carrying that child's result. It fills the gap the tool-result stream
  leaves by design: a delegation group's results burst together, in call order, only after every
  child has joined, so an observer reading them alone cannot tell a delegation queued behind the
  Parallel agents cap from one already running, and cannot show an early finisher as done while
  its siblings work. Both dispatch paths emit the pair — a lone delegation reports the same
  timing a pooled one does — and the event carries the CHILD's identity (its depth, under the
  spawning call's id). Nothing about history changed: the up-front tool-call burst, the commit
  order, and the trailing result burst are all untouched (ADR 0039 decision 4), and a cancelled
  group, which never becomes a result, emits no finished phase. Observation only, and additive —
  a consumer that ignores the variant loses liveness and nothing else.

- **A sub-agent waiting for a slot now says so: `scheduled`.** Ask for twenty delegations with
  `parallel-agents` set to five and fifteen of them are queued — the model's calls are all announced
  at once, but only five children exist. Those queued rows used to look like running ones with
  nothing to show; each now states `scheduled` in its right-hand slot and nothing else — no tool-call
  count, no context fill, no live action — and carries no ▶ at all, since there is no work behind it
  to open and clicking it did nothing anyway. The row becomes an ordinary live delegation the instant
  its child starts. A lone delegation starts immediately and never shows the state.

- **apogee knows a few model shapes out of the box, and the profile now follows a model switch.**
  Run minimax-m3 with no configuration at all and its `</mm:think>` no longer leaks into the reply:
  a small built-in table matches the model name and applies the shape — gemma's `<think>`, gpt-oss's
  harmony channels, minimax-m3's `<mm:think>` — announcing itself once, as
  `model profile: minimax-m3 (built-in) — thinking: delimited`, so a wrong match has a first clue.
  Your own `model-profiles:` entry always wins over a built-in one and applies silently; an entry
  with `thinking: {style: none}` turns a wrong built-in match back off. The resolution now runs
  wherever the bound model is decided — at startup, on the mid-session switch the heartbeat
  observes, for a scheduled Firing, and for `apogee headless` — so a session that changes models
  reads the new model's dialect instead of the departed one's. Editing `model-profiles:` in
  `/settings` still applies to the running session, re-resolved for the model you are on.

- **A `servers:` entry can now be flagged as the one that takes delegations: `sub-agents: true`.** The
  flagged entry is the **Sub-agent server** (ADR 0045) — the cheap box a smart session hands its grunt
  work to — and it may carry its own `bypass:` and `mechanisms:`, saying what delegations to that
  server run as rather than what the session runs as: a present key replaces the value a child would
  have inherited whole, an absent one still inherits the parent's live posture. Every entry may also
  pin `context-window:` now, the top-level key per server, for an endpoint that advertises no window
  of its own. Three defects are refused at startup, each naming the entry: a SECOND flagged entry
  (delegations route to one server, so the message names both entries to choose between), `bypass:`
  or `mechanisms:` on an entry the flag is absent from (the posture rides the flag), and a negative
  `context-window:`.

- **Delegations now actually run on the flagged server.** Point `sub-agents: true` at a second
  `servers:` entry and every delegation this session makes runs THERE — a cheap model doing the grunt
  work while your smart one keeps the conversation — with the session itself staying exactly where it
  was. A second heartbeat observes that server on the same ten-second cadence as the session's, and
  what it finds is what the delegations get: the entry's `model:`, `context-window:` and
  `parallel-agents:` pins wherever you set them, and the server's own bound model, per-slot window and
  slot count wherever you did not. The model profile is resolved for whichever model that turns out to
  be, so a grunt model with its own thinking tags is read correctly even when the session's model has
  none, and the entry's `bypass:`/`mechanisms:` posture is what its delegations run with. Should that
  server be unreachable — or serving nothing anyone can name — delegations fall back to the session's
  own server with the session's posture, the behaviour every session had before this existed, and pick
  the other server back up on the beat after it returns. With no entry flagged nothing changes at all:
  no second monitor is started, and no delegation goes anywhere it did not go yesterday.

- **The session says where its delegations are going, and follows a config edit there.** Routing
  announces itself once when it starts — `sub-agents: routing to grunt (qwen3-8b)` — and once when it
  stops: `sub-agents: grunt unavailable — delegations run on the session server`. One line per change
  of destination, never one per delegation, and the line is said the first time either way, so a
  flagged server that was never reachable is visible instead of silently unused. Editing `servers:`
  while apogee runs now moves routing with it, like every other setting since the live-config work:
  add `sub-agents: true` and the session starts observing that server, remove it and delegations come
  home immediately rather than at the next beat, move the flag to another entry and the new server is
  picked up and announced on its own first beat. Editing the flagged entry in place — its `bypass:`,
  its `mechanisms:`, any of its pins — keeps the delegations flowing and applies at the next beat, and
  a `mechanisms:` key this build does not know is refused on the spot with the session left running
  exactly as it was.

- **A delegation that ran on another model now says which one.** With routing on, a sub-agent's
  collapsed line closes with the model that actually did the work — `4 tool calls · 12k/32k ·
  found three gaps · qwen3-4b` — and the headless run's per-delegation line does the same:
  `sub-agent: 12k/32k · repo-scout · qwen3-4b`. It is the first thing worth knowing when a
  delegation behaves unlike the session that asked for it. Nothing appears when the child ran on the
  session's own model, so routing off — or a Sub-agent server bound to the same model — renders
  exactly the line it always did. The answer is frozen when the reading lands and is kept in the
  session record, so a resumed session still shows the model the run really used rather than the one
  it happens to reopen on.

- **The read-only tools gained a seam for extra read-only roots.** A path a read tool is handed is
  now resolvable against the workspace root *plus* any additional read-only roots the host mounts —
  the groundwork for letting a model read the bundled files of a skill it has attached. Roots are
  tried workspace-first, each accepted path is pinned to the root that accepted it, and every root
  keeps its own fence: a symlink inside an extra root that points out of it is refused exactly as one
  inside the workspace is. Extra roots answer to absolute paths only, so no relative name can mean two
  files, and they are read live, so a change on the host's side is honoured by the next read. Nothing
  is mounted yet — with no extra root configured the tools resolve, refuse and word their refusals
  exactly as before — and the seam is read-only by construction: no write tool can take it.

- **`read_file` and `list_dir` now read under the host's configured read-only roots.** The two tools
  take the seam above: an absolute path the workspace refuses is resolved against each mounted
  read-only root in turn, so a file outside the workspace the host has deliberately opened up — a
  skills library, once it is wired — reads and lists like any other. A listing that starts in such a
  root walks entirely inside it, subdirectory by subdirectory, measured against that root rather than
  the workspace. Both tools say so in their description, so the model knows the address is worth
  trying. Everything else holds: relative paths still resolve against the workspace alone, a path
  under no root is refused with the same uniform escape message it always was, and with nothing
  mounted the two tools behave exactly as before. Mounting a directory for reading does not make it
  writable — `write_file`, `edit_existing_file` and the rest of the write family never see the roots
  and refuse the very paths `read_file` can read.

- **`grep` and `find_files` now search under the host's configured read-only roots.** The two
  discovery tools complete the read-only set: an absolute `path` the workspace refuses is resolved
  against each mounted read-only root in turn, and the whole walk is then pinned to the root that
  accepted it — matches and file names come back measured from that root, and every file the walk
  opens goes through that root's fence rather than the workspace's. So a skills library, once it is
  wired, can be searched by content and by file name, not just read a file at a time. Both tools say
  so in their description. A symlink inside such a root that points out of it is skipped exactly as
  one inside the workspace is, workspace-relative searches are unchanged, a path under no root is
  still refused with the same uniform escape message, and with nothing mounted both tools behave
  exactly as before.

- **Your skill folders are now mounted for the model to read.** The dirs skills are discovered in —
  `~/.apogee/skills`, the project's `.apogee/skills`, and its bare `skills/` when
  `use-project-skills` is on — are handed to the read tools as read-only roots, so the reference
  files, prompts and scripts bundled beside a `SKILL.md` can finally be read, listed, grepped and
  found by name. It follows the setting: flip `use-project-skills` in `/settings` and the project
  folder is mounted or unmounted for the very next read, with no restart. A headless run mounts the
  same dirs a session does, and a sub-agent inherits them from its parent without any wiring of its
  own. The dirs stay unwritable — no write or execution tool can reach them — and each keeps its own
  fence, so a symlink inside a skill folder that points out of it is refused. Nothing here is
  skills-specific below the composition root: the engine gained a generic `ExtraReadRoots` seam
  (`domain.Config`), and an embedder can mount whatever its own user has opened up.

- **An attached skill now tells the model where its own files live.** Reading the folder was only
  half the fix — the model still had no address for it. The injected skill block gained one fixed
  line naming the skill's folder and the tools that can read it, so a skill whose `SKILL.md` points
  at `references/testing.go.md` can actually be followed there. The line is written by the harness
  itself, not by the system prompt, so editing the prompt cannot quietly break the promise the
  read-only mount makes. A skill with no folder behind it is unchanged: no line, and the block is
  exactly what it always was.

- **The prompt that says confinement is unavailable now also says what to do about it.** When Auto
  wants to fence a command and the host cannot, the approval pane has been truthfully blaming the
  host — and leaving you to work out the way forward from a prompt that offers only allow or deny.
  It now carries a second line under the reason, `Fix: /confine off runs commands unconfined this
  session (disposable machines only)` — the same escape `/confine status` describes. Both prompts with
  that cause carry it — the one raised up front, and the one raised when a box that looked
  establishable failed at run time — because the fix is the same either way. Every other approval
  is untouched: a prompt you got because of the mode you chose has nothing to fix, only a mode to
  be in, so it reads exactly as it always has.

- **The copy primitive can now fence its two ends at two different roots.** `security.SafeCopyFileFrom`
  reads its source through an `os.Root` pinned at the SOURCE's root and writes its destination through
  an `os.Root` pinned at the DESTINATION's root, so a copy whose source is a read from somewhere the
  destination fence knows nothing about — a configured read-only root, such as the skills library — is
  expressible without loosening the write end: the destination root bounds the only thing the call
  creates, and a path escaping its OWN root is refused with nothing written, however it happens to lie
  relative to the other one. Every guarantee the one-root copy already made is kept — parents created
  inside the fence, bytes staged in the destination's own parent and renamed over it, the destination
  landing with the SOURCE's mode, a non-regular source refused. `SafeCopyFile` is now that function's
  equal-roots case and behaves exactly as before, so no tool's fence moves with this entry:
  `copy_file` still fences both of its ends at the workspace.

- **The residuals-sweep + configwrite-split plan is a saved, committed repo doc.**
  `docs/plans/2026-08-13 - 07 - residuals-sweep-and-configwrite-split-plan.md` landed in `73e8cf1`,
  closing the remember-model run's untracked-plan-doc residual; the plan itself stays unexecuted.

### Changed

- **The configwrite prose names files now, not positions.** The `internal/config` writer split left
  comments pointing "above" and "below" at text that had moved into another file:
  `configwrite_keysource.go`'s banner ("the same contract the two writers above are"),
  `configwrite.go`'s "Each writer above spells its own key", "the writers above's contract" and "the
  verification below is what catches that", and `configwrite_scalar.go`'s "the acknowledgement writer
  above" together with "the acknowledgement writer's contract above". Each now names the file it
  means, and the function where one is meant — `verifiedEntrySplice` in
  `internal/config/configwrite_keysource.go`, the acknowledgement writer in
  `internal/config/configwrite.go`, the scalar setting writer in
  `internal/config/configwrite_scalar.go`. `serverEntryAt`'s "every verification below" names
  `verifiedEntrySplice` as well. Comment-only: not a line of code changed.

- **The configwrite split's stranded plumbing now sits beside its callers.** Three helpers the
  split left behind moved to where the code that calls them lives, verbatim: `appendBlock` — the
  end-of-file block append the acknowledgement writer, the scalar writer and the legacy fold all
  reach for — out of `internal/config/configwrite.go` into `internal/config/configsplice.go`, the
  machinery every config writer shares; `listValue`, whose only callers are `renderSettingValue`
  and `scalarAtPath`, into `internal/config/configwrite_scalar.go`; and `lineCount`, whose only
  caller is `spliceTextBlock`, into `internal/config/configwrite_scalarsplice.go`. The pair's
  shared doc comment split with them, each half keeping the deliberate-duplication note about
  `cmd/apogee/settingsrows.go` that applies to it. A pure move: every line of code is
  byte-identical to the line it replaced, and the package suite is the proof.

- **The scalar setting writer's splice machinery now lives in its own file.**
  `internal/config/configwrite_scalar.go` had grown to 803 lines, double the ~400-line guide, so
  the machinery its drivers reach for moved to a new `internal/config/configwrite_scalarsplice.go`
  (448 and 375 lines): targeting (`ScalarTarget`, `ScalarTargetIn`, `valueFitsOneLine`), the
  text/block-scalar rendering (`spliceTextBlock`, `textLineParts`, `blockScalarEnd`,
  `textBlockBody`, `blockScalarHeader`) and the insertion placement with its commented-example scan
  (`scalarInsertion`, `settingLines`, `indentLines`, `CommentedExampleLine`,
  `commentedExampleBlockEnd`, `commentedKey`, `isCommentLine`, `deleteLines`). The writer core keeps
  the entry points, the key admission, the value rendering, the splice drivers and the verification.
  A pure move: not one signature, line of logic or doc comment changed, and the package's
  golden-file scalar suite is the proof.

- **The diagnostics tests take their temp roots symlink-resolved, like the rest of the package.**
  `internal/tools/diagnostics_test.go` held the package's last 15 raw `t.TempDir()` roots while
  every sibling suite — `exec_fence_test.go`, `read_file_test.go`, `file_ops_test.go`,
  `write_file_test.go`, `file_edit_test.go`, `find_replace_test.go`, and one call in this file
  already — routes through `tempRoot(t)`, which resolves the root's symlinks by the same rule
  `realPath` uses. On a box whose `TMPDIR` is reached through a symlink (macOS `/tmp`) a raw root
  is not what the tool resolves a path under it to, so any assertion on a writer's bare sentence
  breaks there and nowhere else — the hazard that already bit bare-sentence assertions elsewhere in
  this package. All 15 now take `tempRoot(t)`; the suite is unchanged in what it asserts, and the
  next bare-sentence assertion added here starts out portable.

- **The issues register records only actionable findings, in one run-residuals section.**
  `ISSUES.md`'s conventions now carry the bar a run residual must clear to be recorded — a defect,
  or a concrete missing test or doc with `file:line` evidence to act on; narration of how an item's
  text and its landed change differ, costs a plan already ratified, and cosmetic observations stop
  at the run's closing report. The eight per-run residual sections of 2026-08-13 merged into a
  single "Run residuals — open" section, each surviving bullet naming its origin run, so the file
  stops accreting a heading per executed plan. Closed under the bar (owner-ratified 2026-08-13),
  each accepted as intended or already recorded elsewhere: the two sub-agent prompt-guard
  item-text-vs-landed notes; `read_file`'s extra-read-root note (the comment at
  `internal/tools/read_file.go:104-109` already documents that an absolute path's root argument is
  unused); the missing `doc.go` file maps for `internal/context` and `internal/title` (both under
  the house ~10-file docmap threshold — the threshold working as designed); the misleading message
  on the already-pushed plan-doc commit `70c3586`; `run_tests`' inherited PATH (deliberate: the
  workspace-resident test runner IS the test command); the unexercised EXDEV copy-then-remove
  fallback in `MoveFile.move` (unreachable on a single-device tmpdir, as its item's hedge allowed);
  and the absent `internal/agent/prompts/` wording-drift README (its plan's design call 9 scoped
  that README to `internal/title` alone). The Darwin `/dev/null` seatbelt live check did not close:
  it moved into the Phase-5 owner-run leftovers entry, where the other hardware-gated passes live.
  The README accept paragraph the in-band-retry run left with a ragged wrap was reflowed to the
  file's width, wording unchanged.

- Repointed the open `verifiedEntrySplice` refusal-message issue in `ISSUES.md` at its post-split
  home (`internal/config/configwrite_keysource.go:280`), replacing the stale
  `internal/config/configwrite.go:1602` citation left behind by the configwrite split.

- Docs: swept the accept-behaviour prose that predated `runsBareAtAccept` — ADR 0028's decision 7
  carries a dated amendment note, `internal/tui/doc.go` states the argument-taking carve-out and
  now calls `/confine` one of the two argument-taking verbs with a grammar of its own
  (`/color-scheme` is the other), `minilang_test.go`'s `/confine` accept comment names the
  picker-pair exception, and the README says which commands complete instead of running.

- **The emergency fold's user bridge is now a plain file rather than a Go string literal.** The
  message appended after an overflow fold — the user turn that closes the folded conversation's
  turn structure and tells the model, in-band, that the history it can see is a summary — was a
  string constant in `internal/agent/compact.go`. It now lives in
  `internal/agent/prompts/overflow-bridge.txt`, the engine package's first embedded prompt asset,
  compiled in with `//go:embed`: still one binary, still nothing read from disk at runtime and
  nothing user-overridable — only the wording moves to where it can be edited as prose. The loader
  strips the single trailing newline the asset ends in (normalising a CRLF checkout first, as
  `internal/context` already does), so the bridge text is byte-identical to the constant it
  replaced and the folded request goes out exactly as before.

- **Every Mechanism's prompt text is now editable prose, not a Go string literal.** The nine
  remaining hard-coded prompt literals in `internal/mechanisms` — the three cot nudges, the two
  decompose directives, the two library behavioural notes plus that Mechanism's injection-block
  header, and the empty-response completion-check nudge — moved into `internal/mechanisms/prompts/`
  as `.txt` assets, joining the tool-loop fragments that were already there and loading through the
  same `go:embed` + `mustPrompt` path. Nothing a model sees changed: every asset is a byte-for-byte
  move of the wording the sim's A/B measured, still compiled into the binary, still never read from
  disk and never user-overridable, with the `@pin` provenance comments, the sentence-joining spaces
  and the trailing newlines staying in Go. The idempotency markers also stay in Go, so the "a
  directive contains its own marker" coupling that keeps a repeat inject a no-op now spans two files
  — a new test pins each marker as a substring of its asset, failing a re-worded asset that would
  otherwise make the directive inject twice.

- **The `internal/domain` package map names every marker interface again**: the `tools.go`
  sentence in `doc.go` enumerated `ReadOnlyTool`, `SubprocessTool`, `ExternalEffectTool`,
  `ReadSourceTool` and `PromptTool` but omitted `ApprovalScoper`, the marker a tool implements to
  disclose what a call reaches beyond what its arguments name. It is now listed with the qualifier
  that distinguishes it from the rest — it is read on the approval path, not by the dispatch
  disposition. Comment-only; no behaviour changes.

- **The writer-disclosure tests no longer depend on an unsymlinked temp dir**: the bare-sentence
  assertions — the ones pinning that `write_file`, `edit_existing_file`, `find_replace`,
  `copy_file`, `move_file`, `delete_file` and `read_file` say nothing extra when a path resolves to
  itself — built their workspace root from a raw `t.TempDir()`. On a host whose temp dir is reached
  through a symlink (macOS `/tmp`) every path under such a root resolves somewhere else than the
  root the test wrote down, so the tools appended the `→ resolves to …` note those assertions exist
  to prove absent, and the suite failed on that box alone. A new `tempRoot(t)` helper beside
  `realPath` in `internal/tools/path_safety_test.go` resolves the root by the same rule the tools
  resolve a path, and the affected roots take it. Test-only — no shipped behaviour changes.

- **Pinned that the approval pane, the call card, and the result sentence name the SAME resolved
  path for every write key**: `TestResolvedPathRidesTheCallAndTheApproval` drove only `write_file`
  (the `path` key that is written directly), so nothing held the `destination`-keyed writers to
  the same agreement. A sibling table test now drives `copy_file` and `move_file` (resolved
  `destination`) and `delete_file` (resolved `path`) through a gated Ask-Before call over a
  workspace whose `docs/notes.md` is a symlink to `store/notes.md`, and asserts
  `domain.ToolCallEvent.ResolvedPath`, `domain.ApprovalRequest.ResolvedPath` and the tool's own
  ` → resolves to …` success sentence all carry the same `filepath.EvalSymlinks`-resolved target.
  Two independent statements of one fact could drift apart silently; they cannot now. Test-only;
  no behaviour moves.

- **`ScopeEnv` now asks `isPathName` whether a key is PATH**: the `add` closure in
  `internal/platform/host.go` carried its own copy of the fold rule — `fold == "PATH"`, with `fold`
  upper-cased on Windows — which said the same thing as `hostRules.isPathName`, the rule
  `ScopeInheritedEnv` already routes through. Two spellings of one rule agree until one of them is
  edited, so the allowlist path now calls `isPathName(key)` and the platform has a single place
  where "this key is the PATH a child resolves its programs through" is decided. The `fold`
  variable stays: the `seen` map still needs it to keep Windows from emitting `PATH` and `Path` as
  two variables. Refactor only; no behaviour moves, and the existing suite pins both sides
  unmodified.

- README documents the three per-entry API key sources: the `servers:` schema example and
  the optional-key enumeration now name `api-key-cmd` and `api-key-env` alongside
  `api-key`, and "The upstream API key" gains a paragraph on the exactly-one-source rule,
  the no-shell command whose trimmed stdout is the key, and the variable-name form.

- The capability battery's two prompts are now plain files, and the rule that governs them is
  enforced rather than stated. `batterySystemPrompt` and `candidatePrompt` moved out of
  `internal/probe/battery.go`'s const block into `internal/probe/prompts/system-prompt.txt` and
  `prompts/candidate-prompt.txt`, compiled in with `//go:embed` — still one binary, still nothing
  read from disk at runtime and nothing user-overridable, only the wording moves to where it can be
  edited as prose. The loader strips the single trailing newline each asset ends in (normalising a
  CRLF checkout first, as the embedded block art already does), so both strings are byte-identical
  to the constants they replace and every probe goes out exactly as before. The fingerprint markers
  (`chainSecret`, the harmony and `<think>` tokens) are not prompt text and stay in code beside the
  observation that reads them. The invariant the old block comment could only assert — every byte
  here is folded into the probe fingerprint, so re-wording one requires a `BatteryVersion` bump —
  now travels with the text: it is stated on the embed var and in a non-embedded
  `prompts/README.md`, and a new pin test holds each asset to its exact wording, so an edit that
  forgets the bump fails the suite instead of silently making new records incomparable with stored
  ones.

- The tool-loop interceptor's directive text now lives in plain files: the eight fixed sentence
  fragments of `tool_loop_interceptor`'s loop-breaking correction moved out of Go string literals
  into `internal/mechanisms/prompts/*.txt`, embedded into the binary with `go:embed`. The wording
  is editable as prose; the branching, the `%s` substitution and the joining spaces stay in code,
  and every branch's directive text is byte-identical to before.

- `internal/title`: the naming call's three prompt texts (the system instruction, the closing
  user instruction and the request-window header) are now plain files under
  `internal/title/prompts/`, embedded with `//go:embed` — the wording is editable prose instead of
  Go string literals, still compiled into the single binary and never read from disk at runtime.

- **The compaction prompts are now plain files rather than Go string literals.** The summarizer's
  system prompt, the sentence that closes the summary call's user message, and the label on the
  folded summary were string constants in `internal/context/compact.go`, where prose that gets
  reworded is hardest to read and hardest to diff. They now live in `internal/context/prompts/` as
  three `.txt` assets compiled in with `//go:embed`: still one binary, still nothing read from disk
  at runtime and nothing user-overridable — only the wording moves to where it can be edited as
  prose. The loader strips the single trailing newline each asset ends in (normalising a CRLF
  checkout first, as the embedded block art already does), so every string is byte-identical to the
  constant it replaced and the summary call goes out exactly as before; a test pins that contract
  over every embedded asset, and the existing test asserting the request's system message *is* the
  summary instruction passes unmodified.

- **`resolvedTargetNote`'s doc comment covers its reader caller too**: the comment at
  `internal/tools/workspace_scoped.go` still described the ` → resolves to <path>` tail as "the tail
  a write tool appends", which stopped being the whole truth when `read_file` began appending the
  same tail. It now names both callers — a write tool says where the write landed, `read_file` says
  where the bytes came from — and records why the reader's case is the one that most needs saying
  out loud: a read FOLLOWS the link rather than replacing its final name, so this tail is the only
  place that redirection is disclosed. The marker note gains the matching caveat that a reader does
  not carry the marker at all. Comment-only; no behaviour moves.

- **Pinned the dangerous-action guard's two-class branch**: a tool declaring BOTH a delegation
  prompt key (`domain.PromptTool`) and a read-source key (`domain.ReadSourceTool`) has both classes
  of value dropped from the write-shaped view, and `TestWriteShapedViewDropsPromptAndSourceKeysTogether`
  now holds that. No shipped tool declares both today — the branch was correct only by inspection —
  and the test asserts the floor in the same breath: the same guarded literal under an ordinary
  argument still hard-refuses. Test-only; no behaviour moves.

- **Three code comments state the shipped behaviour again**: `internal/domain/events.go` recorded
  the wire-silent invariant as "nothing is added to a tool's arguments or its result", which the
  resolves-to disclosure notes falsified — the arguments half is intact, and the comment now names
  the result-string note (` → resolves to <path>`, written by the tool, not the engine) as the
  deliberate exception and says why it travels in the result rather than only on the Event.
  `internal/present/server.go` and `internal/present/server_test.go` both listed `.xhtml` among the
  active-content formats rung 2 shows; `.xhtml` left rung 1's allow-list on 2026-08-12 without ever
  entering rung 2's, so both comments now name only formats a rung actually serves. No behaviour
  moves.

- **Documented the dangerous-action guard's prompt-key exemption**: `internal/security/doc.go` and
  the confinement execution contract now record that a tool-declared delegation prompt
  (`domain.PromptTool`) is outside every rule's sight, with the child-guarded-at-the-action-site
  rationale.

- **`TODO.md` is merged into `ISSUES.md`, and this changelog is now the closed trail.** The two
  files had converged on one job from two directions — a defect list and a parked-work register —
  so they are now one file with the distinction kept as sections: `ISSUES.md` carries **Open
  defects** (verified, unfixed problems) and **Parked / deferred work** (the former `TODO.md`
  entries, deferred by decision), and `TODO.md` is gone. Every item earned its place by
  re-verification against the working tree (2026-08-13): 17 defects and all 22 parked entries
  survive, with drifted file:line citations, moved helper locations and five plan paths that had
  since been archived corrected in the process; the two topic overlaps (the `SetSampling`
  wholesale-replace defect ↔ the parity entry's request-side knobs; the approved out-of-workspace
  write ↔ the tool × mode matrix) are cross-referenced, not folded, so a live defect never hides
  inside a parked design. Two entries closed on verification, both already recorded in this
  section's own bullets: the unbounded-reply defect (shipped as the output-cap work, ADR 0046) and
  the streaming-preview whole-buffer re-render (fixed via `previewTail`). The convention changes
  with the merge: a resolved item now leaves `ISSUES.md` entirely and is recorded HERE — no
  closed-entries section lives in the register. `TODO.md`'s existing one-line trail therefore
  lands in this bullet, standing constraints intact:
  - **Naming Sub-Agents** — shipped 2026-08-09 (`docs/plans/archived/2026-08-09 - 00 -
    subagent-naming-and-newline-key-plan.md`). Standing: the name is display identity only, never
    privilege (ADR 0005); it stays OPTIONAL with the task's first line as fallback; the rail
    carries no label of its own.
  - **VS Code agent-CLI allowlist** — moot 2026-08-03 (`docs/plans/archived/2026-08-03 - 01 -
    session-name-on-the-top-rule-plan.md`): apogee sets no terminal title; the session name rides
    the `▔` top rule. Standing: the terminal's title bar and tab are a closed route — do not
    re-file an escape sequence, a settings recipe or an upstream PR for them.
  - **Mid-string token completion** — shipped 2026-07-28 (ADR 0027 decision 5). Standing: the
    caret walk is `seatCaret` (a `CursorEnd`-then-`CursorDown` step over LOGICAL rows, now in
    `internal/tui/lineeditor.go`) — bubbles' phantom trailing sub-line makes a naive `CursorDown`
    loop stall or spin, so do not "simplify" it back.
  - **Read/list tool-name detection** — closed 2026-07-19. Standing: `syntax`/`autofix` keep the
    narrower sim-only `isWriteTool` set; search/exec tool spellings stay out of scope.
  - **General system-prompt / template story** — closed 2026-07-26 (ADR 0023); the
    marker-phrase-suppression interaction and the host-override residual live on as parked
    entries in `ISSUES.md`.
  - **Auto-mode confinement degradation is silent** — closed 2026-07-21 (ADR 0012 amendment;
    ADR 0021). Standing: never loosen `resolveLadderAuto` — the user's decision must stay
    reachable, the tool never decides. The stderr-only startup-notice residue is a parked entry
    in `ISSUES.md`.
  - **Validated-set twin ladders** — done 2026-07-22 (`cmd/apogee/validatedsets.go`; ADR 0016 §5,
    ADR 0021 §4).
  - **Windows disk-label walk kept full-tree + progress notice** — shipped 2026-07-23. Standing:
    pruning the walk stays rejected — it would dissolve the fence for excluded trees.
  - **`internal/platform` Windows confinement file split** — closed 2026-07-25. Standing:
    `confiner_windows_test.go`, `host.go` and `winlabel/journal_test.go` exceed the ~400-line
    guideline BY DECISION; if `host.go` is ever picked up it wants its own entry.

- **"Always allow this session" now remembers the call you read, not the tool it used.** The
  allow-for-session memory was keyed on the tool NAME alone, so one allow on `terminal` pre-cleared
  every later shell command for the rest of the Session — and since that memory belongs to the whole
  agent tree, an allow granted inside a sub-agent cleared the prompt for its parent and its siblings
  too. An approved gate also runs with no confinement box, so the second command was both unread and
  unfenced. The key now carries a digest of the call's arguments beside the tool name, so the answer
  authorises the call that was on the screen. The arguments are digested in a canonical spelling —
  keys sorted, a duplicated key collapsed to the LAST occurrence, which is the value stdlib JSON
  hands the executor and the value the approval pane shows — so a reordered or duplicated key cannot
  mint a second identity for one executed call, and each scalar keeps its wire bytes so nothing is
  rounded or substituted on the way into the key. Arguments that do not decode produce no key at
  all, which means the call is asked about every time. MCP is deliberately untouched and keeps
  ADR 0012's server grain: approving one of a server's tools still clears its siblings. The lifetime
  is untouched too — an allow still survives `/clear` and lasts the Session. The cost is real and
  you will notice it: allowing `npm test` no longer clears `npm run build`.

- **The dangerous-action floor now covers the two control planes a coding host hands the model.**
  The floor named `~/.ssh`, the AWS credentials, `.netrc` and `.npmrc`, but neither the
  repository's own `.git/` nor apogee's `~/.apogee` — an asymmetry rather than a judgement, and
  the more useful pair of the four for anyone whose bytes reach the model. A write to
  `.git/hooks/pre-commit` or `.git/config` is delayed code execution on the operator's machine:
  the next ordinary `git` command runs the hook, or the `core.hooksPath`, filter and textconv
  drivers a config names, outside any confinement box — the same persistence shape as a shell rc
  file, and reachable inside the workspace fence, where `confine-to-workspace` has nothing to say
  about it. A write under `~/.apogee` reaches the global `config.yaml`, which is the one source a
  dangerous-rule REMOVAL is honoured from, so a single write there can dissolve this floor for
  every later run, plus the skill library and the session records. Both are now tier 1: refused in
  every mode, with no per-call override. The rules stop at the control plane and nowhere further —
  `.gitignore`, `.gitattributes`, `.github/`, `.git/info/exclude`, a clone URL ending in `.git`
  and a project's own `<workspace>/.apogee/skills` are all ordinary work and none of them match.
  Nothing about the merge semantics moves, so a global config can still remove either rule on a
  machine where it gets in the way, and a project config still cannot.

- **apogee resolves the OS opener itself now, and refuses one that lives inside your workspace.**
  The presentation ladder's first rung used to hand the bare names `open`, `xdg-open` and `cmd` to
  the exec package, which looked them up against apogee's own `PATH` at the moment of launch — the
  last bare-name launch anywhere in the code. `present_document` is read-only, so it auto-runs in
  every mode including Plan, with no approval and no confinement box behind it: a `PATH` entry
  inside the workspace was therefore a program the model could choose by writing a file. Each of
  the three names is now resolved to an absolute path *before* anything starts, and one that
  resolves inside the workspace root is refused with a message naming the resolved file and the
  fence that refused it — the same rule the git tools, `python_exec`, `run_tests` and `go vet`
  already apply to their own programs. A machine that simply has no opener on `PATH` is unchanged:
  that is still a normal outcome, and the document is presented on the transcript rung as before.
  A `present.command` you configured yourself is untouched — it names one application, and it is
  your own configuration, with the same standing as your shell.

- **An HTML page or an SVG is no longer handed to your browser by the OS opener.**
  `present_document` is read-only, so it runs in every mode including Plan, and `present.auto-open`
  is on by default — which meant a `.html` file that need only have ARRIVED in a cloned repo could
  be handed to your default browser with no approval anywhere. A browser is a runtime rather than a
  viewer: script in that page reaches loopback, your RFC1918 network and `169.254.169.254` from the
  browser's own network position, past none of the URL filtering apogee's own network tools go
  through. `.html`, `.htm`, `.xhtml` and `.svg` therefore leave the opener's extension allow-list,
  by the same rule that already excluded the macro-bearing office formats — and further along it,
  since a macro needs one *Enable Content* click and a `<script>` needs none. **What changes for
  you:** on a local session, `present_document report.html` degrades to the transcript rung — the
  path is still presented, and you open it yourself if that is what you meant — instead of
  launching a browser. Everything else in the set is untouched: markdown, text, data files,
  documents, images and PDFs still open in the application that knows them, and a `present.command`
  you configured yourself is unbounded as before, because it names one application and the
  extension selects nothing.

- **A document served to your browser now carries a restrictive Content-Security-Policy.** On a
  remote session the ladder's second rung serves the document over loopback-or-LAN HTTP instead of
  opening it, and that rung keeps the active formats above — because a served response can carry a
  policy and a `file://` launch cannot. Every served document now answers with
  `default-src 'none'`, which refuses script, `fetch`, XHR and every subresource load, plus a bare
  `sandbox` (CSP has no directive for `<meta http-equiv="refresh">`, and the bare form withholds
  the top-level navigation that would answer it), `form-action`/`base-uri`/`frame-ancestors` set to
  `'none'`, and `X-Content-Type-Options: nosniff` so the extension keeps deciding what a document
  is. `img-src` and `style-src` stay open enough that a self-contained report still renders with its
  own images and its own inline stylesheet.

- **A `python_exec` snippet imports the standard library, not the repo.** A program fed to CPython
  on standard input runs with the working directory — for this tool, the workspace root — at the
  FRONT of `sys.path`, so a repo-root `json.py`, `socket.py` or `subprocess.py` owned the matching
  `import` in a snippet whose approved `code` showed nothing of the sort, and confinement was no
  answer (the box leaves read and exec open, so the Auto path ran the repo's `json.py` too). The
  interpreter now runs with `PYTHONSAFEPATH=1`, which drops that entry. Because the variable landed
  in 3.11 and every older interpreter ignores it **silently** — `python3` on a stock macOS is
  frequently 3.9 — the tool measures the interpreter's version first and passes `-I` (isolated
  mode) instead when it is older, so the fix is not a no-op on the hosts that need it most. Nothing
  is injected into your code and `PYTHONPATH` is never set. **What changes for you:** a snippet
  that imports a project module now needs to say so — `import sys; sys.path.append('.')` — which
  is the point, because that line is then visible in the `code` you approve. The tool description
  says so, and `sys.path.append` behaves identically under either mechanism.

- **`terminal` and `python_exec` no longer hand your inference-server key to the subprocess.**
  `APOGEE_API_KEY` is now dropped from the environment those two tools launch with; everything else
  — `PATH`, `HOME`, your own variables — is inherited exactly as before, because these tools exist
  to run your ordinary developer tooling. A configured server key was never at risk: keys in
  `config.yaml` are file-only and never reached an environment in the first place.

- **A program apogee launches may no longer come from a directory the model can write.** Every
  tool that resolves an executable on `PATH` — the five git tools, `python_exec`, `run_tests` and
  `diagnostics`' `go vet` half, plus the autofix Mechanism's formatter probe — now refuses one
  that resolves inside the workspace or any configured extra writable path. The chain it closes is
  short: a confined call is *allowed* to write inside its box, so it can plant (or overwrite,
  keeping the 0755 bit) an executable there, and a later call that resolves its program on `PATH`
  would run those bytes — possibly unconfined, outside the box the first call was fenced by. The
  refusal names the **resolved** path and says which fence refused it, rather than reporting the
  program as missing. **The cost, accepted deliberately:** an activated in-repo virtualenv
  (`<repo>/.venv/bin/python3`) and a `node_modules/.bin` entry ahead of the system entries on
  `PATH` are exactly this shape, so `python_exec` and `run_tests` refuse them; point `PATH` at an
  interpreter or runner outside the workspace, or run it through `terminal`. There is no config
  key to switch the rule off — one would be a new operator-armed footgun. The git subprocess's
  scrubbed environment now scopes its `PATH` value the same way: workspace-resident entries (and
  every relative entry, which names a directory relative to the child's own working directory) are
  dropped, so the programs *git* resolves — hooks, credential helpers, pagers, diff drivers —
  cannot come out of the workspace either.

- **One space now separates a tool row from its ▶/▼ fold indicator.** The field reserved at a row's
  right edge was three spaces wide, which read as a gap rather than as the indicator belonging to the
  row it marks; it is one space everywhere now — `exit 0 · +10 more lines ▶` — for every tool row,
  grouped member and sub-agent head alike. The two columns the field gives up go back to the row: the
  dotted leader runs two cells further, and at narrow widths a target that had to be dropped or
  clipped keeps a little more of itself.

- **`read_file` can now locate a substring while it reads.** An optional `locate` parameter reports
  the absolute 1-based line numbers the substring falls on, as one `Located "…" on lines: 5, 9` line
  directly under the read's header — `on no lines` when it matches nothing. The scan always covers
  the **whole file**, even when `start_line`/`end_line`/`max_lines` narrow the content that comes
  back, so a match outside the returned span is still reported at its true line number: the parameter
  answers "where is this?", not "is it in the part you showed me?". A read that asks for no locate
  renders byte-identical to before. The tool's description advertises the affordance by name, because
  small models discover capabilities by name rather than by reading a parameter schema. The TUI says
  it on the row too — the target picks up a `· locate "…"` qualifier beside any range it already
  shows (`path:12–80 · locate "…"`) — and the located lines ride the tool summary as data rather than
  as a sentence a host has to parse back out.

- **The model profile is configured per model now: `model-profiles:` replaces `model-profile:`.** The
  block that said how a model speaks the wire — its tool-call format and its inline thinking channel —
  was one GLOBAL setting, so a machine that runs several models had to be re-edited on every switch. It
  is a map now, keyed by a **pattern the model name contains** (a case-insensitive substring), and the
  block under each key is exactly what the old one was: `model-profiles: {"minimax-m3": {thinking:
  {style: delimited, start: "<mm:think>", end: "</mm:think>"}}}`. A matching entry supplies the WHOLE
  profile, both axes, and the longest matching pattern wins. The retired global key is a **loud startup
  error** rather than a silently unread one (no back-compat layer, pre-production): the message names
  the line, echoes your own block back nested under a pattern placeholder, and says what to paste.
  `apogee probe model` suggests its findings in the new spelling too, keyed by the model it probed.

- **The shipped `config.yaml` documents `model-profiles:` now.** The template seeded into `~/.apogee`
  on first run passed the key over in silence: the built-in shape table means a known family needs no
  entry, but a reader whose model speaks a dialect apogee does not know — or whose built-in match is
  the wrong one — had nothing in the file to reach for. It is documented like every other key now: a
  commented example, the substring-pattern rule and how ties resolve, the three built-in patterns and
  the notice they print, both axes with the values they take, and the retired global `model-profile:`
  named as retired rather than left to be discovered at startup. The `servers:` entry's `model` key
  points at it, since the name a run binds is what the profile is matched on. An existing
  `~/.apogee/config.yaml` is never rewritten, so this reaches a fresh install. `apogee probe model
  --help` picks up the same spelling.

- **The key-source writer has its own file.** `internal/config/configwrite.go` had grown past 1,800
  lines carrying every writer in the package, so the half that rewrites the key source of a single
  `servers:` entry — the `api-key:` → `api-key-cmd:` swap a consented key migration persists, and
  the per-entry `plaintext-key-ok:` marker that declines it (ADR 0047) — moves verbatim into
  `internal/config/configwrite_keysource.go`, carrying its own section banner as the file's header.
  A pure move: no exported name changes, no logic changes, the ADR 0035 splice contract untouched,
  and `doc.go`'s file map gains the new file so the docmap test keeps the navigation aid honest.

- Refactor: `internal/config/configwrite.go`'s scalar-setting writer moved verbatim into a new
  `internal/config/configwrite_scalar.go` (ADR 0043 split-by-concern), carrying its section banner
  as the file header and the shared splice helpers embedded in that span. Behaviour-preserving:
  no exported name or logic changed.

- **The splice machinery every config writer shares has its own file.** The line-and-node plumbing
  the acknowledgement writer, the scalar `/settings` writer, the key-source writer, the per-entry
  setting writer and the legacy fold all reach for — read the file, parse it for positions, find a
  key in the node tree, cut and rejoin the text, verify the result against the original, replace the
  file atomically — moves verbatim out of `internal/config/configwrite.go` and
  `internal/config/configwrite_scalar.go` into a new `internal/config/configsplice.go`, whose header
  states the ADR 0035 contract those writers now inherit rather than each restate. A pure move: no
  exported name changed, no logic changed, `cmd/apogee`'s callers compile untouched, and `doc.go`'s
  file map gains a line per writer file so the navigation aid keeps up.

### Removed

- **`open_file` is gone, merged into `read_file`.** It was the read-and-locate twin of a tool that
  already read files, and the roster is itself a discovery surface: a second name for one job costs
  more than it buys. Everything it did survives as `read_file`'s `locate` parameter above, and the
  `domain.OpenedFile` summary variant went with it — `ReadSpan` carries the locate facts now. Nothing
  else about `read_file` moved: same label, same stat, same output when no locate is asked for.
  `"open_file"` stays in the Mechanisms' read-spelling family as a retired name models may still
  emit, so a model reaching for it is still recognised as reading. A `tools.disabled` entry still
  naming it starts fine, disables nothing, and draws the standard unknown-name startup notice.

### Fixed

- **The entry-splice refusal names what the edit failed to place.** `verifiedEntrySplice`
  (`internal/config/configwrite_keysource.go`) is the gate every `servers:`-entry write passes
  before it reaches the disk, and its "did not land where a reader would look" refusal spelled the
  thing it had been placing as "the key source" — true of the two writers it was written for (the
  `api-key-cmd:` swap and the `plaintext-key-ok:` acknowledgement) and wrong of the per-entry
  setting writer that later joined them, where a splice that missed its entry while recording a
  picked `model:` or a committed `launch-profile:` told the reader a key source had gone astray.
  The refusal now takes the noun from its caller: the key-source pair pass "the key source", so
  their message is unchanged, and the setting writer passes the noun its allow-list row spells —
  "the model" or "the launch profile" — one field beside the key so a future writable key cannot
  be added without saying what to call it. A direct unit test drives the gate with a splice that
  did not land and pins both nouns.

- **A carriage return in a displayed FIELD is folded like a newline.** `flattenField` — the
  display seam that folds an argument key, a skill name or summary, a pop-up title or a gate's
  reason onto the one row the pane drew for it — guarded on `\n` and `\t` only, while its input-side
  sibling `flattenLine` has folded `\r` all along. `stripEscapes` drops the carriage return, but the
  callers that hand the seam a model's own bytes unstripped (`skills.go`, the pop-up title,
  `toolpresent.go`'s argument label) do not, and a terminal reading one returns the cursor to
  column 0 so the rest of the field overwrites the row already drawn. The fold now covers all three,
  one rune for one space — a `\r\n` becomes two spaces — so the rune count a later clip counts
  (`clipRunes`) is still what the row will hold.

- **The register's `auto-title` entry is retracted as stale.** `ISSUES.md` held that committing
  `auto-title` in `/settings` writes the file but cannot reach the running session, because the key
  has no case in the binary's `applySettingFor` dispatcher (`cmd/apogee/wire_settings.go`). It is a
  renderer-owned key: `settingsApplyLive` (`internal/tui/settings.go`) tries `settingsApplyLocal`
  first, whose `settingKeyAutoTitle` case sets `Options.AutoTitle` on the Model itself, and the
  automatic-naming gate reads that field per prompt (`internal/tui/autotitle.go`) — so the binary
  dispatcher is never consulted for this key and the missing case is by design, exactly as for the
  other renderer-owned keys. Live apply shipped 2026-08-06 with the live-apply dispatcher
  (`056583d`) and is covered by `TestSettingsPaneRendererOwnedKeysApplyWithoutTheSeam`. Nothing in
  the code changed; the entry is simply gone from the register.

- **The pop-up-fold comment stops citing an ISSUES entry that is not there.** The block above
  `TestWrappedSurfacesBreakInThePaintersMeasure` (`internal/tui/render_test.go`) ended by sending
  the reader to "ISSUES.md with the rest of the ADR 0030 residue" for the pop-up pane's fold, but
  that register entry holds only `hangingPrefixes` and never covered the pop-up. The sentence now
  states the fact on its own — the fold is the lipgloss pane's own deliberate behaviour, not a
  residue tracked anywhere — so the comment stops pointing at a page that would not answer it.
  Everything the block says about why the pop-up body is deliberately absent from the test is
  unchanged.

- **The prefix-once comments stop counting `MechanismRegistry.Add`'s rejection gates.**
  `internal/agent/construct.go` and `internal/agent/enable_mechanisms_test.go` both said "Add's
  three rejections already carry the `apogee: ` prefix" while `Add` now has four gates (empty ID,
  reserved sentinel, duplicate ID, no hook interface). Both now say "Add's rejections" — no count,
  so the next gate cannot re-stale them. The substantive claim is unchanged: every rejection
  arrives prefixed, so the enable path appends its context rather than wrapping it in a second
  prefix.

- **The prompt box's width mirrors now weigh a tab the way the textarea itself does.**
  `wrapRowStarts` — the mirror of the bubbles textarea's own soft-wrap, and through it
  `inputContentRows`, the count that sizes the prompt box — measured the runes exactly as handed
  over, while the widget rewrites every `\t` as four spaces before it wraps anything
  (`runeutil.NewSanitizer`'s default, applied on every write path). A tab therefore weighed one
  space-like column to the mirrors and four to the widget, so a tab-bearing line broke in different
  places on the two sides and the box could size itself to a row count the widget never drew — the
  last divergence the width-authority work left standing. Both mirrors now expand tabs the same way
  first, in the one place the wrap is derived, so the box's height and the rows the accent overlay
  paints on stay off one ruler. The expansion is kept apart from the transcript side's `expandTabs`
  on purpose, though the two spell the same four spaces today: that one follows lipgloss because the
  PAINTER applies it, this one follows the widget's sanitizer because the WIDGET applied it, and a
  mirror answers to its widget alone (ADR 0030 §6). No draft can reach this with a tab in it today —
  the textarea sanitizes tabs out of everything written into it — so nothing on screen moves; what
  changes is that the mirrors are now right for any caller that hands them text the widget has not
  already cleaned, and that both widget-oracle suites now carry tab-bearing lines (a leading tab, a
  tab inside a word, one at the wrap column, a line of nothing but tabs) plus tabs in the generated
  prompt-draft sweep, so a regression fails against a real textarea rather than sliding quietly.

- **A hook that sets one sampling field no longer erases the other — most consequentially the
  reply ceiling.** `SamplingParams` has said since it was written that "a nil field leaves the
  loop's value untouched", and the loop leans on exactly that: it stamps the output cap into the
  Request before any pre-request hook runs, precisely so the cap is the loop's own value rather
  than a projection-time constant and a hook can override it (ADR 0046). But `SetSampling`
  replaced the whole struct, so the promise was false in the one direction that matters: a
  Mechanism setting only `Temperature` handed back a `SamplingParams` whose `MaxTokens` was nil,
  and the request went to the Upstream with no ceiling at all — a silent un-capping, from a hook
  that never mentioned the cap. `SetSampling` now merges field-wise: a non-nil field overwrites,
  a nil field leaves the current value alone. A hook that sets `MaxTokens` still overrides the
  loop's stamp exactly as before, `revision` still bumps on every call (it is the acted-fire
  probe, counting mutator calls rather than fields written), and there is deliberately no
  clearing surface — a hook cannot reset a field back to nil, which would be new API.

- **A tab in a one-row field is now folded like a newline, so the width a row is laid out at is the
  width it draws at.** `stripEscapes` deliberately keeps `\n` and `\t`, because the wrapped bodies
  that are its biggest callers are railed by both — but a FIELD is a name rather than a body, and
  `flattenField` folded only the newline. Every one-row seam routes through it: the popup title, the
  approval pane's Reason / Fix / Scope and Sub-agent lines, argument labels, the resolved-path note,
  and the autocomplete and skill rows. A tab reaching one of those measures as a single cell to
  lipgloss while the terminal expands it to the next tab stop, so the row is laid out at one width
  and drawn at another: the label beside it slides, and a clip that trusted the measured width cuts
  in the wrong place — a model's own bytes deciding where a row's structure appears, which is the
  same family as the newline forgery the fold already prevented. `flattenField` now folds `\t` to a
  single space alongside `\n`, one rune for one rune as before. `stripEscapes` is unchanged: body
  text keeps its tabs.

- **The plaintext-key notice now states the reason that is actually true for the run it came from.**
  The notice hard-coded "this machine has no secret store apogee can move it into" — true in the
  session, where it is printed only after a live probe came back empty, but false in a headless run,
  which never probes at all and prints the notice for any plaintext `api-key:` (ADR 0047 — the
  migration offer is a consented edit, so an unattended run only reports). A headless user with a
  perfectly good Keychain or keyring was being sent to look for a store that was there all along.
  `plaintextKeyNotice` now takes the reason from its caller: the session passes the probe-failed
  sentence unchanged, and headless says "headless runs never prompt, so apogee cannot offer to move
  it into a secret store". The rest of the notice — the `api-key-env:` / `api-key-cmd:`
  alternatives, `chmod 600`, and `plaintext-key-ok: true` — is unchanged on both paths.

- **A store tool that echoes the secret back can no longer publish it through apogee's own error
  message.** Migration hands the key to `security` or `secret-tool` on STDIN, and a tool that cannot
  use its input tends to quote that input: `security -i` reports on the command LINE it read — the
  `add-generic-password … -w <key>` line the secret travels in — and a `secret-tool` wrapper logging
  what it received would echo the raw key the same way. Both of `Store.Write`'s failures fold that
  captured stderr into their error, and an error goes to the terminal, the session log, and the bug
  report a user pastes it into: exactly the readable place the migration exists to get the key out
  of. The captured text is now redacted before either message is built — every occurrence of the key
  becomes `[redacted]`, in the raw spelling and in the quoted spelling that went over the wire, since
  `security -i` parses the line apogee wrote and a key needing quotes appears there escaped. The rest
  of the tool's words are kept unchanged, because the tool's own sentence is almost always the part
  that names the fix.

- **The `rm -rf` hard-refuse rules now spell the macOS home in their own system-path list.**
  `rm-rf-root-home-system` and `rm-fr-root-home-system` (`internal/security/rules.go`) listed
  `home` but not `users`, so the enumeration that documents what a "root, home, or system path"
  means was missing the spelling the desktop persona actually uses — the same `/users/` the
  `write-ssh-keys` and `write-credential-persistence` rules already carry. Both patterns now list
  it. Behaviour is unchanged today: the alternation's leading bare `/` branch already matched every
  absolute target, so `rm -rf /Users/alice` and `rm -fr /Users/alice` hard-refused before this
  change and are now pinned by tests in `TestDangerousActionGuard_Tier1HardRefuse` and
  `TestDefaultDangerousRules_HomeAnchoredRulesMatchTheMacOSHome`. The fix matters the moment that
  bare `/` branch is ever narrowed — at which point the enumeration becomes the live boundary and
  a missing `users` would be a real hole.

- **A copy now also reaches the clipboard of a terminal that ignores OSC 52.** Selecting text with
  the mouse flashed `copied N chars` and handed the selection to `tea.SetClipboard` — the OSC 52
  escape, which is cross-terminal and survives an SSH hop, and which several terminals drop on the
  floor (some until the human turns the feature on). Where it was dropped the confirmation was a
  promise about an empty clipboard. The same text now goes out over a second channel beside it: a
  best-effort write to the host's own clipboard program (`pbcopy`, `xclip`/`xsel`/`wl-copy`,
  `clip.exe`) through `github.com/atotto/clipboard`, already linked into the binary and still
  CGO-free. OSC 52 stays first and unchanged, so nothing regresses over SSH; the system write runs
  off the render path and swallows its error, so a machine with no clipboard program degrades to
  exactly the old behaviour rather than reporting a failure for a copy that may well have landed.
  The confirmation flash stays unconditional. The write sits behind one injectable package-level
  seam, so a test can watch what a copy actually hands over.

- **Accepting the `/model` or `/server` row in the completion menu now opens its picker instead of
  parking the verb in the box.** Both verbs read an argument, and the accept path treated every
  argument-taking verb the same way: it spliced `/model ` into the draft and waited for a token the
  human was never going to type — the picker was reachable only by typing the whole verb and
  pressing ⏎ a second time. Their bare form is a whole verb, though: it opens a chooser and changes
  nothing until that chooser's own accept, exactly like `/settings`. A new `runsBareAtAccept` flag
  on the command registry says so, carried by `/model` and `/server` alone, and the accept path
  completes an argument-taking verb only when the row does not carry it. `/color-scheme`,
  `/confine`, `/rename` and `/schedule` still complete and wait, and the whole-line argument form
  (`/model qwen`) is untouched — an argument token never reaches the accept path. A registry pin
  fails if a third verb ever picks the flag up.

- **`2>/dev/null` works again inside a confined tool call on macOS.** The seatbelt profile is
  deny-default for `file-write*` and re-grants writes only beneath the box's writable roots, so
  the most ordinary line of POSIX shell there is — redirecting output into the null device — died
  with `cannot create /dev/null: Permission denied` in every confined call, and a command that
  merely silenced its own noise failed as if the work itself had failed. The profile now emits one
  unconditional `(allow file-write* (literal "/dev/null"))` clause after the deny, so writes to the
  single device file that swallows them are exempt from the fence whatever the box holds —
  including a box with no writable roots at all. The exemption is backend-level: it is not part of
  `ConfinementBox`, it does not widen the exec fence's writable set, and it is exactly `/dev/null`
  — `/dev` itself stays closed, no other device is granted, and every other out-of-box write is
  denied exactly as before.

- **`2>/dev/null` works again inside a confined tool call.** Both POSIX confinement backends deny
  file writes by default and re-grant them only beneath the box's writable roots, and `/dev/null`
  was quietly taken with them: the most common shell idiom there is died with
  `cannot create /dev/null: Permission denied` in every confined terminal call on a landlock
  kernel. The landlock backend now adds one path-beneath allow rule for the literal `/dev/null`
  after the writable roots. Because that rule's parent is a file rather than a directory it
  carries a file-applicable mask of its own — `WRITE_FILE`, plus `TRUNCATE` from ABI 3 so
  `> /dev/null` still opens — where asking for the directory-only rights the roots use would make
  `landlock_add_rule` answer `EINVAL` and take the whole confinement down with it, not just the
  exemption. The exempt set is exactly one device, and the exemption lives inside the backend:
  `ConfinementBox` is unchanged, the exec fence's writable set is unchanged, and an ordinary
  out-of-box write is still OS-denied — both halves pinned live on a landlock kernel, alongside
  per-ABI unit tests for the new mask.

- **The `/` menu re-scans your skills again after you send a line that ends in a skill token.** The
  menu re-reads the skill folders when it OPENS — that is what makes a skill added since launch show
  up in it — and "opens" is tracked by a flag saying the box currently sits in a `/` region. Sending
  emptied the box but left that flag set, so the next `/` you typed did not read as an opening: the
  menu came back listing the catalog as it stood before the send, and a skill added in between was
  missing from it until you dismissed a menu by hand. Sending now clears the flag along with the
  text, so a menu opening on a freshly emptied box re-scans exactly as the first one of the session
  does. The scan still runs off the render loop and still fires once per opening, not per keystroke.

- **`/skills` no longer freezes the interface while it re-reads your skill folders.** The verb
  re-scans the skill source dirs before it lists them — that is what makes a skill added since
  launch show up — but it ran that walk on the goroutine that paints the screen, so on a large
  library the whole interface sat still until the disk answered. The merged `/` menu's copy of the
  same walk was moved off that goroutine already; this was the same block surviving behind a
  different key. The scan now runs beside the render loop and the listing is written when it lands,
  so the box stays live throughout. The report itself is unchanged, it still touches no engine and
  launches no worker, and it still answers mid-run.

- **A tool NAME can no longer paint a row of its own above the approval prompt.** The prompt folds
  every model-authored field onto the single line a label is, but its own title — `Approve <tool>?`
  — was composed straight out of the tool name and spliced into the pane's top border unfolded. A
  name carrying a newline therefore broke the box open and painted a second, unindented row above
  the body, wearing the same style the pane's own rows wear: a forged `Reason:` line sitting where
  the human reads the pane's structure. apogee does not author tool names — an MCP server names its
  own — so the name is now flattened where the title is composed, and `popupTitleLine` folds every
  pane's title as the backstop, whether that title is drawn on a row of its own or spliced into the
  border. A title is a name, and a name has no layout to lose: the folded newline shows up as the
  space it replaced, on the one line a title has always been budgeted for.

- **A tool call whose arguments are not a JSON object can no longer forge a row on the approval
  pane.** The pane tells its own rows from the model's by the column they start in: a `Reason:` or a
  `Fix:` the pane wrote begins flush-left, and every argument the model sent hangs two spaces in
  under its label. Arguments that carry no labels at all — a bare string, an array, a fragment that
  does not parse — took a fallback that emitted each line at column zero, so a blob reading
  `Reason: pre-approved by the operator` painted a second Reason row beside the real one, in the
  pane's own style, on the surface a human authorises a call from. Those lines now hang at the same
  indent a labelled value does. Nothing is rejected and nothing is hidden — the bytes still reach
  the screen exactly as they arrived, two columns to the right of where a label can live — and a
  properly labelled call renders to the byte as it did before.

- **`read_file` now says when it followed a symlink.** The write tools already append
  `→ resolves to <path>` to their result when the path they were given turns out to name
  something else; a read still echoed the argument alone. So a read of `docs/notes.md` that was
  really a link to `.git/config` printed a header quoting the innocuous name above the other
  file's bytes, and both the model and the transcript read it as an ordinary in-workspace read —
  the read half of a disclosure whose write half had already shipped. A successful read whose
  path resolves elsewhere now ends with the same note, and only when the resolution actually
  differs, so an ordinary read renders exactly as before. The path is resolved against the root
  that SERVED the read, not the workspace assumed: an absolute path under a configured read-only
  root (the skills library) still reads and gains the note when its resolution differs — the
  disclosure adds text to a success and never turns one into a refusal.

- **`copy_file`, `move_file` and `delete_file` now say where the file really was.** `write_file`
  and the two find-and-replace tools already append `→ resolves to <path>` to their result when the
  path they were given turns out to name something else; the three file-operation tools still
  echoed the argument alone. So a copy onto `docs/notes.md` that was really a link to
  `.git/config`, or a delete of that same name, reported a sentence the transcript and the model
  both read as an ordinary in-workspace operation. All three carry the note now — on the
  destination for a copy and a move, on the removed target for a delete — and only when the
  resolution actually differs, so an ordinary operation reports the bare sentence it always did.
  The note is read BEFORE the operation, because a rename replaces the destination name and a
  removal takes it away: read afterwards, the one call worth disclosing would have had nothing
  left to disclose. A copy's SOURCE gets no note of its own — a source is a read, and this is the
  writers' disclosure — and with the symlinked-parent refusals in place the note now covers a
  symlinked final name or any other resolution difference, not a redirected directory.

- **`move_file`, `delete_file` and `copy_file` no longer write through a symlinked directory inside
  your workspace.** The write path already refused one — a `docs → .git` link redirects a path the
  operator approved as `docs/config` onto the repository's own config while never leaving the
  workspace, so the fence itself has nothing to say about it — but only `write_file` and its
  relatives went through that check. A rename, a removal and a copy still followed the link. All
  three refuse it now, on every chain they MUTATE: both ends of a rename (which unlinks one name
  and creates the other), the target of a removal (the unlink lands wherever the chain leads), and
  a copy's destination. A copy's SOURCE is deliberately exempt and still follows in-root links,
  because a source is a read — which is what keeps a skills library assembled from linked source
  dirs readable, and reads disclose where they landed rather than refusing. `move_file`'s
  copy-then-remove fallback, which exists for a rename the filesystem cannot perform across a
  mount point, now treats this refusal as final instead of retrying it: retrying would have
  performed through the link the very write that was just refused, and then failed to remove the
  source — leaving the file in both places and the move half-done.

- **The SSH-key and credential dangerous-action rules now fire on a Mac.** Both rules anchored on
  `~`, `/home/<name>`, `/root` and `$HOME` only, so on the desktop persona — macOS, where a home is
  `/Users/<name>` — a write to `/Users/alice/.ssh/id_rsa` or `/Users/alice/.aws/credentials` matched
  nothing and the hard-refuse floor never fired for the exact paths it exists to protect. Both
  patterns now spell the macOS home alongside the Linux one, the way the newer
  `write-apogee-control-plane` rule already did. Precision is unchanged: an ordinary file in a macOS
  home, `~/.aws/config` (not `credentials`), and a name that merely starts with `.ssh` still pass.

- **`terminal` and `python_exec` no longer let the workspace supply the programs their child
  resolves.** Both tools inherit your environment as it stands — that is what makes them usable for
  real development — and that included a `PATH` naming directories inside the workspace: an
  activated `<repo>/.venv/bin`, a `node_modules/.bin`. Bytes the model was allowed to write could
  therefore become the `git`, the `ssh` or the `curl` that a shell line, a Python snippet, or
  anything either of them spawns resolves for itself — the plant-then-exec chain apogee already
  refuses at its own resolution sites but cannot check inside somebody else's process. The child's
  `PATH` now drops every entry resolving inside the workspace root, plus every entry that is not an
  absolute location (an empty or relative entry names a directory inside the child's own working
  directory, which is the workspace itself). Everything else is inherited exactly as before, only
  apogee's own credentials are still removed, `PYTHONSAFEPATH` still wins over an inherited
  spelling, and python's interpreter-version probe is scoped the same way. The per-entry rule is
  the one the git tools and the Go toolchain already applied through `platform.Host.ScopeEnv`; it
  simply had no shape that fitted a tool inheriting everything, and now it does
  (`platform.Host.ScopeInheritedEnv`). `run_tests` deliberately keeps the unscoped inheritance its
  repo-authored runners need — a workspace-resident `node_modules/.bin/jest` IS the test command
  there.

- **`run_tests` no longer hands the project's test runner apogee's own API key.** It was the one
  execution tool that gave its child the parent environment whole — `terminal` and `python_exec`
  both strip `APOGEE_API_KEY` before they start anything for the model, but the test runner got it,
  and a test suite is repo-authored code running under a threat model where the bytes in the
  workspace are untrusted. The runner now starts in exactly the environment those two tools use: the
  operator's environment, toolchain variables and all (build caches, virtualenv, `NODE_PATH` — an
  allowlist there would break runs that work in your shell), minus apogee's own credentials.

- **A failed command's outcome slot now names its exit code instead of whatever line its output
  opened with.** A `terminal` or `python_exec` call that exited non-zero was summarised as `error:`
  plus the first line of its output — for a failed `ls -la` that read `error: total 20760`, a
  listing header rather than a diagnostic — and the rest of what the command printed was not shown
  in the block at all. Such a call now reads `error: exit 2` in the slot, the red twin of a clean
  run's `exit 0`, with the lines the command printed laid out beneath it exactly as a successful
  run's are. The code is read off the `[exit code N]` marker the tool appends at the END of its
  output, so a command that printed the same phrase itself cannot forge it, and a negative code (a
  run whose leader exited but whose pipe stayed held) is named as it stands. A result carrying no
  marker — a run refused before the process started — keeps the first-line wording, as does every
  other tool: for a tool that fails in prose, that first line IS the error message. What the model
  receives is unchanged.

- **A delegated task is no longer refused for naming a guarded path in its instructions.** The
  dangerous-action guard read a `sub_agent` dispatch's task prose as if the host were about to
  perform it, so a delegation that merely NAMED a fenced literal was hard-refused with no per-call
  override — a `/security-audit` sub-agent briefed to report what "the readable git surfaces —
  `.git/logs/HEAD`, `.git/config`, `.git/packed-refs`" disclose never launched, stopped by the rule
  that fences writes to a repository's git control plane. A tool now declares which of its arguments
  carry instruction prose addressed to another agent (`sub_agent` declares `task` and `name`), and
  that text is outside EVERY rule's sight, not just the write-shaped ones: a prompt describes an
  action, it never performs one. The floor is unmoved where it matters — the exemption is per
  declared key, so the declaring tool's other arguments stay inspected, a shell heredoc writing to
  `~/.ssh` still refuses, an MCP tool with a coincidental `task` argument is inspected in full, and
  the delegated agent's own tool calls are each judged one level down, at the action site, where the
  text is a command rather than a description of one.

- **A Schedule's Firing now runs under the `max-output-tokens:` of the server the session is ON, not
  the one it launched against.** Every other fact a Firing composes itself from is read at fire
  time — the endpoint, the model, the system prompt, the Mechanism set, the fan-out width, the
  context window — but the reply ceiling was not: it was inherited from the Config copied at launch,
  so after a `/server` move a Firing was bounded by a ceiling belonging to a server the session had
  left. A Firing that moved onto a roomier entry was cut off early, and one that moved onto a
  tighter entry could run past the very ceiling an operator set to stop it — unattended, which is
  the case the cap exists for. The ceiling now travels with the rest of the per-Firing resolution,
  and an entry that pins nothing hands the derivation back to the engine's own reply budget rather
  than un-bounding anything: a resolution that says NOTHING about the ceiling leaves the Firing's
  existing bound standing.

- **A `max-output-tokens:` edited on the `servers:` entry the session is ON now applies at once, not
  at the next bind or `/server` move.** The window pin beside it started riding the rebind the moment
  it committed; the reply ceiling could not, because `RebindSpec` carried no ceiling to hand the
  engine — so two pins in the same block of the same file applied at two different times, and a
  ceiling edited to rein in a session that was already running away did nothing until that session
  moved servers. The spec now carries the ceiling and a `servers:` apply rides one rebind for both
  bounds, driven only when the edit actually MOVED the window or the ceiling this session resolves
  to — an edit to another entry, or one restating the number already in force, installs the list and
  rebinds nothing. Dropping the pin is as live as adding one: the ceiling goes straight back to the
  engine's own derivation from the reply budget. A rebind that says NOTHING about the ceiling leaves
  it exactly where it was, so no model change can silently un-bound a reply an operator bounded.

- **A `context-window:` edited on the `servers:` entry the session is ON now applies at once, not at
  the next beat.** The two spellings of one pin applied at two different times: the top-level
  `context-window:` key re-drove the per-model resolution the moment it was committed, while the same
  pin on the bound entry only re-derived a latch that nothing reads until the next rebind — so an
  edited window described the running session from whenever the heartbeat next happened to observe a
  model change, seconds or minutes away, and a window edited to fix a session that was already
  budgeting wrong did nothing until then. A `servers:` apply now rides that same rebind, through the
  one door the top-level key takes. It rides only when the edit actually MOVED the window this
  session resolves to — the entry's pin over the top-level key, compared across the install — so an
  edit to another entry, another key on the same entry, or a pin dropped onto a top-level key that
  already said the same number installs the list and rebinds nothing; a rebind re-resolves every
  per-model binding, resets the token estimator and is refused mid-Exchange, so a gratuitous one
  would cost a running turn for numbers nobody changed. The reply ceiling beside it is unchanged: a
  `max-output-tokens:` pin still reaches the engine through a bind or a `/server` move.

- **A session that STARTS on a pinned entry now budgets against that entry's window.** The
  per-entry `context-window:` pin reached a session that MOVED onto the entry, but not one that was
  bound to it: the pin was never flattened onto the resolved options the way `max-output-tokens:`
  already was, so a startup bind, a pre-bound session's first pick and a headless run all budgeted
  against the top-level `context-window:` key — and, on a cloud endpoint that advertises no window
  at all, against nothing — until the first beat rebound seconds later. The pin now rides the entry
  the bind step takes and is resolved over the top-level key into the Config the engine is built
  from, so it bounds the session's first Turn rather than its second; the same resolved number is
  what the footer's gauge opens on, so the gauge and the Budget cannot describe two different
  servers. The second half of the same hole: a `context-window:` edited in the pane on the entry the
  session is already on now re-resolves in the running session, exactly as `parallel-agents:` on
  that entry does, instead of leaving the pin describing the file as it stood at the last move.
  Precedence is unchanged and still single-sited — the entry's pin outranks the top-level key,
  neither pinned leaves the window to what the heartbeat observes.

- **A `/server` switch now takes the new server's context window and reply ceiling with it.** The
  reply cap followed a `servers:` entry everywhere a session could arrive on one — the startup bind,
  a routed sub-agent spawn — except the one place a session moves: `/server` re-pointed the wire and
  left the engine capping replies from the new server at the old server's number, or at a ceiling
  derived from the old server's window. The per-entry `context-window:` pin had the same hole, and a
  wider one: even when a switch briefly carried a window, the first heartbeat seconds later re-bound
  the observed one over the entry's pin. A move now carries both bounds — the entry's
  `context-window:` (which outranks the top-level key, since it describes the server the session is
  actually on) and its `max-output-tokens:`, as written — and the window pin stays in force for the
  rebinds that follow, so the gauge, the Budget and the ceiling on the wire all describe the same
  server. An entry that pins neither drops the retired server's numbers rather than inheriting them:
  the window falls back to the top-level pin that survives a move, and the cap is derived afresh from
  the window the session is now on. A profile load's follow-the-profile move is unchanged in
  substance — a Launch profile's server is in no `servers:` list, so it pins nothing and the session
  keeps the top-level window.

- **A long streaming reply no longer starves the TUI's event loop.** While a reply streamed,
  expand/collapse clicks stopped responding — measured at 95% CPU and a 0.48 s click round-trip
  after only 180 s of streaming, still climbing, against a flat 0.05–0.07 s once the same reply was
  committed, which is why a restart-plus-resume cured it. The in-flight buffer is the one transcript
  block the paint cache cannot serve — the cache is keyed by entry index and the live buffer is not
  an entry — so the preview re-rendered the WHOLE buffer through the markdown renderer on every
  repaint, and repaints fire per 30 ms token flush plus at 2 Hz while a tool call is open, for the
  whole duration of a delegation. That is O(len(reply)) per repaint and O(N²) over a turn, with
  clicks queued behind those renders rather than dropped — so an even number of impatient clicks
  drained to a visible no-op. The preview now renders only the last 256 raw lines of the buffer:
  it can contribute at most one viewport of rows at the bottom of the frame, so everything above
  that tail was being wrapped, styled and then thrown away. A repaint now costs a screen rather
  than a reply, at any length. The buffer itself still keeps every byte and the committed entry
  still re-renders whole through the cache; the one visible trade is that a markdown construct
  opened above the cut (an unclosed code fence, a list) can render unstyled in the preview's tail
  until the reply commits and heals it.

- **`diagnostics` no longer hands the Go toolchain git's environment, and it now says which
  package it vetted.** The `go vet` half ran with `safeGitEnv` — an allowlist written for a program
  steered by `GIT_*` and a pager. `GOFLAGS`, `GOWORK`, `GOTOOLCHAIN`, `CGO_ENABLED`, `GOPATH` and
  `CC` are all absent from it, so an operator's own Go hardening was stripped and **nothing was put
  back**, while `HOME` passed and the persistent `go env -w` file still applied. Vetting an
  attacker-authored checkout therefore let that checkout have a say in how the toolchain ran: a
  `toolchain` line in its `go.mod` could make `go` download and execute a different toolchain, a
  `go.work` could widen the build, and a `#cgo` directive could reach the host C compiler. The vet
  subprocess now runs on a Go-specific environment that PINS the hardening instead of removing it —
  `GOFLAGS=-mod=readonly`, `GOWORK=off`, `GOTOOLCHAIN=local`, `CGO_ENABLED=0` and `GOENV=off`, over
  an allowlist carrying only what a build cache needs — so the vetted repository no longer chooses
  anything about the process that reads it. This is scope-and-honesty rather than a demonstrated
  execution hole: `go vet` never links, and the toolchain download was the one exec in reach.
  **Deliberate cost:** `GOENV=off` also drops an operator's persisted `GOPROXY`/`GOMODCACHE`, so on
  a cold module cache a vet may fail to resolve dependencies — which degrades to a reported finding,
  never a tool error. The second half is what the call CLAIMED: the tool takes one filename but vets
  the whole package directory around it, which its description never said and its result never
  mentioned. The description now declares it, and every vet result — clean or findings — names the
  package directory it read, beside the file the call asked about.

- **Reading the skill library no longer trips the dangerous-action floor.** The `~/.apogee`
  control-plane rule the hostile-bytes batch added was matched — like every rule — against a
  call's full text with no regard for what the tool DOES, so the first step of every skill run
  died on it: `list_dir` of the skill's own directory under `~/.apogee/skills` was refused as a
  "write or delete", and `copy_file` materializing a resource out of the library would have been
  refused on its `source` string the same way. The same shape hid in the older path rules —
  reading `.git/config` to inspect a remote was refused as a write to the git control plane. The
  four write-shaped rules (`~/.ssh`, credential files, `.git` control plane, `~/.apogee`) now
  carry a `WritesOnly` class: a tool that declares itself read-only skips them, and a
  write-capable tool that declares an argument a read-only source (`copy_file`'s `source`, via
  the new `domain.ReadSourceTool`) has that value judged by the read fence instead of by a rule
  about writes. Everything else is exactly as strict as before: the command-shaped rules
  (`rm -rf`, fork bomb, `curl | bash`) ignore the class entirely, a tool that declares nothing —
  `terminal`, `python_exec`, every MCP tool — is fully inspected, an unknown tool is inspected as
  write-capable, and the write half of every rule keeps the hard floor: copying INTO
  `~/.apogee/skills` or MOVING a file out of it (`move_file` deliberately declares no read-only
  source — its source is deleted) refuses as it did.

- **A command that backgrounds a process no longer leaves it running after the call, and a run
  whose descendants wedged the output pipe no longer reports success.** The process-group teardown
  was wired onto `cmd.Cancel` alone, which fires only when the run's context is cancelled or its
  timeout expires — so a command that exited cleanly after backgrounding something left that
  descendant alive, and the tool rendered the call as a green tick. That is a persistence primitive
  handed to whoever authored the bytes the model is acting on, and it contradicted the execution
  tools' own documented contract: one-shot, a fresh process per call, no persistent shell
  (ADR 0008). The teardown now also runs after a normal `Wait`, on POSIX as a negative-PID kill of
  the process group and on Windows as a termination of the Job Object that already held the tree,
  so every exit path leaves no descendants rather than only the cancelled one. The second half is
  what the call reported while this was open: when something still holds the output pipe after the
  process exits, `Wait` is cut off at the five-second drain limit and returns `exec.ErrWaitDelay`,
  which is not an exit error — so the exit code fell through to the leader's own status, 0, and the
  truncated output read as a clean success. A wedged drain is now surfaced on the result, the exit
  code is no longer flattened to 0, and the tool result says in words that something the command
  left running still held the pipe and was killed. **Deliberate cost:** a `terminal` call that
  intentionally backgrounds a long-running server now has it reaped when the call returns. That was
  already the documented contract, but it is behaviour someone may be relying on by accident.

- **A bidi override in a tool argument can no longer reorder the approval pane.** A right-to-left
  override reverses the glyphs of the row it sits in without changing a byte the executor reads, so
  the pane could show one command while the tool ran another — the same "read one thing, run
  another" family as the forged rows and duplicate keys fixed beside it, and invisible to both,
  because flattening a newline does nothing to U+202E. Three seams kept these: the transcript's
  escape strip, which is the decision surface, tested C0 and DEL only; the title strip tested
  `unicode.IsControl`, which is Cc; and the session-id validator used the same C0 test — the last
  two being how a forged title rides into a saved session and back out onto the history browser.
  All three now handle the bidi set — the embeddings and overrides U+202A–U+202E, the isolates
  U+2066–U+2069, and the marks U+200E and U+200F — the two display seams by dropping it, the id
  validator by refusing the id outright, because an id is an identity rather than prose. The set is
  deliberately the bidi controls and NOT all of `unicode.Cf`: Cf also holds U+200D ZWJ, which is
  load-bearing inside an emoji sequence, and U+00AD soft hyphen, and dropping those where text is
  DISPLAYED would mangle prose a person legitimately wrote. The seams where untrusted bytes arrive
  or are stored — the fetched-header and URL neutering, the library store's note sanitizer — go on
  dropping all of Cf; that asymmetry is intended, and a test now pins the narrow set as narrow so a
  later "consistency" change breaks a test rather than someone's emoji.

- **The git tools no longer run programs the repository names.** The five tools scrubbed the
  ENVIRONMENT they handed git — an allowlist, so no inherited variable could redirect config, auth
  or the pager — and stopped there. But git also runs programs the REPOSITORY names, and on an
  attacker-authored checkout those are attacker-authored scripts: `git_commit` executed that
  repo's `.git/hooks/pre-commit`, `git_branch` its `post-checkout`, and the two read-only tools ran
  whatever textconv or external-diff driver its `.gitattributes` selected and its config defined. A
  hook is a shell script git runs on the operator's behalf with no gate of ours in front of it, and
  the read-path drivers are worse than that: they execute during what the operator approved as an
  inspection, and `git_diff_range` then reports the driver's rendering rather than the stored bytes
  — a textconv driver that prints a constant made a real change read as `No differences found`.
  Every invocation now carries `-c core.hooksPath=`, which empties the directory git resolves hooks
  from and so covers EVERY hook, including the `post-*` ones a `--no-verify` never reaches; the
  committing path passes `--no-verify` as well, because that is the one hook that can also veto or
  rewrite the message the operator approved; and the two read paths pass `--no-textconv` and
  `--no-ext-diff`. `GIT_CONFIG_NOSYSTEM=1` joins the scrubbed environment, dropping the system
  config from the files git merges. Delivery needs a repository shipped WITH its `.git` — a
  tarball, a mirror, an NFS checkout — or one in-workspace write into an existing `.git/hooks/`;
  a plain `git clone` carries no hooks, so the write is the realistic variant, and it is now
  refused on the dangerous floor as well. Two residuals are deliberate: `HOME` stays on the
  allowlist, so your own `~/.gitconfig` still applies (the threat model trusts the operator and
  distrusts the bytes in the workspace), and a `.gitattributes` clean/smudge filter names a driver
  whose command lives in config, which git offers no global switch to refuse — the read-path
  drivers, the ones a mere inspection would otherwise execute, are the half that is closed.

- **A directory symlink inside the workspace can no longer redirect a write.** The workspace fence
  answers one question — does this path leave the root — so a symlink that stays INSIDE it was
  followed, deliberately: only the final name of a write was protected. A cloned repo shipping
  `docs → .git` therefore turned an approved `write_file docs/config` into a rewrite of the
  repository's own config, entirely within the workspace, so `confine-to-workspace` never fired and
  the approval pane read `docs/config` from beginning to end. A write now refuses a path whose
  PARENT chain crosses a symlink — before anything is staged, created or `mkdir`'d, so a nested
  target cannot create directories on the far side of the link on its way to being refused — and
  the refusal names the component that is a link and where it points, rather than reading as an
  ordinary I/O failure. The rule is "a write reaches its target through real directories": the
  final NAME is unchanged, still REPLACED rather than written through when it is a symlink, and a
  parent pointing outside the workspace still reports the same "outside the workspace" refusal it
  always did, because that half is the fence's to answer and every caller already matches it.

- **An edit now names the file it actually read.** Reads keep following a symlink that stays inside
  the workspace — refusing them would break the ordinary linked-file layouts a repo legitimately
  has — which leaves the sharper half of the same trick: `docs/notes.md` as a link to `.git/config`
  is READ through, patched, and the result written back to `docs/notes.md`, which discloses the
  target's contents to the model and destroys the link, reported as "applied patch to
  docs/notes.md". `edit_existing_file`, `single_find_and_replace` and `multi_find_and_replace` now
  carry the same `→ resolves to <path>` disclosure `write_file` gained for its write target, naming
  the file the bytes came from whenever that is not the file the argument named. It is resolved
  BEFORE the write, because the write leaves a plain file behind whatever the name was — a note
  taken afterwards would go quiet on exactly the call worth disclosing.

- **Opening the `/` menu no longer stalls the screen while the skill catalog is re-scanned.** The
  merged menu re-scans the skill source dirs whenever the caret enters a `/` token, so a skill
  written since launch both shows in the menu and resolves when invoked. That scan ran on the same
  goroutine that draws the screen: the keystroke which opened the menu waited on a full walk of
  every source dir before the frame it was typed into could be painted, and every other message the
  loop had to handle — streamed reply tokens included — waited behind it. On a large library, a
  network-mounted home, or a workspace tree a cloned repo made big on purpose, that is long enough
  to read as a hang. The walk now runs off the loop as a command and reports back when it finishes
  (ADR 0011's division: disk work on a worker, its result as a message). The menu opens immediately
  over the catalog as it stood and repaints over the fresh one the moment the scan lands. Two
  things follow from the scan outliving the keystroke, and both are held: the highlighted row
  survives the repaint — matched by name, not by index — so a scan finishing while you are arrowing
  down the list cannot move the selection out from under you, and a menu you dismissed with esc
  stays dismissed instead of being painted back by a result arriving after it. The `/skills` report
  still scans inline, where the walk IS the answer you asked for rather than a side effect of a
  keystroke.

- **A cloned repo can no longer relocate the skill loader's fence by symlinking a source dir.**
  Discovery pinned its `os.Root` by opening `<workspace>/.apogee/skills` directly, and an open
  follows symlinks in EVERY component of the path it is handed, the last one included. So a repo
  that shipped `.apogee`, `.apogee/skills` — or `skills`, with project skills on — as a symlink
  moved the fence itself: the walk meant to be confined to a folder in the workspace read a tree
  anywhere on the disk, and every `SKILL.md` it found there loaded as instructions the model can be
  handed. (A symlink BELOW a source dir was already refused; the anchor naming that dir was the
  gap.) The loader now pins its root at the workspace and reaches the source dir THROUGH that
  fence, so every component of a workspace anchor is resolved inside the workspace root. The rule is
  containment, not a symlink ban: a source dir symlinked to another folder within the same base
  still loads, while one pointing outside is passed over and RECORDED, so `/skills` names the dir
  and why it was not scanned instead of leaving a vanished library indistinguishable from an absent
  one. The global library below the apogee home is the operator's own territory rather than a
  repo's, so its anchor keeps following the symlink that names it — a dotfiles-managed library still
  loads — and the fence pins at the library that symlink RESOLVES to, so a symlink below it that
  leaves is refused all the same. The walk is bounded to match:
  the existing cap stopped the CATALOG at 1024 skills but never stopped the walk, so a tree that
  loads no skills at all — a million empty folders, or one folder nested a million deep — was still
  toured in full, on every mid-session reload. Discovery now descends at most 4096 directories and
  8 levels below each source dir, noting where it stopped the same way.

- **A skill id can no longer be a command line.** A workspace's `.apogee/skills` is an
  unconditional source and the catalog re-scans mid-session, so a cloned repo could ship a skill
  whose id is `confine off --save`. The merged `/` menu's shadow guard dropped a skill only when its
  WHOLE id matched a command verb, found no collision there, and offered the row like any other
  skill — while the parser, which cuts a `/` line at its first space or tab, read the accepted token
  as `/confine` with arguments: Auto's fence off and the host persisted, from a row the human
  believed was a skill. Two layers now close it. The loader REFUSES an id holding whitespace or a
  control character — an id is ONE token, and a repo authors both the frontmatter and the folder
  name an id can be derived from — so such a skill never reaches the catalog and the skip is
  reported like any other malformed one. And the menu's guard now keys on the id's FIRST TOKEN, read
  from the parser's own cut rather than restated beside it, so the two layers cannot drift apart
  again; a registry-wide test asserts no verb of `commandSpecs` can be out-parsed by a skill id.
  Nothing legitimate moves: a kebab-case id loads as before, a display name may still be prose, and
  a skill shadowed by a verb stays invocable as a `/token` anywhere but at the head of the line.

- **A write whose path does not point where it reads now says so, on every surface that shows the
  call.** apogee has always resolved a write's true target — it is what the blast-radius ladder
  classifies the call by — but consumed it as a bool and nothing else, so the approval pane, the
  tool card and the write's own result sentence all quoted the model's argument: a `docs/notes.md`
  whose `docs` is a symlink out of the workspace read as an ordinary in-workspace write right up to
  the moment it landed elsewhere. All three now keep the argument exactly as the model wrote it and
  add `→ resolves to <where it lands>` beside it. The line is drawn ONLY when the two differ, so
  every ordinary prompt, card and result is unchanged to the byte. It is the ENGINE that decides
  there is something to say (`domain.ApprovalRequest.ResolvedPath`,
  `domain.ToolCallEvent.ResolvedPath` — both populated only on a divergence), and it hands over the
  very path the gate judged the call by, so what you are shown and what the gate decided from can
  never be two readings of one call. It rides the call event as well as the approval because the
  surfaces that show a write are not only the gated ones: in Allow-Edits and Auto an in-workspace
  write runs with no prompt at all, and the tool card is then the only place the resolution is ever
  read. Observation only and additive — the engine stays wire-silent, nothing is added to a tool's
  arguments, and a Driver that ignores the new fields renders what it always did.

- Approval pane and tool cards no longer let a long argument value or a repeated key hide what a
  call will do: each value is capped at eight lines and an elided value (or an elided pane body)
  keeps its LAST line under the `… (+N more lines)` marker as well as its head — the tail is where
  an appended payload lives — and a key the model wrote twice is shown once, carrying the value the
  executor actually receives (stdlib JSON is last-wins, as are both guards) and marked
  `(duplicate key — last of N wins)`.

- **A tool call can no longer paint rows of its own on the approval prompt.** The prompt draws one
  row per line of its body and every row is styled alike, so any model-authored string that reached
  it carrying a newline painted extra rows — and an extra row could be a second `Reason:` line,
  sitting above the real one and indistinguishable from it. An argument NAME was the shortest route
  (JSON puts no restriction on what a key may hold), and a `sub_agent` task was the loudest, because
  that line leads the body. Each of the prompt's label lines — the Sub-agent line, `Reason:` and
  `Fix:` — is now folded onto the single line a label is, as is every argument name, so a newline
  inside one shows up as the space it replaced rather than as a row the prompt did not write. **What
  does not change:** an argument's VALUE keeps every line it arrived with, indented under its own
  label, because those lines are the fact you are ruling on — a four-line command still reads as the
  four lines that will run.

- **A routed delegation's context fill is now measured against the window it actually filled.** Send
  the grunt work to a Sub-agent server with an 8k window from a session running a 128k model and the
  child's line read `7k/128k` — a nearly-empty gauge for a child that was in fact nearly full,
  because both the TUI and `apogee headless` painted every delegation's fill against the SESSION's
  window. Each reading now carries the window of the agent that produced it, so the same run reads
  `7k/8k` and the number means what it says. Delegations that ran on the session's own upstream are
  unchanged — an unrouted child inherits the parent's window verbatim, and a reading that names no
  window (a session recorded before this) still falls back to the session's. Like the fill itself
  the limit is frozen when the reading lands and kept in the session record, so a resumed session
  repaints the window the run really filled.

- **A Sub-agent server that never says how big its window is no longer leaves the child without
  one.** Flag a `servers:` entry for delegations, give it no `context-window:` pin, and point it at
  a server whose heartbeat reports no per-slot window either, and every routed child used to be
  built against a window of zero: its Budget and automatic Compaction went inactive, and its
  readings carried no limit, so both the TUI and `apogee headless` fell back to painting that
  child's fill against the SESSION's window — the one window it was not working in. A target that
  names no window now leaves the parent's standing, so the child is never constructed windowless
  and its readings state the limit it actually filled. A target that does name a window still
  overrides the parent's, as routing has always done.

- **A stray thinking closer no longer leaks into the reply.** Some chat templates pre-open the
  thinking channel themselves — the server has already consumed the start token by the time the
  model's content arrives — so the reply carries a bare `</mm:think>` (seen live from minimax-m3)
  with nothing that looks like a span to strip. The stripper now reads an end token with no opener
  of its own as closing an implicit span opened where the last one left off — position 0 for the
  first: everything ahead of it is reasoning, never visible content, and a normal span later in the
  same message still strips as it always did. Every such closer is absorbed, not just the first, so
  a model that re-opens the channel that way leaves no stray closer behind either. A closer that
  follows its own opener is untouched, and so is a span still being streamed.

- **A delegation in a fan-out is marked done the moment IT finishes, not when the whole group
  joins.** The transcript now follows each `sub_agent` block's own lifecycle phase, so the member
  that reported first wears its ✓ and says `done` while its siblings are still working — and its
  report can be read inside it right away, because the finished phase carries it. Before this the
  display could only go by the group's trailing result burst, which by design arrives in call order
  after the last child has joined, leaving early finishers looking busy for as long as the slowest
  one ran. Nothing about history changed, and the burst that follows is a no-op on a member already
  marked: the report is folded in once, never twice.

- **A running sub-agent's row no longer flickers through the child's every tool call.** Its summary
  said `N tool calls · 12k/32k · reading · some/long/path.go`, and that last cell was re-read on
  every frame: the one part of the row that changed several times a second sat beside the two parts
  worth reading, and pulled the eye to the least durable thing on screen. The row now reads
  `N tool calls · 12k/32k` while the child works — the calls it used to name are all still there,
  each as a block of its own inside the run, one click away. One live word survives: `· delegating`,
  shown only while the most recent call open inside the run is itself a sub-agent, which is the one
  state the run's own (collapsed) blocks cannot show. Finished rows are untouched — still the
  report's first line, or `done`. Lone and grouped delegations read alike, and the status line's own
  activity phrase is unaffected.

- **Expanding a sub-agent no longer wipes its summary.** An open delegation reverted to the raw call
  row, so the `N tool calls · 12k/32k · done` a reader had just been shown vanished the moment they
  clicked it — the click that asks for more said less. The open row now carries the same right-hand
  slot its collapsed row carried, running or finished, lone or grouped, and the report, the delegated
  prompt and the railed span come out beneath it. Collapsed rows are unchanged; only the body was
  ever the fold's business.

- **An approval prompt for a terminal command no longer blames the host for a gate the autonomy rung
  asked for.** In `ask-before` or `allow-edits` the prompt read
  `Reason: subprocess execution (confinement unavailable on this host)` even on a machine whose
  `/confine status` reported a backend confining perfectly well — two statements out of the same
  apogee, contradicting each other, and the prompt was the wrong one: those rungs gate the
  subprocess surface as a mode decision, capable host or not. Such a gate now reads
  `Reason: subprocess execution`, the bare statement of the reach being authorised, in the family
  every other class already uses. The confinement wording is kept for the one case that earns it —
  Auto with `confine-to-workspace` on and a backend that cannot fence — where it is true, and where
  it is what sends a reader to `/confine`. Which calls gate is unchanged; only what a gate says is.

- **A command asked to run fenced never runs unfenced instead.** If a run arrived carrying the
  instruction to confine but nothing to confine it with — an empty box handle, the shape a
  mis-wired embedding or a future backend can hand down — the command used to launch anyway,
  outside the fence, without a word to anyone. It now stops before launching and reports that
  confinement is unavailable, which is the same route a backend that tries and fails already
  takes: you are asked to approve the unconfined run, in the prompt that says why. Runs that were
  never meant to be fenced — everything you approve, and every call under a rung that gates
  instead of confining — are untouched and still run exactly as before. Nothing in apogee's own
  tools could produce the broken shape today; the door is now shut for what can.

- **A tool reporting that it could not confine something is never answered with silence.** apogee
  read that report as its signal to demote a fenced run to an approval — but on a call nothing had
  been asked to fence, no demote was waiting for it: the call was recorded as executed, no error
  reached you, and the model was handed an empty result for a tool that had in fact refused to run.
  Any tool you or an extension registers could land there. Such a report now behaves like any other
  tool failure on an unfenced call — the error is shown and the model is told the call failed —
  while a genuinely fenced run that cannot establish its fence still demotes to the approval prompt
  exactly as before.

- **A store tool's complaint can no longer leak the beginning of a key it was cut off mid-secret.**
  Stderr from `security` or `secret-tool` is captured under a 4 KiB cap, and the cap is a BYTE cut:
  when it fell inside the key the tool had echoed back, the key's first bytes stayed in the buffer as
  a fragment the whole-value redaction could not match, and rode into the refusal message — the
  terminal, the session log and the pasted bug report that migrating the key out of the config file
  exists to keep it away from. A capture that filled the cap now has its tail trimmed of the longest
  run that spells the beginning of the secret before redaction runs, in both spellings the secret
  travels in (macOS quotes it onto the `security -i` command line). A capture that stopped short of
  the cap was never halved, so nothing is trimmed from it and the tool keeps its own words.

- **A one-line settings field decides for itself what it may hold.**
  `lineEditor.flattenLine` — the fold a single-line field applies to text that arrives through a door
  no keystroke binding covers (a bracketed paste, a clipboard reply) — folded only the newline, so a
  tab or a carriage return in that text was left to whatever the text widget underneath happened to
  do with it. It now folds all three, each to one space, rune for rune, so the caret still stands on
  the rune it stood on. In practice the bubbles textarea's own sanitizer still gets there first on
  every write (a tab spends as four spaces, a carriage return as a newline), so nothing the human
  pastes changes shape today; what changes is where the invariant lives — at the field, not borrowed
  from a dependency's default configuration, and so still true if that default is ever reconfigured
  or the widget replaced. The display-seam sibling `flattenField` already folded tabs for the same
  reason.

- **The README's key-source failure list now names the empty variable.** `api-key-env:` refuses a
  variable that is set but *empty* — its own message, separate from the unset one, because the two
  have separate fixes (nobody exported it, versus a command that produced nothing) — but the
  README's sentence listed only "an unset variable" among the failures, so a reader could take an
  exported-but-empty variable for a working key source and expect a request to go out keyless. The
  sentence now reads "a non-zero exit, a 60-second timeout, empty output, or an unset or empty
  variable", matching what `resolveEnvKey` actually does. Docs-only; no behaviour changes.

- **Pinned that `ScopeEnv` scopes the Windows `Path` spelling under a real workspace root**: every
  Windows subtest of `TestScopeEnvKeepsTheCallersAllowlistAndAddsThePlatformFloor` passed an empty
  root, which scopes nothing, and the only test driving `windowsRules()` against a real root went
  through `ScopeInheritedEnv` — so on the allowlist path the case-insensitive fold and the PATH
  scrub could have come apart without a failing test, in the direction that matters (the spelling
  that survives the fold handing the child back the workspace directories the other one dropped).
  A new subtest drives `windowsRules().ScopeEnv` over `C:\work\repo` with the allowlist naming both
  `Path` and `PATH`, each holding a value that mixes in-workspace, relative and system entries, and
  asserts the surviving spelling is the SCOPED one: the workspace and relative entries gone, the
  system entry kept, the folded duplicate absent, and the platform floor following the allowlist
  untouched. Test-only; no shipped behaviour changes.

- Exec-fence and diagnostics tests now build their roots with the `tempRoot(t)` helper, so their
  exact-path assertions hold on hosts whose `TMPDIR` is a symlink (macOS `/tmp`).

- Documented the recursive-delete rules' actual precision boundary in
  `internal/security/rules.go`: the bare `/` branch hard-refuses EVERY absolute target
  (the project's own directory included) and only relative/`./` targets stay allowed —
  the system-path enumeration is retained as documentation of the worst cases, not as the
  discriminating branch. Comment only; no regex, behaviour or test change.

- Corrected three drifted doc comments: `promptFS` (`internal/mechanisms/toolloop.go`) now
  describes `prompts/` as the package's prompt text for every mechanism rather than one
  directive's fragments; the `takesArgs` bullet (`internal/tui/command.go`) names both verbs
  that keep a dedicated parse (`/confine` and `/color-scheme`); and the package doc
  (`internal/tui/doc.go`) acknowledges that `/schedule`'s prompt form reads the line's raw
  tail instead of a plain token list. Comments only; no behaviour change.

## [0.13.0] — 2026-08-11

### Added

- **`/usage` — what this session has spent, agent by agent.** The status gauge is a *fill*: it says
  how full the window stands right now, nothing about the tokens a long run burned through and
  compacted away, and nothing at all about a delegate whose window closed when its run ended.
  `/usage` opens a popup that answers that question instead — `agent · calls · prompt · completion ·
  total · ctx`, one row for the main agent, one for every sub-agent that reported a count (in
  transcript order, indented under it, named by the delegation or the first line of its task), and a
  `session` row adding them up wherever there is more than one agent to total. `esc` closes it, a
  click outside dismisses it without swallowing the click, and the wheel scrolls the rows when a
  session fanned out past the pane's row budget. The verb is safe while the model works — the pane
  reads what the frame already holds and calls nothing — which is exactly when the question gets
  asked. The counting itself lives in the **engine**, not the screen: every agent, main and each
  delegate, keeps its own running totals and stamps them on each `UsageEvent` it emits, so every
  Driver reads the same numbers with the latest event per agent winning and no consumer summing
  anything. Compaction is counted too, on an event flagged `Maintenance` that the fill gauge and the
  tokens/sec clock skip and the totals accept — so the report is right immediately after a
  `/compact` rather than pretending the fold was free. The totals ride a session save and come back
  on reload; sessions recorded before this feature reload with zeros. `apogee headless` reports the
  same figures on stderr — `usage: calls 3 · prompt 18k · completion 1k · total 19k` for the run and
  one such line per delegated run — and they are on `run.Result` for any other Driver to read.
- **An expanded sub-agent is now a framed span that opens with what the delegate was asked.**
  Opening a delegation used to reveal a `⤷ sub-agent` label and then, straight away, the child's
  tool calls — the one thing missing was the instruction that produced them. The row you open a
  delegation from is now the top-left corner of its own frame (`┌─┶ …`), the whole span runs behind
  a gold `│` rail at column 0, one `┊` parts it from the next row of its `✦ Sub-Agent (N)` list
  where the blank separator used to be — a span with no further member behind it, a lone delegation
  included, simply ends on that separator — and the
  first thing inside the frame is the delegation's full task, rendered as markdown and wrapped to
  the railed width. The label went with it: a frame already says "this is inside the delegate"
  without spending a row on saying so, and the task is now retained through a session save and
  reload rather than being dropped once the call was presented.
  - **One shape, everywhere, live.** A lone delegation, a member of a `✦ Sub-Agent (N)` list and a
    delegation nested inside another all open into the same frame at their own left edge, and it is
    drawn from the first token: the header wears the spinner star and the child's streamed text
    lands inside the rail, so a run settling into the transcript changes nothing about its shape.
  - **A finished delegation is marked `✓`** — after its name and before the dots, on the collapsed
    row and on the open header alike, painted in a new green `success` scheme role seeded in both
    shipped schemes. A delegation still working carries none, and a failed one is marked by its red
    outcome slot alone: a green tick beside a red `error: …` would be two answers to one question.
    The slot still says `done` / `failed` exactly as before.
- **Tool rows now read left-to-right: what was touched, a dotted leader, what came of it.** A tool
  call's branch line puts its target on the left, runs a faint `⋯` leader across the gap, and sets
  the outcome — `12 lines`, `+2 −2`, `exit 0`, a red `error: …` — flush against the block's right
  edge, with the `▶`/`▼` in a field reserved past it. Every row fills its width exactly, so the
  outcomes line up down the edge of a group instead of drifting with the length of each target, and
  a row is the same shape open and closed: clicking one no longer moves it out from under the
  pointer. When the width runs short the row gives things up in a fixed order — the dots flex down
  to a single `⋯` first, then the target is cut with `…`, and only a row too narrow to hold even a
  cut target drops it outright. What happened is the half worth keeping, so the outcome is the last
  thing to go and prints whole until the row itself is narrower than it is.
  - A collapsed tool block is now at most three rows, because a targeted branch is always exactly
    one — the `▶`/`▼` moved off such a block's header onto that branch row, where the spec draws it.
  - **A one-line output only takes the outcome slot while the row can still spare it.** A command
    whose whole output came to one line rides the branch as before, but on a row that would leave
    the target under ~15 cells the line goes back to being the block's body and the slot shows the
    count instead — `1 line`. Nothing is lost: the block now has something to reveal, so it wears an
    indicator and one click shows the line whole, spelled exactly as the tool printed it. A row
    about a command you can no longer read is a row about nothing, which is what the guard buys back
    at narrow widths.
  - A cut target on its own is no longer a reason to click: with the row identical in both states,
    expanding a bodiless call whose path the width trimmed revealed nothing, so it now wears no
    indicator at all. Blocks with a body are unchanged.
  - **New `tool-leader` scheme role** for the dots, seeded in both shipped schemes from the same
    faint tone the `▶` beside them wears. It is its own role so a scheme can damp the leader without
    dragging the indicator's tone along with it; `/color-scheme export` writes it like any other.
- **Every tool now says the one thing worth saying about itself.** The tool cards were brought to
  the ratified per-tool table (`docs/layout/tool-layout.md`): each tool has a short label, a target
  that leads its row, and an outcome slot worded for that tool alone. Labels lost their filler —
  `Read File` is `Read`, `List Dir` is `List`, `Run` is `Terminal`, `Run Python` is `Python`,
  `Run Tests` is `Tests`, `View Diff` is `Diff Preview`, `Web Search` is `Search`, and `grep` is
  `Grep` rather than the `Search` it used to share with it.
  - **`Edit File` split into `Edit` and `Replace`.** A patch (`edit_existing_file`) and a
    find-and-replace are different acts, and blocks group by label, so two adjacent calls of the two
    kinds now head two blocks instead of reading as one `Edit File (2)`.
  - **Targets carry the qualifier that changes what a call did**: a read's line range
    (`main.go:12–80`), an open's `· locate "…"`, a listing's `· recursive`, a grep's include glob, a
    test run's filter. A presented document leads with its title rather than its path.
  - **Outcome slots are per-tool.** A read, an open and a write say `154 lines`; an edit or a diff
    preview says `+8 −3` and a batch replace `2 changes`; a grep says `12 hits`, a listing
    `40 entries`, a search `3 results`, a find `12 files`; a command says `exit 0`, a test run
    `PASS`/`FAIL`, a diagnosis `clean`, a delegation `done`; git says `3 changed`, `12 commits` or
    the short hash it just wrote. Copies, moves, deletes, branch switches and presentations say
    nothing at all — their row already does — and the dots simply run to the `▶`.
  - A one-line output still takes the slot where it fits, with the typed stat behind it as the
    narrow-row fallback, so a command that printed one line reads as that line and a command that
    printed forty reads as `exit 0` with the output a click away. A commit is the exception: its
    line (`6fd6ff7 feat: x`) repeats the subject its row already leads with, so the slot keeps the
    short hash at every width and the line lays out in the body.
  - Nothing was added to any tool result or crossed any wire for this: a write's line count and an
    edit's diffstat are read off the call's own arguments, and a stat that cannot be had honestly
    leaves the tool's own first line in the slot rather than inventing a number.
  - `open_file`'s locate report moved off the branch into the block's body, where the line numbers
    sit under the path and term that asked for them.
- **An open tool block now closes with a right-aligned `see less…`**, the row a reader who has just
  read to the end of the output is already on. It joins the block's click surface, which is the
  whole block: its header, its branch row, every line of its body and now the footer under them all
  toggle the block a click lands on — and inside a folded group, the row you clicked toggles the
  member it belongs to rather than the run. A drag still selects and copies as before; only a
  press-and-release that never moved is a toggle. Blocks with nothing to reveal grow no footer,
  because a `see less…` that did nothing would be an affordance about nothing.
- **A batch of different tools now folds into one `✦ Tools (N calls)` block.** Adjacent calls used
  to stack one header per tool, so eight calls in a row cost eight blocks and a screen of scroll.
  They now fold under a single umbrella carrying one row per consecutive run of the same tool, in
  time order — calls are never reordered to merge, so `read, terminal, read` stays three rows — with
  the run's own count beside its label (`Read (3)`) and the run's outcome aggregated on the right:
  a red `2 errors` when anything in it failed, the natural sum where the stats add up (`570 lines`,
  `+8 −3`, `12 hits`), and nothing at all when they do not. Anything between two calls still breaks
  the batch — narration, a note, an approval — and a sub-agent block always does.
  - **Two clicks reach any single line of output.** A click on a type row lists the calls behind it,
    each in the same target-leader-outcome shape as anywhere else; a click on one of those opens its
    body under a `│` gutter. The two levels are independent and both survive scrolling and new
    messages, so a run you opened stays open while the model keeps working beneath it.
  - **The umbrella header closes everything under it.** It has no fold state of its own — its floor
    is the type rows, it never collapses to one line — so clicking it is the one gesture that puts a
    whole exploded batch away, and it does nothing at all while nothing is open.
  - The header's star blinks while any call in the batch is still running, and the running call is
    the last row: the block appears the moment a second differently-labelled call starts, rather
    than settling into shape only once the work is done.

- **A fan-out of sub-agents now reads as one `✦ Sub-Agent (N)` list.** Delegations standing next to
  each other used to cost a full block each, so three parallel agents filled the screen with headers
  before any of them had reported. They now fold into one list with a row per agent — its name (or
  the head of its task) on the left, and on the right the same live tail a lone delegation carries:
  how much work is behind it, how full its context is, and what it is touching right now, replaced by
  its report once it lands. Delegations group only with each OTHER: one still breaks a batch of
  ordinary tool calls and never becomes a row of it.
  - **Opening a row opens that agent's whole run**, railed exactly as before, with every
    block inside it keeping its own fold state. The list resumes underneath, so the siblings stay one
    click away while you read through one of them.

- **The transcript now folds from the keyboard too — `⌥↑` / `⌥↓`.** Everything the mouse can open
  was reachable only by pointing at it, which is no help over ssh, in a terminal without mouse
  reporting, or for anyone who simply keeps their hands on the keys. `⌥↑` (or `⌥↓`) now lights a bar
  on a tool block and hands the arrows to the transcript: `↑`/`↓` walk from one foldable thing to the
  next, `⏎` opens or closes the one under the bar, and `esc` gives the keys back. The walk visits
  exactly what a click can reach, at the level it is showing — a block, a group member, a type row
  inside a `✦ Tools (N calls)` umbrella — and one stop per thing, so a forty-line output is a single
  step rather than forty. The view scrolls to keep the bar in sight as you go.
  - **Typing ends the walk and still types.** The letter that broke you out of it opens the message
    in the prompt box instead of being swallowed, so there is no mode to remember being in. An
    approval or a question borrows the arrows back for as long as it is up, then the walk resumes
    exactly where it was standing.

- **`<u>…</u>` renders as underlined text.** Markdown spells no underline of its own — CommonMark
  spends `__` on strong emphasis — so the renderer recognises the one HTML pair a model actually
  reaches for, and assistant text saying `press <u>Enter</u>` now shows *Enter* underlined with the
  tags consumed. It works everywhere inline markup does, table cells included. The match is exact
  lowercase bytes: `<U>`, `<u >`, `__text__` and every other tag stay literal, an unterminated `<u>`
  mid-stream stays literal like an unterminated `` ` `` or `**`, and a `<u>` inside a code span is
  still just text. This is not the start of an HTML parser.
- **Type-to-filter across the selector pop-ups.** Every overlay that offers a list now narrows as
  you type: `/model` over the models a server advertises, `/model` over llama-launcher's Launch
  profiles, `/server`, `/schedule`'s cycle and mode panes, `/schedule-stop`, and the `/sessions`
  browser. Any printable key extends a case-insensitive filter — there is no activation key to press
  first, because the pane is modal and no letter is a verb inside it — and a row survives when what
  you typed appears anywhere in what that row shows: its name, its `— endpoint` or `— backend`
  gloss, its `· running` marker. A three-hundred-model offering is now a few keystrokes away instead
  of an arrow-scroll through an eight-row window.
  - **The filter only prunes.** Surviving rows keep the offering's own order, so nothing is ranked
    up out from under the highlight while you type, and `⏎` always takes the row the pane painted.
  - **`⌫` takes back a character; `esc` still closes the pane outright**, even mid-filter, so the
    hint's `esc close` never means something else. The filter is cleared with the overlay — the next
    open starts on the whole list.
  - **What you typed shows in the pane** as a `filter: qwen▌` line under the title, set off by a
    blank line above and below, and only while there is something in it. A filter that matches
    nothing leaves the pane open with that line over no rows, rather than closing or complaining.
    On a terminal too short for everything, the rows shrink before the filter line does.
- **The `/sessions` browser's letter verbs are now chords — `^r` rename, `^d` delete, `^a`
  this-workspace/all-workspaces.** This changes muscle memory on purpose: a browser you can type
  into cannot also treat `d` as delete, and the saved-session store is exactly the place you want to
  type a name. The delete confirm's `y`/`n` and the rename editor are unchanged.

  The type-to-filter grammar and the rebound verbs are documented in `layout.md` (§"One overlay for
  'which one?'" and the `/sessions` browser paragraph beside it).
- **Color schemes — apogee's palette is now a file you can pick, and write.** Every colour on screen
  comes from the active **colour scheme**: one YAML file of 28 semantic roles (`error`, `code`,
  `tool-header`, `skill`, `file-ref`, `muted`/`muted-bright`, the four autonomy modes, the four
  spinner stops, …), named for what they mean rather than where they are drawn. Two schemes ship,
  compiled into the binary: **`dark`**, which is the palette apogee has always drawn with apart from
  three roles retuned for legibility while the scheme system was being built (`code`, `tool-header`
  and `tool-marker`) and stays the default, and a new **`light`** for light terminals. Nothing is
  installed on disk and nothing is ever downloaded.
  - **`ui.color-scheme` in the `ui:` block** selects it, and **`~/.apogee/schemes/<name>.yaml`
    shadows a built-in of the same name** — write `dark.yaml` there and you have adjusted `dark`
    while still typing `dark`; delete the file and the shipped one is back.
  - **`/color-scheme`** is the verb: bare, it lists what this session can switch to with the
    current one marked; `/color-scheme <name>` switches the screen **and saves the choice** through
    the same write the settings pane uses; `/color-scheme export <name>` writes an editable,
    fully-commented copy of a built-in into `~/.apogee/schemes/` and refuses to overwrite a file
    that is already there.
  - **The `/settings` pane carries a picker row** for the key — built-ins and your own files in one
    list — and it applies **live**: the whole screen is rebuilt and repainted on the ⏎ that saves,
    no restart anywhere.
  - **Tool-call headers carry their own role.** `tool-header` paints a block's header label (`Run`,
    `List Dir`, `Sub-Agent`, …) and the sub-agent rail that hangs off it, instead of borrowing the
    `code` role that paints inline code and fenced blocks — so what apogee *ran* reads apart from
    the code it *prints*, and either tone can be retuned without dragging the other along.
  - **A defective scheme costs colour, never the session.** A bad hex, an unknown key, an unreadable
    file or a name that resolves to nothing each fall back to the default and say so in a dim
    transcript note naming the file and the key — at start-up and after a switch alike. Every key is
    optional, so a two-line scheme file that changes two roles is a perfectly good scheme.
  - Schemes recolour **only what apogee already colours** — no full-screen background is painted, and
    no glyph, marker or layout is themed. Recorded in
    [ADR 0040](docs/adr/0040-color-schemes-are-embedded-roles-with-user-shadowing.md), with the role
    vocabulary in `CONTEXT.md` §Color scheme and the per-role prose in `layout.md`.
- **`editor:` names the editor, and saving `config.yaml` is what applies an edit — from any editor,
  in any window.** The `/settings` pane's `⏎` jump on a block no row can hold used to suspend into
  `$VISUAL`, else `$EDITOR`, else `vi`, and to diff the file when that program exited. That contract
  broke on every editor a desktop actually opens: `open`/`xdg-open`/`code` return before the window
  is even on screen, so the exit-triggered diff read unchanged bytes and concluded you had edited
  nothing.
  - **A new file-only `editor:` key** takes a whole command line (`editor: code -w` — split on
    spaces, so flags travel with the program) and heads a **four-rung ladder**: this key, then
    `$VISUAL`, then `$EDITOR`, then your platform's default opener (`open` on macOS, `xdg-open` on
    Linux, `cmd /c start` on Windows). Config beats environment, so the row shows the command that
    will really run. **Unset now means your desktop's default `.yaml` application, not `vi`** — a
    deliberate behaviour change. When nothing on the ladder is installed, the row refuses and names
    all three ways to set an editor instead of repeating a `$PATH` error.
  - **Terminal editors still take the terminal** — `vi`, `vim`, `nvim`, `nano`, `pico`, `emacs`,
    `micro`, `hx`, `kak` suspend the TUI and re-read on exit, exactly as before, because one drawn
    over a live alternate screen is broken. **Everything else is started detached**: the pane stays
    up, nothing waits on it, and the row says `· opened in your editor`.
  - **`~/.apogee/config.yaml` is now watched for the whole session** — a poll of mtime and size on a
    one-second ticker, in-process, no new dependency and no daemon. Any save applies live through
    the same re-read/diff/apply an in-pane edit uses, whoever wrote it: the pane's jump, a GUI editor
    left open in another window, a `vim ~/.apogee/config.yaml` in a second terminal. Each key that
    changed wears the same ` *` on its row.
  - **A file that does not parse changes nothing** and the session keeps the settings it had — a poll
    will catch a half-written save sooner or later. Only three consecutive unreadable saves surface a
    transcript note, and not again until the file parses. A write the pane made itself does not apply
    twice, and `server:` is still never re-applied by a re-read: it names where the *next* session
    starts. Recorded in [ADR 0041](docs/adr/0041-the-config-file-is-watched.md), which supersedes ADR
    0037's editor ladder and its diff-on-exit trigger; the rest of ADR 0037 stands.
- **Sub-agents now run in parallel, bounded by a per-server Parallel agents cap.** When one reply
  asks for several `sub_agent` delegations, the top-level agent runs them **concurrently** instead
  of one after another — the wait for three delegated sub-tasks becomes roughly the longest of the
  three rather than their sum. Nothing about a single delegation changes, and a server that offers
  one slot keeps today's strictly serial behaviour to the byte.
  - **The cap is pin-else-discover-else-1.** A `servers:` entry may carry the new optional
    **`parallel-agents: N`** key; set, it is a **pin** discovery never overrides (the
    `context-window:` idiom). Absent, the cap is discovered from the live server — `total_slots` in
    the `/props` response apogee already fetches every heartbeat, which is the `--parallel N` a
    llama.cpp server was started with. No signal at all means **1**. A negative value is refused at
    startup naming the entry; `0` reads as unset. Remember the trade the server makes: `--parallel N`
    splits its context into N slots, so **more parallel agents means a smaller window each** — the
    per-slot number apogee has always shown (ADR 0024).
  - **Depth 0 only, and siblings are independent.** A sub-agent's own delegations still run serially
    inline, so there is no slot accounting and no deadlock. A child's failure — an error, a tripped
    breaker, a denied approval — becomes *that child's* tool result and no sibling is cancelled;
    `esc` still signals every in-flight child, waits for them, and rolls the whole parent turn back.
    A mixed reply runs its leaf tools first in emitted order and *then* fans out, so a write a child
    depends on has landed before the children start, and results are appended in call order however
    the children finish.
  - **Approvals and `ask_user` questions share one queue, and each prompt names the child asking.**
    Two children raising an approval — or putting a question to you, or one of each — at once produce
    two prompts one at a time, the asking child blocked and its siblings still running; the pane
    leads with `Sub-agent: <the task it was given>` so "which agent wants this?" is answerable at a
    glance. It is a **single** queue covering both kinds, not one per kind, because a driver draws
    one prompt: the approval pane and the ask box are the same screen and the same keyboard. The
    queue sits in the engine, so every driver — the TUI, the bench, a future daemon — gets
    one-prompt-at-a-time without building a queue of its own (ADR 0031). Without it the second
    request replaced the first on that one surface and the first child hung until you cancelled the
    turn.
  - **Every event a child emits carries the call-ID of the `sub_agent` call that spawned it**, so
    interleaved streams stay attributable: usage readings, audit records and per-run stderr lines
    land on the right child. It is one additive `EventBase` member, persisted as an **additive
    transcript member** — no blob version bump, since an omitempty member is invisible to an older
    build while a bump would make every blob this build writes unreadable to one. (`AuditEvent`
    keeps its own `CallID`, the *audited* call, which shadows the promoted one.)
  - **The scrollback shows one block per child**, in the order the calls were made, each holding
    only its own child's work: its own tool count, its own context fill, its own ticking activity
    phrase, its own streamed text. Expanding, collapsing, clicking and resuming are unchanged — a
    per-child block is a tool block like any other — and a session with one delegate at a time
    renders exactly as it always has. The rendering rules are in `layout.md`.
  - **Guided decomposition dispatches a batch of `min(cap, remaining)` delegations per Turn**
    instead of exactly one (ADR 0014's amendment), keeping the quiescent boundary between batches
    and the singular wording when a batch is one. The Mechanism stays **default-off**: the changed
    stack must re-pass the ADR 0009 bench gate before that can change.
  - **`apogee headless` gets the same width**, so an unattended run fans out exactly as wide as a
    session on that server would (ADR 0031's benchable-all-the-way-up). It resolves the cap through
    the same pin-else-discover-else-1 rule, differing only in how the discovery half is fetched: with
    no heartbeat to read, a run with no pin asks the server **once**, while it composes. A pin skips
    that call — discovery could never overrule it — and a probe that cannot answer costs nothing but
    the serial floor the run would have had anyway.
  - **A scheduled firing gets it too**, taken from the session it fires beneath at the moment it
    fires — so a firing fans out exactly as wide as the server the session is on *now*, and a
    `/server` switch moves the width with it. Previously a firing composed its run from the config
    copied before the session bound anything, which named no width at all: every scheduled run
    delegated one child at a time however the entry read.
  - Recorded in
    [ADR 0039](docs/adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md),
    with amendments to ADR 0013 §5 (per-child atomicity) and ADR 0014 §3 (batch = the cap); the
    vocabulary is `CONTEXT.md` §Parallel agents.
- **A delegation can carry a name.** The `sub_agent` tool now takes an optional `name` argument
  beside `task` — a short label for what that child is for, normalised to a trimmed first line.
  It is display identity only, never privilege (ADR 0005), and it stays optional: a delegation
  that names nothing behaves exactly as before, with every display falling back to the delegated
  task's first line.
  - **The name travels with the child to every place that asks on its behalf.** An Approval a child
    raises and a question it puts through `ask_user` both carry it — `domain.ApprovalRequest` and
    `domain.AskRequest` gained an additive `SubAgentName` beside `SubAgentTask`, with
    `domain.WithSubAgentName` / `domain.SubAgentNameFromContext` as the ask path's carrier — and a
    headless run reports it on `run.SubAgentUsage.Name`. All three are additive fields: an unnamed
    delegation leaves them empty, which is the signal to fall back to the task.
  - **The collapsed run header leads with the name.** The `Sub-Agent` block's target — the text
    beside the label — is the delegation's name when the call gave one, clipped and escape-stripped
    exactly as a task line is, so a fan-out of concurrent children reads as what each is *for*
    rather than as several openings of one instruction. Saved sessions replay it with no format
    change: the name is already the header's finished text, so it rides the record's existing
    target and the transcript wire gained no member.
  - **The status line says WHICH delegate is working, and the prompt panes say which one is asking.**
    The left slot's phrase takes the acting child's name in place of the generic word —
    `repo-scout · reading · main.go` where it used to read `sub-agent · reading · main.go` — resolved
    per frame from the event's spawning call against that run's header, so with several children
    running at once the slot names the one whose event it is showing. The approval pane's and the ask
    box's `Sub-agent:` line leads with the name and keeps the task behind it
    (`Sub-agent: repo-scout — audit the config loader`), under the clip those lines already had,
    spent now on the whole line so a named prompt is never longer than an unnamed one. A delegation
    that named nothing renders byte-for-byte as before in all three places.
  - **`apogee headless` reports each delegated run by its name.** The per-run stderr line —
    `sub-agent: 12k/32k · repo-scout` — now leads with the delegation's name where the call gave one,
    on the collapsed run header's rule and under the same clip the task label already had, so a
    headless fan-out reads as which child filled which window. An unnamed delegation prints the
    delegated task exactly as before.

### Changed

- **A collapsed tool call is one row shorter: the `+N more lines` count moved into the row itself.**
  The remainder a collapsed block counts — the output, the diff, the record of an answered question
  — used to hang on a line of its own under the branch. It now rides the outcome slot at that
  branch's right edge, after the middle dot the stats already speak in: `go test ./... ⋯ exit 0 ·
  +3 more lines`. A lone `Terminal`, `Ask User` or `Diff Preview` call therefore collapses to its
  header and exactly one row, and a scrollback of them reads as a list rather than as a wall. The
  count wears whatever the slot wears, red included on a call that failed, and an expanded block
  drops it, having nothing left to count. On a row too narrow to seat the count beside a target
  still worth reading, the count is what gives way first — the `▶` at the edge says there is more
  either way. The row it left behind was also the one place in the transcript where a click could
  only ever *open*; with it gone, every row of a block toggles, wherever the pointer is.

- **A tool row's outcome now stands out from the dots that lead to it.** The right-aligned summary
  at the end of a tool row — `12 lines`, `exit 0 · 1.2s`, `+8 −3`, `[1 files found, showing 1-1]` —
  used to borrow the same faint gray the row's target and body wear, which under the shipped `dark`
  scheme was the very tone the leader dots run in: the one part of the row that says what *happened*
  read as filler. It now wears the `tool-marker` role, the same "apogee is talking" tone the
  `+N more lines` marker carries, and steps up to the new `tool-marker-bright` while the block is
  open, so an opened block lifts as a whole. Every kind of summary takes it, promoted one-line
  outputs included; a failed call's red still overrides everything and remains the only marking a
  failure leaves on the row.

- **New `tool-marker-bright` scheme role**, the open-block step of `tool-marker`, shipped in both
  schemes and written by `/color-scheme export` like any other role.

- **The tool rows' dotted leader is now defined beside every other glyph.** The `⋯` the leader is
  built from sat on its own in the leader-row code; it now lives in the one glyph block the branch
  marks, the bullets and the table rules already share, so changing the character is a single edit
  in one known place. Its colour was already a scheme role of its own (`tool-leader`). Nothing on
  screen changed.

- **The `sub_agent` tool now tells the model it may delegate several times in one reply.** The
  parallel fan-out has been there since the per-server Parallel agents cap landed, but nothing on
  the model-facing surface said so, and a model that does not batch tool calls of its own accord
  never tried: a live run dispatched every phase of a multi-agent workflow one call per turn, so the
  cap never engaged and independent sub-tasks ran end to end. The description now says outright that
  sub_agent may be called several times in a single reply and that siblings run concurrently, so
  independent work is dispatched together. Nothing about the schema, the name or the dispatch path
  changed — only what the model is told about them.

- **`docs/layout/tool-layout.md` is the canonical tool-block spec now, and `layout.md` points at
  it.** The two `layout.md` sections that carried the tool-block shape — §"The rules behind the
  tool-call sketch" and §"Collapsed and expanded blocks" — keep only the grammar every block obeys
  (the row budget, the colour roles, workspace-relative paths, what a body may quote, the blank line
  between blocks) and hand the row shape, what groups, the fold states, the interaction and the
  per-tool table to the canon spec; where the two disagree about a tool block, the spec wins. The
  sketch opening `layout.md` was redrawn to what the renderer paints today — leader rows with the
  outcome at the edge, the renamed labels, the `see less…` footer — and the `internal/tui` doc
  comments that cited the moved prose now cite the spec. The spec itself records the four cells
  that shipped differently from its ratified table (Title-Case labels, no durations, `ask_user`'s
  answer in the slot, `git_diff_range`'s three-dot range).

- **The prompt legend advertises `⇧⏎` only where `⇧⏎` actually works.** That chord needs the
  enhanced (kitty) keyboard protocol: on a terminal that has not negotiated it, shift+enter is
  folded into a plain `⏎` and the "newline" the legend promised sends the message instead. The empty
  box now starts on the honest legend — `⏎ send · ⌥⏎ newline · ↑ recall · ⌃c quit` — and upgrades
  itself to `⏎ send · ⇧⏎/⌥⏎ newline · …` within the first frames on terminals that answer the
  protocol query. `⌥⏎` is byte-distinct and works everywhere, and `⌃j` remains a third, undocumented
  fallback; the shift+enter binding itself is unchanged, so nothing that worked stops working.

- **A collapsed tool block now stands at most four rows: its header, two rows of target, and the
  count of what it hides.** A long command used to soft-wrap over as many rows as it needed and then
  spend one more previewing the first line of its output, so a scrollback of tool calls read as a
  wall of text whose height depended on which tool filled it. The target is clipped to two rows now,
  ending in a `…` that says it goes on, and no line of the body is painted at all — the marker
  beneath counts the body whole, so `+5 more lines` over a five-line output means exactly that. The
  marker's wording lost its leading ellipsis for the same reason: a cut row says its own
  continuation, and the marker counts only what never got a row.
  - Everything the collapsed shape withholds is still one click away, and a block whose TARGET alone
    is cut now wears the expand indicator too — a long path with no output at all is something to
    open. Because the cut depends on how wide the block is being painted, so does the indicator: the
    same call in a wider window shows its target whole and offers no toggle.
  - **The targetless shapes spend the same budget.** An MCP call shown as its verbatim arguments, a
    registered call whose target argument never arrived, a stray result — there the branch lines are
    the block's content, so two of them show, each clipped to a single row, above the same
    `+N more lines` count. One overlong argument line used to soft-wrap the block down the screen,
    and now cannot; a block whose lines the width alone cuts wears the expand indicator like any
    other. Scheduled Firings and a sub-agent run's collapsed head follow, so every shape wearing a
    tool block stands at most four rows collapsed, whatever filled it.
  - **The whole block takes the click now, not just its header row.** Every row a tool block paints
    folds it and unfolds it — the header, the clipped target rows beneath it, and, once it is open,
    the full target and every line of output — so a reader who has finished with a `Run`'s output
    closes it by clicking the output rather than travelling back up to the header. The prompt block
    has always worked this way; the tool block now does too, at every depth and under every glyph
    that borrows its shape. The `+N more lines` marker is the one row that keeps its own meaning: it
    exists only in the collapsed paint, so a click there can only ever open. A block with nothing
    behind it is a target on none of its rows, and as everywhere else in the transcript a drag still
    selects — only a motionless click toggles.
  - **The expand/collapse indicators are now `▶` and `▼`**, replacing the smaller `▸`/`▾`.
- **Consecutive same-label tool calls now group even when they carry output.** Grouping used to
  admit only calls with nothing hanging beneath them, so a batch of reads folded into one aligned
  block while a batch of `Run`s or edits — the batches that fill the most screen — stayed a column
  of separate four-row blocks. They fold now, and the block says how many: the header reads
  `✦ Run (3)`, the count in the faint chrome tone rather than the label's gold.
  - **A grouped member is exactly one row.** The target is clipped to a trailing `…` at the room
    its outcome leaves it, and the outcome is never traded away — a member shows what happened to
    the file even when the path had to be cut. Every summary in a block opens in the same column,
    clipped targets included. A member with something to reveal wears a `▶` flush against the
    block's right edge; the field is reserved on every row, so the indicators line up down the
    edge. Ten grouped calls read as ten lines.
  - **The group header itself is inert** — no indicator, no click — because expansion belongs to
    the members, each with its own state.
  - **A grouped member opens on its own.** Click anywhere on a member's row and that member alone
    unfolds: its target in full, its output verbatim beneath a `│` gutter, and a right-aligned
    `see less…` closing it, while its siblings stay one row each and the block keeps its single
    header. Its `▶` becomes a `▼` in the same column, and every row the open member paints — the
    target rows, the body, the `see less…` — closes it again, so the pointer never has to travel
    back to where it started. The gutter is painted in the detail gray and never in the sub-agent
    rail's gold, so an opened member inside a delegate's run cannot be read as part of the run's
    frame. As everywhere else in the transcript, a drag still selects: only a motionless click
    toggles.
  - **An open block reads a step brighter.** A tool block paints its target, its outcome and its
    output in a lighter gray once it is expanded, so the one block a reader opened stands out of the
    closed ones around it instead of being one more dim paragraph in the column. It is one step and
    the same hue — an open block should be easier to read, not a different kind of thing — and it
    reaches the block's text alone: the `▶`/`▼`, the `+N more lines` marker and an open member's
    `│` gutter stay chrome, and a diff's red and green stay exactly as they were, since that colour
    says which way a line went rather than how loudly to say it.
  - **An answered question's record still stands alone**, and so does a delegation. The record was
    kept out of a group by the body it carries, which is no longer a reason for anything; the block
    now says outright that it must not fold into its neighbours, and says it across a save and
    resume too. A `Sub-Agent` block says the same, because it heads a whole run rather than being
    one of a batch — including the run that came to nothing, so two refused delegations in a row
    stay two blocks.

  The row budget, the grouping scope, the per-member state and the click surface are specified in
  `layout.md` (§"The rules behind the tool-call sketch" and §"Collapsed and expanded blocks"),
  rewritten from the sketch in `docs/layout/tool-layout.md`.
- **llama-launcher is now a property of the server it fronts, not of apogee.** The top-level
  `llama-launcher:` key is **retired**, and the setting moves onto the `servers:` entry the launcher
  starts servers for. A global key made a claim about the whole config that stopped being true the
  moment the list held more than one server: on a machine with the launcher installed, `/model`
  answered from the launcher's side on *every* entry, so a session switched to a remote provider was
  offered this machine's Launch profiles instead of the hundreds of models that server advertises.
  Now the integration **follows the session** — `/model` offers Launch profiles while you are on the
  entry that names a launcher, and the moment you `/server` somewhere else the same verb goes back
  to what that server advertises. Coming home is the two steps it looks like: `/server <name>`, then
  `/model`.
  - **The key has three shapes on an entry.** Absent means no launcher for that server — `/model`
    lists what it advertises and `/unload-model` / `/stop-server` answer
    `llama-launcher not configured` — and that is the default every remote entry wants.
    `llama-launcher: auto` reads the launcher's own default config
    (`~/.config/llama-launcher/config.yaml`), and a path reads that config instead (`~` expands).
    Nothing is checked at startup, as before: a config that is not there is reported the first time a
    command reaches for it, naming the path.
  - **`auto` is written by hand, where the old key auto-detected by existence.** The retired key lit
    the integration up on its own whenever the launcher's default config happened to exist, and
    stat-gated exactly because it was silent; an entry saying `auto` is already an explicit choice,
    so the file is read when a verb asks for it and named in the error if it is missing. There is
    also no per-entry `off` — absent *is* off, and a config offering two spellings of one state only
    invites them to drift, so an `off` value is refused with the fix ("remove the key").
  - **A `/model` load keeps the launcher it just used.** Activating a Launch profile can move the
    session to an endpoint no entry names; that move leaves the integration on, so the next `/model`
    still answers from the launcher's side. A `--endpoint` override start carries no launcher, and a
    launcher on another machine is unchanged — reach that one as an `mcp-servers:` entry pointing at
    the launcher's MCP adapter; the two still compose.
  - **A config still setting the top-level key is refused at startup, with the fix to paste.** The
    error names the file and the line, and prints the complete `servers:` entry to put the value on
    (an old bare `llama-launcher:` — the auto-detect shape — pastes as `auto`; an old `off` needs
    only the deletion, since an entry with no key already has the launcher off). The refusal runs
    before the retired-schema fold does any work, so a file is never half-rewritten and then
    refused, and no `config.yaml.bak-*` is left behind by a start that did not finish. Nothing
    migrates itself: this is a pre-production schema break, and the paste-able fix is what is owed
    instead of a shim.
  - **The `llama-launcher` row leaves `/settings`.** With the setting inside the `servers:` block it
    is edited where that block is edited — `⏎ opens $EDITOR` — so there is no top-level row to
    apply live. Recorded as an amendment to ADR 0029 decision 4 and a note on ADR 0037.
- **A context size never spills out of its unit again — one formatter now spells every one of
  them.** The rule is that the displayed number never reaches a thousand: past `999k` the spelling
  steps up rather than running on, so a million-token window reads `1M` and never the `1048k` it
  used to. The ladder is **binary** — 1024 to a rung, plain `k`/`M`/`G` suffixes — because the
  windows themselves are powers of two and the models are named for them, so `131072` reads `128k`
  the way its model card does; the price of that choice, taken deliberately, is that a decimal round
  number no longer reads round (`128000` is `125k`). `k` is a whole number (`2k`, `32k`, `977k`),
  `M` and `G` carry one decimal only while it says something (`1.1M`, against `1M` and `15M`), and
  an unknown size still spells as nothing at all, so a cell with no reading behind it disappears
  instead of painting a `0`.
  - **Every display site goes through it**: the status line's context gauge, the startup box, the
    sub-agent block's `12k/32k` cell, the rebind and heartbeat notes, the `— 32k` gloss in the
    `/model` and `/server` pickers, the standing-content Budget warning, and `apogee headless`'s
    `sub-agent:` lines on stderr. Three hand-rolled helpers — a coarse and a fine one in the TUI,
    plus the coarse one's twin, deliberately duplicated in the CLI half because the TUI's was
    unexported — are gone, replaced by the shared `internal/format` package both halves import.
  - **Rounding is half-up now, where the old helper truncated**, so `1999` reads `2k` rather than
    `1k`. Sizes that were already round mostly read the same (`32768` was and stays `32k`); the ones
    that move are the ones that were being rounded down or spelled past their unit.
  - `apogee probe host` is deliberately **not** in the sweep: it is a diagnostic and prints the exact
    number the server reported (`context window 131072`). File sizes keep their own `KiB`/`MiB`
    spelling — different domain. The rule is written down in `layout.md` (§"The status line's right
    slot").
- **The `+N more lines` marker is orange under `dark` now.** The remainder marker beneath a
  collapsed tool block used to be a light gray-blue — the prompt block's `see more` tone carried
  onto a body column — and the shipped `dark` scheme paints the `tool-marker` role a warm `#FFB050`
  instead, so the row that counts a hidden body stands clear of the dim text around it. Only the
  value moved: the marker keeps its own role, its no-background, no-bold-weight treatment and its
  one-click open, the `light` scheme keeps its blue, and a scheme of your own that sets
  `tool-marker` still overrides whatever ships.
- **Docs housekeeping: superseded docs move to their `archived/` homes, and skill-run scaffolding
  leaves the repo.** The `2026-08-03 - 00 - readme-drift-fix` handoff (both spots it named were
  fixed in the 2026-08-05 README rework) is now under `docs/handoffs/archived/`, and the
  `prompt-box-layout.md` mockup — fully redundant with `layout.md` — under a new
  `docs/layout/archived/`. `docs/skill-runs/` is gitignored: the per-run prompt copies and ledgers
  the orchestrator skills write are machine junk with a run's lifetime, so a finished run's dir is
  deleted rather than committed.
- **`AGENTS.md` and the README now read true against the working tree.** The agent guide's
  distribution bullet had stood still since v0.11.0 — it now names the Homebrew tap and the six
  prebuilt archives per release beside build-from-source, keeping the never-`go install …@latest`
  warning — and the `CHANGELOG` + `VERSION` bullet describes the practice actually followed
  (per-feature `VERSION` micro-bumps, release headings only at a release cut). In the README: the
  Status line reads `v0.12.x`, "Newest on `main`" names colour schemes, parallel sub-agents and the
  watched `config.yaml` instead of the scheduling wave, the archive example uses the current
  release, the `make check` row names the ADR-0010 import invariant and the `--help` smoke, the
  `make install` search order matches `INSTALL_CANDIDATES` (`~/.local/bin` before
  `/opt/homebrew/bin` — the Makefile's own comment carried the same inversion and is fixed too),
  the `make dist` tool claim no longer says everything but `zip` ships with Go, and a short
  "Reading the code?" pointer sends contributors to `AGENTS.md` as the single map.
- **`CONTEXT.md` says what the code says on three points.** The mid-run `/command` policy now names
  the Schedule pair beside the reporting verbs (`/schedule` and `/schedule-stop` are safe while a
  worker works because they touch only the scheduler library, ADR 0033); the network tools are
  spelled `web_fetch`/`http_request` as the tool registry names them; and the post-tool-result hook
  point names `error_enrichment` as its resident, with `correct_tool_result` marked deferred
  (owner-ratified 2026-07-04, a bench-side experimental hook until a production trigger is found).
  The two code comments carrying the same staleness — `internal/tui/command.go`'s `safeWhileRunning`
  and `internal/domain/mechanism.go`'s `PostToolResultHook` — are corrected with them.
- **The layout specs read to the renderer's truth again.** `layout.md`'s opening paragraph had the
  right-gutter arithmetic off by one and contradicted its own scroll-bar section (one free column
  beside a painted bar, two to the window edge while the gutter is blank); the scroll-bar key is no
  longer described as "fixed for the run" now that `/settings` applies it to the running session
  (ADR 0037) and re-lays the frame out; the sketch at the top carries the current prompt-box legend
  (`⏎ send · ⌥⏎ newline · ↑ recall · ⌃c quit`) and a `Sub-Agent` run drawn in its railed shape with
  the `N tool calls · <used>/<window> · gist` head; and the autocomplete section lists all six verbs
  that take arguments rather than four. `docs/layout/user-questions-layout.md` strikes the
  `[1]`/`[2]`/`[3]` digit shortcuts (ratified out 2026-08-04), draws the always-painted hint row on
  the multi-option mockup, and lowercases the approval body's `command:` label. The two renderer
  comments riding the same drift — `internal/tui/theme.go`'s `bodyRightGutter` and
  `internal/tui/autocomplete.go`'s accept-behaviour note — are corrected with them.
- **The Mechanism catalogue lists all 21 Mechanisms again.** `guided_decomposition` had been
  registered in code since 2026-07-05 without a row in `docs/design/mechanism-catalogue.md`, so the
  authoritative map listed twenty while the code built twenty-one and the README promised that many.
  It now carries its Table A identity row (pre-request steer plus post-response fan-out
  follow-through, `After toolfilter`, `Requires tool_result_cap`, incompatible with `decompose` and
  `truncate_history`, depth-0 and once-per-Exchange gates), a Table B verdict marking it the first
  catalogue row that is *not* a port, and a ledger line whose bench validation is the ADR 0009 gate
  over the `guided_decomposition` + `tool_result_cap` stack. The `truncate_history` row's F7 note no
  longer reads as a half-recorded edge: the declaration lives on one descriptor, but either side
  naming the other fails start-up, so the two can never be enabled together whichever row you read.
- **Three decisions that were only ever recorded in design drafts are now ADRs.** The dependency
  policy becomes **ADR 0042** — one static CGO-free binary, every external program (`git`, a Python,
  the Go toolchain, the formatter ladder) a runtime-detected enhancement that degrades to a named
  result, an in-process rung wherever one is possible (`grep` needs no ripgrep; autofix keeps its
  `go/format.Source` tail), and exactly one bounded exception in macOS `sandbox-exec` for Auto-mode
  confinement, which costs a mode rather than the agent. **ADR 0001** gains the hook-mutation
  discipline it always depended on: hooks read `Message` value snapshots and edit by index
  (`SetMessageContent`/`Insert`/`DropRange`/`Replace`), never the loop's backing slice, and
  `Message.Content` stays a string with unknown wire structure preserved in `Extra`. **ADR 0022**
  gains the `ToolOutcome` rationale — why a committed tool result carries its own `ok`/`error`
  verdict as an `omitempty` snapshot sibling, why text sniffing survives only as the pre-marker
  fallback anchored to the result's first line, and why neither needed a `SessionVersion` bump.
- **`docs/design/` now holds only live contracts.** With their "why"s salvaged into ADRs, the two
  design drafts that were never ground truth move to a new `docs/design/archived/`: the Phase-1
  `technical-design.md` scaffold (frozen at its 2026-06-23 vintage, self-declared
  non-authoritative) and the P0.1 `hook-mutation-api.md` draft. Each opens with a tombstone naming
  what replaced it, and the technical design's §5 component rows are declared closed — component
  narration stops there, so no future plan amends them. What remains in `docs/design/` is the three
  contracts that are still binding: the confinement execution contract, the MCP client, and the
  Mechanism catalogue. Live citations (`TODO.md`, four in the catalogue) point at the new paths;
  code comments citing section numbers are left as the history references they are.
- **`TODO.md` is live work again, and `ISSUES.md` carries one new gap.** The `/server` persistence
  bullet is gone — its three non-goals were subsumed, not added, when the `servers:` list became the
  single definition (ADR 0036) — and the four struck-through width-authority narratives are
  compressed to closed-trail one-liners that keep their standing constraints (the surviving
  `lipgloss.Style.Width` site is ADR 0030 §6's widget-mirror exception; the widget mirror's taller
  counts need no clamping change). Stale code references are refreshed: `Request.InjectContext` and
  `AppendToSystem` in `internal/domain/hooks.go`, `ConfineWritablePaths`' two readers in
  `internal/agent/dispatch.go`, `domain.AskRequest`'s depth gap (ADR 0039 gave it the delegation's
  task and name, still not its depth), and the parked `schedule` tool's daemon trigger, which now
  has a shape to build against (ADR 0034). `ISSUES.md` loses its one closed line and gains the gap
  the doc-landscape audit surfaced: an *approved* out-of-workspace write reaches the Gate but still
  errors at Execute, because the write tool's os.Root fence never learned to honour an approval —
  the confinement contract's §4 "unreachable row" is only half true, and closing it either way is an
  owner decision.
- **The two kept design contracts read true against the code again.** The confinement execution
  contract's §6.1 pinned harness signatures gain the `Shell` third parameter the probes actually
  take (taken from the caller so the battery runs natively on Windows), §6.2's shell-line anchors
  point at the per-OS `confinetest/lines_other.go` and `lines_windows.go` instead of line numbers
  inside `confinetest.go`, and §4's "the out-of-workspace row is unreachable" paragraph is rewritten
  to the half-landed truth: the row is now reached (dispatch classifies with
  `resolveTargetUnbounded`) while the `Execute`-side fence still refuses an approved escape, the
  open `ISSUES.md` entry it now cites. `mcp-client.md` picks up the Resolution vocabulary — the
  retired `dispoGate`/"disposition table" becomes `resolveGate` and the Resolution ladder
  (`internal/agent/resolution.go`), and the D5 gate is named Resolution, not disposition.
- **`cmd/apogee` has a package map.** The composition root was the last sizable package in the repo
  without a `doc.go`: 25 files, no map, and a package comment that named the binary's job but not
  where any of it lived. It now meets the standard every `internal/*` package already does — the
  role of the layer, then every non-test file in one line, grouped the way the package actually
  divides (entry and command surface, the config cluster, the two `/settings` seams, the session's
  wiring, the subcommands, the confined-exec twins). The narration that used to sit on `main.go`
  opens it unchanged, so the package still has exactly one package comment.
- **A follow-up pass over four residues the doc sweep itself left.** `ISSUES.md` no longer says the
  confinement contract still calls the out-of-workspace row "unreachable" — that paragraph was
  rewritten in this same sweep, and the entry now cites what §4 actually says; `README.md` counts
  the Mechanism catalogue honestly, twenty ports plus apogee's own `guided_decomposition`, instead
  of calling all 21 ported; the `whileRunning` flag's doc covers the Schedule pair that carries it
  (they write to the scheduler library, never this session's engine) instead of promising every
  mid-run verb only reports; and `layout.md`'s opening mockup draws the EXPANDED `Sub-Agent` head
  with the report's own summary rather than the collapsed run's count line, with one overlong prose
  line reflowed and the approval pane's renderer comment spelling its argument label as the spec
  does.
- **File-level structure is now a written rule — [ADR 0043](docs/adr/0043-files-split-by-concern-and-config-gets-a-package.md).**
  ADR 0010 settled the layout above the file and said nothing below it, which is how a 4,743-line
  `model.go` and a 2,932-line `wire.go` grew inside a package layout the doc audit called
  exemplary. Four calls close the gap: a coordinator file splits by concern cluster and keeps only
  coordination; a composition root splits into `wire_<seam>.go` files with a file-top map naming
  every seam; the config cluster (core, writer, migrate, watch, key registry, the resolved options)
  becomes `internal/config` while the /settings display projection stays in the binary, because the
  schema, the precedence and the masking are the binary's knowledge and the renderer holds none of
  it (ADR 0011); and a package past ~10 non-test files carries a `doc.go` file map that a test
  enforces, so the navigation aid cannot rot. Nothing about behaviour, ownership or layering moves —
  `internal/config` imports `internal/domain` and never root, `internal/tui` stays flat, and the
  "the binary owns it" stances of ADRs 0024 and 0028 stand.
- **The transcript renderer is nine files instead of one.** The tool-row work above grew
  `internal/tui/render.go` to ~2,760 lines, so it was cut along the seams its painters already
  had: the transcript walk stays in `render.go`, and the sub-agent umbrella, the user block, the
  start-up box, the tool block and its super-group walk, the leader row and its promote-guard, the
  collapsed-block state, the branch rows and their details, the wrapping and depth-rail primitives,
  and the two frame-arithmetic helpers each get a file named for what it holds. A pure move —
  every line verbatim, nothing renamed, no behaviour changed — with `doc.go`'s file map extended
  to name all nine, which is what the ADR-0043 guard checks.

### Fixed

- **The model you configure is the model you get, even when the server does not list it.**
  Discovery used to match `model:` against `/v1/models` by exact id and, on no match, silently bind
  the server's FIRST advertised model instead — so a hint that is perfectly valid on the wire but
  absent from the list ran someone else's model without a word. It is trusted now: the configured id
  always goes out as configured, and the advertised list only supplies the context window. An exact
  entry supplies it as before; a variant slug such as `deepseek/deepseek-v4-pro:exacto` inherits the
  window of its base entry (the part before the first `:`), which is exactly the OpenRouter case
  that surfaced this — variants are served but never listed. An id that matches nothing at all still
  runs, with the window unknown, which leaves the Budget and auto-compaction inactive exactly as an
  advertised model that reports no window does, and lets a genuinely wrong id fail loudly on the
  next request instead of quietly serving a different model. An empty model list is no longer fatal
  when something is configured to run.
  - **A session bound to a model that is not advertised now says so, once, at startup**:
    one transcript note naming the id and what the window came to — the base entry it was inherited
    from, or unknown with the Budget and auto-compaction it costs. An advertised model, and a start
    with no `model:` at all, stay silent.
  - Per-model configuration keys on the id you configured. `system-prompt-models:`, the
    Validated-set identity and the `apogee probe model` record are all keyed on the RESOLVED model,
    which is now the full configured id rather than whatever the server happened to list first —
    write per-model entries against the id you wrote in `model:`.
- **An upstream failure can no longer arrive as a silent empty reply.** OpenAI-compatible
  aggregators deliver a provider's failure *in band*: an HTTP 200 whose JSON body — or whose SSE
  stream — carries `{"error": {…}}` and no usable choices. apogee's wire structs ignored that
  member, so the 200 read as a success with nothing in it, and the loop committed a blank assistant
  turn with no error anywhere on screen; the only trace was the retry a minute later that happened
  to come back as a real `HTTP 429`. Both paths parse the member now and surface it at once. A
  streamed one ends the stream on a terminal error carrying the whole raw event — code, message and
  the provider detail an aggregator packs into `metadata` — passed through the same redaction and
  length cap every other upstream body gets, so an API key in the payload never reaches the screen.
  A non-streamed one returns the same `*StatusError` an out-of-band status returns, so every caller
  that already branches on a status keeps branching. An in-band `400` whose message names a context
  overflow is classified as an overflow rather than a generic fault, which puts it in front of the
  history fold that answers the out-of-band form. There are no hidden re-requests: the server has
  already decided, so an in-band error is terminal.

- **An empty upstream reply now fails the turn visibly instead of committing a blank message.** A
  reply with no visible text and no tool calls is a non-answer, and it used to land in the
  transcript and in the saved session as an empty assistant message, indistinguishable from a model
  that had nothing to say. The loop emits the same visible error a stream fault does — naming the
  finish reason, which is usually the whole diagnosis — and commits nothing. The guard sits after
  the post-response hooks have resolved, so the `empty_response_recovery` Mechanism keeps first
  claim: its retry runs first, and when that retry produces content the guard never fires. It is
  engine-level, so it holds in Bypass too, where no Mechanism is watching — failure honesty is
  provider correctness, not something you should have to enable. A thinking-only reply counts as
  empty; reasoning nobody asked to see is still not an answer.
  - **Self-regulation no longer reads an empty reply as harm.** An empty final response was one of
    the two harmful proxy signals a Turn could raise for the effectiveness judgment. Now that it is
    an upstream fault rather than something a Mechanism could have shepherded, the signal is dropped
    rather than re-homed, and a tool-result error is the harmful proxy on its own. Recorded in
    `CONTEXT.md` §Self-regulation.

- **A rate-limited retry waits as long as the server asked it to, and backs off a full second when
  it was not asked.** A 429's `Retry-After` was ignored outright: apogee re-asked after 200ms and
  then 400ms, three requests into a limit that wanted seconds — which is how a short throttle
  becomes a longer one. The header is honoured now in both its forms, delta-seconds and HTTP-date,
  up to a 30-second cap; a wait longer than the cap gives up immediately and surfaces the error
  instead of sitting blocked on what is effectively a ban, and it costs no retry budget on the way
  out. Every wait is cancellable, so ending the turn ends it rather than sitting out the delay.
  Without a header a 429 backs off from a 1-second base — 1s, then 2s — while transport faults and
  5xx keep the 200ms base they always had.

- **A resumed session no longer folds two delegations into one `✦ Sub-Agent (2)` block.** A
  sub-agent call heads a whole delegation and never becomes a row in someone's list, and the live
  presenter says so. That verdict rides the wire, but a transcript saved before it existed carries
  nothing for it, so replaying an older record put two span-less heads — two delegations refused at
  the depth bound, with no nested entries to give the painter's span rule anything to see — under a
  single counted header. The decoder re-derives the verdict from the record's tool name now, so an
  older blob replays exactly as a freshly presented one does: one block per delegation.

- **…and no longer folds two answered questions into one `✦ Ask User (2)` block either.** The same
  gap, one record over: an answered `ask_user` block keeps the permanent record of an exchange — the
  question, the ticked choices, the answer — and reads as a card rather than a row in a list of
  questions, so it never groups with its neighbour. A transcript saved before that verdict rode the
  wire carried nothing for it, and replaying two answered questions in a row put both under a single
  counted header, hiding each record behind the other. Unlike a sub-agent head, a name does not
  settle this one — a question still awaiting its answer is an ordinary pending call and still
  groups, and so does one whose result came back an error, which never got to keep a record at all.
  So the decoder reads the record's own footprint instead: answered, with the exchange beneath it.
  A question that failed to reach anyone now replays grouped beside its neighbour exactly as it
  painted live, instead of splitting into a block of its own on the next reload.

- **A saved transcript now records whose words a tool block's summary line is.** A summary is either
  the block's own wording — a typed phrase, an `error: …` line — whose paths are spelled relative to
  the workspace, or a line promoted onto the branch as it stands (a one-line command output, the
  answer typed into an `ask_user` question), which is quoted content nothing may respell. The live
  presenter decides that when the line is made, and the wire form had no member for it, so every
  replayed block came back claiming the block's own words. Nothing reads the mark after a decode
  today, so no painted row changes either way — this is the record keeping a verdict it was
  silently dropping, so the next seam to consult it is not misled. Additive: a transcript written
  before the member decodes exactly as it does now.

- **"Allow for session" now covers the whole agent tree, so sub-agents stop re-asking for what you
  already granted.** The memory of an allow-for-session was kept per `Agent`, and a sub-agent is a
  new one: a delegation-heavy run in Auto could ask for the same shell command or the same MCP tool
  once per child, which is precisely the prompt fatigue the choice exists to end. The memory is now
  the **Session's** — one cache on the approver queueing seam, the single object a parent and all
  its descendants already share — so "allow for session" means the Session and not the one Agent
  that happened to ask. An allow granted anywhere in the tree clears that prompt everywhere,
  including for the parent and for siblings started later, and it outlives the child that earned
  it. It stays in memory only: a restored session starts with an empty memory, which errs toward a
  prompt too many rather than an unapproved call.
  - **A duplicate approval queued behind the one that answered it auto-clears instead of asking
    twice.** Siblings in a fan-out reach a gate at the same instant (ADR 0039), so the second one
    has already checked the memory — and found nothing — by the time the human allows the first.
    It re-checks after it reaches the front of the queue and clears itself, emitting its ordinary
    approval event with an allow-for-session verdict so the transcript still says why a prompt you
    were expecting never appeared.
  - **Forced gates are untouched.** A Tier-2 speed-bump or a runtime demote still asks every single
    time and still seeds nothing: it carries no cache key, and the seam keys everything off that, so
    a forced allow-for-session authorises its own call and nothing later — exactly as before.
    Recorded in [ADR 0013](docs/adr/0013-the-sub-agent-orchestrator-is-the-recursion-point-with-isolated-live-guard-state.md)
    (amended), whose statelessness boundary no longer withholds this cache from a child; parent
    conversation and pending input stay withheld as they were.

## [0.12.0] — 2026-08-07

### Added

- **`apogee headless` — one prompt, one unattended run, printed to stdout.** Give it a prompt as a
  quoted argument or pipe one on stdin, and it runs that prompt to completion with nobody watching,
  prints the answer, and exits with a status a script can act on: `0` the run completed, `1` the run
  started and failed (model or tool error, cancellation, a record that would not save), `2` the run
  never started (usage, configuration, a refused mode). That is apogee's first distinct-exit-code
  convention, and it is opt-in — every other command still exits `0` or `1` exactly as before. The
  command is a thin CLI over the same shared runner a `/schedule` firing uses (`internal/run`) —
  argument parsing and exit codes, not a second agent — which makes it the second Driver over the
  embeddable engine and the tripwire ADR 0031 named for it: a capability welded to the TUI now
  breaks visibly here. See ADR 0033 (decision 6) and ADR 0034.
  - **Only the answer goes to stdout.** Resolution notices and the closing
    `session: … · turns: … · denied: …` summary go to stderr, so a pipeline reads the model's text
    and nothing else. The answer is stripped of terminal control characters on the way out — the
    ESC that opens an ANSI sequence, and the bare BEL or CR that would ring the bell or rewind the
    line without one — while its own newlines and tabs come through untouched.
  - **Two modes, because the other two exist to consult a human.** `--mode` takes `plan` (the
    default, read-only) or `auto`; `ask-before` and `allow-edits` are refused before anything is
    composed. `auto` is refused outright on a host whose confinement backend cannot fence the
    filesystem — the cell where an interactive run quietly falls back to asking, and an unattended
    one has nobody to ask — and prints the launch's own warning, word for word, when confinement has
    been switched off entirely.
  - **Settings resolve exactly as a session's do** — flag over `APOGEE_*` environment over
    `config.yaml` — so the run has the shape a session on this host would have, Mechanisms and
    system prompt included. It is saved to `~/.apogee/sessions` and is browsable in `/sessions`
    beside the conversations it ran next to; `--no-save` runs it and records nothing. There is no
    `--api-key` flag, for the reason there is none anywhere else: a secret on the command line lands
    in shell history and in `ps` output.
  - **Nothing waits for a human.** Every gated action is refused rather than parked — the refusals
    are the `denied:` count, never the exit code — `ask_user` and `present_document` are not
    registered, and no MCP server is contacted. `Ctrl-C` and `SIGTERM` end the run rather than the
    process, so an interrupted run still prints what it reached and still saves its record.

  Documented in `README.md` ("Running one prompt") and defined in `CONTEXT.md` ("Driver", "Firing").

- **A question can now take more than one of its answers.** The model asks with
  `multi_select: true` on `ask_user` when several of the choices could apply at once — "which of
  these findings should I fix?" — and every offered answer gets a `[x]`/`[ ]` box in front of it.
  `↑↓` still move the highlight, `␣` ticks and un-ticks the row you are on, and `⏎` sends every row
  you ticked, one answer per line and in the order they were offered. Nothing was added to the
  bottom of the list to press: the key that answers every other prompt answers this one too.
  - **`⏎` with nothing ticked still sends the highlighted answer**, so a question you want to answer
    with one option takes exactly the keypresses it always did.
  - **Typing still writes your own answer.** The offering hides the moment the box is non-empty and
    `⏎` sends only what you typed; delete back to empty and the offering comes back with your ticks
    exactly as you left them.
  - **Single-answer questions are untouched** — on the wire, on the screen, and at the keyboard. The
    flag is optional and off by default, `␣` on such a question still just types a space into a
    custom answer, and the reply to a single choice is byte-identical to the one Apogee always sent.
  - **The boxes are a column, not padding.** They line up down the pane through the same pop-up
    column machinery the approval prompt's shortcut letters use, and an answer too long for the
    width wraps under its own text rather than under the box beside it.

  Defined in `CONTEXT.md` ("Ask-user") and specced in `layout.md`, with the pinned mockup in
  `docs/layout/user-questions-layout.md`.

- **`/settings` — your whole configuration on one screen, and every change takes effect in the
  session you make it in.** The verb opens the first **full-height** pane apogee has: one row per
  setting, in the
  order the starter `config.yaml` documents them and grouped under section headings, each showing
  the value *this run* resolved for it — with `(env)` or `(flag)` on the rows a higher-precedence
  source beat the file at, and `••••` where the upstream `api-key:` would be. Thirty-odd keys are a
  screen to read rather than a choice to scan, so the transcript gives way entirely while it is up;
  `↑/↓` move the `❯`, a fixed two-line `Description:` header above the list says what the key under
  the cursor is for, section labels stand in white above the rows they open, the row being typed
  into is lit, and the mouse works where the keys do — a click selects a row, the wheel walks the
  list. `esc` closes. It is
  idle-only, and it is the first surface from which apogee writes your config at all.
  - **An edit is persisted the moment you commit it, one key at a time.** `⏎` toggles a true/false
    row, opens a selection popup on a row with a fixed set of values, opens a buffer on the row for
    a string
    or a number, or opens a multi-line field for the inline system prompt (`⏎` makes a new line,
    `ctrl+s` saves, `esc` discards). Both are real fields: cursor keys, `home`/`end`, word jumps, a
    paste that lands in the field you can see rather than in the chat box behind the pane, and a
    mouse that seats the caret, drags a selection and — in the multi-line field — walks the prose
    under the wheel, exactly as in the prompt box. A value row folds a pasted line break onto its one
    line; the system-prompt field keeps every line you gave it. What lands
    in `~/.apogee/config.yaml` is a **line splice**: your comments, your
    layout and every other key untouched, the result re-parsed and compared against the original
    before it replaces the file, written atomically. A key that was still one of the file's
    commented examples has its active line inserted directly below that example, where the
    documentation for it already is. A value the key cannot hold — a port out of range, an endpoint
    with no host — is refused before anything is written, with the reason on the row and your text
    still in the buffer.
  - **And what is saved is applied, on the same `⏎`.** No setting waits for a restart and no row
    says "(next launch)": the key is routed to whatever puts it into effect — an engine setter, an
    idle rebind, or the wiring that owns it — so the next thing apogee does uses it. The row keeps a
    ` *` after its value (`false *`), which says *you changed this here, this session*, and it is
    cleared only by a relaunch. The `context-files:` pair is the one that lands at a boundary
    instead, because it is part of the prefix every request is cached against, and the row says so:
    `· applies at next clear`. Where an environment variable or a flag outranks the file, the edit
    still applies and is still written, and the row adds that the override wins again at your next
    start; if a write lands but the live apply refuses it, the row says exactly that
    (`saved — live apply failed: …`).
  - **`backspace` unsets a key**, arming a reset the hint line asks you to confirm with `⏎`. What
    that removes is the key's **line**, not its value: the setting goes back to following the
    built-in default rather than being pinned to today's spelling of it — and that default is
    applied on the same keypress, the row reporting it as `default *`. The one row it does nothing
    on is `server:`, whose line is the *recording* of a switch rather than a value the pane writes —
    removing it would leave the session running against a server the file no longer names — so the
    hint line drops the key there and choosing a server stays the way that key changes.
  - **The blocks no row can hold open your editor.** `servers:`, `mcp-servers:`, `mechanisms:`,
    `validated-sets:`, `system-prompt-models:` and the model profile carry an `· ⏎ opens $EDITOR`
    pointer, and that is what `⏎` does: apogee suspends into `$VISUAL`, else `$EDITOR`, else `vi`
    (`notepad` on Windows), with the cursor on that key's line where the editor takes one. On return
    the file is re-read, validated, and every key that changed is applied the way an in-pane edit is
    — a changed `mcp-servers:` **reconnects**, dialling the new set first and swapping the tools over
    only once it is up, so a server that will not come back leaves the old connections serving and
    the reason on the row. The jump is offered between runs only. The confinement keys keep
    `· use /confine`, because switching Auto's fence off asks for an acknowledgement that stays
    with that verb — and the `server:` row now performs the full **live switch**, the same move
    `/server` makes, from the same list and recorded the same way; picking the server you are
    already on switches nothing and the row says `· already on <name>`, since the transcript where
    `/server` would have said it is behind the pane.
  - **The pane cannot drift from the schema.** It renders from a new declarative key registry in
    the binary — one row per configuration key, with its kind, default, sources, editability and
    one-line description — and a reflection guard pins that registry to a bijection with the config
    struct, so a key added to the config without a registry row fails the build's tests rather than
    quietly going missing from the screen. Settings resolution now reads each key's environment
    variable and flag name from the same rows, so source metadata has one home.

  Ratified in ADR 0035 (the persistence contract) and ADR 0037 (the live apply, which supersedes
  0035's mode-only decision), defined in `CONTEXT.md` ("Settings surface") and specced in
  `layout.md` ("What 'height' means", where the full-height pane class is written down) with the
  pinned mockup in `docs/layout/settings-screen-layout.md`.

- **You can see how much context a sub-agent used.** A collapsed sub-agent run's summary line now
  states the delegate's own context fill between the call count and the gist —
  `4 tool calls · 12k/32k · found the caller in wire.go` — spelled in the same whole thousands the
  status line's gauge uses, so the two numbers on screen read as one language. It ticks as the
  child's turns land and freezes on the run's final reading, so a finished block goes on saying what
  it filled. Until now a sub-agent's usage reached every sink and every consumer dropped it.
  - **It is the delegate's fill, not a share of yours.** A sub-agent inherits the parent's context
    window verbatim, so the reading is that agent's own window filling up; it does not move the
    status line's gauge, and — unlike the call count beside it — it is **not transitive**: a nested
    run's reading rides the nested block and never accrues to the run above it. The chrome still
    carries exactly one gauge.
  - **The figure survives a resume.** It is stored on the transcript entry when it folds, limit
    included, so reopening a session shows each finished run's final figure rather than a blank.
    Sessions recorded before this existed decode to no reading and simply show the line they always
    did — the cell hides itself whenever either half is missing, the same condition the gauge hides
    itself on.
  - **`apogee headless` reports the same thing per run.** Before its closing summary it prints one
    stderr line per sub-agent run, in finish order — `sub-agent: 12k/32k · review the wire seam` —
    the task being the delegated prompt's first line, stripped of control characters and clipped:
    every C0 control and DEL is dropped as it is from the answer, except the newline and the tab,
    which fold to a space here so the label cannot forge a second line or a false column. Runs with
    no reading are skipped, stdout stays answer-only, and the summary line itself is unchanged.

  Closes the standing issue "I cannot see how much of its context a sub agent has used"; specced in
  `layout.md` ("A sub-agent run collapses to its call block") and defined in `CONTEXT.md`
  ("Sub-agent").

- **`⌃l` forces a full repaint.** The readline meaning of the key, and here it is a repair tool: the
  renderer paints each frame as a diff against its own model of the screen, so a terminal that has
  smeared or eaten part of the frame keeps showing that damage until something marks the whole screen
  dirty. `⌃l` is that something — the same resync a window resize performs, without resizing. It is
  live in every state, it sends nothing to the model, and it changes nothing you can lose except a
  drag-selection highlight (which every keypress drops, as it always has).

- **`apogee probe terminal` measures the terminal instead of trusting it.** A third subject beside
  `apogee probe host` and `apogee probe model`, and free like the first: it writes real escape
  sequences to your terminal and reads the answers back, then prints what it found — what it says
  about synchronized output and grapheme clustering (modes 2026 and 2027), how many cells it really
  advances for an emoji or a combining sequence with that mode off and on, where its tab stops are
  and whether a tab erases what it passes over, what the terminal does when a glyph lands in the last
  column (a pending wrap or an immediate one), and the
  capabilities it really has beside the ones the renderer assumes from `TERM`. Sections that
  disagree are marked `MISMATCH` and set the exit status, so the command can be checked by a script
  and not only read. It needs a real terminal on both stdin and stdout and says so when it does not
  have one; no model is called and nothing is written.

- **Two hidden diagnostic flags for arguing about a rendering bug: `--tui-trace` and `--tui-diag`.**
  Each takes a file path, each is off unless named, and each costs nothing when unset. `--tui-trace`
  records the exact bytes the renderer wrote, one quoted Go string per write, so a corrupted frame
  can be replayed through a virtual terminal rather than only described; `--tui-diag` records what
  the terminal told the program about itself — the environment the renderer read, the width method
  it started on, the window size, the colour profile and every mode report, each written once and
  again only when it changes. They are portable rather than Windows-only, because those
  are the two artifacts any rendering bug is argued from on any OS. They stay out of `--help`: they
  are for a bug report, not for a session.

### Changed

- **An answered Ask User block keeps a record of the question, the choices and what was picked.**
  The popup that put the question takes the offering off the screen with it, so the block left
  behind said only what the human answered — and of a several-line answer, only its FIRST line: the
  rest reached the screen nowhere at all. The answered block now carries the exchange beneath its
  branch: every line of the question as it was put, one line per offered choice behind `[x]` or
  `[ ]` with the given answer(s) ticked, and any answer line no choice accounts for. It becomes an
  ordinary expandable block on the strength of that body — one record line while collapsed, the whole
  record when opened. The branch line itself is unchanged, still the human's own words quoted and
  never respelled, and a question still waiting for its answer is untouched: while it is up the popup
  IS the live view of the offering, and the record materialises when the answer lands.
  - **Nothing crossed the wire for it.** The question and the choices are the model's own call
    arguments and the answer is the result the tool already returned, so the record is a render-time
    act on what the transcript was holding anyway — no tool result grows, no token is spent, and the
    engine stays wire-silent (ADR 0031).
  - **The model is told to stop restating the question.** `ask_user`'s description now ends by
    saying never to repeat the question or the choices in the message before the call: the popup
    puts them to the human and the transcript keeps the record afterwards, so a preamble spelling
    them out again buys nothing and spends tokens.

- **Breaking (config), with a one-time automatic migration: the `servers:` list is the single
  definition of what apogee can talk to.** The top-level `endpoint:`, `api-key:`, `host-alias:` and
  `model:` quadruple is retired from the schema — one `servers:` entry now carries all of it as
  `name` (the label `/server` lists it under, the argument `/server <name>` takes, and the host
  alias the footer shows), `endpoint`, an optional `api-key` and an optional `model` hint. A session
  starts on the entry a new `server:` key names, the third key with all four precedence layers
  beside `mode:` and `bypass:` (`--server`, `APOGEE_SERVER`).
  - **A `/server` switch records where you left off.** Every move onto a listed entry splices
    `server: <name>` back into `config.yaml` through the settings writer — that one key, comments
    and layout untouched — and the move's own line says `· server: saved` when it did, so the next
    launch starts where the last session ended. A move onto a server the list does not name (an
    `--endpoint` override, a llama-launcher profile) has no name to record and writes nothing;
    a write that fails lands as a note under the move, which stands either way.
  - **The first run asks instead of refusing to start.** With `server:` unset — or naming an entry
    that is gone, which is now stated and survivable rather than fatal — the TUI starts
    **pre-bound**: no engine is constructed, the `/server` picker opens by itself over your entries,
    and the choice both builds the session's engine and records the key. With no entries configured
    at all it opens `/settings` instead and points back at `~/.apogee/config.yaml`. Construction is
    merely deferred, never attempted without an endpoint, so ADR 0024's `errMissingEndpoint` posture
    is untouched. Only the TUI can ask a human: `apogee headless` and `apogee probe` refuse a
    startup with no determinable server instead, naming the config file and the fix.
  - **Raw overrides still run one session elsewhere.** `--endpoint` / `APOGEE_ENDPOINT` builds an
    ephemeral unlisted entry that wins over any name and is never persisted, taking its bearer token
    from `APOGEE_API_KEY` and its model hint from `--model` / `APOGEE_MODEL`; with no endpoint
    override those two variables overlay the corresponding fields of whichever entry the session
    starts on. They are no longer config keys at all, so `/settings` no longer carries a row for
    them — the `servers:` block is one summarized, file-edited row, and with it the pane's last
    masked value is gone.
  - **An old config migrates itself, once, with a backup.** The first read of a file still carrying
    the quadruple folds it into a `servers:` entry plus a `server:` pointer: the original is copied
    to a timestamped `config.yaml.bak-YYYYMMDD-HHMMSS` sibling first, comments and unrelated keys
    survive, the rewrite is re-parsed and compared against the original before it replaces the file,
    and one startup line names what moved and where the backup is. A fold that cannot be made safely
    — no endpoint among those keys, a name the list already uses, a `server:` already set — writes
    **nothing**, leaves the apogee home exactly as it found it, and refuses with a ready-to-paste
    replacement block. A config already in the new schema is never touched.

  Ratified in ADR 0036 (amending ADR 0028 and ADR 0035's write set), defined in `CONTEXT.md`
  ("Upstream", `/server`) and documented in `README.md` ("The servers you run models on").

### Removed

- **`/skill` — the two-step picker verb is gone.** Naming a skill is what invoking it already is:
  type its `/<skill-id>` anywhere in your message, or pick it from the merged `/` menu, which lists
  every skill beside the commands and splices the same token. The separate "pick a skill by name"
  verb was a second route to the one destination since ADR 0027 made skills inline tokens, and a
  bare `/skill` now earns the same `unknown command or skill` refusal as any other unknown verb. A
  skill whose id collides with a command verb is still reachable — the merged menu hides it, but the
  `/id` token typed mid-message resolves as it always did.
  - **The `menuOnly` command flag left with it**, and took a display bug with it: `/skill` was the
    only verb that carried the flag, so it was also the only row that wore a `— idle only` tag in
    the `/` menu while the model worked even though picking it there worked fine. No surviving verb
    is menu-only, so the tag now says what it means everywhere it appears.

### Fixed

- **A double-width tool target no longer pushes the context gauge off the status line.** The left
  slot caps the target it shows so the gauge always has room beside it, and that cap was spent in
  RUNES while the screen bills CELLS: a CJK path or a filename carrying an emoji is 32 runes the
  terminal pays up to 64 cells for, so the phrase outgrew its budget, the whole row was then
  truncated against the window, and the gauge the cap exists to protect was gone from an 80-column
  line. The cap is now measured through the width authority — the same measure the painter is
  using — so 32 means 32 columns whatever the glyphs are, ellipsis included. A plain-ASCII target
  under the cap is unchanged. The tab half of the same defect was already fixed; this closes the
  rest of it.

- **The TUI drops every control character from untrusted text, not only the ESC byte.** The
  transcript's sanitizer is the seam every model-, repo- and disk-supplied string passes on its way
  to the screen, and it removed the ESC that opens an ANSI sequence while letting the rest of the
  class through: a bare BEL rang the terminal bell, a CR rewound the line so what followed overwrote
  what you had already read, and a NUL or a DEL took string length while occupying no display cell —
  the same lie to the column math that an unstripped ESC tells the pane. Streamed model text, a
  stored session file and a picker row's label all arrive through it, so any of the three could
  scramble a frame. The whole C0 class goes now, DEL with it, while the newline and the tab a body is
  wrapped and railed by come through untouched. `apogee headless` closed the same gap on its own
  output; this is the other half of it.

- **A sub-agent's live reply no longer appears in the main transcript before folding into its run
  block.** While a delegate was generating, its streamed text painted as a top-level assistant
  block in the parent's own voice, outside the collapsed run that was producing it, and jumped
  inside only when the child's final message landed. The transcript kept a single streaming buffer
  that never recorded whose tokens had filled it, so the preview had nowhere to paint but depth 0.
  The buffer is routed by depth now: the live preview paints at the agent's own level — railed
  under `⤷ sub-agent` in an expanded run, and elided with the rest of the span in a collapsed one,
  where the blinking header and the status line's `sub-agent · responding` already say a delegate
  is talking. Two leaks out of that same shared slot went with it: a delegate that ended without a
  final message — cancelled, or faulted mid-turn — left text the parent's next event committed at
  the top level for good, and a delegate re-streaming its turn wiped whatever the parent had
  streamed. The buffer is committed at the depth that filled it and discarded only by the level
  that owns it.

- **`apogee probe` and `apogee probe model` print their report to stdout.** Both reports travelled
  on stderr, so `apogee probe > host.txt` wrote an empty file and put the whole report in the
  terminal — the command's product now goes to stdout, where a redirect or a pipe can take it, while
  the preamble, the record warnings and every other notice stay on stderr exactly as before.

- **`/new` and `/clear` are no longer recorded as recallable prompts.** Walking back with `↑` could
  land on the session reset you typed earlier, one `⏎` away from wiping the conversation again —
  recall exists to re-send a line, and that is the one line where re-sending is never what you
  meant. The session-reset pair is now sent like any other command and recorded like none of them,
  in memory and on disk. Everything else you send stays recallable, `/version` and the other
  whole-line commands included.

- **The Windows TUI no longer ghosts.** Under Windows Terminal and VS Code's integrated terminal,
  fragments of an earlier frame survived every repaint: streamed text arrived corrupted, the
  activity spinner left a trail behind it, scrolling smeared, and only resizing the window put the
  screen back. The cause was apogee's own start-up ordering. A Windows console screen buffer rewrites
  every bare line feed as carriage-return + line feed unless it is told not to, and the renderer
  means two different things by the two: a bare line feed asks for the next row *at the same column*.
  The flag that turns that rewriting off is set per screen buffer, and apogee switched to the
  alternate screen before it was set — so the flag landed on the buffer nobody was painting to, every
  such row was painted from column 1 instead, and the cells the renderer believed it had just
  overwritten were never touched. apogee now prepares the console before it takes the alternate
  screen, and hands the shell its own console mode back on the way out. A classic conhost window
  never showed the bug and is unchanged; nothing changes on macOS or Linux. Recorded in ADR 0038.
  - **The painter is told what terminal it is talking to.** Windows shells leave `TERM` empty, and
    the renderer reads exactly that variable to decide what the terminal can do — so it believed it
    could not address a column at all, which is what turned any single error into permanent
    corruption instead of one bad frame. apogee now names `xterm-256color` (and `truecolor`) to the
    renderer alone, only when nothing else has named one and only on a real terminal: the process
    environment is untouched, so no tool call or child process inherits it, and a `TERM` you or your
    WSL/MSYS shell set is never overridden. Colours are unchanged.
  - **Synchronized output is declined on Windows.** Measured out of a real pseudoconsole, the
    terminal forwards apogee's begin/end pair back to back as an empty window and re-serializes the
    frame after it has closed — so the frame was never presented atomically, while asking for it cost
    the cursor-hiding that keeps a repaint from flickering. apogee stops asking there, and keeps
    asking everywhere else, where the answer is honoured.

## [0.11.0] — 2026-08-05

### Added

- **The prompt box remembers what you have sent, and `↑` walks it back.** Press `↑` on an empty box
  and the newest prompt you sent in this workspace lands in it with the caret at the end; keep
  pressing to step further back, `↓` to come forward, and one `↓` past the newest empties the box
  again — the terminal's own gesture, so a long instruction is edited and re-sent rather than
  retyped. It works the same while the model is running, where `⏎` queues what you recalled as an
  interjection.
  - **The arrows go back to the caret the moment you touch anything else.** The walk owns `↑/↓`
    only while the box holds a freshly recalled line you have taken no other action in; typing,
    editing, pasting, clicking in the box or resizing the window ends recall mode there and then,
    and a draft you typed yourself never starts a walk at all. Under an ask, `↑/↓` still move the
    choice highlight — recall is not offered there.
  - **A recalled `/command` does not pop the suggestion menu.** That menu claims `↑/↓` before recall
    ever sees them, so a walk would be stolen by its own first entry; loading an entry dismisses the
    pane instead, and it comes back the moment you act — which is the same moment the arrows do.
  - **What is recorded is what you sent**: ordinary messages, whole-line `/command` invocations, and
    interjections. Answers to the model's own questions are not — they answer it rather than speak
    your input. A prompt is recallable the instant it is sent, without waiting for anything to be
    re-read.
  - **One file per workspace, under your apogee home** — `~/.apogee/prompts/<digest>.jsonl`, owner-
    only (`0600`), never anything written into the project tree. Consecutive duplicates collapse,
    the newest 1000 entries are what a start-up load hands back, and the file compacts itself rather
    than growing without bound. Recall is a convenience and never a failure: an unreadable file
    costs you the walk and nothing else.

  Defined in `CONTEXT.md` ("Prompt recall") and specced in `layout.md` ("The prompt box's
  mini-language").

- **A prompt taller than three rows collapses to three, with a `see more (+N lines)…` toggle on the
  last of them.** A pasted spec or a long instruction used to bury everything around it under a wall
  of your own text — every wrapped row of it, every time you scrolled past. Now the block paints its
  first three rows, truncates the third far enough to make room, and counts what it is holding back
  at the right edge; a click anywhere in the block opens it whole, with a
  `see less…` row closing it again. Dragging across a prompt still selects text exactly as before:
  only a motionless click toggles.
  - **Collapsed is the default, always** — the prompt you just sent included, and every prompt of a
    resumed session. Whether a block is open is a way of LOOKING at the scrollback and is never
    written to the session record.
  - **The trigger is measured at paint time against the width being painted**, so widening the window
    can open a prompt that was collapsed and narrowing it can collapse one that was not. The entry
    itself never changes — the same body is simply laid out to a different column count.
  - **Interjections (`⧖`) collapse by the same rule** — a remark delivered to a working model is
    the same shape in the scrollback as a prompt you sent at idle.
  - **The sticky header shows the block as it is painted**, so a collapsed prompt sticks as its
    three-row shape and a deliberately opened one sticks open — undone by the same one click.

  Specced in `layout.md` ("Collapsed and expanded blocks").

- **The transcript's scroll bar has a switch — `ui.show-scrollbar` in `config.yaml`'s `ui:` block.**
  It ships on, which is the look apogee has always had: a column down the session area's right-hand
  edge, and a bar painted in it once there is more transcript than window. Set it to `false` and the
  column goes away **with** the bar rather than staying behind as an empty stripe — the transcript
  text takes that width back and wraps to the window edge instead, still short of it by the free
  column it always keeps. Scrolling itself is untouched: what the key hides is the indicator, never
  the movement. Config-file only, like the rest of the `ui:` block, and read once at start-up, so
  the width the transcript wraps to is exactly as fixed for the run as it was before.

  Specced in `layout.md` ("The scroll bar and the column it hangs in").

- **The top rule wears the session's name — `▔▔▔▔ the name ▔▔▔▔`, centered on the hairline above the
  status line.** A screen full of panes said nothing about which conversation each one held; now the
  frame itself does, on a row it paints. The name follows every route that can set one — the automatic
  naming call, either form of `/rename`, the `/sessions` browser's `r` on the live session, and a
  resumed record's own name — and until one of those has spoken it shows the same first-prompt
  heuristic the first save stamps, so the rule is named the instant you ask for something. It carries
  no live state by design: the row says *which* conversation this is, and everything that changes has
  a home in the chrome below it.
  - **A name is clipped only when the rule has no room for it.** There is no fixed maximum — three `▔`
    cells and a space stay on each side, and the name gets everything between them, whole, however
    long it is, measured in the cells the terminal really paints rather than in runes. A short name in
    a wide window simply keeps its long `▔` runs, and a window too narrow to show a name honestly goes
    back to a plain rule.
  - **An unnamed session gets the plain unbroken rule** — no `apogee` placeholder, because the frame
    is already unmistakably apogee's from every other row — and `/clear` returns the row there with
    the session it starts.
  - **It costs the frame no row.** The `▔` hairline that caps the bottom chrome was already there and
    already spanned the window, so the name rides a row the transcript had paid for. It is the route
    that replaced the terminal window title added earlier the same day, which is withdrawn: it reached
    no tab or window on any terminal tested.

  Specced in `layout.md` ("The top rule wears the session's name").

- **Tool blocks collapse and expand — click a header, or its `… +N more lines` marker, to see
  everything a call actually returned.** The compact block is unchanged and it is still what you
  get by default: a body shows its first line, whatever tool filled it, with the
  `… +N more lines` remainder counting the rest. What changed is that the rest is still *there* —
  the transcript retains every line of every body and the caps became a **paint-time act** — so a
  **motionless click on a block's header** opens it in full, a second click closes it again, and a
  click on the remainder marker opens the block that body belongs to. Dragging is untouched:
  motion is what separates a selection from a click, header lines included, exactly as it already
  does in the prompt. After a toggle the line under the cursor **keeps its screen row** — the block
  grows or shrinks below it — whether the view is riding the tail or scrolled up.
  - **A sub-agent run collapses to its call block.** Collapsed — the default, including while it is
    still working — a `Sub-Agent` call stands alone and its whole railed span is elided, every
    inner block, `⤷` label and rail with it. Its summary line carries the run in two tempi:
    `4 tool calls · reading main.go` while it works, ticking as inner calls land, then
    `4 tool calls · ` plus the report's first line once the report arrives. The count is
    **transitive**, so one number says how much work happened in there however deeply it nested.
    Expanding reveals the inner blocks *in their own states* — a nested run stays collapsed inside
    an expanded parent, because this is one rule applied at every depth and not a special case.
  - **A live block's star blinks, half a second on and half a second bare.** While a block still
    holds a call whose result has not landed — or a run whose report has not — its header glyph
    shows `✦` for half a second, then leaves its cell bare for half a second: a space that holds the
    star's column, so the label beside it never shifts. The phase rides the spinner's own tick, so
    work in progress is visible without the transcript carrying a timer of its own, and the
    transcript is repainted only on the tick that actually flips the phase — two repaints a second
    while something is open, and none at all while nothing is. It settles to `✦` when the result
    lands.
  - **The header says which state it is in — and that it can be clicked at all.** A header whose
    block has something to reveal now trails its label with `▸` while the block is collapsed and
    `▾` while it is expanded, painted in the faint detail tone so it reads as chrome beside the
    tool's name. A header that hides nothing wears neither, which makes clickability visible for
    the first time: the indicator and the click target are one rule, so if you can see it you can
    click it. The `… +N more lines` marker gained a tone of its own to match — light gray-blue, the
    quieter sibling of the prompt block's `see more` — so a body line that happens to start with
    `…` is never mistaken for the affordance beneath it.
  - **No block is exempt any more, targetless calls included.** An unregistered or MCP tool prints
    its arguments as the block's own branches, and a 60-line argument blob used to stand
    60 rows tall forever — the one shape that never collapsed. It now caps at the same budget as
    every other block, with the same `… +N more lines` marker and the same one-click toggle, and so
    do a registered call that arrived without its target and a stray `result` that matched no call.
    Nothing is lost: the approval popup is where you approve an action and it still shows the
    verbatim arguments at decision time — the transcript block is the record, and a record may
    collapse.
  - **And an unrecognised call's block labels its arguments, exactly as the approval prompt does.**
    The block for a tool the transcript has no presenter for — an MCP tool, anything not yet
    registered — used to paint the raw JSON it arrived in, so the same call read one way in the
    popup you approved it in and another way in the record of it. It now uses the one rendering:
    a `name:` line per argument in the order the model wrote them, the value's own real lines
    beneath it, no braces around the set and no quoted key names. What each surface shows is only
    how MANY of those lines it seats — the prompt shows them whole, the block collapses them to the
    house budget. Arguments with no names to label are still shown exactly as they arrived, and
    every value prints as the model sent it, absolute paths included.
  - **One budget, and the diff spends it too.** A collapsed `View Diff` stood up to 23 rows tall —
    twenty hunk lines under its header, branch and marker — the last body with an allowance of its
    own, and by a wide margin the tallest thing a collapsed transcript could hold. It now keeps the
    same single line as every other block, so no collapsed block anywhere is taller than four rows
    and a wall of blocks reads as a list of what happened rather than a wall of diff. Nothing is
    lost: the `+2 -2` on the branch still counts the whole change, and one click shows every hunk.
  - **An edit shows the lines it changes.** A find-and-replace or an `edit_existing_file` reported
    only that it had happened — `┕ main.go replaced text in main.go` and nothing else — so the one
    thing worth seeing, *what* changed, was the one thing the transcript never showed. Every edit
    block now hangs the changed lines beneath its report, red for what goes and green for what
    arrives, per replacement and in the order the call listed them. Collapsed it is the same four
    rows as every other block, and one click shows the whole change. The lines are read off the
    call's **own arguments**: the model already said what it wanted changed, so nothing extra is
    sent to it or asked of it and the tokens a session spends are untouched. Consecutive edits now
    stand alone rather than grouping into one block, on the standing rule that a call carrying a
    body breaks a run.
  - **A write shows what it writes.** `write_file` reported a byte count and nothing else, so a
    file the model created was a number in the transcript. The block now hangs the content beneath
    its branch, every line of it green, with the `+25 bytes` still riding above — the count says how
    much, the lines say what. It is read off the call's **own arguments** exactly as an edit's is,
    collapses to the same four rows, and an empty write still shows nothing, because it wrote
    nothing.
  - **The state is the view's alone.** It is never encoded with the transcript: a resumed session
    paints everything collapsed and `/clear` forgets it with everything else. A block that hides
    nothing has nothing to toggle — a group of body-less calls, or any block whose lines already
    fit the collapsed budget — and a click there keeps its ordinary selection meaning. Toggling is
    mouse-only for now, on the same precedent that keeps transcript selection mouse-only; a
    keyboard block-cursor is its own future feature.

  Specced in `layout.md` ("Collapsed and expanded blocks").

- **A new session names itself from your first prompt — and `/rename` sets or re-asks for a name.**
  A saved session was listed under the first line you typed, cut at 50 characters, so the
  `/sessions` browser read as a wall of half-sentences. Now the first thing you say in a fresh
  session also goes out as one small extra completion — that prompt, your workspace name and the
  date — and the reply becomes the session's title, so the browser lists `fix the escape handling in
  the session picker` instead of `hey, could you take a look at why the escape se…`. The naming call
  goes to the **same server and the same model the session is already on**, so there is nothing new
  to configure and no second endpoint to point anywhere.
  - **It costs one short request and nothing else.** The call is cosmetic, never structural: it is
    not a Turn, it fires at no hook point, it never shapes what the model is actually answering, it
    never reaches the transcript and it never moves the context gauge. It goes out **in parallel
    with your first message**, so on a single-slot server it queues behind that answer and lands in
    the gap before you say the next thing — the point in a session where the context is smallest and
    an eviction costs least. If the call fails, or the reply has nothing usable in it, the title
    apogee derived from your prompt simply stands and **nothing is said**: a maintenance nicety must
    never nag.
  - **`/rename` is the manual half.** `/rename <name>` sets the title outright; bare `/rename` asks
    the model for one. Unlike the automatic call this verb always speaks — every outcome, refusals
    included, lands as a transcript note, because a command you typed that quietly did nothing is a
    bug in the interface. A name **you** set is never overwritten by an automatic title that arrives
    late, and naming a session *before* it has said anything works too: the title waits for the
    record and is applied the moment one exists, overriding the derived name. Bare `/rename` is an
    explicit request, so its answer applies even over a name you set a moment ago — and it still
    works when automatic naming is switched off.
  - **One sanitizer, for the model's reply and for what you type.** A leading `<think>…</think>`
    block is stripped, the first real line is taken (code-fence lines a small model wraps its answer
    in count as noise, not content), and ANSI and control escapes, surrounding quotes or backticks, a
    leading `//` or `#` marker and a leading `Title:` label all come off; inner whitespace collapses,
    a trailing period goes, and the result is word-boundary truncated to 50 runes — the same cap the
    derived title always used. Nothing left after that counts as a failure and is handled as above,
    which is also why a pasted name cannot smuggle an escape sequence past what a model's reply
    could not.
  - **One new file-only key, `auto-title:`, default on.** `auto-title: false` stops the automatic
    call firing and nothing else — `/rename` keeps both of its forms, bare included. Automatic naming
    happens once per new session record, including after `/clear` and `/new`, and never on a session
    you resumed, which already has a name.

  See the 2026-07-31 addendum to
  [ADR 0022](docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md).

- **Markdown tables in an answer now render as a table.** A pipe table the model emitted used to
  fall through to the plain-paragraph path: every row was word-wrapped on its own, the columns
  lined up only as far as the model's own source padding happened to carry them, the `|---|:---:|`
  delimiter row appeared verbatim as a line of visible punctuation, and a row wider than the chat
  soft-wrapped into a second one. The transcript now draws it: **borderless aligned columns** —
  each cell padded to the widest in its column, columns two spaces apart, no verticals and no
  frame — with a **bold header** over one continuous `─` rule running the full width of the table,
  gutters included, and the delimiter row's alignment markers honoured (`:--` left, `--:` right,
  `:-:` centred). Cells are rendered as inline markdown first, so `**bold**` and `` `code` `` inside
  a cell style exactly as they do in a paragraph, and it is that rendered width — not the source
  width — that sets the column, so markup characters and colour escapes never push one open. Every
  line of the block — header, rule and rows alike — ends in the same column, so the table shows one
  straight edge to the scroll-bar gutter beside it and a drag selection addresses the same columns
  the eye does.
  - **It never overflows the chat.** Where the columns plus their gutters do not fit, the widest
    column is shrunk until they do and an over-wide cell is cut with a `…` tail; where even
    single-cell columns cannot fit, the block falls back to the plain paragraphs it rendered as
    before, which always fits. One row is always exactly one line — no cell wraps in this version.
  - **A half-arrived table is just text.** A table becomes a table on the row that completes it —
    the header row plus a valid delimiter row beneath it — so everything streaming in ahead of that
    reads as ordinary paragraphs, the same contract every other half-typed construct keeps. Columns
    go on measuring themselves as rows arrive, so a table may widen as it streams.

  Plain text and prose that merely *contains* pipes are untouched, byte for byte. The renderer stays
  hand-rolled and dependency-free — no markdown library joins the build — and the visual contract is
  specced in `layout.md` ("Markdown tables in assistant text").

- **`/model` brings the server up too — plus `/unload-model` and `/stop-server`, for freeing its
  model or shutting it down without leaving the session.** `/server` moves between servers that
  are already *running*; this makes one **exist**. Apogee now imports **llama-launcher** — the separate tool
  that stores Launch profiles (which model file, which server, under what flags) and knows how to
  start llama.cpp, Ollama and LM Studio — as a library, and drives it from your conversation.
  Configure a launcher and `/model` answers from its side: the picker lists the **Launch profiles**
  the launcher's config defines, in the launcher's own order (favourites first), each row carrying
  the backend, the context window the profile configures, `· running` when that profile is live
  right now, and the port when it is somewhere other than where this session is pointed; picking
  one starts what it takes to serve it. `/model <name>` activates one by name, and a host without a
  launcher keeps exactly the `/model` it had — what the server advertises. `/unload-model` frees
  the model of the server this session is on and says whether that also stopped it — on a managed
  llama.cpp server the model is baked into the process, so it does. `/stop-server` stops that
  server, after which the footer's ordinary offline handling narrates the rest. All three are
  ordinary rows in the `/` menu: the two that act on this session's own server say so in their
  names, which is what keeps them safe to offer.
  - **The launcher actuates, the heartbeat observes.** Apogee grows no process manager of its own:
    it asks the launcher to change the world, and the next ten-second beat binds whatever it finds
    — the same single path a model changed from the server side already travels. A profile that
    resolves to another server moves the session there, conversation and all (the `/server` fold,
    with the profile's name as the host label); a profile on the server you are already on moves
    nothing at all, and the beat rebinds the model under you.
  - **Narrated, not modal.** A load blocks while the server comes up, so each launcher step lands
    as a transcript note as it happens and the footer's model slot shows `loading <profile>…` until
    the beat completes the move. One actuation runs at a time — while one is in flight, a send and
    the other switching commands are refused with a single line — and there is no mid-flight
    cancel: `/stop-server` is the cancel, available the moment the verb returns. A beat that fails
    *during* an actuation counts nothing toward the offline crossing (the server is expected to be
    down mid-restart), and a health wait that times out is not a dead end: the launcher deliberately
    leaves the server running and names its PID and log path, and apogee adds the honest coda —
    the heartbeat will bind it if it comes up.
  - **One new file-only key, `llama-launcher:`, and it usually needs nothing.** Unset means
    **auto-detect**: apogee reads the launcher's own default config
    (`~/.config/llama-launcher/config.yaml`) if that file is there, so a machine with the launcher
    installed needs no configuration. On a machine without one nothing is lost: `/model` lists what
    the server advertises, as it always did, and `/unload-model` and `/stop-server` answer
    `llama-launcher not configured`. `off` keeps the integration off on a machine that *does* have a
    launcher config; a path names a different one. Nothing is probed at startup (the `servers:`
    posture — a missing config is reported at the first command, never as a refusal to start), and
    every command re-reads the file, so a profile added in the launcher's own TUI is offered by the
    next `/model`.
  - **The dependency.** `github.com/airiclenz/llama-launcher v1.6.1`, a tagged release imported at
    the composition root only — the engine and the TUI see nil-degrading closures, never the
    library — so a bare clone still builds from source and no build tags enter apogee. It compiles
    on all three platforms: on **Windows** everything the launcher drives over HTTP works
    (discovery, loading and unloading models against Ollama or LM Studio, activating a profile on a
    server that is already up), while starting a managed `llama-server` or signalling one to stop
    reports a clean unsupported error. A launcher on *another* machine stays what it was — an
    `mcp-servers:` entry pointing at its MCP adapter; the two compose.

  See [ADR 0029](docs/adr/0029-the-launcher-actuates-local-servers-and-the-beat-completes-every-move.md).

- **`/model` — switch models without restarting anything.** Type `/model` and apogee lists what
  your server is actually serving right now, each with its context window, the model you are
  already on left out (a row that switches nothing is not a choice); pick another with ⏎ and the
  session is on it before the keypress is over —
  the footer, the start-up box and the context budget all move together, and the transcript says
  `model changed: A → B`. `/model <id>` switches straight away without opening the list. It is the
  same machinery the heartbeat already used to follow a model *you* changed from the server side,
  so there is no second way for a binding to move and nothing new to keep in step. When there is
  nothing to pick from, apogee says which reason it is — no monitor wired, the server not
  answering, nothing advertised yet, nothing but the model already bound — instead of opening an
  empty pane. On a host with llama-launcher configured the same verb offers that tool's Launch
  profiles instead (see above).

- **`/server` — move a running session to another server, conversation and all.** Name your other
  machines in a new `servers:` block and `/server` offers them (plus the one you launched against,
  always, so switching away is never one-way). Picking one re-points the session at that endpoint
  with its own API key: the footer flips to the new host and `connecting…`, and the new server's
  first heartbeat — fired immediately, not ten seconds later — binds whatever it is serving and
  announces `connected: <model>`. Your conversation, autonomy mode, approvals and confinement all
  survive the move untouched; only the things that actually describe a server change. Between the
  switch and that first bind a send is refused with the new endpoint named, rather than quietly
  going somewhere stale.
  - Each entry takes a `name` (the label in the list, the `/server <name>` argument, **and** the
    host name the footer shows once you are on it), an `endpoint`, and optionally that server's own
    `api-key` and `model` hint. `APOGEE_API_KEY` still belongs to the server you start on, so a
    keyed remote carries its own key in the file.
  - The switch lasts for the **session**: nothing is written back to `config.yaml` and the next
    launch starts at `endpoint:` again — the same posture `/confine off` without `--save` takes.
  - Unreachable current server? `/server` still works. Switching away is exactly what you want to
    do when the machine you were on stopped answering.

  See [ADR 0028](docs/adr/0028-a-server-switch-rehomes-the-session-and-the-first-beat-completes-it.md).

- **Embedders: `Agent.SwitchUpstream(UpstreamSpec)` and the `apogee.UpstreamSpec` alias.** The
  engine half of `/server` — bind a fresh provider client at another endpoint and key at a
  quiescent boundary, leaving the session with no model bound for the heartbeat's ordinary rebind
  to complete. Idle-only, like `Rebind`: it refuses mid-Exchange with `ErrInputPending`, so no
  request is ever re-pointed at a different server underneath itself. Additive surface — an
  embedder who never calls it is unaffected.

- **Skills are invoked by typing `/<skill-id>` straight into your message — one `/` namespace for
  everything.** A skill used to be a two-step ritual: `/skill`, pick from a list, watch a violet
  chip appear *above* the box, then write the message. Now you write
  `/code-audit please check the parser` and that is the invocation — the token sits in the text,
  the model sees it *and* the skill's instructions, and the transcript keeps an honest record of
  what you asked for. The `/skill <name>` picker is still there for browsing (it now types the
  token for you), and only a token your catalog actually knows counts: any other `/word` inside a
  message is ordinary text, so `/usr/bin` and dates travel untouched.
  - **One menu.** Typing `/` opens a single dropdown listing **commands and skills** together —
    commands first with their summaries, then skills marked with `✦`. A skill whose id collides
    with a command verb is reached through the `/skill` picker (the command wins the bare name).
  - **`/skills`** lists your discovered skills — id, display name and summary, re-scanned first, so
    a skill you added since launch shows up. With none installed it tells you the three folders
    discovery looked in, so "no skills" is a next step rather than a dead end.
  - **The menu works anywhere in the line.** Completion follows the token your **cursor** is on,
    for `/`, `@` and the picker alike — so you can start a message, reach for a command halfway
    through, or go back and fix a misspelled skill id, and get the same menu the end of the buffer
    gives you. Accepting splices over that token alone; everything on either side is untouched.
  - **Accepting a command runs it and keeps your draft.** Pick `compact` from the menu with half a
    message written and the `/compact` is cut out, the command fires, and the rest of what you
    typed is still there with the cursor where it belongs. Arguments still belong to the
    whole-line form (`/confine off --save`), which is unchanged.
  - **Skills and files light up as you type.** A `/token` turns violet exactly when it names a
    skill you have, an `@path` turns blue exactly when the file is in your workspace listing.
    Everything else stays plain — so a typo simply never lights, and you see it before you send
    instead of after. Nothing is read off disk to paint it.

  See [ADR 0027](docs/adr/0027-one-slash-namespace-with-inline-skill-tokens.md).

- **The `/` menu and the safe commands now work while the model is running.** Previously the
  command and skill menus vanished the moment a turn started — exactly when you are composing the
  next message — and every `/command` was refused with "commands run at idle". Now the menu opens
  in both states, and each verb carries its own policy: `/version`, `/skills` and `/confine`'s
  status report **run immediately** mid-run; `/clear`, `/new`, `/sessions`, `/compact`,
  `/continue` and `/confine off|on` still wait for a quiet engine, but their rows are shown
  **tagged `— idle only`** rather than hidden, and picking one prints the note and leaves your
  draft alone. Skill and file tokens are just message content, so they ride the queued message to
  the model like the rest of it.

- **apogee now reads your project's `AGENTS.md` — workspace context files.** The system prompt says
  what *you* always want said; this is what *this project* wants said. At the start of every session
  apogee looks for `AGENTS.md` in the workspace root and folds its content into the same first system
  message, after your prompt, under a `## Workspace context: AGENTS.md` header — so a repo that
  already keeps one for other agents is picked up with **nothing to configure**. A new file-only
  `context-files:` block is the control: `names:` lists the names to look for (**all** of the ones
  that exist are included, in your order — it is an inclusion list, not a fallback chain) and
  `enable: false` turns it off, as does an explicitly empty `names: []`. Names are looked up in the
  workspace root only — no walk-up, no global context file; standing text that follows *you* is what
  `system-prompt-text` / `system-prompt-file` already are.
  - **Discovery, so absence is normal.** A name that does not exist is skipped silently (one config
    travels across repos that carry different files, or none), and so is an empty file; a file that
    is **present but unreadable** is reported in the transcript and skipped, never a startup failure.
    A malformed *name* — absolute, `..`, empty, or listed twice — **is** a startup error naming
    `context-files.names` and the value, on every OS, so a travelling config fails where it was
    written.
  - **The content is data, verbatim.** The system prompt's placeholders do not apply to it: `{{…}}`
    in some repo's `AGENTS.md` reaches the model exactly as written and can never fail apogee's
    startup.
  - **Read at session start, not per request.** The files are re-read by `/clear`, `/new` and a
    `/sessions` resume, and **never** mid-conversation — so what the model was told is fixed for the
    whole session (your server's prefix cache survives it) and an edit to `AGENTS.md` lands on your
    next `/new`. Sub-agents inherit the parent's bytes. Nothing enters your history or a saved
    session.
  - **You are told what it costs.** Each loaded file is named with its size at every session
    boundary, and when the standing system content (prompt + context files) outgrows its share of the
    context window apogee says so — advisory only: nothing is ever capped or truncated.

  For embedders this is **additive**: `apogee.Config` gains one field, `ContextFiles` (the names to
  look for; nil = off), alongside a read-only `ContextFilesReport()` on the engine, so this is a
  **minor** bump and an embedder that sets nothing is byte-identical to before. See
  [ADR 0026](docs/adr/0026-workspace-context-files-are-session-scoped-prompt-data.md).

- **Sessions now persist continuously, are browsable, and resume with their scrollback intact — the
  session system.** Previously a conversation was saved **only on a clean quit** and `--resume
  <path>` reopened it into an empty-looking view (the engine remembered, but nothing replayed). Now:
  - **Per-Turn autosave.** The active session is written to `~/.apogee/sessions/` after **every
    completed Turn**, so a crash or `kill -9` loses at most the last Turn, not the whole session.
    Saving is asynchronous and best-effort — a save failure is noted once and the conversation keeps
    running (recovery is noted too). Empty sessions are never written.
  - **Dual-representation records.** Each session is stored as an id-addressed record wrapping the
    engine's conversation **and** the TUI scrollback (user/assistant text, tool cards, notes,
    sub-agent depth), so resuming **repaints the full scrollback** and relights the context-usage
    gauge — the view no longer starts blank over a model that still remembers.
  - **`/sessions` history browser** — an in-TUI overlay that lists your saved sessions (title ·
    relative time · message count, newest first), with `enter` to resume, `d` to delete (with a
    `y/n` confirm), `r` to rename inline, and `a` to toggle between this workspace and all
    workspaces. Titles default to the first user message and can be renamed.
  - **`/clear` and `/new` now close the current session into history and start a fresh one** (both
    verbs are kept). Neither deletes — the old session stays in the browser; discarding is `d`.
  - **`apogee --continue`** resumes this workspace's most recent session without naming it, and
    **`--resume`** now accepts a **session id (from `/sessions`) or a file path**. Old pre-existing
    timestamp session files still load and resume (without scrollback replay — the model still
    remembers).
  - **Interrupted tasks resume.** A session killed mid-task resumes to the last completed Turn with
    a note; **`/continue`** then picks up the unfinished work where it stopped (sending a new message
    instead discards it and continues fresh).

  Agent mode, tool approvals, confinement, and MCP connections are deliberately **not** part of a
  saved session — they are re-established or re-confirmed on resume. See
  [ADR 0022](docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md).

- **`ask_user` can offer multiple-choice answers.** When a question has a natural closed set, the
  model may now pass an optional `choices` list of short answer options alongside it; the human
  picks one with `↑`/`↓` and `⏎`, or ignores the list and types a custom free-text answer exactly as
  before — the choices never gate the reply. Without `choices` the tool behaves as it always has.

- **Tool cards no longer degrade when a tool changes its wording, and embedders can read a tool's
  outcome as data.** The one-line summary a tool card puts beside its target (`1 - 154` after a
  read, `+2 -2` after a diff, `12 matches` after a grep) was reconstructed by pattern-matching the
  result text that is written for the *model*, so re-phrasing a tool's own header quietly dropped a
  card back to its first line. Seven built-ins — `read_file`, `write_file`, `list_dir`, `grep`, `view_diff`,
  `web_search`, `open_file` — now report their outcome as a typed value beside that text, and the
  view renders the value. **Every card reads exactly as it did before** (the change was accepted
  against a byte-for-byte oracle); what changed is that it keeps reading that way.

  For embedders this is **additive** — no existing code changes shape. `apogee.ToolResult` gains one
  optional field, `Summary`, and the new `apogee.ToolSummary` sum is re-exported with its seven
  variants (`ReadSpan`, `WroteBytes`, `ListedEntries`, `MatchedLines`, `DiffStat`, `SearchHits`,
  `OpenedFile`), so a headless or bench host can switch on a tool's outcome instead of parsing prose.
  The sum is **sealed** the way `Event` is: you can read every variant and add none. Your own tools
  are unaffected — attaching a summary is optional, `Summary` nil is the normal case, and a result
  without one renders from its text exactly as before (see
  [ADR 0002](docs/adr/0002-tools-are-an-open-extension-point-mechanisms-are-curated.md)'s and
  [ADR 0011](docs/adr/0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md)'s 2026-07-25
  notes). Summaries are never persisted and never sent to the model.

- **The status-line spinner is now selectable, and can drift through colour.** A new file-only `ui:`
  block in `~/.apogee/config.yaml` carries two **independent**, fully optional keys: `spinner`
  (`snake` | `glitter` | `classic`) picks the animation shown while a turn runs, and `spinner-color`
  says whether it is coloured. `snake` — the new default — walks a six-dot snake around the outer
  ring of a 4x4 dot grid (two braille cells side by side), one lap a second; `glitter` re-rolls two
  braille cells twenty times a second while the number of lit dots breathes in and out over six
  seconds, swelling to a solid `⣿⣿` and falling away again; `classic` is the single rotating cell
  `⣾⣽⣻⢿⡿⣟⣯⣷` apogee has always shown, kept as a **permanent** choice rather than a fallback. The
  colour loop is a soft ten-second lap through three colours already in the palette (periwinkle →
  turquoise → blue), blended in Oklch so there is no visible step or seam at the wrap, and it takes
  ten seconds under every style.

  Because the two keys are independent, all six combinations work: `spinner: classic` with
  `spinner-color: false` is byte-for-byte the status line apogee rendered before this change, and
  `classic` with the loop on is those same glyphs in the new colours. Both keys are config-file only
  (no flag or env), an absent block resolves exactly as a run does today apart from the default
  style, and an unknown style name is a loud startup error naming `ui.spinner` and listing the styles
  this build knows. apogee does no terminal-profile detection, so on a 256-colour terminal the
  gradient steps and on a 16-colour one it collapses — `spinner-color: false` is the escape hatch.
  The animation is also now apogee's own rather than a `bubbles` spinner widget: every frame is a pure
  function of a frame counter, which is what the value-copied Model of
  [ADR 0011](docs/adr/0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md) requires.

- **apogee now sends a system prompt, and it is yours to write.** Until now a conversation started at
  your first message: apogee had no standing instructions at all, so anything you wanted it to always
  know had to be retyped every session. Three new **file-only** keys in `~/.apogee/config.yaml` fix
  that. `system-prompt-text:` holds the prompt inline; `system-prompt-file:` names a file holding it
  instead (a leading `~` expands, and a relative path resolves against your apogee home, so a prompt
  file travels with the rest of your config); and `system-prompt-models:` keys per-model prompts by
  the model name apogee resolves at startup. A matching per-model entry **replaces** the global
  prompt whole — it does not merge — and an entry naming a model you are not running is inert: never
  selected, its file never read, so one config carries a prompt per model across every machine.
  Setting both the text and the file at one level is a startup error naming both keys.

  Three placeholders are substituted **fresh on every request**: `{{workspace}}` (the workspace path),
  `{{datetime}}` (today's **date** — deliberately not a timestamp, which would change the prompt every
  turn and throw away your server's prefix cache), and `{{mode}}` (the autonomy mode, so a Shift+Tab
  is reflected from the next request onward). The spelling is strict and the set is closed: anything
  else between double braces — including `{{ workspace }}`, with spaces — is a startup error listing
  the three, rather than raw braces shipped to the model.

  **A fresh install now starts with a default prompt** — a five-line persona and context frame, active
  (uncommented) in the starter `config.yaml`, the one setting in that file that is not a commented
  example. Edit it to make it your own, or delete it / comment it out to send no system prompt at all.
  **An existing `~/.apogee/config.yaml` is untouched by an upgrade** and has no such key, so it keeps
  today's promptless behaviour byte for byte; there is no compiled-in fallback. The prompt is
  **request-scoped**: it is the first system message on the wire — mechanism directives and the
  profile's tool-menu block fold in after it, in one system message — and it never enters your
  conversation history or a saved session. Sub-agents inherit it; the compaction summariser and the
  `apogee probe` battery keep their own dedicated prompts and never see it.

  For embedders this is **additive**: `apogee.Config` gains one field, `SystemPrompt`, carrying the
  **template** (not a rendered string — the mode and the date are live), so this is a **minor** bump
  and an embedder that sets nothing is byte-identical to before. An unknown placeholder fails
  `New`/`Resume` loudly, the way a bad model profile does. See
  [ADR 0023](docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md).

- **apogee now watches the server it is talking to — the upstream Heartbeat.** Model, context window
  and reachability were read **once**, at startup, and never again: restart your llama.cpp server
  with a different model (or a different `-c`) beside a running apogee and the footer kept showing
  the old name over the old window, the context gauge kept dividing by a window that no longer
  existed, and the Budget kept measuring against it. Every **ten seconds** apogee now asks the server
  what it is actually serving and follows the answer:
  - **The display follows reality.** Footer, start-up box and the gauge's denominator move with the
    server, and the transcript says what changed, once: `model changed: gemma-4 → gpt-oss-20b,
    context 32k → 16k` (the window clause only when the window moved).
  - **So do the bindings — this is not a cosmetic refresh.** A model change re-resolves the outgoing
    request's model id, the per-model **system prompt**
    ([ADR 0023](docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md)),
    the matching **validated Mechanism set**
    ([ADR 0016](docs/adr/0016-curation-is-per-model-validated-sets-keyed-by-fingerprint.md)), the
    Mechanism registry, and the compaction budget — together, or not at all. Your **conversation,
    mode, approvals, confinement and tools stand**, and a change observed while a reply is streaming
    is applied the moment that exchange finishes, never in the middle of it.
  - **apogee starts without a server.** Startup no longer probes: the TUI paints **instantly** —
    where it used to stall up to five seconds, and refuse to start at all when `model:` was unset and
    the server was down — shows `connecting…`, and binds the moment a server answers (`connected:
    gemma-4, context 32k`). Start apogee first and your server second, in either order.
  - **Offline is a state, not a failure.** When the server stops answering the footer says `offline`
    and a send is refused with a note naming the endpoint and the reason — **and your typed message
    stays in the box**. Scrollback, `/clear`, `/sessions`, `/version`, `/confine` and Shift+Tab all
    keep working. A failed beat while a reply is streaming is ignored outright (a live stream is
    better evidence than a timed-out status call on a busy single-slot server), and an established
    session needs two consecutive quiet failures before it says offline, so a moment of load cannot
    flicker the footer. Recovery is noted once too.
  - **`context-window:` is now a pin, and `model:` is a hint.** A configured window is **never**
    overridden by the heartbeat — it stays the escape hatch for a server that misreports its own —
    while a configured model is followed while the server serves it and yields, with a notice, when
    the server loads something else. Leaving `context-window:` unset now means "discover it, and keep
    discovering it".
  - Two long-standing display bugs go with it: a **pinned model was probed with an empty hint**, so on
    a multi-model server apogee adopted the *first* model's context window rather than the pinned
    one's; and the **gauge clamped its bar but not its percentage**, printing readings like `41k
    137%`. Both fixed.

  Every beat also carries the server's whole `/v1/models` offering into the TUI's state — the
  offering `/model` now lists and picks from — and the rebind path a beat takes is the one both
  `/model` and `/server` complete their switch through. Building those seams was half the point.
  For embedders this is **additive**, a **minor** bump: `apogee.Agent`
  gains `Rebind(RebindSpec)` and the root facade re-exports `apogee.RebindSpec`, and `Config.Model`
  may now be **empty** at construction — a model-less agent is a legitimate object (clear context,
  restore a session, switch mode) and only `Submit` refuses until a model is bound, so a host can
  start before its server exists exactly as the TUI does. `Rebind` is **idle-only** (it refuses
  mid-exchange, the way `ClearContext` does) and validate-then-commit, so a spec the new model cannot
  satisfy leaves every binding intact. Internally the dead `provider.ServerManager` is deleted: its
  liveness half is superseded by the heartbeat, which observes strictly more, and its local-server
  launch half will be rebuilt over the beat when that work lands. See
  [ADR 0024](docs/adr/0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md).

- **A keyed server is now usable — the `api-key` setting.** A server that wants a bearer token
  (llama.cpp started with `--api-key`, LM Studio, a remote vLLM, any keyed OpenAI-compatible
  proxy) could not be driven at all: the provider client had always been able to send
  `Authorization: Bearer …`, but nothing ever gave it a key. Now `api-key:` in
  `~/.apogee/config.yaml` — or `APOGEE_API_KEY` in the environment, which **overrides** the file —
  carries the token onto **every** wire that reaches the server: your conversation (sub-agents
  included), the ten-second heartbeat, the `apogee probe` host report's reachability probe, and
  `apogee probe model`'s battery. Partial wiring was the failure to avoid — a session that works
  while the footer shows a permanent `401`.
  - **There is deliberately no `--api-key` flag.** A secret typed on the command line lands in
    your shell history and in `ps` output on every OS; the file and the environment variable
    cover every invocation shape.
  - **An unset key changes nothing.** Empty — the local-server default — sends no
    `Authorization` header at all, so a keyless setup behaves exactly as it did before.
  - **The value is never shown; its presence is.** `apogee probe`'s upstream block gained one
    line reading `api key: configured (sent as a bearer token)` or naming the two places a key
    would come from — because "is my key even loaded?" is the first question behind every 401 —
    and the client already redacts the key from error text a server echoes back. The caveat is
    documented rather than engineered around: `config.yaml` is plain text, so prefer the
    environment variable on a shared machine, or restrict the file yourself.

  For embedders this is **additive**: `apogee.Config` gains `APIKey`, passed straight to the
  session's provider client.

- **You can now type while the model works — and what you type reaches it mid-task. Interjections.**
  The prompt box used to be dead for the whole of a run: keys scrolled the transcript, `⏎` did
  nothing, a paste was dropped, and the only way to say "also check the tests" was to wait for the
  task to finish and start a new one — by which point the turns that could have acted on it were
  spent. Now the box stays live, and a message you enter while a run is in flight is **queued and
  delivered into that run**:
  - **Delivered at the next tool boundary, not after the task.** Your message is committed into the
    running exchange between turns, so the model reads it with the rest of the work still ahead of
    it. If it is already writing its final answer there is no next boundary, so the message goes out
    the moment the exchange ends, as the next one. Nothing is clock-timed — "queued" means *sent at
    the first boundary that exists*.
  - **You can see what is waiting.** Queued messages appear as dim `⧖` rows directly above the input
    box (three at a time, under a `… N more queued` marker), the status line reads `N queued`, and
    the placeholder changes to `⏎ queue · esc stop` so the box says what `⏎` will do. Each message
    joins the transcript **when it is actually delivered**, so the scrollback stays an honest record
    of what the model saw and when.
  - **`esc` stops everything, including the queue.** Nothing auto-sends after a stop or an error:
    the rows stay put under a one-line note, and the next `⏎` — even on an empty box — sends them.
    **Backspace on an empty box** takes the newest queued message back into the editor, exactly as
    you typed it, to edit or discard.
  - **Whatever is still queued when a task ends naturally goes out by itself**, as one message
    joining the queued lines oldest-first (a `/compact` that completes flushes the same way). A
    queue held over a stop survives `/clear` — it is outgoing input, not context — but not a quit:
    queued messages are never written to a session.
  - **`/commands` still run at idle only.** Typing one while the model works is refused with a note
    and your line is left in the box, rather than being queued into a refusal later.

  **One thing deliberately changes:** the single-key transcript scroll while a run is in flight
  (`j`/`k`/space) is gone, because those keys now type. `PgUp`/`PgDn` and the mouse wheel scroll the
  transcript in every state, as before. Typing stays blocked where another decision owns the
  keyboard — at an approval prompt (`a`/`d`/`s`), while the model is asking you a question (the box
  holds the answer), and after an error (`⏎` dismisses).

  For embedders this is **additive**, a **minor** bump: `apogee.Agent` gains
  `Interject(UserInput) error`, which commits a marked user message into the open exchange and is
  documented as callable only from the goroutine driving `Step`, **between** Steps — the same
  boundary the per-turn `Snapshot` already uses, so it takes no lock. `apogee.Message` gains an
  `Interjected` flag (the derived exchange boundary skips it, so a mid-task message joins the running
  exchange instead of starting a new one) and `apogee.ErrNoOpenExchange` is re-exported. Saved
  sessions round-trip the flag with **no version bump**, in both directions. See
  [ADR 0025](docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md).

- **The prompt caret is now the real terminal cursor, and it never blinks — the `cursor-shape`
  setting.** The box painted its own simulated cursor, which blinked with no way to turn it off. It
  now draws the **terminal's own** cursor at the caret, steady, in the shape you name:
  `cursor-shape: block` (the default), `underline`, or `bar`, a file-only key in
  `~/.apogee/config.yaml` — an unknown value is a startup error listing the three. The cursor shows
  wherever the box is editable, including while the model works, and is **hidden** where it is not
  (an approval prompt, or after an error), so the caret is never sitting in a box that ignores it.
  Your terminal's own cursor is restored when apogee exits.

  Inheriting the shape your terminal is configured with is deliberately **not** offered: a
  full-screen terminal program names a cursor shape on every frame, so "whatever you had" is not
  something apogee can express while it runs — the key is the honest substitute. There is no blink
  option; nothing blinks.

- **`/schedule` — put a prompt on a cycle and let it run itself for as long as apogee is open.** A
  standing instruction rather than a task: `/schedule 30m re-run the tests and note anything new`
  puts that line on the clock, and every half hour it runs on its own, in a session of its own. One
  verb, three forms — bare `/schedule` lists what is live (`name · every 30m · plan · next 14:05 ·
  3 fired`, plus `running now` while one is going), `/schedule <prompt>` asks for the cycle and the
  mode in the same overlay `/model` and `/server` open (presets `1m` through `4h`, then `plan` or
  `auto`), and `/schedule <cycle> [auto] <prompt>` creates one outright from any Go duration of 30
  seconds or more. `/schedule-stop` takes one off the clock: the only live one straight away, a
  picker over the rows when several are. Both verbs work while the model is busy — a firing is a run
  of its own, so putting one on the clock needs no quiet moment in this session.
  - **A firing is a session, not a background job.** Each one builds a **fresh** agent against the
    server and model you are bound to *at that moment*, submits the prompt, and saves an ordinary
    session record titled `<schedule> — <HH:MM>` — so it browses, resumes, renames and deletes in
    `/sessions` exactly like a session you held yourself, tagged `⟳ <schedule>` beside its title so a
    run reads as one of a series. Nothing carries over between firings: every one starts on an empty
    context window, which is the point of a cycle rather than one conversation growing forever. What
    a firing does *not* record is scrollback, so resuming one takes the documented "resumed, no
    scrollback recorded" path — the conversation is all there, the painted transcript is not.
  - **`plan` or `auto`, decided when the schedule is created.** The mode is the schedule's own and is
    independent of whatever mode you later switch this session to; `auto` is gated by the same
    eligibility ladder that gates *launching* in auto, and where the host has closed it the row is
    still offered — taking it prints the reason and leaves the pane open, so `plan` is one keypress
    away and the prompt you typed is not lost. Ask-before and allow-edits schedules are deliberately
    absent: both exist to consult a human and a firing has none, so its approver **denies** every
    gated action with a recorded reason instead of parking on it, `ask_user` and `present_document`
    are unregistered, and a firing's deliverable is a file in the workspace with its path in the
    conversation. Firings also run without MCP tools — live host connections are not something a
    second agent may borrow.
  - **One at a time, and never on top of your own work.** A tick that lands while that schedule's
    previous firing is still going is **skipped** with a note, and the next tick fires normally —
    never queued, never two at once. A tick that lands while *you* are mid-exchange waits for the
    session to go quiet rather than contending with the task in front of you, and further ticks
    arriving during that wait are skipped the same way, so at most one firing is ever pending.
  - **A firing's answer comes to you, in a block you can open.** What a run actually *said* is the
    payoff of putting a prompt on a cycle, and it lives in a session record you would have to go and
    open. So every firing gets one expandable block in the chat instead — the tool block's shape and
    behaviour under a header of its own, `⟳ Schedule` beside the schedule's name. It appears the
    moment the run starts, carrying `firing now` and the prompt it was given, and the *same* block is
    filled in when the run returns: it stays where it was announced rather than jumping to where it
    finished. Collapsed, it shows the answer's first line over the usual `… +N more lines`; opened, it
    carries the whole answer, the prompt beneath it, one stats line (`2 turns · 4s`, plus a
    `· N denied` cell only when a gated action was really refused) and the record pointer —
    `saved as "…" — find it in /sessions` — dropped when nothing was persisted. A failed firing words
    its header `error: …` and shows no answer, but keeps the stats and any record the run salvaged.
    The `⟳` never blinks: the spinner belongs to the work *you* are doing, and this session is idle
    while a firing runs.
  - **The block is scrollback, so it is saved and repainted like everything else** — and one still
    open when apogee closed comes back closed, saying so in its own words rather than claiming a run
    is in flight that died with the program that scheduled it.
  - **Created (with cycle, mode and next fire), tick skipped and stopped stay one-line notes** —
    lifecycle facts with no body, where a block would be an empty drawer. Notes and blocks alike stay
    in the session record, because each records something that actually happened while you had this
    session open.
  - **Schedules die with apogee.** Nothing is written to `config.yaml` and nothing survives a quit —
    "while apogee is open, re-run this every N minutes" is the whole promise, deliberately. Durable
    schedules are the future daemon's value-add over the very same library.

  See [ADR 0033](docs/adr/0033-the-scheduler-is-a-library-and-the-tui-is-its-first-driver-surface.md);
  the three panes, the firing block and the browser's tag are specced in `layout.md`.

- **Under that surface: a scheduler library and a one-firing headless runner.** The TUI owns nothing
  but input and display here. `internal/schedule` owns every when-and-how decision — the cycle floor,
  the skip-the-tick overlap policy, the quiescence gate, the lifetime — behind injected seams and a
  clock, so a daemon that does not exist yet can drive it without a terminal; `internal/run` performs
  one firing (fresh agent, denying approver, the record saved once at completion, and on a failed run
  whatever completed) and is the shared core the deferred `apogee headless` subcommand will be a thin
  CLI over rather than a second runner. Both stay `internal/`, so the public Go API is **unchanged**
  by this feature. The session record gains two optional `Meta` fields carrying the schedule's id and
  name — empty on every ordinary session, what the `/sessions` tag reads — and because older builds
  ignore unknown fields and never write them, records round-trip both ways with **no `RecordVersion`
  bump**. What the block shows crosses those same seams as plain data, so a Driver that is not this
  TUI gets all of it too: the runner reports a firing's final answer — the top-level one, never a
  sub-agent's — beside its turn count and how many actions its fail-safe denier refused, and the
  scheduler times the run on its own injected clock and puts that elapsed on the finished **and** the
  failed event, along with whatever outcome a failed run salvaged. The library reads none of it; it
  carries it.

- **The release carries binaries now, and Homebrew installs them.** Every GitHub release ships all
  **six** targets — Linux, macOS and Windows × `amd64` and `arm64` — as versioned archives holding
  the binary, the README and the LICENSE, with a `SHA256SUMS` file beside them; `brew install
  airiclenz/tap/apogee` downloads the archive for your platform instead of compiling anything, so
  neither route needs a Go toolchain on the machine that runs apogee. Building from source is
  unchanged and stays the shortest path to the tip of `main`. One caveat travels with the
  convenience, and the README states it where the download is rather than in a footnote: the
  binaries are **not code-signed** yet, so a browser download on macOS is quarantined until
  `xattr -d com.apple.quarantine` clears it, and Windows SmartScreen warns about an unrecognised
  publisher. Signing is follow-on work; until it lands, `SHA256SUMS` is the check worth making.
  - **`make dist` is where the archives come from.** One target builds and packs the whole
    publishable matrix on whichever machine cuts the release — `.tar.gz` for Linux and macOS,
    `.zip` for Windows, the sums computed over all six — so a release is reproducible from the
    repository rather than from whatever commands the last one happened to use. `make cross` keeps
    its old job: the same six builds thrown away, as a compile check.

### Changed

- **The two prompts that ask you something are menus now.** An approval used to be a paragraph with a
  legend under it (`a allow · s always · d deny`), and an ask prompt a question under an
  `the assistant is asking:` heading. Both are menu boxes: the tool the decision turns on rides the
  top border — `╭────── Approve terminal? ──────╮` — the options are rows you walk with `↑/↓`, `❯`
  marks the one `⏎` sends, and the others sit dim behind a `·`. Nothing you knew stopped working:
  `a`, `s` and `d` still answer an approval the moment you press them, `esc` still cancels the run,
  and typing under an ask still swaps to a free-text answer of your own.
  - **The approval's shortcut letters moved beside the options they take** — `Allow [a]`,
    `Always allow this session [s]`, `Deny [d]`, `Cancel [esc]`, aligned in a column of their own —
    so the legend row is gone and the pane spends that row on what you are deciding about instead.
    On the shortest terminal a pane is drawn in at all, that is the difference between seeing a
    decision row and seeing none.
  - **An approval shows the arguments, not the JSON they arrived in.** Every tool's arguments are
    labelled lines — `command:` with the shell line indented under it, `path:` with the file under
    that — in the order the model wrote them, so a command spanning several lines reads as the lines
    it will actually run instead of one `"…\n…"` string. No braces around the set, no quoted key
    names, nothing to read past. Nothing is summarised away to get there: every argument is on the
    screen, a working directory or a timeout beside the command included, because what you decide
    against must be what the tool will actually receive — and arguments that cannot be labelled at
    all (a blob that does not parse) are still shown exactly as they arrived.
  - **An ask prompt's answers may be prose.** A long option wraps with a hanging indent instead of
    being cut off, a blank line separates one answer from the next, and the tool no longer asks the
    model for short single-line choices. An answer is seated whole or not at all, so a short window
    scrolls rather than offering you two thirds of a sentence to decide on.
  - **A short terminal never leaves an ask prompt anonymous.** On a window with a single row to spare
    for the question — a half-height split, say — that row goes to the `… (+N more lines)` count, so
    the question moves up onto the box's top border instead, rather than leaving you a live `⏎` and
    nothing on the screen saying what it would answer. Give the box its rows back and the border is
    plain again.
  - **And an ordinary one never shows a count where the question belongs.** Four prose answers with a
    blank line between each pair cost nine rows, which is nearly everything an 80×24 window has to
    give a pane — so the question was being squeezed out on a terminal with room to spare. It now
    keeps up to three lines before the answers claim the rest: a short question still costs the
    options nothing, and a window too short for both still puts an answer on the screen ahead of the
    question's third line.

  Mocked up in `docs/layout/user-questions-layout.md`; specced in `layout.md` (What "height" means,
  The Column contract).

- **A skill you invoked is the coloured token in your own text now, not a tag row under it.** A sent
  prompt used to say it twice: the message with `/refocus` in it, and then a violet `✦ Refocus` row
  beneath naming the same skill again. Now the `/refocus` inside the block is simply painted in the
  skill violet — exactly as it lit up in the prompt box before you pressed `⏎` — so one surface says
  it once. Queued interjections (`⧖`) read the same way, a skill named twice in one message is
  accented at both places, and a selection dragged across the block still wins over the colour.
  - **The colour is recorded with the message rather than looked up when it is drawn.** Where each
    token sits is captured at send time and saved with the session, so re-opening that session later
    paints what the send actually resolved to even if the skill has been renamed or deleted since —
    the block stays an honest record of what the model was given. Prompts sent before this release
    carry no such marks and paint plain.

  Ruled in ADR 0027's 2026-08-04 addendum; specced in `layout.md` ("Collapsed and expanded blocks",
  "The prompt box's mini-language").

- **A streaming reply no longer re-renders the whole transcript for every token.** A local model
  streaming at speed pinned a core — and the GPU behind the terminal's compositor with it, which is
  what made the fan audible for the length of a run — because each SSE delta became its own screen
  update, and each screen update re-parsed, re-styled and re-wrapped the entire scrollback from the
  top. The bill therefore grew with the conversation: the longer a session ran, the more work every
  single token cost. Nothing about what is painted changed — the output is byte-identical — only
  how often it is rebuilt and how much of it.
  - **Adjacent tokens are merged inside the TUI's event sink over a short window** — about two
    frames at 60 fps, imperceptible as latency, and it caps token-driven repaints near ~33/s no
    matter how fast the provider streams. Merging, never dropping: no token's text is lost or
    reordered, and any other kind of event delivers the merged text ahead of itself, because every
    one of them depends on the tokens that preceded it. The window never outlives the Step that
    opened it — the sink is flushed the instant a Step returns, the cancel path included — so
    events still arrive inside the Step that emitted them, which is what the worker and the Model
    both rest on.
  - **A finished block's paint is reused until something it depends on actually changes.** Each
    block's rendered lines are cached against the width they were laid out to, the expanded and
    done flags across its span, the blink phase for a live block, and the width measure the
    terminal answered with — so a steady-state streaming repaint costs the live tail rather than
    the whole scrollback. There are no invalidation hooks to forget: a key that no longer matches
    simply misses and the block is painted again.

- **`/skills` now tells a skill that lost an id clash apart from one that is broken.** Every skip
  discovery recorded used to be headed `N skills found but not loaded` — true of a malformed
  `SKILL.md`, and a libel on a shadowed one, which parsed perfectly and simply is not the copy
  `/<id>` runs. Shadowed skills get their own section now, `N skills shadowed by another of the same
  id`, with the copy that lost on one line and the file that is live on the next, so "which of my
  two copies does this run?" is answered with a path you can open. A report whose only skips are
  shadows no longer claims anything failed to load. The where-we-looked note an empty catalog prints
  also lists the three source dirs in their new priority order (ADR 0032), your global library last
  — the one that wins a clash.

- **A markdown table cell that does not fit its column now wraps inside it instead of being cut with
  a `…`.** A long cell used to lose its tail the moment the columns had to be squeezed — the widest
  column shrank, the sentence in it stopped mid-word at a `…`, and what the model actually wrote was
  simply not on screen. Now that cell wraps onto as many further lines as it needs inside its own
  column, so the row becomes as tall as its tallest cell and nothing is dropped. There is no `…`
  anywhere in the table contract any more, and no height cap either — a cap would only put the cut
  back at a different threshold.
  - **Columns are told apart by a vertical rule, `│` with one space either side**, because a wrapped
    row needs a visible column boundary to still read as columns. It wears the muted colour of the
    header rule — the frame is not content — and the header rule crosses it at a `┼`, so that line
    stays one continuous stroke the full width of the table. Nothing else was added: no outer frame
    and no corners, so the table still sits in the body column like any other
    paragraph rather than reading as a boxed object. Cells are top-aligned, and every line of the
    block — header, rule, body, the continuation lines of a wrapped cell and the blank filler beside
    them — is still padded to exactly the table's width, so the straight right edge the scroll-bar
    gutter and a drag selection both depend on holds through a wrapped row.
  - **A table too narrow to read is still plain paragraphs.** No column is shrunk below four cells of
    content — narrower than that a wrapped cell comes apart into a letter or two per line, which is
    vertical text with a rule beside it, not a column — and where the width cannot give every column
    that minimum the block falls back to the paragraphs it rendered before, which always fit. The
    floor is a floor on the shrink, not a width every column is handed: a column whose content is
    naturally narrower keeps its own width and is never charged the four cells it would not use.

  Specced in `layout.md` ("Markdown tables in assistant text").

- **Adjacent rows of a markdown table are now ruled apart.** A table drew exactly one horizontal
  rule, under its header, and everything below it ran together — which got harder to read the moment
  cells started wrapping, because a two-line row and the row under it looked like one four-line
  block. Now the same faint `─` runs between every pair of adjacent body rows, crossing each column
  divider at a `┼` exactly as the header's rule does, so a row boundary is as visible as a column
  boundary. It is one stroke used in two places, not two: the header keeps its distinction through
  its **bold** cells rather than through a rule of its own.
  - **The table is still ruled, not boxed.** No rule above the first body row, where the header's
    rule already sits; none below the last, because there is no bottom frame to close; and none
    inside a row — a cell that wraps onto further lines is still one row, and its continuation lines
    stay rule-free, so "more of this row" never reads as "the next row". Still no outer frame and no
    corners.

  Specced in `layout.md` ("Markdown tables in assistant text").

- **The transcript's scroll bar is one line in two weights — the thumb thinned from `█` to `┃`.**
  The bar used to be a solid block riding over a hairline track, two shapes that shared a column
  without sharing an axis; the thumb is now the heavy vertical `┃` over the same light `│` track, so
  the column reads as a single centred stroke that simply thickens over the stretch of transcript
  on screen. Nothing about the bar's geometry moved: the same one-cell gutter, the same thumb
  sizing and placement, the same two tones, and the same `ui.show-scrollbar` switch deciding
  whether the column exists at all. Specced in `layout.md` ("The scroll bar and the column it
  hangs in").

- **The context gauge names the window it measures against — `8k/98k 8%`.** The status line's
  gauge used to state the tokens used and leave you to find the window they were used out of
  somewhere else; it now spells both, because a fill only means something beside the limit it
  fills. Nothing else about it moved: it stays dark until the first turn reports usage, the
  percentage is still clamped to 100 beside an unclamped count — so a conversation carried into a
  smaller window reads as the plain contradiction `137k/98k 100%` rather than an impossible
  percentage — and the counts keep their honest lowercase SI `k`.

- **The footer names the workspace you are in, not the window you already read in the gauge.** Its
  left slot now closes on the directory this session is rooted in — `host ✦ model ✦ ~/Repos/apogee`
  — written with your home directory as `~`, and only where `~` really is a whole path component,
  so a sibling like `/Users/you-other/proj` stays spelled out rather than being abbreviated into a
  directory you are not in. It sits after the model because the line reads outward-in: the server,
  the model on it, and last the place here. The context window left that slot for the gauge above,
  which states it beside what has actually been spent; it is still a session fact and a change to
  it is still noted in the transcript. `connecting…` and `loading <profile>…` now replace the model
  word alone — the host and the workspace stay put behind them, since neither is a fact about a
  model nobody has named yet — and the `✦ offline` marker still closes the slot in the error tone.

- **The prompt box closes its own frame, the footer sheds its, and a hairline closes the screen.**
  The box used to stop at its side walls and borrow the footer's top divider for a bottom edge,
  which drew the two as one boxed-in unit three rows deep. The box now draws its own `╰───╯`, and
  the footer under it is a single frameless line: no `├──┤` divider, no `│` bars around the text,
  no `╰──╯` rule beneath. It takes the status line's posture instead — the same two-column lead,
  the same unbroken black field across the window, and the mode marker ending in the very column
  the gauge above it ends in. Under the footer, a `▁` hairline closes the screen: the `▔` at the
  top of the bottom chrome inverted, in the same recessive tone, so the whole section is bracketed
  by one rule above the status line and its mirror below the footer rather than the footer sitting
  flush against the terminal's last row. Nothing about what the footer says changed, and the frame
  still spends the same eight rows on chrome — the two rows the footer gave up are exactly the
  box's new bottom border and the new hairline — so every row budget, floor and pane threshold is
  where it was. Specced in `layout.md`.

- **The footer's mode marker leads with a symbol for the rung you are on** — `⊞ plan`,
  `◐ ask before`, `✔ allow edits`, `⏵⏵ auto`. Each glyph is drawn in that mode's own colour, as one
  piece with the word rather than as a badge beside it, so the autonomy level reads from the shape
  before the word is read — useful precisely when Shift+Tab has just moved it. The words, the
  colours, and the column the marker ends in are all unchanged, and the mode is still stated in
  full everywhere it is spoken in a sentence.

- **A tool block now spells the paths it names relative to the workspace.** The target leading a
  branch line and the one-line summary beside it read `docs/plan.md`, not
  `/home/me/proj/docs/plan.md`, so a project's own files stop spending the row's width repeating a
  prefix you already know. There is no leading `./` and no leading separator, the workspace root
  itself reads `.` — the ordinary spelling of "here", which is therefore what `pwd` shows — and the
  status line's live phrase reads the same shortened path the block beneath it will, because both
  are worded from one view.
  - **A path that genuinely is elsewhere keeps its absolute form.** Anything outside the workspace
    prints in full, because a relative spelling would say it was not — and that matters most on
    exactly the line it is most dangerous to misread, the write outside the project you are being
    asked to approve. A path that arrived relative is already in this form and passes through
    untouched. Only the root's own spelling is recognised, so a line that merely contains a slash — a
    URL, a fraction, a regex — is left alone, and a sibling directory whose name only opens with the
    root's (`/home/me/proj-old`) stays whole.
  - **What a block quotes is never respelled.** The rule reaches the paths a block *names* and stops
    there. The body beneath the branch — a diff's hunk lines, an edit's replacement string, a
    command's output, an unregistered tool's argument values — prints exactly as it was written,
    absolute paths included: an in-workspace path sitting inside file content *is* content, and
    shortening it would show you a spelling the file will not actually contain. A one-line output
    promoted onto the branch is quoted in that same sense — one row lower the identical line would
    have been a body — and so is the answer you type to an `Ask User` question, which is your words
    and not the block's. Nothing decides this by reading a line, because a content line can look
    exactly like a path; a line is respelled only where the presenter that put it there says it is
    the block's own wording.
  - **It is presentation and nothing else.** The arguments the model sent and the output a tool
    returned are never rewritten, so the agent's view of a path and the transcript's differ in
    spelling and in nothing else.

  Specced in `layout.md` ("The rules behind the tool-call sketch").

- **Bare `/rename` now names a session for what it has become, not for how it opened.** Typed late
  in a long session it re-derived a title from the very first thing you said and nothing else, so a
  session that opened on one task and moved to another stayed listed under the task it left. The
  call now reads a **bounded window of the user side of the session**: your opening request and the
  three most recent are always in, and the ones between them are added newest-first while a rune
  budget lasts. A short session therefore hands over its whole user side; a long one hands over its
  two ends, with the omitted middle standing as a single marker that says how many were left out.
  Every included request is excerpted and the budget is a hard cap, so the naming call stays
  bounded however long the session runs — it cannot grow with the transcript — and it is still one
  short completion off to the side of the conversation, never a Turn. Mid-turn interjections are
  steering rather than requests and stay out of the window.
  - **The model is asked for the dominant thread, biased recent.** The instruction now says to name
    the main thread of the work rather than enumerate the requests, and says outright that when the
    session has moved to a different task the title names what it moved to. You look for a session
    by what you were last doing, so recency is instructed rather than left to a small model
    answering the last thing it read.
  - **Automatic naming still reads your first prompt.** The call that fires on a fresh session's
    first message sends exactly one request — at that moment exactly one exists — and its firing
    rules, the `auto-title:` key, the heuristic fallback and the never-clobber rule are all
    untouched. Both forms do now share one instruction, so the automatic call asks for the dominant
    thread in the same words rather than in a second set that could drift.

  See the addendum to
  [ADR 0022](docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md).

- **The `/` menu now reads alphabetically.** The command dropdown used to list its rows in the order
  the table happened to declare them, which was neither the order they were added nor one you could
  predict; the rows now run `clear` … `version`, so a command is found where its name says it is.
  The order lives in the table itself rather than in a sort at render time — the table is the
  registry, and display order is one of the things it declares — and a test pins it, so a row added
  out of place fails loudly instead of quietly landing at the bottom. Nothing about what the
  commands do, or which of them stay available while the model runs, changes.

- **The status line's right slot now ends two columns short of the window edge.** Whatever occupies
  that slot — the context-usage gauge, `esc stop` while a turn runs, `enter dismiss` after an error,
  the primed-`ctrl+c` hint, the mouse-copy flash — used to be justified hard against the last
  column. A text hint only *read* as inset, because a glyph does not paint its whole cell; the
  gauge's track paints solid, so it ran visibly into the terminal edge while the hints beside it did
  not. The margin now belongs to the **whole slot**, so every occupant ends in the same column —
  mirroring the two columns the left slot leads with, and landing directly above the last character
  of the footer's mode marker. The row is still one unbroken black band to the edge. A window too
  narrow for both slots still drops the right one whole; that threshold simply arrives two columns
  earlier.

- **The `connected: …` note is no longer printed when the first heartbeat lands clean.** Starting
  apogee against a server that was already up and serving a model printed
  `connected: <model>, context <n>` into the transcript directly beneath the start-up box that had
  *just been restated with the same host, model and context* — the same news twice, in the one case
  where there was no news at all. **First contact** — the first beat ever to land, with no failed
  beat before it — now refreshes the box and the footer silently. The note still prints wherever it
  carries something: a cold start against a **down** server (offline first, then one `connected: …`
  when the server appears, which doubles as the recovery statement), and a server that was up but
  **modelless** at launch, when a model finally loads. `context window unknown` and the validated-set
  notices are not gated on this and still print on a quiet first contact.

- **The attached-skill chip strip above the prompt box is gone.** A skill is text in your message
  now (`/skill-id`), not state parked beside it, so the strip has nothing left to show: no chips,
  no Backspace-to-pop-a-chip, no attachment silently carried into `/continue` or dropped by
  `/compact`. The **sent** message still shows what you invoked — the `/token` itself, painted in
  the skill violet right there in the transcript. That is the record of what you asked for, and it
  is built from the tokens your message actually contained. Removing a skill is deleting its token.

- **Autocomplete completes the token at your cursor, not the last word of the box.** This was a
  deliberate deferral (`TODO.md`, "cursor-position-free, robust"); it turned out to be the reason a
  message already in the box had no `/` namespace at all. All three regions — `/`, `@` and
  `/skill <partial>` — now complete mid-string and splice in place. Fixing the caret's own walk for
  it also fixed a **pre-existing hang**: an ordinary keystroke that grew the box while the cursor
  sat below a soft-wrapped line ending in a space could spin the re-seat loop forever.

- **A lone `/word` that names nothing is refused instead of sent.** Typing `/skills` before it
  existed — or `/code-adit` at any time — used to reach the model as prose, which is exactly the
  confusion this release exists to remove. Now apogee answers
  `unknown command or skill: /code-adit — nothing sent` and leaves your line in the box for a
  one-character fix. Only a line that is **nothing but** that one token is guarded, so a slash
  anywhere in a real message is still just text. A bare `/skill` gets the picker's usage line.

- **Breaking (Go API): a Mechanism no longer describes itself — the registry holds catalogue rows.**
  `apogee.Mechanism` is **removed**; `apogee.RegisteredMechanism` takes its place, a row of
  `{Descriptor, Ordering, Hook}` in which the descriptor and the ordering constraints are catalogue
  **data supplied at registration** and `Hook` is the value that carries the behaviour.
  `MechanismRegistry.Add` now takes a `RegisteredMechanism` and `Ordered` returns them, so a hook
  value implements only its hook interface(s) and no longer needs `Descriptor()` / `Ordering()`
  methods of its own. The alias is removed rather than quietly redefined, so an out-of-date embedder
  gets a loud compile error instead of a silent shape change; under `0.x` this ships as a documented
  break with notice rather than a major bump (see
  [ADR 0003](docs/adr/0003-mechanisms-are-a-constraint-declared-registry-not-a-fixed-pipeline.md)'s
  2026-07-25 amendment and [ADR 0015](docs/adr/0015-catalogued-mechanisms-are-enabled-by-id-through-config.md)'s
  realisation note).

  **No shipped behaviour changes.** The same Mechanisms fire at the same hook points in the same
  deterministic order — same topological sort, same stable canonical-ID tiebreak — under the same
  Bypass, self-regulation, incompatibility and requirement gates, with the same error messages and
  matchable sentinels. The **CLI and TUI are unaffected**, and so are the file-only `mechanisms:`
  config block, `Config.EnableMechanisms`, `CataloguedMechanisms()`, the enable errors, and
  experimental hooks (`AddExperimental` still takes a bare value).

  What it bought internally: one catalogue table instead of two hand-synced maps, one
  `register(row{…})` call per Mechanism instead of a seven-part registration ritual, 42 dead
  metadata methods deleted, the `"library"` Mechanism ID no longer re-declared as a literal in the
  engine (a catalogue row declares the dependencies it needs, so the build loop is uniform for every
  ID), and one shared Mechanism-stack validity checker behind both the loud startup gate and the
  soft validated-set skip. A Mechanism and the row describing it can no longer drift, because they
  are joined once where the Mechanism is built rather than kept in step by a guard test.
- **A tool that reaches the network but is not one of Apogee's own url-filtered tools now asks for
  approval in Auto instead of running unattended.** Auto's "network runs freely" cell was always
  meant for Apogee's own network tools, which filter every URL through url-safety (the scheme/host
  rules plus the default-on SSRF floor). It was keyed on a tool merely *declaring* that it reaches
  the network, so a host-registered tool could reach any address unattended and unfiltered. Apogee's
  own network tools now reach the network through a single choke point that applies url-safety, and
  carrying the mark that choke point confers is what earns the unattended cell; any other
  network-reaching tool raises an Approval prompt, whose reason reads `unfiltered network reach`
  rather than `network reach` so you can see which kind of reach you are authorising. The change is
  tighten-only — nothing that used to prompt stops prompting — and `confine-to-workspace: false`
  ("I am the sandbox") is unaffected. **`web_fetch`, `http_request`, and `web_search` behave exactly
  as before.** Embedders: an in-process tool of your own that reaches the network gates in Auto, the
  same way your own write tools already do.
- **Every network tool's failure message now names only the host, never the full request URL.** A
  blocked or failed `web_fetch` / `http_request` previously echoed the underlying error, which for a
  bad URL embedded the whole URL — including any credential in its query string. All three network
  tools now share one message renderer that reports the bare host and scrubs the URL out of the
  reason, so the protection that was `web_search`'s alone (its API-key redaction) is now every
  network tool's by construction. A url-safety block also states itself once instead of repeating
  the phrase (`url blocked by url-safety (host 127.0.0.1): security: url blocked by url-safety: …`),
  so the reason is what you read. Successful results are unchanged.
- **Selector popups now share one look — a solid-black pane spanning the full window width.** A
  single shared painter (`internal/tui/popup.go`) draws every boxed selector overlay — a title row,
  `❯`-marked rows with the selected row highlighted, and a key-hint footer — so the pickers line up
  flush with the input box below instead of each carrying its own chrome. The pane is filled solid
  black end to end: the border, the padding, and the gap after a row shorter than the box all sit on
  black, so no strip is ever left on the terminal background. Two user-visible changes fall out of
  the unification:
  - **The `/sessions` history browser spans the full window width.** Its box was capped at 72 columns
    and, on any wider terminal, stopped short of the pane; it now fills the whole terminal width,
    flush with the input box. Rows wider than the box truncate with `…` inside it instead of wrapping
    and breaking the layout.
  - **The `/command`, `@file`, and skill dropdowns adopt the boxed popup chrome.** They previously
    painted as borderless faint rows truncated to the raw window width; they now render as the same
    titled, bordered pane (`commands` / `files` / `skills`) with the `❯` selected-row highlight and a
    `↑/↓ select · ⏎/tab accept · esc dismiss` key legend, also spanning the full window width.
- **`/clear` and `/new` now start a fresh session: they wipe the scrollback and reprint the start-up
  box.** Previously the two verbs reset the engine's memory but left the transcript untouched, adding
  a `context cleared …` note while the start-up box stayed put. The owner now wants them to "basically
  start a new session": the whole view is cleared and the start-up box is re-seeded, so it is
  byte-identical to a fresh launch at the same window size — no prior messages, and no note (the
  reprinted box is itself the signal). The live context-usage gauge and tok/s readout fall back to
  empty with the discarded conversation. The reset funnels through one new seam, `startNewSession()`,
  which the forthcoming session system will wrap to persist the outgoing conversation before the
  reset; on a `ClearContext` error the view is left intact and the failure is noted, so a
  fresh-looking view never lies about an engine that still remembers. This reverses the earlier
  "the box survives `/clear`" behaviour deliberately.
- **The agent loop's Turn lifecycle is consolidated into one `turnLifecycle` module — an internal
  refactor with no behaviour or public-API change.** The Turn's four exits (complete /
  Exchange-complete / abandoned / cancelled), its one-permitted overflow fold-and-rebuild, and its
  Exchange-boundary maintenance — previously three exit helpers, a copy-pasted recovery ritual, and
  scattered boundary-write sites in `loop.go` — now live behind a table-tested owner in
  `internal/agent/turn.go`. The semantics are locked to the prior behaviour and the existing suite
  is the pin; `agentState`'s session-JSON schema stays byte-compatible. Separately, `loop.go`'s
  construction and domain→wire translation clusters moved verbatim into `construct.go` and
  `wire.go`.
- **The ask and approval prompts now render as bordered popup panes.** Both overlays adopt the
  shared popup module (`internal/tui/popup.go`) instead of their own bold+faint text: the ask
  question, and the approval reason and pretty-printed arguments, word-wrap inside the pane rather
  than losing their tail to an ellipsis; the pane spans the full window width, flush with the input
  box; and an over-tall body is capped to the live screen budget with an explicit `… (+N more
  lines)` marker, so the input box the answer is typed into is never pushed off-screen.

### Fixed

- **The `/` menu offers the best match first.** Typing `/imple` listed `/feature-implementation`
  above `/implement-plan`: the rows came back in catalog order, so the skill that merely *contains*
  what you typed stood above the one that *starts* with it — and since the first row is the
  highlighted one, tab or `⏎` accepted the wrong skill. Rows now rank by match quality — an exact
  name first, then the names your text starts, then the ones it only appears inside — and ties keep
  the order they always had (the commands alphabetically, then the skills), so a bare `/` menu reads
  exactly as it did before. The ranking also happens *before* the list is cut to eight rows, which
  settles a second defect of the same shape: in a crowded menu, a skill whose name you had begun
  typing could be dropped to make room for weaker matches that merely sorted earlier.

- **The context gauge stays on the status line when a file name holds a tab.** The status line caps
  the tool target it shows — "reading · src/main.go" — so that a long path can never push the gauge
  at the right of the row off the screen. That cap was counted in characters while the screen pays
  four columns for a tab, so a path holding sixteen of them was let through at four times the width
  the cap had budgeted, filled the row on its own, and the gauge went missing for as long as the call
  ran. The cap is counted over the expanded target now, so it bounds the columns the phrase actually
  spends and the gauge keeps its place. The same phrase reports the open call inside a collapsed
  sub-agent run, which now shows a tab-bearing name at the same honest length.

- **A tool block's summaries line up when a file name holds a tab.** A block pads every target out to
  its widest one so the outcomes beside them — `1 - 154`, `+2 -2`, an `error: …` — form a straight
  column down the block. That padding was measured with a tab counting for nothing and then handed to
  a wrap that turns it into four spaces, so the one branch naming a tab-bearing path opened its
  summary four columns right of every other branch's — and a block draws nothing between the target
  and the summary, so the column is the only thing holding them in line. The target is expanded
  before it is measured and padded now, so every summary in a block starts in the same column.

- **A pop-up's columns, a presented document's path and the start-up card hold their shape around a
  tab.** Three more surfaces measured a tab as nothing while the screen spends four columns on it,
  and each broke differently. In a pop-up — the `/sessions` browser, the `/` and `@` dropdowns, an
  ask or an approval prompt — a tab in one cell opened that row's next column four cells right of
  where every other row opened its own, with the far end of the row cut to pay for it, and a pane
  has no rule between its columns to fall back on. A presented document's path line, emitted raw so
  the terminal can linkify it, painted four columns wider than the transcript had composed it. And a
  host, model or version carrying one ran the start-up card's info row past the card's own right
  border, folding that row in two once the overrun passed the window edge. All three expand tabs
  before anything measures them now, so a pane's columns stay one straight edge and the card keeps
  its border.

- **A markdown table whose cell holds a tab keeps its columns straight.** A reply pasting a table
  with a tab inside one cell drew that row four columns too wide: the cell was measured as if the
  tab were nothing, nothing styled it on the way out, and the screen turned it into four spaces
  after every column width had been settled — so the row's `│` came down beside the `┼` of the rule
  above it instead of through it, and the whole table read as broken rather than as one long row.
  The cells are expanded before the columns are measured now, so a tab-bearing cell sits in its
  column and the table's rules and dividers line up.

- **A tab-indented line in a code block keeps its own line.** The same tab that used to spill a sent
  prompt past its block did it to fenced code too, and there it cost more than a stray column: a
  reply pasting tab-indented Go, a Makefile recipe or a diff had the line measured as if the tab were
  nothing, so it overran the width and the screen broke it wherever it happened to run out — an
  unindented, unaligned second row that is not where the code block would have wrapped it. Tabs are
  expanded before the block measures anything now, so a long code line breaks at the block's own edge
  and stays indented and coloured as code across the break.

- **A sent prompt containing a tab stays inside its block, and its skill token keeps its colour.** A
  tab weighs nothing when the transcript measures a line and four columns once the block is painted,
  so a prompt carrying one composed four cells more than it had room for: the row spilled past the
  block's right edge, split itself across two rows on the way to the screen, and the violet accent on
  an invoked `/token` slid four columns to the left of the token — colouring the tail of the word
  before it instead. Tabs are expanded to spaces before anything measures them now, so the block
  paints exactly the width it was given and the accent lands on the token, on every terminal.

- **Backspace and Del delete the prompt text you selected with the mouse.** Dragging across text in
  the input box and pressing Backspace used to make the highlight vanish and take exactly one
  character with it — the selected text survived, and you deleted it by hand. Both keys now remove
  the whole selected range, in either drag direction, counting runes rather than bytes so a CJK or
  emoji selection goes as one; the caret lands where the selection started, and it behaves the same
  while the model is working. With nothing selected the keys are untouched: Backspace on an empty
  box still takes the newest queued interjection back into the editor, and on a non-empty box it
  still deletes one character.

- **A click on a tool block's header lands while a reply is streaming.** Collapsing or expanding a
  call during a run was a coin flip — very often nothing happened at all — while the same click on
  an idle transcript was instant and always has been. Two independent misses were behind that, and
  both are gone.
  - **The toggle is resolved from where you pressed, not from where you released.** A motionless
    click was hit-tested a second time at release, against *screen* coordinates, and every streamed
    token scrolls the transcript to its tail underneath your finger — so in the 50–150 ms between
    press and release the row you aimed at had moved out from under the pointer and the release
    resolved nothing. The press already records its target in content coordinates, which appending
    cannot shift; that is what the toggle reads now, identically for a header, a `… +N more lines`
    marker and a collapsed prompt's `see more (+N lines)…` block.
  - **A press on a block that is still working survives the block repainting under it.** A live
    block's header star blinks with the spinner, and a repaint of any line the press sat on
    discarded the press outright — so the running calls you most want to open were the ones whose
    headers could not be clicked at all. A press with no drag behind it paints no highlight, so
    there is no stale text it could be covering; it now rides out repaints. A real drag selection
    still drops the moment its text changes, exactly as before, which is the rule that keeps a
    highlight from describing text that is no longer there.

- **`apogee probe model` dates itself in your own time zone.** The report's `probed at` line, and
  the two dates its record section names — `changed since …` when a model swapped behind an
  unchanged label, and `the record from … continues to apply` under `--no-save` — were printed in
  UTC, because UTC is how the instant is stored and the display simply inherited it. All three now
  convert at the format seam, the same fix the scheduler's titles just had. The spelling stays
  RFC3339, so the offset is right there in the line and the reading is never ambiguous. Nothing
  about storage moves: the probe record on disk keeps its UTC `probed-at` stamp, and the tests
  pinning the three lines are built from local's own offset, so they hold on any machine's `TZ`.

- **A scheduled run's title is spelled in your own time zone by construction, not by luck.** The
  `<schedule name> — HH:MM` title a Firing's record is saved under — the one its block points at and
  `/sessions` lists it by — was formatted in whatever zone the instant behind it carried, and the
  clock behind it is an injectable seam: a caller handing the runner a UTC-located clock got a UTC
  title. It now converts to local at the format seam, the way the `next HH:MM` in `/schedule`'s
  listing and its created notice always have. Storage is deliberately untouched — a record's
  `createdAt` / `updatedAt` stamps and every piece of scheduling arithmetic stay UTC, which is where
  they belong — and both display paths are now pinned by tests that hold on any machine's `TZ`.

- **A cloned repository can no longer quietly redefine one of your own skills — your library wins,
  and the copy that lost is named.** Skills are discovered from three layered dirs, and until now
  the project's `.apogee/skills` outranked your global `<apogee home>/skills` on an id clash. A repo
  shipping `.apogee/skills/<id>/SKILL.md` under the display name and summary of a skill you had used
  for months therefore replaced it outright: you typed `/<id>`, the repo's instructions were
  prepended to that turn with full agent authority, and the `/skill` picker showed a single row with
  *your* wording on it. Two changes close that.
  - **Your global library is now the highest-priority source.** A project can still contribute a
    skill id you do not have — that is the point of the extension — but it can never take over one
    you do. The two project dirs keep their order between themselves: the bare `skills/` dir (still
    only read when `use-project-skills` is on) still outranks `.apogee/skills`.
  - **A shadowed skill is recorded instead of forgotten.** Whichever copy loses a clash is kept on
    the catalog with the winning file's path, so `/skills` can show you both. That also covers two
    skill folders inside a *single* dir declaring the same id — a case that silently dropped one
    until now.

  Decided in `docs/adr/0032-the-user-skill-library-outranks-the-workspace.md`, which also records
  the deliberate divergence from apogee-code here: a `SKILL.md` written for either tool still loads
  in both, only clash resolution differs.

- **The `autofix` Mechanism no longer runs a formatter outside every guard apogee has — and it no
  longer runs the one formatter that executes code from the repository it is formatting.** When
  `autofix` repaired a syntax-broken file the model was about to write, it shelled out to the
  language's formatter directly: no mode check, no approval, no confinement box, no bound on the
  kill path. Two things follow from that one seam, and both are closed.
  - **The spawn is now gated and fenced.** A formatter runs only when the hook carries a subprocess
    permit — so in Plan, Ask-Before and Allow-Edits `autofix` does not spawn at all — and, when the
    permit names a box, the command is confined to it before it starts. A box that cannot be
    established **skips that formatter** rather than falling back to running it unfenced; the file
    is simply left as-is for the syntax stage to correct, which is what `autofix` has always done
    with a formatter it could not use. The in-process `gofmt` repair spawns nothing and is
    unaffected: it still runs in every mode, Plan included.
  - **The JavaScript / TypeScript rungs are gone.** That formatter resolves its configuration by
    walking up from the file's path and `require()`s whatever `.prettierrc.js` or plugin it finds —
    so a `.ts` or `.js` write into a hostile repository turned a silent formatting pass into running
    that repository's code as you. The remaining three (`goimports`, `black`, `rustfmt`) read
    declarative config only. A broken `.ts` file is now left for the syntax stage, exactly as one in
    a language with no formatter installed always has been.
  - **A wedged formatter can no longer freeze the session.** The formatter's clock now hangs off the
    turn's own context, so cancelling stops one mid-run, and the wait after a kill is bounded too: a
    wrapper-shaped formatter that leaves a grandchild holding the pipes used to block the agent
    permanently, mid-turn. The 3-second bound is unchanged.

- **A pop-up pane no longer paints a spacer row against the bottom chrome.** The `/sessions` browser
  and the `/model` | `/server` picker — and the approval or ask prompt with them — each stood one
  blank row clear of the `▔` hairline, while the `/` and `@` dropdown and the staged-message band
  sat flush on the input box: the same offering looked deliberately placed in one slot and adrift in
  the other, depending only on which surface you had opened. The frame's single gap row now sits
  ABOVE that slot instead of below it, so a pane opened there seats its bottom border directly on
  the hairline and every pane in the frame reads alike. With no pane open the frame is unchanged —
  the blank row still lands directly above the `▔` — and the row budget is untouched either way: the
  frame spends exactly one gap row in every composition, so the transcript pays no more and no less
  for a pane than it did before.

- **A narrow start-up card no longer eats the end of a host or model name without saying so.** In a
  window too narrow for the card's side-by-side layout, the facts stack under the logo — and a value
  wider than the card simply stopped at the border: at 29 columns a host of `192.168.64.1:1111` was
  painted as `192.168.64.1:111`, a port one digit short, which reads as the address you configured
  rather than as a cut. The card now fits its own rows to the room it has, and a value it cannot show
  whole ends in the same `…` every other clipped line in apogee carries, so what was eaten is visible
  as missing. At every width that can seat the values — which is every ordinary window — not a
  character of the card moves.

- **Wrapped text now breaks where the terminal actually draws, not a cell short.** Every wrapped
  surface — the assistant's prose, a markdown table cell, your own prompt block — chose its line
  breaks with a ruler that counts a glyph like `⚠️` as two cells, while most terminals paint it in
  one. A line carrying one broke a column earlier than it needed to, so it sat inside a narrower
  column than the window had actually given it, and a word that would have fit was pushed down a
  row. The wrap now measures the way the terminal paints — the same measure the absolute width cap
  was already held in. On text without such a glyph, which is nearly all of it, not a single break
  moves.
  - **And a prompt block carrying one no longer paints a row it did not count.** The block's rows
    were filled out to the full width with that same two-cell ruler, which does not merely pad past
    its width — it wraps — so one row of your prompt could quietly become two, out of step with the
    rows the scroll bar, the sticky header and a click were all counting. They are filled in the
    painted measure now, like every other row of the block.

- **An emoji in a pop-up row no longer knocks that row's columns out of line.** A row carrying a
  glyph like `⚠️` — a symbol plus the invisible codepoint that asks for its colour form — was
  measured as two terminal cells while most terminals draw it in one, so that row alone was padded
  a cell short: in the `/sessions` browser, the `/model` picker and the `/` dropdown, the fact
  beside such an entry started one column left of every other row's, and the offering read as
  ragged where it should read as a table. The staged-message band above the input box had the same
  arithmetic behind it, and left a one-column seam of the terminal's own background at the end of
  any queued line carrying one. Both now measure the way the terminal actually paints. A pane title
  is affected in the other direction and is fixed with them: it was being clipped with an `…` at
  widths that could in fact seat it whole, so a name the terminal had room for lost its tail.

- **A pop-up pane or a start-up card carrying an emoji no longer grows a row it never composed.**
  The same `⚠️`-shaped disagreement, one layer out: the pane's rounded box and the start-up card
  were each drawn by handing a total width to the styling library, and a width there does not merely
  pad a short row — past its width it *wraps*. So a row the terminal paints in exactly the room the
  box has could still measure one cell too wide to the library, and got folded onto a second line: a
  `/sessions` entry or a `/model` row came back cut in two, the pane stood a row taller than the
  window had budgeted for it — on a short terminal, exactly where a pane has no row to spare — and
  neither half of the split row reached the pane's right border. The start-up card did the same with
  the model name, splitting it across two lines and standing a row taller than its own logo. Both
  boxes now draw their own rows in the measure the terminal paints in, so one composed row is one
  painted row at every width.

- **A bare `/rename` on a long session no longer answers "could not name this session".** Naming a
  session is one small completion asking for one short title, and it capped that completion at 1024
  tokens — which on a thinking model is not a cap on the answer but a cap on the *thinking plus* the
  answer. Given the window a big session sends it — the opening request and the most recent ones —
  the model spent all 1024 of them reasoning about what to call the thing and came back with the
  answer field empty, and apogee, seeing an empty reply, said it could not name the session. It was
  not flaky: the same session failed the same way every time, and the silent automatic naming at
  first prompt was very likely losing meaty opening prompts the same way without ever mentioning it.
  The call now asks the server to skip the reasoning pass altogether — an eight-word title needs no
  chain of thought, and skipping it makes the call fast as well as reliable — and the cap is raised
  to 4096 as the backstop for servers whose chat template ignores that request, sized so that even a
  model that thinks anyway has room left to write the title afterwards.
  - **A server that refuses the request outright is not left worse off than before.** The "no
    thinking" ask rides an extra field, and a strict OpenAI-compatible server may reject a request
    carrying a field it does not know. When the rejection is a 4xx, the call is re-sent once without
    it, so a fix for "naming fails on big sessions" can never become "naming fails everywhere". The
    call still takes no retries in the ordinary sense: a rejection comes back before the server has
    generated a token, so that second attempt costs the next exchange no queue time.
  - **And if the budget does run out anyway, the note says so.** A bare `/rename` whose reply was
    all thinking and no title now reads *"the model spent its whole reply thinking and never wrote
    the title — try /rename <name>"*, which points at the model rather than sending you looking for
    a broken server. Every other naming failure keeps the note it had, and the automatic naming
    stays silent on all of them by design — it falls back to the first-prompt heuristic title.

- **Sending a prompt no longer throws the session chat away.** A new prompt landed at the top of an
  apparently empty transcript, with everything said before it out of sight above — the session
  padded blank rows below the newest prompt so the view could scroll it all the way up, and then
  scrolled there. The padding is gone: a prompt is appended at the tail of the history that is
  already on screen, the view follows it down as the answer streams, and the prompt climbs to the
  top row only once the answer beneath it has genuinely filled the area — where the sticky header
  takes over and holds it there, exactly as before. Scrolling up still detaches the view, and
  scrolling back to the bottom or sending a prompt still resumes following.

  Specced in `layout.md`'s opening paragraph.

- **The terminal's own scroll bar no longer sits beside apogee for the whole run.** Two separate
  things kept it lit. The first: apogee draws on the alternate screen, but that is named per frame,
  and the very first frame — the placeholder shown before the window's size is known — did not name
  it, so apogee opened on the **primary** screen and pushed a line or two into the scrollback. Every
  frame it emits now names the alternate screen. The second, and the one that actually kept the bar
  up: macOS Terminal.app copies the primary screen into its scrollback at the moment *any* program
  switches to the alternate screen, so a run that never wrote a byte to the primary screen still
  left the terminal with saved lines to scroll and a bar to show them with. apogee now claims the
  alternate screen itself, ahead of the renderer, and erases the terminal's saved lines in the same
  write — in that order, since an erase sent first only clears a scrollback the switch immediately
  refills.

  The trade is deliberate and worth stating plainly: **the shell scrollback from before the launch
  does not survive apogee starting.** No sequence puts the terminal's bar out and leaves the saved
  lines in place. The screen itself is still handed back — the primary screen is restored on the way
  out, so the shell returns to what it had.

- **Reading a file that talks about errors is no longer mistaken for a file that is missing.** The
  optional `read_loop` Mechanism watches for the model hunting for a file that does not exist, and
  it worked out which reads had failed by looking for words like `error:`, "not found" and "does not
  exist" **anywhere in the result** — but the result of a *successful* read is the file itself, and
  the files a coding agent reads are full of exactly those words. So one successful read of a
  perfectly ordinary source file counted as a miss, the workspace still looked empty, and the model
  was told: *"STOP. The workspace is empty. The file `store.go` does not exist because nothing has
  been created yet. Call write_file now."* — an invitation to overwrite a real file with a
  reconstruction from memory. Apogee now records, beside each tool result it commits, whether the
  tool actually reported a failure, and the Mechanism reads that instead of guessing from the text.
  Sessions saved before this release carry no such record, so their history is still read the old
  way, but only from the result's **first line**, where a failure message actually is. The
  Mechanism is off by default, so a stock session was never affected.

- **The syntax check no longer calls correct Python and Ruby broken.** The optional `syntax`
  Mechanism — and `autofix`, which judges a formatter's repair against the same check — read `//`
  as the start of a comment in *every* language and stopped reading the line there. In Python `//`
  is floor division, so `print(xs[len(xs) // 2])` looked like a line whose brackets were never
  closed, and the Mechanism handed the model back a correction saying its working code had an
  unclosed parenthesis — spending up to three extra requests in the turn inviting it to "fix" code
  that was already right, and leaving `autofix` unable to improve anything because the errors were
  never there. Ruby's `//` empty regex literal was the same, in the bracket count and in the
  "looks truncated" check. `//` now opens a comment only in the languages where it does, `/* … */`
  blocks are skipped instead of having their commented-out braces counted against the file, and
  the checker has valid-code coverage for every language it claims to understand. Both Mechanisms
  are off by default, so a stock session was never affected.

- **Plan mode no longer offers the model two tools it then refuses.** In Plan, `git_diff_range` and
  `diagnostics` were on the menu the model is shown — both honestly declare that they only read —
  but both shell out to a program (the system `git`, the Go toolchain), and a call that leaves the
  process is exactly what Plan does not allow, so the call came back refused. A small model reads
  that as a broken tool and tries again, or works around it; either way the Turn is spent on
  nothing. The menu and the refusal now decide from the same fact — what a tool actually reaches,
  not what it says about itself — so Plan shows only the tools it will really run: reading files,
  listing, searching, diffing what is already on disk, asking you a question, showing you a
  document, and delegating to a sub-agent (which inherits Plan, so it is read-only too). Nothing
  else changes: on every other rung the model still sees the whole toolbox, and no tool's
  permission moved.

  See the 2026-08-02 amendment to
  [the confinement execution contract](docs/design/confinement-execution-contract.md) §4.

- **Clicking a pop-up pane no longer copies text you cannot see, and a short window no longer
  pushes the input box off the screen.** The panes that open above the prompt — the approval and
  ask prompts, `/sessions`, `/model`, `/server` — are painted over the bottom of the chat area, but
  the mouse was still mapping those rows to the chat lines hidden underneath. On an 80×24 terminal
  a click on the approval pane's top border quietly started a selection in the transcript, and
  dragging down the pane put text that was nowhere on screen onto your clipboard with a
  `copied 19 chars` confirmation — while eating the click you meant for the pane. Those rows now
  belong to the pane and to nothing else. Separately, the `/sessions` browser and the pickers sized
  themselves without asking what the window could spare: eight saved sessions on a 20-row terminal
  composed a 21-row frame, and on a 12-row one a 21-row frame, shoving the input box and the footer
  clean off the alternate screen — easy to hit in a split tmux pane. Every pane now takes its row
  budget from the same place the prompts already did, and that budget can shrink to nothing: on a
  short window the pane gets smaller and the chat area gives way, never the box you type into.
  That last part now holds for the panes that carry prose too — the approval and ask prompts. They
  kept one line back for the question or the approval reason no matter how little room was left,
  which made them one row taller than the smallest window a pane fits in at all, so on a 12-row
  terminal the input box went off the bottom again while `/sessions` fitted. A pane with nothing
  left to spend now shows no prose at all and shrinks to the four rows every pane shares — its
  border, the tool name or the question's heading, and the key legend you act on.
  - **It still tells you what it is not showing.** Shrinking to those four rows briefly meant that
    on a 12-to-15-row terminal an approval prompt appeared with its reason and its arguments simply
    gone and nothing saying so — you were ruling on what a tool may do against text the pane had
    dropped quietly. The `… (+12 more lines)` count now outranks the prose it counts: with one row
    left it is the whole body, and with none left it moves up beside the pane's name, so a
    half-height tmux pane reads `approve write_file?  … (+12 more lines)`. The window can take the
    text away; it cannot take away your knowing there was some.
  - **…on a narrow pane as well as a short one.** A half-height pane is usually a half-width pane
    too, and the count moved onto the title row only to be cut off the end of it: at 42 columns and
    below the tool name filled the row and the elision was silent again, on exactly the split
    terminal the whole arrangement is for. The title row is now fitted to the pane's width instead
    of being cut down to it, and it gives way in the order you read it in — the count keeps its
    place and loses its wording first (`approve write_file?  … +12`), and only past that does the
    **name** shorten (`approve writ…  … +12`). The number is the last thing on the row to go.
  - **…and it says what it is not showing about the CHOICES too, not just the prose.** The count
    covered the text a pane dropped but never the entries: on a 12-to-16-row terminal the ask
    prompt appeared with all four of its answers gone — while the key legend still read
    `↑↓ select`, inviting you to pick between choices that were nowhere on the screen — and
    `/sessions` listed none of its eight saved sessions with nothing to say it was holding any
    back. Both now count them on the title row in the same marker the prose uses
    (`saved sessions  (all workspaces)  … (+8 more lines)`), shedding its words on the same narrow
    ladder. Entries merely scrolled out of a window the pane *did* get are untouched: they are one
    keypress away, and a marker for them would cost a row of the very list it describes.
  - **…and the `/` and `@` suggestion menu, the last pane still exempt from all of it.** Every other
    pane now asks the window what it can spare; the dropdown still asked for its eight rows whatever
    the window had. So a bare `/` on a 12-row terminal — the whole verb table, in a pane wanting 12
    rows for itself with 8 rows of chrome underneath it — pushed the input box you were typing into
    clean off the screen, which is the one pane you are most likely to have open on a small window,
    because it opens while you type. It now shrinks like the rest: fewer rows where there is less
    room, the row you have arrowed onto always among the ones shown, and on a window with no rows to
    give, the menu counted onto the title row (`commands and skills  … +14`) instead of an empty
    pane under a hint still offering `↑/↓ select`.
  - **…and now the panes size themselves against each other, not one at a time against the whole
    window.** Every pane above was measuring the window as though it were the only thing in it —
    which it usually is, and is not the moment you type `/` while messages are queued. The band of
    staged messages above the box was not in the budget at all, so a dropdown plus that band composed
    a frame four rows past the bottom of a 12-row terminal, an approval prompt plus the band one row
    past a **20**-row one, and five queued messages overflowed a 12-row terminal with no pane open at
    all. All of them now draw from one allocation made for the whole frame before anything is
    painted, and they give way in a fixed order: the chat area first, then the staged band — a
    reminder, whose count the status line carries anyway — then the `/` and `@` menu, then
    `/sessions` and the pickers, and last the approval or ask prompt, which is what the run is
    actually blocked on. The box you type into and the footer are never in the division. Whatever the
    allocation makes the band hold back is counted in the band's own `… 4 more queued` marker, the
    same one its three-row cap already used, and a window too short for even that leaves the count to
    the status line's `N queued`. The upshot: no combination of panes, band, window size and message
    length composes a frame taller than your terminal, at any size the frame fits in at all (its own
    floor is eight rows — see below) — the property is now checked with every pane opened both alone
    and beside a live queue, at every draft length.
  - **…and an approval you are being asked for is never missing from the screen.** That one
    allocation had one thing outside it: the editor box, which grows with what you type. So a
    half-written three-line message plus a 12- or 13-row terminal left the frame under four rows
    above the box, and the approval prompt — last in the give-way order, and the one thing that
    order exists to protect — was not drawn **at all**. All you saw was `approval needed` on the
    status line, without even the name of the tool, while `a`, `d` and `s` were live and would
    answer a prompt that was nowhere on the screen. The editor's **extra** rows now give way to it:
    the box always keeps a row, always shows what you were typing, and everything it grew past that
    is the prompt's if the prompt needs it. Nothing else takes the box's rows — `/sessions` and the
    pickers own the keyboard while they are open, and the `/` menu is completing the very draft it
    would shrink. The same holds for the answer you type into a question's borrowed box: the
    question stays on the screen however long the answer gets.
  - **…and a suggestion menu no longer sits, frozen, beside that decision.** A `/` or `@` menu left
    open while the model worked survived into the approval or question that interrupted it, where
    nothing could filter it, dismiss it or accept from it — those keys belong to the decision — yet
    it went on taking rows from the pane that decision is drawn in. It is now closed when the prompt
    arrives.
  - **…and a queue the window is too small to show still says so at any width.** When the band of
    staged messages is dropped entirely, the status line's `N queued` is the only thing left saying
    anything is waiting — and that line was composed at full length and then cut to the window, so
    at 20 columns the running phrase pushed the count off the end and a short, narrow terminal
    showed neither. The line is now fitted to the window in the order you read it, exactly as a
    pane's title row is: the count keeps its place and the phrase shortens around it
    (`⣾ read… · 5 queued`).
  - **…and neither does the message you are typing, however long it gets.** Everything above put the
    panes, the band and the chat area into one row budget and left the **editor box** out of it
    except when a decision prompt was up. So with nothing open at all, a six-line message on a 12-row
    terminal composed a 14-row frame and a ten-line one overflowed every terminal under eighteen
    rows — the input box and the footer pushed off the alternate screen by the very thing you were
    typing into. The box is now bounded by the window as well: five rows on a 12-row terminal, one
    row on an 8-row one, with the chat area paying for them exactly as it pays for a pane's.
    **Nothing you typed is touched** — past the cap the box is a window onto your message that
    scrolls with the cursor, so the line you are writing is always the one you can see, and what is
    out of sight is counted on the box's own top border in the same marker the panes use
    (`╭─ … (+5 more lines) ───╮`, and `… +5` where the window is too narrow for the words).
  - **…and the frame now fits every terminal down to its own floor, which is eight rows.** Below
    twelve rows no pane is drawn, but the frame itself went on being one row too tall all the way
    down: on an 8-row terminal it composed nine rows while completely idle, because the chat
    viewport was floored at one row whether or not the window had paid for it. Eight rows is what is
    left when the chat area is gone — the blank line, the `▔` rule, the status line, the box's
    border and its one row, and the footer's three — and none of them may give way, so eight is the
    floor rather than a budget. At eight the frame fits exactly; below eight it composes its floor
    and stops there, so a long message, a queue or an open pane can no longer make an already-too-
    small window worse. It is stated in `layout.md` rather than papered over by clipping the frame,
    which would have made the whole property trivially true and hidden the next real overflow.

- **A question from the model no longer eats the message you were half-way through typing.** When a
  tool asks you something, apogee borrows the input box for your answer and empties it first — and
  what it emptied out was simply gone, even though it was the one thing in the interface that cannot
  be reconstructed from anywhere. Whatever you had typed is now put aside and handed straight back
  the moment the question lets go of the box: after you answer it, and after a question that dies
  with its exchange (an `esc` stop, a fault) too. On that second path a half-typed answer is kept as
  well, below the message it interrupted — neither half of what you typed is thrown away.

- **Stopping with Esc no longer swallows a message you queued while the model was working — and no
  longer puts one the model already received back on the queue.** A message typed mid-task is
  delivered at the next tool-round boundary, and Esc scraps the exchange it was delivered into — so
  a queued message that happened to go out in the seconds before you pressed Esc was thrown away
  with it: gone from what the model remembers, gone from the queue, and left in the scrollback as a
  `⧖` line claiming a delivery nothing had a record of. The counter said `1 queued` after some stops
  and nothing after others, on timing you cannot see. The queue now holds exactly what the model
  never received. A message still waiting when the stop lands **stays queued** under the usual hold
  note (`⏎ sends them`, and Backspace still takes it back into the editor), because a stop already
  on its way no longer hands anything to an exchange that is about to be scrapped. One that did
  reach the model goes **with** that exchange, dropped from the conversation like the rest of the
  scrapped work — sent is sent, so it is not offered back for a second send you never asked for —
  and its `⧖` block stays in the scrollback beside the `cancelled` note as the record of what the
  model read before you stopped it, exactly as a stopped answer's half-written text does. An
  exchange that ends **on its own** is untouched: what it delivered is history.

  See the 2026-08-02 and 2026-08-03 amendments to
  [ADR 0025](docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md).

- **Loading a Launch profile no longer leaves a second heartbeat running, or moves the session from
  the wrong place.** Every `/model` profile load added one more upstream check to the session: after
  one load the server was polled twice per interval, after two loads three times — needless traffic
  against a single-slot local server, and it also halved the delay before the footer says *server
  offline*, so a brief hiccup flipped the display (and refused your next message) in about half the
  time it is meant to take. The load's own follow-up check now retires the one already running, so a
  session polls exactly once per interval however many profiles you load.
  - **And a load that moves the session now commits that move at a safe moment.** When the profile
    you load lives on a different server, apogee re-points the whole session at it. That re-pointing
    used to happen inside the background load — which, on a large model, blocks for minutes — while
    the heartbeat could be re-binding the session at the same instant from the other side. Nothing
    ordered the two, so a server that swaps its model underneath you mid-load (Ollama loading on
    demand, a second client, an action in the LM Studio window) could leave the session pointed at
    one server holding another one's settings. The move is now handed back by the loader and
    performed at the same quiescent point every other binding change uses, and a binding observed
    while a load is in flight waits for that point instead of racing it. A load that fails to move
    the session says so and leaves it exactly where it was.

  See the 2026-08-02 amendment to
  [ADR 0029](docs/adr/0029-the-launcher-actuates-local-servers-and-the-beat-completes-every-move.md).

- **A session on a server that does not advertise its context window no longer wedges when the
  history gets long.** When apogee cannot discover the window and you have not set `context-window:`
  yourself, none of the size bounds can do arithmetic — and the last line of defence, the emergency
  fold that summarizes the history after the server rejects an oversized request, was rendering the
  *whole* conversation into its own summary request. That request then overflowed exactly like the
  one it was sent to rescue, so every message you typed cost two rejected round-trips, printed a
  "context window exceeded" error and abandoned the turn, for the rest of the session — the only way
  out was `/clear` (and `/compact` failed the same way). With no window known the summary now carries
  a conservative fixed budget of history — small enough to fit even a 4k-token server, large enough
  to summarize from — so the fold works and the task continues. Nothing changes for a session whose
  window is known.
  - **And when recovery genuinely cannot help, the error says what to do about it.** Where no window
    is known, the give-up message now ends by naming the remedy — set `context-window:` (in tokens)
    in your config, or use a server that reports the window — instead of leaving you with an error
    that will repeat identically on every message. The server's own message still comes first, and a
    session that knows its window keeps the exact wording it had.
  - **The three other size bounds work there too, instead of standing down.** Rescuing the fold was
    not enough on its own: one large file read still went into the history whole, and the fold keeps
    the most recent message no matter what, so the very next summary attempt choked on it and the
    session wedged again. Without a known window apogee now applies the same conservative assumption
    everywhere it applies it to the fold — a single tool result that would not fit is shortened to
    its head and tail (with a note telling the model to re-read the part it needs), a long history is
    summarized at the next quiet point rather than growing until the server rejects it, and a
    conversation that clearly cannot fit is folded before the request goes out rather than after.
    That last one also brings back the only protection against a server whose rejection apogee cannot
    recognise as an overflow at all. What each of those bounds weighs is the conversation itself —
    never the fixed weight of the tool list and the system prompt, which ride every request and which
    no amount of summarizing can shrink; weighing those too would have folded your history away every
    single turn, on a conversation of four short messages, without ever getting under the bound.
    **The trade-off is deliberate:** if your server has a *large* window it never advertised,
    apogee now treats it as a small one and shortens things it did not have to — so if you are on a
    big model whose server is quiet about its window, set `context-window:` and everything sizes to
    the real number again. The compaction notice you may see in that situation names the same key.
    A session whose window is known is completely unaffected.

  See the 2026-08-02 amendments to
  [ADR 0018](docs/adr/0018-context-overflow-recovers-structurally-the-emergency-fold-and-one-retry.md).

- **A sub-agent that dies mid-task now says so, instead of reporting success.** When a delegated
  sub-agent was cut off by the server — a local model restarting, a connection dropping, a context
  overflow it could not recover from — the delegation still came back to the main model as a
  completed tool call. What it carried was whatever the sub-agent happened to have said last, which
  is typically its opening narration (*"starting on it — reading the entry point first"*), or a bare
  *"completed with no final message"* placeholder. The main model then built on work that was never
  done. A cut-off delegation is now reported as a failure that names the fault, so the model can
  retry or tell you, and the error you already saw in the transcript matches what the model was
  told. The same failure also used to count as *productive* work for the self-regulation machinery,
  clearing every mechanism strike and lifting a tripped Turn Budget on the strength of a request
  that failed; it now counts as the failure it is. A sub-agent that genuinely finishes, and one you
  stop with Esc, behave exactly as before.

- **Tightening the mode mid-run now reaches sub-agents of sub-agents.** Shift+Tab down to Plan
  while a delegation is running is meant to reach everything still working: a sub-agent runs a whole
  exchange inside one of the parent's tool calls, so the mode you set has to catch it in flight. It
  did — but only one level down. A sub-agent that had itself delegated further kept running on the
  mode it was spawned in, so the deepest worker went on auto-approving writes after you had already
  put the session in Plan, and its privileges ended up wider than those of the agent that spawned it.
  Each level now composes its parent's *effective* mode rather than the parent's own, so a tightening
  anywhere above reaches every level below it, at any depth. Loosening is unchanged and still
  impossible mid-flight: no sub-agent can rise above the mode it was spawned under, whatever you
  cycle the top-level session to. See the 2026-08-02 amendment to
  [ADR 0013](docs/adr/0013-the-sub-agent-orchestrator-is-the-recursion-point-with-isolated-live-guard-state.md).

- **Naming a session can no longer roll the conversation back a whole turn.** Saving your session
  and setting its name are two different writes to the same file, and they went out independently:
  the per-turn save replaces the record wholesale, while setting a name *reads* the record, changes
  the title and writes it back. Run both at once — which is exactly what happened, since automatic
  naming is on by default and its answer lands beside a save — and the name write puts back the
  version of the record it read a moment earlier, discarding the turn the save had just stored.
  Your engine state and your scrollback both revert one turn, which is precisely what the per-turn
  cadence exists to prevent; a probe over the store lost the newer record in a quarter of runs. The
  same window was open to a `/rename` typed while a save was running, and to deleting a session from
  `/sessions`, which could remove a record an already-dispatched save then re-created.
  - **Every write to a session record now goes through one queue.** A save, a rename and a delete
    can never overlap; saves still collapse latest-wins, so a burst of turns is still one write plus
    one waiting. The store itself also serialises the three, so a rename's read-modify-write is
    atomic against a save whoever asks for it.
  - **A name that answered before your session first reached disk is no longer lost.** apogee mints
    a session's id at the *start* of its first save, so a name arriving in the window before that
    file exists was renaming a record that was not there yet — and was dropped without a word. It is
    now held and applied at the next successful save.
  - **Quitting and `/clear` write through that queue too, so a name you just set survives them.** The
    closing save both do — the one that catches the notes made after your last turn — went straight
    at the file, outside the queue. Landing in the instant after a rename it wrote the *old* title
    back, so a session you had just named through `/sessions` or `/rename` could be back under its
    first-line name the moment you quit. A quit now waits for the writes it asked for to land before
    the program exits, which also means a rename or a delete you requested on the way out is no
    longer abandoned half-done.
  - **`/clear` no longer files the outgoing conversation twice.** Closing a session and starting a
    fresh one is a save followed by retiring the old session's id. If a save was still on its way to
    disk when you typed `/clear`, it arrived after that retirement and was written as a **brand-new
    session** — so `/sessions` listed the conversation you had just closed twice, and the fresh
    session then carried on updating the duplicate under the old one's name and start time. The
    retirement now waits its turn in the same queue, so one conversation is one record.
  - **Resuming a session can no longer overwrite it with the one you were leaving.** Picking a
    session in `/sessions` switches which file gets saved from then on, and that switch used to
    happen the instant the record loaded — ahead of any save still on its way to disk. That save then
    landed in the file you had *just resumed*, replacing its conversation with the one you had left
    and taking its scrollback with it. The switch now waits its turn behind the writes it belongs
    after, so the outgoing conversation finishes saving into its own file first. Nothing you see
    changes: the resumed session paints straight away, as it always did.
  - **Deleting the session you are in no longer leaves a duplicate of it behind.** Deleting the
    *current* session's file keeps the conversation alive in memory and gives it a fresh id for the
    next turn. That renumbering also went straight through, so a save already on its way to disk
    arrived after it and filed the live conversation as a **second** session — and the deletion then
    removed the wrong one of the two, leaving the copy you meant to delete in the list. It is queued
    now, in the order you asked for it: everything still pending, then the renumbering, then the
    deletion.

- **A mechanism that cannot be enabled no longer reports itself as `apogee: apogee: …`.** When a
  mechanism named for arming collides with one already in the registry — the same ID twice, or an ID
  a host had pre-registered itself — construction fails loudly, as it should. But the failure wrapped
  the registry's own rejection in a *second* `apogee: ` prefix, because by house convention a returned
  error already carries one, so the printed line read `apogee: enable mechanism "validate": apogee:
  mechanism ID "validate" is already registered`. It now leads with the registry's prefixed rejection
  and appends the context, reading `apogee: mechanism ID "validate" is already registered — while
  enabling mechanism "validate"`. Which failures refuse construction is unchanged; only the wording is.

- **A library store that could not be read no longer reports itself as `apogee: apogee: …`.** When
  `library.json` is corrupt or unreadable, apogee degrades to an empty store and says so on stderr
  rather than blocking startup — but the notice wrapped the store's error in a *second* `apogee: `
  prefix, because by house convention a returned error already carries one. The notice now leads
  with the store's own prefixed error and appends the consequence, so it reads `apogee: decode
  library store "…": invalid character 'o' … — library store degraded to empty`: one prefix, the
  cause first and what apogee did about it last. Nothing about the degrade itself changes — a
  broken store still falls back to empty, still injects nothing, and still never blocks a run.

- **A file, a filename or a tool argument can no longer turn the rest of your screen into a
  clickable link.** apogee already stripped terminal escape sequences out of untrusted text, but it
  did so one producer at a time — and the list had holes. A tool call's **target** (taken verbatim
  from the model's own arguments, and painted on the status line the moment a call is announced,
  before any approval prompt), a tool **result's** summary and output lines, a recovered **error**
  notice, the `/skills` catalogue, the autocomplete dropdown's skill and file rows, the
  `resumed: …` note and the automatic model-rebind note all went to the terminal unfiltered. That
  is reachable with no user action beyond normal use: a cloned repo controls the first line of any
  file the model reads, the first line of any command's output, its own filenames and its own
  `SKILL.md` front matter. It matters because the chat is drawn through a cell buffer that
  **deliberately honours OSC 8 hyperlinks** and never closes one at a cell or line boundary, so a
  single unterminated link opener turns everything painted after it — the rest of the transcript,
  the input box, the footer — into one clickable link to an attacker's URL. Since the way apogee
  hands you a finished document is "cmd+click the path we print", that is aimed squarely at the one
  mechanism the presentation ladder always promises.
  - **Stripping now happens at the seams, not at the call sites.** Every note, error, approval
    record, tool card and popup row is sanitized where it enters the view, so a new producer is
    covered the day it is written rather than the day someone remembers to wrap it.
  - **The `@file` dropdown strips the path itself, not just the row you see.** A suggestion is not
    only displayed, it is *inserted*: accepting one writes it into the message you are composing. So
    the filename is cleaned once, up front, and the row and the text it inserts are now the same
    string — previously the row was cleaned and the insertion was not, which left the input box
    relying on the text widget's own filtering to catch it and made an accepted suggestion for such
    a file compare as "not yet typed out", so ⏎ re-inserted it instead of sending. The trade is that
    a file whose *name* contains an escape byte can no longer be referenced from the dropdown: the
    reference is reported as unresolvable and skipped rather than quietly read.

- **A repo's own dangerous-action rules can no longer cancel one of apogee's.** The rules that
  hard-refuse an `rm -rf /` or make a `sudo …` stop for your approval ship with apogee, and a
  project config is only ever allowed to *tighten* them — it can add a rule, never remove or
  loosen one, so cloning a repo cannot lower the floor apogee runs at. Reusing a shipped rule's id
  at a **stricter** tier slipped through that: it was treated as an in-place upgrade, so the
  replacement's pattern took the shipped one's place. A repo could therefore redefine
  `sudo-escalation` as "hard-refuse" over a pattern matching nothing and `sudo` would stop being
  noticed at all — dressing the removal of a rule up as a tightening of it. A stricter same-id
  project rule is now kept **beside** the shipped one instead of over it: the shipped pattern goes
  on matching everything it always did, and a call the project's own pattern also matches is
  reported at the project's stricter tier — tightening that can only add severity, never subtract
  coverage. Equal-or-looser same-id rules are still dropped, as before. Nothing changes in a run
  today: the project/global rule merge has no config surface yet, and this lands ahead of the one
  parked in `TODO.md`.

- **On Windows, confining a workspace no longer reaches outside it through hard links.** The
  Windows fence works by marking the files in your workspace low-integrity for the duration of the
  run, and the pass that does it skipped symlinks precisely so it could never touch something
  outside the box. Hard links are not symlinks, and they were not skipped — but on NTFS every name
  of a file shares **one** security descriptor, so marking the copy inside your workspace marked
  the file everywhere else it is linked. That is not hypothetical: a `pnpm` project's
  `node_modules` is *entirely* hard links into the global package store under `%LOCALAPPDATA%`, so
  confining such a workspace quietly opened the user's whole package store to every low-integrity
  process on the machine — the agent's confined commands, but also browser sandbox children —
  outside the box and outside what the teardown journal recorded. A file with more than one name
  is now left alone: it is not marked, nothing is recorded for it, and — exactly like a file whose
  existing label cannot be read — that one path is simply read-only to the confined command
  instead of failing the session.
  - **Teardown skips them too.** Clearing the marks at the end of a run wrote a blank integrity
    setting over *every* file in the workspace, including the ones the pass above now refuses to
    mark — and on a hard link that write reaches the same shared record, so a label a store file
    carried in its own right was in the path of apogee's cleanup. Both passes now skip a file with
    more than one name: apogee neither marks it nor unmarks it. Skipping is not counted as a
    cleanup failure, so it never holds the teardown record open or surfaces as leftover state to
    report.

- **Confinement works on older Linux kernels again — and a confined command can move a file
  inside your workspace.** On Linux the workspace fence is landlock, and apogee asks the kernel
  for it in one go: here are the kinds of write I want fenced, here is the directory they stay
  allowed under. The list it asked for included one right no kernel before 6.2 has ever heard of,
  and a kernel that does not recognise a right in that list rejects the whole request — so on
  **Ubuntu 22.04, Debian 12 and RHEL 9**, the very kernels Auto was widened to reach, every
  confined command died before it started with `landlock_create_ruleset: invalid argument`, while
  apogee went on reporting that it *could* confine. Auto became a mode in which nothing runs. The
  request is now built from the landlock version the kernel actually reports, so each kernel is
  asked only for what it knows: older kernels fence everything they can (only truncating an
  existing file is beyond them), and 6.2 and newer are unchanged.
  - **Renaming across directories works inside the box now.** Landlock refuses to move or link a
    file from one directory to another unless it is *explicitly* told to allow it — and apogee had
    never told it — so a confined `git mv`, or any tool that writes a temporary file and renames it
    into place, failed even when both directories were inside your own workspace. That permission
    is now granted beneath every writable root, on the kernels that have it (5.19 and newer). It
    stops at the fence like every other write: moving a file *out* of the workspace is still
    refused.

- **A presented document is re-checked against your workspace on every fetch, and the doc server
  can no longer be pinned open by a stranger.** On a remote session, `present_document` serves a
  finished deliverable at a one-off URL carrying a random token (rung 2 of the presentation
  ladder). The file behind that URL was checked once, when the link was made, and re-opened by its
  plain path on every request afterwards — so anything that replaced the file *after* the link was
  printed, a `report.html` swapped for a symlink to `~/.apogee/config.yaml` or a `git checkout` of
  a branch carrying that name, was served instead, at a URL whose token the model already had. A
  grant is now the pair *(workspace root, name inside it)* and every fetch re-opens it through the
  same workspace fence the file tools use: a document that has become a link out of the workspace,
  a directory, or a file that is simply gone is the same bare 404 the server gives everything else.
  Editing a presented document still shows the new content on the next fetch, exactly as before.
  - **The port is bounded now.** It answers whoever can route to the box — a wrong token is a
    served 404 — and it bounded only how long a connection could take to send its *headers*. A
    peer could complete one request and then hold the connection open indefinitely, as many times
    over as it liked, until this process ran out of file descriptors and the agent's own file and
    network operations began to fail. Responses and idle keep-alives now have their own finite
    bounds, and at most 32 connections are held at once; past that, new ones are closed
    immediately rather than queued, and the cap frees as those connections end.

- **A session file you resume by path can no longer decide where apogee writes.** Every saved
  session is stored under its id, and that id is also the *filename* the per-Turn autosave, a
  rename and the `/sessions` browser's delete act on. The id was read straight out of the record's
  own contents with nothing checked but that it was non-empty, and `--resume <path>` accepts any
  readable session file — so a session file shipped inside a repo could declare an id of
  `../../.claude/settings` and have the next autosave write *there*, or declare the id of a session
  you already have and have that conversation overwritten. An id that is not a plain filename —
  a traversal, an absolute path, anything with a separator, a dot-prefixed or control-character
  name — is now refused wherever ids enter the store: a record declaring one neither loads nor
  lists (the browser skips it like any other file it cannot read), and no save, load or delete acts
  on one. And a session resumed from an explicit **path** now continues under a **freshly minted
  id**: its conversation, title and scrollback carry over exactly as before, but it becomes a new
  session of your own store instead of writing through the identity a file claimed for itself.
  Resuming by id — `--resume <id>`, the handle `/sessions` shows — still continues that session in
  place, `--continue` is unchanged, and every session already in your store, pre-plan files
  included, still lists, loads, renames and deletes exactly as it did.

- **Previewing a change to a huge file can no longer take the whole app down with it.** `view_diff`
  read the file it was given with no size limit at all and then built a comparison table with one
  cell per pair of lines — a table that grows with the *product* of the two line counts. Two 6,000
  line versions of a file already cost 288 MiB of it; a large generated file — a lock file, a CSV, a
  long log — runs to gigabytes and the process is killed, taking the in-flight Turn and anything
  unsaved in the transcript with it. `view_diff` is read-only, so it never asks before running, in
  any mode. Both sides are now bounded before anything is compared: the file is held to the same
  10 MiB ceiling `read_file` uses, the proposed content to the 512 KiB ceiling the write tools use,
  and a pair too large to lay out as a table comes back as a **diffstat with a sentence saying the
  line-by-line rendering was withheld** instead of being rendered at any cost. Every diff small
  enough to read is byte-identical to before.
  - **The file is also read through the workspace fence now**, like every other read in apogee: the
    path was checked and then opened again by name, so a symlinked component swapped between the
    two followed the swap out of the workspace — measured at 66 escapes in 2,000 calls under a
    racing swap. The same narrowing `read_file` took comes with it: an in-workspace symlink whose
    target is written as an *absolute* path is refused, and a refusal says the path is outside the
    workspace rather than reporting the file as missing.

- **`present_document`, `diagnostics` and `list_dir` now do their file I/O through the workspace
  fence, not just their path check.** All three checked the path you gave them against the workspace
  boundary and then acted on the *resolved string* — a plain stat, a plain directory read, a parse
  by filename. Each of those re-walks the path from scratch, so a component swapped to point outside
  the workspace between the check and the act was followed rather than refused: the narrow window
  the read and write tools had already closed, left open in the three tools the earlier sweep did
  not reach. All three are read-only, so they run without asking in *every* mode, Plan included.
  Each now opens through a pinned workspace root and works from that one handle — `present_document`
  confirms the document exists by an fstat of the descriptor it opened, `list_dir` walks by name and
  opens every subdirectory through the fence, and `diagnostics` reads the source once and hands the
  *bytes* to the Go parser instead of the filename. What you see is unchanged: the same listings in
  the same order, the same diagnostics, the same presented paths, and a link that stays inside your
  workspace is still followed exactly as before. A refusal now always says the path is outside the
  workspace rather than reporting the file as missing, so a blocked read can never be mistaken for
  an absent one.

- **`grep` no longer reads through a symlink that leaves your workspace.** Only the `path` argument
  you handed `grep` was checked against the workspace boundary — the files the search itself walked
  onto were opened by name, following any symlink it found. git stores symlinks verbatim, so a
  cloned repo could ship an innocuous `notes.txt` pointing at `~/.ssh/id_rsa` or `~/.aws/credentials`
  and have an ordinary search return the matching lines of that file as tool output. `grep` is
  read-only, so it runs without asking in *every* mode, Plan included, which made this the one read
  in apogee that reached outside with nothing to approve. Every file a search opens now goes through
  the same workspace fence the read tools use: a link that resolves outside the workspace is skipped
  and its content is never returned, while a link that stays inside the workspace — including one
  pointing out of the subdirectory you searched but still within the workspace — matches exactly as
  before, with the same reported locations. One narrowing comes with it, the same one `read_file`
  took: an in-workspace symlink whose target is written as an *absolute* path is skipped by the
  walk, because the fence resolves relative components only — the file it points at is still
  searched under its own name, so what is lost is the duplicate hit, not the content.

- **A repo's `AGENTS.md` can no longer be a symlink to a file outside your workspace.** Workspace
  context files — the `AGENTS.md` / `CLAUDE.md` conventions apogee folds into the standing system
  message of every request — were read by name with a plain read that follows symlinks. A cloned
  repo shipping `AGENTS.md` as a link to `~/.ssh/id_rsa`, or to your own `~/.apogee/config.yaml`
  with its upstream API key, therefore had that file's contents sent to the model the first time
  you started apogee in the clone, with nothing to see but a name and a byte count in the session
  notice. Context files are now opened through the same workspace fence every other read in apogee
  goes through: a name that resolves outside the workspace is refused, its content never reaches
  the model, and the session notice reports the skip out loud beside the files that did load. The
  check on the configured *names* is unchanged, and a normal context file loads exactly as before.

- **A reply cut off mid-stream no longer shows a raw `<|start|>assistant` marker.** When a gpt-oss
  answer was still arriving — or had been truncated by the token limit — the last, unfinished
  message kept its `<|start|>role` control token in the visible text, so the answer read as
  `<|start|>assistanthello` instead of `hello`. The unfinished tail is now cut at the same place a
  finished message is, so harmony's control tokens stay out of what you read, streaming or not.

- **A crash can no longer wipe what apogee has learned about your model.** The Library — the
  per-model record of corrections and behavioural observations built up across sessions — was
  rewritten whole, in place, on every single observation. Losing power or killing the process during
  one of those writes left a half-written `library.json`, and the next start quietly read it as an
  *empty* Library: not just the observation in flight, everything ever learned, with no error beyond
  a line on stderr. The store is now written to a temporary file beside it and renamed into place,
  the same way session records are saved, so the file on disk is only ever a complete one — an
  interrupted write leaves the previous Library exactly as it was.

- **Switching to a branch that does not exist no longer throws away your uncommitted work.** Asking
  the model to switch to a branch whose name also happens to be a tracked file or directory —
  `docs`, `tests`, `main.go` — did not fail. git read the name as a *path* instead of a branch and
  quietly restored those files from the index, so every uncommitted edit under them was gone: never
  staged, never stashed, nothing to undo. The tool then reported git's success text, so the model
  believed it had switched branch and you were never told anything had been discarded. The branch
  name is now pinned to the branch position, so the same call fails loudly with
  `fatal: invalid reference: docs` and your edits are untouched. The same pinning applies to
  `create`'s starting point. Switching to a branch that really exists is unchanged.

- **Approving a dangerous call "for this session" no longer waves through everything after it.**
  When the dangerous-action guard stops a call to force your approval — a `sudo …`, a
  `curl … | bash` — the prompt offers "allow for this session" like any other. Pressing it used to
  remember that answer under the tool's name, so every later ordinary call to that tool ran
  unprompted for the rest of the session: the one approval boundary Ask-Before and Allow-Edits have
  was gone, and you had authorised only the single risky call you were shown. For an MCP tool the
  remembered key is the whole server, so one forced prompt cleared every tool that server offers.
  A forced prompt now authorises exactly the call in front of you and remembers nothing —
  matching what it already did in the other direction, where a cached approval never suppressed a
  forced prompt. Ordinary "allow for this session" approvals are unchanged.

- **Opening a file no longer crashes apogee mid-answer.** With the Mechanisms on, any successful
  `open_file` could take the whole TUI down the instant the file had been read — the window
  vanished and the answer in flight was lost (per-turn saves survived, so the session itself was
  intact on reopen). The cause was in the bookkeeping that decides whether a Mechanism actually
  *changed* a tool's result: it compared the two results whole, and a successful `open_file`
  attaches a structured summary — how many lines, which ones matched — that cannot be compared
  that way. The comparison is done field by field now. It fired exactly when a Mechanism looked at
  a result and left it alone, which is what `error_enrichment` — shipped on by default in the
  gemma-4 set — does on every result that is not an error, so a plain successful read was enough
  to hit it. Nothing about when a Mechanism counts as having fired changes.

- **An untouched session no longer lands in your history.** Launching apogee, running a couple of
  slash commands — `/confine` to see where the fence is, `/skills` to read the catalogue, `/model`
  to check what is loaded — and then quitting used to file a record titled `Session 2026-08-01`
  reading `0 msgs`, because anything printed to the scrollback counted as a session worth keeping. A
  session is saved only once you have actually **sent a prompt** now, so a window you merely poked
  at leaves your `/sessions` list exactly as you found it. Nothing else changes: the record still
  appears the moment you send that first prompt, and every later save — after each turn, when the
  session goes idle, and on quit — behaves as before.
  - **One cosmetic exception, by design.** Reopening one of the oldest session files — from before
    scrollbacks were stored — and quitting without asking anything skips the usual refresh of its
    "last updated" time and context reading. The record itself is already on disk and nothing in it
    is lost.

  See the second 2026-08-01 addendum to
  [ADR 0022](docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md).

- **Reopening a session no longer leaves a mark on it.** The `resumed: <title>` line apogee prints
  when you pick a session back up — along with its "no scrollback recorded" variant, the note saying
  the session was interrupted mid-task, and the `context: AGENTS.md (3.1 KiB)` line naming the
  workspace files the session loaded — was being written into the session's own record, so every
  reopen added another copy: resume a session five times and its saved scrollback carried five
  `resumed:` lines, none of which you ever typed. The context line compounded even faster, because it
  is reprinted at *every* session boundary — each launch, each `/clear`, each reopen. Those notices
  are **display-only** now. You still see each one exactly as before, on every resume, because each
  is worked out fresh from the record being opened and from the files on disk as they are now; what
  changes is that the record keeps only the conversation. Existing session files keep loading
  unchanged — the stored format did not move — and the lines a session already collected replay as
  ordinary scrollback; no new ones are added to it.
  - **A window you never spoke in is no longer filed as a session.** Starting apogee in a repo with
    context files and quitting without asking anything used to leave a session behind whose entire
    scrollback was that one `context:` line. A launch with no conversation in it leaves your
    `/sessions` list untouched now — a rule since sharpened further, to the first prompt (see the
    entry above).

- **A session file carrying its own system message no longer doubles the system prompt.** apogee
  composes the system prompt fresh for every request and never writes it into a session's stored
  conversation, so no record apogee itself wrote holds one — but a hand-edited file, or one from an
  older build that stored it, does: reopening such a session put the stored copy *and* the freshly
  composed one on the wire, and the tool instructions folded into the stored copy, so the model was
  handed two sets of standing instructions with only one of them current. Any system message at the
  head of a restored conversation is dropped as the session loads now, leaving exactly one — today's
  — to go out. Ordinary session files are unaffected in every respect.

- **Pop-up rows line up in columns now.** Every overlay pane built its rows by gluing their fields
  into one string — `name — endpoint`, `Skill Name  what it does`, `a task · 5m ago · 3 msgs` — so
  the second and third tiers landed wherever the first happened to end and each pane read as a
  ragged list rather than a table. A row is a list of **cells** now, and the pop-up module lays them
  out as vertically aligned **columns**: each column as wide as its widest cell, adjacent columns
  two spaces apart, so summaries, endpoints, backends, context windows, timestamps and message
  counts each start at one shared screen column. It holds across every pane — the `/` command and
  skill menu (where a skill's description is aligned against the command summaries, so the merged
  menu reads as one table), the `/skill` picker, `/model` in both of its offerings, `/server`, and
  the `/sessions` browser.
  - **The columns do not move while you scroll.** Widths are measured over *all* of a pane's rows,
    not just the eight in the window, so bringing a long row into view never shifts the columns
    already on screen. They are measured in painted display cells too, by the same width authority
    the rest of the TUI uses, so a CJK or emoji cell claims what it actually occupies.
  - **A missing tier leaves a gap, not a shift.** Each pane has a fixed column schema and an absent
    tier is an empty cell that pads like any other, so a profile with no stated context window or a
    server with no `· current` mark cannot slide the tiers after it sideways — while a column *no*
    row fills collapses away entirely and costs the pane nothing. Separators lead the cell they
    introduce (`— llamacpp`, `· 32k`, `(:8080)`), so the separator glyphs line up as well as the
    words after them.
  - **Nothing that was one field changed.** `@` file suggestions, an armed rename buffer, the ask
    prompt and the approval prompt are single-cell rows and render byte for byte as before.
    Truncation is still whole-row — a narrow terminal drops the rightmost tiers with a trailing `…`
    rather than scrambling the alignment of what is left — and it is display-width aware now, so a
    row of wide runes is cut at the pane's edge instead of overflowing it. The visual contract is
    specced in `layout.md` (the Column contract, under "One overlay for 'which one?'").

- **Emoji no longer shift the chat out from under the scroll bar, the pointer or the caret.** A
  line carrying `⚠️` — any emoji that ends in the invisible VARIATION SELECTOR-16 — was measured
  as two columns by the layout code and painted as one by the terminal, and everything downstream
  inherited that one-cell lie. The TUI now has a **single width authority** that measures in
  whichever of the two measures the terminal is actually painting in: it starts where the painter
  starts and follows it to the grapheme measure the moment the terminal announces mode 2027, so
  what apogee counts and what you see are the same number on a modern terminal and on Apple
  Terminal alike. Three visible defects close with it:
  - **The scroll bar stays in its column.** A row with such an emoji dropped its `│`/`█` one
    column left of every other row's, so the bar came out with a notch in it beside exactly the
    lines that had emoji in them. Every row now composes to the same painted width — the frame is
    squared in the painted measure rather than by lipgloss's own padding — and the bar draws one
    straight line down the edge.
  - **A drag copies the text you highlighted.** The terminal reports a *painted* column; the
    selection was cut and measured in the other measure, so on a row like `❯ danger ⚠️ zebra` a
    drag onto `zebra` could copy `" zebr"` — the neighbouring glyph — and paint a highlight one
    cell wider than the text it stood for. Pointer, highlight and clipboard now address the same
    cells.
  - **The prompt caret lands under the glyph you clicked.** This completes the v1.1.0 `caretTo`
    fix, whose entry claimed it used "the same width the widget's own cursor math uses" — it did
    not: it accumulated width *per rune*, while the text widget wraps and positions its cursor
    over whole grapheme clusters. Clicking after an emoji put the caret one glyph past the
    pointer, and a wrapped line of them could paint an inline accent a row too high. Both mirrors
    of the widget's math now measure with the widget's own function.

  Everything else is byte-identical: every marker glyph the TUI draws (`✦ ❯ ┝ ┕ │ ⤷ • ▤ ⧖ ─ ▔ █`)
  measures one cell in both measures, so a transcript without emoji renders exactly as before. The
  decision, the two upstream defects worked around on the way, and the deliberate prompt-box
  exception are recorded in
  [ADR 0030](docs/adr/0030-the-tui-has-one-width-authority-and-it-mirrors-the-painter.md).

- **The prompt box is as tall as the draft it holds.** Its height came from a second guess at how
  the text widget wraps rather than from the widget's own wrap, and the two disagreed on roughly
  two drafts in five. Where the guess came in low — `hello world` in a five-column box wraps to
  four rows and it sized the box for three — the box came up short and the widget scrolled inside
  it, taking the top of what you had typed out of sight while there was still room on screen for
  the box to grow; where it came in high, the box carried a blank row under the text. The height is
  now read off the same mirror of the widget the inline `/skill` and `@file` accents are painted
  through, so the box, the accents and the widget all agree on where each row is. Tabs stay the one
  case both mirrors get wrong — the widget expands them (`TODO.md`).

- **Text stays inside the chat on very narrow layouts.** `layout.md` promises that no rendered
  line ever exceeds the width its block was given; a line of pipes or hyphens broke that promise
  at any width — the wrapper grows a word onto an already-full line when the break is a
  breakpoint rather than a space, so `| --- | --- | --- |` came back eleven cells wide at a limit
  of eight, and a hyphenated word like "sub-agent" did it at twelve. Visible where the usable
  width gets small: deeply nested sub-agent blocks (each level spends two columns on its rail),
  a table falling back to plain paragraphs, and genuinely narrow terminals — the over-long line
  spilled past the scroll-bar gutter. Wrapped lines are now re-broken to the limit whatever the
  wrapper returns, with the one exception nothing can divide: a single glyph wider than the whole
  limit gets a line of its own.

- **The dangerous-action guard no longer refuses a file because of what the file *says*.** The
  guard matched its rules against every string in a tool call's arguments — the body a write
  carries included — so writing a document that merely *mentioned* a guarded path was refused as
  if it were a write to that path: saving a note quoting `~/.ssh` came back as *"refused by the
  dangerous-action guard: write or delete under the SSH key directory (~/.ssh)"*, and because
  that rule is Tier-1 hard-refuse there was no per-call override to get past it. The literals sit
  in Apogee's own docs (`CONTEXT.md`, ADR 0012, `TODO.md`, this changelog), so the guard had made
  the project's security documentation unwritable — and a read-only `grep` for the same literal
  was refused as a write too. Rules now match the call's **action** — the tool, its target paths,
  its command lines and its code — while payload-bearing arguments (a file body, a replacement
  string, a search pattern, a commit message, a request body) are excluded from the inspected
  text. The exclusion is a deny-list over keys Apogee's own tools declare, so an unrecognized
  argument from an MCP tool stays inspected, and every key that decides what the host actually
  does still is: a shell heredoc writing to `~/.ssh` matches as before, because the heredoc lives
  in the command.
- **`make install` no longer reports success after putting the binary somewhere your shell cannot
  see it.** On a machine where no candidate directory was both on `PATH` and writable without
  `sudo` — the ordinary macOS case, where `/usr/local/bin` belongs to root and neither
  `~/.local/bin` nor `~/bin` is on the default `PATH` that `/etc/paths` builds — the target fell
  through to creating `~/.local/bin`, copied the binary there, printed `installed apogee -> …` and
  exited 0, with the fact that it was unreachable relegated to a trailing warning. Typing `apogee`
  then did nothing, on a build that had just said it was installed. Auto-detection now never
  installs off-`PATH`: it stops, prints each candidate with the reason it was passed over (missing,
  not on `PATH`, not writable), and gives the two one-line ways to finish — a `sudo install` into
  `/usr/local/bin`, or `make install PREFIX=…` with the line that puts that directory on `PATH`.
  The Go bin dir (`go env GOBIN`, else `$(go env GOPATH)/bin`) joins the candidate list, which is
  where a Go developer's `PATH` usually already points, so the common macOS case now just works.
  An explicit `PREFIX` is still honoured off-`PATH`, with the warning it always had. Installing
  over a copy of `apogee` that sits earlier on `PATH` — a stale `go install` binary in `~/go/bin`,
  say — now warns that the name still resolves to the other one, rather than leaving you to wonder
  why the version did not change.

- **Frontmatter a human reads without hesitation no longer costs a skill its place in the
  catalogue.** `SKILL.md` frontmatter was parsed as strict YAML and nothing else, so ordinary
  authoring slips deleted the whole skill: an unquoted `description:` containing `": "` (YAML reads
  it as a nested mapping), a tab indent, an unclosed quote. These files are shared with tools whose
  parsers are more forgiving, so the same skill would list in one and be missing here. Strict YAML
  is still the canonical path and a valid block keeps its exact YAML meaning — quoting, block
  scalars, comments — but a hard YAML failure now falls through to a line-by-line `key: value` scan
  that recovers the author's intent. The recovery is bounded so it cannot invent meaning: first
  declaration wins, an unmodelled key (`metadata:`, `allowed-tools:`) ends the current field rather
  than being folded into it, and a block with no recognisable key is still skipped — reported with
  the original YAML diagnosis, which names the line and the fault.

- **A stray blank line above the opening `---` no longer loads a garbage skill.** The fence had to
  be the very first thing in the file; one leading blank line and the frontmatter was not
  frontmatter, so the file fell through to the plain-Markdown path and produced an entry whose name
  and summary were both `"---"` — the fence itself, sitting in the picker. Leading blank space is
  now skipped before the fence. Only whitespace, so a genuine plain-Markdown skill still takes the
  fallback path it is meant to.

- **A skill apogee refuses to load now says so, instead of just not being there.** Discovery is
  deliberately soft — one malformed `SKILL.md` must never sink the whole catalog — but the reason
  it skipped a file was assembled and then thrown away at every call site, so a skill with (say) a
  stray `": "` in its unquoted `description:` was indistinguishable from one that did not exist:
  absent from the picker, absent from `/skills`, and no error anywhere to search for. The failures
  now ride **on** the catalog beside the skills that loaded (`Catalog.Skipped`), so the snapshot the
  picker lists and the reasons `/skills` prints always describe one scan, and `/skills` closes with
  a *found but not loaded* section naming each skill, why it was refused, and the file to go and
  fix. Dropping the load error is now lossless, which is what makes the soft-skip behaviour safe to
  keep.

- **A launcher that binds `0.0.0.0` is now recognised as the server your session is already on.**
  A wildcard bind and a loopback dial are two spellings of one server, but three places compared
  them as plain strings, so on a launcher configured that way — the common local setup — the whole
  surface misread its own machine: `/unload-model` and `/stop-server` refused every session that had
  not itself performed a load, a load onto the profile already serving fired the *moved* fold and
  re-pointed the wire and re-announced the seed for a move that changed nothing, and a genuine load
  onto a wildcard-bound profile handed the session `http://0.0.0.0:<port>` as its endpoint —
  an address to listen on, not one to reach, and on Windows not connectable at all. One predicate
  now decides both compares: equal spellings, or equal ports with an unspecified launcher host and
  an endpoint host this machine answers as. A wildcard bind is not a claim on the LAN, so a peer
  address, a name this side cannot resolve, and a second port each still name two servers. The
  address a session is *moved to* takes the spelling this machine dials — a wildcard becomes the
  loopback of its own family, an explicitly configured host is handed over exactly as it stands —
  which is the same projection the picker's rows already carry, so the endpoint and the profile row
  naming it agree after a move as they did before one. `/unload-model` and `/stop-server` still
  address the launcher in the launcher's own spelling: matching is normalised, addressing is not.

- **A model the session moved to is no longer yanked back to the one `config.yaml` named.** The
  discovery hint — which of the models a multi-model server serves apogee means — was fixed at
  launch and never moved again, so on a server serving both, a session that had bound a *different*
  model would have the next heartbeat resolve the configured one, read it as a change, and rebind
  back to it within ten seconds. The hint now follows the binding: whenever a rebind commits, that
  is the model discovery asks about from then on. Pre-existing (it was reachable any time the
  heartbeat moved a binding off the configured model), and load-bearing for `/model` — without it a
  pick would have flapped back before you finished reading the note.

- **A skill invoked in a message typed while the model works now actually reaches the model.** The
  staging path parsed your line, then built the queued message **without** the skills it had just
  found — so a skill you named mid-run was silently dropped and the model never saw its
  instructions. The queued message now carries its skill ids (and merges them, first-seen, when
  several held rows go out together), and the loop resolves them at delivery exactly as it does for
  a message sent at idle.

- **A delivered mid-run message now shows the skills it invoked, exactly as a sent one does.** The
  `⧖` block written when the model picks up a queued message carried its text alone, so the violet
  `/token` accent that records what you invoked appeared only when the message happened to be
  flushed into a new turn instead — the same message, two different records, depending on timing.
  The delivered block now paints its tokens the same way.

- **The transcript now follows the model's output — and stays put where you scrolled to.** Every
  repaint used to re-pin the view to the last user prompt, so the moment an answer outgrew the
  screen it streamed out of sight and you had to scroll down by hand, again and again, to watch it
  arrive. The view now **sticks to the bottom**: while it is at the bottom, each repaint keeps the
  tail on screen as tokens land, with the prompt the answer belongs to overlaid on the top row as
  the sticky header. An answer that fits the screen still opens with its prompt pinned to the top,
  exactly as before — nothing about the short-reply look changes.
  - **Scrolling up detaches, and detached means still.** Wherever you scroll to is where the view
    stays; appended output no longer drags it away mid-read. Scrolling back down to the very bottom
    **re-attaches** and following resumes with the next token — there is no jump-to-bottom button
    to hunt for, the wheel or `PgDn` is the affordance. Sending a prompt re-attaches too, as do
    `/new`, `/continue`, resuming a session from `/sessions`, and a staged message going out: asking
    for something means you are done reading history.
  - A transcript shorter than the viewport can no longer get stuck detached — at that size the view
    is always at the bottom, so it always follows.
  - **Selecting the sticky-header row now copies the header.** A drag over the top row took the
    reply line the header was drawn *over* — the line the scroll offset put there, which you could
    not see — so copying the prompt off the header was impossible. Mouse selection and its highlight
    now read the same overlay-aware row→line mapping the header is drawn from, so the top row copies
    what is printed on it. Pre-existing (it held whenever you had scrolled up); following the tail
    made it the norm for any answer taller than the screen. Every other row is untouched.

- **An `@`-reference to a file whose name contains spaces now resolves — write it quoted,
  `@"docs/my plan.md"`.** The reference was cut at its first whitespace and the opening quote rode
  along into it, so `@"docs/plans/2026-07-23 - 04 - version-build-number-plan.md"` reached the loop
  as `"docs/plans/2026-07-23` and could never resolve: a workspace file with a space in its name was
  unreferenceable by any spelling. An `@` at a word boundary followed by a quote (`"` or `'` — every
  shell takes either, so apogee does too) now opens a **quoted** reference that runs to its matching
  quote, and ordinary prose may follow the closing quote; an unterminated quote runs to the end of
  that line and never crosses a newline, so a stray quote cannot swallow the rest of a multi-line
  message. There are no escape sequences — a name containing `"` is quoted with `'`, and vice versa.
  Bare `@path` references, emails (`foo@bar.com`) and a mid-word `@` behave byte-for-byte as before,
  and `@x` and `@"x"` are one reference rather than two. Resolution is unchanged: a reference still
  arrives at the loop as a plain workspace-relative path, read within the workspace fence and
  reported-and-skipped when it is missing.

  The `@` autocomplete speaks the same grammar. It keeps completing **across** the spaces typed
  inside a quoted reference (the overlay used to die at the first one), each row shows exactly what
  accepting it will insert (`@"my plan.md"` for a spaced path, `@path` otherwise), accepting splices
  the canonical double-quoted form whenever the path needs it — decided by the path, not by whether
  you opened a quote yourself — and a fully typed quoted reference submits on `⏎` instead of
  re-completing, in whichever of the two dialects you typed it.

- **Selecting text no longer dies the moment the model streams — and the transcript is selectable in
  every state.** apogee captures the mouse (for scrolling and for click-to-position), which turns the
  terminal's own click-drag selection off, so selection is apogee's to implement — and it dropped
  **every** transcript selection on **every** repaint. While a reply streamed that is once per token,
  so a drag died before it began and a copied highlight vanished a moment later. Selections now
  survive by a **keep-if-unchanged** rule: a selection lives on exactly while every line it spans is
  identical before and after the repaint. In practice you can drag-select a settled paragraph, keep
  extending it, and copy it while the model keeps writing underneath — and a selection over the
  still-moving tail drops honestly the instant that text changes, rather than silently pointing at
  something else. **What you copy is always exactly what you see**, because the copy slices the very
  lines the rule protected. Text becomes selectable again the moment the stream has passed it.

  Scope is now the same in every state: **transcript** selection works while idle, running, at an
  approval prompt, while the model is asking you something, and after an error. **Prompt-box**
  selection follows the box — wherever you may edit it (idle, answering a question, and now while the
  model works) a click positions the caret and a drag selects; at an approval prompt and after an
  error the box stays inert and the transcript covers copying. The terminal's own `shift`+drag
  selection is untouched and remains available as it always was.

- **An MCP server on `localhost` or your LAN now connects — it used to stop apogee from starting.**
  A `mcp-servers:` entry with an endpoint like `http://127.0.0.1:7331/mcp` or
  `http://192.168.64.1:7331/mcp` — the ordinary way to run an MCP server — was refused by the
  resolved-IP SSRF floor before any connection was made. Because connecting the configured set is
  all-or-nothing and an MCP failure is fatal, that one entry meant **apogee would not start**, with
  no way to allow it from configuration. That floor exists to stop the *model* pivoting to internal
  addresses; an MCP endpoint is one **you** type into your own `~/.apogee/config.yaml` and is never
  model-supplied, so it is now exempt from it — exactly as the LLM `endpoint:` already was. What
  does **not** loosen:
  - **Your own host allow/deny policy still applies** to the endpoint, and everything the model
    drives (`web_fetch`, `http_request`, `web_search`) keeps the blanket floor, unchanged, pre-flight
    and at dial time.
  - **The connection is pinned to that endpoint's own addresses.** A DNS rebind, or anything else
    that points that transport at a *different* private address, is still refused at dial. The
    exemption is the one address you named, not "private addresses are fine here". An endpoint whose
    addresses cannot be resolved fails the connect rather than dialling unpinned.
  - **Redirects are no longer followed on an MCP transport.** The MCP client had every part of the
    network tools' HTTP client except their no-follow redirect policy, so it auto-followed a
    redirect that could carry a vetted connection to a host the endpoint check never saw. Configure
    a redirecting server at the URL it redirects to.
  - **The endpoint handed to the MCP SDK is now the same string url-safety checked** (normalised
    once — trimmed, host lower-cased and IDNA-mapped, a trailing root dot dropped) instead of the
    raw configured text, closing the same check-one-string/dial-another gap already closed for the
    network tools.

  MCP tools themselves are untouched: an MCP tool still asks for approval in Auto, per server, as it
  always has. See [ADR 0012](docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md)
  (amendment 2026-07-26).

- **A network response cut short mid-body no longer reads to the model as a complete one.** The
  network funnel read the response body and **discarded the read error**, while its "response
  truncated" marker was raised by the 2 MiB cap alone — so a server that streamed slowly past the
  request timeout, or a connection reset mid-body, came back as a plain `HTTP 200 OK` carrying only
  the first chunk, with nothing to say the rest was missing. `web_fetch`, `http_request` and
  `web_search` now report a cut-short body as a failure the model can see (*"response from host
  <host> was cut short: …"*, host-only as every funnel message is), and a body cut short by the
  **caller's** cancellation is raised as a cancellation, so the Turn is rolled back instead of
  completing over a half-read page. The 2 MiB cap is unchanged and still a clean, marked truncation.

- **A network call no longer leaves an open socket and two goroutines behind for half a minute.**
  Every `web_fetch` / `http_request` / `web_search` call builds its own HTTP client, and nothing
  released the connection pool afterwards — so Go kept the call's connection, and the two pump
  goroutines that pin the whole transport with it, alive for the 30-second idle timeout after the
  tool had already answered. Network tools auto-run unattended in Auto, where dozens of calls in a
  Turn is ordinary, so a long agentic run accumulated a socket and two goroutines per call for no
  benefit (a fresh handshake was paid every time regardless). The funnel now drains the pool as the
  call returns. Nothing about url-safety changes: the dial-time SSRF control, the no-follow redirect
  policy and the timeout ceiling are untouched.

- **A network tool's timeout now covers its DNS lookup, so a black-holed hostname can no longer hang
  a Turn for minutes.** Before reaching out, every `web_fetch` / `http_request` / `web_search` call
  resolves the host to check it against the SSRF floor — and that lookup ran outside the request
  timeout, bounded only by the machine's own resolver configuration. A name delegated to a
  nameserver that silently drops queries therefore blocked for `timeout × attempts × nservers` (the
  default is roughly 10 seconds; `options timeout:30 attempts:5` in `/etc/resolv.conf` makes it
  minutes) **on top of** the HTTP timeout, and `http_request`'s own `timeout_seconds` did not bound
  it at all. The resolved timeout is now a single deadline **shared** by lookup, dial and body —
  one budget spent between the phases, not a fresh allowance handed to each — so a request that
  asks for one second takes about one second whatever the endpoint's DNS does, and the two-minute
  ceiling bounds a whole call rather than each of its halves. A budget spent in the lookup is
  reported to the model as a blocked URL (the call failed, the conversation continues); a
  cancellation by *you* is still raised as a cancellation, so the Turn rolls back.

- **`web_fetch` now shows where a refused redirect points, so a redirected page is no longer a dead
  end.** Apogee deliberately does not follow redirects — one could carry a URL-checked request on to
  an unchecked (private) host — and the justification has always been that the model can follow it
  *itself* with a fresh, fully re-checked call. It could not: the result carried the status line and
  the content type and dropped the `Location` header, so an ordinary `http`→`https` or
  trailing-slash canonicalisation handed the model `HTTP 302 Found`, an empty body and no way
  forward. A 3xx response now renders its target (`http_request` always did — it prints the whole
  header block). **Nothing follows anything**: the redirect policy, the pre-flight check and the
  dial-time SSRF floor are untouched, and the next call is checked exactly like the first. Because
  the target is text the *server* chose, it is neutralised on the way out — control, bidi-override
  and zero-width characters are stripped, whitespace is folded, and an oversized value is cut at 2 KB
  and marked, so a hostile server cannot spoof a URL you read or flood the model past the response
  cap through a header. `web_search` still reports a redirecting endpoint as status + host only,
  which keeps a configured search API key out of the answer.

### Security

- **A Mechanism firing at a hook point may now only spawn a process when the autonomy ladder says
  it may.** A hook runs between the loop's own steps — it never reaches the per-call resolution, so
  it has no verdict, no approval gate and no confinement box of its own, and anything it shelled out
  to ran outside all three. Post-response hooks now fire under an explicit **subprocess permit**
  carried on the context, and **its absence is a refusal, not a licence**: in Plan, Ask-Before and
  Allow-Edits nothing is granted, so a hook cannot run a command at all. In Auto it is granted with
  the same box a subprocess tool would get — the workspace root, your writable paths and network
  allowlist — or, with `confine-to-workspace` off, unfenced, which is what that flag already means
  everywhere else. A host whose kernel cannot enforce filesystem confinement grants nothing rather
  than falling back to an unfenced spawn. A sub-agent reads its **effective** mode, so a parent that
  tightens mid-delegation closes the child's hook-time surface too. The permit is installed at
  post-response only; every other hook point keeps the "may not spawn" default. Contract:
  `docs/design/confinement-execution-contract.md` §10.

- **A tool's blast-radius class is now decided by what it *does*, not by what it *says* about
  itself.** The per-call classification consulted a tool's own `ReadOnly()` declaration **before**
  any of the structural markers, so a tool that declared itself read-only took the read-only row and
  auto-ran in **every** mode — including Plan, and including Auto with no confinement and no
  approval — however much reach it actually had. The markers are unfakeable by construction (a
  network tool cannot carry the url-filter marker without routing through the guarded funnel); the
  declaration is a bare claim a tool makes about itself, and it won. Now every marker is consulted
  first and read-only is the **terminal floor**, reached only by a tool no marker claimed. Two
  consequences:
  - **A host-registered tool that reaches the network can no longer buy itself the read-only row by
    declaring `ReadOnly() == true`.** It takes a network class: url-filtered if it routes through
    Apogee's funnel, otherwise gated in Auto like any other unvouched-for network reach.
  - **`git_diff_range` and `diagnostics` are classified as the subprocess launchers they are.** Both
    write nothing — the declaration is honest — but one runs the system `git` and the other shells
    out to `go vet`, which compiles the workspace's source, resolves modules from `GOPROXY` and
    honours a `toolchain` directive in the repo's own `go.mod`. They previously ran raw, outside any
    confinement box, in every mode. In Auto they are now **confined** like `terminal` (and gated
    where the host cannot confine), on the middle rungs they **ask**, and in Plan a call is
    **refused** — they stay in Plan's tool menu, which is a convenience filter, but the class is the
    boundary.

  Tighten-only: no call that previously asked or was confined now runs freely, and the tools that
  carry no marker (`read_file`, `grep`, `view_diff`, `open_file`, `list_dir`, `ask_user`,
  `present_document`) are untouched. See `docs/design/confinement-execution-contract.md` §4
  (amendment 2026-07-26).

- **`present_document` no longer hands the OS an arbitrary model-named file to launch.** On a local
  desktop the tool auto-opens the presented document (rung 1 of the presentation ladder) with
  `open`, `cmd /c start ""` or `xdg-open` — and on every desktop it is the **extension** that
  chooses which program runs. The path was fenced to the workspace and had to be a regular file,
  but the *name* was the model's, so presenting `report.command`, `report.bat`, `notes.hta` or
  `report.desktop` executed it with the user's full privileges outside any confinement box (in Auto
  the model can write such a file in-workspace first; in Plan a checked-in `build.bat` in a hostile
  repo is enough). The launch is now bounded by an **allow-list of extensions an OS handler renders
  rather than runs** — documents, images and text (`.pdf`, `.docx`, `.md`, `.png`, `.csv`, `.html`,
  … ), deliberately wider than the browser-renderable set the remote rung serves, and with every
  office format whose container can carry a macro — `.docm`/`.xlsm`/`.pptm` **and** the pre-2007
  binary `.doc`/`.xls`/`.ppt`, which have no macro-free variant — and everything script-shaped left
  out. Anything else builds **no command at all** and degrades to the baseline rung: the path is
  still shown in the transcript for the user to open themselves, the tool result still reads
  `shown`, and the call is not an error.
  A configured `present.command` is unchanged — it names one application, so the extension selects
  nothing there. See [ADR 0019](docs/adr/0019-documents-are-presented-not-opened.md) (amendment
  2026-07-26).

- **A model-chosen file *name* can no longer smuggle a command through the Windows opener.** The
  allow-list above bounds which program the extension selects; on Windows the launch itself still
  travelled through a **shell**. The auto-open rung there is `cmd /c start "" <path>`, Go joins
  that argv into one command line quoting an argument only when it holds a space or a quote, and
  cmd.exe **re-parses** the joined line — where `&`, `|`, `^`, `<`, `>` and `%` are syntax. So
  presenting `report&calc&.html` from a space-free workspace path — an extension squarely inside
  the allow-list — read back as three commands and ran the middle one with the user's full
  privileges, outside any confinement box: the injection rode the rest of the name, not the
  extension. The Windows rung now refuses a path carrying any character cmd.exe can read as
  grammar — the operators, the `%`/`!` expansions (live even inside quotes / under machine-wide
  delayed expansion), an embedded quote, the `;`/`,`/`=` delimiters that would split an unquoted
  path into two `start` arguments, and control characters. A refused name builds **no command at
  all** and degrades exactly as a refused extension does: the path is still shown in the
  transcript and the result reads `shown`. Names with spaces or parentheses still open, macOS and
  Linux need no name bound (`open` and `xdg-open` take the path as one argv element, no shell),
  and a configured `present.command` is untouched. The Windows opener ships unexercised, so this
  closes the hole before it is live. See
  [ADR 0019](docs/adr/0019-documents-are-presented-not-opened.md) (second amendment 2026-07-26).

- **A URL is now normalised once, so url-safety judges the name the transport actually dials.** The
  guard parsed the whitespace-trimmed URL and lower-cased its host before matching the allow/deny
  lists, but the request was built from the string exactly as it arrived — and Go's transport
  applies its own IDNA mapping before connecting. Three spellings therefore reached a host the guard
  had judged under a *different* name: a **trailing DNS root dot** (`evil.com.` resolves exactly as
  `evil.com` and virtually every virtual-host server accepts it, yet it matched no `DenyHosts` entry
  spelled without the dot — one appended character defeated a denial), a **Unicode host**
  (`http://ⓖxample.com/` was checked as `ⓖxample.com` and dialled as `gxample.com`), and
  **leading/trailing whitespace** (checked trimmed, then unable to build the request at all). The
  network funnel now normalises once — trim, IDNA-map a non-ASCII host exactly as `net/http` does,
  lower-case, drop a single root dot — and both checks and requests that one form; the guard applies
  the same normal form on its own, so the MCP endpoint check is covered too. No shipped
  configuration populates `AllowHosts`/`DenyHosts` yet, so this was **latent rather than live** — it
  is fixed ahead of the config key that would make it live.

- **A URL that does not parse no longer carries an API key out to the model.** Every network-tool
  failure names the bare host and scrubs the request URL out of the cause, because that URL may hold
  a configured API key. url-safety's *"unparseable url"* reason defeated the scrubbing: it quoted the
  parse error back, and Go quotes a URL with `%q`, which **escapes** it — so a URL carrying an
  interior ASCII control character (the ends are trimmed; the middle is not) reached the model as
  `…?key=SECRET\x01x` with a literal backslash-x, which the redaction — searching for the real
  bytes — could not find. `web_fetch` and `http_request` take that URL straight from the model, so
  one control character was enough to read back any key a caller had put in it. url-safety now
  reports the bare reason and never quotes the URL at all, and the redaction additionally strips a
  URL's escaped spelling wherever an error text quotes one.

- **The SSRF floor now denies the NAT64 *local-use* prefix outright.** A NAT64 translator carries a
  real IPv4 destination inside an IPv6 address, so the floor decodes that embedded v4 and re-checks
  it against every private range — but it did so only for the well-known prefix `64:ff9b::/96`.
  RFC 8215 reserves a second one, `64:ff9b:1::/48`, *specifically* for translating to non-global
  (private) IPv4 space, and it was not covered at all: on an IPv6-only network running such a
  translator — a realistic enterprise or mobile setup — `http://[64:ff9b:1::a9fe:a9fe]/` reached the
  cloud metadata service at 169.254.169.254, and `[64:ff9b:1::7f00:1]` reached loopback, neither of
  which the same address under the well-known prefix could. The local-use prefix is **not** decoded
  the way the well-known one is: RFC 8215 fixes no translation prefix length, so the embedded v4's
  bit offset is the operator's choice (RFC 6052's `/48`, `/56`, `/64` and `/96` forms put it in four
  different places) and the leftover suffix bits are caller-controlled — a decode that reads one
  fixed offset is a guess, and a wrong guess reads a public-looking value while the gateway forwards
  to a private one. The whole `/48` is reserved local-use space with no legitimate public
  destination, so the **entire range** is denied, including an address embedding a public v4.
  Denied outright alongside it: 6to4 (`2002::/16`), the IPv4-compatible form `::a.b.c.d` and
  deprecated site-local (`fec0::/10`) — all obsolete, all able to front a v4 destination, and none
  a legitimate target for a coding-agent fetch. The **well-known** prefix `64:ff9b::/96` keeps its
  decode unchanged (RFC 6052 fixes it at `/96`, so the embedded v4 is unambiguously the low 32
  bits), and an address there embedding a public v4 still passes. The floor remains tighten-only and
  no other range moved.

- **The ask and approval prompts escape-strip all model-authored text before rendering.** The ask
  question and its choices, and the approval tool name, reason, and arguments, are stripped of the
  terminal escape (`ESC`) byte at the point they enter the popup, closing a gap where untrusted
  model output reached the screen raw. Stripping removes only the `ESC` byte, so the raw tool name
  the human approves is still shown verbatim.

- **`read_file` and `open_file` now read *through* the workspace fence instead of around it, closing
  a symlink-swap race.** Both tools checked the path first and then read it with a plain
  stat-and-read, which re-walked the path and followed symlinks a second time. A workspace component
  that already *was* a symlink pointing outside the workspace was refused by that check — that half
  was never exploitable — but a component swapped to an outside-pointing symlink **after the check
  and before the read** was followed, and the outside file was read. That is a swap a write-capable
  confined subprocess can perform, and in a racing swap loop the old path returned the outside file
  on a small fraction of reads, reproducibly. Both tools now open the file through a root pinned at
  the workspace directory — the mechanism the write tools adopted when the same race was closed on
  the write side (the security-review checkpoint after `v1.0.0`) — and the size check and the read
  share that one pinned handle: the size is taken from the very descriptor the content is then
  read from, so there is no window between them, and the same racing loop now returns the outside
  file **zero** times. The read itself is hard-bounded too, so a file grown past the size cap
  mid-call is refused with the same "file too large" message instead of being materialised.

  Three behaviour changes come with it:
  - **An in-workspace symlink whose target is spelled as an absolute path is now refused, even when
    that target is inside the workspace** — it reports `security: path resolves outside the
    workspace root: …` where it previously read the linked file. The pinned root resolves relative
    components only and cannot honour an absolute target, and the workspace fence is tighten-only,
    so this narrowing is kept deliberately rather than worked around. **Relative** in-workspace
    symlinks — as the named file or as a directory component — read exactly as before.
  - **A symlink-shaped escape now carries the pinned root's own detail** after the uniform prefix
    (`security: path resolves outside the workspace root: openat ssh/id_rsa: path escapes from
    parent`) where it used to carry the requested path (`…: "ssh/id_rsa"`). A `../` traversal or an
    absolute path outside the workspace is still caught before any descriptor is opened and its
    message is byte-identical to before.
  - **A read that fails after the size check passed now reports `file not found: <path>`** — the
    phrasing the write tools already use — instead of the raw OS error (for example `open
    /ws/socket: no such device or address`).

  The `@file` reference reader shares the same one-handle mechanism and cap discipline, and the
  raw error its ErrorEvent surfaces for a symlink-shaped escape now carries the same `openat`
  detail where it named the stat operation before.

  Everything else is untouched: a successful read, a missing file, a directory, an oversized file,
  and a traversal or out-of-workspace absolute path produce the same result and the same message as
  before, and an escape still surfaces as "outside the workspace" rather than being disguised as a
  missing file.

- **`http_request`'s response headers now reach the model neutralised and bounded, like
  `web_fetch`'s redirect target.** The tool rendered every response header sorted but **verbatim
  and uncapped** — and the header block sits *outside* the 2 MB response cap (the transport accepts
  a 10 MiB one by default), so a hostile server answering a one-byte body under a huge header block
  handed the model exactly what the body cap exists to refuse, and a header value carrying a bidi
  override, zero-width characters or a CRLF-folded fake status line landed raw in a block the model
  reads as the server's own facts. Every rendered header name and value now goes through the same
  neutralisation as `web_fetch`'s `Location` — control, bidi-override and zero-width characters
  stripped, whitespace folded so nothing can open a line of its own — with each value cut at 4 KB
  and the block as a whole at 64 KB, every cut visibly marked rather than silently shortened.
  Ordinary headers (`Content-Type: text/html; charset=utf-8`, a normal `Date`) render byte for byte
  as before, and nothing is redacted: a response header is response *content*. The request side was
  already bounded; this closes the response side. The funnel itself is untouched.

- **Network-failure messages now scrub the request URL in the spelling the request actually
  carries.** Every funnel failure names the bare host and strips the (possibly key-bearing) request
  URL out of the underlying cause — but the scrub matched the URL as the *model* spelled it, while
  the request itself is built from the normalised form (trimmed, host lower-cased and IDNA-mapped,
  root dot dropped). A cause embedding the normalised spelling of a divergently-spelled URL would
  therefore have ridden out, key and all, past a redaction searching for a string the message never
  contained — the same check-one-string/use-another asymmetry already closed on the guard/dial
  seam, left standing on the message seam. **Latent rather than live:** every shipped error that
  embeds a URL is a transport error whose URL is dropped wholesale before redaction, so no key
  leaked; the gap is closed ahead of any error shape that would make it real. Failure messages
  still name the host exactly as the caller spelled it, and no message wording changes.

## [0.10.4] — 2026-07-31

### Fixed

- **The sub-agent rail is continuous now, and it is orange.** The `│` line marking a sub-agent's
  work down the left of the chat broke at every blank row — and since each block is separated by
  one, the rail came out as disconnected stubs rather than one frame around the run. The separating
  row now carries the rail too, drawn as deep as both of the blocks it sits between, so a run reads
  as a single vertical-ruled section from its `⤷ sub-agent` label to its last line. The same rule
  keeps runs apart: the row above a second sub-agent call is bare, because that call's own block
  sits at the parent's level, so two sub-agent calls in a row are never joined into one. The rail
  and its `⤷` label are painted in the tool header's orange instead of dim grey — one tone for the
  whole sub-agent frame, matching the `✦` markers it encloses. A transcript with no sub-agent in it
  renders exactly as before, byte for byte.

## [0.9.4] — 2026-07-28

### Changed

- **Queued messages now read as one band above the input box.** The staged interjections waiting to
  go out — the rows shown above the box while the agent works, or held there at idle after a stop —
  used to sit flush against the left edge as bare faint lines. They now render as a group: each row
  is indented two columns into the same body column the status line's text uses, the whole band is
  painted faint on black **out to the full window width** so no strip of terminal background shows
  through past the text, and one blank black row frames the group above and below. The band joins
  the input box's own black interior directly beneath it, so the bottom chrome reads as one block.
  Order, the three-row cap, the `… N more queued` overflow marker (now inside the frame, indented
  and painted like every other row) and the status line's `N queued` readout are unchanged, and a
  **delivered** ⧖ block in the transcript looks exactly as before. With nothing queued there is no
  band and no frame. See `layout.md`'s *staged-interjection band* section.

## [0.8.0] — 2026-07-23

*Release version **reset from the `1.x` line to `0.8.0`.** The `1.x` numbering overstated
maturity for a pre-production tool, so the human-facing release version now sits in the `0.x`
range — initial development, and under SemVer `0.x` makes no stability promise. The reset changed
no code behaviour; `--version` and `/version` still carry the full build provenance. The
`v1.0.0`–`v1.7.0` git tags and their GitHub releases were removed. Note: `proxy.golang.org`
retains the old `v1.x` module versions immutably, so `go install …@latest` still resolves to
`v1.7.0` — this line is distributed from source / GitHub Releases, not `go install`.*

*Merge-plan **Phase 5 — cross-platform hardening & retirement** — the last open phase — closed
2026-07-22 (`docs/plans/2026-07-22 - 00 - phase5-cross-platform-hardening-plan.md`). Its
deliverable was "cross-compiled binaries for Win/Mac/Linux, **Auto confined on all three**", and
that is now true.*

### Added

- **`apogee --version`, from a single build-version source, now with a build number.** The binary
  reports its version from the top-level `VERSION` file, embedded verbatim via `go:embed` — the
  single source of truth for the release number, identical on every build path (`make build`,
  `go build`, `go run`, `go install`) with no `-ldflags` override that could drift. A
  build-provenance suffix is appended, rendered as `vX.Y.Z+<count>.g<rev>[.dirty]`: the short
  commit id and `.dirty` marker come from Go's own VCS stamp at runtime (`debug.ReadBuildInfo`),
  and the **build number** — the repository's commit count (`git rev-list --count HEAD`) — is the
  one field the runtime cannot derive, so a release build injects it via `-ldflags -X` (the
  `Makefile` computes it). A bare `go build` omits the number and reports just `+g<rev>`; the
  version *number* itself is always the embedded `VERSION` file. Cobra's `--version` flag reads the
  string via `cmd.Version`, and the same value is threaded to the TUI through the `Options.Version`
  seam (the renderer never imports the version facade), where the in-TUI `/version` command and the
  start-up box read it.
- **A one-time start-up box.** Launch now opens with a rounded card at the top of the transcript
  carrying the block-art `APOGEE` wordmark and the session's `host` / `model` / `version`. It
  reuses the prompt box's rounded border glyphs (`lipgloss.RoundedBorder()`) but drops the black
  fill — the same characters, a transparent self-closing card — and is seeded once as the first
  transcript entry, so it reflows on resize, survives `/clear`, and never redraws. Its `host`,
  `model`, and `version` come from the same `Options` seam the footer and `/version` read
  (`host` through a shared `hostDisplay` helper), so the box and the footer can never drift.
- **Auto mode is confined on Windows: the fence is a restricted low-integrity token, and the box
  is a label on the disk.** Windows was the last Phase-0 stub — `denyConfiner`, so `--mode auto`
  reported `{FSWrite:false, NetworkEgress:false}`, every terminal/`python_exec` call took the
  Approval path, and the degradation notice fired on every session. That was *correct* under
  ADR 0012 ("confine if you can, gate if you can't"), never a bug, and it is what this release
  ends. The two shipped backends fence by **path policy** — landlock takes a ruleset of
  path-beneath rules, seatbelt a profile with `allow file-write*` under the box's roots, and
  neither touches your disk. Windows has no facility of that shape: mandatory integrity control
  fences by **identity**, and nothing in that model takes "these paths are writable" as an
  argument. Everything below follows from that one asymmetry. **The fence** is a restricted,
  Low-integrity primary token handed to `SysProcAttr.Token`: the child runs at Low, every object
  carrying no explicit label is implicitly Medium with `NO_WRITE_UP`, so a write outside the box
  is denied by the kernel *before* the DACL is consulted — and because a process inherits its
  creator's token, the denial covers the whole descendant tree for free (the Windows equivalent of
  "the domain survives `execve`"). `CreateRestrictedToken(…, DISABLE_MAX_PRIVILEGE, …)` is defence
  in depth, not the fence — no restricting SIDs and no deny-only SIDs, which break ordinary
  programs and buy nothing the integrity level does not already give. **The box** is a mandatory
  label written on `WorkspaceRoot ∪ WritablePaths` for the run and **reverted on teardown** — a
  side effect on the user's disk that landlock and seatbelt do not have, accepted deliberately
  because it is the only way the box's writable half can be expressed at all, journalled per-PID
  under `<apogee home>/confinement/` against a crash, recovered at construction, and reported by
  `apogee probe host` when an interrupted run left one outstanding. There is **no helper process,
  no argv sentinel and no argv rewrite**: Linux needs its 42-line re-exec helper only because the
  CGO-free way to run code between `fork` and `execve` is to *be* a process that restricts itself,
  and Windows has no "restrict myself" API to mirror — so `Confine` sets the token and returns,
  `cmd.Path`/`cmd.Args` are untouched, and `maybeDispatchConfinedExec` gains no Windows arm.
  (`internal/platform/{confiner_windows.go,winconfine.go}`;
  [ADR 0020](docs/adr/0020-windows-confinement-is-a-low-integrity-token-and-the-box-is-a-disk-label.md);
  `docs/design/confinement-execution-contract.md` §9.)
- **What the Windows backend honestly claims, and where it stops.** Capabilities are
  `{FSWrite: true, NetworkEgress: false}` — **network egress is not claimed on Windows**, because
  the token fences the filesystem and nothing else, and a `ConfinementBox` carrying a non-empty
  `NetworkAllow` **fails closed** with `ErrConfinementUnavailable` (mirroring landlock's
  `networkDenyDecision`) rather than pretending a requested tightening happened. `AutoEligible()`
  stays FSWrite-only per ADR 0012, so Windows is Auto-eligible on that basis alone. The supported
  floor is **Windows 10 1809 / build 17763 / Server 2019** — the oldest branch under any
  servicing, at or above Go's own floor — read from the un-shimmed `RtlGetNtVersionNumbers`;
  **below it nothing changes**: `NewConfiner()` returns the deny backend and the degradation
  notice fires exactly as before. Honesty is split across two moments, the one structural
  difference from Linux/macOS: `Capabilities()` probes the facility **once at construction**,
  while a per-run labelling failure is a `Confine`-time `ErrConfinementUnavailable` that feeds the
  execution contract's forced-Gate path. A box root that cannot be labelled — or cannot even be
  *compared* (an unresolvable 8.3 short name, a device path, a drive-relative `C:work`) — fails
  closed; a failure on an individual descendant is tolerated, because one locked file becoming
  read-only to the child must not gate a whole session; symlinks and reparse points are skipped,
  since `SetNamedSecurityInfo` follows them and labelling one would mutate a target outside the
  box.
- **The Windows label walk no longer looks like a hang: a startup progress notice, and the walk is
  pre-warmed.** Labelling the workspace Low costs ~1 ms/object (ADR 0020 §2), so the *first*
  confined command in a workspace with a large `.git` or `node_modules` used to block visibly with
  no explanation — the click-through-frustration trap Auto was built to avoid. Under **Auto +
  confine-to-workspace on the Windows token backend**, launch now runs that one-time walk eagerly
  at startup, pre-alt-screen, after printing a one-line notice to stderr ("labelling the workspace
  … Low … a large .git or node_modules may take several seconds"); the first in-session `Confine`
  then hits the memo and no-ops. It is a pure **timing** change — *what* is labelled and the
  teardown revert are unchanged (semantics kept; pruning the walk was deliberately rejected) — and
  a genuine no-op everywhere else: the notice and walk fire only on Windows with `FSWrite: true`,
  so Linux/macOS startup is byte-identical. Real pruning of `.git`/`node_modules` is deferred to
  the parked box-local `%TEMP%`/toolchain-cache design.
- **`apogee probe` — the diagnosis command, in two halves with deliberately asymmetric cost.**
  Promised twice (as the confinement-diagnosis subcommand and as model capability probing) and
  blocked on a CLI that had no subcommands at all. `apogee probe` now prints the **host report**
  from the parent's own `RunE` — OS/arch, the Confiner backend and its capability matrix, the
  `AutoEligible()` verdict, the effective `confine-to-workspace` *after* the host acknowledgement
  is resolved, the workspace root and config home, endpoint reachability and the `/v1/models` +
  llama.cpp `/props` discovery outcomes reported **separately**, and an outstanding Windows label
  journal if there is one. It runs no agent, calls no model, and writes nothing — unlike the root
  command it does not even seed a starter config, which is pinned by a test — and it resolves
  flags, `APOGEE_*` and `config.yaml` exactly as a session would, so what it reports is what a
  session on this host would run with. `apogee probe host` is the same report under a named child,
  so a script never has to rely on a bare parent's semantics staying put. Because `/confine
  status` (TUI) and `apogee probe` (CLI) answer the same question, the selection and notice logic
  was **extracted, not duplicated**: `internal/probe`'s `BackendName` / `DegradedNotice` /
  `CapabilityLine` are the single source both render, so two views of one verdict cannot drift,
  and the host report closes with the startup degradation notice verbatim.
  (`cmd/apogee/probe.go`, `internal/probe`;
  [ADR 0021](docs/adr/0021-probe-is-two-halves-the-host-report-is-free-the-model-battery-is-an-explicit-act.md).)
- **`apogee probe model` — the capability battery, and the `ConfidenceMedium` fingerprint slot
  finally filled.** The model half is an **explicit act**, never a side effect of typing the bare
  noun, because it spends live model calls *and* writes: it asks the model to emit a native tool
  call, return a JSON object, and carry a tool result into a second call, then reports what it
  observed, an ordinal **capability tier**, and the model-profile knobs the findings suggest as
  **paste-ready YAML** (printed in the `offerNotice` tradition — `config.yaml` is never edited).
  It then records a versioned, owner-private (0700/0600) probe record keyed on endpoint +
  advertised label + timestamp, which `internal/library`'s resolver consults as the middle rung of
  the ladder its `fingerprint.go:40` comment reserved: **weights hash (high) → stored probe record
  (medium) → metadata label (low)**. Persistence is the point rather than a convenience — identity
  resolves through a pure offline call at startup, so a Medium tier that was never written down
  could never be observed. **Probing does not rename your model.** The behavioral tier promotes the
  **advertised label** to medium confidence and files the observed feature vector beside it as a
  separate **behavioral signature** (`probe:<battery>:<features>[:lp-<digest>]`) — evidence, never
  a match key. The first implementation minted a synthesised label from the features, which matched
  no Validated-set entry, no user alias and no Library key, so the command advertised as the
  promotion from *offered* to *auto-applied* silently did the opposite; that is recorded as a dated
  Amendment to ADR 0021 and the signature keeps ADR 0021 §6's substance (a fuzzy feature match over
  observed capabilities, logprobs preferred where the server exposes them, **never** a hash of
  response text) — only its role moved, from identity to evidence, and drift detection now rests on
  it. Consequence worth knowing before you run it: at medium confidence a matching Validated set
  **auto-applies** instead of being offered (ADR 0016 §5), so probing is the act that switches that
  automatism on — `--no-save` runs the whole battery and records nothing, and the record's path is
  printed either way so deleting the file undoes it. An **incomplete** battery mints no fingerprint
  at all (a hole in the evidence must not become an identity), and `probe model` refuses when
  neither `--model` nor the server names a model, since with no label there is nothing to key a
  claim on. *Adaptive prompt complexity is deliberately NOT built*: the probe ships the capability
  tier as a **signal with no automatism**, and the transform is recorded as a `TODO.md` follow-on,
  because a model-facing transform is a Mechanism by definition and earns its place on the ADR 0009
  non-inferiority gate with a bench campaign behind it — validated, not assumed.
- **Cobra subcommands, with bare `apogee` byte-identical.** The root command now accepts children
  (the seam shipped empty, so the Commands section in `--help` appeared only when `probe` landed).
  Everything load-bearing is unchanged: `maybeDispatchConfinedExec` is still the first thing `main`
  does, before Cobra parses anything, `Args: cobra.NoArgs` is retained on the root `RunE`, and no
  existing flag or environment path moved. `apogee headless` remains **deferred** — the skeleton
  merely makes it possible later.
- **Windows kills the whole process tree, not just the leader.** `internal/tools`' teardown stub
  killed only the process it started, so a cancelled `terminal` call could leave a grandchild
  running. The container is now an unnamed **Job Object** created between `Start` and `Wait` (a
  process can only be assigned to a job *after* `CreateProcess` returns, so `runSubprocess` runs
  Start → contain → Wait instead of `cmd.Run()`; POSIX takes a no-op teardown and the path is
  byte-for-byte what `Run` did) with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, terminated explicitly by
  `cmd.Cancel`, honouring the same contract §2.4 obligations as the POSIX `Setpgid` +
  negative-PID-kill path, `WaitDelay` unchanged. A **clean** run clears the kill-on-close limit
  before closing the handle, so a process the command deliberately backgrounded outlives it exactly
  as it does under a POSIX group leader — the limit's real job is the crash path. Both halves are
  pinned by native tests **verified by negative control** (stub out the containment and the tree
  test fails; remove the limit-clear and the survival test fails), and the shared decision function
  `planTreeKill` is untagged and table-tested on every OS.
- **The `platform` `Shell`/`Path` seam is real on both hosts.** The two Phase-0 widening `TODO`s
  are retired: `Shell{Command, CommandLine, Quote, ScopeEnv}` and `Path{Contains}` now
  live in **one untagged rule table** compiled on every target — only `Current()`'s choice is
  build-tagged — so Windows semantics are table-tested from a Linux run and executed natively on
  Windows. `CommandLine` exists because Windows has no argv at the syscall boundary: `os/exec`
  joins arguments with `syscall.EscapeArg`, which escapes an embedded quote as `\"`, a form
  `cmd.exe` does not understand (measured: a `cmd /c` of `echo "hello world"` prints `\"hello
  world\"`, and a redirect to a quoted spaced path dies with "The filename, directory name, or
  volume label syntax is incorrect"), so the verbatim command line goes to `SysProcAttr.CmdLine`
  and is `""` on POSIX, where `execve` takes a real argv. `Quote` answers to **both** parsers that
  read that line: `CommandLineToArgvW`'s backslash rules in the child (every backslash run touching
  a quote is doubled) and `cmd.exe`'s quote-toggling in front of it — so a value carrying a quote
  of its own is caret-escaped, which is what stops a quote plus an `&` from reaching `cmd` as a
  live command separator. Both halves are proven by a native round-trip through a real `cmd /c`,
  not by a golden string. `Contains` is the case-folded containment
  the Windows Confiner needs, and it is **"resolve, else refuse"**: `\\?\` and `\\?\UNC\` are
  normalised, drive-relative (`C:work`) and device (`\\.\…`) paths are refused as non-locations,
  and an 8.3-shaped component is expanded through `GetLongPathNameW` (walking up to the longest
  *existing* prefix, since the API is undefined for a path that does not exist yet) or the answer
  is `false`. There is deliberately **no `LookPath` wrapper** — `os/exec` already implements per-OS
  lookup including `%PATHEXT%`, so one would be dead surface. Two existing callers were adopted:
  the `terminal` tool carries the verbatim command line, and `safeGitEnv` runs through `ScopeEnv`,
  so a Windows `git` child finally gets `%SystemRoot%`/`%ComSpec%`/`%PATHEXT%`/the profile paths
  its POSIX-shaped allowlist never named (POSIX output is byte-identical — that floor is empty by
  design).

### Fixed

*The Phase 5 code review (`docs/reviews/code-review-2026-07-22.md` — 5 High, 11 Medium) found the
implementation faithful to its ADRs on the happy path and concentrated its defects in the paths
that had never run live. All of them are fixed below, before the owner's live-enforcement proofs
(`docs/plans/2026-07-22 - 02 - phase5-review-fixes-plan.md`).*

- **The Windows label journal now survives every path that could lose it.** The journal exists so
  the one disk mutation apogee performs — the Low mandatory labels on the box roots (ADR 0020 §2) —
  is always revertible, and four defects could leave it wrong or absent. (a) It was deleted
  *regardless* of whether the revert succeeded: `restoreLabels` and `recoverLabelJournals` now
  remove the file **only** when the revert returned nil, so a failed teardown leaves the record on
  disk for the next `NewConfiner()` to retry and for `apogee probe host` to report; the decision is
  an untagged helper, table-tested off Windows, and the surviving `Close()` error — previously
  swallowed at the composition root — prints one stderr line naming the journal path and the
  `icacls` remedy (`platform.ConfinementTeardownNotice`, sharing its wording with the residue
  notice). (b) The journal could record apogee's **own** Low label as the user's prior state, so
  teardown *restored* the label it was meant to remove, self-perpetuatingly: entries are now
  deduped per path (case-folded, first prior wins) and any prior naming the LOW level — `LW` or
  `S-1-16-4096`, in any descriptor spelling — is recorded as *empty*, so the revert clears to
  unlabelled. Where a prior is ambiguous, restoring toward **less** privilege is the safe
  direction, and ADR 0020's own manual remedy says an explicit Medium label and no label are
  behaviourally identical. (c) An unresolvable `%USERPROFILE%` meant no journal home and labelling
  proceeded anyway: `labelBox` now returns `ErrConfinementUnavailable` **before any label read or
  write**, which the execution contract demotes to a Gate — nothing is ever labelled without an
  undo record. (d) Writes were truncate-in-place and re-issued for every pre-labelled descendant
  found during the walk: journals are now published atomically (temp file, `Sync`, `os.Rename`) and
  a journal that cannot be decoded is no longer silently skipped by `confinementResidue` — it is
  reported as "journal present but unreadable", with the manual remedy stated as the only one.
  (`internal/platform/{winconfine.go,confiner_windows.go}`, `cmd/apogee/wire.go`.)
- **`apogee probe` reports outstanding confinement residue instead of healing it away.** In the
  `probe.Inputs` literal the Confiner was constructed *before* the residue was read, and on Windows
  that constructor reverts labels and deletes dead journals — so the interrupted-run case ADR 0020
  §2 assigns to `probe host` could never be reported, on a command three surfaces pin as read-only.
  The residue is now captured into a local first, and the probe path constructs through a new
  `platform.NewReportConfiner()` whose Windows variant performs **no** recovery: the read-only
  pledge (ADR 0021 §1, the README, the command's own `Long` text) stays absolute and unamended, the
  session constructor still heals on the next real run, and — since the journal now survives a
  failed revert — nothing is lost by reporting rather than repairing. The stale "the host half
  stays read-only" comment and the two docs that claimed construction touches no disk (ADR 0020
  §2/§3, `docs/design/confinement-execution-contract.md` §9.2) were corrected in the same change.
- **Ordinary cmd.exe command lines are no longer rejected by a POSIX parser.** The `terminal` tool
  ran `shlex.Split` over every command before handing it to the shell — including on Windows, where
  the shell is cmd.exe — so `echo don't panic` and `dir "C:\Program Files\"` died with "could not
  parse command line" on one of the three shipped platforms. The pre-flight now runs only where the
  platform hands the shell a real argv (the established raw-command-line convention:
  `shellHost.CommandLine(line) == ""`), and cmd reports its own errors, which is the honest
  arrangement — cmd has no stable quoting grammar worth pre-parsing, and no malformed-input class
  survives a check that would be truthful. POSIX behaviour is byte-identical. The
  "shlex-validated" claim in `docs/design/technical-design.md` §5 (P3.8) documented the defect and
  is corrected.
- **The Job Object handle is released on the two routine early-exit paths.** The teardown handle
  was created before `Confine` ran, but a `Confine` error returned before `runWithTeardown` was
  ever called, and a `cmd.Start()` failure returned before that function installed its own
  `defer`. Ownership moves to `runSubprocess`, which defers `release()` immediately after creating
  the teardown; the redundant inner `defer` is gone and `release` stays idempotent. A clean run is
  pinned to release exactly once, so the fix cannot silently become a double-release.
- **`probe model` can no longer claim an auto-apply that the next session start will refuse.**
  `autoApplyKeys` mirrored the startup ladder but omitted its catalogue-validation step, so a
  user-local validated-set entry naming a nonexistent mechanism ID was reported as `AUTO-APPLIES`
  and then skipped with a warning at startup. It now runs the same `validated.Validate` step and
  names the skip in the report's `suppressed` wording, carrying the catalogue's own reason
  verbatim. Consolidating the two ladders into one shared "would this entry apply" function is
  recorded in `TODO.md` for `/improve-codebase-architecture` rather than done here.
- **The review's remaining findings landed as tests and seams, not behaviour changes**: the
  pre-spend refusal gates in `probe model` are covered (empty `/v1/models`, discovery failure —
  zero `/chat/completions` hits, config home untouched) and the vacuous
  `TestGatherModelWritesNothing` — which asserted an unrelated temp dir was empty — is deleted in
  favour of the honest cmd-level coverage; the Windows build-floor decision moved into the untagged
  `belowWindowsFloor` predicate so the deny-vs-token selection is provable on every OS; the
  `windowsQuote` table gained its adversarial rows (which surfaced the quoting fix recorded under
  *Added* above) and the caller-less `Path.ExecExt` is gone; and the confined-`%TEMP%` /
  `ConfineWritablePaths` gap is now recorded in `TODO.md` with its anchors and called out in the
  README, rather than left implicit.

*The post-fixes review (`docs/reviews/code-review-2026-07-22-phase5-post-fixes.md` — 2 High,
12 Medium) found the hardened journal correct on its happy path and concentrated its defects at
the lifecycle's **edges**, plus one recurrence of the twin-ladder defect class. All of it is
fixed below (`docs/plans/2026-07-22 - 03 - phase5-second-review-fixes-plan.md`).*

- **The label journal's fail-closed rule now holds at every edge, in both directions** — no label
  without a journalled prior, no retirement while labels remain — where the first pass had proven
  it only for the happy path. Five edges were open. (a) A descendant whose prior label could not
  be *read* was relabelled anyway, unjournalled — a foreign security label destroyed with no
  record; the walk's three-way choice is now the untagged `descendantLabelDecision`, and a read
  error skips the path entirely (no label, no entry — the same tolerated-descendant posture as
  any other locked file). (b) `clearLabelTree` swallowed every descendant failure, so teardown
  retired the journal while Low labels remained on disk with no residue report; below-root
  failures are now counted into `clearTreeOutcome` — tolerating only `os.IsNotExist` — and any
  remainder keeps the journal for the next session's retry. (c) A prior-labelled file the agent
  deleted (routine workspace activity) failed the revert *forever* — `Close` warned every
  session, recovery failed silently every startup; a vanished path is now a completed revert,
  the "restored, not reconstructed" posture the root already took. (d) A failed ROOT label write
  refused the box but stranded its just-journalled entry, turning every later `Close` into a
  failing no-op and `apogee probe` into a permanent false alarm over a disk carrying no label;
  `labelBox` now unwinds the entry (`unwindLabelEntry`) when it recorded no foreign prior — an
  entry *with* one is kept, ambiguity resolving toward the record. (e) Two concurrent sessions
  on one workspace un-fenced each other: A's teardown stripped the shared root while B ran, and
  B's labelled-memo never re-labels; teardown and recovery now leave any root named in a live
  sibling's journal in place (`revertibleRoots`, untagged and liveness-injected) — the sibling
  owns the clear obligation, so the retiring journal still retires. And the single behaviour the
  machinery exists to deliver — a foreign prior label actually restored end-to-end — is now
  pinned natively and verified by negative control against both silent regressions (prior-restore
  loop deleted; clear/restore order swapped), alongside a construction test proving session
  recovery never deletes a journal it cannot decode.
- **A foreign prior label on a shared root now survives concurrent sibling sessions.** Follow-on
  to (e) above, found by its own implementation pass: when session A's teardown spared the shared
  root for live session B, A still *restored* the root's foreign prior — overwriting the Low
  label B was fenced by — retired its journal, and B's later clear wiped the restored label; the
  only record of the foreign prior was destroyed. A prior at or under a root ANY sibling journal
  still claims (live or dead — the file is the undischarged claim either way) is now **handed
  off** instead of restored: the retiring journal survives, rewritten to exactly the deferred
  prior entries under its original owner (`restorablePriors`, untagged and table-tested;
  `retireLabelJournal` grew the third fate beside "retired" and "kept whole"), and the first
  construction after the claiming journals are gone completes the restore — recovery now sweeps
  until no journal retires, so a handoff whose claimant died is finished in the same pass rather
  than the next session. The A-Close-then-B-Close sequence is pinned natively end-to-end and
  verified by negative control against both regressions (the pre-fix restore-now revert; the
  handoff record dropped at retirement).
  (`internal/platform/{winconfine.go,confiner_windows.go}`.)
- **`probe model` and startup no longer run twin identity ladders — the claim IS startup's
  decision.** The defect class closed for catalogue validation recurred at two more rungs:
  with no `model:` pinned, the report claimed `AUTO-APPLIES` for a record startup's empty model
  id can never resolve, and a `--model` naming a reachable weights file resolves at High so the
  Medium behavioural record is inert — both now suppressed lines naming the reason. The
  consolidation recorded in `TODO.md` was pulled forward rather than patched around: one shared
  ladder, `startupSetDecision` (`cmd/apogee/validatedsets.go` — off-switches,
  `ResolveFingerprintFrom`, `Match`, explicit-`mechanisms:` precedence, catalogue validation, in
  startup's order), which `resolveValidatedSet` enacts and `autoApplyKeys` reports — computed
  against the disk as the probe run leaves it, the promotion computed counterfactually with the
  record rung removed. The three off-switch branches each carry a test; parity is now by
  construction, not by parallel re-implementation.
- **The probe report's remaining honesty gaps are closed.** The no-record branch claimed
  "identity stays at the label tier" even when an earlier saved record survives and still
  applies — exactly the drift-check scenario `--no-save` serves; it now names the surviving
  record's date ("none new — the record from <date> continues to apply"). The v1-record warning
  the `Long` text promises was silently discarded on the model path's only read; it now prints
  on stderr. And `--workspace` changed nothing `probe model` reports or writes — dropped, per
  the probe commands' own flag rule (only `probe host` reads it).
- **A directory genuinely named like a short name is containable.** For a directory literally
  named `demo~1`, `GetLongPathName` returns its input — it *is* the long name — and that
  authoritative answer was indistinguishable from the nothing-resolvable fallback, so `Contains`
  refused a perfectly resolvable workspace into Gate. The `longPath` seam now signals authority
  (`(string, ok)`): `split` trusts an answered resolution without re-running the shape test,
  while an 8.3-shaped component the resolver could not verify still rejects, and POSIX rules are
  untouched.
- **The label guardrail sees through reparse-point roots and trailing-dot spellings.** The
  guardrail was lexical while `SetNamedSecurityInfo` is not: a root spelled `C:\Windows.` (OS
  canonicalisation strips the dot) or a junction targeting a protected location passed
  `Contains` and would have labelled the target Low. A box root that is itself a reparse point
  is now refused outright, every root is resolved to its final on-disk form
  (`GetFinalPathNameByHandle`) before the guardrail judges it — so the journal names the
  location the OS actually mutates — and the untagged rule table folds trailing dots and spaces
  off Windows components. Invariant hygiene, not an emergency: the roots come from trusted
  config and the confined Low child cannot create the precondition (ADR 0020 §6 gains the
  refusal sentence).

### Changed

- **The confinement degradation notice narrows to the hosts where it was always the honest
  answer.** Its trigger cell is unchanged (Auto **and** confinement asked for **and**
  `FSWrite == false`); what changed is that Windows ≥ build 17763 no longer lands in it. It still
  fires on an older Windows and in the containers where `landlock_create_ruleset` returns `ENOSYS`
  regardless of kernel version. Verified live on the execution host: `apogee probe host` prints
  `backend: token (fs-write: available · network: unavailable)` / `auto: eligible` and **no
  notice**, and the real `Terminal` tool under `platform.NewConfiner()` writes inside the box and
  is denied outside it with the Job Object and the verbatim command line composing unchanged. The
  escape battery (`internal/platform/confinetest`) now runs natively on Windows through
  `cmd /c` — in-box and writable-path writes land; out-of-box, user-profile and **nested-exec**
  writes all die with "Access is denied." and no file, so token inheritance across a second `exec`
  is **asserted, not assumed**. The below-floor path stays **untested** — no such host exists here.
- **Measured cost of the Windows disk mutation, which ADR 0020 accepted but did not quantify:
  ~1 ms per object.** A synthetic 5,051-object tree took **5.2 s to label and 2.2 s to revert**.
  It is paid once per box — the first confined command of a session — and once at shutdown, but a
  workspace with a large `.git` or `node_modules` will make that first `Confine` visibly block.
  Recorded rather than tuned: pruning the walk changes the ratified box semantics, so the cheap
  remedies (a startup notice, excluding ignored trees) are an owner call, not a silent one.
- **`internal/provider` gained an opt-in logprobs pair and a separate runtime context window.**
  `Request.LogProbs` and `RawResponse.TopCandidates` let the battery prefer logprobs where a server
  exposes them; the request fields are emitted as omitted pointers, so **every existing caller's
  bytes are unchanged**. `ModelInfo.RuntimeContextWindow` is new because the host report must state
  the `/v1/models` and llama.cpp `/props` outcomes separately, and `Discover` previously folded the
  `/props` window into `ContextWindow` with no way to tell which probe answered.
- **Fingerprint resolution grew its full ladder without breaking its callers.**
  `ResolveFingerprint(modelID)` is kept as the two-rung wrapper; the three-rung form is
  `ResolveFingerprintFrom(Sources{ModelID, Endpoint, ProbeDir})`, because the middle rung needs the
  endpoint and the home the old signature cannot carry. `internal/agent`'s call site adopts it too,
  deriving the probe dir from the injected `Config.ConfigDir` (an empty one simply removes the
  rung — never an ambient `~/.apogee`, per ADR 0001), so the Library's keying and the Validated-set
  match key **identically**; if only the wire-time call site could reach Medium, ADR 0021's
  consequences would be false in-loop.
- **Probe records written by an earlier build of `main` are not readable.** The ADR 0021 Amendment
  moved the record's `fingerprint` field to `behavior` and bumped `ProbeRecordVersion` 1 → 2. Old
  records are **skipped with a warning** naming the one-command fix — re-run `apogee probe model`
  once per model — and no migration tooling is built, deliberately: nothing was released with
  version 1. The same note ships in `apogee probe model --help`.

### Removed

- **The proxy and the OpenCode plugin / transform-server bridge — retired, on the record.** The
  decision was taken with the merge itself (merge plan §6 #4, 2026-06-22) and has governed every
  phase since, but nothing in *this* repo ever said so — which left a reader who met the
  `internal/proxy/…` breadcrumbs scattered through `internal/mechanisms` no way to tell a dead
  ancestor from a live dependency. Recorded here in the form it was actually executed: apogee-sim's
  OpenAI-compatible **reverse proxy**, the **transform-server bridge** and the **OpenCode plugin**
  are **not ported forward**, they remain in apogee-sim's git history as reference, and **apogee
  *is* the integration** — one integrated tool, not a Core exposed through peer integrations
  (`CONTEXT.md`'s retired-vocabulary section). Nothing is deleted and no behaviour changes, because
  none of that code was ever ported: this repo's only references to it are the **`@pin` provenance
  comments** on the ported Mechanisms (seventeen files in `internal/mechanisms` — `toolloop.go`
  pins `internal/proxy/tool_loop_interceptor.go`, `grammar.go` pins `proxy.go`'s
  `injectGrammarConstraint`, and so on), which say where a behaviour came from and what its A/B
  measured. Those are **history pins, not live references, and they stay verbatim**; the word's
  other occurrences in the tree — self-regulation's "proxy signals" and `internal/tools`' refusal
  of the `Proxy-*` hop-by-hop headers — are unrelated senses. Archival on the apogee-sim side is
  that repo's business, not this one's.
  (Merge plan §4 "Phase 5 — Cross-platform hardening & retirement", §6 #4.)

## [1.7.0] — 2026-07-21

### Added

- **`present_document` — the tool that shows a finished document to the user.** A Skill that
  produces a report — an architecture review, a research summary, a migration plan — used to end
  with a file on disk the user never saw: `write_file` renders as a one-line
  `Write File <path> +N bytes` card and nothing else, so the deliverable an Exchange spent its
  whole Budget producing was, from the user's seat, invisible. The model now closes that work with
  one dumb, explicitly named affordance — `present_document {path[, title]}` — and supplies
  nothing but a path and an optional title: the **host** picks the mechanism, so a ~4B–35B model
  never reasons about platforms, which is the thing it is worst at. The tool is the exact shape of
  `ask_user`: mode-**independent** (it never routes through the Approval gate), `ReadOnly()` so it
  runs in **every** mode including Plan (presenting writes nothing), **not** a safety gate, and
  **not** an `ExternalEffectTool` — the user's own display is not a non-forkable remote the bench
  must stub, any more than the human answering `ask_user` is. It is registered **only** when the
  host supplies a `Presenter`, so a headless embedder is unaffected by construction. The path is
  resolved inside the workspace root and must be an existing regular file; that, and a Turn
  cancelled mid-presentation, are the only ways the call fails.
  (`internal/tools`; [ADR 0019](docs/adr/0019-documents-are-presented-not-opened.md).)
- **The presentation ladder — the host decides how, and the baseline is never skipped.** The new
  `internal/present` package carries the host-side mechanisms and the TUI walks them per call, the
  highest applicable rung running *in addition to* rung 0, never instead of it. **Rung 0
  (always)** is the transcript entry carrying the workspace-relative path; it is the rung that is
  never wrong. **Rung 1** auto-opens the document when the session is **local** (no
  `SSH_CONNECTION` / `SSH_TTY` / `SSH_CLIENT`) and a desktop is detected (darwin/windows always;
  linux `DISPLAY` or `WAYLAND_DISPLAY`) — `open`, `cmd /c start "" <path>`, `xdg-open` — launched
  detached with its streams on the null device so an opener can never scribble on the Bubble Tea
  screen. **Rung 2** covers the remote case, which for this project is normal rather than exotic:
  a browser-renderable deliverable (`.html`, `.htm`, `.svg`, `.pdf`) is registered with an
  embedded **doc server** and its URL joins the entry, so one cmd+click opens it in the browser on
  the user's *own* machine, with **no host back-channel** anywhere in the path (rejected on
  security grounds). The server is a **capability-token allowlist, not a file server**: only
  explicitly presented files, each under a random 32-hex token at `/d/<token>/<basename>`, no
  directory listing, the only 200 a **GET or HEAD** whose path matches a grant exactly, and the
  same bare 404 for everything else — prefix walks, `..`, and every other method, refused as
  not-found rather than not-allowed because a 405 would confirm a real token — content-type from
  the extension and never sniffed, the file **re-read from disk per request** (so re-presenting
  after an edit shows fresh content), started lazily on the first served presentation, request
  logging discarded (net/http would log the token), and closed on app shutdown. Its advertised
  address is the server IP from `$SSH_CONNECTION` (known-routable, so it outranks the config key),
  else the `present.host` fallback, else an outbound-dial probe (no packets need to arrive), else
  `127.0.0.1`. **Rung 3** swaps rung 1's OS opener for the application named in
  `present.command`. Everything above rung 0 **fails visible**: an opener that errors, a server
  that cannot bind, a machine with nothing to open into — none of them fail the call, the outcome
  degrades to the baseline, the transcript says what happened, and the tool result names the rung
  (`opened` / `served` / `shown`) so the model tells the user the truth instead of asserting a
  success it cannot observe. The opener runs host-side, **outside** tool confinement, deliberately
  and for the reason ADR 0012 gives: it is the host's own act on the user's own desktop session,
  not a model-chosen subprocess. (`internal/present`, `internal/tui`.)
- **A first-class presentation entry in the transcript.** A deliverable is not plumbing, so it
  does not render as a tool card: the entry carries its own glyph (`▤`, deliberately not the tool
  `✦`), the optional title, and then the path — and, when served, the URL — as **plain text on
  their own lines**, unstyled, unwrapped and unclipped, one token per line. That is not a
  cosmetic choice: terminal linkification *is* the mechanism (Zed, VS Code, iTerm2, WezTerm and
  kitty all detect plain paths and URLs; Zed's cmd+click even opens the file through its remote
  server), and markup or a mid-token wrap is what breaks it. The line closes with what actually
  happened — "opened on your machine", "cmd+click to open", or a degraded "<reason> — path shown".
  The `Present` tool card keeps the one-line grammar the rest of the suite uses. (`internal/tui`.)
- **A file-only `present:` config block** — `auto-open` (default true; false disables rung 1 and
  **never** rung 0), `command` (a `{path}` template; the path is appended when the template does
  not mention it), `port` (default 0 — ephemeral, because the URL is printed fresh per
  presentation, so a stable port buys nothing) and `host` (the advertised address; a *fallback*
  for topologies `$SSH_CONNECTION` cannot describe, not an override). No flag and no environment
  variable, matching the newer keys' posture. The shipped `config.yaml` template documents all
  four, states plainly that apogee never auto-opens on a remote box (there is no display there to
  open into), and carries the macOS **Local Network permission** gotcha as the first thing to
  check when a served URL is unreachable — Chrome fails it with a generic "this site can't be
  reached" while Safari tends to work straight away. (`cmd/apogee`.)
- **`Presenter` — a new host delegate on `Config`, and additive public API surface.** `Presenter`
  joins `Approver` / `Asker` / `Confiner` on `domain.Config` and is re-exported from the facade
  with `PresentRequest` / `PresentOutcome` / `PresentMethod` and the three method constants
  (`PresentOpened`, `PresentServed`, `PresentShown`), so an out-of-module embedder can implement
  the interface and supply its own presentation mechanisms. Structs rather than bare strings, for
  the same freeze-safety reason `AskRequest` documents. Nothing exported is removed or re-typed
  and a nil `Presenter` leaves the tool unregistered, so the bench and every headless embedder are
  unaffected by construction: **additive ⇒ minor**. (`internal/domain`, `apogee.go`.)

## [1.6.0] — 2026-07-21

Post-`v1.5.0`, **additive** (minor) — two features end to end, plus a presentation pass over how
the chat reads. First: **Auto mode no longer degrades in silence.** On a host that cannot
fence a subprocess, ADR 0012's ladder ("confine if you can, gate
if you can't") sends every terminal command to Approval. That is correct, and it is the *common*
case rather than an exotic one — `landlock_create_ruleset` returns **`ENOSYS`** in most containers
whatever the kernel version — but nothing anywhere said so, so Auto simply read as broken
(`ISSUES.md`, 2026-07-21). Apogee now says so at startup, and offers the decision as a command:
`/confine off` for this session, `/confine off --save` to record *this machine* as disposable in
`~/.apogee/config.yaml` — a **host-scoped** acknowledgement, so a throwaway container's "I am the
sandbox" never follows the config file onto a laptop. **The ladder itself is untouched** and
auto-loosening stays forbidden: what shipped is visibility plus a signposted route to a decision
only the user may take (ADR 0012, amendment 2026-07-21). **No breaking change** — the public facade
(`apogee.go`) only *gains* methods, `Agent.SetConfineToWorkspace` / `Agent.ConfineToWorkspace`
(additive ⇒ **minor**, the same shape as the `Budget` methods in `v1.4.0`); nothing exported is
removed or re-typed. The whole journey is pinned by an acceptance test driven against a Confiner
that reports no filesystem confinement, so it reproduces identically on a machine that *can* fence.

Alongside it, a **presentation-only pass over the transcript layout** (`layout.md` is the amended
spec of record): assistant text no longer drags the model's own padding blank lines into the
scrollback, a tool header trades its `[brackets]` for a bold-orange label carrying nothing but
that label, a batch of same-label tool calls folds into one aligned block instead of five
stacked ones — with a lone call taking the very same shape — and a call's outcome is now split
into the one line that rides its branch and the body beneath it, which is what finally lets a
`View Diff` show `+2 -2` beside the path *and* the diff underneath. The four land as
separate **Changed** entries below because each is separately visible, but they are one change to
how a session *reads* — nothing the model sees is touched: no tool result, no event payload, no
upstream conversation, and nothing exported. `TestTranscriptLayoutGolden` pins the whole rendered
scrollback of a mixed session — prompt, narration, a grouped batch, a standalone `Run`, a diff
under its diffstat, an approval note, a sub-agent read — blank lines included, so a regression in
any one of the four shows up as a layout diff rather than a subtly taller chat.

Second: **a session no longer dies when the context window fills mid-task.** A `/refocus` against a
32k-window model hit `request (57546 tokens) exceeds the available context size (32768 tokens)` and
lost the whole Exchange — automatic Compaction is Exchange-boundary-only by design, so it could not
reach a doc-heavy Exchange growing past the window, and the only reducer that could act there was a
default-off Mechanism. Recovery is now **structural**, sitting with Budget and Compaction rather
than in the catalogue ([ADR 0018](docs/adr/0018-context-overflow-recovers-structurally-the-emergency-fold-and-one-retry.md),
per ADR 0006's rule that the floor stays on in the baseline and must be *functional*): an overflow
folds the history and re-sends the same Turn once, and a request the estimate already says cannot
fit is folded before it is sent at all. It runs under `--bypass`; `auto-compact: false` opts out of
it exactly as it opts out of boundary folding; nothing exported changed and no Event variant was
added, so this is behaviour-only.

### Added

- **Structural context-overflow recovery: the emergency fold and one retry.** A request the model's
  context window rejects is no longer a terminal fault. It is now its own Turn outcome, and the loop
  answers it by folding the conversation — the same generative Compaction `/compact` runs, keeping
  the protected prefix and replacing the rest with one summary — then re-sending the *same* Turn
  once. The fold is the one that may run **mid-Exchange**, deliberately amending the
  Exchange-boundary-only rule for this path alone (the estimate-driven trigger and the on-demand
  `/compact` still wait for the boundary: their caller can wait, a dying Turn cannot). It closes
  with a user-role bridge message — "the conversation above was compacted … continue the task from
  the summary" — so the retried request ends `…first-user | assistant-summary | user-bridge`: strict
  role alternation, no dangling tool calls, template-legal for any chat format; the open Exchange's
  rollback boundary is re-anchored to that bridge. Recovery is **structural** (it consults
  `auto-compact`, never `--bypass`) and bounded to **one fold per Turn**: a second overflow gives up
  exactly as before — the same sanitized `ErrorEvent` from source `"loop"`, the same abandoned
  Exchange — as does `auto-compact: false`, nothing left past the protected prefix to shed, or a
  failed summary call. Success is **quiet**: no new Event variant, the fold showing up only as the
  context gauge dropping on the next `UsageEvent`, and a cancel after a fold *keeps* the fold (it is
  history maintenance, not part of the Turn's attempt). (`internal/agent`; ADR 0018.)
- **A predictive guard that folds before sending a request that cannot fit.** Between building a
  request and sending it, the loop estimates it with the measure the whole engine shares
  (`PromptChars` through the calibrated chars→token ratio) against the full working room
  (`ContextLimit − ResponseReserve`) — never a softer fraction, because a fold is a lossy rewrite of
  the user's history. It saves the round-trip on a predictable overflow and covers the one case the
  reactive path cannot: a server whose 400 body cannot be classified as an overflow, where the
  stream yields a plain error and no recovery would ever fire. While the Budget is **uncalibrated**
  (no server usage reported yet — Turn 1, every sub-agent, and the first Turn after a resume, where
  the estimator is not serialized but the restored history may already sit near the window) the
  guard is **damped**, not disabled: it demands twice the working room, which is exactly the
  estimator's clamp band (8.0/4.0), so a false positive is impossible inside it while a pathological
  case still fires with room to spare. The asymmetry is the reason — an under-estimate costs
  nothing (the wire overflow routes to the reactive path) while an over-estimate spends an
  irreversible fold on a request that would have fit. A predictive fold spends the Turn's one fold,
  and a refused one simply sends the request as before. (`internal/agent`.)
- **A structural floor on a single oversized tool result.** A tool result whose estimate exceeds the
  *entire* History allocation is now clamped — head/tail plus an elision marker pointing at a
  `start_line`/`end_line` re-read — as it enters the conversation, at the one seam every result
  crosses, so no route (a plain call, a confined run, an approved gate, a sub-agent delegation, an
  error result) can bypass it. A result that large survives no reducer and can doom the Turn
  outright: the emergency fold's own summary call keeps the most recent message unconditionally, so
  a fresh giant result *is* that message and overflows the fold meant to rescue the Turn. It is
  structural rather than a Mechanism because ADR 0006 requires the baseline's reducers to be on and
  functional, and because `tool_result_cap` is default-off, Bypass-disabled, withdrawable, and caps
  only the turns *before* the most recent tool call — never the result that overflows; that
  Mechanism stays the tighter, A/B-able valve above the floor and, when enabled, fires first. The
  clamp rewrites the conversation, so the raw result never reaches history, a snapshot, or the
  transcript, and both reducers now share one rendering (`context.TruncateToolResult`) so the model
  learns a single elision idiom. (`internal/agent`, `internal/context`.)
- **A startup notice when Auto degrades to approval on an unfenceable host.** Entering `--mode
  auto` with confinement asked for (the default) on a host whose Confiner backend reports
  `FSWrite == false` now prints one stderr notice naming the backend, saying plainly that commands
  cannot be fenced here and therefore fall back to Approval, and pointing at `/confine off` (this
  session) and `/confine off --save` (remember this host). Nothing is worded as repairing a
  malfunction, because nothing is broken — a host that cannot fence is the ladder working as
  specified. It is the mirror of the existing unconfined-Auto warning and the two never both fire;
  the three lower modes make no confinement promise and stay silent, so this is never a general
  startup nag. (`cmd/apogee`.)
- **`/confine` — report and change Auto's blast radius from the chat.** A new verb in the TUI
  mini-language, and the first one that takes arguments: `/confine` (or `/confine status`) reports
  the backend, what it can actually enforce here, the host id an acknowledgement is matched
  against, and the effective setting — read live, so it reflects an earlier toggle — closing with
  the two remedy lines only on the host that prompts the question (Auto, confined, no fs-write
  fencing). `/confine off` and `/confine on` swap the blast radius on the running Agent,
  synchronous and idle-safe like `/clear` and taking effect on the next tool call, each recording a
  transcript confirmation that states the radius in plain words (`off` → "auto runs every command
  unfenced, with your full privileges") and says whether it was session-only or written to disk.
  Turning confinement off where it works, or off when it is already off, is allowed and simply says
  so: a legitimate choice, just not the degraded case. An argument the grammar does not understand
  — an unknown subcommand, an unknown flag, or a `--save` that is not persisting an `off` — is a
  parse **error carrying the usage line**, never a silent no-op: the one command that can widen what
  Auto may touch must never leave a user believing a mistyped line took effect. A slash command was
  chosen over a startup y/N prompt or an extra choice on the Approval prompt precisely to keep the
  accept away from the moment of peak frustration. (`internal/tui`, plus the composition root
  handing the TUI the backend/capability/host-id facts it must not derive itself.)
- **`Agent.SetConfineToWorkspace` / `Agent.ConfineToWorkspace` — Auto's blast radius is now a live,
  runtime-swappable setting.** `confine-to-workspace` was read from the construction Config on every
  tool call, so changing it meant restarting Apogee. It is now a live field on the Agent — seeded
  from `Config.ConfineToWorkspace`, read by the per-call Resolution through the accessor, and
  swappable at any time — exactly mirroring the `SetMode`/`Mode` pair behind Shift+Tab. Both methods
  are goroutine-safe (their own `RWMutex`, a sibling of `modeMu`), so the UI may toggle while the
  worker is mid-Step; the change lands on the **next** tool call with no rebuild and no registry
  churn. A sub-agent spawned after a toggle inherits the parent's live value, as it already did for
  the mode; one already mid-flight keeps what it was spawned with, so a toggle can neither loosen
  nor tighten a running delegation. The toggle changes only the running Session — nothing is written
  to disk — and the engine never flips the flag on its own initiative; it only carries out the
  user's explicit act. (`internal/agent`.)
- **`unconfined-hosts:` — the host-scoped confinement acknowledgement.** A new global-config-only
  list in `~/.apogee/config.yaml` recording *which machines* you have acknowledged as disposable, so
  Auto may run unconfined **there** without that claim following the file onto every other host.
  Each entry carries an `id` (matched against the new `platform.HostID()`), plus a free-form
  `acknowledged` date and `note` for the human reading the file back later. Resolution runs in the
  order the ADR fixes: an explicit `confine-to-workspace: false` still wins and still means *every*
  host (it is unchanged and not deprecated); else an entry naming **this** machine yields an
  effective `false`; else the secure default `true`. An explicit `confine-to-workspace: true` does
  not veto a matching entry — the flag states the global default, the entry states a fact about one
  machine, and the more specific claim wins. Like the flag, the list is settable **only** from the
  global config file — no flag, no environment variable — so a hostile repo's invocation environment
  cannot name your host. An id matching no machine is simply "not this host", never an error (the
  list is meant to accumulate machines), and an entry with no `id` is skipped with one startup
  notice rather than blocking the run. A host that can supply **no identity of its own** — no
  hostname *and* no machine-id file — never matches either, however the entry is spelled: the id
  such a host computes is the same on every one of them, so honouring it would let a single saved
  acknowledgement loosen the lot. That match is reported and ignored, and `--save` refuses to
  record it in the first place, so nothing is written that could quietly travel. The template
  `config.yaml` documents the block beside `confine-to-workspace`. (`cmd/apogee`.)
- **`platform.HostID()` — the machine interlock the acknowledgement is matched against.** A stable
  per-machine id shaped `<sanitized hostname>-<first 6 hex of sha256(machine identifier)>` (e.g.
  `devbox-a1b2c3`), where the identifier is the first available of `/etc/machine-id`,
  `/var/lib/dbus/machine-id`, else the hostname itself — no shelling out, so it stays
  dependency-free and correct on hosts (and future Windows builds) where neither file exists. It is
  a **safety interlock, not an authentication mechanism**: it stops an acknowledgement travelling
  between machines unnoticed, it does not resist forgery (anyone who can edit the config can write
  any id), and it fails closed — an ephemeral container with a fresh machine-id per run simply does
  not match its stored entry and is confined again. The value is deterministic within a process and
  across runs, never empty (a failing `os.Hostname()` yields `unknown-<hash>`), and restricted to
  `[A-Za-z0-9_.-]` so it is safe as an unquoted YAML scalar. Exactly one composed value is *not*
  per-machine — the one a host with neither a hostname nor a machine id computes, which is identical
  on every such host — so `platform.IsUnidentifiedHostID` names it and both callers refuse it as an
  identity: it never matches during resolution and it is never written to disk. (`internal/platform`.)
- **A comment-preserving config writer behind `/confine off --save`.** Saving appends this host's
  `unconfined-hosts:` entry (id, today's date, and a note saying what put the line there and that
  deleting it re-confines the machine) to `~/.apogee/config.yaml`, and reports the file back so the
  confirmation can name it. Your config survives intact. The file is edited as **text**, guided by
  the parsed node positions, never round-tripped through unmarshal→marshal: `yaml.v3` hangs comments
  off nodes, and the seeded template is *entirely* comments — it parses to no nodes at all — so a
  re-marshal would have handed you back a file with one setting in it and every word of
  documentation deleted. Comments, key order, indentation, and your own edits come back
  byte-identical. Because the key ships commented out, the writer **inserts** rather than
  substitutes: it appends to an existing list (matching that list's own indentation), starts one
  under a bare `unconfined-hosts:` key, or adds a documented block at the end of the file — so it
  stays correct against a config you have since reordered or rewritten by hand. Saving the same host
  twice records it once (the second call reports the entry already on disk and writes nothing). An
  absent config is seeded from the embedded template first, so `--save` never leaves a bare fragment
  where a documented file belongs. The write is atomic (temp + rename in the same directory) and
  preserves the file's mode, since a config may hold endpoint details. Every splice is re-parsed and
  compared against the original *before* anything is written — the result must be the old list plus
  exactly this entry, with no other setting touched — so an exotic file shape (a flow-style list, a
  second YAML document apogee would never read) is refused with a "add the entry by hand" message
  rather than quietly mangled, and a failed write surfaces as an error instead of a save that did
  not happen. A save that fails never invalidates the session toggle that already happened, and the
  confirmation says so. (`cmd/apogee`.)

- **`apogee.AuditEvent` — the facade's Event re-export is complete.** `apogee.go` aliased every
  Event variant except `AuditEvent`, which has shipped in `internal/domain` since the Phase-3
  security remediation. Because `internal/` is unimportable from outside the module, an embedder
  could *receive* an audit observation through the `Event` interface but had no way to name the
  type in a switch — the variant was effectively unreachable across the facade. The alias closes
  that; the variant's own contract is untouched, so this is additive. `example_test.go`'s
  compile-time facade guard now names every variant, `ReasoningEvent` and `UsageEvent` included, so
  the next omission is a build failure rather than a silent gap. (`apogee.go`.)

### Changed

- **Blank-line hygiene in the transcript — one empty line between blocks, never three.** Models pad
  their replies: a trailing `\n\n` (and often a leading one) survived the commit verbatim, and the
  renderer drew every one of those empty lines *on top of* its own one-line block separator, so a
  chat session grew two- and three-row gaps between every answer and whatever came next. Committed
  assistant text — both a `MessageEvent` and the pre-tool narration the first `ToolCall` finalises —
  is now trimmed of its leading and trailing blank lines, and interior runs of two or more blank
  lines collapse to a single one, so `layout.md`'s "exactly one empty line between blocks" is
  finally true rather than aspirational. **Fenced code blocks are exempt**: a blank line between two
  statements is part of the code and comes back verbatim. Text that is blank all the way through now
  commits **no entry at all**, where it used to leave a bare `✦` marker line sitting in the
  scrollback — and a blank *canonical* message still falls back to the streamed tokens, so nothing
  that arrived is lost. The live streaming preview trims only its trailing blanks, and only for
  display: the buffer itself keeps them, because a mid-stream `\n\n` may be a paragraph break the
  model is about to continue, while a just-opened empty buffer still shows its lone marker so you
  can see streaming has begun. Presentation only — the tool results, event payloads, and the
  upstream conversation the model sees are untouched. (`internal/tui`.)
- **Tool labels lost their `[brackets]` and gained colour.** A tool call's header now reads
  `✦ Read File main.go` instead of `✦ [Read File] main.go`, with the label alone rendered **bold in
  orange `#f0883e`** — the tone inline code and the auto-mode marker already carry — and the target
  left plain. The brackets were doing the work of making the label stand out from the target;
  weight and colour do it better and cost no columns. The styling is deliberately **uniform**: a
  known friendly label, an unregistered tool's raw name, and the stray-result `result` header all
  look the same. That retires the old "a bare name means the tool has no presentation entry"
  signal, which was the brackets' side effect rather than anything a reader could name — an unknown
  tool still falls back to its raw name and its verbatim pretty-printed arguments, so nothing about
  what the model asked for is hidden. The style is baked into the header before it is wrapped
  (the `markdown.go` posture — `ansi.Wrap` is SGR-aware and `lipgloss.Width` strips ANSI), so the
  soft-wrap and sticky-header arithmetic are unperturbed. Presentation only. (`internal/tui`.)
- **A batch of same-label tool calls is now one block, not five — and every tool call takes that
  same shape whether it is alone or in a batch.** Five file reads used to render as five separate
  headers, each with its own branch line and its own blank separator — a tall, noisy column for
  what the reader thinks of as one action. Consecutive tool calls at the same sub-agent depth
  carrying the same label now fold into a single block: one `✦ Read File` header, then one
  `┝`/`┕` branch per call whose target is **padded to the block's widest** so the detail column
  lines up (`┝ README.md 1 - 154` / `┝ TODO.md   1 - 408` / `┕ ISSUES.md 1 - 8`). The header
  carries **the label alone and never a target** — for a group, a lone call, a call still in
  flight and the stray-result `result` header alike — and the target always leads the first
  branch instead (`✦ Read File` / `┕ main.go 1 - 154`). That is what stops a block from visually
  reshaping the moment a second call joins it: a block of one is byte-identical in shape to a
  block of many, and growing one only ever *adds a line*. What a branch carries follows from which
  halves of the call's outcome are filled (see the entry below), and nothing else: the one-line
  summary follows the target on the branch, the body lays out **beneath** it at the branch
  marker's width rather than sprouting `┝`/`┕` branches of its own (a `Run` and its `… +N more
  lines` remainder, a red/green diff body under its diffstat); an in-flight call has neither yet
  and shows the bare target until its result lands; and a call with no target at all is the one
  shape with no target line — the header stands alone and the lines are the branches, as an
  unregistered tool's pretty-printed arguments and a stray result still render. Two different
  tools sharing a label — a single and a multi find-and-replace are both "Edit File" — do group,
  because the reader groups by what the header says, not by tool id; anything between two calls
  (narration, a note, an approval, an error) ends the run, and a call carrying a body or no target
  keeps its own block. **Grouping is render-time only** — the transcript's
  append-only entry list, its call/result pairing, and the open-call signal the status line reads
  are untouched, so a call arriving mid-stream joins its group on the next repaint for free.
  Nothing is clipped for alignment's sake; an overlong branch soft-wraps as before. Presentation
  only. (`internal/tui`.)
- **A tool call's outcome is now a summary line plus a body — and `View Diff` finally renders the
  shape `layout.md` has always sketched.** The presentation model carried one flat list of detail
  lines, and the renderer picked the block's shape by *counting* it: exactly one line joined the
  target on the branch, two or more laid out beneath a bare target. That made the sketch's diff
  block unreachable — a `+2 -2` diffstat plus its diff body was simply a three-line list, so the
  summary lost its place on the branch and the target lost its. The outcome is now split at the
  source: a one-line **summary** that rides the branch beside the target (`1 - 154`, `+2 -2`,
  `error: …`) and a **body** that hangs beneath it (a command's output, a diff's lines). The shape
  follows from which halves are filled and **never from how many body lines there are** — a body
  of one lays out exactly like a body of ten. `View Diff` is the one producer filling both, and it
  reads as the sketch always promised: `┕ main.go +2 -2` with the red/green diff beneath. The
  diffstat is counted over the **whole** diff, not the lines that survive the 20-line body cap —
  it is the one number a truncated body cannot tell you — and always names both counts (`+2 -0`
  for an addition-only change). `No changes detected` is not a diff and stays the single plain
  line it was. **Every other block renders byte-for-byte as before**, which is the point of the
  rule that any outcome fitting on one line is a summary: a one-line `Run`, `Git Branch` or
  `Diagnostics` result still rides its branch beside the command (`┕ pwd /workspace/repos/apogee`)
  and still folds into a group, while only output needing the `… +N more lines` remainder becomes
  a body beneath the command, exactly as it already did. Presentation only. (`internal/tui`.)

## [1.5.0] — 2026-07-21

Post-`v1.4.0`, **additive** (minor) — one TUI affordance and the one Event variant it needed to
be honest. The status line's left slot no longer reports the turn index (a number that answered
nothing the human was asking) but **what the worker is doing right now**, with an elapsed clock:
`thinking · 12s`, `reading · main.go · 3s`, `running · npm test · 8s`, `sub-agent · searching ·
6s`. Making `thinking` a fact rather than a guess required the engine to reveal that it is
reasoning, which is the new `domain.ReasoningEvent` — an observation-only variant on both the
native reasoning channel and the inline `<think>`/harmony spans that stay held off the visible
stream. **No breaking change**: per this changelog's own rule (ADR 0001 §consequences), a new
Event variant is a **minor** bump, and the public facade (`apogee.go`) only *gains* the
`ReasoningEvent` alias — nothing exported is removed or re-typed. The loop's visible token
stream, the committed assistant message, and history are byte-identical; ADR 0011's renderer
contract is untouched (no new `uiState`, no agent logic in the TUI), so no new ADR.

### Added

- **`domain.ReasoningEvent` — the observability seam for the model's reasoning channel.** A new
  Event variant beside `TokenEvent`, re-exported on the facade as `apogee.ReasoningEvent`,
  carrying one newly-revealed chunk of reasoning. It is **observation only**: it never changes
  history (the channel is already preserved as `reasoning_content` on the assistant message) and
  a consumer may treat its arrival alone as a liveness signal and ignore `Text` entirely, which
  is what the TUI does. The engine emits it on both paths — natively from
  `provider.DeltaThinking`, and inline from `emitReasoningDelta`, `emitVisibleDelta`'s mirror
  that runs *while* the stripper is mid-channel (the same prefix-stability guard: an unclosed
  span's tail is what the stripper routes into reasoning, and closed spans never change, so the
  accumulation only ever grows a suffix). `Text` is untrusted model output — any consumer that
  ever *displays* it must escape-strip exactly as the TUI's token path does. Nothing else moved:
  the `TokenEvent` sequence, the reply, and the recorded conversation are unchanged.
  (`internal/domain`, `internal/agent`, root facade.)
- **A live activity status line with an elapsed clock.** While a worker runs, the status line's
  left slot renders what is happening instead of `turn N`: `thinking` (a request in flight, or
  reasoning chunks arriving), `responding` (visible text streaming, keeping its `tok/s` suffix),
  `<verb> · <target>` for an open tool call, `retrying`, `compacting`, `stopping` — each with an
  elapsed clock that restarts only when the phrase itself changes, and prefixed with `sub-agent ·`
  at nesting depth > 0. Idle renders nothing at all. The whole left slot — spinner, phrase,
  clock, `tok/s` — hangs in the transcript's own text column (the two columns a `✦`/`❯` marker
  occupies), so it lines up with the body text above it instead of sitting flush left
  (`layout.md`); the indent is inside the width budget, so a narrow window still clips the line
  rather than wrapping it. The vocabulary lives in a new pure,
  table-tested `internal/tui/activity.go`: `foldActivity` derives the phrase from the same Event
  stream the transcript folds, and the handful of transitions no Event announces (a submit,
  `/continue`, `/compact`, an Esc stop, the worker's terminal message) set it directly; `stopping`
  is sticky until the worker actually unwinds. The per-tool active verb (`reading`, `editing`,
  `running python`, `delegating`, …) is one new column in the existing name-keyed presentation
  registry rather than a second parallel switch, so an unregistered dynamic MCP tool inherits the
  same `running <raw name>` fallback the transcript header already uses. No new `uiState` —
  `compacting` and `stopping` are activities, not lifecycle states (ADR 0011). (`internal/tui`.)

### Changed

- **The chat body now breaks two columns short of the scroll bar.** The transcript's text no
  longer wraps flush against whatever sits at its right edge: the body is rendered to a
  `bodyRightGutter`-narrower column than the viewport, so two columns stay free between the last
  painted glyph and the scroll bar, and three between the glyph and the window edge while the bar
  is blank — the mirror of the `bodyIndent` gutter on the left. The gutter is deliberately a
  constant rather than a function of whether the bar is currently painted: the scroll-bar column
  is reserved unconditionally, and the bar appears inside it the moment the content overflows, so
  a wrap width that tracked its visibility would re-wrap the whole visible transcript mid-reply.
  The viewport keeps its full width — only the content is narrower — so the sticky-to-top offset
  (`wrappedOffset`) and the mouse mapping still measure against the viewport, and the wrap width
  is floored at one column, so a window too narrow to hold the gutter still renders rather than
  going negative. (`internal/tui`, `layout.md`.)

### Removed

- **The `turn N` readout and the transcript turn counter behind it.** The status line's turn index
  is replaced by the activity phrase, and with its last reader gone the `turn` field on the TUI
  transcript and the eight assignments that maintained it are deleted — keeping a field alive for
  a test assertion is dead state, and the resumed turn index is already asserted from its
  authoritative source (`Resume`'s `TurnIndex`). Internal to `internal/tui`; no public-surface
  change. (`internal/tui`.)

## [1.4.0] — 2026-07-21

Post-`v1.3.0`, **additive** (minor) — three strands plus a TUI affordance. The **Validated-set
runtime surface** (ADR 0016): a per-model Mechanism set that passed the non-inferiority gate now
reaches users at startup, shipped with the binary (`internal/validated/shipped.json`) and
user-local (`~/.apogee/validated/`), whole-set-or-nothing and off under an explicit
`mechanisms:` block or Bypass. The **guided-decomposition hardening** (F1–F7): the fan-out's
Exchange scoping, marker handling, and remainder cursor are corrected, a deferred correction now
dies with its Exchange (F6), and `guided_decomposition + truncate_history` is refused at startup
as incompatible (F7). And the **architecture-deepening consolidations** (D1–D7, ADR 0017): the
Exchange boundary, the chars→token arithmetic, the history-scan shapes, the read/list tool-name
spelling families (F8), and the per-tool spec ritual each fold to one implementation. Plus `/new`
in the TUI, an alias of `/clear`. **No breaking change** (sanity-checked against the
`v1.3.0..HEAD` diff): the public facade (`apogee.go`) is untouched, and the types it aliases only
*gain* methods — `Budget.EstimateTokens` / `Budget.HistoryExceedsAllocation` (D4) and
`Conversation.ClearDeferred` / `TruncateDeferred` / `DeferredLen` (F6); nothing exported is
removed or re-typed. `domain.ExchangeView` (D1) stays internal per ADR 0017 §1.

### Added

- **`/new` — start a fresh conversation (alias of `/clear`).** The TUI chat mini-language now
  recognises `/new` as an alias of `/clear`: the parser accepts it as its own verb and `runCommand`
  routes it through the same synchronous context reset (`Engine.ClearContext`, staying idle, no
  worker), and the `/` autocomplete menu offers it. Purely additive — `/clear`'s behaviour is
  unchanged. (`internal/tui`.)
- **The Validated-set runtime surface (ADR 0016).** A per-model Mechanism set that passed the
  non-inferiority gate on a model now reaches users at startup: `cmd/apogee` matches the resolved
  model fingerprint against Validated-set entries — shipped with the binary
  (`internal/validated/shipped.json`, first entry: the gemma-4-e4b-it-qat pruned 16) and
  user-local (`~/.apogee/validated/*.json`, one entry per file, user wins a key collision) — and
  folds an applying set into `Config.EnableMechanisms` at wire time (the engine and bench arms are
  untouched; ADR 0015's single enable path stands). Semantics per the ADR's 2026-07-19
  realisation: auto-apply at ≥ medium fingerprint confidence; at low (name-only) confidence the
  per-session notice **offers** the set, applied only by the explicit `validated-sets: alias:`
  config (an identity alias is the confirm, a differing one the §3 transfer — consulted at any
  confidence); whole-set-or-nothing (a non-empty `mechanisms:` block or Bypass suppresses the
  apply; a defective entry — unknown ID, invalid stacking, malformed file — is skipped with a
  warning, never partially applied, never a blocked startup); a dangling alias is a loud startup
  error. New config block `validated-sets:` (`enable` off-switch, default on; `alias` map),
  file-only. New package `internal/validated`; shipped entries are pinned against the live
  catalogue by test. (`internal/validated`, `cmd/apogee`.)
- **ADR 0017 — the Exchange is a derived domain working value.** Ratifies the architecture
  deepening plan's D1–D3 (docs only; the code lands in that plan's items 3–4): the Exchange
  boundary is derived from the conversation — the messages strictly after the last `RoleUser`
  message, stable across injections — as an `internal/domain` `ExchangeView` working value shared
  by the loop and the hooks; the engine's cached `exchangeStart` and its S2 repair arithmetic are
  to be replaced by that derivation (`inExchange` stays; snapshot `ExchangeStart` becomes
  ignored-on-read, old snapshots stay resumable); Exchange end concentrates into one engine-side
  `closeExchange` owner of the F6 "a deferral dies with its Exchange" invariant; `ExchangeView`
  stays unexported at the root until an external consumer exists. CONTEXT.md's **Exchange** entry
  now names the code home. (`docs/adr/`, `CONTEXT.md`.)
- **`internal/domain/domaintest` — the hook seam's second adapter (test support, D6).** A fluent
  `ConversationBuilder` returning `[]domain.Message`, canned message/tool-call constructors
  (including the `read_file` call shape the read-counting Mechanisms inspect), and a settable
  `FakeLoopView` implementing `domain.LoopView` (zero value usable; its conversation view is built
  through the domain's own engine seam, so the pairing helpers cannot drift from production). The
  four package-shared `internal/mechanisms` fixture helpers (`readCall`/`userMsg`/`assistantText`/
  `assistantCall`) are now thin delegates — signatures unchanged, no test rewrites. Internal test
  support only; no public-surface change. (`internal/domain/domaintest`, `internal/mechanisms`.)
- **`domain.ExchangeView` — the Exchange boundary derived in one place (ADR 0017 §1, D1).** A new
  working value plus `CurrentExchange` constructor over the minimal unexported `Len()/At(i)` read
  surface (satisfied by both `*Conversation` and the hooks' `conversationView`): `Found`,
  `UserIndex`, `After` (copies), `RangeAfter` (allocation-free). The derivation — the current
  Exchange is the messages strictly after the last `RoleUser` message — now has exactly one
  implementation (`lastRoleIndex`); `InjectContext` and `conversationView.LastUser` route through
  it via `lastIndex`, public behaviour unchanged. No callers yet (the engine and Mechanisms follow
  in the deepening plan's items 4–5); not exported at the root. Internal only; no public-surface
  change. (`internal/domain`.)
- **`Budget.EstimateTokens` and `Budget.HistoryExceedsAllocation` — one token arithmetic (D4).**
  The chars→token conversion now has a single implementation: two methods on the `Budget` struct
  (root-aliased, so the public surface gains them — **additive → minor**). `EstimateTokens(chars)`
  rounds up (the estimator's calibrated ground truth) and yields 0 on a non-positive
  `CharsPerToken`, keeping token-gated behaviour inert until calibration;
  `HistoryExceedsAllocation(msgs)` is the single compare behind both the engine's auto-Compaction
  trigger (`Agent.historyExceedsAllocation`) and any hook reading the Budget, so the two can never
  disagree. The pure character measure `PromptChars` moved to `internal/domain` (ADR 0010's
  lowest-layer rule; `internal/context` keeps a thin delegate), `context.TokenEstimator` keeps its
  calibration and default-ratio fallback and delegates the rounding, guided decomposition's signal
  thresholds and the Library's injection-budget cap estimate through the shared method (the D4
  authorized delta: truncation → ceil, at most one token per comparison; no fixtures were
  boundary-exact), and `capMaxChars` stays the documented tokens→chars inverse. (`internal/domain`,
  `internal/context`, `internal/agent`, `internal/mechanisms`.)

### Fixed

- **Guided decomposition accepts only majority-marked enumerations.** The case-1 intercept now
  treats a steered reply as an enumeration only when the parsed list is in-bounds (2..12) **and** a
  strict majority of its lines carried an explicit ordered/bullet marker (F4). A compliant numbered
  list still fans out; multi-line prose, a clarifying question, a refusal, or an empty reply is
  declined whole. Unmarked lines are still kept as subtasks when the majority test passes
  (small-model tolerance). `guidedDecompositionStripMarker` now reports whether a marker was present.
  (`internal/mechanisms`.)
- **Guided decomposition anchors the remainder cursor on the delegation-bearing enumeration.** The
  cursor now scans only the current Exchange (the messages after the last user message) and anchors on
  the first assistant message that **both** parses as an in-bounds subtask list **and** carries a
  `sub_agent` call (F3) — the pair that uniquely identifies the real enumeration. A prior-Exchange
  answer, mid-Exchange narration, or a compaction summary can no longer shadow it, and a previous
  Exchange's fan-out cannot consume the current one's items or resume across the boundary.
  (`internal/mechanisms`.)
- **Guided decomposition consumes dispatched subtasks by exact match, once each.** The remainder
  cursor now removes an enumeration item only when a dispatched `sub_agent` task equals the item
  itself or the item plus the appended report-hygiene ask, and each dispatched task consumes at most
  one item occurrence. Duplicate items each need their own dispatch, and dispatching a longer
  prefix-nested item ("Add tests for the CLI") no longer also drops a shorter one ("Add tests"). An
  off-script task matching no item still leaves the remainder intact (§5). (`internal/mechanisms`.)
- **Guided decomposition re-defers the directive across off-script tool Turns.** When a directive is
  steering and the model answers a mid-fan-out Turn with a tool call other than `sub_agent`, the
  post-response intercept now re-defers the remaining-items directive (with the remainder intact)
  instead of letting it drain away — one off-script tool call no longer silently drops the fan-out
  queue (F2). A no-tool final answer still ends the Exchange and is never re-deferred.
  (`internal/mechanisms`.)
- **Guided decomposition steers at most once per Exchange.** The pre-request gate now stays quiet for
  the rest of an Exchange once a fan-out has begun in it, judged from committed history — any
  assistant message after the last user ask that carries a `sub_agent` call (F1). This stops the gate
  re-steering on the synthesis Turn (where the request-scoped steer/directive markers have drained but
  signal B still reads oversized), which had looped the decomposition; a model that delegated
  unprompted this Exchange is likewise left alone. A new user ask re-arms the gate. The marker-based
  check remains as the same-request double-steer guard. (`internal/mechanisms`.)
- **Guided decomposition markers are line-anchored and role-scoped.** The steer/directive marker scan
  now matches only in `RoleUser` and `RoleSystem` messages (the only places the loop injects) and only
  where the marker starts a line (F5). An assistant echo of the phrase, a tool result, or `@file`-style
  user content carrying it mid-line no longer counts as an outstanding steer or a fan-out directive; the
  real injected steer and drained directive (marker at a line start) still do. The marker strings are
  unchanged (the loop-level tests' wire contract). (`internal/mechanisms`; ADR 0014 Realisation addendum.)
- **Deferred Response Actions expire at the Exchange boundary.** A deferred correction (an
  `ActionDefer` decision, e.g. a guided-decomposition remaining-items directive) is now cleared
  whenever an Exchange ends — a completed final answer, a terminal fault (`abandonTurn`), or an
  `AbortExchange` — so a stale fan-out directive can no longer survive a fault or abort into the next
  Exchange (F6). A cancelled Turn now truncates the queue back to its pre-hooks floor before restoring
  the drained injections, so a re-attempt or snapshot carries exactly the one restored directive
  rather than two contradictory copies. `domain.Conversation` gains `ClearDeferred`, `TruncateDeferred`,
  and `DeferredLen`. (`internal/agent`, `internal/domain`; CONTEXT.md.)
- **Guided decomposition is incompatible with `truncate_history`.** The `guided_decomposition`
  descriptor now declares `IncompatibleWith: [decompose, truncate_history]` (F7): a mid-Exchange
  truncation longer than its keep window can drop the enumeration message the fan-out cursor
  re-derives the remainder from, destroying the fan-out mid-flight. Co-enabling the two is refused at
  startup with `ErrIncompatibleMechanisms`; the valid `guided_decomposition + tool_result_cap` stack is
  unaffected. (`internal/mechanisms`; `docs/design/mechanism-catalogue.md`, ADR 0014 Realisation.)

### Changed

- **Single-sourced read/list tool-name spelling families.** The read trio
  (`read_file`/`readFile`/`open_file`) and the five list spellings
  (`list_files`/`listFiles`/`list_dir`/`listDir`/`list_directory`) are now hoisted as two spelling
  families beside the write side's `wave4WriteTools`, and every read/list set composes from them
  instead of hand-copying (F8) — closing the drift class that had shipped defects in two prior review
  rounds. Each set keeps its own documented membership; search/exec spellings stay out of scope. Four
  diverged sets are corrected as part of the composition (each a behaviour change with a mutation-pinned
  test): `cotReadOnlyTools` now counts `list_directory`, `libraryListTools` and `toolFilterAnalysisKeep`
  now carry `listFiles`/`listDir`, and `fileHintListTools` now carries `listDir`. Three stale wiring
  comments that still pointed at `cmd/apogee/wire.go` are repointed at the engine's single build path
  (`buildEnabledMechanisms`, `internal/agent/loop.go`) after the ADR 0015 wire.go collapse.
  (`internal/mechanisms`.)
- **Exchange end has one engine-side owner; the rollback boundary reads through one seam (ADR
  0017 §§2–3).** The three Exchange-end sites (`completeTurn`'s exchange-complete branch,
  `abandonTurn`, `AbortExchange`) now route through one private `closeExchange` owning the F6
  "a deferral dies with its Exchange" invariant (`cancelTurn` stays distinct by design — the
  Exchange remains open there). The planned deletion of the cached `exchangeStart` did NOT land:
  item-4 verification showed a mid-Exchange `truncate_history` fold can drop the open Exchange's
  opening user message (the gap note would anchor the derivation and be over-dropped on abort —
  pinned by `TestExchangeStartRepairedAfterMidExchangeTruncation`), so per the ADR's recorded
  fallback the cache and its S2 repair stay, with all readers routed through the one
  `exchangeBoundary()` helper and the snapshot's `exchangeStart` still round-tripping (newly
  pinned by `TestSnapshot_RoundTripsExchangeBoundaryForAbort`). Behaviour-preserving; internal
  only, no public-surface change. (`internal/agent`; ADR 0017 + CONTEXT.md record the fallback.)
- **Guided decomposition reads the Exchange through the domain seam (ADR 0017 §1).** The
  Mechanism's three current-Exchange scans — the F1 fan-out-begun check, the enumeration anchor,
  and the dispatched-task window — now derive the boundary via `domain.CurrentExchange`, routed
  through the one `guidedDecompositionCurrentExchangeStart` accessor; the Mechanism-local
  "last `RoleUser`" derivation (`conv.LastUser()`) is deleted. Marker handling, list parsing, and
  every threshold are unchanged, and the whole suite passes unchanged; a new drift pin
  (`TestGuidedDecompositionAgreesWithDomainCurrentExchange`) asserts the cursor helpers agree with
  the seam's own output on a two-Exchange history. Behaviour-preserving; internal only, no
  public-surface change. (`internal/mechanisms`.)
- **One copy of each shared history-scan shape, beside the spelling families (D5).** The
  hand-rolled `conv.Range(...)` walks the history-inspecting Mechanisms each carried now live once
  in `internal/mechanisms/historyscan.go`, composing with the F8 spelling families: read-attempt
  path counting with successes and failures separate (`readAttemptCounts` — readloop's two
  detectors shrink to threshold-plus-sort wrappers), successful-read paths over the latest read
  episode (`recentSuccessfulReadPaths` — readrepeat's private scan deleted), and written paths
  since an index (`writtenPathsSince` — `writtenPaths` is now a thin delegate over the whole
  conversation). filehint's private copies are deleted onto the already-shared helpers: its
  duplicate write set and write scan (`fileHintWriteTools`/`fileHintHasWrittenFiles`) fold into
  `hasWrittenFiles` over `wave4WriteTools`, and its marker scan (`fileHintAlreadyInjected`) into
  `requestContains`. Per-Mechanism membership and thresholds stay at the call sites (D5);
  readloop's `isGreenfieldContext` stays local as a composite write/read/list early-exit scan no
  shared shape expresses. The three Mechanisms' suites pass unchanged (the behaviour contract);
  the helpers gain their own table-driven suite over the family spellings via `domaintest`.
  Behaviour-preserving; internal only, no public-surface change. (`internal/mechanisms`.)
- **Embedded tool spec and typed arg decoding fold the per-tool ritual (D7).** Each of the 20
  built-in tools hand-rolled the same shape: a package-var schema string, three metadata methods
  (`Name`/`Description`/`Schema`), and a decode-and-error preamble in `Execute`. The identity now
  lives in one embeddable `toolSpec` value per tool (name + description + the raw JSON schema
  string, still visible and reviewable — no schema generation, D7/ADR 0002) providing the three
  methods via embedding, and the preamble folds into one generic `decodeToolArgs[A]` helper
  wrapping `decodeArgs`, so the standard "invalid arguments: …" result is built in exactly one
  place (all 20 sites already shared that wording verbatim — nothing begged unification).
  `internal/mcp`'s `serverTool` is untouched: it does not share the ritual (its identity arrives
  per-server at runtime, its description carries a fallback, and its arguments pass through raw).
  Tool names, schemas, results, and error strings are unchanged — the whole `internal/tools`
  suite passes untouched, plus a new test pinning that a tool built from a spec reports exactly
  the spec's name/description/schema bytes. Behaviour-preserving; internal only, no
  public-surface change. (`internal/tools`.)

### Tested

- **Sub-agent spawn under the production `EnableMechanisms` arm.** New loop-level tests arm the
  `guided_decomposition + tool_result_cap` stack by ID with `Config.Mechanisms` left nil (the engine
  builds it), drive one real delegation, and prove a spawned sub-agent inherits the parent's
  already-built registry (ADR 0015): the spawn succeeds, the child nests at `Depth == 1`, and the
  child fires a catalogued Mechanism (`tool_result_cap`) from the inherited stack — through both `New`
  and `Resume`. Reverting subagent.go's `EnableMechanisms` clear makes them fail with the
  already-registered rejection. (`internal/agent`.)
- **Corrupt-store degrade and the descriptor clone contract.** A new loop-level test seeds
  `LibraryDir/library.json` with garbage bytes and arms `EnableMechanisms=["library"]`: construction
  still succeeds, the build path emits the degrade notice to `os.Stderr` exactly once, and the armed
  library Mechanism runs over the resulting empty store — with the model fingerprint forced
  high-confidence (a reachable weight file) so the empty store, not the confidence gate, is the sole
  barrier, proving no injection leaks from the corrupt content. A root test mutates a returned
  `CataloguedMechanisms()` descriptor's `Requires` / `IncompatibleWith` slices and re-queries, pinning
  the documented per-call clone contract (ADR 0015 §3). (`internal/agent`, root.)

## [1.3.0] — 2026-07-05

Post-`v1.2.0`, **additive** (minor) — two features. The `guided_decomposition` Mechanism
(ADR 0014), built up item-by-item behind the Mechanism catalogue and shipped **default-off**
(the bench flips it on, not this work). And the public Mechanism enable surface (ADR 0015) —
the 2026-07-05 handoff's path (b): an external module (apogee-sim, the ADR 0001 consumer) can
now arm any catalogued Mechanism stack in-process by ID. **No breaking change**
(sanity-checked against the `v1.2.0..HEAD` diff): the public facade (`apogee.go`) only
*gains* symbols — `Config.EnableMechanisms`, `CataloguedMechanisms()`, the
`MechanismDescriptor` / `Capability` / `SuppressionPolicy` aliases with their constant
values, and the `ErrMissingRequirement` / `ErrUnknownMechanism` re-exports; nothing exported
is removed or re-typed.

### Guided decomposition (`guided_decomposition`, default-off)

- **The `Requires` stacking relation.** `MechanismDescriptor` gains a `Requires []MechanismID`
  field — the dual of `IncompatibleWith` — and `New` now runs the new
  `MechanismRegistry.ValidateRequirements` gate alongside the ordering and incompatibility
  checks: every registered Mechanism's required peers must also be registered, else the new
  `ErrMissingRequirement` sentinel refuses construction ("X requires Y — enable both or neither;
  they are benched as a stack"), the same startup-gate posture as `ErrOrderingCycle`. It is
  enable-time only (ADR 0014 §4): live suppression of a required peer mid-Session is not
  re-checked. (`internal/domain`, `internal/agent`.)
- **Hook-visible loop depth and post-response tool-call synthesis.** `LoopView` gains a
  `Depth()` method — 0 for a top-level Agent, parent+1 for a sub-agent (ADR 0013) — so a gate
  can steer only the primary call, never a nested delegation (ADR 0014 §5); the loop stamps it
  from the Agent's nesting level through the new engine-seam `Request.SetDepth`. `Response`
  gains `AppendToolCall`, letting a post-response Mechanism add a `sub_agent` delegation the
  model never emitted: the loop reads it back through `ToolCalls()`, commits it on the assistant
  message, and dispatches it through the full per-call Resolution (the ADR 0013 recursion point,
  driving a real nested child) exactly like a model-emitted call. An in-place response mutation
  combined with a returned `ActionDefer` both take effect. (`internal/domain`, `internal/agent`.)
- **The `guided_decomposition` gate and enumeration steer (pre-request half).** The new
  `guided_decomposition` Mechanism (`internal/mechanisms`, catalogue-registered, ordered `After
  toolfilter`) lands its pre-request half: a strikes-3 proactive-nudge, `IncompatibleWith`
  `decompose` and `Requires` `tool_result_cap`. On an oversized PRIMARY call — a known window,
  top-level depth, a `sub_agent` on the final menu, and a measured size signal (a fresh user message
  over the `FileContext` budget, or mid-Exchange history over the `History` budget with the model
  still calling tools) — it injects an enumeration steer asking for ONLY a numbered list of at most 7
  self-contained subtasks. The steer is marker-idempotent and stays quiet once a fan-out directive is
  already steering (no double-steer); the post-response intercept and serialized follow-through land
  next. (`internal/mechanisms`.)
- **The intercept and serialized follow-through (post-response half).** `guided_decomposition` gains
  its `PostResponse` half on the same struct. On the enumeration Turn — the steer outstanding, the
  model's reply a bounded (2..12) subtask list with no tool calls — it parses the list, synthesizes
  the FIRST `sub_agent` delegation onto the response (the enumeration text left verbatim), and defers
  a remaining-items directive carrying the rest plus a compact-report hygiene ask (ADR 0014 §4). Each
  following Turn re-derives the remainder from honest history — the model's own list message and the
  `sub_agent` CALLS in the conversation (never the child results, so a report capped by
  `tool_result_cap` leaves the cursor exact) — minus the just-delegated task, and re-defers the
  shrunken directive until none remain. It carries no per-Mechanism state (snapshot/resume-safe and
  suppression-clean), declines an out-of-bounds list whole, and no-ops on anything else.
  (`internal/mechanisms`.)
- **Wire-up proof and end-to-end fan-out acceptance.** Loop-level tests drive the whole stack through
  the REAL loop with nothing of the Mechanism mocked: an oversized primary call gets the enumeration
  steer, the model's list is intercepted into a REAL nested `sub_agent` fan-out serialized one
  delegation per Turn (child events nesting at `Depth == 1`), the remaining-items directive rides each
  following request and shrinks, and the Exchange ends on a no-tool synthesis with the enumeration
  verbatim and all three child reports in honest history. A snapshot taken mid-fan-out round-trips the
  pending directive (`conversationJSON.Deferred`) and a resumed Agent completes the fan-out; Bypass is
  the silent control arm (ADR 0014 §1); and a cancel during a child rolls back only that parent Turn
  (ADR 0013 §5). Config-surface tests pin the ADR 0014 §4 stacking gates: enabling `guided_decomposition`
  without `tool_result_cap` is the `ErrMissingRequirement` startup error, the stack boots, and adding
  `decompose` is the incompatibility error. The commented `mechanisms:` example in the config template
  gains the stack. (`internal/agent`, `cmd/apogee`.)
- **Docs close-out.** The feature's cross-cutting doc edits are reconciled under this one heading:
  CONTEXT.md's Guided decomposition entry now disambiguates it from the shipped `decompose` Mechanism
  (a prompt-shaping nudge — steers wording, not delegation; the two are declared incompatible), and
  ADR 0014 gains a dated Realisation note recording the decisions locked at implementation — queue
  delivery as one re-derived deferred directive per Turn, `IncompatibleWith: [decompose]`,
  registry-level `Requires` validation, verbatim enumeration text, and the 7/12 subtask bounds — plus
  the authorized per-item deviations. (Docs: `CONTEXT.md`, `docs/adr/0014`.)

### The public Mechanism enable surface (`Config.EnableMechanisms`, ADR 0015)

- **Catalogued descriptors become static, queryable data + a matchable unknown-ID sentinel.** Each
  catalogued Mechanism's `MechanismDescriptor` is now a single package-level value that both the
  built instance's `Descriptor()` returns and the catalogue registers beside the constructor
  (equality by construction), and a new `mechanisms.Descriptors()` returns every row — sorted by ID,
  duplicate-free, slice fields cloned — so a Mechanism's metadata is available without building one
  (the backing for the forthcoming public `CataloguedMechanisms()` query, ADR 0015 §3). A new
  `domain.ErrUnknownMechanism` sentinel is wrapped by `mechanisms.Build`'s unknown-ID error (which
  still names the known IDs), so a typo'd or deferred ID fails loudly AND matchably via `errors.Is`
  (ADR 0015 §4). (`internal/mechanisms`, `internal/domain`.)
- **`Config.EnableMechanisms` arms catalogued Mechanisms by ID at construction.** `Config` gains an
  `EnableMechanisms []MechanismID` field: `New` and `Resume` build each named catalogued Mechanism
  and merge it INTO `Config.Mechanisms` (a fresh registry when nil), so catalogued Mechanisms and
  bench experimental hooks coexist in one arm (ADR 0015 §1–2). The engine derives the build `Deps`
  the way `cmd/apogee/wire.go` does — a Library store rooted at `Config.LibraryDir` and Loaded only
  when `library` is enabled (never an ambient root; a corrupt/absent store degrades to empty and
  never blocks construction), the model fingerprint resolved once, and the grammar seam left inert —
  entirely internal (no `Deps` type on the public surface). IDs build in sorted order for a
  deterministic error surface, then the existing ordering/incompatibility/requirements gates run over
  the merged registry unchanged: an unknown ID (`ErrUnknownMechanism`), an ID listed twice or already
  pre-built (the already-registered rejection), and a half-armed `Requires` stack
  (`ErrMissingRequirement`) each fail `New`/`Resume`; an empty/nil list arms nothing (default-off).
  A spawned sub-agent inherits the parent's already-built registry, so it fires the same Mechanisms
  without re-building them. `cmd/apogee`'s own YAML→registry path is unchanged for now (it collapses
  onto this engine path in a follow-up). (`internal/domain`, `internal/agent`.)
- **`cmd/apogee` collapses to a YAML→ID-list producer.** `cmd/apogee/wire.go` no longer builds a
  registry: `buildMechanismRegistry` and the cmd-side `Deps` derivation (the Library store /
  fingerprint / `LookPath` wiring, now dead) are deleted. The composition root still validates EVERY
  `mechanisms:` key — enabled AND disabled — against the known catalogue at the startup boundary (a
  typo'd DISABLED key, which the engine never sees, must still fail loudly there), then hands the
  sorted enabled IDs to `Config.EnableMechanisms` and lets `New`/`Resume` build them (ADR 0015 §1).
  The YAML `mechanisms:` surface, the config template, and every user-visible behaviour are unchanged
  — the same loud errors refuse to boot at the same startup boundary (unknown key, half-stack,
  incompatibility), only the `%w` chain behind some of them moved from the cmd path onto the engine
  path. (`cmd/apogee`.)
- **The public Mechanism surface: descriptors, catalogue query, and matchable enable errors.** The
  root facade now exposes the enable surface an embedder needs: `MechanismDescriptor`, `Capability`,
  and `SuppressionPolicy` (with their constant values) are re-exported, and a new
  `apogee.CataloguedMechanisms()` returns every catalogued Mechanism's descriptor — sorted by ID,
  duplicate-free, slice fields cloned — so a host can read each Mechanism's Capability, suppression
  policy, and `IncompatibleWith` / `Requires` stacking relations and plan an `EnableMechanisms` arm
  (e.g. a leave-one-out arm by `Requires` traversal) WITHOUT building any Mechanism (ADR 0015 §3). The
  enable-time sentinels `ErrMissingRequirement` (the dual of `ErrIncompatibleMechanisms`) and
  `ErrUnknownMechanism` are re-exported so `errors.Is` matches them through the root (ADR 0015 §4).
  `Config.Mechanisms`' doc comment is reframed as the experimental-hook carrier that points at
  `EnableMechanisms` for catalogued enablement (the field keeps its name under v1 semver — no
  rename). Runnable godoc Examples arm the `guided_decomposition + tool_result_cap` stack and compute
  a leave-one-out arm from the catalogue query. (`apogee.go`, `internal/domain`.)
- **The bench-readiness contract becomes a true external-surface consumer.** `benchreadiness_test.go`
  now arms every arm through the PUBLIC enable surface — catalogued Mechanisms by ID via
  `Config.EnableMechanisms`, experimental hooks via `AddExperimental` — and no longer imports
  `internal/mechanisms` or `internal/library` or builds the catalogue by hand, so a separate module
  (apogee-sim) can now do everything this test does (ADR 0015 Consequences). It adds the acceptance the
  bench campaign needs, all through the root API: a half-armed `Requires` stack refuses construction
  (`apogee.ErrMissingRequirement`), a bogus ID refuses (`apogee.ErrUnknownMechanism`), the
  catalogued+experimental combined arm still co-fires both in deterministic order, and a leave-one-out
  arm set computed from `apogee.CataloguedMechanisms()` — the full compatible stack and every
  member-omitted arm — constructs successfully. (`benchreadiness_test.go`.)
- **Docs close-out.** The enable surface's cross-cutting doc edits are reconciled under this one
  heading: ADR 0015 gains a dated Realisation note recording the authorized implementation
  deviation — a spawned sub-agent inherits the parent's already-built registry (clearing
  `EnableMechanisms`) rather than rebuilding, and a degraded Library store degrades to empty
  rather than failing construction — and the README's Configuration section now names the public
  library enable surface (`Config.EnableMechanisms` / `apogee.CataloguedMechanisms()`) alongside
  the unchanged `mechanisms:` YAML path. CONTEXT.md is unchanged — the grill crystallised no new
  term (ADR 0015 Consequences / locked decision 7). (Docs: `docs/adr/0015`, `README.md`.)

## [1.2.0] — 2026-07-04

Post-`v1.1.0`, **additive** (minor) — Phase 4 merges the apogee-sim Mechanisms into the
loop (`docs/plans/archived/phase-4-detail-plan.md`; ratified catalogue at
`docs/design/mechanism-catalogue.md`). **No breaking change** (sanity-checked against the
`v1.1.0..HEAD` diff): the public facade (`apogee.go`) only *gains* symbols — the sole new
top-level export is `ErrIncompatibleMechanisms`; nothing exported is removed or re-typed. Every
other new surface is additive — new `Config` fields (the `mechanisms:` block and the `auto-compact`
key) plus the now-consumed `LibraryDir` root (a pre-`v1.1.0` field Phase 4 finally reads, not a new
field), new advisory `domain.Budget` fields, and new `domain` types
(`ModelFingerprint`, `FingerprintResolver`) that are *not* re-exported at the root. The one
changed signature (`domain.NewRequest`'s fired-ledger argument) is an internal engine seam, never
on the public surface — so this is a **minor** bump, not a major one.

### Catalogued Mechanisms now dispatch in a deterministic order behind the Bypass gate

- **Registered Mechanisms finally run.** A Mechanism added to the `MechanismRegistry` via
  `Add` used to be validated but never dispatched — only the bench's experimental hooks
  fired. Now, at each of the five hook points, the loop dispatches the catalogued
  Mechanisms **first**, in a deterministic total order (`MechanismRegistry.Ordered` — a
  topological sort of each Mechanism's `Before`/`After` `OrderingConstraints` with a stable
  tiebreak by canonical `MechanismID`, so a shuffled registration order yields identical
  output, ADR 0003), then the experimental hooks in registration order (unchanged). Each
  fires under the same recover boundary and emits a `MechanismFiredEvent` under its **real**
  `MechanismID` (experimental hooks keep the synthetic `experimental` attribution).
- **`Config.Bypass` now gates dispatch (ADR 0006).** Under Bypass, every catalogued
  non-`off-ramp` Mechanism is skipped — proactive-nudge and response-repair go silent —
  while `off-ramp` recovery guarantees still run; experimental hooks are never Bypass-gated
  (they are the bench's own instruments), and the structural context machinery (Budget,
  Compaction) is unaffected.
- **Incompatible Mechanisms fail loudly at construction.** `New` now also runs
  `MechanismRegistry.ValidateIncompatibilities`, returning the new
  `ErrIncompatibleMechanisms` sentinel when two registered Mechanisms declare each other via
  `MechanismDescriptor.IncompatibleWith` — the same startup-gate posture as
  `ErrOrderingCycle`, so a config that enables two mutually-exclusive Mechanisms is refused
  rather than silently running both. (`internal/domain`, `internal/agent`, root re-exports.)

### Mechanisms now self-regulate: effectiveness tracking, Adaptive Suppression, the Turn Budget

- **A catalogued Mechanism that is not helping is now withdrawn for the rest of the
  Session.** A per-Session tracker judges each Turn on proxy signals — a Turn is
  **productive** when it reads a new file or writes one (a tool error or an empty/no-op
  response is not). **Adaptive Suppression** (per Mechanism): a Mechanism that fires through
  three consecutive non-productive Turns is skipped at dispatch for the rest of the Session,
  with a clear-path that re-opens every Mechanism on the next productive Turn. **The Turn
  Budget** (global): after eight consecutive non-productive Turns every non-exempt Mechanism
  is withdrawn until productive activity resumes. A `SuppressionPolicy: exempt` off-ramp
  bypasses both — suppressing it would leave a failed Turn with no way out (ADR 0006).
- **`LoopView.Fired` finally answers.** The declared-but-inert per-Session fire counter now
  reports real fires, read live within a hook pass (a Mechanism sees a peer's fire from
  earlier in the same pass — the cross-Mechanism coupling seam). No new public surface: the
  tracker is internal to `internal/agent`; `domain.NewRequest` gains a `fired` ledger
  argument on the engine seam only.
- **Reset on Resume.** The tracker is per-Session and not serialized: a resumed Agent starts
  with clean suppression state (the accepted v1 posture — fresh state can only cause a
  withdrawn Mechanism to be re-tried, never wrongly withheld). (`internal/agent`,
  `internal/domain`.)

### A file-only `mechanisms:` config block wires the catalogue into the loop

- **Catalogued Mechanisms are now opt-in from `config.yaml`.** A new file-only `mechanisms:`
  block (no flag/env, like `mcp-servers` / `model-profile`) maps a canonical mechanism ID to
  `enabled: true|false`. Every Mechanism defaults **off** (D1 — default-off until bench-proven);
  a `true` entry turns one on. An **unknown ID is a loud startup error** listing the catalogue
  this build knows, so a typo'd key never silently disables a Mechanism. `--bypass` still wins:
  an enabled non-off-ramp Mechanism is not dispatched under bypass (ADR 0006 / the item-2 gate).
- **The catalogue constructor seam.** `internal/mechanisms` gains `Build(id, deps)` over a
  constructor table (`Deps` carries the construction-injected collaborators — D3; the Library
  store is nil until it lands). The composition root (`cmd/apogee`) drives the table for each
  enabled ID and folds the built Mechanisms into `Config.Mechanisms` before construction. The
  table ships **empty** — the port waves fill one row per Mechanism — so a config with no
  `mechanisms:` block behaves exactly as before. (`cmd/apogee`, `internal/mechanisms`, README +
  starter `config.yaml`.)

### Wave 1: the `validate` / `syntax` / `autofix` response-robustness Mechanisms

- **The measured-win response cascade is ported.** Three post-response Mechanisms — dispatched in
  the deterministic order `validate` → `autofix` → `syntax` (catalogue Table A as amended by the
  reorder entry below; originally shipped `validate` → `syntax` → `autofix`) — now ship in the
  `internal/mechanisms` catalogue (default **off**, D1). `validate` checks each requested tool call
  against the tool menu the model was shown and its own arguments (unknown tool name, empty/malformed
  JSON, missing required parameter); `syntax` checks a file-writing call's content (Go through the
  real parser, other languages through a bracket/string/truncation heuristic); `autofix` repairs
  syntax-broken write content and writes the improved payload back to the call the loop will dispatch.
- **Corrections retry in place (amended C5 — R1; superseding this entry's original ActionDefer
  delivery).** `validate`/`syntax` return `ActionRetry` with the sim's correction message — the
  loop re-streams the corrected request in the same Turn (see the delivery-switch entry below).
  `autofix` intercepts in place via `Response.SetToolCallArguments`, which is effective because a
  Response's tool calls are dispatched only after post-response review.
- **`gofmt` is always in-process; other formatters are construction-probed and gracefully absent
  (superseding this entry's original fire-time PATH-gating — see the autofix entry below).** Go is
  formatted with the standard library's `go/format` — no external dependency — with `goimports`
  preferred when found; `black` / `prettier` / `rustfmt` repair only when their executable was
  resolved at construction, and a formatter's absence, failure, or timeout leaves the payload
  untouched (standing requirement #2). What no formatter can improve is left for `syntax` to
  correct. (`internal/mechanisms`.)

### Wave 1: the `empty_response_recovery` / `tool_use_enforcer` off-ramps

- **The two recovery guarantees are ported (catalogue Table A).** Both are post-response Mechanisms
  with Capability **off-ramp** and SuppressionPolicy **exempt**, so they run even under Bypass (D5)
  and are never withdrawn by Adaptive Suppression or the Turn Budget — without them a failed Turn has
  no way out (CONTEXT "Off-ramp"). They ship in the `internal/mechanisms` catalogue, default **off**
  (D1). `empty_response_recovery` fires when the model returns nothing — no text and no tool call —
  mid-task with tools available and recent progress; `tool_use_enforcer` fires when the user asked for
  an action but the model answered with prose twice running, having never used a tool (the sim's
  intent classifier, folded in inline per catalogue C6).
- **Empty replies and narration both retry in place (amended C5 — R1; superseding this entry's
  original retry/defer split).** `empty_response_recovery` returns `ActionRetry` carrying the sim's
  first-attempt completion-check nudge verbatim; `tool_use_enforcer` returns `ActionRetry` with the
  sim's "use a tool" correction, the retried request carrying the superseded narration (the sim's
  `retryForToolUse` exchange). Both stay bounded by the loop's existing `maxPostResponseRetries`
  cap so an always-empty model still terminates. (`internal/mechanisms`.)

### ActionRetry now carries the corrective exchange onto the retried request

- **A post-response retry delivers its correction in the same Turn (R1, amending catalogue
  C5).** `PostResponseDecision.Inject` now rides `ActionRetry` too: when a post-response
  Mechanism retries with a correction, the loop appends the superseded assistant message
  (text + tool calls, when non-empty) and then the correction as a role-safe user message
  to the in-flight request before re-streaming — request-scoped, never committed to history
  — mirroring apogee-sim's own retry builders. Corrections accumulate across attempts (the
  sim's escalating re-asks), bounded by the existing `maxPostResponseRetries` cap; at the
  cap the last response passes through. An `Inject`-less retry stays a bare re-stream, and
  `ActionDefer` keeps its next-request semantics unchanged. (`internal/domain`,
  `internal/agent`.)
- **The retry appendage is hidden from post-response scanners (second-review fix, sim
  parity).** On a retry cycle the request-scoped superseded attempt + correction no longer
  masquerade as committed history to the history-aware Mechanisms: `Request.View()` is now
  bounded to the length frozen at the first `AppendSupersededAssistant`, so `read_repeat`
  never counts a never-executed superseded read as already-read and `tool_loop_interceptor`
  compares the retried response against the last **committed** turn, not the superseded
  attempt. The appendage still reaches the model through `Request.State()` — only the
  mechanism view changes — matching apogee-sim, whose retry builders ran their detectors
  against the unmutated request. (`internal/domain`, `internal/agent`.)
- **The retry-view boundary now survives an empty superseded response (third-review fix).**
  When a retried response is wholly empty, nothing is appended, so the correction lands
  *below* the frozen `committedLen` rather than after it — and the boundary was static, so
  `Request.View()` evicted the real user ask (the insert-before-last-user shape) or the newest
  tool result (the system-prepend shape) from the post-response scanners. `committedLen` is now
  MAINTAINED, not just frozen: a below-boundary `InjectContext` insert and an
  `appendOrCreateSystem` prepend each advance it, so `View()` stays pinned to the same committed
  history. `Request.State()` (the model-facing projection) is byte-identical — the correction
  still reaches the model unchanged; only the mechanism view is corrected. (`internal/domain`;
  tests in `internal/agent`.)

### Wave 1 rides the retry seam: corrections deliver in the same Turn

- **The four shipped Mechanisms switch `ActionDefer` → `ActionRetry` (amended C5, R1).**
  `validate` and `syntax` now short-circuit the response-repair cascade on a failing call —
  the correction re-streams the corrected request in the same Turn instead of waiting for the
  next request — so the catalogue's "short-circuits cascade on fail" holds for real.
  `tool_use_enforcer` re-calls in-cycle exactly like the sim's `retryForToolUse`: the retried
  request carries the superseded narration plus the "use a tool" correction, fixing the review
  finding that the correction sat until the next user Submit. `empty_response_recovery`
  upgrades its bare re-stream to carry the sim's first-attempt completion-check nudge verbatim
  (`empty_recovery.go` @pin); the attempt-2 nudge ladder, system directive, and temperature
  escalation stay recorded bench-pending divergences (R2). Everything remains bounded by
  `maxPostResponseRetries` — an always-empty model terminates, its final reply passing through.
  Proven loop-level through the scripted-responder harness, including both off-ramps firing at
  dispatch (registry-built) under Bypass and through a tripped Turn Budget.
  (`internal/mechanisms`; tests in `internal/agent`.)

### autofix repairs like the sim: construction-probed formatters, issue-count gating, repair-before-correct

- **The formatter table is resolved once at construction (D3).** `mechanisms.Deps` gains
  `LookPath` (nil ⇒ `exec.LookPath`); `newAutofix` probes goimports/black/prettier/rustfmt
  through it exactly once and caches the resolved paths — the sim's LookPath-cached formatter
  table — so a fire never touches PATH. The package-var-at-fire-time probe is deleted, and
  `cmd/apogee` wires the production `exec.LookPath`.
- **Repair only, gated on improvement.** autofix now acts only on syntax-broken write content
  and keeps a formatter's output only when it *reduces* the `checkSyntax` issue count (the
  sim's `AttemptFix` gate) — clean content is never beautified, and a "fix" that fixes nothing
  is discarded. The sim's `sanitizePath` NUL/CR/LF guard is restored alongside the kept `-`
  prefix hardening on formatter argv.
- **Cascade reorder: `validate` → `autofix` → `syntax`.** The sim runs detect → `tryAutoFix` →
  correct-the-remainder (`response_analysis.go:72-88` @pin), so repair now precedes the
  correction stage — `syntax`'s retry covers only what a formatter could not fix, ending the
  review's double-correction finding. Catalogue Table A and the post-response cascade section
  record the amendment. (`internal/mechanisms`, `cmd/apogee`.)

### Self-regulation judges the NEXT Turn on four proxy signals, and only acted fires count

- **Next-Turn judgment (R3).** Fires recorded in Turn N are now judged by Turn N+1's outcome —
  a Mechanism's intervention can only show up in what the model does next — instead of by the
  Turn they fired in. Each completed Turn is classified **three-way**: *productive* (a novel
  file read, or a successful write/action), *harmful* (a tool-result error, or an empty final
  response — both newly-recognized harmful signals; they used to merely be "not productive"),
  or *neutral* (neither — e.g. a substantive text-only answer), with productive winning when
  signals mix. Adaptive-Suppression strikes and the Turn-Budget streak advance **only on a
  harmful Turn**; a neutral Turn freezes both; a productive Turn stays the global clear-path.
  Consequence (the review's point): a pure Q&A session neither strikes Mechanisms nor trips
  the Turn Budget. A cancelled Turn's rollback now also restores the novelty credit of the
  reads it booked, so the mandated re-attempt is not penalized as a wasted re-read.
- **Fired means acted (R4).** A catalogued Mechanism is booked (`recordFire` +
  `MechanismFiredEvent` + the judgment set) only when its invocation **intervened**: it
  returned a non-zero post-response Action, or it mutated its working value —
  `Request`/`Response`/`Conversation` gain an internal revision counter with an engine-seam
  `Revision()` accessor, and the tool-stage hooks compare call/result snapshots. An
  inspect-and-do-nothing invocation is no longer a fire, matching apogee-sim's `FiredCounts`
  (interventions, not invocations); `LoopView.Fired` therefore counts actions. Experimental
  hooks keep the always-booked behaviour under the synthetic ID (bench observability).
- **The experimental sentinel ID is now reserved in domain (R5).** The `"experimental"`
  constant moves to `domain.ExperimentalMechanismID`, and `MechanismRegistry.Add` refuses a
  catalogued Mechanism claiming it — a real Mechanism can no longer masquerade as the bench's
  own instrument. (`internal/agent`, `internal/domain`.)

### Registry + config hardening: duplicate IDs refused, every `mechanisms:` key validated

- **`MechanismRegistry.Add` refuses a duplicate `MechanismID`.** Two Mechanisms registered
  under the same ID used to pass `Add` and be silently collapsed to one by the dispatch
  order's ID map; the second `Add` is now a loud error naming the ID — the same startup-gate
  posture as the reserved-sentinel refusal above.
- **A typo'd `mechanisms:` key now fails startup even when mapped to `false`.** README and
  the starter `config.yaml` always promised a loud unknown-ID error, but only *enabled* keys
  were checked (through the build path) — a misspelled `false` entry was silently accepted.
  Every key is now validated against the catalogue's known IDs (`mechanisms.KnownIDs`):
  disabled keys are checked by name — a disabled Mechanism is still never constructed — and
  the error lists the known catalogue exactly like the enabled-key path.
  (`internal/domain`, `cmd/apogee`.)

### The Phase-4 wave-1 review pass is closed out

- **The 2026-07-04 review of Phase-4 items 1–6 landed as five corrective fixes plus a docs
  close-out** (`docs/plans/phase-4-review-fixes-plan.md`), each detailed in its own entry
  above. The behaviour changes in one line: post-response corrections **retry in place**
  within the same Turn (amended catalogue C5, R1); `autofix` probes formatters **at
  construction** and repairs only when it reduces the issue count, running **before**
  `syntax`; self-regulation judges the **next Turn** three-way on four proxy signals and
  books only **acted** fires; the registry and config **refuse duplicate, reserved, and
  unknown** mechanism IDs loudly. The deliberate divergences from the sim (the R2 retry-
  ladder refinements and per-mechanism throttle counters — bench-pending) are recorded in
  the catalogue, and the Phase-4 detail plan carries the review's NOTES trail under items
  3, 5, and 6. (Docs: `docs/design/mechanism-catalogue.md`,
  `docs/plans/archived/phase-4-detail-plan.md`.)

### Wave 2: the `truncate_history` drop-the-middle history rewrite (`correct_tool_result` deferred)

- **A cheap, structural alternative to generative Compaction is ported (catalogue Table A).**
  `truncate_history` is a history-rewrite Mechanism that drops the middle of the conversation,
  keeping the protected prefix (leading system messages + the first user message,
  `Conversation.PrefixEnd`) and the last few assistant-anchored exchanges, cutting **only** at
  `Conversation.AssistantBoundaries()` so a tool result never gets separated from the assistant
  call that produced it (strict chat templates reject an orphaned tool message). At the cut it
  inserts a single static gap note; when fewer exchanges exist than the keep window it is a
  no-op (and books no fire — the loop keys acted fires on `Conversation.Revision`, R4). Ported
  verbatim from apogee-sim `internal/sim/intervention.go` `truncateHistory` @pin. Capability
  **proactive-nudge** (a context-shaper — disabled under Bypass, D5, while the structural Budget
  and Compaction stay on, D6), SuppressionPolicy **strikes-3**, default **off** (D1). It ships in
  the `internal/mechanisms` catalogue, buildable via the `mechanisms:` config block.
- **No phantom acted-fire on an ungrown, already-truncated history (second-review fix).** Re-running
  `truncate_history` when the conversation has not grown a new assistant boundary since the last cut
  used to re-drop and re-insert the same gap note — rebuilding the identical shape but bumping
  `Conversation.Revision`, which the loop reads as an acted fire (R4). The rewrite now detects that the
  only pending drop is the gap note it inserted last time and returns without mutating, so Revision
  stays put and no `MechanismFiredEvent` is booked. The truncation content stays sim-faithful and the
  grown-history path (real middle to shed) still truncates and books normally. (`internal/mechanisms`.)
- **`correct_tool_result` is deferred, not ported (owner-ratified 2026-07-04).** The pinned sim
  defines no production trigger for it — it is a lab-only intervention with an operator-supplied
  correction — so inventing gating logic would ship behaviour with no evidence. The loop already
  exposes the lab surface (an experimental post-tool-result hook can replace a result via the
  mutation API), so the bench plays the operator without a catalogued Mechanism; a bench-discovered
  trigger would motivate a new plan item. (`internal/mechanisms`; catalogue Table A/B.)

### The Budget allocator + usage-calibrated token accounting make `LoopView.Budget()` honest

- **`LoopView.Budget()` now reports honest token accounting.** The loop's former trivial
  `defaultCharsPerToken = 4.0` estimate is replaced by a per-Session `TokenEstimator`
  (`internal/context`) the loop **calibrates against server-reported usage**: each Turn, the
  reported prompt tokens snap `Budget.Used` to the real context fill, and prompt-tokens vs the
  characters actually sent recompute the chars→token ratio — bounded to a sane range `[2, 8]` and
  smoothed (an exponential moving average) so the ratio converges toward the model's real
  tokenizer across Turns while a single anomalous report cannot swing it. Uncalibrated (a fresh
  or resumed Agent, before its first `UsageEvent`) it reports the default ratio and a zero `Used`.
- **The Budget is now the single authority on how much room each part gets (CONTEXT: Budget).**
  `internal/context.Allocate` splits the discovered context window (`n_ctx`) across a response
  reserve and the prompt's parts — system prompt, file context, conversation history — with the
  parts summing to the window exactly; an unknown window yields the zero allocation (treated as
  unbounded). `domain.Budget` gains the advisory `ResponseReserve`/`SystemPrompt`/`FileContext`/
  `History` fields (additive; the root `apogee.Budget` alias picks them up), which the item-9
  context reducers will consume. It is **structural**, not a Mechanism: it stays live under
  Bypass (D5/D6). Nothing in the request path is reshaped by it yet — the allocation is advisory
  until the reducers land. (`internal/context`, `internal/agent`, `internal/domain`.)

### Tool-result capping + the automatic Compaction trigger — the two Budget consumers

- **`tool_result_cap`: a config-gated tool-result capping Mechanism.** The surviving half of
  apogee-sim's `compress` (catalogue C3 SPLIT), ported as a pre-request Mechanism: any single tool
  result whose content exceeds its fraction of the Budget (40% of the working window — the window
  less the response reserve — in characters, via the calibrated chars→token ratio) is trimmed to a
  head/tail-plus-elision-marker form through `Request.SetMessageContent` (an in-place edit), while
  the **most recent tool-call Turn is always protected**. Default-off (D1); `proactive-nudge` /
  `strikes-3`, so Bypass disables it and it self-regulates like its peers. (`internal/mechanisms`.)
- **Automatic, budget-driven Compaction.** The generative `Compact` (the `/compact` reducer) now
  also fires **automatically**: at a quiescent boundary, before a Turn's request is built, the loop
  folds the conversation when `internal/context.HistoryExceedsAllocation` reports the history has
  outgrown its Budget `History` allocation. It runs the same fold (protected prefix, `Replace`
  write-back) before it consumes new input, so a just-submitted message survives as its own turn;
  it is non-reentrant, and a fold fault surfaces as an `ErrorEvent` leaving history untouched. It is
  **structural**, not a Mechanism: on by default and **on even under Bypass** (a naked model still
  overflows its window — decision 12), with a file-only `auto-compact: false` opt-out
  (`ContextConfig.CompactionEnabled`). The on-demand `/compact` is unaffected by the gate.
  (`internal/context`, `internal/agent`, `cmd/apogee`.)
- **Auto-compaction is Exchange-boundary-only and saturates on an oversized prefix (second-review
  fix).** The automatic trigger now also requires **not** `inExchange`: a mid-Exchange over-budget
  Turn (a tool continuation) defers the fold to the next Exchange opening rather than folding a
  half-finished Turn into a summary (`tool_result_cap` is the mid-Exchange relief valve). A fold that
  still cannot bring the history under its `History` allocation — the protected prefix (system prompt
  + first user message) alone exceeds it — emits exactly one `compaction` `ErrorEvent` and then
  **stands down** until the estimate drops back under the allocation (growth alone no longer thrashes
  the fold every Turn); the on-demand `/compact` ignores saturation. And a mid-Exchange history
  rewrite (`truncate_history`) now **repairs `exchangeStart`** by the drop delta, floored just past
  the prefix + gap note, so `AbortExchange` (Esc) rolls back to exactly the Exchange boundary with no
  orphaned tool results. (`internal/agent`.)
- **The saturation latch is now gated on a fold that ran (third-review fix).** A `Compact` that
  **skips** (too few messages past the protected prefix to be worth folding) folds nothing, so it
  proves nothing about whether folding can help — yet the auto-trigger used to run its post-fold
  saturation check on the skip too, latching off (one `ErrorEvent`) and permanently disabling
  auto-compaction whenever the history was over its allocation but too short to fold. `autoCompact`
  now returns on `Result.Skipped` before the saturation check, so only a fold that **ran** and still
  left the history (protected prefix + summary) over its allocation can saturate; a skipped boundary
  re-checks for free at the next opening. (`internal/agent`.)
- **Context-window discovery for pinned models + a `context-window:` key (second-review fix).** A
  configured `model:` no longer silently disables the Budget and automatic Compaction. Window
  discovery is split out of `resolveModel` and now runs for a pinned model too — keeping the pinned
  id but adopting the server's advertised window — and is **non-fatal**: a failed probe leaves the
  window unknown with a one-line notice, so an offline pinned-model start still works (the no-model
  path keeps its existing fatal semantics). A new file-only `context-window:` key (tokens) overrides
  discovery and skips the probe. When the window is still unknown while Compaction is on, startup
  prints one notice naming the consequence and the key. (`cmd/apogee`, `internal/domain` comment.)
- **No redundant context-window probe on the no-model path (third-review fix).** When the server
  advertised no window on a zero-config (no-model) startup, `resolveModel`'s discovery probe left the
  window at 0, so the separate `resolveContextWindow` self-guard (`opts.contextWindow > 0`) did not
  fire and it probed the server a second time. `resolveModel` now reports whether it probed and the
  root skips `resolveContextWindow` when model discovery already ran — one probe for the whole
  no-model startup, regardless of the advertised window. The pinned-model path is unchanged (still
  probes for its window; a failed probe stays non-fatal). (`cmd/apogee`.)
- **`context-window` precedence and the `ContextConfig` threading are now pinned by tests
  (third-review fix, Tests).** A test proves a `context-window:` key wins over the server-advertised
  window on the no-model path (`resolveModel` keeps the discovered id but not the advertised window),
  and a `runRoot` test proves `opts.contextWindow` reaches `Config.Context.MaxContextTokens` (via the
  loud-zero notice) — closing the mutation gap the pinned-model-only coverage left open. (`cmd/apogee`
  tests.)
- **`cached_content_intercept`'s schema-gate conservative fallbacks are now pinned by tests
  (third-review fix, Tests).** A redundant re-read that would otherwise be capped is proven left
  byte-identical (no fire, R4) when the pending read tool is absent from the (toolfilter-narrowed)
  menu, carries an empty schema, or carries a schema that does not parse — closing the mutation gap
  the earlier coverage left silent. (`internal/mechanisms` tests.)

### Wave 3: the `toolfilter` / `filehint` / `grammar` request shapers

- **`toolfilter`: relevance-scored tool-menu narrowing.** A pre-request Mechanism that trims the
  tool menu for small models, ported from apogee-sim `internal/toolfilter` @pin. It activates
  reactively — only when the menu is large (30+ tools) or the model has hallucinated a tool absent
  from the menu — and never when the menu is already within the keep limit (10). It scores each tool
  against the last user message's keywords (exact name > name-part > description match), keeps every
  recently-used tool whole (plus the read-only exploration tools when the request is analysis-focused),
  and re-sets the menu to the top-scored subset via `Request.SetTools`. The narrowing is
  **request-scoped** (the loop rebuilds the full menu each Turn, so it never mutates the menu
  globally) and deterministic (stable score-tie ordering). It declares `Before decompose` (item 12).
- **`filehint`: role-safe workspace file hints.** A pre-request Mechanism ported from apogee-sim
  `internal/filehint` + `file_hint_detector` @pin. After the model lists a directory but before it
  reads anything, it scores the listed files against the user prompt (a TF-IDF-ish weight plus a
  language-extension boost) and injects a hint suggesting the most relevant files to read, through
  the role-safe `Request.InjectContext` (which folds into the system prompt when the conversation
  ends in a tool result). A stable marker makes the inject **idempotent** (no double-inject), and a
  greenfield-creation task with no files written yet is suppressed.
- **`grammar`: a backend-capability-gated json_schema constraint.** A pre-request Mechanism ported
  from apogee-sim `internal/grammar` + `injectGrammarConstraint` @pin: it derives a `json_schema`
  from the current tool menu and sets it as the request's `response_format` so a model that cannot
  emit native tool calls is constrained to a valid tool-call shape. It is **capability-gated** by the
  new D3-injected `mechanisms.Deps.GrammarConstraint` — false on every current apogee backend (no
  such probe is wired, and the provider wire does not yet carry request extras), so grammar **no-ops
  today** (catalogue Table B). An existing `response_format` always wins.
- All three ship default **off** (D1), `proactive-nudge` / `strikes-3` (disabled under Bypass, D5;
  self-regulating), buildable via the `mechanisms:` config block. (`internal/mechanisms`.)
- **`toolfilter` / `filehint` carry the sim's camelCase spellings (second-review fix).** The
  analysis-keep set (`toolfilter`) now also holds the sim's `readFile`, and the directory-listing set
  (`filehint`) holds the sim's `listFiles` — completing the item-10 "plus the sim spellings" claim so
  a mixed MCP menu with camelCase tool names still narrows and hints. (The write-tool and file-read
  sets already carried every sim spelling.) (`internal/mechanisms`.)
- **The sim-seeded pre-request ordering edges are now declared (second-review fix).** The catalogue's
  §Ordering seeds are now live `OrderingConstraints`, not just prose: the `cot` nudges (`stall_nudge`,
  `list_nudge`, `tool_use_directive`) and `library` inject `Before toolfilter`, and `tool_result_cap`
  runs `After decompose` — so it sorts last among the pre-request shapers, trimming tool results after
  context is assembled. Previously the order rested on the D4 ID tiebreak alone, which matched the sim
  for the nudges/library but sorted `tool_result_cap` *before* `toolfilter`. Table A's "none" cells
  were amended per D7 to record the edges, so §Ordering, Table A, and the code now agree, and a
  regression test pins the resulting order. (`internal/mechanisms`, `docs/design/mechanism-catalogue.md`.)

### Wave 3: the history-aware `error_enrichment` / `read_loop` / `read_repeat` / `tool_loop_interceptor` / `cached_content_intercept` family

The cross-turn aggregators, ported from the pinned apogee-sim source (catalogue Table A/B), each
deciding by scanning the conversation across Turns at its **relocated** hook point. All ship default
**off** (D1), `strikes-3` and non-exempt (so disabled under Bypass, D5), buildable via the
`mechanisms:` block. (`internal/mechanisms`.)

- **`error_enrichment`: repeated-error clarification at post-tool-result.** Ported from apogee-sim
  `internal/proxy/error_enrichment` @pin and relocated to post-tool-result: when a write-tool call
  fails, and the same file already failed the same way earlier this Session, it appends
  category-specific guidance (syntax / import / type / build / permission / runtime) to the failing
  result the model reads next. The current failure uses the authoritative `ToolResult.IsError`; prior
  failures in history are string-classified (a committed tool-result message no longer carries the
  flag). A marker keeps one hint per repeated-error episode.
- **`read_loop`: the consolidated read-loop detector at pre-request.** Ported from apogee-sim
  `internal/proxy/read_loop_detector` @pin, folding the sim's three variants (normal / greenfield /
  successful) into one Mechanism (catalogue C2): a role-safe hint fires on repeated failed reads of
  the same file (threshold 1 on an empty workspace, 2 otherwise) or three successful re-reads without
  a write. The deterministic hint is its own idempotency marker.
- **`read_repeat`: redundant re-read retry at post-response.** Ported from apogee-sim
  `internal/proxy/read_repeat_interceptor` @pin: when the whole response only re-reads files already
  read successfully in a recent Turn, it retries in place (`ActionRetry`, R1) with a "you already
  read these, proceed" correction.
- **`tool_loop_interceptor`: identical-repeat-turn detector at post-response.** Ported from apogee-sim
  `internal/proxy/tool_loop_interceptor` @pin (inventory-missed, found in the checkout — catalogue
  Table B): when the response repeats the previous Turn's exact tool-call key, it retries with a
  loop-breaking directive. The sim's per-Session count threshold and 30s cooldown are dropped (R2 —
  self-regulation and the loop retry cap substitute).
- **`cached_content_intercept`: redundant-re-read cap at pre-tool-exec.** Ported from apogee-sim
  `internal/proxy/cached_content_intercept` @pin and relocated to pre-tool-exec: a re-read of a file
  already read successfully and unchanged since is capped to a header-only slice, reclaiming the
  window the full re-dump would cost (the content is already in context). The sim rewrote the result
  post-execution; pre-tool-exec has no result-substitution primitive, so the port expresses the same
  token-saving intent through the pending call's arguments.
- The re-read family (`read_loop` / `read_repeat` / `cached_content_intercept`) is pairwise
  **incompatible** — at most one is enabled at a time (the sim's per-request exclusivity as an apogee
  startup gate). In the post-response cascade the resolved dispatch order is
  `read_repeat → tool_loop_interceptor → validate → autofix → syntax` (the sim's response-side
  priority).
- **Write detection now sees apogee's own edit tools (second-review fix).** The history family's
  "did this call mutate a file / was it a write action" checks (`read_repeat`, `read_loop`,
  `cached_content_intercept`, `error_enrichment`, `tool_loop_interceptor`, the off-ramps,
  `deriveWriteTarget`) moved from the sim-only `isWriteTool` set to a new apogee-complete
  `isFileMutatingTool` predicate that also counts `edit_existing_file` /
  `single_find_and_replace` / `multi_find_and_replace`; the content-repair Mechanisms (`syntax`,
  `autofix`) stay on the narrower sim-only set (their payloads are file fragments, not full files).
  `open_file` joins the family read set (its result places file content in the conversation like
  `read_file`). And `read_repeat` now collects each turn's write paths **before** its reads, so a
  same-turn read-then-write to a path no longer counts that read as a redundant re-read.
- **`cached_content_intercept` gates its cap on the tool schema (second-review fix).** The read cap
  is now applied only when the pending tool's argument schema (via `view.Tools()`) declares a
  `max_lines` property; a read tool lacking it — e.g. a strict MCP server with
  `additionalProperties:false` — is inspected but never handed an argument it would reject, so the
  re-read proceeds uncapped and no fire is booked. This makes the mechanism's "benign no-op" fidelity
  note literally true instead of relying on the third-party tool tolerating an unknown field.
- **The `isFileMutatingTool` history-family sites now have edit-tool coverage (third-review fix, Tests).**
  Tests exercise `edit_existing_file` / `single_find_and_replace` at the three sites the earlier
  suite left untested and that can carry regression-detecting coverage: `empty_response_recovery`
  treats a recent edit as progress worth recovering (`hasRecentProgress`), the `tool_loop_interceptor`
  directive credits an edit as work already done (`extractConversationContext`), and the `read_loop`
  hint excludes an edit-written path from its "create X" suggestion (`writtenPaths`) — each test fails
  when its site is mutated to exclude the edit tools. The fourth site (`wroteRecently` in the
  `tool_use_enforcer`) cannot be pinned: `shouldEnforceToolUse`'s `!hasEverUsedTools` gate stands the
  enforcer down whenever any edit call is present, so the `wroteRecently` edit branch is never the
  deciding factor — documented in place rather than covered by a vacuous test. (`internal/mechanisms`
  tests.)

### Wave 4: the `decompose` request shaper + the `stall_nudge` / `list_nudge` / `tool_use_directive` completion nudges

The last of the request shapers, ported from the pinned apogee-sim source (catalogue Table A/B), each
a pre-request Mechanism shipping default **off** (D1), `proactive-nudge` / `strikes-3` (disabled under
Bypass, D5; self-regulating), buildable via the `mechanisms:` block. (`internal/mechanisms`.)

- **`decompose`: one-step focus + history collapse.** Ported from apogee-sim `internal/decompose`
  @pin. For a small model that stalls on long multi-step prompts it (1) collapses the complex
  multi-step user messages still sitting in conversation history to a short task summary (via
  `Request.SetMessageContent`) so the model cannot re-read a full step-by-step plan from an earlier
  turn, and (2) hints the single next actionable step of the current prompt into the system prompt
  (via the idempotent `Request.AppendToSystem`), leaving the full user message intact. It declares
  `After toolfilter` (trim the menu before the user-message rewrite — the mirror of toolfilter's
  `Before decompose`).
- **The read-loop coupling gates active decomposition (D2).** decompose's `RequestMeta.FiredCounts`
  peek in the sim becomes a live `LoopView.Fired("read_loop")` query: once the consolidated
  `read_loop` Mechanism has **acted** this Session (R4), active decomposition — which would override
  the focus to "step 1: …" and fight the read-loop hint — is muted, while the harmless history
  collapse still runs.
- **The completion nudges are the `cot` family, split three ways (catalogue C4).** apogee-sim's `cot`
  Transform is not itself a tracked Mechanism — it emits three tracked nudges, which apogee ships as
  three independent pre-request Mechanisms: `tool_use_directive` (an action was asked for but the
  model has not used a tool yet → "use a tool"), `stall_nudge` (read-only for the stall threshold of
  turns with a write tool available → "proceed with the modifications"), and `list_nudge` (an analysis
  request that listed directories but read no files → "read the files you found"). Each injects one
  system directive through the idempotent `AppendToSystem`; the "nudge cap" is a stateless window on
  the read-only turn count. `stall_nudge` ⊥ `list_nudge` (contradictory directives) — declared
  `IncompatibleWith`, so at most one is enabled per config (the apogee startup gate subsuming the
  sim's runtime `!wantListNudge` preference).
- **`intent` and `cot` are folded, not ported as Mechanisms (catalogue C4/C6).** The shared intent
  classifier (`hasActionIntent` / `hasAnalysisIntent`) already landed inline in wave 1 and is reused
  here; `cot` ships only as its three nudges. This closes the Phase-4 request-shaper catalogue —
  `library` (item 14) is the only remaining un-ported catalogue Mechanism.

### The Library learning substrate: a confidence-tagged `ModelFingerprint` and a file-backed store

The substrate the Library Mechanisms (item 14) build on — no Mechanism yet, so nothing observes or
injects until item 14 wires it. (`internal/domain`, new `internal/library`.)

- **`ModelFingerprint` — a confidence-tagged model identity.** New `domain.ModelFingerprint`
  (`Label` + `FingerprintConfidence`) and the `FingerprintResolver` seam. `internal/library`'s
  production resolver returns the best available tier: a **weights-hash (high)** when the model id is
  a reachable weight file (`.gguf` / `.ggml` / `.bin` / `.safetensors`) — a SHA-256 over the file size
  plus its head and tail, so two builds sharing a label but differing in weights diverge without
  hashing multi-gigabyte files at startup — else the **metadata label (low)** (the bare model id). The
  **behavioral-probe (medium)** tier is the Phase-5 `apogee probe`: the enum slot and the resolver
  interface exist so it slots in behind the same seam, but no resolver produces it yet (D8).
- **A file-backed, versioned Library store.** New `library.Store`, rooted at an injected directory
  (`Config.LibraryDir`) and **never** an ambient `~/.apogee` (ADR 0001) — the bench's isolated root
  falls out for free (decision 11). It holds per-fingerprint observations (`Entry`) with the sim's
  Bayesian confidence counts (`Score = (observations − successes + 1) / (observations + 2)`, capped at
  0.95), so a pattern the model grows out of stops qualifying for injection without being deleted. It
  persists to a single `library.json` with a schema `Version` (like `domain.Session`), is process-local
  (a mutex guards intra-process access; no cross-process locking claims in v1), and degrades a missing,
  corrupt, or too-new store to **empty-with-a-soft-error** (the skills-catalog posture — a broken
  Library never bricks a run). A zero fingerprint (unidentified model) is inert: nothing is recorded.

### The Library Mechanism: cross-session observe + confidence-gated inject

Item 14 wires the Library substrate (item 13) into the loop as the catalogued `library`
Mechanism — default-off (D1), fully inert under `--bypass` (it is `proactive-nudge`, so item 2's
dispatch gate skips both halves). The single `library` catalogue row is realized as ONE Mechanism
implementing BOTH hooks. Ported from apogee-sim's `library` observer/transform. (`internal/mechanisms`,
`cmd/apogee`.)

- **Observe (post-response).** After each response the Mechanism records completed-Turn outcomes into
  the store, keyed on the model fingerprint: tool-call validation failures (corrections),
  narration-instead-of-acting and shallow-exploration behavioural patterns, examples of valid complex
  tool calls, and the success signal that decays a pattern the model has grown out of. It is a pure
  observer — it never mutates the response and books no fire, so it does not skew self-regulation.
- **Inject (pre-request).** When the fingerprint clears the confidence gate — "prefer not to inject
  under uncertainty", so a low-confidence metadata-label identity does **not** inject — the Mechanism
  appends the highest-scoring qualifying observations to the system prompt (idempotent on a marker),
  intent-filtered and capped to a 200-token injection budget, and backs off when the window is nearly
  full.
- **Store + fingerprint injected at construction (D3).** `cmd/apogee/wire.go` constructs and Loads the
  store under `Config.LibraryDir` (never an ambient `~/.apogee`, ADR 0001) and resolves the model
  fingerprint once, wiring both into the constructor `Deps` only when `library` is enabled — so the
  inject and observe halves share one identity, and a config without `library` reads no store file.
  Two agents on two `LibraryDir`s stay isolated (decision 11). Longitudinal bench validation
  (improves-over-sessions AND never-below-baseline) stays **pending**.
- **Stored observations are now treated as untrusted data (second-review fix, Security).** Library
  entries persist model- and tool-result-derived text and re-inject it into a future system prompt, so
  the store is now hardened against a hostile-repo → store → system-prompt payload channel. A new
  `library.SanitizeContent` strips control characters, folds CR/LF (and any whitespace) into single
  spaces, and collapses runs; it runs at `Store.Record` time — so poison never lands on disk in
  directive-capable form — **and** again when the injection block is rendered, defending stores written
  before this landed. The complex-call "example" observer records only the call **shape** — the tool
  name and its sorted parameter **names** — never argument **values**. The injected block's header now
  opens with an explicit data-not-instructions frame so entries cannot read as directives. No store
  schema bump (entries stay compatible). (`internal/library`, `internal/mechanisms`.)
- **The sanitizer now strips Unicode format characters, and example param names are schema-filtered
  (third-review fix, Security).** `SanitizeContent` stripped only Cc controls (`unicode.IsControl`), so
  bidi overrides, zero-width characters, the BOM and soft hyphens rode through into the store and the
  injected block; the strip now also covers Cf/Co/Cs. And the complex-call "example" recorded the raw
  keys of the model's arguments object — free-form, model-controlled strings — so a junk key bearing
  directive text could land on a clean observation. The recorded names are now the **intersection** of
  the call's argument keys with the tool schema's declared `properties`, and a call whose schema yields
  no properties records no example at all (prefer not to record under uncertainty); the 5+-param
  complexity gate reads the schema, never the argument keys, so junk keys can never promote a simple
  call. (`internal/library`, `internal/mechanisms`.)
- **Bypass leaves a pre-seeded Library store byte-for-byte untouched (second-review fix, test-only).**
  A loop-level test seeds a populated `library.json`, wires a registry-backed agent with `library`
  enabled and `Config.Bypass` on, drives an observe-triggering Exchange, and asserts the store file's
  bytes are unchanged — the item-14 mandate now has its literal regression. (`internal/agent`.)

### Bench-readiness proof: the embeddable two-arm contract is now a permanent regression

Item 15 adds `benchreadiness_test.go`, the executable definition of "benchable" (ADR 0001): a
root-package consumer test that drives the real Agent exactly the way apogee-sim will — the public
`New` / `Resume` / `Submit` / `Step` / `Snapshot` / `Close` surface over the real provider client
dialing one scripted OpenAI-compatible httptest model, catalogued Mechanisms enabled via `Config`
(`toolfilter` / `decompose` / `truncate_history` / `library`), and experimental hooks at all five
hook points. It constructs a mechanisms-on arm and a **Bypass** arm against isolated temp state
roots, Steps both to their quiescent boundaries, then Snapshots and Resumes forks. It asserts: the
enabled shapers ACT in the registry's deterministic dispatch order visible in the
`MechanismFiredEvent` stream (`toolfilter` before `decompose`, then the experimental hook) while an
inspect-only Mechanism books no fire (R4); the Bypass arm fires no catalogued Mechanism yet runs all
five experimental hooks; agent-driven writes stay inside each injected root (the Library store lands
under the mechanisms-on arm's `LibraryDir`, the Bypass arm's stays empty); and forks resumed from one
snapshot diverge independently in their own roots. If a future change breaks the bench contract, this
test breaks first. Test-only — no product change. (root `apogee_test`.)

## [1.1.0] — 2026-07-03

Post-`v1.0.0`, **additive** (minor) — the start of the apogee-code TUI
feature-parity track. See
`docs/handoffs/2026-06-26 - 00 - chat-mini-language-core.md` and
`docs/handoffs/2026-06-26 - 01 - skills-system.md`.

### Drag-select-to-copy in the transcript (screen-space)

- **You can now drag-select text in the chat transcript and copy it to the clipboard**, the same
  gesture the prompt box already supported. A left-click-drag inside the transcript viewport
  highlights the span and, on release, copies the rendered text over OSC52 (`tea.SetClipboard` —
  cross-terminal and SSH-safe) with the usual "copied N chars" confirmation. The selection is
  **screen-space** ("copy what you see"): it anchors in content coordinates (rendered-line index +
  display cell) into the cached `m.lines`, so it survives a mid-drag wheel-scroll; on release it
  slices each spanned line with `ansi.Cut`, strips the styling, and trims the block's trailing pad.
  Markers, rail gutters, and soft-wrap breaks are copied verbatim (the accepted terminal-native
  semantics — the one-way render pipeline stays one-way, no line→entry reverse index). The mouse
  handlers arbitrate by region — a point in the input rectangle drives the prompt editor, a point
  in the viewport drives the transcript — so the two selections never coexist. The selection clears
  on any transcript change (a streamed token, a submit) and on resize; a bare click copies nothing.
  Drag auto-scroll at the viewport edge is deferred. (`internal/tui/mouse.go`, `model.go`.) Closes
  the "cannot select text in the transcript" ISSUES entry.

### Chat input lifted into a `promptEditor` module (internal refactor)

- **The chat input cluster now lives in its own type**, `promptEditor` (`internal/tui/prompteditor.
  go`), instead of scattered across the god-Model. It gathers the five loose input-side concerns the
  architecture review (candidate #3) called one coherent concept — the textarea, the autocomplete
  overlay (+ its `skillRegion` edge-trigger), the staged-skill chips, the workspace file cache, and
  the prompt drag-selection. The `Model` embeds it **anonymously**, so the fields and the
  self-contained methods promote onto the Model (`m.input`, `m.pendingSkills`, `m.caretTo(...)` all
  resolve through it) and every existing call site — and all package tests — stay unchanged. Model
  top-level field count drops **32 → 27**; the six input-cluster fields now have a single home.
- **Purely structural — no behaviour changes.** Only methods that touch nothing but the editor's own
  fields moved to it (`newPromptEditor`, `submitParse`, `reset`, `rows`, and the caret re-seat trio
  `caretTo`/`reseatCaret`/`reseatInput`); methods that also read Model-owned state (theme, window
  size, `Options`, lifecycle) deliberately stay on the Model rather than duplicate that state. The
  Model stays the coordinator (lifecycle state machine, transcript + render cache, stats/gauge,
  theme, layout); the editor never touches the engine — it only turns typed input into
  send-ingredients the Model routes. New editor-direct unit tests exercise the lifted logic without
  a Model or a fake engine (`internal/tui/prompteditor_test.go`).

### Model profile config surface (tool-call format + thinking channels)

- **`Config` gains a `Profile ModelProfile` seam** describing how the configured model speaks the
  wire (CONTEXT: Model profile) — its tool-call format (native / markdown-fenced / custom-regex)
  and its inline thinking-channel style (none / delimited `<think>…</think>` / gpt-oss harmony).
  The new public domain types are re-exported from the root facade (`apogee.ModelProfile`,
  `ToolCallFormat`, `ThinkingProfile`, `ThinkingStyle` and their consts) — an **additive minor**
  (decision #18). A **zero profile is native tool calls with no inline thinking**, so every
  shipped model behaves exactly as before (the byte-identical anchor).
- **Plumbed from `config.yaml`** as a file-only `model-profile:` block (a per-model concern, like
  `mcp-servers` — no flag/env), mapped to the domain type at the host boundary. **No loop consumer
  yet**: the loop's parse seam is crossed in a following change, so this is a pure, provably
  behaviour-neutral config-surface addition.

### Model profile wired into the loop (fenced/regex tool calls + thinking/harmony stripping)

- **The loop now consumes `Config.Profile` at the parse seam.** A new `processing.ParserFor(domain.
  ModelProfile)` translates the declarative profile onto `internal/processing`'s existing, frozen
  `ToolCallingConfig`/`ThinkingConfig` and returns the text-format `ToolCallParser` plus a unified
  `ContentStripper` (the `none`/`delimited`/`harmony` thinking styles behind one `Strip` +
  `IsMidChannel` interface). `internal/agent` selects both once in `newAgent`, so the oracle config
  types never surface in the loop and a bad profile (unknown format / thinking style) fails
  construction loudly rather than falling back to native.
- **At the seam:** the reply's inline thinking/harmony channel is stripped out of the visible
  content and preserved as `reasoning_content` in history (the harmony `commentary` channel folds
  into reasoning); when the structured **native** path produced no calls, a markdown-fenced or
  custom-regex tool call is recovered from the *stripped* visible content, its markup removed from
  the committed assistant text, and it is assigned a deterministic `text_call_<turn>` ID (not the
  oracle's wall-clock ID, so snapshot/resume and tests stay stable). Native calls always win when
  present.
- **A recorded, deliberate divergence from the apogee-code oracle:** a text-parsed call is stored
  **structurally** on the assistant message (`ToolCalls`), so dispatch, events, and snapshot/resume
  keep **one** path for every format; the oracle instead commits stripped text with only a
  tool-role result. Chat templates tolerate native-shaped history better than the loop tolerates two
  history shapes.
- **A zero profile is byte-identical** to the pre-change loop: the no-op stripper and no-op parser
  leave `reply.content` and the native calls untouched, so every shipped (native) model behaves
  exactly as before. The frozen `internal/processing` oracle types, parsers, and parity tests are
  unchanged — only the new `ParserFor`/`ContentStripper` and the loop caller were added. **Live
  in-flight token suppression while streaming is a following change; this fixes committed history
  and the final message.**

### In-flight thinking/harmony tokens held off the live stream (native unchanged)

- **`streamResponse` now emits a `TokenEvent` for the newly-revealed *visible* content**, not the
  raw content delta, using the same `ContentStripper`. While the accumulated content ends inside an
  unclosed inline reasoning span (`IsMidChannel`), token emission is held, so a model that inlines
  `<think>…</think>` or gpt-oss harmony channels no longer flashes that markup (or its reasoning)
  onto a live UI before the post-stream strip; the visible text is revealed once the span closes.
- **A native / no-inline-thinking profile is byte-identical, event-for-event:** the no-op stripper
  is never mid-channel and returns the content untouched, so every content delta emits verbatim and
  unbuffered exactly as before. A channel start token split across two deltas briefly reveals its
  partial prefix live (matching the oracle's `isThinking`); this recorded edge is accepted — the
  post-stream strip still removes it from the committed message and final `MessageEvent`.

### Fenced/regex models now receive a text tool menu + emission instructions (native unchanged)

- **A new `processing.InstructionsFor(domain.ModelProfile, []domain.ToolDef)` renders the emit
  side of a non-native profile:** the text tool menu (name, description, JSON-schema parameters)
  plus the format-specific tool-call instructions and a live example — ported from the apogee-code
  context builder, driven by the *same* profile knobs and defaults the parser reads, so what the
  model is told and what the loop parses cannot drift. It is the request-seam mirror of `ParserFor`.
- **`toProviderRequest` now injects the block and suppresses the native `tools` array for a
  non-native tool-call format.** The rendered menu + instructions are folded into the wire request's
  system channel (appended to a hook-seeded system message, else a sole system message at position
  0) and the native `tools` array is dropped — sending both would double-tell the model in two
  formats, and a chat template without tool support can error on the array. For a non-native profile
  the text menu is the **only** channel the model learns its tools from; before this change a
  fenced/regex model received a native array its template may not render and no instructions.
- **Wire-only, tracked per request:** the block never enters domain history, the snapshot, or any
  event — exactly like the native `tools` array, which is also rebuilt per request and never
  persisted. It is re-rendered over each request's **mode-filtered** menu, so a Plan-mode switch (or
  any menu change) is reflected on the next Turn with no history rewrite.
- **A native/zero profile is byte-identical:** `InstructionsFor` returns `""`, so there is no
  injection and no suppression — the native `tools` array and the message list are exactly today's.

### Dispatch decision collapsed into one Resolution verdict (internal refactor)

- **The per-call dispatch decision is now one `Resolution`**, computed by a single pure resolver
  (`internal/agent/resolution.go`): the tighten-only guard floor, the autonomy-ladder × blast-radius
  table, the confinement-capability check, and the precomputed runtime-demote contingency are all
  decided in full before anything executes. `internal/agent/dispatch.go` is now a thin executor that
  gathers facts, calls the resolver once, and carries the verdict out — it holds no ladder,
  guard-tier, or demote decision of its own. The old `disposition.go` decision path is retired.
  **No behavior change**: unexported and internal-only (no public API / semver impact). The term
  "disposition" is retired from code, surviving in prose only as the historical name of the
  post-guard ladder stage. `docs/design/confinement-execution-contract.md` §4 amended in place.

### MCP "allow for this session" now caches at server grain (ADR 0012 conformance)

- **Approving one of an MCP server's tools "for this session" now clears the whole server**, not
  just that one qualified tool: approving `github__search` pre-clears `github__create_issue` and
  every other `github__*` tool for the Session, honouring ADR 0012's server-grain promise (the
  cache had always keyed on the qualified tool name, so each `github__*` tool re-prompted). The
  allow-for-session cache key for an `mcp` gate is now `mcp-server:<alias>`; the `mcp-server:`
  prefix keeps that grain collision-proof against ordinary tool names, and a **different** server
  (`jira__*`) is never pre-cleared by another's approval. A **forced** gate (a Tier-2
  dangerous-action speed-bump) still skips the cache and re-prompts, unchanged. Every non-MCP
  class keeps the tighter tool-name grain, so nothing else loosens.

### Compact tool print-outs in the chat (full built-in coverage)

- **The TUI's tool-presentation registry now covers every built-in tool**, not just the
  Phase-2 four: the edit family, `view_diff`, `open_file`, `terminal`, `python_exec`, the
  git family, `diagnostics`, `web_fetch`, `http_request`, `web_search`, `sub_agent`, and
  `ask_user` each render as `✦ [Label] target` — no more raw tool names with JSON argument
  braces in the transcript. Only a dynamic (MCP) tool keeps the raw-name + JSON fallback.
- **Results no longer dump raw into the chat**: `web_search` shows "N results", the fetch/
  request tools their `HTTP 200 OK` status line, free-form output (a command run, a
  diagnostics or sub-agent report) its first line plus a "+N more lines" count, `open_file`
  its Located line or a line count. `view_diff` renders red/green diff lines (the reserved
  diff detail kinds get their first producer), capped at 20 lines.
- Detail and target lines are clipped at 160 runes so a minified blob cannot flood a row.
  The approval dialog still shows the full pretty-printed arguments — the security surface
  (the model's request is never hidden) is unchanged.

### Web search works out of the box (DuckDuckGo default)

- **`web_search` is now default-ON**: with no `web-search-endpoint` configured it uses a
  built-in DuckDuckGo HTML provider — no config, no API key (reverses the P3.11 default-off
  decision; the predecessor apogee-code shipped the same built-in). Set
  `web-search-endpoint: off` (or `none`/`disabled`) to disable the tool — a graceful
  "web search is disabled" result, no request made.
- **The DuckDuckGo provider POSTs the query** as a form field, the way DDG's own search
  form submits: the HTML front-end answers a plain GET with its bot-challenge ("anomaly")
  page — zero result anchors, so every search rendered "No results found". A custom
  endpoint keeps the `q` GET-parameter contract unchanged.
- **An explicitly configured DuckDuckGo endpoint selects the built-in provider**: an
  endpoint whose host is `html.duckduckgo.com` (with or without scheme) now gets the same
  POST + browser-header treatment as the default, instead of degrading to the
  custom-endpoint GET that DDG answers with the challenge page.
- **Results are auto-cleaned**: the DuckDuckGo page (and any custom endpoint's HTML
  response, by Content-Type or body sniff) is parsed into numbered `title / url / snippet`
  results; a custom endpoint's JSON/text response still passes through verbatim. A
  rate-limit/consent page degrades to "No results found", never a crash.
- **Non-2xx responses are now tool errors** naming only the status and endpoint host
  (previously the status + raw body passed through as a normal result). The M2 key
  redaction (`endpointHost`/`scrubURLError`) and the always-on SSRF floor are unchanged.
- **Scheme-less custom endpoints self-heal**: an endpoint like `search.example.com/s`
  (no `https://`) used to parse with an empty host and every request was rejected by
  url-safety; it now self-heals to `https://`. This repairs hand-edited configs — the
  shipped config template never carried a broken value (its endpoint line was always
  commented out), and first-run seeding never overwrites an existing config.

### Context compaction (`/compact`)

- **`/compact` now performs real generative compaction** (replaces the
  `ErrCompactionNotImplemented` stub). The new `internal/context.Compact` reducer
  summarizes the conversation through a single upstream call and replaces the folded
  history with one assistant summary message, keeping the protected prefix (leading
  system messages + the first user message, `Conversation.PrefixEnd`) verbatim so the
  original task framing survives. A conversation with too little past the prefix is
  skipped; a summary-call failure or cancellation leaves the history untouched.
- **Wired through `Agent.Compact`** (guarded to a quiescent boundary like `ClearContext`,
  returning `ErrInputPending` mid-Exchange). The summary call is *silent* — it reuses the
  loop's request projection but emits no `TokenEvent`/`UsageEvent`, so it neither streams
  into the transcript nor moves the live gauge; it runs at low temperature.
- **TUI** drives `/compact` on a worker goroutine (it is a real upstream call and must not
  block the `Update` loop — ADR 0011): the spinner runs, `Esc` cancels, and on success a
  "context compacted" note lands while the context-fill gauge resets so the next Turn
  re-measures the smaller fill.
- **Removed** the now-unused `ErrCompactionNotImplemented` sentinel (it was never in a
  released version).

### Fixes

- **Prompt box no longer scrolls the first line out of view as it auto-grows.** Typing past the
  wrap width grew the input box, but bubbles' `textarea.SetHeight` only repositions its internal
  view when the caret falls *outside* it — never when the box grows — so a stale downward scroll
  offset survived: the first line was hidden above and a phantom blank row showed below, with the
  caret pinned to the top visual row. `layout` (`internal/tui/model.go`) now re-seats the caret
  after a height change through the shared `reseatCaret` idiom (`MoveToBegin` "unscrolls" to the
  top, then the widget's own `CursorDown` walks back to the caret's real row, re-clamping the
  offset with none of the textarea's wrap re-derived); it runs only on an actual height change, so
  vertical caret navigation keeps the widget's sticky goal column. A companion fix corrects
  `inputContentRows` (`internal/tui/render.go`) to count the trailing row the textarea reserves for
  a logical line that exactly fills the width, so the box no longer sizes one row short at a
  wrap-fill boundary (which had stranded the same offset the re-seat could not then reach). At the
  `maxInputRows` cap the textarea's legitimate internal scrolling is preserved (offset =
  contentRows − height). Closes the prompt-scroll and auto-sizing ISSUES entries.

- **Auto mode now works on macOS — seatbelt fences the workspace correctly.** The
  `sandbox-exec` profile embedded the box's writable roots verbatim, but seatbelt
  matches a write against its *kernel-canonical* path; on macOS `/tmp` and `/var`
  are symlinks into `/private`, so a box rooted at `/var/folders/...` never matched
  the resolved `/private/var/folders/...` and seatbelt denied **every** in-workspace
  write — Auto mode could not write at all. `seatbeltProfile`
  (`internal/platform/seatbelt.go`) now resolves each writable root through symlinks
  (`filepath.EvalSymlinks`, falling back to the cleaned path for a not-yet-created
  root) before emitting the `(subpath ...)`, so the profile matches the kernel's view
  and agrees with path-safety (which already resolves the same way). Landlock is
  unaffected — it is fd-based (`unix.Open(root, O_PATH)`), so the kernel resolves
  symlinks to the inode the rule keys on. Closes the `v1.0.0` "Box-root
  canonicalization" post-release residual; verified on real macOS hardware
  (`TestSeatbeltProbe` in-box write rows now pass under live `sandbox-exec`).

- **Context window now reads the runtime size from llama.cpp `/props`.** Discovery
  (`internal/provider.Discover`) probes `GET /props` after `/v1/models` and prefers
  its `default_generation_settings.n_ctx` — the `-c`/`--ctx-size` the server was
  actually launched with — over the model's advertised *training* window
  (`context_length`, else `meta.n_ctx_train`), which is often far larger than the
  loaded window. This fixes the live context-fill gauge measuring usage against the
  wrong denominator (it barely moved on a server loaded well under its training
  context). Best-effort: a non-llama.cpp server (no `/props`) keeps the `/v1/models`
  value, and a probe failure never fails discovery. Ports the oracle's previously
  deferred `llamacpp-props` strategy; the `ollama-show`/`ollama-tags` strategies
  remain unported (additive, not needed yet).

- **`/compact` and the context gauge now tell the truth.** Four fixes to the
  compaction/gauge seam that had it reporting outcomes it did not produce:
  (a) an Esc landing *after* a compaction committed reported "cancelled" while the
  history had already folded — `startCompact` (`internal/tui/worker.go`) now
  classifies the outcome from `Compact`'s returned error (`context.Canceled`), not a
  post-hoc `ctx.Err()` read, so a committed fold reports as compacted;
  (b) a no-op compaction (conversation too small to fold — the reducer's
  `Result.Skipped`) printed "context compacted" and hid the gauge — `Agent.Compact`
  now returns the skip signal through the `Engine` seam and the TUI says "nothing to
  compact" and leaves the gauge untouched;
  (c) `/clear` left the gauge and tok/s readout lit from the discarded session —
  `ClearContext` now zeroes `ctxUsed`/`tokPerSec` like a fold does;
  (d) a cancelled or faulted stream emits no terminal `UsageEvent`, so the
  generation clock survived into the next turn and mistimed its tok/s — `finishWorker`
  now clears `genStart` on every terminal message.

- **A loop fault no longer risks re-wedging the engine.** The `errMsg` handler
  (`internal/tui/model.go`) now calls `AbortExchange` before returning to the errored
  state, mirroring the `cancelledMsg` recovery: if a `Step` ever faults mid-Exchange
  the interrupted Exchange is discarded so the next `/clear` or message is accepted
  rather than refused with `ErrInputPending`. A latent fix — `Step` surfaces faults as
  an `ErrorEvent` at a boundary today — but it closes the error flavour of the post-Esc
  un-wedge. The `/compact` failure/cancel spine (both `startCompact` outcomes and the
  reducer's overflow/cancel/silence faults) is now covered by tests.

- **`/compact` now survives high context fill.** The reducer sent the *entire* rendered
  transcript as one summary request, so near `n_ctx − compactMaxTokens` the summary call
  itself overflowed (`DeltaContextOverflow`) — compaction deterministically failed at exactly
  the fill it exists to relieve, leaving `/clear` as the only recovery. `internal/context.Compact`
  now bounds the rendered transcript to a character budget derived from the discovered context
  window: it keeps the protected prefix and a budgeted tail of the most recent messages (the
  latest is always kept) and elides the middle with a `[... N earlier message(s) omitted ...]`
  marker, so the summary call stays within the window. The budget is computed in
  `Agent.compactTranscriptChars` from `Context.MaxContextTokens` (now threaded from upstream
  discovery in `cmd/apogee/wire.go`) minus the response reserve and prompt overhead; it is 0
  (render everything, as before) when the window is unknown, since there is no safe basis to
  bound. The overflow test flips from "errors cleanly" to "succeeds via the budget"; the
  unbudgetable case (no discovered window, or a server that rejects even a minimal prompt) still
  surfaces the fault cleanly with the conversation untouched. This makes on-demand `/compact`
  robust; the automatic compaction trigger (which fires *at* high fill by definition) is still
  parked in `TODO.md`.

- **Mouse selection and bracketed paste now handle the prompt correctly.** Two input
  fixes on shipped TUI behaviour:
  (a) a click or drag on a prompt row with wide glyphs (CJK, emoji) landed the caret on
  the wrong rune — `caretTo` (`internal/tui/mouse.go`) fed a display-**cell** column into
  the textarea's rune-indexed `SetCursorColumn` (clamped by cell width, not rune count),
  so a drag-copy could put **different text on the clipboard than was highlighted**. It
  now converts the cell column to a rune offset by walking the visual sub-line's runes and
  accumulating `runewidth` (the same width the widget's own cursor math uses), clamped by
  rune count;
  **[Correction, 2026-07-31]** the parenthetical is wrong and the fix was therefore partial:
  the textarea measures whole **grapheme clusters** with `uniseg.StringWidth`, not runes with
  `runewidth`, so a cluster carrying VARIATION SELECTOR-16 (`⚠️`) still left the caret one glyph
  past the pointer. Corrected under Unreleased → Fixed, "Emoji no longer shift the chat out from
  under the scroll bar, the pointer or the caret"; the rule is in
  [ADR 0030](docs/adr/0030-the-tui-has-one-width-authority-and-it-mirrors-the-painter.md);
  (b) bracketed paste (default-on in bubbletea v2) fell into `Update`'s `default:` case,
  so the textarea inserted the text but skipped the post-edit refresh — a multi-line paste
  rendered unwrapped until the next keypress, the autocomplete overlay went stale, and a
  live drag-selection's cached offsets no longer matched the value (a later copy took the
  wrong runes). A new `tea.PasteMsg` case (`internal/tui/model.go`) mirrors the keypress
  edit path: it clears the selection, inserts, recomputes autocomplete, and re-lays out;
  a paste while a worker runs is dropped, as keystrokes are.

- **A sub-agent now sees a mid-delegation mode tightening (ADR 0013).** `newChildAgent`
  froze the parent's mode at spawn, so a Shift+Tab from Auto down to Plan while a sub-agent
  ran (many Turns on a small model) flipped the footer but left the child auto-approving
  writes until its Exchange ended — a tighten-direction ADR-0005 violation. The orchestrator
  now injects a tighten-only view of the parent's live mode into the child (`Agent.liveMode`,
  the parent's `modeMu`-guarded `Mode` accessor captured as a closure — never the shared field
  or mutex). The child's disposition (`effectiveMode`) takes `TighterMode(parentLive,
  spawnMode)` — a new ladder-index helper in `internal/domain/config.go` where Plan <
  Ask-Before < Allow-Edits < Auto — so a parent tightening below the child's spawn mode
  gates/refuses the child's next call, while a parent loosening can never loosen a child
  spawned tighter (loosening mid-flight stays impossible). A top-level agent (nil view)
  behaves exactly as before.

- **Cleanup batch — leaked cancels, bounded untrusted reads, escape hardening, quit race,
  dead code.** A sweep of small hardening fixes on shipped behaviour:
  - *Leaked cancels.* `finishWorker` (`internal/tui/model.go`) nil'd the worker's `CancelFunc`
    without calling it, leaking one cancellable child context (and its timer resources) per
    completed exchange for the session. It now cancels before clearing.
  - *Bounded reads of untrusted files.* Skills discovery read `SKILL.md` unbounded at startup
    (`.apogee/skills` is always scanned — a hostile-repo OOM), and the `@file` 10 MB cap was
    checked only *after* `SafeReadFile` had already slurped the whole file. Both now bound
    before materializing — skills via an `io.LimitReader` (1 MiB/file) plus a global skill-count
    cap, `@file` via a new `security.SafeStat` fenced size check — mirroring the read_file tool.
  - *Terminal-escape hardening.* Untrusted model text and skill display names are now
    escape-stripped at the transcript boundary (`internal/tui/transcript.go`), so a
    model- or `SKILL.md`-supplied `\x1b]52;…` (OSC 52 clipboard write) or CSI payload can never
    reach the terminal. Not exploitable in the current layout (verified empirically at review),
    but this closes it at the source rather than relying on the cellbuf and footer ordering.
  - *Quit-while-busy teardown race.* `quit()` returned `tea.Quit` without joining the in-flight
    worker, so `runRoot`'s deferred `Close()` teardown could race a worker still inside `Step`
    (benign while `Close` is a no-op, a use-after-close the moment it gains real teardown). The
    exit is now deferred until the worker's single terminal Msg arrives.
  - *Dead code.* Removed the zero-caller `Engine.Mode()` seam method, the unused `fitLeftRight`
    footer helper, and the standalone `workspaceFiles` walk plus its unreachable `m.files == nil`
    autocomplete fallback (`newModel` always installs the cache). The three skill-chip
    render/ID-resolution copies were merged onto one `renderSkillChip` renderer and the shared
    `skillDisplayNames` resolver.
  - *Test gaps.* Added coverage for the loop's `UsageEvent` emission hop (Delta.Usage → event
    fields/Depth, and no event when Usage is nil), the combined skills→files→text injection
    order in one Submit, the `@file` oversize refusal, the escape-strip boundary, and the
    bounded skill-file read.

### TUI

- **Context-fill gauge restyled** to match `llama-launcher`: a solid two-tone strip —
  full blocks for the filled cells, an eighth-block partial cell (`▏▎▍▌▋▊▉`) for
  sub-cell granularity, and a solid dark-gray track behind the remainder — replacing
  the old `█░` dotted bar. Periwinkle fill, a min-sliver floor so any nonzero usage
  shows at least `▏`, and a clamp at the window limit. Bar width is now 10 cells (was
  6). The status line composes the gauge raw rather than re-wrapping it in a
  background style, so the bar keeps its own per-cell backgrounds.

### Skills system + `/skill` (apogee-code feature-parity)

- **`internal/skills` package** discovers user-authored skills — a folder
  containing a `SKILL.md` (YAML frontmatter `id`|`name`, `displayName`,
  `summary`|`description`, plus a Markdown body; a no-frontmatter fallback sniffs
  the first lines) — from three layered dirs: `~/.apogee/skills`, the workspace's
  `.apogee/skills`, and (when `use-project-skills` is on) the workspace's bare
  `skills/`. Later source wins on an ID collision. Each dir is walked through
  `os.OpenRoot` so a symlink can't escape it; a missing dir is skipped and a
  malformed skill is skipped with a soft error (one bad file never blanks the
  catalog). No builtin/embedded skills and no auto-created `~/.apogee/skills` ship
  in v1 (additive future hooks).
- **`/skill` in the TUI** — the `/` menu offers `/skill`, which chains into a skill
  picker; a pick pops a chip above the input, and submit attaches the chosen IDs.
  An empty message with skills attached is a valid send. `/skill` is deliberately
  **not** a parser command (attachment is the only way it acts), so an unknown
  `/skill foo` is still sent as an ordinary message. `/clear` and `/compact` drop
  staged chips; `/continue` carries them.
- **Attached skills now resolve** (replaces the `SkillIDs` "reserved/ignored"
  stub): the loop maps each `UserInput.SkillIDs` entry through `Config.Skills` and
  prepends its body to the user message for that one Turn (order: skills → `@file`
  blocks → user text). An unknown ID, or any ID with no resolver wired, is reported
  via an `ErrorEvent` and dropped — never silently ignored.

### Configuration

- **`use-project-skills`** (config-file only, default **true**) gates discovery of
  the workspace's bare `skills/` folder (the global library and the project's
  `.apogee/skills` are always loaded). Documented in the seeded `config.yaml`.

### Chat input mini-language (core)

- **Parse/route layer** between the TUI input box and the agent: `/`-prefixed
  lines route to local command handlers, `@file` tokens are extracted as
  references, and an autocomplete overlay (commands + workspace files, the latter
  via a bounded `os.Root` walk) mirrors the approval-prompt overlay.
- **Commands**: `/clear` (drop the model's context, keep the visible transcript),
  `/continue` ("Please continue"), and `/compact` (generative compaction — the command
  surface and the `Agent.Compact` seam landed here; the reducer that folds the history
  through them shipped in the same track, see the "Context compaction (`/compact`)"
  section above).
- **`@file` references now resolve** (behaviour change): the loop reads each
  `UserInput.FileRefs` entry within the workspace fence (`security.SafeReadFile`,
  `os.Root`-pinned) and injects its content into the user message — replacing the
  prior "refs ignored" `ErrorEvent`. A missing, oversized, or escaping ref is
  reported and skipped; the Turn still proceeds.

### Public API (additive — minor)

- `Agent.ClearContext() error` — drop the conversation history at a quiescent
  boundary (the host's transcript is unaffected); refused mid-Exchange.
- `Agent.Compact(context.Context) (skipped bool, err error)` — on-demand generative
  Compaction: summarizes the conversation and folds the history at a quiescent boundary
  (refused mid-Exchange with `ErrInputPending`, like `ClearContext`). `skipped` is true
  when the conversation was too small past the protected prefix to fold — no upstream
  call, history untouched; always false on error.
- `UserInput.SkillIDs []string` — the skills attached in chat; the loop resolves
  each through `Config.Skills` and prepends its body to the Turn (was reserved).
- `Config.Skills SkillResolver` — host-supplied resolver for attached skill IDs
  (nil ⇒ attached IDs are reported and dropped). `SkillResolver` and its return
  type `ResolvedSkill` are re-exported on the root facade; the disk-backed catalog
  stays internal (`internal/skills`).

## [1.0.0] — 2026-06-25

The first stable release. `v1.0.0` cuts the public Go API after Phase 3 brought
the agent to feature-parity with apogee-code's non-UI behaviour, with **Auto
mode confined** on Linux (landlock) and macOS (seatbelt). Every consumer — the
TUI, the bench, and the embeddable library surface — has exercised the API, so
semver now begins (ADR 0001 §18, amended).

The public surface is the root `apogee` package: `Agent` (`New`/`Resume`),
`Config` and its host delegates (`EventSink`, `Approver`, `Asker`,
`ExternalEffects`), the four-rung `Mode` ladder, the `Tool`/`ToolRegistry`
extension point with the `ReadOnlyTool`/`ExternalEffectTool` markers, the
`Event` variants, and the hook points. Tools live behind the registry (an open
extension point, ADR 0002), not as root types.

### Confinement (Auto mode is real)

- **Blast-radius confinement model** (ADR 0012, supersedes ADR 0004): a tool
  call runs without a human gate only if its blast radius is bounded — by **OS
  confinement** for the unbounded subprocess/network surface, or by Apogee's own
  **path-safety-to-workspace** for its own in-process writes. Confinement
  attaches to blast radius, at a single **subprocess granularity** on every OS
  (no in-process per-thread landlock, no thread-discard).
- **Four-rung autonomy ladder**: Plan → Ask-Before → **Allow-Edits** → Auto.
  The new `ModeAllowEdits` rung auto-approves Apogee's own workspace-scoped
  writes (no confinement needed; identical on every OS) and gates everything
  else.
- **Linux landlock backend** (`//go:build linux`): ABI probed at startup; an
  honest capability matrix (`FSWrite` at ABI ≥1 / kernel ≥5.13, `NetworkEgress`
  at ABI ≥4 / kernel ≥6.7); a confined subprocess applies the landlock domain
  after fork, before `execve`, so the child is fenced and the parent stays
  unrestricted. Raw `golang.org/x/sys/unix` syscalls (now a direct dependency).
- **macOS seatbelt backend** (`//go:build darwin`): a `sandbox-exec` profile
  generated from the `ConfinementBox` (workspace-write-only + network-open by
  default), presence-probed, no new Go dependency.
- **`Confine(ctx, box, *exec.Cmd)`** prepare-in-place contract: the tool builds
  an idiomatic `*exec.Cmd`; the backend rewrites it to launch confined. The
  `confine-to-workspace` global-config key (default `true`) tunes Auto's blast
  radius; `confine-to-workspace=false` is the explicit "I am the sandbox"
  (VM-only) opt-out. `AutoEligible()` requires filesystem confinement only;
  where confinement is unavailable, subprocess tools gate through Approval
  ("confine if you can, gate if you can't") rather than refusing Auto.

### Tools (feature-parity with apogee-code's non-UI surface)

- **File-editing family**: find-replace (single + multi), `edit`/apply-edit,
  `diff`, `open-file` — pure-Go, stateless, carrying the unexported
  `workspaceScopedWriter` marker so Allow-Edits/Auto bound them by path-safety.
- **Execution tools**: `terminal` and `python-exec` — one-shot, stateless, the
  first `Confiner` consumers; process-group teardown on cancel
  (`Setpgid` + `cmd.Cancel` + `WaitDelay`).
- **`git` tool**: branch / commit / diff-range over the system `git`, detected
  and graceful-degrading when absent.
- **`diagnostics` tool**: in-process `go/parser` + optional `go vet`,
  read-only, graceful when the toolchain is absent.
- **Network + host tools**: `web_fetch`, `http_request`, `web_search`
  (external-effect, Approval-gated as MCP-kind / auto-run url-filtered as
  network-kind per the disposition table) and `ask_user` (the new `Asker` host
  delegate). These are routed through the `ExternalEffects.Do` boundary
  (ADR 0008) so the bench can stub them.
- The existing `read_file` / `write_file` / `list_dir` / `grep` built-ins carry
  forward; `write_file` carries the workspace-scoped-writer marker.

### Processing (parity-complete port)

- **All apogee-code tool-call formats parse**: native/JSON `tool_calls`,
  markdown-fenced, and custom-regex, each gated by **ported TS test vectors**.
- **Full harmony / thinking-channel set** handled, with a `processor-factory`
  that selects the format per model/response. The package stays `domain`-only.

### Security guardrails (the human-in-the-loop layer)

- **`internal/security`** consolidates the Phase-1 per-tool path-safety into one
  reusable guard and adds **url-safety**, an **arg-guard**, a **circuit-breaker**
  (halts a runaway tool-loop), and an **audit record** (bounded ring buffer with
  a dropped-count). These run in all modes and a sub-agent inherits them.
- **Two-tier dangerous-action guard** (a footgun-guard, NOT a security
  boundary): a hard-refuse tier (`rm -rf` of root/home/system, fork bombs,
  `~/.ssh`/credential/persistence writes) and a force-approval tier
  (`curl | bash`-class). It runs first and is **tighten-only**; project config
  may only add rules, never dissolve a floor rule by ID.
- **Default-on SSRF floor** for the network tools: loopback / private ranges /
  IMDS `169.254.169.254` / link-local / CGNAT / `0.0.0.0` / NAT64 denied by
  **resolved IP** (pre-flight and at dial time, closing DNS-rebinding),
  tighten-only.

### Sub-agents

- **Sub-agent orchestrator** (ADR 0013): a sub-agent is the embeddable `Agent`,
  constructed through an internal orchestrator that threads the parent's `Mode`,
  `Approver`, `Confiner`, and guardrails verbatim (or stricter) with a tool
  **`Subset` ≤ the parent's** (ADR 0005). It is exposed as a
  dispatch-transparent **`sub_agent`** recursion point — never confined or gated
  as a unit; each child tool call gets the full per-call disposition one level
  down.
- **Isolated live guard state** (`Guards.ForSubAgent`): a sub-agent gets a fresh
  circuit-breaker and audit log over a shared read-only dangerous ruleset.
- Nested events re-emit into the parent stream at **`Depth = parent.Depth + 1`**.
- Stepping is **top-level-only for v1** behind a swappable driver; a sub-agent
  runs atomically within the parent Turn (no mid-sub-agent snapshot; cancel
  rolls back to the parent's pre-`sub_agent` boundary).

### MCP

- **MCP client** on the official Go SDK (`modelcontextprotocol/go-sdk` v1.6.1):
  stdio / SSE / streamable-http transports. Server tools surface into the
  registry as `ExternalEffectTool` of kind `mcp`, so they **Approval-gate in
  Auto** under `confine-to-workspace=true` (an external server Apogee cannot
  fence). **Resume reconnects fresh** — no server-side-state promise (ADR 0008).

### TUI

- **Nested-event rendering**: `Depth > 0` sub-agent events render as a framed,
  labelled block (Phase-2's "tolerate" → "render").

### Notes

- Cross-build stays green on all 6 targets (linux/darwin/windows ×
  amd64/arm64, `CGO_ENABLED=0`); OS-specific confinement is build-tagged behind
  the `denyConfiner` (Windows/other) fallback. **Windows confinement is Phase 5**
  — Auto is simply unavailable on Windows until then.
- The `internal/` packages never import the root module path (ADR 0010).
- Direct dependency additions this release: `golang.org/x/sys` (landlock),
  `github.com/google/shlex` (terminal command splitting),
  `github.com/modelcontextprotocol/go-sdk` (MCP client).

### Known post-release verification (owner-run / CI)

These confinement **enforcement** proofs cannot run in the development
environment and are deferred to an owner-run / CI verification after the tag.
They are not acceptance failures — the hermetic disposition/logic tests (caps
honesty, generated profile strings, command rewriting, fail-closed paths) run
on every host and pass, and the live escape-probe batteries **self-skip loudly**
where the OS cannot enforce:

- **Linux landlock live enforcement** — ✅ **confirmed on a landlock-enabled
  kernel (2026-07-23).** Ran on an Ubuntu devbox, kernel **7.0.0-28-generic**
  aarch64 with `landlock` live in `/sys/kernel/security/lsm`; `apogee probe`
  reports `backend: landlock (fs-write: available · network: available)`, so
  `confinetest.Probe` runs live instead of self-skipping. Under a full
  `make check` (race detector on, cgo enabled) the landlock-tagged battery
  passes live: a confined subprocess's out-of-workspace and `~/`-profile writes
  are OS-denied, a non-allowlisted connect is denied while network-open connects,
  the domain is inherited across `exec`, and the parent stays unrestricted. The
  earlier caveat (dev-host kernel had `CONFIG_SECURITY_LANDLOCK` off) no longer
  applies to this box.
- **macOS seatbelt live enforcement** — ✅ **confirmed on macOS hardware
  (2026-07-02).** `confinetest.Probe` now runs under live `sandbox-exec` on a real
  Mac: a confined subprocess is fenced to the workspace, out-of-box and `~/.ssh`
  writes are OS-denied, the parent stays unrestricted, and network-deny tightens
  while network-open connects. (This surfaced and fixed the box-root canonicalization
  bug below.) The Linux landlock arm above is now closed too (2026-07-23).
- **Live Auto-confined deliverable run** — the opt-in `APOGEE_LIVE_ENDPOINT`
  end-to-end run (a real coding conversation in Auto, a shell write outside the
  workspace OS-denied, an MCP tool still raising Approval, a sub-agent delegated
  and its nested work rendered) is owner-run on Linux (landlock) and macOS
  (seatbelt). **Linux (landlock) arm ✅ confirmed (2026-07-23)** on an Ubuntu
  devbox (kernel 7.0.0-28-generic aarch64, landlock backend) against a real
  gemma-4-E4B endpoint: in `--mode auto`, step-1 out-of-workspace write
  (`echo … > ~/apogee-escape-test.txt`) was OS-denied with **no** approval prompt
  while the in-workspace write succeeded, the `demo__ping` MCP tool **still raised
  Approval**, and a delegated sub-agent's nested `NOTES.md` write **rendered** in
  the transcript; afterwards `~/apogee-escape-test.txt` was confirmed absent.
  macOS (seatbelt) arm still open.
- **Box-root canonicalization** — ✅ **resolved (2026-07-02).** Was a real bug, not
  just a verification gap: seatbelt embedded box roots verbatim and denied every
  in-workspace write when the root passed through a symlink (macOS `/var`, `/tmp`).
  Fixed by resolving each writable root through symlinks in `seatbeltProfile`; see
  the `[1.1.0]` Fixes entry.

[1.7.0]: https://github.com/airiclenz/apogee/releases/tag/v1.7.0
[1.6.0]: https://github.com/airiclenz/apogee/releases/tag/v1.6.0
[1.5.0]: https://github.com/airiclenz/apogee/releases/tag/v1.5.0
[1.4.0]: https://github.com/airiclenz/apogee/releases/tag/v1.4.0
[1.3.0]: https://github.com/airiclenz/apogee/releases/tag/v1.3.0
[1.2.0]: https://github.com/airiclenz/apogee/releases/tag/v1.2.0
[1.1.0]: https://github.com/airiclenz/apogee/releases/tag/v1.1.0
[1.0.0]: https://github.com/airiclenz/apogee/releases/tag/v1.0.0
