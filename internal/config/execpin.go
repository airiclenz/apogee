package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/airiclenz/apogee/internal/adoption"
	"github.com/airiclenz/apogee/internal/domain"
)

// The Adoption kinds of the two execution keys a Project config may propose (ADR 0104 §1). They
// are distinct from the Allow kind `mcp-servers`, so a server entry and an Allow rule naming the
// same server never share a pin.
const (
	MCPServerAdoptionKind = string(domain.ProposalMCPServer)
	ReactionAdoptionKind  = string(domain.ProposalReaction)
)

// pinFilesKey is the canonical text's key holding the pinned project files. No field of either
// schema is spelled `files`, so it can never shadow a pinned field.
const pinFilesKey = "files"

// pinFieldOrder is the order changedSince names changed fields in: the order the adoption pane
// draws them (docs/layout/user-questions-layout.md §Adoption pane), a server's fields first and a
// reaction's after, since the two sets never meet in one text. A field outside it follows, sorted.
var pinFieldOrder = []string{
	"name", "transport", "command", "args", "endpoint", "env", "env-allowlist", "headers",
	"headers-env", "auth", "client-id", "client-secret-env",
	"id", "run", "advise", "gate",
}

// pinArgvKeys are the pinned fields whose every element is an argv element a file may be named
// by: a server's program and arguments, and a reaction's three action keys when they hold a
// command. A reaction's webhook mapping names no file.
var pinArgvKeys = []string{"command", "args", "run", "advise", "gate"}

// pinEnvKey is the pinned field whose KEY=VALUE entries may name a file in their VALUE — a repo
// `env: [NODE_OPTIONS=--require ./hook.js]` makes the server load hook.js — so the value is
// searched for project files as an argv is.
const pinEnvKey = "env"

// pinnedFile is one project file a pin holds: its path relative to the Project root,
// slash-separated, and the hex SHA-256 of its content.
type pinnedFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// ResolveAgainstProjectRoot is the one resolution base a repo entry's relative argv element and
// working directory share (ADR 0104 §4): an absolute name stands as written, a relative one is
// joined onto projectRoot. The pin is built against the same base, so the file whose digest was
// pinned is the file the spawn reads.
func ResolveAgainstProjectRoot(projectRoot, name string) string {
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}
	return filepath.Join(projectRoot, name)
}

// execPin builds the Adoption entry of a repo `mcp-servers:` entry: kind `mcp-server`, handle its
// name, and the canonical text of the WHOLE entry (ADR 0104 §2) with the project files it names.
// A field the entry does not set is left out, so adding an empty one changes nothing; only
// `env-allowlist:` keeps its empty spelling, since `[]` and absent say different things.
func (m mcpServerConfig) execPin(projectRoot string) (adoption.Entry, error) {
	fields := map[string]any{}
	putText(fields, "name", m.Name)
	putText(fields, "transport", m.Transport)
	putText(fields, "command", m.Command)
	putList(fields, "args", m.Args)
	putList(fields, pinEnvKey, m.Env)
	putText(fields, "endpoint", m.Endpoint)
	if m.EnvAllowlist != nil {
		fields["env-allowlist"] = nonNilList(*m.EnvAllowlist)
	}
	putMap(fields, "headers", m.Headers)
	putMap(fields, "headers-env", m.HeadersEnv)
	putText(fields, "auth", m.Auth)
	putText(fields, "client-id", m.ClientID)
	putText(fields, "client-secret-env", m.ClientSecretEnv)
	return pinEntry(MCPServerAdoptionKind, m.Name, projectRoot, fields)
}

// execPin builds the Adoption entry of a repo `reactions:` entry: kind `reaction`, handle its id,
// and the canonical text of its id and its `run:`, `advise:` and `gate:` values with the project
// files they name. `on:`, `timeout:`, `enabled:` and `workspace:` are parameters that change
// without asking (ADR 0104 §2), so they are never in it. An action value is pinned as decoded — an
// argv list, or a webhook mapping whose `url:`, `headers:` and `headers-env:` it carries whole.
func (r reactionConfig) execPin(projectRoot string) (adoption.Entry, error) {
	id := strings.TrimSpace(r.ID)
	fields := map[string]any{"id": id}
	for key, value := range map[string]any{"run": r.Run, "advise": r.Advise, "gate": r.Gate} {
		if value != nil {
			fields[key] = value
		}
	}
	return pinEntry(ReactionAdoptionKind, id, projectRoot, fields)
}

// pinEntry folds the project files fields names into them and renders the Adoption entry.
func pinEntry(kind, handle, projectRoot string, fields map[string]any) (adoption.Entry, error) {
	text, err := pinText(projectRoot, fields)
	if err != nil {
		return adoption.Entry{}, fmt.Errorf("%s %q: %w", kind, handle, err)
	}
	return adoption.Entry{Kind: kind, Handle: handle, Text: text}, nil
}

// pinText renders fields as canonical JSON — sorted keys, no insignificant whitespace (ADR 0104
// §3) — with every project file they name under `files`, sorted by path. A named file that is
// missing is simply absent, so its disappearance moves the text. It fails when a value cannot be
// rendered or an in-root file cannot be read: a file whose content cannot be pinned cannot be
// trusted to stay what was adopted.
func pinText(projectRoot string, fields map[string]any) (string, error) {
	files, err := pinnedFiles(projectRoot, fields)
	if err != nil {
		return "", err
	}
	delete(fields, pinFilesKey)
	if len(files) > 0 {
		fields[pinFilesKey] = files
	}
	return canonicalJSON(fields)
}

// canonicalJSON encodes value with Go's map encoding, which sorts keys, without indentation, HTML
// escaping or the encoder's trailing newline.
func canonicalJSON(value any) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", fmt.Errorf("render the pin: %w", err)
	}
	return strings.TrimSuffix(buffer.String(), "\n"), nil
}

// pinnedFiles is every existing regular file inside projectRoot that fields name — an argv element
// as a whole, an `env:` value by any of its envValueCandidates — once each, sorted by path.
func pinnedFiles(projectRoot string, fields map[string]any) ([]pinnedFile, error) {
	if projectRoot == "" {
		return nil, nil
	}
	realRoot := realPathOrSelf(projectRoot)
	byPath := map[string]pinnedFile{}
	for _, name := range fileCandidates(fields) {
		file, ok, err := pinFile(projectRoot, realRoot, name)
		if err != nil {
			return nil, err
		}
		if ok {
			byPath[file.Path] = file
		}
	}
	files := make([]pinnedFile, 0, len(byPath))
	for _, file := range byPath {
		files = append(files, file)
	}
	slices.SortFunc(files, func(a, b pinnedFile) int { return strings.Compare(a.Path, b.Path) })
	return files, nil
}

// fileCandidates lists every name in fields that may name a project file: each element of an argv
// field, and each candidate of each `env:` entry's value.
func fileCandidates(fields map[string]any) []string {
	var names []string
	for _, key := range pinArgvKeys {
		names = append(names, textsOf(fields[key])...)
	}
	for _, entry := range textsOf(fields[pinEnvKey]) {
		if _, value, ok := strings.Cut(entry, "="); ok {
			names = append(names, envValueCandidates(value)...)
		}
	}
	return names
}

// envValueCandidates splits an `env:` value into the names a launched program may read a file by:
// the whole value, each whitespace-separated word (`--require ./hook.js`), the part of a word after
// its first `=` (`--require=./hook.js`), and each element of a path list (`LD_PRELOAD=a.so:b.so`).
// A candidate that names no in-root file costs nothing, so the split errs toward more of them.
func envValueCandidates(value string) []string {
	names := []string{value}
	for _, word := range strings.Fields(value) {
		names = append(names, word)
		if _, after, ok := strings.Cut(word, "="); ok {
			names = append(names, after)
		}
	}
	for _, name := range slices.Clone(names) {
		names = append(names, filepath.SplitList(name)...)
	}
	return names
}

// textsOf reads a pinned field as a list of text: a single string, a []string as the schema
// decodes one, or a []any as YAML and JSON decode one. Anything else — a webhook mapping, an absent
// field — holds no argv and reads as none.
func textsOf(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []string:
		return typed
	case []any:
		texts := make([]string, 0, len(typed))
		for _, element := range typed {
			if text, ok := element.(string); ok {
				texts = append(texts, text)
			}
		}
		return texts
	default:
		return nil
	}
}

// pinFile pins name when it resolves, against the Project root, to an existing regular file whose
// path — as spelled and with its symlinks evaluated — lies inside the root. It reports false for
// any other name, a missing file included, and fails only when such a file cannot be read.
func pinFile(projectRoot, realRoot, name string) (pinnedFile, bool, error) {
	if strings.TrimSpace(name) == "" {
		return pinnedFile{}, false, nil
	}
	path := ResolveAgainstProjectRoot(projectRoot, name)
	relative, inside := relativeInsideRoot(projectRoot, path)
	if !inside {
		return pinnedFile{}, false, nil
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return pinnedFile{}, false, nil
	}
	if _, realInside := relativeInsideRoot(realRoot, realPathOrSelf(path)); !realInside {
		return pinnedFile{}, false, nil
	}
	digest, err := fileDigest(path)
	if err != nil {
		return pinnedFile{}, false, err
	}
	return pinnedFile{Path: filepath.ToSlash(relative), SHA256: digest}, true, nil
}

// relativeInsideRoot reports whether path lies strictly inside root, with its path relative to root.
func relativeInsideRoot(root, path string) (string, bool) {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", false
	}
	return relative, true
}

// realPathOrSelf is path with its symlinks evaluated, or path cleaned when they cannot be.
func realPathOrSelf(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return resolved
}

// fileDigest is the hex SHA-256 of the file at path.
func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("pin %s: %w", path, err)
	}
	// A read-only file's close reports nothing the digest depends on.
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("pin %s: %w", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// VerifyExecPins is the spawn-time check of an adopted repo entry (ADR 0104 §4): it rebuilds the
// pin of the entry text holds against the files under projectRoot now and returns the path of
// every pinned file whose content changed, that has gone missing, or that the entry names but did
// not pin when it was adopted — empty when the spawn may go ahead. A caller refuses the run on any
// path it returns and re-proposes the entry. It fails when text is not a pin or a named in-root
// file cannot be read, which the caller treats as a mismatch too.
func VerifyExecPins(projectRoot, text string) ([]string, error) {
	var fields map[string]any
	if err := json.Unmarshal([]byte(text), &fields); err != nil {
		return nil, fmt.Errorf("verify pinned files: %w", err)
	}
	rebuilt, err := pinText(projectRoot, fields)
	if err != nil {
		return nil, fmt.Errorf("verify pinned files: %w", err)
	}
	return changedSince(text, rebuilt), nil
}

// changedSince names what moved between the text an entry was adopted with and its text now: each
// pinned field whose value differs, in the pane's order (pinFieldOrder), and then each pinned file,
// by path, whose digest differs or that only one of the two texts holds — a file gone missing
// since included. It names nothing when either text is not a pin, since there is then nothing to
// name a change against.
func changedSince(adoptedText, text string) []string {
	before, beforeFiles, ok := splitPin(adoptedText)
	if !ok {
		return nil
	}
	after, afterFiles, ok := splitPin(text)
	if !ok {
		return nil
	}
	return append(changedFields(before, after), changedFiles(beforeFiles, afterFiles)...)
}

// splitPin reads a pin's text into its fields, kept as their canonical bytes, and its pinned files
// by path.
func splitPin(text string) (map[string]json.RawMessage, map[string]string, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &fields); err != nil {
		return nil, nil, false
	}
	var files []pinnedFile
	if raw, ok := fields[pinFilesKey]; ok {
		if err := json.Unmarshal(raw, &files); err != nil {
			return nil, nil, false
		}
		delete(fields, pinFilesKey)
	}
	digests := make(map[string]string, len(files))
	for _, file := range files {
		digests[file.Path] = file.SHA256
	}
	return fields, digests, true
}

// changedFields names each field whose canonical bytes differ between before and after, present
// in only one of them included, in pinFieldOrder and then sorted.
func changedFields(before, after map[string]json.RawMessage) []string {
	var changed []string
	for _, key := range unionKeys(before, after) {
		if !bytes.Equal(before[key], after[key]) {
			changed = append(changed, key)
		}
	}
	slices.SortStableFunc(changed, func(a, b string) int { return fieldRank(a) - fieldRank(b) })
	return changed
}

// fieldRank is a field's place in pinFieldOrder, with every other field after them all.
func fieldRank(key string) int {
	if index := slices.Index(pinFieldOrder, key); index >= 0 {
		return index
	}
	return len(pinFieldOrder)
}

// changedFiles names each path whose digest differs between before and after, present in only one
// of them included, sorted.
func changedFiles(before, after map[string]string) []string {
	var changed []string
	for _, path := range unionKeys(before, after) {
		beforeDigest, inBefore := before[path]
		afterDigest, inAfter := after[path]
		if inBefore != inAfter || beforeDigest != afterDigest {
			changed = append(changed, path)
		}
	}
	return changed
}

// unionKeys is every key of either map, sorted.
func unionKeys[V any](first, second map[string]V) []string {
	keys := make([]string, 0, len(first)+len(second))
	for key := range first {
		keys = append(keys, key)
	}
	for key := range second {
		if _, ok := first[key]; !ok {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return keys
}

// putText sets fields[key] to a non-empty value.
func putText(fields map[string]any, key, value string) {
	if value != "" {
		fields[key] = value
	}
}

// putList sets fields[key] to a non-empty list, in its own order: an argv's and an env's order
// decide what runs.
func putList(fields map[string]any, key string, values []string) {
	if len(values) > 0 {
		fields[key] = values
	}
}

// putMap sets fields[key] to a non-empty map, which the encoder renders sorted by key.
func putMap(fields map[string]any, key string, values map[string]string) {
	if len(values) > 0 {
		fields[key] = values
	}
}

// nonNilList is values, or an empty list in place of nil, so an explicitly empty list renders as
// `[]` rather than `null`.
func nonNilList(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
