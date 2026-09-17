package security

import (
	"fmt"
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

// gitAddedPrefix is the prefix git puts on the added-side path in a diff header.
const gitAddedPrefix = "b/"

// SecretFindings reports the secret material a staged commit would carry: first every path in
// paths whose basename matches a secret-bearing glob (in paths order, class secretPathClass),
// then every content class detected on an ADDED line of diff (in diff order, each attributed
// to the file named by the nearest preceding `+++ b/<path>` header). Only `+`-prefixed lines
// are read — a removed `-` line, a context line and the `+++` header itself never match — and
// each (path, class) pair is reported once, at its first occurrence. The function is pure:
// the caller precomputes both the staged diff and the staged paths (D6), so it never touches
// git or the filesystem and can be judged in a table test.
//
// The order is pinned rather than incidental: a `.env` both staged by name and carrying an
// AWS key yields `.env (secret-bearing file name)` before `.env (AWS access key id)`, and the
// Hint the dispatch prints depends on that.
func SecretFindings(diff string, paths []string) []SecretFinding {
	var (
		findings []SecretFinding
		seen     = map[SecretFinding]bool{}
		add      = func(f SecretFinding) {
			if seen[f] {
				return
			}
			seen[f] = true
			findings = append(findings, f)
		}
	)

	for _, p := range paths {
		if isSecretBearingName(path.Base(p)) {
			add(SecretFinding{Path: p, Class: secretPathClass})
		}
	}

	current := ""
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, addedFileHeader) {
			current = addedFilePath(line)
			continue
		}
		if !strings.HasPrefix(line, "+") {
			continue
		}
		for _, c := range secretContentPatterns {
			if c.pattern.MatchString(line[1:]) {
				add(SecretFinding{Path: current, Class: c.class})
			}
		}
	}
	return findings
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
