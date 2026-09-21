//go:build !windows

package security

import "syscall"

// openNonblock is the flag SafeOpen adds to its read-only open so a FIFO with no writer cannot
// block the open before the descriptor is fstatted and refused. It stays set on the descriptor
// SafeOpen returns; read(2) and getdents ignore it on a regular file and a directory.
const openNonblock = syscall.O_NONBLOCK
