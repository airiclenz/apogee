package mcpauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	handlerEndpoint = "https://mcp.example.com/mcp"
	staleAccess     = "access-old"
	staleRefresh    = "refresh-old"
)

// fakeTokenServer is an authorization server's token endpoint that rotates refresh tokens: each
// refresh token is good for one refresh, and a spent one is answered with invalid_grant.
type fakeTokenServer struct {
	srv *httptest.Server
	// reject, when set, answers every refresh with invalid_grant.
	reject bool

	mu       sync.Mutex
	live     map[string]bool
	minted   int
	forms    []url.Values
	basicIDs []string
}

// newFakeTokenServer starts a token endpoint accepting the refresh token initial.
func newFakeTokenServer(t *testing.T, initial string) *fakeTokenServer {
	t.Helper()
	f := &fakeTokenServer{live: map[string]bool{initial: true}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serveToken))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTokenServer) serveToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forms = append(f.forms, r.PostForm)
	if id, _, ok := r.BasicAuth(); ok {
		f.basicIDs = append(f.basicIDs, id)
	}
	presented := r.PostForm.Get("refresh_token")
	if f.reject || !f.live[presented] {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":             "invalid_grant",
			"error_description": "refresh token is not active",
		})
		return
	}
	delete(f.live, presented)
	f.minted++
	next := fmt.Sprintf("refresh-%d", f.minted)
	f.live[next] = true
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  fmt.Sprintf("access-%d", f.minted),
		"token_type":    "Bearer",
		"expires_in":    3600,
		"refresh_token": next,
	})
}

// refreshes is how many refresh requests the server has seen, and the forms they carried.
func (f *fakeTokenServer) refreshes() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.forms...)
}

// storedRecord is a record bound to handlerEndpoint whose token endpoint is f's.
func (f *fakeTokenServer) storedRecord(expiry time.Time) Record {
	return Record{
		AccessToken:   staleAccess,
		RefreshToken:  staleRefresh,
		TokenType:     "Bearer",
		Expiry:        expiry,
		TokenEndpoint: f.srv.URL + "/token",
		Resource:      handlerEndpoint,
		Client:        Client{ID: "client-1", TokenEndpointAuthMethod: authMethodNone},
	}
}

// newTestHandler builds a handler over store for testServer, with loopback clients.
func newTestHandler(t *testing.T, store *Store) *Handler {
	t.Helper()
	h, err := NewHandler(HandlerConfig{
		Name:           testServer,
		Endpoint:       handlerEndpoint,
		Store:          store,
		EndpointClient: loopbackClient(),
		AuthClient:     loopbackClient(),
	})
	if err != nil {
		t.Fatalf("NewHandler error = %v", err)
	}
	return h
}

// currentToken draws the handler's token through its TokenSource under ctx.
func currentToken(t *testing.T, ctx context.Context, h *Handler) (string, error) {
	t.Helper()
	source, err := h.TokenSource(ctx)
	if err != nil {
		t.Fatalf("TokenSource error = %v", err)
	}
	token, err := source.Token()
	if err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

// challenge is a response to a request carrying bearer, with status and an optional challenge.
func challenge(bearer string, status int, wwwAuthenticate string) (*http.Request, *http.Response) {
	req := httptest.NewRequest(http.MethodPost, handlerEndpoint, nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp := &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}
	if wwwAuthenticate != "" {
		resp.Header.Set("WWW-Authenticate", wwwAuthenticate)
	}
	return req, resp
}

func TestHandlerServesAValidStoredTokenWithoutRefreshing(t *testing.T) {
	t.Parallel()
	tokens := newFakeTokenServer(t, staleRefresh)
	store := NewStore(t.TempDir())
	saveRecord(t, store, testServer, handlerEndpoint, tokens.storedRecord(time.Now().Add(time.Hour)))
	h := newTestHandler(t, store)

	got, err := currentToken(t, context.Background(), h)

	if err != nil || got != staleAccess {
		t.Fatalf("Token = %q, %v; want the stored %q", got, err, staleAccess)
	}
	if n := len(tokens.refreshes()); n != 0 {
		t.Errorf("refreshes = %d; want 0 for a valid token", n)
	}
}

func TestHandlerRefreshesAnExpiredTokenAndPersistsTheRotation(t *testing.T) {
	t.Parallel()
	tokens := newFakeTokenServer(t, staleRefresh)
	store := NewStore(t.TempDir())
	saveRecord(t, store, testServer, handlerEndpoint, tokens.storedRecord(time.Now().Add(-time.Minute)))
	h := newTestHandler(t, store)

	got, err := currentToken(t, context.Background(), h)

	if err != nil || got != "access-1" {
		t.Fatalf("Token = %q, %v; want the refreshed access-1", got, err)
	}
	forms := tokens.refreshes()
	if len(forms) != 1 {
		t.Fatalf("refreshes = %d; want exactly 1", len(forms))
	}
	want := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {staleRefresh},
		"resource":      {handlerEndpoint},
		"client_id":     {"client-1"},
	}
	if fmt.Sprint(forms[0]) != fmt.Sprint(want) {
		t.Errorf("refresh form = %v; want %v", forms[0], want)
	}
	saved, ok, err := store.Load(testServer, handlerEndpoint)
	if err != nil || !ok {
		t.Fatalf("Load = %v, %v; want the saved record", ok, err)
	}
	if saved.AccessToken != "access-1" || saved.RefreshToken != "refresh-1" || !saved.Expiry.After(time.Now()) {
		t.Errorf("saved = %q / %q / %v; want access-1 / the rotated refresh-1 / a future expiry",
			saved.AccessToken, saved.RefreshToken, saved.Expiry)
	}
}

func TestHandlerRefreshOutlivesACancelledContext(t *testing.T) {
	t.Parallel()
	tokens := newFakeTokenServer(t, staleRefresh)
	store := NewStore(t.TempDir())
	saveRecord(t, store, testServer, handlerEndpoint, tokens.storedRecord(time.Now().Add(-time.Minute)))
	h := newTestHandler(t, store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := currentToken(t, ctx, h)

	if err != nil || got != "access-1" {
		t.Fatalf("Token under a cancelled ctx = %q, %v; want the refreshed access-1", got, err)
	}
}

func TestHandlersSharingARecordNeverReuseASpentRefreshToken(t *testing.T) {
	t.Parallel()
	tokens := newFakeTokenServer(t, staleRefresh)
	store := NewStore(t.TempDir())
	saveRecord(t, store, testServer, handlerEndpoint, tokens.storedRecord(time.Now().Add(-time.Minute)))
	first := newTestHandler(t, store)
	second := newTestHandler(t, store)

	firstToken, firstErr := currentToken(t, context.Background(), first)
	secondToken, secondErr := currentToken(t, context.Background(), second)

	if firstErr != nil || secondErr != nil {
		t.Fatalf("Token errors = %v, %v; want none", firstErr, secondErr)
	}
	if secondToken != firstToken || len(tokens.refreshes()) != 1 {
		t.Fatalf("second handler = %q after %d refresh(es); want the first's %q from disk and 1 refresh",
			secondToken, len(tokens.refreshes()), firstToken)
	}

	req, resp := challenge(secondToken, http.StatusUnauthorized, `Bearer error="invalid_token"`)
	if err := second.Authorize(context.Background(), req, resp); err != nil {
		t.Fatalf("Authorize after a 401 = %v; want a refresh with the rotated token", err)
	}
	forms := tokens.refreshes()
	if len(forms) != 2 || forms[1].Get("refresh_token") != "refresh-1" {
		t.Errorf("refreshes = %v; want a second one presenting the rotated refresh-1", forms)
	}
}

func TestHandlerAuthorizeOnAForbidden(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		wwwAuthenticate string
		wantLogin       bool
	}{
		{name: "no challenge is the server's own refusal", wwwAuthenticate: ""},
		{name: "another error is the server's own refusal", wwwAuthenticate: `Bearer error="invalid_request"`},
		{name: "insufficient_scope needs a login", wwwAuthenticate: `Bearer error="insufficient_scope", scope="admin"`, wantLogin: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tokens := newFakeTokenServer(t, staleRefresh)
			store := NewStore(t.TempDir())
			saveRecord(t, store, testServer, handlerEndpoint, tokens.storedRecord(time.Now().Add(time.Hour)))
			h := newTestHandler(t, store)
			req, resp := challenge(staleAccess, http.StatusForbidden, tt.wwwAuthenticate)

			err := h.Authorize(context.Background(), req, resp)

			if got := errors.Is(err, ErrLoginRequired); got != tt.wantLogin {
				t.Errorf("Authorize = %v; want ErrLoginRequired %v", err, tt.wantLogin)
			}
			if !tt.wantLogin && err != nil {
				t.Errorf("Authorize = %v; want nil so the transport retries", err)
			}
			if n := len(tokens.refreshes()); n != 0 {
				t.Errorf("refreshes = %d; want none on a 403", n)
			}
		})
	}
}

func TestHandlerAuthorizeForcesOneRefreshOnA401(t *testing.T) {
	t.Parallel()
	tokens := newFakeTokenServer(t, staleRefresh)
	store := NewStore(t.TempDir())
	// A zero expiry never expires on its own: only the server's 401 retires the token.
	saveRecord(t, store, testServer, handlerEndpoint, tokens.storedRecord(time.Time{}))
	h := newTestHandler(t, store)
	req, resp := challenge(staleAccess, http.StatusUnauthorized, `Bearer error="invalid_token"`)

	err := h.Authorize(context.Background(), req, resp)

	if err != nil {
		t.Fatalf("Authorize = %v; want nil after a refresh", err)
	}
	got, err := currentToken(t, context.Background(), h)
	if err != nil || got != "access-1" || len(tokens.refreshes()) != 1 {
		t.Errorf("Token = %q, %v after %d refresh(es); want access-1 after exactly 1",
			got, err, len(tokens.refreshes()))
	}
}

func TestHandlerNeedsALogin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		record func(*fakeTokenServer) Record
		reject bool
	}{
		{
			name:   "the authorization server rejects the refresh",
			record: func(f *fakeTokenServer) Record { return f.storedRecord(time.Now().Add(-time.Minute)) },
			reject: true,
		},
		{
			name: "an expired token has no refresh token",
			record: func(f *fakeTokenServer) Record {
				r := f.storedRecord(time.Now().Add(-time.Minute))
				r.RefreshToken = ""
				return r
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tokens := newFakeTokenServer(t, staleRefresh)
			tokens.reject = tt.reject
			store := NewStore(t.TempDir())
			saveRecord(t, store, testServer, handlerEndpoint, tt.record(tokens))
			h := newTestHandler(t, store)

			_, err := currentToken(t, context.Background(), h)

			assertLoginRequired(t, err)
		})
	}
}

func TestNewHandlerWithoutARecordNeedsALogin(t *testing.T) {
	t.Parallel()
	store := NewStore(t.TempDir())

	_, err := NewHandler(HandlerConfig{
		Name:           testServer,
		Endpoint:       handlerEndpoint,
		Store:          store,
		EndpointClient: loopbackClient(),
		AuthClient:     loopbackClient(),
	})

	assertLoginRequired(t, err)
}

// assertLoginRequired fails the test unless err is ErrLoginRequired naming the login command.
func assertLoginRequired(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("error = %v; want ErrLoginRequired", err)
	}
	if want := "apogee mcp login " + testServer; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q; want it to name %q", err, want)
	}
}

func TestHandlerRefreshAuthenticatesTheClientAsRegistered(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		method     string
		wantForm   url.Values
		wantBasic  bool
		wantNoForm []string
	}{
		{
			name:       "public client sends its id alone",
			method:     authMethodNone,
			wantForm:   url.Values{"client_id": {"client-1"}},
			wantNoForm: []string{"client_secret"},
		},
		{
			name:     "client_secret_post sends the secret in the form",
			method:   authMethodSecretPost,
			wantForm: url.Values{"client_id": {"client-1"}, "client_secret": {"secret-1"}},
		},
		{
			name:       "client_secret_basic sends a Basic header",
			method:     authMethodSecretBasic,
			wantBasic:  true,
			wantNoForm: []string{"client_id", "client_secret"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tokens := newFakeTokenServer(t, staleRefresh)
			store := NewStore(t.TempDir())
			record := tokens.storedRecord(time.Now().Add(-time.Minute))
			record.Client.TokenEndpointAuthMethod = tt.method
			saveRecord(t, store, testServer, handlerEndpoint, record)
			h, err := NewHandler(HandlerConfig{
				Name:           testServer,
				Endpoint:       handlerEndpoint,
				Store:          store,
				EndpointClient: loopbackClient(),
				AuthClient:     loopbackClient(),
				ClientSecret:   "secret-1",
			})
			if err != nil {
				t.Fatalf("NewHandler error = %v", err)
			}

			if _, err := currentToken(t, context.Background(), h); err != nil {
				t.Fatalf("Token error = %v", err)
			}

			form := tokens.refreshes()[0]
			for key, values := range tt.wantForm {
				if form.Get(key) != values[0] {
					t.Errorf("form %s = %q; want %q", key, form.Get(key), values[0])
				}
			}
			for _, key := range tt.wantNoForm {
				if form.Has(key) {
					t.Errorf("form carries %s; want it absent", key)
				}
			}
			tokens.mu.Lock()
			gotBasic := len(tokens.basicIDs) == 1 && tokens.basicIDs[0] == "client-1"
			tokens.mu.Unlock()
			if gotBasic != tt.wantBasic {
				t.Errorf("Basic client-1 = %v; want %v", gotBasic, tt.wantBasic)
			}
		})
	}
}
