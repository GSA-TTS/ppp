package cli

import (
	"fmt"
	"io"
	"os"
)

// fileExists reports whether path exists and is not a directory. Any stat error
// other than "not exist" is treated conservatively as "does not exist" so
// callers fall back to their fail-closed defaults.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// outf writes a formatted line to w, propagating any write error so command
// RunE bodies surface (rather than silently drop) output failures. It exists so
// the many status prints stay one-liners while remaining errcheck-clean.
func outf(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format, args...)
	return err
}

// outln writes a single line (with trailing newline) to w, propagating any
// write error. Companion to outf for the no-format case.
func outln(w io.Writer, s string) error {
	_, err := fmt.Fprintln(w, s)
	return err
}
