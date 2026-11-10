package security

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// SecretsRuleID names the dangerous-action rule that forces an approval look on a git_commit
// whose staged content or staged file names carry secret material (ADR 0080). It is the ID the
// pop-up and the audit trail report, so it reads like the built-in rule IDs in rules.go.
const SecretsRuleID = "commit-secrets"

// SecretFinding is one (path, class) pair the scanner flagged: the staged file and the kind
// of secret it appears to carry. Class is the human-facing noun the Hint prints — one of the
// content classes below, or secretPathClass when the file NAME is what gives it away.
type SecretFinding struct {
	Path  string
	Class string
}

// secretPathClass is the class of a finding made on a file's name alone — a key or env file
// staged regardless of what its bytes look like.
const secretPathClass = "secret-bearing file name"

// secretContentPattern pairs a class with the pattern that detects it on one added line.
type secretContentPattern struct {
	class   string
	pattern *regexp.Regexp
}

// secretContentPatterns is the built-in content floor, precision over recall: each pattern is
// a vendor-fixed prefix or a structural shape that ordinary source text does not produce, so a
// hit is a secret often enough to be worth an approval look and rarely enough that the look
// stays meaningful. The order is the order classes are tried on a line; a line matching two
// classes yields both, deduped per (path, class) by the caller.
var secretContentPatterns = []secretContentPattern{
	{class: "private key", pattern: regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{class: "AWS access key id", pattern: regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{class: "GitHub token", pattern: regexp.MustCompile(
		`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,}\b|\bgithub_pat_[A-Za-z0-9_]{22,}\b`)},
	{class: "Slack token", pattern: regexp.MustCompile(`\bxox[abpr]-[A-Za-z0-9-]{10,}\b`)},
	{class: "JWT", pattern: regexp.MustCompile(
		`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)},
}

// secretPathGlobs are the basename globs that flag a staged file by its name alone. They are
// matched with path.Match against the basename only, so a directory named `.env` or `keys`
// never counts — the file staged under it is judged on its own name.
var secretPathGlobs = []string{
	"*.pem", "*.key", "*.p12", "*.pfx",
	"id_rsa*", "id_ed25519*", "id_ecdsa*",
	".env", ".env.*",
}

// secretPathExceptions are the basenames the `.env.*` glob would take that are, by convention,
// the committed TEMPLATE of an env file rather than the file itself.
var secretPathExceptions = map[string]bool{
	".env.example":  true,
	".env.sample":   true,
	".env.template": true,
}

// publicKeySuffix is the suffix of an OpenSSH PUBLIC key file. `id_rsa*` and its siblings
// would otherwise take `id_rsa.pub`, and a public key is exactly what the ratified call never
// flags — so a basename ending in it matches NO glob, whatever else its name says.
const publicKeySuffix = ".pub"

// addedFileHeader is the unified-diff header that names the file the following `+` lines were
// added to. Git writes it as `+++ b/<path>`, C-quoting a path with unusual bytes and appending a
// tab after one containing spaces; a deleted file's header reads `+++ /dev/null` and carries no
// added lines, so it only needs to be recognised as a header and not as content.
const addedFileHeader = "+++ "

// removedFileHeader, fileDiffHeader and hunkHeader are the other unified-diff lines that decide
// whether a `+++ ` line is a header: git writes `+++ b/<path>` straight after the `--- a/<path>`
// line of a file's header block (opened by `diff --git`), and every `@@` hunk header after it
// starts that file's content. Inside a hunk an added line whose own text begins `++ ` reads
// `+++ …`, and a removed `-- x` line reads `--- x`, so neither prefix alone tells a header from
// content.
const (
	removedFileHeader = "--- "
	fileDiffHeader    = "diff --git "
	hunkHeader        = "@@"
)

// gitAddedPrefix is the prefix git puts on the added-side path in a diff header.
const gitAddedPrefix = "b/"

// SecretFindings reports the secret material a staged commit would carry: first every path in
// paths whose basename matches a secret-bearing glob (in paths order, class secretPathClass),
// then every content class detected on an ADDED line of diff (in diff order, each attributed
// to the file named by the nearest preceding `+++ b/<path>` header). Only `+`-prefixed lines
// are read — a removed `-` line, a context line and the `+++` header itself never match — and
// each (path, class) pair is reported once, at its first occurrence. A `+++ ` line is that
// header only when it directly follows a `--- ` line and no `@@` hunk header has come since
// the last `diff --git` line (or the start of diff); any other `+++ ` line is an added line
// whose content begins `++ `, scanned and attributed like the rest. The function is pure:
// the caller precomputes both the staged diff and the staged paths (D6), so it never touches
// git or the filesystem and can be judged in a table test.
//
// The order is pinned rather than incidental: a `.env` both staged by name and carrying an
// AWS key yields `.env (secret-bearing file name)` before `.env (AWS access key id)`, and the
// Hint the dispatch prints depends on that.
//
// It is the whole-string form of [SecretScanner], which a caller reading a diff of any size
// feeds as it arrives; the two answer identically.
func SecretFindings(diff string, paths []string) []SecretFinding {
	scanner := NewSecretScanner()
	for _, p := range paths {
		scanner.scanPath(p)
	}
	// A SecretScanner that has not ended never refuses a write.
	_, _ = scanner.Write([]byte(diff))
	return scanner.Findings()
}

// SecretScanner is [SecretFindings] fed incrementally, for a staged diff too large to hold as
// one string: the diff is written to it as an io.Writer in chunks of any size, the staged
// paths through [SecretScanner.NameListWriter], and [SecretScanner.Findings] ends both streams
// and answers exactly what SecretFindings would for the whole diff and the whole path list —
// the same findings, path findings first, each (path, class) pair once. It holds the findings,
// the pending partial line and the pending partial name, never the diff: its memory is bounded
// by the longest single line, not by the diff's size. The diff and the name list may be written
// in either order, but each is one stream — interleaving two diffs' bytes mixes their lines. A
// SecretScanner is not safe for concurrent use.
type SecretScanner struct {
	pathFindings    []SecretFinding // name findings, in listing order
	contentFindings []SecretFinding // content findings, in diff order
	seen            map[SecretFinding]bool
	pendingLine     []byte // the diff's bytes since its last newline
	pendingName     []byte // the name list's bytes since its last NUL
	current         string // the path the nearest preceding `+++ ` header named
	inHunk          bool   // an `@@` line has come since the last `diff --git` line
	afterRemoved    bool   // the previous line was a header-block `--- ` line
	hasEnded        bool   // Findings has run; the streams take no more bytes
}

// errSecretScanEnded is a write to a SecretScanner whose Findings already ran: the bytes
// would never be scanned, so the write fails rather than dropping them unseen.
var errSecretScanEnded = errors.New("apogee: security: write to a secret scanner that has ended")

// NewSecretScanner returns an empty SecretScanner, ready for the diff and the name list.
func NewSecretScanner() *SecretScanner {
	return &SecretScanner{seen: map[SecretFinding]bool{}}
}

// Write scans the next chunk of the staged diff. Each complete line is judged as it arrives;
// the bytes after the chunk's last newline wait for the rest of their line. It always consumes
// all of p, and fails only after Findings has ended the stream.
func (s *SecretScanner) Write(p []byte) (int, error) {
	if s.hasEnded {
		return 0, errSecretScanEnded
	}
	consumed := len(p)
	for {
		newline := bytes.IndexByte(p, '\n')
		if newline < 0 {
			s.pendingLine = append(s.pendingLine, p...)
			return consumed, nil
		}
		s.scanLine(takePending(&s.pendingLine, p[:newline]))
		p = p[newline+1:]
	}
}

// NameListWriter returns the writer the staged paths go to, in git's `--name-only -z` form:
// each path raw and NUL-terminated, written in chunks of any size. Every non-empty field is
// judged by its name alone, exactly as SecretFindings judges an entry of paths; an empty field
// (the run of a trailing NUL) names nothing. Like Write it fails only after Findings has run.
func (s *SecretScanner) NameListWriter() io.Writer {
	return secretNameList{scanner: s}
}

// Findings ends both streams and returns what they carried: the name findings in listing
// order, then the content findings in diff order. The diff's final, unterminated line and the
// name list's final, unterminated field are judged here, as SecretFindings judges the text
// after the last newline. Calling it again returns the same findings; a write after it fails.
func (s *SecretScanner) Findings() []SecretFinding {
	if !s.hasEnded {
		s.hasEnded = true
		s.scanLine(takePending(&s.pendingLine, nil))
		if name := takePending(&s.pendingName, nil); name != "" {
			s.scanPath(name)
		}
	}
	if len(s.pathFindings)+len(s.contentFindings) == 0 {
		return nil // a clean scan answers nil, as SecretFindings always has
	}
	findings := make([]SecretFinding, 0, len(s.pathFindings)+len(s.contentFindings))
	findings = append(findings, s.pathFindings...)
	return append(findings, s.contentFindings...)
}

// takePending returns the pending bytes followed by tail as one string and empties pending. A
// line or name that arrived whole in one chunk is converted without touching the buffer.
func takePending(pending *[]byte, tail []byte) string {
	if len(*pending) == 0 {
		return string(tail)
	}
	joined := string(append(*pending, tail...))
	*pending = (*pending)[:0]
	return joined
}

// record adds f to findings unless its (path, class) pair was reported already. The one seen
// set serves both lists: a name finding's class (secretPathClass) is never a content class, so
// sharing it dedups each list exactly as separate sets would.
func (s *SecretScanner) record(findings *[]SecretFinding, f SecretFinding) {
	if s.seen[f] {
		return
	}
	s.seen[f] = true
	*findings = append(*findings, f)
}

// scanPath judges one staged path by its basename.
func (s *SecretScanner) scanPath(p string) {
	if isSecretBearingName(path.Base(p)) {
		s.record(&s.pathFindings, SecretFinding{Path: p, Class: secretPathClass})
	}
}

// scanLine judges one diff line (without its newline): it tracks the header state that tells
// a `+++ b/<path>` header from an added line beginning `++ `, and matches every content class
// against an added line's text.
func (s *SecretScanner) scanLine(line string) {
	isHeader := s.afterRemoved && !s.inHunk && strings.HasPrefix(line, addedFileHeader)
	s.afterRemoved = !s.inHunk && strings.HasPrefix(line, removedFileHeader)
	switch {
	case strings.HasPrefix(line, fileDiffHeader):
		s.inHunk = false
		return
	case strings.HasPrefix(line, hunkHeader):
		s.inHunk = true
		return
	case isHeader:
		s.current = addedFilePath(line)
		return
	}
	if !strings.HasPrefix(line, "+") {
		return
	}
	for _, c := range secretContentPatterns {
		if c.pattern.MatchString(line[1:]) {
			s.record(&s.contentFindings, SecretFinding{Path: s.current, Class: c.class})
		}
	}
}

// secretNameList is the io.Writer NameListWriter hands out: it splits git's NUL-terminated
// listing into paths for its scanner.
type secretNameList struct {
	scanner *SecretScanner
}

// Write judges every complete NUL-terminated path in p and keeps the trailing partial one for
// the next chunk.
func (w secretNameList) Write(p []byte) (int, error) {
	s := w.scanner
	if s.hasEnded {
		return 0, errSecretScanEnded
	}
	consumed := len(p)
	for {
		nul := bytes.IndexByte(p, 0)
		if nul < 0 {
			s.pendingName = append(s.pendingName, p...)
			return consumed, nil
		}
		if name := takePending(&s.pendingName, p[:nul]); name != "" {
			s.scanPath(name)
		}
		p = p[nul+1:]
	}
}

// isSecretBearingName reports whether a basename alone marks a staged file as secret material:
// it matches one of secretPathGlobs, is not one of the env-template exceptions, and is not a
// public key (publicKeySuffix).
func isSecretBearingName(base string) bool {
	if strings.HasSuffix(base, publicKeySuffix) || secretPathExceptions[base] {
		return false
	}
	for _, glob := range secretPathGlobs {
		// The globs are literals fixed above, so path.Match cannot return ErrBadPattern.
		if ok, _ := path.Match(glob, base); ok {
			return true
		}
	}
	return false
}

// addedFilePath extracts the path a `+++ b/<path>` header names: the trailing tab git appends
// after a path with spaces is dropped, a C-quoted path is unquoted, and the `b/` prefix is
// stripped. `+++ /dev/null` (a deletion) yields "/dev/null" verbatim; no added line follows it.
func addedFilePath(header string) string {
	name := strings.TrimRight(strings.TrimPrefix(header, addedFileHeader), "\t")
	if strings.HasPrefix(name, `"`) {
		if unquoted, err := strconv.Unquote(name); err == nil {
			name = unquoted
		}
	}
	return strings.TrimPrefix(name, gitAddedPrefix)
}

// secretsHintMaxListed is how many findings the Hint names before folding the rest into a
// `+N more` count — three keeps the pop-up's Fix row to one line.
const secretsHintMaxListed = 3

// SecretsHint renders the findings as the rule Hint the approval pop-up's Fix row and the
// model's deny text carry — the pinned wording
// `staged secret material: <path> (<class>)[, <path> (<class>)…] — unstage it, or approve to
// commit anyway`, naming at most secretsHintMaxListed findings and then `, +N more`. An empty
// slice renders as the empty string: no findings, no hint.
func SecretsHint(fs []SecretFinding) string {
	if len(fs) == 0 {
		return ""
	}
	listed := fs
	if len(listed) > secretsHintMaxListed {
		listed = listed[:secretsHintMaxListed]
	}
	parts := make([]string, 0, len(listed)+1)
	for _, f := range listed {
		parts = append(parts, fmt.Sprintf("%s (%s)", f.Path, f.Class))
	}
	if extra := len(fs) - len(listed); extra > 0 {
		parts = append(parts, fmt.Sprintf("+%d more", extra))
	}
	return "staged secret material: " + strings.Join(parts, ", ") +
		" — unstage it, or approve to commit anyway"
}
