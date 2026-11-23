package main

// The side panel's changed-files section follows the session's writes, driven through the terminal:
// a scripted model lands one write_file per prompt, and each path comes back as a row under the
// section's header on the frame. The section's paint is memoised across frames (internal/tui
// panelframe.go, panelMemo); the Model-level tests pin the memo against a cold paint, and this is
// the journey that proves a real landed write still reaches the painted column.

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

const (
	// sidePanelFilesHeader is the changed-files section's title as its header paints it.
	sidePanelFilesHeader = "Changed files"

	// The two prompts, the paths their write_file calls land, and the replies after each result.
	sidePanelFirstPrompt  = "Write the first file."
	sidePanelSecondPrompt = "Write the second file."
	sidePanelFirstPath    = "first-panel.txt"
	sidePanelSecondPath   = "second-panel.txt"
	sidePanelFirstReply   = "The first file is written."
	sidePanelSecondReply  = "The second file is written."
)

// TestE2ESidePanelChangedFilesFollowWrites lands two write_file calls, one per prompt, on a
// 160-column window that shows the panel: after the first, its path is a row under "Changed files";
// after the second, the header counts two and both paths are rows under it.
func TestE2ESidePanelChangedFilesFollowWrites(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, sidePanelFilesScript())
	drv := tuitest.NewDriver(t, sidePanelDragSize)
	sess := launchTUIConfigured(t, drv, stub, sidePanelAutoConfig, "--mode", "allow-edits")
	waitIdle(drv)

	submit(drv, sidePanelFirstPrompt)
	awaitReply(drv, sidePanelFirstReply, 1)
	drv.WaitFor(func() bool { return panelFilesList(drv.Frame(), 1, sidePanelFirstPath) },
		tuitest.Awaiting(fmt.Sprintf("%q under %q in the side panel", sidePanelFirstPath, sidePanelFilesHeader)))
	waitIdle(drv)

	submit(drv, sidePanelSecondPrompt)
	awaitReply(drv, sidePanelSecondReply, 1)
	drv.WaitFor(func() bool {
		return panelFilesList(drv.Frame(), 2, sidePanelFirstPath, sidePanelSecondPath)
	}, tuitest.Awaiting(fmt.Sprintf("%q and %q under %q (2) in the side panel",
		sidePanelFirstPath, sidePanelSecondPath, sidePanelFilesHeader)))

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// sidePanelFilesScript is the upstream the journey talks to: each prompt answered by one write_file
// call, each result by its own reply, and anything else — a title request — by a plain answer.
func sidePanelFilesScript() stubllm.Script {
	write := func(path string) []stubllm.ToolCall {
		return []stubllm.ToolCall{{Name: "write_file", Arguments: `{"path":"` + path + `","content":"panel\n"}`}}
	}
	return stubllm.Script{Model: "panel-files-model", Turns: []stubllm.Turn{
		{When: &stubllm.Match{LastMessage: regexp.QuoteMeta(sidePanelFirstPrompt) + "$"}, ToolCalls: write(sidePanelFirstPath)},
		{When: &stubllm.Match{LastMessage: regexp.QuoteMeta(sidePanelSecondPrompt) + "$"}, ToolCalls: write(sidePanelSecondPath)},
		{When: &stubllm.Match{ToolResult: "write_file"}, Text: sidePanelFirstReply},
		{When: &stubllm.Match{ToolResult: "write_file"}, Text: sidePanelSecondReply},
		{Repeat: true, Text: "OK."},
	}}
}

// panelFilesList reports whether the side panel's column on frame paints the changed-files header
// with a count of count and, on rows below it, every one of paths.
func panelFilesList(frame tuitest.Frame, count int, paths ...string) bool {
	rows := panelColumnRows(frame)
	header := fmt.Sprintf("%s (%d)", sidePanelFilesHeader, count)
	for at, row := range rows {
		if !strings.Contains(row, header) {
			continue
		}
		below := strings.Join(rows[at+1:], "\n")
		for _, path := range paths {
			if !strings.Contains(below, path) {
				return false
			}
		}
		return true
	}
	return false
}

// panelColumnRows is the text of the side panel's column on frame, one string per row: the cells
// right of the separator at the default width, so a path the transcript also names is read off the
// panel alone.
func panelColumnRows(frame tuitest.Frame) []string {
	left := frame.Width() - sidePanelDefaultWidth + 1
	rows := make([]string, frame.Height())
	for y := range rows {
		var row strings.Builder
		for x := left; x < frame.Width(); x++ {
			row.WriteString(frame.Cell(x, y).Rune)
		}
		rows[y] = row.String()
	}
	return rows
}
