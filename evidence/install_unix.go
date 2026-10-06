//go:build !windows

package evidence

import (
	"os"
	"path/filepath"
)

// The file has already been fsynced and closed by writeBody. Persist the
// directory entry on platforms with POSIX directory-fsync semantics.
func installBody(from, to string) error {
	if err := os.Rename(from, to); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(to))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
