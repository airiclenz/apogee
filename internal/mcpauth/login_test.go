package mcpauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/airiclenz/apogee/internal/security"
)

const (
	testCode        = "the-code"
	testRedirectURL = "http://127.0.0.1:43123/callback"
	testAccessToken = "access-token-1"
	testScope       = "read write"
)

// fakeOAuth is an MCP resource server and its authorization server on two loopback origins. It
// records what the login sent so a test can assert on the wire.
type fakeOAuth struct {
	resource *httptest.Server
	authSrv  *httptest.Server
	// endpointPath is the MCP endpoint's path on the resource server ("/mcp" or "/mcp/").
	endpointPath string
	// probeStatus, when non-zero, is what the endpoint answers instead of its 401.
	probeStatus int
	// hintMetadata puts a resource_metadata URL in the 401 challenge; without it the login has
	// to find the metadata at the well-known path.
	hintMetadata bool

	mu             sync.Mutex
	registrations  []oauthex.ClientRegistrationMetadata
	authorizeQuery url.Values
	tokenForm      url.Values
}

// newFakeOAuth starts both servers, closed when the test ends.
func newFakeOAuth(t *testing.T, endpointPath string) *fakeOAuth {
	t.Helper()
	f := &fakeOAuth{endpointPath: endpointPath, hintMetadata: true}
	f.resource = httptest.NewServer(http.HandlerFunc(f.serveResource))
	f.authSrv = httptest.NewServer(http.HandlerFunc(f.serveAuth))
	t.Cleanup(f.resource.Close)
	t.Cleanup(f.authSrv.Close)
	return f
}

// endpoint is the MCP endpoint's full URL.
func (f *fakeOAuth) endpoint() string {
	return f.resource.URL + f.endpointPath
}

// writeJSON answers status with v as an application/json body.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// serveResource is the MCP server: a 401 on the endpoint and its protected-resource metadata at
// the path-inserted well-known URL.
func (f *fakeOAuth) serveResource(w http.ResponseWriter, r *http.Request) {
	metadataPath := "/.well-known/oauth-protected-resource/" + strings.TrimLeft(f.endpointPath, "/")
	switch r.URL.Path {
	case f.endpointPath:
		if f.probeStatus != 0 {
			w.WriteHeader(f.probeStatus)
			return
		}
		challenge := fmt.Sprintf(`Bearer scope=%q`, testScope)
		if f.hintMetadata {
			challenge += fmt.Sprintf(`, resource_metadata=%q`, f.resource.URL+metadataPath)
		}
		w.Header().Set("WWW-Authenticate", challenge)
		w.WriteHeader(http.StatusUnauthorized)
	case metadataPath:
		writeJSON(w, http.StatusOK, oauthex.ProtectedResourceMetadata{
			Resource:             f.endpoint(),
			AuthorizationServers: []string{f.authSrv.URL},
		})
	default:
		http.NotFound(w, r)
	}
}

// serveAuth is the authorization server: RFC 8414 metadata, registration and the token endpoint.
func (f *fakeOAuth) serveAuth(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/oauth-authorization-server":
		writeJSON(w, http.StatusOK, oauthex.AuthServerMeta{
			Issuer:                            f.authSrv.URL,
			AuthorizationEndpoint:             f.authSrv.URL + "/authorize",
			TokenEndpoint:                     f.authSrv.URL + "/token",
			RegistrationEndpoint:              f.authSrv.URL + "/register",
			CodeChallengeMethodsSupported:     []string{"S256"},
			TokenEndpointAuthMethodsSupported: []string{"none", "client_secret_post"},
		})
	case "/register":
		f.serveRegister(w, r)
	case "/token":
		f.serveToken(w, r)
	default:
		http.NotFound(w, r)
	}
}

// serveRegister records a registration and issues a public client for it.
func (f *fakeOAuth) serveRegister(w http.ResponseWriter, r *http.Request) {
	var requested oauthex.ClientRegistrationMetadata
	if err := json.NewDecoder(r.Body).Decode(&requested); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.registrations = append(f.registrations, requested)
	id := fmt.Sprintf("dcr-client-%d", len(f.registrations))
	f.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  id,
		"redirect_uris":              requested.RedirectURIs,
		"token_endpoint_auth_method": "none",
	})
}

// serveToken records the exchange and issues a token when the code and the PKCE verifier check.
func (f *fakeOAuth) serveToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.tokenForm = r.PostForm
	challenge := f.authorizeQuery.Get("code_challenge")
	f.mu.Unlock()

	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if r.PostForm.Get("code") != testCode || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  testAccessToken,
		"refresh_token": "refresh-token-1",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         testScope,
	})
}

// wire is what the fake saw on the wire, copied under its lock.
type wire struct {
	registrations  []oauthex.ClientRegistrationMetadata
	authorizeQuery url.Values
	tokenForm      url.Values
}

// seen returns a snapshot of what the fake recorded.
func (f *fakeOAuth) seen() wire {
	f.mu.Lock()
	defer f.mu.Unlock()
	return wire{registrations: f.registrations, authorizeQuery: f.authorizeQuery, tokenForm: f.tokenForm}
}

// fetcher answers the authorize URL as a user who approved it would, recording its query; state
// overrides the state echoed back when non-empty.
func (f *fakeOAuth) fetcher(state string) CodeFetcher {
	return func(_ context.Context, authorizeURL string) (string, string, error) {
		u, err := url.Parse(authorizeURL)
		if err != nil {
			return "", "", err
		}
		f.mu.Lock()
		f.authorizeQuery = u.Query()
		f.mu.Unlock()
		if state == "" {
			state = u.Query().Get("state")
		}
		return testCode, state, nil
	}
}

// loopbackClient is a client that reaches the test servers: the flow tests stand in for the
// vetted endpoint client and NewAuthClient, which refuse loopback by design.
func loopbackClient() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

// loginConfig is a DCR login against f, saving into store.
func (f *fakeOAuth) loginConfig(store *Store) LoginConfig {
	return LoginConfig{
		Name:           testServer,
		Endpoint:       f.endpoint(),
		Store:          store,
		EndpointClient: loopbackClient(),
		AuthClient:     loopbackClient(),
		RedirectURL:    testRedirectURL,
		FetchCode:      f.fetcher(""),
	}
}

func TestLoginRegistersByDCRAndSavesTheRecord(t *testing.T) {
	t.Parallel()
	fake := newFakeOAuth(t, "/mcp")
	store := NewStore(t.TempDir())

	record, err := Login(context.Background(), fake.loginConfig(store))

	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	want := Record{
		AccessToken:   testAccessToken,
		RefreshToken:  "refresh-token-1",
		TokenType:     "Bearer",
		Expiry:        record.Expiry,
		Issuer:        fake.authSrv.URL,
		TokenEndpoint: fake.authSrv.URL + "/token",
		Resource:      fake.endpoint(),
		Scopes:        []string{"read", "write"},
		Client: Client{
			ID:                      "dcr-client-1",
			RegistrationEndpoint:    fake.authSrv.URL + "/register",
			RedirectURIs:            []string{testRedirectURL},
			TokenEndpointAuthMethod: "none",
		},
	}
	if !reflect.DeepEqual(record, want) || record.Expiry.IsZero() {
		t.Errorf("Login() = %+v\nwant %+v", record, want)
	}
	saved, ok, err := store.Load(testServer, fake.endpoint())
	if ok && saved.Expiry.Equal(record.Expiry) {
		// The file round-trips the instant, not the monotonic reading or the location.
		saved.Expiry = record.Expiry
	}
	if err != nil || !ok || !reflect.DeepEqual(saved, record) {
		t.Errorf("store.Load() = %+v, %v, %v; want the returned record", saved, ok, err)
	}
	if len(fake.seen().registrations) != 1 || fake.seen().registrations[0].RedirectURIs[0] != testRedirectURL {
		t.Errorf("registrations = %+v, want one for %s", fake.seen().registrations, testRedirectURL)
	}
}

func TestLoginWithAPreregisteredClientDoesNotRegister(t *testing.T) {
	t.Parallel()
	fake := newFakeOAuth(t, "/mcp")
	store := NewStore(t.TempDir())
	cfg := fake.loginConfig(store)
	cfg.ClientID = "pre-1"
	cfg.ClientSecret = "pre-secret"

	record, err := Login(context.Background(), cfg)

	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if len(fake.seen().registrations) != 0 {
		t.Errorf("registrations = %d, want none for a preregistered client", len(fake.seen().registrations))
	}
	want := Client{ID: "pre-1", TokenEndpointAuthMethod: "client_secret_post"}
	if !reflect.DeepEqual(record.Client, want) {
		t.Errorf("record.Client = %+v, want %+v (the secret is never saved)", record.Client, want)
	}
	if got := fake.seen().tokenForm.Get("client_secret"); got != "pre-secret" {
		t.Errorf("token request client_secret = %q, want the configured secret", got)
	}
}

func TestLoginRefusesAStateMismatch(t *testing.T) {
	t.Parallel()
	fake := newFakeOAuth(t, "/mcp")
	cfg := fake.loginConfig(NewStore(t.TempDir()))
	cfg.FetchCode = fake.fetcher("forged-state")

	_, err := Login(context.Background(), cfg)

	if !errors.Is(err, ErrStateMismatch) {
		t.Fatalf("Login() error = %v, want ErrStateMismatch", err)
	}
	if fake.seen().tokenForm != nil {
		t.Errorf("the code was exchanged despite the state mismatch: %v", fake.seen().tokenForm)
	}
}

func TestLoginSendsThePKCEVerifier(t *testing.T) {
	t.Parallel()
	fake := newFakeOAuth(t, "/mcp")

	_, err := Login(context.Background(), fake.loginConfig(NewStore(t.TempDir())))

	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if got := fake.seen().authorizeQuery.Get("code_challenge_method"); got != "S256" {
		t.Errorf("authorize code_challenge_method = %q, want S256", got)
	}
	if fake.seen().tokenForm.Get("code_verifier") == "" {
		t.Error("the token request carried no code_verifier")
	}
}

func TestLoginBindsTheEndpointAsTheResourceOnBothLegs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		endpointPath string
		hintMetadata bool
	}{
		{name: "metadata named by the challenge", endpointPath: "/mcp", hintMetadata: true},
		{name: "metadata at the well-known path", endpointPath: "/mcp", hintMetadata: false},
		{name: "trailing slash still matches its metadata", endpointPath: "/mcp/", hintMetadata: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeOAuth(t, tc.endpointPath)
			fake.hintMetadata = tc.hintMetadata

			record, err := Login(context.Background(), fake.loginConfig(NewStore(t.TempDir())))

			if err != nil {
				t.Fatalf("Login() error = %v", err)
			}
			want := fake.endpoint()
			if got := fake.seen().authorizeQuery.Get("resource"); got != want {
				t.Errorf("authorize resource = %q, want %q", got, want)
			}
			if got := fake.seen().tokenForm.Get("resource"); got != want {
				t.Errorf("token resource = %q, want %q", got, want)
			}
			if record.Resource != want {
				t.Errorf("record.Resource = %q, want %q", record.Resource, want)
			}
		})
	}
}

func TestLoginReusesAStoredRegistrationOnlyForTheSameRedirect(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		storedRedirect   string
		wantRegistration bool
		wantClientID     string
	}{
		{name: "matching redirect is reused", storedRedirect: testRedirectURL, wantClientID: "stored-client"},
		{
			name:             "stale redirect is re-registered",
			storedRedirect:   "http://127.0.0.1:1/old",
			wantRegistration: true,
			wantClientID:     "dcr-client-1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeOAuth(t, "/mcp")
			store := NewStore(t.TempDir())
			saveRecord(t, store, testServer, fake.endpoint(), Record{
				AccessToken: "expired",
				Client: Client{
					ID:                      "stored-client",
					RegistrationEndpoint:    fake.authSrv.URL + "/register",
					RedirectURIs:            []string{tc.storedRedirect},
					TokenEndpointAuthMethod: "none",
				},
			})

			record, err := Login(context.Background(), fake.loginConfig(store))

			if err != nil {
				t.Fatalf("Login() error = %v", err)
			}
			if got := len(fake.seen().registrations) == 1; got != tc.wantRegistration {
				t.Errorf("registered = %v, want %v", got, tc.wantRegistration)
			}
			if record.Client.ID != tc.wantClientID {
				t.Errorf("record.Client.ID = %q, want %q", record.Client.ID, tc.wantClientID)
			}
		})
	}
}

func TestLoginWithoutA401IsNoAuthRequired(t *testing.T) {
	t.Parallel()
	fake := newFakeOAuth(t, "/mcp")
	fake.probeStatus = http.StatusOK

	_, err := Login(context.Background(), fake.loginConfig(NewStore(t.TempDir())))

	if !errors.Is(err, ErrNoAuthRequired) {
		t.Fatalf("Login() error = %v, want ErrNoAuthRequired", err)
	}
}

// noProxy keeps the auth-client tests off the environment's proxy settings.
func noProxy(*http.Request) (*url.URL, error) { return nil, nil }

func TestNewAuthClientRefusesALoopbackServerUnderTheDefaultGuard(t *testing.T) {
	t.Parallel()
	var hits int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
	}))
	t.Cleanup(server.Close)

	resp, err := NewAuthClient(security.URLGuard{}, noProxy).Get(server.URL)

	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("Get() of a loopback authorization server succeeded, want a refusal")
	}
	if !errors.Is(err, security.ErrURLBlocked) {
		t.Errorf("Get() error = %v, want it to wrap security.ErrURLBlocked", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Errorf("the server was reached %d times, want none", hits)
	}
}

func TestNewAuthClientFailsAnOversizeRegistrationBody(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(w, `{"client_id":"c","client_name":"%s"}`, strings.Repeat("x", maxAuthResponseBytes))
	}))
	t.Cleanup(server.Close)
	client := NewAuthClient(security.URLGuard{}.DisableIPFloor(), noProxy)
	meta := &oauthex.ClientRegistrationMetadata{RedirectURIs: []string{testRedirectURL}}

	_, err := oauthex.RegisterClient(context.Background(), server.URL+"/register", meta, client)

	var tooLarge *http.MaxBytesError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("RegisterClient() error = %v, want the body bound's *http.MaxBytesError", err)
	}
}

func TestNewAuthClientDoesNotFollowARedirect(t *testing.T) {
	t.Parallel()
	var followed bool
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			mu.Lock()
			followed = true
			mu.Unlock()
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	resp, err := NewAuthClient(security.URLGuard{}.DisableIPFloor(), noProxy).Get(server.URL + "/start")

	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want the redirect itself (%d)", resp.StatusCode, http.StatusFound)
	}
	mu.Lock()
	defer mu.Unlock()
	if followed {
		t.Error("the redirect was followed")
	}
}
