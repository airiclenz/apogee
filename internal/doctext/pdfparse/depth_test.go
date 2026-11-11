package pdfparse

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// beyondMaxDepth is how far past MaxDepth a too-deep fixture nests: a few hundred levels, never
// millions, so a parser whose bound broke fails the assertion instead of overflowing the stack.
const beyondMaxDepth = MaxDepth + 300

// panicOf runs read and returns what it panicked with, or nil when it returned.
func panicOf(read func()) (recovered any) {
	defer func() {
		recovered = recover()
	}()
	read()
	return nil
}

// isTooDeep reports whether a recovered panic value is the parser's recursion bound.
func isTooDeep(recovered any) bool {
	err, ok := recovered.(error)
	return ok && errors.Is(err, ErrTooDeep)
}

// nestedArrays returns depth balanced arrays, one inside the next.
func nestedArrays(depth int) string {
	return strings.Repeat("[", depth) + strings.Repeat("]", depth)
}

// nestedDictionaries returns depth balanced dictionaries, one inside the next.
func nestedDictionaries(depth int) string {
	return strings.Repeat("<< /A ", depth) + "null" + strings.Repeat(" >>", depth)
}

// nestedDefinitions returns depth `N G obj … endobj` definitions, one inside the next.
func nestedDefinitions(depth int) string {
	return strings.Repeat("1 0 obj ", depth) + "null" + strings.Repeat(" endobj", depth)
}

func TestBufferReadObject_BoundsNestingAtMaxDepth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		source   string
		wantDeep bool
	}{
		{name: "arrays nested to MaxDepth", source: nestedArrays(MaxDepth)},
		{name: "arrays nested one past MaxDepth", source: nestedArrays(MaxDepth + 1), wantDeep: true},
		{name: "arrays nested far past MaxDepth", source: nestedArrays(beyondMaxDepth), wantDeep: true},
		{name: "dictionaries nested to MaxDepth", source: nestedDictionaries(MaxDepth)},
		{name: "dictionaries nested one past MaxDepth", source: nestedDictionaries(MaxDepth + 1), wantDeep: true},
		{name: "definitions nested to MaxDepth", source: nestedDefinitions(MaxDepth)},
		{name: "definitions nested one past MaxDepth", source: nestedDefinitions(MaxDepth + 1), wantDeep: true},
		{name: "a definition around arrays nested to MaxDepth", source: "1 0 obj " +
			nestedArrays(MaxDepth) + " endobj", wantDeep: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			buffer := newBuffer(strings.NewReader(testCase.source), 0)
			buffer.allowEOF = true

			recovered := panicOf(func() { buffer.readObject() })

			if isTooDeep(recovered) != testCase.wantDeep {
				t.Fatalf("readObject panicked with %v, want ErrTooDeep: %v", recovered, testCase.wantDeep)
			}
			if !testCase.wantDeep && recovered != nil {
				t.Fatalf("readObject panicked with %v, want it to read the object", recovered)
			}
			if buffer.depth != 0 {
				t.Errorf("buffer depth after the read = %d, want 0", buffer.depth)
			}
		})
	}
}

// chainedReader returns a Reader over objects, numbered from 1, whose cross-reference table also
// holds a chain of links compressed entries: object len(objects)+i lives in the object stream
// len(objects)+i+1, and the object after the last link is a plain `null`, not a stream. Resolving
// the chain's head, object len(objects)+1, loads links+1 objects one inside the next before the
// parser finds that the end is no object stream. The Reader is built by hand because a classic
// cross-reference table cannot express a compressed entry.
func chainedReader(t *testing.T, links int, objects ...string) *Reader {
	t.Helper()

	var document bytes.Buffer
	document.WriteString("%PDF-1.4\n")
	table := []xref{{ptr: objptr{0, 65535}}}
	for index, body := range objects {
		number := uint32(index + 1)
		table = append(table, xref{ptr: objptr{number, 0}, offset: int64(document.Len())})
		fmt.Fprintf(&document, "%d 0 obj\n%s\nendobj\n", number, body)
	}
	for link := range links {
		number := uint32(len(objects) + 1 + link)
		table = append(table, xref{ptr: objptr{number, 0}, inStream: true, stream: objptr{number + 1, 0}})
	}
	end := uint32(len(objects) + 1 + links)
	table = append(table, xref{ptr: objptr{end, 0}, offset: int64(document.Len())})
	fmt.Fprintf(&document, "%d 0 obj\nnull\nendobj\n", end)

	return &Reader{
		f:       bytes.NewReader(document.Bytes()),
		end:     int64(document.Len()),
		xref:    table,
		trailer: dict{"Root": objptr{1, 0}},
	}
}

func TestReaderResolve_BoundsAChainOfLoadsAtMaxDepth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		loads    int
		wantDeep bool
	}{
		{name: "a chain of MaxDepth loads", loads: MaxDepth},
		{name: "a chain one load past MaxDepth", loads: MaxDepth + 1, wantDeep: true},
		{name: "a chain far past MaxDepth", loads: beyondMaxDepth, wantDeep: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reader := chainedReader(t, testCase.loads-1)

			recovered := panicOf(func() { reader.resolve(objptr{}, objptr{1, 0}) })

			// Within the bound the chain ends on its own malformed last link: a panic, but never
			// the bound's.
			if recovered == nil || isTooDeep(recovered) != testCase.wantDeep {
				t.Fatalf("resolve panicked with %v, want ErrTooDeep: %v", recovered, testCase.wantDeep)
			}
			end := uint32(testCase.loads)
			if value := reader.resolve(objptr{}, objptr{end, 0}); value.Kind() != Null {
				t.Errorf("a load after the failed chain read %v, want the null object", value)
			}
		})
	}
}

// TestPage_FailsTheDocumentPastMaxDepthAndReadsTheNextPage pins the bound's reach past a page's
// own recover: a page whose font loads through a chain past MaxDepth panics with ErrTooDeep
// rather than returning a page error, and the count it raised is restored, so the next page of
// the same Reader still reads.
func TestPage_FailsTheDocumentPastMaxDepthAndReadsTheNextPage(t *testing.T) {
	t.Parallel()

	const pageText = "Readable page"
	content := "BT\n/F1 24 Tf\n72 720 Td\n(" + pageText + ") Tj\nET"
	reader := chainedReader(t, beyondMaxDepth,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 5 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 8 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 7 0 R >> >> /Contents 6 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	)

	recovered := panicOf(func() { _, _ = reader.Page(1).GetPlainText(nil) })
	text, err := reader.Page(2).GetPlainText(nil)

	if !isTooDeep(recovered) {
		t.Fatalf("page 1 panicked with %v, want ErrTooDeep", recovered)
	}
	if err != nil || !strings.Contains(text, pageText) {
		t.Errorf("page 2 read %q, %v, want its own text", text, err)
	}
}
