package tools

import (
	"cmp"
	"os"
	"slices"
	"strings"
)

// redactedMarker is what a configured secret's value becomes on its way out. It is deliberately
// NOT network.go's "[redacted-url]": that marker says a request URL was removed from an error
// string, this one says a credential value was removed from arbitrary output, and a reader who
// meets either wants to know which happened.
const redactedMarker = "[redacted]"

// RedactSecrets returns text with every occurrence of the current value of each variable named in
// secretEnv replaced by "[redacted]".
//
// It is the OUTPUT-side half of the credential scrub whose input-side half is subprocessEnv
// (exec_common.go): the scrub keeps a configured `api-key-env:` value (ADR 0047) out of a child's
// environment, and this keeps a value the child printed anyway — from its own config file, from a
// URL it built, from an environment apogee did not spawn it with — out of the text apogee then
// hands a model, a reporter or a session record. The two are complementary rather than
// alternative: neither one makes the other redundant.
//
// The names are the configured spellings, whitespace-trimmed; a blank entry, a name nothing
// exported, and a name exported EMPTY are all skipped, because an empty value would otherwise
// match at every position and redact the whole of text. Values are replaced longest first, so a
// secret that is a prefix of another never consumes the longer one and leaves its tail in the
// clear. Lookup is os.LookupEnv, so the value redacted is whatever the process holds now.
//
// It is a pure function of text, secretEnv and the environment: no I/O beyond the env read, and
// nothing about the caller — tool, Reaction or subprocess — reaches it.
func RedactSecrets(text string, secretEnv []string) string {
	if text == "" || len(secretEnv) == 0 {
		return text
	}

	values := make([]string, 0, len(secretEnv))
	seen := make(map[string]struct{}, len(secretEnv))
	for _, configuredName := range secretEnv {
		name := strings.TrimSpace(configuredName)
		if name == "" {
			continue
		}
		value, ok := os.LookupEnv(name)
		if !ok || value == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	slices.SortFunc(values, func(a, b string) int { return cmp.Compare(len(b), len(a)) })

	for _, value := range values {
		text = strings.ReplaceAll(text, value, redactedMarker)
	}
	return text
}
