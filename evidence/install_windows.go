//go:build windows

package evidence

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// writeBody syncs the file itself before publishing it. Windows cannot flush
// the read-only directory handle used by os.Open: use a write-through move
// instead. Keep all move errors visible; never ignore access-denied failures.
// https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-movefileexw
func installBody(from, to string) error {
	src, err := extendedBodyPath(from)
	if err != nil {
		return err
	}
	dst, err := extendedBodyPath(to)
	if err != nil {
		return err
	}
	err = windows.MoveFileEx(src, dst, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
	if err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	return nil
}

func extendedBodyPath(path string) (*uint16, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(abs, `\\?\`) {
		if strings.HasPrefix(abs, `\\`) {
			abs = `\\?\UNC\` + abs[2:]
		} else {
			abs = `\\?\` + abs
		}
	}
	return windows.UTF16PtrFromString(abs)
}
