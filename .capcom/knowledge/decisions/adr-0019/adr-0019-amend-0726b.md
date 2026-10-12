---
title: "ADR 0019 amendment of 2026-07-26: rung 1's Windows launch refuses a name cmd.exe would parse"
---

**Why now.** The amendment above bounds which *program* the extension selects; on Windows the launch
itself still travels through a **shell**. Rung 1's opener there is `cmd /c start "" <path>`: Go
joins that argv into one command line, quoting an argument only when it holds a space or a quote
(`syscall.EscapeArg`), and cmd.exe then **re-parses** the joined line — where `&`, `|`, `^`, `<`,
`>` and `%` are syntax. A model-written file named `report&calc&.html` in a space-free workspace
path therefore reads back as three commands, and the middle one runs — with the user's full
privileges, outside any confinement box, under an extension (`.html`) squarely inside the
allow-list, because the injection rides the rest of the **name**. Raised by the url-safety plan's
item 2 verifier (2026-07-26, item 17); the Windows opener ships unexercised, so this closes the
hole before it is live.

**(a) The bound.** On Windows — and only there — rung 1 refuses a path carrying any character
cmd.exe can read as its own grammar: the operators `&` `|` `^` `<` `>`; the expansions `%` (live
even inside double quotes) and `!` (live when a machine-wide registry key enables delayed
expansion); the quote (Go escapes an embedded `"` as `\"`, which cmd's parser does not honour, so
the two disagree about where the quoted region ends); the token delimiters `;` `,` `=` (an unquoted
path holding one splits into **two** `start` arguments, and `start` resolves its first argument
like a command name); and ASCII control characters. A refused path builds **no argv at all** and
reports the existing `ErrNoOpener`, so the ladder degrades to rung 0 exactly as it does for a
refused extension — the path is still presented, the tool result still reads `shown`, never an
error (§4's degrade, unchanged).

**(b) A space is not refused, and neither are parentheses.** Go double-quotes an argument holding a
space, and (a)'s set is exactly the set that stays live inside — or breaks out of — those quotes,
which is why refusing it suffices. Parentheses are literal to cmd mid-argument once (a)'s set is
gone, and `report(1).html` is a name real deliverables have.

**(c) The other rungs and the other desktops are untouched.** `open` and `xdg-open` receive the
path as one execve argument with no shell in between, so macOS and Linux need no name bound. Rung 3
stays unbounded (the previous amendment's (c) — a `present.command` names one application and is
launched without cmd.exe), rung 2 serves over HTTP and launches nothing, and the extension
allow-list stands unchanged beside this bound: the extension decides what the handler would *do*
with the file; the name bound decides whether cmd.exe would even hand the handler the file the
model named.
