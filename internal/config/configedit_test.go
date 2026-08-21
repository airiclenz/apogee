package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ----------------------------------------------------------------------------
// The write transaction's own properties
// ----------------------------------------------------------------------------
//
// Six writers edit config.yaml and every one of them runs the same transaction (configedit.go):
// seed and read the file, splice, re-parse, verify, replace it atomically with its mode preserved.
// The properties of THAT sequence — an absent file is seeded, a refusal writes nothing at all, a
// result the parser cannot read never lands, a written file keeps the permissions it had, a splice
// with nothing to do is a confirmation rather than a rewrite — belong to no one writer, and they
// are asserted here once, against edit itself, with splices the cases write.
//
// What stays in the six writer suites is what only that writer knows: which place in the file it
// addresses, what it cuts in, what its own verify step refuses, and the wording it refuses in. A
// writer suite that also re-asserted the sequence would be pinning the transaction five more times
// through five keyholes, which is what this file replaces.

// The config the cases splice into: two settings and nothing else, so a case that must show the
// file came back untouched can compare it whole.
const editTestConfig = "server: box\nmode: auto\n"

// splicedTo stands in for a writer's splice with a result the case prepared itself, so what runs is
// the transaction around it rather than any writer's line arithmetic.
func splicedTo(updated string) editSplice {
	return func(fileConfig, []byte) ([]byte, error) { return []byte(updated), nil }
}

// spliceNothing is the splice that reports the file already says what was asked for — the arm that
// writes nothing and verifies nothing.
func spliceNothing(fileConfig, []byte) ([]byte, error) { return nil, nil }

// acceptEdit is the verify step of a case that is not about verification.
func acceptEdit(fileConfig, fileConfig, []byte) error { return nil }

// onlyFileIn lists the directory, so a case can say that the write left nothing beside the config —
// writeConfigAtomically goes through a temp file in the same directory, and a temp file surviving
// the transaction is a half-written config the next reader could find.
func onlyFileIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the config directory: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// An absent config is seeded from the embedded template before anything is spliced, so an edit
// never leaves a bare fragment where a documented file belongs — and the seeded file is the
// template plus what the splice put in it.
func TestEditSeedsAnAbsentConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	var spliced string

	err := edit(path, func(_ fileConfig, data []byte) ([]byte, error) {
		spliced = string(data)
		return append(data, []byte("mode: plan\n")...), nil
	}, acceptEdit)

	if err != nil {
		t.Fatalf("edit an absent config: %v", err)
	}
	if spliced != string(defaultConfigYAML) {
		t.Errorf("the splice was handed %d bytes, want the embedded template", len(spliced))
	}
	written := readTestConfig(t, path)
	if !strings.HasPrefix(written, string(defaultConfigYAML)) || !strings.HasSuffix(written, "mode: plan\n") {
		t.Errorf("the seeded config is not the template plus the splice:\n%s", written)
	}
	if got := onlyFileIn(t, dir); len(got) != 1 || got[0] != "config.yaml" {
		t.Errorf("the directory holds %v, want the config alone", got)
	}
}

// A config may hold endpoint details, so a rewrite must never widen its permissions: the mode the
// file had is the mode it comes back with, whatever the umask of the process making the edit.
func TestEditPreservesTheFileMode(t *testing.T) {
	t.Parallel()
	path := writeTestConfig(t, editTestConfig)
	const mode = 0o640
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if err := edit(path, splicedTo("server: other\nmode: auto\n"), acceptEdit); err != nil {
		t.Fatalf("edit: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != mode {
		t.Errorf("mode after the edit = %04o, want %04o", got, mode)
	}
}

// Every way the transaction can refuse, and the one thing they have in common: the file on disk is
// byte-for-byte what it was, with no temp file beside it. "Refused" and "written" can never be the
// same outcome, which is what lets a writer state its refusal as advice to edit the file by hand.
func TestEditRefusalsWriteNothing(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		splice  editSplice
		verify  editVerify
		wantMsg string
	}{
		{
			name:    "the splice could not do it surgically",
			splice:  func(fileConfig, []byte) ([]byte, error) { return nil, errors.New("no line to edit") },
			verify:  acceptEdit,
			wantMsg: "no line to edit",
		},
		{
			name:    "the result would not parse",
			splice:  splicedTo("server: box\n  mode: auto\n"),
			verify:  acceptEdit,
			wantMsg: "the edited file would not parse",
		},
		{
			name:   "the verify step refused it",
			splice: splicedTo("server: other\nmode: plan\n"),
			verify: func(fileConfig, fileConfig, []byte) error {
				return errors.New("the edit would have changed more than server; edit the file by hand")
			},
			wantMsg: "edit the file by hand",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(editTestConfig), 0o600); err != nil {
				t.Fatalf("write the config: %v", err)
			}

			err := edit(path, tt.splice, tt.verify)

			if err == nil {
				t.Fatalf("the edit was accepted; want a refusal")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("refusal = %v, want it to mention %q", err, tt.wantMsg)
			}
			// A file-shape refusal is about a file, so it names the one it is about.
			if !strings.Contains(err.Error(), path) {
				t.Errorf("refusal = %v, want it to name the config %q", err, path)
			}
			if got := readTestConfig(t, path); got != editTestConfig {
				t.Errorf("a refused edit changed the file:\n%s", got)
			}
			if got := onlyFileIn(t, dir); len(got) != 1 || got[0] != "config.yaml" {
				t.Errorf("a refused edit left %v beside the config", got)
			}
		})
	}
}

// A splice that reports nothing to do is a confirmation: the file already says what was asked for,
// so it is not opened for writing at all — a surface that re-commits an unchanged row cannot churn
// the user's file — and there is nothing for a verify step to be asked about.
func TestEditWritesNothingWhenTheSpliceHasNothingToDo(t *testing.T) {
	t.Parallel()
	path := writeTestConfig(t, editTestConfig)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	asked := false
	verify := func(fileConfig, fileConfig, []byte) error {
		asked = true
		return nil
	}

	if err := edit(path, spliceNothing, verify); err != nil {
		t.Fatalf("a no-op edit reported an error: %v", err)
	}

	if got := readTestConfig(t, path); got != editTestConfig {
		t.Errorf("a no-op edit rewrote the file:\n%s", got)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat again: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("a no-op edit touched the file: %v -> %v", before.ModTime(), after.ModTime())
	}
	if asked {
		t.Error("a no-op edit asked the verify step about bytes that do not exist")
	}
}

// The read is the one step of the transaction a caller decides, and editFrom is where it says so:
// the bytes the read returns are the bytes the splice is handed, and a read that refuses stops the
// transaction where it stands, in the read's own wording.
func TestEditFromTakesTheReadTheCallerBrings(t *testing.T) {
	t.Parallel()

	t.Run("the read's bytes are what the splice sees", func(t *testing.T) {
		t.Parallel()
		path := writeTestConfig(t, editTestConfig)
		const brought = "server: elsewhere\n"
		read := func(string) ([]byte, error) { return []byte(brought), nil }

		err := editFrom(path, read, func(_ fileConfig, data []byte) ([]byte, error) {
			if string(data) != brought {
				t.Errorf("the splice was handed %q, want the read's bytes", data)
			}
			return []byte("server: elsewhere\nmode: plan\n"), nil
		}, acceptEdit)

		if err != nil {
			t.Fatalf("editFrom: %v", err)
		}
		if got := readTestConfig(t, path); got != "server: elsewhere\nmode: plan\n" {
			t.Errorf("the written config is %q, want the spliced bytes", got)
		}
	})

	t.Run("a read that refuses stops the transaction", func(t *testing.T) {
		t.Parallel()
		path := writeTestConfig(t, editTestConfig)
		read := func(string) ([]byte, error) { return nil, errors.New("apogee: this file is not mine to read") }
		splice := func(fileConfig, []byte) ([]byte, error) {
			t.Error("the splice ran although the read refused")
			return nil, nil
		}

		err := editFrom(path, read, splice, acceptEdit)

		if err == nil || err.Error() != "apogee: this file is not mine to read" {
			t.Fatalf("error = %v, want the read's own refusal", err)
		}
		if got := readTestConfig(t, path); got != editTestConfig {
			t.Errorf("a refused read changed the file:\n%s", got)
		}
	})
}

// A destination that cannot be written surfaces as an error rather than as a silent success. A
// directory where the config file should be is the portable way to say it — unlike file
// permissions, it holds for root too — and an empty path is the shape a Driver that never resolved
// an apogee home hands in.
func TestEditRefusesADestinationItCannotWrite(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := edit(path, splicedTo(editTestConfig), acceptEdit); err == nil {
		t.Error("writing over a directory was accepted; want an error")
	}
	if err := edit("", splicedTo(editTestConfig), acceptEdit); err == nil {
		t.Error("an edit with no config path was accepted; want an error")
	}
}

// The transaction's middle, over bytes a caller already holds: what it hands back is what may be
// written, and nothing else — the checked bytes on a verified edit, and nil for a splice that found
// nothing to do, so a caller cannot write a splice nothing checked.
func TestVerifiedEditReturnsOnlyCheckedBytes(t *testing.T) {
	t.Parallel()

	t.Run("a verified edit comes back whole", func(t *testing.T) {
		t.Parallel()
		const updated = "server: other\nmode: auto\n"

		got, err := verifiedEdit([]byte(editTestConfig), splicedTo(updated), acceptEdit)

		if err != nil {
			t.Fatalf("the edit was refused: %v", err)
		}
		if string(got) != updated {
			t.Errorf("returned %q, want the checked bytes back", got)
		}
	})

	t.Run("a splice with nothing to do comes back as nothing to write", func(t *testing.T) {
		t.Parallel()

		got, err := verifiedEdit([]byte(editTestConfig), spliceNothing, acceptEdit)

		if err != nil {
			t.Fatalf("a no-op edit reported an error: %v", err)
		}
		if got != nil {
			t.Errorf("a no-op edit returned %d bytes to write", len(got))
		}
	})
}

// The verify step is asked its question about the config on both sides of the splice plus the bytes
// themselves: before is the file as a reader parses it, after is the edit as a reader will parse it,
// and updated is the text — the half of a check that must read the file the way any YAML reader
// does rather than through fileConfig.
func TestVerifiedEditAsksVerifyAboutBothSidesOfTheSplice(t *testing.T) {
	t.Parallel()
	const updated = "server: other\nmode: auto\n"
	var gotBefore, gotAfter fileConfig
	var gotBytes []byte

	if _, err := verifiedEdit([]byte(editTestConfig), splicedTo(updated),
		func(before, after fileConfig, data []byte) error {
			gotBefore, gotAfter, gotBytes = before, after, data
			return nil
		}); err != nil {
		t.Fatalf("the edit was refused: %v", err)
	}

	if gotBefore.Server != "box" {
		t.Errorf("verify saw before.Server = %q, want the config the edit started from", gotBefore.Server)
	}
	if gotAfter.Server != "other" {
		t.Errorf("verify saw after.Server = %q, want the config the splice produced", gotAfter.Server)
	}
	if string(gotBytes) != updated {
		t.Errorf("verify saw %q, want the edited bytes", gotBytes)
	}
}

// The one deliberate ordering in the transaction: the starting config is parsed before the splice,
// because a splice needs to know what the file already says, but the decoder's error is held back
// until the splice has had its say. A splice that can name WHICH part of the file it could not read
// gives the better refusal; when it does not object, the decoder's error is the answer — including
// for a splice that found nothing to do, because a confirmation that the file already says it is a
// claim about a file apogee cannot read.
func TestVerifiedEditHoldsTheDecoderErrorUntilTheSpliceHasSpoken(t *testing.T) {
	t.Parallel()
	// A top level that is a list: nothing a settings reader can make sense of, and the shape a
	// splice's own check names.
	const notSettings = "- server\n- mode\n"
	// A file that IS settings-shaped but holds a value the schema's type cannot take.
	const typeError = "context-window: lots\nmode: auto\n"

	for _, tt := range []struct {
		name    string
		data    string
		splice  editSplice
		wantMsg string
	}{
		{
			name:    "a splice that objects is the refusal",
			data:    notSettings,
			splice:  func(fileConfig, []byte) ([]byte, error) { return nil, errors.New("not a mapping of settings") },
			wantMsg: "not a mapping of settings",
		},
		{
			name:    "a splice that did its work still meets the decoder's error",
			data:    typeError,
			splice:  splicedTo("context-window: lots\nmode: plan\n"),
			wantMsg: "cannot unmarshal",
		},
		{
			name:    "a splice with nothing to do is refused rather than confirmed",
			data:    typeError,
			splice:  spliceNothing,
			wantMsg: "cannot unmarshal",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := verifiedEdit([]byte(tt.data), tt.splice, acceptEdit)

			if err == nil {
				t.Fatalf("the edit was accepted; want a refusal")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("refusal = %v, want it to mention %q", err, tt.wantMsg)
			}
			if got != nil {
				t.Errorf("a refused edit returned %d bytes to write", len(got))
			}
		})
	}
}

// The same held-back error at the transaction's outer edge, where a user reads it: re-setting a
// value a config already carries is not a confirmation when the rest of that config does not parse
// into settings — the file is refused, named, and left exactly as it was.
func TestEditRefusesAConfigTheParserCannotRead(t *testing.T) {
	t.Parallel()
	const content = "context-window: lots\nmode: auto\n"
	path := writeTestConfig(t, content)

	err := edit(path, spliceNothing, acceptEdit)

	if err == nil {
		t.Fatal("a config that does not parse was confirmed; want a refusal")
	}
	if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "cannot unmarshal") {
		t.Errorf("refusal = %v, want it to name the config and the decoder's error", err)
	}
	if got := readTestConfig(t, path); got != content {
		t.Errorf("a refused edit changed the file:\n%s", got)
	}
}

// sameApartFrom is the half of the verification every writer shares, so what it does and does not
// forgive is worth pinning on its own: a block created for one key compares equal to the absent
// block it replaced, while a change to any other setting — including one in the same block — does
// not.
func TestSameApartFrom(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		before string
		after  string
		path   string
		want   bool
	}{
		{
			name:   "the target key alone",
			before: "mode: auto\nserver: box\n",
			after:  "mode: plan\nserver: box\n",
			path:   "mode", want: true,
		},
		{
			name:   "a block created for the target key",
			before: "mode: auto\n",
			after:  "mode: auto\nui:\n  spinner: glitter\n",
			path:   "ui.spinner", want: true,
		},
		{
			name:   "a block emptied by resetting the target key",
			before: "mode: auto\nui:\n  spinner: glitter\n",
			after:  "mode: auto\n",
			path:   "ui.spinner", want: true,
		},
		{
			name:   "a neighbour in the same block",
			before: "ui:\n  spinner: snake\n  show-scrollbar: true\n",
			after:  "ui:\n  spinner: glitter\n",
			path:   "ui.spinner", want: false,
		},
		{
			name:   "a neighbour elsewhere in the file",
			before: "mode: auto\nserver: box\n",
			after:  "mode: plan\nserver: other\n",
			path:   "mode", want: false,
		},
		{
			name:   "a path the schema does not have cannot be verified",
			before: "mode: auto\n",
			after:  "mode: auto\n",
			path:   "ui.sparkle", want: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var b, a fileConfig
			if err := yaml.Unmarshal([]byte(tt.before), &b); err != nil {
				t.Fatalf("parse before: %v", err)
			}
			if err := yaml.Unmarshal([]byte(tt.after), &a); err != nil {
				t.Fatalf("parse after: %v", err)
			}
			if got := sameApartFrom(b, a, tt.path); got != tt.want {
				t.Errorf("sameApartFrom(..., %q) = %v, want %v", tt.path, got, tt.want)
			}
			// The comparison must not consume what it compares: a second call sees the same answer,
			// which is what proves it copied the blocks it reached into rather than blanking them.
			if got := sameApartFrom(b, a, tt.path); got != tt.want {
				t.Errorf("a second sameApartFrom(..., %q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// The legacy migration changes two keys as one edit, so it verifies against two paths: both are
// forgiven together, and a third difference is still caught — a forgiven path must not become a
// licence to rewrite the file.
func TestSameApartFromTwoPaths(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		before string
		after  string
		want   bool
	}{
		{
			name:   "the fold's own two keys",
			before: "mode: auto\n",
			after:  "mode: auto\nservers:\n  - name: box\n    endpoint: http://box:1111\nserver: box\n",
			want:   true,
		},
		{
			name:   "one path forgiven does not forgive the other keys",
			before: "mode: auto\n",
			after:  "mode: plan\nservers:\n  - name: box\n    endpoint: http://box:1111\nserver: box\n",
			want:   false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var b, a fileConfig
			if err := yaml.Unmarshal([]byte(tt.before), &b); err != nil {
				t.Fatalf("parse before: %v", err)
			}
			if err := yaml.Unmarshal([]byte(tt.after), &a); err != nil {
				t.Fatalf("parse after: %v", err)
			}
			if got := sameApartFrom(b, a, "servers", "server"); got != tt.want {
				t.Errorf(`sameApartFrom(..., "servers", "server") = %v, want %v`, got, tt.want)
			}
		})
	}
}
