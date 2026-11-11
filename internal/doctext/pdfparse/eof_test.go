package pdfparse

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/testtime"
)

// endOfInputDeadline bounds one read of a truncated fixture. It is fixed, not scaled by the
// slow-box factor: the parser reads these few bytes in microseconds on any box, while a read loop
// that never ends allocates on every turn, so a longer wait only lets a regression eat more
// memory before it fails.
var endOfInputDeadline = testtime.Fixed(5 * time.Second)

// readWithin runs read on a goroutine of its own and returns what it read, or what it panicked
// with. When read has not returned by endOfInputDeadline it panics, never t.Fatal: nothing stops
// the read goroutine, and only ending the test binary keeps a loop that never ends from
// allocating while the rest of the package runs.
func readWithin(t *testing.T, read func() object) (result object, recovered any) {
	t.Helper()

	type outcome struct {
		result    object
		recovered any
	}
	done := make(chan outcome, 1)
	go func() {
		var finished outcome
		defer func() {
			finished.recovered = recover()
			done <- finished
		}()
		finished.result = read()
	}()

	select {
	case finished := <-done:
		return finished.result, finished.recovered
	case <-time.After(endOfInputDeadline):
		panic(fmt.Sprintf("%s: the read did not end within %v of reaching end of input", t.Name(), endOfInputDeadline))
	}
}

// programBuffer returns a buffer over source set up as Interpret reads a content stream or CMap:
// end of input is allowed, and neither object references nor streams are read.
func programBuffer(source string) *buffer {
	buffer := newBuffer(strings.NewReader(source), 0)
	buffer.allowEOF = true
	buffer.allowObjptr = false
	buffer.allowStream = false
	return buffer
}

func TestBufferReadObject_EndOfInput_ClosesOpenSyntax(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source string
		want   object
	}{
		{name: "an unclosed array", source: "[ 1 2", want: array{int64(1), int64(2)}},
		{name: "an unclosed array inside an unclosed array", source: "[ 1 [ 2",
			want: array{int64(1), array{int64(2)}}},
		{name: "an unclosed dictionary inside an unclosed array", source: "[ << /A 1",
			want: array{dict{"A": int64(1)}}},
		{name: "an unclosed array inside an unclosed dictionary", source: "<< /K [ 1",
			want: dict{"K": array{int64(1)}}},
		{name: "an unclosed literal string inside an unclosed array", source: "[ (abc",
			want: array{"abc"}},
		{name: "an unterminated hex string", source: "<41", want: "A"},
		{name: "an unterminated hex string after a space", source: "<41 ", want: "A"},
		{name: "an unterminated hex string ending in a lone digit", source: "<414", want: "A"},
		{name: "an unterminated hex string inside an unclosed array", source: "[ <41",
			want: array{"A"}},
		{name: "an empty unterminated hex string", source: "<", want: ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			buffer := programBuffer(testCase.source)

			result, recovered := readWithin(t, func() object { return buffer.readObject() })

			if recovered != nil {
				t.Fatalf("readObject panicked with %v, want it to close what end of input left open", recovered)
			}
			if !reflect.DeepEqual(result, testCase.want) {
				t.Fatalf("readObject = %#v, want %#v", result, testCase.want)
			}
			if next := buffer.readToken(); next != io.EOF {
				t.Errorf("token after the read = %#v, want io.EOF so the interpreter stops", next)
			}
		})
	}
}
