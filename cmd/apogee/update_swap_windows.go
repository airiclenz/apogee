//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
)

// swapLeavesOldExecutable is true here: Windows will not replace or delete a running executable,
// only rename it, so the swap moves it to `<exe>.old` and the next start sweeps that file.
const swapLeavesOldExecutable = true

// swapExecutable renames the running executable to `<exePath>.old`, then the staged binary to
// exePath. When the second rename fails the first is undone, so exePath still names the old
// binary; when that undo fails too, both errors are reported with the path the old binary is at.
func swapExecutable(stagedPath, exePath string) error {
	oldPath := exePath + oldBinarySuffix
	if err := os.Rename(exePath, oldPath); err != nil {
		return fmt.Errorf("apogee update: could not move %s aside: %w", exePath, err)
	}
	if err := os.Rename(stagedPath, exePath); err != nil {
		if restoreErr := os.Rename(oldPath, exePath); restoreErr != nil {
			return fmt.Errorf("apogee update: could not replace %s, and the old binary is left at %s: %w",
				exePath, oldPath, errors.Join(err, restoreErr))
		}
		return fmt.Errorf("apogee update: could not replace %s: %w", exePath, err)
	}
	return nil
}
