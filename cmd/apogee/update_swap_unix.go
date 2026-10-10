//go:build !windows

package main

import (
	"fmt"
	"os"
)

// swapLeavesOldExecutable is false here: a Unix rename replaces the running executable's directory
// entry in one step, and the running process keeps its open inode, so no `<exe>.old` is made.
const swapLeavesOldExecutable = false

// swapExecutable moves the staged binary over exePath with one atomic rename: the path names the
// old binary until it names the new one, never neither.
func swapExecutable(stagedPath, exePath string) error {
	if err := os.Rename(stagedPath, exePath); err != nil {
		return fmt.Errorf("apogee update: could not replace %s: %w", exePath, err)
	}
	return nil
}
