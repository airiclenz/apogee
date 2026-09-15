package tools

import (
	"html"
	"regexp"
	"strings"
)

// htmlTextMode selects how much of an HTML document's structure cleanHTMLText keeps.
type htmlTextMode int

const (
	// htmlFragment cleans a snippet lifted out of a page — a search result's title or
	// snippet — into ONE line: tags become spaces, entities decode, whitespace collapses.
	// It is web_search's form and its output is pinned byte-for-byte
	// (TestCleanHTMLText_FragmentFormIsUnchanged).
	htmlFragment htmlTextMode = iota
	// htmlPage cleans a whole document for reading: <script>, <style> and <noscript> bodies
	// and comments are dropped, block-level tags become line breaks, a <pre> keeps its own
	// lines and indentation, and every other tag becomes a space. It is web_fetch's form.
	htmlPage
)

var (
	htmlTagRE = regexp.MustCompile(`(?s)<[^>]+>`)
	// htmlSkippedRE matches the elements whose BODIES are not page text — code the browser
	// runs, rules it applies, the fallback it shows without a script engine — and comments.
	// Go's regexp has no back-references, so each element is spelled out.
	htmlSkippedRE = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>|<style\b[^>]*>.*?</style\s*>|<noscript\b[^>]*>.*?</noscript\s*>|<!--.*?-->`)
	htmlPreRE     = regexp.MustCompile(`(?is)<pre\b[^>]*>.*?</pre\s*>`)
	// htmlBlockTagRE matches the tags that start or end a block of text in the page's own
	// reading order; each becomes a line break so paragraphs, headings, list items and rows
	// do not run into one line.
	htmlBlockTagRE = regexp.MustCompile(`(?i)</?(?:p|div|h[1-6]|li|ul|ol|dl|dt|dd|tr|table|thead|tbody|tfoot|br|hr|blockquote|pre|section|article|header|footer|nav|aside|main|title|form|fieldset|figure|figcaption|details|summary|address|option|select|textarea)\b[^>]*>`)
)

// The page cleaner works with sentinel runes so one whitespace pass can collapse markup
// indentation without losing the structure it must keep: a block break is marked with
// htmlBreak (the carriage return, which the CRLF normalisation has already removed from the
// text) and the spaces and tabs INSIDE a <pre> are swapped for two private-use runes so the
// collapse leaves a code block's indentation alone and the final pass restores them.
const (
	htmlBreak    = "\r"
	htmlPreSpace = "\uE000"
	htmlPreTab   = "\uE001"
)

// cleanHTMLText turns HTML into text the model can read. In htmlFragment mode the input is a
// snippet and the result is a single line (tags become spaces, entities decode — stdlib html
// covers the full named set; &nbsp; becomes U+00A0, which unicode counts as space — and
// whitespace runs collapse to single spaces). In htmlPage mode the input is a whole document:
// script, style and noscript bodies and comments are dropped, block tags become line breaks,
// a <pre> keeps its lines and indentation, blank lines are dropped, and each remaining line
// has its whitespace collapsed.
func cleanHTMLText(s string, mode htmlTextMode) string {
	if mode == htmlFragment {
		s = htmlTagRE.ReplaceAllString(s, " ")
		s = html.UnescapeString(s)
		return strings.Join(strings.Fields(s), " ")
	}

	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = htmlSkippedRE.ReplaceAllString(s, htmlBreak)
	s = htmlPreRE.ReplaceAllStringFunc(s, protectPreformatted)
	s = htmlBlockTagRE.ReplaceAllString(s, htmlBreak)
	s = htmlTagRE.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)

	var b strings.Builder
	b.Grow(len(s))
	for _, segment := range strings.Split(s, htmlBreak) {
		line := strings.Join(strings.Fields(segment), " ")
		if line == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	out := strings.ReplaceAll(b.String(), htmlPreSpace, " ")
	return strings.ReplaceAll(out, htmlPreTab, "\t")
}

// protectPreformatted marks a <pre> element's newlines as block breaks and its spaces and
// tabs with the sentinels the whitespace collapse leaves alone, so the block comes out with
// the lines and indentation it had. Entities inside it decode later like any other text.
func protectPreformatted(pre string) string {
	pre = strings.ReplaceAll(pre, "\n", htmlBreak)
	pre = strings.ReplaceAll(pre, " ", htmlPreSpace)
	return strings.ReplaceAll(pre, "\t", htmlPreTab)
}
