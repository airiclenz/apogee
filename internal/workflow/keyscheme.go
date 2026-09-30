package workflow

import (
	"encoding/json"
	"fmt"
	"io/fs"
)

// keyInput is everything an item key may be taken over: the stage and its repeat round, the text a
// verify or merge adds beside the stage's brief (the source item's key and claim, or the manifest,
// each as the scheme being keyed renders it; empty for a fanout), the item, the folder the stage's prompt file is read from, and the
// workspace its context files are read from.
type keyInput struct {
	stage     Stage
	round     int
	suffix    string
	item      Item
	prompts   fs.FS
	workspace fs.FS
}

// keyScheme is one item-key formula some apogee build shipped, numbered in the order they shipped.
// Its key func must reproduce that build's keys exactly, so a workflow folder the build wrote can
// still be read: a change to any key input or its encoding adds a new scheme at the front of
// keySchemes and never edits an existing one. TestKeySchemeGolden pins each scheme's output.
//
// verdict is the rule that build read a verify child's verdict off its receipt with: the verdict a
// merge's manifest names is part of the merge item's key, so a scheme keeps its own rule.
type keyScheme struct {
	id      int
	key     func(input keyInput) (string, error)
	verdict func(item ItemResult) Verdict
}

// keySchemes is every item-key formula ever shipped, newest first. keySchemes[0] is the current
// one: every item a run prepares is keyed through it.
var keySchemes = []keyScheme{
	{id: 2, key: keySchemeWithPromptBody, verdict: verdictOf},
	{id: 1, key: keySchemeWithoutPromptBody, verdict: verdictOfAnyStatus},
}

// keySchemeWithPromptBody is scheme 2 (from commit 5f1cba20): stageKeyBrief, which covers the
// contents of the stage's prompt file, plus the suffix, the item and the context files.
func keySchemeWithPromptBody(input keyInput) (string, error) {
	brief, err := stageKeyBrief(input.stage, input.round, input.prompts)
	if err != nil {
		return "", err
	}
	return briefItemKey(brief+input.suffix, input)
}

// keySchemeWithoutPromptBody is scheme 1 (v0.23.4 and every build before it): the stage brief
// without the prompt file's contents — only its path — plus the suffix, the item and the context
// files.
func keySchemeWithoutPromptBody(input keyInput) (string, error) {
	brief, err := stageKeyBriefWithoutPromptBody(input.stage, input.round)
	if err != nil {
		return "", err
	}
	return briefItemKey(brief+input.suffix, input)
}

// briefItemKey is ItemKey over brief, the input's item and its stage's context files, its error
// naming the stage and item.
func briefItemKey(brief string, input keyInput) (string, error) {
	key, err := ItemKey(brief, input.item, input.stage.Context, input.workspace)
	if err != nil {
		return "", fmt.Errorf("workflow: stage %q, item %q: %w", input.stage.Name, input.item.Label, err)
	}
	return key, nil
}

// stageKeyBriefWithoutPromptBody is scheme 1's stage brief, v0.23.4's stageKeyBrief: every field
// of the stage a child's work depends on and the repeat round, but not the prompt file's contents.
func stageKeyBriefWithoutPromptBody(stage Stage, round int) (string, error) {
	encoded, err := json.Marshal(struct {
		Stage   string      `json:"stage"`
		Task    string      `json:"task,omitempty"`
		Prompt  string      `json:"prompt,omitempty"`
		Out     string      `json:"out,omitempty"`
		Returns ReceiptSpec `json:"returns,omitempty"`
		Tools   []string    `json:"tools,omitempty"`
		Round   int         `json:"round,omitempty"`
	}{stage.Name, stage.Task, stage.Prompt, stage.Out, stage.Returns, stage.Tools, round})
	if err != nil {
		return "", fmt.Errorf("workflow: stage %q: encode the item key brief: %w", stage.Name, err)
	}
	return string(encoded), nil
}

// verdictOfAnyStatus is scheme 1's verdict rule, v0.23.4's verdictOf: the receipt's verdict field
// read whatever the child's status — a partial receipt's confirmed stays confirmed — unclear when it
// sent no readable verdict, none when a cancel left the item unfinished.
func verdictOfAnyStatus(item ItemResult) Verdict {
	if item.Phase != PhaseDone || item.Receipt == nil {
		return ""
	}
	value, _ := item.Receipt.Fields[VerdictField].(string)
	switch verdict := Verdict(value); verdict {
	case VerdictConfirmed, VerdictRefuted:
		return verdict
	default:
		return VerdictUnclear
	}
}
