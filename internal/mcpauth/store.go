package mcpauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// dirName is the directory under the apogee home that holds every server's record.
const dirName = "mcp-auth"

// recordExt is the extension a record's file carries after the server-name stem.
const recordExt = ".json"

// dirPerm and filePerm scope the records to the owner: they hold bearer and refresh tokens.
const (
	dirPerm  os.FileMode = 0o700
	filePerm os.FileMode = 0o600
)

// tempPattern names a save's temp file; the leading dot keeps a half-written one out of a plain
// directory listing, and the .tmp suffix keeps it from ever reading as a record.
const tempPattern = ".apogee-mcp-auth-*.tmp"

// defaultPorts maps a scheme to the port a normalised endpoint leaves implicit.
var defaultPorts = map[string]string{"http": "80", "https": "443"}

// serverNamePattern is the safe-file-stem rule: an ASCII letter or digit first, then letters,
// digits, '.', '_' or '-'. The leading character rules out hidden files, "." and "..", and the
// alphabet rules out separators and spaces on every OS.
var serverNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ErrInvalidServerName is wrapped by ValidateServerName's refusal, so a caller can tell an unsafe
// name from an I/O failure.
var ErrInvalidServerName = errors.New("not a safe file stem")

// ValidateServerName reports whether name may name a record file: it returns nil for a name
// matching ^[A-Za-z0-9][A-Za-z0-9._-]*$ (`docs`, `docs.v2`, `a_b-1`) and an error wrapping
// ErrInvalidServerName for anything else (`My Docs`, `.hidden`, `a/b`, `..`, the empty name). It is
// the one home of the rule; every package that builds a record path from a server name calls it.
func ValidateServerName(name string) error {
	if serverNamePattern.MatchString(name) {
		return nil
	}
	return fmt.Errorf(
		"mcpauth: server name %q is %w (use letters, digits, '.', '_' and '-', starting with a letter or digit)",
		name,
		ErrInvalidServerName,
	)
}

// Client is the OAuth client a token was issued to: a Dynamic Client Registration result, or a
// preregistered `client-id:`. Secret is set only for a registration that returned one; a
// preregistered client's secret comes from its `client-secret-env:` variable and is never stored.
// RegistrationEndpoint and RedirectURIs are set for a registration only: a later login reuses it
// only while both still match. TokenEndpointAuthMethod (RFC 7591 §2) is how the client
// authenticates at the token endpoint, kept so a refresh sends its credentials the same way.
type Client struct {
	ID                      string   `json:"client_id"`
	Secret                  string   `json:"client_secret,omitempty"`
	RegistrationEndpoint    string   `json:"registration_endpoint,omitempty"`
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

// Record is one server's persisted OAuth state: the token, what a refresh needs without
// re-discovery, and the client registration the token belongs to.
type Record struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	Expiry       time.Time `json:"expiry,omitzero"`
	// Issuer is the authorization server's issuer, TokenEndpoint where a refresh is posted, and
	// Resource the RFC 8707 resource value the token was bound to.
	Issuer        string   `json:"issuer,omitempty"`
	TokenEndpoint string   `json:"token_endpoint,omitempty"`
	Resource      string   `json:"resource,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
	Client        Client   `json:"client"`
}

// storedRecord is a record file's content: the Record beside the normalised endpoint it was
// minted for, which Load compares against the configured one.
type storedRecord struct {
	Endpoint string `json:"endpoint"`
	Record
}

// Store loads, saves and deletes the per-server records under one apogee home. It holds no state
// beyond the directory, so any number of Stores — in this process or another — may share it; a
// save's rename is the only coordination the files need.
type Store struct {
	dir string
}

// NewStore returns a Store over <home>/mcp-auth. home is the apogee home directory the caller
// resolved (config.ApogeeHome); nothing is created until the first Save.
func NewStore(home string) *Store {
	return &Store{dir: filepath.Join(home, dirName)}
}

// Load returns the record saved for the server name when it was minted for endpoint. ok is false
// on a miss: no file, or a file whose endpoint differs from endpoint once both are normalised. An
// unsafe name, an endpoint that is not an absolute URL, an unreadable file and a file that is not a
// record are errors.
func (s *Store) Load(name, endpoint string) (Record, bool, error) {
	path, err := s.recordPath(name)
	if err != nil {
		return Record{}, false, err
	}
	want, err := normalizeEndpoint(endpoint)
	if err != nil {
		return Record{}, false, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("mcpauth: read token record %q: %w", path, err)
	}

	var stored storedRecord
	if err := json.Unmarshal(data, &stored); err != nil {
		return Record{}, false, fmt.Errorf("mcpauth: decode token record %q: %w", path, err)
	}
	if stored.Endpoint != want {
		return Record{}, false, nil
	}
	return stored.Record, true, nil
}

// Save writes record as the server name's record, minted for endpoint, replacing any earlier one
// through a temp file and a rename. It creates the directory 0700 (and narrows an existing one to
// 0700) and the file 0600.
func (s *Store) Save(name, endpoint string, record Record) error {
	path, err := s.recordPath(name)
	if err != nil {
		return err
	}
	normalized, err := normalizeEndpoint(endpoint)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(storedRecord{Endpoint: normalized, Record: record}, "", "  ")
	if err != nil {
		return fmt.Errorf("mcpauth: encode token record for %q: %w", name, err)
	}

	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return fmt.Errorf("mcpauth: create token dir %q: %w", s.dir, err)
	}
	// MkdirAll leaves an existing directory's mode alone; the tokens need it owner-only whoever
	// created it.
	if err := os.Chmod(s.dir, dirPerm); err != nil {
		return fmt.Errorf("mcpauth: restrict token dir %q: %w", s.dir, err)
	}
	return atomicWrite(path, data)
}

// Delete removes the server name's record. A record that does not exist is not an error, so a
// logout of a server never logged in succeeds.
func (s *Store) Delete(name string) error {
	path, err := s.recordPath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("mcpauth: delete token record %q: %w", path, err)
	}
	return nil
}

// recordPath is the file holding the server name's record, refused for an unsafe name.
func (s *Store) recordPath(name string) (string, error) {
	if err := ValidateServerName(name); err != nil {
		return "", err
	}
	return filepath.Join(s.dir, name+recordExt), nil
}

// normalizeEndpoint is the form an endpoint is keyed by: scheme and host lower-cased, the scheme's
// default port dropped and trailing slashes trimmed from the path, so spellings of one endpoint
// that a server cannot tell apart match each other.
func normalizeEndpoint(endpoint string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return "", fmt.Errorf("mcpauth: parse endpoint: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("mcpauth: endpoint %q is not an absolute URL", endpoint)
	}

	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	switch {
	case port != "" && port != defaultPorts[u.Scheme]:
		u.Host = net.JoinHostPort(host, port)
	case strings.Contains(host, ":"):
		// An IPv6 literal keeps its brackets once its port is gone.
		u.Host = "[" + host + "]"
	default:
		u.Host = host
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = strings.TrimRight(u.RawPath, "/")
	return u.String(), nil
}

// atomicWrite writes data to a temp file in path's directory and renames it into place, so a
// reader never observes a half-written record and a crash mid-save leaves the previous one intact.
// The temp file is removed on every path except a successful rename.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, tempPattern)
	if err != nil {
		return fmt.Errorf("mcpauth: create temp token file in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("mcpauth: chmod temp token file %q: %w", tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("mcpauth: write temp token file %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("mcpauth: close temp token file %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("mcpauth: rename token file into %q: %w", path, err)
	}
	return nil
}
