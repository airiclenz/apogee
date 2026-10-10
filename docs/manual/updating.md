# Updating apogee — `apogee update`

apogee never replaces itself in the background. When the interactive TUI starts it asks
github.com whether a newer release is out (the [`update-check`](configuration.md#update-check) key
turns that off) and, when one is, the startup box's version row names it and the command that
upgrades the way you installed apogee:

```
v0.24.11 → v0.25.0 · brew upgrade apogee
```

Running that command is always up to you
([ADR 0097](../adr/0097-the-tui-checks-for-a-newer-release-and-names-the-upgrade-command.md)).

## Which command upgrades your install

| You installed apogee with | Upgrade with |
|---|---|
| Homebrew (`airiclenz/tap`) | `brew upgrade apogee` |
| Scoop (bucket `airiclenz/scoop-bucket`) | `scoop update apogee` |
| winget (`AiricLenz.Apogee`) | `winget upgrade AiricLenz.Apogee` |
| `go install …@latest` | `go install github.com/airiclenz/apogee/cmd/apogee@latest` |
| A source checkout (`make install`) | `git pull && make install` |
| A release archive, unpacked by hand | `apogee update` |

apogee tells these apart by where its own executable lives (symlinks resolved) and by how it was
built: a binary under a Homebrew Cellar, a Scoop `apps\apogee` directory or a winget package
directory belongs to that package manager; a binary built without a commit stamp came from
`go install`; one built from a checkout without the release stamp `make dist` sets is a source
build; and a stamped release binary anywhere else came from an archive.

## `apogee update`

```
apogee update            # ask, then replace a release-archive install
apogee update --yes      # replace without asking (for scripts)
apogee update --check    # only say whether a newer release exists
```

**Only a release-archive install is apogee's to replace.** On any other install the command changes
nothing: it prints the channel and its upgrade command and exits 1 —

```
apogee was installed via Homebrew — run: brew upgrade apogee
```

— and it never runs a package manager for you, because that manager owns the binary and its
records.

On a release-archive install it looks up the latest published release. When you already run it,
it prints `apogee v0.25.0 is up to date` and exits 0. Otherwise it asks
`Update v0.24.11 → v0.25.0? [y/N]` — anything but `y` or `yes` cancels — and then:

1. downloads the archive for your platform and the release's `SHA256SUMS`, and refuses an archive
   whose checksum does not match;
2. unpacks the binary into a temporary directory beside the one it replaces;
3. runs the new binary's `--version` and requires it to report the release it asked for;
4. swaps it into place.

Any failure before the swap leaves your installed binary exactly as it was. Without `--yes` the
command needs a terminal to ask on: with stdin piped or redirected it refuses and exits 1, so a
script states its intent with `--yes`. `--check` reports `apogee v0.24.11 → v0.25.0 is available`
and downloads and writes nothing.

The directory the binary lives in must be writable by you — the new binary is staged there so the
swap is a rename. On Linux and macOS the rename replaces the binary in one step. Windows does not
let a running program be replaced, so the old `apogee.exe` is renamed to `apogee.exe.old` first
(and renamed back if the swap fails); the next apogee start deletes that leftover.

The download is verified against the checksums published with the same release, which catches a
corrupt or truncated download but is not a signature: the release binaries are not code-signed
yet.
