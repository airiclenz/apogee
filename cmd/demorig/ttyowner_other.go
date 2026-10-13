//go:build !linux && !windows

package main

import (
	"errors"
	"os"
)

// ttyOwners has no portable source outside Linux's /proc, so the probe reports the owner unknown.
func ttyOwners(*os.File) ([]string, error) {
	return nil, errors.New("owner unknown off linux")
}
