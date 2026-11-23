package gitconfigkeys

import "strings"

// MergeDriverSource names a merge driver key, merge.<x>.driver. It is a member of [Source] that
// internal/gitexec answers differently from the rest: a merge driver runs only on a content
// merge, so the runner empties it on every call and refuses only a merge-capable argv instead of
// the whole repository. Which of the names it matches a `-c` override can empty is the runner's
// rule and stays there.
const MergeDriverSource = `merge\..*\.driver`

// Source is the one source string behind every "config name whose VALUE is a program git
// executes" pattern: an sshCommand, an editor, a pager, an askpass, a filter or merge or diff
// driver, a credential helper, a gpg.program, an ssh signing-key command, a proxy command, a
// remote helper (remote.<name>.vcs). internal/gitexec anchors it as its CommandConfigName — the
// repo-local probe that refuses a repository's git calls — and its tests hand that string to
// git's --get-regexp verbatim, so it must stay POSIX-ERE compatible: plain ( ) groups, no (?:
// and no (?i). A name it matches is spelled lowercase, so a caller lowers what it matches first.
const Source = `core\.(sshcommand|editor|pager|askpass|gitproxy|alternaterefscommand)|sequence\.editor|diff\.external|diff\..*\.(command|textconv)|` + MergeDriverSource + `|mergetool\..*\.cmd|difftool\..*\.cmd|filter\..*\.(clean|smudge|process)|credential\.helper|credential\..*\.helper|gpg\.program|gpg\..*\.program|gpg\.ssh\.defaultkeycommand|uploadpack\.packobjectshook|remote\..*\.(proxy|vcs)|pager\..*`

// WriteViewSuffix is what the shell write view (internal/security's GitCommandConfigName) adds
// to [Source]: the keys git runs, or reads more config through, without being asked — the hooks
// directory (core.hookspath), the fsmonitor hook (core.fsmonitor), the interactive diff filter
// (interactive.difffilter) and the include paths (include.path, includeif.<cond>.path). The
// probe leaves them out on purpose — gitexec's hardening options neutralise the first two on
// every call and its --includes listing already reads what an include names, so refusing them
// there would cost a repository its git tools for a key that reaches no program of apogee's —
// but a `git config` line that SETS one is a write into the repository's control plane all the
// same, run by the operator's next ordinary git. alias.* stays out: git runs an alias only when
// it is typed.
//
// It also names the remote, url, protocol and submodule keys that reach a program on a
// transport or a submodule run: remote.<n>.uploadpack and remote.<n>.receivepack (the program
// run on the far side of a local or ssh transport), protocol.allow and protocol.ext.allow (which
// open the ext:: transport, whose URL is a command line), a url.<base>.insteadof or
// url.<base>.pushinsteadof whose base is an ext:: URL (the only rewrite that names a program —
// a URL-form helper name cannot carry a slash), and submodule.<n>.update (a `!` value is a
// command). They stay out of [Source] because the git hosts neutralise each per verb —
// `-c protocol.ext.allow=never`, `--upload-pack=git-upload-pack`, `--receive-pack=git-receive-pack`,
// `-c submodule.recurse=false` with `--recurse-submodules=no` — but the operator's next ordinary
// git honours them. Other protocol.<x>.allow names and non-ext:: rewrites never match. The
// values that name no program after all are [ValueIsBenign]'s.
const WriteViewSuffix = `core\.hookspath|core\.fsmonitor|interactive\.difffilter|include\.path|includeif\..*\.path` +
	`|remote\..*\.(uploadpack|receivepack)|url\.ext::.*\.(insteadof|pushinsteadof)` +
	`|protocol\.allow|protocol\.ext\.allow|submodule\..*\.update`

// WriteViewSource is [Source] widened by [WriteViewSuffix]: every key a `git config` line can
// set to reach a program.
const WriteViewSource = Source + `|` + WriteViewSuffix

// Anchored wraps a source string so the compiled pattern matches a whole config name, never a
// part of one.
func Anchored(source string) string {
	return `^(` + source + `)$`
}

// ValueIsBenign reports whether setting the [WriteViewSuffix] key name to value names no
// program: a git-boolean core.fsmonitor (`true` turns on git's builtin daemon and `false` turns
// the hook off), a protocol.allow or protocol.ext.allow of `never` (which hardens the transport
// rather than opening it), or a submodule.<n>.update mode without a leading `!`. Under a removal
// form value is a value-pattern, so only the core.fsmonitor exemption — which predates the
// removal rule — still reads it. name is spelled lowercase.
func ValueIsBenign(name, value string, removal bool) bool {
	if name == "core.fsmonitor" {
		return gitBooleans[strings.ToLower(value)]
	}
	if removal {
		return false
	}
	if name == "protocol.allow" || name == "protocol.ext.allow" {
		return strings.ToLower(value) == "never"
	}
	if strings.HasPrefix(name, "submodule.") && strings.HasSuffix(name, ".update") {
		return !strings.HasPrefix(strings.TrimSpace(value), "!")
	}
	return false
}

// gitBooleans are the value spellings git reads as a boolean (git-config(1), "boolean"), lowered:
// git compares them case-insensitively.
var gitBooleans = map[string]bool{
	"true": true, "false": true, "yes": true, "no": true, "on": true, "off": true, "1": true, "0": true,
}
