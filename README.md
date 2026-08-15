<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="graphics/apogee-logo-light.svg">
    <source media="(prefers-color-scheme: light)" srcset="graphics/apogee-logo-dark.svg">
    <img src="graphics/apogee-logo-light.svg" alt="Apogee" width="200">
  </picture>
</p>

A **terminal coding agent**, built with **local models in mind**: <br>
capable with frontier models, engineered so even small, locally-run LLMs (~4B–35B) deliver.

<p align="center">
  <img src="graphics/demo.gif" alt="Apogee finding and fixing a failing Go test against a local model, with a follow-up instruction queued mid-run and delivered at the next tool boundary">
</p>

Apogee is a single, cross-platform tool that drops into any IDE's integrated 
terminal — or any standalone terminal — on Windows, macOS, and Linux. It runs
against any OpenAI-compatible endpoint: a local LLM server (llama.cpp, Ollama,
LM Studio, vLLM) keeps your code on your machine and needs no API key, and a
keyed one — a remote vLLM, an OpenAI-compatible cloud provider — is one
`api-key` on its `servers:` entry away. Either way you get the full agentic tool-use loop, with
autonomy fenced at the operating-system level rather than on trust.

## What this repo is

Apogee is built on three commitments:

- **A complete agent with a UX that gets out of your way.** The full agentic
  loop — provider abstraction, a 27-tool suite (file ops, grep, git, terminal,
  tests, web, sub-agents, showing you a finished document), an MCP client,
  sessions that survive a crash — inside a terminal UI built with care: type
  your next message while the model streams and queue it into the running task,
  recall any prompt you have sent, collapse what you are done reading, click
  every path it prints.
- **Autonomy you can trust.** Four autonomy modes end in Auto, the unsupervised
  one — and Auto is fenced at the operating-system level on all three platforms
  (Linux landlock, macOS seatbelt, a restricted Windows token), not by a prompt
  asking the model to behave. Where the OS genuinely cannot enforce the fence,
  apogee asks before each command instead of running it unbounded.
- **Small models stay first-class.** Gated mechanisms — context compression,
  tool-call validation + auto-retry, behavioural nudges, a cross-session
  learning *Library* — run *inside* the agent loop, where they have the most
  leverage, and make small, locally-run models measurably better at sustained
  agentic coding. Each fires only when the model needs it, and nothing is
  carried forward on faith: every mechanism is A/B-tested against real local
  models before it earns a place in the loop, and the same eval bench measures
  any change to what a model sees — prompts, compaction, tool wording — so the
  agent's behaviour is regression-tested like its code.

Under the hood, apogee is an **embeddable engine**, and the TUI is its first
front-end, not its identity. The whole loop is a Go library with no wire surface
of its own: the eval bench already drives it in-process, `apogee headless` is the
second front-end over the same core, and the ones still to come — a scheduling
daemon, other surfaces entirely — compose that same engine. That commitment is written down and binding — see
[the north-star decision record](docs/adr/0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md).

Working on this repo *with* a coding agent? [`AGENTS.md`](AGENTS.md) is the
agent-facing map — where the docs live, and the conventions that aren't derivable
from the code.

## Why Go

Portability is the primary goal. Go cross-compiles to a single static binary with
no runtime — the gold standard for "drop into any terminal on any OS." It also lets
us use **one language for both the agent and the bench that evaluates it**. The TUI
is built on the Charm stack (Bubble Tea + Lipgloss + Bubbles) with Cobra for the CLI.

## Status

**`v0.14.x` on `main` — pre-production.** Under SemVer a `0.x` version makes no
API-stability promise — the Go API may still move while the tool hardens. That
says nothing about how you get the tool: every release ships prebuilt binaries
for all six targets and a Homebrew formula installs them — see
[Install](#install). One warning:
`go install github.com/airiclenz/apogee/cmd/apogee@main` works, but **not**
`go install …@latest` — proxy.golang.org immutably retains retired `v1.x` module
versions, so `@latest` resolves to the stale `v1.7.0`.

Functionally the loop is complete: full tool suite, MCP client, sub-agents, skills,
and OS-confined Auto mode on **all three** platforms — Linux landlock, macOS
seatbelt, and a Windows restricted low-integrity token (Windows 10 1809 / build
17763 / Server 2019 and newer). Where the facility is genuinely missing (an older
Windows build, or most containers, where landlock reports `ENOSYS` whatever the
kernel version), Auto asks before each shell/subprocess call instead of running it
unbounded; apogee says so at startup, `apogee probe` answers "what would Auto do on
this box?" without running an agent, and `/confine` is the way out (see
[Auto mode's blast radius](#auto-modes-blast-radius)).

Newest on `main`: **apogee can remember your model choice per server** — with
`remember-model: true`, an explicit `/model` pick is written into that server's
entry and the next session there starts on it; a launcher-fronted server records
its Launch profile instead and reloads it at startup, unless a server is already
running there (ADR 0048). Alongside it: **API keys can live outside the config
file** — `api-key-cmd:` / `api-key-env:` name a source instead of a token, and
at startup apogee offers to move a plaintext key into the machine's secret store
and rewrite the entry itself — plus **delegations on a server of their own**
(`sub-agents: true` routes every sub-agent there), replies bounded so a thinking
model cannot reason its way into the context wall, and a retry that survives a
transient upstream error mid-stream instead of failing the turn. Released just
before those: colour schemes and `/color-scheme`, sub-agents that run in
parallel, the watched `config.yaml`, the
[`/settings`](#the-settings-screen--settings) screen,
[`apogee headless`](#running-one-prompt--apogee-headless), and scheduled prompts
(`/schedule`, each firing saving its own browsable session). The mechanism catalogue
is complete at 21 mechanisms — the predecessor's twenty, all ported, plus
apogee's own `guided_decomposition` — and the first Validated set
(`gemma-4-e4b-it-qat`) ships with the binary; current work is per-model bench
validation of that catalogue alongside TUI layout refinement. See
[`docs/plans/`](docs/plans/) and the [`CHANGELOG`](CHANGELOG.md) for what's
next.

## Install

Three ways in, and all three land the same thing: one static binary with no runtime
to install beside it. Homebrew if you are on macOS or Linux, a prebuilt archive on
any of the six targets, or a clone if you would rather build it yourself.

**Homebrew — macOS and Linux:**

```bash
brew tap airiclenz/tap
brew trust --tap airiclenz/tap   # once per machine, on Homebrew 5.1+
brew install apogee
apogee --version
```

The formula installs the prebuilt binary for your platform, so nothing is compiled
and no Go toolchain is needed; `brew upgrade apogee` moves you to the next release.
The `brew trust` line is what Homebrew 5.1+ wants before it will load a third-party
tap without warning you — it is recorded once per machine, and
`brew untrust --tap airiclenz/tap` revokes it.

**A prebuilt archive — any of the six targets.** Every release carries Linux, macOS
and Windows × `amd64` and `arm64` on the
[releases page](https://github.com/airiclenz/apogee/releases/latest). Each archive
holds the binary, the README and the LICENSE, and a `SHA256SUMS` file sits beside
them so you can check what you downloaded.

```bash
# macOS / Linux — set these two to your release and platform
VERSION=0.12.0
PLATFORM=darwin_arm64   # or darwin_amd64 · linux_amd64 · linux_arm64

curl -fsSLO "https://github.com/airiclenz/apogee/releases/download/v$VERSION/apogee_${VERSION}_${PLATFORM}.tar.gz"
tar -xzf "apogee_${VERSION}_${PLATFORM}.tar.gz"
sudo install -m 0755 "apogee_${VERSION}_${PLATFORM}/apogee" /usr/local/bin/apogee
apogee --version
```

On Windows, download `apogee_<version>_windows_arm64.zip` (or `_amd64`), unpack it,
and put `apogee.exe` somewhere on your `PATH`.

Two notes on the archives, because the binaries are **not code-signed** yet. On
macOS, a download made in a *browser* is quarantined and Gatekeeper will refuse to
run it — `xattr -d com.apple.quarantine ./apogee` clears that, or use the `curl`
above, which never sets it. On Windows, SmartScreen may warn about an unrecognised
publisher for the same reason. Signing both is a recorded follow-on (`ISSUES.md`);
until then `SHA256SUMS` is the check that is actually worth making.

**From source:** a clone plus `make build` — see
[Building from source](#building-from-source) for the prerequisites and the
`Makefile` targets.

## Key capabilities

- **Local-first, cloud-capable** — any OpenAI-compatible endpoint: a local server
  keeps every byte on your machine and needs no API key; a keyed one — a remote
  vLLM, an OpenAI-compatible cloud provider — is one `api-key` on its
  `servers:` entry away.
- **Agentic tool use** — multi-step loop with file edits, shell, search, git, web,
  and sub-agents.
- **Four autonomy modes** — Plan (read-only), Ask-Before (writes need approval),
  Allow-Edits (workspace-scoped writes auto-approved), Auto (autonomous, confined
  at the OS level via Linux landlock / macOS seatbelt / a Windows low-integrity
  token; where the OS cannot fence a command, Auto asks before it rather than
  running it unbounded).
- **Type — and select — while it works** — the prompt box stays live during a run: write
  your next message while the model streams, and press `⏎` to *queue* it. A queued message
  is delivered **into the running task** at the next tool boundary, so a remark like "also
  check the tests" lands while there are still turns left to act on it; anything still
  queued when the task finishes goes out as the next message. `esc` stops everything and
  holds the queue. `↑` on an empty box walks back through what you have already sent in this
  workspace — newest first, `↓` forward again, one `↓` past the newest to get the blank box
  back — so a long prompt is re-sent or edited rather than retyped; the recall list is per
  workspace and lives in `~/.apogee/prompts/`. Transcript text stays selectable in every
  state, mid-stream included.
- **Colours you choose** — the palette is a **colour scheme**: one YAML file of 29 semantic roles.
  `dark` and `light` ship in the binary, `ui.color-scheme` picks one, `/settings` offers a picker
  that applies live, and your own `~/.apogee/schemes/<name>.yaml` shadows a built-in of the same
  name — start from `/color-scheme export dark` and edit. A defective file costs colour, never the
  session.
- **Deliverables you actually see** — `present_document` ends a report-producing task
  by showing the file: opened on your desktop when apogee runs locally, served over a
  one-off link when it runs on a remote box, and always printed as a clickable path
  in the transcript. See [Showing a finished document](#showing-a-finished-document).
- **Sessions that survive anything** — every completed turn autosaves to
  `~/.apogee/sessions/`, so a crash loses at most the turn in flight. `apogee
  --continue` reopens this workspace's latest session, `/sessions` browses them all,
  and a resumed session repaints its full scrollback; an interrupted task picks up
  where it stopped with `/continue`. See [Sessions](#sessions).
- **Scheduled prompts** — `/schedule` runs a prompt on a cycle for as long as apogee
  is open: each firing runs headless in Plan or Auto mode and saves its own session,
  browsable like any other (tagged `⟳` in `/sessions`); `/schedule-stop` takes one
  off the clock.
- **One prompt, no UI** — `apogee headless "…"` (or a prompt on stdin) runs a single
  prompt to completion unattended, prints the answer to stdout and exits `0`/`1`/`2` so a
  script can tell a bad outcome from a bad invocation. See
  [Running one prompt](#running-one-prompt--apogee-headless).
- **Diagnosable without running an agent** — `apogee probe` reports what this host can
  do (backend, capability matrix, Auto verdict, roots, endpoint reachability) for free
  and offline; `apogee probe model` runs a capability battery against the model. See
  [Diagnosing a host](#diagnosing-a-host--apogee-probe).
- **MCP support** — connect external tool servers over stdio / SSE / streamable-http.
- **Model profiles** — adapt to models that don't speak native tool-calls: the tool
  menu and format instructions are injected as text on the request side, markdown-fenced
  or custom-regex tool calls are parsed back out of the reply, and inline thinking /
  harmony channels are stripped — all driven by a per-model profile (native models
  stay byte-identical on the wire).
- **Small-model mechanisms** — context compaction is built in; tool-call
  validation/auto-retry, syntax + autofix, behavioural nudges, and the cross-session
  Library are all catalogued — each default-off, gated so it only fires when the
  model needs it, and enabled per model via Validated sets backed by bench evidence.
- **Validated, not assumed** — every mechanism is A/B-tested against real local models
  via an eval/simulation bench (which imports Apogee as a Go library and drives
  the real loop in-process) before it earns a place in the loop — and the same
  bench guards every change to prompts, compaction, and tool wording against
  regression.

## In-chat commands, skills, and file references

Typing `/` in the prompt opens **one menu of commands and skills**; `@` completes a
workspace file path, and an `@path` in a message hands that file to the model. A path
containing spaces is written quoted — `@"docs/my plan.md"` (single quotes are accepted
too) — and the autocomplete keeps completing across the spaces and inserts the quotes
for you.

Both work **anywhere in the line and at any time**: the menu completes the token your
cursor is on, so you can start typing a message and reach for a command halfway
through, or go back and fix a misspelled name. Accepting a command from the menu
**runs it and keeps the rest of your draft** — unless the command takes arguments, in
which case the menu completes it to `/command ` and waits for you to type them; `/model`
and `/server` are the exception to that exception and run straight away, since bare
they only open a picker. The menu stays open while the model is working, too —
commands that need a quiet engine wear an `— idle only` tag for as long as the engine
is busy, and say so if you pick one anyway, while `/version`, `/skills`, `/usage`,
`/effort`, `/schedule`, `/schedule-stop` and `/confine`'s status report answer
immediately. Once the engine is idle that tag is gone from the menu entirely — there
is nothing left for it to warn about. A token
lights up in the box exactly when it resolves — the `skill` role for a skill your catalog
has, the `file-ref` role for a file your workspace has (violet and green under `dark`) — so
a typo is visible before you send.

| What you type | Does | While the model works |
|---|---|---|
| `/<skill-id>` | Invoke a skill — type its id anywhere in your message | ✅ rides the queued message |
| `@<path>` | Hand a workspace file to the model | ✅ rides the queued message |
| `/skills` | List the discovered skills — id, name, summary and where each came from | ✅ |
| `/version` | Show the apogee version | ✅ |
| `/usage` | What this session has spent — one row for the main agent, one per sub-agent, and a session total | ✅ |
| `/confine` | Report or change Auto's blast radius — see [below](#auto-modes-blast-radius) | ✅ report only |
| `/effort` | Set how hard the model thinks this session — `off`, `low`, `medium`, `high`, or `auto` (back to the profile); bare reports what resolved — see [below](#configuration) | ✅ |
| `/schedule` | Run a prompt on a cycle — bare lists what is live, `/schedule <prompt>` asks for the cycle and mode, `/schedule <cycle> [auto] <prompt>` creates one outright | ✅ |
| `/schedule-stop` | Take a schedule off the clock — the only one straight away, a picker when several are live | ✅ |
| `/clear` (or `/new`) | Close this session into history and start a fresh one | — |
| `/compact` | Summarise the conversation to reclaim context | — |
| `/continue` | Ask the model to keep going | — |
| `/sessions` | Browse saved sessions — resume, rename, or delete | — |
| `/rename` | Rename this session — `/rename <name>` sets it, bare `/rename` asks the model for one | — |
| `/model` | Switch model — the Launch profiles [llama-launcher](#local-servers--llama-launcher) defines when one is configured, what this server serves when not; picker, or `/model <name>` | — |
| `/server` | Move this session to another server you configured — picker, or `/server <name>` | — |
| `/unload-model` | Free the model of the server this session is on — see [below](#local-servers--llama-launcher) | — |
| `/stop-server` | Stop the server this session is on — see [below](#local-servers--llama-launcher) | — |
| `/color-scheme` | Recolour the screen — bare lists what you can switch to, `/color-scheme <name>` switches and saves, `/color-scheme export <name>` writes an editable copy of a built-in to `~/.apogee/schemes/` | — |
| `/settings` | Browse and change every setting, live — see [below](#the-settings-screen--settings) | — |

A lone `/word` that names neither a command nor a skill is **not** sent to the model:
apogee says `unknown command or skill: /…` and leaves your line in the box to fix.
Anywhere else in a message a slash is just text, so paths like `/usr/bin` travel
untouched.

The keys are few, and the empty prompt box advertises them: `⏎` sends — *queues*, while
the model works — `⇧⏎`/`⌥⏎` opens a new line, `↑`/`↓` walk back and forward through the
prompts you have already sent in this workspace, `esc` stops a run, `⌃c` quits. The box
advertises `⇧⏎` only on terminals that negotiated the enhanced (kitty) keyboard
protocol — the thing that makes that chord arrive as anything other than a plain `⏎`;
everywhere else the legend names `⌥⏎` alone, which works on every terminal. Beyond
the box, `⇧⇥` cycles the autonomy mode — Plan → Ask-Before → Allow-Edits → Auto — at
any time, mid-run included, and `PgUp`/`PgDn` scroll the transcript. `⌥↑`/`⌥↓` light a
bar on the transcript and hand the arrows to it: `↑`/`↓` walk from one foldable block to
the next — a tool call, a group member, a type row — `⏎` opens or closes the one under
the bar, and `esc`, or simply typing your next message, gives the keys back. `⌃l` is the
readline redraw: it forces a full repaint, which is the way back from a terminal that
has smeared or eaten part of the frame. It sends nothing, edits nothing and interrupts
nothing — the only thing it takes with it is a mouse drag-selection's highlight, which
every keypress drops.

### The settings screen — `/settings`

`/settings` opens a **full-height pane** over your whole configuration: one row per setting,
in the order the starter `config.yaml` documents them and grouped under section headings,
each row showing the value **this run resolved** for it. Where a higher-precedence source
beat the file, the row says which — `(env)` or `(flag)` — so a key that reads one way in the
file and another on screen explains itself. The conversation gives way
entirely while the pane is up, because thirty-odd keys are a screen to read rather
than a choice to scan: `↑/↓` move the `❯`, a fixed two-line `Description:` header above the
list says what the key under the cursor is for, and `esc` closes the pane and hands the
transcript back. Section labels stand in white above the rows they open, the row being typed
into is lit, and the mouse works where the keys do — a click selects a row, the wheel walks
the list one row per notch. It needs a quiet engine, so it is **idle only**.

**Editing writes one key, when you ask.** `⏎` on a true/false row toggles it, `⏎` on a row
with a fixed set of values — `mode:`, `server:` — opens that list to pick from, `⏎` on a
string or a number opens a buffer on the row itself, and `⏎` on the inline system prompt
opens a multi-line field over the list, where `⏎` makes a new line, `ctrl+s` saves and `esc`
discards. A buffer is a real field: the arrow keys, `home`/`end` and word jumps move the
caret, and the mouse seats it and drags a selection exactly as it does in the prompt box. Each
committed edit is spliced straight into `~/.apogee/config.yaml` — your
comments, your layout and every other key untouched, the result re-parsed and compared
against the original before it replaces the file — and a key that was still one of the
commented examples lands directly below it. A value the key cannot hold is refused before
anything is written, with the reason on the row and your text still in the buffer. Nothing
else is ever written: apogee still makes no edit to that file you did not ask for.

**And what is saved is applied — to the session you are in.** The `⏎` that persists a key
also puts it into effect, so no setting waits for a restart: change `mode:`, `bypass:`, a
mechanism switch, the web-search endpoint, the presentation keys or the model profile and the
next thing apogee does uses it. The row keeps a ` *` after its value — `false *` — which says
*you changed this here, this session*; it is cleared only by a relaunch. One pair lands at a
boundary the session crosses anyway rather than mid-conversation, and says so on the row: the
`context-files:` keys are part of the prefix every request is cached against, so they take
effect at the next `/clear` — `· applies at next clear`. On a key an environment variable or
a flag is overriding, the edit still applies and is still written, and the row adds that the
override will win again the next time apogee starts — startup precedence is unchanged. If a
write lands but the live apply refuses it, the row says exactly that
(`saved — live apply failed: …`) rather than leaving you to guess which half happened.

**`backspace` unsets.** On a row you have set, `backspace` arms a reset, the hint line asks
for a confirming `⏎`, and what that sends **removes the key's line** from the file rather than
writing today's default into it — so the key goes back to following the built-in default
instead of being pinned to a copy of it. The default is applied on the same keypress, and the
row reports it with the same marker: `default *`.

**The blocks no row can hold open your editor.** `servers:`, `mcp-servers:`,
`validated-sets: alias:`, `system-prompt-models:` and the model profile render as a summary with an
`· ⏎ opens $EDITOR` pointer, and that is what `⏎` does — in the editor the
[four-rung ladder](#configuration) `editor:` heads, with the cursor on that key's line where the
editor takes a line argument. A **terminal** editor (`vi`, `vim`, `nvim`, `nano`, `pico`,
`emacs`, `micro`, `hx`, `kak`) has to own the terminal, so apogee suspends into it and re-reads
the file when it exits; a non-zero exit (`:cq`) discards that re-read. Anything else — a GUI
editor, your desktop's opener — is started **detached**: the pane stays up, nothing waits on the
window that opened somewhere else, and the row says `· opened in your editor`. Either way the
edit lands the same way, because what applies it is the file being **saved**, not the editor
exiting. Every key that changed is applied the way an in-pane edit is — a changed `mcp-servers:`
**reconnects**, connecting the new set first and swapping the tools over only once it is up, so a
server that will not come back leaves the old connections serving and the reason on the row; a
changed `model-profiles:` swaps the parser. The jump is offered between runs only — mid-run the
row asks you to wait, while in-pane edits stay open. The confinement keys are the one pair that
goes nowhere near it: they carry `· use /confine`, because switching Auto's fence off asks for an
acknowledgement that belongs with [that verb](#auto-modes-blast-radius). And the `server:` row
**moves the session** — the same switch `/server` performs, chosen from the same list, recorded
the same way.

**`mechanisms:` is the one block the pane opens itself.** `⏎` on that row opens a list of every
catalogued mechanism with `on`/`off` beside each; `⏎` or `space` flips the highlighted one, writing
and applying it on that keypress, and the list **stays open** so a posture is set in one visit. `esc`
goes back. Switching one off writes `<id>: false` rather than deleting the line, and — as ever — a
non-empty `mechanisms:` block means manual control, so the Validated set measured for the bound model
is no longer applied on top.

## Sessions

Every conversation is a session, saved continuously: after each completed turn the
session is written to `~/.apogee/sessions/` (asynchronously, best-effort), so a
crash or `kill -9` costs at most the turn in flight. A saved session stores the
engine's conversation **and** the TUI scrollback, so resuming repaints the
transcript you actually saw — tool cards included — and relights the context
gauge, instead of opening an empty view over a model that still remembers.

- `apogee --continue` resumes this workspace's most recent session; `--resume`
  takes a session id (from `/sessions`) or a file path.
- `/sessions` opens the in-TUI browser (newest first): typing filters the list,
  `⏎` resumes, `^r` renames inline, `^d` deletes after a confirm, `^a` toggles
  between this workspace and all workspaces. The verbs are chords precisely so
  the letters are free to type with — every selector pop-up filters as you type.
  A new session names itself: on its first prompt apogee asks the
  model, in a single call off to the side of the conversation, for a short title
  (`auto-title:`, a file-only key, on by default). With that off — or when the
  call fails or answers with nothing usable — the title falls back to the first
  user message, or to a dated `Session <date>` when that message is empty or
  opens a code fence. A bare `/rename` later re-reads the session — your opening
  request plus the most recent ones — and names it for what it has become, so
  one that moved on to another task gets named for where it ended up.
- A run of a `/schedule` saves its own session, so it browses like every other:
  the browser tags it `⟳ <schedule>` beside its title, so a run reads as one of a
  series rather than as a session nobody remembers starting. Ordering, resume,
  rename and delete treat it exactly like a session you held yourself.
- `/clear` (or `/new`) closes the current session into history and starts a fresh
  one — neither deletes; discarding is an explicit `^d` in the browser.
- A session killed mid-task resumes to the last completed turn and says so;
  `/continue` then picks the unfinished work back up, while sending a new message
  instead discards it and continues fresh.
- The session's **name is written on the top rule**, the hairline above the status
  line — `▔▔▔▔ the name ▔▔▔▔` — so a screen full of panes says which conversation
  each one is. It shows whatever named the session, from `/rename` or from the automatic
  naming call, and a session with no name yet shows a plain unbroken rule. Nothing
  needs configuring in your terminal for it: it is a row apogee paints itself.

Autonomy mode, tool approvals, confinement, and MCP connections are deliberately
**not** part of a saved session — they are re-established or re-confirmed on
resume, so yesterday's approvals never silently apply to today's run.

## Configuration

Settings resolve by precedence, highest first: a command-line flag overrides an
`APOGEE_*` environment variable, which overrides `~/.apogee/config.yaml`, which
overrides the built-in default. A documented starter `config.yaml` is written to
`~/.apogee` on first run (your edits are never overwritten): every setting is
there as a commented example, with one exception — `system-prompt-text:`, the
default system prompt, ships active. Three keys carry all four layers —
`server:` (`--server`, `APOGEE_SERVER`), `mode:` and `bypass:`. Every other key
is **file-only** (no flag or env): the `servers:` list, the system prompt, the
model profile, MCP servers, the web-search endpoint and the small-model
mechanisms among them. Two raw overrides are not config keys at all — `--endpoint`
/ `APOGEE_ENDPOINT` runs one session against a server the file does not list,
while `APOGEE_API_KEY` and `--model` / `APOGEE_MODEL` carry that server's token
and model hint or overlay those two fields of the listed entry a session starts
on (see [The servers you run models on](#the-servers-you-run-models-on)). The
upstream key has an environment variable but deliberately **no flag** (see
[The upstream API key](#the-upstream-api-key)).

That file is also readable and editable from inside apogee:
[`/settings`](#the-settings-screen--settings) lists every setting with the value this
run resolved for it, and writes a committed edit back as a **single key**, comments and
layout preserved. Apogee writes that file in exactly three places and nowhere else: a
committed edit you asked for, the `server:` line a `/server` switch records for your
next start, and the one-time migration of a config still written in the retired
schema — which copies the file aside first and says so on startup. "Your edits are
never overwritten" stands: nothing is rewritten at upgrade, and no line you wrote is
touched at any other time.

**And that file is watched.** While apogee runs it polls `~/.apogee/config.yaml`, and a save
applies itself to the session you are in — whoever wrote it: the `/settings` pane's `⏎` jump, a
GUI editor you left open in another window, a `vim ~/.apogee/config.yaml` in a second terminal.
No key waits for a restart and nothing has to be re-entered in the pane; every key that came back
different is applied exactly as an in-pane edit is, and its row repaints wearing the same ` *`. A
file that does not parse changes nothing — the session keeps running the settings it had, because
a poll will sooner or later read a half-written save — and only when three saves in a row fail to
parse does apogee say so in the transcript, once, until the file parses again. `server:` is the
one key a re-read never moves: it names where the *next* session starts (see
[The servers you run models on](#the-servers-you-run-models-on)). The watcher is a poll of the
file's timestamp and size on a one-second ticker — no daemon, no filesystem-notification
dependency (ADR 0041).

Catalogued mechanisms are opt-in by canonical ID. Every mechanism ships **off**
until its A/B bench run proves it a win, so enabling one is a deliberate config
choice:

```yaml
# ~/.apogee/config.yaml
mechanisms:
  validate: true   # tool-call validation + auto-retry
  syntax: true     # write-content syntax check
  autofix: true    # formatter pass on tool-call payloads
```

An unknown ID is a startup error that lists the IDs this build knows; `--bypass`
still wins (an enabled non-off-ramp mechanism does not fire under bypass). The same
catalogued mechanisms are enabled by ID from the Go API through
`Config.EnableMechanisms` (with `apogee.CataloguedMechanisms()` to enumerate them), so
a library embedder arms the identical stack without the config file. The
catalogue currently counts **21** mechanisms — see
[`docs/design/mechanism-catalogue.md`](docs/design/mechanism-catalogue.md) for
what each one does.

The **built-in tools** are all on by default, and `tools:` `disabled:` (a file-only key) is how
you take one off the menu — the model is never shown it, and a call naming it is refused as a tool
that does not exist:

```yaml
# ~/.apogee/config.yaml
tools:
  disabled: [view_diff, single_find_and_replace]
```

It exists because a long tool list is itself a cost for a small model: fewer, clearer tools can
beat more of them, and this is the switch that lets you find out on your own work rather than
guess. The names are the ones the model calls a tool by — the same names the transcript shows
while a tool runs — and a name that is not a tool is a startup **notice** rather than an error,
with the rest of the list still applying, so a typo costs you the tool you meant to disable and
nothing else. The key is global (it applies to every model this config runs) and it is live like
every other: save the file, or commit the row in `/settings`, and the next request is built from
the roster that is left. An MCP server's tools are not listed here — they come and go with the
server, so drop the server from `mcp-servers:` instead.

Automatic context **Compaction** keeps a long session from overflowing the model's
window: when the conversation history outgrows its budgeted share, apogee folds the
older turns into a summary (the same reducer as the `/compact` command) before the
next request. The same fold is also apogee's **overflow recovery**: when a request
does not fit the window after all — or the estimate already says it cannot — the
history is folded mid-task and the turn is re-sent once, so a long task survives
instead of dying on "context window exceeded". It is structural and load-bearing —
it stays on even under `--bypass` — so it is on by default; set `auto-compact: false`
(a file-only key) to manage the window yourself with `/compact` instead, which opts
out of the recovery too.

The context **window** these budgets are measured against is discovered from the
server — live, not once: apogee asks every ten seconds, so switching the loaded model
under a running session re-binds the window with it. Set `context-window:` (a file-only
key, in tokens) only when your server does not advertise a window, or when its number is
wrong for how you run it; that key is a **pin** the heartbeat never overrides. With no
window known, the Budget and automatic compaction stay inactive and apogee says so in the
transcript the moment it binds a model without one. How that window is **split** is a second
file-only key: apogee holds a fifth of it back for the model's reply and lets the prompt fill
the rest, and `response-reserve:` (a fraction above 0 and below 1) sets your own share instead
— raise it for a model that answers at length, lower it to spend more of the window on history.

Every reply is **bounded**, and by the same budget: apogee tells the server how many tokens
one answer may take, using the room it already reserves for the reply — clamped to between
4,096 and 32,768 tokens, and to the floor when no window is known. Without that ceiling a
thinking model can reason for an hour and hit the context wall instead of answering. Set
`max-output-tokens:` on a `servers:` entry (in tokens) to pin your own ceiling for that
server, whatever its window says — which is how you let a cloud endpoint that advertises no
window answer at length. A reply that runs into that ceiling with nothing visible to show
for it fails the turn and names the cap and roughly what the reasoning cost, rather than
reporting an empty reply: the remedy is a bigger ceiling or a smaller task, not a retry.

**How hard a model thinks** is a property of the model, so it rides its profile: a
`model-profiles:` entry's `thinking:` block takes `effort:` — `off`, `low`, `medium` or
`high` — and apogee forwards that to the server's chat template, which is where a
reasoning model's dial actually lives:

```yaml
# ~/.apogee/config.yaml
model-profiles:
  qwen3.8:
    thinking:
      effort: medium
```

Leave the key out and **nothing at all** is sent, so the model's own default stands —
which is exactly why you would set it: Qwen3.8's template reasons at its `xhigh` default
unless told otherwise, which is a great deal of thinking for a one-line edit. `off` asks
for no reasoning at all. The key is orthogonal to `style:` beside it, which only says how
reasoning *arrives*; a value outside those four is a startup error, and a template that
rejects an effort it does not support fails the turn with a message naming this key.
`/effort <level>` layers a **session override** on top — `auto` drops it and hands the
model back to its profile, bare `/effort` reports what the two layers resolved to. That
override is session intent, not configuration: it survives a model switch, is never
written to the file, and stays on the primary loop — a delegated sub-agent resolves
effort from its own profile.

The prompt's caret is the **real terminal cursor**, and it never blinks. Set
`cursor-shape:` (a file-only key) to `block` (the default), `underline`, or `bar` to say
which shape it takes; your terminal's own cursor comes back when apogee exits. A
full-screen terminal program has to name a cursor shape on every frame, so inheriting the
one your terminal is configured with is not something apogee can express while it runs —
this key is the honest substitute. The cursor is shown wherever the box is editable
(including while the model works) and hidden where it is not, such as at an approval
prompt.

Set `editor:` (a file-only key) to the command an external edit opens in — the whole command
line, split on spaces, so flags travel with the program and the file is appended as the last
argument:

```yaml
# ~/.apogee/config.yaml
editor: code -w
```

It heads a **four-rung ladder**, highest first: this key, then `$VISUAL`, then `$EDITOR`, then
your platform's default opener — `open` on macOS, `xdg-open` on Linux, `cmd /c start` on Windows.
An explicit setting outranks an ambient one, so a command you put here is the command that runs and
the `/settings` row showing it is not being quietly beaten by a variable it cannot show. Leaving it
unset means *whatever already opens `.yaml` on this desktop*, not `vi`; the row is then blank,
because the rungs below this key are not this key's to record. If nothing on the ladder names a
program this machine has, the `⏎` jump refuses on the row and names all three ways to set one
rather than repeating a "not found in `$PATH`" you cannot act on.

### The servers you run models on

The `servers:` list is the **single definition** of what apogee can talk to — one
entry per OpenAI-compatible server — and the `server:` key names the one a session
starts on.

```yaml
# ~/.apogee/config.yaml
servers:
  - name: workstation
    endpoint: http://192.168.64.1:1111
    model: gpt-oss-20b           # optional hint; the heartbeat binds what is served
  - name: rented-box
    endpoint: https://llm.example.com
    api-key: sk-rented-token     # optional; or api-key-cmd: / api-key-env: — exactly one of the three

server: workstation
```

An entry's `name` is the label `/server` lists it under, the argument
`/server <name>` takes, the value `server:` points at, and the host name the status
footer shows while the session is on it — one name for all four jobs, so no two
entries may share one. `endpoint` is required; `api-key` (or `api-key-cmd` /
`api-key-env` — exactly one of the three), `model`, `parallel-agents` and
`sub-agents` are optional, as is `llama-launcher`, which lets apogee start,
switch and stop that server itself — [below](#local-servers--llama-launcher).

**Several sub-agents at once.** When one reply asks for several delegations, apogee
runs them concurrently — as many at a time as that server's cap allows. Unset, the cap
is whatever the server says: a llama.cpp started with `--parallel N` advertises N slots
and N becomes the cap; a server that advertises nothing runs delegations one at a time,
as apogee always has. `parallel-agents: N` (a file-only key) sets the width yourself,
and is a **pin** apogee never overrides. Mind the trade the server makes for you:
`--parallel N` splits its context into N slots, so more parallel agents means a smaller
window each — the per-slot number is the one apogee has always shown you. A sub-agent's
own delegations stay one at a time. `apogee headless` resolves the cap the same way a
session does: the pin if the entry carries one, and otherwise a single look at what the
server advertises, taken once as the run is composed. A scheduled firing runs at the
width the session it fires beneath is running at, read when it fires — so a `/server`
switch carries the new server's cap into the next firing.

**Delegations can run on a server of their own.** `sub-agents: true` on one
entry (a file-only key) sends every delegation there: your conversation stays on
the session's server while sub-agents fill the flagged one, and each delegated
run says which model it ran on. If that server's API key cannot be resolved,
delegations fall back to the session's own server and the reason is reported
once.

**`server:` keeps itself current.** Every `/server` switch onto a listed entry
splices `server: <name>` back into the file — that one key, your comments and layout
untouched — so your next start begins where you left off. A move onto a server the
list does not name (an `--endpoint` URL, a llama-launcher profile) has no name to
record and writes nothing.

**And it can remember the model too.** `remember-model: true` (a file-only key,
off by default) makes an explicit `/model` pick write itself into that entry's
`model:` key, so your next session on that server starts bound to it. A
launcher-fronted entry records the Launch profile name in `launch-profile:`
instead, and an interactive session that starts there loads that profile again —
unless any server is already running under that launcher, which apogee joins
rather than replaces. Only an explicit pick or a committed load records: a model
change the heartbeat merely observed, and the `--model` / `APOGEE_MODEL`
overrides, never write anything. See
[ADR 0048](docs/adr/0048-apogee-remembers-the-model-choice-per-server.md).

**The first run asks.** With `server:` unset, apogee starts with **no server
bound** — no engine constructed, nothing pointed anywhere — opens the `/server`
picker over your entries, and records what you choose. A `server:` naming an entry
that is gone is handled the same way: apogee says which name went missing and opens
the picker, rather than refusing to start. With the list empty it opens
[`/settings`](#the-settings-screen--settings) instead and points you back at this
file — add an entry and restart. `apogee headless` and `apogee probe` have nobody
to ask, so there a startup with no determinable server is refused outright, naming
the config file and the line or block that would fix it.

**An override runs one session elsewhere.** `--endpoint` / `APOGEE_ENDPOINT` starts
this run on an unlisted server: it wins over any `server:` name, takes its bearer
token from `APOGEE_API_KEY` and its model hint from `--model` / `APOGEE_MODEL`, and
is never written back. `--server` / `APOGEE_SERVER` picks a listed entry by name
instead, riding the ordinary flag-over-env-over-file precedence on the `server:`
key; with no endpoint override, the key and hint variables overlay those two fields
of whichever entry the session starts on.

**A config in the retired schema migrates itself, once.** The four top-level keys
this schema replaced — the endpoint, the api key, the alias and the model hint that
used to sit outside any list — are folded into a `servers:` entry plus a `server:`
pointer the first time this build reads the file: the original is copied to a
timestamped `config.yaml.bak-YYYYMMDD-HHMMSS` sibling first, your comments and every
other key survive the rewrite, the result is re-parsed and compared against the
original before it replaces the file, and one startup line names what moved and
where the backup is. If the fold cannot be made
safely — no `endpoint:` among those keys, a name the list already uses, a `server:`
you already set — **nothing is written at all** and the error carries the block to
paste in their place. A config already in the new schema is never touched.

### The upstream API key

A local server usually wants no credentials, but some do: llama.cpp started with
`--api-key`, LM Studio, a remote vLLM, any keyed OpenAI-compatible proxy. Give
apogee that token and it rides **every** wire to the endpoint as
`Authorization: Bearer <key>` — your conversation, the ten-second heartbeat, and
both halves of `apogee probe` — so a keyed server never leaves the footer stuck
on a `401` while the session works. It belongs to the server that wants it, so it
lives in that server's entry:

```yaml
# ~/.apogee/config.yaml
servers:
  - name: rented-box
    endpoint: https://llm.example.com
    api-key: sk-my-server-token
```

```console
$ APOGEE_API_KEY=sk-my-server-token apogee
```

The environment variable **overlays** the key of the entry this session starts on
(and carries the token for an `--endpoint` override, which has no entry to take one
from), and there is **no `--api-key` flag** on purpose: a secret typed on the
command line lands in your shell history and in `ps` output on every OS. Leave the
key out — the local default — and no `Authorization` header is sent at all, exactly
as before this key existed.

The value is never displayed: `apogee probe` reports only *whether* a key was
resolved (`api key: configured (sent as a bearer token)`), the settings screen
summarizes the whole `servers:` block rather than rendering it, and the provider
client redacts the key from any error text the server echoes back. One caveat is
yours to weigh: `config.yaml` is plain text, so on a shared machine prefer the
environment variable, or restrict the file's permissions yourself.

**The key need not live in this file.** An entry names its key one of three ways, and
**exactly one** of them — a second source on one entry is a startup refusal, because
nothing can say which one the file meant. `api-key:` is the literal token above.
`api-key-cmd:` is a command whose standard output *is* the key
(`api-key-cmd: pass show apogee/rented-box`,
`api-key-cmd: op read op://Private/rented-box/credential`), so the token stays in the
manager that already holds it: the line is split on spaces and quotes and run **with no
shell** — pipes, redirections and `$VARIABLES` need a wrapper script of your own — and
the command's stdout, trailing whitespace trimmed, is the key. `api-key-env:` names an
environment variable rather than holding a key (`api-key-env: OPENROUTER_API_KEY`), read
from the environment apogee itself was started in — and dropped from the environment the
`terminal`, `python_exec` and `run_tests` tools hand a subprocess, so a command the model
chose cannot read that key back out. Both resolve the first time this session actually
needs that server's key — never at startup for entries you do not use — and the answer is
remembered for the rest of the session. A non-zero exit, a 60-second timeout, empty output,
or an unset or empty variable is an **error** naming the entry, never a silent keyless
request: "no key" is spelled by leaving all three keys out.

**A plaintext key earns an offer to move.** When the machine has a secret store
apogee can use — the macOS Keychain, or a Secret Service keyring via
`secret-tool` — startup offers to move each plaintext `api-key:` into it. Taking
the offer stores the key, reads it back through the very `api-key-cmd:` line
about to be written, and only on a match rewrites the entry — one line, comments
and layout untouched. "Not now" asks again next start; "never for this entry"
writes `plaintext-key-ok: true` beside the key and is not asked again. A machine
with no usable store — and every unattended `apogee headless` run — gets a notice
naming the entries and the alternatives instead.

### Local servers — llama-launcher

`/server` moves a session between servers that are **already running**. Bringing one
*up* is what [llama-launcher](https://github.com/airiclenz/llama-launcher) does — a
separate tool that stores the **Launch profiles** llama.cpp itself has no store for:
which model file, which server (llama.cpp, Ollama, LM Studio), and under what flags.
Apogee imports it as a library, so three commands act on this machine's servers:

- **`/model`** — make the world serve a profile. While the session is on a server entry
  that names a launcher, "switch model" is answered from the launcher's side: the picker
  lists the **Launch profiles**
  its config defines, in the launcher's own order (favourites first), instead of the
  one-row list a single-model server advertises. Each row carries the backend, the
  context window the profile configures, `· running` when that profile is live right now,
  and the port when it is not the one this session is pointed at; the profile already
  serving this session is not offered, so every row you can see switches something.
  `/model <name>` activates one by name. On a server with no launcher the verb is
  unchanged — what the server advertises, minus the model you are already on.
- **`/unload-model`** — free the model of the server this session is on. On a *managed*
  llama.cpp server the model is baked into the process, so unloading it stops the
  server — the transcript says which of the two happened.
- **`/stop-server`** — stop the server this session is on; the footer's ordinary offline
  handling narrates the rest.

All three are ordinary menu rows. The last two name what they act on, which is what makes
them safe to offer: neither can touch anything but the server this session is talking to.

Apogee never becomes a process manager. The launcher **actuates**, the ten-second
heartbeat **observes**, and it is the next beat that binds whatever it finds — the same
path a model changed from the server side already travels. A profile that resolves to
another server moves the session there, conversation and all, exactly like `/server`; a
profile on the server you are already on moves nothing.

A load blocks while the server comes up, so it is narrated rather than modal: each
launcher step lands as a transcript note as it happens, and the footer's model slot
shows `loading <profile>…` until the beat binds. One actuation runs at a time — while
one is in flight, sends and the other switching commands are refused with a single line
— and there is no mid-flight cancel: `/stop-server` is the cancel, available the moment
the verb returns. When the health wait times out the launcher deliberately leaves the server
running and names its PID and log path; apogee prints that and adds the honest coda —
the heartbeat will bind it if it comes up.

One **file-only** key drives all of it, and it belongs to the server it fronts:

```yaml
# ~/.apogee/config.yaml
servers:
  - name: workstation
    endpoint: http://192.168.64.1:1111
    llama-launcher: auto         # absent = no launcher · auto = its default config · or a path
  - name: rented-box
    endpoint: https://llm.example.com
```

**The launcher follows the session.** While you are on an entry that carries the key,
`/model` answers from the launcher's side and the other two verbs act on that server;
`/server` onto any other entry — the rented box above, an OpenRouter — and the same
`/model` goes back to listing what *that* server advertises. This is the point of the
key living on the entry: one config can hold a machine you launch models on and a remote
provider whose model list you would rather not lose. Coming home is the two steps it
looks like: `/server workstation`, then `/model`.

Absent (the default) means **no launcher for that server**: `/model` simply lists what
it advertises, and `/unload-model` and `/stop-server` answer
`llama-launcher not configured`. `auto` reads the launcher's own default config under
your home directory — `~/.config/llama-launcher/config.yaml` — and a path reads that
config instead (`~` expands). Nothing is checked at startup — a config that is not there
is reported the first time a command reaches for it, naming the path, never as a refusal
to start — and every command re-reads the file, so a profile added in the launcher's own
TUI is offered by the next `/model`.

Activating a profile that resolves to an endpoint no entry names keeps the launcher on
for that session, so the next `/model` still answers from its side. A session started
with `--endpoint` carries no launcher at all.

**Upgrading from the old global key.** `llama-launcher:` used to sit at the top level and
turn the integration on for every server at once. A config that still sets it is refused
at startup, with the file, the line, and the complete `servers:` entry to paste in its
place — an old bare `llama-launcher:` (the auto-detect shape) becomes `auto`, and an old
`off` needs only the deletion, since an entry with no key already has the launcher off.

Two limits are worth knowing. The launcher runs local processes, so the verbs that start
and stop one need a Unix-like host: on **Windows** apogee still builds and everything the
launcher drives over HTTP works (discovery, loading and unloading models against Ollama
or LM Studio, activating a profile on a server that is already up), while starting a
managed `llama-server` or signalling one to stop reports a clean unsupported error. And a
launcher on **another machine** is a different thing — reach that one as an
`mcp-servers:` entry pointing at the launcher's MCP adapter; the two compose. See
[the launcher design record](docs/adr/0029-the-launcher-actuates-local-servers-and-the-beat-completes-every-move.md).

### The system prompt

The system prompt is the standing instruction apogee sends ahead of your first
message, as the first system message of every request. A fresh install starts with
a short default one; **delete it or comment it out to send no system prompt at
all**. An existing `~/.apogee/config.yaml` — which an upgrade never touches —
carries no such key, so nothing changes for a setup you already run.

```yaml
# ~/.apogee/config.yaml
system-prompt-text: |
  You are apogee, a coding agent working in the workspace at {{workspace}}.
  Today's date is {{datetime}}. You are operating in {{mode}} mode.

# ...or keep it in a file of its own instead (never both at once):
# system-prompt-file: ~/prompts/apogee.md

# ...and optionally override it for one model:
# system-prompt-models:
#   qwen2.5-coder:
#     system-prompt-text: "Be terse. Use tools; do not narrate."
```

Three placeholders are substituted fresh on every request: `{{workspace}}` (the
workspace path), `{{datetime}}` (today's **date** — not a timestamp, which would
change the prompt every turn and throw away your server's prefix cache), and
`{{mode}}` (the autonomy mode, so a Shift+Tab shows up from the next request on).
The spelling is strict and the set is closed — anything else in double braces,
`{{ workspace }}` included, is a startup error listing the three.

A `system-prompt-models:` entry keyed by the model name apogee resolves at startup
**replaces** the global prompt for that model, whole; an entry naming a model you
are not running is simply inert — never selected, its file never read — so one
config can carry a prompt per model across every machine it travels to. Sub-agents
inherit your prompt; apogee's own internal calls (the conversation summariser,
`apogee probe`'s battery) keep their dedicated prompts and never see it; and the
prompt never enters your conversation history or a saved session.

Beside your prompt, apogee folds in the **project's own** standing text: it looks
for `AGENTS.md` in the workspace root at the start of every session and appends it
to that same first system message, under a header naming the file — so a repo that
already keeps one for other agents is picked up with nothing to configure. The
file-only `context-files:` block is where you change that: `names:` is the list of
names to look for (all of the ones that exist are included, in your order) and
`enable: false` turns the whole thing off. A name that is not there is skipped
silently — one config travels across repos that carry different files, or none —
and a file that is present but unreadable is reported in the transcript rather than
stopping apogee. The content goes out **verbatim**: the placeholders above do not
apply to it, so a repo's own `{{braces}}` can never fail your startup. The files
are read when a session starts and re-read on `/clear`, `/new`, or a resume, never
mid-conversation, so editing `AGENTS.md` while apogee runs takes effect on your
next `/new`; apogee names each file it loaded, with its size, and warns you (never
truncates) when the standing content outgrows its share of the context window.

### Showing a finished document

When the model finishes a deliverable — a report, a review, an HTML summary — it calls
`present_document` and hands apogee nothing but the path. **Apogee decides how to show
it; the model never reasons about your platform.** Whatever it decides, the document's
workspace-relative path is always printed in the transcript, which most terminals (Zed,
VS Code, iTerm2, WezTerm, kitty) make cmd/ctrl+clickable. Above that baseline: on your
own desktop the file is opened in its associated application — documents, images and
text only, because anything the OS would *run* rather than show (a `.bat`, a `.command`,
a `.desktop`) is left as a path for you to open deliberately. A **web page counts as
something it would run**: `.html`, `.htm`, `.xhtml` and `.svg` are left as a path too,
because a browser executes what a page carries — including a page that merely arrived in
a repo you cloned — and a `file://` launch can carry no policy to stop it. Over SSH — a
devbox, a VM, a container — browser-renderable documents (`.html`, `.htm`, `.svg`,
`.pdf`) are served from a small built-in server and the URL is printed beside the path,
so one cmd+click opens the document in the browser on *your* machine; that rung keeps the
web formats precisely because a served response *can* carry a policy, and every document
it serves is answered under `default-src 'none'` with `nosniff`. Apogee never auto-opens
on the remote box: there is no display there to open into. If a rung fails, the
transcript says so and falls back to the path.

The built-in server hands out one random-token URL per presented document — no directory
listing, no other file reachable — re-reads the file per request, starts only when a
document is actually served, and stops when apogee exits. Four **file-only** keys tune
all of this:

```yaml
# ~/.apogee/config.yaml
present:
  auto-open: true        # open documents on a LOCAL desktop run; false = only print the path
  command: "zed {path}"  # open with THIS application instead of the OS default
  port: 0                # the built-in server's port; 0 (default) picks a free one per session
  host: ""               # address the printed URL advertises; empty = detected
```

`host` is a fallback, not an override: over SSH the address you connected to this box on
is used, because it is known-routable. If a printed URL is unreachable on **macOS
Sequoia or later**, the first browser connection to a local-network address needs Local
Network permission — Chrome fails with a generic "this site can't be reached" until you
allow it in System Settings → Privacy & Security → Local Network, while Safari tends to
work straight away. The path line works regardless.

### Auto mode's blast radius

Auto is the one unsupervised mode, so it is fenced: filesystem writes are confined to
the workspace at the OS level, the network is open, and MCP still asks. All three
platforms have a backend — landlock on Linux, `sandbox-exec` on macOS, a restricted
low-integrity token on Windows. Where the OS cannot fence a command — a Windows build
older than 10 1809 (17763), and most containers, where landlock reports `ENOSYS`
regardless of kernel version — Auto keeps the promise the honest way and asks before
each shell call instead of running it unbounded ("confine if you can, gate if you
can't"). That is not a fault, so Apogee says so at startup rather than letting Auto
look broken.

**On Windows the fence is a token, and the box is a mark on your disk.** No Windows
facility takes "these paths are writable" as an argument, so the command runs under a
restricted, *low-integrity* token — the kernel then denies it any write to an object
that is not explicitly marked low, and the denial is inherited by every process it
spawns. The workspace is what carries that mark for the session, and it is reverted on
exit; an interrupted run leaves a journal behind, which `apogee probe` reports. Two
things worth knowing before you use it: network egress is **not** claimed on Windows
(the network is open there exactly as elsewhere, and a box that asks for network *deny*
is refused rather than silently ignored), and the marking pass costs roughly a
millisecond per file or directory — with a large `.git` or `node_modules` in the
workspace, the first confined command of a session visibly pauses while it runs
(measured: ~5 s to mark a 5,000-object tree, ~2 s to revert it), after which every later
command in that session pays nothing. And one limit: what the Windows fence covers is
workspace-scoped writes. A low-integrity process cannot write to an unmarked directory
at all, so a confined `go build`, `pip install` or `npm ci` fails when it reaches its
cache or `%TEMP%` outside the workspace — giving the toolchain a box-local temp and
cache directory is a recorded follow-on (`ISSUES.md`), not something Apogee does yet.

If the machine is disposable and you would rather have Auto unfenced there, `/confine`
is the route. `/confine` (or `/confine status`) reports the backend, what it can
actually enforce here, this host's id, and the effective setting. `/confine off` runs
Auto unconfined **for this session** and writes nothing; `/confine off --save` also
records this machine in `~/.apogee/config.yaml`, comments and formatting intact:

```yaml
# ~/.apogee/config.yaml
unconfined-hosts:
  - id: "devbox-a1b2c3"                # this machine's id — /confine reports it
    acknowledged: "2026-07-21"
    note: "disposable container, landlock unavailable"
```

The acknowledgement is **host-scoped on purpose**: "this machine is disposable" is a
claim about one machine, so it must not travel with your config file onto a laptop. The
id is a safety interlock, not authentication — it fails closed, so an unrecognised
machine is simply confined again. Delete the entry to re-confine a host; `/confine on`
does the same for the running session.

`confine-to-workspace: false` remains the global blanket loosen and still means *every*
host. Both keys are **global-config-file-only** — no flag, no environment variable, and
no project config — because editing that file is the deliberate acknowledgement, and a
repo you cloned must never be able to make that claim for you.

## Diagnosing a host — `apogee probe`

`apogee probe` answers "what would Auto do on this machine?" without running an agent.
It reads `config.yaml` and the `APOGEE_*` environment exactly as a session would, and
reports the OS/arch, the confinement backend and what it can *actually* enforce here,
the Auto verdict, the effective `confine-to-workspace` after any host acknowledgement,
the workspace root and config home, and whether the configured endpoint answers
(`/v1/models`, plus llama.cpp's `/props`). It is free, offline and **read-only** — no
model is called, no starter config is seeded, nothing is written. `apogee probe host`
is the same report under a named child, for scripts.

```console
$ apogee probe
apogee probe — host report
  (no agent runs, no model is called, nothing is written)

host
  os/arch:       windows/arm64
  ...
confinement (ADR 0012)
  backend:       token (fs-write: available · network: unavailable)
  auto:          eligible — the backend can fence terminal commands, so auto runs them confined
```

`apogee probe model` is the other half, and it is deliberately an **explicit act**
rather than something the bare noun triggers, because it costs live model calls *and*
writes. It runs a three-part capability battery — a native tool call, JSON/structured
output, and a multi-step tool chain — then prints what it observed, an ordinal
capability tier, and the `model-profiles:` entry the findings suggest — keyed by the
model it probed, and paste-ready as YAML
(your `config.yaml` is never edited). It also records a **behavioral fingerprint**: the
model keeps its advertised name — probing never renames it, so Validated-set entries,
aliases and Library observations keyed on that name keep matching — but its identity
rises from *low* to *medium* confidence, which is what promotes a matching Validated
set from offered to auto-applied on later runs. `--no-save` runs the whole battery and
records nothing; the record's path is printed either way, so deleting that file undoes
it.

`apogee probe terminal` is the third subject, and it is free like the host report. It
**measures** the terminal instead of trusting it: it writes real escape sequences to your
terminal and reads the answers back, then prints what it found — how it answers about
synchronized output and grapheme clustering (modes 2026 and 2027), how many cells it
really advances for an emoji or a combining sequence with that mode off and on, where its
tab stops are and whether a tab erases what it passes over, what happens when a glyph
lands in the last column (a pending wrap or an immediate one), and the capabilities it
actually has beside the ones apogee's renderer assumes from `TERM`. A section whose answer
disagrees with what was assumed is marked `MISMATCH` and
sets the exit status, so the report can be checked by a script and not only read. It needs
a real terminal on both stdin and stdout — a redirect or a pipe leaves nobody to answer —
and it calls no model and writes nothing.

```console
$ apogee probe terminal
apogee probe terminal — measured, not assumed
  (nothing is written; the screen is restored)

  size:          120 columns × 30 rows
  TERM:          (unset)
  ...
last-column wrap
    step                    cursor (CPR)  console API  deferred wrap would be
    wrote the final column  6,120         6,120        6,120 (pending)
    wrote one more          7,2           7,2          7,2
  OK — the terminal holds a pending wrap at the last column — the semantics the renderer emits against
```

**When a frame comes out wrong**, two hidden flags record the evidence a rendering bug is
argued from — `--tui-trace <file>` writes the exact bytes the renderer emitted, one quoted
string per write, so a corrupted frame can be replayed rather than only described, and
`--tui-diag <file>` writes what the terminal told apogee about itself: the environment the
renderer read, the width method it started on, the window size, the colour profile, and
every mode report the terminal sent — each written once and again only when it changes, so
the file stays short enough to paste into a bug report.
Both default off and cost nothing unless you name a file, both work on every OS, and
neither appears in `--help`: they are for a bug report, not for a session. `⌃l` is the
in-session counterpart — it forces a full repaint and is usually all a smeared frame needs.

## Running one prompt — `apogee headless`

`apogee headless` runs a single prompt to completion with nobody watching and prints
the answer to stdout. It is the same unattended run a `/schedule` firing performs, over
the same shared runner — the second front-end onto the engine rather than a second
agent.

```console
$ apogee headless "list the exported types in apogee.go, one per line"
Agent
Config
...
session: 20260805-141233-7f2a · turns: 3 · denied: 0
```

The prompt is the single **quoted** argument; with no argument the whole of stdin is
the prompt, so `cat task.md | apogee headless` works too. Empty from both is a usage
error. `--endpoint`, `--model`, `--workspace` and `--config` resolve exactly as a
session's do — flag over `APOGEE_*` environment over `config.yaml` — so the run has the
shape a session on this host would have; which listed entry it starts on comes from
`APOGEE_SERVER` or the `server:` key, there being no `--server` flag on this command.
Nobody is there to answer a picker, so a run those sources leave with no server to start
on is refused before anything is composed — the config file and the fix named, exit `2`. It is saved to
`~/.apogee/sessions` and shows up in `/sessions` like any other; `--no-save` runs it and
records nothing.

`--mode` takes `plan` (the default, read-only) or `auto` — the two modes that never need
a human. `ask-before` and `allow-edits` are refused, and so is `auto` on a host whose
confinement backend cannot fence the filesystem: there the interactive fallback is
approval, and an unattended run has nobody to approve (see
[Auto mode's blast radius](#auto-modes-blast-radius)). Whatever the mode, every gated
action is refused rather than parked — the refusals are the `denied:` count — `ask_user`
and `present_document` are not registered, and no MCP server is contacted.

Only the model's answer goes to **stdout**; resolution notices and the one-line summary
go to **stderr**, so a pipeline reads the text and nothing else. A run that delegated
adds one stderr line per sub-agent run just ahead of that summary —
`sub-agent: 12k/32k · <the name it was given, else the task>`, in the order the runs
finished — because
each child fills a context window of its own that the run's own figures say nothing
about; a run that delegated nothing prints none. Beside those, and on the same terms,
comes what the run **spent**: `usage: calls 3 · prompt 18k · completion 1k · total 19k`
for the run itself and one such line per delegated run (labelled the same way), counting
every model call the agent made — the compaction folds included, which no context reading
shows. They are the addends and never a sum: an agent that made no call prints no line.
The exit status says which kind of
thing happened:

| Exit | Means |
|---|---|
| `0` | the run completed |
| `1` | the run started and failed — model or tool error, cancellation, a record that would not save |
| `2` | the run never started — usage, configuration, a refused mode |

## Building from source

Not the only way in any more — [Install](#install) has Homebrew and prebuilt
archives — but it stays the shortest path to the tip of `main`.

**Prerequisites:** Go 1.26+ (the toolchain version pinned in `go.mod`).

```bash
git clone https://github.com/airiclenz/apogee.git
cd apogee
make build      # compiles ./apogee
./apogee --help
```

A `Makefile` wraps the common Go invocations:

| Command | Does |
|---|---|
| `make build` | Compile the binary to `./apogee` |
| `make install` | Build, then copy the binary to a directory on your `PATH` |
| `make run ARGS="--help"` | Build-and-run, passing flags via `ARGS` |
| `make test` | Run the test suite with the race detector |
| `make cross` | Cross-build all six release targets (Linux/macOS/Windows × amd64/arm64) |
| `make dist` | Build the publishable release archives into `dist/`, plus `SHA256SUMS` |
| `make check` | The full acceptance gate — gofmt, vet, build, race tests, the ADR-0010 import invariant, cross-build, and an `apogee --help` smoke run |
| `make help` | List every target |

To run `apogee` from anywhere, `make install` copies the built binary to the first
directory that is both on your `PATH` and writable without `sudo`, trying
`/usr/local/bin`, your Go bin dir (`go env GOBIN`, else `$(go env GOPATH)/bin`),
`~/.local/bin`, `/opt/homebrew/bin` and `~/bin` in that order. It never installs
somewhere your shell cannot find it: if nothing qualifies — the usual case on macOS,
where `/usr/local/bin` belongs to root — it stops and prints the two ways to finish,
either `sudo install -m 0755 ./apogee /usr/local/bin/apogee` or an explicit
`make install PREFIX=~/.local/bin` plus the line that puts that directory on your
`PATH`. `PREFIX` overrides the search entirely.

No clone at all? `go install github.com/airiclenz/apogee/cmd/apogee@main` builds and
installs straight from the tip of `main` into your Go bin dir (pin a commit with
`@<sha>` instead). Only `@latest` is off-limits — proxy.golang.org immutably retains
the retired `v1.x` module versions, so `@latest` resolves to stale `v1.7.0`.

Prefer the raw toolchain? `go build -o apogee ./cmd/apogee` does the same thing — the
Makefile just gives the common commands one-word names. Releases are cross-compiled to
all **six** targets — Linux, macOS and Windows × `amd64` and `arm64` — from any one of
them: the tree is CGO-free, so `make dist` builds and packs the entire published
matrix on whichever machine cuts the release (`make cross` is the same six builds
thrown away, as a compile check), and every OS-specific backend is behind a build tag
rather than a separate artifact. `make dist` needs `zip` on the box for the two
Windows archives; everything else it reaches for is either the Go toolchain itself or
standard on any Unix-like box (`tar`, `sed`, and `sha256sum`/`shasum`).

**Reading the code?** [`AGENTS.md`](AGENTS.md) is the single map: it says where each
kind of knowledge lives — `CONTEXT.md` for the domain language, `docs/adr/` for the
settled decisions, `docs/design/` for the contracts, `layout.md` for the TUI spec — and
states the conventions you cannot derive from the source. Per-package `doc.go` files
carry the file-by-file tours from there.

> **Note:** launch the TUI with `apogee --endpoint <openai-compatible-url> --model <name>`
> to hold a real coding conversation with a local model. All four autonomy modes, the
> full tool suite, MCP, sub-agents, sessions, and skills are live; `apogee probe`
> reports which confinement case this machine is in (see
> [Auto mode's blast radius](#auto-modes-blast-radius)).

## License

MIT — see [LICENSE](LICENSE).
