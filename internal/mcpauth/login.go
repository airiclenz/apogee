package mcpauth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"

	"github.com/airiclenz/apogee/internal/security"
)

// ErrNoAuthRequired is returned by Login when the server answered the unauthenticated probe
// without a 401: it does not ask for OAuth, so there is nothing to log in to.
var ErrNoAuthRequired = errors.New("the server did not ask for authorization")

// ErrStateMismatch is returned by Login when the authorization response's state is not the one
// the authorize URL carried — a response to some other request, which is never exchanged.
var ErrStateMismatch = errors.New("the authorization response's state does not match the request")

// Token endpoint authentication methods (RFC 7591 §2) a registration records.
const (
	authMethodNone        = "none"
	authMethodSecretPost  = "client_secret_post"
	authMethodSecretBasic = "client_secret_basic"
)

// s256 is the one PKCE code challenge method a login uses.
const s256 = "S256"

// resourceParam is the RFC 8707 parameter binding the token to the MCP server.
const resourceParam = "resource"

// registeredClientName is the client_name a Dynamic Client Registration asks for.
const registeredClientName = "apogee"

// probeBody is the unauthenticated request a login opens with: an MCP initialize, the first
// request any client sends, so a protected server answers it with its 401 challenge.
const probeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18",` +
	`"capabilities":{},"clientInfo":{"name":"apogee","version":"login"}}}`

// CodeFetcher shows the user authorizeURL and returns the authorization code and state the
// authorization server redirected back with. It owns all of the login's user interaction.
type CodeFetcher func(ctx context.Context, authorizeURL string) (code, state string, err error)

// LoginConfig is everything one server's login needs; the caller resolves all of it, so this
// package reaches for no config, environment or ambient home of its own.
type LoginConfig struct {
	// Name is the server's alias, the record's file stem (ValidateServerName).
	Name string
	// Endpoint is the server's vetted endpoint string (the form its transport is handed). It is
	// the RFC 8707 resource the token is bound to and what protected-resource metadata must name.
	Endpoint string
	// Store is where the record is read (a reusable registration) and saved.
	Store *Store
	// EndpointClient is the endpoint's own vetted, origin-pinned client: it carries the probe and
	// every request to the endpoint's origin.
	EndpointClient *http.Client
	// AuthClient carries every request to any other origin (NewAuthClient).
	AuthClient *http.Client
	// ClientID names a preregistered client; empty means Dynamic Client Registration.
	// ClientSecret is that client's secret, empty for a public client. It is never saved.
	ClientID     string
	ClientSecret string
	// RedirectURL is the redirect URI the authorization server sends the code to.
	RedirectURL string
	// FetchCode obtains the authorization code for the authorize URL.
	FetchCode CodeFetcher
}

// Login logs in to the OAuth-protected MCP server cfg names and saves the result: it probes the
// endpoint unauthenticated, follows the 401's WWW-Authenticate challenge or the RFC 9728
// well-known locations to protected-resource metadata, fetches the authorization server's RFC
// 8414 metadata, registers a client by Dynamic Client Registration unless cfg names one (or a
// stored registration still fits), runs the authorization code flow with PKCE S256, a state
// check and the RFC 8707 resource parameter, and exchanges the code. The record — the token, the
// issuer, token endpoint, resource and scopes a refresh needs, and the client — is saved to
// cfg.Store before it is returned.
//
// A server that answers the probe without a 401 is ErrNoAuthRequired; a mismatched state is
// ErrStateMismatch. Error text names the server and never carries more of the endpoint than its
// origin.
func Login(ctx context.Context, cfg LoginConfig) (Record, error) {
	if err := cfg.validate(); err != nil {
		return Record{}, err
	}
	record, err := (&loginFlow{cfg: cfg}).run(ctx)
	if err != nil {
		return Record{}, security.NewOriginRedactor(cfg.Endpoint).RedactErr(
			fmt.Errorf("mcpauth: log in to server %q: %w", cfg.Name, err),
		)
	}
	return record, nil
}

// validate refuses a config missing a piece the flow cannot run without, before any request.
func (cfg LoginConfig) validate() error {
	if err := ValidateServerName(cfg.Name); err != nil {
		return err
	}
	switch {
	case cfg.Endpoint == "":
		return fmt.Errorf("mcpauth: server %q: no endpoint to log in to", cfg.Name)
	case cfg.Store == nil, cfg.EndpointClient == nil, cfg.AuthClient == nil, cfg.FetchCode == nil:
		return fmt.Errorf("mcpauth: server %q: login needs a store, both HTTP clients and a code fetcher", cfg.Name)
	case cfg.RedirectURL == "":
		return fmt.Errorf("mcpauth: server %q: login needs a redirect URL", cfg.Name)
	}
	return nil
}

// loginFlow is one Login's run over its config.
type loginFlow struct {
	cfg LoginConfig
}

// run is Login's sequence, each step's failure returned unwrapped for Login to word.
func (f *loginFlow) run(ctx context.Context) (Record, error) {
	challenges, err := f.probe(ctx)
	if err != nil {
		return Record{}, err
	}
	prm, err := f.resourceMetadata(ctx, challenges)
	if err != nil {
		return Record{}, err
	}
	asm, err := f.authServerMetadata(ctx, prm.AuthorizationServers[0])
	if err != nil {
		return Record{}, err
	}
	client, secret, err := f.resolveClient(ctx, asm)
	if err != nil {
		return Record{}, err
	}
	record, err := f.authorize(ctx, asm, client, secret, requestedScopes(challenges, prm))
	if err != nil {
		return Record{}, err
	}
	if err := f.cfg.Store.Save(f.cfg.Name, f.cfg.Endpoint, record); err != nil {
		return Record{}, err
	}
	return record, nil
}

// clientFor is the client a request to rawURL goes over (routeClient).
func (f *loginFlow) clientFor(rawURL string) *http.Client {
	return routeClient(rawURL, f.cfg.Endpoint, f.cfg.EndpointClient, f.cfg.AuthClient)
}

// routeClient is the client a request to rawURL goes over: endpointClient for the endpoint's own
// origin (the operator named it, and a local server's origin is unreachable under the floor),
// authClient for any other. Login and the connected handler's refresh both route through it.
func routeClient(rawURL, endpoint string, endpointClient, authClient *http.Client) *http.Client {
	target, err := url.Parse(rawURL)
	if err != nil {
		return authClient
	}
	endpointURL, err := url.Parse(endpoint)
	if err != nil {
		return authClient
	}
	want, ok := security.CanonicalOrigin(endpointURL)
	if got, gotOK := security.CanonicalOrigin(target); ok && gotOK && got == want {
		return endpointClient
	}
	return authClient
}

// probe sends the unauthenticated initialize and returns the 401's WWW-Authenticate challenges.
// Any other answer below 500 is ErrNoAuthRequired; a server error is reported as one.
func (f *loginFlow) probe(ctx context.Context) ([]oauthex.Challenge, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.cfg.Endpoint, strings.NewReader(probeBody))
	if err != nil {
		return nil, fmt.Errorf("build the probe request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := f.cfg.EndpointClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("probe the endpoint: %w", err)
	}
	_ = resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
	case resp.StatusCode >= http.StatusInternalServerError:
		return nil, fmt.Errorf("probe the endpoint: the server answered HTTP %d", resp.StatusCode)
	default:
		return nil, fmt.Errorf("%w (it answered HTTP %d)", ErrNoAuthRequired, resp.StatusCode)
	}
	challenges, err := oauthex.ParseWWWAuthenticate(resp.Header.Values("WWW-Authenticate"))
	if err != nil {
		return nil, fmt.Errorf("parse the WWW-Authenticate challenge: %w", err)
	}
	return challenges, nil
}

// prmCandidates lists where the endpoint's protected-resource metadata may live, in the MCP
// specification's order: the challenge's resource_metadata URL, then the RFC 9728 well-known URL
// with the endpoint's path inserted, then the well-known URL at the root.
func prmCandidates(challenges []oauthex.Challenge, endpoint string) []string {
	var candidates []string
	for _, challenge := range challenges {
		if metadataURL := challenge.Params["resource_metadata"]; metadataURL != "" {
			candidates = append(candidates, metadataURL)
			break
		}
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return candidates
	}
	wellKnown := *u
	wellKnown.RawQuery, wellKnown.Fragment, wellKnown.RawPath = "", "", ""
	wellKnown.Path = "/.well-known/oauth-protected-resource/" + strings.TrimLeft(u.Path, "/")
	candidates = append(candidates, wellKnown.String())
	wellKnown.Path = "/.well-known/oauth-protected-resource"
	return append(candidates, wellKnown.String())
}

// resourceMetadata finds the endpoint's protected-resource metadata. Each candidate must name the
// endpoint itself as its resource (RFC 9728 §3.3); the first that does decides. With none found
// the 2025-03-26 fallback applies: the endpoint's origin is the authorization server.
func (f *loginFlow) resourceMetadata(
	ctx context.Context,
	challenges []oauthex.Challenge,
) (*oauthex.ProtectedResourceMetadata, error) {
	for _, candidate := range prmCandidates(challenges, f.cfg.Endpoint) {
		prm, err := oauthex.GetProtectedResourceMetadata(ctx, candidate, f.cfg.Endpoint, f.clientFor(candidate))
		if err != nil || prm == nil {
			// A candidate that is absent, malformed or names another resource is skipped, as the
			// specification's discovery order requires; the fallback below still applies.
			continue
		}
		if len(prm.AuthorizationServers) == 0 {
			return nil, errors.New("protected-resource metadata names no authorization server")
		}
		return prm, nil
	}

	u, err := url.Parse(f.cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse the endpoint: %w", err)
	}
	origin := url.URL{Scheme: u.Scheme, Host: u.Host}
	return &oauthex.ProtectedResourceMetadata{
		Resource:             f.cfg.Endpoint,
		AuthorizationServers: []string{origin.String()},
	}, nil
}

// authServerMetadata fetches the RFC 8414 (or OpenID) metadata of the authorization server at
// issuer. A server that publishes none gets the 2025-03-26 default endpoints under its root.
func (f *loginFlow) authServerMetadata(ctx context.Context, issuer string) (*oauthex.AuthServerMeta, error) {
	asm, err := auth.GetAuthServerMetadata(ctx, issuer, f.clientFor(issuer))
	if err != nil {
		return nil, fmt.Errorf("fetch the authorization server's metadata: %w", err)
	}
	if asm == nil {
		base := strings.TrimRight(issuer, "/")
		return &oauthex.AuthServerMeta{
			Issuer:                issuer,
			AuthorizationEndpoint: base + "/authorize",
			TokenEndpoint:         base + "/token",
			RegistrationEndpoint:  base + "/register",
		}, nil
	}
	if !slices.Contains(asm.CodeChallengeMethodsSupported, s256) {
		return nil, fmt.Errorf("the authorization server at %s does not support PKCE %s", issuer, s256)
	}
	return asm, nil
}

// resolveClient returns the client the login authorizes as, and its secret for the exchange: the
// preregistered client when cfg names one (its secret is never saved), else the stored
// registration when it was made at this registration endpoint for this redirect URL, else a new
// Dynamic Client Registration.
func (f *loginFlow) resolveClient(ctx context.Context, asm *oauthex.AuthServerMeta) (Client, string, error) {
	if f.cfg.ClientID != "" {
		method := preregisteredAuthMethod(asm.TokenEndpointAuthMethodsSupported, f.cfg.ClientSecret != "")
		return Client{ID: f.cfg.ClientID, TokenEndpointAuthMethod: method}, f.cfg.ClientSecret, nil
	}

	stored, ok, err := f.cfg.Store.Load(f.cfg.Name, f.cfg.Endpoint)
	if err != nil {
		return Client{}, "", err
	}
	if ok && stored.Client.ID != "" &&
		stored.Client.RegistrationEndpoint != "" &&
		stored.Client.RegistrationEndpoint == asm.RegistrationEndpoint &&
		slices.Contains(stored.Client.RedirectURIs, f.cfg.RedirectURL) {
		return stored.Client, stored.Client.Secret, nil
	}

	client, err := f.register(ctx, asm.RegistrationEndpoint)
	if err != nil {
		return Client{}, "", err
	}
	return client, client.Secret, nil
}

// register makes a Dynamic Client Registration (RFC 7591) at registrationEndpoint as a public
// native client for the redirect URL, and returns the registration as the record keeps it.
func (f *loginFlow) register(ctx context.Context, registrationEndpoint string) (Client, error) {
	if registrationEndpoint == "" {
		return Client{}, errors.New(
			"the authorization server offers no dynamic client registration; configure a client-id: for it",
		)
	}
	requested := &oauthex.ClientRegistrationMetadata{
		RedirectURIs:            []string{f.cfg.RedirectURL},
		TokenEndpointAuthMethod: authMethodNone,
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		ClientName:              registeredClientName,
		ApplicationType:         "native",
	}
	registered, err := oauthex.RegisterClient(ctx, registrationEndpoint, requested, f.clientFor(registrationEndpoint))
	if err != nil {
		return Client{}, fmt.Errorf("register a client: %w", err)
	}

	redirectURIs := registered.RedirectURIs
	if len(redirectURIs) == 0 {
		redirectURIs = requested.RedirectURIs
	}
	method := registered.TokenEndpointAuthMethod
	if method == "" {
		// RFC 7591 §2: an omitted method defaults to client_secret_basic — meaningful only when
		// the server issued a secret; a secretless registration is the public client asked for.
		method = authMethodNone
		if registered.ClientSecret != "" {
			method = authMethodSecretBasic
		}
	}
	return Client{
		ID:                      registered.ClientID,
		Secret:                  registered.ClientSecret,
		RegistrationEndpoint:    registrationEndpoint,
		RedirectURIs:            redirectURIs,
		TokenEndpointAuthMethod: method,
	}, nil
}

// preregisteredAuthMethod chooses how a preregistered client authenticates at the token
// endpoint: none for a public client; otherwise client_secret_post when offered (OAuth 2.1's
// preference), then client_secret_basic, which RFC 8414 also makes the default when the server
// lists no methods.
func preregisteredAuthMethod(supported []string, hasSecret bool) string {
	switch {
	case !hasSecret:
		return authMethodNone
	case slices.Contains(supported, authMethodSecretPost):
		return authMethodSecretPost
	default:
		return authMethodSecretBasic
	}
}

// authStyle maps a recorded token endpoint auth method to how the oauth2 package sends the
// client's credentials; "none" sends the client id in the form and no secret.
func authStyle(method string) oauth2.AuthStyle {
	switch method {
	case authMethodSecretBasic:
		return oauth2.AuthStyleInHeader
	case authMethodSecretPost, authMethodNone:
		return oauth2.AuthStyleInParams
	default:
		return oauth2.AuthStyleAutoDetect
	}
}

// requestedScopes is the scope a login asks for: the Bearer challenge's scope, else what the
// protected-resource metadata lists as supported, else none (the server's default).
func requestedScopes(challenges []oauthex.Challenge, prm *oauthex.ProtectedResourceMetadata) []string {
	for _, challenge := range challenges {
		if challenge.Scheme == "bearer" && challenge.Params["scope"] != "" {
			return strings.Fields(challenge.Params["scope"])
		}
	}
	return prm.ScopesSupported
}

// authorize runs the authorization code flow — PKCE S256, a fresh state, the resource parameter
// on both legs — and returns the record of the exchanged token.
func (f *loginFlow) authorize(
	ctx context.Context,
	asm *oauthex.AuthServerMeta,
	client Client,
	secret string,
	scopes []string,
) (Record, error) {
	config := &oauth2.Config{
		ClientID:     client.ID,
		ClientSecret: secret,
		Endpoint: oauth2.Endpoint{
			AuthURL:   asm.AuthorizationEndpoint,
			TokenURL:  asm.TokenEndpoint,
			AuthStyle: authStyle(client.TokenEndpointAuthMethod),
		},
		RedirectURL: f.cfg.RedirectURL,
		Scopes:      scopes,
	}
	verifier := oauth2.GenerateVerifier()
	state := rand.Text()
	resource := oauth2.SetAuthURLParam(resourceParam, f.cfg.Endpoint)

	authorizeURL := config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), resource)
	code, gotState, err := f.cfg.FetchCode(ctx, authorizeURL)
	if err != nil {
		return Record{}, err
	}
	if gotState != state {
		return Record{}, ErrStateMismatch
	}
	if code == "" {
		return Record{}, errors.New("the authorization response carried no code")
	}

	exchangeCtx := context.WithValue(ctx, oauth2.HTTPClient, f.clientFor(asm.TokenEndpoint))
	token, err := config.Exchange(exchangeCtx, code, oauth2.VerifierOption(verifier), resource)
	if err != nil {
		return Record{}, fmt.Errorf("exchange the authorization code: %w", err)
	}
	return Record{
		AccessToken:   token.AccessToken,
		RefreshToken:  token.RefreshToken,
		TokenType:     token.Type(),
		Expiry:        token.Expiry,
		Issuer:        asm.Issuer,
		TokenEndpoint: asm.TokenEndpoint,
		Resource:      f.cfg.Endpoint,
		Scopes:        grantedScopes(token, scopes),
		Client:        client,
	}, nil
}

// grantedScopes is the scope the token response reports (RFC 6749 §5.1), or the requested scope
// when the response omits it — which the RFC says means the request was granted as asked.
func grantedScopes(token *oauth2.Token, requested []string) []string {
	if granted, ok := token.Extra("scope").(string); ok && granted != "" {
		return strings.Fields(granted)
	}
	return requested
}
