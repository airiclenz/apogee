// Package update asks GitHub whether a newer apogee release has been published.
//
// It is the only package that holds apogee's own update traffic, and cmd/apogee is the only
// package that reaches it: the embeddable engine stays wire-silent (ADR 0031). The request goes to
// apogee's fixed release URL, never to a model-chosen one, so it does not pass through
// security.URLGuard, which guards model-driven requests.
//
// [Latest] reads the newest published release's tag from the redirect GitHub answers
// `HEAD /releases/latest` with — no API call, so no API rate limit — and [Newer] orders that tag
// against the running version.
package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"
)

// DefaultBaseURL is the repository whose published releases the lookup reads.
const DefaultBaseURL = "https://github.com/airiclenz/apogee"

// lookupTimeout bounds one lookup end to end. The check runs off the boot path, so this only
// stops a stalled connection from holding its goroutine; it never delays the user.
const lookupTimeout = 5 * time.Second

// latestReleasePath answers with a redirect to the newest published (non-draft, non-pre-release)
// release's page.
const latestReleasePath = "/releases/latest"

// releaseTagDirectory is the path a release page lives under: `<repo>/releases/tag/<tag>`.
const releaseTagDirectory = "/releases/tag"

// Client is the HTTP side of a lookup: the repository base URL and an http.Client that hands
// redirects back unfollowed. Build one with [NewClient]; the zero value is not usable.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient returns a Client for the repository at baseURL (normally [DefaultBaseURL]). Its
// http.Client honours HTTP_PROXY/HTTPS_PROXY/NO_PROXY, never follows a redirect and gives up after
// five seconds.
func NewClient(baseURL string) Client {
	return Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Transport: &http.Transport{Proxy: http.ProxyFromEnvironment},
			// The redirect IS the answer: following it would fetch a whole HTML page to learn
			// nothing the Location header did not already say.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Timeout: lookupTimeout,
		},
	}
}

// Latest returns the `vX.Y.Z` tag of the newest published release. It sends
// `HEAD <base>/releases/latest` and reads the tag from the last segment of the 3xx answer's
// Location (`<base>/releases/tag/vX.Y.Z`; a relative Location resolves against the request).
// Every other outcome is an error: a transport failure or timeout, a non-3xx status, a missing
// Location, a Location outside `/releases/tag/`, or a final segment that is not a release tag —
// GitHub redirects to `/releases` itself when nothing is published yet. Cancelling ctx aborts the
// request.
func Latest(ctx context.Context, client Client) (string, error) {
	if client.httpClient == nil {
		return "", errors.New("update: client not built with NewClient")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodHead, client.baseURL+latestReleasePath, nil)
	if err != nil {
		return "", fmt.Errorf("update: build latest-release request: %w", err)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("update: look up latest release: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	return tagFromRedirect(response)
}

// tagFromRedirect extracts the release tag from a /releases/latest response.
func tagFromRedirect(response *http.Response) (string, error) {
	if response.StatusCode < http.StatusMultipleChoices || response.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("update: latest release: want a redirect, got %s", response.Status)
	}
	location, err := response.Location()
	if err != nil {
		return "", fmt.Errorf("update: latest release redirect: %w", err)
	}

	directory, tag := path.Split(location.Path)
	if !strings.HasSuffix(strings.TrimSuffix(directory, "/"), releaseTagDirectory) {
		return "", fmt.Errorf("update: latest release redirect %q is not a release page", location.Path)
	}
	if !releaseTagPattern.MatchString(tag) {
		return "", fmt.Errorf("update: latest release redirect names %q, not a vX.Y.Z tag", tag)
	}
	return tag, nil
}
