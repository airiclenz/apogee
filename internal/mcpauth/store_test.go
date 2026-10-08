package mcpauth

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

const (
	testServer   = "docs"
	testEndpoint = "https://mcp.example.com/v1/mcp"
)

// sampleRecord is a record with every field set, so a round trip proves each one survives.
func sampleRecord() Record {
	return Record{
		AccessToken:   "access-1",
		RefreshToken:  "refresh-1",
		TokenType:     "Bearer",
		Expiry:        time.Date(2026, 10, 8, 12, 30, 0, 0, time.UTC),
		Issuer:        "https://auth.example.com",
		TokenEndpoint: "https://auth.example.com/oauth/token",
		Resource:      "https://mcp.example.com/v1/mcp",
		Scopes:        []string{"read", "write"},
		Client: Client{
			ID:                   "client-1",
			Secret:               "secret-1",
			RegistrationEndpoint: "https://auth.example.com/oauth/register",
		},
	}
}

// saveRecord saves record for name and endpoint in store, failing the test on an error.
func saveRecord(t *testing.T, store *Store, name, endpoint string, record Record) {
	t.Helper()
	if err := store.Save(name, endpoint, record); err != nil {
		t.Fatalf("Save(%q, %q) error = %v", name, endpoint, err)
	}
}

func TestStoreRoundTripsEveryField(t *testing.T) {
	t.Parallel()
	store := NewStore(t.TempDir())
	want := sampleRecord()

	saveRecord(t, store, testServer, testEndpoint, want)
	got, ok, err := store.Load(testServer, testEndpoint)

	if err != nil || !ok {
		t.Fatalf("Load() ok = %v, err = %v; want a hit", ok, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Load() = %+v\nwant %+v", got, want)
	}
}

func TestStoreSaveReplacesTheEarlierRecord(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	store := NewStore(home)
	saveRecord(t, store, testServer, testEndpoint, sampleRecord())
	rotated := sampleRecord()
	rotated.AccessToken = "access-2"
	rotated.RefreshToken = "refresh-2"

	saveRecord(t, store, testServer, testEndpoint, rotated)
	got, ok, err := store.Load(testServer, testEndpoint)

	if err != nil || !ok || got.AccessToken != "access-2" || got.RefreshToken != "refresh-2" {
		t.Fatalf("Load() = %+v, %v, %v; want the rotated tokens", got, ok, err)
	}
	entries, err := os.ReadDir(filepath.Join(home, dirName))
	if err != nil {
		t.Fatalf("read token dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != testServer+recordExt {
		t.Errorf("token dir holds %v; want only %s%s (no temp file left)", entries, testServer, recordExt)
	}
}

func TestStoreKeepsRecordsPrivate(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX permission bits")
	}
	home := t.TempDir()
	dir := filepath.Join(home, dirName)
	// A directory someone else created world-readable is narrowed by the first save.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("pre-create token dir: %v", err)
	}

	saveRecord(t, NewStore(home), testServer, testEndpoint, sampleRecord())

	path := filepath.Join(dir, testServer+recordExt)
	for p, want := range map[string]os.FileMode{path: filePerm, dir: dirPerm} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %q: %v", p, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%q perm = %o, want %o", p, got, want)
		}
	}
}

func TestStoreLoadMisses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		saved    bool
		endpoint string
	}{
		{name: "no record", saved: false, endpoint: testEndpoint},
		{name: "different path", saved: true, endpoint: "https://mcp.example.com/v2/mcp"},
		{name: "different host", saved: true, endpoint: "https://other.example.com/v1/mcp"},
		{name: "different scheme", saved: true, endpoint: "http://mcp.example.com/v1/mcp"},
		{name: "different port", saved: true, endpoint: "https://mcp.example.com:8443/v1/mcp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := NewStore(t.TempDir())
			if tt.saved {
				saveRecord(t, store, testServer, testEndpoint, sampleRecord())
			}

			got, ok, err := store.Load(testServer, tt.endpoint)

			if err != nil || ok || !reflect.DeepEqual(got, Record{}) {
				t.Errorf("Load(%q) = %+v, %v, %v; want a miss", tt.endpoint, got, ok, err)
			}
		})
	}
}

func TestStoreLoadHitsAcrossEquivalentSpellings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, saved, loaded string
	}{
		{"case of scheme and host", "https://mcp.example.com/v1/mcp", "HTTPS://MCP.Example.COM/v1/mcp"},
		{"default https port", "https://mcp.example.com/v1/mcp", "https://mcp.example.com:443/v1/mcp"},
		{"default http port", "http://127.0.0.1/mcp", "http://127.0.0.1:80/mcp"},
		{"trailing slash", "https://mcp.example.com/v1/mcp", "https://mcp.example.com/v1/mcp/"},
		{"bare host", "https://mcp.example.com", "https://mcp.example.com/"},
		{"ipv6 default port", "https://[::1]/mcp", "https://[::1]:443/mcp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := NewStore(t.TempDir())
			saveRecord(t, store, testServer, tt.saved, sampleRecord())

			_, ok, err := store.Load(testServer, tt.loaded)

			if err != nil || !ok {
				t.Errorf("Load(%q) after Save(%q): ok = %v, err = %v; want a hit", tt.loaded, tt.saved, ok, err)
			}
		})
	}
}

func TestStoreRefusesAnEndpointThatIsNotAnAbsoluteURL(t *testing.T) {
	t.Parallel()
	store := NewStore(t.TempDir())

	saveErr := store.Save(testServer, "mcp.example.com/v1", sampleRecord())
	_, _, loadErr := store.Load(testServer, "/v1/mcp")

	if saveErr == nil || loadErr == nil {
		t.Errorf("Save err = %v, Load err = %v; want both refused", saveErr, loadErr)
	}
}

func TestStoreLoadReportsACorruptRecord(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	store := NewStore(home)
	saveRecord(t, store, testServer, testEndpoint, sampleRecord())
	path := filepath.Join(home, dirName, testServer+recordExt)
	if err := os.WriteFile(path, []byte("{not json"), filePerm); err != nil {
		t.Fatalf("corrupt record: %v", err)
	}

	_, ok, err := store.Load(testServer, testEndpoint)

	if err == nil || ok {
		t.Errorf("Load() ok = %v, err = %v; want a decode error", ok, err)
	}
}

func TestStoreDeleteRemovesTheRecord(t *testing.T) {
	t.Parallel()
	store := NewStore(t.TempDir())
	saveRecord(t, store, testServer, testEndpoint, sampleRecord())

	if err := store.Delete(testServer); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	_, ok, err := store.Load(testServer, testEndpoint)

	if err != nil || ok {
		t.Errorf("Load() after Delete ok = %v, err = %v; want a miss", ok, err)
	}
}

func TestStoreDeleteOfAMissingRecordIsNotAnError(t *testing.T) {
	t.Parallel()
	store := NewStore(t.TempDir())

	err := store.Delete(testServer)

	if err != nil {
		t.Errorf("Delete() of a missing record error = %v; want nil", err)
	}
}

func TestStoreRefusesAPathTraversalName(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	store := NewStore(home)
	victim := filepath.Join(home, "victim.json")
	if err := os.WriteFile(victim, []byte("{}"), filePerm); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	name := "../victim"

	saveErr := store.Save(name, testEndpoint, sampleRecord())
	_, _, loadErr := store.Load(name, testEndpoint)
	deleteErr := store.Delete(name)

	for op, err := range map[string]error{"Save": saveErr, "Load": loadErr, "Delete": deleteErr} {
		if !errors.Is(err, ErrInvalidServerName) {
			t.Errorf("%s(%q) error = %v; want ErrInvalidServerName", op, name, err)
		}
	}
	if _, err := os.Stat(victim); err != nil {
		t.Errorf("the file outside the token dir was touched: %v", err)
	}
}

func TestValidateServerName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		wantOK bool
	}{
		{"docs", true},
		{"docs.v2", true},
		{"a_b-1", true},
		{"My Docs", false},
		{".hidden", false},
		{"a/b", false},
		{`a\b`, false},
		{"..", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateServerName(tt.name)

			if tt.wantOK && err != nil {
				t.Errorf("ValidateServerName(%q) = %v; want nil", tt.name, err)
			}
			if !tt.wantOK && !errors.Is(err, ErrInvalidServerName) {
				t.Errorf("ValidateServerName(%q) = %v; want ErrInvalidServerName", tt.name, err)
			}
		})
	}
}
