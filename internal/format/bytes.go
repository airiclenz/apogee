package format

import "fmt"

// Bytes renders a file size for the session notice: bytes below a kibibyte, else KiB or
// MiB to one decimal (3174 → "3.1 KiB"). Binary units, because the number it describes is a
// file's size on disk.
func Bytes(n int) string {
	const kib = 1024
	switch {
	case n < kib:
		return fmt.Sprintf("%d B", n)
	case n < kib*kib:
		return fmt.Sprintf("%.1f KiB", float64(n)/kib)
	default:
		return fmt.Sprintf("%.1f MiB", float64(n)/(kib*kib))
	}
}
