package prompt

import (
	"strings"
	"testing"
	"time"
)

// fixedNow is the render clock every test below uses: late in the day, so a date-only
// render is visibly not a timestamp.
var fixedNow = time.Date(2026, 7, 26, 23, 59, 58, 0, time.UTC)

// The whole substitution contract in one template: all four placeholders, one of them
// repeated, every occurrence replaced and nothing else touched.
func TestRenderSubstitutesAllPlaceholders(t *testing.T) {
	t.Parallel()

	template := "You are in {{workspace}} on {{datetime}} in {{mode}} mode.\nScratch: {{scratch}}.\nStill {{workspace}}."
	in := Inputs{Workspace: "/home/eric/apogee", Mode: "ask-before", Now: fixedNow, Scratch: "/home/eric/.apogee/scratch/s1"}
	want := "You are in /home/eric/apogee on 2026-07-26 in ask-before mode.\nScratch: /home/eric/.apogee/scratch/s1.\nStill /home/eric/apogee."

	if got := Render(template, in); got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
}

// {{datetime}} is a DATE, never a timestamp: a per-request clock time would change the
// first system message every turn and bust the server's prefix KV cache.
func TestRenderDateIsDateOnly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{name: "one second before midnight", now: fixedNow, want: "2026-07-26"},
		{name: "midnight rolls to the next date", now: fixedNow.Add(2 * time.Second), want: "2026-07-27"},
		{name: "single-digit month and day are zero-padded", now: time.Date(2027, 1, 5, 8, 30, 0, 0, time.UTC), want: "2027-01-05"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Render(PlaceholderDatetime, Inputs{Now: tt.now})
			if got != tt.want {
				t.Errorf("Render(%s) = %q, want %q", PlaceholderDatetime, got, tt.want)
			}
			if strings.Contains(got, ":") {
				t.Errorf("Render(%s) = %q, want no time-of-day", PlaceholderDatetime, got)
			}
		})
	}
}

// Everything the language accepts: the known four (alone and together), text with no
// placeholders at all, the empty template (= no prompt configured), and brace-ish text
// that is not a {{…}} token.
func TestValidateAcceptsKnownAndEmpty(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		template string
	}{
		{name: "empty template", template: ""},
		{name: "no placeholders", template: "You are apogee, a coding agent."},
		{name: "workspace alone", template: "cwd: " + PlaceholderWorkspace},
		{name: "datetime alone", template: "today: " + PlaceholderDatetime},
		{name: "mode alone", template: "mode: " + PlaceholderMode},
		{name: "scratch alone", template: "scratch: " + PlaceholderScratch},
		{name: "all four, one repeated", template: PlaceholderWorkspace + " " + PlaceholderDatetime + " " + PlaceholderMode + " " + PlaceholderScratch + " " + PlaceholderMode},
		{name: "literal single braces are not placeholders", template: "use { and } freely"},
		{name: "unclosed braces pass through", template: "a {{ dangling opener"},
		{name: "a closing pair without an opener passes through", template: "a dangling closer }}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := Validate(tt.template); err != nil {
				t.Errorf("Validate(%q) = %v, want nil", tt.template, err)
			}
		})
	}
}

// Spelling is strict — a near-miss is a startup error, never a literal shipped to the
// model — and the error names the offender AND the whole known set, so the fix is in the
// message.
func TestValidateRejectsUnknownListingKnown(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		template string
		offender string
	}{
		{name: "unknown name", template: "hi {{foo}}", offender: "{{foo}}"},
		{name: "spaces are not tolerated", template: "cwd: {{ workspace }}", offender: "{{ workspace }}"},
		{name: "case is not folded", template: "cwd: {{WORKSPACE}}", offender: "{{WORKSPACE}}"},
		{name: "an empty token is still a token", template: "hi {{}}", offender: "{{}}"},
		{name: "the first offender is named", template: PlaceholderMode + " {{first}} {{second}}", offender: "{{first}}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := Validate(tt.template)
			if err == nil {
				t.Fatalf("Validate(%q) = nil, want an unknown-placeholder error", tt.template)
			}
			msg := err.Error()
			if !strings.Contains(msg, tt.offender) {
				t.Errorf("Validate(%q) error %q does not name the offender %q", tt.template, msg, tt.offender)
			}
			for _, known := range []string{PlaceholderWorkspace, PlaceholderDatetime, PlaceholderMode, PlaceholderScratch} {
				if !strings.Contains(msg, known) {
					t.Errorf("Validate(%q) error %q does not list the known placeholder %s", tt.template, msg, known)
				}
			}
		})
	}
}

// KnownList is the one source both cmd/apogee and the engine quote, so it must name every
// known placeholder in the documented comma-joined form.
func TestKnownListNamesEveryPlaceholder(t *testing.T) {
	t.Parallel()

	want := "{{workspace}}, {{datetime}}, {{mode}}, {{scratch}}"
	if got := KnownList(); got != want {
		t.Errorf("KnownList() = %q, want %q", got, want)
	}
}
