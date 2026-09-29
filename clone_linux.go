package untracked

import (
	"errors"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// clonePath copies src to dst, cloning each regular file with the FICLONE
// ioctl (btrfs, XFS, ...). Once the file system rejects cloning, the rest
// of the tree is copied normally and fallback is reported.
func clonePath(src, dst string) (fallback bool, err error) {
	err = copyTree(src, dst, func(s, d string, perm fs.FileMode) error {
		if fallback {
			return plainCopy(s, d, perm)
		}
		cerr := ficlone(s, d, perm)
		if cerr == nil {
			return nil
		}
		if errors.Is(cerr, unix.EOPNOTSUPP) || errors.Is(cerr, unix.EXDEV) ||
			errors.Is(cerr, unix.EINVAL) || errors.Is(cerr, unix.ENOTTY) {
			fallback = true
			if err := os.Remove(d); err != nil {
				return err
			}
			return plainCopy(s, d, perm)
		}
		return cerr
	})
	return fallback, err
}

func ficlone(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if err := unix.IoctlFileClone(int(out.Fd()), int(in.Fd())); err != nil { //nolint:gosec // file descriptors fit in int
		out.Close()
		return err
	}
	return out.Close()
}
