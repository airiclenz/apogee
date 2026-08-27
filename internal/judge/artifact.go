package judge

import (
	"image/color"
	"strings"

	"github.com/airiclenz/apogee/internal/tuitest"
)

// Kind says what a piece of evidence IS, so the judge reads it as the thing it is rather than as
// undifferentiated text: a frame has columns that mean something, a transcript has turns, a trace
// is machine output nobody wrote for a reader.
type Kind string

const (
	// KindFrame is a terminal snapshot — what the screen showed, row by row.
	KindFrame Kind = "terminal frame"
	// KindStdout is what a command printed.
	KindStdout Kind = "command output"
	// KindFile is the contents of a file the run produced.
	KindFile Kind = "file contents"
	// KindTranscript is a conversation: the user's prompts and the agent's replies.
	KindTranscript Kind = "conversation transcript"
	// KindTrace is machine-emitted diagnostics — the --tui-trace stream, a log.
	KindTrace Kind = "diagnostic trace"
)

// Artifact is one named piece of evidence a verdict is rendered on. The Name is what a reason
// points at, so it is worth spending a word on: "the outcome slot" beats "frame2".
type Artifact struct {
	Name string
	Kind Kind
	Text string
}

// kind is the label the prompt prints, defaulting for an Artifact built without one.
func (a Artifact) kind() Kind {
	if a.Kind == "" {
		return "text"
	}
	return a.Kind
}

// Tone names one of the colour scheme's tones for the judge. A styled frame carries SGR sequences
// no model reads reliably, so [FrameArtifact] replaces them with the tone NAMES the caller passes
// — the scheme's error colour becomes ⟨red⟩…⟨/red⟩ — and a rubric can then say "the error line is
// red" and mean it. The caller passes the tones because only the caller knows which scheme is
// loaded; this package never reaches into one.
type Tone struct {
	Name  string
	Color color.Color
}

// The attribute names a styled frame uses, alongside the caller's tone names.
const (
	boldTag    = "bold"
	faintTag   = "faint"
	reverseTag = "reverse"
)

// FrameArtifact serialises a tuitest frame as evidence.
//
// Without styles it is the frame's plain text — the right artifact for a claim about wording,
// layout or content, and the cheaper one. With styles, each row is walked as [tuitest.Frame.StyleRuns]
// and every run whose foreground matches one of the caller's tones, or that is bold, faint or
// reverse, is wrapped in named tags: ⟨red⟩the error⟨/red⟩, ⟨bold⟩the header⟨/bold⟩. Tags nest
// colour outermost.
//
// A colour with no matching Tone is NOT named. That is deliberate: inventing a name for an
// unrecognised RGB triple would let a rubric assert a colour nobody declared, and the judge would
// be agreeing with the serialiser rather than with the screen.
func FrameArtifact(name string, f tuitest.Frame, withStyles bool, tones ...Tone) Artifact {
	if !withStyles {
		return Artifact{Name: name, Kind: KindFrame, Text: f.String()}
	}
	var b strings.Builder
	for y := 0; y < f.Height(); y++ {
		if y > 0 {
			b.WriteByte('\n')
		}
		for _, run := range f.StyleRuns(y) {
			writeRun(&b, run, tones)
		}
	}
	return Artifact{Name: name, Kind: KindFrame, Text: b.String()}
}

// writeRun writes one style run with its tags around it.
func writeRun(b *strings.Builder, run tuitest.Run, tones []Tone) {
	tags := make([]string, 0, 4)
	if name, ok := toneName(run.FG, tones); ok {
		tags = append(tags, name)
	}
	for _, attr := range []struct {
		on  bool
		tag string
	}{{run.Bold, boldTag}, {run.Faint, faintTag}, {run.Reverse, reverseTag}} {
		if attr.on {
			tags = append(tags, attr.tag)
		}
	}
	for _, tag := range tags {
		b.WriteString("⟨" + tag + "⟩")
	}
	b.WriteString(run.Text)
	for i := len(tags) - 1; i >= 0; i-- {
		b.WriteString("⟨/" + tags[i] + "⟩")
	}
}

// toneName resolves a cell colour to the caller's name for it, comparing resolved RGBA so an
// indexed colour and the literal it resolves to are one tone.
func toneName(c color.Color, tones []Tone) (string, bool) {
	if c == nil {
		return "", false
	}
	for _, tone := range tones {
		if tone.Name != "" && tuitest.SameColor(c, tone.Color) {
			return tone.Name, true
		}
	}
	return "", false
}
