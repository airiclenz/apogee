package security

// openNonblock is the flag SafeOpen adds to its read-only open. Windows has no O_NONBLOCK, and a
// named pipe there lives under \\.\pipe\ rather than inside a workspace tree, so nothing is added.
const openNonblock = 0
