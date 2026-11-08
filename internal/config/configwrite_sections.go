package config

import (
	"fmt"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The side-panel block writer
// ----------------------------------------------------------------------------
//
// The `ui.side-panel:` block (ADR 0110) sits one level deeper than the scalar writer reaches
// (scalarPathDepth): both its keys are three segments long, `ui.side-panel.show` and
// `ui.side-panel.sections`. So this writer owns the whole block. It finds `ui:` and `side-panel:`
// in the parsed document, creates whichever of the two is absent, and splices the one line its key
// sits on, under the contract every config writer keeps (configedit.go): comments, key order and the
// other keys stay exactly as found, the result is re-parsed and compared before anything is written,
// and a shape it cannot read surgically is refused ("edit the file by hand"), never folded.
//
// `show` is a scalar, written like any enum. `sections` is a list of `section: mode` entries, and the
// file carries it as one flow line, the template's own spelling. A pin, unpin or hide changes ONE
// entry's mode (SaveSidePanelSection): the entries the file writes keep their order and their modes,
// and a section the file does not name brings in the whole list as the session resolves it, so the
// display order on screen is the order the file now spells. A list written one entry per line is
// refused rather than rewritten: turning the user's lines into one is a rewrite they did not ask for
// (valueFitsOneLine's rule for a name list).

// sidePanelBlockKey is the `side-panel:` block's key inside `ui:`, and uiBlockKey that block's own.
const (
	sidePanelBlockKey = "side-panel"
	uiBlockKey        = "ui"
	// sidePanelBlockPath is the block's dotted path, the registry paths of both keys less their leaf.
	sidePanelBlockPath = uiBlockKey + "." + sidePanelBlockKey
	// sidePanelShowLeaf and sidePanelSectionsLeaf are the two keys of the block as the file spells them.
	sidePanelShowLeaf     = "show"
	sidePanelSectionsLeaf = "sections"
)

// IsSidePanelKey reports whether key is one of the `ui.side-panel:` block's two keys, which
// [SaveSidePanelSetting] writes and [ResetSidePanelSetting] resets rather than [SaveConfigSetting] and
// [ResetConfigSetting]: the scalar writer refuses a path three segments deep.
func IsSidePanelKey(key string) bool {
	return key == domain.UIKeySidePanelShow || key == domain.UIKeySidePanelSections
}

// SaveSidePanelSetting writes one key of the `ui.side-panel:` block, value spelled as the file and the
// registry row spell it: `auto`, `on` or `off` for `ui.side-panel.show`, and the whole bracketed list
// for `ui.side-panel.sections`, which lands in its canonical spelling with every section named. A
// value the key cannot hold is refused before the file is opened. The write is [SaveConfigSetting]'s
// in every other respect: an absent config is seeded from the template, a re-set of what the file
// already says writes nothing, and the replace is atomic and mode-preserving.
func SaveSidePanelSetting(path, key, value string) error {
	switch key {
	case domain.UIKeySidePanelShow:
		return saveSidePanelShow(path, value)
	case domain.UIKeySidePanelSections:
		entries, err := domain.ParseSidePanelSections(value)
		if err != nil {
			return err
		}
		_, err = saveSidePanelSections(path, func([]domain.SidePanelEntry) ([]domain.SidePanelEntry, error) {
			return entries, nil
		})
		return err
	}
	return fmt.Errorf("apogee: %q is not a key of the %s block", key, sidePanelBlockPath)
}

// ResetSidePanelSetting removes one key of the `ui.side-panel:` block from the file, so the key
// resolves from its default again — [ResetConfigSetting] for the block the scalar writer cannot reach.
// The key's active line goes, and with it each block the removal leaves empty (`side-panel:`, then
// `ui:`), as the scalar reset drops a block it empties; comments, order and every other key stay as
// found. A key the file does not set is already at its default: a no-op, and nothing is written.
//
// Only `ui.side-panel.show` is reset here. `ui.side-panel.sections` is a structured list edited in
// the file, which no settings surface resets — refused as [ResetConfigSetting] refuses such a key.
func ResetSidePanelSetting(path, key string) error {
	switch key {
	case domain.UIKeySidePanelShow:
		k, ok := LookupKey(key)
		if !ok {
			return fmt.Errorf("apogee: %q is not a setting apogee knows", key)
		}
		splice := func(_ fileConfig, data []byte) ([]byte, error) {
			return spliceSidePanelDelete(data, sidePanelShowLeaf, k.Kind)
		}
		verify := func(before, after fileConfig, updated []byte) error {
			return verifyScalarEdit(before, after, updated, k, "", false)
		}
		return edit(path, splice, verify)
	case domain.UIKeySidePanelSections:
		return fmt.Errorf("apogee: %s is not a simple value; edit it in config.yaml", key)
	}
	return fmt.Errorf("apogee: %q is not a key of the %s block", key, sidePanelBlockPath)
}

// SaveSidePanelSection sets one section's mode in `ui.side-panel.sections` — what a pin, an unpin or
// a hide of that section persists — and answers the list the file now resolves to, in its canonical
// spelling, for the caller to land in the session as the next start would read it.
//
// The file's own entries keep their order and their modes; the named entry's mode is changed in
// place. A section the file's list does not name — or a file with no list at all — is written as the
// whole list the session resolves (domain.ParseSidePanelSections: the named entries first, the rest
// after them in auto), with the one mode changed, so the sections stay in the order they show in. A
// section or mode this build does not know is refused before the file is opened, and so is a list in
// the file that no longer parses.
func SaveSidePanelSection(path string, section domain.SidePanelSection,
	mode domain.SidePanelMode) (domain.SidePanelSections, error) {
	asked := []domain.SidePanelEntry{{Section: section, Mode: mode}}
	if _, err := domain.ParseSidePanelSections(string(domain.SpellSidePanelSections(asked))); err != nil {
		return "", err
	}
	return saveSidePanelSections(path, func(written []domain.SidePanelEntry) ([]domain.SidePanelEntry, error) {
		return withSectionMode(written, section, mode)
	})
}

// withSectionMode is the file's written entries with section's mode set to mode: changed in place
// where the list names the section, and otherwise the whole resolved list with that one mode
// changed. written is never modified.
func withSectionMode(written []domain.SidePanelEntry, section domain.SidePanelSection,
	mode domain.SidePanelMode) ([]domain.SidePanelEntry, error) {
	named := func(e domain.SidePanelEntry) bool { return e.Section == section }
	next := slices.Clone(written)
	if !slices.ContainsFunc(next, named) {
		resolved, err := domain.ParseSidePanelSections(string(domain.SpellSidePanelSections(written)))
		if err != nil {
			return nil, err
		}
		next = resolved
	}
	next[slices.IndexFunc(next, named)].Mode = mode
	return next, nil
}

// saveSidePanelShow writes `ui.side-panel.show`, judged by the key's own registry row first — the
// enum's vocabulary and the startup check (validateSettingValue) — so a refused value touches nothing.
func saveSidePanelShow(path, value string) error {
	k, ok := LookupKey(domain.UIKeySidePanelShow)
	if !ok {
		return fmt.Errorf("apogee: %q is not a setting apogee knows", domain.UIKeySidePanelShow)
	}
	if err := validateSettingValue(k, value); err != nil {
		return err
	}
	text, want, err := renderSettingValue(k, value)
	if err != nil {
		return fmt.Errorf("apogee: %w", err)
	}
	splice := func(_ fileConfig, data []byte) ([]byte, error) {
		return spliceSidePanelLine(data, sidePanelShowLeaf, KindEnum, text)
	}
	verify := func(before, after fileConfig, updated []byte) error {
		return verifyScalarEdit(before, after, updated, k, want, true)
	}
	return edit(path, splice, verify)
}

// saveSidePanelSections writes the list next makes of the file's written entries, inside the one
// transaction, so the entries it starts from are the ones the file holds under the lock rather than
// a reading taken before it. It answers the list the file then resolves to — also when the file
// already said it, and nothing was written.
func saveSidePanelSections(path string,
	next func(written []domain.SidePanelEntry) ([]domain.SidePanelEntry, error)) (domain.SidePanelSections, error) {
	var resolved domain.SidePanelSections
	var want domain.SidePanelSections
	splice := func(before fileConfig, data []byte) ([]byte, error) {
		entries, err := next(writtenSidePanelSections(before))
		if err != nil {
			return nil, err
		}
		parsed, err := domain.ParseSidePanelSections(string(domain.SpellSidePanelSections(entries)))
		if err != nil {
			return nil, err
		}
		want, resolved = domain.SpellSidePanelSections(entries), domain.SpellSidePanelSections(parsed)
		return spliceSidePanelLine(data, sidePanelSectionsLeaf, KindStringList, string(want))
	}
	verify := func(before, after fileConfig, _ []byte) error {
		return verifySidePanelSections(before, after, want)
	}
	if err := edit(path, splice, verify); err != nil {
		return "", err
	}
	return resolved, nil
}

// writtenSidePanelSections is the `ui.side-panel.sections` entries a parsed config writes, as
// written, or nil when it names no list.
func writtenSidePanelSections(fc fileConfig) []domain.SidePanelEntry {
	if fc.UI == nil || fc.UI.SidePanel == nil {
		return nil
	}
	return fc.UI.SidePanel.Sections
}

// verifySidePanelSections is the sections write's gate: the edited config must read back the list
// exactly as spelled, and agree with the config it started from on every other setting.
func verifySidePanelSections(before, after fileConfig, want domain.SidePanelSections) error {
	written := writtenSidePanelSections(after)
	switch {
	case written == nil || domain.SpellSidePanelSections(written) != want:
		return fmt.Errorf("the edit did not put %s where a reader would look for it; edit the file by hand",
			domain.UIKeySidePanelSections)
	case !sameApartFrom(before, after, domain.UIKeySidePanelSections):
		return fmt.Errorf("the edit would have changed more than %s; edit the file by hand",
			domain.UIKeySidePanelSections)
	}
	return nil
}

// sidePanelSpot is where the block and one of its keys stand in the parsed document: block is
// `side-panel` as a key of `ui:` (its parent fields are `ui:`'s), and leaf is the key as a key of
// `side-panel:`, unset while the block is absent or empty.
type sidePanelSpot struct {
	block ScalarTarget
	leaf  ScalarTarget
}

// sidePanelSpotIn locates leaf in the document. Refused, as ScalarTargetIn refuses them one level up:
// a `ui:` or `side-panel:` written in flow style, and either one holding something other than a block.
func sidePanelSpotIn(doc *yaml.Node, leaf string, kind Kind) (sidePanelSpot, error) {
	block, err := ScalarTargetIn(doc, Key{Path: sidePanelBlockPath})
	if err != nil {
		return sidePanelSpot{}, err
	}
	spot := sidePanelSpot{block: block, leaf: ScalarTarget{Key: leaf, Kind: kind, Parent: sidePanelBlockKey}}
	if !block.IsSet() {
		return spot, nil
	}
	body := block.ValueNode
	switch {
	case body.Style&yaml.FlowStyle != 0:
		return spot, fmt.Errorf("its %s: block is written in flow style ({...}); edit the file by hand",
			sidePanelBlockKey)
	case isNullNode(body):
		spot.leaf.ParentKey, spot.leaf.ParentBody, spot.leaf.ParentNull = block.KeyNode, body, true
		return spot, nil
	case body.Kind != yaml.MappingNode:
		return spot, fmt.Errorf("its %s: is not a block of settings; edit the file by hand", sidePanelBlockKey)
	}
	spot.leaf.ParentKey, spot.leaf.ParentBody = block.KeyNode, body
	spot.leaf.KeyNode, spot.leaf.ValueNode = mappingEntry(body, leaf)
	return spot, nil
}

// spliceSidePanelLine sets the block's leaf key to text: its active line rewritten in place, keeping
// the user's spacing and end-of-line note (scalarLineParts), or a new line inserted where the key has
// none (sidePanelInsertion). It returns nil bytes when the file already spells the line this way.
func spliceSidePanelLine(data []byte, leaf string, kind Kind, text string) ([]byte, error) {
	doc, err := Document(data)
	if err != nil {
		return nil, err
	}
	lines := SplitConfigLines(data)
	spot, err := sidePanelSpotIn(doc, leaf, kind)
	if err != nil {
		return nil, err
	}
	if !spot.leaf.IsSet() {
		block, at, err := sidePanelInsertion(lines, spot, leaf+": "+text)
		switch {
		case err != nil:
			return nil, err
		case at == 0:
			return joinConfigLines(appendBlock(lines, block)), nil
		}
		return insertAt(lines, block, at, fmt.Sprintf("the place for %s:", leaf))
	}
	head, gap, tail, err := scalarLineParts(lines, spot.leaf)
	if err != nil {
		return nil, err
	}
	line := head + gap + text + tail
	if lines[spot.leaf.KeyNode.Line-1] == line {
		return nil, nil
	}
	out := slices.Clone(lines)
	out[spot.leaf.KeyNode.Line-1] = line
	return joinConfigLines(out), nil
}

// spliceSidePanelDelete removes the block's leaf key: its active line, then `side-panel:` when the
// key was the block's only one, then `ui:` when that block was the only one there. It returns nil
// bytes when the file does not set the key.
func spliceSidePanelDelete(data []byte, leaf string, kind Kind) ([]byte, error) {
	doc, err := Document(data)
	if err != nil {
		return nil, err
	}
	lines := SplitConfigLines(data)
	spot, err := sidePanelSpotIn(doc, leaf, kind)
	if err != nil {
		return nil, err
	}
	if !spot.leaf.IsSet() {
		return nil, nil
	}
	if _, _, _, err := scalarLineParts(lines, spot.leaf); err != nil {
		return nil, err
	}
	drop := []int{spot.leaf.KeyNode.Line}
	if len(spot.leaf.ParentBody.Content) == 2 {
		drop = append(drop, spot.block.KeyNode.Line)
		if spot.block.ParentBody != nil && len(spot.block.ParentBody.Content) == 2 {
			drop = append(drop, spot.block.ParentKey.Line)
		}
	}
	return deleteLines(lines, drop...)
}

// sidePanelInsertion is the lines to insert for a block key the file does not set, and the 1-based
// line to insert them after — 0 for "append at the end" — or the refusal of a file that documents
// `ui:` in two places (CommentedExampleLine). Each block the key needs and the file lacks
// is created around it, indented the way the block it joins already is:
//
//   - `side-panel:` holds keys: the setting joins them, after its last line;
//   - `side-panel:` is there with nothing under it: the setting is its first child;
//   - `ui:` holds keys but no `side-panel:`: the block and the setting join `ui:`, after its last line;
//   - `ui:` is there with nothing under it: the block and the setting are its first child;
//   - no `ui:` at all: the whole nest, below the commented `# ui:` example block where the file
//     documents one (scalarInsertion's placement), and at the end otherwise.
func sidePanelInsertion(lines []string, spot sidePanelSpot, setting string) ([]string, int, error) {
	block, leaf := spot.block, spot.leaf
	nested := []string{sidePanelBlockKey + ":", indentLine(listIndent, setting)}
	switch {
	case leaf.ParentNull:
		return []string{indentLine(leaf.ParentKey.Column-1+listIndent, setting)}, leaf.ParentKey.Line, nil
	case leaf.ParentBody != nil:
		return []string{indentLine(leaf.ParentBody.Column-1, setting)}, maxNodeLine(leaf.ParentBody), nil
	case block.ParentNull:
		return indentLines(block.ParentKey.Column-1+listIndent, nested), block.ParentKey.Line, nil
	case block.ParentBody != nil:
		return indentLines(block.ParentBody.Column-1, nested), maxNodeLine(block.ParentBody), nil
	}
	at, err := commentedExampleBlockEnd(lines, uiBlockKey)
	return append([]string{uiBlockKey + ":"}, indentLines(listIndent, nested)...), at, err
}
