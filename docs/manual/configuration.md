# Configuration

Settings resolve by precedence, highest first: a command-line flag overrides an
`APOGEE_*` environment variable, which overrides `~/.apogee/config.yaml`, which
overrides the built-in default. A documented starter `config.yaml` is written to
`~/.apogee` on first run (your edits are never overwritten): nearly every setting
is there as a commented example, and the few that ship active carry the value
apogee already recommends. Three keys carry all four layers —
`server:` (`--server`, `APOGEE_SERVER`), `mode:` and `bypass:`. Every other key
is **file-only** (no flag or env): the `servers:` list, the system prompt, the
model profile, MCP servers, [the web-search
endpoint](#where-web_search-looks--web-search-endpoint) and the small-model
mechanisms among them. Two raw overrides are not config keys at all — `--endpoint`
/ `APOGEE_ENDPOINT` runs one session against a server the file does not list,
while `APOGEE_API_KEY` and `--model` / `APOGEE_MODEL` carry that server's token
and model hint or overlay those two fields of the listed entry a session starts
on (see [The servers you run models on](#the-servers-you-run-models-on)). The
upstream key has an environment variable but deliberately **no flag** (see
[The upstream API key](#the-upstream-api-key)). Every `APOGEE_*` variable the binary
reads, with its flag and its precedence, is listed under [Environment
overrides](#environment-overrides).

That file is also readable and editable from inside apogee:
[`/settings`](commands.md#the-settings-screen--settings) lists every setting with the value this
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
different is applied exactly as an in-pane edit is, and its row repaints wearing a ` ~` — the marker
for *a save on disk moved this key*, beside the ` *` a row wears when you changed it in the pane. An
edit reaches the runs this session raises, too: a `/schedule` firing composes itself from the
settings the session is running at the moment it fires, so a tool you disabled or a host you denied
is disabled and denied for it as well. A file that does not parse changes nothing — the session
keeps running the settings it had, because a poll will sooner or later read a half-written save —
and only when three saves in a row fail to parse does apogee say so in the transcript, once, until
the file parses again. `server:` is the one ordinary key a re-read never moves: it names where the
*next* session starts (see [The servers you run models on](#the-servers-you-run-models-on)). The
confinement pair — `confine-to-workspace:` and `unconfined-hosts:` — is left alone by a re-read as
well; that interlock stays single-homed in `/confine` (ADR 0012). A re-read that applied anything
says so in the transcript, in one line naming the keys that landed. The watcher is a poll of the
file's timestamp and size on a one-second ticker — no daemon, no filesystem-notification
dependency (ADR 0041).

Catalogued mechanisms are opt-in by canonical ID. Every mechanism ships **off**
until its A/B bench run proves it a win, so enabling one is a deliberate config
choice:

```yaml
# ~/.apogee/config.yaml
mechanisms:
  validate: true   # tool-call validation + auto-retry
  syntax: true     # write-content syntax check + auto-retry
  autofix: true    # formatter pass on tool-call payloads
```

The `syntax` mechanism is only the RETRY half: every write tool already appends its own
in-process syntax verdict to the success result it hands the model, always on and not
configurable, and enabling `syntax` adds the automatic correction Turn on top of it.

An unknown ID is a startup error that lists the IDs this build knows; `--bypass`
still wins (an enabled non-off-ramp mechanism does not fire under bypass). The same
catalogued mechanisms are enabled by ID from the Go API through
`Config.EnableMechanisms` (with `apogee.CataloguedMechanisms()` to enumerate them), so
a library embedder arms the identical stack without the config file. The
catalogue currently counts **21** mechanisms — see
[`docs/design/mechanism-catalogue.md`](../design/mechanism-catalogue.md) for
what each one does.

The **built-in tools** are all on by default — all but the default-off **Console family**
(`console_open`, `console_send`, `console_read`, `console_close`;
[what they do](#the-console-family)) — and `tools:` (a file-only block) is how you change that:
`disabled:` takes a tool off the menu — the model is never shown it, and a call naming it is
refused as a tool that does not exist — while `enabled:` puts one back on, for a tool this build
leaves off by default. The Console family is what that second list is for today: the four tools
are in the binary and offered to nobody until you name them.

```yaml
# ~/.apogee/config.yaml
tools:
  disabled: [view_diff, single_find_and_replace]
  enabled: [console_open, console_send, console_read, console_close]
```

It exists because a long tool list is itself a cost for a small model: fewer, clearer tools can
beat more of them, and this is the switch that lets you find out on your own work rather than
guess. The names are the ones the model calls a tool by — the same names the transcript shows
while a tool runs — and a name that is not a tool is a startup **notice** rather than an error,
with the rest of the list still applying, so a typo costs you the tool you meant to disable and
nothing else. A name written under both lists is a notice too, and `disabled:` wins.

The names this build knows are fixed. In menu order they are `read_file`, `write_file`, `list_dir`,
`grep`, `find_files`, `single_find_and_replace`, `multi_find_and_replace`, `edit_existing_file`,
`view_diff`, `copy_file`, `move_file`, `delete_file`, `terminal`, `python_exec`, `git_branch`,
`git_commit`, `git_diff_range`, `git_status`, `git_log`, `diagnostics`, `run_tests`, `web_fetch`,
`http_request`, `web_search`, `sub_agent`, the default-off Console four `console_open`,
`console_send`, `console_read` and `console_close`, `load_skill` — the model's own door onto the
[skill catalogue](#skills-apogee-ships--use-shipped-skills), which every apogee run wires — and
the two the **host** supplies rather than the build — `ask_user` and `present_document`, which the
TUI wires and a `headless` or `daemon` run does not, so a list naming one of those in a run without
them is no typo and raises nothing: it simply matches no tool that run offers. That list is what
`tools.disabled:`, `tools.enabled:` and a `model-profiles:` entry's `tools:` axis are all checked
against, and a name outside it is the notice above (as of this build; `KnownToolNames` in
`internal/tools` is the source, and a test fails when this list falls behind it).

The block is global (it applies to every model this config runs) and it is live like every other:
save the file, or commit the row in `/settings`, and the next request is built from the roster that
is left. An MCP server's tools are not listed here — they come and go with the server, so drop the
server from `mcp-servers:` instead.

A single model can also carry a roster of its own: a `model-profiles:` entry
takes the same two keys as a third axis, and what it says about a tool beats what the global block
says about it — that is how a tool stays off for the models that drown in it and on for the one that
wants it. One roster ships that way already: bind a **qwen3.8** build and the built-in shape table
offers it the Console family without your config saying anything, because that is the model that
asked for the family by name. A `tools:` axis of your own for that model replaces the built-in one
whole, the way every axis here replaces the built-in's — so `disabled: [console_open]` under a
`qwen3.8` entry of yours turns the whole family back off rather than trimming one tool out of it;
re-list the ones you still want under `enabled:`.

## Environment overrides

Eight `APOGEE_*` variables are read, and they divide by what each one can reach. Three of them carry
ordinary config keys and so ride the four-layer precedence above — flag, then variable, then file,
then default: `APOGEE_SERVER` (`--server`) names the `servers:` entry this session starts on,
`APOGEE_MODE` (`--mode`) the autonomy mode it starts in, and `APOGEE_BYPASS` (`--bypass`) the
Mechanisms switch spelled out below. A value one of these cannot parse — `APOGEE_MODE=fast`,
`APOGEE_BYPASS=maybe` — is a startup **error** naming the variable and the value, never a setting
that quietly falls back to its default. Being config keys, they appear in `/settings`: a row a
variable won says which variable won it, and committing an edit to that row applies now and then
tells you the variable outranks the file again at the next launch.

Three more override the **upstream one run talks to** rather than any key, which is why nothing ever
writes them back: `APOGEE_ENDPOINT` (`--endpoint`) points the session at a server `servers:` does
not list, `APOGEE_MODEL` (`--model`) asks for a different model than the entry it starts on names,
and `APOGEE_API_KEY` carries that server's bearer token. The key has **no flag** on purpose — a
secret typed on the command line lands in your shell history and in `ps` output (see
[The upstream API key](#the-upstream-api-key)). Inside each pair the flag still beats the variable,
and a flag you spelled out wins even when what you spelled is empty.

The last two the config file cannot set at all, because they say WHERE resolution itself runs — the
file would have to be found before it could name them. `APOGEE_CONFIG` (`--config`, then the
variable, then `~/.apogee`) is the apogee home holding `config.yaml`, the library and your saved
sessions. `APOGEE_WORKSPACE` (`--workspace`, then the variable, then the current directory) is the
workspace root — the fence every file tool is scoped to, so it decides what the model may read and
write at all, not merely which directory a session opens in.

`APOGEE_BYPASS` earns a paragraph of its own, because it gives something up. It turns apogee's
**Mechanisms off for the whole session**: every catalogued mechanism is skipped wherever it would
have fired, and the Validated set your bound model would otherwise be given is not applied either —
so a small model runs with none of the help apogee exists to give it. That is the point of it.
Bypass is the honest "Mechanisms-off" floor every mechanism is measured against on the bench
([ADR 0006](../adr/0006-bypass-mode-is-the-mechanisms-off-floor.md)), and it is the very code path
you can run yourself. What stays on is the agent's structure — context compaction, the Budget, the
empty-response off-ramp, the rest of the loop — so the floor is a working agent rather than a naked
model. The same switch is the `bypass` row in `/settings`, and it is live: flip it mid-session and
the next hook evaluation already sees it. [**Bypass mode**](../../CONTEXT.md) in `CONTEXT.md` is the
full definition.

## What the network tools may reach — `url-safety:`

`url-safety:` is the host layer over everything apogee reaches on the model's behalf. It takes two
lists — `allow-hosts` and `deny-hosts` — and they bind `web_fetch`, `http_request`, `web_search`
and an `sse` or `streamable-http` entry in `mcp-servers:` alike. Both are empty by default, which
means every host. An entry matches the host it names **and its subdomains**, so `example.com`
covers `api.example.com`; `deny-hosts` wins over `allow-hosts`, so a host written on both is
blocked; and the moment `allow-hosts` names anything it becomes the whole permitted set, every
host outside it refused. Only `http` and `https` are ever dialled, whatever the lists say.

Write a host the way you say it. Each entry is put into the same normal form the dialled host is
put into before the two are compared — whitespace trimmed, the surrounding brackets of an IPv6
literal removed (you write `[::1]`, the transport dials `::1`), IDNA-mapped when it is not ASCII,
lower-cased, trailing DNS root dot dropped — so `Example.COM.` blocks the host that arrives as
`example.com`. That happens where the guard is built, which is why neither list has a strictness
of its own for you to learn.

```yaml
# ~/.apogee/config.yaml
url-safety:
  allow-hosts: [docs.example.com, api.github.com]
  deny-hosts: [ads.example.com]
```

Underneath the lists sits the part no configuration can move: the **always-on SSRF floor**.
Loopback, private, link-local and cloud-metadata addresses are refused, along with CGNAT
(`100.64/10`), the whole `0.0.0.0/8`, the TEST-NET and benchmark ranges, and the NAT64 and
obsolete IPv6 transition ranges. The floor judges the **resolved** address, before the request
leaves and again as the connection is dialled, so a public name that resolves inward is refused
both times. The lists can only ever tighten what the floor already permits — there is no spelling
here that opens an address it closes.

Redirects are never followed. A 3xx comes back as itself: `web_fetch` renders the `Location`
header for the model, which may then ask for that URL in a fresh call that is checked from
scratch — one vetted request can never be carried somewhere unvetted. The LLM endpoint under
`servers:` is not subject to these lists at all; it is the address you launched apogee against
rather than one a model chose, though it too refuses to follow a redirect.

A configured MCP endpoint is the one deliberate exemption. An `mcp-servers:` address is one you
wrote in your own config, so the floor — which exists to stop a *model* pivoting to internal
addresses — is not applied to it, and the connection is pinned to that endpoint's own resolved
addresses instead ([ADR
0012](../adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md)). Your
two host lists still apply: a denied endpoint is refused at startup with the same url-safety
message, and any *other* private address that connection is later pointed at stays refused.

If your machine reaches out through an egress proxy, apogee uses it. `HTTP_PROXY`, `HTTPS_PROXY`
and `NO_PROXY` from the process environment are honoured by the network tools, by the MCP
transports and by the LLM client alike. The destination is still judged before anything leaves the
process, so a proxy cannot launder a private address; what the connection is pinned to is the
proxy's own addresses, because the proxy is what is actually dialled.

Both lists are live. `/settings` carries them as the `url-safety.allow-hosts` and
`url-safety.deny-hosts` rows, and committing either — or saving the file — rebuilds the session's
tool set around the new guard; that swap waits for the session to be idle, so a commit made
mid-turn is reported on the row and re-committing retries it. The connected MCP servers follow the
same edit: an `sse` or `streamable-http` endpoint your new lists close is disconnected there and
then, and the row names it (`mcp server docs disconnected — its endpoint is denied`). The rest of
your servers keep their connections, and an edit that closes no configured endpoint leaves every
one of them untouched. The block is file-only (no flag, no
environment variable) and global: it applies to every model this config runs.

## Where `web_search` looks — `web-search-endpoint:`

One string, with three things it can be. Unset — the default — is the built-in DuckDuckGo
provider, so web search works out of the box with no API key and nothing to run. The value `off`
(or `none`, or `disabled`; case does not matter) turns the tool into a graceful refusal without
taking it off the menu: the model still sees `web_search`, and a call answers "web search is
disabled on this host (web-search-endpoint: off); web_search is unavailable." Anything else is
your own search backend, which receives the query as the `q` URL parameter — an HTML response is
cleaned into title/url/snippet results, a JSON or text one passes through unchanged. A
scheme-less value heals to `https://`, and the only value refused at startup is text that no URL
parse can make sense of even after that.

```yaml
# ~/.apogee/config.yaml
web-search-endpoint: https://search.example.com/search
```

Disabling is not removing: `off` leaves a tool on the menu that says no, while
`tools.disabled: [web_search]` takes the name away from the model altogether. The endpoint is a
host reached over the network like any other, so `url-safety:` and the SSRF floor above judge it
exactly as they judge a `web_fetch` URL — a backend on `localhost` is refused by the floor
whatever your lists say. The key is file-only (no flag, no environment variable) and live:
commit the `web-search-endpoint` row in `/settings`, or save the file, and the tool is re-pointed
in place — the next call goes to the new endpoint, mid-session, with nothing rebuilt.

## Skills a repository ships — `use-project-skills:`

A **skill** is a folder holding a `SKILL.md` — frontmatter naming it, and a Markdown body of
instructions. Apogee always scans two places for them: your global library at `~/.apogee/skills`,
and the project's own `.apogee/skills`. `use-project-skills:` (default `true`) adds a third, the
workspace's bare `skills/` folder — the convention a repository follows when its skills are meant
for whichever agent shows up.

```yaml
# ~/.apogee/config.yaml
use-project-skills: false
```

Know what that turns on. A skill is prompt text somebody else wrote: it appears in your `/` menu,
and invoking it prepends the author's instructions to the message you send. Cloning a repository
therefore means accepting that its skills are on your menu, one keystroke from your next message
— read them the way you read its build scripts. Two things bound the trust. Your own library wins
every id collision ([ADR 0032](../adr/0032-the-user-skill-library-outranks-the-workspace.md)), so
a repository can contribute a **new** skill id but can never quietly replace one you invoke by
muscle memory; the copy it displaced is recorded rather than dropped, and
[`/skills`](commands.md) names both the live one and the shadowed one. And the skill folders the
model may read are mounted **read-only**, by their resolved real path — a `skills/` or
`.apogee/skills` that is a symlink pointing out of the workspace is neither loaded nor mounted,
so a repository cannot use one to widen what the file tools can reach.

The flip is live: commit the `use-project-skills` row in `/settings`, or save the file, and the
sources are re-scanned there and then — the `/` menu changes in the session you are already in.
[`/skills`](commands.md) lists what was discovered and where each one came from, and a skill body
may write [`{{SKILL_DIR}}`](commands.md#skill_dir-in-skill-bodies) to name the files bundled
beside its own `SKILL.md`.

## Skills apogee ships — `use-shipped-skills:`

Apogee carries a small set of skills of its own, compiled into the binary rather than installed:
there is no folder to look in, nothing to keep up to date, and they simply appear in the `/` menu
beside the ones you wrote. `use-shipped-skills:` (default `true`) is the switch that leaves them
out.

There are four, and they cover the working habits a model most often needs reminding of rather
than anything about apogee itself:

| Skill | What it is for |
| --- | --- |
| `debugging` | Chase a bug down in four steps — reproduce, isolate, fix the cause, verify |
| `planning` | Restate the goal, enumerate the steps, then execute one at a time |
| `code-review` | Correctness first, with a real trigger and a verified finding for every remark |
| `commit-hygiene` | One logical change per commit, a message that says why, checks run first |

They are attached the way any skill is — write `/debugging` in your message, or pick it from the
`/` menu — and [`/skills`](commands.md) lists them beside the ones you installed yourself.

The model can also reach one on its own, through a tool called `load_skill`. It takes a skill id,
or a few words describing the task when it does not know one, and gets back that skill's
instructions — or, when nothing clearly matches, a short list of ids to ask again with. It searches
the same catalogue you see in `/skills`, so a skill you wrote is as reachable as one apogee ships,
and nothing about the catalogue is put in front of the model until it asks: no ids, no summaries
and no bodies ride along in every request. The tool is on by default and is an ordinary entry in the
`tools:` roster near the top of this page — `tools.disabled: [load_skill]` closes the door,
globally or for one model.

```yaml
# ~/.apogee/config.yaml
use-shipped-skills: false
```

They are the **weakest** claim on a skill id in the system — below your global library, below the
project's folders, below everything on disk. A skill of the same id anywhere else wins, and the
shipped copy is recorded as shadowed rather than dropped, so writing your own `debugging` replaces
apogee's without your having to switch anything off. That is the same collision rule
[ADR 0032](../adr/0032-the-user-skill-library-outranks-the-workspace.md) states for the folders,
with the shipped source added at the bottom. `/skills export <id>` is the shortcut for exactly
that: it writes apogee's copy into `~/.apogee/skills/<id>/` for you to edit — see
[the commands page](commands.md#in-chat-commands-skills-and-file-references).

The flip is live, exactly like `use-project-skills:` above: commit the `use-shipped-skills` row in
`/settings`, or save the file, and the catalog is re-scanned in the session you are already in.
It is file-only — no flag, no environment variable — and it reads the same in every Driver, so a
`/skill` token in a [headless](headless.md) prompt or a [daemon](daemon.md) Firing resolves to the
same body your session resolves.

## Skill suggestions — `ui.skill-suggestions:`

A library grows past what anyone can recall, which is exactly where a menu you have to open stops
helping. While you type, apogee ranks the skill catalog against your draft and names the closest
three in a one-row band above the input box; `⇥` opens the `/` menu on exactly those. It is on by
default, and the flip is live: commit the `ui.skill-suggestions` row in `/settings`, or save the
file, and the band goes — or comes back — in the session you are already in. The keys, the
spent-once rule and what the row looks like are on the [commands
page](commands.md#suggested-skills).

```yaml
# ~/.apogee/config.yaml
ui:
  skill-suggestions: false
```

What the matcher reads is each skill's **id, display name, summary and `triggers:`** — never its
body. That is what keeps ranking a large library cheap, and it is also the whole of what suggestion
touches: the catalog stays on this side of the wire, and a skill reaches the model only when you
invoke it with `/id`
([ADR 0061](../adr/0061-skill-suggestions-are-driver-side-over-an-engine-matcher.md)).

`triggers:` is the lever a skill's author has over this. It is an optional top-level frontmatter
key — a YAML list, or one comma-separated string — naming the phrases somebody would actually type
when they want the skill:

```yaml
---
name: Brew release
description: Cut a release and publish it to a Homebrew tap.
triggers:
  - cut a release
  - publish to homebrew
  - bump the formula
---
```

Write them the way a request is worded, not the way the skill is titled: front-load the words a user
types, and keep each phrase to the words that carry the meaning. A phrase matches **whole words, in
order, side by side** once the small connecting words are set aside — `cut a release` hits "can you
cut the release for me" and does not hit "release notes" — so a phrase made only of common words
never hits at all. A hit does two things at once: it lifts the skill above ones matched by wording
alone, and it admits the skill on its own, with no other evidence required — the only floor left is
the general one, that a draft carry at least three words before anything is suggested.
Phrases are lowercased and their whitespace normalised, capped at 64 characters each and 32 to a
skill, and `/skills` lists them back so you can see what a skill declared.

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

Before Compaction is ever reached, **Pruning** clears the stale bulk out of the window.
A long session fills up with the *output* of tool calls the model has already read and
moved past — whole files, long searches, build logs — and that output crowds out the
conversation itself. When the history outgrows **60%** of its budgeted share, apogee
replaces the oldest and largest of those results with a one-line stub,
`[pruned: N lines from <tool> <argument> — re-run the call if you need it]`, until the
history is back under **40%**; the model can always re-run a call whose output it still
needs. The four most recent tool-calling turns are never touched, so the work in progress
is left alone, and nothing but tool output is ever pruned — your prompts, the replies and
the system content all stay. Each pass prints one line in the transcript saying how many
results went and roughly how many tokens that freed. Like Compaction it is structural — it
stays on even under `--bypass` — so it is on by default; set `prune-tool-results: false`
(a file-only key) to keep every result verbatim and manage the window yourself.

A **delegated sub-agent** runs its task in one exchange of its own, and how long that
exchange lasts is the sub-agent's call, not yours: it reads, greps and edits until it
decides it is finished, and a model that keeps deciding otherwise can spend an unbounded
number of tokens on a single delegation. `delegate-max-steps:` (a file-only key) is the
ceiling, counted in **turns** — one request plus the tools it asked for — after which
apogee ends the delegation cleanly and hands your agent what the sub-agent produced so
far, marked as partial. It does not cut the sub-agent off mid-sentence, though: first
it takes the tools away, tells the sub-agent why, and spends one further turn — an
extra one, outside the ceiling — asking it to sum up what it found and what it left
unfinished, so what your agent receives is a report rather than an interrupted sentence.
The default is **80**; `0` lets a delegation run unbounded, which is what it did before
this key existed. It bounds sub-agents only, never the session you are talking to. A
`sub_agent` call may ask for a lower ceiling of its own through its `max_steps` argument;
it can never raise this one.

The context **window** these budgets are measured against is discovered from the
server — live, not once: apogee asks every ten seconds, so switching the loaded model
under a running session re-binds the window with it. Set `context-window:` (a file-only
key, in tokens) only when your server does not advertise a window, or when its number is
wrong for how you run it; that key is a **pin** the heartbeat never overrides. It takes a
whole number of tokens: a fractional or negative value — and a whole number written with a
decimal point or an exponent, `65536.0` or `1e3` — is refused when the config loads, rather
than rounded into a window you did not write. With no
window known, the Budget and automatic compaction stay inactive and apogee says so in the
transcript the moment it binds a model without one. How that window is **split** is a second
file-only key: apogee holds a fifth of it back for the model's reply and lets the prompt fill
the rest, and `response-reserve:` (a fraction above 0 and below 1) sets your own share instead
— raise it for a model that answers at length, lower it to spend more of the window on history.
The same key on a `servers:` entry sets that share for one server only and outranks the
top-level one while the session is on it, following a `/server` switch, a scheduled run and a
delegation onto that server the way its `context-window:` pin does.

How much of that window you actually **work in** is a separate number. Every guard apogee
runs scales with the window — the budget it allocates, the tool output it lets through, the
point it decides to compact — so a model advertising a very large one makes all of them
expensive at once, and a single delegated task can re-send hundreds of thousands of tokens
on every step. `working-window:` (a file-only key, in tokens) bounds the room those guards
work in without changing what the model can hold: apogee budgets, caps and compacts inside
your number, while the advertised window still says when a request genuinely will not fit.
The reply ceiling follows your number too — it is derived from the share of the working room
held back for the reply — so bounding the room also stops a big-window model deriving the
same maximum reply every time. On a 1M-window model, `working-window: 200000` is a sane
place to start. Leave it unset (`0`) and the working room is the whole advertised window,
which is what apogee always did. The same key on a `servers:` entry bounds that one server's
room and outranks the top-level one while the session is on it — following a `/server`
switch, a scheduled run and a delegation exactly as the two keys above do — and it may not
exceed that entry's own `context-window:` pin, which is the roof it sits under.

Every reply is **bounded**, and by the same budget: apogee tells the server how many tokens
one answer may take, using the room it already reserves for the reply — clamped to between
4,096 and 32,768 tokens, and to the floor when no window is known. Without that ceiling a
thinking model can reason for an hour and hit the context wall instead of answering. Set
`max-output-tokens:` on a `servers:` entry (in tokens) to pin your own ceiling for that
server, whatever its window says — which is how you let a cloud endpoint that advertises no
window answer at length. A reply that runs into that ceiling with nothing visible to show
for it fails the turn and names the cap and roughly what the reasoning cost, rather than
reporting an empty reply: the remedy is a bigger ceiling or a smaller task, not a retry. A
**sub-agent** is held to a stricter rule, because its answer is read by a model rather than by
you: a delegate's reply that runs into the ceiling without asking for a tool fails the turn
even when it carries text, and the delegating agent is told the cap was the cause — a
truncated answer is never passed back as the delegated result.

**How hard a model thinks** is a property of the model, so it rides its profile: a
`model-profiles:` entry's `thinking:` block takes `effort:` — `off`, `low`, `medium` and
`high`, plus the wider levels some servers report: `minimal`, `xhigh`, `max`, and `none`
(which is how the OpenRouter wire spells `off`). Apogee forwards that to the server,
which is where a reasoning model's dial actually lives:

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
reasoning *arrives*; a value outside those eight is a startup error, and a server that
rejects an effort it does not support fails the turn with a message naming this key.

**Apogee works out for itself whether the model has a dial at all**, from what the
heartbeat already asks the server — a llama.cpp chat template that reads the kwarg, or a
`/v1/models` entry naming that model's own levels. Nothing extra is asked for. When there
is a dial, the effort the next request will carry shows in the status footer and
`/effort` opens a **picker** of the levels this model reports; when there is none, the
footer segment is gone and `/effort` is not offered in the command menu — type it anyway
and it tells you the model reports no dial. Picking a level layers a **session override**
on top of the profile, and the picker's `auto` row drops it again. That override is
session intent, not configuration: it is never written to the file and stays on the
primary loop — a delegated sub-agent resolves effort from its own profile — and it
survives a model switch, unless the model you switch to reports a set of levels that does
not include it, in which case it is cleared and the transcript says so.

**Three different wires carry the same dial**, and apogee picks between them from what it
detected: llama.cpp and other template-driven servers take `chat_template_kwargs`,
OpenRouter takes a `reasoning` object, and OpenAI's own reasoning endpoints (o-series,
gpt-5) and Groq take a top-level `reasoning_effort` field. That third one cannot be
detected — those endpoints advertise nothing that gives it away — and neither can a
self-hosted vLLM, SGLang or TGI, which honours the template kwargs but serves no `/props`.
Reach for `effort-dialect:` on the `servers:` entry when you are on a provider like that:

```yaml
# ~/.apogee/config.yaml
servers:
  - name: openai
    endpoint: https://api.openai.com/v1
    api-key-env: OPENAI_API_KEY
    effort-dialect: openai
```

`auto` — the default — detects as above. `kwargs`, `reasoning` and `openai` each force one
of the three wires, and also tell apogee the dial exists, so the picker and the footer
come back on a server it could not read. `off` forces the opposite: never send an effort
at all, the escape hatch for a server that errors on the kwarg. Anything else is a startup
error naming the entry and the key. See
[ADR 0060](../adr/0060-effort-is-detected-passively-dialected-per-server-and-picked.md).

A model that thinks for a long time can also go silent. `ui.stall-after` — a duration
written the way Go spells one (`90s`, `2m`), default `90s`, `0` turns it off — sets how
long the engine may say nothing before the status line adds a warning-tinted `quiet`
qualifier to its running phrase: `thinking · quiet · 12m`. It reports a fact, not a
verdict — nothing has arrived in that long — and any engine event clears it.

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

## The servers you run models on

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
`api-key-env` — exactly one of the three), `model`, `parallel-agents`,
`working-window` (the room a session on that server works in, above)
and `effort-dialect` (which of the three wires carries the
thinking-effort dial, described above) are optional, as are `description` — free
text saying what that server is **for**, which the `/sub-agents-server` picker
shows and which the model reads when you let it pick the seat
([below](#letting-the-model-pick-the-seat)) — and `llama-launcher`,
which lets apogee start, switch and stop that server itself — [below](#local-servers--llama-launcher).

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
switch carries the new server's cap into the next firing. A `/model` profile load that
moves the session arrives at the new server's cap the same way; because the entry a
profile load builds pins nothing, delegations run one at a time there until that
server's own first heartbeat says how many slots it has.

**Delegations can run on a server of their own.** The root `sub-agents-server:`
key names the entry every delegation runs on: your conversation stays on the
session's server while sub-agents fill that one, and each delegated run says
which model it ran on. Any entry may be named, the one this session is on
included, and leaving the key unset is what apogee did before it existed —
delegations share the session's server. If the named server's API key cannot be
resolved, delegations fall back to the session's own server and the reason is
reported once. A name no entry carries is not refused either: apogee says which
name went missing, lists the names your file does carry, and routes to the
session's server.

    sub-agents: no servers entry named "rented-bx" — delegations run on the session server (configured: workstation, rented-box)

**And they move mid-session.** `/sub-agents-server` opens a picker over your
entries — `/sub-agents-server <name>` takes one straight away — and every
delegation spawned after the pick runs there; sub-agents already working stay on
the server they started on. The pick is written back into the file as
`sub-agents-server: <name>`, the way `/server` records its own choice, so your
next session delegates to the same place without being asked. Unlike the file's key, a name the picker
does not know is refused and nothing moves. It is also the only way the key
changes from inside apogee: the `sub-agents-server` row on the
[settings screen](commands.md#the-settings-screen--settings) is read-only — it
reads `auto (session server)` while the key is unset — because `⏎` on a server
row switches the *session's* upstream, which is the one thing this key does not
do.

**And the last row is the way back out.** Under your entries the picker offers
one more row — `auto`, whose second cell reads
`— no routing; delegations run on this session's own server` — and it names no
entry: taking it, or typing `/sub-agents-server auto`, points the delegations
back at the server this session is itself talking to and *removes* the
`sub-agents-server:` key from the file, so the opt-out survives a restart the
same way a pick does. The line it writes says which write it was:

    sub-agents server: auto · this session's own server · sub-agents-server: cleared

where a named pick reads `· sub-agents-server: saved` instead — the two are
opposites and never interchangeable. A `servers:` entry you actually named
`auto` still wins in both forms: the entries are your file's, the row is the
picker's, and the file answers first.

Sub-agents there also ask for thinking effort the way that server understands
it — the entry's `effort-dialect:` if it names one, else whatever the server
advertises. A target that advertises no dialect and pins none is the
one gap: its delegates keep speaking the *session* server's dialect, so a
sub-agent's request to think less can go out in a shape the target server
never reads — most visibly when a long sub-agent run compacts and its summary
comes back as reasoning and nothing else. apogee tells you once when it routes
there:

    sub-agents: rented-box advertises no thinking-effort dialect — delegates there speak this session's; set effort-dialect: on its entry

Adding `effort-dialect:` to that entry — the same key, the same
values as above — is the fix.

**A config that still flags an entry is offered the move.** Earlier builds marked
the delegation target with a `sub-agents:` flag on the entry itself. Nothing reads
that flag any more, so a file still carrying it would quietly delegate to the
session's own server; where apogee finds one it says so at start-up and offers
one edit — `move it` writes `sub-agents-server: <name>`, drops the retired flag,
and re-points this session's delegations at that entry there and then. `not now`
leaves the file exactly as it is and the offer comes back at the next start-up.
An unattended `apogee headless` run never prompts, so it gets a stderr notice
naming the entries instead — the same bargain a plaintext key gets:

    apogee: cheaper still carries the retired sub-agents: true flag in ~/.apogee/config.yaml, and headless runs never prompt, so apogee cannot offer to migrate it. The flag no longer routes anything — set sub-agents-server: <entry> at the root of the file and drop the flag.

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
[ADR 0048](../adr/0048-apogee-remembers-the-model-choice-per-server.md).

**The first run asks.** With `server:` unset, apogee starts with **no server
bound** — no engine constructed, nothing pointed anywhere — opens the `/server`
picker over your entries, and records what you choose. A `server:` naming an entry
that is gone is handled the same way: apogee says which name went missing and opens
the picker, rather than refusing to start. With the list empty it opens
[`/settings`](commands.md#the-settings-screen--settings) instead and points you back at this
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

### Letting the model pick the seat

Everything above settles where delegations run before the session starts. The
root `sub-agents-choice:` key can hand that decision to the top-level model
instead, one delegation at a time.

```yaml
# ~/.apogee/config.yaml
servers:
  - name: workstation
    endpoint: http://192.168.64.1:1111
    model: gpt-oss-20b
    description: the big local model, for review, design and ambiguous investigation
  - name: grunt-box
    endpoint: http://192.168.64.9:1111
    model: qwen3-4b
    description: small and fast, good at greps, file reads and mechanical edits

server: workstation
sub-agents-server: grunt-box
sub-agents-choice: model      # fixed (the default) or model
```

`fixed` is the default and is what apogee did before this key existed:
`sub-agents-server:` decides, and every delegation goes where it points. `model`
adds one optional argument to the sub-agent tool — `run_on`, which takes
`session` (this session's own server) or `sub-agents-server` (the entry
`sub-agents-server:` names). A call that says nothing is unchanged: it runs
wherever `sub-agents-server:` would have sent it, so turning the key on moves
nothing by itself. The `sub-agents-choice` row on the
[settings screen](commands.md#the-settings-screen--settings) switches it live,
and the change applies to the next request.

The choice is offered to the **top-level** model only. A sub-agent's own tool
never carries `run_on`, so a delegation's own delegations stay where their
parent ran.

**The model chooses from what you wrote.** With `model` on, apogee adds one line
to the host orientation it already sends, describing both places a delegation can
go in the same words — the model, the entry's name, and the entry's
`description:`:

    - Delegations: run_on "session" = gpt-oss-20b on workstation — the big local model, for review, design and ambiguous investigation; run_on "sub-agents-server" = qwen3-4b on grunt-box — small and fast, good at greps, file reads and mechanical edits; unset = sub-agents-server. Keep judgment-heavy sub-tasks (review, design, ambiguous investigation) on the stronger seat and send mechanical ones (search, mechanical edits, running tests) to the other.

`description:` is the only part of that line you write, and it is why the key is
worth having: a choice between two bare names is a coin toss. The line carries
**no availability state** and moves only where you move it — `/server`,
`/model`, `/sub-agents-server` — so a server going down, or coming back, never
rewrites the system prompt mid-session.

**When the far seat is not there.** A delegation that asked for
`sub-agents-server` while there is no usable target — the server is down, its key
will not resolve, the name matches no entry — still runs. It falls back to the
session's own server, exactly as a fixed route does, and its result gains one
last line so the model that made the choice can see it was overruled:

    note: ran on the session server — the sub-agents server was unavailable

You are told the same thing once, by the routing notice `sub-agents-server:`
already prints; the note is for the model, on the result of the call that asked.

**Two seats in one reply.** When one reply's delegations land on both servers,
apogee runs that batch at the smaller of the two `parallel-agents` caps, so
neither box is oversubscribed. A reply whose delegations all land on one seat
runs at that seat's own cap, exactly as above.

See [ADR 0069](../adr/0069-the-top-level-model-picks-the-delegation-seat.md).

## The upstream API key

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
the command's stdout, trailing whitespace trimmed, is the key. Its **program is resolved
before it runs and refused if it lands inside the workspace**, the way apogee fences every
program it executes: the config file is yours, the workspace is the model's, and a key
command sitting in the latter would hand the model the credential the key source exists to
protect. So a wrapper script of your own belongs outside the workspace, or is named by an
absolute path. `api-key-env:` names an
environment variable rather than holding a key (`api-key-env: OPENROUTER_API_KEY`), read
from the environment apogee itself was started in — and dropped from the environment the
`terminal`, `python_exec`, `run_tests` and `console_open` tools hand a subprocess, so a command
the model chose cannot read that key back out. Both resolve the first time this session actually
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

## Local servers — llama-launcher

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
[the launcher design record](../adr/0029-the-launcher-actuates-local-servers-and-the-beat-completes-every-move.md).

## The system prompt

The system prompt is the standing instruction apogee sends ahead of your first
message, as the first system message of every request.

**With nothing configured, apogee sends its own default prompt** — base steering
built into the binary, so every upgrade brings you the current text instead of a
copy frozen on the day you installed. Nothing needs uncommenting to get it.

Four rungs, first hit wins:

1. a `system-prompt-models:` entry matching the model this session is bound to;
2. the top-level `system-prompt-text:` or `system-prompt-file:`;
3. apogee's **built-in default**, unless `use-default-prompt: false`;
4. no system prompt at all.

Whatever hits **replaces** everything below it, whole: your prompt is never merged
with apogee's, and half a prompt is never sent. So `use-default-prompt:` (default
`true`) matters in exactly one case — nothing configured — where `false` is how you
ask for the promptless run:

```yaml
# ~/.apogee/config.yaml
use-default-prompt: false
```

**Layering.** Beside that ladder sits one additive channel: `system-prompt-layers:`, an
ordered list of fragments appended to whatever the ladder picked. The rung that wins still
supplies the whole prompt — layers are not a fifth rung and replace nothing; they simply
follow it, in the order you wrote them, joined by a blank line. They are opt-in and
unconditional: nothing is appended unless you list it, and everything you list is sent,
whichever rung won. A `system-prompt-models:` entry replaces the selected prompt but never
the layers. Layers with no prompt configured are sent on their own, and do **not** bring
the built-in default along with them — the default stays what it is, the fallback for a
prompt you configured nothing for at all.

Each entry states exactly one of `text:` or `file:`; both in one entry, or neither, is a
startup error naming the offending index:

```yaml
# ~/.apogee/config.yaml
system-prompt-layers:
  - text: "This box has no network. Prefer rg over grep."
  - file: house-conventions.md
```

A `file:` resolves exactly as `system-prompt-file` does — a leading `~` expands, and a
relative path resolves against your apogee home rather than the workspace. Unlike a
`system-prompt-models:` entry you are not running, a layer's file is read on every run
that resolves the prompt, so a missing one is a startup error naming the index and the
path. Layers are templates in the same closed placeholder language as the prompt itself
(below). See
[the layering record](../adr/0067-system-prompt-layers-are-an-explicit-additive-channel.md).

An existing `~/.apogee/config.yaml` that carries a `system-prompt-text:` of its own
keeps it: that is rung 2, and it wins, so nothing changes under a setup you already
run. To start from apogee's text instead, open `system-prompt-text` in `/settings`
with nothing configured — the built-in prompt is already in the editor, yours to
change and save. There is deliberately no export command for it: a second copy on
disk would freeze again exactly what building it in unfroze.

Whenever a system message goes out at all — because you have a prompt, or workspace
context files (below), or both — apogee places its own short **orientation block**
right after your prompt, ahead of any workspace context files, naming the workspace,
this session's scratch directory and any read-only library roots the model may read
from. Ahead of them is deliberate: nothing a repository ships can then precede the
host's own facts. That block is not part of `system-prompt-text`, cannot be edited out
of it, and is not sent when you have configured neither a prompt nor context files.

```yaml
# ~/.apogee/config.yaml
# Your own prompt, replacing apogee's built-in one whole:
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

Four placeholders are substituted fresh on every request: `{{workspace}}` (the
workspace path), `{{datetime}}` (today's **date** — not a timestamp, which would
change the prompt every turn and throw away your server's prefix cache),
`{{mode}}` (the autonomy mode, so a Shift+Tab shows up from the next request on),
and `{{scratch}}` (this session's scratch directory). The spelling is strict and
the set is closed — anything else in double braces, `{{ workspace }}` included,
is a startup error listing the four.

A `system-prompt-models:` entry keyed by the model name apogee resolves at startup
**replaces** the global prompt for that model, whole; an entry naming a model you
are not running is simply inert — never selected, its file never read — so one
config can carry a prompt per model across every machine it travels to. Sub-agents
inherit your prompt; apogee's own internal calls (the conversation summariser,
`apogee probe`'s battery) keep their dedicated prompts and never see it; and the
prompt never enters your conversation history or a saved session.

Beside your prompt, apogee folds in the **project's own** standing text: it looks
for `AGENTS.md` in the workspace root at the start of every session and adds it to
that same first system message, between a header and a footer naming the file — so a
repo that already keeps one for other agents is picked up with nothing to configure.
The file-only `context-files:` block is where you change that: `names:` is the list of
names to look for (all of the ones that exist are included, in your order) and
`enable: false` turns the whole thing off. A name that is not there is skipped
silently — one config travels across repos that carry different files, or none —
and a file that is present but unreadable is reported in the transcript rather than
stopping apogee. The content goes out **verbatim**: the placeholders above do not
apply to it, so a repo's own `{{braces}}` can never fail your startup. The one line
apogee touches is one that spells its own headers: a content line beginning with a
`## Workspace context:` / `## End of workspace context:` header or with the
orientation block's first line is sent behind a `[workspace text] ` prefix, so a file
cannot pass its own prose off as apogee's. The files are read when a session starts
and re-read on `/clear`, `/new`, or a resume, never
mid-conversation, so editing `AGENTS.md` while apogee runs takes effect on your
next `/new`; apogee names each file it loaded, with its size, and warns you (never
truncates) when the standing content outgrows its share of the context window.

## Showing a finished document

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
a repo you cloned — and a `file://` launch can carry no policy to stop it. **A document
that can carry code counts too**: the OpenDocument formats (`.odt`, `.ods`, `.odp`) hold
Basic macros with no macro-free variant to tell them apart, and an `.epub` is a zip of
web pages its reader may run, so those are left as a path as well; `.docx`, `.xlsx`,
`.pptx`, `.pdf` and `.rtf` still open, because there the extension itself states the file
carries no macro. Over SSH — a
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

## Auto mode's blast radius

Auto is the one unsupervised mode, so it is fenced: filesystem writes are confined to
the workspace at the OS level, the network is open, and MCP still asks. All three
platforms have a backend — landlock on Linux, `sandbox-exec` on macOS, a restricted
low-integrity token on Windows. Where the OS cannot fence a command — a Windows build
older than 10 1809 (17763), and most containers, where landlock reports `ENOSYS`
regardless of kernel version — Auto keeps the promise the honest way and asks before
each shell call instead of running it unbounded ("confine if you can, gate if you
can't"). That is not a fault, so Apogee says so at startup rather than letting Auto
look broken.

One Linux fence is real but incomplete: on a kernel older than **6.2** (landlock ABI 1–2 —
Ubuntu 22.04, Debian 12, RHEL 9) the kernel has no way to restrict *truncation*, so a confined
command still cannot create or write a file outside the workspace but can empty one that is
already there. Auto is still fenced and still eligible; Apogee names the gap rather than
implying a fence it does not have — `apogee probe` and `/confine` show it as
`unfenced: truncate(2)` on the backend line, and Auto says it once at startup. A kernel 6.2 or
newer closes it; until then, treat Auto's fence as create-and-write only.

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
Auto unconfined **for this session** and writes nothing — a Schedule that fires from
inside that session still runs with the fence the session started with, and `--save` is
the route for later sessions' Firings. `/confine off --save` also records this machine
in `~/.apogee/config.yaml`, comments and formatting intact:

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

`/settings` states the same blast radius where an escalation actually happens: the mode
row's `auto` value carries that sentence beside it in the value list — before the ⏎ that
takes the rung — and repeats it as the row's note once the escalation lands.

The footer says it while Auto runs. On the auto rung the mode marker at the footer's right
edge carries a second word for the blast radius: **`confined`** — commands run fenced to the
workspace by a backend that can enforce it — **`gated`**, where the backend cannot fence, so
each terminal command asks for approval instead, and **`unconfined`**, painted in the error
colour, where Auto runs every command with your full privileges. The word is the live setting,
so a `/confine off` or `/confine on` shows up in the footer the moment it takes; the three
lower rungs carry no word at all, because the flag is read by auto mode only. `/confine` is
where it changes, and `/confine status` is the long form of the same fact.

## The Console family

Nearly every tool apogee gives a model is one shot: `terminal` runs a command, the command ends,
the process is gone — nothing of it is left to talk to. The **Console family** is the exception.
`console_open` starts an interactive program under a pseudo-terminal and *leaves it running*, so a
Python REPL keeps its imports, a shell keeps the directory it was `cd`'d into, and a dev server
keeps serving while the model goes on working around it. The open answers with an id — `console 3`
— and the other three tools drive that id: `console_send` types into it (a line of input, or raw
bytes for a control key like Ctrl-C), `console_read` collects whatever it has printed since the
last look, and `console_close` ends it and reports how it exited. What the model gets back is the
program's output with terminal escape sequences stripped out, so a progress bar that repaints
itself a thousand times does not spend the context window on cursor motion nobody can see.

The four tools ship **off**, which is what the `tools:` block above is for: name them under
`enabled:` and every model this config runs is offered them, or put them on one model's roster with
a `model-profiles:` entry — as the built-in table already does for `qwen3.8`. They are off because
four extra menu slots are a real cost to a small model, and a model that never needs an interactive
program should not pay for them.

**A Console is live host state, not part of your conversation.** It belongs to the running apogee
process and to nothing else: it is never written into a session file, so a session you resume — or
one restored on another machine — comes back with no Consoles at all, and an id the model remembers
from before is simply an id this process does not know (`no console 3 (open consoles: 1, 2)`).
`/clear` — and its `/new` alias — closes every open Console along with the history that named them,
quitting apogee closes them, and the Consoles a sub-agent opened close when its delegation ends.
Nothing here is undone by `/undo` either — a Console that dropped your database table dropped it
for real.

**Four at a time.** One apogee process holds at most **4** open Consoles — a fixed number, not a
setting — and a fifth open is refused, naming the ids that could be closed instead. A program that
has already exited still holds its slot until it is closed, because it still holds an id and the
output nobody has read yet. The cap is what keeps a forgotten dev server from quietly outliving the
task that started it.

**Approval and the fence are decided per call, never per Console.** Opening one and typing into it
count as command execution, exactly like `terminal`: they are what the mode gates in Ask-Before,
and what the OS fence confines in Auto. The Resolution is taken again for every single
`console_send`, so the mode a Console was opened under never becomes a standing permission —
switching to Plan, or `/confine on`, changes what the *next* send is allowed to do, though neither
reaches back to touch a process that is already running. If the fence stops a confined Console's
program mid-run, the next read says so in the same words the one-shot tools use (`[blocked by
workspace confinement: …]`). Reading and closing ask nobody: they run in every mode, because
neither one can start anything.

**POSIX only, for now.** Consoles need a pseudo-terminal, and apogee has one on macOS, Linux and
the BSDs. On Windows the four tools are still on the menu — so a config that enables them is not a
startup notice about tools that do not exist — but `console_open` answers `console is not supported
on Windows yet` rather than pretending. ConPTY support is a later change.
