package mcpauth

import "net/http"

// Binding is the OAuth facts one server's login and its connected handler share: which server,
// at which endpoint, over which clients, as which client and under which issuer. LoginConfig and
// HandlerConfig both embed it, so the two read one set of facts; the caller resolves all of it,
// so this package reaches for no config, environment or ambient home of its own.
type Binding struct {
	// Name is the server's alias, the record's file stem (ValidateServerName).
	Name string
	// Endpoint is the server's vetted endpoint string (the form its transport is handed). It is
	// the RFC 8707 resource the token is bound to and what protected-resource metadata must name.
	Endpoint string
	// Store holds the server's record: a login reads a reusable registration from it and saves
	// the result; the handler loads the record and saves refreshed tokens back. A record issued to
	// another client than ClientID names is no record at all (clientMatches): the handler never
	// sends a token, or spends a refresh token, minted for a client the configuration dropped.
	Store *Store
	// EndpointClient is the endpoint's own vetted, origin-pinned client: it carries a login's
	// probe and every request to the endpoint's origin, including a refresh to a token endpoint
	// there. AuthClient carries every request to any other origin (NewAuthClient) — routeClient.
	EndpointClient *http.Client
	AuthClient     *http.Client
	// ClientID is the server's configured `client-id:`, the preregistered client a login
	// authorizes as; empty when apogee registers its own client (Dynamic Client Registration).
	ClientID string
	// ClientSecret is a preregistered client's secret (its `client-secret-env:` value), empty for
	// a public client; a refresh sends it when the record's registration carries none. It is
	// never saved.
	ClientSecret string
	// Issuer, when set, is the one authorization server the server's tokens may come from: a
	// login fails with IssuerMismatchError unless the endpoint names it, and a record another
	// authorization server issued — or one that names none — is no record at all (issuerMatches),
	// so no refresh, and no client secret, is ever sent to a token endpoint the operator did not
	// pin. Empty means no pin — a login uses the first authorization server the endpoint names —
	// and is refused beside a ClientSecret.
	Issuer string
}

// validate refuses a binding whose shared facts cannot be used: an unsafe server name, or a client
// secret without a configured issuer (requireSecretIssuer). Login and NewHandler both run it,
// since an embedder Host reaches this package directly; each config checks its own pieces beside
// it.
func (b Binding) validate() error {
	if err := ValidateServerName(b.Name); err != nil {
		return err
	}
	return requireSecretIssuer(b.Name, b.ClientSecret, b.Issuer)
}
