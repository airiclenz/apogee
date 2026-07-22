package platform

import (
	"os"
	"strings"
)

// hostRules is the per-OS behaviour table behind Host: the shell argv prefix, the
// platform-essential environment variables, and the one flag (windows) that switches
// path syntax, path case-folding and quoting style.
//
// Both rule sets are compiled on EVERY target — only Current's choice of rule set is
// build-tagged — so Windows semantics are table-testable from a Linux or macOS test run
// (the injected-seam pattern internal/present established: the OS is a parameter, never
// an ambient runtime.GOOS read). The native Windows run is additional proof, not the
// only proof.
type hostRules struct {
	// windows selects Windows semantics: backslash-separated, drive-lettered,
	// case-insensitive paths; cmd.exe quoting; and a verbatim process command line.
	windows bool
	// shell is the argv prefix that hands a command line to the platform shell —
	// {"sh", "-c"} on POSIX, {"cmd", "/c"} on Windows.
	shell []string
	// envKeys are the variables a process on this platform needs in order to start and
	// behave normally, over and above whatever allowlist a caller names (see ScopeEnv).
	envKeys []string
	// longPath resolves an 8.3 short path ("C:\PROGRA~1") to its long form. It is nil in
	// the pure rule sets — the shared tests inject a deterministic fake — and wired to the
	// real OS resolver by Current on Windows. A nil resolver means short names are simply
	// not comparable, which Contains reports as "not contained" rather than guessing.
	longPath func(string) string
}

// posixRules is the POSIX rule set (Linux, macOS and the other Unix targets): `sh -c`,
// exact-case slash-separated paths, and no platform-essential environment variables — a
// POSIX process needs only what its caller allowlists (PATH, HOME and friends are the
// caller's policy, not the platform's floor).
func posixRules() hostRules {
	return hostRules{
		windows: false,
		shell:   []string{"sh", "-c"},
		envKeys: nil,
	}
}

// windowsRules is the Windows rule set: `cmd /c`, case-insensitive backslash
// paths, and the environment floor below which ordinary Windows programs misbehave in
// ways that look nothing like a missing variable (a child without %SystemRoot% cannot
// initialise Winsock or CryptoAPI; git without %HOMEDRIVE%/%USERPROFILE% cannot find the
// user's config; anything without %PATHEXT% cannot resolve a .cmd shim).
//
// The returned rules carry no long-path resolver, so they are pure: Current supplies the
// OS resolver on a real Windows host, and the shared tests inject a fake.
func windowsRules() hostRules {
	return hostRules{
		windows: true,
		shell:   []string{"cmd", "/c"},
		envKeys: []string{
			"SystemRoot", "SystemDrive", "windir", "ComSpec", "PATHEXT",
			"TEMP", "TMP", "USERPROFILE", "HOMEDRIVE", "HOMEPATH",
			"APPDATA", "LOCALAPPDATA", "PROGRAMDATA",
			"USERNAME", "COMPUTERNAME",
			"NUMBER_OF_PROCESSORS", "PROCESSOR_ARCHITECTURE", "OS",
		},
	}
}

// Command returns the argv that runs line through the platform shell.
func (r hostRules) Command(line string) []string {
	argv := make([]string, 0, len(r.shell)+1)
	argv = append(argv, r.shell...)
	return append(argv, line)
}

// CommandLine returns the verbatim process command line Command's argv must be launched
// with on this platform, or "" when the platform's own argv joining is faithful (POSIX).
func (r hostRules) CommandLine(line string) string {
	if !r.windows {
		return ""
	}
	return strings.Join(r.shell, " ") + " " + line
}

// Quote returns arg quoted so the platform shell reads it as a single argument.
func (r hostRules) Quote(arg string) string {
	if r.windows {
		return windowsQuote(arg)
	}
	return posixQuote(arg)
}

// ScopeEnv returns the scoped environment for a subprocess: every key in keys that is
// present in the environment, in the order given, followed by this platform's essential
// variables that keys did not already name.
func (r hostRules) ScopeEnv(keys []string, lookup func(string) (string, bool)) []string {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	out := make([]string, 0, len(keys)+len(r.envKeys))
	seen := make(map[string]struct{}, len(keys)+len(r.envKeys))
	add := func(key string) {
		if key == "" {
			return
		}
		// Windows environment names are case-insensitive, so PATH and Path are one
		// variable and must not be emitted twice.
		fold := key
		if r.windows {
			fold = strings.ToUpper(key)
		}
		if _, dup := seen[fold]; dup {
			return
		}
		seen[fold] = struct{}{}
		if value, ok := lookup(key); ok {
			out = append(out, key+"="+value)
		}
	}
	for _, key := range keys {
		add(key)
	}
	for _, key := range r.envKeys {
		add(key)
	}
	return out
}

// Contains reports whether target is root itself or lies beneath it.
//
// The comparison is lexical and component-wise: both paths are normalised (separators
// unified, "." and ".." resolved, a Windows \\?\ long-path prefix stripped), then root's
// components must be a prefix of target's — so C:\Work2 is NOT inside C:\Work, which a
// plain string prefix test gets wrong. Component comparison is case-folded on Windows and
// exact on POSIX. A relative path is never contained in an absolute one (or vice versa),
// and an empty root contains nothing.
//
// It resolves no symlinks and touches the filesystem only to expand an 8.3 short name
// (and only when a component is actually shaped like one, and only where Current wired a
// resolver) — callers hand it absolute, already-resolved paths.
func (r hostRules) Contains(root, target string) bool {
	rootAnchor, rootParts, rootOK := r.split(root)
	targetAnchor, targetParts, targetOK := r.split(target)
	if !rootOK || !targetOK {
		return false
	}
	if !r.sameComponent(rootAnchor, targetAnchor) {
		return false
	}
	if len(targetParts) < len(rootParts) {
		return false
	}
	for i, part := range rootParts {
		if !r.sameComponent(part, targetParts[i]) {
			return false
		}
	}
	return true
}

// sameComponent compares two path components under this platform's case rules.
func (r hostRules) sameComponent(a, b string) bool {
	if r.windows {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// split normalises p and decomposes it into its anchor (the part that says where the path
// starts — "/" on POSIX, "C:" or `\\server\share` or `\` on Windows, "" for a relative
// path) and its remaining components with "." and ".." resolved. It reports ok=false for a
// path it cannot compare honestly: an empty path, a Windows device path (`\\.\PhysicalDrive0`),
// a drive-relative path ("C:work", which means "the current directory on C:" and is
// therefore not a location), or an unresolvable 8.3 short name.
func (r hostRules) split(p string) (anchor string, parts []string, ok bool) {
	if p == "" {
		return "", nil, false
	}
	sep := "/"
	if r.windows {
		sep = `\`
		p = strings.ReplaceAll(p, "/", sep)
		var stripped bool
		if p, stripped = stripLongPathPrefix(p); !stripped && strings.HasPrefix(p, `\\.\`) {
			return "", nil, false // a device namespace path is not a filesystem location
		}
		if hasShortName(p, sep) {
			if r.longPath == nil {
				return "", nil, false // an 8.3 name with no resolver: reject, never guess
			}
			p = strings.ReplaceAll(r.longPath(p), "/", sep)
			if hasShortName(p, sep) {
				return "", nil, false // the resolver could not expand it either
			}
		}
	}

	rest := p
	switch {
	case r.windows && strings.HasPrefix(p, sep+sep):
		// UNC: the anchor is \\server\share; anything shorter names no location.
		fields := splitNonEmpty(p[2:], sep)
		if len(fields) < 2 {
			return "", nil, false
		}
		anchor = sep + sep + fields[0] + sep + fields[1]
		rest = strings.Join(fields[2:], sep)
	case r.windows && len(p) >= 2 && p[1] == ':' && isDriveLetter(p[0]):
		if len(p) == 2 || p[2:3] != sep {
			return "", nil, false // "C:" or "C:work" — drive-relative, not a location
		}
		anchor, rest = p[:2], p[3:]
	case strings.HasPrefix(p, sep):
		anchor, rest = sep, p[1:]
	default:
		anchor = "" // relative
	}

	for _, field := range splitNonEmpty(rest, sep) {
		switch field {
		case ".":
		case "..":
			if len(parts) > 0 && parts[len(parts)-1] != ".." {
				parts = parts[:len(parts)-1]
			} else if anchor == "" {
				parts = append(parts, "..") // a relative path may climb above itself
			}
			// ".." at an absolute root is the root itself, as every OS resolves it.
		default:
			parts = append(parts, field)
		}
	}
	return anchor, parts, true
}

// stripLongPathPrefix removes the Windows \\?\ extended-length prefix, mapping the UNC
// form (`\\?\UNC\server\share`) back to its ordinary spelling so the two forms of one path
// compare equal. It reports whether a prefix was present.
func stripLongPathPrefix(p string) (string, bool) {
	const prefix = `\\?\`
	if !strings.HasPrefix(p, prefix) {
		return p, false
	}
	rest := p[len(prefix):]
	if unc := `UNC\`; len(rest) >= len(unc) && strings.EqualFold(rest[:len(unc)], unc) {
		return `\\` + rest[len(unc):], true
	}
	return rest, true
}

// hasShortName reports whether any component of p is shaped like an 8.3 short name, the
// one spelling of a Windows path that cannot be compared lexically: PROGRA~1 is "Program
// Files" on one machine and "Program Files (x86)" on the next.
func hasShortName(p, sep string) bool {
	for _, component := range splitNonEmpty(p, sep) {
		if isShortName(component) {
			return true
		}
	}
	return false
}

// isShortName reports whether name has the 8.3 alias shape: up to eight name characters, a
// tilde, a generation number, and an optional extension of up to three characters. A tilde
// alone does NOT qualify — "my~file.txt" is an ordinary long name, and treating it as an
// alias would refuse to compare a perfectly comparable path.
func isShortName(name string) bool {
	base, ext := name, ""
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		base, ext = name[:dot], name[dot+1:]
	}
	if len(ext) > 3 || strings.Contains(ext, ".") || len(base) > 8 {
		return false
	}
	tilde := strings.IndexByte(base, '~')
	if tilde <= 0 || tilde == len(base)-1 {
		return false
	}
	for i := tilde + 1; i < len(base); i++ {
		if base[i] < '0' || base[i] > '9' {
			return false
		}
	}
	return true
}

// splitNonEmpty splits s on sep, dropping the empty fields that repeated separators and a
// trailing separator produce.
func splitNonEmpty(s, sep string) []string {
	fields := strings.Split(s, sep)
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

// isDriveLetter reports whether c is an ASCII letter, the only legal Windows drive letter.
func isDriveLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// posixQuote wraps s in single quotes — the only POSIX quoting that is literal for every
// byte — closing, escaping and reopening around any embedded single quote.
func posixQuote(s string) string {
	out := make([]byte, 0, len(s)+2)
	out = append(out, '\'')
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			out = append(out, '\'', '\\', '\'', '\'')
			continue
		}
		out = append(out, s[i])
	}
	return string(append(out, '\''))
}

// windowsQuote returns s as one argument of a command line handed to cmd.exe.
//
// TWO parsers read that line and they agree on nothing. The child process splits it with
// CommandLineToArgvW, which counts backslashes: a run of n backslashes that immediately
// precedes a double quote is an escape run — 2n+1 backslashes yield n literal backslashes
// and a LITERAL quote, 2n yield n literal backslashes and a quote that opens or closes the
// argument. cmd.exe, in front of it, counts nothing: every quote toggles its own quote
// state, and the characters in cmdMetacharacters are syntax wherever that state is
// "outside".
//
// A value with no quote of its own needs nothing clever. One surrounding pair puts the
// whole token inside a single cmd-quoted region — so metacharacters are inert — and
// CommandLineToArgvW reads it back verbatim, provided a trailing backslash run is doubled
// so it escapes itself rather than the closing quote ("C:\dir\" would otherwise leave the
// argument open).
//
// A value that DOES contain a quote cannot stay inside one cmd-quoted region: the \" that
// CommandLineToArgvW requires for a literal quote is still just a quote to cmd, which
// toggles out and reads the REST of the token as its own syntax — a value carrying both a
// quote and an & would hand cmd a live command separator. Such a value is emitted
// caret-escaped instead: every metacharacter, quotes included, is prefixed with ^, so cmd
// never enters quote mode at all, strips the carets, and passes the child exactly the
// CommandLineToArgvW-correct string. That branch's output is meaningful only to cmd —
// which is the only place Command and CommandLine ever send it.
//
// It does NOT neutralise %VAR% — cmd expands variables before either parser sees the line
// and there is no in-line escape for it — so a caller embedding untrusted text is quoting
// a value cmd may still expand. Apogee's callers quote filesystem paths.
func windowsQuote(s string) string {
	quoted := windowsArgvQuote(s)
	if !strings.ContainsRune(s, '"') {
		return quoted
	}
	return caretEscape(quoted)
}

// windowsArgvQuote wraps s in double quotes under CommandLineToArgvW's backslash rules:
// ANY backslash run that immediately precedes a quote — an embedded one or the closing
// one — is doubled, and an embedded quote takes one further backslash on top. A run that
// precedes an ordinary character is not an escape run and is emitted as it stands.
func windowsArgvQuote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	backslashes := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			backslashes++
		case '"':
			b.WriteString(strings.Repeat(`\`, 2*backslashes+1))
			backslashes = 0
			b.WriteByte('"')
		default:
			b.WriteString(strings.Repeat(`\`, backslashes))
			backslashes = 0
			b.WriteByte(s[i])
		}
	}
	b.WriteString(strings.Repeat(`\`, 2*backslashes))
	b.WriteByte('"')
	return b.String()
}

// cmdMetacharacters are the bytes cmd.exe reads as syntax outside a quoted region. The
// double quote is one of them: it is the character that opens and closes such a region.
const cmdMetacharacters = `&|<>()^!"`

// caretEscape prefixes every cmd.exe metacharacter in line with ^, cmd's escape for the
// single character that follows it. With every quote escaped too, cmd never enters quote
// mode, so there is no "outside the quotes" for anything to leak into: cmd strips the
// carets and passes line on byte for byte.
func caretEscape(line string) string {
	var b strings.Builder
	b.Grow(len(line) + 8)
	for i := 0; i < len(line); i++ {
		if strings.IndexByte(cmdMetacharacters, line[i]) >= 0 {
			b.WriteByte('^')
		}
		b.WriteByte(line[i])
	}
	return b.String()
}

// The rule table must satisfy the Host contract at compile time, on every target.
var _ Host = hostRules{}
