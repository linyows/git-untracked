package untracked

import (
	"errors"

	"golang.org/x/sys/unix"
)

// clonePath copies src to dst with clonefile(2), which clones a whole
// directory tree in one call on APFS. It falls back to a plain copy on file
// systems without copy-on-write support and reports whether it did so.
func clonePath(src, dst string) (fallback bool, err error) {
	err = unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW)
	if err == nil {
		return false, nil
	}
	if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EXDEV) {
		return true, copyPath(src, dst)
	}
	return false, err
}
