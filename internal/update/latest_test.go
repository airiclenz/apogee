package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// hostPlaceholder in a stand-in Location is replaced by the request's own scheme and host, so a
// case can ask for an absolute Location before the server's address is known.
const hostPlaceholder = "{host}"

// newReleaseServer starts a stand-in for the GitHub repository that answers every request with
// status and, when location is non-empty, that Location header. The returned channel carries the
// method and path of the first request, so a test can pin what Latest asked for.
func newReleaseServer(t *testing.T, status int, location string) (*httptest.Server, <-chan string) {
	t.Helper()
	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		select {
		case requests <- request.Method + " " + request.URL.Path:
		default:
		}
		if location != "" {
			writer.Header().Set("Location", strings.ReplaceAll(location, hostPlaceholder, "http://"+request.Host))
		}
		writer.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server, requests
}

func TestLatest_RedirectToReleaseTag_ReturnsTag(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		status   int
		location string
	}{
		{name: "302 relative Location", status: http.StatusFound, location: "/releases/tag/v0.25.0"},
		{name: "302 absolute Location", status: http.StatusFound, location: hostPlaceholder + "/releases/tag/v0.25.0"},
		{name: "301 under a repository path", status: http.StatusMovedPermanently, location: "/owner/repo/releases/tag/v0.25.0"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			server, requests := newReleaseServer(t, testCase.status, testCase.location)

			tag, err := Latest(context.Background(), NewClient(server.URL))

			if err != nil {
				t.Fatalf("Latest: unexpected error %v", err)
			}
			if tag != "v0.25.0" {
				t.Errorf("Latest = %q, want v0.25.0", tag)
			}
			if request := <-requests; request != "HEAD /releases/latest" {
				t.Errorf("request = %q, want HEAD /releases/latest", request)
			}
		})
	}
}

func TestLatest_NonRedirectOrBadLocation_ReturnsError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		status   int
		location string
	}{
		{name: "200 OK", status: http.StatusOK},
		{name: "404 Not Found", status: http.StatusNotFound},
		{name: "redirect without Location", status: http.StatusFound},
		{name: "redirect to releases index (nothing published)", status: http.StatusFound, location: "/releases"},
		{name: "redirect outside releases/tag", status: http.StatusFound, location: "/elsewhere/v0.25.0"},
		{name: "redirect to a non-semver tag", status: http.StatusFound, location: "/releases/tag/nightly"},
		{name: "redirect to a pre-release tag", status: http.StatusFound, location: "/releases/tag/v0.25.0-rc1"},
		{name: "redirect to a tag with build metadata", status: http.StatusFound, location: "/releases/tag/v0.25.0+build"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			server, _ := newReleaseServer(t, testCase.status, testCase.location)

			tag, err := Latest(context.Background(), NewClient(server.URL))

			if err == nil {
				t.Errorf("Latest = %q, want an error", tag)
			}
		})
	}
}

func TestLatest_ServerNeverAnswers_ReturnsErrorAtDeadline(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	tag, err := Latest(ctx, NewClient(server.URL))

	if err == nil {
		t.Errorf("Latest = %q, want a timeout error", tag)
	}
}

func TestLatest_ZeroClient_ReturnsError(t *testing.T) {
	t.Parallel()

	tag, err := Latest(context.Background(), Client{})

	if err == nil {
		t.Errorf("Latest = %q, want an error for an unbuilt Client", tag)
	}
}
