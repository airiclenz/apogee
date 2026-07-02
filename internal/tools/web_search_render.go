package tools

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// ----------------------------------------------------------------------------
// web_search rendering — DuckDuckGo HTML parsing + generic HTML cleaning
// ----------------------------------------------------------------------------
//
// The built-in DuckDuckGo provider returns a full HTML page; parseDDGResults extracts
// structured title/url/snippet results from it. A custom endpoint may return HTML too
// (parsed structurally when it is a DDG mirror, cleaned to plain text otherwise) or an
// already-clean JSON/text document (passed through verbatim). All parsing is best-effort
// by design: a rate-limit or consent page carries no result anchors and renders as
// "No results", never a crash. If DDG ever reorders the anchor attributes (href before
// class), parsing degrades to zero results — graceful, and the first place to look.

const (
	// ddgParseCap bounds how many result anchors are parsed from a page (a 2 MB body could
	// carry far more than anyone renders).
	ddgParseCap = 20
	// ddgRenderMax is how many parsed results reach the model.
	ddgRenderMax = 5
)

var (
	ddgResultLinkRE = regexp.MustCompile(`(?s)<a[^>]+class="result__a"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	ddgSnippetRE    = regexp.MustCompile(`(?s)<a[^>]+class="result__snippet"[^>]*>(.*?)</a>`)
	ddgUddgRE       = regexp.MustCompile(`uddg=([^&]+)`)
	htmlTagRE       = regexp.MustCompile(`(?s)<[^>]+>`)
)

// searchResult is one parsed search hit.
type searchResult struct {
	title   string
	url     string
	snippet string
}

// renderSearch decides how a 2xx search response reaches the model (non-2xx is handled by
// Execute as an errorResult before dispatch):
//   - DuckDuckGo (the default): ALWAYS the structured parse — a page with no result anchors
//     (rate-limit, consent challenge, redirect stub) renders "No results", never its raw
//     text, which would be noise.
//   - custom endpoint, HTML response (Content-Type or body sniff): the structured parse
//     first (covers a self-hosted DDG mirror), generic tag-stripping otherwise.
//   - custom endpoint, non-HTML response: verbatim pass-through — the backend's own clean
//     JSON/text document, exactly as before.
func renderSearch(provider searchProvider, resp *http.Response, body, query string, truncated bool) string {
	if provider == providerDuckDuckGo {
		results := parseDDGResults(body)
		if len(results) == 0 {
			return "No results found for: " + query
		}
		return renderStructuredResults(results)
	}
	if contentLooksHTML(resp.Header.Get("Content-Type"), body) {
		if results := parseDDGResults(body); len(results) > 0 {
			return renderStructuredResults(results)
		}
		cleaned := cleanHTMLText(body)
		if cleaned == "" {
			return "No results found for: " + query
		}
		if truncated {
			cleaned += fmt.Sprintf("\n\n[results truncated at %d bytes]", maxNetworkResponseBytes)
		}
		return cleaned
	}
	return renderSearchResult(resp, body, truncated)
}

// parseDDGResults extracts structured results from a DuckDuckGo-shaped HTML page: result
// anchors (class "result__a") paired by position with snippet anchors (class
// "result__snippet"). Best-effort: entries with an empty title or a dead URL are skipped,
// and a page with no anchors yields nil.
func parseDDGResults(body string) []searchResult {
	links := ddgResultLinkRE.FindAllStringSubmatch(body, ddgParseCap)
	if len(links) == 0 {
		return nil
	}
	snippets := ddgSnippetRE.FindAllStringSubmatch(body, ddgParseCap)

	results := make([]searchResult, 0, len(links))
	for i, m := range links {
		title := cleanHTMLText(m[2])
		target := ddgRealURL(m[1])
		if title == "" || target == "" {
			continue
		}
		snippet := ""
		if i < len(snippets) {
			snippet = cleanHTMLText(snippets[i][1])
		}
		results = append(results, searchResult{title: title, url: target, snippet: snippet})
	}
	return results
}

// ddgRealURL unwraps DuckDuckGo's //duckduckgo.com/l/?uddg=<escaped-url> redirector to the
// target URL. Fallback is the raw href — unless that is itself a bare redirector, which is
// useless to the model and drops the result (empty return).
func ddgRealURL(href string) string {
	if m := ddgUddgRE.FindStringSubmatch(href); m != nil {
		if real, err := url.QueryUnescape(m[1]); err == nil && real != "" {
			return real
		}
	}
	if strings.Contains(href, "duckduckgo.com/l/") {
		return ""
	}
	return href
}

// cleanHTMLText strips tags from an HTML fragment and normalizes it to a single line: tags
// become spaces, entities decode (stdlib html covers the full named set; &nbsp; becomes
// U+00A0, which unicode counts as space), and whitespace runs collapse to single spaces.
func cleanHTMLText(s string) string {
	s = htmlTagRE.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

// contentLooksHTML reports whether a response is HTML, by Content-Type or by sniffing the
// body prefix (a backend that omits the header still gets cleaned rather than dumped raw).
func contentLooksHTML(contentType, body string) bool {
	if strings.Contains(strings.ToLower(contentType), "html") {
		return true
	}
	head := body
	if len(head) > 512 {
		head = head[:512]
	}
	head = strings.ToLower(strings.TrimSpace(head))
	return strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html")
}

// renderStructuredResults renders parsed results as a numbered title/url/snippet list,
// capped at ddgRenderMax.
func renderStructuredResults(results []searchResult) string {
	if len(results) > ddgRenderMax {
		results = results[:ddgRenderMax]
	}
	var b strings.Builder
	for i, r := range results {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "%d. %s\n   %s", i+1, r.title, r.url)
		if r.snippet != "" {
			fmt.Fprintf(&b, "\n   %s", r.snippet)
		}
	}
	return b.String()
}

// renderSearchResult is the verbatim pass-through for a custom endpoint's non-HTML (clean)
// response: a status line and the (capped) raw body, the backend's own result document.
func renderSearchResult(resp *http.Response, body string, truncated bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP %s\n\n", resp.Status)
	b.WriteString(body)
	if truncated {
		fmt.Fprintf(&b, "\n\n[results truncated at %d bytes]", maxNetworkResponseBytes)
	}
	return b.String()
}
