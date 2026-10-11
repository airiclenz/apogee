package platform

import "testing"

// TestDecodeConsoleOutput pins the per-line codepage decode: UTF-8 lines pass untouched, invalid
// lines decode through the codepage's table, a byte-capped trailing rune is not a decode trigger,
// and a codepage without a table passes everything through.
func TestDecodeConsoleOutput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		input    string
		codepage uint32
		want     string
	}{
		{
			name:     "cp850 non-breaking space digit grouping",
			input:    "83\xff293",
			codepage: 850,
			want:     "83 293",
		},
		{
			name:     "cp437 box drawing and umlauts",
			input:    "\xc9\xcd\xbb\r\n\xba Gr\x94\xe1e \x81ber \x8erger \xba\r\n\xc8\xcd\xbc\r\n",
			codepage: 437,
			want:     "╔═╗\r\n║ Größe über Ärger ║\r\n╚═╝\r\n",
		},
		{
			name:     "valid utf-8 is untouched",
			input:    "für Größe ╔═╗\n",
			codepage: 850,
			want:     "für Größe ╔═╗\n",
		},
		{
			name:     "truncated trailing rune on the last line is untouched",
			input:    "line one\nf\xc3\xbcr \xc3",
			codepage: 850,
			want:     "line one\nf\xc3\xbcr \xc3",
		},
		{
			name:     "utf-8 line beside a cp850 line decodes only the cp850 line",
			input:    "f\xc3\xbcr\nGr\x94\xe1e\n",
			codepage: 850,
			want:     "für\nGröße\n",
		},
		{
			name:     "65001 passes through",
			input:    "83\xff293",
			codepage: 65001,
			want:     "83\xff293",
		},
		{
			name:     "cp857 has no table and passes through",
			input:    "83\xff293",
			codepage: 857,
			want:     "83\xff293",
		},
		{
			name:     "unknown codepage passes through",
			input:    "Gr\x94\xe1e",
			codepage: 12345,
			want:     "Gr\x94\xe1e",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := DecodeConsoleOutput([]byte(tc.input), tc.codepage)

			if got != tc.want {
				t.Fatalf("DecodeConsoleOutput(%q, %d) = %q, want %q", tc.input, tc.codepage, got, tc.want)
			}
		})
	}
}
