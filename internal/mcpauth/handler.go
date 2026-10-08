package mcpauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"

	"github.com/airiclenz/apogee/internal/platform"
)

// ErrLoginRequired is what every error a Handler returns for a server that needs an interactive
// login wraps: no stored record, a stored token that cannot be refreshed, an authorization server
// that rejected the refresh, or a server that asks for a scope the token lacks. The error's text
// names the `apogee mcp login <name>` command; nothing in this package ever prompts.
var ErrLoginRequired = errors.New("an OAuth login is required")

// refreshTimeout bounds one refresh, whichever client carries it: the auth client has its own
// timeout, the endpoint's client (a token endpoint on the endpoint's own origin) has none.
const refreshTimeout = authClientTimeout

// lockWait is how long a refresh queues behind another holder of the record's lock — another
// handler in this process or another apogee — before it gives up: one holder's refresh at most.
const lockWait = refreshTimeout + 5*time.Second

// lockSuffix names a record's lock file beside it; the leading dot keeps it out of a listing.
const lockSuffix = ".lock"

// insufficientScope is the Bearer challenge error (RFC 6750 §3.1) that asks for a step-up login.
const insufficientScope = "insufficient_scope"

// HandlerConfig is everything one server's connected handler needs; the caller resolves all of
// it, as for Login.
type HandlerConfig struct {
	// Name is the server's alias, the record's file stem (ValidateServerName).
	Name string
	// Endpoint is the server's vetted endpoint string, the one Login was given.
	Endpoint string
	// Store holds the server's record; refreshed tokens are saved back to it.
	Store *Store
	// EndpointClient carries a refresh to a token endpoint on the endpoint's own origin, and
	// AuthClient one to any other origin (NewAuthClient) — the routing Login uses.
	EndpointClient *http.Client
	AuthClient     *http.Client
	// ClientSecret is a preregistered client's secret (its `client-secret-env:` value), sent on a
	// refresh when the record's registration carries none. It is never saved.
	ClientSecret string
}

// Handler is the SDK's auth.OAuthHandler for one `auth: oauth` server: it hands the transport the
// stored bearer token, refreshes it when it expires or the server refuses it, and persists a
// rotated refresh token before the new access token is used. It never prompts: a server that
// needs a login gets an error wrapping ErrLoginRequired. It is safe for concurrent use, and
// handlers in this process or another that share a record coordinate through a lock file beside
// it, so a refresh token one of them spent is never sent again by another.
type Handler struct {
	cfg HandlerConfig

	mu     sync.Mutex
	record Record
}

// NewHandler loads cfg's record and returns the handler over it. A missing record — never logged
// in, logged out, or minted for another endpoint — is ErrLoginRequired; an unsafe name, a missing
// store or client, and an unreadable record are plain errors.
func NewHandler(cfg HandlerConfig) (*Handler, error) {
	if err := ValidateServerName(cfg.Name); err != nil {
		return nil, err
	}
	if cfg.Store == nil || cfg.EndpointClient == nil || cfg.AuthClient == nil {
		return nil, fmt.Errorf("mcpauth: server %q: the OAuth handler needs a store and both HTTP clients", cfg.Name)
	}
	record, ok, err := cfg.Store.Load(cfg.Name, cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	if !ok || record.AccessToken == "" {
		return nil, loginRequired(cfg.Name, "no stored token for this endpoint", nil)
	}
	return &Handler{cfg: cfg, record: record}, nil
}

// TokenSource returns the source the transport draws each request's bearer token from. ctx is
// the connection's context: a refresh the source runs keeps its values but never its
// cancellation, so a refresh that a closing connection triggers still completes and is saved.
func (h *Handler) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	return handlerTokenSource{handler: h, ctx: ctx}, nil
}

// Authorize answers a 401 or 403 the endpoint returned to req. A 403 that is not an
// insufficient_scope challenge is the server's own refusal, not an authorization problem: Authorize
// returns nil and the transport's retry surfaces it. An insufficient_scope 403 needs a new login.
// A 401 forces one refresh — or adopts a newer token another handler saved — and the transport
// retries with it; a 401 that no refresh can answer is ErrLoginRequired. Authorize closes resp's
// body.
func (h *Handler) Authorize(ctx context.Context, req *http.Request, resp *http.Response) error {
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxAuthResponseBytes))

	if resp.StatusCode == http.StatusForbidden {
		challenges, err := oauthex.ParseWWWAuthenticate(resp.Header.Values("WWW-Authenticate"))
		if err != nil || challengeError(challenges) != insufficientScope {
			// A malformed challenge is no step-up request either; the retry reports the 403.
			return nil
		}
		return loginRequired(h.cfg.Name, "the server asks for a scope the stored token lacks", nil)
	}

	rejected := strings.TrimSpace(strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer "))
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.refreshLocked(ctx, rejected)
}

// handlerTokenSource is the oauth2.TokenSource a Handler gives the transport, bound to the
// connection's context.
type handlerTokenSource struct {
	handler *Handler
	ctx     context.Context
}

// Token returns the current access token, refreshing it first when it has expired.
func (s handlerTokenSource) Token() (*oauth2.Token, error) {
	return s.handler.token(s.ctx)
}

// token is the current access token, refreshed first when it is no longer valid.
func (h *Handler) token(ctx context.Context) (*oauth2.Token, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !tokenValid(h.record) {
		if err := h.refreshLocked(ctx, ""); err != nil {
			return nil, err
		}
	}
	return &oauth2.Token{
		AccessToken: h.record.AccessToken,
		TokenType:   h.record.TokenType,
		Expiry:      h.record.Expiry,
	}, nil
}

// refreshLocked replaces h.record with a usable one; h.mu is held. rejected is the access token
// the server refused, empty when the held token merely expired. Under the record's lock it
// re-reads the record from disk first: a valid token there that is not the rejected one was saved
// by another handler and is adopted as is. Otherwise the disk record's refresh token — the newest
// one, never a copy this handler may hold after another spent it — is exchanged, and the result is
// saved before it is used.
func (h *Handler) refreshLocked(ctx context.Context, rejected string) error {
	release, err := h.lockRecord()
	if err != nil {
		return err
	}
	defer release()

	disk, ok, err := h.cfg.Store.Load(h.cfg.Name, h.cfg.Endpoint)
	if err != nil {
		return err
	}
	if !ok {
		return loginRequired(h.cfg.Name, "the stored token is gone", nil)
	}
	if tokenValid(disk) && disk.AccessToken != rejected {
		h.record = disk
		return nil
	}
	if disk.RefreshToken == "" {
		return loginRequired(h.cfg.Name, "the token is no longer accepted and there is no refresh token", nil)
	}

	refreshed, err := h.requestRefresh(ctx, disk)
	if err != nil {
		return err
	}
	h.record = refreshed
	if err := h.cfg.Store.Save(h.cfg.Name, h.cfg.Endpoint, refreshed); err != nil {
		return fmt.Errorf("mcpauth: server %q: save the refreshed token: %w", h.cfg.Name, err)
	}
	return nil
}

// lockRecord takes the lock file beside the server's record, which every handler sharing the
// record — in this process or another — refreshes under, and returns its release.
func (h *Handler) lockRecord() (func(), error) {
	if err := os.MkdirAll(h.cfg.Store.dir, dirPerm); err != nil {
		return nil, fmt.Errorf("mcpauth: create token dir %q: %w", h.cfg.Store.dir, err)
	}
	path := filepath.Join(h.cfg.Store.dir, "."+h.cfg.Name+lockSuffix)
	release, err := platform.AcquireLockWait(path, lockWait)
	if err != nil {
		return nil, fmt.Errorf("mcpauth: server %q: lock the token record: %w", h.cfg.Name, err)
	}
	return release, nil
}

// tokenResponse is the token endpoint's successful answer (RFC 6749 §5.1). ExpiresIn is a
// json.Number because some servers send it as a string.
type tokenResponse struct {
	AccessToken  string      `json:"access_token"`
	TokenType    string      `json:"token_type"`
	ExpiresIn    json.Number `json:"expires_in"`
	RefreshToken string      `json:"refresh_token"`
	Scope        string      `json:"scope"`
}

// tokenErrorResponse is the token endpoint's error answer (RFC 6749 §5.2).
type tokenErrorResponse struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

// requestRefresh exchanges record's refresh token at its stored token endpoint, sending the stored
// resource and the client's credentials the way its registration recorded, and returns record
// with the new token. It runs on ctx's values without its cancellation, bounded by
// refreshTimeout. A 4xx answer is the authorization server rejecting the refresh, which is
// ErrLoginRequired; a transport failure or any other answer is a plain error.
func (h *Handler) requestRefresh(ctx context.Context, record Record) (Record, error) {
	if record.TokenEndpoint == "" {
		return Record{}, loginRequired(h.cfg.Name, "the stored token names no token endpoint", nil)
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshTimeout)
	defer cancel()

	req, err := h.refreshRequest(ctx, record)
	if err != nil {
		return Record{}, err
	}
	client := routeClient(record.TokenEndpoint, h.cfg.Endpoint, h.cfg.EndpointClient, h.cfg.AuthClient)
	resp, err := client.Do(req)
	if err != nil {
		return Record{}, fmt.Errorf("mcpauth: server %q: refresh the token: %w", h.cfg.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(http.MaxBytesReader(nil, resp.Body, maxAuthResponseBytes))
	if err != nil {
		return Record{}, fmt.Errorf("mcpauth: server %q: read the refresh answer: %w", h.cfg.Name, err)
	}

	switch {
	case resp.StatusCode >= http.StatusBadRequest && resp.StatusCode < http.StatusInternalServerError:
		return Record{}, loginRequired(h.cfg.Name, "the authorization server rejected the refresh", refreshRejection(resp.StatusCode, body))
	case resp.StatusCode != http.StatusOK:
		return Record{}, fmt.Errorf("mcpauth: server %q: refresh the token: the token endpoint answered HTTP %d",
			h.cfg.Name, resp.StatusCode)
	}
	return h.applyRefresh(record, body)
}

// refreshRequest builds the refresh POST: grant_type=refresh_token, the refresh token and the RFC
// 8707 resource, with the client authenticated per its recorded token endpoint auth method — the
// secret in a Basic header (client_secret_basic, both halves form-encoded per RFC 6749 §2.3.1) or
// in the form (client_secret_post), else the client id alone.
func (h *Handler) refreshRequest(ctx context.Context, record Record) (*http.Request, error) {
	resource := record.Resource
	if resource == "" {
		resource = h.cfg.Endpoint
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {record.RefreshToken},
		resourceParam:   {resource},
	}
	secret := record.Client.Secret
	if secret == "" {
		secret = h.cfg.ClientSecret
	}
	useBasic := record.Client.TokenEndpointAuthMethod == authMethodSecretBasic && secret != ""
	if !useBasic {
		form.Set("client_id", record.Client.ID)
		if secret != "" && record.Client.TokenEndpointAuthMethod != authMethodNone {
			form.Set("client_secret", secret)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, record.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("mcpauth: server %q: build the refresh request: %w", h.cfg.Name, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if useBasic {
		req.SetBasicAuth(url.QueryEscape(record.Client.ID), url.QueryEscape(secret))
	}
	return req, nil
}

// applyRefresh decodes a successful refresh answer onto record: the new access token and its
// expiry, the rotated refresh token when the server issued one (else the old one stays), and the
// granted scope when the answer reports it.
func (h *Handler) applyRefresh(record Record, body []byte) (Record, error) {
	var answer tokenResponse
	if err := json.Unmarshal(body, &answer); err != nil {
		return Record{}, fmt.Errorf("mcpauth: server %q: decode the refresh answer: %w", h.cfg.Name, err)
	}
	if answer.AccessToken == "" {
		return Record{}, fmt.Errorf("mcpauth: server %q: the refresh answer carried no access token", h.cfg.Name)
	}

	record.AccessToken = answer.AccessToken
	record.TokenType = answer.TokenType
	record.Expiry = time.Time{}
	if seconds, err := answer.ExpiresIn.Int64(); err == nil && seconds > 0 {
		record.Expiry = time.Now().Add(time.Duration(seconds) * time.Second)
	}
	if answer.RefreshToken != "" {
		record.RefreshToken = answer.RefreshToken
	}
	if answer.Scope != "" {
		record.Scopes = strings.Fields(answer.Scope)
	}
	return record, nil
}

// refreshRejection words a rejected refresh's answer: the OAuth error code and description when
// the body carries them, else the bare status.
func refreshRejection(status int, body []byte) error {
	var answer tokenErrorResponse
	if json.Unmarshal(body, &answer) != nil || answer.Error == "" {
		return fmt.Errorf("the token endpoint answered HTTP %d", status)
	}
	if answer.Description == "" {
		return fmt.Errorf("the token endpoint answered HTTP %d: %s", status, answer.Error)
	}
	return fmt.Errorf("the token endpoint answered HTTP %d: %s: %s", status, answer.Error, answer.Description)
}

// tokenValid reports whether record's access token may still be sent: present and unexpired by
// oauth2's own margin. A zero expiry never expires — only a 401 retires such a token.
func tokenValid(record Record) bool {
	return (&oauth2.Token{AccessToken: record.AccessToken, Expiry: record.Expiry}).Valid()
}

// challengeError is the error parameter of the first Bearer challenge that carries one.
func challengeError(challenges []oauthex.Challenge) string {
	for _, challenge := range challenges {
		if challenge.Scheme == "bearer" && challenge.Params["error"] != "" {
			return challenge.Params["error"]
		}
	}
	return ""
}

// LoginRequiredError is the error a Handler returns for a server that needs an interactive login.
// It matches ErrLoginRequired under errors.Is and unwraps to its cause, when it has one.
type LoginRequiredError struct {
	// Server is the server's alias, the one `apogee mcp login` takes.
	Server string
	// Reason says why the stored token cannot be used.
	Reason string
	// Cause is the failure behind Reason, nil when there is none.
	Cause error
}

// Error names the server, the reason, the cause when there is one, and the command to run.
func (e *LoginRequiredError) Error() string {
	reason := e.Reason
	if e.Cause != nil {
		reason += ": " + e.Cause.Error()
	}
	return fmt.Sprintf("mcpauth: server %q needs an OAuth login (%s) — run `apogee mcp login %s`",
		e.Server, reason, e.Server)
}

// Is reports whether target is ErrLoginRequired.
func (e *LoginRequiredError) Is(target error) bool {
	return target == ErrLoginRequired
}

// Unwrap returns the cause.
func (e *LoginRequiredError) Unwrap() error {
	return e.Cause
}

// loginRequired builds the LoginRequiredError for the server name.
func loginRequired(name, reason string, cause error) error {
	return &LoginRequiredError{Server: name, Reason: reason, Cause: cause}
}
